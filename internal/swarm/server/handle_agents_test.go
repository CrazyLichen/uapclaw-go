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

// TestHandleAgentsList 验证 agents.list 返回空列表。
func TestHandleAgentsList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsList, json.RawMessage(`{}`))

	resp, err := s.handleAgentsList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["agents"]; !ok {
		t.Error("payload 应包含 agents")
	}
}

// TestHandleAgentsGet 验证 agents.get 返回 NOT_FOUND。
func TestHandleAgentsGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsGet, json.RawMessage(`{}`))

	resp, err := s.handleAgentsGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsGet 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_FOUND）")
	}
}

// TestHandleAgentsCreate 验证 agents.create 返回 NOT_IMPLEMENTED。
func TestHandleAgentsCreate(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsCreate, json.RawMessage(`{}`))

	resp, err := s.handleAgentsCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsCreate 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestHandleAgentsToolsList 验证 agents.tools_list 返回空列表。
func TestHandleAgentsToolsList(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentsToolsList, json.RawMessage(`{}`))

	resp, err := s.handleAgentsToolsList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentsToolsList 返回错误: %v", err)
	}
	if _, ok := resp.Payload["tools"]; !ok {
		t.Error("payload 应包含 tools")
	}
}
