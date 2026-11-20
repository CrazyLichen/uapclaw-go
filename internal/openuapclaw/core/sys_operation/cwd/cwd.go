package cwd

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CwdState 每个-Agent 的可变 CWD 状态容器。
// Python: CwdState dataclass (cwd.py:48-59)。
//
// 与 Python 一致，本类型只是纯数据容器，不承载读取逻辑；
// 三级回退（cwd -> originalCwd -> os.getcwd()）全部在模块级 GetCwd 里。
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
// Python: init_cwd(cwd, project_root, workspace, team_workspace) (cwd.py:167-198)
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
// Python: init_cwd(cwd, project_root=...) (cwd.py:169)
func WithProjectRoot(root string) cwdOption {
	return func(s *CwdState) {
		s.projectRoot = resolve(root)
	}
}

// WithWorkspace 设置 agent workspace 选项。
// Python: init_cwd(cwd, workspace=...) (cwd.py:171)
func WithWorkspace(path string) cwdOption {
	return func(s *CwdState) {
		s.workspace = resolve(path)
	}
}

// WithTeamWorkspace 设置团队 workspace 选项。
// Python: init_cwd(cwd, team_workspace=...) (cwd.py:172)
func WithTeamWorkspace(path string) cwdOption {
	return func(s *CwdState) {
		s.teamWorkspace = resolve(path)
	}
}

// WithCwdState 将 CwdState 注入 context。
// Python: _cwd_state.set(state) (cwd.py:198)
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
//
// Python: get_cwd() (cwd.py:80-87)
//
//	s = _state()
//	return s.cwd or s.original_cwd or os.getcwd()
//
// 三级回退完全内联在本函数（与 Python 结构一致）：
//  1. CwdState.cwd
//  2. CwdState.originalCwd
//  3. os.Getwd() —— 失败时返回 error（Python 抛 OSError）
//
// 返回 error 而非吞掉：Python 的异常会自然向上传播，Go 用显式 error 表达。
// 调用方须处理或传播；command_runtime.go 用 if err != nil 对应 Python 的
// try/except Exception。
func GetCwd(ctx context.Context) (string, error) {
	if s := CwdStateFromCtx(ctx); s != nil {
		cwd, original := s.snapshotCwd()
		if cwd != "" {
			return cwd, nil
		}
		if original != "" {
			return original, nil
		}
	}
	return os.Getwd()
}

// GetOriginalCwd 从 context 中获取会话起始点。
// Python: get_original_cwd() (cwd.py:102-105)
//
// 注：本次 9.90d-1 改动只针对 GetCwd/GetProjectRoot（command_runtime 的
// 依赖面）。GetOriginalCwd 保持历史签名，其 os.Getwd() 回退仍吞 error，
// 待后续章节需要时统一处理。
func GetOriginalCwd(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetOriginalCwd()
	}
	wd, _ := os.Getwd()
	return wd
}

// GetProjectRoot 从 context 中获取项目根目录。
//
// Python: get_project_root() (cwd.py:115-121)
//
//	s = _state()
//	return s.project_root or get_original_cwd()
//
// 其中 get_original_cwd() = s.original_cwd or os.getcwd()。
// 因此三级回退：projectRoot -> originalCwd -> os.Getwd()，
// 最后一级失败时返回 error（Python 抛 OSError）。
func GetProjectRoot(ctx context.Context) (string, error) {
	if s := CwdStateFromCtx(ctx); s != nil {
		root, original := s.snapshotProjectRoot()
		if root != "" {
			return root, nil
		}
		if original != "" {
			return original, nil
		}
	}
	return os.Getwd()
}

// GetWorkspace 从 context 中获取 agent workspace。
// Python: get_workspace() (cwd.py:131-139)
// 返回空字符串表示未设置（Python 返回 None）。
func GetWorkspace(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetWorkspace()
	}
	return ""
}

// GetTeamWorkspace 从 context 中获取团队 workspace。
// Python: get_team_workspace() (cwd.py:149-157)
// 返回空字符串表示未设置。
func GetTeamWorkspace(ctx context.Context) string {
	if s := CwdStateFromCtx(ctx); s != nil {
		return s.GetTeamWorkspace()
	}
	return ""
}

// ResolveCwd 解析工作目录。
// Python: ShellOperation._resolve_cwd(cwd) (shell_operation.py:864-874)
//
// 解析优先级：
//  1. explicitCwd 非空且为绝对路径 → 直接使用（resolve）
//  2. explicitCwd 非空且为相对路径 → 基于 GetCwd(ctx) 解析
//  3. explicitCwd 为空 → 使用 GetCwd(ctx)
//
// 返回 error 以传播 GetCwd 的 os.Getwd() 异常。
func ResolveCwd(ctx context.Context, explicitCwd string) (string, error) {
	if explicitCwd == "" {
		got, err := GetCwd(ctx)
		if err != nil {
			return "", err
		}
		return got, nil
	}
	target := filepath.Clean(explicitCwd)
	if !filepath.IsAbs(target) {
		base, err := GetCwd(ctx)
		if err != nil {
			return "", err
		}
		target = filepath.Join(base, target)
	}
	return resolve(target), nil
}

// ResolvePath 基于当前 CWD 解析文件路径。
// Python: FsOperation._resolve_path(path) (fs_operation.py:1098-1133)
//
// 对相对路径：基于 GetCwd(ctx) 解析
// 对绝对路径：直接使用
//
// 返回 error 以传播 GetCwd 的 os.Getwd() 异常。
func ResolvePath(ctx context.Context, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	base, err := GetCwd(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(base, path)), nil
}

// GetOriginalCwd 获取会话起始点。
// Python: get_original_cwd() (cwd.py:102-105)
func (s *CwdState) GetOriginalCwd() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.originalCwd != "" {
		return s.originalCwd
	}
	wd, _ := os.Getwd()
	return wd
}

// GetWorkspace 获取 agent workspace。
// Python: get_workspace() (cwd.py:131-139)
func (s *CwdState) GetWorkspace() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspace
}

// GetTeamWorkspace 获取团队 workspace。
// Python: get_team_workspace() (cwd.py:149-157)
func (s *CwdState) GetTeamWorkspace() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.teamWorkspace
}

// SetCwd 更新当前工作目录。
// Python: set_cwd(cwd) (cwd.py:90-97)
// 调用方：EnterWorktreeTool（切换到 worktree）、ExitWorktreeTool（恢复原始 CWD）
func (s *CwdState) SetCwd(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = resolve(cwd)
}

// SetOriginalCwd 更新会话起始点。
// Python: set_original_cwd(cwd) (cwd.py:108-110)
// 调用方：EnterWorktreeTool（同步切到 worktree）、ExitWorktreeTool（恢复）
func (s *CwdState) SetOriginalCwd(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.originalCwd = resolve(cwd)
}

// SetProjectRoot 设置项目根目录。
// Python: set_project_root(root) (cwd.py:124-126)
// 应在 agent 启动时调用一次。
func (s *CwdState) SetProjectRoot(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectRoot = resolve(root)
}

// SetWorkspace 设置 agent workspace。
// Python: set_workspace(path) (cwd.py:142-144)
func (s *CwdState) SetWorkspace(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspace = resolve(path)
}

// SetTeamWorkspace 设置团队 workspace。
// Python: set_team_workspace(path) (cwd.py:160-162)
func (s *CwdState) SetTeamWorkspace(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teamWorkspace = resolve(path)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// snapshotCwd 读取 cwd 与 originalCwd 两个字段（加锁快照）。
// 只做字段读取，不做 os.Getwd() 回退——回退是模块级 GetCwd 的职责。
func (s *CwdState) snapshotCwd() (cwd, originalCwd string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cwd, s.originalCwd
}

// snapshotProjectRoot 读取 projectRoot 与 originalCwd 两个字段（加锁快照）。
// 内联读取 originalCwd 而非调用 GetOriginalCwd()，避免嵌套加锁风险。
func (s *CwdState) snapshotProjectRoot() (projectRoot, originalCwd string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projectRoot, s.originalCwd
}

// resolve 解析路径为绝对路径。
// Python: _resolve(path) (cwd.py:65-66)
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
