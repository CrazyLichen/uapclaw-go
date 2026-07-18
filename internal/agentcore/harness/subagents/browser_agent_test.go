package subagents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	bm "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/browser_move"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildBrowserAgentConfig_默认配置 测试所有默认值
func TestBuildBrowserAgentConfig_默认配置(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildBrowserAgentConfig(model, params)

	require.NotNil(t, cfg)
	assert.Equal(t, "browser_agent", cfg.AgentCard.GetName())
	assert.Equal(t, BrowserAgentFactoryName, cfg.FactoryName)
	assert.Equal(t, 25, cfg.MaxIterations)
	assert.False(t, cfg.EnableTaskLoop)
	assert.True(t, cfg.RestrictToWorkDir)
	assert.Equal(t, model, cfg.Model)
	assert.NotNil(t, cfg.FactoryKwargs)
	assert.Contains(t, cfg.FactoryKwargs, "settings")
	_, ok := cfg.FactoryKwargs["settings"].(*bm.RuntimeSettings)
	assert.True(t, ok, "FactoryKwargs[\"settings\"] 应为 *bm.RuntimeSettings 类型")
}

// TestBuildBrowserAgentConfig_CN提示词 测试中文提示词
func TestBuildBrowserAgentConfig_CN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "cn"}

	cfg := BuildBrowserAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "浏览器自动化代理")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "Playwright MCP")
}

// TestBuildBrowserAgentConfig_EN提示词 测试英文提示词
func TestBuildBrowserAgentConfig_EN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "en"}

	cfg := BuildBrowserAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "browser automation agent")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "Dedicated browser subagent")
}

// TestBuildBrowserAgentConfig_自定义MaxIterations 测试覆盖默认值
func TestBuildBrowserAgentConfig_自定义MaxIterations(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{MaxIterations: 50}

	cfg := BuildBrowserAgentConfig(model, params)

	assert.Equal(t, 50, cfg.MaxIterations)
}

// TestBuildBrowserAgentConfig_用户覆盖Card 测试用户提供的 AgentCard
func TestBuildBrowserAgentConfig_用户覆盖Card(t *testing.T) {
	model := &llm.Model{}
	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("custom_browser"),
		agentschema.WithAgentDescription("自定义浏览器助手"),
	)
	params := &hschema.SubagentCreateParams{Card: customCard}

	cfg := BuildBrowserAgentConfig(model, params)

	assert.Equal(t, "custom_browser", cfg.AgentCard.GetName())
	assert.Equal(t, "自定义浏览器助手", cfg.AgentCard.GetDescription())
}

// TestBuildBrowserAgentConfig_用户覆盖SystemPrompt 测试用户提供的系统提示词
func TestBuildBrowserAgentConfig_用户覆盖SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{SystemPrompt: "自定义提示词"}

	cfg := BuildBrowserAgentConfig(model, params)

	assert.Equal(t, "自定义提示词", cfg.SystemPrompt)
}

// TestBuildBrowserAgentConfig_FactoryKwargs包含Settings 测试 FactoryKwargs 包含 settings
func TestBuildBrowserAgentConfig_FactoryKwargs包含Settings(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildBrowserAgentConfig(model, params)

	require.NotNil(t, cfg.FactoryKwargs)
	settings, ok := cfg.FactoryKwargs["settings"].(*bm.RuntimeSettings)
	assert.True(t, ok, "FactoryKwargs[\"settings\"] 应为 *bm.RuntimeSettings 类型")
	assert.NotNil(t, settings)
}

// TestDefaultBrowserAgentSystemPrompt 测试辅助函数
func TestDefaultBrowserAgentSystemPrompt(t *testing.T) {
	assert.Contains(t, DefaultBrowserAgentSystemPrompt("cn"), "浏览器自动化代理")
	assert.Contains(t, DefaultBrowserAgentSystemPrompt("en"), "browser automation agent")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultBrowserAgentSystemPrompt("fr"), "浏览器自动化代理")
}

// TestDefaultBrowserAgentDescription 测试辅助函数
func TestDefaultBrowserAgentDescription(t *testing.T) {
	assert.Contains(t, DefaultBrowserAgentDescription("cn"), "Playwright MCP")
	assert.Contains(t, DefaultBrowserAgentDescription("en"), "Dedicated browser subagent")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultBrowserAgentDescription("fr"), "Playwright MCP")
}

// TestBuildBrowserAgentConfig_RestrictToWorkDir_nil保持默认 测试 RestrictToWorkDir 为 nil 时保持默认 true
func TestBuildBrowserAgentConfig_RestrictToWorkDir_nil保持默认(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{} // RestrictToWorkDir 为 nil

	cfg := BuildBrowserAgentConfig(model, params)

	// nil 表示未设置，保持 NewSubAgentConfig 默认 true
	assert.True(t, cfg.RestrictToWorkDir, "RestrictToWorkDir 为 nil 时应保持默认 true")
}

// TestBuildBrowserAgentConfig_RestrictToWorkDir_显式false 测试显式设置为 false
func TestBuildBrowserAgentConfig_RestrictToWorkDir_显式false(t *testing.T) {
	model := &llm.Model{}
	restrictFalse := false
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictFalse}

	cfg := BuildBrowserAgentConfig(model, params)

	assert.False(t, cfg.RestrictToWorkDir, "显式设置 false 时应为 false")
}
