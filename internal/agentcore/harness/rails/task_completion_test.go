package rails

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// --- 辅助函数测试 ---

func TestNormalizePromiseText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello world", normalizePromiseText("  hello   world  "))
	assert.Equal(t, "abc", normalizePromiseText("abc"))
	assert.Equal(t, "", normalizePromiseText(""))
	assert.Equal(t, "a b c", normalizePromiseText("a\n\tb\nc"))
}

func TestExtractPromiseBlock(t *testing.T) {
	t.Parallel()

	// 正常提取
	assert.Equal(t, "done", ExtractPromiseBlock("<promise>done</promise>"))
	assert.Equal(t, "task complete", ExtractPromiseBlock("<promise>task complete</promise>"))

	// 带空白
	assert.Equal(t, "done", ExtractPromiseBlock("<promise>  done  </promise>"))

	// 多行内容
	assert.Equal(t, "done\ndetails here", ExtractPromiseBlock("<promise>done\ndetails here</promise>"))

	// 无 promise 标签
	assert.Equal(t, "", ExtractPromiseBlock("no promise here"))
	assert.Equal(t, "", ExtractPromiseBlock(""))

	// 大小写不敏感
	assert.Equal(t, "done", ExtractPromiseBlock("<PROMISE>done</PROMISE>"))
}

func TestPromiseMatches(t *testing.T) {
	t.Parallel()

	// 精确匹配
	assert.True(t, PromiseMatches("done", "done"))

	// 首行匹配
	assert.True(t, PromiseMatches("done extra details", "done"))

	// 空值
	assert.False(t, PromiseMatches("", "done"))
	assert.False(t, PromiseMatches("done", ""))

	// 不匹配
	assert.False(t, PromiseMatches("other", "done"))

	// 首行后跟换行+详情
	assert.True(t, PromiseMatches("done\nextra details", "done"))
}

// --- 构造函数测试 ---

func TestNewTaskCompletionRail_默认构造(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	assert.Equal(t, 10, r.Priority())
	assert.Equal(t, "", r.taskInstruction)
	assert.Equal(t, "", r.completionPromise)
	assert.Equal(t, 1, r.requiredConfirmations)
	assert.False(t, r.allowPromiseDetails)
	assert.Equal(t, 0, r.maxRounds)
	assert.Equal(t, float64(0), r.timeoutSeconds)
	assert.Nil(t, r.extraEvaluators)
}

func TestNewTaskCompletionRail_带选项(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(
		WithTaskInstruction("请完成：{query}"),
		WithCompletionPromise("task_done"),
		WithRequiredConfirmations(3),
		WithAllowPromiseDetails(true),
		WithMaxRounds(10),
		WithTimeoutSeconds(300),
	)
	assert.Equal(t, "请完成：{query}", r.taskInstruction)
	assert.Equal(t, "task_done", r.completionPromise)
	assert.Equal(t, 3, r.requiredConfirmations)
	assert.True(t, r.allowPromiseDetails)
	assert.Equal(t, 10, r.maxRounds)
	assert.Equal(t, float64(300), r.timeoutSeconds)
}

func TestWithRequiredConfirmations_最小值(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithRequiredConfirmations(0))
	assert.Equal(t, 1, r.requiredConfirmations)
}

// --- BuildEvaluators 测试 ---

func TestBuildEvaluators_全参数(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(
		WithMaxRounds(5),
		WithTimeoutSeconds(60),
		WithCompletionPromise("done"),
	)
	evaluators := r.BuildEvaluators()
	require.Len(t, evaluators, 3)
	assert.Equal(t, "MaxRoundsEvaluator", evaluators[0].Name())
	assert.Equal(t, "TimeoutEvaluator", evaluators[1].Name())
	assert.Equal(t, "CompletionPromiseEvaluator", evaluators[2].Name())
}

func TestBuildEvaluators_仅MaxRounds(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithMaxRounds(5))
	evaluators := r.BuildEvaluators()
	require.Len(t, evaluators, 1)
	assert.Equal(t, "MaxRoundsEvaluator", evaluators[0].Name())
}

func TestBuildEvaluators_仅CompletionPromise(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	evaluators := r.BuildEvaluators()
	require.Len(t, evaluators, 1)
	assert.Equal(t, "CompletionPromiseEvaluator", evaluators[0].Name())
}

func TestBuildEvaluators_默认无参(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	evaluators := r.BuildEvaluators()
	assert.Len(t, evaluators, 0)
}

func TestBuildEvaluators_含额外评估器(t *testing.T) {
	t.Parallel()

	custom := task_loop.NewCustomPredicateEvaluator("custom", func(_ task_loop.StopEvaluationContext) bool { return false })
	r := NewTaskCompletionRail(
		WithMaxRounds(5),
		WithExtraEvaluators(custom),
	)
	evaluators := r.BuildEvaluators()
	require.Len(t, evaluators, 2)
	assert.Equal(t, "MaxRoundsEvaluator", evaluators[0].Name())
	assert.Equal(t, "custom", evaluators[1].Name())
}

// --- GetCallbacks 测试 ---

func TestTaskCompletionRail_GetCallbacks(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	callbacks := r.GetCallbacks()

	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeTaskIteration)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// --- BeforeModelCall 测试 ---

func TestBeforeModelCall_无Promise时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	// completionPromise 为空，BeforeModelCall 直接返回 nil
	err := r.BeforeModelCall(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestBeforeModelCall_注入节(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	callbacks := r.GetCallbacks()

	// 验证回调已注册
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeModelCall)
}

// --- BeforeTaskIteration 测试 ---

func TestBeforeTaskIteration_无TaskInstruction时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	err := r.BeforeTaskIteration(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestBeforeTaskIteration_格式化Query(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithTaskInstruction("请完成：{query}"))
	callbacks := r.GetCallbacks()

	// 验证回调已注册
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeTaskIteration)
}

// --- AfterTaskIteration 测试 ---

func TestAfterTaskIteration_无Promise时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail()
	err := r.AfterTaskIteration(context.TODO(), nil)
	assert.NoError(t, err)
}

func TestAfterTaskIteration_回调已注册(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	callbacks := r.GetCallbacks()

	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterTaskIteration)
}

// --- extractOutput 测试 ---

func TestExtractOutput_正常提取(t *testing.T) {
	t.Parallel()

	// 构造一个带有 TaskIterationInputs 的回调上下文
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"output": "hello world"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	assert.Equal(t, "hello world", extractOutput(cbc))
}

func TestExtractOutput_无Result(t *testing.T) {
	t.Parallel()

	inputs := &agentinterfaces.TaskIterationInputs{Result: nil}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	assert.Equal(t, "", extractOutput(cbc))
}

func TestExtractOutput_无Output键(t *testing.T) {
	t.Parallel()

	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"other": "value"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	assert.Equal(t, "", extractOutput(cbc))
}

func TestExtractOutput_Output非字符串(t *testing.T) {
	t.Parallel()

	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"output": 42},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	assert.Equal(t, "42", extractOutput(cbc))
}

func TestExtractOutput_非TaskIterationInputs(t *testing.T) {
	t.Parallel()

	// 传入 ModelCallInputs 而非 TaskIterationInputs，应返回空
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	assert.Equal(t, "", extractOutput(cbc))
}

// --- BeforeModelCall 完整测试 ---

func TestBeforeModelCall_注入完成信号节(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	agent := newFakeBaseAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	require.NoError(t, err)

	// 验证 builder 中已添加 completion_signal 节
	builder := agent.SystemPromptBuilder()
	assert.NotNil(t, builder)
	assert.True(t, builder.HasSection("completion_signal"))
}

func TestBeforeModelCall_builder为nil时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	agent := &fakeBaseAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("test"), builder: nil}
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.BeforeModelCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// --- BeforeTaskIteration 完整测试 ---

func TestBeforeTaskIteration_格式化Query完整(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithTaskInstruction("请完成以下任务：{query}"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Query:      "写一个排序算法",
		IsFollowUp: false,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeTaskIteration(context.Background(), cbc)
	require.NoError(t, err)

	assert.Equal(t, "请完成以下任务：写一个排序算法", inputs.Query)
}

func TestBeforeTaskIteration_isFollowUp时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithTaskInstruction("请完成：{query}"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Query:      "原始查询",
		IsFollowUp: true,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeTaskIteration(context.Background(), cbc)
	require.NoError(t, err)

	// follow-up 时 query 不被修改
	assert.Equal(t, "原始查询", inputs.Query)
}

func TestBeforeTaskIteration_query为空时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithTaskInstruction("请完成：{query}"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Query:      "",
		IsFollowUp: false,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeTaskIteration(context.Background(), cbc)
	require.NoError(t, err)

	assert.Equal(t, "", inputs.Query)
}

func TestBeforeTaskIteration_非TaskIterationInputs时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithTaskInstruction("请完成：{query}"))
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.BeforeTaskIteration(context.Background(), cbc)
	assert.NoError(t, err)
}

// --- AfterTaskIteration 完整测试 ---

func TestAfterTaskIteration_promise匹配成功(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{
			"output": "任务完成 <promise>task_done</promise>",
		},
	}
	agent := newFakeBaseAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
	// promise 匹配成功后调用 notifyEvaluator，但因为 fakeBaseAgent 不实现 DeepAgentInterface，
	// notifyEvaluator 内部类型断言会安全返回
}

func TestAfterTaskIteration_promise不匹配(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{
			"output": "任务完成 <promise>other_promise</promise>",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

func TestAfterTaskIteration_allowPromiseDetails时匹配(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(
		WithCompletionPromise("task_done"),
		WithAllowPromiseDetails(true),
	)
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{
			"output": "任务完成 <promise>task_done 额外详情</promise>",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

func TestAfterTaskIteration_无promise标签时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{
			"output": "任务完成，没有 promise 标签",
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

func TestAfterTaskIteration_output为空时跳过(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("task_done"))
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := r.AfterTaskIteration(context.Background(), cbc)
	require.NoError(t, err)
}

// --- GetCallbacks 完整调用测试 ---

func TestTaskCompletionRail_GetCallbacks_调用BeforeModelCall(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	require.True(t, ok)

	agent := newFakeBaseAgent()
	inputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)

	builder := agent.SystemPromptBuilder()
	assert.True(t, builder.HasSection("completion_signal"))
}

func TestTaskCompletionRail_GetCallbacks_调用BeforeTaskIteration(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithTaskInstruction("完成：{query}"))
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackBeforeTaskIteration]
	require.True(t, ok)

	inputs := &agentinterfaces.TaskIterationInputs{Query: "hello"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
	assert.Equal(t, "完成：hello", inputs.Query)
}

func TestTaskCompletionRail_GetCallbacks_调用AfterTaskIteration(t *testing.T) {
	t.Parallel()

	r := NewTaskCompletionRail(WithCompletionPromise("done"))
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[agentinterfaces.CallbackAfterTaskIteration]
	require.True(t, ok)

	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"output": "<promise>done</promise>"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	err := fn(context.Background(), cbc)
	require.NoError(t, err)
}

// --- notifyEvaluator 测试 ---

func TestNotifyEvaluator_非DeepAgentInterface时安全返回(t *testing.T) {
	t.Parallel()

	// fakeBaseAgent 不实现 DeepAgentInterface，应安全返回
	agent := newFakeBaseAgent()
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"output": "<promise>done</promise>"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	// 不会 panic
	notifyEvaluator(cbc, "done")
}

func TestNotifyEvaluator_完整链路(t *testing.T) {
	t.Parallel()

	// 构建一个实现 DeepAgentInterface 的 mock agent
	ev := task_loop.NewCompletionPromiseEvaluator("task_done", 1)
	coord := task_loop.NewLoopCoordinator([]task_loop.StopConditionEvaluator{ev})

	agent := &fakeDeepAgentForNotify{
		fakeBaseAgent: *newFakeBaseAgent(),
		coord:         coord,
	}
	inputs := &agentinterfaces.TaskIterationInputs{
		Result: map[string]any{"output": "<promise>task_done</promise>"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	notifyEvaluator(cbc, "task_done")

	// 验证 evaluator 被通知
	assert.True(t, ev.ShouldStop(task_loop.StopEvaluationContext{}))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fakeDeepAgentForNotify 实现 DeepAgentInterface 的测试 mock
type fakeDeepAgentForNotify struct {
	// hinterfaces.UnimplementedDeepAgentInteraction 交互方法空实现（9.87）
	hinterfaces.UnimplementedDeepAgentInteraction
	fakeBaseAgent
	coord *task_loop.LoopCoordinator
}

func (f *fakeDeepAgentForNotify) ReactAgent() *agents.ReActAgent { return nil }
func (f *fakeDeepAgentForNotify) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return f.coord
}
func (f *fakeDeepAgentForNotify) LoopController() controller.ControllerInterface { return nil }
func (f *fakeDeepAgentForNotify) EventHandler() modules.EventHandler             { return nil }
func (f *fakeDeepAgentForNotify) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeDeepAgentForNotify) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeDeepAgentForNotify) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForNotify) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForNotify) SetAutoInvokeScheduled(_ bool)        {}
func (f *fakeDeepAgentForNotify) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}
func (f *fakeDeepAgentForNotify) CreateSubagent(_ context.Context, _ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}

// Invoke 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// SwitchMode 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) SwitchMode(_ sessioninterfaces.SessionFacade, _ string) {}

// RestoreModeAfterPlanExit 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}

// GetPlanFilePath 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string { return "" }

// SaveState 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {
}

// FindRailsByType 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) FindRailsByType(_ ...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}

// AddRail 实现 DeepAgentInterface 接口（M-02 新增方法的测试桩）。
func (f *fakeDeepAgentForNotify) AddRail(_ agentinterfaces.AgentRail) {} // M-02: 接口新增 AddRail 的测试桩

func (f *fakeDeepAgentForNotify) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// InnerInvokeOverride 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentForNotify) InnerInvokeOverride() func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// 确保编译时 fakeDeepAgentForNotify 满足必要的接口
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForNotify)(nil)

// ──────────────────────────── goal 分支测试 ────────────────────────────

// TestSetGoalManager_惰性建sinkAndEvaluator 验证 SetGoalManager 注入 + 惰性建 sink/evaluator。
func TestSetGoalManager_惰性建sinkAndEvaluator(t *testing.T) {
	r := NewTaskCompletionRail()
	assert.Nil(t, r.goalReportSink)
	assert.Nil(t, r.goalEvaluator)

	r.SetGoalManager(goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             &fakeGoalStoreForRail{},
		EventManager:      task_loop.NewEventManager(),
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(_ context.Context, _ hschema.RoundWorkKind, _, _ string) {},
		EmitEvent:         func(_ hschema.InteractionEvent) {},
		NotifyWork:        func() {},
	}))

	assert.NotNil(t, r.goalManager)
	assert.NotNil(t, r.goalReportSink)
	assert.NotNil(t, r.goalEvaluator)
}

// TestValidateGoalState_各分支 验证状态与代际校验。
func TestValidateGoalState_各分支(t *testing.T) {
	active := goal.CreateGoalRecord("sess-1", "目标", nil, nil)
	active.GoalID = "g1"
	active.Revision = 2

	// nil record 空记录
	assert.False(t, validateGoalState(nil, "g1", 2, false))
	// status 不匹配
	active.Status = goal.GoalStatusCompleted
	assert.False(t, validateGoalState(active, "g1", 2, false))
	// allowPaused 下 PAUSED 通过
	active.Status = goal.GoalStatusPaused
	assert.True(t, validateGoalState(active, "g1", 2, true))
	assert.False(t, validateGoalState(active, "g1", 2, false))
	// goalID 不匹配
	active.Status = goal.GoalStatusActive
	assert.False(t, validateGoalState(active, "g2", 2, false))
	// revision 不匹配
	assert.False(t, validateGoalState(active, "g1", 3, false))
	// 全匹配
	assert.True(t, validateGoalState(active, "g1", 2, false))
	// goalID 为空（对齐 Python None 语义，只查 status）
	assert.True(t, validateGoalState(active, "", 0, false))
}

// TestGetRunKind_三级回退 验证 run_kind 解析三级回退。
func TestGetRunKind_三级回退(t *testing.T) {
	r := NewTaskCompletionRail()

	// 第一级：TaskIterationInputs.RunKind
	inputs := &agentinterfaces.TaskIterationInputs{RunKind: agentinterfaces.RunKindGoal}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	assert.Equal(t, "goal", r.getRunKind(cbc))

	// 第二级：InvokeInputs.RunKind
	inv := &agentinterfaces.InvokeInputs{RunKind: agentinterfaces.RunKindCron}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, inv, nil)
	assert.Equal(t, "cron", r.getRunKind(cbc2))

	// 第三级：extra["run_kind"]
	inputs3 := &agentinterfaces.TaskIterationInputs{}
	cbc3 := agentinterfaces.NewAgentCallbackContext(nil, inputs3, nil)
	cbc3.SetExtra("run_kind", "goal")
	assert.Equal(t, "goal", r.getRunKind(cbc3))

	// 全无 → 空
	cbc4 := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.TaskIterationInputs{}, nil)
	assert.Equal(t, "", r.getRunKind(cbc4))
}

// TestResolveGoalLang_回退 验证语言解析回退 cn。
func TestResolveGoalLang_回退(t *testing.T) {
	assert.Equal(t, "en", resolveGoalLang("en"))
	assert.Equal(t, "cn", resolveGoalLang("cn"))
	assert.Equal(t, "cn", resolveGoalLang("xx"))
	assert.Equal(t, "cn", resolveGoalLang(""))
}

// fakeGoalStoreForRail 测试用 goal store（Load 返回 nil）。
type fakeGoalStoreForRail struct{}

func (f *fakeGoalStoreForRail) SessionID() string              { return "sess-1" }
func (f *fakeGoalStoreForRail) Load() *goal.GoalRecord         { return nil }
func (f *fakeGoalStoreForRail) Save(_ *goal.GoalRecord)        {}
func (f *fakeGoalStoreForRail) Clear()                         {}
func (f *fakeGoalStoreForRail) Commit(_ context.Context) error { return nil }

// TestInit_从agent继承GoalManager兜底 验证 rail 未显式注入时从 agent 继承。
// Python: init() 里 if self._goal_manager is None: 从 agent.goal_manager 继承 (L238-241)
func TestInit_从agent继承GoalManager兜底(t *testing.T) {
	mgr := goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             &fakeGoalStoreForRail{},
		EventManager:      task_loop.NewEventManager(),
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(_ context.Context, _ hschema.RoundWorkKind, _, _ string) {},
		EmitEvent:         func(_ hschema.InteractionEvent) {},
		NotifyWork:        func() {},
	})

	r := NewTaskCompletionRail()
	assert.Nil(t, r.goalManager, "未注入时应为 nil")

	agent := newFakeBaseAgent()
	agent.goalMgr = mgr

	require.NoError(t, r.Init(context.Background(), agent))
	assert.Same(t, mgr, r.goalManager, "Init 应从 agent 继承 GoalManager")
	assert.NotNil(t, r.goalReportSink, "继承走 SetGoalManager，应惰性建 sink")
}

// TestInit_幂等不重复注册 验证二次 Init 不重复 append goalTools。
func TestInit_幂等不重复注册(t *testing.T) {
	mgr := goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             &fakeGoalStoreForRail{},
		EventManager:      task_loop.NewEventManager(),
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(_ context.Context, _ hschema.RoundWorkKind, _, _ string) {},
		EmitEvent:         func(_ hschema.InteractionEvent) {},
		NotifyWork:        func() {},
	})

	r := NewTaskCompletionRail()
	r.SetGoalManager(mgr)
	agent := newFakeBaseAgent()

	require.NoError(t, r.Init(context.Background(), agent))
	first := len(r.goalTools)
	require.Greater(t, first, 0, "首次 Init 应注册工具")

	require.NoError(t, r.Init(context.Background(), agent))
	assert.Equal(t, first, len(r.goalTools), "二次 Init 不应重复注册")
}
