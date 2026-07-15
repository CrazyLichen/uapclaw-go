package interfaces

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// Workflow 工作流执行接口（最小定义，领域八扩展）。
//
// 对应 Python: openjiuwen/core/workflow/workflow.py (Workflow)
// Python 的 Workflow 有 invoke/stream/card 三个能力，
// Go 当前定义 Invoke/Stream/Card 三个方法，对齐 Python。
// Invoke 返回值暂用 (any, error)，领域八扩展为 (*WorkflowOutput, error)。
type Workflow interface {
	// Invoke 非流式调用工作流
	//
	// 对应 Python: Workflow.invoke(inputs, session, context, **kwargs) -> WorkflowOutput
	Invoke(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (any, error)
	// Stream 流式调用工作流
	//
	// 对应 Python: Workflow.stream(inputs, session, context, stream_modes, **kwargs) -> AsyncIterator[WorkflowChunk]
	// 返回 channel 中的 stream.Schema 对应 Python 的 WorkflowChunk = Union[OutputSchema, CustomSchema, TraceSchema]。
	Stream(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (<-chan stream.Schema, error)
	// Card 返回工作流配置卡片
	//
	// 对应 Python: Workflow.card 属性（@property）
	// 用于 tracer 装饰器提取 instanceInfo.metadata（id/name/description/version）。
	Card() *schema.WorkflowCard
}

// WorkflowOptions 工作流执行选项。
type WorkflowOptions struct {
	// Session 工作流会话（对齐 Python workflow.invoke(inputs, session=...)）
	Session *session.WorkflowSession
	// Context 模型上下文，待领域八具体化（对齐 Python workflow.invoke(inputs, context=...)）
	Context any
}

// WorkflowOption 工作流执行选项函数（预留，领域八扩展）。
type WorkflowOption func(*WorkflowOptions)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithWorkflowSession 设置工作流会话。
func WithWorkflowSession(sess *session.WorkflowSession) WorkflowOption {
	return func(o *WorkflowOptions) { o.Session = sess }
}

// WithWorkflowContext 设置模型上下文。
func WithWorkflowContext(ctx any) WorkflowOption {
	return func(o *WorkflowOptions) { o.Context = ctx }
}

// NewWorkflowOptions 从选项列表构建 WorkflowOptions。
func NewWorkflowOptions(opts ...WorkflowOption) *WorkflowOptions {
	o := &WorkflowOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ──────────────────────────── 非导出函数 ────────────────────────────
