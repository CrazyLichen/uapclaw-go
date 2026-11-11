// 与 Python 逐值对照的回归测试。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/reviewer_redaction.py
// 这些用例的期望值都是从 Python 源码真实跑出来的（不是推断），
// 用于防止后续改动悄悄偏离上游语义。

package redaction

import "testing"

func TestMaskParts_对齐Python(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/home/u/secret", "/[redacted]/[redacted]/[redacted]"},
		{"//a//b", "//[redacted]//[redacted]"},
		{"a/b", "[redacted]/[redacted]"},
		{"abc", "[redacted]"},
		{"/", "/"},
		{"", ""},
	}
	for _, c := range cases {
		if got := maskParts(c.in); got != c.want {
			t.Errorf("maskParts(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRequireLiteral_对齐Python(t *testing.T) {
	cases := []struct {
		val       string
		wantError bool
	}{
		{"$(whoami)", true},
		{"'$(whoami)'", false},
		{"abc123", false},
		// ⚠️ 反引号被 secretAssignmentPattern 排除，正则匹配不到值 → 不报错
		// （Python 同款行为，已用真实正则验证）
		{"`id`", false},
		{"a\\b", true},
		{"$x", true},
		{"'$x", false},
	}
	for _, c := range cases {
		_, err := RedactSecretValuesPreserveFormatE("password="+c.val, true)
		if (err != nil) != c.wantError {
			t.Errorf("password=%q -> err=%v, wantError=%v", c.val, err, c.wantError)
		}
	}
}

// TestSecretAssignment_大于号排除 验证正则排除 > （对齐 Python [^"'\s,;}&|<>`]）。
func TestSecretAssignment_大于号排除(t *testing.T) {
	// Python: password=a>b 的值部分只匹配到 "a"，> 留在外面
	got := RedactSecretValuesPreserveFormat("password=a>b")
	if got != "password=[redacted]>b" {
		t.Errorf("got %q, want %q（> 应被排除在值之外）", got, "password=[redacted]>b")
	}
}
