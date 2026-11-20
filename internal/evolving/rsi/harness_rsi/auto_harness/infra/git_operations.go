package infra

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 组件级日志常量（agentcore 域）
const logComponent = logger.ComponentAgentCore

// defaultGitCodeAPIBase GitCode API 基地址（D-e2-7：测试可注入替换）
const defaultGitCodeAPIBase = "https://api.gitcode.com"

// ──────────────────────────── 结构体 ────────────────────────────

// GitOperations Git 操作（orchestrator 基础设施，不继承 Tool）。
//
// 对齐 class GitOperations：branch / push / PR (GitCode API)。
type GitOperations struct {
	// runner 命令执行注入缝
	runner CommandRunner
	// ctx 命令执行上下文（Go 侧新增；Python asyncio 语义由调用方协程化）
	ctx context.Context
	// workspace git 仓库工作目录
	workspace string
	// remote fork 远程名称
	remote string
	// baseBranch 目标分支
	baseBranch string
	// forkOwner fork 所有者
	forkOwner string
	// upstreamOwner 上游仓库所有者
	upstreamOwner string
	// upstreamRepo 上游仓库名称
	upstreamRepo string
	// userName git commit 用户名
	userName string
	// userEmail git commit 邮箱
	userEmail string
	// gitcodeUsername GitCode 登录用户名
	gitcodeUsername string
	// token GitCode API token
	token string
	// gitEnv 认证环境
	gitEnv map[string]string
	// apiBase GitCode API 基地址（测试注入；默认 https://api.gitcode.com）
	apiBase string
}

// GitOperationsOption GitOperations 构造选项
type GitOperationsOption func(*GitOperations)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGitOperations 构造 Git 操作器（对齐 __init__；token 为空回落
// GITCODE_ACCESS_TOKEN 环境变量）。
func NewGitOperations(
	ctx context.Context,
	runner CommandRunner,
	workspace string,
	remote string,
	baseBranch string,
	forkOwner string,
	upstreamOwner string,
	upstreamRepo string,
	gitcodeUsername string,
	gitcodeToken string,
	userName string,
	userEmail string,
	opts ...GitOperationsOption,
) *GitOperations {
	token := gitcodeToken
	if token == "" {
		token = os.Getenv("GITCODE_ACCESS_TOKEN")
	}
	g := &GitOperations{
		runner:          runner,
		ctx:             ctx,
		workspace:       workspace,
		remote:          remote,
		baseBranch:      baseBranch,
		forkOwner:       forkOwner,
		upstreamOwner:   upstreamOwner,
		upstreamRepo:    upstreamRepo,
		userName:        userName,
		userEmail:       userEmail,
		gitcodeUsername: gitcodeUsername,
		token:           token,
		gitEnv:          BuildGitAuthEnv(gitcodeUsername, token),
		apiBase:         defaultGitCodeAPIBase,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// WithAPIBase 覆盖 GitCode API 基地址（测试注入用，D-e2-7）
func WithAPIBase(base string) GitOperationsOption {
	return func(g *GitOperations) { g.apiBase = base }
}

// SetWorkspace 更新 git 命令工作目录（对齐 set_workspace）
func (g *GitOperations) SetWorkspace(workspace string) {
	g.workspace = workspace
}

// CreateBranch 创建并切换到新分支（对齐 create_branch）
func (g *GitOperations) CreateBranch(branchName string) map[string]any {
	code, out := g.git([]string{"checkout", "-b", branchName}, nil)
	return map[string]any{
		"success": code == 0,
		"branch":  branchName,
		"output":  out,
	}
}

// CollectStatus 以结构化形式收集当前 git 状态（对齐 collect_status）
func (g *GitOperations) CollectStatus() map[string][]string {
	code, out := g.git([]string{"status", "--porcelain", "--untracked-files=all"}, nil)
	status := map[string][]string{
		"dirty_files":           {},
		"tracked_modified_files": {},
		"untracked_files":       {},
		"renamed_files":         {},
	}
	if code != 0 || out == "" {
		return status
	}

	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) < 4 {
			continue
		}
		marker := line[:2]
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		if strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			newPath := strings.TrimSpace(parts[1])
			status["renamed_files"] = append(status["renamed_files"], newPath)
			path = newPath
		}
		normalized := strings.ReplaceAll(path, "\\", "/")
		status["dirty_files"] = append(status["dirty_files"], normalized)
		if marker == "??" {
			status["untracked_files"] = append(status["untracked_files"], normalized)
		} else {
			status["tracked_modified_files"] = append(status["tracked_modified_files"], normalized)
		}
	}

	// dict.fromkeys 去重保序
	for key, values := range status {
		status[key] = dedupePreserveOrder(values)
	}
	return status
}

// ListDirtyFiles 返回当前脏文件（对齐 list_dirty_files）
func (g *GitOperations) ListDirtyFiles() []string {
	status := g.CollectStatus()
	return status["dirty_files"]
}

// CurrentBranch 返回当前分支名（对齐 current_branch）
func (g *GitOperations) CurrentBranch() string {
	_, out := g.git([]string{"rev-parse", "--abbrev-ref", "HEAD"}, nil)
	return strings.TrimSpace(out)
}

// CurrentHead 返回当前 HEAD sha（对齐 current_head）
func (g *GitOperations) CurrentHead() string {
	_, out := g.git([]string{"rev-parse", "HEAD"}, nil)
	return strings.TrimSpace(out)
}

// DiffStat 返回 git diff --stat 摘要（对齐 diff_stat）
func (g *GitOperations) DiffStat(paths []string) string {
	args := []string{"diff", "--stat"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	_, out := g.git(args, nil)
	return strings.TrimSpace(out)
}

// DiffStatAgainstBase 返回当前分支相对 base 的 diff stat（对齐 diff_stat_against_base）
func (g *GitOperations) DiffStatAgainstBase() string {
	_, out := g.git([]string{"diff", "--stat", fmt.Sprintf("origin/%s...HEAD", g.baseBranch)}, nil)
	return strings.TrimSpace(out)
}

// AddPaths 暂存显式 repo 相对路径（对齐 add_paths）
func (g *GitOperations) AddPaths(paths []string) map[string]any {
	if len(paths) == 0 {
		return map[string]any{
			"success": false,
			"output":  "no paths provided",
		}
	}
	args := append([]string{"add", "--"}, paths...)
	code, out := g.git(args, nil)
	return map[string]any{
		"success": code == 0,
		"output":  out,
	}
}

// Commit 在当前工作区创建提交（对齐 commit）
func (g *GitOperations) Commit(message string) map[string]any {
	code, out := g.git([]string{"commit", "-m", message}, nil)
	return map[string]any{
		"success": code == 0,
		"output":  out,
	}
}

// DiffNameOnly 返回 git diff --name-only <revision> 归一化路径（对齐 diff_name_only）
func (g *GitOperations) DiffNameOnly(revision string) []string {
	_, out := g.git([]string{"diff", "--name-only", revision}, nil)
	return normalizePathLines(out)
}

// DiffNameOnlyAgainstBase 返回当前分支相对 base 变更文件（对齐 diff_name_only_against_base）
func (g *GitOperations) DiffNameOnlyAgainstBase() []string {
	_, out := g.git([]string{"diff", "--name-only", fmt.Sprintf("origin/%s...HEAD", g.baseBranch)}, nil)
	return normalizePathLines(out)
}

// HasCommitsAgainstBase 返回 HEAD 是否含 origin/base 之外的提交（对齐 has_commits_against_base）
func (g *GitOperations) HasCommitsAgainstBase() bool {
	code, out := g.git([]string{"rev-list", "--count", fmt.Sprintf("origin/%s..HEAD", g.baseBranch)}, nil)
	if code != 0 {
		return false
	}
	n, err := parseIntOrZero(out)
	if err != nil {
		return false
	}
	return n > 0
}

// FindExistingIssueFixRef 查找承载该 issue 修复的既有本地/远程分支
// （对齐 find_existing_issue_fix_ref；打分与排序逐条照抄）
func (g *GitOperations) FindExistingIssueFixRef(issueNumber string, allowedFiles []string) map[string]any {
	if issueNumber == "" {
		return map[string]any{"success": false, "reason": "missing issue number"}
	}
	currentBranch := g.CurrentBranch()
	code, out := g.git([]string{
		"for-each-ref",
		"--format=%(refname:short)",
		"refs/heads",
		"refs/remotes/origin",
		"refs/remotes/autoharness",
	}, nil)
	if code != 0 {
		return map[string]any{"success": false, "reason": out}
	}

	needle := fmt.Sprintf("issue-%s", issueNumber)
	exactNeedles := []string{
		fmt.Sprintf("fix/issue-%s", issueNumber),
		fmt.Sprintf("fix-issue-%s", issueNumber),
		fmt.Sprintf("auto-harness/fix-issue-%s", issueNumber),
	}
	allowed := map[string]bool{}
	for _, f := range allowedFiles {
		allowed[f] = true
	}
	var candidates []map[string]any
	for _, ref := range dedupePreserveOrder(splitTrimLines(out)) {
		if ref == "" {
			continue
		}
		normalized := strings.TrimPrefix(strings.TrimPrefix(ref, "origin/"), "autoharness/")
		if normalized == currentBranch {
			continue
		}
		lowerRef := strings.ToLower(normalized)
		matched := strings.Contains(lowerRef, needle)
		for _, value := range exactNeedles {
			if strings.Contains(lowerRef, value) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		count := g.commitCountAgainstBase(ref)
		if count <= 0 {
			continue
		}
		files := g.diffNameOnlyForRefAgainstBase(ref)
		overlap := 0
		if len(allowed) > 0 {
			for _, f := range files {
				if allowed[f] {
					overlap++
				}
			}
		}
		score := overlap*100 - count
		if strings.HasPrefix(lowerRef, "fix/") {
			score += 50
		}
		if strings.HasPrefix(lowerRef, "fix-issue-") {
			score += 25
		}
		candidates = append(candidates, map[string]any{
			"ref":          ref,
			"commit_count": count,
			"files":        files,
			"score":        score,
		})
	}
	if len(candidates) == 0 {
		return map[string]any{
			"success": false,
			"reason":  fmt.Sprintf("no existing branch found for issue %s", issueNumber),
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i]["score"].(int) > candidates[j]["score"].(int)
	})
	result := map[string]any{"success": true}
	for k, v := range candidates[0] {
		result[k] = v
	}
	return result
}

// CherryPickRefCommits 将 origin/base..ref 的提交 cherry-pick 到当前分支
// （对齐 cherry_pick_ref_commits；失败即 abort）
func (g *GitOperations) CherryPickRefCommits(ref string) map[string]any {
	code, out := g.git([]string{"rev-list", "--reverse", fmt.Sprintf("origin/%s..%s", g.baseBranch, ref)}, nil)
	if code != 0 {
		return map[string]any{"success": false, "output": out}
	}
	var commits []string
	for _, line := range splitTrimLines(out) {
		if line != "" {
			commits = append(commits, line)
		}
	}
	if len(commits) == 0 {
		return map[string]any{"success": false, "output": "no commits to cherry-pick"}
	}
	var outputs []string
	for _, commit := range commits {
		code, pickOut := g.git([]string{"cherry-pick", commit}, nil)
		outputs = append(outputs, pickOut)
		if code != 0 {
			g.git([]string{"cherry-pick", "--abort"}, nil)
			return map[string]any{
				"success": false,
				"output":  strings.Join(outputs, "\n"),
				"commits": commits,
			}
		}
	}
	return map[string]any{
		"success": true,
		"output":  strings.Join(outputs, "\n"),
		"commits": commits,
	}
}

// StatusPorcelain 返回原始 git status --porcelain 输出（对齐 status_porcelain）
func (g *GitOperations) StatusPorcelain() string {
	_, out := g.git([]string{"status", "--porcelain", "--untracked-files=all"}, nil)
	return rstrip(out)
}

// ShowLastCommitStat 返回最新提交的紧凑摘要（对齐 show_last_commit_stat）
func (g *GitOperations) ShowLastCommitStat() string {
	_, out := g.git([]string{"show", "--stat", "--format=fuller", "-1"}, nil)
	return strings.TrimSpace(out)
}

// DiscardWorktreeChanges 通过 git checkout . 丢弃当前工作树变更（对齐 discard_worktree_changes）
func (g *GitOperations) DiscardWorktreeChanges() bool {
	code, _ := g.git([]string{"checkout", "."}, nil)
	return code == 0
}

// DiffAgainst 返回 git diff <revision> 输出（对齐 diff_against）
func (g *GitOperations) DiffAgainst(revision string) string {
	_, out := g.git([]string{"diff", revision}, nil)
	return out
}

// DiffAgainstBase 返回当前分支相对 base 的完整 diff（对齐 diff_against_base）
func (g *GitOperations) DiffAgainstBase() string {
	_, out := g.git([]string{"diff", fmt.Sprintf("origin/%s...HEAD", g.baseBranch)}, nil)
	return out
}

// Push 推送到 fork 远程（对齐 push；三次尝试链与诊断逐条照抄）
func (g *GitOperations) Push(branchName string) map[string]any {
	attempts := []struct {
		label string
		args  []string
	}{
		{"branch force-with-lease push", []string{"push", "-u", "--force-with-lease", g.remote, branchName}},
		{"branch push", []string{"push", "-u", g.remote, branchName}},
		{"HEAD refspec push", []string{"push", "-u", g.remote, fmt.Sprintf("HEAD:refs/heads/%s", branchName)}},
	}
	_, remoteURL := g.git([]string{"remote", "get-url", g.remote}, nil)
	_, remotePushURL := g.git([]string{"remote", "get-url", "--push", g.remote}, nil)
	_, originPushURL := g.git([]string{"remote", "get-url", "--push", "origin"}, nil)
	context := g.collectPushContext(
		branchName,
		strings.TrimSpace(remoteURL),
		strings.TrimSpace(remotePushURL),
		strings.TrimSpace(originPushURL),
	)
	logger.Info(logComponent).Str("context", context).Msg("[AutoHarnessGitPush] context")
	var outputs []string
	outputs = append(outputs, strings.TrimSpace(fmt.Sprintf("[push context]\n%s", context)))
	for _, attempt := range attempts {
		safeCmd := g.sanitizeGitArgs(attempt.args)
		logger.Info(logComponent).
			Str("label", attempt.label).
			Str("cmd", safeCmd).
			Msg("[AutoHarnessGitPush] attempt start")
		startedAt := time.Now()
		code, out := g.git(attempt.args, nil)
		elapsed := time.Since(startedAt).Seconds()
		sanitized := g.sanitizeGitOutput(out)
		logger.Info(logComponent).
			Str("label", attempt.label).
			Int("code", code).
			Float64("elapsed", elapsed).
			Str("output", sanitized).
			Msg("[AutoHarnessGitPush] attempt end")
		outputs = append(outputs, strings.TrimSpace(fmt.Sprintf(
			"[%s]\ncmd=%s\ncode=%d\nelapsed=%.2fs\n%s", attempt.label, safeCmd, code, elapsed, sanitized)))
		if code == 0 {
			return map[string]any{
				"success": true,
				"output":  strings.Join(outputs, "\n\n"),
			}
		}
	}
	diagnostics := g.collectPushDiagnostics(branchName)
	logger.Warn(logComponent).
		Str("diagnostics", diagnostics).
		Msg("[AutoHarnessGitPush] all attempts failed")
	outputs = append(outputs, strings.TrimSpace(fmt.Sprintf("[push diagnostics]\n%s", diagnostics)))
	return map[string]any{
		"success": false,
		"output":  strings.Join(outputs, "\n\n"),
	}
}

// BuildPRWebURL 返回手动创建 PR/MR 的 GitCode 网页 URL（对齐 build_pr_web_url）
func (g *GitOperations) BuildPRWebURL(headBranch string) string {
	if g.forkOwner == "" || g.upstreamRepo == "" {
		return ""
	}
	sourceBranch := url.PathEscape(headBranch)
	targetBranch := url.PathEscape(g.baseBranch)
	return fmt.Sprintf(
		"https://gitcode.com/%s/%s/merge_requests/new?source_branch=%s&target_branch=%s",
		g.forkOwner, g.upstreamRepo, sourceBranch, targetBranch,
	)
}

// CreatePR 异步创建 PR（对齐 create_pr；Go 同步执行，D-e2-4）
func (g *GitOperations) CreatePR(title string, body string, headBranch string) map[string]any {
	return g.createPRSync(title, body, headBranch)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// git 执行 git 命令并返回 (returncode, output)（对齐 _git）
func (g *GitOperations) git(args []string, extraEnv map[string]string) (int, string) {
	env := make(map[string]string, len(g.gitEnv))
	for k, v := range g.gitEnv {
		env[k] = v
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	result, err := g.runner.Run(g.ctx, g.workspace, envMapToSlice(env), "git", args...)
	if err != nil {
		// 启动失败（如未安装 git）按 returncode 非 0 语义返回
		return 1, result.Output + err.Error()
	}
	return result.ExitCode, rstrip(result.Output)
}

// sanitizeGitOutput 移除 git 命令输出中的凭据（对齐 _sanitize_git_output）
func (g *GitOperations) sanitizeGitOutput(output string) string {
	sanitized := output
	if g.token != "" {
		sanitized = strings.ReplaceAll(sanitized, g.token, "***")
		if g.gitcodeUsername != "" {
			raw := fmt.Sprintf("%s:%s", g.gitcodeUsername, g.token)
			sanitized = strings.ReplaceAll(
				sanitized,
				base64.StdEncoding.EncodeToString([]byte(raw)),
				"***",
			)
		}
	}
	if g.gitcodeUsername != "" {
		re1 := regexp.MustCompile(regexp.QuoteMeta(g.gitcodeUsername) + ":[^@\\s]+@")
		sanitized = re1.ReplaceAllString(sanitized, "***:***@")
		sanitized = strings.ReplaceAll(
			sanitized,
			fmt.Sprintf("%s:***@", g.gitcodeUsername),
			"***:***@",
		)
	}
	re2 := regexp.MustCompile(`(?i)(authorization|private-token|access_token)([=:]\s*)[^\s&]+`)
	sanitized = re2.ReplaceAllString(sanitized, "${1}${2}***")
	return sanitized
}

// sanitizeGitArgs 返回用于日志的安全单行 git 命令（对齐 _sanitize_git_args）
func (g *GitOperations) sanitizeGitArgs(args []string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = g.sanitizeGitOutput(arg)
	}
	return "git " + strings.Join(parts, " ")
}

// authenticatedRemoteURL 返回携带 basic auth 凭据的 HTTPS 远程 URL
// （对齐 _authenticated_remote_url；无可注入凭据时返回空串）
func (g *GitOperations) authenticatedRemoteURL(remoteURL string) string {
	if !strings.HasPrefix(remoteURL, "https://") || g.gitcodeUsername == "" || g.token == "" {
		return ""
	}
	parts, err := url.Parse(remoteURL)
	if err != nil {
		return ""
	}
	username := url.QueryEscape(g.gitcodeUsername)
	token := url.QueryEscape(g.token)
	parts.User = url.UserPassword(username, token)
	return parts.String()
}

// commitCountAgainstBase 返回 ref 中不可达自 origin/base 的提交数
// （对齐 _commit_count_against_base）
func (g *GitOperations) commitCountAgainstBase(ref string) int {
	code, out := g.git([]string{"rev-list", "--count", fmt.Sprintf("origin/%s..%s", g.baseBranch, ref)}, nil)
	if code != 0 {
		return 0
	}
	n, err := parseIntOrZero(out)
	if err != nil {
		return 0
	}
	return n
}

// diffNameOnlyForRefAgainstBase 返回候选 ref 相对 origin/base 的变更文件
// （对齐 _diff_name_only_for_ref_against_base）
func (g *GitOperations) diffNameOnlyForRefAgainstBase(ref string) []string {
	_, out := g.git([]string{"diff", "--name-only", fmt.Sprintf("origin/%s...%s", g.baseBranch, ref)}, nil)
	return normalizePathLines(out)
}

// collectPushContext 收集安全的推送上下文用于诊断远程失败（对齐 _collect_push_context）
func (g *GitOperations) collectPushContext(branchName, remoteURL, remotePushURL, originPushURL string) string {
	checks := []struct {
		label string
		args  []string
	}{
		{"workspace", []string{"rev-parse", "--show-toplevel"}},
		{"current_branch", []string{"branch", "--show-current"}},
		{"head", []string{"rev-parse", "HEAD"}},
		{"head_subject", []string{"log", "-1", "--pretty=%s"}},
		{"status", []string{"status", "--short", "--branch"}},
		{"push_default", []string{"config", "--get", "push.default"}},
		{"remote_pushdefault", []string{"config", "--get", "remote.pushDefault"}},
		{"http_version", []string{"config", "--get", "http.version"}},
		{"extraheader_configured", []string{"config", "--get", "http.https://gitcode.com/.extraheader"}},
	}
	lines := []string{
		fmt.Sprintf("workspace_config=%s", g.workspace),
		fmt.Sprintf("remote=%s", g.remote),
		fmt.Sprintf("branch_name=%s", branchName),
		fmt.Sprintf("remote_url=%s", g.sanitizeGitOutput(remoteURL)),
		fmt.Sprintf("remote_push_url=%s", g.sanitizeGitOutput(remotePushURL)),
		fmt.Sprintf("origin_push_url=%s", g.sanitizeGitOutput(originPushURL)),
	}
	for _, check := range checks {
		code, out := g.git(check.args, nil)
		value := g.sanitizeGitOutput(out)
		if check.label == "extraheader_configured" && value != "" {
			value = "yes"
		}
		lines = append(lines, fmt.Sprintf("%s: code=%d value=%s", check.label, code, value))
	}
	return strings.Join(lines, "\n")
}

// collectPushDiagnostics 推送失败后运行只读检查与带 trace 的 dry-run
// （对齐 _collect_push_diagnostics）
func (g *GitOperations) collectPushDiagnostics(branchName string) string {
	traceEnv := map[string]string{
		"GIT_TRACE":           "1",
		"GIT_CURL_VERBOSE":    "1",
		"GIT_TRACE_CURL":      "1",
		"GIT_TERMINAL_PROMPT": "0",
	}
	checks := []struct {
		label string
		args  []string
		env   map[string]string
	}{
		{"remote -v", []string{"remote", "-v"}, nil},
		{"status", []string{"status", "--short", "--branch"}, nil},
		{"current branch", []string{"branch", "--show-current"}, nil},
		{"head", []string{"rev-parse", "HEAD"}, nil},
		{"ls-remote HEAD", []string{"ls-remote", g.remote, "HEAD"}, nil},
		{"ls-remote target", []string{"ls-remote", g.remote, fmt.Sprintf("refs/heads/%s", branchName)}, nil},
		{"dry-run push", []string{"push", "--dry-run", g.remote, fmt.Sprintf("HEAD:refs/heads/%s", branchName)}, nil},
		{"dry-run push trace", []string{"push", "--dry-run", g.remote, fmt.Sprintf("HEAD:refs/heads/%s", branchName)}, traceEnv},
	}
	var lines []string
	for _, check := range checks {
		startedAt := time.Now()
		code, out := g.git(check.args, check.env)
		elapsed := time.Since(startedAt).Seconds()
		sanitized := g.sanitizeGitOutput(out)
		lines = append(lines, strings.TrimSpace(fmt.Sprintf(
			"[%s]\ncmd=%s\ncode=%d\nelapsed=%.2fs\n%s",
			check.label, g.sanitizeGitArgs(check.args), code, elapsed, sanitized)))
	}
	return strings.Join(lines, "\n\n")
}

// extractPRURL 从 GitCode 多种响应形态中提取 PR/MR URL（对齐 _extract_pr_url）
func (g *GitOperations) extractPRURL(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{
			"html_url", "web_url", "url", "pr_url", "pull_request_url", "merge_request_url",
		} {
			if candidate, ok := v[key].(string); ok && strings.HasPrefix(candidate, "http") {
				return candidate
			}
		}
		for _, key := range []string{"data", "pull_request", "merge_request", "result"} {
			if candidate := g.extractPRURL(v[key]); candidate != "" {
				return candidate
			}
		}
		// dict.values() 顺序在 Go map 上不确定——Python 3.7+ 为插入序；
		// 为可复现按键排序遍历（D-e2-10：值序遍历确定性化）
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if candidate := g.extractPRURL(v[k]); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, item := range v {
			if candidate := g.extractPRURL(item); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

// createPRSync 通过 GitCode API 创建 MR（对齐 _create_pr_sync）
func (g *GitOperations) createPRSync(title string, body string, headBranch string) map[string]any {
	owner := url.PathEscape(g.upstreamOwner)
	repo := url.PathEscape(g.upstreamRepo)
	apiURL := fmt.Sprintf("%s/api/v5/repos/%s/%s/pulls?access_token=%s", g.apiBase, owner, repo, g.token)
	requestPayload := map[string]any{
		"title": title,
		"head":  fmt.Sprintf("%s:%s", g.forkOwner, headBranch),
		"base":  g.baseBranch,
		"body":  body,
	}
	payload, _ := json.Marshal(requestPayload)
	manualURL := g.BuildPRWebURL(headBranch)
	diagnosticContext := map[string]any{
		"api":         fmt.Sprintf("POST /repos/%s/%s/pulls", g.upstreamOwner, g.upstreamRepo),
		"target_repo": fmt.Sprintf("%s/%s", g.upstreamOwner, g.upstreamRepo),
		"source_repo": fmt.Sprintf("%s/%s", g.forkOwner, g.upstreamRepo),
		"head":        requestPayload["head"],
		"base":        g.baseBranch,
		"title":       title,
		"body_bytes":  len([]byte(body)),
		"manual_url":  manualURL,
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		logger.Error(logComponent).
			Str("context", fmt.Sprintf("%v", diagnosticContext)).
			Err(err).
			Msg("GitCode PR creation failed")
		return map[string]any{
			"success":     false,
			"error":       err.Error(),
			"manual_url":  diagnosticContext["manual_url"],
			"diagnostics": diagnosticContext,
		}
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error(logComponent).
			Str("context", fmt.Sprintf("%v", diagnosticContext)).
			Err(err).
			Msg("GitCode PR creation failed")
		return map[string]any{
			"success":     false,
			"error":       err.Error(),
			"manual_url":  diagnosticContext["manual_url"],
			"diagnostics": diagnosticContext,
		}
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	bodyText := string(rawBody)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// HTTPError 分支
		sanitizedBody := g.sanitizeGitOutput(bodyText)
		reason := http.StatusText(resp.StatusCode)
		logger.Error(logComponent).
			Int("status", resp.StatusCode).
			Str("reason", reason).
			Str("context", fmt.Sprintf("%v", diagnosticContext)).
			Str("body", sanitizedBody).
			Msg("GitCode PR creation HTTP error")
		return map[string]any{
			"success":       false,
			"error":         fmt.Sprintf("HTTP %d %s: %s", resp.StatusCode, reason, sanitizedBody),
			"http_status":   resp.StatusCode,
			"response_body": sanitizedBody,
			"manual_url":    diagnosticContext["manual_url"],
			"diagnostics":   diagnosticContext,
		}
	}

	var data any
	if jsonErr := json.Unmarshal(rawBody, &data); jsonErr != nil {
		data = map[string]any{"raw_body": bodyText}
	}
	prURL := g.extractPRURL(data)
	if prURL != "" {
		return map[string]any{
			"success":     true,
			"pr_url":      prURL,
			"manual_url":  manualURL,
			"response":    data,
			"diagnostics": diagnosticContext,
		}
	}
	logger.Warn(logComponent).
		Str("context", fmt.Sprintf("%v", diagnosticContext)).
		Str("response", fmt.Sprintf("%v", data)).
		Str("manual_url", manualURL).
		Msg("GitCode PR creation returned no URL")
	return map[string]any{
		"success":     false,
		"error":       "GitCode PR creation returned no URL",
		"manual_url":  manualURL,
		"response":    data,
		"diagnostics": diagnosticContext,
	}
}

// normalizePathLines 输出按行归一化为路径列表（diff_name_only 共用；\→/，去重保序）
func normalizePathLines(out string) []string {
	var files []string
	for _, line := range strings.Split(out, "\n") {
		normalized := strings.ReplaceAll(strings.TrimSpace(line), "\\", "/")
		if normalized != "" {
			files = append(files, normalized)
		}
	}
	return dedupePreserveOrder(files)
}

// splitTrimLines 按行分割并去两端空白
func splitTrimLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines
}

// dedupePreserveOrder 保序去重（对齐 list(dict.fromkeys(value))）
func dedupePreserveOrder(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// parseIntOrZero 解析整数（对齐 int(out.strip() or "0")；失败返回错误）
func parseIntOrZero(out string) (int, error) {
	s := strings.TrimSpace(out)
	if s == "" {
		return 0, nil
	}
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
