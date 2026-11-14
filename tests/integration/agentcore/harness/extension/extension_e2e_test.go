//go:build integration

package extension_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	harness "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	toolskillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionSuite 领域 15.15 插件/模板热加载端到端套件。
type ExtensionSuite struct {
	isuite.AgentSuite
}

// TestExtensionSuite 套件入口。
func TestExtensionSuite(t *testing.T) {
	suite.Run(t, new(ExtensionSuite))
}

// 端到端用例：真文件型 plugin 包 → loader → resolver → binder → DeepAgent 台账。

// writeFileFixture 落盘文本文件。
func writeFileFixture(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// writePluginFixture 落盘一个含 prompt_sections + skill leaf 的真文件型插件包，
// 返回包根目录。
func (s *ExtensionSuite) writePluginFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFileFixture(t, filepath.Join(dir, "manifest.json"), `{
		"package_type": "plugin",
		"id": "e2e-plugin",
		"name": "端到端插件",
		"prompt_sections": [{"name": "sops", "content": {"cn": "端到端守则", "en": "e2e sop"}, "priority": 20}]
	}`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "e2e-skill"), 0o755))
	writeFileFixture(t, filepath.Join(dir, "skills", "e2e-skill", "SKILL.md"), "# e2e skill")
	return dir
}

// newExtensionTestAgent 构造带 MockLLM 与 SkillUseRail 的 DeepAgent。
func (s *ExtensionSuite) newExtensionTestAgent(ctx context.Context) (*harness.DeepAgent, error) {
	// 热绑段落会对内层 ReActAgent 走 configure（校验 timeout>0），套件默认
	// ClientConfig 未设 Timeout 时补齐
	clientCfg := s.ClientConfig
	if clientCfg.Timeout <= 0 {
		clientCfg.Timeout = 30
		clientCfg.MaxRetries = 1
	}
	model, err := llm.NewModel(clientCfg, s.ModelConfig)
	require.NoError(s.T(), err)
	return s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Model: model, Skills: []string{},
	})
}

// TestLoadPlugin端到端_段落与技能挂载 真文件包 → 六入口 → 段落/技能绑定 →
// 台账入账 → 卸载逆序撤销 → 重复卸载无操作。
func (s *ExtensionSuite) TestLoadPlugin端到端_段落与技能挂载() {
	ctx := s.T().Context()
	agent, err := s.newExtensionTestAgent(ctx)
	require.NoError(s.T(), err)
	// 挂 SkillUseRail（bindSkill 的前置条件，factory enable_skill_discovery 等价）
	rail := toolskillrails.NewSkillUseRail(nil)
	require.NoError(s.T(), agent.RegisterRail(ctx, rail))

	pluginDir := s.writePluginFixture(s.T())
	record, err := agent.LoadPlugin(ctx, pluginDir)
	require.NoError(s.T(), err)
	assert.NotEmpty(s.T(), record.LoadID)
	assert.Equal(s.T(), pluginDir+string(filepath.Separator)+"manifest.json", record.SourceURI)

	// 段落绑定 + 台账
	assert.NotNil(s.T(), agent.SystemPromptBuilder().GetSection("sops"))
	require.Contains(s.T(), agent.LoadRecordsSnapshot(), record.LoadID)

	// 卸载闭环
	labels, err := agent.UnloadExtension(ctx, *record)
	require.NoError(s.T(), err)
	assert.NotEmpty(s.T(), labels)
	assert.Nil(s.T(), agent.SystemPromptBuilder().GetSection("sops"))
	assert.NotContains(s.T(), agent.LoadRecordsSnapshot(), record.LoadID)

	// 重复卸载无操作
	labels, err = agent.UnloadExtension(ctx, *record)
	require.NoError(s.T(), err)
	assert.Empty(s.T(), labels)
}

// Test插件热绑skill_存活于adapter技能刷新 热绑 skill 后 rail.SkillsDir 含包根，
// DeepAgentConfig.Skills 记账，刷新（adapter D-5 数据源同源）后仍可读出技能。
func (s *ExtensionSuite) Test插件热绑skill_存活于adapter技能刷新() {
	ctx := s.T().Context()
	agent, err := s.newExtensionTestAgent(ctx)
	require.NoError(s.T(), err)
	rail := toolskillrails.NewSkillUseRail(nil)
	require.NoError(s.T(), agent.RegisterRail(ctx, rail))

	// 内存 spec 热绑（路径必须绝对）：技能 leaf 目录
	pkg := s.T().TempDir()
	leaf := filepath.Join(pkg, "spec-skill")
	require.NoError(s.T(), os.MkdirAll(leaf, 0o755))
	writeFileFixture(s.T(), filepath.Join(leaf, "SKILL.md"), "# spec skill")

	spec := hschema.PluginSpec{
		ID:     "skill-plugin",
		Skills: []hschema.SkillSpec{{Dir: leaf, Mode: "all"}},
	}
	record, err := agent.LoadPluginSpec(ctx, spec)
	require.NoError(s.T(), err)
	require.Len(s.T(), record.Refs, 1)
	assert.Equal(s.T(), hres.ResourceKindSkill, record.Refs[0].Kind)

	// config.Skills 已记账（D-5 数据源）
	cfg := agent.DeepConfig()
	require.NotNil(s.T(), cfg)
	assert.Contains(s.T(), cfg.Skills, pkg, "包根应提交到 DeepAgentConfig.Skills")

	// 卸载后 config.Skills 出账
	_, err = agent.UnloadExtension(ctx, *record)
	require.NoError(s.T(), err)
	assert.NotContains(s.T(), agent.DeepConfig().Skills, pkg)
}

// Test失败回滚端到端 同段名冲突触发整批失败：已绑 refs 逆序撤销、台账不入账。
func (s *ExtensionSuite) Test失败回滚端到端() {
	ctx := s.T().Context()
	agent, err := s.newExtensionTestAgent(ctx)
	require.NoError(s.T(), err)

	// 第一次加载成功
	dir := s.writePluginFixture(s.T())
	first, err := agent.LoadPlugin(ctx, dir)
	require.NoError(s.T(), err)

	// 第二次加载同包：同段名（ReplaceExisting=false）冲突 → 整批失败
	_, err = agent.LoadPlugin(ctx, dir)
	require.Error(s.T(), err)

	// 第一次的绑定不受影响；失败批不入账
	assert.NotNil(s.T(), agent.SystemPromptBuilder().GetSection("sops"))
	snap := agent.LoadRecordsSnapshot()
	assert.Len(s.T(), snap, 1)
	assert.Contains(s.T(), snap, first.LoadID)

	// 回滚不产生残留台账
	_, err = agent.UnloadExtension(ctx, *first)
	require.NoError(s.T(), err)
	assert.Empty(s.T(), agent.LoadRecordsSnapshot())
}

// TestUnloadHarnessConfig_按路径卸载 deprecated 入口链路（load → unload by path）。
func (s *ExtensionSuite) TestUnloadHarnessConfig_按路径卸载() {
	ctx := s.T().Context()
	agent, err := s.newExtensionTestAgent(ctx)
	require.NoError(s.T(), err)

	dir := s.writePluginFixture(s.T())
	_, err = agent.LoadHarnessConfig(ctx, dir)
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), agent.SystemPromptBuilder().GetSection("sops"))

	labels, err := agent.UnloadHarnessConfig(ctx, dir)
	require.NoError(s.T(), err)
	assert.NotEmpty(s.T(), labels)
	assert.Nil(s.T(), agent.SystemPromptBuilder().GetSection("sops"))
}

// newSubagentCard 构造测试用子代理卡片。
func newSubagentCard(t *testing.T, name string) *agentschema.AgentCard {
	t.Helper()
	return agentschema.NewAgentCard(agentschema.WithAgentID(name), agentschema.WithAgentName(name))
}

// TestLoadAgentTemplateSpec_subagent物化与卸载 模板 spec 物化 subagent →
// SubagentRail 补装（ensureSubagentRailReady）→ 卸载出账。
func (s *ExtensionSuite) TestLoadAgentTemplateSpec_subagent物化与卸载() {
	ctx := s.T().Context()
	agent, err := s.newExtensionTestAgent(ctx)
	require.NoError(s.T(), err)

	spec := hschema.AgentTemplateSpec{
		AgentCard: *newSubagentCard(s.T(), "tpl-root"),
		Subagents: []hschema.AgentTemplateSpec{{
			AgentCard: *newSubagentCard(s.T(), "tpl-child"),
		}},
	}
	// 父模型取 agent 的 deep_config.model（LoadAgentTemplateSpec 内部注入）
	record, err := agent.LoadAgentTemplateSpec(ctx, spec)
	require.NoError(s.T(), err)
	assert.NotEmpty(s.T(), record.LoadID)

	// 卸载后 subagent 出账
	_, err = agent.UnloadExtension(ctx, *record)
	require.NoError(s.T(), err)
	assert.NotContains(s.T(), agent.LoadRecordsSnapshot(), record.LoadID)
}
