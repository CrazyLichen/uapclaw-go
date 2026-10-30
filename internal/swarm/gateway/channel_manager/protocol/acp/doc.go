// Package acp 提供 ACP（Agent Client Protocol）stdio 通道（11.25）。
//
// AcpChannel 是 IDE 进程侧对端：stdin 逐行 JSON（E2A envelope 或 ACP
// JSON-RPC 请求/响应三路由），stdout 逐行 JSON-RPC；作为 WS 客户端连
// GatewayServer /acp 路由（模式一默认经本地回环 ws://127.0.0.1:19001/acp
// 自举，模式二 --gateway-url 指向远端）。双 response_mode（e2a/jsonrpc）、
// per-request ctx、pending RPC 表、stdin EOF 5s 宽限、idle finalize 3s timer
//（timer 槽引用比对防 superseded）。
//
// 与 gateway 包 AcpGatewayBridge 的分工：桥挂在 GatewayServer /acp 路由上
// 服务 WS IDE（网关侧）；本通道是 IDE/CLI 进程侧对端（stdio + WS 客户端）。
// 二者经 WS 互传 E2A wire 帧与裸 JSON-RPC 帧。
//
// 文件目录：
//
//	acp/
//	├── doc.go                   # 包文档
//	├── acp_channel.go           # AcpChannel 本体（三路由入站 + jsonrpc 六方法 + E2A 双模式出站 + WS 客户端族 + pending/finalize 家族 + 测试钩子）
//	├── acp_channel_test.go      # 31 例 Python 映射 + Go 增强（timer 方向/pending 生命周期）
//	└── ws_client_coverage_test.go # WS 族/messageToE2aResponse 五分支/错误分支覆盖率补齐（Go 增强项）
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/protocol/acp/acp_connect.py
// （AcpChannel L663-1738 + WS 客户端族 L1526-1738；常量 L47-52；配置 L55-63；上下文 L65-81）。
package acp
