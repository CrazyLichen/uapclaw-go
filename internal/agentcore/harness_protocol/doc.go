// Package harness_protocol 提供第三方 agent harness 的 provider-neutral 协议值对象层。
//
// 本包是「OpenJiuwen 宿主」与「Claude Code / Codex / DSH 等外部 CLI」之间的
// 边界数据契约：所有跨进程交换的数据都在这层做构造期校验与 JSON 冻结，
// 避免坏值进到子进程才暴露。本包不调用模型、不注册工具、不发起 IO。
//
// 依赖方向：本包是被依赖方，禁止 import agent_teams/* 或 agentcore/harness/*。
//
// 文件目录：
//
//	harness_protocol/
//	├── doc.go           # 包文档
//	├── errors.go        # 5 类错误层次（HarnessError 基类 + 4 子类）
//	├── state.go         # HarnessState（5 相位）
//	├── models.go        # 22 条导出面：JSON 值助手 3 + 2 Protocol + 5 枚举 + 8 值对象 + 4 常量
//	├── tools.go         # 工具与 MCP 描述（4 值对象 + McpTransport + ToolGateway）
//	├── results.go       # 轮次终态结果（3 枚举 + 7 值对象）
//	├── events.go        # 观测面事件（11 枚举 + 10 载荷 + HarnessEvent 信封 + EventRetentionOf + EventBufferConfig）
//	├── stream.go        # 单消费者观测游标接口 HarnessEventCursor + ErrEventStreamDone 哨兵
//	├── protocol.go      # 4 个协议接口（HarnessProtocol/HarnessModelControl/HarnessProvider/ModelConfigProvider）
//	└── placeholders.go  # 检查点占位（⤵️ 15.4）+ 2 个协作方窄接口（⤵️ 15.3）
//
// 对应 Python 代码：openjiuwen/harness_protocol/（models/tools/results/state/errors/events/stream/protocol.py）
//
// 核心类型索引：
//
//	值对象：HarnessCard / HarnessInput / SendReceipt / ModelSelection / ModelEndpoint /
//	         ModelChoice / ModelOption / HarnessContext / ToolDefinition / ToolInvocation /
//	         ToolExecutionResult / McpServerConfig / ContentBlock / TurnMessage /
//	         TurnTermination / MonetaryAmount / TurnUsage / TurnError / TurnResult /
//	         EventBufferConfig / OutputEvent / ItemLifecycleEvent / UsageUpdatedEvent /
//	         ModelRequestEvent / StateChangedEvent / TurnLifecycleEvent / HookObservedEvent /
//	         DiagnosticEvent / ProviderEvent / UnknownEvent / HarnessEvent
//	枚举：  HarnessState / HarnessCapability / HostCapability / DeliveryMode / AbortMode /
//	         ResumePolicy / McpTransport / TurnStatus / MessageRole / TurnTerminationKind /
//	         TurnEventKind / OutputKind / OutputChannel / OutputOperation / UsageUpdateMode /
//	         EventRetention / EventOverflowPolicy / ItemEventKind / HookEventPhase /
//	         DiagnosticLevel / ModelRequestStatus / CheckpointReason
//	接口：  HarnessProtocol / HarnessModelControl / HarnessProvider / ModelConfigProvider /
//	         HarnessTelemetry / ToolGateway / HarnessEventPayload / HarnessEventCursor /
//	         HarnessCheckpointSink / HarnessHookDispatcher / HarnessInteractionHandler
//	构造：  全部 NewXxx(...) (Xxx, error)，校验顺序与上游 __post_init__ 一致
//	助手：  FreezeJsonValue / FreezeJsonObject / JsonValueToBuiltin /
//	         EventRetentionOf / IsTerminalTurnEventKind
//
// 未落地的占位（⤵️ 回填后消除标注）：
//
//	HarnessCheckpoint            ⤵️ 15.4（checkpoints.py:28-45，5 必填 + 4MiB 校验）
//	CheckpointReason             ⤵️ 15.4（checkpoints.py:18-26）
//	CheckpointSaveReceipt        ⤵️ 15.4（checkpoints.py:66-79）
//	HarnessCheckpointSink        ⤵️ 15.4（checkpoints.py:79-111，幂等 + CAS）
//	HarnessHookDispatcher        ⤵️ 15.3（hooks.py，回调与决策语义）
//	HarnessInteractionHandler    ⤵️ 15.3（interactions.py，请求-应答语义）
package harness_protocol
