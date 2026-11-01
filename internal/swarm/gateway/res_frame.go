package gateway

import "github.com/gorilla/websocket"

// ──────────────────────────── 导出函数 ────────────────────────────

// SendResFrame 写入 res 帧，供通道包的本地 handler（如 channel_manager/tui）复用。
//
// Python 对应：本地 handler 通过 register_cli_handlers(channel=gateway_server) 拿到
// channel 对象，handler 内直接调 channel.send_response(...)（tui_connect.py）。Go 侧
// 以「注入 ResponseSender」表达同一依赖：tui.BuildRouteBinding 的 RouteParams.Sender
// 由装配点（app_gateway.go）传入本 server 实例。
//
// 语义完全转发 writeResponseFrame（零行为变更）：includePayload=false 时不带
// payload 键，对齐 Python 内联错误帧的四键形状。
func (s *GatewayServer) SendResFrame(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string, includePayload bool) {
	writeResponseFrame(ws, reqID, ok, payload, errMsg, code, includePayload)
}
