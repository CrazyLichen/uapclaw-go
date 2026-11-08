package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHandleTeamDelete 验证 team.delete 缺 team_name 时拒绝。
//
// 10.3.28c 起 team.delete 不再是返回 ok 的桩：它走 provisioner.DeleteTeam
// 的两阶段事务（整体静默 → 逐会话删 → Runner 一次），因此缺参数必须报
// BAD_REQUEST，而不是假装成功——假装成功会让前端以为团队已删。
// Python: agent_ws_server.py _handle_lifecycle_request 的 team 分支
func TestHandleTeamDelete(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodTeamDelete, json.RawMessage(`{}`))

	resp, err := s.handleTeamDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeamDelete 返回错误: %v", err)
	}
	if ok, _ := resp.Payload["ok"].(bool); ok {
		t.Error("缺 team_name 不应返回 ok=true")
	}
	if code, _ := resp.Payload["code"].(string); code != "BAD_REQUEST" {
		t.Errorf("code 应为 BAD_REQUEST，实际: %v", resp.Payload["code"])
	}
}

// TestHandleTeamDelete_有团队名走事务 带 team_name 时进入 provisioner 删除事务。
//
// 测试环境没有真实 Runner，DeleteAgentTeam 必然失败——这里断言的是
// "确实走了事务并如实报告失败"（而不是桩时代的假装成功）。成功路径由
// runtime 包的 TestDeleteTeam_* 用注入的 DeleteAgentTeam 覆盖。
func TestHandleTeamDelete_有团队名走事务(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodTeamDelete,
		json.RawMessage(`{"team_name":"team-a"}`))

	resp, err := s.handleTeamDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeamDelete 不应返回 error: %v", err)
	}
	// Runner 不可用时应如实报告失败，而不是假装成功
	if ok, _ := resp.Payload["ok"].(bool); ok {
		t.Errorf("Runner 不可用时不应返回 ok=true，实际 payload: %v", resp.Payload)
	}
	if _, exists := resp.Payload["failed_session_ids"]; !exists {
		t.Errorf("失败时应带 failed_session_ids 字段，实际 payload: %v", resp.Payload)
	}
}

// TestHandleTeamSnapshot 验证 team.snapshot 返回空快照。
func TestHandleTeamSnapshot(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodTeamSnapshot, json.RawMessage(`{}`))

	resp, err := s.handleTeamSnapshot(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeamSnapshot 返回错误: %v", err)
	}
	if _, ok := resp.Payload["snapshot"]; !ok {
		t.Error("payload 应包含 snapshot")
	}
}

// TestHandleTeamHistoryGet 验证 team.history.get 返回空列表。
func TestHandleTeamHistoryGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodTeamHistoryGet, json.RawMessage(`{}`))

	resp, err := s.handleTeamHistoryGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeamHistoryGet 返回错误: %v", err)
	}
	if _, ok := resp.Payload["history"]; !ok {
		t.Error("payload 应包含 history")
	}
}
