package acpchat

import "testing"

// TestNormalizeProfileSpecForCLI 回归（T-09）：CLI 入口按 cli.py 宽松语义
// 丢弃类型不符的 args/env，避免被工具侧的严格校验拒掉。
func TestNormalizeProfileSpecForCLI(t *testing.T) {
	spec := map[string]any{
		"command": "npx",
		"args":    "not-a-list",
		"env":     "not-a-map",
		"cwd":     "/tmp",
	}
	got := NormalizeProfileSpecForCLI(spec)
	if _, exists := got["args"]; exists {
		t.Error("非 list 的 args 应被丢弃（对齐 cli.py）")
	}
	if _, exists := got["env"]; exists {
		t.Error("非 map 的 env 应被丢弃（对齐 cli.py）")
	}
	if got["command"] != "npx" || got["cwd"] != "/tmp" {
		t.Errorf("其他字段应原样保留: %+v", got)
	}
	if spec["args"] != "not-a-list" {
		t.Error("不应修改入参 map")
	}

	ok := map[string]any{"command": "npx", "args": []any{"a"}, "env": map[string]any{"K": "v"}}
	gotOK := NormalizeProfileSpecForCLI(ok)
	if _, exists := gotOK["args"]; !exists {
		t.Error("合法 args 应保留")
	}
	if _, exists := gotOK["env"]; !exists {
		t.Error("合法 env 应保留")
	}
}
