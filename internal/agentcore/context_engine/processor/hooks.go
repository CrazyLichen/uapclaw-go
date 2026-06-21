package processor

import (
	"context"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ProcessorType 返回处理器类型标识。
//
// 默认返回空字符串，具体处理器应覆写此方法返回自身结构体名。
// 对应 Python: ContextProcessor.processor_type()（由元类自动注入类名）
func (p *BaseProcessor) ProcessorType() string {
	return ""
}

// OnAddMessages 默认透传消息（no-op）。
//
// 仅在 TriggerAddMessages 返回 true 时被调用。
// 默认实现直接返回输入的消息列表，不执行任何变换。
//
// 对应 Python: ContextProcessor.on_add_messages() 默认实现
func (p *BaseProcessor) OnAddMessages(_ context.Context, _ iface.ModelContext, messages []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	return nil, messages, nil
}

// OnGetContextWindow 默认透传上下文窗口（no-op）。
//
// 仅在 TriggerGetContextWindow 返回 true 时被调用。
// 默认实现直接返回输入的上下文窗口，不执行任何变换。
//
// 对应 Python: ContextProcessor.on_get_context_window() 默认实现
func (p *BaseProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, cw, nil
}

// TriggerAddMessages 默认不触发。
//
// 每次消息添加时调用，必须轻量。
// 默认实现始终返回 false，表示此处理器不需要介入。
//
// 对应 Python: ContextProcessor.trigger_add_messages() 默认实现
func (p *BaseProcessor) TriggerAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	return false, nil
}

// TriggerGetContextWindow 默认不触发。
//
// 每次上下文窗口获取时调用，必须轻量。
// 默认实现始终返回 false，表示此处理器不需要介入。
//
// 对应 Python: ContextProcessor.trigger_get_context_window() 默认实现
func (p *BaseProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}

// IsAPIRound 判断消息列表是否构成一个完整的 API 轮次。
//
// 通过调用 GroupCompletedAPIRounds 判断最后一条消息
// 是否恰好落在某个已完成轮次的结束位置。
//
// 对应 Python: ContextProcessor._api_round(messages)
func (p *BaseProcessor) IsAPIRound(messages []llm_schema.BaseMessage) bool {
	rounds := GroupCompletedAPIRounds(messages)
	if len(rounds) == 0 {
		return false
	}
	lastEnd := rounds[len(rounds)-1][1]
	return lastEnd == len(messages)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
