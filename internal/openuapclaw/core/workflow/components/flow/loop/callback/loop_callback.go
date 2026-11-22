// Package callback 提供循环组件的四阶段回调件（LoopCallback 基类 + Output/
// IntermediateLoopVar 两个标准实现），全部走 8.10 AtomicNode 原子链写 state。
//
// 对应 Python 代码：openjiuwen/core/workflow/components/flow/loop/callback/
//
// 文件目录：
//
//	callback/
//	├── doc.go                    # 包文档
//	├── loop_callback.go          # LoopCallback 基座 + 四阶段常量
//	├── output.go                 # OutputCallback 轮次结果累积与收割
//	└── intermediate_loop_var.go  # IntermediateLoopVarCallback 中间变量注入
package callback

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
)

// ──────────────────────────── 常量 ────────────────────────────

// 四阶段常量（loop_callback.py:11-14 逐字）
const (
	// StageFirstLoop 首轮（Python FIRST_LOOP = "first_in_loop"）
	StageFirstLoop = "first_in_loop"
	// StageStartRound 每轮开始前（Python START_ROUND = "start_round"）
	StageStartRound = "start_round"
	// StageEndRound 每轮结束后（Python END_ROUND = "end_round"）
	StageEndRound = "end_round"
	// StageOutLoop 跳出循环（Python OUT_LOOP = "out_loop"）
	StageOutLoop = "out_loop"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopCallback 循环回调基座（Python loop_callback.py:16 LoopCallback(AtomicNode)
// 抽象基类 → Go 组合嵌入 AtomicNode；偏差 D3 同款：四阶段抽象方法 → 构造期回调槽，
// 任一 nil panic ≙ 抽象方法必实现）。
type LoopCallback struct {
	graph.AtomicNode
	// FirstInLoopFn 首轮回调槽（Python first_in_loop 抽象方法）
	FirstInLoopFn func(sess session.BaseSession) (any, error)
	// StartRoundFn 每轮开始回调槽（Python start_round 抽象方法）
	StartRoundFn func(sess session.BaseSession) (any, error)
	// EndRoundFn 每轮结束回调槽（Python end_round 抽象方法）
	EndRoundFn func(sess session.BaseSession, loopTimes int) (any, error)
	// OutLoopFn 跳出回调槽（Python out_loop 抽象方法）
	OutLoopFn func(sess session.BaseSession) (any, error)
}

// NewLoopCallback 构造回调基座。四槽任一 nil panic（Python 抽象方法必实现约束）。
func NewLoopCallback(
	first func(session.BaseSession) (any, error),
	start func(session.BaseSession) (any, error),
	end func(session.BaseSession, int) (any, error),
	out func(session.BaseSession) (any, error),
) LoopCallback {
	if first == nil || start == nil || end == nil || out == nil {
		panic("四阶段回调槽不能为 nil（对齐 Python 抽象方法必须实现）")
	}
	lc := LoopCallback{FirstInLoopFn: first, StartRoundFn: start, EndRoundFn: end, OutLoopFn: out}
	lc.AtomicNode = graph.NewAtomicNode(func(_ context.Context, kwargs map[string]any) (any, error) {
		// Python loop_callback.py:19-32 _atomic_invoke 一比一
		stage, _ := kwargs["loop_stage"].(string)
		sess, _ := kwargs["session"].(session.BaseSession)
		loopTimes, _ := kwargs["loop_times"].(int)
		var output any
		var err error
		switch stage {
		case StageFirstLoop:
			output, err = lc.FirstInLoopFn(sess)
		case StageStartRound:
			output, err = lc.StartRoundFn(sess)
		case StageEndRound:
			output, err = lc.EndRoundFn(sess, loopTimes)
		default:
			output, err = lc.OutLoopFn(sess)
		}
		if err != nil {
			return nil, err
		}
		// Python: if output is not None: session.state().set_outputs(output)
		if output != nil {
			ws, ok := sess.State().(state.WorkflowState)
			if !ok {
				return nil, fmt.Errorf("session is not support commit state")
			}
			m, isMap := output.(map[string]any)
			if !isMap {
				return nil, fmt.Errorf("set_outputs expects dict, got %T (Python set_outputs 契约)", output)
			}
			ws.SetOutputs(m)
		}
		return nil, nil // Python: return None
	})
	return lc
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Call 触发回调（Python __call__(loop_stage, session, loop_times=None) → atomic_invoke）。
func (l *LoopCallback) Call(ctx context.Context, stage string, sess session.BaseSession, loopTimes *int) error {
	kwargs := map[string]any{"loop_stage": stage, "session": sess}
	if loopTimes != nil {
		kwargs["loop_times"] = *loopTimes
	}
	_, err := l.AtomicInvoke(ctx, kwargs)
	return err
}
