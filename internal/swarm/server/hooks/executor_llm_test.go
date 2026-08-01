//go:build llm

package hooks

import (
	"context"
	"testing"
)

// TestHookExecutor_queryLLM_真实调用 测试 queryLLM 真实 LLM 调用
// 运行方式: go test -tags=llm ./internal/swarm/server/hooks/... -v -run TestHookExecutor_queryLLM
// 需要: 真实 API Key 和网络连接
func TestHookExecutor_queryLLM_真实调用(t *testing.T) {
	cfg := LLMConfig{
		APIKey:         "",
		APIBase:        "",
		ClientProvider: "",
		DefaultModel:   "",
	}
	exec := NewHookExecutor(cfg)
	_, err := exec.queryLLM(context.Background(), "test prompt", "test-model")
	// 真实调用可能因缺少 API Key 而失败
	if err != nil {
		t.Logf("queryLLM failed (expected without real API key): %v", err)
	}
}

// TestHookExecutor_runPromptHook_真实LLM 测试 runPromptHook 真实 LLM 调用
// 运行方式: go test -tags=llm ./internal/swarm/server/hooks/... -v -run TestHookExecutor_runPromptHook
func TestHookExecutor_runPromptHook_真实LLM(t *testing.T) {
	cfg := LLMConfig{
		APIKey:         "",
		APIBase:        "",
		ClientProvider: "",
		DefaultModel:   "",
	}
	exec := NewHookExecutor(cfg)
	result := exec.runPromptHook(context.Background(), map[string]any{
		"type":    "prompt",
		"prompt":  "Is this safe?",
		"timeout": 10,
	}, map[string]any{"tool_name": "test"})
	t.Logf("runPromptHook result: outcome=%q, error=%q", result.Outcome, result.Error)
}
