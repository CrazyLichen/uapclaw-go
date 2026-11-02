package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

// resourcesConfigPath 返回出厂配置模板相对本包测试文件的路径
func resourcesConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "resources", "config.yaml")
}

// loadResourcesConfig 加载出厂配置模板（New 只构造，必须显式 Load）
func loadResourcesConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := New(resourcesConfigPath(t))
	if err != nil {
		t.Fatalf("构造配置对象失败: %v", err)
	}
	if _, err := cfg.Load(); err != nil {
		t.Fatalf("加载出厂模板失败: %v", err)
	}
	return cfg
}

// requiredKeyPaths 出厂模板必须包含的键路径（Go 生产代码真实读取的键）
var requiredKeyPaths = []string{
	"preferred_language",
	"logging.level",
	"logging.console_level",
	"logging.common",
	"logging.gateway",
	"logging.channel",
	"logging.agent_server",
	"logging.permissions",
	"logging.agent_core",
	"logging.full",
	"memory.mode",
	"memory.engine",
	"memory.external.provider",
	"memory.external.user_id",
	"memory.external.scope_id",
	"memory.external.mem0.user_id",
	"memory.external.mem0.agent_id",
	"models.defaults",
	"embed.embed_api_key",
	"embed.embed_base_url",
	"embed.embed_model",
	"react.agent_name",
	"react.max_iterations",
	"react.enable_task_loop",
	"react.enable_task_planning",
	"react.skill_mode",
	"react.subagents.general_agent.enabled",
	"react.subagents.code_agent.enabled",
	"react.subagents.research_agent.enabled",
	"gateway.host",
	"gateway.port",
	"gateway.session_map_scope",
	"heartbeat.every",
	"heartbeat.target",
	"heartbeat.active_hours.start",
	"heartbeat.active_hours.end",
	"channels.web.send_file_allowed",
	"permissions.enabled",
	"permissions.schema",
	"permissions.permission_mode",
	"team.team_name",
	"team.lifecycle",
	"team.teammate_mode",
	"team.spawn_mode",
	"team.transport.type",
	"team.storage.type",
	"modes.agent.fast.memory.enabled",
	"modes.agent.fast.memory.is_proactive",
	"modes.agent.plan.memory.enabled",
	"modes.agent.plan.memory.is_proactive",
	"modes.team.memory.enabled",
	"mcp.servers",
	"hooks.disable_all_hooks",
	"extensions.extension_dirs",
	"browser.chrome_path",
	"acp_agents",
	"agents",
}

// TestResourcesConfigYAML_键路径齐全 断言模板包含全部被代码读取的键
func TestResourcesConfigYAML_键路径齐全(t *testing.T) {
	cfg := loadResourcesConfig(t)
	for _, key := range requiredKeyPaths {
		if cfg.Get(key) == nil {
			t.Errorf("出厂模板缺少键：%s", key)
		}
	}
}

// TestResourcesConfigYAML_关键值 断言模板值与已裁决的出厂值一致
func TestResourcesConfigYAML_关键值(t *testing.T) {
	cfg := loadResourcesConfig(t)
	cases := []struct {
		key  string
		want any
	}{
		{"heartbeat.every", 3600},
		{"react.max_iterations", 100},
		{"react.enable_task_loop", true},
		{"react.subagents.general_agent.enabled", true},
		{"modes.agent.fast.memory.enabled", true},
		{"modes.agent.fast.memory.is_proactive", false},
		{"modes.agent.plan.memory.enabled", true},
		{"modes.agent.plan.memory.is_proactive", true},
		{"modes.team.memory.enabled", false},
		{"channels.web.send_file_allowed", true},
		{"team.team_name", "uapclaw_team"},
		{"memory.external.mem0.user_id", "uapclawswarm-user"},
		{"memory.external.mem0.agent_id", "uapclawswarm"},
		{"memory.mode", "local"},
		{"memory.engine", "builtin"},
	}
	for _, tc := range cases {
		got := cfg.Get(tc.key)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %v (%T)，期望 %v (%T)", tc.key, got, got, tc.want, tc.want)
		}
	}
}

// TestResourcesConfigYAML_不含死键 断言已删除的死段不会回流
func TestResourcesConfigYAML_不含死键(t *testing.T) {
	cfg := loadResourcesConfig(t)
	for _, dead := range []string{"server", "workspace", "logging.format", "telemetry"} {
		if cfg.Get(dead) != nil {
			t.Errorf("出厂模板不应包含死段/未实现段：%s", dead)
		}
	}
}
