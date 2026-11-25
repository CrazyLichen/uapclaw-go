package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExperienceSubmissionServiceAdapter ExperienceSubmissionService 的
// EvolutionSubmissionService 接口缝适配器（15.21 接缝真实现接线）。
// Python: approval_submission_service 返回 manager.experience_submission_service
// （skill_evolution_rail.py:673）——中断 rail preflight 直接调用服务方法。
//
// 2026-12-24 15.24b：随 submission.go 真实现落地，替换原 nil 占位；
// 偏差登记（spec「已知偏差」）：PrepareEvolveSubmission 语义 = runtime
// ResolveSelectedProposals + 服务 ValidateExperienceDrafts（Python 为
// 服务方法内部持有 runtime 引用，Go 由适配器组合，归属不变）。
type ExperienceSubmissionServiceAdapter struct {
	svc *experience.ExperienceSubmissionService
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExperienceSubmissionServiceAdapter 构造适配器：绑定 store 与调用方
// 持有的 pending 快照表（与 ExperienceManager 共用同一张表）。
// Python: manager.experience_submission_service 与 manager 共享
// _pending_approval_snapshots。
func NewExperienceSubmissionServiceAdapter(
	store *checkpointing.EvolutionStore,
	pendingApprovalSnapshots map[string]*checkpointing.PendingChange,
) *ExperienceSubmissionServiceAdapter {
	svc := experience.NewExperienceSubmissionService(store)
	svc.BindPendingApprovalSnapshots(pendingApprovalSnapshots)
	return &ExperienceSubmissionServiceAdapter{svc: svc}
}

// PrepareEvolveSubmission 预检 evolve 提交（不消费 ref）。
// Python: prepare_evolve_submission (submission.py:143-160)。
func (a *ExperienceSubmissionServiceAdapter) PrepareEvolveSubmission(
	reviewRuntime *review.EvolutionReviewRuntime,
	evolutionReviewRef string,
	subject map[string]any,
	selectedProposalIDs []string,
	sessionID string,
) (*review.EvolutionProposalSelection, error) {
	selection, err := reviewRuntime.ResolveSelectedProposals(evolutionReviewRef, subject, selectedProposalIDs, sessionID)
	if err != nil {
		return nil, err
	}
	drafts := make([]any, 0, len(selection.ExperienceDrafts))
	for _, item := range selection.ExperienceDrafts {
		drafts = append(drafts, item)
	}
	if _, _, err := a.svc.ValidateExperienceDrafts(context.Background(), subject, drafts); err != nil {
		return nil, err
	}
	return &selection, nil
}

// PrepareSimplifySubmission 预检 simplify 提交（不执行）。
// Python: prepare_simplify_submission (submission.py:201-207)。
func (a *ExperienceSubmissionServiceAdapter) PrepareSimplifySubmission(subject map[string]any, actions []map[string]any) error {
	anyActions := make([]any, 0, len(actions))
	for _, action := range actions {
		anyActions = append(anyActions, action)
	}
	return a.svc.PrepareSimplifySubmission(context.Background(), subject, anyActions)
}
