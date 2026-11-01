//go:build integration

// Package gateway_test 提供 GatewayServer 多路由链路的集成测试。
//
// 覆盖 CLI 路由（/tui）全链路往返：gorilla WS 客户端 → GatewayServer
// （CLI ForwardMethods + 本地 ack handler + tui 默认 mode 注入）→
// InboundGatewayServer → ChannelManager → MessageHandler → AgentClient →
// ChannelTransport（进程内）→ AgentServer → DeepAdapter → llm.Model →
// ClientRegistry MockModelClient，出站沿 robotMessages → externalChannels
// 回到 WS 客户端。对齐 Python tests/system_tests/test_cli_channel_ws.py。
//
// 文件目录：
//
//	gateway/
//	├── doc.go                       # 包文档
//	├── team_continuous_dialog_test.go # TeamDialogSuite team 模式连续对话 + 团队事件
//	└── tui_cli_route_test.go        # CliRouteFullChainSuite 双 mode 全链路测试
//
// team 链路（2026-11-01 全通）
//
// 首轮：leader build_team（注册 leader 的 team_member 行）→ spawn_member 写成员
// → send_message 触发 _auto_start_members 启动成员 → MemberSpawnedEvent →
// TeamMonitor → team.member / team.message 推送到 WS 客户端。
//
// 回信：成员轮次独立运行（inprocess spawn 用 WithoutCancel 切断调用方取消传播，
// 否则工具 ctx 已 canceled 会让成员 Invoke 立刻返回并清空 streamQueue），
// 成员调 send_message 回 leader → MessageEvent → team.message（成员→leader）。
//
// 并发：首轮未结束时第二条 chat.send 走 Interact → Steer → TeamHarness.Steer
// 投递进任务循环，触发续轮（原为空桩会静默丢弃）。
//
// 注意：测试文件名以 t 开头确保排序在 doc.go 之后——go/build 处理目录内
// 首个 _test.go 声明 package X_test 时依赖文件迭代顺序，测试文件在前会
// 触发 xtest 后缀剥离误判（p.Name != pkg 分支）。
//
// 对应 Python 代码：tests/system_tests/test_cli_channel_ws.py + jiuwenswarm/gateway/channel_manager/tui/tui_connect.py
package gateway_test
