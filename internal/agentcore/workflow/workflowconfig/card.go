// Package workflowconfig 工作流配置/卡片叶包（偏差 D-14-2 破环，D-17-8 同款先例）：
// session 侧（config/internal/facade）直引本包，workflow 根以别名锚点保持调用面零改动。
//
// 文件目录：
//
//	workflowconfig/
//	├── doc.go             # 包文档
//	├── card.go            # WorkflowCard 元数据卡片
//	└── workflow_config.go # WorkflowConfig/WorkflowSpec/NodeSpec 族
//
// 对应 Python 代码：openjiuwen/core/workflow/base.py + workflow_config.py
package workflowconfig

import (
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowCard 工作流元数据卡片。
// Python: openjiuwen/core/workflow/base.py WorkflowCard(BaseCard)；
// 继承以嵌入 schema.BaseCard 承载。
// 偏差 D-1：Python input_params 为 dict|Type[BaseModel] Union，Go 用 map[string]any 承载。
type WorkflowCard struct {
	schema.BaseCard
	// Version 工作流版本号（Python 默认 ''）
	Version string `json:"version,omitempty"`
	// InputParams 输入参数定义（JSON Schema map）；Python Optional，nil 语义=缺省
	InputParams map[string]any `json:"input_params,omitempty"`
}

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

// ToolInfo 返回工具信息，InputParams 为 nil 时传空 map。
func (c *WorkflowCard) ToolInfo() schema.ToolInfoInterface {
	params := c.InputParams
	if params == nil {
		params = map[string]any{}
	}
	return schema.NewToolInfo(c.Name, c.Description, params)
}

// AbilityName Ability 接口：名称（对齐 Python ability_name）。
func (c *WorkflowCard) AbilityName() string { return c.Name }

// AbilityID Ability 接口：标识。
func (c *WorkflowCard) AbilityID() string { return c.ID }

// AbilityKind Ability 接口：种类=工作流。
func (c *WorkflowCard) AbilityKind() schema.AbilityKind { return schema.AbilityKindWorkflow }
