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

// TestHandleExtensionsList 验证 extensions.list 返回空列表。
func TestHandleExtensionsList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodExtensionsList, json.RawMessage(`{}`))

	resp, err := s.handleExtensionsList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleExtensionsList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["extensions"]; !ok {
		t.Error("payload 应包含 extensions")
	}
}

// TestHandleHooksList 验证 hooks.list 返回 events、disable_all_hooks、source。
func TestHandleHooksList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHooksList, json.RawMessage(`{}`))

	resp, err := s.handleHooksList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHooksList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["events"]; !ok {
		t.Error("payload 应包含 events")
	}
	if _, ok := resp.Payload["disable_all_hooks"]; !ok {
		t.Error("payload 应包含 disable_all_hooks")
	}
	if _, ok := resp.Payload["source"]; !ok {
		t.Error("payload 应包含 source")
	}
}

// TestHandleExtensionsImport 验证 extensions.import 返回 ok=true。
func TestHandleExtensionsImport(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodExtensionsImport, json.RawMessage(`{}`))

	resp, err := s.handleExtensionsImport(context.Background(), req)
	if err != nil {
		t.Fatalf("handleExtensionsImport 返回错误: %v", err)
	}
	if ok := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}
