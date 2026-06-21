# 5.19 ContextEvent 设计文档

## 概述

5.19 实现上下文引擎的事件与状态类型，覆盖三个部分：

| 部分 | 类型 | Go 文件 | Python 源文件 |
|------|------|---------|--------------|
| A | ContextEvent（处理器结果） | `context_engine/processor/base.go` | `processor/base.py` |
| B | ContextCompressionState + 辅助类型 | `context_engine/schema/context_state.go` | `schema/context_state.py` |
| C | ContextCallEventType + EventData | `runner/callback/events.go` | `runner/callback/events.py` |

### 在 Agent 会话流程中的位置

```
Agent ReAct 循环
  → LLM 调用 → 工具调用 → 上下文消息增长
    → Context.add_messages()        ← 触发 on_add_processors
    → Context.get_context_window()  ← 触发 on_get_context_window
      → 各 Processor（压缩器/卸载器）执行
        → 返回 ContextEvent          ← 部分 A
      → ProcessorStateRecorder 构建 ContextCompressionState  ← 部分 B
        → emit 到 callback 框架 + session stream             ← 部分 C
    → ContextEngine 返回处理后的上下文窗口给 LLM
```

### Python 中同名不同类的区分

Python 中有两个同名的 `ContextEvent`：

1. **`processor/base.py` 的 `ContextEvent(BaseModel)`**：处理器执行结果，4 字段（event_type/messages_to_modify/compact_summary/compression_usage）→ **5.19 实现**
2. **`common/logging/events.py` 的 `ContextEvent(BaseLogEvent)`**：结构化日志事件，5 字段（message_type/message_content/message_role/context_size/max_context_size），目前无代码实际引用，属于预留设计 → **不在 5.19 实现，归入后续日志系统模块**

---

## 部分 A：ContextEvent（processor/base.go）

### 文件位置

`internal/agentcore/context_engine/processor/base.go`

5.19 只放 ContextEvent，Processor 基类留到 5.21。需要配套 `processor/doc.go`。

### 类型定义

```go
package processor

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvent 上下文处理器执行结果，由各 Processor 的 OnAddMessages / OnGetContextWindow 返回。
//
// 当处理器实际执行了操作时返回非 nil 的 ContextEvent，携带修改了哪些消息索引、
// 压缩摘要和压缩用量信息。Context 实例读取这些字段构建 ContextCompressionState。
// 处理器未触发（noop）时返回 nil。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextEvent)
type ContextEvent struct {
	// EventType 处理器类型标识（如 "DialogueCompressor"、"MessageOffloader"）
	EventType string `json:"event_type"`
	// MessagesToModify 被处理器修改的消息索引列表
	MessagesToModify []int `json:"messages_to_modify"`
	// CompactSummary 压缩摘要文本
	CompactSummary string `json:"compact_summary"`
	// CompressionUsage 压缩调用用量（token 数、费用等）
	CompressionUsage map[string]any `json:"compression_usage,omitempty"`
}
```

### 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| CompressionUsage 类型 | `map[string]any` | Python 是 `Dict[str, Any]`，Processor 基类内部也是 dict 合并逻辑；schema 包的 ContextCompressionUsage 是独立序列化模型，两者职责不同 |
| 是否需要工厂函数 | 不需要 | Go 直接用结构体字面量 `&ContextEvent{EventType: p.ProcessorType()}` 更符合惯用法 |
| 文件位置 | processor/base.go | 与 Processor 基类同文件，ContextEvent 是 Processor 的返回类型，一体两面 |

### processor/doc.go

```go
// Package processor 提供上下文处理器插件体系。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages  — 消息即将被添加时
//  2. OnGetContextWindow — 上下文窗口即将返回时
//
// 文件目录：
//
//	processor/
//	├── doc.go    # 包文档
//	└── base.go   # ContextEvent 处理器结果类型
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/
package processor
```

---

## 部分 B：ContextCompressionState 及辅助类型（schema/context_state.go）

### 文件位置

`internal/agentcore/context_engine/schema/context_state.go`

### 常量

```go
// ContextCompressionStateType 压缩状态事件类型标识。
// 用于回调事件名和 session stream 的 OutputSchema.Type 字段。
//
// 对应 Python: CONTEXT_COMPRESSION_STATE_TYPE = "context.compression_state"
const ContextCompressionStateType = "context.compression_state"
```

### 结构体

#### ContextCompressionMetric

```go
// ContextCompressionMetric 上下文压缩前后指标快照。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionMetric)
type ContextCompressionMetric struct {
	// Time 观测时间（ISO 8601 毫秒精度），空串表示未记录
	Time string `json:"time,omitempty"`
	// Messages 消息数量
	Messages int `json:"messages"`
	// Tokens Token 数量
	Tokens int `json:"tokens"`
	// ContextPercent 上下文使用百分比（0-100），0 且 omitempty 省略表示无上限
	ContextPercent int `json:"context_percent,omitempty"`
}
```

字段设计说明：
- `Time`：值类型 `string` + `omitempty`，空串 = 未记录。Python 是 `str | None`，序列化为 `null` 或 ISO 字符串。Go 中空串省略后 JSON 行为等价。
- `ContextPercent`：值类型 `int` + `omitempty`，0 省略 = 无上限。Python 是 `int | None`，实际场景中 0% 极少出现，省略即可。

#### ContextCompressionSaved

```go
// ContextCompressionSaved 上下文压缩节省量。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionSaved)
type ContextCompressionSaved struct {
	// Messages 节省的消息数
	Messages int `json:"messages"`
	// Tokens 节省的 Token 数
	Tokens int `json:"tokens"`
	// Percent 节省百分比
	Percent float64 `json:"percent"`
}
```

#### ContextCompressionUsage

```go
// ContextCompressionUsage 上下文压缩 LLM 调用用量。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionUsage)
type ContextCompressionUsage struct {
	// Calls LLM 调用次数
	Calls int `json:"calls"`
	// InputTokens 输入 Token 数
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 Token 数
	OutputTokens int `json:"output_tokens"`
	// TotalTokens 总 Token 数
	TotalTokens int `json:"total_tokens"`
	// CacheTokens 缓存 Token 数
	CacheTokens int `json:"cache_tokens"`
	// InputCost 输入费用
	InputCost float64 `json:"input_cost"`
	// OutputCost 输出费用
	OutputCost float64 `json:"output_cost"`
	// TotalCost 总费用
	TotalCost float64 `json:"total_cost"`
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// Details 每次 LLM 调用的原始用量详情
	Details []map[string]any `json:"details,omitempty"`
}
```

#### CompressionStatus / CompressionPhase

```go
// CompressionStatus 压缩操作状态字面量类型。
//
// 对应 Python: Literal["started", "completed", "noop", "skipped", "failed"]
type CompressionStatus string

const (
	CompressionStarted   CompressionStatus = "started"
	CompressionCompleted CompressionStatus = "completed"
	CompressionNoop      CompressionStatus = "noop"
	CompressionSkipped   CompressionStatus = "skipped"
	CompressionFailed    CompressionStatus = "failed"
)

// CompressionPhase 压缩操作阶段字面量类型。
//
// 对应 Python: Literal["add_messages", "get_context_window", "active_compress"]
type CompressionPhase string

const (
	PhaseAddMessages      CompressionPhase = "add_messages"
	PhaseGetContextWindow  CompressionPhase = "get_context_window"
	PhaseActiveCompress   CompressionPhase = "active_compress"
)
```

用自定义 `string` 类型 + 常量而非 `iota` 枚举，序列化时直接输出字符串值，与 Python JSON 对齐。

#### ContextCompressionState

```go
// ContextCompressionState 上下文压缩状态完整快照。
//
// 由 ProcessorStateRecorder.BuildState() 构建，记录一次压缩操作的完整生命周期。
// 通过回调框架和 session stream 发射，供外部系统观测上下文引擎行为。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionState)
type ContextCompressionState struct {
	// Type 事件类型标识，固定为 ContextCompressionStateType
	Type string `json:"type"`
	// OperationID 操作唯一标识
	OperationID string `json:"operation_id"`
	// Status 操作状态
	Status CompressionStatus `json:"status"`
	// Phase 操作阶段
	Phase CompressionPhase `json:"phase"`
	// Processor 处理器类型名称
	Processor string `json:"processor"`
	// Model 使用的 LLM 模型名称
	Model string `json:"model"`
	// Before 压缩前指标
	Before ContextCompressionMetric `json:"before"`
	// After 压缩后指标，nil 表示操作未完成或被跳过
	After *ContextCompressionMetric `json:"after,omitempty"`
	// Statistic 上下文统计快照
	Statistic contextengine.ContextStats `json:"statistic"`
	// Saved 压缩节省量，nil 表示无节省（操作未完成）
	Saved *ContextCompressionSaved `json:"saved,omitempty"`
	// CompressionUsage LLM 调用用量，nil 表示未调用 LLM
	CompressionUsage *ContextCompressionUsage `json:"compression_usage,omitempty"`
	// DurationMs 操作耗时（毫秒），0 且 omitempty 省略表示未完成
	DurationMs int `json:"duration_ms,omitempty"`
	// ContextMax 上下文窗口 Token 上限，0 且 omitempty 省略表示无上限
	ContextMax int `json:"context_max,omitempty"`
	// Summary 人类可读的操作摘要
	Summary string `json:"summary"`
	// CompactSummary 紧凑摘要（供流式输出）
	CompactSummary string `json:"compact_summary"`
	// Error 错误信息，空串表示无错误
	Error string `json:"error,omitempty"`
}
```

### 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 可选字段策略 | After/Saved 用指针；其余用值类型 + omitempty | 结构体零值和"未设置"无法区分，必须用指针；int/string 的零值在业务语义中等价于"未设置" |
| Statistic 类型 | `contextengine.ContextStats` | schema 包导入 context_engine 包（子包→父包），不形成循环（context_engine 不导入 schema） |
| CompressionStatus/Phase | 自定义 string 类型 + 常量 | 不用 iota 枚举，序列化直接输出字符串值，与 Python Literal 对齐 |
| schema→context_engine 依赖 | 允许 | 子包→父包在 Go 中合法，对齐 Python `from context_engine.base import ContextStats` |

### 需回填的 doc.go

`schema/doc.go` 文件目录新增：

```
//	schema/
//	├── doc.go              # 包文档
//	├── config.go           # ContextEngineConfig 上下文引擎配置
//	├── context_state.go    # 压缩状态模型（ContextCompressionState + 辅助类型）
//	└── offload.go          # Offload 消息模型
```

---

## 部分 C：ContextCallEventType + EventData（callback/events.go）

### 文件位置

`internal/agentcore/runner/callback/events.go`（追加到已有文件）

### 类型定义

```go
// ContextCallEventType 上下文调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents)
type ContextCallEventType string

const (
	// ContextUpdated 上下文更新事件（add_messages 后触发）
	ContextUpdated ContextCallEventType = "_framework:context_updated"
	// ContextOffloaded 上下文卸载事件（offload 后触发）
	ContextOffloaded ContextCallEventType = "_framework:context_offloaded"
	// ContextRetrieved 上下文检索事件（get_context_window 后触发）
	ContextRetrieved ContextCallEventType = "_framework:context_retrieved"
	// ContextCleared 上下文清空事件（clear 后触发）
	ContextCleared ContextCallEventType = "_framework:context_cleared"
	// ContextCompressionStateEvent 压缩状态事件（处理器执行后触发）
	ContextCompressionStateEvent ContextCallEventType = "_framework:context.compression_state"
)

// ContextCallEventData 上下文调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents) +
//              openjiuwen/core/runner/callback/framework.py (trigger kwargs)
type ContextCallEventData struct {
	// Event 事件类型
	Event ContextCallEventType
	// SessionID 会话标识
	SessionID string
	// ContextID 上下文标识
	ContextID string
	// State 压缩状态（仅 ContextCompressionStateEvent 事件有值，实际类型 *schema.ContextCompressionState）
	State any
	// Context 上下文实例引用（实际类型 context_engine.ModelContext）
	Context any
	// Extra 额外数据
	Extra map[string]any
}
```

### CallbackFramework 扩展

在 `callback/framework.go` 中追加：

```go
// ContextCallbackFunc 上下文事件回调函数类型。
type ContextCallbackFunc func(ctx context.Context, data *ContextCallEventData) any

// CallbackFramework 新增字段：
contextCallbacks map[ContextCallEventType][]ContextCallbackFunc

// 新增方法：
OnContext(event ContextCallEventType, fn ContextCallbackFunc)
OffContext(event ContextCallEventType, fn ContextCallbackFunc)
TriggerContext(ctx context.Context, data *ContextCallEventData) []any
```

实现模式与 OnLLM/OffLLM/TriggerLLM 完全一致。

### 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| State/Context 类型 | `any` | callback 是基础设施包，不应反向依赖业务包（context_engine/schema）。调用方自行类型断言。与 LLMCallEventData.Response 的 any 模式一致 |
| ContextCompressionStateEvent 命名 | 加 `Event` 后缀 | 避免与 schema 包的 `ContextCompressionState` 结构体同名冲突 |
| 事件名 | `_framework:` 前缀 | 与已有 LLM/Tool/Session 事件格式一致。注意 `context.compression_state` 带点号，与 Python 对齐 |
| 框架方法 | 5.19 一并实现 | OnContext/OffContext/TriggerContext 与类型定义同步，保持框架完整性 |

### 需回填的 doc.go

`callback/doc.go` 事件体系新增：

```
//	ContextCallEventType  — Context 生命周期事件（5 种），预定义枚举事件名
```

文件目录新增：

```
//	events.go 中追加 ContextCallEventType + ContextCallEventData
```

---

## 需回填的已有文件清单

| 文件 | 回填内容 |
|------|---------|
| `schema/doc.go` | 文件目录新增 `context_state.go` 条目 |
| `context_engine/doc.go` | 文件目录新增 `processor/` 子包条目 |
| `callback/doc.go` | 事件体系说明新增 Context 域；文件目录更新 |
| `callback/framework.go` | 新增 `contextCallbacks` 字段 + `OnContext/OffContext/TriggerContext` 方法 |
| `callback/events.go` | 追加 `ContextCallEventType` + `ContextCallEventData` + `String()` 方法 |

## 不需要回填的文件

| 文件 | 理由 |
|------|------|
| `base.go` (ContextStats) | 保持原位，schema 通过导入 context_engine 包引用 |
| `offload.go` | 无需改动 |
| `config.go` | 无需改动 |
| `IMPLEMENTATION_PLAN.md` | 5.19 状态从 ☐ 改为 ✅（实现完成后更新） |
