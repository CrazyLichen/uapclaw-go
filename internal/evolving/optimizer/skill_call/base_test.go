package skill_call

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	skillop "github.com/uapclaw/uapclaw-go/internal/agentcore/operator/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillExperienceOptimizerBase_Domain(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	assert.Equal(t, "skill_experience", b.Domain())
}

func TestSkillExperienceOptimizerBase_DefaultTargets(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	assert.Equal(t, []string{schema.ExperiencesTarget}, b.DefaultTargets())
}

func TestSkillExperienceOptimizerBase_RequiresForwardData(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	assert.True(t, b.RequiresForwardData())
}

func TestSkillExperienceOptimizerBase_Bind提取OnlineContexts(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	op := skillop.NewSkillExperienceOperator("test_skill")
	ops := map[string]operator.Operator{op.OperatorID(): op}

	ctx := &experience.EvolutionContext{SkillName: "test_skill"}
	config := map[string]any{"online_contexts": map[string]*experience.EvolutionContext{"test_skill": ctx}}
	n := b.Bind(ops, nil, config)
	assert.Equal(t, 1, n)
	assert.NotNil(t, b.onlineContexts["test_skill"])
}

func TestSkillExperienceOptimizerBase_Bind无OnlineContexts(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	op := skillop.NewSkillExperienceOperator("test_skill")
	ops := map[string]operator.Operator{op.OperatorID(): op}

	n := b.Bind(ops, nil, nil)
	assert.Equal(t, 1, n)
	assert.Equal(t, map[string]*experience.EvolutionContext{}, b.onlineContexts)
}

func TestSkillExperienceOptimizerBase_UpdateLLM(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	// nil llm 应被拒绝
	b.UpdateLLM(nil, "new-model")
	assert.Nil(t, b.llm)
}

func TestInitialScoreBySignal_常量验证(t *testing.T) {
	assert.Equal(t, 0.65, InitialScoreBySignal["execution_failure"])
	assert.Equal(t, 0.70, InitialScoreBySignal["user_correction"])
	assert.Equal(t, 0.60, InitialScoreBySignal["script_artifact"])
	assert.Equal(t, 0.50, InitialScoreBySignal["conversation_review"])
	assert.Equal(t, 4, len(InitialScoreBySignal))
}

func TestGenerateRecordsLLMPolicy_常量验证(t *testing.T) {
	assert.Equal(t, 150.0, GenerateRecordsLLMPolicy.AttemptTimeoutSecs)
	assert.Equal(t, 300.0, GenerateRecordsLLMPolicy.TotalBudgetSecs)
	assert.Equal(t, 2, GenerateRecordsLLMPolicy.MaxAttempts)
}

func TestTeamInitialScoreBySignal_常量验证(t *testing.T) {
	assert.Equal(t, 0.65, TeamInitialScoreBySignal["trajectory_issue"])
	assert.Equal(t, 0.70, TeamInitialScoreBySignal["user_intent"])
	assert.Equal(t, 0.68, TeamInitialScoreBySignal["team_skill_mixed"])
	assert.Equal(t, 3, len(TeamInitialScoreBySignal))
}

func TestRemoveSkillPrefix(t *testing.T) {
	assert.Equal(t, "test_skill", removeSkillPrefix("skill_experience_test_skill"))
	assert.Equal(t, "other", removeSkillPrefix("other"))
}
