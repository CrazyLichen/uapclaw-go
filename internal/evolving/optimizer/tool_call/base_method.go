package tool_call

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseMethod Beam Search 方法基类。
// 提供公共配置和 ProduceAnswerFromAPICall 方法。
//
// 对应 Python: BaseMethod
type BaseMethod struct {
	// config 配置字典
	config map[string]any
	// verbose 是否输出详细日志
	verbose bool
	// model LLM 模型客户端
	model *llm.Model
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent tool_call 包日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseMethod 创建 BaseMethod 实例。
//
// 对齐 Python: BaseMethod.__init__(self, config)
func NewBaseMethod(config map[string]any, model *llm.Model) *BaseMethod {
	verbose := false
	if v, ok := config["verbose"]; ok {
		if vi, ok := v.(int); ok && vi > 0 {
			verbose = true
		}
		if vb, ok := v.(bool); ok {
			verbose = vb
		}
	}
	return &BaseMethod{
		config:  config,
		verbose: verbose,
		model:   model,
	}
}

// ProduceAnswerFromAPICall 根据 API 调用结果生成自然语言回答。
//
// 对齐 Python: BaseMethod.produce_answer_from_api_call(instruction, doc_str, api_response)
//
//	1. 构建提示词（一比一复刻 Python 原文）
//	2. 使用 InvokeWithVerify 调用 LLM
//	3. 验证输出包含 "answer" 字段
func (m *BaseMethod) ProduceAnswerFromAPICall(
	ctx context.Context,
	instruction string,
	docStr string,
	apiResponse string,
) (string, error) {
	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
Please respond in natural language text. Do not include code in your responses. You are given an API tool with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

Documentation:
%s

You are given the following instruction: "%s"
To produce a response to the instruction, you made an API call to the given tool, which returned the following results:
%s

Given the instruction and the results of API call, produce an effective and short answer (less than 300 letters) to the user in natural language. Your answer must be based on the results of the API call, do not hallucinate or answer anything not in the API results. You must not include code, comments, JSON data structures, notes, or other irrelevant information in your answer. If there is an error or failure using the tool, you must report the error in your answer and do not make things up, especially when you receive an input about invalid parameters. Also, absolutely do NOT tell a user about a simulated response. Treat every successful API output as real. Every successful API call contains real data. This is very important.

Finally, organize your output in the following JSON format:
{
    "answer": answer
}
You must strictly follow the output format. You can begin your task now.`,
		docStr, instruction, apiResponse)

	// 对齐 Python: prompt = format_prompt_llama(system_prompt="", user_prompt=user_prompt)
	prompt := FormatPromptLlama("", userPrompt)

	// 对齐 Python: verify_output(output)
	verifyFn := func(output string) (any, error) {
		outputJSON := ParseJSON(output, "answer")
		if _, ok := outputJSON["answer"]; !ok {
			return nil, fmt.Errorf("answer field is required")
		}
		if _, hasErr := outputJSON["error"]; hasErr {
			return nil, fmt.Errorf("%v", outputJSON["error"])
		}
		answer, _ := outputJSON["answer"].(string)
		return answer, nil
	}

	// 对齐 Python: get_rits_response(config['gen_model_id'], prompt, config['llm_api_key'], verify_output, max_attempts=15, ...)
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:      15,
		TotalBudgetSecs:  300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:  1.0,
	}

	modelName := getConfigString(m.config, "gen_model_id")
	result, err := InvokeWithVerify(ctx, m.model, modelName, prompt, policy, verifyFn)
	if err != nil {
		return "", err
	}

	answer, ok := result.(string)
	if !ok {
		// 如果是 error 字典
		if errMap, ok := result.(map[string]any); ok {
			if errMsg, hasErr := errMap["error"]; hasErr {
				return "", fmt.Errorf("%v", errMsg)
			}
		}
		return fmt.Sprintf("%v", result), nil
	}
	return answer, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getConfigString 从配置字典获取字符串值。
func getConfigString(config map[string]any, key string) string {
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getConfigInt 从配置字典获取整数值。
func getConfigInt(config map[string]any, key string) int {
	if v, ok := config[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return 0
}

// getConfigFloat 从配置字典获取浮点数值。
func getConfigFloat(config map[string]any, key string) float64 {
	if v, ok := config[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0.0
}

// toIntSafe 安全地将 any 转为 int（不返回 error）。
// 供 example_method.go 等同包文件使用。
func toIntSafe(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// toJSON 将对象序列化为 JSON 字符串。
func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// toString 安全地将 any 转为 string。
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
