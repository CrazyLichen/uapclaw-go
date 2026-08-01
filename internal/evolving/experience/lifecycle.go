package experience

import (
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LocalApplyPreview 本地应用预览合约（frozen，Go 用值语义）。
//
// 对应 Python: LocalApplyPreview
type LocalApplyPreview struct {
	// SkillName 技能名称
	SkillName string
	// Records 演进记录列表
	Records []checkpointing.EvolutionRecord
	// ApplyResults 应用结果列表
	ApplyResults []schema.ApplyResult
	// ChangeType 变更类型
	ChangeType string
	// LifecycleStage 生命周期阶段
	LifecycleStage string
}

// PendingCommitResult 暂存变更提交结果。
//
// 对应 Python: PendingCommitResult
type PendingCommitResult struct {
	// AppliedCount 已应用数量
	AppliedCount int
	// PendingCount 待定数量
	PendingCount int
}

// HostFacingExperienceResult host-facing 稳定形态结果合约。
//
// 对应 Python: HostFacingExperienceResult
type HostFacingExperienceResult struct {
	// SkillName 技能名称
	SkillName string
	// RequestID 请求标识
	RequestID *string
	// Effect 效果类型
 Effect string
	// ChangeType 变更类型
	ChangeType string
	// AppliedCount 已应用数量
	AppliedCount int
	// RejectedCount 已拒绝数量
	RejectedCount int
	// PendingCount 待定数量
	PendingCount int
	// Status 状态（pending_approval / persisted / partial / rejected）
	Status string
	// Errors 错误列表
	Errors []string
	// Metadata 附加元数据
	Metadata map[string]any
}

// RebuildRequest 技能重建请求参数。
//
// 对应 Python: RebuildRequest
type RebuildRequest struct {
	// SkillName 技能名称
	SkillName string
	// UserIntent 用户意图
	UserIntent *string
	// MinScore 最低分数阈值
	MinScore float64
	// Metadata 附加元数据
	Metadata map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// HostFacingExperienceResultPendingApproval 创建 pending_approval 状态的 HostFacingExperienceResult。
// 对应 Python: HostFacingExperienceResult.pending_approval()
func HostFacingExperienceResultPendingApproval(
	skillName string, requestID string, changeType string, pendingCount int,
) HostFacingExperienceResult {
	return HostFacingExperienceResult{
		SkillName:    skillName,
		RequestID:    strPtr(requestID),
		Effect:       schema.PendingChangeEffect,
		ChangeType:   changeType,
		PendingCount: pendingCount,
		Status:       "pending_approval",
	}
}

// HostFacingExperienceResultPersisted 创建 persisted/partial 状态的 HostFacingExperienceResult。
// 对应 Python: HostFacingExperienceResult.persisted()
func HostFacingExperienceResultPersisted(
	skillName string, requestID *string, changeType string,
	appliedCount int, pendingCount int, errors []string,
) HostFacingExperienceResult {
	status := "persisted"
	if pendingCount > 0 || len(errors) > 0 {
		status = "partial"
	}
	errs := errors
	if errs == nil {
		errs = []string{}
	}
	return HostFacingExperienceResult{
		SkillName:    skillName,
		RequestID:    requestID,
		Effect:       schema.StateEffect,
		ChangeType:   changeType,
		AppliedCount: appliedCount,
		PendingCount: pendingCount,
		Status:       status,
		Errors:       errs,
	}
}

// HostFacingExperienceResultRejected 创建 rejected 状态的 HostFacingExperienceResult。
// 对应 Python: HostFacingExperienceResult.rejected()
func HostFacingExperienceResultRejected(
	skillName string, requestID *string, changeType string, rejectedCount int,
) HostFacingExperienceResult {
	return HostFacingExperienceResult{
		SkillName:      skillName,
		RequestID:      requestID,
		Effect:         schema.StateEffect,
		ChangeType:     changeType,
		RejectedCount:  rejectedCount,
		Status:         "rejected",
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strPtr 将字符串转为 *string。
func strPtr(s string) *string {
	return &s
}
