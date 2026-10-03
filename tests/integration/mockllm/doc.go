//go:build integration

// Package mockllm 提供集成测试专用的 Mock LLM 客户端。
//
// MockModelClient 实现 BaseModelClient 接口，支持：
//   - 预设响应队列（纯文本/工具调用/错误）
//   - 流式响应模拟
//   - 调用历史记录（用于断言）
//
// 对齐 Python tests/unit_tests/fixtures/mock_llm.py:MockLLMModel
//
// 文件目录：
//
//	mockllm/
//	├── doc.go              # 包文档
//	├── mock_client.go      # MockModelClient 实现
//	└── mock_client_test.go # Mock 自身单元测试
package mockllm
