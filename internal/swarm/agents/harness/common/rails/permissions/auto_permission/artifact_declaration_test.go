package auto_permission

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// TestRequestedFilePaths_绝对路径 验证原样保留。
func TestRequestedFilePaths_绝对路径(t *testing.T) {
	facts := permissions.ToolDecisionFacts{ReadPaths: []string{"/ws/a.txt"}}
	got := requestedFilePaths(facts)
	if len(got) != 1 || got[0] != "/ws/a.txt" {
		t.Errorf("got %v", got)
	}
}

// TestRequestedFilePaths_空输入 验证返回空切片。
func TestRequestedFilePaths_空输入(t *testing.T) {
	got := requestedFilePaths(permissions.ToolDecisionFacts{})
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// TestRequestedFilePaths_多条路径 验证顺序保持。
func TestRequestedFilePaths_多条路径(t *testing.T) {
	facts := permissions.ToolDecisionFacts{ReadPaths: []string{"/ws/a.txt", "/ws/b.txt"}}
	got := requestedFilePaths(facts)
	if len(got) != 2 || got[0] != "/ws/a.txt" || got[1] != "/ws/b.txt" {
		t.Errorf("got %v", got)
	}
}
