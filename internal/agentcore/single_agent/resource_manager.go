package single_agent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
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
type Workflow interface {
	// Execute 执行工作流
	Execute(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (any, error)
}

// Agent Agent 执行接口（最小定义，领域六扩展）。
type Agent interface {
	// Invoke 调用 Agent
	Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)
}

// ContextEngine 上下文引擎接口（预留，领域五回填）。
type ContextEngine interface {
	// CreateContext 创建上下文
	CreateContext(ctx context.Context, contextID string, session Session) (any, error)
}

// Session 会话接口（预留，领域五回填）。
type Session interface {
	// GetSessionID 获取会话 ID
	GetSessionID() string
	// CreateWorkflowSession 创建工作流子会话 ⤵️ 预留
	CreateWorkflowSession() Session
	// GetState 获取会话状态
	GetState(key string) any
	// UpdateState 更新会话状态
	UpdateState(state map[string]any)
}

// ResourceOptions 实例获取选项。
type ResourceOptions struct {
	// Tag 资源标签
	Tag string
	// Session 会话实例 ⤵️ 预留
	Session Session
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
func WithResourceSession(session Session) ResourceOption {
	return func(o *ResourceOptions) { o.Session = session }
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
