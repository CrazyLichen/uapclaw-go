package runner

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRef Agent引用，支持按ID查找或直接传入实例。
// 对齐 Python: agent: str | BaseAgent | LegacyBaseAgent
type AgentRef struct {
	// id Agent ID（按ID查找时设置）
	id string
	// agent Agent实例（按实例传入时设置）
	agent interfaces.BaseAgent
}

// WorkflowRef 工作流引用，支持按ID查找或直接传入实例。
// 对齐 Python: workflow: str | Workflow
type WorkflowRef struct {
	// id 工作流ID（按ID查找时设置）
	id string
	// workflow 工作流实例（按实例传入时设置）
	workflow interfaces.Workflow
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ByAgentID 创建按ID查找的AgentRef。
func ByAgentID(id string) AgentRef {
	return AgentRef{id: id}
}

// ByAgent 创建按实例传入的AgentRef。
func ByAgent(agent interfaces.BaseAgent) AgentRef {
	return AgentRef{agent: agent}
}

// IsByID 判断是否按ID查找。
func (r AgentRef) IsByID() bool {
	return r.id != ""
}

// IsByInstance 判断是否按实例传入。
func (r AgentRef) IsByInstance() bool {
	return r.agent != nil
}

// ID 返回Agent ID。
func (r AgentRef) ID() string {
	return r.id
}

// Agent 返回Agent实例。
func (r AgentRef) Agent() interfaces.BaseAgent {
	return r.agent
}

// ByWorkflowID 创建按ID查找的WorkflowRef。
func ByWorkflowID(id string) WorkflowRef {
	return WorkflowRef{id: id}
}

// ByWorkflow 创建按实例传入的WorkflowRef。
func ByWorkflow(wf interfaces.Workflow) WorkflowRef {
	return WorkflowRef{workflow: wf}
}

// IsByID 判断是否按ID查找。
func (r WorkflowRef) IsByID() bool {
	return r.id != ""
}

// IsByInstance 判断是否按实例传入。
func (r WorkflowRef) IsByInstance() bool {
	return r.workflow != nil
}

// ID 返回工作流ID。
func (r WorkflowRef) ID() string {
	return r.id
}

// Workflow 返回工作流实例。
func (r WorkflowRef) Workflow() interfaces.Workflow {
	return r.workflow
}

// ──────────────────────────── 非导出函数 ────────────────────────────
