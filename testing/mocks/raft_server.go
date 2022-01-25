// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/cubefs/blobstore/common/raftserver (interfaces: RaftServer)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	raftserver "github.com/cubefs/blobstore/common/raftserver"
	gomock "github.com/golang/mock/gomock"
)

// MockRaftServer is a mock of RaftServer interface.
type MockRaftServer struct {
	ctrl     *gomock.Controller
	recorder *MockRaftServerMockRecorder
}

// MockRaftServerMockRecorder is the mock recorder for MockRaftServer.
type MockRaftServerMockRecorder struct {
	mock *MockRaftServer
}

// NewMockRaftServer creates a new mock instance.
func NewMockRaftServer(ctrl *gomock.Controller) *MockRaftServer {
	mock := &MockRaftServer{ctrl: ctrl}
	mock.recorder = &MockRaftServerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRaftServer) EXPECT() *MockRaftServerMockRecorder {
	return m.recorder
}

// AddLearner mocks base method.
func (m *MockRaftServer) AddLearner(arg0 context.Context, arg1 uint64, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddLearner", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddLearner indicates an expected call of AddLearner.
func (mr *MockRaftServerMockRecorder) AddLearner(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddLearner", reflect.TypeOf((*MockRaftServer)(nil).AddLearner), arg0, arg1, arg2)
}

// AddMember mocks base method.
func (m *MockRaftServer) AddMember(arg0 context.Context, arg1 uint64, arg2 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddMember", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// AddMember indicates an expected call of AddMember.
func (mr *MockRaftServerMockRecorder) AddMember(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddMember", reflect.TypeOf((*MockRaftServer)(nil).AddMember), arg0, arg1, arg2)
}

// IsLeader mocks base method.
func (m *MockRaftServer) IsLeader() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsLeader")
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsLeader indicates an expected call of IsLeader.
func (mr *MockRaftServerMockRecorder) IsLeader() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsLeader", reflect.TypeOf((*MockRaftServer)(nil).IsLeader))
}

// Propose mocks base method.
func (m *MockRaftServer) Propose(arg0 context.Context, arg1 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Propose", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Propose indicates an expected call of Propose.
func (mr *MockRaftServerMockRecorder) Propose(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Propose", reflect.TypeOf((*MockRaftServer)(nil).Propose), arg0, arg1)
}

// ReadIndex mocks base method.
func (m *MockRaftServer) ReadIndex(arg0 context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadIndex", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// ReadIndex indicates an expected call of ReadIndex.
func (mr *MockRaftServerMockRecorder) ReadIndex(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadIndex", reflect.TypeOf((*MockRaftServer)(nil).ReadIndex), arg0)
}

// RemoveMember mocks base method.
func (m *MockRaftServer) RemoveMember(arg0 context.Context, arg1 uint64) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoveMember", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemoveMember indicates an expected call of RemoveMember.
func (mr *MockRaftServerMockRecorder) RemoveMember(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoveMember", reflect.TypeOf((*MockRaftServer)(nil).RemoveMember), arg0, arg1)
}

// Status mocks base method.
func (m *MockRaftServer) Status() raftserver.Status {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Status")
	ret0, _ := ret[0].(raftserver.Status)
	return ret0
}

// Status indicates an expected call of Status.
func (mr *MockRaftServerMockRecorder) Status() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockRaftServer)(nil).Status))
}

// Stop mocks base method.
func (m *MockRaftServer) Stop() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Stop")
}

// Stop indicates an expected call of Stop.
func (mr *MockRaftServerMockRecorder) Stop() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Stop", reflect.TypeOf((*MockRaftServer)(nil).Stop))
}

// TransferLeadership mocks base method.
func (m *MockRaftServer) TransferLeadership(arg0 context.Context, arg1, arg2 uint64) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "TransferLeadership", arg0, arg1, arg2)
}

// TransferLeadership indicates an expected call of TransferLeadership.
func (mr *MockRaftServerMockRecorder) TransferLeadership(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "TransferLeadership", reflect.TypeOf((*MockRaftServer)(nil).TransferLeadership), arg0, arg1, arg2)
}

// Truncate mocks base method.
func (m *MockRaftServer) Truncate(arg0 uint64) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Truncate", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Truncate indicates an expected call of Truncate.
func (mr *MockRaftServerMockRecorder) Truncate(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Truncate", reflect.TypeOf((*MockRaftServer)(nil).Truncate), arg0)
}