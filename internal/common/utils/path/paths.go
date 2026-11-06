package path

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedPaths 缓存已解析的 ConfigDir 和 AgentWorkspaceDir。
//
// Python: jiuwenswarm/common/utils.py 中的 _config_dir / _workspace_dir 全局变量。
// 未初始化时（~/.uapclaw/config/ 不存在）回退到 resources 目录，
// 与 Python _resolve_paths() 行为对齐。
type ResolvedPaths struct {
	// ConfigDir 配置目录
	ConfigDir string
	// WorkspaceDir Agent 工作空间目录
	WorkspaceDir string
	// Fallback 为 true 表示 ConfigDir 来自 resources 回退而非用户目录
	Fallback bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EnvHome 自定义用户主目录环境变量。
	// Python: JIUWENSWARM_HOME
	EnvHome = "UAPCLAW_HOME"

	// EnvDataDir 自定义数据根目录环境变量。
	// Python: JIUWENSWARM_DATA_DIR
	EnvDataDir = "UAPCLAW_DATA_DIR"

	// EnvResourcesDir 自定义资源目录环境变量。
	// Go 版特有，Python 通过 __file__ 反推包位置，Go 用环境变量覆盖。
	EnvResourcesDir = "UAPCLAW_RESOURCES_DIR"

	// DefaultDir 默认工作区目录名。
	// Python: ".jiuwenswarm"
	DefaultDir = ".uapclaw"

	// DefaultInstancesDir 默认命名实例根目录名。
	// Python: ".jiuwenswarm-instances"
	DefaultInstancesDir = ".uapclaw-instances"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// cacheMu 保护下面三个缓存槽的「指针」与「值」。
	//
	// 为什么需要：SetUserHome/ResetCache 会在测试 cleanup 里重置缓存，
	// 而测试进程仍可能有后台 goroutine 在读路径（曾触发 DATA RACE）。
	//
	// 设计要点：Once 用指针 + 原子置换，绝不在持有 cacheMu 时调 one.Do
	// （重置会换掉指针，而 one.Do 的执行体会再取 cacheMu 写值 —— 自锁）。
	cacheMu sync.Mutex

	// userHomeSlot 主目录缓存槽（指针由 cacheMu 保护：置空/换取都是写操作，
	// 用 atomic.Pointer 会让读取看到瞬时 nil）
	userHomeSlot = &pathCacheSlot{}
	// workspaceSlot 数据根目录缓存槽（指针由 cacheMu 保护，理由同 userHomeSlot）
	workspaceSlot = &pathCacheSlot{}
	// resolvedSlot 解析结果缓存槽（指针由 cacheMu 保护，理由同 userHomeSlot）
	resolvedSlot = &pathCacheSlot{}
)

// pathCacheSlot 一个可整体置换的缓存槽：Once 保证只算一次，value 存结果。
//
// 注意：once 必须是**值**而非指针 —— 若为指针，重置会把 resolveSlot 换成
// 新槽，而仍在旧 once.Do 里的调用方会继续持有旧指针，
// 导致「已判定的新 once + 旧槽的 value」混杂。
type pathCacheSlot struct {
	// once 保证只解析一次（重置时整体换新）
	once sync.Once
	// value 缓存结果
	value any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// UserHomeDir 获取用户主目录。
//
// 优先级（与 Python get_user_home() 对齐）：
//  1. 缓存值（首次调用后缓存）
//  2. UAPCLAW_HOME 环境变量
//  3. 标准库 os.UserHomeDir()
func UserHomeDir() string {
	slot := loadSlot(&userHomeSlot)
	slot.once.Do(func() {
		value := ""
		if env := os.Getenv(EnvHome); env != "" {
			value = env
		} else if home, err := os.UserHomeDir(); err == nil {
			value = home
		} else {
			// 降级到当前目录
			value = "."
		}
		slot.setValue(value)
	})
	value, _ := slot.value.(string)
	return value
}

// ExpandHome 将路径中的 ~ 展开为用户主目录。
// Python: Path.expanduser()
//
// 支持：
//   - "~" → 展开为 UserHomeDir()
//   - "~/foo" → 展开为 UserHomeDir()/foo
//   - "~/" → 展开为 UserHomeDir()/
//   - "~user/foo" → **原样返回**（见下方说明）
//   - 其他路径原样返回
//
// 关于 "~user"：Python 的 expanduser 会查 passwd；查不到时原样返回。Go 侧不做
// passwd 查询（跨平台成本高、生产用不到），统一原样返回 —— 比旧实现把
// "~otheruser" 拼成 "home/otheruser"（一个不存在的绝对路径）更接近 Python 语义。
func ExpandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	home := UserHomeDir()
	if len(path) == 1 {
		return home
	}
	if path[1] == '/' || path[1] == '\\' {
		return filepath.Join(home, path[2:])
	}
	// ~user/ 格式不支持，返回原路径
	return path
}

// ResolveSymlinksLenient 按 Python Path.resolve(strict=False) 语义解析路径。
//
// 与直接调用 filepath.EvalSymlinks 的区别：目标路径可能**尚不存在**（如待创建
// 的文件），此时 EvalSymlinks 必然失败。Python 的做法是解析「已存在的前缀」中
// 的符号链接、保留不存在的尾部；Go 侧同样逐级上溯最近的已存在祖先，解析后把
// 剩余部分拼回，因此 `ws/link -> /etc` 下的 `ws/link/new.txt` 会解析成
// `/etc/new.txt`，不会被误判为仍在 `ws` 内。
//
// 入参先做 ~ 展开与绝对化；磁盘根仍不存在等极端情况返回清理后的绝对路径。
func ResolveSymlinksLenient(p string) string {
	expanded := ExpandHome(p)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		abs = expanded
	}
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		return resolved
	}
	return resolveExistingAncestor(abs)
}

// IsPathWithin 判断 p 是否等于 root 或位于其下（按路径段比较，非字符串前缀）。
//
// 对齐 Python Path.relative_to：`/ws` 不包含 `/ws-evil`，但包含 `/ws` 本身与
// `/ws/a/b`。两侧应已由 ResolveSymlinksLenient 解析，调用方负责。
func IsPathWithin(root, p string) bool {
	if root == "" || p == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// SetUserHome 设置自定义主目录并重置所有缓存。
//
// 调用后 WorkspaceDir()、ConfigDir()、AgentWorkspaceDir() 都会重新解析。
// Python: set_user_home(path)
func SetUserHome(p string) {
	resetPathCache(p)
}

// ResetCache 重置所有路径缓存（供测试使用）。
//
// 并发安全：写入与本包的读取共用 cacheMu，因此测试 cleanup 里调用它
// 不会再与后台 goroutine 的 WorkspaceDir()/UserHomeDir() 形成数据竞争。
// 语义上仍建议先 FlushMetadataQueue/FlushHistoryQueue，让后台任务跑完，
// 否则重置后它可能读到新目录并继续写盘（这是逻辑问题，不是竞争）。
func ResetCache() {
	resetPathCache("")
}

// WorkspaceDir 获取数据根目录（~/.uapclaw/）。
//
// 此函数不受未初始化回退影响，始终返回用户目录下的路径。
// Python: get_user_workspace_dir()
//
// 优先级：
//  1. 缓存值
//  2. UAPCLAW_DATA_DIR 环境变量（用于多实例隔离）
//  3. UserHomeDir() 拼接 ".uapclaw" 子目录
func WorkspaceDir() string {
	slot := loadSlot(&workspaceSlot)
	slot.once.Do(func() {
		value := os.Getenv(EnvDataDir)
		if value == "" {
			value = filepath.Join(UserHomeDir(), DefaultDir)
		}
		slot.setValue(value)
	})
	value, _ := slot.value.(string)
	return value
}

// ConfigDir 获取配置目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/config/，
// 未初始化时回退到 ResourcesDir()。
// Python: get_config_dir()
func ConfigDir() string {
	paths := getResolvedPaths()
	return paths.ConfigDir
}

// AgentWorkspaceDir 获取 Agent 工作空间目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/agent/workspace/，
// 未初始化时回退到 ResourcesDir()/agent/workspace。
// Python: get_workspace_dir()
func AgentWorkspaceDir() string {
	paths := getResolvedPaths()
	return paths.WorkspaceDir
}

// IsInitialized 检查工作区是否已初始化。
//
// 判断依据：WorkspaceDir()/config/ 目录是否存在。
// Python: _resolve_paths() 中 user_config_dir.exists() 判断
func IsInitialized() bool {
	configDir := filepath.Join(WorkspaceDir(), "config")
	return dirExists(configDir)
}

// ResourcesDir 获取外部资源目录。
//
// 三级回退（Go 版特有，Python 通过 __file__ 反推包位置）：
//  1. UAPCLAW_RESOURCES_DIR 环境变量
//  2. 可执行文件同目录的 resources/
//  3. 当前工作目录的 resources/
//
// 找不到时返回错误。
func ResourcesDir() (string, error) {
	// 优先使用环境变量
	if env := os.Getenv(EnvResourcesDir); env != "" {
		if dirExists(env) {
			return env, nil
		}
		return "", fmt.Errorf("UAPCLAW_RESOURCES_DIR 指向的目录不存在: %s", env)
	}

	// 尝试可执行文件同目录
	execPath, err := os.Executable()
	if err == nil {
		resDir := filepath.Join(filepath.Dir(execPath), "resources")
		if dirExists(resDir) {
			return resDir, nil
		}
	}

	// 尝试当前工作目录
	if cwd, err := os.Getwd(); err == nil {
		resDir := filepath.Join(cwd, "resources")
		if dirExists(resDir) {
			return resDir, nil
		}
	}

	return "", fmt.Errorf("找不到 resources 目录，请设置 UAPCLAW_RESOURCES_DIR 环境变量或确认 resources/ 目录存在于可执行文件同目录或当前目录")
}

// ConfigFile 返回配置文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_file()。
func ConfigFile() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// EnvFile 返回环境变量文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_dir() 同级 .env。
func EnvFile() string {
	return filepath.Join(ConfigDir(), ".env")
}

// 以下路径辅助函数始终基于 WorkspaceDir() 派生，不受回退影响。
// Python: 各 get_xxx_dir() 函数

// AgentRootDir 返回 Agent 根目录：WorkspaceDir()/agent
func AgentRootDir() string {
	return filepath.Join(WorkspaceDir(), "agent")
}

// AgentMemoryDir 返回 Agent 记忆目录：WorkspaceDir()/agent/workspace/memory
func AgentMemoryDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "memory")
}

// AgentSkillsDir 返回 Agent 技能目录：WorkspaceDir()/agent/workspace/skills
func AgentSkillsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "skills")
}

// AgentSessionsDir 返回 Agent 会话目录：WorkspaceDir()/agent/sessions
func AgentSessionsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "sessions")
}

// AgentInteractionsDir 返回 Agent 交互目录：WorkspaceDir()/agent/workspace/interactions
func AgentInteractionsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "interactions")
}

// CheckpointDir 返回检查点目录：WorkspaceDir()/agent/.checkpoint
func CheckpointDir() string {
	return filepath.Join(WorkspaceDir(), "agent", ".checkpoint")
}

// LogsDir 返回日志目录：WorkspaceDir()/agent/.logs
func LogsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", ".logs")
}

// DeepAgentTodoDir 返回 DeepAgent 待办目录：WorkspaceDir()/agent/workspace/todo
func DeepAgentTodoDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "todo")
}

// DeepAgentMessagesDir 返回 DeepAgent 消息目录：WorkspaceDir()/agent/workspace/messages
func DeepAgentMessagesDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "messages")
}

// DeepAgentAgentsDir 返回 DeepAgent 子 Agent 目录：WorkspaceDir()/agent/workspace/agents
func DeepAgentAgentsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "agents")
}

// DeepAgentHeartbeatPath 返回 DeepAgent 心跳文件路径：WorkspaceDir()/agent/workspace/HEARTBEAT.md
func DeepAgentHeartbeatPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "HEARTBEAT.md")
}

// DeepAgentAgentMDPath 返回 DeepAgent Agent 描述文件路径：WorkspaceDir()/agent/workspace/AGENT.md
func DeepAgentAgentMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "AGENT.md")
}

// DeepAgentSoulMDPath 返回 DeepAgent Soul 文件路径：WorkspaceDir()/agent/workspace/SOUL.md
func DeepAgentSoulMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "SOUL.md")
}

// DeepAgentIdentityMDPath 返回 DeepAgent Identity 文件路径：WorkspaceDir()/agent/workspace/IDENTITY.md
func DeepAgentIdentityMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "IDENTITY.md")
}

// DeepAgentUserMDPath 返回 DeepAgent User 文件路径：WorkspaceDir()/agent/workspace/USER.md
func DeepAgentUserMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "USER.md")
}

// CronJobsPath 返回 cron 任务文件路径：WorkspaceDir()/agent/home/cron_jobs.json。
// Python: get_cron_jobs_path()（gateway 与 agentserver 共享同一文件）
func CronJobsPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "home", "cron_jobs.json")
}

// AgentTeamsHomeDir 返回 Agent Teams 主目录：WorkspaceDir()/.agent_teams。
//
// 目录名带前导点，对齐 Python openjiuwen get_agent_teams_home()：
// jiuwenswarm 启动时把 openjiuwen home 配置为数据根，团队状态位于 <data>/.agent_teams。
func AgentTeamsHomeDir() string {
	return filepath.Join(WorkspaceDir(), ".agent_teams")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getResolvedPaths 获取已解析的路径（带回退逻辑）。
//
// Python: _resolve_paths()，核心逻辑：
//   - ~/.uapclaw/config/ 存在 → 已初始化模式，指向用户目录
//   - 不存在 → 未初始化模式，回退到 resources 目录
//
// 纯计算，不含日志调用。workspace 在调用后根据 Fallback 字段补日志。
// resetPathCache 原子地重置三个缓存槽；userHome 非空时同时设置主目录。
//
// 一起清空 Once 与值：只清值不清 Once 会让下一次调用复用旧值。
func resetPathCache(userHome string) {
	homeSlot := &pathCacheSlot{}
	if userHome != "" {
		// 设置主目录等价于"已算好"：直接填值并标记 once 已执行
		homeSlot.value = userHome
		homeSlot.once.Do(func() {})
	}
	cacheMu.Lock()
	userHomeSlot = homeSlot
	workspaceSlot = &pathCacheSlot{}
	resolvedSlot = &pathCacheSlot{}
	cacheMu.Unlock()
}

// loadSlot 在 cacheMu 保护下取当前缓存槽（避免读到半更新指针）。
func loadSlot(slot **pathCacheSlot) *pathCacheSlot {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	return *slot
}

// setValue 在 cacheMu 保护下写入缓存结果。
//
// 读取侧不加锁（一次值读取，配合 once 的执行语义已足以保证 happens-before），
// 因此这里也绝不能在持锁时调用 once.Do —— 否则与 resetPathCache 构成自锁。
func (s *pathCacheSlot) setValue(value any) {
	cacheMu.Lock()
	s.value = value
	cacheMu.Unlock()
}

func getResolvedPaths() *ResolvedPaths {
	slot := loadSlot(&resolvedSlot)
	slot.once.Do(func() {
		slot.setValue(resolvePathsOnce())
	})
	paths, _ := slot.value.(*ResolvedPaths)
	return paths
}

// resolvePathsOnce 真正计算一次解析结果（不碰缓存槽）。
func resolvePathsOnce() *ResolvedPaths {
	resolvedPaths := &ResolvedPaths{}
	{
		workspaceDir := WorkspaceDir()
		userConfigDir := filepath.Join(workspaceDir, "config")
		userWorkspaceDir := filepath.Join(workspaceDir, "agent", "workspace")

		if dirExists(userConfigDir) {
			// 已初始化：指向用户目录
			resolvedPaths = &ResolvedPaths{
				ConfigDir:    userConfigDir,
				WorkspaceDir: userWorkspaceDir,
				Fallback:     false,
			}
		} else {
			// 未初始化：回退到 resources 目录
			resDir, err := ResourcesDir()
			if err != nil {
				// resources 也找不到，降级为用户目录（后续操作因目录不存在自然报错）
				resolvedPaths = &ResolvedPaths{
					ConfigDir:    userConfigDir,
					WorkspaceDir: userWorkspaceDir,
					Fallback:     true,
				}
				return resolvedPaths
			}
			resolvedPaths = &ResolvedPaths{
				ConfigDir:    resDir,
				WorkspaceDir: filepath.Join(resDir, "agent", "workspace"),
				Fallback:     true,
			}
		}
	}
	return resolvedPaths
}

// dirExists 检查目录是否存在。
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// resolveExistingAncestor 解析最近已存在祖先的符号链接，再拼回不存在的尾部。
//
// 对齐 Python Path.resolve(strict=False)：`/ws/link/new.txt` 中 `/ws/link` 存在
// 则解析为 `/etc`，尾部 `new.txt` 原样拼接，得到 `/etc/new.txt`。
func resolveExistingAncestor(abs string) string {
	remainder := make([]string, 0, 4)
	current := filepath.Clean(abs)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// 已到磁盘根仍不存在：无符号链接可解析，返回清理后的绝对路径
			return filepath.Clean(abs)
		}
		remainder = append(remainder, filepath.Base(current))
		current = parent
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			continue
		}
		parts := make([]string, 0, len(remainder)+1)
		parts = append(parts, resolved)
		for i := len(remainder) - 1; i >= 0; i-- {
			parts = append(parts, remainder[i])
		}
		return filepath.Join(parts...)
	}
}
