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
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cubefs/blobstore/api/clustermgr"
	"github.com/cubefs/blobstore/clustermgr/diskmgr"
	"github.com/cubefs/blobstore/clustermgr/volumemgr"
	"github.com/cubefs/blobstore/common/codemode"
	apierrors "github.com/cubefs/blobstore/common/errors"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/raftserver"
	"github.com/cubefs/blobstore/common/rpc"
	"github.com/cubefs/blobstore/common/trace"
)

var testServiceCfg = &Config{
	Region:    "test-region",
	IDC:       []string{"z0", "z1", "z2"},
	ClusterID: 1,
	Readonly:  false,
	VolumeMgrConfig: volumemgr.VolumeMgrConfig{
		VolumeDBPath: "/tmp/tmpsvrvolumedb-" + strconv.Itoa(rand.Intn(100000000)),
	},
	NormalDBPath: "/tmp/tmpsvrnormaldb-" + strconv.Itoa(rand.Intn(100000000)),
	CodeModePolicies: []codemode.Policy{
		{
			ModeName:  codemode.EC15P12.Name(),
			MinSize:   1048577,
			MaxSize:   1073741824,
			SizeRatio: 0.8,
			Enable:    true,
		},
		{
			ModeName:  codemode.EC6P6.Name(),
			MinSize:   0,
			MaxSize:   1048576,
			SizeRatio: 0.2,
			Enable:    true,
		},
	},
	ClusterCfg:               map[string]interface{}{},
	ClusterReportIntervalS:   1,
	MetricReportIntervalM:    1,
	HeartbeatNotifyIntervalS: 1,
	ChunkSize:                17179869184,
	RaftConfig: RaftConfig{
		RaftDBPath: "/tmp/tmpsvrraftdb-" + strconv.Itoa(rand.Intn(10000000)),
		ServerConfig: raftserver.Config{
			NodeId:       1,
			ListenPort:   GetFreePort(),
			TickInterval: 1,
			ElectionTick: 2,
			WalDir:       "/tmp/tmpsvrraftwal-" + strconv.Itoa(rand.Intn(10000000)),
			Peers:        map[uint64]string{1: "127.0.0.1:60110"},
		},
	},
	DiskMgrConfig: diskmgr.DiskMgrConfig{
		RefreshIntervalS: 300,
		RackAware:        false,
		HostAware:        true,
	},
}

func clear(testService *Service) {
	os.RemoveAll(testService.NormalDBPath)
	os.RemoveAll(testService.VolumeMgrConfig.VolumeDBPath)
	os.RemoveAll(testService.RaftConfig.RaftDBPath)
	os.RemoveAll(testService.RaftConfig.ServerConfig.WalDir)
}

func initTestService(t *testing.T) *Service {
	cfg := *testServiceCfg

	cfg.NormalDBPath = "/tmp/tmpsvrnormaldb-" + strconv.Itoa(rand.Intn(10000000))
	cfg.VolumeMgrConfig.VolumeDBPath = "/tmp/tmpsvrvolumedb-" + strconv.Itoa(rand.Intn(10000000))
	cfg.RaftConfig.RaftDBPath = "/tmp/tmpsvrraftdb-" + strconv.Itoa(rand.Intn(10000000))
	cfg.RaftConfig.ServerConfig.WalDir = "/tmp/tmpsvrraftwal-" + strconv.Itoa(rand.Intn(10000000))
	cfg.ClusterCfg[proto.VolumeReserveSizeKey] = "20000000"
	os.Mkdir(cfg.NormalDBPath, 0o755)
	os.Mkdir(cfg.VolumeMgrConfig.VolumeDBPath, 0o755)
	os.Mkdir(cfg.RaftConfig.RaftDBPath, 0o755)

	testService, err := New(&cfg)
	assert.NoError(t, err)

	return testService
}

func initTestClusterClient(testService *Service) *clustermgr.Client {
	// mux := server.MockRegist(testService, http.NewServeMux())
	ph := rpc.DefaultRouter.Router.PanicHandler
	rpc.DefaultRouter = rpc.New()
	rpc.DefaultRouter.Router.PanicHandler = ph
	mux := NewHandler(testService)
	server := httptest.NewServer(mux)
	return clustermgr.New(&clustermgr.Config{
		LbConfig: rpc.LbConfig{
			Hosts: []string{server.URL},
		},
	})
}

func TestBidAlloc(t *testing.T) {
	testService := initTestService(t)
	defer clear(testService)
	defer testService.Close()
	testClusterClient := initTestClusterClient(testService)

	_, ctx := trace.StartSpanFromContext(context.Background(), "")
	// test bid alloc
	{
		ret, err := testClusterClient.AllocBid(ctx, &clustermgr.BidScopeArgs{Count: 10})
		assert.NoError(t, err)
		assert.Equal(t, proto.BlobID(1), ret.StartBid)
		assert.Equal(t, proto.BlobID(10), ret.EndBid)

		_, err = testClusterClient.AllocBid(ctx, &clustermgr.BidScopeArgs{Count: 100001})
		assert.Error(t, err)
	}
}

func TestManage(t *testing.T) {
	testService := initTestService(t)
	defer clear(testService)
	defer testService.Close()
	testClusterClient := initTestClusterClient(testService)

	_, ctx := trace.StartSpanFromContext(context.Background(), "")

	// test member add or remove
	{
		err := testClusterClient.AddMember(ctx, &clustermgr.AddMemberArgs{PeerID: 2, Host: "127.0.0.1", MemberType: clustermgr.MemberTypeMin})
		assert.NotNil(t, err)

		err = testClusterClient.AddMember(ctx, &clustermgr.AddMemberArgs{PeerID: 2, Host: "127.0.0.1", MemberType: clustermgr.MemberTypeNormal})
		assert.NoError(t, err)

		err = testClusterClient.RemoveMember(ctx, 10)
		assert.Equal(t, apierrors.ErrIllegalArguments.Error(), err.Error())
		err = testClusterClient.RemoveMember(ctx, 1)
		assert.Equal(t, apierrors.ErrRequestNotAllow.Error(), err.Error())

		err = testClusterClient.AddMember(ctx, &clustermgr.AddMemberArgs{PeerID: 2, Host: "127.0.0.1", MemberType: clustermgr.MemberTypeNormal})
		assert.Equal(t, apierrors.CodeDuplicatedMemberInfo, err.(rpc.HTTPError).StatusCode())

		err = testClusterClient.TransferLeadership(ctx, 2)
		assert.NoError(t, err)
	}

	// test stat
	{
		statInfo, err := testClusterClient.Stat(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, statInfo)
	}
}

type mockWriter struct{}

func (m *mockWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (m *mockWriter) Header() http.Header {
	return http.Header{}
}

func (m *mockWriter) WriteHeader(statusCode int) {
}

func TestNewService(t *testing.T) {
	cfg := *testServiceCfg

	cfg.ClusterReportIntervalS = 0
	cfg.MetricReportIntervalM = 0
	cfg.HeartbeatNotifyIntervalS = 0

	cfg.NormalDBPath = "/tmp/tmpsvrnormaldb-" + strconv.Itoa(rand.Intn(10000000))
	cfg.VolumeMgrConfig.VolumeDBPath = "/tmp/tmpsvrvolumedb-" + strconv.Itoa(rand.Intn(10000000))
	cfg.RaftConfig.RaftDBPath = "/tmp/tmpsvrraftdb-" + strconv.Itoa(rand.Intn(10000000))
	cfg.RaftConfig.ServerConfig.WalDir = "/tmp/tmpsvrraftwal-" + strconv.Itoa(rand.Intn(10000000))
	cfg.ClusterCfg[proto.VolumeReserveSizeKey] = "20000000"
	os.Mkdir(cfg.NormalDBPath, 0o755)
	os.Mkdir(cfg.VolumeMgrConfig.VolumeDBPath, 0o755)
	os.Mkdir(cfg.RaftConfig.RaftDBPath, 0o755)

	testService, err := New(&cfg)
	assert.NoError(t, err)
	assert.NotNil(t, testService)

	testService.report(context.Background())
	testService.metricReport(context.Background())

	req, err := http.NewRequest(http.MethodPost, "/", nil)
	assert.NoError(t, err)

	testService.forwardToLeader(&mockWriter{}, req)

	defer clear(testService)
	defer testService.Close()
}

func GetFreePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
