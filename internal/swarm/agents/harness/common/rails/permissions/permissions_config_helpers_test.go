package permissions

import (
	"testing"
)

// TestToolNameFromParams 验证工具名提取优先 tool 再 name。
//
// Python: str(params.get("tool") or params.get("name") or "").strip()
func TestToolNameFromParams(t *testing.T) {
	if got := toolNameFromParams(map[string]any{"tool": "bash"}); got != "bash" {
		t.Fatalf("应取 tool，实际 %q", got)
	}
	if got := toolNameFromParams(map[string]any{"name": "read_file"}); got != "read_file" {
		t.Fatalf("应取 name，实际 %q", got)
	}
	if got := toolNameFromParams(map[string]any{"tool": "  "}); got != "" {
		t.Fatalf("空白应归一为空串，实际 %q", got)
	}
	if got := toolNameFromParams(map[string]any{}); got != "" {
		t.Fatalf("空 params 应返回空串，实际 %q", got)
	}
}
