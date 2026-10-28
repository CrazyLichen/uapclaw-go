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
//	├── doc.go                    # 包文档
//	└── tui_cli_route_test.go     # CliRouteFullChainSuite 双 mode 全链路测试
//
// 注意：测试文件名以 t 开头确保排序在 doc.go 之后——go/build 处理目录内
// 首个 _test.go 声明 package X_test 时依赖文件迭代顺序，测试文件在前会
// 触发 xtest 后缀剥离误判（p.Name != pkg 分支）。
//
// 对应 Python 代码：tests/system_tests/test_cli_channel_ws.py + jiuwenswarm/gateway/channel_manager/tui/tui_connect.py
package gateway_test
