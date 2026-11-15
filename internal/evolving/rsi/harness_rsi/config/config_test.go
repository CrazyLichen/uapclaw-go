package config

import (
	"os"
	"path/filepath"
	"testing"

	harness_rsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// TestConfig_EvaluatorDefaults EvaluatorConfig 默认值逐字段对齐 Python dataclass
func TestConfig_EvaluatorDefaults(t *testing.T) {
	cfg, err := EvaluatorConfigFromDict(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Backend", cfg.Backend, "single_harness"},
		{"EvaluationMethod", cfg.EvaluationMethod, "script-based"},
		{"JudgeAgentMaxIterations", cfg.JudgeAgentMaxIterations, 300},
		{"JudgeTimeoutSec", cfg.JudgeTimeoutSec, 900},
		{"JudgeMaxRetries", cfg.JudgeMaxRetries, 2},
		{"JudgeSuccessScore", cfg.JudgeSuccessScore, 0.8},
		{"ScoreMode", cfg.ScoreMode, "binary"},
		{"JudgeBaselineComparison", cfg.JudgeBaselineComparison, true},
		{"TransientCaseRetryLimit", cfg.TransientCaseRetryLimit, 2},
		{"SolverBackend", cfg.SolverBackend, "deep_agent"},
		{"SolverMaxIterations", cfg.SolverMaxIterations, 300},
		{"SolverOnMaxIterations", cfg.SolverOnMaxIterations, "error"},
		{"SolverMaxIterationsHardLimit", cfg.SolverMaxIterationsHardLimit, 300},
		{"UapclawswarmStartupTimeoutSec", cfg.UapclawswarmStartupTimeoutSec, 120},
		{"UapclawswarmRuntimeTimeoutSec", cfg.UapclawswarmRuntimeTimeoutSec, 3600},
		{"UapclawswarmRuntimeProfile", cfg.UapclawswarmRuntimeProfile, "task86"},
		{"OutputRepair.MaxAttempts", cfg.OutputRepair.MaxAttempts, 3},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
}

// TestConfig_EvaluatorPostInit 校验链错误文案逐字对齐
func TestConfig_EvaluatorPostInit(t *testing.T) {
	cases := []struct {
		mutate func(*EvaluatorConfig)
		want   string
	}{
		{func(c *EvaluatorConfig) { c.ScoreMode = "both" }, "evaluator.score_mode must be binary or continuous"},
		{func(c *EvaluatorConfig) { c.SolverMaxIterations = 0 }, "evaluator.solver_max_iterations must be a positive integer"},
		{func(c *EvaluatorConfig) { c.SolverMaxIterationsHardLimit = 1 }, "evaluator.solver_max_iterations_hard_limit must be an integer >= solver_max_iterations"},
		{func(c *EvaluatorConfig) { c.SolverOnMaxIterations = "boom" }, "evaluator.solver_on_max_iterations is invalid"},
	}
	for _, testCase := range cases {
		cfg, _ := EvaluatorConfigFromDict(map[string]any{})
		testCase.mutate(&cfg)
		_, err := NewEvaluatorConfig(cfg)
		if err == nil || err.Error() != testCase.want {
			t.Fatalf("want %q, got %v", testCase.want, err)
		}
	}
}

// TestConfig_SolverIterationLimit 字符串数字解析与非正值拒绝
func TestConfig_SolverIterationLimit(t *testing.T) {
	value, err := solverIterationLimit("150")
	if err != nil || value != 150 {
		t.Fatalf("string digits must parse: %v %v", value, err)
	}
	if _, err := solverIterationLimit("abc"); err == nil || err.Error() != "evaluator.solver_max_iterations must be a positive integer" {
		t.Fatalf("unexpected error: %v", err)
	}
	if value, err := solverIterationLimit(nil); err != nil || value != 300 {
		t.Fatalf("nil must default to 300: %v %v", value, err)
	}
}

// TestConfig_PromotionConfig 晋级配置校验与 Continuous
func TestConfig_PromotionConfig(t *testing.T) {
	if _, err := PromotionConfigFromDict(map[string]any{"policy_version": "bad"}); err == nil ||
		err.Error() != "unsupported promotion.policy_version; start a new task using continuous_two_stage_v2" {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := PromotionConfigFromDict(map[string]any{"min_improvement": 1.5}); err == nil ||
		err.Error() != "promotion.min_improvement must be a finite number between 0 and 1" {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := PromotionConfigFromDict(map[string]any{"confirmation_runs": 2}); err == nil ||
		err.Error() != "continuous_two_stage_v2 has only target and full evaluations; remove confirmation_runs" {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg, err := PromotionConfigFromDict(map[string]any{"policy_version": "legacy_epoch_v1"})
	if err != nil || cfg.Continuous() {
		t.Fatalf("legacy version must not be continuous: %v %v", cfg, err)
	}
}

// TestConfig_SchedulingLegacyPolicy legacy promotion_policy 迁移与冲突
func TestConfig_SchedulingLegacyPolicy(t *testing.T) {
	if _, err := OrchestratorSchedulingConfigFromDict(map[string]any{"promotion_policy": "x"}); err == nil ||
		err.Error() != "scheduling.promotion_policy must be migrated to promotion.policy_version" {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := OrchestratorSchedulingConfigFromDict(map[string]any{"full_evaluation_concurrency": 0}); err == nil ||
		err.Error() != "scheduling.full_evaluation_concurrency must be a positive integer" {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := OrchestratorSchedulingConfigFromDict(map[string]any{"evaluation_strategy": "only"}); err == nil ||
		err.Error() != "scheduling.evaluation_strategy must be hybrid" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestConfig_TopLevelDerivations 顶层推导：模型引用三级回退、硬上限继承、continuous 联动
func TestConfig_TopLevelDerivations(t *testing.T) {
	cfg, err := AutoCoordinatingHarnessConfigFromDict(map[string]any{
		"model_configs": map[string]any{},
		"evaluator": map[string]any{
			"score_mode": "binary",
			"model_config_ref": "eval-model",
		},
		"promotion": map[string]any{"policy_version": "continuous_two_stage_v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelConfigs.Evaluation != "eval-model" {
		t.Fatalf("evaluation ref fallback failed: %q", cfg.ModelConfigs.Evaluation)
	}
	// continuous 强制 evaluator.score_mode=continuous
	if cfg.Evaluator.ScoreMode != "continuous" {
		t.Fatalf("continuous promotion must force continuous score mode, got %s", cfg.Evaluator.ScoreMode)
	}
	// runtime_iteration_hard_limit 未显式配置时继承 evaluator 硬上限
	if cfg.MemberOptimizer.RuntimeIterationHardLimit != 300 {
		t.Fatalf("hard limit inheritance failed: %d", cfg.MemberOptimizer.RuntimeIterationHardLimit)
	}
	// max_epochs < 1
	if _, err := AutoCoordinatingHarnessConfigFromDict(map[string]any{"max_epochs": 0}); err == nil ||
		err.Error() != "max_epochs must be greater than or equal to 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestConfig_TopLevelLegacyConflict legacy policy 与新版本冲突
func TestConfig_TopLevelLegacyConflict(t *testing.T) {
	_, err := AutoCoordinatingHarnessConfigFromDict(map[string]any{
		"scheduling": map[string]any{"promotion_policy": "epoch_full_evaluation"},
		"promotion":  map[string]any{"policy_version": "continuous_two_stage_v2"},
	})
	if err == nil || err.Error() != "conflicting scheduling.promotion_policy and promotion.policy_version; remove the old selector" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestConfig_EvaluatorProtocolConfig binary 模式弹出 score_mode 与 judge_baseline_comparison
func TestConfig_EvaluatorProtocolConfig(t *testing.T) {
	binary, _ := EvaluatorConfigFromDict(map[string]any{})
	payload := EvaluatorProtocolConfig(binary)
	if _, exists := payload["score_mode"]; exists {
		t.Fatal("binary mode must drop score_mode")
	}
	if _, exists := payload["judge_baseline_comparison"]; exists {
		t.Fatal("binary mode must drop judge_baseline_comparison")
	}
	continuous, _ := EvaluatorConfigFromDict(map[string]any{"score_mode": "continuous"})
	payload = EvaluatorProtocolConfig(continuous)
	if _, exists := payload["score_mode"]; !exists {
		t.Fatal("continuous mode must keep score_mode")
	}
	continuous.JudgeBaselineComparison = false
	payload = EvaluatorProtocolConfig(continuous)
	if _, exists := payload["judge_baseline_comparison"]; exists {
		t.Fatal("no-baseline must drop judge_baseline_comparison")
	}
}

// TestConfig_BoolValueHelpers 宽松布尔解析边界
func TestConfig_BoolValueHelpers(t *testing.T) {
	for _, input := range []any{"true", "yes", "y", "1", "on", true, 1} {
		if value, err := boolValue(input, false); err != nil || !value {
			t.Fatalf("expected true for %v: %v %v", input, value, err)
		}
	}
	for _, input := range []any{"false", "no", "n", "0", "off", false, 0} {
		if value, err := boolValue(input, true); err != nil || value {
			t.Fatalf("expected false for %v: %v %v", input, value, err)
		}
	}
	if _, err := boolValue("maybe", false); err == nil {
		t.Fatal("invalid string must error")
	}
	if _, err := boolValue(2, false); err == nil {
		t.Fatal("non-0/1 int must error")
	}
}

// TestConfig_StringList 单字符串包装与过滤 nil 元素
func TestConfig_StringList(t *testing.T) {
	data := map[string]any{
		"single": "only",
		"multi":  []any{"a", nil, "b"},
	}
	if got := stringList(data, "single"); len(got) != 1 || got[0] != "only" {
		t.Fatalf("single string must wrap: %v", got)
	}
	if got := stringList(data, "multi"); len(got) != 2 {
		t.Fatalf("nil elements must be filtered: %v", got)
	}
	if got := stringList(data, "missing"); got != nil {
		t.Fatalf("missing key must be nil: %v", got)
	}
}

// TestConfig_OutputRepairFromDict output_repair 子配置接入
func TestConfig_OutputRepairFromDict(t *testing.T) {
	cfg, err := EvaluatorConfigFromDict(map[string]any{
		"output_repair": map[string]any{"max_attempts": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputRepair.MaxAttempts != 5 || cfg.OutputRepair.MaxStagnantAttempts != 2 {
		t.Fatalf("output_repair override failed: %+v", cfg.OutputRepair)
	}
	var zero harness_rsi.OutputRepairConfig
	_ = zero
}

// TestLoader_BootstrapAndLoad 缺失时 bootstrap 默认模板并成功加载
func TestLoader_BootstrapAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "orchestrator.yaml")
	cfg, err := LoadAutoCoordinatingHarnessConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxEpochs != 5 || cfg.DataLoader.BatchSize != 2 {
		t.Fatalf("template defaults mismatch: epochs=%d batch=%d", cfg.MaxEpochs, cfg.DataLoader.BatchSize)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("template must be written to path: %v", err)
	}
}
