package experience

import (
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionContext 在线/离线演进输入上下文。
//
// 对应 Python: EvolutionContext / OnlineEvolutionContext
type EvolutionContext struct {
	// SkillName 技能名称
	SkillName string
	// Signals 检测到的演进信号列表
	Signals []signal.EvolutionSignal
	// SkillContent 技能 SKILL.md 内容
	SkillContent string
	// Messages 对话消息列表
	Messages []map[string]any
	// ExistingDescRecords 现有描述层演进记录
	ExistingDescRecords []checkpointing.EvolutionRecord
	// ExistingBodyRecords 现有主体层演进记录
	ExistingBodyRecords []checkpointing.EvolutionRecord
	// UserQuery 用户查询
	UserQuery string
	// Trajectory 对话轨迹
	Trajectory *trajectory.Trajectory
	// ExistingScriptRecords 现有脚本层演进记录
	ExistingScriptRecords []checkpointing.EvolutionRecord
	// Metadata 附加元数据
	Metadata map[string]any
}

// OnlineEvolutionContext 类型别名，对齐 Python。
// 对应 Python: OnlineEvolutionContext = EvolutionContext
type OnlineEvolutionContext = EvolutionContext

// OnlineEvolutionStatus 在线演进结果状态（string 常量而非 iota 枚举）。
// 对应 Python: Literal["staged", "auto_approved", ...]
type OnlineEvolutionStatus = string

// PendingChange 等待审批的暂存演进记录快照。
//
// 类型别名，指向 checkpointing.PendingChange。
// Go 不允许 checkpointing ↔ experience 循环引用，
// 因此 PendingChange 的实际定义在 checkpointing 包中，
// experience 包通过类型别名提供等效访问。
//
// 对应 Python: openjiuwen/agent_evolving/experience/types.py PendingChange
type PendingChange = checkpointing.PendingChange

// ExperienceProposal 经验提案（审批前）。
//
// 对应 Python: ExperienceProposal
type ExperienceProposal struct {
	// SkillName 技能名称
	SkillName string
	// Records 提案中的演进记录列表
	Records []checkpointing.EvolutionRecord
	// RequiresApproval 是否需要审批
	RequiresApproval bool
	// Source 来源标识
	Source string
	// UserQuery 用户查询
	UserQuery string
	// SignalType 信号类型
	SignalType *string
	// SignalSource 信号来源
	SignalSource *string
}

// ExperienceApprovalRequest 审批面向视图。
//
// 对应 Python: ExperienceApprovalRequest
type ExperienceApprovalRequest struct {
	// SkillName 技能名称
	SkillName string
	// Proposal 经验提案
	Proposal ExperienceProposal
	// PendingChange 暂存变更
	PendingChange *checkpointing.PendingChange
	// RequestID 请求标识
	RequestID *string
	// ApplyResults 应用结果列表
	ApplyResults []schema.ApplyResult
}

// OnlineEvolutionResult 在线演进编排器返回的结构化结果。
//
// 对应 Python: OnlineEvolutionResult
type OnlineEvolutionResult struct {
	// SkillName 技能名称
	SkillName string
	// Status 在线演进状态
	Status OnlineEvolutionStatus
	// Request 审批请求（staged 时有值）
	Request *ExperienceApprovalRequest
	// Message 附加消息
	Message string
}

// ExperienceApplyResult 经验变更应用结果。
//
// 对应 Python: ExperienceApplyResult
type ExperienceApplyResult struct {
	// SkillName 技能名称
	SkillName string
	// AppliedCount 已应用数量
	AppliedCount int
	// RejectedCount 已拒绝数量
	RejectedCount int
	// PendingCount 待定数量
	PendingCount int
	// Errors 错误列表
	Errors []string
	// Metadata 附加元数据
	Metadata map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// OnlineEvolutionStatusStaged 已暂存待审批
	OnlineEvolutionStatusStaged OnlineEvolutionStatus = "staged"
	// OnlineEvolutionStatusAutoApproved 自动审批通过
	OnlineEvolutionStatusAutoApproved OnlineEvolutionStatus = "auto_approved"
	// OnlineEvolutionStatusNoEvolutionNoRecords 无演进无记录
	OnlineEvolutionStatusNoEvolutionNoRecords OnlineEvolutionStatus = "no_evolution_no_records"
	// OnlineEvolutionStatusSkippedNoInput 跳过（无输入）
	OnlineEvolutionStatusSkippedNoInput OnlineEvolutionStatus = "skipped_no_input"
	// OnlineEvolutionStatusSkippedSkillNotFound 跳过（技能不存在）
	OnlineEvolutionStatusSkippedSkillNotFound OnlineEvolutionStatus = "skipped_skill_not_found"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RecordCount 返回提案中的记录数量。
// 对应 Python: ExperienceProposal.record_count
func (p *ExperienceProposal) RecordCount() int {
	return len(p.Records)
}

// Ok 判断应用结果是否成功。
// 对应 Python: ExperienceApplyResult.ok
func (r *ExperienceApplyResult) Ok() bool {
	return len(r.Errors) == 0 && r.PendingCount == 0
}

// ToHostResult 返回 host-facing 稳定形态。
// 对应 Python: ExperienceApprovalRequest.to_host_result()
func (r *ExperienceApprovalRequest) ToHostResult() HostFacingExperienceResult {
	pendingCount := 0
	changeType := schema.SkillExperienceEntry
	if r.PendingChange != nil {
		pendingCount = len(r.PendingChange.Payload)
		changeType = r.PendingChange.ChangeType
	}
	requestID := ""
	if r.RequestID != nil {
		requestID = *r.RequestID
	}
	return HostFacingExperienceResultPendingApproval(
		r.SkillName, requestID, changeType, pendingCount,
	)
}

// ToHostResult 返回 host-facing 稳定形态。
// 对应 Python: ExperienceApplyResult.to_host_result()
func (r *ExperienceApplyResult) ToHostResult(requestID *string, changeType string) HostFacingExperienceResult {
	if r.RejectedCount > 0 {
		return HostFacingExperienceResultRejected(r.SkillName, requestID, changeType, r.RejectedCount)
	}
	return HostFacingExperienceResultPersisted(r.SkillName, requestID, changeType, r.AppliedCount, r.PendingCount, r.Errors)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
