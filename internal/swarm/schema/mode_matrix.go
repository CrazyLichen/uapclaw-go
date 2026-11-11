package schema

import (
	"fmt"
	"strings"
)

// 本文件是 gateway/common/mode_matrix.py 的 Go 对应物。
//
// Python: gateway/common/mode_matrix.py
//
// 职责：运行模式的表驱动归一与判定。全部函数都是无状态纯函数，
// 供 AgentServer / Gateway / Runtime / Process CLI 共用。
//
// 与 mode.go 的分工：
//   - mode.go    —— Mode 枚举（对应上游 common/schema/message.py 的 Mode）
//   - 本文件     —— mode_matrix.py 的表与函数
//
// 两个易混函数（上游也是两个，语义不同）：
//   - CanonicalizeModeText：只查 MODE_ALIASES，空值回落 "agent"
//   - DeprecateMode：空值原样返回 → CanonicalizeModeText → 再查 DEPRECATION_MAP

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedMode 一次请求解析出的完整运行模式视图。
// Python: gateway/common/mode_matrix.py:196-221 ResolvedMode（10 字段）
type ResolvedMode struct {
	// ManagerMode AgentManager / adapter 选型用的一级模式
	ManagerMode string
	// SubMode 子模式（normal / plan / team，可空）
	SubMode string
	// CanonicalMode 写回 params["mode"] 的规范值
	CanonicalMode string
	// WorkMode 归一化后的 work / code；历史请求为空
	WorkMode string
	// IsPlan 本次请求是否要求处于 plan
	IsPlan bool
	// IsTeam 本次请求是否集群模式
	IsTeam bool
	// IsCodeProfile 是否使用 CodeAdapter / code 团队 profile
	IsCodeProfile bool
	// Profile 本次请求使用的 profile（normal / code）
	Profile string
	// NormalMode 退出 plan 后应回到的 canonical 模式
	NormalMode string
	// FromWebComposition 是否由 Web 的 mode + work_mode 组合而来
	FromWebComposition bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// newCanonicalModeSegments 新三段 canonical 模式 → (role, environment, state)。
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

// teamCanonicalModes 所有表示"集群"的 canonical 模式。
// Python: gateway/common/mode_matrix.py TEAM_CANONICAL_MODES
var teamCanonicalModes = map[string]struct{}{
	"team": {}, "team.plan.normal": {}, "code.team": {}, "team.plan.code": {},
	"team.work.normal": {}, "team.work.plan": {},
	"team.code.normal": {}, "team.code.plan": {}, "team.design": {},
}

// planCanonicalModes 所有表示"正处于 plan"的 canonical 模式。
// Python: gateway/common/mode_matrix.py PLAN_CANONICAL_MODES
var planCanonicalModes = map[string]struct{}{
	"agent.plan": {}, "code.plan": {},
	"team.plan.normal": {}, "team.plan.code": {},
	"agent.work.plan": {}, "agent.code.plan": {},
	"team.work.plan": {}, "team.code.plan": {},
}

// singleAgentCanonicalModes 单 agent 流程允许的 canonical 模式。
// Python: gateway/common/mode_matrix.py SINGLE_AGENT_CANONICAL_MODES
//
// 上游只有 4 个新三段成员，不含任何 code.* 旧串。旧串必须先经
// DeprecateMode 映射后才在集合内（见 is_single_agent_mode 的调用方）。
var singleAgentCanonicalModes = map[string]struct{}{
	"agent.work.normal": {}, "agent.work.plan": {},
	"agent.code.normal": {}, "agent.code.plan": {},
}

// modeAliases 历史简写 → 完整 canonical。
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
// Python: gateway/common/mode_matrix.py WEB_COMPOSABLE_MODES
var webComposableModes = map[string]struct{}{
	"agent":             {},
	"agent.plan":        {},
	"team":              {},
	"agent.work.normal": {},
	"agent.work.plan":   {},
}

// webComposeTable (mode, work_mode) → (managerMode, subMode, canonicalMode)。
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

// ──────────────────────────── 导出函数 ────────────────────────────

// CanonicalizeMode 把任意入站模式串 canonicalize 到新三段命名。
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

// CanonicalizeModeText 归一 mode 文本：小写去空白 → 空值回落 agent → 只解析 MODE_ALIASES。
// Python: gateway/common/mode_matrix.py:214-223 canonicalize_mode_text（不套
// DEPRECATION_MAP，保留 agent / code / team.plan.normal 等 legacy 串原文）。
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

// DeprecateMode 旧 canonical 静默映射到新 canonical；非旧串原样返回。
// Python: gateway/common/mode_matrix.py:341-358 deprecate_mode
//
// 铁律 3：空/空白串原样返回，不做 canonicalize_mode_text 的空串回落，
// 避免把空值误映射成 agent.work.normal。归一化由调用方在上游完成。
func DeprecateMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return mode
	}
	text := CanonicalizeModeText(mode)
	if mapped, ok := deprecatedModes[text]; ok {
		return mapped
	}
	return text
}

// NormalizeWorkMode 把任意来源的 work_mode 归一成 work / code；非法时返回空串。
// Python: gateway/common/mode_matrix.py:237-242 normalize_work_mode
func NormalizeWorkMode(raw any) string {
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "code":
		return "code"
	case "work":
		return "work"
	default:
		return ""
	}
}

// ReadRequestWorkMode 读取请求中显式携带的 work_mode；缺失或非法时返回空串。
// Python: gateway/common/mode_matrix.py:245-253 read_request_work_mode
//
// 只有 Web 会携带该字段，因此它同时充当「这是 Web 组合模式请求」的判据。
func ReadRequestWorkMode(params map[string]any) string {
	if params == nil {
		return ""
	}
	return NormalizeWorkMode(params["work_mode"])
}

// BaseModeWithoutPlan canonical plan 模式退出后应回到的普通模式。
// Python: gateway/common/mode_matrix.py:330-333 base_mode_without_plan
//
// 新三段命名下 x.y.plan → x.y.normal；非 plan 串原样返回。
func BaseModeWithoutPlan(canonical string) string {
	text := CanonicalizeModeText(canonical)
	if strings.HasSuffix(text, ".plan") {
		return strings.TrimSuffix(text, ".plan") + ".normal"
	}
	return text
}

// ResolveRequestMode 解析一次请求的运行模式。
// Python: gateway/common/mode_matrix.py:413-489 resolve_request_mode
//
// 优先走 Web 组合分支；只有「存在合法 work_mode」且「mode 是 Web 支持的基础模式」
// 同时成立才生效。其余取值（code.plan / code.team / team.plan.* / agent.fast 等
// 完整模式串）都交给 legacyResolver。
//
// legacyResolver 对应上游 legacy_resolver 参数（resolve_agent_request_mode），
// 返回 (managerMode, subMode, canonicalMode)。
func ResolveRequestMode(
	params map[string]any,
	legacyResolver func(mode, workMode string) (managerMode, subMode, canonicalMode string),
	workMode string,
) ResolvedMode {
	var rawMode any
	if params != nil {
		rawMode = params["mode"]
	}
	modeText := CanonicalizeModeText(modeTextValue(rawMode))

	work := NormalizeWorkMode(workMode)
	if work == "" {
		work = ReadRequestWorkMode(params)
	}

	// Python: :438-461 web 组合分支
	if work != "" && IsWebComposableMode(modeText) {
		if m, s, c, ok := ComposeWebMode(modeText, work); ok {
			return ResolvedMode{
				ManagerMode:        m,
				SubMode:            s,
				CanonicalMode:      c,
				WorkMode:           work,
				IsPlan:             IsPlanMode(c),
				IsTeam:             IsTeamMode(c),
				IsCodeProfile:      work == "code",
				Profile:            profileOf(work),
				NormalMode:         BaseModeWithoutPlan(c),
				FromWebComposition: true,
			}
		}
	}

	// Python: :463-489 legacy 分支
	m, s, c := legacyResolver(modeText, work)
	return ResolvedMode{
		ManagerMode:        m,
		SubMode:            s,
		CanonicalMode:      c,
		WorkMode:           work,
		IsPlan:             IsPlanMode(c),
		IsTeam:             IsTeamMode(c),
		IsCodeProfile:      m == "code",
		Profile:            profileOf(m),
		NormalMode:         BaseModeWithoutPlan(c),
		FromWebComposition: false,
	}
}

// IsPlanMode 判断 canonical 模式是否处于 plan。
// Python: gateway/common/mode_matrix.py:261-263 is_plan_mode
//
// 内部用 canonicalize_mode_text（只查别名，不查弃用映射）。
func IsPlanMode(mode string) bool {
	_, ok := planCanonicalModes[CanonicalizeModeText(mode)]
	return ok
}

// IsTeamMode 判断 canonical 模式是否为集群。
// Python: gateway/common/mode_matrix.py:266-268 is_team_mode
func IsTeamMode(mode string) bool {
	_, ok := teamCanonicalModes[CanonicalizeModeText(mode)]
	return ok
}

// IsSingleAgentMode 判断模式是否走单 agent 流程。
// Python: gateway/common/mode_matrix.py:271-273 is_single_agent_mode
//
// 上游集合只有 4 个新三段成员，不含 code.* 旧串。调用方若持有旧串，
// 必须先 DeprecateMode 再调用（对齐上游 service.py:3822 的写法）。
func IsSingleAgentMode(mode string) bool {
	_, ok := singleAgentCanonicalModes[CanonicalizeModeText(mode)]
	return ok
}

// IsCodeProfileMode 判断是否 code profile 族（旧 code.* 与新 agent.code.* / team.code.*）。
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
// Python: resolve_new_canonical_mode 用 canonicalize_mode_text（只解析别名，不套
// DEPRECATION_MAP）；legacy 串（agent/code/team.plan.normal…）必须交给调用方自己的
// 分支处理，不能被提前折叠成新 canonical。
func ResolveNewCanonicalModeText(text string) (managerMode, subMode, canonical string, ok bool) {
	segments, found := newCanonicalModeSegments[text]
	if !found {
		return "", "", "", false
	}
	return resolveSegments(segments, text)
}

// ResolveNewCanonicalMode 把新三段 canonical 模式解析为 (managerMode, subMode, canonical)。
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
	return resolveSegments(segments, text)
}

// IsWebComposableMode 判断 mode 是否参与 Web 的 mode + work_mode 组合。
// Python: gateway/common/mode_matrix.py WEB_COMPOSABLE_MODES
func IsWebComposableMode(modeText string) bool {
	_, ok := webComposableModes[modeText]
	return ok
}

// ComposeWebMode 把 Web 的 mode + work_mode 组合成后端三元组。
// Python: gateway/common/mode_matrix.py compose_web_mode（_WEB_MODE_TABLE）
func ComposeWebMode(modeText, workMode string) (managerMode, subMode, canonical string, ok bool) {
	entry, found := webComposeTable[[2]string{modeText, workMode}]
	if !found {
		return "", "", "", false
	}
	return entry[0], entry[1], entry[2], true
}

// SupportsSingleAgentPermissionProfile 该 mode/sub_mode/work_mode 组合是否支持
// 单 Agent 权限档位（只允许已知的独立 normal/plan 档）。
//
// Python: gateway/common/mode_matrix.py supports_single_agent_permission_profile
// 参数用 any 以兼容 Mode 枚举与裸字符串（Python 用 getattr(mode, "value", mode)）。
func SupportsSingleAgentPermissionProfile(mode, subMode, workMode any) bool {
	rawMode := modeTextValue(mode)
	if strings.TrimSpace(rawMode) == "" {
		return false
	}
	text := CanonicalizeModeText(rawMode)
	sub := strings.ToLower(strings.TrimSpace(modeTextValue(subMode)))
	work := strings.ToLower(strings.TrimSpace(modeTextValue(workMode)))
	if work != "" && work != "work" && work != "code" {
		return false
	}
	switch text {
	case "agent", "code":
		allowed := []string{"", "normal", "plan"}
		if text == "agent" {
			allowed = []string{"", "normal", "plan", "fast"}
		}
		if !containsModeText(allowed, sub) {
			return false
		}
		if sub == "plan" || sub == "fast" {
			text = text + "." + sub
		}
	default:
		if sub != "" && sub != "normal" && sub != "plan" {
			return false
		}
	}
	if (text == "agent" || text == "agent.plan") && work != "" {
		if _, _, canonical, ok := ComposeWebMode(text, work); ok {
			text = canonical
		}
	}
	canonical := CanonicalizeMode(text)
	if _, allowed := singleAgentCanonicalModes[canonical]; !allowed {
		return false
	}
	if work == "" {
		return true
	}
	return strings.Contains(canonical, ".code.") == (work == "code")
}

// IsNewCanonicalMode 判断模式串是否属于新三段命名 canonical 集合。
// Python: gateway/common/mode_matrix.py NEW_CANONICAL_MODES（9 个，含 team.design）。
// 只按原文查表，不做别名/弃用映射。
func IsNewCanonicalMode(mode string) bool {
	_, ok := newCanonicalModeSegments[strings.ToLower(strings.TrimSpace(mode))]
	return ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSegments 把 (role, environment, state) 三元组解析为模式三元组。
// 与上游 resolve_new_canonical_mode 的分支顺序一致。
func resolveSegments(segments [3]string, text string) (managerMode, subMode, canonical string, ok bool) {
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

// profileOf web 分支按 work_mode 判、legacy 分支按 manager_mode 判。
// Python: mode_matrix.py:457（work_mode == "code"）与 :484（manager_mode == "code"）
func profileOf(flag string) string {
	if flag == "code" {
		return "code"
	}
	return "normal"
}

// modeTextValue 取 mode 的文本值（Mode 枚举取底层串，其余走 fmt）。
func modeTextValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case Mode:
		return string(v)
	default:
		return fmt.Sprint(value)
	}
}

// containsModeText 判断切片是否含指定文本。
func containsModeText(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
