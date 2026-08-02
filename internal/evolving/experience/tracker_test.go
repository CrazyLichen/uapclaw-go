package experience

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExperienceTracker_ConsumeEvalState 测试评估状态消费
func TestExperienceTracker_ConsumeEvalState(t *testing.T) {
	t.Run("达到间隔返回记录", func(t *testing.T) {
		sessionPresentedRecords["test_sess1"] = []PresentedRecordEntry{{SkillName: "skill1", Snippet: "snippet1"}}
		sessionEvalCounter["test_sess1"] = 2
		tracker := &ExperienceTracker{evalInterval: 3}
		result := tracker.ConsumeEvalState("test_sess1")
		if len(result) != 1 {
			t.Errorf("ConsumeEvalState 镀度 = %d, 期望 1", len(result))
		}
		if sessionEvalCounter["test_sess1"] != 0 {
			t.Errorf("counter 应被重置为 0, 实际 %d", sessionEvalCounter["test_sess1"])
		}
		delete(sessionPresentedRecords, "test_sess1")
		delete(sessionEvalCounter, "test_sess1")
	})
	t.Run("未达间隔返回空", func(t *testing.T) {
		sessionEvalCounter["test_sess2"] = 0
		tracker := &ExperienceTracker{evalInterval: 3}
		result := tracker.ConsumeEvalState("test_sess2")
		if len(result) != 0 {
			t.Errorf("ConsumeEvalState 镀度 = %d, 期望 0", len(result))
		}
		if sessionEvalCounter["test_sess2"] != 1 {
			t.Errorf("counter 应为 1, 实际 %d", sessionEvalCounter["test_sess2"])
		}
		delete(sessionEvalCounter, "test_sess2")
	})
}

// TestExperienceTracker_ClearSession 测试会话清理
func TestExperienceTracker_ClearSession(t *testing.T) {
	sessionPresentedRecords["test_sess3"] = []PresentedRecordEntry{{SkillName: "skill1"}}
	sessionEvalCounter["test_sess3"] = 5
	tracker := &ExperienceTracker{}
	tracker.ClearSession("test_sess3")
	if sessionPresentedRecords["test_sess3"] != nil {
		t.Errorf("sessionPresentedRecords 应被清理")
	}
	if sessionEvalCounter["test_sess3"] != 0 {
		t.Errorf("sessionEvalCounter 应为 0")
	}
}

// TestNewExperienceTracker 测试跟踪器创建
func TestNewExperienceTracker(t *testing.T) {
	tracker := NewExperienceTracker(nil, nil, 5)
	if tracker.evalInterval != 5 {
		t.Errorf("evalInterval = %d, 期望 5", tracker.evalInterval)
	}
}

// TestRecordScoreUpdate_ToMap 测试 RecordScoreUpdate 转换为 map
func TestRecordScoreUpdate_ToMap(t *testing.T) {
	t.Run("完整字段", func(t *testing.T) {
		update := &RecordScoreUpdate{Score: 0.85, UsageStats: map[string]any{"times_used": 3}}
		m := update.ToMap()
		if m["score"] != 0.85 {
			t.Errorf("score = %v, 期望 0.85", m["score"])
		}
		if m["usage_stats"] == nil {
			t.Errorf("usage_stats 应存在")
		}
	})
	t.Run("零值 Score 和 nil UsageStats", func(t *testing.T) {
		update := &RecordScoreUpdate{Score: 0, UsageStats: nil}
		m := update.ToMap()
		if m["score"] != float64(0) {
			t.Errorf("score = %v, 期望 0", m["score"])
		}
		// UsageStats 为 nil：Go 的 map[string]any(nil) 作为 any 存入 map 后，
		// 取出时为 typed nil（interface 有 type 但 value 为 nil），
		// 与 untyped nil 不等。此处验证实际行为：usage_stats 值为 typed nil map
		raw := m["usage_stats"]
		if raw == nil {
			// 如果是 untyped nil，说明 ToMap 没存入 usage_stats 键
			t.Errorf("usage_stats 键应存在")
		}
		stats, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("usage_stats 类型应为 map[string]any, 实际 %T", raw)
		}
		if stats != nil {
			t.Errorf("usage_stats 值应为 nil map, 实际 %v", stats)
		}
	})
}

// TestUpdatesToMap 测试 updatesToMap 转换
func TestUpdatesToMap(t *testing.T) {
	t.Run("非空 map", func(t *testing.T) {
		updates := map[string]*RecordScoreUpdate{
			"rec1": &RecordScoreUpdate{Score: 0.75, UsageStats: map[string]any{"times_used": 2}},
		}
		result := updatesToMap(updates)
		if len(result) != 1 {
			t.Errorf("updatesToMap 镀度 = %d, 期望 1", len(result))
		}
		if result["rec1"]["score"] != 0.75 {
			t.Errorf("score = %v, 期望 0.75", result["rec1"]["score"])
		}
	})
	t.Run("空 map", func(t *testing.T) {
		result := updatesToMap(nil)
		if len(result) != 0 {
			t.Errorf("updatesToMap(nil) 镀度 = %d, 期望 0", len(result))
		}
	})
}
