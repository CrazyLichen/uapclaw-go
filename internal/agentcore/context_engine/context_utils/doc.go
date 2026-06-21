// Package context_utils 提供上下文引擎的工具辅助函数。
//
// 包含消息类型解析、工具名回溯查找等无状态工具方法，
// 供 context_engine 下各处理器和上下文实例使用。
// 当前仅包含 MicroCompactProcessor 所需的工具名解析函数，
// 后续步骤（5.24-5.31）按需回填其他工具方法。
//
// 文件目录：
//
//	context_utils/
//	├── doc.go           # 包文档
//	├── resolve.go       # 工具名解析函数（ResolveToolNameFromMessage 等）
//
// 对应 Python 代码：openjiuwen/core/context_engine/context/context_utils.py
package context_utils
