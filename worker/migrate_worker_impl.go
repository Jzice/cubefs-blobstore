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
	"sync"

	"github.com/cubefs/blobstore/common/codemode"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/trace"
	"github.com/cubefs/blobstore/util/retry"
	"github.com/cubefs/blobstore/worker/base"
	"github.com/cubefs/blobstore/worker/client"
)

// balance and disk drop task use

// ErrNotReadyForMigrate not ready for migrate
var ErrNotReadyForMigrate = errors.New("not ready for migrate")

type chunkState struct {
	retErr    error
	chunkInfo *client.ChunkInfo
}

// MigrateWorker used to manager migrate task
type MigrateWorker struct {
	t           *proto.MigrateTask
	taskType    string
	bolbNodeCli IVunitAccess

	benchmarkBids            []*ShardInfoSimple
	downloadShardConcurrency int
	forbiddenDirectDownload  bool
}

// MigrateTaskEx migrate task execution machine
type MigrateTaskEx struct {
	taskInfo *proto.MigrateTask
	taskType string

	downloadShardConcurrency int
	blobNodeCli              IVunitAccess
}

// NewMigrateWorker returns migrate worker
func NewMigrateWorker(task MigrateTaskEx) ITaskWorker {
	return &MigrateWorker{
		t:                        task.taskInfo,
		bolbNodeCli:              task.blobNodeCli,
		taskType:                 task.taskType,
		downloadShardConcurrency: task.downloadShardConcurrency,
		forbiddenDirectDownload:  task.taskInfo.ForbiddenDirectDownload,
	}
}

func (w *MigrateWorker) canDirectDownload() bool {
	return !w.forbiddenDirectDownload
}

// GenTasklets generates migrate tasklets
func (w *MigrateWorker) GenTasklets(ctx context.Context) ([]Tasklet, *WorkError) {
	span := trace.SpanFromContextSafe(ctx)
	if base.BigBufPool == nil {
		panic("BigBufPool should init before")
	}

	// balance and disk drop task need to ensure most chunks are in read-only state
	if err := retry.Timed(3, 1000).On(func() error {
		if majorityLocked(ctx, w.bolbNodeCli, w.t.Sources, w.t.CodeMode) {
			return nil
		}
		return ErrNotReadyForMigrate
	}); err != nil {
		return nil, OtherError(ErrNotReadyForMigrate)
	}

	migBids, benchmarkBids, err := GenMigrateBids(ctx, w.bolbNodeCli, w.t.Sources, w.t.Destination, w.t.CodeMode, []uint8{})
	if err != nil {
		span.Errorf("gen migrate bids failed: err[%v]", err)
		return nil, err
	}

	w.benchmarkBids = benchmarkBids
	span.Debugf("task info: taskType[%s], benchmarkBids size[%d], need migrate bids size[%d]", w.taskType, len(benchmarkBids), len(migBids))
	tasklets := BidsSplit(ctx, migBids, base.BigBufPool.GetBufSize())
	return tasklets, nil
}

// ExecTasklet execute migrate tasklet
func (w *MigrateWorker) ExecTasklet(ctx context.Context, tasklet Tasklet) *WorkError {
	replicas := w.t.Sources
	mode := w.t.CodeMode
	shardRecover := NewShardRecover(replicas, mode, tasklet.bids, base.BigBufPool, w.bolbNodeCli, w.downloadShardConcurrency)
	defer shardRecover.ReleaseBuf()

	return MigrateBids(ctx,
		shardRecover,
		w.t.SourceVuid.Index(),
		w.t.Destination,
		w.canDirectDownload(),
		tasklet.bids,
		w.bolbNodeCli)
}

// Check checks migrate task execute result
func (w *MigrateWorker) Check(ctx context.Context) *WorkError {
	return CheckVunit(ctx, w.benchmarkBids, w.t.Destination, w.bolbNodeCli)
}

// GetBenchmarkBids returns benchmark bids
func (w *MigrateWorker) GetBenchmarkBids() []*ShardInfoSimple {
	return w.benchmarkBids
}

// CancelArgs returns cancel args
func (w *MigrateWorker) CancelArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation) {
	return w.t.TaskID, w.taskType, w.t.Sources, w.t.Destination
}

// CompleteArgs returns complete args
func (w *MigrateWorker) CompleteArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation) {
	return w.t.TaskID, w.taskType, w.t.Sources, w.t.Destination
}

// ReclaimArgs returns reclaim args
func (w *MigrateWorker) ReclaimArgs() (taskID, taskType string, src []proto.VunitLocation, dest proto.VunitLocation) {
	return w.t.TaskID, w.taskType, w.t.Sources, w.t.Destination
}

// TaskType returns task type
func (w *MigrateWorker) TaskType() (taskType string) {
	return w.taskType
}

//----------------------------------------------------------
func majorityLocked(ctx context.Context, vunitAccess IVunitAccess, replicas []proto.VunitLocation, mode codemode.CodeMode) (success bool) {
	chunksStat := getChunksStat(ctx, vunitAccess, replicas)

	lockedCnt := 0
	for _, chunkStat := range chunksStat {
		if chunkStat.retErr == nil && chunkStat.chunkInfo != nil && chunkStat.chunkInfo.Locked() {
			lockedCnt++
		}
	}

	return lockedCnt >= minLockedMajorityNum(mode)
}

func getChunksStat(ctx context.Context, vunitAccess IVunitAccess, replicas []proto.VunitLocation) map[proto.Vuid]*chunkState {
	results := make(map[proto.Vuid]*chunkState, len(replicas))
	wg := sync.WaitGroup{}
	var mu sync.Mutex
	for idx := range replicas {
		replica := replicas[idx]
		vuid := replica.Vuid
		wg.Add(1)
		go func() {
			defer wg.Done()
			chunkInfo, err := vunitAccess.StatChunk(ctx, replica)
			mu.Lock()
			defer mu.Unlock()
			results[vuid] = &chunkState{retErr: err, chunkInfo: chunkInfo}
		}()
	}
	wg.Wait()
	return results
}

func minLockedMajorityNum(mode codemode.CodeMode) int {
	modeInfo := mode.Tactic()
	// ensure that the corresponding volume fails to be written
	return modeInfo.M + modeInfo.L + 1
}
