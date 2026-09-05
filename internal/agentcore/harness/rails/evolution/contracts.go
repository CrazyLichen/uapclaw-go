package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionHostEventMeta 演化事件元数据，携带于 OutputSchema.Payload["_evolution_meta"] 中。
type EvolutionHostEventMeta struct {
	// EventKind 事件类型
	EventKind EvolutionEventKind
	// RailKind 轨道类型（可选）
	RailKind *string
	// Stage 阶段（可选）
	Stage *string
	// SkillName 技能名称（可选）
	SkillName *string
	// RequestID 请求标识（可选）
	RequestID *string
	// SignalType 信号类型（可选）
	SignalType *string
	// Source 来源（可选）
	Source *string
	// Status 状态（可选）
	Status *string
}

// EvolutionSnapshot 异步演化快照，在回调上下文仍活跃时捕获。
type EvolutionSnapshot struct {
	// Trajectory 对话轨迹
	Trajectory *trajectory.Trajectory
	// Messages 消息列表
	Messages []map[string]any
	// SkillName 技能名称（可选）
	SkillName *string
}

// EvolutionRequestResult 主动用户触发的演化 API 返回的结构化结果。
type EvolutionRequestResult struct {
	// SkillName 技能名称
	SkillName string
	// RequestID 请求标识（可选）
	RequestID *string
	// ApprovalEvent 审批事件（可选）
	ApprovalEvent *stream.OutputSchema
	// Records 演进记录列表
	Records []checkpointing.EvolutionRecord
	// AutoApproved 是否自动审批
	AutoApproved bool
}

// SimplifyRequestResult 主动精简请求 API 返回的结构化结果。
type SimplifyRequestResult struct {
	// SkillName 技能名称
	SkillName string
	// RequestID 请求标识（可选）
	RequestID *string
	// ApprovalEvent 审批事件（可选）
	ApprovalEvent *stream.OutputSchema
	// Actions 精简操作列表
	Actions []map[string]any
}

// TeamSkillQuestion 团队技能审批问题。
type TeamSkillQuestion struct {
	// Section 章节
	Section string
	// Content 内容
	Content string
}

// ApprovalManager 审批管理器窄接口。
//
// ExperienceManager 隐式满足此接口：
//   - ApproveRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) ✅
//   - RejectRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) ✅
type ApprovalManager interface {
	// ApproveRequest 持久化或应用一个暂存的审批请求。
	ApproveRequest(ctx context.Context, requestID string) (experience.ExperienceApplyResult, error)
	// RejectRequest 拒绝一个暂存的审批请求。
	RejectRequest(ctx context.Context, requestID string) (experience.ExperienceApplyResult, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// EvolutionEventKind 演化事件类型。
type EvolutionEventKind = string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EvolutionEventKindApproval 审批事件
	EvolutionEventKindApproval EvolutionEventKind = "approval"
	// EvolutionEventKindProgress 进度事件
	EvolutionEventKindProgress EvolutionEventKind = "progress"
	// EvolutionEventKindOutcome 结果事件
	EvolutionEventKindOutcome EvolutionEventKind = "outcome"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// PendingApprovalSnapshotStore 暂存审批快照映射。
type PendingApprovalSnapshotStore = map[string]*experience.PendingChange

// ──────────────────────────── 导出函数 ────────────────────────────

// HasChanges 是否有变更。
func (r EvolutionRequestResult) HasChanges() bool {
	return len(r.Records) > 0 || r.ApprovalEvent != nil
}

// HasChanges 是否有变更。
func (r SimplifyRequestResult) HasChanges() bool {
	return len(r.Actions) > 0 || r.ApprovalEvent != nil
}

// ToPayload 返回 JSON payload 形态，跳过空字段。
func (m EvolutionHostEventMeta) ToPayload() map[string]string {
	payload := map[string]string{"event_kind": m.EventKind}
	if m.RailKind != nil {
		payload["rail_kind"] = *m.RailKind
	}
	if m.Stage != nil {
		payload["stage"] = *m.Stage
	}
	if m.SkillName != nil {
		payload["skill_name"] = *m.SkillName
	}
	if m.RequestID != nil {
		payload["request_id"] = *m.RequestID
	}
	if m.SignalType != nil {
		payload["signal_type"] = *m.SignalType
	}
	if m.Source != nil {
		payload["source"] = *m.Source
	}
	if m.Status != nil {
		payload["status"] = *m.Status
	}
	return payload
}

// ToLegacyDict 返回供轨道钩子和测试使用的 dict 形态。
func (s EvolutionSnapshot) ToLegacyDict() map[string]any {
	snapshot := map[string]any{
		"trajectory": s.Trajectory,
		"messages":   s.Messages,
	}
	if s.SkillName != nil {
		snapshot["skill_name"] = *s.SkillName
	}
	return snapshot
}

// FromLegacyDict 从 dict 恢复 EvolutionSnapshot。
func FromLegacyDict(snapshot map[string]any) EvolutionSnapshot {
	var traj *trajectory.Trajectory
	if t, ok := snapshot["trajectory"]; ok {
		if typed, ok := t.(*trajectory.Trajectory); ok {
			traj = typed
		}
	}

	messages := []map[string]any{}
	if m, ok := snapshot["messages"]; ok {
		if typed, ok := m.([]map[string]any); ok {
			messages = typed
		}
	}

	var skillName *string
	if sn, ok := snapshot["skill_name"]; ok {
		if typed, ok := sn.(string); ok {
			skillName = &typed
		}
	}

	return EvolutionSnapshot{
		Trajectory: traj,
		Messages:   messages,
		SkillName:  skillName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
