package evolution

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── splitResponseTokenFields 测试 ────────────────────────────

func TestSplitResponseTokenFields_nil(t *testing.T) {
	resp, ptids, ctids, lp := splitResponseTokenFields(nil)
	assert.Nil(t, resp)
	assert.Nil(t, ptids)
	assert.Nil(t, ctids)
	assert.Nil(t, lp)
}

func TestSplitResponseTokenFields_无token字段(t *testing.T) {
	orig := llmschema.NewAssistantMessage("hello")
	resp, ptids, ctids, lp := splitResponseTokenFields(orig)
	assert.NotNil(t, resp)
	// content 字段存在但类型为 MessageContent，不做精确值比较
	assert.Contains(t, resp, "content")
	assert.Nil(t, ptids)
	assert.Nil(t, ctids)
	assert.Nil(t, lp)
}

func TestSplitResponseTokenFields_含token字段(t *testing.T) {
	orig := llmschema.NewAssistantMessage("hello",
		llmschema.WithPromptTokenIDs([]int{1, 2, 3}),
		llmschema.WithCompletionTokenIDs([]int{4, 5}),
		llmschema.WithLogprobs(map[string]any{"top_logprobs": []any{"a"}}),
	)
	resp, ptids, ctids, lp := splitResponseTokenFields(orig)
	assert.NotNil(t, resp)
	// token 字段已从 map 中移除
	assert.NotContains(t, resp, "prompt_token_ids")
	assert.NotContains(t, resp, "completion_token_ids")
	assert.NotContains(t, resp, "logprobs")
	// 从返回值中获取
	assert.Equal(t, []int{1, 2, 3}, ptids)
	assert.Equal(t, []int{4, 5}, ctids)
	assert.NotNil(t, lp)
}

// ──────────────────────────── normalizeSkillNames 测试 ────────────────────────────

func TestNormalizeSkillNames_nil(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames(nil))
}

func TestNormalizeSkillNames_字符串(t *testing.T) {
	assert.Equal(t, map[string]bool{"foo": true}, normalizeSkillNames("foo"))
}

func TestNormalizeSkillNames_字符串前后空格(t *testing.T) {
	assert.Equal(t, map[string]bool{"bar": true}, normalizeSkillNames("  bar  "))
}

func TestNormalizeSkillNames_空字符串(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames(""))
}

func TestNormalizeSkillNames_列表(t *testing.T) {
	assert.Equal(t, map[string]bool{"a": true, "b": true}, normalizeSkillNames([]string{"a", "b"}))
}

func TestNormalizeSkillNames_列表含空格和空项(t *testing.T) {
	assert.Equal(t, map[string]bool{"x": true}, normalizeSkillNames([]string{" x ", "", "  "}))
}

func TestNormalizeSkillNames_其他类型(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames(42))
}

// ──────────────────────────── normalizeMemberRole 测试 ────────────────────────────

func TestNormalizeMemberRole_nil(t *testing.T) {
	assert.Nil(t, normalizeMemberRole(nil))
}

func TestNormalizeMemberRole_字符串(t *testing.T) {
	s := "leader"
	assert.Equal(t, &s, normalizeMemberRole("leader"))
}

func TestNormalizeMemberRole_空字符串(t *testing.T) {
	assert.Nil(t, normalizeMemberRole(""))
}

func TestNormalizeMemberRole_自定义Stringer(t *testing.T) {
	// 验证实现 String() 接口的非 string 类型被正确处理
	role := &stringerMock{val: "admin"}
	result := normalizeMemberRole(role)
	s := "admin"
	assert.Equal(t, &s, result)
}

// stringerMock 实现 fmt.Stringer 接口的测试辅助类型
type stringerMock struct {
	val string
}

func (s *stringerMock) String() string {
	return s.val
}

// ──────────────────────────── collectMessagesFromTrajectory 测试 ────────────────────────────

func TestCollectMessagesFromTrajectory_nil(t *testing.T) {
	assert.Equal(t, []map[string]any{}, collectMessagesFromTrajectory(nil))
}

func TestCollectMessagesFromTrajectory_空轨迹(t *testing.T) {
	traj := &trajectory.Trajectory{Steps: []*trajectory.TrajectoryStep{}}
	assert.Equal(t, []map[string]any{}, collectMessagesFromTrajectory(traj))
}

func TestCollectMessagesFromTrajectory_含LLM步骤(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{
						{"role": "user", "content": "hi"},
						{"role": "assistant", "content": "hello"},
					},
				},
			},
		},
	}
	msgs := collectMessagesFromTrajectory(traj)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0]["role"])
	assert.Equal(t, "assistant", msgs[1]["role"])
}

func TestCollectMessagesFromTrajectory_去重(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{
						{"role": "user", "content": "hi"},
						{"role": "user", "content": "hi"},
					},
				},
			},
		},
	}
	msgs := collectMessagesFromTrajectory(traj)
	assert.Len(t, msgs, 1)
}

// ──────────────────────────── normalizeCallbackMessages 测试 ────────────────────────────

func TestNormalizeCallbackMessages_空(t *testing.T) {
	result := normalizeCallbackMessages(nil)
	assert.Equal(t, []map[string]any{}, result)
}

func TestNormalizeCallbackMessages_浅拷贝(t *testing.T) {
	original := []map[string]any{
		{"role": "user", "content": "hi"},
	}
	result := normalizeCallbackMessages(original)
	assert.Len(t, result, 1)
	// 修改 result 不应影响 original
	result[0]["content"] = "changed"
	assert.Equal(t, "hi", original[0]["content"])
}
