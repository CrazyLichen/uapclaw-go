package processcli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveConfiguredModelName_环境变量兜底 MODEL_NAME 生效。
func TestResolveConfiguredModelName_环境变量兜底(t *testing.T) {
	t.Setenv("MODEL_NAME", "env-model")
	t.Setenv("UAPCLAW_CONFIG_DIR", filepath.Join(t.TempDir(), "nonexistent"))
	t.Setenv("UAPCLAW_CONFIG_YAML", "")
	t.Setenv("UAPCLAW_DATA_DIR", filepath.Join(t.TempDir(), "nonexistent"))
	if got := ResolveConfiguredModelName(); got != "env-model" {
		t.Errorf("got %q, 期望 env-model", got)
	}
}

// TestResolveConfiguredModelName_读config 从 models.defaults 首项取 model_name。
func TestResolveConfiguredModelName_读config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `models:
  defaults:
    - model_client_config:
        model_name: yaml-model
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)
	if got := ResolveConfiguredModelName(); got != "yaml-model" {
		t.Errorf("got %q, 期望 yaml-model", got)
	}
}

// TestResolveConfiguredModelName_环境变量展开 ${VAR} 与 ${VAR:-default}。
// Python: display_context.py:76-93 _resolve_env_text
func TestResolveConfiguredModelName_环境变量展开(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `models:
  defaults:
    - model_client_config:
        model_name: "${MY_MODEL:-fallback-model}"
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)
	t.Setenv("MY_MODEL", "")
	if got := ResolveConfiguredModelName(); got != "fallback-model" {
		t.Errorf("默认值未展开，得 %q", got)
	}
	t.Setenv("MY_MODEL", "actual-model")
	if got := ResolveConfiguredModelName(); got != "actual-model" {
		t.Errorf("环境变量未生效，得 %q", got)
	}
}

// TestResolveDisplayMode_去normal后缀 agent.work.normal → agent.work。
// Python: display_context.py:254-261
func TestResolveDisplayMode_去normal后缀(t *testing.T) {
	if got := ResolveDisplayMode("agent.work.normal", "work"); got != "agent.work" {
		t.Errorf("got %q, 期望 agent.work", got)
	}
	if got := ResolveDisplayMode("agent.code.plan", "code"); got != "agent.code.plan" {
		t.Errorf("got %q, 期望 agent.code.plan", got)
	}
}

// TestResolveCliWorkMode_三段优先 mode 自带 profile 时以它为准。
// Python: display_context.py:262-283
func TestResolveCliWorkMode_三段优先(t *testing.T) {
	cases := []struct {
		mode, workMode, want string
	}{
		{"agent.code.normal", "work", "code"},
		{"team.work.normal", "code", "work"},
		{"code.normal", "work", "code"},
		{"code", "", "code"},
		{"agent", "work", "work"},
		{"agent", "", "code"},
		{"team", "code", "code"},
	}
	for _, c := range cases {
		if got := ResolveCliWorkMode(c.mode, c.workMode); got != c.want {
			t.Errorf("ResolveCliWorkMode(%q, %q) = %q, 期望 %q",
				c.mode, c.workMode, got, c.want)
		}
	}
}
