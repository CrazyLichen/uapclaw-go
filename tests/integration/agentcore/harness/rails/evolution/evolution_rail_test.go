//go:build integration

package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionRailSuite 测试 EvolutionRail 基类和 TrajectoryRail 的轨迹收集生命周期。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_evolution_rail.py
//   - TestEvolutionRail: trajectory_collection_basic, no_op_without_builder
//   - TestEvolutionRailAccumulation: same_session_keeps_builder, new_session_replaces_builder
//   - TestTrajectoryRail: trajectory_rail_collects_only, priority
//   - TestEvolutionRailAsyncMode: drain, cleanup_background_tasks
type EvolutionRailSuite struct {
	isuite.BaseIntegrationSuite
}

// testExtension 集成测试用 EvolutionExtension 空实现。
type testExtension struct{}

func (testExtension) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (testExtension) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (testExtension) OnAfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (testExtension) OnAfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (testExtension) OnAfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (testExtension) OnAfterEvolutionTriggered(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (testExtension) AllowEvolutionTrigger(_ evolution.EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	return true
}
func (testExtension) SnapshotForEvolution(_ context.Context, traj *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *evolution.EvolutionSnapshot {
	return &evolution.EvolutionSnapshot{Trajectory: traj}
}
func (testExtension) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *evolution.EvolutionSnapshot) error {
	return nil
}
func (testExtension) GetEvolutionTotalTimeoutSecs() float64 { return 0 }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEvolutionRailSuite 运行 EvolutionRail 集成测试套件。
func TestEvolutionRailSuite(t *testing.T) {
	suite.Run(t, new(EvolutionRailSuite))
}

// TestEvolutionRail_轨迹收集生命周期 测试完整 invoke 生命周期的轨迹收集。
//
// 对齐 Python: TestEvolutionRail.test_trajectory_collection_basic
func (s *EvolutionRailSuite) TestEvolutionRail_轨迹收集生命周期() {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithTrajectoryStore(store),
		evolution.WithAsyncEvolution(false),
	)

	// 1. BeforeInvoke — 创建 builder
	cbc := newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_123"}, nil)
	err := rail.BeforeInvoke(s.Ctx, cbc)
	s.Require().NoError(err)
	s.NotNil(rail.Builder())

	// 2. AfterModelCall — 记录 LLM 步骤
	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{
			llmschema.NewUserMessage("hello"),
		},
		Response: &llmschema.AssistantMessage{
			DefaultMessage: *llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "hi there"),
		},
	}
	cbc = newCBC(nil, modelInputs, nil)
	err = rail.AfterModelCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// 3. AfterToolCall — 记录 Tool 步骤
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolName:   "read_file",
		ToolArgs:   map[string]any{"file_path": "/tmp/test.txt"},
		ToolResult: "file contents",
	}
	cbc = newCBC(nil, toolInputs, nil)
	err = rail.AfterToolCall(s.Ctx, cbc)
	s.Require().NoError(err)

	// 4. AfterInvoke — 保存轨迹
	cbc = newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_123"}, nil)
	err = rail.AfterInvoke(s.Ctx, cbc)
	s.Require().NoError(err)

	// 验证轨迹保存（Query 需要两个参数：version, filters）
	trajs := store.Query("", map[string]any{"session_id": "conv_123"})
	s.Len(trajs, 1)
	traj := trajs[0]
	s.Equal("conv_123", traj.SessionID)
	s.Equal("online", traj.Source)
	s.Len(traj.Steps, 2)

	// 检查 LLM 步骤
	s.Equal(trajectory.StepKindLLM, traj.Steps[0].Kind)
	s.NotNil(traj.Steps[0].Detail)

	// 检查 Tool 步骤
	s.Equal(trajectory.StepKindTool, traj.Steps[1].Kind)
	s.NotNil(traj.Steps[1].Detail)
}

// TestEvolutionRail_无Builder时AfterModelCall空操作 测试未初始化 builder 时不崩溃。
//
// 对齐 Python: TestEvolutionRail.test_no_op_without_builder
func (s *EvolutionRailSuite) TestEvolutionRail_无Builder时AfterModelCall空操作() {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithTrajectoryStore(store),
	)

	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("test")},
		Response: &llmschema.AssistantMessage{
			DefaultMessage: *llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "ok"),
		},
	}
	cbc := newCBC(nil, modelInputs, nil)
	err := rail.AfterModelCall(s.Ctx, cbc)
	s.Require().NoError(err)

	trajs := store.Query("", nil)
	s.Len(trajs, 0)
}

// TestEvolutionRail_同Session复用Builder 测试同 session 的 builder 复用。
//
// 对齐 Python: TestEvolutionRailAccumulation.test_same_session_default_keeps_builder_after_invoke
func (s *EvolutionRailSuite) TestEvolutionRail_同Session复用Builder() {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithTrajectoryStore(store),
		evolution.WithAsyncEvolution(false),
	)

	// Round 1
	cbc := newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_multi"}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))

	cbc = newCBC(nil, &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("q1")},
		Response: &llmschema.AssistantMessage{
			DefaultMessage: *llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "a1"),
		},
	}, nil)
	s.Require().NoError(rail.AfterModelCall(s.Ctx, cbc))

	cbc = newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_multi"}, nil)
	s.Require().NoError(rail.AfterInvoke(s.Ctx, cbc))

	// builder 不应为 nil（同 session 复用）
	s.NotNil(rail.Builder())
	s.Len(store.Query("", map[string]any{"session_id": "conv_multi"}), 1)
}

// TestEvolutionRail_新Session替换Builder 测试不同 session 创建新 builder。
//
// 对齐 Python: TestEvolutionRailAccumulation.test_new_session_replaces_builder
func (s *EvolutionRailSuite) TestEvolutionRail_新Session替换Builder() {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithTrajectoryStore(store),
	)

	cbc := newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_a"}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))
	firstBuilder := rail.Builder()

	cbc = newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_b"}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))

	s.NotEqual(firstBuilder, rail.Builder())
	s.Equal("conv_b", rail.Builder().SessionID())
}

// TestEvolutionRail_GetCallbacks注册5个事件 测试 GetCallbacks 返回 5 个回调事件。
func (s *EvolutionRailSuite) TestEvolutionRail_GetCallbacks注册5个事件() {
	rail := evolution.NewEvolutionRail(testExtension{})
	callbacks := rail.GetCallbacks()

	s.Contains(callbacks, agentinterfaces.CallbackBeforeInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterModelCall)
	s.Contains(callbacks, agentinterfaces.CallbackAfterToolCall)
	s.Contains(callbacks, agentinterfaces.CallbackAfterInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// TestEvolutionRail_Priority60 测试基类优先级。
func (s *EvolutionRailSuite) TestEvolutionRail_Priority60() {
	rail := evolution.NewEvolutionRail(testExtension{})
	s.Equal(60, rail.Priority())
}

// TestEvolutionRail_EmitHostEvent和Drain 测试主机事件缓存和排空。
func (s *EvolutionRailSuite) TestEvolutionRail_EmitHostEvent和Drain() {
	rail := evolution.NewEvolutionRail(testExtension{})

	events := rail.DrainPendingHostEvents(false, nil)
	s.Len(events, 0)

	event := &stream.OutputSchema{Type: "test", Index: 0, Payload: map[string]any{}}
	rail.EmitHostEvent(event)

	events = rail.DrainPendingHostEvents(false, nil)
	s.Len(events, 1)
	s.Equal("test", events[0].Type)

	events = rail.DrainPendingHostEvents(false, nil)
	s.Len(events, 0)
}

// TestEvolutionRail_CleanupBackgroundTasks 测试清理后台任务。
func (s *EvolutionRailSuite) TestEvolutionRail_CleanupBackgroundTasks() {
	rail := evolution.NewEvolutionRail(testExtension{})
	err := rail.CleanupBackgroundTasks()
	s.Require().NoError(err)
}

// TestEvolutionRail_DrainPendingApprovalEvents别名 测试兼容别名。
func (s *EvolutionRailSuite) TestEvolutionRail_DrainPendingApprovalEvents别名() {
	rail := evolution.NewEvolutionRail(testExtension{})
	events := rail.DrainPendingApprovalEvents(false, nil)
	s.Len(events, 0)
}

// TestEvolutionRail_SetTrajectorySink要求TeamID 测试绑定 Sink 时要求 teamID。
func (s *EvolutionRailSuite) TestEvolutionRail_SetTrajectorySink要求TeamID() {
	rail := evolution.NewEvolutionRail(testExtension{})

	err := rail.SetTrajectorySink(&fakeTrajectorySink{}, "")
	s.Require().Error(err)
	s.Contains(err.Error(), "team_id is required")

	err = rail.SetTrajectorySink(&fakeTrajectorySink{}, "team_1")
	s.Require().NoError(err)
}

// TestEvolutionRail_WithDisabledSkills 测试禁用技能配置。
func (s *EvolutionRailSuite) TestEvolutionRail_WithDisabledSkills() {
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithDisabledSkills([]string{"Skill-A", "skill_b"}),
	)
	// normalizeSkillNames 只做 TrimSpace，不做小写转换
	s.True(rail.DisabledSkills()["Skill-A"])
	s.True(rail.DisabledSkills()["skill_b"])
	s.False(rail.DisabledSkills()["skill-a"]) // 不同大小写
	s.False(rail.DisabledSkills()["skill-c"])
}

// TestEvolutionRail_WithMaxTrajectorySteps 测试最大轨迹步骤配置。
func (s *EvolutionRailSuite) TestEvolutionRail_WithMaxTrajectorySteps() {
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithMaxTrajectorySteps(50),
	)
	cbc := newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_max"}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))
	s.NotNil(rail.Builder())
}

// TestEvolutionRail_WithDefaultMemberRole 测试默认成员角色配置。
func (s *EvolutionRailSuite) TestEvolutionRail_WithDefaultMemberRole() {
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithDefaultMemberRole("researcher"),
	)
	err := rail.SetTrajectorySink(&fakeTrajectorySink{}, "team_1")
	s.Require().NoError(err)
}

// TestEvolutionRail_WithEvolutionTriggerNone 测试触发点设为 NONE。
func (s *EvolutionRailSuite) TestEvolutionRail_WithEvolutionTriggerNone() {
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithEvolutionTrigger(evolution.TriggerNone),
	)
	s.NotNil(rail)
}

// TestEvolutionRail_WithMaxConcurrentEvolution 测试并发演化数配置。
func (s *EvolutionRailSuite) TestEvolutionRail_WithMaxConcurrentEvolution() {
	rail := evolution.NewEvolutionRail(testExtension{},
		evolution.WithMaxConcurrentEvolution(3),
	)
	s.NotNil(rail)
}

// TestTrajectoryRail_Priority10 测试 TrajectoryRail 优先级。
//
// 对齐 Python: TestTrajectoryRail.test_priority
func (s *EvolutionRailSuite) TestTrajectoryRail_Priority10() {
	rail := evolution.NewTrajectoryRail()
	s.Equal(10, rail.Priority())
}

// TestTrajectoryRail_只收集不演化 测试 TrajectoryRail 只收集轨迹，不触发演化。
//
// 对齐 Python: TestTrajectoryRail.test_trajectory_rail_collects_only
func (s *EvolutionRailSuite) TestTrajectoryRail_只收集不演化() {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := evolution.NewTrajectoryRail(evolution.WithTrajectoryStore(store))

	cbc := newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_789"}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))

	cbc = newCBC(nil, &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("test")},
		Response: &llmschema.AssistantMessage{
			DefaultMessage: *llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "ok"),
		},
	}, nil)
	s.Require().NoError(rail.AfterModelCall(s.Ctx, cbc))

	cbc = newCBC(nil, &agentinterfaces.InvokeInputs{ConversationID: "conv_789"}, nil)
	s.Require().NoError(rail.AfterInvoke(s.Ctx, cbc))

	trajs := store.Query("", map[string]any{"session_id": "conv_789"})
	s.Len(trajs, 1)
	s.Equal("conv_789", trajs[0].SessionID)
}

// TestTrajectoryRail_GetCallbacks 测试 TrajectoryRail 注册回调事件。
func (s *EvolutionRailSuite) TestTrajectoryRail_GetCallbacks() {
	rail := evolution.NewTrajectoryRail()
	callbacks := rail.GetCallbacks()
	s.Contains(callbacks, agentinterfaces.CallbackBeforeInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterInvoke)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newCBC 创建回调上下文辅助方法。
func newCBC(
	agent agentinterfaces.BaseAgent,
	inputs agentinterfaces.EventInputs,
	sess sessioninterfaces.SessionFacade,
) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)
}

// fakeTrajectorySink 用于测试的轨迹 Sink 桩。
type fakeTrajectorySink struct{}

func (f *fakeTrajectorySink) PublishMemberTrajectory(_ *trajectory.MemberTrajectorySnapshot) {}
