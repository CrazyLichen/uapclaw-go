package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// TestCurrentCommandRuntimePaths 验证四个路径锚点均非空
func TestCurrentCommandRuntimePaths(t *testing.T) {
	ctx := context.Background()
	paths, err := CurrentCommandRuntimePaths(ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if paths.CurrentCwd == "" {
		t.Error("CurrentCwd should not be empty")
	}
	if paths.ProjectRoot == "" {
		t.Error("ProjectRoot should not be empty")
	}
	if paths.AgentWorkspaceRoot == "" {
		t.Error("AgentWorkspaceRoot should not be empty")
	}
	if !filepath.IsAbs(paths.AgentWorkspaceRoot) {
		t.Errorf("AgentWorkspaceRoot should be absolute, got %q", paths.AgentWorkspaceRoot)
	}
}

// TestCurrentCommandRuntimePaths_requireRuntimeCwd 验证强制要求时仍能回退（无 CwdState）
func TestCurrentCommandRuntimePaths_requireRuntimeCwd(t *testing.T) {
	// 无 CwdState 绑定时，GetCwd 回退到 os.Getwd()，不会为空
	paths, err := CurrentCommandRuntimePaths(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paths.CurrentCwd == "" {
		t.Error("CurrentCwd should not be empty")
	}
}

// TestResolveCommandWorkdir_空字符串用cwd 验证空值取当前 cwd
func TestResolveCommandWorkdir_空字符串用cwd(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		AgentWorkspaceRoot: dir,
	}

	got, err := ResolveCommandWorkdir("", paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Errorf("got %q, want %q", got, filepath.Clean(dir))
	}
}

// TestResolveCommandWorkdir_nil用cwd 验证 nil 取当前 cwd
func TestResolveCommandWorkdir_nil用cwd(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		AgentWorkspaceRoot: dir,
	}

	got, err := ResolveCommandWorkdir(nil, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Errorf("got %q, want %q", got, filepath.Clean(dir))
	}
}

// TestResolveCommandWorkdir_相对路径基于cwd 验证相对路径拼接
func TestResolveCommandWorkdir_相对路径基于cwd(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		AgentWorkspaceRoot: dir,
	}

	got, err := ResolveCommandWorkdir("sub", paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(sub) {
		t.Errorf("got %q, want %q", got, filepath.Clean(sub))
	}
}

// TestResolveCommandWorkdir_绝对路径在根内 验证绝对路径直接通过
func TestResolveCommandWorkdir_绝对路径在根内(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		AgentWorkspaceRoot: dir,
	}

	got, err := ResolveCommandWorkdir(sub, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(sub) {
		t.Errorf("got %q, want %q", got, filepath.Clean(sub))
	}
}

// TestResolveCommandWorkdir_越界拒绝 验证三根之外的绝对路径被拒
func TestResolveCommandWorkdir_越界拒绝(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		AgentWorkspaceRoot: dir,
	}

	_, err := ResolveCommandWorkdir("/etc/passwd", paths)
	if err == nil {
		t.Fatal("expected error for path outside workspace")
	}
	if !strings.Contains(err.Error(), "workdir is outside project workspace") {
		t.Errorf("error = %q", err.Error())
	}
}

// TestResolveCommandWorkdir_非字符串拒绝 验证类型错误
func TestResolveCommandWorkdir_非字符串拒绝(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd: dir, ProjectRoot: dir, AgentWorkspaceRoot: dir,
	}

	_, err := ResolveCommandWorkdir(42, paths)
	if err == nil {
		t.Fatal("expected error for non-string workdir")
	}
	if !strings.Contains(err.Error(), "workdir must be a string") {
		t.Errorf("error = %q", err.Error())
	}
}

// TestResolveCommandWorkdir_workspaceRoot参与白名单 验证 workspace_root 也允许
func TestResolveCommandWorkdir_workspaceRoot参与白名单(t *testing.T) {
	projectDir := t.TempDir()
	wsDir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         projectDir,
		ProjectRoot:        projectDir,
		WorkspaceRoot:      wsDir,
		AgentWorkspaceRoot: projectDir,
	}

	got, err := ResolveCommandWorkdir(wsDir, paths)
	if err != nil {
		t.Fatalf("workspace root should be allowed: %v", err)
	}
	if got != filepath.Clean(wsDir) {
		t.Errorf("got %q, want %q", got, filepath.Clean(wsDir))
	}
}

// TestResolveCommandWorkdir_workspaceRoot为空不参与 验证空 workspace_root 被跳过
func TestResolveCommandWorkdir_workspaceRoot为空不参与(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		WorkspaceRoot:      "", // 空串，不参与白名单
		AgentWorkspaceRoot: dir,
	}

	_, err := ResolveCommandWorkdir(other, paths)
	if err == nil {
		t.Fatal("empty workspace_root should not whitelist other dirs")
	}
}

// TestResolveCommandWorkdir_父目录穿越拒绝 验证 ../ 逃逸被拒
func TestResolveCommandWorkdir_父目录穿越拒绝(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         filepath.Join(dir, "sub"),
		ProjectRoot:        filepath.Join(dir, "sub"),
		AgentWorkspaceRoot: filepath.Join(dir, "sub"),
	}

	_, err := ResolveCommandWorkdir("../escape", paths)
	if err == nil {
		t.Fatal("parent traversal should be rejected")
	}
}

// TestResolveCommandWorkdir_三根白名单 验证 project_root/workspace_root/agent_workspace 三根白名单。
//
// 自 mcp_command_test.go 迁移：原测试调用已删除的私有 resolveCommandWorkdir，
// 改用公开的 CommandRuntimePaths + ResolveCommandWorkdir。
func TestResolveCommandWorkdir_三根白名单(t *testing.T) {
	paths, err := CurrentCommandRuntimePaths(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 相对路径基于 cwd 应通过（cwd 本身在三根内或其回退链内）
	if _, err := ResolveCommandWorkdir(".", paths); err != nil {
		t.Errorf("相对路径基于 cwd 应通过: %v", err)
	}
	// /etc 通常不在任何允许根内（除非极端环境），越界应返回错误
	if _, err := ResolveCommandWorkdir("/etc", paths); err == nil {
		t.Error("/etc 越界应返回错误")
	}
}

// TestResolveCommandWorkdir_绝对路径在白名单内 AgentWorkspaceDir 恒在白名单。
//
// 自 mcp_command_test.go 迁移：同上。
func TestResolveCommandWorkdir_绝对路径在白名单内(t *testing.T) {
	paths, err := CurrentCommandRuntimePaths(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	agentDir := paths.AgentWorkspaceRoot
	if _, err := ResolveCommandWorkdir(agentDir, paths); err != nil {
		t.Errorf("AgentWorkspaceDir 应在白名单: %v", err)
	}
	// 子目录也在白名单
	if _, err := ResolveCommandWorkdir(filepath.Join(agentDir, "sub"), paths); err != nil {
		t.Errorf("AgentWorkspaceDir 子目录应在白名单: %v", err)
	}
}

// TestCurrentCommandRuntimePaths_带CwdState 验证 ctx 绑定 CwdState 时的取值路径。
//
// 覆盖 currentCwdPath / currentProjectRoot / currentWorkspaceRoot 的「有值」分支。
func TestCurrentCommandRuntimePaths_带CwdState(t *testing.T) {
	dir := t.TempDir()
	ws := t.TempDir()
	state := cwd.InitCwd(dir, cwd.WithProjectRoot(dir), cwd.WithWorkspace(ws))
	ctx := cwd.WithCwdState(context.Background(), state)

	paths, err := CurrentCommandRuntimePaths(ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paths.CurrentCwd != filepath.Clean(dir) {
		t.Errorf("CurrentCwd = %q, want %q", paths.CurrentCwd, filepath.Clean(dir))
	}
	if paths.ProjectRoot != filepath.Clean(dir) {
		t.Errorf("ProjectRoot = %q, want %q", paths.ProjectRoot, filepath.Clean(dir))
	}
	if paths.WorkspaceRoot == "" {
		t.Error("WorkspaceRoot should be set from CwdState")
	}
}

// TestCurrentCommandRuntimePaths_无Workspace 验证未设置 workspace 时返回空串。
//
// 覆盖 currentWorkspaceRoot 的空值分支。
func TestCurrentCommandRuntimePaths_无Workspace(t *testing.T) {
	dir := t.TempDir()
	state := cwd.InitCwd(dir)
	ctx := cwd.WithCwdState(context.Background(), state)

	paths, err := CurrentCommandRuntimePaths(ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paths.WorkspaceRoot != "" {
		t.Errorf("WorkspaceRoot = %q, want empty（Python None）", paths.WorkspaceRoot)
	}
}
