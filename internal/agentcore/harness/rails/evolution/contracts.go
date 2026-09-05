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
// 对齐 Python: EvolutionHostEventMeta (frozen dataclass)
type EvolutionHostEventMeta struct {
	// EventKind 事件类型
	// 对齐 Python: event_kind: EvolutionEventKind
	EventKind EvolutionEventKind
	// RailKind 轨道类型（可选）
	// 对齐 Python: rail_kind: Optional[str] = None
	RailKind *string
	// Stage 阶段（可选）
	// 对齐 Python: stage: Optional[str] = None
	Stage *string
	// SkillName 技能名称（可选）
	// 对齐 Python: skill_name: Optional[str] = None
	SkillName *string
	// RequestID 请求标识（可选）
	// 对齐 Python: request_id: Optional[str] = None
	RequestID *string
	// SignalType 信号类型（可选）
	// 对齐 Python: signal_type: Optional[str] = None
	SignalType *string
	// Source 来源（可选）
	// 对齐 Python: source: Optional[str] = None
	Source *string
	// Status 状态（可选）
	// 对齐 Python: status: Optional[str] = None
	Status *string
}

// EvolutionSnapshot 异步演化快照，在回调上下文仍活跃时捕获。
// 对齐 Python: EvolutionSnapshot (frozen dataclass)
type EvolutionSnapshot struct {
	// Trajectory 对话轨迹
	// 对齐 Python: trajectory: Trajectory
	Trajectory *trajectory.Trajectory
	// Messages 消息列表
	// 对齐 Python: messages: list[dict]
	Messages []map[string]any
	// SkillName 技能名称（可选）
	// 对齐 Python: skill_name: Optional[str] = None
	SkillName *string
}

// EvolutionRequestResult 主动用户触发的演化 API 返回的结构化结果。
// 对齐 Python: EvolutionRequestResult (frozen dataclass)
type EvolutionRequestResult struct {
	// SkillName 技能名称
	// 对齐 Python: skill_name: str
	SkillName string
	// RequestID 请求标识（可选）
	// 对齐 Python: request_id: Optional[str] = None
	RequestID *string
	// ApprovalEvent 审批事件（可选）
	// 对齐 Python: approval_event: Optional[OutputSchema] = None
	ApprovalEvent *stream.OutputSchema
	// Records 演进记录列表
	// 对齐 Python: records: list[EvolutionRecord] = field(default_factory=list)
	Records []checkpointing.EvolutionRecord
	// AutoApproved 是否自动审批
	// 对齐 Python: auto_approved: bool = False
	AutoApproved bool
}

// SimplifyRequestResult 主动精简请求 API 返回的结构化结果。
// 对齐 Python: SimplifyRequestResult (frozen dataclass)
type SimplifyRequestResult struct {
	// SkillName 技能名称
	// 对齐 Python: skill_name: str
	SkillName string
	// RequestID 请求标识（可选）
	// 对齐 Python: request_id: Optional[str] = None
	RequestID *string
	// ApprovalEvent 审批事件（可选）
	// 对齐 Python: approval_event: Optional[OutputSchema] = None
	ApprovalEvent *stream.OutputSchema
	// Actions 精简操作列表
	// 对齐 Python: actions: list[dict[str, Any]] = field(default_factory=list)
	Actions []map[string]any
}

// TeamSkillQuestion 团队技能审批问题。
// 对齐 Python: _build_team_skill_experience_question_event 的 questions 输入
type TeamSkillQuestion struct {
	// Section 章节
	Section string
	// Content 内容
	Content string
}

// ApprovalManager 审批管理器窄接口。
// 对齐 Python: ApprovalManagerProtocol (Protocol)
//
// ExperienceManager 隐式满足此接口：
//   - ApproveRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) ✅
//   - RejectRequest(ctx context.Context, requestID string) (ExperienceApplyResult, error) ✅
type ApprovalManager interface {
	// ApproveRequest 持久化或应用一个暂存的审批请求。
	// 对齐 Python: ApprovalManagerProtocol.approve_request
	ApproveRequest(ctx context.Context, requestID string) (experience.ExperienceApplyResult, error)
	// RejectRequest 拒绝一个暂存的审批请求。
	// 对齐 Python: ApprovalManagerProtocol.reject_request
	RejectRequest(ctx context.Context, requestID string) (experience.ExperienceApplyResult, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// EvolutionEventKind 演化事件类型。
// 对齐 Python: EvolutionEventKind = Literal["approval", "progress", "outcome"]
type EvolutionEventKind = string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EvolutionEventKindApproval 审批事件
	// 对齐 Python: "approval"
	EvolutionEventKindApproval EvolutionEventKind = "approval"
	// EvolutionEventKindProgress 进度事件
	// 对齐 Python: "progress"
	EvolutionEventKindProgress EvolutionEventKind = "progress"
	// EvolutionEventKindOutcome 结果事件
	// 对齐 Python: "outcome"
	EvolutionEventKindOutcome EvolutionEventKind = "outcome"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// PendingApprovalSnapshotStore 暂存审批快照映射。
// 对齐 Python: PendingApprovalSnapshotStore = MutableMapping[str, PendingChange]
type PendingApprovalSnapshotStore = map[string]*experience.PendingChange

// ──────────────────────────── 导出函数 ────────────────────────────

// HasChanges 是否有变更。
// 对齐 Python: EvolutionRequestResult.has_changes (property)
func (r EvolutionRequestResult) HasChanges() bool {
	// 对齐 Python: return bool(self.records or self.approval_event)
	return len(r.Records) > 0 || r.ApprovalEvent != nil
}

// HasChanges 是否有变更。
// 对齐 Python: SimplifyRequestResult.has_changes (property)
func (r SimplifyRequestResult) HasChanges() bool {
	// 对齐 Python: return bool(self.actions or self.approval_event)
	return len(r.Actions) > 0 || r.ApprovalEvent != nil
}

// ToPayload 返回 JSON payload 形态，跳过空字段。
// 对齐 Python: EvolutionHostEventMeta.to_payload() -> dict[str, str]
func (m EvolutionHostEventMeta) ToPayload() map[string]string {
	// 对齐 Python L34: payload: dict[str, str] = {"event_kind": self.event_kind}
	payload := map[string]string{"event_kind": m.EventKind}
	// 对齐 Python L35-47: 遍历 7 个可选字段，跳过 None
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
// 对齐 Python: EvolutionSnapshot.to_legacy_dict() -> dict[str, Any]
func (s EvolutionSnapshot) ToLegacyDict() map[string]any {
	// 对齐 Python L60-62: snapshot: dict[str, Any] = {"trajectory": ..., "messages": ...}
	snapshot := map[string]any{
		"trajectory": s.Trajectory,
		"messages":   s.Messages,
	}
	// 对齐 Python L64-65: if self.skill_name is not None: snapshot["skill_name"] = ...
	if s.SkillName != nil {
		snapshot["skill_name"] = *s.SkillName
	}
	return snapshot
}

// FromLegacyDict 从 dict 恢复 EvolutionSnapshot。
// 对齐 Python: EvolutionSnapshot.from_legacy_dict(cls, snapshot: dict[str, Any])
func FromLegacyDict(snapshot map[string]any) EvolutionSnapshot {
	// 对齐 Python L69: trajectory=snapshot["trajectory"]
	var traj *trajectory.Trajectory
	if t, ok := snapshot["trajectory"]; ok {
		if typed, ok := t.(*trajectory.Trajectory); ok {
			traj = typed
		}
	}

	// 对齐 Python L70: messages=list(snapshot.get("messages", []))
	messages := []map[string]any{}
	if m, ok := snapshot["messages"]; ok {
		if typed, ok := m.([]map[string]any); ok {
			messages = typed
		}
	}

	// 对齐 Python L71: skill_name=snapshot.get("skill_name")
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
