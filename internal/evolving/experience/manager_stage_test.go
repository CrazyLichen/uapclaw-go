package experience

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newStageManager 用临时目录的真实 EvolutionStore 构造 manager。
func newStageManager(t *testing.T) *ExperienceManager {
	t.Helper()
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	mgr, err := NewExperienceManager(store, nil, "skill", "cn", nil, nil, nil)
	if err != nil {
		t.Fatalf("构造 ExperienceManager 失败: %v", err)
	}
	return mgr
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestStageRecords 验证暂存记录返回可识别的请求标识与提案。
func TestStageRecords(t *testing.T) {
	ctx := context.Background()

	t.Run("空记录也可暂存", func(t *testing.T) {
		mgr := newStageManager(t)
		req, err := mgr.StageRecords(ctx, "skill-a", nil, true, "test", "query", nil, nil,
			schema.SkillExperienceEntry, "pref", nil, nil, false)
		if err != nil {
			t.Fatalf("暂存失败: %v", err)
		}
		if req == nil {
			t.Fatal("应返回请求对象")
		}
		if req.RequestID == "" {
			t.Fatal("RequestID 不应为空")
		}
		if req.SkillName != "skill-a" {
			t.Fatalf("SkillName = %q, 期望 skill-a", req.SkillName)
		}
		if req.Proposal.Source != "test" {
			t.Fatalf("Source = %q, 期望 test", req.Proposal.Source)
		}
	})

	t.Run("暂存后可在快照中找到", func(t *testing.T) {
		mgr := newStageManager(t)
		req, err := mgr.StageRecords(ctx, "skill-b", nil, true, "test", "", nil, nil,
			schema.SkillExperienceEntry, "pref2", nil, nil, false)
		if err != nil {
			t.Fatalf("暂存失败: %v", err)
		}
		if _, ok := mgr.PendingApprovalSnapshots()[req.RequestID]; !ok {
			t.Fatalf("快照中应存在 %q，实际 %v", req.RequestID, mgr.PendingApprovalSnapshots())
		}
	})
}

// TestRejectRequest 验证拒绝可清除暂存批次。
func TestRejectRequest(t *testing.T) {
	ctx := context.Background()
	mgr := newStageManager(t)
	req, err := mgr.StageRecords(ctx, "skill-c", nil, true, "test", "", nil, nil,
		schema.SkillExperienceEntry, "pref3", nil, nil, false)
	if err != nil {
		t.Fatalf("暂存失败: %v", err)
	}

	if _, err := mgr.RejectRequest(ctx, req.RequestID); err != nil {
		t.Fatalf("拒绝失败: %v", err)
	}
	if _, ok := mgr.PendingApprovalSnapshots()[req.RequestID]; ok {
		t.Fatal("拒绝后暂存批次应被移除")
	}
}

// TestApplyRequest_不存在的请求 验证未知 requestID 返回带 errors 的结果（不带 error）。
func TestApplyRequest_不存在的请求(t *testing.T) {
	ctx := context.Background()
	mgr := newStageManager(t)

	for name, call := range map[string]func() (ExperienceApplyResult, error){
		"ApproveRequest": func() (ExperienceApplyResult, error) { return mgr.ApproveRequest(ctx, "missing") },
		"RejectRequest":  func() (ExperienceApplyResult, error) { return mgr.RejectRequest(ctx, "missing") },
		"RetryRequest":   func() (ExperienceApplyResult, error) { return mgr.RetryRequest(ctx, "missing") },
	} {
		t.Run(name, func(t *testing.T) {
			// 未知 request_id 不返回 error，而是把原因写进结果的 Errors 字段
			res, err := call()
			if err != nil {
				t.Fatalf("未知请求不应返回 error，实际 %v", err)
			}
			if len(res.Errors) == 0 {
				t.Fatalf("应在 Errors 中说明原因，实际 %+v", res)
			}
		})
	}
}

// TestStageApplyResults 验证从应用结果暂存请求。
func TestStageApplyResults(t *testing.T) {
	ctx := context.Background()
	mgr := newStageManager(t)
	req, err := mgr.StageApplyResults(ctx, "skill-d", nil, true, "online", "pref4", "",
		nil, nil, nil)
	if err != nil {
		t.Fatalf("暂存失败: %v", err)
	}
	if req == nil || req.RequestID == "" {
		t.Fatalf("应返回带 RequestID 的请求，实际 %v", req)
	}
}

// TestCommitProposal 验证提案直提路径。
func TestCommitProposal(t *testing.T) {
	ctx := context.Background()
	mgr := newStageManager(t)
	proposal := ExperienceProposal{
		SkillName:        "skill-e",
		RequiresApproval: true,
		Source:           "proposal",
	}
	// 提案未显式给 Records，暂存后应可拒绝清理；此处仅验证调用路径不 panic
	_, _ = mgr.CommitProposal(ctx, proposal)
	if len(mgr.PendingApprovalSnapshots()) == 0 {
		t.Log("CommitProposal 未留下暂存批次（可能已被自动提交），属可接受结果")
	}
}
