package experts

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/schema"
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
	// D-5 收窄：快照为类型化值拷贝，prompt_sections 置空（Python model_copy 语义）
	assert.Empty(t, snapshot.PromptSections)
	require.Len(t, snapshot.Skills, 3)

	// wire 形状对齐：json tag 序列化（Python model_dump(mode="json") 等价）
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	typedBack := hschema.AgentTemplateSpec{}
	require.NoError(t, json.Unmarshal(raw, &typedBack))
	require.Len(t, typedBack.Skills, 3)
	assert.Empty(t, typedBack.PromptSections)

	spec := ApplyTemplateSnapshot(baseSpec(), snapshot)
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
