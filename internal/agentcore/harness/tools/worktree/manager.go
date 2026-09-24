package worktree

import (
	"context"
	"fmt"
	fnmatch "github.com/danwakefield/fnmatch"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeManager Worktree 生命周期管理器。
// Python: WorktreeManager
//
// 协调 worktree 创建/删除、session 状态、post-creation setup、事件分发和 rail 调度。
// 这是唯一的业务逻辑入口 — tools 和 spawn 代码委托到这里。
//
// Manager 是 owner-agnostic 的：调用方提供可选的 event_handler 接收
// WorktreeCreatedEvent / WorktreeRemovedEvent 载荷并转换为各自系统的传输协议。
type WorktreeManager struct {
	// config Worktree 配置
	config WorktreeConfig
	// backend 后端实现
	backend WorktreeBackend
	// eventHandler 事件处理器
	eventHandler WorktreeEventHandler
	// lifecycleRails 生命周期 hook 列表
	lifecycleRails []WorktreeLifecycleRail
	// sessionState worktree 会话状态容器，tool 通过 manager 引用直接操作
	// 对齐 Python: WorktreeSessionState（Go 用 context.Value 传播改为 manager 直接持有）
	sessionState *WorktreeSessionState
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorktreeManager 创建新的 WorktreeManager。
// Python: WorktreeManager.__init__(config, backend, event_handler, rails)
func NewWorktreeManager(config WorktreeConfig, backend WorktreeBackend, opts ...ManagerOption) *WorktreeManager {
	o := &managerOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if backend == nil {
		b, err := CreateBackend("git", config)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("Failed to create default GitBackend, using empty config")
			backend = &GitBackend{config: config}
		} else {
			backend = b
		}
	}
	return &WorktreeManager{
		config:         config,
		backend:        backend,
		eventHandler:   o.eventHandler,
		lifecycleRails: o.lifecycleRails,
		sessionState:   InitWorktreeSessionState(),
	}
}

// Backend 返回后端访问器。
// Python: WorktreeManager.backend property
func (m *WorktreeManager) Backend() WorktreeBackend {
	return m.backend
}

// SessionState 返回 worktree 会话状态容器。
// tool 通过 manager 引用直接操作 session，绕开 ctx 传播限制。
func (m *WorktreeManager) SessionState() *WorktreeSessionState {
	return m.sessionState
}

// Config 返回配置访问器。
func (m *WorktreeManager) Config() WorktreeConfig {
	return m.config
}

// Enter 创建或恢复一个 worktree 并进入它。
// Python: WorktreeManager.enter(slug, *, member_name, team_name)
//
// 设置 ContextVar session。由 EnterWorktreeTool 调用。
func (m *WorktreeManager) Enter(ctx context.Context, slug, memberName, teamName string) (*WorktreeSession, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	repoRoot, err := findCanonicalGitRoot(ctx, cwd.GetCwd(ctx))
	if err != nil || repoRoot == "" {
		return nil, fmt.Errorf("无法创建 worktree: 不在 git 仓库中")
	}

	originalCwd := cwd.GetCwd(ctx)
	originalBranch, _ := getCurrentBranch(ctx, repoRoot)
	targetPath, err := m.resolveTargetPath(ctx, slug)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	result, err := m.backend.Create(ctx, slug, repoRoot, targetPath)
	if err != nil {
		return nil, fmt.Errorf("创建 worktree 失败: %w", err)
	}
	durationMs := time.Since(start).Seconds() * 1000

	if !result.Existed {
		m.postCreationSetup(ctx, repoRoot, result.WorktreePath)
	}

	session := &WorktreeSession{
		OriginalCWD:        originalCwd,
		WorktreePath:       result.WorktreePath,
		WorktreeName:       slug,
		WorktreeBranch:     result.WorktreeBranch,
		OriginalBranch:     originalBranch,
		OriginalHeadCommit: result.HeadCommit,
		MemberName:         memberName,
		TeamName:           teamName,
		HookBased:          result.HookBased,
		LifecyclePolicy:    m.resolvePolicy(),
		CreationDurationMs: durationMs,
		UsedSparsePaths:    len(m.config.SparsePaths) > 0,
	}

	m.sessionState.SetCurrentSession(session)

	logger.Info(logComponent).Str("slug", slug).Str("worktree_path", result.WorktreePath).
		Str("status", func() string {
			if result.Existed {
				return "recovered"
			}
			return "created"
		}()).Float64("duration_ms", durationMs).
		Msg("已进入 worktree")

	if m.eventHandler != nil {
		_ = m.eventHandler(ctx, &WorktreeCreatedEvent{
			WorktreeName: slug,
			WorktreePath: result.WorktreePath,
			OwnerID:      memberName,
			Tag:          teamName,
			Existed:      result.Existed,
		})
	}

	return session, nil
}

// Exit 退出当前 worktree 会话。
// Python: WorktreeManager.exit(action, *, discard_changes)
func (m *WorktreeManager) Exit(ctx context.Context, action string, discardChanges bool) (map[string]string, error) {
	session, err := RequireCurrentSession(ctx)
	if err != nil {
		return nil, err
	}

	if action == "remove" && !discardChanges {
		summary := m.CountChanges(ctx, session)
		// 失败即关闭：无法确定状态时拒绝删除
		if summary == nil {
			return nil, exception.BuildError(exception.StatusToolWorktreeExitInvalid,
				exception.WithParam("reason", fmt.Sprintf(
					"无法确认 %s 处的 worktree 状态。"+
						"拒绝在未显式确认的情况下删除。"+
						"设置 discard_changes=True 继续，或使用 action='keep' "+
						"保留 worktree。", session.WorktreePath)))
		}
		if summary.ChangedFiles > 0 || summary.Commits > 0 {
			parts := []string{}
			if summary.ChangedFiles > 0 {
				parts = append(parts, fmt.Sprintf("%d 个未提交文件", summary.ChangedFiles))
			}
			if summary.Commits > 0 {
				parts = append(parts, fmt.Sprintf("%d 个提交在 %s 上", summary.Commits, session.WorktreeBranch))
			}
			return nil, exception.BuildError(exception.StatusToolWorktreeExitInvalid,
				exception.WithParam("reason", fmt.Sprintf(
					"Worktree 存在 %s。"+
						"删除将永久丢弃此工作。"+
						"请与用户确认，然后设置 discard_changes=True 继续，"+
						"或使用 action='keep' 保留 worktree。",
					strings.Join(parts, " 和 "))))
		}
	}

	repoRoot, _ := findCanonicalGitRoot(ctx, session.OriginalCWD)

	if action == "keep" {
		SetCurrentSession(ctx, nil)
		logger.Info(logComponent).Str("worktree_name", session.WorktreeName).
			Str("worktree_path", session.WorktreePath).Msg("worktree preserved")
		return map[string]string{
			"action":          "keep",
			"original_cwd":    session.OriginalCWD,
			"worktree_path":   session.WorktreePath,
			"worktree_branch": session.WorktreeBranch,
		}, nil
	}

	// action 为 "remove"
	if repoRoot != "" {
		m.removeWorktreeInternal(ctx, session.WorktreePath, repoRoot)
	}

	SetCurrentSession(ctx, nil)

	if m.eventHandler != nil {
		_ = m.eventHandler(ctx, &WorktreeRemovedEvent{
			WorktreeName: session.WorktreeName,
			WorktreePath: session.WorktreePath,
			OwnerID:      session.MemberName,
			Tag:          session.TeamName,
		})
	}

	logger.Info(logComponent).Str("worktree_name", session.WorktreeName).
		Str("worktree_path", session.WorktreePath).Msg("worktree removed")
	return map[string]string{
		"action":          "remove",
		"original_cwd":    session.OriginalCWD,
		"worktree_path":   session.WorktreePath,
		"worktree_branch": session.WorktreeBranch,
	}, nil
}

// CreateOwnerWorktree 为 caller-defined owner 创建轻量级 worktree。
// Python: WorktreeManager.create_owner_worktree(slug)
//
// 与 Enter 不同，不修改 ContextVar session 或更改进程 CWD。
func (m *WorktreeManager) CreateOwnerWorktree(ctx context.Context, slug string) (*WorktreeCreateResult, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	repoRoot, err := findCanonicalGitRoot(ctx, cwd.GetCwd(ctx))
	if err != nil || repoRoot == "" {
		return nil, fmt.Errorf("无法创建 owner worktree: 不在 git 仓库中")
	}

	targetPath, err := m.resolveTargetPath(ctx, slug)
	if err != nil {
		return nil, err
	}
	result, err := m.backend.Create(ctx, slug, repoRoot, targetPath)
	if err != nil {
		return nil, err
	}

	if !result.Existed {
		m.postCreationSetup(ctx, repoRoot, result.WorktreePath)
	} else {
		// 更新 mtime 防止清理
		now := time.Now()
		_ = os.Chtimes(result.WorktreePath, now, now)
	}

	return result, nil
}

// CreateAgentWorktree 向后兼容别名。
// Python: create_agent_worktree = create_owner_worktree
func (m *WorktreeManager) CreateAgentWorktree(ctx context.Context, slug string) (*WorktreeCreateResult, error) {
	return m.CreateOwnerWorktree(ctx, slug)
}

// RecoverWorktreeForOwner 恢复持久 owner 的 worktree session。
// Python: WorktreeManager.recover_worktree_for_owner(owner_id, tag)
func (m *WorktreeManager) RecoverWorktreeForOwner(ctx context.Context, ownerID, tag string) (*WorktreeSession, error) {
	slug := ownerSlug(ownerID)
	repoRoot, err := findCanonicalGitRoot(ctx, cwd.GetCwd(ctx))
	if err != nil || repoRoot == "" {
		return nil, nil
	}

	wtPath, err := m.resolveTargetPath(ctx, slug)
	if err != nil {
		return nil, nil
	}
	headSHA, err := readWorktreeHeadSHA(wtPath)
	if err != nil || headSHA == "" {
		return nil, nil
	}

	branch, _ := getCurrentBranch(ctx, wtPath)
	return &WorktreeSession{
		OriginalCWD:        repoRoot,
		WorktreePath:       wtPath,
		WorktreeName:       slug,
		WorktreeBranch:     branch,
		OriginalHeadCommit: headSHA,
		MemberName:         ownerID,
		TeamName:           tag,
		LifecyclePolicy:    m.resolvePolicy(),
	}, nil
}

// RecoverWorktreeForMember 向后兼容别名。
// Python: recover_worktree_for_member = recover_worktree_for_owner
func (m *WorktreeManager) RecoverWorktreeForMember(ctx context.Context, memberName, teamName string) (*WorktreeSession, error) {
	return m.RecoverWorktreeForOwner(ctx, memberName, teamName)
}

// CountChanges 计算未提交变更和新提交。
// Python: WorktreeManager.count_changes(session)
//
// 返回 nil 表示无法确定（fail-closed）。
func (m *WorktreeManager) CountChanges(ctx context.Context, session *WorktreeSession) *WorktreeChangeSummary {
	changes, err := statusPorcelain(ctx, session.WorktreePath)
	if err != nil {
		return nil
	}
	changedFiles := len(changes)

	if session.OriginalHeadCommit == "" {
		return nil
	}

	commits := countCommitsSince(ctx, session.OriginalHeadCommit, session.WorktreePath)
	if commits == nil {
		return nil
	}

	return &WorktreeChangeSummary{
		ChangedFiles: changedFiles,
		Commits:      *commits,
	}
}

// CleanupWorktreesByPrefix 按 slug 前缀批量清理。
// Python: WorktreeManager.cleanup_worktrees_by_prefix(slug_prefix, *, force)
func (m *WorktreeManager) CleanupWorktreesByPrefix(ctx context.Context, slugPrefix string, force bool) ([]string, error) {
	policy := m.resolvePolicy()
	if policy == WorktreeLifecyclePolicyDurable && !force {
		logger.Info(logComponent).Str("slug_prefix", slugPrefix).
			Msg("跳过 worktree 清理：持久策略已激活")
		return nil, nil
	}

	repoRoot, err := findCanonicalGitRoot(ctx, cwd.GetCwd(ctx))
	if err != nil || repoRoot == "" {
		return nil, nil
	}

	workspace := cwd.GetWorkspace(ctx)
	if workspace == "" {
		logger.Info(logComponent).Str("slug_prefix", slugPrefix).
			Msg("跳过 worktree 清理：Agent 工作区未设置")
		return nil, nil
	}

	wtDir := WorktreesDir(workspace)
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return nil, nil
	}

	var removed []string
	for _, entry := range entries {
		slug := entry.Name()
		if !strings.HasPrefix(slug, slugPrefix) {
			continue
		}
		wtPath := filepath.Join(wtDir, slug)

		if !force {
			summary := m.checkChanges(ctx, wtPath)
			if summary != nil && (summary.ChangedFiles > 0 || summary.Commits > 0) {
				logger.Warn(logComponent).Str("slug", slug).
					Msg("跳过 worktree：存在未提交变更")
				continue
			}
		}

		if m.removeWorktreeInternal(ctx, wtPath, repoRoot) {
			removed = append(removed, wtPath)
			if m.eventHandler != nil {
				_ = m.eventHandler(ctx, &WorktreeRemovedEvent{
					WorktreeName: slug,
					WorktreePath: wtPath,
				})
			}
		}
	}

	if len(removed) > 0 {
		_ = worktreePrune(ctx, repoRoot)
	}

	return removed, nil
}

// CleanupTeamWorktrees 团队调用方的批量清理包装。
// Python: WorktreeManager.cleanup_team_worktrees(team_name, *, force)
func (m *WorktreeManager) CleanupTeamWorktrees(ctx context.Context, teamName string, force bool) ([]string, error) {
	return m.CleanupWorktreesByPrefix(ctx, "teammate-", force)
}

// RemoveWorktree 删除单个 worktree。
// Python: WorktreeManager.remove_worktree(worktree_path, repo_root)
func (m *WorktreeManager) RemoveWorktree(ctx context.Context, worktreePath, repoRoot string) bool {
	return m.removeWorktreeInternal(ctx, worktreePath, repoRoot)
}

// resolveTargetPath 计算 worktree 文件系统路径。
// Python: WorktreeManager._resolve_target_path(slug)
//
// workspace 为空时直接返回 error，对齐 Python 抛 RuntimeError。
func (m *WorktreeManager) resolveTargetPath(ctx context.Context, slug string) (string, error) {
	workspace := cwd.GetWorkspace(ctx)
	if workspace == "" {
		return "", fmt.Errorf("workspace 未设置，无法解析 worktree 目标路径")
	}
	return WorktreePathFor(workspace, slug), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ownerSlug 从 owner 标识派生 worktree slug。
// Python: WorktreeManager._owner_slug(owner_id)
func ownerSlug(ownerID string) string {
	if len(ownerID) > 8 {
		return "teammate-" + ownerID[:8]
	}
	return "teammate-" + ownerID
}

// resolvePolicy 解析有效的生命周期策略。
// Python: WorktreeManager._resolve_policy()
func (m *WorktreeManager) resolvePolicy() WorktreeLifecyclePolicy {
	if m.config.LifecyclePolicy != WorktreeLifecyclePolicyAuto {
		return m.config.LifecyclePolicy
	}
	return WorktreeLifecyclePolicyEphemeral
}

// fireBeforeCreate 调用 BeforeWorktreeCreate hook。
// 返回最后一个非 nil slug 修改，nil 表示不干预。
// Python: WorktreeManager._fire_rail("before_worktree_create", slug, repo_root)
func (m *WorktreeManager) fireBeforeCreate(ctx context.Context, cbc *interfaces.AgentCallbackContext, slug, repoRoot string) (*string, error) {
	var lastResult *string
	for _, rail := range m.lifecycleRails {
		r, err := rail.BeforeWorktreeCreate(ctx, cbc, slug, repoRoot)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("fireBeforeCreate hook failed")
			continue
		}
		if r != nil {
			lastResult = r
		}
	}
	return lastResult, nil
}

// fireAfterCreate 调用 AfterWorktreeCreate hook。
// Python: WorktreeManager._fire_rail("after_worktree_create", session)
func (m *WorktreeManager) fireAfterCreate(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession) {
	for _, rail := range m.lifecycleRails {
		if err := rail.AfterWorktreeCreate(ctx, cbc, session); err != nil {
			logger.Warn(logComponent).Err(err).Msg("fireAfterCreate hook failed")
		}
	}
}

// fireBeforeExit 调用 BeforeWorktreeExit hook。
// 返回最后一个非 nil action 修改，nil 表示不干预。
// Python: WorktreeManager._fire_rail("before_worktree_exit", session, action)
func (m *WorktreeManager) fireBeforeExit(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession, action string) (*string, error) {
	var lastResult *string
	for _, rail := range m.lifecycleRails {
		r, err := rail.BeforeWorktreeExit(ctx, cbc, session, action)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("fireBeforeExit hook failed")
			continue
		}
		if r != nil {
			lastResult = r
		}
	}
	return lastResult, nil
}

// fireAfterExit 调用 AfterWorktreeExit hook。
// Python: WorktreeManager._fire_rail("after_worktree_exit", session, action)
func (m *WorktreeManager) fireAfterExit(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession, action string) {
	for _, rail := range m.lifecycleRails {
		if err := rail.AfterWorktreeExit(ctx, cbc, session, action); err != nil {
			logger.Warn(logComponent).Err(err).Msg("fireAfterExit hook failed")
		}
	}
}

// fireOnFileWrite 调用 OnWorktreeFileWrite hook。
// 返回 false 表示任何 rail 阻止写入，true 表示允许。
// Python: WorktreeManager._fire_rail("on_worktree_file_write", session, file_path)
func (m *WorktreeManager) fireOnFileWrite(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession, filePath string) bool {
	for _, rail := range m.lifecycleRails {
		if !rail.OnWorktreeFileWrite(ctx, cbc, session, filePath) {
			return false
		}
	}
	return true
}

// fireBeforeCommit 调用 BeforeWorktreeCommit hook。
// 返回最后一个非 nil message 修改，nil 表示不干预。
// Python: WorktreeManager._fire_rail("before_worktree_commit", session, message)
func (m *WorktreeManager) fireBeforeCommit(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession, message string, files []string) (*string, error) {
	var lastResult *string
	for _, rail := range m.lifecycleRails {
		r, err := rail.BeforeWorktreeCommit(ctx, cbc, session, message, files)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("fireBeforeCommit hook failed")
			continue
		}
		if r != nil {
			lastResult = r
		}
	}
	return lastResult, nil
}

// fireAfterCommit 调用 AfterWorktreeCommit hook。
// Python: WorktreeManager._fire_rail("after_worktree_commit", session, commit_sha)
func (m *WorktreeManager) fireAfterCommit(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession, commitHash string) {
	for _, rail := range m.lifecycleRails {
		if err := rail.AfterWorktreeCommit(ctx, cbc, session, commitHash); err != nil {
			logger.Warn(logComponent).Err(err).Msg("fireAfterCommit hook failed")
		}
	}
}

// fireOnSync 调用 OnWorktreeSync hook。
// 返回所有 rail 过滤后的文件列表。
// Python: WorktreeManager._fire_rail("on_worktree_sync", session)
func (m *WorktreeManager) fireOnSync(ctx context.Context, cbc *interfaces.AgentCallbackContext, session *WorktreeSession, direction string, files []string) []string {
	filtered := files
	for _, rail := range m.lifecycleRails {
		filtered = rail.OnWorktreeSync(ctx, cbc, session, direction, filtered)
	}
	return filtered
}

// checkChanges 检查 worktree 路径的未提交变更。
// Python: WorktreeManager._check_changes(wt_path)
func (m *WorktreeManager) checkChanges(ctx context.Context, wtPath string) *WorktreeChangeSummary {
	changes, err := statusPorcelain(ctx, wtPath)
	if err != nil {
		return nil
	}
	return &WorktreeChangeSummary{ChangedFiles: len(changes), Commits: 0}
}

// removeWorktreeInternal 通过后端删除 worktree。
// Python: WorktreeManager._remove_worktree(wt_path, repo_root)
func (m *WorktreeManager) removeWorktreeInternal(ctx context.Context, wtPath, repoRoot string) bool {
	return m.backend.Remove(ctx, wtPath, repoRoot)
}

// postCreationSetup 新 worktree 的后置设置。
// Python: WorktreeManager._post_creation_setup(repo_root, worktree_path)
//
// 1. 符号链接配置的目录
// 2. 拷贝 gitignored include 文件
// 3. 配置 git hooks 路径
func (m *WorktreeManager) postCreationSetup(ctx context.Context, repoRoot, worktreePath string) {
	// 1. 符号链接目录
	dirs := m.config.SymlinkDirectories
	for _, d := range dirs {
		if strings.Contains(d, "..") || strings.HasPrefix(d, "/") {
			logger.Warn(logComponent).Str("dir", d).Msg("Skipping symlink: path traversal detected")
			continue
		}
		src := filepath.Join(repoRoot, d)
		dst := filepath.Join(worktreePath, d)
		if err := os.Symlink(src, dst); err != nil {
			if !os.IsExist(err) && !os.IsNotExist(err) {
				logger.Warn(logComponent).Str("dir", d).Err(err).Msg("Failed to create symlink")
			}
		}
	}

	// 2. 拷贝 gitignored include 文件
	if len(m.config.IncludePatterns) > 0 {
		if _, err := m.copyIncludeFiles(ctx, repoRoot, worktreePath, m.config.IncludePatterns); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to copy include files")
		}
	}

	// 3. 配置 hooks 路径
	m.configureHooksPath(ctx, repoRoot, worktreePath)
}

// copyIncludeFiles 拷贝 gitignored 文件到 worktree。
// Python: WorktreeManager._copy_include_files(repo_root, worktree_path, patterns)
func (m *WorktreeManager) copyIncludeFiles(ctx context.Context, repoRoot, worktreePath string, patterns []string) ([]string, error) {
	r := runGit(ctx, []string{"ls-files", "--others", "--ignored", "--exclude-standard", "--directory"}, repoRoot)
	if !r.OK() || r.Stdout == "" {
		return nil, nil
	}

	var copied []string
	for _, entry := range strings.Split(r.Stdout, "\n") {
		entry = strings.TrimSpace(entry)
		if entry == "" || strings.HasSuffix(entry, "/") {
			continue
		}
		// 简单模式匹配
		matched := false
		for _, pattern := range patterns {
			if fnmatch.Match(pattern, entry, 0) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		src := filepath.Join(repoRoot, entry)
		dst := filepath.Join(worktreePath, entry)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			logger.Warn(logComponent).Str("entry", entry).Err(err).Msg("Failed to create directory for include file")
			continue
		}
		if err := copyFile(src, dst); err != nil {
			logger.Warn(logComponent).Str("entry", entry).Err(err).Msg("Failed to copy include file")
			continue
		}
		copied = append(copied, entry)
	}
	return copied, nil
}

// configureHooksPath 配置 core.hooksPath。
// Python: WorktreeManager._configure_hooks_path(repo_root, worktree_path)
func (m *WorktreeManager) configureHooksPath(ctx context.Context, repoRoot, worktreePath string) {
	candidates := []string{
		filepath.Join(repoRoot, ".husky"),
		filepath.Join(repoRoot, ".git", "hooks"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			_ = runGit(ctx, []string{"config", "core.hooksPath", candidate}, worktreePath)
			logger.Debug(logComponent).Str("hooks_path", candidate).Msg("Worktree hooks path configured")
			return
		}
	}
}

// copyFile 拷贝单个文件，保留权限和修改时间。
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	dstF, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = dstF.Close() }()

	if _, err := io.Copy(dstF, f); err != nil {
		return err
	}

	// 恢复修改时间
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
