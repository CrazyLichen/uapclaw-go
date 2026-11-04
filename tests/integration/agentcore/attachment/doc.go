//go:build integration

// Package attachment 提供 Prompt Attachment 地基的集成测试。
//
// 覆盖 snapshot/delta/removed 全链路、顺序契约、压缩后基线回退、
// provider 投影矩阵、重启恢复、多会话隔离、跨轮插入。
// 跨 internal/agentcore/{harness,context_engine,session} 三个二级子系统。
//
// 对应 Python：openjiuwen/harness/prompts/prompt_attachment_manager.py
//
// 文件目录：
//
//	attachment/
//	├── doc.go                        # 包文档
//	├── context_helper.go             # SessionFacade mock + 真实 ContextEngine 构造
//	├── attachment_flow_test.go       # IT-1 全链路 + IT-2 顺序契约
//	├── attachment_compact_test.go    # IT-3 压缩回退 + IT-5 重启恢复
//	└── attachment_provider_test.go   # IT-4 投影矩阵 + IT-6 隔离 + IT-7 跨轮插入
package attachment
