package subagents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildVerificationAgentConfig_默认配置 测试所有默认值
func TestBuildVerificationAgentConfig_默认配置(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildVerificationAgentConfig(model, params)

	require.NotNil(t, cfg)
	assert.Equal(t, "verification_agent", cfg.AgentCard.GetName())
	assert.Empty(t, cfg.FactoryName, "VerificationAgent 不设 FactoryName（对齐 Python：走通用路径）")
	assert.Equal(t, 40, cfg.MaxIterations, "VerificationAgent MaxIterations 默认应为 40")
	assert.False(t, cfg.EnableTaskLoop)
	assert.False(t, cfg.RestrictToWorkDir, "VerificationAgent RestrictToWorkDir 默认应为 false")
	assert.Equal(t, model, cfg.Model)
}

// TestBuildVerificationAgentConfig_CN提示词 测试中文提示词关键内容
func TestBuildVerificationAgentConfig_CN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "cn"}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "对抗性验证专家")
	assert.Contains(t, cfg.SystemPrompt, "VERDICT: PASS")
	assert.Contains(t, cfg.SystemPrompt, "VERDICT: FAIL")
	assert.Contains(t, cfg.SystemPrompt, "VERDICT: PARTIAL")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "对抗性验证专家")
}

// TestBuildVerificationAgentConfig_EN提示词 测试英文提示词关键内容
func TestBuildVerificationAgentConfig_EN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "en"}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "adversarial verification specialist")
	assert.Contains(t, cfg.SystemPrompt, "VERDICT: PASS")
	assert.Contains(t, cfg.SystemPrompt, "VERDICT: FAIL")
	assert.Contains(t, cfg.SystemPrompt, "VERDICT: PARTIAL")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "Adversarial verification specialist")
}

// TestBuildVerificationAgentConfig_自定义MaxIterations 测试覆盖默认值
func TestBuildVerificationAgentConfig_自定义MaxIterations(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{MaxIterations: 60}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.Equal(t, 60, cfg.MaxIterations)
}

// TestBuildVerificationAgentConfig_用户覆盖Card 测试用户提供的 AgentCard
func TestBuildVerificationAgentConfig_用户覆盖Card(t *testing.T) {
	model := &llm.Model{}
	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("custom_verifier"),
		agentschema.WithAgentDescription("自定义验证助手"),
	)
	params := &hschema.SubagentCreateParams{Card: customCard}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.Equal(t, "custom_verifier", cfg.AgentCard.GetName())
	assert.Equal(t, "自定义验证助手", cfg.AgentCard.GetDescription())
}

// TestBuildVerificationAgentConfig_用户覆盖SystemPrompt 测试用户提供的系统提示词
func TestBuildVerificationAgentConfig_用户覆盖SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{SystemPrompt: "自定义验证提示词"}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.Equal(t, "自定义验证提示词", cfg.SystemPrompt)
}

// TestBuildVerificationAgentConfig_默认Rails 测试默认 Rails 包含 SysOperationRail + VerificationRail
func TestBuildVerificationAgentConfig_默认Rails(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{} // Rails 为 nil

	cfg := BuildVerificationAgentConfig(model, params)

	require.Len(t, cfg.Rails, 2, "默认应有 2 个 Rail")
	_, hasSysOp := cfg.Rails[0].(*rails.SysOperationRail)
	assert.True(t, hasSysOp, "第一个 Rail 应为 SysOperationRail")
	_, hasVerif := cfg.Rails[1].(*subagent.VerificationRail)
	assert.True(t, hasVerif, "第二个 Rail 应为 VerificationRail")
}

// TestBuildVerificationAgentConfig_用户覆盖Rails 测试用户指定 Rails 时不覆盖
func TestBuildVerificationAgentConfig_用户覆盖Rails(t *testing.T) {
	model := &llm.Model{}
	customRails := []sainterfaces.AgentRail{rails.NewSysOperationRail()}
	params := &hschema.SubagentCreateParams{Rails: customRails}

	cfg := BuildVerificationAgentConfig(model, params)

	require.Len(t, cfg.Rails, 1, "用户指定 Rails 时不应覆盖")
	_, hasSysOp := cfg.Rails[0].(*rails.SysOperationRail)
	assert.True(t, hasSysOp, "应保留用户的 SysOperationRail")
}

// TestBuildVerificationAgentConfig_RestrictToWorkDir_nil默认false 测试 RestrictToWorkDir 为 nil 时默认 false
func TestBuildVerificationAgentConfig_RestrictToWorkDir_nil默认false(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{} // RestrictToWorkDir 为 nil

	cfg := BuildVerificationAgentConfig(model, params)

	assert.False(t, cfg.RestrictToWorkDir, "VerificationAgent RestrictToWorkDir 为 nil 时应默认 false")
}

// TestBuildVerificationAgentConfig_RestrictToWorkDir_显式true 测试显式设置为 true
func TestBuildVerificationAgentConfig_RestrictToWorkDir_显式true(t *testing.T) {
	model := &llm.Model{}
	restrictTrue := true
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictTrue}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.True(t, cfg.RestrictToWorkDir, "显式设置 true 时应为 true")
}

// TestBuildVerificationAgentConfig_RestrictToWorkDir_显式false 测试显式设置为 false
func TestBuildVerificationAgentConfig_RestrictToWorkDir_显式false(t *testing.T) {
	model := &llm.Model{}
	restrictFalse := false
	params := &hschema.SubagentCreateParams{RestrictToWorkDir: &restrictFalse}

	cfg := BuildVerificationAgentConfig(model, params)

	assert.False(t, cfg.RestrictToWorkDir, "显式设置 false 时应为 false")
}

// TestDefaultVerificationAgentSystemPrompt 测试辅助函数
func TestDefaultVerificationAgentSystemPrompt(t *testing.T) {
	assert.Contains(t, DefaultVerificationAgentSystemPrompt("cn"), "对抗性验证专家")
	assert.Contains(t, DefaultVerificationAgentSystemPrompt("en"), "adversarial verification specialist")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultVerificationAgentSystemPrompt("fr"), "对抗性验证专家")
}

// TestDefaultVerificationAgentDescription 测试辅助函数
func TestDefaultVerificationAgentDescription(t *testing.T) {
	assert.Contains(t, DefaultVerificationAgentDescription("cn"), "对抗性验证专家")
	assert.Contains(t, DefaultVerificationAgentDescription("en"), "Adversarial verification specialist")
	// 未知语言回退到 cn
	assert.Contains(t, DefaultVerificationAgentDescription("fr"), "对抗性验证专家")
}
