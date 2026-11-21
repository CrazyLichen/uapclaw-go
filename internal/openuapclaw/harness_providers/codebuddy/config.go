package codebuddy

import (
	"fmt"
	"slices"
	"strings"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultCliPath CliPath 零值默认：取 PATH 上的 codebuddy。
	constDefaultCliPath = "codebuddy"
	// defaultTurnTimeoutSeconds 单轮超时零值默认（秒）。
	constDefaultTurnTimeoutSeconds = 600
	// defaultInactivityTimeoutSeconds 轮内无输出超时零值默认（秒）。
	constDefaultInactivityTimeoutSeconds = 120
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodebuddyConfig provider 自有配置。
// Python 无对应物（偏差 D-15.22-a）；字段形态参考 dsh/config.py:16-51 的
// provider 自有配置面，裁剪到 CLI 子进程所需最小集。
type CodebuddyConfig struct {
	// CliPath CLI 可执行路径；空取 PATH 上的 "codebuddy"
	CliPath string
	// Model 模型 ID（--model）；空用 CLI 默认
	Model string
	// PermissionMode 权限模式（--permission-mode）；空用 CLI 默认
	PermissionMode string
	// Cwd 会话工作目录（上下文 Cwd 空时兜底）
	Cwd string
	// Env 子进程环境覆盖（Python: env，dsh 同名面）
	Env map[string]string
	// McpConfig --mcp-config 载荷（JSON 文件路径或内联 JSON 串）
	McpConfig string
	// EventBufferCapacity 观测缓冲容量；0 走基座默认 1024
	EventBufferCapacity int
	// TurnTimeoutSeconds 单轮超时秒数；0 默认 600
	TurnTimeoutSeconds int
	// InactivityTimeoutSeconds 轮内无输出超时秒数；0 默认 120
	InactivityTimeoutSeconds int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodebuddyConfig 构造并执行零值默认与合法性校验。
// Python 无对应物（对齐 dsh NewDshHarnessConfig 的 __post_init__ 等价层）。
func NewCodebuddyConfig(values CodebuddyConfig) (CodebuddyConfig, error) {
	// 零值默认（Python dataclass 字段默认等价层）
	if values.CliPath == "" {
		values.CliPath = constDefaultCliPath
	}
	if values.TurnTimeoutSeconds == 0 {
		values.TurnTimeoutSeconds = constDefaultTurnTimeoutSeconds
	}
	if values.InactivityTimeoutSeconds == 0 {
		values.InactivityTimeoutSeconds = constDefaultInactivityTimeoutSeconds
	}
	if values.EventBufferCapacity == 0 {
		values.EventBufferCapacity = 1024
	}

	// 合法性校验：提供时必须为正数（对齐 dsh config.py:87-96 的 timeout 面）
	if values.TurnTimeoutSeconds < 0 {
		return CodebuddyConfig{}, fmt.Errorf("codebuddy turn_timeout_seconds must be positive when provided")
	}
	if values.InactivityTimeoutSeconds < 0 {
		return CodebuddyConfig{}, fmt.Errorf("codebuddy inactivity_timeout_seconds must be positive when provided")
	}
	if values.EventBufferCapacity <= 0 {
		return CodebuddyConfig{}, fmt.Errorf("codebuddy event_buffer_capacity must be positive")
	}
	// env 必须是字符串到字符串的映射（Go 类型系统保证，留空）
	return values, nil
}

// CodebuddyConfigFromMapping 校验 provider SPI 映射而不启动 CLI 子进程。
// Python 无对应物；形态对齐 dsh config.py:117-140 from_mapping
// （未知键闭集拒绝 + 逐键类型收窄；类型漂移取严格语义——收窄失败即报错，
// 与 plan 用例约定一致）。
func CodebuddyConfigFromMapping(config harnessprotocol.JsonObject) (CodebuddyConfig, error) {
	values := make(map[string]any, len(config))
	for key, value := range config {
		values[key] = value
	}
	known := map[string]bool{
		"cli_path": true, "model": true, "permission_mode": true,
		"cwd": true, "env": true, "mcp_config": true,
		"event_buffer_capacity": true, "turn_timeout_seconds": true,
		"inactivity_timeout_seconds": true,
	}
	unknown := slices.Sorted(func(yield func(string) bool) {
		for key := range values {
			if !known[key] {
				yield(key)
			}
		}
	})
	if len(unknown) > 0 {
		return CodebuddyConfig{}, fmt.Errorf("unknown codebuddy configuration fields: %s", strings.Join(unknown, ", "))
	}

	var result CodebuddyConfig
	// 标量字段：逐键收窄（数字统一 float64，依 YAML/JSON 解码惯例做类型分流）
	stringFields := map[string]*string{
		"cli_path":        &result.CliPath,
		"model":           &result.Model,
		"permission_mode": &result.PermissionMode,
		"cwd":             &result.Cwd,
		"mcp_config":      &result.McpConfig,
	}
	for name, target := range stringFields {
		raw, present := values[name]
		if !present {
			continue
		}
		v, ok := raw.(string)
		if !ok {
			return CodebuddyConfig{}, fmt.Errorf("codebuddy config key %s must be a string", name)
		}
		*target = v
	}
	intFields := map[string]*int{
		"event_buffer_capacity":      &result.EventBufferCapacity,
		"turn_timeout_seconds":       &result.TurnTimeoutSeconds,
		"inactivity_timeout_seconds": &result.InactivityTimeoutSeconds,
	}
	for name, target := range intFields {
		raw, present := values[name]
		if !present {
			continue
		}
		v, ok := raw.(float64)
		if !ok {
			return CodebuddyConfig{}, fmt.Errorf("codebuddy config key %s must be a number", name)
		}
		*target = int(v)
	}

	// env 转 map[string]string（对齐 dsh config.py:127-131）
	if raw, ok := values["env"]; ok {
		mapping, ok := raw.(map[string]any)
		if !ok {
			return CodebuddyConfig{}, fmt.Errorf("codebuddy config key env must be a mapping")
		}
		env := make(map[string]string, len(mapping))
		for key, value := range mapping {
			strValue, isStr := value.(string)
			if !isStr {
				return CodebuddyConfig{}, fmt.Errorf("codebuddy env must map strings to strings")
			}
			env[key] = strValue
		}
		result.Env = env
	}

	return NewCodebuddyConfig(result)
}

// Mapping 导出为 mapping 入参（测试往返用；零值字段省略）。
func (c CodebuddyConfig) Mapping() harnessprotocol.JsonObject {
	result := harnessprotocol.JsonObject{}
	if c.CliPath != "" {
		result["cli_path"] = c.CliPath
	}
	if c.Model != "" {
		result["model"] = c.Model
	}
	if c.PermissionMode != "" {
		result["permission_mode"] = c.PermissionMode
	}
	if c.Cwd != "" {
		result["cwd"] = c.Cwd
	}
	if c.McpConfig != "" {
		result["mcp_config"] = c.McpConfig
	}
	if c.Env != nil {
		env := map[string]any{}
		for key, value := range c.Env {
			env[key] = value
		}
		result["env"] = env
	}
	if c.EventBufferCapacity != 0 {
		result["event_buffer_capacity"] = float64(c.EventBufferCapacity)
	}
	if c.TurnTimeoutSeconds != 0 {
		result["turn_timeout_seconds"] = float64(c.TurnTimeoutSeconds)
	}
	if c.InactivityTimeoutSeconds != 0 {
		result["inactivity_timeout_seconds"] = float64(c.InactivityTimeoutSeconds)
	}
	return result
}
