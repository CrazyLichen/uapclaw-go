package logger

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestComponent_String(t *testing.T) {
	tests := []struct {
		component Component
		expected  string
	}{
		{ComponentCommon, "common"},
		{ComponentGateway, "gateway"},
		{ComponentChannel, "channel"},
		{ComponentAgentServer, "agent_server"},
		{ComponentPermissions, "permissions"},
		{Component(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.component.String(); got != tt.expected {
			t.Errorf("Component(%d).String() = %q, 期望 %q", tt.component, got, tt.expected)
		}
	}
}

func TestComponent_GoString(t *testing.T) {
	if got := ComponentChannel.GoString(); got != "Componentchannel" {
		t.Errorf("期望 Componentchannel，实际 %s", got)
	}
}

func TestComponent_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ComponentAgentServer)
	if err != nil {
		t.Fatalf("MarshalJSON 失败: %v", err)
	}
	if string(data) != `"agent_server"` {
		t.Errorf("期望 \"agent_server\"，实际 %s", string(data))
	}
}

func TestComponent_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected Component
	}{
		{`"common"`, ComponentCommon},
		{`"gateway"`, ComponentGateway},
		{`"channel"`, ComponentChannel},
		{`"agent_server"`, ComponentAgentServer},
		{`"permissions"`, ComponentPermissions},
		{`"unknown"`, ComponentCommon},
	}
	for _, tt := range tests {
		var c Component
		if err := json.Unmarshal([]byte(tt.input), &c); err != nil {
			t.Fatalf("UnmarshalJSON(%s) 失败: %v", tt.input, err)
		}
		if c != tt.expected {
			t.Errorf("UnmarshalJSON(%s) = %v, 期望 %v", tt.input, c, tt.expected)
		}
	}
}

func TestComponent_LogFileName(t *testing.T) {
	tests := []struct {
		component Component
		expected  string
	}{
		{ComponentCommon, "common.log"},
		{ComponentGateway, "gateway.log"},
		{ComponentChannel, "channel.log"},
		{ComponentAgentServer, "agent_server.log"},
		{ComponentPermissions, "permissions.log"},
		{Component(99), "gateway.log"},
	}
	for _, tt := range tests {
		if got := tt.component.LogFileName(); got != tt.expected {
			t.Errorf("Component(%d).LogFileName() = %q, 期望 %q", tt.component, got, tt.expected)
		}
	}
}

func TestComponentFromString(t *testing.T) {
	tests := []struct {
		name     string
		expected Component
	}{
		{"common", ComponentCommon},
		{"gateway", ComponentGateway},
		{"channel", ComponentChannel},
		{"agent_server", ComponentAgentServer},
		{"permissions", ComponentPermissions},
		{"unknown", ComponentCommon},
		{"", ComponentCommon},
	}
	for _, tt := range tests {
		got := componentFromString(tt.name)
		if got != tt.expected {
			t.Errorf("componentFromString(%q) = %v, 期望 %v", tt.name, got, tt.expected)
		}
	}
}

func TestAllComponents(t *testing.T) {
	comps := allComponents()
	if len(comps) != 5 {
		t.Errorf("期望 5 个组件，实际 %d", len(comps))
	}
	// 确保顺序正确
	if comps[0] != ComponentCommon {
		t.Errorf("期望 comps[0] = ComponentCommon，实际 %v", comps[0])
	}
	if comps[4] != ComponentPermissions {
		t.Errorf("期望 comps[4] = ComponentPermissions，实际 %v", comps[4])
	}
}
