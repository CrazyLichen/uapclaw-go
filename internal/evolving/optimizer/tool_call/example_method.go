package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// APICallToExampleMethod Example Stage 方法。
// 生成 API 调用示例，形成正负例集。实现 BeamSearchMethod 接口。
//
// 对应 Python: APICallToExampleMethod
type APICallToExampleMethod struct {
	BaseMethod
	// runToolWithAPICall API 调用函数
	runToolWithAPICall APIWrapperFunc
	// evalFn 评估函数
	evalFn *SimpleEval
	// apiKeys API 密钥模板
	apiKeys any
	// nonOptParams 非优化参数
	nonOptParams []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAPICallToExampleMethod 创建 APICallToExampleMethod 实例。
//
// 对齐 Python: APICallToExampleMethod(config, api_call_fn, eval_fn, api_keys=None, non_opt_params=[])
//
// 参数顺序对齐 pipeline.go 调用: NewAPICallToExampleMethod(config, model, callAPIFn, evalFn, nil, nil)
func NewAPICallToExampleMethod(
	config map[string]any,
	model *llm.Model,
	apiCallFn APIWrapperFunc,
	evalFn *SimpleEval,
	apiKeys any,
	nonOptParams []string,
) *APICallToExampleMethod {
	if nonOptParams == nil {
		nonOptParams = []string{}
	}
	return &APICallToExampleMethod{
		BaseMethod:         *NewBaseMethod(config, model),
		runToolWithAPICall: apiCallFn,
		evalFn:             evalFn,
		apiKeys:            apiKeys,
		nonOptParams:       nonOptParams,
	}
}

// Step 执行单步扩展，返回 output/data/score。
//
// 对齐 Python: APICallToExampleMethod.step(tool, prev_outputs, it)
//
//  1. 获取原始描述
//  2. 拒绝采样循环（num_init_loop 次）：生成 API 调用 → 执行 → 批判
//  3. Q/A 生成和精炼循环（num_refine_steps 次）：生成指令 → 生成回答 → 批判指令 → 批量反思
//  4. 评估：使用 evalFn 评估
//  5. 返回 (output, instructions, score)
func (m *APICallToExampleMethod) Step(
	ctx context.Context,
	tool map[string]any,
	examples any,
	prevOutputs []any,
	it int,
) (output any, data any, score float64, err error) {
	logger.Info(logComponent).Msg("Inside method, trying to step")

	// 对齐 Python: prev_outputs = copy.copy(prev_outputs) if prev_outputs is not None else []
	prevOutputsCopy := make([]any, len(prevOutputs))
	copy(prevOutputsCopy, prevOutputs)

	// 对齐 Python: description = self.get_original_description(tool)
	description := m.GetOriginalDescription(tool)
	logger.Info(logComponent).
		Str("description", description).
		Msg("Original desc obtained")

	// 对齐 Python: tool_for_opt = copy.deepcopy(tool)
	toolForOpt := deepCopyMap(tool)
	logger.Info(logComponent).
		Str("tool_for_opt", fmt.Sprintf("%v", toolForOpt)).
		Msg("Tool_for_opt")

	// 1. 拒绝采样：
	// 在初始循环中，生成候选 API 调用、运行工具，并判断
	// 是否存在错误
	numInitLoop := getConfigInt(m.config, "num_init_loop")
	if numInitLoop <= 0 {
		numInitLoop = 1
	}
	var fnCall map[string]any
	var toolRes string
	outputs := map[string]any{}

	for i := 0; i < numInitLoop; i++ {
		var err error
		fnCall, err = m.GenerateAPICallFromDescription(ctx, toolForOpt, nil, 1, prevOutputsCopy)
		if err != nil {
			logger.Error(logComponent).Err(err).Int("init_loop", i).Msg("GenerateAPICallFromDescription failed")
			continue
		}
		logger.Info(logComponent).Msg("API call generation completed")
		logger.Info(logComponent).
			Str("fn_call", fmt.Sprintf("%v", fnCall)).
			Msg("API call params")

		// 对齐 Python: tool_res, status_code = self.run_tool_with_api_call(tool_for_opt, fn_call)
		var statusCode int
		toolRes, statusCode = m.runToolWithAPICall(toolForOpt, fnCall)
		outputs = map[string]any{
			"fn_call":      fnCall,
			"tool_results": toolRes,
			"status_code":  statusCode,
			"score":        statusCode,
		}
		logger.Info(logComponent).
			Int("status_code", statusCode).
			Msg("Run tool with api call completed")

		// 对齐 Python: api_analysis = self.critique_api_call(tool_for_opt, fn_call, tool_res)
		apiAnalysis, err := m.CritiqueAPICall(ctx, toolForOpt, fnCall, toolRes)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("CritiqueAPICall failed")
			continue
		}
		logger.Info(logComponent).
			Str("results", fmt.Sprintf("%v", apiAnalysis)).
			Msg("critique_api_call finished")

		// 对齐 Python: if api_analysis['err_code'] == -1:
		errCode := toIntSafe(apiAnalysis["err_code"])
		if errCode == -1 {
			outputs["status_code"] = -1
			outputs["score"] = -1
			outputs["api_reflection"] = apiAnalysis["analysis"]
			prevOutputsCopy = append(prevOutputsCopy, outputs)
			if m.verbose {
				fnCallJSON, _ := json.Marshal(fnCall)
				logger.Info(logComponent).
					Str("fn_call", string(fnCallJSON)).
					Msg("verbose: bad fn_call")
				logger.Info(logComponent).
					Str("analysis", fmt.Sprintf("%v", apiAnalysis["analysis"])).
					Msg("verbose: api_reflection")
			}
			continue
		}
		break
	}

	// 2. 问答生成与精炼：
	// 在 n_refine 步中，从有效的 API 调用生成问答
	// 通过批量自反思评估并更新
	numRefineSteps := getConfigInt(m.config, "num_refine_steps")
	if numRefineSteps <= 0 {
		numRefineSteps = 1
	}
	numFeedbackSteps := getConfigInt(m.config, "num_feedback_steps")
	if numFeedbackSteps <= 0 {
		numFeedbackSteps = 2
	}

	var insts []string
	var scores []float64
	var analyses []string
	var answers []string
	var refls []string
	var instOutput map[string]any

	for nRefine := 0; nRefine < numRefineSteps; nRefine++ {
		// 对齐 Python: inst = self.generate_instruction_from_api_call(...)
		inst, err := m.GenerateInstructionFromAPICall(ctx, toolForOpt, fnCall, toolRes, instOutput)
		if err != nil {
			logger.Error(logComponent).Err(err).Int("refine_step", nRefine).Msg("GenerateInstructionFromAPICall failed")
			continue
		}

		// 对齐 Python: ans = self.produce_answer_from_api_call(inst, json.dumps(tool_for_opt), tool_res)
		docStr := jsonStr(toolForOpt)
		ans, ansErr := m.ProduceAnswerFromAPICall(ctx, inst, docStr, toolRes)
		if ansErr != nil {
			ans = ""
		}

		// 对齐 Python: inst_eval = self.critique_instruction(...)
		instEval, err := m.CritiqueInstruction(ctx, toolForOpt, inst, fnCall, toolRes, ans)
		if err != nil {
			logger.Error(logComponent).Err(err).Int("refine_step", nRefine).Msg("CritiqueInstruction failed")
			instEval = map[string]any{"analysis": "", "score": 1}
		}

		insts = append(insts, inst)
		answers = append(answers, ans)

		scoreVal := toIntSafe(instEval["score"])
		scores = append(scores, float64(scoreVal))
		analyses = append(analyses, fmt.Sprintf("%v", instEval["analysis"]))

		// 对齐 Python: insts[-self.config['num_feedback_steps']:]
		feedbackInsts := lastN(insts, numFeedbackSteps)
		feedbackScores := lastNFloat(scores, numFeedbackSteps)
		feedbackAnalyses := lastN(analyses, numFeedbackSteps)

		// 对齐 Python: batch_refl = self.batch_reflection_with_scores(...)
		batchRefl, err := m.BatchReflectionWithScores(ctx, toolForOpt, fnCall, feedbackInsts, feedbackScores, feedbackAnalyses)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("BatchReflectionWithScores failed")
			batchRefl = ""
		}
		refls = append(refls, strings.TrimSpace(batchRefl))

		instOutput = map[string]any{
			"instructions":     feedbackInsts,
			"scores":           feedbackScores,
			"batch_reflection": batchRefl,
		}

		// 对齐 Python: if inst_eval['score'] == 3: break
		if scoreVal == 3 {
			break
		}
	}

	// 3. 使用下游 LLM 评估新生成的示例——用于
	// 筛选困难示例
	scoreEvalWeight := getConfigFloat(m.config, "score_eval_weight")
	var evalScore float64

	if scoreEvalWeight > 0 {
		logger.Info(logComponent).Msg("Eval step: Using eval fn")
		if len(insts) > 0 && len(answers) > 0 {
			lastInst := strings.TrimSpace(insts[len(insts)-1])
			lastAns := strings.TrimSpace(answers[len(answers)-1])
			if lastInst != "" && lastAns != "" {
				// 对齐 Python: examples = [(insts[-1].strip(), fn_call, tool_res, answers[-1].strip())]
				exampleTuples := []ExampleTuple{{
					Instruction: lastInst,
					FnCall:      fnCall,
					FnOutput:    toolRes,
					Answer:      lastAns,
				}}
				// 对齐 Python: eval_res = self.eval_fn(tool, description, examples, runs=1)
				evalRes := m.evalFn.Eval(ctx, tool, description, exampleTuples, 1)
				evalScore = evalRes.ScoreAvg / 100.0
			} else {
				evalScore = 1.0
			}
		} else {
			evalScore = 1.0
		}
	} else {
		logger.Info(logComponent).Msg("Eval step: hard coded eval_score as score_eval_weight=0")
		evalScore = 1.0
	}

	// 对齐 Python: final_score = scores[-1] + self.config['score_eval_weight'] * (1. - eval_score)
	var finalScore float64
	if len(scores) > 0 {
		finalScore = scores[len(scores)-1] + scoreEvalWeight*(1.0-evalScore)
	}

	outputs["answers"] = answers
	outputs["instructions"] = insts
	outputs["scores"] = scores
	outputs["analyses"] = analyses
	outputs["batch_reflections"] = refls
	outputs["score"] = finalScore

	return outputs, insts, finalScore, nil
}

// GetExamples BeamSearchMethod 接口实现。
// APICallToExampleMethod 不需要预加载示例，返回 nil。
//
// 对齐 Python: APICallToExampleMethod.get_examples(tool)
func (m *APICallToExampleMethod) GetExamples(ctx context.Context, tool map[string]any) any {
	return nil
}

// GenerateAPICallFromDescription 根据工具描述生成 API 调用。
//
// 对齐 Python: APICallToExampleMethod.generate_api_call_from_description(tool, example_calls, num_gen, prev_output)
func (m *APICallToExampleMethod) GenerateAPICallFromDescription(
	ctx context.Context,
	tool map[string]any,
	exampleCalls []string,
	numGen int,
	prevOutput []any,
) (map[string]any, error) {
	functionName, _ := tool["name"].(string)
	docStr := jsonStr(tool)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`A tool is an API.
You are given an API tool with the following
documentation, which includes the functionality
description, required parameters, code snippets for API calls, etc.

Documentation:
%s
`, docStr)

	if len(exampleCalls) > 0 {
		quotedCalls := make([]string, len(exampleCalls))
		for i, call := range exampleCalls {
			quotedCalls[i] = fmt.Sprintf(`"%s"`, call)
		}
		userPrompt += fmt.Sprintf(`
Example use cases for this API tool are:
%s

`, strings.Join(quotedCalls, "\n"))
	}

	if m.apiKeys != nil {
		apiKeysJSON := jsonStr(m.apiKeys)
		userPrompt += fmt.Sprintf(
			"You have access to the following API keys:"+
				" %s. You must use real API keys"+
				" instead of placeholders when creating an API call.\n\n",
			apiKeysJSON,
		)
	}

	userPrompt += fmt.Sprintf(`Your task is to write %d example API call
for the given API tool given its purpose and parameters list.
The API call you produced will be executed as function call later and
return result if correct, or error if you provide incorrect syntax,
format, or parameters. Given the documentation and description, think
of possible example API calls and produce those that are likely to be
correctly executed. Think of parameter values that are likely API calls
that people use in the real world and be the intension to find out
the api's capabilities. The goal is to generate realistic, slightly
edge-case API calls that are valid, executable, and reveal subtle
limits in the system (e.g., language-restricted fields, domestic-only
locations, silent defaults, etc.). The generated API call MUST be
executable and real. Parameter values must be filled in and not
placeholding text. You must include the required parameters,
and optionally give parameters that are labeled as "optional
parameters". Do not hallucinate and produce parameters that
are not under "required" or "optional". Produce diverse
parameter values if you are asked to generate multiple API calls,
but be factual and do not use fake parameters.

You can only use the given function %s and not anything else. Create an API call that include the function name, and the parameters to be input to the API. Include all the required and optional parameters in a single dictionary without separating them. Do not include the URL or other irrelevant information. The output should be in the following JSON format that represents a function call:
{
    "name": "%s",
    "arguments": {
        "parameter_1": <param_value_1>,
        "parameter_2": <param_value_2>
    }
}

You must strictly follow the output format, including "name", "arguments", and parameters.
`, numGen, functionName, functionName)

	if len(prevOutput) > 0 {
		userPrompt += "Previously you generated the following API calls for this "
		userPrompt += fmt.Sprintf("function %s, which where then executed and critiqued:\n", functionName)
		for i, out := range prevOutput {
			outputMap, ok := out.(map[string]any)
			if !ok {
				continue
			}
			i1 := i + 1
			if reflection, hasRefl := outputMap["api_reflection"]; hasRefl {
				fnCallStr := jsonStr(outputMap["fn_call"])
				toolResultsStr := jsonStr(outputMap["tool_results"])
				if len(toolResultsStr) > 512 {
					toolResultsStr = toolResultsStr[:512]
				}
				statusCode := outputMap["status_code"]
				userPrompt += fmt.Sprintf(
					`%d. fn_call="%s" fn_output="%s" status=%v reflection="This is an example of a bad function call. Here is your reflection: %v"`+"\n",
					i1, fnCallStr, toolResultsStr, statusCode, reflection,
				)
			} else {
				fnCallStr := jsonStr(outputMap["fn_call"])
				toolResultsStr := jsonStr(outputMap["tool_results"])
				if len(toolResultsStr) > 512 {
					toolResultsStr = toolResultsStr[:512]
				}
				statusCode := outputMap["status_code"]
				userPrompt += fmt.Sprintf(
					`%d. fn_call="%s" fn_output="%s" status=%v reflection="This is an example of a good and reasonable function call. You should generate a function call that differs from this if possible; do not generate the same function call unless there are no parameters for this function."`+"\n",
					i1, fnCallStr, toolResultsStr, statusCode,
				)
			}
		}
		userPrompt += "You should improve your response based on these reflections.\n\n"
	}

	userPrompt += "Do not output anything other than the JSON output. Now you can begin your task."
	prompt := FormatPromptLlama("", userPrompt)

	// 对齐 Python: verify_output(output)
	verifyFn := func(output string) (any, error) {
		fn := ParseJSON(output)

		if len(fn) == 0 {
			return nil, fmt.Errorf("Output must be a dict")
		}

		if _, ok := fn["name"]; !ok {
			return nil, fmt.Errorf(`incorrect output format, "name" required for function`)
		}

		if _, ok := fn["arguments"]; !ok {
			return nil, fmt.Errorf(
				`incorrect output format, "arguments" required for function %v`,
				fn["name"],
			)
		}

		if fnName, ok := fn["name"].(string); ok && fnName != functionName {
			return nil, fmt.Errorf(
				"Output function '%s' is inconsistent with the given function '%s'. You must only use the given function %s!",
				fnName, functionName, functionName,
			)
		}

		return fn, nil
	}

	logger.Info(logComponent).Msg("Sending request to generate tool use examples")

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
	modelName := getConfigString(m.config, "gen_model_id")

	result, resultErr := InvokeWithVerify(ctx, m.model, modelName, prompt, policy, verifyFn)
	if resultErr != nil {
		return map[string]any{}, resultErr
	}
	if resultMap, ok := result.(map[string]any); ok {
		return resultMap, nil
	}
	return map[string]any{}, nil
}

// CritiqueAPICall 批判 API 调用结果。
//
// 对齐 Python: APICallToExampleMethod.critique_api_call(tool, fn_call, fn_response)
func (m *APICallToExampleMethod) CritiqueAPICall(
	ctx context.Context,
	tool map[string]any,
	fnCall map[string]any,
	fnResponse string,
) (map[string]any, error) {
	functionName, _ := tool["name"].(string)
	docStr := jsonStr(tool)
	fnCallStr := jsonStr(fnCall)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
You are given an API tool with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

Documentation:
%s

Previously you were asked to write an example API
call for the function %s given its purpose
and parameters list, and you generated the following
function call: %s. `,
		docStr, functionName, fnCallStr)

	fnResponseTruncated := fnResponse
	if len(fnResponse) > 2048 {
		fnResponseTruncated = fnResponse[:2048]
		userPrompt += fmt.Sprintf(`The function call you produced
was later executed and returned the following result.
Example of function result: "%s", etc.`, fnResponseTruncated)
	} else {
		userPrompt += fmt.Sprintf(`The function call
you produced was later executed and returned the
following result: "%s".`, fnResponse)
	}

	userPrompt += `Your task is to analyze the response and check if there are any errors.
        1. If there are no errors and everything looks reasonable, give an err_code of 0, and don't provide analysis.
        2. If there is an error, give an err_code of -1. Then in your analysis, describe and analyze in detail why the error occurred based on the error message. Then, based on your analysis, give detailed suggestions to improve the function call so that no errors will be produced. You must give detailed analysis and suggestions, do not simply repeat the error message. The analysis and suggestions should be in the "analysis" field in the output.

        Note that even if the "error" field in the result is empty, the "response" field may contain an error when using the function call. If this is the case you must treat this as an error and analyze the failure. The response field may also be in HTML format. Keep your analysis to less than 200 characters.

        Your output should be in the following JSON format:
        {
            "analysis": your analysis and suggestions,
            "err_code": error code (-1 for error, 0 for correct)
        }

You can begin your task now.`

	prompt := FormatPromptLlama("", userPrompt)

	// 对齐 Python: verify_output(output)
	verifyFn := func(output string) (any, error) {
		outputJSON := ParseJSON(output)

		if _, ok := outputJSON["analysis"]; !ok {
			return nil, fmt.Errorf(`No "analysis" found in output`)
		}

		if _, ok := outputJSON["err_code"]; !ok {
			return nil, fmt.Errorf(`No "err_code" found in output`)
		}

		// 对齐 Python: output_json["analysis"] = str(output_json.get("analysis", "")).strip()
		analysis := strings.TrimSpace(fmt.Sprintf("%v", outputJSON["analysis"]))
		outputJSON["analysis"] = analysis

		// 对齐 Python: output_json["err_code"] = int(output_json.get("err_code"))
		outputJSON["err_code"] = toIntSafe(outputJSON["err_code"])

		return outputJSON, nil
	}

	logger.Info(logComponent).Msg("Sending request to critique api")

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
	modelName := getConfigString(m.config, "eval_model_id")

	result, resultErr := InvokeWithVerify(ctx, m.model, modelName, prompt, policy, verifyFn)
	if resultErr != nil {
		return map[string]any{"analysis": "", "err_code": 0}, resultErr
	}
	if resultMap, ok := result.(map[string]any); ok {
		return resultMap, nil
	}
	return map[string]any{"analysis": "", "err_code": 0}, nil
}

// GenerateInstructionFromAPICall 根据 API 调用结果生成指令。
//
// 对齐 Python: APICallToExampleMethod.generate_instruction_from_api_call(tool, fn_call, fn_response, prev_output)
func (m *APICallToExampleMethod) GenerateInstructionFromAPICall(
	ctx context.Context,
	tool map[string]any,
	fnCall map[string]any,
	fnResponse string,
	prevOutput map[string]any,
) (string, error) {
	functionName, _ := tool["name"].(string)
	docStr := jsonStr(tool)
	fnCallStr := jsonStr(fnCall)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
        You are given an API tool with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

        Documentation:
        %s

        For the function %s, you are given the following function call: %s, and executing the function call returned the following result: %s. Your task is to generate a user instruction in natural language that requires the given function call to be completed. Here are some guidelines to follow:
        1. The instruction must be a scenario or problem that cannot be solved without calling the given function %s. This is your main objective.
        2. The problem can be complex and require other tools or APIs, but you must include the given API function.
        3. You should not directly or explicitly ask for the function to be called; the problem itself must inherently be solved by the function.
        4. Based on the function, function call, its parameters, parameter values, and function execution responses, you should produce a real and reasonable instruction.
        5. You must use information from the parameter values of the function call to create the response. You must include the value of every parameter from the given function call in the user instruction you generated, including each list/dict element of the parameter values. Do not ignore any parameters/values from the function call.
        6. You must NOT include specific function calls in your response. You should not explicitly show the function names. You should also never explicitly name the parameter names in your response. You should not show any variable names.
        7. Your response has to be in natural language. Do not show any variables, function calls, or code.
        8. The instruction must not be longer than 3 sentences. It should not be longer than 300 letters. Be succinct and do not spend too much on describing irrelevant background.
        9. You should respond in the user's first-person perspective.
        10. You are a human user. You are asking a question or giving an instruction. Do not answer in the perspective of an AI assistant. Remember, the user does not know about the API function and thus cannot ask to call the function.
        11. Remember, you are asking a question, so do not answer your own question in the response. Your goal is to give a querying instruction or question, not producing answers or function calls.
        12. Be creative and think about what users will ask in real-world scenarios.
        `,
		docStr, functionName, fnCallStr, fnResponse, functionName)

	if m.apiKeys != nil {
		userPrompt += fmt.Sprintf(`13. The instruction should include which API key to use if an API key is required. You have access to the following API keys: %s.`, jsonStr(m.apiKeys))
	}

	userPrompt += `
        Your output should be in the following JSON format:
        {
            "instruction": generated instruction
        }`

	if prevOutput != nil {
		// 对齐 Python: 格式化之前的指令和分数
		formattedLines := []string{}

		if instructions, ok := prevOutput["instructions"].([]string); ok {
			if scoreVals, ok := prevOutput["scores"].([]float64); ok {
				for i, inst := range instructions {
					scoreVal := 0.0
					if i < len(scoreVals) {
						scoreVal = scoreVals[i]
					}
					formattedLines = append(formattedLines, fmt.Sprintf(`%d. instruction="%s" score=%v`, i+1, inst, scoreVal))
				}
			}
		}

		formatted := strings.Join(formattedLines, "\n")

		userPrompt += fmt.Sprintf(`Previously you generated the following
instructions for this function call, which were rated and analyzed:
            %s
            Based on these ratings, you are given the following analysis: %v
            You should improve your instructions based on these suggestions. `,
			formatted, prevOutput["batch_reflection"])
	}

	userPrompt += "You must strictly follow the output format. Now you can begin your task."
	prompt := FormatPromptLlama("", userPrompt)

	// 对齐 Python: verify_output(output)
	verifyFn := func(output string) (any, error) {
		outputJSON := ParseJSON(output, "instruction")

		if _, ok := outputJSON["instruction"]; !ok {
			return nil, fmt.Errorf(`No "instruction" found in output`)
		}

		instruction, _ := outputJSON["instruction"].(string)
		return strings.TrimSpace(instruction), nil
	}

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
	modelName := getConfigString(m.config, "eval_model_id")

	result, resultErr := InvokeWithVerify(ctx, m.model, modelName, prompt, policy, verifyFn)
	if resultErr != nil {
		return "", resultErr
	}
	if str, ok := result.(string); ok {
		return str, nil
	}
	return fmt.Sprintf("%v", result), nil
}

// CritiqueInstruction 批判生成的指令。
//
// 对齐 Python: APICallToExampleMethod.critique_instruction(tool, instruction, fn_call, fn_response, answer)
func (m *APICallToExampleMethod) CritiqueInstruction(
	ctx context.Context,
	tool map[string]any,
	instruction string,
	fnCall map[string]any,
	fnResponse string,
	answer string,
) (map[string]any, error) {
	fnCallStr := jsonStr(fnCall)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
You are given an instruction "%s",
function call "%s" and an answer "%s",
your task is to give a `+"`score`"+` based on the following rules:
1. You must return 1 if any of the following
conditions is met (for instruction only):
    (1) instruction is empty, nonsense, or not in natural language; or
    (2) instruction is explicitly including function
    calls or asking for function calls or contains function names; or
    (3) instruction includes exact function parameter names; or
    (4) instruction includes code or variable assignment; or
    (5) instruction is longer than 3 sentences or 300 letters; or
    (6) instruction does not include a question, query, request, or
    problem to be solved; or
    (7) instruction is not in first-person perspective (not using "I" as pronoun),
    or is in the perspective of an AI assistant instead of a user; or
    (8) any parameter value in the function call is not present in the instruction`,
		instruction, fnCallStr, answer)

	if m.apiKeys != nil {
		userPrompt += `; or (9) instruction does include the corresponding API key when an API key is required`
	}

	userPrompt += fmt.Sprintf(`. An instruction that satisfies any of these conditions
is a bad instruction and should be scored a 1.

2. If the answer is a sorry message, not a positive/straight
response for the given instruction, or mentions any
errors (API error, invalid parameter error, ..., etc.),
mentions cannot use API or cannot respond, return 1. Any
errors must be scored a 1, no exceptions.

3. If the answer is a positive/straight response
for the given instruction, you have to further check.
    3.1 If the answer is not sufficient to
    determine whether they solve the instruction or not, return 2.
    3.2 If you are confident that the answer
    is sufficient to determine whether the solve the
    instruction or not, return 3 if solvable or 1 if unsolvable.

Finally, organize your output in the following JSON format:

{
    "analysis": your reasoning,
    "score": score
}

You must strictly follow the output format. Your reasoning
should not be longer than 200 words. You must also strictly
follow the scoring rules, and remember that the score must
be a number between 1 and 3. You can begin your task now.`)

	prompt := FormatPromptLlama("", userPrompt)

	// 对齐 Python: verify_output(output)
	verifyFn := func(output string) (any, error) {
		outputJSON := ParseJSON(output, "analysis")

		if len(outputJSON) == 0 {
			return nil, fmt.Errorf("incorrect output format (not a dict), you have to output Dict containing your analysis and rating")
		}

		if _, ok := outputJSON["analysis"]; !ok {
			return nil, fmt.Errorf(`incorrect output format, "analysis" required.`)
		}

		if _, ok := outputJSON["score"]; !ok {
			return nil, fmt.Errorf(`incorrect output format, "score" required.`)
		}

		// 对齐 Python: output_json["analysis"] = str(output_json.get("analysis", "")).strip()
		analysis := strings.TrimSpace(fmt.Sprintf("%v", outputJSON["analysis"]))
		outputJSON["analysis"] = analysis

		// 对齐 Python: output_json["score"] = int(output_json.get("score"))
		scoreVal := toIntSafe(outputJSON["score"])
		outputJSON["score"] = float64(scoreVal)

		return outputJSON, nil
	}

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
	modelName := getConfigString(m.config, "eval_model_id")

	result, resultErr := InvokeWithVerify(ctx, m.model, modelName, prompt, policy, verifyFn)
	if resultErr != nil {
		return map[string]any{"analysis": "", "score": 1}, resultErr
	}
	if resultMap, ok := result.(map[string]any); ok {
		return resultMap, nil
	}
	return map[string]any{"analysis": "", "score": 1}, nil
}

// BatchReflectionWithScores 批量反思，分析指令与分数的关系并给出改进建议。
//
// 对齐 Python: APICallToExampleMethod.batch_reflection_with_scores(tool, fn_call, instructions, scores, analyses)
func (m *APICallToExampleMethod) BatchReflectionWithScores(
	ctx context.Context,
	tool map[string]any,
	fnCall map[string]any,
	instructions []string,
	scores []float64,
	analyses []string,
) (string, error) {
	functionName, _ := tool["name"].(string)
	docStr := jsonStr(tool)
	fnCallStr := jsonStr(fnCall)

	lines := make([]string, 0, len(instructions))
	for i, inst := range instructions {
		if i < len(scores) && i < len(analyses) {
			lines = append(lines, fmt.Sprintf(`%d. instruction="%s" score=%v analysis="%s"`, i+1, inst, scores[i], analyses[i]))
		}
	}

	formatted := strings.Join(lines, "\n")

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`You are given an API tool with the
following documentation, which includes the functionality
description, required parameters, code snippets for API calls, etc.

Documentation:
%s

Previously, given the function call %s,
you were asked to generate example instructions that require
the use of the function %s to complete.
The example instructions generated by you were then scored
by an expert on whether the instructions can be fulfilled
using the given API function. Scores are in a scale
between 1 (lowest) and 3 (highest). Below are the
generated instructions, scores, and analyses:

%s

Task:
1. Firstly, identify and contrast the patterns of
instructions and function calls that have achieved
good scores with those that have not. If there are
no bad scores, only summarize the patterns of the good ones.
2. Next, specify the suggestions that can lead to
improved performance for the generated instructions
and function calls with bad scores. You should
focus on capturing the high-level pattern of the
examples relevant to the API documentation.
Note that both the function and the function
call cannot be changed, and focus your
suggestions on how to improve the example
instructions, including deciding what information
to use from parameters of the function call.

Keep your analysis and suggestions to less
than 500 characters. You can now start your task.`,
		docStr, fnCallStr, functionName, formatted)

	prompt := FormatPromptLlama("", userPrompt)

	// 对齐 Python: verify_output — 仅返回 stripped text
	verifyFn := func(output string) (any, error) {
		return strings.TrimSpace(output), nil
	}

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
	modelName := getConfigString(m.config, "eval_model_id")

	result, resultErr := InvokeWithVerify(ctx, m.model, modelName, prompt, policy, verifyFn)
	if resultErr != nil {
		return "", resultErr
	}
	if str, ok := result.(string); ok {
		return str, nil
	}
	return strings.TrimSpace(fmt.Sprintf("%v", result)), nil
}

// GetOriginalDescription 获取工具的原始描述。
// 从 ToolBench 格式的 description 中提取原始描述文本。
//
// 对齐 Python: APICallToExampleMethod.get_original_description(tool)
func (m *APICallToExampleMethod) GetOriginalDescription(tool map[string]any) string {
	description, _ := tool["description"].(string)
	indicator := `The description of this function is: "`
	found := strings.Index(description, indicator)
	if found != -1 {
		description = description[found+len(indicator):]
		if len(description) > 0 && description[len(description)-1] == '"' {
			description = description[:len(description)-1]
		}
	}
	return description
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jsonStr 将任意值序列化为 JSON 字符串。
// 对齐 Python: json.dumps(obj, ensure_ascii=False)
func jsonStr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// deepCopyMap 深拷贝 map[string]any。
// 对齐 Python: copy.deepcopy(tool)
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice 深拷贝 []any。
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// lastN 返回字符串切片的最后 n 个元素。
func lastN(slice []string, n int) []string {
	if n >= len(slice) {
		return slice
	}
	return slice[len(slice)-n:]
}

// lastNFloat 返回浮点数切片的最后 n 个元素。
func lastNFloat(slice []float64, n int) []float64 {
	if n >= len(slice) {
		return slice
	}
	return slice[len(slice)-n:]
}

// toInt 将 any 值安全转换为 int。
// 对齐 Python: int(value) — 无法转换时返回 0
func toInt(v any) int {
	return toIntSafe(v)
}

// formatExampleCalls 格式化示例调用列表。
// 对齐 Python: os.linesep.join(f'"{api_call}"' for api_call in example_calls)
func formatExampleCalls(calls []string) string {
	var parts []string
	for _, call := range calls {
		parts = append(parts, fmt.Sprintf(`"%s"`, call))
	}
	return strings.Join(parts, "\n")
}
