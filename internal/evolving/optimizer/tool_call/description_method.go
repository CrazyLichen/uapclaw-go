package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolDescriptionMethod 基于正负例对比的工具描述优化方法。
// 通过批判描述、对比正负例、生成增强描述来迭代优化工具描述。
//
// 对应 Python: ToolDescriptionMethod
type ToolDescriptionMethod struct {
	BaseMethod
	// evalFn 评估函数
	evalFn *SimpleEval
}

// descExampleResultPair 示例与结果的配对。
type descExampleResultPair struct {
	Example ExampleTuple
	Result  map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// descPerformanceThreshold 正负例分类阈值
	descPerformanceThreshold = 60.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolDescriptionMethod 创建 ToolDescriptionMethod 实例。
//
// 对齐 Python: ToolDescriptionMethod.__init__(self, config, eval_fn)
func NewToolDescriptionMethod(config map[string]any, model *llm.Model, evalFn *SimpleEval) *ToolDescriptionMethod {
	return &ToolDescriptionMethod{
		BaseMethod: *NewBaseMethod(config, model),
		evalFn:     evalFn,
	}
}

// Step 执行单步扩展。
// it==0 时返回原始描述，it>0 时加载负例并生成增强描述。
//
// 对齐 Python: ToolDescriptionMethod.step(tool, examples, prev_outputs, it)
func (m *ToolDescriptionMethod) Step(
	ctx context.Context,
	tool map[string]any,
	examples any,
	prevOutputs []any,
	it int,
) (output any, data any, score float64, err error) {
	var outputMap map[string]any

	if it == 0 {
		description := m.GetOriginalDescription(tool)
		outputMap = map[string]any{
			"description": description,
			"iteration":   0,
		}
		logger.Info(logComponent).
			Str("output", fmt.Sprintf("%v", outputMap)).
			Msg("Current description - original description")
	} else {
		// 对齐 Python: load negative ex
		functionName := getToolName(tool)
		negExamples := m.GetNegativeExamples(functionName)
		examplesObtained := map[string]any{
			"neg_examples": negExamples,
			"examples":     examples,
		}
		// 对齐 Python: improve with neg ex
		outputMap = m.Generate(ctx, tool, examplesObtained, prevOutputs, it)
		logger.Info(logComponent).
			Str("output", fmt.Sprintf("%v", outputMap)).
			Msg("Current description - generated description")
	}

	// 对齐 Python: eval with pos ex
	exampleTuples := descToExampleTuples(examples)
	results := m.EvalLoop(ctx, tool, toString(outputMap["description"]), exampleTuples, 1)

	// 对齐 Python: output = output | results
	for k, v := range descResultsToMap(results) {
		outputMap[k] = v
	}

	description := toString(outputMap["description"])
	scoreAvg := descToFloat64(outputMap["score_avg"])
	return outputMap, description, scoreAvg, nil
}

// Generate 生成增强描述。
//
// 对齐 Python: ToolDescriptionMethod.generate(tool, examples, prev_outputs, it)
func (m *ToolDescriptionMethod) Generate(
	ctx context.Context,
	tool map[string]any,
	examples any,
	prevOutputs []any,
	it int,
) map[string]any {
	logger.Info(logComponent).Msg("Generating desc")
	output := m.GenerateDescriptionFromDocumentation(ctx, tool, examples, prevOutputs)
	logger.Info(logComponent).Msg("Generating desc finished")
	output["iteration"] = it
	return output
}

// EvalLoop 评估循环。
//
// 对齐 Python: ToolDescriptionMethod.eval_loop(tool, description, examples, runs)
func (m *ToolDescriptionMethod) EvalLoop(
	ctx context.Context,
	tool map[string]any,
	description string,
	examples []ExampleTuple,
	runs int,
) *EvalResult {
	return m.evalFn.Eval(ctx, tool, description, examples, runs)
}

// CritiqueDescriptions 批判描述（正负例对比版）。
// Python 中有两个同名方法，后者覆盖前者，此处实现后者（正负例对比版）。
//
// 对齐 Python: ToolDescriptionMethod.critique_descriptions（第二个定义，204-311 行）
func (m *ToolDescriptionMethod) CritiqueDescriptions(
	ctx context.Context,
	tool map[string]any,
	examples []ExampleTuple,
	prevOutputs []map[string]any,
) (map[string]any, error) {
	functionName := getToolName(tool)
	docStr := toJSON(tool)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
        You are given a function %s with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

        Documentation:
        %s

        `, functionName, docStr)

	if len(examples) > 0 && prevOutputs != nil && len(prevOutputs) > 0 {
		// 对齐 Python: Separate positive and negative examples based on performance threshold
		positiveExamples := []map[string]any{}
		negativeExamples := []map[string]any{}

		// 对齐 Python: prev_outputs[::-1][:self.config['num_feedback_steps']][::-1]
		numFeedbackSteps := getConfigInt(m.config, "num_feedback_steps")
		reversedOutputs := descReverseSlice(prevOutputs)
		if numFeedbackSteps > 0 && len(reversedOutputs) > numFeedbackSteps {
			reversedOutputs = reversedOutputs[:numFeedbackSteps]
		}
		selectedOutputs := descReverseSlice(reversedOutputs)

		for _, output := range selectedOutputs {
			scoreAvg := descToFloat64(output["score_avg"])
			if scoreAvg >= descPerformanceThreshold {
				positiveExamples = append(positiveExamples, output)
			} else {
				negativeExamples = append(negativeExamples, output)
			}
		}

		// 对齐 Python: Add positive examples section
		if len(positiveExamples) > 0 {
			userPrompt += "\n=== POSITIVE EXAMPLES (Good Performance) ===\n"
			userPrompt += "The following tool descriptions achieved good performance:\n\n"

			for _, output := range positiveExamples {
				itVal := descToInt(output["iteration"])
				if itVal == 0 {
					userPrompt += "Original description: "
				} else {
					userPrompt += fmt.Sprintf("Iteration #%d, description=", itVal)
				}
				userPrompt += fmt.Sprintf("%s\n", toString(output["description"]))

				userPrompt += "Instructions solved successfully: "
				results := descToSliceOfMaps(output["results"])
				for i, pair := range descZipExampleAndResult(examples, results) {
					userPrompt += fmt.Sprintf("%d. instruction=\"%s\", answer=\"%s\", errors: ", i+1, pair.Example.Instruction, toString(pair.Result["answer"]))
					errs := descToSliceOfMaps(pair.Result["errors"])
					if len(errs) == 0 {
						userPrompt += "None"
					} else {
						for j, errItem := range errs {
							userPrompt += fmt.Sprintf("(%d) function_call=%s, arguments=%s, error=%s ",
								j, toString(errItem["function_name"]),
								toJSON(errItem["arguments"]),
								descTruncateString(toString(errItem["error_msg"]), 512),
							)
						}
					}
					userPrompt += fmt.Sprintf(". Ground truth: %s.\n", toJSON(pair.Example.FnCall))
				}

				userPrompt += fmt.Sprintf("Performance: score=%v%%, stdev=%v.\n\n",
					output["score_avg"], output["score_std"])
			}
		}

		// 对齐 Python: Add negative examples section
		if len(negativeExamples) > 0 {
			userPrompt += "\n=== NEGATIVE EXAMPLES (Poor Performance) ===\n"
			userPrompt += "The following tool descriptions had poor performance:\n\n"

			for _, output := range negativeExamples {
				itVal := descToInt(output["iteration"])
				if itVal == 0 {
					userPrompt += "Original description: "
				} else {
					userPrompt += fmt.Sprintf("Iteration #%d, description=", itVal)
				}
				userPrompt += fmt.Sprintf("%s\n", toString(output["description"]))

				userPrompt += "Instructions with problems: "
				results := descToSliceOfMaps(output["results"])
				for i, pair := range descZipExampleAndResult(examples, results) {
					userPrompt += fmt.Sprintf("%d. instruction=\"%s\", answer=\"%s\", errors: ", i+1, pair.Example.Instruction, toString(pair.Result["answer"]))
					errs := descToSliceOfMaps(pair.Result["errors"])
					if len(errs) == 0 {
						userPrompt += "None"
					} else {
						for j, errItem := range errs {
							userPrompt += fmt.Sprintf("(%d) function_call=%s, arguments=%s, error=%s ",
								j, toString(errItem["function_name"]),
								toJSON(errItem["arguments"]),
								descTruncateString(toString(errItem["error_msg"]), 512),
							)
						}
					}
					userPrompt += fmt.Sprintf(". Ground truth: %s.\n", toJSON(pair.Example.FnCall))
				}

				userPrompt += fmt.Sprintf("Performance: score=%v%%, stdev=%v.\n\n",
					output["score_avg"], output["score_std"])
			}
		}

		// 对齐 Python: critique prompt 一比一复刻
		userPrompt += fmt.Sprintf(`
            Now your task is to critique the descriptions by comparing positive and negative examples. A good description maximizes the score, minimizes the stdev, and helps the assistant correctly use the function without errors. In your analysis:

            (1) POSITIVE PATTERN ANALYSIS: Identify what makes the high-performing descriptions (>%.0f%%) successful. What specific phrases, structures, or information do they contain that help the assistant use the function correctly?

            (2) NEGATIVE PATTERN ANALYSIS: Identify what causes low-performing descriptions to fail. What specific errors does the assistant make, and what aspects of these descriptions lead to confusion or incorrect function calls?

            (3) CONTRAST AND RECOMMENDATIONS: Compare positive vs negative patterns. What are the key differences? What specific improvements would transform a negative example into a positive one?

            Your analysis should be less than 500 characters long, do not violate.
            `, descPerformanceThreshold)
	}

	prompt := FormatPromptLlama("", userPrompt)

	verifyFn := func(output string) (any, error) {
		return map[string]any{"analysis": strings.TrimSpace(output)}, nil
	}

	policy := descDefaultPolicy()
	return descInvokeWithVerifyToMap(ctx, m.model, getConfigString(m.config, "eval_model_id"), prompt, policy, verifyFn)
}

// CritiqueAllDescriptions 批判所有描述（正负例对比）。
//
// 对齐 Python: ToolDescriptionMethod.critique_all_descriptions
func (m *ToolDescriptionMethod) CritiqueAllDescriptions(
	ctx context.Context,
	tool map[string]any,
	examples any,
	prevOutputs []map[string]any,
) (map[string]any, error) {
	functionName := getToolName(tool)
	docStr := toJSON(tool)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
        You are given a function %s with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

        Documentation:
        %s`, functionName, docStr)

	// 对齐 Python: examples is a dict with "examples" and "neg_examples"
	examplesMap, ok := examples.(map[string]any)
	if !ok || len(examplesMap) == 0 {
		prompt := FormatPromptLlama("", userPrompt)
		verifyFn := func(output string) (any, error) {
			return map[string]any{"analysis": strings.TrimSpace(output)}, nil
		}
		policy := descDefaultPolicy()
		return descInvokeWithVerifyToMap(ctx, m.model, getConfigString(m.config, "eval_model_id"), prompt, policy, verifyFn)
	}

	positiveExamples := descToExampleTuples(examplesMap["examples"])
	negativeExamples := descToExampleTuples(examplesMap["neg_examples"])

	// 对齐 Python: Add positive examples section
	if len(positiveExamples) > 0 {
		userPrompt += "\n=== POSITIVE EXAMPLES (Good Performance) ===\n"
		userPrompt += "The following examples achieved good performance:\n\n"

		for i, ex := range positiveExamples {
			fnOutput := ex.FnOutput
			userPrompt += fmt.Sprintf("%d. instruction=\"%s\", Ground truth: %s.\n", i+1, ex.Instruction, toJSON(ex.FnCall))
			if len(fnOutput) > 256 {
				fnOutput = fnOutput[:256]
				userPrompt += fmt.Sprintf("Example response of the function: %s, etc", fnOutput)
			} else {
				userPrompt += fmt.Sprintf("Response of the function: %s", fnOutput)
			}
		}
	}

	// 对齐 Python: Add negative examples section
	if len(negativeExamples) > 0 {
		userPrompt += "\n=== NEGATIVE EXAMPLES (Poor Performance) ===\n"
		userPrompt += "The following tool descriptions had poor performance:\n\n"

		for i, ex := range negativeExamples {
			fnOutput := ex.FnOutput
			userPrompt += fmt.Sprintf("%d. instruction=\"%s\", The ", i+1, ex.Instruction)
			userPrompt += fmt.Sprintf("function call system generated: %s.", toJSON(ex.FnCall))
			if len(fnOutput) > 256 {
				fnOutput = fnOutput[:256]
				userPrompt += fmt.Sprintf("Example response of the function: %s", fnOutput)
			} else {
				userPrompt += fmt.Sprintf("Response of the function: %s", fnOutput)
			}
		}
	}

	// 对齐 Python: critique prompt 一比一复刻
	userPrompt += `
            Now your task is to critique the descriptions by comparing positive and negative examples. In your analysis:

            (1) POSITIVE PATTERN ANALYSIS: Identify patterns in successful cases. What specific phrases, structures, or information do they contain that help the assistant use the function correctly?

            (2) NEGATIVE PATTERN ANALYSIS: Identify what causes un-successful cases. What specific errors does the assistant make, and what aspects of these descriptions lead to confusion or incorrect function calls?

            (3) CONTRAST AND RECOMMENDATIONS: Compare positive vs negative patterns. What are the key differences? Analyze carefully to uncover any unspecified constrains or limitations?

            Your analysis should be less than 500 characters long, do not violate.
            `

	prompt := FormatPromptLlama("", userPrompt)

	verifyFn := func(output string) (any, error) {
		return map[string]any{"analysis": strings.TrimSpace(output)}, nil
	}

	policy := descDefaultPolicy()
	return descInvokeWithVerifyToMap(ctx, m.model, getConfigString(m.config, "eval_model_id"), prompt, policy, verifyFn)
}

// CritiqueNegativeExamples 批判负例。
//
// 对齐 Python: ToolDescriptionMethod.critique_negative_examples
func (m *ToolDescriptionMethod) CritiqueNegativeExamples(
	ctx context.Context,
	tool map[string]any,
	examples []ExampleTuple,
) (map[string]any, error) {
	functionName := getToolName(tool)
	docStr := toJSON(tool)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
        You are given a function %s with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

        Documentation:
        %s
        `, functionName, docStr)

	if len(examples) > 0 {
		userPrompt += (
			// 对齐 Python: 一比一复刻
			"\nPreviously, the given tool was used in solving instructions " +
				"by a tool assistant with the following function descriptions:\n")
		userPrompt += (
			// 对齐 Python: 一比一复刻
			"Here are the instructions the assistant " +
				"tried to solve with this tool description, with " +
				"their corresponding answers and errors produced by the assistant: ")

		for i, ex := range examples {
			fnOutput := ex.FnOutput
			if len(fnOutput) > 256 {
				fnOutput = fnOutput[:256]
				userPrompt += fmt.Sprintf("Example response of the function: %s, etc", fnOutput)
			} else {
				userPrompt += fmt.Sprintf("Response of the function: %s", fnOutput)
			}

			userPrompt += fmt.Sprintf("%d. instruction=\"%s\"", i+1, ex.Instruction)
			userPrompt += ". The system generated function call as below "
			userPrompt += fmt.Sprintf("  base on the original documentation: %s.\n", toJSON(ex.FnCall))
			userPrompt += fmt.Sprintf("The runction output obtained is %s: fn_output. ", fnOutput)
			userPrompt += fmt.Sprintf("And thus result to answer=\"%s\"", ex.Answer)
		}

		// 对齐 Python: critique prompt 一比一复刻
		userPrompt += `

            Now your task is to critique the descriptions based on these results. In your analysis:
            (1) Identify how the descriptions affect the function call errors of the assistant. Be specific on which errors the assistant tends to make, and find patterns in the description that causes the assistant to make such errors.
            (2) Identify any constrains or limitations the tool have. Analyze how the description can be improved so that it reflect the ability constrains.

            Your analysis should be less than 500 characters long, do not violate.
            `
	}

	prompt := FormatPromptLlama("", userPrompt)

	verifyFn := func(output string) (any, error) {
		return map[string]any{"analysis": strings.TrimSpace(output)}, nil
	}

	policy := descDefaultPolicy()
	return descInvokeWithVerifyToMap(ctx, m.model, getConfigString(m.config, "eval_model_id"), prompt, policy, verifyFn)
}

// GenerateDescriptionFromDocumentation 从文档生成增强描述。
//
// 对齐 Python: ToolDescriptionMethod.generate_description_from_documentation
func (m *ToolDescriptionMethod) GenerateDescriptionFromDocumentation(
	ctx context.Context,
	tool map[string]any,
	examples any,
	prevOutputs []any,
) map[string]any {
	// 对齐 Python: td - MOD PROMPT TO ANALYZE NEGATIVE CASE
	examplesMap, _ := examples.(map[string]any)
	var pos []ExampleTuple
	if examplesMap != nil {
		pos = descToExampleTuples(examplesMap["examples"])
		// neg 在 CritiqueAllDescriptions 中通过 examples 整体传递
	}

	typedPrevOutputs := descToSliceOfMapsFromAny(prevOutputs)
	tmp, _ := m.CritiqueDescriptions(ctx, tool, pos, typedPrevOutputs)
	tmpContrast, _ := m.CritiqueAllDescriptions(ctx, tool, examples, typedPrevOutputs)

	analysis := toString(tmp["analysis"])
	analysisContrast := toString(tmpContrast["analysis"])
	functionName := getToolName(tool)
	docStr := toJSON(tool)

	// 对齐 Python: user_prompt 一比一复刻
	userPrompt := fmt.Sprintf(`
        You are given an API tool with the following documentation, which includes the functionality description, required parameters, code snippets for API calls, etc.

        Documentation:
        %s

        `, docStr)

	if len(examplesMap) > 0 && prevOutputs != nil && len(prevOutputs) > 0 {
		userPrompt += (
			// 对齐 Python: 一比一复刻
			"\nPreviously, the given tool was used in solving instructions " +
				"by a tool assistant with the following function descriptions:\n")

		numFeedbackSteps := getConfigInt(m.config, "num_feedback_steps")
		reversedOutputs := descReverseAnySlice(prevOutputs)
		if numFeedbackSteps > 0 && len(reversedOutputs) > numFeedbackSteps {
			reversedOutputs = reversedOutputs[:numFeedbackSteps]
		}
		selectedOutputs := descReverseAnySlice(reversedOutputs)

		for _, outputAny := range selectedOutputs {
			output := descToMap(outputAny)
			itVal := descToInt(output["iteration"])
			if itVal == 0 {
				userPrompt += "Original description: "
			} else {
				userPrompt += fmt.Sprintf("Iteration #%d, description=", itVal)
			}
			userPrompt += fmt.Sprintf("%s\n", toString(output["description"]))
			userPrompt += "Performance of this description is: "
			userPrompt += fmt.Sprintf(" score=%v%%, stdev=%v.\n", output["score_avg"], output["score_std"])
		}
		// 对齐 Python: analysis 拼接一比一复刻
		userPrompt += fmt.Sprintf("\nFurthermore, an analysis was performed on the "+
			"descriptions for the previous iterations: \"%s\". An "+
			"analysis was performed on the negative cases for the cons"+
			"trains and ability limits of the function: \"%s\"", analysis, analysisContrast)

		// 对齐 Python: enhancement prompt 一比一复刻
		userPrompt += fmt.Sprintf(`
Your task is to further enhance the description for the function %s to MODIFY THE TOOL DESCRIPTION and PARAMETER DESCRIPTION part, with the objective of maximizing the score, minimizing the stdev, and help the assistant correctly use the function without errors.

Incorporate the analysis and generate the enhanced descriptions. The enhanced description should focus on what this tool can or cannot do, and add the capability boundaries of the tool, e.g., "returns summaries, not full text", "covers domestic locations only", "supports English language only", etc.

The enhanced description should not be longer than 1000 characters, do not violate this.
`, functionName)
	}

	// 对齐 Python: desired_desc_schema 一比一复刻
	desiredDescSchema := `{"type":"","name":"","description":"","parameters":{"type":"","properties":{"<PARAMETER_NAME_0>":{"type":"","description":""},"<PARAMETER_NAME_1>":{"type":"","description":""}},"required":["<PARAMETER_NAME>"]}}`

	// 对齐 Python: IMPORTANT output format 一比一复刻
	userPrompt += fmt.Sprintf(`
**IMPORTANT**: You must preserve the exact JSON schema structure provided below. Only modify the text content - do not change schema structure.

**IMPORTANT**: Since no extra fields can be added, include capability boundaries within the main tool description text. Be explicit about what the function CANNOT do to prevent misuse.

**Required Output Format:**
Return JSON following this exact schema structure (modify only description texts):
{
    "description": %s
}

**Critical**: Maintain all field names, types, and schema structure. Only enhance the textual detail contents.
`, desiredDescSchema)

	prompt := FormatPromptLlama("", userPrompt)

	verifyFn := func(output string) (any, error) {
		outputJSON := ParseJSON(output, "description")
		if _, ok := outputJSON["description"]; !ok {
			return nil, fmt.Errorf("no \"description\" found in output")
		}
		outputJSON["description"] = strings.TrimSpace(fmt.Sprintf("%v", outputJSON["description"]))
		return outputJSON, nil
	}

	policy := descDefaultPolicy15()
	result, err := InvokeWithVerify(ctx, m.model, getConfigString(m.config, "gen_model_id"), prompt, policy, verifyFn)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if resultMap, ok := result.(map[string]any); ok {
		return resultMap
	}
	return map[string]any{"description": fmt.Sprintf("%v", result)}
}

// LoadExamples 从 JSON 文件加载示例。
//
// 对齐 Python: ToolDescriptionMethod.load_examples(examples_dir, function_name, max_num_examples)
func (m *ToolDescriptionMethod) LoadExamples(examplesDir, functionName string, maxNumExamples int) ([]ExampleTuple, error) {
	examplesPath := examplesDir + "/" + functionName + ".json"
	logger.Info(logComponent).
		Str("examples_path", examplesPath).
		Msg("Trying to load examples")

	data, err := os.ReadFile(examplesPath)
	if err != nil {
		return nil, fmt.Errorf("读取示例文件失败: %w", err)
	}

	var allOutputs []any
	if err := json.Unmarshal(data, &allOutputs); err != nil {
		return nil, fmt.Errorf("解析示例文件失败: %w", err)
	}
	if allOutputs == nil {
		return nil, fmt.Errorf("示例文件内容为空")
	}

	return descSelectExamples(allOutputs, maxNumExamples, true), nil
}

// GetNegativeExamples 获取负例。
// 如果配置路径不存在，回退到从示例目录加载。
//
// 对齐 Python: ToolDescriptionMethod.get_negative_examples(function_name)
func (m *ToolDescriptionMethod) GetNegativeExamples(functionName string) []ExampleTuple {
	examplesPath := getConfigString(m.config, "neg_ex_input_path")
	maxNumExamples := getConfigInt(m.config, "num_examples_for_desc")

	var allOutputs []any

	if _, err := os.Stat(examplesPath); err == nil {
		// 对齐 Python: load from provided path
		data, err := os.ReadFile(examplesPath)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("examples_path", examplesPath).
				Msg("读取负例文件失败")
			return nil
		}
		if err := json.Unmarshal(data, &allOutputs); err != nil {
			logger.Error(logComponent).Err(err).
				Str("examples_path", examplesPath).
				Msg("解析负例文件失败")
			return nil
		}
	} else {
		// 对齐 Python: if not found, fallback to load from self play examples
		logger.Warn(logComponent).
			Str("examples_path", examplesPath).
			Msg("NO NEGATIVE FILE FOUND, FALLBACK TO LOAD GENERATED EXAMPLES")
		examplesPath = getConfigString(m.config, "examples_dir") + "/" + functionName + ".json"
		data, err := os.ReadFile(examplesPath)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("examples_path", examplesPath).
				Msg("回退读取示例文件失败")
			return nil
		}
		if err := json.Unmarshal(data, &allOutputs); err != nil {
			logger.Error(logComponent).Err(err).
				Str("examples_path", examplesPath).
				Msg("回退解析示例文件失败")
			return nil
		}
	}

	if allOutputs == nil {
		logger.Error(logComponent).Msg("负例文件内容为空")
		return nil
	}

	return descSelectNegativeExamples(allOutputs, maxNumExamples)
}

// GetOriginalDescription 获取工具的原始描述。
// 如果描述中包含 'The description of this function is: "' 前缀，则提取其中的内容。
//
// 对齐 Python: ToolDescriptionMethod.get_original_description(tool)
func (m *ToolDescriptionMethod) GetOriginalDescription(tool map[string]any) string {
	description := toString(tool["description"])
	indicator := "The description of this function is: \""
	found := strings.Index(description, indicator)
	if found != -1 {
		description = description[found+len(indicator):]
		// 对齐 Python: description[found + len(indicator): -1]
		if len(description) > 0 {
			description = description[:len(description)-1]
		}
	}
	return description
}

// GetExamples 获取工具的示例数据（BeamSearchMethod 接口方法）。
//
// 对齐 Python: ToolDescriptionMethod.get_examples(tool)
func (m *ToolDescriptionMethod) GetExamples(ctx context.Context, tool map[string]any) any {
	functionName := getToolName(tool)
	var examples []ExampleTuple
	examplesDir := getConfigString(m.config, "examples_dir")
	if examplesDir != "" {
		maxNumExamples := getConfigInt(m.config, "num_examples_for_desc")
		loaded, err := m.LoadExamples(examplesDir, functionName, maxNumExamples)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("function_name", functionName).
				Msg("加载示例失败")
		} else {
			examples = loaded
		}
	}
	logger.Info(logComponent).
		Int("example_count", len(examples)).
		Str("function_name", functionName).
		Interface("examples", examples).
		Msg("Examples loaded for tool")
	return examples
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// descDefaultPolicy 创建默认 LLM 调用策略（15 次尝试）。
func descDefaultPolicy() llm_resilience.LLMInvokePolicy {
	return llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
}

// descDefaultPolicy15 创建 15 次尝试的 LLM 调用策略（generate 用）。
func descDefaultPolicy15() llm_resilience.LLMInvokePolicy {
	return llm_resilience.LLMInvokePolicy{
		MaxAttempts:        15,
		TotalBudgetSecs:    300,
		AttemptTimeoutSecs: 60,
		BackoffBaseSecs:    1.0,
	}
}

// descInvokeWithVerifyToMap 调用 InvokeWithVerify 并将结果断言为 map[string]any。
func descInvokeWithVerifyToMap(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
	verifyFn VerifyFunc,
) (map[string]any, error) {
	result, err := InvokeWithVerify(ctx, model, modelName, prompt, policy, verifyFn)
	if err != nil {
		return nil, err
	}
	if resultMap, ok := result.(map[string]any); ok {
		return resultMap, nil
	}
	return map[string]any{"result": result}, nil
}

// descToFloat64 安全地将 any 转为 float64。
func descToFloat64(v any) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// descToInt 安全地将 any 转为 int。
func descToInt(v any) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

// descToMap 安全地将 any 转为 map[string]any。
func descToMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// descToSliceAny 安全地将 any 转为 []any。
func descToSliceAny(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// descToSliceOfMaps 安全地将 any 转为 []map[string]any。
func descToSliceOfMaps(v any) []map[string]any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		result := make([]map[string]any, 0, len(s))
		for _, item := range s {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	}
	if s, ok := v.([]map[string]any); ok {
		return s
	}
	return nil
}

// descToSliceOfMapsFromAny 将 []any 转为 []map[string]any。
func descToSliceOfMapsFromAny(v []any) []map[string]any {
	result := make([]map[string]any, 0, len(v))
	for _, item := range v {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// descToExampleTuples 将 any 转为 []ExampleTuple。
// 支持从 []ExampleTuple 或 []any（每个元素为 []any: [instruction, fn_call, fn_output, answer]）转换。
func descToExampleTuples(v any) []ExampleTuple {
	if v == nil {
		return nil
	}
	// 如果已经是 []ExampleTuple
	if tuples, ok := v.([]ExampleTuple); ok {
		return tuples
	}
	// 如果是 []any 形式
	if slice, ok := v.([]any); ok {
		result := make([]ExampleTuple, 0, len(slice))
		for _, item := range slice {
			if t, ok := item.(ExampleTuple); ok {
				result = append(result, t)
			} else if arr, ok := item.([]any); ok && len(arr) >= 4 {
				inst, _ := arr[0].(string)
				fnCall, _ := arr[1].(map[string]any)
				fnOutput, _ := arr[2].(string)
				ans, _ := arr[3].(string)
				result = append(result, ExampleTuple{
					Instruction: inst,
					FnCall:      fnCall,
					FnOutput:    fnOutput,
					Answer:      ans,
				})
			}
		}
		return result
	}
	return nil
}

// descIsString 判断 any 是否为 string 类型。
func descIsString(v any) bool {
	_, ok := v.(string)
	return ok
}

// descTruncateString 截断字符串到指定长度。
func descTruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// descReverseSlice 反转 []map[string]any 切片。
func descReverseSlice(s []map[string]any) []map[string]any {
	result := make([]map[string]any, len(s))
	copy(result, s)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// descReverseAnySlice 反转 []any 切片。
func descReverseAnySlice(s []any) []any {
	result := make([]any, len(s))
	copy(result, s)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// descZipExampleAndResult 将示例和结果配对。
func descZipExampleAndResult(examples []ExampleTuple, results []map[string]any) []descExampleResultPair {
	n := len(examples)
	if len(results) < n {
		n = len(results)
	}
	pairs := make([]descExampleResultPair, n)
	for i := 0; i < n; i++ {
		pairs[i] = descExampleResultPair{
			Example: examples[i],
			Result:  results[i],
		}
	}
	return pairs
}

// descResultsToMap 将 EvalResult 转为 map[string]any。
func descResultsToMap(r *EvalResult) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	// 将 Results 转为 []map[string]any
	resultsSlice := make([]map[string]any, len(r.Results))
	for i, item := range r.Results {
		errorsSlice := make([]map[string]any, len(item.Errors))
		for j, e := range item.Errors {
			errorsSlice[j] = map[string]any{
				"function_name": e.FunctionName,
				"arguments":     e.Arguments,
				"error_msg":     e.ErrorMsg,
			}
		}
		resultsSlice[i] = map[string]any{
			"instruction":                 item.Instruction,
			"expected_fn_call":            item.ExpectedFnCall,
			"generated_fn_call":           item.GeneratedFnCall,
			"fn_call_score":               item.FnCallScore,
			"execution_result":            item.ExecutionResult,
			"execution_error":             item.ExecutionError,
			"output_effectiveness_score":  item.OutputEffectivenessScore,
			"weighted_score":              item.WeightedScore,
			"answer":                      item.Answer,
			"errors":                      errorsSlice,
		}
	}
	return map[string]any{
		"score_avg":            r.ScoreAvg,
		"score_std":            r.ScoreStd,
		"fn_call_accuracy":     r.FnCallAccuracy,
		"output_effectiveness": r.OutputEffectiveness,
		"results":              resultsSlice,
	}
}

// descSelectExamples 从 allOutputs 中选择示例（LoadExamples 用，score >= 3）。
func descSelectExamples(allOutputs []any, maxNumExamples int, breakOnFirst bool) []ExampleTuple {
	selectedExamples := []ExampleTuple{}
	for _, nodeHistoryAny := range allOutputs {
		nodeHistory, ok := nodeHistoryAny.([]any)
		if !ok {
			continue
		}
		// 对齐 Python: node_history[::-1]
		for i := len(nodeHistory) - 1; i >= 0; i-- {
			stepOutput := descToMap(nodeHistory[i])
			if stepOutput == nil {
				continue
			}
			fnCall := descToMap(stepOutput["fn_call"])
			fnOutput := toString(stepOutput["tool_results"])
			instructions := descToSliceAny(stepOutput["instructions"])
			answers := descToSliceAny(stepOutput["answers"])
			if len(instructions) == 0 || len(answers) == 0 {
				continue
			}
			inst := toString(instructions[len(instructions)-1])
			ans := toString(answers[len(answers)-1])

			if scores, ok := stepOutput["scores"]; ok {
				scoreSlice := descToSliceAny(scores)
				if len(scoreSlice) > 0 {
					score := descToFloat64(scoreSlice[len(scoreSlice)-1])
					if score >= 3.0 && descIsString(inst) && descIsString(ans) {
						selectedExamples = append(selectedExamples, ExampleTuple{
							Instruction: strings.TrimSpace(inst),
							FnCall:      fnCall,
							FnOutput:    fnOutput,
							Answer:      strings.TrimSpace(ans),
						})
						if breakOnFirst {
							break
						}
					}
				}
			}
		}
	}

	if maxNumExamples > 0 && len(selectedExamples) > maxNumExamples {
		selectedExamples = selectedExamples[:maxNumExamples]
	}
	return selectedExamples
}

// descSelectNegativeExamples 从 allOutputs 中选择负例（score 1 <= x < 3，或无 scores 字段）。
func descSelectNegativeExamples(allOutputs []any, maxNumExamples int) []ExampleTuple {
	selectedExamples := []ExampleTuple{}
	for _, nodeHistoryAny := range allOutputs {
		nodeHistory, ok := nodeHistoryAny.([]any)
		if !ok {
			continue
		}
		for i := len(nodeHistory) - 1; i >= 0; i-- {
			stepOutput := descToMap(nodeHistory[i])
			if stepOutput == nil {
				continue
			}
			// 对齐 Python: check all variables exist
			requiredKeys := []string{"instructions", "fn_call", "tool_results", "answers"}
			allExist := true
			for _, k := range requiredKeys {
				if _, ok := stepOutput[k]; !ok {
					allExist = false
					break
				}
			}
			if !allExist {
				continue
			}

			fnCall := descToMap(stepOutput["fn_call"])
			fnOutput := toString(stepOutput["tool_results"])
			instructions := descToSliceAny(stepOutput["instructions"])
			answers := descToSliceAny(stepOutput["answers"])
			if len(instructions) == 0 || len(answers) == 0 {
				continue
			}
			inst := toString(instructions[len(instructions)-1])
			ans := toString(answers[len(answers)-1])

			if scores, ok := stepOutput["scores"]; ok {
				scoreSlice := descToSliceAny(scores)
				if len(scoreSlice) > 0 {
					score := descToFloat64(scoreSlice[len(scoreSlice)-1])
					// 对齐 Python: SCORE THRESHOLD 1. <= score < 3.
					if score >= 1.0 && score < 3.0 && descIsString(inst) && descIsString(ans) {
						selectedExamples = append(selectedExamples, ExampleTuple{
							Instruction: strings.TrimSpace(inst),
							FnCall:      fnCall,
							FnOutput:    fnOutput,
							Answer:      strings.TrimSpace(ans),
						})
					}
				}
			} else {
				selectedExamples = append(selectedExamples, ExampleTuple{
					Instruction: strings.TrimSpace(inst),
					FnCall:      fnCall,
					FnOutput:    fnOutput,
					Answer:      strings.TrimSpace(ans),
				})
			}
		}
	}

	if maxNumExamples > 0 && len(selectedExamples) > maxNumExamples {
		selectedExamples = selectedExamples[:maxNumExamples]
	}
	return selectedExamples
}
