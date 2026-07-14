package workspace

import (
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// 重导出 path 包常量，保持 workspace 外部 API 不变。
const (
	// EnvHome 自定义用户主目录环境变量。
	EnvHome = path.EnvHome
	// EnvDataDir 自定义数据根目录环境变量。
	EnvDataDir = path.EnvDataDir
	// EnvResourcesDir 自定义资源目录环境变量。
	EnvResourcesDir = path.EnvResourcesDir
	// DefaultDir 默认工作区目录名。
	DefaultDir = path.DefaultDir
	// DefaultInstancesDir 默认命名实例根目录名。
	DefaultInstancesDir = path.DefaultInstancesDir
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fallbackLogged 防止重复打回退日志
	fallbackLogged bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UserHomeDir 获取用户主目录。
func UserHomeDir() string {
	return path.UserHomeDir()
}

// SetUserHome 设置自定义主目录并重置所有缓存。
func SetUserHome(p string) {
	path.SetUserHome(p)
}

// WorkspaceDir 获取数据根目录（~/.uapclaw/）。
func WorkspaceDir() string {
	return path.WorkspaceDir()
}

// ConfigDir 获取配置目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/config/，
// 未初始化时回退到 ResourcesDir()。
// 对应 Python: get_config_dir()
func ConfigDir() string {
	dir := path.ConfigDir()
	logFallbackIfNeeded()
	return dir
}

// AgentWorkspaceDir 获取 Agent 工作空间目录。
func AgentWorkspaceDir() string {
	dir := path.AgentWorkspaceDir()
	logFallbackIfNeeded()
	return dir
}

// IsInitialized 检查工作区是否已初始化。
func IsInitialized() bool {
	return path.IsInitialized()
}

// ResourcesDir 获取外部资源目录。
func ResourcesDir() (string, error) {
	return path.ResourcesDir()
}

// ConfigFile 返回配置文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_file()。
func ConfigFile() string {
	return path.ConfigFile()
}

// EnvFile 返回环境变量文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_dir() 同级 .env。
func EnvFile() string {
	return path.EnvFile()
}

// 以下路径辅助函数始终基于 WorkspaceDir() 派生，不受回退影响。
// 对应 Python: 各 get_xxx_dir() 函数

// AgentRootDir 返回 Agent 根目录：WorkspaceDir()/agent
func AgentRootDir() string {
	return path.AgentRootDir()
}

// AgentMemoryDir 返回 Agent 记忆目录：WorkspaceDir()/agent/workspace/memory
func AgentMemoryDir() string {
	return path.AgentMemoryDir()
}

// AgentSkillsDir 返回 Agent 技能目录：WorkspaceDir()/agent/workspace/skills
func AgentSkillsDir() string {
	return path.AgentSkillsDir()
}

// AgentSessionsDir 返回 Agent 会话目录：WorkspaceDir()/agent/sessions
func AgentSessionsDir() string {
	return path.AgentSessionsDir()
}

// AgentInteractionsDir 返回 Agent 交互目录：WorkspaceDir()/agent/workspace/interactions
func AgentInteractionsDir() string {
	return path.AgentInteractionsDir()
}

// CheckpointDir 返回检查点目录：WorkspaceDir()/agent/.checkpoint
func CheckpointDir() string {
	return path.CheckpointDir()
}

// LogsDir 返回日志目录：WorkspaceDir()/agent/.logs
func LogsDir() string {
	return path.LogsDir()
}

// DeepAgentTodoDir 返回 DeepAgent 待办目录：WorkspaceDir()/agent/workspace/todo
func DeepAgentTodoDir() string {
	return path.DeepAgentTodoDir()
}

// DeepAgentMessagesDir 返回 DeepAgent 消息目录：WorkspaceDir()/agent/workspace/messages
func DeepAgentMessagesDir() string {
	return path.DeepAgentMessagesDir()
}

// DeepAgentAgentsDir 返回 DeepAgent 子 Agent 目录：WorkspaceDir()/agent/workspace/agents
func DeepAgentAgentsDir() string {
	return path.DeepAgentAgentsDir()
}

// DeepAgentHeartbeatPath 返回 DeepAgent 心跳文件路径：WorkspaceDir()/agent/workspace/HEARTBEAT.md
func DeepAgentHeartbeatPath() string {
	return path.DeepAgentHeartbeatPath()
}

// DeepAgentAgentMDPath 返回 DeepAgent Agent 描述文件路径：WorkspaceDir()/agent/workspace/AGENT.md
func DeepAgentAgentMDPath() string {
	return path.DeepAgentAgentMDPath()
}

// DeepAgentSoulMDPath 返回 DeepAgent Soul 文件路径：WorkspaceDir()/agent/workspace/SOUL.md
func DeepAgentSoulMDPath() string {
	return path.DeepAgentSoulMDPath()
}

// DeepAgentIdentityMDPath 返回 DeepAgent Identity 文件路径：WorkspaceDir()/agent/workspace/IDENTITY.md
func DeepAgentIdentityMDPath() string {
	return path.DeepAgentIdentityMDPath()
}

// DeepAgentUserMDPath 返回 DeepAgent User 文件路径：WorkspaceDir()/agent/workspace/USER.md
func DeepAgentUserMDPath() string {
	return path.DeepAgentUserMDPath()
}

// AgentTeamsHomeDir 返回 Agent Teams 主目录：WorkspaceDir()/agent_teams。
// 对齐 Python: get_agent_teams_home()
func AgentTeamsHomeDir() string {
	return path.AgentTeamsHomeDir()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// logFallbackIfNeeded 检查 path 包的回退状态并补日志。
//
// path 包是纯计算不含日志，workspace 负责在回退时输出日志。
func logFallbackIfNeeded() {
	if fallbackLogged {
		return
	}
	// 通过比较 ConfigDir 和 WorkspaceDir/config 判断是否回退
	configDir := path.ConfigDir()
	userConfigDir := filepath.Join(path.WorkspaceDir(), "config")
	if configDir != userConfigDir {
		logger.Info(logComponent).Str("config_dir", configDir).Msg("工作区未初始化，回退到 resources 目录")
		fallbackLogged = true
	}
}

// dirExists 检查目录是否存在。
func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}
