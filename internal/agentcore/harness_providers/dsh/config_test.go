package dsh

import (
	"strings"
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游对齐：tests/unit_tests/harness_providers/dsh/test_harness.py:197-211
// test_config_rejects_values_that_the_sdk_cannot_accept
func TestNewDshHarnessConfig_缺省值(t *testing.T) {
	config, err := NewDshHarnessConfig(DefaultDshHarnessConfig())
	require.NoError(t, err)
	assert.Equal(t, "deepseek-official", config.Provider)
	assert.Equal(t, "deepseek-v4-flash", config.Model)
	assert.Equal(t, "replace", config.SystemPromptMode)
	assert.Equal(t, "skip", config.SkillConflict)
	assert.Equal(t, 1.0, config.ShutdownTimeoutSeconds)
	assert.Equal(t, 1024, config.EventBufferCapacity)
	assert.Empty(t, config.Skills)
}

// D-15.12-g 落点：skills 构造期经 NormalizeSkills 校验。
func TestNewDshHarnessConfig_skills归一(t *testing.T) {
	config, err := NewDshHarnessConfig(DshHarnessConfig{
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
		Skills: []hp.SkillSource{{Dir: "/tmp/skills", Mode: "all"}},
	})
	require.NoError(t, err)
	require.Len(t, config.Skills, 1)
	assert.Equal(t, "all", config.Skills[0].Mode)

	// 非法 conflict 在 skills 归一步即拒绝（Python __post_init__ 首行委托）
	_, err = NewDshHarnessConfig(DshHarnessConfig{
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
		SkillConflict: "bogus",
	})
	assert.ErrorContains(t, err, "skill_conflict must be skip, replace or append")
}

// 表驱动覆盖 __post_init__ 全部分支（config.py:53-115），错误文本逐字一致。
func TestNewDshHarnessConfig_校验拒绝(t *testing.T) {
	cases := []struct {
		name string
		// mutate 以 DefaultDshHarnessConfig 为基座做字段覆盖（对齐 Python
		// dataclass 默认先于 __post_init__ 校验的构造语义）
		mutate func(base *DshHarnessConfig)
		want   string
	}{
		{"append与env_var互斥", func(b *DshHarnessConfig) { b.SystemPromptMode, b.SystemPromptEnvVar = "append", "X" }, "append mode cannot use system_prompt_env_var"},
		{"mode非法", func(b *DshHarnessConfig) { b.SystemPromptMode = "bogus" }, "system_prompt_mode must be 'append' or 'replace'"},
		{"provider为空", func(b *DshHarnessConfig) { b.Provider = "" }, "DSH provider and model must not be empty"},
		{"model为空", func(b *DshHarnessConfig) { b.Model = "" }, "DSH provider and model must not be empty"},
		{"max_tokens非正", func(b *DshHarnessConfig) { b.MaxTokens = -1 }, "DSH max_tokens must be positive"},
		{"buffer非正", func(b *DshHarnessConfig) { b.EventBufferCapacity = -3 }, "DSH event_buffer_capacity must be positive"},
		{"init_timeout非正", func(b *DshHarnessConfig) { b.InitializeTimeoutSeconds = -1 }, "DSH initialize_timeout_seconds must be positive when provided"},
		{"request_timeout非正", func(b *DshHarnessConfig) { b.RequestTimeoutSeconds = -2 }, "must be positive when provided"},
		{"shutdown_timeout非正", func(b *DshHarnessConfig) { b.ShutdownTimeoutSeconds = -1 }, "must be positive when provided"},
		{"dsh_home空白", func(b *DshHarnessConfig) { b.DshHome = "   " }, "DSH dsh_home must not be empty"},
		{"profile空白", func(b *DshHarnessConfig) { b.Profile = " " }, "DSH profile must not be empty"},
		{"system_prompt_env_var空白", func(b *DshHarnessConfig) { b.SystemPromptEnvVar = "\t" }, "DSH system_prompt_env_var must not be empty"},
		{"launch_args空数组", func(b *DshHarnessConfig) { b.LaunchArgsOverride = []string{} }, "DSH launch_args_override must contain non-empty strings"},
		{"launch_args空元素", func(b *DshHarnessConfig) { b.LaunchArgsOverride = []string{"--x", ""} }, "DSH launch_args_override must contain non-empty strings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := DefaultDshHarnessConfig()
			tc.mutate(&base)
			_, err := NewDshHarnessConfig(base)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// Python: config.py:117-140 DshHarnessConfig.from_mapping
func TestDshHarnessConfigFromMapping(t *testing.T) {
	// 未知字段 sorted 报错
	_, err := DshHarnessConfigFromMapping(harnessprotocol.JsonObject{
		"zzz": 1, "aaa": 2, "model": "deepseek-v4-flash",
	})
	require.Error(t, err)
	assert.Equal(t, "unknown DSH configuration fields: aaa, zzz", err.Error())

	// 合法映射：env 与数组字段转换
	config, err := DshHarnessConfigFromMapping(harnessprotocol.JsonObject{
		"model":                "deepseek-v4-flash",
		"env":                  map[string]any{"DSH_HOME": "/d"},
		"patches":              []any{"p.yml"},
		"launch_args_override": []any{"--profile", "sdk"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DSH_HOME": "/d"}, config.Env)
	assert.Equal(t, []string{"p.yml"}, config.Patches)
	assert.Equal(t, []string{"--profile", "sdk"}, config.LaunchArgsOverride)

	// skills dict 形态在 FromMapping 层转 SkillSource（D-15.12-g：unmarshal 层转换）
	config, err = DshHarnessConfigFromMapping(harnessprotocol.JsonObject{
		"skills": []any{map[string]any{"dir": "/tmp/skills", "mode": "auto_list"}},
	})
	require.NoError(t, err)
	require.Len(t, config.Skills, 1)
	assert.Equal(t, "/tmp/skills", config.Skills[0].Dir)
	assert.Equal(t, "auto_list", config.Skills[0].Mode)
}

func TestDshHarnessConfigFromMapping_skills非法形态(t *testing.T) {
	_, err := DshHarnessConfigFromMapping(harnessprotocol.JsonObject{
		"skills": []any{map[string]any{"dir": ""}},
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "skill dir must be a non-empty path"), err.Error())
}
