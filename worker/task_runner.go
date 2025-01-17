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

// duties：schedule long period task(disk repair & balance & disk drop) to run
// a task split many tasklets to run in concurrent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	api "github.com/cubefs/blobstore/api/scheduler"
	comerrors "github.com/cubefs/blobstore/common/errors"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/rpc"
	"github.com/cubefs/blobstore/common/trace"
	"github.com/cubefs/blobstore/util/taskpool"
	"github.com/cubefs/blobstore/worker/base"
)

// task runner status
const (
	TaskInit = iota + 1
	TaskRunning
	TaskStopping
	TaskSuccess
	TaskStopped
)

var errKilled = errors.New("task killed")

// TaskState task state
type TaskState struct {
	mu    sync.Mutex
	state int
}

func (ts *TaskState) setStatus(s int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if s == ts.state {
		return
	}

	if s == TaskRunning && ts.stoppedState() {
		return
	}

	ts.state = s
}

func (ts *TaskState) stopped() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.stoppedState()
}

func (ts *TaskState) stoppedState() bool {
	if ts.state == TaskSuccess || ts.state == TaskStopped {
		return true
	}
	return false
}

func (ts *TaskState) alive() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.state == TaskInit || ts.state == TaskRunning {
		return true
	}
	return false
}

// WokeErrorType worker error type
type WokeErrorType uint8

const (
	// StatusInterrupt interrupt error status
	StatusInterrupt = 596
)

// task runner error type
const (
	DstErr WokeErrorType = iota + 1
	SrcErr
	OtherErr
)

// WorkError with error type and error
type WorkError struct {
	errType WokeErrorType
	err     error
}

// String return error message with error type and error detail message
func (e *WorkError) String() string {
	return fmt.Sprintf("type %v, %v", e.errType, e.err)
}

// Error returns error info
func (e *WorkError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return ""
}

// DstError returns destination error type
func DstError(err error) *WorkError {
	return genWorkError(err, DstErr)
}

// SrcError returns source error type
func SrcError(err error) *WorkError {
	return genWorkError(err, SrcErr)
}

// OtherError returns other error type
func OtherError(err error) *WorkError {
	return genWorkError(err, OtherErr)
}

// ShouldReclaim returns true if the task should reclaim
func ShouldReclaim(e *WorkError) bool {
	if e.errType != DstErr {
		return false
	}

	errCode := rpc.DetectStatusCode(e.err)
	if errCode == StatusInterrupt {
		return false
	}

	if errCode == comerrors.CodeOverload {
		return false
	}

	return true
}

func genWorkError(err error, errType WokeErrorType) *WorkError {
	if err == nil {
		return nil
	}
	return &WorkError{errType: errType, err: err}
}

// ITaskWorker define interface used for task execution
type ITaskWorker interface {
	// split tasklets accord by volume benchmark bids
	GenTasklets(ctx context.Context) ([]Tasklet, *WorkError)
	// define tasklet execution operator ,eg:disk repair & migrate
	ExecTasklet(ctx context.Context, t Tasklet) *WorkError
	// check whether the task is executed successfully when volume task finish
	Check(ctx context.Context) *WorkError
	CancelArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation)
	CompleteArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation)
	ReclaimArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation)
	TaskType() (taskType string)
	GetBenchmarkBids() []*ShardInfoSimple
}

// TaskSchedulerCli define the interface of scheuler use
type TaskSchedulerCli interface {
	ReclaimTask(ctx context.Context, args *api.ReclaimTaskArgs) (err error)
	CancelTask(ctx context.Context, args *api.CancelTaskArgs) (err error)
	CompleteTask(ctx context.Context, args *api.CompleteTaskArgs) (err error)
	ReportTask(ctx context.Context, args *api.TaskReportArgs) (err error)
}

// Tasklet is the smallest unit of task exe
type Tasklet struct {
	bids []*ShardInfoSimple
}

// DataSizeByte returns total bids size
func (t *Tasklet) DataSizeByte() uint64 {
	var dataSize uint64
	for _, info := range t.bids {
		dataSize += uint64(info.Size)
	}
	return dataSize
}

// TaskRunner used to manage task
type TaskRunner struct {
	taskID string
	w      ITaskWorker
	idc    string

	taskletRunConcurrency int
	state                 TaskState
	ctx                   context.Context
	cancel                context.CancelFunc

	stopMu     sync.Mutex
	stopReason *WorkError

	schedulerCli TaskSchedulerCli

	statsMu sync.Mutex
	stats   proto.TaskStatistics // work run statics info
}

// NewTaskRunner return task runner
func NewTaskRunner(
	ctx context.Context,
	taskID string,
	w ITaskWorker,
	idc string,
	taskletRunConcurrency int,
	schedulerCli TaskSchedulerCli) *TaskRunner {
	ctx, cancel := context.WithCancel(ctx)
	_, ctx = trace.StartSpanFromContext(ctx, "taskRunner")

	task := TaskRunner{
		taskID:                taskID,
		w:                     w,
		idc:                   idc,
		taskletRunConcurrency: taskletRunConcurrency,
		ctx:                   ctx,
		cancel:                cancel,
		schedulerCli:          schedulerCli,
	}
	task.state.setStatus(TaskInit)
	return &task
}

// Run runs task
func (r *TaskRunner) Run() {
	span := trace.SpanFromContext(r.ctx)
	span.Infof("start run task: taskID[%s]", r.taskID)

	r.state.setStatus(TaskRunning)

	tasklets, err := r.w.GenTasklets(r.ctx)
	if err != nil {
		span.Errorf("generate tasklets failed: taskID[%s], code[%d],err[%+v]", r.taskID, rpc.DetectStatusCode(err), err)
		r.cancelOrReclaim(err)
		return
	}

	totalDataSize, totalShardCnt := totalDataSizeAndShardCnt(r.w.GetBenchmarkBids())
	remainDataSize, remainShardCnt := totalDataSizeAndShardCntByTasklets(tasklets)
	migratedDataSize := totalDataSize - remainDataSize
	migratedShardCnt := totalShardCnt - remainShardCnt
	r.statsMu.Lock()
	r.stats.InitTotal(totalDataSize, totalShardCnt)
	r.stats.Add(migratedDataSize, migratedShardCnt)
	r.statsMu.Unlock()
	r.statsAndReportTask(r.ctx, 0, 0)

	// all tasks are put into the task pool at one time to be executed
	span.Infof("start exec task: taskID[%s], tasklets len[%d]", r.taskID, len(tasklets))
	taskletsPool := taskpool.New(r.taskletRunConcurrency, len(tasklets))
	wg := sync.WaitGroup{}
	for i, t := range tasklets {
		tasklet := t
		_, ctx := trace.StartSpanFromContextWithTraceID(r.ctx, "execTaskletWrap", fmt.Sprintf("%s-%d", span.TraceID(), i))
		wg.Add(1)

		taskletsPool.Run(func() {
			r.execTaskletWrap(ctx, tasklet)
			wg.Done()
		})
	}
	wg.Wait()
	taskletsPool.Close()
	r.cancel()
	span.Infof("all tasklets has finished: taskID[%s]", r.taskID)

	if r.stopReason != nil {
		r.cancelOrReclaim(r.stopReason)
		return
	}

	// so far all tasklets are completed
	// check whether the task is executed correctly
	span.Infof("check task: taskID[%s]", r.taskID)
	err = r.w.Check(r.ctx)
	if err != nil {
		r.cancelOrReclaim(err)
		return
	}

	// task completed，send complete request to scheduler
	r.completeTask()
	span.Infof("task Runner finish: taskID[%s]", r.taskID)
}

func (r *TaskRunner) execTaskletWrap(ctx context.Context, t Tasklet) {
	span := trace.SpanFromContext(ctx)

	select {
	case <-r.ctx.Done():
		span.Infof("tasklet canceled: taskID[%s]", r.taskID)
	default:
		retErr := r.w.ExecTasklet(ctx, t)
		if retErr != nil {
			r.stopWithFail(retErr)
			return
		}

		r.statsAndReportTask(r.ctx, t.DataSizeByte(), uint64(len(t.bids)))
	}
}

// Stop stops task
func (r *TaskRunner) Stop() {
	r.state.setStatus(TaskStopping)
	r.stopWithFail(OtherError(errKilled))
}

func (r *TaskRunner) stopWithFail(fail *WorkError) {
	span := trace.SpanFromContext(r.ctx)
	span.Infof("stop task: taskID[%s], err_type[%d], err[%+v]", r.taskID, fail.errType, fail.err)

	r.stopMu.Lock()
	defer r.stopMu.Unlock()

	if r.stopReason == nil {
		r.stopReason = fail
	}
	r.cancel()
}

func (r *TaskRunner) cancelOrReclaim(retErr *WorkError) {
	span := trace.SpanFromContext(r.ctx)

	defer r.state.setStatus(TaskStopped)

	if ShouldReclaim(retErr) {
		taskID, taskType, src, dest := r.w.ReclaimArgs()
		args := api.ReclaimTaskArgs{
			TaskId:   taskID,
			TaskType: taskType,
			IDC:      r.idc,
			Src:      src,
			Dest:     dest,
			Reason:   retErr.Error(),
		}
		span.Infof("reclaim task: taskID[%s], err[%s]", r.taskID, retErr.String())
		err := r.schedulerCli.ReclaimTask(r.ctx, &args)
		if err != nil {
			span.Errorf("reclaim task failed: taskID[%s], taskType[%s], src[%+v], dst[%+v], code[%d], err[%+v]",
				taskID, taskType, src, dest, rpc.DetectStatusCode(err), err)
		}
		base.WorkerStatsInst().AddReclaim()
		return
	}

	span.Infof("cancel task: taskID[%s], err[%+v]", r.taskID, retErr)
	taskID, taskType, src, dest := r.w.CancelArgs()
	args := api.CancelTaskArgs{
		TaskId:   taskID,
		TaskType: taskType,
		IDC:      r.idc,
		Src:      src,
		Dest:     dest,
		Reason:   retErr.Error(),
	}
	err := r.schedulerCli.CancelTask(r.ctx, &args)
	if err != nil {
		span.Errorf("cancel failed: taskID[%s], taskType[%s], src[%+v], dest[%+v], code[%d], err[%+v]",
			taskID, taskType, src, dest, rpc.DetectStatusCode(err), err)
	}
	base.WorkerStatsInst().AddCancel()
}

func (r *TaskRunner) completeTask() {
	span := trace.SpanFromContext(r.ctx)

	defer r.state.setStatus(TaskSuccess)

	span.Infof("complete task: taskID[%s]", r.taskID)
	taskID, taskType, src, dest := r.w.CompleteArgs()
	args := api.CompleteTaskArgs{
		TaskId:   taskID,
		TaskType: taskType,
		IDC:      r.idc,
		Src:      src,
		Dest:     dest,
	}
	err := r.schedulerCli.CompleteTask(r.ctx, &args)
	if err != nil {
		span.Errorf("complete failed: taskID[%s], taskType[%s], src[%+v], dest[%+v], code[%d], err[%+v]",
			taskID, taskType, src, dest, rpc.DetectStatusCode(err), err)
	}
}

func (r *TaskRunner) statsAndReportTask(ctx context.Context, increaseDataSize, increaseShardCnt uint64) {
	span := trace.SpanFromContext(ctx)

	r.statsMu.Lock()
	r.stats.Add(increaseDataSize, increaseShardCnt)
	r.statsMu.Unlock()

	reportArgs := api.TaskReportArgs{
		TaskId:               r.taskID,
		TaskType:             r.w.TaskType(),
		TaskStats:            r.stats,
		IncreaseDataSizeByte: int(increaseDataSize),
		IncreaseShardCnt:     int(increaseShardCnt),
	}
	err := r.schedulerCli.ReportTask(ctx, &reportArgs)
	if err != nil {
		span.Errorf("report task failed: taskID[%s], code[%d], err[%+v]", r.taskID, rpc.DetectStatusCode(err), err)
	}
}

// Stopped returns true if task is stopped
func (r *TaskRunner) Stopped() bool {
	return r.state.stopped()
}

// Alive returns true if task is alive
func (r *TaskRunner) Alive() bool {
	return r.state.alive()
}

func totalDataSizeAndShardCntByTasklets(tasklets []Tasklet) (dataSize, shardCnt uint64) {
	var bidsCnt, idx int
	for _, tasklet := range tasklets {
		bidsCnt += len(tasklet.bids)
	}
	bids := make([]*ShardInfoSimple, bidsCnt)
	for _, tasklet := range tasklets {
		for _, bid := range tasklet.bids {
			bids[idx] = bid
			idx++
		}
	}
	return totalDataSizeAndShardCnt(bids)
}

func totalDataSizeAndShardCnt(bids []*ShardInfoSimple) (dataSize, shardCnt uint64) {
	for _, info := range bids {
		dataSize += uint64(info.Size)
	}
	return dataSize, uint64(len(bids))
}
