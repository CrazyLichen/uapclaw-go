// Package workflow 提供工作流执行的基础类型定义。
// Python: openjiuwen/core/workflow/base.py
//
// 文件目录：
//
//	workflow/
//	├── doc.go                # 包文档
//	├── base.go               # WORKFLOW_DRAWABLE/WorkflowCard/WorkflowChunkType/WorkflowChunk/GenerateWorkflowKey（8.16，一比一对应 base.py；WorkflowExecutionState/WorkflowOutput 已迁叶包 workflowbase，8.26 收窄回填）
//	└── workflow_config.go   # WorkflowSpec/NodeSpec/CompIOConfig/ExceptionConfig（8.12 前置件 D6，8.15 ⤴️ 完整同步）
//
// 对应 Python 代码：openjiuwen/core/workflow/base.py
package workflow
