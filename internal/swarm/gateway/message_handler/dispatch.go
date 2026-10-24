package message_handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareAgentDispatchMessage 准备发往 AgentServer 的消息。
//
// Python: _prepare_agent_dispatch_message (L1287-1312)：
// 当前实现为 identity return（直接返回 msg），ACP session alias 处理待后续回填。
func (mh *MessageHandler) prepareAgentDispatchMessage(_ context.Context, msg *schema.Message) *schema.Message {
	// TODO(#ACP): ACP session alias 处理（等 ACP 章节回填）
	// Python: ACP渠道检测
	//     Python: msg = await self._ensure_acp_agent_session(msg)
	return msg
}

// shouldEmitProcessingStatusForStream 判断是否需要为流式请求发送 processing_status 事件。
//
// Python: _should_emit_processing_status_for_stream (L1866-1890)：
// 仅 chat.send 请求发送 processing_status，其他请求不发送。
func (mh *MessageHandler) shouldEmitProcessingStatusForStream(msg *schema.Message) bool {
	return msg.ReqMethod == schema.ReqMethodChatSend
}

// nonStreamRPCMayRunParallel 判断非流式 RPC 是否可以并行执行，避免慢 RPC 阻塞队列。
//
// Python: _non_stream_rpc_may_run_parallel (L1837-1865)：
// chat.send / chat.cancel / chat.resume / chat.user_answer 必须串行，其他非流式 RPC 可并行。
func (mh *MessageHandler) nonStreamRPCMayRunParallel(env *e2a.E2AEnvelope) bool {
	if env.IsStream {
		return false
	}
	method := strings.ToLower(env.Method)
	return method != string(schema.ReqMethodChatSend) &&
		method != string(schema.ReqMethodChatCancel) &&
		method != "chat.resume" &&
		method != string(schema.ReqMethodChatAnswer)
}

// extractModeFromParams 从消息 params 中提取 mode 字段。
//
// 供 forwardLoop 中注册 streamModes 使用。
func (mh *MessageHandler) extractModeFromParams(msg *schema.Message) string {
	if len(msg.Params) == 0 {
		return "plan"
	}
	var paramsMap map[string]any
	if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
		return "plan"
	}
	if mode, ok := paramsMap["mode"]; ok {
		if s, isStr := mode.(string); isStr && s != "" {
			return s
		}
	}
	return "plan"
}

// shouldTriggerBeforeChatRequestHook 判断是否触发聊天前钩子。
//
// Python: _should_trigger_before_chat_request_hook (L1856-1863)：
// 仅 CHAT_SEND / CHAT_RESUME / CHAT_ANSWER。
func (mh *MessageHandler) shouldTriggerBeforeChatRequestHook(msg *schema.Message) bool {
	return msg.ReqMethod == schema.ReqMethodChatSend ||
		msg.ReqMethod == schema.ReqMethodChatResume ||
		msg.ReqMethod == schema.ReqMethodChatAnswer
}

// triggerBeforeChatRequestHook 触发 Gateway 聊天前钩子。
//
// Python: _trigger_before_chat_request_hook (L1871-1888)：
// 构建 GatewayChatHookContext → trigger(BEFORE_CHAT_REQUEST) → 扩展原地改 params →
// 从同一对象读回（Go：FromMap 写回后重新序列化进 agentMsg.Params）。
// params 非对象时归一化为空 map（对齐 Python isinstance 检查）。
func (mh *MessageHandler) triggerBeforeChatRequestHook(ctx context.Context, agentMsg *schema.Message) {
	if !mh.shouldTriggerBeforeChatRequestHook(agentMsg) {
		return
	}
	extReg, extErr := extensions.GetInstanceErr()
	if extErr != nil || extReg == nil {
		return
	}

	var params map[string]any
	if len(agentMsg.Params) > 0 {
		_ = json.Unmarshal(agentMsg.Params, &params)
	}
	if params == nil {
		params = map[string]any{}
	}

	var reqMethodStr *string
	if agentMsg.ReqMethod != "" {
		s := string(agentMsg.ReqMethod)
		reqMethodStr = &s
	}
	// SessionID 条件取指针：空串留 nil，对齐 Python msg.session_id 为 None 时
	// hook ctx 收到 None（message.py L246 str | None）。空串取指针会把
	// session_id 序列化为 "" 而非 null，扩展判存语义有差（T-08）。
	var sessionIDStr *string
	if agentMsg.SessionID != "" {
		s := agentMsg.SessionID
		sessionIDStr = &s
	}
	hookCtx := &extensions.GatewayChatHookContext{
		RequestID: agentMsg.ID,
		ChannelID: agentMsg.ChannelID,
		SessionID: sessionIDStr,
		ReqMethod: reqMethodStr,
		Params:    params,
	}
	data := hookCtx.ToMap()
	extReg.Trigger(ctx, extensions.GatewayBeforeChatRequest, data)
	hookCtx.FromMap(data)
	if hookCtx.Params != nil {
		if raw, err := json.Marshal(hookCtx.Params); err == nil {
			agentMsg.Params = raw
		}
	}
}
