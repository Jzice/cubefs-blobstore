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

package rpc

const (
	BLOBNODE_API_URI_STAT           = "/stat"
	BLOBNODE_API_URI_STAT_FMT       = "%v/stat"
	BLOBNODE_API_URI_DEBUG_STAT     = "/debug/stat"
	BLOBNODE_API_URI_DEBUG_STAT_FMT = "%v/debug/stat"

	BLOBNODE_API_URI_DISK_STAT      = "/disk/stat/diskid/:diskid"
	BLOBNODE_API_URI_DISK_STAT_FMT  = "%v/disk/stat/diskid/%v"
	BLOBNODE_API_URI_DISK_PROBE     = "/disk/probe"
	BLOBNODE_API_URI_DISK_PROBE_FMT = "%v/disk/probe"

	BLOBNODE_API_URI_CHUNK_CREATE      = "/chunk/create/diskid/:diskid/vuid/:vuid"
	BLOBNODE_API_URI_CHUNK_CREATE_FMT  = "/chunk/create/diskid/%v/vuid/%v"
	BLOBNODE_API_URI_CHUNK_RELEASE     = "/chunk/release/diskid/:diskid/vuid/:vuid"
	BLOBNODE_API_URI_CHUNK_RELEASE_FMT = "/chunk/release/diskid/%v/vuid/%v"

	BLOBNODE_API_URI_CHUNK_READONLY  = "/chunk/readonly/diskid/:diskid/vuid/:vuid"
	BLOBNODE_API_URI_CHUNK_READWRITE = "/chunk/readwrite/diskid/:diskid/vuid/:vuid"
	BLOBNODE_API_URI_CHUNK_LIST      = "/chunk/list/diskid/:diskid"
	BLOBNODE_API_URI_CHUNK_STAT      = "/chunk/stat/diskid/:diskid/vuid/:vuid"
	BLOBNODE_API_URI_CHUNK_COMPACT   = "/chunk/compact/diskid/:diskid/vuid/:vuid"
)

//r.Handle(http.MethodPost, "/chunk/create/diskid/:diskid/vuid/:vuid", service.ChunkCreate_, rpc.OptArgsURI(), rpc.OptArgsQuery())
//r.Handle(http.MethodPost, "/chunk/release/diskid/:diskid/vuid/:vuid", service.ChunkRelease_, rpc.OptArgsURI(), rpc.OptArgsQuery())
//r.Handle(http.MethodPost, "/chunk/readonly/diskid/:diskid/vuid/:vuid", service.ChunkReadonly_, rpc.OptArgsURI())
//r.Handle(http.MethodPost, "/chunk/readwrite/diskid/:diskid/vuid/:vuid", service.ChunkReadwrite_, rpc.OptArgsURI())
//r.Handle(http.MethodGet, "/chunk/list/diskid/:diskid", service.ChunkList_, rpc.OptArgsURI())
//r.Handle(http.MethodGet, "/chunk/stat/diskid/:diskid/vuid/:vuid", service.ChunkStat_, rpc.OptArgsURI())
//r.Handle(http.MethodPost, "/chunk/compact/diskid/:diskid/vuid/:vuid", service.ChunkCompact_, rpc.OptArgsURI())

//r.Handle(http.MethodGet, "/shard/get/diskid/:diskid/vuid/:vuid/bid/:bid", service.ShardGet_, rpc.OptArgsURI(), rpc.OptArgsQuery())
//r.Handle(http.MethodGet, "/shard/list/diskid/:diskid/vuid/:vuid/startbid/:startbid/status/:status/count/:count", service.ShardList_, rpc.OptArgsURI())
//r.Handle(http.MethodPost, "/shards", service.GetShards, rpc.OptArgsBody())
//r.Handle(http.MethodGet, "/shard/stat/diskid/:diskid/vuid/:vuid/bid/:bid", service.ShardStat_, rpc.OptArgsURI())
//r.Handle(http.MethodPost, "/shard/markdelete/diskid/:diskid/vuid/:vuid/bid/:bid", service.ShardMarkdelete_, rpc.OptArgsURI())
//r.Handle(http.MethodPost, "/shard/delete/diskid/:diskid/vuid/:vuid/bid/:bid", service.ShardDelete_, rpc.OptArgsURI())
//r.Handle(http.MethodPost, "/shard/put/diskid/:diskid/vuid/:vuid/bid/:bid/size/:size", service.ShardPut_, rpc.OptArgsURI(), rpc.OptArgsQuery())
