# 5.22 DialogueCompressor 对话压缩器实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现对话历史压缩器 DialogueCompressor，当上下文超过阈值时将已完成的 ReAct 轮次压缩为摘要消息。

**Architecture:** DialogueCompressor 实现 ContextProcessor 接口，嵌入 BaseProcessor 复用通用能力，内部持有 llm.Model 实例调用 LLM 生成压缩摘要。通过 context_engine 包的注册表实现 init() 自动注册。同步新增 ReplaceMessages 通用替换函数供后续处理器复用。

**Tech Stack:** Go 1.22+, llm.Model + JsonOutputParser, processor.BaseProcessor, context_engine.ProcessorFactory 注册表

**Spec:** `docs/superpowers/specs/2026-09-09-dialogue-compressor-design.md`

---

## Task 1: context_engine 注册表

**Files:**
- Create: `internal/agentcore/context_engine/registry.go`
- Create: `internal/agentcore/context_engine/registry_test.go`
- Modify: `internal/agentcore/context_engine/base.go:75`
- Modify: `internal/agentcore/context_engine/doc.go`

- [ ] **Step 1: 写 registry.go 的失败测试**

```go
// 文件: internal/agentcore/context_engine/registry_test.go
package context_engine

import (
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// mockProcessor 用于测试的 mock 处理器
type mockProcessor struct {
	*processor.BaseProcessor
}

func (m *mockProcessor) ProcessorType() string { return "MockProcessor" }

// mockConfig 用于测试的 mock 配置
type mockConfig struct{}

func (m *mockConfig) Validate() error { return nil }

// TestRegisterProcessorFactory 测试注册工厂函数
func TestRegisterProcessorFactory(t *testing.T) {
	// 清理注册表
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := func(config processor.ProcessorConfig) processor.ContextProcessor {
		return &mockProcessor{}
	}

	RegisterProcessorFactory("MockProcessor", factory)

	got, ok := GetProcessorFactory("MockProcessor")
	if !ok {
		t.Fatal("期望找到已注册的工厂，但未找到")
	}
	if got == nil {
		t.Fatal("期望工厂非 nil")
	}

	// 验证工厂能创建处理器实例
	cfg := &mockConfig{}
	p := got(cfg)
	if p == nil {
		t.Fatal("期望工厂创建处理器实例非 nil")
	}
	if p.ProcessorType() != "MockProcessor" {
		t.Fatalf("期望 ProcessorType=MockProcessor，实际=%s", p.ProcessorType())
	}
}

// TestGetProcessorFactory_NotFound 测试获取未注册的工厂
func TestGetProcessorFactory_NotFound(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	_, ok := GetProcessorFactory("NonExistent")
	if ok {
		t.Fatal("期望未注册的工厂返回 false")
	}
}

// TestListProcessorFactories 测试列出已注册的工厂
func TestListProcessorFactories(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := func(config processor.ProcessorConfig) processor.ContextProcessor {
		return &mockProcessor{}
	}

	RegisterProcessorFactory("ProcessorA", factory)
	RegisterProcessorFactory("ProcessorB", factory)

	types := ListProcessorFactories()
	if len(types) != 2 {
		t.Fatalf("期望 2 个已注册类型，实际 %d", len(types))
	}

	typeSet := make(map[string]bool)
	for _, tp := range types {
		typeSet[tp] = true
	}
	if !typeSet["ProcessorA"] || !typeSet["ProcessorB"] {
		t.Fatal("期望包含 ProcessorA 和 ProcessorB")
	}
}

// TestRegisterProcessorFactory_Concurrent 测试并发注册安全性
func TestRegisterProcessorFactory_Concurrent(t *testing.T) {
	processorFactoriesMu.Lock()
	processorFactories = make(map[string]ProcessorFactory)
	processorFactoriesMu.Unlock()

	factory := func(config processor.ProcessorConfig) processor.ContextProcessor {
		return &mockProcessor{}
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			RegisterProcessorFactory("ConcurrentProcessor", factory)
		}(i)
	}
	wg.Wait()

	_, ok := GetProcessorFactory("ConcurrentProcessor")
	if !ok {
		t.Fatal("并发注册后期望能找到工厂")
	}
}

// Suppress unused import warning
var _ llm_schema.BaseMessage
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/ -run TestRegister -v -count=1 2>&1 | head -20`
Expected: 编译失败（registry.go 不存在）

- [ ] **Step 3: 实现 registry.go**

```go
// 文件: internal/agentcore/context_engine/registry.go
package context_engine

import (
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProcessorFactory 处理器工厂函数类型。
//
// 根据 ProcessorConfig 创建对应的 ContextProcessor 实例。
// 对应 Python: ContextEngine._PROCESSOR_MAP 中存储的 processor_class，
// 运行时通过 processor_class(config) 创建实例。
type ProcessorFactory func(config processor.ProcessorConfig) processor.ContextProcessor

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// processorFactories 处理器工厂注册表
	processorFactories = make(map[string]ProcessorFactory)
	// processorFactoriesMu 注册表读写锁
	processorFactoriesMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterProcessorFactory 注册处理器工厂函数。
//
// 各处理器在 init() 函数中调用此函数将自己注册到全局注册表，
// 5.30 ContextEngine 门面实现时通过 GetProcessorFactory 获取工厂创建实例。
//
// 对应 Python: @ContextEngine.register_processor() 装饰器
func RegisterProcessorFactory(processorType string, factory ProcessorFactory) {
	processorFactoriesMu.Lock()
	defer processorFactoriesMu.Unlock()
	processorFactories[processorType] = factory
}

// GetProcessorFactory 获取处理器工厂函数。
//
// 返回工厂函数和是否找到的标志。5.30 ContextEngine._create_processor 对应使用。
//
// 对应 Python: ContextEngine._PROCESSOR_MAP.get(processor_type)
func GetProcessorFactory(processorType string) (ProcessorFactory, bool) {
	processorFactoriesMu.RLock()
	defer processorFactoriesMu.RUnlock()
	factory, ok := processorFactories[processorType]
	return factory, ok
}

// ListProcessorFactories 列出所有已注册的处理器类型名称。
//
// 返回排序后的类型名称列表，便于调试和诊断。
func ListProcessorFactories() []string {
	processorFactoriesMu.RLock()
	defer processorFactoriesMu.RUnlock()
	types := make([]string, 0, len(processorFactories))
	for k := range processorFactories {
		types = append(types, k)
	}
	sort.Strings(types)
	return types
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 修改 base.go 中 RegisterProcessor 接口签名**

将 `RegisterProcessor(processorType string, processor any)` 改为 `RegisterProcessor(processorType string, p processor.ContextProcessor)`。

在 `internal/agentcore/context_engine/base.go` 中：
- 旧: `RegisterProcessor(processorType string, processor any)`
- 新: `RegisterProcessor(processorType string, p processor.ContextProcessor)`

- [ ] **Step 5: 更新 context_engine/doc.go 文件目录**

在 `internal/agentcore/context_engine/doc.go` 的文件目录树中添加 `registry.go` 条目。

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/ -run TestRegister -v -count=1`
Expected: PASS

- [ ] **Step 7: 运行全量编译确认无破坏**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/context_engine/registry.go internal/agentcore/context_engine/registry_test.go internal/agentcore/context_engine/base.go internal/agentcore/context_engine/doc.go
git commit -m "feat(context_engine): 添加处理器工厂注册表 + RegisterProcessor 类型安全改造"
```

---

## Task 2: ReplaceMessages 通用替换函数

**Files:**
- Create: `internal/agentcore/context_engine/processor/replace.go`
- Create: `internal/agentcore/context_engine/processor/replace_test.go`
- Modify: `internal/agentcore/context_engine/processor/doc.go`

- [ ] **Step 1: 写 ReplaceMessages 的失败测试**

```go
// 文件: internal/agentcore/context_engine/processor/replace_test.go
package processor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestReplaceMessages_SingleReplacement 测试单个替换
func TestReplaceMessages_SingleReplacement(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewUserMessage("msg3"),
	}

	replacements := []Replacement{
		{
			StartIdx: 1,
			EndIdx:   2,
			Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("replaced")},
		},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 3 {
		t.Fatalf("期望 3 条消息，实际 %d", len(result))
	}
	if result[0].GetContent() != "msg0" {
		t.Fatalf("期望 msg0，实际 %s", result[0].GetContent())
	}
	if result[1].GetContent() != "replaced" {
		t.Fatalf("期望 replaced，实际 %s", result[1].GetContent())
	}
	if result[2].GetContent() != "msg3" {
		t.Fatalf("期望 msg3，实际 %s", result[2].GetContent())
	}
}

// TestReplaceMessages_MultipleReplacements 测试多个替换（从后往前）
func TestReplaceMessages_MultipleReplacements(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewUserMessage("msg3"),
		llm_schema.NewUserMessage("msg4"),
		llm_schema.NewUserMessage("msg5"),
	}

	replacements := []Replacement{
		{StartIdx: 1, EndIdx: 2, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("A")}},
		{StartIdx: 4, EndIdx: 5, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("B")}},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 4 {
		t.Fatalf("期望 4 条消息，实际 %d", len(result))
	}
	expected := []string{"msg0", "A", "msg3", "B"}
	for i, exp := range expected {
		if result[i].GetContent() != exp {
			t.Fatalf("位置 %d: 期望 %s，实际 %s", i, exp, result[i].GetContent())
		}
	}
}

// TestReplaceMessages_ExpansionReplacement 测试替换后消息数增加
func TestReplaceMessages_ExpansionReplacement(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
	}

	replacements := []Replacement{
		{
			StartIdx: 1,
			EndIdx:   1,
			Messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("a"),
				llm_schema.NewUserMessage("b"),
			},
		},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 4 {
		t.Fatalf("期望 4 条消息，实际 %d", len(result))
	}
	expected := []string{"msg0", "a", "b", "msg2"}
	for i, exp := range expected {
		if result[i].GetContent() != exp {
			t.Fatalf("位置 %d: 期望 %s，实际 %s", i, exp, result[i].GetContent())
		}
	}
}

// TestReplaceMessages_EmptyReplacements 测试空替换列表
func TestReplaceMessages_EmptyReplacements(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
	}

	result := ReplaceMessages(messages, nil)

	if len(result) != 2 {
		t.Fatalf("期望 2 条消息，实际 %d", len(result))
	}
}

// TestReplaceMessages_UnorderedReplacements 测试乱序输入（应自动按 StartIdx 降序处理）
func TestReplaceMessages_UnorderedReplacements(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewUserMessage("msg3"),
	}

	// 先写后面的替换，再写前面的，验证排序逻辑
	replacements := []Replacement{
		{StartIdx: 2, EndIdx: 3, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("B")}},
		{StartIdx: 0, EndIdx: 0, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("A")}},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 3 {
		t.Fatalf("期望 3 条消息，实际 %d", len(result))
	}
	expected := []string{"A", "msg1", "B"}
	for i, exp := range expected {
		if result[i].GetContent() != exp {
			t.Fatalf("位置 %d: 期望 %s，实际 %s", i, exp, result[i].GetContent())
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/ -run TestReplaceMessages -v -count=1 2>&1 | head -10`
Expected: 编译失败（replace.go 不存在）

- [ ] **Step 3: 实现 replace.go**

```go
// 文件: internal/agentcore/context_engine/processor/replace.go
package processor

import (
	"sort"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Replacement 消息替换描述，指定消息列表中某范围替换为新消息。
//
// 对应 Python: ContextUtils.replace_messages() 的参数封装
type Replacement struct {
	// StartIdx 被替换范围的起始索引（含）
	StartIdx int
	// EndIdx 被替换范围的结束索引（含）
	EndIdx int
	// Messages 替换后的消息列表
	Messages []llm_schema.BaseMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReplaceMessages 将消息列表中指定范围替换为新消息。
//
// 从后往前处理（避免索引偏移），每个 Replacement 将
// messages[startIdx:endIdx+1] 替换为 replacement.Messages。
//
// 对应 Python: ContextUtils.replace_messages() + DialogueCompressor._apply_replacements()
// ⤵️ 5.23-5.29 其他处理器均可复用此函数
func ReplaceMessages(messages []llm_schema.BaseMessage, replacements []Replacement) []llm_schema.BaseMessage {
	if len(replacements) == 0 {
		return messages
	}

	// 按 StartIdx 降序排序，从后往前替换避免索引偏移
	sorted := make([]Replacement, len(replacements))
	copy(sorted, replacements)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartIdx > sorted[j].StartIdx
	})

	updated := make([]llm_schema.BaseMessage, len(messages))
	copy(updated, messages)

	for _, r := range sorted {
		if r.StartIdx < 0 || r.EndIdx >= len(updated) || r.StartIdx > r.EndIdx {
			continue // 跳过无效索引，与 Python IndexError 行为对齐
		}
		// messages[:start] + replacement + messages[end+1:]
		replacement := make([]llm_schema.BaseMessage, len(r.Messages))
		copy(replacement, r.Messages)
		updated = append(updated[:r.StartIdx], append(replacement, updated[r.EndIdx+1:]...)...)
	}

	return updated
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/ -run TestReplaceMessages -v -count=1`
Expected: PASS

- [ ] **Step 5: 更新 processor/doc.go**

在 `internal/agentcore/context_engine/processor/doc.go` 的文件目录树中添加 `replace.go` 条目。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/replace.go internal/agentcore/context_engine/processor/replace_test.go internal/agentcore/context_engine/processor/doc.go
git commit -m "feat(processor): 添加 ReplaceMessages 通用消息替换函数"
```

---

## Task 3: DialogueCompressorConfig + 内部类型 + 常量

**Files:**
- Create: `internal/agentcore/context_engine/processor/dialogue_compressor.go`
- Create: `internal/agentcore/context_engine/processor/dialogue_compressor_test.go`

本 Task 实现配置结构体、内部类型、常量、构造函数、init() 注册，以及所有不依赖 LLM 调用的辅助方法。

- [ ] **Step 1: 写配置校验和轮次识别的失败测试**

```go
// 文件: internal/agentcore/context_engine/processor/dialogue_compressor_test.go
package processor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestDialogueCompressorConfig_Validate 测试配置校验
func TestDialogueCompressorConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  DialogueCompressorConfig
		wantErr bool
	}{
		{
			name: "合法配置",
			config: DialogueCompressorConfig{
				TokensThreshold:         10000,
				CompressionTargetTokens: 1800,
			},
			wantErr: false,
		},
		{
			name: "TokensThreshold为零",
			config: DialogueCompressorConfig{
				TokensThreshold:         0,
				CompressionTargetTokens: 1800,
			},
			wantErr: true,
		},
		{
			name: "CompressionTargetTokens为零",
			config: DialogueCompressorConfig{
				TokensThreshold:         10000,
				CompressionTargetTokens: 0,
			},
			wantErr: true,
		},
		{
			name: "MessagesThreshold为负数",
			config: DialogueCompressorConfig{
				TokensThreshold:         10000,
				CompressionTargetTokens: 1800,
				MessagesThreshold:       -1,
			},
			wantErr: true,
		},
		{
			name: "MessagesToKeep为负数",
			config: DialogueCompressorConfig{
				TokensThreshold:         10000,
				CompressionTargetTokens: 1800,
				MessagesToKeep:          -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestGetCompressPairs 测试对话轮次配对
func TestGetCompressPairs(t *testing.T) {
	tests := []struct {
		name     string
		messages []llm_schema.BaseMessage
		want     [][2]int
	}{
		{
			name:     "空消息列表",
			messages: nil,
			want:     nil,
		},
		{
			name: "纯对话：User→Assistant",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("hello"),
				llm_schema.NewAssistantMessage("hi"),
			},
			want: [][2]int{{0, 1}},
		},
		{
			name: "含 tool_calls 的 ReAct 轮次",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("查天气"),
				llm_schema.NewAssistantMessage("调用工具", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					llm_schema.NewToolCall("get_weather", `{"city":"beijing"}`, "tc1"),
				})),
				llm_schema.NewToolMessage("北京25°C", "tc1"),
				llm_schema.NewAssistantMessage("北京25度晴天"),
			},
			want: [][2]int{{0, 3}},
		},
		{
			name: "两轮对话",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("查北京"),
				llm_schema.NewAssistantMessage("调用", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					llm_schema.NewToolCall("get_weather", `{"city":"beijing"}`, "tc1"),
				})),
				llm_schema.NewToolMessage("25°C", "tc1"),
				llm_schema.NewAssistantMessage("北京25度"),
				llm_schema.NewUserMessage("查上海"),
				llm_schema.NewAssistantMessage("调用", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					llm_schema.NewToolCall("get_weather", `{"city":"shanghai"}`, "tc2"),
				})),
				llm_schema.NewToolMessage("28°C", "tc2"),
				llm_schema.NewAssistantMessage("上海28度"),
			},
			want: [][2]int{{0, 3}, {4, 7}},
		},
		{
			name: "Assistant 有 tool_calls 不作为终点",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("hello"),
				llm_schema.NewAssistantMessage("调用", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					llm_schema.NewToolCall("tool1", `{}`, "tc1"),
				})),
				// 缺少 ToolMessage 和最终 AssistantMessage → 未完成轮次
			},
			want: nil,
		},
		{
			name: "连续 UserMessage 只取第一个",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("a"),
				llm_schema.NewUserMessage("b"),
				llm_schema.NewAssistantMessage("reply"),
			},
			want: [][2]int{{0, 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCompressPairs(tt.messages)
			if len(got) != len(tt.want) {
				t.Fatalf("期望 %v 对，实际 %v 对", len(tt.want), len(got))
			}
			for i, pair := range got {
				if pair[0] != tt.want[i][0] || pair[1] != tt.want[i][1] {
					t.Errorf("配对 %d: 期望 [%d,%d]，实际 [%d,%d]",
						i, tt.want[i][0], tt.want[i][1], pair[0], pair[1])
				}
			}
		})
	}
}

// TestFindLastFinalAssistantIdx 测试查找最后一条最终 AssistantMessage
func TestFindLastFinalAssistantIdx(t *testing.T) {
	tests := []struct {
		name     string
		messages []llm_schema.BaseMessage
		want     int
	}{
		{
			name:     "空列表",
			messages: nil,
			want:     -1,
		},
		{
			name: "有最终 Assistant",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("hello"),
				llm_schema.NewAssistantMessage("reply"),
			},
			want: 1,
		},
		{
			name: "只有 tool_calls 的 Assistant",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("hello"),
				llm_schema.NewAssistantMessage("调用", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					llm_schema.NewToolCall("tool1", `{}`, "tc1"),
				})),
			},
			want: -1,
		},
		{
			name: "最后一个是不含 tool_calls 的 Assistant",
			messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("hello"),
				llm_schema.NewAssistantMessage("调用", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					llm_schema.NewToolCall("tool1", `{}`, "tc1"),
				})),
				llm_schema.NewToolMessage("result", "tc1"),
				llm_schema.NewAssistantMessage("final reply"),
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindLastFinalAssistantIdx(tt.messages)
			if got != tt.want {
				t.Errorf("期望 %d，实际 %d", tt.want, got)
			}
		})
	}
}

// TestGetCompressIdx 测试压缩截止位置计算
func TestGetCompressIdx(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewAssistantMessage("msg1"), // 最终 assistant
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewAssistantMessage("msg3"), // 最终 assistant
	}

	tests := []struct {
		name           string
		messagesToKeep int
		keepLastRound  bool
		want           int
	}{
		{
			name:           "不保留尾部+保留最后一轮",
			messagesToKeep: 0,
			keepLastRound:  true,
			want:           3, // min(lastFinalAssistant=3, len=4) = 3
		},
		{
			name:           "不保留尾部+不保留最后一轮",
			messagesToKeep: 0,
			keepLastRound:  false,
			want:           4, // len(messages)
		},
		{
			name:           "保留1条+保留最后一轮",
			messagesToKeep: 1,
			keepLastRound:  true,
			want:           3, // min(lastFinalAssistant=3, len-1=3) = 3
		},
		{
			name:           "保留2条+不保留最后一轮",
			messagesToKeep: 2,
			keepLastRound:  false,
			want:           2, // len(messages) - 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := &DialogueCompressor{
				messagesToKeep: tt.messagesToKeep,
				keepLastRound:  tt.keepLastRound,
			}
			got := dc.GetCompressIdx(messages)
			if got != tt.want {
				t.Errorf("期望 %d，实际 %d", tt.want, got)
			}
		})
	}
}

// TestWrapMemoryBlock 测试摘要包装格式
func TestWrapMemoryBlock(t *testing.T) {
	result := WrapMemoryBlock("test summary")
	if result[:23] != "[DIALOGUE_MEMORY_BLOCK]" {
		t.Errorf("期望以 [DIALOGUE_MEMORY_BLOCK] 开头")
	}
	if result[len(result)-12:] != "test summary" {
		t.Errorf("期望以 'test summary' 结尾，实际 %s", result[len(result)-12:])
	}
}

// TestIsValidBlocksPayload 测试 JSON payload 校验
func TestIsValidBlocksPayload(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    bool
	}{
		{"nil", nil, false},
		{"非map", "string", false},
		{"空map", map[string]any{}, false},
		{"有blocks但非list", map[string]any{"blocks": "not list"}, false},
		{"有效payload", map[string]any{"blocks": []any{}}, true},
		{"有效payload含内容", map[string]any{"blocks": []any{map[string]any{"block_id": "react_1", "summary": "test"}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidBlocksPayload(tt.input)
			if got != tt.want {
				t.Errorf("期望 %v，实际 %v", tt.want, got)
			}
		})
	}
}

// TestSerializeMessage 测试消息序列化
func TestSerializeMessage(t *testing.T) {
	tests := []struct {
		name  string
		index int
		msg   llm_schema.BaseMessage
		want  string
	}{
		{
			name:  "UserMessage",
			index: 0,
			msg:   llm_schema.NewUserMessage("hello"),
			want:  "[0] role=user | content=hello",
		},
		{
			name:  "AssistantMessage with tool_calls",
			index: 1,
			msg: llm_schema.NewAssistantMessage("thinking", llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("get_weather", `{}`, "tc1"),
			})),
			want: "[1] role=assistant | tool_calls=get_weather | content=thinking",
		},
		{
			name:  "ToolMessage",
			index: 2,
			msg:   llm_schema.NewToolMessage("result", "tc1"),
			want:  "[2] role=tool | tool_call_id=tc1 | content=result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SerializeMessage(tt.index, tt.msg)
			if got != tt.want {
				t.Errorf("期望 %q，实际 %q", tt.want, got)
			}
		})
	}
}

// TestEstimateContentTokens 测试 Token 估算
func TestEstimateContentTokens(t *testing.T) {
	tests := []struct {
		name    string
		content any
		want    int
	}{
		{"字符串", "abcdef", 2},      // len/3 = 6/3 = 2
		{"空字符串", "", 0},
		{"非字符串", 12345, 2},        // json.Marshal → "12345" → len/3 = 5/3 = 1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateContentTokens(tt.content)
			if got != tt.want {
				t.Errorf("期望 %d，实际 %d", tt.want, got)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/ -run "TestDialogueCompressor|TestGetCompressPairs|TestFindLastFinalAssistant|TestGetCompressIdx|TestWrapMemoryBlock|TestIsValidBlocksPayload|TestSerializeMessage|TestEstimateContentTokens" -v -count=1 2>&1 | head -10`
Expected: 编译失败

- [ ] **Step 3: 实现 dialogue_compressor.go**

完整实现文件，包含：
- `DialogueCompressorConfig` 结构体 + `Validate()`
- `compressTarget` / `dialogueRound` 内部类型
- `DialogueCompressor` 结构体 + 构造函数 + init() 注册
- `defaultCompressionPrompt` 常量 + `dialogueMemoryBlockMarker` 常量
- `DialogueCompressorOption` / `WithCompressorModel` 选项
- `GetCompressPairs` / `FindLastFinalAssistantIdx` / `GetCompressIdx` / `BuildCompressTargets` / `CollectCompleteRounds`
- `SerializeMessage` / `BuildSplitContextPayload` / `BuildTargetsPayload`
- `WrapMemoryBlock` / `BuildMemoryMessage`
- `IsValidBlocksPayload` / `HasCompressionBenefit` / `CountMessagesTokens` / `EstimateContentTokens`
- `ExtractCompactSummaryFromReplacements`
- `TriggerAddMessages` / `OnAddMessages` / `InvokeMultiBlockCompression`
- `BuildJSONReplacements` / `BuildFallbackReplacement`
- `SaveState` / `LoadState` / `ProcessorType`

代码参照 Python `dialogue_compressor.py` 逐方法对齐实现，所有注释使用中文，遵循项目编码规范（结构体→枚举→常量→全局变量→导出函数→非导出函数）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/ -run "TestDialogueCompressor|TestGetCompressPairs|TestFindLastFinalAssistant|TestGetCompressIdx|TestWrapMemoryBlock|TestIsValidBlocksPayload|TestSerializeMessage|TestEstimateContentTokens" -v -count=1`
Expected: PASS

- [ ] **Step 5: 更新 processor/doc.go 文件目录**

在 `internal/agentcore/context_engine/processor/doc.go` 的文件目录树中添加 `dialogue_compressor.go` 条目。

- [ ] **Step 6: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/context_engine/processor/dialogue_compressor.go internal/agentcore/context_engine/processor/dialogue_compressor_test.go internal/agentcore/context_engine/processor/doc.go
git commit -m "feat(processor): 实现 DialogueCompressor 对话压缩器 (5.22)"
```

---

## Task 4: OnAddMessages 集成测试 + 补充测试

**Files:**
- Modify: `internal/agentcore/context_engine/processor/dialogue_compressor_test.go`

- [ ] **Step 1: 写 OnAddMessages 集成测试（使用 mock Model 和 mock ModelContext）**

测试 OnAddMessages 端到端流程：
- mock Model 返回含 ParserContent 的 AssistantMessage
- mock ModelContext 提供 SetMessages/TokenCounter/Len/GetMessages
- 验证替换结果、ContextEvent 字段、压缩收益判断

测试 BuildSplitContextPayload / BuildTargetsPayload 的输出格式。

测试 TriggerAddMessages 的触发条件（消息数阈值 / Token 阈值）。

测试 Fallback 路径（JSON 解析失败时用 LLM 原始输出）。

测试 MODEL_CALL_FAILED 错误被捕获后的降级行为。

- [ ] **Step 2: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/ -v -count=1`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_engine/processor/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/processor/dialogue_compressor_test.go
git commit -m "test(processor): 补充 DialogueCompressor OnAddMessages 集成测试"
```

---

## Task 5: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 5.22 状态为 ✅**

将 `| 5.22 | ☐ | DialogueCompressor | 对话压缩器 |` 更新为：
`| 5.22 | ✅ | DialogueCompressor | ✅ DialogueCompressorConfig（值类型+默认值，8字段）；✅ compressTarget/dialogueRound 内部类型；✅ 核心方法链（OnAddMessages→GetCompressIdx→BuildCompressTargets→InvokeMultiBlockCompression→BuildJSONReplacements→ApplyReplacements）；✅ Fallback 路径；✅ LLM 调用通过 llm.Model + JsonOutputParser + response.ParserContent；✅ init() 自动注册到 context_engine.RegisterProcessorFactory；✅ ReplaceMessages 通用替换函数（⤵️ 5.23-5.29 复用）；✅ context_engine 注册表（ProcessorFactory + Register/Get/List）；✅ RegisterProcessor 接口参数 any→ContextProcessor 类型安全改造；⤵️ 5.30 回填 ContextEngine 门面使用注册表；⤴️ 5.31 回填 ModelContext.SetMessages/TokenCounter | `openjiuwen/core/context_engine/processor/compressor/` (DialogueCompressor) |`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.22 DialogueCompressor 实现状态为已完成"
```
