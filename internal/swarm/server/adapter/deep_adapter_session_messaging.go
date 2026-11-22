// Package adapter —— session messaging 工具注册生命周期。
// Python: interface_deep.py:14230-14304 _ensure_session_messaging_tools_registered
// + :14305-14312 _update_session_tools 首块（messaging 先于 send_file/cron）。
package adapter

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	sessionmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// sessionMessagingToolNames必需注册的工具名集合。
// Python: interface_deep.py:14282-14289 required_names
var sessionMessagingRequiredNames = map[string]struct{}{
	"session_list":            {},
	"session_send_message":    {},
	"session_message_list":    {},
	"session_message_resolve": {},
	"session_continue_queued": {},
	"session_read":            {},
}

// sessionMessagingCronReservedPrefixes 预留会话前缀（不挂工具）。
// Python: interface_deep.py:14245-14247 ("heartbeat", "health_check", "cron")
var sessionMessagingCronReservedPrefixes = []string{"heartbeat", "health_check", "cron"}

// SetSessionMessageServiceProvider 注入 mailbox 服务提供方（门面 ensureAdapter 调用）。
// Python: get_current_runtime() 每次请求任务级读取；Go 无等价传递手段，
// 改 provider 闭包注入（AgentManager runtimeProvider 同款姿势）。
func (d *DeepAdapter) SetSessionMessageServiceProvider(provider func() any) {
	d.sessionMessageServiceProvider = provider
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// currentSessionMessageServicePort 读当前 mailbox 服务（经 provider → Runtime
// 引用 → server port 适配），nil=未装配或不支持。
// Python: :14252 runtime = get_current_runtime(); runtime.session_message_service
func (d *DeepAdapter) currentSessionMessageServicePort() sessionmtools.SessionMessagingServicePort {
	if d.sessionMessageServiceProvider == nil {
		return nil
	}
	svcAny := d.sessionMessageServiceProvider()
	if svcAny == nil {
		return nil
	}
	if port, ok := svcAny.(sessionmtools.SessionMessagingServicePort); ok {
		return port
	}
	if svc, ok := svcAny.(interface {
		SessionMessagingPort() sessionmtools.SessionMessagingServicePort
	}); ok {
		return svc.SessionMessagingPort()
	}
	return nil
}

// sessionMessagingEligible 判定当前请求是否允许挂载跨会话消息工具。
// Python: :14241-14253 eligible 门卫（五个条件同序）。
func (d *DeepAdapter) sessionMessagingEligible(config *runtimeConfig, port sessionmtools.SessionMessagingServicePort) bool {
	if config == nil || port == nil {
		return false
	}
	// Python: normalized_session_id and not startswith(("heartbeat","health_check","cron"))
	sessionID := strings.TrimSpace(config.SessionID)
	if sessionID == "" {
		return false
	}
	for _, prefix := range sessionMessagingCronReservedPrefixes {
		if strings.HasPrefix(sessionID, prefix) {
			return false
		}
	}
	// Python: str(channel_id or "").strip().lower() in {"web", "tui"}
	channel := strings.ToLower(strings.TrimSpace(config.ChannelID))
	if channel != "web" && channel != "tui" {
		return false
	}
	// Python: not is_team_mode(deprecate_mode(self._last_mode))
	if sschema.IsTeamMode(sschema.DeprecateMode(d.lastMode)) {
		return false
	}
	return true
}

// ensureSessionMessageToolsRegistered 按资格注册/摘除跨会话消息工具。
// Python: _ensure_session_messaging_tools_registered (interface_deep.py:14230-14304)
func (d *DeepAdapter) ensureSessionMessageToolsRegistered(ctx context.Context, config *runtimeConfig) {
	port := d.currentSessionMessageServicePort()
	// rail set_service（:14254-14256）
	if d.sessionMessagingRouteRail != nil {
		if d.sessionMessagingEligible(config, port) {
			d.sessionMessagingRouteRail.SetService(port)
		} else {
			d.sessionMessagingRouteRail.SetService(nil)
		}
	}
	if !d.sessionMessagingEligible(config, port) {
		// 非 eligible：移除已注册工具（:14257-14280）
		if d.sessionMessagingToolkit != nil {
			d.removeSessionMessagingTools()
		}
		return
	}
	// toolkit 懒建/SetService（:14286-14300）
	if d.sessionMessagingToolkit == nil {
		d.sessionMessagingToolkit = sessionmtools.NewSessionMessagingToolkit(port)
	} else {
		d.sessionMessagingToolkit.SetService(port)
	}
	registered := d.registeredToolNames()
	if requiredSubset(d.sessionMessagingToolkit, registered) {
		return
	}
	// 注册缺失工具（:14301-14304）
	if d.instance == nil {
		logger.Error(logComponent).
			Msg("[DeepAdapter] instance not initialized, session messaging tools registration skipped")
		return
	}
	resourceMgr := runner.GetResourceMgr()
	for _, t := range d.sessionMessagingToolkit.GetTools() {
		card := t.Card()
		if _, hit := registered[card.Name]; hit {
			continue
		}
		if existing, gerr := resourceMgr.GetTool([]string{card.ID}); gerr != nil || len(existing) == 0 {
			if aerr := resourceMgr.AddTool(t); aerr != nil {
				logger.Error(logComponent).
					Err(aerr).
					Str("tool_id", card.ID).
					Msg("[DeepAdapter] session messaging tool registration failed")
				continue
			}
		}
		d.instance.AbilityManager().Add(card)
		registered[card.Name] = struct{}{}
	}
}

// removeSessionMessagingTools 摘除 toolkit 提供且已注册的工具。
// Python: :14257-14280（非 eligible 分支）
func (d *DeepAdapter) removeSessionMessagingTools() {
	registered := d.registeredToolNames()
	removed := false
	for _, t := range d.sessionMessagingToolkit.GetTools() {
		if _, hit := registered[t.Card().Name]; !hit {
			continue
		}
		d.instance.AbilityManager().Remove(t.Card().Name)
		_, _ = runner.GetResourceMgr().RemoveTool([]string{t.Card().ID})
		removed = true
	}
	if removed {
		logger.Info(logComponent).Msg("[DeepAdapter] session messaging tools removed (ineligible)")
	}
}

// registeredToolNames 收集 ability_manager 当前注册的工具名。
// Python: :14243-14245 registered_names
func (d *DeepAdapter) registeredToolNames() map[string]struct{} {
	out := map[string]struct{}{}
	if d.instance == nil {
		return out
	}
	for _, existing := range d.instance.AbilityManager().List() {
		out[existing.AbilityName()] = struct{}{}
	}
	return out
}

// requiredSubset required_names ⊆ registered 判定（幂等跳过）。
// Python: :14301 if required_names <= registered_names: return
func requiredSubset(tk *sessionmtools.SessionMessagingToolkit, registered map[string]struct{}) bool {
	for _, t := range tk.GetTools() {
		if _, hit := registered[t.Card().Name]; !hit {
			return false
		}
	}
	return true
}
