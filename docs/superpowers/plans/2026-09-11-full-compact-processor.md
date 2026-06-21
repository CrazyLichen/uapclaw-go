# FullCompactProcessor 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.24 FullCompactProcessor 全量压缩处理器，创建 compressor/ 子包，迁移已有处理器，实现双路径压缩（Session Memory 优先 + LLM 回退）和状态重注入。

**Architecture:** 将 processor/ 下已有的 DialogueCompressor 和 MicroCompactProcessor 迁移到新建的 compressor/ 子包，删除 context_utils/ 目录将其函数移入 compressor/util.go，新增 FullCompactProcessor 完整实现。基类基础设施（BaseProcessor 等）留在 processor/ 不动。

**Tech Stack:** Go 1.22+，项目已有的 llm.Model / BaseMessage / ModelContext 接口体系

---

### Task 1: 创建 compressor/ 子包骨架 + util.go

**Files:**
- Create: `internal/agentcore/context_engine/processor/compressor/doc.go`
- Create: `internal/agentcore/context_engine/processor/compressor/util.go`

- [ ] **Step 1: 创建 compressor/ 目录**

```bash
mkdir -p internal/agentcore/context_engine/processor/compressor
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package compressor 提供上下文引擎的压缩处理器实现。
//
// 包含多种压缩策略，从轻量级到重量级渐进式介入：
//   - MicroCompactProcessor：清除旧工具结果内容（不调用 LLM）
//   - DialogueCompressor：对话轮次级压缩（调用 LLM）
//   - FullCompactProcessor：全量压缩，最后防线（调用 LLM 或使用 Session Memory）
//
// 文件目录：
//
//	compressor/
//	├── doc.go                     # 包文档
//	├── util.go                    # 包级共享函数
//	├── dialogue_compressor.go     # DialogueCompressor 对话压缩器
//	├── micro_compact_processor.go # MicroCompactProcessor 微压缩处理器
//	└── full_compact_processor.go  # FullCompactProcessor 全量压缩处理器
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/compressor/
package compressor
```

- [ ] **Step 3: 创建 util.go，从 context_utils 迁移 3 个函数**

从 `internal/agentcore/context_engine/context_utils/resolve.go` 迁移 `ExtractToolName`、`ResolveToolCallFromMessage`、`ResolveToolNameFromMessage`，改包名为 `compressor`，import 路径更新。

```go
package compressor

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractToolName 从 ToolCall 中提取工具名称。
//
// 优先返回 ToolCall.Name，为空时返回空字符串。
// Go 端 ToolCall 结构直接包含 Name 字段（与 Python 的 Function.Name 不同）。
//
// 对应 Python: ContextUtils.extract_tool_name()
func ExtractToolName(toolCall *llm_schema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	if toolCall.Name != "" {
		return toolCall.Name
	}
	return ""
}

// ResolveToolCallFromMessage 从 ToolMessage 回溯查找对应的 ToolCall 对象。
//
// 通过 ToolMessage 的 ToolCallID 匹配 AssistantMessage.ToolCalls 中的 ID，
// 从后往前遍历 contextMessages 查找最近的匹配。
//
// 对应 Python: ContextUtils.resolve_tool_call_from_message()
func ResolveToolCallFromMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) *llm_schema.ToolCall {
	tm, ok := message.(*llm_schema.ToolMessage)
	if !ok {
		return nil
	}
	toolCallID := tm.ToolCallID
	if toolCallID == "" {
		return nil
	}

	for i := len(contextMessages) - 1; i >= 0; i-- {
		am, ok := contextMessages[i].(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, tc := range am.ToolCalls {
			if tc.ID == toolCallID {
				return tc
			}
		}
	}
	return nil
}

// ResolveToolNameFromMessage 从 ToolMessage 回溯查找对应的工具名称。
//
// 内部调用 ResolveToolCallFromMessage 找到 ToolCall，再调用 ExtractToolName 提取名称。
// 未找到时返回空字符串。
//
// 对应 Python: ContextUtils.resolve_tool_name_from_message()
func ResolveToolNameFromMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) string {
	toolCall := ResolveToolCallFromMessage(message, contextMessages)
	return ExtractToolName(toolCall)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 迁移 context_utils 测试文件**

将 `internal/agentcore/context_engine/context_utils/resolve_test.go` 迁移到 `internal/agentcore/context_engine/processor/compressor/util_test.go`，包名改为 `compressor`，import 路径更新。

- [ ] **Step 5: 删除 context_utils/ 目录**

```bash
rm -rf internal/agentcore/context_engine/context_utils/
```

- [ ] **Step 6: 更新 processor/doc.go，移除 context_utils 条目，添加 compressor/ 条目**

- [ ] **Step 7: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/...
```

预期：编译失败，因为 micro_compact_processor.go 还在引用已删除的 context_utils。这是正常的，Task 2 会修复。

---

### Task 2: 迁移 DialogueCompressor 和 MicroCompactProcessor 到 compressor/ 子包

**Files:**
- Move: `internal/agentcore/context_engine/processor/dialogue_compressor.go` → `internal/agentcore/context_engine/processor/compressor/dialogue_compressor.go`
- Move: `internal/agentcore/context_engine/processor/micro_compact_processor.go` → `internal/agentcore/context_engine/processor/compressor/micro_compact_processor.go`
- Move: `internal/agentcore/context_engine/processor/dialogue_compressor_test.go` → `internal/agentcore/context_engine/processor/compressor/dialogue_compressor_test.go`
- Move: `internal/agentcore/context_engine/processor/micro_compact_processor_test.go` → `internal/agentcore/context_engine/processor/compressor/micro_compact_processor_test.go`
- Modify: 迁移文件中的 `package processor` → `package compressor`
- Modify: 迁移文件中的 import 路径更新（BaseProcessor 等需引用 processor 父包）
- Modify: `internal/agentcore/context_engine/processor/doc.go` 更新文件目录

- [ ] **Step 1: 移动 dialogue_compressor.go**

```bash
mv internal/agentcore/context_engine/processor/dialogue_compressor.go internal/agentcore/context_engine/processor/compressor/dialogue_compressor.go
```

- [ ] **Step 2: 修改 dialogue_compressor.go 的包名和 import**

将 `package processor` 改为 `package compressor`，更新 import：
- `BaseProcessor` → 需 import processor 父包：`"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"`，使用 `processor.BaseProcessor`
- `context_engine` 包引用保持不变
- `iface` 引用保持不变
- 所有对 processor 包内函数/类型的引用需加 `processor.` 前缀（如 `processor.NewBaseProcessor`、`processor.GroupCompletedAPIRounds`、`processor.Replacement`、`processor.ReplaceMessages`）

- [ ] **Step 3: 移动 dialogue_compressor_test.go 并更新**

```bash
mv internal/agentcore/context_engine/processor/dialogue_compressor_test.go internal/agentcore/context_engine/processor/compressor/dialogue_compressor_test.go
```

包名改为 `compressor`，更新所有 import 路径，对 processor 包内引用加 `processor.` 前缀。

- [ ] **Step 4: 移动 micro_compact_processor.go 并更新**

```bash
mv internal/agentcore/context_engine/processor/micro_compact_processor.go internal/agentcore/context_engine/processor/compressor/micro_compact_processor.go
```

包名改为 `package compressor`，import 更新：
- 删除 `context_utils` import
- 引用 `context_utils.ResolveToolNameFromMessage` → 改为 `ResolveToolNameFromMessage`（同包内）
- 添加 processor 父包 import，`BaseProcessor` → `processor.BaseProcessor`

- [ ] **Step 5: 移动 micro_compact_processor_test.go 并更新**

```bash
mv internal/agentcore/context_engine/processor/micro_compact_processor_test.go internal/agentcore/context_engine/processor/compressor/micro_compact_processor_test.go
```

包名改为 `compressor`，更新所有 import 路径。

- [ ] **Step 6: 更新 processor/doc.go**

从文件目录中移除 `dialogue_compressor.go` 和 `micro_compact_processor.go`，添加 `compressor/` 子包条目。

- [ ] **Step 7: 编译并运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/... && go test ./internal/agentcore/context_engine/... -count=1
```

预期：编译通过，所有已有测试通过。

- [ ] **Step 8: 提交**

```bash
git add -A && git commit -m "refactor: 创建 compressor/ 子包，迁移 DialogueCompressor 和 MicroCompactProcessor"
```

---

### Task 3: FullCompactProcessorConfig 配置结构体 + Validate + NewFullCompactProcessorConfig 构造函数

**Files:**
- Create: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`

- [ ] **Step 1: 编写 FullCompactProcessorConfig 测试**

在 `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go` 中编写配置验证测试：

```go
func TestFullCompactProcessorConfig_Validate(t *testing.T) {
	t.Run("默认配置有效", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		assert.NoError(t, cfg.Validate())
	})
	t.Run("TriggerTotalTokens必须大于0", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		cfg.TriggerTotalTokens = 0
		assert.Error(t, cfg.Validate())
	})
	t.Run("CompressionCallMaxTokens必须大于0", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		cfg.CompressionCallMaxTokens = 0
		assert.Error(t, cfg.Validate())
	})
	t.Run("StateSnapshotMaxChars必须大于0", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		cfg.StateSnapshotMaxChars = 0
		assert.Error(t, cfg.Validate())
	})
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestFullCompactProcessorConfig -v
```

预期：编译失败，FullCompactProcessorConfig 未定义。

- [ ] **Step 3: 在 full_compact_processor.go 中实现 FullCompactProcessorConfig + Validate + 构造函数**

```go
package compressor

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FullCompactProcessorConfig 全量压缩处理器配置。
//
// 当上下文窗口 token 数超过 TriggerTotalTokens 时触发全量压缩，
// 将历史消息替换为 LLM 生成的摘要，仅保留最近 MessagesToKeep 条原文。
//
// 对应 Python: FullCompactProcessorConfig (pydantic.BaseModel)
type FullCompactProcessorConfig struct {
	// TriggerTotalTokens 触发全量压缩的 token 阈值，默认 180000
	TriggerTotalTokens int
	// CompressionCallMaxTokens 压缩调用最大 token 预算，默认 200000
	CompressionCallMaxTokens int
	// MessagesToKeep 保留最近 N 条消息原文，默认 10
	MessagesToKeep int
	// SessionMemoryEnabled 优先使用 session memory 而非 LLM 全量压缩，默认 true
	SessionMemoryEnabled bool
	// Model 压缩模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 压缩模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
	// KeepToolMessagePairs 保留 tool result 时也保留匹配的 assistant tool-call，默认 true
	KeepToolMessagePairs bool
	// StateSnapshotMaxChars 每个状态重注入快照最大字符数，默认 4000
	StateSnapshotMaxChars int
	// ReinjectRecentSkills 重注入最近 N 个 skill 读取轮次，默认 3
	ReinjectRecentSkills int
	// ReinjectFileToolNames 文件相关状态重注入工具名
	ReinjectFileToolNames []string
	// ReinjectToolResultHintNames 工具结果提示工具名
	ReinjectToolResultHintNames []string
	// Marker 压缩边界标记，默认 "[FULL_COMPACT_BOUNDARY]"
	Marker string
	// StateMarker 状态消息标记，默认 "[FULL_COMPACT_STATE]"
	StateMarker string
	// SyntheticUserMarker 合成 user 标记
	SyntheticUserMarker string
	// SummaryIntro 摘要前缀文本
	SummaryIntro string
	// RecentMessagesNotice 保留消息提示文本
	RecentMessagesNotice string
	// SessionMemoryMarker session memory 边界标记
	SessionMemoryMarker string
	// SessionMemoryIntro session memory 前缀文本
	SessionMemoryIntro string
}

// Validate 校验配置字段。
func (c *FullCompactProcessorConfig) Validate() error {
	if c.TriggerTotalTokens <= 0 {
		return fmt.Errorf("FullCompactProcessorConfig.TriggerTotalTokens 必须大于 0，当前值: %d", c.TriggerTotalTokens)
	}
	if c.CompressionCallMaxTokens <= 0 {
		return fmt.Errorf("FullCompactProcessorConfig.CompressionCallMaxTokens 必须大于 0，当前值: %d", c.CompressionCallMaxTokens)
	}
	if c.StateSnapshotMaxChars <= 0 {
		return fmt.Errorf("FullCompactProcessorConfig.StateSnapshotMaxChars 必须大于 0，当前值: %d", c.StateSnapshotMaxChars)
	}
	return nil
}

// NewFullCompactProcessorConfig 创建默认配置实例。
func NewFullCompactProcessorConfig() *FullCompactProcessorConfig {
	defaultFileToolNames := []string{"read_file", "write_file", "edit_file", "glob", "grep"}
	return &FullCompactProcessorConfig{
		TriggerTotalTokens:          180000,
		CompressionCallMaxTokens:    200000,
		MessagesToKeep:              10,
		SessionMemoryEnabled:        true,
		KeepToolMessagePairs:        true,
		StateSnapshotMaxChars:       4000,
		ReinjectRecentSkills:        3,
		ReinjectFileToolNames:       defaultFileToolNames,
		ReinjectToolResultHintNames: append([]string{}, defaultFileToolNames...),
		Marker:                 "[FULL_COMPACT_BOUNDARY]",
		StateMarker:            "[FULL_COMPACT_STATE]",
		SyntheticUserMarker:    "[earlier conversation truncated for compaction retry]",
		SummaryIntro:           "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.",
		RecentMessagesNotice:   "Recent messages are preserved verbatim.",
		SessionMemoryMarker:    "[SESSION_MEMORY_BOUNDARY]",
		SessionMemoryIntro:     "Earlier conversation has been replaced with the session memory file. Use it as the canonical summary of prior work.",
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestFullCompactProcessorConfig -v
```

预期：PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 添加 FullCompactProcessorConfig 配置结构体"
```

---

### Task 4: FullCompactProcessor 结构体 + 工厂注册 + TriggerAddMessages

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写 FullCompactProcessor 结构体和 TriggerAddMessages 测试**

```go
func TestFullCompactProcessor_TriggerAddMessages(t *testing.T) {
	t.Run("非完整API轮次不触发", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		p := NewFullCompactProcessor(cfg)
		mc := &fakeModelContext{tokenCount: 200000}
		// 只有 user 消息，不是完整 API round
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
		}
		triggered, err := p.TriggerAddMessages(context.Background(), mc, messages)
		assert.NoError(t, err)
		assert.False(t, triggered)
	})
	t.Run("完整API轮次且token超阈值触发", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		p := NewFullCompactProcessor(cfg)
		mc := &fakeModelContext{tokenCount: 200000}
		// user + assistant = 完整 API round
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewAssistantMessage("hi"),
		}
		triggered, err := p.TriggerAddMessages(context.Background(), mc, messages)
		assert.NoError(t, err)
		assert.True(t, triggered)
	})
	t.Run("完整API轮次但token未超阈值不触发", func(t *testing.T) {
		cfg := NewFullCompactProcessorConfig()
		p := NewFullCompactProcessor(cfg)
		mc := &fakeModelContext{tokenCount: 100000} // 低于 180000
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewAssistantMessage("hi"),
		}
		triggered, err := p.TriggerAddMessages(context.Background(), mc, messages)
		assert.NoError(t, err)
		assert.False(t, triggered)
	})
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 FullCompactProcessor 结构体、NewFullCompactProcessor 构造函数、TriggerAddMessages、ProcessorType、init() 注册**

```go
// FullCompactProcessor 全量压缩处理器，上下文管理的"最后防线"。
//
// 当其他更轻量的处理器都无法将上下文降到安全阈值时，FullCompact 启动全量压缩：
// 将历史对话整体替换为 LLM 生成的摘要，仅保留最近 N 条消息原文。
// 优先使用 Session Memory 路径，不可用时回退到 LLM 全量压缩。
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/full_compact_processor.py (FullCompactProcessor)
type FullCompactProcessor struct {
	*processor.BaseProcessor
	// fcpConfig 全量压缩处理器具体配置
	fcpConfig *FullCompactProcessorConfig
	// model 压缩用 LLM 实例
	model *llm.Model
	// reinjector 状态重注入器
	reinjector *FullCompactStateReinjector
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFullCompactProcessor 创建全量压缩处理器实例。
func NewFullCompactProcessor(config *FullCompactProcessorConfig) *FullCompactProcessor {
	bp := processor.NewBaseProcessor(config)
	p := &FullCompactProcessor{
		BaseProcessor: bp,
		fcpConfig:     config,
		reinjector:    newFullCompactStateReinjector(),
	}
	if config.Model != nil && config.ModelClient != nil {
		p.model = llm.NewModel(config.ModelClient, config.Model)
	}
	return p
}

// ProcessorType 返回处理器类型标识。
func (p *FullCompactProcessor) ProcessorType() string {
	return "FullCompactProcessor"
}

// TriggerAddMessages 判断是否触发全量压缩。
//
// 条件：(1) 消息构成完整 API 轮次 (2) 上下文窗口 token 数 > TriggerTotalTokens
func (p *FullCompactProcessor) TriggerAddMessages(ctx context.Context, mc iface.ModelContext, messages []llm_schema.BaseMessage, opts ...iface.Option) (bool, error) {
	if !p.IsAPIRound(messages) {
		return false, nil
	}
	totalTokens := p.countContextWindowTokens(mc)
	return totalTokens > p.fcpConfig.TriggerTotalTokens, nil
}

func init() {
	context_engine.RegisterProcessorFactory("FullCompactProcessor", func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		cfg, ok := config.(*FullCompactProcessorConfig)
		if !ok {
			return nil, fmt.Errorf("FullCompactProcessor: 配置类型错误，期望 *FullCompactProcessorConfig")
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		return NewFullCompactProcessor(cfg), nil
	})
}
```

同时实现 `countContextWindowTokens` 辅助方法。

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestFullCompactProcessor_TriggerAddMessages -v
```

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactProcessor 结构体和 TriggerAddMessages"
```

---

### Task 5: 消息识别方法族 + 序列化方法

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写消息识别和序列化测试**

测试用例覆盖：
- `_isBoundaryMessage`：SystemMessage 以 Marker 开头 → true，其他 → false
- `_isStateMessage`：UserMessage 以 StateMarker 开头 → true，其他 → false
- `_isSessionMemoryBoundaryMessage`：SystemMessage 以 SessionMemoryMarker 开头 → true
- `_isSessionMemorySummaryMessage`：UserMessage 以 SessionMemoryIntro 开头 → true
- `_isSyntheticMarkerMessage`：UserMessage 内容 == SyntheticUserMarker → true
- `_serializeMessage`：UserMessage / AssistantMessage(含ToolCalls) / ToolMessage 各一种
- `_serializeMessages`：多条消息序列化
- `_formatSummary`：含 `<analysis>` + `<summary>` 标签 / 无标签 / 仅有 `<summary>`

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现消息识别方法族和序列化方法**

5 个消息识别方法 + `_serializeMessage` + `_serializeMessages` + `_formatSummary` + `_buildFallbackSummary` + `_makeStateMessage` + `truncateStateText`

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactProcessor 消息识别和序列化方法"
```

---

### Task 6: 消息选择与截断方法

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写测试**

测试用例覆盖：
- `_selectMessagesToKeep`：正常保留最近 N 条 / 消息不足 N 条 / 空
- `_adjustStartIndexForToolPairs`：ToolMessage 在保留边界时向前扩展包含对应 AssistantMessage
- `_truncateForPromptBudget`：消息超预算时从前往后丢弃 API round

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 `_selectMessagesToKeep` + `_adjustStartIndexForToolPairs` + `_truncateForPromptBudget` + `_findLastCompactionBoundaryIndex`**

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactProcessor 消息选择与截断方法"
```

---

### Task 7: FullCompactStateReinjector + 4 个 Builder

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写 Reinjector 和 Builder 测试**

测试用例覆盖：
- `FullCompactStateReinjector.RegisterBuilder` / `IterBuilders`
- `buildPlanReinjectedContent`：返回空字符串
- `buildSkillReinjectedContent`：含 skill 读取的 API round → 返回 UserMessage 列表；无 skill 读取 → 返回 nil
- `buildTaskStatusReinjectedContent`：⤵️ 5.31 回填，当前返回空字符串
- `buildPlanModeReinjectedContent`：⤵️ 5.31 回填，当前返回空字符串
- `buildReinjectedStateMessages`：集成测试，验证完整重注入消息序列构建

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 `ReinjectedStateBuilderSpec` + `FullCompactStateReinjector` + 全局单例 `defaultReinjector` + 4 个 Builder + `buildReinjectedStateMessages`**

`buildTaskStatusReinjectedContent` 和 `buildPlanModeReinjectedContent` 内部用 ⤵️ 标注，返回空字符串。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactStateReinjector 和 4 个 Builder"
```

---

### Task 8: LLM 全量压缩路径 — _generateSummary + _buildFullCompactMessages

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写 LLM 压缩路径测试**

使用 fake LLM Model 测试：
- `_generateSummary`：fake Model 返回含 `<analysis>` + `<summary>` 的响应 → 正确提取摘要
- `_generateSummary`：fake Model 返回 error → 回退到 `_buildFallbackSummary`
- `_buildFullCompactMessages`：完整流程 → 返回 prefix + boundary + summary + kept + reinjectedState
- 无 model 配置时 → 使用 `_buildFallbackSummary`

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 `_generateSummary` + `_buildFullCompactMessages`**

包含 BASE_COMPACT_PROMPT 完整常量（从 Python 翻译）。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactProcessor LLM 全量压缩路径"
```

---

### Task 9: Session Memory 路径 + OnAddMessages 主入口

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写 Session Memory 路径和 OnAddMessages 测试**

测试用例覆盖：
- `_buildSessionMemoryMessages`：SessionMemoryEnabled=false → 返回 nil，走 LLM 路径
- `_buildReplacementMessages`：无历史 boundary → prefix 为空，activeMessages = allMessages
- `_buildReplacementMessages`：有历史 boundary → 正确分割 prefix + activeMessages
- `OnAddMessages`：完整流程集成测试（fake Model + fake ModelContext）

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 Session Memory 路径方法（⤵️ 标注待回填部分）+ `_buildReplacementMessages` + `OnAddMessages`**

Session Memory 路径的 5 个方法（`_loadSessionMemoryRuntime` / `_loadSessionMemoryText` / `_resolveSessionMemoryPath` / `_selectMessagesAfterSessionMemory` / `_invalidateSessionMemoryAnchor`）内部用 `// ⤵️ 5.31 回填` 注释标注，返回零值/nil，确保 Session Memory 路径当前不可用时自动回退到 LLM 路径。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactProcessor Session Memory 路径和 OnAddMessages 主入口"
```

---

### Task 10: SaveState/LoadState + _countPromptTokens + 遗留辅助方法

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go`

- [ ] **Step 1: 编写测试**

测试用例覆盖：
- `SaveState` / `LoadState`：返回空 map / 空操作
- `_countPromptTokens`：通过 fake TokenCounter 计算
- `_buildSummaryMessage` / `_buildSessionMemoryMessage`：消息文本构建
- `_buildHeadTailTruncatedText`：头尾截断

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 `SaveState` + `LoadState` + `_countPromptTokens` + `_buildSummaryMessage` + `_buildSessionMemoryMessage` + `_buildHeadTailTruncatedText`**

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(compressor): 实现 FullCompactProcessor 状态持久化和辅助方法"
```

---

### Task 11: 更新 IMPLEMENTATION_PLAN.md + doc.go + 最终验证

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` — 5.24 行状态改为 ✅，5.21 中引用 compressor 迁移信息
- Modify: `internal/agentcore/context_engine/doc.go` — 更新文件目录
- Modify: `internal/agentcore/context_engine/processor/doc.go` — 更新文件目录
- Modify: `internal/agentcore/context_engine/processor/compressor/doc.go` — 最终确认文件目录

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

5.24 行：`☐` → `✅`，补充完成内容描述。
5.21 行：如有提及 processor 迁移，更新说明。

- [ ] **Step 2: 更新各级 doc.go 文件目录**

- [ ] **Step 3: 全量编译和测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test ./internal/agentcore/context_engine/... -count=1 -cover
```

预期：编译通过，所有测试通过，覆盖率 ≥ 85%。

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "docs: 更新 5.24 FullCompactProcessor 实现状态和文档"
```
