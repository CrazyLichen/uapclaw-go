package modules

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
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
	sessionID         string
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

// TestIntentRecognizer_Recognize_骨架 测试识别意图骨架（当前返回 nil）
func TestIntentRecognizer_Recognize_骨架(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	recognizer := NewIntentRecognizer(cfg, tm, nil)

	event, err := schema.FromUserInput("帮我查天气")
	require.NoError(t, err)

	intents, err := recognizer.Recognize(context.Background(), event, nil)
	assert.NoError(t, err)
	assert.Nil(t, intents) // 骨架实现，⤵️ 6.23 回填
}

// TestIntentRecognizer_SetModelProvider 测试设置模型提供者
func TestIntentRecognizer_SetModelProvider(t *testing.T) {
	cfg := defaultTestControllerConfig()
	tm := NewTaskManager(cfg)
	recognizer := NewIntentRecognizer(cfg, tm, nil)

	assert.Nil(t, recognizer.modelProvider)
	// 注入 mock provider（验证接口可注入）
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

// mockModelProvider 模型提供者桩实现
type mockModelProvider struct{}

func (m *mockModelProvider) Invoke(_ context.Context, _ []any, _ []map[string]any) (any, error) {
	return nil, nil
}
