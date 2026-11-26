package providers

import (
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
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

// evolutionSwarmContextFace SwarmBuildContext 的演进注册消费面适配
// （team.EvolutionSwarmContextFace 结构化满足；team↔buildctx 成环，
// providers 以方法集包装承载）。
type evolutionSwarmContextFace struct {
	view *buildctx.SwarmBuildContext
}

// EvolutionSessionID 会话 id。
func (f evolutionSwarmContextFace) EvolutionSessionID() string { return f.view.SessionID }

// EvolutionChannel 渠道键。
func (f evolutionSwarmContextFace) EvolutionChannel() string { return f.view.Channel }

// EvolutionTeamID 团队名。
func (f evolutionSwarmContextFace) EvolutionTeamID() string { return f.view.TeamID }

// EvolutionRole 团队角色。
func (f evolutionSwarmContextFace) EvolutionRole() string { return f.view.Role }

// EvolutionConfig 已解析配置。
func (f evolutionSwarmContextFace) EvolutionConfig() map[string]any { return f.view.Config }

// EvolutionLanguage 语言码。
func (f evolutionSwarmContextFace) EvolutionLanguage() string { return f.view.Language }

// EvolutionTeamWsRoot 团队共享工作区根。
func (f evolutionSwarmContextFace) EvolutionTeamWsRoot() string { return f.view.TeamWsRoot }

// EvolutionMemberName 成员名。
func (f evolutionSwarmContextFace) EvolutionMemberName() string { return f.view.MemberName }

// EvolutionProjectDir 项目目录。
func (f evolutionSwarmContextFace) EvolutionProjectDir() string { return f.view.ProjectDir }
