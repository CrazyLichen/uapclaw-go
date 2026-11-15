package evaluationresultanalyzer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCaseReader_ReadEvalRef 对照 Python read_eval_ref。
// Python: case_reader.py:59-70
func TestCaseReader_ReadEvalRef(t *testing.T) {
	dir := t.TempDir()
	reader := NewCaseReader()

	// ① 正常读取
	path := filepath.Join(dir, "eval_ref.yaml")
	if err := os.WriteFile(path, []byte("eval_id: e1\ncases: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := reader.ReadEvalRef(path)
	if err != nil {
		t.Fatal(err)
	}
	if data["eval_id"] != "e1" {
		t.Fatalf("内容不符: %v", data)
	}

	// ② 路径不存在 → ValueError 等价文案
	missing := filepath.Join(dir, "missing.yaml")
	_, err = reader.ReadEvalRef(missing)
	if err == nil || err.Error() != "eval_ref path not found: "+missing {
		t.Fatalf("错误文案不符: %v", err)
	}

	// ③ 空文件 → 空 mapping
	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	data2, err := reader.ReadEvalRef(empty)
	if err != nil || len(data2) != 0 {
		t.Fatalf("空文件应得空 mapping: %v %v", data2, err)
	}
}

// TestCaseReader_ReadSummary_G4字段映射 对照 Python read_summary。
// Python: case_reader.py:72-101
func TestCaseReader_ReadSummary_G4字段映射(t *testing.T) {
	dir := t.TempDir()
	reader := NewCaseReader()

	// ① 空路径 → 零值
	if got := reader.ReadSummary(""); got != (EvaluationSummaryInput{}) {
		t.Fatalf("空路径应返回零值, got %+v", got)
	}

	// ② 文件不存在 → 零值
	if got := reader.ReadSummary(filepath.Join(dir, "no.json")); got != (EvaluationSummaryInput{}) {
		t.Fatalf("缺失文件应返回零值, got %+v", got)
	}

	// ③ G4 映射：passed_cases/failed_cases 优先，passed_count/failed_count 回落
	path := filepath.Join(dir, "summary.json")
	if err := os.WriteFile(path, []byte(`{
		"total_cases": 10,
		"passed_cases": 7,
		"failed_cases": 3,
		"average_score": 0.85,
		"evaluation_method": "exact_match"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := reader.ReadSummary(path)
	if got.TotalCases != 10 || got.PassedCount != 7 || got.FailedCount != 3 ||
		got.AverageScore != 0.85 || got.EvaluationMethod != "exact_match" {
		t.Fatalf("G4 映射不符: %+v", got)
	}

	// ④ 回落键名 passed_count/failed_count
	path2 := filepath.Join(dir, "summary2.json")
	if err := os.WriteFile(path2, []byte(`{"passed_count": 5, "failed_count": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := reader.ReadSummary(path2)
	if got2.PassedCount != 5 || got2.FailedCount != 2 {
		t.Fatalf("回落键名映射不符: %+v", got2)
	}
}

// TestCaseReader_ReadCaseInputs_扫描与G3对照 对照 Python read_case_inputs。
// Python: case_reader.py:103-184
func TestCaseReader_ReadCaseInputs_扫描与G3对照(t *testing.T) {
	dir := t.TempDir()
	reader := NewCaseReader()

	// ① 目录不存在 → 空列表
	if got := reader.ReadCaseInputs(filepath.Join(dir, "nope")); len(got) != 0 {
		t.Fatalf("目录不存在应返回空, got %v", got)
	}

	// ② 正常扫描：两个案例目录（排序按目录名），result.json + trace.json
	for _, name := range []string{"case-b", "case-a"} {
		caseDir := filepath.Join(dir, name)
		if err := os.Mkdir(caseDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	resultA := `{
		"case_id": "c1",
		"status": "failed",
		"score": 0.5,
		"error": "boom",
		"evaluation": {"method": "llm_judge", "passed": true, "reason": "r1",
			"metadata": {"m": 1}},
		"metadata": {"training_signal": {"s": 1}, "execution": {"iters": 2}}
	}`
	traceA := `{"input": "task text", "response": "answer text",
		"behavior_trace": {"trajectory_window_summary": {"window_size": 5, "event_count": 3}}}`
	resultB := `{"status": "passed", "result": "fallback-response",
		"evaluation": {"passed": false, "reason": "r2"}}`
	if err := os.WriteFile(filepath.Join(dir, "case-a", "result.json"), []byte(resultA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "case-a", "trace.json"), []byte(traceA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "case-b", "result.json"), []byte(resultB), 0o644); err != nil {
		t.Fatal(err)
	}

	got := reader.ReadCaseInputs(dir)
	if len(got) != 2 {
		t.Fatalf("应读到 2 案例, got %d", len(got))
	}
	// 排序按目录名：case-a 在前
	a, b := got[0], got[1]
	if a.CaseID != "c1" || a.Status != "failed" || a.Score != 0.5 || a.Error != "boom" {
		t.Fatalf("案例A 字段不符: %+v", a)
	}
	if a.Input != "task text" || a.Response != "answer text" {
		t.Fatalf("G3 input/response 不符: %q %q", a.Input, a.Response)
	}
	if a.EvaluationMethod != "llm_judge" || !a.EvaluationPassed || a.EvaluationReason != "r1" {
		t.Fatalf("评估字段不符: %+v", a)
	}
	if a.EvaluationMetadata["m"] != float64(1) {
		t.Fatalf("评估元数据不符: %v", a.EvaluationMetadata)
	}
	if a.TrainingSignal["s"] != float64(1) {
		t.Fatalf("训练信号不符: %v", a.TrainingSignal)
	}
	if a.ExecutionMetadata["iters"] != float64(2) {
		t.Fatalf("执行元数据不符: %v", a.ExecutionMetadata)
	}
	if a.TrajectoryWindowSummary["window_size"] != float64(5) {
		t.Fatalf("轨迹窗口摘要不符: %v", a.TrajectoryWindowSummary)
	}
	if a.NormalizedTraceSummary == nil {
		t.Fatal("归一化摘要应非 nil（缺 judge/normalized_trace.json 时为空 map 语义等同）")
	}
	if a.TerminationReason != "" {
		t.Fatalf("无终止原因应为空串, got %q", a.TerminationReason)
	}
	if a.TracePath != filepath.Join(dir, "case-a", "trace.json") ||
		a.ResultPath != filepath.Join(dir, "case-a", "result.json") {
		t.Fatalf("路径字段不符: %s %s", a.TracePath, a.ResultPath)
	}
	// case-b：无 trace.json → input 空、response 回落 result
	if b.Input != "" || b.Response != "fallback-response" {
		t.Fatalf("案例B G3 回落不符: %q %q", b.Input, b.Response)
	}
	if b.CaseID != "case-b" {
		t.Fatalf("缺 case_id 应回落目录名, got %q", b.CaseID)
	}
	if b.Expected != nil {
		t.Fatal("Python 字面量 expected=None，Go 应为 nil")
	}
}

// TestCaseReader_ReadCaseInputs_终止原因对照 对照 Python _read_termination_reason。
// Python: case_reader.py:416-421
func TestCaseReader_ReadCaseInputs_终止原因对照(t *testing.T) {
	dir := t.TempDir()
	reader := NewCaseReader()
	caseDir := filepath.Join(dir, "case-t")
	if err := os.Mkdir(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// trace.response.termination_reason 优先
	if err := os.WriteFile(filepath.Join(caseDir, "result.json"),
		[]byte(`{"result": {"termination_reason": "from-result"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "trace.json"),
		[]byte(`{"response": {"termination_reason": "from-trace"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := reader.ReadCaseInputs(dir)
	if len(got) != 1 || got[0].TerminationReason != "from-trace" {
		t.Fatalf("trace 优先于 result: %+v", got)
	}

	// metadata.termination_reason 兜底
	if err := os.WriteFile(filepath.Join(caseDir, "trace.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "result.json"),
		[]byte(`{"metadata": {"termination_reason": "from-metadata"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := reader.ReadCaseInputs(dir)
	if len(got2) != 1 || got2[0].TerminationReason != "from-metadata" {
		t.Fatalf("metadata 兜底不符: %+v", got2)
	}
}

// TestCaseReader_ReadCaseInputs_基准契约 对照 Python _read_benchmark_test_contract。
// Python: case_reader.py:187-247
func TestCaseReader_ReadCaseInputs_基准契约(t *testing.T) {
	dir := t.TempDir()
	reader := NewCaseReader()
	caseDir := filepath.Join(dir, "case-k")
	if err := os.Mkdir(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 数据集文件：cases 列表含 verification_contract
	datasetPath := filepath.Join(dir, "dataset.json")
	if err := os.WriteFile(datasetPath, []byte(`{"cases": [
		{"case_id": "k1", "verification_contract": {
			"fail_to_pass": ["t1", "t2"],
			"pass_to_pass": ["t3"],
			"test_patch": "diff --git a"
		}},
		{"case_id": "k2"}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "result.json"),
		[]byte(`{"case_id": "k1", "metadata": {"case_path": "`+datasetPath+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := reader.ReadCaseInputs(dir)
	if len(got) != 1 {
		t.Fatalf("应读到 1 案例, got %d", len(got))
	}
	contract := got[0].BenchmarkTestContract
	if contract["provenance"] != datasetPath+"#case_id=k1.verification_contract" {
		t.Fatalf("provenance 不符: %v", contract["provenance"])
	}
	f2p := contract["fail_to_pass"].([]string)
	if len(f2p) != 2 || f2p[0] != "t1" || f2p[1] != "t2" {
		t.Fatalf("fail_to_pass 不符: %v", f2p)
	}
	if contract["test_patch"] != "diff --git a" {
		t.Fatalf("test_patch 不符: %v", contract["test_patch"])
	}

	// 无契约案例（k2）→ 空 contract；case_path 缺失 → 空
	caseDir2 := filepath.Join(dir, "case-k2b")
	if err := os.Mkdir(caseDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir2, "result.json"),
		[]byte(`{"case_id": "k2", "metadata": {"case_path": "`+datasetPath+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := reader.ReadCaseInputs(dir)
	if len(got2) != 2 {
		t.Fatalf("应读到 2 案例, got %d", len(got2))
	}
	for _, c := range got2 {
		if c.CaseID == "k2" && len(c.BenchmarkTestContract) != 0 {
			t.Fatalf("k2 无契约应为空, got %v", c.BenchmarkTestContract)
		}
	}
}

// TestCaseReader_ReadCaseInputs_数据集ID列表解码 对照 Python _test_id_list 的
// JSON 字符串解码分支与 must_pass/regression 回落键。
// Python: case_reader.py:217-219, 250-259
func TestCaseReader_ReadCaseInputs_数据集ID列表解码(t *testing.T) {
	dir := t.TempDir()
	reader := NewCaseReader()
	caseDir := filepath.Join(dir, "case-j")
	if err := os.Mkdir(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	datasetPath := filepath.Join(dir, "dataset.json")
	// fail_to_pass 为 JSON 字符串；仅 must_pass/regression 键
	if err := os.WriteFile(datasetPath, []byte(`{"cases": [
		{"instance_id": "j1", "verification_contract": {
			"must_pass": "[\"tA\", \"tB\"]",
			"regression": "tC"
		}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "result.json"),
		[]byte(`{"case_id": "j1", "metadata": {"case_path": "`+datasetPath+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := reader.ReadCaseInputs(dir)
	contract := got[0].BenchmarkTestContract
	// JSON 字符串解码为清单
	f2p, _ := contract["fail_to_pass"].([]string)
	if len(f2p) != 2 || f2p[0] != "tA" || f2p[1] != "tB" {
		t.Fatalf("JSON 字符串应解码为清单, got %v", contract["fail_to_pass"])
	}
	// 单字符串包装为单元素
	p2p, _ := contract["pass_to_pass"].([]string)
	if len(p2p) != 1 || p2p[0] != "tC" {
		t.Fatalf("单字符串应包装为清单, got %v", contract["pass_to_pass"])
	}
	// instance_id 回落为 case_id
	if got[0].CaseID != "j1" {
		t.Fatalf("instance_id 应回落, got %q", got[0].CaseID)
	}
}

// TestProjectExecutionHistory 对照 Python project_execution_history。
// Python: case_reader.py:385-413
func TestProjectExecutionHistory(t *testing.T) {
	traceData := map[string]any{
		"traces": []any{
			map[string]any{
				"trace_id":    "tr1",
				"member_role": "planner",
				"messages": []any{
					map[string]any{
						"role": "system", "content": "sys",
						"message_index": 0, "step_pointer": "",
					},
					map[string]any{
						"role": "user", "content": "hello",
						"message_index": 1, "step_pointer": "s1", "extra": "dropped",
						"tool_calls": []any{
							map[string]any{
								"name": "bash", "input": "ls", "output": "ok",
								"error": "", "step_pointer": "s1", "hidden": 1,
							},
							"not-a-dict",
						},
					},
				},
			},
			map[string]any{"trace_id": "tr2"}, // 无 messages → 跳过
			"not-a-dict",                      // 非 dict → 跳过
		},
	}
	got := ProjectExecutionHistory(traceData)
	traces := got["traces"].([]map[string]any)
	if len(traces) != 1 {
		t.Fatalf("应保留 1 条轨迹, got %d", len(traces))
	}
	if traces[0]["trace_id"] != "tr1" || traces[0]["role"] != "planner" {
		t.Fatalf("轨迹字段不符: %v", traces[0])
	}
	messages := traces[0]["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("system 角色应被剔除, got %d", len(messages))
	}
	msg := messages[0]
	if _, ok := msg["extra"]; ok {
		t.Fatal("message_fields 白名单外的键应被剔除")
	}
	if msg["content"] != "hello" || msg["message_index"] != 1 || msg["step_pointer"] != "s1" {
		t.Fatalf("白名单字段应保留: %v", msg)
	}
	calls := msg["tool_calls"].([]map[string]any)
	if len(calls) != 1 {
		t.Fatalf("非 dict call 应剔除, got %d", len(calls))
	}
	if _, ok := calls[0]["hidden"]; ok {
		t.Fatal("call 白名单外的键应被剔除")
	}
	if calls[0]["name"] != "bash" || calls[0]["input"] != "ls" {
		t.Fatalf("call 字段不符: %v", calls[0])
	}

	// trace.role 回落：无 member_role 时用 role（无 messages 的轨迹会被剔除，Python 同）
	got2 := ProjectExecutionHistory(map[string]any{
		"traces": []any{map[string]any{
			"trace_id": "x", "role": "fallback",
			"messages": []any{map[string]any{"role": "user", "content": "c", "message_index": 0, "step_pointer": ""}},
		}},
	})
	traces2 := got2["traces"].([]map[string]any)
	if traces2[0]["role"] != "fallback" {
		t.Fatalf("role 回落不符: %v", traces2[0]["role"])
	}

	// 非 dict 输入 → 空 traces
	got3 := ProjectExecutionHistory(map[string]any{})
	if traces3, ok := got3["traces"].([]map[string]any); !ok || len(traces3) != 0 {
		t.Fatalf("空输入应得空 traces: %v", got3)
	}
}

// TestExcerptStr_截断边界 对照 Python _excerpt。
// Python: case_reader.py:371-374
func TestExcerptStr_截断边界(t *testing.T) {
	if got := excerptStr("short", 10); got != "short" {
		t.Fatalf("未超限应原样, got %q", got)
	}
	long := "0123456789abcdef"
	if got := excerptStr(long, 8); got != "01234..." {
		t.Fatalf("截断应加省略号, got %q", got)
	}
	if got := excerptStr("exact-8", 8); got != "exact-8" {
		t.Fatalf("恰等限长应原样, got %q", got)
	}
}
