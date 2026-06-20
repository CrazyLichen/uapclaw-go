# BaseMessage 接口化 + Offload 消息模型 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 BaseMessage 从具体结构体改为接口（引入 DefaultMessage 默认实现），并实现 Offload 消息模型（4 个独立子类型 + Offloadable 接口）。

**Architecture:** BaseMessage 接口定义 getter/setter 方法，DefaultMessage 提供默认实现。UserMessage/SystemMessage 嵌入 DefaultMessage，AssistantMessage/ToolMessage 嵌入 DefaultMessage + 扩展字段。OffloadXxxMessage 嵌入对应 XxxMessage + OffloadInfo，自动满足 BaseMessage 和 Offloadable 两个接口。所有 `[]*BaseMessage` 改为 `[]BaseMessage`。

**Tech Stack:** Go 1.22+, 标准库 encoding/json

---

## 文件结构

### 修改的文件（按依赖顺序）

| # | 文件 | 变更内容 |
|---|------|---------|
| 1 | `foundation/llm/schema/message.go` | 核心变更：BaseMessage struct→interface + DefaultMessage struct + 构造函数/选项函数适配 |
| 2 | `foundation/llm/schema/assistant_message.go` | BaseMessage→DefaultMessage 嵌入；MarshalJSON/UnmarshalJSON 内部字段引用适配 |
| 3 | `foundation/llm/schema/tool_message.go` | BaseMessage→DefaultMessage 嵌入；构造函数适配 |
| 4 | `foundation/llm/schema/message_chunk.go` | BaseMessage→DefaultMessage 字面量构造；Merge 方法适配 |
| 5 | `foundation/llm/schema/doc.go` | 更新文档 |
| 6 | `foundation/llm/model_clients/messages_param.go` | toBaseMessage 重写为接口断言；ConvertMessagesToDict 适配 |
| 7 | `foundation/llm/model_clients/base_client.go` | baseMsg 字段访问改为接口方法 |
| 8 | `foundation/prompt/template.go` | `[]*schema.BaseMessage`→`[]schema.BaseMessage`；ToMessages 返回类型；deepCopyContent 适配 |
| 9 | `foundation/prompt/assembler.go` | `[]*schema.BaseMessage`→`[]schema.BaseMessage`；assignMessageContent 签名；deepCopyMessages 适配 |
| 10 | `foundation/store/db/base_message_store.go` | `*schema.BaseMessage`→`schema.BaseMessage` |
| 11 | `memory/manage/model/message_manager.go` | `NewBaseMessage` 调用适配 |
| 12 | `memory/manage/model/sql_message_store.go` | `rowToMessageAndMeta` 按 role 构造具体类型 |
| 13 | `context_engine/base.go` | `[]*llm_schema.BaseMessage`→`[]llm_schema.BaseMessage`（ModelContext + ContextWindow） |
| 14 | `context_engine/token/base.go` | `[]*llm_schema.BaseMessage`→`[]llm_schema.BaseMessage` |
| 15 | `context_engine/schema/doc.go` | 更新文件目录 |

### 新增的文件

| # | 文件 | 内容 |
|---|------|------|
| 16 | `context_engine/schema/offload.go` | OffloadInfo + 4 个 Offload 子类型 + GetOffloadInfo + 构造函数 + NewOffloadMessage 工厂 + IsOffloaded + UnmarshalOffloadMessage + UnmarshalMessage |
| 17 | `context_engine/schema/offload_test.go` | Offload 完整单元测试 |

### 测试文件（需适配）

| # | 文件 | 变更内容 |
|---|------|---------|
| 18 | `foundation/llm/schema/message_test.go` | BaseMessage 测试适配 |
| 19 | `foundation/llm/schema/assistant_message_test.go` | 字段访问方式适配 |
| 20 | `foundation/llm/schema/tool_message_test.go` | 字段访问方式适配 |
| 21 | `foundation/llm/schema/message_chunk_test.go` | Merge 相关适配 |
| 22 | `foundation/llm/model_clients/messages_param_test.go` | toBaseMessage 测试适配 |
| 23 | `foundation/prompt/template_test.go` | `[]*schema.BaseMessage`→`[]schema.BaseMessage` |
| 24 | `foundation/prompt/assembler_test.go` | `[]*schema.BaseMessage`→`[]schema.BaseMessage` |
| 25 | `store/db/base_message_store_test.go` | 接口签名适配 |
| 26 | `memory/manage/model/sql_message_store_test.go` | NewBaseMessage 调用适配 |
| 27 | `memory/manage/model/message_manager_test.go` | mock 适配 |
| 28 | `session/tracer/decorator_test.go` | BaseMessage 字面量适配 |
| 29 | `context_engine/base_test.go` | `[]*llm_schema.BaseMessage`→`[]llm_schema.BaseMessage` |

### 文档文件

| # | 文件 | 变更内容 |
|---|------|---------|
| 30 | `foundation/llm/schema/doc.go` | 更新消息体系描述 |
| 31 | `foundation/prompt/doc.go` | 更新示例代码 |
| 32 | `foundation/store/db/doc.go` | 更新描述 |
| 33 | `memory/manage/model/doc.go` | 更新描述 |
| 34 | `context_engine/doc.go` | 更新文件目录 |

---

## Task 1: DefaultMessage 结构体 + BaseMessage 接口定义

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/message.go`

本 Task 只定义类型和接口方法，不修改构造函数和选项函数（Task 2 处理）。

- [ ] **Step 1: 在 message.go 中定义 DefaultMessage 结构体和 BaseMessage 接口**

在现有 `BaseMessage` struct 定义位置，将其替换为：

```go
// ──────────────────────────── 接口 ────────────────────────────

// BaseMessage 消息基类接口，所有消息类型均实现此接口。
//
// 对应 Python: BaseMessage (Pydantic BaseModel)，作为所有消息类型的基类。
// Go 端使用接口替代 Python 的类继承，实现统一的多态访问。
//
// 接口包含 getter + setter，支持消息创建后的字段修改
// （与当前代码中 msg.Content = ... 的直接赋值行为一致）。
// 具体类型仍可直接通过字段名访问（如 msg.Role），通过接口访问时使用 getter/setter。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message.py (BaseMessage)
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

// ──────────────────────────── 结构体 ────────────────────────────

// DefaultMessage BaseMessage 接口的默认实现，提供 Role/Content/Name/Metadata 四个基础字段。
//
// 其他消息类型（UserMessage/SystemMessage/AssistantMessage/ToolMessage）
// 通过嵌入 DefaultMessage 复用基础字段和 BaseMessage 接口实现。
//
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

// GetRole 返回消息角色
func (m *DefaultMessage) GetRole() RoleType { return m.Role }

// SetRole 设置消息角色
func (m *DefaultMessage) SetRole(role RoleType) { m.Role = role }

// GetContent 返回消息内容
func (m *DefaultMessage) GetContent() MessageContent { return m.Content }

// SetContent 设置消息内容
func (m *DefaultMessage) SetContent(content MessageContent) { m.Content = content }

// GetName 返回消息发送者名称
func (m *DefaultMessage) GetName() string { return m.Name }

// SetName 设置消息发送者名称
func (m *DefaultMessage) SetName(name string) { m.Name = name }

// GetMetadata 返回附加元数据
func (m *DefaultMessage) GetMetadata() map[string]any { return m.Metadata }

// SetMetadata 设置附加元数据
func (m *DefaultMessage) SetMetadata(metadata map[string]any) { m.Metadata = metadata }
```

同时将 `UserMessage` 和 `SystemMessage` 的嵌入从 `BaseMessage` 改为 `DefaultMessage`：

```go
// UserMessage 用户消息，role 固定为 "user"。
type UserMessage struct {
	DefaultMessage
}

// SystemMessage 系统消息，role 固定为 "system"。
type SystemMessage struct {
	DefaultMessage
}
```

- [ ] **Step 2: 修改 MessageOption 和 WithXxx 选项函数**

```go
// MessageOption DefaultMessage 构造选项函数。
type MessageOption func(*DefaultMessage)

// WithMessageName 设置消息发送者名称。
func WithMessageName(name string) MessageOption {
	return func(m *DefaultMessage) { m.Name = name }
}

// WithMetadata 设置附加元数据。
func WithMetadata(metadata map[string]any) MessageOption {
	return func(m *DefaultMessage) { m.Metadata = metadata }
}

// WithMultiModalContent 设置多模态内容。
func WithMultiModalContent(parts ...ContentPart) MessageOption {
	return func(m *DefaultMessage) { m.Content = NewMultiModalContent(parts...) }
}
```

- [ ] **Step 3: 修改构造函数**

```go
// NewDefaultMessage 创建 DefaultMessage 实例。
//
// 对应 Python: BaseMessage(role=..., content=..., name=..., metadata=...)
func NewDefaultMessage(role RoleType, content string, opts ...MessageOption) *DefaultMessage {
	msg := &DefaultMessage{
		Role:    role,
		Content: NewTextContent(content),
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// NewUserMessage 创建用户消息，role 固定为 "user"。
func NewUserMessage(content string, opts ...MessageOption) *UserMessage {
	msg := NewDefaultMessage(RoleTypeUser, content, opts...)
	return &UserMessage{DefaultMessage: *msg}
}

// NewSystemMessage 创建系统消息，role 固定为 "system"。
func NewSystemMessage(content string, opts ...MessageOption) *SystemMessage {
	msg := NewDefaultMessage(RoleTypeSystem, content, opts...)
	return &SystemMessage{DefaultMessage: *msg}
}
```

删除原 `NewBaseMessage` 函数，替换为 `NewDefaultMessage`。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/llm/schema/`

预期：其他包引用旧 `BaseMessage` struct 会报编译错误，这是正常的。本 Task 只确保 schema 包自身编译通过。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/message.go
git commit -m "refactor: BaseMessage struct→interface + DefaultMessage 默认实现

- BaseMessage 改为接口，定义 GetRole/SetRole/GetContent/SetContent/GetName/SetName/GetMetadata/SetMetadata 8 个方法
- 新增 DefaultMessage struct 作为 BaseMessage 的默认实现
- UserMessage/SystemMessage 嵌入改为 DefaultMessage
- MessageOption 参数类型改为 *DefaultMessage
- NewBaseMessage 重命名为 NewDefaultMessage"
```

---

## Task 2: AssistantMessage + ToolMessage 嵌入 DefaultMessage

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/assistant_message.go`
- Modify: `internal/agentcore/foundation/llm/schema/tool_message.go`

- [ ] **Step 1: 修改 AssistantMessage 嵌入和构造函数**

`assistant_message.go` 中：
- `BaseMessage` → `DefaultMessage`（嵌入字段）
- `BaseMessage: BaseMessage{...}` → `DefaultMessage: DefaultMessage{...}`（字面量构造）
- `AssistantMessageOption func(*AssistantMessage)` 不变
- 所有 `WithXxx` 选项函数不变（操作 `m.ToolCalls` 等扩展字段）

关键变更点——`NewAssistantMessage`：
```go
func NewAssistantMessage(content string, opts ...AssistantMessageOption) *AssistantMessage {
	msg := &AssistantMessage{
		DefaultMessage: DefaultMessage{
			Role:    RoleTypeAssistant,
			Content: NewTextContent(content),
		},
		FinishReason: FinishReasonNull,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}
```

关键变更点——`ToOpenAIDict` 中字段访问不变（`m.Content`、`m.Name`、`m.Metadata` 通过嵌入的 `DefaultMessage` 字段提升仍可直接访问）。

关键变更点——`UnmarshalJSON`：
- `m.Role = raw.Role` 等字段赋值不变（通过 DefaultMessage 提升的字段仍可直接赋值）

- [ ] **Step 2: 修改 ToolMessage 嵌入和构造函数**

`tool_message.go` 中：
- `BaseMessage` → `DefaultMessage`（嵌入字段）
- 构造函数适配：

```go
func NewToolMessage(toolCallID, content string, opts ...MessageOption) *ToolMessage {
	msg := NewDefaultMessage(RoleTypeTool, content, opts...)
	return &ToolMessage{
		DefaultMessage: *msg,
		ToolCallID:     toolCallID,
	}
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/llm/schema/`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/assistant_message.go internal/agentcore/foundation/llm/schema/tool_message.go
git commit -m "refactor: AssistantMessage/ToolMessage 嵌入改为 DefaultMessage"
```

---

## Task 3: MessageChunk 适配 DefaultMessage

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/message_chunk.go`

- [ ] **Step 1: 修改 AssistantMessageChunk 和 ToolMessageChunk 构造函数**

所有 `BaseMessage{...}` 字面量替换为 `DefaultMessage{...}`：

`NewAssistantMessageChunk`:
```go
func NewAssistantMessageChunk(content string, opts ...AssistantMessageChunkOption) *AssistantMessageChunk {
	chunk := &AssistantMessageChunk{
		AssistantMessage: AssistantMessage{
			DefaultMessage: DefaultMessage{
				Role:    RoleTypeAssistant,
				Content: NewTextContent(content),
			},
			FinishReason: FinishReasonNull,
		},
	}
	for _, opt := range opts {
		opt(chunk)
	}
	return chunk
}
```

`NewToolMessageChunk`:
```go
func NewToolMessageChunk(toolCallID, content string, opts ...ToolMessageChunkOption) *ToolMessageChunk {
	chunk := &ToolMessageChunk{
		ToolMessage: ToolMessage{
			DefaultMessage: DefaultMessage{
				Role:    RoleTypeTool,
				Content: NewTextContent(content),
			},
			ToolCallID: toolCallID,
		},
	}
	for _, opt := range opts {
		opt(chunk)
	}
	return chunk
}
```

`Merge` 方法中：
```go
// AssistantMessageChunk.Merge — 内部构造
return &AssistantMessageChunk{
    AssistantMessage: AssistantMessage{
        DefaultMessage: DefaultMessage{
            Role:     c.Role,
            Content:  mergedContent,
            Name:     mergedName,
            Metadata: mergedMetadata,
        },
        // ... 其余字段不变
    },
}

// ToolMessageChunk.Merge — 内部构造
return &ToolMessageChunk{
    ToolMessage: ToolMessage{
        DefaultMessage: DefaultMessage{
            Role:    c.Role,
            Content: NewTextContent(mergedContent),
        },
        ToolCallID: mergedToolCallID,
    },
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/llm/schema/`

- [ ] **Step 3: 运行 schema 包所有测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/schema/ -v -count=1`

预期：所有测试通过。如果测试中使用了 `BaseMessage` struct 类型名，需适配为 `DefaultMessage`。

- [ ] **Step 4: 适配 schema 包测试文件**

根据编译/测试错误，逐个修复 `*_test.go` 中的引用：
- `message_test.go`：`NewBaseMessage` → `NewDefaultMessage`；直接字段访问不变
- `assistant_message_test.go`：直接字段访问不变（通过 DefaultMessage 提升）
- `tool_message_test.go`：同上
- `message_chunk_test.go`：同上

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/schema/
git commit -m "refactor: MessageChunk 适配 DefaultMessage + 测试适配"
```

---

## Task 4: model_clients 包适配

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/messages_param.go`
- Modify: `internal/agentcore/foundation/llm/model_clients/base_client.go`

- [ ] **Step 1: 重写 toBaseMessage 函数**

`messages_param.go` 中 `toBaseMessage` 从"提取嵌入字段"改为"接口断言"：

```go
// toBaseMessage 将消息转换为 BaseMessage 接口。
// 由于所有消息类型（UserMessage/AssistantMessage/ToolMessage 等）
// 都实现了 BaseMessage 接口，直接做类型断言即可。
func toBaseMessage(msg any) (llmschema.BaseMessage, bool) {
	m, ok := msg.(llmschema.BaseMessage)
	return m, ok
}
```

- [ ] **Step 2: 修改 base_client.go 中 convertOneMessage**

`base_client.go` 中 `convertOneMessage` 方法，将字段直接访问改为接口方法调用：

```go
func (e *BaseClientEmbed) convertOneMessage(msg any) (map[string]any, error) {
	baseMsg, ok := toBaseMessage(msg)
	if !ok {
		return nil, fmt.Errorf("不支持的消息类型: %T", msg)
	}

	msgDict := map[string]any{
		"role":    baseMsg.GetRole().String(),
		"content": baseMsg.GetContent(),
	}
	if baseMsg.GetName() != "" {
		msgDict["name"] = baseMsg.GetName()
	}
	// AssistantMessage 特有字段 — 仍通过具体类型断言获取
	if am, ok := msg.(*llmschema.AssistantMessage); ok {
		if len(am.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(am.ToolCalls))
			for _, tc := range am.ToolCalls {
				calls = append(calls, tc.ToOpenAIFormat())
			}
			msgDict["tool_calls"] = calls
		}
		if am.ReasoningContent != "" {
			msgDict["reasoning_content"] = am.ReasoningContent
		}
	}
	// ToolMessage 特有字段
	if tm, ok := msg.(*llmschema.ToolMessage); ok {
		if tm.ToolCallID != "" {
			msgDict["tool_call_id"] = tm.ToolCallID
		}
	}
	return msgDict, nil
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/llm/model_clients/...`

- [ ] **Step 4: 适配 model_clients 测试**

修复 `messages_param_test.go` 中的 `TestToBaseMessage`：
- `toBaseMessage` 返回值从 `*llmschema.BaseMessage` 变为 `llmschema.BaseMessage`
- 验证方式从 `baseMsg.Role` 改为 `baseMsg.GetRole()`

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/llm/model_clients/... -v -count=1`

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/
git commit -m "refactor: model_clients 适配 BaseMessage 接口"
```

---

## Task 5: prompt 包适配

**Files:**
- Modify: `internal/agentcore/foundation/prompt/template.go`
- Modify: `internal/agentcore/foundation/prompt/assembler.go`

- [ ] **Step 1: 修改 template.go**

核心变更：
- `[]*schema.BaseMessage` → `[]schema.BaseMessage`
- `ToMessages()` 返回类型从 `([]*schema.BaseMessage, error)` 改为 `([]schema.BaseMessage, error)`
- `&um.BaseMessage` → 直接使用 `um`（UserMessage 已实现 BaseMessage 接口）
- `deepCopyContent()` 中 `case []*schema.BaseMessage` → `case []schema.BaseMessage`

- [ ] **Step 2: 修改 assembler.go**

核心变更：
- `[]*schema.BaseMessage` → `[]schema.BaseMessage`
- `assignMessageContent(msg *schema.BaseMessage, ...)` → `assignMessageContent(msg schema.BaseMessage, ...)`
- 方法内部 `msg.Content = ...` → `msg.SetContent(...)`
- `buildFormatterList` 和 `format` 中 `case []*schema.BaseMessage` → `case []schema.BaseMessage`
- 字段访问 `msg.Content.IsText()` → `msg.GetContent().IsText()` 等
- `deepCopyMessages` 需要重写——接口类型不能直接 JSON 序列化/反序列化

**deepCopyMessages 新实现策略**：遍历消息列表，对每条消息做 JSON 序列化/反序列化深拷贝，使用 `UnmarshalMessage` 工厂函数（Task 8 中实现）。在此 Task 中先使用临时方案：

```go
// deepCopyMessages 深拷贝消息列表。
func deepCopyMessages(msgs []schema.BaseMessage) ([]schema.BaseMessage, error) {
	result := make([]schema.BaseMessage, 0, len(msgs))
	for _, msg := range msgs {
		data, err := json.Marshal(msg)
		if err != nil {
			return nil, fmt.Errorf("深拷贝消息失败: %w", err)
		}
		copied, err := schema.UnmarshalMessage(data)
		if err != nil {
			return nil, fmt.Errorf("深拷贝消息反序列化失败: %w", err)
		}
		result = append(result, copied)
	}
	return result, nil
}
```

由于 `UnmarshalMessage` 尚未实现，此 Task 先在 `schema` 包中添加一个最小化的 `UnmarshalMessage`（只处理 user/system/assistant/tool 四种 role），Task 8 再扩展支持 Offload。

- [ ] **Step 3: 在 schema/message.go 中添加 UnmarshalMessage 最小实现**

```go
// UnmarshalMessage 从 JSON 反序列化为对应消息类型。
// 根据 role 字段自动分派：
//   - "user" → *UserMessage
//   - "system" → *SystemMessage
//   - "assistant" → *AssistantMessage
//   - "tool" → *ToolMessage
func UnmarshalMessage(data []byte) (BaseMessage, error) {
	var peek struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("UnmarshalMessage 解析 role 失败: %w", err)
	}
	switch peek.Role {
	case "user":
		var m UserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 UserMessage 失败: %w", err)
		}
		return &m, nil
	case "system":
		var m SystemMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 SystemMessage 失败: %w", err)
		}
		return &m, nil
	case "assistant":
		var m AssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 AssistantMessage 失败: %w", err)
		}
		return &m, nil
	case "tool":
		var m ToolMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalMessage 反序列化 ToolMessage 失败: %w", err)
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("UnmarshalMessage 不支持的角色: %q", peek.Role)
	}
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/prompt/...`

- [ ] **Step 5: 适配 prompt 测试**

修复 `template_test.go` 和 `assembler_test.go`：
- `[]*schema.BaseMessage{&um.BaseMessage, am}` → `[]schema.BaseMessage{um, am}`
- `schema.NewBaseMessage(...)` → `schema.NewDefaultMessage(...)`
- `result.([]*schema.BaseMessage)` → `result.([]schema.BaseMessage)`
- 字段访问 `msgs[0].Content.Text()` → `msgs[0].GetContent().Text()`（通过接口访问时）

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/prompt/... -v -count=1`

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/foundation/prompt/ internal/agentcore/foundation/llm/schema/message.go
git commit -m "refactor: prompt 包适配 BaseMessage 接口 + UnmarshalMessage 最小实现"
```

---

## Task 6: store/message + memory 包适配

**Files:**
- Modify: `internal/agentcore/foundation/store/db/base_message_store.go`
- Modify: `internal/agentcore/memory/manage/model/message_manager.go`
- Modify: `internal/agentcore/memory/manage/model/sql_message_store.go`

- [ ] **Step 1: 修改 BaseMessageStore 接口签名**

`base_message_store.go` 中：
- `*schema.BaseMessage` → `schema.BaseMessage`
- `GetMessageByID` 返回值、`MessageAdd.Message`、`MessageAndMeta.Message` 字段类型

- [ ] **Step 2: 修改 MessageManager**

`message_manager.go` 中：
- `schema.NewBaseMessage(role, req.Content)` → `schema.NewDefaultMessage(role, req.Content)`

- [ ] **Step 3: 修改 SqlMessageStore**

`sql_message_store.go` 中：
- `GetMessageByID` 和 `rowToMessageAndMeta` 返回值从 `*schema.BaseMessage` 改为 `schema.BaseMessage`
- `rowToMessageAndMeta` 中 `&schema.BaseMessage{Role: role, Content: content}` 改为按 role 构造具体类型：

```go
func (s *SqlMessageStore) rowToMessageAndMeta(row map[string]any) (schema.BaseMessage, *MessageMetadata, error) {
    // ... 解析 role 字符串 ...
    var msg schema.BaseMessage
    switch role {
    case schema.RoleTypeUser:
        msg = schema.NewUserMessage(contentStr)
    case schema.RoleTypeSystem:
        msg = schema.NewSystemMessage(contentStr)
    case schema.RoleTypeAssistant:
        msg = schema.NewAssistantMessage(contentStr)
    case schema.RoleTypeTool:
        // ToolMessage 需要 tool_call_id，从 row 中提取
        msg = schema.NewToolMessage(toolCallID, contentStr)
    default:
        msg = schema.NewDefaultMessage(role, contentStr)
    }
    // ...
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/foundation/store/... ./internal/agentcore/memory/...`

- [ ] **Step 5: 适配相关测试**

修复 `base_message_store_test.go`、`sql_message_store_test.go`、`message_manager_test.go` 中的引用。

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/store/... ./internal/agentcore/memory/... -v -count=1`

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/foundation/store/ internal/agentcore/memory/
git commit -m "refactor: store/message + memory 适配 BaseMessage 接口"
```

---

## Task 7: context_engine 包适配

**Files:**
- Modify: `internal/agentcore/context_engine/base.go`
- Modify: `internal/agentcore/context_engine/token/base.go`

- [ ] **Step 1: 修改 context_engine/base.go**

核心变更：所有 `[]*llm_schema.BaseMessage` → `[]llm_schema.BaseMessage`

涉及：
- `ModelContext` 接口的 `GetMessages`/`SetMessages`/`PopMessages`/`AddMessages`/`GetContextWindow`
- `ContextWindow` 结构体的 `SystemMessages`/`ContextMessages` 字段
- `NewContextWindow()` 构造函数
- `GetMessages()`/`GetTools()` 方法
- `StatMessages`/`StatTools`/`StatContextWindow` 预留方法签名

- [ ] **Step 2: 修改 context_engine/token/base.go**

`TokenCounter` 接口中 `[]*llm_schema.BaseMessage` → `[]llm_schema.BaseMessage`

- [ ] **Step 3: 适配 context_engine 测试**

修复 `base_test.go`：
- `[]*llm_schema.BaseMessage{sysMsg}` → `[]llm_schema.BaseMessage{sysMsg}`
- `llm_schema.NewBaseMessage(...)` → `llm_schema.NewDefaultMessage(...)`

- [ ] **Step 4: 编译验证 + 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/... -v -count=1`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/
git commit -m "refactor: context_engine 适配 BaseMessage 接口"
```

---

## Task 8: 其他文件适配 + 全项目编译验证

**Files:**
- Modify: `internal/agentcore/session/tracer/decorator_test.go`
- 其他可能有遗漏的文件

- [ ] **Step 1: 修复 decorator_test.go**

`BaseMessage: llmschema.BaseMessage{Content: ...}` → `DefaultMessage: llmschema.DefaultMessage{Content: ...}`

- [ ] **Step 2: 全项目编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

修复所有剩余编译错误。

- [ ] **Step 3: 全项目测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/... -count=1`

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "refactor: 完成全项目 BaseMessage 接口化适配"
```

---

## Task 9: Offload 消息模型实现

**Files:**
- Create: `internal/agentcore/context_engine/schema/offload.go`
- Create: `internal/agentcore/context_engine/schema/offload_test.go`
- Modify: `internal/agentcore/context_engine/schema/doc.go`

前置条件：Task 1-8 已完成，BaseMessage 接口化改造完毕。

- [ ] **Step 1: 编写 offload.go — OffloadInfo + Offloadable 接口**

```go
package schema

import (
	"encoding/json"
	"fmt"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// Offloadable 卸载消息接口，等价 Python isinstance(msg, OffloadMixin)。
//
// 4 个 Offload 子类型均实现此接口，用于防重复卸载检查：
//
//	_, ok := msg.(Offloadable)
//
// 对应 Python: isinstance(message, OffloadMixin)
type Offloadable interface {
	llm_schema.BaseMessage
	// GetOffloadInfo 获取卸载元数据
	GetOffloadInfo() OffloadInfo
}

// ──────────────────────────── 结构体 ────────────────────────────

// OffloadInfo 卸载元数据，等价 Python OffloadMixin。
//
// 当对话消息被卸载到外部存储时，OffloadInfo 携带检索句柄和存储后端类型，
// 使 LLM 可以通过 reloader_tool 按需取回原始内容。
//
// 对应 Python: openjiuwen/core/context_engine/schema/messages.py (OffloadMixin)
type OffloadInfo struct {
	// OffloadType 存储后端类型："in_memory" 或 "filesystem"
	OffloadType string `json:"offload_type"`
	// OffloadHandle 唯一检索标识：内存模式为 UUID hex，文件系统模式为文件路径
	OffloadHandle string `json:"offload_handle"`
	// Metadata 附加元数据（原始 token 数、时间戳、内容类型等）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// OffloadUserMessage 卸载的用户消息占位符。
//
// 对应 Python: OffloadUserMessage(UserMessage, OffloadMixin)
type OffloadUserMessage struct {
	llm_schema.UserMessage
	OffloadInfo
}

// OffloadAssistantMessage 卸载的助手消息占位符。
//
// 对应 Python: OffloadAssistantMessage(AssistantMessage, OffloadMixin)
type OffloadAssistantMessage struct {
	llm_schema.AssistantMessage
	OffloadInfo
}

// OffloadSystemMessage 卸载的系统消息占位符。
//
// 对应 Python: OffloadSystemMessage(SystemMessage, OffloadMixin)
type OffloadSystemMessage struct {
	llm_schema.SystemMessage
	OffloadInfo
}

// OffloadToolMessage 卸载的工具消息占位符。
//
// 对应 Python: OffloadToolMessage(ToolMessage, OffloadMixin)
type OffloadToolMessage struct {
	llm_schema.ToolMessage
	OffloadInfo
}
```

- [ ] **Step 2: 编写 GetOffloadInfo 实现**

```go
// GetOffloadInfo 实现 Offloadable 接口
func (m *OffloadUserMessage) GetOffloadInfo() OffloadInfo      { return m.OffloadInfo }
func (m *OffloadAssistantMessage) GetOffloadInfo() OffloadInfo  { return m.OffloadInfo }
func (m *OffloadSystemMessage) GetOffloadInfo() OffloadInfo     { return m.OffloadInfo }
func (m *OffloadToolMessage) GetOffloadInfo() OffloadInfo       { return m.OffloadInfo }
```

- [ ] **Step 3: 编写构造函数 + 工厂函数**

```go
// NewOffloadUserMessage 创建卸载的用户消息
func NewOffloadUserMessage(content, handle, offloadType string, opts ...llm_schema.MessageOption) *OffloadUserMessage {
	msg := llm_schema.NewUserMessage(content, opts...)
	return &OffloadUserMessage{
		UserMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadSystemMessage 创建卸载的系统消息
func NewOffloadSystemMessage(content, handle, offloadType string, opts ...llm_schema.MessageOption) *OffloadSystemMessage {
	msg := llm_schema.NewSystemMessage(content, opts...)
	return &OffloadSystemMessage{
		SystemMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadToolMessage 创建卸载的工具消息
func NewOffloadToolMessage(toolCallID, content, handle, offloadType string, opts ...llm_schema.MessageOption) *OffloadToolMessage {
	msg := llm_schema.NewToolMessage(toolCallID, content, opts...)
	return &OffloadToolMessage{
		ToolMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadAssistantMessage 创建卸载的助手消息
func NewOffloadAssistantMessage(content, handle, offloadType string, opts ...llm_schema.AssistantMessageOption) *OffloadAssistantMessage {
	msg := llm_schema.NewAssistantMessage(content, opts...)
	return &OffloadAssistantMessage{
		AssistantMessage: *msg,
		OffloadInfo: OffloadInfo{
			OffloadType:   offloadType,
			OffloadHandle: handle,
		},
	}
}

// NewOffloadMessage 工厂函数，根据 role 自动分派创建对应 Offload 子类型。
// 等价 Python: create_offload_message(role, content, offload_handle, offload_type, **kwargs)
func NewOffloadMessage(role llm_schema.RoleType, content, handle, offloadType string, opts ...llm_schema.MessageOption) Offloadable {
	switch role {
	case llm_schema.RoleTypeAssistant:
		// AssistantMessage 使用 AssistantMessageOption，此处使用默认构造
		return NewOffloadAssistantMessage(content, handle, offloadType)
	case llm_schema.RoleTypeTool:
		return NewOffloadToolMessage("", content, handle, offloadType, opts...)
	case llm_schema.RoleTypeSystem:
		return NewOffloadSystemMessage(content, handle, offloadType, opts...)
	default:
		return NewOffloadUserMessage(content, handle, offloadType, opts...)
	}
}
```

- [ ] **Step 4: 编写 IsOffloaded 辅助函数**

```go
// IsOffloaded 检查消息是否为已卸载的占位符。
// 等价 Python: isinstance(message, OffloadMixin)，用于处理器防重复卸载。
func IsOffloaded(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(Offloadable)
	return ok
}
```

- [ ] **Step 5: 编写 OffloadAssistantMessage 自定义序列化**

由于 `AssistantMessage` 已有自定义 `MarshalJSON`/`UnmarshalJSON`，嵌入它的 `OffloadAssistantMessage` 必须自行实现序列化：

```go
// MarshalJSON 实现 json.Marshaler 接口，合并 AssistantMessage 和 OffloadInfo 字段。
func (m *OffloadAssistantMessage) MarshalJSON() ([]byte, error) {
	// 序列化内层 AssistantMessage（调用其自定义 MarshalJSON）
	inner, err := json.Marshal(&m.AssistantMessage)
	if err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 序列化内层失败: %w", err)
	}
	// 序列化 OffloadInfo
	outer, err := json.Marshal(&m.OffloadInfo)
	if err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 序列化 OffloadInfo 失败: %w", err)
	}
	// 合并两个 JSON 对象
	var baseMap, extraMap map[string]json.RawMessage
	if err := json.Unmarshal(inner, &baseMap); err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 合并解析内层失败: %w", err)
	}
	if err := json.Unmarshal(outer, &extraMap); err != nil {
		return nil, fmt.Errorf("OffloadAssistantMessage 合并解析 OffloadInfo 失败: %w", err)
	}
	for k, v := range extraMap {
		baseMap[k] = v
	}
	return json.Marshal(baseMap)
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，解析 AssistantMessage 和 OffloadInfo 字段。
func (m *OffloadAssistantMessage) UnmarshalJSON(data []byte) error {
	// 先反序列化 AssistantMessage 部分
	if err := json.Unmarshal(data, &m.AssistantMessage); err != nil {
		return fmt.Errorf("OffloadAssistantMessage 反序列化内层失败: %w", err)
	}
	// 再反序列化 OffloadInfo 部分
	if err := json.Unmarshal(data, &m.OffloadInfo); err != nil {
		return fmt.Errorf("OffloadAssistantMessage 反序列化 OffloadInfo 失败: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: 编写 UnmarshalOffloadMessage 和扩展 UnmarshalMessage**

```go
// UnmarshalOffloadMessage 从 JSON 反序列化为对应 Offload 子类型。
// 根据 role 字段自动分派。
func UnmarshalOffloadMessage(data []byte) (Offloadable, error) {
	var peek struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("UnmarshalOffloadMessage 解析 role 失败: %w", err)
	}
	switch peek.Role {
	case "user":
		var m OffloadUserMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadUserMessage 失败: %w", err)
		}
		return &m, nil
	case "system":
		var m OffloadSystemMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadSystemMessage 失败: %w", err)
		}
		return &m, nil
	case "assistant":
		var m OffloadAssistantMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadAssistantMessage 失败: %w", err)
		}
		return &m, nil
	case "tool":
		var m OffloadToolMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("UnmarshalOffloadMessage 反序列化 OffloadToolMessage 失败: %w", err)
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("UnmarshalOffloadMessage 不支持的角色: %q", peek.Role)
	}
}
```

同时在 `schema/message.go` 中的 `UnmarshalMessage` 扩展支持 Offload 类型：

```go
func UnmarshalMessage(data []byte) (BaseMessage, error) {
	var peek struct {
		Role        string `json:"role"`
		OffloadType string `json:"offload_type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, fmt.Errorf("UnmarshalMessage 解析 role 失败: %w", err)
	}
	// 如果包含 offload_type 字段，使用 Offload 反序列化
	if peek.OffloadType != "" {
		return context_engine_schema.UnmarshalOffloadMessage(data)
	}
	// ... 原有 switch 逻辑不变 ...
}
```

注意：此处 `UnmarshalMessage` 在 `llm/schema` 包中，而 `UnmarshalOffloadMessage` 在 `context_engine/schema` 包中。由于 `llm/schema` 不能导入 `context_engine/schema`（会产生循环依赖），需要调整设计——将 `UnmarshalMessage` 移到更高层，或在 `UnmarshalMessage` 中仅处理 4 种基本类型，Offload 的反序列化由 `context_engine/schema` 包单独提供。

**最终决策**：`UnmarshalMessage` 留在 `llm/schema` 包中，只处理 4 种基本消息类型。Offload 反序列化由 `context_engine/schema.UnmarshalOffloadMessage` 单独提供。调用方根据业务场景选择使用哪个函数。

- [ ] **Step 7: 编写完整测试文件 offload_test.go**

测试覆盖：
- 4 个 Offload 子类型的构造和字段验证
- `NewOffloadMessage` 工厂函数各 role 分支
- `IsOffloaded` 对 Offload 和非 Offload 消息的判断
- `Offloadable` 接口断言（`msg.(Offloadable)`）
- `BaseMessage` 接口兼容性（`var m llm_schema.BaseMessage = offloadMsg`）
- OffloadUserMessage/OffloadSystemMessage/OffloadToolMessage JSON 往返
- OffloadAssistantMessage JSON 往返（含 ToolCalls/OffloadInfo）
- `UnmarshalOffloadMessage` 各 role 分支

- [ ] **Step 8: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/schema/... -v -count=1`

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/context_engine/schema/
git commit -m "feat: 实现 Offload 消息模型（OffloadInfo + 4 子类型 + Offloadable 接口 + 序列化 + 工厂函数）"
```

---

## Task 10: 更新文档 + IMPLEMENTATION_PLAN.md 状态同步

**Files:**
- Modify: `internal/agentcore/foundation/llm/schema/doc.go`
- Modify: `internal/agentcore/foundation/prompt/doc.go`
- Modify: `internal/agentcore/foundation/store/db/doc.go`
- Modify: `internal/agentcore/memory/manage/model/doc.go`
- Modify: `internal/agentcore/context_engine/schema/doc.go`
- Modify: `internal/agentcore/context_engine/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 schema/doc.go**

更新消息体系描述，反映 BaseMessage 接口 + DefaultMessage 默认实现的新架构。

- [ ] **Step 2: 更新其他 doc.go**

更新 prompt/doc.go 中的示例代码（`[]*schema.BaseMessage` → `[]schema.BaseMessage`）。
更新 store/db/doc.go 和 memory/manage/model/doc.go 中的类型描述。
更新 context_engine/schema/doc.go 添加 offload.go 条目。
更新 context_engine/doc.go 的文件目录。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 5.18 状态从 `☐` 改为 `✅`，补充说明：
- ✅ BaseMessage 接口化（struct→interface + DefaultMessage 默认实现）
- ✅ Offload 消息模型（OffloadInfo + 4 个子类型 + Offloadable 接口）
- ✅ NewOffloadMessage 工厂函数 + IsOffloaded 辅助函数
- ✅ OffloadAssistantMessage 自定义序列化
- ✅ UnmarshalMessage 通用消息反序列化 + UnmarshalOffloadMessage Offload 专用反序列化

- [ ] **Step 4: 全项目编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test ./internal/agentcore/... -count=1`

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "docs: 更新文档 + IMPLEMENTATION_PLAN.md 5.18 状态同步"
```
