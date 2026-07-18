package subagents

import (
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildResearchAgentConfig_默认配置 测试所有默认值
func TestBuildResearchAgentConfig_默认配置(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildResearchAgentConfig(model, params)

	require.NotNil(t, cfg)
	assert.Equal(t, "research_agent", cfg.AgentCard.GetName())
	assert.Equal(t, "research_agent", cfg.FactoryName)
	assert.Equal(t, 15, cfg.MaxIterations)
	assert.False(t, cfg.EnableTaskLoop)
	assert.True(t, cfg.RestrictToWorkDir)
	assert.Equal(t, model, cfg.Model)
}

// TestBuildResearchAgentConfig_CN提示词 测试中文提示词
func TestBuildResearchAgentConfig_CN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "cn"}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "研究助理")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "研究调查")
}

// TestBuildResearchAgentConfig_EN提示词 测试英文提示词
func TestBuildResearchAgentConfig_EN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "en"}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "research assistant")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "research and investigation")
}

// TestBuildResearchAgentConfig_自定义MaxIterations 测试覆盖默认值
func TestBuildResearchAgentConfig_自定义MaxIterations(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{MaxIterations: 25}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Equal(t, 25, cfg.MaxIterations)
}

// TestBuildResearchAgentConfig_用户覆盖Card 测试用户提供的 AgentCard
func TestBuildResearchAgentConfig_用户覆盖Card(t *testing.T) {
	model := &llm.Model{}
	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("custom_researcher"),
		agentschema.WithAgentDescription("自定义研究助手"),
	)
	params := &hschema.SubagentCreateParams{Card: customCard}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Equal(t, "custom_researcher", cfg.AgentCard.GetName())
	assert.Equal(t, "自定义研究助手", cfg.AgentCard.GetDescription())
}

// TestBuildResearchAgentConfig_用户覆盖SystemPrompt 测试用户提供的系统提示词
func TestBuildResearchAgentConfig_用户覆盖SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{SystemPrompt: "自定义提示词"}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Equal(t, "自定义提示词", cfg.SystemPrompt)
}

// TestDefaultResearchAgentSystemPrompt 测试辅助函数
func TestDefaultResearchAgentSystemPrompt(t *testing.T) {
	assert.Contains(t, DefaultResearchAgentSystemPrompt("cn"), "研究助理")
	assert.Contains(t, DefaultResearchAgentSystemPrompt("en"), "research assistant")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultResearchAgentSystemPrompt("fr"), "研究助理")
}

// TestDefaultResearchAgentDescription 测试辅助函数
func TestDefaultResearchAgentDescription(t *testing.T) {
	assert.Contains(t, DefaultResearchAgentDescription("cn"), "研究调查")
	assert.Contains(t, DefaultResearchAgentDescription("en"), "research and investigation")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultResearchAgentDescription("fr"), "研究调查")
}
