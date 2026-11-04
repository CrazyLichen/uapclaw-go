package runtime

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedMode 请求模式解析结果。
type ResolvedMode struct {
	// CanonicalMode 权威模式串
	CanonicalMode string
	// IsPlan 是否规划模式
	IsPlan bool
	// IsTeam 是否团队模式
	IsTeam bool
	// NormalMode 退出 plan 后应回到的普通模式
	NormalMode string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PlanEntrySourceSlashCommand TUI /plan 命令来源
	PlanEntrySourceSlashCommand = "slash_command"
	// PlanEntrySourcePlanToggle Web 计划开关来源
	PlanEntrySourcePlanToggle = "plan_toggle"
	// PlanEntrySourceKey 请求参数中的进入来源键
	PlanEntrySourceKey = "plan_entry_source"
	// PlanReminderOriginalQueryKey 激活提醒改写前原 query 的存放键
	PlanReminderOriginalQueryKey = "plan_reminder_original_query"
	// PlanModeExitedEvent 计划模式结束事件类型
	PlanModeExitedEvent = "plan_mode_exited"
	// PreviousSessionModeKey 上一次会话模式参数键
	PreviousSessionModeKey = "_session_previous_mode"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// chatTurnMethods 参与 plan 状态同步的请求方法（空串视为默认 chat）。
var chatTurnMethods = map[string]struct{}{
	"":            {},
	"chat.send":   {},
	"chat":        {},
	"chat.resume": {},
	"chat.answer": {},
}

// planEntrySources 显式进入计划模式的来源集合。
var planEntrySources = map[string]struct{}{
	PlanEntrySourceSlashCommand: {},
	PlanEntrySourcePlanToggle:   {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveRequestRuntimeMode 把请求 mode/subMode 解析为权威模式。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode。
// 三段 canonical 直接按串语义解析（见 schema.ResolveNewCanonicalMode）；
// 其余走归一并按后缀判定 plan/team。
func ResolveRequestRuntimeMode(mode, subMode string) ResolvedMode {
	raw := strings.TrimSpace(mode)
	if raw == "" {
		if sub := strings.TrimSpace(subMode); sub != "" {
			raw = "agent.work." + sub
		}
	}
	canonical := string(schema.NormalizeMode(raw))
	return ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        schema.IsPlanMode(canonical),
		IsTeam:        schema.IsTeamMode(canonical),
		NormalMode:    baseModeWithoutPlan(canonical),
	}
}

// baseModeWithoutPlan 返回退出 plan 后应回到的普通模式。
//
// Python: mode_matrix.base_mode_without_plan / _PLAN_EXIT_MODES
// 新三段命名下 `x.y.plan` → `x.y.normal`；非 plan 串原样返回。
func baseModeWithoutPlan(canonical string) string {
	if strings.HasSuffix(canonical, ".plan") {
		return strings.TrimSuffix(canonical, ".plan") + ".normal"
	}
	return canonical
}

// ShouldSync 判断请求是否参与 plan 状态同步。
//
// Python: PlanModeController.should_sync（只认 chat 类方法）。
func ShouldSync(reqMethod string) bool {
	_, ok := chatTurnMethods[strings.TrimSpace(reqMethod)]
	return ok
}

// IsPlanEntrySource 判断进入来源是否属于显式进入。
//
// Python: PlanModeController.is_explicit_entry
func IsPlanEntrySource(source string) bool {
	_, ok := planEntrySources[strings.TrimSpace(source)]
	return ok
}
