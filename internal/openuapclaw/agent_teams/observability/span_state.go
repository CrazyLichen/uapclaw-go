package observability

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
	toolSpanMap  map[string][]trace.Span // key=toolName 工具名
	agentSpanMap map[string][]trace.Span // key=agentID Agent 标识
}

// spanStateKeyType OtelSpanState 的 context key 类型。
type spanStateKeyType struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// spanStateRegistry 全局 OtelSpanState 注册表，用于 ShutdownObservability 时批量重置
	// 对齐 Python: reset_all() 重置所有 ContextVars
	spanStateRegistry   = make(map[*OtelSpanState]struct{})
	spanStateRegistryMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NextChunkSeq 递增并返回下一个 chunk 序号。
// Python: LlmSpanState.next_chunk_seq()
func (s *LlmSpanState) NextChunkSeq() int {
	s.ChunkCount++
	return s.ChunkCount
}

// InitSpanState 创建新的 OtelSpanState 实例，并注册到全局注册表。
// Python: _llm_span_stack = ContextVar(default=[]) 等 3 个
func InitSpanState() *OtelSpanState {
	s := &OtelSpanState{
		toolSpanMap:  make(map[string][]trace.Span),
		agentSpanMap: make(map[string][]trace.Span),
	}
	// 注册到全局注册表，供 ShutdownObservability 时 ResetAll
	spanStateRegistryMu.Lock()
	spanStateRegistry[s] = struct{}{}
	spanStateRegistryMu.Unlock()
	return s
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

// SnapshotTools 返回工具 span 栈的快照（toolName → 栈，浅拷贝切片）。
// cascade 安全网遍历用；Python 直接读 ContextVar 的 map（单线程），
// Go 侧经快照避免持锁回调 End。
func (s *OtelSpanState) SnapshotTools() map[string][]trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]trace.Span, len(s.toolSpanMap))
	for name, stack := range s.toolSpanMap {
		out[name] = append([]trace.Span(nil), stack...)
	}
	return out
}

// SetTools 回写某工具名收口后剩余的栈（cascade 安全网用）。
func (s *OtelSpanState) SetTools(toolName string, remaining []trace.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(remaining) == 0 {
		delete(s.toolSpanMap, toolName)
		return
	}
	s.toolSpanMap[toolName] = remaining
}

// AgentSpanIDs 返回全部 agent span 的 SpanID（cascade 的 llm 父比对用）。
// Python: cascade 以 _current_agent_span 单槽判定；Go 无单槽（偏差），
// 全部 agent span 的 id 都参与比对。
func (s *OtelSpanState) AgentSpanIDs() []trace.SpanID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []trace.SpanID
	for _, stack := range s.agentSpanMap {
		for _, span := range stack {
			if span == nil {
				continue
			}
			if sc := span.SpanContext(); sc.IsValid() {
				ids = append(ids, sc.SpanID())
			}
		}
	}
	return ids
}

// AgentTraceID 返回首个 agent span 的 TraceID hex（cascade 的 llm 迹线定位用）。
// Python: tracker 内部 _resolve_root_span().context.trace_id。
func (s *OtelSpanState) AgentTraceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stack := range s.agentSpanMap {
		for _, span := range stack {
			if span == nil {
				continue
			}
			if sc := span.SpanContext(); sc.IsValid() {
				return sc.TraceID().String()
			}
		}
	}
	return ""
}

// CloseAgentSpans 收口全部 recording 的 agent span 并清空（安全网兜底路径）。
// Python: close_current_agent_span 关闭 _current_agent_span 单槽；Go 无单槽
// （偏差），收口 agentSpanMap 内全部 recording span。
func (s *OtelSpanState) CloseAgentSpans() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, stack := range s.agentSpanMap {
		for _, span := range stack {
			if span == nil || !span.IsRecording() {
				continue
			}
			logger.Warn(logComponent).Str("agent_id", id).
				Str("span_id", func() string {
					if sc := span.SpanContext(); sc.IsValid() {
						return sc.SpanID().String()
					}
					return "0"
				}()).
				Msg("otel: close_current_agent_span - closing unclosed agent span")
			span.End()
		}
	}
	s.agentSpanMap = make(map[string][]trace.Span)
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

// ResetAllSpanStates 重置所有已注册的 OtelSpanState 实例。
// 对齐 Python: reset_all() — 在 ShutdownObservability 中调用，确保测试隔离。
func ResetAllSpanStates() {
	spanStateRegistryMu.Lock()
	defer spanStateRegistryMu.Unlock()
	for s := range spanStateRegistry {
		s.ResetAll()
	}
	spanStateRegistry = make(map[*OtelSpanState]struct{})
}

// PeekToolSpan 窥视指定 toolName 栈顶 tool span（不移除）。
// Python: get_current_tool_span（span_context.py:1052）。
func (s *OtelSpanState) PeekToolSpan(toolName string) trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stack := s.toolSpanMap[toolName]; len(stack) > 0 {
		return stack[len(stack)-1]
	}
	return nil
}

// PeekAgentSpan 窥视指定槽位栈顶 agent span（不移除）。
// Python: get_current_agent_span（_current_agent_span ContextVar；Go 以
// "current" 槽位承载，偏差 spec §4）。
func (s *OtelSpanState) PeekAgentSpan(slot string) trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stack := s.agentSpanMap[slot]; len(stack) > 0 {
		return stack[len(stack)-1]
	}
	return nil
}

// PeekAnyToolSpan 窥视全部工具桶里最近压入的 tool span（派发检测用）。
// Python: get_current_tool_span（span_context.py:1052）遍历各桶取最新；
// Go 单遍扫描等价。
func (s *OtelSpanState) PeekAnyToolSpan() trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest trace.Span
	for _, stack := range s.toolSpanMap {
		if len(stack) == 0 {
			continue
		}
		latest = stack[len(stack)-1]
	}
	return latest
}
