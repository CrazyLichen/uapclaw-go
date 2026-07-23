package schema

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// Mode Agent 运行模式枚举。
//
// 定义 AgentServer 通信链路中的运行模式，用于适配器选择和消息路由。
// 值为点分字符串格式（如 "agent.plan"），与 Python Mode 枚举值一一对应。
// 6 个模式分为三族：
//   - agent 族：agent.plan（深度规划）、agent.fast（快速响应）
//   - code 族：code.plan（代码规划）、code.normal（代码常态）、code.team（代码团队）
//   - team 族：team（团队运行时）
//
// 对应 Python: jiuwenswarm/common/schema/message.py (Mode)
type Mode string

const (
	// ─── agent 族 ───

	// ModeAgentPlan 深度规划模式
	ModeAgentPlan Mode = "agent.plan"
	// ModeAgentFast 快速响应模式
	ModeAgentFast Mode = "agent.fast"

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

// modeLookup 字符串值到 Mode 枚举的查找表，用于 ParseMode/IsValidMode 的 O(1) 查找。
var modeLookup map[string]Mode

// ──────────────────────────── 导出函数 ────────────────────────────

// AllModes 返回所有 Mode 枚举值。
// 用于遍历清理等场景。
func AllModes() []Mode {
	return []Mode{
		// agent 族
		ModeAgentPlan,
		ModeAgentFast,
		// code 族
		ModeCodePlan,
		ModeCodeNormal,
		ModeCodeTeam,
		// team 族
		ModeTeam,
	}
}

// ParseMode 从字符串解析 Mode，非法值回退到 default。
// 对齐 Python Mode.from_raw(raw_mode, default) 语义：
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
// 对齐 Python Mode.to_runtime_mode()，返回枚举的字符串值本身。
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
