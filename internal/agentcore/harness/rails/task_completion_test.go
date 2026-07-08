package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
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
	err := r.BeforeModelCall(nil, nil)
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
	err := r.BeforeTaskIteration(nil, nil)
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
	err := r.AfterTaskIteration(nil, nil)
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
func (f *fakeDeepAgentForNotify) DeepConfig() *hschema.DeepAgentConfig         { return nil }
func (f *fakeDeepAgentForNotify) IsInvokeActive() bool                         { return false }
func (f *fakeDeepAgentForNotify) IsAutoInvokeScheduled() bool                  { return false }
func (f *fakeDeepAgentForNotify) SetAutoInvokeScheduled(_ bool)                {}
func (f *fakeDeepAgentForNotify) ScheduleAutoInvokeOnSpawnDone(_ string) error { return nil }
func (f *fakeDeepAgentForNotify) CreateSubagent(_ string, _ string) (hinterfaces.DeepAgentInterface, error) {
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
func (f *fakeDeepAgentForNotify) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {}

// 确保编译时 fakeDeepAgentForNotify 满足必要的接口
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForNotify)(nil)
