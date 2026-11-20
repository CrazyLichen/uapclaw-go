package singleharness

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// 造一个最小 refs/harness 目录树（PackageFingerprint 用）。
func makeHarnessDir(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDigest 稳定摘要与键序无关。
func TestDigest(t *testing.T) {
	a := Digest(map[string]any{"x": 1.5, "y": "中"})
	b := Digest(map[string]any{"y": "中", "x": 1.5})
	if a != b || len(a) != 64 {
		t.Fatalf("摘要应稳定: %s vs %s", a, b)
	}
}

// TestPackageFingerprint 包指纹：内容寻址、目录无关、忽略产物、异常分支。
func TestPackageFingerprint(t *testing.T) {
	dir := t.TempDir()
	harness := filepath.Join(dir, "harness")
	makeHarnessDir(t, harness, map[string]string{
		"manifest.json":          "{\"name\":\"h\"}",
		"skills/s.yaml":          "a: 1",
		"__pycache__/junk.pyc":   "junk",
		"skills/__pycache__/j.p": "junk",
		"notes.pyc":              "compiled",
	})
	refsA := filepath.Join(dir, "refs_a.yaml")
	refsB := filepath.Join(dir, "refs_b.yaml")
	// 相对与绝对两种写法
	if err := os.WriteFile(refsA, []byte("harness_refs:\n  solver: harness\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refsB, []byte(fmt.Sprintf("harness_refs:\n  solver: %s\n", harness)), 0o644); err != nil {
		t.Fatal(err)
	}
	fa, err := PackageFingerprint(refsA)
	if err != nil {
		t.Fatalf("相对路径指纹失败: %v", err)
	}
	fb, err := PackageFingerprint(refsB)
	if err != nil {
		t.Fatalf("绝对路径指纹失败: %v", err)
	}
	if fa != fb {
		t.Fatalf("指纹应与目录写法无关: %s vs %s", fa, fb)
	}
	// 内容变化 → 指纹变化
	makeHarnessDir(t, harness, map[string]string{"skills/s.yaml": "a: 2"})
	fc, _ := PackageFingerprint(refsA)
	if fc == fa {
		t.Fatal("内容变化应改变指纹")
	}
	// 目录缺失
	missing := filepath.Join(dir, "missing.yaml")
	if err := os.WriteFile(missing, []byte("harness_refs:\n  solver: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageFingerprint(missing); err == nil || err.Error() != "Harness package directory is unavailable" {
		t.Fatalf("缺失目录应报错: %v", err)
	}
	// 无包
	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte("harness_refs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageFingerprint(empty); err == nil || err.Error() != "Harness refs contain no packages" {
		t.Fatalf("空包应报错: %v", err)
	}
}

// TestCompleteScores 完整分数校验全分支。
func TestCompleteScores(t *testing.T) {
	dir := t.TempDir()
	evalSeq := 0
	writeEval := func(t *testing.T, rows string) string {
		t.Helper()
		evalSeq++
		evalRef := filepath.Join(dir, fmt.Sprintf("eval_%d.yaml", evalSeq))
		content := "cases:\n" + rows
		if err := os.WriteFile(evalRef, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return evalRef
	}
	writeResult := func(t *testing.T, name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	// 合法路径
	writeResult(t, "r1.json", `{"execution_status":"ok","status":"passed","score":0.8,"evaluation":{"method":"rubric","metadata":{"score_mode":"continuous"}}}`)
	evalRef := writeEval(t, "  - case_id: c1\n    score: 0.8\n    status: passed\n    result_path: r1.json\n")
	scores, err := CompleteScores(evalRef, map[string]bool{"c1": true})
	if err != nil {
		t.Fatalf("合法评估应通过: %v", err)
	}
	if scores["c1"] != 0.8 {
		t.Fatalf("分数错误: %v", scores)
	}
	// 重复 case_id
	dup := writeEval(t, "  - case_id: c1\n    score: 0.8\n    status: passed\n    result_path: r1.json\n  - case_id: c1\n    score: 0.8\n    status: passed\n    result_path: r1.json\n")
	if _, err := CompleteScores(dup, map[string]bool{"c1": true}); err == nil || err.Error() != "Evaluation contains duplicate or missing case identities" {
		t.Fatalf("重复应拒绝: %v", err)
	}
	// 分数越界
	oob := writeEval(t, "  - case_id: c1\n    score: 1.5\n    status: passed\n    result_path: r1.json\n")
	if _, err := CompleteScores(oob, map[string]bool{"c1": true}); err == nil || err.Error() != "Invalid correctness score for c1" {
		t.Fatalf("越界应拒绝: %v", err)
	}
	// bool 分数
	boolScore := writeEval(t, "  - case_id: c1\n    score: true\n    status: passed\n    result_path: r1.json\n")
	if _, err := CompleteScores(boolScore, map[string]bool{"c1": true}); err == nil {
		t.Fatal("bool 分数应拒绝")
	}
	// infrastructure_skip
	skip := writeEval(t, "  - case_id: c1\n    score: 0.8\n    status: passed\n    metadata: {infrastructure_skip: true}\n    result_path: r1.json\n")
	if _, err := CompleteScores(skip, map[string]bool{"c1": true}); err == nil {
		t.Fatal("skip 应拒绝")
	}
	// 结果执行失败
	writeResult(t, "r2.json", `{"execution_status":"error","status":"passed","score":0.8}`)
	errEval := writeEval(t, "  - case_id: c2\n    score: 0.8\n    status: passed\n    result_path: r2.json\n")
	if _, err := CompleteScores(errEval, map[string]bool{"c2": true}); err == nil || err.Error() != "Inconsistent or failed execution for c2" {
		t.Fatalf("执行失败应拒绝: %v", err)
	}
	// 结果分数不一致
	writeResult(t, "r3.json", `{"execution_status":"ok","status":"passed","score":0.1,"evaluation":{"method":"rubric"}}`)
	mismatch := writeEval(t, "  - case_id: c3\n    score: 0.8\n    status: passed\n    result_path: r3.json\n")
	if _, err := CompleteScores(mismatch, map[string]bool{"c3": true}); err == nil {
		t.Fatal("分数不一致应拒绝")
	}
	// 二元 LLM 评分拒绝
	writeResult(t, "r4.json", `{"execution_status":"ok","status":"passed","score":1.0,"evaluation":{"method":"llm_as_judge","metadata":{"score_mode":"binary"}}}`)
	binary := writeEval(t, "  - case_id: c4\n    score: 1.0\n    status: passed\n    result_path: r4.json\n")
	if _, err := CompleteScores(binary, map[string]bool{"c4": true}); err == nil || err.Error() != "Binary LLM grades cannot enter a continuous promotion run" {
		t.Fatalf("二元 LLM 评分应拒绝: %v", err)
	}
	// 期望用例集不匹配
	if _, err := CompleteScores(evalRef, map[string]bool{"other": true}); err == nil ||
		err.Error() != "Evaluation does not exactly cover its frozen case set" {
		t.Fatalf("用例集不匹配应拒绝: %v", err)
	}
	// 空评估
	empty := writeEval(t, "")
	if _, err := CompleteScores(empty, map[string]bool{}); err == nil {
		t.Fatal("空评估应拒绝")
	}
}

// TestMeanScoreAndImprovements 均值与提升判定。
func TestMeanScoreAndImprovements(t *testing.T) {
	if _, err := MeanScore(map[string]float64{}); err == nil || err.Error() != "Cannot score an empty evaluation" {
		t.Fatalf("空评估应拒绝: %v", err)
	}
	mean, err := MeanScore(map[string]float64{"a": 0.5, "b": 1.0})
	if err != nil || mean != 0.75 {
		t.Fatalf("均值错误: %v %v", mean, err)
	}
	if !FirstImproved(1.0, 0.5, 0.4) {
		t.Fatal("超阈值应通过")
	}
	if FirstImproved(1.0, 0.5, 0.5) {
		t.Fatal("恰好阈值不应通过（严格大于+EPSILON）")
	}
	targets := map[string]bool{"a": true, "b": true}
	base := map[string]float64{"a": 0.5, "b": 0.5}
	higher := map[string]float64{"a": 0.9, "b": 0.6}
	if !TargetsImproved(higher, base, targets, 0.05) {
		t.Fatal("全部提升应通过")
	}
	if TargetsImproved(higher, base, map[string]bool{}, 0.05) {
		t.Fatal("空目标应拒绝")
	}
	partial := map[string]float64{"a": 0.9, "b": 0.5}
	if TargetsImproved(partial, base, targets, 0.05) {
		t.Fatal("任一未提升应拒绝")
	}
	if TargetsImproved(higher, base, map[string]bool{"missing": true}, 0.05) {
		t.Fatal("未知目标应拒绝")
	}
}

// TestFullEvaluationDecision 两阶段全量评估决策。
func TestFullEvaluationDecision(t *testing.T) {
	baseline := map[string]float64{"a": 0.5, "b": 0.5}
	targets := map[string]bool{"a": true}
	scores := map[string]float64{"a": 0.9, "b": 0.55}
	proof := map[string]any{
		"identity":               "run-1",
		"qualified":              true,
		"target_case_ids":        []any{"a"},
		"baseline_scores_digest": Digest(baseline),
	}
	decision, err := FullEvaluationDecision(scores, baseline, targets, 0.1, proof, "run-1")
	if err != nil {
		t.Fatalf("决策失败: %v", err)
	}
	if decision["accepted"] != true || decision["reason"] != "two_stage_improvement_confirmed" {
		t.Fatalf("应确认提升: %v", decision)
	}
	if diff := decision["full_score_delta"].(float64) - (0.725 - 0.5); diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("delta 错误: %v", decision["full_score_delta"])
	}
	// 目标未提升
	bad := map[string]float64{"a": 0.5, "b": 0.55}
	decision, err = FullEvaluationDecision(bad, baseline, targets, 0.1, proof, "run-1")
	if err != nil {
		t.Fatalf("决策失败: %v", err)
	}
	if decision["reason"] != "target_case_not_improved" || decision["accepted"] != false {
		t.Fatalf("目标未提升应拒绝: %v", decision)
	}
	// 非目标回滚超过 max_regression
	regressed := map[string]float64{"a": 0.9, "b": 0.3}
	decision, err = FullEvaluationDecision(regressed, baseline, targets, 0.1, proof, "run-1")
	if err != nil {
		t.Fatalf("决策失败: %v", err)
	}
	if decision["reason"] != "non_target_case_regressed" {
		t.Fatalf("回滚应被记录: %v", decision)
	}
	// 全体未过平均提升线
	flat := map[string]float64{"a": 0.9, "b": 0.1}
	decision, err = FullEvaluationDecision(flat, baseline, targets, 0.5, proof, "run-1")
	if err != nil {
		t.Fatalf("决策失败: %v", err)
	}
	if decision["reason"] != "full_score_not_improved" {
		t.Fatalf("平均未提升应记录: %v", decision)
	}
	// 证据不匹配各分支
	wrongIdentity := map[string]any{"identity": "other", "qualified": true, "target_case_ids": []any{"a"}, "baseline_scores_digest": Digest(baseline)}
	if _, err := FullEvaluationDecision(scores, baseline, targets, 0.1, wrongIdentity, "run-1"); err == nil {
		t.Fatal("身份不匹配应拒绝")
	}
	unqualified := map[string]any{"identity": "run-1", "qualified": false, "target_case_ids": []any{"a"}, "baseline_scores_digest": Digest(baseline)}
	if _, err := FullEvaluationDecision(scores, baseline, targets, 0.1, unqualified, "run-1"); err == nil {
		t.Fatal("未限定应拒绝")
	}
	wrongTargets := map[string]any{"identity": "run-1", "qualified": true, "target_case_ids": []any{"b"}, "baseline_scores_digest": Digest(baseline)}
	if _, err := FullEvaluationDecision(scores, baseline, targets, 0.1, wrongTargets, "run-1"); err == nil {
		t.Fatal("目标集不匹配应拒绝")
	}
	wrongDigest := map[string]any{"identity": "run-1", "qualified": true, "target_case_ids": []any{"a"}, "baseline_scores_digest": "deadbeef"}
	if _, err := FullEvaluationDecision(scores, baseline, targets, 0.1, wrongDigest, "run-1"); err == nil {
		t.Fatal("基线摘要不匹配应拒绝")
	}
	// 用例集不同
	if _, err := FullEvaluationDecision(map[string]float64{"a": 0.9}, baseline, targets, 0.1, proof, "run-1"); err == nil {
		t.Fatal("用例集不同应拒绝")
	}
}

// TestInconclusiveCapabilityCases 不可判定用例收集。
func TestInconclusiveCapabilityCases(t *testing.T) {
	dir := t.TempDir()
	// c1：证据完整 → 可判定；c2：结果文件缺失 → 不可判定；c3：时序不完整 → 不可判定
	if err := os.WriteFile(filepath.Join(dir, "r1.json"), []byte(
		`{"metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":true}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r3.json"), []byte(
		`{"metadata":{"execution":{"execution_evidence":{"status":"complete","timing_complete":false}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	evalRef := filepath.Join(dir, "eval.yaml")
	content := "cases:\n" +
		"  - case_id: c1\n    result_path: r1.json\n" +
		"  - case_id: c2\n    result_path: r2.json\n" +
		"  - case_id: c3\n    result_path: r3.json\n"
	if err := os.WriteFile(evalRef, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := []map[string]string{
		{"target_case_id": "c1"},
		{"target_case_id": "c2"},
		{"target_case_id": "c3"},
	}
	got := InconclusiveCapabilityCases(evalRef, missing)
	if got["c1"] {
		t.Fatal("c1 证据完整应可判定")
	}
	if !got["c2"] || !got["c3"] {
		t.Fatalf("c2/c3 应不可判定: %v", got)
	}
	// 空声明 → 空
	if got := InconclusiveCapabilityCases(evalRef, nil); len(got) != 0 {
		t.Fatalf("空声明应为空: %v", got)
	}
	// 评估文件缺失 → 全部不可判定
	if got := InconclusiveCapabilityCases(filepath.Join(dir, "nope.yaml"), missing); len(got) != 3 {
		t.Fatalf("评估缺失应全部不可判定: %v", got)
	}
	// cases 非列表
	badRef := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badRef, []byte("cases: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := InconclusiveCapabilityCases(badRef, missing); len(got) != 3 {
		t.Fatalf("cases 非列表应全部不可判定: %v", got)
	}
}

// TestPromotionPolicyHelpers 助手函数补全。
func TestPromotionPolicyHelpers(t *testing.T) {
	// expandUser
	if got := expandUser("/no/tilde"); got != "/no/tilde" {
		t.Fatalf("无波浪线路径应原样: %s", got)
	}
	// asFloat / numberEquals / isJSONNumber / isBoolValue / isFiniteNumber
	if asFloat(1) != 1 || asFloat(int64(2)) != 2 || asFloat(1.5) != 1.5 {
		t.Fatal("asFloat 分支错误")
	}
	if numberEquals(1, 1.0) != true || numberEquals(1, 2.0) || numberEquals("x", 1) || numberEquals(nil, nil) {
		t.Fatal("numberEquals 行为错误")
	}
	if !isJSONNumber(1) || isJSONNumber("x") || !isBoolValue(true) || isBoolValue(1) {
		t.Fatal("类型判定错误")
	}
	if !isFiniteNumber(1.5) || isFiniteNumber(math.Inf(1)) {
		t.Fatal("有限性判定错误")
	}
	// sameKeys 家族
	if !sameKeys(map[string]float64{"a": 1}, map[string]bool{"a": true}) {
		t.Fatal("sameKeys 应相等")
	}
	if sameKeys(map[string]float64{"a": 1}, map[string]bool{}) {
		t.Fatal("sameKeys 长度不等应 false")
	}
	if !sameKeysBB(map[string]bool{"a": true}, map[string]bool{"a": true}) || sameKeysBB(map[string]bool{"a": true}, map[string]bool{}) {
		t.Fatal("sameKeysBB 行为错误")
	}
	if !sameKeysFF(map[string]float64{"a": 1}, map[string]float64{"a": 2}) || sameKeysFF(map[string]float64{"a": 1}, map[string]float64{}) {
		t.Fatal("sameKeysFF 行为错误")
	}
	if !targetsSubset(map[string]bool{"a": true}, map[string]float64{"a": 1, "b": 2}) {
		t.Fatal("子集应通过")
	}
	if targetsSubset(map[string]bool{"c": true}, map[string]float64{"a": 1}) {
		t.Fatal("非子集应拒绝")
	}
	// pathEntry / withinDir / toString / jsonNumberToString
	if pathEntry(filepath.Join(t.TempDir(), "nope")) != nil {
		t.Fatal("缺失路径应返回 nil")
	}
	tmp := t.TempDir()
	if !withinDir(filepath.Join(tmp, "x"), tmp) {
		t.Fatal("目录内应通过")
	}
	if withinDir("/etc/passwd", tmp) {
		t.Fatal("目录外应拒绝")
	}
	if toString("s") != "s" || toString(nil) != "" || toString(42) != "42" || toString(1.5) != "1.5" {
		t.Fatal("toString 行为错误")
	}
	// rowStatus
	if rowStatus(map[string]any{"status": "passed"}) != "passed" || rowStatus(map[string]any{}) != "" {
		t.Fatal("rowStatus 行为错误")
	}
}
