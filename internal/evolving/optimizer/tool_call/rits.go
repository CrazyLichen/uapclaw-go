package tool_call

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// VerifyFunc 验证+解析函数类型。
// 接收 LLM 输出文本，返回解析后的对象；验证失败时返回 error 触发重试。
//
// 对齐 Python: verify_fn(output) — 成功返回解析后对象，失败抛异常触发 tenacity 重试
type VerifyFunc func(string) (any, error)

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
			"error": fmt.Sprintf("Cannot complete LLM call. Error: %v", err),
		}, nil
	}

	if verifyFn == nil {
		return raw, nil
	}

	return cachedResult, nil
}

// InvokeText 简单 LLM 文本调用（无 verify_fn）。
// 内部直接调用 llm_resilience.InvokeTextWithRetry。
//
// 对齐 Python: rits_response(model_id, prompt, llm_api_key) 不带 verify_fn 的情况
func InvokeText(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
) (string, error) {
	return llm_resilience.InvokeTextWithRetry(ctx, model, modelName, prompt, policy)
}

// InvokeFunctionCall 使用 LLM Function Calling 模式生成函数调用。
// SimpleEval 专用，直接调用 model.Invoke + WithTools。
//
// 对齐 Python: SimpleEval._generate_function_call 中
//
//	api_response = anyio.run(lambda: client.invoke(
//	    messages=[{"role": "user", "content": instruction}],
//	    tools=[{"type": "function", "function": tool}],
//	))
//	fn_args = api_response.tool_calls[0].arguments
//	function_name = api_response.tool_calls[0].name
func InvokeFunctionCall(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	instruction string,
	toolInfo cschema.ToolInfoInterface,
) (map[string]any, error) {
	messages := model_clients.NewMessagesParam(
		llmschema.NewUserMessage(instruction),
	)
	response, err := model.Invoke(ctx, messages,
		model_clients.WithInvokeModel(modelName),
		model_clients.WithTools(toolInfo),
	)
	if err != nil {
		return nil, fmt.Errorf("function calling failed: %w", err)
	}

	// 对齐 Python: api_response.tool_calls
	toolCalls := response.ToolCalls
	if len(toolCalls) == 0 {
		return nil, fmt.Errorf("LLM did not generate any tool calls")
	}

	// 对齐 Python: fn_args = api_response.tool_calls[0].arguments
	// 对齐 Python: function_name = api_response.tool_calls[0].name
	tc := toolCalls[0]
	var args map[string]any
	if tc.Arguments != "" {
		if jsonErr := json.Unmarshal([]byte(tc.Arguments), &args); jsonErr != nil {
			args = map[string]any{}
		}
	}

	return map[string]any{
		"name":      tc.Name,
		"arguments": args,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	invokeWithVerifyImpl = invokeWithVerifyDefault
}
