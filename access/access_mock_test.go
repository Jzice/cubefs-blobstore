// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/cubefs/blobstore/access (interfaces: StreamHandler,Limiter)

// Package access is a generated GoMock package.
package access

import (
	context "context"
	io "io"
	reflect "reflect"

	access0 "github.com/cubefs/blobstore/api/access"
	codemode "github.com/cubefs/blobstore/common/codemode"
	proto "github.com/cubefs/blobstore/common/proto"
	gomock "github.com/golang/mock/gomock"
)

// MockStreamHandler is a mock of StreamHandler interface.
type MockStreamHandler struct {
	ctrl     *gomock.Controller
	recorder *MockStreamHandlerMockRecorder
}

// MockStreamHandlerMockRecorder is the mock recorder for MockStreamHandler.
type MockStreamHandlerMockRecorder struct {
	mock *MockStreamHandler
}

// NewMockStreamHandler creates a new mock instance.
func NewMockStreamHandler(ctrl *gomock.Controller) *MockStreamHandler {
	mock := &MockStreamHandler{ctrl: ctrl}
	mock.recorder = &MockStreamHandlerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockStreamHandler) EXPECT() *MockStreamHandlerMockRecorder {
	return m.recorder
}

// Alloc mocks base method.
func (m *MockStreamHandler) Alloc(arg0 context.Context, arg1 uint64, arg2 uint32, arg3 proto.ClusterID, arg4 codemode.CodeMode) (*access0.Location, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Alloc", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(*access0.Location)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Alloc indicates an expected call of Alloc.
func (mr *MockStreamHandlerMockRecorder) Alloc(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Alloc", reflect.TypeOf((*MockStreamHandler)(nil).Alloc), arg0, arg1, arg2, arg3, arg4)
}

// Delete mocks base method.
func (m *MockStreamHandler) Delete(arg0 context.Context, arg1 *access0.Location) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Delete", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Delete indicates an expected call of Delete.
func (mr *MockStreamHandlerMockRecorder) Delete(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Delete", reflect.TypeOf((*MockStreamHandler)(nil).Delete), arg0, arg1)
}

// Get mocks base method.
func (m *MockStreamHandler) Get(arg0 context.Context, arg1 io.Writer, arg2 access0.Location, arg3, arg4 uint64) (func() error, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(func() error)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockStreamHandlerMockRecorder) Get(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockStreamHandler)(nil).Get), arg0, arg1, arg2, arg3, arg4)
}

// Put mocks base method.
func (m *MockStreamHandler) Put(arg0 context.Context, arg1 io.Reader, arg2 int64, arg3 access0.HasherMap) (*access0.Location, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Put", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(*access0.Location)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Put indicates an expected call of Put.
func (mr *MockStreamHandlerMockRecorder) Put(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Put", reflect.TypeOf((*MockStreamHandler)(nil).Put), arg0, arg1, arg2, arg3)
}

// PutAt mocks base method.
func (m *MockStreamHandler) PutAt(arg0 context.Context, arg1 io.Reader, arg2 proto.ClusterID, arg3 proto.Vid, arg4 proto.BlobID, arg5 int64, arg6 access0.HasherMap) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PutAt", arg0, arg1, arg2, arg3, arg4, arg5, arg6)
	ret0, _ := ret[0].(error)
	return ret0
}

// PutAt indicates an expected call of PutAt.
func (mr *MockStreamHandlerMockRecorder) PutAt(arg0, arg1, arg2, arg3, arg4, arg5, arg6 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PutAt", reflect.TypeOf((*MockStreamHandler)(nil).PutAt), arg0, arg1, arg2, arg3, arg4, arg5, arg6)
}

// MockLimiter is a mock of Limiter interface.
type MockLimiter struct {
	ctrl     *gomock.Controller
	recorder *MockLimiterMockRecorder
}

// MockLimiterMockRecorder is the mock recorder for MockLimiter.
type MockLimiterMockRecorder struct {
	mock *MockLimiter
}

// NewMockLimiter creates a new mock instance.
func NewMockLimiter(ctrl *gomock.Controller) *MockLimiter {
	mock := &MockLimiter{ctrl: ctrl}
	mock.recorder = &MockLimiterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockLimiter) EXPECT() *MockLimiterMockRecorder {
	return m.recorder
}

// Acquire mocks base method.
func (m *MockLimiter) Acquire(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Acquire", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Acquire indicates an expected call of Acquire.
func (mr *MockLimiterMockRecorder) Acquire(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Acquire", reflect.TypeOf((*MockLimiter)(nil).Acquire), arg0)
}

// Reader mocks base method.
func (m *MockLimiter) Reader(arg0 context.Context, arg1 io.Reader) io.Reader {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Reader", arg0, arg1)
	ret0, _ := ret[0].(io.Reader)
	return ret0
}

// Reader indicates an expected call of Reader.
func (mr *MockLimiterMockRecorder) Reader(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Reader", reflect.TypeOf((*MockLimiter)(nil).Reader), arg0, arg1)
}

// Release mocks base method.
func (m *MockLimiter) Release(arg0 string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Release", arg0)
}

// Release indicates an expected call of Release.
func (mr *MockLimiterMockRecorder) Release(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Release", reflect.TypeOf((*MockLimiter)(nil).Release), arg0)
}

// Status mocks base method.
func (m *MockLimiter) Status() Status {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Status")
	ret0, _ := ret[0].(Status)
	return ret0
}

// Status indicates an expected call of Status.
func (mr *MockLimiterMockRecorder) Status() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockLimiter)(nil).Status))
}

// Writer mocks base method.
func (m *MockLimiter) Writer(arg0 context.Context, arg1 io.Writer) io.Writer {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Writer", arg0, arg1)
	ret0, _ := ret[0].(io.Writer)
	return ret0
}

// Writer indicates an expected call of Writer.
func (mr *MockLimiterMockRecorder) Writer(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Writer", reflect.TypeOf((*MockLimiter)(nil).Writer), arg0, arg1)
}
