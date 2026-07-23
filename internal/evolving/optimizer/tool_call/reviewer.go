package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolDescriptionReviewer 工具描述审查器，通过 LLM 对工具描述进行格式化、清洗、交叉检查和翻译。
//
// 对齐 Python: ToolDescriptionReviewer
type ToolDescriptionReviewer struct {
	// evalModelID 评估模型 ID
	evalModelID string
	// llmAPIKey LLM API 密钥
	llmAPIKey string
	// model LLM 模型客户端
	model *llm.Model
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolDescriptionReviewer 创建 ToolDescriptionReviewer 实例。
//
// 对齐 Python: ToolDescriptionReviewer.__init__(eval_model_id, llm_api_key)
func NewToolDescriptionReviewer(evalModelID string, llmAPIKey string, model *llm.Model) *ToolDescriptionReviewer {
	return &ToolDescriptionReviewer{
		evalModelID: evalModelID,
		llmAPIKey:   llmAPIKey,
		model:       model,
	}
}

// Format 将文本描述转换为目标 JSON 结构。
// 使用中文版 prompt，一比一复刻 Python 原文。
//
// 对齐 Python: ToolDescriptionReviewer.format(json_schema, description, example)
func (r *ToolDescriptionReviewer) Format(
	ctx context.Context,
	jsonSchema map[string]any,
	description string,
	example *string,
) (map[string]any, error) {
	// 对齐 Python: prompt（中文版）— 一比一复刻
	schemaJSON, _ := json.MarshalIndent(jsonSchema, "", "  ")
	prompt := fmt.Sprintf(`将下面输入转换为目标 JSON 结构。必须满足：

- 输出只允许是有效 JSON，且严格匹配目标结构的键路径与层级（不多不少）。
- 语义必须完全保留：不新增、不删减、不改写含义；可改写措辞以压缩。
- description 去冗余是强制要求：
    - 任何 "每项包含/含有/由…组成/字段包括…" 这类字段清单式描述都必须删除或改写为非清单表述。
    - 不得在 description 中重复 schema 已表达的信息：字段名、字段类型、required 已涵盖的"必填"。
    - 仅保留 schema 无法表达或未显式表达的约束到 description，例如：
        - 覆盖区间/不得留隙/分段规则
        - 默认值语义（如 inflationRate 默认 0）
        - 业务规则（按年累加、考虑通胀等）
    - 枚举值列表只出现一次，放在最贴近字段的位置（通常是该字段的 description）；不得在父级/子级重复。
    如输入中 description 同时包含"字段清单 + 业务约束"，只保留业务约束部分。
    - 若某个 description 完全是冗余字段清单，允许变为简短描述，但不得留空（除非输入本身为空）。
- 请直接输出转换后的 JSON，不要附加解释。

这是目标的json 模板:
%s

下面是你需要修改的json，生成后请自检：所有 description 中不得出现"含/包含/包括/each item/contains/fields"等字段列举句式；否则重写直到满足。

Input:
%s
`, string(schemaJSON), description)

	// 对齐 Python: verify_output = lambda output: json.loads(output)
	verifyFn := func(output string) (any, error) {
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			return nil, err
		}
		return result, nil
	}

	// 对齐 Python: get_rits_response('gpt-5.2', prompt, self.llm_api_key, verify_output=verify_output, max_attempts=5, ...)
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        5,
		TotalBudgetSecs:    120,
		AttemptTimeoutSecs: 30,
		BackoffBaseSecs:    1.0,
	}

	response, err := InvokeWithVerify(ctx, r.model, r.evalModelID, prompt, policy, verifyFn)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "Format").
			Err(err).
			Msg("Format 调用 LLM 失败")
		return nil, fmt.Errorf("Format 调用 LLM 失败: %w", err)
	}

	if resultMap, ok := response.(map[string]any); ok {
		return resultMap, nil
	}

	logger.Error(logComponent).
		Str("method", "Format").
		Str("response_type", fmt.Sprintf("%T", response)).
		Msg("Format 返回结果类型不是 map[string]any")
	return nil, fmt.Errorf("Format 返回结果类型不是 map[string]any: %T", response)
}

// CleanAndDeduplicate 清洗并去重工具描述 JSON。
// 提示词一比一复刻 Python 原文（英文）。
//
// 对齐 Python: ToolDescriptionReviewer.clean_and_deduplicate(data)
func (r *ToolDescriptionReviewer) CleanAndDeduplicate(
	ctx context.Context,
	data map[string]any,
) (map[string]any, error) {
	// 对齐 Python: prompt — 一比一复刻
	dataJSON, _ := json.MarshalIndent(data, "", "  ")
	prompt := fmt.Sprintf(`
Given a tool description JSON, go throught the content sentence
by sentence and perform the following cleaning tasks:

1. Remove usage example in the main tool description
2. Remove redundant "必填"/"可选"/"required"/"optional" markers in parameter
descriptions if they appear in 'required' session
3. Remove verbose, redundant descriptions including:
   - Disclaimers like "若输入无效会返回空结果",
    "若输入代码无效或未收录会返回未找到或空结果"
   - Obvious statements like "结果可能有延迟"
   - Suggestions like "调用者应自行进行进一步分析或合成总结",
    "调用者应在本接口返回后自行进行进一步分析"
   - Irrelevant exclusions that are clearly not in the tool's
    functional scope. e.g. the tool name is maps_directions,
    since it's a direction tool, statements like "不提供预订或支付功能"
    or "不支持语音导航" is clearly irrelevant and need to be removed.
   - Any other unnecessary verbose content
4. Clean up descriptions: for parameter descriptions incorrectly
mixed into the tool descriptions, relocate them to ensure that
each parameter description is correctly placed in its corresponding
parameter description instead of the main tool description session.

**Pay attention to KEEP statements on ACTUAL functionality boundaries**
Keep only unique, essential, and actionable information. Output only the
cleaned JSON without explanations. DO NOT change the overall structure of JSON.

Input JSON:
%s
`, string(dataJSON))

	verifyFn := func(output string) (any, error) {
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			return nil, err
		}
		return result, nil
	}

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        5,
		TotalBudgetSecs:    120,
		AttemptTimeoutSecs: 30,
		BackoffBaseSecs:    1.0,
	}

	response, err := InvokeWithVerify(ctx, r.model, r.evalModelID, prompt, policy, verifyFn)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "CleanAndDeduplicate").
			Err(err).
			Msg("CleanAndDeduplicate 调用 LLM 失败")
		return nil, fmt.Errorf("CleanAndDeduplicate 调用 LLM 失败: %w", err)
	}

	if resultMap, ok := response.(map[string]any); ok {
		return resultMap, nil
	}

	return nil, fmt.Errorf("CleanAndDeduplicate 返回结果类型不是 map[string]any: %T", response)
}

// CrossCheck 交叉检查修改后的描述与原始描述，补充丢失信息并整理位置。
// 提示词一比一复刻 Python 原文（中文）。
//
// 对齐 Python: ToolDescriptionReviewer.cross_check(data, ori_tool)
func (r *ToolDescriptionReviewer) CrossCheck(
	ctx context.Context,
	data map[string]any,
	oriTool string,
) (map[string]any, error) {
	// 对齐 Python: prompt（中文版）— 一比一复刻
	dataJSON, _ := json.MarshalIndent(data, "", "  ")
	prompt := fmt.Sprintf(`比较原始描述和修改后的描述，按照以下要求整理修改后的描述：
1. 补充修改后的描述丢失的信息：例如，参数可选值列表丢失，需把原始描述中的列表补充道修改后的对应位置。
2. 确保参数描述信息和工具描述信息位置正确：参考原始描述，确保工具描述中只包含对工具能力、边界等信息，确保参数具体细节要求应在对应的参数描述中，例如："仅支持经纬度作为输入"应当放在对应的参数描述中，不应当放在主工具能力边界中。

确保不要改变json格式，仅修改文字内容。不要删除内容，仅做整理和补充丢失信息。

原始描述：
%s

修改后描述（待优化）：
%s
`, oriTool, string(dataJSON))

	verifyFn := func(output string) (any, error) {
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			return nil, err
		}
		return result, nil
	}

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        5,
		TotalBudgetSecs:    120,
		AttemptTimeoutSecs: 30,
		BackoffBaseSecs:    1.0,
	}

	response, err := InvokeWithVerify(ctx, r.model, r.evalModelID, prompt, policy, verifyFn)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "CrossCheck").
			Err(err).
			Msg("CrossCheck 调用 LLM 失败")
		return nil, fmt.Errorf("CrossCheck 调用 LLM 失败: %w", err)
	}

	if resultMap, ok := response.(map[string]any); ok {
		return resultMap, nil
	}

	return nil, fmt.Errorf("CrossCheck 返回结果类型不是 map[string]any: %T", response)
}

// TranslateToChinese 将工具描述中的英文翻译为中文。
// 如果文本主要是中文则直接返回，不调用 LLM。
// 提示词一比一复刻 Python 原文（英文）。
//
// 对齐 Python: ToolDescriptionReviewer.translate_to_chinese(data)
func (r *ToolDescriptionReviewer) TranslateToChinese(
	ctx context.Context,
	data map[string]any,
) (map[string]any, error) {
	// 对齐 Python: json_str = json.dumps(data, ensure_ascii=False)
	jsonStr, _ := json.Marshal(data)

	// 对齐 Python: if not self._is_mostly_english(json_str): return data
	if !isMostlyEnglish(string(jsonStr)) {
		return data, nil
	}

	// 对齐 Python: prompt — 一比一复刻
	dataJSON, _ := json.MarshalIndent(data, "", "  ")
	prompt := fmt.Sprintf(`Translate all English text in the following JSON to Chinese.
Keep JSON structure unchanged. Keep technical terms and code examples as-is.
Output only the translated JSON without explanations.

Input JSON:
%s
`, string(dataJSON))

	verifyFn := func(output string) (any, error) {
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			return nil, err
		}
		return result, nil
	}

	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:        5,
		TotalBudgetSecs:    120,
		AttemptTimeoutSecs: 30,
		BackoffBaseSecs:    1.0,
	}

	response, err := InvokeWithVerify(ctx, r.model, r.evalModelID, prompt, policy, verifyFn)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "TranslateToChinese").
			Err(err).
			Msg("TranslateToChinese 调用 LLM 失败")
		return nil, fmt.Errorf("TranslateToChinese 调用 LLM 失败: %w", err)
	}

	if resultMap, ok := response.(map[string]any); ok {
		return resultMap, nil
	}

	return nil, fmt.Errorf("TranslateToChinese 返回结果类型不是 map[string]any: %T", response)
}

// Process 按步骤顺序执行描述处理流程。
// 步骤顺序：clean → cross_check → translate
//
// 对齐 Python: ToolDescriptionReviewer.process(data, ori_tool, steps)
func (r *ToolDescriptionReviewer) Process(
	ctx context.Context,
	data map[string]any,
	oriTool string,
	steps []string,
) (map[string]any, error) {
	// 对齐 Python: result = data
	result := data

	for _, step := range steps {
		var err error
		switch step {
		// 对齐 Python: if step == "cross_check": result = self.cross_check(data=data, ori_tool=ori_tool)
		case "cross_check":
			result, err = r.CrossCheck(ctx, data, oriTool)
		// 对齐 Python: elif step == "clean": result = self.clean_and_deduplicate(result)
		case "clean":
			result, err = r.CleanAndDeduplicate(ctx, result)
		// 对齐 Python: elif step == "translate": result = self.translate_to_chinese(result)
		case "translate":
			result, err = r.TranslateToChinese(ctx, result)
		// 对齐 Python: else: raise ValueError(f"Unknown processing step: {step}")
		default:
			return nil, fmt.Errorf("未知的处理步骤: %s", step)
		}
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isMostlyEnglish 判断文本是否主要由英文字符组成。
// 计算英文字符占比，超过 0.7 则判断为英文。
//
// 对齐 Python: ToolDescriptionReviewer._is_mostly_english(text)
func isMostlyEnglish(text string) bool {
	// 对齐 Python: text_no_space = re.sub(r'\s+', '', text)
	re := regexp.MustCompile(`\s+`)
	textNoSpace := re.ReplaceAllString(text, "")

	// 对齐 Python: if len(text_no_space) == 0: return False
	if len(textNoSpace) == 0 {
		return false
	}

	// 对齐 Python: english_chars = len(re.findall(r'[a-zA-Z]', text_no_space))
	englishRe := regexp.MustCompile(`[a-zA-Z]`)
	englishChars := len(englishRe.FindAllString(textNoSpace, -1))

	// 对齐 Python: english_ratio = english_chars / len(text_no_space)
	// 对齐 Python: return english_ratio > 0.7
	englishRatio := float64(englishChars) / float64(len(textNoSpace))
	return englishRatio > 0.7
}
