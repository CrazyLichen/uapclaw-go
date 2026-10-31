package a2a

import (
	"testing"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
)

// TestBuildAgentCard 对齐 Python AgentCard 构造 (L213-238)
func TestBuildAgentCard(t *testing.T) {
	cfg := A2AChannelConfig{
		Host:            "127.0.0.1",
		Port:            19100,
		RPCPath:         "/a2a",
		ProtocolVersion: "1.0.0",
		AppName:         "UapClaw Gateway A2A Server",
		AppDescription:  "A2A ingress for UapClaw Gateway",
		AppVersion:      "0.1.0",
	}
	card := BuildAgentCard(&cfg)

	if card.Name != cfg.AppName || card.Description != cfg.AppDescription || card.Version != cfg.AppVersion {
		t.Errorf("card = %q/%q/%q", card.Name, card.Description, card.Version)
	}
	if len(card.SupportedInterfaces) != 1 {
		t.Fatalf("SupportedInterfaces 数量 = %d", len(card.SupportedInterfaces))
	}
	iface := card.SupportedInterfaces[0]
	if iface.URL != "http://127.0.0.1:19100/a2a" {
		t.Errorf("URL = %q", iface.URL)
	}
	// 偏差 D2：规范常量为大写 JSONRPC
	if iface.ProtocolBinding != sdka2a.TransportProtocolJSONRPC {
		t.Errorf("ProtocolBinding = %q, 期望 JSONRPC", iface.ProtocolBinding)
	}
	if string(iface.ProtocolVersion) != "1.0.0" {
		t.Errorf("ProtocolVersion = %q", iface.ProtocolVersion)
	}
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		t.Errorf("Capabilities = %+v", card.Capabilities)
	}
	if len(card.DefaultInputModes) != 1 || card.DefaultInputModes[0] != "text/plain" {
		t.Errorf("DefaultInputModes = %v", card.DefaultInputModes)
	}
	if len(card.Skills) != 1 {
		t.Fatalf("Skills 数量 = %d", len(card.Skills))
	}
	skill := card.Skills[0]
	if skill.ID != "chat" || skill.Name != "chat" {
		t.Errorf("skill = %+v", skill)
	}
	if skill.Description != "Send user prompt to UapClaw via Gateway" {
		t.Errorf("skill description = %q", skill.Description)
	}
	if len(skill.Tags) != 3 || skill.Tags[2] != "uapclawswarm" {
		t.Errorf("skill tags = %v", skill.Tags)
	}
}

// TestBuildAgentCard_nil nil 配置返回 nil
func TestBuildAgentCard_nil(t *testing.T) {
	if card := BuildAgentCard(nil); card != nil {
		t.Errorf("nil 配置应返回 nil, 实际 %+v", card)
	}
}
