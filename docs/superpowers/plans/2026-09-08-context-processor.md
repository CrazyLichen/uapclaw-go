# 5.21 ContextProcessor 插件基类实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ContextProcessor 接口和 BaseProcessor 结构体，为所有上下文处理器提供统一契约和默认实现。

**Architecture:** 接口 + 嵌入结构体模式。定义 ContextProcessor 接口（7个方法），BaseProcessor 结构体实现所有默认行为。具体处理器嵌入 BaseProcessor，只覆写感兴趣的钩子。按职责拆文件：base.go（核心定义）、hooks.go（钩子默认）、state.go（状态持久化）、offload.go（卸载方法族）、usage.go（用量追踪）、round.go（API 轮次分组）。

**Tech Stack:** Go 1.22+, github.com/google/uuid, github.com/uapclaw/uapclaw-go 内部包

---

### Task 1: base.go — 核心定义（接口 + 结构体 + Option 模式）

**Files:**
- Modify: `internal/agentcore/context_engine/processor/base.go`
- Modify: `internal/agentcore/context_engine/processor/base_test.go`

- [ ] **Step 1: 重写 base.go，添加接口定义和结构体**

将现有 base.go 中的 ContextEvent 保留，新增 ProcessorConfig、ContextProcessor、ProcessorOption、BaseProcessor 定义。注意保持源码声明排列顺序规范。

```go
package processor

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// ProcessorConfig 处理器配置接口，所有处理器配置必须实现。
//
// 各具体处理器定义自己的 Config 结构体并实现此接口，
// 基类通过接口持有配置，子类通过类型断言获取具体配置。
//
// 对应 Python: pydantic.BaseModel（作为处理器配置基类）
type ProcessorConfig interface {
	// Validate 校验配置参数
	Validate() error
}

// ContextProcessor 上下文处理器接口，所有处理器插件必须实现。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages      — 消息即将被添加时
//  2. OnGetContextWindow  — 上下文窗口即将返回时
//
// 每个处理器通过 Trigger* 方法判断是否介入，仅在返回 true 时
// 才调用对应的 On* 方法执行实际处理。实现必须是无状态的，
// 或通过 SaveState/LoadState 支持跨会话恢复。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextProcessor)
type ContextProcessor interface {
	// OnAddMessages 处理即将添加的消息，返回 ContextEvent 和变换后的消息列表。
	// 仅在 TriggerAddMessages 返回 true 时调用。
	OnAddMessages(ctx context.Context, mc context_engine.ModelContext, messages []llm_schema.BaseMessage, opts ...Option) (*ContextEvent, []llm_schema.BaseMessage, error)

	// OnGetContextWindow 处理即将返回的上下文窗口，返回 ContextEvent 和变换后的窗口。
	// 仅在 TriggerGetContextWindow 返回 true 时调用。
	OnGetContextWindow(ctx context.Context, mc context_engine.ModelContext, cw context_engine.ContextWindow, opts ...Option) (*ContextEvent, context_engine.ContextWindow, error)

	// TriggerAddMessages 判断是否需要介入消息添加。每次 AddMessages 调用均执行，必须轻量。
	TriggerAddMessages(ctx context.Context, mc context_engine.ModelContext, messages []llm_schema.BaseMessage, opts ...Option) (bool, error)

	// TriggerGetContextWindow 判断是否需要介入上下文窗口获取。每次 GetContextWindow 调用均执行，必须轻量。
	TriggerGetContextWindow(ctx context.Context, mc context_engine.ModelContext, cw context_engine.ContextWindow, opts ...Option) (bool, error)

	// SaveState 导出处理器内部状态为可序列化的 map。
	SaveState() map[string]any

	// LoadState 从 map 恢复处理器内部状态。
	LoadState(state map[string]any)

	// ProcessorType 返回处理器类型标识字符串（Go 结构体名）。
	ProcessorType() string
}

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvent 上下文处理器执行结果，由各 Processor 的 OnAddMessages / OnGetContextWindow 返回。
//
// 当处理器实际执行了操作时返回非 nil 的 ContextEvent，携带修改了哪些消息索引、
// 压缩摘要和压缩用量信息。Context 实例读取这些字段构建 ContextCompressionState。
// 处理器未触发（noop）时返回 nil。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextEvent)
type ContextEvent struct {
	// EventType 处理器类型标识（如 "DialogueCompressor"、"MessageOffloader"）
	EventType string `json:"event_type"`
	// MessagesToModify 被处理器修改的消息索引列表
	MessagesToModify []int `json:"messages_to_modify"`
	// CompactSummary 压缩摘要文本
	CompactSummary string `json:"compact_summary"`
	// CompressionUsage 压缩调用用量（token 数、费用等）
	CompressionUsage map[string]any `json:"compression_usage,omitempty"`
}

// ProcessorOption 处理器可选参数，替代 Python **kwargs。
//
// 对应 Python: ContextProcessor.offload_messages(**kwargs) 中的关键字参数
type ProcessorOption struct {
	// SysOperation 系统操作接口
	// ⤵️ 9.32 回填：将 any 替换为 SysOperation 接口类型
	SysOperation any
	// OffloadHandle 卸载句柄，未指定时自动生成 UUID
	OffloadHandle string
	// OffloadType 卸载类型："filesystem" 或 "in_memory"
	OffloadType string
	// OffloadPath 卸载文件路径，未指定时自动生成
	OffloadPath string
	// Extra 额外参数
	Extra map[string]any
}

// BaseProcessor 上下文处理器基类，提供所有处理器的默认实现。
//
// 具体处理器嵌入此结构体，只需覆写感兴趣的钩子方法。
// 默认行为：Trigger* 返回 false（不触发），On* 透传输入，
// SaveState/LoadState 空操作。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextProcessor)
type BaseProcessor struct {
	// config 处理器配置，各子类实现 ProcessorConfig 接口
	config ProcessorConfig
	// compressionUsage 压缩调用用量追踪
	compressionUsage map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseProcessor 创建处理器基类实例
func NewBaseProcessor(config ProcessorConfig) *BaseProcessor {
	return &BaseProcessor{
		config: config,
	}
}

// Config 返回处理器配置（只读）
func (p *BaseProcessor) Config() ProcessorConfig {
	return p.config
}

// Option 处理器选项函数类型
type Option func(*ProcessorOption)

// WithSysOperation 设置系统操作接口
// ⤵️ 9.32 回填参数类型
func WithSysOperation(op any) Option {
	return func(o *ProcessorOption) { o.SysOperation = op }
}

// WithOffloadHandle 设置卸载句柄
func WithOffloadHandle(handle string) Option {
	return func(o *ProcessorOption) { o.OffloadHandle = handle }
}

// WithOffloadType 设置卸载类型
func WithOffloadType(offloadType string) Option {
	return func(o *ProcessorOption) { o.OffloadType = offloadType }
}

// WithOffloadPath 设置卸载文件路径
func WithOffloadPath(path string) Option {
	return func(o *ProcessorOption) { o.OffloadPath = path }
}

// WithExtra 设置额外参数
func WithExtra(key string, value any) Option {
	return func(o *ProcessorOption) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// newProcessorOption 从选项列表构建 ProcessorOption
func newProcessorOption(opts ...Option) *ProcessorOption {
	po := &ProcessorOption{}
	for _, opt := range opts {
		opt(po)
	}
	return po
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 更新 base_test.go，添加新类型的测试**

在现有测试后面追加 ProcessorConfig、BaseProcessor、ProcessorOption 的测试：

```go
// testConfig 测试用处理器配置
type testConfig struct {
	Name  string
	Value int
}

func (c *testConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	return nil
}

// TestProcessorConfig_校验通过 验证合法配置
func TestProcessorConfig_校验通过(t *testing.T) {
	c := &testConfig{Name: "test", Value: 10}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() 不应返回错误，实际: %v", err)
	}
}

// TestProcessorConfig_校验失败 验证非法配置
func TestProcessorConfig_校验失败(t *testing.T) {
	c := &testConfig{Name: "", Value: 10}
	if err := c.Validate(); err == nil {
		t.Error("Validate() 应返回错误，实际 nil")
	}
}

// TestNewBaseProcessor 验证 BaseProcessor 构造
func TestNewBaseProcessor(t *testing.T) {
	c := &testConfig{Name: "compressor", Value: 5}
	p := NewBaseProcessor(c)
	if p == nil {
		t.Fatal("NewBaseProcessor 返回 nil")
	}
	if p.Config() != c {
		t.Error("Config() 应返回传入的配置")
	}
	if p.compressionUsage != nil {
		t.Error("compressionUsage 初始值应为 nil")
	}
}

// TestProcessorOption_默认值 验证 ProcessorOption 零值
func TestProcessorOption_默认值(t *testing.T) {
	po := newProcessorOption()
	if po.SysOperation != nil {
		t.Error("SysOperation 默认应为 nil")
	}
	if po.OffloadHandle != "" {
		t.Error("OffloadHandle 默认应为空")
	}
	if po.OffloadType != "" {
		t.Error("OffloadType 默认应为空")
	}
	if po.OffloadPath != "" {
		t.Error("OffloadPath 默认应为空")
	}
	if po.Extra != nil {
		t.Error("Extra 默认应为 nil")
	}
}

// TestProcessorOption_选项函数 验证 With* 选项函数
func TestProcessorOption_选项函数(t *testing.T) {
	po := newProcessorOption(
		WithOffloadHandle("abc123"),
		WithOffloadType("filesystem"),
		WithOffloadPath("/tmp/offload.json"),
		WithExtra("key1", "value1"),
	)
	if po.OffloadHandle != "abc123" {
		t.Errorf("OffloadHandle = %q, want abc123", po.OffloadHandle)
	}
	if po.OffloadType != "filesystem" {
		t.Errorf("OffloadType = %q, want filesystem", po.OffloadType)
	}
	if po.OffloadPath != "/tmp/offload.json" {
		t.Errorf("OffloadPath = %q, want /tmp/offload.json", po.OffloadPath)
	}
	if po.Extra["key1"] != "value1" {
		t.Errorf("Extra[key1] = %v, want value1", po.Extra["key1"])
	}
}

// TestWithSysOperation 验证 SysOperation 选项
func TestWithSysOperation(t *testing.T) {
	op := struct{}{}
	po := newProcessorOption(WithSysOperation(op))
	if po.SysOperation == nil {
		t.Error("SysOperation 不应为 nil")
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/base.go internal/agentcore/context_engine/processor/base_test.go
git commit -m "feat(processor): 添加 ContextProcessor 接口、ProcessorConfig 接口、BaseProcessor 结构体和 Option 模式"
```

---

### Task 2: hooks.go — 钩子默认实现 + ProcessorType + IsAPIRound

**Files:**
- Create: `internal/agentcore/context_engine/processor/hooks.go`
- Create: `internal/agentcore/context_engine/processor/hooks_test.go`

- [ ] **Step 1: 创建 hooks.go**

```go
package processor

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ProcessorType 返回处理器类型标识。
//
// 默认返回空字符串，具体处理器应覆写此方法返回自身结构体名。
// 对应 Python: ContextProcessor.processor_type()（由元类自动注入类名）
func (p *BaseProcessor) ProcessorType() string {
	return ""
}

// OnAddMessages 默认透传消息（no-op）。
//
// 仅在 TriggerAddMessages 返回 true 时被调用。
// 默认实现直接返回输入的消息列表，不执行任何变换。
//
// 对应 Python: ContextProcessor.on_add_messages() 默认实现
func (p *BaseProcessor) OnAddMessages(_ context.Context, _ context_engine.ModelContext, messages []llm_schema.BaseMessage, _ ...Option) (*ContextEvent, []llm_schema.BaseMessage, error) {
	return nil, messages, nil
}

// OnGetContextWindow 默认透传上下文窗口（no-op）。
//
// 仅在 TriggerGetContextWindow 返回 true 时被调用。
// 默认实现直接返回输入的上下文窗口，不执行任何变换。
//
// 对应 Python: ContextProcessor.on_get_context_window() 默认实现
func (p *BaseProcessor) OnGetContextWindow(_ context.Context, _ context_engine.ModelContext, cw context_engine.ContextWindow, _ ...Option) (*ContextEvent, context_engine.ContextWindow, error) {
	return nil, cw, nil
}

// TriggerAddMessages 默认不触发。
//
// 每次消息添加时调用，必须轻量。
// 默认实现始终返回 false，表示此处理器不需要介入。
//
// 对应 Python: ContextProcessor.trigger_add_messages() 默认实现
func (p *BaseProcessor) TriggerAddMessages(_ context.Context, _ context_engine.ModelContext, _ []llm_schema.BaseMessage, _ ...Option) (bool, error) {
	return false, nil
}

// TriggerGetContextWindow 默认不触发。
//
// 每次上下文窗口获取时调用，必须轻量。
// 默认实现始终返回 false，表示此处理器不需要介入。
//
// 对应 Python: ContextProcessor.trigger_get_context_window() 默认实现
func (p *BaseProcessor) TriggerGetContextWindow(_ context.Context, _ context_engine.ModelContext, _ context_engine.ContextWindow, _ ...Option) (bool, error) {
	return false, nil
}

// IsAPIRound 判断消息列表是否构成一个完整的 API 轮次。
//
// 通过调用 GroupCompletedAPIRounds 判断最后一条消息
// 是否恰好落在某个已完成轮次的结束位置。
//
// 对应 Python: ContextProcessor._api_round(messages)
func (p *BaseProcessor) IsAPIRound(messages []llm_schema.BaseMessage) bool {
	rounds := GroupCompletedAPIRounds(messages)
	if len(rounds) == 0 {
		return false
	}
	lastEnd := rounds[len(rounds)-1][1]
	return lastEnd == len(messages)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 hooks_test.go**

```go
package processor

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestProcessorType_默认返回空字符串 验证基类 ProcessorType 默认值
func TestProcessorType_默认返回空字符串(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	if pt := p.ProcessorType(); pt != "" {
		t.Errorf("ProcessorType() = %q, want 空字符串", pt)
	}
}

// TestOnAddMessages_默认透传 验证默认透传消息
func TestOnAddMessages_默认透传(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}
	event, result, err := p.OnAddMessages(context.Background(), nil, msgs)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event != nil {
		t.Error("默认 OnAddMessages 应返回 nil ContextEvent")
	}
	if len(result) != len(msgs) {
		t.Errorf("结果消息数 = %d, want %d", len(result), len(msgs))
	}
}

// TestOnAddMessages_空消息列表 验证空消息列表透传
func TestOnAddMessages_空消息列表(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	event, result, err := p.OnAddMessages(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event != nil {
		t.Error("默认 OnAddMessages 应返回 nil ContextEvent")
	}
	if result != nil {
		t.Errorf("空消息列表应透传 nil，实际 %v", result)
	}
}

// TestOnGetContextWindow_默认透传 验证默认透传上下文窗口
func TestOnGetContextWindow_默认透传(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	cw := context_engine.ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{llm_schema.NewSystemMessage("sys")},
		ContextMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
	}
	event, result, err := p.OnGetContextWindow(context.Background(), nil, cw)
	if err != nil {
		t.Fatalf("OnGetContextWindow 返回错误: %v", err)
	}
	if event != nil {
		t.Error("默认 OnGetContextWindow 应返回 nil ContextEvent")
	}
	if len(result.SystemMessages) != 1 || len(result.ContextMessages) != 1 {
		t.Error("透传后消息数量不一致")
	}
}

// TestTriggerAddMessages_默认不触发 验证默认不触发
func TestTriggerAddMessages_默认不触发(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	triggered, err := p.TriggerAddMessages(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if triggered {
		t.Error("默认 TriggerAddMessages 应返回 false")
	}
}

// TestTriggerGetContextWindow_默认不触发 验证默认不触发
func TestTriggerGetContextWindow_默认不触发(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	triggered, err := p.TriggerGetContextWindow(context.Background(), nil, context_engine.ContextWindow{})
	if err != nil {
		t.Fatalf("TriggerGetContextWindow 返回错误: %v", err)
	}
	if triggered {
		t.Error("默认 TriggerGetContextWindow 应返回 false")
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/...`
Expected: 编译成功（此时可能因缺少 round.go 中的 GroupCompletedAPIRounds 而失败，先跳过，Task 6 补全后统一验证）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/processor/hooks.go internal/agentcore/context_engine/processor/hooks_test.go
git commit -m "feat(processor): 添加 BaseProcessor 钩子默认实现和 ProcessorType"
```

---

### Task 3: state.go — 状态持久化默认实现

**Files:**
- Create: `internal/agentcore/context_engine/processor/state.go`
- Create: `internal/agentcore/context_engine/processor/state_test.go`

- [ ] **Step 1: 创建 state.go**

```go
package processor

// ──────────────────────────── 导出函数 ────────────────────────────

// SaveState 导出处理器内部状态为可序列化的 map。
//
// 默认实现返回空 map。具体处理器应覆写此方法，
// 将自身状态导出为 JSON 兼容的 map。
//
// 对应 Python: ContextProcessor.save_state()（抽象方法）
func (p *BaseProcessor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态。
//
// 默认实现为空操作。具体处理器应覆写此方法，
// 从 JSON 兼容的 map 中恢复自身状态。
//
// 对应 Python: ContextProcessor.load_state()（抽象方法）
func (p *BaseProcessor) LoadState(_ map[string]any) {
	// 默认空操作
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 state_test.go**

```go
package processor

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSaveState_默认返回空map 验证默认 SaveState
func TestSaveState_默认返回空map(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	state := p.SaveState()
	if state == nil {
		t.Error("SaveState() 不应返回 nil")
	}
	if len(state) != 0 {
		t.Errorf("默认 SaveState 应返回空 map，实际 %d 项", len(state))
	}
}

// TestLoadState_默认空操作 验证默认 LoadState 不会 panic
func TestLoadState_默认空操作(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	// 不应 panic
	p.LoadState(map[string]any{"key": "value"})
}

// TestSaveState_LoadState_往返 验证默认实现的保存/恢复往返
func TestSaveState_LoadState_往返(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	state := p.SaveState()
	p2 := NewBaseProcessor(c)
	p2.LoadState(state)
	state2 := p2.SaveState()
	if len(state2) != 0 {
		t.Errorf("往返后 SaveState 应仍为空 map，实际 %d 项", len(state2))
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run TestSaveState -v && go test ./internal/agentcore/context_engine/processor/... -run TestLoadState -v`
Expected: 通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/state.go internal/agentcore/context_engine/processor/state_test.go
git commit -m "feat(processor): 添加 BaseProcessor SaveState/LoadState 默认实现"
```

---

### Task 4: usage.go — CompressionUsage 追踪方法族

**Files:**
- Create: `internal/agentcore/context_engine/processor/usage.go`
- Create: `internal/agentcore/context_engine/processor/usage_test.go`

- [ ] **Step 1: 创建 usage.go**

```go
package processor

import llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 导出函数 ────────────────────────────

// ResetCompressionUsage 重置压缩用量追踪。
//
// 对应 Python: ContextProcessor._reset_compression_usage()
func (p *BaseProcessor) ResetCompressionUsage() {
	p.compressionUsage = nil
}

// RecordCompressionUsage 记录压缩 LLM 调用的用量。
//
// 从 AssistantMessage 的 UsageMetadata 字段提取用量信息并合并到基类追踪中。
//
// 对应 Python: ContextProcessor._record_compression_usage(response)
func (p *BaseProcessor) RecordCompressionUsage(response *llm_schema.AssistantMessage) {
	usage := ExtractUsageMetadata(response)
	if usage == nil {
		return
	}
	p.compressionUsage = MergeCompressionUsage(p.compressionUsage, usage)
}

// CurrentCompressionUsage 获取当前压缩用量快照。
//
// 返回用量 map 的副本，避免外部修改影响内部状态。
//
// 对应 Python: ContextProcessor._current_compression_usage()
func (p *BaseProcessor) CurrentCompressionUsage() map[string]any {
	if p.compressionUsage == nil {
		return nil
	}
	result := make(map[string]any, len(p.compressionUsage))
	for k, v := range p.compressionUsage {
		result[k] = v
	}
	return result
}

// ExtractUsageMetadata 从 AssistantMessage 中提取用量元数据，
// 转换为标准 map 格式。
//
// 提取结果映射：
//
//	calls=1, input_tokens, output_tokens, total_tokens, cache_tokens,
//	input_cost, output_cost, total_cost, model_name, details=[data]
//
// 对应 Python: ContextProcessor._extract_usage_metadata(response)
func ExtractUsageMetadata(msg *llm_schema.AssistantMessage) map[string]any {
	if msg == nil || msg.UsageMetadata == nil {
		return nil
	}
	um := msg.UsageMetadata
	return map[string]any{
		"calls":         1,
		"input_tokens":  um.InputTokens,
		"output_tokens": um.OutputTokens,
		"total_tokens":  um.TotalTokens,
		"cache_tokens":  um.CacheTokens,
		"input_cost":    um.InputCost,
		"output_cost":   um.OutputCost,
		"total_cost":    um.TotalCost,
		"model_name":    um.ModelName,
		"details":       []map[string]any{usageMetadataToMap(um)},
	}
}

// MergeCompressionUsage 合并两份压缩用量。
//
// 合并规则（与 Python 对齐）：
//   - calls, input_tokens, output_tokens, total_tokens, cache_tokens → 累加（int）
//   - input_cost, output_cost, total_cost → 累加（float64）
//   - model_name → 取 left 非空值，否则取 right
//   - details → 追加合并
//
// 对应 Python: ContextProcessor._merge_compression_usage(left, right)
func MergeCompressionUsage(left, right map[string]any) map[string]any {
	if left == nil {
		if right == nil {
			return nil
		}
		return copyMap(right)
	}
	if right == nil {
		return copyMap(left)
	}
	merged := copyMap(left)

	// 累加整数字段
	for _, key := range []string{"calls", "input_tokens", "output_tokens", "total_tokens", "cache_tokens"} {
		merged[key] = toInt(merged[key]) + toInt(right[key])
	}
	// 累加浮点数字段
	for _, key := range []string{"input_cost", "output_cost", "total_cost"} {
		merged[key] = toFloat64(merged[key]) + toFloat64(right[key])
	}
	// model_name 取 left 非空值，否则取 right
	if merged["model_name"] == "" || merged["model_name"] == nil {
		merged["model_name"] = right["model_name"]
	}
	// details 追加合并
	leftDetails := toSliceOfMaps(merged["details"])
	rightDetails := toSliceOfMaps(right["details"])
	merged["details"] = append(leftDetails, rightDetails...)

	return merged
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// usageMetadataToMap 将 UsageMetadata 转为 map
func usageMetadataToMap(um *llm_schema.UsageMetadata) map[string]any {
	return map[string]any{
		"code":                um.Code,
		"err_msg":             um.ErrMsg,
		"model_name":          um.ModelName,
		"input_tokens":        um.InputTokens,
		"output_tokens":       um.OutputTokens,
		"total_tokens":        um.TotalTokens,
		"cache_tokens":        um.CacheTokens,
		"input_cost":          um.InputCost,
		"output_cost":         um.OutputCost,
		"total_cost":          um.TotalCost,
		"total_latency":       um.TotalLatency,
		"first_token_time":    um.FirstTokenTime,
		"request_start_time":  um.RequestStartTime,
	}
}

// copyMap 创建 map 的浅拷贝
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// toInt 将 any 转为 int
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// toFloat64 将 any 转为 float64
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// toSliceOfMaps 将 any 转为 []map[string]any
func toSliceOfMaps(v any) []map[string]any {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []map[string]any:
		return s
	case []any:
		result := make([]map[string]any, 0, len(s))
		for _, item := range s {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	default:
		return nil
	}
}
```

- [ ] **Step 2: 创建 usage_test.go**

```go
package processor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtractUsageMetadata_nil消息 验证 nil 消息返回 nil
func TestExtractUsageMetadata_nil消息(t *testing.T) {
	result := ExtractUsageMetadata(nil)
	if result != nil {
		t.Error("nil 消息应返回 nil")
	}
}

// TestExtractUsageMetadata_无UsageMetadata 验证无 UsageMetadata 返回 nil
func TestExtractUsageMetadata_无UsageMetadata(t *testing.T) {
	msg := llm_schema.NewAssistantMessage("hello")
	result := ExtractUsageMetadata(msg)
	if result != nil {
		t.Error("无 UsageMetadata 应返回 nil")
	}
}

// TestExtractUsageMetadata_正常提取 验证正常提取用量
func TestExtractUsageMetadata_正常提取(t *testing.T) {
	msg := llm_schema.NewAssistantMessage("hello",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
			CacheTokens:  30,
			InputCost:    0.01,
			OutputCost:   0.005,
			TotalCost:    0.015,
			ModelName:    "qwen-max",
		}),
	)
	result := ExtractUsageMetadata(msg)
	if result == nil {
		t.Fatal("提取结果不应为 nil")
	}
	if result["calls"] != 1 {
		t.Errorf("calls = %v, want 1", result["calls"])
	}
	if result["input_tokens"] != 100 {
		t.Errorf("input_tokens = %v, want 100", result["input_tokens"])
	}
	if result["output_tokens"] != 50 {
		t.Errorf("output_tokens = %v, want 50", result["output_tokens"])
	}
	if result["total_tokens"] != 150 {
		t.Errorf("total_tokens = %v, want 150", result["total_tokens"])
	}
	if result["cache_tokens"] != 30 {
		t.Errorf("cache_tokens = %v, want 30", result["cache_tokens"])
	}
	if result["model_name"] != "qwen-max" {
		t.Errorf("model_name = %v, want qwen-max", result["model_name"])
	}
	details, ok := result["details"].([]map[string]any)
	if !ok || len(details) != 1 {
		t.Fatalf("details 应为长度1的切片，实际 %v", result["details"])
	}
	if details[0]["model_name"] != "qwen-max" {
		t.Errorf("details[0].model_name = %v, want qwen-max", details[0]["model_name"])
	}
}

// TestMergeCompressionUsage_左nil 验证左参数为 nil
func TestMergeCompressionUsage_左nil(t *testing.T) {
	right := map[string]any{"calls": 1, "total_tokens": 100}
	result := MergeCompressionUsage(nil, right)
	if result["calls"] != 1 {
		t.Errorf("calls = %v, want 1", result["calls"])
	}
}

// TestMergeCompressionUsage_右nil 验证右参数为 nil
func TestMergeCompressionUsage_右nil(t *testing.T) {
	left := map[string]any{"calls": 2, "total_tokens": 200}
	result := MergeCompressionUsage(left, nil)
	if result["calls"] != 2 {
		t.Errorf("calls = %v, want 2", result["calls"])
	}
}

// TestMergeCompressionUsage_双方nil 验证双方为 nil
func TestMergeCompressionUsage_双方nil(t *testing.T) {
	result := MergeCompressionUsage(nil, nil)
	if result != nil {
		t.Error("双方 nil 应返回 nil")
	}
}

// TestMergeCompressionUsage_累加合并 验证累加合并逻辑
func TestMergeCompressionUsage_累加合并(t *testing.T) {
	left := map[string]any{
		"calls":         1,
		"input_tokens":  100,
		"output_tokens": 50,
		"total_tokens":  150,
		"cache_tokens":  30,
		"input_cost":    0.01,
		"output_cost":   0.005,
		"total_cost":    0.015,
		"model_name":    "qwen-max",
		"details":       []map[string]any{{"total_tokens": 150}},
	}
	right := map[string]any{
		"calls":         1,
		"input_tokens":  200,
		"output_tokens": 80,
		"total_tokens":  280,
		"cache_tokens":  40,
		"input_cost":    0.02,
		"output_cost":   0.008,
		"total_cost":    0.028,
		"model_name":    "qwen-plus",
		"details":       []map[string]any{{"total_tokens": 280}},
	}
	result := MergeCompressionUsage(left, right)
	if result["calls"] != 2 {
		t.Errorf("calls = %v, want 2", result["calls"])
	}
	if result["input_tokens"] != 300 {
		t.Errorf("input_tokens = %v, want 300", result["input_tokens"])
	}
	if result["total_tokens"] != 430 {
		t.Errorf("total_tokens = %v, want 430", result["total_tokens"])
	}
	if result["model_name"] != "qwen-max" {
		t.Errorf("model_name = %v, want qwen-max（取 left 非空值）", result["model_name"])
	}
	details, ok := result["details"].([]map[string]any)
	if !ok || len(details) != 2 {
		t.Fatalf("details 应为长度2的切片，实际 %v", result["details"])
	}
}

// TestMergeCompressionUsage_modelName左空取右 验证 model_name 左空时取右
func TestMergeCompressionUsage_modelName左空取右(t *testing.T) {
	left := map[string]any{
		"calls":        1,
		"input_tokens": 100,
		"model_name":   "",
	}
	right := map[string]any{
		"calls":        1,
		"input_tokens": 50,
		"model_name":   "qwen-plus",
	}
	result := MergeCompressionUsage(left, right)
	if result["model_name"] != "qwen-plus" {
		t.Errorf("model_name = %v, want qwen-plus（left 为空时取 right）", result["model_name"])
	}
}

// TestResetCompressionUsage 验证重置用量
func TestResetCompressionUsage(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msg := llm_schema.NewAssistantMessage("hello",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
			InputTokens: 100,
			TotalTokens: 150,
		}),
	)
	p.RecordCompressionUsage(msg)
	if p.CurrentCompressionUsage() == nil {
		t.Fatal("RecordCompressionUsage 后用量不应为 nil")
	}
	p.ResetCompressionUsage()
	if p.CurrentCompressionUsage() != nil {
		t.Error("ResetCompressionUsage 后用量应为 nil")
	}
}

// TestRecordCompressionUsage_记录并获取 验证记录用量后获取快照
func TestRecordCompressionUsage_记录并获取(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msg := llm_schema.NewAssistantMessage("hello",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
			InputTokens: 100,
			TotalTokens: 150,
			ModelName:   "qwen-max",
		}),
	)
	p.RecordCompressionUsage(msg)
	usage := p.CurrentCompressionUsage()
	if usage == nil {
		t.Fatal("CurrentCompressionUsage 不应为 nil")
	}
	if usage["input_tokens"] != 100 {
		t.Errorf("input_tokens = %v, want 100", usage["input_tokens"])
	}
	// 验证返回的是副本
	usage["input_tokens"] = 999
	if p.CurrentCompressionUsage()["input_tokens"] == 999 {
		t.Error("CurrentCompressionUsage 应返回副本，不应影响内部状态")
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run "TestExtractUsageMetadata|TestMergeCompressionUsage|TestResetCompressionUsage|TestRecordCompressionUsage" -v`
Expected: 通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/usage.go internal/agentcore/context_engine/processor/usage_test.go
git commit -m "feat(processor): 添加 CompressionUsage 追踪方法族（ExtractUsageMetadata/MergeCompressionUsage/RecordCompressionUsage）"
```

---

### Task 5: round.go — GroupCompletedAPIRounds 函数

**Files:**
- Create: `internal/agentcore/context_engine/processor/round.go`
- Create: `internal/agentcore/context_engine/processor/round_test.go`

- [ ] **Step 1: 创建 round.go**

```go
package processor

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GroupCompletedAPIRounds 将消息列表按已完成的 API 轮次分组，
// 返回每个轮次的 [start, end) 半开区间列表。
//
// 核心逻辑：
//   - 遇到 AssistantMessage 不含 tool_calls → 一轮完成
//   - 遇到 AssistantMessage 含 tool_calls → 收集 ID，等待 ToolMessage 回复
//   - 所有 pending tool_call_id 收到回复 → 一轮完成
//   - 遇到 UserMessage 且无 pending → 开始新轮次
//
// 对应 Python: openjiuwen/core/context_engine/context/session_memory_manager.py
//
//	(group_completed_api_rounds)
func GroupCompletedAPIRounds(messages []llm_schema.BaseMessage) [][2]int {
	var rounds [][2]int
	currentStart := -1
	var pendingToolCallIDs map[string]bool

	for index, message := range messages {
		if currentStart == -1 {
			currentStart = index
		} else if isUserMessage(message) && len(pendingToolCallIDs) == 0 {
			currentStart = index
		}

		if isAssistantMessage(message) {
			toolCalls := getToolCalls(message)
			if len(toolCalls) > 0 {
				pendingToolCallIDs = make(map[string]bool)
				hasValidID := false
				for _, tc := range toolCalls {
					if tc.ID != "" {
						pendingToolCallIDs[tc.ID] = true
						hasValidID = true
					}
				}
				if !hasValidID {
					// tool_calls 的 ID 全为空，直接视为一轮完成
					rounds = append(rounds, [2]int{currentStart, index + 1})
					currentStart = -1
					pendingToolCallIDs = nil
				}
				continue
			}
			// AssistantMessage 不含 tool_calls → 一轮完成
			rounds = append(rounds, [2]int{currentStart, index + 1})
			currentStart = -1
			pendingToolCallIDs = nil
			continue
		}

		if isToolMessage(message) && len(pendingToolCallIDs) > 0 {
			toolCallID := getToolCallID(message)
			if toolCallID != "" {
				delete(pendingToolCallIDs, toolCallID)
			}
			if len(pendingToolCallIDs) == 0 {
				rounds = append(rounds, [2]int{currentStart, index + 1})
				currentStart = -1
			}
		}
	}

	return rounds
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isUserMessage 判断消息是否为用户消息
func isUserMessage(msg llm_schema.BaseMessage) bool {
	return msg.GetRole() == llm_schema.RoleTypeUser
}

// isAssistantMessage 判断消息是否为助手消息
func isAssistantMessage(msg llm_schema.BaseMessage) bool {
	return msg.GetRole() == llm_schema.RoleTypeAssistant
}

// isToolMessage 判断消息是否为工具消息
func isToolMessage(msg llm_schema.BaseMessage) bool {
	return msg.GetRole() == llm_schema.RoleTypeTool
}

// getToolCalls 从 AssistantMessage 中获取 tool_calls
func getToolCalls(msg llm_schema.BaseMessage) []*llm_schema.ToolCall {
	am, ok := msg.(*llm_schema.AssistantMessage)
	if !ok {
		return nil
	}
	return am.ToolCalls
}

// getToolCallID 从 ToolMessage 中获取 tool_call_id
func getToolCallID(msg llm_schema.BaseMessage) string {
	tm, ok := msg.(*llm_schema.ToolMessage)
	if !ok {
		return ""
	}
	return tm.ToolCallID
}
```

- [ ] **Step 2: 创建 round_test.go**

```go
package processor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGroupCompletedAPIRounds_空消息 验证空消息列表
func TestGroupCompletedAPIRounds_空消息(t *testing.T) {
	result := GroupCompletedAPIRounds(nil)
	if len(result) != 0 {
		t.Errorf("空消息应返回空切片，实际 %d 项", len(result))
	}
}

// TestGroupCompletedAPIRounds_纯对话 验证不含工具调用的对话轮次
func TestGroupCompletedAPIRounds_纯对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 2} {
		t.Errorf("轮次区间 = %v, want [0, 2)", result[0])
	}
}

// TestGroupCompletedAPIRounds_多轮纯对话 验证多轮不含工具调用的对话
func TestGroupCompletedAPIRounds_多轮纯对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("天气怎样"),
		llm_schema.NewAssistantMessage("晴天"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 2 {
		t.Fatalf("轮次数 = %d, want 2", len(result))
	}
	if result[0] != [2]int{0, 2} {
		t.Errorf("轮次1 = %v, want [0, 2)", result[0])
	}
	if result[1] != [2]int{2, 4} {
		t.Errorf("轮次2 = %v, want [2, 4)", result[1])
	}
}

// TestGroupCompletedAPIRounds_含工具调用 验证含工具调用的轮次
func TestGroupCompletedAPIRounds_含工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查一下",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "北京：晴天 25°C"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 3} {
		t.Errorf("轮次区间 = %v, want [0, 3)", result[0])
	}
}

// TestGroupCompletedAPIRounds_多轮含工具 验证多轮含工具调用
func TestGroupCompletedAPIRounds_多轮含工具(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("北京今天晴天"),
		llm_schema.NewUserMessage("上海呢"),
		llm_schema.NewAssistantMessage("我也查一下",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_2", Name: "get_weather", Arguments: `{"city":"上海"}`},
			}),
		),
		llm_schema.NewToolMessage("call_2", "多云"),
		llm_schema.NewAssistantMessage("上海多云"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 3 {
		t.Fatalf("轮次数 = %d, want 3", len(result))
	}
	// 第1轮: [0, 4) — User → Assistant(tool_calls) → Tool → Assistant(无tool_calls)
	if result[0] != [2]int{0, 4} {
		t.Errorf("轮次1 = %v, want [0, 4)", result[0])
	}
	// 第2轮: [4, 5) — User → Assistant(无tool_calls)
	if result[1] != [2]int{4, 5} {
		t.Errorf("轮次2 = %v, want [4, 5)", result[1])
	}
	// 第3轮: [5, 8) — User → Assistant(tool_calls) → Tool → Assistant(无tool_calls)
	if result[2] != [2]int{5, 8} {
		t.Errorf("轮次3 = %v, want [5, 8)", result[2])
	}
}

// TestGroupCompletedAPIRounds_未完成轮次 验证未完成的轮次不计入结果
func TestGroupCompletedAPIRounds_未完成轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		// 缺少 ToolMessage 回复 → 轮次未完成
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 0 {
		t.Errorf("未完成轮次应返回 0 项，实际 %d 项", len(result))
	}
}

// TestGroupCompletedAPIRounds_多个并行工具调用 验证同一轮次中的多个工具调用
func TestGroupCompletedAPIRounds_多个并行工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询北京和上海天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
				{ID: "call_2", Name: "get_weather", Arguments: `{"city":"上海"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "北京：晴天"),
		llm_schema.NewToolMessage("call_2", "上海：多云"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 4} {
		t.Errorf("轮次区间 = %v, want [0, 4)", result[0])
	}
}

// TestGroupCompletedAPIRounds_部分完成 验证部分完成的情况
func TestGroupCompletedAPIRounds_部分完成(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		// 缺少 ToolMessage → 第二轮未完成
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 2} {
		t.Errorf("轮次区间 = %v, want [0, 2)", result[0])
	}
}

// TestIsAPIRound 验证 IsAPIRound 方法
func TestIsAPIRound(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)

	// 完整轮次
	complete := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	if !p.IsAPIRound(complete) {
		t.Error("完整轮次应返回 true")
	}

	// 不完整轮次
	incomplete := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
	}
	if p.IsAPIRound(incomplete) {
		t.Error("未完成轮次应返回 false")
	}

	// 空消息
	if p.IsAPIRound(nil) {
		t.Error("空消息应返回 false")
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run "TestGroupCompletedAPIRounds|TestIsAPIRound" -v`
Expected: 通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/round.go internal/agentcore/context_engine/processor/round_test.go
git commit -m "feat(processor): 添加 GroupCompletedAPIRounds 和 IsAPIRound 实现"
```

---

### Task 6: offload.go — OffloadMessages 方法族

**Files:**
- Create: `internal/agentcore/context_engine/processor/offload.go`
- Create: `internal/agentcore/context_engine/processor/offload_test.go`

- [ ] **Step 1: 创建 offload.go**

```go
package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// offloadMessageHandle 内存卸载消息占位符格式
	offloadMessageHandle = "[[OFFLOAD: handle=%s, type=%s]]"
	// offloadMessageHandleWithPath 文件系统卸载消息占位符格式（含路径）
	offloadMessageHandleWithPath = "[[OFFLOAD: type=%s, path=%s]]"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// OffloadMessages 将消息卸载到文件系统或内存。
//
// 根据选项中的 OffloadType 决定卸载目标：
//   - "in_memory"：卸载到内存（通过 ModelContext.OffloadMessages 存入）
//   - "filesystem"（默认）：卸载到文件系统，失败时 fallback 到内存
//
// 对应 Python: ContextProcessor.offload_messages()
func (p *BaseProcessor) OffloadMessages(ctx context.Context, mc context_engine.ModelContext, role string, content string, messages []llm_schema.BaseMessage, opts ...Option) (llm_schema.BaseMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	po := newProcessorOption(opts...)

	offloadHandle := po.OffloadHandle
	if offloadHandle == "" {
		offloadHandle = uuid.New().Hex()
	}

	offloadType := po.OffloadType
	if offloadType == "" {
		offloadType = "filesystem"
	}

	if mc == nil {
		return nil, nil
	}

	if offloadType == "in_memory" {
		return p.offloadMessagesToMemory(mc, role, content, messages, offloadHandle)
	}

	// filesystem 模式
	sessionID := mc.SessionID()
	offloadPath := po.OffloadPath
	if offloadPath == "" {
		offloadPath = p.GenerateOffloadPath("", sessionID, offloadHandle)
	}

	writeSuccess := p.writeOffloadToFile(sessionID, offloadHandle, offloadPath, messages, po.SysOperation)
	if !writeSuccess {
		// fallback 到内存模式
		return p.offloadMessagesToMemory(mc, role, content, messages, offloadHandle)
	}

	return p.offloadMessagesToFilesystem(role, content, offloadHandle, offloadPath)
}

// GenerateOffloadPath 生成 offload 文件路径。
//
// 目录结构: {workspaceDir}/context/{sessionID}_context/offload/{handle}.json
// 若 workspaceDir 为空，使用 memory/offloads/{sessionID}/{handle}.json。
//
// 对应 Python: ContextProcessor._generate_offload_path()
func (p *BaseProcessor) GenerateOffloadPath(workspaceDir, sessionID, offloadHandle string) string {
	fileName := offloadHandle + ".json"
	if workspaceDir != "" {
		return filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
	}
	return filepath.Join("memory", "offloads", sessionID, fileName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// offloadMessagesToMemory 将消息卸载到内存。
//
// ⤵️ 5.31 回填：需 ModelContext.OffloadMessages(handle, messages) 方法
// 当前实现预留调用点，待 5.31 ModelContext 补充 OffloadMessages 方法后回填。
func (p *BaseProcessor) offloadMessagesToMemory(mc context_engine.ModelContext, role string, content string, messages []llm_schema.BaseMessage, offloadHandle string) (llm_schema.BaseMessage, error) {
	content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "in_memory")

	// ⤵️ 5.31 回填：调用 mc.OffloadMessages(offloadHandle, messages) 存入内存
	// if om, ok := mc.(interface{ OffloadMessages(string, []llm_schema.BaseMessage) }); ok {
	//     om.OffloadMessages(offloadHandle, messages)
	// } else {
	//     return nil, nil
	// }

	return schema.NewOffloadMessage(
		llm_schema.RoleType(role),
		content,
		offloadHandle,
		"in_memory",
	), nil
}

// offloadMessagesToFilesystem 将消息卸载到文件系统。
func (p *BaseProcessor) offloadMessagesToFilesystem(role string, content string, offloadHandle string, offloadPath string) (llm_schema.BaseMessage, error) {
	if offloadPath != "" {
		content = content + fmt.Sprintf(offloadMessageHandleWithPath, "filesystem", offloadPath)
	} else {
		content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "filesystem")
	}

	return schema.NewOffloadMessage(
		llm_schema.RoleType(role),
		content,
		offloadHandle,
		"filesystem",
	), nil
}

// writeOffloadToFile 写入卸载内容到文件系统。
//
// ⤵️ 9.32 回填：优先使用 SysOperation 异步写，移除 os 兜底路径
func (p *BaseProcessor) writeOffloadToFile(sessionID string, offloadHandle string, offloadPath string, messages []llm_schema.BaseMessage, sysOperation any) bool {
	messageData := map[string]any{
		"offload_handle": offloadHandle,
		"messages":       serializeMessages(messages),
	}
	contentJSON, err := json.Marshal(messageData)
	if err != nil {
		return false
	}

	// ⤵️ 9.32 回填：当 sysOperation 不为 nil 时，优先使用 SysOperation 写文件
	_ = sysOperation // 暂时忽略

	// 兜底：使用 os 直接写文件
	if !filepath.IsAbs(offloadPath) {
		return false
	}
	dir := filepath.Dir(offloadPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(offloadPath, contentJSON, 0o644); err != nil {
		return false
	}
	return true
}

// serializeMessages 将消息列表序列化为可 JSON 化的切片
func serializeMessages(messages []llm_schema.BaseMessage) []any {
	result := make([]any, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]any{
			"role":    string(msg.GetRole()),
			"content": msg.GetContent(),
		})
	}
	return result
}
```

- [ ] **Step 2: 创建 offload_test.go**

```go
package processor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestOffloadMessages_空消息列表 验证空消息返回 nil
func TestOffloadMessages_空消息列表(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	result, err := p.OffloadMessages(context.Background(), nil, "assistant", "摘要", nil)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result != nil {
		t.Error("空消息列表应返回 nil")
	}
}

// TestOffloadMessages_nilModelContext 验证 nil ModelContext 返回 nil
func TestOffloadMessages_nilModelContext(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result, err := p.OffloadMessages(context.Background(), nil, "user", "摘要", msgs)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result != nil {
		t.Error("nil ModelContext 应返回 nil")
	}
}

// TestGenerateOffloadPath_有工作目录 验证有工作目录时的路径生成
func TestGenerateOffloadPath_有工作目录(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	path := p.GenerateOffloadPath("/workspace", "session123", "handle456")
	expected := filepath.Join("/workspace", "context", "session123_context", "offload", "handle456.json")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

// TestGenerateOffloadPath_无工作目录 验证无工作目录时的路径生成
func TestGenerateOffloadPath_无工作目录(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	path := p.GenerateOffloadPath("", "session123", "handle456")
	expected := filepath.Join("memory", "offloads", "session123", "handle456.json")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

// TestWriteOffloadToFile_相对路径失败 验证相对路径写入失败
func TestWriteOffloadToFile_相对路径失败(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result := p.writeOffloadToFile("session1", "handle1", "relative/path.json", msgs, nil)
	if result {
		t.Error("相对路径应写入失败")
	}
}

// TestWriteOffloadToFile_绝对路径成功 验证绝对路径写入成功
func TestWriteOffloadToFile_绝对路径成功(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	tmpDir := t.TempDir()
	offloadPath := filepath.Join(tmpDir, "offload", "test.json")
	result := p.writeOffloadToFile("session1", "handle1", offloadPath, msgs, nil)
	if !result {
		t.Fatal("绝对路径应写入成功")
	}
	// 验证文件内容
	data, err := os.ReadFile(offloadPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if len(data) == 0 {
		t.Error("文件内容不应为空")
	}
}

// TestOffloadMessages_in_memory模式 验证 in_memory 模式
func TestOffloadMessages_in_memory模式(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}

	// 注意：当前 ModelContext 为 nil 会返回 nil，
	// 此测试验证选项参数传递正确性
	result, err := p.OffloadMessages(context.Background(), nil, "user", "摘要", msgs,
		WithOffloadType("in_memory"),
		WithOffloadHandle("test-handle"),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	// ModelContext 为 nil → 返回 nil
	if result != nil {
		t.Error("nil ModelContext 应返回 nil")
	}
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -run "TestOffloadMessages|TestGenerateOffloadPath|TestWriteOffloadToFile" -v`
Expected: 通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/offload.go internal/agentcore/context_engine/processor/offload_test.go
git commit -m "feat(processor): 添加 OffloadMessages 方法族和 GenerateOffloadPath"
```

---

### Task 7: 全量编译 + 测试验证

**Files:** 无新增

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 2: processor 包全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -v -cover`
Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: context_engine 全包测试（回归）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -v`
Expected: 所有测试通过（无回归）

---

### Task 8: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/context_engine/processor/doc.go`
- Modify: `internal/agentcore/context_engine/doc.go`

- [ ] **Step 1: 更新 processor/doc.go**

```go
// Package processor 提供上下文处理器插件体系。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages     — 消息即将被添加时
//  2. OnGetContextWindow — 上下文窗口即将返回时
//
// 每个处理器通过 Trigger* 方法判断是否介入，仅在返回 true 时
// 才调用对应的 On* 方法执行实际处理。
//
// 文件目录：
//
//	processor/
//	├── doc.go       # 包文档
//	├── base.go      # ContextProcessor 接口 + ProcessorConfig 接口 + ContextEvent 结构体
//	│               # + BaseProcessor 结构体 + ProcessorOption/Option + 构造函数
//	├── hooks.go     # BaseProcessor 钩子默认实现 + ProcessorType + IsAPIRound
//	├── state.go     # BaseProcessor SaveState/LoadState 默认实现
//	├── offload.go   # OffloadMessages 方法族 + offload 常量 + GenerateOffloadPath
//	├── usage.go     # CompressionUsage 追踪方法族（ExtractUsageMetadata/MergeCompressionUsage 等）
//	└── round.go     # GroupCompletedAPIRounds 包级导出函数
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/
package processor
```

- [ ] **Step 2: 更新 context_engine/doc.go**

在 processor/ 子包条目中追加新文件：

```
//	├── processor/
//	│   ├── doc.go              # Processor 子包文档
//	│   ├── base.go             # ContextProcessor 接口 + ProcessorConfig 接口 + ContextEvent + BaseProcessor
//	│   ├── hooks.go            # 钩子默认实现 + ProcessorType + IsAPIRound
//	│   ├── state.go            # SaveState/LoadState 默认实现
//	│   ├── offload.go          # OffloadMessages 方法族 + offload 常量
//	│   ├── usage.go            # CompressionUsage 追踪方法族
//	│   └── round.go            # GroupCompletedAPIRounds 函数
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/processor/doc.go internal/agentcore/context_engine/doc.go
git commit -m "docs(processor): 更新 doc.go 文件目录，新增 hooks/state/offload/usage/round 文件"
```

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 5.21 状态**

将 5.21 行从 `☐` 改为 `✅`，补充完成说明：

```
| 5.21 | ✅ | ContextProcessor 插件 | ✅ ContextProcessor 接口（7个方法：OnAddMessages/OnGetContextWindow/TriggerAddMessages/TriggerGetContextWindow/SaveState/LoadState/ProcessorType）；✅ ProcessorConfig 接口（Validate）；✅ BaseProcessor 结构体 + 默认实现（no-op 透传、不触发、空状态）；✅ ProcessorOption/Option 模式（替代 Python **kwargs）；✅ OffloadMessages 方法族（in_memory/filesystem/fallback）；✅ CompressionUsage 追踪（ExtractUsageMetadata/MergeCompressionUsage/RecordCompressionUsage）；✅ GroupCompletedAPIRounds 函数 + IsAPIRound；⤵️ 9.32 回填 ProcessorOption.SysOperation 类型 + writeOffloadToFile 改用 SysOperation；⤵️ 5.31 回填 ModelContext.OffloadMessages/WorkspaceDir 方法 | `openjiuwen/core/context_engine/processor/base.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 5.21 状态为已完成"
```
