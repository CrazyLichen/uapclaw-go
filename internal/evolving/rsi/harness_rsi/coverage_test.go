package harnessrsi

import (
	"errors"
	"testing"
)

// TestCoverage_ModelOutputHasMojibake 可见乱码判定
func TestCoverage_ModelOutputHasMojibake(t *testing.T) {
	if ModelOutputHasMojibake("") || ModelOutputHasMojibake("正常内容 normal") {
		t.Fatal("normal/empty text must not be flagged")
	}
	if !ModelOutputHasMojibake(mustMojibake(t, "需要修复的中文样本数据内容较多一些才有效果")) {
		t.Fatal("mojibake text must be flagged")
	}
}

// TestCoverage_IOPath_Windows分支 Windows 长路径分支（非 Windows 平台恒等返回）
func TestCoverage_IOPath_Windows分支(t *testing.T) {
	path := ioPath("/tmp/x")
	if path != "/tmp/x" && path[1] != ':' && path[:4] != `\\?\` {
		// 非 Windows 平台应返回绝对路径
		if path == "/tmp/x" {
			return
		}
	}
	if path != "/tmp/x" {
		t.Fatalf("non-windows ioPath must return absolute path unchanged semantics, got %q", path)
	}
}

// TestCoverage_OutputRepairConfigFromDict_非法形态 非 map 与非法值路径
func TestCoverage_OutputRepairConfigFromDict_非法形态(t *testing.T) {
	// nil → 默认（Python from_dict(None)）
	if cfg, err := OutputRepairConfigFromDict(nil); err != nil || cfg.MaxAttempts != 3 {
		t.Fatalf("nil must default: %+v %v", cfg, err)
	}
	// 非 map 报错（Python raise "output_repair must be a mapping"）
	if _, err := OutputRepairConfigFromDict("not a mapping"); err == nil ||
		err.Error() != "output_repair must be a mapping" {
		t.Fatalf("non-mapping must error: %v", err)
	}
	if _, err := OutputRepairConfigFromDict(map[string]any{"max_attempts": "x"}); err == nil ||
		err.Error() != "output_repair.max_attempts must be a positive integer" {
		t.Fatalf("invalid value must error: %v", err)
	}
	// 合法覆盖
	cfg, err := OutputRepairConfigFromDict(map[string]any{"max_attempts": 7, "max_stagnant_attempts": 4})
	if err != nil || cfg.MaxAttempts != 7 || cfg.MaxStagnantAttempts != 4 {
		t.Fatalf("valid override failed: %+v %v", cfg, err)
	}
}

// TestCoverage_DictInt dictInt 边界
func TestCoverage_DictInt(t *testing.T) {
	if value, err := dictInt(map[string]any{"k": int64(9)}, "k", 1); err != nil || value != 9 {
		t.Fatalf("int64 passthrough failed: %v %v", value, err)
	}
	if value, err := dictInt(map[string]any{"k": 2.0}, "k", 1); err != nil || value != 2 {
		t.Fatalf("float integral passthrough failed: %v %v", value, err)
	}
	if value, err := dictInt(map[string]any{"k": true}, "k", 1); err == nil {
		t.Fatalf("bool must error, got %v", value)
	}
	if value, err := dictInt(map[string]any{"k": nil}, "k", 5); err != nil || value != 5 {
		t.Fatalf("nil must default: %v %v", value, err)
	}
	if value, err := dictInt(map[string]any{}, "missing", 3); err != nil || value != 3 {
		t.Fatalf("missing must default: %v %v", value, err)
	}
}

// TestCoverage_Canonicalize 嵌套 map/list 指纹稳定
func TestCoverage_Canonicalize(t *testing.T) {
	fingerprint1 := fingerprintJSON([]any{[]any{"e"}, map[string]any{"b": 1, "a": map[string]any{"z": []any{1, 2}}}})
	fingerprint2 := fingerprintJSON([]any{[]any{"e"}, map[string]any{"a": map[string]any{"z": []any{1, 2}}, "b": 1}})
	if fingerprint1 != fingerprint2 {
		t.Fatal("canonical fingerprints must be key-order independent")
	}
}

// TestCoverage_ExpandUser tilde 展开
func TestCoverage_ExpandUser(t *testing.T) {
	if expandUser("~/config.yaml") == "~/config.yaml" {
		t.Fatal("tilde must expand when home is available")
	}
	if expandUser("/abs/path.yaml") != "/abs/path.yaml" {
		t.Fatal("absolute path must be unchanged")
	}
}

// TestCoverage_IsRetryable_TimeoutError 超时错误类型与 errorText 空值
func TestCoverage_IsRetryable_TimeoutError(t *testing.T) {
	if errorText(nil) != "" {
		t.Fatal("nil error text must be empty")
	}
	if !IsRetryableModelCallFailure(errors.New("HTTP 503 Service Unavailable")) {
		t.Fatal("uppercase transient status must be retryable")
	}
}
