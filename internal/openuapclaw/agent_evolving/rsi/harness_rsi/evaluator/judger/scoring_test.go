package judger

import (
	"math"
	"strings"
	"testing"
)

// init 注册测试用契约实现（生产由 evaluator 包 init 注册真身）。
func init() {
	RegisterRequirementResultsContract(func(items []map[string]any) map[string]any {
		return map[string]any{"schema_version": 1, "items": items}
	})
}

// TestFiniteNumber_类型与范围拒绝 对齐 Python finite_number：bool 拒、非数值拒、
// 非有限值与越界拒（bool-as-int 类型层不可能，登记已知偏差）。
func TestFiniteNumber_类型与范围拒绝(t *testing.T) {
	if _, err := FiniteNumber(true, 0, 1, "score"); err == nil || !strings.Contains(err.Error(), "score must be a number") {
		t.Fatalf("bool 应拒: %v", err)
	}
	if _, err := FiniteNumber("0.5", 0, 1, "score"); err == nil || !strings.Contains(err.Error(), "score must be a number") {
		t.Fatalf("字符串应拒: %v", err)
	}
	if _, err := FiniteNumber(math.NaN(), 0, 1, "score"); err == nil || !strings.Contains(err.Error(), "must be finite and between") {
		t.Fatalf("NaN 应拒: %v", err)
	}
	if v, err := FiniteNumber(1, 0, 1, "score"); err != nil || v != 1 {
		t.Fatalf("合法 int 应通过: v=%v err=%v", v, err)
	}
	if _, err := FiniteNumber(nil, 0, 1, "score"); err == nil || !strings.Contains(err.Error(), "score must be a number") {
		t.Fatalf("缺失值(None)应报 must be a number: %v", err)
	}
}

// TestScoringContract_rubric编号与referenceAnswer首位 对齐 Python scoring_contract。
func TestScoringContract_rubric编号与referenceAnswer首位(t *testing.T) {
	caseMapping := map[string]any{
		"case_id": "c1",
		"request": "题目",
		"reference": map[string]any{
			"answer": "参考答案",
			"required_behaviors": []any{
				map[string]any{"id": "b1", "description": "行为一", "weight": 2.0},
			},
			"rubric":              []any{"话术包含核实步骤"},
			"forbidden_behaviors": []any{"不得承诺退款"},
		},
	}
	behaviors, forbidden, err := ScoringContract(caseMapping)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if behaviors[0]["id"] != "reference_answer" {
		t.Fatalf("reference_answer 应首位插入, got %v", behaviors[0]["id"])
	}
	if behaviors[1]["id"] != "b1" || behaviors[1]["weight"] != 2.0 {
		t.Fatalf("required_behaviors 应保留 weight, got %v", behaviors[1])
	}
	if behaviors[2]["id"] != "rubric_001" || behaviors[2]["weight"] != 1.0 {
		t.Fatalf("rubric 应编号 rubric_001 权重1, got %v", behaviors[2])
	}
	if forbidden[0]["penalty"] != 0.3 {
		t.Fatalf("forbidden 默认 penalty 应为 0.3, got %v", forbidden[0]["penalty"])
	}
}

// TestScoringContract_错误文案 对齐 Python 各拒绝路径文案。
func TestScoringContract_错误文案(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want string
	}{
		{"空清单", map[string]any{"reference": map[string]any{}},
			"llm_as_judge requires reference.answer, reference.rubric or reference.required_behaviors"},
		{"answer_role非法", map[string]any{"reference": map[string]any{"answer": "x", "answer_role": "bad"}},
			"reference.answer_role must be criterion or reference"},
		{"penalty_mode非法", map[string]any{"reference": map[string]any{"answer": "x", "penalty_mode": "bad"}},
			"reference.penalty_mode must be ceiling or subtract"},
		{"weight为零", map[string]any{"reference": map[string]any{
			"required_behaviors": []any{map[string]any{"id": "b", "weight": 0}}}},
			"behavior weight must be positive"},
		{"ID重复", map[string]any{"reference": map[string]any{
			"required_behaviors": []any{map[string]any{"id": "b"}, map[string]any{"id": "b"}}}},
			"behavior IDs must be unique"},
	}
	for _, tc := range cases {
		_, _, err := ScoringContract(tc.in)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%s: want %q got %v", tc.name, tc.want, err)
		}
	}
}

// TestParseJudgeOutput_围栏与候选扫描 对齐 Python parse_judge_output。
func TestParseJudgeOutput_围栏与候选扫描(t *testing.T) {
	verdict := `{"overall_reason": "r", "behaviors": [{"id": "b1", "score": 1, "reason": "ok", "evidence": "e"}]}`
	if got, err := ParseJudgeOutput(verdict); err != nil || got["overall_reason"] != "r" {
		t.Fatalf("直接 JSON 应解析: %v %v", got, err)
	}
	fenced := "说明文字\n```json\n" + verdict + "\n```\n结尾"
	if got, err := ParseJudgeOutput(fenced); err != nil || got["overall_reason"] != "r" {
		t.Fatalf("围栏 JSON 应解析: %v %v", got, err)
	}
	if _, err := ParseJudgeOutput("没有任何 JSON"); err == nil || err != ErrMissingJudgeVerdict {
		t.Fatalf("无判决应报 MissingJudgeVerdict, got %v", err)
	}
	if _, err := ParseJudgeOutput("```json\n不完整"); err == nil ||
		!strings.Contains(err.Error(), "judge output must contain one complete JSON fence") {
		t.Fatalf("围栏不完整应报错, got %v", err)
	}
	// 以 { 开头且尾部多余数据：Python decoder.decode 直接报 Extra data（非 ambiguous）。
	two := verdict + " 和另一个 " + verdict
	if _, err := ParseJudgeOutput(two); err == nil ||
		!strings.Contains(err.Error(), "extra data after judge JSON") {
		t.Fatalf("开头即 JSON 且尾部多余应报 extra data, got %v", err)
	}
	// 散文包裹两个判决 → ambiguous（候选扫描发现两个 payload）。
	proseTwo := "结论一 " + verdict + " 结论二 " + verdict
	if _, err := ParseJudgeOutput(proseTwo); err == nil ||
		!strings.Contains(err.Error(), "ambiguous judge output: more than one structured payload") {
		t.Fatalf("散文包裹两个判决应拒, got %v", err)
	}
	if _, err := ParseJudgeOutput("{\"score\": 1, \"score\": 2}"); err == nil ||
		!strings.Contains(err.Error(), "duplicate judge JSON key: score") {
		t.Fatalf("重复键应拒, got %v", err)
	}
}

// TestScoreJudgeOutput_ceiling与subtract 对齐 Python score_judge_output 罚分两档。
func TestScoreJudgeOutput_ceiling与subtract(t *testing.T) {
	behaviors := []map[string]any{
		{"id": "b1", "description": "d1", "weight": 2.0},
		{"id": "b2", "description": "d2", "weight": 1.0},
		{"id": "b3", "description": "d3", "weight": 1.0},
	}
	forbidden := []map[string]any{{"id": "f1", "description": "fd", "penalty": 0.5}}
	verdict := map[string]any{
		"overall_reason": "r",
		"behaviors": []any{
			map[string]any{"id": "b1", "score": 1, "reason": "ok", "evidence": "e1"},
			map[string]any{"id": "b2", "score": 0, "reason": "no", "evidence": "e2"},
			map[string]any{"id": "b3", "score": 1, "reason": "ok", "evidence": "e3"},
		},
		"forbidden_hits": []any{
			map[string]any{"id": "f1", "triggered": true, "reason": "bad", "evidence": "ef"},
		},
	}
	// base = (1*2+0*1+1*1)/4 = 0.75；ceiling = min(0.75, 1-0.5) = 0.5
	score, normalized, _, err := ScoreJudgeOutput(verdict, behaviors, forbidden, "ceiling")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if score != 0.5 || normalized["base_score"] != 0.75 || normalized["total_deduction"] != 0.25 {
		t.Fatalf("ceiling 分数不符: score=%v base=%v ded=%v", score, normalized["base_score"], normalized["total_deduction"])
	}
	// subtract: max(0, 0.75-0.5) = 0.25
	subScore, _, _, err := ScoreJudgeOutput(verdict, behaviors, forbidden, "subtract")
	if err != nil || subScore != 0.25 {
		t.Fatalf("subtract 分数不符: %v %v", subScore, err)
	}
	dims := normalized["dimensions"].(map[string]any)
	if dims["behavior_count"] != 3 || dims["fail_count"] != 1 {
		t.Fatalf("dimensions 不符: %v", dims)
	}
}

// TestScoreJudgeOutput_覆盖校验 对齐 Python _result_items 漏评/多评拒绝。
func TestScoreJudgeOutput_覆盖校验(t *testing.T) {
	behaviors := []map[string]any{{"id": "b1", "description": "d", "weight": 1.0}}
	verdict := map[string]any{
		"overall_reason": "r",
		"behaviors":      []any{},
	}
	if _, _, _, err := ScoreJudgeOutput(verdict, behaviors, nil, "ceiling"); err == nil ||
		!strings.Contains(err.Error(), "behaviors must score every supplied ID exactly once, with no additional IDs") {
		t.Fatalf("漏评应拒, got %v", err)
	}
	if _, _, _, err := ScoreJudgeOutput(map[string]any{"overall_reason": ""}, behaviors, nil, "ceiling"); err == nil ||
		!strings.Contains(err.Error(), "judge overall_reason must be a non-empty string") {
		t.Fatalf("overall_reason 空应拒, got %v", err)
	}
	if _, _, _, err := ScoreJudgeOutput(map[string]any{"overall_reason": "r"}, behaviors, nil, "bad"); err == nil ||
		!strings.Contains(err.Error(), "penalty_mode must be ceiling or subtract") {
		t.Fatalf("penalty_mode 非法应拒, got %v", err)
	}
}
