# AgentCallbackManager + CallbackInfo 统一包装 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现步骤 6.6 AgentCallbackManager（PerAgent 实例级回调管理器），并将 CallbackFramework 所有域统一改造为 CallbackInfo 泛型包装，重命名全局 Agent 域，新增 PerAgent 域。

**Architecture:** AgentCallbackManager 不自持回调存储，将注册/触发委托给全局 CallbackFramework 的 PerAgent 域，通过 `{agentID}_{event}` 前缀实现命名空间隔离。所有域回调用泛型 CallbackInfo[F] 包装以支持优先级排序和元数据。各域 Trigger 逻辑提炼为泛型公共方法 triggerCallbacks。

**Tech Stack:** Go 1.21+（泛型）、sync.RWMutex、sort.SliceStable、Functional Options 模式

**设计文档：** `docs/superpowers/specs/2026-09-28-agent-callback-manager-design.md`

---

## Task 1: 新增 CallbackInfo 泛型结构体 + Functional Options

**Files:**
- Create: `internal/agentcore/runner/callback/callback_info.go`
- Create: `internal/agentcore/runner/callback/options.go`
- Create: `internal/agentcore/runner/callback/callback_info_test.go`
- Create: `internal/agentcore/runner/callback/options_test.go`

- [ ] **Step 1: 创建 callback_info.go**

```go
package callback

import "sort"

// ──────────────────────────── 结构体 ────────────────────────────

// CallbackInfo 回调注册信息，包装回调函数及其元数据。
//
// 对应 Python: CallbackInfo (openjiuwen/core/runner/callback/models.py)
// 回调按 Priority 降序排列（数值越大越先执行），
// 相同 Priority 按 CreatedAt 升序排列（先注册的先执行）。
type CallbackInfo[F any] struct {
	// Callback 回调函数
	Callback F
	// Priority 执行优先级（降序，数值越大越先执行）
	Priority int
	// Once 是否只执行一次（执行后自动禁用）
	Once bool
	// Enabled 是否启用
	Enabled bool
	// Namespace 命名空间（用于分组注销）
	Namespace string
	// Tags 标签集合（用于过滤）
	Tags []string
	// MaxRetries 失败后最大重试次数
	MaxRetries int
	// RetryDelay 重试间隔（秒）
	RetryDelay float64
	// Timeout 执行超时（秒），0 表示不限
	Timeout float64
	// CreatedAt 注册时间戳（秒）
	CreatedAt float64
	// Wrapper 装饰器 wrapper 函数（用于反注册）
	Wrapper F
	// CallbackType 语义类型标记（如 "transform"）
	CallbackType string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortCallbacks 按 Priority 降序排列，相同 Priority 按 CreatedAt 升序排列（先注册的先执行）。
//
// 对应 Python: self._callbacks[event].sort(key=lambda x: x.priority, reverse=True)
func sortCallbacks[F any](callbacks []*CallbackInfo[F]) {
	sort.SliceStable(callbacks, func(i, j int) bool {
		if callbacks[i].Priority != callbacks[j].Priority {
			return callbacks[i].Priority > callbacks[j].Priority // 降序
		}
		return callbacks[i].CreatedAt < callbacks[j].CreatedAt // 升序
	})
}
```

- [ ] **Step 2: 创建 options.go**

```go
package callback

// ──────────────────────────── 结构体 ────────────────────────────

// callbackOptionConfig 回调注册选项内部配置。
type callbackOptionConfig struct {
	Priority     int
	Once         bool
	Namespace    string
	Tags         []string
	MaxRetries   int
	RetryDelay   float64
	Timeout      float64
	CallbackType string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CallbackOption 回调注册选项（Functional Options 模式）。
type CallbackOption func(*callbackOptionConfig)

// WithPriority 设置优先级。
func WithPriority(p int) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Priority = p
	}
}

// WithOnce 设置一次性执行。
func WithOnce() CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Once = true
	}
}

// WithNamespace 设置命名空间。
func WithNamespace(ns string) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Namespace = ns
	}
}

// WithTags 设置标签集合。
func WithTags(tags ...string) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Tags = tags
	}
}

// WithMaxRetries 设置最大重试次数。
func WithMaxRetries(n int) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.MaxRetries = n
	}
}

// WithRetryDelay 设置重试间隔（秒）。
func WithRetryDelay(d float64) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.RetryDelay = d
	}
}

// WithTimeout 设置执行超时（秒），0 表示不限。
func WithTimeout(t float64) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.Timeout = t
	}
}

// WithCallbackType 设置语义类型标记。
func WithCallbackType(t string) CallbackOption {
	return func(cfg *callbackOptionConfig) {
		cfg.CallbackType = t
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyCallbackOptions 应用选项列表，返回配置。
func applyCallbackOptions(opts ...CallbackOption) callbackOptionConfig {
	cfg := callbackOptionConfig{
		Namespace: "default",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
```

- [ ] **Step 3: 创建 callback_info_test.go**

编写 CallbackInfo 排序测试，覆盖：Priority 降序、相同 Priority 按 CreatedAt 升序、空列表。

```go
package callback

import (
	"math"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSortCallbacks_优先级降序 测试按 Priority 降序排列。
func TestSortCallbacks_优先级降序(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 1 }, Priority: 10, CreatedAt: 1},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 2 }, Priority: 30, CreatedAt: 2},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 3 }, Priority: 20, CreatedAt: 3},
	}
	sortCallbacks(callbacks)
	if callbacks[0].Priority != 30 || callbacks[1].Priority != 20 || callbacks[2].Priority != 10 {
		t.Errorf("期望优先级降序 [30,20,10]，实际 [%d,%d,%d]",
			callbacks[0].Priority, callbacks[1].Priority, callbacks[2].Priority)
	}
}

// TestSortCallbacks_相同优先级按创建时间升序 测试相同 Priority 按 CreatedAt 升序。
func TestSortCallbacks_相同优先级按创建时间升序(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 1 }, Priority: 10, CreatedAt: 3.0},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 2 }, Priority: 10, CreatedAt: 1.0},
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 3 }, Priority: 10, CreatedAt: 2.0},
	}
	sortCallbacks(callbacks)
	if callbacks[0].CreatedAt != 1.0 || callbacks[1].CreatedAt != 2.0 || callbacks[2].CreatedAt != 3.0 {
		t.Errorf("期望 CreatedAt 升序 [1,2,3]，实际 [%v,%v,%v]",
			callbacks[0].CreatedAt, callbacks[1].CreatedAt, callbacks[2].CreatedAt)
	}
}

// TestSortCallbacks_空列表 测试空列表排序不 panic。
func TestSortCallbacks_空列表(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{}
	sortCallbacks(callbacks) // 不应 panic
}

// TestSortCallbacks_单元素 测试单元素排序。
func TestSortCallbacks_单元素(t *testing.T) {
	callbacks := []*CallbackInfo[LLMCallbackFunc]{
		{Callback: func(_ context.Context, _ *LLMCallEventData) any { return 1 }, Priority: 10, CreatedAt: 1.0},
	}
	sortCallbacks(callbacks)
	if callbacks[0].Priority != 10 {
		t.Errorf("期望 Priority=10，实际 %d", callbacks[0].Priority)
	}
}

// TestCallbackInfo_字段默认值 测试 CallbackInfo 各字段。
func TestCallbackInfo_字段默认值(t *testing.T) {
	fn := func(_ context.Context, _ *LLMCallEventData) any { return nil }
	info := CallbackInfo[LLMCallbackFunc]{
		Callback: fn,
		Priority: 0,
		Enabled:  true,
	}
	if info.Callback == nil {
		t.Error("Callback 不应为 nil")
	}
	if !info.Enabled {
		t.Error("Enabled 应为 true")
	}
	if info.Once {
		t.Error("Once 默认应为 false")
	}
	// 验证零值字段
	if info.MaxRetries != 0 {
		t.Errorf("MaxRetries 默认应为 0，实际 %d", info.MaxRetries)
	}
	if info.Timeout != 0 {
		t.Errorf("Timeout 默认应为 0，实际 %f", info.Timeout)
	}
	if !math.IsNaN(info.RetryDelay) || info.RetryDelay != 0 {
		// RetryDelay 零值应为 0
		if info.RetryDelay != 0 {
			t.Errorf("RetryDelay 默认应为 0，实际 %f", info.RetryDelay)
		}
	}
}
```

注意：需在 callback_info_test.go 的 import 中加 `"context"`。

- [ ] **Step 4: 创建 options_test.go**

```go
package callback

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestApplyCallbackOptions_默认值 测试无选项时的默认值。
func TestApplyCallbackOptions_默认值(t *testing.T) {
	cfg := applyCallbackOptions()
	if cfg.Priority != 0 {
		t.Errorf("Priority 默认应为 0，实际 %d", cfg.Priority)
	}
	if cfg.Namespace != "default" {
		t.Errorf("Namespace 默认应为 'default'，实际 %q", cfg.Namespace)
	}
	if cfg.Once {
		t.Error("Once 默认应为 false")
	}
}

// TestWithPriority 测试 WithPriority 选项。
func TestWithPriority(t *testing.T) {
	cfg := applyCallbackOptions(WithPriority(100))
	if cfg.Priority != 100 {
		t.Errorf("期望 Priority=100，实际 %d", cfg.Priority)
	}
}

// TestWithOnce 测试 WithOnce 选项。
func TestWithOnce(t *testing.T) {
	cfg := applyCallbackOptions(WithOnce())
	if !cfg.Once {
		t.Error("期望 Once=true")
	}
}

// TestWithNamespace 测试 WithNamespace 选项。
func TestWithNamespace(t *testing.T) {
	cfg := applyCallbackOptions(WithNamespace("my_ns"))
	if cfg.Namespace != "my_ns" {
		t.Errorf("期望 Namespace='my_ns'，实际 %q", cfg.Namespace)
	}
}

// TestWithTags 测试 WithTags 选项。
func TestWithTags(t *testing.T) {
	cfg := applyCallbackOptions(WithTags("tag1", "tag2"))
	if len(cfg.Tags) != 2 || cfg.Tags[0] != "tag1" || cfg.Tags[1] != "tag2" {
		t.Errorf("期望 Tags=[tag1,tag2]，实际 %v", cfg.Tags)
	}
}

// TestWithMaxRetries 测试 WithMaxRetries 选项。
func TestWithMaxRetries(t *testing.T) {
	cfg := applyCallbackOptions(WithMaxRetries(3))
	if cfg.MaxRetries != 3 {
		t.Errorf("期望 MaxRetries=3，实际 %d", cfg.MaxRetries)
	}
}

// TestWithRetryDelay 测试 WithRetryDelay 选项。
func TestWithRetryDelay(t *testing.T) {
	cfg := applyCallbackOptions(WithRetryDelay(0.5))
	if cfg.RetryDelay != 0.5 {
		t.Errorf("期望 RetryDelay=0.5，实际 %f", cfg.RetryDelay)
	}
}

// TestWithTimeout 测试 WithTimeout 选项。
func TestWithTimeout(t *testing.T) {
	cfg := applyCallbackOptions(WithTimeout(10.0))
	if cfg.Timeout != 10.0 {
		t.Errorf("期望 Timeout=10.0，实际 %f", cfg.Timeout)
	}
}

// TestWithCallbackType 测试 WithCallbackType 选项。
func TestWithCallbackType(t *testing.T) {
	cfg := applyCallbackOptions(WithCallbackType("transform"))
	if cfg.CallbackType != "transform" {
		t.Errorf("期望 CallbackType='transform'，实际 %q", cfg.CallbackType)
	}
}

// TestApplyCallbackOptions_多选项组合 测试多个选项组合。
func TestApplyCallbackOptions_多选项组合(t *testing.T) {
	cfg := applyCallbackOptions(
		WithPriority(50),
		WithOnce(),
		WithNamespace("test_ns"),
		WithMaxRetries(2),
	)
	if cfg.Priority != 50 || !cfg.Once || cfg.Namespace != "test_ns" || cfg.MaxRetries != 2 {
		t.Errorf("多选项组合不正确: %+v", cfg)
	}
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/callback/... -run "TestSortCallbacks|TestCallbackInfo_字段默认值|TestApplyCallbackOptions|TestWith" -v -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/runner/callback/callback_info.go internal/agentcore/runner/callback/options.go internal/agentcore/runner/callback/callback_info_test.go internal/agentcore/runner/callback/options_test.go
git commit -m "feat(callback): 新增 CallbackInfo 泛型包装和 Functional Options"
```

---

## Task 2: 重命名全局 Agent 域类型/常量/方法

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`
- Modify: `internal/agentcore/runner/callback/framework.go`
- Modify: `internal/agentcore/runner/callback/events_test.go`
- Modify: `internal/agentcore/runner/callback/framework_test.go`
- Modify: `internal/agentcore/runner/callback/logging.go`
- Modify: `internal/agentcore/runner/callback/logging_test.go`
- Modify: `internal/agentcore/single_agent/base.go`（TriggerAgent → TriggerGlobalAgent 调用点）
- Modify: `internal/agentcore/single_agent/base_test.go`（OnAgent → OnGlobalAgent 等）

- [ ] **Step 1: 重命名 events.go 中的类型和常量**

使用 `Edit` 工具执行以下替换（`replace_all: true`）：

| 旧 | 新 |
|----|-----|
| `AgentCallGlobalEventType` | `GlobalAgentEventType` |
| `AgentCallbackFunc` | `GlobalAgentCallbackFunc` |
| `AgentCallEventData` | `GlobalAgentEventData` |
| `AgentStarted` | `GlobalAgentStarted` |
| `AgentInvokeInput` | `GlobalAgentInvokeInput` |
| `AgentInvokeOutput` | `GlobalAgentInvokeOutput` |
| `AgentStreamInput` | `GlobalAgentStreamInput` |
| `AgentStreamOutput` | `GlobalAgentStreamOutput` |

注意：`TransformAgentIOInputFunc`/`TransformAgentIOOutputFunc` 中的 `AgentCallGlobalEventType` 参数类型也要改，但函数名不改。

- [ ] **Step 2: 在 events.go 末尾新增 PerAgentCallbackFunc 类型**

在 `AgentCallbackFunc` 重命名为 `GlobalAgentCallbackFunc` 之后，在 events.go 枚举区块后新增：

```go
// PerAgentCallbackFunc 实例级 PerAgent 回调函数类型。
// agentCallbackContext 实际类型为 *rail.AgentCallbackContext，回调内需类型断言。
//
// 对应 Python: AnyAgentCallback = Union[AgentCallback, SyncAgentCallback]
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error
```

- [ ] **Step 3: 重命名 framework.go 中的字段和方法**

| 旧 | 新 |
|----|-----|
| `agentCallbacks` | `globalAgentCallbacks` |
| `OnAgent` | `OnGlobalAgent` |
| `OffAgent` | `OffGlobalAgent` |
| `TriggerAgent` | `TriggerGlobalAgent` |

同时将 `agentTransformIO` 的键类型从 `AgentCallGlobalEventType` 改为 `GlobalAgentEventType`（自动跟随重命名）。

- [ ] **Step 4: 适配所有外部引用**

搜索项目中所有使用旧名称的代码，逐一替换：

- `single_agent/base.go`：`TriggerAgent` → `TriggerGlobalAgent`、`OnAgent` → `OnGlobalAgent`、`AgentCallGlobalEventType` → `GlobalAgentEventType`、`AgentCallbackFunc` → `GlobalAgentCallbackFunc`、`AgentCallEventData` → `GlobalAgentEventData`
- `single_agent/base_test.go`：同上
- `callback/events_test.go`：同上
- `callback/framework_test.go`：同上
- `callback/logging.go`：如引用旧名则替换
- `callback/logging_test.go`：如引用旧名则替换
- 所有其他引用 `AgentStarted`/`AgentInvokeInput` 等常量的代码

- [ ] **Step 5: 运行编译确认**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/...`

Expected: 编译通过，无错误

- [ ] **Step 6: 运行受影响的测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/callback/... ./internal/agentcore/single_agent/... -v -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(callback): 重命名全局Agent域 GlobalAgentEventType/GlobalAgentCallbackFunc/GlobalAgentEventData，新增 PerAgentCallbackFunc"
```

---

## Task 3: CallbackFramework 所有域改造为 CallbackInfo 包装

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go`
- Modify: `internal/agentcore/runner/callback/framework_test.go`
- Modify: `internal/agentcore/runner/callback/logging.go`
- Modify: `internal/agentcore/runner/callback/logging_test.go`

- [ ] **Step 1: 改造 CallbackFramework 结构体**

将所有 map 的 value 类型从 `[]XXXFunc` 改为 `[]*CallbackInfo[XXXFunc]`：

```go
type CallbackFramework struct {
	mu                  sync.RWMutex
	llmCallbacks        map[LLMCallEventType][]*CallbackInfo[LLMCallbackFunc]
	toolCallbacks       map[ToolCallEventType][]*CallbackInfo[ToolCallbackFunc]
	sessionCallbacks    map[SessionCallEventType][]*CallbackInfo[SessionCallbackFunc]
	customCallbacks     map[string][]*CallbackInfo[CustomCallbackFunc]
	contextCallbacks    map[ContextCallEventType][]*CallbackInfo[ContextCallbackFunc]
	globalAgentCallbacks map[GlobalAgentEventType][]*CallbackInfo[GlobalAgentCallbackFunc]
	perAgentCallbacks   map[string][]*CallbackInfo[PerAgentCallbackFunc]
	llmTransformIO      map[LLMCallEventType]*llmTransformIOEntry
	agentTransformIO    map[GlobalAgentEventType]*agentTransformIOEntry
	toolTransformIO     map[ToolCallEventType]*toolTransformIOEntry
}
```

- [ ] **Step 2: 新增 triggerStrategy 枚举和 triggerCallbacks 泛型公共方法**

在 framework.go 非导出函数区块新增：

```go
// triggerStrategy 回调执行策略。
type triggerStrategy int

const (
	// strategyCollect 收集所有返回值，不中断（观测型）
	strategyCollect triggerStrategy = iota
	// strategyAbortOnError 遇 error 中断（控制型）
	strategyAbortOnError
)

// triggerCallbacks 泛型触发核心逻辑（CallbackFramework 非导出方法）。
func (fw *CallbackFramework) triggerCallbacks[F any, E comparable, D any](
	callbacksMap map[E][]*CallbackInfo[F],
	event E,
	data D,
	ctx context.Context,
	strategy triggerStrategy,
	execute func(F, context.Context, D) (any, error),
) ([]any, error) {
	if ctx == nil {
		return nil, nil
	}

	fw.mu.RLock()
	callbacks := callbacksMap[event]
	fw.mu.RUnlock()

	// ⤵️ 回填：BEFORE 钩子执行（对应 Python: _execute_hooks(event, HookType.BEFORE)）

	var results []any
	for _, info := range callbacks {
		if !info.Enabled {
			continue
		}
		if info.CallbackType == "transform" {
			continue
		}

		// ⤵️ 回填：过滤器检查（对应 Python: _apply_filters）
		// ⤵️ 回填：熔断器检查（对应 Python: _circuit_breakers）
		// ⤵️ 回填：回调级超时控制（对应 Python: trigger_with_timeout）
		// ⤵️ 回填：回调级重试（对应 Python: max_retries/retry_delay）

		result, err := execute(info.Callback, ctx, data)

		if err != nil {
			// ⤵️ 回填：ERROR 钩子执行（对应 Python: _execute_hooks(event, HookType.ERROR)）
			// ⤵️ 回填：指标记录（is_error=True）
			if strategy == strategyAbortOnError {
				return nil, err
			}
			continue
		}

		// ⤵️ 回填：指标记录（is_error=False）
		results = append(results, result)

		if info.Once {
			info.Enabled = false
		}
	}

	// ⤵️ 回填：AFTER 钩子执行（对应 Python: _execute_hooks(event, HookType.AFTER)）

	return results, nil
}
```

- [ ] **Step 3: 改造所有 On* 方法**

每个 On* 方法签名增加 `opts ...CallbackOption`，内部构造 CallbackInfo 并排序。以 OnLLM 为例：

```go
func (fw *CallbackFramework) OnLLM(event LLMCallEventType, fn LLMCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[LLMCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.llmCallbacks[event] = append(fw.llmCallbacks[event], info)
	sortCallbacks(fw.llmCallbacks[event])
}
```

同样改造 OnTool、OnSession、OnCustom、OnContext、OnGlobalAgent。需在 framework.go 的 import 中加 `"time"`。

- [ ] **Step 4: 改造所有 Off* 方法**

Off* 方法改为比较 `info.Callback` 的指针：

```go
func (fw *CallbackFramework) OffLLM(event LLMCallEventType, fn LLMCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	callbacks, ok := fw.llmCallbacks[event]
	if !ok {
		return
	}
	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.llmCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}
```

同样改造 OffTool、OffSession、OffCustom、OffContext、OffGlobalAgent。

- [ ] **Step 5: 改造所有 Trigger* 方法，使用 triggerCallbacks 公共方法**

```go
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) []any {
	if data == nil {
		return nil
	}
	results, _ := fw.triggerCallbacks(fw.llmCallbacks, data.Event, data, ctx,
		strategyCollect,
		func(fn LLMCallbackFunc, ctx context.Context, data *LLMCallEventData) (any, error) {
			return fn(ctx, data), nil
		},
	)
	return results
}
```

同样改造 TriggerTool、TriggerSession、TriggerCustom、TriggerContext、TriggerGlobalAgent。

- [ ] **Step 6: 新增 PerAgent 域方法**

```go
func (fw *CallbackFramework) OnPerAgent(event string, fn PerAgentCallbackFunc, opts ...CallbackOption) {
	cfg := applyCallbackOptions(opts...)
	info := &CallbackInfo[PerAgentCallbackFunc]{
		Callback:     fn,
		Priority:     cfg.Priority,
		Once:         cfg.Once,
		Enabled:      true,
		Namespace:    cfg.Namespace,
		Tags:         cfg.Tags,
		MaxRetries:   cfg.MaxRetries,
		RetryDelay:   cfg.RetryDelay,
		Timeout:      cfg.Timeout,
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
		CallbackType: cfg.CallbackType,
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.perAgentCallbacks[event] = append(fw.perAgentCallbacks[event], info)
	sortCallbacks(fw.perAgentCallbacks[event])
}

func (fw *CallbackFramework) OffPerAgent(event string, fn PerAgentCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	callbacks, ok := fw.perAgentCallbacks[event]
	if !ok {
		return
	}
	for i, info := range callbacks {
		if fmt.Sprintf("%p", info.Callback) == fmt.Sprintf("%p", fn) {
			fw.perAgentCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

func (fw *CallbackFramework) OffAllPerAgent(event string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	delete(fw.perAgentCallbacks, event)
}

func (fw *CallbackFramework) TriggerPerAgent(ctx context.Context, event string, agentCallbackContext any) error {
	_, err := fw.triggerCallbacks(fw.perAgentCallbacks, event, agentCallbackContext, ctx,
		strategyAbortOnError,
		func(fn PerAgentCallbackFunc, ctx context.Context, data any) (any, error) {
			return nil, fn(ctx, data)
		},
	)
	return err
}

func (fw *CallbackFramework) HasPerAgentHooks(event string) bool {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return len(fw.perAgentCallbacks[event]) > 0
}
```

- [ ] **Step 7: 改造 NewCallbackFramework 初始化**

```go
func NewCallbackFramework() *CallbackFramework {
	fw := &CallbackFramework{
		llmCallbacks:        make(map[LLMCallEventType][]*CallbackInfo[LLMCallbackFunc]),
		toolCallbacks:       make(map[ToolCallEventType][]*CallbackInfo[ToolCallbackFunc]),
		sessionCallbacks:    make(map[SessionCallEventType][]*CallbackInfo[SessionCallbackFunc]),
		customCallbacks:     make(map[string][]*CallbackInfo[CustomCallbackFunc]),
		contextCallbacks:    make(map[ContextCallEventType][]*CallbackInfo[ContextCallbackFunc]),
		globalAgentCallbacks: make(map[GlobalAgentEventType][]*CallbackInfo[GlobalAgentCallbackFunc]),
		perAgentCallbacks:   make(map[string][]*CallbackInfo[PerAgentCallbackFunc]),
		llmTransformIO:      make(map[LLMCallEventType]*llmTransformIOEntry),
		agentTransformIO:    make(map[GlobalAgentEventType]*agentTransformIOEntry),
		toolTransformIO:     make(map[ToolCallEventType]*toolTransformIOEntry),
	}
	// 默认注册 LLM 日志回调
	fw.OnLLM(LLMCallStarted, LoggingLLMCallback)
	// ... 其余 8 个日志回调不变
	return fw
}
```

- [ ] **Step 8: 改造 GetCallbacksForTest**

```go
func (fw *CallbackFramework) GetCallbacksForTest(event LLMCallEventType) []*CallbackInfo[LLMCallbackFunc] {
	fw.mu.RLock()
	defer fw.mu.RUnlock()
	return fw.llmCallbacks[event]
}
```

- [ ] **Step 9: 适配 logging.go 中 OnLLM 调用**

logging.go 中如直接调用 `fw.OnLLM`，需确认调用签名兼容（新增 opts 为可变参数，不影响无 opts 调用）。

- [ ] **Step 10: 适配 framework_test.go**

所有测试中 `OnLLM(event, fn)` 调用不变（opts 可选），但 `GetCallbacksForTest` 返回类型变了。需要适配测试中遍历回调列表的代码。

- [ ] **Step 11: 新增 PerAgent 域测试**

在 framework_test.go 中新增：

- `TestOnPerAgent_TriggerPerAgent`：注册+触发+验证
- `TestOffPerAgent`：注销+验证
- `TestOffAllPerAgent`：清除+验证
- `TestHasPerAgentHooks`：检查+验证
- `TestTriggerPerAgent_优先级排序`：验证高优先级先执行
- `TestTriggerPerAgent_错误中断`：验证 error 中断后续回调
- `TestTriggerPerAgent_Once回调`：验证一次性执行

- [ ] **Step 12: 运行编译和测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/... && go test ./internal/agentcore/runner/callback/... -v -count=1`

Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add -A
git commit -m "feat(callback): 所有域统一CallbackInfo包装+泛型triggerCallbacks+PerAgent域"
```

---

## Task 4: 新增 AgentCallbackManager

**Files:**
- Create: `internal/agentcore/single_agent/rail/manager.go`
- Create: `internal/agentcore/single_agent/rail/manager_test.go`

- [ ] **Step 1: 创建 manager.go**

```go
package rail

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCallbackManager PerAgent 实例级回调管理器。
//
// 对应 Python: AgentCallbackManager (openjiuwen/core/single_agent/agent_callback_manager.py)
// 不自持回调存储，将注册/触发委托给全局 CallbackFramework，
// 通过 "{agentID}_{event}" 前缀实现命名空间隔离。
type AgentCallbackManager struct {
	// agentID Agent 唯一标识，用于构造事件名前缀
	agentID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentCallbackManager 创建回调管理器。
func NewAgentCallbackManager(agentID string) *AgentCallbackManager {
	return &AgentCallbackManager{agentID: agentID}
}

// RegisterCallback 注册回调。
//
// 对应 Python: AgentCallbackManager.register_callback(event, callback, priority)
// 委托给 CallbackFramework.OnPerAgent(agentEvent, fn, opts...)
func (m *AgentCallbackManager) RegisterCallback(ctx context.Context, event AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) {
	agentEvent := m.getAgentEvent(event)
	cb.GetCallbackFramework().OnPerAgent(agentEvent, fn, opts...)
}

// RegisterRail 批量注册一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.register_rail(rail)
// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail，遍历 rail.getCallbacks() 注册
func (m *AgentCallbackManager) RegisterRail(_ context.Context, _ any, _ ...cb.CallbackOption) error {
	// ⤵️ 6.7 回填：实现 Rail 批量注册
	return nil
}

// UnregisterRail 批量注销一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.unregister_rail(rail)
// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
func (m *AgentCallbackManager) UnregisterRail(_ context.Context, _ any) error {
	// ⤵️ 6.7 回填：实现 Rail 批量注销
	return nil
}

// Unregister 注销指定事件上的单个回调。
//
// 对应 Python: AgentCallbackManager.unregister(event, callback)
func (m *AgentCallbackManager) Unregister(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) {
	agentEvent := m.getAgentEvent(event)
	cb.GetCallbackFramework().OffPerAgent(agentEvent, fn)
}

// Clear 清除回调。不传 event 时清除所有事件的回调。
//
// 对应 Python: AgentCallbackManager.clear(event)
func (m *AgentCallbackManager) Clear(events ...AgentCallbackEvent) {
	fw := cb.GetCallbackFramework()
	if len(events) == 0 {
		for _, e := range AllCallbackEvents() {
			fw.OffAllPerAgent(m.getAgentEvent(e))
		}
		return
	}
	for _, e := range events {
		fw.OffAllPerAgent(m.getAgentEvent(e))
	}
}

// HasHooks 检查指定事件是否有已注册的回调。
//
// 对应 Python: AgentCallbackManager.has_hooks(event)
func (m *AgentCallbackManager) HasHooks(event AgentCallbackEvent) bool {
	agentEvent := m.getAgentEvent(event)
	return cb.GetCallbackFramework().HasPerAgentHooks(agentEvent)
}

// Execute 触发指定事件的所有回调。
//
// 对应 Python: AgentCallbackManager.execute(event, ctx)
// 委托给 CallbackFramework.TriggerPerAgent(ctx, agentEvent, railCtx)
func (m *AgentCallbackManager) Execute(ctx context.Context, event AgentCallbackEvent, railCtx *AgentCallbackContext) error {
	agentEvent := m.getAgentEvent(event)
	return cb.GetCallbackFramework().TriggerPerAgent(ctx, agentEvent, railCtx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getAgentEvent 生成带 agentID 前缀的事件名。
//
// 对应 Python: AgentCallbackManager._get_agent_event(event)
// 返回格式: "{agentID}_{event}"，如 "agent1_before_model_call"
func (m *AgentCallbackManager) getAgentEvent(event AgentCallbackEvent) string {
	return m.agentID + "_" + string(event)
}
```

- [ ] **Step 2: 创建 manager_test.go**

```go
package rail

import (
	"context"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewAgentCallbackManager 测试创建回调管理器。
func TestNewAgentCallbackManager(t *testing.T) {
	m := NewAgentCallbackManager("agent1")
	if m.agentID != "agent1" {
		t.Errorf("期望 agentID='agent1'，实际 %q", m.agentID)
	}
}

// TestGetAgentEvent 测试事件名前缀生成。
func TestGetAgentEvent(t *testing.T) {
	m := NewAgentCallbackManager("agent1")
	got := m.getAgentEvent(CallbackBeforeInvoke)
	want := "agent1_before_invoke"
	if got != want {
		t.Errorf("期望 %q，实际 %q", want, got)
	}
}

// TestRegisterCallback_And_Execute 测试注册回调并触发。
func TestRegisterCallback_And_Execute(t *testing.T) {
	m := NewAgentCallbackManager("test_agent")
	executed := false
	fn := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		executed = true
		return nil
	})

	ctx := context.Background()
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn)

	// 构造一个最小 AgentCallbackContext（仅用于触发，不需要完整初始化）
	railCtx := &AgentCallbackContext{}

	err := m.Execute(ctx, CallbackBeforeInvoke, railCtx)
	if err != nil {
		t.Errorf("Execute 返回错误: %v", err)
	}
	if !executed {
		t.Error("回调未执行")
	}

	// 清理
	m.Clear(CallbackBeforeInvoke)
}

// TestHasHooks 测试检查回调是否存在。
func TestHasHooks(t *testing.T) {
	m := NewAgentCallbackManager("test_agent2")
	if m.HasHooks(CallbackAfterInvoke) {
		t.Error("未注册时不应有回调")
	}

	fn := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error { return nil })
	m.RegisterCallback(context.Background(), CallbackAfterInvoke, fn)

	if !m.HasHooks(CallbackAfterInvoke) {
		t.Error("注册后应有回调")
	}

	m.Clear(CallbackAfterInvoke)
}

// TestUnregister 测试注销回调。
func TestUnregister(t *testing.T) {
	m := NewAgentCallbackManager("test_agent3")
	executed := false
	fn := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		executed = true
		return nil
	})

	ctx := context.Background()
	m.RegisterCallback(ctx, CallbackBeforeModelCall, fn)
	m.Unregister(CallbackBeforeModelCall, fn)

	railCtx := &AgentCallbackContext{}
	err := m.Execute(ctx, CallbackBeforeModelCall, railCtx)
	if err != nil {
		t.Errorf("Execute 返回错误: %v", err)
	}
	if executed {
		t.Error("注销后回调不应执行")
	}
}

// TestClear_指定事件 测试清除指定事件的回调。
func TestClear_指定事件(t *testing.T) {
	m := NewAgentCallbackManager("test_agent4")
	fn1 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error { return nil })
	fn2 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error { return nil })

	ctx := context.Background()
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn1)
	m.RegisterCallback(ctx, CallbackAfterInvoke, fn2)

	m.Clear(CallbackBeforeInvoke)

	if m.HasHooks(CallbackBeforeInvoke) {
		t.Error("清除后 BeforeInvoke 不应有回调")
	}
	if !m.HasHooks(CallbackAfterInvoke) {
		t.Error("AfterInvoke 不应被清除")
	}

	m.Clear()
}

// TestClear_全部事件 测试清除所有事件的回调。
func TestClear_全部事件(t *testing.T) {
	m := NewAgentCallbackManager("test_agent5")
	fn := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error { return nil })

	ctx := context.Background()
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn)
	m.RegisterCallback(ctx, CallbackAfterInvoke, fn)

	m.Clear()

	if m.HasHooks(CallbackBeforeInvoke) || m.HasHooks(CallbackAfterInvoke) {
		t.Error("全部清除后不应有回调")
	}
}

// TestRegisterCallback_优先级 测试优先级排序。
func TestRegisterCallback_优先级(t *testing.T) {
	m := NewAgentCallbackManager("test_agent6")
	var order []int
	fn1 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		order = append(order, 1)
		return nil
	})
	fn2 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		order = append(order, 2)
		return nil
	})
	fn3 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		order = append(order, 3)
		return nil
	})

	ctx := context.Background()
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn1, cb.WithPriority(10)) // 低
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn2, cb.WithPriority(30)) // 高
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn3, cb.WithPriority(20)) // 中

	railCtx := &AgentCallbackContext{}
	err := m.Execute(ctx, CallbackBeforeInvoke, railCtx)
	if err != nil {
		t.Errorf("Execute 返回错误: %v", err)
	}

	want := []int{2, 3, 1} // 30→20→10
	if len(order) != 3 || order[0] != 2 || order[1] != 3 || order[2] != 1 {
		t.Errorf("期望执行顺序 %v，实际 %v", want, order)
	}

	m.Clear(CallbackBeforeInvoke)
}

// TestExecute_错误中断 测试回调返回 error 时中断。
func TestExecute_错误中断(t *testing.T) {
	m := NewAgentCallbackManager("test_agent7")
	var order []int
	fn1 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		order = append(order, 1)
		return nil
	})
	fnErr := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		order = append(order, 2)
		return context.DeadlineExceeded
	})
	fn3 := cb.PerAgentCallbackFunc(func(_ context.Context, _ any) error {
		order = append(order, 3)
		return nil
	})

	ctx := context.Background()
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn1, cb.WithPriority(30))
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fnErr, cb.WithPriority(20))
	m.RegisterCallback(ctx, CallbackBeforeInvoke, fn3, cb.WithPriority(10))

	railCtx := &AgentCallbackContext{}
	err := m.Execute(ctx, CallbackBeforeInvoke, railCtx)
	if err == nil {
		t.Error("期望返回错误")
	}

	want := []int{1, 2} // fn3 不应执行
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("期望执行顺序 %v，实际 %v", want, order)
	}

	m.Clear(CallbackBeforeInvoke)
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/... -run "TestNewAgentCallbackManager|TestGetAgentEvent|TestRegisterCallback|TestHasHooks|TestUnregister|TestClear|TestExecute" -v -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/single_agent/rail/manager.go internal/agentcore/single_agent/rail/manager_test.go
git commit -m "feat(rail): 新增 AgentCallbackManager 回调管理器"
```

---

## Task 5: 回填 Fire/FireLifecycle + BaseAgent 接口 + WarpBaseAgent

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go`
- Modify: `internal/agentcore/single_agent/interfaces/interface.go`
- Modify: `internal/agentcore/single_agent/base.go`
- Modify: `internal/agentcore/single_agent/rail/context_test.go`
- Modify: `internal/agentcore/single_agent/base_test.go`

- [ ] **Step 1: 回填 Fire() 方法**

在 `rail/context.go` 中替换 Fire 占位：

```go
// Fire 触发回调事件。
//
// 对应 Python: AgentCallbackContext.fire(event)
func (c *AgentCallbackContext) Fire(event AgentCallbackEvent) error {
	c.event = event
	manager, ok := c.agent.CallbackManager().(*AgentCallbackManager)
	if !ok {
		return nil
	}
	return manager.Execute(c.context(), event, c)
}
```

注意：`c.context()` 需要获取 context.Context，如果 AgentCallbackContext 没有 context 字段，则用 `context.Background()`。需检查 context.go 中是否有 context 字段或方法。

- [ ] **Step 2: 回填 FireLifecycle() 中两处 Fire 调用**

替换 `_ = before` 占位为 `if err := c.Fire(before); err != nil { return err }`
替换 `_ = after` 占位为 `_ = c.Fire(after)`（异常安全，忽略 after 错误）

- [ ] **Step 3: 回填 BaseAgent 接口**

在 `interfaces/interface.go` 中修改：

- `CallbackManager() any` 保持 any（避免循环依赖），更新注释
- `RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error`
- `RegisterRail(ctx context.Context, rail any, opts ...callback.CallbackOption) error`
- `UnregisterRail(ctx context.Context, rail any) error`

需要在 interface.go 的 import 中加 `cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"`。

- [ ] **Step 4: 回填 WarpBaseAgent**

在 `base.go` 中：

1. `callbackManager any` → `callbackManager *rail.AgentCallbackManager`
2. `CallbackManager() any` 返回 `w.callbackManager`
3. `RegisterCallback` 委托实现
4. `RegisterRail` 委托实现
5. `UnregisterRail` 委托实现
6. 构造时初始化 `w.callbackManager = rail.NewAgentCallbackManager(w.card.ID)`

- [ ] **Step 5: 适配 context_test.go**

Fire/FireLifecycle 的测试需要适配：
- 之前 Fire 是 panic 占位，测试可能跳过或用 recover
- 现在需要构造完整的 AgentCallbackContext（含 agent.CallbackManager() 返回有效的 Manager）

- [ ] **Step 6: 适配 base_test.go**

- `OnAgent` → `OnGlobalAgent`
- `TriggerAgent` → `TriggerGlobalAgent`
- 确认 `CallbackManager()` 返回值测试适配

- [ ] **Step 7: 运行编译和测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/... && go test ./internal/agentcore/single_agent/rail/... ./internal/agentcore/single_agent/... -v -count=1`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(rail): 回填 Fire/FireLifecycle + BaseAgent接口 + WarpBaseAgent委托实现"
```

---

## Task 6: 更新 doc.go 和全量测试

**Files:**
- Modify: `internal/agentcore/runner/callback/doc.go`
- Modify: `internal/agentcore/single_agent/rail/doc.go`

- [ ] **Step 1: 更新 callback/doc.go**

新增 CallbackInfo、Functional Options、PerAgent 域、triggerCallbacks 公共方法说明。更新文件目录树。

- [ ] **Step 2: 更新 rail/doc.go**

新增 manager.go（AgentCallbackManager）条目。更新文件目录树。

- [ ] **Step 3: 运行全量编译和测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/... && go test ./internal/agentcore/... -v -count=1`

Expected: PASS

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md 中 6.6 状态**

将 6.6 步骤状态从 `☐` 改为 `✅`，填写产出。

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md 6.6 状态"
```
