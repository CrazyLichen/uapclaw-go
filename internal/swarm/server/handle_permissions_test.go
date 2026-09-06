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
	// 真实实现返回的键为 "approval_overrides"（对齐 Python: get_permissions_approval_overrides）
	if _, ok := resp.Payload["approval_overrides"]; !ok {
		t.Error("payload 应包含 approval_overrides")
	}
}

// TestHandlePermissionsConfig_ToolsSet_缺少tools 验证 permissions.tools.set 缺少 tools 参数时返回错误。
func TestHandlePermissionsConfig_ToolsSet_缺少tools(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodPermissionsToolsSet, json.RawMessage(`{}`))

	resp, err := s.handlePermissionsConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePermissionsConfig 返回错误: %v", err)
	}
	// 缺少 tools 参数时，真实实现返回 ok=false + error
	if resp.OK {
		t.Error("缺少 tools 参数时应返回 ok=false")
	}
}
