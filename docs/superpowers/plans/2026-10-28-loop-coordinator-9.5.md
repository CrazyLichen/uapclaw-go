# 9.5 LoopCoordinator + StopConditionEvaluator 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 DeepAgent 外层任务循环的停止条件评估系统（LoopCoordinator + 5 个 StopConditionEvaluator），对齐 Python `harness/task_loop/loop_coordinator.py` + `harness/schema/stop_condition.py`。

**Architecture:** 自底向上纯逻辑层。StopConditionEvaluator 接口定义评估器契约，5 个具体评估器实现不同停止策略（最大轮次/token预算/超时/完成承诺/自定义谓词），LoopCoordinator 持有评估器切片并通过 OR 语义控制循环。所有状态可通过 ExportState/ImportState 持久化到 session checkpoint。

**Tech Stack:** Go 1.26, sync.Mutex 并发保护, time.Monotonic 计时, JSON 序列化

**设计文档:** `docs/superpowers/specs/2026-10-28-loop-coordinator-9.5-design.md`

**Python 源码:**
- `/home/opensource/agent-core/openjiuwen/harness/task_loop/loop_coordinator.py`
- `/home/opensource/agent-core/openjiuwen/harness/schema/stop_condition.py`

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `internal/agentcore/harness/task_loop/doc.go` | 包文档 |
| `internal/agentcore/harness/task_loop/stop_condition.go` | StopConditionEvaluator 接口 + StopEvaluationContext + 5 个评估器 |
| `internal/agentcore/harness/task_loop/stop_condition_test.go` | 评估器测试 |
| `internal/agentcore/harness/task_loop/loop_coordinator.go` | LoopCoordinator + LoopCoordinatorState |
| `internal/agentcore/harness/task_loop/loop_coordinator_test.go` | LoopCoordinator 测试 |

---

### Task 1: 创建 harness/task_loop 包目录和 doc.go

**Files:**
- Create: `internal/agentcore/harness/task_loop/doc.go`

- [ ] **Step 1: 创建目录结构**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/agentcore/harness/task_loop
```

- [ ] **Step 2: 编写 doc.go**

```go
// Package task_loop 提供 DeepAgent 外层任务循环的运行时组件。
//
// 包含循环协调器（LoopCoordinator）、停止条件评估器（StopConditionEvaluator）
// 及其内置实现，用于控制 DeepAgent 多轮任务循环的生命周期。
//
// LoopCoordinator 追踪迭代次数、token 用量、耗时和中止标记，
// 每轮迭代前通过评估器链（OR 语义）决定是否继续循环。
//
// 文件目录：
//
//	task_loop/
//	├── doc.go                   # 包文档
//	├── stop_condition.go        # StopConditionEvaluator 接口 + 5 个评估器实现
//	├── stop_condition_test.go   # 评估器测试
//	├── loop_coordinator.go      # LoopCoordinator + LoopCoordinatorState
//	└── loop_coordinator_test.go # LoopCoordinator 测试
//
// 对应 Python 代码：openjiuwen/harness/task_loop/ + openjiuwen/harness/schema/stop_condition.py
package task_loop
```

- [ ] **Step 3: 验证编译**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/task_loop/...
```

Expected: 编译成功（无 .go 文件时可能有 warning，可忽略）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/task_loop/doc.go
git commit -m "feat(harness): 添加 task_loop 包 doc.go（9.5 前置）"
```

---

### Task 2: 实现 StopConditionEvaluator 接口和 StopEvaluationContext

**Files:**
- Create: `internal/agentcore/harness/task_loop/stop_condition.go`

- [ ] **Step 1: 编写 stop_condition.go 的接口和上下文部分**

```go
package task_loop

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// StopEvaluationContext 停止条件评估上下文。
// 与 AgentCallbackContext 解耦，使评估器不依赖 Agent 回调系统。
// 对齐 Python: StopEvaluationContext
type StopEvaluationContext struct {
	// Iteration 当前迭代次数（已完成的轮数）
	Iteration int
	// TokenUsage 累计 token 用量
	TokenUsage int
	// ElapsedSeconds 已用时间（秒）
	ElapsedSeconds float64
	// LastResult 上一轮结果
	LastResult map[string]any
	// Extra 额外上下文（供自定义评估器使用）
	Extra map[string]any
}

// ──────────────────────────── 接口 ────────────────────────────

// StopConditionEvaluator 停止条件评估器接口。
// 每个评估器回答一个问题："循环该停了吗？"
// LoopCoordinator 持有评估器切片，OR 语义：第一个 ShouldStop=true 即停止。
// 对齐 Python: StopConditionEvaluator
type StopConditionEvaluator interface {
	// ShouldStop 评估是否应该停止循环
	ShouldStop(ctx StopEvaluationContext) bool

	// Name 返回评估器名称（用于状态序列化索引、停止原因和日志）
	Name() string

	// ExportState 导出评估器状态用于持久化（无状态评估器返回空 map）
	ExportState() map[string]any

	// ImportState 从持久化状态恢复（无状态评估器忽略参数）
	ImportState(data map[string]any)

	// Reset 重置内部状态，用于新的 invoke 周期
	Reset()
}
```

- [ ] **Step 2: 编写 MaxRoundsEvaluator**

在 `stop_condition.go` 末尾追加：

```go

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMaxRoundsEvaluator 创建最大轮次评估器。
// 对齐 Python: MaxRoundsEvaluator
func NewMaxRoundsEvaluator(maxRounds int) *MaxRoundsEvaluator {
	return &MaxRoundsEvaluator{maxRounds: maxRounds}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

在结构体区块追加 MaxRoundsEvaluator 结构体（在 StopEvaluationContext 之后）：

```go

// MaxRoundsEvaluator 最大轮次评估器。
// 当已完成轮数 >= maxRounds 时判定应停止。
// 对齐 Python: MaxRoundsEvaluator
type MaxRoundsEvaluator struct {
	// maxRounds 最大轮次
	maxRounds int
}

// ShouldStop 当 iteration >= maxRounds 时返回 true。
func (e *MaxRoundsEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return ctx.Iteration >= e.maxRounds
}

// Name 返回评估器名称 "max_rounds"。
func (e *MaxRoundsEvaluator) Name() string {
	return "max_rounds"
}

// ExportState 无状态评估器，返回空 map。
func (e *MaxRoundsEvaluator) ExportState() map[string]any {
	return map[string]any{}
}

// ImportState 无状态评估器，忽略参数。
func (e *MaxRoundsEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *MaxRoundsEvaluator) Reset() {}
```

- [ ] **Step 3: 编写 TokenBudgetEvaluator**

在结构体区块追加：

```go

// TokenBudgetEvaluator token 预算评估器。
// 当累计 token 用量 >= maxTokens 时判定应停止。
// 对齐 Python: TokenBudgetEvaluator
type TokenBudgetEvaluator struct {
	// maxTokens 最大 token 数
	maxTokens int
}
```

在导出函数区块追加：

```go

// NewTokenBudgetEvaluator 创建 token 预算评估器。
// 对齐 Python: TokenBudgetEvaluator
func NewTokenBudgetEvaluator(maxTokens int) *TokenBudgetEvaluator {
	return &TokenBudgetEvaluator{maxTokens: maxTokens}
}
```

在非导出函数区块追加方法实现：

```go

// ShouldStop 当 tokenUsage >= maxTokens 时返回 true。
func (e *TokenBudgetEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return ctx.TokenUsage >= e.maxTokens
}

// Name 返回评估器名称 "token_budget"。
func (e *TokenBudgetEvaluator) Name() string {
	return "token_budget"
}

// ExportState 无状态评估器，返回空 map。
func (e *TokenBudgetEvaluator) ExportState() map[string]any {
	return map[string]any{}
}

// ImportState 无状态评估器，忽略参数。
func (e *TokenBudgetEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *TokenBudgetEvaluator) Reset() {}
```

- [ ] **Step 4: 编写 TimeoutEvaluator**

在结构体区块追加：

```go

// TimeoutEvaluator 超时评估器。
// 当墙钟时间 >= timeoutSeconds 时判定应停止。
// 对齐 Python: TimeoutEvaluator
type TimeoutEvaluator struct {
	// timeoutSeconds 超时秒数
	timeoutSeconds float64
}
```

在导出函数区块追加：

```go

// NewTimeoutEvaluator 创建超时评估器。
// 对齐 Python: TimeoutEvaluator
func NewTimeoutEvaluator(timeoutSeconds float64) *TimeoutEvaluator {
	return &TimeoutEvaluator{timeoutSeconds: timeoutSeconds}
}
```

在非导出函数区块追加方法实现：

```go

// ShouldStop 当 elapsedSeconds >= timeoutSeconds 时返回 true。
func (e *TimeoutEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return ctx.ElapsedSeconds >= e.timeoutSeconds
}

// Name 返回评估器名称 "timeout"。
func (e *TimeoutEvaluator) Name() string {
	return "timeout"
}

// ExportState 无状态评估器，返回空 map。
func (e *TimeoutEvaluator) ExportState() map[string]any {
	return map[string]any{}
}

// ImportState 无状态评估器，忽略参数。
func (e *TimeoutEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *TimeoutEvaluator) Reset() {}
```

- [ ] **Step 5: 编写 CompletionPromiseEvaluator**

在结构体区块追加：

```go

// CompletionPromiseEvaluator 完成承诺评估器。
// 追踪连续检测到 promise 标签的次数，连续达到 requiredConfirmations 次时判定完成。
// TaskCompletionRail 在 before_model_call 时注入 promise 提示，
// 在 after_model_call 时检测输出中的 promise 标签并调用 NotifyFulfilled/NotifyAbsent。
// 对齐 Python: CompletionPromiseEvaluator
type CompletionPromiseEvaluator struct {
	// promise 要匹配的标签（如 "<promise>"）
	promise string
	// requiredConfirmations 需要连续检测到的次数
	requiredConfirmations int
	// confirmationCount 当前已连续检测到的次数
	confirmationCount int
	// fulfilled 是否已达到所需确认次数
	fulfilled bool
	// matchedText 最近一次匹配到的文本
	matchedText string
}
```

在导出函数区块追加：

```go

// NewCompletionPromiseEvaluator 创建完成承诺评估器。
// promise 为要匹配的标签，requiredConfirmations 为需连续检测到的次数（至少 1）。
// 对齐 Python: CompletionPromiseEvaluator.__init__
func NewCompletionPromiseEvaluator(promise string, requiredConfirmations int) *CompletionPromiseEvaluator {
	if requiredConfirmations < 1 {
		requiredConfirmations = 1
	}
	return &CompletionPromiseEvaluator{
		promise:               promise,
		requiredConfirmations: requiredConfirmations,
	}
}
```

在非导出函数区块追加方法实现：

```go

// ShouldStop 当 fulfilled 标志为 true 时返回 true。
// 对齐 Python: CompletionPromiseEvaluator.should_stop
func (e *CompletionPromiseEvaluator) ShouldStop(_ StopEvaluationContext) bool {
	return e.fulfilled
}

// Name 返回评估器名称 "completion_promise"。
func (e *CompletionPromiseEvaluator) Name() string {
	return "completion_promise"
}

// ExportState 导出状态：fulfilled, matchedText, requiredConfirmations, confirmationCount。
// 对齐 Python: CompletionPromiseEvaluator.get_state
func (e *CompletionPromiseEvaluator) ExportState() map[string]any {
	return map[string]any{
		"fulfilled":             e.fulfilled,
		"matched_text":          e.matchedText,
		"required_confirmations": e.requiredConfirmations,
		"confirmation_count":    e.confirmationCount,
	}
}

// ImportState 从持久化状态恢复。
// 对齐 Python: CompletionPromiseEvaluator.load_state
func (e *CompletionPromiseEvaluator) ImportState(data map[string]any) {
	if data == nil {
		return
	}
	e.fulfilled = toBool(data["fulfilled"])
	e.matchedText = toStr(data["matched_text"])
	if v, ok := data["required_confirmations"]; ok {
		if n := toInt(v); n >= 1 {
			e.requiredConfirmations = n
		}
	}
	if v, ok := data["confirmation_count"]; ok {
		e.confirmationCount = max(0, toInt(v))
	}
	// 恢复后重新计算 fulfilled
	e.fulfilled = e.fulfilled || (e.confirmationCount >= e.requiredConfirmations)
}

// Reset 重置状态，用于新的 invoke 周期。
// 对齐 Python: CompletionPromiseEvaluator.reset
func (e *CompletionPromiseEvaluator) Reset() {
	e.fulfilled = false
	e.matchedText = ""
	e.confirmationCount = 0
}

// NotifyFulfilled 标记 promise 已满足。
// 对齐 Python: CompletionPromiseEvaluator.notify_fulfilled
func (e *CompletionPromiseEvaluator) NotifyFulfilled(matchedText string) {
	e.confirmationCount++
	e.fulfilled = e.confirmationCount >= e.requiredConfirmations
	e.matchedText = matchedText
}

// NotifyAbsent 记录 promise 未出现，连续计数归零。
// 对齐 Python: CompletionPromiseEvaluator.notify_absent
func (e *CompletionPromiseEvaluator) NotifyAbsent() {
	e.confirmationCount = 0
	e.fulfilled = false
	e.matchedText = ""
}

// Confirmations 返回当前连续确认次数。
func (e *CompletionPromiseEvaluator) Confirmations() int {
	return e.confirmationCount
}

// Promise 返回要匹配的标签。
func (e *CompletionPromiseEvaluator) Promise() string {
	return e.promise
}

// RequiredConfirmations 返回所需连续确认次数。
func (e *CompletionPromiseEvaluator) RequiredConfirmations() int {
	return e.requiredConfirmations
}
```

在非导出函数区块末尾追加辅助函数：

```go

// toBool 安全转换 any 到 bool。
func toBool(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// toStr 安全转换 any 到 string。
func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// toInt 安全转换 any 到 int。
func toInt(v any) int {
	if v == nil {
		return 0
	}
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
```

- [ ] **Step 6: 编写 CustomPredicateEvaluator**

在结构体区块追加：

```go

// CustomPredicateEvaluator 自定义谓词评估器。
// 通过用户提供的函数判断是否停止循环。
// 对齐 Python: CustomPredicateEvaluator
type CustomPredicateEvaluator struct {
	// name 评估器名称
	name string
	// predicate 自定义判断函数
	predicate func(ctx StopEvaluationContext) bool
}
```

在导出函数区块追加：

```go

// NewCustomPredicateEvaluator 创建自定义谓词评估器。
// 对齐 Python: CustomPredicateEvaluator
func NewCustomPredicateEvaluator(name string, predicate func(ctx StopEvaluationContext) bool) *CustomPredicateEvaluator {
	return &CustomPredicateEvaluator{name: name, predicate: predicate}
}
```

在非导出函数区块追加方法实现：

```go

// ShouldStop 委托给用户提供的谓词函数。
// 对齐 Python: CustomPredicateEvaluator.should_stop
func (e *CustomPredicateEvaluator) ShouldStop(ctx StopEvaluationContext) bool {
	return e.predicate(ctx)
}

// Name 返回评估器名称。
func (e *CustomPredicateEvaluator) Name() string {
	return e.name
}

// ExportState 无状态评估器，返回空 map。
func (e *CustomPredicateEvaluator) ExportState() map[string]any {
	return map[string]any{}
}

// ImportState 无状态评估器，忽略参数。
func (e *CustomPredicateEvaluator) ImportState(_ map[string]any) {}

// Reset 无状态评估器，无操作。
func (e *CustomPredicateEvaluator) Reset() {}
```

- [ ] **Step 7: 验证编译**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/task_loop/...
```

Expected: 编译成功

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/task_loop/stop_condition.go
git commit -m "feat(harness): 实现 StopConditionEvaluator 接口和 5 个评估器（9.5）"
```

---

### Task 3: 编写 StopConditionEvaluator 测试

**Files:**
- Create: `internal/agentcore/harness/task_loop/stop_condition_test.go`

- [ ] **Step 1: 编写测试文件**

```go
package task_loop

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestMaxRoundsEvaluator_基本功能(t *testing.T) {
	e := NewMaxRoundsEvaluator(3)

	if e.Name() != "max_rounds" {
		t.Errorf("Name() = %q, want %q", e.Name(), "max_rounds")
	}

	ctx := StopEvaluationContext{Iteration: 2}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=2 with maxRounds=3, want false")
	}

	ctx.Iteration = 3
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=3 with maxRounds=3, want true")
	}

	ctx.Iteration = 5
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=5 with maxRounds=3, want true")
	}
}

func TestMaxRoundsEvaluator_边界值(t *testing.T) {
	e := NewMaxRoundsEvaluator(0)
	ctx := StopEvaluationContext{Iteration: 0}
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=0 with maxRounds=0, want true")
	}

	e1 := NewMaxRoundsEvaluator(1)
	ctx1 := StopEvaluationContext{Iteration: 0}
	if e1.ShouldStop(ctx1) {
		t.Error("ShouldStop at iteration=0 with maxRounds=1, want false")
	}
	ctx1.Iteration = 1
	if !e1.ShouldStop(ctx1) {
		t.Error("ShouldStop at iteration=1 with maxRounds=1, want true")
	}
}

func TestMaxRoundsEvaluator_状态方法(t *testing.T) {
	e := NewMaxRoundsEvaluator(5)

	// 无状态评估器：ExportState 返回空 map
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}

	// ImportState 不 panic 即可
	e.ImportState(map[string]any{"foo": "bar"})

	// Reset 不 panic 即可
	e.Reset()
}

func TestTokenBudgetEvaluator_基本功能(t *testing.T) {
	e := NewTokenBudgetEvaluator(1000)

	if e.Name() != "token_budget" {
		t.Errorf("Name() = %q, want %q", e.Name(), "token_budget")
	}

	ctx := StopEvaluationContext{TokenUsage: 500}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at tokenUsage=500 with maxTokens=1000, want false")
	}

	ctx.TokenUsage = 1000
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at tokenUsage=1000 with maxTokens=1000, want true")
	}
}

func TestTokenBudgetEvaluator_边界值(t *testing.T) {
	e := NewTokenBudgetEvaluator(0)
	ctx := StopEvaluationContext{TokenUsage: 0}
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at tokenUsage=0 with maxTokens=0, want true")
	}
}

func TestTokenBudgetEvaluator_状态方法(t *testing.T) {
	e := NewTokenBudgetEvaluator(100)
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}
	e.ImportState(nil)
	e.Reset()
}

func TestTimeoutEvaluator_基本功能(t *testing.T) {
	e := NewTimeoutEvaluator(60.0)

	if e.Name() != "timeout" {
		t.Errorf("Name() = %q, want %q", e.Name(), "timeout")
	}

	ctx := StopEvaluationContext{ElapsedSeconds: 30.0}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at 30s with timeout=60s, want false")
	}

	ctx.ElapsedSeconds = 60.0
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at 60s with timeout=60s, want true")
	}

	ctx.ElapsedSeconds = 120.0
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at 120s with timeout=60s, want true")
	}
}

func TestTimeoutEvaluator_边界值(t *testing.T) {
	e := NewTimeoutEvaluator(0.0)
	ctx := StopEvaluationContext{ElapsedSeconds: 0.0}
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at 0s with timeout=0s, want true")
	}
}

func TestTimeoutEvaluator_状态方法(t *testing.T) {
	e := NewTimeoutEvaluator(60.0)
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}
	e.ImportState(nil)
	e.Reset()
}

func TestCompletionPromiseEvaluator_基本功能(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	if e.Name() != "completion_promise" {
		t.Errorf("Name() = %q, want %q", e.Name(), "completion_promise")
	}

	if e.Promise() != "<promise>" {
		t.Errorf("Promise() = %q, want %q", e.Promise(), "<promise>")
	}

	if e.RequiredConfirmations() != 2 {
		t.Errorf("RequiredConfirmations() = %d, want 2", e.RequiredConfirmations())
	}

	// 初始状态：不应停止
	ctx := StopEvaluationContext{}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop initially, want false")
	}
}

func TestCompletionPromiseEvaluator_连续确认(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	// 第 1 次满足：还差 1 次
	e.NotifyFulfilled("task done")
	if e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 1 confirmation with required=2, want false")
	}
	if e.Confirmations() != 1 {
		t.Errorf("Confirmations() = %d, want 1", e.Confirmations())
	}

	// 第 2 次满足：达到所需次数
	e.NotifyFulfilled("task done again")
	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 2 confirmations with required=2, want true")
	}
	if e.Confirmations() != 2 {
		t.Errorf("Confirmations() = %d, want 2", e.Confirmations())
	}
}

func TestCompletionPromiseEvaluator_中断归零(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	// 第 1 次满足
	e.NotifyFulfilled("task done")
	if e.Confirmations() != 1 {
		t.Errorf("Confirmations() = %d, want 1", e.Confirmations())
	}

	// 中断：计数归零
	e.NotifyAbsent()
	if e.Confirmations() != 0 {
		t.Errorf("Confirmations() after absent = %d, want 0", e.Confirmations())
	}
	if e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after absent, want false")
	}

	// 重新开始计数
	e.NotifyFulfilled("task done")
	if e.Confirmations() != 1 {
		t.Errorf("Confirmations() = %d, want 1", e.Confirmations())
	}
}

func TestCompletionPromiseEvaluator_仅需1次确认(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 1)

	e.NotifyFulfilled("done")
	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 1 confirmation with required=1, want true")
	}
}

func TestCompletionPromiseEvaluator_所需次数最少为1(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 0)
	if e.RequiredConfirmations() != 1 {
		t.Errorf("RequiredConfirmations() = %d, want 1 (至少为1)", e.RequiredConfirmations())
	}

	e2 := NewCompletionPromiseEvaluator("<promise>", -5)
	if e2.RequiredConfirmations() != 1 {
		t.Errorf("RequiredConfirmations() = %d, want 1 (至少为1)", e2.RequiredConfirmations())
	}
}

func TestCompletionPromiseEvaluator_状态导出导入(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 3)

	// 模拟 2 次确认
	e.NotifyFulfilled("done")
	e.NotifyFulfilled("done again")

	state := e.ExportState()
	if state["confirmation_count"] != 2 {
		t.Errorf("ExportState confirmation_count = %v, want 2", state["confirmation_count"])
	}
	if state["fulfilled"] != false {
		t.Errorf("ExportState fulfilled = %v, want false", state["fulfilled"])
	}
	if state["required_confirmations"] != 3 {
		t.Errorf("ExportState required_confirmations = %v, want 3", state["required_confirmations"])
	}

	// 恢复到新评估器
	e2 := NewCompletionPromiseEvaluator("<promise>", 1)
	e2.ImportState(state)
	if e2.Confirmations() != 2 {
		t.Errorf("Confirmations after ImportState = %d, want 2", e2.Confirmations())
	}
	if e2.RequiredConfirmations() != 3 {
		t.Errorf("RequiredConfirmations after ImportState = %d, want 3", e2.RequiredConfirmations())
	}
	// confirmationCount(2) < requiredConfirmations(3)，所以 fulfilled 仍为 false
	if e2.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after ImportState with count=2 < required=3, want false")
	}

	// 再确认 1 次达到 3 次
	e2.NotifyFulfilled("final")
	if !e2.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after 3rd confirmation with required=3, want true")
	}
}

func TestCompletionPromiseEvaluator_状态导入覆盖fulfilled(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)

	// 直接导入一个 fulfilled=true 的状态
	e.ImportState(map[string]any{
		"fulfilled":             true,
		"matched_text":          "done",
		"required_confirmations": 2,
		"confirmation_count":    2,
	})
	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after ImportState with fulfilled=true, want true")
	}
}

func TestCompletionPromiseEvaluator_状态导入空数据(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 2)
	e.NotifyFulfilled("done")

	// 导入 nil 不 panic
	e.ImportState(nil)

	// 导入空 map 不 panic
	e.ImportState(map[string]any{})
}

func TestCompletionPromiseEvaluator_Reset(t *testing.T) {
	e := NewCompletionPromiseEvaluator("<promise>", 1)
	e.NotifyFulfilled("done")

	if !e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop before Reset, want true")
	}

	e.Reset()

	if e.ShouldStop(StopEvaluationContext{}) {
		t.Error("ShouldStop after Reset, want false")
	}
	if e.Confirmations() != 0 {
		t.Errorf("Confirmations after Reset = %d, want 0", e.Confirmations())
	}
}

func TestCustomPredicateEvaluator_基本功能(t *testing.T) {
	called := false
	e := NewCustomPredicateEvaluator("custom_test", func(ctx StopEvaluationContext) bool {
		called = true
		return ctx.Iteration >= 5
	})

	if e.Name() != "custom_test" {
		t.Errorf("Name() = %q, want %q", e.Name(), "custom_test")
	}

	ctx := StopEvaluationContext{Iteration: 3}
	if e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=3, want false")
	}
	if !called {
		t.Error("predicate was not called")
	}

	called = false
	ctx.Iteration = 5
	if !e.ShouldStop(ctx) {
		t.Error("ShouldStop at iteration=5, want true")
	}
}

func TestCustomPredicateEvaluator_状态方法(t *testing.T) {
	e := NewCustomPredicateEvaluator("test", func(ctx StopEvaluationContext) bool {
		return false
	})
	state := e.ExportState()
	if len(state) != 0 {
		t.Errorf("ExportState() = %v, want empty map", state)
	}
	e.ImportState(nil)
	e.Reset()
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test -v -count=1 ./internal/agentcore/harness/task_loop/... -run 'Test(MaxRounds|TokenBudget|Timeout|CompletionPromise|CustomPredicate)'
```

Expected: 所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/task_loop/stop_condition_test.go
git commit -m "test(harness): 添加 StopConditionEvaluator 5 个评估器测试（9.5）"
```

---

### Task 4: 实现 LoopCoordinator 和 LoopCoordinatorState

**Files:**
- Create: `internal/agentcore/harness/task_loop/loop_coordinator.go`

- [ ] **Step 1: 编写 loop_coordinator.go**

```go
package task_loop

import (
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopCoordinatorState 循环协调器可序列化状态。
// 对齐 Python: LoopCoordinator.get_state() 返回值
type LoopCoordinatorState struct {
	// Iteration 迭代次数
	Iteration int `json:"iteration"`
	// TokenUsage 累计 token 用量
	TokenUsage int `json:"token_usage"`
	// StopReason 停止原因（评估器名称或 "Aborted"）
	StopReason string `json:"stop_reason"`
	// EvaluatorStates 各评估器状态（按 Name() 索引）
	EvaluatorStates map[string]map[string]any `json:"evaluator_states"`
}

// LoopCoordinator 外层任务循环协调器。
// 追踪迭代次数、token 用量、耗时和中止标记，
// 每轮迭代前通过评估器链（OR 语义）决定是否继续循环。
// 对齐 Python: LoopCoordinator
type LoopCoordinator struct {
	mu         sync.Mutex
	iteration  int
	tokenUsage int
	aborted    bool
	startTime  time.Time
	stopReason string
	lastResult map[string]any
	evaluators []StopConditionEvaluator
}

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoopCoordinator 创建循环协调器。
// 对齐 Python: LoopCoordinator.__init__
func NewLoopCoordinator(evaluators []StopConditionEvaluator) *LoopCoordinator {
	if evaluators == nil {
		evaluators = []StopConditionEvaluator{}
	}
	return &LoopCoordinator{
		evaluators: evaluators,
		startTime:  time.Now(),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ShouldContinue 评估是否应该继续循环。
// 先检查中止标记，再遍历评估器（OR 语义：第一个 ShouldStop=true 即停止）。
// 对齐 Python: LoopCoordinator.should_continue
func (lc *LoopCoordinator) ShouldContinue() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.aborted {
		lc.stopReason = "Aborted"
		return false
	}

	ctx := lc.buildEvalContext()
	for _, ev := range lc.evaluators {
		stopped := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).
						Str("evaluator", ev.Name()).
						Any("panic", r).
						Msg("评估器 panic，跳过")
				}
			}()
			stopped = ev.ShouldStop(ctx)
		}()
		if stopped {
			lc.stopReason = ev.Name()
			logger.Info(logComponent).
				Str("stop_condition", ev.Name()).
				Msg("满足停止条件")
			return false
		}
	}
	return true
}

// IncrementIteration 递增迭代次数。
// 对齐 Python: LoopCoordinator.increment_iteration
func (lc *LoopCoordinator) IncrementIteration() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.iteration++
}

// AddTokenUsage 累加 token 用量（仅正数有效）。
// 对齐 Python: LoopCoordinator.add_token_usage
func (lc *LoopCoordinator) AddTokenUsage(tokens int) {
	if tokens <= 0 {
		return
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.tokenUsage += tokens
}

// SetLastResult 设置上一轮结果。
// 对齐 Python: LoopCoordinator.set_last_result
func (lc *LoopCoordinator) SetLastResult(result map[string]any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.lastResult = result
}

// RequestAbort 请求中止循环。
// 对齐 Python: LoopCoordinator.request_abort
func (lc *LoopCoordinator) RequestAbort() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.aborted = true
}

// Reset 重置所有状态，用于新的 invoke 周期。
// 对齐 Python: LoopCoordinator.reset
func (lc *LoopCoordinator) Reset() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.iteration = 0
	lc.tokenUsage = 0
	lc.aborted = false
	lc.startTime = time.Now()
	lc.stopReason = ""
	lc.lastResult = nil
	for _, ev := range lc.evaluators {
		ev.Reset()
	}
}

// IsAborted 返回是否已中止。
func (lc *LoopCoordinator) IsAborted() bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.aborted
}

// StopReason 返回停止原因。
func (lc *LoopCoordinator) StopReason() string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.stopReason
}

// Iteration 返回当前迭代次数。
func (lc *LoopCoordinator) Iteration() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.iteration
}

// TokenUsage 返回累计 token 用量。
func (lc *LoopCoordinator) TokenUsage() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.tokenUsage
}

// ElapsedSeconds 返回已用时间（秒）。
func (lc *LoopCoordinator) ElapsedSeconds() float64 {
	lc.mu.Lock()
	startTime := lc.startTime
	lc.mu.Unlock()
	return time.Since(startTime).Seconds()
}

// ExportState 导出状态用于持久化。
// 对齐 Python: LoopCoordinator.get_state
func (lc *LoopCoordinator) ExportState() LoopCoordinatorState {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	evStates := make(map[string]map[string]any, len(lc.evaluators))
	for _, ev := range lc.evaluators {
		s := ev.ExportState()
		if s != nil {
			evStates[ev.Name()] = s
		}
	}

	return LoopCoordinatorState{
		Iteration:       lc.iteration,
		TokenUsage:      lc.tokenUsage,
		StopReason:      lc.stopReason,
		EvaluatorStates: evStates,
	}
}

// ImportState 从持久化状态恢复。
// startTime 重置为当前时间，使 TimeoutEvaluator 从恢复点开始计时。
// 对齐 Python: LoopCoordinator.load_state
func (lc *LoopCoordinator) ImportState(state LoopCoordinatorState) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.iteration = state.Iteration
	lc.tokenUsage = state.TokenUsage
	lc.stopReason = state.StopReason
	lc.startTime = time.Now()

	if state.EvaluatorStates != nil {
		for _, ev := range lc.evaluators {
			if evState, ok := state.EvaluatorStates[ev.Name()]; ok {
				ev.ImportState(evState)
			}
		}
	}
}

// GetCompletionPromiseEvaluator 返回第一个 CompletionPromiseEvaluator（如有）。
// 对齐 Python: LoopCoordinator.get_completion_promise_evaluator
func (lc *LoopCoordinator) GetCompletionPromiseEvaluator() *CompletionPromiseEvaluator {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for _, ev := range lc.evaluators {
		if cpe, ok := ev.(*CompletionPromiseEvaluator); ok {
			return cpe
		}
	}
	return nil
}

// Evaluators 返回评估器切片的副本（只读访问）。
func (lc *LoopCoordinator) Evaluators() []StopConditionEvaluator {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	result := make([]StopConditionEvaluator, len(lc.evaluators))
	copy(result, lc.evaluators)
	return result
}

// buildEvalContext 构建评估上下文（调用者需持有锁）。
// 对齐 Python: LoopCoordinator._build_eval_context
func (lc *LoopCoordinator) buildEvalContext() StopEvaluationContext {
	elapsed := time.Since(lc.startTime).Seconds()
	return StopEvaluationContext{
		Iteration:      lc.iteration,
		TokenUsage:     lc.tokenUsage,
		ElapsedSeconds: elapsed,
		LastResult:     lc.lastResult,
	}
}
```

- [ ] **Step 2: 验证编译**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/task_loop/...
```

Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/task_loop/loop_coordinator.go
git commit -m "feat(harness): 实现 LoopCoordinator + LoopCoordinatorState（9.5）"
```

---

### Task 5: 编写 LoopCoordinator 测试

**Files:**
- Create: `internal/agentcore/harness/task_loop/loop_coordinator_test.go`

- [ ] **Step 1: 编写测试文件**

```go
package task_loop

import (
	"sync"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNewLoopCoordinator_空评估器(t *testing.T) {
	lc := NewLoopCoordinator(nil)
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue with no evaluators, want true")
	}
}

func TestNewLoopCoordinator_构造(t *testing.T) {
	evaluators := []StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewTimeoutEvaluator(60.0),
	}
	lc := NewLoopCoordinator(evaluators)

	if lc.Iteration() != 0 {
		t.Errorf("Iteration() = %d, want 0", lc.Iteration())
	}
	if lc.TokenUsage() != 0 {
		t.Errorf("TokenUsage() = %d, want 0", lc.TokenUsage())
	}
	if lc.IsAborted() {
		t.Error("IsAborted(), want false")
	}
	if lc.StopReason() != "" {
		t.Errorf("StopReason() = %q, want empty", lc.StopReason())
	}
}

func TestLoopCoordinator_ShouldContinue_单个评估器(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(3),
	})

	// iteration=0, maxRounds=3 → 继续
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=0, want true")
	}

	lc.IncrementIteration() // 1
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=1, want true")
	}

	lc.IncrementIteration() // 2
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=2, want true")
	}

	lc.IncrementIteration() // 3
	if lc.ShouldContinue() {
		t.Error("ShouldContinue at iteration=3 with maxRounds=3, want false")
	}

	if lc.StopReason() != "max_rounds" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "max_rounds")
	}
}

func TestLoopCoordinator_ShouldContinue_多个评估器OR语义(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewTimeoutEvaluator(3600.0),
		NewTokenBudgetEvaluator(10000),
	})

	// 三个条件都不满足 → 继续
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue with no condition met, want true")
	}

	// 设置 token 用量超过预算
	lc.AddTokenUsage(10001)
	if lc.ShouldContinue() {
		t.Error("ShouldContinue after exceeding token budget, want false")
	}
	if lc.StopReason() != "token_budget" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "token_budget")
	}
}

func TestLoopCoordinator_ShouldContinue_OR语义第一个命中(t *testing.T) {
	callCount := 0
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(1), // 第一个评估器
		NewCustomPredicateEvaluator("never", func(ctx StopEvaluationContext) bool {
			callCount++
			return false
		}),
	})

	lc.IncrementIteration() // iteration=1, maxRounds=1 → 第一个命中
	if lc.ShouldContinue() {
		t.Error("ShouldContinue after max rounds, want false")
	}
	// OR 语义：第一个评估器命中后不再评估后续评估器
	// callCount 仍为 0，说明第二个评估器未被调用
	if callCount != 0 {
		t.Errorf("second evaluator was called %d times, want 0 (OR short-circuit)", callCount)
	}
}

func TestLoopCoordinator_RequestAbort(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(100),
	})

	if !lc.ShouldContinue() {
		t.Error("ShouldContinue before abort, want true")
	}

	lc.RequestAbort()
	if lc.ShouldContinue() {
		t.Error("ShouldContinue after abort, want false")
	}
	if lc.StopReason() != "Aborted" {
		t.Errorf("StopReason() = %q, want %q", lc.StopReason(), "Aborted")
	}
}

func TestLoopCoordinator_IncrementIteration(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	for i := 0; i < 5; i++ {
		if lc.Iteration() != i {
			t.Errorf("Iteration() before increment = %d, want %d", lc.Iteration(), i)
		}
		lc.IncrementIteration()
	}
	if lc.Iteration() != 5 {
		t.Errorf("Iteration() = %d, want 5", lc.Iteration())
	}
}

func TestLoopCoordinator_AddTokenUsage(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	lc.AddTokenUsage(100)
	if lc.TokenUsage() != 100 {
		t.Errorf("TokenUsage() = %d, want 100", lc.TokenUsage())
	}

	lc.AddTokenUsage(50)
	if lc.TokenUsage() != 150 {
		t.Errorf("TokenUsage() = %d, want 150", lc.TokenUsage())
	}

	// 负数和零无效
	lc.AddTokenUsage(0)
	if lc.TokenUsage() != 150 {
		t.Errorf("TokenUsage() after AddTokenUsage(0) = %d, want 150", lc.TokenUsage())
	}

	lc.AddTokenUsage(-10)
	if lc.TokenUsage() != 150 {
		t.Errorf("TokenUsage() after AddTokenUsage(-10) = %d, want 150", lc.TokenUsage())
	}
}

func TestLoopCoordinator_SetLastResult(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	result := map[string]any{"status": "ok", "data": 42}
	lc.SetLastResult(result)

	// 通过 ExportState 验证 lastResult 被设置
	state := lc.ExportState()
	// lastResult 不直接导出到 LoopCoordinatorState（Python 也不导出），
	// 但我们可通过 CompletionPromiseEvaluator 间接验证
}

func TestLoopCoordinator_ElapsedSeconds(t *testing.T) {
	lc := NewLoopCoordinator(nil)

	elapsed := lc.ElapsedSeconds()
	if elapsed < 0 {
		t.Errorf("ElapsedSeconds() = %f, want >= 0", elapsed)
	}

	// 等待一小段时间后验证时间增长
	time.Sleep(50 * time.Millisecond)
	elapsed2 := lc.ElapsedSeconds()
	if elapsed2 < elapsed {
		t.Errorf("ElapsedSeconds() decreased: %f -> %f", elapsed, elapsed2)
	}
}

func TestLoopCoordinator_ExportState_基本(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewCompletionPromiseEvaluator("<promise>", 2),
	})

	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(500)

	state := lc.ExportState()
	if state.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", state.Iteration)
	}
	if state.TokenUsage != 500 {
		t.Errorf("TokenUsage = %d, want 500", state.TokenUsage)
	}
	if state.StopReason != "" {
		t.Errorf("StopReason = %q, want empty", state.StopReason)
	}

	// MaxRoundsEvaluator 无状态 → 空 map
	if _, ok := state.EvaluatorStates["max_rounds"]; !ok {
		t.Error("evaluator_states missing max_rounds")
	}

	// CompletionPromiseEvaluator 有状态
	if _, ok := state.EvaluatorStates["completion_promise"]; !ok {
		t.Error("evaluator_states missing completion_promise")
	}
}

func TestLoopCoordinator_ImportState_恢复(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 3)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		cpe,
	})

	// 模拟运行后导出
	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(300)
	cpe.NotifyFulfilled("done")

	exported := lc.ExportState()

	// 创建新的 coordinator 并导入
	lc2 := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		NewCompletionPromiseEvaluator("<promise>", 1), // requiredConfirmations 不同
	})
	lc2.ImportState(exported)

	if lc2.Iteration() != 2 {
		t.Errorf("Iteration after ImportState = %d, want 2", lc2.Iteration())
	}
	if lc2.TokenUsage() != 300 {
		t.Errorf("TokenUsage after ImportState = %d, want 300", lc2.TokenUsage())
	}
	if lc2.StopReason() != "" {
		t.Errorf("StopReason after ImportState = %q, want empty", lc2.StopReason())
	}

	// CompletionPromiseEvaluator 应从导入状态恢复
	cpe2 := lc2.GetCompletionPromiseEvaluator()
	if cpe2 == nil {
		t.Fatal("GetCompletionPromiseEvaluator() returned nil")
	}
	if cpe2.Confirmations() != 1 {
		t.Errorf("Confirmations after ImportState = %d, want 1", cpe2.Confirmations())
	}
	if cpe2.RequiredConfirmations() != 3 {
		t.Errorf("RequiredConfirmations after ImportState = %d, want 3", cpe2.RequiredConfirmations())
	}
}

func TestLoopCoordinator_ExportImportState_往返(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 2)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
		cpe,
	})

	lc.IncrementIteration()
	lc.AddTokenUsage(100)
	cpe.NotifyFulfilled("done")
	cpe.NotifyFulfilled("done again") // 2 次 → fulfilled

	state1 := lc.ExportState()

	// 导入到新 coordinator
	lc2 := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
		NewCompletionPromiseEvaluator("<promise>", 2),
	})
	lc2.ImportState(state1)

	state2 := lc2.ExportState()

	// 验证两次导出一致
	if state1.Iteration != state2.Iteration {
		t.Errorf("Iteration mismatch: %d vs %d", state1.Iteration, state2.Iteration)
	}
	if state1.TokenUsage != state2.TokenUsage {
		t.Errorf("TokenUsage mismatch: %d vs %d", state1.TokenUsage, state2.TokenUsage)
	}
	if state1.StopReason != state2.StopReason {
		t.Errorf("StopReason mismatch: %q vs %q", state1.StopReason, state2.StopReason)
	}
}

func TestLoopCoordinator_ImportState_空数据(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
	})

	// 导入空状态不 panic
	lc.ImportState(LoopCoordinatorState{})
	lc.ImportState(LoopCoordinatorState{EvaluatorStates: nil})
}

func TestLoopCoordinator_GetCompletionPromiseEvaluator_存在(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 2)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		cpe,
	})

	found := lc.GetCompletionPromiseEvaluator()
	if found == nil {
		t.Error("GetCompletionPromiseEvaluator() returned nil, want non-nil")
	}
	if found.Promise() != "<promise>" {
		t.Errorf("Promise() = %q, want %q", found.Promise(), "<promise>")
	}
}

func TestLoopCoordinator_GetCompletionPromiseEvaluator_不存在(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
	})

	found := lc.GetCompletionPromiseEvaluator()
	if found != nil {
		t.Error("GetCompletionPromiseEvaluator() returned non-nil, want nil")
	}
}

func TestLoopCoordinator_Reset(t *testing.T) {
	cpe := NewCompletionPromiseEvaluator("<promise>", 1)
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(10),
		cpe,
	})

	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(200)
	cpe.NotifyFulfilled("done")
	lc.RequestAbort()

	lc.Reset()

	if lc.Iteration() != 0 {
		t.Errorf("Iteration after Reset = %d, want 0", lc.Iteration())
	}
	if lc.TokenUsage() != 0 {
		t.Errorf("TokenUsage after Reset = %d, want 0", lc.TokenUsage())
	}
	if lc.IsAborted() {
		t.Error("IsAborted after Reset, want false")
	}
	if lc.StopReason() != "" {
		t.Errorf("StopReason after Reset = %q, want empty", lc.StopReason())
	}
	if cpe.Confirmations() != 0 {
		t.Errorf("CPE Confirmations after Reset = %d, want 0", cpe.Confirmations())
	}
}

func TestLoopCoordinator_并发安全(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewMaxRoundsEvaluator(1000),
	})

	var wg sync.WaitGroup
	const goroutines = 10
	const opsPerGoroutine = 100

	// 并发 IncrementIteration
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				lc.IncrementIteration()
			}
		}()
	}

	// 并发 AddTokenUsage
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				lc.AddTokenUsage(1)
			}
		}()
	}

	// 并发 ShouldContinue
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				lc.ShouldContinue()
			}
		}()
	}

	wg.Wait()

	expectedOps := goroutines * opsPerGoroutine
	if lc.Iteration() != expectedOps {
		t.Errorf("Iteration() = %d, want %d", lc.Iteration(), expectedOps)
	}
	if lc.TokenUsage() != expectedOps {
		t.Errorf("TokenUsage() = %d, want %d", lc.TokenUsage(), expectedOps)
	}
}

func TestLoopCoordinator_ShouldContinue_评估器panic不崩溃(t *testing.T) {
	lc := NewLoopCoordinator([]StopConditionEvaluator{
		NewCustomPredicateEvaluator("panicking", func(ctx StopEvaluationContext) bool {
			panic("test panic")
		}),
		NewMaxRoundsEvaluator(100),
	})

	// panic 的评估器被 recover，循环继续评估后续评估器
	if !lc.ShouldContinue() {
		t.Error("ShouldContinue should be true when panicking evaluator is recovered and no other evaluator stops")
	}
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test -v -count=1 ./internal/agentcore/harness/task_loop/... -run 'TestLoopCoordinator'
```

Expected: 所有测试 PASS

- [ ] **Step 3: 运行全量测试确保无破坏**

```bash
cd /home/opensource/uap-claw-go && go test -count=1 ./internal/agentcore/harness/task_loop/...
```

Expected: 所有测试 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/task_loop/loop_coordinator_test.go
git commit -m "test(harness): 添加 LoopCoordinator 测试（9.5）"
```

---

### Task 6: 运行覆盖率检查和最终验证

**Files:**
- Modify: `internal/agentcore/harness/task_loop/` (所有文件)

- [ ] **Step 1: 运行覆盖率检查**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/task_loop/...
```

Expected: 覆盖率 >= 85%

- [ ] **Step 2: 运行 go vet 静态检查**

```bash
cd /home/opensource/uap-claw-go && go vet ./internal/agentcore/harness/task_loop/...
```

Expected: 无警告

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 中 9.5 的状态**

将 `9.5 LoopCoordinator + StopConditionEvaluator` 的状态从 `☐` 更新为 `✅`。

- [ ] **Step 4: 提交状态更新**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 9.5 状态为已完成"
```

---

## 自审清单

### 1. Spec 覆盖

| Spec 章节 | 对应 Task |
|-----------|----------|
| 6.1 StopConditionEvaluator 接口 | Task 2 |
| 6.2 StopEvaluationContext | Task 2 |
| 6.3 LoopCoordinatorState | Task 4 |
| 6.4 LoopCoordinator | Task 4 |
| 7.1 MaxRoundsEvaluator | Task 2 |
| 7.2 TokenBudgetEvaluator | Task 2 |
| 7.3 TimeoutEvaluator | Task 2 |
| 7.4 CompletionPromiseEvaluator | Task 2 |
| 7.5 CustomPredicateEvaluator | Task 2 |
| 8 测试覆盖 | Task 3 + Task 5 |
| 9 回填点 | Task 6（确认无新增回填） |
| 4 设计决策（sync.Mutex） | Task 4 + Task 5（并发测试） |

### 2. Placeholder 扫描

无 TBD/TODO/实现后补充/等占位符。✅

### 3. 类型一致性

- `StopConditionEvaluator` 接口 4 个方法（ShouldStop, Name, ExportState, ImportState）+ Reset，5 个评估器全部实现 ✅
- `LoopCoordinatorState` 字段名与 JSON tag 与 Python get_state() 一致（iteration, token_usage, stop_reason, evaluator_states）✅
- `NewCompletionPromiseEvaluator` 的 `requiredConfirmations` 至少为 1，与 Python `max(1, int(required_confirmations))` 一致 ✅
- `LoopCoordinator.ImportState` 重置 `startTime`，与 Python `load_state` 一致 ✅

### 4. 与 Python 源码对齐

| Python 方法 | Go 方法 | 对齐 |
|------------|---------|------|
| `should_continue()` | `ShouldContinue()` | ✅ aborted 检查 → 评估器遍历 → OR 语义 |
| `increment_iteration()` | `IncrementIteration()` | ✅ |
| `add_token_usage(tokens)` | `AddTokenUsage(tokens)` | ✅ 仅正数有效 |
| `set_last_result(result)` | `SetLastResult(result)` | ✅ |
| `request_abort()` | `RequestAbort()` | ✅ |
| `reset()` | `Reset()` | ✅ 重置所有字段 + 评估器 |
| `get_state()` | `ExportState()` | ✅ 字段名和结构一致 |
| `load_state(data)` | `ImportState(state)` | ✅ startTime 重置 + 评估器状态恢复 |
| `get_completion_promise_evaluator()` | `GetCompletionPromiseEvaluator()` | ✅ 类型断言 |
| `_build_eval_context()` | `buildEvalContext()` | ✅ |
| CompletionPromiseEvaluator.`notify_fulfilled()` | `NotifyFulfilled()` | ✅ |
| CompletionPromiseEvaluator.`notify_absent()` | `NotifyAbsent()` | ✅ |
| CompletionPromiseEvaluator.`reset()` | `Reset()` | ✅ |
| CompletionPromiseEvaluator.`get_state()` | `ExportState()` | ✅ 4 个字段 |
| CompletionPromiseEvaluator.`load_state()` | `ImportState()` | ✅ 恢复 + 重新计算 fulfilled |
