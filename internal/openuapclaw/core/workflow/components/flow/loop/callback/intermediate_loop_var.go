package callback

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IntermediateLoopVarCallback 中间变量注入回调（Python intermediate_loop_var.py:9
// IntermediateLoopVarCallback(LoopCallback)）：首轮把中间变量写进 state，让循环体内
// 节点能引用（${loop.user_num} 或 ${loop.intermediateLoopVar.ss}）。
type IntermediateLoopVarCallback struct {
	LoopCallback
	// intermediateLoopVar 变量映射（Python self.intermediate_loop_var）
	intermediateLoopVar map[string]any
	// intermediateLoopVarRoot 根包装键（Python self.intermediate_loop_var_root，默认 ""）
	intermediateLoopVarRoot string
}

// NewIntermediateLoopVarCallback 构造（intermediate_loop_var.py:10-13 一比一）。
func NewIntermediateLoopVarCallback(m map[string]any, root string) *IntermediateLoopVarCallback {
	c := &IntermediateLoopVarCallback{intermediateLoopVar: m, intermediateLoopVarRoot: root}
	c.LoopCallback = NewLoopCallback(c.firstInLoop, c.startRound, c.endRound, c.outLoop)
	return c
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// firstInLoop 首轮注入（intermediate_loop_var.py:13-23 一比一）：
// get_inputs(映射) → 非 nil update+commit → root 非空时包装 {root: localVars} 返回。
func (c *IntermediateLoopVarCallback) firstInLoop(sess session.BaseSession) (any, error) {
	// Python: local_vars = session.state().get_inputs(self.intermediate_loop_var)
	var localVars map[string]any
	if ws, ok := sess.State().(state.WorkflowState); ok {
		raw := ws.GetInputs(state.StringKey(""))
		if m, isMap := raw.(map[string]any); isMap {
			localVars = m
		}
		// Python: if local_vars: session.state().update(local_vars); commit()
		if len(localVars) > 0 {
			if err := sess.State().Update(localVars); err != nil {
				return nil, err
			}
			ws.Commit() // Python: session.state().commit()
		}
	}
	// Python: if self.intermediate_loop_var_root: local_vars = {root: local_vars}
	if c.intermediateLoopVarRoot != "" {
		localVars = map[string]any{c.intermediateLoopVarRoot: localVars}
	}
	return localVars, nil
}

// outLoop 无操作（intermediate_loop_var.py:26-27 return None）。
func (c *IntermediateLoopVarCallback) outLoop(_ session.BaseSession) (any, error) { return nil, nil }

// startRound 无操作（intermediate_loop_var.py:29-30 return None）。
func (c *IntermediateLoopVarCallback) startRound(_ session.BaseSession) (any, error) { return nil, nil }

// endRound 无操作（intermediate_loop_var.py:32-33 return None）。
func (c *IntermediateLoopVarCallback) endRound(_ session.BaseSession, _ int) (any, error) {
	return nil, nil
}
