package singleharness

import (
	"os"
	"path/filepath"
	"testing"

	sourceEvidence "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/source_evidence"
)

// TestEvaluationContext 评估上下文：完整签名与缺件空签名。
func TestEvaluationContext(t *testing.T) {
	dir := t.TempDir()
	harness := filepath.Join(dir, "harness")
	if err := os.MkdirAll(harness, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harness, "m.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := filepath.Join(dir, "refs.yaml")
	if err := os.WriteFile(refs, []byte("harness_refs:\n  solver: "+harness+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	casePath := filepath.Join(dir, "case1")
	if err := os.MkdirAll(casePath, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{{"case_id": "c1", "case_path": filepath.Join(casePath, "case.json")}}
	context := EvaluationContext(refs, map[string]any{"protocol": "llm"}, cases)
	if context["signature"] == "" {
		t.Fatal("完整 harness 应有签名")
	}
	caseDigests, _ := context["cases"].(map[string]any)
	if caseDigests["c1"] == "" {
		t.Fatalf("用例摘要应生成: %v", caseDigests)
	}
	// 缺件 → 空签名
	badRefs := filepath.Join(dir, "bad_refs.yaml")
	if err := os.WriteFile(badRefs, []byte("harness_refs:\n  solver: missing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badContext := EvaluationContext(badRefs, map[string]any{}, cases)
	if badContext["signature"] != "" {
		t.Fatal("缺件应空签名")
	}
}

// TestStampEvaluationAndMatching 戳记与匹配。
func TestStampEvaluationAndMatching(t *testing.T) {
	dir := t.TempDir()
	harness := filepath.Join(dir, "harness")
	if err := os.MkdirAll(harness, 0o755); err != nil {
		t.Fatal(err)
	}
	refs := filepath.Join(dir, "refs.yaml")
	if err := os.WriteFile(refs, []byte("harness_refs:\n  solver: "+harness+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(dir, "r1.json")
	if err := os.WriteFile(resultPath, []byte(`{"status":"passed","execution_status":"ok","evaluation":{"method":"rubric"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tracePath := filepath.Join(dir, "t1.json")
	if err := os.WriteFile(tracePath, []byte(`{"spans":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	casePath := filepath.Join(dir, "case1")
	if err := os.MkdirAll(casePath, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{{"case_id": "c1", "case_path": filepath.Join(casePath, "case.json")}}
	context := EvaluationContext(refs, map[string]any{}, cases)

	// 评估文件缺 cases 键也应可戳记
	evalPath := filepath.Join(dir, "eval.yaml")
	if err := os.WriteFile(evalPath, []byte("cases:\n  - case_id: c1\n    status: passed\n    score: 0.9\n    result_path: "+resultPath+"\n    trace_path: "+tracePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StampEvaluation(evalPath, context); err != nil {
		t.Fatalf("戳记失败: %v", err)
	}
	// 匹配成功
	selected := MatchingCases([]string{evalPath}, context)
	if _, ok := selected["c1"]; !ok {
		t.Fatalf("应匹配到 c1: %v", selected)
	}
	// 空签名不匹配
	if got := MatchingCases([]string{evalPath}, map[string]any{"signature": "", "cases": map[string]any{}}); len(got) != 0 {
		t.Fatal("空签名应返回空")
	}
	// 签名不匹配
	otherContext := map[string]any{"signature": "other", "cases": map[string]any{"c1": "x"}}
	if got := MatchingCases([]string{evalPath}, otherContext); len(got) != 0 {
		t.Fatal("签名不一致应返回空")
	}
	// 用例摘要不匹配
	digestMismatch := map[string]any{"signature": context["signature"], "cases": map[string]any{"c1": "wrong"}}
	if got := MatchingCases([]string{evalPath}, digestMismatch); len(got) != 0 {
		t.Fatal("用例摘要不一致应返回空")
	}
	// 状态缺失/结果错误/skip 分支
	evalPath2 := filepath.Join(dir, "eval2.yaml")
	if err := os.WriteFile(evalPath2, []byte("cases:\n  - case_id: c1\n    status: error\n    score: 0.9\n    result_path: "+resultPath+"\n    trace_path: "+tracePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StampEvaluation(evalPath2, context); err != nil {
		t.Fatal(err)
	}
	if got := MatchingCases([]string{evalPath2}, context); len(got) != 0 {
		t.Fatal("error 状态不应作为证据")
	}
	// 缺 result 文件
	evalPath3 := filepath.Join(dir, "eval3.yaml")
	if err := os.WriteFile(evalPath3, []byte("cases:\n  - case_id: c1\n    status: passed\n    score: 0.9\n    result_path: missing.json\n    trace_path: "+tracePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StampEvaluation(evalPath3, context); err != nil {
		t.Fatal(err)
	}
	if got := MatchingCases([]string{evalPath3}, context); len(got) != 0 {
		t.Fatal("缺 result 文件不应作为证据")
	}
}

// TestMaterializeSource 证据物化：复制/引用重写/来源登记。
func TestMaterializeSource(t *testing.T) {
	dir := t.TempDir()
	// 造源评估目录
	sourceEval := filepath.Join(dir, "src_eval")
	if err := os.MkdirAll(sourceEval, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceEval, "r1.json"), []byte(`{"status":"passed"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceEval, "t1.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	evalPath := filepath.Join(dir, "eval.yaml")
	if err := os.WriteFile(evalPath, []byte("dataset:\n  name: demo\ncases:\n  - case_id: c1\n    status: passed\n    score: 0.9\n    result_path: "+filepath.Join(sourceEval, "r1.json")+"\n    trace_path: "+filepath.Join(sourceEval, "t1.json")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	context := map[string]any{"signature": "sig", "cases": map[string]any{"c1": "d1"}}
	cases := []map[string]any{{"case_id": "c1"}}
	selected := map[string]matchedCase{
		"c1": {Path: evalPath, Case: map[string]any{
			"case_id": "c1", "status": "passed", "score": 0.9,
			"result_path": filepath.Join(sourceEval, "r1.json"),
			"trace_path":  filepath.Join(sourceEval, "t1.json"),
		}},
	}
	outputDir := filepath.Join(dir, "batch")
	out, err := MaterializeSource(cases, selected, outputDir, "refs.yaml", context, map[string]bool{"c1": true})
	if err != nil {
		t.Fatalf("物化失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "cases", "c001", "r1.json")); err != nil {
		t.Fatal("证据应被复制")
	}
	payload := sourceEvidence.ReadEvidenceMapping(out)
	if payload["eval_id"] == "" || payload["harness_refs_path"] != "refs.yaml" {
		t.Fatalf("payload 字段缺失: %v", payload)
	}
	evidencePayload, _ := payload["source_evidence"].(map[string]any)
	reused := anySlice(evidencePayload["reused_case_ids"])
	if len(reused) != 1 || reused[0] != "c1" {
		t.Fatalf("复用清单错误: %v", evidencePayload["reused_case_ids"])
	}
	evaluated := anySlice(evidencePayload["evaluated_case_ids"])
	if len(evaluated) != 0 {
		t.Fatalf("评估清单应为空: %v", evaluated)
	}
	// 未复用路径
	out2, err := MaterializeSource(cases, selected, filepath.Join(dir, "batch2"), "refs.yaml", context, map[string]bool{})
	if err != nil {
		t.Fatalf("物化失败: %v", err)
	}
	payload2 := sourceEvidence.ReadEvidenceMapping(out2)
	sourceEvidence2, _ := payload2["source_evidence"].(map[string]any)
	evaluated2 := anySlice(sourceEvidence2["evaluated_case_ids"])
	if len(evaluated2) != 1 || evaluated2[0] != "c1" {
		t.Fatalf("未复用应进评估清单: %v", evaluated2)
	}
	// 未选中的用例报错
	if _, err := MaterializeSource([]map[string]any{{"case_id": "cx"}}, selected, filepath.Join(dir, "batch3"), "r", context, nil); err == nil {
		t.Fatal("未选用例应报错")
	}
}

// anySlice 断言 []string 或 []any 为字符串切片。
func anySlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, _ := item.(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}
