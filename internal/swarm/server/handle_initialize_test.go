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

// TestHandleInitialize 验证 initialize 返回 capabilities 和 protocol_version。
func TestHandleInitialize(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodInitialize, json.RawMessage(`{}`))

	resp, err := s.handleInitialize(context.Background(), req)
	if err != nil {
		t.Fatalf("handleInitialize 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if _, ok := resp.Payload["capabilities"]; !ok {
		t.Error("payload 应包含 capabilities")
	}
	if pv, ok := resp.Payload["protocol_version"]; !ok || pv != "1.0" {
		t.Errorf("payload.protocol_version 应为 1.0, 实际: %v", pv)
	}
}

// TestHandleACPToolResponse 验证 acp.tool_response 返回 ok=true。
func TestHandleACPToolResponse(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodACPToolResponse, json.RawMessage(`{}`))

	resp, err := s.handleACPToolResponse(context.Background(), req)
	if err != nil {
		t.Fatalf("handleACPToolResponse 返回错误: %v", err)
	}
	if ok, _ := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}
