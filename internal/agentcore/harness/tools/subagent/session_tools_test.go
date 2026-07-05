package subagent

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeProvider 测试用 SessionToolProvider mock
type fakeProvider struct {
	// eventHandler 预设的事件处理器
	eventHandler modules.EventHandler
	// deepConfig 预设的 DeepAgentConfig
	deepConfig *hschema.DeepAgentConfig
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

// Testhschema.SessionSpawnTaskType 常量值正确
func Testhschema.SessionSpawnTaskType(t *testing.T) {
	if hschema.SessionSpawnTaskType != "session_spawn_task" {
		t.Fatalf("期望 session_spawn_task, 实际 %s", hschema.SessionSpawnTaskType)
	}
}

// TestSessionsListTool_Invoke_空列表 toolkit 为空时返回默认消息
func TestSessionsListTool_Invoke_空列表(t *testing.T) {
	tk := NewSessionToolkit()
	tool := NewSessionsListTool(tk, "cn")
	result, err := tool.Invoke(context.Background(), map[string]any{}, nil)
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
	tool := NewSessionsListTool(tk, "cn")
	result, err := tool.Invoke(context.Background(), map[string]any{}, nil)
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
	tool := NewSessionsListTool(tk, "en")
	result, err := tool.Invoke(context.Background(), map[string]any{}, nil)
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
	tool := NewSessionsListTool(tk, "cn")
	if tool.Card().Name != "sessions_list" {
		t.Errorf("期望 sessions_list, 实际 %s", tool.Card().Name)
	}
}

// TestSessionsSpawnTool_Card 卡片名称正确
func TestSessionsSpawnTool_Card(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	if tool.Card().Name != "sessions_spawn" {
		t.Errorf("期望 sessions_spawn, 实际 %s", tool.Card().Name)
	}
}

// TestSessionsSpawnTool_Invoke_未启用TaskLoop enable_task_loop 为 false 时返回错误
func TestSessionsSpawnTool_Invoke_未启用TaskLoop(t *testing.T) {
	provider := &fakeProvider{
		deepConfig: &hschema.DeepAgentConfig{EnableTaskLoop: false},
	}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	_, err := tool.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsSpawnTool_Invoke_EventHandler为nil event_handler 为 nil 时返回错误
func TestSessionsSpawnTool_Invoke_EventHandler为nil(t *testing.T) {
	provider := &fakeProvider{
		deepConfig:    &hschema.DeepAgentConfig{EnableTaskLoop: true},
		eventHandler: nil,
	}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	_, err := tool.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsCancelTool_Card 卡片名称正确
func TestSessionsCancelTool_Card(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tool := NewSessionsCancelTool(provider, tk, "cn")
	if tool.Card().Name != "sessions_cancel" {
		t.Errorf("期望 sessions_cancel, 实际 %s", tool.Card().Name)
	}
}

// TestSessionsCancelTool_Invoke_缺少TaskID task_id 为空时返回错误
func TestSessionsCancelTool_Invoke_缺少TaskID(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tool := NewSessionsCancelTool(provider, tk, "cn")
	_, err := tool.Invoke(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestSessionsCancelTool_Invoke_任务不存在 toolkit 中无该任务时返回错误
func TestSessionsCancelTool_Invoke_任务不存在(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tool := NewSessionsCancelTool(provider, tk, "cn")
	_, err := tool.Invoke(context.Background(), map[string]any{"task_id": "nonexistent"}, nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

// TestBuildSessionTools 构建三个工具
func TestBuildSessionTools(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tools := BuildSessionTools(provider, tk, "cn", "")
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

// TestBuildSessionsListInputParams 参数列表为空
func TestBuildSessionsListInputParams(t *testing.T) {
	params := buildSessionsListInputParams()
	if len(params) != 0 {
		t.Fatalf("期望 0 个参数, 实际 %d", len(params))
	}
}

// TestBuildSessionsSpawnInputParams 两个必需参数
func TestBuildSessionsSpawnInputParams(t *testing.T) {
	params := buildSessionsSpawnInputParams()
	if len(params) != 2 {
		t.Fatalf("期望 2 个参数, 实际 %d", len(params))
	}
	if params[0].Name != "subagent_type" {
		t.Errorf("第 0 个参数期望 subagent_type, 实际 %s", params[0].Name)
	}
	if params[1].Name != "task_description" {
		t.Errorf("第 1 个参数期望 task_description, 实际 %s", params[1].Name)
	}
}

// TestBuildSessionsCancelInputParams 一个必需参数
func TestBuildSessionsCancelInputParams(t *testing.T) {
	params := buildSessionsCancelInputParams()
	if len(params) != 1 {
		t.Fatalf("期望 1 个参数, 实际 %d", len(params))
	}
	if params[0].Name != "task_id" {
		t.Errorf("第 0 个参数期望 task_id, 实际 %s", params[0].Name)
	}
}

// TestSessionsListTool_Stream 返回 Stream 不支持错误
func TestSessionsListTool_Stream(t *testing.T) {
	tk := NewSessionToolkit()
	tool := NewSessionsListTool(tk, "cn")
	_, err := tool.Stream(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回 Stream 不支持错误")
	}
}

// TestSessionsSpawnTool_Stream 返回 Stream 不支持错误
func TestSessionsSpawnTool_Stream(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	_, err := tool.Stream(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回 Stream 不支持错误")
	}
}

// TestSessionsCancelTool_Stream 返回 Stream 不支持错误
func TestSessionsCancelTool_Stream(t *testing.T) {
	provider := &fakeProvider{}
	tk := NewSessionToolkit()
	tool := NewSessionsCancelTool(provider, tk, "cn")
	_, err := tool.Stream(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("期望返回 Stream 不支持错误")
	}
}

// TestSessionsSpawnTool_Invoke_TaskManager为nil TaskManager 为 nil 时返回错误
func TestSessionsSpawnTool_Invoke_TaskManager为nil(t *testing.T) {
	// 构造 EventHandler 但 base.TaskManager 为 nil
	handler := &fakeEventHandler{}
	provider := &fakeProvider{
		deepConfig:    &hschema.DeepAgentConfig{EnableTaskLoop: true},
		eventHandler: handler,
	}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	_, err := tool.Invoke(context.Background(), map[string]any{
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
	handler := &fakeEventHandler{taskManager: tm}
	provider := &fakeProvider{
		deepConfig:    &hschema.DeepAgentConfig{EnableTaskLoop: true},
		eventHandler: handler,
	}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	result, err := tool.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["success"] != true {
		t.Error("期望 success=true")
	}
	if result["status"] != "pending" {
		t.Errorf("期望 pending, 实际 %v", result["status"])
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
	handler := &fakeEventHandler{taskManager: tm}
	provider := &fakeProvider{
		deepConfig:    &hschema.DeepAgentConfig{EnableTaskLoop: true},
		eventHandler: handler,
	}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "en", "")
	result, err := tool.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "test task",
	}, nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	msg, _ := result["message"].(string)
	if msg == "" {
		t.Error("期望有英文消息")
	}
}

// TestSessionsSpawnTool_Invoke_带Session 传入 Session 时使用其 sessionID
func TestSessionsSpawnTool_Invoke_带Session(t *testing.T) {
	cfg := config.DefaultControllerConfig()
	tm := modules.NewTaskManager(cfg)
	handler := &fakeEventHandler{taskManager: tm}
	provider := &fakeProvider{
		deepConfig:    &hschema.DeepAgentConfig{EnableTaskLoop: true},
		eventHandler: handler,
	}
	tk := NewSessionToolkit()
	tool := NewSessionsSpawnTool(provider, tk, "cn", "")
	result, err := tool.Invoke(context.Background(), map[string]any{
		"subagent_type":    "general-purpose",
		"task_description": "测试任务",
	})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["success"] != true {
		t.Error("期望 success=true")
	}
}

// TestSessionsCancelTool_Invoke_Scheduler为nil TaskScheduler 为 nil 时返回错误
func TestSessionsCancelTool_Invoke_Scheduler为nil(t *testing.T) {
	handler := &fakeEventHandler{}
	provider := &fakeProvider{
		eventHandler: handler,
	}
	tk := NewSessionToolkit()
	tk.UpsertRunning("task-1", "sub-1", "测试任务")
	tool := NewSessionsCancelTool(provider, tk, "cn")
	_, err := tool.Invoke(context.Background(), map[string]any{"task_id": "task-1"}, nil)
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

// DeepConfig 实现 SessionToolProvider 接口
func (f *fakeProvider) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }

// EventHandler 实现 SessionToolProvider 接口
func (f *fakeProvider) EventHandler() modules.EventHandler { return f.eventHandler }

// fakeEventHandler 测试用 EventHandler mock
type fakeEventHandler struct {
	// taskManager 预设的 TaskManager
	taskManager *modules.TaskManager
	// taskScheduler 预设的 TaskScheduler
	taskScheduler *modules.TaskScheduler
}

// GetBase 实现 modules.EventHandler 接口
func (f *fakeEventHandler) GetBase() *modules.EventHandlerBase {
	return &modules.EventHandlerBase{
		TaskManager:   f.taskManager,
		TaskScheduler: f.taskScheduler,
	}
}

// HandleInput 实现 modules.EventHandler 接口
func (f *fakeEventHandler) HandleInput(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}

// HandleTaskInteraction 实现 modules.EventHandler 接口
func (f *fakeEventHandler) HandleTaskInteraction(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}

// HandleTaskCompletion 实现 modules.EventHandler 接口
func (f *fakeEventHandler) HandleTaskCompletion(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}

// HandleTaskFailed 实现 modules.EventHandler 接口
func (f *fakeEventHandler) HandleTaskFailed(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}

// HandleFollowUp 实现 modules.EventHandler 接口
func (f *fakeEventHandler) HandleFollowUp(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return nil, nil
}

// PrepareRound 实现 modules.EventHandler 接口
func (f *fakeEventHandler) PrepareRound() int { return 0 }

// WaitCompletion 实现 modules.EventHandler 接口
func (f *fakeEventHandler) WaitCompletion(_ context.Context, _ time.Duration) map[string]any {
	return nil
}

// OnAbort 实现 modules.EventHandler 接口
func (f *fakeEventHandler) OnAbort() {}
