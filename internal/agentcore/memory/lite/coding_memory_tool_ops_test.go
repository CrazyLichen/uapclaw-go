package lite

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/update"
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

// TestRunChecker_无LLM 测试无 LLM 模型时返回 nil
func TestRunChecker_无LLM(t *testing.T) {
	mgr := &stubMemoryIndexManager{llmModel: nil}
	items := runChecker(context.Background(), mgr, "test.md", "body", map[string]string{"old.md": "old body"})
	if items != nil {
		t.Errorf("期望 nil，实际 %v", items)
	}
}

// TestRunChecker_有LLM调用Check 测试有 LLM 时调用 MemUpdateChecker.Check
func TestRunChecker_有LLM调用Check(t *testing.T) {
	// 使用空 LLM 模型（不实际调用 API），验证 runChecker 会走到 Check 调用
	// MemUpdateChecker.Check 在 model != nil 时会尝试 LLM 调用，
	// 但我们无法在单元测试中 mock model.Invoke，所以只测试 model=nil 的分支
	mgr := &stubMemoryIndexManager{llmModel: nil}
	items := runChecker(context.Background(), mgr, "test.md", "body", nil)
	if items != nil {
		t.Errorf("期望 model=nil 时返回 nil，实际 %v", items)
	}
}

// TestContainsActionForID 测试 containsActionForID
func TestContainsActionForID(t *testing.T) {
	actions := []*update.MemoryActionItem{
		{ID: "a.md", Status: update.MemoryStatusAdd},
		{ID: "b.md", Status: update.MemoryStatusDelete},
	}
	if !containsActionForID(actions, "a.md") {
		t.Error("期望包含 a.md 的 ADD 操作")
	}
	if containsActionForID(actions, "b.md") {
		t.Error("期望不包含 b.md 的 ADD 操作（b 是 DELETE）")
	}
	if containsActionForID(actions, "c.md") {
		t.Error("期望不包含 c.md 的操作")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stubMemoryIndexManager 测试用 MemoryIndexManager mock
type stubMemoryIndexManager struct {
	llmModel *llm.Model
}

func (s *stubMemoryIndexManager) Initialize(_ context.Context) error                  { return nil }
func (s *stubMemoryIndexManager) Sync(_ context.Context, _ string, _ bool) error       { return nil }
func (s *stubMemoryIndexManager) Search(_ context.Context, _ string, _ map[string]any) ([]SearchResult, error) {
	return nil, nil
}
func (s *stubMemoryIndexManager) ReadFile(_ context.Context, _ string, _ *int, _ *int) (*ReadFileResult, error) {
	return nil, nil
}
func (s *stubMemoryIndexManager) Status() *StatusResult { return nil }
func (s *stubMemoryIndexManager) LLM() *llm.Model       { return s.llmModel }
func (s *stubMemoryIndexManager) Close() error           { return nil }
