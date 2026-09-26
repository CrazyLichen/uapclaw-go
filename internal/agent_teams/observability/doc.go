// Package observability 提供团队可观测性基础设施，集成 OpenTelemetry。
//
// 三层架构：
//   - Callback 层：OtelCallbackHandler 注册到 CallbackFramework，拦截 LLM/Tool/Agent 事件
//   - Monitor 层：OtelTeamMonitorHandler 消费 TeamAgent EventMessage 事件流
//   - Rail 层：ObservabilityRail 覆盖 DeepAgent task_iteration 边界
//
// SpanState 通过 context.Value 传播（同 SessionState 模式），
// 每个 DeepAgent 有独立的 OtelSpanState 实例，天然并发隔离。
//
// 文件目录：
//
//	observability/
//	├── doc.go               # 包文档
//	├── config.go            # ObservabilityConfig 配置结构体
//	├── semconv.go           # 语义约定常量
//	├── redaction.go         # Prompt/Completion 脱敏
//	├── span_state.go        # OtelSpanState + ctx 注入
//	├── callback_handler.go  # OtelCallbackHandler
//	├── monitor_handler.go   # OtelTeamMonitorHandler
//	├── rail.go              # ObservabilityRail
//	└── setup.go             # 生命周期管理 + EventListenerRegistrar 接口
//
// 对应 Python 代码：openjiuwen/agent_teams/observability/
package observability
