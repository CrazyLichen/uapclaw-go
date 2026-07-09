package subagent

import (
	"context"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDeepAgentProvider 测试用 DeepAgentInterface mock
type fakeDeepAgentProvider struct {
	// reactAgent 预设的 ReActAgent
	reactAgent *agents.ReActAgent
	// loopController 预设的 LoopController
	loopController controller.ControllerInterface
	// eventHandler 预设的事件处理器
	eventHandler modules.EventHandler
	// state 预设的 DeepAgentState
	state *hschema.DeepAgentState
	// deepConfig 预设的 DeepAgentConfig
	deepConfig *hschema.DeepAgentConfig
	// invokeActive 预设的 invoke 活跃标记
	invokeActive bool
	// autoInvokeScheduled 预设的自动 invoke 调度标记
	autoInvokeScheduled bool
	// subagent 预设的子 Agent
	subagent interfaces.DeepAgentInterface
	// createSubagentErr 预设的 CreateSubagent 错误
	createSubagentErr error
}

// fakeLoopCoordinator 用于测试的模拟循环协调器
type fakeLoopCoordinator struct {
	// iteration 迭代次数
	iteration int
}

// fakeController 测试用 ControllerInterface mock
type fakeController struct {
	// taskManager 预设的 TaskManager
	taskManager *modules.TaskManager
	// taskScheduler 预设的 TaskScheduler
	taskScheduler *modules.TaskScheduler
}

// fakeHandlerSess 用于测试的模拟会话门面
type fakeHandlerSess struct {
	// sessionID 会话标识
	sessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSessionToolkit 测试创建 SessionToolkit
func TestNewSessionToolkit(t *testing.T) {
	tk := NewSessionToolkit()
	if tk == nil {
		t.Fatal("NewSessionToolkit 返回 nil")
	}
	if len(tk.ListAll()) != 0 {
		t.Fatal("新创建的 SessionToolkit 应为空")
	}
}

// TestSessionToolkit_UpsertRunning 测试插入运行任务
func TestSessionToolkit_UpsertRunning(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	row := tk.Get("task-1")
	if row == nil {
		t.Fatal("应找到 task-1")
	}
	if row.Status != "running" {
		t.Fatalf("期望 running, 实际 %s", row.Status)
	}
	if row.SubSessionID != "sub-1" || row.Description != "研究A方向" {
		t.Fatalf("字段不匹配: %+v", row)
	}
}

// TestSessionToolkit_MarkCompleted 测试标记完成
func TestSessionToolkit_MarkCompleted(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tk.MarkCompleted("task-1", "研究结果")
	row := tk.Get("task-1")
	if row.Status != "completed" {
		t.Fatalf("期望 completed, 实际 %s", row.Status)
	}
	if row.Result != "研究结果" {
		t.Fatalf("期望 研究结果, 实际 %s", row.Result)
	}
}

// TestSessionToolkit_MarkFailed 测试标记失败
func TestSessionToolkit_MarkFailed(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tk.MarkFailed("task-1", "网络错误")
	row := tk.Get("task-1")
	if row.Status != "error" {
		t.Fatalf("期望 error, 实际 %s", row.Status)
	}
	if row.Error != "网络错误" {
		t.Fatalf("期望 网络错误, 实际 %s", row.Error)
	}
}

// TestSessionToolkit_MarkCanceled 测试标记取消
func TestSessionToolkit_MarkCanceled(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tk.MarkCanceled("task-1")
	row := tk.Get("task-1")
	if row.Status != "canceled" {
		t.Fatalf("期望 canceled, 实际 %s", row.Status)
	}
}

// TestSessionToolkit_MarkCompleted_不存在的任务 测试标记不存在任务无副作用
func TestSessionToolkit_MarkCompleted_不存在的任务(t *testing.T) {
	tk := NewSessionToolkit()
	tk.MarkCompleted("nonexistent", "result")
	if row := tk.Get("nonexistent"); row != nil {
		t.Fatal("不应创建不存在的任务行")
	}
}

// TestSessionToolkit_ListAll 测试列出所有任务
func TestSessionToolkit_ListAll(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "任务1")
	tk.UpsertRunning("task-2", "sub-2", "任务2")
	all := tk.ListAll()
	if len(all) != 2 {
		t.Fatalf("期望 2, 实际 %d", len(all))
	}
}

// TestSessionToolkit_Clear 测试清空
func TestSessionToolkit_Clear(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "任务1")
	tk.Clear()
	if len(tk.ListAll()) != 0 {
		t.Fatal("清空后应为空")
	}
}

// TestSessionToolkit_UpsertRunning_覆盖 测试重复 upsert 覆盖
func TestSessionToolkit_UpsertRunning_覆盖(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "旧描述")
	tk.UpsertRunning("task-1", "sub-2", "新描述")
	row := tk.Get("task-1")
	if row.Description != "新描述" {
		t.Fatalf("期望 新描述, 实际 %s", row.Description)
	}
	if row.SubSessionID != "sub-2" {
		t.Fatalf("期望 sub-2, 实际 %s", row.SubSessionID)
	}
	if row.Status != "running" {
		t.Fatalf("期望 running, 实际 %s", row.Status)
	}
}

// TestSessionSpawnTaskTypeConstant 常量值正确
func TestSessionSpawnTaskTypeConstant(t *testing.T) {
	if hschema.SessionSpawnTaskType != "session_spawn_task" {
		t.Fatalf("期望 session_spawn_task, 实际 %s", hschema.SessionSpawnTaskType)
	}
}

// TestSessionsListTool_Invoke_空列表 toolkit 为空时返回默认消息
func TestSessionsListTool_Invoke_空列表(t *testing.T) {
	tk := NewSessionToolkit()
	tl := NewSessionsListTool(tk, "cn", "")
	result, err := tl.Invoke(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["success"] != true {
		t.Error("期望 success=true")
	}
	data, _ := result["data"].(string)
	if data != "当前会话没有后台子任务" {
		t.Errorf("期望 '当前会话没有后台子任务', 实际 %q", data)
	}
}

// TestSessionsListTool_Invoke_有任务 toolkit 中有任务时返回任务列表
func TestSessionsListTool_Invoke_有任务(t *testing.T) {
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "研究A方向")
	tl := NewSessionsListTool(tk, "cn", "")
	result, err := tl.Invoke(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["success"] != true {
		t.Error("期望 success=true")
	}
	data, _ := result["data"].(string)
	if data == "" || data == "当前会话没有后台子任务" {
		t.Errorf("期望包含任务信息, 实际 %q", data)
	}
}

// TestSessionsListTool_Invoke_英文 语言为 en 时返回英文消息
func TestSessionsListTool_Invoke_英文(t *testing.T) {
	tk := NewSessionToolkit()
	tl := NewSessionsListTool(tk, "en", "")
	result, err := tl.Invoke(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	data, _ := result["data"].(string)
	if data != "No background tasks for this session" {
		t.Errorf("期望英文消息, 实际 %q", data)
	}
}

// TestSessionsListTool_Card 卡片名称正确
func TestSessionsListTool_Card(t *testing.T) {
	tk := NewSessionToolkit()
	tl := NewSessionsListTool(tk, "cn", "")
	if tl.Card().Name != "sessions_list" {
		t.Errorf("期望 sessions_list, 实际 %s", tl.Card().Name)
	}
}

// TestSessionsSpawnTool_Card 卡片名称正确
func TestSessionsSpawnTool_Card(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	if tl.Card().Name != "sessions_spawn" {
		t.Errorf("期望 sessions_spawn, 实际 %s", tl.Card().Name)
	}
}

// TestSessionsSpawnTool_Invoke_未启用TaskLoop enable_task_loop 为 false 时返回错误
func TestSessionsSpawnTool_Invoke_未启用TaskLoop(t *testing.T) {
	provider := &fakeDeepAgentProvider{
		deepConfig: &hschema.DeepAgentConfig{EnableTaskLoop: false},
	}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	_, err := tl.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsSpawnTool_Invoke_LoopController为nil loop_controller 为 nil 时返回错误
func TestSessionsSpawnTool_Invoke_LoopController为nil(t *testing.T) {
	provider := &fakeDeepAgentProvider{
		deepConfig:     &hschema.DeepAgentConfig{EnableTaskLoop: true},
		loopController: nil,
	}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	_, err := tl.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsCancelTool_Card 卡片名称正确
func TestSessionsCancelTool_Card(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tl := NewSessionsCancelTool(provider, tk, "cn", "")
	if tl.Card().Name != "sessions_cancel" {
		t.Errorf("期望 sessions_cancel, 实际 %s", tl.Card().Name)
	}
}

// TestSessionsCancelTool_Invoke_缺少TaskID task_id 为空时返回错误
func TestSessionsCancelTool_Invoke_缺少TaskID(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tl := NewSessionsCancelTool(provider, tk, "cn", "")
	_, err := tl.Invoke(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsCancelTool_Invoke_任务不存在 toolkit 中无该任务时返回错误
func TestSessionsCancelTool_Invoke_任务不存在(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tl := NewSessionsCancelTool(provider, tk, "cn", "")
	_, err := tl.Invoke(context.Background(), map[string]any{"task_id": "nonexistent"}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestBuildSessionTools 构建三个工具
func TestBuildSessionTools(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tools := BuildSessionTools(provider, tk, "cn", "", "")
	if len(tools) != 3 {
		t.Fatalf("期望 3 个工具, 实际 %d", len(tools))
	}
	if tools[0].Card().Name != "sessions_list" {
		t.Errorf("第 0 个工具期望 sessions_list, 实际 %s", tools[0].Card().Name)
	}
	if tools[1].Card().Name != "sessions_spawn" {
		t.Errorf("第 1 个工具期望 sessions_spawn, 实际 %s", tools[1].Card().Name)
	}
	if tools[2].Card().Name != "sessions_cancel" {
		t.Errorf("第 2 个工具期望 sessions_cancel, 实际 %s", tools[2].Card().Name)
	}
}

// TestGenerateTokenHex 生成长度正确
func TestGenerateTokenHex(t *testing.T) {
	token := generateTokenHex(4)
	// 4 字节 = 8 十六进制字符
	if len(token) != 8 {
		t.Fatalf("期望 8 字符, 实际 %d", len(token))
	}
}

// TestSessionsListTool_Stream 返回 Stream 不支持错误
func TestSessionsListTool_Stream(t *testing.T) {
	tk := NewSessionToolkit()
	tl := NewSessionsListTool(tk, "cn", "")
	_, err := tl.Stream(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回 Stream 不支持错误")
	}
}

// TestSessionsSpawnTool_Stream 返回 Stream 不支持错误
func TestSessionsSpawnTool_Stream(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	_, err := tl.Stream(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回 Stream 不支持错误")
	}
}

// TestSessionsCancelTool_Stream 返回 Stream 不支持错误
func TestSessionsCancelTool_Stream(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	tk := NewSessionToolkit()
	tl := NewSessionsCancelTool(provider, tk, "cn", "")
	_, err := tl.Stream(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回 Stream 不支持错误")
	}
}

// TestSessionsSpawnTool_Invoke_TaskManager为nil TaskManager 为 nil 时返回错误
func TestSessionsSpawnTool_Invoke_TaskManager为nil(t *testing.T) {
	// 构造 LoopController 但 TaskManager 为 nil
	ctrl := &fakeController{}
	provider := &fakeDeepAgentProvider{
		deepConfig:     &hschema.DeepAgentConfig{EnableTaskLoop: true},
		loopController: ctrl,
	}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	_, err := tl.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsSpawnTool_Invoke_成功 提交任务成功时返回 pending 状态
func TestSessionsSpawnTool_Invoke_成功(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	ctrl := &fakeController{taskManager: tm}
	provider := &fakeDeepAgentProvider{
		deepConfig:     &hschema.DeepAgentConfig{EnableTaskLoop: true},
		loopController: ctrl,
	}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	result, err := tl.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, tool.WithToolSession(&fakeHandlerSess{sessionID: "test-session"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["success"] != true {
		t.Error("期望 success=true")
	}
	data, _ := result["data"].(map[string]any)
	if data["status"] != "pending" {
		t.Errorf("期望 pending, 实际 %v", data["status"])
	}
	// 验证 toolkit 中有任务
	all := tk.ListAll()
	if len(all) != 1 {
		t.Fatalf("期望 1 个任务, 实际 %d", len(all))
	}
}

// TestSessionsSpawnTool_Invoke_英文语言 英文语言时返回英文消息
func TestSessionsSpawnTool_Invoke_英文语言(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	ctrl := &fakeController{taskManager: tm}
	provider := &fakeDeepAgentProvider{
		deepConfig:     &hschema.DeepAgentConfig{EnableTaskLoop: true},
		loopController: ctrl,
	}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "en", "", "")
	result, err := tl.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "test task",
	}, tool.WithToolSession(&fakeHandlerSess{sessionID: "test-session"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	data, _ := result["data"].(map[string]any)
	msg, _ := data["message"].(string)
	if msg == "" {
		t.Error("期望有英文消息")
	}
}

// TestSessionsSpawnTool_Invoke_带Session 传入 Session 时使用其 sessionID
func TestSessionsSpawnTool_Invoke_带Session(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	ctrl := &fakeController{taskManager: tm}
	provider := &fakeDeepAgentProvider{
		deepConfig:     &hschema.DeepAgentConfig{EnableTaskLoop: true},
		loopController: ctrl,
	}
	tk := NewSessionToolkit()
	tl := NewSessionsSpawnTool(provider, tk, "cn", "", "")
	result, err := tl.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, tool.WithToolSession(&fakeHandlerSess{sessionID: "parent-sess"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["success"] != true {
		t.Error("期望 success=true")
	}
}

// TestSessionsCancelTool_Invoke_Scheduler为nil TaskScheduler 为 nil 时返回错误
func TestSessionsCancelTool_Invoke_Scheduler为nil(t *testing.T) {
	ctrl := &fakeController{}
	provider := &fakeDeepAgentProvider{
		loopController: ctrl,
	}
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "测试任务")
	tl := NewSessionsCancelTool(provider, tk, "cn", "")
	_, err := tl.Invoke(context.Background(), map[string]any{"task_id": "task-1"}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestJoinLines 多行连接
func TestJoinLines(t *testing.T) {
	result := joinLines([]string{"a", "b", "c"})
	if result != "a\nb\nc" {
		t.Errorf("期望 'a\\nb\\nc', 实际 %q", result)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Card 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) Card() *agentschema.AgentCard {
	return nil
}

// ReactAgent 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) ReactAgent() *agents.ReActAgent { return f.reactAgent }

// LoopCoordinator 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) LoopCoordinator() interfaces.LoopCoordinatorInterface {
	return &fakeLoopCoordinator{}
}

// LoopController 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) LoopController() controller.ControllerInterface {
	return f.loopController
}

// EventHandler 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) EventHandler() modules.EventHandler { return f.eventHandler }

// LoadState 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return f.state
}

// DeepConfig 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }

// IsInvokeActive 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) IsInvokeActive() bool { return f.invokeActive }

// IsAutoInvokeScheduled 实现 DeepAgentInterface 接口
func (f *fakeDeepAgentProvider) IsAutoInvokeScheduled() bool { return f.autoInvokeScheduled }

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

// Iteration 实现 LoopCoordinatorInterface 接口
func (f *fakeLoopCoordinator) Iteration() int { return f.iteration }

// RequestAbort 实现 LoopCoordinatorInterface 接口
func (f *fakeLoopCoordinator) RequestAbort() {}

// GetCompletionPromiseEvaluator 实现 LoopCoordinatorInterface 接口
func (f *fakeLoopCoordinator) GetCompletionPromiseEvaluator() interfaces.CompletionPromiseEvaluatorInterface {
	return nil
}

// TaskManager 实现 ControllerInterface 接口
func (f *fakeController) TaskManager() *modules.TaskManager { return f.taskManager }

// TaskScheduler 实现 ControllerInterface 接口
func (f *fakeController) TaskScheduler() *modules.TaskScheduler { return f.taskScheduler }

// Init 实现 ControllerInterface 接口
func (f *fakeController) Init(_ *agentschema.AgentCard, _ *config.ControllerConfig, _ agentinterfaces.AbilityManagerInterface, _ iface.ContextEngine) {
}

// Start 实现 ControllerInterface 接口
func (f *fakeController) Start(_ context.Context) error { return nil }

// Stop 实现 ControllerInterface 接口
func (f *fakeController) Stop(_ context.Context) error { return nil }

// Invoke 实现 ControllerInterface 接口
func (f *fakeController) Invoke(_ context.Context, _ *cschema.InputEvent, _ *session.Session) (*cschema.ControllerOutput, error) {
	return nil, nil
}

// Stream 实现 ControllerInterface 接口
func (f *fakeController) Stream(_ context.Context, _ *cschema.InputEvent, _ *session.Session, _ []stream.StreamMode) (<-chan *stream.OutputSchema, <-chan error) {
	return nil, nil
}

// PublishEventAsync 实现 ControllerInterface 接口
func (f *fakeController) PublishEventAsync(_ context.Context, _ *session.Session, _ cschema.Event) error {
	return nil
}

// SetEventHandler 实现 ControllerInterface 接口
func (f *fakeController) SetEventHandler(_ modules.EventHandler) {}

// AddTaskExecutor 实现 ControllerInterface 接口
func (f *fakeController) AddTaskExecutor(_ string, _ func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor) controller.ControllerInterface {
	return f
}

// BindSession 实现 ControllerInterface 接口
func (f *fakeController) BindSession(_ context.Context, _ *session.Session) error { return nil }

// UnbindSession 实现 ControllerInterface 接口
func (f *fakeController) UnbindSession(_ context.Context, _ *session.Session) error { return nil }

// Config 实现 ControllerInterface 接口
func (f *fakeController) Config() *config.ControllerConfig { return nil }

// EventHandler 实现 ControllerInterface 接口
func (f *fakeController) EventHandler() modules.EventHandler { return nil }

// GetSessionID 实现 SessionFacade 接口
func (f *fakeHandlerSess) GetSessionID() string {
	return f.sessionID
}

// UpdateState 实现 SessionFacade 接口
func (f *fakeHandlerSess) UpdateState(_ map[string]any) {}

// GetState 实现 SessionFacade 接口
func (f *fakeHandlerSess) GetState(_ state.StateKey) (any, error) {
	return nil, nil
}

// DumpState 实现 SessionFacade 接口
func (f *fakeHandlerSess) DumpState() map[string]any {
	return map[string]any{}
}

// WriteStream 实现 SessionFacade 接口
func (f *fakeHandlerSess) WriteStream(_ context.Context, _ any) error {
	return nil
}

// WriteCustomStream 实现 SessionFacade 接口
func (f *fakeHandlerSess) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}

// GetEnv 实现 SessionFacade 接口
func (f *fakeHandlerSess) GetEnv(_ string, _ ...any) any {
	return nil
}

// Interact 实现 SessionFacade 接口
func (f *fakeHandlerSess) Interact(_ context.Context, _ any) error {
	return nil
}

// 编译时接口检查
var _ interfaces.DeepAgentInterface = (*fakeDeepAgentProvider)(nil)
var _ controller.ControllerInterface = (*fakeController)(nil)
