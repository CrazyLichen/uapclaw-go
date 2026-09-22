package worktree

import (
	"context"
	fnmatch "github.com/danwakefield/fnmatch"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testManagerCtx 创建带超时的测试 context
func testManagerCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// setupGitRepo 在 t.TempDir() 中初始化一个 git 仓库并做初始提交
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}
	_ = exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	// 初始提交
	testFile := filepath.Join(dir, "README.md")
	_ = os.WriteFile(testFile, []byte("# test"), 0o644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "init").Run()
	return dir
}

// TestNewWorktreeManager 测试构造
func TestNewWorktreeManager(t *testing.T) {
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)
	if mgr == nil {
		t.Fatal("NewWorktreeManager 不应返回 nil")
	}
	if mgr.Backend() == nil {
		t.Error("默认 Backend 不应为 nil")
	}
	if mgr.Config().CleanupAfterDays != 30 {
		t.Errorf("CleanupAfterDays 期望 30，实际 %d", mgr.Config().CleanupAfterDays)
	}
}

// TestNewWorktreeManager_WithOptions 测试带选项构造
func TestNewWorktreeManager_WithOptions(t *testing.T) {
	cfg := NewWorktreeConfig()
	handler := func(_ context.Context, _ WorktreeEvent) error { return nil }
	mgr := NewWorktreeManager(cfg, nil, WithEventHandler(handler))
	if mgr.eventHandler == nil {
		t.Error("WithEventHandler 应设置 eventHandler")
	}
}

// TestOwnerSlug 测试 slug 派生
func TestOwnerSlug(t *testing.T) {
	tests := []struct {
		ownerID  string
		expected string
	}{
		{"1234567890ab", "teammate-12345678"},
		{"short", "teammate-short"},
	}
	for _, tt := range tests {
		got := ownerSlug(tt.ownerID)
		if got != tt.expected {
			t.Errorf("ownerSlug(%q) = %q, want %q", tt.ownerID, got, tt.expected)
		}
	}
}

// TestResolvePolicy 测试生命周期策略解析
func TestResolvePolicy(t *testing.T) {
	// AUTO → EPHEMERAL
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)
	if mgr.resolvePolicy() != WorktreeLifecyclePolicyEphemeral {
		t.Errorf("AUTO 应解析为 EPHEMERAL，实际 %s", mgr.resolvePolicy())
	}

	// DURABLE → DURABLE
	cfg.LifecyclePolicy = WorktreeLifecyclePolicyDurable
	mgr = NewWorktreeManager(cfg, nil)
	if mgr.resolvePolicy() != WorktreeLifecyclePolicyDurable {
		t.Errorf("DURABLE 应保持 DURABLE，实际 %s", mgr.resolvePolicy())
	}

	// EPHEMERAL → EPHEMERAL
	cfg.LifecyclePolicy = WorktreeLifecyclePolicyEphemeral
	mgr = NewWorktreeManager(cfg, nil)
	if mgr.resolvePolicy() != WorktreeLifecyclePolicyEphemeral {
		t.Errorf("EPHEMERAL 应保持 EPHEMERAL，实际 %s", mgr.resolvePolicy())
	}
}

// TestPostCreationSetup_Symlink 测试符号链接创建
func TestPostCreationSetup_Symlink(t *testing.T) {
	repoRoot := setupGitRepo(t)
	wtDir := t.TempDir()
	ctx := testManagerCtx(t)

	// 创建需要符号链接的目录
	venvDir := filepath.Join(repoRoot, ".venv")
	_ = os.MkdirAll(venvDir, 0o755)

	cfg := NewWorktreeConfig()
	cfg.SymlinkDirectories = []string{".venv"}
	mgr := NewWorktreeManager(cfg, nil)

	if err := mgr.postCreationSetup(ctx, repoRoot, wtDir); err != nil {
		t.Fatalf("postCreationSetup 失败: %v", err)
	}

	linkPath := filepath.Join(wtDir, ".venv")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("符号链接 .venv 不存在: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error(".venv 应为符号链接")
	}
}

// TestPostCreationSetup_IncludeFiles 测试 gitignored 文件拷贝
func TestPostCreationSetup_IncludeFiles(t *testing.T) {
	repoRoot := setupGitRepo(t)
	wtDir := t.TempDir()
	ctx := testManagerCtx(t)

	// 创建 .env.local 文件
	envFile := filepath.Join(repoRoot, ".env.local")
	_ = os.WriteFile(envFile, []byte("KEY=value"), 0o644)

	// 添加 .env.local 到 .gitignore
	gitignore := filepath.Join(repoRoot, ".gitignore")
	_ = os.WriteFile(gitignore, []byte(".env.local\n"), 0o644)
	_ = exec.Command("git", "-C", repoRoot, "add", ".gitignore").Run()
	_ = exec.Command("git", "-C", repoRoot, "commit", "-m", "add gitignore").Run()

	cfg := NewWorktreeConfig()
	cfg.IncludePatterns = []string{".env.local"}
	mgr := NewWorktreeManager(cfg, nil)

	if err := mgr.postCreationSetup(ctx, repoRoot, wtDir); err != nil {
		t.Fatalf("postCreationSetup 失败: %v", err)
	}

	copiedPath := filepath.Join(wtDir, ".env.local")
	if _, err := os.Stat(copiedPath); os.IsNotExist(err) {
		t.Error(".env.local 应被拷贝到 worktree")
	}
}

// TestPostCreationSetup_HooksPath 测试 hooks 路径配置
func TestPostCreationSetup_HooksPath(t *testing.T) {
	repoRoot := setupGitRepo(t)
	wtDir := t.TempDir()
	ctx := testManagerCtx(t)

	// 确保 .git/hooks 目录存在
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	_ = os.MkdirAll(hooksDir, 0o755)

	// 在 worktree 中做初始提交（否则 git config 会失败）
	_ = exec.Command("git", "init", wtDir).Run()
	_ = exec.Command("git", "-C", wtDir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", wtDir, "config", "user.name", "Test").Run()
	readme := filepath.Join(wtDir, "README.md")
	_ = os.WriteFile(readme, []byte("# test"), 0o644)
	_ = exec.Command("git", "-C", wtDir, "add", ".").Run()
	_ = exec.Command("git", "-C", wtDir, "commit", "-m", "init").Run()

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)

	mgr.configureHooksPath(ctx, repoRoot, wtDir)

	// 验证 core.hooksPath 已设置
	r := runGit(ctx, []string{"config", "core.hooksPath"}, wtDir)
	if !r.OK() {
		t.Error("core.hooksPath 应已设置")
	}
}

// TestFnmatchMatch 测试 fnmatch 模式匹配（对齐 Python fnmatch）
func TestFnmatchMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{".env.local", ".env.local", true},
		{".env.production", ".env.*", true},
		{"src/main.go", "*.go", true},
		{"anything", "*", true},
		{"other", ".env.local", false},
	}
	for _, tt := range tests {
		got := fnmatch.Match(tt.pattern, tt.name, 0)
		if got != tt.want {
			t.Errorf("fnmatch.Match(%q, %q, 0) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

// TestCountChanges_无变更 测试无变更场景
func TestCountChanges_无变更(t *testing.T) {
	repoRoot := setupGitRepo(t)
	ctx := testManagerCtx(t)

	// 获取 HEAD commit SHA
	headSHA, _ := revParse(ctx, "HEAD", repoRoot)

	session := &WorktreeSession{
		WorktreePath:       repoRoot,
		OriginalHeadCommit: headSHA,
	}

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)

	summary := mgr.CountChanges(ctx, session)
	if summary == nil {
		t.Fatal("CountChanges 不应返回 nil（仓库干净）")
	}
	if summary.ChangedFiles != 0 {
		t.Errorf("ChangedFiles 期望 0，实际 %d", summary.ChangedFiles)
	}
}

// TestCountChanges_无HeadCommit 测试 fail-closed
func TestCountChanges_无HeadCommit(t *testing.T) {
	repoRoot := setupGitRepo(t)
	ctx := testManagerCtx(t)

	session := &WorktreeSession{
		WorktreePath:       repoRoot,
		OriginalHeadCommit: "", // 空 → fail-closed
	}

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)

	summary := mgr.CountChanges(ctx, session)
	if summary != nil {
		t.Error("OriginalHeadCommit 为空时应返回 nil（fail-closed）")
	}
}

// TestCleanupWorktreesByPrefix_空目录 测试空 .worktrees 目录
func TestCleanupWorktreesByPrefix_空目录(t *testing.T) {
	_ = t.TempDir() // 预留
	ctx := testManagerCtx(t)

	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)

	// workspace 未设置 → 直接返回空
	removed, err := mgr.CleanupWorktreesByPrefix(ctx, "teammate-", false)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("无 workspace 时应返回空列表，实际: %v", removed)
	}
}

// TestExit_Keep 测试保留 worktree 退出
func TestExit_Keep(t *testing.T) {
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, nil)

	// 使用 manager 内部的 sessionState 注入 ctx，确保 Exit 清空的是同一个 state
	ctx := WithWorktreeSessionState(context.Background(), mgr.SessionState())

	session := &WorktreeSession{
		OriginalCWD:    "/home/user/project",
		WorktreePath:   "/tmp/worktree-test",
		WorktreeName:   "test",
		WorktreeBranch: "worktree-test",
	}
	SetCurrentSession(ctx, session)

	result, err := mgr.Exit(ctx, "keep", false)
	if err != nil {
		t.Fatalf("Exit(keep) 不应返回错误: %v", err)
	}
	if result["action"] != "keep" {
		t.Errorf("action 期望 keep，实际 %s", result["action"])
	}
	if GetCurrentSession(ctx) != nil {
		t.Error("Exit 后 session 应清空")
	}
}

// TestExit_Remove无变更 测试删除无变更 worktree
func TestExit_Remove无变更(t *testing.T) {
	repoRoot := setupGitRepo(t)
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	ctx := testManagerCtx(t)
	ctx = WithWorktreeSessionState(ctx, mgr.SessionState())

	headSHA, _ := revParse(ctx, "HEAD", repoRoot)

	session := &WorktreeSession{
		OriginalCWD:        repoRoot,
		WorktreePath:       repoRoot, // 使用同一目录（测试不真正删除）
		WorktreeName:       "test-remove",
		WorktreeBranch:     "worktree-test-remove",
		OriginalHeadCommit: headSHA,
	}
	SetCurrentSession(ctx, session)

	result, err := mgr.Exit(ctx, "remove", false)
	if err != nil {
		t.Fatalf("Exit(remove) 无变更不应返回错误: %v", err)
	}
	if result["action"] != "remove" {
		t.Errorf("action 期望 remove，实际 %s", result["action"])
	}
}

// TestExit_Remove有变更拒绝 测试 fail-closed
func TestExit_Remove有变更拒绝(t *testing.T) {
	repoRoot := setupGitRepo(t)
	cfg := NewWorktreeConfig()
	mgr := NewWorktreeManager(cfg, &fakeBackend{})
	ctx := testManagerCtx(t)
	ctx = WithWorktreeSessionState(ctx, mgr.SessionState())

	// 创建变更
	testFile := filepath.Join(repoRoot, "changed.txt")
	_ = os.WriteFile(testFile, []byte("modified"), 0o644)

	headSHA, _ := revParse(ctx, "HEAD", repoRoot)

	session := &WorktreeSession{
		OriginalCWD:        repoRoot,
		WorktreePath:       repoRoot,
		WorktreeName:       "test-deny",
		WorktreeBranch:     "worktree-test-deny",
		OriginalHeadCommit: headSHA,
	}
	SetCurrentSession(ctx, session)

	_, err := mgr.Exit(ctx, "remove", false)
	if err == nil {
		t.Error("有变更时 Exit(remove, discard=false) 应返回错误")
	}
	if !strings.Contains(err.Error(), "WORKTREE_EXIT_INVALID") && !strings.Contains(err.Error(), "未提交文件") {
		t.Errorf("错误应包含变更信息: %v", err)
	}
}

// fakeBackend 用于测试的假后端
type fakeBackend struct{}

func (f *fakeBackend) Create(_ context.Context, slug, _, targetPath string) (*WorktreeCreateResult, error) {
	return &WorktreeCreateResult{WorktreePath: targetPath, WorktreeBranch: WorktreeBranchName(slug)}, nil
}
func (f *fakeBackend) Remove(_ context.Context, _, _ string) bool { return true }
func (f *fakeBackend) Exists(_ context.Context, _ string) bool    { return false }
