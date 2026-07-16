// Package tool_call 提供工具描述参数句柄。
//
// ToolCallOperator 管理工具描述参数，不执行工具调用。
// 参数变更通过 onParameterUpdated 回调推送给消费者。
//
// 文件目录：
//
//	tool_call/
//	├── doc.go                 # 子包文档
//	└── tool_call_operator.go  # ToolCallOperator
//
// 对应 Python 代码：openjiuwen/core/operator/tool_call/
package tool_call
