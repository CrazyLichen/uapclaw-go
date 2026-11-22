package judger

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
)

// ──────────────────────────── build_judger 工厂 ────────────────────────────

// TestBuildJudger_路由 对齐 Python __init__.py build_judger 的五类路由。
func TestBuildJudger_路由(t *testing.T) {
	cfg := newJudgeTestConfig(t, "http://127.0.0.1:1")
	cases := []struct {
		method string
		expect string
	}{
		{"exact_match", "exact_match"},
		{"Exact-Match", "exact_match"}, // 大写+连杆归一
		{"llm_as_judge", "llm_as_judge"},
		{"script_based", "script_based"},
		{"rule_based", "script_based"},
		{"", "script_based"},
		{"default", "script_based"},
		{"pass_through", "script_based"},
	}
	for _, tc := range cases {
		cfg.EvaluationMethod = tc.method
		j, err := BuildJudger(cfg)
		if err != nil {
			t.Fatalf("method %q 报错: %v", tc.method, err)
		}
		if j.MethodName() != tc.expect {
			t.Fatalf("method %q 应路由到 %s，实得 %s", tc.method, tc.expect, j.MethodName())
		}
	}
	cfg.EvaluationMethod = "nonsense"
	if _, err := BuildJudger(cfg); err == nil || !strings.Contains(err.Error(), "unsupported evaluation_method") {
		t.Fatalf("未知 method 应报错: %v", err)
	}
}

// ──────────────────────────── exact_match ────────────────────────────

// TestExactMatchJudger_validateCase 对齐 Python validate_case 三拒绝。
func TestExactMatchJudger_validateCase(t *testing.T) {
	j := &ExactMatchJudger{}
	err := j.ValidateCase(map[string]any{"reference": map[string]any{"rubric": []any{"规则"}}})
	if err == nil || !strings.Contains(err.Error(), "not rubric or file grading") {
		t.Fatalf("rubric 应拒绝: %v", err)
	}
	err = j.ValidateCase(map[string]any{"reference": map[string]any{"files": []any{"a.txt"}}})
	if err == nil || !strings.Contains(err.Error(), "not rubric or file grading") {
		t.Fatalf("files 应拒绝: %v", err)
	}
	err = j.ValidateCase(map[string]any{"swebench": map[string]any{"timeout_sec": 1800.0}})
	if err == nil || !strings.Contains(err.Error(), "not rubric or file grading") {
		t.Fatalf("swebench 应拒绝: %v", err)
	}
	err = j.ValidateCase(map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "a reference answer is required") {
		t.Fatalf("无参考答案应拒绝: %v", err)
	}
}

// TestExactMatchJudger_Judge 对齐 judge 主链（严格相等 + 失败分轨）。
func TestExactMatchJudger_Judge(t *testing.T) {
	j := &ExactMatchJudger{}
	caseMapping := map[string]any{"reference": map[string]any{"answer": "标准答案"}}
	result, err := j.Judge(nil, caseMapping, &stubExecution{response: "标准答案", status: "passed"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 1.0 || !result.Passed || result.Reason != "" {
		t.Fatalf("一致应满分: %+v", result)
	}
	result, err = j.Judge(nil, caseMapping, &stubExecution{response: "别的", status: "passed"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 0.0 || result.Passed || result.Reason != "response does not exactly match reference answer" {
		t.Fatalf("不一致应零分: %+v", result)
	}
	result, err = j.Judge(nil, caseMapping, &stubExecution{status: "failed", errText: "boom"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 0.0 || result.Reason != "boom" {
		t.Fatalf("执行失败走 _failure_result: %+v", result)
	}
	// answer 信封解包后严格相等。
	result, _ = j.Judge(nil, caseMapping, &stubExecution{
		response: map[string]any{"result_type": "answer", "output": "标准答案"}, status: "passed"}, "", nil)
	if result.Score != 1.0 {
		t.Fatalf("answer 信封应解包比对: %+v", result)
	}
}

// ──────────────────────────── script_based ────────────────────────────

// TestScriptBasedJudger_validateCase 对齐 Python validate_case 四分支。
func TestScriptBasedJudger_validateCase(t *testing.T) {
	j := &ScriptBasedJudger{}
	err := j.ValidateCase(map[string]any{"reference": map[string]any{"rubric": []any{"规则"}}})
	if err == nil || !strings.Contains(err.Error(), "does not evaluate reference.rubric") {
		t.Fatalf("rubric 应拒绝: %v", err)
	}
	err = j.ValidateCase(map[string]any{"swebench": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "official_dataset_path is missing") {
		t.Fatalf("swebench 缺数据集路径应拒绝: %v", err)
	}
	err = j.ValidateCase(map[string]any{"reference": map[string]any{"files": []any{"a.txt"}}})
	if err == nil || !strings.Contains(err.Error(), "no supported verifier adapter") {
		t.Fatalf("files 应拒绝: %v", err)
	}
	err = j.ValidateCase(map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "no backend JudgeResult or reference answer") {
		t.Fatalf("无后端结果无参考答案应拒绝: %v", err)
	}
}

// TestScriptBasedJudger_Judge_后端结果直通 对齐 judge 首分支。
func TestScriptBasedJudger_Judge_后端结果直通(t *testing.T) {
	j := &ScriptBasedJudger{}
	execution := &stubExecutionWithJudge{stubExecution: stubExecution{status: "passed"},
		judge: &JudgeResult{Method: "swebench_official", Score: 1.0, Passed: true, Reason: "官方判过"}}
	result, err := j.Judge(nil, map[string]any{"swebench": map[string]any{}}, execution, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != execution.judge {
		t.Fatalf("后端已判分应直通返回: %+v", result)
	}
}

// TestScriptBasedJudger_Judge_参考答案比对 对齐非 swebench 分支。
func TestScriptBasedJudger_Judge_参考答案比对(t *testing.T) {
	j := &ScriptBasedJudger{}
	caseMapping := map[string]any{"reference": map[string]any{"answer": "参考"}}
	result, err := j.Judge(nil, caseMapping, &stubExecution{response: "参考", status: "passed"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != 1.0 || result.Metadata["rule_engine_status"] != "reference_comparison" {
		t.Fatalf("参考比对应满分并登记 rule_engine_status: %+v", result)
	}
	result, _ = j.Judge(nil, caseMapping, &stubExecution{response: "不同", status: "passed"}, "", nil)
	if result.Score != 0.0 || result.Reason != "response does not match reference" {
		t.Fatalf("不匹配应零分: %+v", result)
	}
}

// TestJudgeSwebench_seam 对齐 _judge_swebench 的缝位行为（空 patch / 官方结果 / 基建错误）。
func TestJudgeSwebench_seam(t *testing.T) {
	t.Run("空patch零分", func(t *testing.T) {
		RegisterSwebenchVerifier(stubVerifier{patch: "  "})
		t.Cleanup(func() { RegisterSwebenchVerifier(nil) })
		outputDir := t.TempDir()
		result, err := judgeSwebench(
			map[string]any{"swebench": map[string]any{}},
			&stubExecution{status: "passed", workspaceDir: t.TempDir()}, outputDir)
		if err != nil {
			t.Fatal(err)
		}
		if result.Method != "swebench_official" || result.Score != 0.0 || result.Metadata["empty_patch"] != true {
			t.Fatalf("空 patch 应零分并登记 empty_patch: %+v", result)
		}
		if _, err := result.Metadata["model_patch_path"].(string); !err {
			t.Fatalf("model_patch_path 应登记: %+v", result.Metadata)
		}
		if result.Metadata["model_patch_chars"] != 0 {
			t.Fatalf("chars 应为 0: %+v", result.Metadata)
		}
	})
	t.Run("官方结果映射", func(t *testing.T) {
		RegisterSwebenchVerifier(stubVerifier{patch: "diff --git a/x b/x", official: map[string]any{
			"score": 1.0, "passed": true, "reason": "tests passed", "extra": "keep",
		}})
		t.Cleanup(func() { RegisterSwebenchVerifier(nil) })
		result, err := judgeSwebench(
			map[string]any{"swebench": map[string]any{}},
			&stubExecution{status: "passed", workspaceDir: t.TempDir()}, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if result.Score != 1.0 || !result.Passed || result.Reason != "tests passed" {
			t.Fatalf("官方结果应映射: %+v", result)
		}
		if result.Metadata["extra"] != "keep" || result.Metadata["model_patch_chars"] != len("diff --git a/x b/x") {
			t.Fatalf("metadata 应保留额外键并登记 patch 长度: %+v", result.Metadata)
		}
		if _, has := result.Metadata["passed"]; has {
			t.Fatalf("passed/score/reason 应从 metadata 剔除: %+v", result.Metadata)
		}
	})
	t.Run("基建错误上抛", func(t *testing.T) {
		RegisterSwebenchVerifier(stubVerifier{infraErr: true})
		t.Cleanup(func() { RegisterSwebenchVerifier(nil) })
		_, err := judgeSwebench(
			map[string]any{"swebench": map[string]any{}},
			&stubExecution{status: "passed", workspaceDir: t.TempDir()}, t.TempDir())
		if err == nil {
			t.Fatal("SWE-bench 基建错误应上抛")
		}
	})
	t.Run("其他错误零分", func(t *testing.T) {
		RegisterSwebenchVerifier(stubVerifier{plainErr: "collector exploded"})
		t.Cleanup(func() { RegisterSwebenchVerifier(nil) })
		result, err := judgeSwebench(
			map[string]any{"swebench": map[string]any{}},
			&stubExecution{status: "passed", workspaceDir: t.TempDir()}, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if result.Score != 0.0 || result.Reason != "collector exploded" || result.Metadata["model_patch_chars"] != 0 {
			t.Fatalf("普通错误应 0 分兜底: %+v", result)
		}
	})
}

// TestLoadSwebenchModelPatch 对齐 _load_swebench_model_patch 两源回退。
func TestLoadSwebenchModelPatch(t *testing.T) {
	// 源1：metadata 捕获路径。
	captured := t.TempDir()
	path := captured + "/model.patch"
	if err := writeFile(path, "captured patch"); err != nil {
		t.Fatal(err)
	}
	execution := &stubExecution{status: "passed", workspaceDir: t.TempDir(),
		metadata: map[string]any{"swebench_model_patch_path": path}}
	patch, err := loadSwebenchModelPatch(execution, captured)
	if err != nil || patch != "captured patch" {
		t.Fatalf("应读捕获路径: %v %q", err, patch)
	}
	// 源2：workspace_dir 回退（seam 注入）。
	RegisterSwebenchVerifier(stubVerifier{patch: "workspace patch"})
	t.Cleanup(func() { RegisterSwebenchVerifier(nil) })
	patch, err = loadSwebenchModelPatch(&stubExecution{status: "passed", workspaceDir: captured}, captured)
	if err != nil || patch != "workspace patch" {
		t.Fatalf("无捕获路径应回退 collect_model_patch: %v %q", err, patch)
	}
	// 捕获路径读取失败 → 基建错误。
	execution = &stubExecution{status: "passed", workspaceDir: t.TempDir(),
		metadata: map[string]any{"swebench_model_patch_path": "/nonexistent/patch.diff"}}
	_, err = loadSwebenchModelPatch(execution, captured)
	if err == nil || !strings.Contains(err.Error(), "failed to read captured SWE-bench patch") {
		t.Fatalf("读取失败应报基建错误: %v", err)
	}
}

// ──────────────────────────── 测试桩 ────────────────────────────

// stubExecutionWithJudge 携带后端判分结果的执行结果桩。
type stubExecutionWithJudge struct {
	stubExecution
	judge *JudgeResult
}

// JudgeResultView 返回后端判分（空实现返回 nil 走判分链）。
func (s *stubExecutionWithJudge) JudgeResultView() *JudgeResult { return s.judge }

// stubVerifier SWE-bench 验证缝的测试桩。
type stubVerifier struct {
	patch    string
	official map[string]any
	infraErr bool
	plainErr string
}

func (s stubVerifier) CollectModelPatch(workspaceDir string) (string, error) {
	if s.infraErr {
		return "", evalerrors.NewEvaluationInfrastructureError("infra failure")
	}
	if s.plainErr != "" {
		return "", assertNotInfra(s.plainErr)
	}
	return s.patch, nil
}

func (s stubVerifier) RunOfficialEvaluation(caseMapping map[string]any, modelPatch, outputDir string) (map[string]any, error) {
	if s.infraErr {
		return nil, evalerrors.NewEvaluationInfrastructureError("infra failure")
	}
	if s.plainErr != "" {
		return nil, assertNotInfra(s.plainErr)
	}
	return s.official, nil
}

// assertNotInfra 构造非基建错误。
func assertNotInfra(msg string) error { return &plainRuntimeError{msg} }

// plainRuntimeError 非基建错误桩。
type plainRuntimeError struct{ msg string }

func (e *plainRuntimeError) Error() string { return e.msg }
