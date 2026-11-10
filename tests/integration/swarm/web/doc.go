//go:build integration

// Package web_test 提供 Web 控制台后端契约的集成测试。
//
// 覆盖 27.2 / 27.10：command.goal 五 action 的 RPC 契约快照、goal 事件流
// 顺序与形状、断线重连补发、无人值守目标自动续跑到终态的全链，以及
// 真 LLM 冒烟（integration && llm）。所有用例经 testhelpers.ServerEnv 走
// 完整 E2A 链路（WebChannel → Gateway → AgentServer → runtime → adapter
// → agentcore/harness），不直接调用 handler。
//
// 文件目录：
//
//	web/
//	├── doc.go                            # 包文档（本文件）
//	├── goal_contract_helpers_test.go      # 共用 suite 基类 + 帧断言助手 + 15 字段校验
//	├── goal_rpc_contract_test.go          # G1：8 组 RPC/事件契约对拍
//	├── goal_event_order_test.go           # G2：O1-O5 事件顺序与形状
//	├── goal_reconnect_test.go             # G3：R1-R4 断线/重连/切会话
//	├── goal_unattended_test.go            # G4 MockLLM 无人值守全链
//	└── goal_realllm_test.go               # G4 真 LLM 冒烟（integration && llm）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
// · jiuwenswarm/server/agent_ws_server.py · openjiuwen/harness/goal/
package web_test
