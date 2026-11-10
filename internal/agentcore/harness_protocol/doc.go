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
//	├── protocol.go      # 4 个协议接口（HarnessProtocol/HarnessModelControl/HarnessProvider/ModelConfigProvider）
//	└── placeholders.go  # 4 个最小形状占位 + 2 个协作方窄接口（⤵️ 15.2/15.3/15.4 回填）
//
// 对应 Python 代码：openjiuwen/harness_protocol/（models/tools/results/state/errors/protocol.py）
//
// 核心类型索引：
//
//	值对象：HarnessCard / HarnessInput / SendReceipt / ModelSelection / ModelEndpoint /
//	         ModelChoice / ModelOption / HarnessContext / ToolDefinition / ToolInvocation /
//	         ToolExecutionResult / McpServerConfig / ContentBlock / TurnMessage /
//	         TurnTermination / MonetaryAmount / TurnUsage / TurnError / TurnResult
//	枚举：  HarnessState / HarnessCapability / HostCapability / DeliveryMode / AbortMode /
//	         ResumePolicy / McpTransport / TurnStatus / MessageRole / TurnTerminationKind /
//	         EventOverflowPolicy / CheckpointReason
//	接口：  HarnessProtocol / HarnessModelControl / HarnessProvider / ModelConfigProvider /
//	         HarnessTelemetry / ToolGateway / HarnessEventCursor / HarnessCheckpointSink /
//	         HarnessHookDispatcher / HarnessInteractionHandler
//	构造：  全部 NewXxx(...) (Xxx, error)，校验顺序与上游 __post_init__ 一致
//
// 最小形状占位（⤵️ 回填后消除标注）：
//
//	HarnessEvent                 ⤵️ 15.2（events.py）
//	EventBufferConfig            ⤵️ 15.2（events.py:98-106，capacity>0 校验）
//	EventOverflowPolicy          ⤵️ 15.2（events.py:87-95）
//	HarnessEventCursor           ⤵️ 15.2（stream.py:9-22，租约语义）
//	HarnessCheckpoint            ⤵️ 15.4（checkpoints.py:28-45，5 必填 + 4MiB 校验）
//	CheckpointReason             ⤵️ 15.4（checkpoints.py:18-26）
//	CheckpointSaveReceipt        ⤵️ 15.4（checkpoints.py:66-79）
//	HarnessCheckpointSink        ⤵️ 15.4（checkpoints.py:79-111，幂等 + CAS）
//	HarnessHookDispatcher        ⤵️ 15.3（hooks.py，回调与决策语义）
//	HarnessInteractionHandler    ⤵️ 15.3（interactions.py，请求-应答语义）
package harness_protocol
