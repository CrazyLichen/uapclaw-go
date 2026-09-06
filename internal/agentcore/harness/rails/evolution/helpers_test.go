package evolution

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNormalizeSkillNames_空切片(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames([]string{}))
}

func TestNormalizeSkillNames_单个名称(t *testing.T) {
	assert.Equal(t, map[string]bool{"foo": true}, normalizeSkillNames([]string{"foo"}))
}

func TestNormalizeSkillNames_多个名称(t *testing.T) {
	assert.Equal(t, map[string]bool{"a": true, "b": true}, normalizeSkillNames([]string{"a", "b"}))
}

func TestNormalizeSkillNames_含空格和空项(t *testing.T) {
	assert.Equal(t, map[string]bool{"x": true}, normalizeSkillNames([]string{" x ", "", "  "}))
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

// ──────────────────────────── baseMessageToMap 测试 ────────────────────────────

func TestBaseMessageToMap_nil(t *testing.T) {
	assert.Equal(t, map[string]any{}, baseMessageToMap(nil))
}

func TestBaseMessageToMap_纯文本消息(t *testing.T) {
	msg := llmschema.NewUserMessage("hello")
	result := baseMessageToMap(msg)
	assert.Equal(t, "user", result["role"])
	assert.NotNil(t, result["content"])
	assert.NotContains(t, result, "name")
	assert.NotContains(t, result, "metadata")
}

func TestBaseMessageToMap_带Name和Metadata(t *testing.T) {
	msg := llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "hello",
		llmschema.WithMessageName("test_user"),
		llmschema.WithMetadata(map[string]any{"key": "val"}),
	)
	result := baseMessageToMap(msg)
	assert.Equal(t, "user", result["role"])
	assert.Equal(t, "test_user", result["name"])
	meta, ok := result["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "val", meta["key"])
}

// ──────────────────────────── toolInfoToMap 测试 ────────────────────────────

func TestToolInfoToMap_nil(t *testing.T) {
	assert.Equal(t, map[string]any{}, toolInfoToMap(nil))
}

func TestToolInfoToMap_完整字段(t *testing.T) {
	tool := cschema.NewToolInfo("search", "搜索工具", map[string]any{"type": "object"})
	result := toolInfoToMap(tool)
	assert.Equal(t, "function", result["type"])
	assert.Equal(t, "search", result["name"])
	assert.Equal(t, "搜索工具", result["description"])
	assert.NotNil(t, result["parameters"])
}

// ──────────────────────────── stringPtr 测试 ────────────────────────────

func TestStringPtr_空字符串(t *testing.T) {
	assert.Nil(t, stringPtr(""))
}

func TestStringPtr_非空字符串(t *testing.T) {
	p := stringPtr("hello")
	assert.NotNil(t, p)
	assert.Equal(t, "hello", *p)
}

// ──────────────────────────── formatSkillName 测试 ────────────────────────────

func TestFormatSkillName_nil(t *testing.T) {
	assert.Equal(t, "unknown", formatSkillName(nil))
}

func TestFormatSkillName_有技能名称(t *testing.T) {
	name := "search_optimization"
	assert.Equal(t, "search_optimization", formatSkillName(&EvolutionSnapshot{SkillName: &name}))
}
