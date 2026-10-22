package extensions

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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
		// typed nil：接口比较中 *string(nil) != nil
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

// TestMemoryHookContext_FromMap 测试 FromMap 写回输出字段
func TestMemoryHookContext_FromMap(t *testing.T) {
	channelID := "ch-001"
	ctx := MemoryHookContext{SessionID: "sess-001", ChannelID: &channelID}
	m := map[string]any{
		"memory_blocks":     []any{"block1", "", "block2"},
		"assistant_message": "answer",
		"metadata":          map[string]any{"k": "v"},
		"session_id":        "篡改输入字段应被忽略",
	}
	ctx.FromMap(m)
	if len(ctx.MemoryBlocks) != 3 {
		t.Errorf("MemoryBlocks len = %d, 期望 3（FromMap 不过滤空块，过滤在消费点）", len(ctx.MemoryBlocks))
	}
	if ctx.AssistantMessage == nil || *ctx.AssistantMessage != "answer" {
		t.Errorf("AssistantMessage 写回失败: %v", ctx.AssistantMessage)
	}
	if ctx.Metadata == nil || ctx.Metadata["k"] != "v" {
		t.Errorf("Metadata 写回失败: %v", ctx.Metadata)
	}
	if ctx.SessionID != "sess-001" {
		t.Errorf("输入字段不应被写回: %q", ctx.SessionID)
	}
}

// TestMemoryHookContext_FromMap_防御分支 测试类型不符与缺失字段
func TestMemoryHookContext_FromMap_防御分支(t *testing.T) {
	ctx := MemoryHookContext{SessionID: "s"}
	ctx.FromMap(map[string]any{
		"memory_blocks":     "不是数组",
		"assistant_message": 42,
		"metadata":          "不是map",
	})
	if ctx.MemoryBlocks != nil {
		t.Errorf("类型不符应跳过: %v", ctx.MemoryBlocks)
	}
	if ctx.AssistantMessage != nil {
		t.Errorf("assistant_message 非字符串应跳过: %v", ctx.AssistantMessage)
	}
	if ctx.Metadata != nil {
		t.Errorf("metadata 非 map 应跳过: %v", ctx.Metadata)
	}
	// nil map 不 panic
	ctx.FromMap(nil)
}

// TestGatewayChatHookContext_FromMap 测试 params 写回
func TestGatewayChatHookContext_FromMap(t *testing.T) {
	ctx := GatewayChatHookContext{RequestID: "r1"}
	ctx.FromMap(map[string]any{
		"params":     map[string]any{"query": "改写后"},
		"request_id": "不应写回",
	})
	if ctx.Params == nil || ctx.Params["query"] != "改写后" {
		t.Errorf("Params 写回失败: %v", ctx.Params)
	}
	if ctx.RequestID != "r1" {
		t.Errorf("输入字段不应写回: %q", ctx.RequestID)
	}
}

// TestAgentServerChatHookContext_FromMap 测试 params 与指针字段写回
func TestAgentServerChatHookContext_FromMap(t *testing.T) {
	ctx := AgentServerChatHookContext{RequestID: "r2"}
	ctx.FromMap(map[string]any{
		"params":     map[string]any{"mode": "code"},
		"session_id": "sess-9",
	})
	if ctx.Params == nil || ctx.Params["mode"] != "code" {
		t.Errorf("Params 写回失败: %v", ctx.Params)
	}
	if ctx.SessionID == nil || *ctx.SessionID != "sess-9" {
		t.Errorf("SessionID 指针写回失败: %v", ctx.SessionID)
	}
}

// TestSystemPromptHookContext_FromMap 测试目录写回
func TestSystemPromptHookContext_FromMap(t *testing.T) {
	ctx := SystemPromptHookContext{}
	ctx.FromMap(map[string]any{
		"home_dir":  "/custom/home",
		"skill_dir": 123, // 类型不符跳过
	})
	if ctx.HomeDir == nil || *ctx.HomeDir != "/custom/home" {
		t.Errorf("HomeDir 写回失败: %v", ctx.HomeDir)
	}
	if ctx.SkillDir != nil {
		t.Errorf("skill_dir 类型不符应跳过: %v", ctx.SkillDir)
	}
}

// TestHookContext_ToMapFromMap_Roundtrip 测试 ToMap→FromMap 闭环
func TestHookContext_ToMapFromMap_Roundtrip(t *testing.T) {
	channelID := "ch"
	assistant := "msg"
	orig := MemoryHookContext{
		SessionID: "s", RequestID: "r", ChannelID: &channelID,
		AgentName: "a", WorkspaceDir: "/w",
		AssistantMessage: &assistant,
		Extra:            map[string]any{"e": 1},
		MemoryBlocks:     []string{"b1"},
		Metadata:         map[string]any{"m": 2},
	}
	restored := MemoryHookContext{SessionID: "s", RequestID: "r", ChannelID: &channelID, AgentName: "a", WorkspaceDir: "/w"}
	restored.FromMap(orig.ToMap())
	if len(restored.MemoryBlocks) != 1 || restored.MemoryBlocks[0] != "b1" {
		t.Errorf("MemoryBlocks 往返不符: %v", restored.MemoryBlocks)
	}
	if restored.AssistantMessage == nil || *restored.AssistantMessage != "msg" {
		t.Errorf("AssistantMessage 往返不符: %v", restored.AssistantMessage)
	}
}

// TestTriggerMemoryBeforeChat_写回注入 测试辅助函数：扩展写入 memory_blocks → inputs 注入
func TestTriggerMemoryBeforeChat_写回注入(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	reg.Register(AgentServerMemoryBeforeChat, func(ctx context.Context, data map[string]any) any {
		data["memory_blocks"] = []any{"块一", "", "块二"}
		return nil
	}, 100)

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	inputs := map[string]any{}

	TriggerMemoryBeforeChat(context.Background(), request, inputs, "/tmp/w", nil)

	// 空块被过滤（对齐 Python join(b for b in blocks if b)），两块拼接
	if inputs["memory_block"] != "块一\n\n块二" {
		t.Errorf("memory_block = %v, 期望 %q", inputs["memory_block"], "块一\n\n块二")
	}
}

// TestTriggerMemoryBeforeChat_无条件写入 测试无扩展时 memory_block 空串也写入（对齐 Python）
func TestTriggerMemoryBeforeChat_无条件写入(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	_ = reg // 无扩展回调场景：仅注册中心存在
	defer ResetInstance()

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	inputs := map[string]any{}

	TriggerMemoryBeforeChat(context.Background(), request, inputs, "/tmp/w", nil)

	v, ok := inputs["memory_block"]
	if !ok {
		t.Error("memory_block 应无条件写入（对齐 Python inputs['memory_block'] = memory_block）")
	} else if v != "" {
		t.Errorf("memory_block 应为空串, 实际 %v", v)
	}
}

// TestTriggerMemoryBeforeChat_进程内原地修改 测试扩展直接改 data map（[]any 形态写回）
func TestTriggerMemoryBeforeChat_进程内原地修改(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	reg.Register(AgentServerMemoryBeforeChat, func(ctx context.Context, data map[string]any) any {
		blocks, _ := data["memory_blocks"].([]string)
		data["memory_blocks"] = append(blocks, "原地追加")
		return nil
	}, 100)

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	inputs := map[string]any{}

	TriggerMemoryBeforeChat(context.Background(), request, inputs, "/tmp/w", nil)

	if inputs["memory_block"] != "原地追加" {
		t.Errorf("memory_block = %v, 期望 %q（原地 []string 修改链路）", inputs["memory_block"], "原地追加")
	}
}

// TestTriggerMemoryAfterChat_无异常 测试 after 钩子辅助函数可调用
func TestTriggerMemoryAfterChat_无异常(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	_ = reg // 仅注册中心存在，无回调场景
	defer ResetInstance()

	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "web", SessionID: &sid}
	TriggerMemoryAfterChat(context.Background(), request, "回答内容", "/tmp/w", nil)
}

// TestBuildMemoryHookContext_SessionID回退 测试 session_id 三态：
// 正常值直传 / nil 回退 default / 空串回退 default（对齐 Python `or "default"` 语义）
func TestBuildMemoryHookContext_SessionID回退(t *testing.T) {
	// 正常值
	sid := "sess-9"
	ctx := buildMemoryHookContext(&schema.AgentRequest{SessionID: &sid}, "/w", nil, nil)
	if ctx.SessionID != "sess-9" {
		t.Errorf("SessionID = %q, 期望直传 sess-9", ctx.SessionID)
	}
	// nil → default 空指针回退默认值
	ctx2 := buildMemoryHookContext(&schema.AgentRequest{}, "/w", nil, nil)
	if ctx2.SessionID != "default" {
		t.Errorf("nil SessionID 应回退 default, 实际 %q", ctx2.SessionID)
	}
	// 空串 → default（Python or 语义：空串为假）
	empty := ""
	ctx3 := buildMemoryHookContext(&schema.AgentRequest{SessionID: &empty}, "/w", nil, nil)
	if ctx3.SessionID != "default" {
		t.Errorf("空串 SessionID 应回退 default, 实际 %q", ctx3.SessionID)
	}
}
