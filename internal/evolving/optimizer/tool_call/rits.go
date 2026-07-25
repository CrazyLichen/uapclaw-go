package tool_call

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerifyFunc 验证+解析函数类型。
// 接收 LLM 输出文本，返回解析后的对象；验证失败时返回 error 触发重试。
//
// 对齐 Python: verify_fn(output) — 成功返回解析后对象，失败抛异常触发 tenacity 重试
type VerifyFunc func(string) (any, error)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// invokeWithVerifyImpl InvokeWithVerify 的实际实现。
// 可在测试中替换为 mock 实现。
var invokeWithVerifyImpl func(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
	verifyFn VerifyFunc,
) (any, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// InvokeWithVerify 带验证的 LLM 文本调用。
// 复用 llm_resilience.InvokeTextWithRetry，将 Python 的 verify_fn 适配为
// isResultUsable（验证文本合法性）+ parseResult（解析验证后的结果）两步。
//
// 对齐 Python: get_rits_response(model_id, prompt, api_key, verify_fn, max_attempts, ...)
//
// 适配逻辑：
//
//	verifyFn 失败 → isResultUsable 返回 false → 触发 llm_resilience 重试
//	verifyFn 成功 → 缓存 parsedResult → isResultUsable 返回 true
//	最终返回缓存的 parsedResult
//
// 对齐 Python get_rits_response 的异常吞没行为：
// 所有 LLM 调用失败都返回 {'error': '...'} 字典，不抛异常
func InvokeWithVerify(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
	verifyFn VerifyFunc,
) (any, error) {
	return invokeWithVerifyImpl(ctx, model, modelName, prompt, policy, verifyFn)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeWithVerifyDefault InvokeWithVerify 的默认实现。
func invokeWithVerifyDefault(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
	verifyFn VerifyFunc,
) (any, error) {
	var cachedResult any

	isResultUsable := func(text string) bool {
		if verifyFn == nil {
			return true
		}
		result, err := verifyFn(text)
		if err != nil {
			return false
		}
		cachedResult = result
		return true
	}

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, model, modelName, prompt, policy,
		llm_resilience.WithIsResultUsable(isResultUsable),
	)
	if err != nil {
		// 对齐 Python get_rits_response: return {'error': f"Cannot complete LLM call. Error: {e}"}
		return map[string]any{
			"error": fmt.Sprintf("无法完成 LLM 调用，错误: %v", err),
		}, nil
	}

	if verifyFn == nil {
		return raw, nil
	}

	return cachedResult, nil
}

// init 初始化默认实现
func init() {
	invokeWithVerifyImpl = invokeWithVerifyDefault
}
