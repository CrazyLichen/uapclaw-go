package dsh

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DshHarnessConfig 传给 deepseek_harness.DeepSeekHarness 的 provider 自有配置。
// Python: config.py:16-51 DshHarnessConfig（frozen dataclass）；字段名与
// deepseek_harness.DeepSeekHarnessConfig 镜像。dsh_home（或 env 里的非空
// DSH_HOME）对 SDK 运行时是必填的，SDK 从不隐式回落 ~/.dsh。自定义 Cordis
// 组合必须消费显式设置的 system_prompt_env_var；否则适配器经临时启动 overlay
// 使用内置系统提示词插件。
//
// SDK 可选依赖刻意不在此导入（偏差 D-dsh-2：Go 侧 SDK 编译期内嵌恒存在）。
type DshHarnessConfig struct {
	// Provider 模型提供方标识（Python: provider，默认 "deepseek-official"）
	Provider string
	// Model 模型名（Python: model，默认 "deepseek-v4-flash"）
	Model string
	// ReasoningEffort 推理力度（Python: reasoning_effort，可空）
	ReasoningEffort string
	// MaxTokens 输出上限；0 表示不设（Python: max_tokens）
	MaxTokens int
	// SystemPromptMode replace（只换原生前缀）或 append（独立宿主 section）。
	// Python: system_prompt_mode
	SystemPromptMode string
	// Skills portable 技能源（构造期经 NormalizeSkills 冻结；Python: skills）
	Skills []providers.SkillSource
	// SkillConflict skip/replace/append（Python: skill_conflict）
	SkillConflict string
	// Cwd 会话工作目录（Python: cwd）
	Cwd string
	// RuntimeCwd 子进程工作目录（Python: runtime_cwd）
	RuntimeCwd string
	// DshBin dsh 可执行路径（Python: dsh_bin）
	DshBin string
	// DshHome DSH home（Python: dsh_home）
	DshHome string
	// Profile Cordis profile（Python: profile）
	Profile string
	// Patches 顺序 --patch 参数（Python: patches）
	Patches []string
	// Env 子进程环境覆盖（Python: env）
	Env map[string]string
	// LaunchArgsOverride 启动参数覆盖（内部逃生门；Python: launch_args_override）
	LaunchArgsOverride []string
	// InitializeTimeoutSeconds 初始化握手超时（Python: initialize_timeout_seconds）
	InitializeTimeoutSeconds float64
	// RequestTimeoutSeconds 普通请求超时（Python: request_timeout_seconds）
	RequestTimeoutSeconds float64
	// ShutdownTimeoutSeconds 关闭超时（Python: shutdown_timeout_seconds，默认 1.0）
	ShutdownTimeoutSeconds float64
	// BaseURL DEEPSEEK_BASE_URL（Python: base_url）
	BaseURL string
	// APIKey DEEPSEEK_API_KEY（Python: api_key）
	APIKey string
	// SystemPromptEnvVar 显式系统提示词环境变量（Python: system_prompt_env_var）
	SystemPromptEnvVar string
	// EventBufferCapacity 观测缓冲容量（Python: event_buffer_capacity，默认 1024）
	EventBufferCapacity int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDshHarnessConfig 构造并执行 __post_init__ 等价校验。
// Python: config.py:53-115 __post_init__
//
// D-15.12-g 落点：skills 在此构造期经 NormalizeSkills 完成校验（Python 统一
// 校验点收窄后的对应物，勿漏）；dict/str → SkillSource 的转换在 provider
// config unmarshal 层（FromMapping）完成。
func NewDshHarnessConfig(values DshHarnessConfig) (DshHarnessConfig, error) {
	// Python dataclass 字段默认（config.py:29-51）：Go 零值回落到默认值
	if values.Provider == "" {
		values.Provider = "deepseek-official"
	}
	if values.Model == "" {
		values.Model = "deepseek-v4-flash"
	}
	if values.SystemPromptMode == "" {
		values.SystemPromptMode = "replace"
	}
	if values.SkillConflict == "" {
		values.SkillConflict = "skip"
	}
	if values.ShutdownTimeoutSeconds == 0 {
		values.ShutdownTimeoutSeconds = 1.0
	}
	if values.EventBufferCapacity == 0 {
		values.EventBufferCapacity = 1024
	}

	// Python: object.__setattr__(self, "skills", normalize_skills(...))（config.py:54）
	normalized, err := providers.NormalizeSkills(values.Skills, values.SkillConflict)
	if err != nil {
		return DshHarnessConfig{}, err
	}
	values.Skills = normalized

	// Python: config.py:55-58
	if values.SystemPromptMode == "append" && values.SystemPromptEnvVar != "" {
		return DshHarnessConfig{}, fmt.Errorf("append mode cannot use system_prompt_env_var")
	}
	if values.SystemPromptMode != "append" && values.SystemPromptMode != "replace" {
		return DshHarnessConfig{}, fmt.Errorf("system_prompt_mode must be 'append' or 'replace'")
	}
	// Python: config.py:59-62（provider/model 必须是非空字符串；Go 类型系统
	// 已保证为 string，保留非空校验）
	if values.Provider == "" || values.Model == "" {
		return DshHarnessConfig{}, fmt.Errorf("DSH provider and model must not be empty")
	}
	// Python: config.py:78-82（max_tokens 提供时必须为正整数；0 = 未提供，
	// 负数 = 显式非法；bool 排除在 Go 不适用，类型系统保证）
	if values.MaxTokens < 0 {
		return DshHarnessConfig{}, fmt.Errorf("DSH max_tokens must be positive")
	}
	// Python: config.py:83-86
	if values.EventBufferCapacity <= 0 {
		return DshHarnessConfig{}, fmt.Errorf("DSH event_buffer_capacity must be positive")
	}
	// Python: config.py:87-96（三个 timeout 提供时必须为正数；Go 零值 = 未提供；
	// 按 Python 字段顺序报错，不用 map 遍历以免错误顺序不确定）
	for _, entry := range []struct {
		name    string
		timeout float64
	}{
		{"initialize_timeout_seconds", values.InitializeTimeoutSeconds},
		{"request_timeout_seconds", values.RequestTimeoutSeconds},
		{"shutdown_timeout_seconds", values.ShutdownTimeoutSeconds},
	} {
		if entry.timeout < 0 {
			return DshHarnessConfig{}, fmt.Errorf("DSH %s must be positive when provided", entry.name)
		}
	}
	// Python: config.py:97-100（dsh_home/profile/system_prompt_env_var 非空白）
	for _, entry := range []struct {
		name  string
		value string
	}{
		{"dsh_home", values.DshHome},
		{"profile", values.Profile},
		{"system_prompt_env_var", values.SystemPromptEnvVar},
	} {
		if entry.value != "" && strings.TrimSpace(entry.value) == "" {
			return DshHarnessConfig{}, fmt.Errorf("DSH %s must not be empty", entry.name)
		}
	}
	// Python: config.py:101-104（env 必须是字符串到字符串的映射；Go 类型系统保证）
	// Python: config.py:105-106（patches 必须是字符串数组；Go 类型系统保证）
	// Python: config.py:107-113（launch_args_override 形状校验）
	if values.LaunchArgsOverride != nil {
		if len(values.LaunchArgsOverride) == 0 {
			return DshHarnessConfig{}, fmt.Errorf("DSH launch_args_override must contain non-empty strings")
		}
		for _, arg := range values.LaunchArgsOverride {
			if arg == "" {
				return DshHarnessConfig{}, fmt.Errorf("DSH launch_args_override must contain non-empty strings")
			}
		}
	}
	return values, nil
}

// DshHarnessConfigFromMapping 校验 provider SPI 映射而不启动 DSH 运行时。
// Python: config.py:117-140 from_mapping
func DshHarnessConfigFromMapping(config harnessprotocol.JsonObject) (DshHarnessConfig, error) {
	values := make(map[string]any, len(config))
	maps.Copy(values, config)
	known := map[string]bool{
		"provider": true, "model": true, "reasoning_effort": true, "max_tokens": true,
		"system_prompt_mode": true, "skills": true, "skill_conflict": true,
		"cwd": true, "runtime_cwd": true, "dsh_bin": true, "dsh_home": true,
		"profile": true, "patches": true, "env": true,
		"launch_args_override": true, "initialize_timeout_seconds": true,
		"request_timeout_seconds": true, "shutdown_timeout_seconds": true,
		"base_url": true, "api_key": true, "system_prompt_env_var": true,
		"event_buffer_capacity": true,
		// spawn 公共键（external/cli_agent/spawn.py:936-942 契约：每个 provider
		// 包共享 cwd/env/inherit_process_env/skills/skill_conflict，另加可选
		// cli_path）：dsh 配置面没有对应字段，容忍并忽略。偏差 D-15.13-l——
		// Python from_mapping 对未知键同样报错且 spawn 同样传入这两个键，上游
		// spawn→dsh 组合从未被真实执行（上游 e2e 只直调 from_mapping），
		// Go 以容忍承载 spawn 文档声明的共享契约。回收条件见 spec §5。
		"inherit_process_env": true,
		"cli_path":            true,
	}
	unknown := slices.Sorted(func(yield func(string) bool) {
		for key := range values {
			if !known[key] {
				yield(key)
			}
		}
	})
	if len(unknown) > 0 {
		return DshHarnessConfig{}, fmt.Errorf("unknown DSH configuration fields: %s", strings.Join(unknown, ", "))
	}

	var result DshHarnessConfig
	// 标量字段：JSON round-trip 后逐字段取出（数字统一 float64，依
	// YAML/JSON 解码惯例做类型分流——参考 2026-10-28 审查教训）
	if v, ok := values["provider"].(string); ok {
		result.Provider = v
	}
	if v, ok := values["model"].(string); ok {
		result.Model = v
	}
	if v, ok := values["reasoning_effort"].(string); ok {
		result.ReasoningEffort = v
	}
	if v, ok := values["max_tokens"].(float64); ok && v > 0 {
		result.MaxTokens = int(v)
	}
	if v, ok := values["system_prompt_mode"].(string); ok {
		result.SystemPromptMode = v
	}
	if v, ok := values["skill_conflict"].(string); ok {
		result.SkillConflict = v
	}
	for name, field := range map[string]*string{
		"cwd":                   &result.Cwd,
		"runtime_cwd":           &result.RuntimeCwd,
		"dsh_bin":               &result.DshBin,
		"dsh_home":              &result.DshHome,
		"profile":               &result.Profile,
		"base_url":              &result.BaseURL,
		"api_key":               &result.APIKey,
		"system_prompt_env_var": &result.SystemPromptEnvVar,
	} {
		if v, ok := values[name].(string); ok {
			*field = v
		}
	}
	if v, ok := values["initialize_timeout_seconds"].(float64); ok {
		result.InitializeTimeoutSeconds = v
	}
	if v, ok := values["request_timeout_seconds"].(float64); ok {
		result.RequestTimeoutSeconds = v
	}
	if v, ok := values["shutdown_timeout_seconds"].(float64); ok {
		result.ShutdownTimeoutSeconds = v
	}
	if v, ok := values["event_buffer_capacity"].(float64); ok && v > 0 {
		result.EventBufferCapacity = int(v)
	}

	// Python: config.py:127-131（env 转 dict）
	if raw, ok := values["env"].(map[string]any); ok {
		env := make(map[string]string, len(raw))
		for key, value := range raw {
			strValue, isStr := value.(string)
			if !isStr {
				return DshHarnessConfig{}, fmt.Errorf("DSH env must map strings to strings")
			}
			env[key] = strValue
		}
		result.Env = env
	}

	// D-15.12-g 收窄落点：skills 反序列化出原始 dict/str 后在此转换
	// （Python normalize_skills 的 str/Mapping 分支由 unmarshal 层承担）
	if raw, ok := values["skills"].([]any); ok {
		sources := make([]providers.SkillSource, 0, len(raw))
		for _, item := range raw {
			switch typed := item.(type) {
			case string:
				source, err := providers.NewSkillSource(typed, "all", nil)
				if err != nil {
					return DshHarnessConfig{}, err
				}
				sources = append(sources, source)
			case map[string]any:
				dir, _ := typed["dir"].(string)
				mode, _ := typed["mode"].(string)
				if mode == "" {
					mode = "all"
				}
				var enabled []string
				if rawEnabled, ok := typed["enabled_skills"].([]any); ok {
					enabled = make([]string, 0, len(rawEnabled))
					for _, name := range rawEnabled {
						enabled = append(enabled, fmt.Sprint(name))
					}
				}
				source, err := providers.NewSkillSource(dir, mode, enabled)
				if err != nil {
					return DshHarnessConfig{}, err
				}
				sources = append(sources, source)
			default:
				return DshHarnessConfig{}, fmt.Errorf("skill source must be a directory or manifest skill declaration")
			}
		}
		result.Skills = sources
	}

	// Python: config.py:133-138（两个数组字段转 tuple）
	for name, target := range map[string]*[]string{
		"patches":              &result.Patches,
		"launch_args_override": &result.LaunchArgsOverride,
	} {
		raw, ok := values[name].([]any)
		if !ok {
			continue
		}
		items := make([]string, 0, len(raw))
		for _, item := range raw {
			items = append(items, fmt.Sprint(item))
		}
		*target = items
	}

	return NewDshHarnessConfig(result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalJSONValue 紧凑 JSON 序列化（供 toDshInput 兜底分支复用）。
func marshalJSONValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
