// Package workflowconfig 工作流配置/卡片叶包（偏差 D-14-2 破环，D-17-8 同款先例）。
// session 侧（config/internal/facade）直引本包；workflow 根以类型别名+构造薄封装
// 保持 workflow.Xxx 调用面零改动。
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
