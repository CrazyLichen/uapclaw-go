package config

import "testing"

// TestParsePositiveTokens 覆盖配置里可能出现的各种写法。
func TestParsePositiveTokens(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		want   int
		wantOK bool
	}{
		{"整数", 262144, 262144, true},
		{"int64", int64(4096), 4096, true},
		{"浮点整数", float64(2048), 2048, true},
		{"字符串纯数字", "131072", 131072, true},
		{"带 k 后缀", "256k", 262144, true},
		{"带空格单位", "256 K", 262144, true},
		{"带 kib", "256KiB", 262144, true},
		{"带 m 后缀", "1m", 1048576, true},
		{"带千分位", "1,048,576", 1048576, true},
		{"零", 0, 0, false},
		{"负数", -1, 0, false},
		{"布尔", true, 0, false},
		{"nil", nil, 0, false},
		{"小数", 1.5, 0, false},
		{"非法字符串", "abc", 0, false},
		{"超界", 1 << 54, 0, false},
	}
	for _, c := range cases {
		got, ok := ParsePositiveTokens(c.value)
		if ok != c.wantOK || got != c.want {
			t.Fatalf("%s: ParsePositiveTokens(%v) = (%d,%v), want (%d,%v)",
				c.name, c.value, got, ok, c.want, c.wantOK)
		}
	}
}
