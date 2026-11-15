package dsh

import (
	"fmt"
	"sort"
	"strings"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DshHarnessConfig 传给 deepseek_harness.DeepSeekHarness 的 provider 自有配置。
// Python: config.py:16-51 DshHarnessConfig（frozen dataclass）；字段名与
// DeepSeekHarnessConfig 镜像。可选 SDK 依赖刻意不在此导入——Go 侧 SDK 编译期
// 内嵌恒存在（偏差 D-dsh-2），缺依赖分支不可达；错误面由 sdkFactory 注入
// fake 覆盖。dsh_home（或 env 中非空 DSH_HOME）对 SDK 运行时是必需的，
// SDK 不会隐式回退 ~/.dsh。自定义 Cordis composition 必须消费
// system_prompt_env_var；否则适配器经临时 launch overlay 使用内建系统提示词插件。
type DshHarnessConfig struct {
	// Provider 模型提供方标识（Python: provider，默认 "deepseek-official"）
	Provider string
	// Model 模型名（Python: model，默认 "deepseek-v4-flash"）
	Model string
	// ReasoningEffort 推理力度（Python: reasoning_effort，可空）
	ReasoningEffort string
	// MaxTokens 输出上限；0 表示不设（Python: max_tokens）
	MaxTokens int
	// SystemPromptMode replace（只换原生前缀）或 append（独立 section）
	// Python: system_prompt_mode
	SystemPromptMode string
	// Skills portable 技能源（构造期经 NormalizeSkills 冻结；Python: skills）
	Skills []hp.SkillSource
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

// DefaultDshHarnessConfig 返回 Python dataclass 字段缺省值。
// Python: config.py:29-51 DshHarnessConfig() 的 dataclass defaults
func DefaultDshHarnessConfig() DshHarnessConfig {
	config, err := NewDshHarnessConfig(DshHarnessConfig{
		Provider: "deepseek-official",
		Model:    "deepseek-v4-flash",
	})
	if err != nil {
		// 缺省值恒满足校验；不可达分支防御
		panic(fmt.Sprintf("dsh: default config must validate: %v", err))
	}
	return config
}

// NewDshHarnessConfig 构造并执行 __post_init__ 等价校验，校验顺序与 Python 逐条一致。
// Python: config.py:53-115 __post_init__
// D-15.12-g 落点：skills 在此构造期经 NormalizeSkills 完成校验（Python 统一
// 校验点收窄后的对应物，勿漏）；dict/str → SkillSource 的转换在本包
// DshHarnessConfigFromMapping 的 unmarshal 层完成。
func NewDshHarnessConfig(values DshHarnessConfig) (DshHarnessConfig, error) {
	// Python: dataclass 字段默认值（config.py:29-51）——Go 零值在此补齐。
	// 注意 provider/model 不补：Python 中显式传空串可构造且 __post_init__ 报错
	// （config.py:61-62），缺省仅由 dataclass 默认（DefaultDshHarnessConfig /
	// FromMapping 的 key 缺失分支）提供。
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

	// Python: __post_init__ 首行 normalize_skills(self.skills, self.skill_conflict)
	normalized, err := hp.NormalizeSkills(values.Skills, values.SkillConflict)
	if err != nil {
		return DshHarnessConfig{}, err
	}
	values.Skills = normalized

	// Python: config.py:55-58 system_prompt_mode 与 env_var 互斥、mode 白名单
	if values.SystemPromptMode == "append" && values.SystemPromptEnvVar != "" {
		return DshHarnessConfig{}, fmt.Errorf("append mode cannot use system_prompt_env_var")
	}
	if values.SystemPromptMode != "append" && values.SystemPromptMode != "replace" {
		return DshHarnessConfig{}, fmt.Errorf("system_prompt_mode must be 'append' or 'replace'")
	}
	// Python: config.py:59-62 provider/model 必须是非空字符串
	// （Go 类型系统已保证是 string，保留非空语义）
	if values.Provider == "" || values.Model == "" {
		return DshHarnessConfig{}, fmt.Errorf("DSH provider and model must not be empty")
	}
	// Python: config.py:63-77 optional 字符串字段：Go 类型系统已保证是 string，
	// 类型分支不适用（登记偏差，无回收条件——语言层差异）
	_ = []string{
		"reasoning_effort", "cwd", "runtime_cwd", "dsh_bin", "dsh_home", "profile",
		"base_url", "api_key", "system_prompt_env_var",
	}
	// Python: config.py:78-82 max_tokens 正整数
	if values.MaxTokens <= 0 && values.MaxTokens != 0 {
		return DshHarnessConfig{}, fmt.Errorf("DSH max_tokens must be positive")
	}
	// Python: config.py:83-86 event_buffer_capacity 正整数
	if values.EventBufferCapacity <= 0 {
		return DshHarnessConfig{}, fmt.Errorf("DSH event_buffer_capacity must be positive")
	}
	// Python: config.py:87-96 三个 timeout 数值且为正
	for _, item := range []struct {
		name    string
		timeout float64
	}{
		{"initialize_timeout_seconds", values.InitializeTimeoutSeconds},
		{"request_timeout_seconds", values.RequestTimeoutSeconds},
		{"shutdown_timeout_seconds", values.ShutdownTimeoutSeconds},
	} {
		if item.timeout != 0 && item.timeout <= 0 {
			return DshHarnessConfig{}, fmt.Errorf("DSH %s must be positive when provided", item.name)
		}
	}
	// Python: config.py:97-100 dsh_home/profile/system_prompt_env_var 非空白
	for _, item := range []struct {
		name  string
		value string
	}{
		{"dsh_home", values.DshHome},
		{"profile", values.Profile},
		{"system_prompt_env_var", values.SystemPromptEnvVar},
	} {
		if item.value != "" && strings.TrimSpace(item.value) == "" {
			return DshHarnessConfig{}, fmt.Errorf("DSH %s must not be empty", item.name)
		}
	}
	// Python: config.py:101-104 env 必须 string→string（Go 类型系统已保证元素类型）
	// Python: config.py:105-106 patches 必须是字符串数组（同上，类型由签名保证）
	// Python: config.py:107-113 launch_args_override 数组且元素非空
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
// Python: config.py:117-140 DshHarnessConfig.from_mapping
func DshHarnessConfigFromMapping(config harnessprotocol.JsonObject) (DshHarnessConfig, error) {
	values := DshHarnessConfig{}

	// Python: config.py:122-125 未知字段 sorted 报错
	known := map[string]bool{
		"provider": true, "model": true, "reasoning_effort": true, "max_tokens": true,
		"system_prompt_mode": true, "skills": true, "skill_conflict": true,
		"cwd": true, "runtime_cwd": true, "dsh_bin": true, "dsh_home": true,
		"profile": true, "patches": true, "env": true,
		"launch_args_override": true, "initialize_timeout_seconds": true,
		"request_timeout_seconds": true, "shutdown_timeout_seconds": true,
		"base_url": true, "api_key": true, "system_prompt_env_var": true,
		"event_buffer_capacity": true,
	}
	unknown := make([]string, 0, len(config))
	for key := range config {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return DshHarnessConfig{}, fmt.Errorf("unknown DSH configuration fields: %s", strings.Join(unknown, ", "))
	}

	// Python: config.py:127-131 env dict → dict
	if raw, ok := config["env"]; ok && raw != nil {
		envMap, ok := raw.(map[string]any)
		if !ok {
			return DshHarnessConfig{}, fmt.Errorf("DSH env must be an object")
		}
		values.Env = make(map[string]string, len(envMap))
		for key, value := range envMap {
			str, ok := value.(string)
			if !ok {
				return DshHarnessConfig{}, fmt.Errorf("DSH env must map strings to strings")
			}
			values.Env[key] = str
		}
	}
	// Python: config.py:133-138 launch_args_override/patches 数组 → tuple
	if raw, ok := config["launch_args_override"]; ok && raw != nil {
		items, err := stringSliceOf(raw, "launch_args_override")
		if err != nil {
			return DshHarnessConfig{}, err
		}
		values.LaunchArgsOverride = items
	}
	if raw, ok := config["patches"]; ok && raw != nil {
		items, err := stringSliceOf(raw, "patches")
		if err != nil {
			return DshHarnessConfig{}, err
		}
		values.Patches = items
	}
	// skills dict/str → SkillSource 的转换在本 unmarshal 层完成（D-15.12-g）
	if raw, ok := config["skills"]; ok && raw != nil {
		sourceList, ok := raw.([]any)
		if !ok {
			return DshHarnessConfig{}, fmt.Errorf("DSH skills must be an array")
		}
		values.Skills = make([]hp.SkillSource, 0, len(sourceList))
		for _, item := range sourceList {
			source, err := skillSourceFromMapping(item)
			if err != nil {
				return DshHarnessConfig{}, err
			}
			values.Skills = append(values.Skills, *source)
		}
	}

	// 其余标量字段逐个解出（Python 由 **values 构造一步完成；Go 逐字段显式）
	scalars := []struct {
		key    string
		target *string
	}{
		{"provider", &values.Provider}, {"model", &values.Model},
		{"reasoning_effort", &values.ReasoningEffort},
		{"system_prompt_mode", &values.SystemPromptMode},
		{"skill_conflict", &values.SkillConflict},
		{"cwd", &values.Cwd}, {"runtime_cwd", &values.RuntimeCwd},
		{"dsh_bin", &values.DshBin}, {"dsh_home", &values.DshHome},
		{"profile", &values.Profile}, {"base_url", &values.BaseURL},
		{"api_key", &values.APIKey}, {"system_prompt_env_var", &values.SystemPromptEnvVar},
	}
	for _, item := range scalars {
		if raw, ok := config[item.key]; ok && raw != nil {
			str, ok := raw.(string)
			if !ok {
				return DshHarnessConfig{}, fmt.Errorf("DSH %s must be a string when provided", item.key)
			}
			*item.target = str
		}
	}
	ints := []struct {
		key    string
		target *int
	}{
		{"max_tokens", &values.MaxTokens}, {"event_buffer_capacity", &values.EventBufferCapacity},
	}
	for _, item := range ints {
		if raw, ok := config[item.key]; ok && raw != nil {
			num, err := intOf(raw)
			if err != nil {
				return DshHarnessConfig{}, fmt.Errorf("DSH %s must be an integer when provided", item.key)
			}
			*item.target = num
		}
	}
	floats := []struct {
		key    string
		target *float64
	}{
		{"initialize_timeout_seconds", &values.InitializeTimeoutSeconds},
		{"request_timeout_seconds", &values.RequestTimeoutSeconds},
		{"shutdown_timeout_seconds", &values.ShutdownTimeoutSeconds},
	}
	for _, item := range floats {
		if raw, ok := config[item.key]; ok && raw != nil {
			num, ok := raw.(float64)
			if !ok {
				return DshHarnessConfig{}, fmt.Errorf("DSH %s must be numeric when provided", item.key)
			}
			*item.target = num
		}
	}
	// Python: from_mapping 缺 key 时由 dataclass 默认值补齐（config.py:29-51）
	if _, ok := config["provider"]; !ok {
		values.Provider = "deepseek-official"
	}
	if _, ok := config["model"]; !ok {
		values.Model = "deepseek-v4-flash"
	}
	return NewDshHarnessConfig(values)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// skillSourceFromMapping 把 SPI 映射里的单个 skill 源转成 SkillSource。
// D-15.12-g 收窄后的落点：dict/str → SkillSource 的转换由 provider config
// unmarshal 层完成（Python skills.py:43-59 三形态统一收口处）。
func skillSourceFromMapping(raw any) (*hp.SkillSource, error) {
	mapping, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("skill source must be an object with a dir")
	}
	dir, _ := mapping["dir"].(string)
	mode, _ := mapping["mode"].(string)
	if mode == "" {
		// Python: skills.py:28 dataclass 字段默认 mode="all"
		mode = "all"
	}
	var enabled []string
	if rawEnabled, ok := mapping["enabled_skills"]; ok && rawEnabled != nil {
		items, err := stringSliceOf(rawEnabled, "enabled_skills")
		if err != nil {
			return nil, err
		}
		enabled = items
	}
	source, err := hp.NewSkillSource(dir, mode, enabled)
	if err != nil {
		return nil, err
	}
	return &source, nil
}

// stringSliceOf 解出字符串数组字段。
// Python: config.py:133-138 values[name] = tuple(items)
func stringSliceOf(raw any, name string) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("DSH %s must be an array", name)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("DSH %s must be an array of strings", name)
		}
		out = append(out, str)
	}
	return out, nil
}

// intOf 解出整数字段（JSON 数字经 YAML/JSON 反序列化为 float64，需转 int）。
func intOf(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	default:
		return 0, fmt.Errorf("not an integer")
	}
}
