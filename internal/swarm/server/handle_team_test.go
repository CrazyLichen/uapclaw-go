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

// TestHandleTeamDelete 验证 team.delete 返回 ok=true。
func TestHandleTeamDelete(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodTeamDelete, json.RawMessage(`{}`))

	resp, err := s.handleTeamDelete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleTeamDelete 返回错误: %v", err)
	}
	if ok := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
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
