package modules

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSessionFacade 测试用 SessionFacade 桩实现
type fakeSessionFacade struct {
	// sessionID 会话标识
	sessionID string
}

// fakeEventHandler 测试用 EventHandler 实现，记录各方法调用次数
type fakeEventHandler struct {
	EventHandlerBase
	handledInput           atomic.Int32
	handledTaskInteraction atomic.Int32
	handledTaskCompletion  atomic.Int32
	handledTaskFailed      atomic.Int32
	handledFollowUp        atomic.Int32
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSessionID 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetSessionID() string { return f.sessionID }

// UpdateState 实现 SessionFacade 接口
func (f *fakeSessionFacade) UpdateState(_ map[string]any) {}

// GetState 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetState(_ state.StateKey) (any, error) { return nil, nil }

// DumpState 实现 SessionFacade 接口
func (f *fakeSessionFacade) DumpState() map[string]any { return nil }

// WriteStream 实现 SessionFacade 接口
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 实现 SessionFacade 接口
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }

// Interact 实现 SessionFacade 接口
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// HandleInput 实现 EventHandler 接口
func (h *fakeEventHandler) HandleInput(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledInput.Add(1)
	return map[string]any{"status": "handled_input"}, nil
}

// HandleTaskInteraction 实现 EventHandler 接口
func (h *fakeEventHandler) HandleTaskInteraction(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledTaskInteraction.Add(1)
	return map[string]any{"status": "handled_task_interaction"}, nil
}

// HandleTaskCompletion 实现 EventHandler 接口
func (h *fakeEventHandler) HandleTaskCompletion(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledTaskCompletion.Add(1)
	return map[string]any{"status": "handled_task_completion"}, nil
}

// HandleTaskFailed 实现 EventHandler 接口
func (h *fakeEventHandler) HandleTaskFailed(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledTaskFailed.Add(1)
	return map[string]any{"status": "handled_task_failed"}, nil
}

// HandleFollowUp 实现 EventHandler 接口
func (h *fakeEventHandler) HandleFollowUp(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	h.handledFollowUp.Add(1)
	return map[string]any{"status": "handled_follow_up"}, nil
}

// GetBase 实现 EventHandler 接口
func (h *fakeEventHandler) GetBase() *EventHandlerBase {
	return &h.EventHandlerBase
}

// PrepareRound 实现 EventHandler 接口
func (h *fakeEventHandler) PrepareRound() int { return 0 }

// WaitCompletion 实现 EventHandler 接口
func (h *fakeEventHandler) WaitCompletion(_ context.Context, _ time.Duration) map[string]any {
	return map[string]any{"status": "completed"}
}

// OnAbort 实现 EventHandler 接口
func (h *fakeEventHandler) OnAbort() {}

// TestEventQueue_订阅取消 测试 Subscribe 后可发布，Unsubscribe 后发布报错
func TestEventQueue_订阅取消(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	eq.Start()
	defer func() { _ = eq.Stop(context.Background()) }()

	handler := &fakeEventHandler{}
	eq.SetEventHandler(handler)

	agentID := "agent-1"
	sessionID := "sess-1"
	sess := &fakeSessionFacade{sessionID: sessionID}

	// 订阅
	err := eq.Subscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 订阅后可发布
	event := &schema.InputEvent{BaseEvent: *schema.NewBaseEvent(schema.EventInput)}
	err = eq.PublishEventAsync(context.Background(), agentID, sess, event)
	require.NoError(t, err)

	// 等待异步处理完成
	assert.Eventually(t, func() bool { return handler.handledInput.Load() >= 1 }, 2*time.Second, 10*time.Millisecond)

	// 取消订阅
	err = eq.Unsubscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 取消订阅后发布报错
	event2 := &schema.InputEvent{BaseEvent: *schema.NewBaseEvent(schema.EventInput)}
	err = eq.PublishEventAsync(context.Background(), agentID, sess, event2)
	assert.Error(t, err)
}

// TestEventQueue_同步发布_等待处理完成 测试 PublishEvent 后 handler 已处理
func TestEventQueue_同步发布_等待处理完成(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	eq.Start()
	defer func() { _ = eq.Stop(context.Background()) }()

	handler := &fakeEventHandler{}
	eq.SetEventHandler(handler)

	agentID := "agent-1"
	sessionID := "sess-1"
	sess := &fakeSessionFacade{sessionID: sessionID}

	err := eq.Subscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 同步发布
	event := &schema.InputEvent{BaseEvent: *schema.NewBaseEvent(schema.EventInput)}
	err = eq.PublishEvent(context.Background(), agentID, sess, event)
	require.NoError(t, err)

	// handler 已处理
	assert.Equal(t, int32(1), handler.handledInput.Load())
}

// TestEventQueue_火忘发布_不等处理 测试 PublishEventAsync 立即返回
func TestEventQueue_火忘发布_不等处理(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	eq.Start()
	defer func() { _ = eq.Stop(context.Background()) }()

	handler := &fakeEventHandler{}
	eq.SetEventHandler(handler)

	agentID := "agent-1"
	sessionID := "sess-1"
	sess := &fakeSessionFacade{sessionID: sessionID}

	err := eq.Subscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 火忘发布，立即返回
	event := &schema.InputEvent{BaseEvent: *schema.NewBaseEvent(schema.EventInput)}
	err = eq.PublishEventAsync(context.Background(), agentID, sess, event)
	require.NoError(t, err)

	// 最终 handler 会处理
	assert.Eventually(t, func() bool { return handler.handledInput.Load() >= 1 }, 2*time.Second, 10*time.Millisecond)
}

// TestEventQueue_Topic路由_不同事件类型到不同Handler 测试不同事件路由到对应 handler
func TestEventQueue_Topic路由_不同事件类型到不同Handler(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	eq.Start()
	defer func() { _ = eq.Stop(context.Background()) }()

	handler := &fakeEventHandler{}
	eq.SetEventHandler(handler)

	agentID := "agent-1"
	sessionID := "sess-1"
	sess := &fakeSessionFacade{sessionID: sessionID}

	err := eq.Subscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 发布 Input 事件
	inputEvent := &schema.InputEvent{BaseEvent: *schema.NewBaseEvent(schema.EventInput)}
	err = eq.PublishEvent(context.Background(), agentID, sess, inputEvent)
	require.NoError(t, err)
	assert.Equal(t, int32(1), handler.handledInput.Load())

	// 发布 TaskCompletion 事件
	completionEvent := &schema.TaskCompletionEvent{BaseEvent: *schema.NewBaseEvent(schema.EventTaskCompletion)}
	err = eq.PublishEvent(context.Background(), agentID, sess, completionEvent)
	require.NoError(t, err)
	assert.Equal(t, int32(1), handler.handledTaskCompletion.Load())

	// 发布 TaskFailed 事件
	failedEvent := &schema.TaskFailedEvent{BaseEvent: *schema.NewBaseEvent(schema.EventTaskFailed)}
	err = eq.PublishEvent(context.Background(), agentID, sess, failedEvent)
	require.NoError(t, err)
	assert.Equal(t, int32(1), handler.handledTaskFailed.Load())

	// 发布 FollowUp 事件
	followUpEvent := &schema.FollowUpEvent{BaseEvent: *schema.NewBaseEvent(schema.EventFollowUp)}
	err = eq.PublishEvent(context.Background(), agentID, sess, followUpEvent)
	require.NoError(t, err)
	assert.Equal(t, int32(1), handler.handledFollowUp.Load())

	// 发布 TaskInteraction 事件
	interactionEvent := &schema.TaskInteractionEvent{BaseEvent: *schema.NewBaseEvent(schema.EventTaskInteraction)}
	err = eq.PublishEvent(context.Background(), agentID, sess, interactionEvent)
	require.NoError(t, err)
	assert.Equal(t, int32(1), handler.handledTaskInteraction.Load())

	// 其他 handler 不应被调用
	assert.Equal(t, int32(1), handler.handledInput.Load())
	assert.Equal(t, int32(1), handler.handledTaskCompletion.Load())
	assert.Equal(t, int32(1), handler.handledTaskFailed.Load())
	assert.Equal(t, int32(1), handler.handledFollowUp.Load())
	assert.Equal(t, int32(1), handler.handledTaskInteraction.Load())
}

// TestEventQueue_启停 测试 Start/Stop 生命周期
func TestEventQueue_启停(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)

	// 启动
	eq.Start()

	// 停止
	err := eq.Stop(context.Background())
	assert.NoError(t, err)
}

// TestEventQueue_buildTopic格式 验证 topic 格式为 "{agentID}_{sessionID}_{eventType}"
func TestEventQueue_buildTopic格式(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	eq := NewEventQueue(cfg)

	topic := eq.buildTopic("agent-1", "sess-1", schema.EventInput)
	assert.Equal(t, "agent-1_sess-1_input", topic)

	topic = eq.buildTopic("agent-2", "sess-2", schema.EventTaskCompletion)
	assert.Equal(t, "agent-2_sess-2_task_completion", topic)

	topic = eq.buildTopic("agent-3", "sess-3", schema.EventFollowUp)
	assert.Equal(t, "agent-3_sess-3_follow_up", topic)
}

// TestEventQueue_SetConfig 测试更新配置
func TestEventQueue_SetConfig(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	eq := NewEventQueue(cfg)

	newCfg := config.DefaultControllerConfig()
	newCfg.EventQueueSize = 200
	eq.SetConfig(newCfg)

	// 验证配置已更新（通过 buildTopic 等间接验证）
	assert.Equal(t, newCfg, eq.config)
}

// TestEventQueue_Stop_未启动时不报错 测试 Stop 前不 Start
func TestEventQueue_Stop_未启动时不报错(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	// 不调用 Start，直接 Stop 应不报错
	err := eq.Stop(context.Background())
	assert.NoError(t, err)
}

// TestEventQueue_订阅后发送TaskCompletionEvent 测试 TaskCompletion 事件路由
func TestEventQueue_订阅后发送TaskCompletionEvent(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	eq.Start()
	defer func() { _ = eq.Stop(context.Background()) }()

	handler := &fakeEventHandler{}
	eq.SetEventHandler(handler)

	agentID := "agent-1"
	sessionID := "sess-1"
	sess := &fakeSessionFacade{sessionID: sessionID}

	err := eq.Subscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 发布 TaskCompletion 事件
	event := &schema.TaskCompletionEvent{BaseEvent: *schema.NewBaseEvent(schema.EventTaskCompletion)}
	err = eq.PublishEvent(context.Background(), agentID, sess, event)
	require.NoError(t, err)

	assert.Equal(t, int32(1), handler.handledTaskCompletion.Load())
}

// TestEventQueue_同步发布Handler返回结果 测试 PublishEvent 返回值
func TestEventQueue_同步发布Handler返回结果(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	cfg.EventQueueSize = 100
	cfg.EventTimeout = 5

	eq := NewEventQueue(cfg)
	eq.Start()
	defer func() { _ = eq.Stop(context.Background()) }()

	handler := &fakeEventHandler{}
	eq.SetEventHandler(handler)

	agentID := "agent-1"
	sessionID := "sess-1"
	sess := &fakeSessionFacade{sessionID: sessionID}

	err := eq.Subscribe(context.Background(), agentID, sessionID)
	require.NoError(t, err)

	// 同步发布 FollowUp 事件
	event := &schema.FollowUpEvent{BaseEvent: *schema.NewBaseEvent(schema.EventFollowUp)}
	err = eq.PublishEvent(context.Background(), agentID, sess, event)
	require.NoError(t, err)

	assert.Equal(t, int32(1), handler.handledFollowUp.Load())
}

// TestEventQueue_订阅无Handler报错 测试未设置 Handler 时 Subscribe 报错
func TestEventQueue_订阅无Handler报错(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	eq := NewEventQueue(cfg)

	err := eq.Subscribe(context.Background(), "agent-1", "sess-1")
	assert.Error(t, err)
}

// TestEventQueue_EventHandler 验证 EventHandler 返回事件处理器
func TestEventQueue_EventHandler(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	eq := NewEventQueue(cfg)
	// EventHandler 可能返回 nil（尚未设置）
	eh := eq.EventHandler()
	// 验证方法可调用且不 panic
	_ = eh
}
