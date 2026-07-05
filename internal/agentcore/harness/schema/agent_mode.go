package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// AgentMode Agent 执行模式枚举
type AgentMode int

const (
	// AgentModeNormal 普通执行模式（默认）
	AgentModeNormal AgentMode = iota
	// AgentModePlan 只读规划模式
	AgentModePlan
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseAgentMode 从字符串解析 AgentMode
func ParseAgentMode(s string) (AgentMode, error) {
	switch strings.ToLower(s) {
	case "normal":
		return AgentModeNormal, nil
	case "plan":
		return AgentModePlan, nil
	default:
		return AgentModeNormal, fmt.Errorf("未知的 AgentMode: %q", s)
	}
}

// String 返回 AgentMode 的字符串表示
func (m AgentMode) String() string {
	switch m {
	case AgentModeNormal:
		return "normal"
	case AgentModePlan:
		return "plan"
	default:
		return fmt.Sprintf("unknown(%d)", m)
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (m AgentMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (m *AgentMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("AgentMode 应为字符串，解析失败: %w", err)
	}
	parsed, err := ParseAgentMode(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
