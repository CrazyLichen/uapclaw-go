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
// team 链路已知缺口（2026-11-01 定位，未修）
//
// 已通：leader 建队 → spawn_member 写成员 → send_message 的 _auto_start_members
// 启动成员 → MemberSpawnedEvent → TeamMonitor → team.member / team.message 推送到
// WS 客户端（两个分类事件均已断言）。
//
// 未通：成员→leader 的回信。实测证据：
//   - RunTeamMember 进入后 ag.Invoke 返回空 map（resKeys=0）、err=nil
//   - 成员侧无 LLM 调用、无 MessageEvent 发布（publishMessageEvent 只有 leader 一次）
//   - 即成员自身轮次的 streamQueue 没有产出 chunk
//
// 下一步定位方向：TeamAgent.Invoke 里 coordination.Start 之后的
// streamQueue 为何无 chunk（同类问题在 leader 侧由 TeamHarness.RunStreaming
// 分支 2 的 _prepare_agent_session 修复解决，成员侧是否走了等价路径需核）。
//
// 注意：测试文件名以 t 开头确保排序在 doc.go 之后——go/build 处理目录内
// 首个 _test.go 声明 package X_test 时依赖文件迭代顺序，测试文件在前会
// 触发 xtest 后缀剥离误判（p.Name != pkg 分支）。
//
// 对应 Python 代码：tests/system_tests/test_cli_channel_ws.py + jiuwenswarm/gateway/channel_manager/tui/tui_connect.py
package gateway_test
