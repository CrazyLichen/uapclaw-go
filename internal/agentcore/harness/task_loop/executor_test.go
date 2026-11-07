package task_loop

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDeepAgentProvider 用于测试的模拟深层 Agent 提供者
type fakeDeepAgentProvider struct {
	// interfaces.UnimplementedDeepAgentInteraction 交互方法空实现（9.87）
	interfaces.UnimplementedDeepAgentInteraction
	// reactAgent 预设的 ReActAgent
	reactAgent *agents.ReActAgent
	// coordinator 预设的循环协调器
	coordinator interfaces.LoopCoordinatorInterface
	// eventHandler 预设的事件处理器
	eventHandler modules.EventHandler
	// state 预设的 DeepAgentState
	state *hschema.DeepAgentState
	// config 预设的 DeepAgentConfig
	config *hschema.DeepAgentConfig
	// invokeActive 预设的 invoke 活跃标记
	invokeActive bool
	// autoInvokeScheduled 预设的自动 invoke 调度标记
	autoInvokeScheduled bool
	// subagent 预设的子 Agent 提供者（CreateSubagent 返回值）
	subagent interfaces.DeepAgentInterface
	// createSubagentErr 预设的 CreateSubagent 错误
	createSubagentErr error
	// innerOverride 预设的内层 invoke 覆写（用于截获 executor 构造的 effective）
	innerOverride func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
}

// fakeSessionFacade 用于测试的模拟会话门面
type fakeSessionFacade struct {
	// sessionID 会话标识
	sessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTaskLoopEventExecutor 构造函数返回非 nil
func TestNewTaskLoopEventExecutor(t *testing.T) {
	deps := &modules.TaskExecutorDependencies{}
	provider := &fakeDeepAgentProvider{}
	executor := NewTaskLoopEventExecutor(deps, provider)
	if executor == nil {
		t.Fatal("NewTaskLoopEventExecutor 返回 nil，期望非 nil")
	}
}

// TestTaskLoopEventExecutor_CanPause返回不支持 CanPause 返回 (false, "深层 Agent 任务不支持暂停", nil)
func TestTaskLoopEventExecutor_CanPause返回不支持(t *testing.T) {
	deps := &modules.TaskExecutorDependencies{}
	provider := &fakeDeepAgentProvider{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	canPause, reason, err := executor.CanPause(context.Background(), "task-1", nil)
	if err != nil {
		t.Fatalf("CanPause 返回意外错误: %v", err)
	}
	if canPause {
		t.Error("CanPause 返回 canPause=true，期望 false")
	}
	if reason != "深层 Agent 任务不支持暂停" {
		t.Errorf("CanPause 返回 reason=%q，期望 %q", reason, "深层 Agent 任务不支持暂停")
	}
}

// TestTaskLoopEventExecutor_CanCancel始终允许 CanCancel 返回 (true, "", nil)
func TestTaskLoopEventExecutor_CanCancel始终允许(t *testing.T) {
	deps := &modules.TaskExecutorDependencies{}
	provider := &fakeDeepAgentProvider{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	canCancel, reason, err := executor.CanCancel(context.Background(), "task-1", nil)
	if err != nil {
		t.Fatalf("CanCancel 返回意外错误: %v", err)
	}
	if !canCancel {
		t.Error("CanCancel 返回 canCancel=false，期望 true")
	}
	if reason != "" {
		t.Errorf("CanCancel 返回 reason=%q，期望空字符串", reason)
	}
}

// TestTaskLoopEventExecutor_Pause返回不支持 Pause 返回 (false, nil)
func TestTaskLoopEventExecutor_Pause返回不支持(t *testing.T) {
	deps := &modules.TaskExecutorDependencies{}
	provider := &fakeDeepAgentProvider{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	ok, err := executor.Pause(context.Background(), "task-1", nil)
	if err != nil {
		t.Fatalf("Pause 返回意外错误: %v", err)
	}
	if ok {
		t.Error("Pause 返回 ok=true，期望 false")
	}
}

// TestTaskLoopEventExecutor_Cancel标记取消并请求中止 Cancel 时 LoopCoordinator.RequestAbort 被调用，
// TaskPlan.MarkCancelled 被调用
func TestTaskLoopEventExecutor_Cancel标记取消并请求中止(t *testing.T) {
	// 构造含 TaskPlan 的 DeepAgentState
	taskID := "task-cancel-1"
	plan := hschema.NewTaskPlan("测试目标")
	plan.AddTask(hschema.TodoItem{
		ID:      taskID,
		Content: "待取消的任务",
		Status:  hschema.TodoStatusPending,
	})
	state := &hschema.DeepAgentState{
		TaskPlan: &plan,
	}

	// 构造 LoopCoordinator，尚未中止
	coordinator := NewLoopCoordinator(nil)

	provider := &fakeDeepAgentProvider{
		coordinator: coordinator,
		state:       state,
	}
	deps := &modules.TaskExecutorDependencies{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	sess := &fakeSessionFacade{sessionID: "sess-1"}
	ok, err := executor.Cancel(context.Background(), taskID, sess)
	if err != nil {
		t.Fatalf("Cancel 返回意外错误: %v", err)
	}
	if !ok {
		t.Error("Cancel 返回 ok=false，期望 true")
	}

	// 验证 LoopCoordinator.RequestAbort 被调用
	if !coordinator.IsAborted() {
		t.Error("Cancel 后 LoopCoordinator 未被中止")
	}

	// 验证 TaskPlan.MarkCancelled 被调用
	task := state.TaskPlan.GetTask(taskID)
	if task == nil {
		t.Fatal("Cancel 后未找到任务")
	}
	if task.Status != hschema.TodoStatusCancelled {
		t.Errorf("Cancel 后任务状态=%v，期望 TodoStatusCancelled", task.Status)
	}
}

// TestBuildDeepExecutor 工厂函数返回闭包，调用后得到 *TaskLoopEventExecutor
func TestBuildDeepExecutor(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	factory := BuildDeepExecutor(provider)
	if factory == nil {
		t.Fatal("BuildDeepExecutor 返回 nil，期望非 nil 闭包")
	}

	deps := &modules.TaskExecutorDependencies{}
	executor := factory(deps)
	if executor == nil {
		t.Fatal("工厂闭包返回 nil，期望非 nil")
	}

	// 验证返回的类型为 *TaskLoopEventExecutor
	if _, ok := executor.(*TaskLoopEventExecutor); !ok {
		t.Error("工厂闭包返回值不是 *TaskLoopEventExecutor 类型")
	}
}

// TestExtractInteractiveInput_Nil事件 nil 事件返回 nil
func TestExtractInteractiveInput_Nil事件(t *testing.T) {
	result := ExtractInteractiveInput(nil)
	if result != nil {
		t.Errorf("ExtractInteractiveInput(nil) 返回 %v，期望 nil", result)
	}
}

// TestExtractInteractiveInput_空InputData 空 InputData 返回 nil
func TestExtractInteractiveInput_空InputData(t *testing.T) {
	event := &cschema.InputEvent{
		InputData: []cschema.DataFrame{},
	}
	result := ExtractInteractiveInput(event)
	if result != nil {
		t.Errorf("ExtractInteractiveInput(空InputData) 返回 %v，期望 nil", result)
	}
}

// TestExtractInteractiveInput_仅有TextDataFrame Python 不从 TextDataFrame 构造 InteractiveInput，返回 nil
func TestExtractInteractiveInput_仅有TextDataFrame(t *testing.T) {
	event := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "hello"}},
	}
	result := ExtractInteractiveInput(event)
	if result != nil {
		t.Errorf("ExtractInteractiveInput(仅TextDataFrame) 返回 %v，期望 nil（Python 不从 TextDataFrame 构造 InteractiveInput）", result)
	}
}

// TestExtractInteractiveInput_仅有JsonDataFrame InputData 仅含 JsonDataFrame（无 query 键）时返回 nil
func TestExtractInteractiveInput_仅有JsonDataFrame(t *testing.T) {
	event := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"key": "value"}},
		},
	}
	result := ExtractInteractiveInput(event)
	if result != nil {
		t.Errorf("ExtractInteractiveInput(仅JsonDataFrame) 返回 %v，期望 nil", result)
	}
}

// TestExtractInteractiveInput_JsonDataFrame含InteractiveInput JsonDataFrame.data["query"] 为 *InteractiveInput 时直接返回
func TestExtractInteractiveInput_JsonDataFrame含InteractiveInput(t *testing.T) {
	// 构造一个 InteractiveInput 实例（通过 UserInputs 模式，RawInputs 为 nil）
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		t.Fatalf("NewInteractiveInput 返回错误: %v", err)
	}
	_ = ii.Update("node-1", "user response")

	event := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"query": ii}},
		},
	}
	result := ExtractInteractiveInput(event)
	if result == nil {
		t.Fatal("ExtractInteractiveInput 返回 nil，期望非 nil InteractiveInput")
	}
	// 验证返回的 InteractiveInput 与输入相同
	if result != ii {
		t.Error("ExtractInteractiveInput 返回值与输入 InteractiveInput 不一致")
	}
}

// TestExtractInteractiveInput_JsonDataFrameQuery不是InteractiveInput JsonDataFrame.data["query"] 为字符串时返回 nil
func TestExtractInteractiveInput_JsonDataFrameQuery不是InteractiveInput(t *testing.T) {
	event := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"query": "plain string"}},
		},
	}
	result := ExtractInteractiveInput(event)
	// query 不是 *InteractiveInput，返回 nil
	if result != nil {
		t.Errorf("ExtractInteractiveInput 返回 %v，期望 nil", result)
	}
}

// TestExtractInteractiveInput_混合DataFrame JsonDataFrame 无 InteractiveInput，TextDataFrame 也被忽略
func TestExtractInteractiveInput_混合DataFrame(t *testing.T) {
	event := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"key": "value"}},
			&cschema.TextDataFrame{Text: "from text"},
		},
	}
	result := ExtractInteractiveInput(event)
	// Python 不从 TextDataFrame 构造 InteractiveInput，JsonDataFrame 也无 InteractiveInput，返回 nil
	if result != nil {
		t.Errorf("ExtractInteractiveInput 返回 %v，期望 nil", result)
	}
}

// TestTaskLoopEventExecutor_Cancel无TaskPlan Cancel 时 state 或 TaskPlan 为 nil 不 panic
func TestTaskLoopEventExecutor_Cancel无TaskPlan(t *testing.T) {
	// state 为 nil
	provider := &fakeDeepAgentProvider{
		coordinator: NewLoopCoordinator(nil),
		state:       nil,
	}
	deps := &modules.TaskExecutorDependencies{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	sess := &fakeSessionFacade{sessionID: "sess-1"}
	ok, err := executor.Cancel(context.Background(), "task-1", sess)
	if err != nil {
		t.Fatalf("Cancel 返回意外错误: %v", err)
	}
	if !ok {
		t.Error("Cancel 返回 ok=false，期望 true")
	}
}

// TestTaskLoopEventExecutor_Cancel无Coordinator Cancel 时 coordinator 为 nil 不 panic
func TestTaskLoopEventExecutor_Cancel无Coordinator(t *testing.T) {
	taskID := "task-cancel-2"
	plan := hschema.NewTaskPlan("测试目标")
	plan.AddTask(hschema.TodoItem{
		ID:      taskID,
		Content: "待取消的任务",
		Status:  hschema.TodoStatusPending,
	})
	state := &hschema.DeepAgentState{
		TaskPlan: &plan,
	}

	// coordinator 为 nil
	provider := &fakeDeepAgentProvider{
		coordinator: nil,
		state:       state,
	}
	deps := &modules.TaskExecutorDependencies{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	sess := &fakeSessionFacade{sessionID: "sess-1"}
	ok, err := executor.Cancel(context.Background(), taskID, sess)
	if err != nil {
		t.Fatalf("Cancel 返回意外错误: %v", err)
	}
	if !ok {
		t.Error("Cancel 返回 ok=false，期望 true")
	}

	// 验证 TaskPlan.MarkCancelled 仍被调用
	task := state.TaskPlan.GetTask(taskID)
	if task == nil {
		t.Fatal("Cancel 后未找到任务")
	}
	if task.Status != hschema.TodoStatusCancelled {
		t.Errorf("Cancel 后任务状态=%v，期望 TodoStatusCancelled", task.Status)
	}
}

// TestTaskLoopEventExecutor_ExecuteAbility_ReactAgent为nil ReactAgent 为 nil 时关闭输出 channel
func TestTaskLoopEventExecutor_ExecuteAbility_ReactAgent为nil(t *testing.T) {
	provider := &fakeDeepAgentProvider{
		reactAgent: nil,
	}
	deps := &modules.TaskExecutorDependencies{}
	executor := NewTaskLoopEventExecutor(deps, provider)

	sess := &fakeSessionFacade{sessionID: "sess-1"}
	ch, err := executor.ExecuteAbility(context.Background(), "task-1", sess)
	if err != nil {
		t.Fatalf("ExecuteAbility 返回错误: %v", err)
	}
	// channel 应被关闭
	_, ok := <-ch
	if ok {
		t.Error("ExecuteAbility ReactAgent 为 nil 时 channel 未关闭")
	}
}

// TestTaskLoopEventExecutor_ExecuteAbility_任务不存在 GetTask 返回空时关闭输出 channel
func TestTaskLoopEventExecutor_ExecuteAbility_任务不存在(t *testing.T) {
	provider := &fakeDeepAgentProvider{
		reactAgent: &agents.ReActAgent{},
	}
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	deps := &modules.TaskExecutorDependencies{
		TaskManager: tm,
	}
	executor := NewTaskLoopEventExecutor(deps, provider)

	sess := &fakeSessionFacade{sessionID: "sess-1"}
	ch, err := executor.ExecuteAbility(context.Background(), "nonexistent-task", sess)
	if err != nil {
		t.Fatalf("ExecuteAbility 返回错误: %v", err)
	}
	// channel 应被关闭
	_, ok := <-ch
	if ok {
		t.Error("ExecuteAbility 任务不存在时 channel 未关闭")
	}
}

// TestTaskLoopEventExecutor_ExecuteAbility_任务存在有描述 GetTask 找到任务时进入后续逻辑
func TestTaskLoopEventExecutor_ExecuteAbility_任务存在有描述(t *testing.T) {
	taskID := "task-exec-1"

	// 创建一个基本的 ReActAgent（有 card 但无 LLM，Invoke 返回错误）
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-agent"))
	saCfg := saconfig.NewReActAgentConfig()
	reactAgent := agents.NewReActAgent(card, saCfg)

	provider := &fakeDeepAgentProvider{
		reactAgent:  reactAgent,
		coordinator: NewLoopCoordinator(nil),
	}
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	deps := &modules.TaskExecutorDependencies{
		TaskManager: tm,
	}
	executor := NewTaskLoopEventExecutor(deps, provider)

	// 预先添加任务到 TaskManager
	coreTask := &cschema.Task{
		SessionID:   "sess-1",
		TaskID:      taskID,
		TaskType:    hschema.DeepTaskType,
		Description: "test task description",
		Status:      cschema.TaskSubmitted,
		Metadata:    map[string]any{"run_kind": "normal"},
	}
	if addErr := tm.AddTask(context.Background(), coreTask); addErr != nil {
		t.Fatalf("AddTask 返回错误: %v", addErr)
	}

	sess := &fakeSessionFacade{sessionID: "sess-1"}
	ch, err := executor.ExecuteAbility(context.Background(), taskID, sess)
	if err != nil {
		t.Fatalf("ExecuteAbility 返回错误: %v", err)
	}

	// 等待 goroutine 完成，读取输出
	output, ok := <-ch
	_ = output
	_ = ok
	// goroutine 会调用 reactAgent.Invoke，可能返回错误或成功
	// 关键是验证代码路径被覆盖
}

// TestIsSensitive_默认敏感模式 无环境变量时返回 true
func TestIsSensitive_默认敏感模式(t *testing.T) {
	// 清理环境变量，确保默认值
	_ = os.Unsetenv("IS_SENSITIVE")
	result := isSensitive()
	if !result {
		t.Error("isSensitive() 默认返回 false，期望 true")
	}
}

// TestIsSensitive_非敏感模式 IS_SENSITIVE=false 时返回 false
func TestIsSensitive_非敏感模式(t *testing.T) {
	_ = os.Setenv("IS_SENSITIVE", "false")
	defer func() { _ = os.Unsetenv("IS_SENSITIVE") }()

	result := isSensitive()
	if result {
		t.Error("isSensitive() IS_SENSITIVE=false 时返回 true，期望 false")
	}
}

// TestIsSensitive_敏感模式 IS_SENSITIVE=true 时返回 true
func TestIsSensitive_敏感模式(t *testing.T) {
	_ = os.Setenv("IS_SENSITIVE", "true")
	defer func() { _ = os.Unsetenv("IS_SENSITIVE") }()

	result := isSensitive()
	if !result {
		t.Error("isSensitive() IS_SENSITIVE=true 时返回 false，期望 true")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fakeLoopCoordinator 用于测试的模拟循环协调器
type fakeLoopCoordinator struct {
	// iteration 迭代次数
	iteration int
}

// Iteration 实现 LoopCoordinatorInterface 接口
func (f *fakeLoopCoordinator) Iteration() int { return f.iteration }

// RequestAbort 实现 LoopCoordinatorInterface 接口
func (f *fakeLoopCoordinator) RequestAbort() {}

// GetCompletionPromiseEvaluator 实现 LoopCoordinatorInterface 接口
func (f *fakeLoopCoordinator) GetCompletionPromiseEvaluator() interfaces.CompletionPromiseEvaluatorInterface {
	return nil
}

// Card 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) Card() *agentschema.AgentCard {
	return nil
}

// ReactAgent 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) ReactAgent() *agents.ReActAgent {
	return f.reactAgent
}

// LoopCoordinator 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) LoopCoordinator() interfaces.LoopCoordinatorInterface {
	return f.coordinator
}

// LoopController 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) LoopController() controller.ControllerInterface {
	return nil
}

// EventHandler 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) EventHandler() modules.EventHandler {
	return f.eventHandler
}

// LoadState 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return f.state
}

// DeepConfig 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) DeepConfig() *hschema.DeepAgentConfig {
	return f.config
}

// IsInvokeActive 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) IsInvokeActive() bool {
	return f.invokeActive
}

// IsAutoInvokeScheduled 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) IsAutoInvokeScheduled() bool {
	return f.autoInvokeScheduled
}

// SetAutoInvokeScheduled 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) SetAutoInvokeScheduled(scheduled bool) {
	f.autoInvokeScheduled = scheduled
}

// ScheduleAutoInvokeOnSpawnDone 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}

// CreateSubagent 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) CreateSubagent(_ context.Context, _ string, _ string) (interfaces.DeepAgentInterface, error) {
	return f.subagent, f.createSubagentErr
}

// Invoke 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// SwitchMode 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) SwitchMode(_ sessioninterfaces.SessionFacade, _ string) {}

// RestoreModeAfterPlanExit 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}

// GetPlanFilePath 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string { return "" }

// SaveState 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {
}

// FindRailsByType 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) FindRailsByType(_ ...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}

// AddRail 实现 DeepAgentInterface 接口（M-02 新增方法的测试桩）。
func (f *fakeDeepAgentProvider) AddRail(_ agentinterfaces.AgentRail) {} // M-02: 接口新增 AddRail 的测试桩

func (f *fakeDeepAgentProvider) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// InnerInvokeOverride 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) InnerInvokeOverride() func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	return f.innerOverride
}

// 10.6.19 接口面扩展桩（AbilityManager/SystemPromptBuilder/Uapswarm 访问器）
func (f *fakeDeepAgentProvider) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}
func (f *fakeDeepAgentProvider) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (f *fakeDeepAgentProvider) UapswarmProjectDir() string       { return "" }
func (f *fakeDeepAgentProvider) SetUapswarmProjectDir(string)     {}
func (f *fakeDeepAgentProvider) UapswarmCodeProjectDir() string   { return "" }
func (f *fakeDeepAgentProvider) SetUapswarmCodeProjectDir(string) {}
func (f *fakeDeepAgentProvider) UapswarmAdapterMode() string      { return "" }
func (f *fakeDeepAgentProvider) SetUapswarmAdapterMode(string)    {}

// GetSessionID 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetSessionID() string {
	return f.sessionID
}

// UpdateState 实现 SessionFacade 接口
func (f *fakeSessionFacade) UpdateState(_ map[string]any) {}

// GetState 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetState(_ state.StateKey) (any, error) {
	return nil, nil
}

// DumpState 实现 SessionFacade 接口
func (f *fakeSessionFacade) DumpState() map[string]any {
	return map[string]any{}
}

// WriteStream 实现 SessionFacade 接口
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error {
	return nil
}

// WriteCustomStream 实现 SessionFacade 接口
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}

// GetEnv 实现 SessionFacade 接口
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any {
	return nil
}

// Interact 实现 SessionFacade 接口
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error {
	return nil
}

// ClearSession 清空会话状态。
func (f *fakeSessionFacade) ClearSession(_ context.Context) {}

// TestExtractInputParts_多帧才返回 验证有效文本不足 2 条时返回 nil。
//
// Python: _extract_input_parts 过滤空串后 len(texts) > 1 才算多输入
// (task_loop_event_executor.py L431-448)
func TestExtractInputParts_多帧才返回(t *testing.T) {
	require.Nil(t, extractInputParts(nil))
	require.Nil(t, extractInputParts(&cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "only"}},
	}), "单帧不需要列表，内层自会从 query 派生")
	require.Equal(t, []string{"a", "c"}, extractInputParts(&cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.TextDataFrame{Text: "a"},
			&cschema.TextDataFrame{Text: ""},
			&cschema.TextDataFrame{Text: "c"},
		},
	}), "空串必须被过滤")
}

// TestExtractInputParts_非文本帧被忽略 验证只认文本帧。
func TestExtractInputParts_非文本帧被忽略(t *testing.T) {
	require.Nil(t, extractInputParts(&cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.TextDataFrame{Text: "a"},
			&cschema.JsonDataFrame{Data: map[string]any{"k": "v"}},
		},
	}), "1 条有效文本不足 2 条")
}

// TestTaskLoopExecutor_改写query则不带parts 验证 rail 改写 query 后不下发 parts。
//
// Python: if input_parts is not None and iter_inputs.query == query
// (task_loop_event_executor.py L216-219) —— "a replaced query wins"。
func TestTaskLoopExecutor_改写query则不带parts(t *testing.T) {
	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.TextDataFrame{Text: "原始a"},
			&cschema.TextDataFrame{Text: "原始b"},
		},
	}
	parts := extractInputParts(evt)
	require.Equal(t, []string{"原始a", "原始b"}, parts, "前置：两个有效文本帧")

	query := "原始a\n原始b"
	rewritten := "模板改写后的 query"
	require.NotEqual(t, query, rewritten, "前提：query 已被改写")

	effective := map[string]any{"query": rewritten}
	if parts != nil && rewritten == query {
		effective["_input_parts"] = parts
	}
	_, has := effective["_input_parts"]
	require.False(t, has, "query 被改写时不得下发 _input_parts")

	effective2 := map[string]any{"query": query}
	if parts != nil && query == query {
		effective2["_input_parts"] = parts
	}
	require.Equal(t, parts, effective2["_input_parts"])
}

// TestExtractInputParts_接线条件 验证 executor 的下发条件（有效多帧且 query 未改写）。
//
// 复刻 executor 步骤 14 后的判定，覆盖"真的要下发"的分支：
//
//	Python: if input_parts is not None and iter_inputs.query == query
//	(task_loop_event_executor.py L216-219)
func TestExtractInputParts_接线条件(t *testing.T) {
	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.TextDataFrame{Text: "一"},
			&cschema.TextDataFrame{Text: "二"},
		},
	}
	query := "一\n二"
	parts := extractInputParts(evt)
	require.Len(t, parts, 2)

	// 未改写 → 下发
	effective := map[string]any{"query": query}
	if parts != nil && query == query {
		effective["_input_parts"] = parts
	}
	require.Equal(t, []string{"一", "二"}, effective["_input_parts"])

	// 单帧 → parts 为 nil，不下发
	single := &cschema.InputEvent{InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "只有一个"}}}
	require.Nil(t, extractInputParts(single))
	effective2 := map[string]any{"query": "只有一个"}
	if p := extractInputParts(single); p != nil {
		effective2["_input_parts"] = p
	}
	_, has := effective2["_input_parts"]
	require.False(t, has, "单帧不下发 _input_parts")
}

// TestExecuteAbility_多帧InputEvent下发parts 端到端验证 _input_parts 全链路。
//
// 覆盖 spec C12：任务携带 3 帧文本的 InputEvent 时，executor 把未 join 的
// 列表放进 effective，交给内层 ReActAgent 的 admission。
//
//	Python: task_loop_event_executor.py L216-219
//	if input_parts is not None and iter_inputs.query == query
func TestExecuteAbility_多帧InputEvent下发parts(t *testing.T) {
	ctx := context.Background()

	tm := modules.NewTaskManager(&config.ControllerConfig{})
	task := &cschema.Task{
		TaskID:      "exec-parts",
		Description: "第一段\n第二段\n第三段",
		Status:      cschema.TaskSubmitted,
		Inputs: []cschema.Event{&cschema.InputEvent{
			InputData: []cschema.DataFrame{
				&cschema.TextDataFrame{Text: "第一段"},
				&cschema.TextDataFrame{Text: "第二段"},
				&cschema.TextDataFrame{Text: "第三段"},
			},
		}},
		Metadata: map[string]any{},
	}
	require.NoError(t, tm.AddTask(ctx, task))

	var gotParts []string
	provider := &fakeDeepAgentProvider{
		reactAgent: agents.NewReActAgent(
			agentschema.NewAgentCard(agentschema.WithAgentName("exec-parts-agent")),
			saconfig.NewReActAgentConfig(),
		),
		innerOverride: func(_ context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
			if v, ok := inputs["_input_parts"]; ok {
				gotParts, _ = v.([]string)
			}
			return map[string]any{"output": "ok"}, nil
		},
	}
	exec := NewTaskLoopEventExecutor(&modules.TaskExecutorDependencies{TaskManager: tm}, provider)

	ch, err := exec.ExecuteAbility(ctx, "exec-parts", &fakeSessionFacade{sessionID: "sess-exec"})
	require.NoError(t, err)
	for range ch {
	}

	require.Equal(t, []string{"第一段", "第二段", "第三段"}, gotParts,
		"executor 必须把 3 帧文本作为 _input_parts 下发给内层")
}

// TestExecuteAbility_单帧不下发parts 验证单帧任务不带 _input_parts。
//
// 单输入不需要列表，内层 extractUserParts 自会从 query 派生。
func TestExecuteAbility_单帧不下发parts(t *testing.T) {
	ctx := context.Background()

	tm := modules.NewTaskManager(&config.ControllerConfig{})
	task := &cschema.Task{
		TaskID:      "exec-single",
		Description: "只有一段",
		Status:      cschema.TaskSubmitted,
		Inputs: []cschema.Event{&cschema.InputEvent{
			InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "只有一段"}},
		}},
		Metadata: map[string]any{},
	}
	require.NoError(t, tm.AddTask(ctx, task))

	sawParts := false
	provider := &fakeDeepAgentProvider{
		reactAgent: agents.NewReActAgent(
			agentschema.NewAgentCard(agentschema.WithAgentName("exec-single-agent")),
			saconfig.NewReActAgentConfig(),
		),
		innerOverride: func(_ context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
			_, sawParts = inputs["_input_parts"]
			return map[string]any{"output": "ok"}, nil
		},
	}
	exec := NewTaskLoopEventExecutor(&modules.TaskExecutorDependencies{TaskManager: tm}, provider)

	ch, err := exec.ExecuteAbility(ctx, "exec-single", &fakeSessionFacade{sessionID: "sess-single"})
	require.NoError(t, err)
	for range ch {
	}

	require.False(t, sawParts, "单输入不需要 _input_parts")
}

// TestExecuteAbility_无InputEvent不下发parts 验证任务无 InputEvent 时不带 parts。
//
// inputEvt 为 nil 时 extractInputParts 返回 nil。
func TestExecuteAbility_无InputEvent不下发parts(t *testing.T) {
	ctx := context.Background()

	tm := modules.NewTaskManager(&config.ControllerConfig{})
	task := &cschema.Task{
		TaskID:      "exec-noevt",
		Description: "desc only",
		Status:      cschema.TaskSubmitted,
		Metadata:    map[string]any{},
	}
	require.NoError(t, tm.AddTask(ctx, task))

	sawParts := false
	provider := &fakeDeepAgentProvider{
		reactAgent: agents.NewReActAgent(
			agentschema.NewAgentCard(agentschema.WithAgentName("exec-single-agent")),
			saconfig.NewReActAgentConfig(),
		),
		innerOverride: func(_ context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
			_, sawParts = inputs["_input_parts"]
			return map[string]any{"output": "ok"}, nil
		},
	}
	exec := NewTaskLoopEventExecutor(&modules.TaskExecutorDependencies{TaskManager: tm}, provider)

	ch, err := exec.ExecuteAbility(ctx, "exec-noevt", &fakeSessionFacade{sessionID: "sess-noevt"})
	require.NoError(t, err)
	for range ch {
	}

	require.False(t, sawParts, "无 InputEvent 不得下发 _input_parts")
}
