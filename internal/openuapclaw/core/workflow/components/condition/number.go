package condition

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NumberCondition 数字条件（死代码，拍板 1 复刻；number.py:10-17）。
// 偏差 D-20-4：Python Union[str, int] → Go any 槽；invoke 比较对象是 inputs 的
// 鸭子怪癖逐字保留（_limit 存而不用，number.py:14 limit_num = inputs）。
type NumberCondition struct {
	BaseCondition
	// limit 上限（Python self._limit，存而不用）
	limit any
}

// NewNumberCondition 构造（number.py:11-13：super().__init__(limit) 传 input_schema）。
func NewNumberCondition(limit any) *NumberCondition {
	c := &NumberCondition{limit: limit}
	c.BaseCondition = NewBaseCondition(c.invokeImpl, nil)
	return c
}

// Invoke 条件求值（Python invoke）。
func (c *NumberCondition) Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error) {
	flag, _, err := c.invokeImpl(ctx, inputs, sess)
	return flag, err
}

// TraceInfo 默认空串（Python Condition.trace_info 默认 ""）。
func (c *NumberCondition) TraceInfo(_ session.BaseSession) any { return "" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeImpl invoke 本体（number.py:14-16 一比一）。
// Python: limit_num = inputs（dict）→ `current_idx < limit_num` 必然 TypeError
// 上抛——Go 以等价错误表达（偏差 D-20-4），不走真实比较。
func (c *NumberCondition) invokeImpl(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, map[string]any, error) {
	if _, err := conditionIndex(sess); err != nil {
		return false, nil, err
	}
	// Python: dict < int → TypeError；Go map 不支持比较 → 等价上抛
	return false, nil, fmt.Errorf("'<' not supported between instances of 'dict' and 'int' (Python TypeError 语义保留，D-20-4)")
}

// NumberConditionInSession 会话版数字条件（loop_comp.py:191 消费；number.py:19-26）。
type NumberConditionInSession struct {
	BaseCondition
	// limit 上限（Python self._limit）
	limit int
}

// NewNumberConditionInSession 构造。Python limit None → ValueError（number.py:23-24）；
// Go 构造期收窄 int 后该分支为不可达防御，注释留档。
func NewNumberConditionInSession(limit int) *NumberConditionInSession {
	c := &NumberConditionInSession{limit: limit}
	c.BaseCondition = NewBaseCondition(func(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, map[string]any, error) {
		// Python: current_idx < limit_num（裸 bool，无 tuple）
		cur, err := conditionIndex(sess)
		if err != nil {
			return false, nil, err
		}
		return cur < c.limit, nil, nil
	}, nil)
	return c
}

// Invoke 条件求值（Python invoke）。
func (c *NumberConditionInSession) Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (bool, error) {
	flag, _, err := c.invokeImpl(ctx, inputs, sess)
	return flag, err
}

// TraceInfo 默认空串（Python Condition.trace_info 默认 ""）。
func (c *NumberConditionInSession) TraceInfo(_ session.BaseSession) any { return "" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeImpl invoke 本体（number.py:20-25 一比一）。
func (c *NumberConditionInSession) invokeImpl(_ context.Context, _ map[string]any, sess session.BaseSession) (bool, map[string]any, error) {
	// Python: limit_num = self._limit; if limit_num is None: raise ValueError（Go 构造期收窄，不可达防御）
	cur, err := conditionIndex(sess)
	if err != nil {
		return false, nil, err
	}
	return cur < c.limit, nil, nil
}
