package evaluator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptimizationSignalsContract_归一化(t *testing.T) {
	score := 0.75
	contract := OptimizationSignalsContract(
		&score,
		map[string]any{
			"b_dim": map[string]any{"availability": "available", "value": 0.5, "source": "s1"},
			"a_dim": map[string]any{"availability": "unavailable", "value": 9},
			"":      map[string]any{"value": 1},
		},
		"test",
	)
	if contract["schema_version"] != OptimizationSignalsSchemaVersion {
		t.Fatalf("schema_version: %v", contract["schema_version"])
	}
	continuous := contract["continuous_score"].(map[string]any)
	if continuous["availability"] != "available" || continuous["value"].(float64) != 0.75 {
		t.Fatalf("continuous_score: %v", continuous)
	}
	if contract["promotion_authority"] != "eval_ref_case_score" {
		t.Fatalf("promotion_authority: %v", contract["promotion_authority"])
	}
	dimensions := contract["dimensions"].(map[string]any)
	if _, ok := dimensions["b_dim"]; !ok {
		t.Fatalf("可用维度应保留: %v", dimensions)
	}
	if _, ok := dimensions["a_dim"]; ok {
		t.Fatalf("不可用维度应丢弃")
	}
	if _, ok := dimensions[""]; ok {
		t.Fatalf("空名维度应丢弃")
	}
}

func TestOptimizationSignalsContract_不可用分数(t *testing.T) {
	contract := OptimizationSignalsContract(nil, nil, "")
	continuous := contract["continuous_score"].(map[string]any)
	if continuous["availability"] != "not_available" || continuous["value"] != nil {
		t.Fatalf("nil 分数应 not_available: %v", continuous)
	}
}

func TestEvaluationOptimizationSignals_显式契约(t *testing.T) {
	// 无 optimization_signals → 空契约
	empty := EvaluationOptimizationSignals(map[string]any{})
	if empty["continuous_score"].(map[string]any)["availability"] != "not_available" {
		t.Fatalf("缺省应 not_available")
	}
	// available 读取 value
	got := EvaluationOptimizationSignals(map[string]any{
		"optimization_signals": map[string]any{
			"continuous_score": map[string]any{
				"availability": "available", "value": 0.5, "source": "rubric",
			},
			"dimensions": map[string]any{
				"d1": map[string]any{"availability": "available", "value": 0.25},
			},
		},
	})
	continuous := got["continuous_score"].(map[string]any)
	if continuous["value"].(float64) != 0.5 || continuous["source"] != "rubric" {
		t.Fatalf("available 应读取 value: %v", continuous)
	}
	// unavailable → 分数不采信
	got = EvaluationOptimizationSignals(map[string]any{
		"optimization_signals": map[string]any{
			"continuous_score": map[string]any{"availability": "unavailable", "value": 0.9},
		},
	})
	if got["continuous_score"].(map[string]any)["value"] != nil {
		t.Fatalf("unavailable 不应采信 value")
	}
}

func TestFiniteFloat_形态矩阵(t *testing.T) {
	if finiteFloat(true) != nil || finiteFloat(nil) != nil {
		t.Fatalf("bool/nil 应 nil")
	}
	if v := finiteFloat(1.5); v == nil || *v != 1.5 {
		t.Fatalf("float 应提取")
	}
	if finiteFloat("abc") != nil {
		t.Fatalf("非数字应 nil")
	}
	if v := finiteFloat("2.5"); v == nil || *v != 2.5 {
		t.Fatalf("数字字符串应提取")
	}
	inf := infFloat()
	if finiteFloat(inf) != nil {
		t.Fatalf("Inf 应 nil")
	}
}

func TestRequirementResultsFromJudgeCriteria(t *testing.T) {
	got := RequirementResultsFromJudgeCriteria([]any{
		map[string]any{"criterion_id": "c1", "score": 1.0, "rationale": "ok"},
		map[string]any{"verifier_id": "c2", "score": 0.5},
		map[string]any{"criterion_id": "", "score": 0.1},
		map[string]any{"criterion_id": "c3", "passed": false},
		"not-a-dict",
	})
	items := got["items"].([]map[string]any)
	if len(items) != 4 {
		t.Fatalf("应 4 条（空 ID 按 Python or 链生成 criterion_N，非映射跳过）: %d", len(items))
	}
	// Python: enumerate(start=1) 生成 criterion_3；normalize 按 (group,id) 排序后居末位
	if items[3]["requirement_id"] != "criterion_3" {
		t.Fatalf("空准则 ID 应按序号生成并排序居末: %v", items[3])
	}
	if items[0]["passed"] != true {
		t.Fatalf("score>=1 应显式通过: %v", items[0])
	}
	if items[1]["requirement_id"] != "c2" {
		t.Fatalf("verifier_id 回退: %v", items[1])
	}
	if items[2]["passed"] != false {
		t.Fatalf("显式 passed 优先: %v", items[2])
	}
}

func TestEvaluationRequirementResults_通用契约优先(t *testing.T) {
	// requirement_results 存在 → 直接归一化
	got := EvaluationRequirementResults(map[string]any{
		"requirement_results": map[string]any{
			"items": []any{
				map[string]any{"requirement_id": "r1", "group": "fail_to_pass", "passed": true, "score": 1.0},
				map[string]any{"requirement_id": "r1", "group": "fail_to_pass", "passed": false, "score": 0.0},
			},
		},
	}, "case1")
	if len(got) != 1 || got[0]["passed"] != false {
		t.Fatalf("后出现者胜出: %v", got)
	}
}

func TestEvaluationRequirementResults_legacy回退(t *testing.T) {
	// atomic_checks
	got := EvaluationRequirementResults(map[string]any{
		"atomic_checks": []any{
			map[string]any{"name": "a1", "passed": true, "detail": "d"},
			map[string]any{"name": "", "passed": false},
		},
	}, "")
	if len(got) != 1 || got[0]["group"] != AtomicCheckGroup {
		t.Fatalf("atomic_checks legacy: %v", got)
	}
	if got[0]["passed"] != true {
		t.Fatalf("缺席不是失败证据（显式 true 保留）")
	}
	// instance_report（按 case_id 取）
	got = EvaluationRequirementResults(map[string]any{
		"instance_report": map[string]any{
			"case1": map[string]any{
				"tests_status": map[string]any{
					"FAIL_TO_PASS": map[string]any{"success": []any{"t1"}, "failure": []any{"t2"}},
					"PASS_TO_PASS": map[string]any{"success": []any{"t3"}},
				},
			},
		},
	}, "case1")
	if len(got) != 3 {
		t.Fatalf("SWE legacy 应 3 条: %d", len(got))
	}
	if got[0]["group"] != FailToPassGroup || got[0]["passed"] != true || got[0]["score"].(float64) != 1.0 {
		t.Fatalf("f2p success: %v", got[0])
	}
	if got[1]["passed"] != false || got[1]["status"] != "failure" {
		t.Fatalf("f2p failure: %v", got[1])
	}
	if got[2]["group"] != PassToPassGroup {
		t.Fatalf("p2p: %v", got[2])
	}
	// 单键 instance_report 免 case_id
	got = EvaluationRequirementResults(map[string]any{
		"instance_report": map[string]any{
			"only": map[string]any{"tests_status": map[string]any{}},
		},
	}, "")
	if len(got) != 0 {
		t.Fatalf("空 tests_status 应空: %v", got)
	}
}

func TestNormalizeRequirementResults_身份与排序(t *testing.T) {
	got := NormalizeRequirementResults([]any{
		map[string]any{"name": "b", "group": "atomic_check"},
		map[string]any{"id": "a"},
		map[string]any{"requirement_id": "  "},
	})
	if len(got) != 2 {
		t.Fatalf("空 ID 跳过: %d", len(got))
	}
	// Python: sorted(by_identity) 按元组排序——atomic_check 组在前
	if got[0]["group"] != AtomicCheckGroup || got[0]["requirement_id"] != "b" {
		t.Fatalf("atomic_check 组应排前: %v", got[0])
	}
	if got[1]["group"] != RequirementGroup || got[1]["requirement_id"] != "a" {
		t.Fatalf("缺省组与 id 回退: %v", got[1])
	}
}

func TestRequirementResultsContract_版本(t *testing.T) {
	contract := RequirementResultsContract(nil)
	if contract["schema_version"] != RequirementResultsSchemaVersion {
		t.Fatalf("schema_version: %v", contract["schema_version"])
	}
}

func infFloat() float64 {
	inf := 1.0
	for i := 0; i < 1024; i++ {
		inf *= 2
	}
	return inf
}

func TestBaselineContexts_各类错误(t *testing.T) {
	dir := t.TempDir()
	// 造基线：嵌套 cases 布局 + 合法评分
	resultPath := filepath.Join(dir, "cases", "c1", "result.json")
	os.MkdirAll(filepath.Dir(resultPath), 0o755)
	os.WriteFile(resultPath, []byte(`{"case_id":"c1","score":0.5,"status":"passed","execution_status":"passed"}`), 0o644)
	tracePath := filepath.Join(dir, "cases", "c1", "trace.json")
	os.WriteFile(tracePath, []byte(`{"steps":[]}`), 0o644)
	evalRef := filepath.Join(dir, "eval_ref.yaml")
	os.WriteFile(evalRef, []byte("cases:\n  - case_id: c1\n    score: 0.5\n    status: passed\n    result_path: "+resultPath+"\n    trace_path: "+tracePath+"\n"), 0o644)
	cases := []map[string]any{{"case_id": "c1", "input": "task", "reference": map[string]any{}}}
	contexts, err := BaselineContexts(evalRef, cases)
	if err != nil {
		t.Fatalf("BaselineContexts: %v", err)
	}
	ctx := contexts["c1"]
	if ctx == nil || ctx.TaskIdentity == "" || ctx.EvidenceDigest == "" {
		t.Fatalf("上下文字段: %+v", ctx)
	}
	if ctx.Fingerprint() == "" {
		t.Fatalf("指纹应非空")
	}
	// validate 通过
	if _, err := ctx.Validate(cases[0]); err != nil {
		t.Fatalf("validate: %v", err)
	}
	// 用例身份变化 → validate 拒绝
	_, err = ctx.Validate(map[string]any{"case_id": "c1", "input": "changed"})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("身份变化应拒绝: %v", err)
	}
	// 缺失用例
	_, err = BaselineContexts(evalRef, []map[string]any{{"case_id": "c9"}})
	if err == nil || !strings.Contains(err.Error(), "does not cover case") {
		t.Fatalf("缺失用例应拒绝: %v", err)
	}
	// 重复用例身份
	dupRef := filepath.Join(dir, "dup.yaml")
	os.WriteFile(dupRef, []byte("cases:\n  - case_id: c1\n    result_path: "+resultPath+"\n  - case_id: c1\n    result_path: "+resultPath+"\n"), 0o644)
	_, err = BaselineContexts(dupRef, cases)
	if err == nil || !strings.Contains(err.Error(), "duplicate case identities") {
		t.Fatalf("重复身份应拒绝: %v", err)
	}
	// 非法评分（>1）——评分取自 result 文件
	badResult := filepath.Join(dir, "cases", "c1", "bad_result.json")
	os.WriteFile(badResult, []byte(`{"case_id":"c1","score":2.0,"status":"passed","execution_status":"passed"}`), 0o644)
	badRef := filepath.Join(dir, "bad.yaml")
	os.WriteFile(badRef, []byte("cases:\n  - case_id: c1\n    score: 2.0\n    status: passed\n    result_path: "+badResult+"\n"), 0o644)
	_, err = BaselineContexts(badRef, cases)
	if err == nil || !strings.Contains(err.Error(), "valid correctness grade") {
		t.Fatalf("越界评分应拒绝: %v", err)
	}
}

func TestCaseIdentity_稳定与内容敏感(t *testing.T) {
	caseA := map[string]any{"case_id": "c1", "input": "task a", "reference": map[string]any{}}
	id1, err := CaseIdentity(caseA)
	if err != nil {
		t.Fatalf("CaseIdentity: %v", err)
	}
	id2, _ := CaseIdentity(caseA)
	if id1 != id2 || len(id1) != 64 {
		t.Fatalf("身份应稳定 64 位: %s", id1)
	}
	caseB := map[string]any{"case_id": "c1", "input": "task b", "reference": map[string]any{}}
	id3, _ := CaseIdentity(caseB)
	if id1 == id3 {
		t.Fatalf("内容变化身份应变")
	}
}

func TestBaselineValidate_错误分支矩阵(t *testing.T) {
	dir := t.TempDir()
	// result 属于别的 case
	mismatchResult := filepath.Join(dir, "cases", "c1", "mismatch.json")
	os.MkdirAll(filepath.Dir(mismatchResult), 0o755)
	os.WriteFile(mismatchResult, []byte(`{"case_id":"other","score":0.5,"status":"passed"}`), 0o644)
	ctx := &BaselineJudgeContext{CaseID: "c1", ResultPath: mismatchResult, TaskIdentity: "x", EvidenceDigest: "x"}
	// evidence digest 不符先触发
	_, err := ctx.Validate(map[string]any{"case_id": "c1"})
	if err == nil {
		t.Fatalf("evidence 不符应拒绝")
	}
	// 修正 evidence：构造目录后取真实 digest
	normalResult := filepath.Join(dir, "cases", "c2", "result.json")
	os.MkdirAll(filepath.Dir(normalResult), 0o755)
	os.WriteFile(normalResult, []byte(`{"case_id":"c2","score":0.5,"status":"passed","execution_status":"passed"}`), 0o644)
	digest, err := evidenceDigest(filepath.Dir(normalResult))
	if err != nil {
		t.Fatalf("evidenceDigest: %v", err)
	}
	identity2, _ := CaseIdentity(map[string]any{"case_id": "c2", "input": "task", "reference": map[string]any{}})
	ctx2 := &BaselineJudgeContext{CaseID: "c1", ResultPath: normalResult, TaskIdentity: identity2, EvidenceDigest: digest}
	// 用例身份与任务身份一致但 result 属于别的 case → belongs to a different case
	_, err = ctx2.Validate(map[string]any{"case_id": "c1", "input": "task", "reference": map[string]any{}})
	// 先触发身份比对：validate 的 case_identity(c1)==TaskIdentity(c2) 不等 → does not match
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("任务身份不匹配应拒绝: %v", err)
	}
	// 用 c2 身份校验 → result 属于 c2 匹配 → 通过
	ctx2b := &BaselineJudgeContext{CaseID: "c2", ResultPath: normalResult, TaskIdentity: identity2, EvidenceDigest: digest}
	if _, err := ctx2b.Validate(map[string]any{"case_id": "c2", "input": "task", "reference": map[string]any{}}); err != nil {
		t.Fatalf("一致应通过: %v", err)
	}
	// execution_status error → 拒绝
	errResult := filepath.Join(dir, "cases", "c3", "result.json")
	os.MkdirAll(filepath.Dir(errResult), 0o755)
	os.WriteFile(errResult, []byte(`{"case_id":"c3","score":0.5,"status":"passed","execution_status":"error"}`), 0o644)
	digest3, _ := evidenceDigest(filepath.Dir(errResult))
	ctx3 := &BaselineJudgeContext{CaseID: "c3", ResultPath: errResult, EvidenceDigest: digest3}
	identity3, _ := CaseIdentity(map[string]any{"case_id": "c3", "input": "task", "reference": map[string]any{}})
	ctx3.TaskIdentity = identity3
	_, err = ctx3.Validate(map[string]any{"case_id": "c3", "input": "task", "reference": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "valid correctness grade") {
		t.Fatalf("execution error 应拒绝: %v", err)
	}
	// harvest artifact 缺失 → 拒绝
	missingArtifact := filepath.Join(dir, "cases", "c4", "result.json")
	os.MkdirAll(filepath.Dir(missingArtifact), 0o755)
	os.WriteFile(missingArtifact, []byte(`{"case_id":"c4","score":0.5,"status":"passed","execution_status":"passed","artifacts":{"harvested":["gone.txt"]}}`), 0o644)
	digest4, _ := evidenceDigest(filepath.Dir(missingArtifact))
	ctx4 := &BaselineJudgeContext{CaseID: "c4", ResultPath: missingArtifact, EvidenceDigest: digest4}
	identity4, _ := CaseIdentity(map[string]any{"case_id": "c4", "input": "task", "reference": map[string]any{}})
	ctx4.TaskIdentity = identity4
	_, err = ctx4.Validate(map[string]any{"case_id": "c4", "input": "task", "reference": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "deliverable is missing") {
		t.Fatalf("缺失交付物应拒绝: %v", err)
	}
	// 助手分支
	if evalToString(42) == "" {
		t.Fatalf("evalToString 数字")
	}
	if !evalAnyEqual(map[string]any{"a": 1}, map[string]any{"a": 1}) {
		t.Fatalf("evalAnyEqual")
	}
	if expandUserB("~/x") == "~/x" {
		t.Fatalf("应展开 ~")
	}
}

func TestAppendOrderedSteps_kind形态与排序(t *testing.T) {
	// kind+detail 单步直出
	step := map[string]any{"kind": "tool", "detail": map[string]any{"tool_name": "bash"}}
	steps := orderedSteps(step)
	if len(steps) != 1 {
		t.Fatalf("kind+detail 应直出: %d", len(steps))
	}
	// 嵌套 map 递归（键排序遍历）
	nested := map[string]any{
		"zz": map[string]any{"steps": []any{step}},
	}
	steps = orderedSteps(nested)
	if len(steps) != 1 {
		t.Fatalf("嵌套递归: %d", len(steps))
	}
	// messages 中非 dict 忽略
	msgs := map[string]any{"messages": []any{"skip", map[string]any{}}}
	if got := orderedSteps(msgs); len(got) != 0 {
		t.Fatalf("空 tool_calls 应无步: %d", len(got))
	}
	sortStrings([]string{"b", "a"})
}

func TestValidate_judgeManifest分支(t *testing.T) {
	dir := t.TempDir()
	caseDir := filepath.Join(dir, "cases", "c5")
	os.MkdirAll(filepath.Join(caseDir, "judge", "j1"), 0o755)
	os.MkdirAll(filepath.Join(caseDir, "artifacts"), 0o755)
	// artifacts 文件 + harvest 记录
	os.WriteFile(filepath.Join(caseDir, "artifacts", "out.txt"), []byte("data"), 0o644)
	// judge manifest 与实际文件一致
	manifest := map[string]any{"files": map[string]any{"out.txt": sha256OfFile(filepath.Join(caseDir, "artifacts", "out.txt"))}}
	manifestJSON, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(caseDir, "judge", "j1", "artifact_manifest.json"), manifestJSON, 0o644)
	result := map[string]any{
		"case_id": "c5", "score": 0.5, "status": "passed", "execution_status": "passed",
		"artifacts": map[string]any{"harvested": []any{"out.txt"}},
		"evaluation": map[string]any{
			"method":   "llm_as_judge",
			"metadata": map[string]any{"score_mode": "continuous", "judge_dir": "judge/j1"},
		},
	}
	resultJSON, _ := json.Marshal(result)
	resultPath := filepath.Join(caseDir, "result.json")
	os.WriteFile(resultPath, resultJSON, 0o644)
	// trace 文件入 digest
	os.WriteFile(filepath.Join(caseDir, "judge", "normalized_trace.json"), []byte(`{}`), 0o644)
	digest, err := evidenceDigest(caseDir)
	if err != nil {
		t.Fatalf("evidenceDigest: %v", err)
	}
	identity, _ := CaseIdentity(map[string]any{"case_id": "c5", "input": "task", "reference": map[string]any{}})
	ctx := &BaselineJudgeContext{CaseID: "c5", ResultPath: resultPath, TaskIdentity: identity, EvidenceDigest: digest}
	if _, err := ctx.Validate(map[string]any{"case_id": "c5", "input": "task", "reference": map[string]any{}}); err != nil {
		t.Fatalf("judge manifest 一致应通过: %v", err)
	}
	// manifest 与实际不符：先改文件再重算 evidence digest（绕过 digest 前置检查）
	os.WriteFile(filepath.Join(caseDir, "artifacts", "out.txt"), []byte("changed"), 0o644)
	digestChanged, _ := evidenceDigest(caseDir)
	ctxChanged := &BaselineJudgeContext{CaseID: "c5", ResultPath: resultPath,
		TaskIdentity: identity, EvidenceDigest: digestChanged}
	_, err = ctxChanged.Validate(map[string]any{"case_id": "c5", "input": "task", "reference": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "differ from the evidence") {
		t.Fatalf("manifest 不符应拒绝: %v", err)
	}
	// manifest 缺失：删除后重算 digest（evidence digest 不含 manifest——与实现一致则走 manifest 分支）
	os.Remove(filepath.Join(caseDir, "judge", "j1", "artifact_manifest.json"))
	digestNoManifest, _ := evidenceDigest(caseDir)
	ctxNoManifest := &BaselineJudgeContext{CaseID: "c5", ResultPath: resultPath,
		TaskIdentity: identity, EvidenceDigest: digestNoManifest}
	_, err = ctxNoManifest.Validate(map[string]any{"case_id": "c5", "input": "task", "reference": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "missing its original judged-artifact manifest") {
		t.Fatalf("manifest 缺失应拒绝: %v", err)
	}
	// TrajectoryGlobJSONL
	os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("{}\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	got := TrajectoryGlobJSONL(dir)
	if len(got) != 1 {
		t.Fatalf("glob: %v", got)
	}
	if got := TrajectoryGlobJSONL(filepath.Join(dir, "missing")); got != nil {
		t.Fatalf("缺失目录应 nil: %v", got)
	}
	if !trajectoryFileExists(filepath.Join(dir, "a.jsonl")) || trajectoryFileExists(filepath.Join(dir, "missing")) {
		t.Fatalf("trajectoryFileExists")
	}
	// boolPtrToAny / GetCallTimeout
	if boolPtrToAny(nil) != nil || boolPtrToAny(boolPtr(true)) != true {
		t.Fatalf("boolPtrToAny")
	}
}

func boolPtr(v bool) *bool { return &v }

func sha256OfFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
