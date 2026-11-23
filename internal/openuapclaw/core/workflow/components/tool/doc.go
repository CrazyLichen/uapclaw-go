// Package tool 工具调用组件：把 foundation/tool 的 Tool 调用包装为工作流 DAG
// 原子节点——编排者在 WorkflowSpec 里为节点配 tool_id，图执行到该节点时按 id
// 从资源管理器取工具、校验入参、调用并把结果整形为 {errCode, errMessage, data}
// 信封交给下游节点（错误信封化不断图）。
//
// 文件目录：
//
//	tool/
//	├── doc.go           # 包文档
//	└── tool_comp.go    # ToolComponentConfig/ToolExecutable/ToolComponent
//
// 对应 Python 代码：openjiuwen/core/workflow/components/tool/tool_comp.py
package tool
