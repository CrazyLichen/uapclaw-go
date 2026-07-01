package resources_manager

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResourceRegistry 资源注册表，聚合 7 个子管理器，提供统一资源管理入口。
//
// 对应 Python: ResourceRegistry (openjiuwen/core/runner/resources_manager/resource_registry.py)
type ResourceRegistry struct {
	// toolMgr 工具管理器
	toolMgr *ToolMgr
	// workflowMgr 工作流管理器
	workflowMgr *WorkflowMgr
	// promptMgr Prompt 管理器
	promptMgr *PromptMgr
	// modelMgr 模型管理器
	modelMgr *ModelMgr
	// agentMgr Agent 管理器
	agentMgr *AgentMgr
	// agentTeamMgr Agent 团队管理器
	agentTeamMgr *AgentTeamMgr
	// sysOperationMgr 系统操作管理器
	sysOperationMgr *SysOperationMgr
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewResourceRegistry 创建资源注册表，初始化所有子管理器。
//
// 对应 Python: ResourceRegistry.__init__()
func NewResourceRegistry() *ResourceRegistry {
	registry := &ResourceRegistry{
		toolMgr:         NewToolMgr(),
		workflowMgr:     newWorkflowMgrPtr(),
		promptMgr:       NewPromptMgr(),
		modelMgr:        newModelMgrPtr(),
		agentMgr:        newAgentMgrPtr(),
		agentTeamMgr:    NewAgentTeamMgr(),
		sysOperationMgr: NewSysOperationMgr(),
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RESOURCE_REGISTRY_INIT").
		Msg("资源注册表初始化完成")

	return registry
}

// Tool 返回工具管理器。
func (r *ResourceRegistry) Tool() *ToolMgr {
	return r.toolMgr
}

// Prompt 返回 Prompt 管理器。
func (r *ResourceRegistry) Prompt() *PromptMgr {
	return r.promptMgr
}

// Model 返回模型管理器。
func (r *ResourceRegistry) Model() *ModelMgr {
	return r.modelMgr
}

// Workflow 返回工作流管理器。
func (r *ResourceRegistry) Workflow() *WorkflowMgr {
	return r.workflowMgr
}

// Agent 返回 Agent 管理器。
func (r *ResourceRegistry) Agent() *AgentMgr {
	return r.agentMgr
}

// AgentTeam 返回 Agent 团队管理器。
func (r *ResourceRegistry) AgentTeam() *AgentTeamMgr {
	return r.agentTeamMgr
}

// SysOperation 返回系统操作管理器。
func (r *ResourceRegistry) SysOperation() *SysOperationMgr {
	return r.sysOperationMgr
}

// RemoveByID 按资源 ID 依次在各子管理器中尝试移除，成功则返回。
// 移除顺序与 Python remove_by_id 一致：Tool → Workflow → Agent → AgentTeam → Prompt → Model → SysOperation。
//
// 对应 Python: ResourceRegistry.remove_by_id(resource_id)
func (r *ResourceRegistry) RemoveByID(resourceID string) {
	// 1. 尝试工具
	if _, err := r.toolMgr.RemoveTool(resourceID); err == nil {
		return
	}

	// 2. 尝试工作流
	if _, err := r.workflowMgr.RemoveWorkflow(resourceID); err == nil {
		return
	}

	// 3. 尝试 Agent
	if _, err := r.agentMgr.RemoveAgent(resourceID); err == nil {
		return
	}

	// 4. 尝试 Agent 团队（⤵️ 预留）
	if _, err := r.agentTeamMgr.RemoveAgentTeam(resourceID); err == nil {
		return
	}

	// 5. 尝试 Prompt
	if _, err := r.promptMgr.RemovePrompt(resourceID); err == nil {
		return
	}

	// 6. 尝试模型
	if _, err := r.modelMgr.RemoveModel(resourceID); err == nil {
		return
	}

	// 7. 尝试系统操作（⤵️ 预留）
	if _, err := r.sysOperationMgr.RemoveSysOperation(resourceID); err == nil {
		return
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newWorkflowMgrPtr 创建 WorkflowMgr 并返回指针。
func newWorkflowMgrPtr() *WorkflowMgr {
	m := NewWorkflowMgr()
	return &m
}

// newModelMgrPtr 创建 ModelMgr 并返回指针。
func newModelMgrPtr() *ModelMgr {
	m := NewModelMgr()
	return &m
}

// newAgentMgrPtr 创建 AgentMgr 并返回指针。
func newAgentMgrPtr() *AgentMgr {
	m := NewAgentMgr()
	return &m
}
