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
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/cubefs/blobstore/api/scheduler"
	"github.com/cubefs/blobstore/common/proto"
)

type mockWorker struct {
	failIdx    int
	taskRetErr error

	checkRetErr    error
	genTaskletsErr error

	taskLetCntMu sync.Mutex
	taskLetCnt   int

	sleepS int
}

func (w *mockWorker) GenTasklets(ctx context.Context) ([]Tasklet, *WorkError) {
	tasklets := []Tasklet{
		{bids: mockGenTasklet([]proto.BlobID{1})},
		{bids: mockGenTasklet([]proto.BlobID{2})},
		{bids: mockGenTasklet([]proto.BlobID{3})},
		{bids: mockGenTasklet([]proto.BlobID{4})},
		{bids: mockGenTasklet([]proto.BlobID{5})},
		{bids: mockGenTasklet([]proto.BlobID{6})},
		{bids: mockGenTasklet([]proto.BlobID{7})},
		{bids: mockGenTasklet([]proto.BlobID{8})},
		{bids: mockGenTasklet([]proto.BlobID{9})},
		{bids: mockGenTasklet([]proto.BlobID{10})},
		{bids: mockGenTasklet([]proto.BlobID{11})},
		{bids: mockGenTasklet([]proto.BlobID{12})},
	}
	if w.genTaskletsErr != nil {
		return tasklets, SrcError(w.genTaskletsErr)
	}
	return tasklets, nil
}

func (w *mockWorker) ExecTasklet(ctx context.Context, t Tasklet) *WorkError {
	fmt.Println("ExecTasklet ", t)
	time.Sleep(time.Duration(w.sleepS) * time.Second)
	w.taskLetCntMu.Lock()
	defer w.taskLetCntMu.Unlock()
	w.taskLetCnt++
	if w.taskLetCnt == w.failIdx {
		return OtherError(w.taskRetErr)
	}
	return nil
}

func (w *mockWorker) Check(ctx context.Context) *WorkError {
	return OtherError(w.checkRetErr)
}

func (w *mockWorker) CancelArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation) {
	return "test_mock_task", "repair", []proto.VunitLocation{}, proto.VunitLocation{}
}

func (w *mockWorker) CompleteArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation) {
	return "test_mock_task", "repair", []proto.VunitLocation{}, proto.VunitLocation{}
}

func (w *mockWorker) ReclaimArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation) {
	return "test_mock_task", "repair", []proto.VunitLocation{}, proto.VunitLocation{}
}

func (w *mockWorker) TaskType() string {
	return "repair"
}

func (w *mockWorker) GetBenchmarkBids() (bids []*ShardInfoSimple) {
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 1})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 2})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 3})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 4})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 5})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 6})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 7})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 8})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 9})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 10})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 11})
	bids = append(bids, &ShardInfoSimple{Size: 0, Bid: 12})
	return
}

type mockCli struct {
	cancelRet   error
	completeRet error
	reclaimRet  error
	wg          sync.WaitGroup
	step        string
}

func (mock *mockCli) CancelTask(ctx context.Context, args *api.CancelTaskArgs) error {
	mock.step = "CancelOrReclaim"
	mock.wg.Done()
	return mock.cancelRet
}

func (mock *mockCli) CompleteTask(ctx context.Context, args *api.CompleteTaskArgs) error {
	mock.step = "Complete"
	mock.wg.Done()
	return mock.completeRet
}

func (mock *mockCli) ReclaimTask(ctx context.Context, args *api.ReclaimTaskArgs) error {
	mock.step = "CancelOrReclaim"
	mock.wg.Done()
	return mock.reclaimRet
}

func (mock *mockCli) ReportTask(ctx context.Context, args *api.TaskReportArgs) (err error) {
	return nil
}

func TestTaskRunner(t *testing.T) {
	cli := mockCli{
		cancelRet:   nil,
		completeRet: nil,
		reclaimRet:  nil,
	}

	// test stop
	fmt.Println("start test tasklet stop")
	w1 := mockWorker{
		taskRetErr:  nil,
		checkRetErr: nil,
		sleepS:      1,
	}
	idc := "z0"
	runner1 := NewTaskRunner(context.Background(), "test_mock_task", &w1, idc, 2, &cli)
	cli.wg.Add(1)
	go runner1.Run()
	time.Sleep(500 * time.Millisecond)
	runner1.Stop()
	cli.wg.Wait()
	require.Equal(t, "CancelOrReclaim", cli.step)
	require.Equal(t, true, w1.taskLetCnt < 12)

	// test tasklet fail
	fmt.Println("start test tasklet fail")
	w2 := mockWorker{
		taskRetErr:  errors.New("mock fail"),
		failIdx:     3,
		checkRetErr: nil,
		sleepS:      0,
	}
	runner2 := NewTaskRunner(context.Background(), "test_mock_task", &w2, idc, 3, &cli)
	cli.wg.Add(1)
	go runner2.Run()
	cli.wg.Wait()
	require.Equal(t, "CancelOrReclaim", cli.step)
	require.Equal(t, true, w1.taskLetCnt < 12)

	// test check fail
	fmt.Println("start test check fail")
	w3 := mockWorker{
		taskRetErr:  nil,
		checkRetErr: errors.New("mock check fail"),
		sleepS:      0,
	}

	fmt.Printf("-------------->runner3 start run\n")
	runner3 := NewTaskRunner(context.Background(), "test_mock_task", &w3, idc, 3, &cli)
	cli.wg.Add(1)
	go runner3.Run()
	cli.wg.Wait()
	require.Equal(t, "CancelOrReclaim", cli.step)
	require.Equal(t, 12, w3.taskLetCnt)

	// test genTasklet fail
	fmt.Println("start test genTasklet fail")
	w4 := mockWorker{
		taskRetErr:     nil,
		genTaskletsErr: errors.New("mock check fail"),
		sleepS:         0,
	}
	runner4 := NewTaskRunner(context.Background(), "test_mock_task", &w4, idc, 3, &cli)
	cli.wg.Add(1)
	go runner4.Run()
	cli.wg.Wait()
	require.Equal(t, "CancelOrReclaim", cli.step)
	require.Equal(t, 0, w4.taskLetCnt)

	// test tasklet complete
	fmt.Println("start test tasklet complete")
	w5 := mockWorker{
		sleepS: 0,
	}
	runner5 := NewTaskRunner(context.Background(), "test_mock_task", &w5, idc, 3, &cli)
	cli.wg.Add(1)
	go runner5.Run()
	cli.wg.Wait()
	require.Equal(t, "Complete", cli.step)
	require.Equal(t, 12, w5.taskLetCnt)
}

func TestTaskState(t *testing.T) {
	s := TaskState{}
	s.setStatus(TaskRunning)
	require.Equal(t, TaskRunning, s.state)
	s.setStatus(TaskRunning)
	require.Equal(t, s.state, TaskRunning)
	require.Equal(t, true, s.alive())
	s.setStatus(TaskStopping)
	require.Equal(t, s.state, TaskStopping)
	require.Equal(t, false, s.alive())
	require.Equal(t, false, s.stopped())

	s.setStatus(TaskStopped)
	require.Equal(t, true, s.stopped())
	require.Equal(t, false, s.alive())
	s.setStatus(TaskSuccess)
	require.Equal(t, false, s.alive())
	require.Equal(t, true, s.stopped())
}
