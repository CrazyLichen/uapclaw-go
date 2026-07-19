package subagents

import (
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestBuildCodeAgentConfig_默认值 验证不传 Card/Prompt 时使用默认值
func TestBuildCodeAgentConfig_默认值(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model: model,
	}

	cfg := BuildCodeAgentConfig(model, params)

	if cfg == nil {
		t.Fatal("cfg 不应为 nil")
	}
	if cfg.AgentCard == nil {
		t.Fatal("AgentCard 不应为 nil")
	}
	if cfg.AgentCard.Name != "code_agent" {
		t.Errorf("AgentCard.Name 期望 %q，实际 %q", "code_agent", cfg.AgentCard.Name)
	}
	if cfg.SystemPrompt == "" {
		t.Error("SystemPrompt 不应为空")
	}
	if cfg.Model != model {
		t.Error("Model 应为传入的 model")
	}
	if cfg.FactoryName != "code_agent" {
		t.Errorf("FactoryName 期望 %q，实际 %q", "code_agent", cfg.FactoryName)
	}
	if cfg.MaxIterations != 15 {
		t.Errorf("MaxIterations 期望 15，实际 %d", cfg.MaxIterations)
	}
	if cfg.RestrictToWorkDir {
		t.Error("RestrictToWorkDir 默认应为 false")
	}
}

// TestBuildCodeAgentConfig_用户覆盖 验证传入自定义 Card/Prompt 时优先使用
func TestBuildCodeAgentConfig_用户覆盖(t *testing.T) {
	model := &llm.Model{}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("my_code"),
		agentschema.WithAgentDescription("自定义描述"),
	)
	params := &hschema.SubagentCreateParams{
		Model:        model,
		Card:         card,
		SystemPrompt: "自定义提示词",
	}

	cfg := BuildCodeAgentConfig(model, params)

	if cfg.AgentCard.Name != "my_code" {
		t.Errorf("AgentCard.Name 期望 %q，实际 %q", "my_code", cfg.AgentCard.Name)
	}
	if cfg.SystemPrompt != "自定义提示词" {
		t.Errorf("SystemPrompt 期望 %q，实际 %q", "自定义提示词", cfg.SystemPrompt)
	}
}

// TestBuildCodeAgentConfig_MaxIterations用户指定 验证用户指定 MaxIterations 时使用指定值
func TestBuildCodeAgentConfig_MaxIterations用户指定(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model:         model,
		MaxIterations: 30,
	}

	cfg := BuildCodeAgentConfig(model, params)

	if cfg.MaxIterations != 30 {
		t.Errorf("MaxIterations 期望 30，实际 %d", cfg.MaxIterations)
	}
}

// TestBuildCodeAgentConfig_RestrictToWorkDir显式设置 验证显式设置 RestrictToWorkDir
func TestBuildCodeAgentConfig_RestrictToWorkDir显式设置(t *testing.T) {
	model := &llm.Model{}
	t.Run("显式true", func(t *testing.T) {
		tr := true
		params := &hschema.SubagentCreateParams{
			Model:             model,
			RestrictToWorkDir: &tr,
		}
		cfg := BuildCodeAgentConfig(model, params)
		if !cfg.RestrictToWorkDir {
			t.Error("RestrictToWorkDir 应为 true")
		}
	})
	t.Run("显式false", func(t *testing.T) {
		fl := false
		params := &hschema.SubagentCreateParams{
			Model:             model,
			RestrictToWorkDir: &fl,
		}
		cfg := BuildCodeAgentConfig(model, params)
		if cfg.RestrictToWorkDir {
			t.Error("RestrictToWorkDir 应为 false")
		}
	})
}

// TestDefaultCodeAgentSystemPrompt_Cn 验证中文提示词非空
func TestDefaultCodeAgentSystemPrompt_Cn(t *testing.T) {
	s := DefaultCodeAgentSystemPrompt("cn")
	if s == "" {
		t.Error("中文提示词不应为空")
	}
}

// TestDefaultCodeAgentSystemPrompt_En 验证英文提示词非空
func TestDefaultCodeAgentSystemPrompt_En(t *testing.T) {
	s := DefaultCodeAgentSystemPrompt("en")
	if s == "" {
		t.Error("英文提示词不应为空")
	}
}

// TestDefaultCodeAgentSystemPrompt_未知语言回退 验证未知语言回退到中文
func TestDefaultCodeAgentSystemPrompt_未知语言回退(t *testing.T) {
	s := DefaultCodeAgentSystemPrompt("ja")
	cn := DefaultCodeAgentSystemPrompt("cn")
	if s != cn {
		t.Error("未知语言应回退到中文")
	}
}

// TestDefaultCodeAgentDescription_Cn 验证中文描述非空
func TestDefaultCodeAgentDescription_Cn(t *testing.T) {
	s := DefaultCodeAgentDescription("cn")
	if s == "" {
		t.Error("中文描述不应为空")
	}
}

// TestDefaultCodeAgentDescription_En 验证英文描述非空
func TestDefaultCodeAgentDescription_En(t *testing.T) {
	s := DefaultCodeAgentDescription("en")
	if s == "" {
		t.Error("英文描述不应为空")
	}
}
