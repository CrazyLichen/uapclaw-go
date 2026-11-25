package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildEvolutionReviewAgentPrompt 双语提示词一比一对拍。
// Python: build_evolution_review_agent_prompt + _EVOLUTION_REVIEW_AGENT_PROMPT_EN/CN
// (review/subagent.py:120-122, 23-82)。
func TestBuildEvolutionReviewAgentPrompt(t *testing.T) {
	en := BuildEvolutionReviewAgentPrompt("en")
	assert.Contains(t, en, "You are the Skill evolution review agent.")
	assert.Contains(t, en, "experience.target must be one of description, body, script.")
	assert.Contains(t, en, "Submit no_evolution for one-off task facts")
	assert.Contains(t, en, "Do not wrap it in Markdown, summarize it, or rewrite any field.")

	cn := BuildEvolutionReviewAgentPrompt("cn")
	assert.Contains(t, cn, "你是 Skill 演进审查 subagent。")
	assert.Contains(t, cn, "experience.target 只能是 description, body, script。")
	assert.Contains(t, cn, "对一次性任务事实、个人偏好、临时环境/权限/网络问题")
	assert.Contains(t, cn, "不要包裹 Markdown，不要总结，不要重写任何字段。")

	// 未知语言回落 cn
	assert.Equal(t, cn, BuildEvolutionReviewAgentPrompt("jp"))

	// 常量名对齐
	assert.Equal(t, "evolution_reviewer", EvolutionReviewAgentName)
}
