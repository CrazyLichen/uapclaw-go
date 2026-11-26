package reviewsubagent

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// TestBuildEvolutionReviewAgentConfig 对齐 subagent.py:125-171：
// name=evolution_reviewer / 双语 description / system prompt 按 language /
// tools=CreateEvolutionReviewTools / enable_task_loop=false / max_iterations=25 /
// restrict_to_work_dir=true / enable_read_image_multimodal=false / rails/mcps/skills 空。
func TestBuildEvolutionReviewAgentConfig(t *testing.T) {
	rt := review.NewEvolutionReviewRuntime(60)
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	config, err := BuildEvolutionReviewAgentConfig(rt, nil, nil, store, "cn", 0, "agent-1")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.NotNil(t, config.AgentCard)
	assert.Equal(t, "evolution_reviewer", config.AgentCard.Name)
	assert.Equal(t, "Skill 演进审查 agent，仅使用只读工具。", config.AgentCard.Description)
	assert.Contains(t, config.SystemPrompt, "你是 Skill 演进审查 subagent")
	assert.False(t, config.EnableTaskLoop)
	assert.Equal(t, 25, config.MaxIterations)
	assert.True(t, config.RestrictToWorkDir)
	assert.False(t, config.EnableReadImageMultimodal)
	assert.Empty(t, config.Mcps)
	assert.Empty(t, config.Rails)
	assert.Empty(t, config.Skills)
	assert.Nil(t, config.Model)
	// 五只读审查工具全部挂接
	assert.Len(t, config.ToolInstances, 5)
	// binding 元数据已挂载
	binding := GetReviewAgentBinding(config)
	require.NotNil(t, binding)
	assert.Same(t, rt, binding.Runtime)
}

// TestBuildEvolutionReviewAgentConfig_英文与自定义迭代 对齐 language="en" 分支与
// max_iterations 透传（subagent.py:143-153）。
func TestBuildEvolutionReviewAgentConfig_英文与自定义迭代(t *testing.T) {
	rt := review.NewEvolutionReviewRuntime(60)
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	config, err := BuildEvolutionReviewAgentConfig(rt, nil, nil, store, "en", 10, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "Skill evolution review agent with read-only tools.", config.AgentCard.Description)
	assert.Contains(t, config.SystemPrompt, "You are the Skill evolution review agent")
	assert.Equal(t, 10, config.MaxIterations)
}

// TestEnsureEvolutionReviewAgentConfig 对齐 :174-187 三分支：
// 不存在 → append；存在且 binding 相等 → 原样；binding 不等或缺 → replace。
func TestEnsureEvolutionReviewAgentConfig(t *testing.T) {
	rt := review.NewEvolutionReviewRuntime(60)
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	config, err := BuildEvolutionReviewAgentConfig(rt, nil, nil, store, "cn", 0, "agent-1")
	require.NoError(t, err)

	// 分支 1：不存在 → append
	other := &harnessschema.SubAgentConfig{}
	subagents := EnsureEvolutionReviewAgentConfig([]*harnessschema.SubAgentConfig{other}, config)
	assert.Len(t, subagents, 2)

	// 分支 2：存在且 binding 相等 → 原样返回
	got := EnsureEvolutionReviewAgentConfig(subagents, config)
	assert.Same(t, &subagents[0], &got[0])
	assert.Len(t, got, 2)

	// 分支 3：binding 不等（新 runtime）→ replace
	rt2 := review.NewEvolutionReviewRuntime(60)
	config2, err := BuildEvolutionReviewAgentConfig(rt2, nil, nil, store, "cn", 0, "agent-1")
	require.NoError(t, err)
	got = EnsureEvolutionReviewAgentConfig(subagents, config2)
	assert.Len(t, got, 2)
	assert.Same(t, config2, got[1])
}

// TestRemoveReplaceEvolutionReviewAgentConfig 对齐 :190-197。
func TestRemoveReplaceEvolutionReviewAgentConfig(t *testing.T) {
	rt := review.NewEvolutionReviewRuntime(60)
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	config, err := BuildEvolutionReviewAgentConfig(rt, nil, nil, store, "cn", 0, "agent-1")
	require.NoError(t, err)
	other := &harnessschema.SubAgentConfig{}
	subagents := []*harnessschema.SubAgentConfig{other, config}

	removed := RemoveEvolutionReviewAgentConfig(subagents)
	assert.Len(t, removed, 1)
	assert.Same(t, other, removed[0])

	replaced := ReplaceEvolutionReviewAgentConfig(subagents, config)
	assert.Len(t, replaced, 2)
	assert.Same(t, config, replaced[1])
}

// TestReviewToolAgentID 对齐 :204-209：
// 非法字符归一；空 parent 兜底 "agent"；前导/尾随 _ 剥离。
func TestReviewToolAgentID(t *testing.T) {
	rt := review.NewEvolutionReviewRuntime(60)
	id := ReviewToolAgentID("agent-1", rt)
	assert.Regexp(t, regexp.MustCompile(`^agent_1_evolution_review_[0-9a-f]+$`), id)
	assert.Equal(t, ReviewToolAgentID("", rt), ReviewToolAgentID("  ", rt))
	assert.Contains(t, ReviewToolAgentID("", rt), "agent_evolution_review_")
}

// TestReviewBindingEqual 对齐 frozen dataclass __eq__：全字段相等判定。
func TestReviewBindingEqual(t *testing.T) {
	rt := review.NewEvolutionReviewRuntime(60)
	a := &ReviewAgentBinding{Runtime: rt}
	b := &ReviewAgentBinding{Runtime: rt}
	assert.True(t, reviewBindingEqual(a, b))
	b.Runtime = review.NewEvolutionReviewRuntime(60)
	assert.False(t, reviewBindingEqual(a, b))
	assert.False(t, reviewBindingEqual(a, nil))
}
