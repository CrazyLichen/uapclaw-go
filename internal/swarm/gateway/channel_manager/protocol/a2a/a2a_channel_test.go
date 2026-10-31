package a2a

import "testing"

// TestConfigFromEnv_默认值 对齐 Python A2AChannelConfig 默认值 (L23-35)
func TestConfigFromEnv_默认值(t *testing.T) {
	cfg := ConfigFromEnv(func(string) string { return "" })

	if cfg.Enabled {
		t.Error("默认应关闭")
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 19100 {
		t.Errorf("Host/Port = %s/%d", cfg.Host, cfg.Port)
	}
	if cfg.RPCPath != "/a2a" {
		t.Errorf("RPCPath = %q", cfg.RPCPath)
	}
	if cfg.CardPath != "/.well-known/agent-card.json" {
		t.Errorf("CardPath = %q", cfg.CardPath)
	}
	if cfg.ExtendedCardPath != "/agent/authenticatedExtendedCard" {
		t.Errorf("ExtendedCardPath = %q", cfg.ExtendedCardPath)
	}
	if cfg.ProtocolVersion != "1.0.0" {
		t.Errorf("ProtocolVersion = %q", cfg.ProtocolVersion)
	}
	if cfg.ChannelID != "a2a" {
		t.Errorf("ChannelID = %q", cfg.ChannelID)
	}
	// 偏差 D9：品牌串映射 JiuwenClaw → UapClaw
	if cfg.AppName != "UapClaw Gateway A2A Server" ||
		cfg.AppDescription != "A2A ingress for UapClaw Gateway" ||
		cfg.AppVersion != "0.1.0" {
		t.Errorf("App* = %q / %q / %q", cfg.AppName, cfg.AppDescription, cfg.AppVersion)
	}
}

// TestConfigFromEnv_覆盖与修剪 覆盖全部 env，含空白值回落
func TestConfigFromEnv_覆盖与修剪(t *testing.T) {
	env := map[string]string{
		"A2A_SERVER_ENABLED":            "on",
		"A2A_SERVER_HOST":               "0.0.0.0",
		"A2A_SERVER_PORT":               "19200",
		"A2A_SERVER_PATH":               "/rpc",
		"A2A_SERVER_CARD_PATH":          "/card.json",
		"A2A_SERVER_EXTENDED_CARD_PATH": "/ext",
		"A2A_SERVER_PROTOCOL_VERSION":   "1.0.1",
		"A2A_SERVER_APP_NAME":           "  Custom  ",
		"A2A_SERVER_APP_DESCRIPTION":    "desc",
		"A2A_SERVER_APP_VERSION":        "9.9.9",
	}
	cfg := ConfigFromEnv(func(k string) string { return env[k] })

	if !cfg.Enabled {
		t.Error("on 应解析为启用")
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 19200 {
		t.Errorf("Host/Port = %s/%d", cfg.Host, cfg.Port)
	}
	if cfg.RPCPath != "/rpc" || cfg.CardPath != "/card.json" || cfg.ExtendedCardPath != "/ext" {
		t.Errorf("路径 = %q/%q/%q", cfg.RPCPath, cfg.CardPath, cfg.ExtendedCardPath)
	}
	if cfg.ProtocolVersion != "1.0.1" || cfg.AppVersion != "9.9.9" || cfg.AppDescription != "desc" {
		t.Errorf("版本/描述 = %q/%q/%q", cfg.ProtocolVersion, cfg.AppVersion, cfg.AppDescription)
	}
	if cfg.AppName != "Custom" {
		t.Errorf("AppName 未修剪: %q", cfg.AppName)
	}

	cfg2 := ConfigFromEnv(func(k string) string {
		if k == "A2A_SERVER_HOST" || k == "A2A_SERVER_PATH" {
			return "   "
		}
		return ""
	})
	if cfg2.Host != "127.0.0.1" || cfg2.RPCPath != "/a2a" {
		t.Errorf("空白值未回落: %q/%q", cfg2.Host, cfg2.RPCPath)
	}
}

// TestConfigFromEnv_非法端口回落 非法端口回默认并保持可用
func TestConfigFromEnv_非法端口回落(t *testing.T) {
	cfg := ConfigFromEnv(func(k string) string {
		if k == "A2A_SERVER_PORT" {
			return "not-a-number"
		}
		return ""
	})
	if cfg.Port != 19100 {
		t.Errorf("非法端口应回落 19100, 实际 %d", cfg.Port)
	}
}

// TestConfigFromEnv_启用取值集合 对齐 Python {"1","true","yes","on"}
func TestConfigFromEnv_启用取值集合(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "On"} {
		if !ConfigFromEnv(func(string) string { return v }).Enabled {
			t.Errorf("%q 应启用", v)
		}
	}
	for _, v := range []string{"0", "false", "", "no", "enable"} {
		if ConfigFromEnv(func(string) string { return v }).Enabled {
			t.Errorf("%q 不应启用", v)
		}
	}
}
