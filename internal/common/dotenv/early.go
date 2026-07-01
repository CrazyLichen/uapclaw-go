package dotenv

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// parsedDotenv 记录已加载的 .env 文件路径。
// 对应 Python: _parsed_dotenv
var parsedDotenv string

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseEarly 在所有子命令执行前预解析 --dotenv/--name 参数。
//
// 本函数在 cobra 的 PersistentPreRunE 钩子中被调用，确保
// UAPCLAW_DATA_DIR 环境变量在任何 workspace 路径函数首次调用前就位。
// Go 版不需要像 Python 那样手动扫描 os.Args，也不需要独立重写
// YAML 解析，因为 Go 的包是编译时链接的，且 workspace 包使用
// sync.Once 惰性求值。
//
// 优先级：
//  1. dotenvPath 不为空：加载指定 .env 文件
//  2. instanceName 不为空：按实例名加载 bootstrap .env
//  3. 两者为空：不做任何操作
//
// 对应 Python: parse_dotenv_early(component_name)
func ParseEarly(dotenvPath string, instanceName string) (string, error) {
	// 优先级 1：--dotenv <path>
	if dotenvPath != "" {
		return loadDotenvByPath(dotenvPath)
	}

	// 优先级 2：--name <name>
	if instanceName != "" {
		return loadBootstrapByName(instanceName)
	}

	// 两者都为空，不做任何操作
	return "", nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsedDotenv 返回已加载的 .env 文件路径。
//
// 如果 ParseEarly 尚未被调用或未加载任何 .env，返回空字符串。
// 对应 Python: get_parsed_dotenv()
func ParsedDotenv() string {
	return parsedDotenv
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadDotenvByPath 加载指定路径的 .env 文件。
//
// 对应 Python: parse_dotenv_early() 中 --dotenv 分支
func loadDotenvByPath(dotenvPath string) (string, error) {
	// 展开路径（~ → 用户主目录）
	expanded := expandHome(dotenvPath)

	// 解析为绝对路径
	absPath, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("解析 .env 路径失败: %w", err)
	}

	// 检查文件存在
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		logger.Warn(logComponent).Str("path", absPath).Msg("--dotenv 文件不存在")
		return "", fmt.Errorf("--dotenv 文件不存在: %s", absPath)
	}

	// 加载 .env 文件（override 模式）
	if err := Load(absPath); err != nil {
		logger.Error(logComponent).Str("path", absPath).Err(err).Msg("加载 .env 文件失败")
		return "", fmt.Errorf("加载 .env 文件失败: %w", err)
	}

	parsedDotenv = absPath
	logger.Info(logComponent).Str("path", absPath).Msg("已加载 --dotenv 文件")
	return absPath, nil
}

// loadBootstrapByName 按实例名加载 bootstrap .env 文件。
//
// 与 Python 的 _load_bootstrap_by_name_early 不同，Go 版可以直接调用
// workspace 包的函数，因为 Go 不存在 Python 的 import 时序问题。
// workspace 包已实现了完整的实例名称验证、配置加载和 bootstrap 创建。
//
// 对应 Python: _load_bootstrap_by_name_early(name, component_name)
func loadBootstrapByName(name string) (string, error) {
	// 验证实例名称
	if err := workspace.ValidateInstanceName(name); err != nil {
		logger.Error(logComponent).Str("name", name).Err(err).Msg("无效的实例名称")
		return "", fmt.Errorf("无效的实例名称 %q: %w", name, err)
	}

	// 获取实例配置
	config, err := workspace.GetInstanceConfig(name)
	if err != nil {
		logger.Error(logComponent).Str("name", name).Err(err).Msg("获取实例配置失败")
		return "", fmt.Errorf("获取实例配置失败: %w", err)
	}
	if config == nil {
		logger.Error(logComponent).Str("name", name).Msg("实例在 instances.yaml 中未找到")
		return "", fmt.Errorf("实例 %q 在 instances.yaml 中未找到，请先运行 'uapclaw init --name %s' 创建", name, name)
	}

	// 检查工作区目录是否存在
	if _, err := os.Stat(config.Workspace); os.IsNotExist(err) {
		logger.Error(logComponent).Str("name", name).Str("workspace", config.Workspace).Msg("实例工作区目录不存在")
		return "", fmt.Errorf("实例工作区目录不存在: %s，请先运行 'uapclaw init --name %s' 创建", config.Workspace, name)
	}

	// 检查 bootstrap .env 是否存在，不存在则创建
	envPath := filepath.Join(config.Workspace, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		// 创建 bootstrap .env 文件
		createdPath, err := workspace.CreateBootstrapEnvForName(name, config.Workspace)
		if err != nil {
			logger.Error(logComponent).Str("name", name).Err(err).Msg("创建 bootstrap .env 失败")
			return "", fmt.Errorf("创建 bootstrap .env 失败: %w", err)
		}
		envPath = createdPath
	}

	// 加载 .env 文件（override 模式）
	if err := Load(envPath); err != nil {
		logger.Error(logComponent).Str("name", name).Str("path", envPath).Err(err).Msg("加载 bootstrap .env 失败")
		return "", fmt.Errorf("加载 bootstrap .env 失败: %w", err)
	}

	parsedDotenv = envPath
	logger.Info(logComponent).Str("name", name).Str("path", envPath).Msg("已加载实例 bootstrap .env")
	return envPath, nil
}

// expandHome 将路径中的 ~ 展开为用户主目录。
func expandHome(path string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if len(path) == 1 {
			return home
		}
		// ~/xxx → /home/user/xxx
		if path[1] == '/' || path[1] == '\\' {
			return filepath.Join(home, path[2:])
		}
		// ~user/xxx 不处理，返回原值
	}
	return path
}
