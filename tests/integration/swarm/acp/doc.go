//go:build integration

// Package acp 承载 ACP stdio 出站客户端（AcpStdioClient）经 acp_chat 工具链的集成测试
// （测试用例在外部测试包 acp_test 中）。
//
// 包内唯一非测试文件是本 doc.go，故包名取 acp 而非 acp_test：cmd/go 不允许
// 目录内非测试文件使用 `<名字>_test` 形式的包名（会与外部测试包冲突，
// 报 "found packages acp ... and acp_test ..."）。
//
// 链路：config acp_agents profile → acpchat.NewAcpChatTool → tool.Invoke →
// acp.AcpStdioClient（spawn 子进程）→ testdata/fakeacp 假对端 → 聚合文本回落。
// 真实调用横跨 internal/swarm/acp、internal/swarm/agents、internal/agentcore/foundation
// 三个二级子系统（integration-audit 第一层门禁要求 ≥3）。
//
// 文件目录：
//
//	acp/
//	├── doc.go               # 包文档
//	├── acp_chat_e2e_test.go # 出站 suite（TestMain 预编译假对端 + 11 例）
//	└── testdata/fakeacp/    # 假 ACP 对端小程序（四 mode：echo/error/permission/fsread）
//
// 已知限制：acp_chat 的客户端缓存是进程级全局 map，且无导出清理 API；
// 各用例使用唯一 profile 名避免串台，残留 peer 子进程活到测试二进制退出，
// 父进程退出时 stdin 关闭 → peer EOF 自然退出（与 Python 进程级 _clients 缓存同语义）。
//
// 对应 Python 代码：tests/system_tests/test_acp_stdio_client_st.py
package acp
