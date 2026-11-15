package dataloader

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// itemRe 加权评分条目正则（对齐 _ITEM；支持 [weight N%]/[deduct N%] 与中文标签）
var itemRe = regexp.MustCompile(
	`^\s*(?:\d+[.)、]\s*)?\[\s*` +
		`(?P<kind>weight|deduct(?:ion)?|权重|扣(?:分)?)\s*[:：]?\s*` +
		`(?P<amount>\d+(?:\.\d+)?)\s*[%％]\s*\]\s*(?P<description>.+)$`)

// numberedRe 编号前缀正则（对齐 _NUMBERED）
var numberedRe = regexp.MustCompile(`^\s*\d+[.)、]\s*`)

// scoringAnnotationRe 评分标注前缀正则（对齐 _SCORING_ANNOTATION）
var scoringAnnotationRe = regexp.MustCompile(
	`^\s*\[\s*(?:weight|deduct(?:ion)?|权重|扣(?:分)?)`)

// headingRe 标题行正则（对齐 _HEADING）
var headingRe = regexp.MustCompile(`^\s*(?:【[^】]+】|#{1,6}\s)`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseWeightedRubric 解析编号百分比条目；歧义权重报错而非猜测
// （对齐 parse_weighted_rubric）
func ParseWeightedRubric(text string) ([]map[string]any, []map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil, fmt.Errorf("judge_rubrics must be non-empty percentage-annotated text")
	}
	positive := []map[string]any{}
	negative := []map[string]any{}
	var current map[string]any
	var currentList *[]map[string]any
	group := false
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := itemRe.FindStringSubmatch(line)
		if match != nil {
			group = numberedRe.MatchString(line) == false
			amount := toFloat(match[2]) / 100
			isWeight := strings.EqualFold(match[1], "weight") || match[1] == "权重"
			if math.IsInf(amount, 0) || math.IsNaN(amount) || amount <= 0 || amount > 1 {
				return nil, nil, fmt.Errorf("rubric percentages must be greater than 0 and at most 100")
			}
			if isWeight {
				currentList = &positive
			} else {
				currentList = &negative
			}
			prefix := "deduction"
			key := "penalty"
			if isWeight {
				prefix = "rubric"
				key = "weight"
			}
			current = map[string]any{
				"id":          fmt.Sprintf("%s_%03d", prefix, len(*currentList)+1),
				"description": strings.TrimSpace(match[3]),
				key:           amount,
			}
			*currentList = append(*currentList, current)
			continue
		}
		if numberedRe.MatchString(line) || strings.HasPrefix(strings.TrimLeft(line, " "), "[") {
			// 编号分组条目的多行描述续行（对齐 group 分支）
			stripped := numberedRe.ReplaceAllString(line, "")
			if current != nil && group && !scoringAnnotationRe.MatchString(stripped) {
				description, _ := current["description"].(string)
				current["description"] = description + "\n" + strings.TrimSpace(line)
				continue
			}
			return nil, nil, fmt.Errorf("unrecognized rubric item; use explicit [weight N%%] or [deduct N%%] annotations")
		}
		if headingRe.MatchString(line) {
			current = nil
		} else if current != nil {
			description, _ := current["description"].(string)
			current["description"] = description + "\n" + strings.TrimSpace(line)
		}
	}
	if len(positive) == 0 && len(negative) == 0 {
		return nil, nil, fmt.Errorf("judge_rubrics contains no supported percentage-annotated items")
	}
	// 正向权重之和必须为 100%（对齐 math.isclose(..., abs_tol=1e-9)）
	if len(positive) > 0 {
		sum := 0.0
		for _, item := range positive {
			sum += item["weight"].(float64)
		}
		if math.Abs(sum-1.0) > 1e-9 {
			return nil, nil, fmt.Errorf("positive rubric percentages must sum to 100; weights will not be silently rescaled")
		}
	}
	return positive, negative, nil
}

// NormalizeGradingCase 显式别名映射进既有 reference 契约，不修改原用例
// （对齐 normalize_grading_case）
func NormalizeGradingCase(caseMapping map[string]any) (map[string]any, error) {
	normalized := copyMap(caseMapping)
	if _, hasCaseID := normalized["case_id"]; !hasCaseID {
		if id, hasID := normalized["id"]; hasID {
			normalized["case_id"] = id
		}
	}
	rawReference, ok := caseMapping["reference"].(map[string]any)
	if caseMapping["reference"] != nil && !ok {
		return nil, fmt.Errorf("reference must be an object")
	}
	aliases := map[string]any{}
	for _, key := range []string{"reference_solution", "judge_rubrics"} {
		if value, exists := caseMapping[key]; exists {
			aliases[key] = value
		}
	}
	// reference.solution ↔ reference_solution 一致性
	if value, exists := rawReference["solution"]; exists {
		if existing, hasAlias := aliases["reference_solution"]; hasAlias && fmt.Sprintf("%v", existing) != fmt.Sprintf("%v", value) {
			return nil, fmt.Errorf("conflicting grading fields: reference_solution and reference.solution")
		}
		aliases["reference_solution"] = value
	}
	// reference.judge_rubrics ↔ judge_rubrics 一致性
	if value, exists := rawReference["judge_rubrics"]; exists {
		if existing, hasAlias := aliases["judge_rubrics"]; hasAlias && fmt.Sprintf("%v", existing) != fmt.Sprintf("%v", value) {
			return nil, fmt.Errorf("conflicting grading fields: judge_rubrics and reference.judge_rubrics")
		}
		aliases["judge_rubrics"] = value
	}
	if len(aliases) == 0 {
		return normalized, nil
	}
	reference := copyMap(rawReference)
	if value, exists := aliases["reference_solution"]; exists {
		if err := setConsistent(reference, "answer", value); err != nil {
			return nil, err
		}
	}
	if value, exists := aliases["judge_rubrics"]; exists {
		text, isStr := value.(string)
		if !isStr || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("judge_rubrics must be non-empty text")
		}
		annotated := false
		for _, line := range strings.Split(text, "\n") {
			if scoringAnnotationRe.MatchString(numberedRe.ReplaceAllString(line, "")) {
				annotated = true
				break
			}
		}
		var positive, negative []map[string]any
		var err error
		if annotated {
			// legacy 数据集保留显式百分比契约
			positive, negative, err = ParseWeightedRubric(text)
			if err != nil {
				return nil, err
			}
		} else {
			// 模型把整体文本作为 prose 规则；不推断、不拆分权重
			positive = []map[string]any{{"id": "rubric_overall", "description": text, "weight": 1.0}}
			negative = []map[string]any{}
		}
		if _, hasRubric := reference["rubric"]; hasRubric {
			return nil, fmt.Errorf("judge_rubrics cannot be combined with reference.rubric")
		}
		if err := setConsistent(reference, "required_behaviors", toAnySlice(positive)); err != nil {
			return nil, err
		}
		if err := setConsistent(reference, "forbidden_behaviors", toAnySlice(negative)); err != nil {
			return nil, err
		}
		if err := setConsistent(reference, "penalty_mode", "subtract"); err != nil {
			return nil, err
		}
		answerRole := "criterion"
		if len(positive) > 0 {
			answerRole = "reference"
		}
		if err := setConsistent(reference, "answer_role", answerRole); err != nil {
			return nil, err
		}
	}
	normalized["reference"] = reference
	return normalized, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setConsistent 一致性字段写入（对齐 _set_consistent）
func setConsistent(target map[string]any, field string, value any) error {
	if existing, exists := target[field]; exists && fmt.Sprintf("%v", existing) != fmt.Sprintf("%v", value) {
		return fmt.Errorf("conflicting grading fields for reference.%s", field)
	}
	target[field] = value
	return nil
}

// toFloat 字符串转浮点
func toFloat(raw string) float64 {
	value, _ := strconv.ParseFloat(raw, 64)
	return value
}

// toAnySlice map 切片转 any 切片
func toAnySlice(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// copyMap 浅拷贝映射
func copyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
