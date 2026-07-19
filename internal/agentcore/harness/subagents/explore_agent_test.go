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

// TestBuildExploreAgentConfig_默认配置 测试所有默认值
func TestBuildExploreAgentConfig_默认配置(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildExploreAgentConfig(model, params)

	require.NotNil(t, cfg)
	assert.Equal(t, "explore_agent", cfg.AgentCard.GetName())
	assert.Empty(t, cfg.FactoryName, "ExploreAgent 不设 FactoryName（对齐 Python：走通用路径）")
	assert.Equal(t, 15, cfg.MaxIterations)
	assert.False(t, cfg.EnableTaskLoop)
	assert.False(t, cfg.RestrictToWorkDir, "ExploreAgent RestrictToWorkDir 默认应为 false")
	assert.Equal(t, model, cfg.Model)
}

// TestBuildExploreAgentConfig_CN提示词 测试中文提示词关键内容
func TestBuildExploreAgentConfig_CN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "cn"}

	cfg := BuildExploreAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "代码库导航专家")
	assert.Contains(t, cfg.SystemPrompt, "仅限只读操作")
	assert.Contains(t, cfg.SystemPrompt, "你没有写入类工具")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "代码库导航子代理")
}

// TestBuildExploreAgentConfig_EN提示词 测试英文提示词关键内容
func TestBuildExploreAgentConfig_EN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "en"}

	cfg := BuildExploreAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "codebase navigation specialist")
	assert.Contains(t, cfg.SystemPrompt, "READ-ONLY OPERATION")
	assert.Contains(t, cfg.SystemPrompt, "You have no write-capable tools")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "Codebase navigation agent")
}

// TestBuildExploreAgentConfig_自定义MaxIterations 测试覆盖默认值
func TestBuildExploreAgentConfig_自定义MaxIterations(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{MaxIterations: 25}

	cfg := BuildExploreAgentConfig(model, params)

	assert.Equal(t, 25, cfg.MaxIterations)
}

// TestBuildExploreAgentConfig_用户覆盖Card 测试用户提供的 AgentCard
func TestBuildExploreAgentConfig_用户覆盖Card(t *testing.T) {
	model := &llm.Model{}
	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("custom_explorer"),
		agentschema.WithAgentDescription("自定义探索助手"),
	)
	params := &hschema.SubagentCreateParams{Card: customCard}

	cfg := BuildExploreAgentConfig(model, params)

	assert.Equal(t, "custom_explorer", cfg.AgentCard.GetName())
	assert.Equal(t, "自定义探索助手", cfg.AgentCard.GetDescription())
}

// TestBuildExploreAgentConfig_用户覆盖SystemPrompt 测试用户提供的系统提示词
func TestBuildExploreAgentConfig_用户覆盖SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{SystemPrompt: "自定义提示词"}

	cfg := BuildExploreAgentConfig(model, params)

	assert.Equal(t, "自定义提示词", cfg.SystemPrompt)
}

// TestBuildExploreAgentConfig_RestrictToWorkDir_nil默认false 测试 RestrictToWorkDir 为 nil 时默认 false
func TestBuildExploreAgentConfig_RestrictToWorkDir_nil默认false(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{} // RestrictToWorkDir 为 nil

	cfg := BuildExploreAgentConfig(model, params)

	// nil 表示未设置，ExploreAgent 默认 false（对齐 Python: restrict_to_work_dir=False）
	assert.False(t, cfg.RestrictToWorkDir, "ExploreAgent RestrictToWorkDir 为 nil 时应默认 false")
}

// TestBuildExploreAgentConfig_RestrictToWorkDir_显式true 测试显式设置为 true
func TestBuildExploreAgentConfig_RestrictToWorkDir_显式true(t *testing.T) {
	model := &llm.Model{}
	restrictTrue := true
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictTrue}

	cfg := BuildExploreAgentConfig(model, params)

	assert.True(t, cfg.RestrictToWorkDir, "显式设置 true 时应为 true")
}

// TestBuildExploreAgentConfig_RestrictToWorkDir_显式false 测试显式设置为 false
func TestBuildExploreAgentConfig_RestrictToWorkDir_显式false(t *testing.T) {
	model := &llm.Model{}
	restrictFalse := false
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictFalse}

	cfg := BuildExploreAgentConfig(model, params)

	assert.False(t, cfg.RestrictToWorkDir, "显式设置 false 时应为 false")
}

// TestDefaultExploreAgentSystemPrompt 测试辅助函数
func TestDefaultExploreAgentSystemPrompt(t *testing.T) {
	assert.Contains(t, DefaultExploreAgentSystemPrompt("cn"), "代码库导航专家")
	assert.Contains(t, DefaultExploreAgentSystemPrompt("en"), "codebase navigation specialist")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultExploreAgentSystemPrompt("fr"), "代码库导航专家")
}

// TestDefaultExploreAgentDescription 测试辅助函数
func TestDefaultExploreAgentDescription(t *testing.T) {
	assert.Contains(t, DefaultExploreAgentDescription("cn"), "代码库导航子代理")
	assert.Contains(t, DefaultExploreAgentDescription("en"), "Codebase navigation agent")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultExploreAgentDescription("fr"), "代码库导航子代理")
}
