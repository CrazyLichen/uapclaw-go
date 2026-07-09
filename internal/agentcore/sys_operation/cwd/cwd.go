package cwd

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CwdState 每个-Agent 的可变 CWD 状态容器。
// 对齐 Python: CwdState dataclass (cwd.py:48-59)。
//
// 通过 context.Value 传播 *CwdState 指针：
//   - 同一 Agent 内的 goroutine 共享同一 CwdState 引用，SetCwd 后立即可见
//   - 子 Agent 调用 InitCwd 创建新 CwdState + WithCwdState 派生新 ctx，父不受影响
//
// 三层 CWD 模型：
//
//	Layer 1 — projectRoot:  项目身份锚点（设置一次，沙箱边界依据）
//	Layer 2 — originalCwd:  会话起始点（worktree 退出时的恢复目标）
//	Layer 3 — cwd:          当前工作目录（高频更新，所有工具的路径锚点）
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
// Python 不需要锁因为 asyncio 是单线程协程。
type CwdState struct {
	mu            sync.RWMutex
	cwd           string
	originalCwd   string
	projectRoot   string
	workspace     string
	teamWorkspace string
}

// cwdStateKeyType CwdState 的 context key 类型。
type cwdStateKeyType struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// cwdOption InitCwd 的选项函数。
type cwdOption func(s *CwdState)

// ──────────────────────────── 全局变量 ────────────────────────────

// cwdStateKey CwdState 的 context key。
var cwdStateKey cwdStateKeyType

// ──────────────────────────── 导出函数 ────────────────────────────

// InitCwd 初始化所有 CWD 层，创建新的 CwdState 实例。
// 对齐 Python: init_cwd(cwd, project_root, workspace, team_workspace) (cwd.py:167-198)
//
// 在 DeepAgent.ensureInitialized 中调用。
// 创建新 CwdState + WithCwdState 派生新 ctx，实现 inter-Agent 隔离。
func InitCwd(cwd string, opts ...cwdOption) *CwdState {
	resolved := resolve(cwd)
	s := &CwdState{
		cwd:         resolved,
		originalCwd: resolved,
		projectRoot: resolved,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithProjectRoot 设置项目根目录选项。
// 对齐 Python: init_cwd(cwd, project_root=...) (cwd.py:169)
func WithProjectRoot(root string) cwdOption {
	return func(s *CwdState) {
		s.projectRoot = resolve(root)
	}
}

// WithWorkspace 设置 agent workspace 选项。
// 对齐 Python: init_cwd(cwd, workspace=...) (cwd.py:171)
func WithWorkspace(path string) cwdOption {
	return func(s *CwdState) {
		s.workspace = resolve(path)
	}
}

// WithTeamWorkspace 设置团队 workspace 选项。
// 对齐 Python: init_cwd(cwd, team_workspace=...) (cwd.py:172)
func WithTeamWorkspace(path string) cwdOption {
	return func(s *CwdState) {
		s.teamWorkspace = resolve(path)
	}
}

// WithCwdState 将 CwdState 注入 context。
// 对齐 Python: _cwd_state.set(state) (cwd.py:198)
func WithCwdState(ctx context.Context, state *CwdState) context.Context {
	return context.WithValue(ctx, cwdStateKey, state)
}

// CwdStateFromCtx 从 context 中获取 CwdState。
// 返回 nil 表示当前 context 未绑定 CwdState。
func CwdStateFromCtx(ctx context.Context) *CwdState {
	if s, ok := ctx.Value(cwdStateKey).(*CwdState); ok {
		return s
	}
	return nil
}

// GetCwd 从 context 中获取当前工作目录。
// 对齐 Python: get_cwd() (cwd.py:80-87)
// 读取优先级：cwd -> originalCwd -> os.Getwd()
func GetCwd(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetCwd()
	}
	wd, _ := os.Getwd()
	return wd
}

// GetOriginalCwd 从 context 中获取会话起始点。
// 对齐 Python: get_original_cwd() (cwd.py:102-105)
func GetOriginalCwd(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetOriginalCwd()
	}
	wd, _ := os.Getwd()
	return wd
}

// GetProjectRoot 从 context 中获取项目根目录。
// 对齐 Python: get_project_root() (cwd.py:115-121)
// 读取优先级：projectRoot -> originalCwd -> os.Getwd()
func GetProjectRoot(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetProjectRoot()
	}
	wd, _ := os.Getwd()
	return wd
}

// GetWorkspace 从 context 中获取 agent workspace。
// 对齐 Python: get_workspace() (cwd.py:131-139)
// 返回空字符串表示未设置（Python 返回 None）。
func GetWorkspace(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetWorkspace()
	}
	return ""
}

// GetTeamWorkspace 从 context 中获取团队 workspace。
// 对齐 Python: get_team_workspace() (cwd.py:149-157)
// 返回空字符串表示未设置。
func GetTeamWorkspace(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetTeamWorkspace()
	}
	return ""
}

// ResolveCwd 解析工作目录。
// 对齐 Python: ShellOperation._resolve_cwd(cwd) (shell_operation.py:864-874)
//
// 解析优先级：
//  1. explicitCwd 非空且为绝对路径 → 直接使用（resolve）
//  2. explicitCwd 非空且为相对路径 → 基于 GetCwd(ctx) 解析
//  3. explicitCwd 为空 → 使用 GetCwd(ctx)
func ResolveCwd(ctx context.Context, explicitCwd string) string {
	if explicitCwd == "" {
		return GetCwd(ctx)
	}
	target := filepath.Clean(explicitCwd)
	if !filepath.IsAbs(target) {
		target = filepath.Join(GetCwd(ctx), target)
	}
	return resolve(target)
}

// ResolvePath 基于当前 CWD 解析文件路径。
// 对齐 Python: FsOperation._resolve_path(path) (fs_operation.py:1098-1133)
//
// 对相对路径：基于 GetCwd(ctx) 解析
// 对绝对路径：直接使用
func ResolvePath(ctx context.Context, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(GetCwd(ctx), path))
}

// GetCwd 获取当前工作目录。
// 对齐 Python: get_cwd() (cwd.py:80-87)
// 读取优先级：cwd -> originalCwd -> os.Getwd()
func (s *CwdState) GetCwd() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cwd != "" {
		return s.cwd
	}
	if s.originalCwd != "" {
		return s.originalCwd
	}
	wd, _ := os.Getwd()
	return wd
}

// GetOriginalCwd 获取会话起始点。
// 对齐 Python: get_original_cwd() (cwd.py:102-105)
func (s *CwdState) GetOriginalCwd() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.originalCwd != "" {
		return s.originalCwd
	}
	wd, _ := os.Getwd()
	return wd
}

// GetProjectRoot 获取项目根目录。
// 对齐 Python: get_project_root() (cwd.py:115-121)
// 读取优先级：projectRoot -> originalCwd -> os.Getwd()
// 注意：内联读取 originalCwd 而非调用 GetOriginalCwd()，避免嵌套加锁风险。
func (s *CwdState) GetProjectRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.projectRoot != "" {
		return s.projectRoot
	}
	if s.originalCwd != "" {
		return s.originalCwd
	}
	wd, _ := os.Getwd()
	return wd
}

// GetWorkspace 获取 agent workspace。
// 对齐 Python: get_workspace() (cwd.py:131-139)
func (s *CwdState) GetWorkspace() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspace
}

// GetTeamWorkspace 获取团队 workspace。
// 对齐 Python: get_team_workspace() (cwd.py:149-157)
func (s *CwdState) GetTeamWorkspace() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.teamWorkspace
}

// SetCwd 更新当前工作目录。
// 对齐 Python: set_cwd(cwd) (cwd.py:90-97)
// 调用方：EnterWorktreeTool（切换到 worktree）、ExitWorktreeTool（恢复原始 CWD）
func (s *CwdState) SetCwd(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = resolve(cwd)
}

// SetOriginalCwd 更新会话起始点。
// 对齐 Python: set_original_cwd(cwd) (cwd.py:108-110)
// 调用方：EnterWorktreeTool（同步切到 worktree）、ExitWorktreeTool（恢复）
func (s *CwdState) SetOriginalCwd(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.originalCwd = resolve(cwd)
}

// SetProjectRoot 设置项目根目录。
// 对齐 Python: set_project_root(root) (cwd.py:124-126)
// 应在 agent 启动时调用一次。
func (s *CwdState) SetProjectRoot(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectRoot = resolve(root)
}

// SetWorkspace 设置 agent workspace。
// 对齐 Python: set_workspace(path) (cwd.py:142-144)
func (s *CwdState) SetWorkspace(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspace = resolve(path)
}

// SetTeamWorkspace 设置团队 workspace。
// 对齐 Python: set_team_workspace(path) (cwd.py:160-162)
func (s *CwdState) SetTeamWorkspace(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teamWorkspace = resolve(path)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolve 解析路径为绝对路径。
// 对齐 Python: _resolve(path) (cwd.py:65-66)
func resolve(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(abs)
}
