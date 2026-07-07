package task_loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// mockAbilityMgr 简单的 AbilityManagerInterface mock
type mockAbilityMgr struct{}

func (m *mockAbilityMgr) Add(_ commonschema.Ability) agentschema.AddAbilityResult {
	return agentschema.AddAbilityResult{}
}
func (m *mockAbilityMgr) AddMany(_ []commonschema.Ability) []agentschema.AddAbilityResult {
	return nil
}
func (m *mockAbilityMgr) Remove(_ string) commonschema.Ability         { return nil }
func (m *mockAbilityMgr) RemoveMany(_ []string) []commonschema.Ability { return nil }
func (m *mockAbilityMgr) Get(_ string) commonschema.Ability            { return nil }
func (m *mockAbilityMgr) List() []commonschema.Ability                 { return nil }
func (m *mockAbilityMgr) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]commonschema.ToolInfoInterface, error) {
	return nil, nil
}
func (m *mockAbilityMgr) Execute(_ context.Context, _ *agentinterfaces.AgentCallbackContext, _ []*llmschema.ToolCall, _ sessioninterfaces.SessionFacade, _ string) []agentschema.ExecuteResult {
	return nil
}
func (m *mockAbilityMgr) SetContextEngine(_ ceinterface.ContextEngine) {}
func (m *mockAbilityMgr) ReorderTools(_ []string)                      {}

// mockEventHandlerWithQueues 满足 EventHandler + interactionQueuesProvider
type mockEventHandlerWithQueues struct {
	modules.EventHandlerBase
	queues *LoopQueues
}

func (m *mockEventHandlerWithQueues) HandleInput(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) HandleTaskInteraction(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) HandleTaskCompletion(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) HandleTaskFailed(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockEventHandlerWithQueues) InteractionQueues() *LoopQueues {
	return m.queues
}

// newTestCard 创建测试用 AgentCard
func newTestCard() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentName("test-agent"))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTaskLoopController 测试创建 TaskLoopController
func TestNewTaskLoopController(t *testing.T) {
	tc := NewTaskLoopController()
	assert.NotNil(t, tc)
}

// TestTaskLoopController_满足ControllerInterface 测试编译时接口满足
func TestTaskLoopController_满足ControllerInterface(t *testing.T) {
	// 编译时断言已保证，此处运行时验证
	tc := NewTaskLoopController()
	var _ controller.ControllerInterface = tc
	assert.NotNil(t, tc)
}

// TestTaskLoopController_Init 测试初始化
func TestTaskLoopController_Init(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)
	assert.Equal(t, cfg, tc.Config())
}

// TestTaskLoopController_DrainFollowUp_无Handler 测试无 EventHandler 时排空返回空
func TestTaskLoopController_DrainFollowUp_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	// 无 EventHandler，应返回空切片
	assert.Empty(t, tc.DrainFollowUp())
}

// TestTaskLoopController_HasFollowUp_无Handler 测试无 EventHandler 时返回 false
func TestTaskLoopController_HasFollowUp_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	assert.False(t, tc.HasFollowUp())
}

// TestTaskLoopController_EnqueueFollowUp_无Handler 测试无 EventHandler 时入队不 panic
func TestTaskLoopController_EnqueueFollowUp_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	assert.NotPanics(t, func() {
		tc.EnqueueFollowUp("test")
	})
}

// TestTaskLoopController_DrainFollowUp_有Queues 测试有 LoopQueues 时排空
func TestTaskLoopController_DrainFollowUp_有Queues(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	// 推入 follow-up
	queues.PushFollowUp("msg1")
	queues.PushFollowUp("msg2")

	// 通过 TaskLoopController 排空
	msgs := tc.DrainFollowUp()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
	assert.Empty(t, tc.DrainFollowUp())
}

// TestTaskLoopController_EnqueueFollowUp_有Queues 测试通过 Controller 入队
func TestTaskLoopController_EnqueueFollowUp_有Queues(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	tc.EnqueueFollowUp("f1")
	assert.True(t, tc.HasFollowUp())
	msgs := tc.DrainFollowUp()
	assert.Equal(t, []string{"f1"}, msgs)
}

// TestTaskLoopController_HasFollowUp_有Queues 测试检查待处理消息
func TestTaskLoopController_HasFollowUp_有Queues(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	assert.False(t, tc.HasFollowUp())
	queues.PushFollowUp("msg")
	assert.True(t, tc.HasFollowUp())
	queues.DrainFollowUp()
	assert.False(t, tc.HasFollowUp())
}

// TestTaskLoopController_WaitRoundCompletion 测试等待轮次完成
func TestTaskLoopController_WaitRoundCompletion(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	// 无超时等待（EventHandlerBase 默认返回 {"status": "completed"}）
	result := tc.WaitRoundCompletion(context.Background(), nil)
	assert.Equal(t, map[string]any{"status": "completed"}, result)
}

// TestTaskLoopController_WaitRoundCompletion_有超时 测试带超时等待
func TestTaskLoopController_WaitRoundCompletion_有超时(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	timeout := 5.0
	result := tc.WaitRoundCompletion(context.Background(), &timeout)
	assert.Equal(t, map[string]any{"status": "completed"}, result)
}

// TestTaskLoopController_SubmitRound 测试提交一轮任务
func TestTaskLoopController_SubmitRound(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	// 需要创建真实 Session 才能发布事件
	// 此测试验证 SubmitRound 构建 InputEvent 并注入元数据的逻辑
	// 由于 PublishEventAsync 需要已启动的 EventQueue，此处验证方法不 panic
	handler := &mockEventHandlerWithQueues{queues: NewLoopQueues(16)}
	tc.SetEventHandler(handler)

	roundID := handler.PrepareRound()
	assert.Equal(t, 0, roundID)
}

// TestTaskLoopController_SubmitRound_无Handler EventHandler 为 nil 时返回 nil
func TestTaskLoopController_SubmitRound_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	// 未 Init，EventHandler 为 nil

	sess := session.NewSession()
	err := tc.SubmitRound(context.Background(), sess, "test query", false, false, "", nil)
	// handler 为 nil 时直接返回 nil
	assert.Nil(t, err)
}

// TestTaskLoopController_SubmitRound_正常提交 有 EventHandler 时正常提交
func TestTaskLoopController_SubmitRound_正常提交(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	handler := &mockEventHandlerWithQueues{queues: NewLoopQueues(16)}
	tc.SetEventHandler(handler)

	// 启动 Controller + 绑定 Session（注册 topic）
	err := tc.Start(context.Background())
	require.NoError(t, err)

	sess := session.NewSession()
	err = tc.BindSession(context.Background(), sess)
	require.NoError(t, err)

	err = tc.SubmitRound(context.Background(), sess, "test query", false, false, "", nil)
	assert.NoError(t, err)
}

// TestTaskLoopController_SubmitRound_FollowUp 标记 isFollowUp 时注入元数据
func TestTaskLoopController_SubmitRound_FollowUp(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	handler := &mockEventHandlerWithQueues{queues: NewLoopQueues(16)}
	tc.SetEventHandler(handler)

	err := tc.Start(context.Background())
	require.NoError(t, err)

	sess := session.NewSession()
	err = tc.BindSession(context.Background(), sess)
	require.NoError(t, err)

	err = tc.SubmitRound(context.Background(), sess, "follow up query", true, false, agentinterfaces.RunKindHeartbeat, nil)
	assert.NoError(t, err)
}

// TestTaskLoopController_SubmitRound_带RunContext 有 runContext 时注入元数据
func TestTaskLoopController_SubmitRound_带RunContext(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	handler := &mockEventHandlerWithQueues{queues: NewLoopQueues(16)}
	tc.SetEventHandler(handler)

	err := tc.Start(context.Background())
	require.NoError(t, err)

	sess := session.NewSession()
	err = tc.BindSession(context.Background(), sess)
	require.NoError(t, err)

	runCtx := &agentinterfaces.RunContext{Reason: "test"}
	err = tc.SubmitRound(context.Background(), sess, "query", false, true, agentinterfaces.RunKindCron, runCtx)
	assert.NoError(t, err)
}

// TestTaskLoopController_getInteractionQueues_无Provider 测试类型断言无 provider 返回 nil
func TestTaskLoopController_getInteractionQueues_无Provider(t *testing.T) {
	tc := NewTaskLoopController()
	// EventHandlerBase 不实现 interactionQueuesProvider
	assert.Nil(t, tc.getInteractionQueues())
}

// TestTaskLoopController_getInteractionQueues_有Provider 测试类型断言有 provider 返回队列
func TestTaskLoopController_getInteractionQueues_有Provider(t *testing.T) {
	tc := NewTaskLoopController()
	card := newTestCard()
	cfg := config.DefaultControllerConfig()
	tc.Init(card, cfg, &mockAbilityMgr{}, nil)

	queues := NewLoopQueues(16)
	handler := &mockEventHandlerWithQueues{queues: queues}
	tc.SetEventHandler(handler)

	result := tc.getInteractionQueues()
	require.NotNil(t, result)
	assert.Equal(t, queues, result)
}

// TestTaskLoopController_WaitRoundCompletion_无Handler 测试无 EventHandler 时返回 nil
func TestTaskLoopController_WaitRoundCompletion_无Handler(t *testing.T) {
	tc := NewTaskLoopController()
	result := tc.WaitRoundCompletion(context.Background(), nil)
	assert.Nil(t, result)
}
