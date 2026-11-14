package rsi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestEngineEvent_五类型判别 5 个事件的 EventType() 恒定值
func TestEngineEvent_五类型判别(t *testing.T) {
	cases := map[string]EngineEvent{
		"status":         &EventStatus{},
		"progress":       &EventProgress{},
		"node":           &EventNode{},
		"progress.usage": &EventUsage{},
		"node.stage":     &NodeStageEvent{},
	}
	for want, ev := range cases {
		if got := ev.EventType(); got != want {
			t.Fatalf("EventType %q != %q", got, want)
		}
	}
}

// TestEventStatus_访问器 status 事件载荷
func TestEventStatus_访问器(t *testing.T) {
	ev := NewEventStatus(RsiStatusRunning)
	if ev.Status() != RsiStatusRunning {
		t.Fatal("status 不符")
	}
}

// TestEventProgress_访问器 progress 事件载荷
func TestEventProgress_访问器(t *testing.T) {
	usage := NewRsiUsage(NewRsiUsageTokens(nil, nil, nil), nil, 1)
	ev := NewEventProgress(2, 10, floatPtr(0.7), floatPtr(0.5), usage)
	if ev.Iteration() != 2 || ev.TotalIterations() != 10 || ev.Score() == nil || *ev.Score() != 0.7 {
		t.Fatal("progress 字段不符")
	}
	if ev.Baseline() == nil || *ev.Baseline() != 0.5 || ev.Usage() != usage {
		t.Fatal("progress 可选字段不符")
	}
}

// TestEventNode_访问器 node 事件载荷与 artifacts 默认空列表
func TestEventNode_访问器(t *testing.T) {
	ev := NewEventNode(nil, nil)
	if ev.Artifacts() == nil || len(ev.Artifacts()) != 0 {
		t.Fatal("artifacts 缺省应为空列表（Python default_factory=list）")
	}
	node := NewRsiTreeNode("n1", 1, nil, "root", false, nil, nil, nil, nil, nil, nil, nil)
	ev2 := NewEventNode(node, []map[string]string{{"k": "v"}})
	if ev2.Node() != node || len(ev2.Artifacts()) != 1 {
		t.Fatal("node 事件字段不符")
	}
}

// TestEventUsage_字段访问器 usage 事件带独立字段（event_id/task_id/call_id）
func TestEventUsage_字段访问器(t *testing.T) {
	mc := NewRsiModelCall("m1", 1, NewRsiUsageTokens(nil, nil, nil), "", nil)
	ev := NewEventUsage(7, "t1", "2026-01-01T00:00:00Z", "c1", mc, strPtr("n1"), strPtr("s1"))
	if ev.EventID() != 7 || ev.TaskID() != "t1" || ev.CallID() != "c1" || ev.ModelCall() != mc {
		t.Fatal("usage 事件字段不符")
	}
	if ev.NodeRef() == nil || *ev.NodeRef() != "n1" || ev.StageRef() == nil || *ev.StageRef() != "s1" {
		t.Fatal("usage 事件引用字段不符")
	}
	if ev.Kind() != "usage" || ev.Family() != "progress" {
		t.Fatal("kind/family 恒定值不符")
	}
}

// TestEventUsage_MarshalJSON 账本行键名与 Python asdict 一致
func TestEventUsage_MarshalJSON(t *testing.T) {
	mc := NewRsiModelCall("m1", 1, NewRsiUsageTokens(intPtr(1), nil, nil), "", nil)
	ev := NewEventUsage(1, "t1", "ts", "c1", mc, nil, nil)
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"event_id", "task_id", "ts", "call_id", "model_call", "node_ref", "stage_ref", "family", "kind", "event_type"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("账本行缺少键 %q: %s", key, data)
		}
	}
}

// TestNodeStageEvent_访问器 stage 透传对象
func TestNodeStageEvent_访问器(t *testing.T) {
	ev := NewNodeStageEvent("n1", map[string]any{"stage": "eval"}, strPtr("note"))
	if ev.NodeRef() != "n1" || ev.Stage()["stage"] != "eval" || ev.Note() == nil || *ev.Note() != "note" {
		t.Fatal("stage 事件字段不符")
	}
}

// TestEventNodeStage_类型别名 别名指向 NodeStageEvent
func TestEventNodeStage_类型别名(t *testing.T) {
	var alias EventNodeStage = NewNodeStageEvent("n", nil, nil)
	if alias.EventType() != "node.stage" {
		t.Fatal("别名判别不符")
	}
}

// TestEmit_无回调跳过 on_event 为 nil 时静默
func TestEmit_无回调跳过(t *testing.T) {
	if err := Emit(context.Background(), nil, NewEventStatus(RsiStatusRunning)); err != nil {
		t.Fatal("nil 回调应静默")
	}
}

// TestEmit_回调错误不吞 回调返回错误必须透传（Python: intentionally not swallowed）
func TestEmit_回调错误不吞(t *testing.T) {
	boom := errors.New("boom")
	err := Emit(context.Background(), func(ctx context.Context, e EngineEvent) error {
		return boom
	}, NewEventStatus(RsiStatusRunning))
	if !errors.Is(err, boom) {
		t.Fatalf("应透传回调错误，得到 %v", err)
	}
}
