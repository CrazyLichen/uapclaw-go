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
