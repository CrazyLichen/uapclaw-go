# 6.24 AsyncCallbackFramework 全量实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `openjiuwen/core/runner/callback/` 全量实现，回填 `framework.go` 中 8 个 `⤵️` 占位符，新建 6 个文件，删除 3 个文件，重组文件结构一一对齐 Python。

**Architecture:** 在现有 `CallbackFramework` 基础上新增枚举、模型、过滤器、回调链、错误类、事件定义、工具函数 7 个模块。核心回填在 `triggerCallbacks` 函数中实现 BEFORE 钩子→过滤器管线→熔断器→超时→重试→指标→ERROR 钩子→AFTER 钩子完整流程。删除集中化日志 `logging.go`，对齐 Python 的 `llm_logger` 内联模式。

**Tech Stack:** Go 1.22+, sync.Mutex, context.WithTimeout, errgroup, zerolog (已有)

---

## 文件结构

### 新建文件

| 文件 | 对应 Python | 职责 |
|------|------------|------|
| `enums.go` | `enums.py` | FilterAction / ChainAction / HookType 三组枚举 |
| `models.go` | `models.py` | CallbackMetrics / FilterResult / ChainContext / ChainResult + 合并原 CallbackInfo[F] |
| `errors.go` | `errors.py` | AbortError（中止触发流程） |
| `filters.go` | `filters.py` | EventFilter 接口 + 7 种过滤器实现 |
| `chain.go` | `chain.py` | CallbackChain（顺序执行+回滚+重试+错误处理） |
| `utils.go` | `utils.py` | 全局单例 + Trigger 便捷函数 |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `events.go` | 新增 scope 工具函数 + EventBase + 5 个缺失域枚举/EventData/回调函数类型 |
| `framework.go` | 新增钩子/过滤器/熔断器/指标/历史字段 + 回填 triggerCallbacks 8 个占位符 + 新增触发/查询方法 + 删除 LoggingLLMCallback 注册 + 移出全局单例 |
| `doc.go` | 更新文件目录和核心类型索引 |

### 删除文件

| 文件 | 原因 |
|------|------|
| `callback_info.go` | 内容移入 `models.go` |
| `logging.go` | 删除集中化日志，对齐 Python llm_logger 内联模式 |
| `logging_test.go` | 随 logging.go 删除 |

---

### Task 1: 新建 enums.go + enums_test.go

**Files:**
- Create: `internal/agentcore/runner/callback/enums.go`
- Create: `internal/agentcore/runner/callback/enums_test.go`

- [ ] **Step 1: 编写 enums.go**

```go
package callback

// ──────────────────────────── 枚举 ────────────────────────────

// FilterAction 过滤器动作，控制回调是否执行。
//
// 对应 Python: openjiuwen/core/runner/callback/enums.py (FilterAction)
type FilterAction string

const (
	// FilterActionContinue 正常执行
	FilterActionContinue FilterAction = "continue"
	// FilterActionStop 停止整个事件处理（不再执行后续回调）
	FilterActionStop FilterAction = "stop"
	// FilterActionSkip 跳过当前回调，继续下一个
	FilterActionSkip FilterAction = "skip"
	// FilterActionModify 修改参数后继续执行
	FilterActionModify FilterAction = "modify"
)

// ChainAction 链式执行动作，控制回调链流程。
//
// 对应 Python: openjiuwen/core/runner/callback/enums.py (ChainAction)
type ChainAction string

const (
	// ChainActionContinue 继续下一个回调
	ChainActionContinue ChainAction = "continue"
	// ChainActionBreak 中断链，返回当前结果
	ChainActionBreak ChainAction = "break"
	// ChainActionRetry 重试当前回调
	ChainActionRetry ChainAction = "retry"
	// ChainActionRollback 回滚所有已执行回调
	ChainActionRollback ChainAction = "rollback"
)

// HookType 生命周期钩子类型。
//
// 对应 Python: openjiuwen/core/runner/callback/enums.py (HookType)
type HookType string

const (
	// HookTypeBefore 事件处理前
	HookTypeBefore HookType = "before"
	// HookTypeAfter 事件处理后
	HookTypeAfter HookType = "after"
	// HookTypeError 出错时
	HookTypeError HookType = "error"
	// HookTypeCleanup 清理阶段
	HookTypeCleanup HookType = "cleanup"
)
```

- [ ] **Step 2: 编写 enums_test.go**

```go
package callback

import "testing"

func TestFilterAction_值验证(t *testing.T) {
	if FilterActionContinue != "continue" {
		t.Errorf("FilterActionContinue = %q, want %q", FilterActionContinue, "continue")
	}
	if FilterActionStop != "stop" {
		t.Errorf("FilterActionStop = %q, want %q", FilterActionStop, "stop")
	}
	if FilterActionSkip != "skip" {
		t.Errorf("FilterActionSkip = %q, want %q", FilterActionSkip, "skip")
	}
	if FilterActionModify != "modify" {
		t.Errorf("FilterActionModify = %q, want %q", FilterActionModify, "modify")
	}
}

func TestChainAction_值验证(t *testing.T) {
	if ChainActionContinue != "continue" {
		t.Errorf("ChainActionContinue = %q, want %q", ChainActionContinue, "continue")
	}
	if ChainActionBreak != "break" {
		t.Errorf("ChainActionBreak = %q, want %q", ChainActionBreak, "break")
	}
	if ChainActionRetry != "retry" {
		t.Errorf("ChainActionRetry = %q, want %q", ChainActionRetry, "retry")
	}
	if ChainActionRollback != "rollback" {
		t.Errorf("ChainActionRollback = %q, want %q", ChainActionRollback, "rollback")
	}
}

func TestHookType_值验证(t *testing.T) {
	if HookTypeBefore != "before" {
		t.Errorf("HookTypeBefore = %q, want %q", HookTypeBefore, "before")
	}
	if HookTypeAfter != "after" {
		t.Errorf("HookTypeAfter = %q, want %q", HookTypeAfter, "after")
	}
	if HookTypeError != "error" {
		t.Errorf("HookTypeError = %q, want %q", HookTypeError, "error")
	}
	if HookTypeCleanup != "cleanup" {
		t.Errorf("HookTypeCleanup = %q, want %q", HookTypeCleanup, "cleanup")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestFilterAction|TestChainAction|TestHookType" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/callback/enums.go internal/agentcore/runner/callback/enums_test.go
git commit -m "feat(callback): 新建 enums.go，定义 FilterAction/ChainAction/HookType 枚举（6.24）"
```

---

### Task 2: 新建 errors.go + errors_test.go

**Files:**
- Create: `internal/agentcore/runner/callback/errors.go`
- Create: `internal/agentcore/runner/callback/errors_test.go`

- [ ] **Step 1: 编写 errors.go**

```go
package callback

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbortError 回调执行中止错误，在回调内部触发以中止整个 trigger 流程。
//
// 对应 Python: openjiuwen/core/runner/callback/errors.py (AbortError)
//
// 传播逻辑（与 Python 对齐）：
//   - Cause != nil → trigger 返回 Cause（对调用方透明，AbortError 仅作为包装器）
//   - Cause == nil → trigger 返回 AbortError 本身
//
// 在 triggerCallbacks 中的处理：
//   1. 记录指标（is_error=True）
//   2. 熔断器记录失败
//   3. 执行 ERROR 钩子（传入 Cause ?? AbortError）
//   4. 日志记录
//   5. 如果 Cause != nil → 返回 (nil, Cause)
//   6. 如果 Cause == nil → 返回 (nil, AbortError)
type AbortError struct {
	// base 内嵌 BaseError，复用异常体系
	base *exception.BaseError
	// Reason 中止原因
	Reason string
	// Cause 原始错误（非 nil 时，trigger 重新抛出 Cause 而非 AbortError）
	Cause error
	// Details 额外详情
	Details any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbortError 创建中止错误。
//
// 对应 Python: AbortError(reason, cause=cause, details=details)
func NewAbortError(reason string, cause error) *AbortError {
	var baseOpts []exception.Option
	baseOpts = append(baseOpts, exception.WithMsg(reason))
	if cause != nil {
		baseOpts = append(baseOpts, exception.WithCause(cause))
	}
	return &AbortError{
		base:   exception.BuildError(exception.StatusCallbackExecutionAborted, baseOpts...),
		Reason: reason,
		Cause:  cause,
	}
}

// NewAbortErrorWithDetails 创建带详情的中止错误。
func NewAbortErrorWithDetails(reason string, cause error, details any) *AbortError {
	ae := NewAbortError(reason, cause)
	ae.Details = details
	return ae
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *AbortError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("callback execution aborted: %s (caused by %v)", e.Reason, e.Cause)
	}
	return fmt.Sprintf("callback execution aborted: %s", e.Reason)
}

// Unwrap 支持 errors.Unwrap/is/As 链。
func (e *AbortError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return e.base
}
```

- [ ] **Step 2: 编写 errors_test.go**

```go
package callback

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewAbortError_无Cause(t *testing.T) {
	ae := NewAbortError("test reason", nil)
	if ae.Reason != "test reason" {
		t.Errorf("Reason = %q, want %q", ae.Reason, "test reason")
	}
	if ae.Cause != nil {
		t.Errorf("Cause = %v, want nil", ae.Cause)
	}
	wantMsg := "callback execution aborted: test reason"
	if ae.Error() != wantMsg {
		t.Errorf("Error() = %q, want %q", ae.Error(), wantMsg)
	}
}

func TestNewAbortError_有Cause(t *testing.T) {
	innerErr := fmt.Errorf("inner error")
	ae := NewAbortError("test reason", innerErr)
	if ae.Cause != innerErr {
		t.Errorf("Cause = %v, want %v", ae.Cause, innerErr)
	}
	wantMsg := "callback execution aborted: test reason (caused by inner error)"
	if ae.Error() != wantMsg {
		t.Errorf("Error() = %q, want %q", ae.Error(), wantMsg)
	}
}

func TestAbortError_errorsAs(t *testing.T) {
	ae := NewAbortError("abort", nil)
	var target *AbortError
	if !errors.As(ae, &target) {
		t.Errorf("errors.As(AbortError, *AbortError) = false, want true")
	}
}

func TestAbortError_Unwrap_有Cause(t *testing.T) {
	innerErr := fmt.Errorf("inner")
	ae := NewAbortError("abort", innerErr)
	unwrapped := errors.Unwrap(ae)
	if unwrapped != innerErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, innerErr)
	}
}

func TestNewAbortErrorWithDetails(t *testing.T) {
	ae := NewAbortErrorWithDetails("reason", nil, map[string]int{"k": 1})
	if ae.Details == nil {
		t.Errorf("Details = nil, want non-nil")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestNewAbortError|TestAbortError" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/callback/errors.go internal/agentcore/runner/callback/errors_test.go
git commit -m "feat(callback): 新建 errors.go，实现 AbortError 中止错误（6.24）"
```

---

### Task 3: 新建 models.go + models_test.go，合并 CallbackInfo[F]

**Files:**
- Create: `internal/agentcore/runner/callback/models.go`
- Create: `internal/agentcore/runner/callback/models_test.go`
- Delete: `internal/agentcore/runner/callback/callback_info.go`
- Delete: `internal/agentcore/runner/callback/callback_info_test.go`

- [ ] **Step 1: 编写 models.go**

将 `callback_info.go` 中的 `CallbackInfo[F]` 和 `sortCallbacks` 移入，新增 `CallbackMetrics`、`FilterResult`、`ChainContext`、`ChainResult`。

```go
package callback

import (
	"sort"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CallbackMetrics 回调执行指标，记录调用次数、耗时、错误率。
// 并发安全：内部使用 sync.Mutex 保护所有字段。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (CallbackMetrics)
type CallbackMetrics struct {
	mu           sync.Mutex
	// CallCount 调用次数
	CallCount int
	// TotalTime 总耗时（秒）
	TotalTime float64
	// MinTime 最小耗时（秒）
	MinTime float64
	// MaxTime 最大耗时（秒）
	MaxTime float64
	// ErrorCount 错误次数
	ErrorCount int
	// LastCallTime 最后调用时间
	LastCallTime time.Time
}

// FilterResult 过滤器返回结果。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (FilterResult)
type FilterResult struct {
	// Action 过滤器动作
	Action FilterAction
	// ModifiedData 修改后数据（仅 FilterActionModify 时使用）
	ModifiedData any
	// Reason 原因说明
	Reason string
}

// ChainContext 链式执行上下文。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (ChainContext)
type ChainContext struct {
	// Event 事件名
	Event string
	// InitialData 初始数据
	InitialData any
	// Results 各回调执行结果
	Results []any
	// Metadata 元数据
	Metadata map[string]any
	// CurrentIndex 当前执行的回调索引
	CurrentIndex int
	// IsCompleted 是否已完成
	IsCompleted bool
	// IsRolledBack 是否已回滚
	IsRolledBack bool
	// StartTime 开始时间
	StartTime time.Time
}

// ChainResult 链式执行结果。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (ChainResult)
type ChainResult struct {
	// Action 链执行动作
	Action ChainAction
	// Result 最终结果
	Result any
	// Context 执行上下文
	Context *ChainContext
	// Error 执行错误
	Error error
}

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

// ──────────────────────────── 导出函数 ────────────────────────────

// Update 记录一次回调执行。
//
// 对应 Python: CallbackMetrics.update(execution_time, is_error)
func (m *CallbackMetrics) Update(executionTime float64, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallCount++
	m.TotalTime += executionTime
	if m.CallCount == 1 || executionTime < m.MinTime {
		m.MinTime = executionTime
	}
	if executionTime > m.MaxTime {
		m.MaxTime = executionTime
	}
	if isError {
		m.ErrorCount++
	}
	m.LastCallTime = time.Now()
}

// AvgTime 平均执行时间（秒）。
//
// 对应 Python: CallbackMetrics.avg_time
func (m *CallbackMetrics) AvgTime() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CallCount == 0 {
		return 0
	}
	return m.TotalTime / float64(m.CallCount)
}

// ToDict 序列化为 map。
//
// 对应 Python: CallbackMetrics.to_dict()
func (m *CallbackMetrics) ToDict() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"call_count":     m.CallCount,
		"total_time":     m.TotalTime,
		"min_time":       m.MinTime,
		"max_time":       m.MaxTime,
		"error_count":    m.ErrorCount,
		"avg_time":       m.AvgTime(),
		"last_call_time": m.LastCallTime,
	}
}

// GetLastResult 获取最后一个结果。
func (c *ChainContext) GetLastResult() any {
	if len(c.Results) == 0 {
		return nil
	}
	return c.Results[len(c.Results)-1]
}

// ElapsedTime 已耗时。
func (c *ChainContext) ElapsedTime() time.Duration {
	return time.Since(c.StartTime)
}

// SetMetadata 设置元数据。
func (c *ChainContext) SetMetadata(key string, value any) {
	if c.Metadata == nil {
		c.Metadata = make(map[string]any)
	}
	c.Metadata[key] = value
}

// GetMetadata 获取元数据。
func (c *ChainContext) GetMetadata(key string) (any, bool) {
	if c.Metadata == nil {
		return nil, false
	}
	v, ok := c.Metadata[key]
	return v, ok
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

- [ ] **Step 2: 编写 models_test.go**

测试 CallbackMetrics 并发安全、FilterResult 构造、ChainContext/ChainResult 方法。

```go
package callback

import (
	"sync"
	"testing"
	"time"
)

func TestCallbackMetrics_Update(t *testing.T) {
	m := &CallbackMetrics{}
	m.Update(0.1, false)
	m.Update(0.3, true)
	m.Update(0.2, false)
	if m.CallCount != 3 {
		t.Errorf("CallCount = %d, want 3", m.CallCount)
	}
	if m.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", m.ErrorCount)
	}
	if m.MinTime != 0.1 {
		t.Errorf("MinTime = %f, want 0.1", m.MinTime)
	}
	if m.MaxTime != 0.3 {
		t.Errorf("MaxTime = %f, want 0.3", m.MaxTime)
	}
}

func TestCallbackMetrics_AvgTime(t *testing.T) {
	m := &CallbackMetrics{}
	if m.AvgTime() != 0 {
		t.Errorf("空指标 AvgTime = %f, want 0", m.AvgTime())
	}
	m.Update(0.2, false)
	m.Update(0.4, false)
	if m.AvgTime() != 0.3 {
		t.Errorf("AvgTime = %f, want 0.3", m.AvgTime())
	}
}

func TestCallbackMetrics_ToDict(t *testing.T) {
	m := &CallbackMetrics{}
	m.Update(0.5, false)
	d := m.ToDict()
	if d["call_count"] != 1 {
		t.Errorf("call_count = %v, want 1", d["call_count"])
	}
}

func TestCallbackMetrics_并发安全(t *testing.T) {
	m := &CallbackMetrics{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Update(0.01, false)
		}()
	}
	wg.Wait()
	if m.CallCount != 100 {
		t.Errorf("CallCount = %d, want 100", m.CallCount)
	}
}

func TestFilterResult_构造(t *testing.T) {
	fr := FilterResult{Action: FilterActionSkip, Reason: "rate limited"}
	if fr.Action != FilterActionSkip {
		t.Errorf("Action = %v, want %v", fr.Action, FilterActionSkip)
	}
}

func TestChainContext_GetLastResult(t *testing.T) {
	c := &ChainContext{Results: []any{"a", "b", "c"}}
	if c.GetLastResult() != "c" {
		t.Errorf("GetLastResult = %v, want c", c.GetLastResult())
	}
	empty := &ChainContext{}
	if empty.GetLastResult() != nil {
		t.Errorf("空 Results GetLastResult = %v, want nil", empty.GetLastResult())
	}
}

func TestChainContext_ElapsedTime(t *testing.T) {
	c := &ChainContext{StartTime: time.Now()}
	d := c.ElapsedTime()
	if d < 0 {
		t.Errorf("ElapsedTime = %v, want >= 0", d)
	}
}

func TestChainContext_Metadata(t *testing.T) {
	c := &ChainContext{}
	c.SetMetadata("key", "value")
	v, ok := c.GetMetadata("key")
	if !ok || v != "value" {
		t.Errorf("GetMetadata = %v, %v; want value, true", v, ok)
	}
	_, ok = c.GetMetadata("missing")
	if ok {
		t.Errorf("GetMetadata(missing) = true, want false")
	}
}

func TestChainResult_构造(t *testing.T) {
	cr := &ChainResult{Action: ChainActionBreak, Result: "done"}
	if cr.Action != ChainActionBreak {
		t.Errorf("Action = %v, want %v", cr.Action, ChainActionBreak)
	}
}
```

- [ ] **Step 3: 删除 callback_info.go 和 callback_info_test.go**

```bash
rm internal/agentcore/runner/callback/callback_info.go
rm internal/agentcore/runner/callback/callback_info_test.go
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestCallbackMetrics|TestFilterResult|TestChainContext|TestChainResult" -v`
Expected: PASS（同时验证 CallbackInfo 编译通过，无 import 循环）

- [ ] **Step 5: 提交**

```bash
git add -A internal/agentcore/runner/callback/
git commit -m "feat(callback): 新建 models.go 合并 CallbackInfo，新增 CallbackMetrics/FilterResult/ChainContext/ChainResult（6.24）"
```

---

### Task 4: 补充 events.go — scope 工具函数 + 缺失域枚举

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`
- Modify: `internal/agentcore/runner/callback/events_test.go`

- [ ] **Step 1: 在 events.go 顶部（常量区块）新增 scope 工具函数和 EventBase**

```go
// ──────────────────────────── 常量 ────────────────────────────

// DefaultScope 默认作用域，与 Python DEFAULT_SCOPE 一致。
const DefaultScope = "_framework"

// BuildEventName 构建带 scope 的事件名。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py build_event_name(scope, event_name)
func BuildEventName(scope, eventName string) string {
	return scope + ":" + eventName
}

// ParseEventName 解析带 scope 的事件名，返回 (scope, eventName)。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py parse_event_name(scoped_event)
func ParseEventName(scopedEvent string) (scope, eventName string) {
	for i := 0; i < len(scopedEvent); i++ {
		if scopedEvent[i] == ':' {
			return scopedEvent[:i], scopedEvent[i+1:]
		}
	}
	return DefaultScope, scopedEvent
}

// EventBase 事件基类，提供 scope 支持。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (EventBase)
type EventBase struct {
	// Scope 作用域
	Scope string
}

// GetEvent 获取带 scope 的完整事件名。
//
// 对应 Python: EventBase.get_event(event_name)
func (e *EventBase) GetEvent(eventName string) string {
	return BuildEventName(e.Scope, eventName)
}
```

- [ ] **Step 2: 新增 5 个缺失域的枚举、EventData、回调函数类型**

在 events.go 枚举区块末尾追加：

```go
// WorkflowEventType 工作流事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (WorkflowEvents)
type WorkflowEventType string

const (
	WorkflowStarted             WorkflowEventType = "_framework:workflow_started"
	WorkflowFinished            WorkflowEventType = "_framework:workflow_finished"
	WorkflowError               WorkflowEventType = "_framework:workflow_error"
	WorkflowCancelled           WorkflowEventType = "_framework:workflow_cancelled"
	WorkflowNodeExecuted        WorkflowEventType = "_framework:node_executed"
	WorkflowNodeError           WorkflowEventType = "_framework:node_error"
	WorkflowEdgeTraversed       WorkflowEventType = "_framework:edge_traversed"
	WorkflowLoopStarted         WorkflowEventType = "_framework:loop_started"
	WorkflowLoopFinished        WorkflowEventType = "_framework:loop_finished"
	WorkflowInvokeInput         WorkflowEventType = "_framework:workflow_invoke_input"
	WorkflowInvokeOutput        WorkflowEventType = "_framework:workflow_invoke_output"
	WorkflowStreamInput         WorkflowEventType = "_framework:workflow_stream_input"
	WorkflowStreamOutput        WorkflowEventType = "_framework:workflow_stream_output"
	WorkflowComponentBatchInput  WorkflowEventType = "_framework:component_batch_input"
	WorkflowComponentBatchOutput WorkflowEventType = "_framework:component_batch_output"
	WorkflowComponentStreamInput  WorkflowEventType = "_framework:component_stream_input"
)

// AgentTeamEventType AgentTeam 事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentTeamEvents)
type AgentTeamEventType string

const (
	AgentTeamP2PReceived    AgentTeamEventType = "_framework:agent_p2p_received"
	AgentTeamPubsubReceived  AgentTeamEventType = "_framework:agent_pubsub_received"
)

// RetrievalEventType 检索事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (RetrievalEvents)
type RetrievalEventType string

const (
	RetrievalStarted RetrievalEventType = "_framework:retrieval_started"
)

// MemoryEventType 记忆事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (MemoryEvents)
type MemoryEventType string

const (
	MemoryAdded          MemoryEventType = "_framework:memory_added"
	MemoryUpdated        MemoryEventType = "_framework:memory_updated"
	MemoryDeleted        MemoryEventType = "_framework:memory_deleted"
	MemorySearchStarted  MemoryEventType = "_framework:memory_search_started"
	MemorySearchFinished MemoryEventType = "_framework:memory_search_finished"
)

// TaskManagerEventType 任务管理事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (TaskManagerEvents)
type TaskManagerEventType string

const (
	TaskCreated   TaskManagerEventType = "_framework:task_created"
	TaskRunning   TaskManagerEventType = "_framework:task_running"
	TaskCompleted TaskManagerEventType = "_framework:task_completed"
	TaskFailed    TaskManagerEventType = "_framework:task_failed"
	TaskCancelled TaskManagerEventType = "_framework:task_cancelled"
	TaskTimeout   TaskManagerEventType = "_framework:task_timeout"
)
```

新增各域 EventData 和回调函数类型：

```go
// WorkflowEventData 工作流事件数据。
type WorkflowEventData struct {
	Event      WorkflowEventType
	WorkflowID string
	NodeID     string
	Inputs     map[string]any
	Result     any
	Error      error
	Extra      map[string]any
}

// AgentTeamEventData AgentTeam 事件数据。
type AgentTeamEventData struct {
	Event   AgentTeamEventType
	AgentID string
	Message any
	Extra   map[string]any
}

// RetrievalEventData 检索事件数据。
type RetrievalEventData struct {
	Event    RetrievalEventType
	Query    string
	Results  any
	Extra    map[string]any
}

// MemoryEventData 记忆事件数据。
type MemoryEventData struct {
	Event    MemoryEventType
	Key      string
	Value    any
	Extra    map[string]any
}

// TaskManagerEventData 任务管理事件数据。
type TaskManagerEventData struct {
	Event    TaskManagerEventType
	TaskID   string
	Status   string
	Result   any
	Error    error
	Extra    map[string]any
}

// WorkflowCallbackFunc 工作流回调函数类型。
type WorkflowCallbackFunc func(ctx context.Context, data *WorkflowEventData) any

// AgentTeamCallbackFunc AgentTeam 回调函数类型。
type AgentTeamCallbackFunc func(ctx context.Context, data *AgentTeamEventData) any

// RetrievalCallbackFunc 检索回调函数类型。
type RetrievalCallbackFunc func(ctx context.Context, data *RetrievalEventData) any

// MemoryCallbackFunc 记忆回调函数类型。
type MemoryCallbackFunc func(ctx context.Context, data *MemoryEventData) any

// TaskManagerCallbackFunc 任务管理回调函数类型。
type TaskManagerCallbackFunc func(ctx context.Context, data *TaskManagerEventData) any
```

- [ ] **Step 3: 在 events_test.go 追加 scope 工具函数和新增域枚举的测试**

```go
func TestBuildEventName(t *testing.T) {
	got := BuildEventName("agent", "started")
	if got != "agent:started" {
		t.Errorf("BuildEventName = %q, want %q", got, "agent:started")
	}
}

func TestParseEventName(t *testing.T) {
	scope, name := ParseEventName("_framework:llm_call_started")
	if scope != "_framework" || name != "llm_call_started" {
		t.Errorf("ParseEventName = %q, %q; want _framework, llm_call_started", scope, name)
	}
	scope2, name2 := ParseEventName("no_colon")
	if scope2 != "_framework" || name2 != "no_colon" {
		t.Errorf("ParseEventName(无冒号) = %q, %q; want _framework, no_colon", scope2, name2)
	}
}

func TestEventBase_GetEvent(t *testing.T) {
	eb := EventBase{Scope: "workflow"}
	got := eb.GetEvent("started")
	if got != "workflow:started" {
		t.Errorf("GetEvent = %q, want %q", got, "workflow:started")
	}
}

func TestWorkflowEventType_值验证(t *testing.T) {
	if WorkflowStarted != "_framework:workflow_started" {
		t.Errorf("WorkflowStarted = %q", WorkflowStarted)
	}
}

func TestMemoryEventType_值验证(t *testing.T) {
	if MemoryAdded != "_framework:memory_added" {
		t.Errorf("MemoryAdded = %q", MemoryAdded)
	}
}

func TestTaskManagerEventType_值验证(t *testing.T) {
	if TaskCreated != "_framework:task_created" {
		t.Errorf("TaskCreated = %q", TaskCreated)
	}
}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestBuildEventName|TestParseEventName|TestEventBase|TestWorkflowEventType|TestMemoryEventType|TestTaskManagerEventType" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/runner/callback/events.go internal/agentcore/runner/callback/events_test.go
git commit -m "feat(callback): 补充 events.go scope 工具函数 + Workflow/AgentTeam/Retrieval/Memory/TaskManager 域枚举（6.24）"
```

---

### Task 5: 新建 filters.go + filters_test.go

**Files:**
- Create: `internal/agentcore/runner/callback/filters.go`
- Create: `internal/agentcore/runner/callback/filters_test.go`

- [ ] **Step 1: 编写 filters.go**

包含 EventFilter 接口 + 7 种过滤器实现。代码较长，此处列出核心结构：

- `EventFilter` 接口：`Name() string` + `Filter(ctx, event, callbackName string, data any) FilterResult`
- `RateLimitFilter`：maxCalls/timeWindow + 滑动窗口(deque) + mu
- `CircuitBreakerFilter`：failureThreshold/timeout + failures/isOpen/lastFailureTime + mu + RecordSuccess/RecordFailure
- `ValidationFilter`：validator func(any) bool
- `LoggingFilter`：结构化日志，始终返回 CONTINUE
- `AuthFilter`：requiredRole string，从 data 中检查 role
- `ParamModifyFilter`：modifier func(any) any，返回 MODIFY
- `ConditionalFilter`：condition func(ctx, event, callbackName string, data any) bool + actionOnFalse FilterAction

- [ ] **Step 2: 编写 filters_test.go**

测试 7 种过滤器的 Filter 逻辑，覆盖 CONTINUE/STOP/SKIP/MODIFY 四种动作。

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestRateLimit|TestCircuitBreaker|TestValidation|TestLogging|TestAuth|TestParamModify|TestConditional" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/callback/filters.go internal/agentcore/runner/callback/filters_test.go
git commit -m "feat(callback): 新建 filters.go，实现 EventFilter 接口 + 7 种过滤器（6.24）"
```

---

### Task 6: 新建 chain.go + chain_test.go

**Files:**
- Create: `internal/agentcore/runner/callback/chain.go`
- Create: `internal/agentcore/runner/callback/chain_test.go`

- [ ] **Step 1: 编写 chain.go**

包含 CallbackChain 结构体 + Add/Remove/Execute/Rollback 方法。Execute 实现优先级排序执行、数据流传递、Break/Retry/Rollback 控制、超时(context.WithTimeout)、重试(maxRetries+retryDelay)、错误处理。

- [ ] **Step 2: 编写 chain_test.go**

测试顺序执行、优先级、Break/Retry/Rollback、超时、错误处理、回滚逆序执行。

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestCallbackChain" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/callback/chain.go internal/agentcore/runner/callback/chain_test.go
git commit -m "feat(callback): 新建 chain.go，实现 CallbackChain 顺序执行+回滚+重试+错误处理（6.24）"
```

---

### Task 7: 新建 utils.go + utils_test.go，移出全局单例

**Files:**
- Create: `internal/agentcore/runner/callback/utils.go`
- Create: `internal/agentcore/runner/callback/utils_test.go`
- Modify: `internal/agentcore/runner/callback/framework.go`

- [ ] **Step 1: 编写 utils.go**

```go
package callback

import "context"

// ──────────────────────────── 全局变量 ────────────────────────────

// globalCallbackFramework 全局回调框架单例。
//
// 对应 Python: Runner.callback_framework（Runner 初始化时创建的全局单例）
var globalCallbackFramework = NewCallbackFramework()

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCallbackFramework 返回全局回调框架单例。
//
// 对应 Python: openjiuwen/core/runner/callback/utils.py get_callback_framework()
func GetCallbackFramework() *CallbackFramework {
	return globalCallbackFramework
}

// Trigger 便捷触发函数，触发自定义事件。
//
// 对应 Python: openjiuwen/core/runner/callback/utils.py trigger(event, **kwargs)
func Trigger(ctx context.Context, event string, data map[string]any) []any {
	return globalCallbackFramework.TriggerCustom(ctx, event, data)
}
```

- [ ] **Step 2: 从 framework.go 删除 globalCallbackFramework 和 GetCallbackFramework**

删除 framework.go 中的：
- `var globalCallbackFramework = NewCallbackFramework()`
- `func GetCallbackFramework() *CallbackFramework { ... }`

- [ ] **Step 3: 编写 utils_test.go**

```go
package callback

import (
	"context"
	"testing"
)

func TestGetCallbackFramework_非nil(t *testing.T) {
	fw := GetCallbackFramework()
	if fw == nil {
		t.Errorf("GetCallbackFramework() = nil, want non-nil")
	}
}

func TestGetCallbackFramework_单例(t *testing.T) {
	fw1 := GetCallbackFramework()
	fw2 := GetCallbackFramework()
	if fw1 != fw2 {
		t.Errorf("GetCallbackFramework() 返回不同实例")
	}
}

func TestTrigger_便捷函数(t *testing.T) {
	var called bool
	fw := GetCallbackFramework()
	fw.OnCustom("test_trigger_util", func(ctx context.Context, data map[string]any) any {
		called = true
		return nil
	})
	Trigger(context.Background(), "test_trigger_util", nil)
	if !called {
		t.Errorf("Trigger 未触发回调")
	}
	fw.OffAllCustom("test_trigger_util")
}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -run "TestGetCallbackFramework|TestTrigger" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/runner/callback/utils.go internal/agentcore/runner/callback/utils_test.go internal/agentcore/runner/callback/framework.go
git commit -m "feat(callback): 新建 utils.go 移出全局单例和 Trigger 便捷函数（6.24）"
```

---

### Task 8: 删除 logging.go + 清理 framework.go 注册

**Files:**
- Delete: `internal/agentcore/runner/callback/logging.go`
- Delete: `internal/agentcore/runner/callback/logging_test.go`
- Modify: `internal/agentcore/runner/callback/framework.go`
- Modify: `internal/agentcore/runner/callback/framework_test.go`

- [ ] **Step 1: 删除 logging.go 和 logging_test.go**

```bash
rm internal/agentcore/runner/callback/logging.go
rm internal/agentcore/runner/callback/logging_test.go
```

- [ ] **Step 2: 从 framework.go NewCallbackFramework 删除 9 行 LoggingLLMCallback 注册**

删除 L147-155 的 `fw.OnLLM(*, LoggingLLMCallback)` 调用，同时更新注释。

- [ ] **Step 3: 修复 framework_test.go 中引用 LoggingLLMCallback 的测试**

调整以下测试用例：
- L175: `TestLoggingLLMCallback_日志回调` — 删除
- L193: 引用 `LoggingLLMCallback` 的断言 — 改为不依赖 LoggingLLMCallback
- L273/L814/L871: 注释中提及 LoggingLLMCallback — 更新注释

- [ ] **Step 4: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A internal/agentcore/runner/callback/
git commit -m "refactor(callback): 删除 logging.go 集中化日志，对齐 Python llm_logger 内联模式（6.24）"
```

---

### Task 9: framework.go 新增字段 + 回填 triggerCallbacks 8 个占位符

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go`

- [ ] **Step 1: 在 CallbackFramework 结构体新增字段**

```go
// hooks 生命周期钩子（事件名 → HookType → 钩子函数列表）
hooks map[string]map[HookType][]HookFunc
// filters 事件级过滤器（事件名 → 过滤器列表）
filters map[string][]EventFilter
// globalFilters 全局过滤器
globalFilters []EventFilter
// callbackFilters 回调级过滤器（回调函数指针 → 过滤器列表）
callbackFilters map[any][]EventFilter
// metrics 执行指标（"{event}:{callbackName}" → CallbackMetrics）
metrics map[string]*CallbackMetrics
// circuitBreakers 熔断器（"{event}:{callbackName}" → *CircuitBreakerFilter）
circuitBreakers map[string]*CircuitBreakerFilter
// chains 回调链（事件名 → CallbackChain）
chains map[string]*CallbackChain
// enableEventHistory 是否启用事件历史
enableEventHistory bool
// eventHistory 事件历史记录（环形缓冲区，最大 1000 条）
eventHistory []eventHistoryEntry
// enableMetrics 是否启用指标
enableMetrics bool
```

新增钩子函数类型：
```go
// HookFunc 生命周期钩子函数类型
type HookFunc func(ctx context.Context, event string, data any)
```

- [ ] **Step 2: 在 NewCallbackFramework 中初始化新字段**

- [ ] **Step 3: 新增 AddFilter/AddGlobalFilter/AddCircuitBreaker/AddHook 方法**

- [ ] **Step 4: 新增 TriggerChain/TriggerParallel/TriggerUntil/TriggerWithTimeout 方法**

- [ ] **Step 5: 新增 GetMetrics/ResetMetrics/GetSlowCallbacks/EnableEventHistory/GetEventHistory/GetStatistics/SaveState 方法**

- [ ] **Step 6: 回填 triggerCallbacks 中的 8 个占位符**

将 L835-L870 的 `⤵️ 回填` 注释替换为实际实现：
- L835: `executeHooks(ctx, string(event), HookTypeBefore, data)` 
- L846: 三级过滤器管线 `applyFilters(ctx, string(event), info, data)`
- L847: 熔断器检查 `checkCircuitBreaker(string(event), info)`
- L848: 超时控制 `context.WithTimeout(ctx, time.Duration(info.Timeout)*time.Second)`
- L849: 重试循环 `for retry := 0; retry <= info.MaxRetries; retry++`
- L854: `executeHooks(ctx, string(event), HookTypeError, data)`
- L855: `updateMetrics(eventKey, executionTime, true)`
- L862: `updateMetrics(eventKey, executionTime, false)`
- L870: `executeHooks(ctx, string(event), HookTypeAfter, data)`

同时加入 AbortError 检测逻辑。

- [ ] **Step 7: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -v`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/runner/callback/framework.go
git commit -m "feat(callback): 回填 triggerCallbacks 8 个占位符，新增钩子/过滤器/熔断器/指标/链式触发方法（6.24）"
```

---

### Task 10: framework.go 新增域 On/Off/Trigger 方法

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go`

- [ ] **Step 1: 新增 Workflow 域 On/Off/Trigger 方法**

`OnWorkflow` / `OffWorkflow` / `TriggerWorkflow`，模式与现有 OnLLM/OffLLM/TriggerLLM 一致。

- [ ] **Step 2: 新增 AgentTeam/Retrieval/Memory/TaskManager 域方法**

同样模式：`On{Domain}` / `Off{Domain}` / `Trigger{Domain}`。

- [ ] **Step 3: 在 CallbackFramework 结构体新增各域 callbacks map 字段**

```go
workflowCallbacks    map[WorkflowEventType][]*CallbackInfo[WorkflowCallbackFunc]
agentTeamCallbacks   map[AgentTeamEventType][]*CallbackInfo[AgentTeamCallbackFunc]
retrievalCallbacks   map[RetrievalEventType][]*CallbackInfo[RetrievalCallbackFunc]
memoryCallbacks      map[MemoryEventType][]*CallbackInfo[MemoryCallbackFunc]
taskManagerCallbacks map[TaskManagerEventType][]*CallbackInfo[TaskManagerCallbackFunc]
```

- [ ] **Step 4: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/runner/callback/framework.go
git commit -m "feat(callback): 新增 Workflow/AgentTeam/Retrieval/Memory/TaskManager 域 On/Off/Trigger 方法（6.24）"
```

---

### Task 11: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/runner/callback/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录和核心类型索引**

更新为：

```
文件目录：

	callback/
	├── doc.go                # 包文档
	├── framework.go          # CallbackFramework 核心（注册/触发/注销/钩子/指标/熔断器/历史）
	├── enums.go              # FilterAction / ChainAction / HookType 枚举
	├── models.go             # CallbackMetrics / FilterResult / ChainContext / ChainResult / CallbackInfo[F]
	├── events.go             # 事件类型定义（scope + 所有域枚举 + EventData + 函数类型）
	├── filters.go            # EventFilter 接口 + 7 种过滤器
	├── chain.go              # CallbackChain（顺序执行+回滚+重试）
	├── errors.go             # AbortError
	├── utils.go              # 全局单例 + Trigger 便捷函数
	└── options.go            # CallbackOption（Functional Options 模式）
```

核心类型索引更新为包含所有新增导出类型。

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/runner/callback/doc.go
git commit -m "docs(callback): 更新 doc.go 文件目录和核心类型索引（6.24）"
```

---

### Task 12: 更新 framework_test.go 覆盖回填逻辑

**Files:**
- Modify: `internal/agentcore/runner/callback/framework_test.go`

- [ ] **Step 1: 新增 BEFORE/AFTER 钩子测试**

```go
func TestTriggerLLM_钩子执行(t *testing.T) {
    // 注册 BEFORE/AFTER 钩子，验证触发顺序
}
```

- [ ] **Step 2: 新增过滤器管线测试**

```go
func TestTriggerLLM_过滤器管线(t *testing.T) {
    // 注册全局+事件级+回调级过滤器，验证三级执行顺序
}
```

- [ ] **Step 3: 新增熔断器测试**

```go
func TestTriggerLLM_熔断器(t *testing.T) {
    // 连续失败后熔断，验证 SKIP；超时后重置
}
```

- [ ] **Step 4: 新增超时和重试测试**

```go
func TestTriggerLLM_超时控制(t *testing.T) { ... }
func TestTriggerLLM_重试逻辑(t *testing.T) { ... }
```

- [ ] **Step 5: 新增 AbortError 传播测试**

```go
func TestTriggerLLM_AbortError_无Cause(t *testing.T) { ... }
func TestTriggerLLM_AbortError_有Cause(t *testing.T) { ... }
```

- [ ] **Step 6: 新增指标记录测试**

```go
func TestTriggerLLM_指标记录(t *testing.T) { ... }
```

- [ ] **Step 7: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/ -v -cover`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/runner/callback/framework_test.go
git commit -m "test(callback): 补充 framework_test.go 回填逻辑测试（钩子/过滤器/熔断器/超时/重试/AbortError/指标）（6.24）"
```

---

### Task 13: 更新 IMPLEMENTATION_PLAN.md 状态标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 6.24 行状态从 ☐ 改为 ✅**

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 6.24 状态为 ✅"
```
