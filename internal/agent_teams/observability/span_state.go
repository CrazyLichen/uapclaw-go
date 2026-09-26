package observability

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LlmSpanState 单次 LLM 调用的 span 追踪状态。
// Python: LlmSpanState dataclass (span_context.py)
//
// Python 有 context_token 字段用于 otel_context.detach，
// Go 不需要：Go 的 ctx 不可变，无全局状态需恢复。
type LlmSpanState struct {
	// Span 开放的 OTel span
	Span trace.Span
	// StartNs span 开启时的单调时间（纳秒）
	StartNs int64
	// FirstChunkNs 首个 stream chunk 的单调时间（0 表示未到达）
	FirstChunkNs int64
	// ChunkCount stream chunk 计数
	ChunkCount int
}

// NextChunkSeq 递增并返回下一个 chunk 序号。
// Python: LlmSpanState.next_chunk_seq()
func (s *LlmSpanState) NextChunkSeq() int {
	s.ChunkCount++
	return s.ChunkCount
}

// OtelSpanState 每-DeepAgent 的可变 span 追踪状态容器。
// Python: _llm_span_stack + _tool_span_map + _agent_span_map (3 个 ContextVar)
//
// 通过 context.Value 传播 *OtelSpanState 指针（同 SessionState 模式）：
//   - 同一 DeepAgent 内的 goroutine 共享同一 OtelSpanState 引用
//   - 子 DeepAgent 创建新实例 + WithSpanState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.Mutex 保护。
type OtelSpanState struct {
	mu           sync.Mutex
	llmStack     []*LlmSpanState
	toolSpanMap  map[string][]trace.Span // key=toolName
	agentSpanMap map[string][]trace.Span // key=agentID
}

// spanStateKeyType OtelSpanState 的 context key 类型。
type spanStateKeyType struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSpanState 创建新的 OtelSpanState 实例。
// Python: _llm_span_stack = ContextVar(default=[]) 等 3 个
func InitSpanState() *OtelSpanState {
	return &OtelSpanState{
		toolSpanMap:  make(map[string][]trace.Span),
		agentSpanMap: make(map[string][]trace.Span),
	}
}

// WithSpanState 将 OtelSpanState 注入 context。
// Python: contextvars 自动传播；Go 通过 context.Value 传播指针
func WithSpanState(ctx context.Context, state *OtelSpanState) context.Context {
	return context.WithValue(ctx, spanStateKeyType{}, state)
}

// SpanStateFromCtx 从 context 中获取 OtelSpanState。
// 返回 nil 表示当前 context 未绑定 SpanState（可观测性未启用）。
func SpanStateFromCtx(ctx context.Context) *OtelSpanState {
	if s, ok := ctx.Value(spanStateKeyType{}).(*OtelSpanState); ok {
		return s
	}
	return nil
}

// PushLlmSpanState 将 LLM span 状态压入栈。
// Python: push_llm_span_state(state)
func (s *OtelSpanState) PushLlmSpanState(state *LlmSpanState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmStack = append(s.llmStack, state)
}

// PopLlmSpanState 弹出（或窥视）栈顶 LLM span 状态。
// Python: pop_llm_span_state(peek=False)
//
// peek=true 时返回栈顶但不移除，用于 stream chunk 回调。
func (s *OtelSpanState) PopLlmSpanState(peek bool) *LlmSpanState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.llmStack) == 0 {
		return nil
	}
	if peek {
		return s.llmStack[len(s.llmStack)-1]
	}
	top := s.llmStack[len(s.llmStack)-1]
	s.llmStack = s.llmStack[:len(s.llmStack)-1]
	return top
}

// CurrentLLMSpanContext 返回当前栈顶 LLM span 的 SpanContext。
// 用于创建子 span 时显式注入 parent（对齐 Python 的 otel_context.attach 效果）。
// 返回空 SpanContext 表示无当前 LLM span（子 span 将成为根 span）。
func (s *OtelSpanState) CurrentLLMSpanContext() trace.SpanContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.llmStack) == 0 {
		return trace.SpanContext{}
	}
	return s.llmStack[len(s.llmStack)-1].Span.SpanContext()
}

// PushToolSpan 将 tool span 压入 map。
// Python: push_tool_span(tool_name, span)
func (s *OtelSpanState) PushToolSpan(toolName string, span trace.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolSpanMap[toolName] = append(s.toolSpanMap[toolName], span)
}

// PopToolSpan 弹出指定 toolName 最近的一个 tool span。
// Python: pop_tool_span(tool_name)
func (s *OtelSpanState) PopToolSpan(toolName string) trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.toolSpanMap[toolName]
	if len(bucket) == 0 {
		return nil
	}
	span := bucket[len(bucket)-1]
	if len(bucket) == 1 {
		delete(s.toolSpanMap, toolName)
	} else {
		s.toolSpanMap[toolName] = bucket[:len(bucket)-1]
	}
	return span
}

// PushAgentSpan 将 agent span 压入 map。
// Python: push_agent_span(agent_id, span)
func (s *OtelSpanState) PushAgentSpan(agentID string, span trace.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSpanMap[agentID] = append(s.agentSpanMap[agentID], span)
}

// PopAgentSpan 弹出指定 agentID 最近的一个 agent span。
// Python: pop_agent_span(agent_id)
func (s *OtelSpanState) PopAgentSpan(agentID string) trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.agentSpanMap[agentID]
	if len(bucket) == 0 {
		return nil
	}
	span := bucket[len(bucket)-1]
	if len(bucket) == 1 {
		delete(s.agentSpanMap, agentID)
	} else {
		s.agentSpanMap[agentID] = bucket[:len(bucket)-1]
	}
	return span
}

// ResetAll 重置所有 span 追踪器。测试用。
// Python: reset_all()
func (s *OtelSpanState) ResetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmStack = nil
	s.toolSpanMap = make(map[string][]trace.Span)
	s.agentSpanMap = make(map[string][]trace.Span)
}

// WithSpanStateAndTimeout 创建带 SpanState 和超时的 context（辅助方法）。
func WithSpanStateAndTimeout(ctx context.Context, state *OtelSpanState, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = WithSpanState(ctx, state)
	return context.WithTimeout(ctx, timeout)
}
