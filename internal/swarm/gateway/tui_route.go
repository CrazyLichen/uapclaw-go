package gateway

import (
	"context"

	"github.com/gorilla/websocket"

	messagehandler "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TuiRouteParams TUI 路由装配参数。
//
// 对齐 Python CliRouteBindParams（tui_connect.py L343-350）裁剪：
// 仅保留 DisconnectHandler 所需的 message_handler；agent_client/
// on_config_saved/cron_controller 属纯本地 handler（config.*/cron.*），
// ⤵️ 11.15 完整范围回填时再扩展。
type TuiRouteParams struct {
	// MessageHandler 断连取消会话的处理器（可 nil，nil 时 DisconnectHandler 跳过）
	MessageHandler *messagehandler.MessageHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// TuiForwardReqMethods /tui 路由转发到 Agent 的 method 集合。
//
// Python: CLI_FORWARD_REQ_METHODS（tui_connect.py L163-248），80 项。
var TuiForwardReqMethods = map[string]bool{
	"agents.create":                  true,
	"agents.delete":                  true,
	"agents.disable":                 true,
	"agents.enable":                  true,
	"agents.get":                     true,
	"agents.list":                    true,
	"agents.tools_list":              true,
	"agents.update":                  true,
	"browser.start":                  true,
	"chat.interrupt":                 true,
	"chat.resume":                    true,
	"chat.send":                      true,
	"chat.user_answer":               true,
	"command.add_dir":                true,
	"command.chrome":                 true,
	"command.compact":                true,
	"command.context":                true,
	"command.diff":                   true,
	"command.mcp":                    true,
	"command.recap":                  true,
	"command.resume":                 true,
	"command.sandbox":                true,
	"command.session":                true,
	"command.status":                 true,
	"extensions.delete":              true,
	"extensions.import":              true,
	"extensions.list":                true,
	"extensions.toggle":              true,
	"history.get":                    true,
	"permissions.rules.create":       true,
	"permissions.rules.get":          true,
	"permissions.rules.update":       true,
	"permissions.tools.get":          true,
	"permissions.tools.update":       true,
	"plugins.disable":                true,
	"plugins.enable":                 true,
	"plugins.install":                true,
	"plugins.list":                   true,
	"plugins.reload":                 true,
	"plugins.uninstall":              true,
	"schedule.cancel":                true,
	"schedule.check_config":          true,
	"schedule.create":                true,
	"schedule.delete":                true,
	"schedule.list":                  true,
	"schedule.logs":                  true,
	"schedule.run":                   true,
	"schedule.status":                true,
	"schedule.update_config":         true,
	"session.fork":                   true,
	"skills.clawhub.download":        true,
	"skills.clawhub.get_token":       true,
	"skills.clawhub.search":          true,
	"skills.clawhub.set_token":       true,
	"skills.evolution.get":           true,
	"skills.evolution.save":          true,
	"skills.evolution.status":        true,
	"skills.get":                     true,
	"skills.import_local":            true,
	"skills.install":                 true,
	"skills.installed":               true,
	"skills.list":                    true,
	"skills.marketplace.add":         true,
	"skills.marketplace.list":        true,
	"skills.marketplace.remove":      true,
	"skills.marketplace.toggle":      true,
	"skills.skillnet.evaluate":       true,
	"skills.skillnet.install":        true,
	"skills.skillnet.install_status": true,
	"skills.skillnet.search":         true,
	"skills.teamskillshub.delete":    true,
	"skills.teamskillshub.info":      true,
	"skills.teamskillshub.init":      true,
	"skills.teamskillshub.install":   true,
	"skills.teamskillshub.pack":      true,
	"skills.teamskillshub.publish":   true,
	"skills.teamskillshub.search":    true,
	"skills.teamskillshub.validate":  true,
	"skills.toggle":                  true,
	"skills.uninstall":               true,
}

// TuiForwardNoLocalHandlerMethods /tui 路由仅转发、无本地 handler 的集合。
//
// Python: CLI_FORWARD_NO_LOCAL_HANDLER_METHODS（L250-330），75 项。
// 差集（forward − no_local）恰为 5 个 chat 类方法。
var TuiForwardNoLocalHandlerMethods = map[string]bool{
	"agents.create":                  true,
	"agents.delete":                  true,
	"agents.disable":                 true,
	"agents.enable":                  true,
	"agents.get":                     true,
	"agents.list":                    true,
	"agents.tools_list":              true,
	"agents.update":                  true,
	"browser.start":                  true,
	"command.add_dir":                true,
	"command.chrome":                 true,
	"command.compact":                true,
	"command.context":                true,
	"command.diff":                   true,
	"command.mcp":                    true,
	"command.recap":                  true,
	"command.resume":                 true,
	"command.sandbox":                true,
	"command.session":                true,
	"command.status":                 true,
	"extensions.delete":              true,
	"extensions.import":              true,
	"extensions.list":                true,
	"extensions.toggle":              true,
	"permissions.rules.create":       true,
	"permissions.rules.get":          true,
	"permissions.rules.update":       true,
	"permissions.tools.get":          true,
	"permissions.tools.update":       true,
	"plugins.disable":                true,
	"plugins.enable":                 true,
	"plugins.install":                true,
	"plugins.list":                   true,
	"plugins.reload":                 true,
	"plugins.uninstall":              true,
	"schedule.cancel":                true,
	"schedule.check_config":          true,
	"schedule.create":                true,
	"schedule.delete":                true,
	"schedule.list":                  true,
	"schedule.logs":                  true,
	"schedule.run":                   true,
	"schedule.status":                true,
	"schedule.update_config":         true,
	"session.fork":                   true,
	"skills.clawhub.download":        true,
	"skills.clawhub.get_token":       true,
	"skills.clawhub.search":          true,
	"skills.clawhub.set_token":       true,
	"skills.evolution.get":           true,
	"skills.evolution.save":          true,
	"skills.evolution.status":        true,
	"skills.get":                     true,
	"skills.import_local":            true,
	"skills.install":                 true,
	"skills.installed":               true,
	"skills.list":                    true,
	"skills.marketplace.add":         true,
	"skills.marketplace.list":        true,
	"skills.marketplace.remove":      true,
	"skills.marketplace.toggle":      true,
	"skills.skillnet.evaluate":       true,
	"skills.skillnet.install":        true,
	"skills.skillnet.install_status": true,
	"skills.skillnet.search":         true,
	"skills.teamskillshub.delete":    true,
	"skills.teamskillshub.info":      true,
	"skills.teamskillshub.init":      true,
	"skills.teamskillshub.install":   true,
	"skills.teamskillshub.pack":      true,
	"skills.teamskillshub.publish":   true,
	"skills.teamskillshub.search":    true,
	"skills.teamskillshub.validate":  true,
	"skills.toggle":                  true,
	"skills.uninstall":               true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTuiRouteConfig 构建 /tui CLI 路由配置。
//
// 对齐 Python build_cli_route_binding（tui_connect.py L2093-2119）：
// forward 集合 80 项、no-local 集合 75 项（差集=5 个 chat 类方法，
// 计数已机器核对 2026-10-28，见 spec 附录 A）。
//
// 与 Python 目录结构的有意偏差：Python tui_connect.py 属 channel_manager
// 子包，但 Go 端 gateway → channel_manager 依赖方向已存在，tui 装配
// 放 gateway 包。
func BuildTuiRouteConfig(params TuiRouteParams) *RouteConfig {
	return &RouteConfig{
		Path:                         "/tui",
		ChannelID:                    "tui",
		ForwardMethods:               TuiForwardReqMethods,
		ForwardNoLocalHandlerMethods: TuiForwardNoLocalHandlerMethods,
		LocalHandlers: map[string]LocalHandlerFunc{
			"chat.send":        tuiChatSendHandler,
			"chat.resume":      tuiChatResumeHandler,
			"chat.interrupt":   tuiChatInterruptHandler,
			"chat.user_answer": tuiChatUserAnswerHandler,
			"history.get":      tuiHistoryGetHandler,
		},
		DisconnectHandler: tuiDisconnectHandler(params.MessageHandler),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tuiChatSendHandler chat.send 本地 ack。
//
// Python: _chat_send（tui_connect.py L1402-1405）——仅返回 ack，
// 消息转发由 ForwardMethods 集合路径完成。
func tuiChatSendHandler(ws *websocket.Conn, reqID string, _ map[string]any, sessionID string) error {
	writeResponseFrame(ws, reqID, true, map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}, "", "", true)
	return nil
}

// tuiChatResumeHandler chat.resume 本地 ack。
//
// Python: _chat_resume（L1407-1410）。
func tuiChatResumeHandler(ws *websocket.Conn, reqID string, _ map[string]any, sessionID string) error {
	writeResponseFrame(ws, reqID, true, map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}, "", "", true)
	return nil
}

// tuiChatInterruptHandler chat.interrupt 本地 ack（intent 可选透传）。
//
// Python: _chat_interrupt（L1412-1417）。
func tuiChatInterruptHandler(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
	payload := map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}
	if intent, ok := params["intent"].(string); ok && intent != "" {
		payload["intent"] = intent
	}
	writeResponseFrame(ws, reqID, true, payload, "", "", true)
	return nil
}

// tuiChatUserAnswerHandler chat.user_answer 本地 ack（request_id 可选透传）。
//
// Python: _chat_user_answer（L1419-1424）。
func tuiChatUserAnswerHandler(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
	payload := map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}
	if rid, ok := params["request_id"].(string); ok && rid != "" {
		payload["request_id"] = rid
	}
	writeResponseFrame(ws, reqID, true, payload, "", "", true)
	return nil
}

// tuiHistoryGetHandler history.get 本地 ack（session_id 覆写 + page_idx 透传）。
//
// Python: _history_get（L1426-1433）。
func tuiHistoryGetHandler(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
	payload := map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}
	if sid, ok := params["session_id"].(string); ok && sid != "" {
		payload["session_id"] = sid
	}
	if v, ok := params["page_idx"]; ok {
		payload["page_idx"] = v
	}
	writeResponseFrame(ws, reqID, true, payload, "", "", true)
	return nil
}

// tuiDisconnectHandler 构造断连回调：桥接 CancelAgentSessionsOnDisconnect。
//
// Python: _tui_disconnect（tui_connect.py L2106-2110）——
// mh 为 None 或无 stale keys 时直接返回。
func tuiDisconnectHandler(mh *messagehandler.MessageHandler) DisconnectFunc {
	return func(_ *websocket.Conn, staleSessionKeys [][2]string) {
		if mh == nil || len(staleSessionKeys) == 0 {
			return
		}
		mh.CancelAgentSessionsOnDisconnect(context.Background(), staleSessionKeys)
	}
}
