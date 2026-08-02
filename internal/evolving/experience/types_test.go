package experience

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExperienceProposal_RecordCount 测试 RecordCount 方法
func TestExperienceProposal_RecordCount(t *testing.T) {
	t.Run("有记录", func(t *testing.T) {
		p := ExperienceProposal{
			Records: []checkpointing.EvolutionRecord{
				{ID: "ev_001"}, {ID: "ev_002"}, {ID: "ev_003"},
			},
		}
		if p.RecordCount() != 3 {
			t.Errorf("RecordCount() = %d, 期望 3", p.RecordCount())
		}
	})
	t.Run("无记录", func(t *testing.T) {
		p := ExperienceProposal{Records: nil}
		if p.RecordCount() != 0 {
			t.Errorf("RecordCount() = %d, 期望 0", p.RecordCount())
		}
	})
}

// TestExperienceApplyResult_Ok 测试 Ok 方法
func TestExperienceApplyResult_Ok(t *testing.T) {
	t.Run("全部成功", func(t *testing.T) {
		r := ExperienceApplyResult{AppliedCount: 3, PendingCount: 0, Errors: nil}
		if !r.Ok() {
			t.Errorf("Ok() = false, 期望 true")
		}
	})
	t.Run("有错误", func(t *testing.T) {
		r := ExperienceApplyResult{AppliedCount: 2, Errors: []string{"append failed"}}
		if r.Ok() {
			t.Errorf("Ok() = true, 期望 false（有错误）")
		}
	})
	t.Run("有待定", func(t *testing.T) {
		r := ExperienceApplyResult{AppliedCount: 2, PendingCount: 1, Errors: nil}
		if r.Ok() {
			t.Errorf("Ok() = true, 期望 false（有待定）")
		}
	})
}

// TestExperienceApprovalRequest_ToHostResult 测试审批请求的 ToHostResult
func TestExperienceApprovalRequest_ToHostResult(t *testing.T) {
	t.Run("有 PendingChange", func(t *testing.T) {
		pending := &checkpointing.PendingChange{
			ChangeType: schema.SkillExperienceEntry,
			Payload:    []checkpointing.EvolutionRecord{{ID: "r1"}, {ID: "r2"}},
		}
		r := ExperienceApprovalRequest{SkillName: "test_skill", PendingChange: pending, RequestID: "req_001"}
		result := r.ToHostResult()
		if result.SkillName != "test_skill" {
			t.Errorf("SkillName = %s, 期望 test_skill", result.SkillName)
		}
		if result.ChangeType != schema.SkillExperienceEntry {
			t.Errorf("ChangeType = %s, 期望 %s", result.ChangeType, schema.SkillExperienceEntry)
		}
		if result.PendingCount != 2 {
			t.Errorf("PendingCount = %d, 期望 2", result.PendingCount)
		}
		if result.Effect != schema.PendingChangeEffect {
			t.Errorf("Effect = %s, 期望 %s", result.Effect, schema.PendingChangeEffect)
		}
		if result.Status != "pending_approval" {
			t.Errorf("Status = %s, 期望 pending_approval", result.Status)
		}
	})
	t.Run("无 PendingChange", func(t *testing.T) {
		r := ExperienceApprovalRequest{SkillName: "test_skill", PendingChange: nil, RequestID: "req_002"}
		result := r.ToHostResult()
		if result.PendingCount != 0 {
			t.Errorf("PendingCount = %d, 期望 0", result.PendingCount)
		}
	})
	t.Run("空 RequestID", func(t *testing.T) {
		pending := &checkpointing.PendingChange{ChangeType: schema.SkillExperienceEntry, Payload: []checkpointing.EvolutionRecord{{ID: "r1"}}}
		r := ExperienceApprovalRequest{SkillName: "test_skill", PendingChange: pending, RequestID: ""}
		result := r.ToHostResult()
		if result.RequestID != "" {
			t.Errorf("RequestID 应为空字符串, 实际 %s", result.RequestID)
		}
	})
}

// TestExperienceApplyResult_ToHostResult 测试应用结果的 ToHostResult
func TestExperienceApplyResult_ToHostResult(t *testing.T) {
	t.Run("全部通过", func(t *testing.T) {
		r := ExperienceApplyResult{SkillName: "test_skill", AppliedCount: 5, PendingCount: 0, RejectedCount: 0}
		result := r.ToHostResult("req_003", schema.SkillExperienceEntry)
		if result.Status != "persisted" {
			t.Errorf("Status = %s, 期望 persisted", result.Status)
		}
		if result.AppliedCount != 5 {
			t.Errorf("AppliedCount = %d, 期望 5", result.AppliedCount)
		}
	})
	t.Run("部分通过", func(t *testing.T) {
		r := ExperienceApplyResult{SkillName: "test_skill", AppliedCount: 3, PendingCount: 2, RejectedCount: 0, Errors: nil}
		result := r.ToHostResult("req_004", schema.SkillExperienceEntry)
		if result.Status != "partial" {
			t.Errorf("Status = %s, 期望 partial（有 pending）", result.Status)
		}
	})
	t.Run("有拒绝", func(t *testing.T) {
		r := ExperienceApplyResult{SkillName: "test_skill", RejectedCount: 3}
		result := r.ToHostResult("req_005", schema.SkillExperienceEntry)
		if result.Status != "rejected" {
			t.Errorf("Status = %s, 期望 rejected", result.Status)
		}
		if result.RejectedCount != 3 {
			t.Errorf("RejectedCount = %d, 期望 3", result.RejectedCount)
		}
	})
}

// TestOnlineEvolutionStatus 测试状态常量值
func TestOnlineEvolutionStatus(t *testing.T) {
	tests := []struct {
		name   string
		status OnlineEvolutionStatus
		expect string
	}{
		{"Staged", OnlineEvolutionStatusStaged, "staged"},
		{"AutoApproved", OnlineEvolutionStatusAutoApproved, "auto_approved"},
		{"NoEvolutionNoRecords", OnlineEvolutionStatusNoEvolutionNoRecords, "no_evolution_no_records"},
		{"SkippedNoInput", OnlineEvolutionStatusSkippedNoInput, "skipped_no_input"},
		{"SkippedSkillNotFound", OnlineEvolutionStatusSkippedSkillNotFound, "skipped_skill_not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.status != tt.expect {
				t.Errorf("状态 %s = %s, 期望 %s", tt.name, tt.status, tt.expect)
			}
		})
	}
}
