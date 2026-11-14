package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
