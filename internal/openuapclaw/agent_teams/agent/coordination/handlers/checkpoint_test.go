package handlers

import (
	"context"
	"testing"

	types "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestCheckpointHandler_GetCallbacks_事件映射 验证 event_key 注册（1 项）。
// Python: CheckpointHandler.EVENT_METHOD_MAP
func TestCheckpointHandler_GetCallbacks_事件映射(t *testing.T) {
	h := NewCheckpointHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	cb := h.GetCallbacks()
	if len(cb) != 1 {
		t.Fatalf("GetCallbacks 返回 %d 项，期望 1 项", len(cb))
	}
	if _, ok := cb[events.TeamEventCheckpointCreated]; !ok {
		t.Fatalf("缺少 event_key=%q", events.TeamEventCheckpointCreated)
	}
}

// TestCheckpointHandler_OnCheckpointCreated_LeaderAnnouncement 验证 leader 收到
// CHECKPOINT_CREATED 后渲染 announcement-only 框架事件并投递。
// Python: on_checkpoint_created (handlers/checkpoint.py:36-66)
func TestCheckpointHandler_OnCheckpointCreated_LeaderAnnouncement(t *testing.T) {
	host := &fakeHost{}
	h := NewCheckpointHandler(host, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventCheckpointCreated,
		Payload: map[string]any{
			"name":          "code-ready",
			"member_name":   "dev-1",
			"message_count": float64(12),
			"description":   "编码就绪",
		},
	}}
	h.OnCheckpointCreated(context.Background(), event)
	if !host.deliverCalled {
		t.Fatal("leader 应收到 announcement 投递")
	}
}

// TestCheckpointHandler_OnCheckpointCreated_非Leader跳过 验证非 leader 不处理。
func TestCheckpointHandler_OnCheckpointCreated_非Leader跳过(t *testing.T) {
	host := &fakeHost{}
	h := NewCheckpointHandler(host, &fakeBP{role: schema.TeamRoleWorker}, nil, &fakePollCtrl{})
	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventCheckpointCreated,
		Payload:   map[string]any{"name": "x", "member_name": "y", "message_count": float64(1)},
	}}
	h.OnCheckpointCreated(context.Background(), event)
	if host.deliverCalled {
		t.Fatal("非 leader 不应投递")
	}
}

// TestCheckpointHandler_OnCheckpointCreated_缺Name跳过 验证 payload 解码失败
// （缺 name）debug 返回不投递。Python: payload decode except 分支。
func TestCheckpointHandler_OnCheckpointCreated_缺Name跳过(t *testing.T) {
	host := &fakeHost{}
	h := NewCheckpointHandler(host, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventCheckpointCreated,
		Payload:   map[string]any{"member_name": "y"},
	}}
	h.OnCheckpointCreated(context.Background(), event)
	if host.deliverCalled {
		t.Fatal("缺 name 不应投递")
	}
}

// TestCheckpointHandler_OnCheckpointCreated_Inner事件跳过 验证内部事件不处理。
func TestCheckpointHandler_OnCheckpointCreated_Inner事件跳过(t *testing.T) {
	host := &fakeHost{}
	h := NewCheckpointHandler(host, &fakeBP{role: schema.TeamRoleLeader}, nil, &fakePollCtrl{})
	h.OnCheckpointCreated(context.Background(), types.CoordinationEvent{Inner: &types.InnerEventMessage{}})
	if host.deliverCalled {
		t.Fatal("inner 事件不应投递")
	}
}
