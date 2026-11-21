package agent_teams

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/vcs"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentContextReader fork 源上下文读取面（spawn.ForkContextSource 的同构镜像——
// fork.go 在 agent_teams 根包，不能 import spawn 子包，故本地声明同款窄接口）。
type AgentContextReader interface {
	// GetCurrentContext 返回当前上下文消息
	GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error)
}

// AgentSessionContextReader 带持久化兜底的 fork 源读取面。
// Python: agent.loop_session.get_state("context") 读取路径的窄接口化。
type AgentSessionContextReader interface {
	AgentContextReader
	// PersistedContextMessages 读取源成员绑定会话持久化的上下文消息；
	// 无可用状态返回 nil（调用方重抛原错误）。
	PersistedContextMessages() []llmschema.BaseMessage
}

// ForkContext 可序列化的 fork 上下文快照（json-native，可跨进程传输）。
// 抓取源成员的完整对话历史注入新成员的 context engine；SystemMessage 全程
// 剥除——目标成员的角色由 TeamPolicyRail 注入，绝不从 fork 源泄漏。
//
// Python: ForkContext dataclass (fork.py:32-46)
type ForkContext struct {
	// Messages 编码后的消息列表（json 原生 dict），可跨进程传输
	Messages []map[string]any `json:"messages"`
	// CompactSplit 非 nil 时 compact_context 压缩 compact_direction 的对侧。
	// 由调用方在抓取后设置。
	// Python: compact_split (fork.py:42-44)
	CompactSplit *int `json:"compact_split,omitempty"`
	// CompactDirection "before"（默认，压缩尾部）/ "after"（压缩头部，保留尾部原文）。
	// Python: compact_direction (fork.py:45-46)
	CompactDirection string `json:"compact_direction,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ForkContextFromMessages 从消息列表构造 fork 快照。
//
// 全程剥除 SystemMessage；checkpoint 非 nil 时按 keep 侧截断：
//   - "before"（默认）：保留 checkpoint 之前的消息。checkpoint 记录在工具
//     调用时点（len(messages)），落在携带 checkpoint 调用的 assistant 之后、
//     其 ToolMessage 结果之前——把闭合 ToolMessage（可能连续多条）跨边界
//     携带过去，否则注入上下文出现悬挂 tool call，产品 rail 会标记
//     「[工具执行被中断]」。
//   - "after"：保留 checkpoint 起的消息。after 窗口起点是 checkpoint 调用
//     的结果块，产生这些调用的 assistant 未被继承，前导孤儿 ToolMessage
//     需剥除。
//
// Python: ForkContext.from_agent (fork.py:52-117) 的截断半区；
// agent 读取半区（GetCtxMessages/持久化兜底）见 TeamAgent.resolveForkSource。
func ForkContextFromMessages(msgs []llmschema.BaseMessage, checkpoint *int, keep string) *ForkContext {
	filtered := make([]llmschema.BaseMessage, 0, len(msgs))
	for _, m := range msgs {
		// Python: msgs = [m for m in msgs if not isinstance(m, SystemMessage)]（fork.py:66）
		if _, isSystem := m.(*llmschema.SystemMessage); isSystem {
			continue
		}
		filtered = append(filtered, m)
	}

	// Python: if checkpoint is not None: if 0 <= checkpoint < len(msgs):（fork.py:68-69）
	if checkpoint != nil && *checkpoint >= 0 && *checkpoint < len(filtered) {
		if keep == "after" {
			// Python: fork.py:88-93——剥前导孤儿 ToolMessage 后全保留
			filtered = trimLeadingOrphanToolMessages(filtered[*checkpoint:])
		} else {
			// Python: fork.py:94-105——保留 checkpoint 之前，补闭合 ToolMessage 块
			truncated := make([]llmschema.BaseMessage, *checkpoint, *checkpoint+8)
			copy(truncated, filtered[:*checkpoint])
			if len(truncated) > 0 {
				last := truncated[len(truncated)-1]
				if isAssistantWithToolCalls(last) && isToolMessage(filtered[*checkpoint]) {
					for i := *checkpoint; i < len(filtered) && isToolMessage(filtered[i]); i++ {
						truncated = append(truncated, filtered[i])
					}
				}
			}
			filtered = truncated
		}
	}

	return &ForkContext{
		Messages:         encodeForkMessages(filtered),
		CompactDirection: "before",
	}
}

// ForkContextFromAgent 从源 agent 抓快照（含持久化兜底）。
//
// 内存上下文读取失败（源 native 被 pause/resume/restart 重建、context pool
// 尚未懒加载）时，回落读源成员绑定会话持久化的消息——fork 继承依然生效。
// 兜底也无果时以 panic 上抛，由调用方（TeamAgent.buildForkContext 的
// defer/recover）收敛为「无继承继续 spawn」。
//
// Python: ForkContext.from_agent (fork.py:52-117)
func ForkContextFromAgent(ctx context.Context, source AgentContextReader, sessionID string, checkpoint *int, keep string) *ForkContext {
	msgs, err := source.GetCurrentContext(ctx, sessionID, "default_context_id")
	if err != nil {
		// Python: except Exception → cls._read_persisted_messages(agent)（fork.py:57-64）
		if sr, ok := source.(AgentSessionContextReader); ok {
			if persisted := sr.PersistedContextMessages(); persisted != nil {
				return ForkContextFromMessages(persisted, checkpoint, keep)
			}
		}
		// Python: raise exc（fork.py:65）——由调用方 except 兜底
		panic(err)
	}
	return ForkContextFromMessages(msgs, checkpoint, keep)
}

// NormalizeForkMessages 返回消息列表原样（BaseMessage 直收分支）。
// 与 NormalizeForkMessageValues 共同对齐 Python normalize_messages 的三的行为。
// Python: normalize_messages (fork.py:150-175)
func NormalizeForkMessages(messages []llmschema.BaseMessage) []llmschema.BaseMessage {
	if messages == nil {
		return nil
	}
	return append([]llmschema.BaseMessage(nil), messages...)
}

// NormalizeForkMessageValues 把 session state 中读到的消息值列表还原为
// BaseMessage 列表：BaseMessage 直收 / dict decode / 其他 → nil（整体退化，
// 不静默降级）。Python: normalize_messages (fork.py:150-175)
func NormalizeForkMessageValues(messages []any) []llmschema.BaseMessage {
	normalized := make([]llmschema.BaseMessage, 0, len(messages))
	for _, message := range messages {
		switch m := message.(type) {
		case llmschema.BaseMessage:
			normalized = append(normalized, m)
		case *llmschema.UserMessage:
			normalized = append(normalized, m)
		case *llmschema.AssistantMessage:
			normalized = append(normalized, m)
		case *llmschema.SystemMessage:
			normalized = append(normalized, m)
		case *llmschema.ToolMessage:
			normalized = append(normalized, m)
		case map[string]any:
			// Python: decode_message(message)（fork.py:165-168）——
			// 畸形持久化消息整体退化返回 nil
			decoded := vcs.DecodeMessage(m)
			if decoded == nil {
				return nil
			}
			normalized = append(normalized, decoded)
		default:
			// Python: else: return None（fork.py:169-171）
			return nil
		}
	}
	return normalized
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trimLeadingOrphanToolMessages 剥除前导孤儿 ToolMessage（其 assistant 未被继承）。
// after 窗口从 checkpoint 调用的结果块开始，结果没有对应 assistant 会显得
// 像被 context rail 中断。
// Python: _trim_leading_orphan_tool_messages (fork.py:119-131)
func trimLeadingOrphanToolMessages(messages []llmschema.BaseMessage) []llmschema.BaseMessage {
	start := 0
	for start < len(messages) && isToolMessage(messages[start]) {
		start++
	}
	return messages[start:]
}

// isToolMessage 判断是否 ToolMessage。
func isToolMessage(m llmschema.BaseMessage) bool {
	_, ok := m.(*llmschema.ToolMessage)
	return ok
}

// isAssistantWithToolCalls 判断是否携带 tool_calls 的 assistant。
// Python: isinstance(last, AssistantMessage) and getattr(last, "tool_calls", None)（fork.py:96-97）
func isAssistantWithToolCalls(m llmschema.BaseMessage) bool {
	assistant, ok := m.(*llmschema.AssistantMessage)
	return ok && len(assistant.ToolCalls) > 0
}

// encodeForkMessages 编码为 json-native dict 列表（复用 vcs codec，
// Python fork.py 用同一 codec：from ...vcs.codec import encode_message）。
func encodeForkMessages(msgs []llmschema.BaseMessage) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, vcs.EncodeMessage(m))
	}
	return out
}

// ──────────────────────────── ForkContext 方法 ────────────────────────────

// ToForkMessages 解码回 BaseMessage 列表，供 context engine 注入。
// Python: to_messages (fork.py:177-179)
func (fc *ForkContext) ToForkMessages() []llmschema.BaseMessage {
	out := make([]llmschema.BaseMessage, 0, len(fc.Messages))
	for _, d := range fc.Messages {
		out = append(out, vcs.DecodeMessage(d))
	}
	return out
}

// IsEmpty 返回是否没有抓到任何消息。
// Python: is_empty (fork.py:181-183)
func (fc *ForkContext) IsEmpty() bool {
	return len(fc.Messages) == 0
}
