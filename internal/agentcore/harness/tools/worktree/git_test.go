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

// ──────────────────────────── gitdir/commondir 校验 ────────────────────────────

// TestReadWorktreeHeadSHA_拒绝gitdir越界 验证 .git 指向缺少 HEAD/commondir 的目录时返回错误。
//
// 真实 worktree 的 gitdir 通常是绝对路径（主仓库 .git/worktrees/x），
// 因此不能限定"必须落在 wtPath 内"；判据改为：目标必须是目录且含 HEAD 与 commondir
// 两个文件——这是真实 git 工作树的必然特征，被篡改成 /etc 之类的路径不会满足。
func TestReadWorktreeHeadSHA_拒绝gitdir越界(t *testing.T) {
	wt := t.TempDir()
	outside := t.TempDir()
	// 外部目录放一个看似合法的 40 字节 HEAD，但缺 commondir（非真实工作树）
	if err := os.WriteFile(filepath.Join(outside, "HEAD"), []byte("0123456789012345678901234567890123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+outside+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sha, err := readWorktreeHeadSHA(wt)
	if err == nil {
		t.Fatalf("缺 commondir 的 gitdir 应返回错误，实际读到 sha=%q", sha)
	}
}

// TestReadWorktreeHeadSHA_绝对gitdir合法 验证绝对路径 gitdir（真实布局）正常工作。
//
// 回归：旧代码无条件 filepath.Join(wtPath, gitdir)，遇到绝对路径会拼成
// wtPath+"/abs/path"，导致真实 worktree 永远读不到 HEAD SHA。
func TestReadWorktreeHeadSHA_绝对gitdir合法(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, repo, "init")
	runGitIn(t, repo, "config", "user.email", "t@t")
	runGitIn(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, repo, "add", ".")
	runGitIn(t, repo, "commit", "-m", "init")

	wtPath := filepath.Join(root, "wt")
	runGitIn(t, repo, "worktree", "add", "-b", "wt-branch", wtPath)

	sha, err := readWorktreeHeadSHA(wtPath)
	if err != nil {
		t.Fatalf("真实 worktree 应能读取 HEAD SHA，实际 %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("SHA 长度应为 40，实际 %q", sha)
	}
}

// TestReadWorktreeHeadSHA_篡改为系统目录 验证 gitdir 指向 /etc 这类目录被拒
func TestReadWorktreeHeadSHA_篡改为系统目录(t *testing.T) {
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /etc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorktreeHeadSHA(wt); err == nil {
		t.Fatal("/etc 缺少 HEAD/commondir，应返回错误")
	}
}

// TestResolveGitDirFile 直接验证 gitdir 三类输入的判定
func TestResolveGitDirFile(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"HEAD", "commondir"} {
		if err := os.WriteFile(filepath.Join(good, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := resolveGitDirFile(root, good); err != nil {
		t.Fatalf("绝对路径且含 HEAD/commondir 应通过，实际 %v", err)
	}
	// 相对路径按 wtPath 解析
	if got, err := resolveGitDirFile(root, "good"); err != nil || got != good {
		t.Fatalf("相对路径应解析为 %q，实际 %q err=%v", good, got, err)
	}
	// 空内容
	if _, err := resolveGitDirFile(root, ""); err == nil {
		t.Fatal("空 gitdir 应报错")
	}
	// 只有 HEAD 缺 commondir
	onlyHead := filepath.Join(root, "onlyhead")
	if err := os.MkdirAll(onlyHead, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onlyHead, "HEAD"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveGitDirFile(root, onlyHead); err == nil {
		t.Fatal("缺 commondir 应报错")
	}
}

// runGitIn 在指定目录执行 git 命令，失败即终止测试。
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v 失败: %v\n%s", args, err, out)
	}
}
