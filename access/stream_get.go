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

package access

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/afex/hystrix-go/hystrix"

	"github.com/cubefs/blobstore/access/controller"
	"github.com/cubefs/blobstore/api/access"
	"github.com/cubefs/blobstore/api/blobnode"
	"github.com/cubefs/blobstore/common/codemode"
	"github.com/cubefs/blobstore/common/ec"
	errcode "github.com/cubefs/blobstore/common/errors"
	"github.com/cubefs/blobstore/common/proto"
	"github.com/cubefs/blobstore/common/rpc"
	"github.com/cubefs/blobstore/common/trace"
	"github.com/cubefs/blobstore/util/errors"
	"github.com/cubefs/blobstore/util/retry"
)

var (
	errNeedReconstructRead = errors.New("need to reconstruct read")
	errCanceledReadShard   = errors.New("canceled read shard")
)

type blobGetArgs struct {
	Vid      proto.Vid
	Bid      proto.BlobID
	BlobSize uint64
	Offset   uint64
	ReadSize uint64
}

type shardData struct {
	index  int
	status bool
	buffer []byte
}

type sortedVuid struct {
	index  int
	vuid   proto.Vuid
	diskID proto.DiskID
	host   string
}

type pipeBuffer struct {
	err    error
	blob   blobGetArgs
	shards [][]byte
}

// Get read file
//     required: location, readSize
//     optional: offset(default is 0)
//
//     first return value is data transfer to copy data after argument checking
//
//  Read data shards firstly, if blob size is small or read few bytes
//  then ec reconstruct-read, try to reconstruct from N+X to N+M
//
//  sorted N+X is, such as we use mode EC6P10L2, X=2 and Read from idc=2
//  shards like this
//              data N 6        |    parity M 10     | local L 2
//        d1  d2  d3  d4  d5  d6  p1 .. p5  p6 .. p10  l1  l2
//   idc   1   1   1   2   2   2     1         2        1   2
//
//sorted  d4  d5  d6  p6 .. p10  d1  d2  d3  p1 .. p5
//read-1 [d4                p10]
//read-2 [d4                p10  d1]
//read-3 [d4                p10  d1  d2]
//...
//read-9 [d4                                       p5]
//failed
func (h *Handler) Get(ctx context.Context, w io.Writer, location access.Location, readSize, offset uint64) (func() error, error) {
	span := trace.SpanFromContextSafe(ctx)
	span.Debugf("get request cluster:%d size:%d offset:%d", location.ClusterID, readSize, offset)

	blobs, err := genLocationBlobs(&location, readSize, offset)
	if err != nil {
		span.Info("illegal argument", err)
		return func() error { return nil }, errcode.ErrIllegalArguments
	}
	if len(blobs) == 0 {
		return func() error { return nil }, nil
	}

	clusterID := location.ClusterID
	var serviceController controller.ServiceController
	if err = retry.Timed(3, 200).On(func() error {
		sc, err := h.clusterController.GetServiceController(clusterID)
		if err != nil {
			return err
		}
		serviceController = sc
		return nil
	}); err != nil {
		span.Error("get service", errors.Detail(err))
		return func() error { return nil }, err
	}

	return func() error {
		getTime := new(times)
		defer func() {
			span.AppendRPCTrackLog(getTime.GetLogs())
		}()

		// try to read data shard only,
		//   if blobsize is small: all data is in the first shard, cos shards aligned by MinShardSize.
		//   read few bytes: read bytes less than quarter of blobsize, like Range:[0-1].
		if len(blobs) == 1 {
			blob := blobs[0]
			sizes, _ := ec.GetBufferSizes(int(blob.BlobSize), location.CodeMode.Tactic())
			if int(blob.BlobSize) <= sizes.ShardSize || blob.ReadSize < blob.BlobSize/4 {
				span.Debugf("read data shard only readsize:%d blobsize:%d shardsize:%d",
					blob.ReadSize, blob.BlobSize, sizes.ShardSize)

				err := h.getDataShardOnly(ctx, getTime, w, serviceController, clusterID, blob)
				if err != errNeedReconstructRead {
					if err != nil {
						span.Error("read data shard only", err)
					}
					return err
				}
				span.Info("read data shard only failed", err)
			}
		}

		// data stream flow:
		// client <--copy-- pipeline <--swap-- readBlob <--copy-- blobnode
		//
		// Alloc N+M shard buffers here, and release after written to client.
		// Replace not-empty buffers in readBlob, need release old-buffers in that function.
		closeCh := make(chan struct{})
		pipeline := func() <-chan pipeBuffer {
			ch := make(chan pipeBuffer, 1)
			go func() {
				defer close(ch)

				var (
					blobVolume  *controller.VolumePhy
					sortedVuids []sortedVuid
				)
				for _, blob := range blobs {
					var err error
					if blobVolume == nil || blobVolume.Vid != blob.Vid {
						blobVolume, err = h.getVolume(ctx, clusterID, blob.Vid, true)
						if err != nil {
							span.Error("get volume", err)
							ch <- pipeBuffer{err: err}
							return
						}

						tactic := blobVolume.CodeMode.Tactic()
						// do not use local shards
						sortedVuids = genSortedVuidByIDC(ctx, serviceController, h.IDC, blobVolume.Units[:tactic.N+tactic.M])
						span.Debugf("to read blob(%d %d %d) with read-shard-x:%d active-shard-n:%d of data-n:%d party-n:%d",
							clusterID, blob.Vid, blob.Bid, h.MinReadShardsX, len(sortedVuids), tactic.N, tactic.M)
						if len(sortedVuids) < tactic.N {
							err = fmt.Errorf("broken blob(%d %d %d)", clusterID, blob.Vid, blob.Bid)
							span.Error(err)
							ch <- pipeBuffer{err: err}
							return
						}
					}

					codeMode := blobVolume.CodeMode
					tactic := codeMode.Tactic()
					sizes, _ := ec.GetBufferSizes(int(blob.BlobSize), tactic)
					shardSize := sizes.ShardSize

					shards := make([][]byte, tactic.N+tactic.M)
					for ii := range shards {
						buf, _ := h.memPool.Alloc(shardSize)
						shards[ii] = buf
					}

					err = h.readOneBlob(ctx, getTime, serviceController, clusterID,
						blobVolume.Vid, codeMode, blob, sortedVuids, shards)
					if err != nil {
						span.Error("read one blob", blob.Bid, err)
						for _, buf := range shards {
							h.memPool.Put(buf)
						}
						ch <- pipeBuffer{err: err}
						return
					}

					select {
					case <-closeCh:
						return
					case ch <- pipeBuffer{blob: blob, shards: shards}:
					}
				}
			}()

			return ch
		}()

		var err error
		for line := range pipeline {
			if line.err != nil {
				return line.err
			}

			startWrite := time.Now()

			idx := 0
			off := line.blob.Offset
			toReadSize := line.blob.ReadSize
			for toReadSize > 0 {
				buf := line.shards[idx]
				l := uint64(len(buf))
				if off >= l {
					idx++
					off -= l
					continue
				}

				toRead := minU64(toReadSize, l-off)
				if _, e := w.Write(buf[off : off+toRead]); e != nil {
					err = errors.Info(e, "write to response")
					break
				}
				idx++
				off = 0
				toReadSize -= toRead
			}

			getTime.AddGetWrite(startWrite)

			for _, buf := range line.shards {
				h.memPool.Put(buf)
			}
			if err != nil {
				span.Error("get request error", err)
				close(closeCh)
				return err
			}
		}

		return nil
	}, nil
}

// 1. try to min-read shards bytes
// 2. if failed try to read next shard to reconstruct
// 3. write the the right offset bytes to writer
func (h *Handler) readOneBlob(ctx context.Context, getTime *times,
	serviceController controller.ServiceController,
	clusterID proto.ClusterID, vid proto.Vid, codeMode codemode.CodeMode,
	blob blobGetArgs, sortedVuids []sortedVuid, shards [][]byte) error {
	span := trace.SpanFromContextSafe(ctx)

	tactic := codeMode.Tactic()
	sizes, err := ec.GetBufferSizes(int(blob.BlobSize), tactic)
	if err != nil {
		return err
	}
	empties := emptyDataShardIndexes(sizes)

	dataN, dataParityN := tactic.N, tactic.N+tactic.M
	minShardsRead := dataN + h.MinReadShardsX
	if minShardsRead > len(sortedVuids) {
		minShardsRead = len(sortedVuids)
	}
	shardSize := len(shards[0])

	stopChan := make(chan struct{})
	nextChan := make(chan struct{}, len(sortedVuids))

	shardPipe := func() <-chan shardData {
		ch := make(chan shardData)
		go func() {
			wg := new(sync.WaitGroup)
			defer func() {
				wg.Wait()
				close(ch)
			}()

			for _, vuid := range sortedVuids[:minShardsRead] {
				if _, ok := empties[vuid.index]; !ok {
					wg.Add(1)
					go func(vuid sortedVuid) {
						ch <- h.readOneShard(ctx, serviceController, clusterID, vid,
							shardSize, blob, vuid, stopChan)
						wg.Done()
					}(vuid)
				}
			}

			for _, vuid := range sortedVuids[minShardsRead:] {
				if _, ok := empties[vuid.index]; ok {
					continue
				}

				select {
				case <-stopChan:
					return
				case <-nextChan:
				}

				wg.Add(1)
				go func(vuid sortedVuid) {
					ch <- h.readOneShard(ctx, serviceController, clusterID, vid,
						shardSize, blob, vuid, stopChan)
					wg.Done()
				}(vuid)
			}
		}()

		return ch
	}()

	received := make(map[int]bool, minShardsRead)
	for idx := range empties {
		received[idx] = true
		h.memPool.Zero(shards[idx])
	}

	startRead := time.Now()
	getTime.AddGetN(int(blob.ReadSize))
	reconstructed := false
	for shard := range shardPipe {
		// swap shard buffer
		if shard.status {
			buf := shards[shard.index]
			shards[shard.index] = shard.buffer
			h.memPool.Put(buf)
		}

		received[shard.index] = shard.status
		if len(received) < dataN {
			continue
		}

		// bad data index
		badIdx := make([]int, 0, 8)
		for i := 0; i < dataN; i++ {
			if succ, ok := received[i]; !ok || !succ {
				badIdx = append(badIdx, i)
			}
		}
		if len(badIdx) == 0 {
			reconstructed = true
			close(stopChan)
			break
		}

		// update bad parity index
		for i := dataN; i < dataParityN; i++ {
			if succ, ok := received[i]; !ok || !succ {
				badIdx = append(badIdx, i)
			}
		}

		badShards := 0
		for _, succ := range received {
			if !succ {
				badShards++
			}
		}
		// it will not wait all the shards, cos has no enough shards to reconstruct
		if badShards > dataParityN-dataN {
			span.Infof("bid(%d) bad(%d) has no enough to reconstruct", blob.Bid, badShards)
			close(stopChan)
			break
		}

		// has bad shards, but have enough shards to reconstruct
		if len(received) >= dataN+badShards {
			span.Debugf("bid(%d) ready to ec reconstruct data", blob.Bid)
			err := h.encoder[codeMode].ReconstructData(shards, badIdx)
			if err == nil {
				reconstructed = true
				close(stopChan)
				break
			}
			span.Infof("bid(%d) ec reconstruct data error:%s", blob.Bid, err.Error())
		}

		if len(received) >= len(sortedVuids) {
			close(stopChan)
			break
		}
		nextChan <- struct{}{}
	}
	getTime.AddGetRead(startRead)

	// release buffer of delayed shards
	go func() {
		for shard := range shardPipe {
			if shard.status {
				h.memPool.Put(shard.buffer)
			}
		}
	}()

	if reconstructed {
		return nil
	}
	return fmt.Errorf("broken blob(%d %d %d)", clusterID, blob.Vid, blob.Bid)
}

func (h *Handler) readOneShard(ctx context.Context, serviceController controller.ServiceController,
	clusterID proto.ClusterID, vid proto.Vid, shardSize int,
	blob blobGetArgs, vuid sortedVuid, stopChan <-chan struct{}) shardData {
	span := trace.SpanFromContextSafe(ctx)
	shardResult := shardData{
		index:  vuid.index,
		status: false,
	}

	args := blobnode.RangeGetShardArgs{
		GetShardArgs: blobnode.GetShardArgs{
			DiskID: vuid.diskID,
			Vuid:   vuid.vuid,
			Bid:    blob.Bid,
		},
		Offset: 0,
		Size:   int64(shardSize),
	}

	var (
		err  error
		body io.ReadCloser
	)
	if err = hystrix.Do(rwCommand, func() error {
		body, err = h.getOneShardFromHost(ctx, serviceController, vuid.host, vuid.diskID, args,
			vuid.index, clusterID, vid, stopChan)
		return err
	}, nil); err != nil {
		if err == errCanceledReadShard {
			span.Debugf("read blob(%d %d %d) on blobnode(%d %d %s) ecidx(%d) canceled",
				clusterID, vid, blob.Bid, vuid.vuid, vuid.diskID, vuid.host, vuid.index)
			return shardResult
		}
		span.Warnf("read blob(%d %d %d) on blobnode(%d %d %s) ecidx(%d): %s",
			clusterID, vid, blob.Bid,
			vuid.vuid, vuid.diskID, vuid.host, vuid.index, errors.Detail(err))
		return shardResult
	}
	defer body.Close()

	buf, err := h.memPool.Alloc(shardSize)
	if err != nil {
		span.Warn(err)
		return shardResult
	}

	_, err = io.ReadFull(body, buf)
	if err != nil {
		h.memPool.Put(buf)
		span.Warnf("read blob(%d %d %d) on blobnode(%d %d %s) ecidx(%d): %s",
			clusterID, vid, blob.Bid,
			vuid.vuid, vuid.diskID, vuid.host, vuid.index, err.Error())
		return shardResult
	}

	shardResult.status = true
	shardResult.buffer = buf
	return shardResult
}

func (h *Handler) getDataShardOnly(ctx context.Context, getTime *times,
	w io.Writer, serviceController controller.ServiceController,
	clusterID proto.ClusterID, blob blobGetArgs) error {
	span := trace.SpanFromContextSafe(ctx)
	if blob.ReadSize == 0 {
		return nil
	}

	blobVolume, err := h.getVolume(ctx, clusterID, blob.Vid, true)
	if err != nil {
		return err
	}
	tactic := blobVolume.CodeMode.Tactic()

	from, to := int(blob.Offset), int(blob.Offset+blob.ReadSize)
	buffer, err := ec.NewRangeBuffer(int(blob.BlobSize), from, to, tactic, h.memPool)
	if err != nil {
		return err
	}
	defer buffer.Release()

	shardSize := buffer.ShardSize
	firstShardIdx := int(blob.Offset) / shardSize
	shardOffset := int(blob.Offset) % shardSize

	startRead := time.Now()
	getTime.AddGetN(int(blob.ReadSize))

	remainSize := blob.ReadSize
	bufOffset := 0
	for i, shard := range blobVolume.Units[firstShardIdx:tactic.N] {
		if remainSize <= 0 {
			break
		}

		toReadSize := minU64(remainSize, uint64(shardSize-shardOffset))
		args := blobnode.RangeGetShardArgs{
			GetShardArgs: blobnode.GetShardArgs{
				DiskID: shard.DiskID,
				Vuid:   shard.Vuid,
				Bid:    blob.Bid,
			},
			Offset: int64(shardOffset),
			Size:   int64(toReadSize),
		}

		body, err := h.getOneShardFromHost(ctx, serviceController, shard.Host, shard.DiskID, args,
			firstShardIdx+i, clusterID, blob.Vid, nil)
		if err != nil {
			span.Warnf("read blob(%d %d %d) on blobnode(%d %d %s) ecidx(%d): %s",
				clusterID, blob.Vid, blob.Bid,
				shard.Vuid, shard.DiskID, shard.Host, firstShardIdx+i, errors.Detail(err))
			return errNeedReconstructRead
		}
		defer body.Close()

		buf := buffer.DataBuf[bufOffset : bufOffset+int(toReadSize)]
		_, err = io.ReadFull(body, buf)
		if err != nil {
			span.Warn(err)
			return errNeedReconstructRead
		}

		// reset next shard offset
		shardOffset = 0
		remainSize -= toReadSize
		bufOffset += int(toReadSize)
	}
	getTime.AddGetRead(startRead)

	if remainSize > 0 {
		return fmt.Errorf("no enough data to read %d", remainSize)
	}

	startWrite := time.Now()
	if _, err := w.Write(buffer.DataBuf[:int(blob.ReadSize)]); err != nil {
		getTime.AddGetWrite(startWrite)
		return errors.Info(err, "write to response")
	}
	getTime.AddGetWrite(startWrite)

	return nil
}

// getOneShardFromHost get body of one shard
func (h *Handler) getOneShardFromHost(ctx context.Context, serviceController controller.ServiceController,
	host string, diskID proto.DiskID, args blobnode.RangeGetShardArgs, // get shard param with host diskid
	index int, clusterID proto.ClusterID, vid proto.Vid, // param to update volume cache
	cancelChan <-chan struct{}, // do not retry again if cancelChan was closed
) (io.ReadCloser, error) {
	span := trace.SpanFromContextSafe(ctx)

	var (
		rbody io.ReadCloser
		rerr  error
	)
	rerr = retry.ExponentialBackoff(3, 200).RuptOn(func() (bool, error) {
		if cancelChan != nil {
			select {
			case <-cancelChan:
				return true, errCanceledReadShard
			default:
			}
		}

		// new child span to get from blobnode, we should finish it here.
		spanChild, ctxChild := trace.StartSpanFromContextWithTraceID(
			context.Background(), "GetFromBlobnode", span.TraceID())
		defer spanChild.Finish()

		body, _, err := h.blobnodeClient.RangeGetShard(ctxChild, host, &args)
		if err == nil {
			rbody = body
			return true, nil
		}

		code := rpc.DetectStatusCode(err)
		switch code {
		case errcode.CodeOverload:
			return true, err

		// EIO and Readonly error, then we need to punish disk in local and no need to retry
		case errcode.CodeDiskBroken, errcode.CodeVUIDReadonly:
			h.punishDisk(ctx, clusterID, diskID, host, "BrokenOrRO")
			span.Infof("punish disk:%d on:%s cos:blobnode/%d", diskID, host, code)
			return true, fmt.Errorf("punished disk (%d %s)", diskID, host)

		// vuid not found means the reflection between vuid and diskID has change,
		// should refresh the blob volume cache
		case errcode.CodeDiskNotFound, errcode.CodeVuidNotFound:
			span.Infof("volume info outdated disk %d on host %s", diskID, host)

			latestVolume, e := h.getVolume(ctx, clusterID, vid, false)
			if e != nil {
				span.Warnf("update volume info with no cache %d %d err: %s", clusterID, vid, e)
				return false, err
			}
			newUnit := latestVolume.Units[index]

			newDiskID := newUnit.DiskID
			if newDiskID != diskID {
				hi, e := serviceController.GetDiskHost(ctx, newDiskID)
				if e == nil && !hi.Punished {
					span.Infof("update disk %d %d %d -> %d", clusterID, vid, diskID, newDiskID)

					host = hi.Host
					diskID = newDiskID
					args.GetShardArgs.DiskID = diskID
					args.GetShardArgs.Vuid = newUnit.Vuid
					return false, err
				}
			}

			h.punishDiskWith(ctx, clusterID, diskID, host, "NotFound")
			span.Debugf("punish threshold disk:%d cos:blobnode/%d", diskID, code)
		}

		// do not retry on timeout then punish threshold this disk
		if errorTimeout(err) {
			h.punishDiskWith(ctx, clusterID, diskID, host, "Timeout")
			return true, err
		}
		span.Debugf("read from disk:%d blobnode/%s", diskID, err.Error())

		err = errors.Base(err, fmt.Sprintf("get shard on (%d %s)", diskID, host))
		return false, err
	})

	return rbody, rerr
}

func genLocationBlobs(location *access.Location, readSize uint64, offset uint64) ([]blobGetArgs, error) {
	if offset+readSize > location.Size {
		return nil, fmt.Errorf("FileSize:%d ReadSize:%d Offset:%d", location.Size, readSize, offset)
	}

	blobSize := uint64(location.BlobSize)
	if blobSize <= 0 {
		return nil, fmt.Errorf("BlobSize:%d", blobSize)
	}

	remainSize := readSize
	firstBlobIdx := offset / blobSize
	blobOffset := offset % blobSize

	idx := uint64(0)
	blobs := make([]blobGetArgs, 0, 1+(readSize+blobOffset)/blobSize)
	for _, blob := range location.Blobs {
		currBlobID := blob.MinBid

		for ii := uint32(0); ii < blob.Count; ii++ {
			if remainSize <= 0 {
				return blobs, nil
			}

			if idx >= firstBlobIdx {
				toReadSize := minU64(remainSize, blobSize-blobOffset)
				if toReadSize > 0 {
					blobs = append(blobs, blobGetArgs{
						Vid:      blob.Vid,
						Bid:      currBlobID,
						BlobSize: minU64(location.Size-idx*blobSize, blobSize), // update the last blob size
						Offset:   blobOffset,
						ReadSize: toReadSize,
					})
				}

				// reset next blob offset
				blobOffset = 0
				remainSize -= toReadSize
			}

			currBlobID++
			idx++
		}
	}

	if remainSize > 0 {
		return nil, fmt.Errorf("no enough data to read %d", remainSize)
	}

	return blobs, nil
}

func genSortedVuidByIDC(ctx context.Context, serviceController controller.ServiceController, idc string,
	vuidPhys []controller.Unit) []sortedVuid {
	span := trace.SpanFromContextSafe(ctx)

	vuids := make([]sortedVuid, 0, len(vuidPhys))
	sortMap := make(map[int][]sortedVuid)

	for idx, phy := range vuidPhys {
		var hostIDC *controller.HostIDC
		if err := retry.ExponentialBackoff(2, 100).On(func() error {
			hi, e := serviceController.GetDiskHost(context.Background(), phy.DiskID)
			if e != nil {
				return e
			}
			hostIDC = hi
			return nil
		}); err != nil {
			span.Warnf("no host of disk(%d %d) %s", phy.Vuid, phy.DiskID, err.Error())
			continue
		}

		dis := distance(idc, hostIDC.IDC, hostIDC.Punished)
		if _, ok := sortMap[dis]; !ok {
			sortMap[dis] = make([]sortedVuid, 0, 8)
		}
		sortMap[dis] = append(sortMap[dis], sortedVuid{
			index:  idx,
			vuid:   phy.Vuid,
			diskID: phy.DiskID,
			host:   phy.Host,
		})
	}

	keys := make([]int, 0, len(sortMap))
	for dis := range sortMap {
		keys = append(keys, dis)
	}
	sort.Ints(keys)

	for _, dis := range keys {
		ids := sortMap[dis]
		rand.Shuffle(len(ids), func(i, j int) {
			ids[i], ids[j] = ids[j], ids[i]
		})
		vuids = append(vuids, ids...)
		if dis > 1 {
			span.Debugf("distance: %d punished vuids: %+v", dis, ids)
		}
	}

	return vuids
}

func distance(idc1, idc2 string, punished bool) int {
	if punished {
		if idc1 == idc2 {
			return 2
		}
		return 3
	}
	if idc1 == idc2 {
		return 0
	}
	return 1
}

func emptyDataShardIndexes(sizes ec.BufferSizes) map[int]struct{} {
	firstEmptyIdx := (sizes.DataSize + sizes.ShardSize - 1) / sizes.ShardSize
	n := sizes.ECDataSize / sizes.ShardSize
	if firstEmptyIdx >= n {
		return make(map[int]struct{})
	}

	set := make(map[int]struct{}, n-firstEmptyIdx)
	for i := firstEmptyIdx; i < n; i++ {
		set[i] = struct{}{}
	}

	return set
}
