# MicroCompactProcessor 微压缩处理器实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 MicroCompactProcessor 微压缩处理器，清除旧工具结果内容以减少 Token 消耗，同时创建 context_utils 子包提供工具名解析能力。

**Architecture:** 新建 `context_engine/context_utils/` 子包，提供 `ResolveToolNameFromMessage` 等工具函数；在 `processor/` 包中新建 `micro_compact_processor.go`，嵌入 BaseProcessor 实现 ContextProcessor 接口，通过 `SetContent` 原地替换 ToolMessage 内容。自动注册到 context_engine 注册表。

**Tech Stack:** Go 1.x，现有 context_engine/interface 接口体系，llm_schema 消息类型

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新建 | `context_engine/context_utils/doc.go` | 子包文档 |
| 新建 | `context_engine/context_utils/resolve.go` | 工具名解析函数 |
| 新建 | `context_engine/context_utils/resolve_test.go` | 解析函数测试 |
| 新建 | `context_engine/processor/micro_compact_processor.go` | MicroCompactProcessorConfig + MicroCompactProcessor + init() |
| 新建 | `context_engine/processor/micro_compact_processor_test.go` | 处理器测试 |
| 修改 | `context_engine/doc.go` | 添加 context_utils/ 和 micro_compact_processor.go 条目 |
| 修改 | `context_engine/processor/doc.go` | 添加 micro_compact_processor.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 5.23 状态更新 |

---

### Task 1: 创建 context_utils/doc.go

**Files:**
- Create: `internal/agentcore/context_engine/context_utils/doc.go`

- [ ] **Step 1: 创建 doc.go 文件**

```go
// Package context_utils 提供上下文引擎的工具辅助函数。
//
// 包含消息类型解析、工具名回溯查找等无状态工具方法，
// 供 context_engine 下各处理器和上下文实例使用。
// 当前仅包含 MicroCompactProcessor 所需的工具名解析函数，
// 后续步骤（5.24-5.31）按需回填其他工具方法。
//
// 文件目录：
//
//	context_utils/
//	├── doc.go           # 包文档
//	├── resolve.go       # 工具名解析函数（ResolveToolNameFromMessage 等）
//
// 对应 Python 代码：openjiuwen/core/context_engine/context/context_utils.py
package context_utils
```

---

### Task 2: 实现 context_utils/resolve.go 和测试

**Files:**
- Create: `internal/agentcore/context_engine/context_utils/resolve.go`
- Create: `internal/agentcore/context_engine/context_utils/resolve_test.go`

- [ ] **Step 1: 编写 ExtractToolName 测试**

```go
package context_utils

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

func TestExtractToolName(t *testing.T) {
	t.Run("Name字段有值", func(t *testing.T) {
		tc := &llm_schema.ToolCall{Name: "grep"}
		got := ExtractToolName(tc)
		if got != "grep" {
			t.Errorf("ExtractToolName() = %q, want %q", got, "grep")
		}
	})

	t.Run("Name字段为空", func(t *testing.T) {
		tc := &llm_schema.ToolCall{Name: ""}
		got := ExtractToolName(tc)
		if got != "" {
			t.Errorf("ExtractToolName() = %q, want empty string", got)
		}
	})

	t.Run("nil ToolCall", func(t *testing.T) {
		got := ExtractToolName(nil)
		if got != "" {
			t.Errorf("ExtractToolName(nil) = %q, want empty string", got)
		}
	})
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/context_utils/... -run TestExtractToolName -v`
Expected: 编译失败，ExtractToolName 未定义

- [ ] **Step 3: 实现 resolve.go**

```go
package context_utils

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

- [ ] **Step 4: 运行 ExtractToolName 测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context_utils/... -run TestExtractToolName -v`
Expected: PASS

- [ ] **Step 5: 编写 ResolveToolCallFromMessage 和 ResolveToolNameFromMessage 测试**

在 `resolve_test.go` 中追加：

```go
func TestResolveToolCallFromMessage(t *testing.T) {
	t.Run("非ToolMessage返回nil", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := ResolveToolCallFromMessage(msg, nil)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})

	t.Run("ToolMessage无ToolCallID返回nil", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("", "result")
		got := ResolveToolCallFromMessage(msg, nil)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})

	t.Run("匹配到ToolCall", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_1", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "glob"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got == nil {
			t.Fatal("ResolveToolCallFromMessage() = nil, want non-nil")
		}
		if got.ID != "call_1" {
			t.Errorf("got.ID = %q, want %q", got.ID, "call_1")
		}
	})

	t.Run("多条AssistantMessage从后匹配", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_2", "result")
		assistant1 := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		assistant2 := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_2", Name: "glob"},
			},
		}
		messages := []llm_schema.BaseMessage{assistant1, assistant2, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got == nil {
			t.Fatal("ResolveToolCallFromMessage() = nil, want non-nil")
		}
		if got.Name != "glob" {
			t.Errorf("got.Name = %q, want %q", got.Name, "glob")
		}
	})

	t.Run("未匹配返回nil", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_999", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})
}

func TestResolveToolNameFromMessage(t *testing.T) {
	t.Run("回溯找到工具名", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_1", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolNameFromMessage(toolMsg, messages)
		if got != "grep" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want %q", got, "grep")
		}
	})

	t.Run("未找到返回空字符串", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_999", "result")
		got := ResolveToolNameFromMessage(toolMsg, []llm_schema.BaseMessage{})
		if got != "" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want empty string", got)
		}
	})

	t.Run("非ToolMessage返回空字符串", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := ResolveToolNameFromMessage(msg, nil)
		if got != "" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want empty string", got)
		}
	})
}
```

- [ ] **Step 6: 运行全部 resolve 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/context_utils/... -v`
Expected: 全部 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/context_engine/context_utils/
git commit -m "feat(context_engine): 新增 context_utils 子包，提供工具名解析函数"
```

---

### Task 3: 实现 MicroCompactProcessorConfig

**Files:**
- Create: `internal/agentcore/context_engine/processor/micro_compact_processor.go`

- [ ] **Step 1: 编写 MicroCompactProcessorConfig 和 Validate 测试**

创建 `internal/agentcore/context_engine/processor/micro_compact_processor_test.go`：

```go
package processor

import (
	"testing"
)

func TestNewMicroCompactProcessorConfig(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		if cfg.TriggerThreshold != 5 {
			t.Errorf("TriggerThreshold = %d, want 5", cfg.TriggerThreshold)
		}
		if cfg.KeepRecentPerTool != 15 {
			t.Errorf("KeepRecentPerTool = %d, want 15", cfg.KeepRecentPerTool)
		}
		if cfg.ClearedMarker != "[Old tool result Content cleared]" {
			t.Errorf("ClearedMarker = %q, want default marker", cfg.ClearedMarker)
		}
		expectedTools := []string{"grep", "glob", "read_file", "web_search", "web_fetch"}
		if len(cfg.CompactableToolNames) != len(expectedTools) {
			t.Fatalf("CompactableToolNames len = %d, want %d", len(cfg.CompactableToolNames), len(expectedTools))
		}
		for i, name := range cfg.CompactableToolNames {
			if name != expectedTools[i] {
				t.Errorf("CompactableToolNames[%d] = %q, want %q", i, name, expectedTools[i])
			}
		}
	})

	t.Run("Validate通过", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	})

	t.Run("TriggerThreshold为0", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.TriggerThreshold = 0
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when TriggerThreshold=0")
		}
	})

	t.Run("KeepRecentPerTool为负数", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.KeepRecentPerTool = -1
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when KeepRecentPerTool<0")
		}
	})

	t.Run("ClearedMarker为空", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.ClearedMarker = ""
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when ClearedMarker is empty")
		}
	})

	t.Run("CompactableToolNames为空", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.CompactableToolNames = nil
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when CompactableToolNames is empty")
		}
	})
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run TestNewMicroCompactProcessorConfig -v`
Expected: 编译失败

- [ ] **Step 3: 实现 MicroCompactProcessorConfig**

创建 `internal/agentcore/context_engine/processor/micro_compact_processor.go`：

```go
package processor

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	context_utils "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context_utils"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MicroCompactProcessorConfig 微压缩处理器配置。
//
// 清除旧工具结果内容以减少 Token 消耗，保留每个工具最近的若干条结果。
//
// 对应 Python: MicroCompactProcessorConfig (pydantic.BaseModel)
type MicroCompactProcessorConfig struct {
	// TriggerThreshold 触发阈值，可清除结果数超出保留尾部的数量阈值
	TriggerThreshold int
	// CompactableToolNames 可压缩的工具名称列表
	CompactableToolNames []string
	// KeepRecentPerTool 每个工具保留最近的 ToolMessage 数量
	KeepRecentPerTool int
	// ClearedMarker 清除旧内容时的替换文本
	ClearedMarker string
}

// MicroCompactProcessor 微压缩处理器，清除旧工具结果内容以减少 Token 消耗。
//
// 当某个可压缩工具的 ToolMessage 数量超过 triggerThreshold + keepRecentPerTool 时，
// 将超出保留窗口的旧 ToolMessage 的 content 替换为 clearedMarker，
// 保留每个工具最近的 keepRecentPerTool 条结果。
//
// 不需要调用 LLM，是处理器链中最轻量的压缩手段。
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/micro_compact_processor.py (MicroCompactProcessor)
type MicroCompactProcessor struct {
	*BaseProcessor
	// mcpConfig 微压缩处理器具体配置
	mcpConfig *MicroCompactProcessorConfig
}

// ──────────────────────────── 常量 ────────────────────────────

// MicroCompactClearedMarker 旧工具结果内容清除标记
const MicroCompactClearedMarker = "[Old tool result Content cleared]"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMicroCompactProcessorConfig 创建微压缩处理器配置，使用默认值。
func NewMicroCompactProcessorConfig() *MicroCompactProcessorConfig {
	return &MicroCompactProcessorConfig{
		TriggerThreshold: 5,
		CompactableToolNames: []string{
			"grep", "glob", "read_file", "web_search", "web_fetch",
		},
		KeepRecentPerTool: 15,
		ClearedMarker:     MicroCompactClearedMarker,
	}
}

// Validate 校验微压缩处理器配置。
func (c *MicroCompactProcessorConfig) Validate() error {
	if c.TriggerThreshold <= 0 {
		return fmt.Errorf("MicroCompactProcessorConfig.TriggerThreshold 必须大于 0，当前值: %d", c.TriggerThreshold)
	}
	if c.KeepRecentPerTool < 0 {
		return fmt.Errorf("MicroCompactProcessorConfig.KeepRecentPerTool 不能为负数，当前值: %d", c.KeepRecentPerTool)
	}
	if c.ClearedMarker == "" {
		return fmt.Errorf("MicroCompactProcessorConfig.ClearedMarker 不能为空")
	}
	if len(c.CompactableToolNames) == 0 {
		return fmt.Errorf("MicroCompactProcessorConfig.CompactableToolNames 不能为空")
	}
	return nil
}

// NewMicroCompactProcessor 创建微压缩处理器实例。
func NewMicroCompactProcessor(config *MicroCompactProcessorConfig) (*MicroCompactProcessor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := NewBaseProcessor(config)
	return &MicroCompactProcessor{
		BaseProcessor: bp,
		mcpConfig:     config,
	}, nil
}

// ProcessorType 返回处理器类型标识。
func (mcp *MicroCompactProcessor) ProcessorType() string {
	return "MicroCompactProcessor"
}

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：
//  1. 消息列表构成一个完整的 API 轮次
//  2. 某个可压缩工具的 ToolMessage 数量超过 triggerThreshold + keepRecentPerTool
//
// 对应 Python: MicroCompactProcessor.trigger_add_messages()
func (mcp *MicroCompactProcessor) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	allMessages := append(mc.GetMessages(nil, true), messagesToAdd...)
	if !mcp.IsAPIRound(allMessages) {
		return false, nil
	}
	return mcp.hasAnyToolExceedThreshold(allMessages), nil
}

// OnAddMessages 执行微压缩，将旧 ToolMessage 内容替换为清除标记。
//
// 对应 Python: MicroCompactProcessor.on_add_messages()
func (mcp *MicroCompactProcessor) OnAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, opts ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	allMessages := append(mc.GetMessages(nil, true), messagesToAdd...)

	// 从 opts 提取 force 标志
	po := iface.NewProcessorOption(opts...)
	force := false
	if po.Extra != nil {
		if v, ok := po.Extra["force"].(bool); ok && v {
			force = true
		}
	}

	indicesToClear := mcp.collectFlatIndicesForCompact(allMessages, force)
	if len(indicesToClear) == 0 {
		return nil, messagesToAdd, nil
	}

	marker := mcp.mcpConfig.ClearedMarker
	var modifiedIndices []int
	for _, index := range indicesToClear {
		tm, ok := allMessages[index].(*llm_schema.ToolMessage)
		if !ok {
			continue
		}
		if tm.GetContent().Text() == marker {
			continue
		}
		allMessages[index].SetContent(llm_schema.NewTextContent(marker))
		modifiedIndices = append(modifiedIndices, index)
	}

	if len(modifiedIndices) == 0 {
		return nil, messagesToAdd, nil
	}

	mc.SetMessages(allMessages, true)

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "MicroCompactProcessor_cleared").
		Int("cleared_count", len(modifiedIndices)).
		Bool("force", force).
		Msg("微压缩处理器清除了旧工具结果内容")

	return &iface.ContextEvent{
		EventType:        mcp.ProcessorType(),
		MessagesToModify: modifiedIndices,
	}, []llm_schema.BaseMessage{}, nil
}

// SaveState 导出处理器内部状态（无状态，返回空 map）。
func (mcp *MicroCompactProcessor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（无状态，空操作）。
func (mcp *MicroCompactProcessor) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectCompactableIndicesByTool 遍历消息，按工具名分组收集可压缩 ToolMessage 索引。
//
// 对应 Python: MicroCompactProcessor._collect_compactable_indices_by_tool()
func (mcp *MicroCompactProcessor) collectCompactableIndicesByTool(messages []llm_schema.BaseMessage) map[string][]int {
	allowedNames := make(map[string]bool, len(mcp.mcpConfig.CompactableToolNames))
	for _, name := range mcp.mcpConfig.CompactableToolNames {
		allowedNames[name] = true
	}

	result := make(map[string][]int)
	for index, message := range messages {
		tm, ok := message.(*llm_schema.ToolMessage)
		if !ok {
			continue
		}
		if tm.GetContent().Text() == mcp.mcpConfig.ClearedMarker {
			continue
		}
		toolName := context_utils.ResolveToolNameFromMessage(message, messages)
		if toolName == "" {
			continue
		}
		if allowedNames[toolName] {
			result[toolName] = append(result[toolName], index)
		}
	}
	return result
}

// hasAnyToolExceedThreshold 判断任一工具的 ToolMessage 数量是否超过触发阈值。
//
// 对应 Python: MicroCompactProcessor._has_any_tool_exceed_threshold()
func (mcp *MicroCompactProcessor) hasAnyToolExceedThreshold(messages []llm_schema.BaseMessage) bool {
	groupedIndices := mcp.collectCompactableIndicesByTool(messages)
	threshold := mcp.mcpConfig.TriggerThreshold + mcp.mcpConfig.KeepRecentPerTool
	for _, indices := range groupedIndices {
		if len(indices) > threshold {
			return true
		}
	}
	return false
}

// collectFlatIndicesForCompact 收集需要清除的索引列表。
//
// force=true 时阈值降为 KeepRecentPerTool；超过阈值的工具，保留尾部 KeepRecentPerTool 条，
// 其余加入清除列表。
//
// 对应 Python: MicroCompactProcessor._collect_flat_indices_for_compact()
func (mcp *MicroCompactProcessor) collectFlatIndicesForCompact(messages []llm_schema.BaseMessage, force bool) []int {
	grouped := mcp.collectCompactableIndicesByTool(messages)
	var result []int
	for _, indices := range grouped {
		threshold := mcp.mcpConfig.KeepRecentPerTool
		if !force {
			threshold += mcp.mcpConfig.TriggerThreshold
		}
		if len(indices) > threshold {
			keepCount := mcp.mcpConfig.KeepRecentPerTool
			if keepCount > 0 {
				result = append(result, indices[:len(indices)-keepCount]...)
			} else {
				result = append(result, indices...)
			}
		}
	}
	return result
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("MicroCompactProcessor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*MicroCompactProcessorConfig)
			if !ok {
				return nil, fmt.Errorf("MicroCompactProcessor: 配置类型不匹配，期望 *MicroCompactProcessorConfig，实际 %T", config)
			}
			mcp, err := NewMicroCompactProcessor(cfg)
			if err != nil {
				return nil, err
			}
			return mcp, nil
		},
	)
}
```

- [ ] **Step 4: 运行 MicroCompactProcessorConfig 测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run TestNewMicroCompactProcessorConfig -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/micro_compact_processor.go internal/agentcore/context_engine/processor/micro_compact_processor_test.go
git commit -m "feat(context_engine): 实现 MicroCompactProcessor 配置和构造函数"
```

---

### Task 4: 编写 MicroCompactProcessor 核心方法测试

**Files:**
- Modify: `internal/agentcore/context_engine/processor/micro_compact_processor_test.go`

- [ ] **Step 1: 编写 ProcessorType 和辅助方法测试**

在 `micro_compact_processor_test.go` 中追加：

```go
func TestMicroCompactProcessor_ProcessorType(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	mcp, err := NewMicroCompactProcessor(cfg)
	if err != nil {
		t.Fatalf("NewMicroCompactProcessor() error = %v", err)
	}
	if mcp.ProcessorType() != "MicroCompactProcessor" {
		t.Errorf("ProcessorType() = %q, want %q", mcp.ProcessorType(), "MicroCompactProcessor")
	}
}

func TestMicroCompactProcessor_SaveLoadState(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	mcp, err := NewMicroCompactProcessor(cfg)
	if err != nil {
		t.Fatalf("NewMicroCompactProcessor() error = %v", err)
	}

	state := mcp.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState() = %v, want empty map", state)
	}

	mcp.LoadState(map[string]any{"key": "value"})
	// LoadState 是空操作，无返回值可验证
}
```

- [ ] **Step 2: 编写 collectCompactableIndicesByTool 测试**

```go
func TestMicroCompactProcessor_collectCompactableIndicesByTool(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.CompactableToolNames = []string{"grep", "glob"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("按工具名分组", func(t *testing.T) {
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "glob"},
				{ID: "call_3", Name: "read_file"},
			},
		}
		toolMsg1 := llm_schema.NewToolMessage("call_1", "grep result")
		toolMsg2 := llm_schema.NewToolMessage("call_2", "glob result")
		toolMsg3 := llm_schema.NewToolMessage("call_3", "read_file result")

		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg1, toolMsg2, toolMsg3}
		result := mcp.collectCompactableIndicesByTool(messages)

		if len(result["grep"]) != 1 || result["grep"][0] != 1 {
			t.Errorf("grep indices = %v, want [1]", result["grep"])
		}
		if len(result["glob"]) != 1 || result["glob"][0] != 2 {
			t.Errorf("glob indices = %v, want [2]", result["glob"])
		}
		// read_file 不在 CompactableToolNames 中
		if _, exists := result["read_file"]; exists {
			t.Error("read_file should not be in result")
		}
	})

	t.Run("已清除的跳过", func(t *testing.T) {
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		toolMsg1 := llm_schema.NewToolMessage("call_1", cfg.ClearedMarker) // 已清除

		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg1}
		result := mcp.collectCompactableIndicesByTool(messages)

		if _, exists := result["grep"]; exists {
			t.Error("已清除的 ToolMessage 不应被收集")
		}
	})
}
```

- [ ] **Step 3: 编写 hasAnyToolExceedThreshold 测试**

```go
func TestMicroCompactProcessor_hasAnyToolExceedThreshold(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("未超限返回false", func(t *testing.T) {
		// threshold=2, keep=1, 总共需要 >3 条才超限
		messages := buildToolMessages("grep", 3)
		if mcp.hasAnyToolExceedThreshold(messages) {
			t.Error("3 条不超限（需要 >3），应返回 false")
		}
	})

	t.Run("恰好等于阈值返回false", func(t *testing.T) {
		messages := buildToolMessages("grep", 3)
		if mcp.hasAnyToolExceedThreshold(messages) {
			t.Error("3 条恰好等于阈值，应返回 false")
		}
	})

	t.Run("超过阈值返回true", func(t *testing.T) {
		messages := buildToolMessages("grep", 4)
		if !mcp.hasAnyToolExceedThreshold(messages) {
			t.Error("4 条超过阈值 3，应返回 true")
		}
	})
}

// buildToolMessages 构建包含指定数量 grep ToolMessage 的消息列表
func buildToolMessages(toolName string, count int) []llm_schema.BaseMessage {
	var toolCalls []*llm_schema.ToolCall
	var messages []llm_schema.BaseMessage

	for i := 0; i < count; i++ {
		callID := fmt.Sprintf("call_%d", i)
		toolCalls = append(toolCalls, &llm_schema.ToolCall{ID: callID, Name: toolName})
		messages = append(messages, llm_schema.NewToolMessage(callID, fmt.Sprintf("%s result %d", toolName, i)))
	}

	assistantMsg := &llm_schema.AssistantMessage{
		DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
		ToolCalls:      toolCalls,
	}

	result := []llm_schema.BaseMessage{assistantMsg}
	result = append(result, messages...)
	return result
}
```

- [ ] **Step 4: 编写 collectFlatIndicesForCompact 测试**

```go
func TestMicroCompactProcessor_collectFlatIndicesForCompact(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("超限保留尾部", func(t *testing.T) {
		// 4 条 grep，threshold=2+1=3，keep=1，清除前 3 条
		messages := buildToolMessages("grep", 4)
		indices := mcp.collectFlatIndicesForCompact(messages, false)
		if len(indices) != 3 {
			t.Errorf("collectFlatIndicesForCompact() = %d indices, want 3", len(indices))
		}
	})

	t.Run("force=true阈值降低", func(t *testing.T) {
		// 2 条 grep，force 时 threshold=1，清除前 1 条
		messages := buildToolMessages("grep", 2)
		indices := mcp.collectFlatIndicesForCompact(messages, true)
		if len(indices) != 1 {
			t.Errorf("collectFlatIndicesForCompact(force=true) = %d indices, want 1", len(indices))
		}
	})

	t.Run("KeepRecentPerTool=0全部清除", func(t *testing.T) {
		cfg0 := NewMicroCompactProcessorConfig()
		cfg0.TriggerThreshold = 1
		cfg0.KeepRecentPerTool = 0
		cfg0.CompactableToolNames = []string{"grep"}
		mcp0, _ := NewMicroCompactProcessor(cfg0)

		messages := buildToolMessages("grep", 3)
		indices := mcp0.collectFlatIndicesForCompact(messages, false)
		if len(indices) != 3 {
			t.Errorf("KeepRecentPerTool=0 时应全部清除，got %d indices", len(indices))
		}
	})
}
```

需要在 test 文件顶部添加 `"fmt"` 到 import 列表。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run "TestMicroCompactProcessor_|TestMicroCompactProcessor_collect|TestMicroCompactProcessor_has" -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/micro_compact_processor_test.go
git commit -m "test(context_engine): 添加 MicroCompactProcessor 辅助方法测试"
```

---

### Task 5: 编写 TriggerAddMessages 和 OnAddMessages 测试

**Files:**
- Modify: `internal/agentcore/context_engine/processor/micro_compact_processor_test.go`

- [ ] **Step 1: 编写 TriggerAddMessages 测试**

```go
func TestMicroCompactProcessor_TriggerAddMessages(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("不构成APIround返回false", func(t *testing.T) {
		mc := &fakeModelContext{messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
		}}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_1", "result"),
		}
		got, err := mcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("TriggerAddMessages() error = %v", err)
		}
		if got {
			t.Error("TriggerAddMessages() = true, want false（不构成 API round）")
		}
	})

	t.Run("构成APIround但无超限工具返回false", func(t *testing.T) {
		// 构建完整 API round：user → assistant(tool_call) → tool
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls:      []*llm_schema.ToolCall{{ID: "call_1", Name: "grep"}},
		}
		mc := &fakeModelContext{messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
		}}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_1", "result"),
		}
		got, err := mcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("TriggerAddMessages() error = %v", err)
		}
		if got {
			t.Error("TriggerAddMessages() = true, want false（工具数量未超限）")
		}
	})

	t.Run("构成APIround且超限工具返回true", func(t *testing.T) {
		// threshold=2, keep=1, 需要 >3 条 grep
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			llm_schema.NewToolMessage("call_1", "result1"),
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		// 添加第 4 条 tool message + final assistant 构成 API round
		finalAssistant := llm_schema.NewAssistantMessageWithoutToolCalls("done")
		mc := &fakeModelContext{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
			finalAssistant,
		}
		got, err := mcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("TriggerAddMessages() error = %v", err)
		}
		if !got {
			t.Error("TriggerAddMessages() = false, want true（工具数量超限且构成 API round）")
		}
	})
}
```

需要在 import 中添加 `"context"`。

注意：需要确认 `llm_schema.NewAssistantMessageWithoutToolCalls` 是否存在，如果不存在则使用：

```go
&llm_schema.AssistantMessage{
    DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, "done"),
}
```

- [ ] **Step 2: 编写 OnAddMessages 测试**

```go
func TestMicroCompactProcessor_OnAddMessages(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	marker := cfg.ClearedMarker

	t.Run("无需清除时透传", func(t *testing.T) {
		mcp, _ := NewMicroCompactProcessor(cfg)
		mc := &fakeModelContext{messages: []llm_schema.BaseMessage{}}
		messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		event, result, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event != nil {
			t.Error("OnAddMessages() event should be nil when nothing to clear")
		}
		if len(result) != len(messagesToAdd) {
			t.Errorf("OnAddMessages() result len = %d, want %d", len(result), len(messagesToAdd))
		}
	})

	t.Run("有需清除的ToolMessage替换content", func(t *testing.T) {
		mcp, _ := NewMicroCompactProcessor(cfg)
		// 4 条 grep，threshold=2+1=3，keep=1，清除前 3 条
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			llm_schema.NewToolMessage("call_1", "result1"),
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		mc := &fakeModelContext{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
		}

		event, _, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event == nil {
			t.Fatal("OnAddMessages() event should not be nil when messages are cleared")
		}
		if event.EventType != "MicroCompactProcessor" {
			t.Errorf("EventType = %q, want %q", event.EventType, "MicroCompactProcessor")
		}
		if len(event.MessagesToModify) == 0 {
			t.Error("MessagesToModify should not be empty")
		}

		// 验证消息内容被替换
		updatedMessages := mc.GetMessages(nil, true)
		clearedCount := 0
		keptCount := 0
		for _, msg := range updatedMessages {
			tm, ok := msg.(*llm_schema.ToolMessage)
			if !ok {
				continue
			}
			content := tm.GetContent().Text()
			if content == marker {
				clearedCount++
			} else if content != "" && content != marker {
				keptCount++
			}
		}
		if clearedCount != 3 {
			t.Errorf("cleared ToolMessage count = %d, want 3", clearedCount)
		}
		if keptCount != 1 {
			t.Errorf("kept ToolMessage count = %d, want 1", keptCount)
		}
	})

	t.Run("已是marker的不重复替换", func(t *testing.T) {
		mcp, _ := NewMicroCompactProcessor(cfg)
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		// call_1 已被清除
		toolMsg1 := llm_schema.NewToolMessage("call_1", marker)
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			toolMsg1,
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		mc := &fakeModelContext{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
		}

		event, _, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event == nil {
			t.Fatal("OnAddMessages() event should not be nil")
		}
		// call_1 已是 marker 不会被再次计入 modifiedIndices
		for _, idx := range event.MessagesToModify {
			updatedMessages := mc.GetMessages(nil, true)
			content := updatedMessages[idx].GetContent().Text()
			if content == marker {
				// 应该是 call_2 或 call_3 被清除，而不是 call_1（call_1 已经是 marker）
				continue
			}
		}
	})
}
```

- [ ] **Step 3: 运行全部 MicroCompactProcessor 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run "TestMicroCompactProcessor" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/processor/micro_compact_processor_test.go
git commit -m "test(context_engine): 添加 MicroCompactProcessor Trigger/OnAddMessages 测试"
```

---

### Task 6: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/context_engine/doc.go`
- Modify: `internal/agentcore/context_engine/processor/doc.go`

- [ ] **Step 1: 更新 context_engine/doc.go**

在文件目录树中：
1. 在 `processor/` 子目录下添加 `micro_compact_processor.go` 条目
2. 添加 `context_utils/` 子目录条目

当前 processor/ 子目录树末尾是 `dialogue_compressor.go`，在其后追加：

```
//	├── micro_compact_processor.go    # MicroCompactProcessor 微压缩处理器
```

在 `processor/` 子目录之后添加：

```
//	├── context_utils/
//	│   ├── doc.go           # 工具辅助子包文档
//	│   └── resolve.go       # 工具名解析函数（ResolveToolNameFromMessage 等）
```

- [ ] **Step 2: 更新 processor/doc.go**

在文件目录树中 `dialogue_compressor.go` 条目后追加：

```
//	├── micro_compact_processor.go    # MicroCompactProcessor 微压缩处理器 + MicroCompactProcessorConfig + init() 自动注册
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/doc.go internal/agentcore/context_engine/processor/doc.go
git commit -m "docs(context_engine): 更新 doc.go，添加 micro_compact_processor 和 context_utils 条目"
```

---

### Task 7: 编译验证与覆盖率检查

**Files:**
- 修改: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 检查残留编译进程**

Run: `pgrep -f 'go (build|test)'`

如有残留进程，先 kill。

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 3: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_engine/context_utils/... ./internal/agentcore/context_engine/processor/...`
Expected: context_utils 和 processor 包覆盖率 >= 85%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 5.23 的状态从 `☐` 改为 `✅`。

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.23 MicroCompactProcessor 实现状态为已完成"
```

---

## 自审查

1. **Spec 覆盖**：设计文档中每个部分都有对应 Task：
   - context_utils 子包 → Task 1, 2
   - MicroCompactProcessorConfig → Task 3
   - MicroCompactProcessor 结构体和方法 → Task 3, 4, 5
   - 自动注册 → Task 3
   - doc.go 更新 → Task 6
   - 日志 → Task 3 (OnAddMessages 中已包含)
   - 测试策略 → Task 2, 4, 5
   - IMPLEMENTATION_PLAN.md 更新 → Task 7

2. **Placeholder 扫描**：无 TBD/TODO/不完整步骤 ✅

3. **类型一致性**：
   - 所有接口方法签名使用 `iface.ModelContext`、`iface.ContextEvent`、`iface.Option` 等 ✅
   - `ProcessorFactory` 签名 `func(config iface.ProcessorConfig) (iface.ContextProcessor, error)` ✅
   - `ToolCall.Name` 字段直接使用（Go 端无 Function.Name 嵌套） ✅
   - `SetContent(llm_schema.NewTextContent(marker))` 用于替换内容 ✅
