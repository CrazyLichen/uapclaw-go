package agent_teams

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/inboundtags"
)

// 本文件把 inboundtags 子包的纯渲染函数转发到父包，保持 25.0b 的既有调用点不变。
//
// 为什么下沉：tracker 子包（agent_teams/tracker）需要 RenderClock / RenderEvent /
// RenderTeamContext / RenderTeamContextWithIdentity / DropSupersededSnapshots，
// 而 Go 的子包不能 import 父包（agent_teams → rails → tracker 会成环），
// 故渲染层下沉到零依赖的叶子包 inboundtags，父包与 tracker 都向它单向依赖。
//
// 实现只有一份（在 inboundtags），此处是纯转调，不是兼容壳（规则 8）。
// 若后续清理调用点，可把这些转发逐个删掉，改为直接写 inboundtags.Xxx。
//
// 对应 Python 代码：openjiuwen/agent_teams/inbound_render.py

// ──────────────────────────── 结构体 ────────────────────────────

// InboundOptions 见 inboundtags.InboundOptions。
// Python: inbound_render.InboundOptions
type InboundOptions = inboundtags.InboundOptions

// EventOptions 见 inboundtags.EventOptions。
// Python: inbound_render.EventOptions
type EventOptions = inboundtags.EventOptions

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InboundTypeDirect 见 inboundtags.InboundTypeDirect。
	InboundTypeDirect = inboundtags.InboundTypeDirect
	// InboundTypeBroadcast 见 inboundtags.InboundTypeBroadcast。
	InboundTypeBroadcast = inboundtags.InboundTypeBroadcast
	// ControllerSender 见 inboundtags.ControllerSender。
	ControllerSender = inboundtags.ControllerSender
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderInbound 见 inboundtags.RenderInbound。
func RenderInbound(content string, opts InboundOptions) string {
	return inboundtags.RenderInbound(content, opts)
}

// RenderControllerInput 见 inboundtags.RenderControllerInput。
func RenderControllerInput(content string) string {
	return inboundtags.RenderControllerInput(content)
}

// RenderClock 见 inboundtags.RenderClock。
func RenderClock(now string) string { return inboundtags.RenderClock(now) }

// RenderEvent 见 inboundtags.RenderEvent。
func RenderEvent(kind, body string, opts EventOptions) string {
	return inboundtags.RenderEvent(kind, body, opts)
}

// RenderTeamContext 见 inboundtags.RenderTeamContext。
func RenderTeamContext(body string) string { return inboundtags.RenderTeamContext(body) }

// RenderTeamContextWithIdentity 见 inboundtags.RenderTeamContextWithIdentity。
func RenderTeamContextWithIdentity(identityBody string, identityConversion, infoBody *string) string {
	return inboundtags.RenderTeamContextWithIdentity(identityBody, identityConversion, infoBody)
}

// RenderTeamPolicy 见 inboundtags.RenderTeamPolicy。
func RenderTeamPolicy(body, tools, note string) string {
	return inboundtags.RenderTeamPolicy(body, tools, note)
}

// RenderTeamPolicyInline 见 inboundtags.RenderTeamPolicyInline。
func RenderTeamPolicyInline(body string) string { return inboundtags.RenderTeamPolicyInline(body) }

// DropSupersededSnapshots 见 inboundtags.DropSupersededSnapshots。
func DropSupersededSnapshots(parts []string) []string {
	return inboundtags.DropSupersededSnapshots(parts)
}
