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

package tinker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Shopify/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/singleflight"

	"github.com/cubefs/blobstore/common/counter"
	comErr "github.com/cubefs/blobstore/common/errors"
	"github.com/cubefs/blobstore/common/kafka"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/rpc"
	"github.com/cubefs/blobstore/common/taskswitch"
	"github.com/cubefs/blobstore/common/trace"
	"github.com/cubefs/blobstore/tinker/base"
	"github.com/cubefs/blobstore/tinker/client"
	"github.com/cubefs/blobstore/tinker/db"
	"github.com/cubefs/blobstore/util/selector"
	"github.com/cubefs/blobstore/util/taskpool"
)

type shardRepairStatus int

// shard repair status
const (
	ShardRepairDone = shardRepairStatus(iota)
	ShardRepairFailed
	ShardRepairUnexpect
	ShardRepairOrphan
)

// shard repair name
const (
	ShardRepair = "shard_repair"
)

// ErrWorkerServiceUnavailable worker service unavailable
var ErrWorkerServiceUnavailable = errors.New("worker service unavailable")

// ShardRepairConfig shard repair config
type ShardRepairConfig struct {
	ClusterID proto.ClusterID
	Idc       string

	TaskPoolSize int      `json:"task_pool_size"`
	BrokerList   []string `json:"broker_list"`

	PriorityTopics       []base.PriorityConsumerConfig `json:"priority_topics"`
	NormalHandleBatchCnt int                           `json:"normal_handle_batch_cnt"`

	FailTopic                base.KafkaConfig  `json:"fail_topic"`
	FailHandleBatchCnt       int               `json:"fail_handle_batch_cnt"`
	FailMsgConsumeIntervalMs int64             `json:"fail_msg_consume_interval_ms"`
	FailMsgSender            kafka.ProducerCfg `json:"fail_msg_sender"`
}

// ShardRepairMgr shard repair manager
type ShardRepairMgr struct {
	taskPool   taskpool.TaskPool
	taskSwitch *taskswitch.TaskSwitch
	volCache   base.IVolumeCache

	normalPriorConsumers base.IConsumer

	failTopicConsumers       []base.IConsumer
	failMsgConsumeIntervalMs time.Duration
	failMsgSender            base.IProducer

	normalHandleBatchCnt int
	failHandlerBatchCnt  int

	workerCli      client.IWorker
	workerSelector selector.Selector

	orphanedShardTable db.IOrphanedShardTbl

	repairSuccessCounter    prometheus.Counter
	repairSuccessCounterMin counter.CounterByMin
	repairFailedCounter     prometheus.Counter
	repairFailedCounterMin  counter.CounterByMin
	errStatsDistribution    *base.ErrorStats

	group singleflight.Group
}

// NewShardRepairMgr returns shard repair manager
func NewShardRepairMgr(
	cfg *ShardRepairConfig,
	vc base.IVolumeCache,
	switchMgr *taskswitch.SwitchMgr,
	offAccessor base.IOffsetAccessor,
	schedulerCli client.IScheduler,
	orphanedShardTbl db.IOrphanedShardTbl,
	workerCli client.IWorker,
) (*ShardRepairMgr, error) {
	priorConsumers, err := base.NewPriorityConsumer(cfg.PriorityTopics, offAccessor)
	if err != nil {
		return nil, err
	}

	failTopicConsumers, err := base.NewKafkaPartitionConsumers(&cfg.FailTopic, offAccessor)
	if err != nil {
		return nil, err
	}

	taskSwitch, err := switchMgr.AddSwitch(taskswitch.ShardRepairSwitchName)
	if err != nil {
		return nil, err
	}

	workerSelector, err := selector.NewSelector(10*1000, func() (hosts []string, err error) {
		hosts, err = schedulerCli.ListService(context.Background(), cfg.ClusterID, cfg.Idc)
		if err != nil {
			return
		}
		if len(hosts) == 0 {
			return nil, ErrWorkerServiceUnavailable
		}
		return
	})
	if err != nil {
		return nil, err
	}

	failMsgSender, err := base.NewMsgSenderEx(cfg.FailTopic.Topic, &cfg.FailMsgSender)
	if err != nil {
		return nil, err
	}

	return &ShardRepairMgr{
		workerCli:      workerCli,
		taskPool:       taskpool.New(cfg.TaskPoolSize, cfg.TaskPoolSize),
		taskSwitch:     taskSwitch,
		volCache:       vc,
		workerSelector: workerSelector,

		normalPriorConsumers: priorConsumers,

		failTopicConsumers:       failTopicConsumers,
		failMsgSender:            failMsgSender,
		failMsgConsumeIntervalMs: time.Duration(cfg.FailMsgConsumeIntervalMs) * time.Millisecond,

		normalHandleBatchCnt: cfg.NormalHandleBatchCnt,
		failHandlerBatchCnt:  cfg.FailHandleBatchCnt,

		orphanedShardTable: orphanedShardTbl,

		repairSuccessCounter: base.NewCounter(cfg.ClusterID, ShardRepair, base.KindSuccess),
		repairFailedCounter:  base.NewCounter(cfg.ClusterID, ShardRepair, base.KindFailed),
		errStatsDistribution: base.NewErrorStats(),
	}, nil
}

// Enabled returns true if shard repair task is enabled, otherwise returns false
func (s *ShardRepairMgr) Enabled() bool {
	return s.taskSwitch.Enabled()
}

// RunTask run shard repair task
func (s *ShardRepairMgr) RunTask() {
	go func() {
		for {
			s.taskSwitch.WaitEnable()
			s.consumerAndRepair(s.normalPriorConsumers, s.normalHandleBatchCnt)
		}
	}()

	failPtConsumeBatchCnt := s.failHandlerBatchCnt / len(s.failTopicConsumers)
	for _, c := range s.failTopicConsumers {
		c := c
		go func() {
			for {
				s.taskSwitch.WaitEnable()
				s.consumerAndRepair(c, failPtConsumeBatchCnt)
				time.Sleep(s.failMsgConsumeIntervalMs)
			}
		}()
	}
}

type shardRepairRet struct {
	status    shardRepairStatus
	err       error
	repairMsg *proto.ShardRepairMsg
}

func (s *ShardRepairMgr) consumerAndRepair(consumer base.IConsumer, batchCnt int) {
	span, ctx := trace.StartSpanFromContext(context.Background(), "consumerAndRepair")
	defer span.Finish()

	if batchCnt <= 0 {
		batchCnt = 1
	}
	msgs := consumer.ConsumeMessages(ctx, batchCnt)

	s.handleMsgBatch(ctx, msgs)

	base.LoopExecUntilSuccess(ctx, "repair consumer.CommitOffset", func() error {
		return consumer.CommitOffset(ctx)
	})
}

func (s *ShardRepairMgr) handleMsgBatch(ctx context.Context, msgs []*sarama.ConsumerMessage) {
	span := trace.SpanFromContextSafe(ctx)
	ctx = trace.ContextWithSpan(ctx, span)

	span.Infof("handle repair msg: len[%d]", len(msgs))

	finishCh := make(chan shardRepairRet, len(msgs))
	for _, m := range msgs {
		func(msg *sarama.ConsumerMessage) {
			s.taskPool.Run(func() {
				s.handleOneMsg(ctx, msg, finishCh)
			})
		}(m)
	}

	for i := 0; i < len(msgs); i++ {
		ret := <-finishCh
		switch ret.status {
		case ShardRepairDone:
			span.Debugf("repair success: vid[%d], bid[%d], reqid[%s]", ret.repairMsg.Vid, ret.repairMsg.Bid, ret.repairMsg.ReqId)
			s.repairSuccessCounter.Inc()
			s.repairSuccessCounterMin.Add()

		case ShardRepairFailed:
			span.Warnf("repair failed and send msg to fail queue: vid[%d], bid[%d], reqid[%s], retry[%d], err[%+v]",
				ret.repairMsg.Vid, ret.repairMsg.Bid, ret.repairMsg.ReqId, ret.repairMsg.Retry, ret.err)
			s.repairFailedCounter.Inc()
			s.repairFailedCounterMin.Add()
			s.errStatsDistribution.AddFail(ret.err)

			base.LoopExecUntilSuccess(ctx, "repair msg send to fail queue", func() error {
				return s.send2FailQueue(ctx, *ret.repairMsg)
			})
		case ShardRepairUnexpect, ShardRepairOrphan:
			s.repairFailedCounter.Inc()
			s.repairFailedCounterMin.Add()
			s.errStatsDistribution.AddFail(ret.err)
			span.Warnf("unexpected result: msg[%+v], err[%+v]", ret.repairMsg, ret.err)
		}
	}
}

func (s *ShardRepairMgr) handleOneMsg(ctx context.Context, msg *sarama.ConsumerMessage, finishCh chan<- shardRepairRet) {
	var repairMsg proto.ShardRepairMsg
	err := json.Unmarshal(msg.Value, &repairMsg)
	if err != nil {
		finishCh <- shardRepairRet{
			status:    ShardRepairUnexpect,
			err:       err,
			repairMsg: nil,
		}
		return
	}

	if !repairMsg.IsValid() {
		finishCh <- shardRepairRet{
			status:    ShardRepairUnexpect,
			err:       proto.ErrInvalidMsg,
			repairMsg: nil,
		}
		return
	}
	pSpan := trace.SpanFromContextSafe(ctx)
	pSpan.Debugf("handle one repair msg: msg[%+v]", repairMsg)
	_, tmpCtx := trace.StartSpanFromContextWithTraceID(context.Background(), "handleRepairMsg", repairMsg.ReqId)
	jobKey := fmt.Sprintf("%d:%d:%s", repairMsg.Vid, repairMsg.Bid, repairMsg.BadIdx)
	_, err, _ = s.group.Do(jobKey, func() (ret interface{}, e error) {
		e = s.repairWithCheckVolConsistency(tmpCtx, repairMsg)
		return
	})

	if isOrphanShard(err) {
		finishCh <- shardRepairRet{
			status:    ShardRepairOrphan,
			err:       err,
			repairMsg: &repairMsg,
		}
		return
	}

	if err != nil {
		finishCh <- shardRepairRet{
			status:    ShardRepairFailed,
			err:       err,
			repairMsg: &repairMsg,
		}
		return
	}

	finishCh <- shardRepairRet{
		status:    ShardRepairDone,
		repairMsg: &repairMsg,
	}
}

func (s *ShardRepairMgr) repairWithCheckVolConsistency(
	ctx context.Context,
	repairMsg proto.ShardRepairMsg) error {
	return DoWithCheckVolConsistency(ctx, s.volCache, repairMsg.Vid, func(info *client.VolInfo) error {
		return s.tryRepair(ctx, info, repairMsg)
	})
}

func (s *ShardRepairMgr) tryRepair(
	ctx context.Context,
	volInfo *client.VolInfo,
	repairMsg proto.ShardRepairMsg) error {
	span := trace.SpanFromContextSafe(ctx)

	err := s.repairShard(ctx, volInfo, repairMsg)
	if err == nil {
		return nil
	}

	if !s.shouldUpdateVol(err) {
		return err
	}

	newVol, err1 := s.volCache.Update(volInfo.Vid)
	if err1 != nil || newVol.EqualWith(*volInfo) {
		// if update volInfo failed or volInfo not updated, don't need retry
		span.Warnf("new volInfo is same or volCache.Update failed: vid[%d], vol cache update err[%+v], repair err[%+v]",
			volInfo.Vid, err1, err)
		return err
	}

	if newVol.EqualWith(*volInfo) {
		span.Errorf("volInfo not updated: volInfo[%+v], newVolInfo[%+v]", volInfo, newVol)
	}
	return s.repairShard(ctx, newVol, repairMsg)
}

func (s *ShardRepairMgr) shouldUpdateVol(err error) bool {
	if errCode := rpc.DetectStatusCode(err); errCode == comErr.CodeDestReplicaBad {
		return true
	}
	return false
}

func (s *ShardRepairMgr) repairShard(ctx context.Context, volInfo *client.VolInfo, repairMsg proto.ShardRepairMsg) (err error) {
	span := trace.SpanFromContextSafe(ctx)

	span.Infof("repair shard: msg[%+v], vol info[%+v]", repairMsg, volInfo)

	hosts := s.workerSelector.GetRandomN(1)
	if len(hosts) == 0 {
		return ErrWorkerServiceUnavailable
	}
	workerHost := hosts[0]

	task := proto.ShardRepairTask{
		Bid:      repairMsg.Bid,
		CodeMode: volInfo.CodeMode,
		Sources:  volInfo.VunitLocations,
		BadIdxs:  repairMsg.BadIdx,
		Reason:   repairMsg.Reason,
	}

	err = s.workerCli.RepairShard(ctx, workerHost, task)
	if err == nil {
		return nil
	}

	if isOrphanShard(err) {
		s.saveOrphanedShard(ctx, repairMsg)
	}

	return err
}

func (s *ShardRepairMgr) saveOrphanedShard(ctx context.Context, repairMsg proto.ShardRepairMsg) {
	span := trace.SpanFromContextSafe(ctx)

	shardInfo := db.ShardInfo{
		ClusterID: repairMsg.ClusterID,
		Vid:       repairMsg.Vid,
		Bid:       repairMsg.Bid,
	}
	span.Infof("save orphaned shard: info[%+v]", shardInfo)

	base.LoopExecUntilSuccess(ctx, "save orphaned shard", func() error {
		return s.orphanedShardTable.SaveOrphanedShard(ctx, shardInfo)
	})
}

func (s *ShardRepairMgr) send2FailQueue(ctx context.Context, msg proto.ShardRepairMsg) error {
	span := trace.SpanFromContextSafe(ctx)

	msg.Retry++
	b, err := json.Marshal(msg)
	if err != nil {
		// just panic if marsh fail
		span.Panicf("send to fail queue msg json.Marshal failed: msg[%+v], err[%+v]", msg, err)
	}

	err = s.failMsgSender.SendMessage(b)
	if err != nil {
		return fmt.Errorf("send message: err[%w]", err)
	}

	return nil
}

func isOrphanShard(err error) bool {
	return rpc.DetectStatusCode(err) == comErr.CodeOrphanShard
}

// GetTaskStats returns task stats
func (s *ShardRepairMgr) GetTaskStats() (success [counter.SLOT]int, failed [counter.SLOT]int) {
	return s.repairSuccessCounterMin.Show(), s.repairFailedCounterMin.Show()
}

// GetErrorStats returns service error stats
func (s *ShardRepairMgr) GetErrorStats() (errStats []string, totalErrCnt uint64) {
	statsResult, totalErrCnt := s.errStatsDistribution.Stats()
	return base.FormatPrint(statsResult), totalErrCnt
}
