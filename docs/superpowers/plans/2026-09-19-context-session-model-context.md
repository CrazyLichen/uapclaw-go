# 5.31 Context 实现 — SessionModelContext 全量实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SessionModelContext（ModelContext 接口的具体实现）及其 5 个依赖子组件，完成所有回填点，使上下文引擎处理器链可实际运行。

**Architecture:** 在 `context_engine/context/` 子包中单层平铺 7 个 Go 文件，1:1 对齐 Python `context/` 目录。SessionModelContext 持有 MessageBuffer/OffloadBuffer/KVCacheManager/ProcessorStateRecorder/SessionMemoryManager，通过 sync.Mutex+TryLock 保护处理器链并发。SessionMemoryManager 通过 SessionMemoryUpdater 接口抽象 agent_edit 依赖，先实现 direct_replace 模式。

**Tech Stack:** Go 1.22+，sync.Mutex+TryLock，*llm.Model（KVCacheRelease），CallbackFramework（事件），Session.WriteStream（流推送）

---

## File Structure

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/context_engine/context/doc.go` | 包文档 |
| `internal/agentcore/context_engine/context/message_buffer.go` | ContextMessageBuffer + OffloadMessageBuffer |
| `internal/agentcore/context_engine/context/kv_cache_manager.go` | KVCacheManager |
| `internal/agentcore/context_engine/context/processor_state_recorder.go` | ProcessorStateRecorder + ProcessorStateInput |
| `internal/agentcore/context_engine/context/context_utils.go` | 8 个补充方法 + 模型映射表 |
| `internal/agentcore/context_engine/context/session_model_context.go` | SessionModelContext 主类 |
| `internal/agentcore/context_engine/context/session_memory_manager.go` | SessionMemoryManager + DirectUpdater + AgentUpdater 骨架 |
| `internal/agentcore/context_engine/context/message_buffer_test.go` | MessageBuffer 测试 |
| `internal/agentcore/context_engine/context/kv_cache_manager_test.go` | KVCacheManager 测试 |
| `internal/agentcore/context_engine/context/processor_state_recorder_test.go` | StateRecorder 测试 |
| `internal/agentcore/context_engine/context/context_utils_test.go` | ContextUtils 测试 |
| `internal/agentcore/context_engine/context/session_model_context_test.go` | SessionModelContext 测试 |
| `internal/agentcore/context_engine/context/session_memory_manager_test.go` | SessionMemoryManager 测试 |

### 修改文件（回填）

| 文件 | 变更 |
|------|------|
| `internal/agentcore/context_engine/interface/types.go` | 补充 WithModel Option，回填 StatMessages/StatTools |
| `internal/agentcore/context_engine/base.go` | 回填 StatContextWindow |
| `internal/agentcore/context_engine/engine.go` | 回填 CreateContext 构造 SessionModelContext |
| `internal/agentcore/context_engine/processor/offload.go` | 回填 offloadMessagesToMemory 调用 mc.OffloadMessages |
| `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go` | 回填 4 个 Session Memory 方法 + 2 个 reinject 方法 |
| `internal/agentcore/context_engine/processor/offloader/message_offloader.go` | 回填 newOffloadHandleAndPath 使用 mc.WorkspaceDir |
| `internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go` | 同上 |
| `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go` | 同上 |
| `IMPLEMENTATION_PLAN.md` | 5.31 状态更新为 ✅ |

---

## Task 1: ContextMessageBuffer

**Files:**
- Create: `internal/agentcore/context_engine/context/doc.go`
- Create: `internal/agentcore/context_engine/context/message_buffer.go`
- Test: `internal/agentcore/context_engine/context/message_buffer_test.go`

- [ ] **Step 1: 创建 doc.go 包文档**

```go
// Package context 提供上下文引擎的具体上下文实例实现。
//
// 本包实现 ModelContext 接口的核心类 SessionModelContext，
// 以及其依赖的子组件：消息缓冲区、KV 缓存管理器、压缩状态记录器、
// 上下文工具方法和会话记忆管理器。
//
// 文件目录：
//
//	context/
//	├── doc.go                        # 包文档
//	├── session_model_context.go      # SessionModelContext 核心类
//	├── message_buffer.go             # ContextMessageBuffer + OffloadMessageBuffer
//	├── kv_cache_manager.go           # KVCacheManager
//	├── processor_state_recorder.go   # ProcessorStateRecorder + ProcessorStateInput
//	├── context_utils.go              # 补充工具方法 + 模型映射表
//	└── session_memory_manager.go     # SessionMemoryManager + UpdateAgent
//
// 对应 Python 代码：openjiuwen/core/context_engine/context/
package context
```

- [ ] **Step 2: 编写 ContextMessageBuffer 测试**

在 `message_buffer_test.go` 中编写以下测试用例：
- `TestNewContextMessageBuffer_空历史` — 空历史消息，size=0
- `TestContextMessageBuffer_AddBack_单条` — 追加单条消息
- `TestContextMessageBuffer_AddBack_列表` — 追加多条消息
- `TestContextMessageBuffer_GetBack_全部` — size=nil 返回全部
- `TestContextMessageBuffer_GetBack_限制数量` — size=2 返回尾部 2 条
- `TestContextMessageBuffer_GetBack_不含历史` — withHistory=false 排除历史
- `TestContextMessageBuffer_PopBack` — 弹出并验证消息被移除
- `TestContextMessageBuffer_SetMessages_替换全部` — withHistory=true 完全替换
- `TestContextMessageBuffer_SetMessages_保留历史` — withHistory=false 保留历史
- `TestContextMessageBuffer_Rebuild` — 重建缓冲区
- `TestContextMessageBuffer_自动裁剪` — maxSize 限制下超过 2 倍自动裁剪
- `TestContextMessageBuffer_Size_受限` — maxSize 下 Size 返回受限值

每个测试使用 `llm_schema.NewUserMessage("test")` 创建测试消息。

- [ ] **Step 3: 实现 ContextMessageBuffer**

在 `message_buffer.go` 中实现 `ContextMessageBuffer` 结构体及其所有方法，对齐 Python `ContextMessageBuffer`：

```go
type ContextMessageBuffer struct {
    maxBufferSize      int
    contextMessages    []llm_schema.BaseMessage
    historyMessagesSize int
}
```

核心逻辑要点：
- `Size()` — maxBufferSize>0 时 `min(len, maxBufferSize)`
- `AddBack()` — 单条 append 或逐条 append，然后 `ifNeedResize()`
- `GetBack()` — 先截取有效窗口，再按 size/withHistory 过滤
- `PopBack()` — 调用 GetBack 获取，然后截断切片；withHistory=true 且弹出数 > context 部分时减少 historyMessagesSize
- `SetMessages()` — withHistory=true 直接替换 + historyMessagesSize=0；false 保留历史前缀
- `Rebuild()` — maxBufferSize>0 截取尾部，否则 copy
- `ifNeedResize()` — 超过 2*maxBufferSize 时裁剪前 maxBufferSize 条

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestContextMessageBuffer -v`
Expected: PASS

- [ ] **Step 5: 编写 OffloadMessageBuffer 测试**

- `TestNewOffloadMessageBuffer_空初始化` — GetAll 返回空 map
- `TestOffloadMessageBuffer_Offload_内存模式` — offload + reload
- `TestOffloadMessageBuffer_Reload_内存未找到` — handle 不存在返回空
- `TestOffloadMessageBuffer_Clear` — 清除指定 handle
- `TestOffloadMessageBuffer_GetAll` — 返回全部
- `TestOffloadMessageBuffer_SetWorkspaceInfo` — 设置后字段正确

- [ ] **Step 6: 实现 OffloadMessageBuffer**

```go
type OffloadMessageBuffer struct {
    inMemoryMessages map[string][]llm_schema.BaseMessage
    sysOperation     any
    workspaceDir     string
    sessionID        string
}
```

方法：`NewOffloadMessageBuffer`/`SetSysOperation`/`SetWorkspaceInfo`/`Offload`/`Reload`/`Clear`/`GetAll`
- `Reload` — in_memory 直接从 map 取；filesystem 调用 `reloadFromFilesystem`
- `reloadFromFilesystem` — 如果 sysOperation 为 nil 返回空；否则构造路径读取 JSON 文件反序列化
- `filesystemReloadPaths` — 精确路径 + glob 模式路径

- [ ] **Step 7: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestOffloadMessageBuffer -v`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/context_engine/context/
git commit -m "feat(context_engine): 实现 ContextMessageBuffer 和 OffloadMessageBuffer"
```

---

## Task 2: KVCacheManager

**Files:**
- Create: `internal/agentcore/context_engine/context/kv_cache_manager.go`
- Test: `internal/agentcore/context_engine/context/kv_cache_manager_test.go`

- [ ] **Step 1: 编写 KVCacheManager 测试**

- `TestNewKVCacheManager_初始化` — lastContextWindow 为 nil
- `TestKVCacheManager_Release_首次不释放` — lastContextWindow=nil 时仅保存快照
- `TestKVCacheManager_Release_无差异不释放` — 相同窗口不调用 model.Release
- `TestKVCacheManager_Release_消息差异释放` — 消息不同时调用 model.Release
- `TestKVCacheManager_Release_工具差异释放` — 工具不同时调用 model.Release
- `TestKVCacheManager_Release_model不支持` — SupportsKVCacheRelease=false 时直接返回
- `TestKVCacheManager_Release_model为nil` — model 为 nil 时直接返回
- `TestKVCacheManager_checkReleaseNeeded_消息前缀匹配` — 前缀一致返回正确 idx
- `TestKVCacheManager_checkReleaseNeeded_工具前缀匹配` — 工具前缀一致

使用 fake model：实现 `SupportsKVCacheRelease() bool` 和 `Release(ctx, ...ReleaseOption) (bool, error)` 接口，记录是否被调用。

- [ ] **Step 2: 实现 KVCacheManager**

```go
type KVCacheManager struct {
    sessionID         string
    lastContextWindow *iface.ContextWindow
}
```

`Release` 方法流程：
1. 从 `ProcessorOption.Extra["model"]` 获取 `*llm.Model`（通过 Option 透传）
2. model 为 nil 或 !model.SupportsKVCacheRelease() → 返回
3. lastContextWindow == nil → 保存快照返回
4. `checkReleaseNeeded(window)` → (shouldRelease, msgIdx, toolIdx)
5. shouldRelease 且 idx 有效 → 调用 `model.Release(ctx, WithReleaseSessionID/WithReleaseMessages/...)`
6. 更新 lastContextWindow

`checkReleaseNeeded` 方法：逐位对比消息/工具前缀，找到首个差异位置。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestKVCacheManager -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/context/kv_cache_manager.go internal/agentcore/context_engine/context/kv_cache_manager_test.go
git commit -m "feat(context_engine): 实现 KVCacheManager"
```

---

## Task 3: ProcessorStateRecorder

**Files:**
- Create: `internal/agentcore/context_engine/context/processor_state_recorder.go`
- Test: `internal/agentcore/context_engine/context/processor_state_recorder_test.go`

- [ ] **Step 1: 编写 ProcessorStateRecorder 测试**

- `TestProcessorStateInput_字段完整性` — 所有字段可设置
- `TestNewProcessorStateRecorder_初始化` — history 为空
- `TestProcessorStateRecorder_History` — 返回副本
- `TestProcessorStateRecorder_LoadHistory_截取` — 超过 historyLimit 截取
- `TestProcessorStateRecorder_BuildState_started` — 构建 started 状态
- `TestProcessorStateRecorder_BuildState_completed` — 构建 completed 状态（含 before+after+saved）
- `TestProcessorStateRecorder_BuildState_failed` — 构建 failed 状态
- `TestProcessorStateRecorder_BuildState_skipped` — 构建 skipped 状态
- `TestProcessorStateRecorder_Emit_记录历史` — emit 后 history 长度增加
- `TestProcessorStateRecorder_Emit_触发回调` — 验证 CallbackFramework 收到事件
- `TestProcessorStateRecorder_Emit_推送Stream` — 验证 Session.WriteStream 被调用
- `TestProcessorStateRecorder_buildMetric` — 验证 metric 构建
- `TestProcessorStateRecorder_buildSaved` — 验证 saved 计算
- `TestProcessorStateRecorder_buildSummary_各种状态` — 验证 summary 文本
- `TestProcessorStateRecorder_resolveModelName` — 从处理器配置提取模型名

使用 fake：fakeTokenCounter/fakeSession/fakeProcessor（含 config）

- [ ] **Step 2: 实现 ProcessorStateInput 结构体**

```go
type ProcessorStateInput struct {
    OperationID      string
    Status           schema.CompressionStatus
    Phase            schema.CompressionPhase
    Trigger          string
    Processor        iface.ContextProcessor
    Reason           string
    BeforeMessages   []llm_schema.BaseMessage
    AfterMessages    []llm_schema.BaseMessage
    StartedAt        time.Time
    EndedAt          time.Time
    Error            string
    MessagesToModify []int
    Force            bool
    ContextMax       int
    CompactSummary   string
    CompressionUsage *schema.ContextCompressionUsage
}
```

- [ ] **Step 3: 实现 ProcessorStateRecorder**

```go
type ProcessorStateRecorder struct {
    sessionID     string
    contextID     string
    getSessionRef func() *session.Session
    tokenCounter  token.TokenCounter
    historyLimit  int
    history       []map[string]any
}
```

方法：`NewProcessorStateRecorder`/`History`/`LoadHistory`/`Emit`/`BuildState` + 内部方法 `record`/`buildMetric`/`buildSaved`/`buildSummary`/`buildStatistic`/`measureMessages`/`resolveModelName`/`compactNumber`/`formatTime`/`contextPercent`

Emit 流程：
1. `record(state)` — 追加到 history
2. logger.Info 记录状态日志
3. `callback.GetCallbackFramework().TriggerContext(ContextCompressionStateEvent, ...)` — 回调
4. `sessionRef.WriteStream(OutputSchema{Type: ContextCompressionStateType, Payload: stateMap})` — stream

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestProcessorStateRecorder -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/context/processor_state_recorder.go internal/agentcore/context_engine/context/processor_state_recorder_test.go
git commit -m "feat(context_engine): 实现 ProcessorStateRecorder"
```

---

## Task 4: ContextUtils 补充

**Files:**
- Create: `internal/agentcore/context_engine/context/context_utils.go`
- Test: `internal/agentcore/context_engine/context/context_utils_test.go`

- [ ] **Step 1: 编写 ContextUtils 测试**

- `TestValidateMessages_单条通过` — 单条 BaseMessage 不报错
- `TestValidateMessages_列表通过` — 列表全部 BaseMessage 不报错
- `TestValidateMessages_列表含非Message` — 含 nil 时报错
- `TestEnsureContextMessageIDs_补全ID` — 无 ID 的消息补上
- `TestEnsureContextMessageIDs_已有ID` — 已有 ID 不覆盖
- `TestValidateAndFixContextWindow_开头无ToolMessage` — 不修改
- `TestValidateAndFixContextWindow_开头有ToolMessage` — 移除开头 ToolMessage
- `TestValidateAndFixContextWindow_全部ToolMessage` — 清空
- `TestResolveContextMax_fallback优先` — fallback 正数直接返回
- `TestResolveContextMax_自定义映射` — modelContextWindowTokens 中查找
- `TestResolveContextMax_内置映射` — ModelDefaultContextWindowTokens 中查找
- `TestResolveContextMax_默认值` — 返回 DefaultContextMaxTokens
- `TestIsCompressionProcessor_压缩类型` — processor_type 含 compressor/compact
- `TestIsCompressionProcessor_非压缩类型` — 其他类型返回 false
- `TestFormatReloadedMessages` — 格式化输出正确
- `TestFindLastNDialogueRound_n轮` — 返回正确的起始索引
- `TestFindLastNDialogueRound_无轮次` — 返回 -1

- [ ] **Step 2: 实现 ContextUtils**

常量和映射表：

```go
const (
    ContextMessageIDKey     = "context_message_id"
    DefaultContextMaxTokens = 200000
)

var ModelDefaultContextWindowTokens = map[string]int{ ... }  // 完整映射表
```

8 个函数：`ValidateMessages`/`EnsureContextMessageIDs`/`ValidateAndFixContextWindow`/`ResolveContextMax`/`IsCompressionProcessor`/`FormatReloadedMessages`/`FindLastNDialogueRound`/`FindLastAIAbsentToolCall`

其中 `FindLastNDialogueRound` 内部调用 `processor.FindAllDialogueRound`。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestValidate|TestEnsure|TestResolve|TestIsCompression|TestFormat|TestFindLastN -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/context/context_utils.go internal/agentcore/context_engine/context/context_utils_test.go
git commit -m "feat(context_engine): 实现 ContextUtils 补充方法"
```

---

## Task 5: SessionMemoryManager

**Files:**
- Create: `internal/agentcore/context_engine/context/session_memory_manager.go`
- Test: `internal/agentcore/context_engine/context/session_memory_manager_test.go`

- [ ] **Step 1: 编写 SessionMemoryConfig 测试**

- `TestNewSessionMemoryConfig_默认值` — 验证所有默认值

- [ ] **Step 2: 编写 SessionMemoryDirectUpdater 测试**

- `TestSessionMemoryDirectUpdater_BindModelDefaults` — 绑定默认模型配置
- `TestSessionMemoryDirectUpdater_SetInheritedSystemPrompt` — 设置继承提示词
- `TestSessionMemoryDirectUpdater_Invoke_成功` — mock model 返回，写入文件
- `TestSessionMemoryDirectUpdater_Invoke_重试` — 首次失败重试成功
- `TestSessionMemoryDirectUpdater_Invoke_重试耗尽` — 全部失败返回错误

使用 fake model：实现 `Invoke` 方法，记录调用次数和返回值。

- [ ] **Step 3: 编写 SessionMemoryManager 测试**

- `TestNewSessionMemoryManager_默认DirectUpdater` — 默认使用 direct_replace
- `TestSessionMemoryManager_ShouldUpdate_首次触发` — tokens >= triggerTokens
- `TestSessionMemoryManager_ShouldUpdate_首次未达阈值` — tokens < triggerTokens 返回 false
- `TestSessionMemoryManager_ShouldUpdate_增量触发` — tokens_since_last >= triggerAddTokens 且 tool_calls >= toolMin
- `TestSessionMemoryManager_ShouldUpdate_增量未达` — 返回 false
- `TestSessionMemoryManager_CollectContextWindow` — 收集上下文窗口
- `TestSessionMemoryManager_Shutdown` — 取消后台任务
- `TestUpdateSessionMemoryRuntime` — 更新运行时状态
- `TestGetSessionMemoryRuntime_默认值` — session 为 nil 时返回默认
- `TestInvalidateSessionMemoryAnchor` — 重置基线
- `TestGetSessionMemoryPath` — 路径格式正确
- `TestReadOrInitSessionMemory_文件存在` — 读取已有文件
- `TestReadOrInitSessionMemory_文件不存在` — 初始化默认模板
- `TestBuildSessionMemoryPrompt` — 占位符替换正确
- `TestBuildDirectSessionMemoryPrompt` — 占位符替换正确
- `TestBuildSystemPromptText_有系统消息` — 提取系统提示词
- `TestBuildSystemPromptText_无系统消息` — 返回空字符串

- [ ] **Step 4: 实现 SessionMemoryConfig**

```go
type SessionMemoryConfig struct {
    TriggerTokens           int
    TriggerAddTokens        int
    ToolMin                 int
    Model                   *llm_schema.ModelRequestConfig
    ModelClient             *llm_schema.ModelClientConfig
    UpdateMode              string  // "agent_edit" | "direct_replace"
    DirectReplaceMaxRetries int
}

func NewSessionMemoryConfig() SessionMemoryConfig { ... }
func (c SessionMemoryConfig) Validate() error { ... }
```

- [ ] **Step 5: 实现 SessionMemoryUpdater 接口**

```go
type SessionMemoryUpdater interface {
    Invoke(ctx context.Context, opts SessionMemoryUpdateOptions) error
    BindModelDefaults(modelConfig *llm_schema.ModelRequestConfig, clientConfig *llm_schema.ModelClientConfig)
    SetInheritedSystemPrompt(prompt string)
}

type SessionMemoryUpdateOptions struct {
    FullContextMessages []llm_schema.BaseMessage
    NotesPath           string
    CurrentNotes        string
}
```

- [ ] **Step 6: 实现 SessionMemoryDirectUpdater**

```go
type SessionMemoryDirectUpdater struct {
    config                SessionMemoryConfig
    model                 *llm.Model
    inheritedSystemPrompt string
}
```

Invoke 流程：
1. 确保 model 已创建（延迟初始化）
2. 构建提示消息列表（system prompt + context messages + user prompt）
3. 调用 `model.Invoke(ctx, messages)` 带重试
4. 规范化输出（去 markdown 代码块）
5. 写入文件 notesPath

- [ ] **Step 7: 实现 SessionMemoryAgentUpdater 骨架**

```go
type SessionMemoryAgentUpdater struct {
    config SessionMemoryConfig
    // ⤵️ 6.x 回填
}

func (u *SessionMemoryAgentUpdater) Invoke(ctx context.Context, opts SessionMemoryUpdateOptions) error {
    return fmt.Errorf("agent_edit 模式尚未实现（⤵️ 6.x 回填）")
}
func (u *SessionMemoryAgentUpdater) BindModelDefaults(...) { }
func (u *SessionMemoryAgentUpdater) SetInheritedSystemPrompt(...) { }
```

- [ ] **Step 8: 实现 SessionMemoryManager**

```go
type SessionMemoryManager struct {
    config SessionMemoryConfig
    updater SessionMemoryUpdater
    tasks   map[string]context.CancelFunc
}
```

方法：`NewSessionMemoryManager`/`BindModelDefaults`/`MaybeScheduleUpdate`/`ShouldUpdate`/`CollectContextWindow`/`UpdateInheritedSystemPrompt`/`Shutdown`

MaybeScheduleUpdate 流程：
1. session/workspace 为 nil → 返回
2. 已有在运行的任务 → 跳过
3. collectContextWindow → shouldUpdate → 不需要 → 返回
4. 创建后台 goroutine 执行更新
5. 后台逻辑：readOrInit → preparePending → updater.Invoke → commitPending → updateRuntime

模块级函数：`getSessionMemoryRuntime`/`updateSessionMemoryRuntime`/`invalidateSessionMemoryAnchor`/`getSessionMemoryPath`/`getPendingSessionMemoryPath`/`readOrInitSessionMemory`/`buildSessionMemoryPrompt`/`buildDirectSessionMemoryPrompt`/`buildSystemPromptText`/`groupCompletedAPIRounds`/`findLastCompletedAPIRoundEnd`/`getContextMessageID`

常量：`sessionMemoryStateKey`/`defaultSessionMemoryTemplate`/`defaultSessionMemoryPrompt`/`directSessionMemoryPrompt`

- [ ] **Step 9: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestSessionMemory -v`
Expected: PASS

- [ ] **Step 10: 提交**

```bash
git add internal/agentcore/context_engine/context/session_memory_manager.go internal/agentcore/context_engine/context/session_memory_manager_test.go
git commit -m "feat(context_engine): 实现 SessionMemoryManager + DirectUpdater"
```

---

## Task 6: SessionModelContext

**Files:**
- Create: `internal/agentcore/context_engine/context/session_model_context.go`
- Modify: `internal/agentcore/context_engine/interface/types.go` — 添加 WithModel Option + Model 字段
- Test: `internal/agentcore/context_engine/context/session_model_context_test.go`

- [ ] **Step 1: 在 interface/types.go 中添加 WithModel Option**

在 ProcessorOption 结构体中添加 Model 字段，添加 WithModel 选项函数：

```go
// ProcessorOption 中新增字段
Model *llm.Model  // KV Cache 释放用模型实例

// WithModel 设置 KV Cache 释放用的模型实例
func WithModel(m *llm.Model) Option {
    return func(o *ProcessorOption) { o.Model = m }
}
```

需要新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`。

- [ ] **Step 2: 编写 SessionModelContext 构造和基础方法测试**

- `TestNewSessionModelContext_基本构造` — 验证字段初始化
- `TestSessionModelContext_Len` — 返回消息数
- `TestSessionModelContext_SessionID` — 返回 sessionID
- `TestSessionModelContext_ContextID` — 返回 contextID
- `TestSessionModelContext_WorkspaceDir_有workspace` — 返回 rootPath
- `TestSessionModelContext_WorkspaceDir_无workspace` — 返回空字符串
- `TestSessionModelContext_TokenCounter` — 返回 tokenCounter
- `TestSessionModelContext_SetSessionRef` — 设置后可获取
- `TestSessionModelContext_满足ModelContext接口` — 编译期接口检查

使用 fake：fakeWorkspace{rootPath: "/tmp"}，fakeTokenCounter

- [ ] **Step 3: 实现 SessionModelContext 结构体和基础方法**

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
    processorLock             sync.Mutex
    activeCompressionInProgress bool
    kvCacheManager            *KVCacheManager
    offloadMessageBuffer      *OffloadMessageBuffer
    reloaderToolCard          *tool.ToolCard
}
```

构造函数 `NewSessionModelContext` 参数：contextID, sessionID, config, historyMessages, processors, tokenCounter, sessionRef, workspace, sysOperation

基础方法：`Len`/`SessionID`/`ContextID`/`WorkspaceDir`/`TokenCounter`/`SetSessionRef`

- [ ] **Step 4: 编写消息操作方法测试**

- `TestSessionModelContext_GetMessages_全部` — size=nil 返回全部
- `TestSessionModelContext_GetMessages_限制数量` — size=2 返回尾部
- `TestSessionModelContext_SetMessages` — 替换后 GetMessages 返回新列表
- `TestSessionModelContext_PopMessages` — 弹出后消息被移除
- `TestSessionModelContext_PopMessages_size负数` — 返回错误
- `TestSessionModelContext_ClearMessages` — 清空后 Len=0

- [ ] **Step 5: 实现消息操作方法**

`GetMessages`/`SetMessages`/`PopMessages`/`ClearMessages` — 委托 messageBuffer，含参数校验。

ClearMessages 额外：重置 offloadMessageBuffer + 触发 ContextCleared 事件。

- [ ] **Step 6: 编写 AddMessages 测试**

- `TestSessionModelContext_AddMessages_单条` — 添加单条消息
- `TestSessionModelContext_AddMessages_多条` — 添加消息列表（通过多次调用 AddMessages）
- `TestSessionModelContext_AddMessages_无处理器` — 消息直接入队
- `TestSessionModelContext_AddMessages_触发被动处理器` — mock processor trigger 返回 true
- `TestSessionModelContext_AddMessages_跳过被动处理器` — mock processor trigger 返回 false
- `TestSessionModelContext_AddMessages_快速路径_主动压缩进行中` — activeCompressionInProgress=true 时仅入队不触发处理器

- [ ] **Step 7: 实现 AddMessages**

AddMessages 流程：
1. ValidateMessages + EnsureContextMessageIDs
2. 快速路径：如果 `activeCompressionInProgress && !processorLock.TryLock()` → 仅 messageBuffer.AddBack → 返回
3. processorLock.Lock()
4. runAddProcessors(messages, force=false, ...)
5. messageBuffer.AddBack(result)
6. processorLock.Unlock()
7. 触发 ContextUpdated 事件

注意：当前 ModelContext 接口签名 `AddMessages(ctx, message, opts...)` 接受单条 BaseMessage。如果 opts 中含多条消息（通过 Extra 传递），需要处理。对齐 Python 的单条或列表语义。

- [ ] **Step 8: 编写 GetContextWindow 测试**

- `TestSessionModelContext_GetContextWindow_基本` — 返回正确窗口
- `TestSessionModelContext_GetContextWindow_按对话轮次截取` — dialogueRound 限制
- `TestSessionModelContext_GetContextWindow_按窗口大小截取` — windowSize 限制
- `TestSessionModelContext_GetContextWindow_enableReload` — 追加 reload system prompt
- `TestSessionModelContext_GetContextWindow_触发GET处理器` — mock processor
- `TestSessionModelContext_GetContextWindow_统计信息` — Statistic 字段填充
- `TestSessionModelContext_GetContextWindow_参数校验` — windowSize<=0 报错

- [ ] **Step 9: 实现 GetContextWindow**

GetContextWindow 流程：
1. 参数校验
2. processorLock.Lock()
3. 复制 systemMessages
4. enableReload → 追加 reloaderSystemPrompt
5. getWindowMessages(sysMsgs, windowSize, dialogueRound) — 按轮次/窗口大小截取
6. 构建 ContextWindow
7. 遍历 processors: trigger + on_get_context_window
8. ValidateAndFixContextWindow(window)
9. kvCacheManager.Release(ctx, window, model) — 条件
10. statContextWindow(window)
11. processorLock.Unlock()
12. 触发 ContextRetrieved 事件
13. 返回 window

- [ ] **Step 10: 编写 CompressContext 测试**

- `TestSessionModelContext_CompressContext_压缩成功` — 返回 "compressed"
- `TestSessionModelContext_CompressContext_无变化` — 返回 "noop"
- `TestSessionModelContext_CompressContext_忙` — 锁被持有时返回 "busy"
- `TestSessionModelContext_CompressContext_无匹配处理器` — 返回 "noop"

- [ ] **Step 11: 实现 CompressContext**

CompressContext 流程：
1. processorLock.TryLock() → 失败返回 "busy"
2. activeCompressionInProgress = true
3. selectProcessors(compressionOnly=true)
4. 无处理器 → 返回 "noop"
5. runAddProcessors([], force=true, compressionOnly=true, ...)
6. activeCompressionInProgress = false
7. processorLock.Unlock()
8. 返回 "compressed" / "noop"

- [ ] **Step 12: 编写统计方法测试**

- `TestSessionModelContext_Statistic` — 返回正确的统计信息
- `TestSessionModelContext_statMessages_按角色` — 各角色计数正确
- `TestSessionModelContext_statMessages_使用UsageMetadata` — 有 usage_metadata 时直接使用 totalTokens
- `TestSessionModelContext_statMessages_逐条计算` — 无 usage_metadata 时逐条计算
- `TestSessionModelContext_statTools` — 工具数和 token 数

- [ ] **Step 13: 实现统计方法**

内部方法：
- `statContextWindow(window)` — 调用 statMessages + statTools + find_all_dialogue_round
- `statMessages(stat, messages)` — 按角色计数 + 优先使用 usage_metadata + 逐条 fallback
- `statTools(stat, tools)` — 工具数 + 逐条 countToolTokens
- `countSingleMessageTokens(msg)` — tokenCounter.CountMessages 或 fallback len/4
- `countToolTokens(toolInfo)` — tokenCounter.CountTools 或 fallback len/4
- `resolveContextModelName(opts)` — 从 option 或实例解析

- [ ] **Step 14: 编写 OffloadMessages/SaveState/LoadState 测试**

- `TestSessionModelContext_OffloadMessages` — 消息存入 offloadBuffer
- `TestSessionModelContext_SaveState` — 返回 messages + offload_messages
- `TestSessionModelContext_LoadState` — 恢复后消息正确
- `TestSessionModelContext_ReloaderTool` — 返回非 nil

- [ ] **Step 15: 实现 OffloadMessages/SaveState/LoadState/ReloaderTool**

- `OffloadMessages` — 委托 offloadMessageBuffer.Offload
- `SaveState` — 返回 {contextID: {messages, offload_messages}}
- `LoadState` — Validate + EnsureIDs + messageBuffer.Rebuild + 重建 offloadBuffer
- `ReloaderTool` — 返回 reload 工具（使用 tool 包构建）

- [ ] **Step 16: 编写内部方法测试**

- `TestSessionModelContext_runAddProcessors_处理器链` — 2 个处理器按序执行
- `TestSessionModelContext_runAddProcessors_处理器异常` — 异常时发射 failed 状态
- `TestSessionModelContext_selectProcessors_按类型` — 过滤匹配类型
- `TestSessionModelContext_selectProcessors_仅压缩` — compressionOnly=true 过滤
- `TestSessionModelContext_getWindowMessages_按轮次` — dialogueRound 截取
- `TestSessionModelContext_getWindowMessages_按窗口大小` — windowSize 截取

- [ ] **Step 17: 实现内部方法**

- `runAddProcessors` — 遍历处理器：trigger → on → buildAndEmitCompressionState
- `selectProcessors` — 按 types/compressionOnly 过滤
- `getWindowMessages` — 先按 dialogueRound 截取，再按 windowSize 截取
- `buildAndEmitCompressionState` — stateRecorder.BuildState + stateRecorder.Emit
- `buildActiveCompressionResult` — 构建 "busy"/"compressed"/"noop" 返回结果

- [ ] **Step 18: 运行全部 SessionModelContext 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context/... -run TestSessionModelContext -v`
Expected: PASS

- [ ] **Step 19: 提交**

```bash
git add internal/agentcore/context_engine/context/session_model_context.go internal/agentcore/context_engine/context/session_model_context_test.go internal/agentcore/context_engine/interface/types.go
git commit -m "feat(context_engine): 实现 SessionModelContext 核心"
```

---

## Task 7: 回填 — StatMessages/StatTools/StatContextWindow

**Files:**
- Modify: `internal/agentcore/context_engine/interface/types.go` — 回填 StatMessages/StatTools
- Modify: `internal/agentcore/context_engine/base.go` — 回填 StatContextWindow

- [ ] **Step 1: 回填 ContextStats.StatMessages**

替换 `interface/types.go` 中 `StatMessages` 方法的空实现，填入实际逻辑：

```go
func (s *ContextStats) StatMessages(messages []llm_schema.BaseMessage, tokenCounter token.TokenCounter) {
    s.TotalMessages = len(messages)
    // 1. 优先使用最后一条 AssistantMessage 的 usage_metadata.total_tokens
    if usageTokens := getLastAssistantUsageTokens(messages); usageTokens > 0 {
        s.TotalTokens = usageTokens
        return
    }
    // 2. 逐条计算 token，按角色累加
    for _, msg := range messages {
        tokens := countSingleMessageTokens(msg, tokenCounter)
        switch msg.GetRole() {
        case llm_schema.RoleTypeSystem:
            s.SystemMessages++
            s.SystemMessageTokens += tokens
        case llm_schema.RoleTypeUser:
            s.UserMessages++
            s.UserMessageTokens += tokens
        case llm_schema.RoleTypeAssistant:
            s.AssistantMessages++
            s.AssistantMessageTokens += tokens
        case llm_schema.RoleTypeTool:
            s.ToolMessages++
            s.ToolMessageTokens += tokens
        }
    }
    s.TotalTokens = s.SystemMessageTokens + s.UserMessageTokens + s.AssistantMessageTokens + s.ToolMessageTokens
}
```

注意：`getLastAssistantUsageTokens` 和 `countSingleMessageTokens` 作为 `interface/types.go` 中的非导出辅助函数实现。

- [ ] **Step 2: 回填 ContextStats.StatTools**

```go
func (s *ContextStats) StatTools(tools []*schema.ToolInfo, tokenCounter token.TokenCounter) {
    s.Tools = len(tools)
    for _, t := range tools {
        s.ToolTokens += countToolTokens(t, tokenCounter)
    }
    s.TotalTokens += s.ToolTokens
}
```

- [ ] **Step 3: 回填 base.go StatContextWindow**

```go
func StatContextWindow(window *iface.ContextWindow, tokenCounter token.TokenCounter) {
    window.Statistic.StatMessages(window.GetMessages(), tokenCounter)
    window.Statistic.StatTools(window.GetTools(), tokenCounter)
    window.Statistic.TotalDialogues = len(processor.FindAllDialogueRound(window.GetMessages()))
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/interface/types.go internal/agentcore/context_engine/base.go
git commit -m "feat(context_engine): 回填 StatMessages/StatTools/StatContextWindow 统计逻辑"
```

---

## Task 8: 回填 — CreateContext 构造 SessionModelContext

**Files:**
- Modify: `internal/agentcore/context_engine/engine.go` — 回填 CreateContext

- [ ] **Step 1: 回填 CreateContext**

替换 `engine.go` 中 `CreateContext` 的 stub 实现（第 135-144 行），构造真实 SessionModelContext：

```go
// 构造 SessionModelContext 实例
mc := NewSessionModelContext(
    contextID,
    sessionID,
    ce.config,
    opt.HistoryMessages,
    processorInstances,
    tokenCounter,
    sess,
    ce.workspace,
    ce.sysOperation,
)

// 存入池
ce.mu.Lock()
ce.contextPool[fullContextID] = mc
ce.mu.Unlock()

// 加载已有状态
loadStateFromSession(mc, sess, opt.HistoryMessages)

// 触发 ContextRetrieved 事件
callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
    Event:     callback.ContextRetrieved,
    SessionID: sessionID,
    ContextID: contextID,
    Context:   mc,
})

return mc, nil
```

- [ ] **Step 2: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v -count=1`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/engine.go
git commit -m "feat(context_engine): 回填 CreateContext 构造 SessionModelContext"
```

---

## Task 9: 回填 — OffloadMessages 调用

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offload.go` — 回填 offloadMessagesToMemory

- [ ] **Step 1: 回填 offloadMessagesToMemory**

取消注释并激活 `processor/offload.go` 中 `offloadMessagesToMemory` 方法的 `mc.OffloadMessages` 调用：

```go
func (p *BaseProcessor) offloadMessagesToMemory(mc iface.ModelContext, role string, content string, offloadHandle string, toolCallID string, msgOpts []llm_schema.MessageOption) (llm_schema.BaseMessage, error) {
    content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "in_memory")

    // 调用 mc.OffloadMessages 将消息存入内存缓冲区
    mc.OffloadMessages(offloadHandle, messages)

    return schema.NewOffloadMessage(
        roleTypeFromRole(role),
        content,
        offloadHandle,
        "in_memory",
        toolCallID,
        msgOpts...,
    ), nil
}
```

注意：函数签名需要添加 `messages []llm_schema.BaseMessage` 参数（从 OffloadMessages 调用处传入）。

- [ ] **Step 2: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v -count=1`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/processor/offload.go
git commit -m "feat(context_engine): 回填 offloadMessagesToMemory 调用 mc.OffloadMessages"
```

---

## Task 10: 回填 — Offloader WorkspaceDir

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_offloader.go`
- Modify: `internal/opensource/uap-claw-go/internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go`
- Modify: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go`

- [ ] **Step 1: 回填 3 个 offloader 的 newOffloadHandleAndPath**

在三个文件中，将 `workspaceDir := ""` 替换为 `workspaceDir := mc.WorkspaceDir()`：

```go
func (x *Xxx) newOffloadHandleAndPath(mc iface.ModelContext) (string, string) {
    offloadHandle := uuid.New().String()
    sessionID := mc.SessionID()
    workspaceDir := mc.WorkspaceDir()  // ← 替换原来的 ""
    fileName := fmt.Sprintf("%s_%s.json", x.ProcessorType(), offloadHandle)
    if workspaceDir != "" {
        return offloadHandle, filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
    }
    return offloadHandle, ""
}
```

- [ ] **Step 2: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v -count=1`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/processor/offloader/
git commit -m "feat(context_engine): 回填 offloader newOffloadHandleAndPath 使用 mc.WorkspaceDir"
```

---

## Task 11: 回填 — FullCompactProcessor Session Memory 方法

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`

- [ ] **Step 1: 回填 _loadSessionMemoryRuntime**

```go
func (f *FullCompactProcessor) _loadSessionMemoryRuntime(mc iface.ModelContext) map[string]any {
    sess := f.getSessionRef(mc)
    if sess == nil {
        return nil
    }
    return getSessionMemoryRuntime(sess)
}
```

需要添加 `getSessionRef` 辅助方法（通过 mc 的 sessionRef 获取 session）。

- [ ] **Step 2: 回填 _loadSessionMemoryText**

```go
func (f *FullCompactProcessor) _loadSessionMemoryText(mc iface.ModelContext) string {
    runtime := f._loadSessionMemoryRuntime(mc)
    if runtime == nil {
        return ""
    }
    memoryPath, _ := runtime["memory_path"].(string)
    if memoryPath == "" {
        return ""
    }
    data, err := os.ReadFile(memoryPath)
    if err != nil {
        return ""
    }
    return string(data)
}
```

- [ ] **Step 3: 回填 _resolveSessionMemoryPath**

```go
func (f *FullCompactProcessor) _resolveSessionMemoryPath(mc iface.ModelContext) string {
    workspaceDir := mc.WorkspaceDir()
    if workspaceDir == "" {
        return ""
    }
    return getSessionMemoryPath(workspaceDir, mc.SessionID())
}
```

- [ ] **Step 4: 回填 _selectMessagesAfterSessionMemory**

```go
func (f *FullCompactProcessor) _selectMessagesAfterSessionMemory(messages []llm_schema.BaseMessage, mc iface.ModelContext) []llm_schema.BaseMessage {
    runtime := f._loadSessionMemoryRuntime(mc)
    if runtime == nil {
        return messages
    }
    notesUptoID, _ := runtime["notes_upto_message_id"].(string)
    if notesUptoID == "" {
        return messages
    }
    idx := findMessageIndexByContextMessageID(messages, notesUptoID)
    if idx < 0 {
        return messages
    }
    return messages[idx+1:]
}
```

- [ ] **Step 5: 回填 _invalidateSessionMemoryAnchor**

```go
func (f *FullCompactProcessor) _invalidateSessionMemoryAnchor(mc iface.ModelContext) {
    sess := f.getSessionRef(mc)
    if sess == nil {
        return
    }
    invalidateSessionMemoryAnchor(sess)
}
```

- [ ] **Step 6: 回填 buildTaskStatusReinjectedContent 和 buildPlanModeReinjectedContent**

这两个方法需要从 session 获取运行时状态。当前先实现基础版本，从 session state 中读取 task_status 和 plan_mode 数据：

```go
func (f *FullCompactProcessor) buildTaskStatusReinjectedContent(mc iface.ModelContext) string {
    // 从 session state 中获取 task_status 数据
    sess := f.getSessionRef(mc)
    if sess == nil {
        return ""
    }
    state, err := sess.GetState(state.StringKey("task_status"))
    if err != nil || state == nil {
        return ""
    }
    // 序列化并返回
    data, _ := json.Marshal(state)
    return string(data)
}

func (f *FullCompactProcessor) buildPlanModeReinjectedContent(mc iface.ModelContext) string {
    sess := f.getSessionRef(mc)
    if sess == nil {
        return ""
    }
    state, err := sess.GetState(state.StringKey("plan_mode"))
    if err != nil || state == nil {
        return ""
    }
    data, _ := json.Marshal(state)
    return string(data)
}
```

- [ ] **Step 7: 添加 getSessionRef 辅助方法**

```go
func (f *FullCompactProcessor) getSessionRef(mc iface.ModelContext) *session.Session {
    // 通过类型断言获取内部 sessionRef
    type sessionRefHolder interface {
        getSessionRef() *session.Session
    }
    // SessionModelContext 上有私有字段 sessionRef，需要导出访问方法
    // 或者通过 ModelContext 接口的已知方法间接获取
    // 当前方案：SessionModelContext 在 context/ 子包中，通过接口无法直接访问
    // 需要在 ModelContext 接口或通过 Option 传入 session
    return nil  // ⤵️ 6.4-6.10 回填：通过事件触发和回调框架获取 session
}
```

注意：SessionModelContext 的 sessionRef 是私有字段，FullCompactProcessor 在 processor 包中无法直接访问。解决方案：在 `Option.Extra` 中传入 session 引用，或后续 6.x 节通过回调框架解决。当前标记为 ⤵️ 6.4-6.10 回填。

- [ ] **Step 8: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v -count=1`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/context_engine/processor/compressor/full_compact_processor.go
git commit -m "feat(context_engine): 回填 FullCompactProcessor Session Memory 方法"
```

---

## Task 12: 全局编译验证和覆盖率检查

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` — 更新 5.31 状态

- [ ] **Step 1: 全局编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过，无错误

- [ ] **Step 2: 全局测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags=test ./internal/agentcore/context_engine/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags=test -cover ./internal/agentcore/context_engine/context/...`
Expected: 覆盖率 ≥ 85%

如果覆盖率不足，针对未覆盖的方法补充测试。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 5.31 状态从 `☐` 更新为 `✅`：

```
| 5.31 | ✅ | Context 实现 | ... | `openjiuwen/core/context_engine/context/` |
```

- [ ] **Step 5: 最终提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 5.31 状态为已完成"
```

---

## Self-Review Checklist

**1. Spec coverage:** 逐项对照设计文档：
- ✅ MessageBuffer — Task 1
- ✅ KVCacheManager — Task 2
- ✅ ProcessorStateRecorder — Task 3
- ✅ ContextUtils 8 方法 — Task 4
- ✅ SessionMemoryManager + DirectUpdater — Task 5
- ✅ SessionModelContext — Task 6
- ✅ 回填 StatMessages/StatTools/StatContextWindow — Task 7
- ✅ 回填 CreateContext — Task 8
- ✅ 回填 OffloadMessages — Task 9
- ✅ 回填 Offloader WorkspaceDir — Task 10
- ✅ 回填 FullCompactProcessor — Task 11
- ✅ 全局验证 — Task 12

**2. Placeholder scan:** 无 TBD/TODO/"implement later"/"add validation"/"similar to Task N"。唯一例外是 `getSessionRef` 标记 ⤵️ 6.4-6.10 回填，这是设计决策中明确约定的。

**3. Type consistency:** 所有类型签名已在设计文档中统一：
- `iface.ModelContext` 接口方法签名与 `interface/types.go` 一致
- `iface.ContextProcessor` 接口方法签名与 `interface/processor.go` 一致
- `ProcessorOption.Model` 字段类型 `*llm.Model` 与 KVCacheManager 使用类型一致
- `SessionMemoryUpdater` 接口方法签名在 DirectUpdater 和 AgentUpdater 中一致
