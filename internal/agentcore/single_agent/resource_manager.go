package single_agent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResourceManager 实例获取接口，AbilityManager 通过此接口获取 Tool/Workflow/Agent 实例。
// 具体实现由领域六/九提供，3.13 阶段使用 NoopResourceManager。
type ResourceManager interface {
	// GetTool 按 ID 获取工具实例
	GetTool(toolID string, opts ...ResourceOption) (tool.Tool, error)
	// GetWorkflow 按 ID 获取工作流实例
	GetWorkflow(workflowID string, opts ...ResourceOption) (Workflow, error)
	// GetAgent 按 ID 获取 Agent 实例
	GetAgent(agentID string, opts ...ResourceOption) (Agent, error)
	// GetMcpToolInfos 获取 MCP 服务器的工具描述列表
	GetMcpToolInfos(serverID string) ([]*schema.ToolInfo, error)
}

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

// Agent Agent 执行接口（最小定义，领域六扩展）。
type Agent interface {
	// Invoke 调用 Agent
	Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)
}


// ResourceOptions 实例获取选项。
type ResourceOptions struct {
	// Tag 资源标签
	Tag string
	// Session 会话实例
	Session context_engine.ContextSession
}

// NoopResourceManager ResourceManager 的空实现，3.13 阶段使用。
// 所有方法返回 NotFound 错误。
type NoopResourceManager struct{}

// WorkflowOptions 工作流执行选项（预留）。
type WorkflowOptions struct{}

// AgentOptions Agent 调用选项（预留）。
type AgentOptions struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ResourceOption 实例获取选项函数。
type ResourceOption func(*ResourceOptions)

// WorkflowOption 工作流执行选项函数（预留，领域八扩展）。
type WorkflowOption func(*WorkflowOptions)

// AgentOption Agent 调用选项函数（预留，领域六扩展）。
type AgentOption func(*AgentOptions)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithResourceTag 设置资源标签。
func WithResourceTag(tag string) ResourceOption {
	return func(o *ResourceOptions) { o.Tag = tag }
}

// WithResourceSession 设置会话实例。
func WithResourceSession(sess context_engine.ContextSession) ResourceOption {
	return func(o *ResourceOptions) { o.Session = sess }
}

// NewResourceOptions 从选项列表构造 ResourceOptions。
func NewResourceOptions(opts ...ResourceOption) *ResourceOptions {
	o := &ResourceOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// GetTool 实现 ResourceManager 接口，返回 NotFound 错误。
func (n *NoopResourceManager) GetTool(toolID string, opts ...ResourceOption) (tool.Tool, error) {
	return nil, exception.BuildError(
		exception.StatusAbilityNotFound,
		exception.WithParam("ability_name", toolID),
		exception.WithMsg("在空资源管理器中未找到工具"),
	)
}

// GetWorkflow 实现 ResourceManager 接口，返回 NotFound 错误。
func (n *NoopResourceManager) GetWorkflow(workflowID string, opts ...ResourceOption) (Workflow, error) {
	return nil, exception.BuildError(
		exception.StatusAbilityNotFound,
		exception.WithParam("ability_name", workflowID),
		exception.WithMsg("在空资源管理器中未找到工作流"),
	)
}

// GetAgent 实现 ResourceManager 接口，返回 NotFound 错误。
func (n *NoopResourceManager) GetAgent(agentID string, opts ...ResourceOption) (Agent, error) {
	return nil, exception.BuildError(
		exception.StatusAbilityNotFound,
		exception.WithParam("ability_name", agentID),
		exception.WithMsg("在空资源管理器中未找到 Agent"),
	)
}

// GetMcpToolInfos 实现 ResourceManager 接口，返回空列表。
func (n *NoopResourceManager) GetMcpToolInfos(serverID string) ([]*schema.ToolInfo, error) {
	return nil, nil
}
