//go:build integration

package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepSearchE2ESuite Controller DeepSearch 三阶段管道 E2E 测试套件
//
// 对齐 Python: test_deepsearch.py
// 测试 Controller 核心管道：TaskManager + EventHandler + TaskScheduler + EventQueue
type DeepSearchE2ESuite struct {
	isuite.BaseIntegrationSuite
	// cfg 控制器配置
	cfg *config.ControllerConfig
	// taskManager 任务管理器
	taskManager *modules.TaskManager
	// eventQueue 事件队列
	eventQueue *modules.EventQueue
	// taskScheduler 任务调度器
	taskScheduler *modules.TaskScheduler
	// sessionID 测试用 session ID
	sessionID string
}

// deepSearchEventHandler DeepSearch 三阶段事件处理器
//
// 对齐 Python: DeepSearchEventHandler
// 注意：GetTask 返回深拷贝，修改返回值不影响内部存储，
// 因此激活下一阶段任务时必须通过 UpdateTaskStatus 持久化变更。
type deepSearchEventHandler struct {
	modules.EventHandlerBase
	// round 当前轮次
	round int
	// handleInputCalled HandleInput 调用次数
	handleInputCalled int
	// handleCompletionCalled HandleTaskCompletion 调用次数
	handleCompletionCalled int
	// mu 保护计数器
	mu sync.Mutex
	// outputChunks 输出分片
	outputChunks []string
}

// mockTaskExecutor 测试用 TaskExecutor 实现
type mockTaskExecutor struct {
	name string
}

// mockSessionFacade 测试用 SessionFacade 实现
type mockSessionFacade struct {
	sessionID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskTypeDataCollect 数据收集任务类型
	taskTypeDataCollect = "data_collect"
	// taskTypeDataAnalysis 数据分析任务类型
	taskTypeDataAnalysis = "data_analysis"
	// taskTypeReportGenerate 报告生成任务类型
	taskTypeReportGenerate = "report_generate"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeepSearchE2E 运行 Controller DeepSearch E2E 测试套件
func TestDeepSearchE2E(t *testing.T) {
	suite.Run(t, new(DeepSearchE2ESuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试前初始化核心组件
func (s *DeepSearchE2ESuite) SetupTest() {
	s.cfg = config.DefaultControllerConfig()
	s.cfg.EventQueueSize = 100
	s.cfg.EnableTaskPersistence = false

	s.sessionID = fmt.Sprintf("test_session_%d", time.Now().UnixNano())
	s.taskManager = modules.NewTaskManager(s.cfg)
	s.eventQueue = modules.NewEventQueue(s.cfg)
	s.taskScheduler = modules.NewTaskScheduler(
		s.cfg,
		s.taskManager,
		nil, // contextEngine — 简化测试不使用
		nil, // abilityMgr — 简化测试不使用
		s.eventQueue,
		nil, // card — 简化测试不使用
	)

	// 接线：TaskManager 的 onTaskSubmitted 回调 → TaskScheduler.NotifyTaskSubmitted
	s.taskManager.SetOnTaskSubmitted(s.taskScheduler.NotifyTaskSubmitted)
}

// TearDownTest 每个测试后停止组件
func (s *DeepSearchE2ESuite) TearDownTest() {
	if s.eventQueue != nil {
		_ = s.eventQueue.Stop(s.Ctx)
	}
}

// ─── mockSessionFacade 实现 sessioninterfaces.SessionFacade ───

func (m *mockSessionFacade) GetSessionID() string         { return m.sessionID }
func (m *mockSessionFacade) UpdateState(_ map[string]any) {}
func (m *mockSessionFacade) GetState(_ state.StateKey) (any, error) {
	return nil, nil
}
func (m *mockSessionFacade) DumpState() map[string]any                  { return nil }
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any           { return nil }
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error { return nil }
func (m *mockSessionFacade) ClearSession(_ context.Context)          {}

// 编译期接口合规检查
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// ─── mockTaskExecutor 实现 modules.TaskExecutor ───

func (e *mockTaskExecutor) ExecuteAbility(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (<-chan *stream.OutputSchema, error) {
	ch := make(chan *stream.OutputSchema, 1)
	ch <- &stream.OutputSchema{
		Type:    "text",
		Index:   0,
		Payload: fmt.Sprintf("%s output", e.name),
	}
	close(ch)
	return ch, nil
}
func (e *mockTaskExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "", nil
}
func (e *mockTaskExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}
func (e *mockTaskExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}
func (e *mockTaskExecutor) Cancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return true, nil
}

// ─── deepSearchEventHandler 实现 modules.EventHandler ───

func (h *deepSearchEventHandler) HandleInput(_ context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	h.mu.Lock()
	h.handleInputCalled++
	h.mu.Unlock()

	// 获取 sessionID
	sessionID := ""
	if input.Session != nil {
		sessionID = input.Session.GetSessionID()
	}

	// 阶段 1：数据收集（优先级 1，立即执行）
	dcTask := schema.NewTask(sessionID, taskTypeDataCollect)
	dcTask.Priority = 1
	dcTask.Status = schema.TaskSubmitted
	dcTask.Metadata = map[string]any{"topic": "芯片", "type": "arxiv"}

	// 阶段 2：数据分析（优先级 2，等待数据收集完成）
	daTask := schema.NewTask(sessionID, taskTypeDataAnalysis)
	daTask.Priority = 2
	daTask.Status = schema.TaskWaiting
	daTask.Metadata = map[string]any{"type": "trend_analysis"}

	// 阶段 3：报告生成（优先级 3，等待数据分析完成）
	rgTask := schema.NewTask(sessionID, taskTypeReportGenerate)
	rgTask.Priority = 3
	rgTask.Status = schema.TaskWaiting
	rgTask.Metadata = map[string]any{"format": "markdown", "type": "research_report"}

	// 逐个添加任务
	for _, task := range []*schema.Task{dcTask, daTask, rgTask} {
		if err := h.TaskManager.AddTask(context.Background(), task); err != nil {
			return nil, err
		}
	}

	h.mu.Lock()
	h.round++
	h.outputChunks = append(h.outputChunks, "成功调用handle_input回调")
	h.mu.Unlock()

	return map[string]any{"status": "success", "tasks_created": 3}, nil
}

func (h *deepSearchEventHandler) HandleTaskInteraction(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return map[string]any{"status": "not_supported"}, nil
}

// HandleTaskCompletion 处理任务完成事件，检查当前优先级所有任务是否完成，
// 若完成则通过 UpdateTaskStatus 激活下一优先级的等待任务。
// 注意：GetTask 返回深拷贝，必须使用 UpdateTaskStatus 持久化状态变更。
func (h *deepSearchEventHandler) HandleTaskCompletion(_ context.Context, input *modules.EventHandlerInput) (map[string]any, error) {
	h.mu.Lock()
	h.handleCompletionCalled++
	eventID := ""
	if input.Event != nil {
		eventID = input.Event.GetEventID()
	}
	h.outputChunks = append(h.outputChunks, fmt.Sprintf("成功调用handle_task_completion回调 event: %s", eventID))
	h.mu.Unlock()

	// 检查当前优先级所有任务是否完成，若完成则激活下一优先级
	allTasks, _ := h.TaskManager.GetTask(context.Background(), nil)
	priorities := sortedUniquePriorities(allTasks)

	for i, currentPriority := range priorities {
		currentTasks := filterByPriority(allTasks, currentPriority)
		allCompleted := allTasksCompleted(currentTasks)

		if allCompleted && i+1 < len(priorities) {
			nextPriority := priorities[i+1]
			nextWaiting := filterByPriorityAndStatus(allTasks, nextPriority, schema.TaskWaiting)
			for _, task := range nextWaiting {
				// 通过 UpdateTaskStatus 持久化状态变更（GetTask 返回深拷贝）
				_ = h.TaskManager.UpdateTaskStatus(context.Background(), task.TaskID, schema.TaskSubmitted)
			}
			if len(nextWaiting) > 0 {
				return map[string]any{"status": "success", "tasks_activated": len(nextWaiting)}, nil
			}
		}
	}

	return map[string]any{"status": "success", "tasks_created": 0}, nil
}

func (h *deepSearchEventHandler) HandleTaskFailed(_ context.Context, _ *modules.EventHandlerInput) (map[string]any, error) {
	return map[string]any{"status": "success", "tasks_failed": 1}, nil
}

// GetBase 返回基础依赖容器（实现 EventHandler 接口）
func (h *deepSearchEventHandler) GetBase() *modules.EventHandlerBase {
	return &h.EventHandlerBase
}

// PrepareRound 准备轮次（实现 EventHandler 接口）
func (h *deepSearchEventHandler) PrepareRound() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.round++
	return h.round
}

// WaitCompletion 等待完成（实现 EventHandler 接口）
func (h *deepSearchEventHandler) WaitCompletion(_ context.Context, _ time.Duration) map[string]any {
	return map[string]any{"status": "completed"}
}

// OnAbort 中止回调（实现 EventHandler 接口）
func (h *deepSearchEventHandler) OnAbort() {}

func (h *deepSearchEventHandler) GetHandleInputCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handleInputCalled
}

func (h *deepSearchEventHandler) GetHandleCompletionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handleCompletionCalled
}

func (h *deepSearchEventHandler) GetOutputChunks() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]string, len(h.outputChunks))
	copy(result, h.outputChunks)
	return result
}

// ─── 辅助函数 ───

// sortedUniquePriorities 从任务列表提取排序后的唯一优先级列表
func sortedUniquePriorities(tasks []*schema.Task) []int {
	seen := map[int]bool{}
	for _, t := range tasks {
		seen[t.Priority] = true
	}
	var result []int
	for p := range seen {
		result = append(result, p)
	}
	// 简单排序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i] > result[j] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// filterByPriority 按优先级筛选任务
func filterByPriority(tasks []*schema.Task, priority int) []*schema.Task {
	var result []*schema.Task
	for _, t := range tasks {
		if t.Priority == priority {
			result = append(result, t)
		}
	}
	return result
}

// filterByPriorityAndStatus 按优先级和状态筛选任务
func filterByPriorityAndStatus(tasks []*schema.Task, priority int, status schema.TaskStatus) []*schema.Task {
	var result []*schema.Task
	for _, t := range tasks {
		if t.Priority == priority && t.Status == status {
			result = append(result, t)
		}
	}
	return result
}

// allTasksCompleted 检查所有任务是否完成
func allTasksCompleted(tasks []*schema.Task) bool {
	for _, t := range tasks {
		if t.Status != schema.TaskCompleted {
			return false
		}
	}
	return true
}

// ──────────────────── 测试方法 ────────────────────

// TestDeepSearch_三阶段任务创建 验证 HandleInput 创建三阶段任务，
// 优先级 1 为 Submitted，优先级 2/3 为 Waiting
func (s *DeepSearchE2ESuite) TestDeepSearch_三阶段任务创建() {
	handler := &deepSearchEventHandler{}
	handler.TaskManager = s.taskManager

	// 模拟 HandleInput
	input := &modules.EventHandlerInput{
		Event: &schema.InputEvent{
			BaseEvent: schema.BaseEvent{
				EventTypeField: schema.EventInput,
				EventID:        "evt_input_1",
			},
		},
		Session: &mockSessionFacade{sessionID: s.sessionID},
	}

	result, err := handler.HandleInput(s.Ctx, input)
	s.Require().NoError(err)
	s.Equal("success", result["status"])

	// 验证任务创建
	allTasks, err := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	s.Require().NoError(err)
	s.Equal(3, len(allTasks), "应创建 3 个任务")

	// 验证优先级分布
	p1Tasks := filterByPriority(allTasks, 1)
	p2Tasks := filterByPriority(allTasks, 2)
	p3Tasks := filterByPriority(allTasks, 3)
	s.Equal(1, len(p1Tasks), "应有 1 个优先级 1 任务")
	s.Equal(1, len(p2Tasks), "应有 1 个优先级 2 任务")
	s.Equal(1, len(p3Tasks), "应有 1 个优先级 3 任务")

	// 验证状态
	s.Equal(schema.TaskSubmitted, p1Tasks[0].Status, "优先级 1 任务应为 Submitted")
	s.Equal(schema.TaskWaiting, p2Tasks[0].Status, "优先级 2 任务应为 Waiting")
	s.Equal(schema.TaskWaiting, p3Tasks[0].Status, "优先级 3 任务应为 Waiting")

	// 验证任务类型
	s.Equal(taskTypeDataCollect, p1Tasks[0].TaskType, "优先级 1 应为 data_collect")
	s.Equal(taskTypeDataAnalysis, p2Tasks[0].TaskType, "优先级 2 应为 data_analysis")
	s.Equal(taskTypeReportGenerate, p3Tasks[0].TaskType, "优先级 3 应为 report_generate")
}

// TestDeepSearch_优先级驱动阶段激活 验证 HandleTaskCompletion 正确检测
// 阶段完成并激活下一阶段
func (s *DeepSearchE2ESuite) TestDeepSearch_优先级驱动阶段激活() {
	handler := &deepSearchEventHandler{}
	handler.TaskManager = s.taskManager

	// 先创建三阶段任务
	_, err := handler.HandleInput(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.InputEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventInput, EventID: "evt_input_2"},
		},
		Session: &mockSessionFacade{sessionID: s.sessionID},
	})
	s.Require().NoError(err)

	allTasks, _ := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p2Tasks := filterByPriority(allTasks, 2)
	s.Equal(schema.TaskWaiting, p2Tasks[0].Status, "激活前数据分析应为 Waiting")

	// 通过 UpdateTaskStatus 模拟数据收集任务完成（GetTask 返回深拷贝）
	p1Tasks := filterByPriority(allTasks, 1)
	s.Require().NoError(s.taskManager.UpdateTaskStatus(s.Ctx, p1Tasks[0].TaskID, schema.TaskCompleted))

	// 调用 HandleTaskCompletion
	completionInput := &modules.EventHandlerInput{
		Event: &schema.TaskCompletionEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventTaskCompletion, EventID: "evt_complete_dc"},
		},
	}
	_, err = handler.HandleTaskCompletion(s.Ctx, completionInput)
	s.Require().NoError(err)

	// 验证数据分析任务被激活
	allTasks, _ = s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p2Tasks = filterByPriority(allTasks, 2)
	s.Equal(schema.TaskSubmitted, p2Tasks[0].Status, "数据收集完成后数据分析应被激活为 Submitted")

	// 优先级 3 仍为 Waiting
	p3Tasks := filterByPriority(allTasks, 3)
	s.Equal(schema.TaskWaiting, p3Tasks[0].Status, "数据分析完成前报告生成应为 Waiting")

	// 验证激活计数
	s.Equal(1, handler.GetHandleCompletionCount(), "HandleTaskCompletion 应被调用 1 次")
}

// TestDeepSearch_完整三阶段链式完成 验证三个阶段依次完成
func (s *DeepSearchE2ESuite) TestDeepSearch_完整三阶段链式完成() {
	handler := &deepSearchEventHandler{}
	handler.TaskManager = s.taskManager

	// 创建三阶段任务
	_, err := handler.HandleInput(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.InputEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventInput, EventID: "evt_input_3"},
		},
		Session: &mockSessionFacade{sessionID: s.sessionID},
	})
	s.Require().NoError(err)

	// 阶段 1 完成 → 激活阶段 2
	allTasks, _ := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p1Tasks := filterByPriority(allTasks, 1)
	s.Require().NoError(s.taskManager.UpdateTaskStatus(s.Ctx, p1Tasks[0].TaskID, schema.TaskCompleted))

	_, err = handler.HandleTaskCompletion(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.TaskCompletionEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventTaskCompletion, EventID: "evt_c1"},
		},
	})
	s.Require().NoError(err)

	// 阶段 2 完成 → 激活阶段 3
	allTasks, _ = s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p2Tasks := filterByPriority(allTasks, 2)
	s.Require().NoError(s.taskManager.UpdateTaskStatus(s.Ctx, p2Tasks[0].TaskID, schema.TaskCompleted))

	_, err = handler.HandleTaskCompletion(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.TaskCompletionEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventTaskCompletion, EventID: "evt_c2"},
		},
	})
	s.Require().NoError(err)

	// 阶段 3 完成
	allTasks, _ = s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p3Tasks := filterByPriority(allTasks, 3)
	s.Require().NoError(s.taskManager.UpdateTaskStatus(s.Ctx, p3Tasks[0].TaskID, schema.TaskCompleted))

	_, err = handler.HandleTaskCompletion(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.TaskCompletionEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventTaskCompletion, EventID: "evt_c3"},
		},
	})
	s.Require().NoError(err)

	// 验证回调次数
	s.Equal(1, handler.GetHandleInputCount(), "HandleInput 应被调用 1 次")
	s.Equal(3, handler.GetHandleCompletionCount(), "HandleTaskCompletion 应被调用 3 次")

	// 验证所有任务完成
	allTasks, _ = s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	for _, t := range allTasks {
		s.Equal(schema.TaskCompleted, t.Status, fmt.Sprintf("任务 %s 应为 Completed", t.TaskID))
	}

	// 验证输出包含所有回调
	outputs := handler.GetOutputChunks()
	s.Contains(outputs, "成功调用handle_input回调", "输出应包含 handle_input 回调")

	completionCount := 0
	for _, out := range outputs {
		if strings.Contains(out, "成功调用handle_task_completion回调") {
			completionCount++
		}
	}
	s.Equal(3, completionCount, "应有 3 次 handle_task_completion 回调输出")
}

// TestDeepSearch_多轮对话独立任务 验证两轮对话各自创建独立任务集
func (s *DeepSearchE2ESuite) TestDeepSearch_多轮对话独立任务() {
	handler := &deepSearchEventHandler{}
	handler.TaskManager = s.taskManager

	// 第一轮
	_, err := handler.HandleInput(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.InputEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventInput, EventID: "round1"},
		},
		Session: &mockSessionFacade{sessionID: s.sessionID},
	})
	s.Require().NoError(err)

	firstRoundTasks, _ := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	firstRoundCount := len(firstRoundTasks)
	s.Equal(3, firstRoundCount, "第一轮应创建 3 个任务")

	// 通过 UpdateTaskStatus 完成第一轮所有任务
	for _, t := range firstRoundTasks {
		s.Require().NoError(s.taskManager.UpdateTaskStatus(s.Ctx, t.TaskID, schema.TaskCompleted))
		handler.HandleTaskCompletion(s.Ctx, &modules.EventHandlerInput{
			Event: &schema.TaskCompletionEvent{
				BaseEvent: schema.BaseEvent{EventTypeField: schema.EventTaskCompletion, EventID: "c_" + t.TaskID},
			},
		})
	}

	// 第二轮
	_, err = handler.HandleInput(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.InputEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventInput, EventID: "round2"},
		},
		Session: &mockSessionFacade{sessionID: s.sessionID},
	})
	s.Require().NoError(err)

	allTasks, _ := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	secondRoundCount := len(allTasks)
	s.GreaterOrEqual(secondRoundCount, firstRoundCount, "第二轮后任务数应 ≥ 第一轮")

	// 验证至少 3 个已完成任务（第一轮的）
	completedCount := 0
	for _, t := range allTasks {
		if t.Status == schema.TaskCompleted {
			completedCount++
		}
	}
	s.GreaterOrEqual(completedCount, 3, "应至少有 3 个已完成任务（第一轮的）")

	// 验证回调次数
	s.Equal(2, handler.GetHandleInputCount(), "HandleInput 应被调用 2 次")
}

// TestDeepSearch_TaskManager生命周期 验证 TaskManager 的
// AddTask、GetTask、UpdateTaskStatus 操作
func (s *DeepSearchE2ESuite) TestDeepSearch_TaskManager生命周期() {
	// 创建并添加任务
	task1 := schema.NewTask(s.sessionID, taskTypeDataCollect)
	task1.Priority = 1
	task1.Status = schema.TaskSubmitted

	err := s.taskManager.AddTask(s.Ctx, task1)
	s.Require().NoError(err)

	task2 := schema.NewTask(s.sessionID, taskTypeDataAnalysis)
	task2.Priority = 2
	task2.Status = schema.TaskWaiting

	err = s.taskManager.AddTask(s.Ctx, task2)
	s.Require().NoError(err)

	// 验证获取
	allTasks, err := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	s.Require().NoError(err)
	s.Equal(2, len(allTasks), "应有 2 个任务")

	// 验证状态更新
	err = s.taskManager.UpdateTaskStatus(s.Ctx, task1.TaskID, schema.TaskWorking)
	s.Require().NoError(err)

	allTasks, _ = s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p1Tasks := filterByPriority(allTasks, 1)
	s.Equal(schema.TaskWorking, p1Tasks[0].Status, "应为 Working")

	err = s.taskManager.UpdateTaskStatus(s.Ctx, task1.TaskID, schema.TaskCompleted)
	s.Require().NoError(err)

	allTasks, _ = s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	p1Tasks = filterByPriority(allTasks, 1)
	s.Equal(schema.TaskCompleted, p1Tasks[0].Status, "应为 Completed")

	// 验证 TaskStatus.IsTerminal
	s.True(schema.TaskCompleted.IsTerminal(), "Completed 应为终止状态")
	s.True(schema.TaskFailed.IsTerminal(), "Failed 应为终止状态")
	s.False(schema.TaskSubmitted.IsTerminal(), "Submitted 不应为终止状态")
	s.False(schema.TaskWorking.IsTerminal(), "Working 不应为终止状态")
	s.False(schema.TaskWaiting.IsTerminal(), "Waiting 不应为终止状态")
}

// TestDeepSearch_EventQueue启动停止 验证 EventQueue 的 Start/Stop 生命周期
func (s *DeepSearchE2ESuite) TestDeepSearch_EventQueue启动停止() {
	s.eventQueue.Start()

	ctx, cancel := context.WithTimeout(s.Ctx, 5*time.Second)
	defer cancel()
	err := s.eventQueue.Stop(ctx)
	s.Require().NoError(err, "EventQueue 停止不应返回错误")
}

// TestDeepSearch_TaskExecutor注册 验证 TaskExecutorRegistry 的
// 注册、获取、移除操作
func (s *DeepSearchE2ESuite) TestDeepSearch_TaskExecutor注册() {
	registry := modules.NewTaskExecutorRegistry()

	// 注册
	collectBuilder := func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return &mockTaskExecutor{name: taskTypeDataCollect}
	}
	registry.AddTaskExecutor(taskTypeDataCollect, collectBuilder)

	// 获取
	executor, err := registry.GetTaskExecutor(taskTypeDataCollect, nil)
	s.Require().NoError(err, "已注册的执行器应能获取")
	s.NotNil(executor, "执行器不应为 nil")

	// 未注册的类型应返回错误
	_, err = registry.GetTaskExecutor("unknown_type", nil)
	s.Error(err, "未注册的执行器应返回错误")

	// 移除
	registry.RemoveTaskExecutor(taskTypeDataCollect)
	_, err = registry.GetTaskExecutor(taskTypeDataCollect, nil)
	s.Error(err, "移除后获取应返回错误")
}

// TestDeepSearch_EventHandler回调输出 验证 EventHandler 的
// 回调输出文本包含预期标记
func (s *DeepSearchE2ESuite) TestDeepSearch_EventHandler回调输出() {
	handler := &deepSearchEventHandler{}
	handler.TaskManager = s.taskManager

	// 执行完整三阶段流程
	_, err := handler.HandleInput(s.Ctx, &modules.EventHandlerInput{
		Event: &schema.InputEvent{
			BaseEvent: schema.BaseEvent{EventTypeField: schema.EventInput, EventID: "evt_1"},
		},
		Session: &mockSessionFacade{sessionID: s.sessionID},
	})
	s.Require().NoError(err)

	outputs := handler.GetOutputChunks()
	s.Contains(outputs, "成功调用handle_input回调", "输出应包含 handle_input 回调标记")

	// 完成三个阶段并验证 completion 回调
	allTasks, _ := s.taskManager.GetTask(s.Ctx, &modules.TaskFilter{SessionID: s.sessionID})
	for priority := 1; priority <= 3; priority++ {
		tasks := filterByPriority(allTasks, priority)
		if len(tasks) > 0 {
			s.Require().NoError(s.taskManager.UpdateTaskStatus(s.Ctx, tasks[0].TaskID, schema.TaskCompleted))
			handler.HandleTaskCompletion(s.Ctx, &modules.EventHandlerInput{
				Event: &schema.TaskCompletionEvent{
					BaseEvent: schema.BaseEvent{EventTypeField: schema.EventTaskCompletion, EventID: fmt.Sprintf("evt_c_%d", priority)},
				},
			})
		}
	}

	outputs = handler.GetOutputChunks()
	completionOutputs := 0
	for _, out := range outputs {
		if strings.Contains(out, "成功调用handle_task_completion回调") {
			completionOutputs++
		}
	}
	s.Equal(3, completionOutputs, "应有 3 次 completion 回调输出")
}

// TestDeepSearch_TaskSchema验证 验证 Task 的字段设置
func (s *DeepSearchE2ESuite) TestDeepSearch_TaskSchema验证() {
	task := schema.NewTask(s.sessionID, taskTypeDataCollect)

	// 验证默认值
	s.NotEmpty(task.TaskID, "TaskID 应自动生成")
	s.Equal(s.sessionID, task.SessionID, "SessionID 应匹配")
	s.Equal(taskTypeDataCollect, task.TaskType, "TaskType 应匹配")
	s.Equal(schema.TaskSubmitted, task.Status, "默认状态应为 Submitted")

	// 设置自定义字段
	task.Priority = 1
	task.ContextID = "ctx_1"
	task.Metadata = map[string]any{"topic": "芯片"}

	s.Equal(1, task.Priority, "Priority 应为 1")
	s.Equal("ctx_1", task.ContextID, "ContextID 应匹配")
	s.Equal("芯片", task.Metadata["topic"], "Metadata.topic 应匹配")

	// 验证 TaskStatus 枚举值（Go 使用 "working" 而非 "running"）
	s.Equal(schema.TaskStatus("submitted"), schema.TaskSubmitted)
	s.Equal(schema.TaskStatus("waiting"), schema.TaskWaiting)
	s.Equal(schema.TaskStatus("working"), schema.TaskWorking)
	s.Equal(schema.TaskStatus("completed"), schema.TaskCompleted)
	s.Equal(schema.TaskStatus("failed"), schema.TaskFailed)
}
