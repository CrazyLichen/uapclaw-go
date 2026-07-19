package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SimpleEval 评估包装器，生成函数调用并评估准确性和输出有效性。
//
// 对应 Python: SimpleEval
type SimpleEval struct {
	// apiWrapper API 调用封装
	apiWrapper APIWrapperFunc
	// fnCallWeight 函数调用准确性权重
	fnCallWeight float64
	// outputEffectivenessWeight 输出有效性权重
	outputEffectivenessWeight float64
	// config 配置
	config map[string]any
	// model LLM 模型客户端（直接持有，用于 Function Calling）
	model *llm.Model
}

// EvalResult 评估结果。
//
// 对齐 Python: SimpleEval.__call__ 返回值
type EvalResult struct {
	// ScoreAvg 平均分
	ScoreAvg float64 `json:"score_avg"`
	// ScoreStd 分数标准差
	ScoreStd float64 `json:"score_std"`
	// FnCallAccuracy 函数调用准确率
	FnCallAccuracy float64 `json:"fn_call_accuracy"`
	// OutputEffectiveness 输出有效性
	OutputEffectiveness float64 `json:"output_effectiveness"`
	// Results 各示例评估结果
	Results []EvalItemResult `json:"results"`
}

// EvalItemResult 单个示例评估结果。
//
// 对齐 Python: SimpleEval._evaluate_single_example 返回值
type EvalItemResult struct {
	// Instruction 指令
	Instruction string `json:"instruction"`
	// ExpectedFnCall 期望的函数调用
	ExpectedFnCall map[string]any `json:"expected_fn_call"`
	// GeneratedFnCall 生成的函数调用
	GeneratedFnCall map[string]any `json:"generated_fn_call"`
	// FnCallScore 函数调用准确性分数
	FnCallScore float64 `json:"fn_call_score"`
	// ExecutionResult 执行结果
	ExecutionResult any `json:"execution_result"`
	// ExecutionError 执行错误
	ExecutionError any `json:"execution_error"`
	// OutputEffectivenessScore 输出有效性分数
	OutputEffectivenessScore float64 `json:"output_effectiveness_score"`
	// WeightedScore 加权分数
	WeightedScore float64 `json:"weighted_score"`
	// Answer 回答
	Answer string `json:"answer"`
	// Errors 错误列表
	Errors []EvalError `json:"errors"`
}

// EvalError 评估错误信息。
type EvalError struct {
	// FunctionName 函数名
	FunctionName string `json:"function_name"`
	// Arguments 参数
	Arguments map[string]any `json:"arguments"`
	// ErrorMsg 错误消息
	ErrorMsg string `json:"error_msg"`
}

// ExampleTuple 示例元组 (instruction, fn_call, fn_output, answer)。
//
// 对齐 Python: examples: List[Tuple[str, Any, str, str]]
type ExampleTuple struct {
	// Instruction 指令
	Instruction string
	// FnCall 函数调用
	FnCall map[string]any
	// FnOutput 函数输出
	FnOutput string
	// Answer 回答
	Answer string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSimpleEval 创建 SimpleEval 实例。
//
// 对齐 Python: SimpleEval.__init__(api_wrapper, config, fn_call_weight, output_effectiveness_weight)
func NewSimpleEval(
	apiWrapper APIWrapperFunc,
	config map[string]any,
	fnCallWeight float64,
	outputEffectivenessWeight float64,
	model *llm.Model,
) *SimpleEval {
	// 对齐 Python: if abs(fn_call_weight + output_effectiveness_weight - 1.0) > 1e-6: raise ValueError
	if math.Abs(fnCallWeight+outputEffectivenessWeight-1.0) > 1e-6 {
		panic(fmt.Sprintf("fn_call_weight and output_effectiveness_weight must sum to 1.0, got %f+%f=%f",
			fnCallWeight, outputEffectivenessWeight, fnCallWeight+outputEffectivenessWeight))
	}
	return &SimpleEval{
		apiWrapper:                apiWrapper,
		fnCallWeight:              fnCallWeight,
		outputEffectivenessWeight: outputEffectivenessWeight,
		config:                    config,
		model:                     model,
	}
}

// Eval 评估工具描述。
//
// 对齐 Python: SimpleEval.__call__(tool, description, examples, runs)
//
//	for run in range(runs):
//	    for i, (instruction, expected_fn_call, fn_output, answer) in enumerate(examples):
//	        result = self._evaluate_single_example(...)
//	    total_score = fn_call_weight * avg_fn_call_score + output_effectiveness_weight * avg_output_score
//	return {score_avg, score_std, fn_call_accuracy, output_effectiveness, results}
func (e *SimpleEval) Eval(
	ctx context.Context,
	tool map[string]any,
	description string,
	examples []ExampleTuple,
	runs int,
) *EvalResult {
	allScores := []float64{}
	allFnCallScores := []float64{}
	allOutputScores := []float64{}
	var allResults []EvalItemResult

	for run := 0; run < runs; run++ {
		totalFnCallScore := 0.0
		totalOutputScore := 0.0
		totalCount := len(examples)

		for i, example := range examples {
			result := e.evaluateSingleExample(ctx, tool, description, example, i)
			allResults = append(allResults, result)
			totalFnCallScore += result.FnCallScore
			totalOutputScore += result.OutputEffectivenessScore
		}

		// 对齐 Python: avg_fn_call_score = total_fn_call_score / total_count
		avgFnCallScore := 0.0
		if totalCount > 0 {
			avgFnCallScore = totalFnCallScore / float64(totalCount)
		}
		avgOutputScore := 0.0
		if totalCount > 0 {
			avgOutputScore = totalOutputScore / float64(totalCount)
		}

		// 对齐 Python: total_score = fn_call_weight * avg_fn_call_score + output_effectiveness_weight * avg_output_score
		totalScore := e.fnCallWeight*avgFnCallScore + e.outputEffectivenessWeight*avgOutputScore
		allScores = append(allScores, totalScore)
		allFnCallScores = append(allFnCallScores, avgFnCallScore)
		allOutputScores = append(allOutputScores, avgOutputScore)
	}

	// 对齐 Python: np.mean(all_scores) * 100.0, np.std(all_scores) * 100.0
	return &EvalResult{
		ScoreAvg:           mean(allScores) * 100.0,
		ScoreStd:           std(allScores) * 100.0,
		FnCallAccuracy:     mean(allFnCallScores) * 100.0,
		OutputEffectiveness: mean(allOutputScores) * 100.0,
		Results:            allResults,
	}
}

// EvaluateFunctionCallAccuracy 评估函数调用准确性。
//
// 对齐 Python: SimpleEval._evaluate_function_call_accuracy(generated_fn_call, expected_fn_call)
//
//	- 函数名匹配权重 0.3
//	- 参数匹配权重 0.7（按参数数量均分）
//	- 返回 0~1 之间的分数
func EvaluateFunctionCallAccuracy(generatedFnCall, expectedFnCall map[string]any) float64 {
	score := 0.0
	maxScore := 0.0

	// 对齐 Python: Check function name (30% weight)
	maxScore += 0.3
	if generatedFnCall["name"] == expectedFnCall["name"] {
		score += 0.3
	}

	// 对齐 Python: Check parameters (70% weight)
	genParams := getArgsMap(generatedFnCall)
	expParams := getArgsMap(expectedFnCall)

	if len(expParams) == 0 && len(genParams) == 0 {
		// 对齐 Python: Both empty parameters
		score += 0.7
		maxScore += 0.7
	} else if len(expParams) > 0 {
		paramScore := 0.0
		for key, expectedValue := range expParams {
			maxScore += 0.7 / float64(len(expParams))
			if genValue, ok := genParams[key]; ok {
				if CompareParameterValues(genValue, expectedValue) {
					paramScore += 0.7 / float64(len(expParams))
				}
			}
		}
		score += paramScore
	} else {
		maxScore += 0.7
	}

	if maxScore > 0 {
		return score / maxScore
	}
	return 0.0
}

// CompareParameterValues 比较参数值，支持类型容忍。
//
// 对齐 Python: SimpleEval._compare_parameter_values(actual, expected)
func CompareParameterValues(actual, expected any) bool {
	// 对齐 Python: if actual == expected: return True
	if actual == expected {
		return true
	}

	// 对齐 Python: Try numeric comparison
	if isNumeric(actual) && isNumeric(expected) {
		actFloat := toFloat(actual)
		expFloat := toFloat(expected)
		if actFloat != nil && expFloat != nil {
			return math.Abs(*actFloat-*expFloat) < 1e-6
		}
	}

	// 对齐 Python: Try string comparison
	actStr := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", actual)))
	expStr := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", expected)))
	if actStr == expStr {
		return true
	}

	return false
}

// SimpleOutputComparison 简单输出比较（兜底）。
//
// 对齐 Python: SimpleEval._simple_output_comparison(execution_result, expected_answer)
func SimpleOutputComparison(executionResult any, expectedAnswer string) float64 {
	if executionResult == nil {
		return 0.0
	}

	var resultStr string
	switch v := executionResult.(type) {
	case string:
		resultStr = v
	default:
		b, _ := json.Marshal(v)
		resultStr = string(b)
	}

	resultLower := strings.TrimSpace(strings.ToLower(resultStr))
	answerLower := strings.TrimSpace(strings.ToLower(expectedAnswer))

	// 对齐 Python: if expected_answer.lower().strip() in result_str.lower().strip(): return 1.0
	if strings.Contains(resultLower, answerLower) {
		return 1.0
	}
	// 对齐 Python: elif result_str.lower().strip() in expected_answer.lower().strip(): return 0.8
	if strings.Contains(answerLower, resultLower) {
		return 0.8
	}
	// 对齐 Python: else: return 0.3
	return 0.3
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evaluateSingleExample 评估单个示例。
//
// 对齐 Python: SimpleEval._evaluate_single_example(example)
func (e *SimpleEval) evaluateSingleExample(
	ctx context.Context,
	tool map[string]any,
	description string,
	example ExampleTuple,
	exampleID int,
) EvalItemResult {
	// 对齐 Python: 构造 example dict（后续步骤会使用）
	_ = map[string]any{
		"tool":              tool,
		"description":       description,
		"instruction":       example.Instruction,
		"expected_fn_call":  example.FnCall,
		"expected_output":   example.FnOutput,
		"answer":            example.Answer,
		"example_id":        exampleID,
	}

	// 对齐 Python: generated_fn_call = self._generate_function_call(tool, description, instruction)
	generatedFnCall, err := e.generateFunctionCall(ctx, tool, description, example.Instruction)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "evaluateSingleExample").
			Int("example_id", exampleID).
			Err(err).
			Msg("Error generating function call")
		return EvalItemResult{
			Instruction:              example.Instruction,
			ExpectedFnCall:           example.FnCall,
			GeneratedFnCall:          nil,
			FnCallScore:              0.0,
			ExecutionResult:          nil,
			ExecutionError:           map[string]any{"error": err.Error()},
			OutputEffectivenessScore: 0.0,
			WeightedScore:            0.0,
			Answer:                   example.Answer,
			Errors: []EvalError{{
				FunctionName: getToolName(tool),
				Arguments:    map[string]any{},
				ErrorMsg:     err.Error(),
			}},
		}
	}

	// 对齐 Python: fn_call_score = self._evaluate_function_call_accuracy(generated_fn_call, expected_fn_call)
	fnCallScore := EvaluateFunctionCallAccuracy(generatedFnCall, example.FnCall)

	// 对齐 Python: Execute the generated function call
	var executionResult any
	var executionError any
	var errors []EvalError

	if e.apiWrapper != nil {
		actualOutput, statusCode := e.apiWrapper(tool, generatedFnCall)
		if statusCode == 0 {
			var parsed any
			if jsonErr := json.Unmarshal([]byte(actualOutput), &parsed); jsonErr == nil {
				executionResult = parsed
			} else {
				executionResult = actualOutput
			}
		} else {
			var parsed any
			if jsonErr := json.Unmarshal([]byte(actualOutput), &parsed); jsonErr == nil {
				executionError = parsed
			} else {
				executionError = actualOutput
			}
			errors = append(errors, EvalError{
				FunctionName: getFnName(generatedFnCall),
				Arguments:    getArgsMap(generatedFnCall),
				ErrorMsg:     fmt.Sprintf("%v", executionError),
			})
		}
	} else {
		logger.Error(logComponent).
			Str("method", "evaluateSingleExample").
			Msg("Missing required input: api_wrapper")
		errors = append(errors, EvalError{
			FunctionName: getToolName(tool),
			Arguments:    map[string]any{},
			ErrorMsg:     "Missing required input: api_wrapper",
		})
	}

	// 对齐 Python: output_effectiveness_score = self._evaluate_output_effectiveness(...)
	outputEffectivenessScore := e.evaluateOutputEffectiveness(
		ctx, example.Instruction, executionResult, executionError, example.Answer,
	)

	// 对齐 Python: weighted_score = fn_call_weight * fn_call_score + output_effectiveness_weight * output_effectiveness_score
	weightedScore := e.fnCallWeight*fnCallScore + e.outputEffectivenessWeight*outputEffectivenessScore

	return EvalItemResult{
		Instruction:              example.Instruction,
		ExpectedFnCall:           example.FnCall,
		GeneratedFnCall:          generatedFnCall,
		FnCallScore:              fnCallScore,
		ExecutionResult:          executionResult,
		ExecutionError:           executionError,
		OutputEffectivenessScore: outputEffectivenessScore,
		WeightedScore:            weightedScore,
		Answer:                   example.Answer,
		Errors:                   errors,
	}
}

// generateFunctionCall 使用 LLM Function Calling 模式生成函数调用。
//
// 对齐 Python: SimpleEval._generate_function_call(tool, description, instruction)
func (e *SimpleEval) generateFunctionCall(
	ctx context.Context,
	tool map[string]any,
	description string,
	instruction string,
) (map[string]any, error) {
	if e.model == nil {
		return nil, fmt.Errorf("model is nil")
	}

	// 对齐 Python: tool schema 处理
	toolForCall := copyMap(tool)
	if _, hasType := toolForCall["type"]; !hasType {
		toolForCall["type"] = "function"
	}

	// 对齐 Python: 如果 description 是 JSON 且包含 function 键，提取
	if descStr, ok := toolForCall["description"].(string); ok {
		var descJSON map[string]any
		if jsonErr := json.Unmarshal([]byte(descStr), &descJSON); jsonErr == nil {
			if fn, hasFn := descJSON["function"]; hasFn {
				if fnMap, ok := fn.(map[string]any); ok {
					toolForCall = map[string]any{
						"name":        fnMap["name"],
						"type":        toolForCall["type"],
						"description": fnMap["description"],
						"parameters":  fnMap["parameters"],
					}
				}
			}
		}
	}

	// 构建 ToolInfoInterface
	toolInfo := cschema.NewToolInfo(
		getToolName(toolForCall),
		getToolDescription(toolForCall),
		getToolParameters(toolForCall),
	)

	// 对齐 Python: 使用 Function Calling
	modelName := getConfigString(e.config, "eval_model_id")
	return InvokeFunctionCall(ctx, e.model, modelName, instruction, toolInfo)
}

// evaluateOutputEffectiveness 评估输出有效性。
//
// 对齐 Python: SimpleEval._evaluate_output_effectiveness(instruction, execution_result, execution_error, expected_answer)
func (e *SimpleEval) evaluateOutputEffectiveness(
	ctx context.Context,
	instruction string,
	executionResult any,
	executionError any,
	expectedAnswer string,
) float64 {
	// 对齐 Python: if execution_error: return 0.0
	if executionError != nil {
		return 0.0
	}

	// 对齐 Python: prompt 一比一复刻
	execResultJSON, _ := json.MarshalIndent(executionResult, "", "  ")
	prompt := fmt.Sprintf(`
Evaluate whether the function execution result effectively solves the user's problem.

User Instruction: %s

Function Execution Result: %s

Expected Answer/Goal: %s

Please evaluate on a scale of 0-100 how well the function execution result addresses the user's instruction and matches the expected answer. Consider:
1. Does the result provide the information requested in the instruction?
2. Is the result accurate and complete?
3. Does it align with the expected answer?

Respond with only a number between 0 and 100. Do not include explainations.
`,
		instruction, string(execResultJSON), expectedAnswer)

	modelName := getConfigString(e.config, "eval_model_id")
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        2,
		TotalBudgetSecs:    60,
		AttemptTimeoutSecs: 30,
		BackoffBaseSecs:    1.0,
	}

	response, err := InvokeText(ctx, e.model, modelName, prompt, policy)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "evaluateOutputEffectiveness").
			Err(err).
			Msg("Error evaluating output effectiveness")
		return SimpleOutputComparison(executionResult, expectedAnswer)
	}

	// 对齐 Python: score = float(response.strip())
	score := 0.0
	response = strings.TrimSpace(response)
	if _, parseErr := fmt.Sscanf(response, "%f", &score); parseErr != nil {
		return SimpleOutputComparison(executionResult, expectedAnswer)
	}

	// 对齐 Python: min(max(score, 0.0), 100.0) / 100.0
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score / 100.0
}

// mean 计算均值。
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// std 计算标准差。
func std(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	sum := 0.0
	for _, v := range values {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum / float64(len(values)))
}

// isNumeric 判断是否为数值类型。
func isNumeric(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}

// toFloat 将数值转为 float64 指针。
func toFloat(v any) *float64 {
	switch n := v.(type) {
	case int:
		f := float64(n)
		return &f
	case float64:
		return &n
	case int64:
		f := float64(n)
		return &f
	case float32:
		f := float64(n)
		return &f
	}
	return nil
}

// getArgsMap 从 fn_call 中获取 arguments 为 map[string]any。
func getArgsMap(fnCall map[string]any) map[string]any {
	args, ok := fnCall["arguments"]
	if !ok {
		return map[string]any{}
	}
	switch a := args.(type) {
	case map[string]any:
		return a
	case string:
		var result map[string]any
		if jsonErr := json.Unmarshal([]byte(a), &result); jsonErr == nil {
			return result
		}
	}
	return map[string]any{}
}

// getFnName 从 fn_call 中获取函数名。
func getFnName(fnCall map[string]any) string {
	if fnCall == nil {
		return "unknown"
	}
	if name, ok := fnCall["name"].(string); ok {
		return name
	}
	return "unknown"
}

// getToolName 从 tool 字典中获取工具名。
func getToolName(tool map[string]any) string {
	if name, ok := tool["name"].(string); ok {
		return name
	}
	return "unknown"
}

// getToolDescription 从 tool 字典中获取描述。
func getToolDescription(tool map[string]any) string {
	if desc, ok := tool["description"].(string); ok {
		return desc
	}
	return ""
}

// getToolParameters 从 tool 字典中获取参数 schema。
func getToolParameters(tool map[string]any) map[string]any {
	if params, ok := tool["parameters"].(map[string]any); ok {
		return params
	}
	return map[string]any{}
}

// copyMap 深拷贝 map[string]any。
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
