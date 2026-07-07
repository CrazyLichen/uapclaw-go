# EventType 枚举设计

> 本文档描述 `internal/swarm/schema/event_type.go` 的实现设计，
> 对应 IMPLEMENTATION_PLAN.md 步骤 10.1.2。

---

## 1. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 全量 26 个一次实现 | 与 ReqMethod 策略一致，后续无需回填 |
| 实现模式 | 完全复刻 ReqMethod | 同包两套枚举行为一致，零学习成本 |
| 分组方式 | 按事件命名空间 | 与值的前缀自然对应：connection/hello/chat/context/todo/team/heartbeat/history |
| 解析策略 | 严格模式，未知值返回 error | 与 ReqMethod 一致，Python 新增事件时需同步更新 |
| MVP 子集 | 不区分 | 全量实现后无需 MVP 标注 |

---

## 2. 流程位置与作用

```
用户入口                swarm 层                              agentcore 层
─────────              ──────────────────                    ─────────────────
CLI/HTTP/Web ─→ Channel ─→ Gateway ─→ E2A编码 ─→ Transport ─→ E2A解码 ─→ AgentServer
                                  │                                    │
                                  │  请求方向: ReqMethod               │  响应方向: EventType
                                  │  (标识调用哪个 RPC)                │  (标识返回什么事件)
                                  │                                    │
                                  └── Message.req_method               └── AgentResponse.event_type
```

EventType 位于通信链路的**响应/事件方向**：

| 作用 | 说明 |
|------|------|
| 事件类型甄别 | AgentServer 向 Gateway/客户端推送时，EventType 标识事件种类 |
| 流式响应分片标记 | 每个 AgentResponseChunk 带 event_type，前端据此决定渲染方式 |
| E2A 协议编解码 | Wire Codec 序列化/反序列化的核心字段 |
| Gateway 消息路由 | MessageHandler 根据 EventType 分发到正确的 Channel 处理器 |

---

## 3. Python 源码对照

对应 Python 源码：`jiuwenswarm/common/schema/message.py` (EventType 类)

Python 共 26 个成员，值与 Go 常量一一对应：

| # | Python 名称 | Python 值 | Go 常量名 | 分组 |
|---|-------------|-----------|-----------|------|
| 1 | CONNECTION_ACK | "connection.ack" | EventTypeConnectionAck | 连接 |
| 2 | HELLO | "hello" | EventTypeHello | 连接 |
| 3 | CHAT_DELTA | "chat.delta" | EventTypeChatDelta | chat 流式 |
| 4 | CHAT_REASONING | "chat.reasoning" | EventTypeChatReasoning | chat 流式 |
| 5 | CHAT_USAGE_METADATA | "chat.usage_metadata" | EventTypeChatUsageMetadata | chat 流式 |
| 6 | CHAT_USAGE_SUMMARY | "chat.usage_summary" | EventTypeChatUsageSummary | chat 流式 |
| 7 | CHAT_FINAL | "chat.final" | EventTypeChatFinal | chat 流式 |
| 8 | CHAT_MEDIA | "chat.media" | EventTypeChatMedia | chat 流式 |
| 9 | CHAT_FILE | "chat.file" | EventTypeChatFile | chat 流式 |
| 10 | CHAT_TOOL_CALL | "chat.tool_call" | EventTypeChatToolCall | chat 工具 |
| 11 | CHAT_TOOL_UPDATE | "chat.tool_update" | EventTypeChatToolUpdate | chat 工具 |
| 12 | CHAT_TOOL_RESULT | "chat.tool_result" | EventTypeChatToolResult | chat 工具 |
| 13 | CONTEXT_USAGE | "context.usage" | EventTypeContextUsage | context |
| 14 | TODO_UPDATED | "todo.updated" | EventTypeTodoUpdated | todo |
| 15 | CHAT_PROCESSING_STATUS | "chat.processing_status" | EventTypeChatProcessingStatus | chat 状态 |
| 16 | CHAT_ERROR | "chat.error" | EventTypeChatError | chat 状态 |
| 17 | CHAT_INTERRUPT_RESULT | "chat.interrupt_result" | EventTypeChatInterruptResult | chat 状态 |
| 18 | CHAT_EVOLUTION_STATUS | "chat.evolution_status" | EventTypeChatEvolutionStatus | chat 状态 |
| 19 | CHAT_SUBTASK_UPDATE | "chat.subtask_update" | EventTypeChatSubtaskUpdate | chat 状态 |
| 20 | CHAT_ASK_USER_QUESTION | "chat.ask_user_question" | EventTypeChatAskUserQuestion | chat 状态 |
| 21 | CHAT_SESSION_RESULT | "chat.session_result" | EventTypeChatSessionResult | chat 状态 |
| 22 | TEAM_MEMBER | "team.member" | EventTypeTeamMember | team |
| 23 | TEAM_TASK | "team.task" | EventTypeTeamTask | team |
| 24 | TEAM_MESSAGE | "team.message" | EventTypeTeamMessage | team |
| 25 | HEARTBEAT_RELAY | "heartbeat.relay" | EventTypeHeartbeatRelay | heartbeat |
| 26 | HISTORY_GET | "history.message" | EventTypeHistoryGet | history |

---

## 4. Go 类型定义

```go
// EventType E2A 协议事件类型枚举。
//
// 定义 AgentServer→Gateway 通信链路中所有合法的事件类型标识，
// 用于 AgentResponse/AgentResponseChunk 的 event_type 字段和 Gateway 消息路由。
// 值为点分字符串格式（如 "chat.delta"），与 Python EventType 枚举值一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (EventType)
type EventType string
```

---

## 5. 常量分组

按事件命名空间分组，每组用 `// ─── 命名空间 ───` 分隔注释：

```
// ─── 连接 ───       connection.ack, hello
// ─── chat 流式 ───   chat.delta, chat.reasoning, chat.usage_metadata,
//                      chat.usage_summary, chat.final, chat.media, chat.file
// ─── chat 工具 ───   chat.tool_call, chat.tool_update, chat.tool_result
// ─── chat 状态 ───   chat.processing_status, chat.error, chat.interrupt_result,
//                      chat.evolution_status, chat.subtask_update,
//                      chat.ask_user_question, chat.session_result
// ─── context ───     context.usage
// ─── todo ───        todo.updated
// ─── team ───        team.member, team.task, team.message
// ─── heartbeat ───   heartbeat.relay
// ─── history ───     history.message
```

---

## 6. 导出函数

| 函数 | 签名 | 行为 |
|------|------|------|
| AllEventTypes | `func AllEventTypes() []EventType` | 返回全部 26 个常量的切片 |
| ParseEventType | `func ParseEventType(s string) (EventType, error)` | 从 eventTypeLookup 查找，找不到返回 `fmt.Errorf("不合法的 EventType 值: %q", s)` |
| IsValidEventType | `func IsValidEventType(s string) bool` | `_, ok := eventTypeLookup[s]; return ok` |

---

## 7. 方法

| 方法 | 签名 | 行为 |
|------|------|------|
| String | `func (et EventType) String() string` | `return string(et)` |
| GoString | `func (et EventType) GoString() string` | `return fmt.Sprintf("schema.EventType(%q)", string(et))` |

---

## 8. 声明排列顺序

遵循 Go 编码规范（规范 2）：

```
1. 结构体 — 无
2. 枚举 — EventType type + const 块
3. 常量 — 无
4. 全局变量 — eventTypeLookup map[string]EventType
5. 导出函数 — AllEventTypes / ParseEventType / IsValidEventType
6. 非导出函数 — init()
```

各类声明之间用分隔注释区分。

---

## 9. 测试覆盖

测试文件 `event_type_test.go`，与 `req_method_test.go` 模式一致：

| 测试函数 | 验证内容 |
|----------|---------|
| TestAllEventTypes | 返回 26 个、无重复、包含关键事件（ConnectionAck/Hello/ChatDelta/ChatFinal/ChatError） |
| TestParseEventType_合法值 | 覆盖各分组典型值 |
| TestParseEventType_非法值 | 空字符串、不存在值、格式错误值 |
| TestIsValidEventType | 合法值返回 true、非法值返回 false |
| TestEventType_String | 值透传 `string(et)` |
| TestEventType_GoString | `schema.EventType("chat.delta")` 格式 |
| TestEventType_JSON往返 | 序列化→反序列化→值相等 |

---

## 10. 需同步更新的文件

| 文件 | 更新内容 |
|------|---------|
| `internal/swarm/schema/doc.go` | 文件目录增加 `event_type.go` 条目，更新包功能概述 |
| `IMPLEMENTATION_PLAN.md` | 步骤 10.1.2 状态 ☐→✅ |

---

## 11. 不在本设计范围内

| 内容 | 原因 |
|------|------|
| EventType 在 AgentResponse/AgentResponseChunk 中的使用 | 属于步骤 10.1.5 |
| EventType 在 E2AEnvelope 中的使用 | 属于步骤 10.2 |
| EventType 在 Gateway MessageHandler 中的路由 | 属于步骤 11.x |
| JSON Marshal/Unmarshal 自定义编解码 | 可后续与 ReqMethod 统一添加 |
