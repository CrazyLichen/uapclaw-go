package schema

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ApproveAction 审批通过动作。
	// 对应 Python: APPROVE_ACTION = "approve"
	ApproveAction = "approve"

	// AppendMode 追加模式。
	// 对应 Python: APPEND_MODE = "append"
	AppendMode = "append"

	// ConversationReviewSignal 对话审查信号。
	// 对应 Python: CONVERSATION_REVIEW_SIGNAL = "conversation_review"
	ConversationReviewSignal = "conversation_review"

	// ExecutionFailureSignal 执行失败信号。
	// 对应 Python: EXECUTION_FAILURE_SIGNAL = "execution_failure"
	ExecutionFailureSignal = "execution_failure"

	// ExperiencesTarget 经验目标名。
	// 对应 Python: EXPERIENCES_TARGET = "experiences"
	ExperiencesTarget = "experiences"

	// ExperienceEntry 经验条目类型。
	// 对应 Python: EXPERIENCE_ENTRY = "experience_entry"
	ExperienceEntry = "experience_entry"

	// LocalApplyCompleted 本地应用完成阶段。
	// 对应 Python: LOCAL_APPLY_COMPLETED = "local_apply_completed"
	LocalApplyCompleted = "local_apply_completed"

	// MergeMode 合并模式。
	// 对应 Python: MERGE_MODE = "merge"
	MergeMode = "merge"

	// PendingChangeEffect 暂存变更效果。
	// 对应 Python: PENDING_CHANGE_EFFECT = "pending_change"
	PendingChangeEffect = "pending_change"

	// RejectAction 审批拒绝动作。
	// 对应 Python: REJECT_ACTION = "reject"
	RejectAction = "reject"

	// ReplaceMode 替换模式。
	// 对应 Python: REPLACE_MODE = "replace"
	ReplaceMode = "replace"

	// RetryAction 重试动作。
	// 对应 Python: RETRY_ACTION = "retry"
	RetryAction = "retry"

	// SkillExperienceEntry 技能经验条目类型。
	// 对应 Python: SKILL_EXPERIENCE_ENTRY = "skill_experience_entry"
	SkillExperienceEntry = "skill_experience_entry"

	// StateEffect 状态效果。
	// 对应 Python: STATE_EFFECT = "state"
	StateEffect = "state"

	// ToolFailureSignal 工具失败信号。
	// 对应 Python: TOOL_FAILURE_SIGNAL = "tool_failure"
	ToolFailureSignal = "tool_failure"

	// TrajectoryIssueSignal 轨迹问题信号。
	// 对应 Python: TRAJECTORY_ISSUE_SIGNAL = "trajectory_issue"
	TrajectoryIssueSignal = "trajectory_issue"

	// UserIntentSignal 用户意图信号。
	// 对应 Python: USER_INTENT_SIGNAL = "user_intent"
	UserIntentSignal = "user_intent"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ValidPatchActions 有效的补丁动作集合。
	// 对应 Python: VALID_PATCH_ACTIONS = frozenset({"append", "merge", "replace", "skip"})
	ValidPatchActions = map[string]bool{
		AppendMode:  true,
		MergeMode:   true,
		ReplaceMode: true,
		"skip":      true,
	}

	// ValidSections 有效的 SKILL.md 区域集合。
	// 对应 Python: VALID_SECTIONS = frozenset({"Instructions", "Examples", ...})
	ValidSections = map[string]bool{
		"Instructions":    true,
		"Examples":        true,
		"Troubleshooting": true,
		"Scripts":         true,
		"Collaboration":   true,
		"Roles":           true,
		"Constraints":     true,
		"Workflow":        true,
	}
)
