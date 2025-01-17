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
	"github.com/cubefs/blobstore/api/clustermgr"
	apierrors "github.com/cubefs/blobstore/common/errors"
	"github.com/cubefs/blobstore/common/rpc"
	"github.com/cubefs/blobstore/common/trace"
)

/*
	manage.go implements cluster manage API
*/

func (s *Service) MemberAdd(c *rpc.Context) {
	ctx := c.Request.Context()
	span := trace.SpanFromContextSafe(ctx)
	args := new(clustermgr.AddMemberArgs)
	if err := c.ParseArgs(args); err != nil {
		c.RespondError(err)
		return
	}
	span.Infof("accept MemberAdd request, args: %v", args)

	if args.MemberType <= clustermgr.MemberTypeMin || args.MemberType >= clustermgr.MemberTypeMax {
		span.Warnf("invalid member type, valid range(%d-%d)", clustermgr.MemberTypeLearner, clustermgr.MemberTypeNormal)
		c.RespondError(apierrors.ErrIllegalArguments)
		return
	}
	status := s.raftNode.Status()
	for i := range status.Peers {
		if status.Peers[i].Id == args.PeerID || status.Peers[i].Host == args.Host {
			c.RespondError(apierrors.ErrDuplicatedMemberInfo)
			return
		}
	}
	var err error
	switch args.MemberType {
	case clustermgr.MemberTypeLearner:
		err = s.raftNode.AddLearner(ctx, args.PeerID, args.Host)
	case clustermgr.MemberTypeNormal:
		err = s.raftNode.AddMember(ctx, args.PeerID, args.Host)
	}
	c.RespondError(err)
}

func (s *Service) MemberRemove(c *rpc.Context) {
	ctx := c.Request.Context()
	span := trace.SpanFromContextSafe(ctx)
	args := new(clustermgr.RemoveMemberArgs)
	if err := c.ParseArgs(args); err != nil {
		c.RespondError(err)
		return
	}
	span.Infof("accept MemberRemove request, args: %v", args)

	if !s.checkPeerIDExist(args.PeerID) {
		span.Warnf("peer_id not exist")
		c.RespondError(apierrors.ErrIllegalArguments)
		return
	}
	// not allow to remove leader directly, must transfer leadership firstly
	if args.PeerID == s.raftNode.Status().Leader {
		c.RespondError(apierrors.ErrRequestNotAllow)
		return
	}

	if err := s.raftNode.RemoveMember(ctx, args.PeerID); err != nil {
		c.RespondError(err)
	}
}

func (s *Service) LeadershipTransfer(c *rpc.Context) {
	ctx := c.Request.Context()
	span := trace.SpanFromContextSafe(ctx)
	args := new(clustermgr.RemoveMemberArgs)
	if err := c.ParseArgs(args); err != nil {
		c.RespondError(err)
		return
	}
	span.Infof("accept LeadershipTransfer request, args: %v", args)

	if !s.checkPeerIDExist(args.PeerID) {
		span.Warnf("peer_id not exist")
		c.RespondError(apierrors.ErrIllegalArguments)
		return
	}
	s.raftNode.TransferLeadership(ctx, s.raftNode.Status().Id, args.PeerID)
}

func (s *Service) Stat(c *rpc.Context) {
	ctx := c.Request.Context()
	span := trace.SpanFromContextSafe(ctx)
	span.Info("accept Stat request")

	ret := new(clustermgr.StatInfo)
	ret.RaftStatus = s.raftNode.Status()
	ret.LeaderHost = s.raftNode.GetLeaderHost()
	ret.SpaceStat = *(s.DiskMgr.Stat(ctx))
	ret.VolumeStat = s.VolumeMgr.Stat(ctx)
	c.RespondJSON(ret)
}

func (s *Service) checkPeerIDExist(peerID uint64) bool {
	peers := s.raftNode.Status().Peers
	found := false
	for i := range peers {
		if peerID == peers[i].Id {
			found = true
		}
	}
	return found
}
