package rails

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// processCliIterationLimitAfterPriority AFTER_REACT_ITERATION 上最后执行：
	// 先看别的 rail 是否已请求 finish，再认领「迭代已完成」。
	processCliIterationLimitAfterPriority = -1000
	// processCliIterationLimitBeforePriority BEFORE_MODEL_CALL 上最先执行：
	// 在普通模型钩子之前拒掉多余的那一轮模型调用。
	processCliIterationLimitBeforePriority = 1000
	// processCliIterationLimitOutput 超限终止时的输出说明。
	processCliIterationLimitOutput = "Agent reached its iteration limit before completion."
	// processCliIterationLimitErrorCode 超限终止的错误码。
	processCliIterationLimitErrorCode = "MAX_ITERATIONS_REACHED"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProcessCliIterationLimitRail 在配置的迭代边界停下，并把原因保留在结果里。
// Python: jiuwenswarm/runtime/iteration_limit.py ProcessCliIterationLimitRail
//
// 内层循环被配置为多跑一轮（configure_process_cli_run 里 max_iterations = limit + 1），
// 让最后一个被中断的工具能在下一个模型边界报告上限之前恢复；那一轮永远不会调模型。
// 成功的最终回答与更早的 force_finish 请求保持原结果。
type ProcessCliIterationLimitRail struct {
	*agentinterfaces.BaseRail
	// maxIterations 配置的迭代上限
	maxIterations int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProcessCliIterationLimitRail 构造迭代上限 rail。
// Python: ProcessCliIterationLimitRail(max_iterations)
func NewProcessCliIterationLimitRail(maxIterations int) *ProcessCliIterationLimitRail {
	return &ProcessCliIterationLimitRail{
		BaseRail:      agentinterfaces.NewBaseRail(),
		maxIterations: maxIterations,
	}
}

// CallbackPriorityFor 按事件返回分档优先级。
// Python: iteration_limit.py:26-29 callback_priority
//
//	-1000 if event == AFTER_REACT_ITERATION else 1000
func (r *ProcessCliIterationLimitRail) CallbackPriorityFor(event agentinterfaces.AgentCallbackEvent) int {
	if event == agentinterfaces.CallbackAfterReactIteration {
		return processCliIterationLimitAfterPriority
	}
	return processCliIterationLimitBeforePriority
}

// BeforeModelCall 超过上限时拒掉本轮模型调用。
// Python: iteration_limit.py:31-34 before_model_call
func (r *ProcessCliIterationLimitRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	// Python: react_iteration > self.max_iterations（严格大于）
	r.finish(cbc, inputs.ReactIteration > r.maxIterations)
	return nil
}

// AfterReactIteration 达到上限时请求终止。
// Python: iteration_limit.py:36-39 after_react_iteration
func (r *ProcessCliIterationLimitRail) AfterReactIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	// Python: react_iteration >= self.max_iterations
	r.finish(cbc, inputs.ReactIteration >= r.maxIterations)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// finish 已耗尽且无人请求终止且无异常时，请求 force_finish。
// Python: iteration_limit.py:41-48 _finish
func (r *ProcessCliIterationLimitRail) finish(cbc *agentinterfaces.AgentCallbackContext, exhausted bool) {
	if !exhausted || cbc.HasForceFinishRequest() || cbc.Exception() != nil {
		return
	}
	cbc.RequestForceFinish(map[string]any{
		"output":      processCliIterationLimitOutput,
		"result_type": "error",
		"code":        processCliIterationLimitErrorCode,
	})
}
