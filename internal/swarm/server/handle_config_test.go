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

// TestHandleAgentReloadConfig 验证 agent.reload_config 正常返回。
func TestHandleAgentReloadConfig(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodAgentReloadConfig, json.RawMessage(`{}`))

	resp, err := s.handleAgentReloadConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentReloadConfig 返回错误: %v", err)
	}
	if !resp.OK {
		t.Errorf("期望 ok=true，实际 false")
	}
	if reloaded, _ := resp.Payload["reloaded"]; reloaded != true {
		t.Errorf("期望 payload.reloaded=true，实际 %v", reloaded)
	}
}

// TestHandleAgentReloadConfig_带参数 验证 agent.reload_config 带配置参数。
func TestHandleAgentReloadConfig_带参数(t *testing.T) {
	s, _ := newTestServer()
	params := json.RawMessage(`{"config": {"models": {}}, "env": {"MODEL_PROVIDER": "openai"}}`)
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodAgentReloadConfig, params)

	resp, err := s.handleAgentReloadConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentReloadConfig 返回错误: %v", err)
	}
	if !resp.OK {
		t.Errorf("期望 ok=true，实际 false，payload: %v", resp.Payload)
	}
}
