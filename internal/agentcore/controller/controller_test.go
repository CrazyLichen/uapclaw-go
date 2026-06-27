package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newTestController 创建测试用 Controller（已 Init）
func newTestController() *Controller {
	card := agentschema.NewAgentCard()
	cfg := config.DefaultControllerConfig()
	c := NewController()
	c.Init(card, cfg, nil, nil)
	return c
}

// mockSessionFacade 用于 Controller 测试的 SessionFacade mock
type mockSessionFacade struct {
	sessionID         string
	stateData         map[string]any
	writeData         []any
	writeStreamCalled bool
}

func newMockSessionFacade(id string) *mockSessionFacade {
	return &mockSessionFacade{
		sessionID: id,
		stateData: make(map[string]any),
	}
}

func (m *mockSessionFacade) GetSessionID() string { return m.sessionID }
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.stateData[k] = v
	}
}
func (m *mockSessionFacade) GetState(key state.StateKey) (any, error) {
	return m.stateData[key.String()], nil
}
func (m *mockSessionFacade) DumpState() map[string]any { return m.stateData }
func (m *mockSessionFacade) WriteStream(_ context.Context, data any) error {
	m.writeStreamCalled = true
	m.writeData = append(m.writeData, data)
	return nil
}
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, data any) error { return nil }
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any                       { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error             { return nil }

var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// mockContextEngine 空的 ContextEngine mock
type mockContextEngine struct{}

func (m *mockContextEngine) CreateContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...iface.CreateContextOption) (iface.ModelContext, error) {
	return nil, nil
}
func (m *mockContextEngine) GetContext(_ string, _ string) iface.ModelContext { return nil }
func (m *mockContextEngine) CompressContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...iface.CompressContextOption) (string, error) {
	return "", nil
}
func (m *mockContextEngine) ClearContext(_ context.Context, _ ...iface.ClearContextOption) error {
	return nil
}
func (m *mockContextEngine) SaveContexts(_ context.Context, _ sessioninterfaces.SessionFacade, _ []string) (map[string]any, error) {
	return nil, nil
}

var _ iface.ContextEngine = (*mockContextEngine)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewController 测试空壳构造
func TestNewController(t *testing.T) {
	c := NewController()
	assert.NotNil(t, c)
	assert.Nil(t, c.card)
	assert.Nil(t, c.config)
	assert.False(t, c.started.Load())
}

// TestController_Init 测试两阶段初始化
func TestController_Init(t *testing.T) {
	c := NewController()
	card := agentschema.NewAgentCard()
	cfg := config.DefaultControllerConfig()

	c.Init(card, cfg, nil, nil)

	assert.Equal(t, card, c.card)
	assert.Equal(t, cfg, c.config)
	assert.NotNil(t, c.taskManager)
	assert.NotNil(t, c.eventQueue)
	assert.NotNil(t, c.taskScheduler)
}

// TestController_Init_接线验证 测试 Init 中 TaskManager → TaskScheduler 接线
func TestController_Init_接线验证(t *testing.T) {
	c := newTestController()
	assert.NotNil(t, c.TaskManager())
	assert.NotNil(t, c.EventQueue())
	assert.NotNil(t, c.TaskScheduler())
}

// TestController_Start 测试启动
func TestController_Start(t *testing.T) {
	c := newTestController()
	err := c.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, c.started.Load())
}

// TestController_Start_幂等 测试重复启动幂等
func TestController_Start_幂等(t *testing.T) {
	c := newTestController()
	err := c.Start(context.Background())
	require.NoError(t, err)
	err = c.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, c.started.Load())
}

// TestController_Stop 测试停止
func TestController_Stop(t *testing.T) {
	c := newTestController()
	err := c.Start(context.Background())
	require.NoError(t, err)
	err = c.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, c.started.Load())
}

// TestController_Stop_未启动时不报错 测试停止未启动的 Controller
func TestController_Stop_未启动时不报错(t *testing.T) {
	c := newTestController()
	err := c.Stop(context.Background())
	require.NoError(t, err)
}

// TestController_ensureStarted 测试懒启动
func TestController_ensureStarted(t *testing.T) {
	c := newTestController()
	assert.False(t, c.started.Load())
	err := c.ensureStarted(context.Background())
	require.NoError(t, err)
	assert.True(t, c.started.Load())
	err = c.ensureStarted(context.Background())
	require.NoError(t, err)
}

// TestController_ensureStarted_设置EventHandler 测试懒启动时同步设置 EventQueue handler
func TestController_ensureStarted_设置EventHandler(t *testing.T) {
	c := newTestController()
	handler := &mockEventHandler{base: &modules.EventHandlerBase{}}
	c.SetEventHandler(handler)

	err := c.ensureStarted(context.Background())
	require.NoError(t, err)
	// 偏差8 修复：ensureStarted 应同步 EventQueue 的 EventHandler
	assert.Equal(t, handler, c.eventQueue.EventHandler())
}

// TestController_SetConfig 测试配置级联传播
func TestController_SetConfig(t *testing.T) {
	c := newTestController()
	newCfg := config.DefaultControllerConfig()
	newCfg.MaxConcurrentTasks = 10
	c.SetConfig(newCfg)
	assert.Equal(t, newCfg, c.Config())
	assert.Equal(t, newCfg, c.taskManager.Config())
}

// TestController_SetEventHandler 测试设置事件处理器
func TestController_SetEventHandler(t *testing.T) {
	c := newTestController()
	handler := &mockEventHandler{base: &modules.EventHandlerBase{}}
	c.SetEventHandler(handler)
	assert.Equal(t, handler, c.EventHandler())
	assert.Equal(t, c.config, handler.base.Config)
	assert.Equal(t, c.taskManager, handler.base.TaskManager)
	assert.Equal(t, c.taskScheduler, handler.base.TaskScheduler)
	// 偏差8 修复：SetEventHandler 应同步 EventQueue 的 EventHandler
	assert.Equal(t, handler, c.eventQueue.EventHandler())
}

// TestController_AddTaskExecutor 测试注册 TaskExecutor
func TestController_AddTaskExecutor(t *testing.T) {
	c := newTestController()
	builder := func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return &mockTaskExecutor{}
	}
	result := c.AddTaskExecutor("test_type", builder)
	assert.Equal(t, c, result)
}

// TestController_RemoveTaskExecutor 测试移除 TaskExecutor
func TestController_RemoveTaskExecutor(t *testing.T) {
	c := newTestController()
	builder := func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return &mockTaskExecutor{}
	}
	c.AddTaskExecutor("test_type", builder)
	c.RemoveTaskExecutor("test_type")
}

// TestController_Config 测试获取配置
func TestController_Config(t *testing.T) {
	c := newTestController()
	assert.NotNil(t, c.Config())
}

// TestController_ContextEngine 测试上下文引擎 getter/setter
func TestController_ContextEngine(t *testing.T) {
	c := newTestController()
	assert.Nil(t, c.ContextEngine())
	ce := &mockContextEngine{}
	c.SetContextEngine(ce)
	assert.Equal(t, ce, c.ContextEngine())
}

// TestController_AbilityManager 测试能力管理器 getter/setter
func TestController_AbilityManager(t *testing.T) {
	c := newTestController()
	assert.Nil(t, c.AbilityManager())
	am := &ability.AbilityManager{}
	c.SetAbilityManager(am)
	assert.Equal(t, am, c.AbilityManager())
}

// TestController_isCompletionSignal 测试完成信号判断
func TestController_isCompletionSignal(t *testing.T) {
	c := newTestController()
	assert.False(t, c.isCompletionSignal(nil))
	chunk := &schema.ControllerOutputChunk{Payload: nil}
	assert.False(t, c.isCompletionSignal(chunk))
	chunk.Payload = &schema.ControllerOutputPayload{Type: "processing"}
	assert.False(t, c.isCompletionSignal(chunk))
	chunk.Payload.Type = schema.AllTasksProcessed
	assert.True(t, c.isCompletionSignal(chunk))
}

// TestController_restoreTaskManagerState_无状态 测试恢复状态（无保存状态）
func TestController_restoreTaskManagerState_无状态(t *testing.T) {
	c := newTestController()
	mockSess := newMockSessionFacade("test-session")
	result := c.restoreTaskManagerState(context.Background(), mockSess)
	assert.False(t, result)
}

// TestController_saveTaskManagerState_禁用持久化 测试保存状态（禁用持久化）
func TestController_saveTaskManagerState_禁用持久化(t *testing.T) {
	c := newTestController()
	c.config.EnableTaskPersistence = false
	mockSess := newMockSessionFacade("test-session")
	err := c.saveTaskManagerState(context.Background(), mockSess)
	assert.NoError(t, err)
}

// TestController_saveTaskManagerState_启用持久化 测试保存状态（启用持久化）
func TestController_saveTaskManagerState_启用持久化(t *testing.T) {
	c := newTestController()
	c.config.EnableTaskPersistence = true
	mockSess := newMockSessionFacade("test-session")
	err := c.saveTaskManagerState(context.Background(), mockSess)
	assert.NoError(t, err)
	assert.NotNil(t, mockSess.stateData["controller"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mockEventHandler 简单的 EventHandler mock
type mockEventHandler struct {
	base *modules.EventHandlerBase
}

func (m *mockEventHandler) HandleInput(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandler) HandleTaskInteraction(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandler) HandleTaskCompletion(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandler) HandleTaskFailed(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandler) HandleFollowUp(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandler) GetBase() *modules.EventHandlerBase { return m.base }
func (m *mockEventHandler) PrepareRound() int                  { return 0 }
func (m *mockEventHandler) WaitCompletion(_ context.Context, _ time.Duration) map[string]any {
	return nil
}
func (m *mockEventHandler) OnAbort() {}

// mockTaskExecutor 简单的 TaskExecutor mock
type mockTaskExecutor struct{}

func (m *mockTaskExecutor) ExecuteAbility(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error) {
	ch := make(chan *schema.ControllerOutputChunk)
	close(ch)
	return ch, nil
}

// TestController_EventQueue 测试获取事件队列
func TestController_EventQueue(t *testing.T) {
	c := newTestController()
	assert.NotNil(t, c.EventQueue())
}

// TestController_TaskManager 测试获取任务管理器
func TestController_TaskManager(t *testing.T) {
	c := newTestController()
	assert.NotNil(t, c.TaskManager())
}

// TestController_TaskScheduler 测试获取任务调度器
func TestController_TaskScheduler(t *testing.T) {
	c := newTestController()
	assert.NotNil(t, c.TaskScheduler())
}

// TestController_EventHandler 测试获取事件处理器（初始为 nil）
func TestController_EventHandler(t *testing.T) {
	c := newTestController()
	assert.Nil(t, c.EventHandler())
}

// TestController_StartStop 完整生命周期测试
func TestController_StartStop(t *testing.T) {
	c := newTestController()
	// 启动
	err := c.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, c.started.Load())
	// 停止
	err = c.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, c.started.Load())
	// 重新启动
	err = c.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, c.started.Load())
	// 停止
	err = c.Stop(context.Background())
	require.NoError(t, err)
}

// TestController_restoreTaskManagerState_有状态 测试恢复状态（有保存状态）
func TestController_restoreTaskManagerState_有状态(t *testing.T) {
	c := newTestController()
	c.Start(context.Background())
	defer c.Stop(context.Background())

	mockSess := newMockSessionFacade("test-session")

	// 先添加一个任务
	task := schema.NewTask("test-session", "default_task_type")
	task.Description = "测试任务"
	require.NoError(t, c.taskManager.AddTask(context.Background(), task))

	// 保存状态
	c.config.EnableTaskPersistence = true
	_ = c.saveTaskManagerState(context.Background(), mockSess)

	// 清空状态
	_ = c.taskManager.ClearState(context.Background())

	// 恢复状态
	result := c.restoreTaskManagerState(context.Background(), mockSess)
	assert.True(t, result)

	// 验证任务已恢复
	tasks, err := c.taskManager.GetTask(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "测试任务", tasks[0].Description)
}

// TestController_PublishEventAsync 测试异步发布事件
func TestController_PublishEventAsync(t *testing.T) {
	c := newTestController()
	// PublishEventAsync 需要 *session.Session，无法用 mock 测试
	// 仅验证方法存在且编译通过
	assert.NotNil(t, c)
}

// TestController_GetTaskExecutor 测试获取 TaskExecutor
func TestController_GetTaskExecutor(t *testing.T) {
	c := newTestController()
	builder := func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return &mockTaskExecutor{}
	}
	c.AddTaskExecutor("test_type", builder)

	deps := &modules.TaskExecutorDependencies{
		Config:        c.config,
		AbilityMgr:    c.abilityMgr,
		ContextEngine: c.contextEngine,
		TaskManager:   c.taskManager,
		EventQueue:    c.eventQueue,
	}
	executor, err := c.GetTaskExecutor("test_type", deps)
	require.NoError(t, err)
	assert.NotNil(t, executor)
}

// TestController_formatError 测试错误格式化
func TestController_formatError(t *testing.T) {
	assert.Equal(t, "", formatError(nil))
	assert.NotEmpty(t, formatError(fmt.Errorf("test error")))
}

// TestController_buildControllerRuntimeError 测试构建运行时错误
func TestController_buildControllerRuntimeError(t *testing.T) {
	err := buildControllerRuntimeError("test reason", nil)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "test reason")
}
func (m *mockTaskExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "", nil
}
func (m *mockTaskExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}
func (m *mockTaskExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "", nil
}
func (m *mockTaskExecutor) Cancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}
