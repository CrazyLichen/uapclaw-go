package context_engineer

import (
	"context"
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FixIncompleteToolContext 验证并修复上下文中不完整的 tool_call/ToolMessage 配对。
//
// 当用户中断导致 tool_call 没有对应的 ToolMessage 时，该函数会：
//  1. 弹出所有消息
//  2. 重新配对 tool_call 和 ToolMessage
//  3. 对缺失的 ToolMessage 生成占位消息
//  4. 将修复后的消息回写
//
// 对齐 Python: ContextProcessorRail.fix_incomplete_tool_context(ctx)
// 调用时机：BeforeInvoke / OnModelException
func FixIncompleteToolContext(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("event_type", "fix_incomplete_tool_context_error").
				Str("error", fmt.Sprintf("%v", r)).
				Msg("修复不完整工具上下文时发生异常")
		}
	}()

	mc := cbc.ModelContext()
	if mc == nil {
		return
	}

	// 获取消息数量
	msgCount := mc.Len()
	if msgCount == 0 {
		return
	}

	// 弹出所有消息
	popped := mc.PopMessages(msgCount, false)
	if len(popped) == 0 {
		return
	}

	// toolMessageCache: toolCallID → *ToolMessage（暂存不匹配的 ToolMessage）
	toolMessageCache := make(map[string]*llmschema.ToolMessage)

	// toolIDCache: 有待配对 ToolMessage 的 tool_call 列表
	type toolIDEntry struct {
		ToolCallID string
		ToolName   string
	}
	var toolIDCache []toolIDEntry

	// flushPendingTools: 对缓存中未配对的 tool_call 生成占位 ToolMessage
	flushPendingTools := func() {
		// 先回写缓存的 ToolMessage
		for _, toolMsg := range toolMessageCache {
			_, _ = mc.AddMessages(ctx, toolMsg)
		}
		toolMessageCache = make(map[string]*llmschema.ToolMessage)

		// 为未配对的 tool_call 生成占位消息
		for _, tc := range toolIDCache {
			placeholderMsg := llmschema.NewToolMessage(
				tc.ToolCallID,
				fmt.Sprintf("[Tool execution interrupted] Tool %s was interrupted by user during execution, no result available.", tc.ToolName),
			)
			_, _ = mc.AddMessages(ctx, placeholderMsg)
		}
		toolIDCache = nil
	}

	// 遍历弹出的消息，重新配对
	for _, msg := range popped {
		switch m := msg.(type) {
		case *llmschema.AssistantMessage:
			// 遇到 AssistantMessage 时，先 flush 之前的 pending tools
			if len(toolIDCache) > 0 {
				logger.Info(logger.ComponentAgentCore).
					Str("event_type", "fix_incomplete_tool_context").
					Msg("Fixed incomplete tool context with placeholder messages")
				flushPendingTools()
			}
			// 回写 AssistantMessage
			_, _ = mc.AddMessages(ctx, m)
			// 入队其 tool_calls
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					// 确保参数是合法 JSON
					tc.Arguments = EnsureJSONArguments(tc.Arguments)
					toolIDCache = append(toolIDCache, toolIDEntry{
						ToolCallID: tc.ID,
						ToolName:   tc.Name,
					})
				}
			}

		case *llmschema.ToolMessage:
			if len(toolIDCache) == 0 {
				// 没有待配对的 tool_call，直接回写
				_, _ = mc.AddMessages(ctx, m)
			} else if m.ToolCallID == toolIDCache[0].ToolCallID {
				// 匹配队列头部的 tool_call
				_, _ = mc.AddMessages(ctx, m)
				toolIDCache = toolIDCache[1:]
			} else {
				// 不匹配，暂存到 cache
				toolMessageCache[m.ToolCallID] = m
			}

		default:
			// 其他消息类型（UserMessage 等），先 flush pending
			if len(toolIDCache) > 0 {
				logger.Info(logger.ComponentAgentCore).
					Str("event_type", "fix_incomplete_tool_context").
					Msg("Fixed incomplete tool context with placeholder messages")
				flushPendingTools()
			}
			_, _ = mc.AddMessages(ctx, m)
		}
	}

	// 处理末尾残留的 pending tools
	if len(toolIDCache) > 0 {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "fix_incomplete_tool_context").
			Msg("Fixed incomplete tool context with placeholder messages")
		flushPendingTools()
	}
}

// EnsureJSONArguments 确保 tool call arguments 是合法 JSON 字符串。
//
// 对齐 Python: ContextProcessorRail._ensure_json_arguments(arguments)
func EnsureJSONArguments(arguments string) string {
	if arguments == "" {
		return "{}"
	}

	// 尝试解析为 JSON
	var parsed any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "illegal_tool_call_arguments").
			Str("arguments", arguments).
			Msg("Illegal Tool call arguments")
		return "{}"
	}

	// 只接受 dict/object 类型
	if _, ok := parsed.(map[string]any); ok {
		return arguments
	}

	logger.Warn(logger.ComponentAgentCore).
		Str("event_type", "illegal_tool_call_arguments").
		Str("arguments", arguments).
		Msg("Illegal Tool call arguments")
	return "{}"
}
