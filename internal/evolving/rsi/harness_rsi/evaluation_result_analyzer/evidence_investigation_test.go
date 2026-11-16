package evaluationresultanalyzer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/xuri/excelize/v2"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

// TestSafeNumericExpression 白名单表达式求值（Python _safe_numeric_expression）
func TestSafeNumericExpression(t *testing.T) {
	if v, err := safeNumericExpression("1 + 2 * 3"); err != nil || v != 7 {
		t.Fatalf("1+2*3 = %v err=%v", v, err)
	}
	if v, err := safeNumericExpression("(1.5 - 0.5) / 2"); err != nil || v != 0.5 {
		t.Fatalf("(1.5-0.5)/2 = %v err=%v", v, err)
	}
	if v, err := safeNumericExpression("-3"); err != nil || v != -3 {
		t.Fatalf("-3 = %v err=%v", v, err)
	}
	// 长度超限
	if _, err := safeNumericExpression(strings.Repeat("1+", 150) + "1"); err == nil || err.Error() != "expression_length" {
		t.Fatalf("超长应 expression_length: %v", err)
	}
	// 复杂度超限
	if _, err := safeNumericExpression(strings.Repeat("+1", 45)[1:]); err == nil {
		t.Fatal("超复杂度应报错")
	}
	// 非白名单节点
	if _, err := safeNumericExpression("__import__('os')"); err == nil {
		t.Fatal("函数调用应拒绝")
	}
	if _, err := safeNumericExpression("1/0"); err == nil {
		t.Fatal("除零应拒绝")
	}
}

// TestCheckRelation 关系判定（Python _check_relation）
func TestCheckRelation(t *testing.T) {
	// expected 缺失
	r := checkRelation(map[string]any{"expression": "1"})
	if r["availability"] != "invalid" || r["reason"] != "expected_must_be_a_finite_number" {
		t.Fatalf("缺 expected: %v", r)
	}
	// 表达式非法
	r = checkRelation(map[string]any{"expression": "bad((", "expected": 1.0})
	if r["availability"] != "invalid" || !strings.HasPrefix(r["reason"].(string), "invalid_numeric_expression") {
		t.Fatalf("非法表达式: %v", r)
	}
	// approximately_equal
	r = checkRelation(map[string]any{"expression": "1.0", "expected": 1.0, "tolerance": 0.1})
	if r["availability"] != "available" || r["holds"] != true {
		t.Fatalf("近似相等: %v", r)
	}
	// less_than
	r = checkRelation(map[string]any{"expression": "1", "expected": 2.0, "operator": "less_than"})
	if r["holds"] != true {
		t.Fatalf("less_than: %v", r)
	}
	// 不支持的操作符
	r = checkRelation(map[string]any{"expression": "1", "expected": 2.0, "operator": "wat"})
	if r["reason"] != "unsupported_relation_operator" {
		t.Fatalf("操作符: %v", r)
	}
}

// TestCompareNumericChange 数值变化（Python _compare_numeric_change）
func TestCompareNumericChange(t *testing.T) {
	r := compareNumericChange(map[string]any{"before_expression": "10", "after_expression": "12", "expected_delta": 2.0})
	if r["availability"] != "available" || r["holds"] != true || r["computed_delta"] != 2.0 {
		t.Fatalf("delta=2: %v", r)
	}
	r = compareNumericChange(map[string]any{"before_expression": "10", "after_expression": "12", "expected_delta": 3.0})
	if r["holds"] != false {
		t.Fatalf("delta!=3 应不成立: %v", r)
	}
	r = compareNumericChange(map[string]any{"before_expression": "10", "after_expression": "12"})
	if r["availability"] != "invalid" || r["reason"] != "expected_delta_must_be_a_finite_number" {
		t.Fatalf("缺 expected_delta: %v", r)
	}
}

// TestTraceEvents 事件展平（Python _trace_events）
func TestTraceEvents(t *testing.T) {
	traceData := map[string]any{
		"traces": []any{
			map[string]any{
				"trace_id": "t1",
				"messages": []any{
					map[string]any{"role": "user", "content": "hello", "message_index": 0},
					map[string]any{"role": "assistant", "content": "world", "message_index": 1},
				},
			},
		},
	}
	events := traceEvents(traceData)
	if len(events) != 2 || events[0]["trace_id"] != "t1" || events[0]["message_index"] != 0 {
		t.Fatalf("事件展平不符: %v", events)
	}
	// 非法 message_index 回退 sequence
	traceData["traces"] = []any{map[string]any{"trace_id": "t2", "messages": []any{map[string]any{"role": "u", "content": "x", "message_index": "bad"}}}}
	events = traceEvents(traceData)
	if events[0]["message_index"] != 0 {
		t.Fatalf("非法 index 应回退 sequence: %v", events[0]["message_index"])
	}
}

// TestSearchTrace 搜索排序（Python _search_trace）
func TestSearchTrace(t *testing.T) {
	events := []map[string]any{
		{"trace_id": "t1", "message_index": 0, "role": "user", "content": "timeout error happened", "step_pointer": "", "tool_calls": nil},
		{"trace_id": "t1", "message_index": 1, "role": "assistant", "content": "all good", "step_pointer": "", "tool_calls": nil},
	}
	r := searchTrace(events, map[string]any{"query": "timeout", "max_results": 1})
	if r["availability"] != "available" || r["matched_event_count"] != 1 {
		t.Fatalf("搜索结果: %v", r)
	}
	r = searchTrace(events, map[string]any{"query": "nonexistent"})
	if r["availability"] != "not_found" {
		t.Fatalf("无命中: %v", r)
	}
}

// TestReadEvent 读事件与歧义（Python _read_event）
func TestReadEvent(t *testing.T) {
	events := []map[string]any{
		{"trace_id": "t1", "message_index": 0, "role": "user", "content": "hello", "step_pointer": "", "tool_calls": nil},
		{"trace_id": "t2", "message_index": 0, "role": "user", "content": "hello2", "step_pointer": "", "tool_calls": nil},
	}
	// 歧义：无 trace_id 多命中
	r := readEvent(events, map[string]any{"message_index": 0})
	if r["availability"] != "ambiguous" || r["reason"] != "trace_id_required_for_non_unique_message_index" {
		t.Fatalf("歧义: %v", r)
	}
	// 带 trace_id 命中
	r = readEvent(events, map[string]any{"message_index": 0, "trace_id": "t2"})
	if r["availability"] != "available" {
		t.Fatalf("命中: %v", r)
	}
	// 未命中
	r = readEvent(events, map[string]any{"message_index": 5, "trace_id": "t1"})
	if r["availability"] != "not_found" {
		t.Fatalf("未命中: %v", r)
	}
	// tool_call_index 收窄
	events2 := []map[string]any{
		{"trace_id": "t1", "message_index": 0, "role": "assistant", "content": "x", "step_pointer": "",
			"tool_calls": []map[string]any{{"name": "read", "input": "a", "output": "b", "error": ""}, {"name": "ls", "input": "c", "output": "d", "error": ""}}},
	}
	r = readEvent(events2, map[string]any{"message_index": 0, "tool_call_index": 1})
	ev := r["event"].(map[string]any)
	calls := ev["tool_calls"].([]map[string]any)
	if len(calls) != 1 || calls[0]["name"] != "ls" {
		t.Fatalf("tool_call_index 收窄: %v", calls)
	}
	// 越界 → 空调用列表
	r = readEvent(events2, map[string]any{"message_index": 0, "tool_call_index": 9})
	ev = r["event"].(map[string]any)
	if calls, ok := ev["tool_calls"].([]map[string]any); ok && len(calls) != 0 {
		t.Fatalf("越界应空: %v", ev["tool_calls"])
	}
}

// TestCompareRuns 运行对比（Python _compare_runs）
func TestCompareRuns(t *testing.T) {
	r := compareRuns(nil, map[string]any{"query": "x"})
	if r["availability"] != "not_available" || r["reason"] != "no_paired_candidate_feedback" {
		t.Fatalf("无反馈: %v", r)
	}
	r = compareRuns(map[string]any{"score": 1}, map[string]any{"query": "score"})
	if r["availability"] != "available" {
		t.Fatalf("可用: %v", r)
	}
	pf := r["paired_feedback"].(map[string]any)
	if _, ok := pf["exact_spans"]; !ok {
		t.Fatalf("有词命中应返回 exact_spans: %v", pf)
	}
}

// TestQueryTerms 分词去停用词（Python _query_terms）
func TestQueryTerms(t *testing.T) {
	terms := queryTerms("The timeout Error in /tmp/log.txt")
	// the/停用词剔除，timeout/error/tmp/log.txt 保留
	if len(terms) == 0 || terms[0] != "timeout" {
		t.Fatalf("分词: %v", terms)
	}
	for _, s := range terms {
		if s == "the" {
			t.Fatal("停用词未剔除")
		}
	}
}

// TestBoundedExactText 有界文本（Python _bounded_exact_text）
func TestBoundedExactText(t *testing.T) {
	r := boundedExactText("short", 10)
	if r["complete"] != true || r["omission_origin"] != "none" {
		t.Fatalf("短文本: %v", r)
	}
	r = boundedExactText(strings.Repeat("x", 100), 10)
	if r["complete"] != false || r["omission_origin"] != "controller_bound" || r["omitted_source_chars"] != 90 {
		t.Fatalf("长文本: %v", r)
	}
}

// TestContinuousPrefixEnd 连续前缀（Python _continuous_prefix_end）
func TestContinuousPrefixEnd(t *testing.T) {
	if got := continuousPrefixEnd([][2]int{{0, 5}, {5, 10}, {20, 30}}); got != 10 {
		t.Fatalf("连续前缀 = %d, want 10", got)
	}
	if got := continuousPrefixEnd(nil); got != 0 {
		t.Fatalf("空 = %d", got)
	}
}

// TestUniqueAutomaticRequestID 自动 id（Python _unique_automatic_request_id）
func TestUniqueAutomaticRequestID(t *testing.T) {
	id := uniqueAutomaticRequestID("q1", "src/a.txt", 100, map[string]bool{})
	if !strings.HasPrefix(id, "q1.auto.") || !strings.HasSuffix(id, ".100") {
		t.Fatalf("id 格式: %q", id)
	}
	// 冲突加后缀
	existing := map[string]bool{id: true}
	id2 := uniqueAutomaticRequestID("q1", "src/a.txt", 100, existing)
	if id2 == id {
		t.Fatal("冲突未回避")
	}
}

// ──────────────────────────── Task 8/9：artifact 与 repository 族 ────────────────────────────

// newInvCase 构造测试用 CaseAnalysisInput（对齐 Python test _case fixture）
func newInvCase(t *testing.T, metadata map[string]any) *CaseAnalysisInput {
	t.Helper()
	caseDir := t.TempDir()
	judgeDir := filepath.Join(caseDir, "judge")
	if err := os.MkdirAll(judgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(caseDir, "result.json")
	resultJSON, _ := json.Marshal(map[string]any{"evaluation": map[string]any{"metadata": metadataOrEmpty(metadata)}})
	if err := os.WriteFile(resultPath, resultJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	trace, _ := json.Marshal(map[string]any{"traces": []map[string]any{{
		"trace_id": "case_001:trial_1",
		"messages": []map[string]any{{
			"role": "assistant", "message_index": 7, "step_pointer": "trial_1:message_7",
			"content": "I need to distinguish the parser and routing hypotheses.",
			"tool_calls": []map[string]any{{
				"name": "read_file", "input": `{"path":"public.txt"}`,
				"output": "prefix\nEXACT_DISCRIMINATOR parser selected legacy mode\nsuffix",
			}},
		}},
	}}})
	if err := os.WriteFile(filepath.Join(judgeDir, "normalized_trace.json"), trace, 0o644); err != nil {
		t.Fatal(err)
	}
	return &CaseAnalysisInput{ResultPath: resultPath, EvaluationMetadata: metadataOrEmpty(metadata)}
}

// metadataOrEmpty nil → 空 map（JSON marshal 需要）
func metadataOrEmpty(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

// TestArtifactPathAliases 快照逻辑名映射（Python _artifact_path_aliases）
func TestArtifactPathAliases(t *testing.T) {
	caseDir := t.TempDir()
	artifacts := filepath.Join(caseDir, "artifacts")
	workspace := filepath.Join(artifacts, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(artifacts, "hashed.xlsx")
	if err := os.WriteFile(stored, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wsStored := filepath.Join(workspace, "nested.xlsx")
	if err := os.WriteFile(wsStored, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{ResultPath: filepath.Join(caseDir, "result.json"), EvaluationMetadata: map[string]any{
		"analysis_artifact_snapshot": map[string]any{"files": []any{
			map[string]any{"path": "hashed.xlsx", "source_path": "Controlling Contract.xlsx"},
			map[string]any{"path": "workspace/nested.xlsx", "source_path": "Old Report.xlsx"},
			map[string]any{"path": "missing.xlsx", "source_path": "Ghost.xlsx"},
		}},
	}}
	aliases := artifactPathAliases(caseInput, artifacts)
	if len(aliases) != 2 {
		t.Fatalf("应命中两个实体文件: %v", aliases)
	}
	if aliases[stored] != "Controlling Contract.xlsx" || aliases[wsStored] != "Old Report.xlsx" {
		t.Fatalf("别名不符: %v", aliases)
	}
}

// TestSelectStructuredArtifactFiles 提示词过滤与排序裁剪（Python _select_structured_artifact_files）
func TestSelectStructuredArtifactFiles(t *testing.T) {
	files := []string{"/a/one.xlsx", "/a/two.xlsx", "/a/three.xlsx"}
	// 无提示词：全部保留（≤上限）
	got := selectStructuredArtifactFiles(files, "no hints", nil)
	if len(got) != 3 {
		t.Fatalf("无提示词应全保留: %v", got)
	}
	// pdf 提示词命中 → 只留 .pdf
	got = selectStructuredArtifactFiles([]string{"/a/one.xlsx", "/a/report.pdf"}, "pdf page document", nil)
	if len(got) != 1 || !strings.HasSuffix(got[0], ".pdf") {
		t.Fatalf("pdf 提示词过滤: %v", got)
	}
}

// TestReadArtifactWindow 文本 artifact 窗口（Python _read_artifact_window 主路径）
func TestReadArtifactWindow(t *testing.T) {
	caseDir := t.TempDir()
	artifacts := filepath.Join(caseDir, "artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("A", 100) + "NEEDLE" + strings.Repeat("B", 100)
	if err := os.WriteFile(filepath.Join(artifacts, "report.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{ResultPath: filepath.Join(caseDir, "result.json")}
	cache := map[string]string{}
	// 无效路径
	r := readArtifactWindow(caseInput, map[string]any{"relative_path": ""}, cache)
	if r["availability"] != "invalid" {
		t.Fatalf("空路径: %v", r)
	}
	// 未找到
	r = readArtifactWindow(caseInput, map[string]any{"relative_path": "artifacts/ghost.txt"}, cache)
	if r["availability"] != "not_found" {
		t.Fatalf("缺失文件: %v", r)
	}
	// 全文窗口
	r = readArtifactWindow(caseInput, map[string]any{"relative_path": "artifacts/report.txt"}, cache)
	if r["availability"] != "available" || r["window_complete"] != true || r["omission_origin"] != "none" {
		t.Fatalf("全文窗口: %v", r)
	}
	// 带起止偏移窗口（Python: source_char_start + max_chars）
	r = readArtifactWindow(caseInput, map[string]any{"relative_path": "artifacts/report.txt", "source_char_start": 95, "max_chars": 20}, cache)
	if r["source_char_start"] != 95 || r["source_char_end"] != 115 || r["window_complete"] != false {
		t.Fatalf("偏移窗口: %v", r)
	}
	if r["omission_origin"] != "controller_read_window" {
		t.Fatalf("省略来源: %v", r)
	}
	if !strings.Contains(r["text"].(string), "NEEDLE") {
		t.Fatalf("窗口文本: %q", r["text"])
	}
	// 不支持的后缀
	if err := os.WriteFile(filepath.Join(artifacts, "blob.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = readArtifactWindow(caseInput, map[string]any{"relative_path": "artifacts/blob.bin"}, cache)
	if r["availability"] != "unsupported" {
		t.Fatalf("不支持后缀: %v", r)
	}
}

// TestUnambiguousLogicalArtifactMatches 逻辑名恢复唯一性与不足阈值（Python _unambiguous_logical_artifact_matches）
// 词项语义：queryTerms 按 Python 正则切分（下划线连成一个词），requested 词项须 ≥2
func TestUnambiguousLogicalArtifactMatches(t *testing.T) {
	aliases := map[string]string{
		"/a/hashed1.xlsx": "Quarterly Financial Report.xlsx",
		"/a/hashed2.xlsx": "Monthly Financial Summary.xlsx",
	}
	// 请求名损坏但词项重叠唯一且达阈值 → 恢复（quarterly/financial 与第一文件重叠 2）
	got := unambiguousLogicalArtifactMatches("quarterly financial", aliases)
	if len(got) != 1 || got[0][1] != "Quarterly Financial Report.xlsx" {
		t.Fatalf("唯一恢复: %v", got)
	}
	// 两文件同分且达阈值 → 返回空（非唯一）
	aliases2 := map[string]string{
		"/a/hashed1.xlsx": "Quarterly Financial Report.xlsx",
		"/a/hashed2.xlsx": "Quarterly Financial Summary.xlsx",
	}
	got = unambiguousLogicalArtifactMatches("quarterly financial", aliases2)
	if len(got) != 0 {
		t.Fatalf("非唯一应空: %v", got)
	}
	// 词项不足 2（下划线连写成一个词）→ 空
	got = unambiguousLogicalArtifactMatches("quarterly_financial", aliases)
	if len(got) != 0 {
		t.Fatalf("词项不足: %v", got)
	}
}

// TestInspectArtifact 文本 artifact 检索主路径（Python _inspect_artifact）
func TestInspectArtifact(t *testing.T) {
	caseDir := t.TempDir()
	artifacts := filepath.Join(caseDir, "artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "log.txt"), []byte("EXACT_DISCRIMINATOR parser selected legacy mode"), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{ResultPath: filepath.Join(caseDir, "result.json")}
	// 无 artifacts 目录 → not_available
	emptyCase := &CaseAnalysisInput{ResultPath: filepath.Join(t.TempDir(), "result.json")}
	r := inspectArtifact(emptyCase, map[string]any{"query": "x"}, map[string]string{})
	if r["availability"] != "not_available" || r["reason"] != "physical_artifact_snapshot_not_available" {
		t.Fatalf("无快照: %v", r)
	}
	// 命中
	r = inspectArtifact(caseInput, map[string]any{"query": "parser legacy"}, map[string]string{})
	if r["availability"] != "available" {
		t.Fatalf("命中: %v", r)
	}
	matches := r["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["source"] != "artifacts/log.txt" {
		t.Fatalf("matches: %v", matches)
	}
	// 不含检索词 → not_found
	r = inspectArtifact(caseInput, map[string]any{"query": "zzz_nothing"}, map[string]string{})
	if r["availability"] != "not_found" {
		t.Fatalf("未命中: %v", r)
	}
}

// TestStructuredArtifactTextXlsx xlsx 公式与单元格文本（Python _xlsx_text；openpyxl data_only=False 对齐）
func TestStructuredArtifactTextXlsx(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	if err := f.SetCellValue(sheet, "A1", 10); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue(sheet, "A2", 20); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellFormula(sheet, "A3", "SUM(A1:A2)"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "result.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	text := structuredArtifactText(path)
	if !strings.Contains(text, "[sheet:"+sheet+"]") {
		t.Fatalf("sheet 头: %q", text)
	}
	if !strings.Contains(text, "A1=10") || !strings.Contains(text, "A3=SUM(A1:A2)") {
		t.Fatalf("单元格与公式: %q", text)
	}
}

// TestStructuredArtifactTextPptx pptx 幻灯片文本（Python _pptx_text；zip+xml 手拆）
func TestStructuredArtifactTextPptx(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.pptx")
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	// 两条 slide + 一条非 slide 文件
	for i, body := range []struct{ name, text string }{
		{"ppt/slides/slide2.xml", `<p:sld><p:cSld><p:spTree><a:t>Second Slide</a:t><a:t>更多文本</a:t></p:spTree></p:cSld></p:sld>`},
		{"ppt/slides/slide1.xml", `<p:sld><p:cSld><p:spTree><a:t>First Slide</a:t></p:spTree></p:cSld></p:sld>`},
		{"docProps/app.xml", `<a:t>ignored</a:t>`},
	} {
		fc, _ := w.Create(body.name)
		if i >= 2 {
			fc.Write([]byte(body.text))
			continue
		}
		fc.Write([]byte(body.text))
	}
	_ = w.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	text := structuredArtifactText(path)
	if !strings.Contains(text, "[ppt/slides/slide1.xml] First Slide") ||
		!strings.Contains(text, "[ppt/slides/slide2.xml] Second Slide 更多文本") {
		t.Fatalf("slide 文本: %q", text)
	}
	if strings.Contains(text, "ignored") {
		t.Fatalf("非 slide 文件不应纳入: %q", text)
	}
}

// TestStructuredArtifactTextMalformed 畸形结构化文件吞异常返回空串（Python except Exception）
func TestStructuredArtifactTextMalformed(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.xlsx", "b.docx", "c.pdf", "d.pptx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a real file"), 0o644); err != nil {
			t.Fatal(err)
		}
		if text := structuredArtifactText(filepath.Join(dir, name)); text != "" {
			t.Fatalf("畸形 %s 应返回空串: %q", name, text)
		}
	}
}

// TestInspectEvaluation 评估元数据检索（Python _inspect_evaluation）
func TestInspectEvaluation(t *testing.T) {
	caseDir := t.TempDir()
	resultPath := filepath.Join(caseDir, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"evaluation":{"metadata":{"summary":"legacy mode failure"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	caseInput := &CaseAnalysisInput{ResultPath: resultPath, EvaluationMetadata: map[string]any{"summary": "legacy mode failure"}}
	r := inspectEvaluation(caseInput, map[string]any{"query": "legacy"})
	if r["availability"] != "available" || r["evidence_class"] != "evaluation_metadata" {
		t.Fatalf("命中: %v", r)
	}
	matches := r["matches"].([]map[string]any)
	sources := map[string]bool{}
	for _, m := range matches {
		sources[m["source"].(string)] = true
	}
	if !sources["case.evaluation_metadata"] || !sources["case.result"] {
		t.Fatalf("两个来源都应可命中: %v", sources)
	}
	r = inspectEvaluation(caseInput, map[string]any{"query": "zzz_nothing"})
	if r["availability"] != "not_found" {
		t.Fatalf("未命中: %v", r)
	}
}

// TestSearchAndReadRepository 仓库检索与防穿越读取（Python _search_repository/_read_repository_file）
func TestSearchAndReadRepository(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repository")
	src := filepath.Join(repo, "src", "router.py")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("def route(mode):\n    return 'legacy' if mode is None else mode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 无仓库 → not_available
	r := searchRepository("", map[string]any{"query": "router"})
	if r["availability"] != "not_available" || r["reason"] != "repository_snapshot_not_available" {
		t.Fatalf("无仓库: %v", r)
	}
	// 搜索命中
	r = searchRepository(repo, map[string]any{"query": "router legacy"})
	if r["availability"] != "available" || r["matched_file_count"] != 1 {
		t.Fatalf("搜索: %v", r)
	}
	files := r["files"].([]map[string]any)
	if files[0]["relative_path"] != "src/router.py" {
		t.Fatalf("relative_path: %v", files)
	}
	// 读取：须先经 search 返回（discovered_paths 守卫）
	discovered := map[string]bool{"src/router.py": true}
	r = readRepositoryFile(repo, map[string]any{"relative_path": "src/router.py"}, discovered)
	if r["availability"] != "available" {
		t.Fatalf("读取: %v", r)
	}
	content := r["content"].(map[string]any)
	if !strings.Contains(content["text"].(string), "return 'legacy'") {
		t.Fatalf("content.text: %v", content)
	}
	// 未 discover 过的路径拒绝
	r = readRepositoryFile(repo, map[string]any{"relative_path": "src/router.py"}, map[string]bool{})
	if r["availability"] != "invalid" || r["reason"] != "relative_path_not_returned_by_prior_search_repository" {
		t.Fatalf("未发现路径: %v", r)
	}
	// 非法相对路径（穿越/绝对）在 safeRelativePath 阶段即拒绝
	if safeRelativePath("../secret.txt") != "" || safeRelativePath("/etc/passwd") != "" {
		t.Fatal("穿越路径应被拒绝")
	}
	// 不支持后缀
	bin := filepath.Join(repo, "blob.bin")
	if err := os.WriteFile(bin, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	discovered["blob.bin"] = true
	r = readRepositoryFile(repo, map[string]any{"relative_path": "blob.bin"}, discovered)
	if r["availability"] != "unsupported" {
		t.Fatalf("不支持后缀: %v", r)
	}
}

// TestRepositoryTextFiles 文本文件清单与 2000 上限（Python _repository_text_files）
func TestRepositoryTextFiles(t *testing.T) {
	repo := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("f%d.go", i)), []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "skip.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := repositoryTextFiles(repo)
	if len(got) != 3 {
		t.Fatalf("应 3 个 .go: %v", got)
	}
	// 排序确定性
	if !sort.StringsAreSorted(got) {
		t.Fatalf("应有序: %v", got)
	}
}

// TestWindowsLongPath 扩展路径前缀（Python _windows_long_path；GOOS 判据）
func TestWindowsLongPath(t *testing.T) {
	got := windowsLongPath("/tmp/x")
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(got, `\\?\`) {
			t.Fatalf("windows 应加前缀: %q", got)
		}
	} else if got != "/tmp/x" {
		t.Fatalf("非 windows 恒等: %q", got)
	}
}
