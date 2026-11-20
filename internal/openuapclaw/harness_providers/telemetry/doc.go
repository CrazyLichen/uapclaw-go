// Package telemetry 提供进程级回环 OTLP 接收器，供 harness provider 订阅
// CLI 子进程导出的模型请求遥测。
//
// 只通过自身 OpenTelemetry 导出上报模型请求的第三方 CLI（Claude Code），
// 会在启动时把导出端点指向本接收器（env 注入，15.7 options 落地）。每个
// harness 各起一个接收器会让监听 socket（与端口）随运行中的 agent 数线性
// 增长；本包维持进程级单例服务器，把解码后的 span/log 事件扇出给订阅者
// 回调。每个订阅者只认领携带自身 OTELResourceSourceID 资源属性的事件，
// 因此一个 socket 即可服务进程内全部 harness。
//
// 接收器惰性启动：首个订阅者触发启动，并存活至进程结束（或显式 Aclose）。
// 绑定失败将进程级永久禁用原生 span 采集——可观测性是 best-effort，
// 绝不阻塞 harness 启动。
//
// 文件目录：
//
//	telemetry/
//	├── doc.go            # 包文档
//	└── otlp_receiver.go # SharedOtlpReceiver 与解码助手
//
// 对应 Python 代码：openjiuwen/harness_providers/telemetry/
package telemetry
