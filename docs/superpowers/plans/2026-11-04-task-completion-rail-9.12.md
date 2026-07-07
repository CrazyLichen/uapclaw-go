# 9.12 TaskCompletionRail 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TaskCompletionRail，完成 DeepAgent 任务循环的"终止信号管理"能力，并回填 deep_agent.go 中 5 处占位代码。

**Architecture:** TaskCompletionRail 嵌入 DeepAgentRail，覆盖 3 个生命周期钩子（BeforeModelCall / BeforeTaskIteration / AfterTaskIteration），使用 Functional Options 模式构造。与 ProgressiveToolRail 同属 rails 包，遵循相同的编码规范和 GetCallbacks 覆盖模式。

**Tech Stack:** Go 1.22+, 标准库 regexp, 已有的 task_loop.StopConditionEvaluator 体系, harness/prompts/sections 提示词节

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `internal/agentcore/harness/rails/task_completion.go` | TaskCompletionRail 结构体 + 3钩子 + BuildEvaluators + 辅助函数 |
| `internal/agentcore/harness/rails/task_completion_test.go` | 单元测试 |
| `internal/agentcore/harness/rails/doc.go` | 更新文件目录 |
| `internal/agentcore/harness/interfaces/deep_agent.go` | LoopCoordinatorInterface 扩展 |
| `internal/agentcore/harness/deep_agent.go` | 5处回填 |

---

### Task 1: LoopCoordinatorInterface 扩展

**Files:**
- Modify: `internal/agentcore/harness/interfaces/deep_agent.go:3-9,48-53`

- [ ] **Step 1: 在 LoopCoordinatorInterface 添加 GetCompletionPromiseEvaluator 方法**

修改 `internal/agentcore/harness/interfaces/deep_agent.go`：

1. 在 import 块中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"`
2. 在 `LoopCoordinatorInterface` 接口中添加方法：

```go
type LoopCoordinatorInterface interface {
	// Iteration 返回当前迭代次数
	Iteration() int
	// RequestAbort 请求中止循环
	RequestAbort()
	// GetCompletionPromiseEvaluator 返回第一个 CompletionPromiseEvaluator（可能为 nil）
	// 对齐 Python: LoopCoordinator.get_completion_promise_evaluator
	GetCompletionPromiseEvaluator() *task_loop.CompletionPromiseEvaluator
}
```

- [ ] **Step 2: 验证 *task_loop.LoopCoordinator 仍满足接口**

运行: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`

因为 `*task_loop.LoopCoordinator` 已有 `GetCompletionPromiseEvaluator() *CompletionPromiseEvaluator` 方法（loop_coordinator.go:237），它自动满足扩展后的接口。

- [ ] **Step 3: 检查 DeepAgent.LoopCoordinator() 返回值兼容**

`DeepAgent.LoopCoordinator()` 返回类型是 `hinterfaces.LoopCoordinatorInterface`，`*task_loop.LoopCoordinator` 满足此接口，无需修改 getter。

运行: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/interfaces/deep_agent.go
git commit -m "feat(9.12): 扩展 LoopCoordinatorInterface 添加 GetCompletionPromiseEvaluator"
```

---

### Task 2: 新建 task_completion.go — 结构体 + 构造函数 + BuildEvaluators

**Files:**
- Create: `internal/agentcore/harness/rails/task_completion.go`

- [ ] **Step 1: 创建 task_completion.go 文件**

```go
package rails

import (
	"context"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskCompletionRail 任务完成检测 Rail。
//
// 负责三件事：
//  1. BeforeModelCall:  向系统提示词注入 <promise> 完成信号节
//  2. BeforeTaskIteration: 首轮迭代时将 taskInstruction 模板应用到查询
//  3. AfterTaskIteration:  检测输出中的 <promise>...</promise> 标签，通知 CompletionPromiseEvaluator
//
// 仅当 enable_task_loop=True 时生效。
// 对齐 Python: TaskCompletionRail (openjiuwen/harness/rails/task_completion_rail.py)
type TaskCompletionRail struct {
	DeepAgentRail
	// taskInstruction 带 {query} 占位符的格式模板，首轮迭代时应用到查询
	taskInstruction string
	// completionPromise 模型须在 <promise>...</promise> 标签内输出以宣告完成的令牌
	completionPromise string
	// requiredConfirmations 需要连续确认的次数
	requiredConfirmations int
	// allowPromiseDetails 是否允许 promise 块包含额外详情
	allowPromiseDetails bool
	// maxRounds 外层循环最大轮数（0 = 不限）
	maxRounds int
	// timeoutSeconds 整个任务循环的墙钟超时秒数（0 = 不限）
	timeoutSeconds float64
	// extraEvaluators 额外自定义评估器
	extraEvaluators []task_loop.StopConditionEvaluator
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskCompletionRailPriority TaskCompletionRail 优先级
	// 对齐 Python: TaskCompletionRail.priority = 10
	taskCompletionRailPriority = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 TaskCompletionRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*TaskCompletionRail)(nil)

// taskCompLogComponent 日志组件标识
var taskCompLogComponent = logger.ComponentAgentCore

// promiseTagPattern 匹配 <promise>...</promise> 标签的正则
var promiseTagPattern = regexp.MustCompile(`(?i)(?s)<promise>\s*(.*?)\s*</promise>`)

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskCompletionOption TaskCompletionRail 构造选项函数。
type TaskCompletionOption func(*TaskCompletionRail)

// WithTaskInstruction 设置任务指令模板。
func WithTaskInstruction(template string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.taskInstruction = template }
}

// WithCompletionPromise 设置完成承诺令牌。
func WithCompletionPromise(promise string) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.completionPromise = promise }
}

// WithRequiredConfirmations 设置连续确认次数。
func WithRequiredConfirmations(n int) TaskCompletionOption {
	return func(r *TaskCompletionRail) {
		if n < 1 {
			n = 1
		}
		r.requiredConfirmations = n
	}
}

// WithAllowPromiseDetails 设置是否允许 promise 块含详情。
func WithAllowPromiseDetails(allow bool) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.allowPromiseDetails = allow }
}

// WithMaxRounds 设置外层循环最大轮数。
func WithMaxRounds(n int) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.maxRounds = n }
}

// WithTimeoutSeconds 设置墙钟超时秒数。
func WithTimeoutSeconds(sec float64) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.timeoutSeconds = sec }
}

// WithExtraEvaluators 设置额外自定义评估器。
func WithExtraEvaluators(evaluators ...task_loop.StopConditionEvaluator) TaskCompletionOption {
	return func(r *TaskCompletionRail) { r.extraEvaluators = evaluators }
}

// NewTaskCompletionRail 创建 TaskCompletionRail 实例。
//
// 默认无参创建，与 Python TaskCompletionRail() 对齐。
// 用户通过 opts 传入策略参数。
// 对齐 Python: TaskCompletionRail.__init__()
func NewTaskCompletionRail(opts ...TaskCompletionOption) *TaskCompletionRail {
	r := &TaskCompletionRail{
		DeepAgentRail:        *NewDeepAgentRail(),
		requiredConfirmations: 1,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.WithPriority(taskCompletionRailPriority)
	return r
}

// BuildEvaluators 根据Rail参数构建评估器链。
//
// 内置评估器顺序：MaxRounds → Timeout → CompletionPromise，
// 然后追加用户传入的额外评估器。
// 对齐 Python: TaskCompletionRail.build_evaluators()
func (r *TaskCompletionRail) BuildEvaluators() []task_loop.StopConditionEvaluator {
	var result []task_loop.StopConditionEvaluator
	if r.maxRounds > 0 {
		result = append(result, task_loop.NewMaxRoundsEvaluator(r.maxRounds))
	}
	if r.timeoutSeconds > 0 {
		result = append(result, task_loop.NewTimeoutEvaluator(r.timeoutSeconds))
	}
	if r.completionPromise != "" {
		result = append(result, task_loop.NewCompletionPromiseEvaluator(
			r.completionPromise, r.requiredConfirmations,
		))
	}
	result = append(result, r.extraEvaluators...)
	return result
}

// BeforeModelCall 在每次 LLM 调用前注入完成信号提示词节。
//
// 对齐 Python: TaskCompletionRail.before_model_call()
func (r *TaskCompletionRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.completionPromise == "" {
		return nil
	}

	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return nil
	}

	lang := builder.Language()
	section := sections.BuildCompletionSignalSection(r.completionPromise, lang)
	builder.AddSection(section)

	logger.Debug(taskCompLogComponent).
		Str("event_type", "task_completion_before_model_call").
		Str("completion_promise", r.completionPromise).
		Msg("已注入完成信号提示词节")
	return nil
}

// BeforeTaskIteration 在首轮迭代时将 taskInstruction 模板应用到查询。
//
// 仅在第一轮（非 follow-up）迭代时格式化查询。
// 对齐 Python: TaskCompletionRail.before_task_iteration()
func (r *TaskCompletionRail) BeforeTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.taskInstruction == "" {
		return nil
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return nil
	}
	if inputs.Query == "" {
		return nil
	}
	if inputs.IsFollowUp {
		return nil
	}

	inputs.Query = strings.ReplaceAll(r.taskInstruction, "{query}", inputs.Query)

	logger.Debug(taskCompLogComponent).
		Str("event_type", "task_completion_before_task_iteration").
		Msg("已格式化首轮任务指令")
	return nil
}

// AfterTaskIteration 在迭代完成后检测输出中的 promise 标签。
//
// 匹配成功则通知 CompletionPromiseEvaluator，使外层循环在下一轮停止。
// 对齐 Python: TaskCompletionRail.after_task_iteration()
func (r *TaskCompletionRail) AfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.completionPromise == "" {
		return nil
	}

	content := extractOutput(cbc)
	if content == "" {
		return nil
	}

	promiseBlock := ExtractPromiseBlock(content)
	if promiseBlock == "" {
		return nil
	}

	matched := normalizePromiseText(promiseBlock)
	expected := normalizePromiseText(r.completionPromise)

	if matched != expected {
		if !r.allowPromiseDetails {
			return nil
		}
		if !PromiseMatches(promiseBlock, r.completionPromise) {
			return nil
		}
		matched = expected
	}

	logger.Info(taskCompLogComponent).
		Str("event_type", "task_completion_promise_fulfilled").
		Str("matched", matched).
		Msg("任务完成承诺已满足")

	notifyEvaluator(cbc, matched)
	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeModelCall + BeforeTaskIteration + AfterTaskIteration。
//
// 对齐 Python: TaskCompletionRail 隐式覆盖 before_model_call + before_task_iteration + after_task_iteration
func (r *TaskCompletionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.BeforeTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizePromiseText 折叠空白字符用于 promise 比较。
// 对齐 Python: _normalize(text)
func normalizePromiseText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// ExtractPromiseBlock 用正则提取 <promise>...</promise> 内容。
// 导出供测试使用。
// 对齐 Python: extract_promise_block(text)
func ExtractPromiseBlock(text string) string {
	if text == "" {
		return ""
	}
	match := promiseTagPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

// PromiseMatches 判断 promise 块首行是否以期望令牌开头。
// 导出供测试使用。
// 对齐 Python: promise_matches(block, expected)
func PromiseMatches(block string, expected string) bool {
	if block == "" || expected == "" {
		return false
	}

	expectedNorm := normalizePromiseText(expected)

	// 取块的首行
	lines := strings.Split(block, "\n")
	firstLine := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			firstLine = trimmed
			break
		}
	}
	if firstLine == "" {
		return false
	}

	firstNorm := normalizePromiseText(firstLine)
	if firstNorm == expectedNorm {
		return true
	}
	return strings.HasPrefix(firstNorm, expectedNorm+" ")
}

// extractOutput 从 TaskIterationInputs.Result["output"] 提取输出文本。
// 对齐 Python: TaskCompletionRail._extract_output(ctx)
func extractOutput(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return ""
	}
	result := inputs.Result
	if result == nil {
		return ""
	}
	output, _ := result["output"]
	if output == nil {
		return ""
	}
	s, ok := output.(string)
	if !ok {
		return fmt.Sprintf("%v", output)
	}
	return s
}

// notifyEvaluator 获取 LoopCoordinator → CompletionPromiseEvaluator → NotifyFulfilled。
// 对齐 Python: TaskCompletionRail._notify_evaluator(ctx, text)
func notifyEvaluator(cbc *agentinterfaces.AgentCallbackContext, text string) {
	agent := cbc.Agent()

	// 通过 DeepAgentInterface 获取 LoopCoordinator
	deepAgent, ok := agent.(interface {
		LoopCoordinator() interface {
			GetCompletionPromiseEvaluator() *task_loop.CompletionPromiseEvaluator
		}
	})
	if !ok {
		return
	}
	coord := deepAgent.LoopCoordinator()
	if coord == nil {
		return
	}
	ev := coord.GetCompletionPromiseEvaluator()
	if ev == nil {
		return
	}
	ev.NotifyFulfilled(text)
}
```

注意：`extractOutput` 中使用了 `fmt.Sprintf`，需要在 import 中添加 `"fmt"`。

修正 import 块：

```go
import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 2: 验证编译通过**

运行: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/task_completion.go
git commit -m "feat(9.12): 新建 TaskCompletionRail 结构体 + 构造函数 + BuildEvaluators + 3钩子"
```

---

### Task 3: 新建 task_completion_test.go — 辅助函数测试

**Files:**
- Create: `internal/agentcore/harness/rails/task_completion_test.go`

- [ ] **Step 1: 创建测试文件 — 辅助函数测试**

```go
package rails

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNormalizePromiseText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello world", normalizePromiseText("  hello   world  "))
	assert.Equal(t, "abc", normalizePromiseText("abc"))
	assert.Equal(t, "", normalizePromiseText(""))
	assert.Equal(t, "a b c", normalizePromiseText("a\n\tb\nc"))
}

func TestExtractPromiseBlock(t *testing.T) {
	t.Parallel()

	// 正常提取
	assert.Equal(t, "done", ExtractPromiseBlock("<promise>done</promise>"))
	assert.Equal(t, "task complete", ExtractPromiseBlock("<promise>task complete</promise>"))

	// 带空白
	assert.Equal(t, "done", ExtractPromiseBlock("<promise>  done  </promise>"))

	// 多行内容
	assert.Equal(t, "done\ndetails here", ExtractPromiseBlock("<promise>done\ndetails here</promise>"))

	// 无 promise 标签
	assert.Equal(t, "", ExtractPromiseBlock("no promise here"))
	assert.Equal(t, "", ExtractPromiseBlock(""))

	// 大小写不敏感
	assert.Equal(t, "done", ExtractPromiseBlock("<PROMISE>done</PROMISE>"))
}

func TestPromiseMatches(t *testing.T) {
	t.Parallel()

	// 精确匹配
	assert.True(t, PromiseMatches("done", "done"))

	// 首行匹配
	assert.True(t, PromiseMatches("done extra details", "done"))

	// 空值
	assert.False(t, PromiseMatches("", "done"))
	assert.False(t, PromiseMatches("done", ""))

	// 不匹配
	assert.False(t, PromiseMatches("other", "done"))

	// 首行后跟空格+详情
	assert.True(t, PromiseMatches("done\nextra details", "done"))
}
```

- [ ] **Step 2: 运行测试**

运行: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -run "TestNormalizePromiseText|TestExtractPromiseBlock|TestPromiseMatches" -v`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/task_completion_test.go
git commit -m "test(9.12): 添加 TaskCompletionRail 辅助函数测试"
```

---

### Task 4: 补充 task_completion_test.go — 构造函数 + BuildEvaluators 测试

**Files:**
- Modify: `internal/agentcore/harness/rails/task_completion_test.go`

- [ ] **Step 1: 添加构造函数和 BuildEvaluators 测试**

在 `task_completion_test.go` 末尾追加：

```go
func TestNewTaskCompletionRail_默认构造(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	assert.Equal(t, 10, r.Priority())
	assert.Equal(t, "", r.taskInstruction)
	assert.Equal(t, "", r.completionPromise)
	assert.Equal(t, 1, r.requiredConfirmations)
	assert.False(t, r.allowPromiseDetails)
	assert.Equal(t, 0, r.maxRounds)
	assert.Equal(t, float64(0), r.timeoutSeconds)
	assert.Nil(t, r.extraEvaluators)
}

func TestNewTaskCompletionRail_带选项(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(
		WithTaskInstruction("请完成：{query}"),
		WithCompletionPromise("task_done"),
		WithRequiredConfirmations(3),
		WithAllowPromiseDetails(true),
		WithMaxRounds(10),
		WithTimeoutSeconds(300),
	)
	assert.Equal(t, "请完成：{query}", r.taskInstruction)
	assert.Equal(t, "task_done", r.completionPromise)
	assert.Equal(t, 3, r.requiredConfirmations)
	assert.True(t, r.allowPromiseDetails)
	assert.Equal(t, 10, r.maxRounds)
	assert.Equal(t, float64(300), r.timeoutSeconds)
}

func TestWithRequiredConfirmations_最小值(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithRequiredConfirmations(0))
	assert.Equal(t, 1, r.requiredConfirmations)
}

func TestBuildEvaluators_全参数(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(
		WithMaxRounds(5),
		WithTimeoutSeconds(60),
		WithCompletionPromise("done"),
	)
	evaluators := r.BuildEvaluators()
	assert.Len(t, evaluators, 3)
	assert.Equal(t, "MaxRoundsEvaluator", evaluators[0].Name())
	assert.Equal(t, "TimeoutEvaluator", evaluators[1].Name())
	assert.Equal(t, "CompletionPromiseEvaluator", evaluators[2].Name())
}

func TestBuildEvaluators_仅MaxRounds(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithMaxRounds(5))
	evaluators := r.BuildEvaluators()
	assert.Len(t, evaluators, 1)
	assert.Equal(t, "MaxRoundsEvaluator", evaluators[0].Name())
}

func TestBuildEvaluators_仅CompletionPromise(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	evaluators := r.BuildEvaluators()
	assert.Len(t, evaluators, 1)
	assert.Equal(t, "CompletionPromiseEvaluator", evaluators[0].Name())
}

func TestBuildEvaluators_默认无参(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	evaluators := r.BuildEvaluators()
	assert.Len(t, evaluators, 0)
}

func TestBuildEvaluators_含额外评估器(t *testing.T) {
	t.Parallel()

	custom := task_loop.NewCustomPredicateEvaluator("custom", func(_ task_loop.StopEvaluationContext) bool { return false })
	r := NewTaskCompletionRail(
		WithMaxRounds(5),
		WithExtraEvaluators(custom),
	)
	evaluators := r.BuildEvaluators()
	assert.Len(t, evaluators, 2)
	assert.Equal(t, "MaxRoundsEvaluator", evaluators[0].Name())
	assert.Equal(t, "custom", evaluators[1].Name())
}
```

注意需要在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"`。

- [ ] **Step 2: 运行测试**

运行: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -run "TestNewTaskCompletion|TestWithRequired|TestBuildEvaluators" -v`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/task_completion_test.go
git commit -m "test(9.12): 添加 TaskCompletionRail 构造函数和 BuildEvaluators 测试"
```

---

### Task 5: 补充 task_completion_test.go — GetCallbacks + 钩子测试

**Files:**
- Modify: `internal/agentcore/harness/rails/task_completion_test.go`

- [ ] **Step 1: 添加 GetCallbacks 测试**

在 `task_completion_test.go` 末尾追加：

```go
func TestTaskCompletionRail_GetCallbacks(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	callbacks := r.GetCallbacks()

	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeTaskIteration)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}
```

注意需要在 import 中添加 `agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"`。

- [ ] **Step 2: 运行测试**

运行: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -run TestTaskCompletionRail_GetCallbacks -v`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/task_completion_test.go
git commit -m "test(9.12): 添加 TaskCompletionRail GetCallbacks 测试"
```

---

### Task 6: 回填 deep_agent.go — 5处占位

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go:77-78,1264-1268,1974-1978,1994-1998,2335-2339`

- [ ] **Step 1: R1 — 字段类型改为具体类型**

将 L77-78:
```go
// taskCompletionRail 任务完成 Rail
// ⤵️ 9.11 回填：TaskCompletionRail 具体类型
taskCompletionRail agentinterfaces.AgentRail
```
改为:
```go
// taskCompletionRail 任务完成 Rail
// ⤴️ 9.12 回填：TaskCompletionRail 具体类型
taskCompletionRail *rails.TaskCompletionRail
```

- [ ] **Step 2: R2 — 取消注释 TaskCompletionRail 创建**

将 L1264-1268:
```go
if config.EnableTaskLoop {
	// ⤵️ 9.11 回填：TaskCompletionRail 创建
	// d.pendingRails = append(d.pendingRails, NewTaskCompletionRail())
	logger.Debug(logComponent).Msg("TaskCompletionRail 待创建，⤵️ 9.11 回填")
}
```
改为:
```go
if config.EnableTaskLoop {
	// ⤴️ 9.12 回填：TaskCompletionRail 创建
	d.pendingRails = append(d.pendingRails, rails.NewTaskCompletionRail())
	logger.Debug(logComponent).Msg("TaskCompletionRail 已创建，⤴️ 9.12 回填")
}
```

- [ ] **Step 3: R3 — 第一处 evaluators 构建**

将 L1974-1978:
```go
var evaluators []task_loop.StopConditionEvaluator
if taskCompRail != nil {
	// ⤵️ 9.11 回填：taskCompletionRail.buildEvaluators()
	evaluators = []task_loop.StopConditionEvaluator{}
}
```
改为:
```go
var evaluators []task_loop.StopConditionEvaluator
if taskCompRail != nil {
	// ⤴️ 9.12 回填：taskCompletionRail.BuildEvaluators()
	evaluators = taskCompRail.BuildEvaluators()
}
```

- [ ] **Step 4: R4 — 第二处 evaluators 构建**

将 L1994-1998（同上模式的另一处）:
```go
var evaluators []task_loop.StopConditionEvaluator
if taskCompRail != nil {
	// ⤵️ 9.11 回填：taskCompletionRail.buildEvaluators()
	evaluators = []task_loop.StopConditionEvaluator{}
}
```
改为:
```go
var evaluators []task_loop.StopConditionEvaluator
if taskCompRail != nil {
	// ⤴️ 9.12 回填：taskCompletionRail.BuildEvaluators()
	evaluators = taskCompRail.BuildEvaluators()
}
```

- [ ] **Step 5: R5 — isTaskCompletionRail 类型检查改为类型断言**

将 L2335-2339:
```go
// isTaskCompletionRail 判断 Rail 是否为 TaskCompletionRail 类型。
func isTaskCompletionRail(r agentinterfaces.AgentRail) bool {
	// ⤵️ 9.11 回填：TaskCompletionRail 类型检查
	return reflect.TypeOf(r).String() == "TaskCompletionRail"
}
```
改为:
```go
// isTaskCompletionRail 判断 Rail 是否为 TaskCompletionRail 类型。
func isTaskCompletionRail(r agentinterfaces.AgentRail) bool {
	// ⤴️ 9.12 回填：TaskCompletionRail 类型检查
	_, ok := r.(*rails.TaskCompletionRail)
	return ok
}
```

- [ ] **Step 6: RegisterRail 中赋值类型适配**

由于 `d.taskCompletionRail` 类型从 `agentinterfaces.AgentRail` 改为 `*rails.TaskCompletionRail`，L411-412 处的赋值：
```go
if isTaskCompletionRail(r) {
	d.taskCompletionRail = r
}
```
需要改为：
```go
if isTaskCompletionRail(r) {
	d.taskCompletionRail = r.(*rails.TaskCompletionRail)
}
```

- [ ] **Step 7: 检查 reflect 导入是否仍被使用**

如果 R5 是 deep_agent.go 中 `reflect` 包的唯一使用处，回填后需从 import 中移除 `"reflect"`。搜索确认是否有其他 reflect 使用。

- [ ] **Step 8: 验证编译通过**

运行: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...`

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/harness/deep_agent.go
git commit -m "feat(9.12): 回填 deep_agent.go 5处 TaskCompletionRail 占位"
```

---

### Task 7: 更新 rails/doc.go

**Files:**
- Modify: `internal/agentcore/harness/rails/doc.go`

- [ ] **Step 1: 在文件目录中添加 task_completion.go 条目**

将 doc.go 中的文件目录部分：
```
// 文件目录：
//
//	rails/
//	├── doc.go           # 包文档
//	├── base.go          # DeepAgentRail 基类
//	└── progressive.go   # ProgressiveToolRail 渐进式工具发现和可调用工具过滤
```
改为：
```
// 文件目录：
//
//	rails/
//	├── doc.go              # 包文档
//	├── base.go             # DeepAgentRail 基类
//	├── progressive.go      # ProgressiveToolRail 渐进式工具发现和可调用工具过滤
//	└── task_completion.go  # TaskCompletionRail 任务完成检测
```

同时更新包功能概述，在 ProgressiveToolRail 描述后追加：
```
//   - TaskCompletionRail：任务完成检测 Rail（注入完成信号提示、检测 promise 标签、通知 LoopCoordinator 停止循环）
```

- [ ] **Step 2: 验证编译通过**

运行: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/doc.go
git commit -m "docs(9.12): 更新 rails/doc.go 添加 TaskCompletionRail"
```

---

### Task 8: 全量编译验证 + 测试覆盖率

**Files:**
- 无新文件

- [ ] **Step 1: 全量编译**

运行: `cd /home/opensource/uap-claw-go && go build ./...`

- [ ] **Step 2: rails 包测试**

运行: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -v -cover`

- [ ] **Step 3: harness 包测试（含 deep_agent 回填）**

运行: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -cover -count=1`

注意：harness 包测试可能较长，设置较长超时。

- [ ] **Step 4: 确认覆盖率 ≥ 85%**

如果覆盖率不足，补充测试用例。

- [ ] **Step 5: 提交（如有补充测试）**

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 9.12 状态从 ☐ 改为 ✅**

找到行：
```
| 9.12 | ☐ | TaskCompletionRail | 任务完成检测 | openjiuwen/harness/rails/ |
```
改为：
```
| 9.12 | ✅ | TaskCompletionRail | 任务完成检测 | openjiuwen/harness/rails/ |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 9.12 状态为已完成"
```
