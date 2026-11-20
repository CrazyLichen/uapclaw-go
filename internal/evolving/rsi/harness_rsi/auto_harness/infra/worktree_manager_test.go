package infra

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func newTestWorktreeConfig(localRepo string) WorktreeConfigSnapshot {
	return WorktreeConfigSnapshot{
		LocalRepo:     localRepo,
		CacheRepoDir:  "/data/repo/agent-core",
		WorktreesDir:  "/data/worktrees",
		GitBaseBranch: "develop-auto-harness",
		RepoURL:       "https://gitcode.com/openJiuwen/agent-core.git",
		GitUserName:   "n",
		GitUserEmail:  "e",
		GitRemote:     "fork",
		ForkOwner:     "forker",
		UpstreamRepo:  "agent-core",
	}
}

// TestSlugify 验证 issue 识别与安全 slug
func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"fix issue #42: crash", "fix-issue-42"},
		{"issue-7 bug", "fix-issue-7"},
		{"Fix Issue #9", "fix-issue-9"},
		{"improve parser speed!!", "improve-parser-speed"},
		{"!!!", "task"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Fatalf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := strings.Repeat("a", 100)
	if got := slugify(long); len(got) != 40 {
		t.Fatalf("长度钳制 = %d", len(got))
	}
}

// TestWorktreeManager_EnsureBaseRepo_三分支 验证 fetch/clone 缓存语义
func TestWorktreeManager_EnsureBaseRepo_三分支(t *testing.T) {
	// 分支1：local_repo 存在 → fetch origin
	runner := newFakeRunner()
	runner.resps["git fetch origin"] = CommandResult{ExitCode: 0, Output: "fetched"}
	mgr := NewWorktreeManager(nil, runner, func() WorktreeConfigSnapshot {
		return newTestWorktreeConfig(t.TempDir())
	}, "u", "tok")
	base, err := mgr.ensureBaseRepo()
	if err != nil || base == "" {
		t.Fatalf("local_repo 分支: %v %q", err, base)
	}
	if runner.calls[0] != fmt.Sprintf("dir=%s cmd=git fetch origin", base) {
		t.Fatalf("fetch cwd = %q", runner.calls[0])
	}

	// 分支2：local_repo 不存在 → 报 local_repo not found
	runner2 := newFakeRunner()
	mgr2 := NewWorktreeManager(nil, runner2, func() WorktreeConfigSnapshot {
		return newTestWorktreeConfig("/nonexistent/repo")
	}, "u", "tok")
	if _, err := mgr2.ensureBaseRepo(); err == nil || !strings.Contains(err.Error(), "local_repo not found") {
		t.Fatalf("缺失 local_repo = %v", err)
	}

	// 分支3：无 local_repo、无缓存 → clone -b <base_branch>
	runner3 := newFakeRunner()
	cacheDir := t.TempDir()
	cfg3 := newTestWorktreeConfig("")
	cfg3.CacheRepoDir = cacheDir
	runner3.resps["git clone -b develop-auto-harness https://gitcode.com/openJiuwen/agent-core.git "+cacheDir] =
		CommandResult{ExitCode: 0, Output: "cloned"}
	mgr3 := NewWorktreeManager(nil, runner3, func() WorktreeConfigSnapshot { return cfg3 }, "u", "tok")
	base3, err := mgr3.ensureBaseRepo()
	if err != nil || base3 != cacheDir {
		t.Fatalf("clone 分支: %v %q", err, base3)
	}
	if runner3.calls[0] != fmt.Sprintf("dir=%s cmd=git clone -b develop-auto-harness https://gitcode.com/openJiuwen/agent-core.git %s", filepath.Dir(cacheDir), cacheDir) {
		t.Fatalf("clone cwd 应为缓存父目录 = %q", runner3.calls[0])
	}

	// clone 失败 → RuntimeError 语义
	runner4 := newFakeRunner()
	runner4.deflt = CommandResult{ExitCode: 128, Output: "clone error"}
	mgr4 := NewWorktreeManager(nil, runner4, func() WorktreeConfigSnapshot {
		cfg := newTestWorktreeConfig("")
		cfg.CacheRepoDir = "/tmp/ah-clone-test"
		return cfg
	}, "u", "tok")
	if _, err := mgr4.ensureBaseRepo(); err == nil || !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("clone 失败 = %v", err)
	}
}

// TestWorktreeManager_Prepare 验证 drop→add→config→remote 全链
func TestWorktreeManager_Prepare(t *testing.T) {
	localRepo := t.TempDir()
	runner := newFakeRunner()
	// 默认失败：命中"分支不存在（show-ref != 0）"与"remote 不存在（get-url != 0）"两条 Python 分支
	runner.deflt = CommandResult{ExitCode: 1, Output: ""}
	runner.resps["git fetch origin"] = CommandResult{ExitCode: 0, Output: "fetched"}
	runner.resps["git worktree prune"] = CommandResult{ExitCode: 0, Output: ""}
	runner.resps["git worktree add -b auto-harness/fix-issue-42"] = CommandResult{ExitCode: 0, Output: ""}
	cfg := newTestWorktreeConfig(localRepo)
	cfg.WorktreesDir = t.TempDir()
	mgr := NewWorktreeManager(nil, runner, func() WorktreeConfigSnapshot { return cfg }, "u", "tok")

	wt, err := mgr.Prepare("fix issue #42 crash")
	if err != nil {
		t.Fatalf("Prepare = %v", err)
	}
	if !strings.Contains(wt, "-fix-issue-42") || !strings.HasPrefix(wt, cfg.WorktreesDir) {
		t.Fatalf("worktree 路径 = %q", wt)
	}
	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"git worktree prune",
		"git show-ref --verify --quiet refs/heads/auto-harness/fix-issue-42",
		"git worktree add -b auto-harness/fix-issue-42",
		"origin/develop-auto-harness",
		"git config user.name n",
		"git config user.e",
		"git remote get-url fork",
		"git remote add fork https://gitcode.com/forker/agent-core.git",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("调用链缺 %q:\n%s", want, joined)
		}
	}
	// 最后一次调用是 remote add，且在 base 上执行
	last := runner.calls[len(runner.calls)-1]
	if !strings.Contains(last, "cmd=git remote add fork https://gitcode.com/forker/agent-core.git") {
		t.Fatalf("末次调用 = %q", last)
	}
	if !strings.HasPrefix(last, fmt.Sprintf("dir=%s cmd=git remote add", localRepo)) {
		t.Fatalf("remote add 应在 base 执行 = %q", last)
	}
}

// TestWorktreeManager_DropExistingBranch_非托管worktree拒绝 验证安全检查
func TestWorktreeManager_DropExistingBranch_非托管worktree拒绝(t *testing.T) {
	localRepo := t.TempDir()
	runner := newFakeRunner()
	cfg := newTestWorktreeConfig(localRepo)
	cfg.WorktreesDir = t.TempDir()
	runner.deflt = CommandResult{ExitCode: 0, Output: ""}
	// show-ref 命中
	runner.resps["git show-ref --verify --quiet refs/heads/auto-harness/x"] = CommandResult{ExitCode: 0}
	// worktree list 报告一个非托管路径占用该分支
	runner.resps["git worktree list --porcelain"] = CommandResult{
		ExitCode: 0,
		Output:   "worktree /elsewhere/wt\nbranch refs/heads/auto-harness/x\n\n",
	}
	mgr := NewWorktreeManager(nil, runner, func() WorktreeConfigSnapshot { return cfg }, "u", "tok")
	err := mgr.dropExistingBranch(localRepo, "auto-harness/x")
	if err == nil || !strings.Contains(err.Error(), "unmanaged worktree") {
		t.Fatalf("非托管 worktree 应拒绝: %v", err)
	}
}

// TestWorktreeManager_IsManagedWorktreePath
func TestWorktreeManager_IsManagedWorktreePath(t *testing.T) {
	cfg := newTestWorktreeConfig("")
	cfg.WorktreesDir = "/data/worktrees"
	mgr := NewWorktreeManager(nil, newFakeRunner(), func() WorktreeConfigSnapshot { return cfg }, "u", "tok")
	if !mgr.isManagedWorktreePath("/data/worktrees/123-x") {
		t.Fatal("托管路径应命中")
	}
	if mgr.isManagedWorktreePath("/elsewhere/x") {
		t.Fatal("外部路径不应命中")
	}
}

// TestWorktreeManager_PrepareReadonlySnapshot 验证 detached 快照
func TestWorktreeManager_PrepareReadonlySnapshot(t *testing.T) {
	localRepo := t.TempDir()
	runner := newFakeRunner()
	cfg := newTestWorktreeConfig(localRepo)
	cfg.WorktreesDir = t.TempDir()
	mgr := NewWorktreeManager(nil, runner, func() WorktreeConfigSnapshot { return cfg }, "u", "tok")
	wt, err := mgr.PrepareReadonlySnapshot("assess")
	if err != nil {
		t.Fatalf("PrepareReadonlySnapshot = %v", err)
	}
	if !strings.HasSuffix(wt, "-assess") {
		t.Fatalf("快照路径 = %q", wt)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "git worktree add --detach") ||
		!strings.Contains(joined, "origin/develop-auto-harness") {
		t.Fatalf("调用链 = %s", joined)
	}
}

// TestWorktreeManager_Cleanup_缺失路径静默返回
func TestWorktreeManager_Cleanup_缺失路径静默返回(t *testing.T) {
	localRepo := t.TempDir()
	runner := newFakeRunner()
	cfg := newTestWorktreeConfig(localRepo)
	mgr := NewWorktreeManager(nil, runner, func() WorktreeConfigSnapshot { return cfg }, "u", "tok")
	mgr.Cleanup("/nonexistent/wt")
	if len(runner.calls) != 0 {
		t.Fatalf("缺失路径不应执行 git: %v", runner.calls)
	}
}
