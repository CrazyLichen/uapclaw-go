package judger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
)

// stubExecution 用例执行结果桩（CaseExecutionView）。
type stubExecution struct {
	response any
	status   string
	errText  string
}

func (s *stubExecution) ResponseView() any           { return s.response }
func (s *stubExecution) ExecutionStatusView() string { return s.status }
func (s *stubExecution) ErrorView() string           { return s.errText }

// validVerdict 面向 behaviors 清单的合法判决 JSON。
func validVerdict() string {
	return `{"overall_reason": "答案一致", "status": "completed", ` +
		`"behaviors": [{"id": "reference_answer", "score": 1.0, "reason": "一致", "evidence": "全等"}], ` +
		`"forbidden_hits": [], "score": 1.0}`
}

// ──────────────────────────── 构造校验链 ────────────────────────────

// TestNewLlmAsJudgeJudger_构造校验链 对齐 Python __init__ 校验顺序。
func TestNewLlmAsJudgeJudger_构造校验链(t *testing.T) {
	// 无模型引用：先于其余校验报错。
	_, err := NewLlmAsJudgeJudger(&config.EvaluatorConfig{})
	if err == nil || !strings.Contains(err.Error(), "requires judge_model_config_ref or model_config_ref") {
		t.Fatalf("无模型引用应报错: %v", err)
	}
	cfg := newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfg.JudgeSuccessScore = 1.5
	if _, err = NewLlmAsJudgeJudger(cfg); err == nil || !strings.Contains(err.Error(), "judge_success_score") {
		t.Fatalf("success_score 越界应报错: %v", err)
	}
	cfg = newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfg.JudgeAgentMaxIterations = 0
	if _, err = NewLlmAsJudgeJudger(cfg); err == nil || !strings.Contains(err.Error(), "judge_agent_max_iterations") {
		t.Fatalf("iterations 非正应报错: %v", err)
	}
	cfg = newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfg.JudgeTimeoutSec = 0
	if _, err = NewLlmAsJudgeJudger(cfg); err == nil || !strings.Contains(err.Error(), "judge_timeout_sec") {
		t.Fatalf("timeout 非正应报错: %v", err)
	}
	cfg = newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfg.JudgeMaxRetries = 6
	if _, err = NewLlmAsJudgeJudger(cfg); err == nil || !strings.Contains(err.Error(), "between 0 and 5") {
		t.Fatalf("retries 越界应报错: %v", err)
	}
	cfg = newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfg.JudgeSuccessScore = 0.8
	judger, err := NewLlmAsJudgeJudger(cfg)
	if err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
	if judger.MethodName() != "llm_as_judge" {
		t.Fatalf("method 应为 llm_as_judge: %s", judger.MethodName())
	}
}

// ──────────────────────────── 执行失败分轨 ────────────────────────────

func init() {
	// 测试二进制不含父包 evaluator 的 init 注册，手工注册瞬态判定桩（对齐 transport.py 标记表）。
	RegisterTransientTransportError(func(errText string) string {
		lowered := strings.ToLower(errText)
		for _, marker := range []string{"connection", "timeout", "rate limit"} {
			if strings.Contains(lowered, marker) {
				return errText
			}
		}
		return ""
	})
}

// TestLlmAsJudge_执行失败分轨 对齐 _execution_failure_result。
func TestLlmAsJudge_执行失败分轨(t *testing.T) {
	cfg := newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfg.JudgeSuccessScore = 0.8
	j, err := NewLlmAsJudgeJudger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Python judge 首步 validate_case（ScoringContract）先于执行状态检查。
	validCase := map[string]any{
		"reference": map[string]any{"answer": "参考答案"},
	}
	result, err := j.Judge(context.Background(), validCase,
		&stubExecution{status: "failed", errText: "solver crashed"}, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("执行失败走 0 分结果不报错: %v", err)
	}
	if result.Score != 0.0 || result.Passed || result.Reason != "solver crashed" {
		t.Fatalf("应为 0 分失败结果: %+v", result)
	}
	// continuous 模式 + 非瞬态错误 → metadata.score_mode 登记。
	cfgContinuous := newJudgeTestConfig(t, "http://127.0.0.1:1")
	cfgContinuous.ScoreMode = "continuous"
	cfgContinuous.JudgeSuccessScore = 0.8
	jc, _ := NewLlmAsJudgeJudger(cfgContinuous)
	result, err = jc.Judge(context.Background(), validCase,
		&stubExecution{status: "failed", errText: "some other failure"}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata["score_mode"] != "continuous" {
		t.Fatalf("非瞬态 continuous 失败应登记 score_mode: %+v", result.Metadata)
	}
	// 瞬态错误 → 不登记。
	result, _ = jc.Judge(context.Background(), validCase,
		&stubExecution{status: "failed", errText: "connection reset"}, t.TempDir(), nil)
	if _, exists := result.Metadata["score_mode"]; exists {
		t.Fatalf("瞬态错误不应登记 score_mode: %+v", result.Metadata)
	}
}

// ──────────────────────────── 全链评估 ────────────────────────────

// buildJudgeCase 构造带参考答案的合法考题与输出目录。
func buildJudgeCase(t *testing.T, baseURL string) (*config.EvaluatorConfig, *LlmAsJudgeJudger, map[string]any, string) {
	t.Helper()
	server := fakeOpenAIServer(t, validVerdict(), "stop", nil)
	t.Cleanup(server.Close)
	cfg := newJudgeTestConfig(t, server.URL)
	cfg.JudgeSuccessScore = 0.8
	j, err := NewLlmAsJudgeJudger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	caseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(caseDir, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	caseMapping := map[string]any{
		"case_id":   "c1",
		"case_path": filepath.Join(caseDir, "case.json"),
		"request":   "题目",
		"input":     "把这句话分类",
		"reference": map[string]any{"answer": "参考答案"},
	}
	return cfg, j, caseMapping, caseDir
}

// TestLlmAsJudge_Judge_全链成功 对齐 judge 主链与留档文件集。
func TestLlmAsJudge_Judge_全链成功(t *testing.T) {
	_, j, caseMapping, caseDir := buildJudgeCase(t, "")
	result, err := j.Judge(context.Background(), caseMapping,
		&stubExecution{response: "参考答案", status: "passed"}, caseDir, nil)
	if err != nil {
		t.Fatalf("全链评估不应报错: %v", err)
	}
	if result.Method != "llm_as_judge" {
		t.Fatalf("method 不符: %s", result.Method)
	}
	if result.Score != 1.0 || !result.Passed {
		t.Fatalf("binary 模式应按 passed 取 1.0: %+v", result)
	}
	if result.Reason != "答案一致" {
		t.Fatalf("reason 应取 overall_reason: %q", result.Reason)
	}
	if result.Metadata["pass_threshold"] != 0.8 || result.Metadata["score_mode"] != "binary" {
		t.Fatalf("metadata 阈值/模式缺失: %+v", result.Metadata)
	}
	if result.Metadata["assessment_attempts"] != 1 || result.Metadata["recovery"] != "none" {
		t.Fatalf("首次成功 attempt/recovery 不符: %+v", result.Metadata)
	}
	parsed, _ := result.Metadata["parsed"].(map[string]any)
	if parsed == nil || parsed["overall_reason"] != "答案一致" {
		t.Fatalf("parsed 应承载规范化判决: %+v", result.Metadata["parsed"])
	}
	// 留档：assessment.json / response_1.json / diagnostics_1.json。
	judgeRoot := filepath.Join(caseDir, "judge")
	entries, _ := os.ReadDir(judgeRoot)
	if len(entries) != 1 {
		t.Fatalf("judge 下应有唯一 evaluation_* 目录: %d", len(entries))
	}
	judgeDir := filepath.Join(judgeRoot, entries[0].Name())
	for _, name := range []string{"assessment.json", "response_1.json", "diagnostics_1.json"} {
		if _, err := os.Stat(filepath.Join(judgeDir, name)); err != nil {
			t.Fatalf("%s 应落盘: %v", name, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(judgeDir, "response_1.json"))
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["recovery"] != "none" {
		t.Fatalf("response_1.json recovery 应为 none: %v", stored["recovery"])
	}
}

// TestLlmAsJudge_缺少输出目录 output_dir 为空报基础设施错误。
func TestLlmAsJudge_缺少输出目录(t *testing.T) {
	_, j, caseMapping, _ := buildJudgeCase(t, "")
	_, err := j.Judge(context.Background(), caseMapping,
		&stubExecution{response: "x", status: "passed"}, "", nil)
	var infra *EvaluationInfrastructureError
	if err == nil || !errorsAs(err, &infra) {
		t.Fatalf("应报基础设施错误: %v", err)
	}
}

// TestLlmAsJudge_基线关闭却传上下文 baseline_context 非 nil 而配置关闭时报错。
func TestLlmAsJudge_基线关闭却传上下文(t *testing.T) {
	_, j, caseMapping, caseDir := buildJudgeCase(t, "")
	_, err := j.Judge(context.Background(), caseMapping,
		&stubExecution{response: "x", status: "passed"}, caseDir, stubBaseline{})
	var infra *EvaluationInfrastructureError
	if err == nil || !errorsAs(err, &infra) {
		t.Fatalf("应报基础设施错误: %v", err)
	}
}

// errorsAs errors.As 的局部替身（避免测试文件引 errors 包名冲突）。
func errorsAs(err error, target **EvaluationInfrastructureError) bool {
	for e := err; e != nil; {
		if v, ok := e.(*EvaluationInfrastructureError); ok {
			*target = v
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// stubBaseline 基线上下文桩（BaselineContextView）。
type stubBaseline struct{}

func (stubBaseline) Validate(map[string]any) (map[string]any, error) { return map[string]any{}, nil }
func (stubBaseline) CaseIDView() string                              { return "b1" }
func (stubBaseline) EvalRefPathView() string                         { return "/tmp/eval.yaml" }
func (stubBaseline) Fingerprint() string                             { return "fp" }
func (stubBaseline) EvidenceDigestView() string                      { return "digest" }
func (stubBaseline) ResultPathView() string                          { return "/tmp/result" }
