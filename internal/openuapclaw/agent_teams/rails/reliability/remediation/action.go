// Package remediation 补救动作分类（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/remediation/action.py
package remediation

// ──────────────────────────── 枚举 ────────────────────────────

// RemediationAction 异常的处置动作，由策略按严重度映射。
//
// Python: action.py RemediationAction(str, Enum)，4 取值全量映射。
// LOCAL_STEER 是唯一自动化动作且严格可逆（对成员自身 LLM 的 steering nudge，
// 受重启强度预算限流）。破坏性动作（停成员/取消任务/spawn）刻意缺位：
// 那是 leader 通过既有团队工具的决策。
type RemediationAction string

// 动作取值。
const (
	// RemediationActionObserveOnly 仅记录日志
	RemediationActionObserveOnly RemediationAction = "observe_only"
	// RemediationActionReportLeader 上报 leader（LLM 决策）
	RemediationActionReportLeader RemediationAction = "report_leader"
	// RemediationActionLocalSteer 本地可逆自纠偏（steering nudge）
	RemediationActionLocalSteer RemediationAction = "local_steer"
	// RemediationActionEscalateUser 升级给用户
	RemediationActionEscalateUser RemediationAction = "escalate_user"
)
