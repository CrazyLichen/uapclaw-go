package harnessrsi

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRuntimePolicy_Validate_非映射与未知键 非映射与未知键的报错文案逐字对齐
func TestRuntimePolicy_Validate_非映射与未知键(t *testing.T) {
	_, err := ValidateRuntimePolicy("not a map", 300)
	if err == nil || err.Error() != "runtime.yaml must contain a mapping" {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ValidateRuntimePolicy(map[string]any{"unknown_key": 1}, 300)
	if err == nil || err.Error() != "runtime.yaml contains unsupported keys: [unknown_key]" {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ValidateRuntimePolicy(map[string]any{}, 0)
	if err == nil || err.Error() != "solver_max_iterations_hard_limit must be a positive integer" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRuntimePolicy_Validate_越界 max_iterations 越界与 on_max_iterations 非法值
func TestRuntimePolicy_Validate_越界(t *testing.T) {
	_, err := ValidateRuntimePolicy(map[string]any{"max_iterations": 0}, 300)
	if err == nil || err.Error() != "runtime.yaml.max_iterations must be an integer between 1 and 300" {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ValidateRuntimePolicy(map[string]any{"max_iterations": 301}, 300)
	if err == nil {
		t.Fatal("over hard limit must be rejected")
	}
	_, err = ValidateRuntimePolicy(map[string]any{"on_max_iterations": "boom"}, 300)
	if err == nil || err.Error() != "runtime.yaml.on_max_iterations must be one of [error return_observations]" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRuntimePolicy_Read_默认值 无 runtime.yaml 时返回默认策略
func TestRuntimePolicy_Read_默认值(t *testing.T) {
	policy, err := ReadRuntimePolicy(t.TempDir(), 300, "error", 300)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxIterations != 300 || policy.OnMaxIterations != "error" ||
		policy.MaxIterationsHardLimit != 300 || policy.Source != "evaluator_default" {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}

// TestRuntimePolicy_Read_合法覆盖 合法覆盖后 source=runtime.yaml
func TestRuntimePolicy_Read_合法覆盖(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "runtime.yaml"), "max_iterations: 50\non_max_iterations: return_observations\n"); err != nil {
		t.Fatal(err)
	}
	policy, err := ReadRuntimePolicy(root, 300, "error", 300)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxIterations != 50 || policy.OnMaxIterations != "return_observations" || policy.Source != "runtime.yaml" {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}

// TestRuntimePolicy_Read_文件路径传入 传入文件路径时取父目录
func TestRuntimePolicy_Read_文件路径传入(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "runtime.yaml"), "max_iterations: 10\n"); err != nil {
		t.Fatal(err)
	}
	policy, err := ReadRuntimePolicy(filepath.Join(root, "runtime.yaml"), 300, "error", 300)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxIterations != 10 {
		t.Fatalf("expected 10, got %d", policy.MaxIterations)
	}
}

// TestRuntimePolicy_Read_symlink拒绝 symlink 拒绝为包本地普通文件要求
func TestRuntimePolicy_Read_symlink拒绝(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "real.yaml")
	if err := writeFile(target, "max_iterations: 10\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "runtime.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := ReadRuntimePolicy(root, 300, "error", 300)
	if err == nil || err.Error() != "runtime.yaml must be a package-local regular file" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRuntimePolicy_Read_超限拒绝 超过 16KiB 拒绝
func TestRuntimePolicy_Read_超限拒绝(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 20000)
	for i := range big {
		big[i] = '#'
	}
	if err := writeFile(filepath.Join(root, "runtime.yaml"), string(big)+"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := ReadRuntimePolicy(root, 300, "error", 300)
	if err == nil || err.Error() != "runtime.yaml exceeds the 16 KiB configuration limit" {
		t.Fatalf("unexpected error: %v", err)
	}
}
