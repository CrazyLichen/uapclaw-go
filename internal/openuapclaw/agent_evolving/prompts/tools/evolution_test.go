package prompts_tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildEvolutionSubjectSchema 双语 subject schema 对拍。
// Python evolution.py:30-57。
func TestBuildEvolutionSubjectSchema(t *testing.T) {
	cn := BuildEvolutionSubjectSchema("cn")
	assert.Equal(t, "演进目标对象。", cn["description"])
	props := cn["properties"].(map[string]any)
	kind := props["kind"].(map[string]any)
	assert.Equal(t, []string{"skill", "swarm-skill"}, kind["enum"])
	assert.Equal(t, "演进对象类型。可选值：skill（常规技能）、swarm-skill（多智能体团队技能）。", kind["description"])
	assert.Equal(t, "演进对象名称。", props["name"].(map[string]any)["description"])
	assert.Equal(t, []string{"kind", "name"}, cn["required"])

	en := BuildEvolutionSubjectSchema("en")
	assert.Equal(t, "Evolution target object.", en["description"])
	assert.Equal(t, "Evolution subject kind. Allowed values: skill, swarm-skill.",
		en["properties"].(map[string]any)["kind"].(map[string]any)["description"])
}

// TestGetEvolutionToolDescription 双语描述对拍（代表工具逐字断言）。
// Python evolution.py:66-76/154-160/318-328。
func TestGetEvolutionToolDescription(t *testing.T) {
	cn, err := GetEvolutionToolDescription("prepare_skill_evolution", "cn")
	require.NoError(t, err)
	assert.Equal(t,
		"在起草新经验前创建技能演进 review ref；仅在用户明确确认后调用，必须提供 subject 对象并设置 "+
			"user_confirmed=true。返回的 ref 供后续 review 和变更工具使用。", cn)

	en, err := GetEvolutionToolDescription("prepare_skill_evolution", "en")
	require.NoError(t, err)
	assert.Contains(t, en, "Use only after explicit user confirmation")

	// list 工具
	cn, err = GetEvolutionToolDescription("list_skill_experiences", "cn")
	require.NoError(t, err)
	assert.Equal(t, "查询已持久化的演进经验索引，用于聚焦查找或覆盖溢出索引；只返回元数据，不返回全文。", cn)

	// simplify 工具
	en, err = GetEvolutionToolDescription("simplify_skill_experiences", "en")
	require.NoError(t, err)
	assert.Contains(t, en, "even when no interrupt rail is installed")

	// 未注册名称
	_, err = GetEvolutionToolDescription("nope", "cn")
	assert.Error(t, err)
}

// TestListProviderInputParams_枚举与默认值 对拍 enum/default 关键字段。
// Python evolution.py:162-218。
func TestListProviderInputParams_枚举与默认值(t *testing.T) {
	p := &ListSkillExperiencesMetadataProvider{}
	params := p.GetInputParams("cn")
	props := params["properties"].(map[string]any)
	assert.Equal(t, []string{"description", "body", "script"}, props["target"].(map[string]any)["enum"])
	assert.Equal(t, []string{"Collaboration", "Constraints", "Examples", "Instructions", "Roles", "Scripts", "Troubleshooting", "Workflow"},
		props["section"].(map[string]any)["enum"])
	sortEntry := props["sort"].(map[string]any)
	assert.Equal(t, []string{"score_desc", "updated_desc"}, sortEntry["enum"])
	assert.Equal(t, "score_desc", sortEntry["default"])
	assert.Equal(t, 20, props["limit"].(map[string]any)["default"])
	assert.Equal(t, []string{"subject"}, params["required"])
}

// TestSixProvidersRegistered 六提供者全部注册且 required 逐一对拍。
// Python evolution.py:384-392 + 各 get_input_params required。
func TestSixProvidersRegistered(t *testing.T) {
	cases := []struct {
		toolName string
		provider interface {
			GetName() string
			GetInputParams(language string) map[string]any
		}
		required []string
	}{
		{"prepare_skill_evolution", &PrepareSkillEvolutionReviewMetadataProvider{}, []string{"subject", "user_confirmed"}},
		{"evolve_review_task", &EvolveReviewTaskMetadataProvider{}, []string{"evolution_review_ref"}},
		{"list_skill_experiences", &ListSkillExperiencesMetadataProvider{}, []string{"subject"}},
		{"read_skill_experiences", &ReadSkillExperiencesMetadataProvider{}, []string{"subject", "record_ids"}},
		{"evolve_skill_experiences", &EvolveSkillExperiencesMetadataProvider{}, []string{"subject", "evolution_review_ref", "selected_proposal_ids"}},
		{"simplify_skill_experiences", &SimplifySkillExperiencesMetadataProvider{}, []string{"subject", "actions"}},
	}
	for _, c := range cases {
		assert.Equal(t, c.toolName, c.provider.GetName())
		params := c.provider.GetInputParams("cn")
		assert.Equal(t, c.required, params["required"], "tool=%s", c.toolName)
		// 全局注册表可见
		_, ok := GetEvolutionToolInputParams(c.toolName, "cn")
		assert.NoError(t, ok)
	}
}
