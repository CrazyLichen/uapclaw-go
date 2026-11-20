package singleharness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
)

// TestReadableTrace 轨迹解码逐行化。
func TestReadableTrace(t *testing.T) {
	trace := `{"a":{"b":"text"},"c":[1,"x"],"d":null,"e":true}`
	got := ReadableTrace(trace)
	want := "$.a.b: text\n$.c[0]: 1\n$.c[1]: x\n$.d: null\n$.e: true"
	if got != want {
		t.Fatalf("轨迹解码错误:\n%s\nwant:\n%s", got, want)
	}
	// 非 JSON 原样
	if got := ReadableTrace("not-json"); got != "not-json" {
		t.Fatalf("非 JSON 应原样: %s", got)
	}
}

// TestValidateObservation 观察校验全分支。
func TestValidateObservation(t *testing.T) {
	evidence := map[string]any{
		"source":    map[string]any{"files": map[string]any{"task": "task text", "impl.py": "line one\nline two"}},
		"candidate": map[string]any{"files": map[string]any{"impl.py": "line one changed\nline two"}},
	}
	// 非 mapping → 全 unknown
	result := ValidateObservation(map[string]any{}, evidence)
	if result["source_check"] != "unknown" || result["candidate_check"] != "unknown" || result["behavior_changed"] != "unknown" {
		t.Fatalf("空观察应全 unknown: %v", result)
	}
	if len(result["evidence"].([]any)) != 0 {
		t.Fatal("空观察证据应为空")
	}
	// 合法引用 + 判定
	raw := map[string]any{
		"source_check":     "no",
		"candidate_check":  "yes",
		"behavior_changed": "yes",
		"evidence": []any{
			map[string]any{"side": "source", "file": "impl.py", "quote": "line one"},
			map[string]any{"side": "candidate", "file": "impl.py", "quote": "line one changed"},
			// 无效项：side 非法/引用过短/file=task/引文不存在且不唯一
			map[string]any{"side": "other", "file": "impl.py", "quote": "line one"},
			map[string]any{"side": "source", "file": "impl.py", "quote": "tiny"},
			map[string]any{"side": "source", "file": "task", "quote": "task text"},
			map[string]any{"side": "source", "file": "nope.py", "quote": "absent quote here"},
		},
	}
	result = ValidateObservation(raw, evidence)
	items := result["evidence"].([]any)
	if len(items) != 2 {
		t.Fatalf("应仅保留 2 条有效引用: %v", items)
	}
	if result["source_check"] != "no" || result["candidate_check"] != "yes" || result["behavior_changed"] != "yes" {
		t.Fatalf("判定应透传: %v", result)
	}
	// 唯一引用修复
	repair := map[string]any{
		"source_check": "yes", "candidate_check": "yes", "behavior_changed": "no",
		"evidence": []any{
			map[string]any{"side": "source", "file": "wrong.py", "quote": "line one\nline two"},
		},
	}
	result = ValidateObservation(repair, evidence)
	items = result["evidence"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["file"] != "impl.py" {
		t.Fatalf("唯一引用应被修复: %v", items)
	}
	// 双侧同文件 → behavior_changed=no 且判定归 unknown
	sameEvidence := map[string]any{
		"source":    map[string]any{"files": map[string]any{"impl.py": "same"}},
		"candidate": map[string]any{"files": map[string]any{"impl.py": "same"}},
	}
	conflict := map[string]any{
		"source_check": "yes", "candidate_check": "no", "behavior_changed": "yes",
		"evidence": []any{},
	}
	result = ValidateObservation(conflict, sameEvidence)
	if result["behavior_changed"] != "no" || result["source_check"] != "unknown" || result["candidate_check"] != "unknown" {
		t.Fatalf("同证据应归一: %v", result)
	}
	// 仅单侧有判定 → behavior_changed 保持 unknown
	oneSided := map[string]any{
		"source_check": "yes", "candidate_check": "unknown", "behavior_changed": "yes",
		"evidence": []any{map[string]any{"side": "source", "file": "impl.py", "quote": "line one"}},
	}
	result = ValidateObservation(oneSided, evidence)
	if result["behavior_changed"] != "unknown" {
		t.Fatalf("单侧不应确认行为变化: %v", result)
	}
}

// TestObservationErrors 观察错误清单。
func TestObservationErrors(t *testing.T) {
	checked := map[string]any{"source_check": "unknown", "candidate_check": "unknown", "behavior_changed": "unknown"}
	// 全缺字段 + evidence 非列表
	raw := map[string]any{}
	errs := observationErrors(raw, checked)
	if len(errs) != 4 {
		t.Fatalf("应报 4 个错误: %v", errs)
	}
	// 已判定但缺证据
	raw = map[string]any{"source_check": "yes", "candidate_check": "unknown", "behavior_changed": "unknown", "evidence": []any{}}
	errs = observationErrors(raw, checked)
	if len(errs) != 1 || len(errs[0]) == 0 {
		t.Fatalf("应报证据缺失: %v", errs)
	}
	// 全合法
	raw = map[string]any{"source_check": "unknown", "candidate_check": "unknown", "behavior_changed": "unknown", "evidence": []any{}}
	if errs := observationErrors(raw, checked); len(errs) != 0 {
		t.Fatalf("全 unknown 应无错误: %v", errs)
	}
	// evidence 列表含非 mapping（schema 错误与引用错误并列）
	raw = map[string]any{"source_check": "unknown", "candidate_check": "unknown", "behavior_changed": "unknown", "evidence": []any{"bad"}}
	if errs := observationErrors(raw, checked); len(errs) != 1 {
		t.Fatalf("非 mapping 引用应报错: %v", errs)
	}
}

// TestFeedbackRoute 反馈路由全分支。
func TestFeedbackRoute(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		observation map[string]any
		taskPassed  bool
		score       *bool
		want        string
	}{
		{map[string]any{"availability": "no"}, false, nil, "repair_activation"},
		{map[string]any{"source_check": "no", "candidate_check": "yes"}, true, nil, "verified"},
		{map[string]any{"source_check": "no", "candidate_check": "yes"}, false, nil, "investigate_residuals"},
		{map[string]any{"source_check": "no", "candidate_check": "yes"}, false, &yes, "verified"},
		{map[string]any{"source_check": "no", "candidate_check": "yes"}, true, &no, "investigate_residuals"},
		{map[string]any{"behavior_changed": "no", "availability": "yes"}, false, nil, "repair_execution"},
		{map[string]any{"behavior_changed": "no"}, false, nil, "collect_behavior_evidence"},
		{map[string]any{"behavior_changed": "yes", "candidate_check": "no"}, false, nil, "revise_implementation_or_cause"},
		{map[string]any{}, false, nil, "collect_behavior_evidence"},
	}
	for i, c := range cases {
		if got := FeedbackRoute(c.observation, c.taskPassed, c.score); got != c.want {
			t.Fatalf("case %d: got %s want %s", i, got, c.want)
		}
	}
}

// TestBehaviorEvidence 证据收集：缺失与可读路径。
func TestBehaviorEvidence(t *testing.T) {
	dir := t.TempDir()
	// 无 cases 目录 → 证据不可读/缺失
	missing := BehaviorEvidence(filepath.Join(dir, "eval.yaml"), "c1")
	if missing["files"] == nil {
		t.Fatal("files 应存在（空映射）")
	}
	if omitted := missing["omitted"].([]string); len(omitted) == 0 {
		t.Fatalf("缺失应登记 omitted: %v", missing)
	}
	// 造一个案例目录（CaseReader 布局）：cases/c1/result.json
	casesDir := filepath.Join(dir, "cases", "c1")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "result.json"), []byte(
		`{"case_id":"c1","status":"passed","score":1.0,
		  "result_path":"`+casesDir+`/result.json","trace_path":"","evaluation":{"method":"rubric","metadata":{"model_patch_path":""}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "trace.json"), []byte(`{"input":"the task","response":"the answer"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "runtime_config.json"), []byte(`{"max":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(casesDir, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "artifacts", "note.md"), []byte("artifact body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "artifacts", "bin.dat"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	// judge/normalized_trace.json → 字段化文本
	judgeDir := filepath.Join(casesDir, "judge")
	if err := os.MkdirAll(judgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(judgeDir, "normalized_trace.json"), []byte(`{"step":"ran"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	evalRef := filepath.Join(dir, "eval.yaml")
	evidence := BehaviorEvidence(evalRef, "c1")
	files := evidence["files"].(map[string]any)
	if files["task"] != "the task" || files["response"] != "the answer" {
		t.Fatalf("task/response 应入证据: %v", files)
	}
	if _, present := files["runtime_config.json"]; !present {
		t.Fatalf("runtime_config 应入证据: %v", files)
	}
	if _, present := files["artifacts/note.md"]; !present {
		t.Fatalf("文本 artifact 应入证据: %v", files)
	}
	if _, present := files["judge/normalized_trace.fields.txt"]; !present {
		t.Fatalf("轨迹应字段化: %v", files)
	}
	if omitted := evidence["omitted"].([]string); len(omitted) == 0 || omitted[0] != "artifacts/bin.dat" {
		t.Fatalf("二进制 artifact 应登记 omitted: %v", omitted)
	}
}

// TestCheckCandidateBehaviorCache 无模型调用路径：契约聚合/结果落盘/字段注入。
func TestCheckCandidateBehaviorCache(t *testing.T) {
	dir := t.TempDir()
	for _, side := range []string{"src", "cand"} {
		caseDir := filepath.Join(dir, side, "cases", "c1")
		if err := os.MkdirAll(caseDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(caseDir, "result.json"), []byte(
			`{"case_id":"c1","status":"passed","score":1.0,"evaluation":{"method":"rubric","metadata":{}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(caseDir, "trace.json"), []byte(`{"input":"task","response":"answer"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	capabilities := []map[string]any{{
		"decision_contracts": []any{
			map[string]any{"expectation": "writes file"},
			"not-a-mapping",
			map[string]any{"expectation": "writes file"},
		},
		"target_case_ids": []any{"c1", "c1", "c2"},
	}}
	results, err := CheckCandidateBehavior(context.Background(),
		filepath.Join(dir, "src", "eval.yaml"), filepath.Join(dir, "cand", "eval.yaml"),
		capabilities, "", filepath.Join(dir, "out"), nil)
	if err != nil {
		t.Fatalf("核查失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("c1/c2 都应有观察: %v", results)
	}
	c1 := results["c1"]
	if len(c1["checks"].([]any)) != 1 {
		t.Fatalf("重复契约应去重: %v", c1["checks"])
	}
	if c1["evidence_sha256"] == "" || c1["source_eval_ref_path"] == "" || c1["candidate_eval_ref_path"] == "" {
		t.Fatalf("字段注入缺失: %v", c1)
	}
	if _, err := os.Stat(filepath.Join(dir, "out", c1["evidence_sha256"].(string)+".json")); err != nil {
		t.Fatal("结果应落盘")
	}
	if _, err := os.Stat(filepath.Join(dir, "out", c1["evidence_sha256"].(string)+".input.json")); err != nil {
		t.Fatal("输入应落盘")
	}
	// OutputRepairConfig 覆盖
	cfg := harnessrsi.DefaultOutputRepairConfig()
	if _, err := CheckCandidateBehavior(context.Background(), filepath.Join(dir, "src", "eval.yaml"), filepath.Join(dir, "cand", "eval.yaml"),
		capabilities, "", filepath.Join(dir, "out2"), &cfg); err != nil {
		t.Fatalf("显式配置应通过: %v", err)
	}
}

// TestBehaviorCheckHelpers 助手补全。
func TestBehaviorCheckHelpers(t *testing.T) {
	if !allChecksObservable([]map[string]any{{"acceptance_observable": true}}) {
		t.Fatal("全可观察应通过")
	}
	if allChecksObservable([]map[string]any{{"acceptance_observable": false}}) {
		t.Fatal("任一不可观察应拒绝")
	}
	if got := string(truncateBytes([]byte("abc日本語def"), 5)); got == "" {
		t.Fatal("截断不应为空")
	}
	if !utf8ValidTail('a') {
		t.Fatal("ASCII 尾应合法")
	}
	if normalizedStringMap(map[string]any{"a": 1})["a"] != 1 {
		t.Fatal("透传失败")
	}
	items := interfaceSlice([]map[string]any{{"a": 1}})
	if len(items) != 1 {
		t.Fatal("切片转换失败")
	}
	if errorTypeName(errFake{}) == "" {
		t.Fatal("类型名不应为空")
	}
	if assistantText(nil) != "" {
		t.Fatal("nil 响应应为空")
	}
	if got := canonicalJSONString(map[string]any{"b": 1, "a": 2}); got != `{"a":2,"b":1}` {
		t.Fatalf("规范 JSON 错误: %s", got)
	}
}

// errFake 测试错误类型。
type errFake struct{}

// Error 实现 error。
func (errFake) Error() string { return "fake" }
