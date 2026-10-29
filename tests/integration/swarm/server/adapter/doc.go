//go:build integration

// Package adapter 提供 swarm 装配层（CodeAdapter）集成测试的包骨架。
//
// 实际测试代码在外部测试包 adapter_test（code_adapter_coding_memory_test.go），
// 对齐 Python tests/unit_tests/agentserver/test_code_adapter_acp_chat_tool.py
// 的装配级测试形态（样板：test_project_memory.py::TestCodeModeIntegration），
// 验证 CodeAdapter 把 agentcore 组件装配到一起后的端到端行为：
// CreateInstance（配置加载→rails 装配→DeepAgent 创建）→ ProcessMessageImpl
// （回调触发→工具执行→落盘验证）。
//
// 文件目录：
//
//	adapter/
//	├── doc.go                                  # 包文档（本文件）
//	└── code_adapter_coding_memory_test.go      # CodingMemoryRail 装配级集成测试
//
// 对应 Python 代码：jiuwenswarm/tests/unit_tests/agentserver/test_code_adapter_acp_chat_tool.py
// 运行方式：go test -tags=integration ./tests/integration/swarm/server/adapter/...
package adapter
