package interrupt

import (
	"testing"
)

// ──────────────────────────── NormalizeOptionValue ────────────────────────────

// TestNormalizeOptionValue_分隔符归一 测试空格/下划线/连字符混用视为同一取值。
func TestNormalizeOptionValue_分隔符归一(t *testing.T) {
	same := []string{"Allow Once", "allow_once", "Allow-once", "ALLOW  ONCE", "  allow_once  "}
	for _, input := range same {
		if got := NormalizeOptionValue(input); got != "allow_once" {
			t.Errorf("%q → %q, want allow_once", input, got)
		}
	}
}

// TestNormalizeOptionValue_CJK不变 测试中文文案归一后保持原样。
func TestNormalizeOptionValue_CJK不变(t *testing.T) {
	if got := NormalizeOptionValue("本次允许"); got != "本次允许" {
		t.Errorf("got %q, want 本次允许（CJK 无词间分隔）", got)
	}
}

// TestNormalizeOptionValue_空值 测试空串与 nil。
func TestNormalizeOptionValue_空值(t *testing.T) {
	if got := NormalizeOptionValue(""); got != "" {
		t.Errorf("空串 got %q", got)
	}
	if got := NormalizeOptionValue(nil); got != "" {
		t.Errorf("nil got %q", got)
	}
	if got := NormalizeOptionValue("   "); got != "" {
		t.Errorf("空白 got %q", got)
	}
}

// ──────────────────────────── ResolvePermissionAction ────────────────────────────

// TestResolvePermissionAction_四档别名 测试四档动作的全部别名。
func TestResolvePermissionAction_四档别名(t *testing.T) {
	cases := map[string]string{
		"allow_once":    AllowOnce,
		"approve":       AllowOnce,
		"accept":        AllowOnce,
		"proceed":       AllowOnce,
		"本次允许":          AllowOnce,
		"批准":            AllowOnce,
		"开始执行":          AllowOnce,
		"接收":            AllowOnce,
		"接受":            AllowOnce,
		"session_allow": SessionAllow,
		"会话内记住":         SessionAllow,
		"always_allow":  AlwaysAllow,
		"allow_always":  AlwaysAllow,
		"永久记住":          AlwaysAllow,
		"总是允许":          AlwaysAllow,
		"reject":        Reject,
		"reject_once":   Reject,
		"拒绝":            Reject,
		"继续规划":          Reject,
		"其他意见":          Reject,
		"keep_planning": Reject,
	}
	for input, want := range cases {
		if got := ResolvePermissionAction(input); got != want {
			t.Errorf("%q → %q, want %q", input, got, want)
		}
	}
}

// TestResolvePermissionAction_未知返回空串 测试未收录词返回空。
//
// Python: 返回 None 表示「没认出来」，与「用户拒绝」是两回事。
func TestResolvePermissionAction_未知返回空串(t *testing.T) {
	for _, input := range []string{"unknown", "skip", "跳过", "ok", ""} {
		if got := ResolvePermissionAction(input); got != "" {
			t.Errorf("%q → %q, want 空串", input, got)
		}
	}
}

// TestResolvePermissionAction_大小写与分隔符 测试归一化后仍能解析。
func TestResolvePermissionAction_大小写与分隔符(t *testing.T) {
	if got := ResolvePermissionAction("ALLOW-ALWAYS"); got != AlwaysAllow {
		t.Errorf("got %q, want %q", got, AlwaysAllow)
	}
	if got := ResolvePermissionAction("Reject Once"); got != Reject {
		t.Errorf("got %q, want %q", got, Reject)
	}
}

// ──────────────────────────── IsKeepPlanningValue ────────────────────────────

// TestIsKeepPlanningValue_三个取值 测试继续规划类取值。
func TestIsKeepPlanningValue_三个取值(t *testing.T) {
	for _, input := range []string{"keep_planning", "继续规划", "其他意见"} {
		if !IsKeepPlanningValue(input) {
			t.Errorf("%q got false, want true", input)
		}
	}
}

// TestIsKeepPlanningValue_普通拒绝不是继续规划 测试区分。
func TestIsKeepPlanningValue_普通拒绝不是继续规划(t *testing.T) {
	for _, input := range []string{"reject", "拒绝", "allow_once"} {
		if IsKeepPlanningValue(input) {
			t.Errorf("%q got true, want false", input)
		}
	}
}

// ──────────────────────────── IsEnterprise ────────────────────────────

// TestIsEnterprise_未设置按个人版 测试默认值。
func TestIsEnterprise_未设置按个人版(t *testing.T) {
	t.Setenv(envEdition, "")
	if IsEnterprise() {
		t.Error("未设置时应为个人版")
	}
}

// TestIsEnterprise_enterprise 测试企业版识别。
func TestIsEnterprise_enterprise(t *testing.T) {
	t.Setenv(envEdition, "enterprise")
	if !IsEnterprise() {
		t.Error("应识别为企业版")
	}
}

// TestIsEnterprise_大小写与空白 测试归一化。
func TestIsEnterprise_大小写与空白(t *testing.T) {
	t.Setenv(envEdition, "  ENTERPRISE  ")
	if !IsEnterprise() {
		t.Error("大小写与空白应被归一化")
	}
}

// TestIsEnterprise_其它值按个人版 测试非 enterprise 值。
func TestIsEnterprise_其它值按个人版(t *testing.T) {
	t.Setenv(envEdition, "personal")
	if IsEnterprise() {
		t.Error("personal 不是企业版")
	}
}
