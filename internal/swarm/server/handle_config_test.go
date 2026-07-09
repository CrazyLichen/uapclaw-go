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

// TestHandleConfigCacheClear 验证 config.cache_clear 返回 ok=true。
func TestHandleConfigCacheClear(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodConfigCacheClear, json.RawMessage(`{}`))

	resp, err := s.handleConfigCacheClear(context.Background(), req)
	if err != nil {
		t.Fatalf("handleConfigCacheClear 返回错误: %v", err)
	}
	if ok, _ := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}

// TestHandleAgentReloadConfig 验证 agent.reload_config 返回 ok=true。
func TestHandleAgentReloadConfig(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentReloadConfig, json.RawMessage(`{}`))

	resp, err := s.handleAgentReloadConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentReloadConfig 返回错误: %v", err)
	}
	if ok, _ := resp.Payload["ok"]; ok != true {
		t.Errorf("payload.ok 应为 true, 实际: %v", ok)
	}
}
