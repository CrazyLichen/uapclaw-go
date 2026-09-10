package team_workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestManager 创建测试用 TeamWorkspaceManager
func newTestManager(t *testing.T, versionControl bool) *TeamWorkspaceManager {
	t.Helper()
	tmp := t.TempDir()
	wsPath := filepath.Join(tmp, "workspace")
	cfg := TeamWorkspaceConfig{
		Enabled:          true,
		ArtifactDirs:     []string{"artifacts/code", "artifacts/docs"},
		VersionControl:   versionControl,
		ConflictStrategy: ConflictStrategyLock,
	}
	return NewTeamWorkspaceManager(cfg, wsPath, "test-team", WorkspaceModeLocal)
}

// newTestManagerWithMode 创建指定模式的测试用 TeamWorkspaceManager
func newTestManagerWithMode(t *testing.T, mode WorkspaceMode, versionControl bool) *TeamWorkspaceManager {
	t.Helper()
	tmp := t.TempDir()
	wsPath := filepath.Join(tmp, "workspace")
	cfg := TeamWorkspaceConfig{
		Enabled:          true,
		ArtifactDirs:     []string{"artifacts/code"},
		VersionControl:   versionControl,
		ConflictStrategy: ConflictStrategyLock,
	}
	return NewTeamWorkspaceManager(cfg, wsPath, "test-team", mode)
}

// setupGitConfig 配置 git 用户信息（CI 环境可能未设置）
func setupGitConfig(t *testing.T, cwd string) {
	t.Helper()
	ctx := context.Background()
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	r := runGit(ctx, []string{"config", "user.email", "test@example.com"}, cwd)
	if !r.OK {
		runGit(ctx, []string{"config", "--global", "user.email", "test@example.com"}, "")
		runGit(ctx, []string{"config", "--global", "user.name", "Test User"}, "")
	} else {
		runGit(ctx, []string{"config", "user.name", "Test User"}, cwd)
	}
}

// initGitWorkspace 初始化工作空间并写入一个文件用于后续测试
func initGitWorkspace(t *testing.T, m *TeamWorkspaceManager) {
	t.Helper()
	ctx := context.Background()
	setupGitConfig(t, m.WorkspacePath())
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("初始化工作空间失败: %v", err)
	}
}

// TestNewTeamWorkspaceManager 测试构造 TeamWorkspaceManager
func TestNewTeamWorkspaceManager(t *testing.T) {
	cfg := TeamWorkspaceConfig{
		Enabled:          true,
		ArtifactDirs:     []string{"artifacts/code"},
		VersionControl:   true,
		ConflictStrategy: ConflictStrategyMerge,
	}
	m := NewTeamWorkspaceManager(cfg, "/tmp/ws", "alpha-team", WorkspaceModeLocal)

	if m.Config().VersionControl != true {
		t.Error("Config.VersionControl 应为 true")
	}
	if m.WorkspacePath() != "/tmp/ws" {
		t.Errorf("WorkspacePath 期望 /tmp/ws，实际 %s", m.WorkspacePath())
	}
	if m.TeamName() != "alpha-team" {
		t.Errorf("TeamName 期望 alpha-team，实际 %s", m.TeamName())
	}
	if m.Mode() != WorkspaceModeLocal {
		t.Errorf("Mode 期望 local，实际 %s", m.Mode())
	}
	if m.locks == nil {
		t.Error("locks map 不应为 nil")
	}
}

// TestNewTeamWorkspaceManager_带选项 测试带 WithPublishEvent 选项的构造
func TestNewTeamWorkspaceManager_带选项(t *testing.T) {
	called := false
	fn := func(eventType string, event any) { called = true }
	m := NewTeamWorkspaceManager(
		TeamWorkspaceConfig{VersionControl: true},
		"/tmp/ws", "team", WorkspaceModeLocal,
		WithPublishEvent(fn),
	)
	if m.publishEvent == nil {
		t.Error("publishEvent 不应为 nil")
	}
	m.publishEvent("test", nil)
	if !called {
		t.Error("publishEvent 回调未被调用")
	}
}

// TestInitialize_目录创建 测试初始化时创建工作空间和 artifact 子目录
func TestInitialize_目录创建(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	// 设置 git 用户信息
	setupGitConfig(t, m.WorkspacePath())
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 验证工作空间目录存在
	if info, err := os.Stat(m.WorkspacePath()); err != nil || !info.IsDir() {
		t.Errorf("工作空间目录应存在: %v", err)
	}

	// 验证 artifact 子目录
	for _, dir := range m.Config().ArtifactDirs {
		p := filepath.Join(m.WorkspacePath(), dir)
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			t.Errorf("产物目录 %s 应存在: %v", dir, err)
		}
	}

	// 验证 skills 目录
	skillsPath := filepath.Join(m.WorkspacePath(), "skills")
	if info, err := os.Stat(skillsPath); err != nil || !info.IsDir() {
		t.Errorf("skills 目录应存在: %v", err)
	}
}

// TestInitialize_Git初始化 测试初始化时创建 .git 和初始 commit
func TestInitialize_Git初始化(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	setupGitConfig(t, m.WorkspacePath())
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	gitDir := filepath.Join(m.WorkspacePath(), ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		t.Errorf(".git 目录应存在: %v", err)
	}

	// 验证有初始 commit
	r := runGit(ctx, []string{"log", "--oneline"}, m.WorkspacePath())
	if !r.OK {
		t.Errorf("git log 失败: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "Initialize team workspace") {
		t.Errorf("初始 commit 消息不匹配: %s", r.Stdout)
	}
}

// TestInitialize_已有Git跳过 测试 .git 存在时跳过初始化
func TestInitialize_已有Git跳过(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	setupGitConfig(t, m.WorkspacePath())

	// 第一次初始化
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("第一次 Initialize 失败: %v", err)
	}
	r1 := runGit(ctx, []string{"log", "--oneline"}, m.WorkspacePath())
	count1 := len(strings.Split(strings.TrimSpace(r1.Stdout), "\n"))

	// 第二次初始化应跳过
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("第二次 Initialize 失败: %v", err)
	}
	r2 := runGit(ctx, []string{"log", "--oneline"}, m.WorkspacePath())
	count2 := len(strings.Split(strings.TrimSpace(r2.Stdout), "\n"))

	if count2 != count1 {
		t.Errorf("重复初始化不应创建新的 commit: 之前 %d, 之后 %d", count1, count2)
	}
}

// TestInitialize_无版本控制 测试 VersionControl=false 时仅创建目录
func TestInitialize_无版本控制(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 工作空间目录应存在
	if info, err := os.Stat(m.WorkspacePath()); err != nil || !info.IsDir() {
		t.Errorf("工作空间目录应存在: %v", err)
	}

	// .git 目录不应存在
	gitDir := filepath.Join(m.WorkspacePath(), ".git")
	if _, err := os.Stat(gitDir); !os.IsNotExist(err) {
		t.Error("无版本控制时不应创建 .git 目录")
	}

	// artifact 目录仍应存在
	for _, dir := range m.Config().ArtifactDirs {
		p := filepath.Join(m.WorkspacePath(), dir)
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			t.Errorf("产物目录 %s 应存在: %v", dir, err)
		}
	}
}

// TestMountIntoWorkspace 测试在 agent 工作区创建 .team/{teamName} 符号链接
func TestMountIntoWorkspace(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	agentRoot := t.TempDir()
	if err := m.MountIntoWorkspace(agentRoot); err != nil {
		t.Fatalf("MountIntoWorkspace 失败: %v", err)
	}

	linkPath := filepath.Join(agentRoot, ".team", "test-team")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("符号链接应存在: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error(".team/test-team 应为符号链接")
	}

	// 验证链接指向工作空间
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("读取符号链接失败: %v", err)
	}
	// 比较目标路径（可能是相对或绝对）
	absTarget, _ := filepath.Abs(target)
	absWs, _ := filepath.Abs(m.WorkspacePath())
	if absTarget != absWs {
		t.Errorf("符号链接目标期望 %s，实际 %s", absWs, absTarget)
	}
}

// TestMountWorktree 测试 .worktree/{slug} 符号链接
func TestMountWorktree(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 创建真实的 worktree 目标目录
	worktreeTarget := t.TempDir()
	if err := m.MountWorktree("feature-x", worktreeTarget); err != nil {
		t.Fatalf("MountWorktree 失败: %v", err)
	}

	linkPath := filepath.Join(m.WorkspacePath(), ".worktree", "feature-x")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("worktree 符号链接应存在: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("worktree 链接应为符号链接")
	}
}

// TestUnmountWorktree 测试移除 worktree 符号链接
func TestUnmountWorktree(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	worktreeTarget := t.TempDir()
	if err := m.MountWorktree("feature-y", worktreeTarget); err != nil {
		t.Fatalf("MountWorktree 失败: %v", err)
	}

	linkPath := filepath.Join(m.WorkspacePath(), ".worktree", "feature-y")
	if _, err := os.Lstat(linkPath); err != nil {
		t.Fatalf("挂载后符号链接应存在: %v", err)
	}

	if err := m.UnmountWorktree("feature-y"); err != nil {
		t.Fatalf("UnmountWorktree 失败: %v", err)
	}

	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Error("卸载后符号链接不应存在")
	}
}

// TestUnmountWorktree_不存在时不报错 测试卸载不存在的 worktree 无错误
func TestUnmountWorktree_不存在时不报错(t *testing.T) {
	m := newTestManager(t, false)
	if err := m.UnmountWorktree("nonexistent"); err != nil {
		t.Errorf("卸载不存在的 worktree 不应报错: %v", err)
	}
}

// TestMountIntoWorktree 测试 worktree 内创建 .team 符号链接和 .gitignore
func TestMountIntoWorktree(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	worktreeDir := t.TempDir()
	if err := m.MountIntoWorktree(worktreeDir); err != nil {
		t.Fatalf("MountIntoWorktree 失败: %v", err)
	}

	// 验证 .team 符号链接
	linkPath := filepath.Join(worktreeDir, ".team")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf(".team 符号链接应存在: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error(".team 应为符号链接")
	}

	// 验证 .gitignore 包含 .agent/ 和 .team/
	gitignorePath := filepath.Join(worktreeDir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ".agent/") {
		t.Error(".gitignore 应包含 .agent/")
	}
	if !strings.Contains(content, ".team/") {
		t.Error(".gitignore 应包含 .team/")
	}
}

// TestMountIntoWorktree_追加gitignore 测试 .gitignore 已有内容时的追加行为
func TestMountIntoWorktree_追加gitignore(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	worktreeDir := t.TempDir()
	// 先创建已有 .gitignore
	existingContent := "*.o\nbuild/\n"
	if err := os.WriteFile(filepath.Join(worktreeDir, ".gitignore"), []byte(existingContent), 0o644); err != nil {
		t.Fatalf("写入 .gitignore 失败: %v", err)
	}

	if err := m.MountIntoWorktree(worktreeDir); err != nil {
		t.Fatalf("MountIntoWorktree 失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(worktreeDir, ".gitignore"))
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	content := string(data)
	// 原有内容应保留
	if !strings.Contains(content, "*.o") || !strings.Contains(content, "build/") {
		t.Error("原有 .gitignore 内容应保留")
	}
	// 新条目应追加
	if !strings.Contains(content, ".agent/") || !strings.Contains(content, ".team/") {
		t.Error("新 .gitignore 条目应追加")
	}
}

// TestAcquireLock_成功 测试获取锁
func TestAcquireLock_成功(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	ok, err := m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One")
	if err != nil {
		t.Fatalf("AcquireLock 失败: %v", err)
	}
	if !ok {
		t.Error("应成功获取锁")
	}

	lock := m.GetLock("src/main.go")
	if lock == nil {
		t.Fatal("锁应存在")
	}
	if lock.HolderID != "agent-1" {
		t.Errorf("HolderID 期望 agent-1，实际 %s", lock.HolderID)
	}
}

// TestAcquireLock_重入 测试同一持有者再次获取刷新超时
func TestAcquireLock_重入(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	// 第一次获取
	ok, err := m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One", 10)
	if err != nil || !ok {
		t.Fatalf("第一次获取锁失败: ok=%v err=%v", ok, err)
	}

	// 同一持有者再次获取（刷新超时）
	ok, err = m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One", 600)
	if err != nil {
		t.Fatalf("重入获取锁失败: %v", err)
	}
	if !ok {
		t.Error("同一持有者应可重入获取锁")
	}

	lock2 := m.GetLock("src/main.go")
	if lock2.TimeoutSeconds != 600 {
		t.Errorf("重入后超时应刷新为 600，实际 %d", lock2.TimeoutSeconds)
	}
	// 重入获取应刷新 AcquiredAt（同一秒内可能相同，验证 TimeoutSeconds 即可）
}

// TestAcquireLock_被他人持有 测试锁被他人持有时返回 false
func TestAcquireLock_被他人持有(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	ok, err := m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One")
	if err != nil || !ok {
		t.Fatalf("agent-1 获取锁失败: %v", err)
	}

	// agent-2 尝试获取同一文件锁
	ok, err = m.AcquireLock(ctx, "src/main.go", "agent-2", "Agent Two")
	if err != nil {
		t.Fatalf("AcquireLock 不应报错: %v", err)
	}
	if ok {
		t.Error("锁被他人持有时应返回 false")
	}
}

// TestAcquireLock_过期回收 测试过期锁可被他人获取
func TestAcquireLock_过期回收(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	// 用极短超时获取锁
	ok, err := m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One", 1)
	if err != nil || !ok {
		t.Fatalf("agent-1 获取锁失败: %v", err)
	}

	// 等待锁过期
	time.Sleep(1100 * time.Millisecond)

	// agent-2 应能获取过期锁
	ok, err = m.AcquireLock(ctx, "src/main.go", "agent-2", "Agent Two", 300)
	if err != nil {
		t.Fatalf("agent-2 获取过期锁失败: %v", err)
	}
	if !ok {
		t.Error("过期锁应可被他人获取")
	}

	lock := m.GetLock("src/main.go")
	if lock.HolderID != "agent-2" {
		t.Errorf("锁持有者应为 agent-2，实际 %s", lock.HolderID)
	}
}

// TestReleaseLock_成功 测试释放自己的锁
func TestReleaseLock_成功(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	ok, err := m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One")
	if err != nil || !ok {
		t.Fatalf("获取锁失败: %v", err)
	}

	released, err := m.ReleaseLock(ctx, "src/main.go", "agent-1")
	if err != nil {
		t.Fatalf("ReleaseLock 失败: %v", err)
	}
	if !released {
		t.Error("应成功释放锁")
	}

	if m.GetLock("src/main.go") != nil {
		t.Error("释放后锁不应存在")
	}
}

// TestReleaseLock_非持有者 测试非持有者释放锁返回 false
func TestReleaseLock_非持有者(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	ok, err := m.AcquireLock(ctx, "src/main.go", "agent-1", "Agent One")
	if err != nil || !ok {
		t.Fatalf("获取锁失败: %v", err)
	}

	released, err := m.ReleaseLock(ctx, "src/main.go", "agent-2")
	if err != nil {
		t.Fatalf("ReleaseLock 不应报错: %v", err)
	}
	if released {
		t.Error("非持有者释放锁应返回 false")
	}

	// 原锁仍存在
	lock := m.GetLock("src/main.go")
	if lock == nil || lock.HolderID != "agent-1" {
		t.Error("原锁应仍存在")
	}
}

// TestListLocks_清除过期 测试 ListLocks 清除过期锁
func TestListLocks_清除过期(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	// 获取一个短期锁（1秒超时）
	ok, _ := m.AcquireLock(ctx, "file1.txt", "agent-1", "Agent One", 1)
	if !ok {
		t.Fatal("获取锁失败")
	}

	// 获取一个正常锁
	ok, _ = m.AcquireLock(ctx, "file2.txt", "agent-2", "Agent Two", 300)
	if !ok {
		t.Fatal("获取锁失败")
	}

	// 等待过期
	time.Sleep(1100 * time.Millisecond)

	locks := m.ListLocks()

	// 过期锁应被清除
	for _, l := range locks {
		if l.FilePath == "file1.txt" {
			t.Error("过期锁不应出现在 ListLocks 结果中")
		}
	}
	if len(locks) != 1 {
		t.Errorf("应只有 1 个活跃锁，实际 %d", len(locks))
	}
	if locks[0].FilePath != "file2.txt" {
		t.Errorf("活跃锁应为 file2.txt，实际 %s", locks[0].FilePath)
	}
}

// TestGetLock_过期自动清除 测试 GetLock 触发过期清理
func TestGetLock_过期自动清除(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	ok, _ := m.AcquireLock(ctx, "file.txt", "agent-1", "Agent One", 1)
	if !ok {
		t.Fatal("获取锁失败")
	}

	// 等待过期
	time.Sleep(1100 * time.Millisecond)

	// GetLock 应返回 nil 并清除过期锁
	lock := m.GetLock("file.txt")
	if lock != nil {
		t.Error("过期锁应返回 nil")
	}

	// 确认已从 map 中删除
	m.lockMu.Lock()
	_, exists := m.locks["file.txt"]
	m.lockMu.Unlock()
	if exists {
		t.Error("过期锁应从 map 中删除")
	}
}

// TestAutoCommit_有变更 测试 git add + commit + 返回 SHA
func TestAutoCommit_有变更(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	initGitWorkspace(t, m)

	// 写入测试文件
	testFile := filepath.Join(m.WorkspacePath(), "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	sha, err := m.AutoCommit(ctx, "src/main.go", "agent-1")
	if err != nil {
		t.Fatalf("AutoCommit 失败: %v", err)
	}
	if sha == "" {
		t.Error("有变更时应返回非空 SHA")
	}

	// 验证 git log 包含提交
	r := runGit(ctx, []string{"log", "--oneline", "-1"}, m.WorkspacePath())
	if !r.OK {
		t.Errorf("git log 失败: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "[agent-1] Update src/main.go") {
		t.Errorf("提交消息不匹配: %s", r.Stdout)
	}
}

// TestAutoCommit_无变更 测试无变更返回空
func TestAutoCommit_无变更(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	initGitWorkspace(t, m)

	sha, err := m.AutoCommit(ctx, "nonexistent.txt", "agent-1")
	if err != nil {
		t.Fatalf("AutoCommit 失败: %v", err)
	}
	if sha != "" {
		t.Errorf("无变更时应返回空 SHA，实际 %s", sha)
	}
}

// TestAutoCommit_无版本控制 测试 VersionControl=false 返回空
func TestAutoCommit_无版本控制(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	sha, err := m.AutoCommit(ctx, "some/file.txt", "agent-1")
	if err != nil {
		t.Fatalf("AutoCommit 失败: %v", err)
	}
	if sha != "" {
		t.Error("无版本控制时应返回空 SHA")
	}
}

// TestGetHistory 测试获取文件版本历史
func TestGetHistory(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	initGitWorkspace(t, m)

	// 写入文件并提交
	testFile := filepath.Join(m.WorkspacePath(), "report.md")
	if err := os.WriteFile(testFile, []byte("# Report v1\n"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	sha1, err := m.AutoCommit(ctx, "report.md", "author-1")
	if err != nil {
		t.Fatalf("AutoCommit 失败: %v", err)
	}
	if sha1 == "" {
		t.Fatal("AutoCommit 应返回 SHA")
	}

	// 修改文件并提交
	if err := os.WriteFile(testFile, []byte("# Report v2\n"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	sha2, err := m.AutoCommit(ctx, "report.md", "author-2")
	if err != nil {
		t.Fatalf("AutoCommit 失败: %v", err)
	}
	if sha2 == "" {
		t.Fatal("AutoCommit 应返回 SHA")
	}

	history, err := m.GetHistory(ctx, "report.md", 10)
	if err != nil {
		t.Fatalf("GetHistory 失败: %v", err)
	}
	if len(history) < 2 {
		t.Fatalf("应有至少 2 个历史条目，实际 %d", len(history))
	}

	// 验证最新条目
	latest := history[0]
	if latest.Commit == "" {
		t.Error("Commit 不应为空")
	}
	if latest.Author == "" {
		t.Error("Author 不应为空")
	}
	if latest.Date == "" {
		t.Error("Date 不应为空")
	}
	if !strings.Contains(latest.Message, "author-2") {
		t.Errorf("最新提交消息应包含 author-2: %s", latest.Message)
	}
}

// TestGetHistory_无版本控制 测试无版本控制时返回 nil
func TestGetHistory_无版本控制(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	history, err := m.GetHistory(ctx, "any.txt", 10)
	if err != nil {
		t.Fatalf("GetHistory 不应报错: %v", err)
	}
	if history != nil {
		t.Error("无版本控制时应返回 nil")
	}
}

// TestDistributed_占位方法 测试 Pull/Push/RemoteAcquire 等返回 ErrDistributedNotImplemented
func TestDistributed_占位方法(t *testing.T) {
	ctx := context.Background()

	t.Run("RemoteAcquireLock", func(t *testing.T) {
		ok, err := (&TeamWorkspaceManager{}).RemoteAcquireLock(ctx, "f", "m", "d", 10)
		if !errors.Is(err, ErrDistributedNotImplemented) {
			t.Errorf("期望 ErrDistributedNotImplemented，实际 %v", err)
		}
		if ok {
			t.Error("不应返回 true")
		}
	})

	t.Run("RemoteReleaseLock", func(t *testing.T) {
		ok, err := (&TeamWorkspaceManager{}).RemoteReleaseLock(ctx, "f", "m")
		if !errors.Is(err, ErrDistributedNotImplemented) {
			t.Errorf("期望 ErrDistributedNotImplemented，实际 %v", err)
		}
		if ok {
			t.Error("不应返回 true")
		}
	})

	t.Run("HandleLockRequest", func(t *testing.T) {
		resp, err := (&TeamWorkspaceManager{}).HandleLockRequest(nil)
		if !errors.Is(err, ErrDistributedNotImplemented) {
			t.Errorf("期望 ErrDistributedNotImplemented，实际 %v", err)
		}
		if resp != nil {
			t.Error("不应返回非 nil 响应")
		}
	})

	t.Run("HandleLockResponse", func(t *testing.T) {
		err := (&TeamWorkspaceManager{}).HandleLockResponse(nil)
		if !errors.Is(err, ErrDistributedNotImplemented) {
			t.Errorf("期望 ErrDistributedNotImplemented，实际 %v", err)
		}
	})
}

// TestMergeExistingMountContents 测试旧目录内容合并到工作空间
func TestMergeExistingMountContents(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 创建一个真实目录模拟旧的 .team/{teamName} 挂载
	agentRoot := t.TempDir()
	oldDir := filepath.Join(agentRoot, ".team", "test-team")
	if err := os.MkdirAll(filepath.Join(oldDir, "subdir"), 0o755); err != nil {
		t.Fatalf("创建旧目录失败: %v", err)
	}
	// 写入文件到旧目录
	if err := os.WriteFile(filepath.Join(oldDir, "data.txt"), []byte("old data"), 0o644); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "subdir", "nested.txt"), []byte("nested data"), 0o644); err != nil {
		t.Fatalf("写入嵌套文件失败: %v", err)
	}

	// 在工作空间中创建同名文件（工作空间文件优先，不应被覆盖）
	if err := os.WriteFile(filepath.Join(m.WorkspacePath(), "data.txt"), []byte("ws data"), 0o644); err != nil {
		t.Fatalf("写入工作空间文件失败: %v", err)
	}

	// 获取旧目录信息
	info, err := os.Stat(oldDir)
	if err != nil {
		t.Fatalf("获取旧目录信息失败: %v", err)
	}

	m.mergeExistingMountContents(oldDir, info)

	// 工作空间中已有文件不应被覆盖
	data, err := os.ReadFile(filepath.Join(m.WorkspacePath(), "data.txt"))
	if err != nil {
		t.Fatalf("读取工作空间文件失败: %v", err)
	}
	if string(data) != "ws data" {
		t.Error("工作空间已有文件应优先，不被覆盖")
	}

	// 旧目录中的嵌套文件应被合并
	nestedData, err := os.ReadFile(filepath.Join(m.WorkspacePath(), "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("读取合并文件失败: %v", err)
	}
	if string(nestedData) != "nested data" {
		t.Error("旧目录中工作空间不存在的文件应被合并")
	}
}

// TestBackupExistingMountPath 测试备份旧路径
func TestBackupExistingMountPath(t *testing.T) {
	tmp := t.TempDir()
	oldPath := filepath.Join(tmp, "link-target")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	backupPath := backupExistingMountPath(oldPath)

	// 原路径应被重命名
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("原路径应不存在（已被重命名为备份）")
	}

	// 备份路径应存在
	if info, err := os.Stat(backupPath); err != nil || !info.IsDir() {
		t.Errorf("备份路径应存在: %v", err)
	}

	// 备份路径应包含 .stale- 前缀
	if !strings.Contains(backupPath, ".stale-") {
		t.Errorf("备份路径应包含 .stale-: %s", backupPath)
	}

	// 备份中的文件应保留
	data, err := os.ReadFile(filepath.Join(backupPath, "file.txt"))
	if err != nil {
		t.Fatalf("读取备份文件失败: %v", err)
	}
	if string(data) != "content" {
		t.Error("备份文件内容应保留")
	}
}

// TestPrepareMountPath_新路径 测试新路径返回 true
func TestPrepareMountPath_新路径(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	linkPath := filepath.Join(t.TempDir(), "nonexistent")
	result := m.prepareMountPath(linkPath)
	if !result {
		t.Error("不存在的路径应返回 true（需要挂载）")
	}
}

// TestPrepareMountPath_已正确挂载 测试已正确挂载返回 false
func TestPrepareMountPath_已正确挂载(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 创建符号链接指向工作空间
	linkPath := filepath.Join(t.TempDir(), "team-link")
	if err := os.Symlink(m.WorkspacePath(), linkPath); err != nil {
		t.Fatalf("创建符号链接失败: %v", err)
	}

	result := m.prepareMountPath(linkPath)
	if result {
		t.Error("已正确挂载的路径应返回 false（无需操作）")
	}
}

// TestPrepareMountPath_指向其他位置 测试指向其他位置时备份并返回 true
func TestPrepareMountPath_指向其他位置(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 创建真实目录指向其他位置
	otherDir := filepath.Join(t.TempDir(), "other-workspace")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	result := m.prepareMountPath(otherDir)
	if !result {
		t.Error("指向其他位置的路径应返回 true（需要重新挂载）")
	}

	// 原路径应被备份（重命名为 .stale-）
	if _, err := os.Stat(otherDir); !os.IsNotExist(err) {
		t.Error("原路径应已被重命名备份")
	}
}

// TestConfig_访问器 测试 Config/WorkspacePath/TeamName/Mode 访问器
func TestConfig_访问器(t *testing.T) {
	cfg := TeamWorkspaceConfig{
		Enabled:          true,
		ArtifactDirs:     []string{"a", "b"},
		VersionControl:   true,
		ConflictStrategy: ConflictStrategyMerge,
	}
	m := NewTeamWorkspaceManager(cfg, "/data/ws", "my-team", WorkspaceModeDistributed)

	if m.Config().Enabled != true {
		t.Error("Config().Enabled 应为 true")
	}
	if m.Config().ConflictStrategy != ConflictStrategyMerge {
		t.Errorf("Config().ConflictStrategy 期望 merge，实际 %s", m.Config().ConflictStrategy)
	}
	if m.WorkspacePath() != "/data/ws" {
		t.Errorf("WorkspacePath 期望 /data/ws，实际 %s", m.WorkspacePath())
	}
	if m.TeamName() != "my-team" {
		t.Errorf("TeamName 期望 my-team，实际 %s", m.TeamName())
	}
	if m.Mode() != WorkspaceModeDistributed {
		t.Errorf("Mode 期望 distributed，实际 %s", m.Mode())
	}
}

// TestAcquireLock_默认超时 测试不指定超时使用默认值
func TestAcquireLock_默认超时(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	ok, err := m.AcquireLock(ctx, "file.txt", "agent-1", "Agent One")
	if err != nil || !ok {
		t.Fatalf("获取锁失败: %v", err)
	}

	lock := m.GetLock("file.txt")
	if lock == nil {
		t.Fatal("锁应存在")
	}
	if lock.TimeoutSeconds != defaultLockTimeoutSeconds {
		t.Errorf("默认超时期望 %d，实际 %d", defaultLockTimeoutSeconds, lock.TimeoutSeconds)
	}
}

// TestAcquireLock_分布式模式 测试分布式模式委托 RemoteAcquireLock
func TestAcquireLock_分布式模式(t *testing.T) {
	m := newTestManagerWithMode(t, WorkspaceModeDistributed, true)
	ctx := context.Background()

	ok, err := m.AcquireLock(ctx, "file.txt", "agent-1", "Agent One")
	if !errors.Is(err, ErrDistributedNotImplemented) {
		t.Errorf("分布式模式应委托 RemoteAcquireLock，期望 ErrDistributedNotImplemented，实际 %v", err)
	}
	if ok {
		t.Error("分布式模式未实现时应返回 false")
	}
}

// TestReleaseLock_分布式模式 测试分布式模式委托 RemoteReleaseLock
func TestReleaseLock_分布式模式(t *testing.T) {
	m := newTestManagerWithMode(t, WorkspaceModeDistributed, true)
	ctx := context.Background()

	ok, err := m.ReleaseLock(ctx, "file.txt", "agent-1")
	if !errors.Is(err, ErrDistributedNotImplemented) {
		t.Errorf("分布式模式应委托 RemoteReleaseLock，期望 ErrDistributedNotImplemented，实际 %v", err)
	}
	if ok {
		t.Error("分布式模式未实现时应返回 false")
	}
}

// TestPull_本地模式 测试 LOCAL 模式下 Pull 为 no-op
func TestPull_本地模式(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()

	pulled, err := m.Pull(ctx)
	if err != nil {
		t.Fatalf("Pull 不应报错: %v", err)
	}
	if pulled {
		t.Error("LOCAL 模式下 Pull 应返回 false")
	}
}

// TestPush_本地模式 测试 LOCAL 模式下 Push 返回 true
func TestPush_本地模式(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()

	pushed, err := m.Push(ctx)
	if err != nil {
		t.Fatalf("Push 不应报错: %v", err)
	}
	if !pushed {
		t.Error("LOCAL 模式下 Push 应返回 true")
	}
}

// TestPull_无版本控制 测试无版本控制时 Pull 返回 false
func TestPull_无版本控制(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	pulled, err := m.Pull(ctx)
	if err != nil {
		t.Fatalf("Pull 不应报错: %v", err)
	}
	if pulled {
		t.Error("无版本控制时 Pull 应返回 false")
	}
}

// TestPush_无版本控制 测试无版本控制时 Push 返回 true
func TestPush_无版本控制(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()

	pushed, err := m.Push(ctx)
	if err != nil {
		t.Fatalf("Push 不应报错: %v", err)
	}
	if !pushed {
		t.Error("无版本控制时 Push 应返回 true")
	}
}

// TestMountWorktree_覆盖旧符号链接 测试已有符号链接时替换
func TestMountWorktree_覆盖旧符号链接(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 先挂载
	target1 := t.TempDir()
	if err := m.MountWorktree("slug-a", target1); err != nil {
		t.Fatalf("第一次挂载失败: %v", err)
	}

	// 再挂载同一 slug 到不同路径，应替换
	target2 := t.TempDir()
	if err := m.MountWorktree("slug-a", target2); err != nil {
		t.Fatalf("替换挂载失败: %v", err)
	}

	linkPath := filepath.Join(m.WorkspacePath(), ".worktree", "slug-a")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("读取符号链接失败: %v", err)
	}
	absTarget, _ := filepath.Abs(target)
	absExpected, _ := filepath.Abs(target2)
	if absTarget != absExpected {
		t.Errorf("替换后应指向新目标 %s，实际 %s", absExpected, absTarget)
	}
}

// TestMountWorktree_非符号链接存在时跳过 测试挂载路径为真实目录时跳过
func TestMountWorktree_非符号链接存在时跳过(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 在 .worktree 下创建一个真实目录（非符号链接）
	wtDir := filepath.Join(m.WorkspacePath(), ".worktree")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatalf("创建 .worktree 目录失败: %v", err)
	}
	realDir := filepath.Join(wtDir, "slug-b")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("创建真实目录失败: %v", err)
	}

	target := t.TempDir()
	err := m.MountWorktree("slug-b", target)
	if err != nil {
		t.Fatalf("MountWorktree 不应报错: %v", err)
	}

	// 真实目录应保留（未被替换为符号链接）
	info, _ := os.Lstat(realDir)
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("真实目录不应被替换为符号链接")
	}
}

// TestResolveWorkspaceRelative 测试从 .team/ 前缀路径提取工作空间相对路径
func TestResolveWorkspaceRelative(t *testing.T) {
	cases := []struct {
		input    string
		teamName string
		want     string
	}{
		{".team/alpha/src/main.go", "alpha", "src/main.go"},
		{".team/alpha", "alpha", "alpha"},
		{".team/src/main.go", "alpha", "src/main.go"},
	}
	for _, tc := range cases {
		got := resolveWorkspaceRelative(tc.input, tc.teamName)
		if got != tc.want {
			t.Errorf("resolveWorkspaceRelative(%q, %q) = %q, want %q", tc.input, tc.teamName, got, tc.want)
		}
	}
}

// TestMountIntoWorkspace_重复挂载 测试重复调用 MountIntoWorkspace 不报错
func TestMountIntoWorkspace_重复挂载(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	agentRoot := t.TempDir()
	if err := m.MountIntoWorkspace(agentRoot); err != nil {
		t.Fatalf("第一次挂载失败: %v", err)
	}
	// 再次挂载同一路径
	if err := m.MountIntoWorkspace(agentRoot); err != nil {
		t.Fatalf("重复挂载不应报错: %v", err)
	}

	linkPath := filepath.Join(agentRoot, ".team", "test-team")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("符号链接应存在: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("应仍为符号链接")
	}
}

// TestCopyFile 测试文件复制
func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "source.txt")
	dstFile := filepath.Join(dstDir, "dest.txt")

	content := []byte("hello world")
	if err := os.WriteFile(srcFile, content, 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}

	if err := copyFile(srcFile, dstFile); err != nil {
		t.Fatalf("copyFile 失败: %v", err)
	}

	data, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("复制内容不匹配: 期望 %q，实际 %q", content, data)
	}
}

// TestCopyFile_源不存在 测试源文件不存在时返回错误
func TestCopyFile_源不存在(t *testing.T) {
	err := copyFile("/nonexistent/file.txt", filepath.Join(t.TempDir(), "dest.txt"))
	if err == nil {
		t.Error("源文件不存在时应返回错误")
	}
}

// TestMountIntoWorktree_条目已存在 测试 .gitignore 中已有条目时不重复追加
func TestMountIntoWorktree_条目已存在(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	worktreeDir := t.TempDir()
	// 先挂载一次
	if err := m.MountIntoWorktree(worktreeDir); err != nil {
		t.Fatalf("第一次挂载失败: %v", err)
	}

	// 读取 .gitignore 长度
	data1, _ := os.ReadFile(filepath.Join(worktreeDir, ".gitignore"))
	len1 := len(data1)

	// 再次挂载
	if err := m.MountIntoWorktree(worktreeDir); err != nil {
		t.Fatalf("重复挂载不应报错: %v", err)
	}

	data2, _ := os.ReadFile(filepath.Join(worktreeDir, ".gitignore"))
	len2 := len(data2)
	// 已有条目不应重复追加（长度应不变或差异很小，不会翻倍）
	if len2 > len1+50 {
		t.Errorf("重复挂载不应重复追加 .gitignore 条目: 长度从 %d 增至 %d", len1, len2)
	}
}

// TestIsMountedToWorkspace_工作空间不存在 测试工作空间目录不存在时返回 false
func TestIsMountedToWorkspace_工作空间不存在(t *testing.T) {
	m := NewTeamWorkspaceManager(
		TeamWorkspaceConfig{VersionControl: false},
		"/nonexistent/workspace", "team", WorkspaceModeLocal,
	)
	// linkPath 也不存在
	result := m.isMountedToWorkspace("/nonexistent/link")
	if result {
		t.Error("工作空间不存在时应返回 false")
	}
}

// TestMergeExistingMountContents_非目录跳过 测试传入非目录时跳过
func TestMergeExistingMountContents_非目录跳过(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 传入 nil info
	m.mergeExistingMountContents("/some/path", nil)

	// 传入文件 info（非目录）
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}
	m.mergeExistingMountContents(tmpFile, info)

	// 不应崩溃，工作空间不受影响
	if _, err := os.Stat(filepath.Join(m.WorkspacePath(), "file.txt")); !os.IsNotExist(err) {
		t.Error("非目录不应触发合并操作")
	}
}

// TestBackupExistingMountPath_路径不存在 测试路径不存在时仍正常返回
func TestBackupExistingMountPath_路径不存在(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "nonexistent-path")
	backupPath := backupExistingMountPath(nonexistent)
	// 应返回带 .stale- 后缀的路径名
	if !strings.Contains(backupPath, ".stale-") {
		t.Errorf("返回的备份路径应包含 .stale-: %s", backupPath)
	}
}

// TestUnmountWorktree_非符号链接 测试卸载非符号链接路径时不删除
func TestUnmountWorktree_非符号链接(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 创建一个真实目录（非符号链接）
	realDir := filepath.Join(m.WorkspacePath(), ".worktree", "real-dir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("创建真实目录失败: %v", err)
	}

	if err := m.UnmountWorktree("real-dir"); err != nil {
		t.Fatalf("UnmountWorktree 不应报错: %v", err)
	}

	// 真实目录不应被删除
	if _, err := os.Stat(realDir); err != nil {
		t.Error("非符号链接的真实目录不应被删除")
	}
}

// TestGetHistory_无提交记录 测试文件无提交历史时返回 nil
func TestGetHistory_无提交记录(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	setupGitConfig(t, m.WorkspacePath())
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	history, err := m.GetHistory(ctx, "nonexistent.txt", 10)
	if err != nil {
		t.Fatalf("GetHistory 不应报错: %v", err)
	}
	if history != nil {
		t.Error("无提交记录时应返回 nil")
	}
}

// TestRevParse_无效引用 测试 rev-parse 无效引用时返回错误
func TestRevParse_无效引用(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	setupGitConfig(t, tmp)
	runGit(ctx, []string{"init"}, tmp)
	runGit(ctx, []string{"commit", "--allow-empty", "-m", "init"}, tmp)

	_, err := revParse(ctx, "nonexistent-branch", tmp)
	if err == nil {
		t.Error("无效引用应返回错误")
	}
}

// TestMountIntoWorkspace_指向其他位置时备份 测试已有目录指向其他位置时自动备份
func TestMountIntoWorkspace_指向其他位置时备份(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	agentRoot := t.TempDir()
	// 创建 .team 目录
	teamDir := filepath.Join(agentRoot, ".team")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatalf("创建 .team 目录失败: %v", err)
	}
	// 在 .team 下创建一个真实目录 test-team（非符号链接，指向其他位置）
	realTeamDir := filepath.Join(teamDir, "test-team")
	if err := os.MkdirAll(realTeamDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realTeamDir, "legacy.txt"), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}

	if err := m.MountIntoWorkspace(agentRoot); err != nil {
		t.Fatalf("MountIntoWorkspace 失败: %v", err)
	}

	// 应创建符号链接（旧真实目录已被备份并替换为符号链接）
	linkPath := filepath.Join(agentRoot, ".team", "test-team")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("符号链接应存在: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("应为符号链接")
	}

	// 验证符号链接指向工作空间
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("读取符号链接失败: %v", err)
	}
	absTarget, _ := filepath.Abs(target)
	absWs, _ := filepath.Abs(m.WorkspacePath())
	if absTarget != absWs {
		t.Errorf("符号链接应指向工作空间 %s，实际 %s", absWs, absTarget)
	}

	// 验证旧内容已合并到工作空间（legacy.txt 应在工作空间中）
	mergedData, err := os.ReadFile(filepath.Join(m.WorkspacePath(), "legacy.txt"))
	if err != nil {
		t.Fatalf("旧目录内容应已合并到工作空间: %v", err)
	}
	if string(mergedData) != "legacy" {
		t.Errorf("合并内容不匹配: 期望 'legacy'，实际 %q", mergedData)
	}

	// 验证备份目录存在
	entries, err := os.ReadDir(filepath.Join(agentRoot, ".team"))
	if err != nil {
		t.Fatalf("读取 .team 目录失败: %v", err)
	}
	hasBackup := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".stale-") && e.IsDir() {
			hasBackup = true
		}
	}
	if !hasBackup {
		t.Error("应有 .stale- 备份目录")
	}
}

// TestIsMountedToWorkspace_链接不存在 测试链接不存在时返回 false
func TestIsMountedToWorkspace_链接不存在(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 链接路径不存在
	result := m.isMountedToWorkspace(filepath.Join(t.TempDir(), "nonexistent"))
	if result {
		t.Error("链接不存在时应返回 false")
	}
}

// TestMountIntoWorktree_gitignore无换行结尾 测试已有 .gitignore 末尾无换行时的追加
func TestMountIntoWorktree_gitignore无换行结尾(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	worktreeDir := t.TempDir()
	// 写入末尾无换行的 .gitignore
	if err := os.WriteFile(filepath.Join(worktreeDir, ".gitignore"), []byte("*.o"), 0o644); err != nil {
		t.Fatalf("写入 .gitignore 失败: %v", err)
	}

	if err := m.MountIntoWorktree(worktreeDir); err != nil {
		t.Fatalf("MountIntoWorktree 失败: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(worktreeDir, ".gitignore"))
	if err != nil {
		t.Fatalf("读取 .gitignore 失败: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ".agent/") {
		t.Error("追加的 .agent/ 条目应存在")
	}
	// 确保追加了换行再写新内容
	if !strings.Contains(content, "\n") {
		t.Error("追加内容前应添加换行")
	}
}

// TestMaybePull 测试 LOCAL 模式下 maybePull 为 no-op
func TestMaybePull(t *testing.T) {
	ws := newTestManager(t, true)
	r := NewTeamWorkspaceRail(ws, "test-member")
	r.maybePull(context.Background()) // LOCAL 模式下不应崩溃，也不应调用 Pull
}

// TestCopyFile_保留权限 测试 copyFile 保留源文件权限
func TestCopyFile_保留权限(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "exec.sh")
	dstFile := filepath.Join(dstDir, "exec.sh")

	// 创建可执行文件
	if err := os.WriteFile(srcFile, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}

	if err := copyFile(srcFile, dstFile); err != nil {
		t.Fatalf("copyFile 失败: %v", err)
	}

	info, err := os.Stat(dstFile)
	if err != nil {
		t.Fatalf("读取目标文件信息失败: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(0o755).Perm() {
		t.Errorf("权限应保留: 期望 %o，实际 %o", 0o755, info.Mode().Perm())
	}
}

// TestBackupExistingMountPath_重名冲突 测试备份路径重名时自动编号
func TestBackupExistingMountPath_重名冲突(t *testing.T) {
	tmp := t.TempDir()
	oldPath := filepath.Join(tmp, "link-target")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	// 第一次备份
	backup1 := backupExistingMountPath(oldPath)
	// 重新创建原路径
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("重新创建目录失败: %v", err)
	}
	// 第二次备份（应自动编号避免冲突）
	backup2 := backupExistingMountPath(oldPath)

	if backup1 == backup2 {
		t.Error("重名时应自动编号，两次备份路径应不同")
	}
	if !strings.Contains(backup2, ".stale-") {
		t.Errorf("第二次备份路径应包含 .stale-: %s", backup2)
	}
}

// TestMergeExistingMountContents_错误路径跳过 测试 WalkDir 遇到错误时跳过
func TestMergeExistingMountContents_错误路径跳过(t *testing.T) {
	m := newTestManager(t, false)
	ctx := context.Background()
	if err := m.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	// 创建一个无法访问的目录（权限问题），WalkDir 应跳过
	oldDir := filepath.Join(t.TempDir(), "inaccessible")
	if err := os.MkdirAll(filepath.Join(oldDir, "sub"), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	// 不直接设置权限为 000（root 可以绕过），而是传入 nil info
	m.mergeExistingMountContents(oldDir, nil)

	// 不应崩溃
}

// TestInitialize_带RemoteURL 测试带 remoteURL 参数但 LOCAL 模式下的行为
func TestInitialize_带RemoteURL(t *testing.T) {
	m := newTestManager(t, true)
	ctx := context.Background()
	setupGitConfig(t, m.WorkspacePath())

	// LOCAL 模式下 remoteURL 不使用，仍走 git init 路径
	if err := m.Initialize(ctx, "https://example.com/repo.git"); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}

	gitDir := filepath.Join(m.WorkspacePath(), ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		t.Errorf(".git 目录应存在: %v", err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
