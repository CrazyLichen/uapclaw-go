# 8.19 BranchComp / BranchRouter 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `components/flow/branch_comp.py + branch_router.py + condition/{condition,expression}.py`（branch 783 行），落地排他网关组件 + 手写 DSL 表达式求值器。

**Architecture:** 三包落地——`components/condition`（新包：Condition 接口 + 8.10 AtomicNode 链 + 手写词法/递归下降求值器）、`components/flow`（Branch/BranchRouter/BranchComponent）。BranchRouter 实现 `pregel.Router`（Dispatch 直出 TriggerMessage），session 走实例字段捕获（8.5 D2 惯例）。

**Tech Stack:** Go 1.x；复用 8.10 `graph.AtomicInvoke`、8.5 `pregel.Selector/Router`、8.1 `RegisterBranchTargets`、8.17 `WorkflowComponent` 槽位、8.14 tracer 四段工具；错误码 101020/101021/101024/101025 已注册（`codes_workflow.go:82-94` 核对过）。

**Spec:** `docs/superpowers/specs/2026-11-21-branch-comp-8.19-design.md`（三拍板 §3：范围 A / 手写求值器① / drawable 跳过②；偏差 D-19-1/2）

**纪律提醒：**
- 全量回归必须 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
- 单包测试可裸跑 `go test ./internal/openuapclaw/core/workflow/components/...`
- 注释全中文；文件声明顺序：结构体→枚举→常量→全局变量→导出函数→非导出函数
- 提交前 `git status --porcelain` 盘点，禁止 `-am`/`-A` 搭车

---

## 文件结构

| 文件 | 职责 | 对应 Python |
|---|---|---|
| `internal/common/constants/constants.go`（修改） | MAX_COLLECTION_SIZE/MAX_EXPRESSION_LENGTH/MAX_AST_DEPTH | `common/constants/constant.py:25-27` |
| `components/condition/doc.go`（新建） | 包文档 | — |
| `components/condition/condition.go`（新建） | Condition 接口 + BaseCondition + FuncCondition + AlwaysTrue | `condition/condition.py` |
| `components/condition/expression.go`（新建） | ExpressionCondition + convertCondition + parser + evaluator | `condition/expression.py` |
| `components/condition/condition_test.go` / `expression_test.go`（新建） | 单测 | — |
| `components/flow/branch_router.go`（新建） | Branch + BranchRouter（实现 pregel.Router） | `flow/branch_router.py` |
| `components/flow/branch_comp.go`（新建） | BranchComponent | `flow/branch_comp.py` |
| `components/flow/branch_router_test.go` / `branch_comp_test.go`（新建） | 单测 | — |
| `tests/integration/agentcore/workflow_branch_comp/`（新建） | 集成套件 | — |
| `IMPLEMENTATION_PLAN.md`（修改） | 8.19 行 + 十八次注记 + 8.13 标注 | — |

---

### Task 0: 常量前置件

**Files:** Modify `internal/common/constants/`（先 `ls` 确认现有文件；若无此包则新建 `constants.go` + `doc.go`，Python 参考路径 `openjiuwen/core/common/constants/constant.py`）

- [ ] **Step 0.1: 定位常量落点**

```bash
ls internal/common/constants/ 2>/dev/null && grep -rn "INDEX" internal/common/constants/ | head -3
```
若包不存在则新建（含 doc.go，规则 1）；Python `constant.py:25-27` 取值：MAX_COLLECTION_SIZE=100000、MAX_EXPRESSION_LENGTH=5000、MAX_AST_DEPTH=50。

- [ ] **Step 0.2: 写失败测试**（若包已存在且已有测试文件则追加）

```go
func TestMaxConstants_取值对齐Python(t *testing.T) {
	assert.Equal(t, 100000, constants.MaxCollectionSize)
	assert.Equal(t, 5000, constants.MaxExpressionLength)
	assert.Equal(t, 50, constants.MaxASTDepth)
}
```

- [ ] **Step 0.3: 实现**

```go
// MaxCollectionSize 表达式集合尺寸上限（constant.py:25）
const MaxCollectionSize = 100000
// MaxExpressionLength 表达式长度上限（constant.py:26）
const MaxExpressionLength = 5000
// MaxASTDepth 表达式 AST 嵌套深度上限（constant.py:27）
const MaxASTDepth = 50
```

- [ ] **Step 0.4: 测试通过后提交**（显式 add 本任务文件）

```bash
git add internal/common/constants/ && git commit -m "feat(constants): 补齐表达式三护栏常量（8.19 前置件）"
```

---

### Task 1: condition 包——Condition 接口 + FuncCondition + AlwaysTrue

**Files:** Create `components/condition/doc.go`、`condition.go`、`condition_test.go`

关键对齐事实（condition.py）：
- `Condition.__call__` → `atomic_invoke(session=session)`（走 8.10 校验+CommitCmp 链）
- `_atomic_invoke`：`input_schema` 非 None 时 `session.state().get_inputs(schema)`，否则 `inputs={}` → 调 `self.invoke` → 元组分支（**D-19-2 不落**，Go 仅 bool 返回）
- `ExpressionCondition` **覆写了 `__call__`**（直接求值，不走 atomic 链）——见 Task 2
- `trace_info`：FuncCondition=函数名、AlwaysTrue="True"、Condition 基类=""

- [ ] **Step 1.1: 写失败测试**

```go
package condition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSession/session 构造复用 components/testutil 的既有桩——实施时先看
// components/flow/testutil_test.go 的 WorkflowState 会话桩能否搬用（同 repo 内复制粘贴即可，
// 条件包与 flow 包测试桩各自独立，不跨包引用 _test 文件）。
```

测试用例（每个都写全）：

```go
func TestFuncCondition_Call_走原子链返回函数值(t *testing.T) {
	called := false
	c := NewFuncCondition(func() bool { called = true; return true })
	got, err := c.Call(context.Background(), mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
	assert.True(t, called)
}

func TestFuncCondition_TraceInfo_返回函数名(t *testing.T) {
	c := NewFuncCondition(myPredicate)
	assert.Equal(t, "myPredicate", c.TraceInfo(nil))
}

func myPredicate() bool { return true }

func TestAlwaysTrue_Call_恒真(t *testing.T) {
	c := NewAlwaysTrue()
	got, err := c.Call(context.Background(), mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
	assert.Equal(t, "True", c.TraceInfo(nil))
}

func TestCondition_Call_session为nil_报sessionNone(t *testing.T) {
	c := NewAlwaysTrue()
	_, err := c.Call(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session is None")
}

func TestCondition_Call_状态非WorkflowCommitState_报错(t *testing.T) {
	c := NewAlwaysTrue()
	_, err := c.Call(context.Background(), newPlainSession(t)) // state 非 WorkflowCommitState 的桩
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session is not support commit state")
}
```

`mustWorkflowSession`/`newPlainSession` 是测试助手：构造 `session.BaseSession` 桩，其 `State()` 分别返回 `state.WorkflowCommitState` 实现 / 普通对象。参考 `components/flow/testutil_test.go` 既有桩改写。

- [ ] **Step 1.2: 跑测试确认编译失败**

```bash
go test ./internal/openuapclaw/core/workflow/components/condition/
```
Expected: 编译失败（符号未定义）

- [ ] **Step 1.3: 实现 condition.go**

```go
// Package condition 提供分支组件的条件求值件：Condition 抽象 + 表达式条件。
//
// 对应 Python 代码：openjiuwen/core/workflow/components/condition/
// （condition.py 52 行 + expression.py 566 行；number.py/array.py 留 8.20）
package condition

import (
	"context"
	"runtime"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Condition 分支条件契约（Python condition.py:11-32 Condition(AtomicNode) 抽象基类）。
// Call ≙ __call__（→ atomic_invoke）；Invoke ≙ 抽象方法；TraceInfo ≙ trace_info。
type Condition interface {
	// Call 评估条件（Python __call__ → atomic_invoke，走 8.10 校验+CommitCmp 链）
	Call(ctx context.Context, sess session.BaseSession) (bool, error)
	// Invoke 条件求值（Python 抽象方法 invoke(inputs, session)）
	Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error)
	// TraceInfo 追踪信息（Python trace_info：string 或 dict）
	TraceInfo(sess session.BaseSession) any
}

// BaseCondition 条件基座：嵌入 8.10 AtomicNode 承载 atomic_invoke 四步链
// （Python 继承 AtomicNode → Go 嵌入）。invokeImpl 由各子类供应（Python 子类
// invoke 方法 → Go 构造期回调槽，AtomicNode D3 同款）。
// 偏差 D-19-2：_atomic_invoke 的元组分支（isinstance(result, tuple) → set_outputs）
// 生产者 ArrayCondition 属 8.20，本节不落，回收条件挂 8.20。
type BaseCondition struct {
	graph.AtomicNode
	// inputSchema 输入模式（Python __init__(input_schema=None)；本节三子类恒 nil，
	// 8.20 ArrayCondition 传 schema 时启用）
	inputSchema any
}

// NewBaseCondition 构造条件基座。invokeImpl 为 nil 时 panic（Python 抽象方法
// invoke 必须实现，AtomicNode D3 同款）。
func NewBaseCondition(
	invokeImpl func(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error),
	inputSchema any,
) BaseCondition {
	if invokeImpl == nil {
		panic("invokeImpl 不能为 nil（对齐 Python 抽象方法 invoke 必须实现）")
	}
	b := BaseCondition{inputSchema: inputSchema}
	b.AtomicNode = graph.NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		// Python _atomic_invoke：session = kwargs["session"]
		sess, _ := kwargs["session"].(session.BaseSession)
		// Python: inputs = session.state().get_inputs(schema) if schema else {}
		inputs := map[string]any{}
		if b.inputSchema != nil {
			ws := sess.State().(state.WorkflowState)
			inputs = ws.GetInputs(b.inputSchema.(state.StateKey)).(map[string]any)
		}
		// Python: result = self.invoke(inputs=inputs, session=session)
		return invokeImpl(ctx, inputs, sess)
	})
	return b
}

// Call 评估条件（Python __call__：return self.atomic_invoke(session=session)）。
// 原子链返回值断言 bool（条件语义）；非 bool 视为契约违约上抛。
func (b *BaseCondition) Call(ctx context.Context, sess session.BaseSession) (bool, error) {
	res, err := b.AtomicInvoke(ctx, map[string]any{"session": sess})
	if err != nil {
		return false, err
	}
	flag, ok := res.(bool)
	if !ok {
		return false, exception.BuildError(exception.StatusExpressionEvalError,
			exception.WithParam("error_msg", "condition invoke result is not bool"))
	}
	return flag, nil
}

// FuncCondition 函数条件（Python condition.py:35-44）。
type FuncCondition struct {
	BaseCondition
	fn func() bool
}

// NewFuncCondition 构造函数条件。
func NewFuncCondition(fn func() bool) *FuncCondition {
	c := &FuncCondition{fn: fn}
	c.BaseCondition = NewBaseCondition(
		func(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
			return c.fn(), nil
		}, nil)
	return c
}

// Invoke 直调函数（Python invoke: return self._func()）。
func (c *FuncCondition) Invoke(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
	return c.fn(), nil
}

// TraceInfo 返回函数名（Python self._func.__name__ → runtime.FuncForPC，
// 8.1 getCallableName 惯例）。
func (c *FuncCondition) TraceInfo(_ session.BaseSession) any {
	return runtime.FuncForPC(reflect.ValueOf(c.fn).Pointer()).Name()
}

// AlwaysTrue 恒真条件（Python condition.py:47-52）。
type AlwaysTrue struct {
	BaseCondition
}

// NewAlwaysTrue 构造恒真条件。
func NewAlwaysTrue() *AlwaysTrue {
	c := &AlwaysTrue{}
	c.BaseCondition = NewBaseCondition(
		func(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
			return true, nil
		}, nil)
	return c
}

// Invoke 恒真（Python invoke: return True）。
func (c *AlwaysTrue) Invoke(_ context.Context, _ map[string]any, _ session.BaseSession) (bool, error) {
	return true, nil
}

// TraceInfo 返回 "True"（Python 逐字复刻）。
func (c *AlwaysTrue) TraceInfo(_ session.BaseSession) any { return "True" }
```

注意：①`exception.StatusExpressionEvalError`/`StatusExpressionSyntaxError` 常量名实施时以 `codes_workflow.go` 实名为准（grep 确认）；②import 按需增删（`reflect`/`exception`）；③`state.WorkflowState`/`StateKey` 形状以现有定义为准，`GetInputs(schema)` 已核（`workflow_commit_state.go:75`）。

- [ ] **Step 1.4: 跑测试通过**

```bash
go test ./internal/openuapclaw/core/workflow/components/condition/
```
Expected: PASS

- [ ] **Step 1.5: 提交**

```bash
git add internal/openuapclaw/core/workflow/components/condition/
git commit -m "feat(condition): Condition 接口+FuncCondition+AlwaysTrue（8.19 Task1，AtomicNode 首消费 D1 回收）"
```

---

### Task 2: ExpressionCondition——预处理链（构造/RULES/convertCondition/getInputs）

**Files:** Create `components/condition/expression.go`、`expression_test.go`

- [ ] **Step 2.1: 写失败测试（预处理面）**

```go
func TestNewExpressionCondition_超长拒绝(t *testing.T) {
	expr := strings.Repeat("a", 5001)
	_, err := NewExpressionCondition(expr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expression length exceeds maximum")
}

func TestConvertCondition_八条规则(t *testing.T) {
	cases := []struct{ in, want string }{
		{"${a} && ${b}", "${a} and ${b}"},
		{"${a} || ${b}", "${a} or ${b}"},
		{"true && false", "True and False"},
		{"is_false_alarm == true", "is_false_alarm == True"}, // 词边界反例：标识符不被误改
		{"length(${list}) > 0", "len(${list}) > 0"},
		{"${a} not_in ${list}", "${a} not in ${list}"},
		{"is_empty(${list})", "_safe_is_empty(${list})"},
		{"is_not_empty(${list})", "_safe_is_not_empty(${list})"},
		{"'a && b'", "'a && b'"}, // 字符串字面量保护：引号内不被替换
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, convertCondition(tc.in, nil))
	}
}

func TestExpressionCondition_GetInputs_变量提取(t *testing.T) {
	c, err := NewExpressionCondition("${amount} > 100")
	require.NoError(t, err)
	sess := mustWorkflowSessionWithGlobals(t, map[string]any{"amount": 150})
	info := c.TraceInfo(sess).(map[string]any)
	assert.Equal(t, "${amount} > 100", info["bool_expression"])
	assert.Equal(t, map[string]any{"${amount}": 150}, info["inputs"])
}

func TestExpressionCondition_Call_空表达式恒真(t *testing.T) {
	c, err := NewExpressionCondition("")
	require.NoError(t, err)
	got, err := c.Call(context.Background(), mustWorkflowSession(t))
	require.NoError(t, err)
	assert.True(t, got)
}
```

- [ ] **Step 2.2: 跑测试确认编译失败**（同 Task 1 方式）

- [ ] **Step 2.3: 实现 expression.go 预处理段**（parser 段 Task 3 补齐后才编译通过；本任务先落数据面函数，文件同 Task 3 合并提交或本任务内 parser 以 `panic("Task3")` 占位**不允许**——两任务必须同文件顺序实施，本步骤落全文件头 + 以下符号，parser 调用点先留 `evaluateExpression` 前向声明，Task 3 填充后才能编译。为避免断链，Task 2 与 Task 3 的代码步骤合并为一个提交，见 Task 3 Step 3.5）

```go
package condition

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 常量 ────────────────────────────

// rules 表达式预翻译规则（Python RULES，expression.py:15-26 逐条复刻；
// true/false 带词边界——上游注释明确防误伤 is_false_alarm 类标识符）。
var rules = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`&&`), "and"},
	{regexp.MustCompile(`\|\|`), "or"},
	{regexp.MustCompile(`\btrue\b`), "True"},
	{regexp.MustCompile(`\bfalse\b`), "False"},
	{regexp.MustCompile(`length\(`), "len("},
	{regexp.MustCompile(`not_in`), "not in"},
	{regexp.MustCompile(`is_empty\(`), "_safe_is_empty("},
	{regexp.MustCompile(`is_not_empty\(`), "_safe_is_not_empty("},
}

var (
	// varPattern ${...} 变量模式（expression.py:33）
	varPattern = regexp.MustCompile(`\$\{[^}]*\}`)
	// innerVarPattern 变量名内层（expression.py:69）
	innerVarPattern = regexp.MustCompile(`\$\{([^{}]*)\}`)
	// stringPattern 字符串字面量（单/双引号含转义，expression.py:138）
	stringPattern = regexp.MustCompile(`("(?:[^"\\]|\\.)*")|('(?:[^'\\]|\\.)*')`)
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExpressionCondition 表达式条件（Python expression.py:29-64）。
// 覆写 Call：Python 覆写 __call__ 直接求值，**不走 atomic 链**（无 session 校验/
// CommitCmp）——逐字保留该行为差异。
type ExpressionCondition struct {
	BaseCondition
	// expression 原始表达式（运行期 string，每次评估时解析——见 spec §3 拍板①）
	expression string
	// matches 构造期抽出的 ${...} 变量名列表（供 trace_info 用）
	matches []string
}

// NewExpressionCondition 构造表达式条件（Python __init__：存字段 + findall +
// 长度护栏）。
func NewExpressionCondition(expr string) (*ExpressionCondition, error) {
	if len(expr) > constants.MaxExpressionLength {
		return nil, exception.BuildError(exception.StatusExpressionEvalError,
			exception.WithParam("error_msg",
				fmt.Sprintf("expression length exceeds maximum allowed length of %d", constants.MaxExpressionLength)))
	}
	c := &ExpressionCondition{expression: expr, matches: varPattern.FindAllString(expr, -1)}
	c.BaseCondition = NewBaseCondition(c.invokeImpl, nil)
	return c, nil
}

// invokeImpl Python invoke（expression.py:56-59）。
func (c *ExpressionCondition) invokeImpl(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, error) {
	if len(c.expression) == 0 {
		return true, nil
	}
	return c.evaluateExpression(c.expression, c.getInputs(sess))
}

// Call 覆写基座（Python __call__ 覆写：直接求值，不走 atomic 链）。
func (c *ExpressionCondition) Call(_ context.Context, sess session.BaseSession) (bool, error) {
	if len(c.expression) == 0 {
		return true, nil
	}
	return c.evaluateExpression(c.expression, c.getInputs(sess))
}

// TraceInfo 返回 dict（Python expression.py:42-46）。
func (c *ExpressionCondition) TraceInfo(sess session.BaseSession) any {
	return map[string]any{"bool_expression": c.expression, "inputs": c.getInputs(sess)}
}

// getInputs 变量取值（expression.py:48-54：空表达式或 session nil 返回 {}，
// 否则 session.state().get_global(match[2:-1])）。
func (c *ExpressionCondition) getInputs(sess session.BaseSession) map[string]any {
	if len(c.expression) == 0 || sess == nil {
		return map[string]any{}
	}
	inputs := map[string]any{}
	ws := sess.State().(state.WorkflowState)
	for _, m := range c.matches {
		inputs[m] = ws.GetGlobal(state.StateKey(m[2 : len(m)-1]))
	}
	return inputs
}

// convertCondition 预翻译（expression.py:131-161 逐字复刻：字面量保护 → RULES → 还原）。
func convertCondition(condition string, _ map[string]any) string {
	var stringLiterals []string
	protected := stringPattern.ReplaceAllStringFunc(condition, func(s string) string {
		idx := len(stringLiterals)
		stringLiterals = append(stringLiterals, s)
		return fmt.Sprintf("__STRING_LITERAL_%d__", idx)
	})
	for _, r := range rules {
		protected = r.pattern.ReplaceAllString(protected, r.replacement)
	}
	for i, lit := range stringLiterals {
		protected = strings.ReplaceAll(protected,
			fmt.Sprintf("__STRING_LITERAL_%d__", i), lit)
	}
	return protected
}

// safeLen 安全长度（expression.py:164-173：None→0，非序列报错）。
func safeLen(value any) (int, error) {
	if value == nil {
		return 0, nil
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.String, reflect.Map, reflect.Chan:
		return v.Len(), nil
	}
	return 0, exception.BuildError(exception.StatusExpressionEvalError,
		exception.WithParam("error_msg",
			fmt.Sprintf("len() operate on non-sequence value of type %s", typeName(value))))
}

// safeIsEmpty 安全判空（expression.py:176-196）。
func safeIsEmpty(value any) (bool, error) {
	if value == nil {
		return true, nil
	}
	switch value.(type) {
	case int, int64, float64, bool:
		return false, exception.BuildError(exception.StatusExpressionEvalError,
			exception.WithParam("error_msg",
				fmt.Sprintf("cannot check emptiness of %s type", typeName(value))))
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.String, reflect.Map:
		if v.Len() > constants.MaxCollectionSize {
			return false, exception.BuildError(exception.StatusExpressionEvalError,
				exception.WithParam("error_msg",
					fmt.Sprintf("collection size exceeds maximum allowed size of %d", constants.MaxCollectionSize)))
		}
		return v.Len() == 0, nil
	}
	return false, nil // 无 __len__ → False（Python: if not hasattr(value, "__len__"): return False）
}

// safeIsNotEmpty 安全判非空（expression.py:199-201）。
func safeIsNotEmpty(value any) (bool, error) {
	empty, err := safeIsEmpty(value)
	return !empty, err
}

// typeName 值类型名（Python type(value).__name__ 等价面）。
func typeName(value any) string {
	return reflect.TypeOf(value).Name()
}
```

- [ ] **Step 2.4: 本任务不单独跑测试**（evaluateExpression 未实现不能编译），与 Task 3 合并验证后统一提交

---

### Task 3: ExpressionCondition——手写 parser + evaluator

**Files:** Modify `components/condition/expression.go`（追加）、`expression_test.go`（追加全语料）

- [ ] **Step 3.1: 写失败测试（求值面全语料）**

```go
func evalOK(t *testing.T, expr string, globals map[string]any, want bool) {
	t.Helper()
	c, err := NewExpressionCondition(expr)
	require.NoError(t, err)
	sess := mustWorkflowSessionWithGlobals(t, globals)
	got, err := c.Call(context.Background(), sess)
	require.NoError(t, err, "expr=%q", expr)
	assert.Equal(t, want, got, "expr=%q", expr)
}

func evalErr(t *testing.T, expr string, globals map[string]any, msgPart string) {
	t.Helper()
	c, err := NewExpressionCondition(expr)
	require.NoError(t, err)
	sess := mustWorkflowSessionWithGlobals(t, globals)
	_, err = c.Call(context.Background(), sess)
	require.Error(t, err, "expr=%q", expr)
	assert.Contains(t, err.Error(), msgPart)
}

func TestEval_比较与逻辑(t *testing.T) {
	g := map[string]any{"a": 150, "b": "hello", "list": []any{1, 2, 3}}
	evalOK(t, "${a} > 100", g, true)
	evalOK(t, "${a} > 100 && ${b} == 'hello'", g, true)
	evalOK(t, "${a} > 100 || ${a} < 50", g, true)
	evalOK(t, "not ${a} < 100", g, true)
	evalOK(t, "${a} in ${list}", g, true)      // 需 list 含 150 → 改 g: list 含 150
	evalOK(t, "${a} not_in ${list}", g, true)
	evalOK(t, "${a} >= 150 and ${a} <= 150", g, true) // RULES 不翻译 and/or 原生写法
	evalOK(t, "100 < ${a} < 200", g, true)     // 链式比较
	evalOK(t, "true", nil, true)
	evalOK(t, "1 + 1 == 2", nil, true)
	evalOK(t, "2 ** 10 == 1024", nil, true)
	evalOK(t, "7 // 2 == 3", nil, true)
	evalOK(t, "'x' * 2 == 'xx'", nil, true)
}

func TestEval_调用与容器(t *testing.T) {
	g := map[string]any{"list": []any{1, 2, 3}, "dict": map[string]any{"k": "v"}, "s": "abc"}
	evalOK(t, "len(${list}) == 3", g, true)
	evalOK(t, "is_empty(${list}) == False", g, true)
	evalOK(t, "is_not_empty(${s})", g, true)
	evalOK(t, "${dict}['k'] == 'v'", g, true)
	evalOK(t, "${dict}.k == 'v'", g, true)     // dict 属性回落（getattr→dict access）
	evalOK(t, "${list}[1] == 2", g, true)
	evalOK(t, "${s}[0:2] == 'ab'", g, true)    // 切片
	evalOK(t, "len([1, 2]) == 2", g, true)     // List 节点
	evalOK(t, "len({'a': 1, 'b': 2}) == 2", g, true) // Dict 节点
}

func TestEval_Name四级解析(t *testing.T) {
	evalOK(t, "var_0 == 5", map[string]any{"${var_0}": 5}, true) // runtime（变量改名后）
	evalOK(t, "True and not False", nil, true)
	evalErr(t, "undefined_var == 1", nil, "is not defined")
}

func TestEval_护栏(t *testing.T) {
	big := make([]any, 100001)
	evalErr(t, "is_empty(${big})", map[string]any{"big": big}, "collection size exceeds maximum")
	evalErr(t, "'__class__' and ${o}.__class__", map[string]any{"o": map[string]any{}}, "disallowed operation")
	evalErr(t, "2 ** 101 > 0", nil, "exponent too large")
	evalErr(t, "${a} +", map[string]any{"a": 1}, "syntax") // 语法错误→EXPRESSION_SYNTAX_ERROR
	evalErr(t, "len(123)", nil, "len() operate on non-sequence")
	evalErr(t, "is_empty(123)", nil, "cannot check emptiness")
}
```

- [ ] **Step 3.2: 实现 evaluator 段（追加到 expression.go）**——完整代码如下，含词法/递归下降 parser/求值器/护栏：

```go
// ──────────────────────────── AST 节点对照表 ────────────────────────────
// Python ast.BoolOp→boolOp / ast.Compare→compare / ast.BinOp→binOp /
// ast.UnaryOp→unaryOp / ast.Name→name / ast.Constant→constant /
// ast.Subscript→subscript / ast.Attribute→attr / ast.Call→call /
// ast.List→listNode / ast.Tuple→tupleNode / ast.Dict→dictNode。
// spec: docs/superpowers/specs/2026-11-21-branch-comp-8.19-design.md §4.2

// ──────────────────────────── 结构体 ────────────────────────────

// exprNode 求值器 AST 节点统一接口。
type exprNode interface{ node() }

type boolOpNode struct {
	isAnd bool
	values []exprNode
}
type compareNode struct {
	first exprNode
	ops   []compareOp   // 与 comparators 逐对 zip（Python Compare 多链）
	rights []exprNode
}
type compareOp struct{ kind string } // == != < <= > >= is "is not" in "not in"
type binOpNode struct {
	kind string // + - * / % ** // << >> | ^ &
	left, right exprNode
}
type unaryOpNode struct {
	kind     string // - + not ~
	operand  exprNode
}
type nameNode struct{ id string }
type constantNode struct{ value any }
type subscriptNode struct {
	value exprNode
	index exprNode        // Index 形态
	slice *sliceNode     // Slice 形态
}
type sliceNode struct{ lower, upper, step exprNode } // 三者可 nil
type attrNode struct {
	value exprNode
	attr  string
}
type callNode struct {
	fn   exprNode
	args []exprNode
}
type listNode struct{ elts []exprNode }
type tupleNode struct{ elts []exprNode }
type dictNode struct{ keys, values []exprNode }

func (n *boolOpNode) node()      {}
func (n *compareNode) node()     {}
func (n *binOpNode) node()       {}
func (n *unaryOpNode) node()     {}
func (n *nameNode) node()        {}
func (n *constantNode) node()    {}
func (n *subscriptNode) node()   {}
func (n *attrNode) node()        {}
func (n *callNode) node()        {}
func (n *listNode) node()        {}
func (n *tupleNode) node()       {}
func (n *dictNode) node()        {}

// ──────────────────────────── 词法 ────────────────────────────

type tokenKind int

const (
	tokNum  tokenKind = iota
	tokStr
	tokName
	tokOp // 运算符/分隔符
)

type token struct {
	kind tokenKind
	text string
	// num 数值字面量（int 解析失败回落 float，对齐 Python 字面量双轨）
	num  any
}

var opTexts = []string{
	"**", "//", "==", "!=", "<=", ">=", "<<", ">>",
	"+", "-", "*", "/", "%", "<", ">", "(", ")", "[", "]", "{", "}",
	",", ":", ".", "=",
}

func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	isIdentStart := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	isIdentPart := func(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'' || c == '"':
			end := i + 1
			for end < len(s) {
				if s[end] == '\\' { end += 2; continue }
				if s[end] == c { break }
				end++
			}
			if end >= len(s) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			lit, err := strconv.Unquote(strconv.Quote(s[i+1 : end]))
			if err != nil { lit = s[i+1 : end] }
			toks = append(toks, token{kind: tokStr, text: lit})
			i = end + 1
		case isDigit(c):
			j := i
			for j < len(s) && (isDigit(s[j]) || s[j] == '.') { j++ }
			text := s[i:j]
			if iv, err := strconv.ParseInt(text, 10, 64); err == nil {
				toks = append(toks, token{kind: tokNum, num: int(iv)})
			} else {
				fv, _ := strconv.ParseFloat(text, 64)
				toks = append(toks, token{kind: tokNum, num: fv})
			}
			i = j
		case isIdentStart(c):
			j := i
			for j < len(s) && isIdentPart(s[j]) { j++ }
			toks = append(toks, token{kind: tokName, text: s[i:j]})
			i = j
		default:
			matched := false
			for _, op := range opTexts {
				if strings.HasPrefix(s[i:], op) {
					toks = append(toks, token{kind: tokOp, text: op})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("invalid character %q", c)
			}
		}
	}
	return toks, nil
}

// ──────────────────────────── parser ────────────────────────────

type exprParser struct {
	toks []token
	pos  int
	// depth 构造深度（Python _check_ast_depth 的等价承载：parser 期即拦，
	// 超过 MAX_AST_DEPTH 报 EXPRESSION_EVAL_ERROR）
	depth int
}

func (p *exprParser) peek() *token {
	if p.pos < len(p.toks) { return &p.toks[p.pos] }
	return nil
}
func (p *exprParser) eat(text string) bool {
	if t := p.peek(); t != nil && t.text == text { p.pos++; return true }
	return false
}
func (p *exprParser) expect(text string) error {
	if !p.eat(text) { return fmt.Errorf("expected %q", text) }
	return nil
}

// parseExpr 优先级链：or < and < not < 比较 < +- < */%// < **/一元 < 后缀
// （对齐 Python ast 结构层）。
func (p *exprParser) parseExpr() (exprNode, error) {
	return p.parseOr()
}

func (p *exprParser) parseOr() (exprNode, error) {
	if err := p.checkDepth(); err != nil { return nil, err }
	left, err := p.parseAnd()
	if err != nil { return nil, err }
	if p.eat("or") {
		values := []exprNode{left}
		for {
			v, err := p.parseAnd()
			if err != nil { return nil, err }
			values = append(values, v)
			if !p.eat("or") { break }
		}
		return &boolOpNode{isAnd: false, values: values}, nil
	}
	return left, nil
}

func (p *exprParser) parseAnd() (exprNode, error) {
	left, err := p.parseNot()
	if err != nil { return nil, err }
	if p.eat("and") {
		values := []exprNode{left}
		for {
			v, err := p.parseNot()
			if err != nil { return nil, err }
			values = append(values, v)
			if !p.eat("and") { break }
		}
		return &boolOpNode{isAnd: true, values: values}, nil
	}
	return left, nil
}

func (p *exprParser) parseNot() (exprNode, error) {
	if p.eat("not") {
		v, err := p.parseNot()
		if err != nil { return nil, err }
		return &unaryOpNode{kind: "not", operand: v}, nil
	}
	return p.parseCompare()
}

func (p *exprParser) parseCompare() (exprNode, error) {
	left, err := p.parseAdd()
	if err != nil { return nil, err }
	var ops []compareOp
	var rights []exprNode
	for {
		var kind string
		for _, k := range []string{"==", "!=", "<=", ">=", "is not", "not in", "<", ">", "in", "is"} {
			if p.eat(k) { kind = k; break }
		}
		if kind == "" { break }
		right, err := p.parseAdd()
		if err != nil { return nil, err }
		ops = append(ops, compareOp{kind: kind})
		rights = append(rights, right)
	}
	if len(ops) == 0 { return left, nil }
	return &compareNode{first: left, ops: ops, rights: rights}, nil
}

func (p *exprParser) parseAdd() (exprNode, error) {
	left, err := p.parseMul()
	if err != nil { return nil, err }
	for {
		t := p.peek()
		if t == nil || (t.text != "+" && t.text != "-") { break }
		p.pos++
		right, err := p.parseMul()
		if err != nil { return nil, err }
		left = &binOpNode{kind: t.text, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseMul() (exprNode, error) {
	left, err := p.parseUnary()
	if err != nil { return nil, err }
	for {
		t := p.peek()
		if t == nil || (t.text != "*" && t.text != "/" && t.text != "%" && t.text != "//") { break }
		p.pos++
		right, err := p.parseUnary()
		if err != nil { return nil, err }
		left = &binOpNode{kind: t.text, left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseUnary() (exprNode, error) {
	t := p.peek()
	if t != nil && (t.text == "-" || t.text == "+" || t.text == "~") {
		p.pos++
		v, err := p.parseUnary()
		if err != nil { return nil, err }
		return &unaryOpNode{kind: t.text, operand: v}, nil
	}
	return p.parsePower()
}

func (p *exprParser) parsePower() (exprNode, error) {
	base, err := p.parsePostfix()
	if err != nil { return nil, err }
	if p.eat("**") {
		exp, err := p.parseUnary() // Python ** 右结合
		if err != nil { return nil, err }
		return &binOpNode{kind: "**", left: base, right: exp}, nil
	}
	return base, nil
}

func (p *exprParser) parsePostfix() (exprNode, error) {
	v, err := p.parseAtom()
	if err != nil { return nil, err }
	for {
		switch {
		case p.eat("."):
			t := p.peek()
			if t == nil || t.kind != tokName { return nil, fmt.Errorf("expected attribute name") }
			p.pos++
			v = &attrNode{value: v, attr: t.text}
		case p.eat("["):
			// Slice 判定：[a:b] / [a:b:c]
			var lower, upper, step exprNode
			isSlice := false
			if !p.eat(":") {
				e, err := p.parseExpr()
				if err != nil { return nil, err }
				if p.eat(":") {
					isSlice = true
					lower = e
				} else {
					if err := p.expect("]"); err != nil { return nil, err }
					v = &subscriptNode{value: v, index: e}
					continue
				}
			} else {
				isSlice = true
			}
			if isSlice {
				if t := p.peek(); t != nil && t.text != "]" && t.text != ":" {
					e, err := p.parseExpr()
					if err != nil { return nil, err }
					upper = e
				}
				if p.eat(":") {
					if t := p.peek(); t != nil && t.text != "]" {
						e, err := p.parseExpr()
						if err != nil { return nil, err }
						step = e
					}
				}
				if err := p.expect("]"); err != nil { return nil, err }
				v = &subscriptNode{value: v, slice: &sliceNode{lower: lower, upper: upper, step: step}}
			}
		case p.eat("("):
			var args []exprNode
			if t := p.peek(); t != nil && t.text != ")" {
				for {
					e, err := p.parseExpr()
					if err != nil { return nil, err }
					args = append(args, e)
					if !p.eat(",") { break }
				}
			}
			if err := p.expect(")"); err != nil { return nil, err }
			v = &callNode{fn: v, args: args}
		default:
			return v, nil
		}
	}
}

func (p *exprParser) parseAtom() (exprNode, error) {
	if err := p.checkDepth(); err != nil { return nil, err }
	t := p.peek()
	if t == nil { return nil, fmt.Errorf("unexpected end of expression") }
	switch {
	case t.kind == tokNum:
		p.pos++
		return &constantNode{value: t.num}, nil
	case t.kind == tokStr:
		p.pos++
		return &constantNode{value: t.text}, nil
	case t.kind == tokName && t.text == "True":
		p.pos++
		return &constantNode{value: true}, nil
	case t.kind == tokName && t.text == "False":
		p.pos++
		return &constantNode{value: false}, nil
	case t.kind == tokName && t.text == "None":
		p.pos++
		return &constantNode{value: nil}, nil
	case t.kind == tokName:
		p.pos++
		return &nameNode{id: t.text}, nil
	case t.text == "(":
		p.pos++
		e, err := p.parseExpr()
		if err != nil { return nil, err }
		if err := p.expect(")"); err != nil { return nil, err }
		return e, nil
	case t.text == "[":
		p.pos++
		var elts []exprNode
		if tt := p.peek(); tt != nil && tt.text != "]" {
			for {
				e, err := p.parseExpr()
				if err != nil { return nil, err }
				elts = append(elts, e)
				if !p.eat(",") { break }
			}
		}
		if len(elts) > constants.MaxCollectionSize {
			return nil, p.evalErr("list contains too many elements, maximum allowed is " +
				strconv.Itoa(constants.MaxCollectionSize))
		}
		if err := p.expect("]"); err != nil { return nil, err }
		return &listNode{elts: elts}, nil
	case t.text == "{":
		p.pos++
		var keys, values []exprNode
		if tt := p.peek(); tt != nil && tt.text != "}" {
			for {
				k, err := p.parseExpr()
				if err != nil { return nil, err }
				if err := p.expect(":"); err != nil { return nil, err }
				v, err := p.parseExpr()
				if err != nil { return nil, err }
				keys = append(keys, k)
				values = append(values, v)
				if !p.eat(",") { break }
			}
		}
		if len(keys) > constants.MaxCollectionSize {
			return nil, p.evalErr("dictionary has too many key-value pairs, maximum allowed is " +
				strconv.Itoa(constants.MaxCollectionSize))
		}
		if err := p.expect("}"); err != nil { return nil, err }
		return &dictNode{keys: keys, values: values}, nil
	}
	return nil, fmt.Errorf("unexpected token %q", t.text)
}

func (p *exprParser) checkDepth() error {
	p.depth++
	if p.depth > constants.MaxASTDepth {
		return p.evalErr(fmt.Sprintf("expression nesting depth exceeds maximum allowed depth of %d",
			constants.MaxASTDepth))
	}
	return nil
}

func (p *exprParser) evalErr(msg string) error {
	return exception.BuildError(exception.StatusExpressionEvalError,
		exception.WithParam("error_msg", msg))
}
```

- [ ] **Step 3.3: 实现 evaluator（追加）**——求值语义逐条对齐 expression.py:261-566：

```go
// evaluateExpression 入口（Python _evaluate_expression，expression.py:66-128）。
func (c *ExpressionCondition) evaluateExpression(expression string, inputs map[string]any) (bool, error) {
	processed := convertCondition(expression, inputs)

	// 变量改名（expression.py:69-77：按长度降序替换防前缀串扰）
	varMapping := map[string]string{}
	for _, m := range innerVarPattern.FindAllString(processed, -1) {
		name := m[2 : len(m)-1]
		varMapping[m] = "var_" + strconv.Itoa(len(varMapping))
		_ = name
	}
	// 注意：Python 按出现序编号 var_i，replacement 用 sorted(key=len, reverse=True) 防串扰
	matches := innerVarPattern.FindAllString(processed, -1)
	replacements := make([]struct{ full, safe string }, 0, len(matches))
	for i, m := range matches {
		replacements = append(replacements, struct{ full, safe string }{m, fmt.Sprintf("var_%d", i)})
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		return len(replacements[i].full) > len(replacements[j].full)
	})
	for _, r := range replacements {
		processed = strings.ReplaceAll(processed, r.full, r.safe)
	}

	// runtime 环境（expression.py:79-92）
	runtime := map[string]any{
		"len":              wrapLen,
		"bool":             func(v any) bool { return truthy(v) },
		"sum":              wrapSum,
		"inputs":           inputs,
		"_safe_is_empty":   wrapIsEmpty,
		"_safe_is_not_empty": wrapIsNotEmpty,
		"is_empty":         wrapIsEmpty,
		"is_not_empty":     wrapIsNotEmpty,
	}
	// 变量值注入 runtime（expression.py:94-97：改名键存在 inputs 时才注入）
	for i, m := range matches {
		if v, ok := inputs[m]; ok {
			runtime[fmt.Sprintf("var_%d", i)] = v
		}
	}

	toks, err := tokenize(processed)
	if err != nil {
		return false, exception.BuildError(exception.StatusExpressionSyntaxError,
			exception.WithParam("error_msg", err.Error()))
	}
	p := &exprParser{toks: toks}
	tree, err := p.parseExpr()
	if err != nil {
		return false, exception.BuildError(exception.StatusExpressionSyntaxError,
			exception.WithParam("error_msg", err.Error()))
	}
	if p.pos != len(p.toks) {
		return false, exception.BuildError(exception.StatusExpressionSyntaxError,
			exception.WithParam("error_msg", fmt.Sprintf("unexpected token %q", p.toks[p.pos].text)))
	}
	result, err := evaluateAST(tree, runtime)
	if err != nil {
		return false, err
	}
	return truthy(result), nil
}

// evaluateAST 求值器（Python _evaluate_ast，expression.py:261-303；节点分支
// 顺序对齐源码，BaseError 原样上抛）。
func evaluateAST(node exprNode, rt map[string]any) (any, error) {
	switch n := node.(type) {
	case *boolOpNode:
		// Python _evaluate_bool_op 先全量求值再 all/any（不做短路优化，逐字复刻）
		var values []bool
		for _, v := range n.values {
			r, err := evaluateAST(v, rt)
			if err != nil { return nil, err }
			values = append(values, truthy(r))
		}
		if n.isAnd {
			for _, b := range values { if !b { return false, nil } }
			return true, nil
		}
		for _, b := range values { if b { return true, nil } }
		return false, nil
	case *compareNode:
		// Python _evaluate_compare：逐对比较，False 即返
		left, err := evaluateAST(n.first, rt)
		if err != nil { return nil, err }
		for i, op := range n.ops {
			right, err := evaluateAST(n.rights[i], rt)
			if err != nil { return nil, err }
			ok, err := compareValues(left, right, op.kind)
			if err != nil { return nil, err }
			if !ok { return false, nil }
			left = right
		}
		return true, nil
	case *nameNode:
		return evalName(n, rt)
	case *constantNode:
		return n.value, nil
	case *subscriptNode:
		return evalSubscript(n, rt)
	case *attrNode:
		return evalAttribute(n, rt)
	case *callNode:
		fn, err := evaluateAST(n.fn, rt)
		if err != nil { return nil, err }
		args := make([]any, 0, len(n.args))
		for _, a := range n.args {
			r, err := evaluateAST(a, rt)
			if err != nil { return nil, err }
			args = append(args, r)
		}
		if fn == nil { return nil, evalErrAny("function <nil> is not defined or not callable") }
		callable, ok := fn.(func([]any) (any, error))
		if !ok {
			return nil, evalErrAny(fmt.Sprintf("function %v is not defined or not callable", fn))
		}
		return callable(args)
	case *listNode:
		out := make([]any, 0, len(n.elts))
		for _, e := range n.elts {
			r, err := evaluateAST(e, rt)
			if err != nil { return nil, err }
			out = append(out, r)
		}
		return out, nil
	case *tupleNode:
		out := make([]any, 0, len(n.elts))
		for _, e := range n.elts {
			r, err := evaluateAST(e, rt)
			if err != nil { return nil, err }
			out = append(out, r)
		}
		return out, nil
	case *dictNode:
		out := map[string]any{}
		for i := range n.keys {
			k, err := evaluateAST(n.keys[i], rt)
			if err != nil { return nil, err }
			v, err := evaluateAST(n.values[i], rt)
			if err != nil { return nil, err }
			out[fmt.Sprintf("%v", k)] = v
		}
		return out, nil
	case *unaryOpNode:
		v, err := evaluateAST(n.operand, rt)
		if err != nil { return nil, err }
		return evalUnary(n.kind, v)
	case *binOpNode:
		left, err := evaluateAST(n.left, rt)
		if err != nil { return nil, err }
		right, err := evaluateAST(n.right, rt)
		if err != nil { return nil, err }
		return evalBinary(n.kind, left, right)
	}
	return nil, evalErrAny(fmt.Sprintf("unsupported AST node type: %T", node))
}

// evalName 四级解析（Python _evaluate_name，expression.py:428-457：runtime →
// inputs → int → float → True/False/None → 报错）。
func evalName(n *nameNode, rt map[string]any) (any, error) {
	if v, ok := rt[n.id]; ok { return v, nil }
	if inputs, ok := rt["inputs"].(map[string]any); ok {
		if v, ok := inputs[n.id]; ok { return v, nil }
	}
	if iv, err := strconv.Atoi(n.id); err == nil { return iv, nil }
	if fv, err := strconv.ParseFloat(n.id, 64); err == nil { return fv, nil }
	switch n.id {
	case "True": return true, nil
	case "False": return false, nil
	case "None": return nil, nil
	}
	return nil, evalErrAny(fmt.Sprintf("name '%s' is not defined", n.id))
}

// compareValues 比较（Python _compare_values，expression.py:395-425：
// in/not in 遇 TypeError 返 False）。
func compareValues(left, right any, op string) (bool, error) {
	switch op {
	case "==": return pyEqual(left, right), nil
	case "!=": return !pyEqual(left, right), nil
	case "<", "<=", ">", ">=":
		lc, lok := left.(int); rc, rok := right.(int)
		lf, lfok := toFloat(left); rf, rfok := toFloat(right)
		if lok && rok {
			switch op {
			case "<": return lc < rc, nil
			case "<=": return lc <= rc, nil
			case ">": return lc > rc, nil
			default: return lc >= rc, nil
			}
		}
		if lfok && rfok {
			switch op {
			case "<": return lf < rf, nil
			case "<=": return lf <= rf, nil
			case ">": return lf > rf, nil
			default: return lf >= rf, nil
			}
		}
		// 非数值（字符串等）按字典序比较；不可比较按 Python TypeError 处理
		ls, lsok := left.(string); rs, rsok := right.(string)
		if lsok && rsok {
			switch op {
			case "<": return ls < rs, nil
			case "<=": return ls <= rs, nil
			case ">": return ls > rs, nil
			default: return ls >= rs, nil
			}
		}
		return false, evalErrAny(fmt.Sprintf("'<' not supported between instances of '%s' and '%s'",
			typeName(left), typeName(right)))
	case "is": return left == right, nil // Go 无对象身份，值相等近似（spec §7 记录）
	case "is not": return left != right, nil
	case "in":
		return containsOp(left, right) // TypeError→False（expression.py:412-416）
	case "not in":
		ok, _ := containsOp(left, right)
		return !ok, nil
	}
	return false, evalErrAny(fmt.Sprintf("unsupported comparison operator: %s", op))
}

// containsOp Python `in`（str/切片/map；TypeError 返 False）。
func containsOp(left, right any) (bool, error) {
	switch r := right.(type) {
	case string:
		l, ok := left.(string)
		if !ok { return false, nil }
		return strings.Contains(r, l), nil
	case []any:
		for _, e := range r {
			if pyEqual(e, left) { return true, nil }
		}
		return false, nil
	case map[string]any:
		_, ok := r[fmt.Sprintf("%v", left)]
		return ok, nil
	}
	return false, nil // TypeError → False
}

// evalBinary 二元运算（Python _evaluate_bin_op，expression.py:324-377；
// Mult/Pow 含护栏）。
func evalBinary(kind string, left, right any) (any, error) {
	// 字符串拼接/重复优先（Python str.__add__/__mul__）
	if kind == "+" {
		ls, lok := left.(string); rs, rok := right.(string)
		if lok && rok { return ls + rs, nil }
	}
	if kind == "*" {
		if ls, ok := left.(string); ok {
			if ri, ok := toInt(right); ok { return strings.Repeat(ls, ri), nil }
		}
		if rs, ok := right.(string); ok {
			if li, ok := toInt(left); ok { return strings.Repeat(rs, li), nil }
		}
		// 列表乘法护栏（expression.py:334-347）
		if llist, ok := left.([]any); ok {
			if ri, ok := toInt(right); ok && ri > 0 {
				if len(llist)*ri > constants.MaxCollectionSize {
					return nil, evalErrAny("operation would create collection exceeding maximum size of " +
						strconv.Itoa(constants.MaxCollectionSize))
				}
			}
		}
	}
	// Pow 指数护栏（expression.py:353-361）
	if kind == "**" {
		if lf, ok := toFloat(left); ok {
			if rf, ok := toFloat(right); ok && rf > 100 {
				return nil, evalErrAny("exponent too large in power operation")
			}
		}
	}
	// 数值双轨：全 int 走 int，否则 float
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if !lok || !rok {
		return nil, evalErrAny(fmt.Sprintf("unsupported operand type(s) for %s: '%s' and '%s'",
			kind, typeName(left), typeName(right)))
	}
	li, lInt := toInt(left)
	ri, rInt := toInt(right)
	bothInt := lInt && rInt
	switch kind {
	case "+":
		if bothInt { return li + ri, nil }
		return lf + rf, nil
	case "-":
		if bothInt { return li - ri, nil }
		return lf - rf, nil
	case "*", "//", "%":
		if bothInt {
			switch kind {
			case "*": return li * ri, nil
			case "//": return li / ri, nil // Python int // int 截断除
			default: return li % ri, nil
			}
		}
		if kind == "*" { return lf * rf, nil }
		if kind == "//" { return math.Floor(lf / rf), nil }
		return math.Mod(lf, rf), nil
	case "/": return lf / rf, nil // Python 真除法恒 float
	case "**":
		return math.Pow(lf, rf), nil
	case "<<":
		return li << uint(ri), nil
	case ">>":
		return li >> uint(ri), nil
	case "|":
		return li | ri, nil
	case "^":
		return li ^ ri, nil
	case "&":
		return li & ri, nil
	}
	return nil, evalErrAny(fmt.Sprintf("unsupported binary operator: %s", kind))
}

// evalUnary 一元运算（Python _evaluate_unary_op，expression.py:307-320）。
func evalUnary(kind string, v any) (any, error) {
	switch kind {
	case "not": return !truthy(v), nil
	case "-":
		if i, ok := toInt(v); ok { return -i, nil }
		f, _ := toFloat(v)
		return -f, nil
	case "+":
		if i, ok := toInt(v); ok { return i, nil }
		f, _ := toFloat(v)
		return f, nil
	case "~":
		i, ok := toInt(v)
		if !ok { return nil, evalErrAny("bad operand type for unary ~") }
		return ^i, nil
	}
	return nil, evalErrAny(fmt.Sprintf("unsupported unary operator: %s", kind))
}

// evalSubscript 下标/切片（Python _evaluate_subscript，expression.py:460-489；
// 尺寸/切片跨度护栏）。
func evalSubscript(n *subscriptNode, rt map[string]any) (any, error) {
	value, err := evaluateAST(n.value, rt)
	if err != nil { return nil, err }
	if l, ok := safeLen(value); ok == nil && l > constants.MaxCollectionSize {
		return nil, evalErrAny("collection size exceeds maximum allowed size of " +
			strconv.Itoa(constants.MaxCollectionSize))
	}
	if n.slice != nil {
		var lower, upper, step any = nil, nil, nil
		if n.slice.lower != nil {
			if lower, err = evaluateAST(n.slice.lower, rt); err != nil { return nil, err }
		}
		if n.slice.upper != nil {
			if upper, err = evaluateAST(n.slice.upper, rt); err != nil { return nil, err }
		}
		if n.slice.step != nil {
			if step, err = evaluateAST(n.slice.step, rt); err != nil { return nil, err }
		}
		if li, ok := toInt(lower); ok {
			if ui, ok2 := toInt(upper); ok2 && ui-li > constants.MaxCollectionSize {
				return nil, evalErrAny("slice operation would create collection exceeding maximum size of " +
					strconv.Itoa(constants.MaxCollectionSize))
			}
		}
		return pythonSlice(value, lower, upper, step)
	}
	index, err := evaluateAST(n.index, rt)
	if err != nil { return nil, err }
	switch v := value.(type) {
	case []any:
		i, ok := toInt(index)
		if !ok { return nil, evalErrAny("list indices must be integers") }
		if i < 0 { i += len(v) }
		if i < 0 || i >= len(v) { return nil, evalErrAny("list index out of range") }
		return v[i], nil
	case string:
		i, ok := toInt(index)
		if !ok { return nil, evalErrAny("string indices must be integers") }
		if i < 0 { i += len(v) }
		if i < 0 || i >= len(v) { return nil, evalErrAny("string index out of range") }
		return string(v[i]), nil
	case map[string]any:
		out, ok := v[fmt.Sprintf("%v", index)]
		if !ok { return nil, evalErrAny(fmt.Sprintf("KeyError: '%v'", index)) }
		return out, nil
	}
	return nil, evalErrAny(fmt.Sprintf("'%s' object is not subscriptable", typeName(value)))
}

// evalAttribute 属性访问（Python _evaluate_attribute，expression.py:492-523：
// dunder 封禁 + 敏感属性表 + getattr→dict 回落）。
func evalAttribute(n *attrNode, rt map[string]any) (any, error) {
	value, err := evaluateAST(n.value, rt)
	if err != nil { return nil, err }
	if strings.HasPrefix(n.attr, "__") && strings.HasSuffix(n.attr, "__") {
		return nil, evalErrAny(fmt.Sprintf("disallowed operation: access to special attribute '%s' is prohibited", n.attr))
	}
	for _, s := range []string{"__class__", "__bases__", "__subclasses__", "__module__", "__dict__"} {
		if n.attr == s {
			return nil, evalErrAny(fmt.Sprintf("disallowed operation: Access to sensitive attribute '%s' is prohibited", n.attr))
		}
	}
	if m, ok := value.(map[string]any); ok {
		if v, ok := m[n.attr]; ok { return v, nil }
	}
	return nil, evalErrAny(fmt.Sprintf("'dict' object has no attribute '%s'", n.attr))
}

// ──────────────────────────── 非导出函数（辅助） ────────────────────────────

// truthy Python truthiness（None/0/空串/空容器为假；bool 直取）。
func truthy(v any) bool {
	switch x := v.(type) {
	case nil: return false
	case bool: return x
	case int: return x != 0
	case float64: return x != 0
	case string: return x != ""
	case []any: return len(x) > 0
	case map[string]any: return len(x) > 0
	}
	return true
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int: return x, true
	case int64: return int(x), true
	case float64:
		if x == float64(int(x)) { return int(x), true }
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int: return float64(x), true
	case int64: return float64(x), true
	case float64: return x, true
	}
	return 0, false
}

// pyEqual 值相等（跨数值双轨；bool 与 int 不混判——Python True==1 恒真，
// 对齐 Python 语义：bool 先归一数值）。
func pyEqual(a, b any) bool {
	if ab, ok := a.(bool); ok { a = boolToNum(ab) }
	if bb, ok := b.(bool); ok { b = boolToNum(bb) }
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok { return af == bf }
		return false
	}
	if as, ok := a.(string); ok {
		bs, ok2 := b.(string)
		return ok2 && as == bs
	}
	if a == nil || b == nil { return a == nil && b == nil }
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func boolToNum(b bool) any { if b { return 1 }; return 0 }

// wrapLen/wrapSum/wrapIsEmpty/wrapIsNotEmpty 把 Go 多返回值适配成
// callNode 可调用的 func([]any) (any, error) 形态。
func wrapLen(args []any) (any, error) {
	if len(args) != 1 { return nil, evalErrAny("len() takes exactly one argument") }
	n, err := safeLen(args[0])
	if err != nil { return nil, err }
	return n, nil
}

func wrapSum(args []any) (any, error) {
	if len(args) == 0 { return nil, evalErrAny("sum() requires at least one argument") }
	total := 0.0
	allInt := true
	for _, a := range args {
		f, ok := toFloat(a)
		if !ok { return nil, evalErrAny("sum() operands must be numeric") }
		if _, isInt := toInt(a); !isInt { allInt = false }
		total += f
	}
	if allInt {
		s := 0
		for _, a := range args { iv, _ := toInt(a); s += iv }
		return s, nil
	}
	return total, nil
}

func wrapIsEmpty(args []any) (any, error) {
	if len(args) != 1 { return nil, evalErrAny("is_empty() takes exactly one argument") }
	b, err := safeIsEmpty(args[0])
	if err != nil { return nil, err }
	return b, nil
}

func wrapIsNotEmpty(args []any) (any, error) {
	if len(args) != 1 { return nil, evalErrAny("is_not_empty() takes exactly one argument") }
	b, err := safeIsEmpty(args[0])
	if err != nil { return nil, err }
	return !b, nil
}

func evalErrAny(msg string) error {
	return exception.BuildError(exception.StatusExpressionEvalError,
		exception.WithParam("error_msg", msg))
}

// pythonSlice 切片（str 与 []any 两形态，负索引/步长对齐 Python 切片规则）。
func pythonSlice(value, lower, upper, step any) (any, error) {
	switch v := value.(type) {
	case string:
		out, err := sliceIndices(len(v), lower, upper, step)
		if err != nil { return nil, err }
		var b strings.Builder
		for _, i := range out { b.WriteByte(v[i]) }
		return b.String(), nil
	case []any:
		out, err := sliceIndices(len(v), lower, upper, step)
		if err != nil { return nil, err }
		res := make([]any, 0, len(out))
		for _, i := range out { res = append(res, v[i]) }
		return res, nil
	}
	return nil, evalErrAny(fmt.Sprintf("'%s' object is not subscriptable", typeName(value)))
}

func sliceIndices(n int, lower, upper, step any) ([]int, error) {
	st := 1
	if step != nil {
		si, ok := toInt(step)
		if !ok || si == 0 { return nil, evalErrAny("slice step cannot be zero") }
		st = si
	}
	norm := func(v any, d int) int {
		if v == nil { return d }
		i, _ := toInt(v)
		if i < 0 { i += n }
		if i < 0 { if st < 0 { i = -1 } else { i = 0 } }
		if i > n { i = n }
		return i
	}
	var out []int
	if st > 0 {
		lo, hi := norm(lower, 0), norm(upper, n)
		for i := lo; i < hi; i += st { out = append(out, i) }
	} else {
		lo, hi := norm(lower, n-1), norm(upper, -1)
		for i := lo; i > hi; i += st { out = append(out, i) }
	}
	return out, nil
}
```

补充 import：`math`、`reflect`、`strconv`。

- [ ] **Step 3.4: 跑测试**

```bash
go test ./internal/openuapclaw/core/workflow/components/condition/
```
Expected: PASS（含 Task 1/2 全部用例）

- [ ] **Step 3.5: 提交（Task 2+3 同一提交，显式 add）**

```bash
git add internal/openuapclaw/core/workflow/components/condition/
git commit -m "feat(condition): ExpressionCondition 表达式求值器（8.19 Task2+3：RULES/手写parser/语义一比一/护栏）"
```

---

### Task 4: Branch + BranchRouter

**Files:** Create `components/flow/branch_router.go`、`branch_router_test.go`

- [ ] **Step 4.1: 写失败测试**

```go
func TestBranch_三态归一(t *testing.T) {
	b1, err := NewBranch(nil /*Condition 实例*/, []string{"x"}, "id1") // 传 AlwaysTrue 实例
	require.NoError(t, err)
	_, ok := b1.condition.(*condition.AlwaysTrue)
	assert.True(t, ok)
	b2, err := NewBranch("${a} > 1", []string{"x"}, "")
	require.NoError(t, err)
	_, ok = b2.condition.(*condition.ExpressionCondition)
	assert.True(t, ok)
	b3, err := NewBranch(myFunc, []string{"x"}, "")
	require.NoError(t, err)
	_, ok = b3.condition.(*condition.FuncCondition)
	assert.True(t, ok)
	_, err = NewBranch(123, []string{"x"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch condition type does not meet the requirements")
}

func TestBranchRouter_AddBranch_校验与归一(t *testing.T) {
	r := NewBranchRouter(false)
	require.Error(t, r.AddBranch(nil, []string{"x"}, ""))                    // condition nil
	require.Error(t, r.AddBranch("a>1", nil, ""))                            // target nil
	require.NoError(t, r.AddBranch("a>1", "x", ""))                          // str target 归一
	assert.Equal(t, map[string]struct{}{"x": {}}, r.AllTargets())
}

func TestBranchRouter_Call_首命中返回(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("${a} > 100", "end_a", "high"))
	require.NoError(t, r.AddBranch(NewAlwaysTarget("end_b"), "end_b", "default"))
	r.SetSession(mustWorkflowSessionWithGlobals(t, map[string]any{"a": 150}))
	targets, err := r.Call(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"end_a"}, targets) // 顺序敏感：首个命中即返回
}

func TestBranchRouter_Call_全不命中报错(t *testing.T) {
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("${a} > 999", "x", "high"))
	r.SetSession(mustWorkflowSessionWithGlobals(t, map[string]any{"a": 1}))
	_, err := r.Call(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch meeting the condition was not found")
}

func TestBranchRouter_SetSession_双类型(t *testing.T) {
	r := NewBranchRouter(false)
	require.Error(t, r.SetSession(123)) // "session type is wrong"
	require.NoError(t, r.SetSession(mustWorkflowSession(t)))
}

func TestBranchRouter_Dispatch_产出Trigger消息(t *testing.T) {
	// BranchRouter 实现 pregel.Router：Dispatch→Call→TriggerMessage(sender=source)
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("true", "end_a", "b1"))
	r.SetSession(mustWorkflowSession(t))
	msgs, err := r.Dispatch(context.Background(), "branch")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "end_a", msgs[0].Target)
}
```

`AlwaysTarget` 测试桩：`condition.Condition` 实现恒返回 `["end_b"]`——注意 Python 里 `add_branch` 的 target 是 Branch 携带的，条件只出 bool；此处桩用于控制命中序。

- [ ] **Step 4.2: 跑测试确认编译失败**

- [ ] **Step 4.3: 实现 branch_router.go**

```go
package flow

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Branch 单条分支：条件 + 目标列表（Python branch_router.py:19-38 Branch）。
type Branch struct {
	// BranchID 分支标识（Python branch_id，可空）
	BranchID string
	// target 命中后的目标节点列表
	target []string
	// condition 条件（三态归一产物）
	condition condition.Condition
}

// NewBranch 构造分支（Python __init__ 三态归一：Condition 实例 / str→ExpressionCondition /
// Callable→FuncCondition；其余 ValueError）。
func NewBranch(cond any, target []string, branchID string) (*Branch, error) {
	b := &Branch{BranchID: branchID, target: target}
	switch c := cond.(type) {
	case condition.Condition:
		b.condition = c
	case string:
		ec, err := condition.NewExpressionCondition(c)
		if err != nil {
			return nil, exception.BuildError(exception.StatusComponentBranchParamInvalid,
				exception.WithParam("reason", err.Error()))
		}
		b.condition = ec
	case func() bool:
		b.condition = condition.NewFuncCondition(c)
	default:
		return nil, errBranchConditionType()
	}
	return b, nil
}

// Evaluate 评估条件（Python evaluate: return self._condition(session)）。
func (b *Branch) Evaluate(ctx context.Context, sess session.BaseSession) (bool, error) {
	return b.condition.Call(ctx, sess)
}

// TraceInfo 追踪信息（Python trace_info: return self._condition.trace_info(session)）。
func (b *Branch) TraceInfo(sess session.BaseSession) any {
	return b.condition.TraceInfo(sess)
}

// BranchRouter 分支路由器（Python branch_router.py:41-112）。
// 实现 pregel.Router：dispatch 阶段同步评估（spec §1 链路②）；
// session 走实例字段捕获（8.5 D2 惯例：Select/Dispatch 的 state 形参恒 nil 不读）。
// 偏差 D-19-1：drawable 三处（env 读取 / DrawableBranchRouter / add_branch 填充）
// 整段跳过，回收条件挂 8.13。
type BranchRouter struct {
	branches    []*Branch
	session     session.BaseSession
	reportTrace bool
}

// NewBranchRouter 构造路由器（Python __init__(report_trace=False)）。
func NewBranchRouter(reportTrace bool) *BranchRouter {
	return &BranchRouter{reportTrace: reportTrace}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AddBranch 登记分支（Python add_branch：nil 校验 → str target 归一 → 构造 Branch）。
func (r *BranchRouter) AddBranch(cond any, target any, branchID string) error {
	if cond == nil || target == nil {
		return exception.BuildError(exception.StatusComponentBranchParamInvalid,
			exception.WithParam("reason", "condition is None or target is None"))
	}
	var targets []string
	switch t := target.(type) {
	case string:
		targets = []string{t}
	case []string:
		targets = t
	}
	// Python add_branch 内 drawable 填充段（branch_router.py:58-64）——D-19-1 跳过
	b, err := NewBranch(cond, targets, branchID)
	if err != nil {
		return exception.BuildError(exception.StatusComponentBranchParamInvalid,
			exception.WithParam("reason", err.Error()))
	}
	r.branches = append(r.branches, b)
	return nil
}

// AllTargets 目标全集（Python all_targets property：set 去重）。
func (r *BranchRouter) AllTargets() map[string]struct{} {
	targets := map[string]struct{}{}
	for _, b := range r.branches {
		for _, t := range b.target {
			targets[t] = struct{}{}
		}
	}
	return targets
}

// SetSession 注入会话（Python set_session 双类型分支）。
func (r *BranchRouter) SetSession(sess session.BaseSession) error {
	if sess == nil {
		return errSessionTypeWrong()
	}
	r.session = sess
	return nil
}

// Call 评估分支（Python __call__，branch_router.py:92-112）：
// report_trace 三段 trace → 顺序评估首个命中返回其 target → 全不命中 101021。
func (r *BranchRouter) Call(ctx context.Context) ([]string, error) {
	sess := r.session
	if r.reportTrace {
		var branches []map[string]any
		for _, b := range r.branches {
			branches = append(branches, map[string]any{
				"branch_id": b.BranchID,
				"condition": b.TraceInfo(sess),
			})
		}
		utils := tracer.TracerWorkflowUtils{}
		utils.TraceComponentBegin(ctx, r.session, false)
		utils.TraceComponentInputs(ctx, r.session, map[string]any{"branches": branches}, false)
	}
	for _, b := range r.branches {
		hit, err := b.Evaluate(ctx, sess)
		if err != nil {
			return nil, err
		}
		if hit {
			if r.reportTrace {
				utils := tracer.TracerWorkflowUtils{}
				utils.TraceComponentOutputs(ctx, r.session, map[string]any{"branch_id": b.BranchID})
				utils.TraceComponentDone(ctx, r.session)
			}
			return b.target, nil
		}
	}
	return nil, exception.BuildError(exception.StatusComponentBranchExecutionError,
		exception.WithParam("reason", "branch meeting the condition was not found"))
}

// Dispatch 实现 pregel.Router（Python 经 ConditionalRouter 包装 __call__ 后产出
// TriggerMessage；Go 侧 BranchRouter 直挂 Router 接口，消息产出对齐 router.py:47）。
// sourceNode 即发起节点名；error 原样上抛（8.3b FIRST_EXCEPTION 分类依赖 errors.As）。
func (r *BranchRouter) Dispatch(ctx context.Context, sourceNode string) ([]pregel.Message, error) {
	targets, err := r.Call(ctx)
	if err != nil {
		return nil, err
	}
	msgs := make([]pregel.Message, 0, len(targets))
	for _, to := range targets {
		msgs = append(msgs, pregel.NewTriggerMessage(sourceNode, to))
	}
	return msgs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func errBranchConditionType() error {
	return exception.BuildError(exception.StatusComponentBranchParamInvalid,
		exception.WithParam("reason", "branch condition type does not meet the requirements"))
}

func errSessionTypeWrong() error {
	return exception.BuildError(exception.StatusComponentBranchParamInvalid,
		exception.WithParam("reason", "session type is wrong"))
}
```

注：①`SetSession` 的 Session/BaseSession 双类型在 Go 收窄为 BaseSession 单形态（8.17 NewComponentSession 已统一包装 NodeSessionFacade→BaseSession；如有 Session 包装层需求实施时按 8.17 惯例登记偏差）；②`TracerWorkflowUtils` 四段签名以 `tracer/workflow.go:81-184` 实参为准（ctx/session/send 形参实施时核对）；③异常常量名 grep `codes_workflow.go` 实名。

- [ ] **Step 4.4: 跑测试通过**

- [ ] **Step 4.5: 提交**

```bash
git add internal/openuapclaw/core/workflow/components/flow/
git commit -m "feat(flow): Branch+BranchRouter（8.19 Task4：三态归一/首命中/Dispatch直挂Router/D-19-1跳过drawable）"
```

---

### Task 5: BranchComponent

**Files:** Create `components/flow/branch_comp.go`、`branch_comp_test.go`

- [ ] **Step 5.1: 写失败测试**

```go
func TestBranchComponent_AddBranch_三段校验(t *testing.T) {
	c := NewBranchComponent()
	require.Error(t, c.AddBranch(nil, []string{"x"}, ""))                 // condition falsy
	require.Error(t, c.AddBranch("a>1", []string{}, ""))                  // target falsy
	require.Error(t, c.AddBranch("a>1", []string{"x", ""}, ""))           // 空项带 index
	assert.Contains(t, c.AddBranch("a>1", []string{"x", ""}, "").Error(), "empty item at index 1")
	require.NoError(t, c.AddBranch("${a} > 1", "x", "b1"))
}

func TestBranchComponent_AddComponent_CNF登记守卫(t *testing.T) {
	g := graph.NewPregelGraph()
	c := NewBranchComponent()
	require.NoError(t, c.AddBranch("true", "e1", "b1"))                    // 单目标：不登记
	c.AddComponent(g, "br", false)
	assert.Empty(t, g.BranchTargetsSnapshot("br"))                        // 访问器以 graph 侧实名为准

	g2 := graph.NewPregelGraph()
	c2 := NewBranchComponent()
	require.NoError(t, c2.AddBranch("true", "e1", "b1"))
	require.NoError(t, c2.AddBranch("false", "e2", "b2"))
	c2.AddComponent(g2, "br", false)                                      // 双目标：登记
	assert.Len(t, g2.BranchTargetsSnapshot("br"), 2)
}

func TestBranchComponent_InvokeFn_置session返回空(t *testing.T) {
	c := NewBranchComponent()
	require.NoError(t, c.AddBranch("true", "x", "b1"))
	out, err := c.InvokeFn(context.Background(), map[string]any{}, mustNodeSession(t))
	require.NoError(t, err)
	assert.Empty(t, out)
	assert.NotNil(t, c.router.session) // set_session 已发生（评估前注入）
}

func TestBranchComponent_SkipTrace_恒真(t *testing.T) {
	assert.True(t, NewBranchComponent().SkipTrace())
}
```

`BranchTargetsSnapshot`：graph 侧若无此访问器则以非导出字段+测试同包或导出访问器承载——**8.1 消费方断言需此读面**，实施时核对 graph 包（若无则加只读访问器并在 spec 登记）。

- [ ] **Step 5.2: 实现 branch_comp.go**

```go
package flow

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// BranchComponent 分支组件（Python branch_comp.py:16-52 BranchComponent）：
// 排他网关——节点体空转（仅 set_session），评估在条件边 dispatch 阶段（spec §1）。
type BranchComponent struct {
	components.WorkflowComponent
	// router 分支路由器（Python self._router，构造期 report_trace=True）
	router *BranchRouter
}

// NewBranchComponent 构造组件（Python __init__: super().__init__() + BranchRouter(True)）。
func NewBranchComponent() *BranchComponent {
	c := &BranchComponent{router: NewBranchRouter(true)}
	// InvokeFn：Python invoke —— set_session(session) → 返回 {}
	c.InvokeFn = func(_ context.Context, _ map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		var base session.BaseSession = sess
		if err := c.router.SetSession(base); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}
	return c
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AddBranch 登记分支（Python add_branch：condition/target falsy 校验 + list 空项
// 逐项校验 → 转调 router.add_branch；错误码 COMPONENT_BRANCH_PARAM_INVALID）。
func (c *BranchComponent) AddBranch(cond any, target any, branchID string) error {
	if cond == nil {
		return errBranchParam("condition is None or empty")
	}
	var targets []string
	switch t := target.(type) {
	case string:
		targets = []string{t}
	case []string:
		targets = t
	default:
		return errBranchParam("target is None or empty")
	}
	if len(targets) == 0 {
		return errBranchParam("target is None or empty")
	}
	for idx, item := range targets {
		if item == "" {
			return errBranchParam(fmt.Sprintf("empty item at index %d in target list", idx))
		}
	}
	return c.router.AddBranch(cond, targets, branchID)
}

// Router 返回路由器（Python router()）。
func (c *BranchComponent) Router() *BranchRouter { return c.router }

// AddComponent 注册进图（Python add_component：add_node + add_conditional_edges +
// targets>1 时 register_branch_targets；Python hasattr 守卫在 Go 以 PregelGraph
// 具体类型承载，无需反射）。
func (c *BranchComponent) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, c.ToExecutable(), opts...)
	g.AddConditionalEdges(nodeID, c.router)
	// Register branch targets for CNF barrier resolution at compile time
	// （branch_comp.py:45-49 逐字复刻：len>1 才登记）
	allTargets := c.router.AllTargets()
	if len(allTargets) > 1 {
		ids := make([]string, 0, len(allTargets))
		for id := range allTargets {
			ids = append(ids, id)
		}
		g.RegisterBranchTargets(nodeID, ids)
	}
	return nil
}

// SkipTrace 跳过追踪（Python skip_trace → True）。
func (c *BranchComponent) SkipTrace() bool { return true }

// ──────────────────────────── 非导出函数 ────────────────────────────

func errBranchParam(reason string) error {
	return exception.BuildError(exception.StatusComponentBranchParamInvalid,
		exception.WithParam("reason", reason))
}
```

- [ ] **Step 5.3: 跑测试**

```bash
go test ./internal/openuapclaw/core/workflow/components/flow/
```
Expected: PASS

- [ ] **Step 5.4: 提交**

```bash
git add internal/openuapclaw/core/workflow/components/flow/
git commit -m "feat(flow): BranchComponent（8.19 Task5：槽位模式/CNF登记守卫/skip_trace）"
```

---

### Task 6: 8.14 接缝盘点（AddConditionalEdges 特判）

**Files:** 核对 `base_workflow.go`；可能 Modify

- [ ] **Step 6.1: 盘点 Go 8.14 的 AddConditionalEdges 路径**

Python `_workflow.py:275-285`：`isinstance(router, BranchRouter)` 时 `router.set_session(self._session)` + add_conditional_edges + register_branch_targets。核对 Go `base_workflow.go` 的对应方法：

```bash
grep -n "AddConditionalEdges\|BranchRouter\|SetSession" internal/openuapclaw/core/workflow/base_workflow.go
```

- [ ] **Step 6.2: 按结果处置（规则 13.2，不许默默跳过）**
  - 已覆盖（BranchRouter 特判+登记在位）→ 记录核对结论到 spec §5
  - 缺失 → 提出讨论（三档：本批补 / ⤵️ 挂指定节 / 不修），经用户确认后处置
- [ ] **Step 6.3: 结论写入 spec §5（不提交代码则无 commit）**

---

### Task 7: 集成测试 + 全量回归

**Files:** Create `tests/integration/agentcore/workflow_branch_comp/branch_comp_suite_test.go`

- [ ] **Step 7.1: 写集成套件**（`//go:build integration` + `suite.Run(`，真实跨 ≥3 子系统：workflow 门面 + graph/pregel 引擎 + condition 求值 + tracer）

场景：
1. **双出口命中表达式分支**：`start → branch(条件命中 end_a) → end_a/end_b`，断言产出走 end_a、trace 含 branch_id
2. **AlwaysTrue 兜底**：表达式不命中走 default 分支
3. **全不命中报错**：断言 COMPONENT_BRANCH_EXECUTION_ERROR（101021）从节点任务失败链上抛
4. **CNF barrier 编译**：下游节点 wait_for_all + 双入（分支互斥出口 + 普通入口）编译通过

套件骨架（对齐 `tests/integration/agentcore/workflow_llm_comp/` 既有套件结构，suite 基座/mock 边界照抄该目录惯例）：

```go
//go:build integration

package workflow_branch_comp

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type BranchCompSuite struct{ suite.Suite }

func TestBranchCompSuite(t *testing.T) { suite.Run(t, new(BranchCompSuite)) }

func (s *BranchCompSuite) Test双出口_表达式命中() {
	// 真实 Workflow + BranchComponent + Start/End 全链（8.14 门面 Invoke），
	// 断言 end_a 路径产出 + tracer 分支段
}

func (s *BranchCompSuite) TestAlwaysTrue兜底() { ... }
func (s *BranchCompSuite) Test全不命中_101021上抛() { ... }
func (s *BranchCompSuite) TestCNFBarrier编译与执行() { ... }
```

- [ ] **Step 7.2: 本地自检两层门禁**

```bash
go run ./cmd/integration-audit -verify
```
Expected: PASS（审计跨度 ≥3）

- [ ] **Step 7.3: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
```
Expected: 零 FAIL

- [ ] **Step 7.4: 提交**

```bash
git add tests/integration/agentcore/workflow_branch_comp/
git commit -m "test(integration): workflow_branch_comp 集成套件（8.19 Task7：双出口/兜底/101021/CNF）"
```

---

### Task 8: 文档收尾

**Files:** Modify `components/condition/doc.go`（Task 1 已建，确认文件树完整）、`components/flow/doc.go`（核对是否已有；无则建）、`IMPLEMENTATION_PLAN.md`

- [ ] **Step 8.1: doc.go 文件树补全**（规则 1：新增文件须入树；condition/doc.go 与 flow/doc.go 各自核）

- [ ] **Step 8.2: IMPLEMENTATION_PLAN.md 8.19 行写 Go 事实**——格式对齐 8.18 行：包路径、文件清单、逐符号映射、`偏差 D-19-1（drawable 段跳过 ⤵️8.13）/D-19-2（元组分支 ⤵️8.20）`、覆盖率数字、spec/plan 路径、Task6 核对结论

- [ ] **Step 8.3: 追加十八次注记**（注记区原注记保留）：

```markdown
> **8.x 实施顺序（2026-11-21 十八次记录）**：8.19 一次收口（三拍板：范围 A——number/array 留 8.20；表达式手写 parser 语义一比一；drawable 段跳过 D-19-1 ⤴️8.13 回填）；8.10 D1 回收兑现（Condition 走 AtomicInvoke 链）；下一节 8.20（含 number.py/array.py），8.13 继续压后并标注 drawable 缝待回填。原注记保留。
```

- [ ] **Step 8.4: 8.13 行/计划文档标注待回填**：8.13 计划文档（若已存在）或 IMPLEMENTATION_PLAN 8.13 行加注 `⤴️ 8.19 跳过 drawable 缝（D-19-1），待本节回填 BranchRouter drawable 分支`

- [ ] **Step 8.5: 覆盖率核验**

```bash
go test -cover ./internal/openuapclaw/core/workflow/components/condition/ ./internal/openuapclaw/core/workflow/components/flow/
```
Expected: 两包 ≥85%

- [ ] **Step 8.6: 最终提交**

```bash
git add docs/superpowers/specs/2026-11-21-branch-comp-8.19-design.md IMPLEMENTATION_PLAN.md internal/openuapclaw/core/workflow/components/
git commit -m "docs(plan): 8.19 收尾——IMPLEMENTATION_PLAN Go 事实+十八次注记+8.13 drawable 待回填标注"
git push
```

---

## 删除清单（规则 8）

本节为纯新增，无被替代旧实现；Task 6 若补 8.14 特判路径，同改动删除对应占位（若有）。锚点核查：无。

## 已知风险

1. **plan 中代码为骨架级**——常量名（`StatusExpressionEvalError` 等）、tracer 签名、state 接口形状以 grep 实名取准；语义与步骤结构以 spec/Python 源码为准，实施者须逐文件对照 Python 原文（规则 9/12）。
2. **Python `is` 语义**：Go 值相等近似，spec §7 已记；`pyEqual` 对 bool→数值归一对齐 Python `True == 1`。
3. **BoolOp 全量求值**（非短路）逐字复刻——上游如此，勿"优化"。
