package providers

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// helpers_evolution.go —— evolution_rails provider 的 map 形参取值助手。
// Python: TeamSkillEvolutionInput.resolve / MemberSkillEvolutionInput.resolve
//（evolution_rails.py:515-544/748-771）的 param_field 读取语义。

// mapAny 取 map 键（非 map 返回 nil）。
func mapAny(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// mapAnyOrEmpty nil 兜底空 map。
func mapAnyOrEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// mapString 取 string 键。
func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// mapBool 取 bool 键。
func mapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// mapStrField 任意 map 的字符串字段（runtimeinheritance.mapStrField 同语义镜像，
// providers 包不能反向 import team）。
func mapStrField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// logComponent 日志组件（swarm provider 系）。
const logComponent = logger.ComponentAgentServer
