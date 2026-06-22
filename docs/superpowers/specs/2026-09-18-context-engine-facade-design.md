# 5.30 ContextEngine 门面设计

## 概述

ContextEngine 是上下文引擎的门面（Facade），管理上下文池、处理器注册、会话状态持久化。属于 Agent 级别组件（不在 Session 中），通过 `agent.contextEngine` 访问。

对应 Python: `openjiuwen/core/context_engine/context_engine.py` (ContextEngine)

## 在 Agent 会话中的位置与作用

```
用户请求 → Agent（ReAct 循环）
             │
             ├─ AbilityManager.SetContextEngine(ce)  ← 注入 ContextEngine
             │
             ├─ 每轮推理前：
             │    ce.CreateContext(ctx, contextID, session) → ModelContext
             │    ModelContext.GetContextWindow(...) → ContextWindow（送入 LLM）
             │
             ├─ LLM 返回后：
             │    ModelContext.AddMessages(ctx, response) → 更新上下文
             │
             ├─ 上下文过长时：
             │    ce.CompressContext(ctx, contextID, session) → 触发压缩
             │
             ├─ 会话结束时：
             │    ce.SaveContexts(ctx, session, contextIDs) → 持久化
             │    ce.ClearContext(ctx, opts...) → 清理
             │
             └─ Agent 生命周期内，ContextEngine 是单例（Agent 级别，非 Session 级别）
```

核心作用：
- **上下文池管理**：维护 `sessionID_contextID → ModelContext` 的映射池，创建/查找/删除上下文实例
- **处理器注册与创建**：通过 `RegisterProcessorFactory` 将处理器类型映射到工厂，按配置创建实例
- **会话状态持久化**：`_saveStateToSession` / `_loadStateFromSession` 将上下文状态存储到 Session
- **压缩调度**：`CompressContext` 主动触发压缩，处理 busy/compressed/noop 三种状态
- **生命周期事件**：`CreateContext`/`ClearContext`/`SaveContexts` 触发回调事件

## 一、Python vs Go 接口差异

| 方法 | Python 签名 | Go 现有签名 | 差异 |
|------|------------|-------------|------|
| `create_context` | `(context_id, session, *, processors, history_messages, token_counter)` | `(ctx, contextID, sess)` | 缺少 processors/history_messages/token_counter |
| `get_context` | `(context_id, session_id)` | `(contextID, sessionID)` | ✅ 对齐 |
| `compress_context` | `(context_id, session, *, session_id, processor_types, **kwargs)` | `(ctx, contextID, sess)` | 缺少 processor_types |
| `clear_context` | `(context_id=None, session_id=None)` 三种粒度 | `(ctx, contextID, sessionID)` 强制两个参数 | 签名语义不同 |
| `save_contexts` | `(session, context_ids=None)` 返回 states | `(ctx, sess, contextIDs)` 返回 error | 缺少返回 states |
| `register_processor` | 类装饰器，`_PROCESSOR_MAP` | 接口方法 `RegisterProcessor` | Python 是类方法，Go 已有 `RegisterProcessorFactory` 全局函数 |

## 二、ContextEngine 接口修改

### 2.1 修改后的接口

```go
// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
    // CreateContext 创建或获取上下文
    CreateContext(ctx context.Context, contextID string, sess *session.Session, opts ...CreateContextOption) (ModelContext, error)
    // GetContext 获取上下文（不存在返回 nil）
    GetContext(contextID string, sessionID string) ModelContext
    // CompressContext 主动压缩上下文，返回 "busy"/"compressed"/"noop"
    CompressContext(ctx context.Context, contextID string, sess *session.Session, opts ...CompressContextOption) (string, error)
    // ClearContext 清空上下文（三种粒度：全清/按session/按context+session）
    ClearContext(ctx context.Context, opts ...ClearContextOption) error
    // SaveContexts 批量持久化上下文状态，返回 contextID → state 映射
    SaveContexts(ctx context.Context, sess *session.Session, contextIDs []string) (map[string]any, error)
}
```

### 2.2 移除的方法

- `RegisterProcessor(processorType string, p ContextProcessor)` — 已有全局 `RegisterProcessorFactory`，无需在接口上重复

### 2.3 变更说明

| 方法 | 变更 | 原因 |
|------|------|------|
| `CreateContext` | 新增 `...CreateContextOption` | 对齐 Python 的 processors/history_messages/token_counter 可选参数 |
| `CompressContext` | 新增 `...CompressContextOption` | 对齐 Python 的 processor_types 过滤参数 |
| `ClearContext` | 改为 `...ClearContextOption` | 对齐 Python 三种粒度清除（全清/按session/按context+session） |
| `SaveContexts` | 返回 `(map[string]any, error)` | 对齐 Python 返回 states |

## 三、ContextEngine 结构体

```go
type contextEngine struct {
    config       schema.ContextEngineConfig   // 全局引擎配置
    workspace    Workspace                     // Option 注入，Python: self._workspace
    sysOperation SysOperation                  // Option 注入，Python: self._sys_operation
    contextPool  map[string]iface.ModelContext // Python: self._context_pool
    mu           sync.RWMutex                  // 保护 contextPool
}
```

### 3.1 构造函数

```go
// NewContextEngine 创建上下文引擎实例。
//
// 必选参数：config（全局配置）。
// 可选参数通过 ContextEngineOption 传入：
//   - WithWorkspace(w) 设置工作空间
//   - WithSysOperation(op) 设置系统操作接口
func NewContextEngine(config schema.ContextEngineConfig, opts ...ContextEngineOption) *contextEngine
```

### 3.2 Workspace 和 SysOperation 的传递方式

Python 中这两个参数是 `__init__` 的可选关键字参数（默认 None），由 ContextEngine 持有并透传：

```python
# Python 构造
self.context_engine = ContextEngine(
    config,
    workspace=self._config.workspace,
    sys_operation=sys_operation,
)

# Python 透传给 SessionModelContext
context = SessionModelContext(...,
    workspace=self._workspace,
    sys_operation=self._sys_operation,
)
```

Go 对齐：构造时 Option 注入，`CreateContext` 时自动透传给 SessionModelContext（⤵️ 5.31 回填实例化逻辑）。

## 四、Option 类型定义

### 4.1 ContextEngineOption（构造器）

| 函数 | 对齐 Python 参数 |
|------|------------------|
| `WithWorkspace(w any) ContextEngineOption` | `workspace=` |
| `WithEngineSysOperation(op any) ContextEngineOption` | `sys_operation=` |

### 4.2 CreateContextOption

| 函数 | 对齐 Python 参数 |
|------|------------------|
| `WithProcessors(specs []ProcessorSpec) CreateContextOption` | `processors=` |
| `WithHistoryMessages(msgs []BaseMessage) CreateContextOption` | `history_messages=` |
| `WithTokenCounter(tc TokenCounter) CreateContextOption` | `token_counter=` |

其中 `ProcessorSpec` 对齐 Python 的 `List[Tuple[str, BaseModel]]`：

```go
// ProcessorSpec 处理器规格，指定类型和配置。
//
// 对应 Python: (processor_type, processor_config) 元组
type ProcessorSpec struct {
    Type   string
    Config ProcessorConfig
}
```

### 4.3 CompressContextOption

| 函数 | 对齐 Python 参数 |
|------|------------------|
| `WithProcessorTypes(types []string) CompressContextOption` | `processor_types=` |
| `WithCompressSysOperation(op any) CompressContextOption` | `sys_operation=`（由 ContextEngine 透传） |
| `WithModelName(name string) CompressContextOption` | `model_name=`（用于 resolve_context_max） |

**说明**：Python 的 `compress_context(processor_types=, sys_operation=self._sys_operation, **kwargs)` 透传 `sys_operation` 和 `**kwargs`。
Go 在 `CompressContext` 方法中自动注入 `ce.sysOperation`（若调用方未指定），通过 `CompressContextOption` 独立补齐各字段，方案 A（各 Option 独立补齐）。

### 4.4 ClearContextOption

| 函数 | 对齐 Python 参数 | 说明 |
|------|------------------|------|
| `WithSessionID(sid string) ClearContextOption` | `session_id=` | 按会话清除 |
| `WithContextID(cid string) ClearContextOption` | `context_id=` | 按上下文清除 |

三种粒度规则（对齐 Python）：
- 不提供任何 Option → 清空所有上下文
- 仅 WithSessionID → 清除该 session 下所有上下文
- WithSessionID + WithContextID → 清除指定上下文

## 五、ModelContext 接口补充（5.30 定义签名，5.31 实现）

### 5.1 新增方法（5.30 初版）

| 新增方法 | Python 对应 | 用途 |
|----------|------------|------|
| `WorkspaceDir() string` | `workspace_dir()` | 获取工作目录路径 |
| `SetSessionRef(sess *session.Session)` | `set_session_ref()` | 设置会话引用 |
| `OffloadMessages(handle string, messages []BaseMessage)` | `offload_messages()` | 卸载消息到内存缓冲区 |
| `SaveState() map[string]any` | `save_state()` | 保存上下文状态 |
| `LoadState(state map[string]any)` | `load_state()` | 恢复上下文状态 |
| `CompressContext(ctx context.Context, opts ...CompressContextOption) (string, error)` | `compress_context()` | 主动压缩 |

### 5.2 签名变更（5.30 补齐 Option 透传）

Python 的 `add_messages`/`get_context_window`/`clear_messages` 均接受 `**kwargs` 透传给 processor，
Go 需要等价支持，采用方案 A（各 Option 独立补齐）：

| 方法 | 原签名 | 新签名 | Python 对应 |
|------|--------|--------|------------|
| `AddMessages` | `(ctx, message any)` | `(ctx, message BaseMessage, opts ...Option)` | `add_messages(messages, **kwargs)` |
| `GetContextWindow` | `(ctx, sysMsgs, tools, ws, dr)` | `(ctx, sysMsgs, tools, ws, dr, opts ...Option)` | `get_context_window(..., **kwargs)` |
| `ClearMessages` | `(ctx, withHistory)` | `(ctx, withHistory, opts ...Option)` | `clear_messages(**kwargs)` |

**`AddMessages` message 参数类型变更说明**：
- 原为 `message any`（接受 `*BaseMessage` 或 `[]*BaseMessage`），但 Python 的 `messages: BaseMessage | List[BaseMessage]` 传入的都是 `BaseMessage` 实例
- Go 的 `BaseMessage` 是接口类型，`UserMessage`/`AssistantMessage`/`ToolMessage` 等具体类型都实现了该接口
- 改为 `message llm_schema.BaseMessage` 更类型安全，单条直接传，多条传 slice（5.31 实现时在内部统一为 `[]BaseMessage`）

## 六、完整回填点清单

### 6.1 5.30/5.31 回填点（ModelContext 接口补充后回填）

| # | 文件 | 行号 | 当前状态 | 回填内容 | 目标步骤 |
|---|------|------|---------|---------|---------|
| 1 | `interface/types.go` | 175 | `StatMessages` 空方法体 | 按角色计数 + token 计算 | 5.31 |
| 2 | `interface/types.go` | 193 | `StatTools` 空方法体 | 工具计数 + token 计算 | 5.31 |
| 3 | `base.go` | 15 | `StatContextWindow` 空方法体 | 统计窗口消息+工具+对话轮次 | 5.31 |
| 4 | `processor/offload.go` | 105 | `offloadMessagesToMemory` 注释掉 `mc.OffloadMessages` 调用 | 调用 `mc.OffloadMessages(handle, messages)` | 5.31 |
| 5 | `processor/offloader/tool_result_budget_processor.go` | 530 | `workspaceDir := ""` | 使用 `mc.WorkspaceDir()` | 5.31 |
| 6 | `processor/offloader/message_offloader.go` | 310 | `workspaceDir := ""` | 使用 `mc.WorkspaceDir()` | 5.31 |
| 7 | `processor/offloader/message_summary_offloader.go` | 505 | `workspaceDir := ""` | 使用 `mc.WorkspaceDir()` | 5.31 |
| 8 | `processor/compressor/full_compact_processor.go` | 800 | `_loadSessionMemoryRuntime` 返回 nil | 需要 session 引用 | 5.31 |
| 9 | `processor/compressor/full_compact_processor.go` | 810 | `_loadSessionMemoryText` 返回 "" | 需要 session 引用 | 5.31 |
| 10 | `processor/compressor/full_compact_processor.go` | 820 | `_resolveSessionMemoryPath` 返回 "" | 需要 session 引用 | 5.31 |
| 11 | `processor/compressor/full_compact_processor.go` | 830 | `_selectMessagesAfterSessionMemory` 返回 nil | 需要 session 引用 | 5.31 |
| 12 | `processor/compressor/full_compact_processor.go` | 840 | `_invalidateSessionMemoryAnchor` 空操作 | 需要 session 引用 | 5.31 |
| 13 | `processor/compressor/full_compact_processor.go` | 1160 | `buildTaskStatusReinjectedContent` 返回 "" | 需要 session 引用 | 5.31 |
| 14 | `processor/compressor/full_compact_processor.go` | 1170 | `buildPlanModeReinjectedContent` 返回 "" | 需要 session 引用 | 5.31 |

### 6.2 9.32 回填点（SysOperation 接口定义后回填）

| # | 文件 | 行号 | 当前状态 | 回填内容 |
|---|------|------|---------|---------|
| 15 | `interface/processor.go` | 83 | `SysOperation any` | 替换为 SysOperation 接口类型 |
| 16 | `interface/processor.go` | 107 | `SysOperation any` | 替换为 SysOperation 接口类型 |
| 17 | `processor/offload.go` | 149 | `sysOperation` 参数未使用 | 优先使用 SysOperation 写文件 |
| 18 | `processor/offloader/tool_result_budget_processor.go` | 65 | `SysOperation any` | 替换为 SysOperation 接口类型 |
| 19 | `processor/offloader/tool_result_budget_processor.go` | 118 | `SysOperation any` | 替换为 SysOperation 接口类型 |

### 6.3 5.30 自身产生的回填点

| # | 回填点 | 说明 |
|---|--------|------|
| 20 | `CreateContext` 中实例化 SessionModelContext | ⤵️ 5.31 回填：当前 `CreateContext` 创建空 ModelContext 占位，5.31 实现 SessionModelContext 后回填实际构造 |
| 21 | `CompressContext` 委托 `mc.CompressContext()` | ⤵️ 5.31 回填：依赖 ModelContext.CompressContext 接口实现 |
| 22 | `_loadStateFromSession` 调用 `mc.LoadState()` | ⤵️ 5.31 回填：依赖 ModelContext.LoadState 接口实现 |

## 七、方法实现要点

### 7.1 CreateContext

对齐 Python `ContextEngine.create_context()`：

1. `contextID = processContextID(contextID)` — 点号替换下划线
2. `fullContextID = sessionID + "_" + contextID` — 组合为池键
3. 若 `fullContextID` 已在池中 → 返回已有 context，调用 `SetSessionRef`，`_loadStateFromSession`
4. 否则：
   - 从 Option 中提取 processors，通过 `GetProcessorFactory` + `_createProcessor` 创建实例
   - 若 Option 未提供 TokenCounter，默认创建 `TiktokenCounter`
   - 构造 SessionModelContext（⤵️ 5.31 回填）
   - `_loadStateFromSession`
   - 存入池
5. 触发 `ContextRetrieved` 事件

### 7.2 CompressContext

对齐 Python `ContextEngine.compress_context()`：

1. 从 session 解析 sessionID
2. `GetContext` 获取 ModelContext
3. 若不存在 → 抛出 `StatusContextExecutionError`
4. 若 ModelContext 不支持 `CompressContext` → 抛出 `StatusContextExecutionError`
5. 委托 `mc.CompressContext(opts...)` — 返回 "busy"/"compressed"/"noop"

### 7.3 ClearContext

对齐 Python `ContextEngine.clear_context()` 三种粒度：

- 无 Option → 清空池，触发 `ContextCleared` 事件
- 仅 WithSessionID → 按前缀匹配删除，触发事件
- WithSessionID + WithContextID → 精确删除，触发事件
- 未找到时记录 Warn 日志

### 7.4 SaveContexts

对齐 Python `ContextEngine.save_contexts()`：

1. 校验 session 非 nil（否则 Warn 日志返回）
2. 若 contextIDs 为 nil → 收集该 session 下所有 contextID
3. 遍历：调用 `mc.SaveState()`，收集到 states map
4. `_saveStateToSession(session, states)`
5. 触发 `ContextOffloaded` 事件
6. 返回 states

### 7.5 _loadStateFromSession / _saveStateToSession

对齐 Python 的静态方法：

```go
func loadStateFromSession(mc ModelContext, sess *session.Session, historyMessages []BaseMessage)
func saveStateToSession(sess *session.Session, states map[string]any)
```

- `loadStateFromSession`：从 `session.GetState("context")` 读取，若 historyMessages 非 nil 则覆盖 messages 字段，调用 `mc.LoadState()`
- `saveStateToSession`：先 `session.UpdateState({"context": nil})` 清空，再 `session.UpdateState({"context": states})` 写入

### 7.6 processContextID

```go
func processContextID(contextID string) string {
    return strings.ReplaceAll(contextID, ".", "_")
}
```

## 八、日志同步

对照 Python `context_engine.py` 中的 logger 调用：

| Python 日志点 | Go 对应 | 级别 |
|--------------|---------|------|
| `context_engine_logger.warning("Delete context failed, session does not exist", ...)` | `ClearContext` 按 session 清除未找到时 | Warn |
| `context_engine_logger.warning("Delete context failed, context does not exist", ...)` | `ClearContext` 按精确删除未找到时 | Warn |
| `context_engine_logger.warning("Save context failed, session cannot be None", ...)` | `SaveContexts` session 为 nil 时 | Warn |

## 九、5.30 实现步骤

1. 修改 `ContextEngine` 接口（移除 RegisterProcessor，补齐 Option 参数）
2. 补充 `ModelContext` 接口缺失的方法签名（WorkspaceDir, OffloadMessages, SaveState, LoadState, CompressContext, SetSessionRef）
3. 定义 `ProcessorSpec` 类型
4. 定义所有 Option 类型（ContextEngineOption / CreateContextOption / CompressContextOption / ClearContextOption）及对应的 With* 函数
5. 实现 `contextEngine` 结构体 + `NewContextEngine` 构造函数
6. 实现 `CreateContext`
7. 实现 `GetContext`（从池中获取，未修改则复用现有逻辑）
8. 实现 `CompressContext`
9. 实现 `ClearContext`（三种粒度 + 事件触发）
10. 实现 `SaveContexts`
11. 实现 `loadStateFromSession` / `saveStateToSession`
12. 实现 `processContextID`
13. 日志同步
14. 更新 `AbilityManager.SetContextEngine` 等消费者适配新接口
15. 单元测试覆盖率 ≥ 85%

## 十、消费者影响

需要同步更新的外部消费者：

| 文件 | 变更 |
|------|------|
| `internal/agentcore/single_agent/ability_manager.go` | `contextEngine` 字段类型不变（接口），`SetContextEngine` 签名不变 |
| `internal/agentcore/runner/callback/events.go` | 已有 `ContextCallEventType`，无需修改 |
| `internal/common/exception/codes_context.go` | 已有状态码，无需修改 |

AbilityManager 当前只持有 `iface.ContextEngine` 引用，接口方法变更后，调用方需适配 Option 参数。
