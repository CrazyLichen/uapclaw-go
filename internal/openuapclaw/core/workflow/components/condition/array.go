package condition

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// DefaultMaxLoopNumber 数组循环长度兜底上限（array.py:10 DEFAULT_MAX_LOOP_NUMBER=1000）
const DefaultMaxLoopNumber = 1000

// ──────────────────────────── 结构体 ────────────────────────────

// ArrayCondition 数组条件（死代码，拍板 1 复刻；array.py:12-27）。
// 鸭子怪癖逐字保留：invoke 按 inputs.get(key, []) 取数组，而 inputs 恒按
// input_schema 取——min_length 实际不随构造期 arrays 变（Python 如此，不修）。
type ArrayCondition struct {
	BaseCondition
	// arrays 构造期数组映射（Python self._arrays）
	arrays map[string]any
}

// NewArrayCondition 构造（array.py:13-15：super().__init__(arrays) 传 input_schema）。
func NewArrayCondition(arrays map[string]any, inputSchema any) *ArrayCondition {
	c := &ArrayCondition{arrays: arrays}
	c.BaseCondition = NewBaseCondition(c.invokeImpl, inputSchema)
	return c
}

// Invoke 条件求值（Python invoke；ArrayCondition 走默认 trace_info ""）。
func (c *ArrayCondition) Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error) {
	flag, _, err := c.invokeImpl(ctx, inputs, sess)
	return flag, err
}

// TraceInfo 默认空串（Python Condition.trace_info 默认 ""）。
func (c *ArrayCondition) TraceInfo(_ session.BaseSession) any { return "" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeImpl invoke 本体（array.py:16-27 一比一）。
func (c *ArrayCondition) invokeImpl(_ context.Context, inputs map[string]any, sess session.BaseSession) (bool, map[string]any, error) {
	// Python: current_idx = session.state().get(INDEX)
	cur, err := conditionIndex(sess)
	if err != nil {
		return false, nil, err
	}
	minLength := DefaultMaxLoopNumber
	updates := map[string]any{}
	for key := range c.arrays {
		arr, _ := inputs[key].([]any) // Python: arr = inputs.get(key, [])
		if len(arr) < minLength {
			minLength = len(arr)
		}
		if cur >= minLength {
			return false, nil, nil // Python: return False（裸 bool）
		}
		updates[key] = arr[cur]
	}
	// Python: session.state().update(updates); io_updates = updates.copy()
	// Python: session.state().update(...) —— update 属 SessionState 基面，直调
	if err := sess.State().Update(updates); err != nil {
		return false, nil, err
	}
	return true, updates, nil // Python: return True, io_updates（tuple）
}

// ArrayConditionInSession 会话版数组条件（loop_comp.py:177 消费；array.py:29-60）。
type ArrayConditionInSession struct {
	BaseCondition
	// arrays 构造期数组映射（Python self._arrays）
	arrays map[string]any
	// minLength 构造期算出的最短数组长度（array.py:33 self._min_length = _check_arrays）
	minLength int
}

// NewArrayConditionInSession 构造（array.py:30-33：super().__init__() 无 schema；
// _check_arrays 构造期校验并算 min_length，None/非 list|tuple 即 ValueError）。
func NewArrayConditionInSession(arrays map[string]any) (*ArrayConditionInSession, error) {
	minLength, err := checkArrays(arrays)
	if err != nil {
		return nil, err
	}
	c := &ArrayConditionInSession{arrays: arrays, minLength: minLength}
	c.BaseCondition = NewBaseCondition(c.invokeImpl, nil)
	return c, nil
}

// Invoke 条件求值（Python invoke）。
func (c *ArrayConditionInSession) Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error) {
	flag, _, err := c.invokeImpl(ctx, inputs, sess)
	return flag, err
}

// TraceInfo 默认空串（Python Condition.trace_info 默认 ""）。
func (c *ArrayConditionInSession) TraceInfo(_ session.BaseSession) any { return "" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeImpl invoke 本体（array.py:31-46 一比一）。
func (c *ArrayConditionInSession) invokeImpl(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, map[string]any, error) {
	// Python: current_idx = session.state().get(INDEX)
	cur, err := conditionIndex(sess)
	if err != nil {
		return false, nil, err
	}
	if cur >= c.minLength {
		return false, nil, nil // array.py:34-35 裸 False
	}
	updates := map[string]any{}
	for key, info := range c.arrays {
		// Python: except (TypeError, IndexError, KeyError) → ValueError("Array loop error in '%s': <error_details>")
		arr, ok := info.([]any)
		if !ok {
			return false, nil, fmt.Errorf("Array loop error in '%s': <error_details>", key)
		}
		if cur >= len(arr) {
			return false, nil, fmt.Errorf("Array loop error in '%s': <error_details>", key)
		}
		updates[key] = arr[cur]
	}
	// Python: session.state().update(updates); io_updates = updates.copy()
	// Python: session.state().update(...) —— update 属 SessionState 基面，直调
	if err := sess.State().Update(updates); err != nil {
		return false, nil, err
	}
	return true, updates, nil
}

// checkArrays 构造期校验（array.py:52-59 _check_arrays 一比一）。
func checkArrays(arrays map[string]any) (int, error) {
	minLength := DefaultMaxLoopNumber
	for key, info := range arrays {
		if info == nil {
			return 0, fmt.Errorf("Value for key '%s' in loop_array cannot be None", key)
		}
		if _, ok := info.([]any); !ok {
			return 0, fmt.Errorf("Expected list/tuple for '%s' in loop_array, got %T", key, info)
		}
		if len(info.([]any)) < minLength {
			minLength = len(info.([]any))
		}
	}
	return minLength, nil
}

// conditionIndex 读循环轮次（Python state().get(INDEX)）。INDEX 未初始化（None）
// 时 Python 与 int 比较会 raise TypeError——真实链路中 _condition_invoke 已先初始化，
// 此处等价上抛防静默错轮。
func conditionIndex(sess session.BaseSession) (int, error) {
	v := sess.State().Get(state.StringKey(constants.Index))
	if v == nil {
		return 0, fmt.Errorf("loop index is not initialized (None cannot compare with int in Python)")
	}
	idx, ok := v.(int)
	if !ok {
		return 0, fmt.Errorf("loop index is not int, got %T", v)
	}
	return idx, nil
}
