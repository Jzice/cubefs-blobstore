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

package clustermgr

import (
	"context"
	"fmt"

	"github.com/cubefs/blobstore/api/blobnode"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/raftserver"
)

const (
	ConsulRegisterPath = "ebs/%s/clusters/"
)

type ClusterInfo struct {
	Region    string          `json:"region"`
	ClusterID proto.ClusterID `json:"cluster_id"`
	Capacity  int64           `json:"capacity"`
	Available int64           `json:"available"`
	Readonly  bool            `json:"readonly"`
	Nodes     []string        `json:"nodes"`
}

type StatInfo struct {
	LeaderHost string            `json:"leader_host"`
	RaftStatus raftserver.Status `json:"raft_status"`
	SpaceStat  SpaceStatInfo     `json:"space_stat"`
	VolumeStat VolumeStatInfo    `json:"volume_stat"`
}

func GetConsulClusterPath(region string) string {
	return fmt.Sprintf(ConsulRegisterPath, region)
}

// APIAccess sub of cluster manager api for access
type APIAccess interface {
	GetConfig(ctx context.Context, key string) (string, error)
	GetService(ctx context.Context, args GetServiceArgs) (ServiceInfo, error)
	GetVolumeInfo(ctx context.Context, args *GetVolumeArgs) (*VolumeInfo, error)
	DiskInfo(ctx context.Context, id proto.DiskID) (*blobnode.DiskInfo, error)
}

// APIAllocator sub of cluster manager api for allocator
type APIAllocator interface {
	GetConfig(ctx context.Context, key string) (string, error)
	AllocVolume(ctx context.Context, args *AllocVolumeArgs) (AllocatedVolumeInfos, error)
	AllocBid(ctx context.Context, args *BidScopeArgs) (*BidScopeRet, error)
	RetainVolume(ctx context.Context, args *RetainVolumeArgs) (RetainVolumes, error)
	RegisterService(ctx context.Context, node ServiceNode, tickInterval, heartbeatTicks, expiresTicks uint32) error
}

// ClientAPI all interface of cluster manager
type ClientAPI interface {
	APIAccess
	APIAllocator
}
