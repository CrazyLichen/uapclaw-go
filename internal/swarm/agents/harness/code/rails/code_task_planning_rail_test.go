package rails

import (
	"context"
	"errors"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	result "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeRailAgent 仅实现 rail 需要的 BaseAgent 子集。
type fakeRailAgent struct {
	agentinterfaces.BaseAgent
	// manager 动态提示词管理器
	manager *hprompts.Manager
	// builder 系统提示词构建器
	builder saprompt.SystemPromptBuilderInterface
}

// PromptAttachments 返回动态提示词管理器
func (f *fakeRailAgent) PromptAttachments() *hprompts.Manager { return f.manager }

// SystemPromptBuilder 返回系统提示词构建器
func (f *fakeRailAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return f.builder }

// fakeRailSession 仅提供会话 ID 的 SessionFacade。
type fakeRailSession struct {
	sessioninterfaces.SessionFacade
	// sessionID 会话标识
	sessionID string
}

// GetSessionID 返回会话标识
func (f *fakeRailSession) GetSessionID() string { return f.sessionID }

// fakeModelContext 仅实现 GetMessages/SetMessages 的 ModelContext。
type fakeModelContext struct {
	ceiface.ModelContext
	// messages 当前消息
	messages []llmschema.BaseMessage
}

// GetMessages 返回消息列表
func (f *fakeModelContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return f.messages, nil
}

// SetMessages 替换消息列表
func (f *fakeModelContext) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	f.messages = messages
}

// stubFs 仅覆盖 ReadFile 的 FsOperation 桩（todo 存储读失败即视为空列表）。
type stubFs struct {
	sysop.FsOperation
}

// ReadFile 返回错误（无待办文件）
func (s *stubFs) ReadFile(_ context.Context, _ string, _ ...sysop.FsOption) (*result.ReadFileResult, error) {
	return nil, errors.New("no todo file")
}

// fakePromptBuilder 记录注入的节名。
type fakePromptBuilder struct {
	// removed 被移除的节名
	removed []string
}

// AddSection 记录但不落库
func (f *fakePromptBuilder) AddSection(_ saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	return nil
}

// RemoveSection 记录移除
func (f *fakePromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	f.removed = append(f.removed, name)
	return nil
}

// Language 返回中文
func (f *fakePromptBuilder) Language() string { return "cn" }

// SetLanguage 忽略
func (f *fakePromptBuilder) SetLanguage(_ string) {}

// GetSection 返回 nil
func (f *fakePromptBuilder) GetSection(_ string) *saprompt.PromptSection { return nil }

// HasSection 恒 false
func (f *fakePromptBuilder) HasSection(_ string) bool { return false }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewCodeTaskPlanningRail_默认值 验证提醒阈值与父类约定。
func TestNewCodeTaskPlanningRail_默认值(t *testing.T) {
	rail := NewCodeTaskPlanningRail("cn")
	require.NotNil(t, rail)
	require.NotNil(t, rail.TaskPlanningRail)
	require.Equal(t, 10, rail.turnsSinceManagement)
	require.Equal(t, 10, rail.turnsBetweenReminders)
	require.Equal(t, 1000, rail.maxTrackedSessions)
	require.False(t, rail.TaskPlanningRail.InjectPromptEnabled(), "code 模式不注入静态 todo 节")
}

// TestCodeTaskPlanningRail_BeforeModelCall_未达阈值不提醒 验证 9 轮内不写附件。
func TestCodeTaskPlanningRail_BeforeModelCall_未达阈值不提醒(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(8, 8)
	env.turn()

	items, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Empty(t, items, "未达阈值不得写提醒附件")
}

// TestCodeTaskPlanningRail_BeforeModelCall_达阈值提醒 验证第 10 轮写入提醒附件。
func TestCodeTaskPlanningRail_BeforeModelCall_达阈值提醒(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(9, 9)
	env.turn()

	items, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, taskReminderSection, items[0].Section)
	require.Equal(t, hprompts.AttachmentTodoReminder, items[0].Kind)
	require.Equal(t, taskReminderSource, items[0].Source)
	require.Equal(t, taskReminderPriority, items[0].Priority)
	require.Contains(t, items[0].Content, taskReminderCurrentPrefix)
	require.Contains(t, items[0].Content, "<!-- task-reminder-delivery:1 -->")
	require.Equal(t, 1, env.rail.states[env.sessionID].DeliverySequence)
}

// TestCodeTaskPlanningRail_BeforeModelCall_内容不变不重复 验证提醒后不足阈值不新增。
func TestCodeTaskPlanningRail_BeforeModelCall_内容不变不重复(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(9, 9)
	env.turn()
	first, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, first, 1)

	env.turn()
	env.turn()
	items, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, items, 1, "内容未变时同名 section 覆盖而非追加")
	require.Equal(t, first[0].Content, items[0].Content)

	// 再满 10 轮 → 追加第二次提醒（序号递增）
	for i := 0; i < 10; i++ {
		env.turn()
	}
	items, err = env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Contains(t, items[0].Content, "<!-- task-reminder-delivery:2 -->")
}

// TestCodeTaskPlanningRail_AfterToolCall_重置计数 验证 todo 工具调用后计数归零。
func TestCodeTaskPlanningRail_AfterToolCall_重置计数(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(1, 1)
	env.turn()
	require.Equal(t, 2, env.rail.states[env.sessionID].TurnsSinceManagement)

	cbc := env.context(&agentinterfaces.ToolCallInputs{ToolName: "todo_create"})
	require.NoError(t, env.rail.AfterToolCall(context.Background(), cbc))
	require.Equal(t, 0, env.rail.states[env.sessionID].TurnsSinceManagement)

	// 非 todo 工具不重置
	env.turn()
	other := env.context(&agentinterfaces.ToolCallInputs{ToolName: "read_file"})
	require.NoError(t, env.rail.AfterToolCall(context.Background(), other))
	require.Equal(t, 1, env.rail.states[env.sessionID].TurnsSinceManagement)
}

// TestCodeTaskPlanningRail_OnUserMessage恢复附件 验证管理器被清空后按状态恢复。
func TestCodeTaskPlanningRail_OnUserMessage恢复附件(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(9, 9)
	env.turn()
	require.NoError(t, func() error {
		_, err := env.manager.ClearSection(env.sessionID, taskReminderSection)
		return err
	}())

	require.NoError(t, env.rail.OnUserMessage(context.Background(), env.context(nil)))
	items, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, items, 1, "OnUserMessage 必须恢复提醒附件")
	require.Contains(t, items[0].Content, taskReminderCurrentPrefix)
}

// TestCodeTaskPlanningRail_状态LRU淘汰 验证跟踪上限淘汰最旧会话。
func TestCodeTaskPlanningRail_状态LRU淘汰(t *testing.T) {
	rail := NewCodeTaskPlanningRail("cn")
	rail.maxTrackedSessions = 2
	rail.saveState("s1", &taskReminderState{Content: "a"})
	rail.saveState("s2", &taskReminderState{Content: "b"})
	rail.saveState("s3", &taskReminderState{Content: "c"})

	_, ok := rail.states["s1"]
	require.False(t, ok, "超限后最旧会话应被淘汰")
	_, ok = rail.states["s3"]
	require.True(t, ok)
	require.Len(t, rail.states, 2)
}

// TestCodeTaskPlanningRail_markIterationCounted 验证同一 ReAct 迭代只计一次。
func TestCodeTaskPlanningRail_markIterationCounted(t *testing.T) {
	rail := NewCodeTaskPlanningRail("cn")
	env := newRailTestEnv(t)
	env.rail = rail

	cbc := env.context(nil)
	cbc.SetExtra(reactIterationExtraKey, 3)
	require.True(t, rail.markIterationCounted(cbc))
	require.False(t, rail.markIterationCounted(cbc), "同一迭代号不得重复计数")

	cbc.SetExtra(reactIterationExtraKey, 4)
	require.True(t, rail.markIterationCounted(cbc))
}

// TestDeliverySequenceFromContent 验证投递序号解析。
func TestDeliverySequenceFromContent(t *testing.T) {
	require.Equal(t, 0, deliverySequenceFromContent(""))
	require.Equal(t, 0, deliverySequenceFromContent("没有标记"))
	require.Equal(t, 0, deliverySequenceFromContent("<!-- task-reminder-delivery:abc -->"))
	require.Equal(t, 7, deliverySequenceFromContent("正文\n\n<!-- task-reminder-delivery:7 -->"))
}

// TestTaskReminderContentCandidates 验证内容候选切分（含 legacy 前缀）。
func TestTaskReminderContentCandidates(t *testing.T) {
	rendered := "前缀\n\n" + taskReminderCurrentPrefix + " 正文\n\n---\n\n其他节"
	candidates := taskReminderContentCandidates(rendered)
	require.NotEmpty(t, candidates)
	require.Equal(t, taskReminderCurrentPrefix+" 正文", candidates[0])

	legacy := taskReminderLegacyPrefix + " 旧正文"
	legacyCandidates := taskReminderContentCandidates(legacy)
	require.NotEmpty(t, legacyCandidates)
	require.Equal(t, legacy, legacyCandidates[len(legacyCandidates)-1])

	require.Empty(t, taskReminderContentCandidates("无关内容"))
}

// TestBuildLegacyTaskReminderContent 验证旧版内容重建（含待办列表）。
func TestBuildLegacyTaskReminderContent(t *testing.T) {
	empty := buildLegacyTaskReminderContent(nil)
	require.Contains(t, empty, taskReminderLegacyPrefix)
	require.NotContains(t, empty, "Here are the existing tasks")

	todos := []hschema.TodoItem{
		{ID: "backend", Content: "实现后端", Status: hschema.TodoStatusInProgress},
		{ID: "verify", Content: "验证", Status: hschema.TodoStatusPending},
	}
	got := buildLegacyTaskReminderContent(todos)
	require.Contains(t, got, "Here are the existing tasks:")
	require.Contains(t, got, "#backend. [in_progress] 实现后端")
	require.Contains(t, got, "#verify. [pending] 验证")
}

// TestNonNegativeInt 验证非负整数转换。
func TestNonNegativeInt(t *testing.T) {
	require.Equal(t, 0, nonNegativeInt(nil))
	require.Equal(t, 0, nonNegativeInt(-3))
	require.Equal(t, 5, nonNegativeInt(5))
	require.Equal(t, 5, nonNegativeInt(int64(5)))
	require.Equal(t, 5, nonNegativeInt(float64(5)))
	require.Equal(t, 5, nonNegativeInt("5"))
	require.Equal(t, 0, nonNegativeInt("x"))
	require.Equal(t, 0, nonNegativeInt(struct{}{}))
}

// TestLastTodoResultIndex 验证最后一条 todo 工具结果定位。
func TestLastTodoResultIndex(t *testing.T) {
	assistant := llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "调用工具")
	assistantMsg := &llmschema.AssistantMessage{
		DefaultMessage: *assistant,
		ToolCalls: []*llmschema.ToolCall{
			{ID: "c1", Name: "read_file"},
			{ID: "c2", Name: "todo_create"},
		},
	}
	messages := []llmschema.BaseMessage{
		assistantMsg,
		llmschema.NewToolMessage("c1", "ok"),
		llmschema.NewToolMessage("c2", "todo ok"),
		llmschema.NewUserMessage("继续"),
	}
	require.Equal(t, 2, lastTodoResultIndex(messages))
	require.Equal(t, -1, lastTodoResultIndex([]llmschema.BaseMessage{llmschema.NewUserMessage("x")}))
}

// TestStateFromHistory 验证从历史推导计数（无工具结果时以助手消息数计）。
func TestStateFromHistory(t *testing.T) {
	env := newRailTestEnv(t)
	cbc := env.context(nil)
	mc := &fakeModelContext{messages: []llmschema.BaseMessage{
		llmschema.NewUserMessage("第1轮"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "答1"),
		llmschema.NewUserMessage("第2轮"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "答2"),
	}}
	cbc.SetModelContext(mc)

	state := stateFromHistory(cbc, env.sessionID)
	require.Equal(t, 2, state.TurnsSinceManagement)
	require.Equal(t, 2, state.TurnsSinceReminder)
}

// TestCodeTaskPlanningRail_GetCallbacks 验证覆盖父类回调。
func TestCodeTaskPlanningRail_GetCallbacks(t *testing.T) {
	rail := NewCodeTaskPlanningRail("cn")
	callbacks := rail.GetCallbacks()
	require.Contains(t, callbacks, agentinterfaces.CallbackOnUserMessage)
	require.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
	require.Contains(t, callbacks, agentinterfaces.CallbackAfterToolCall)
	require.NoError(t, callbacks[agentinterfaces.CallbackBeforeModelCall](context.Background(), nilContextErrorCtx()))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// railTestEnv code rail 单测环境。
type railTestEnv struct {
	t         *testing.T
	rail      *CodeTaskPlanningRail
	manager   *hprompts.Manager
	agent     *fakeRailAgent
	sessionID string
	modelCtx  *fakeModelContext
	builder   *fakePromptBuilder
}

// newRailTestEnv 构建单测环境并绑定可用的 todo 工具基类。
func newRailTestEnv(t *testing.T) *railTestEnv {
	t.Helper()
	manager := hprompts.NewManager()
	builder := &fakePromptBuilder{}
	agent := &fakeRailAgent{manager: manager, builder: builder}
	_, todoTool := todo.CreateTodosTool(t.TempDir(), &stubFs{}, "cn", "agent-1")

	rail := NewCodeTaskPlanningRail("cn")
	rail.BindTodoTool(&todoTool)
	return &railTestEnv{
		t:         t,
		rail:      rail,
		manager:   manager,
		agent:     agent,
		sessionID: "sess-1",
		modelCtx:  &fakeModelContext{},
		builder:   builder,
	}
}

// context 构造带会话与模型上下文的回调上下文。
func (e *railTestEnv) context(inputs agentinterfaces.EventInputs) *agentinterfaces.AgentCallbackContext {
	cbc := agentinterfaces.NewAgentCallbackContext(e.agent, inputs, &fakeRailSession{sessionID: e.sessionID})
	cbc.SetModelContext(e.modelCtx)
	return cbc
}

// seedState 预置提醒计数状态（模拟历史累积）。
func (e *railTestEnv) seedState(turnsSinceManagement, turnsSinceReminder int) {
	e.rail.saveState(e.sessionID, &taskReminderState{
		TurnsSinceManagement: turnsSinceManagement,
		TurnsSinceReminder:   turnsSinceReminder,
	})
}

// turn 执行一轮 BeforeModelCall。
func (e *railTestEnv) turn() {
	e.t.Helper()
	require.NoError(e.t, e.rail.BeforeModelCall(context.Background(), e.context(nil)))
}

// nilContextErrorCtx 返回用于空入参回调的上下文。
func nilContextErrorCtx() any {
	return (*agentinterfaces.AgentCallbackContext)(nil)
}

// TestTaskReminderHash 验证哈希对同一内容稳定、对不同内容变化。
func TestTaskReminderHash(t *testing.T) {
	a := taskReminderHash("内容A", "s1")
	b := taskReminderHash("内容A", "s1")
	c := taskReminderHash("内容B", "s1")
	require.NotEmpty(t, a)
	require.Equal(t, a, b)
	require.NotEqual(t, a, c)
}

// TestLatestTaskReminderHistoryState 验证交付账本解析与变更下标。
func TestLatestTaskReminderHistoryState(t *testing.T) {
	content := buildTaskReminderContent()
	msg := attachmentMessage(content, map[string]string{taskReminderSection: taskReminderHash(content, "s1")}, "s1")
	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("第1轮"),
		msg,
		llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "答"),
	}
	hash, index := latestTaskReminderHistoryState(messages, "s1")
	require.Equal(t, taskReminderHash(content, "s1"), hash)
	require.Equal(t, 1, index)

	// 其他会话的附件不参与
	other := attachmentMessage(content, map[string]string{taskReminderSection: "x"}, "s2")
	require.Equal(t, "", func() string {
		h, _ := latestTaskReminderHistoryState([]llmschema.BaseMessage{other}, "s1")
		return h
	}())
}

// TestTaskReminderPayloadIndex 验证按哈希定位提醒消息下标。
func TestTaskReminderPayloadIndex(t *testing.T) {
	content := buildTaskReminderContent()
	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("第1轮"),
		attachmentMessage(content, nil, "s1"),
	}
	target := taskReminderHash(content, "s1")
	require.Equal(t, 1, taskReminderPayloadIndex(messages, "s1", target))
	// 哈希不匹配时回落到「含提醒前缀」的截断消息
	require.Equal(t, 1, taskReminderPayloadIndex(messages, "s1", "不存在的哈希"))
}

// TestCodeTaskPlanningRail_从历史恢复提醒 验证状态为空时按历史哈希恢复内容。
func TestCodeTaskPlanningRail_从历史恢复提醒(t *testing.T) {
	env := newRailTestEnv(t)
	content := buildTaskReminderContent() + "\n\n<!-- task-reminder-delivery:3 -->"
	env.modelCtx.messages = []llmschema.BaseMessage{
		llmschema.NewUserMessage("第1轮"),
		attachmentMessage(content, map[string]string{taskReminderSection: taskReminderHash(content, env.sessionID)}, env.sessionID),
		llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "答"),
	}

	require.NoError(t, env.rail.OnUserMessage(context.Background(), env.context(nil)))
	items, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, content, items[0].Content)
	require.Equal(t, 3, env.rail.states[env.sessionID].DeliverySequence)
}

// TestCodeTaskPlanningRail_管理器状态领先时回写附件 验证管理器序号落后时不覆盖状态。
func TestCodeTaskPlanningRail_管理器状态领先时回写附件(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(0, 0)
	env.rail.states[env.sessionID].Content = "权威内容"
	env.rail.states[env.sessionID].DeliverySequence = 5
	// 管理器里是旧内容（序号 1）
	_, err := env.manager.AddSection(env.sessionID, taskReminderSection, "旧内容",
		hprompts.AttachmentTodoReminder, taskReminderSource,
		hprompts.WithAttachmentMetadata(map[string]any{"delivery_sequence": 1}))
	require.NoError(t, err)

	require.NoError(t, env.rail.OnUserMessage(context.Background(), env.context(nil)))
	items, err := env.manager.ListByFilter(hprompts.AttachmentFilter{SessionID: env.sessionID})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "权威内容", items[0].Content, "状态领先时必须以状态回写附件")
}

// TestCodeTaskPlanningRail_历史状态覆盖缓存 验证管理器新内容回填状态。
func TestCodeTaskPlanningRail_历史状态覆盖缓存(t *testing.T) {
	env := newRailTestEnv(t)
	_, err := env.manager.AddSection(env.sessionID, taskReminderSection, "管理器内容",
		hprompts.AttachmentTodoReminder, taskReminderSource,
		hprompts.WithAttachmentMetadata(map[string]any{"delivery_sequence": 4, "item_count": 2}))
	require.NoError(t, err)

	require.NoError(t, env.rail.OnUserMessage(context.Background(), env.context(nil)))
	state := env.rail.states[env.sessionID]
	require.Equal(t, "管理器内容", state.Content)
	require.Equal(t, 4, state.DeliverySequence)
	require.Equal(t, 2, state.ItemCount)
}

// TestCodeTaskPlanningRail_回调入口 验证 GetCallbacks 的闭包正确转发。
func TestCodeTaskPlanningRail_回调入口(t *testing.T) {
	env := newRailTestEnv(t)
	callbacks := env.rail.GetCallbacks()
	cbc := env.context(nil)
	require.NoError(t, callbacks[agentinterfaces.CallbackOnUserMessage](context.Background(), cbc))
	require.NoError(t, callbacks[agentinterfaces.CallbackBeforeModelCall](context.Background(), cbc))

	toolCbc := env.context(&agentinterfaces.ToolCallInputs{ToolName: "todo_list"})
	require.NoError(t, callbacks[agentinterfaces.CallbackAfterToolCall](context.Background(), toolCbc))
}

// TestCodeTaskPlanningRail_无会话或无管理器跳过 验证守卫分支。
func TestCodeTaskPlanningRail_无会话或无管理器跳过(t *testing.T) {
	rail := NewCodeTaskPlanningRail("cn")
	require.NoError(t, rail.OnUserMessage(context.Background(), nil))
	require.NoError(t, rail.BeforeModelCall(context.Background(), nil))

	noSession := agentinterfaces.NewAgentCallbackContext(&fakeRailAgent{manager: hprompts.NewManager()}, nil, nil)
	require.NoError(t, rail.BeforeModelCall(context.Background(), noSession))

	noManager := agentinterfaces.NewAgentCallbackContext(&fakeRailAgent{}, nil, &fakeRailSession{sessionID: "s1"})
	require.NoError(t, rail.OnUserMessage(context.Background(), noManager))
}

// TestIsAttachmentHistoryMessage 验证附件身份判定。
func TestIsAttachmentHistoryMessage(t *testing.T) {
	require.False(t, isAttachmentHistoryMessage(nil, "s1"))
	require.False(t, isAttachmentHistoryMessage(llmschema.NewUserMessage("普通"), "s1"))

	noSession := llmschema.NewUserMessage("x", llmschema.WithMetadata(map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
	}))
	require.True(t, isAttachmentHistoryMessage(noSession, "s1"))

	otherSession := attachmentMessage("x", nil, "s2")
	require.False(t, isAttachmentHistoryMessage(otherSession, "s1"))
}

// attachmentMessage 构造带附件标记的历史消息。
func attachmentMessage(content string, state map[string]string, sessionID string) llmschema.BaseMessage {
	meta := map[string]any{
		hprompts.MetadataKeyAttachmentHistory: true,
		hprompts.MetadataKeyMode:              hprompts.ModeSnapshot,
		hprompts.MetadataKeySessionID:         sessionID,
	}
	if state != nil {
		meta[hprompts.MetadataKeyState] = state
	}
	return llmschema.NewUserMessage(content, llmschema.WithMetadata(meta))
}

// TestCodeTaskPlanningRail_OnUserMessage仍生效 回归：既有 rail 不感知 Inputs 类型变化。
// 25.0a 把 on_user_message 的 ctx.inputs 从 *InvokeInputs 换成 *UserMessageInputs，
// 并把触发点收敛到 admitUserMessage。该 rail 的 OnUserMessage 只读
// cbc.Agent()/Session()，不读 cbc.inputs，因此必须照常工作。
func TestCodeTaskPlanningRail_OnUserMessage仍生效(t *testing.T) {
	env := newRailTestEnv(t)
	env.seedState(5, 5)

	inputs := &agentinterfaces.UserMessageInputs{Parts: []string{"hi"}, Source: "query"}
	require.NotPanics(t, func() {
		require.NoError(t, env.rail.OnUserMessage(context.Background(), env.context(inputs)))
	})
}

// GetCacheIdentity 返回零值缓存身份（测试替身，不参与 KV 亲和）。
func (f *fakeRailSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回空父会话（测试替身）。
func (f *fakeRailSession) GetParentSessionID() string { return "" }
