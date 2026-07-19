package subagents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildPlanAgentConfig_默认配置 测试所有默认值
func TestBuildPlanAgentConfig_默认配置(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildPlanAgentConfig(model, params)

	require.NotNil(t, cfg)
	assert.Equal(t, "plan_agent", cfg.AgentCard.GetName())
	assert.Empty(t, cfg.FactoryName, "PlanAgent 不设 FactoryName（对齐 Python：走通用路径）")
	assert.Equal(t, 25, cfg.MaxIterations)
	assert.False(t, cfg.EnableTaskLoop)
	assert.False(t, cfg.RestrictToWorkDir, "PlanAgent RestrictToWorkDir 默认应为 false")
	assert.Equal(t, model, cfg.Model)
}

// TestBuildPlanAgentConfig_CN提示词 测试中文提示词关键内容
func TestBuildPlanAgentConfig_CN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "cn"}

	cfg := BuildPlanAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "架构设计与规划专家")
	assert.Contains(t, cfg.SystemPrompt, "只读模式")
	assert.Contains(t, cfg.SystemPrompt, "Critical Files for Implementation")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "架构设计专家")
}

// TestBuildPlanAgentConfig_EN提示词 测试英文提示词关键内容
func TestBuildPlanAgentConfig_EN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "en"}

	cfg := BuildPlanAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "software architect and planning specialist")
	assert.Contains(t, cfg.SystemPrompt, "READ-ONLY MODE")
	assert.Contains(t, cfg.SystemPrompt, "Critical Files for Implementation")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "Architecture design specialist")
}

// TestBuildPlanAgentConfig_自定义MaxIterations 测试覆盖默认值
func TestBuildPlanAgentConfig_自定义MaxIterations(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{MaxIterations: 50}

	cfg := BuildPlanAgentConfig(model, params)

	assert.Equal(t, 50, cfg.MaxIterations)
}

// TestBuildPlanAgentConfig_用户覆盖Card 测试用户提供的 AgentCard
func TestBuildPlanAgentConfig_用户覆盖Card(t *testing.T) {
	model := &llm.Model{}
	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("custom_planner"),
		agentschema.WithAgentDescription("自定义规划助手"),
	)
	params := &hschema.SubagentCreateParams{Card: customCard}

	cfg := BuildPlanAgentConfig(model, params)

	assert.Equal(t, "custom_planner", cfg.AgentCard.GetName())
	assert.Equal(t, "自定义规划助手", cfg.AgentCard.GetDescription())
}

// TestBuildPlanAgentConfig_用户覆盖SystemPrompt 测试用户提供的系统提示词
func TestBuildPlanAgentConfig_用户覆盖SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{SystemPrompt: "自定义提示词"}

	cfg := BuildPlanAgentConfig(model, params)

	assert.Equal(t, "自定义提示词", cfg.SystemPrompt)
}

// TestBuildPlanAgentConfig_RestrictToWorkDir_nil默认false 测试 RestrictToWorkDir 为 nil 时默认 false
func TestBuildPlanAgentConfig_RestrictToWorkDir_nil默认false(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{} // RestrictToWorkDir 为 nil

	cfg := BuildPlanAgentConfig(model, params)

	// nil 表示未设置，PlanAgent 默认 false（区别于 ResearchAgent 的 true）
	assert.False(t, cfg.RestrictToWorkDir, "PlanAgent RestrictToWorkDir 为 nil 时应默认 false")
}

// TestBuildPlanAgentConfig_RestrictToWorkDir_显式true 测试显式设置为 true
func TestBuildPlanAgentConfig_RestrictToWorkDir_显式true(t *testing.T) {
	model := &llm.Model{}
	restrictTrue := true
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictTrue}

	cfg := BuildPlanAgentConfig(model, params)

	assert.True(t, cfg.RestrictToWorkDir, "显式设置 true 时应为 true")
}

// TestBuildPlanAgentConfig_RestrictToWorkDir_显式false 测试显式设置为 false
func TestBuildPlanAgentConfig_RestrictToWorkDir_显式false(t *testing.T) {
	model := &llm.Model{}
	restrictFalse := false
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictFalse}

	cfg := BuildPlanAgentConfig(model, params)

	assert.False(t, cfg.RestrictToWorkDir, "显式设置 false 时应为 false")
}

// TestDefaultPlanAgentSystemPrompt 测试辅助函数
func TestDefaultPlanAgentSystemPrompt(t *testing.T) {
	assert.Contains(t, DefaultPlanAgentSystemPrompt("cn"), "架构设计与规划专家")
	assert.Contains(t, DefaultPlanAgentSystemPrompt("en"), "software architect and planning specialist")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultPlanAgentSystemPrompt("fr"), "架构设计与规划专家")
}

// TestDefaultPlanAgentDescription 测试辅助函数
func TestDefaultPlanAgentDescription(t *testing.T) {
	assert.Contains(t, DefaultPlanAgentDescription("cn"), "架构设计专家")
	assert.Contains(t, DefaultPlanAgentDescription("en"), "Architecture design specialist")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultPlanAgentDescription("fr"), "架构设计专家")
}
