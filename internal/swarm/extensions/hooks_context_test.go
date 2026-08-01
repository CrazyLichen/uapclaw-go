package extensions

import (
	"testing"
)

// TestMemoryHookContext_字段完整性 测试 MemoryHookContext 字段赋值
func TestMemoryHookContext_字段完整性(t *testing.T) {
	channelID := "ch-001"
	assistantMsg := "hello"
	ctx := MemoryHookContext{
		SessionID:        "sess-001",
		RequestID:        "req-001",
		ChannelID:        &channelID,
		AgentName:        "agent-001",
		WorkspaceDir:     "/tmp/workspace",
		AssistantMessage: &assistantMsg,
		Extra:            map[string]any{"key": "val"},
		MemoryBlocks:     []string{"block1", "block2"},
		Metadata:         map[string]any{"meta": "data"},
	}
	if ctx.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", ctx.SessionID, "sess-001")
	}
	if *ctx.ChannelID != "ch-001" {
		t.Errorf("ChannelID = %q, want %q", *ctx.ChannelID, "ch-001")
	}
	if len(ctx.MemoryBlocks) != 2 {
		t.Errorf("MemoryBlocks len = %d, want 2", len(ctx.MemoryBlocks))
	}
}

// TestMemoryHookContext_ToMap 测试 ToMap 序列化
func TestMemoryHookContext_ToMap(t *testing.T) {
	ctx := MemoryHookContext{
		SessionID:    "sess-001",
		RequestID:    "req-001",
		AgentName:    "agent-001",
		WorkspaceDir: "/tmp/workspace",
	}
	m := ctx.ToMap()
	if m["session_id"] != "sess-001" {
		t.Errorf("ToMap()[session_id] = %q, want %q", m["session_id"], "sess-001")
	}
	if m["agent_name"] != "agent-001" {
		t.Errorf("ToMap()[agent_name] = %q, want %q", m["agent_name"], "agent-001")
	}
}

// TestMemoryHookContext_可选字段 测试 nil 可选字段
func TestMemoryHookContext_可选字段(t *testing.T) {
	ctx := MemoryHookContext{
		SessionID:    "sess-001",
		RequestID:    "req-001",
		AgentName:    "agent-001",
		WorkspaceDir: "/tmp/workspace",
	}
	if ctx.ChannelID != nil {
		t.Error("ChannelID should be nil")
	}
	if ctx.AssistantMessage != nil {
		t.Error("AssistantMessage should be nil")
	}
}

// TestGatewayChatHookContext_字段完整性 测试 GatewayChatHookContext 字段赋值
func TestGatewayChatHookContext_字段完整性(t *testing.T) {
	sessionID := "sess-001"
	reqMethod := "chat.send"
	ctx := GatewayChatHookContext{
		RequestID: "req-001",
		ChannelID: "ch-001",
		SessionID: &sessionID,
		ReqMethod: &reqMethod,
		Params:    map[string]any{"mode": "agent"},
	}
	if ctx.RequestID != "req-001" {
		t.Errorf("RequestID = %q, want %q", ctx.RequestID, "req-001")
	}
	if *ctx.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", *ctx.SessionID, "sess-001")
	}
	if ctx.Params["mode"] != "agent" {
		t.Errorf("Params[mode] = %v, want %q", ctx.Params["mode"], "agent")
	}
}

// TestGatewayChatHookContext_ToMap 测试 ToMap 序列化
func TestGatewayChatHookContext_ToMap(t *testing.T) {
	ctx := GatewayChatHookContext{
		RequestID: "req-001",
		ChannelID: "ch-001",
		Params:    map[string]any{},
	}
	m := ctx.ToMap()
	if m["request_id"] != "req-001" {
		t.Errorf("ToMap()[request_id] = %q, want %q", m["request_id"], "req-001")
	}
}

// TestAgentServerChatHookContext_字段完整性 测试 AgentServerChatHookContext 字段赋值
func TestAgentServerChatHookContext_字段完整性(t *testing.T) {
	sessionID := "sess-001"
	reqMethod := "chat.send"
	ctx := AgentServerChatHookContext{
		RequestID: "req-001",
		ChannelID: "ch-001",
		SessionID: &sessionID,
		ReqMethod: &reqMethod,
		Params:    map[string]any{"mode": "agent"},
	}
	if ctx.ChannelID != "ch-001" {
		t.Errorf("ChannelID = %q, want %q", ctx.ChannelID, "ch-001")
	}
}

// TestAgentServerChatHookContext_ToMap 测试 ToMap 序列化
func TestAgentServerChatHookContext_ToMap(t *testing.T) {
	ctx := AgentServerChatHookContext{
		RequestID: "req-001",
		ChannelID: "ch-001",
		Params:    map[string]any{},
	}
	m := ctx.ToMap()
	if m["channel_id"] != "ch-001" {
		t.Errorf("ToMap()[channel_id] = %q, want %q", m["channel_id"], "ch-001")
	}
}

// TestSystemPromptHookContext_字段完整性 测试 SystemPromptHookContext 字段赋值
func TestSystemPromptHookContext_字段完整性(t *testing.T) {
	homeDir := "/home/test"
	skillDir := "/skills"
	ctx := SystemPromptHookContext{
		HomeDir:  &homeDir,
		SkillDir: &skillDir,
	}
	if *ctx.HomeDir != "/home/test" {
		t.Errorf("HomeDir = %q, want %q", *ctx.HomeDir, "/home/test")
	}
	if *ctx.SkillDir != "/skills" {
		t.Errorf("SkillDir = %q, want %q", *ctx.SkillDir, "/skills")
	}
}

// TestSystemPromptHookContext_ToMap 测试 ToMap 序列化
func TestSystemPromptHookContext_ToMap(t *testing.T) {
	ctx := SystemPromptHookContext{}
	m := ctx.ToMap()
	// nil *string 存入 map[string]any 后为 typed nil，需用反射判断
	homeDirVal := m["home_dir"]
	if homeDirVal != nil {
		// typed nil: *string(nil) != nil in interface comparison
		// 使用 stringPtr 检查
		if ptr, ok := homeDirVal.(*string); ok && ptr != nil {
			t.Errorf("ToMap()[home_dir] = %v, want nil", homeDirVal)
		}
	}
	skillDirVal := m["skill_dir"]
	if skillDirVal != nil {
		if ptr, ok := skillDirVal.(*string); ok && ptr != nil {
			t.Errorf("ToMap()[skill_dir] = %v, want nil", skillDirVal)
		}
	}
}
