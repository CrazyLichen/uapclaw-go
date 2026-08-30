package modules

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// defaultTestControllerConfig 创建测试用默认配置
func defaultTestControllerConfig() *config.ControllerConfig {
	return config.DefaultControllerConfig()
}

// mockSessionFacade 模拟会话门面
type mockSessionFacade struct {
	writeStreamCalled bool
	writeCustomCalled bool
	sessionID        string
}

func (m *mockSessionFacade) GetSessionID() string                   { return m.sessionID }
func (m *mockSessionFacade) UpdateState(_ map[string]any)           {}
func (m *mockSessionFacade) GetState(_ state.StateKey) (any, error) { return nil, nil }
func (m *mockSessionFacade) DumpState() map[string]any              { return nil }
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error {
	m.writeStreamCalled = true
	return nil
}
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	m.writeCustomCalled = true
	return nil
}
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any           { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// mockContextEngine 模拟上下文引擎
type mockContextEngine struct {
	contexts map[string]iface.ModelContext
}

func newMockContextEngine() *mockContextEngine {
	return &mockContextEngine{contexts: make(map[string]iface.ModelContext)}
}

func (m *mockContextEngine) GetContext(contextID, sessionID string) iface.ModelContext {
	return m.contexts[contextID]
}

func (m *mockContextEngine) CreateContext(_ context.Context, contextID string, _ sessioninterfaces.SessionFacade, _ ...iface.CreateContextOption) (iface.ModelContext, error) {
	mc := newMockModelContext()
	m.contexts[contextID] = mc
	return mc, nil
}

func (m *mockContextEngine) CompressContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...iface.CompressContextOption) (*iface.CompressContextResult, error) {
	return &iface.CompressContextResult{Result: "noop"}, nil
}

func (m *mockContextEngine) ClearContext(_ context.Context, _ ...iface.ClearContextOption) error {
	return nil
}

func (m *mockContextEngine) SaveContexts(_ context.Context, _ sessioninterfaces.SessionFacade, _ []string) (map[string]any, error) {
	return nil, nil
}

// mockModelContext 模拟模型上下文
type mockModelContext struct {
	messages []llmschema.BaseMessage
}

func newMockModelContext() *mockModelContext {
	return &mockModelContext{messages: make([]llmschema.BaseMessage, 0)}
}

func (m *mockModelContext) Len() int { return len(m.messages) }
func (m *mockModelContext) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	if size <= 0 || size >= len(m.messages) {
		return m.messages, nil
	}
	return m.messages[:size], nil
}
func (m *mockModelContext) SetMessages(messages []llmschema.BaseMessage, withHistory bool) {
	m.messages = messages
}
func (m *mockModelContext) PopMessages(size int, withHistory bool) []llmschema.BaseMessage {
	if size >= len(m.messages) {
		result := m.messages
		m.messages = nil
		return result
	}
	result := m.messages[len(m.messages)-size:]
	m.messages = m.messages[:len(m.messages)-size]
	return result
}
func (m *mockModelContext) ClearMessages(_ context.Context, withHistory bool, _ ...iface.Option) error {
	m.messages = nil
	return nil
}
func (m *mockModelContext) AddMessages(_ context.Context, messages []llmschema.BaseMessage, _ ...iface.Option) ([]llmschema.BaseMessage, error) {
	m.messages = append(m.messages, messages...)
	return messages, nil
}
func (m *mockModelContext) GetContextWindow(_ context.Context, _ []llmschema.BaseMessage, _ []cschema.ToolInfoInterface, _, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return iface.NewContextWindow(), nil
}
func (m *mockModelContext) Statistic() *iface.ContextStats { return &iface.ContextStats{} }
func (m *mockModelContext) SessionID() string              { return "test-session" }
func (m *mockModelContext) ContextID() string              { return "test-context" }
func (m *mockModelContext) TokenCounter() token.TokenCounter  { return nil }
func (m *mockModelContext) ReloaderTool() tool.Tool           { return nil }
func (m *mockModelContext) WorkspaceDir() string              { return "" }
func (m *mockModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade)  {}
func (m *mockModelContext) GetSessionRef() sessioninterfaces.SessionFacade    { return nil }
func (m *mockModelContext) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any { return nil }
func (m *mockModelContext) LoadState(_ map[string]any) {}
func (m *mockModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (*iface.CompressContextResult, error) {
	return &iface.CompressContextResult{Result: "noop"}, nil
}

// mockModelProvider 模型提供者 mock，支持多轮响应
type mockModelProvider struct {
	// responses 按调用顺序返回的响应列表
	responses []*llmschema.AssistantMessage
	// callCount 记录调用次数
	callCount int
	// err 可选：指定某次调用返回错误（在第 errOnCall 次调用时返回）
	err error
	// errOnCall 在第几次调用时返回错误
	errOnCall int
}

func (m *mockModelProvider) Invoke(_ context.Context, messages model_clients.MessagesParam, tools []cschema.ToolInfoInterface) (*llmschema.AssistantMessage, error) {
	m.callCount++
	if m.err != nil && m.callCount == m.errOnCall {
		return nil, m.err
	}
	if len(m.responses) == 0 {
		return llmschema.NewAssistantMessage(""), nil
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewIntentRecognizer 测试意图识别器构造
// 偏差15 修复：对齐 Python，不再接收 abilityMgr 参数
func TestNewIntentRecognizer(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	recognizer := NewIntentRecognizer(cfg, tm, nil)
	assert.NotNil(t, recognizer)
	assert.Equal(t, cfg, recognizer.config)
	assert.Equal(t, tm, recognizer.taskManager)
	assert.NotEmpty(t, recognizer.systemMessage)
	assert.NotEmpty(t, recognizer.userPromptTemplate)
}

// TestIntentRecognizer_Recognize_无ModelProvider 测试无 ModelProvider 时跳过识别
func TestIntentRecognizer_Recognize_无ModelProvider(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	ce := newMockContextEngine()
	recognizer := NewIntentRecognizer(cfg, tm, ce)

	event, err := schema.FromUserInput("帮我查天气")
	require.NoError(t, err)

	sess := &mockSessionFacade{sessionID: "test-session"}
	intents, err := recognizer.Recognize(context.Background(), event, sess)
	assert.NoError(t, err)
	assert.Nil(t, intents)
}

// TestIntentRecognizer_Recognize_单工具调用 测试单次 create_task 工具调用
func TestIntentRecognizer_Recognize_单工具调用(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	ce := newMockContextEngine()
	recognizer := NewIntentRecognizer(cfg, tm, ce)

	// 第一次返回带 tool_calls 的响应，第二次返回空 tool_calls
	firstResp := llmschema.NewAssistantMessage("")
	firstResp.ToolCalls = []*llmschema.ToolCall{
		llmschema.NewToolCall("call-1", "create_task", `{"confidence":0.9,"task_description":"查询天气"}`),
	}
	secondResp := llmschema.NewAssistantMessage("已完成")

	provider := &mockModelProvider{
		responses: []*llmschema.AssistantMessage{firstResp, secondResp},
	}
	recognizer.SetModelProvider(provider)

	event, err := schema.FromUserInput("帮我查天气")
	require.NoError(t, err)

	sess := &mockSessionFacade{sessionID: "test-session"}
	intents, err := recognizer.Recognize(context.Background(), event, sess)
	assert.NoError(t, err)
	assert.Len(t, intents, 1)
	assert.Equal(t, schema.IntentCreateTask, intents[0].IntentType)
	assert.Contains(t, intents[0].TargetTaskDescription, "查询天气")
}

// TestIntentRecognizer_Recognize_非InputEvent 测试非 InputEvent 类型返回错误
func TestIntentRecognizer_Recognize_非InputEvent(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	ce := newMockContextEngine()
	recognizer := NewIntentRecognizer(cfg, tm, ce)

	provider := &mockModelProvider{}
	recognizer.SetModelProvider(provider)

	// 使用 TaskFailedEvent 而非 InputEvent
	event := &schema.TaskFailedEvent{ErrorMessage: "test"}
	sess := &mockSessionFacade{sessionID: "test-session"}
	_, err := recognizer.Recognize(context.Background(), event, sess)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "InputEvent")
}

// TestIntentRecognizer_Recognize_LLM调用失败 测试 LLM 调用返回错误
func TestIntentRecognizer_Recognize_LLM调用失败(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	ce := newMockContextEngine()
	recognizer := NewIntentRecognizer(cfg, tm, ce)

	provider := &mockModelProvider{
		err:      fmt.Errorf("LLM 服务不可用"),
		errOnCall: 1,
	}
	recognizer.SetModelProvider(provider)

	event, err := schema.FromUserInput("帮我查天气")
	require.NoError(t, err)

	sess := &mockSessionFacade{sessionID: "test-session"}
	_, err = recognizer.Recognize(context.Background(), event, sess)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM 服务不可用")
}

// TestIntentRecognizer_SetModelProvider 测试设置模型提供者
func TestIntentRecognizer_SetModelProvider(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	recognizer := NewIntentRecognizer(cfg, tm, nil)

	assert.Nil(t, recognizer.modelProvider)
	mockProvider := &mockModelProvider{}
	recognizer.SetModelProvider(mockProvider)
	assert.Equal(t, mockProvider, recognizer.modelProvider)
}

// TestNewEventHandlerWithIntentRecognition 测试创建基于意图识别的事件处理器
func TestNewEventHandlerWithIntentRecognition(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	assert.NotNil(t, handler)
	assert.Nil(t, handler.recognizer)
}

// TestEventHandlerWithIntentRecognition_InitRecognizer 测试初始化识别器
func TestEventHandlerWithIntentRecognition_InitRecognizer(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)

	handler := NewEventHandlerWithIntentRecognition()
	handler.Config = cfg
	handler.TaskManager = tm
	handler.InitRecognizer()

	assert.NotNil(t, handler.recognizer)
	assert.Equal(t, cfg, handler.recognizer.config)
	assert.Equal(t, tm, handler.recognizer.taskManager)
}

// TestEventHandlerWithIntentRecognition_HandleTaskInteraction_类型正确 测试处理任务交互事件（类型正确）
func TestEventHandlerWithIntentRecognition_HandleTaskInteraction_类型正确(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	sess := &mockSessionFacade{}

	interaction := []schema.DataFrame{&schema.TextDataFrame{Text: "请提供更多信息"}}
	event := &schema.TaskInteractionEvent{
		Interaction: interaction,
	}
	input := &EventHandlerInput{Event: event, Session: sess}

	result, err := handler.HandleTaskInteraction(context.Background(), input)
	assert.NoError(t, err)
	assert.Nil(t, result)
	assert.True(t, sess.writeStreamCalled)
}

// TestEventHandlerWithIntentRecognition_HandleTaskInteraction_类型错误 测试处理任务交互事件（类型错误）
func TestEventHandlerWithIntentRecognition_HandleTaskInteraction_类型错误(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	sess := &mockSessionFacade{}

	inputEvent, _ := schema.FromUserInput("hello")
	input := &EventHandlerInput{Event: inputEvent, Session: sess}

	_, err := handler.HandleTaskInteraction(context.Background(), input)
	assert.Error(t, err)
}

// TestEventHandlerWithIntentRecognition_HandleTaskCompletion_类型正确 测试处理任务完成事件
func TestEventHandlerWithIntentRecognition_HandleTaskCompletion_类型正确(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	sess := &mockSessionFacade{}

	taskResult := []schema.DataFrame{&schema.TextDataFrame{Text: "结果"}}
	event := &schema.TaskCompletionEvent{
		TaskResult: taskResult,
	}
	input := &EventHandlerInput{Event: event, Session: sess}

	result, err := handler.HandleTaskCompletion(context.Background(), input)
	assert.NoError(t, err)
	assert.Nil(t, result)
	assert.True(t, sess.writeStreamCalled)
}

// TestEventHandlerWithIntentRecognition_HandleTaskCompletion_类型错误 测试处理任务完成事件（类型错误）
func TestEventHandlerWithIntentRecognition_HandleTaskCompletion_类型错误(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	sess := &mockSessionFacade{}

	inputEvent, _ := schema.FromUserInput("hello")
	input := &EventHandlerInput{Event: inputEvent, Session: sess}

	_, err := handler.HandleTaskCompletion(context.Background(), input)
	assert.Error(t, err)
}

// TestEventHandlerWithIntentRecognition_HandleTaskFailed_类型正确 测试处理任务失败事件
func TestEventHandlerWithIntentRecognition_HandleTaskFailed_类型正确(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	sess := &mockSessionFacade{}

	event := &schema.TaskFailedEvent{
		ErrorMessage: "执行失败",
	}
	input := &EventHandlerInput{Event: event, Session: sess}

	result, err := handler.HandleTaskFailed(context.Background(), input)
	assert.NoError(t, err)
	assert.Nil(t, result)
	assert.True(t, sess.writeStreamCalled)
}

// TestEventHandlerWithIntentRecognition_HandleTaskFailed_类型错误 测试处理任务失败事件（类型错误）
func TestEventHandlerWithIntentRecognition_HandleTaskFailed_类型错误(t *testing.T) {
	handler := NewEventHandlerWithIntentRecognition()
	sess := &mockSessionFacade{}

	inputEvent, _ := schema.FromUserInput("hello")
	input := &EventHandlerInput{Event: inputEvent, Session: sess}

	_, err := handler.HandleTaskFailed(context.Background(), input)
	assert.Error(t, err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestIntentRecognizer_prepareUserMessage 测试构建用户消息
func TestIntentRecognizer_prepareUserMessage(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	recognizer := NewIntentRecognizer(cfg, tm, nil)

	// 无任务
	msg, err := recognizer.prepareUserMessage(context.Background(), "帮我查天气")
	require.NoError(t, err)
	assert.Contains(t, msg, "无")
	assert.Contains(t, msg, "帮我查天气")

	// 有任务
	task := schema.NewTask("session-1", "default_task_type")
	task.Description = "查询天气"
	task.Status = schema.TaskSubmitted
	require.NoError(t, tm.AddTask(context.Background(), task))

	msg, err = recognizer.prepareUserMessage(context.Background(), "更新天气")
	require.NoError(t, err)
	assert.Contains(t, msg, "查询天气")
	assert.Contains(t, msg, "更新天气")
}

// TestInvokeToolkitMethod 测试工具方法分发
func TestInvokeToolkitMethod(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	ce := newMockContextEngine()
	recognizer := NewIntentRecognizer(cfg, tm, ce)

	event, err := schema.FromUserInput("测试")
	require.NoError(t, err)
	toolkits := NewIntentToolkits(event, 0.5)

	tests := []struct {
		name      string
		toolName  string
		args      string
		wantErr   bool
		intentType schema.IntentType
	}{
		{
			name:     "create_task",
			toolName: "create_task",
			args:     `{"confidence":0.9,"task_description":"测试任务"}`,
			wantErr:  false,
			intentType: schema.IntentCreateTask,
		},
		{
			name:     "pause_task",
			toolName: "pause_task",
			args:     `{"confidence":0.9,"task_id":"task-1"}`,
			wantErr:  false,
			intentType: schema.IntentPauseTask,
		},
		{
			name:     "cancel_task",
			toolName: "cancel_task",
			args:     `{"confidence":0.9,"task_id":"task-1"}`,
			wantErr:  false,
			intentType: schema.IntentCancelTask,
		},
		{
			name:     "resume_task",
			toolName: "resume_task",
			args:     `{"confidence":0.9,"task_id":"task-1"}`,
			wantErr:  false,
			intentType: schema.IntentResumeTask,
		},
		{
			name:     "unknown_task",
			toolName: "unknown_task",
			args:     `{"confidence":0.9,"question_for_user":"你想做什么？"}`,
			wantErr:  false,
			intentType: schema.IntentUnknownTask,
		},
		{
			name:     "create_dependent_task",
			toolName: "create_dependent_task",
			args:     `{"confidence":0.9,"task_description":"依赖任务","dependent_task_ids":["t1","t2"]}`,
			wantErr:  false,
			intentType: schema.IntentContinueTask,
		},
		{
			name:     "modify_task",
			toolName: "modify_task",
			args:     `{"confidence":0.9,"task_id":"task-1","new_task_description":"新描述"}`,
			wantErr:  false,
			intentType: schema.IntentModifyTask,
		},
		{
			name:     "supplement_task",
			toolName: "supplement_task",
			args:     `{"confidence":0.9,"task_id":"task-1","supplement_info":"补充信息"}`,
			wantErr:  false,
			intentType: schema.IntentSupplementTask,
		},
		{
			name:     "未知工具",
			toolName: "nonexistent_tool",
			args:     `{"confidence":0.9}`,
			wantErr:  true,
		},
		{
			name:     "参数解析失败",
			toolName: "create_task",
			args:     `invalid json`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, _, err := recognizer.invokeToolkitMethod(toolkits, tt.toolName, tt.args)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, intent)
			assert.Equal(t, tt.intentType, intent.IntentType)
		})
	}
}

// TestToFloat64 测试 toFloat64 辅助函数
func TestToFloat64(t *testing.T) {
	assert.Equal(t, 0.0, toFloat64(nil))
	assert.Equal(t, 1.5, toFloat64(1.5))
	assert.Equal(t, 1.0, toFloat64(1))
	assert.Equal(t, 2.0, toFloat64(int64(2)))
	assert.Equal(t, 0.5, toFloat64(float32(0.5)))
	assert.Equal(t, 0.0, toFloat64("not a number"))
}

// TestToStringSlice 测试 toStringSlice 辅助函数
func TestToStringSlice(t *testing.T) {
	assert.Nil(t, toStringSlice(nil))
	assert.Nil(t, toStringSlice("not a slice"))
	assert.Empty(t, toStringSlice([]any{}))

	result := toStringSlice([]any{"a", "b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)

	// 混合类型，仅保留 string
	mixed := toStringSlice([]any{"a", 123, "b"})
	assert.Equal(t, []string{"a", "b"}, mixed)
}
