package harness

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	toolskillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	subagentrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 辅助构造 ────────────────────────────

// newBoundTestAgent 构造带 DeepAgentConfig 的测试 agent（binder 依赖 requireDeepConfig）。
func newBoundTestAgent(t *testing.T) *DeepAgent {
	t.Helper()
	agent := newTestDeepAgent()
	// 提供 model_name 非空的模型配置：apply_prompt_builder_to_react_agent 会
	// 对内层 ReActAgent 走 configure（校验 ModelNameVal）。
	model, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientID: "test-client", ClientProvider: "OpenAI", APIKey: "test-key", APIBase: "http://localhost:0/v1", Timeout: 30, MaxRetries: 1,
	}, &llmschema.ModelRequestConfig{ModelName: "test-model"})
	require.NoError(t, err)
	cfg := &hschema.DeepAgentConfig{Card: agent.Card(), Model: model}
	cfg.Language = "cn"
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))
	return agent
}

// newTestToolCard 构造测试工具卡片。
func newTestToolCard(name string) *tool.ToolCard {
	return tool.NewToolCard(name, "测试工具", nil, nil)
}

// newPromptSection 构造测试提示词段落。
func newPromptSection(name, text string) saprompts.PromptSection {
	return saprompts.NewPromptSection(name, map[string]string{"cn": text, "en": text}, 10)
}

// stubRail 满足 AgentRail 的最小桩。
type stubRail struct {
	agentinterfaces.AgentRail
	name string
}

func (s *stubRail) Init(context.Context, agentinterfaces.BaseAgent) error { return nil }
func (s *stubRail) Uninit(agentinterfaces.BaseAgent) error                { return nil }
func (s *stubRail) Name() string                                          { return s.name }
func (s *stubRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}

// ──────────────────────────── 绑定测试 ────────────────────────────

// TestBindTool_ToolCard绑定与config记账 对齐 Python :92-109。
func TestBindTool_ToolCard绑定与config记账(t *testing.T) {
	agent := newBoundTestAgent(t)
	card := newTestToolCard("ext_tool")

	ref, err := bindTool(agent, tool.ToolResource{Card: card})
	require.NoError(t, err)
	assert.Equal(t, hres.ResourceKindTool, ref.Kind)
	assert.NotEmpty(t, ref.Identity)
	assert.Equal(t, []string{"ext_tool"}, ref.Extra["ability_names"])
	assert.NotNil(t, agent.AbilityManager().Get("ext_tool"))
	require.NotNil(t, agent.DeepConfig())
	require.Len(t, agent.DeepConfig().Tools, 1)
}

// TestBindTool_同名冲突报错 对齐 Python :95-97。
func TestBindTool_同名冲突报错(t *testing.T) {
	agent := newBoundTestAgent(t)
	card := newTestToolCard("dup_tool")
	_, err := bindTool(agent, tool.ToolResource{Card: card})
	require.NoError(t, err)
	_, err = bindTool(agent, tool.ToolResource{Card: card})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Tool already bound")
}

// TestBindTool_非法资源类型报错 对齐 Python :93-94。
func TestBindTool_非法资源类型报错(t *testing.T) {
	agent := newBoundTestAgent(t)
	_, err := bindTool(agent, tool.ToolResource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported tool resource")
}

// TestBindRail_成功与同名冲突 对齐 Python :150-161。
func TestBindRail_成功与同名冲突(t *testing.T) {
	ctx := context.Background()
	agent := newBoundTestAgent(t)
	rail := &stubRail{name: "stub"}

	ref, err := bindRail(ctx, agent, rail)
	require.NoError(t, err)
	assert.Equal(t, hres.ResourceKindRail, ref.Kind)
	assert.NotEmpty(t, ref.Identity)

	_, err = bindRail(ctx, agent, rail)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Rail already bound")
}

// TestBindPromptSection_新增与冲突 对齐 Python :164-184。
func TestBindPromptSection_新增与冲突(t *testing.T) {
	ctx := context.Background()
	agent := newTestDeepAgent()
	// builder 为 nil（未配置）时 bindPromptSection 应报错
	_, err := bindPromptSection(ctx, agent, hres.ResolvedPromptSection{
		Section: newPromptSection("sop", "内容"),
	})
	require.Error(t, err, "未 configure() 时 builder 为 nil，应报错")
}

// TestBindPromptSection_ReplaceExisting快照 builder 就绪时替换并快照。
func TestBindPromptSection_ReplaceExisting快照(t *testing.T) {
	ctx := context.Background()
	agent := newBoundTestAgent(t)
	builder := agent.SystemPromptBuilder()
	require.NotNil(t, builder, "ConfigureDeepConfig 后应有 builder")
	builder.AddSection(newPromptSection("identity", "原身份"))

	// Plugin 段（不替换）遇同名应报错
	_, err := bindPromptSection(ctx, agent, hres.ResolvedPromptSection{
		Section: newPromptSection("identity", "插件身份"), ReplaceExisting: false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Prompt section already bound")

	// AgentTemplate 根段（替换）应成功并带快照
	ref, err := bindPromptSection(ctx, agent, hres.ResolvedPromptSection{
		Section: newPromptSection("identity", "模板身份"), ReplaceExisting: true,
	})
	require.NoError(t, err)
	assert.Equal(t, hres.ResourceKindPromptSection, ref.Kind)
	assert.Equal(t, true, ref.Extra["previous_exists"])
	snapshot, _ := ref.Extra["previous_snapshot"].(map[string]any)
	require.NotNil(t, snapshot)
	assert.Equal(t, "identity", snapshot["name"])
}

// TestSkillPathsToRailMounts_leaf取父目录 对齐 Python :186-202。
func TestSkillPathsToRailMounts_leaf取父目录(t *testing.T) {
	dir := t.TempDir()
	leaf := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(leaf, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(leaf, "SKILL.md"), []byte("# s"), 0o644))
	packageDir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(packageDir, 0o755))

	roots, names := skillPathsToRailMounts([]string{leaf, packageDir, leaf})
	assert.Equal(t, []string{dir, packageDir}, roots)
	assert.Equal(t, []string{"my-skill"}, names)
}

// TestApplyExtensionHot_回滚 中途失败对已绑 refs 逆序撤销。
// 对齐 Python :27-62 的 except 分支。
func TestApplyExtensionHot_回滚(t *testing.T) {
	ctx := context.Background()
	agent := newBoundTestAgent(t)
	card := newTestToolCard("rollback_tool")

	parts := hres.NewExtensionParts()
	parts.Tools = append(parts.Tools, tool.ToolResource{Card: card})
	// 第二步放同名 rail 冲突资源：先绑一次制造冲突
	conflict := &stubRail{name: "stub-conflict"}
	require.NoError(t, agent.RegisterRail(ctx, conflict))
	parts.Rails = append(parts.Rails, conflict)

	_, err := ApplyExtensionHot(ctx, agent, parts)
	require.Error(t, err)
	// 回滚后工具能力被撤销
	assert.Nil(t, agent.AbilityManager().Get("rollback_tool"))
	// config.Tools 也被清空
	assert.Empty(t, agent.DeepConfig().Tools)
}

// TestUnbindTool 对齐 Python :344-353。
func TestUnbindTool(t *testing.T) {
	agent := newBoundTestAgent(t)
	card := newTestToolCard("unbind_tool")
	ref, err := bindTool(agent, tool.ToolResource{Card: card})
	require.NoError(t, err)

	require.NoError(t, unbind(context.Background(), agent, ref))
	assert.Nil(t, agent.AbilityManager().Get("unbind_tool"))
	assert.Empty(t, agent.DeepConfig().Tools)
}

// TestEnsureSubagentRailReady_无subagent返回nil 对齐 Python :316-318。
func TestEnsureSubagentRailReady_无subagent返回nil(t *testing.T) {
	agent := newBoundTestAgent(t)
	ref, err := ensureSubagentRailReady(context.Background(), agent)
	require.NoError(t, err)
	assert.Nil(t, ref)
}

// TestEnsureSubagentRailReady_补装rail 对齐 Python :320-339。
func TestEnsureSubagentRailReady_补装rail(t *testing.T) {
	agent := newBoundTestAgent(t)
	sub := &hschema.SubAgentConfig{
		AgentCard: agentschema.NewAgentCard(agentschema.WithAgentName("child-a")),
	}
	ref, err := bindSubagent(agent, sub)
	require.NoError(t, err)
	assert.Equal(t, hres.ResourceKindSubagent, ref.Kind)

	ensureRef, err := ensureSubagentRailReady(context.Background(), agent)
	require.NoError(t, err)
	require.NotNil(t, ensureRef)
	assert.Equal(t, hres.ResourceKindRail, ensureRef.Kind)
	assert.Contains(t, ensureRef.Identity, "SubagentRail")
	// rail 已注册
	rails := agent.FindRailsByType(subagentRailType())
	require.Len(t, rails, 1)
}

// TestUnbindSubagent 对齐 Python :457-466。
func TestUnbindSubagent(t *testing.T) {
	agent := newBoundTestAgent(t)
	sub := &hschema.SubAgentConfig{
		AgentCard: agentschema.NewAgentCard(agentschema.WithAgentName("child-b")),
	}
	ref, err := bindSubagent(agent, sub)
	require.NoError(t, err)

	require.NoError(t, unbind(context.Background(), agent, ref))
	assert.Nil(t, agent.AbilityManager().Get("child-b"))
	assert.Empty(t, agent.DeepConfig().Subagents)
}

// TestUnbindRail_已注册走unregister_pending走remove 对齐 Python :390-396。
func TestUnbindRail_已注册走unregister_pending走remove(t *testing.T) {
	ctx := context.Background()
	agent := newBoundTestAgent(t)
	rail := &stubRail{name: "unbind-rail"}
	ref, err := bindRail(ctx, agent, rail)
	require.NoError(t, err)
	require.NoError(t, unbind(ctx, agent, ref))
	assert.Nil(t, agent.FindRailByName(ref.Identity))

	// pending rail：只排队未注册
	pending := &stubRail{name: "pending-rail"}
	agent.AddRail(pending)
	require.NoError(t, unbind(ctx, agent, hres.NewResourceRef(hres.ResourceKindRail, "pending-rail")))
	assert.Nil(t, agent.FindRailByName("pending-rail"))
}

// TestUnbind_未知kind报错 对齐 Python :463-465。
func TestUnbind_未知kind报错(t *testing.T) {
	agent := newBoundTestAgent(t)
	err := unbind(context.Background(), agent, hres.ResourceRef{Kind: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported hot unbind")
}

// TestSkillUseRailAccessor_快照与摘除 访问器语义（bind/unbind skill 依赖）。
func TestSkillUseRailAccessor_快照与摘除(t *testing.T) {
	rail := toolskillrails.NewSkillUseRail(nil)
	rail.SetSkillsDir([]string{"/a", "/b"})
	assert.Equal(t, []string{"/a", "/b"}, rail.SkillsDir())

	rail.SetEnableCache(false)
	rail.RestoreEnabledSkills(map[string]struct{}{"x": {}})
	rail.DiscardEnabledSkill("x")
	assert.Empty(t, rail.EnabledSkillsSnapshot())
}

// subagentRailType 返回 SubagentRail 的反射类型。
func subagentRailType() reflect.Type { return reflect.TypeOf(&subagentrails.SubagentRail{}) }
