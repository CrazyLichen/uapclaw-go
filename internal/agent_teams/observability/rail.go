package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// spanKey task_iteration span 在 railCtx.Extra 中的存储键
	// Python: _SPAN_KEY = "_otel_task_iter_span"
	spanKey = "_otel_task_iter_span"
	// railTracerName Rail 层 tracer 名称
	// Python: _TRACER_NAME = "openjiuwen.agent_teams.observability.rail"
	railTracerName = "openjiuwen.agent_teams.observability.rail"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ObservabilityRail DeepAgent 可观测性 Rail。
// Python: ObservabilityRail(DeepAgentRail) (rail.py)
//
// 仅覆盖 task_iteration 边界（Callback 层未覆盖的缺口）。
// LLM/Tool/Agent 生命周期钩子故意不覆盖——OtelCallbackHandler 已处理，避免重复 span。
type ObservabilityRail struct {
	rails.DeepAgentRail
	// injectedTracer 可选显式注入的 tracer（测试用）
	injectedTracer trace.Tracer
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewObservabilityRail 创建 ObservabilityRail。
// Python: ObservabilityRail(tracer=...)
func NewObservabilityRail(tracer trace.Tracer) *ObservabilityRail {
	r := &ObservabilityRail{
		injectedTracer: tracer,
	}
	r.DeepAgentRail = *rails.NewDeepAgentRail()
	return r
}

// BeforeTaskIteration 每个 task_iteration 开始时：打开 span。
// Python: ObservabilityRail.before_task_iteration(ctx)
func (r *ObservabilityRail) BeforeTaskIteration(_ context.Context, railCtx *agentinterfaces.AgentCallbackContext) error {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("error", rec).Msg("otel rail before_task_iteration 异常")
		}
	}()

	// Python: inputs = ctx.inputs
	inputs := railCtx.Inputs()
	// Python: iteration = int(getattr(inputs, "iteration", 0) or 0)
	iteration := 0
	if inputs != nil {
		iteration = extractIteration(inputs)
	}
	// Python: is_follow_up = bool(getattr(inputs, "is_follow_up", False))
	isFollowUp := extractIsFollowUp(inputs)

	// Python: span = self._tracer().start_span(name=f"deepagent.task_iteration.{iteration}", kind=SpanKind.INTERNAL)
	_, span := r.tracer().Start(context.Background(),
		fmt.Sprintf("deepagent.task_iteration.%d", iteration),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	// Python: span.set_attribute(DA_TASK_ITERATION, iteration)
	span.SetAttributes(
		attribute.Int(DATaskIteration, iteration),
		attribute.Bool(DATaskIsFollowUp, isFollowUp),
	)
	// Python: ctx.extra[_SPAN_KEY] = span
	railCtx.Extra()[spanKey] = span
	return nil
}

// AfterTaskIteration 每个 task_iteration 结束时：关闭 span。
// Python: ObservabilityRail.after_task_iteration(ctx)
func (r *ObservabilityRail) AfterTaskIteration(_ context.Context, railCtx *agentinterfaces.AgentCallbackContext) error {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("error", rec).Msg("otel rail after_task_iteration 异常")
		}
	}()

	// Python: span: Span | None = ctx.extra.pop(_SPAN_KEY, None)
	spanVal, ok := railCtx.Extra()[spanKey]
	if !ok {
		return nil
	}
	span, ok := spanVal.(trace.Span)
	if !ok {
		return nil
	}
	delete(railCtx.Extra(), spanKey)

	// Python: if ctx.exception is not None: span.record_exception / set_status(ERROR)
	if railCtx.Exception() != nil {
		span.RecordError(railCtx.Exception())
		span.SetStatus(codes.Error, railCtx.Exception().Error())
	} else {
		// Python: span.set_status(Status(StatusCode.OK))
		span.SetStatus(codes.Ok, "")
	}
	// Python: span.end()
	span.End()
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tracer 解析 tracer。
// Python: ObservabilityRail._tracer()
func (r *ObservabilityRail) tracer() trace.Tracer {
	if r.injectedTracer != nil {
		return r.injectedTracer
	}
	return GetTracer(railTracerName)
}

// extractIteration 从 inputs 中提取 iteration 数值。
func extractIteration(inputs agentinterfaces.EventInputs) int {
	type iterGetter interface{ GetIteration() int }
	if ig, ok := inputs.(iterGetter); ok {
		return ig.GetIteration()
	}
	// 尝试从 map 类型取值
	type mapLike interface{ ToMap() map[string]any }
	if ml, ok := inputs.(mapLike); ok {
		if v, ok := ml.ToMap()["iteration"]; ok {
			if n, ok := v.(int); ok {
				return n
			}
		}
	}
	return 0
}

// extractIsFollowUp 从 inputs 中提取 is_follow_up 布尔值。
func extractIsFollowUp(inputs agentinterfaces.EventInputs) bool {
	type followUpGetter interface{ GetIsFollowUp() bool }
	if fg, ok := inputs.(followUpGetter); ok {
		return fg.GetIsFollowUp()
	}
	type mapLike interface{ ToMap() map[string]any }
	if ml, ok := inputs.(mapLike); ok {
		if v, ok := ml.ToMap()["is_follow_up"]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}
