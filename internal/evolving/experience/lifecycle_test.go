package experience

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHostFacingExperienceResult_PendingApproval 测试 pending_approval 工厂方法
func TestHostFacingExperienceResult_PendingApproval(t *testing.T) {
	result := HostFacingExperienceResultPendingApproval("test_skill", "req_001", schema.SkillExperienceEntry, 3)
	if result.SkillName != "test_skill" {
		t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
	}
	if result.Effect != schema.PendingChangeEffect {
		t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.PendingChangeEffect)
	}
	if result.ChangeType != schema.SkillExperienceEntry {
		t.Errorf("ChangeType = %s, 期望 %s", result.ChangeType, schema.SkillExperienceEntry)
	}
	if result.PendingCount != 3 {
		t.Errorf("PendingCount = %d, 期望 3", result.PendingCount)
	}
	if result.Status != "pending_approval" {
		t.Errorf("Status = %s, 期望 pending_approval", result.Status)
	}
}

// TestHostFacingExperienceResult_Persisted 测试 persisted 工厂方法
func TestHostFacingExperienceResult_Persisted(t *testing.T) {
	t.Run("全部成功", func(t *testing.T) {
		result := HostFacingExperienceResultPersisted("test_skill", "req_002", schema.SkillExperienceEntry, 5, 0, nil)
		if result.Status != "persisted" {
			t.Errorf("Status = %s, 期望 persisted", result.Status)
		}
		if result.AppliedCount != 5 {
			t.Errorf("AppliedCount = %d, 期望 5", result.AppliedCount)
		}
		if result.Effect != schema.StateEffect {
			t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.StateEffect)
		}
	})
	t.Run("部分成功", func(t *testing.T) {
		result := HostFacingExperienceResultPersisted("test_skill", "req_003", schema.SkillExperienceEntry, 3, 2, []string{"err1"})
		if result.Status != "partial" {
			t.Errorf("Status = %s, 期望 partial（有 pending+errors）", result.Status)
		}
	})
	t.Run("nil errors 变空列表", func(t *testing.T) {
		result := HostFacingExperienceResultPersisted("test_skill", "req_006", schema.SkillExperienceEntry, 2, 0, nil)
		if result.Errors == nil {
			t.Errorf("Errors 应为非 nil 空列表")
		}
		if len(result.Errors) != 0 {
			t.Errorf("Errors 镀度 = %d, 期望 0", len(result.Errors))
		}
	})
}

// TestHostFacingExperienceResult_Rejected 测试 rejected 工厂方法
func TestHostFacingExperienceResult_Rejected(t *testing.T) {
	result := HostFacingExperienceResultRejected("test_skill", "req_004", schema.SkillExperienceEntry, 3)
	if result.Status != "rejected" {
		t.Errorf("Status = %s, 期望 rejected", result.Status)
	}
	if result.RejectedCount != 3 {
		t.Errorf("RejectedCount = %d, 期望 3", result.RejectedCount)
	}
	if result.Effect != schema.StateEffect {
		t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.StateEffect)
	}
}
