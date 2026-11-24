package config

import "testing"

// TestToolRetrievalEnabled_双开关门控 对齐 Python is_tool_retrieval_enabled（config.py:469-479）。
func TestToolRetrievalEnabled_双开关门控(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"nil 配置", nil, false},
		{"双缺省", map[string]any{}, false},
		{"仅 enabled", map[string]any{"progressive_tool_enabled": true}, false},
		{"仅 optimize", map[string]any{"progressive_tool_optimize": true}, false},
		{"双 true", map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": true}, true},
		{"字符串双开", map[string]any{"progressive_tool_enabled": "on", "progressive_tool_optimize": "1"}, true},
		{"optimize 带空格", map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": " true "}, true},
		{"optimize 非法串", map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": "false"}, false},
		{"enabled 关", map[string]any{"progressive_tool_enabled": false, "progressive_tool_optimize": true}, false},
	}
	for _, tc := range cases {
		if got := ToolRetrievalEnabled(tc.cfg); got != tc.want {
			t.Errorf("%s: want %v got %v", tc.name, tc.want, got)
		}
	}
}
