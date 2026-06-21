// Package offloader 提供上下文引擎的消息卸载处理器实现。
//
// 卸载处理器在对话消息数或 Token 数超过阈值时，将大消息的内容裁剪
// 并卸载到文件系统或内存，生成轻量占位符。原始内容可通过 reloader_tool
// 按 offload_handle 取回。
//
// 当前实现：
//   - MessageOffloader：基础裁剪卸载，将大消息截断为 trim_size + 省略标记
//
// 后续实现（5.28/5.29）将继承 MessageOffloader：
//   - MessageSummaryOffloader：用 LLM 生成摘要替代简单裁剪
//   - ToolResultBudgetProcessor：按轮次控制工具结果 Token 预算
//
// 文件目录：
//
//	offloader/
//	├── doc.go                     # 包文档
//	├── message_offloader.go       # MessageOffloader + Config
//	└── message_offloader_test.go  # 单元测试
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/offloader/
package offloader
