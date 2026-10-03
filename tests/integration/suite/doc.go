//go:build integration

// Package suite 提供集成测试共享的 TestSuite 基类。
//
// 等价于 Python conftest.py 的 fixture 依赖链，通过 Suite 嵌套实现：
//
//	BaseIntegrationSuite → RunnerSuite → SessionSuite → AgentSuite
//
// 每层在 SetupSuite 中初始化自身依赖，在 TearDownSuite 中清理资源，
// 子 Suite 嵌入父 Suite 后自动继承其全部字段和方法。
//
// 文件目录：
//
//	suite/
//	├── doc.go              # 包文档
//	├── base_suite.go       # BaseIntegrationSuite（MockLLM + Context）
//	├── runner_suite.go     # RunnerSuite（+ Runner 启动/停止）
//	├── session_suite.go    # SessionSuite（+ Session 创建/销毁）
//	└── agent_suite.go      # AgentSuite（+ Agent 注册/执行）
package suite
