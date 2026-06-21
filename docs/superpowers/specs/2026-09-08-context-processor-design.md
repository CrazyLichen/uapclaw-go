# 5.21 ContextProcessor 插件基类设计

## 概述

实现 `ContextProcessor` 接口和 `BaseProcessor` 结构体，为所有上下文处理器（压缩器 5.22-5.26、卸载器 5.27-5.29）提供统一契约和默认实现。这是 5.21-5.31 处理器链的骨架。

### 在 Agent 会话流程中的位置

```
用户输入 → Session → ModelContext.AddMessages()
                              │
                              ▼
                    ┌─── Trigger 链 ───┐
                    │ Processor.trigger_add_messages()     │
                    │ 哪些处理器需要介入？                 │
                    └──────────────────────────────────────┘
                              │
                              ▼
                    ┌─── OnAddMessages 链 ───┐
                    │ Processor.on_add_messages()          │
                    │ 消息变换/过滤/压缩                  │
                    └──────────────────────────────────────┘
                              │
                              ▼
                    消息写入 ModelContext
                              │
                              ▼
              LLM 推理 → ModelContext.GetContextWindow()
                              │
                              ▼
                    ┌─── Trigger 链 ───┐
                    │ Processor.trigger_get_context_window()│
                    │ 哪些处理器需要介入？                 │
                    └──────────────────────────────────────┘
                              │
                              ▼
                    ┌─── OnGetContextWindow 链 ───┐
                    │ Processor.on_get_context_window()    │
                    │ 上下文压缩/卸载/重排                │
                    └──────────────────────────────────────┘
                              │
                              ▼
                    返回最终 ContextWindow → LLM
```

### 作用

1. **双钩子模式**：在消息添加前（`OnAddMessages`）和上下文窗口返回前（`OnGetContextWindow`）两个生命周期点提供拦截能力
2. **Trigger→Action 分离**：先轻量判断是否需要介入，再执行重操作，避免每次都做昂贵的 LLM 调用
3. **状态可持久化**：通过 `SaveState`/`LoadState` 支持跨会话恢复处理器状态
4. **通用卸载能力**：基类提供 `OffloadMessages` 方法族，子类复用即可将大消息移到文件系统/内存
5. **用量追踪**：`RecordCompressionUsage` 统一记录压缩 LLM 调用的 token 消耗

## 决策汇总

| 决策点 | 选择 | 理由 |
|-------|------|------|
| Processor 类型标识 | 接口方法 `ProcessorType() string` | 最简洁，Go 惯用方式 |
| Offload 方法族归属 | 放在 BaseProcessor 中 | 与 Python 对齐，所有处理器继承 |
| 状态持久化类型 | `map[string]any` | 与 Python `Dict[str, Any]` 完全对齐 |
| Compression 用量追踪 | 放在 BaseProcessor 中 | 与 Python 对齐，Compressor 子类使用，Offloader 忽略无副作用 |
| Processor 表达方式 | 接口 + BaseProcessor 结构体 | 兼顾接口抽象和默认实现复用 |
| ProcessorType 返回值 | Go 结构体名字符串 | 与 Python 类名对齐，便于跨语言对照排查 |
| IsAPIRound 方法 | 放在 BaseProcessor 上 | 与 Python 对齐 |
| 文件组织 | 按职责拆多文件 | 职责清晰，每个文件聚焦一个关注点 |

## 接口设计

### ContextProcessor 接口

```go
// ContextProcessor 上下文处理器接口，所有处理器插件必须实现。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages      — 消息即将被添加时
//  2. OnGetContextWindow  — 上下文窗口即将返回时
//
// 每个处理器通过 Trigger* 方法判断是否介入，仅在返回 true 时
// 才调用对应的 On* 方法执行实际处理。实现必须是无状态的，
// 或通过 SaveState/LoadState 支持跨会话恢复。
type ContextProcessor interface {
    // OnAddMessages 处理即将添加的消息，返回 ContextEvent 和变换后的消息列表。
    // 仅在 TriggerAddMessages 返回 true 时调用。
    OnAddMessages(ctx context.Context, mc ModelContext, messages []llm_schema.BaseMessage, opts ...Option) (*ContextEvent, []llm_schema.BaseMessage, error)

    // OnGetContextWindow 处理即将返回的上下文窗口，返回 ContextEvent 和变换后的窗口。
    // 仅在 TriggerGetContextWindow 返回 true 时调用。
    OnGetContextWindow(ctx context.Context, mc ModelContext, cw ContextWindow, opts ...Option) (*ContextEvent, ContextWindow, error)

    // TriggerAddMessages 判断是否需要介入消息添加。每次 AddMessages 调用均执行，必须轻量。
    TriggerAddMessages(ctx context.Context, mc ModelContext, messages []llm_schema.BaseMessage, opts ...Option) (bool, error)

    // TriggerGetContextWindow 判断是否需要介入上下文窗口获取。每次 GetContextWindow 调用均执行，必须轻量。
    TriggerGetContextWindow(ctx context.Context, mc ModelContext, cw ContextWindow, opts ...Option) (bool, error)

    // SaveState 导出处理器内部状态为可序列化的 map。
    SaveState() map[string]any

    // LoadState 从 map 恢复处理器内部状态。
    LoadState(state map[string]any)

    // ProcessorType 返回处理器类型标识字符串（Go 结构体名）。
    ProcessorType() string
}
```

### ProcessorConfig 接口

```go
// ProcessorConfig 处理器配置接口，所有处理器配置必须实现。
//
// 各具体处理器定义自己的 Config 结构体并实现此接口，
// 基类通过接口持有配置，子类通过类型断言获取具体配置。
type ProcessorConfig interface {
    // Validate 校验配置参数
    Validate() error
}
```

### ProcessorOption 与 Option 模式

```go
// ProcessorOption 处理器可选参数，替代 Python **kwargs
type ProcessorOption struct {
    // SysOperation 系统操作接口
    // ⤵️ 9.32 回填：将 any 替换为 SysOperation 接口类型
    SysOperation any
    // OffloadHandle 卸载句柄，未指定时自动生成 UUID
    OffloadHandle string
    // OffloadType 卸载类型："filesystem" 或 "in_memory"
    OffloadType string
    // OffloadPath 卸载文件路径，未指定时自动生成
    OffloadPath string
    // Extra 额外参数
    Extra map[string]any
}

// Option 处理器选项函数类型
type Option func(*ProcessorOption)

func WithSysOperation(op any) Option           // ⤵️ 9.32 回填参数类型
func WithOffloadHandle(handle string) Option
func WithOffloadType(offloadType string) Option
func WithOffloadPath(path string) Option
func WithExtra(key string, value any) Option
```

### BaseProcessor 结构体

```go
// BaseProcessor 上下文处理器基类，提供所有处理器的默认实现。
//
// 具体处理器嵌入此结构体，只需覆写感兴趣的钩子方法。
// 默认行为：Trigger* 返回 false（不触发），On* 透传输入，
// SaveState/LoadState 空操作。
type BaseProcessor struct {
    // config 处理器配置，各子类实现 ProcessorConfig 接口
    config ProcessorConfig
    // compressionUsage 压缩调用用量追踪
    compressionUsage map[string]any
}

func NewBaseProcessor(config ProcessorConfig) *BaseProcessor
func (p *BaseProcessor) Config() ProcessorConfig
```

## 文件组织

```
processor/
├── doc.go              # 包文档
├── base.go             # ContextProcessor 接口 + ProcessorConfig 接口 + ContextEvent 结构体
│                       # + BaseProcessor 结构体 + ProcessorOption/Option + 构造函数
├── hooks.go            # BaseProcessor 的 4 个钩子默认实现 + ProcessorType + IsAPIRound
├── state.go            # BaseProcessor 的 SaveState/LoadState 默认实现
├── offload.go          # OffloadMessages 方法族 + offload 常量 + GenerateOffloadPath
│                       # + offloadMessagesToMemory + offloadMessagesToFilesystem
│                       # + writeOffloadToFile
├── usage.go            # CompressionUsage 追踪方法族
│                       # + ResetCompressionUsage/RecordCompressionUsage
│                       # + CurrentCompressionUsage/ExtractUsageMetadata/MergeCompressionUsage
├── round.go            # GroupCompletedAPIRounds 包级导出函数
└── base_test.go        # 测试
```

### 各文件详细内容

#### base.go — 核心定义

```
结构体区块：ProcessorConfig、ContextProcessor、ContextEvent、ProcessorOption、BaseProcessor
导出函数：NewBaseProcessor、NewContextEvent、Config()
          Option 类型 + With* 选项函数 + newProcessorOption
```

#### hooks.go — 钩子默认实现

```
导出函数：
  - ProcessorType() string               → 默认返回空字符串，子类覆写
  - OnAddMessages(...)                   → 透传消息，返回 (nil, messages, nil)
  - OnGetContextWindow(...)              → 透传窗口，返回 (nil, cw, nil)
  - TriggerAddMessages(...)              → 返回 (false, nil)
  - TriggerGetContextWindow(...)          → 返回 (false, nil)
  - IsAPIRound(messages) bool            → 调用 GroupCompletedAPIRounds
```

#### state.go — 状态持久化

```
导出函数：
  - SaveState() map[string]any           → 默认返回空 map
  - LoadState(state map[string]any)      → 默认空操作
```

#### offload.go — 卸载方法族

```
常量：
  - offloadMessageHandle       = "[[OFFLOAD: handle=%s, type=%s]]"
  - offloadMessageHandleWithPath = "[[OFFLOAD: type=%s, path=%s]]"

导出函数：
  - OffloadMessages(ctx, role, content, messages, opts...) → 核心卸载入口
  - GenerateOffloadPath(workspaceDir, sessionID, handle) string

非导出函数：
  - offloadMessagesToMemory(...)         → 卸载到内存
    ⤵️ 5.31 回填：需 ModelContext.OffloadMessages(handle, messages) 方法
  - offloadMessagesToFilesystem(...)     → 卸载到文件系统
  - writeOffloadToFile(...)              → 写入 JSON 文件
    ⤵️ 9.32 回填：优先使用 SysOperation 异步写，移除 os 兜底路径
```

OffloadMessages 核心流程：

```
OffloadMessages(role, content, messages, opts)
  │
  ├─ messages 为空 → 返回 nil, nil
  │
  ├─ 未指定 OffloadHandle → 自动生成 UUID
  │
  ├─ offloadType == "in_memory"
  │   └─ offloadMessagesToMemory()
  │       ├─ 拼接 content + offloadMessageHandle
  │       ├─ 调用 mc.OffloadMessages(handle, messages) 存入内存
  │       └─ NewOffloadMessage() 创建 OffloadAssistantMessage 返回
  │
  └─ offloadType == "filesystem"（默认）
      ├─ GenerateOffloadPath() 生成文件路径
      ├─ writeOffloadToFile() 写入 JSON 文件
      │   ├─ 成功 → offloadMessagesToFilesystem() 创建带 path 的 OffloadMessage
      │   └─ 失败 → fallback 到 offloadMessagesToMemory()
      └─ 返回 OffloadMessage
```

#### usage.go — 用量追踪

```
导出函数：
  - ResetCompressionUsage()                          → 重置为 nil
  - RecordCompressionUsage(response *AssistantMessage) → 提取并合并用量
  - CurrentCompressionUsage() map[string]any          → 返回副本
  - ExtractUsageMetadata(msg *AssistantMessage) map[string]any  → 包级函数
  - MergeCompressionUsage(left, right map[string]any) map[string]any → 包级函数
```

ExtractUsageMetadata 实现（零反射，直接访问 `UsageMetadata` 结构体字段）：

```go
func ExtractUsageMetadata(msg *llm_schema.AssistantMessage) map[string]any {
    if msg == nil || msg.UsageMetadata == nil {
        return nil
    }
    um := msg.UsageMetadata
    return map[string]any{
        "calls":         1,
        "input_tokens":  um.InputTokens,
        "output_tokens": um.OutputTokens,
        "total_tokens":  um.TotalTokens,
        "cache_tokens":  um.CacheTokens,
        "input_cost":    um.InputCost,
        "output_cost":   um.OutputCost,
        "total_cost":    um.TotalCost,
        "model_name":    um.ModelName,
        "details":       []map[string]any{usageMetadataToMap(um)},
    }
}
```

MergeCompressionUsage 合并规则（与 Python 对齐）：
- `calls`, `input_tokens`, `output_tokens`, `total_tokens`, `cache_tokens` → 累加（int）
- `input_cost`, `output_cost`, `total_cost` → 累加（float64）
- `model_name` → 取 left 非空值，否则取 right
- `details` → 追加合并

#### round.go — API 轮次分组

```go
// GroupCompletedAPIRounds 将消息列表按已完成的 API 轮次分组，
// 返回每个轮次的 [start, end) 半开区间列表。
//
// 核心逻辑：
//   - 遇到 AssistantMessage 不含 tool_calls → 一轮完成
//   - 遇到 AssistantMessage 含 tool_calls → 收集 ID，等待 ToolMessage 回复
//   - 所有 pending tool_call_id 收到回复 → 一轮完成
//   - 遇到 UserMessage 且无 pending → 开始新轮次
func GroupCompletedAPIRounds(messages []llm_schema.BaseMessage) [][2]int
```

## 依赖关系

| 依赖项 | 当前状态 | 5.21 处理方式 | 回填章节 |
|-------|---------|-------------|---------|
| `*llm_schema.AssistantMessage` | ✅ 已实现 | ExtractUsageMetadata 参数类型 | — |
| `*llm_schema.UsageMetadata` | ✅ 已实现 | 直接访问字段，零反射 | — |
| `llm_schema.BaseMessage` 接口 | ✅ 5.18 | 钩子签名 + GroupCompletedAPIRounds | — |
| `llm_schema.ToolMessage` | ✅ 已实现 | GroupCompletedAPIRounds 类型判断 | — |
| `ModelContext` 接口 | ✅ 5.15 | 钩子签名参数 | — |
| `ContextWindow` 结构体 | ✅ 5.16 | 钩子签名参数 | — |
| `ContextEvent` 结构体 | ✅ 5.19 | 同文件，自然使用 | — |
| `NewOffloadMessage` 工厂 | ✅ 5.18 | offload.go 引用 | — |
| `Offloadable` 接口 | ✅ 5.18 | offload.go 引用 | — |
| `TokenCounter` 接口 | 🔄 5.20 | 基类 Trigger 默认 false 不依赖 | — |
| `ModelContext.OffloadMessages` | ❌ 5.31 | `offloadMessagesToMemory` 需调用 | **5.31 ⤴️** |
| `ModelContext.WorkspaceDir` | ❌ 5.31 | `GenerateOffloadPath` 需调用 | **5.31 ⤴️** |
| `SysOperation` | ❌ 9.32 | `any` 占位 + `os` 同步写兜底 | **9.32 ⤴️** |

## 回填标记汇总

| 回填来源 | 回填目标 | 内容 |
|---------|---------|------|
| 9.32 | 5.21 `ProcessorOption.SysOperation` | `any` → 具体接口类型 |
| 9.32 | 5.21 `writeOffloadToFile` | 优先使用 SysOperation 异步写，移除 os 兜底路径 |
| 5.31 | 5.21 `offloadMessagesToMemory` | ModelContext 需补充 OffloadMessages(handle, messages) 方法 |
| 5.31 | 5.21 `GenerateOffloadPath` | ModelContext 需补充 WorkspaceDir() string 方法 |

## 对应 Python 代码

`openjiuwen/core/context_engine/processor/base.py` — ContextProcessor 基类 + ContextEvent + MetaContextProcessor + offload 方法族 + usage 追踪
`openjiuwen/core/context_engine/context/session_memory_manager.py` — group_completed_api_rounds 函数

## 测试策略

- `base_test.go`：ProcessorConfig/ContextProcessor/BaseProcessor/ProcessorOption 构造与校验测试
- `hooks_test.go`：钩子默认行为测试（透传、不触发）
- `state_test.go`：SaveState/LoadState 默认行为测试
- `offload_test.go`：OffloadMessages 核心流程测试（in_memory/filesystem/fallback）、路径生成测试
- `usage_test.go`：ExtractUsageMetadata 提取测试、MergeCompressionUsage 合并测试、RecordCompressionUsage 追踪测试
- `round_test.go`：GroupCompletedAPIRounds 分组测试（纯对话/含 tool_calls/多轮混合）
