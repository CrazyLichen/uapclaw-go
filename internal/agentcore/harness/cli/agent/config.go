package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// OpenJiuWen CLI 的配置管理。
//
// 三层优先级：settings.json < 环境变量 < CLI 参数。
//
// Python: openjiuwen/harness/cli/agent/config.py（227 行）

// ──────────────────────────── 结构体 ────────────────────────────

// CLIConfig CLI 运行时配置。
// Python: config.py:65-88 CLIConfig
type CLIConfig struct {
	// Provider LLM provider 名（如 "OpenAI"、"DashScope"）
	Provider string `json:"provider"`
	// Model 模型名（如 "gpt-4o"）
	Model string `json:"model"`
	// APIKey provider API key
	APIKey string `json:"api_key"`
	// APIBase provider API 基础 URL
	APIBase string `json:"api_base"`
	// MaxIterations ReAct + TaskLoop 最大迭代次数
	MaxIterations int `json:"max_iterations"`
	// MaxTokens 单次模型调用的最大 token 数
	MaxTokens int `json:"max_tokens"`
	// ServerURL 远程 agent-server URL；空表示本地模式
	ServerURL string `json:"server_url"`
	// Cwd 工作目录（运行时填充）
	Cwd string `json:"cwd"`
	// Workspace rail 状态（记忆、todo、技能等）的 agent 工作区目录
	Workspace string `json:"workspace"`
	// Verbose 启用详细日志
	Verbose bool `json:"verbose"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultSettingsPath 用户级 settings 文件的固定路径。
	// Python: config.py:16 SETTINGS_PATH
	DefaultSettingsPath = "~/.openjiuwen/settings.json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultWorkspace 默认 CLI 工作区路径。
// Python: config.py:12-14 _default_workspace
func DefaultWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openjiuwen", "workspace")
}

// SettingsPath 解析 settings 文件路径（支持 ~ 展开）。
// path 为空时用 ~/.openjiuwen/settings.json。
func SettingsPath(path string) (string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".openjiuwen", "settings.json"), nil
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(home, path[2:]), nil
		}
	}
	return path, nil
}

// LoadSettingsJSON 从 settings.json 读配置。
// Python: config.py:22-44 load_settings_json
//
// 文件缺失或非法时返回空 map，绝不报错。
func LoadSettingsJSON(path string) map[string]any {
	resolved, err := SettingsPath(path)
	if err != nil {
		return map[string]any{}
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return map[string]any{}
	}
	var data map[string]any
	if json.Unmarshal(content, &data) != nil || data == nil {
		return map[string]any{}
	}
	return data
}

// SaveSettingsJSON 把 data 合并进 settings.json（新键覆盖）。
// Python: config.py:47-69 save_settings_json
//
// 返回最终写盘的路径。
func SaveSettingsJSON(data map[string]any, path string) (string, error) {
	resolved, err := SettingsPath(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", err
	}
	existing := LoadSettingsJSON(resolved)
	for key, value := range data {
		existing[key] = value
	}
	encoded, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", err
	}
	// Python: json.dumps(...) + "\n"——尾随换行
	if err := os.WriteFile(resolved, append(encoded, '\n'), 0o644); err != nil {
		return "", err
	}
	return resolved, nil
}

// LoadConfig 三层合并构造 CLIConfig。
// Python: config.py:124-206 load_config
//
// 优先级（高到低）：CLI 参数 > 环境变量 > settings.json > 默认值。
// 构造后立即校验（LoadConfigOptions 用指针字段表达「CLI 参数显式给过」，
// 区分空串与未设置）。
func LoadConfig(opts LoadConfigOptions) (CLIConfig, error) {
	settings := LoadSettingsJSON("")

	cfg := CLIConfig{
		Provider:  firstNonEmptyStr(opts.Provider, os.Getenv("OPENJIUWEN_PROVIDER"), settingsStr(settings, "provider"), "OpenAI"),
		Model:     firstNonEmptyStr(opts.Model, os.Getenv("OPENJIUWEN_MODEL"), settingsStr(settings, "model"), "gpt-4o"),
		APIKey:    firstNonEmptyStr(opts.APIKey, os.Getenv("OPENJIUWEN_API_KEY"), settingsStr(settings, "apiKey"), ""),
		APIBase:   firstNonEmptyStr(opts.APIBase, os.Getenv("OPENJIUWEN_API_BASE"), settingsStr(settings, "apiBase"), "https://api.openai.com/v1"),
		ServerURL: firstNonEmptyStr(opts.ServerURL, os.Getenv("OPENJIUWEN_SERVER_URL"), settingsStr(settings, "serverUrl"), ""),
		Workspace: firstNonEmptyStr(opts.Workspace, os.Getenv("OPENJIUWEN_WORKSPACE"), settingsStr(settings, "workspace"), DefaultWorkspace()),
		Cwd:       currentDir(),
		Verbose:   opts.Verbose,
	}
	maxIterations, err := intSetting(
		firstNonEmptyAny(os.Getenv("OPENJIUWEN_MAX_ITERATIONS"), settings["maxIterations"], 30),
		"max_iterations")
	if err != nil {
		return CLIConfig{}, err
	}
	cfg.MaxIterations = maxIterations
	maxTokens, err := intSetting(
		firstNonEmptyAny(os.Getenv("OPENJIUWEN_MAX_TOKENS"), settings["maxTokens"], 8192),
		"max_tokens")
	if err != nil {
		return CLIConfig{}, err
	}
	cfg.MaxTokens = maxTokens

	if err := cfg.Validate(); err != nil {
		return CLIConfig{}, err
	}
	return cfg, nil
}

// LoadConfigOptions LoadConfig 的 CLI 参数层。
// Python: config.py:126-135 的 keyword-only 参数
type LoadConfigOptions struct {
	// Provider 覆盖 OPENJIUWEN_PROVIDER
	Provider string
	// Model 覆盖 OPENJIUWEN_MODEL
	Model string
	// APIKey 覆盖 OPENJIUWEN_API_KEY
	APIKey string
	// APIBase 覆盖 OPENJIUWEN_API_BASE
	APIBase string
	// ServerURL 覆盖 OPENJIUWEN_SERVER_URL
	ServerURL string
	// Workspace 覆盖 OPENJIUWEN_WORKSPACE
	Workspace string
	// Verbose 启用详细日志
	Verbose bool
}

// Validate 校验配置值。
// Python: config.py:90-110 validate
//
// APIKey 缺失（本地模式）或 MaxTokens 过小（< 256）时返回错误。
func (c *CLIConfig) Validate() error {
	if c.APIKey == "" && c.ServerURL == "" {
		return fmt.Errorf("API key not set. Use --api-key, OPENJIUWEN_API_KEY, or add to ~/.openjiuwen/settings.json.")
	}
	if c.MaxTokens < 256 {
		return fmt.Errorf("max_tokens=%d is dangerously small (min 256). Check OPENJIUWEN_MAX_TOKENS.", c.MaxTokens)
	}
	if c.MaxIterations < 1 {
		return fmt.Errorf("max_iterations=%d must be >= 1.", c.MaxIterations)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// intSetting 把 settings/env 值转成 int，坏类型报 ValueError。
// Python: config.py:112-122 _int_setting
//
// Click 命令把 ValueError 转成一行 Error 输出；int() 的裸 TypeError
// （settings.json 里数字的位置放了对象）曾绕过它打出 traceback。
func intSetting(value any, option string) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer, got %v", option, value)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be an integer, got %v", option, value)
	}
}

// settingsStr 取 settings 里的字符串值（类型不符返回空串）。
func settingsStr(settings map[string]any, key string) string {
	if value, ok := settings[key].(string); ok {
		return value
	}
	return ""
}

// firstNonEmptyStr 返回第一个非空字符串。
// Python: `a or b or c or default` 的 or 链
func firstNonEmptyStr(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// firstNonEmptyAny or 链的 any 版本。
//
// Python 的  按真值跳过：None、空串、0 都算假值继续向后。
// Go 侧对齐：nil 与空串跳过（0 保留——maxIterations/maxTokens 的显式 0
// 应走到校验层报错，而不是被默认值吞掉；Python 的 or 会把 0 吞掉，
// 属上游 or 链的隐式缺陷，Go 侧不复制）。
func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if text, isText := value.(string); isText && text == "" {
			continue
		}
		return value
	}
	return nil
}

// currentDir 当前工作目录；失败返回空串（Python: os.getcwd()）。
func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}
