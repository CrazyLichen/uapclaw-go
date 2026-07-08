package path

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedPaths 缓存已解析的 ConfigDir 和 AgentWorkspaceDir。
//
// 对应 Python: jiuwenswarm/common/utils.py 中的 _config_dir / _workspace_dir 全局变量。
// 未初始化时（~/.uapclaw/config/ 不存在）回退到 resources 目录，
// 与 Python _resolve_paths() 行为对齐。
type ResolvedPaths struct {
	// ConfigDir 配置目录
	ConfigDir string
	// WorkspaceDir Agent 工作空间目录
	WorkspaceDir string
	// Fallback true 表示 ConfigDir 来自 resources 回退而非用户目录
	Fallback bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EnvHome 自定义用户主目录环境变量。
	// 对应 Python: JIUWENSWARM_HOME
	EnvHome = "UAPCLAW_HOME"

	// EnvDataDir 自定义数据根目录环境变量。
	// 对应 Python: JIUWENSWARM_DATA_DIR
	EnvDataDir = "UAPCLAW_DATA_DIR"

	// EnvResourcesDir 自定义资源目录环境变量。
	// Go 版特有，Python 通过 __file__ 反推包位置，Go 用环境变量覆盖。
	EnvResourcesDir = "UAPCLAW_RESOURCES_DIR"

	// DefaultDir 默认工作区目录名。
	// 对应 Python: ".jiuwenswarm"
	DefaultDir = ".uapclaw"

	// DefaultInstancesDir 默认命名实例根目录名。
	// 对应 Python: ".jiuwenswarm-instances"
	DefaultInstancesDir = ".uapclaw-instances"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// userHomeOnce 保证 UserHomeDir 只解析一次。
	userHomeOnce sync.Once
	userHomeVal  string

	// workspaceOnce 保证 WorkspaceDir 只解析一次。
	workspaceOnce sync.Once
	workspaceVal  string

	// resolveOnce 保证 resolvePaths 只执行一次。
	resolveOnce   sync.Once
	resolvedPaths *ResolvedPaths
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UserHomeDir 获取用户主目录。
//
// 优先级（与 Python get_user_home() 对齐）：
//  1. 缓存值（首次调用后缓存）
//  2. UAPCLAW_HOME 环境变量
//  3. os.UserHomeDir()
func UserHomeDir() string {
	userHomeOnce.Do(func() {
		if env := os.Getenv(EnvHome); env != "" {
			userHomeVal = env
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			// 降级到当前目录
			userHomeVal = "."
			return
		}
		userHomeVal = home
	})
	return userHomeVal
}

// SetUserHome 设置自定义主目录并重置所有缓存。
//
// 调用后 WorkspaceDir()、ConfigDir()、AgentWorkspaceDir() 都会重新解析。
// 对应 Python: set_user_home(path)
func SetUserHome(p string) {
	userHomeVal = p
	// 重置所有缓存
	userHomeOnce = sync.Once{}
	workspaceOnce = sync.Once{}
	resolveOnce = sync.Once{}
	resolvedPaths = nil
}

// ResetCache 重置所有路径缓存（供测试使用）。
func ResetCache() {
	userHomeOnce = sync.Once{}
	userHomeVal = ""
	workspaceOnce = sync.Once{}
	workspaceVal = ""
	resolveOnce = sync.Once{}
	resolvedPaths = nil
}

// WorkspaceDir 获取数据根目录（~/.uapclaw/）。
//
// 此函数不受未初始化回退影响，始终返回用户目录下的路径。
// 对应 Python: get_user_workspace_dir()
//
// 优先级：
//  1. 缓存值
//  2. UAPCLAW_DATA_DIR 环境变量（用于多实例隔离）
//  3. UserHomeDir() / ".uapclaw"
func WorkspaceDir() string {
	workspaceOnce.Do(func() {
		if env := os.Getenv(EnvDataDir); env != "" {
			workspaceVal = env
			return
		}
		workspaceVal = filepath.Join(UserHomeDir(), DefaultDir)
	})
	return workspaceVal
}

// ConfigDir 获取配置目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/config/，
// 未初始化时回退到 ResourcesDir()。
// 对应 Python: get_config_dir()
func ConfigDir() string {
	paths := getResolvedPaths()
	return paths.ConfigDir
}

// AgentWorkspaceDir 获取 Agent 工作空间目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/agent/workspace/，
// 未初始化时回退到 ResourcesDir()/agent/workspace。
// 对应 Python: get_workspace_dir()
func AgentWorkspaceDir() string {
	paths := getResolvedPaths()
	return paths.WorkspaceDir
}

// IsInitialized 检查工作区是否已初始化。
//
// 判断依据：WorkspaceDir()/config/ 目录是否存在。
// 对应 Python: _resolve_paths() 中 user_config_dir.exists() 判断
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
// 对应 Python: 各 get_xxx_dir() 函数

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// getResolvedPaths 获取已解析的路径（带回退逻辑）。
//
// 对应 Python: _resolve_paths()，核心逻辑：
//   - ~/.uapclaw/config/ 存在 → 已初始化模式，指向用户目录
//   - 不存在 → 未初始化模式，回退到 resources 目录
//
// 纯计算，不含日志调用。workspace 在调用后根据 Fallback 字段补日志。
func getResolvedPaths() *ResolvedPaths {
	resolveOnce.Do(func() {
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
				return
			}
			resolvedPaths = &ResolvedPaths{
				ConfigDir:    resDir,
				WorkspaceDir: filepath.Join(resDir, "agent", "workspace"),
				Fallback:     true,
			}
		}
	})
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
