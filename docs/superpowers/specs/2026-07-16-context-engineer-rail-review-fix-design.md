# ContextEngineRail 审查问题修复设计

## 概述

ContextEngineRail（9.22）实现完成后审查发现 8 个偏差/问题，本设计文档逐项定义修复方案。

---

## #1 `MergeProcessors` 参数 `any` → 具体类型 + `fillModelDefaults` 去 reflect

### 问题

`MergeProcessors` 的 `modelConfig`/`modelClientConfig` 参数使用 `any`，`fillModelDefaults` 内部用 reflect 设置 Model/ModelClient 字段。编译期无法检查类型安全。

### 修复

**1.1 `ProcessorConfig` 接口新增 `SetModelDefaults` 方法**

文件：`internal/agentcore/context_engine/interface/processor.go`

```go
type ProcessorConfig interface {
    Validate() error
    SetModelDefaults(model *llmschema.ModelRequestConfig, modelClient *llmschema.ModelClientConfig)
}
```

**1.2 所有 ProcessorConfig 实现添加 `SetModelDefaults`**

有 Model/ModelClient 字段的 Config（7个），实现逻辑：字段为 nil 且参数非 nil 时赋值。

- `MessageSummaryOffloaderConfig` — `processor/offloader/message_summary_offloader.go`
- `DialogueCompressorConfig` — `processor/compressor/dialogue_compressor.go`
- `CurrentRoundCompressorConfig` — `processor/compressor/current_round_compressor.go`
- `RoundLevelCompressorConfig` — `processor/compressor/round_level_compressor.go`
- `FullCompactProcessorConfig` — `processor/compressor/full_compact_processor.go`
- `MessageOffloaderConfig` — `processor/offloader/message_offloader.go`
- `SessionMemoryConfig` — `context/session_memory_manager.go`

无 Model/ModelClient 字段的 Config（2个），空实现：

- `ToolResultBudgetProcessorConfig` — `processor/offloader/tool_result_budget_processor.go`
- `MicroCompactProcessorConfig` — `processor/compressor/micro_compact_processor.go`

**1.3 `MergeProcessors`/`fillModelDefaults`/`buildMergedConfig` 签名改具体类型**

文件：`internal/agentcore/harness/rails/context_engineer/merge_config.go`

```go
func MergeProcessors(
    base []iface.ProcessorSpec,
    overrides []iface.ProcessorSpec,
    modelConfig *llmschema.ModelRequestConfig,
    modelClientConfig *llmschema.ModelClientConfig,
) []iface.ProcessorSpec

func fillModelDefaults(cfg iface.ProcessorConfig, modelConfig *llmschema.ModelRequestConfig, modelClientConfig *llmschema.ModelClientConfig) {
    cfg.SetModelDefaults(modelConfig, modelClientConfig)
}
```

**1.4 测试中的 fake Config 补空实现**

- `context_engine/interface/processor_test.go` 中的 `fakeProcessorConfig`
- `context_engine/registry_test.go` 中的 `mockProcessorConfig`
- `context_engine/processor/base_test.go` 中的 `testConfig`
- `context_engine/processor/compressor/dialogue_compressor_test.go` 中的 `testConfig`

**1.5 `merge_config_test.go` 中 `MergeProcessors` 测试改传具体类型**

---

## #2 `sessionMemoryConfig`/`sessionMemoryMgr` 用 `interface{}`

### 问题

两个字段用 `interface{}` 占位，无类型约束。

### 修复

保留字段，加 `// ⤵️ TODO: 后续回填` 注释标记，等 session memory 集成章节实现时再定义具体类型。

文件：`internal/agentcore/harness/rails/context_engineer/context_processor_rail.go`

```go
// sessionMemoryConfig 会话记忆配置
// ⤵️ TODO: 后续回填 — 等 session memory 集成时改为具体类型
sessionMemoryConfig interface{}
// sessionMemoryMgr 会话记忆管理器
// ⤵️ TODO: 后续回填 — 等 session memory 集成时改为具体类型
sessionMemoryMgr interface{}
```

---

## #3 `buildPresetProcessors` 只返回 Type 不返回 Config

### 问题

Go 中 `buildPresetProcessors` 只返回 `Type`，Config 留空。Python 中每个 preset 都有完整 Config 实例，且对部分默认值做了显式覆盖。

### 修复

**3.1 导入 processor 子包的 Config struct**

文件：`context_processor_rail.go` 新增 import：

```go
offloader "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/offloader"
compressor "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
```

**3.2 `buildPresetProcessors` 返回带完整 Config 的 ProcessorSpec**

默认路径（session_memory_enabled=false），对齐 Python `_build_preset_processors` 非session memory路径：

```go
return []ceiface.ProcessorSpec{
    {
        Type: "MessageSummaryOffloader",
        Config: &offloader.MessageSummaryOffloaderConfig{
            LargeMessageThreshold: 10000,  // Python: large_message_threshold=10000 (覆盖默认1000)
            OffloadMessageTypes:   []string{"tool"},
            ProtectedToolNames:    []string{"read_file:*SKILL.md", "reload_original_context_messages"},
            KeepLastRound:         false,  // Python: keep_last_round=False (覆盖默认True)
            Model:                 modelConfig,
            ModelClient:           modelClientConfig,
        },
    },
    {
        Type: "DialogueCompressor",
        Config: &compressor.DialogueCompressorConfig{
            TokensThreshold:         100000,  // Python: tokens_threshold=100000 (覆盖默认10000)
            MessagesToKeep:          10,      // Python: messages_to_keep=10
            KeepLastRound:           false,   // Python: keep_last_round=False (覆盖默认True)
            CompressionTargetTokens: 1800,
            Model:                   modelConfig,
            ModelClient:             modelClientConfig,
        },
    },
    {
        Type: "CurrentRoundCompressor",
        Config: &compressor.CurrentRoundCompressorConfig{
            TokensThreshold: 100000,
            MessagesToKeep:  3,
            Model:           modelConfig,
            ModelClient:     modelClientConfig,
        },
    },
    {
        Type: "RoundLevelCompressor",
        Config: &compressor.RoundLevelCompressorConfig{
            TriggerTotalTokens: 230000,
            TargetTotalTokens:  160000,
            KeepRecentMessages: 6,  // Python: keep_recent_messages=6 (覆盖默认0)
            Model:              modelConfig,
            ModelClient:        modelClientConfig,
        },
    },
}
```

Session memory 路径（session_memory_enabled=true）：

```go
return []ceiface.ProcessorSpec{
    {
        Type:   "ToolResultBudgetProcessor",
        Config: &offloader.ToolResultBudgetProcessorConfig{},
    },
    {
        Type:   "MicroCompactProcessor",
        Config: &compressor.MicroCompactProcessorConfig{},
    },
    {
        Type: "FullCompactProcessor",
        Config: &compressor.FullCompactProcessorConfig{
            Model:       modelConfig,
            ModelClient: modelClientConfig,
        },
    },
}
```

**3.3 MessagesToKeep 字段类型适配**

`MessageSummaryOffloaderConfig.MessagesToKeep` 是 `*int`（Python 中 `messages_to_keep=None`），Go 中传 `nil`。
`DialogueCompressorConfig.MessagesToKeep` 是 `int`，传 `10`。

---

## #4 `getSystemPromptBuilder` 脆弱类型断言

### 问题

`getSystemPromptBuilder` 定义私有接口 `promptBuilderProvider` 做类型断言，但 `BaseAgent` 接口已有 `SystemPromptBuilder()` 方法。

### 修复

- 删除 `getSystemPromptBuilder` 函数
- 所有调用点（`ContextProcessorRail.Init`、`ContextAssembleRail.Init`）改为 `agent.SystemPromptBuilder()`

文件：`context_processor_rail.go`、`context_assemble_rail.go`

---

## #5 重复常量 `sessionRuntimeAttr`/`sessionStateKey`

### 问题

`deep_agent.go` 和 `refresh_task_state.go` 各自定义了同名同值的非导出常量。Python 中这两个常量定义在 `harness/schema/state.py`，两处 import 同一来源。

### 修复

**5.1 在 `harness/schema/state.go` 新增导出常量**

```go
const (
    // SessionRuntimeAttr 会话运行时属性键
    // 对齐 Python: _SESSION_RUNTIME_ATTR = "_deepagent_runtime_state"
    SessionRuntimeAttr = "_deepagent_runtime_state"

    // SessionStateKey 会话状态持久化键
    // 对齐 Python: _SESSION_STATE_KEY = "deepagent"
    SessionStateKey = "deepagent"
)
```

注意：Python 中值为 `"deepagent"` 和 `"_deepagent_runtime_state"`，Go 中当前值为 `"deep_agent_state"` 和 `"_deep_agent_runtime_state"`。需确认是否对齐 Python 值。

**5.2 `deep_agent.go` 删除本地常量，改引用 `hschema.SessionRuntimeAttr`/`hschema.SessionStateKey`**

**5.3 `refresh_task_state.go` 删除本地常量，改引用 `hschema.SessionRuntimeAttr`/`hschema.SessionStateKey`**

---

## #6 无效 `var _ SessionFacade = nil`

### 问题

`var _ sessioninterfaces.SessionFacade = nil` 永远不会触发编译错误。正确的接口合规检查已在 test 文件中。

### 修复

删除 `refresh_task_state.go` 中的 `var _ sessioninterfaces.SessionFacade = nil`。

---

## #7 缺 context section 注入

### 问题

`ContextAssembleRail.BeforeModelCall` 缺少 context section 注入。Python 中通过 `sys_operation.fs()` 读取 workspace 配置文件（AGENT.md 等）和每日记忆，注入到 System Prompt。

### 修复

**7.1 在 `sections/context.go` 新增 `ReadContextFiles` 函数**

```go
func ReadContextFiles(ctx context.Context, fsOp sysop.FsOperation, ws *workspace.Workspace) map[string]string
```

逻辑对齐 Python `_read_context_file`：
- 遍历 `wscontent.ContextFiles` 列表
- 对 MEMORY.md 特殊处理：`ws.GetNodePath(WorkspaceNodeMEMORY)` + `MEMORY.md`
- 其他文件：`ws.GetNodePath(fileKey)`
- 通过 `fsOp.ReadFile(path)` 读取内容
- 调用 `IsUnfilledTemplate` 过滤空模板

**7.2 在 `sections/context.go` 新增 `ReadDailyMemory` 函数**

```go
func ReadDailyMemory(ctx context.Context, fsOp sysop.FsOperation, ws *workspace.Workspace, timezone string) (string, string)
```

逻辑对齐 Python `_read_daily_memory`：
- `ws.GetNodePath(WorkspaceNodeMEMORY)` + `WorkspaceNodeDAILYMEMORY`
- `fsOp.ListFiles(dailyMemoryDir)` 列出文件
- 检查当日文件 `{date}.md` 是否存在
- 存在则 `fsOp.ReadFile(fullPath)` 读取内容
- 返回 (content, dateStr)

**7.3 `ContextAssembleRail.BeforeModelCall` 中注入 context section**

在 workspace section 和 tools section 之后，新增：
1. 获取 `r.SysOperation()` — 从 `DeepAgentRail` 继承
2. 调用 `sections.ReadContextFiles` 读取配置文件
3. 调用 `sections.ReadDailyMemory` 读取每日记忆
4. 将 daily memory 加入 files map
5. 调用已有的 `sections.BuildContextSection(files, lang)` 构建 section
6. `r.systemPromptBuilder.AddSection(contextSection)` 注入

**7.4 `ContextAssembleRail.Init` 中也需要设置 SysOperation/Workspace**

对齐 Python `ContextAssembleRail.init` 和其他 Rail（如 HeartbeatRail、TaskPlanningRail）的模式：从 agent 的 DeepConfig 获取 SysOperation 和 Workspace，设置到 Rail 自身。

**7.5 提示词一比一复刻 Python**

Go 中 `workspace_content/workspace_header.go` 的常量已经与 Python 一比一对齐，`BuildContextSection` 的逻辑也已对齐。新增的 `ReadContextFiles`/`ReadDailyMemory` 不涉及新提示词，只是读取文件内容后传入已有函数。

---

## #8 mock 代码冗余

### 问题

`refresh_task_state_test.go` 和 `fix_incomplete_tool_context_test.go` 重复定义了 `mockSessionFacade`/`mockMinimalSession`。

### 修复

**8.1 新建 `test_helpers_test.go`**

提取共享 mock：
- `mockSessionFacade`（合并 `mockMinimalSession`，统一实现）
- `mockModelContext`

**8.2 两个 test 文件删除重复 mock 定义**

---

## 实施顺序

1. #1 — ProcessorConfig 接口变更 + MergeProcessors 签名（影响最广，先做）
2. #5 — 常量去重（简单，为后续改动铺垫）
3. #6 — 删除无效检查（简单）
4. #4 — 删除 getSystemPromptBuilder（简单）
5. #3 — buildPresetProcessors 完整 Config（依赖 #1 的 SetModelDefaults）
6. #2 — sessionMemory 字段 TODO 标记（简单）
7. #7 — context section 注入（最大新增工作量）
8. #8 — mock 去重（测试清理）
