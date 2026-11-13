package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件对齐 tests 里 load_settings_json / load_config 的行为。

// writeSettings 写一个 settings 文件并返回路径。
func writeSettings(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// ──────────────────────────── settings.json ────────────────────────────

// TestLoadSettingsJSON_正常 对齐 config.py:22-44。
func TestLoadSettingsJSON_正常(t *testing.T) {
	path := writeSettings(t, t.TempDir(), `{"model": "m1", "apiKey": "k1"}`)
	settings := LoadSettingsJSON(path)
	assert.Equal(t, "m1", settings["model"])
	assert.Equal(t, "k1", settings["apiKey"])
}

// TestLoadSettingsJSON_缺失返回空 文件不存在 → {}。
func TestLoadSettingsJSON_缺失返回空(t *testing.T) {
	assert.Empty(t, LoadSettingsJSON(filepath.Join(t.TempDir(), "nope.json")))
}

// TestLoadSettingsJSON_非法返回空 坏 JSON 或顶层非对象 → {}。
func TestLoadSettingsJSON_非法返回空(t *testing.T) {
	dir := t.TempDir()
	path := writeSettings(t, dir, "{not json")
	assert.Empty(t, LoadSettingsJSON(path))
	path = writeSettings(t, dir, "[1,2]")
	assert.Empty(t, LoadSettingsJSON(path))
}

// TestSaveSettingsJSON_合并且尾随换行 对齐 config.py:47-69。
func TestSaveSettingsJSON_合并且尾随换行(t *testing.T) {
	dir := t.TempDir()
	path := writeSettings(t, dir, `{"model": "m1", "keep": true}`)

	resolved, err := SaveSettingsJSON(map[string]any{"model": "m2"}, path)
	require.NoError(t, err)
	assert.Equal(t, path, resolved)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(content)
	assert.Contains(t, text, `"model": "m2"`) // 新键覆盖
	assert.Contains(t, text, `"keep": true`)  // 旧键保留
	assert.True(t, text[len(text)-1] == '\n', "尾随换行（Python: + '\\n'）")
}

// TestSaveSettingsJSON_自动建目录 目标目录不存在时创建。
func TestSaveSettingsJSON_自动建目录(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "deep")
	_, err := SaveSettingsJSON(map[string]any{"k": "v"}, filepath.Join(nested, "settings.json"))
	require.NoError(t, err)
	assert.NotEmpty(t, LoadSettingsJSON(filepath.Join(nested, "settings.json")))
}

// ──────────────────────────── CLIConfig.Validate ────────────────────────────

// TestCLIConfig_Validate 对齐 config.py:90-110。
func TestCLIConfig_Validate(t *testing.T) {
	t.Run("本地模式缺 key 报错", func(t *testing.T) {
		cfg := CLIConfig{MaxTokens: 8192, MaxIterations: 30}
		require.ErrorContains(t, cfg.Validate(), "API key not set")
	})
	t.Run("远程模式免 key", func(t *testing.T) {
		cfg := CLIConfig{ServerURL: "http://x", MaxTokens: 8192, MaxIterations: 30}
		assert.NoError(t, cfg.Validate())
	})
	t.Run("max_tokens 过小", func(t *testing.T) {
		cfg := CLIConfig{APIKey: "k", MaxTokens: 100, MaxIterations: 30}
		require.ErrorContains(t, cfg.Validate(), "dangerously small")
	})
	t.Run("max_iterations 为 0", func(t *testing.T) {
		cfg := CLIConfig{APIKey: "k", MaxTokens: 8192, MaxIterations: 0}
		require.ErrorContains(t, cfg.Validate(), "must be >= 1")
	})
}

// ──────────────────────────── intSetting ────────────────────────────

// TestIntSetting_各形态 对齐 config.py:112-122。
func TestIntSetting_各形态(t *testing.T) {
	n, err := intSetting(30, "max_iterations")
	require.NoError(t, err)
	assert.Equal(t, 30, n)
	n, err = intSetting("42", "max_iterations")
	require.NoError(t, err)
	assert.Equal(t, 42, n)
	n, err = intSetting(8192.0, "max_tokens")
	require.NoError(t, err)
	assert.Equal(t, 8192, n)
	_, err = intSetting(map[string]any{}, "max_tokens")
	require.ErrorContains(t, err, "must be an integer")
	_, err = intSetting("abc", "max_tokens")
	require.ErrorContains(t, err, "must be an integer")
}

// ──────────────────────────── LoadConfig 三层合并 ────────────────────────────

// setupEnv 测试期清理环境变量。
func setupEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENJIUWEN_PROVIDER", "OPENJIUWEN_MODEL", "OPENJIUWEN_API_KEY",
		"OPENJIUWEN_API_BASE", "OPENJIUWEN_SERVER_URL", "OPENJIUWEN_WORKSPACE",
		"OPENJIUWEN_MAX_ITERATIONS", "OPENJIUWEN_MAX_TOKENS",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

// TestLoadConfig_默认值 全空时的默认链。
func TestLoadConfig_默认值(t *testing.T) {
	setupEnv(t)
	cfg, err := LoadConfig(LoadConfigOptions{APIKey: "k"})
	require.NoError(t, err)
	assert.Equal(t, "OpenAI", cfg.Provider)
	assert.Equal(t, "gpt-4o", cfg.Model)
	assert.Equal(t, "https://api.openai.com/v1", cfg.APIBase)
	assert.Equal(t, 30, cfg.MaxIterations)
	assert.Equal(t, 8192, cfg.MaxTokens)
	assert.Equal(t, DefaultWorkspace(), cfg.Workspace)
}

// TestLoadConfig_三层优先级 CLI > env > settings。
func TestLoadConfig_三层优先级(t *testing.T) {
	setupEnv(t)
	dir := t.TempDir()
	// 用 HOME 指向临时目录，让默认 settings 路径命中测试文件
	t.Setenv("HOME", dir)
	settingsDir := filepath.Join(dir, ".openjiuwen")
	require.NoError(t, os.MkdirAll(settingsDir, 0o755))
	settingsPath := filepath.Join(settingsDir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath,
		[]byte(`{"model":"settings-model","provider":"settings-provider"}`), 0o644))
	t.Setenv("OPENJIUWEN_MODEL", "env-model")

	cfg, err := LoadConfig(LoadConfigOptions{APIKey: "k"})
	require.NoError(t, err)
	assert.Equal(t, "env-model", cfg.Model, "env > settings")
	assert.Equal(t, "settings-provider", cfg.Provider, "settings 兜底")

	// CLI 参数最高
	cfg, err = LoadConfig(LoadConfigOptions{APIKey: "k", Model: "cli-model"})
	require.NoError(t, err)
	assert.Equal(t, "cli-model", cfg.Model, "CLI > env")
}

// TestLoadConfig_校验失败传播 api_key 缺失时整体报错。
func TestLoadConfig_校验失败传播(t *testing.T) {
	setupEnv(t)
	_, err := LoadConfig(LoadConfigOptions{})
	require.ErrorContains(t, err, "API key not set")
}

// TestSettingsPath_波浪号 ~ 展开（~ / ~/sub / 原样）。
func TestSettingsPath_波浪号(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := SettingsPath("~")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	got, err = SettingsPath("~/sub/settings.json")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "sub/settings.json"), got)

	got, err = SettingsPath("/abs/settings.json")
	require.NoError(t, err)
	assert.Equal(t, "/abs/settings.json", got)

	got, err = SettingsPath("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".openjiuwen", "settings.json"), got)
}

// TestLoadConfig_显式workspace覆盖 与 cwd 填充。
func TestLoadConfig_显式workspace覆盖(t *testing.T) {
	setupEnv(t)
	cfg, err := LoadConfig(LoadConfigOptions{APIKey: "k", Workspace: "/tmp/custom-ws"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom-ws", cfg.Workspace)
	assert.NotEmpty(t, cfg.Cwd, "cwd 运行时填充")
	assert.False(t, cfg.Verbose)

	cfg, err = LoadConfig(LoadConfigOptions{APIKey: "k", Verbose: true})
	require.NoError(t, err)
	assert.True(t, cfg.Verbose)
}

// TestLoadConfig_坏类型int传播 settings 里数字位置放对象时报错。
func TestLoadConfig_坏类型int传播(t *testing.T) {
	setupEnv(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	settingsDir := filepath.Join(dir, ".openjiuwen")
	require.NoError(t, os.MkdirAll(settingsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(settingsDir, "settings.json"),
		[]byte(`{"maxTokens": {"bad": true}, "apiKey": "k"}`), 0o644))

	_, err := LoadConfig(LoadConfigOptions{})
	// Python 注释明言：裸 TypeError 曾绕过 Click 的错误行打出 traceback
	require.ErrorContains(t, err, "max_tokens must be an integer")
}

// ──────────────────────────── factory.go 核心 ────────────────────────────

// TestDefaultSkillDirs_顺序与优先级 对齐 factory.py:49-55。
func TestDefaultSkillDirs_顺序与优先级(t *testing.T) {
	dirs := DefaultSkillDirsSnapshot()
	require.Len(t, dirs, 4)
	assert.Equal(t, "~/.openjiuwen/workspace/skills", dirs[0])
	assert.Equal(t, "~/.claude/skills", dirs[1])
	assert.Equal(t, "~/.codex/skills", dirs[2])
	assert.Equal(t, "~/.jiuwenclaw/workspace/skills", dirs[3])
	// 快照是副本，改快照不影响常量
	dirs[0] = "x"
	assert.Equal(t, "~/.openjiuwen/workspace/skills", DefaultSkillDirs[0])
}

// TestLoadMcpConfigs_正常读取 对齐 factory.py:334-396。
func TestLoadMcpConfigs_正常读取(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mcpDir := filepath.Join(home, ".openjiuwen")
	require.NoError(t, os.MkdirAll(mcpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "mcp.json"), []byte(`{
		"mcpServers": {
			"fs": {"transport": "stdio", "command": "npx", "args": ["-y", "@mcp/fs"], "env": {"K": "V"}},
			"api": {"url": "https://x/mcp", "auth_headers": {"H": "v"}},
			"legacy": {"client_type": "sse", "server_path": "https://y"}
		}
	}`), 0o644))

	configs := LoadMcpConfigs()
	require.Len(t, configs, 3)
	byName := map[string]map[string]any{}
	for _, cfg := range configs {
		byName[cfg["server_name"].(string)] = cfg
	}
	fs := byName["fs"]
	assert.Equal(t, "stdio", fs["client_type"])
	params := fs["params"].(map[string]any)
	assert.Equal(t, "npx", params["command"])
	assert.Equal(t, "https://x/mcp", byName["api"]["server_path"])
	assert.Equal(t, "sse", byName["legacy"]["client_type"])
}

// TestLoadMcpConfigs_缺失与坏文件 静默返回空。
func TestLoadMcpConfigs_缺失与坏文件(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Empty(t, LoadMcpConfigs())

	mcpDir := filepath.Join(home, ".openjiuwen")
	require.NoError(t, os.MkdirAll(mcpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mcpDir, "mcp.json"), []byte("{bad"), 0o644))
	assert.Empty(t, LoadMcpConfigs())
}

// TestFilterNoneValues 剔除 nil。
func TestFilterNoneValues(t *testing.T) {
	filtered := filterNoneValues(map[string]any{"a": 1, "b": nil})
	assert.Equal(t, map[string]any{"a": 1}, filtered)
}

// TestCreateBackend_远程未实现 对齐 factory.py:769-773。
func TestCreateBackend_远程未实现(t *testing.T) {
	_, err := CreateBackend(CLIConfig{ServerURL: "http://x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported in the MVP")
}

// TestCreateBackend_本地模式 MVP 默认路径。
func TestCreateBackend_本地模式(t *testing.T) {
	backend, err := CreateBackend(CLIConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, backend.SessionID())
	assert.Contains(t, backend.SessionID(), "cli-")
}

// TestLocalBackend_空流 MVP 骨架的流为空。
func TestLocalBackend_空流(t *testing.T) {
	backend := NewLocalBackend(CLIConfig{})
	require.NoError(t, backend.Start(context.Background()))
	ch, err := backend.RunStreaming(context.Background(), "查询", nil)
	require.NoError(t, err)
	_, open := <-ch
	assert.False(t, open, "骨架后端的流立即关闭")
	require.NoError(t, backend.Stop(context.Background()))
	require.NoError(t, backend.Abort(context.Background()))
}
