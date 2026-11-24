package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// usePanelConfigDir 临时配置目录（同 control 包布局 <home>/.uapclaw/config）。
func usePanelConfigDir(t *testing.T, yamlContent string) {
	t.Helper()
	home := t.TempDir()
	pathutil.SetUserHome(home)
	t.Cleanup(func() { pathutil.SetUserHome("") })
	configDir := filepath.Join(home, ".uapclaw", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if yamlContent == "" {
		yamlContent = "{}\n"
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestHandleModelsList_ShapeAndZenAppend 验证 models.list 形状（Python :803）。
func TestHandleModelsList_ShapeAndZenAppend(t *testing.T) {
	usePanelConfigDir(t, `
models:
  defaults:
    - model_id: mid-1
      model_client_config:
        model_name: qwen-max
        api_base: http://x
        api_key: k1
        client_provider: OpenAI
      model_config_obj:
        temperature: 0.7
      is_default: true
`)
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-ml", "web", schema.ReqMethodModelsList, json.RawMessage(`{}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("models.list 失败: %v", err)
	}
	models, ok := resp.Payload["models"].([]map[string]any)
	// 27.4 内置智谱 key 落库后，models.list = 配置条目 + 内置免费档追加
	//（AppendZhipuFreeModels 恒展示）——形状断言聚焦配置首条。
	if !ok || len(models) != 2 {
		t.Fatalf("期望 2 条模型（配置 1 + 内置免费档 1），got %v", resp.Payload["models"])
	}
	m := models[0]
	if m["model_name"] != "qwen-max" || m["origin_index"] != 0 || m["is_default"] != true {
		t.Fatalf("首条字段不符: %v", m)
	}
	if m["has_api_key"] != true || m["model_provider"] != "OpenAI" {
		t.Fatalf("api 字段不符: %v", m)
	}
	if _, ok := resp.Payload["model_groups"]; !ok {
		t.Fatal("payload 缺 model_groups")
	}
	if resp.Payload["active_model"] != "qwen-max" {
		t.Fatalf("active_model 应为首条模型名，got %v", resp.Payload["active_model"])
	}
	// 末条为内置免费档（is_free 标记）
	last := models[len(models)-1]
	if last["model_name"] != "glm-4.5-flash" || last["is_free"] != true {
		t.Fatalf("末条应为内置免费档 glm-4.5-flash: %v", last)
	}
}

// TestHandleModelsValidate_ParamChecks 验证必填与 provider 校验文案逐字（Python :693-716）。
func TestHandleModelsValidate_ParamChecks(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-mv", "web", schema.ReqMethodModelsValidate,
		json.RawMessage(`{"api_base":"http://x","model":"m1","model_provider":"OpenAI"}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if resp.OK {
		t.Fatal("缺 api_key 非 OAuth provider 应 400")
	}
	if msg, _ := resp.Payload["error"].(string); !strings.Contains(msg, "api_base, model, model_provider, and api_key") {
		t.Fatalf("错误文案不符: %v", msg)
	}
	if code, _ := resp.Payload["code"].(string); code != "BAD_REQUEST" {
		t.Fatalf("期望 BAD_REQUEST，got %v", code)
	}
	req2 := schema.NewAgentRequest("req-mv2", "web", schema.ReqMethodModelsValidate,
		json.RawMessage(`{"api_base":"http://x","api_key":"k","model":"m1","model_provider":"Nope"}`))
	resp2, _ := s.handleConfigPanelRequest(context.Background(), req2)
	if msg, _ := resp2.Payload["error"].(string); !strings.Contains(msg, "Model provider must be one of") {
		t.Fatalf("provider 校验文案不符: %v", msg)
	}
}

// TestHandleModelsReplaceAll_NewEntry 新条目整体替换（Python :911）。
func TestHandleModelsReplaceAll_NewEntry(t *testing.T) {
	usePanelConfigDir(t, "{}")
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-mr", "web", schema.ReqMethodModelsReplaceAll,
		json.RawMessage(`{"models":[{"model_name":"m-new","api_base":"http://y","api_key":"sk-9","model_provider":"OpenAI"}]}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !resp.OK {
		t.Fatalf("期望成功，got %v", resp.Payload)
	}
	if resp.Payload["count"] != 1 {
		t.Fatalf("期望 count=1，got %v", resp.Payload["count"])
	}
	if resp.Payload["applied_without_restart"] != true {
		t.Fatal("force 重载应 applied_without_restart=true")
	}
}

// TestHandleModelsReplaceAll_Validation 缺 api_key / 非法 provider 报 400（Python :539/:542）。
func TestHandleModelsReplaceAll_Validation(t *testing.T) {
	usePanelConfigDir(t, "{}")
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-mr2", "web", schema.ReqMethodModelsReplaceAll,
		json.RawMessage(`{"models":[{"model_name":"m","api_base":"http://y","model_provider":"OpenAI"}]}`))
	resp, _ := s.handleConfigPanelRequest(context.Background(), req)
	if resp.OK {
		t.Fatal("缺 api_key 新条目应 400")
	}
	if msg, _ := resp.Payload["error"].(string); !strings.Contains(msg, "api_key is required") {
		t.Fatalf("文案不符: %v", msg)
	}
}

// TestHandleModelsReplaceAll_MergePreservesPlaceholder origin 命中保占位符（Python :337）。
func TestHandleModelsReplaceAll_MergePreservesPlaceholder(t *testing.T) {
	usePanelConfigDir(t, `
models:
  defaults:
    - model_id: mid-1
      model_client_config:
        model_name: qwen-max
        api_base: http://x
        api_key: ${API_KEY}
        client_provider: ${MODEL_PROVIDER}
      is_default: true
`)
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-mr3", "web", schema.ReqMethodModelsReplaceAll,
		json.RawMessage(`{"models":[{"model_id":"mid-1","model_name":"qwen-max","api_base":"http://x","api_key":"${API_KEY}","model_provider":"OpenAI","origin_index":0,"is_default":true}]}`))
	resp, err := s.handleConfigPanelRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !resp.OK {
		t.Fatalf("期望成功，got %v", resp.Payload)
	}
}
