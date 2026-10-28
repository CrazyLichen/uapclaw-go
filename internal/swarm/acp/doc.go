// Package acp 提供外部 ACP 子进程（Codex CLI / Gemini CLI 等）的 stdio JSON-RPC 客户端（10.6.24d）。
//
// AcpStdioClient spawn 子进程，通过 stdin/stdout 交换 JSON-RPC 2.0 消息，
// 支持出站 rpc 调用（initialize/session/new/session/prompt）、入站通知
// （session/update）与对端反向请求（fs 读写、权限请求）。
//
// 文件目录：
//
//	acp/
//	├── doc.go                # 包文档
//	├── stdio_client.go       # AcpStdioClient 主体（连接/聊天/关闭）
//	├── stdio_client_parse.go # stdout 缓冲 JSON 解析（raw_decode 语义）
//	└── subprocess_env.go     # 子进程环境变量构造（profile 隔离敏感键）
//
// 对应 Python 代码：jiuwenswarm/acp/stdio_client.py · subprocess_env.py
package acp
