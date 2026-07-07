# 10.1.4 Message 模型设计

> 实现计划步骤 10.1.4：Message 模型 — 内部统一消息格式
> 对应 Python：`jiuwenswarm/common/schema/message.py` (Message)

## 1. 流程位置与作用

**在实现计划中的位置**：层 1 Schema 层（10.1），步骤 10.1.4

**依赖关系**：
```
✅ 10.1.1 ReqMethod → ✅ 10.1.2 EventType → ✅ 10.1.3 Mode → ☐ 10.1.4 Message ← 当前
                                                              ↓
                                              10.1.5~10.1.8 → 层2 E2A → ...→ 层6 CLI+Web
```

**核心作用**：Message 是 Gateway 和 Channel 层的**统一内部消息格式**，是整个通信链路的"流通货币"：
- **入站**：Channel（Web/REPL/IM）收到外部请求 → 构造 `type=req` 的 Message → 交给 Gateway
- **内部转换**：Gateway 通过 `gateway_normalize` 将 Message 转为 E2AEnvelope → 发给 AgentServer
- **出站**：AgentServer 返回 AgentResponse → 转回 `type=res/event` 的 Message → 经 Channel 推回外部

## 2. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| params/payload 类型 | `json.RawMessage` | mvp_plan 决策：延迟解析，性能好，不丢字段 |
| type 字段 | 自定义 `MessageType` + 3 个常量 | 与 ReqMethod/EventType/Mode 风格一致 |
| metadata 类型 | `map[string]any` | Gateway 需要频繁读写 enable_memory 等内部字段 |
| timestamp 类型 | `float64` | 与 Python `time.time()` 返回值精度完全一致 |
| 三态布尔（enable_memory 等） | `bool`，零值 false | MVP 简化，不区分"未设置"与"显式 false" |
| enable_streaming 默认值 | 工厂函数默认设 `true` | Python 默认 `enable_streaming=True`，Go bool 零值 `false` 不对齐，工厂函数中显式设置 `EnableStreaming=true` 弥补 |
| 构造辅助 | `NewReqMessage` / `NewResMessage` / `NewEventMessage` | 与 Python 构造模式对齐，减少重复代码 |
| omitempty 策略 | 指针/切片/map 加 omitempty，其他不加 | 兼顾 JSON 紧凑和字段存在性 |
| ID 生成 | UUID v4，32 hex 无连字符 | 与 Python `uuid4().hex` 对齐 |
| 测试要求 | 完整 JSON 往返，覆盖全字段 | 层 1 验证点要求 |
| Validate | 提供 `Validate() error`，仅校验必须提供的字段 | 防御性编程，互斥约束由工厂函数保证 |
| MessageType 归属 | 放 message.go 中 | 与 Python 放在一起对齐，3 个常量不值得独立文件 |
| stream_seq/stream_id | 值类型 `int`/`string`，零值=未设置 | 与 bool 简化策略一致，MVP 避免指针 |

## 3. 文件结构

```
internal/swarm/schema/
├── doc.go              # 更新：添加 message.go 条目
├── req_method.go       # 已有
├── event_type.go       # 已有
├── mode.go             # 已有
├── message.go          # 新增：MessageType 枚举 + Message 结构体 + 工厂函数 + Validate
└── message_test.go     # 新增：完整往返测试 + Validate 测试 + 工厂函数测试
```

## 4. MessageType 枚举

```go
// MessageType 消息方向类型。
//
// 定义 Message 的方向语义：req（请求）、res（响应）、event（事件）。
// 与 Python Literal["req", "res", "event"] 一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (Message.type)
type MessageType string

const (
    // MessageTypeReq 请求消息
    MessageTypeReq MessageType = "req"
    // MessageTypeRes 响应消息
    MessageTypeRes MessageType = "res"
    // MessageTypeEvent 事件消息
    MessageTypeEvent MessageType = "event"
)
```

提供方法：`ParseMessageType`、`IsValidMessageType`、`String`、`GoString`，与 EventType/Mode 风格一致。

## 5. Message 结构体

```go
// Message 统一内部消息结构。
//
// 作为 Gateway/Channel 层的流通货币，承载请求、响应、事件三种方向的消息。
// 字段布局对齐 Python Message dataclass，JSON tag 与 Python 字段名一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (Message)
type Message struct {
    // ─── 必填字段 ───

    // ID 消息唯一标识（UUID v4，32 hex 无连字符）
    ID string `json:"id"`
    // Type 消息方向：req/res/event
    Type MessageType `json:"type"`
    // ChannelID 来源渠道标识
    ChannelID string `json:"channel_id"`
    // SessionID 会话标识（req 中总是非空，res/event 中可能为空）
    SessionID string `json:"session_id"`
    // Params 请求参数（req 用 json.RawMessage 延迟解析，res/event 为空）
    Params json.RawMessage `json:"params"`
    // Timestamp Unix 秒时间戳（含小数精度，对齐 Python time.time()）
    Timestamp float64 `json:"timestamp"`
    // OK 是否成功
    OK bool `json:"ok"`

    // ─── 可选字段（字符串/bool 不加 omitempty，保留零值输出） ───

    // Provider IM 平台名称（仅 IM 渠道 req 使用）
    Provider string `json:"provider"`
    // ChatID IM 聊天标识
    ChatID string `json:"chat_id"`
    // UserID 发送者标识
    UserID string `json:"user_id"`
    // BotID 机器人标识
    BotID string `json:"bot_id"`
    // ReqMethod 请求方法（仅 req 使用）
    ReqMethod ReqMethod `json:"req_method"`
    // EventType 事件类型（仅 event/res 使用）
    EventType EventType `json:"event_type"`
    // Mode 运行模式（仅 req 使用）
    Mode Mode `json:"mode"`
    // IsStream 是否流式请求（仅 req 使用）
    IsStream bool `json:"is_stream"`
    // StreamSeq 流式序号（0 表示未设置，实际序号从 1 开始）
    StreamSeq int `json:"stream_seq"`
    // StreamID 流式标识（空串表示未设置）
    StreamID string `json:"stream_id"`
    // GroupDigitalAvatar 数字分身群聊模式
    GroupDigitalAvatar bool `json:"group_digital_avatar"`
    // EnableMemory 是否启用记忆
    EnableMemory bool `json:"enable_memory"`
    // EnableStreaming 是否启用流式输出
    // 注意：Python 默认 True，Go bool 零值 False；工厂函数显式设 True 对齐 Python
    EnableStreaming bool `json:"enable_streaming"`

    // ─── 可选字段（指针/切片/map 加 omitempty） ───

    // Payload 响应/事件负载（res/event 用，req 为 nil）
    Payload json.RawMessage `json:"payload,omitempty"`
    // Metadata 扩展元数据（Gateway 需要主动读写内部字段）
    Metadata map[string]any `json:"metadata,omitempty"`
}
```

### 5.1 字段分组说明

| 分组 | 字段 | omitempty | 理由 |
|------|------|-----------|------|
| 必填 | id, type, channel_id, session_id, params, timestamp, ok | 不加 | 始终输出 |
| 可选-值类型 | provider, chat_id, user_id, bot_id, req_method, event_type, mode, is_stream, stream_seq, stream_id, group_digital_avatar, enable_memory, enable_streaming | 不加 | 保留零值输出，与 Python 行为对齐 |
| 可选-引用类型 | payload, metadata | 加 | nil 时省略合理 |

## 6. Validate() error

仅校验**必须提供的字段**，互斥约束由工厂函数保证：

| # | 规则 | 说明 | Python 依据 |
|---|------|------|------------|
| 1 | `id` 非空 | 所有构造都提供 id | ✅ |
| 2 | `type` 合法 | 必须是 req/res/event 之一 | ✅ |
| 3 | `channel_id` 非空 | 所有构造都提供 channel_id | ✅ |
| 4 | `type=req` 时 `req_method` 非零值 | Python 端 req 必定设置 req_method | ✅ |
| 5 | `type=event` 时 `event_type` 非零值 | Python 端 event 必定设置 event_type | ✅ |

**不校验的规则**（互斥约束，由工厂函数保证）：
- req 时 event_type/payload 为零值
- res 时 req_method/mode/is_stream 为零值，params 为空
- event 时 req_method/mode/is_stream 为零值，params 为空
- session_id 非空（res/event 中可能为空）

## 7. 工厂函数

```go
// MessageOption 消息可选配置函数
type MessageOption func(*Message)

// WithMode 设置运行模式
func WithMode(m Mode) MessageOption

// WithIsStream 设置是否流式
func WithIsStream(v bool) MessageOption

// WithProvider 设置 IM 平台名称
func WithProvider(p string) MessageOption

// WithChatID 设置 IM 聊天标识
func WithChatID(id string) MessageOption

// WithUserID 设置发送者标识
func WithUserID(id string) MessageOption

// WithBotID 设置机器人标识
func WithBotID(id string) MessageOption

// WithMetadata 设置扩展元数据
func WithMetadata(m map[string]any) MessageOption

// WithGroupDigitalAvatar 设置数字分身群聊模式
func WithGroupDigitalAvatar(v bool) MessageOption

// WithEnableMemory 设置是否启用记忆
func WithEnableMemory(v bool) MessageOption

// WithEnableStreaming 设置是否启用流式输出
func WithEnableStreaming(v bool) MessageOption

// WithSessionID 设置会话标识
func WithSessionID(id string) MessageOption

// WithStreamSeq 设置流式序号
func WithStreamSeq(seq int) MessageOption

// WithStreamID 设置流式标识
func WithStreamID(id string) MessageOption

// WithEventType 设置事件类型（仅 res 使用，如 CHAT_FINAL）
func WithEventType(et EventType) MessageOption

// NewReqMessage 构造请求消息。
// 自动生成 UUID id 和 float64 timestamp，设置 type=MessageTypeReq, ok=true, enable_streaming=true。
// 工厂函数保证：event_type=零值, payload=nil, mode=零值, is_stream=false。
func NewReqMessage(channelID, sessionID string, reqMethod ReqMethod, params json.RawMessage, opts ...MessageOption) *Message

// NewResMessage 构造响应消息。
// 自动生成 UUID id 和 float64 timestamp，设置 type=MessageTypeRes, enable_streaming=true。
// 工厂函数保证：req_method=零值, mode=零值, is_stream=false, params=nil。
func NewResMessage(channelID, sessionID string, ok bool, payload json.RawMessage, opts ...MessageOption) *Message

// NewEventMessage 构造事件消息。
// 自动生成 UUID id 和 float64 timestamp，设置 type=MessageTypeEvent, ok=true, enable_streaming=true。
// 工厂函数保证：req_method=零值, mode=零值, is_stream=false, params=nil。
func NewEventMessage(channelID, sessionID string, eventType EventType, payload json.RawMessage, opts ...MessageOption) *Message
```

## 8. 辅助函数

```go
// NowTimestamp 返回当前时间的 Unix 秒浮点数，对齐 Python time.time()
func NowTimestamp() float64

// NewMessageID 生成 UUID v4，32 hex 无连字符，对齐 Python uuid4().hex
func NewMessageID() string
```

## 9. 测试覆盖

| 测试类 | 内容 |
|--------|------|
| MessageType 枚举 | ParseMessageType/IsValidMessageType/String/GoString |
| JSON 往返（req） | 全字段 Message{type=req} → Marshal → Unmarshal → 逐字段比对 |
| JSON 往返（res） | 全字段 Message{type=res} → Marshal → Unmarshal → 逐字段比对 |
| JSON 往返（event） | 全字段 Message{type=event} → Marshal → Unmarshal → 逐字段比对 |
| JSON 往返（最小） | 仅必填字段的 Message 往返 |
| Validate 合法 | req/res/event 各构造合法 Message，Validate 返回 nil |
| Validate 非法 | 缺 id、非法 type、缺 channel_id、req 缺 req_method、event 缺 event_type |
| 工厂函数 | NewReqMessage/NewResMessage/NewEventMessage 字段正确性 + 默认值 |
| Option 函数 | 各 With* 函数正确设置对应字段 |
| omitempty 验证 | payload=nil/metadata=nil 时 JSON 省略；非 nil 时输出 |
| NowTimestamp | 返回值在合理范围内（> 1e9） |
| NewMessageID | 返回 32 字符 hex 串 |

## 10. doc.go 更新

更新 `internal/swarm/schema/doc.go` 的文件目录：

```
// schema/
// ├── doc.go           # 包文档
// ├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
// ├── event_type.go    # EventType 枚举（26 个事件类型）
// ├── mode.go          # Mode 枚举（6 个运行模式）
// └── message.go       # MessageType 枚举 + Message 模型 + 工厂函数 + Validate
```

## 11. 回填内容

- **IMPLEMENTATION_PLAN.md**：10.1.4 状态 ☐ → ✅
- **doc.go**：文件目录添加 message.go 条目
