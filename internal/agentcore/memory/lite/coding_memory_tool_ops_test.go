package lite

import (
	"context"
	"testing"
)

// TestValidateCodingMemoryPath_非法穿越 测试目录穿越
func TestValidateCodingMemoryPath_非法穿越(t *testing.T) {
	ws := newTestWorkspace(t, "coding_memory")
	ok, _ := ValidateCodingMemoryPath("../etc/passwd", ws)
	if ok {
		t.Error("Expected path traversal to be rejected")
	}
}

// TestValidateCodingMemoryPath_绝对路径 测试绝对路径
func TestValidateCodingMemoryPath_绝对路径(t *testing.T) {
	ws := newTestWorkspace(t, "coding_memory")
	ok, _ := ValidateCodingMemoryPath("/etc/passwd", ws)
	if ok {
		t.Error("Expected absolute path to be rejected")
	}
}

// TestValidateCodingMemoryPath_非MD文件 测试非 .md 后缀
func TestValidateCodingMemoryPath_非MD文件(t *testing.T) {
	ws := newTestWorkspace(t, "coding_memory")
	ok, _ := ValidateCodingMemoryPath("test.txt", ws)
	if ok {
		t.Error("Expected non-md file to be rejected")
	}
}

// TestValidateCodingMemoryPath_无Workspace 测试无 workspace
func TestValidateCodingMemoryPath_无Workspace(t *testing.T) {
	ok, _ := ValidateCodingMemoryPath("test.md", nil)
	if ok {
		t.Error("Expected nil workspace to be rejected")
	}
}

// TestValidateCodingMemoryPath_合法路径 测试合法路径
func TestValidateCodingMemoryPath_合法路径(t *testing.T) {
	ws := newTestWorkspace(t, "coding_memory")
	ok, resolved := ValidateCodingMemoryPath("test.md", ws)
	if !ok {
		t.Error("Expected valid path")
	}
	if resolved == "" {
		t.Error("Expected resolved path")
	}
}

// TestCodingMemoryReadWithContext_无Workspace 测试无 workspace
func TestCodingMemoryReadWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{}
	result := CodingMemoryReadWithContext(ctx, toolCtx, "test.md", nil, nil)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Success {
		t.Error("Expected success=false")
	}
}

// TestCodingMemoryWriteWithContext_无Workspace 测试无 workspace
func TestCodingMemoryWriteWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{}
	result := CodingMemoryWriteWithContext(ctx, toolCtx, "test.md", "content")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false")
	}
}

// TestCodingMemoryWriteWithContext_无Frontmatter 测试无 frontmatter
func TestCodingMemoryWriteWithContext_无Frontmatter(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace: newTestWorkspace(t, "coding_memory"),
		},
	}
	result := CodingMemoryWriteWithContext(ctx, toolCtx, "test.md", "no frontmatter")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false for missing frontmatter")
	}
}

// TestCodingMemoryWriteWithContext_不完整Frontmatter 测试不完整 frontmatter
func TestCodingMemoryWriteWithContext_不完整Frontmatter(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace: newTestWorkspace(t, "coding_memory"),
		},
	}
	// 缺少 type 字段
	result := CodingMemoryWriteWithContext(ctx, toolCtx, "test.md", "---\nname: test\ndescription: d\n---\nbody")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false for incomplete frontmatter")
	}
}

// TestCodingMemoryWriteWithContext_无Body 测试无 body
func TestCodingMemoryWriteWithContext_无Body(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace: newTestWorkspace(t, "coding_memory"),
		},
	}
	// 只有 frontmatter 无 body
	result := CodingMemoryWriteWithContext(ctx, toolCtx, "test.md", "---\nname: test\ndescription: d\ntype: user\n---")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result["success"] != false {
		t.Error("Expected success=false for no body")
	}
}

// TestCodingMemoryEditWithContext_无Workspace 测试无 workspace
func TestCodingMemoryEditWithContext_无Workspace(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{}
	result := CodingMemoryEditWithContext(ctx, toolCtx, "test.md", "old", "new")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Success {
		t.Error("Expected success=false")
	}
}

// TestCodingMemoryEditWithContext_空OldText 测试空 old_text
func TestCodingMemoryEditWithContext_空OldText(t *testing.T) {
	ctx := context.Background()
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace: newTestWorkspace(t, "coding_memory"),
		},
	}
	result := CodingMemoryEditWithContext(ctx, toolCtx, "test.md", "", "new")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Success {
		t.Error("Expected success=false for empty old_text")
	}
}

// TestResolveMemoryDir 测试解析记忆目录
func TestResolveMemoryDir(t *testing.T) {
	// 有 CodingMemoryDir
	ctx := &CodingMemoryToolContext{
		CodingMemoryDir: "/custom/dir",
	}
	result := resolveMemoryDir(ctx, "/resolved/path/test.md")
	if result != "/custom/dir" {
		t.Errorf("Expected /custom/dir, got %s", result)
	}
	// 无 CodingMemoryDir，从 resolved path 推导
	ctx2 := &CodingMemoryToolContext{}
	result2 := resolveMemoryDir(ctx2, "/memory/dir/test.md")
	if result2 != "/memory/dir" {
		t.Errorf("Expected /memory/dir, got %s", result2)
	}
}

// TestGetFileLock 测试文件锁获取
func TestGetFileLock(t *testing.T) {
	lock1 := getFileLock("/test/path1.md")
	lock2 := getFileLock("/test/path1.md")
	lock3 := getFileLock("/test/path2.md")
	if lock1 != lock2 {
		t.Error("Expected same lock for same path")
	}
	if lock1 == lock3 {
		t.Error("Expected different lock for different path")
	}
}

// TestSnapshotEqual 测试快照比较
func TestSnapshotEqual(t *testing.T) {
	if !snapshotEqual(nil, nil) {
		t.Error("Expected nil snapshots to be equal")
	}
	if !snapshotEqual(map[string]bool{"a.md": true, "b.md": true}, map[string]bool{"b.md": true, "a.md": true}) {
		t.Error("Expected same-content snapshots to be equal")
	}
	if snapshotEqual(map[string]bool{"a.md": true}, map[string]bool{"a.md": true, "b.md": true}) {
		t.Error("Expected different-length snapshots to be not equal")
	}
	if snapshotEqual(map[string]bool{"a.md": true}, map[string]bool{"b.md": true}) {
		t.Error("Expected different-content snapshots to be not equal")
	}
}

// TestSnapshotMemoryFiles_空目录 测试空目录
func TestSnapshotMemoryFiles_空目录(t *testing.T) {
	ctx := &CodingMemoryToolContext{}
	result := snapshotMemoryFiles(ctx, "")
	if result != nil {
		t.Errorf("Expected nil for empty dir, got %v", result)
	}
}
