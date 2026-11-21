package judger

// 判分契约：评分展开、判决解析与分数重算。
// Python: openjiuwen/rsi/harness_rsi/evaluator/judger/scoring.py（285 行）。
// 方法体步骤逐条对齐 Python 原文，步骤顺序不可调换（规则 9/12）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"

	dataloader "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/data_loader"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrMissingJudgeVerdict 判决缺失哨兵：输出中不含任何可识别 JSON 判决
// （Python MissingJudgeVerdictError，恢复链以此决定是否进入格式修复）。
var ErrMissingJudgeVerdict = errors.New("judge output contains no JSON verdict")

// requirementResultsContractResolver 需求结果契约解析缝（依赖反转）。
// Python score_judge_output 直接调 evaluator.requirement_results.requirement_results_contract；
// Go 侧 evaluator 包已 import 本包（metrics_collector.go），反向 import 将成环，
// 故由 evaluator 包 init 注册实现（偏差登记：spec §10 缝位 4）。步骤归属不变。
var requirementResultsContractResolver func(items []map[string]any) map[string]any

// RegisterRequirementResultsContract 由 evaluator 包注册契约实现（init 调用）。
func RegisterRequirementResultsContract(fn func(items []map[string]any) map[string]any) {
	requirementResultsContractResolver = fn
}

// fenceRe JSON 围栏匹配（Python L135-139，MULTILINE|DOTALL|IGNORECASE）。
var fenceRe = regexp.MustCompile("(?ims)^```(?:json)?[ \t]*\r?\n(.*?)^```[ \t]*(?:\r?\n|$)")

// braceRe JSON 起始字符扫描（Python L112/L150 re.finditer(r"[\[{]", text)）。
var braceRe = regexp.MustCompile(`[\[{]`)

// verdictKeyRe 候选位置解析失败时是否重抛（Python L156-158）。
var verdictKeyRe = regexp.MustCompile(`^\{\s*"(?:status|overall_reason|behaviors|forbidden_hits|score)"\s*:`)

// ──────────────────────────── 导出函数 ────────────────────────────

// FiniteNumber 拒绝 bool、可转字符串与非有限分数。
// Python: scoring.py:20 finite_number。bool-as-int 在 Go 类型层不可能，属已知类型层偏差。
func FiniteNumber(value any, minimum, maximum float64, name string) (float64, error) {
	var v float64
	switch n := value.(type) {
	case bool:
		return 0, fmt.Errorf("%s must be a number", name)
	case int:
		v = float64(n)
	case int64:
		v = float64(n)
	case float64:
		v = n
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", name)
		}
		v = parsed
	default:
		return 0, fmt.Errorf("%s must be a number", name)
	}
	if math.IsInf(v, 0) || math.IsNaN(v) || v < minimum || v > maximum {
		return 0, fmt.Errorf("%s must be finite and between %v and %v", name, minimum, maximum)
	}
	return v, nil
}

// ScoringContract 接受 answer/rubric 并保留旧的 required/forbidden behavior 格式。
// Python: scoring.py:61 scoring_contract。
func ScoringContract(caseMapping map[string]any) ([]map[string]any, []map[string]any, error) {
	normalized, err := dataloader.NormalizeGradingCase(caseMapping)
	if err != nil {
		return nil, nil, err
	}
	rawReference, present := normalized["reference"]
	if !present {
		rawReference = map[string]any{}
	}
	reference, ok := rawReference.(map[string]any)
	if !ok {
		return nil, nil, errors.New("reference must be an object")
	}
	answerRole, _ := reference["answer_role"].(string)
	if answerRole == "" {
		answerRole = "criterion"
	}
	if answerRole != "criterion" && answerRole != "reference" {
		return nil, nil, errors.New("reference.answer_role must be criterion or reference")
	}
	penaltyMode, _ := reference["penalty_mode"].(string)
	if penaltyMode == "" {
		penaltyMode = "ceiling"
	}
	if penaltyMode != "ceiling" && penaltyMode != "subtract" {
		return nil, nil, errors.New("reference.penalty_mode must be ceiling or subtract")
	}
	behaviors, err := normalizeItems(normalizeItemsInput(reference["required_behaviors"]), false)
	if err != nil {
		return nil, nil, err
	}
	rubric, ok := reference["rubric"].([]any)
	if reference["rubric"] != nil && !ok {
		return nil, nil, errors.New("reference.rubric must be a list of strings")
	}
	for index, text := range rubric {
		s, isStr := text.(string)
		if !isStr || strings.TrimSpace(s) == "" {
			return nil, nil, errors.New("reference.rubric must contain non-empty strings")
		}
		behaviors = append(behaviors, map[string]any{
			"id":          fmt.Sprintf("rubric_%03d", index+1),
			"description": s,
			"weight":      1.0,
		})
	}
	if referenceAnswer(normalized) != nil && answerRole == "criterion" {
		// 首位插入参考答案项（Python L80-88 insert(0, ...)）。
		behaviors = append([]map[string]any{{
			"id":          "reference_answer",
			"description": "The answer matches the reference and satisfies explicit output constraints.",
			"weight":      1.0,
		}}, behaviors...)
	}
	if len(behaviors) == 0 {
		return nil, nil, errors.New("llm_as_judge requires reference.answer, reference.rubric or reference.required_behaviors")
	}
	if err := uniqueIDs(behaviors); err != nil {
		return nil, nil, err
	}
	forbidden, err := normalizeItems(normalizeItemsInput(reference["forbidden_behaviors"]), true)
	if err != nil {
		return nil, nil, err
	}
	return behaviors, forbidden, nil
}

// ParseJudgeOutput 接受一个完整 JSON 判决，允许围栏或前后散文包裹。
// Python: scoring.py:131 parse_judge_output。
func ParseJudgeOutput(raw string) (map[string]any, error) {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") && strings.Contains(text, "```") {
		loc := fenceRe.FindStringSubmatchIndex(text)
		if loc == nil {
			return nil, errors.New("judge output must contain one complete JSON fence")
		}
		outside := text[:loc[0]] + text[loc[1]:]
		if strings.Contains(outside, "```") || containsJudgePayload(outside) {
			return nil, errors.New("ambiguous judge output: more than one structured payload")
		}
		text = strings.TrimSpace(text[loc[2]:loc[3]])
	}
	var parsed any
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		dec := json.NewDecoder(strings.NewReader(text))
		value, err := decodeJSONValue(dec)
		if err != nil {
			return nil, err
		}
		// Python decoder.decode 在尾部多余数据时报 Extra data（对齐校验）。
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("extra data after judge JSON: %w", errOrExtra(err))
		}
		parsed = value
	} else {
		var candidates []any
		end := 0
		for _, loc := range braceRe.FindAllStringIndex(text, -1) {
			if loc[0] < end {
				continue
			}
			value, endOffset, err := rawDecodeAt(text, loc[0])
			if err != nil {
				// Python L156-158：判决键开头的候选解析失败时重抛。
				if text[loc[0]] == '{' && verdictKeyRe.MatchString(text[loc[0]:]) {
					return nil, err
				}
				continue
			}
			end = endOffset
			if containsJudgePayload(text[loc[0]:end]) {
				candidates = append(candidates, value)
			}
		}
		if len(candidates) == 0 {
			return nil, ErrMissingJudgeVerdict
		}
		if len(candidates) != 1 {
			return nil, errors.New("ambiguous judge output: more than one structured payload")
		}
		parsed = candidates[0]
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, errors.New("judge output must be an object")
	}
	return obj, nil
}

// ScoreJudgeOutput 先校验全覆盖，再应用可信权重与惩罚。
// Python: scoring.py:193 score_judge_output。返回 (score, normalized, requirements)。
func ScoreJudgeOutput(parsed map[string]any, behaviors, forbidden []map[string]any,
	penaltyMode string) (float64, map[string]any, map[string]any, error) {
	overallReason, err := requiredText(parsed, "overall_reason")
	if err != nil {
		return 0, nil, nil, err
	}
	if penaltyMode != "ceiling" && penaltyMode != "subtract" {
		return 0, nil, nil, errors.New("penalty_mode must be ceiling or subtract")
	}
	results, err := resultItems(parsed["behaviors"], behaviors, "behaviors")
	if err != nil {
		return 0, nil, nil, err
	}
	rawHits, present := parsed["forbidden_hits"]
	if !present {
		rawHits = []any{}
	}
	hits, err := resultItems(rawHits, forbidden, "forbidden_hits")
	if err != nil {
		return 0, nil, nil, err
	}
	for i, result := range results {
		score, err := FiniteNumber(result["score"], 0, 1, "score")
		if err != nil {
			return 0, nil, nil, err
		}
		result["score"] = score
		if _, err := requiredText(result, "reason"); err != nil {
			return 0, nil, nil, err
		}
		if _, err := requiredText(result, "evidence"); err != nil {
			return 0, nil, nil, err
		}
		result["description"] = behaviors[i]["description"]
		result["weight"] = behaviors[i]["weight"]
	}
	for i, hit := range hits {
		triggered, ok := hit["triggered"].(bool)
		if !ok {
			return 0, nil, nil, errors.New("forbidden triggered must be a boolean")
		}
		if _, err := requiredText(hit, "reason"); err != nil {
			return 0, nil, nil, err
		}
		if _, err := requiredText(hit, "evidence"); err != nil {
			return 0, nil, nil, err
		}
		hit["triggered"] = triggered
		hit["penalty"] = forbidden[i]["penalty"]
		hit["description"] = forbidden[i]["description"]
	}
	totalWeight := 0.0
	for _, item := range behaviors {
		totalWeight += item["weight"].(float64)
	}
	weighted := 0.0
	for i, result := range results {
		weighted += result["score"].(float64) * behaviors[i]["weight"].(float64)
	}
	score := weighted / totalWeight
	baseScore := score
	var penalties []float64
	for _, hit := range hits {
		if hit["triggered"].(bool) {
			penalties = append(penalties, hit["penalty"].(float64))
		}
	}
	if len(penalties) > 0 {
		if penaltyMode == "subtract" {
			// math.fsum 语义用补偿求和等价（Python L225）。
			score = math.Max(0.0, score-pyFsum(penalties))
		} else {
			// 既有调用方保留 legacy ceiling 而非累计扣减（Python L227-228）。
			maxPenalty := penalties[0]
			for _, p := range penalties {
				maxPenalty = math.Max(maxPenalty, p)
			}
			score = math.Min(score, 1.0-maxPenalty)
		}
	}
	dimensions := ComputeDimensions(results)
	triggeredIDs := []string{}
	for _, hit := range hits {
		if hit["triggered"].(bool) {
			triggeredIDs = append(triggeredIDs, hit["id"].(string))
		}
	}
	dimensions["triggered_forbidden_behaviors"] = triggeredIDs
	normalized := map[string]any{
		"overall_reason":  overallReason,
		"overall_score":   score,
		"base_score":      baseScore,
		"penalty_mode":    penaltyMode,
		"total_deduction": baseScore - score,
		"behaviors":       results,
		"forbidden_hits":  hits,
		"dimensions":      dimensions,
	}
	var requirements []map[string]any
	for _, item := range results {
		requirements = append(requirements, map[string]any{
			"requirement_id": item["id"],
			"group":          "requirement",
			"score":          item["score"],
			"passed":         item["score"].(float64) == 1.0,
			"evidence":       item["evidence"],
			"source":         "llm_as_judge.behaviors",
		})
	}
	for _, hit := range hits {
		requirements = append(requirements, map[string]any{
			"requirement_id": hit["id"],
			"group":          "forbidden",
			"score":          boolScore(hit["triggered"].(bool)),
			"passed":         !hit["triggered"].(bool),
			"evidence":       hit["evidence"],
			"source":         "llm_as_judge.forbidden_hits",
		})
	}
	return score, normalized, requirementResultsContractResolver(requirements), nil
}

// ComputeDimensions 保留历史 Analyzer 输入契约，不生成优化建议。
// Python: scoring.py:266 compute_dimensions。
func ComputeDimensions(results []map[string]any) map[string]any {
	scores := map[string]any{}
	var low []string
	diagnostics := map[string]any{}
	sum := 0.0
	for _, item := range results {
		id := item["id"].(string)
		score := item["score"].(float64)
		scores[id] = score
		sum += score
		reason := item["reason"].(string)
		failureReason := ""
		if score < 1.0 {
			low = append(low, id)
			failureReason = reason
		}
		diagnostics[id] = map[string]any{
			"reason":         reason,
			"failure_reason": failureReason,
			"evidence":       item["evidence"],
		}
	}
	return map[string]any{
		"per_behavior_scores":  scores,
		"low_score_behaviors":  low,
		"avg_behavior_score":   sum / float64(len(results)),
		"behavior_count":       len(results),
		"pass_count":           len(results) - len(low),
		"fail_count":           len(low),
		"behavior_diagnostics": diagnostics,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeItemsInput 把缺失/nil 的清单输入归一为空切片（Python .get(key, []) 语义）。
func normalizeItemsInput(raw any) any {
	if raw == nil {
		return []any{}
	}
	return raw
}

// normalizeItems 归一 behavior/forbidden 条目。
// Python: scoring.py:29 _normalize_items。
func normalizeItems(raw any, forbidden bool) ([]map[string]any, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, errors.New("behavior requirements must be a list")
	}
	var items []map[string]any
	for index, value := range list {
		var item map[string]any
		if s, isStr := value.(string); isStr {
			item = map[string]any{"id": s, "description": s}
		} else if m, isMap := value.(map[string]any); isMap {
			item = cloneMap(m)
		} else {
			return nil, errors.New("each behavior must be a string or object")
		}
		if _, present := item["id"]; !present {
			if desc, hasDesc := item["description"]; hasDesc {
				item["id"] = desc
			} else {
				item["id"] = fmt.Sprintf("behavior_%d", index+1)
			}
		}
		if _, present := item["description"]; !present {
			item["description"] = item["id"]
		}
		for _, key := range []string{"id", "description"} {
			fieldValue, isStr := item[key].(string)
			if !isStr || strings.TrimSpace(fieldValue) == "" {
				return nil, fmt.Errorf("behavior %s must be a non-empty string", key)
			}
		}
		if forbidden {
			penaltyRaw, present := item["penalty"]
			if !present {
				penaltyRaw = 0.3
			}
			penalty, err := FiniteNumber(penaltyRaw, 0, 1, "penalty")
			if err != nil {
				return nil, err
			}
			item["penalty"] = penalty
		} else {
			weightRaw, present := item["weight"]
			if !present {
				weightRaw = 1.0
			}
			weight, err := FiniteNumber(weightRaw, 0, 1e6, "weight")
			if err != nil {
				return nil, err
			}
			if weight == 0 {
				return nil, errors.New("behavior weight must be positive")
			}
			item["weight"] = weight
		}
		items = append(items, item)
	}
	if err := uniqueIDs(items); err != nil {
		return nil, err
	}
	return items, nil
}

// uniqueIDs 校验 ID 唯一。Python: scoring.py:55 _unique_ids。
func uniqueIDs(items []map[string]any) error {
	seen := map[string]bool{}
	for _, item := range items {
		id := item["id"].(string)
		if seen[id] {
			return errors.New("behavior IDs must be unique")
		}
		seen[id] = true
	}
	return nil
}

// referenceAnswer 取考题参考答案（向后兼容别名）。
// Python: judger/base.py:65 _reference_answer。
func referenceAnswer(caseMapping map[string]any) any {
	if reference, ok := caseMapping["reference"].(map[string]any); ok {
		if v, present := reference["answer"]; present {
			return v
		}
		if v, present := reference["expected_output"]; present {
			return v
		}
	}
	if v, present := caseMapping["reference_answer"]; present {
		return v
	}
	if v, present := caseMapping["expected_output"]; present {
		return v
	}
	return nil
}

// decodeJSONValue 严格递归解码：任何对象出现重复键即报错（对齐 Python
// object_pairs_hook=_unique_json_object；NaN/Infinity 字面量 Go 解码器天然拒绝，
// 对齐 parse_constant=_reject_json_constant）。
func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, isDelim := tok.(json.Delim); isDelim {
		switch delim {
		case '{':
			obj := map[string]any{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := keyTok.(string)
				if _, dup := obj[key]; dup {
					return nil, fmt.Errorf("duplicate judge JSON key: %s", key)
				}
				value, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				obj[key] = value
			}
			if _, err := dec.Token(); err != nil { // 消费 '}'
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				value, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, value)
			}
			if _, err := dec.Token(); err != nil { // 消费 ']'
				return nil, err
			}
			return arr, nil
		}
	}
	return tok, nil
}

// rawDecodeAt 从 text[start:] 起解码一个 JSON 值，返回值与消费结束偏移。
// 对齐 Python JSONDecoder.raw_decode。
func rawDecodeAt(text string, start int) (any, int, error) {
	dec := json.NewDecoder(strings.NewReader(text[start:]))
	value, err := decodeJSONValue(dec)
	if err != nil {
		return nil, 0, err
	}
	return value, start + int(dec.InputOffset()), nil
}

// containsJudgePayload 拒绝竞争判决，不拒数值向量或数据示例。
// Python: scoring.py:108 _contains_judge_payload。
func containsJudgePayload(text string) bool {
	end := 0
	for _, loc := range braceRe.FindAllStringIndex(text, -1) {
		if loc[0] < end {
			continue
		}
		value, endOffset, err := rawDecodeAt(text, loc[0])
		if err != nil {
			continue
		}
		end = endOffset
		items := []any{value}
		if arr, ok := value.([]any); ok {
			items = arr
		}
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"overall_reason", "behaviors", "forbidden_hits", "score"} {
				if _, present := obj[key]; present {
					return true
				}
			}
			if status, _ := obj["status"].(string); status == "unavailable" {
				return true
			}
		}
	}
	return false
}

// resultItems 校验每个 ID 恰好出现一次并按 expected 序输出。
// Python: scoring.py:174 _result_items。
func resultItems(raw any, expected []map[string]any, name string) ([]map[string]any, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list of objects", name)
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		obj, isMap := item.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("%s must be a list of objects", name)
		}
		id, isStr := obj["id"].(string)
		if !isStr {
			return nil, fmt.Errorf("%s IDs must be strings", name)
		}
		ids = append(ids, id)
	}
	expectedIDs := map[string]bool{}
	seen := map[string]bool{}
	for _, item := range expected {
		expectedIDs[item["id"].(string)] = true
	}
	for _, id := range ids {
		if seen[id] || !expectedIDs[id] {
			return nil, fmt.Errorf("%s must score every supplied ID exactly once, with no additional IDs", name)
		}
		seen[id] = true
	}
	if len(ids) != len(expected) {
		return nil, fmt.Errorf("%s must score every supplied ID exactly once, with no additional IDs", name)
	}
	byID := map[string]map[string]any{}
	for _, item := range list {
		byID[item.(map[string]any)["id"].(string)] = cloneMap(item.(map[string]any))
	}
	out := make([]map[string]any, 0, len(expected))
	for _, item := range expected {
		out = append(out, byID[item["id"].(string)])
	}
	return out, nil
}

// requiredText 取必填非空字符串字段。Python: scoring.py:186 _required_text。
func requiredText(item map[string]any, key string) (string, error) {
	value, isStr := item[key].(string)
	if !isStr || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("judge %s must be a non-empty string", key)
	}
	return value, nil
}

// pyFsum 补偿求和，对齐 Python math.fsum 精确语义（Neumaier 变体）。
func pyFsum(values []float64) float64 {
	total := 0.0
	compensation := 0.0
	for _, v := range values {
		adjusted := total + v
		if math.Abs(total) >= math.Abs(v) {
			compensation += (total - adjusted) + v
		} else {
			compensation += (v - adjusted) + total
		}
		total = adjusted
	}
	return total + compensation
}

// cloneMap 浅拷贝（Python dict(item) 语义）。
func cloneMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// boolScore 触发违禁计 0 分，未触发计 1 分（Python L256 条件表达式）。
func boolScore(triggered bool) float64 {
	if triggered {
		return 0.0
	}
	return 1.0
}

// errOrExtra 尾部多余数据时的错误兜底（Token 已值但流未到 EOF 视为 extra data）。
func errOrExtra(err error) error {
	if err == nil {
		return errors.New("trailing content")
	}
	return err
}
