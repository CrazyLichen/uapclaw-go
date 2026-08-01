package extensions

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestGatewayHookEvents_作用域 测试 Gateway 事件作用域
func TestGatewayHookEvents_作用域(t *testing.T) {
	events := NewGatewayHookEvents()
	if events.Scope != "gateway" {
		t.Errorf("Scope = %q, want %q", events.Scope, "gateway")
	}
}

// TestGatewayHookEvents_常量值 测试 Gateway 事件常量值格式
func TestGatewayHookEvents_常量值(t *testing.T) {
	wantStarted := "gateway:gateway_started"
	wantStopped := "gateway:gateway_stopped"
	wantBeforeChat := "gateway:before_chat_request"

	if GatewayStarted != wantStarted {
		t.Errorf("GatewayStarted = %q, want %q", GatewayStarted, wantStarted)
	}
	if GatewayStopped != wantStopped {
		t.Errorf("GatewayStopped = %q, want %q", GatewayStopped, wantStopped)
	}
	if GatewayBeforeChatRequest != wantBeforeChat {
		t.Errorf("GatewayBeforeChatRequest = %q, want %q", GatewayBeforeChatRequest, wantBeforeChat)
	}
}

// TestGatewayHookEvents_GetEvent 测试 GetEvent 方法构建 scoped 事件名
func TestGatewayHookEvents_GetEvent(t *testing.T) {
	events := NewGatewayHookEvents()
	result := events.GetEvent("custom_event")
	want := "gateway:custom_event"
	if result != want {
		t.Errorf("GetEvent(custom_event) = %q, want %q", result, want)
	}
}

// TestAgentServerHookEvents_作用域 测试 AgentServer 事件作用域
func TestAgentServerHookEvents_作用域(t *testing.T) {
	events := NewAgentServerHookEvents()
	if events.Scope != "agent_server" {
		t.Errorf("Scope = %q, want %q", events.Scope, "agent_server")
	}
}

// TestAgentServerHookEvents_常量值 测试 AgentServer 事件常量值格式
func TestAgentServerHookEvents_常量值(t *testing.T) {
	wantStarted := "agent_server:agent_server_started"
	wantStopped := "agent_server:agent_server_stopped"
	wantBeforeChat := "agent_server:before_chat_request"
	wantMemoryBeforeChat := "agent_server:memory_before_chat"
	wantMemoryAfterChat := "agent_server:memory_after_chat"
	wantBeforePromptBuild := "agent_server:before_system_prompt_build"

	if AgentServerStarted != wantStarted {
		t.Errorf("AgentServerStarted = %q, want %q", AgentServerStarted, wantStarted)
	}
	if AgentServerStopped != wantStopped {
		t.Errorf("AgentServerStopped = %q, want %q", AgentServerStopped, wantStopped)
	}
	if AgentServerBeforeChatRequest != wantBeforeChat {
		t.Errorf("AgentServerBeforeChatRequest = %q, want %q", AgentServerBeforeChatRequest, wantBeforeChat)
	}
	if AgentServerMemoryBeforeChat != wantMemoryBeforeChat {
		t.Errorf("AgentServerMemoryBeforeChat = %q, want %q", AgentServerMemoryBeforeChat, wantMemoryBeforeChat)
	}
	if AgentServerMemoryAfterChat != wantMemoryAfterChat {
		t.Errorf("AgentServerMemoryAfterChat = %q, want %q", AgentServerMemoryAfterChat, wantMemoryAfterChat)
	}
	if AgentServerBeforeSystemPromptBuild != wantBeforePromptBuild {
		t.Errorf("AgentServerBeforeSystemPromptBuild = %q, want %q", AgentServerBeforeSystemPromptBuild, wantBeforePromptBuild)
	}
}

// TestAgentServerHookEvents_GetEvent 测试 GetEvent 方法构建 scoped 事件名
func TestAgentServerHookEvents_GetEvent(t *testing.T) {
	events := NewAgentServerHookEvents()
	result := events.GetEvent("custom_event")
	want := "agent_server:custom_event"
	if result != want {
		t.Errorf("GetEvent(custom_event) = %q, want %q", result, want)
	}
}

// TestHookEventBase_ParseEventName_一致性 测试事件名解析与 BuildEventName 一致
func TestHookEventBase_ParseEventName_一致性(t *testing.T) {
	scope, name := schema.ParseEventName(GatewayBeforeChatRequest)
	if scope != "gateway" || name != "before_chat_request" {
		t.Errorf("ParseEventName(%q) = (%q, %q), want (%q, %q)",
			GatewayBeforeChatRequest, scope, name, "gateway", "before_chat_request")
	}

	rebuilt := schema.BuildEventName(scope, name)
	if rebuilt != GatewayBeforeChatRequest {
		t.Errorf("BuildEventName(%q, %q) = %q, want %q", scope, name, rebuilt, GatewayBeforeChatRequest)
	}
}
