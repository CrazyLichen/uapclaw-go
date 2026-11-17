package dsh

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	providers "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// 缺省值：Python config.py:29-51 字段默认（test_harness.py:197 前置语境）。
func TestNewDshHarnessConfig_缺省值(t *testing.T) {
	cfg, err := NewDshHarnessConfig(DshHarnessConfig{})
	require.NoError(t, err)
	assert.Equal(t, "deepseek-official", cfg.Provider)
	assert.Equal(t, "deepseek-v4-flash", cfg.Model)
	assert.Equal(t, "replace", cfg.SystemPromptMode)
	assert.Equal(t, "skip", cfg.SkillConflict)
	assert.Equal(t, 1.0, cfg.ShutdownTimeoutSeconds)
	assert.Equal(t, 1024, cfg.EventBufferCapacity)
	assert.Empty(t, cfg.Skills)
}

// 构造期 skills 归一：D-15.12-g 收窄落点（Python __post_init__ 首行 +
// skills.py:43-58 normalize_skills）。
func TestNewDshHarnessConfig_skills归一(t *testing.T) {
	cfg, err := NewDshHarnessConfig(DshHarnessConfig{
		Skills: []providers.SkillSource{
			mustSkillSource(t, "/tmp/skills-lib", "all", nil),
		},
		SkillConflict: "replace",
	})
	require.NoError(t, err)
	require.Len(t, cfg.Skills, 1)
	assert.Equal(t, "all", cfg.Skills[0].Mode)
}

// 校验拒绝：Python config.py:55-115 __post_init__ 每个分支，错误文本逐字一致。
func TestNewDshHarnessConfig_校验拒绝(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*DshHarnessConfig)
		want string
	}{
		{"append与env_var互斥", func(c *DshHarnessConfig) {
			c.SystemPromptMode = "append"
			c.SystemPromptEnvVar = "DSH_PROMPT"
		}, "append mode cannot use system_prompt_env_var"},
		{"mode非法", func(c *DshHarnessConfig) {
			c.SystemPromptMode = "prepend"
		}, "system_prompt_mode must be 'append' or 'replace'"},
		// provider/model 显式空串在 Go 与「未提供」不可区分（零值回落默认值），
		// Python config.py:61-62 的 ValueError 分支不可表达——登记为
		// D-dsh-5 同款零值语义（filter-None 等价物），行为只减不减。
		{"max_tokens非正", func(c *DshHarnessConfig) {
			c.MaxTokens = -1
		}, "DSH max_tokens must be positive"},
		{"buffer非正", func(c *DshHarnessConfig) {
			c.EventBufferCapacity = -5 // 0 = 未提供回落默认 1024，负数才显式非法
		}, "DSH event_buffer_capacity must be positive"},
		{"initialize_timeout非正", func(c *DshHarnessConfig) {
			c.InitializeTimeoutSeconds = -0.5
		}, "DSH initialize_timeout_seconds must be positive when provided"},
		{"request_timeout非正", func(c *DshHarnessConfig) {
			c.RequestTimeoutSeconds = -2.0
		}, "DSH request_timeout_seconds must be positive when provided"},
		{"shutdown_timeout非正", func(c *DshHarnessConfig) {
			c.ShutdownTimeoutSeconds = -1
		}, "DSH shutdown_timeout_seconds must be positive when provided"},
		{"dsh_home空白", func(c *DshHarnessConfig) {
			c.DshHome = "   "
		}, "DSH dsh_home must not be empty"},
		{"profile空白", func(c *DshHarnessConfig) {
			c.Profile = " "
		}, "DSH profile must not be empty"},
		{"system_prompt_env_var空白", func(c *DshHarnessConfig) {
			c.SystemPromptEnvVar = "\t"
		}, "DSH system_prompt_env_var must not be empty"},
		{"skill_conflict非法", func(c *DshHarnessConfig) {
			c.SkillConflict = "overwrite"
		}, "skill_conflict must be skip, replace or append"},
		{"launch_args空数组", func(c *DshHarnessConfig) {
			c.LaunchArgsOverride = []string{}
		}, "DSH launch_args_override must contain non-empty strings"},
		{"launch_args含空元素", func(c *DshHarnessConfig) {
			c.LaunchArgsOverride = []string{"--profile", ""}
		}, "DSH launch_args_override must contain non-empty strings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DshHarnessConfig{}
			tc.mut(&cfg)
			_, err := NewDshHarnessConfig(cfg)
			require.Error(t, err)
			if tc.want != "" {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

// 未知字段报错：Python config.py:118-125 from_mapping。
func TestDshHarnessConfigFromMapping_未知字段报错(t *testing.T) {
	_, err := DshHarnessConfigFromMapping(map[string]any{
		"provider": "deepseek-official",
		"nope":     1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown DSH configuration fields: nope")
}

// spawn 公共键容忍：external/cli_agent/spawn.py:936-942 契约的 cwd/env/
// inherit_process_env/skills/skill_conflict 与可选 cli_path 全量传入时构造
// 成功；dsh 无对应字段的 inherit_process_env/cli_path 被忽略。
// 偏差 D-15.13-l（spec §5）：Python from_mapping 同样拒未知键且上游 spawn→dsh
// 组合从未被真实执行，Go 以容忍承载 spawn 文档声明的共享契约。
func TestDshHarnessConfigFromMapping_spawn公共键容忍(t *testing.T) {
	cfg, err := DshHarnessConfigFromMapping(map[string]any{
		"provider":            "deepseek-official",
		"model":               "deepseek-v4-flash",
		"cwd":                 "/w",
		"env":                 map[string]any{"K": "V"},
		"inherit_process_env": false,
		"skills":              []any{},
		"skill_conflict":      "skip",
		"cli_path":            "/opt/dsh",
	})
	require.NoError(t, err)
	assert.Equal(t, "/w", cfg.Cwd)
	assert.Equal(t, map[string]string{"K": "V"}, cfg.Env)
	// 被忽略的键不得漏进配置面（dsh 配置无此二者字段）
	assert.Equal(t, "skip", cfg.SkillConflict)
}

// 集合字段转换：Python config.py:127-140（env dict、两个数组字段转 tuple）。
func TestDshHarnessConfigFromMapping_集合字段转换(t *testing.T) {
	cfg, err := DshHarnessConfigFromMapping(map[string]any{
		"provider":             "deepseek-official",
		"model":                "deepseek-v4-flash",
		"env":                  map[string]any{"K": "V"},
		"patches":              []any{"/a.yml", "/b.yml"},
		"launch_args_override": []any{"--x"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"K": "V"}, cfg.Env)
	assert.Equal(t, []string{"/a.yml", "/b.yml"}, cfg.Patches)
	assert.Equal(t, []string{"--x"}, cfg.LaunchArgsOverride)
}

// mustSkillSource 构造一个已校验的 SkillSource 测试助手。
func mustSkillSource(t *testing.T, dir, mode string, enabled []string) providers.SkillSource {
	t.Helper()
	src, err := providers.NewSkillSource(dir, mode, enabled)
	require.NoError(t, err)
	return src
}
