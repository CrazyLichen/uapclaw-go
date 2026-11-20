package evaluationresultanalyzer

import (
	"fmt"
	"testing"
)

// TestBuildTaskClauses_条款切分 对照 Python build_task_clauses。
// Python: task_contract.py:11-29
func TestBuildTaskClauses_条款切分(t *testing.T) {
	// ① 多段落切分：行号区间、哈希、条款 ID 格式
	task := "para one line1\npara one line2\n\npara two\n"
	clauses := BuildTaskClauses(task)
	if len(clauses) != 2 {
		t.Fatalf("应切出 2 条条款, got %d: %v", len(clauses), clauses)
	}
	c0 := clauses[0]
	if c0["line_start"] != 1 || c0["line_end"] != 2 {
		t.Fatalf("条款0 行号区间应为 1-2, got %v-%v", c0["line_start"], c0["line_end"])
	}
	if c0["text"] != "para one line1\npara one line2" {
		t.Fatalf("条款0 文本应保留原文并去尾行界, got %q", c0["text"])
	}
	c1 := clauses[1]
	if c1["line_start"] != 4 || c1["line_end"] != 4 {
		t.Fatalf("条款1 行号区间应为 4-4, got %v-%v", c1["line_start"], c1["line_end"])
	}
	// clause_id 格式 clause_<start:04d>_<sha256[:10]>
	id := c0["clause_id"].(string)
	if len(id) < len("clause_0001_") || id[:len("clause_0001_")] != "clause_0001_" {
		t.Fatalf("clause_id 前缀不符: %s", id)
	}
	digest := id[len("clause_0001_"):]
	if len(digest) != 10 {
		t.Fatalf("哈希应为 10 位 hex, got %q", digest)
	}
	for _, ch := range digest {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Fatalf("哈希应为小写 hex, got %q", digest)
		}
	}

	// ② 同文本同哈希（确定性）
	again := BuildTaskClauses(task)
	if again[0]["clause_id"] != id {
		t.Fatal("同输入的 clause_id 应稳定")
	}

	// ③ 空段/纯空白段跳过
	blank := BuildTaskClauses("a\n   \nb\n")
	if len(blank) != 2 {
		t.Fatalf("纯空白段应跳过, got %d", len(blank))
	}

	// ④ 空任务 → 无条款
	if got := BuildTaskClauses(""); len(got) != 0 {
		t.Fatalf("空任务应无条款, got %v", got)
	}

	// ⑤ CRLF 行界：原文保留 \r\n，切分不受影响
	crlf := BuildTaskClauses("one\r\n\r\ntwo\r\n")
	if len(crlf) != 2 || crlf[0]["text"] != "one" {
		t.Fatalf("CRLF 应正常切分, got %v", crlf)
	}

	// ⑥ 末尾无换行：连续非空行属于同一条款（text 跨行保留）
	noeol := BuildTaskClauses("x\ny")
	if len(noeol) != 1 || noeol[0]["text"] != "x\ny" || noeol[0]["line_start"] != 1 || noeol[0]["line_end"] != 2 {
		t.Fatalf("末尾无换行的连续块应为单条款, got %v", noeol)
	}
}

// TestNormalizeContractChecks_校验分支 对照 Python normalize_contract_checks。
// Python: task_contract.py:32-77
func TestNormalizeContractChecks_校验分支(t *testing.T) {
	task := "keep tests green\n\nship docs\n"
	clauses := BuildTaskClauses(task)
	goodID := clauses[0]["clause_id"].(string)

	// ① 缺键 → 空清单
	if got := NormalizeContractChecks(map[string]any{}, task); len(got) != 0 {
		t.Fatalf("缺键应返回空, got %v", got)
	}

	// ② 非列表 → 模式错误
	if got := NormalizeContractChecks(map[string]any{"task_contract_checks": "oops"}, task); len(got) != 1 ||
		got[0] != "[contract_schema] task_contract_checks must be a list" {
		t.Fatalf("非列表错误文案不符: %v", got)
	}

	// ③ 合法条款 ID：回填 quote/行号，无错误
	check := map[string]any{
		"clause_id":                         goodID,
		"interpretation":                    "必须保持测试通过",
		"recommendation_preserves_contract": true,
	}
	diagnosis := map[string]any{"task_contract_checks": []any{check}}
	if got := NormalizeContractChecks(diagnosis, task); len(got) != 0 {
		t.Fatalf("合法检查不应有错误, got %v", got)
	}
	if check["task_quote"] != "keep tests green" || check["line_start"] != 1 || check["line_end"] != 1 {
		t.Fatalf("条款回填不符: %v", check)
	}
	if check["clause_id"] != goodID {
		t.Fatal("clause_id 应保持")
	}

	// ④ 未知 clause_id → citation 错误含可用 ID 前 30
	bad := map[string]any{
		"clause_id":                         "clause_9999_ffffffff",
		"interpretation":                    "x",
		"recommendation_preserves_contract": true,
	}
	got := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{bad}}, task)
	if len(got) != 1 || !contains(got[0], "[contract_citation]") || !contains(got[0], "Available IDs (first 30)") {
		t.Fatalf("未知 ID 错误文案不符: %v", got)
	}
	if !contains(got[0], goodID) {
		t.Fatal("错误文案应列可用 ID")
	}

	// ⑤ 无 clause_id 但 task_quote 唯一精确匹配 → 定位条款并回填
	quoteCheck := map[string]any{
		"task_quote":                        "ship docs",
		"interpretation":                    "x",
		"recommendation_preserves_contract": true,
	}
	got2 := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{quoteCheck}}, task)
	if len(got2) != 0 {
		t.Fatalf("唯一 quote 匹配应成功, got %v", got2)
	}
	if quoteCheck["clause_id"] != clauses[1]["clause_id"] {
		t.Fatalf("应回填匹配条款 ID, got %v", quoteCheck["clause_id"])
	}

	// ⑥ quote 非唯一 → citation 错误
	dupTask := "same\n\nsame\n"
	dupCheck := map[string]any{
		"task_quote":                        "same",
		"interpretation":                    "x",
		"recommendation_preserves_contract": true,
	}
	got3 := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{dupCheck}}, dupTask)
	if len(got3) != 1 || !contains(got3[0], "[contract_citation]") {
		t.Fatalf("非唯一 quote 应报 citation 错误: %v", got3)
	}

	// ⑦ interpretation 缺失/空白 → schema 错误
	noInterp := map[string]any{
		"clause_id":                         goodID,
		"interpretation":                    "   ",
		"recommendation_preserves_contract": true,
	}
	got4 := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{noInterp}}, task)
	if len(got4) != 1 || !contains(got4[0], ".interpretation must state the relevant guarantee") {
		t.Fatalf("interpretation 错误文案不符: %v", got4)
	}

	// ⑧ preserves 缺失 → required 错误；false → conflict 错误
	noPreserve := map[string]any{"clause_id": goodID, "interpretation": "x"}
	got5 := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{noPreserve}}, task)
	if len(got5) != 1 || !contains(got5[0], "is required (boolean)") {
		t.Fatalf("preserves 缺失错误不符: %v", got5)
	}
	falsePreserve := map[string]any{
		"clause_id":                         goodID,
		"interpretation":                    "x",
		"recommendation_preserves_contract": false,
	}
	got6 := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{falsePreserve}}, task)
	if len(got6) != 1 || !contains(got6[0], "[contract_conflict]") {
		t.Fatalf("false preserves 应报 conflict: %v", got6)
	}

	// ⑧ 条目非对象 → schema 错误
	got7 := NormalizeContractChecks(map[string]any{"task_contract_checks": []any{"str"}}, task)
	if len(got7) != 1 || !contains(got7[0], "task_contract_checks[0] must be an object") {
		t.Fatalf("非对象条目错误不符: %v", got7)
	}

	// ⑩ 多错误累积（顺序保持索引序）
	multi := []any{
		map[string]any{"clause_id": goodID, "interpretation": "x", "recommendation_preserves_contract": false},
		"not-a-dict",
	}
	got8 := NormalizeContractChecks(map[string]any{"task_contract_checks": multi}, task)
	if len(got8) != 2 || !contains(got8[0], "[contract_conflict]") || !contains(got8[1], "task_contract_checks[1]") {
		t.Fatalf("多错误累积顺序不符: %v", got8)
	}
}

// contains 包含判断（测试辅助）。
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOfStr(s, sub) >= 0)
}

// indexOfStr 子串查找（测试辅助）。
func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// 编译期引用 fmt（保留占位断言格式化能力）。
var _ = fmt.Sprintf
