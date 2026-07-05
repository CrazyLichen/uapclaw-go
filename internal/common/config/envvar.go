package config

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DecryptFunc 敏感字段解密函数。
// 当环境变量名包含 api_key 或 token 时，Config 会自动调用此函数解密。
// envName: 环境变量名；value: 环境变量原始值。
// 返回 (解密后的值, 是否需要解密)。
type DecryptFunc func(envName, value string) (string, bool)

// ──────────────────────────── 全局变量 ────────────────────────────

// envVarPattern 匹配 ${VAR:-default} 或 ${VAR} 格式。
// 分组 1: 变量名；分组 2: 默认值（可选）。
var envVarPattern = regexp.MustCompile(`\$\{([^:}]+)(?::-([^}]*))?\}`)

// sensitiveKeywords 需要解密的敏感字段关键词。
var sensitiveKeywords = []string{"api_key", "token"}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveEnvVars 递归解析配置中的环境变量替换语法 ${VAR:-default}。
//
// 规则：
//   - ${VAR:-default} → VAR 为空或不存在时使用 default
//   - ${VAR}          → VAR 不存在时返回空字符串
//   - 递归处理 map[string]any / []any
//   - 若 decryptFn 不为 nil 且变量名包含 api_key/token，自动调用解密
func ResolveEnvVars(value any, decryptFn DecryptFunc) any {
	switch v := value.(type) {
	case string:
		return resolveEnvVarString(v, decryptFn)
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			result[key] = ResolveEnvVars(val, decryptFn)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = ResolveEnvVars(item, decryptFn)
		}
		return result
	default:
		return value
	}
}

// ResolveEnvVarString 解析单个字符串中的环境变量替换语法。
func ResolveEnvVarString(s string, decryptFn DecryptFunc) string {
	return resolveEnvVarString(s, decryptFn)
}

// ResolvePath 解析配置中的路径，支持 ~ 展开为用户主目录。
func ResolvePath(p string) string {
	if p == "" {
		return p
	}
	// 展开 ~/ 为用户主目录
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	// 展开 ~\ 为 Windows 用户主目录
	if runtime.GOOS == "windows" && strings.HasPrefix(p, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveEnvVarString 替换字符串中所有 ${VAR:-default} 和 ${VAR} 占位符。
func resolveEnvVarString(s string, decryptFn DecryptFunc) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		submatch := envVarPattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		varName := submatch[1]
		defaultVal := ""
		if len(submatch) >= 3 {
			defaultVal = submatch[2]
		}

		// 读取环境变量
		envVal, exists := os.LookupEnv(varName)

		// 环境变量不存在或为空时使用默认值
		if !exists || envVal == "" {
			if defaultVal != "" {
				return defaultVal
			}
			return ""
		}

		// 检查是否需要解密
		if decryptFn != nil && isSensitiveVar(varName) {
			if decrypted, ok := decryptFn(varName, envVal); ok {
				return decrypted
			}
		}

		return envVal
	})
}

// isSensitiveVar 判断变量名是否包含敏感关键词。
// 同时匹配下划线形式（api_key）和连字符形式（api-key）。
func isSensitiveVar(varName string) bool {
	// 将连字符替换为下划线，统一匹配 api_key / api-key 两种形式
	normalized := strings.ReplaceAll(strings.ToLower(varName), "-", "_")
	for _, kw := range sensitiveKeywords {
		if strings.Contains(normalized, kw) {
			return true
		}
	}
	return false
}
