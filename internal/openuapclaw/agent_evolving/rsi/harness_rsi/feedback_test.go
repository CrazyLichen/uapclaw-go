package harnessrsi

import (
	"strings"
	"testing"
)

// TestFeedback_OutputRepairConfig_默认与校验 默认 3/2，非法值报错文案逐字对齐
func TestFeedback_OutputRepairConfig_默认与校验(t *testing.T) {
	def := DefaultOutputRepairConfig()
	if def.MaxAttempts != 3 || def.MaxStagnantAttempts != 2 {
		t.Fatalf("unexpected defaults: %+v", def)
	}
	_, err := NewOutputRepairConfig(0, 2)
	if err == nil || err.Error() != "output_repair.max_attempts must be a positive integer" {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = NewOutputRepairConfig(3, 0)
	if err == nil || err.Error() != "output_repair.max_stagnant_attempts must be a positive integer" {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg, err := OutputRepairConfigFromDict(nil); err != nil || cfg.MaxAttempts != 3 {
		t.Fatalf("nil dict must return defaults: %+v err=%v", cfg, err)
	}
}

// TestFeedback_Tracker_停止原因 attempt_limit_reached 优先于 no_progress，同指纹触发停滞
func TestFeedback_Tracker_停止原因(t *testing.T) {
	cfg, err := NewOutputRepairConfig(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewFeedbackTracker(cfg, nil)
	if !tracker.Failed([]string{"e"}, "s1") {
		t.Fatal("first failure must allow retry")
	}
	// 同指纹第二次 → no_progress（attempts=2 < max=3，但停滞已到上限 → 停止）
	if tracker.Failed([]string{"e"}, "s1") {
		t.Fatalf("stagnant limit must stop, stop=%s", tracker.StopReason)
	}
	if tracker.StopReason != "no_progress" {
		t.Fatalf("expected no_progress, got %s", tracker.StopReason)
	}
	// attempts 达上限 → attempt_limit_reached
	tracker2 := NewFeedbackTracker(cfg, nil)
	tracker2.Failed([]string{"a"}, "x")
	tracker2.Failed([]string{"b"}, "y")
	if tracker2.Failed([]string{"c"}, "z") {
		t.Fatal("attempt limit must stop")
	}
	if tracker2.StopReason != "attempt_limit_reached" {
		t.Fatalf("expected attempt_limit_reached, got %s", tracker2.StopReason)
	}
}

// TestFeedback_Tracker_覆盖钳制 maxAttempts 覆盖取 min(config, max(0,n))
func TestFeedback_Tracker_覆盖钳制(t *testing.T) {
	cfg := DefaultOutputRepairConfig()
	cover := 1
	tracker := NewFeedbackTracker(cfg, &cover)
	if tracker.MaxAttempts != 1 {
		t.Fatalf("expected clamped to 1, got %d", tracker.MaxAttempts)
	}
	negative := -5
	tracker2 := NewFeedbackTracker(cfg, &negative)
	if tracker2.MaxAttempts != 0 {
		t.Fatalf("expected 0 for negative override, got %d", tracker2.MaxAttempts)
	}
}

// TestFeedback_CorrectionMessage_截断与文案 24,000 截断附固定后缀，errors 逐条前缀
func TestFeedback_CorrectionMessage_截断与文案(t *testing.T) {
	message := CorrectionMessage("task", "short", []string{"bad field"})
	if !strings.HasPrefix(message, "task\n\n## Previous output") {
		t.Fatalf("message must start with original task: %q", message[:20])
	}
	long := make([]byte, 25000)
	for i := range long {
		long[i] = 'x'
	}
	truncated := CorrectionMessage("t", string(long), nil)
	if !strings.Contains(truncated, "[Previous output truncated; consult the original task and evidence for omitted facts.]") {
		t.Fatal("truncated message must carry the notice suffix")
	}
	if !strings.Contains(message, "- bad field\n") {
		t.Fatal("errors must be prefixed with dash")
	}
}
