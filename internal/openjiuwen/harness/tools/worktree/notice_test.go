package worktree

import (
	"strings"
	"testing"
)

// TestBuildWorktreeNotice 测试输出格式
func TestBuildWorktreeNotice(t *testing.T) {
	result := BuildWorktreeNotice("/home/user/project", "/tmp/worktree-abc")

	if !strings.Contains(result, "/tmp/worktree-abc") {
		t.Error("输出应包含 worktree 路径")
	}
	if !strings.Contains(result, "/home/user/project") {
		t.Error("输出应包含 parent 路径")
	}
	if !strings.Contains(result, "isolated git worktree") {
		t.Error("输出应包含关键提示文本")
	}
	if !strings.Contains(result, "Important:") {
		t.Error("输出应包含 Important 段")
	}
}
