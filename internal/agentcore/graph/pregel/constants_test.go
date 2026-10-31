package pregel

import "testing"

// TestConstants_值与Python一致 逐值断言 11 颗常量。
// 任何字符串被误改（多/少下划线、大小写变动）都会在此失败。
func TestConstants_值与Python一致(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Start", Start, "__start__"},
		{"End", End, "__end__"},
		{"TaskStatusInterrupt", TaskStatusInterrupt, "__interrupt__"},
		{"TaskStatusError", TaskStatusError, "__error__"},
		{"NsSeparator", NsSeparator, ":"},
		{"NsReplaceChar", NsReplaceChar, "#"},
		{"NsKey", NsKey, "ns"},
		{"ParentNsKey", ParentNsKey, "parent_ns"},
		{"SessionIDKey", SessionIDKey, "session_id"},
		{"RecursionLimitKey", RecursionLimitKey, "recursion_limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q，期望 %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestConstants_递归上限为10000 断言 MaxRecursiveLimit 的数值。
func TestConstants_递归上限为10000(t *testing.T) {
	if MaxRecursiveLimit != 10000 {
		t.Errorf("MaxRecursiveLimit = %d，期望 10000", MaxRecursiveLimit)
	}
}
