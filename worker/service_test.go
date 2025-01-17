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

package worker

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/cubefs/blobstore/api/scheduler"
	"github.com/cubefs/blobstore/api/worker"
	"github.com/cubefs/blobstore/common/codemode"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/rpc"
	"github.com/cubefs/blobstore/util/limit/count"
	"github.com/cubefs/blobstore/worker/client"
)

type mBlobNodeCli struct{}

func (m *mBlobNodeCli) StatChunk(ctx context.Context, location proto.VunitLocation) (ci *client.ChunkInfo, err error) {
	return
}

func (m *mBlobNodeCli) StatShard(ctx context.Context, location proto.VunitLocation, bid proto.BlobID) (*client.ShardInfo, error) {
	return nil, nil
}

func (m *mBlobNodeCli) ListShards(ctx context.Context, location proto.VunitLocation) ([]*client.ShardInfo, error) {
	return nil, nil
}

func (m *mBlobNodeCli) GetShard(ctx context.Context, location proto.VunitLocation, bid proto.BlobID) (io.ReadCloser, uint32, error) {
	return nil, 0, nil
}

func (m *mBlobNodeCli) GetShards(ctx context.Context, location proto.VunitLocation, Bids []proto.BlobID) (content map[proto.BlobID]io.Reader, err error) {
	return nil, nil
}

func (m *mBlobNodeCli) PutShard(ctx context.Context, location proto.VunitLocation, bid proto.BlobID, size int64, body io.Reader) (err error) {
	return
}

type mScheCli struct {
	id                int
	inspectID         int
	repairTaskCnt     int
	diskDropTaskCnt   int
	balanceTaskCnt    int
	acquireInspectCnt int
}

func (m *mScheCli) AcquireTask(ctx context.Context, args *api.AcquireArgs) (ret *api.WorkerTask, err error) {
	// fmt.Println("acquire task...")
	m.id++
	mode := codemode.EC6P10L2
	srcReplicas, _ := genMockVol(1, mode)
	destVuid, _ := proto.NewVuid(1, uint8(0), 2)
	dst := proto.VunitLocation{
		Vuid:   destVuid,
		Host:   "http://127.0.0.1",
		DiskID: 1,
	}

	task := api.WorkerTask{}
	switch m.id % 3 {
	case 0:
		task.Repair = &proto.VolRepairTask{
			CodeMode:    mode,
			Destination: dst,
			Sources:     srcReplicas,
		}
		task.TaskType = proto.RepairTaskType
		task.Repair.TaskID = fmt.Sprintf("repair_%d", m.id)
		m.repairTaskCnt++
	case 1:
		task.Balance = &proto.MigrateTask{
			CodeMode:    mode,
			Destination: dst,
			Sources:     srcReplicas,
		}
		task.TaskType = proto.BalanceTaskType
		task.Balance.TaskID = fmt.Sprintf("balance_%d", m.id)
		m.balanceTaskCnt++
	case 2:
		task.DiskDrop = &proto.MigrateTask{
			CodeMode:    mode,
			Destination: dst,
			Sources:     srcReplicas,
		}
		task.TaskType = proto.DiskDropTaskType
		task.DiskDrop.TaskID = fmt.Sprintf("disk_drop_%d", m.id)
		m.diskDropTaskCnt++
	}
	return &task, nil
}

func (m *mScheCli) AcquireInspectTask(ctx context.Context) (ret *api.WorkerInspectTask, err error) {
	m.inspectID++
	m.acquireInspectCnt++
	ret = &api.WorkerInspectTask{}
	task := &proto.InspectTask{}
	task.Mode = codemode.EC6P10L2
	ret.Task = task
	ret.Task.TaskId = fmt.Sprintf("inspect_%d", m.inspectID)
	return
}

func (m *mScheCli) ReclaimTask(ctx context.Context, args *api.ReclaimTaskArgs) error {
	return nil
}

func (m *mScheCli) CancelTask(ctx context.Context, args *api.CancelTaskArgs) error {
	return nil
}

func (m *mScheCli) CompleteTask(ctx context.Context, args *api.CompleteTaskArgs) error {
	return nil
}

func (m *mScheCli) CompleteInspect(ctx context.Context, args *api.CompleteInspectArgs) error {
	return nil
}

// report doing tasks
func (m *mScheCli) RenewalTask(ctx context.Context, args *api.TaskRenewalArgs) (ret *api.TaskRenewalRet, err error) {
	ret = &api.TaskRenewalRet{
		Repair:   make(map[string]string),
		Balance:  make(map[string]string),
		DiskDrop: make(map[string]string),
	}
	return
}

func (m *mScheCli) ReportTask(ctx context.Context, args *api.TaskReportArgs) (err error) {
	return nil
}

func (m *mScheCli) RegisterService(ctx context.Context, args *api.RegisterServiceArgs) (err error) {
	return nil
}

func (m *mScheCli) DeleteService(ctx context.Context, args *api.DeleteServiceArgs) (err error) {
	return nil
}

var (
	workerServer *httptest.Server
	once         sync.Once
	schedulerCli = &mScheCli{}
	blobnodeCli  = &mBlobNodeCli{}
)

func newMockService() *Service {
	scheduler := schedulerCli
	blobnode := blobnodeCli
	wf := &mockWorkerFactory{
		newRepairWorkerFn: NewMockRepairWorker,
		newMigWorkerFn:    NewmockMigrateWorker,
	}
	return &Service{
		shardRepairLimit: count.New(1),
		inspectTaskMgr:   NewInspectTaskMgr(1, blobnode, scheduler),
		taskRenter: NewTaskRenter("z0", scheduler, NewTaskRunnerMgr(0, 2, 2,
			2, 2, scheduler, wf)),
		schedulerCli: scheduler,
		blobNodeCli:  blobnode,
		Config:       Config{AcquireIntervalMs: 1},

		acquireCh: make(chan struct{}, 1),
		closeCh:   make(chan struct{}, 1),

		taskRunnerMgr: NewTaskRunnerMgr(0, 2, 2,
			2, 2, scheduler, wf),
	}
}

func runMockService(s *Service) string {
	once.Do(func() {
		workerServer = httptest.NewServer(NewHandler(s))
	})
	return workerServer.URL
}

func TestServiceAPI(t *testing.T) {
	runMockService(newMockService())
	workerCli := worker.New(&worker.Config{})
	testCases := []struct {
		args *worker.ShardRepairArgs
		code int
	}{
		{
			args: &worker.ShardRepairArgs{
				Task: proto.ShardRepairTask{},
			},
			code: 704,
		},
	}
	for _, tc := range testCases {
		err := workerCli.RepairShard(context.Background(), workerServer.URL, tc.args)
		require.Equal(t, tc.code, rpc.DetectStatusCode(err))
	}

	_, err := workerCli.Stats(context.Background(), workerServer.URL)
	require.NoError(t, err)
}

func TestSvr(t *testing.T) {
	svr := newMockService()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		svr.Run()
		wg.Done()
	}()

	time.Sleep(100 * time.Millisecond)
	svr.Close()
	wg.Wait()

	require.Equal(t, schedulerCli.repairTaskCnt, len(svr.taskRunnerMgr.repair))
	require.Equal(t, schedulerCli.balanceTaskCnt, len(svr.taskRunnerMgr.balance))
	require.Equal(t, schedulerCli.diskDropTaskCnt, len(svr.taskRunnerMgr.diskDrop))
}

func TestNewService(t *testing.T) {
	cfg := &Config{
		ServiceRegister: ServiceRegisterConfig{Host: "http://127.0.0.1:123"},
	}

	_, err := NewService(cfg)
	require.NoError(t, err)

	cfg = &Config{}
	_, err = NewService(cfg)
	require.Error(t, err)
}

func TestFixConfigItem(t *testing.T) {
	var item int
	item = 0
	fixConfigItemInt(&item, 100)
	require.Equal(t, item, 100)

	item = -1
	fixConfigItemInt(&item, 100)
	require.Equal(t, item, 100)

	item = 10
	fixConfigItemInt(&item, 100)
	require.Equal(t, item, 10)
}

func TestCfgFix(t *testing.T) {
	var cfg Config
	err := cfg.checkAndFix()
	require.Error(t, err)

	cfg.ServiceRegister.Host = "host1"

	err = cfg.checkAndFix()
	require.NoError(t, err)
	fixConfigItemInt(&cfg.AcquireIntervalMs, 500)
	fixConfigItemInt(&cfg.MaxTaskRunnerCnt, 1)
	fixConfigItemInt(&cfg.RepairConcurrency, 1)
	fixConfigItemInt(&cfg.BalanceConcurrency, 1)
	fixConfigItemInt(&cfg.DiskDropConcurrency, 1)
	fixConfigItemInt(&cfg.ShardRepairConcurrency, 1)
	fixConfigItemInt(&cfg.InspectConcurrency, 1)
	fixConfigItemInt(&cfg.DownloadShardConcurrency, 10)

	require.Equal(t, 500, cfg.AcquireIntervalMs)
	require.Equal(t, 1, cfg.MaxTaskRunnerCnt)
	require.Equal(t, 1, cfg.RepairConcurrency)
	require.Equal(t, 1, cfg.BalanceConcurrency)
	require.Equal(t, 1, cfg.DiskDropConcurrency)
	require.Equal(t, 1, cfg.ShardRepairConcurrency)
	require.Equal(t, 1, cfg.InspectConcurrency)
	require.Equal(t, 10, cfg.DownloadShardConcurrency)

	cfg.AcquireIntervalMs = 600
	cfg.MaxTaskRunnerCnt = 100
	cfg.RepairConcurrency = 100
	cfg.BalanceConcurrency = 100
	cfg.DiskDropConcurrency = 100
	cfg.ShardRepairConcurrency = 100
	cfg.InspectConcurrency = 100
	cfg.DownloadShardConcurrency = 100

	require.Equal(t, 600, cfg.AcquireIntervalMs)
	require.Equal(t, 100, cfg.MaxTaskRunnerCnt)
	require.Equal(t, 100, cfg.RepairConcurrency)
	require.Equal(t, 100, cfg.BalanceConcurrency)
	require.Equal(t, 100, cfg.DiskDropConcurrency)
	require.Equal(t, 100, cfg.ShardRepairConcurrency)
	require.Equal(t, 100, cfg.InspectConcurrency)
	require.Equal(t, 100, cfg.DownloadShardConcurrency)
}
