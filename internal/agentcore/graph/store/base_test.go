package store

import (
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
)

// TestGraphState_IsResumable 三条件判定
func TestGraphState_IsResumable(t *testing.T) {
	empty := &GraphState{}
	if empty.IsResumable() {
		t.Fatal("空状态不应 resumable")
	}
	withNode := &GraphState{PendingNode: map[string]PendingNode{"n": {NodeName: "n", Status: "interrupt"}}}
	if !withNode.IsResumable() {
		t.Fatal("有 pending_node 应 resumable")
	}
	withBuf := &GraphState{PendingBuffer: []pregel.Message{pregel.NewTriggerMessage("a", "n")}}
	if !withBuf.IsResumable() {
		t.Fatal("有 pending_buffer 应 resumable")
	}
	withCV := &GraphState{ChannelValues: map[string][]pregel.ChannelSnapshot{"n": {{}}}}
	if !withCV.IsResumable() {
		t.Fatal("有 channel_values 应 resumable")
	}
}

// TestCreateState_nil 兜底三容器为空容器非 nil
func TestCreateState_nil(t *testing.T) {
	s := CreateState("ns1", 3, nil, nil, nil, nil)
	if s.Ns != "ns1" || s.Step != 3 {
		t.Fatalf("基础字段失真: %+v", s)
	}
	if s.PendingBuffer == nil || len(s.PendingBuffer) != 0 {
		t.Fatal("pending_buffer 应兜底为空切片")
	}
	if s.PendingNode == nil || len(s.PendingNode) != 0 {
		t.Fatal("pending_node 应兜底为空 map")
	}
	if s.NodeVersion == nil || len(s.NodeVersion) != 0 {
		t.Fatal("node_version 应兜底为空 map")
	}
}

// TestGraphState_Clone 深拷贝独立性
func TestGraphState_Clone(t *testing.T) {
	orig := &GraphState{
		Ns:   "ns",
		Step: 2,
		ChannelValues: map[string][]pregel.ChannelSnapshot{
			"n1": {{Messages: []pregel.Message{pregel.NewTriggerMessage("a", "n1")}, Received: []string{"a"}}},
		},
		PendingBuffer: []pregel.Message{pregel.NewBarrierMessage("b", "barrier:x&y->z")},
		PendingNode:   map[string]PendingNode{"n2": {NodeName: "n2", Status: "error", Exception: []error{errors.New("boom")}}},
		NodeVersion:   map[string]int{"n1": 1},
	}
	cp := orig.Clone()

	// 改副本不影响原值
	cp.ChannelValues["n1"][0].Messages[0].Sender = "changed"
	cp.ChannelValues["n1"][0].Received[0] = "changed"
	cp.PendingBuffer[0].Target = "changed"
	pn := cp.PendingNode["n2"]
	pn.Status = "changed"
	cp.PendingNode["n2"] = pn
	cp.NodeVersion["n1"] = 99

	if orig.ChannelValues["n1"][0].Messages[0].Sender != "a" {
		t.Fatal("ChannelValues 未深拷贝")
	}
	if orig.ChannelValues["n1"][0].Received[0] != "a" {
		t.Fatal("Received 未深拷贝")
	}
	if orig.PendingBuffer[0].Target != "barrier:x&y->z" {
		t.Fatal("PendingBuffer 未深拷贝")
	}
	if orig.PendingNode["n2"].Status != "error" {
		t.Fatal("PendingNode 值未隔离")
	}
	if orig.NodeVersion["n1"] != 1 {
		t.Fatal("NodeVersion 值未隔离")
	}

	// nil 状态 Clone 返回 nil
	var nilState *GraphState
	if nilState.Clone() != nil {
		t.Fatal("nil Clone 应返回 nil")
	}
}

// ──────────────────────────── GraphStore 日志壳 ────────────────────────────

// fakeStore 可控返回的存储桩
type fakeStore struct {
	getState  *GraphState
	getErr    error
	saveErr   error
	delErr    error
	getCalls  int
	saveCalls int
	delCalls  int
	lastDelNs string
}

func (f *fakeStore) Get(sessionID, ns string) (*GraphState, error) {
	f.getCalls++
	return f.getState, f.getErr
}
func (f *fakeStore) Save(sessionID, ns string, state *GraphState) error {
	f.saveCalls++
	return f.saveErr
}
func (f *fakeStore) Delete(sessionID, ns string) error {
	f.delCalls++
	f.lastDelNs = ns
	return f.delErr
}

// TestGraphStore_Get 未命中 debug 日志、异常透传
func TestGraphStore_Get(t *testing.T) {
	gs := NewGraphStore(&fakeStore{getState: &GraphState{Ns: "ns"}})
	st, err := gs.Get("s1", "ns")
	if err != nil || st == nil {
		t.Fatalf("命中透传失败: %v %+v", err, st)
	}

	gs = NewGraphStore(&fakeStore{}) // 未命中
	st, err = gs.Get("s1", "ns")
	if err != nil || st != nil {
		t.Fatalf("未命中应透传 (nil,nil): %v %+v", err, st)
	}

	boom := errors.New("db down")
	gs = NewGraphStore(&fakeStore{getErr: boom})
	if _, err = gs.Get("s1", "ns"); err != boom {
		t.Fatalf("异常应原样透传: %v", err)
	}
}

// TestGraphStore_Save 前后日志 + 异常透传
func TestGraphStore_Save(t *testing.T) {
	boom := errors.New("disk full")
	gs := NewGraphStore(&fakeStore{saveErr: boom})
	if err := gs.Save("s1", "ns", &GraphState{}); err != boom {
		t.Fatalf("save 异常应透传: %v", err)
	}
	ok := NewGraphStore(&fakeStore{})
	if err := ok.Save("s1", "ns", &GraphState{}); err != nil {
		t.Fatal(err)
	}
}

// TestGraphStore_Delete 空 ns 透传 all 语义 + 异常透传（Python 走 debug 级也 re-raise）
func TestGraphStore_Delete(t *testing.T) {
	fs := &fakeStore{}
	gs := NewGraphStore(fs)
	if err := gs.Delete("s1", ""); err != nil {
		t.Fatal(err)
	}
	if fs.lastDelNs != "" {
		t.Fatal("空 ns 应原样透传")
	}
	boom := errors.New("del fail")
	gs = NewGraphStore(&fakeStore{delErr: boom})
	if err := gs.Delete("s1", "ns1"); err != boom {
		t.Fatalf("delete 异常应透传: %v", err)
	}
}
