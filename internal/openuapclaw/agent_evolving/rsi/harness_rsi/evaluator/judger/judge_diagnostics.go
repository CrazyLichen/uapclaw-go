package judger

// 单次裁判评估尝试的完成元数据采集。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/judge_diagnostics.py（34 行）。
// Python 用 ContextVar 承载采集列表；Go 以 context.Context 值承载（偏差登记：
// contextvars token → context 键，作用域由 ctx 派生保证）。

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// judgeCallsKeyType 诊断采集列表的 context 键类型。
type judgeCallsKeyType struct{}

// judgeCallsKey 采集列表 context 键。
var judgeCallsKey judgeCallsKeyType

// ──────────────────────────── 全局变量 ────────────────────────────

// JudgeDiagnosticFields 用量诊断字段清单（Python fields 元组）。
var JudgeDiagnosticFields = []string{"input_tokens", "output_tokens", "total_tokens", "reasoning_tokens"}

// ──────────────────────────── 导出函数 ────────────────────────────

// CollectJudgeModelCalls 开启一次评估尝试的模型调用采集。
// Python: judge_diagnostics.py collect_judge_model_calls——返回携带采集列表的
// ctx 与读取句柄；defer cancel() 对齐 contextmanager finally reset。
func CollectJudgeModelCalls(ctx context.Context) (context.Context, *JudgeCalls, func()) {
	calls := &JudgeCalls{Items: []map[string]any{}}
	return context.WithValue(ctx, judgeCallsKey, calls), calls, func() {}
}

// RecordJudgeModelResponse 记录一次模型响应的完成元数据（未开启采集时忽略）。
// Python: judge_diagnostics.py record_judge_model_response。
func RecordJudgeModelResponse(ctx context.Context, finishReason string, usage *llmschema.UsageMetadata) {
	value := ctx.Value(judgeCallsKey)
	if value == nil {
		return
	}
	calls, _ := value.(*JudgeCalls)
	if calls == nil {
		return
	}
	diagnostic := map[string]any{}
	// Python: finish_reason 存在且不等于 "null" 才记录。
	if finishReason != "" && finishReason != llmschema.FinishReasonNull {
		diagnostic["finish_reason"] = finishReason
	}
	if usage != nil {
		usageMap := map[string]any{}
		// Python isinstance(getattr(usage, key, None), (int, float))：0 值也是合法
		// int 保留；reasoning_tokens 缺省 None 剔除——Go 用指针承载缺省语义。
		usageMap["input_tokens"] = float64(usage.InputTokens)
		usageMap["output_tokens"] = float64(usage.OutputTokens)
		usageMap["total_tokens"] = float64(usage.TotalTokens)
		if usage.ReasoningTokens != nil {
			usageMap["reasoning_tokens"] = float64(*usage.ReasoningTokens)
		}
		if len(usageMap) > 0 {
			diagnostic["usage"] = usageMap
		}
	}
	if len(diagnostic) > 0 {
		calls.Items = append(calls.Items, diagnostic)
	}
}

// ──────────────────────────── 结构体 ────────────────────────────

// JudgeCalls 单次评估尝试内采集到的模型调用诊断。
type JudgeCalls struct {
	// Items 逐次调用的诊断条目
	Items []map[string]any
}
