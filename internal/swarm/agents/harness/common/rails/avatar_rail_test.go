package rails

import (
	"context"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeBaseAgentForAvatar 用于测试的 mock BaseAgent
type fakeBaseAgentForAvatar struct {
	sb saprompt.SystemPromptBuilderInterface
}

func (f *fakeBaseAgentForAvatar) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgentForAvatar) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgentForAvatar) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgentForAvatar) Card() *agentschema.AgentCard                            { return nil }
func (f *fakeBaseAgentForAvatar) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgentForAvatar) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeBaseAgentForAvatar) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *fakeBaseAgentForAvatar) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.sb
}
func (f *fakeBaseAgentForAvatar) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForAvatar) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForAvatar) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// newTestContextWithPerm 构建带 PermissionContext 的 context
func newTestContextWithPerm(perm *sschema.PermissionContext) context.Context {
	ctx := context.Background()
	if perm != nil {
		ctx = sschema.WithPermissionContextValue(ctx, perm)
	}
	return ctx
}

// ──────────────────────────── BeforeModelCall 测试 ────────────────────────────

// TestAvatarPromptRail_BeforeModelCall_无PermissionContext 验证 perm_ctx 为 nil 时只跳过 avatar 相关注入
func TestAvatarPromptRail_BeforeModelCall_无PermissionContext(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{sb: builder}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	// 无 PermissionContext 时不应注入 avatar_identity
	if builder.HasSection("avatar_identity") {
		t.Error("无 PermissionContext 时不应注入 avatar_identity")
	}
}

// TestAvatarPromptRail_BeforeModelCall_数字分身模式 验证群聊数字分身注入 identity + notice + interaction
func TestAvatarPromptRail_BeforeModelCall_数字分身模式(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{sb: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionAvatarPrincipalName("张三"),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)

	err := rail.BeforeModelCall(ctx, cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	if !builder.HasSection("avatar_identity") {
		t.Error("数字分身模式应注入 avatar_identity")
	}
	if !builder.HasSection("group_chat_memory_notice") {
		t.Error("数字分身模式应注入 group_chat_memory_notice")
	}
	if !builder.HasSection("interaction_guidance") {
		t.Error("数字分身模式应注入 interaction_guidance")
	}
	// enable_memory=true 时不注入 memory_fully_disabled
	if builder.HasSection("memory_fully_disabled") {
		t.Error("enable_memory=true 时不应注入 memory_fully_disabled")
	}
}

// TestAvatarPromptRail_BeforeModelCall_记忆完全禁用 验证 enable_memory=false 时注入 memory_fully_disabled
func TestAvatarPromptRail_BeforeModelCall_记忆完全禁用(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{sb: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionAvatarPrincipalName("张三"),
		sschema.WithPermissionEnableMemory(false),
	)
	ctx := newTestContextWithPerm(perm)
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)

	err := rail.BeforeModelCall(ctx, cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	if !builder.HasSection("memory_fully_disabled") {
		t.Error("enable_memory=false + 数字分身时应注入 memory_fully_disabled")
	}
}

// TestAvatarPromptRail_BeforeModelCall_非数字分身 不应注入 avatar 相关 section
func TestAvatarPromptRail_BeforeModelCall_非数字分身(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{sb: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(false),
		sschema.WithPermissionAvatarMode(false),
	)
	ctx := newTestContextWithPerm(perm)
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)

	err := rail.BeforeModelCall(ctx, cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	if builder.HasSection("avatar_identity") {
		t.Error("非数字分身模式不应注入 avatar_identity")
	}
	if builder.HasSection("group_chat_memory_notice") {
		t.Error("非数字分身模式不应注入 group_chat_memory_notice")
	}
}

// TestAvatarPromptRail_BeforeModelCall_清除旧注入 验证连续调用时清除上次注入的 section
func TestAvatarPromptRail_BeforeModelCall_清除旧注入(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{sb: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionAvatarPrincipalName("张三"),
	)
	ctx := newTestContextWithPerm(perm)

	// 第一次调用：注入 avatar_identity
	cbc1 := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	rail.BeforeModelCall(ctx, cbc1)
	if !builder.HasSection("avatar_identity") {
		t.Fatal("第一次调用后应存在 avatar_identity")
	}

	// 第二次调用：无 PermissionContext → 应清除 avatar_identity
	cbc2 := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	rail.BeforeModelCall(context.Background(), cbc2)
	if builder.HasSection("avatar_identity") {
		t.Error("第二次调用后 avatar_identity 应被清除")
	}
}

// ──────────────────────────── BeforeToolCall 测试 ────────────────────────────

// TestAvatarPromptRail_BeforeToolCall_群聊禁写 验证群聊模式拦截 write_memory/edit_memory
func TestAvatarPromptRail_BeforeToolCall_群聊禁写(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)
	agent := &fakeBaseAgentForAvatar{sb: nil}

	for _, toolName := range []string{"write_memory", "edit_memory"} {
		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(agent, toolInputs, nil)

		rail.BeforeToolCall(ctx, cbc)

		if cbc.Extra()["_skip_tool"] != true {
			t.Errorf("群聊模式应拦截 %s", toolName)
		}
	}
}

// TestAvatarPromptRail_BeforeToolCall_记忆完全禁用 验证拦截所有 5 个记忆工具
func TestAvatarPromptRail_BeforeToolCall_记忆完全禁用(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(false),
	)
	ctx := newTestContextWithPerm(perm)
	agent := &fakeBaseAgentForAvatar{sb: nil}

	for _, toolName := range []string{"write_memory", "edit_memory", "read_memory", "memory_search", "memory_get"} {
		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(agent, toolInputs, nil)

		rail.BeforeToolCall(ctx, cbc)

		if cbc.Extra()["_skip_tool"] != true {
			t.Errorf("记忆完全禁用应拦截 %s", toolName)
		}
	}
}

// TestAvatarPromptRail_BeforeToolCall_正常放行 验证非记忆工具不被拦截
func TestAvatarPromptRail_BeforeToolCall_正常放行(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)
	agent := &fakeBaseAgentForAvatar{sb: nil}

	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "search"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, toolInputs, nil)

	rail.BeforeToolCall(ctx, cbc)

	if cbc.Extra()["_skip_tool"] == true {
		t.Error("非记忆工具不应被拦截")
	}
}

// TestAvatarPromptRail_BeforeToolCall_读取放行 验证群聊模式下 read_memory 不被拦截
func TestAvatarPromptRail_BeforeToolCall_读取放行(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)
	agent := &fakeBaseAgentForAvatar{sb: nil}

	for _, toolName := range []string{"read_memory", "memory_search", "memory_get"} {
		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(agent, toolInputs, nil)

		rail.BeforeToolCall(ctx, cbc)

		if cbc.Extra()["_skip_tool"] == true {
			t.Errorf("群聊模式（enable_memory=true）下 %s 不应被拦截", toolName)
		}
	}
}

// TestAvatarPromptRail_rejectTool 设置 ToolMsg
func TestAvatarPromptRail_rejectTool(t *testing.T) {
	rail := NewAvatarPromptRail()
	agent := &fakeBaseAgentForAvatar{sb: nil}

	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolName: "write_memory",
		ToolCall: &llmschema.ToolCall{ID: "call-123"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, toolInputs, nil)

	rail.rejectTool(cbc, toolInputs, "[PERMISSION_DENIED] test")

	if cbc.Extra()["_skip_tool"] != true {
		t.Error("_skip_tool 应为 true")
	}
	if toolInputs.ToolResult != "[PERMISSION_DENIED] test" {
		t.Errorf("ToolResult = %q, 期望拒绝消息", toolInputs.ToolResult)
	}
	if toolInputs.ToolMsg == nil {
		t.Fatal("ToolMsg 不应为 nil")
	}
}

// ──────────────────────────── 提示词内容测试 ────────────────────────────

// TestBuildAvatarPrompt_中文 验证中文身份提示词内容
func TestBuildAvatarPrompt_中文(t *testing.T) {
	prompt := buildAvatarPrompt("张三", "cn")
	if prompt == "" {
		t.Fatal("中文身份提示词不应为空")
	}
	if !strings.Contains(prompt, "数字分身模式") {
		t.Error("中文提示词应包含 '数字分身模式'")
	}
	if !strings.Contains(prompt, "张三") {
		t.Error("中文提示词应包含主体名称 '张三'")
	}
	if !strings.Contains(prompt, "第一人称视角") {
		t.Error("中文提示词应包含 '第一人称视角'")
	}
	if !strings.Contains(prompt, "不暴露身份") {
		t.Error("中文提示词应包含 '不暴露身份'")
	}
	if !strings.Contains(prompt, "工具调用") {
		t.Error("中文提示词应包含 '工具调用'（能力不受影响段落）")
	}
}

// TestBuildAvatarPrompt_英文 验证英文身份提示词内容
func TestBuildAvatarPrompt_英文(t *testing.T) {
	prompt := buildAvatarPrompt("Alice", "en")
	if prompt == "" {
		t.Fatal("英文身份提示词不应为空")
	}
	if !strings.Contains(prompt, "Digital Avatar Mode") {
		t.Error("英文提示词应包含 'Digital Avatar Mode'")
	}
	if !strings.Contains(prompt, "Alice") {
		t.Error("英文提示词应包含主体名称 'Alice'")
	}
}

// TestBuildAvatarPrompt_无主体名称 验证无名称时使用"用户本人"
func TestBuildAvatarPrompt_无主体名称(t *testing.T) {
	prompt := buildAvatarPrompt("", "cn")
	if !strings.Contains(prompt, "用户本人") {
		t.Error("无主体名称时中文提示词应包含 '用户本人'")
	}
}

// TestBuildInteractionPrompt_中文 验证追问指引内容
func TestBuildInteractionPrompt_中文(t *testing.T) {
	prompt := buildInteractionPrompt("cn")
	if prompt == "" {
		t.Fatal("中文追问指引不应为空")
	}
	if !strings.Contains(prompt, "多轮交互指引") {
		t.Error("中文指引应包含 '多轮交互指引'")
	}
	if !strings.Contains(prompt, "群聊追问@") {
		t.Error("中文指引应包含 '群聊追问@'")
	}
	if !strings.Contains(prompt, "私聊追问") {
		t.Error("中文指引应包含 '私聊追问'")
	}
	if !strings.Contains(prompt, "必须追问") {
		t.Error("中文指引应包含 '必须追问'")
	}
}

// TestBuildMemoryFullyDisabledPrompt_中文 验证记忆完全禁用提示词
func TestBuildMemoryFullyDisabledPrompt_中文(t *testing.T) {
	prompt := buildMemoryFullyDisabledPrompt("cn")
	if !strings.Contains(prompt, "记忆系统 - 已完全禁用") {
		t.Error("中文提示词应包含 '记忆系统 - 已完全禁用'")
	}
	if !strings.Contains(prompt, "write_memory") {
		t.Error("中文提示词应包含 'write_memory'")
	}
	if !strings.Contains(prompt, "memory_search") {
		t.Error("中文提示词应包含 'memory_search'")
	}
}

// TestBuildGroupChatMemoryNotice_中文 验证群聊禁写通知
func TestBuildGroupChatMemoryNotice_中文(t *testing.T) {
	prompt := buildGroupChatMemoryNotice("cn")
	if !strings.Contains(prompt, "禁止调用 write_memory/edit_memory") {
		t.Error("中文通知应包含 '禁止调用 write_memory/edit_memory'")
	}
}

// TestBuildGroupChatMemoryNotice_英文 验证英文群聊禁写通知
func TestBuildGroupChatMemoryNotice_英文(t *testing.T) {
	prompt := buildGroupChatMemoryNotice("en")
	if !strings.Contains(prompt, "write_memory/edit_memory calls are prohibited") {
		t.Error("英文通知应包含 'write_memory/edit_memory calls are prohibited'")
	}
}

// TestNewAvatarPromptRail 验证构造函数
func TestNewAvatarPromptRail(t *testing.T) {
	rail := NewAvatarPromptRail()
	if rail == nil {
		t.Fatal("NewAvatarPromptRail 不应返回 nil")
	}
	if rail.Priority() != avatarPromptRailPriority {
		t.Errorf("Priority = %d, 期望 %d", rail.Priority(), avatarPromptRailPriority)
	}
	if len(rail.injectedSections) != 0 {
		t.Error("初始 injectedSections 应为空")
	}
}

// TestAvatarPromptRail_嵌入DeepAgentRail 验证可以当作 DeepAgentRail 使用
func TestAvatarPromptRail_嵌入DeepAgentRail(t *testing.T) {
	rail := NewAvatarPromptRail()
	// 确认可以赋值给 DeepAgentRail
	// 确认嵌入的 DeepAgentRail 可以当作 *rails.DeepAgentRail 使用
	_ = (*rails.DeepAgentRail)(&rail.DeepAgentRail)
}
