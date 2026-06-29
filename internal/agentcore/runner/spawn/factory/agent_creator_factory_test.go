package factory

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewDefaultAgentCreator(t *testing.T) {
	creator := NewDefaultAgentCreator()
	if creator == nil {
		t.Fatal("期望创建器非 nil")
	}
}

func TestSupportedAgentTypes(t *testing.T) {
	types := SupportedAgentTypes()
	if len(types) == 0 {
		t.Fatal("期望至少支持一种 Agent 类型")
	}
	found := false
	for _, tp := range types {
		if tp == AgentTypeReAct {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("期望支持 %s，实际 %v", AgentTypeReAct, types)
	}
}

func TestDefaultAgentCreator_CreateByType_ReAct(t *testing.T) {
	creator := NewDefaultAgentCreator()
	ctx := context.Background()

	agentCard := map[string]any{
		"name":        "test_agent",
		"description": "测试 Agent",
	}

	initKwargs := map[string]any{
		"model_name":     "qwen-max",
		"model_provider": "dashscope",
		"max_iterations": float64(5),
	}

	agent, err := creator.CreateByType(ctx, AgentTypeReAct, agentCard, initKwargs)
	if err != nil {
		t.Fatalf("CreateByType 失败: %v", err)
	}
	if agent == nil {
		t.Fatal("期望 agent 非 nil")
	}
}

func TestDefaultAgentCreator_CreateByType_ReAct_空InitKwargs(t *testing.T) {
	creator := NewDefaultAgentCreator()
	ctx := context.Background()

	agentCard := map[string]any{
		"name": "test_agent",
	}

	agent, err := creator.CreateByType(ctx, AgentTypeReAct, agentCard, nil)
	if err != nil {
		t.Fatalf("CreateByType 失败: %v", err)
	}
	if agent == nil {
		t.Fatal("期望 agent 非 nil")
	}
}

func TestDefaultAgentCreator_CreateByType_不支持的类型(t *testing.T) {
	creator := NewDefaultAgentCreator()
	ctx := context.Background()

	agentCard := map[string]any{
		"name": "test_agent",
	}

	_, err := creator.CreateByType(ctx, "unknown_agent", agentCard, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestBuildReActAgentConfig(t *testing.T) {
	kwargs := map[string]any{
		"model_name":           "qwen-max",
		"model_provider":       "dashscope",
		"api_key":              "test-key",
		"api_base":             "https://api.example.com",
		"max_iterations":       float64(10),
		"prompt_template_name": "default",
	}

	cfg := buildReActAgentConfig(kwargs)
	if cfg == nil {
		t.Fatal("期望 config 非 nil")
	}
	if cfg.ModelNameVal != "qwen-max" {
		t.Errorf("期望 ModelNameVal = qwen-max，实际 %s", cfg.ModelNameVal)
	}
	if cfg.MaxIterations != 10 {
		t.Errorf("期望 MaxIterations = 10，实际 %d", cfg.MaxIterations)
	}
}

func TestBuildReActAgentConfig_空Kwargs(t *testing.T) {
	cfg := buildReActAgentConfig(nil)
	if cfg == nil {
		t.Fatal("期望 config 非 nil")
	}
}
