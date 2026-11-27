//go:build integration

// Package personalcontext_test 提供 personal_context Host 空态集成测试（24.3）。
//
// 链路：E2A 信封 → AgentServer.handleMessage 分发 → personal_context ws_handler →
// HostAPI → personal_context.yaml 落盘；覆盖 26 method 代表性集合、流式四事件
// 帧序与 best-effort 生命周期（对齐 Python tests/unit_tests/agentserver/
// test_personal_context_websocket_api.py 空态子集）。
//
// 文件目录：
//
//	personal_context/
//	├── doc.go                    # 包文档
//	└── personal_context_test.go  # 分发面与空态行为用例
package personalcontext_test
