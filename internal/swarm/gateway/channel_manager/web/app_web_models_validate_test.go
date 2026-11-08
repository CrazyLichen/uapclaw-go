package web

import (
	"context"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestModelsValidate_必填校验 验证各必填字段缺失时返回 BAD_REQUEST 与明确文案。
func TestModelsValidate_必填校验(t *testing.T) {
	wc := newTestChannel()
	handler := modelsValidate(wc)

	tests := []struct {
		name    string
		params  map[string]any
		wantSub string
	}{
		{"params 为 nil", nil, "params 是必填项"},
		{"缺 api_base", map[string]any{"api_key": "k", "model": "m", "model_provider": "OpenAI"}, "api_base 是必填项"},
		{"缺 api_key", map[string]any{"api_base": "http://x", "model": "m", "model_provider": "OpenAI"}, "api_key 是必填项"},
		{"缺 model", map[string]any{"api_base": "http://x", "api_key": "k", "model_provider": "OpenAI"}, "model 是必填项"},
		{"缺 model_provider", map[string]any{"api_base": "http://x", "api_key": "k", "model": "m"}, "model_provider 是必填项"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := callHandler(handler, tt.params, "")
			if err == nil {
				t.Fatalf("应返回错误，实际通过")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("错误文案 %q 应含 %q", err.Error(), tt.wantSub)
			}
			if code := errorCode(err); code != WsErrBadRequest {
				t.Fatalf("错误码 = %q, 期望 %q", code, WsErrBadRequest)
			}
		})
	}
}

// TestModelsValidate_未知provider 验证 provider 不在白名单时被拒。
func TestModelsValidate_未知provider(t *testing.T) {
	wc := newTestChannel()
	handler := modelsValidate(wc)

	_, err := callHandler(handler, map[string]any{
		"api_base":       "http://x",
		"api_key":        "k",
		"model":          "m",
		"model_provider": "no-such-provider",
	}, "")
	if err == nil {
		t.Fatal("未知 provider 应被拒")
	}
	if !strings.Contains(err.Error(), "model_provider 必须是以下之一") {
		t.Fatalf("错误文案不符: %q", err.Error())
	}
	if code := errorCode(err); code != WsErrBadRequest {
		t.Fatalf("错误码 = %q, 期望 %q", code, WsErrBadRequest)
	}
}

// TestModelsValidate_合法provider 进入真实校验阶段 验证前置校验全通过后不再返回
// BAD_REQUEST（真实网络校验会失败，但错误不应是参数校验类）。
func TestModelsValidate_合法provider(t *testing.T) {
	wc := newTestChannel()
	handler := modelsValidate(wc)

	provider := ""
	for _, p := range availableModelProviders {
		provider = p
		break
	}
	if provider == "" {
		t.Skip("可用 provider 列表为空，跳过")
	}

	_, err := callHandler(handler, map[string]any{
		"api_base":       "http://127.0.0.1:1",
		"api_key":        "k",
		"model":          "m",
		"model_provider": provider,
	}, "")
	// 前置校验通过后进入真实调用；此处只要求不是"必填/白名单"类错误
	if err != nil {
		for _, sub := range []string{"是必填项", "必须是以下之一"} {
			if strings.Contains(err.Error(), sub) {
				t.Fatalf("合法参数不应触发参数校验错误: %v", err)
			}
		}
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestClearAgentConfigCache_NilClient 验证 agentClient 为 nil 时直接返回（不 panic）。
func TestClearAgentConfigCache_NilClient(t *testing.T) {
	clearAgentConfigCache(nil, context.Background())
}

// TestUpdateYAMLKeyInConfig_未知键 验证未知配置键报错而非静默写入。
func TestUpdateYAMLKeyInConfig_未知键(t *testing.T) {
	err := updateYAMLKeyInConfig("no-such-key", true)
	if err == nil || !strings.Contains(err.Error(), "未知的 YAML 配置键") {
		t.Fatalf("应报未知键，实际 %v", err)
	}
}
