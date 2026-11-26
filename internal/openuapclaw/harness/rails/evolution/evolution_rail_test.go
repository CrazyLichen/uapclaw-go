package evolution

import (
	"context"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

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

// ──────────────────────────── 【12-04】SetTrajectorySink 测试已删除 ────────────────────────────
// sink 链为 Go 侧 registry 遗产，上游无对应物（规则 8）。

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

	// 通过 buildTrajectory 验证 span 已被记录
	traj := rail.buildTrajectory()
	assert.NotNil(t, traj)
	assert.NotEmpty(t, trajectory.IterSpans(traj))
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

	// 记录一个 LLM span（span 模型）
	require.NoError(t, rail.Builder().RecordTestStep(trajectory.TestStep{Kind: "llm", Messages: baseMsgMaps([]llmschema.BaseMessage{llmschema.NewUserMessage("hi")})}))

	traj := rail.buildTrajectory()
	assert.NotNil(t, traj)
	assert.Equal(t, "sess-1", *traj.SessionID())
	assert.Len(t, trajectory.IterSpans(traj), 1)
}

// ──────────────────────────── 【12-04】publishTrajectorySnapshot 测试已删除 ────────────────────────────

// ──────────────────────────── emitBackgroundOutcomeEvent 测试 ────────────────────────────

func TestEmitBackgroundOutcomeEvent_失败状态(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	outcome := map[string]string{
		"status":      "failed",
		"message":     "evolution error occurred",
		"rail_kind":   "skill_evolution",
		"skill_name":  "search_optimization",
		"request_id":  "req-1",
		"stage":       "apply",
		"signal_type": "experience",
		"source":      "auto",
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

	// 记录 span 使 builder 非空（span 模型）
	rail.Builder().RecordTestStep(trajectory.TestStep{Kind: "llm", Messages: baseMsgMaps([]llmschema.BaseMessage{llmschema.NewUserMessage("hi")})})

	err := rail.AfterInvoke(context.Background(), cbc)
	assert.NoError(t, err)
	assert.Equal(t, 1, ext.runEvolutionCalled)
}

// ──────────────────────────── normalizeSkillNames / normalizeNameSet 测试 ────────────────────────────

func TestNormalizeSkillNames(t *testing.T) {
	result := normalizeSkillNames([]string{"a", "b"})
	assert.Equal(t, map[string]bool{"a": true, "b": true}, result)
}

// TestWithDefaultMemberRole 验证 WithDefaultMemberRole 选项。
func TestWithDefaultMemberRole(t *testing.T) {
	r := &EvolutionRail{}
	WithDefaultMemberRole("leader")(r)
	assert.Equal(t, "leader", r.defaultMemberRole)
}

func TestNormalizeNameSet_字符串输入(t *testing.T) {
	result := normalizeSkillNames([]string{"x"})
	assert.Equal(t, map[string]bool{"x": true}, result)
}

// ──────────────────────────── isSkillDisabled 测试 ────────────────────────────

func TestIsSkillDisabled(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithDisabledSkills([]string{"foo"}))
	assert.True(t, rail.isSkillDisabled("foo"))
	assert.False(t, rail.isSkillDisabled("bar"))
}

// ──────────────────────────── trajectoryToMessages 测试 ────────────────────────────

func TestCollectMessagesFromTrajectory(t *testing.T) {
	traj := testTrajectoryFromSteps("test", []trajectory.TestStep{llmStep(llmschema.NewUserMessage("hi"))})
	msgs := trajectoryToMessages(traj)
	assert.Len(t, msgs, 1)
}

// ──────────────────────────── getAgentIDStr 测试 ────────────────────────────

func TestGetAgentIDStr_nil(t *testing.T) {
	assert.Equal(t, "unknown", getAgentIDStr(nil))
}

func TestGetAgentIDStr_noAgent(t *testing.T) {
	cbc := &agentinterfaces.AgentCallbackContext{}
	assert.Equal(t, "unknown", getAgentIDStr(cbc))
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

// ──────────────────────────── AfterModelCall 从 Config 获取模型名 ────────────────────────────

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

// 【12-04】fakeTrajectorySink 已删除（sink 链规则 8）。

// fakeSessionFacade 用于测试的模拟 SessionFacade
type fakeSessionFacade struct {
	sessionID string
	// stateMap 模拟会话状态存储（evolution_interrupt_rail 测试需要 auto_confirm 存取）
	stateMap map[string]any
	stateMu  sync.Mutex
}

// GetAgentID 空桩（SessionFacade 15.24c 扩面：GetAgentID/GetTeamID）。
func (f *fakeSessionFacade) GetAgentID() string { return "" }

// GetTeamID 空桩（SessionFacade 15.24c 扩面）。
func (f *fakeSessionFacade) GetTeamID() string { return "" }

func (f *fakeSessionFacade) GetSessionID() string { return f.sessionID }

func (f *fakeSessionFacade) UpdateState(data map[string]any) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	if f.stateMap == nil {
		f.stateMap = map[string]any{}
	}
	for k, v := range data {
		f.stateMap[k] = v
	}
}

func (f *fakeSessionFacade) GetState(key state.StateKey) (any, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	v, ok := f.stateMap[key.String()]
	if !ok {
		return nil, nil
	}
	return v, nil
}
func (f *fakeSessionFacade) DumpState() map[string]any                  { return map[string]any{} }
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error {
	return nil
}
func (f *fakeSessionFacade) ClearSession(_ context.Context) {}

// 确保实现接口
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
	messages := trajectoryToMessages(traj)
	return &EvolutionSnapshot{Trajectory: traj, Messages: messages}
}

// ---------------------------------------------------------------------------
// formatTimeoutSecs 超时格式化
// ---------------------------------------------------------------------------

// TestFormatTimeoutSecs_整数 测试整数秒格式化。
func TestFormatTimeoutSecs_整数(t *testing.T) {
	assert.Equal(t, "30", formatTimeoutSecs(30.0))
}

// TestFormatTimeoutSecs_小数 测试小数秒格式化。
func TestFormatTimeoutSecs_小数(t *testing.T) {
	assert.Equal(t, "1.5", formatTimeoutSecs(1.5))
}

// TestFormatTimeoutSecs_零 测试零值格式化。
func TestFormatTimeoutSecs_零(t *testing.T) {
	assert.Equal(t, "0", formatTimeoutSecs(0.0))
}

// TestFormatTimeoutSecs_去除尾零 测试去除尾部零。
func TestFormatTimeoutSecs_去除尾零(t *testing.T) {
	assert.Equal(t, "10", formatTimeoutSecs(10.00))
}

// TestFormatTimeoutSecs_去除尾点 测试去除尾部小数点。
func TestFormatTimeoutSecs_去除尾点(t *testing.T) {
	assert.Equal(t, "5", formatTimeoutSecs(5.0))
}

// GetCacheIdentity 返回零值缓存身份（测试替身，不参与 KV 亲和）。
// Python: SessionFacade.get_cache_identity 的测试替身实现。
func (f *fakeSessionFacade) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回空父会话（测试替身）。
func (f *fakeSessionFacade) GetParentSessionID() string { return "" }
