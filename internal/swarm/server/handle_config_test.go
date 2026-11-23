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
	if ok := resp.Payload["ok"]; ok != true {
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
	if reloaded := resp.Payload["reloaded"]; reloaded != true {
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

// TestHandleConfigPanelRequest_MoaStatusFallback 验证 config.moa_status 走
// Python except 兜底形 {"configured": false}（moa_runtime 缺口 ⤵️ 登记见 27.4 行）。
func TestHandleConfigPanelRequest_MoaStatusFallback(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-moa", "web", schema.ReqMethodConfigMoaStatus, json.RawMessage(`{}`))

	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("handleConfigPanelRequest 返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("期望 ok=true")
	}
	if configured, ok := resp.Payload["configured"].(bool); !ok || configured {
		t.Fatalf("期望 configured=false（Python except 兜底形），got %v", resp.Payload["configured"])
	}
}

// TestHandleConfigPanelRequest_ZenEntriesAndWarm 验证 zen 两方法（Python
// config_adapter.py:191-210）：entries 回 {"models": [...]}，warm 回 {"ok": true}。
func TestHandleConfigPanelRequest_ZenEntriesAndWarm(t *testing.T) {
	s, _ := newTestServer()

	req := schema.NewAgentRequest("req-zen1", "web", schema.ReqMethodModelsZenEntries, json.RawMessage(`{}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("zen entries 返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("zen entries 期望 ok=true")
	}
	if models, ok := resp.Payload["models"].([]map[string]any); !ok || len(models) != 0 {
		t.Fatalf("空缓存期望 models 为空列表，got %T %v", resp.Payload["models"], resp.Payload["models"])
	}

	req = schema.NewAgentRequest("req-zen2", "web", schema.ReqMethodModelsZenWarm, json.RawMessage(`{}`))
	resp, err = s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("zen warm 返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("zen warm 期望 ok=true")
	}
	if okv, ok := resp.Payload["ok"].(bool); !ok || !okv {
		t.Fatalf("zen warm 期望 payload.ok=true，got %v", resp.Payload["ok"])
	}
}

// TestHandleConfigPanelRequest_ConfigGet 常规读链：无 config.yaml 环境下回面板
// 默认值；未知方法回 BAD_REQUEST（Python: config_service.py:579 尾部）。
func TestHandleConfigPanelRequest_ConfigGet(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-cg", "web", schema.ReqMethodConfigGet, json.RawMessage(`{}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("config.get 返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("config.get 期望 ok=true")
	}
	for _, key := range []string{"context_engine_enabled", "permissions_enabled", "swarmflow_enabled", "enable_free_models"} {
		if _, ok := resp.Payload[key]; !ok {
			t.Fatalf("面板缺键 %s", key)
		}
	}
}

// TestHandleConfigPanelRequest_未知方法BAD_REQUEST 验证 unsupported 分支
// （locale.set_conf 写链 Task5 接线前未注册 → BAD_REQUEST）。
func TestHandleConfigPanelRequest_未知方法BAD_REQUEST(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-bad", "web", schema.ReqMethodLocaleSetConf, json.RawMessage(`{}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if resp.OK {
		t.Fatalf("写链未注册时期望 ok=false（BAD_REQUEST）")
	}
	if code, _ := resp.Payload["code"].(string); code != "BAD_REQUEST" {
		t.Fatalf("期望 code=BAD_REQUEST，got %v", resp.Payload["code"])
	}
}
