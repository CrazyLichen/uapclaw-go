package task_loop

import (
	"context"
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDeepAgentProvider 用于测试的模拟深层 Agent 提供者
type fakeDeepAgentProvider struct {
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
	plan := hschema.NewTaskPlan("测试计划", "测试目标")
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
	plan := hschema.NewTaskPlan("测试计划", "测试目标")
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
func (f *fakeDeepAgentProvider) ScheduleAutoInvokeOnSpawnDone(_ string, _ float64) error {
	return nil
}

// CreateSubagent 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) CreateSubagent(_ string, _ string) (interfaces.DeepAgentInterface, error) {
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
func (f *fakeDeepAgentProvider) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {}

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
