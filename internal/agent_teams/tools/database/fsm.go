package database

import "github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"

// ──────────────────────────── 导出函数 ────────────────────────────

// IsValidMemberTransition 检查 MemberStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current, new, MEMBER_TRANSITIONS)
// 委托 fsm 包实现，本包仅提供 string 版 wrapper（避免 database→schema 循环依赖）。
func IsValidMemberTransition(current, target string) bool {
	return fsm.IsValidMemberTransition(current, target)
}

// IsValidExecutionTransition 检查 ExecutionStatus 状态转换是否合法。
// 对齐 Python: is_valid_transition(current, target, EXECUTION_TRANSITIONS)
// 委托 fsm 包实现。
func IsValidExecutionTransition(current, target string) bool {
	return fsm.IsValidExecutionTransition(current, target)
}
