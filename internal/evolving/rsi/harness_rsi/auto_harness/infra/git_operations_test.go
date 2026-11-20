package infra

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRunner 记录调用并按脚本返回
type fakeRunner struct {
	calls  []string
	resps  map[string]CommandResult
	deflt  CommandResult
	runErr error
}

func (f *fakeRunner) Run(_ context.Context, dir string, _ []string, name string, args ...string) (CommandResult, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, fmt.Sprintf("dir=%s cmd=%s", dir, cmd))
	if f.runErr != nil {
		return CommandResult{}, f.runErr
	}
	if r, ok := f.resps[cmd]; ok {
		return r, nil
	}
	return f.deflt, nil
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{resps: map[string]CommandResult{}, deflt: CommandResult{ExitCode: 0, Output: ""}}
}

func newTestGitOps(runner CommandRunner) *GitOperations {
	return NewGitOperations(
		context.Background(), runner, "/ws", "fork", "develop",
		"forker", "openJiuwen", "agent-core",
		"user1", "tok123", "name", "mail",
	)
}

// TestBuildGitAuthEnv_无凭据 验证无凭据时仅设置非交互变量
func TestBuildGitAuthEnv_无凭据(t *testing.T) {
	env := BuildGitAuthEnv("", "")
	if env["GIT_TERMINAL_PROMPT"] != "0" || env["GCM_INTERACTIVE"] != "never" {
		t.Fatalf("非交互变量缺失: %v", env)
	}
	if _, ok := env["GIT_CONFIG_COUNT"]; ok {
		t.Fatal("无凭据不应注入 extraheader")
	}
	if _, ok := env["PATH"]; !ok {
		t.Fatal("应复制 os.environ")
	}
}

// TestBuildGitAuthEnv_凭据注入 验证 6 条 GIT_CONFIG 注入与 basic 编码
func TestBuildGitAuthEnv_凭据注入(t *testing.T) {
	env := BuildGitAuthEnv("user1", "tok123")
	if env["GIT_CONFIG_COUNT"] != "6" {
		t.Fatalf("GIT_CONFIG_COUNT = %q", env["GIT_CONFIG_COUNT"])
	}
	if env["GIT_CONFIG_KEY_2"] != "http.https://gitcode.com/.extraheader" {
		t.Fatalf("KEY_2 = %q", env["GIT_CONFIG_KEY_2"])
	}
	if !strings.HasPrefix(env["GIT_CONFIG_VALUE_2"], "AUTHORIZATION: basic ") {
		t.Fatalf("VALUE_2 = %q", env["GIT_CONFIG_VALUE_2"])
	}
	// base64("user1:tok123") = dXNlcjE6dG9rMTIz
	if !strings.Contains(env["GIT_CONFIG_VALUE_2"], "dXNlcjE6dG9rMTIz") {
		t.Fatalf("basic 编码漂移: %q", env["GIT_CONFIG_VALUE_2"])
	}
	if env["GIT_CONFIG_VALUE_3"] != "HTTP/1.1" || env["GIT_CONFIG_VALUE_5"] != "60" {
		t.Fatalf("http 配置漂移: %v", env)
	}
}

// TestGitOperations_命令执行与出口码 验证 _git 出口码与输出 rstrip
func TestGitOperations_命令执行与出口码(t *testing.T) {
	runner := newFakeRunner()
	runner.resps["git status --porcelain --untracked-files=all"] =
		CommandResult{ExitCode: 0, Output: " M a.py\n?? b.py\n\n"}
	runner.resps["git rev-list --count origin/develop..HEAD"] =
		CommandResult{ExitCode: 1, Output: "fatal: bad\n"}
	g := newTestGitOps(runner)

	if got := g.StatusPorcelain(); got != " M a.py\n?? b.py" {
		t.Fatalf("StatusPorcelain = %q", got)
	}
	if g.HasCommitsAgainstBase() {
		t.Fatal("rev-list 失败应返回 false")
	}
	// git 命令走 runner 且 cwd=workspace
	if len(runner.calls) == 0 || !strings.HasPrefix(runner.calls[0], "dir=/ws cmd=git") {
		t.Fatalf("runner 调用漂移: %v", runner.calls)
	}
}

// TestGitOperations_启动失败未安装git
func TestGitOperations_启动失败未安装git(t *testing.T) {
	runner := newFakeRunner()
	runner.runErr = fmt.Errorf(`exec: "git": executable file not found in $PATH`)
	g := newTestGitOps(runner)
	if g.DiscardWorktreeChanges() {
		t.Fatal("git 未安装应失败而非 panic（严格对齐：报错不降级）")
	}
}

// TestGitOperations_CollectStatus 验证 porcelain 解析（重命名/未跟踪/去重）
func TestGitOperations_CollectStatus(t *testing.T) {
	runner := newFakeRunner()
	runner.resps["git status --porcelain --untracked-files=all"] = CommandResult{
		ExitCode: 0,
		Output: " M a.py\n M a.py\nR  old.py -> new.py\n?? u1.txt\n?? u2.txt\n",
	}
	g := newTestGitOps(runner)
	status := g.CollectStatus()
	if len(status["dirty_files"]) != 4 || status["dirty_files"][1] != "new.py" {
		t.Fatalf("dirty_files = %v", status["dirty_files"])
	}
	// Python 语义：R 重命名的目标路径同样计入 tracked_modified_files（marker != "??"）
	if len(status["tracked_modified_files"]) != 2 ||
		status["tracked_modified_files"][0] != "a.py" || status["tracked_modified_files"][1] != "new.py" {
		t.Fatalf("tracked = %v", status["tracked_modified_files"])
	}
	if len(status["untracked_files"]) != 2 {
		t.Fatalf("untracked = %v", status["untracked_files"])
	}
	if len(status["renamed_files"]) != 1 || status["renamed_files"][0] != "new.py" {
		t.Fatalf("renamed = %v", status["renamed_files"])
	}
	// 失败返回空结构
	runner2 := newFakeRunner()
	runner2.resps["git status --porcelain --untracked-files=all"] = CommandResult{ExitCode: 128, Output: ""}
	status2 := newTestGitOps(runner2).CollectStatus()
	for _, key := range []string{"dirty_files", "tracked_modified_files", "untracked_files", "renamed_files"} {
		if len(status2[key]) != 0 {
			t.Fatalf("失败时 %s 应为空", key)
		}
	}
}

// TestGitOperations_AddPathsCommit 验证空路径拒绝与 add/commit 透传
func TestGitOperations_AddPathsCommit(t *testing.T) {
	runner := newFakeRunner()
	g := newTestGitOps(runner)
	res := g.AddPaths(nil)
	if res["success"] != false || res["output"] != "no paths provided" {
		t.Fatalf("空路径 = %v", res)
	}
	res = g.AddPaths([]string{"a.py", "b.py"})
	if res["success"] != true {
		t.Fatalf("add = %v", res)
	}
	if runner.calls[len(runner.calls)-1] != "dir=/ws cmd=git add -- a.py b.py" {
		t.Fatalf("add 调用 = %q", runner.calls[len(runner.calls)-1])
	}
	res = g.Commit("feat: x")
	if res["success"] != true {
		t.Fatalf("commit = %v", res)
	}
	if !strings.Contains(runner.calls[len(runner.calls)-1], "commit -m feat: x") {
		t.Fatalf("commit 调用 = %q", runner.calls[len(runner.calls)-1])
	}
}

// TestGitOperations_sanitize 验证输出脱敏（token/basic/URL/头字段）
func TestGitOperations_sanitize(t *testing.T) {
	g := newTestGitOps(newFakeRunner())
	out := g.sanitizeGitOutput("token tok123 leak\nuser1:secret@host\nAuthorization: Bearer abc\nprivate-token: xyz\naccess_token=q")
	for _, bad := range []string{"tok123", "user1:secret@", "Bearer abc", "xyz", "q\n"} {
		_ = bad
	}
	if strings.Contains(out, "tok123") {
		t.Fatalf("token 未脱敏: %q", out)
	}
	if strings.Contains(out, "dXNlcjE6") || strings.Contains(out, "user1:") {
		t.Fatalf("用户名/编码未脱敏: %q", out)
	}
	if !strings.Contains(out, "***:***@") {
		t.Fatalf("URL 脱敏缺失: %q", out)
	}
	if strings.Contains(out, "Bearer abc") || strings.Contains(out, "xyz") {
		t.Fatalf("头字段未脱敏: %q", out)
	}
	if args := g.sanitizeGitArgs([]string{"push", "fork", "tok123"}); strings.Contains(args, "tok123") {
		t.Fatalf("args 未脱敏: %q", args)
	}
}

// TestGitOperations_authenticatedRemoteURL 验证非 https/缺凭据返回空
func TestGitOperations_authenticatedRemoteURL(t *testing.T) {
	g := newTestGitOps(newFakeRunner())
	if got := g.authenticatedRemoteURL("https://gitcode.com/foo/bar.git"); got == "" {
		t.Fatal("https+凭据应返回带认证 URL")
	} else if !strings.Contains(got, "user1:tok123@") {
		t.Fatalf("URL 缺凭据: %q", got)
	}
	if got := g.authenticatedRemoteURL("git@gitcode.com:foo/bar.git"); got != "" {
		t.Fatalf("非 https 应为空: %q", got)
	}
}

// TestGitOperations_FindExistingIssueFixRef 验证打分排序与无候选
func TestGitOperations_FindExistingIssueFixRef(t *testing.T) {
	runner := newFakeRunner()
	refs := "origin/fix/issue-42\nautoharness/fix-issue-42\nfeature-x\norigin/fix/issue-42-stale\n"
	runner.resps["git for-each-ref --format=%(refname:short) refs/heads refs/remotes/origin refs/remotes/autoharness"] =
		CommandResult{ExitCode: 0, Output: refs}
	runner.resps["git branch --show-current"] = CommandResult{ExitCode: 0, Output: "main"}
	// fix/issue-42: 2 commits, files a.py
	runner.resps["git rev-list --count origin/develop..origin/fix/issue-42"] = CommandResult{ExitCode: 0, Output: "2"}
	runner.resps["git diff --name-only origin/develop...origin/fix/issue-42"] = CommandResult{ExitCode: 0, Output: "a.py\n"}
	// fix-issue-42 (autoharness): 3 commits, files a.py b.py
	runner.resps["git rev-list --count origin/develop..autoharness/fix-issue-42"] = CommandResult{ExitCode: 0, Output: "3"}
	runner.resps["git diff --name-only origin/develop...autoharness/fix-issue-42"] = CommandResult{ExitCode: 0, Output: "a.py\nb.py\n"}
	// fix/issue-42-stale: 0 commits → 排除
	runner.resps["git rev-list --count origin/develop..origin/fix/issue-42-stale"] = CommandResult{ExitCode: 0, Output: "0"}

	g := newTestGitOps(runner)
	res := g.FindExistingIssueFixRef("42", []string{"a.py"})
	if res["success"] != true {
		t.Fatalf("应命中候选: %v", res)
	}
	// 打分：fix/issue-42 = 1*100+50-2=148；fix-issue-42 = 100+25-3=122
	if res["ref"] != "origin/fix/issue-42" {
		t.Fatalf("最高分候选 = %v", res["ref"])
	}
	// 无候选
	res2 := g.FindExistingIssueFixRef("999", nil)
	if res2["success"] != false || res2["reason"] != "no existing branch found for issue 999" {
		t.Fatalf("无候选 = %v", res2)
	}
	// 缺 issue 号
	res3 := g.FindExistingIssueFixRef("", nil)
	if res3["success"] != false || res3["reason"] != "missing issue number" {
		t.Fatalf("缺号 = %v", res3)
	}
}

// TestGitOperations_CherryPickRefCommits 验证逐个 pick 与失败 abort
func TestGitOperations_CherryPickRefCommits(t *testing.T) {
	runner := newFakeRunner()
	runner.resps["git rev-list --reverse origin/develop..origin/f"] =
		CommandResult{ExitCode: 0, Output: "c1\nc2\n"}
	g := newTestGitOps(runner)
	res := g.CherryPickRefCommits("origin/f")
	if res["success"] != true || len(res["commits"].([]string)) != 2 {
		t.Fatalf("成功 pick = %v", res)
	}
	// 失败分支
	runner2 := newFakeRunner()
	runner2.resps["git rev-list --reverse origin/develop..origin/f"] =
		CommandResult{ExitCode: 0, Output: "c1\nc2\n"}
	runner2.resps["git cherry-pick c2"] = CommandResult{ExitCode: 1, Output: "conflict"}
	res2 := newTestGitOps(runner2).CherryPickRefCommits("origin/f")
	if res2["success"] != false {
		t.Fatalf("冲突应失败: %v", res2)
	}
	aborted := false
	for _, c := range runner2.calls {
		if strings.HasSuffix(c, "cherry-pick --abort") {
			aborted = true
		}
	}
	if !aborted {
		t.Fatal("失败后应 abort")
	}
	// rev-list 失败
	runner3 := newFakeRunner()
	runner3.resps["git rev-list --reverse origin/develop..origin/f"] = CommandResult{ExitCode: 128, Output: "err"}
	if r := newTestGitOps(runner3).CherryPickRefCommits("origin/f"); r["success"] != false {
		t.Fatal("rev-list 失败应失败")
	}
	// 无提交
	runner4 := newFakeRunner()
	runner4.resps["git rev-list --reverse origin/develop..origin/f"] = CommandResult{ExitCode: 0, Output: ""}
	if r := newTestGitOps(runner4).CherryPickRefCommits("origin/f"); r["output"] != "no commits to cherry-pick" {
		t.Fatalf("无提交 = %v", r)
	}
}

// TestGitOperations_Push_成功与诊断 验证三段尝试链与失败诊断
func TestGitOperations_Push_成功与诊断(t *testing.T) {
	runner := newFakeRunner()
	runner.deflt = CommandResult{ExitCode: 0, Output: "pushed"}
	runner.resps["git push -u --force-with-lease fork feat"] = CommandResult{ExitCode: 0, Output: "ok"}
	g := newTestGitOps(runner)
	res := g.Push("feat")
	if res["success"] != true {
		t.Fatalf("首次推送应成功: %v", res)
	}

	// 全部失败 → 诊断
	runner2 := newFakeRunner()
	runner2.deflt = CommandResult{ExitCode: 128, Output: "denied"}
	g2 := newTestGitOps(runner2)
	res2 := g2.Push("feat")
	if res2["success"] != false {
		t.Fatal("全失败应返回 false")
	}
	if !strings.Contains(res2["output"].(string), "[push diagnostics]") {
		t.Fatalf("输出缺诊断段: %q", res2["output"])
	}
	if !strings.Contains(res2["output"].(string), "dry-run push trace") {
		t.Fatal("诊断应含 trace dry-run")
	}
}

// TestGitOperations_BuildPRWebURL 验证 fork→上游 MR 网页 URL
func TestGitOperations_BuildPRWebURL(t *testing.T) {
	g := newTestGitOps(newFakeRunner())
	got := g.BuildPRWebURL("feat/a")
	want := "https://gitcode.com/forker/agent-core/merge_requests/new?source_branch=feat%2Fa&target_branch=develop"
	if got != want {
		t.Fatalf("BuildPRWebURL = %q, want %q", got, want)
	}
	g2 := NewGitOperations(context.Background(), newFakeRunner(), "/ws", "", "develop", "", "", "", "", "", "", "")
	if got := g2.BuildPRWebURL("x"); got != "" {
		t.Fatalf("缺 fork_owner 应为空: %q", got)
	}
}

// TestGitOperations_ExtractPRURL 验证多种响应形态
func TestGitOperations_ExtractPRURL(t *testing.T) {
	g := newTestGitOps(newFakeRunner())
	if got := g.extractPRURL(map[string]any{"html_url": "http://x/1"}); got != "http://x/1" {
		t.Fatalf("html_url = %q", got)
	}
	if got := g.extractPRURL(map[string]any{
		"data": map[string]any{"web_url": "http://x/2"},
	}); got != "http://x/2" {
		t.Fatalf("嵌套 data = %q", got)
	}
	if got := g.extractPRURL([]any{map[string]any{"url": "http://x/3"}}); got != "http://x/3" {
		t.Fatalf("列表 = %q", got)
	}
	if got := g.extractPRURL(map[string]any{"nope": 1}); got != "" {
		t.Fatalf("无 URL 应为空: %q", got)
	}
}

// TestGitOperations_CreatePR_成功与HTTP错误 验证 GitCode API 调用（D-e2-7 注入 base）
func TestGitOperations_CreatePR_成功与HTTP错误(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/repos/openJiuwen/agent-core/pulls") {
			t.Errorf("请求形态漂移: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"html_url":"https://gitcode.com/pr/1"}`)
	}))
	defer srv.Close()

	runner := newFakeRunner()
	runner.deflt = CommandResult{ExitCode: 0, Output: ""}
	g := newTestGitOps(runner)
	g.apiBase = srv.URL
	res := g.CreatePR("t", "b", "feat")
	if res["success"] != true || res["pr_url"] != "https://gitcode.com/pr/1" {
		t.Fatalf("成功创建 = %v", res)
	}
	if res["manual_url"] == "" {
		t.Fatal("应带 manual_url")
	}

	// HTTP 403 → error 分支
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden body tok123", http.StatusForbidden)
	}))
	defer srv2.Close()
	g2 := newTestGitOps(newFakeRunner())
	g2.apiBase = srv2.URL
	res2 := g2.CreatePR("t", "b", "feat")
	if res2["success"] != false || res2["http_status"] != http.StatusForbidden {
		t.Fatalf("HTTP 错误 = %v", res2)
	}
	// 响应体中的 token 应被脱敏
	if strings.Contains(res2["response_body"].(string), "tok123") {
		t.Fatalf("错误体未脱敏: %v", res2["response_body"])
	}

	// 200 但无 URL → no URL 分支
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"weird": 1}`)
	}))
	defer srv3.Close()
	g3 := newTestGitOps(newFakeRunner())
	g3.apiBase = srv3.URL
	res3 := g3.CreatePR("t", "b", "feat")
	if res3["success"] != false || res3["error"] != "GitCode PR creation returned no URL" {
		t.Fatalf("无 URL = %v", res3)
	}
}

// TestGitOperations_DiffNameOnly 验证路径归一化与去重
func TestGitOperations_DiffNameOnly(t *testing.T) {
	runner := newFakeRunner()
	runner.resps["git diff --name-only HEAD"] = CommandResult{ExitCode: 0, Output: "a.py\nb.py\na.py\n"}
	runner.resps["git diff --name-only origin/develop...HEAD"] = CommandResult{ExitCode: 0, Output: "c.py\n"}
	g := newTestGitOps(runner)
	if files := g.DiffNameOnly("HEAD"); len(files) != 2 || files[0] != "a.py" {
		t.Fatalf("DiffNameOnly = %v", files)
	}
	if files := g.DiffNameOnlyAgainstBase(); len(files) != 1 || files[0] != "c.py" {
		t.Fatalf("AgainstBase = %v", files)
	}
}
