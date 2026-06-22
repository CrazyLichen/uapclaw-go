# 5.31 Context 实现 — SessionModelContext 全量实现设计

## 概述

5.31 实现上下文引擎的核心运行时组件 `SessionModelContext`，它是 `ModelContext` 接口的具体实现，是整个上下文引擎的"心脏"。同时实现其 5 个依赖子组件：MessageBuffer、KVCacheManager、ProcessorStateRecorder、ContextUtils（补充）、SessionMemoryManager。

全量对齐 Python `openjiuwen/core/context_engine/context/` 目录下 6 个文件。

## 流程位置与作用

```
用户请求 → Session → ContextEngine.CreateContext()
                         │
                         ▼
                  SessionModelContext ← 5.31 实现目标
                    ┌──────────────────────────────────┐
                    │ 消息存储       MessageBuffer      │
                    │ 处理器调度     ProcessorChain      │
                    │ 窗口构建       ContextWindow       │
                    │ Token 统计     Statistic           │
                    │ 消息卸载/重载  OffloadBuffer       │
                    │ 状态持久化     Save/LoadState      │
                    │ 压缩状态记录   StateRecorder       │
                    │ KV 缓存管理    KVCacheManager      │
                    │ 会话记忆       SessionMemoryMgr    │
                    └──────────────────────────────────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
         AddMessages  GetContextWindow  CompressContext
         (被动处理器)   (窗口+统计)      (主动压缩)
```

**核心作用**：消息生命周期管理、处理器链调度、上下文窗口构建、Token 统计、消息卸载/重载、状态持久化。没有它，所有已实现的处理器（8 个压缩器/卸载器）都无法实际运行。

## 设计决策

| # | 决策点 | 结论 | 理由 |
|---|--------|------|------|
| 1 | 实现范围 | 全量（6 文件） | 一步到位，完整对齐 Python |
| 2 | 并发模型 | sync.Mutex + TryLock | 对齐 Python asyncio.Lock.locked()+acquire()，CompressContext 用 TryLock 实现快速路径返回 "busy" |
| 3 | SessionMemory | 接口抽象 + 骨架实现 | direct_replace 先跑通，agent_edit 留接口桩（依赖领域6 ReActAgent） |
| 4 | 文件组织 | 单层平铺 context/ | 与 Python 目录结构 1:1 对齐 |
| 5 | ContextUtils | 放 context/context_utils.go，8 个缺失方法 + 模型映射表 | SessionModelContext 专用方法独立管理，调用 processor 包已有方法 |
| 6 | KVCacheManager | 直接用 *llm.Model，Option 透传 | Go 端已有 Model.Release/SupportsKVCacheRelease |
| 7 | ProcessorStateRecorder | 完整实现（回调+stream+历史） | Go 端已有 CallbackFramework.TriggerContext + Session.WriteStream |

## 文件清单

```
context_engine/context/
├── doc.go                        # 包文档
├── session_model_context.go      # SessionModelContext 主类
├── message_buffer.go             # ContextMessageBuffer + OffloadMessageBuffer
├── kv_cache_manager.go           # KVCacheManager
├── processor_state_recorder.go   # ProcessorStateRecorder + StateInput
├── context_utils.go              # 8 个补充方法 + 模型映射表
└── session_memory_manager.go     # SessionMemoryManager + UpdateAgent + Config
```

## 各组件详细设计

### 1. MessageBuffer (`message_buffer.go`)

**ContextMessageBuffer** — 消息缓冲区

```go
type ContextMessageBuffer struct {
    maxBufferSize      int              // 最大缓冲区大小，0 表示无限制
    contextMessages    []llm_schema.BaseMessage  // 所有消息（历史+上下文）
    historyMessagesSize int             // 历史消息数量标记位置
}
```

方法：

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `NewContextMessageBuffer(history, maxSize)` | `__init__` | 构造函数 |
| `Size() int` | `size()` | 有效消息数 |
| `AddBack(messages)` | `add_back()` | 追加消息 |
| `GetBack(size, withHistory)` | `get_back()` | 获取尾部消息 |
| `PopBack(size, withHistory)` | `pop_back()` | 弹出尾部消息 |
| `SetMessages(messages, withHistory)` | `set_messages()` | 替换消息列表 |
| `Rebuild(history)` | `rebulid()` | 从历史消息重建（保留 Python 拼写作为注释说明） |

内部方法：`ifNeedResize()` — 超 2 倍 maxBufferSize 时自动裁剪前半部分。

**OffloadMessageBuffer** — 卸载消息缓冲区

```go
type OffloadMessageBuffer struct {
    inMemoryMessages map[string][]llm_schema.BaseMessage  // 内存存储
    sysOperation     any                                    // 系统操作接口
    workspaceDir     string                                 // 工作空间目录
    sessionID        string                                 // 会话 ID
}
```

方法：

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `NewOffloadMessageBuffer(init)` | `__init__` | 构造函数 |
| `SetSysOperation(op)` | `set_sys_operation()` | 设置系统操作 |
| `SetWorkspaceInfo(dir, sid)` | `set_workspace_info()` | 设置工作空间信息 |
| `Offload(handle, offloadType, messages)` | `offload()` | 卸载消息 |
| `Reload(ctx, handle, offloadType)` | `async reload()` | 重新加载消息 |
| `reloadFromFilesystem(ctx, handle)` | `_reload_from_filesystem()` | 从文件系统加载 |
| `filesystemReloadPaths(handle)` | `_filesystem_reload_paths()` | 候选文件路径 |
| `Clear(handle, offloadType)` | `clear()` | 清除指定卸载消息 |
| `GetAll()` | `get_all()` | 返回全部内存卸载消息 |

### 2. KVCacheManager (`kv_cache_manager.go`)

```go
type KVCacheManager struct {
    sessionID          string
    lastContextWindow  *iface.ContextWindow  // 上一次窗口快照
}
```

方法：

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `NewKVCacheManager(sessionID)` | `__init__` | 构造函数 |
| `Release(ctx, window, model, opts...)` | `async release()` | 检查差异并释放 KV 缓存 |
| `checkReleaseNeeded(window)` | `_check_release_needed()` | 前缀对比找首个差异位置 |

Release 方法流程：
1. 检查 `model.SupportsKVCacheRelease()`，不支持则返回
2. `lastContextWindow == nil` 时仅保存快照，不释放
3. `checkReleaseNeeded` 对比前后窗口的消息/工具列表
4. 有差异时调用 `model.Release(ctx, WithReleaseSessionID/WithReleaseMessages/WithReleaseMessagesIdx/...)`
5. 更新 `lastContextWindow`

### 3. ProcessorStateRecorder (`processor_state_recorder.go`)

```go
type ProcessorStateInput struct {
    OperationID       string
    Status            schema.CompressionStatus
    Phase             schema.CompressionPhase
    Trigger           string
    Processor         iface.ContextProcessor
    Reason            string
    BeforeMessages    []llm_schema.BaseMessage
    AfterMessages     []llm_schema.BaseMessage
    StartedAt         time.Time
    EndedAt           time.Time
    Error             string
    MessagesToModify  []int
    Force             bool
    ContextMax        int
    CompactSummary    string
    CompressionUsage  *schema.ContextCompressionUsage
}

type ProcessorStateRecorder struct {
    sessionID      string
    contextID      string
    getSessionRef  func() *session.Session
    tokenCounter   token.TokenCounter
    historyLimit   int
    history        []map[string]any
}
```

方法：

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `NewProcessorStateRecorder(...)` | `__init__` | 构造函数 |
| `History()` | `history()` | 返回历史副本 |
| `LoadHistory(history)` | `load_history()` | 加载历史（截取最后 N 条） |
| `Emit(ctx, mc, state)` | `async emit()` | 记录+回调+stream 推送 |
| `BuildState(input)` | `build_state()` | 构建压缩状态对象 |
| `buildMetric(messages, contextMax, observedAt)` | `_build_metric()` | 构建指标 |
| `buildSaved(before, after)` | `_build_saved()` | 构建节省量 |
| `buildSummary(input)` | `_build_summary()` | 构建人类可读摘要 |
| `buildStatistic(messages)` | `_build_statistic()` | 构建按角色统计 |
| `resolveModelName(processor, trigger, force)` | `_resolve_model_name()` | 从处理器配置解析模型名 |

Emit 方法流程：
1. `_record(state)` — 追加到 history
2. 日志输出（status/phase/processor/model/summary）
3. `callback.GetCallbackFramework().TriggerContext(ContextCompressionStateEvent, ...)` — 触发回调
4. `sessionRef.WriteStream(OutputSchema{Type: ContextCompressionStateType, Payload: state})` — stream 推送

### 4. ContextUtils 补充 (`context_utils.go`)

已有方法（在 `processor/util.go` 和 `processor/round.go` 中）— 不重复实现，context 包通过 import 调用：
- `FindAllDialogueRound` → `processor.FindAllDialogueRound`
- `EstimateMessageTokens` → `processor.EstimateMessageTokens`
- `EstimateContentTokens` → `processor.EstimateContentTokens`
- `ExtractToolName` → `processor.ExtractToolName`
- `ResolveToolCallFromMessage` → `processor.ResolveToolCallFromMessage`
- `ResolveToolNameFromMessage` → `processor.ResolveToolNameFromMessage`
- `GroupCompletedAPIRounds` → `processor.GroupCompletedAPIRounds`
- `FindLastFinalAssistantIdx` → `processor.FindLastFinalAssistantIdx`
- `ReplaceMessages` → `processor.ReplaceMessages`

需要补充的 8 个方法：

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `ValidateMessages(messages)` | `validate_messages()` | 运行时消息类型校验 |
| `EnsureContextMessageIDs(messages)` | `ensure_context_message_ids()` | 给消息补上 UUID 元数据 |
| `ValidateAndFixContextWindow(window)` | `validate_and_fix_context_window()` | 移除开头连续 ToolMessage |
| `ResolveContextMax(modelName, fallback, custom)` | `resolve_context_max()` | 解析最大上下文 token |
| `IsCompressionProcessor(processor)` | `is_compression_processor()` | 判断是否为压缩类型处理器 |
| `FormatReloadedMessages(handle, messages)` | `format_reloaded_messages()` | 格式化重载消息为可读字符串 |
| `FindLastNDialogueRound(messages, n)` | `find_last_n_dialogue_round()` | 找倒数第 n 轮起始索引 |
| `FindLastAIAbsentToolCall(messages)` | `find_last_ai_message_without_tool_call()` | 最后一个无 tool_call 的 assistant 消息索引 |

常量：

```go
const (
    ContextMessageIDKey     = "context_message_id"
    DefaultContextMaxTokens = 200000
)

var ModelDefaultContextWindowTokens = map[string]int{
    "glm-5":                200000,
    "glm-4-long":           200000,
    "gpt-4o":               128000,
    "gpt-4o-mini":          128000,
    "deepseek-v3":          128000,
    "claude-opus-4.6":      1000000,
    "claude-sonnet-4.6":    1000000,
    "qwen-max":             32000,
    "qwen-plus":            131072,
    // ... 完整映射表
}
```

### 5. SessionModelContext (`session_model_context.go`)

```go
type SessionModelContext struct {
    contextID                 string
    sessionID                 string
    messageBuffer             *ContextMessageBuffer
    defaultWindowSize         int
    enableReload              bool
    contextWindowTokens       int
    modelName                 string
    modelContextWindowTokens  map[string]int
    workspace                 any
    sysOperation              any
    sessionRef                *session.Session
    defaultDialogueRound      int
    tokenCounter              token.TokenCounter
    processors                []iface.ContextProcessor
    stateRecorder             *ProcessorStateRecorder
    processorLock             sync.Mutex           // 对齐 Python asyncio.Lock
    activeCompressionInProgress bool                // 对齐 Python _active_compression_in_progress
    kvCacheManager            *KVCacheManager
    offloadMessageBuffer      *OffloadMessageBuffer
    reloaderToolCard          *tool.ToolCard
}
```

构造函数 `NewSessionModelContext`：
1. ValidateMessages + EnsureContextMessageIDs 校验历史消息
2. 创建 ContextMessageBuffer
3. 从 config 读取默认窗口大小/对话轮次/reload 配置
4. 创建 ProcessorStateRecorder
5. 条件创建 KVCacheManager
6. 创建 OffloadMessageBuffer
7. 创建 reloaderToolCard

核心方法（实现 ModelContext 接口）：

| 方法 | 并发保护 | 说明 |
|------|---------|------|
| `Len()` | 无锁 | 返回 messageBuffer.Size() |
| `GetMessages(size, withHistory)` | 无锁 | 委托 messageBuffer.GetBack |
| `SetMessages(messages, withHistory)` | 无锁 | Validate + EnsureIDs + 委托 messageBuffer |
| `PopMessages(size, withHistory)` | 无锁 | 委托 messageBuffer.PopBack |
| `ClearMessages(ctx, withHistory, opts...)` | 无锁 | PopAll + 重置 offloadBuffer + 触发 ContextCleared 事件 |
| `AddMessages(ctx, message, opts...)` | Mutex Lock | 快速路径(activeCompression)仅入队；正常路径运行处理器链 + 入队 + 触发 ContextUpdated 事件 |
| `GetContextWindow(ctx, sys, tools, ws, dr, opts...)` | Mutex Lock | 窗口构建 + GET处理器链 + KVCacheRelease + 统计 + 触发 ContextRetrieved 事件 |
| `CompressContext(ctx, opts...)` | Mutex TryLock | 快速路径返回 "busy"；正常路径运行处理器链 + 返回结果 |
| `Statistic()` | 无锁 | 委托 statMessages |
| `SessionID() / ContextID()` | 无锁 | 返回字段 |
| `TokenCounter()` | 无锁 | 返回字段 |
| `WorkspaceDir()` | 无锁 | workspace.rootPath 或 "" |
| `SetSessionRef(sess)` | 无锁 | 设置引用 |
| `OffloadMessages(handle, messages)` | 无锁 | 委托 offloadMessageBuffer.Offload |
| `SaveState()` | 无锁 | 返回 {messages, offload_messages} |
| `LoadState(state)` | 无锁 | Validate + EnsureIDs + Rebuild buffer |
| `ReloaderTool()` | 无锁 | 返回 reload 工具实例 |

内部方法：

| 方法 | 说明 |
|------|------|
| `runAddProcessors(ctx, messages, force, types, compressionOnly, opts...)` | 遍历处理器执行 trigger+on，记录状态 |
| `getAndEmitCompressionState(ctx, input)` | 构建+发射压缩状态 |
| `buildActiveCompressionResult(result, includeState, historyStart)` | 构建主动压缩返回结果 |
| `selectProcessors(types, compressionOnly)` | 筛选处理器 |
| `getWindowMessages(sysMsgs, windowSize, dialogueRound)` | 按轮次/窗口大小截取 |
| `statContextWindow(window)` | 统计窗口消息+工具+对话轮次 |
| `statMessages(stat, messages)` | 按角色统计 + token 计算 |
| `statTools(stat, tools)` | 工具 token 统计 |
| `countSingleMessageTokens(msg)` | 单消息 token 计算 |
| `countToolTokens(toolInfo)` | 单工具 token 计算 |
| `resolveContextModelName(opts...)` | 从 option 或实例解析模型名 |

AddMessages 流程（对齐 Python）：
```
1. ValidateMessages + EnsureContextMessageIDs
2. 若 activeCompressionInProgress && processorLock 被 TryLock 失败:
   → 仅入队 messageBuffer.AddBack
   → 返回
3. processorLock.Lock()
4. runAddProcessors(messages, force=false, ...)
5. messageBuffer.AddBack(result)
6. processorLock.Unlock()
7. 触发 ContextUpdated 事件
```

CompressContext 流程（对齐 Python）：
```
1. processorLock.TryLock():
   → 失败: 返回 "busy"
2. activeCompressionInProgress = true
3. selectProcessors(compressionOnly=true)
4. runAddProcessors([], force=true, compressionOnly=true, ...)
5. activeCompressionInProgress = false
6. processorLock.Unlock()
7. 返回 "compressed" / "noop"
```

GetContextWindow 流程（对齐 Python）：
```
1. 参数校验
2. processorLock.Lock()
3. 复制 systemMessages
4. enableReload → 追加 RELOADER_SYSTEM_PROMPT
5. getWindowMessages(sysMsgs, windowSize, dialogueRound)
6. 构建 ContextWindow
7. 遍历 processors: trigger_get_context_window + on_get_context_window
8. ValidateAndFixContextWindow(window)
9. kvCacheManager.Release(ctx, window, model, ...) (条件)
10. statContextWindow(window)
11. processorLock.Unlock()
12. 触发 ContextRetrieved 事件
13. 返回 window
```

### 6. SessionMemoryManager (`session_memory_manager.go`)

**SessionMemoryConfig**

```go
type SessionMemoryConfig struct {
    TriggerTokens          int              // 首次触发阈值，默认 10000
    TriggerAddTokens       int              // 增量触发阈值，默认 5000
    ToolMin                int              // 最小工具调用数，默认 3
    Model                  *llm.ModelRequestConfig   // 模型请求配置
    ModelClient            *llm.ModelClientConfig     // 模型客户端配置
    UpdateMode             string           // "agent_edit" | "direct_replace"，默认 "direct_replace"
    DirectReplaceMaxRetries int             // 最大重试次数，默认 2
}
```

**SessionMemoryUpdater 接口**（抽象 ReActAgent 依赖）

```go
// SessionMemoryUpdater 会话记忆更新器接口。
// direct_replace 模式由 SessionMemoryDirectUpdater 实现；
// agent_edit 模式 ⤵️ 6.x 回填，由 SessionMemoryAgentUpdater 实现。
type SessionMemoryUpdater interface {
    // Invoke 执行记忆更新
    Invoke(ctx context.Context, opts SessionMemoryUpdateOptions) error
    // BindModelDefaults 绑定默认模型配置
    BindModelDefaults(modelConfig *llm.ModelRequestConfig, clientConfig *llm.ModelClientConfig)
    // SetInheritedSystemPrompt 设置继承的系统提示词
    SetInheritedSystemPrompt(prompt string)
}
```

**SessionMemoryDirectUpdater** — direct_replace 模式实现

```go
type SessionMemoryDirectUpdater struct {
    config                 SessionMemoryConfig
    model                  *llm.Model     // 延迟创建
    inheritedSystemPrompt  string
}
```

方法：
- `Invoke(ctx, opts)` — 构建 prompt → 调用 model.Invoke → 规范化输出 → 写入文件
- `BindModelDefaults(...)` — 绑定默认模型配置
- `SetInheritedSystemPrompt(prompt)` — 设置继承的系统提示词

**SessionMemoryAgentUpdater** — agent_edit 模式骨架（⤵️ 6.x 回填）

```go
type SessionMemoryAgentUpdater struct {
    config    SessionMemoryConfig
    // ⤵️ 6.x: agent *react_agent.ReActAgent
    // ⤵️ 6.x: agentCard *agent_schema.AgentCard
}
```

Invoke 返回 `fmt.Errorf("agent_edit 模式尚未实现（⤵️ 6.x 回填）")`

**SessionMemoryManager** — 调度器

```go
type SessionMemoryManager struct {
    config       SessionMemoryConfig
    updater      SessionMemoryUpdater
    tasks        map[string]context.CancelFunc  // 后台任务管理
}
```

方法：

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `NewSessionMemoryManager(config)` | `__init__` | 构造函数，默认使用 DirectUpdater |
| `BindModelDefaults(...)` | `bind_model_defaults()` | 绑定默认模型配置 |
| `MaybeScheduleUpdate(ctx, session, mc, workspace)` | `async maybe_schedule_update()` | 判断是否需更新，创建后台任务 |
| `ShouldUpdate(session, mc, window)` | `should_update()` | 基于阈值判断 |
| `CollectContextWindow(mc)` | `collect_context_window()` | 收集上下文窗口 |
| `UpdateInheritedSystemPrompt(messages)` | `update_inherited_system_prompt()` | 提取系统提示词 |
| `Shutdown()` | `shutdown()` | 取消所有后台任务 |

模块级辅助函数（放同一文件）：

| 函数 | 对齐 Python | 说明 |
|------|------------|------|
| `getSessionMemoryRuntime(session)` | `get_session_memory_runtime()` | 从 session 获取运行时状态 |
| `updateSessionMemoryRuntime(session, state)` | `update_session_memory_runtime()` | 更新运行时状态 |
| `invalidateSessionMemoryAnchor(session)` | `invalidate_session_memory_anchor()` | 重置基线 |
| `getSessionMemoryPath(workspace, sessionID)` | `_get_session_memory_path()` | 笔记文件路径 |
| `getPendingSessionMemoryPath(path)` | `_get_pending_session_memory_path()` | 待提交路径 |
| `readOrInitSessionMemory(path)` | `_read_or_init_session_memory()` | 读取或初始化笔记 |
| `buildSessionMemoryPrompt(notesPath, currentNotes)` | `build_session_memory_prompt()` | 构建提示词 |
| `buildDirectSessionMemoryPrompt(notesPath, currentNotes)` | `build_direct_session_memory_prompt()` | 构建 direct_replace 提示词 |
| `buildSystemPromptText(messages)` | `build_system_prompt_text()` | 提取系统提示词文本 |
| `groupCompletedAPIRounds(messages)` | `group_completed_api_rounds()` | API 轮次分组 |
| `findLastCompletedAPIRoundEnd(messages)` | `find_last_completed_api_round_end()` | 最后完成轮次结束索引 |
| `getContextMessageID(message)` | `get_context_message_id()` | 获取消息 ID |

常量：
- `sessionMemoryStateKey = "__session_memory__"`
- `defaultSessionMemoryTemplate` — Markdown 模板
- `defaultSessionMemoryPrompt` — agent_edit 提示词
- `directSessionMemoryPrompt` — direct_replace 提示词

## 回填清单

5.31 完成后需要回填的位置：

| 来源 | 回填目标 | 文件 | 变更内容 |
|------|---------|------|---------|
| 5.16 | `ContextStats.StatMessages()` | interface/types.go | 实现按角色统计 + token 计算 |
| 5.16 | `ContextStats.StatTools()` | interface/types.go | 实现工具计数 + token 计算 |
| base | `StatContextWindow()` | base.go | 调用 StatMessages + StatTools + 计算对话轮次 |
| 5.21 | `offloadMessagesToMemory()` | processor/offload.go | 调用 mc.OffloadMessages() |
| 5.24 | `_loadSessionMemoryRuntime` 等 4 个方法 | compressor/full_compact_processor.go | 使用 SessionMemoryManager |
| 5.24 | `buildTaskStatusReinjectedContent` | compressor/full_compact_processor.go | 使用 session 引用 |
| 5.24 | `buildPlanModeReinjectedContent` | compressor/full_compact_processor.go | 使用 session 引用 |
| 5.29 | 3 个 offloader 的 `newOffloadHandleAndPath` | offloader/*.go | 使用 mc.WorkspaceDir() |
| 5.30 | `CreateContext()` 构造 SessionModelContext | engine.go | 替换 stub 返回真实实例 |

## 接口变更

### ModelContext Option 扩展

需要在 `interface/types.go` 中新增：

```go
// WithModel 设置 KV Cache 释放用的模型实例
func WithModel(m *llm.Model) Option { ... }

// Option 中的 Model 字段
type ProcessorOptions struct {
    // ... 已有字段
    Model *llm.Model  // KV Cache 释放用
}
```

### Session 接口

无需变更，已有 `WriteStream`/`GetState`/`UpdateState`/`GetSessionID`。

## 依赖关系

```
context/
├── 导入 processor (util/round/replace 方法)
├── 导入 iface (ModelContext/ContextWindow/ContextStats/Option)
├── 导入 schema (ContextCompressionState/Config/Offload)
├── 导入 token (TokenCounter)
├── 导入 llm_schema (BaseMessage/AssistantMessage/ToolMessage/...)
├── 导入 llm (Model — KVCacheManager + SessionMemoryDirectUpdater)
├── 导入 session (Session — stateRecorder.emit)
├── 导入 callback (TriggerContext — 事件触发)
└── 导入 exception (build_error — 参数校验)
```

## 测试策略

| 文件 | 测试重点 |
|------|---------|
| message_buffer_test.go | AddBack/GetBack/PopBack/SetMessages/Rebuild，含 maxSize 裁剪、history 边界 |
| kv_cache_manager_test.go | checkReleaseNeeded 前缀对比、Release 调用 model.Release 条件、首次不释放 |
| processor_state_recorder_test.go | BuildState 构建、Emit 触发回调+stream、历史截取、summary 生成 |
| context_utils_test.go | 8 个补充方法的单元测试、模型映射表查找 |
| session_model_context_test.go | 接口满足性、AddMessages/CompressContext/GetContextWindow 核心流程、Mutex TryLock 快速路径、统计计算、SaveState/LoadState、事件触发 |
| session_memory_manager_test.go | ShouldUpdate 阈值判断、DirectUpdater.Invoke、prompt 构建、运行时状态管理 |

覆盖率目标：≥ 85%，使用 fake 实现替代外部依赖（TokenCounter/Model/Session/Processor）。
