package condition

import (
	"context"
	"reflect"
	"runtime"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
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
// invoke 方法 → Go 构造期回调槽，AtomicNode 偏差 D3 同款）。
// 偏差 D-19-2：_atomic_invoke 的元组分支（isinstance(result, tuple) → set_outputs）
// 生产者 ArrayCondition 属 8.20，本节不落，回收条件挂 8.20。
type BaseCondition struct {
	graph.AtomicNode
	// inputSchema 输入模式（Python __init__(input_schema=None)；本节三子类恒 nil，
	// 8.20 ArrayCondition 传 schema 时启用）
	inputSchema any
}

// NewBaseCondition 构造条件基座。invokeImpl 为 nil 时 panic（Python 抽象方法
// invoke 必须实现，AtomicNode 偏差 D3 同款）。
func NewBaseCondition(
	invokeImpl func(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error),
	inputSchema any,
) BaseCondition {
	if invokeImpl == nil {
		panic("invokeImpl 不能为 nil（对齐 Python 抽象方法 invoke 必须实现）")
	}
	b := BaseCondition{inputSchema: inputSchema}
	b.AtomicNode = graph.NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		// Python _atomic_invoke（condition.py:18-25）：session = kwargs["session"]
		sess, _ := kwargs["session"].(session.BaseSession)
		// Python: inputs = session.state().get_inputs(self._input_schema) if
		//         self._input_schema is not None else {}
		inputs := map[string]any{}
		if b.inputSchema != nil {
			ws, ok := sess.State().(state.WorkflowState)
			if !ok {
				return nil, exception.BuildError(exception.StatusGraphStateCommitError,
					exception.WithParam("reason", "session is not support commit state"))
			}
			inputs, _ = ws.GetInputs(b.inputSchema.(state.StateKey)).(map[string]any)
		}
		// Python: result = self.invoke(inputs=inputs, session=session)
		// 元组分支（result[1] → set_outputs）属 D-19-2，挂 8.20
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
	return runtimeFuncName(c.fn)
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

// TraceInfo 返回 "True"（Python trace_info 逐字复刻）。
func (c *AlwaysTrue) TraceInfo(_ session.BaseSession) any { return "True" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// runtimeFuncName 函数名提取（Python __name__ 等价面：FuncForPC 取符号路径后
// 截末段，对齐 Python 简名语义）。
func runtimeFuncName(fn any) string {
	full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	if idx := strings.LastIndex(full, "/"); idx >= 0 {
		full = full[idx+1:]
	}
	if idx := strings.LastIndex(full, "."); idx >= 0 {
		full = full[idx+1:]
	}
	return full
}
