// Package token 提供上下文引擎的 Token 计数能力。
//
// 定义 TokenCounter 抽象接口，供 ModelContext 统计消息和工具定义的 Token 数量。
// 具体实现（如 TiktokenCounter）在后续步骤中提供。
//
// 文件目录：
//
//	token/
//	├── doc.go     # 包文档
//	└── base.go    # TokenCounter 接口定义
//
// 对应 Python 代码：openjiuwen/core/context_engine/token/
package token
