package schema

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// Mode Agent 运行模式枚举。
//
// 定义 AgentServer 通信链路中的运行模式，用于适配器选择和消息路由。
// 值为点分字符串格式（如 "agent.work.plan"），与 Python Mode 枚举值一一对应。
// 6 个模式分为三族：
//   - agent 族：agent.work.plan（深度规划）、agent.work.normal（快速响应）
//   - code 族：code.plan（代码规划）、code.normal（代码常态）、code.team（代码团队）
//   - team 族：team（团队运行时）
//
// Python: jiuwenswarm/common/schema/message.py (Mode)
type Mode string

const (
	// ─── agent 族 ───

	// ModeAgentWorkPlan agent-work 规划模式
	ModeAgentWorkPlan Mode = "agent.work.plan"
	// ModeAgentWorkNormal agent-work 常态模式
	ModeAgentWorkNormal Mode = "agent.work.normal"
	// ModeAgentCodePlan agent-code 规划模式
	ModeAgentCodePlan Mode = "agent.code.plan"
	// ModeAgentCodeNormal agent-code 常态模式
	ModeAgentCodeNormal Mode = "agent.code.normal"

	// ─── team 族 ───

	// ModeTeamWorkNormal 团队 work 常态模式
	ModeTeamWorkNormal Mode = "team.work.normal"
	// ModeTeamWorkPlan 团队 work 规划模式
	ModeTeamWorkPlan Mode = "team.work.plan"
	// ModeTeamCodeNormal 团队 code 常态模式
	ModeTeamCodeNormal Mode = "team.code.normal"
	// ModeTeamCodePlan 团队 code 规划模式
	ModeTeamCodePlan Mode = "team.code.plan"
	// ModeTeamDesign 设计师团队模式
	ModeTeamDesign Mode = "team.design"
	// ModeTeam 裸 team（等价 team.work.normal 的旧形式）
	ModeTeam Mode = "team"

	// ─── code 族（保留历史常量名，值已升级为新 canonical） ───

	// ModeCodePlan code 规划模式（权威值 agent.code.plan）
	ModeCodePlan Mode = "agent.code.plan"
	// ModeCodeNormal code 常态模式（权威值 agent.code.normal）
	ModeCodeNormal Mode = "agent.code.normal"
	// ModeCodeTeam code 团队模式（权威值 team.code.normal）
	ModeCodeTeam Mode = "team.code.normal"

	// ─── legacy 成员（对齐 Python Mode 的历史成员，值即旧 canonical） ───
	//
	// Python: openjiuwen_protocol.message.Mode 保留 AGENT/AGENT_PLAN/AGENT_FAST 等
	// 历史成员以兼容旧持久化反解析；`from_raw` 会经 DEPRECATION_MAP 折叠到新
	// canonical，故这些值只能由"直接构造"产生（如 ACP 入站 `Mode.AGENT`）。
	// 本仓 ModeFromRaw 同样先 CanonicalizeMode，因此 legacy 成员不会被解析产出。

	// ModeAgent 代理模式（legacy，值 agent）
	ModeAgent Mode = "agent"
	// ModeAgentPlan 代理规划模式（legacy，值 agent.plan）
	ModeAgentPlan Mode = "agent.plan"
	// ModeAgentFast 代理快速模式（legacy，值 agent.fast）
	ModeAgentFast Mode = "agent.fast"
	// ModeCodePlanLegacy code 规划（legacy，值 code.plan）
	ModeCodePlanLegacy Mode = "code.plan"
	// ModeCodeNormalLegacy code 常态（legacy，值 code.normal）
	ModeCodeNormalLegacy Mode = "code.normal"
	// ModeCodeTeamLegacy code 团队（legacy，值 code.team）
	ModeCodeTeamLegacy Mode = "code.team"
	// ModeTeamPlanNormal 团队规划（legacy，值 team.plan.normal）
	ModeTeamPlanNormal Mode = "team.plan.normal"
	// ModeTeamPlanCode 团队代码规划（legacy，值 team.plan.code）
	ModeTeamPlanCode Mode = "team.plan.code"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// newCanonicalModeSegments 新三段 canonical 模式 → (role, environment, state)。
//
// Python: gateway/common/mode_matrix.py NEW_CANONICAL_MODE_RESOLUTION
var newCanonicalModeSegments = map[string][3]string{
	"agent.work.normal": {"agent", "work", "normal"},
	"agent.work.plan":   {"agent", "work", "plan"},
	"agent.code.normal": {"agent", "code", "normal"},
	"agent.code.plan":   {"agent", "code", "plan"},
	"team.work.normal":  {"team", "work", "normal"},
	"team.work.plan":    {"team", "work", "plan"},
	"team.code.normal":  {"team", "code", "normal"},
	"team.code.plan":    {"team", "code", "plan"},
	"team.design":       {"team", "design", "normal"},
}

// runtimeModeLegacyCollapse legacy 成员 → 运行时串的折叠表。
//
// 契约来自上游单测 tests/unit_tests/evolution/test_message.py:136-158：
// `agent.plan`/`agent.fast` 的运行时串都是 "agent"（0.2.5 起 plan/fast 与 agent 合并），
// 其余 legacy 成员原样返回，新三段 canonical 原样返回。
var runtimeModeLegacyCollapse = map[Mode]string{
	ModeAgentPlan:        "agent",
	ModeAgentFast:        "agent",
	ModeTeamPlanNormal:   "team.plan.normal",
	ModeTeamPlanCode:     "team.plan.code",
	ModeCodePlanLegacy:   "code.plan",
	ModeCodeNormalLegacy: "code.normal",
	ModeCodeTeamLegacy:   "code.team",
}

// modeLookup 字符串值到 Mode 枚举的查找表，用于 ModeFromRaw/IsValidMode 的 O(1) 查找。
var modeLookup map[string]Mode

// ──────────────────────────── 导出函数 ────────────────────────────

// teamCanonicalModes 所有表示"集群"的 canonical 模式。
//
// Python: gateway/common/mode_matrix.py TEAM_CANONICAL_MODES
var teamCanonicalModes = map[string]struct{}{
	"team": {}, "team.plan.normal": {}, "code.team": {}, "team.plan.code": {},
	"team.work.normal": {}, "team.work.plan": {},
	"team.code.normal": {}, "team.code.plan": {}, "team.design": {},
}

// planCanonicalModes 所有表示"正处于 plan"的 canonical 模式。
//
// Python: gateway/common/mode_matrix.py PLAN_CANONICAL_MODES
var planCanonicalModes = map[string]struct{}{
	"agent.plan": {}, "code.plan": {},
	"team.plan.normal": {}, "team.plan.code": {},
	"agent.work.plan": {}, "agent.code.plan": {},
	"team.work.plan": {}, "team.code.plan": {},
}

// singleAgentCanonicalModes 单 agent 流程允许的 canonical 模式。
//
// Python: gateway/common/mode_matrix.py SINGLE_AGENT_CANONICAL_MODES（code 族经
// canonicalize 后落到 agent.code.*，本项目暂保留 code.* 形式，见 IsSingleAgentMode）。
var singleAgentCanonicalModes = map[string]struct{}{
	"agent.work.normal": {}, "agent.work.plan": {},
	"agent.code.normal": {}, "agent.code.plan": {},
}

// IsPlanMode 判断 canonical 模式是否处于 plan。
//
// Python: mode_matrix.is_plan_mode
func IsPlanMode(mode string) bool {
	_, ok := planCanonicalModes[CanonicalizeMode(mode)]
	return ok
}

// IsTeamMode 判断 canonical 模式是否为集群。
//
// Python: mode_matrix.is_team_mode
func IsTeamMode(mode string) bool {
	_, ok := teamCanonicalModes[CanonicalizeMode(mode)]
	return ok
}

// IsSingleAgentMode 判断模式是否走单 agent 流程。
//
// Python: mode_matrix.is_single_agent_mode。偏差：上游先 canonicalize（code.normal →
// agent.code.normal）再查表；本项目 code 族暂保留 code.* 形式（见 spec §5.1 偏差说明），
// 因此额外接受 code profile 族的模式。
func IsSingleAgentMode(mode string) bool {
	_, ok := singleAgentCanonicalModes[CanonicalizeMode(mode)]
	return ok
}

// IsCodeProfileMode 判断是否 code profile 族（旧 code.* 与新 agent.code.* / team.code.*）。
//
// Python: gateway/common/mode_matrix.py is_code_profile_mode
func IsCodeProfileMode(mode string) bool {
	switch CanonicalizeMode(mode) {
	case "agent.code.normal", "agent.code.plan", "team.code.normal", "team.code.plan":
		return true
	default:
		return false
	}
}

// IsAgentMode 判断是否 agent 工作族（含历史 agent.plan/agent.fast/plan/fast 与新 agent.work.*）。
//
// Python: gateway/common/mode_matrix.py is_agent_mode
func IsAgentMode(mode string) bool {
	// Python: memory/config.py is_agent_mode（字面集合，含历史 bare plan/fast）
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "agent", "agent.plan", "agent.fast", "plan", "fast",
		"agent.work.normal", "agent.work.plan":
		return true
	default:
		return false
	}
}

// ResolveNewCanonicalModeText 同 ResolveNewCanonicalMode，但只按原文查新三段表。
//
// Python: resolve_new_canonical_mode 用 canonicalize_mode_text（只解析别名，不套
// DEPRECATION_MAP）；legacy 串（agent/code/team.plan.normal…）必须交给调用方自己的
// 分支处理，不能被提前折叠成新 canonical。
func ResolveNewCanonicalModeText(text string) (managerMode, subMode, canonical string, ok bool) {
	segments, found := newCanonicalModeSegments[text]
	if !found {
		return "", "", "", false
	}
	role, environment, state := segments[0], segments[1], segments[2]
	switch {
	case role == "team" && environment == "code":
		return "code", "team", text, true
	case environment == "code":
		return "code", state, text, true
	case state == "plan":
		return role, "plan", text, true
	default:
		return role, "", text, true
	}
}

// CanonicalizeModeText 归一 mode 文本：小写去空白 → 空值回落 agent → 只解析 MODE_ALIASES。
//
// Python: gateway/common/mode_matrix.py canonicalize_mode_text（不套 DEPRECATION_MAP，
// 保留 agent / code / team.plan.normal 等 legacy 串原文）。
func CanonicalizeModeText(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" {
		return "agent"
	}
	if mapped, ok := modeAliases[t]; ok {
		return mapped
	}
	return t
}

// IsWebComposableMode 判断 mode 是否参与 Web 的 mode + work_mode 组合。
//
// Python: gateway/common/mode_matrix.py WEB_COMPOSABLE_MODES
func IsWebComposableMode(modeText string) bool {
	_, ok := webComposableModes[modeText]
	return ok
}

// ComposeWebMode 把 Web 的 mode + work_mode 组合成后端三元组。
//
// Python: gateway/common/mode_matrix.py compose_web_mode（_WEB_MODE_TABLE）
func ComposeWebMode(modeText, workMode string) (managerMode, subMode, canonical string, ok bool) {
	entry, found := webComposeTable[[2]string{modeText, workMode}]
	if !found {
		return "", "", "", false
	}
	return entry[0], entry[1], entry[2], true
}

// ResolveNewCanonicalMode 把新三段 canonical 模式解析为 (managerMode, subMode, canonical)。
//
// Python: gateway/common/mode_matrix.py resolve_new_canonical_mode。
// environment/state 已内嵌在串里，不能按第一个点拆：
//   - team.code.*  → ("code", "team")
//   - agent/team.code.* → ("code", <state>)
//   - *.work.plan  → (<role>, "plan")；*.work.normal → (<role>, "")
func ResolveNewCanonicalMode(raw string) (managerMode, subMode, canonical string, ok bool) {
	text := CanonicalizeMode(raw)
	segments, found := newCanonicalModeSegments[text]
	if !found {
		return "", "", "", false
	}
	role, environment, state := segments[0], segments[1], segments[2]
	switch {
	case role == "team" && environment == "code":
		return "code", "team", text, true
	case environment == "code":
		return "code", state, text, true
	case state == "plan":
		return role, "plan", text, true
	default:
		return role, "", text, true
	}
}

// modeAliases 历史简写 → 完整 canonical。
//
// Python: gateway/common/mode_matrix.py MODE_ALIASES
var modeAliases = map[string]string{
	"team.plan":   "team.plan.normal",
	"team.code":   "code.team",
	"team.work":   "team.work.normal",
	"team.normal": "team.work.normal",
	"agent.work":  "agent.work.normal",
	"agent.code":  "agent.code.normal",
}

// deprecatedModes 旧 canonical → 新 canonical 的静默映射。
//
// Python: gateway/common/mode_matrix.py DEPRECATION_MAP
var deprecatedModes = map[string]string{
	"agent":            "agent.work.normal",
	"agent.plan":       "agent.work.plan",
	"agent.fast":       "agent.work.normal",
	"code":             "agent.code.normal",
	"code.normal":      "agent.code.normal",
	"code.plan":        "agent.code.plan",
	"code.team":        "team.code.normal",
	"team":             "team.work.normal",
	"team.plan.normal": "team.work.plan",
	"team.plan.code":   "team.code.plan",
}

// webComposableModes Web 的 mode + work_mode 组合支持的 mode 取值。
//
// Python: gateway/common/mode_matrix.py WEB_COMPOSABLE_MODES
var webComposableModes = map[string]struct{}{
	"agent":             {},
	"agent.plan":        {},
	"team":              {},
	"agent.work.normal": {},
	"agent.work.plan":   {},
}

// webComposeTable (mode, work_mode) → (managerMode, subMode, canonicalMode)。
//
// Python: gateway/common/mode_matrix.py _WEB_MODE_TABLE。
// code profile 的单 agent 组合只在 legacy mode 上提供；新三段 agent.work.* 自带
// work profile，不再参与 code 组合。
var webComposeTable = map[[2]string][3]string{
	{"agent", "work"}:             {"agent", "", "agent"},
	{"agent.plan", "work"}:        {"agent", "plan", "agent.plan"},
	{"agent", "code"}:             {"code", "normal", "code.normal"},
	{"agent.plan", "code"}:        {"code", "plan", "code.plan"},
	{"team", "work"}:              {"team", "", "team"},
	{"team", "code"}:              {"code", "team", "code.team"},
	{"agent.work.normal", "work"}: {"agent", "", "agent.work.normal"},
	{"agent.work.plan", "work"}:   {"agent", "plan", "agent.work.plan"},
}

// CanonicalizeMode 把任意入站模式串 canonicalize 到新三段命名。//
// Python: gateway/common/mode_matrix.py canonicalize_mode_text（别名 → 弃用映射 →
// 未命中原样返回）。与 NormalizeMode 的区别：本函数不做"未知串回落默认"，
// 只做确定的迁移映射。
func CanonicalizeMode(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	if mapped, ok := modeAliases[t]; ok {
		t = mapped
	}
	if mapped, ok := deprecatedModes[t]; ok {
		return mapped
	}
	return t
}

// NormalizeMode 把入站 mode 串归一到权威值，未知串回落 agent.work.normal。
//
// 上游以三段串为唯一权威值（2826a9d23 模式合并），两段串（agent.plan/agent.fast）
// 全量切换后不再识别。合法集合 = 新三段 canonical + code 族 + 裸 team。
func NormalizeMode(raw string) Mode {
	t := CanonicalizeMode(raw)
	if t == "" {
		return ModeAgentWorkNormal
	}
	if _, ok := planCanonicalModes[t]; ok {
		return Mode(t)
	}
	if _, ok := teamCanonicalModes[t]; ok {
		return Mode(t)
	}
	if _, ok := singleAgentCanonicalModes[t]; ok {
		return Mode(t)
	}
	return ModeAgentWorkNormal
}

// AllModes 返回所有 Mode 枚举值。
// 用于遍历清理等场景。
func AllModes() []Mode {
	return []Mode{
		// agent 族（work / code 两个 environment）
		ModeAgentWorkNormal,
		ModeAgentWorkPlan,
		ModeAgentCodeNormal,
		ModeAgentCodePlan,
		// team 族
		ModeTeamWorkNormal,
		ModeTeamWorkPlan,
		ModeTeamCodeNormal,
		ModeTeamCodePlan,
		ModeTeamDesign,
		// 裸 team（历史形式，仍作为合法值保留）
		ModeTeam,
		// legacy 成员（对齐 Python Mode 历史成员；解析路径不可达，仅供直接构造）
		ModeAgent,
		ModeAgentPlan,
		ModeAgentFast,
		ModeCodePlanLegacy,
		ModeCodeNormalLegacy,
		ModeCodeTeamLegacy,
		ModeTeamPlanNormal,
		ModeTeamPlanCode,
	}
}

// ModeFromRaw 从原始入参解析 Mode，非法/非字符串/空值回退到 default。
//
// Python: openjiuwen_protocol.message.Mode.from_raw(raw_mode, default=None)。
// 与 Python 逐条对应：
//  1. 已是 Mode → 原样返回（Go 的 Mode 是 string 类型，同值即同枚举）
//  2. 非字符串 → default
//  3. strip + lower 后为空 → default
//  4. 查表（经 DEPRECATION_MAP 归一），未命中 → default
func ModeFromRaw(rawMode any, defaultVal Mode) Mode {
	if m, ok := rawMode.(Mode); ok {
		return m
	}
	s, ok := rawMode.(string)
	if !ok {
		return defaultVal
	}
	normalized := CanonicalizeMode(s)
	if normalized == "" {
		return defaultVal
	}
	if m, ok := modeLookup[normalized]; ok {
		return m
	}
	return defaultVal
}

// IsNewCanonicalMode 判断模式串是否属于新三段命名 canonical 集合。
//
// Python: gateway/common/mode_matrix.py NEW_CANONICAL_MODES（9 个，含 team.design）。
// 只按原文查表，不做别名/弃用映射。
func IsNewCanonicalMode(mode string) bool {
	_, ok := newCanonicalModeSegments[strings.ToLower(strings.TrimSpace(mode))]
	return ok
}

// IsValidMode 判断字符串是否为合法的 Mode 值。
func IsValidMode(s string) bool {
	_, ok := modeLookup[CanonicalizeMode(s)]
	return ok
}

// String 实现 fmt.Stringer 接口。
func (m Mode) String() string {
	return string(m)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (m Mode) GoString() string {
	return fmt.Sprintf("schema.Mode(%q)", string(m))
}

// ToRuntimeMode 返回模式的运行时字符串值。
//
// Python: Mode.to_runtime_mode()——legacy 成员先折叠（`agent.*` → "agent"），
// 新三段 canonical 原样返回；下游 `acp_connect.py:1596` 用它注入 params["mode"]。
func (m Mode) ToRuntimeMode() string {
	if s, ok := runtimeModeLegacyCollapse[m]; ok {
		return s
	}
	return string(m)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建查找表
	modes := AllModes()
	modeLookup = make(map[string]Mode, len(modes))
	for _, m := range modes {
		modeLookup[string(m)] = m
	}
}
