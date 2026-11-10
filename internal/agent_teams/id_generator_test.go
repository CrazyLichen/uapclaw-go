package agent_teams

import (
	"strings"
	"testing"
)

// TestGenerateID_前缀与长度 验证 kind 映射与 body 长度。
func TestGenerateID_前缀与长度(t *testing.T) {
	cases := map[string]string{
		"async_tool":    "x",
		"swarmflow":     "w",
		"session_spawn": "s",
		"unknown":       "t",
	}
	for kind, prefix := range cases {
		id := GenerateID(kind)
		if !strings.HasPrefix(id, prefix) {
			t.Fatalf("kind=%s id=%s 前缀应为 %s", kind, id, prefix)
		}
		if len(id) != len(prefix)+8 {
			t.Fatalf("kind=%s id=%s 长度应为 %d", kind, id, len(prefix)+8)
		}
	}
}

// TestGenerateID_自定义长度 验证 length 参数。
func TestGenerateID_自定义长度(t *testing.T) {
	if got := GenerateID("async_tool", 4); len(got) != 5 {
		t.Fatalf("长度应为 5，实际 %d (%s)", len(got), got)
	}
}
