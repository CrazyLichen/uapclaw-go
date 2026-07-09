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

// TestHandlePermissionsConfig_ToolsGet 验证 permissions.tools.get 返回空列表。
func TestHandlePermissionsConfig_ToolsGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodPermissionsToolsGet, json.RawMessage(`{}`))

	resp, err := s.handlePermissionsConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePermissionsConfig 返回错误: %v", err)
	}
	if _, ok := resp.Payload["tools"]; !ok {
		t.Error("payload 应包含 tools")
	}
}

// TestHandlePermissionsConfig_RulesGet 验证 permissions.rules.get 返回空列表。
func TestHandlePermissionsConfig_RulesGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodPermissionsRulesGet, json.RawMessage(`{}`))

	resp, err := s.handlePermissionsConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePermissionsConfig 返回错误: %v", err)
	}
	if _, ok := resp.Payload["rules"]; !ok {
		t.Error("payload 应包含 rules")
	}
}

// TestHandlePermissionsConfig_ApprovalOverridesGet 验证 permissions.approval_overrides.get 返回空列表。
func TestHandlePermissionsConfig_ApprovalOverridesGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodPermissionsApprovalOverridesGet, json.RawMessage(`{}`))

	resp, err := s.handlePermissionsConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePermissionsConfig 返回错误: %v", err)
	}
	if _, ok := resp.Payload["overrides"]; !ok {
		t.Error("payload 应包含 overrides")
	}
}

// TestHandlePermissionsConfig_ToolsSet 验证 permissions.tools.set 返回 ok=true。
func TestHandlePermissionsConfig_ToolsSet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodPermissionsToolsSet, json.RawMessage(`{}`))

	resp, err := s.handlePermissionsConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePermissionsConfig 返回错误: %v", err)
	}
	if ok, _ := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}
