// session/update 构造纯函数（11.25）。
// 对齐 Python jiuwenswarm/common/e2a/acp/session_updates.py 全 185 行。
package acp

import (
	"strings"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpSessionUpdateState session/update 状态跟踪。
// Python: AcpSessionUpdateState Protocol（L15-21）；Protocol→struct 归档偏差 §9-7。
// Python 字段 None 初值 → Go 字符串零值 "" / map 零值 nil，语义一致。
type AcpSessionUpdateState struct {
	// AssistantMessageID assistant 消息 ID（空串 = Python None）
	AssistantMessageID string
	// AssistantText assistant 累积文本
	AssistantText string
	// ThoughtMessageID thought 消息 ID
	ThoughtMessageID string
	// ThoughtText thought 累积文本
	ThoughtText string
	// UserMessageID user 消息 ID（Python L20，session_updates 内部不写入）
	UserMessageID string
	// ToolCallCache 工具调用缓存（toolCallID → descriptor 摘要）
	ToolCallCache map[string]map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// buildIncrementalTextUpdateKind 文本槽位判别（替代 Python 传 ensure/reset 函数对的
// 惰性求值语义：reset 只在回退分支真正调用，ensure 只在需要时调用）。
type buildIncrementalTextUpdateKind int

const (
	assistantTextKind buildIncrementalTextUpdateKind = iota
	thoughtTextKind
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildAcpSessionUpdate 构造 session/update。
// Python: build_acp_session_update (L101-147)。
// 分流：delta/reasoning → 文本 chunk（IsReasoningEvent 复用）；tool_call → BuildAcpToolCallUpdate；
// tool_update/tool_result → BuildAcpToolResultUpdate；subtask_update → plan 原样；
// todo_updated → BuildAcpTodoUpdate；processing_status → session_info_update；其余 nil。
func BuildAcpSessionUpdate(msg *schema.Message, payload map[string]any, state *AcpSessionUpdateState) map[string]any {
	switch msg.EventType {
	case schema.EventTypeChatDelta, schema.EventTypeChatReasoning:
		text := strAny(payload["content"])
		if text == "" {
			return nil
		}
		// 注意：EventType 是 defined type，须经 string() 转换再传 any 断言，
		// 否则 IsReasoningEvent 内部 .(string) 静默失败（chat.reasoning 会被误判）。
		if IsReasoningEvent(string(msg.EventType), payload) {
			state.ThoughtText += text
			return map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"messageId":     ensureThoughtMessageID(state),
				"content":       map[string]any{"type": "text", "text": text},
			}
		}
		state.AssistantText += text
		return map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"messageId":     ensureAssistantMessageID(state),
			"content":       map[string]any{"type": "text", "text": text},
		}
	case schema.EventTypeChatToolCall:
		return BuildAcpToolCallUpdate(payload, ensureToolCallCache(state))
	case schema.EventTypeChatToolUpdate, schema.EventTypeChatToolResult:
		return BuildAcpToolResultUpdate(payload, ensureToolCallCache(state))
	case schema.EventTypeChatSubtaskUpdate:
		return map[string]any{
			"sessionUpdate": "plan",
			"plan":          cpMap(payload),
		}
	case schema.EventTypeTodoUpdated:
		return BuildAcpTodoUpdate(payload)
	case schema.EventTypeChatProcessingStatus:
		return map[string]any{
			"sessionUpdate": "session_info_update",
			"status":        processingStatusFromPayload(payload),
		}
	}
	return nil
}

// BuildAcpFinalTextUpdate 构造 final text update。
// Python: build_acp_final_text_update (L150-175)：is_reasoning_event("chat.final", payload)
// 分流 thought/assistant 后走 _build_incremental_text_update 三态。
func BuildAcpFinalTextUpdate(payload map[string]any, state *AcpSessionUpdateState) map[string]any {
	text := strAny(payload["content"])
	if text == "" {
		return nil
	}
	if IsReasoningEvent("chat.final", payload) {
		return buildIncrementalTextUpdate(state, text, thoughtTextKind, "agent_thought_chunk")
	}
	return buildIncrementalTextUpdate(state, text, assistantTextKind, "agent_message_chunk")
}

// BuildAcpUsageUpdate 构造 usage_update。
// Python: build_acp_usage_update (L178-185)：usage 非 dict 或空 → nil；否则一层浅拷贝。
func BuildAcpUsageUpdate(payload map[string]any) map[string]any {
	usage, _ := payload["usage"].(map[string]any)
	if len(usage) == 0 {
		return nil
	}
	return map[string]any{
		"sessionUpdate": "usage_update",
		"usage":         cpMap(usage),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureToolCallCache 确保 cache 非 nil 并返回。
// Python: _ensure_tool_call_cache (L24-29)。
func ensureToolCallCache(state *AcpSessionUpdateState) map[string]map[string]any {
	if state.ToolCallCache == nil {
		state.ToolCallCache = make(map[string]map[string]any)
	}
	return state.ToolCallCache
}

// ensureAssistantMessageID 确保非空。
// Python: _ensure_assistant_message_id (L32-35)。
func ensureAssistantMessageID(state *AcpSessionUpdateState) string {
	if state.AssistantMessageID == "" {
		state.AssistantMessageID = uuid.New().String()
	}
	return state.AssistantMessageID
}

// resetAssistantMessageID 重置换新。
// Python: _reset_assistant_message_id (L38-40)。
func resetAssistantMessageID(state *AcpSessionUpdateState) string {
	state.AssistantMessageID = uuid.New().String()
	return state.AssistantMessageID
}

// ensureThoughtMessageID 确保 thought 非空。
// Python: _ensure_thought_message_id (L43-46)。
func ensureThoughtMessageID(state *AcpSessionUpdateState) string {
	if state.ThoughtMessageID == "" {
		state.ThoughtMessageID = uuid.New().String()
	}
	return state.ThoughtMessageID
}

// resetThoughtMessageID 重置 thought 换新。
// Python: _reset_thought_message_id (L49-51)。
func resetThoughtMessageID(state *AcpSessionUpdateState) string {
	state.ThoughtMessageID = uuid.New().String()
	return state.ThoughtMessageID
}

// buildIncrementalTextUpdate 增量三态构造。
// Python: _build_incremental_text_update (L59-98)：
// 空 text → nil；existing 空 → 直发+ensure；text == existing → nil；
// 前缀扩展 → delta+ensure；回退 → 全文+reset。
// Go 形态差异（对应 Python 传 _ensure/_reset 函数对）：以 kind 枚举 + 延迟调用
// ensureXxxID/resetXxxID 保证 reset 不被急切求值（否则会无条件换 ID，破坏前缀
// 扩展复用 messageId 的语义）。
func buildIncrementalTextUpdate(state *AcpSessionUpdateState, text string, kind buildIncrementalTextUpdateKind, updateKind string) map[string]any {
	if text == "" {
		return nil
	}
	var existing string
	if kind == thoughtTextKind {
		existing = state.ThoughtText
	} else {
		existing = state.AssistantText
	}
	if existing != "" {
		if text == existing {
			return nil
		}
		var delta string
		if strings.HasPrefix(text, existing) {
			delta = text[len(existing):]
		}
		if delta != "" {
			if kind == thoughtTextKind {
				state.ThoughtText = existing + delta
			} else {
				state.AssistantText = existing + delta
			}
			return map[string]any{
				"sessionUpdate": updateKind,
				"messageId":     ensureIDForKind(state, kind),
				"content":       map[string]any{"type": "text", "text": delta},
			}
		}
		// 回退：全文重发 + reset 换新 messageId
		if kind == thoughtTextKind {
			state.ThoughtText = text
		} else {
			state.AssistantText = text
		}
		return map[string]any{
			"sessionUpdate": updateKind,
			"messageId":     resetIDForKind(state, kind),
			"content":       map[string]any{"type": "text", "text": text},
		}
	}
	// 全新：直发全文 + ensure
	if kind == thoughtTextKind {
		state.ThoughtText = text
	} else {
		state.AssistantText = text
	}
	return map[string]any{
		"sessionUpdate": updateKind,
		"messageId":     ensureIDForKind(state, kind),
		"content":       map[string]any{"type": "text", "text": text},
	}
}

// ensureIDForKind 槽位对应的 ensure messageID。
func ensureIDForKind(state *AcpSessionUpdateState, kind buildIncrementalTextUpdateKind) string {
	if kind == thoughtTextKind {
		return ensureThoughtMessageID(state)
	}
	return ensureAssistantMessageID(state)
}

// resetIDForKind 槽位对应的 reset messageID。
func resetIDForKind(state *AcpSessionUpdateState, kind buildIncrementalTextUpdateKind) string {
	if kind == thoughtTextKind {
		return resetThoughtMessageID(state)
	}
	return resetAssistantMessageID(state)
}

// processingStatusFromPayload 解析 processing/idle。
// Python L144: bool(payload.get("is_processing", True))。
func processingStatusFromPayload(payload map[string]any) string {
	if v, ok := payload["is_processing"].(bool); ok {
		if v {
			return "processing"
		}
		return "idle"
	}
	return "processing"
}

// cpMap 一层浅拷贝（e2a 包 copyMapAny 同构，acp 包内私有）。
func cpMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
