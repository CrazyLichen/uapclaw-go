# 10.3.9 EvolutionHelpers 实现设计

> 本文档描述 10.3.9 EvolutionHelpers 的完整实现方案，包括：
> - `server/gateway_push/` 推送传输层（对齐 Python `jiuwenswarm/server/gateway_push/`）
> - `server/adapter/evolution/` 纯工具模块（对齐 Python `evolution_helpers.py`）
> - `server/agent_server.go` 单例 + SendPush 高层方法
> - `server/handle_session.go` delivery_context 补齐
> - 消费者对接修正

## 1. 背景

### 1.1 10.3.9 在 Agent 会话中的流程位置

```
用户消息 → Gateway → E2A编码 → ChannelTransport → E2A解码 → AgentServer
                                                              ↓
                                                    AgentWebSocketServer.Dispatch()
                                                              ↓
                                                    Adapter.ProcessMessageStreamImpl()
                                                              ↓
                                                    ┌─── mode=="team"? ───┐
                                                    │                     │
                                               team_helpers         deep 流式处理
                                                    │                     │
                                         process_team_message_stream()    │
                                                    │              watchEvolutionAndPush()
                                         _watch_team_evolution_and_push()
                                                    │
                                         EvolutionHelpers 提供:
                                         ├─ 事件分类（is_approval/is_outcome/is_progress）
                                         ├─ 状态提取（request_id/stage/terminal）
                                         ├─ 推送函数（push_status/push_event/broadcast）
                                         └─ 审批分组（group_evolution_approvals）
```

### 1.2 10.3.7-11 合并行子步骤映射

| 子步骤 | Python 文件 | Go 对应 | 状态 |
|--------|-----------|---------|------|
| **10.3.7 CodeAgentRail** | `code_agent_rail.py` (18KB) | `deep_adapter_rails.go` 占位 | ⤵️ 待实现 |
| **10.3.8 TeamHelpers** | `team_helpers.py` (51.5KB) | `deep_adapter_team.go` 占位 | ⤵️ 待实现 |
| **10.3.9 EvolutionHelpers** | `evolution_helpers.py` (15.6KB) | **本次实现** | 🔄 |
| **10.3.10 RecapPrompts** | `recap_prompts.py` (783B) | `deep_adapter_evolution.go` 已实现 | ✅ |
| **10.3.11 SysOpBuilder** | `sysop_builder.py` (50.5KB) | `deep_adapter_config.go` 已实现 | ✅ |

### 1.3 回填关系（不能丢失的占位标记）

当前 Go 侧有大量占位标记为 `⤵️ 10.6.3-10` 的代码：

1. **`deep_adapter_evolution.go`** — `watchEvolutionAndPush()`、`handleEvolutionApproval()`、`buildSkillEvolutionRail()` 占位
2. **`deep_adapter_team.go`** — `findTeamSkillRail()`、`handleTeamSkillEvolveApproval()` 占位
3. **`deep_adapter_slash.go`** — 6 个 `/evolve` 命令占位
4. Gateway 侧 `evolution.go` — 已完整实现审批状态机（15 个方法），依赖 AgentServer 侧推送 `chat.evolution_status` 事件

---

## 2. 推送链路对齐

### 2.1 Python 推送链路（3 层）

```
1. GatewayPushTransport 协议（gateway_push/transport.py）
   └─ async def send_push(self, msg: dict) → None

2. WebSocketGatewayPushTransport 实现（gateway_push/transport.py）
   └─ send_push() → AgentWebSocketServer.get_instance().send_push(msg)

3. AgentWebSocketServer.send_push()（agent_ws_server.py）
   └─ wire = build_server_push_wire(msg) → ws.send(json.dumps(wire))
```

### 2.2 Go 推送链路当前状态（2 层，缺少第 1 层）

```
1. ❌ 缺少：GatewayPushTransport 协议 + 实现

2. AgentServer.sendToGateway(data []byte)（agent_server.go）
   └─ ChannelTransport.RecvCh() <- data 或 transport.Send(data)

3. transport.BuildServerPushWire(msg)（wire.go）✅ 已有，但无业务调用方
```

### 2.3 补齐后的 Go 推送链路（对齐 Python）

```
1. GatewayPushTransport 接口（server/gateway_push/transport.go）
   └─ SendPush(ctx, msg map[string]any) error

2. ChannelPushTransport 实现（server/gateway_push/transport.go）
   └─ SendPush() → AgentServer.GetInstance().SendPush(ctx, msg)

3. AgentServer.SendPush()（server/agent_server.go）
   └─ wire := BuildServerPushWire(msg) → json.Marshal → sendToGateway(data)
```

---

## 3. 新增文件详细设计

### 3.1 `server/gateway_push/doc.go`

```go
// Package gateway_push 提供 AgentServer → Gateway 的下行推送抽象与实现。
//
// 定义 GatewayPushTransport 接口——所有 server_push 场景的统一推送入口，
// 以及 ChannelPushTransport 进程内实现（通过 AgentServer 单例发送）。
// 将来跨进程模式使用 WebSocketPushTransport（也在本包中实现）。
//
// 所有 server_push 场景（evolution 状态/cron 触发/文件推送/多会话工具等）
// 统一通过 GatewayPushTransport.SendPush 推送，不直接操作底层 Transport。
//
// 文件目录：
//
//	gateway_push/
//	├── doc.go         # 包文档
//	└── transport.go   # GatewayPushTransport 接口 + ChannelPushTransport 实现
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/
package gateway_push
```

### 3.2 `server/gateway_push/transport.go`

#### 接口定义

```go
// GatewayPushTransport AgentServer → Gateway 的推送传输协议。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py (GatewayPushTransport)
// 所有 server_push 场景统一通过此接口推送，不直接操作底层 Transport。
type GatewayPushTransport interface {
    // SendPush 向 Gateway 发送一条 server_push 语义的消息。
    //
    // msg 格式与 Python AgentWebSocketServer.send_push 入参一致：
    //   {request_id, channel_id, session_id, payload, metadata?, response_kind?}
    // 内部自动调 BuildServerPushWire 编码为 E2A wire 格式。
    SendPush(ctx context.Context, msg map[string]any) error
}
```

#### ChannelPushTransport 实现

```go
// ChannelPushTransport 进程内推送实现，通过 AgentServer 单例发送。
//
// 对齐 Python: jiuwenswarm/server/gateway_push/transport.py
// (WebSocketGatewayPushTransport)
//
// Python 通过 AgentWebSocketServer.get_instance().send_push(msg) 推送，
// Go 侧同样通过 AgentServer.GetInstance().SendPush(msg) 推送。
type ChannelPushTransport struct{}

func NewChannelPushTransport() *ChannelPushTransport {
    return &ChannelPushTransport{}
}

func (t *ChannelPushTransport) SendPush(ctx context.Context, msg map[string]any) error {
    server := GetInstance() // AgentServer 包级函数
    if server == nil {
        logger.Warn(logComponent).Msg("ChannelPushTransport: AgentServer 单例未初始化")
        return fmt.Errorf("AgentServer 单例未初始化")
    }
    return server.SendPush(ctx, msg)
}
```

**注意**：`ChannelPushTransport` 引用 `GetInstance()` 是 `server` 包的导出函数。由于 `gateway_push` 是 `server` 的子包，可以直接引用。

### 3.3 `server/adapter/evolution/doc.go`

```go
// Package evolution 提供技能演进（Skill Evolution）事件分类、状态提取和推送的共享辅助工具。
//
// 本包是纯工具模块，不含外部依赖，所有函数均为无状态纯函数或简单数据结构。
// 消费者通过 EvolutionPushContext 注入推送传输和回调函数来使用推送能力。
//
// 核心功能：
//   - 事件分类：将 SDK 内部演进事件分为 approval/outcome/progress/stream 四类
//   - 状态提取：从原始事件中提取 request_id、stage、terminal 等字段
//   - Noop 检测：根据消息内容识别"无演进信号"场景，映射到细粒度 noop 阶段
//   - 推送桥接：通过 EvolutionPushContext 将演进状态推送到 Gateway 侧
//   - 审批分组：按 request_id 聚合同一审批的多个事件
//
// 文件目录：
//
//	evolution/
//	├── doc.go         # 包文档
//	└── helpers.go     # 3 结构体 + 常量/变量 + ~22 导出函数 + 1 非导出函数
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/evolution_helpers.py
package evolution
```

### 3.4 `server/adapter/evolution/helpers.go`

#### 常量

```go
const (
    // TeamEvolutionIdleSleepSec watcher 空闲轮询间隔
    TeamEvolutionIdleSleepSec = 1.0
    // TeamEvolutionEventTimeoutSec 事件超时
    TeamEvolutionEventTimeoutSec = 900.0
    // TeamEvolutionEventTimeoutGraceSec 超时宽限
    TeamEvolutionEventTimeoutGraceSec = 5.0

    // TeamEvolutionStartStage 起始阶段
    TeamEvolutionStartStage = "collecting"
    // TeamEvolutionStartMessage 起始消息
    TeamEvolutionStartMessage = "Running team skill evolution analysis..."
    // TeamEvolutionNoopStage 无演进（通用）
    TeamEvolutionNoopStage = "no_evolution_generated"
    // TeamEvolutionNoopNoSkillStage 无演进（无技能）
    TeamEvolutionNoopNoSkillStage = "no_evolution_no_skill"
    // TeamEvolutionNoopNoSignalStage 无演进（无信号）
    TeamEvolutionNoopNoSignalStage = "no_evolution_no_signal"
    // TeamEvolutionNoopNoRecordsStage 无演进（无记录）
    TeamEvolutionNoopNoRecordsStage = "no_evolution_no_records"
    // TeamEvolutionHiddenStage 隐藏阶段
    TeamEvolutionHiddenStage = "hidden"
)
```

#### 全局变量（集合与映射）

```go
var (
    // TeamEvolutionNoopMarkers 通用 noop 标记
    TeamEvolutionNoopMarkers = []string{
        "no existing skill found",
        "no evolution signals detected",
        "no evolution records generated",
    }
    // TeamEvolutionNoSkillMarkers 无技能标记
    TeamEvolutionNoSkillMarkers = []string{
        "no skill usage",
        "no existing skill",
        "no regular skill could be attributed",
        "no team/swarm skill",
    }
    // TeamEvolutionNoSignalMarkers 无信号标记
    TeamEvolutionNoSignalMarkers = []string{
        "no actionable evolution signals detected",
        "no evolution signals detected",
    }

    // TeamEvolutionNoopStages noop 阶段集合
    TeamEvolutionNoopStages = map[string]struct{}{
        TeamEvolutionNoopStage:          {},
        TeamEvolutionNoopNoSkillStage:   {},
        TeamEvolutionNoopNoSignalStage:  {},
        TeamEvolutionNoopNoRecordsStage: {},
    }
    // TeamEvolutionHiddenTerminalStages 隐藏终结阶段集合
    TeamEvolutionHiddenTerminalStages = map[string]struct{}{
        TeamEvolutionHiddenStage: {},
        "failed":                 {},
        "timed_out":              {},
    }
    // TeamEvolutionVisibleProgressStages 可见进度阶段集合
    TeamEvolutionVisibleProgressStages = map[string]struct{}{
        "generating":          {},
        "approval_required":   {},
        "completed":           {},
        TeamEvolutionNoopStage:          {},
        TeamEvolutionNoopNoSkillStage:   {},
        TeamEvolutionNoopNoSignalStage:  {},
        TeamEvolutionNoopNoRecordsStage: {},
    }

    // sdkProgressStageMap SDK→显示阶段映射
    // 对齐 Python: _SDK_PROGRESS_STAGE_MAP
    sdkProgressStageMap = map[string]string{
        "started":              "detecting",
        "detecting_signals":    "detecting",
        "staging":              "generating",
        "generating_updates":   "generating",
        "approval_required":    "approval_required",
        "auto_approved":        "completed",
        "cancelled":            TeamEvolutionHiddenStage,
        "completed":            "completed",
        "failed":               "failed",
        "timed_out":            "timed_out",
    }

    // sdkProgressTerminalStages SDK 终结阶段集合
    // 对齐 Python: _SDK_PROGRESS_TERMINAL_STAGES
    sdkProgressTerminalStages = map[string]struct{}{
        "auto_approved": {},
        "cancelled":     {},
        "completed":     {},
        "failed":        {},
        "timed_out":     {},
    }
)
```

#### 结构体

```go
// EvolutionPushContext evolution 推送上下文。
// 对齐 Python: EvolutionPushContext
type EvolutionPushContext struct {
    // Transport 推送传输
    Transport gatewaypush.GatewayPushTransport
    // ChannelID 通道标识（可能为空）
    ChannelID string
    // SessionID 会话标识
    SessionID string
}

// EvolutionStatusUpdate evolution 状态更新。
// 对齐 Python: EvolutionStatusUpdate
type EvolutionStatusUpdate struct {
    // RequestID 请求标识
    RequestID string
    // Status 状态
    Status string
    // Stage 阶段
    Stage string
    // Message 消息
    Message string
}

// EvolutionProgressStatus evolution 进度状态。
// 对齐 Python: EvolutionProgressStatus
type EvolutionProgressStatus struct {
    // Stage 阶段
    Stage string
    // Message 消息
    Message string
    // RequestID 请求标识（nil 表示无）
    RequestID *string
    // Terminal 是否终结
    Terminal bool
}

// TerminalProgressItem 终结进度条目。
// 对齐 Python: terminal_progress_from_events 返回的 tuple
type TerminalProgressItem struct {
    // RequestID 请求标识
    RequestID *string
    // Terminal 终结信息
    Terminal map[string]string
}
```

#### 函数类型定义

```go
// BuildPushMessageFunc 构建 server_push 消息的函数类型。
// 对齐 Python: build_server_push_message 回调参数
type BuildPushMessageFunc func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any

// ParseStreamChunkFunc 解析流式 chunk 的函数类型。
// 对齐 Python: parse_stream_chunk 回调参数
type ParseStreamChunkFunc func(evt any) map[string]any

// BroadcastEventFunc 广播事件的函数类型。
// 对齐 Python: broadcast_event 回调参数
type BroadcastEventFunc func(channelID *string, sessionID string, parsed map[string]any)

// WarnMissingRequestIDFunc 缺少 request_id 时的警告回调。
// 对齐 Python: group_evolution_approvals 的 warn_missing_request_id 参数
type WarnMissingRequestIDFunc func(sessionID string)
```

#### 导出函数清单（22 个）

| # | Go 函数 | Python 函数 | 说明 |
|---|---------|------------|------|
| 1 | `EventPayloadDict(evt any) map[string]any` | `event_payload_dict` | 提取事件 payload |
| 2 | `EventType(evt any) string` | `event_type` | 提取事件类型 |
| 3 | `ResolveEvolutionEventTimeoutSec(rail any, opts ...float64) float64` | `resolve_evolution_event_timeout_sec` | 解析超时 |
| 4 | `IsEvolutionApprovalEvent(evt any) bool` | `is_evolution_approval_event` | 判断审批事件（检查 event_type） |
| 5 | `EvolutionEventKind(evt any) string` | `evolution_event_kind` | 判断事件类别（approval/outcome/progress/stream） |
| 6 | `IsEvolutionOutcomeEvent(evt any) bool` | `is_evolution_outcome_event` | 判断结果事件 |
| 7 | `EvolutionOutcomeFromEvent(evt any) map[string]string` | `evolution_outcome_from_event` | 提取结果 |
| 8 | `ExtractEvolutionRequestID(evt any) *string` | `extract_evolution_request_id` | 提取 request_id |
| 9 | `EvolutionProgressStatusFromEvent(evt any) *EvolutionProgressStatus` | `evolution_progress_status_from_event` | 提取进度状态 |
| 10 | `VisibleEvolutionProgressFromEvents(events []any) []EvolutionProgressStatus` | `visible_evolution_progress_from_events` | 过滤可见进度 |
| 11 | `ProgressForRequest(statuses []EvolutionProgressStatus, requestID string) []EvolutionProgressStatus` | `progress_for_request` | 按 request_id 过滤 |
| 12 | `TerminalStage(terminal map[string]string) string` | `terminal_stage` | 提取终结阶段 |
| 13 | `TerminalProgressFromEvents(events []any) []TerminalProgressItem` | `terminal_progress_from_events` | 提取终结进度 |
| 14 | `TeamEvolutionTerminalProgress(evt any) map[string]string` | `team_evolution_terminal_progress` | 判断终结进度 |
| 15 | `BuildEvolutionStatusUpdate(requestID, status, stage string, message ...string) EvolutionStatusUpdate` | `build_evolution_status_update` | 构建状态更新 |
| 16 | `TeamEvolutionEndUpdate(requestID string, terminal map[string]string) EvolutionStatusUpdate` | `team_evolution_end_update` | 构建终结更新 |
| 17 | `GroupEvolutionApprovals(sessionID string, events []any, warnMissing ...WarnMissingRequestIDFunc) (map[string][]any, []string)` | `group_evolution_approvals` | 审批分组 |
| 18 | `MakeTeamEvolutionCycleRequestID(sessionID string, cycleIndex int) string` | `make_team_evolution_cycle_request_id` | 生成 request_id |
| 19 | `PushEvolutionStatus(ctx context.Context, pushCtx *EvolutionPushContext, update EvolutionStatusUpdate, buildMsgFn BuildPushMessageFunc, includePayloadRequestID ...bool) error` | `push_evolution_status` | 推送状态 |
| 20 | `PushEvolutionEvent(ctx context.Context, pushCtx *EvolutionPushContext, requestID string, evt any, buildMsgFn BuildPushMessageFunc) error` | `push_evolution_event` | 推送事件 |
| 21 | `BroadcastEvolutionProgress(ctx context.Context, channelID *string, sessionID string, events []any, parseChunk ParseStreamChunkFunc, broadcastEvent BroadcastEventFunc) error` | `broadcast_evolution_progress` | 广播进度 |
| 22 | `PushEvolutionProgress(ctx context.Context, pushCtx *EvolutionPushContext, requestID string, events []any, parseChunk ParseStreamChunkFunc, buildMsgFn BuildPushMessageFunc) error` | `push_evolution_progress` | 推送进度 |

#### 非导出函数（1 个）

```go
// noopStageFromMessage 从消息内容推断 noop 阶段。
// 对齐 Python: _noop_stage_from_message()
func noopStageFromMessage(messageLower string) *string { ... }
```

---

## 4. 修改文件详细设计

### 4.1 `server/agent_server.go`

#### 新增单例机制

```go
// ──────────────── 全局变量 ────────────────
var (
    // agentServerInstance AgentServer 单例实例
    agentServerInstance *AgentServer
    // agentServerOnce 保证单例只设置一次
    agentServerOnce sync.Once
)

// ──────────────── 导出函数 ────────────────

// GetInstance 返回 AgentServer 单例实例。
// 对齐 Python: AgentWebSocketServer.get_instance()
func GetInstance() *AgentServer { return agentServerInstance }

// ResetInstance 重置单例（仅用于测试）。
// 对齐 Python: AgentWebSocketServer.reset_instance()
func ResetInstance() {
    agentServerInstance = nil
    agentServerOnce = sync.Once{}
}
```

#### 在 `Start()` 中设置单例

在 `run()` 方法开头（`running = true` 之后）添加：

```go
// 设置单例
agentServerOnce.Do(func() {
    agentServerInstance = s
})
```

#### 新增 SendPush 高层方法

```go
// SendPush AgentServer 主动向 Gateway 推送消息（高层方法）。
//
// 对齐 Python: AgentWebSocketServer.send_push(msg)
// 内部流程：BuildServerPushWire(msg) → json.Marshal → sendToGateway(data)
// 这是所有 server_push 场景的统一入口。
func (s *AgentServer) SendPush(ctx context.Context, msg map[string]any) error {
    wire := transport.BuildServerPushWire(msg)
    data, err := json.Marshal(wire)
    if err != nil {
        logger.Error(logComponent).Err(err).Msg("SendPush: wire 编码失败")
        return fmt.Errorf("wire 编码失败: %w", err)
    }
    s.sendToGateway(data)

    responseKind := ""
    if rk, ok := msg["response_kind"].(string); ok {
        responseKind = strings.TrimSpace(rk)
    }
    if responseKind != "" {
        channelID, _ := msg["channel_id"].(string)
        logger.Info(logComponent).
            Str("channel_id", channelID).
            Str("response_kind", responseKind).
            Msg("SendPush response_kind wire 已发送")
    }
    return nil
}
```

### 4.2 `server/handle_session.go` — delivery_context 补齐

#### 新增常量

```go
const (
    // deliveryContextKind 推送类型
    deliveryContextKind = "server_push"
)
```

#### 新增全局变量

```go
var (
    // deliveryContextCache 内存缓存，解决异步写入时读取到陈旧数据的竞态
    deliveryContextCache = make(map[string]map[string]any)
    // deliveryContextMu 保护缓存的读写锁
    deliveryContextMu sync.RWMutex
)
```

#### 新增导出函数

```go
// SetSessionDeliveryContext 刷新 session 级 delivery context，
// 供异步 server_push 恢复路由上下文。
//
// 对齐 Python: set_session_delivery_context()
func SetSessionDeliveryContext(
    sessionID string,
    channelID *string,
    sourceRequestID *string,
    routeMetadata map[string]any,
    deliveryKind ...string,
) map[string]any { ... }

// GetSessionDeliveryContext 读取 session 级 delivery context。
//
// 对齐 Python: get_session_delivery_context()
func GetSessionDeliveryContext(sessionID string) map[string]any { ... }

// BuildServerPushMessage 基于 session delivery context 构造 server_push 消息。
//
// 对齐 Python: build_server_push_message()
// 被 evolution_helpers 和其他推送场景调用。
func BuildServerPushMessage(
    sessionID, requestID string,
    payload map[string]any,
    fallbackChannelID ...string,
) map[string]any {
    deliveryCtx := GetSessionDeliveryContext(sessionID)
    channelID := "default"
    if deliveryCtx != nil {
        if cid, ok := deliveryCtx["channel_id"].(string); ok && cid != "" {
            channelID = cid
        }
    }
    if len(fallbackChannelID) > 0 && fallbackChannelID[0] != "" && channelID == "default" {
        channelID = fallbackChannelID[0]
    }

    message := map[string]any{
        "request_id": requestID,
        "channel_id": channelID,
        "session_id": sessionID,
        "payload":    payload,
    }
    if deliveryCtx != nil {
        if rm, ok := deliveryCtx["route_metadata"].(map[string]any); ok && len(rm) > 0 {
            message["metadata"] = deepCopyMap(rm)
        }
    }
    return message
}
```

### 4.3 `server/adapter/doc.go` — 更新文件目录

新增 `evolution/` 子包条目：

```
//	├── evolution/               # Evolution 事件分类/状态提取/推送辅助（10.3.9）
//	│   ├── doc.go               # 包文档
//	│   └── helpers.go           # 3 结构体 + 常量/变量 + ~22 导出函数
```

---

## 5. 消费者对接

### 5.1 可直接替换

| 消费者 | 当前实现 | 替换为 | 说明 |
|--------|---------|--------|------|
| `deep_adapter_evolution.go` → `isApprovalEvent(requestID)` | 按 requestID 前缀判断 | **删除**，在 chat.answer 处理中直接用 if/else if 分流 | 对齐 Python 按前缀分流逻辑 |

### 5.2 保持 ⤵️ 标记（有外部依赖）

| 消费者 | 依赖 | 说明 |
|--------|------|------|
| `watchEvolutionAndPush()` | SkillEvolutionRail | 依赖 10.6.3-10 |
| `handleEvolutionApproval()` | SkillEvolutionRail | 依赖 10.6.3-10 |
| `buildSkillEvolutionRail()` | SkillEvolutionRail | 依赖 10.6.3-10 |
| `processTeamMessageStream()` | TeamManager | 依赖 10.3.8 |
| `handleTeamSkillEvolveApproval()` | TeamSkillEvolutionRail | 依赖 10.6.3-10 |
| 6 个 `/evolve` 命令 | SkillEvolutionRail | 依赖 10.6.3-10 |

### 5.3 isApprovalEvent 修正详解

**当前 Go 实现（错误）**：
```go
// 在 deep_adapter_evolution.go 中
func (d *DeepAdapter) isApprovalEvent(requestID string) bool {
    return strings.HasPrefix(requestID, "skill_evolve_") ||
        strings.HasPrefix(requestID, "evolve_simplify_") ||
        strings.HasPrefix(requestID, "team_skill_evolve_")
}
```

**Python 中的正确做法**：
- `is_evolution_approval_event(evt)` — 检查事件类型（`event_type == "chat.ask_user_question"`），在 evolution_helpers 包
- `_is_approval_event(evt)` — 死代码，同上
- `_is_evolution_approval_request_id(request_id)` — Gateway 侧检查 requestID 前缀
- AgentServer 侧分流：直接 if/else if 按前缀

**修正方案**：
1. 删除 `DeepAdapter.isApprovalEvent` 方法
2. 在 chat.answer 处理逻辑中，按 requestID 前缀分流（对齐 Python）：
   ```go
   switch {
   case strings.HasPrefix(requestID, "team_skill_evolve_"):
       // → handleTeamSkillEvolveApproval
   case strings.HasPrefix(requestID, "evolve_simplify_"):
       // → handleGovernanceApproval
   case strings.HasPrefix(requestID, "skill_evolve_"):
       // → handleEvolutionApproval
   }
   ```

---

## 6. Python 中 Evolution 相关判断函数层次总结

| 层次 | Python 函数 | 检查对象 | 位置 | Go 对应 |
|------|------------|---------|------|---------|
| **事件类型判断** | `is_evolution_approval_event(evt)` | 事件的 event_type | evolution_helpers 包 | `evolution.IsEvolutionApprovalEvent(evt)` 本次新增 |
| **requestID 前缀（Gateway）** | `_is_evolution_approval_request_id(request_id)` | requestID 前缀 | gateway/message_handler | `IsEvolutionApprovalRequestID` ✅ 已有 |
| **requestID 分流（AgentServer）** | if/elif 直接判断 | requestID 前缀 | interface_deep.py | 删除 isApprovalEvent，改用 if/else if |
| **死代码** | `_is_approval_event(evt)` | 事件类型 | interface_deep.py | 不实现 |

---

## 7. 实现顺序

1. **`server/agent_server.go`** — 新增单例 + SendPush
2. **`server/gateway_push/`** — 新建包，接口 + ChannelPushTransport 实现
3. **`server/handle_session.go`** — 补齐 delivery_context + BuildServerPushMessage
4. **`server/adapter/evolution/`** — 新建包，helpers.go 全部内容
5. **`server/adapter/evolution/helpers_test.go`** — 单元测试
6. **消费者修正** — 删除 isApprovalEvent，chat.answer 分流
7. **doc.go 更新** — agent_server / adapter / gateway_push
8. **IMPLEMENTATION_PLAN.md 更新** — 10.3.9 状态
