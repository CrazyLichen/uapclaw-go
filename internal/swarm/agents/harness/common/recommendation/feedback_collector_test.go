package recommendation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── RecordFeedback 测试分组 ────────────────────────────

// TestRecordFeedback_显式反馈 写入 buffer，含 rec 元数据。
// Python: feedback_collector.py:47
func TestRecordFeedback_显式反馈(t *testing.T) {
	resetRecommendationState(t)

	RecordExplicitFeedback("rec_1", "explicit_like", &RecMeta{
		RecType: "skill_recommend", RecTarget: "翻译技能", RecContent: "帮你翻文档",
	})

	state := LoadRecommendationState("")
	require.Len(t, state.FeedbackBuffer, 1)
	fb := state.FeedbackBuffer[0]
	assert.Equal(t, "rec_1", fb["rec_id"])
	assert.Equal(t, "explicit_like", fb["feedback_type"])
	assert.Equal(t, "skill_recommend", fb["rec_type"])
	assert.Equal(t, "翻译技能", fb["rec_target"])
	assert.Equal(t, "帮你翻文档", fb["rec_content"])
	assert.Equal(t, "", fb["user_reply"])
	assert.NotZero(t, fb["timestamp"])
}

// TestRecordFeedback_隐式反馈 feedback_type 占位 implicit，存用户回复原文。
// Python: feedback_collector.py:157
func TestRecordFeedback_隐式反馈(t *testing.T) {
	resetRecommendationState(t)

	RecordImplicitFeedback("rec_2", "这个不需要")

	state := LoadRecommendationState("")
	require.Len(t, state.FeedbackBuffer, 1)
	fb := state.FeedbackBuffer[0]
	assert.Equal(t, "implicit", fb["feedback_type"], "隐式反馈用占位类型，不预分类情感")
	assert.Equal(t, "这个不需要", fb["user_reply"], "必须存原文供梯度 LLM 判断")
}

// TestRecordFeedback_history已写入时取history数据 rec 在 history 里时优先用其字段。
func TestRecordFeedback_history已写入(t *testing.T) {
	resetRecommendationState(t)

	// 先写一条 recommendation_history
	state := &RecommendationState{
		RecommendationHistory: []map[string]any{
			{"id": "rec_3", "type": "task_reminder", "target": "整理报表", "content": "该整理了"},
		},
	}
	state.Touch()
	SaveRecommendationState(state, "")

	// meta 传不同值，验证 history 优先
	RecordExplicitFeedback("rec_3", "explicit_dislike", &RecMeta{
		RecType: "skill_recommend", RecTarget: "错的", RecContent: "错的",
	})

	loaded := LoadRecommendationState("")
	require.Len(t, loaded.FeedbackBuffer, 1)
	fb := loaded.FeedbackBuffer[0]
	assert.Equal(t, "task_reminder", fb["rec_type"], "应优先取 history 里的类型")
	assert.Equal(t, "整理报表", fb["rec_target"])
	assert.Equal(t, "该整理了", fb["rec_content"])
}

// TestRecordFeedback_history未写入用meta兜底 时序竞态下用调用方元数据填。
// Python: feedback_collector.py:100-113
func TestRecordFeedback_history未写入用meta兜底(t *testing.T) {
	resetRecommendationState(t)

	RecordExplicitFeedback("rec_unknown", "explicit_like", &RecMeta{
		RecType: "skill_recommend", RecTarget: "翻译技能", RecContent: "翻文档",
	})

	state := LoadRecommendationState("")
	require.Len(t, state.FeedbackBuffer, 1, "查不到 history 也必须记录，否则竞态丢反馈")
	assert.Equal(t, "skill_recommend", state.FeedbackBuffer[0]["rec_type"])
	assert.Equal(t, "翻译技能", state.FeedbackBuffer[0]["rec_target"])
}

// TestRecordFeedback_同rec显式只留第一条 二次点赞/踩被丢弃。
// Python: feedback_collector.py:118-137
func TestRecordFeedback_同rec显式只留第一条(t *testing.T) {
	resetRecommendationState(t)

	RecordExplicitFeedback("rec_dup", "explicit_like", nil)
	RecordExplicitFeedback("rec_dup", "explicit_dislike", nil)

	state := LoadRecommendationState("")
	require.Len(t, state.FeedbackBuffer, 1, "显式是单选语义，后续应丢弃")
	assert.Equal(t, "explicit_like", state.FeedbackBuffer[0]["feedback_type"], "保留第一条")
}

// TestRecordFeedback_同rec隐式只留第一条 后续回复被丢弃。
func TestRecordFeedback_同rec隐式只留第一条(t *testing.T) {
	resetRecommendationState(t)

	RecordImplicitFeedback("rec_imp", "第一条回复")
	RecordImplicitFeedback("rec_imp", "第二条回复")

	state := LoadRecommendationState("")
	require.Len(t, state.FeedbackBuffer, 1)
	assert.Equal(t, "第一条回复", state.FeedbackBuffer[0]["user_reply"])
}

// TestRecordFeedback_显式与隐式共存 两类信号互补不互斥，各留一条。
func TestRecordFeedback_显式与隐式共存(t *testing.T) {
	resetRecommendationState(t)

	RecordExplicitFeedback("rec_both", "explicit_like", nil)
	RecordImplicitFeedback("rec_both", "顺便说下这个挺好用")

	state := LoadRecommendationState("")
	require.Len(t, state.FeedbackBuffer, 2, "显式与隐式各留一条，互不排斥")
}

// TestRecordFeedback_不同rec都记录。
func TestRecordFeedback_不同rec都记录(t *testing.T) {
	resetRecommendationState(t)

	RecordExplicitFeedback("rec_a", "explicit_like", nil)
	RecordExplicitFeedback("rec_b", "explicit_dislike", nil)

	state := LoadRecommendationState("")
	assert.Len(t, state.FeedbackBuffer, 2)
}

// TestRecordFeedback_超容量截断 FIFO 保留最新 20 条。
// Python: feedback_collector.py:133
func TestRecordFeedback_超容量截断(t *testing.T) {
	resetRecommendationState(t)

	for i := 0; i < 25; i++ {
		RecordExplicitFeedback("rec_"+string(rune('a'+i)), "explicit_like", nil)
	}

	state := LoadRecommendationState("")
	assert.Len(t, state.FeedbackBuffer, feedbackBufferCapacity, "应截断到 20 条")
	// 最早的 5 条被丢弃
	assert.Equal(t, "rec_f", state.FeedbackBuffer[0]["rec_id"], "应保留最新 20 条")
}

// TestRecordFeedback_更新时间戳 记录后 LastUpdated 被刷新。
func TestRecordFeedback_更新时间戳(t *testing.T) {
	resetRecommendationState(t)

	RecordExplicitFeedback("rec_ts", "explicit_like", nil)
	state := LoadRecommendationState("")
	assert.NotEmpty(t, state.LastUpdated)
}

// ──────────────────────────── ConsumeFeedbackBuffer 测试分组 ────────────────────────────

// TestConsumeFeedbackBuffer 取出全部并清空。
// Python: feedback_collector.py:172
func TestConsumeFeedbackBuffer(t *testing.T) {
	t.Run("空buffer返回nil", func(t *testing.T) {
		assert.Nil(t, ConsumeFeedbackBuffer(&RecommendationState{}))
	})

	t.Run("nil state返回nil", func(t *testing.T) {
		assert.Nil(t, ConsumeFeedbackBuffer(nil))
	})

	t.Run("取出并清空", func(t *testing.T) {
		resetRecommendationState(t)
		RecordExplicitFeedback("rec_x", "explicit_like", nil)
		RecordImplicitFeedback("rec_y", "回复")

		state := LoadRecommendationState("")
		require.Len(t, state.FeedbackBuffer, 2)

		feedbacks := ConsumeFeedbackBuffer(state)
		assert.Len(t, feedbacks, 2)
		assert.Empty(t, state.FeedbackBuffer, "消费后内存态应清空")

		// 磁盘也应已清空
		reloaded := LoadRecommendationState("")
		assert.Empty(t, reloaded.FeedbackBuffer, "消费后磁盘应已清空")
	})
}

// ──────────────────────────── FindLatestRecommendation 测试分组 ────────────────────────────

// TestFindLatestRecommendation 按会话找最近一条推荐。
// Python: feedback_collector.py:199
func TestFindLatestRecommendation(t *testing.T) {
	t.Run("无history返回nil", func(t *testing.T) {
		resetRecommendationState(t)
		assert.Nil(t, FindLatestRecommendation("sess-x", 0))
	})

	t.Run("找到对应会话最新一条", func(t *testing.T) {
		resetRecommendationState(t)
		state := &RecommendationState{
			RecommendationHistory: []map[string]any{
				{"id": "r1", "session_id": "sess-a", "tick_at": 100.0},
				{"id": "r2", "session_id": "sess-b", "tick_at": 200.0},
				{"id": "r3", "session_id": "sess-a", "tick_at": 300.0},
			},
		}
		SaveRecommendationState(state, "")

		got := FindLatestRecommendation("sess-a", 0)
		require.NotNil(t, got)
		assert.Equal(t, "r3", got["id"], "应取该会话最新一条")
	})

	t.Run("会话无推荐返回nil", func(t *testing.T) {
		resetRecommendationState(t)
		state := &RecommendationState{
			RecommendationHistory: []map[string]any{
				{"id": "r1", "session_id": "sess-a", "tick_at": 100.0},
			},
		}
		SaveRecommendationState(state, "")

		assert.Nil(t, FindLatestRecommendation("sess-other", 0))
	})

	t.Run("maxAge过滤过期项", func(t *testing.T) {
		resetRecommendationState(t)
		state := &RecommendationState{
			RecommendationHistory: []map[string]any{
				{"id": "r_old", "session_id": "sess-a", "tick_at": 1000.0},
			},
		}
		SaveRecommendationState(state, "")

		got := FindLatestRecommendation("sess-a", 60)
		assert.Nil(t, got, "tick_at 远超时间窗，应返回 nil")
	})

	t.Run("缺tick_at按0处理不崩", func(t *testing.T) {
		resetRecommendationState(t)
		state := &RecommendationState{
			RecommendationHistory: []map[string]any{
				{"id": "r_no_tick", "session_id": "sess-a"},
			},
		}
		SaveRecommendationState(state, "")

		// maxAge=0 时 cutoff=0，tickAt=0 不小于 0，应返回该条
		got := FindLatestRecommendation("sess-a", 0)
		require.NotNil(t, got)
		assert.Equal(t, "r_no_tick", got["id"])
	})

	t.Run("tick_at类型异常时跳过", func(t *testing.T) {
		resetRecommendationState(t)
		state := &RecommendationState{
			RecommendationHistory: []map[string]any{
				{"id": "r_bad", "session_id": "sess-a", "tick_at": []any{1, 2}},
				{"id": "r_ok", "session_id": "sess-a", "tick_at": 300.0},
			},
		}
		SaveRecommendationState(state, "")

		got := FindLatestRecommendation("sess-a", 0)
		require.NotNil(t, got)
		assert.Equal(t, "r_ok", got["id"], "脏 tick_at 应跳过，继续找前一条")
	})
}

// ──────────────────────────── 内部辅助函数 ────────────────────────────

// TestIsExplicitFeedback 显式反馈类型判定。
func TestIsExplicitFeedback(t *testing.T) {
	assert.True(t, isExplicitFeedback("explicit_like"))
	assert.True(t, isExplicitFeedback("explicit_dislike"))
	assert.False(t, isExplicitFeedback("implicit"))
	assert.False(t, isExplicitFeedback(""))
	assert.False(t, isExplicitFeedback("unknown"))
}

// TestFirstNonEmpty 首个非空值。
func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "b", firstNonEmpty("", "b", "c"))
	assert.Equal(t, "a", firstNonEmpty("a", "b"))
	assert.Equal(t, "", firstNonEmpty("", "", ""))
	assert.Equal(t, "", firstNonEmpty())
}

// TestParseTickAt 各种类型解析。
func TestParseTickAt(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		want    float64
		wantErr bool
	}{
		{"float64", 1.5, 1.5, false},
		{"float32", float32(2.5), 2.5, false},
		{"int", 3, 3.0, false},
		{"int64", int64(4), 4.0, false},
		{"字符串数字", "5.5", 5.5, false},
		{"nil按0", nil, 0, false},
		{"字符串非数字", "abc", 0, true},
		{"切片", []any{1}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTickAt(tc.value)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// resetRecommendationState 清空磁盘状态，避免测试间污染。
//
// 注意：SaveRecommendationState 走 workspace 默认路径（进程级缓存），
// 故此处直接写空状态实现「重置」而非删文件。
func resetRecommendationState(t *testing.T) {
	t.Helper()
	SaveRecommendationState(&RecommendationState{}, "")
}
