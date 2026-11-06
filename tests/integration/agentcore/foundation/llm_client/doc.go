//go:build integration

// Package llm_client 提供 LLM 客户端 C 组对齐（C-A~C-G）的集成测试。
//
// 测试跨三个子系统：
//   - internal/agentcore/foundation/llm（客户端 + 共享 Transport + usage 抽取）
//   - internal/agentcore/foundation/llm/schema（配置与超时语义）
//   - internal/agentcore/context_engine/usage（会话级 KV cache 用量聚合）
//
// 覆盖：
//   - 流式持续输出不被总超时切断（C-01）+ usage 归一字段落到聚合层（C-04）
//   - 编码错误分类不被包装成 MODEL_CALL_FAILED（C-10）
//   - finish_reason 原样保留（C-03）
//
// 文件目录：
//
//	llm_client/
//	├── doc.go                    # 包文档
//	└── llm_client_test.go        # 客户端-usage 聚合跨子系统集成测试
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/ + .../context_engine/usage/
package llm_client
