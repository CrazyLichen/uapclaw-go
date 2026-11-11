package schema

import (
	"fmt"
)

// 本文件是 Mode 枚举本体（对应上游 openjiuwen_protocol.message.Mode）。
//
// 表驱动的归一与判定（modeAliases / deprecatedModes / Is*Mode 等）在
// mode_matrix.go 中，对应上游 gateway/common/mode_matrix.py。

// ──────────────────────────── 枚举 ────────────────────────────

// Mode Agent 运行模式枚举。
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

// ──────────────────────────── 全局变量 ────────────────────────────

// runtimeModeLegacyCollapse legacy 成员 → 运行时串的折叠表。
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

// NormalizeMode 把入站 mode 串归一到权威值，未知串回落 agent.work.normal。
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
