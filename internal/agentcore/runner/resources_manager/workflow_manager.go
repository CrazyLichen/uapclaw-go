package resources_manager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer/decorator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowMgr 工作流资源管理器，嵌入 AbstractManager 复用 provider 注册/获取/注销能力。
// GetWorkflow 支持可选的 tracer 装饰：当 session 非 nil 时，返回装饰后的工作流实例。
//
// 对应 Python: WorkflowMgr (openjiuwen/core/runner/resources_manager/workflow_manager.py)
type WorkflowMgr struct {
	AbstractManager[interfaces.Workflow]
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowMgr 创建工作流资源管理器。
func NewWorkflowMgr() WorkflowMgr {
	return WorkflowMgr{
		AbstractManager: NewAbstractManager[interfaces.Workflow](),
	}
}

// AddWorkflow 注册工作流提供者。
//
// 对应 Python: WorkflowMgr.add_workflow(workflow_id, provider)
func (m *WorkflowMgr) AddWorkflow(workflowID string, provider WorkflowProvider) error {
	if workflowID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "workflow"),
			exception.WithParam("reason", "workflow id is empty"),
		)
	}
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", "workflow"),
			exception.WithParam("reason", "workflow provider is nil"),
		)
	}

	// 将 WorkflowProvider 包装为 AbstractManager 所需的 func(context.Context) (T, error) 签名
	wrappedProvider := func(ctx context.Context) (interfaces.Workflow, error) {
		return provider(ctx, nil)
	}

	err := m.RegisterProvider(workflowID, wrappedProvider)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "WORKFLOW_ADD_ERROR").
			Str("workflow_id", workflowID).
			Err(err).
			Msg("添加工作流失败")
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("card", workflowID),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_ADD_SUCCESS").
		Str("workflow_id", workflowID).
		Msg("添加工作流成功")
	return nil
}

// AddWorkflows 批量注册工作流提供者。
//
// 对应 Python: WorkflowMgr.add_workflows(workflows)
func (m *WorkflowMgr) AddWorkflows(workflows []WorkflowEntry) {
	for _, entry := range workflows {
		if entry.ID == "" || entry.Provider == nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "WORKFLOW_ADD_ERROR").
				Str("workflow_id", entry.ID).
				Msg("批量添加工作流跳过无效条目")
			continue
		}
		if err := m.AddWorkflow(entry.ID, entry.Provider); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "WORKFLOW_ADD_ERROR").
				Str("workflow_id", entry.ID).
				Err(err).
				Msg("批量添加工作流失败")
		}
	}
}

// RemoveWorkflow 注销工作流提供者，返回被注销的 provider。
//
// 对应 Python: WorkflowMgr.remove_workflow(workflow_id)
func (m *WorkflowMgr) RemoveWorkflow(workflowID string) (WorkflowProvider, error) {
	unwrapped, err := m.UnregisterProvider(workflowID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "WORKFLOW_REMOVE_ERROR").
			Str("workflow_id", workflowID).
			Err(err).
			Msg("移除工作流失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", workflowID),
			exception.WithParam("resource_type", "workflow"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 将 wrapped provider 还原为 WorkflowProvider
	provider := func(ctx context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
		return unwrapped(ctx)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_REMOVE_SUCCESS").
		Str("workflow_id", workflowID).
		Msg("移除工作流成功")
	return provider, nil
}

// GetWorkflow 获取工作流实例。
// 先调用 GetResource 获取工作流，如果 session 非 nil 则调用 decorator.DecorateWorkflowWithTrace 进行追踪装饰。
//
// 对应 Python: WorkflowMgr.get_workflow(workflow_id, session)
func (m *WorkflowMgr) GetWorkflow(ctx context.Context, workflowID string, session decorator.TracerSession) (interfaces.Workflow, error) {
	w, err := m.GetResource(ctx, workflowID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "WORKFLOW_GET_ERROR").
			Str("workflow_id", workflowID).
			Err(err).
			Msg("获取工作流失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", workflowID),
			exception.WithParam("resource_type", "workflow"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 如果 session 非 nil，进行追踪装饰
	if session != nil {
		w = decorator.DecorateWorkflowWithTrace(w, session)
	}

	return w, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
