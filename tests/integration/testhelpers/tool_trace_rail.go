package testhelpers

import (
	"context"
	"sync"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolTraceRail 工具调用追踪 Rail。
// 记录 BeforeToolCall 的工具名称序列，对齐 Python ToolTraceRail。
//
// Python: tests/system_tests/harness/test_deep_agent_e2e.py ToolTraceRail
type ToolTraceRail struct {
	agentinterfaces.BaseRail
	// mu 保护并发访问
	mu sync.Mutex
	// toolCalls 工具调用名称序列
	toolCalls []string
	// toolExecCounts 工具名称到执行次数的映射
	toolExecCounts map[string]int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolTraceRail 创建工具调用追踪 Rail。
func NewToolTraceRail() *ToolTraceRail {
	return &ToolTraceRail{
		toolExecCounts: make(map[string]int),
	}
}

// GetCallbacks 覆盖基类回调映射，注册 BeforeToolCall 回调。
// Go struct embedding 不提供虚方法派发，必须显式注册才能被回调框架调用。
func (r *ToolTraceRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackBeforeToolCall, func(ctx context.Context, railCtx any) error {
			return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// BeforeToolCall 记录工具调用。
func (r *ToolTraceRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil || cbc.Inputs() == nil {
		return nil
	}
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || toolInputs == nil {
		return nil
	}
	r.mu.Lock()
	r.toolCalls = append(r.toolCalls, toolInputs.ToolName)
	r.toolExecCounts[toolInputs.ToolName]++
	r.mu.Unlock()
	return nil
}

// GetExecutionCount 返回指定工具的执行次数。
// 对齐 Python ToolTraceRail.get_execution_count(tool_name)。
func (r *ToolTraceRail) GetExecutionCount(toolName string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolExecCounts[toolName]
}

// GetCallSequence 返回工具调用名称序列。
func (r *ToolTraceRail) GetCallSequence() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]string, len(r.toolCalls))
	copy(result, r.toolCalls)
	return result
}

// TotalCalls 返回总工具调用次数。
func (r *ToolTraceRail) TotalCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.toolCalls)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
