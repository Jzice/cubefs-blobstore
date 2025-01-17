// Copyright 2022 The CubeFS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/api"

	cmapi "github.com/cubefs/blobstore/api/clustermgr"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/redis"
	"github.com/cubefs/blobstore/common/trace"
	"github.com/cubefs/blobstore/util/errors"
	"github.com/cubefs/blobstore/util/log"
)

// TODO: how to stop service of one cluster???

// AlgChoose algorithm of choose cluster
type AlgChoose uint32

const (
	minAlg AlgChoose = iota
	// AlgAvailable available capacity and some random alloc
	AlgAvailable
	// AlgRandom completely random alloc
	AlgRandom
	maxAlg
)

func (alg AlgChoose) String() string {
	switch alg {
	case AlgAvailable:
		return "Available"
	case AlgRandom:
		return "Random"
	default:
		return "Unknow"
	}
}

// errors
var (
	ErrNoSuchCluster   = errors.New("cluster not found")
	ErrInvalidAllocAlg = errors.New("invalid alloc algorithm")
)

// ClusterController controller of clusters in one region
type ClusterController interface {
	// Region returns region in configuration
	Region() string
	// All returns all cluster info in this region
	All() []*cmapi.ClusterInfo
	// ChooseOne returns a available cluster to upload
	ChooseOne() (*cmapi.ClusterInfo, error)
	// GetServiceController return ServiceController in specified cluster
	GetServiceController(clusterID proto.ClusterID) (ServiceController, error)
	// GetVolumeGetter return VolumeGetter in specified cluster
	GetVolumeGetter(clusterID proto.ClusterID) (VolumeGetter, error)
	// GetConfig get specified config of key from cluster manager
	GetConfig(ctx context.Context, key string) (string, error)
	// ChangeChooseAlg change alloc algorithm
	ChangeChooseAlg(alg AlgChoose) error
}

// IsValidAlg choose algorithm is valid or not
func IsValidAlg(alg AlgChoose) bool {
	return alg > minAlg && alg < maxAlg
}

// ClusterConfig cluster config
//
// Region and RegionMagic are paired,
// magic cannot change if one region was deployed.
type ClusterConfig struct {
	IDC               string              `json:"-"`
	Region            string              `json:"region"`
	RegionMagic       string              `json:"region_magic"`
	ClusterReloadSecs int                 `json:"cluster_reload_secs"`
	ServiceReloadSecs int                 `json:"service_reload_secs"`
	CMClientConfig    cmapi.Config        `json:"clustermgr_client_config"`
	RedisClientConfig redis.ClusterConfig `json:"redis_client_config"`

	ServicePunishThreshold      uint32 `json:"service_punish_threshold"`
	ServicePunishValidIntervalS int    `json:"service_punish_valid_interval_s"`
}

type cluster struct {
	clusterInfo *cmapi.ClusterInfo
	client      *cmapi.Client
}

type clusterMap map[proto.ClusterID]*cluster

type clusterQueue []*cmapi.ClusterInfo

type clusterControllerImpl struct {
	region           string
	kvClient         *api.Client
	allocAlg         uint32
	totalAvailableTB int64
	clusters         atomic.Value // all clusters
	available        atomic.Value // available clusters
	serviceMgrs      sync.Map
	volumeGetters    sync.Map
	roundRobinCount  uint64 // a count for round robin

	config ClusterConfig
}

// NewClusterController returns a cluster controller
func NewClusterController(cfg *ClusterConfig, kvClient *api.Client) (ClusterController, error) {
	controller := &clusterControllerImpl{
		region:   cfg.Region,
		kvClient: kvClient,
		config:   *cfg,
	}
	atomic.StoreUint32(&controller.allocAlg, uint32(AlgAvailable))

	err := controller.load()
	if err != nil {
		return nil, errors.Base(err, "load cluster failed")
	}

	if cfg.ClusterReloadSecs <= 0 {
		cfg.ClusterReloadSecs = 3
	}
	tick := time.NewTicker(time.Duration(cfg.ClusterReloadSecs) * time.Second)
	go func() {
		defer tick.Stop()
		for range tick.C {
			if err := controller.load(); err != nil {
				log.Warn("load timer error", err)
			}
		}
	}()
	return controller, nil
}

func (c *clusterControllerImpl) load() error {
	span := trace.SpanFromContextSafe(context.Background())

	path := cmapi.GetConsulClusterPath(c.region)
	span.Debug("to list consul path", path)

	pairs, _, err := c.kvClient.KV().List(path, nil)
	if err != nil {
		return err
	}
	span.Debugf("found %d clusters", len(pairs))

	allClusters := make(clusterMap)
	available := make([]*cmapi.ClusterInfo, 0, len(pairs))
	totalAvailableTB := int64(0)
	for _, pair := range pairs {
		clusterInfo := &cmapi.ClusterInfo{}
		err := json.Unmarshal(pair.Value, clusterInfo)
		if err != nil {
			span.Warnf("decode failed, raw:%s, error:%s", string(pair.Value), err.Error())
			continue
		}

		clusterKey := filepath.Base(pair.Key)
		span.Debug("found cluster", clusterKey)

		clusterID, err := strconv.Atoi(clusterKey)
		if err != nil {
			span.Warn("invalid cluster id", clusterKey, err)
			continue
		}
		if clusterInfo.ClusterID != proto.ClusterID(clusterID) {
			span.Warn("mismatch cluster id", clusterInfo.ClusterID, clusterID)
			continue
		}

		allClusters[proto.ClusterID(clusterID)] = &cluster{clusterInfo: clusterInfo}
		if !clusterInfo.Readonly && clusterInfo.Available > 0 {
			available = append(available, clusterInfo)
			totalAvailableTB += int64(clusterInfo.Available >> 40)
		} else {
			span.Debug("readonly or no available cluster", clusterID)
		}
	}

	sort.Slice(available, func(i, j int) bool {
		return available[i].Capacity < available[j].Capacity
	})

	newClusters := make([]*cmapi.ClusterInfo, 0, len(allClusters))
	for clusterID := range allClusters {
		if _, ok := c.serviceMgrs.Load(clusterID); !ok {
			newClusters = append(newClusters, allClusters[clusterID].clusterInfo)
		}
	}

	for _, newCluster := range newClusters {
		conf := c.config.CMClientConfig
		conf.Hosts = newCluster.Nodes
		cmCli := cmapi.New(&conf)

		clusterID := newCluster.ClusterID
		allClusters[clusterID].client = cmCli

		removeThisCluster := func() {
			delete(allClusters, clusterID)
			if !newCluster.Readonly && newCluster.Available > 0 {
				for j := range available {
					if available[j].ClusterID == clusterID {
						available = append(available[:j], available[j+1:]...)
						break
					}
				}
			}
		}

		serviceController, err := NewServiceController(ServiceConfig{
			ClusterID:                   clusterID,
			IDC:                         c.config.IDC,
			ReloadSec:                   c.config.ServiceReloadSecs,
			ServicePunishThreshold:      c.config.ServicePunishThreshold,
			ServicePunishValidIntervalS: c.config.ServicePunishValidIntervalS,
		}, cmCli)
		if err != nil {
			removeThisCluster()
			span.Warn("new service manager failed", clusterID, err)
			continue
		}

		var redisCli *redis.ClusterClient
		if len(c.config.RedisClientConfig.Addrs) > 0 {
			redisCli = redis.NewClusterClient(&c.config.RedisClientConfig)
		}
		volumeGetter, err := NewVolumeGetter(clusterID, cmCli, redisCli, -1)
		if err != nil {
			removeThisCluster()
			span.Warn("new volume getter failed", clusterID, err)
			continue
		}

		c.serviceMgrs.Store(clusterID, serviceController)
		c.volumeGetters.Store(clusterID, volumeGetter)

		span.Debug("loaded new cluster", clusterID)
	}

	c.clusters.Store(allClusters)
	c.available.Store(clusterQueue(available))
	atomic.StoreInt64(&c.totalAvailableTB, totalAvailableTB)

	span.Infof("loaded %d clusters, %d available, total %dTB", len(allClusters), len(available), totalAvailableTB)
	return nil
}

func (c *clusterControllerImpl) Region() string {
	return c.region
}

func (c *clusterControllerImpl) All() []*cmapi.ClusterInfo {
	allClusters := c.clusters.Load().(clusterMap)

	ret := make([]*cmapi.ClusterInfo, 0, len(allClusters))
	for _, clusterInfo := range allClusters {
		ret = append(ret, clusterInfo.clusterInfo)
	}

	return ret
}

func (c *clusterControllerImpl) ChooseOne() (*cmapi.ClusterInfo, error) {
	alg := AlgChoose(atomic.LoadUint32(&c.allocAlg))

	switch alg {
	case AlgAvailable:
		totalAvailableTB := atomic.LoadInt64(&c.totalAvailableTB)
		if totalAvailableTB <= 0 {
			return nil, fmt.Errorf("no available space %d", totalAvailableTB)
		}
		randValue := rand.Int63n(totalAvailableTB)
		available := c.available.Load().(clusterQueue)
		for _, cluster := range available {
			if cluster.Available>>40 >= randValue {
				return cluster, nil
			}
			randValue -= cluster.Available >> 40
		}
		return nil, fmt.Errorf("no available cluster by %s", alg.String())

	case AlgRandom:
		available := c.available.Load().(clusterQueue)
		if len(available) > 0 {
			count := atomic.AddUint64(&c.roundRobinCount, 1)
			length := uint64(len(available))
			return available[count%length], nil
		}
		return nil, fmt.Errorf("no available cluster by %s", alg.String())
	}

	return nil, fmt.Errorf("not implemented algorithm %s(%d)", alg.String(), alg)
}

func (c *clusterControllerImpl) ChangeChooseAlg(alg AlgChoose) error {
	if !IsValidAlg(alg) {
		return ErrInvalidAllocAlg
	}

	atomic.StoreUint32(&c.allocAlg, uint32(alg))
	return nil
}

func (c *clusterControllerImpl) GetServiceController(clusterID proto.ClusterID) (ServiceController, error) {
	if serviceController, exist := c.serviceMgrs.Load(clusterID); exist {
		if controller, ok := serviceController.(ServiceController); ok {
			return controller, nil
		}
		return nil, fmt.Errorf("not service controller for %d", clusterID)
	}
	return nil, fmt.Errorf("no service controller of %d", clusterID)
}

func (c *clusterControllerImpl) GetVolumeGetter(clusterID proto.ClusterID) (VolumeGetter, error) {
	if volumeGetter, exist := c.volumeGetters.Load(clusterID); exist {
		if getter, ok := volumeGetter.(VolumeGetter); ok {
			return getter, nil
		}
		return nil, fmt.Errorf("not volume getter for %d", clusterID)
	}
	return nil, fmt.Errorf("no volume getter for %d", clusterID)
}

func (c *clusterControllerImpl) GetConfig(ctx context.Context, key string) (ret string, err error) {
	span := trace.SpanFromContextSafe(ctx)

	allClusters := c.clusters.Load().(clusterMap)
	if len(allClusters) == 0 {
		return "", ErrNoSuchCluster
	}

	for _, cluster := range allClusters {
		if ret, err = cluster.client.GetConfig(ctx, key); err == nil {
			return
		}
		span.Warnf("get config[%s] from cluster[%d] failed, err: %v", key, cluster.clusterInfo.ClusterID, err)
	}
	return
}
