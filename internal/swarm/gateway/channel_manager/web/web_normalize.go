package web

import (
	"encoding/json"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NormAndForwardFunc 标准化并转发消息的回调函数类型。
//
// 对齐 Python _normalize_and_forward_message：先 normalize 再 deliver_to_message_handler。
// 返回 true 表示短路后续本地 handler，false 表示本地 handler 继续执行。
type NormAndForwardFunc func(msg *schema.Message) bool

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ForwardReqMethods 仅转发到 Agent 的 Web method 集合。
//
// 这些方法的消息会通过 _on_message_cb 转发到 MessageHandler，
// 同时本地 handler 也会执行返回 ack 响应。
//
// 对齐 Python _FORWARD_REQ_METHODS
var ForwardReqMethods = map[string]bool{
	"initialize":                     true,
	"session.create":                 true,
	"session.switch":                 true,
	"acp.tool_response":              true,
	"team.delete":                    true,
	"session.delete":                 true,
	"chat.send":                      true,
	"chat.interrupt":                 true,
	"chat.resume":                    true,
	"chat.user_answer":               true,
	"history.get":                    true,
	"browser.start":                  true,
	"skills.marketplace.list":        true,
	"skills.list":                    true,
	"skills.installed":               true,
	"skills.get":                     true,
	"skills.toggle":                  true,
	"skills.install":                 true,
	"skills.import_local":            true,
	"skills.marketplace.add":         true,
	"skills.marketplace.remove":      true,
	"skills.marketplace.toggle":      true,
	"skills.uninstall":               true,
	"skills.skillnet.search":         true,
	"skills.skillnet.install":        true,
	"skills.skillnet.install_status": true,
	"skills.skillnet.evaluate":       true,
	"skills.clawhub.get_token":       true,
	"skills.clawhub.set_token":       true,
	"skills.clawhub.search":          true,
	"skills.clawhub.download":        true,
	"skills.teamskillshub.info":      true,
	"skills.teamskillshub.init":      true,
	"skills.teamskillshub.validate":  true,
	"skills.teamskillshub.pack":      true,
	"skills.teamskillshub.search":    true,
	"skills.teamskillshub.install":   true,
	"skills.teamskillshub.publish":   true,
	"skills.teamskillshub.delete":    true,
	"skills.evolution.status":        true,
	"skills.evolution.get":           true,
	"skills.evolution.save":          true,
	"plugins.list":                   true,
	"plugins.install":                true,
	"plugins.uninstall":              true,
	"plugins.enable":                 true,
	"plugins.disable":                true,
	"plugins.reload":                 true,
	"extensions.list":                true,
	"extensions.import":              true,
	"extensions.delete":              true,
	"extensions.toggle":              true,
	"team.snapshot":                  true,
	"team.history.get":               true,
	"agents.list":                    true,
	"agents.get":                     true,
	"agents.create":                  true,
	"agents.update":                  true,
	"agents.delete":                  true,
	"agents.enable":                  true,
	"agents.disable":                 true,
	"agents.tools_list":              true,
	"schedule.check_config":          true,
	"schedule.update_config":         true,
	"schedule.create":                true,
	"schedule.run":                   true,
	"schedule.list":                  true,
	"schedule.status":                true,
	"schedule.logs":                  true,
	"schedule.cancel":                true,
	"schedule.delete":                true,
}

// ForwardNoLocalHandlerMethods 仅转发、无本地 handler 的方法集合。
//
// 这些方法的消息仅通过 _on_message_cb 转发，本地没有对应 handler。
// 与 ForwardReqMethods 的差集即为"既有转发又有本地 handler"的方法
// （如 chat.send、chat.interrupt 等）。
//
// 对齐 Python _FORWARD_NO_LOCAL_HANDLER_METHODS
var ForwardNoLocalHandlerMethods = map[string]bool{
	"initialize":                     true,
	"session.create":                 true,
	"session.switch":                 true,
	"acp.tool_response":              true,
	"team.delete":                    true,
	"browser.start":                  true,
	"team.snapshot":                  true,
	"team.history.get":               true,
	"skills.marketplace.list":        true,
	"skills.list":                    true,
	"skills.installed":               true,
	"skills.get":                     true,
	"skills.toggle":                  true,
	"skills.install":                 true,
	"skills.import_local":            true,
	"skills.marketplace.add":         true,
	"skills.marketplace.remove":      true,
	"skills.marketplace.toggle":      true,
	"skills.uninstall":               true,
	"skills.skillnet.search":         true,
	"skills.skillnet.install":        true,
	"skills.skillnet.install_status": true,
	"skills.skillnet.evaluate":       true,
	"skills.clawhub.get_token":       true,
	"skills.clawhub.set_token":       true,
	"skills.clawhub.search":          true,
	"skills.clawhub.download":        true,
	"skills.teamskillshub.info":      true,
	"skills.teamskillshub.init":      true,
	"skills.teamskillshub.validate":  true,
	"skills.teamskillshub.pack":      true,
	"skills.teamskillshub.search":    true,
	"skills.teamskillshub.install":   true,
	"skills.teamskillshub.publish":   true,
	"skills.teamskillshub.delete":    true,
	"skills.evolution.status":        true,
	"skills.evolution.get":           true,
	"skills.evolution.save":          true,
	"plugins.list":                   true,
	"plugins.install":                true,
	"plugins.uninstall":              true,
	"plugins.enable":                 true,
	"plugins.disable":                true,
	"plugins.reload":                 true,
	"extensions.list":                true,
	"extensions.import":              true,
	"extensions.delete":              true,
	"extensions.toggle":              true,
	"agents.list":                    true,
	"agents.get":                     true,
	"agents.create":                  true,
	"agents.update":                  true,
	"agents.delete":                  true,
	"agents.enable":                  true,
	"agents.disable":                 true,
	"agents.tools_list":              true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeGatewayMessage 对网关入站消息进行标准化处理。
//
// 转换规则（对齐 Python _normalize_gateway_message）：
//  1. content → query：params 中无 query 但有 content 时，复制 content 到 query
//  2. resume → cancel：req_method 为 CHAT_RESUME 时，改为 CHAT_CANCEL 并设置 intent=resume
//  3. is_stream 推断：msg.IsStream 或 method 为 chat.send/history.get 时为 true
//
// 返回标准化后的新 Message（不修改原始消息）。
func NormalizeGatewayMessage(msg *schema.Message) *schema.Message {
	reqMethod := msg.ReqMethod
	if reqMethod == "" {
		reqMethod = schema.ReqMethodChatSend
	}

	// 解析 params 并处理 content → query 映射
	var params map[string]any
	if len(msg.Params) > 0 {
		_ = json.Unmarshal(msg.Params, &params)
	}
	if params == nil {
		params = make(map[string]any)
	}
	if _, hasQuery := params["query"]; !hasQuery {
		if content, hasContent := params["content"]; hasContent {
			params["query"] = content
		}
	}

	// resume → cancel + intent=resume
	if reqMethod == schema.ReqMethodChatResume {
		reqMethod = schema.ReqMethodChatCancel
		if _, hasIntent := params["intent"]; !hasIntent {
			params["intent"] = "resume"
		}
	}

	// is_stream 推断
	isStream := msg.IsStream || reqMethod == schema.ReqMethodChatSend || reqMethod == schema.ReqMethodHistoryGet

	// 重新序列化 params
	paramsJSON, _ := json.Marshal(params)

	// 构建标准化后的新消息
	return &schema.Message{
		ID:        msg.ID,
		Type:      msg.Type,
		ChannelID: msg.ChannelID,
		SessionID: msg.SessionID,
		Params:    paramsJSON,
		Timestamp: msg.Timestamp,
		OK:        msg.OK,
		ReqMethod: reqMethod,
		Mode:      msg.Mode,
		IsStream:  isStream,
		StreamSeq: msg.StreamSeq,
		StreamID:  msg.StreamID,
		Metadata:  msg.Metadata,
	}
}

// MakeNormAndForward 创建标准化+转发回调函数。
//
// 对齐 Python _make_norm_and_forward (app_gateway.py)：
// 三层路由逻辑：
//   - 非 forwardMethods → 不转发，返回 false（让本地 handler 处理）
//   - forwardMethods 中且有本地 handler → 转发 + 返回 false（本地 handler 继续执行）
//   - noLocalHandlerMethods 中 → 仅转发 + 返回 true（短路本地 handler）
func MakeNormAndForward(
	channelMgr *cm.ChannelManager,
	forwardMethods map[string]bool,
	noLocalHandlerMethods map[string]bool,
) NormAndForwardFunc {
	return func(msg *schema.Message) bool {
		// 从 metadata 获取 method
		methodVal := ""
		if msg.Metadata != nil {
			if m, ok := msg.Metadata["method"].(string); ok {
				methodVal = m
			}
		}

		// 非 ForwardReqMethods → 不转发，让本地 handler 处理
		if !forwardMethods[methodVal] {
			return false
		}

		// 在 ForwardReqMethods 中 → 转发到 MessageHandler
		normalized := NormalizeGatewayMessage(msg)
		channelMgr.DeliverToMessageHandler(normalized)

		logger.Info(logComponent).
			Str("event_type", "gateway_inbound").
			Str("msg_id", msg.ID).
			Str("channel_id", msg.ChannelID).
			Str("method", methodVal).
			Msg("Gateway inbound -> MessageHandler")

		// ForwardNoLocalHandlerMethods → 短路本地 handler
		if noLocalHandlerMethods[methodVal] {
			return true
		}
		return false
	}
}

// BuildUserMessage 从 RPC 请求参数构建入站 Message。
//
// 对齐 Python WebChannel._handle_raw_message 中构建 user_message 的逻辑。
func BuildUserMessage(reqID, method string, params map[string]any, sessionID string, query map[string][]string) *schema.Message {
	reqMethod, _ := schema.ParseReqMethod(method)
	paramsJSON, _ := json.Marshal(params)

	return &schema.Message{
		ID:        reqID,
		Type:      schema.MessageTypeReq,
		ChannelID: "web",
		SessionID: sessionID,
		Params:    paramsJSON,
		Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
		OK:        true,
		ReqMethod: reqMethod,
		Mode:      parseMode(params),
		Metadata:  map[string]any{"method": method, "query": query},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseMode 从 params 解析运行模式。
//
// 对齐 Python WebChannel._parse_mode，默认 Mode.AGENT_PLAN。
func parseMode(params map[string]any) schema.Mode {
	if params != nil {
		if mode, ok := params["mode"].(string); ok && mode != "" {
			return schema.Mode(mode)
		}
	}
	return schema.ModeAgentPlan
}
