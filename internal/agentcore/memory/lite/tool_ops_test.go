package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// TestValidateMemoryPath_非法穿越 测试目录穿越
func TestValidateMemoryPath_非法穿越(t *testing.T) {
	ws := newTestWorkspace(t, "memory")
	ok, _ := ValidateMemoryPath("../etc/passwd", ws)
	if ok {
		t.Error("Expected path traversal to be rejected")
	}
}

// TestValidateMemoryPath_绝对路径 测试绝对路径
func TestValidateMemoryPath_绝对路径(t *testing.T) {
	ws := newTestWorkspace(t, "memory")
	ok, _ := ValidateMemoryPath("/etc/passwd", ws)
	if ok {
		t.Error("Expected absolute path to be rejected")
	}
}

// TestValidateMemoryPath_合法路径 测试合法路径
func TestValidateMemoryPath_合法路径(t *testing.T) {
	ws := newTestWorkspace(t, "memory")
	ok, resolved := ValidateMemoryPath("test.md", ws)
	if !ok {
		t.Error("Expected valid path")
	}
	if resolved == "" {
		t.Error("Expected resolved path")
	}
}

// TestValidateMemoryPath_UserMD 测试 USER.md 路径解析
func TestValidateMemoryPath_UserMD(t *testing.T) {
	ws := newTestWorkspace(t, "memory", "USER.md")
	ok, _ := ValidateMemoryPath("USER.md", ws)
	if !ok {
		t.Error("Expected USER.md to be valid")
	}
}

// TestValidateMemoryPath_无Workspace 测试无 workspace
func TestValidateMemoryPath_无Workspace(t *testing.T) {
	ok, _ := ValidateMemoryPath("test.md", nil)
	if ok {
		t.Error("Expected nil workspace to be rejected")
	}
}

// TestMemorySearchWithContext_无Manager 测试无 manager
func TestMemorySearchWithContext_无Manager(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := MemorySearchWithContext(ctx, toolCtx, "test", nil, nil, "")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if !result.Disabled {
		t.Error("Expected disabled=true")
	}
}

// TestReadMemoryWithContext_无Workspace 测试无 workspace
func TestReadMemoryWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := ReadMemoryWithContext(ctx, toolCtx, "test.md", nil, nil)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Success {
		t.Error("Expected success=false")
	}
}

// TestEditMemoryWithContext_无Workspace 测试无 workspace
func TestEditMemoryWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := EditMemoryWithContext(ctx, toolCtx, "test.md", "old", "new")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Success {
		t.Error("Expected success=false")
	}
}

// TestWriteMemoryWithContext_无Workspace 测试无 workspace
func TestWriteMemoryWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &MemoryToolContext{}
	result := WriteMemoryWithContext(ctx, toolCtx, "test.md", "content", false)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Success {
		t.Error("Expected success=false")
	}
}

// TestLineRangeToFsRead 测试行范围转换
func TestLineRangeToFsRead(t *testing.T) {
	// 无 offset
	result := lineRangeToFsRead(nil, nil)
	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}
	// offset + limit
	offset := 5
	limit := 10
	result = lineRangeToFsRead(&offset, &limit)
	if result == nil {
		t.Fatal("Expected non-nil")
	}
	if (*result)[0] != 5 || (*result)[1] != 14 {
		t.Errorf("Expected [5, 14], got %v", *result)
	}
	// offset 无 limit
	result = lineRangeToFsRead(&offset, nil)
	if result == nil {
		t.Fatal("Expected non-nil")
	}
	if (*result)[0] != 5 || (*result)[1] != -1 {
		t.Errorf("Expected [5, -1], got %v", *result)
	}
}

// TestViewLines 测试行视图切片
func TestViewLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	// 全部
	text, total, start, end, truncated := viewLines(lines, nil, nil)
	if total != 5 || text != "a\nb\nc\nd\ne" || truncated {
		t.Errorf("Unexpected: text=%q total=%d start=%d end=%d truncated=%v", text, total, start, end, truncated)
	}
	// offset + limit
	offset := 2
	limit := 2
	text, _, start, end, truncated = viewLines(lines, &offset, &limit)
	if text != "b\nc" || start != 1 || end != 3 || !truncated {
		t.Errorf("Unexpected: text=%q start=%d end=%d truncated=%v", text, start, end, truncated)
	}
}

// TestViewLines_超出范围 测试超出范围
func TestViewLines_超出范围(t *testing.T) {
	lines := []string{"a", "b"}
	offset := 10
	limit := 5
	text, total, start, end, truncated := viewLines(lines, &offset, &limit)
	if total != 2 {
		t.Errorf("Expected total=2, got %d", total)
	}
	if text != "" {
		t.Errorf("Expected empty text for out-of-range, got %q", text)
	}
	if start != 2 {
		t.Errorf("Expected start=2, got %d", start)
	}
	_ = end
	_ = truncated
}

// newTestWorkspace 创建测试用 workspace
func newTestWorkspace(t *testing.T, nodeNames ...string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	dirs := make([]map[string]any, 0, len(nodeNames))
	for _, name := range nodeNames {
		nodeDir := filepath.Join(dir, name)
		if err := os.MkdirAll(nodeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, map[string]any{"name": name})
	}
	return &workspace.Workspace{
		RootPath:    dir,
		Directories: dirs,
	}
}
