package logger

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 枚举 ────────────────────────────

// Component 日志组件枚举，决定日志路由到哪个文件。
// 对应 Python: _log_component_from_logger_name 的返回值
type Component int

const (
	// ComponentCommon 基础设施层日志 → common.log（config/workspace/dotenv/version 等公共包）
	ComponentCommon Component = iota
	// ComponentGateway Gateway 日志 → gateway.log
	ComponentGateway
	// ComponentChannel swarm/channel/* 日志 → channel.log
	ComponentChannel
	// ComponentAgentServer swarm/server/* 日志 → agent_server.log
	ComponentAgentServer
	// ComponentPermissions 安全/权限相关日志 → permissions.log + agent_server.log
	ComponentPermissions
	// ComponentAgentCore agentcore/* 日志 → agent_core.log
	ComponentAgentCore
)

// componentStrings Component 枚举到字符串的映射。
var componentStrings = [...]string{"common", "gateway", "channel", "agent_server", "permissions", "agent_core"}

// String 返回组件的字符串表示。
func (c Component) String() string {
	if c < 0 || int(c) >= len(componentStrings) {
		return "unknown"
	}
	return componentStrings[c]
}

// GoString 实现 fmt.GoStringer 接口。
func (c Component) GoString() string {
	return fmt.Sprintf("Component%s", c.String())
}

// MarshalJSON 实现 json.Marshaler 接口。
func (c Component) MarshalJSON() ([]byte, error) {
	return []byte(`"` + c.String() + `"`), nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
func (c *Component) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = s[1 : len(s)-1] // 去掉引号
	for i, name := range componentStrings {
		if name == s {
			*c = Component(i)
			return nil
		}
	}
	*c = ComponentCommon
	return nil
}

// LogFileName 返回组件对应的日志文件名。
func (c Component) LogFileName() string {
	switch c {
	case ComponentCommon:
		return "common.log"
	case ComponentGateway:
		return "gateway.log"
	case ComponentChannel:
		return "channel.log"
	case ComponentAgentServer:
		return "agent_server.log"
	case ComponentPermissions:
		return "permissions.log"
	case ComponentAgentCore:
		return "agent_core.log"
	default:
		return "gateway.log"
	}
}

// allComponents 返回所有组件枚举值，用于遍历。
func allComponents() []Component {
	return []Component{ComponentCommon, ComponentGateway, ComponentChannel, ComponentAgentServer, ComponentPermissions, ComponentAgentCore}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// componentFromString 从字符串解析 Component，未匹配时返回 ComponentCommon。
func componentFromString(name string) Component {
	for i, s := range componentStrings {
		if s == name {
			return Component(i)
		}
	}
	return ComponentCommon
}

// ensure Component implements required interfaces at compile time.
var (
	_ fmt.Stringer     = ComponentGateway
	_ fmt.GoStringer   = ComponentGateway
	_ json.Marshaler   = ComponentGateway
	_ json.Unmarshaler = (*Component)(nil)
)
