# 9.7 SessionSpawnExecutor 设计文档

## 概述

实现 DeepAgent 的会话子进程执行器（SessionSpawnExecutor）及其配套组件，完成 `session_spawn_task` 类型的任务执行链路。全量纳入 9.7 范围，Python 一比一对照，文件组织严格镜像 Python 目录结构。

### 在 Agent 会话流程中的位置

```
用户请求
  ↓
DeepAgent.Invoke()              ← 9.1（未实现，通过 DeepAgentProvider 接口解耦）
  ↓
TaskLoopController.RunLoop()    ← 9.4 ✅
  ↓
LoopCoordinator 协调轮次        ← 9.5 ✅
  ↓
TaskScheduler 调度任务
  ├── TaskType = "deep_agent_task"     → TaskLoopEventExecutor（9.6 ✅）→ ReActAgent.Invoke()
  └── TaskType = "session_spawn_task"  → SessionSpawnExecutor（9.7 本步骤）→ DeepAgent.create_subagent().invoke()
  ↓
TaskLoopEventHandler 路由事件   ← 9.6 ✅（回填 SessionSpawn 分支）
```

### 核心作用

SessionSpawnExecutor 是 DeepAgent **子会话派生**的执行引擎。当主 Agent 的任务循环中遇到 `session_spawn_task` 类型的任务时，此执行器负责：

1. 从任务元数据中提取 `subagent_type`、`task_description`、`sub_session_id`
2. 通过 `DeepAgent.create_subagent()` 创建子 Agent 实例
3. 调用子 Agent 的 `invoke()` 方法执行子任务
4. 将子任务结果封装为 `TASK_COMPLETION` / `TASK_FAILED` 事件产出

### 任务来源

**不是用户直接指定，而是 LLM 在推理过程中自主决定调用 `sessions_spawn` 工具。** 完整触发链路：

```
用户提问 → ReActAgent 推理 → LLM 判断需要拆分子任务
→ LLM 调用 sessions_spawn 工具 → SessionsSpawnTool 提交 session_spawn_task
→ TaskScheduler 调度 → SessionSpawnExecutor 执行子 Agent
```

## 依赖关系

### 前置依赖（已完成）

| 章节 | 组件 | 状态 |
|------|------|------|
| 9.2 | DeepAgentConfig | ✅ |
| 9.4 | TaskLoopController | ✅ |
| 9.5 | LoopCoordinator | ✅ |
| 9.6 | TaskLoopEventExecutor + TaskLoopEventHandler | ✅ |
| 6.11 | ReActAgent | ✅ |

### 后续回填依赖（未实现，通过接口解耦）

| 章节 | 回填内容 |
|------|---------|
| 9.1 DeepAgent | DeepAgentProvider 接口替换为 `*DeepAgent`；`CreateSubagent` / `ScheduleAutoInvokeOnSpawnDone` 方法实现 |

**决策：不提前实现 9.1/9.3，9.7 完全通过 DeepAgentProvider 接口解耦。**

## 文件组织（严格镜像 Python）

### 新建文件

| Python 文件 | Go 新建文件 | 组件 |
|------------|------------|------|
| `task_loop/session_spawn_executor.py` | `task_loop/session_spawn_executor.go` | `SessionSpawnExecutor` + `BuildSessionSpawnExecutor` + `buildErrorChunk` |
| `tools/subagent/session_tools.py` | `tools/subagent/session_tools.go` | `SessionTaskRow` + `SessionToolkit` + `SessionsListTool` + `SessionsSpawnTool` + `SessionsCancelTool` + `BuildSessionTools` |
| (无对应，新包) | `tools/subagent/doc.go` | 包文档 |

### 回填已有文件

| Go 文件 | 回填内容 |
|--------|---------|
| `task_loop/executor.go` | ① DeepAgentProvider 扩展 `CreateSubagent` 方法<br>② `⤵️ 9.7 回填` 注释更新 |
| `task_loop/handler.go` | ① `sessionToolkit` 类型 `any` → `*subagent.SessionToolkit`<br>② `SetSessionToolkit` 参数类型更新<br>③ `HandleTaskCompletion` SessionSpawn 分支完善<br>④ `HandleTaskFailed` SessionSpawn 分支完善 |
| `task_loop/doc.go` | 文件目录添加 `session_spawn_executor.go` |

## 核心类型设计

### 1. DeepAgentProvider 接口扩展（executor.go）

```go
type DeepAgentProvider interface {
    // ... 已有方法保持不变 ...

    // CreateSubagent 创建子 Agent 实例。
    // ⤵️ 9.1 回填：9.1 实现 DeepAgent 后，由 *DeepAgent.CreateSubagent 实现。
    // 对齐 Python: DeepAgent.create_subagent
    CreateSubagent(subagentType string, subSessionID string) (DeepAgentProvider, error)

    // ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke
    // ⤵️ 9.1 回填：实现 SessionSpawn 完成后的自动 invoke 调度
    ScheduleAutoInvokeOnSpawnDone(steerText string) error
}
```

`CreateSubagent` 返回 `DeepAgentProvider`（而非具体类型），因为子 Agent 本身也是 DeepAgent，可以继续创建更深层子 Agent。对齐 Python `create_subagent() -> DeepAgent`。

### 2. SessionSpawnExecutor（task_loop/session_spawn_executor.go）

```go
type SessionSpawnExecutor struct {
    // deps 任务执行器依赖
    deps *modules.TaskExecutorDependencies
    // provider 深层 Agent 提供者（用于 CreateSubagent）
    provider DeepAgentProvider
}
```

方法一比一对照 Python：

| Python 方法 | Go 方法 | 返回值 |
|------------|--------|--------|
| `__init__(deps, deep_agent)` | `NewSessionSpawnExecutor(deps, provider)` | `*SessionSpawnExecutor` |
| `execute_ability(task_id, session)` | `ExecuteAbility(ctx, taskID, sess)` | `(<-chan *stream.OutputSchema, error)` |
| `_build_error_chunk(task_id, error)` | `buildErrorChunk(taskID, errMsg)` | `*stream.OutputSchema` |
| `can_pause()` | `CanPause(...)` | `(false, "Session spawn 任务不支持暂停", nil)` |
| `pause()` | `Pause(...)` | `(false, nil)` |
| `can_cancel()` | `CanCancel(...)` | `(true, "", nil)` |
| `cancel()` | `Cancel(...)` | `(true, nil)` |
| `build_session_spawn_executor(deep_agent)` | `BuildSessionSpawnExecutor(provider)` | 工厂闭包 |

### 3. ExecuteAbility 核心流程（对照 Python session_spawn_executor.py:45-98）

```
1. 从 TaskManager.GetTask 获取任务元数据
2. 提取 metadata: subagent_type, task_description, sub_session_id
3. 日志：开始执行（info，含 task_id/subagent_type/sub_session_id）
4. 调用 provider.CreateSubagent(subagentType, subSessionID) 创建子 Agent
5. 调用 subAgent.Invoke(ctx, {query, conversation_id}) 执行子任务
6. 提取子任务输出
7. 日志：执行完成（info，含 task_id/output_len）
8. 发送 TASK_COMPLETION 事件到输出 channel
9. 异常路径：日志（error，含 task_id/err）+ 发送 TASK_FAILED 事件
```

### 4. SessionTaskRow + SessionToolkit（tools/subagent/session_tools.go）

```go
type SessionTaskRow struct {
    TaskID       string `json:"task_id"`
    SubSessionID string `json:"sub_session_id"`
    Description  string `json:"description"`
    Status       string `json:"status"`
    Result       string `json:"result,omitempty"`
    Error        string `json:"error,omitempty"`
}

type SessionToolkit struct {
    rows map[string]*SessionTaskRow
    mu   sync.RWMutex
}
```

SessionToolkit 方法一比一对照 Python：

| Python 方法 | Go 方法 |
|------------|--------|
| `upsert_running(task_id, sub_session_id, description)` | `UpsertRunning(taskID, subSessionID, description)` |
| `mark_completed(task_id, result)` | `MarkCompleted(taskID, result)` |
| `mark_failed(task_id, error)` | `MarkFailed(taskID, err)` |
| `mark_canceled(task_id)` | `MarkCanceled(taskID)` |
| `list_all()` | `ListAll() []*SessionTaskRow` |
| `get(task_id)` | `Get(taskID) *SessionTaskRow` |
| `clear()` | `Clear()` |

### 5. 三个工具（tools/subagent/session_tools.go）

使用 `foundation/tool.MapFunction` 模式实现 `Tool` 接口：

| 工具 | 结构体 | 核心逻辑 | 对齐 Python |
|------|--------|---------|------------|
| `sessions_list` | `SessionsListTool` | 调用 `SessionToolkit.ListAll()` 格式化输出 | `SessionsListTool.invoke` |
| `sessions_spawn` | `SessionsSpawnTool` | 生成 taskID + subSessionID → 提交 TaskManager → UpsertRunning | `SessionsSpawnTool.invoke` |
| `sessions_cancel` | `SessionsCancelTool` | 通过 scheduler.CancelTask → MarkCanceled | `SessionsCancelTool.invoke` |

每个工具结构体持有：
- `SessionsListTool`：`toolkit *SessionToolkit`
- `SessionsSpawnTool`：`provider DeepAgentProvider` + `toolkit *SessionToolkit`
- `SessionsCancelTool`：`provider DeepAgentProvider` + `toolkit *SessionToolkit`

工厂函数：

```go
// BuildSessionTools 构建 session 工具集。
// 对齐 Python: build_session_tools
func BuildSessionTools(provider DeepAgentProvider, toolkit *SessionToolkit, language string, availableAgents string, agentID string) []tool.Tool
```

### 6. SessionsSpawnTool.Invoke 核心流程（对照 Python session_tools.py:259-327）

```
1. 检查 enable_task_loop（从 DeepConfig 读取）
2. 获取 TaskManager（从 EventHandler 基础依赖获取）
3. 提取 subagent_type, task_description 从 inputs
4. 生成 task_id (uuid) + sub_session_id (parent_session_id + "_sub_" + random_hex)
5. 构建 Task（task_type=SessionSpawnTaskType, status=SUBMITTED, metadata 含 subagent_type/task_description/sub_session_id）
6. TaskManager.AddTask 提交任务
7. SessionToolkit.UpsertRunning 注册跟踪
8. 日志：已提交（info，含 task_id/sub_session_id/subagent_type）
9. 返回 ToolOutput{success=true, data={status:"pending", message:...}}
```

## 回填逻辑详解

### executor.go 回填

**位置1：第44行 ScheduleAutoInvokeOnSpawnDone**
- 已在 DeepAgentProvider 接口上定义，标注 `⤵️ 9.1 回填`
- 9.7 不需要实现具体逻辑，仅确保接口签名正确

**位置2：第65-69行 常量注册注释**
- 更新 `⤵️ 9.7 回填：注册到 TaskExecutorRegistry` 注释
- 说明注册时机：9.1 的 `_setup_task_loop` 中调用 `BuildSessionSpawnExecutor(provider)` 注册

**位置3：第398行 BuildDeepExecutor 注释**
- 更新注释，说明 BuildSessionSpawnExecutor 同理在 9.1 中注册

### handler.go 回填

**位置1：第38行 sessionToolkit 字段**
```go
// 当前：sessionToolkit any
// 回填：sessionToolkit *subagent.SessionToolkit
```

**位置2：第410行 SetSessionToolkit 方法**
```go
// 当前：func (h *TaskLoopEventHandler) SetSessionToolkit(toolkit any)
// 回填：func (h *TaskLoopEventHandler) SetSessionToolkit(toolkit *subagent.SessionToolkit)
```

**位置3：第264-269行 HandleTaskCompletion SessionSpawn 分支**

对照 Python `TaskLoopEventHandler.handle_task_completion` (390-392行) + `_complete_session_spawn` (530-587行)：

```go
// 回填后：
if taskType == SessionSpawnTaskType {
    h.completeSessionSpawn(taskID, input, false)
    return map[string]any{"status": "session_spawn_completed", "task_id": taskID}, nil
}
```

**位置4：第308-313行 HandleTaskFailed SessionSpawn 分支**

对照 Python `handle_task_failed` (455-457行)：

```go
// 回填后：
if taskType == SessionSpawnTaskType {
    h.completeSessionSpawn(taskID, input, true)
    return map[string]any{"status": "session_spawn_failed", "task_id": taskID, "error": errMsg}, nil
}
```

**新增方法：completeSessionSpawn**

对照 Python `_complete_session_spawn` (530-587行)，这是 SessionSpawn 完成后的核心路由逻辑：

```go
// completeSessionSpawn 处理 SESSION_SPAWN 完成/失败。
// 对齐 Python: TaskLoopEventHandler._complete_session_spawn
func (h *TaskLoopEventHandler) completeSessionSpawn(taskID string, input *modules.EventHandlerInput, isError bool) {
    // 1. 提取结果/错误字符串（截断：成功 500 字符，错误 300 字符）
    var resultStr, errorStr string
    if isError {
        errorStr = extractErrorFromEvent(input)  // 对齐 Python: _extract_error_from_event
    } else {
        resultStr = extractResultFromEvent(input) // 对齐 Python: _extract_result_from_event
    }

    // 2. 更新 SessionToolkit 状态
    h.mu.Lock()
    toolkit := h.sessionToolkit
    h.mu.Unlock()
    if toolkit != nil {
        if isError {
            toolkit.MarkFailed(taskID, errorStr)
        } else {
            toolkit.MarkCompleted(taskID, resultStr)
        }
    }

    // 3. 格式化 steer 文本（中英文双语模板）
    taskDesc, _ := input.Event.GetMetadata()["task_description"].(string)
    language := "cn"
    if cfg := h.provider.DeepConfig(); cfg != nil {
        language = cfg.Language
    }
    steerText := formatSessionSpawnSteer(taskDesc, isError, resultStr, errorStr, language)

    // 4. 两条分支路径
    if h.provider.IsInvokeActive() {
        // Path 1: 有活跃 invoke → push_steer 注入引导队列
        if h.interactionQueues != nil {
            h.interactionQueues.PushSteer(steerText)
        }
        logger.Info(logComponent).Str("task_id", taskID).Msg("SessionSpawn 完成，steer 注入（活跃 invoke）")
    } else {
        // Path 2: 无活跃 invoke → 延迟调度自动 invoke
        if !h.provider.IsAutoInvokeScheduled() {
            h.provider.SetAutoInvokeScheduled(true)
            _ = h.provider.ScheduleAutoInvokeOnSpawnDone(steerText)
        }
        logger.Info(logComponent).Str("task_id", taskID).Msg("SessionSpawn 完成，自动 invoke 已调度")
    }
}
```

**新增辅助方法（对照 Python _extract_result_from_event / _extract_error_from_event / _format_session_spawn_steer）：**

```go
// extractResultFromEvent 从完成事件提取结果字符串，截断 500 字符。
// 对齐 Python: _extract_result_from_event (590-603行)
func extractResultFromEvent(input *modules.EventHandlerInput) string {
    event := input.Event
    if tce, ok := event.(*cschema.TaskCompletionEvent); ok {
        for _, df := range tce.TaskResult {
            if jsonDF, ok := df.(*cschema.JsonDataFrame); ok {
                if output, ok := jsonDF.Data["output"]; ok {
                    s := fmt.Sprintf("%v", output)
                    if len(s) > 500 { s = s[:500] }
                    return s
                }
            }
            if textDF, ok := df.(*cschema.TextDataFrame); ok {
                s := textDF.Text
                if len(s) > 500 { s = s[:500] }
                return s
            }
        }
    }
    return ""
}

// extractErrorFromEvent 从失败事件提取错误字符串，截断 300 字符。
// 对齐 Python: _extract_error_from_event (606-610行)
func extractErrorFromEvent(input *modules.EventHandlerInput) string {
    event := input.Event
    if tfe, ok := event.(*cschema.TaskFailedEvent); ok {
        s := tfe.ErrorMessage
        if len(s) > 300 { s = s[:300] }
        return s
    }
    return "unknown"
}

// formatSessionSpawnSteer 格式化 SessionSpawn 完成的 steer 文本。
// 对齐 Python: _format_session_spawn_steer (613-633行)
func formatSessionSpawnSteer(taskDesc string, isError bool, result string, err string, language string) string {
    // 中英文双语模板，对照 Python templates 字典
    ...
}
```

**关键区别于之前错误设计：**
- ❌ 之前用 `fmt.Sprintf("%v", output)` 直接赋值，无截断，类型不精确
- ✅ 现在对照 Python 的 `str(output)[:500]`，用 `fmt.Sprintf("%v", output)` 转 string + 截断 500 字符
- ✅ 错误提取对照 Python 的 `str(error_message)[:300]`，截断 300 字符
- ✅ 完整实现了 `_complete_session_spawn` 的两条路径（steer vs auto-invoke），之前完全遗漏

## 回填点完整清单（⤵️ 9.7，不能丢失）

| # | 文件 | 位置 | 回填内容 | 状态 |
|---|------|------|----------|------|
| 1 | `executor.go:44` | DeepAgentProvider.ScheduleAutoInvokeOnSpawnDone | 接口签名已存在，标注 ⤵️ 9.1 回填 | 保持 |
| 2 | `executor.go:65` | DeepTaskType 注册注释 | 更新注释说明注册时机 | 回填 |
| 3 | `executor.go:68-69` | SessionSpawnTaskType 注册注释 | 更新注释说明注册时机 | 回填 |
| 4 | `executor.go:398` | BuildDeepExecutor 注释 | 更新注释 | 回填 |
| 5 | `handler.go:38` | sessionToolkit 字段类型 | `any` → `*subagent.SessionToolkit` | 回填 |
| 6 | `handler.go:410` | SetSessionToolkit 参数类型 | `any` → `*subagent.SessionToolkit` | 回填 |
| 7 | `handler.go:264-269` | HandleTaskCompletion SessionSpawn 分支 | 调用 `completeSessionSpawn(taskID, input, false)` | 回填 |
| 8 | `handler.go:308-313` | HandleTaskFailed SessionSpawn 分支 | 调用 `completeSessionSpawn(taskID, input, true)` | 回填 |
| 9 | `handler.go` (新增) | `completeSessionSpawn` 方法 | 完整实现：提取结果/错误 → 更新 toolkit → 格式化 steer → 两条路径(steer/auto-invoke) | 新增 |
| 10 | `handler.go` (新增) | `extractResultFromEvent` | 从完成事件提取结果字符串，截断 500 字符 | 新增 |
| 11 | `handler.go` (新增) | `extractErrorFromEvent` | 从失败事件提取错误字符串，截断 300 字符 | 新增 |
| 12 | `handler.go` (新增) | `formatSessionSpawnSteer` | 中英文双语 steer 文本模板 | 新增 |

## 日志同步（对照 Python）

| Python 日志点 | Go 日志位置 | 级别 | 字段 |
|-------------|-----------|------|------|
| `[SessionSpawnExecutor] Executing task_id=..., subagent_type=..., sub_session_id=...` | `SessionSpawnExecutor.ExecuteAbility` 开始 | Info | task_id, subagent_type, sub_session_id |
| `[SessionSpawnExecutor] task_id=... completed, output_len=...` | `ExecuteAbility` 成功 | Info | task_id, output_len |
| `[SessionSpawnExecutor] task_id=... failed: ...` (exception) | `ExecuteAbility` 异常 | Error | task_id, err |
| `[SessionSpawnExecutor] Cancelling task_id=...` | `Cancel` | Info | task_id |
| `[SessionsSpawnTool] Submitted task_id=..., sub_session_id=..., subagent_type=...` | `SessionsSpawnTool.Invoke` | Info | task_id, sub_session_id, subagent_type |
| `[SessionsCancelTool] Cancelled task_id=...` | `SessionsCancelTool.Invoke` | Info | task_id |
| `[SessionSpawn] task_id=... completed, steer pushed (active invoke)` | `completeSessionSpawn` Path 1 | Info | task_id |
| `[SessionSpawn] task_id=... completed, auto-invoke scheduled` | `completeSessionSpawn` Path 2 | Info | task_id |

## 测试策略

- `session_spawn_executor_test.go`：使用 fake DeepAgentProvider mock 测试 ExecuteAbility/CanPause/Pause/CanCancel/Cancel/BuildSessionSpawnExecutor
- `session_tools_test.go`：测试 SessionToolkit 各状态转换、SessionsListTool/SpawnTool/CancelTool 的 Invoke 逻辑
- `handler_test.go`：更新已有 SessionSpawn 分支测试，验证 toolkit.MarkCompleted/MarkFailed 调用
- 覆盖率目标：≥ 85%

## 不在 9.7 范围内的内容

| 组件 | 原因 | 归属章节 |
|------|------|---------|
| `tools/subagent/task_tool.py` (TaskTool) | TaskTool 是同步 Tool 调用路径，与 SessionSpawn 异步路径独立 | 后续章节 |
| `DeepAgent.create_subagent` 实际实现 | 需要 9.1 DeepAgent 主体 | 9.1 |
| `DeepAgent._setup_task_loop` 中注册逻辑 | 需要 9.1 DeepAgent 主体 | 9.1 |
| `SubagentRail` 中 SessionToolkit 创建和注入 | Rails 系统属于 9.11-9.24 | 9.x Rails |
| `ScheduleAutoInvokeOnSpawnDone` 具体逻辑 | 需要 DeepAgent 的 invoke 循环 | 9.1 |
