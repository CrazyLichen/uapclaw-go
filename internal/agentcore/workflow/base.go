package workflow

import (
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowCard 工作流元数据卡片。
// Python: openjiuwen/core/workflow/base.py WorkflowCard(BaseCard)；
// 继承以嵌入 schema.BaseCard 承载（对应 openjiuwen/core/common/schema/card.py BaseCard）。
// 偏差 D-1：Python input_params 为 dict|Type[BaseModel] Union，Go 用 any 承载。
type WorkflowCard struct {
	schema.BaseCard
	// Version 工作流版本号（Python 默认 ''）
	Version string `json:"version,omitempty"`
	// InputParams 输入参数定义（map[string]any 或其他形态；Python Optional，空值语义=缺省）
	InputParams any `json:"input_params,omitempty"`
}

// WorkflowOutput 工作流执行结果。
// Python: openjiuwen/core/workflow/base.py WorkflowOutput(BaseModel)
type WorkflowOutput struct {
	// Result 输出数据
	Result any
	// State 执行状态
	State WorkflowExecutionState
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowChunkType 工作流输出数据块类型。
// Python: base.py WorkflowChunkType(str, Enum)
type WorkflowChunkType string

const (
	// WorkflowChunkTypeInteraction 用户/Agent 交互流
	WorkflowChunkTypeInteraction WorkflowChunkType = "interaction"
	// WorkflowChunkTypeOutput 最终输出节点流
	WorkflowChunkTypeOutput WorkflowChunkType = "output"
	// WorkflowChunkTypeError 错误信息流
	WorkflowChunkTypeError WorkflowChunkType = "error"
)

// WorkflowExecutionState 工作流执行状态。
// Python: openjiuwen/core/workflow/base.py WorkflowExecutionState(str, Enum)
type WorkflowExecutionState string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// WORKFLOW_DRAWABLE 工作流可绘制标记
	WORKFLOW_DRAWABLE = "WORKFLOW_DRAWABLE"
	// WorkflowExecutionStateCompleted 执行完成
	WorkflowExecutionStateCompleted WorkflowExecutionState = "COMPLETED"
	// WorkflowExecutionStateInputRequired 需要用户输入（中断）
	WorkflowExecutionStateInputRequired WorkflowExecutionState = "INPUT_REQUIRED"
	// WorkflowExecutionStateError 执行出错
	WorkflowExecutionStateError WorkflowExecutionState = "ERROR"
)

// WorkflowChunk 工作流输出数据块类型别名。
// Python: base.py WorkflowChunk = Union[OutputSchema, CustomSchema, TraceSchema]；
// 偏差 D-2：Go 无 Union，先用 any 承载，8.14 Workflow 真实消费时再定稿。
type WorkflowChunk = any

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期验证 WorkflowCard 实现 schema.Ability 与 schema.CardInterface
var (
	_ schema.Ability       = (*WorkflowCard)(nil)
	_ schema.CardInterface = (*WorkflowCard)(nil)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowCard 创建工作流卡片，ID 自动生成 32 位 UUID hex（对齐 BaseCard default_factory）。
func NewWorkflowCard(opts ...schema.CardOption) *WorkflowCard {
	return &WorkflowCard{BaseCard: *schema.NewBaseCard(opts...)}
}

// ToolInfo 返回工具信息，InputParams 为 nil 时传空 map（对齐 Python tool_info 的 `if self.input_params else {}`）。
func (c *WorkflowCard) ToolInfo() schema.ToolInfoInterface {
	params, ok := c.InputParams.(map[string]any)
	if !ok || params == nil {
		params = map[string]any{}
	}
	return schema.NewToolInfo(c.Name, c.Description, params)
}

// AbilityName 返回能力名称。
func (c *WorkflowCard) AbilityName() string { return c.Name }

// AbilityID 返回能力唯一标识。
func (c *WorkflowCard) AbilityID() string { return c.ID }

// AbilityKind 返回能力类型（工作流）。
func (c *WorkflowCard) AbilityKind() schema.AbilityKind { return schema.AbilityKindWorkflow }

// GenerateWorkflowKey 生成工作流缓存键。
// Python: base.py generate_workflow_key → f"{workflow_id}_{workflow_version}"
func GenerateWorkflowKey(workflowID, workflowVersion string) string {
	return workflowID + "_" + workflowVersion
}

// ──────────────────────────── 非导出函数 ────────────────────────────
