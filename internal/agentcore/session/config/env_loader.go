package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// trySetEnv 尝试将环境变量值设置到 envs 字典中，根据类型映射进行转换。
// 对应 Python: _try_set_env
func trySetEnv(envs map[string]any, configKey, envKey string, value any) {
	if value == nil {
		return
	}
	envType, exists := constants.EnvConfigTypes[envKey]
	if !exists {
		// 无类型映射，直接设置
		envs[configKey] = value
		return
	}

	switch envType {
	case constants.EnvConfigTypeFloat:
		trySetFloat(envs, configKey, envKey, value)
	case constants.EnvConfigTypeInt:
		trySetInt(envs, configKey, envKey, value)
	case constants.EnvConfigTypeBool:
		trySetBool(envs, configKey, envKey, value)
	default:
		envs[configKey] = value
	}
}

// trySetFloat 尝试将值转换为 float64 并设置
func trySetFloat(envs map[string]any, configKey, envKey string, value any) {
	switch v := value.(type) {
	case float64:
		envs[configKey] = v
	case float32:
		envs[configKey] = float64(v)
	case int:
		envs[configKey] = float64(v)
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			logger.Warn(logComponent).
				Str("env_key", envKey).
				Str("value", v).
				Str("expected_type", "float").
				Msg("环境变量 float 转换失败，使用默认值")
			return
		}
		envs[configKey] = f
	default:
		logger.Warn(logComponent).
			Str("env_key", envKey).
			Str("value_type", fmt.Sprintf("%T", value)).
			Str("expected_type", "float").
			Msg("环境变量 float 转换失败，使用默认值")
	}
}

// trySetInt 尝试将值转换为 int 并设置
func trySetInt(envs map[string]any, configKey, envKey string, value any) {
	switch v := value.(type) {
	case int:
		envs[configKey] = v
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			logger.Warn(logComponent).
				Str("env_key", envKey).
				Str("value", v).
				Str("expected_type", "int").
				Msg("环境变量 int 转换失败，使用默认值")
			return
		}
		envs[configKey] = i
	default:
		logger.Warn(logComponent).
			Str("env_key", envKey).
			Str("value_type", fmt.Sprintf("%T", value)).
			Str("expected_type", "int").
			Msg("环境变量 int 转换失败，使用默认值")
	}
}

// trySetBool 尝试将值转换为 bool 并设置
func trySetBool(envs map[string]any, configKey, envKey string, value any) {
	switch v := value.(type) {
	case bool:
		envs[configKey] = v
	case string:
		lower := strings.ToLower(v)
		if lower != "true" && lower != "false" {
			logger.Warn(logComponent).
				Str("env_key", envKey).
				Str("value", v).
				Str("expected_type", "bool").
				Msg("环境变量 bool 转换失败，使用默认值")
			return
		}
		envs[configKey] = lower == "true"
	default:
		logger.Warn(logComponent).
			Str("env_key", envKey).
			Str("value_type", fmt.Sprintf("%T", value)).
			Str("expected_type", "bool").
			Msg("环境变量 bool 转换失败，使用默认值")
	}
}

// loadEnvConfigs 从 os.Getenv 和 context 加载环境变量配置。
// 对应 Python: _load_env_configs
// 优先级：os.Getenv > context.Value > 内置默认值
func loadEnvConfigs(ctx context.Context) map[string]any {
	envConfigs := make(map[string]any)

	for _, entry := range constants.EnvConfigKeys {
		// 先从 os.Getenv 读取
		if osValue := os.Getenv(entry.EnvKey); osValue != "" {
			trySetEnv(envConfigs, entry.ConfigKey, entry.EnvKey, osValue)
		}
		// 再从 context.Value 读取（os.Getenv 优先级更高，如果已经设置则跳过）
		if _, exists := envConfigs[entry.ConfigKey]; exists {
			continue
		}
		if ctxEnvs := getEnvsFromContext(ctx); ctxEnvs != nil {
			if ctxValue, ok := ctxEnvs[entry.EnvKey]; ok {
				trySetEnv(envConfigs, entry.ConfigKey, entry.EnvKey, ctxValue)
			}
		}
	}

	return envConfigs
}
