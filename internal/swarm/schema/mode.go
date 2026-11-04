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

	// ─── code 族 ───

	// ModeCodePlan 代码规划模式
	ModeCodePlan Mode = "code.plan"
	// ModeCodeNormal 代码常态模式
	ModeCodeNormal Mode = "code.normal"
	// ModeCodeTeam 代码团队模式
	ModeCodeTeam Mode = "code.team"

	// ─── team 族 ───

	// ModeTeam 团队运行时模式
	ModeTeam Mode = "team"
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

// modeLookup 字符串值到 Mode 枚举的查找表，用于 ParseMode/IsValidMode 的 O(1) 查找。
var modeLookup map[string]Mode

// ──────────────────────────── 导出函数 ────────────────────────────

// IsCodeProfileMode 判断是否 code profile 族（旧 code.* 与新 agent.code.* / team.code.*）。
//
// Python: gateway/common/mode_matrix.py is_code_profile_mode
func IsCodeProfileMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "code", "code.normal", "code.plan", "code.team",
		"team.plan.code",
		"agent.code.normal", "agent.code.plan",
		"team.code.normal", "team.code.plan":
		return true
	default:
		return false
	}
}

// IsAgentMode 判断是否 agent 工作族（含历史 agent.plan/agent.fast/plan/fast 与新 agent.work.*）。
//
// Python: gateway/common/mode_matrix.py is_agent_mode
func IsAgentMode(mode string) bool {
	t := strings.ToLower(strings.TrimSpace(mode))
	switch t {
	case "agent", "plan", "fast", "agent.plan", "agent.fast":
		return true
	}
	return strings.HasPrefix(t, "agent.work.")
}

// ResolveNewCanonicalMode 把新三段 canonical 模式解析为 (managerMode, subMode, canonical)。
//
// Python: gateway/common/mode_matrix.py resolve_new_canonical_mode。
// environment/state 已内嵌在串里，不能按第一个点拆：
//   - team.code.*  → ("code", "team")
//   - agent/team.code.* → ("code", <state>)
//   - *.work.plan  → (<role>, "plan")；*.work.normal → (<role>, "")
func ResolveNewCanonicalMode(raw string) (managerMode, subMode, canonical string, ok bool) {
	text := strings.ToLower(strings.TrimSpace(raw))
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

// NormalizeMode 把入站 mode 串归一到权威值，未知串回落 agent.work.normal。
//
// 上游以三段串为唯一权威值（2826a9d23 模式合并），两段串（agent.plan/agent.fast）
// 全量切换后不再识别。
func NormalizeMode(raw string) Mode {
	switch m := Mode(strings.TrimSpace(raw)); m {
	case ModeAgentWorkPlan, ModeAgentWorkNormal, ModeCodePlan, ModeCodeNormal, ModeCodeTeam, ModeTeam:
		return m
	default:
		return ModeAgentWorkNormal
	}
}

// AllModes 返回所有 Mode 枚举值。
// 用于遍历清理等场景。
func AllModes() []Mode {
	return []Mode{
		// agent 族
		ModeAgentWorkPlan,
		ModeAgentWorkNormal,
		// code 族
		ModeCodePlan,
		ModeCodeNormal,
		ModeCodeTeam,
		// team 族
		ModeTeam,
	}
}

// ParseMode 从字符串解析 Mode，非法值回退到 default。
// Python: Mode.from_raw(raw_mode, default) 语义：
// 先对输入做 strip + lower 标准化，再查找合法值；
// 空字符串或未识别值均返回 default。
func ParseMode(s string, defaultVal Mode) Mode {
	normalized := strings.TrimSpace(strings.ToLower(s))
	if normalized == "" {
		return defaultVal
	}
	if m, ok := modeLookup[normalized]; ok {
		return m
	}
	return defaultVal
}

// IsValidMode 判断字符串是否为合法的 Mode 值。
func IsValidMode(s string) bool {
	_, ok := modeLookup[s]
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
// Python: Mode.to_runtime_mode()，返回枚举的字符串值本身。
func (m Mode) ToRuntimeMode() string {
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
