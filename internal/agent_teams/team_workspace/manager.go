package team_workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PublishEventFunc 事件发布回调函数类型。
// 对齐 Python: Callable[[str, BaseEventMessage], Awaitable[None]]
type PublishEventFunc func(eventType string, event any)

// managerConfig ManagerOption 配置
type managerConfig struct {
	publishEvent PublishEventFunc
}

// ManagerOption Manager 可选参数
type ManagerOption func(*managerConfig)

// TeamWorkspaceManager 团队工作空间管理器。
// 对齐 Python: TeamWorkspaceManager
//
// 处理团队共享工作空间的锁定、版本控制、同步和冲突检测。
// 文件 I/O 由 SysOperation 工具通过 .team/ 符号链接挂载处理，
// 本管理器仅管理元数据和版本控制。
//
// 两种操作模式：
//   - LOCAL：单 _team_workspace/ 目录、符号链接挂载、内存锁
//   - DISTRIBUTED：每节点克隆、git push/pull 同步、Leader 协调锁（Phase 3）
type TeamWorkspaceManager struct {
	config        TeamWorkspaceConfig
	workspacePath string
	teamName      string
	mode          WorkspaceMode
	locks         map[string]WorkspaceFileLock
	lockMu        sync.Mutex
	publishEvent  PublishEventFunc
}

// gitResult git 命令执行结果
type gitResult struct {
	OK     bool
	Stdout string
	Stderr string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// gitTimeoutSeconds git 命令超时秒数
	gitTimeoutSeconds = 30
	// defaultLockTimeoutSeconds 默认锁超时秒数
	defaultLockTimeoutSeconds = 300
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrDistributedNotImplemented 分布式模式尚未实现错误
var ErrDistributedNotImplemented = errors.New("分布式模式尚未实现")

// logComponent 日志组件
const logComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// WithPublishEvent 设置事件发布回调。
func WithPublishEvent(fn PublishEventFunc) ManagerOption {
	return func(c *managerConfig) {
		c.publishEvent = fn
	}
}

// NewTeamWorkspaceManager 创建 TeamWorkspaceManager。
// 对齐 Python: TeamWorkspaceManager.__init__
func NewTeamWorkspaceManager(config TeamWorkspaceConfig, workspacePath, teamName string, mode WorkspaceMode, opts ...ManagerOption) *TeamWorkspaceManager {
	cfg := managerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &TeamWorkspaceManager{
		config:        config,
		workspacePath: workspacePath,
		teamName:      teamName,
		mode:          mode,
		locks:         make(map[string]WorkspaceFileLock),
		publishEvent:  cfg.publishEvent,
	}
}

// Initialize 初始化工作空间目录和 git 仓库。
// 对齐 Python: TeamWorkspaceManager.initialize
//
// 当 config.VersionControl 为 false 时，仅创建工作空间和产物目录，
// 不初始化 git 仓库，工作空间作为普通共享目录使用。
//
// 当 config.VersionControl 为 true 时：
//   - LOCAL 模式：git init 创建新仓库并提交空初始提交
//   - DISTRIBUTED 模式 Leader：git init + 添加 remote origin（如提供 remoteURL）
//   - DISTRIBUTED 模式 Remote 节点：git clone 从 remoteURL 克隆
//
// 如 .git 目录已存在则跳过 git init。
func (m *TeamWorkspaceManager) Initialize(ctx context.Context, remoteURL ...string) error {
	if err := os.MkdirAll(m.workspacePath, 0o755); err != nil {
		return fmt.Errorf("创建工作空间目录失败: %w", err)
	}

	// 无论是否启用版本控制，均创建产物目录
	for _, d := range m.config.ArtifactDirs {
		if err := os.MkdirAll(filepath.Join(m.workspacePath, d), 0o755); err != nil {
			return fmt.Errorf("创建产物目录 %s 失败: %w", d, err)
		}
	}

	// 共享技能目录；每个成员的 SkillUseRail 通过 .team/{teamName} 挂载访问
	if err := os.MkdirAll(filepath.Join(m.workspacePath, "skills"), 0o755); err != nil {
		return fmt.Errorf("创建 skills 目录失败: %w", err)
	}

	if !m.config.VersionControl {
		logger.Info(logComponent).
			Str("workspace_path", m.workspacePath).
			Msg("工作空间初始化为普通共享目录（版本控制已禁用）")
		return nil
	}

	gitDir := filepath.Join(m.workspacePath, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		logger.Debug(logComponent).
			Str("workspace_path", m.workspacePath).
			Msg("工作空间已初始化")
		return nil
	}

	remote := ""
	if len(remoteURL) > 0 {
		remote = remoteURL[0]
	}

	// DISTRIBUTED 非 Leader 节点：克隆仓库
	if m.mode == WorkspaceModeDistributed && remote != "" {
		parent := filepath.Dir(m.workspacePath)
		name := filepath.Base(m.workspacePath)
		r := runGit(ctx, []string{"clone", remote, name}, parent)
		if !r.OK {
			return fmt.Errorf("克隆工作空间仓库失败: %s", r.Stderr)
		}
		logger.Info(logComponent).
			Str("remote_url", remote).
			Msg("已从远程克隆工作空间仓库")
	} else {
		// Leader 或 LOCAL：初始化新仓库
		r := runGit(ctx, []string{"init"}, m.workspacePath)
		if !r.OK {
			return fmt.Errorf("git init 失败: %s", r.Stderr)
		}
		r = runGit(ctx, []string{"commit", "--allow-empty", "-m", "Initialize team workspace"}, m.workspacePath)
		if !r.OK {
			return fmt.Errorf("初始提交失败: %s", r.Stderr)
		}
		logger.Info(logComponent).
			Str("workspace_path", m.workspacePath).
			Msg("已初始化工作空间 git 仓库")
	}

	// DISTRIBUTED Leader：设置 remote origin
	if m.mode == WorkspaceModeDistributed && remote != "" {
		existing := runGit(ctx, []string{"remote", "get-url", "origin"}, m.workspacePath)
		if !existing.OK {
			r := runGit(ctx, []string{"remote", "add", "origin", remote}, m.workspacePath)
			if !r.OK {
				return fmt.Errorf("添加 remote origin 失败: %s", r.Stderr)
			}
			logger.Info(logComponent).
				Str("remote_url", remote).
				Msg("已添加 remote origin")
		}
	}

	return nil
}

// MountIntoWorkspace 在 agent 工作区创建 .team/{teamName} 符号链接。
// 对齐 Python: TeamWorkspaceManager.mount_into_workspace
func (m *TeamWorkspaceManager) MountIntoWorkspace(workspaceRoot string) error {
	teamDir := filepath.Join(workspaceRoot, ".team")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		return fmt.Errorf("创建 .team 目录失败: %w", err)
	}
	linkPath := filepath.Join(teamDir, m.teamName)
	if m.prepareMountPath(linkPath) {
		if err := mountDirectory(m.workspacePath, linkPath); err != nil {
			return fmt.Errorf("挂载团队工作空间失败: %w", err)
		}
		logger.Debug(logComponent).
			Str("team_name", m.teamName).
			Str("link_path", linkPath).
			Msg("已挂载团队工作空间")
	}
	return nil
}

// MountWorktree 在团队工作空间暴露 worktree 符号链接。
// 对齐 Python: TeamWorkspaceManager.mount_worktree
func (m *TeamWorkspaceManager) MountWorktree(slug, worktreePath string) error {
	wtDir := filepath.Join(m.workspacePath, ".worktree")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		return fmt.Errorf("创建 .worktree 目录失败: %w", err)
	}
	linkPath := filepath.Join(wtDir, slug)

	// 检查路径是否已存在
	info, err := os.Lstat(linkPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(linkPath)
		} else {
			logger.Warn(logComponent).
				Str("link_path", linkPath).
				Msg("Worktree 挂载路径存在且不是符号链接，跳过")
			return nil
		}
	}

	if err := mountDirectory(worktreePath, linkPath); err != nil {
		return fmt.Errorf("挂载 worktree 失败: %w", err)
	}
	logger.Debug(logComponent).
		Str("slug", slug).
		Str("link_path", linkPath).
		Msg("已挂载 worktree")
	return nil
}

// UnmountWorktree 移除 worktree 符号链接。
// 对齐 Python: TeamWorkspaceManager.unmount_worktree
func (m *TeamWorkspaceManager) UnmountWorktree(slug string) error {
	linkPath := filepath.Join(m.workspacePath, ".worktree", slug)
	info, err := os.Lstat(linkPath)
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(linkPath); err != nil {
			return fmt.Errorf("移除 worktree 符号链接失败: %w", err)
		}
		logger.Debug(logComponent).
			Str("slug", slug).
			Str("link_path", linkPath).
			Msg("已卸载 worktree")
	}
	return nil
}

// MountIntoWorktree 在 worktree 内创建 .team 符号链接并更新 .gitignore。
// 对齐 Python: TeamWorkspaceManager.mount_into_worktree
func (m *TeamWorkspaceManager) MountIntoWorktree(worktreePath string) error {
	linkPath := filepath.Join(worktreePath, ".team")
	if m.prepareMountPath(linkPath) {
		if err := mountDirectory(m.workspacePath, linkPath); err != nil {
			return fmt.Errorf("挂载 .team 到 worktree 失败: %w", err)
		}
	}

	// 更新 .gitignore
	gitignorePath := filepath.Join(worktreePath, ".gitignore")
	entriesToAdd := []string{".agent/", ".team/"}
	existing := ""
	data, err := os.ReadFile(gitignorePath)
	if err == nil {
		existing = string(data)
	}

	var additions []string
	for _, e := range entriesToAdd {
		if !strings.Contains(existing, e) {
			additions = append(additions, e)
		}
	}

	if len(additions) > 0 {
		f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("打开 .gitignore 失败: %w", err)
		}
		defer f.Close()

		if existing != "" && !strings.HasSuffix(existing, "\n") {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("写入 .gitignore 失败: %w", err)
			}
		}
		if _, err := f.WriteString("# Agent Teams managed\n"); err != nil {
			return fmt.Errorf("写入 .gitignore 失败: %w", err)
		}
		for _, entry := range additions {
			if _, err := f.WriteString(entry + "\n"); err != nil {
				return fmt.Errorf("写入 .gitignore 失败: %w", err)
			}
		}
	}

	return nil
}

// AutoCommit 自动提交文件变更。
// 对齐 Python: TeamWorkspaceManager.auto_commit
//
// LOCAL 模式下：git add + diff --cached + commit。
// DISTRIBUTED 模式下额外执行 push，失败时 pull 后重试一次。
func (m *TeamWorkspaceManager) AutoCommit(ctx context.Context, relativePath, memberName string) (string, error) {
	if !m.config.VersionControl {
		return "", nil
	}

	runGit(ctx, []string{"add", relativePath}, m.workspacePath)

	status := runGit(ctx, []string{"diff", "--cached", "--quiet"}, m.workspacePath)
	if status.OK {
		return "", nil // 无暂存变更
	}

	msg := fmt.Sprintf("[%s] Update %s", memberName, relativePath)
	result := runGit(ctx, []string{"commit", "-m", msg}, m.workspacePath)
	if !result.OK {
		return "", nil
	}

	sha, err := revParse(ctx, "HEAD", m.workspacePath)
	if err != nil {
		return "", err
	}

	if m.mode == WorkspaceModeDistributed {
		pushed, _ := m.Push(ctx)
		if !pushed {
			_, _ = m.Pull(ctx)
			retry, _ := m.Push(ctx)
			if !retry {
				logger.Error(logComponent).
					Str("relative_path", relativePath).
					Msg("工作空间推送重试后仍失败")
			}
		}
	}

	return sha, nil
}

// GetHistory 获取文件版本历史。
// 对齐 Python: TeamWorkspaceManager.get_history
func (m *TeamWorkspaceManager) GetHistory(ctx context.Context, relativePath string, limit int) ([]HistoryEntry, error) {
	if !m.config.VersionControl {
		return nil, nil
	}

	if m.mode == WorkspaceModeDistributed {
		_, _ = m.Pull(ctx)
	}

	// limit <= 0 使用默认值 10（对齐 Python: limit: int = 10）
	if limit <= 0 {
		limit = 10
	}

	args := []string{
		"log",
		fmt.Sprintf("--max-count=%d", limit),
		"--format=%H|%an|%ai|%s",
		"--",
		relativePath,
	}
	r := runGit(ctx, args, m.workspacePath)
	if !r.OK || r.Stdout == "" {
		return nil, nil
	}

	var history []HistoryEntry
	for _, line := range strings.Split(r.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) == 4 {
			history = append(history, HistoryEntry{
				Commit:  parts[0],
				Author:  parts[1],
				Date:    parts[2],
				Message: parts[3],
			})
		}
	}
	return history, nil
}

// GetLock 获取文件锁状态。
// 对齐 Python: TeamWorkspaceManager.get_lock
//
// 无网络请求，仅返回缓存锁状态。
func (m *TeamWorkspaceManager) GetLock(filePath string) *WorkspaceFileLock {
	m.lockMu.Lock()
	defer m.lockMu.Unlock()

	lock, ok := m.locks[filePath]
	if !ok {
		return nil
	}
	if lock.IsExpired() {
		delete(m.locks, filePath)
		return nil
	}
	return &lock
}

// AcquireLock 获取文件锁。
// 对齐 Python: TeamWorkspaceManager.acquire_lock
//
// LOCAL 模式或 Leader 节点：内存锁 + sync.Mutex 保护，可重入，过期锁可回收。
// DISTRIBUTED 非 Leader：委托 RemoteAcquireLock（Phase 3）。
func (m *TeamWorkspaceManager) AcquireLock(ctx context.Context, filePath, memberName, displayName string, timeoutSeconds ...int) (bool, error) {
	timeout := defaultLockTimeoutSeconds
	if len(timeoutSeconds) > 0 {
		timeout = timeoutSeconds[0]
	}

	if m.mode == WorkspaceModeDistributed {
		return m.RemoteAcquireLock(ctx, filePath, memberName, displayName, timeout)
	}

	// LOCAL 模式
	m.lockMu.Lock()
	defer m.lockMu.Unlock()

	existing, ok := m.locks[filePath]
	if ok && !existing.IsExpired() && existing.HolderID != memberName {
		return false, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	m.locks[filePath] = WorkspaceFileLock{
		FilePath:       filePath,
		HolderID:       memberName,
		HolderName:     displayName,
		AcquiredAt:     now,
		TimeoutSeconds: timeout,
	}
	return true, nil
}

// ReleaseLock 释放文件锁。
// 对齐 Python: TeamWorkspaceManager.release_lock
func (m *TeamWorkspaceManager) ReleaseLock(ctx context.Context, filePath, memberName string) (bool, error) {
	if m.mode == WorkspaceModeDistributed {
		return m.RemoteReleaseLock(ctx, filePath, memberName)
	}

	// LOCAL 模式
	m.lockMu.Lock()
	defer m.lockMu.Unlock()

	existing, ok := m.locks[filePath]
	if !ok || existing.HolderID != memberName {
		return false, nil
	}
	delete(m.locks, filePath)
	return true, nil
}

// ListLocks 列出所有活跃锁。
// 对齐 Python: TeamWorkspaceManager.list_locks
func (m *TeamWorkspaceManager) ListLocks() []WorkspaceFileLock {
	m.lockMu.Lock()
	defer m.lockMu.Unlock()

	// 清除过期锁
	var expiredKeys []string
	for k, v := range m.locks {
		if v.IsExpired() {
			expiredKeys = append(expiredKeys, k)
		}
	}
	for _, k := range expiredKeys {
		delete(m.locks, k)
	}

	result := make([]WorkspaceFileLock, 0, len(m.locks))
	for _, v := range m.locks {
		result = append(result, v)
	}
	return result
}

// Pull 拉取远程变更（LOCAL 模式下 no-op）。
// 对齐 Python: TeamWorkspaceManager.pull
func (m *TeamWorkspaceManager) Pull(ctx context.Context) (bool, error) {
	if !m.config.VersionControl {
		return false, nil
	}
	if m.mode != WorkspaceModeDistributed {
		return false, nil
	}

	r := runGit(ctx, []string{"pull", "--rebase", "--autostash", "origin", "main"}, m.workspacePath)
	if !r.OK {
		return false, nil
	}
	return !strings.Contains(r.Stdout, "Already up to date"), nil
}

// Push 推送本地提交（LOCAL 模式下 no-op）。
// 对齐 Python: TeamWorkspaceManager.push
func (m *TeamWorkspaceManager) Push(ctx context.Context) (bool, error) {
	if !m.config.VersionControl {
		return true, nil
	}
	if m.mode != WorkspaceModeDistributed {
		return true, nil
	}

	r := runGit(ctx, []string{"push", "origin", "main"}, m.workspacePath)
	if !r.OK {
		logger.Warn(logComponent).
			Str("stderr", r.Stderr).
			Msg("工作空间推送失败，将在下次写入时重试")
		return false, nil
	}
	return true, nil
}

// RemoteAcquireLock 分布式远程获取锁（占位）。
// 对齐 Python: TeamWorkspaceManager._remote_acquire_lock
func (m *TeamWorkspaceManager) RemoteAcquireLock(ctx context.Context, filePath, memberName, displayName string, timeoutSeconds ...int) (bool, error) {
	return false, ErrDistributedNotImplemented
}

// RemoteReleaseLock 分布式远程释放锁（占位）。
// 对齐 Python: TeamWorkspaceManager._remote_release_lock
func (m *TeamWorkspaceManager) RemoteReleaseLock(ctx context.Context, filePath, memberName string) (bool, error) {
	return false, ErrDistributedNotImplemented
}

// HandleLockRequest 处理锁请求（占位，仅 Leader 调用）。
// 对齐 Python: TeamWorkspaceManager.handle_lock_request
//
// 参数 request 须为 schema.WorkspaceLockRequestEvent 类型，
// 返回 *schema.WorkspaceLockResponseEvent。因避免循环依赖，
// 此处使用 any 类型占位，待 Phase 3 实现时由上层强转。
func (m *TeamWorkspaceManager) HandleLockRequest(request any) (any, error) {
	return nil, ErrDistributedNotImplemented
}

// HandleLockResponse 处理锁响应（占位，仅 Remote 调用）。
// 对齐 Python: TeamWorkspaceManager.handle_lock_response
//
// 参数 response 须为 schema.WorkspaceLockResponseEvent 类型。
func (m *TeamWorkspaceManager) HandleLockResponse(response any) error {
	return ErrDistributedNotImplemented
}

// Config 返回配置
func (m *TeamWorkspaceManager) Config() TeamWorkspaceConfig { return m.config }

// WorkspacePath 返回工作空间绝对路径
func (m *TeamWorkspaceManager) WorkspacePath() string { return m.workspacePath }

// TeamName 返回团队名
func (m *TeamWorkspaceManager) TeamName() string { return m.teamName }

// Mode 返回工作模式
func (m *TeamWorkspaceManager) Mode() WorkspaceMode { return m.mode }

// ──────────────────────────── 非导出函数 ────────────────────────────

// runGit 执行 git 命令。
// 对齐 Python: _run_git
func runGit(ctx context.Context, args []string, cwd string) gitResult {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitTimeoutSeconds*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "git", args...)
	cmd.Dir = cwd

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return gitResult{
		OK:     err == nil,
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
}

// revParse 执行 git rev-parse。
// 对齐 Python: rev_parse
func revParse(ctx context.Context, ref, cwd string) (string, error) {
	r := runGit(ctx, []string{"rev-parse", ref}, cwd)
	if !r.OK {
		return "", fmt.Errorf("git rev-parse %s 失败: %s", ref, r.Stderr)
	}
	return r.Stdout, nil
}

// mountDirectory 创建目录符号链接。
// 对齐 Python: TeamWorkspaceManager._mount_directory
//
// TODO: Windows junction fallback
func mountDirectory(targetPath, linkPath string) error {
	return os.Symlink(targetPath, linkPath)
}

// prepareMountPath 准备挂载路径。
// 对齐 Python: TeamWorkspaceManager._prepare_mount_path
//
// 返回 true 表示需要在 linkPath 创建挂载，false 表示已正确挂载。
func (m *TeamWorkspaceManager) prepareMountPath(linkPath string) bool {
	// 路径不存在（且不是悬空符号链接）→ 需要挂载
	info, err := os.Lstat(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true
		}
		// 其他错误（如权限），也尝试挂载
		return true
	}

	// 已正确挂载到工作空间 → 无需操作
	if m.isMountedToWorkspace(linkPath) {
		return false
	}

	// 存在但指向其他位置：合并内容、备份旧路径、准备挂载
	m.mergeExistingMountContents(linkPath, info)
	backupPath := backupExistingMountPath(linkPath)
	logger.Warn(logComponent).
		Str("link_path", linkPath).
		Str("backup_path", backupPath).
		Msg("已替换旧的团队工作空间挂载路径，之前的内容已移至备份")
	return true
}

// mergeExistingMountContents 合并旧挂载目录内容。
// 对齐 Python: TeamWorkspaceManager._merge_existing_mount_contents
//
// 当 .team/<team_name> 是一个真实目录（而非符号链接）时，
// 将其内容合并到工作空间中，避免用户产物丢失。
// 已有的工作空间文件优先，不覆盖。
func (m *TeamWorkspaceManager) mergeExistingMountContents(linkPath string, info os.FileInfo) {
	// 仅处理真实目录，跳过符号链接
	if info == nil || info.IsDir() == false || info.Mode()&os.ModeSymlink != 0 {
		return
	}

	filepath.WalkDir(linkPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relRoot, relErr := filepath.Rel(linkPath, path)
		if relErr != nil {
			return nil
		}

		dstRoot := m.workspacePath
		if relRoot != "." {
			dstRoot = filepath.Join(m.workspacePath, relRoot)
		}

		if d.IsDir() {
			_ = os.MkdirAll(dstRoot, 0o755)
			return nil
		}

		// 仅复制工作空间中不存在的文件
		if _, statErr := os.Stat(dstRoot); os.IsNotExist(statErr) {
			copyFile(path, dstRoot)
		}
		return nil
	})
}

// backupExistingMountPath 备份旧挂载路径。
// 对齐 Python: TeamWorkspaceManager._backup_existing_mount_path
func backupExistingMountPath(linkPath string) string {
	stamp := time.Now().UTC().Format("20060102150405")
	backupPath := fmt.Sprintf("%s.stale-%s", linkPath, stamp)
	counter := 1
	for {
		if _, err := os.Lstat(backupPath); os.IsNotExist(err) {
			break
		}
		counter++
		backupPath = fmt.Sprintf("%s.stale-%s-%d", linkPath, stamp, counter)
	}
	_ = os.Rename(linkPath, backupPath)
	return backupPath
}

// isMountedToWorkspace 检查链接是否已指向工作空间。
// 对齐 Python: TeamWorkspaceManager._is_mounted_to_workspace
func (m *TeamWorkspaceManager) isMountedToWorkspace(linkPath string) bool {
	linkInfo, err := os.Stat(linkPath)
	if err != nil {
		return false
	}
	wsInfo, err := os.Stat(m.workspacePath)
	if err != nil {
		return false
	}
	return os.SameFile(linkInfo, wsInfo)
}

// maybePull 节流拉取（LOCAL 模式下 no-op）。
// 对齐 Python: TeamWorkspaceRail._maybe_pull
func (m *TeamWorkspaceManager) maybePull() {} // LOCAL 模式下无需操作

// resolveWorkspaceRelative 从 .team/ 前缀路径提取工作空间相对路径。
// 对齐 Python: TeamWorkspaceRail._resolve_workspace_relative
//
// 处理两种布局：
//   - Hub:   .team/{team_name}/artifacts/report.md → artifacts/report.md
//   - Legacy: .team/artifacts/report.md             → artifacts/report.md
func resolveWorkspaceRelative(path, teamName string) string {
	const prefix = ".team/"
	afterPrefix := path[len(prefix):]
	teamNamePrefix := teamName + "/"
	if strings.HasPrefix(afterPrefix, teamNamePrefix) {
		return afterPrefix[len(teamNamePrefix):]
	}
	return afterPrefix
}

// copyFile 复制单个文件，保留权限。
func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}

	// 保留源文件权限
	si, err := s.Stat()
	if err != nil {
		return err
	}
	return os.Chmod(dst, si.Mode())
}
