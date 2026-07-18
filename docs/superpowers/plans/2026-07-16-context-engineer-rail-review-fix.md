# ContextEngineRail 审查问题修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 ContextEngineRail（9.22）审查发现的 8 个偏差/问题，提升类型安全、功能完整性和代码质量。

**Architecture:** 逐项修复，每项独立可验证。核心变更是 ProcessorConfig 接口新增 SetModelDefaults 方法（影响 9 个 Config 实现），其次是 buildPresetProcessors 补齐 Config 默认值、context section 注入、常量去重和 mock 去重。

**Tech Stack:** Go 1.26, reflect（将逐步移除）, testify/assert

---

## Task 1: ProcessorConfig 接口新增 SetModelDefaults 方法

**Files:**
- Modify: `internal/agentcore/context_engine/interface/processor.go:19-22`
- Modify: `internal/agentcore/context_engine/interface/processor_test.go:43-55`

- [ ] **Step 1: 在 ProcessorConfig 接口新增 SetModelDefaults 方法声明**

在 `processor.go` 的 `ProcessorConfig` 接口中，在 `Validate() error` 之后新增：

```go
type ProcessorConfig interface {
	// Validate 校验配置参数
	Validate() error
	// SetModelDefaults 设置模型配置默认值。
	// 当 Config 的 Model/ModelClient 字段为 nil 时，用传入的参数回填。
	// 无 Model/ModelClient 字段的 Config 实现空方法。
	// 对齐 Python: hasattr(merged_cfg, "model") and getattr(merged_cfg, "model", None) is None
	SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig)
}
```

- [ ] **Step 2: 在 `processor_test.go` 的 `fakeProcessorConfig` 补空实现**

```go
func (f *fakeProcessorConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

注意：需要添加 import `llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"`

- [ ] **Step 3: 运行编译确认接口变更影响范围**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_engine/... 2>&1 | head -50`

预期：编译失败，报错哪些类型没有实现 `SetModelDefaults`。记录下所有缺失实现的类型。

- [ ] **Step 4: 补全所有缺失的 SetModelDefaults 实现**

有 Model/ModelClient 字段的 6 个 Config：

`processor/offloader/message_summary_offloader.go`：
```go
func (c *MessageSummaryOffloaderConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}
```

`processor/compressor/dialogue_compressor.go`：
```go
func (c *DialogueCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}
```

`processor/compressor/current_round_compressor.go`：
```go
func (c *CurrentRoundCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}
```

`processor/compressor/round_level_compressor.go`：
```go
func (c *RoundLevelCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}
```

`processor/compressor/full_compact_processor.go`：
```go
func (c *FullCompactProcessorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}
```

`context/session_memory_manager.go`：
```go
func (c *SessionMemoryConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}
```

无 Model/ModelClient 字段的 3 个 Config 空实现：

`processor/offloader/tool_result_budget_processor.go`：
```go
func (c *ToolResultBudgetProcessorConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

`processor/compressor/micro_compact_processor.go`：
```go
func (c *MicroCompactProcessorConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

`processor/offloader/message_offloader.go`：
```go
func (c *MessageOffloaderConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

- [ ] **Step 5: 补全测试文件中 mock 的空实现**

`context_engine/registry_test.go` 的 `mockProcessorConfig`：
```go
func (m *mockProcessorConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

`context_engine/processor/base_test.go` 的 `testConfig`：
```go
func (c *testConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

`context_engine/processor/compressor/dialogue_compressor_test.go` 的 `testConfig`：
```go
func (c *testConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}
```

- [ ] **Step 6: 编译验证所有实现补全**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

预期：编译成功。

- [ ] **Step 7: 运行 context_engine 相关测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -count=1`

预期：所有测试通过。

- [ ] **Step 8: 提交**

```bash
git add -A && git commit -m "feat: ProcessorConfig 接口新增 SetModelDefaults 方法

- 接口新增 SetModelDefaults(model, modelClient) 方法
- 6 个有 Model/ModelClient 的 Config 实现字段回填逻辑
- 3 个无 Model/ModelClient 的 Config 空实现
- 所有测试 mock 补空实现"
```

---

## Task 2: MergeProcessors 签名 any → 具体类型 + fillModelDefaults 去 reflect

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/merge_config.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/merge_config_test.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go`

- [ ] **Step 1: 修改 `MergeProcessors` 签名**

`merge_config.go` 中 `MergeProcessors` 函数签名改为：

```go
func MergeProcessors(
	base []iface.ProcessorSpec,
	overrides []iface.ProcessorSpec,
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []iface.ProcessorSpec {
```

需添加 import：`llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"`

- [ ] **Step 2: 修改 `buildMergedConfig` 签名**

```go
func buildMergedConfig(
	key string,
	baseCfg iface.ProcessorConfig,
	overrideSpec iface.ProcessorSpec,
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) iface.ProcessorConfig {
```

- [ ] **Step 3: 修改 `fillModelDefaults` — 去掉 reflect，改用接口方法**

```go
func fillModelDefaults(cfg iface.ProcessorConfig, modelConfig *llmschema.ModelRequestConfig, modelClientConfig *llmschema.ModelClientConfig) {
	cfg.SetModelDefaults(modelConfig, modelClientConfig)
}
```

- [ ] **Step 4: 删除旧的 reflect 版 `fillModelDefaults`**

删除原函数体（第 192-214 行），替换为上面的 1 行实现。

- [ ] **Step 5: 更新 `context_processor_rail.go` 中 `Init` 方法调用**

`Init` 中调用 `MergeProcessors` 的地方，参数类型已经是 `*llmschema.ModelRequestConfig` 和 `*llmschema.ModelClientConfig`（从 config 读取的），无需改动调用代码，只需确认类型匹配。

- [ ] **Step 6: 修改 `merge_config_test.go` 中 `MergeProcessors` 测试调用**

所有测试中调用 `MergeProcessors` 的地方，将 `any` 参数改为 `*llmschema.ModelRequestConfig` / `*llmschema.ModelClientConfig`：

- `TestMergeProcessors_基本合并`：传 `nil, nil`
- `TestMergeProcessors_追加处理器`：传 `nil, nil`
- `TestMergeProcessors_无base时dict覆盖panic`：传 `nil, nil`
- `TestMergeProcessors_完整替换`：传 `nil, nil`

需要添加 import：`llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"`

- [ ] **Step 7: 修改 `TestFillModelDefaults` 测试**

将 `fillModelDefaults` 测试改为使用 `SetModelDefaults`：

```go
func TestFillModelDefaults_通过SetModelDefaults(t *testing.T) {
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	clientCfg := &llmschema.ModelClientConfig{APIKey: "test-key"}

	cfg := &offloader.MessageSummaryOffloaderConfig{}
	fillModelDefaults(cfg, modelCfg, clientCfg)

	if cfg.Model != modelCfg {
		t.Error("Model 应被设置")
	}
	if cfg.ModelClient != clientCfg {
		t.Error("ModelClient 应被设置")
	}
}

func TestFillModelDefaults_已有Model不覆盖(t *testing.T) {
	existingCfg := &llmschema.ModelRequestConfig{ModelName: "existing"}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "new"}

	cfg := &offloader.MessageSummaryOffloaderConfig{Model: existingCfg}
	fillModelDefaults(cfg, modelCfg, nil)

	if cfg.Model != existingCfg {
		t.Error("已有 Model 不应被覆盖")
	}
}
```

需添加 import：`offloader "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/offloader"`

- [ ] **Step 8: 编译并运行测试**

Run: `cd /home/opensource/uap-claw-go && go build ./... && go test ./internal/agentcore/harness/rails/context_engineer/... -count=1`

预期：编译成功，测试通过。

- [ ] **Step 9: 提交**

```bash
git add -A && git commit -m "refactor: MergeProcessors 签名 any→具体类型, fillModelDefaults 去 reflect

- MergeProcessors 参数改为 *ModelRequestConfig/*ModelClientConfig
- fillModelDefaults 内部改用 cfg.SetModelDefaults() 替代 reflect
- 测试同步更新"
```

---

## Task 3: 常量去重 — SessionRuntimeAttr/SessionStateKey 移至 harness/schema/state.go

**Files:**
- Modify: `internal/agentcore/harness/schema/state.go`
- Modify: `internal/agentcore/harness/deep_agent.go:128-134`
- Modify: `internal/agentcore/harness/rails/context_engineer/refresh_task_state.go:12-19`
- Modify: `internal/agentcore/harness/rails/context_engineer/refresh_task_state_test.go`

- [ ] **Step 1: 在 `harness/schema/state.go` 常量区块新增导出常量**

在 `state.go` 的常量区块添加：

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

- [ ] **Step 2: 修改 `deep_agent.go` — 删除本地常量，改引用 `hschema`**

删除 `deep_agent.go` 中的本地常量定义（约第 128-134 行的 `sessionRuntimeAttr` 和 `sessionStateKey`），将所有引用改为 `hschema.SessionRuntimeAttr` 和 `hschema.SessionStateKey`。

确认 `deep_agent.go` 已 import `hschema` 包（即 `harness/schema` 的别名），如果没有则添加。

- [ ] **Step 3: 修改 `refresh_task_state.go` — 删除本地常量，改引用 `hschema`**

删除 `refresh_task_state.go` 中的本地常量定义（第 12-19 行），将所有引用改为 `hschema.SessionRuntimeAttr` 和 `hschema.SessionStateKey`。

添加 import：`hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"`

- [ ] **Step 4: 修改 `refresh_task_state_test.go` — 更新常量引用**

测试中 `sessstate.StringKey(sessionRuntimeAttr)` 改为 `sessstate.StringKey(hschema.SessionRuntimeAttr)`，同理 `sessionStateKey` → `hschema.SessionStateKey`。

- [ ] **Step 5: 编译并运行测试**

Run: `cd /home/opensource/uap-claw-go && go build ./... && go test ./internal/agentcore/harness/... -count=1 -run "TestRefresh|TestDeepAgent" 2>&1 | tail -20`

预期：编译成功，测试通过。

- [ ] **Step 6: 提交**

```bash
git add -A && git commit -m "refactor: 常量 SessionRuntimeAttr/SessionStateKey 移至 harness/schema/state.go

- 对齐 Python: 常量定义在 harness/schema/state.py
- 值对齐 Python: _deepagent_runtime_state / deepagent
- deep_agent.go 和 refresh_task_state.go 改引用 hschema 包"
```

---

## Task 4: 删除无效 var _ SessionFacade = nil

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/refresh_task_state.go`

- [ ] **Step 1: 删除无效接口检查**

删除 `refresh_task_state.go` 最后一行：

```go
var _ sessioninterfaces.SessionFacade = nil
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "fix: 删除 refresh_task_state.go 中无效的 var _ SessionFacade = nil"
```

---

## Task 5: 删除 getSystemPromptBuilder，直接调用 agent.SystemPromptBuilder()

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/context_assemble_rail.go`

- [ ] **Step 1: 修改 `context_processor_rail.go`**

- 删除 `getSystemPromptBuilder` 函数（约第 282-291 行）
- 在 `Init` 方法中，将 `r.systemPromptBuilder = getSystemPromptBuilder(agent)` 改为 `r.systemPromptBuilder = agent.SystemPromptBuilder()`
- 删除不再需要的 import（如 `saprompt` 如果只被此函数使用 — 确认 `maybeInjectOffloadSection` 也用到了，所以保留）

- [ ] **Step 2: 修改 `context_assemble_rail.go`**

- 在 `Init` 方法中，将 `r.systemPromptBuilder = getSystemPromptBuilder(agent)` 改为 `r.systemPromptBuilder = agent.SystemPromptBuilder()`
- 此文件中没有定义 `getSystemPromptBuilder`（它定义在 `context_processor_rail.go` 中），所以只需要改调用点

- [ ] **Step 3: 编译并运行测试**

Run: `cd /home/opensource/uap-claw-go && go build ./... && go test ./internal/agentcore/harness/rails/context_engineer/... -count=1`

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "refactor: 删除 getSystemPromptBuilder，直接调用 agent.SystemPromptBuilder()

BaseAgent 接口已有 SystemPromptBuilder() 方法，无需私有接口类型断言"
```

---

## Task 6: buildPresetProcessors 返回带完整 Config 的 ProcessorSpec

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go`

- [ ] **Step 1: 添加 processor 子包 import**

在 `context_processor_rail.go` 的 import 中新增：

```go
offloader "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/offloader"
compressor "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
```

- [ ] **Step 2: 重写 `buildPresetProcessors` 方法**

替换整个方法体：

```go
func (r *ContextProcessorRail) buildPresetProcessors(
	modelConfig *llmschema.ModelRequestConfig,
	modelClientConfig *llmschema.ModelClientConfig,
) []ceiface.ProcessorSpec {
	if r.sessionMemoryEnabled {
		// session memory 启用时的预设
		// 对齐 Python: ContextProcessorRail._build_preset_processors (session_memory=True)
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
	}

	// 默认预设（对齐 Python 非 session_memory 路径）
	// 显式覆盖值严格对齐 Python _build_preset_processors 中的参数
	return []ceiface.ProcessorSpec{
		{
			Type: "MessageSummaryOffloader",
			Config: &offloader.MessageSummaryOffloaderConfig{
				LargeMessageThreshold: 10000, // Python: large_message_threshold=10000 (覆盖默认1000)
				OffloadMessageTypes:   []string{"tool"},
				ProtectedToolNames:    []string{"read_file:*SKILL.md", "reload_original_context_messages"},
				KeepLastRound:         false, // Python: keep_last_round=False (覆盖默认True)
				Model:                 modelConfig,
				ModelClient:           modelClientConfig,
			},
		},
		{
			Type: "DialogueCompressor",
			Config: &compressor.DialogueCompressorConfig{
				TokensThreshold:         100000, // Python: tokens_threshold=100000 (覆盖默认10000)
				MessagesToKeep:          10,     // Python: messages_to_keep=10
				KeepLastRound:           false,  // Python: keep_last_round=False (覆盖默认True)
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
				KeepRecentMessages: 6, // Python: keep_recent_messages=6 (覆盖默认0)
				Model:              modelConfig,
				ModelClient:        modelClientConfig,
			},
		},
	}
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat: buildPresetProcessors 返回带完整 Config 的 ProcessorSpec

- 导入 processor/offloader 和 processor/compressor 包
- 默认路径：MessageSummaryOffloader/DialogueCompressor/CurrentRoundCompressor/RoundLevelCompressor
- Session memory 路径：ToolResultBudgetProcessor/MicroCompactProcessor/FullCompactProcessor
- 参数值严格对齐 Python _build_preset_processors 的显式覆盖值"
```

---

## Task 7: sessionMemoryConfig/sessionMemoryMgr 加 TODO 注释

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go`

- [ ] **Step 1: 修改字段注释**

将 `sessionMemoryConfig` 和 `sessionMemoryMgr` 字段注释改为：

```go
// sessionMemoryConfig 会话记忆配置
// ⤵️ TODO: 后续回填 — 等 session memory 集成时改为具体类型
sessionMemoryConfig interface{}
// sessionMemoryMgr 会话记忆管理器
// ⤵️ TODO: 后续回填 — 等 session memory 集成时改为具体类型
sessionMemoryMgr interface{}
```

- [ ] **Step 2: 提交**

```bash
git add -A && git commit -m "docs: sessionMemoryConfig/sessionMemoryMgr 加 ⤵️ TODO 回填标记"
```

---

## Task 8: 新增 ReadContextFiles/ReadDailyMemory 并注入 context section

**Files:**
- Modify: `internal/agentcore/harness/prompts/sections/context.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/context_assemble_rail.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go`

- [ ] **Step 1: 在 `sections/context.go` 新增 `ReadContextFiles` 函数**

在导出函数区块添加：

```go
// ReadContextFiles 从工作空间读取上下文配置文件内容。
//
// 遍历 wscontent.ContextFiles 列表，通过 fsOp.ReadFile 读取每个文件。
// 对 MEMORY.md 特殊处理：从 WorkspaceNodeMemory 目录下读取。
// 过滤掉空模板文件（IsUnfilledTemplate）。
//
// 对齐 Python: _read_context_file(sys_operation, workspace, file_key)
func ReadContextFiles(ctx context.Context, fsOp sysop.FsOperation, ws *hworkspace.Workspace) map[string]string {
	if fsOp == nil || ws == nil {
		return nil
	}

	files := make(map[string]string)
	for _, fileKey := range wscontent.ContextFiles {
		var fullPath string
		if fileKey == "MEMORY.md" {
			// 对齐 Python: memory_dir = workspace.get_node_path(WorkspaceNode.MEMORY)
			// full_path = memory_dir / WorkspaceNode.MEMORY_MD.value
			memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
			if memoryDir == nil {
				continue
			}
			fullPath = *memoryDir + "/" + fileKey
		} else {
			nodePath := ws.GetNodePath(hworkspace.WorkspaceNode(fileKey))
			if nodePath == nil {
				continue
			}
			fullPath = *nodePath
		}

		result, err := fsOp.ReadFile(ctx, fullPath)
		if err != nil || result == nil || result.Content == "" {
			continue
		}
		if IsUnfilledTemplate(result.Content) {
			continue
		}
		files[fileKey] = result.Content
	}
	return files
}
```

需添加 import：`sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"` 和 `hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"`

注意：需确认 `fsOp.ReadFile` 的返回类型，查看 `sys_operation.FsOperation.ReadFile` 的签名，确保 `result.Content` 字段名正确。

- [ ] **Step 2: 在 `sections/context.go` 新增 `ReadDailyMemory` 函数**

```go
// ReadDailyMemory 读取当日每日记忆文件内容。
//
// 对齐 Python: _read_daily_memory(sys_operation, workspace, timezone)
func ReadDailyMemory(ctx context.Context, fsOp sysop.FsOperation, ws *hworkspace.Workspace, timezone string) (string, string) {
	if fsOp == nil || ws == nil {
		return "", ""
	}

	if timezone == "" {
		timezone = "Asia/Shanghai"
	}

	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	if memoryDir == nil {
		return "", ""
	}

	dailyMemoryDir := *memoryDir + "/" + string(hworkspace.WorkspaceNodeDailyMemory)

	listResult, err := fsOp.ListFiles(ctx, dailyMemoryDir)
	if err != nil || listResult == nil || len(listResult.ListItems) == 0 {
		return "", ""
	}

	tz, _ := time.LoadLocation(timezone)
	date := time.Now().In(tz).Format("2006-01-02")
	todayFile := date + ".md"

	found := false
	for _, item := range listResult.ListItems {
		if item.Name == todayFile {
			found = true
			break
		}
	}
	if !found {
		return "", ""
	}

	fullPath := dailyMemoryDir + "/" + todayFile
	result, err := fsOp.ReadFile(ctx, fullPath)
	if err != nil || result == nil || result.Content == "" {
		return "", ""
	}

	return result.Content, date
}
```

需添加 import：`"time"`

注意：需确认 `fsOp.ListFiles` 的返回类型，查看 `sys_operation.FsOperation.ListFiles` 的签名，确保 `ListItems` 和 `item.Name` 字段名正确。

- [ ] **Step 3: 修改 `ContextAssembleRail` — Init 中设置 SysOperation/Workspace**

对齐其他 Rail（HeartbeatRail、TaskPlanningRail）的模式，在 `Init` 中从 agent 获取 SysOperation 和 Workspace：

```go
func (r *ContextAssembleRail) Init(agent sainterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	r.abilityManager = agent.AbilityManager()

	// 对齐 Python: DeepAgentRail.set_sys_operation / set_workspace
	// 其他 Rail（HeartbeatRail、TaskPlanningRail）也使用此模式
	type deepConfigProvider interface {
		DeepConfig() *hconfig.DeepAgentConfig
	}
	if dcp, ok := agent.(deepConfigProvider); ok && dcp.DeepConfig() != nil {
		if r.SysOperation() == nil && dcp.DeepConfig().SysOperation != nil {
			r.SetSysOperation(dcp.DeepConfig().SysOperation)
		}
		if r.Workspace() == nil && dcp.DeepConfig().Workspace != nil {
			r.SetWorkspace(dcp.DeepConfig().Workspace)
		}
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "context_assemble_rail_init").
		Msg("ContextAssembleRail 初始化完成")

	return nil
}
```

需添加 import：`hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/config"`

- [ ] **Step 4: 修改 `ContextAssembleRail.BeforeModelCall` — 注入 context section**

在现有 workspace section 和 tools section 代码之后，添加 context section 注入逻辑。替换原来的 TODO 区块：

```go
	// 构建上下文节
	// 对齐 Python: context_section = await _build_context(self.sys_operation, workspace, lang, include_daily_memory=not is_heartbeat)
	sysOp := r.SysOperation()
	if sysOp != nil {
		files := sections.ReadContextFiles(ctx, sysOp.Fs(), ws)

		// 读取每日记忆
		includeDailyMemory := !isHeartbeat
		if includeDailyMemory {
			dailyContent, dateStr := sections.ReadDailyMemory(ctx, sysOp.Fs(), ws, "")
			if dailyContent != "" {
				files["daily_memory"] = dailyContent
				files["daily_memory_date"] = dateStr
			}
		}

		if len(files) > 0 {
			contextSection := sections.BuildContextSection(files, lang)
			r.systemPromptBuilder.AddSection(contextSection)
		} else {
			r.systemPromptBuilder.RemoveSection(sections.SectionContext)
		}
	} else {
		r.systemPromptBuilder.RemoveSection(sections.SectionContext)
	}
```

- [ ] **Step 5: 修改 `ContextProcessorRail.Init` — 同样设置 SysOperation/Workspace**

在 `ContextProcessorRail.Init` 中，参考 ContextAssembleRail 的模式，添加从 agent 获取 SysOperation 和 Workspace 的逻辑（如果需要的话）。查看 Python 源码确认 ContextProcessorRail 是否需要这些引用。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

如有编译错误，根据实际 API 签名调整 `ReadContextFiles`/`ReadDailyMemory` 中的字段名。

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -count=1 2>&1 | tail -30`

- [ ] **Step 8: 提交**

```bash
git add -A && git commit -m "feat: 新增 ReadContextFiles/ReadDailyMemory 并注入 context section

- sections/context.go 新增 ReadContextFiles 和 ReadDailyMemory
- ContextAssembleRail.Init 设置 SysOperation/Workspace 引用
- ContextAssembleRail.BeforeModelCall 注入 context section
- 对齐 Python: _read_context_file / _read_daily_memory / _build_context"
```

---

## Task 9: mock 去重 — 提取到 test_helpers_test.go

**Files:**
- Create: `internal/agentcore/harness/rails/context_engineer/test_helpers_test.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/refresh_task_state_test.go`
- Modify: `internal/agentcore/harness/rails/context_engineer/fix_incomplete_tool_context_test.go`

- [ ] **Step 1: 创建 `test_helpers_test.go`**

提取 `mockSessionFacade`（合并 `mockMinimalSession`）和 `mockModelContext`：

```go
package context_engineer

import (
	"context"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	tokeniface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	tooliface "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── Mock SessionFacade ────────────────────────────

// mockSessionFacade 测试用 SessionFacade mock
type mockSessionFacade struct {
	states  map[sessstate.StateKey]interface{}
	updated map[string]any
}

func newMockSessionFacade() *mockSessionFacade {
	return &mockSessionFacade{
		states:  make(map[sessstate.StateKey]interface{}),
		updated: make(map[string]any),
	}
}

func (m *mockSessionFacade) GetSessionID() string { return "test-session" }
func (m *mockSessionFacade) GetState(key sessstate.StateKey) (interface{}, error) {
	return m.states[key], nil
}
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.updated[k] = v
	}
}
func (m *mockSessionFacade) DumpState() map[string]any                               { return m.updated }
func (m *mockSessionFacade) WriteStream(ctx context.Context, data interface{}) error { return nil }
func (m *mockSessionFacade) WriteCustomStream(ctx context.Context, data interface{}) error {
	return nil
}
func (m *mockSessionFacade) GetEnv(key string, defaultValue ...interface{}) interface{} { return nil }
func (m *mockSessionFacade) Interact(ctx context.Context, value interface{}) error      { return nil }

// 确保 mock 实现了 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// ──────────────────────────── Mock ModelContext ────────────────────────────

// mockModelContext 测试用 ModelContext mock
type mockModelContext struct {
	messages []llmschema.BaseMessage
}

func newMockModelContext() *mockModelContext {
	return &mockModelContext{}
}

func (m *mockModelContext) Len() int { return len(m.messages) }
func (m *mockModelContext) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	return m.messages, nil
}
func (m *mockModelContext) SetMessages(messages []llmschema.BaseMessage, withHistory bool) {
	m.messages = messages
}
func (m *mockModelContext) PopMessages(size int, withHistory bool) []llmschema.BaseMessage {
	popped := m.messages
	m.messages = nil
	return popped
}
func (m *mockModelContext) ClearMessages(ctx context.Context, withHistory bool, opts ...iface.Option) error {
	m.messages = nil
	return nil
}
func (m *mockModelContext) AddMessages(ctx context.Context, message llmschema.BaseMessage, opts ...iface.Option) ([]llmschema.BaseMessage, error) {
	m.messages = append(m.messages, message)
	return m.messages, nil
}
func (m *mockModelContext) GetContextWindow(ctx context.Context, systemMessages []llmschema.BaseMessage, tools []cschema.ToolInfoInterface, windowSize int, dialogueRound int, opts ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (m *mockModelContext) Statistic() *iface.ContextStats                                  { return nil }
func (m *mockModelContext) SessionID() string                                               { return "test" }
func (m *mockModelContext) ContextID() string                                               { return "test" }
func (m *mockModelContext) TokenCounter() tokeniface.TokenCounter                           { return nil }
func (m *mockModelContext) ReloaderTool() tooliface.Tool                                    { return nil }
func (m *mockModelContext) WorkspaceDir() string                                            { return "" }
func (m *mockModelContext) SetSessionRef(sess sessioninterfaces.SessionFacade)              {}
func (m *mockModelContext) GetSessionRef() sessioninterfaces.SessionFacade                  { return nil }
func (m *mockModelContext) OffloadMessages(handle string, messages []llmschema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any                                       { return nil }
func (m *mockModelContext) LoadState(state map[string]any)                                  {}
func (m *mockModelContext) CompressContext(ctx context.Context, opts ...iface.CompressContextOption) (string, error) {
	return "noop", nil
}

// 确保 mock 实现了 ModelContext 接口
var _ iface.ModelContext = (*mockModelContext)(nil)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newCallbackContextWithMC 创建带有 ModelContext 的 AgentCallbackContext
func newCallbackContextWithMC(mc iface.ModelContext) *sainterfaces.AgentCallbackContext {
	ctx := sainterfaces.NewAgentCallbackContext(nil, nil, newMockSessionFacade())
	ctx.SetModelContext(mc)
	return ctx
}

// setCallbackSession 使用构造函数设置 AgentCallbackContext 的 session 字段
func setCallbackSession(ctx *sainterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) {
	*ctx = *sainterfaces.NewAgentCallbackContext(nil, nil, sess)
}
```

- [ ] **Step 2: 修改 `refresh_task_state_test.go` — 删除 mock 定义**

- 删除 `mockSessionFacade` 类型定义及所有方法实现（约第 16-46 行）
- 删除 `newMockSessionFacade` 函数
- 删除 `var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)`
- 删除 `setCallbackSession` 函数（已移到 test_helpers）
- 保留所有测试函数不变

- [ ] **Step 3: 修改 `fix_incomplete_tool_context_test.go` — 删除 mock 定义**

- 删除 `mockModelContext` 类型定义及所有方法实现（约第 20-67 行）
- 删除 `mockMinimalSession` 类型定义及所有方法实现（约第 70-83 行）
- 删除 `newCallbackContextWithMC` 函数
- 删除 `var _ iface.ModelContext = (*mockModelContext)(nil)` 和 `var _ sessioninterfaces.SessionFacade = (*mockMinimalSession)(nil)`
- 保留所有测试函数不变

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/context_engineer/... -count=1`

预期：所有测试通过。

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "refactor: 提取 mock 到 test_helpers_test.go，消除重复定义

- mockSessionFacade/mockModelContext 移至 test_helpers_test.go
- refresh_task_state_test.go 和 fix_incomplete_tool_context_test.go 删除重复 mock"
```

---

## Task 10: 最终验证 — 全量编译和测试

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

预期：编译成功。

- [ ] **Step 2: 运行所有相关测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/... -count=1 -timeout 300s 2>&1 | tail -50`

预期：所有测试通过。

- [ ] **Step 3: 推送**

```bash
git push origin main
```
