package message_handler

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareAgentDispatchMessage 准备发往 AgentServer 的消息。
//
// 对齐 Python _prepare_agent_dispatch_message (L1287-1312)：
// 当前实现为 identity return（直接返回 msg），ACP session alias 处理待后续回填。
func (mh *MessageHandler) prepareAgentDispatchMessage(_ context.Context, msg *schema.Message) *schema.Message {
	// TODO: ACP session alias 处理（等 ACP 章节回填）
	// Python: if msg.channel_id == _ACP_CHANNEL_ID:
	//     msg = await self._ensure_acp_agent_session(msg)
	return msg
}

// shouldEmitProcessingStatusForStream 判断是否需要为流式请求发送 processing_status 事件。
//
// 对齐 Python _should_emit_processing_status_for_stream (L1866-1890)：
// 仅 chat.send 请求发送 processing_status，其他请求不发送。
func (mh *MessageHandler) shouldEmitProcessingStatusForStream(msg *schema.Message) bool {
	return msg.ReqMethod == schema.ReqMethodChatSend
}

// nonStreamRPCMayRunParallel 判断非流式 RPC 是否可以并行执行，避免慢 RPC 阻塞队列。
//
// 对齐 Python _non_stream_rpc_may_run_parallel (L1837-1865)：
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
