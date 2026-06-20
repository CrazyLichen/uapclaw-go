# 5.18 BaseMessage 接口化 + Offload 消息模型 设计

## 一、背景

5.18 要求实现 Offload 消息模型（`OffloadUserMessage/AssistantMessage/SystemMessage/ToolMessage`），
对应 Python `openjiuwen/core/context_engine/schema/messages.py`。

Python 使用多重继承实现 `OffloadUserMessage(UserMessage, OffloadMixin)`，通过 `isinstance(msg, OffloadMixin)` 
做防重复卸载检查。Go 没有多重继承，当前 `BaseMessage` 是具体结构体，消息列表统一使用 `[]*BaseMessage`，
无法对 Offload 子类型做接口断言。

**核心问题**：Go 中 `*OffloadUserMessage` ≠ `*BaseMessage`，无法放入 `[]*BaseMessage` 列表，
也无法做 `msg.(Offloadable)` 类型断言。

**解决方案**：将 `BaseMessage` 从具体结构体改为接口，引入 `DefaultMessage` 作为默认实现，
所有消息类型（UserMessage/AssistantMessage/ToolMessage/OffloadXxxMessage）都实现 `BaseMessage` 接口。

## 二、类型体系设计

### 2.1 层次结构

```
当前（结构体嵌入）:
  BaseMessage struct { Role, Content, Name, Metadata }
    ├── UserMessage           struct { BaseMessage }
    ├── SystemMessage         struct { BaseMessage }
    ├── AssistantMessage      struct { BaseMessage; ToolCalls; UsageMetadata; ... }
    ├── ToolMessage           struct { BaseMessage; ToolCallID }
    ├── AssistantMessageChunk struct { AssistantMessage }
    └── ToolMessageChunk      struct { ToolMessage }

目标（接口 + 默认实现）:
  BaseMessage interface { GetRole(); SetRole(); GetContent(); SetContent(); ... }
    │
    ├── DefaultMessage struct { Role, Content, Name, Metadata }    ← 默认实现
    │     ├── UserMessage           struct { DefaultMessage }
    │     └── SystemMessage         struct { DefaultMessage }
    │
    ├── AssistantMessage      struct { DefaultMessage; ToolCalls; UsageMetadata; ... }
    ├── ToolMessage           struct { DefaultMessage; ToolCallID }
    ├── AssistantMessageChunk struct { AssistantMessage }
    ├── ToolMessageChunk      struct { ToolMessage }
    │
    └── OffloadXxxMessage struct { XxxMessage; OffloadInfo }      ← 新增
```

### 2.2 BaseMessage 接口定义

```go
// BaseMessage 消息基类接口，所有消息类型均实现此接口。
//
// 对应 Python: BaseMessage (Pydantic BaseModel)，作为所有消息类型的基类。
// Go 端使用接口替代 Python 的类继承，实现统一的多态访问。
//
// 接口包含 getter + setter，支持消息创建后的字段修改
// （与当前代码中 msg.Content = ... 的直接赋值行为一致）。
type BaseMessage interface {
    // GetRole 获取消息角色
    GetRole() RoleType
    // SetRole 设置消息角色
    SetRole(role RoleType)
    // GetContent 获取消息内容
    GetContent() MessageContent
    // SetContent 设置消息内容
    SetContent(content MessageContent)
    // GetName 获取消息发送者名称
    GetName() string
    // SetName 设置消息发送者名称
    SetName(name string)
    // GetMetadata 获取附加元数据
    GetMetadata() map[string]any
    // SetMetadata 设置附加元数据
    SetMetadata(metadata map[string]any)
}
```

### 2.3 DefaultMessage 默认实现

```go
// DefaultMessage BaseMessage 接口的默认实现，提供 Role/Content/Name/Metadata 四个基础字段。
//
// 其他消息类型（UserMessage/SystemMessage/AssistantMessage/ToolMessage）
// 通过嵌入 DefaultMessage 复用基础字段和 BaseMessage 接口实现。
// 对应 Python: BaseMessage(role=..., content=..., name=..., metadata=...)
type DefaultMessage struct {
    // Role 消息角色
    Role RoleType `json:"role"`
    // Content 消息内容
    Content MessageContent `json:"content"`
    // Name 消息发送者名称（可选，function calling 场景）
    Name string `json:"name,omitempty"`
    // Metadata 附加元数据
    Metadata map[string]any `json:"metadata,omitempty"`
}

// GetRole/SetRole/GetContent/SetContent/GetName/SetName/GetMetadata/SetMetadata
// 8 个方法由 DefaultMessage 实现，嵌入 DefaultMessage 的子类型自动获得这些方法。
```

### 2.4 消息子类型调整

```go
// UserMessage 用户消息，role 固定为 "user"。
type UserMessage struct {
    DefaultMessage   // 改为嵌入 DefaultMessage
}

// SystemMessage 系统消息，role 固定为 "system"。
type SystemMessage struct {
    DefaultMessage   // 改为嵌入 DefaultMessage
}

// AssistantMessage 助手消息，LLM 响应的核心载体。
type AssistantMessage struct {
    DefaultMessage   // 改为嵌入 DefaultMessage（保留 ToolCalls/UsageMetadata 等扩展字段不变）
    ToolCalls []*ToolCall `json:"tool_calls,omitempty"`
    // ... 其余字段不变
}

// ToolMessage 工具返回消息。
type ToolMessage struct {
    DefaultMessage   // 改为嵌入 DefaultMessage（保留 ToolCallID 不变）
    ToolCallID string `json:"tool_call_id"`
}
```

### 2.5 Offload 消息模型

```go
// OffloadInfo 卸载元数据，等价 Python OffloadMixin。
type OffloadInfo struct {
    // OffloadType 存储后端类型："in_memory" 或 "filesystem"
    OffloadType string `json:"offload_type"`
    // OffloadHandle 唯一检索标识
    OffloadHandle string `json:"offload_handle"`
    // Metadata 附加元数据
    Metadata map[string]any `json:"metadata,omitempty"`
}

// Offloadable 卸载消息接口，等价 Python isinstance(msg, OffloadMixin)。
//
// 4 个 Offload 子类型均实现此接口，用于防重复卸载检查：
//   _, ok := msg.(Offloadable)
type Offloadable interface {
    BaseMessage  // 嵌入 BaseMessage 接口，Offload 消息同时也是普通消息
    GetOffloadInfo() OffloadInfo
}

// OffloadUserMessage 卸载的用户消息占位符
type OffloadUserMessage struct {
    UserMessage      // 嵌入 UserMessage（其内部嵌入 DefaultMessage）
    OffloadInfo
}

// OffloadAssistantMessage 卸载的助手消息占位符
type OffloadAssistantMessage struct {
    AssistantMessage // 嵌入完整 AssistantMessage
    OffloadInfo
}

// OffloadSystemMessage 卸载的系统消息占位符
type OffloadSystemMessage struct {
    SystemMessage    // 嵌入 SystemMessage（其内部嵌入 DefaultMessage）
    OffloadInfo
}

// OffloadToolMessage 卸载的工具消息占位符
type OffloadToolMessage struct {
    ToolMessage      // 嵌入完整 ToolMessage
    OffloadInfo
}
```

**关键优势**：`OffloadXxxMessage` 嵌入 `XxxMessage`，自动获得 `BaseMessage` 接口的所有方法。
可以直接放入 `[]BaseMessage` 列表，也可以做 `msg.(Offloadable)` 类型断言。

## 三、接口方法与嵌入的提升规则

### 3.1 Go 接口嵌入的方法提升

```go
// OffloadUserMessage 嵌入 UserMessage，UserMessage 嵌入 DefaultMessage
// DefaultMessage 实现了 BaseMessage 接口的 8 个方法
// → OffloadUserMessage 自动拥有这 8 个方法 → 满足 BaseMessage 接口
// → OffloadUserMessage 也满足 Offloadable 接口（BaseMessage + GetOffloadInfo）
```

### 3.2 字段访问的变更

| 操作 | 当前方式 | 接口化后方式 |
|------|---------|------------|
| 读取 Role | `msg.Role` | `msg.GetRole()` (通过接口) 或 `msg.Role` (具体类型仍可直接访问字段) |
| 写入 Content | `msg.Content = ...` | `msg.SetContent(...)` (通过接口) 或 `msg.Content = ...` (具体类型) |
| 通过接口访问 | 不适用 | `var m BaseMessage = userMsg; m.GetRole()` |

**注意**：具体类型（`*UserMessage` 等）仍然可以直接通过字段名访问（`msg.Role`），
因为 `DefaultMessage` 的字段是导出的。只有通过 `BaseMessage` 接口访问时才需要用 getter/setter。

### 3.3 消息列表类型变更

```go
// 当前
GetMessages(size *int, withHistory bool) []*BaseMessage

// 改为
GetMessages(size *int, withHistory bool) []BaseMessage
```

所有 `[]*BaseMessage` 改为 `[]BaseMessage`，对应 `ContextWindow`、`TokenCounter`、`ModelContext` 等接口。

## 四、序列化策略

### 4.1 DefaultMessage 序列化

`DefaultMessage` 没有自定义 `MarshalJSON`/`UnmarshalJSON`，依赖 Go 默认 JSON 序列化。
嵌入 `DefaultMessage` 的子类型，字段自动提升到 JSON 顶层。

**OffloadUserMessage 的 JSON 输出**（示例）：
```json
{
  "role": "user",
  "content": "...[[OFFLOAD: handle=abc, type=in_memory]]",
  "offload_type": "in_memory",
  "offload_handle": "abc",
  "metadata": {}
}
```

### 4.2 需要自定义序列化的类型

| 类型 | 需要自定义 Marshal | 需要自定义 Unmarshal | 原因 |
|------|-------------------|---------------------|------|
| AssistantMessage | ✅ 已有 | ✅ 已有 | ToolCalls 格式转换、Content 延迟解析 |
| OffloadAssistantMessage | ✅ **必须新增** | ✅ **必须新增** | 嵌入 AssistantMessage(有自定义序列化) |
| 其他类型 | ❌ | ❌ | 依赖默认序列化即可 |

**OffloadAssistantMessage 序列化实现**：
- MarshalJSON：先序列化 `AssistantMessage`，再序列化 `OffloadInfo`，合并为平级 JSON
- UnmarshalJSON：临时结构体解析所有字段，分发给 `AssistantMessage` 和 `OffloadInfo`

### 4.3 反序列化工厂

```go
// UnmarshalOffloadMessage 从 JSON 反序列化为对应 Offload 子类型。
// 根据 role 字段自动分派。
func UnmarshalOffloadMessage(data []byte) (Offloadable, error)

// UnmarshalMessage 从 JSON 反序列化为对应消息类型。
// 根据 role + offload_type/offload_handle 存在与否决定返回普通消息或 Offload 消息。
func UnmarshalMessage(data []byte) (BaseMessage, error)
```

## 五、关键函数

| 函数 | 等价 Python | 用途 |
|------|------------|------|
| `NewBaseMessage(role, content, ...opts)` | `BaseMessage(role=..., content=...)` | 创建 DefaultMessage |
| `NewUserMessage(content, ...opts)` | `UserMessage(content=...)` | 不变 |
| `NewAssistantMessage(content, ...opts)` | `AssistantMessage(content=...)` | 不变 |
| `NewOffloadMessage(role, content, handle, offloadType, ...opts)` | `create_offload_message()` | 工厂函数 |
| `IsOffloaded(msg BaseMessage) bool` | `isinstance(msg, OffloadMixin)` | 防重复卸载 |
| `UnmarshalOffloadMessage(data)` | Pydantic 自动 | Offload 专用反序列化 |
| `UnmarshalMessage(data)` | Pydantic 自动 | 通用消息反序列化 |

## 六、MessageOption 调整

```go
// 当前
type MessageOption func(*BaseMessage)

// 改为：MessageOption 作用于 DefaultMessage
type MessageOption func(*DefaultMessage)
```

所有 `WithXxx` 选项函数参数从 `*BaseMessage` 改为 `*DefaultMessage`，
内部逻辑不变（仍然设置 Role/Content/Name/Metadata）。

## 七、影响范围

### 7.1 必须修改的文件（schema 包内部）

| 文件 | 变更内容 |
|------|---------|
| `schema/message.go` | BaseMessage struct → interface + DefaultMessage struct；MessageOption 参数类型；构造函数返回类型 |
| `schema/assistant_message.go` | `BaseMessage` → `DefaultMessage`；MarshalJSON/UnmarshalJSON 内部字段引用 |
| `schema/tool_message.go` | `BaseMessage` → `DefaultMessage`；构造函数 |
| `schema/message_chunk.go` | `BaseMessage{...}` → `DefaultMessage{...}`；Merge 方法内部构造 |
| `schema/doc.go` | 更新文档 |

### 7.2 必须修改的文件（schema 包外部）

| 文件 | 变更内容 |
|------|---------|
| `model_clients/messages_param.go` | `toBaseMessage` 改为接口断言；`[]*BaseMessage` → `[]BaseMessage` |
| `model_clients/base_client.go` | `baseMsg.Role` → `baseMsg.GetRole()`；`baseMsg.Content` → `baseMsg.GetContent()` |
| `prompt/template.go` | `&um.BaseMessage` → `um`（直接传接口值）；`[]*schema.BaseMessage` → `[]schema.BaseMessage` |
| `prompt/assembler.go` | `msg.Content` → `msg.GetContent()`/`msg.SetContent()`；`[]*schema.BaseMessage` → `[]schema.BaseMessage` |
| `context_engine/base.go` | `[]*llm_schema.BaseMessage` → `[]llm_schema.BaseMessage`（ModelContext 接口 + ContextWindow 结构体） |
| `context_engine/token/base.go` | `[]*llm_schema.BaseMessage` → `[]llm_schema.BaseMessage` |
| `context_engine/base_test.go` | 适配新类型 |

### 7.3 需要验证的文件

| 文件 | 需验证 |
|------|--------|
| `store/message/db/base_message_store.go` | `*schema.BaseMessage` → `schema.BaseMessage` |
| `store/message/db/sql_message_store.go` | 反序列化逻辑调整 |
| `session/tracer/decorator.go` | 消息类型断言 |

### 7.4 新增文件

| 文件 | 内容 |
|------|------|
| `context_engine/schema/offload.go` | OffloadInfo + 4 个 Offload 子类型 + 构造函数 + 工厂 + IsOffloaded + UnmarshalOffloadMessage |
| `context_engine/schema/offload_test.go` | Offload 单元测试 |
| `context_engine/schema/doc.go` | 更新文件目录 |

## 八、回填关系

### 8.1 已有回填点（5.18 不回填）

| 回填点 | 来源 | 回填时机 |
|--------|------|---------|
| `ModelContext.ReloadTool()` | 5.15 | 5.27/5.28 处理器 |
| `StatMessages/StatTools/StatContextWindow` | 5.16 | 5.31 Context 实现 |
| `ContextEngineConfig.EnableReload` | 5.17 | 已在 5.17 定义 |

### 8.2 5.18 被后续步骤回填

| 5.18 定义 | 被谁使用 |
|-----------|---------|
| `Offloadable` 接口 | 5.27/5.28 处理器（防重复卸载检查） |
| `NewOffloadMessage` 工厂 | 5.27/5.28 处理器（创建占位符） |
| `IsOffloaded` 函数 | 5.27/5.28 处理器 |
| `UnmarshalMessage` 工厂 | 5.31 Context 实现（状态持久化反序列化） |
| `BaseMessage` 接口 | 5.20+ 所有上下文引擎代码 |

## 九、不做的范围

- ❌ 不实现 `ReloaderTool`（5.27/5.28 范畴）
- ❌ 不实现 `OffloadMessageBuffer`（5.27/5.28 范畴）
- ❌ 不实现处理器链（5.21+ 范畴）
- ❌ 不修改 `ContextEngineConfig`
