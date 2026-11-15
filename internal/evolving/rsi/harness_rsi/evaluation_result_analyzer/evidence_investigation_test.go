package evaluationresultanalyzer

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEvidenceInvestigation_常量面板 常量逐值断言（Python 常量面板）
func TestEvidenceInvestigation_常量面板(t *testing.T) {
	cases := map[string]int{
		"maxHypotheses":                    6,
		"maxRequests":                      12,
		"maxSearchResults":                 5,
		"maxEventChars":                    12000,
		"maxArtifactFileChars":             200000,
		"maxArtifactFiles":                 100,
		"maxArtifactWindowChars":           12000,
		"maxAutomaticArtifactWindows":      12,
		"maxAutomaticArtifactSources":      4,
		"maxAutomaticArtifactChars":        96000,
		"maxStructuredArtifactsPerRequest": 16,
		"maxRepositoryFiles":               2000,
		"maxStructuredCells":               20000,
		"maxStructuredPages":               100,
	}
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"maxHypotheses", maxHypotheses, 6},
		{"maxRequests", maxRequests, 12},
		{"maxSearchResults", maxSearchResults, 5},
		{"maxEventChars", maxEventChars, 12000},
		{"maxArtifactFileChars", maxArtifactFileChars, 200000},
		{"maxArtifactFiles", maxArtifactFiles, 100},
		{"maxArtifactWindowChars", maxArtifactWindowChars, 12000},
		{"maxAutomaticArtifactWindows", maxAutomaticArtifactWindows, 12},
		{"maxAutomaticArtifactSources", maxAutomaticArtifactSources, 4},
		{"maxAutomaticArtifactChars", maxAutomaticArtifactChars, 96000},
		{"maxStructuredArtifactsPerRequest", maxStructuredArtifactsPerRequest, 16},
		{"maxRepositoryFiles", maxRepositoryFiles, 2000},
		{"maxStructuredCells", maxStructuredCells, 20000},
		{"maxStructuredPages", maxStructuredPages, 100},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Fatalf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	_ = cases
	// 后缀集
	for _, s := range []string{".csv", ".diff", ".json", ".log", ".md", ".patch", ".txt", ".xml", ".yaml", ".yml"} {
		if !textArtifactSuffixes[s] {
			t.Fatalf("文本后缀缺 %s", s)
		}
	}
	for _, s := range []string{".docx", ".pdf", ".pptx", ".xlsx"} {
		if !structuredArtifactSuffixes[s] {
			t.Fatalf("结构化后缀缺 %s", s)
		}
	}
}

// TestNormalizeSemantics 归一化语义（Python _normalized_semantics）
func TestNormalizeSemantics(t *testing.T) {
	if got := normalizedSemantics("Hello World_123！"); got != "hello world_123" {
		t.Fatalf("got %q", got)
	}
	// Python 实测（ground truth）：中文按单字分词
	if got := normalizedSemantics("缺少 证据"); got != "缺 少 证 据" {
		t.Fatalf("中文逐字: got %q", got)
	}
}

// TestCausalHypothesisSemanticId 确定性指纹
func TestCausalHypothesisSemanticId(t *testing.T) {
	a := CausalHypothesisSemanticId("Claim A", "Falsified IF B")
	b := CausalHypothesisSemanticId("claim  a", "falsified if b")
	if a != b {
		t.Fatalf("语义等价应同指纹: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "chs:") || len(a) != len("chs:")+24 {
		t.Fatalf("指纹格式不符: %q", a)
	}
}

// TestNormalizeCausalInvestigation 归一化主链（Python normalize_causal_investigation）
func TestNormalizeCausalInvestigation(t *testing.T) {
	// 非 mapping / 缺 hypotheses → nil
	if NormalizeCausalInvestigation(nil) != nil {
		t.Fatal("nil 输入应返 nil")
	}
	if NormalizeCausalInvestigation(map[string]any{}) != nil {
		t.Fatal("缺 hypotheses 应返 nil")
	}
	// 正常：假设 + 请求
	plan := map[string]any{
		"hypotheses": []any{
			map[string]any{
				"claim":        "The tool ignores timeout",
				"falsified_if": "timeout log line exists",
				"evidence_requests": []any{
					map[string]any{"operation": "search_trace", "query": "timeout"},
				},
			},
		},
	}
	result := NormalizeCausalInvestigation(plan)
	if result == nil {
		t.Fatal("正常计划不应返 nil")
	}
	// ready_without_more_evidence：有请求时恒 false
	if result["ready_without_more_evidence"].(bool) {
		t.Fatal("有请求时应为 false")
	}
	// claim 或 falsified_if 缺失跳过；全跳过后 < min_hypotheses → nil
	bad := map[string]any{
		"hypotheses": []any{
			map[string]any{"claim": "only claim"},
		},
	}
	if NormalizeCausalInvestigation(bad) != nil {
		t.Fatal("缺 falsified_if 应返 nil")
	}
	// 语义去重
	dup := map[string]any{
		"hypotheses": []any{
			map[string]any{"claim": "Same Claim", "falsified_if": "Same Falsifier"},
			map[string]any{"claim": "same claim", "falsified_if": "same falsifier"},
		},
	}
	res2 := NormalizeCausalInvestigation(dup)
	if len(res2["hypotheses"].([]map[string]any)) != 1 {
		t.Fatal("语义重复假设未去重")
	}
	// numeric_change 强制检测：双正则命中不可关闭
	forced := map[string]any{
		"hypotheses": []any{
			map[string]any{
				"claim":                         "score decreased by 5%",
				"falsified_if":                  "no change",
				"numeric_change_check_required": false,
			},
		},
	}
	res3 := NormalizeCausalInvestigation(forced)
	h0 := res3["hypotheses"].([]map[string]any)[0]
	if h0["numeric_change_check_required"].(bool) != true {
		t.Fatal("percent 语义应强制 numeric check（显式 false 不可关闭）")
	}
	// hypothesis_id 冲突回退 h{n}
	ids := map[string]any{
		"hypotheses": []any{
			map[string]any{"claim": "A one", "falsified_if": "B", "hypothesis_id": "h1"},
			map[string]any{"claim": "C two", "falsified_if": "D", "hypothesis_id": "h1"},
		},
	}
	res4 := NormalizeCausalInvestigation(ids)
	hs := res4["hypotheses"].([]map[string]any)
	if hs[0]["hypothesis_id"] != "h1" || hs[1]["hypothesis_id"] != "h2" {
		t.Fatalf("id 冲突未回退: %q %q", hs[0]["hypothesis_id"], hs[1]["hypothesis_id"])
	}
}

// TestNormalizeCausalInvestigation_请求去重与上限 请求指纹去重/上限裁剪/id 冲突
func TestNormalizeCausalInvestigation_请求去重与上限(t *testing.T) {
	mkReq := func(id string) map[string]any {
		return map[string]any{"operation": "search_trace", "query": "same", "request_id": id}
	}
	plan := map[string]any{
		"hypotheses": []any{
			map[string]any{
				"claim": "A", "falsified_if": "B",
				"evidence_requests": []any{mkReq("qa"), mkReq("qb")},
			},
		},
	}
	res := NormalizeCausalInvestigation(plan)
	reqs := res["evidence_requests"].([]map[string]any)
	if len(reqs) != 1 {
		t.Fatalf("指纹相同应去重: %d", len(reqs))
	}
	if reqs[0]["hypothesis_ids"] == nil {
		t.Fatal("去重时应合并 hypothesis_ids")
	}
}

// TestSafeRelativePath 相对路径校验（Python _safe_relative_path）
func TestSafeRelativePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a/b/c.txt", "a/b/c.txt"},
		{"a\\b.txt", "a/b.txt"},
		{"/abs/path", ""},
		{"C:\\x", ""},
		{"a/../b", ""},
		{"./a/./b", "a/b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := safeRelativePath(c.in); got != c.want {
			t.Fatalf("safeRelativePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPositiveIntHelpers 整数助手
func TestPositiveIntHelpers(t *testing.T) {
	if positiveInt("3", 5) != 3 || positiveInt("0", 5) != 5 || positiveInt("x", 5) != 5 {
		t.Fatal("positiveInt 边界不符")
	}
	if v := optionalNonnegativeInt("4"); v == nil || *v != 4 {
		t.Fatal("optionalNonnegativeInt(4) 应为 4")
	}
	if optionalNonnegativeInt("-1") != nil {
		t.Fatal("负数应为 nil")
	}
	if optionalNonnegativeInt("abc") != nil {
		t.Fatal("非数字应为 nil")
	}
}

// TestRequestExecutionFingerprint 指纹忽略本地标签（Python _request_execution_fingerprint）
func TestRequestExecutionFingerprint(t *testing.T) {
	a := map[string]any{"request_id": "q1", "hypothesis_ids": []string{"h1"}, "purpose": "p", "operation": "search_trace", "query": "x"}
	b := map[string]any{"request_id": "q9", "hypothesis_ids": []string{"h2"}, "purpose": "other", "operation": "search_trace", "query": "x"}
	if requestExecutionFingerprint(a) != requestExecutionFingerprint(b) {
		t.Fatal("本地标签不同、执行语义相同应同指纹")
	}
	// JSON 规范化可解析
	var v map[string]any
	if json.Unmarshal([]byte(requestExecutionFingerprint(a)), &v) != nil {
		t.Fatal("指纹应为规范 JSON")
	}
}
