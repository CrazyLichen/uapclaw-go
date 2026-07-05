package handoff

import (
	"encoding/json"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HandoffSignal 交接信号，携带目标 Agent、上下文消息和交接原因。
//
// 对应 Python: HandoffSignal(target=str, message=Optional[str], reason=Optional[str])
// Python 中 HandoffSignal 是 frozen dataclass，Go 中为值结构体。
type HandoffSignal struct {
	// Target 目标 Agent ID
	Target string
	// Message 上下文消息（可选）
	Message string
	// Reason 交接原因（可选）
	Reason string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// HandoffTargetKey 交接目标键名
	HandoffTargetKey = "__handoff_to__"
	// HandoffMessageKey 交接消息键名
	HandoffMessageKey = "__handoff_message__"
	// HandoffReasonKey 交接原因键名
	HandoffReasonKey = "__handoff_reason__"
	// defaultContextID 默认上下文标识
	defaultContextID = "default_context_id"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// stateKeyContext 上下文状态键
var stateKeyContext = state.StringKey("context")

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractHandoffSignal 从 Agent 执行结果中提取交接信号。
//
// 两层提取策略：
//  1. 第一层：从 result map 中查找 handoff payload（findHandoffPayload）
//  2. 第二层：若第一层未找到且 agentSession 非 nil，从 session 消息历史查找（findHandoffFromSession）
//
// 对应 Python: extract_handoff_signal(result, agent_session=None)
func ExtractHandoffSignal(result map[string]any, agentSession sessioninterfaces.SessionFacade) *HandoffSignal {
	// 第一层：从 result map 中查找
	payload := findHandoffPayload(result)
	if payload == nil && agentSession != nil {
		// 第二层：从 session 消息历史查找
		payload = findHandoffFromSession(agentSession)
	}
	if payload == nil {
		return nil
	}

	// 提取 target 字段
	target, ok := payload[HandoffTargetKey]
	if !ok {
		return nil
	}
	targetStr, ok := target.(string)
	if !ok || targetStr == "" {
		return nil
	}

	// 提取 message 和 reason 字段
	message := ""
	if msg, ok := payload[HandoffMessageKey]; ok {
		if msgStr, ok := msg.(string); ok {
			message = msgStr
		}
	}
	reason := ""
	if rsn, ok := payload[HandoffReasonKey]; ok {
		if rsnStr, ok := rsn.(string); ok {
			reason = rsnStr
		}
	}

	return &HandoffSignal{
		Target:  targetStr,
		Message: message,
		Reason:  reason,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// findHandoffPayload 从 result map 中查找 handoff payload。
//
// 查找策略：
//  1. result 顶层包含 HandoffTargetKey → 直接返回 result
//  2. 遍历 output/result/content 子键，若子值为 map 且包含 HandoffTargetKey → 返回子 map
//
// 对应 Python: _find_handoff_payload(result)
func findHandoffPayload(result map[string]any) map[string]any {
	if result == nil {
		return nil
	}

	// 路径 1：顶层包含 HandoffTargetKey
	if _, ok := result[HandoffTargetKey]; ok {
		return result
	}

	// 路径 2：在 output/result/content 子键中查找
	for _, key := range []string{"output", "result", "content"} {
		sub, ok := result[key]
		if !ok {
			continue
		}
		subMap, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := subMap[HandoffTargetKey]; ok {
			return subMap
		}
	}

	return nil
}

// findHandoffFromSession 从 session 消息历史中查找 handoff payload。
//
// 查找策略：
//  1. 从 session state 获取 "context" 状态
//  2. 从 context 中获取 defaultContextID 对应的默认上下文
//  3. 从默认上下文的 messages 列表倒序查找 role="tool" 的消息
//  4. 尝试 JSON 解析消息 content，若包含 HandoffTargetKey 则返回
//
// 对应 Python: _find_handoff_from_session(agent_session)
func findHandoffFromSession(agentSession sessioninterfaces.SessionFacade) map[string]any {
	if agentSession == nil {
		return nil
	}

	// 获取 context 状态
	ctxState, err := agentSession.GetState(stateKeyContext)
	if err != nil || ctxState == nil {
		return nil
	}
	ctxStateMap, ok := ctxState.(map[string]any)
	if !ok {
		return nil
	}

	// 获取默认上下文
	defaultCtx, ok := ctxStateMap[defaultContextID]
	if !ok {
		return nil
	}
	defaultCtxMap, ok := defaultCtx.(map[string]any)
	if !ok {
		return nil
	}

	// 获取消息列表
	messagesVal, ok := defaultCtxMap["messages"]
	if !ok {
		return nil
	}
	messages, ok := messagesVal.([]any)
	if !ok {
		return nil
	}

	// 倒序遍历消息，查找 role="tool" 且 content 包含 HandoffTargetKey
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := msg["role"]
		roleStr, ok := role.(string)
		if !ok || roleStr != "tool" {
			continue
		}
		content := msg["content"]
		contentStr, ok := content.(string)
		if !ok || contentStr == "" {
			continue
		}

		// 尝试 JSON 解析
		var parsed map[string]any
		if err := json.Unmarshal([]byte(contentStr), &parsed); err == nil {
			if _, ok := parsed[HandoffTargetKey]; ok {
				return parsed
			}
		}
	}

	return nil
}
