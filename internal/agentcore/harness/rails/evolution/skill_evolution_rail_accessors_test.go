package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSkillRail_SharingAccessors 验证共享相关访问器在未启用共享时为空。
func TestSkillRail_SharingAccessors(t *testing.T) {
	r := newConstructedRail(t)
	// 未配置 sharing_config 时共享组件不构建
	assert.False(t, r.IsSharingEnabled())
	assert.Nil(t, r.ExperienceSharer())
	assert.Nil(t, r.ShareStager())
	assert.Nil(t, r.KeywordExtractor())
}

// TestSkillRail_UpdateLLM 验证热更新 LLM 后模型名与组件联动。
func TestSkillRail_UpdateLLM(t *testing.T) {
	r := newConstructedRail(t)
	r.UpdateLLM(newTestLLMModel(t), "hot-model")
	assert.Equal(t, "hot-model", r.ModelName())
	assert.NotNil(t, r.Evolver())
	assert.NotNil(t, r.Scorer())
}

// TestSkillRail_SetSysOperation 验证系统操作下发给基类与 store 且不 panic。
func TestSkillRail_SetSysOperation(t *testing.T) {
	r := newConstructedRail(t)
	assert.NotPanics(t, func() { r.SetSysOperation(nil) })
}

// TestSkillRail_ShouldHintSimplifyOrRebuild 验证无演进日志时返回 false。
func TestSkillRail_ShouldHintSimplifyOrRebuild(t *testing.T) {
	r := newConstructedRail(t)
	assert.False(t, r.ShouldHintSimplifyOrRebuild("no-such-skill"))
}

// TestSkillRail_RecordPresentedExperiences 验证空记录时不 panic 且不写追踪。
func TestSkillRail_RecordPresentedExperiences(t *testing.T) {
	r := newConstructedRail(t)
	assert.NotPanics(t, func() {
		r.RecordPresentedExperiences(context.Background(), "skill-a", "snippet", "sess-1", nil)
	})
	assert.NotPanics(t, func() {
		r.RecordPresentedExperiences(context.Background(), "skill-a", "snippet", "sess-1", []string{"rec-1"})
	})
}

// TestSkillRail_SnapshotForEvolution_空轨迹 验证无消息时不产生快照（与 Skill rail 同语义）。
func TestSkillRail_SnapshotForEvolution_空轨迹(t *testing.T) {
	r := newConstructedRail(t)
	assert.Nil(t, r.SnapshotForEvolution(context.Background(), nil, nil))
}

// TestSkillRail_RunEvolution_关闭扫描 验证 auto_scan 关闭时直接返回 nil。
func TestSkillRail_RunEvolution_关闭扫描(t *testing.T) {
	r := newConstructedRail(t, WithAutoScan(false))
	assert.NoError(t, r.RunEvolution(context.Background(), nil, nil))
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestResolveIncrementalMessages 验证增量消息解析的三条分支。
func TestResolveIncrementalMessages(t *testing.T) {
	r := newConstructedRail(t)
	messages := []map[string]any{{"role": "user"}}

	t.Run("snapshot 带增量时直接返回它", func(t *testing.T) {
		snap := &EvolutionSnapshot{IncrementalMessages: []map[string]any{{"role": "assistant"}}}
		got := r.resolveIncrementalMessages(messages, nil, snap)
		require.Len(t, got, 1)
		assert.Equal(t, "assistant", got[0]["role"])
	})

	t.Run("snapshot 无增量时返回全量", func(t *testing.T) {
		snap := &EvolutionSnapshot{}
		got := r.resolveIncrementalMessages(messages, nil, snap)
		assert.Len(t, got, len(messages))
	})

	t.Run("无 snapshot 无 cbc 时返回全量", func(t *testing.T) {
		got := r.resolveIncrementalMessages(messages, nil, nil)
		assert.Len(t, got, len(messages))
	})

	t.Run("有 cbc 时按偏移量切片", func(t *testing.T) {
		cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv-x"}, nil)
		got := r.resolveIncrementalMessages(messages, cbc, nil)
		// 首次调用偏移量为 0，返回全量
		assert.Len(t, got, len(messages))
		// 第二次调用偏移已推进到 len(messages)，应返回空
		got2 := r.resolveIncrementalMessages(messages, cbc, nil)
		assert.Empty(t, got2, "同一会话第二次调用应只返回新增消息")
	})
}

// TestGetExcerptKeyFromCtx 验证摘要键的取用优先级。
func TestGetExcerptKeyFromCtx(t *testing.T) {
	r := newConstructedRail(t)

	t.Run("cbc 为 nil 返回空", func(t *testing.T) {
		assert.Empty(t, r.getExcerptKeyFromCtx(nil))
	})

	t.Run("优先取 ConversationID", func(t *testing.T) {
		cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv-1"}, nil)
		assert.Equal(t, "conv-1", r.getExcerptKeyFromCtx(cbc))
	})

	t.Run("无 inputs 时返回空", func(t *testing.T) {
		cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
		assert.Empty(t, r.getExcerptKeyFromCtx(cbc))
	})
}

// TestExcerptOffsetRoundTrip 验证偏移量的读写闭环。
func TestExcerptOffsetRoundTrip(t *testing.T) {
	r := newConstructedRail(t)
	const key = "offset-test-key"

	assert.Equal(t, 0, r.getExcerptOffsetByKey(key), "未设置时偏移应为 0")
	r.setExcerptOffsetByKey(key, 7)
	assert.Equal(t, 7, r.getExcerptOffsetByKey(key))
}
