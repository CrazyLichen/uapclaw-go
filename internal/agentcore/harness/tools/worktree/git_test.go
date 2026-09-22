package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCtx 创建带超时的测试 context
func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestRunGit_正常命令 测试正常 git 命令
func TestRunGit_正常命令(t *testing.T) {
	ctx := testCtx(t)
	r := runGit(ctx, []string{"--version"}, "")
	if !r.OK() {
		t.Fatalf("git --version 应成功: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "git version") {
		t.Errorf("输出应包含 'git version'，实际: %s", r.Stdout)
	}
}

// TestRunGit_错误命令 测试非零返回码
func TestRunGit_错误命令(t *testing.T) {
	ctx := testCtx(t)
	r := runGit(ctx, []string{"nonexistent-command"}, "")
	if r.OK() {
		t.Error("不存在的命令应返回非零退出码")
	}
}

// TestfindCanonicalGitRoot 测试在真实 git 仓库中查找主仓库根目录
func TestFindCanonicalGitRoot(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := testCtx(t)

	// 初始化 git 仓库
	if err := exec.Command("git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}

	root, err := findCanonicalGitRoot(ctx, tmpDir)
	if err != nil {
		t.Fatalf("findCanonicalGitRoot 失败: %v", err)
	}
	if root == "" {
		t.Error("findCanonicalGitRoot 不应返回空字符串")
	}
}

// TestreadWorktreeHeadSHA 测试快速路径验证
func TestReadWorktreeHeadSHA(t *testing.T) {
	// 在非 worktree 目录应返回错误
	tmpDir := t.TempDir()
	_, err := readWorktreeHeadSHA(tmpDir)
	if err == nil {
		t.Error("非 worktree 目录应返回错误")
	}
}

// TeststatusPorcelain 测试在临时 git 仓库中验证
func TestStatusPorcelain(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := testCtx(t)

	// 初始化 git 仓库
	if err := exec.Command("git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}

	// 空仓库应无变更
	changes, err := statusPorcelain(ctx, tmpDir)
	if err != nil {
		t.Fatalf("statusPorcelain 失败: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("空仓库应无变更，实际: %d", len(changes))
	}

	// 创建一个未跟踪文件
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	changes, err = statusPorcelain(ctx, tmpDir)
	if err != nil {
		t.Fatalf("statusPorcelain 失败: %v", err)
	}
	if len(changes) == 0 {
		t.Error("有未跟踪文件时应返回变更")
	}
}

// TestgetCurrentBranch 测试分支名读取
func TestGetCurrentBranch(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := testCtx(t)

	// 初始化 git 仓库并做一次提交（否则 HEAD 不指向分支）
	if err := exec.Command("git", "init", tmpDir).Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}
	_ = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run()
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello"), 0o644)
	_ = exec.Command("git", "-C", tmpDir, "add", ".").Run()
	_ = exec.Command("git", "-C", tmpDir, "commit", "-m", "init").Run()

	branch, err := getCurrentBranch(ctx, tmpDir)
	if err != nil {
		t.Fatalf("getCurrentBranch 失败: %v", err)
	}
	if branch == "" {
		t.Error("分支名不应为空")
	}
}
