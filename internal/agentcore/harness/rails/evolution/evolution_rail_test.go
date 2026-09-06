package evolution

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── baseMessageToMap 测试 ────────────────────────────

func TestBaseMessageToMap_nil(t *testing.T) {
	assert.Equal(t, map[string]any{}, baseMessageToMap(nil))
}

func TestBaseMessageToMap_纯文本消息(t *testing.T) {
	msg := llmschema.NewUserMessage("hello")
	result := baseMessageToMap(msg)
	assert.Equal(t, "user", result["role"])
	assert.NotNil(t, result["content"])
	assert.NotContains(t, result, "name")
	assert.NotContains(t, result, "metadata")
}

func TestBaseMessageToMap_带Name和Metadata(t *testing.T) {
	msg := llmschema.NewDefaultMessage(llmschema.RoleTypeUser, "hello",
		llmschema.WithMessageName("test_user"),
		llmschema.WithMetadata(map[string]any{"key": "val"}),
	)
	result := baseMessageToMap(msg)
	assert.Equal(t, "user", result["role"])
	assert.Equal(t, "test_user", result["name"])
	meta, ok := result["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "val", meta["key"])
}

// ──────────────────────────── toolInfoToMap 测试 ────────────────────────────

func TestToolInfoToMap_nil(t *testing.T) {
	assert.Equal(t, map[string]any{}, toolInfoToMap(nil))
}

func TestToolInfoToMap_完整字段(t *testing.T) {
	tool := cschema.NewToolInfo("search", "搜索工具", map[string]any{"type": "object"})
	result := toolInfoToMap(tool)
	assert.Equal(t, "function", result["type"])
	assert.Equal(t, "search", result["name"])
	assert.Equal(t, "搜索工具", result["description"])
	assert.NotNil(t, result["parameters"])
}

// ──────────────────────────── EvolutionRail 构造测试 ────────────────────────────

func TestNewEvolutionRail_默认值(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	assert.NotNil(t, rail)
	assert.Equal(t, 60, rail.Priority())
	assert.NotNil(t, rail.TrajectoryStore())
	assert.Equal(t, TriggerAfterInvoke, rail.evolutionTrigger)
	assert.True(t, rail.asyncEvolution)
	assert.Empty(t, rail.DisabledSkills())
	assert.Nil(t, rail.Builder())
}

func TestNewEvolutionRail_WithTrajectoryStore(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewEvolutionRail(noOpExtension{}, WithTrajectoryStore(store))
	assert.Equal(t, store, rail.TrajectoryStore())
}

func TestNewEvolutionRail_WithMaxTrajectorySteps(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithMaxTrajectorySteps(100))
	assert.Equal(t, 100, rail.maxTrajectorySteps)
}

func TestNewEvolutionRail_WithEvolutionTrigger(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithEvolutionTrigger(TriggerAfterModelCall))
	assert.Equal(t, TriggerAfterModelCall, rail.evolutionTrigger)
}

func TestNewEvolutionRail_WithAsyncEvolution(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithAsyncEvolution(false))
	assert.False(t, rail.asyncEvolution)
}

func TestNewEvolutionRail_WithMaxConcurrentEvolution(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithMaxConcurrentEvolution(3))
	assert.Equal(t, 3, cap(rail.evolutionSem))
}

func TestNewEvolutionRail_WithDisabledSkills(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithDisabledSkills([]string{"skill_a", "skill_b"}))
	assert.True(t, rail.DisabledSkills()["skill_a"])
	assert.True(t, rail.DisabledSkills()["skill_b"])
}

// ──────────────────────────── EvolutionRail SetTrajectorySink 测试 ────────────────────────────

func TestSetTrajectorySink_正常绑定(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	sink := &fakeTrajectorySink{}
	rail.SetTrajectorySink(sink, "team-1", "leader")
	assert.Equal(t, sink, rail.trajectorySink)
	assert.Equal(t, "team-1", rail.teamID)
	assert.Equal(t, "leader", rail.memberRole)
}

func TestSetTrajectorySink_空TeamID不生效(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	sink := &fakeTrajectorySink{}
	rail.SetTrajectorySink(sink, "", "leader")
	assert.Nil(t, rail.trajectorySink)
}

func TestSetTrajectorySink_nilSink不绑定(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	rail.SetTrajectorySink(nil, "team-1")
	assert.Nil(t, rail.trajectorySink)
}

// ──────────────────────────── EvolutionRail EmitHostEvent / Drain 测试 ────────────────────────────

func TestEmitHostEvent_正常缓存(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	event := &stream.OutputSchema{Type: "test", Index: 0, Payload: map[string]any{"data": "test"}}
	rail.EmitHostEvent(event)
	assert.Len(t, rail.pendingHostEvents, 1)
}

func TestEmitHostEvent_nil忽略(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	rail.EmitHostEvent(nil)
	assert.Empty(t, rail.pendingHostEvents)
}

func TestDrainPendingHostEvents_不等待(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	event := &stream.OutputSchema{Type: "test", Index: 0, Payload: map[string]any{"data": "test"}}
	rail.EmitHostEvent(event)
	events := rail.DrainPendingHostEvents(false, nil)
	assert.Len(t, events, 1)
	// drain 后缓冲清空
	assert.Empty(t, rail.pendingHostEvents)
}

func TestDrainPendingApprovalEvents_别名(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	event := &stream.OutputSchema{Type: "test", Index: 0, Payload: map[string]any{"data": "test"}}
	rail.EmitHostEvent(event)
	events := rail.DrainPendingApprovalEvents(false, nil)
	assert.Len(t, events, 1)
}

// ──────────────────────────── EvolutionRail GetCallbacks 测试 ────────────────────────────

func TestGetCallbacks_注册5个事件(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	callbacks := rail.GetCallbacks()
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeInvoke)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterToolCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterInvoke)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// ──────────────────────────── EvolutionRail BeforeInvoke 测试 ────────────────────────────

func TestBeforeInvoke_创建新Builder(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})

	err := rail.BeforeInvoke(context.Background(), cbc)
	assert.NoError(t, err)
	assert.NotNil(t, rail.Builder())
}

func TestBeforeInvoke_同Session复用Builder(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})

	// 第一次 invoke 创建 builder
	err := rail.BeforeInvoke(context.Background(), cbc)
	require.NoError(t, err)
	firstBuilder := rail.Builder()

	// 第二次同 session 复用
	err = rail.BeforeInvoke(context.Background(), cbc)
	require.NoError(t, err)
	assert.Equal(t, firstBuilder, rail.Builder())
}

func TestBeforeInvoke_不同Session创建新Builder(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc1 := &agentinterfaces.AgentCallbackContext{}
	cbc1.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})

	err := rail.BeforeInvoke(context.Background(), cbc1)
	require.NoError(t, err)
	firstBuilder := rail.Builder()

	cbc2 := &agentinterfaces.AgentCallbackContext{}
	cbc2.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-2"})

	err = rail.BeforeInvoke(context.Background(), cbc2)
	require.NoError(t, err)
	assert.NotEqual(t, firstBuilder, rail.Builder())
}

// ──────────────────────────── EvolutionRail AfterModelCall 测试 ────────────────────────────

func TestAfterModelCall_记录LLM步骤(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	// 先创建 builder
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	// 模拟模型调用回调
	resp := llmschema.NewAssistantMessage("response")
	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{
			llmschema.NewUserMessage("hello"),
		},
		Tools:    []cschema.ToolInfoInterface{},
		Response: resp,
	}
	cbcModel := &agentinterfaces.AgentCallbackContext{}
	cbcModel.SetInputs(modelInputs)

	err := rail.AfterModelCall(context.Background(), cbcModel)
	assert.NoError(t, err)
}

func TestAfterModelCall_builder为空跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ModelCallInputs{})
	err := rail.AfterModelCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── EvolutionRail AfterToolCall 测试 ────────────────────────────

func TestAfterToolCall_记录Tool步骤(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	// 先创建 builder
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	// 模拟工具调用回调
	toolInputs := &agentinterfaces.ToolCallInputs{
		ToolName: "search",
		ToolArgs: map[string]any{"query": "test"},
		ToolCall: &llmschema.ToolCall{ID: "tc-1", Name: "search"},
	}
	cbcTool := &agentinterfaces.AgentCallbackContext{}
	cbcTool.SetInputs(toolInputs)

	err := rail.AfterToolCall(context.Background(), cbcTool)
	assert.NoError(t, err)
}

func TestAfterToolCall_builder为空跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: "search"})
	err := rail.AfterToolCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── EvolutionRail AfterInvoke 测试 ────────────────────────────

func TestAfterInvoke_保存轨迹(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	// 先创建 builder
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	// 记录一些步骤
	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("hello")},
		Response: llmschema.NewAssistantMessage("world"),
	}
	cbcModel := &agentinterfaces.AgentCallbackContext{}
	cbcModel.SetInputs(modelInputs)
	require.NoError(t, rail.AfterModelCall(context.Background(), cbcModel))

	// after_invoke 保存轨迹
	cbcInvoke := &agentinterfaces.AgentCallbackContext{}
	cbcInvoke.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})
	err := rail.AfterInvoke(context.Background(), cbcInvoke)
	assert.NoError(t, err)

	// 通过 buildTrajectory 验证步骤已被记录
	traj := rail.buildTrajectory()
	assert.NotNil(t, traj)
	assert.NotEmpty(t, traj.Steps)
}

func TestAfterInvoke_builder为空跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{})
	err := rail.AfterInvoke(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── EvolutionRail AfterTaskIteration 测试 ────────────────────────────

func TestAfterTaskIteration_noOpExtension不触发演化(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithEvolutionTrigger(TriggerAfterTaskIteration))
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.TaskIterationInputs{Iteration: 1})
	err := rail.AfterTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── EvolutionRail CleanupBackgroundTasks 测试 ────────────────────────────

func TestCleanupBackgroundTasks_无任务(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	err := rail.CleanupBackgroundTasks()
	assert.NoError(t, err)
}

// ──────────────────────────── EvolutionRail resetTrajectoryBuilder 测试 ────────────────────────────

func TestResetTrajectoryBuilder(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))
	assert.NotNil(t, rail.Builder())

	rail.resetTrajectoryBuilder()
	assert.Nil(t, rail.Builder())
}

// ──────────────────────────── EvolutionRail resolveSessionID 测试 ────────────────────────────

func TestResolveSessionID_从Session获取(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	sess := &fakeSessionFacade{sessionID: "from-session"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, sess)
	inputs := &agentinterfaces.InvokeInputs{ConversationID: "from-input"}

	sid := rail.resolveSessionID(cbc, inputs)
	assert.Equal(t, "from-session", sid)
}

func TestResolveSessionID_回退到ConversationID(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	inputs := &agentinterfaces.InvokeInputs{ConversationID: "from-input"}

	sid := rail.resolveSessionID(cbc, inputs)
	assert.Equal(t, "from-input", sid)
}

// ──────────────────────────── EvolutionRail buildTrajectory 测试 ────────────────────────────

func TestBuildTrajectory_nilBuilder(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	assert.Nil(t, rail.buildTrajectory())
}

func TestBuildTrajectory_正常构建(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-1"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	// 记录步骤
	rail.Builder().RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindLLM,
		Detail: &trajectory.LLMCallDetail{
			Model:    "test-model",
			Messages: []map[string]any{{"role": "user", "content": "hi"}},
		},
	})

	traj := rail.buildTrajectory()
	assert.NotNil(t, traj)
	assert.Equal(t, "sess-1", traj.SessionID)
	assert.Len(t, traj.Steps, 1)
}

// ──────────────────────────── TrajectoryRail 测试 ────────────────────────────

func TestNewTrajectoryRail_默认值(t *testing.T) {
	rail := NewTrajectoryRail()
	assert.NotNil(t, rail)
	assert.Equal(t, 10, rail.Priority())
	assert.NotNil(t, rail.TrajectoryStore())
}

func TestNewTrajectoryRail_带选项(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewTrajectoryRail(WithTrajectoryStore(store))
	assert.Equal(t, store, rail.TrajectoryStore())
}

func TestTrajectoryRail_收集轨迹(t *testing.T) {
	rail := NewTrajectoryRail()

	// before_invoke 初始化 builder
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-traj"})
	err := rail.BeforeInvoke(context.Background(), cbc)
	require.NoError(t, err)

	// 记录 LLM 步骤
	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("hi")},
		Response: llmschema.NewAssistantMessage("hello"),
	}
	cbcModel := &agentinterfaces.AgentCallbackContext{}
	cbcModel.SetInputs(modelInputs)
	err = rail.AfterModelCall(context.Background(), cbcModel)
	require.NoError(t, err)

	// 验证 builder 有步骤
	traj := rail.buildTrajectory()
	assert.NotNil(t, traj)
	assert.Len(t, traj.Steps, 1)
}

func TestTrajectoryRail_RunEvolution不执行任何操作(t *testing.T) {
	rail := NewTrajectoryRail()
	// TrajectoryRail 使用 noOpExtension，RunEvolution 不做任何事
	err := rail.ext.RunEvolution(context.Background(), nil, nil)
	assert.NoError(t, err)
}

// ──────────────────────────── noOpExtension 测试 ────────────────────────────

func TestNoOpExtension_AllMethods(t *testing.T) {
	ext := noOpExtension{}
	cbc := &agentinterfaces.AgentCallbackContext{}

	assert.NoError(t, ext.OnBeforeInvoke(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterModelCall(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterToolCall(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterInvoke(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterTaskIteration(context.Background(), cbc))
	assert.NoError(t, ext.OnAfterEvolutionTriggered(context.Background(), nil, cbc))
	assert.True(t, ext.AllowEvolutionTrigger(TriggerAfterInvoke, cbc))
	assert.NoError(t, ext.RunEvolution(context.Background(), nil, nil))
}

func TestNoOpExtension_SnapshotForEvolution(t *testing.T) {
	ext := noOpExtension{}
	traj := &trajectory.Trajectory{
		SessionID: "test",
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{{"role": "user", "content": "hi"}},
				},
			},
		},
	}
	snapshot := ext.SnapshotForEvolution(context.Background(), traj, nil)
	assert.NotNil(t, snapshot)
	assert.Equal(t, traj, snapshot.Trajectory)
	assert.NotEmpty(t, snapshot.Messages)
}

// ──────────────────────────── EvolutionTriggerPoint 测试 ────────────────────────────

func TestEvolutionTriggerPoint_值(t *testing.T) {
	assert.Equal(t, EvolutionTriggerPoint("after_invoke"), TriggerAfterInvoke)
	assert.Equal(t, EvolutionTriggerPoint("after_model_call"), TriggerAfterModelCall)
	assert.Equal(t, EvolutionTriggerPoint("after_tool_call"), TriggerAfterToolCall)
	assert.Equal(t, EvolutionTriggerPoint("after_task_iteration"), TriggerAfterTaskIteration)
	assert.Equal(t, EvolutionTriggerPoint("none"), TriggerNone)
}

// ──────────────────────────── publishTrajectorySnapshot 测试 ────────────────────────────

func TestPublishTrajectorySnapshot_无Sink跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	traj := &trajectory.Trajectory{SessionID: "sess-1", Meta: map[string]any{"member_id": "m-1"}}
	// 不绑定 sink，不应 panic
	rail.publishTrajectorySnapshot(traj)
}

func TestPublishTrajectorySnapshot_无TeamID跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	sink := &fakeTrajectorySink{}
	// teamID 为空时 sink 不生效
	rail.trajectorySink = sink
	rail.teamID = ""
	traj := &trajectory.Trajectory{SessionID: "sess-1", Meta: map[string]any{"member_id": "m-1"}}
	rail.publishTrajectorySnapshot(traj)
}

func TestPublishTrajectorySnapshot_无MemberID跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	sink := &fakeTrajectorySink{}
	rail.trajectorySink = sink
	rail.teamID = "team-1"
	traj := &trajectory.Trajectory{SessionID: "sess-1", Meta: map[string]any{}}
	rail.publishTrajectorySnapshot(traj)
}

func TestPublishTrajectorySnapshot_正常发布(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	sink := &fakeTrajectorySink{}
	rail.trajectorySink = sink
	rail.teamID = "team-1"
	rail.memberRole = "leader"
	traj := &trajectory.Trajectory{
		SessionID: "sess-1",
		Meta:      map[string]any{"member_id": "m-1", "member_role": "worker"},
	}
	rail.publishTrajectorySnapshot(traj)
	// 不会 panic 即成功
}

// ──────────────────────────── emitBackgroundOutcomeEvent 测试 ────────────────────────────

func TestEmitBackgroundOutcomeEvent_失败状态(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	outcome := map[string]string{
		"status":     "failed",
		"message":    "evolution error occurred",
		"rail_kind":  "skill_evolution",
		"skill_name": "search_optimization",
		"request_id": "req-1",
		"stage":      "apply",
		"signal_type": "experience",
		"source":     "auto",
	}
	rail.emitBackgroundOutcomeEvent(outcome)
	assert.Len(t, rail.pendingHostEvents, 1)
}

func TestEmitBackgroundOutcomeEvent_空消息(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	outcome := map[string]string{"status": "completed"}
	rail.emitBackgroundOutcomeEvent(outcome)
	assert.Len(t, rail.pendingHostEvents, 1)
}

func TestEmitBackgroundOutcomeEvent_空状态(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	outcome := map[string]string{}
	rail.emitBackgroundOutcomeEvent(outcome)
	assert.Len(t, rail.pendingHostEvents, 1)
}

// ──────────────────────────── syncEvolution 触发测试 ────────────────────────────

func TestAfterInvoke_同步模式触发演化(t *testing.T) {
	ext := &countingExtension{}
	rail := NewEvolutionRail(ext,
		WithEvolutionTrigger(TriggerAfterInvoke),
		WithAsyncEvolution(false),
	)
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-sync"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	// 记录步骤使 builder 非空
	rail.Builder().RecordStep(&trajectory.TrajectoryStep{
		Kind: trajectory.StepKindLLM,
		Detail: &trajectory.LLMCallDetail{
			Model:    "test",
			Messages: []map[string]any{{"role": "user", "content": "hi"}},
		},
	})

	err := rail.AfterInvoke(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 1, ext.runEvolutionCalled)
}

// ──────────────────────────── stringPtr / _isBlank 测试 ────────────────────────────

func TestStringPtr_空字符串(t *testing.T) {
	assert.Nil(t, stringPtr(""))
}

func TestStringPtr_非空字符串(t *testing.T) {
	p := stringPtr("hello")
	assert.NotNil(t, p)
	assert.Equal(t, "hello", *p)
}

func TestIsBlank(t *testing.T) {
	assert.True(t, _isBlank(""))
	assert.True(t, _isBlank("   "))
	assert.False(t, _isBlank("a"))
}

// ──────────────────────────── normalizeSkillNamesGo / _normalizeNameSet 测试 ────────────────────────────

func TestNormalizeSkillNamesGo(t *testing.T) {
	result := normalizeSkillNamesGo([]string{"a", "b"})
	assert.Equal(t, map[string]bool{"a": true, "b": true}, result)
}

func TestNormalizeNameSet(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	result := rail._normalizeNameSet([]string{"x"})
	assert.Equal(t, map[string]bool{"x": true}, result)
}

// ──────────────────────────── _isSkillDisabled 测试 ────────────────────────────

func TestIsSkillDisabled(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithDisabledSkills([]string{"foo"}))
	assert.True(t, rail._isSkillDisabled("foo"))
	assert.False(t, rail._isSkillDisabled("bar"))
}

// ──────────────────────────── _collectMessagesFromTrajectory 测试 ────────────────────────────

func TestCollectMessagesFromTrajectory_通过Rail方法(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{{"role": "user", "content": "hi"}},
				},
			},
		},
	}
	msgs := rail._collectMessagesFromTrajectory(traj)
	assert.Len(t, msgs, 1)
}

// ──────────────────────────── _getAgentIDStr 测试 ────────────────────────────

func TestGetAgentIDStr_nil(t *testing.T) {
	assert.Equal(t, "unknown", _getAgentIDStr(nil))
}

func TestGetAgentIDStr_noAgent(t *testing.T) {
	cbc := &agentinterfaces.AgentCallbackContext{}
	assert.Equal(t, "unknown", _getAgentIDStr(cbc))
}

// ──────────────────────────── BeforeInvoke inputs 类型不匹配 ────────────────────────────

func TestBeforeInvoke_非InvokeInputs跳过(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ModelCallInputs{})
	err := rail.BeforeInvoke(context.Background(), cbc)
	assert.NoError(t, err)
	// builder 不会被创建（因为 sessionID 为空时回退为空）
}

// ──────────────────────────── AfterModelCall with model name from Config ────────────────────────────

func TestAfterModelCall_从Config获取ModelName(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-model"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	resp := llmschema.NewAssistantMessage("response")
	modelInputs := &agentinterfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{llmschema.NewUserMessage("hello")},
		Response: resp,
	}
	cbcModel := &agentinterfaces.AgentCallbackContext{}
	cbcModel.SetInputs(modelInputs)

	err := rail.AfterModelCall(context.Background(), cbcModel)
	assert.NoError(t, err)
	// 没有 Agent 设置，model name 为 "unknown" — 不 panic 即通过
}

// ──────────────────────────── 辅助类型 ────────────────────────────

// fakeTrajectorySink 用于测试的模拟轨迹写入端点
type fakeTrajectorySink struct{}

func (f *fakeTrajectorySink) PublishMemberTrajectory(_ *trajectory.MemberTrajectorySnapshot) {}

// fakeSessionFacade 用于测试的模拟 SessionFacade
type fakeSessionFacade struct {
	sessionID string
}

func (f *fakeSessionFacade) GetSessionID() string                        { return f.sessionID }
func (f *fakeSessionFacade) UpdateState(_ map[string]any)                {}
func (f *fakeSessionFacade) GetState(_ state.StateKey) (any, error)     { return nil, nil }
func (f *fakeSessionFacade) DumpState() map[string]any                  { return map[string]any{} }
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error {
	return nil
}

// 确保实现接口
var _ trajectory.TrajectorySink = (*fakeTrajectorySink)(nil)
var _ sessioninterfaces.SessionFacade = (*fakeSessionFacade)(nil)

// countingExtension 用于测试演化触发计数的扩展点实现
type countingExtension struct {
	noOpExtension
	runEvolutionCalled int
}

func (c *countingExtension) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *EvolutionSnapshot) error {
	c.runEvolutionCalled++
	return nil
}

func (c *countingExtension) SnapshotForEvolution(_ context.Context, traj *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	messages := collectMessagesFromTrajectory(traj)
	return &EvolutionSnapshot{Trajectory: traj, Messages: messages}
}
