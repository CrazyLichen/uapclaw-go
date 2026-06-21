// Package token 提供上下文引擎的 Token 计数能力。
//
// 定义 TokenCounter 抽象接口及其 TiktokenCounter 实现，供 ModelContext 统计
// 消息和工具定义的 Token 数量。TiktokenCounter 基于 tiktoken-go/tokenizer
// 纯 Go 库，BPE 字典编译期嵌入，无需运行时下载。
//
// 文件目录：
//
//	token/
//	├── doc.go                 # 包文档
//	├── base.go                # TokenCounter 接口定义
//	└── tiktoken_counter.go    # TiktokenCounter 实现（基于 tiktoken-go/tokenizer）
//
// 对应 Python 代码：openjiuwen/core/context_engine/token/
package token
