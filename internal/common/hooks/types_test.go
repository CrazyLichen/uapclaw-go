package hooks

import "testing"

// TestHookType_常量值 测试 HookType 对齐 Python HookType(COMMAND/PROMPT)
func TestHookType_常量值(t *testing.T) {
	if HookTypeCommand != "command" {
		t.Errorf("HookTypeCommand = %q, want %q", HookTypeCommand, "command")
	}
	if HookTypePrompt != "prompt" {
		t.Errorf("HookTypePrompt = %q, want %q", HookTypePrompt, "prompt")
	}
}

// TestHookEvent_常量值 测试 17 个 HookEvent 常量对齐 Python HookEvent
func TestHookEvent_常量值(t *testing.T) {
	want := map[string]string{
		"PreToolUse":         HookEventPreToolUse,
		"PostToolUse":        HookEventPostToolUse,
		"PostToolUseFailure": HookEventPostToolUseFailure,
		"Stop":               HookEventStop,
		"UserPromptSubmit":   HookEventUserPromptSubmit,
		"SessionStart":       HookEventSessionStart,
		"SessionEnd":         HookEventSessionEnd,
		"Notification":       HookEventNotification,
		"PermissionRequest":  HookEventPermissionRequest,
		"PermissionDenied":   HookEventPermissionDenied,
		"SubagentStart":      HookEventSubagentStart,
		"SubagentStop":       HookEventSubagentStop,
		"ConfigChange":       HookEventConfigChange,
		"InstructionsLoaded": HookEventInstructionsLoaded,
		"Setup":              HookEventSetup,
		"BeforeModelCall":    HookEventBeforeModelCall,
		"AfterModelCall":     HookEventAfterModelCall,
	}
	for pythonName, goConst := range want {
		if goConst != pythonName {
			t.Errorf("Go constant = %q, want Python value %q", goConst, pythonName)
		}
	}
}

// TestIsRailEvent 测试 AgentServer Rail 层事件判断
func TestIsRailEvent(t *testing.T) {
	railEvents := []string{
		HookEventPreToolUse, HookEventPostToolUse, HookEventPostToolUseFailure,
		HookEventStop, HookEventPermissionRequest, HookEventPermissionDenied,
		HookEventSubagentStart, HookEventSubagentStop,
		HookEventBeforeModelCall, HookEventAfterModelCall,
	}
	for _, e := range railEvents {
		if !IsRailEvent(e) {
			t.Errorf("IsRailEvent(%q) = false, want true", e)
		}
	}
	// Gateway 事件应返回 false
	if IsRailEvent(HookEventUserPromptSubmit) {
		t.Errorf("IsRailEvent(%q) = true, want false", HookEventUserPromptSubmit)
	}
}

// TestIsGatewayEvent 测试 Gateway 层事件判断
func TestIsGatewayEvent(t *testing.T) {
	gatewayEvents := []string{
		HookEventUserPromptSubmit, HookEventSessionStart, HookEventSessionEnd,
		HookEventNotification, HookEventConfigChange, HookEventInstructionsLoaded,
		HookEventSetup,
	}
	for _, e := range gatewayEvents {
		if !IsGatewayEvent(e) {
			t.Errorf("IsGatewayEvent(%q) = false, want true", e)
		}
	}
	// Rail 事件应返回 false
	if IsGatewayEvent(HookEventPreToolUse) {
		t.Errorf("IsGatewayEvent(%q) = true, want false", HookEventPreToolUse)
	}
}

// TestAgentRailEvents_完整性 测试 Rail 事件总数为 10
func TestAgentRailEvents_完整性(t *testing.T) {
	if len(AgentRailEvents) != 10 {
		t.Errorf("AgentRailEvents count = %d, want 10", len(AgentRailEvents))
	}
}

// TestGatewayEvents_完整性 测试 Gateway 事件总数为 7
func TestGatewayEvents_完整性(t *testing.T) {
	if len(GatewayEvents) != 7 {
		t.Errorf("GatewayEvents count = %d, want 7", len(GatewayEvents))
	}
}
