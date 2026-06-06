package config

import (
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGetServerConfig(t *testing.T) {
	cfg, err := New("testdata/config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	_, _ = cfg.Load()

	server, err := cfg.GetServerConfig()
	if err != nil {
		t.Fatalf("GetServerConfig 失败: %v", err)
	}

	if server.AgentServer.Host != "0.0.0.0" {
		t.Errorf("期望 agentserver.host = 0.0.0.0，实际 %s", server.AgentServer.Host)
	}
	if server.AgentServer.Port != 8765 {
		t.Errorf("期望 agentserver.port = 8765，实际 %d", server.AgentServer.Port)
	}
	if server.Gateway.Host != "0.0.0.0" {
		t.Errorf("期望 gateway.host = 0.0.0.0，实际 %s", server.Gateway.Host)
	}
	if server.Gateway.Port != 8766 {
		t.Errorf("期望 gateway.port = 8766，实际 %d", server.Gateway.Port)
	}
}

func TestUpdateServerConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, _ := New(cfgPath)
	_ = cfg.Save(map[string]any{
		"server": map[string]any{
			"agentserver": map[string]any{"host": "0.0.0.0", "port": 8765},
			"gateway":     map[string]any{"host": "0.0.0.0", "port": 8766},
		},
	})
	_, _ = cfg.Load()

	err := cfg.UpdateServerConfig(&ServerConfig{
		AgentServer: AgentServerConfig{Host: "127.0.0.1", Port: 9999},
		Gateway:     GatewayConfig{Host: "127.0.0.1", Port: 8888},
	})
	if err != nil {
		t.Fatalf("UpdateServerConfig 失败: %v", err)
	}

	// 重新加载验证
	cfg2, _ := New(cfgPath)
	_, _ = cfg2.Load()
	server, err := cfg2.GetServerConfig()
	if err != nil {
		t.Fatalf("GetServerConfig 失败: %v", err)
	}
	if server.AgentServer.Host != "127.0.0.1" {
		t.Errorf("期望 127.0.0.1，实际 %s", server.AgentServer.Host)
	}
	if server.AgentServer.Port != 9999 {
		t.Errorf("期望 9999，实际 %d", server.AgentServer.Port)
	}
}

func TestGetLoggingConfig(t *testing.T) {
	cfg, err := New("testdata/config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	_, _ = cfg.Load()

	logging, err := cfg.GetLoggingConfig()
	if err != nil {
		t.Fatalf("GetLoggingConfig 失败: %v", err)
	}

	if logging.Level != "info" {
		t.Errorf("期望 level = info，实际 %s", logging.Level)
	}
	if logging.Format != "json" {
		t.Errorf("期望 format = json，实际 %s", logging.Format)
	}
	if logging.ConsoleLevel != "info" {
		t.Errorf("期望 console_level = info，实际 %s", logging.ConsoleLevel)
	}
	if logging.Common != "info" {
		t.Errorf("期望 common = info，实际 %s", logging.Common)
	}
	if logging.Gateway != "info" {
		t.Errorf("期望 gateway = info，实际 %s", logging.Gateway)
	}
	if logging.Channel != "info" {
		t.Errorf("期望 channel = info，实际 %s", logging.Channel)
	}
	if logging.AgentServer != "info" {
		t.Errorf("期望 agent_server = info，实际 %s", logging.AgentServer)
	}
	if logging.Permissions != "info" {
		t.Errorf("期望 permissions = info，实际 %s", logging.Permissions)
	}
	if logging.AgentCore != "info" {
		t.Errorf("期望 agent_core = info，实际 %s", logging.AgentCore)
	}
	if logging.Full != "info" {
		t.Errorf("期望 full = info，实际 %s", logging.Full)
	}
}

func TestUpdateLoggingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, _ := New(cfgPath)
	_ = cfg.Save(map[string]any{
		"logging": map[string]any{"level": "info", "format": "json"},
	})
	_, _ = cfg.Load()

	err := cfg.UpdateLoggingConfig(&LoggingConfig{
		Level:        "debug",
		Format:       "text",
		ConsoleLevel: "debug",
		Common:       "debug",
		Gateway:      "debug",
		Channel:      "debug",
		AgentServer:  "debug",
		Permissions:  "debug",
		AgentCore:    "debug",
		Full:         "debug",
	})
	if err != nil {
		t.Fatalf("UpdateLoggingConfig 失败: %v", err)
	}

	cfg2, _ := New(cfgPath)
	_, _ = cfg2.Load()
	logging, err := cfg2.GetLoggingConfig()
	if err != nil {
		t.Fatalf("GetLoggingConfig 失败: %v", err)
	}
	if logging.Level != "debug" {
		t.Errorf("期望 debug，实际 %s", logging.Level)
	}
	if logging.Format != "text" {
		t.Errorf("期望 text，实际 %s", logging.Format)
	}
}

func TestGetWorkspaceConfig(t *testing.T) {
	cfg, err := New("testdata/config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	_, _ = cfg.Load()

	workspace, err := cfg.GetWorkspaceConfig()
	if err != nil {
		t.Fatalf("GetWorkspaceConfig 失败: %v", err)
	}

	if workspace.Path != "~/.uapclaw" {
		t.Errorf("期望 ~/.uapclaw，实际 %s", workspace.Path)
	}
}

func TestUpdateWorkspaceConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, _ := New(cfgPath)
	_ = cfg.Save(map[string]any{
		"workspace": map[string]any{"path": "~/.uapclaw"},
	})
	_, _ = cfg.Load()

	err := cfg.UpdateWorkspaceConfig(&WorkspaceConfig{Path: "/custom/path"})
	if err != nil {
		t.Fatalf("UpdateWorkspaceConfig 失败: %v", err)
	}

	cfg2, _ := New(cfgPath)
	_, _ = cfg2.Load()
	workspace, err := cfg2.GetWorkspaceConfig()
	if err != nil {
		t.Fatalf("GetWorkspaceConfig 失败: %v", err)
	}
	if workspace.Path != "/custom/path" {
		t.Errorf("期望 /custom/path，实际 %s", workspace.Path)
	}
}

func TestGetServerConfig_段不存在(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, _ := New(cfgPath)
	_ = cfg.Save(map[string]any{})
	_, _ = cfg.Load()

	_, err := cfg.GetServerConfig()
	if err == nil {
		t.Error("期望返回错误，因为 server 段不存在")
	}
}

// TestGetLoggingConfig_段不存在 测试 logging 段不存在时返回错误
func TestGetLoggingConfig_段不存在(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, _ := New(cfgPath)
	_ = cfg.Save(map[string]any{})
	_, _ = cfg.Load()

	_, err := cfg.GetLoggingConfig()
	if err == nil {
		t.Error("期望返回错误，因为 logging 段不存在")
	}
}

// TestGetWorkspaceConfig_段不存在 测试 workspace 段不存在时返回错误
func TestGetWorkspaceConfig_段不存在(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, _ := New(cfgPath)
	_ = cfg.Save(map[string]any{})
	_, _ = cfg.Load()

	_, err := cfg.GetWorkspaceConfig()
	if err == nil {
		t.Error("期望返回错误，因为 workspace 段不存在")
	}
}
