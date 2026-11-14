package harness

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"

	toolskillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// 15.15 DeepAgent 六入口测试（deep_agent.py:2211-2453）。

// writeFile 落盘文本文件。
func writeFileExt(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// writePromptOnlyPlugin 在 dir 写一个仅含 prompt_sections 的插件包。
func writePromptOnlyPlugin(t *testing.T, dir string) string {
	t.Helper()
	manifest := filepath.Join(dir, "manifest.json")
	writeFileExt(t, manifest, `{
		"package_type": "plugin",
		"id": "it-p1",
		"name": "热载插件",
		"prompt_sections": [{"name": "sops", "content": {"cn": "守则内容"}, "priority": 20}]
	}`)
	return manifest
}

// newFullTestAgent 构造带模型与 SkillUseRail 的测试 agent。
func newFullTestAgent(t *testing.T) *DeepAgent {
	t.Helper()
	agent := newBoundTestAgent(t)
	rail := toolskillrails.NewSkillUseRail(nil)
	require.NoError(t, agent.RegisterRail(context.Background(), rail))
	return agent
}

// TestLoadPlugin_文件包端到端 manifest.json 插件热加载：段落绑定 + 台账入账 + rail 注册数锚点。
func TestLoadPlugin_文件包端到端(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	railsBefore := len(agent.FindRailsByType(reflect.TypeOf(&toolskillrails.SkillUseRail{})))

	dir := t.TempDir()
	manifest := writePromptOnlyPlugin(t, dir)

	record, err := agent.LoadPlugin(ctx, dir)
	require.NoError(t, err)
	assert.NotEmpty(t, record.LoadID)
	assert.Equal(t, manifest, record.SourceURI)
	labels := LoadRecordLabels(*record)
	require.Len(t, labels, 1)
	assert.Contains(t, labels[0], "prompt_section:sops")
	// 段落已进入 builder
	assert.NotNil(t, agent.SystemPromptBuilder().GetSection("sops"))
	// rail 注册数锚点：无 rail 声明的插件不改变注册数
	assert.Len(t, agent.FindRailsByType(reflect.TypeOf(&toolskillrails.SkillUseRail{})), railsBefore)
	// 台账入账
	require.Contains(t, agent.LoadRecordsSnapshot(), record.LoadID)
}

// TestLoadPlugin_重复加载同名段落报错 同段名（ReplaceExisting=false）第二次 load 报错。
func TestLoadPlugin_重复加载同名段落报错(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	dir := t.TempDir()
	writePromptOnlyPlugin(t, dir)

	_, err := agent.LoadPlugin(ctx, dir)
	require.NoError(t, err)
	_, err = agent.LoadPlugin(ctx, dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Prompt section already bound")
}

// TestLoadPluginSpec_路径非绝对报错 spec 带相对路径时 resolve 拒绝。
// Python: load_plugin_spec 的 _require_absolute_path 语义（:2259-2279）。
func TestLoadPluginSpec_路径非绝对报错(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	spec := hschema.PluginSpec{
		ID:     "p-rel",
		Skills: []hschema.SkillSpec{{Dir: "relative/skills/a"}},
	}
	_, err := agent.LoadPluginSpec(ctx, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

// TestLoadAgentTemplateSpec_activeTemplate记账 spec 加载后 activeAgentTemplate 记账。
func TestLoadAgentTemplateSpec_activeTemplate记账(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	card := agentschema.NewAgentCard(agentschema.WithAgentName("tpl-root"))
	spec := hschema.AgentTemplateSpec{AgentCard: *card}

	record, err := agent.LoadAgentTemplateSpec(ctx, spec)
	require.NoError(t, err)
	assert.NotEmpty(t, record.LoadID)

	assertActiveTemplate(t, agent, record.LoadID)
}

// assertActiveTemplate 断言台账含 load_id 且 activeAgentTemplate 指向它。
func assertActiveTemplate(t *testing.T, agent *DeepAgent, loadID string) {
	t.Helper()
	require.Contains(t, agent.LoadRecordsSnapshot(), loadID)
	agent.loadRecordsMu.Lock()
	defer agent.loadRecordsMu.Unlock()
	require.NotNil(t, agent.activeAgentTemplate)
	assert.Equal(t, loadID, agent.activeAgentTemplate.LoadID)
}

// TestUnloadExtension_未知LoadID无操作 对齐 Python :2371-2373。
func TestUnloadExtension_未知LoadID无操作(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	labels, err := agent.UnloadExtension(ctx, hres.LoadRecord{LoadID: "missing"})
	require.NoError(t, err)
	assert.Empty(t, labels)
}

// TestUnloadExtension_卸载闭环 load → unload：段落移除、台账出账、重复卸载无操作。
func TestUnloadExtension_卸载闭环(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	dir := t.TempDir()
	writePromptOnlyPlugin(t, dir)

	record, err := agent.LoadPlugin(ctx, dir)
	require.NoError(t, err)

	labels, err := agent.UnloadExtension(ctx, *record)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	assert.Nil(t, agent.SystemPromptBuilder().GetSection("sops"))
	assert.NotContains(t, agent.LoadRecordsSnapshot(), record.LoadID)

	// 重复卸载：无操作
	labels, err = agent.UnloadExtension(ctx, *record)
	require.NoError(t, err)
	assert.Empty(t, labels)
}

// TestLoadHarnessConfig_deprecated转调 deprecated 入口与 LoadPlugin 行为一致。
// Python: load_harness_config (deep_agent.py:2387-2405)。
func TestLoadHarnessConfig_deprecated转调(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	dir := t.TempDir()
	writePromptOnlyPlugin(t, dir)

	labels, err := agent.LoadHarnessConfig(ctx, dir)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	assert.NotNil(t, agent.SystemPromptBuilder().GetSection("sops"))
}

// TestUnloadHarnessConfig_按sourceURI找账 对齐 Python unload_harness_config。
func TestUnloadHarnessConfig_按sourceURI找账(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	dir := t.TempDir()
	writePromptOnlyPlugin(t, dir)

	_, err := agent.LoadHarnessConfig(ctx, dir)
	require.NoError(t, err)
	labels, err := agent.UnloadHarnessConfig(ctx, dir)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	assert.Nil(t, agent.SystemPromptBuilder().GetSection("sops"))

	// 无匹配路径：返回空列表（缺失即无操作）
	labels, err = agent.UnloadHarnessConfig(ctx, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, labels)
}

// TestLoadPluginAbility_预构建能力 对齐 Python :2337-2365。
func TestLoadPluginAbility_预构建能力(t *testing.T) {
	ctx := context.Background()
	agent := newFullTestAgent(t)
	card := newTestToolCard("ability_tool")

	record, err := agent.LoadPluginAbility(ctx, []tool.ToolResource{{Card: card}}, nil, nil)
	require.NoError(t, err)
	require.Len(t, record.Refs, 1)
	assert.Equal(t, hres.ResourceKindTool, record.Refs[0].Kind)
	assert.NotNil(t, agent.AbilityManager().Get("ability_tool"))

	// 卸载
	_, err = agent.UnloadExtension(ctx, *record)
	require.NoError(t, err)
	assert.Nil(t, agent.AbilityManager().Get("ability_tool"))
}
