// Package workflow 工作流编排层（8.14 Workflow/BaseWorkflow）。
//
// 配置类型本体沉 workflowconfig 叶包（偏差 D-14-2 破环，D-17-8 同款先例）：
// session 侧（config/internal/facade）直接引叶包，本包以类型别名+构造薄封装
// 保持 workflow.Xxx 调用面零改动。
package workflow

import (
	workflowconfig "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/workflow/workflowconfig"
)

// ──────────────────────────── 类型别名锚点 ────────────────────────────

// CompIOConfig 组件 I/O 配置（叶包别名锚点）。
type CompIOConfig = workflowconfig.CompIOConfig

// ExceptionConfig 节点异常处理配置（叶包别名锚点）。
type ExceptionConfig = workflowconfig.ExceptionConfig

// NodeSpec 组件节点规格（叶包别名锚点）。
type NodeSpec = workflowconfig.NodeSpec

// WorkflowSpec 工作流图规格（叶包别名锚点）。
type WorkflowSpec = workflowconfig.WorkflowSpec

// WorkflowConfig 工作流配置（叶包别名锚点）。
type WorkflowConfig = workflowconfig.WorkflowConfig

// WorkflowConfigOption / NodeSpecOption 选项函数（叶包别名锚点）。
type WorkflowConfigOption = workflowconfig.WorkflowConfigOption
type NodeSpecOption = workflowconfig.NodeSpecOption

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExceptionConfig 叶包构造薄封装。
func NewExceptionConfig() *ExceptionConfig { return workflowconfig.NewExceptionConfig() }

// NewNodeSpec 叶包构造薄封装。
func NewNodeSpec(opts ...NodeSpecOption) (*NodeSpec, error) {
	return workflowconfig.NewNodeSpec(opts...)
}

// NewWorkflowSpec 叶包构造薄封装。
func NewWorkflowSpec() *WorkflowSpec { return workflowconfig.NewWorkflowSpec() }

// NewWorkflowConfig 叶包构造薄封装。
func NewWorkflowConfig(card *WorkflowCard, opts ...WorkflowConfigOption) (*WorkflowConfig, error) {
	return workflowconfig.NewWorkflowConfig(card, opts...)
}

// WithWorkflowSpec / WithMaxNestingDepth / WithMaxRetries 选项薄封装。
func WithWorkflowSpec(spec *WorkflowSpec) WorkflowConfigOption {
	return workflowconfig.WithWorkflowSpec(spec)
}
func WithMaxNestingDepth(depth int) WorkflowConfigOption {
	return workflowconfig.WithMaxNestingDepth(depth)
}
func WithMaxRetries(n int) NodeSpecOption { return workflowconfig.WithMaxRetries(n) }

// 叶包内部助手（仅叶包内可见，此处不需要）。
var _ = workflowconfig.NewWorkflowSpec
