//go:build integration

// Package integration 提供集成测试基础设施和用例。
//
// 集成测试采用 Mock LLM + 真实 Agent 逻辑模式，
// 对齐 Python tests/system_tests/ 的设计理念：
// LLM 调用走 MockModelClient，其余组件（Runner、Session、Tool、Rail、Memory）全部真实执行。
//
// 目录结构：
//
//	integration/
//	├── doc.go               # 包文档
//	├── mockllm/             # 共享 Mock LLM 基础设施
//	├── suite/               # 共享 TestSuite 基类（conftest.py 等价物）
//	├── agentcore/           # agentcore 集成测试（对齐 Python system_tests）
//	├── swarm/               # 平台层集成测试
//	├── external/            # 外部服务真实连接测试
//	└── cmd/                 # CLI 集成测试
//
// 对应 Python 代码：agent-core/tests/system_tests/
// 运行方式：make test-integration
package integration
