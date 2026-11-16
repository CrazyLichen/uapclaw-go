package experts

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cmschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// 15.14 模板 → Team 成员装配测试。
// Python: tests/unit_tests/agent_teams/experts/test_template_member.py

// templateFixture 构造模板（对齐 Python 测试助手 _template）。
func templateFixture(t *testing.T, sections []hschema.PromptSectionSpec, skills ...string) hschema.AgentTemplateSpec {
	t.Helper()
	spec := hschema.AgentTemplateSpec{
		AgentCard: saschema.AgentCard{BaseCard: cmschema.BaseCard{ID: "clip", Name: "clip", Description: "Shot artist"}},
	}
	spec.PromptSections = sections
	for _, name := range skills {
		spec.Skills = append(spec.Skills, hschema.SkillSpec{Dir: "/pkg/skills/" + name})
	}
	return spec
}

// baseSpec 构造 base spec（对齐 Python 测试助手 _base）。
func baseSpec() hschema.DeepAgentSpec {
	base := hschema.NewDeepAgentSpec()
	base.Language = "en"
	base.Skills = []string{"shared", "clip"}
	base.Workspace = &hschema.WorkspaceSpec{RootPath: "/ws/teammate"}
	return base
}

// TestRenderTemplatePersona_优先级排序_strip空段丢弃 Python: test_persona_orders_by_priority_strips_and_drops_empty_sections。
func TestRenderTemplatePersona_优先级排序_strip空段丢弃(t *testing.T) {
	template := templateFixture(t, []hschema.PromptSectionSpec{
		{Name: "rules", Priority: 30, Content: map[string]string{"cn": "  规则 {{ workspace }}  \n"}},
		{Name: "blank", Priority: 20, Content: map[string]string{"cn": "   \n"}},
		{Name: "identity", Priority: 10, Content: map[string]string{"cn": "\n我是 {{language}}\n"}},
	})
	persona := RenderTemplatePersona(template, "cn", "/ws")
	assert.Equal(t, "我是 cn\n\n规则 /ws", persona)
}

// TestRenderTemplatePersona_语言回退与占位符 Python: test_persona_language_fallback_and_placeholders。
func TestRenderTemplatePersona_语言回退与占位符(t *testing.T) {
	template := templateFixture(t, []hschema.PromptSectionSpec{
		{Name: "a", Priority: 10, Content: map[string]string{"en": "EN {{ who }} {{ unknown }}"}, RenderParams: map[string]any{"who": "me"}},
		{Name: "b", Priority: 20, Content: map[string]string{"cn": "只有中文"}},
		{Name: "c", Priority: 30, Content: map[string]string{"fr": "seulement"}},
	})
	assert.Equal(t, "EN me {{ unknown }}\n\n只有中文\n\nseulement", RenderTemplatePersona(template, "cn", ""))
	assert.Equal(t, RenderTemplatePersona(template, "cn", ""), RenderTemplatePersona(template, "  ", ""))
	assert.Equal(t, RenderTemplatePersona(template, "cn", ""), RenderTemplatePersona(template, "", ""))
}

// TestTemplateSnapshot_剥段与skills并集 Python: test_snapshot_drops_prompt_sections_and_apply_unions_skill_names。
func TestTemplateSnapshot_剥段与skills并集(t *testing.T) {
	template := templateFixture(t, []hschema.PromptSectionSpec{
		{Name: "identity", Content: map[string]string{"cn": "x"}},
	}, "clip", "storyboard", "clip")
	snapshot := TemplateSnapshot(template)
	// Python model_dump(mode="json") 保留 prompt_sections 为空列表（非省略键）
	sections, has := snapshot["prompt_sections"]
	require.True(t, has, "snapshot 保留 prompt_sections 键（pydantic 默认列表序列化为 []）")
	assert.Equal(t, []any{}, sections)
	skills, ok := snapshot["skills"].([]any)
	require.True(t, ok)
	require.Len(t, skills, 3)

	spec := ApplyTemplateSnapshot(baseSpec(), snapshot)
	// D-5：字段已收敛为类型，快照 map 经 JSON 往返重建；PromptSections 带omitempty，
	// 空列表在类型化 wire 上省略（Python model_validate 下 [] 与缺省等值，无字节一致性消费方）
	raw, err := json.Marshal(spec.AgentTemplateSpec)
	require.NoError(t, err)
	typedBack := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &typedBack))
	expected := map[string]any{}
	for k, v := range snapshot {
		if k != "prompt_sections" {
			expected[k] = v
		}
	}
	assert.Equal(t, expected, typedBack)
	assert.Equal(t, []string{"shared", "clip", "storyboard"}, spec.Skills)
	assert.Nil(t, baseSpec().AgentTemplateSpec, "base spec 不被原地修改")
}

// TestComposeTemplateMember_与分步装配一致 Python: test_compose_equals_the_step_by_step_assembly。
func TestComposeTemplateMember_与分步装配一致(t *testing.T) {
	template := templateFixture(t, []hschema.PromptSectionSpec{
		{Name: "identity", Content: map[string]string{"en": "Work in {{ workspace }} ({{ language }})"}},
	}, "render")
	base := baseSpec()
	member := ComposeTemplateMember(base, template, "")

	stepwiseSpec := ApplyTemplateSnapshot(base, TemplateSnapshot(template))
	stepwisePersona := RenderTemplatePersona(template, base.Language, "/ws/teammate")
	require.NotNil(t, member.Spec.AgentTemplateSpec)
	// spec 逐字段对照（model_dump 等价：JSON 往返比较）
	raw1, err := json.Marshal(member.Spec.AgentTemplateSpec)
	require.NoError(t, err)
	raw2, err := json.Marshal(stepwiseSpec.AgentTemplateSpec)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw2), string(raw1))
	assert.Equal(t, stepwisePersona, member.Persona)
	assert.Equal(t, "Work in /ws/teammate (en)", member.Persona)
}

// TestComposeTemplateMember_语言与工作区默认 Python: test_compose_defaults_language_and_workspace。
func TestComposeTemplateMember_语言与工作区默认(t *testing.T) {
	template := templateFixture(t, []hschema.PromptSectionSpec{
		{Name: "identity", Content: map[string]string{"cn": "{{language}}|{{workspace}}"}},
	})
	empty := baseSpec()
	empty.Language = ""
	empty.Workspace = nil
	assert.Equal(t, "cn|", ComposeTemplateMember(empty, template, "").Persona)
	assert.Equal(t, "cn|/ws/teammate", ComposeTemplateMember(baseSpec(), template, "cn").Persona)
}
