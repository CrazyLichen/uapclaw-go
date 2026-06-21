# MessageOffloader 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.27 MessageOffloader — 上下文引擎的消息卸载处理器，当消息数/Token 数超阈值时裁剪大消息并卸载到文件系统或内存。

**Architecture:** 先将 `compressor/util.go` 共享工具函数迁移到 `processor/util.go`，使 offloader 子包可复用；然后在 `processor/offloader/` 子包中实现 MessageOffloader，嵌入 `*processor.BaseProcessor`，覆写 TriggerAddMessages/OnAddMessages 等钩子方法，通过 init() 注册到 context_engine。对 5.31/9.32 的依赖采用占位对齐策略。

**Tech Stack:** Go 1.22+、标准库 `path/filepath`（替代 Python fnmatch）、`github.com/google/uuid`

---

## 文件变更映射

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新增 | `internal/agentcore/context_engine/processor/util.go` | 从 compressor 迁移的共享工具函数 |
| 新增 | `internal/agentcore/context_engine/processor/util_test.go` | 对应测试（从 compressor 迁移） |
| 删除 | `internal/agentcore/context_engine/processor/compressor/util.go` | 已迁移，删除原文件 |
| 删除 | `internal/agentcore/context_engine/processor/compressor/util_test.go` | 已迁移，删除原文件 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/dialogue_compressor.go` | 改用 `processor.XXX()` 调用 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/dialogue_compressor_test.go` | 改用 `processor.XXX()` 调用 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/micro_compact_processor.go` | 改用 `processor.XXX()` 调用 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/current_round_compressor.go` | 改用 `processor.XXX()` 调用 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go` | 改用 `processor.XXX()` 调用 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/full_compact_processor_test.go` | 改用 `processor.XXX()` 调用 |
| 修改 | `internal/agentcore/context_engine/processor/doc.go` | 更新文件目录 + 新增 util.go 条目 |
| 修改 | `internal/agentcore/context_engine/processor/compressor/doc.go` | 移除 util.go 条目 |
| 新增 | `internal/agentcore/context_engine/processor/offloader/doc.go` | offloader 子包文档 |
| 新增 | `internal/agentcore/context_engine/processor/offloader/message_offloader.go` | MessageOffloader + Config |
| 新增 | `internal/agentcore/context_engine/processor/offloader/message_offloader_test.go` | 单元测试 |

---

## Task 1: 迁移 compressor/util.go 到 processor/util.go

**Files:**
- Create: `internal/agentcore/context_engine/processor/util.go`
- Create: `internal/agentcore/context_engine/processor/util_test.go`
- Delete: `internal/agentcore/context_engine/processor/compressor/util.go`
- Delete: `internal/agentcore/context_engine/processor/compressor/util_test.go`

- [ ] **Step 1: 复制 util.go 到 processor 包，修改 package 声明和 import**

将 `compressor/util.go` 的全部内容复制到 `processor/util.go`，做以下修改：
1. `package compressor` → `package processor`
2. 删除 `import "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/processor"` （自身包不需要导入）
3. 所有对 `processor.XXX` 的调用改为直接调用 `XXX`（如同包内函数）
4. `GroupCompletedAPIRoundsMessages` 内部调用 `GroupCompletedAPIRounds` 已在同包，无需修改

同时，从 `compressor/dialogue_compressor.go` 中提取 `FindLastFinalAssistantIdx` 函数到 `processor/util.go`（因为它也是共享工具函数，MessageOffloader 的 `getOffloadRange` 需要等价功能）。

- [ ] **Step 2: 复制 util_test.go 到 processor 包，修改 package 声明**

将 `compressor/util_test.go` 的全部内容复制到 `processor/util_test.go`，做以下修改：
1. `package compressor` → `package processor`
2. 所有对 `processor.XXX` 的调用改为直接调用 `XXX`
3. import 路径中移除 `"github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/processor"` （自身包）

- [ ] **Step 3: 删除 compressor 下的原文件**

删除：
- `internal/agentcore/context_engine/processor/compressor/util.go`
- `internal/agentcore/context_engine/processor/compressor/util_test.go`

- [ ] **Step 4: 更新 compressor 包中的调用方**

所有 `compressor/` 下的 `.go` 文件，将原来直接调用的工具函数改为 `processor.XXX()` 调用，并添加 import：

```go
import "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/processor"
```

具体替换映射：

| 原调用 | 新调用 |
|--------|--------|
| `ExtractToolName(...)` | `processor.ExtractToolName(...)` |
| `ResolveToolCallFromMessage(...)` | `processor.ResolveToolCallFromMessage(...)` |
| `ResolveToolNameFromMessage(...)` | `processor.ResolveToolNameFromMessage(...)` |
| `MessageToText(...)` | `processor.MessageToText(...)` |
| `GroupCompletedAPIRoundsMessages(...)` | `processor.GroupCompletedAPIRoundsMessages(...)` |
| `MessageSignature(...)` | `processor.MessageSignature(...)` |
| `RoundSignature(...)` | `processor.RoundSignature(...)` |
| `FlattenGroups(...)` | `processor.FlattenGroups(...)` |
| `IsSkillFilePath(...)` | `processor.IsSkillFilePath(...)` |
| `ExtractArgumentValue(...)` | `processor.ExtractArgumentValue(...)` |
| `RoundContainsSkillRead(...)` | `processor.RoundContainsSkillRead(...)` |
| `EstimateContentTokens(...)` | `processor.EstimateContentTokens(...)` |
| `IsSummaryMessage(...)` | `processor.IsSummaryMessage(...)` |
| `CollectSummaryIndices(...)` | `processor.CollectSummaryIndices(...)` |
| `CountMessagesTokens(...)` | `processor.CountMessagesTokens(...)` |
| `FindLastCompletedAPIRoundEndIdx(...)` | `processor.FindLastCompletedAPIRoundEndIdx(...)` |
| `IterSummaryMergeRanges(...)` | `processor.IterSummaryMergeRanges(...)` |
| `ParseToolArguments(...)` | `processor.ParseToolArguments(...)` |
| `DescribeToolCall(...)` | `processor.DescribeToolCall(...)` |
| `FindToolResultText(...)` | `processor.FindToolResultText(...)` |
| `ExtractToolResultHint(...)` | `processor.ExtractToolResultHint(...)` |
| `ExtractSkillNameFromPath(...)` | `processor.ExtractSkillNameFromPath(...)` |
| `ExtractSkillFileContent(...)` | `processor.ExtractSkillFileContent(...)` |

需要修改的文件：
- `compressor/dialogue_compressor.go`：`CountMessagesTokens` → `processor.CountMessagesTokens`，`FindLastFinalAssistantIdx` 保留在文件内（作为本包使用的便利函数）或改用 `processor.FindLastFinalAssistantIdx`
- `compressor/dialogue_compressor_test.go`：`EstimateContentTokens` → `processor.EstimateContentTokens`
- `compressor/micro_compact_processor.go`：`ResolveToolNameFromMessage` → `processor.ResolveToolNameFromMessage`
- `compressor/current_round_compressor.go`：`CountMessagesTokens`、`FindLastCompletedAPIRoundEndIdx`、`MessageToText`、`IterSummaryMergeRanges`、`CollectSummaryIndices`、`IsSummaryMessage` → 加 `processor.` 前缀
- `compressor/full_compact_processor.go`：`MessageToText`、`FlattenGroups`、`GroupCompletedAPIRoundsMessages`、`EstimateContentTokens`、`MessageSignature`、`RoundSignature`、`RoundContainsSkillRead` → 加 `processor.` 前缀
- `compressor/full_compact_processor_test.go`：`IsSkillFilePath`、`ExtractArgumentValue`、`RoundContainsSkillRead`、`MessageToText`、`FlattenGroups` → 加 `processor.` 前缀

注意：`dialogue_compressor.go` 中的 `SerializeMessage`、`FindLastFinalAssistantIdx`、`GetCompressPairs` 是本包专用函数，不需要迁移，保留原位。但 `FindLastFinalAssistantIdx` 需要同时在 `processor/util.go` 中提供一份等价的 `FindLastFinalAssistantIdx` 导出函数供 offloader 使用。

- [ ] **Step 5: 更新 doc.go 文件**

更新 `processor/doc.go`：在文件目录中添加 `util.go` 条目。

更新 `compressor/doc.go`：从文件目录中移除 `util.go` 条目。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/...`

Expected: 编译成功，无错误。

- [ ] **Step 7: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -v -count=1`

Expected: 所有测试通过。

- [ ] **Step 8: 提交**

```bash
git add -A
git commit -m "refactor: 将 compressor/util.go 共享工具函数迁移到 processor 包"
```

---

## Task 2: 创建 offloader 子包 + MessageOffloaderConfig

**Files:**
- Create: `internal/agentcore/context_engine/processor/offloader/doc.go`
- Create: `internal/agentcore/context_engine/processor/offloader/message_offloader.go`

- [ ] **Step 1: 创建 offloader/doc.go**

```go
// Package offloader 提供上下文引擎的消息卸载处理器实现。
//
// 卸载处理器在对话消息数或 Token 数超过阈值时，将大消息的内容裁剪
// 并卸载到文件系统或内存，生成轻量占位符。原始内容可通过 reloader_tool
// 按 offload_handle 取回。
//
// 当前实现：
//   - MessageOffloader：基础裁剪卸载，将大消息截断为 trim_size + 省略标记
//
// 后续实现（5.28/5.29）将继承 MessageOffloader：
//   - MessageSummaryOffloader：用 LLM 生成摘要替代简单裁剪
//   - ToolResultBudgetProcessor：按轮次控制工具结果 Token 预算
//
// 文件目录：
//
//	offloader/
//	├── doc.go                     # 包文档
//	├── message_offloader.go       # MessageOffloader + Config
//	└── message_offloader_test.go  # 单元测试
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/offloader/
package offloader
```

- [ ] **Step 2: 创建 message_offloader.go — Config 部分**

```go
package offloader

import (
	"fmt"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageOffloaderConfig 消息卸载器配置。
//
// 规则评估顺序：
//  1. messages_to_keep：最近 N 条消息始终保留
//  2. messages_threshold：总消息数超此值时触发卸载
//  3. tokens_threshold：总 Token 数超此值时触发卸载
//
// 仅角色在 offload_message_type 中且 Token 长度大于 large_message_threshold 的消息
// 才符合卸载条件。设置 keep_last_round=True 可独立保留最后一轮对话。
//
// 对应 Python: MessageOffloaderConfig (pydantic.BaseModel)
type MessageOffloaderConfig struct {
	// MessagesThreshold 消息数触发阈值，nil 表示不启用
	MessagesThreshold *int
	// TokensThreshold Token 数触发阈值（默认 20000）
	TokensThreshold int
	// LargeMessageThreshold 大消息判定阈值（默认 1000）
	LargeMessageThreshold int
	// OffloadMessageTypes 可卸载的消息角色列表（默认 ["tool"]）
	OffloadMessageTypes []string
	// ProtectedToolNames 受保护的工具名称列表（默认 ["reload_original_context_messages"]）
	ProtectedToolNames []string
	// TrimSize 裁剪保留 Token 数（默认 100）
	TrimSize int
	// MessagesToKeep 保留最近 N 条消息，nil 表示不保留
	MessagesToKeep *int
	// KeepLastRound 保留最后一轮完整对话（默认 true）
	KeepLastRound bool
}

// MessageOffloader 消息卸载器，基于消息数/Token 数阈值触发卸载。
//
// 当对话上下文超过安全限制时，对大消息执行裁剪并卸载到外部存储，
// 生成轻量占位符以减少 Token 消耗。
//
// 对应 Python: openjiuwen/core/context_engine/processor/offloader/message_offloader.py (MessageOffloader)
type MessageOffloader struct {
	*processor.BaseProcessor
	// config 具体配置（类型断言获取）
	config *MessageOffloaderConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// omitString 省略标记，等价 Python OMIT_STRING = "..."
	omitString = "..."
	// defaultTokensThreshold 默认 Token 触发阈值
	defaultTokensThreshold = 20000
	// defaultLargeMessageThreshold 默认大消息判定阈值
	defaultLargeMessageThreshold = 1000
	// defaultTrimSize 默认裁剪保留长度
	defaultTrimSize = 100
	// defaultKeepLastRound 默认保留最后一轮
	defaultKeepLastRound = true
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageOffloader 创建消息卸载器实例。
//
// 对应 Python: MessageOffloader.__init__(config)
func NewMessageOffloader(config *MessageOffloaderConfig) (*MessageOffloader, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := processor.NewBaseProcessor(config)
	return &MessageOffloader{
		BaseProcessor: bp,
		config:        config,
	}, nil
}

// Validate 校验消息卸载器配置。
//
// 交叉校验：
//   - TrimSize < LargeMessageThreshold
//   - MessagesToKeep < MessagesThreshold（两者均非 nil 时）
//
// 对应 Python: MessageOffloader._validate_config()
func (c *MessageOffloaderConfig) Validate() error {
	// 应用默认值
	c.applyDefaults()

	if c.TrimSize >= c.LargeMessageThreshold {
		return fmt.Errorf("MessageOffloaderConfig.TrimSize(%d) 不能大于等于 LargeMessageThreshold(%d)",
			c.TrimSize, c.LargeMessageThreshold)
	}
	if c.MessagesThreshold != nil && c.MessagesToKeep != nil {
		if *c.MessagesToKeep >= *c.MessagesThreshold {
			return fmt.Errorf("MessageOffloaderConfig.MessagesToKeep(%d) 不能大于等于 MessagesThreshold(%d)",
				*c.MessagesToKeep, *c.MessagesThreshold)
		}
	}
	return nil
}

// ProcessorType 返回处理器类型标识。
func (mo *MessageOffloader) ProcessorType() string { return "MessageOffloader" }

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件（按顺序评估）：
//  1. MessagesToKeep != nil && 总消息数 <= MessagesToKeep → false
//  2. MessagesThreshold != nil && 总消息数 > MessagesThreshold → 检查候选 → true/false
//  3. 总 Token 数 > TokensThreshold → 检查候选 → true/false
//
// 对应 Python: MessageOffloader.trigger_add_messages()
func (mo *MessageOffloader) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	cfg := mo.config
	allMessages := append(mc.GetMessages(nil, true), messagesToAdd...)
	messageSize := len(allMessages)

	if cfg.MessagesToKeep != nil && messageSize <= *cfg.MessagesToKeep {
		return false, nil
	}

	if cfg.MessagesThreshold != nil && messageSize > *cfg.MessagesThreshold {
		if !mo.hasOffloadCandidate(allMessages, mc) {
			return false, nil
		}
		logger.Info(logger.ComponentAgentCore).
			Str("processor_type", mo.ProcessorType()).
			Int("message_size", messageSize).
			Int("threshold", *cfg.MessagesThreshold).
			Msg("上下文消息数超过阈值，触发卸载")
		return true, nil
	}

	// 计算 Token 数
	tokenCounter := mc.TokenCounter()
	if tokenCounter != nil {
		contextTokens, _ := tokenCounter.CountMessages(mc.GetMessages(nil, true), "")
		addTokens, _ := tokenCounter.CountMessages(messagesToAdd, "")
		tokens := contextTokens + addTokens
		if tokens > cfg.TokensThreshold {
			if !mo.hasOffloadCandidate(allMessages, mc) {
				return false, nil
			}
			logger.Info(logger.ComponentAgentCore).
				Str("processor_type", mo.ProcessorType()).
				Int("tokens", tokens).
				Int("threshold", cfg.TokensThreshold).
				Msg("上下文 Token 数超过阈值，触发卸载")
			return true, nil
		}
	}

	return false, nil
}

// OnAddMessages 执行消息卸载。
//
// 对应 Python: MessageOffloader.on_add_messages()
func (mo *MessageOffloader) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	contextMessages := mc.GetMessages(nil, true)
	allMessages := append(contextMessages, messagesToAdd...)
	contextSize := len(contextMessages)

	event, processedMessages, err := mo.offloadLargeMessages(ctx, allMessages, mc, opts...)
	if err != nil {
		return nil, messagesToAdd, err
	}

	// 分离 contextMessages 和 messagesToAdd
	updatedContext := processedMessages[:contextSize]
	updatedToAdd := processedMessages[contextSize:]
	mc.SetMessages(updatedContext, true)

	return event, updatedToAdd, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (mo *MessageOffloader) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (mo *MessageOffloader) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyDefaults 应用默认值
func (c *MessageOffloaderConfig) applyDefaults() {
	if c.TokensThreshold == 0 {
		c.TokensThreshold = defaultTokensThreshold
	}
	if c.LargeMessageThreshold == 0 {
		c.LargeMessageThreshold = defaultLargeMessageThreshold
	}
	if len(c.OffloadMessageTypes) == 0 {
		c.OffloadMessageTypes = []string{"tool"}
	}
	if len(c.ProtectedToolNames) == 0 {
		c.ProtectedToolNames = []string{"reload_original_context_messages"}
	}
	if c.TrimSize == 0 {
		c.TrimSize = defaultTrimSize
	}
	// KeepLastRound 默认 true（零值为 false，需显式设置）
	// 注意：Go 零值为 false，但 Python 默认 true
	// 调用方应显式设置；此处不强制覆盖
}

// offloadLargeMessages 遍历卸载范围，逐条卸载大消息。
//
// 对应 Python: MessageOffloader._offload_large_messages()
func (mo *MessageOffloader) offloadLargeMessages(ctx context.Context, messages []llm_schema.BaseMessage, mc iface.ModelContext, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	processedMessages := make([]llm_schema.BaseMessage, len(messages))
	copy(processedMessages, messages)

	offloadRange := mo.getOffloadRange(messages)
	event := &iface.ContextEvent{
		EventType: mo.ProcessorType(),
	}

	for idx := offloadRange - 1; idx >= 0; idx-- {
		msg := processedMessages[idx]
		if !mo.shouldOffloadMessage(msg, processedMessages, mc) {
			continue
		}
		offloadMsg, err := mo.offloadMessage(ctx, msg, mc, opts...)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", mo.ProcessorType()).
				Int("message_idx", idx).
				Err(err).
				Msg("卸载消息失败，跳过")
			continue
		}
		if offloadMsg == nil {
			continue
		}
		processedMessages = processor.ReplaceMessages(processedMessages, []processor.Replacement{
			{StartIdx: idx, EndIdx: idx, Messages: []llm_schema.BaseMessage{offloadMsg}},
		})
		event.MessagesToModify = append(event.MessagesToModify, idx)
	}

	if len(event.MessagesToModify) == 0 {
		return nil, processedMessages, nil
	}
	return event, processedMessages, nil
}

// offloadMessage 卸载单条消息：裁剪内容 + 调用 BaseProcessor.OffloadMessages。
//
// 对应 Python: MessageOffloader._offload_message()
func (mo *MessageOffloader) offloadMessage(ctx context.Context, message llm_schema.BaseMessage, mc iface.ModelContext, opts ...iface.Option) (llm_schema.BaseMessage, error) {
	content := message.GetContent().Text()
	cfg := mo.config

	// 裁剪内容
	trimmedContent := content
	if len(content) > cfg.TrimSize {
		trimmedContent = content[:cfg.TrimSize] + omitString
	}

	// 生成 offload handle 和 path
	offloadHandle, offloadPath := mo.newOffloadHandleAndPath(mc)

	// 调用基类 OffloadMessages
	offloadMsg, err := mo.OffloadMessages(
		ctx, mc,
		message.GetRole().String(),
		trimmedContent,
		[]llm_schema.BaseMessage{message},
		iface.WithOffloadHandle(offloadHandle),
		iface.WithOffloadPath(offloadPath),
	)
	if err != nil {
		return nil, err
	}
	return offloadMsg, nil
}

// newOffloadHandleAndPath 生成卸载句柄和文件路径。
//
// 对应 Python: MessageOffloader._new_offload_handle_and_path()
//
// ⤵️ 5.31 回填：mc.WorkspaceDir() 方法
func (mo *MessageOffloader) newOffloadHandleAndPath(mc iface.ModelContext) (string, string) {
	offloadHandle := uuid.New().String()
	sessionID := mc.SessionID()

	// ⤵️ 5.31 回填：使用 mc.WorkspaceDir() 获取工作目录
	// 当前 ModelContext 接口没有 WorkspaceDir() 方法，使用空字符串
	workspaceDir := ""

	fileName := fmt.Sprintf("%s_%s.json", mo.ProcessorType(), offloadHandle)
	if workspaceDir != "" {
		return offloadHandle, filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
	}
	return offloadHandle, ""
}

// getOffloadRange 计算卸载范围（不在此范围内的消息不会被卸载）。
//
// 对应 Python: MessageOffloader._get_offload_range()
func (mo *MessageOffloader) getOffloadRange(messages []llm_schema.BaseMessage) int {
	keepIndex := len(messages)
	if mo.config.MessagesToKeep != nil {
		keepIndex = len(messages) - *mo.config.MessagesToKeep
	}

	if mo.config.KeepLastRound {
		lastAIMsgIdx := processor.FindLastFinalAssistantIdx(messages)
		if lastAIMsgIdx != -1 && lastAIMsgIdx < keepIndex {
			return lastAIMsgIdx
		}
	}
	return keepIndex
}

// hasOffloadCandidate 检查卸载范围内是否存在可卸载的候选消息。
//
// 对应 Python: MessageOffloader._has_offload_candidate()
func (mo *MessageOffloader) hasOffloadCandidate(messages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	offloadRange := mo.getOffloadRange(messages)
	for idx := offloadRange - 1; idx >= 0; idx-- {
		if mo.shouldOffloadMessage(messages[idx], messages, mc) {
			return true
		}
	}
	return false
}

// shouldOffloadMessage 判断消息是否符合卸载条件。
//
// 5 条规则（全部通过才卸载）：
//  1. 角色在 OffloadMessageTypes 中
//  2. content 是字符串
//  3. content 长度 > LargeMessageThreshold
//  4. 不是已卸载消息（OffloadMixin）
//  5. 不是受保护工具的结果
//
// 对应 Python: MessageOffloader._should_offload_message()
func (mo *MessageOffloader) shouldOffloadMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	cfg := mo.config

	// 规则 1：角色检查
	roleMatch := false
	role := message.GetRole().String()
	for _, rt := range cfg.OffloadMessageTypes {
		if rt == role {
			roleMatch = true
			break
		}
	}
	if !roleMatch {
		return false
	}

	// 规则 2：content 必须是字符串
	content := message.GetContent().Text()
	if content == "" && message.GetContent().Text() == "" {
		// content 不是纯文本（可能是多模态），不卸载
		return false
	}

	// 规则 3：长度检查
	if len(content) <= cfg.LargeMessageThreshold {
		return false
	}

	// 规则 4：已卸载消息不重复卸载
	if schema.IsOffloaded(message) {
		return false
	}

	// 规则 5：受保护工具消息不卸载
	if mo.isProtectedToolMessage(message, contextMessages) {
		return false
	}

	return true
}

// isProtectedToolMessage 检查消息是否为受保护工具的结果。
//
// 支持 "tool_name" 和 "tool_name:pattern" 两种格式。
// 后者使用 filepath.Match 对工具参数值做通配符匹配。
//
// 对应 Python: MessageOffloader._is_protected_tool_message()
func (mo *MessageOffloader) isProtectedToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	// 只检查 ToolMessage
	if message.GetRole() != llm_schema.RoleTypeTool {
		return false
	}

	// 回溯查找对应的 ToolCall
	toolCall := processor.ResolveToolCallFromMessage(message, contextMessages)
	if toolCall == nil {
		return false
	}

	toolName := processor.ExtractToolName(toolCall)
	toolArgs := extractToolArgs(toolCall)

	for _, protected := range mo.config.ProtectedToolNames {
		if colonIdx := strings.Index(protected, ":"); colonIdx != -1 {
			protectedTool := protected[:colonIdx]
			protectedPattern := protected[colonIdx+1:]
			if toolName == protectedTool && matchPattern(toolArgs, protectedPattern) {
				return true
			}
		} else {
			if toolName == protected {
				return true
			}
		}
	}
	return false
}

// extractToolArgs 从 ToolCall 提取参数字典。
//
// 支持多种格式：JSON string、map 结构。
//
// 对应 Python: MessageOffloader._extract_tool_args()
func extractToolArgs(toolCall *llm_schema.ToolCall) map[string]any {
	if toolCall == nil {
		return map[string]any{}
	}
	// ToolCall.Arguments 是 JSON string
	if toolCall.Arguments != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(toolCall.Arguments), &parsed); err == nil {
			return parsed
		}
	}
	return map[string]any{}
}

// matchPattern 检查参数值是否匹配通配符模式。
//
// 使用 filepath.Match 替代 Python fnmatch，支持 * ? [...] 通配符。
//
// 对应 Python: MessageOffloader._match_pattern()
func matchPattern(args map[string]any, pattern string) bool {
	for _, value := range args {
		if strVal, ok := value.(string); ok {
			if matched, _ := filepath.Match(pattern, strVal); matched {
				return true
			}
		}
	}
	return false
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("MessageOffloader",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*MessageOffloaderConfig)
			if !ok {
				return nil, fmt.Errorf("MessageOffloader: 配置类型不匹配，期望 *MessageOffloaderConfig，实际 %T", config)
			}
			return NewMessageOffloader(cfg)
		},
	)
}
```

注意：文件头部需补全 import 列表，包括 `"context"`, `"encoding/json"`, `"fmt"`, `"path/filepath"`, `"strings"`。

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/offloader/...`

Expected: 编译成功。

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "feat(offloader): 添加 MessageOffloader 和 MessageOffloaderConfig"
```

---

## Task 3: MessageOffloaderConfig.Validate 单元测试

**Files:**
- Create: `internal/agentcore/context_engine/processor/offloader/message_offloader_test.go`

- [ ] **Step 1: 编写 Config.Validate 测试**

```go
package offloader

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMessageOffloaderConfig_Validate_正常配置(t *testing.T) {
	threshold := 100
	keep := 5
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		MessagesToKeep:        &keep,
		KeepLastRound:         true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("期望验证通过，实际错误: %v", err)
	}
}

func TestMessageOffloaderConfig_Validate_TrimSize过大(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 100,
		TrimSize:              200, // > LargeMessageThreshold
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("期望 TrimSize >= LargeMessageThreshold 时报错，实际通过")
	}
}

func TestMessageOffloaderConfig_Validate_MessagesToKeep过大(t *testing.T) {
	threshold := 10
	keep := 15
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		MessagesToKeep:        &keep,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("期望 MessagesToKeep >= MessagesThreshold 时报错，实际通过")
	}
}

func TestMessageOffloaderConfig_Validate_默认值(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("期望验证通过，实际错误: %v", err)
	}
	if len(cfg.OffloadMessageTypes) == 0 || cfg.OffloadMessageTypes[0] != "tool" {
		t.Errorf("期望 OffloadMessageTypes 默认为 [tool]，实际: %v", cfg.OffloadMessageTypes)
	}
	if len(cfg.ProtectedToolNames) == 0 || cfg.ProtectedToolNames[0] != "reload_original_context_messages" {
		t.Errorf("期望 ProtectedToolNames 默认值，实际: %v", cfg.ProtectedToolNames)
	}
}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/offloader/... -run TestMessageOffloaderConfig -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 3: 提交**

```bash
git add -A
git commit -m "test(offloader): 添加 MessageOffloaderConfig.Validate 单元测试"
```

---

## Task 4: shouldOffloadMessage + isProtectedToolMessage 单元测试

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_offloader_test.go`

- [ ] **Step 1: 添加 shouldOffloadMessage 测试**

```go
func TestMessageOffloader_shouldOffloadMessage_角色不匹配(t *testing.T) {
	mo := newTestMessageOffloader()
	msg := llm_schema.NewUserMessage(strings.Repeat("a", 2000))
	// OffloadMessageTypes 默认 ["tool"]，UserMessage 不匹配
	if mo.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("UserMessage 不在 OffloadMessageTypes 中，期望返回 false")
	}
}

func TestMessageOffloader_shouldOffloadMessage_内容太短(t *testing.T) {
	mo := newTestMessageOffloader()
	// 创建短内容的 ToolMessage
	msg := llm_schema.NewToolMessage("call_1", "short content")
	if mo.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("内容长度 <= LargeMessageThreshold，期望返回 false")
	}
}

func TestMessageOffloader_shouldOffloadMessage_已卸载消息(t *testing.T) {
	mo := newTestMessageOffloader()
	msg := schema.NewOffloadToolMessage("call_1", strings.Repeat("a", 2000), "handle123", "filesystem")
	if mo.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("已卸载消息不应重复卸载")
	}
}

func TestMessageOffloader_shouldOffloadMessage_受保护工具消息(t *testing.T) {
	mo := newTestMessageOffloader()
	// 构造 ToolMessage + 对应的 AssistantMessage(含 ToolCall)
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "reload_original_context_messages", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("call_1", strings.Repeat("a", 2000))
	messages := []llm_schema.BaseMessage{am, tm}

	if mo.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("受保护工具消息不应被卸载")
	}
}

func TestMessageOffloader_shouldOffloadMessage_符合卸载条件(t *testing.T) {
	mo := newTestMessageOffloader()
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "grep", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("call_1", strings.Repeat("a", 2000))
	messages := []llm_schema.BaseMessage{am, tm}

	if !mo.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("普通工具的大消息应该被卸载")
	}
}

// newTestMessageOffloader 创建测试用 MessageOffloader 实例
func newTestMessageOffloader() *MessageOffloader {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate() // 应用默认值
	bp := processor.NewBaseProcessor(cfg)
	return &MessageOffloader{
		BaseProcessor: bp,
		config:        cfg,
	}
}
```

- [ ] **Step 2: 添加 isProtectedToolMessage 测试**

```go
func TestMessageOffloader_isProtectedToolMessage_非ToolMessage(t *testing.T) {
	mo := newTestMessageOffloader()
	msg := llm_schema.NewUserMessage("hello")
	if mo.isProtectedToolMessage(msg, nil) {
		t.Fatal("UserMessage 不是 ToolMessage，期望返回 false")
	}
}

func TestMessageOffloader_isProtectedToolMessage_精确匹配(t *testing.T) {
	mo := newTestMessageOffloader()
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "reload_original_context_messages"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if !mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("工具名精确匹配受保护列表，期望返回 true")
	}
}

func TestMessageOffloader_isProtectedToolMessage_通配符匹配(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		ProtectedToolNames:    []string{"read_file:*.md"},
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	tc := &llm_schema.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"file_path": "/tmp/test.md"}`}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if !mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("read_file + *.md 通配符匹配 file_path=/tmp/test.md，期望返回 true")
	}
}

func TestMessageOffloader_isProtectedToolMessage_不匹配(t *testing.T) {
	mo := newTestMessageOffloader()
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("grep 不在受保护列表中，期望返回 false")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/offloader/... -run "TestMessageOffloader_should|TestMessageOffloader_isProtected" -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "test(offloader): 添加 shouldOffloadMessage 和 isProtectedToolMessage 单元测试"
```

---

## Task 5: getOffloadRange + hasOffloadCandidate + matchPattern + extractToolArgs 单元测试

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_offloader_test.go`

- [ ] **Step 1: 添加 getOffloadRange 测试**

```go
func TestMessageOffloader_getOffloadRange_KeepLastRound(t *testing.T) {
	mo := newTestMessageOffloader()
	// 构造消息：user, assistant(无tool_calls), user, tool, assistant(有tool_calls)
	um1 := llm_schema.NewUserMessage("q1")
	am1 := llm_schema.NewAssistantMessage("a1") // 无 tool_calls
	um2 := llm_schema.NewUserMessage("q2")
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am2 := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("c1", "result")
	messages := []llm_schema.BaseMessage{um1, am1, um2, am2, tm}

	// KeepLastRound=true，最后不含 tool_calls 的 AI 消息在 index=1
	offloadRange := mo.getOffloadRange(messages)
	if offloadRange != 1 {
		t.Fatalf("期望 offloadRange=1（最后无 tool_calls 的 AI 消息），实际=%d", offloadRange)
	}
}

func TestMessageOffloader_getOffloadRange_不保留最后一轮(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q1"),
		llm_schema.NewAssistantMessage("a1"),
	}
	offloadRange := mo.getOffloadRange(messages)
	if offloadRange != 2 {
		t.Fatalf("KeepLastRound=false 时，offloadRange 应等于 len(messages)=2，实际=%d", offloadRange)
	}
}

func TestMessageOffloader_getOffloadRange_MessagesToKeep(t *testing.T) {
	keep := 2
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		MessagesToKeep:        &keep,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q1"),
		llm_schema.NewAssistantMessage("a1"),
		llm_schema.NewUserMessage("q2"),
		llm_schema.NewAssistantMessage("a2"),
		llm_schema.NewUserMessage("q3"),
	}
	offloadRange := mo.getOffloadRange(messages)
	// len=5, keep=2 → offloadRange = 5-2 = 3
	if offloadRange != 3 {
		t.Fatalf("期望 offloadRange=3（5-2），实际=%d", offloadRange)
	}
}
```

- [ ] **Step 2: 添加 matchPattern 和 extractToolArgs 测试**

```go
func TestMatchPattern_匹配(t *testing.T) {
	args := map[string]any{"file_path": "/tmp/test.md"}
	if !matchPattern(args, "*.md") {
		t.Fatal("期望 *.md 匹配 /tmp/test.md")
	}
}

func TestMatchPattern_不匹配(t *testing.T) {
	args := map[string]any{"file_path": "/tmp/test.go"}
	if matchPattern(args, "*.md") {
		t.Fatal("期望 *.md 不匹配 /tmp/test.go")
	}
}

func TestMatchPattern_空参数(t *testing.T) {
	args := map[string]any{}
	if matchPattern(args, "*.md") {
		t.Fatal("期望空参数不匹配")
	}
}

func TestExtractToolArgs_JSON格式(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"file_path": "/tmp/test.md"}`}
	args := extractToolArgs(tc)
	if args["file_path"] != "/tmp/test.md" {
		t.Fatalf("期望 file_path=/tmp/test.md，实际: %v", args)
	}
}

func TestExtractToolArgs_空参数(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep", Arguments: ""}
	args := extractToolArgs(tc)
	if len(args) != 0 {
		t.Fatalf("期望空 map，实际: %v", args)
	}
}

func TestExtractToolArgs_无效JSON(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep", Arguments: "invalid"}
	args := extractToolArgs(tc)
	if len(args) != 0 {
		t.Fatalf("期望空 map（无效 JSON），实际: %v", args)
	}
}

func TestExtractToolArgs_nil(t *testing.T) {
	args := extractToolArgs(nil)
	if len(args) != 0 {
		t.Fatalf("期望空 map（nil ToolCall），实际: %v", args)
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/offloader/... -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "test(offloader): 添加 getOffloadRange/matchPattern/extractToolArgs 单元测试"
```

---

## Task 6: TriggerAddMessages + OnAddMessages 集成测试

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_offloader_test.go`

- [ ] **Step 1: 添加 fakeModelContext 实现**

```go
// fakeModelContext 用于测试的 ModelContext 模拟实现
type fakeModelContext struct {
	messages    []llm_schema.BaseMessage
	sessionID   string
	tokenCount  int
	tokenErr    error
}

func (f *fakeModelContext) Len() int                                          { return len(f.messages) }
func (f *fakeModelContext) GetMessages(_ *int, _ bool) []llm_schema.BaseMessage { return f.messages }
func (f *fakeModelContext) SetMessages(msgs []llm_schema.BaseMessage, _ bool)  { f.messages = msgs }
func (f *fakeModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage { return nil }
func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool) error      { return nil }
func (f *fakeModelContext) AddMessages(_ context.Context, _ any) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (f *fakeModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage,
	_ []*schema.ToolInfo, _ *int, _ *int) (*iface.ContextWindow, error) {
	return nil, nil
}
func (f *fakeModelContext) Statistic() *iface.ContextStats  { return nil }
func (f *fakeModelContext) SessionID() string                { return f.sessionID }
func (f *fakeModelContext) ContextID() string                { return "" }
func (f *fakeModelContext) TokenCounter() token.TokenCounter { return f }
func (f *fakeModelContext) ReloaderTool() tool.Tool          { return nil }

// 实现 token.TokenCounter 接口
func (f *fakeModelContext) CountMessages(messages []llm_schema.BaseMessage, _ string) (int, error) {
	return f.tokenCount, f.tokenErr
}
func (f *fakeModelContext) CountTools(_ []*schema.ToolInfo, _ string) (int, error) {
	return 0, nil
}
```

- [ ] **Step 2: 添加 TriggerAddMessages 测试**

```go
func TestMessageOffloader_TriggerAddMessages_消息数超阈值(t *testing.T) {
	threshold := 5
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       999999,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	// 构造 6 条消息（超过阈值 5），其中包含大 ToolMessage
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	tm := llm_schema.NewToolMessage("c1", strings.Repeat("a", 2000))

	existingMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q1"),
		am,
		tm,
		llm_schema.NewAssistantMessage("a1"),
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q2"),
		llm_schema.NewAssistantMessage("a2"),
	}

	mc := &fakeModelContext{messages: existingMessages, sessionID: "test-session"}
	triggered, err := mo.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if !triggered {
		t.Fatal("消息数超过阈值且有候选，期望触发")
	}
}

func TestMessageOffloader_TriggerAddMessages_未达阈值(t *testing.T) {
	threshold := 100
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       999999,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	mc := &fakeModelContext{
		messages:   []llm_schema.BaseMessage{llm_schema.NewUserMessage("q")},
		sessionID:  "test-session",
		tokenCount: 0,
	}
	triggered, err := mo.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("a")})
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if triggered {
		t.Fatal("消息数未达阈值，期望不触发")
	}
}
```

- [ ] **Step 3: 添加 OnAddMessages 测试**

```go
func TestMessageOffloader_OnAddMessages_卸载大消息(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       999999,
		LargeMessageThreshold: 100,
		TrimSize:              10,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	// 构造：大 ToolMessage 应被卸载
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls(tc))
	largeContent := strings.Repeat("a", 500)
	tm := llm_schema.NewToolMessage("c1", largeContent)

	existingMessages := []llm_schema.BaseMessage{am, tm}
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("done")}

	mc := &fakeModelContext{messages: existingMessages, sessionID: "test-session"}
	event, result, err := mo.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event == nil {
		t.Fatal("期望返回 ContextEvent，实际为 nil")
	}
	if event.EventType != "MessageOffloader" {
		t.Fatalf("期望 EventType=MessageOffloader，实际=%s", event.EventType)
	}
	if len(event.MessagesToModify) == 0 {
		t.Fatal("期望有消息被修改")
	}

	// 验证消息列表中原来的 ToolMessage 被替换为 OffloadMessage
	updatedContext := mc.GetMessages(nil, true)
	for _, msg := range updatedContext {
		if msg.GetRole() == llm_schema.RoleTypeTool {
			if schema.IsOffloaded(msg) {
				// 被卸载的 ToolMessage 的 content 应该被截断
				content := msg.GetContent().Text()
				if !strings.Contains(content, omitString) {
					t.Fatalf("卸载后的消息内容应包含省略标记，实际: %s", content)
				}
			}
		}
	}
	_ = result // 返回的 messagesToAdd 部分
}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/offloader/... -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "test(offloader): 添加 TriggerAddMessages 和 OnAddMessages 集成测试"
```

---

## Task 7: 更新 processor/doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/context_engine/processor/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 processor/doc.go 文件目录**

在文件目录中添加 `util.go` 和 `offloader/` 子包条目：

```
// 文件目录：
//
//	processor/
//	├── doc.go          # 包文档
//	├── base.go         # BaseProcessor 结构体 + 构造函数
//	├── hooks.go        # BaseProcessor 钩子默认实现 + ProcessorType + IsAPIRound
//	├── state.go        # BaseProcessor SaveState/LoadState 默认实现
//	├── offload.go      # OffloadMessages 方法族 + offload 常量 + GenerateOffloadPath
//	├── usage.go        # CompressionUsage 追踪方法族
//	├── round.go        # GroupCompletedAPIRounds 包级导出函数
//	├── replace.go      # Replacement 结构体 + ReplaceMessages 通用替换函数
//	├── util.go         # 包级共享工具函数（从 compressor 迁移）
//	├── compressor/     # 压缩处理器子包
//	│   ├── doc.go                          # 子包文档
//	│   ├── dialogue_compressor.go          # DialogueCompressor 对话压缩器
//	│   ├── current_round_compressor.go     # CurrentRoundCompressor 当轮增量压缩器
//	│   ├── micro_compact_processor.go      # MicroCompactProcessor 微压缩处理器
//	│   └── full_compact_processor.go       # FullCompactProcessor 全量压缩处理器
//	└── offloader/      # 卸载处理器子包
//	    ├── doc.go                     # 子包文档
//	    ├── message_offloader.go       # MessageOffloader 消息卸载器
//	    └── message_offloader_test.go  # 单元测试
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 5.27 步骤的状态从 `☐` 改为 `✅`。

- [ ] **Step 3: 提交**

```bash
git add -A
git commit -m "docs: 更新 processor doc.go 和 IMPLEMENTATION_PLAN.md（5.27 完成）"
```

---

## Task 8: 全量编译和测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

Expected: 编译成功。

- [ ] **Step 2: 运行 context_engine 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 3: 检查测试覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_engine/processor/offloader/...`

Expected: 覆盖率 >= 85%。
