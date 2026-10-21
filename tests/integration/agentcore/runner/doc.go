//go:build integration

// Package runner_test 提供 Runner 模块的集成测试。
//
// 对齐 Python: tests/system_tests/runner/test_runner.py
// （Runner 启停、资源注册/注销、MCP 工具生命周期）。
//
// 文件目录：
//
//	runner/
//	├── doc.go               # 包文档
//	├── runner_test.go       # Runner 启停 + 资源注册/注销 + Agent 资源管理生命周期（RunnerLifeCycleSuite）
//	└── mcp_lifecycle_test.go # MCP 工具生命周期全链路（McpLifecycleSuite）
//
// MCP 生命周期测试说明：
// Python 原测试通过 unittest.mock.patch 类级替换 SseClient/StdioClient/PlaywrightClient；
// Go 无类级 patch 机制，改用 ResourceMgr.SetMcpClientFactory 工厂注入 mock 客户端，
// 走真实 AddMcpServer → GetMcpToolIDs → GetMcpToolInfos → GetTool → Invoke → RemoveMcpServer 全路径。
//
// Agent 资源管理测试说明：
// 对齐 Python test_runner_agent_resource_management 的资源管理链路
// （AddAgent → RunAgent(ByAgentID) 会话 PreRun/Invoke/PostRun → RemoveAgent → 再运行失败）；
// 其交互中断恢复部分（Questioner __interaction__）依赖 WorkflowAgent/IntentDetection/Questioner
// （计划 8.14-8.24 未移植），待工作流组件移植后补充。
package runner_test
