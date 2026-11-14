package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestResolvedPromptSection 等用例引用 single_agent/prompts 的 PromptSection。

// ──────────────────────────── 15.15 类型层 ────────────────────────────

// TestResourceKind_成员全集 成员集合对齐 Python extension_resolver.py:41-50。
// 数量断言经机器核对：grep -A8 "^class ResourceKind" extension_resolver.py 数成员为 6。
func TestResourceKind_成员全集(t *testing.T) {
	kinds := []ResourceKind{
		ResourceKindTool,
		ResourceKindMCP,
		ResourceKindRail,
		ResourceKindPromptSection,
		ResourceKindSkill,
		ResourceKindSubagent,
	}
	require.Len(t, kinds, 6)
	for _, kind := range kinds {
		assert.NotEmpty(t, string(kind))
	}
}

// TestResourceKind_字符串值 对齐 Python str 枚举的 .value。
func TestResourceKind_字符串值(t *testing.T) {
	assert.Equal(t, "tool", string(ResourceKindTool))
	assert.Equal(t, "mcp", string(ResourceKindMCP))
	assert.Equal(t, "rail", string(ResourceKindRail))
	assert.Equal(t, "prompt_section", string(ResourceKindPromptSection))
	assert.Equal(t, "skill", string(ResourceKindSkill))
	assert.Equal(t, "subagent", string(ResourceKindSubagent))
}

// TestNewResourceRef 默认 extra 为空 map（Python Field(default_factory=dict)）。
func TestNewResourceRef(t *testing.T) {
	ref := NewResourceRef(ResourceKindRail, "core.ask_user")
	assert.Equal(t, ResourceKindRail, ref.Kind)
	assert.Equal(t, "core.ask_user", ref.Identity)
	assert.NotNil(t, ref.Extra)
	assert.Empty(t, ref.Extra)
}

// TestNewLoadRecord load_id 为 uuid4().hex 等价（32 位十六进制）。
// Python: extension_resolver.py:63 default_factory=lambda: uuid4().hex
func TestNewLoadRecord(t *testing.T) {
	record := NewLoadRecord()
	assert.Regexp(t, `^[0-9a-f]{32}$`, record.LoadID)
	assert.NotNil(t, record.Refs)
	assert.Empty(t, record.SourceURI)
}

// TestNewLoadRecord_唯一性 连续生成的 load_id 不重复。
func TestNewLoadRecord_唯一性(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		id := NewLoadRecord().LoadID
		_, dup := seen[id]
		assert.False(t, dup, "load_id 重复: %s", id)
		seen[id] = struct{}{}
	}
}

// TestNewExtensionParts 各字段默认空切片（Python dataclass default_factory=list）。
func TestNewExtensionParts(t *testing.T) {
	parts := NewExtensionParts()
	assert.NotNil(t, parts.Tools)
	assert.NotNil(t, parts.Mcps)
	assert.NotNil(t, parts.Rails)
	assert.NotNil(t, parts.PromptSections)
	assert.NotNil(t, parts.Skills)
	assert.NotNil(t, parts.Subagents)
	assert.Empty(t, parts.Tools)
}

// TestResolvedSkill_形状 对齐 Python extension_resolver.py:69-74。
func TestResolvedSkill_形状(t *testing.T) {
	skill := ResolvedSkill{Directory: "/abs/skills/a", Mode: "auto_list"}
	assert.Equal(t, "/abs/skills/a", skill.Directory)
	assert.Equal(t, "auto_list", skill.Mode)
	assert.Nil(t, skill.EnabledSkills)
}

// TestResolvedPromptSection_形状 对齐 Python extension_resolver.py:78-88。
func TestResolvedPromptSection_形状(t *testing.T) {
	section := saprompts.PromptSection{Name: "identity", Content: map[string]string{"cn": "你"}, Priority: 10}
	resolved := ResolvedPromptSection{Section: section, ReplaceExisting: true}
	assert.True(t, resolved.ReplaceExisting)
	assert.Equal(t, "identity", resolved.Section.Name)
}

// ──────────────────────────── resolve 装配层测试 ────────────────────────────

// TestBuildMcpServerConfig_stdioSetdefault 对齐 Python :197-228：
// params 已有键不被 spec 字段覆盖（setdefault 语义）。
func TestBuildMcpServerConfig_stdioSetdefault(t *testing.T) {
	spec := hschema.McpServerSpec{
		Type: "stdio", ServerName: "fs", Command: "node", Args: []string{"s.js"},
		Cwd: "/w", Env: map[string]string{"K": "V"},
		Params: map[string]any{"command": "override", "custom": 1},
	}
	cfg := buildMcpServerConfig(&spec)
	assert.Equal(t, "override", cfg.Params["command"], "params 已有 command 时不被覆盖")
	assert.Equal(t, []string{"s.js"}, cfg.Params["args"])
	assert.Equal(t, "/w", cfg.Params["cwd"])
	assert.Equal(t, map[string]string{"K": "V"}, cfg.Params["env"])
	assert.Equal(t, 1, cfg.Params["custom"])
	assert.Equal(t, "fs", cfg.ServerName)
	assert.Equal(t, "node", cfg.ServerPath)
	assert.Equal(t, "stdio", cfg.ClientType)
}

// TestBuildMcpServerConfig_http路径与默认名 对齐 Python :222-227。
func TestBuildMcpServerConfig_http路径与默认名(t *testing.T) {
	spec := hschema.McpServerSpec{Type: "sse", URL: "http://x/sse"}
	cfg := buildMcpServerConfig(&spec)
	assert.Equal(t, "http://x/sse", cfg.ServerPath)
	assert.Equal(t, "mcp_server", cfg.ServerName)

	withID := spec
	withID.ServerID = "sid-1"
	cfg2 := buildMcpServerConfig(&withID)
	assert.Equal(t, "sid-1", cfg2.ServerID)
}

// TestResolvePluginParts_成员与ReplaceExisting 对齐 Python :103-117。
func TestResolvePluginParts_成员与ReplaceExisting(t *testing.T) {
	spec := hschema.PluginSpec{
		ID: "p1",
		PromptSections: []hschema.PromptSectionSpec{
			{Name: "sop", Content: map[string]string{"cn": "你好 {{ language }}"}, Priority: 5},
		},
		Skills: []hschema.SkillSpec{{Dir: "/abs/skills/a", Mode: "all"}},
	}
	parts, err := ResolvePluginParts(spec, nil)
	require.NoError(t, err)
	require.Len(t, parts.PromptSections, 1)
	assert.False(t, parts.PromptSections[0].ReplaceExisting, "Plugin 段落恒不替换")
	assert.Equal(t, "你好 cn", parts.PromptSections[0].Section.Content["cn"])
	require.Len(t, parts.Skills, 1)
	assert.Equal(t, "/abs/skills/a", parts.Skills[0].Directory)
	assert.Empty(t, parts.Subagents, "Plugin 从不产出 subagents")
}

// TestResolveAgentTemplateParts_父模型缺失报错 对齐 Python :130-133。
func TestResolveAgentTemplateParts_父模型缺失报错(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		Subagents: []hschema.AgentTemplateSpec{{}},
	}
	_, err := ResolveAgentTemplateParts(spec, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_parent_model")
}

// TestResolveAgentTemplateParts_subagent物化 对齐 Python :134-151 + :153-173。
func TestResolveAgentTemplateParts_subagent物化(t *testing.T) {
	model := &llm.Model{}
	ctx := newTestBuildContext(t)
	ctx.GetExtras()[ParentModelExtrasKeyRes] = model
	spec := hschema.AgentTemplateSpec{
		Subagents: []hschema.AgentTemplateSpec{{
			AgentCard: mustCard(t, "child-1", "child", "子模板"),
			PromptSections: []hschema.PromptSectionSpec{
				{Name: "identity", Content: map[string]string{"cn": "子代理", "en": "child"}, Priority: 10},
			},
			Skills: []hschema.SkillSpec{{Dir: "/s/a"}, {Dir: "/s/b"}},
		}},
	}
	parts, err := ResolveAgentTemplateParts(spec, ctx)
	require.NoError(t, err)
	require.Len(t, parts.Subagents, 1)
	sub := parts.Subagents[0]
	assert.Equal(t, "child-1", sub.AgentCard.ID)
	assert.Equal(t, "子代理", sub.SystemPrompt)
	assert.Equal(t, []string{"/s/a", "/s/b"}, sub.Skills)
	require.Len(t, parts.PromptSections, 0)
}

// TestResolveAgentTemplateParts_段落ReplaceExisting 对齐 Python :144-147。
func TestResolveAgentTemplateParts_段落ReplaceExisting(t *testing.T) {
	ctx := newTestBuildContext(t)
	ctx.GetExtras()[ParentModelExtrasKeyRes] = &llm.Model{}
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{
			{Name: "identity", Content: map[string]string{"cn": "根 persona"}, Priority: 10},
		},
	}
	parts, err := ResolveAgentTemplateParts(spec, ctx)
	require.NoError(t, err)
	require.Len(t, parts.PromptSections, 1)
	assert.True(t, parts.PromptSections[0].ReplaceExisting, "AgentTemplate 根 persona 允许替换")
}

// newTestBuildContext 构造带 extras 的最小 BuildContext。
func newTestBuildContext(t *testing.T) hschema.BuildContextInterface {
	t.Helper()
	return &hschema.BuildContext{Extras: map[string]any{}}
}

// mustCard 构造测试用 AgentCard（与 harness_providers 测试同款）。
func mustCard(t *testing.T, id, name, description string) saschema.AgentCard {
	t.Helper()
	card := saschema.NewAgentCard(
		saschema.WithAgentID(id),
		saschema.WithAgentName(name),
		saschema.WithAgentDescription(description),
	)
	require.NotNil(t, card)
	return *card
}
