package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCoverage_EvaluatorFromDictFloatAndBoolError 浮点字段与非法布尔路径
func TestCoverage_EvaluatorFromDictFloatAndBoolError(t *testing.T) {
	cfg, err := EvaluatorConfigFromDict(map[string]any{
		"judge_success_score":       0.95,
		"judge_baseline_comparison": "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JudgeSuccessScore != 0.95 || cfg.JudgeBaselineComparison {
		t.Fatalf("float/bool override failed: %+v", cfg)
	}
	if _, err := EvaluatorConfigFromDict(map[string]any{"judge_baseline_comparison": "maybe"}); err == nil ||
		err.Error() != "expected a boolean value, got maybe" {
		t.Fatalf("invalid bool must error: %v", err)
	}
	if _, err := EvaluatorConfigFromDict(map[string]any{"output_repair": "bad"}); err == nil ||
		err.Error() != "output_repair must be a mapping" {
		t.Fatalf("non-map output_repair must error: %v", err)
	}
}

// TestCoverage_MemberOptimizerFromDictExplicitHardLimit 显式硬上限与非法布尔
func TestCoverage_MemberOptimizerFromDictExplicitHardLimit(t *testing.T) {
	cfg, err := MemberOptimizerConfigFromDict(map[string]any{
		"runtime_iteration_hard_limit": "150",
		"freeze":                       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeIterationHardLimit != 150 || !cfg.Freeze {
		t.Fatalf("explicit hard limit failed: %+v", cfg)
	}
	if _, err := MemberOptimizerConfigFromDict(map[string]any{"freeze": "maybe"}); err == nil ||
		err.Error() != "expected a boolean value, got maybe" {
		t.Fatalf("invalid bool must error: %v", err)
	}
}

// TestCoverage_LoadExistingAndInvalidYaml 既有配置加载与非法 YAML 报错
func TestCoverage_LoadExistingAndInvalidYaml(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orchestrator.yaml")
	if err := os.WriteFile(path, []byte("workspace_dir: .local/rsi\nmax_epochs: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAutoCoordinatingHarnessConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceDir != ".local/rsi" || cfg.MaxEpochs != 3 {
		t.Fatalf("existing config load failed: %+v", cfg)
	}
	invalid := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(invalid, []byte("{invalid: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAutoCoordinatingHarnessConfig(invalid); err == nil {
		t.Fatal("invalid yaml must error")
	}
	// 校验失败路径
	invalidValues := filepath.Join(t.TempDir(), "badvalues.yaml")
	if err := os.WriteFile(invalidValues, []byte("max_epochs: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAutoCoordinatingHarnessConfig(invalidValues); err == nil ||
		err.Error() != "max_epochs must be greater than or equal to 1" {
		t.Fatalf("invalid values must error: %v", err)
	}
}
