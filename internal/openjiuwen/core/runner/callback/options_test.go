package callback

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestApplyCallbackOptions_默认值 测试无选项时的默认值。
func TestApplyCallbackOptions_默认值(t *testing.T) {
	cfg := applyCallbackOptions()
	if cfg.Priority != 0 {
		t.Errorf("Priority 默认应为 0，实际 %d", cfg.Priority)
	}
	if cfg.Namespace != "default" {
		t.Errorf("Namespace 默认应为 'default'，实际 %q", cfg.Namespace)
	}
	if cfg.Once {
		t.Error("Once 默认应为 false")
	}
}

// TestWithPriority 测试 WithPriority 选项。
func TestWithPriority(t *testing.T) {
	cfg := applyCallbackOptions(WithPriority(100))
	if cfg.Priority != 100 {
		t.Errorf("期望 Priority=100，实际 %d", cfg.Priority)
	}
}

// TestWithOnce 测试 WithOnce 选项。
func TestWithOnce(t *testing.T) {
	cfg := applyCallbackOptions(WithOnce())
	if !cfg.Once {
		t.Error("期望 Once=true")
	}
}

// TestWithNamespace 测试 WithNamespace 选项。
func TestWithNamespace(t *testing.T) {
	cfg := applyCallbackOptions(WithNamespace("my_ns"))
	if cfg.Namespace != "my_ns" {
		t.Errorf("期望 Namespace='my_ns'，实际 %q", cfg.Namespace)
	}
}

// TestWithTags 测试 WithTags 选项。
func TestWithTags(t *testing.T) {
	cfg := applyCallbackOptions(WithTags("tag1", "tag2"))
	if len(cfg.Tags) != 2 || cfg.Tags[0] != "tag1" || cfg.Tags[1] != "tag2" {
		t.Errorf("期望 Tags=[tag1,tag2]，实际 %v", cfg.Tags)
	}
}

// TestWithMaxRetries 测试 WithMaxRetries 选项。
func TestWithMaxRetries(t *testing.T) {
	cfg := applyCallbackOptions(WithMaxRetries(3))
	if cfg.MaxRetries != 3 {
		t.Errorf("期望 MaxRetries=3，实际 %d", cfg.MaxRetries)
	}
}

// TestWithRetryDelay 测试 WithRetryDelay 选项。
func TestWithRetryDelay(t *testing.T) {
	cfg := applyCallbackOptions(WithRetryDelay(0.5))
	if cfg.RetryDelay != 0.5 {
		t.Errorf("期望 RetryDelay=0.5，实际 %f", cfg.RetryDelay)
	}
}

// TestWithTimeout 测试 WithTimeout 选项。
func TestWithTimeout(t *testing.T) {
	cfg := applyCallbackOptions(WithTimeout(10.0))
	if cfg.Timeout != 10.0 {
		t.Errorf("期望 Timeout=10.0，实际 %f", cfg.Timeout)
	}
}

// TestWithCallbackType 测试 WithCallbackType 选项。
func TestWithCallbackType(t *testing.T) {
	cfg := applyCallbackOptions(WithCallbackType("transform"))
	if cfg.CallbackType != "transform" {
		t.Errorf("期望 CallbackType='transform'，实际 %q", cfg.CallbackType)
	}
}

// TestApplyCallbackOptions_多选项组合 测试多个选项组合。
func TestApplyCallbackOptions_多选项组合(t *testing.T) {
	cfg := applyCallbackOptions(
		WithPriority(50),
		WithOnce(),
		WithNamespace("test_ns"),
		WithMaxRetries(2),
	)
	if cfg.Priority != 50 || !cfg.Once || cfg.Namespace != "test_ns" || cfg.MaxRetries != 2 {
		t.Errorf("多选项组合不正确: %+v", cfg)
	}
}
