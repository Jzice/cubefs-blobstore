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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/cubefs/blobstore/api/scheduler"
)

type MockReportCli struct {
	renewalFail   error
	failTaskIDMap map[string]bool
}

func (m *MockReportCli) RenewalTask(ctx context.Context, tasks *api.TaskRenewalArgs) (ret *api.TaskRenewalRet, err error) {
	result := api.TaskRenewalRet{}
	result.Repair = make(map[string]string)
	result.Balance = make(map[string]string)
	result.DiskDrop = make(map[string]string)

	for taskID := range tasks.Repair {
		if _, ok := m.failTaskIDMap[taskID]; ok {
			result.Repair[taskID] = "mock fail"
		}
	}

	for taskID := range tasks.Balance {
		if _, ok := m.failTaskIDMap[taskID]; ok {
			result.Balance[taskID] = "mock fail"
		}
	}

	for taskID := range tasks.DiskDrop {
		if _, ok := m.failTaskIDMap[taskID]; ok {
			result.DiskDrop[taskID] = "mock fail"
		}
	}

	return &result, m.renewalFail
}

func TestReport(t *testing.T) {
	idc := "Z0"
	tm := initTestTaskRunnerMgr(t, 10)
	time.Sleep(200 * time.Millisecond)
	// test renewal ok
	reportCli := MockReportCli{
		renewalFail:   nil,
		failTaskIDMap: make(map[string]bool),
	}
	taskRenter := NewTaskRenter(idc, &reportCli, tm)
	taskRenter.renewalTask()
	require.Equal(t, 10, len(tm.GetRepairAliveTask()))
	require.Equal(t, 10, len(tm.GetBalanceAliveTask()))
	require.Equal(t, 10, len(tm.GetDiskDropAliveTask()))

	//////test renewal fail

	tm2 := initTestTaskRunnerMgr(t, 10)
	time.Sleep(200 * time.Millisecond)
	reportCli2 := MockReportCli{
		renewalFail:   nil,
		failTaskIDMap: make(map[string]bool),
	}
	reportCli2.failTaskIDMap["repair_1"] = true
	reportCli2.failTaskIDMap["balance_1"] = true
	reportCli2.failTaskIDMap["diskDrop_1"] = true

	taskRenter2 := NewTaskRenter(idc, &reportCli2, tm2)
	taskRenter2.renewalTask()
	require.Equal(t, 9, len(tm2.GetRepairAliveTask()))
	require.Equal(t, 9, len(tm2.GetBalanceAliveTask()))
	require.Equal(t, 9, len(tm2.GetDiskDropAliveTask()))

	// test all renewal fail
	tm3 := initTestTaskRunnerMgr(t, 10)
	time.Sleep(200 * time.Millisecond)
	reportCli3 := MockReportCli{
		renewalFail:   errors.New("mock fail"),
		failTaskIDMap: make(map[string]bool),
	}
	taskRenter3 := NewTaskRenter(idc, &reportCli3, tm3)
	taskRenter3.renewalTask()
	require.Equal(t, 0, len(tm3.GetRepairAliveTask()))
	require.Equal(t, 0, len(tm3.GetBalanceAliveTask()))
	require.Equal(t, 0, len(tm3.GetDiskDropAliveTask()))
}
