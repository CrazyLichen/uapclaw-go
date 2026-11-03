package tui

import (
	"context"
	"fmt"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	messagehandler "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识（/tui 通道）
	logComponent = logger.ComponentGateway
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouteParams /tui 路由装配参数。
//
// 对齐 Python CliRouteBindParams（tui_connect.py L344-353）裁剪：仅保留
// DisconnectHandler 所需的 message_handler。res 帧发送能力不在此注入——Python 的
// channel（gateway_server）是 install 阶段才传进来的，Go 侧同样在 Install 里从
// server 取（见 RouteServer）。agent_client/on_config_saved/cron_controller 属
// 纯本地 handler（config.*/cron.*），完整范围随 11.15 扩展（当前 5 个
// chat/history handler 已对齐 Python CLI 差集）。
type RouteParams struct {
	// MessageHandler 断连取消会话的处理器（可 nil，nil 时 DisconnectHandler 跳过）
	MessageHandler *messagehandler.MessageHandler
}

// ResponseSender res 帧发送器，对齐 Python 本地 handler 拿到的 channel 的
// send_response(...) 能力。实现方为 gateway.GatewayServer。
type ResponseSender interface {
	// SendResFrame 写一条 res 帧（恒带 payload 键，对齐 Python send_response）
	SendResFrame(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string)
}

// RouteServer Install 阶段注入的通道对象，与 Python register_cli_handlers(channel=...)
// 的 channel 同构：既能注册本地 handler，也能替 handler 发 res 帧。
//
// 实现方为 gateway.GatewayServer；以接口而非具体类型表达，避免 gateway ↔ tui 成环。
// 不满足本接口时 Install 直接跳过（与 Python 传入无 send_response 的对象等价于不可用）。
type RouteServer interface {
	// ResponseSender 发送 res 帧的能力
	ResponseSender
	// RegisterLocalHandler 注册 path+method 对应的本地 handler
	RegisterLocalHandler(path, method string, handler routing.LocalHandlerFunc)
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ForwardReqMethods /tui 路由转发到 Agent 的 method 集合。
//
// Python: CLI_FORWARD_REQ_METHODS（tui_connect.py L101-197），80 项。
var ForwardReqMethods = map[string]bool{
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

// ForwardNoLocalHandlerMethods /tui 路由仅转发、无本地 handler 的集合。
//
// Python: CLI_FORWARD_NO_LOCAL_HANDLER_METHODS（L199-329），75 项。
// 差集（forward − no_local）恰为 5 个 chat 类方法。
var ForwardNoLocalHandlerMethods = map[string]bool{
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

// BuildRouteBinding 构建 /tui CLI 路由 binding。
//
// 对齐 Python build_cli_route_binding（tui_connect.py L2093-2119）：
// forward 集合 80 项、no-local 集合 75 项（差集=5 个 chat 类方法，
// 计数已机器核对 2026-10-28，见 spec 附录 A）；
// install 回调在 GatewayServer 构造后注册 5 个本地 handler
// （对应 Python register_cli_handlers 的 channel 注入：handler 用的 channel 与
// 注册用的 channel 是同一个对象，故此处由 Install 的 server 统一提供）；
// disconnect_handler 桥接 CancelAgentSessionsOnDisconnect。
//
// 目录对齐：Python 该实现位于 jiuwenswarm/gateway/channel_manager/tui/tui_connect.py，
// 2026-11-01 由 gateway 包迁入本包（原 gateway/tui_route.go 注释里"tui 装配放
// gateway 包"的偏差已消除）。
func BuildRouteBinding(params RouteParams) *routing.RouteBinding {
	return &routing.RouteBinding{
		Path:                         "/tui",
		ChannelID:                    "tui",
		ForwardMethods:               ForwardReqMethods,
		ForwardNoLocalHandlerMethods: ForwardNoLocalHandlerMethods,
		DisconnectHandler:            disconnectHandler(params.MessageHandler),
		Install: func(server any) {
			// Python: register_cli_handlers(channel=gateway_server, ...)——
			// channel 既用于 register_local_handler(path, method, handler)，
			// 也被 handler 用来 send_response。
			rs, ok := server.(RouteServer)
			if !ok {
				// 断言失败会让 /tui 的 5 个本地 handler（chat.send/resume/interrupt/
				// user_answer、history.get）全部不注册，消息改走转发路径，现象是
				// "ack 变慢/行为变了"而日志里毫无线索。RouteServer 是 ResponseSender
				// 与 RegisterLocalHandler 的组合接口，任一处签名漂移都会走到这里。
				logger.Warn(logComponent).Str("server_type", fmt.Sprintf("%T", server)).
					Msg("tui Install: server 未实现 RouteServer，本地 handler 全部未注册")
				return
			}
			rs.RegisterLocalHandler("/tui", "chat.send", chatSendHandler(rs))
			rs.RegisterLocalHandler("/tui", "chat.resume", chatResumeHandler(rs))
			rs.RegisterLocalHandler("/tui", "chat.interrupt", chatInterruptHandler(rs))
			rs.RegisterLocalHandler("/tui", "chat.user_answer", chatUserAnswerHandler(rs))
			rs.RegisterLocalHandler("/tui", "history.get", historyGetHandler(rs))
			// 记忆 RPC（对齐 Python tui_connect.py L1903-1907）
			for method := range memoryHandlers {
				rs.RegisterLocalHandler("/tui", method, memoryLocalHandler(rs, method))
			}
		},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// chatSendHandler chat.send 本地 ack。
//
// Python: _chat_send（tui_connect.py L1402-1405）——仅返回 ack，
// 消息转发由 ForwardMethods 集合路径完成。
func chatSendHandler(send ResponseSender) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, _ map[string]any, sessionID string) error {
		send.SendResFrame(ws, reqID, true, map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}, "", "")
		return nil
	}
}

// chatResumeHandler chat.resume 本地 ack。
//
// Python: _chat_resume（L1407-1410）。
func chatResumeHandler(send ResponseSender) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, _ map[string]any, sessionID string) error {
		send.SendResFrame(ws, reqID, true, map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}, "", "")
		return nil
	}
}

// chatInterruptHandler chat.interrupt 本地 ack（intent 可选透传）。
//
// Python: _chat_interrupt（L1412-1417）。
func chatInterruptHandler(send ResponseSender) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
		payload := map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}
		if intent, ok := params["intent"].(string); ok && intent != "" {
			payload["intent"] = intent
		}
		send.SendResFrame(ws, reqID, true, payload, "", "")
		return nil
	}
}

// chatUserAnswerHandler chat.user_answer 本地 ack（request_id 可选透传）。
//
// Python: _chat_user_answer（L1419-1424）。
func chatUserAnswerHandler(send ResponseSender) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
		payload := map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}
		if rid, ok := params["request_id"].(string); ok && rid != "" {
			payload["request_id"] = rid
		}
		send.SendResFrame(ws, reqID, true, payload, "", "")
		return nil
	}
}

// historyGetHandler history.get 本地 ack（session_id 覆写 + page_idx 透传）。
//
// Python: _history_get（L1426-1433）。
func historyGetHandler(send ResponseSender) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
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
		send.SendResFrame(ws, reqID, true, payload, "", "")
		return nil
	}
}

// disconnectHandler 构造断连回调：桥接 CancelAgentSessionsOnDisconnect。
//
// Python: _tui_disconnect（tui_connect.py L2106-2110）——
// mh 为 None 或无 stale keys 时直接返回。
func disconnectHandler(mh *messagehandler.MessageHandler) routing.DisconnectFunc {
	return func(_ *websocket.Conn, staleSessionKeys [][2]string) {
		if mh == nil || len(staleSessionKeys) == 0 {
			return
		}
		mh.CancelAgentSessionsOnDisconnect(context.Background(), staleSessionKeys)
	}
}
