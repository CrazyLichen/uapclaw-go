package runtime

import (
	"encoding/json"
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

// ResolveAgentRequestMode 解析 mode 为 manager mode、sub-mode 与 canonical mode。
//
// Python: jiuwenswarm/runtime/request.py resolve_agent_request_mode（L266-330）。
// 与上游一致：裸 "agent"/"plan"/"fast" 不下沉 plan；只有显式 plan 串才带 plan 状态。
func ResolveAgentRequestMode(rawMode any, workMode string) (mode, subMode, canonical string) {
	text := ""
	if s, ok := rawMode.(string); ok {
		text = strings.ToLower(strings.TrimSpace(s))
	}
	// 新三段 canonical 短路：environment/state 已内嵌，不能再按第一个点拆
	if m, s, c, ok := schema.ResolveNewCanonicalMode(text); ok {
		return m, s, c
	}
	if text == "auto_harness" {
		return "auto_harness", "auto_harness", "auto_harness"
	}
	normalizedWork := strings.ToLower(strings.TrimSpace(workMode))
	if text == "plan" || text == "fast" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	// team.plan 属历史串，canonicalize 后落到 team.work.plan
	if canonicalMode := schema.CanonicalizeMode(text); canonicalMode != text {
		if m, s, c, ok := schema.ResolveNewCanonicalMode(canonicalMode); ok {
			return m, s, c
		}
	}
	parts := strings.SplitN(text, ".", 2)
	mode = parts[0]
	if mode == "" {
		mode = "agent"
	}
	if mode == "agent" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	// team 族的 subMode 不参与实例查找（上游显式置 None）
	if mode == "team" {
		return "team", "", "team"
	}
	if len(parts) > 1 {
		subMode = parts[1]
	}
	if mode == "code" && subMode != "plan" && subMode != "normal" && subMode != "team" {
		subMode = "normal"
	}
	canonical = mode
	if subMode != "" {
		canonical = mode + "." + subMode
	}
	return mode, subMode, canonical
}

// ApplyResolvedModeToRequest 解析并把 canonical mode 写回 request.Params。
//
// Python: jiuwenswarm/runtime/request.py apply_resolved_mode_to_request（L345-360）。
func ApplyResolvedModeToRequest(req *schema.AgentRequest, workMode string) (mode, subMode string) {
	params := parseRequestParams(req)
	mode, subMode, canonical := ResolveAgentRequestMode(params["mode"], workMode)
	params["mode"] = canonical
	writeRequestParams(req, params)
	return mode, subMode
}

// ResolveRequestRuntimeModeFromRequest 从请求解析权威运行时模式。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode（L331-344）。
func ResolveRequestRuntimeModeFromRequest(req *schema.AgentRequest) ResolvedMode {
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	_, _, canonical := ResolveAgentRequestMode(params["mode"], workMode)
	return ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        schema.IsPlanMode(canonical),
		IsTeam:        schema.IsTeamMode(canonical),
		NormalMode:    baseModeWithoutPlan(canonical),
	}
}

// parseRequestParams 解析 request.Params 为可写 map（nil/非法时返回空 map）。
func parseRequestParams(req *schema.AgentRequest) map[string]any {
	if req == nil || len(req.Params) == 0 {
		return map[string]any{}
	}
	params := map[string]any{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return map[string]any{}
	}
	return params
}

// writeRequestParams 把 params 序列化回 request.Params。
func writeRequestParams(req *schema.AgentRequest, params map[string]any) {
	if req == nil {
		return
	}
	updated, err := json.Marshal(params)
	if err != nil {
		return
	}
	req.Params = json.RawMessage(updated)
}

// ResolveAgentRequestModeFromRequest 纯读取请求的 mode（不修改 request）。
//
// Python: _handle_cancel / AgentManager 用 resolve_agent_request_mode 的只读形态。
func ResolveAgentRequestModeFromRequest(req *schema.AgentRequest) (mode, subMode string) {
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	mode, subMode, _ = ResolveAgentRequestMode(params["mode"], workMode)
	return mode, subMode
}
