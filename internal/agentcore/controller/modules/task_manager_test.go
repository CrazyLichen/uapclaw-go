package modules

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestTask 创建测试用 Task 的工厂函数。
func newTestTask(sessionID, taskID, taskType string, status schema.TaskStatus) *schema.Task {
	return &schema.Task{
		SessionID: sessionID,
		TaskID:    taskID,
		TaskType:  taskType,
		Status:    status,
		Priority:  0,
		Metadata:  map[string]any{},
	}
}

// newTestTaskWithParent 创建带父任务的测试 Task。
func newTestTaskWithParent(sessionID, taskID, taskType string, status schema.TaskStatus, parentID string) *schema.Task {
	return &schema.Task{
		SessionID:    sessionID,
		TaskID:       taskID,
		TaskType:     taskType,
		Status:       status,
		Priority:     0,
		ParentTaskID: parentID,
		Metadata:     map[string]any{},
	}
}

// newTestTaskManager 创建测试用 TaskManager。
func newTestTaskManager() *TaskManager {
	return NewTaskManager(config.DefaultControllerConfig())
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTaskManager_AddTask_正常 验证正常添加任务
func TestTaskManager_AddTask_正常(t *testing.T) {
	tm := newTestTaskManager()
	task := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)

	err := tm.AddTask(context.Background(), task)
	assert.NoError(t, err)

	// 验证任务已添加
	tasks, err := tm.GetTask(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task1", tasks[0].TaskID)
}

// TestTaskManager_AddTask_重复ID报错 验证重复添加相同ID任务报错
func TestTaskManager_AddTask_重复ID报错(t *testing.T) {
	tm := newTestTaskManager()
	task := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)

	err := tm.AddTask(context.Background(), task)
	assert.NoError(t, err)

	err = tm.AddTask(context.Background(), task)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Equal(t, exception.StatusAgentControllerTaskParamError, baseErr.Status())
}

// TestTaskManager_GetTask_按TaskID 验证按任务ID查询
func TestTaskManager_GetTask_按TaskID(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))

	// 按单个 TaskID 查询
	tasks, err := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task1", tasks[0].TaskID)

	// 按多个 TaskID 查询
	tasks, err = tm.GetTask(context.Background(), &TaskFilter{TaskID: []string{"task1", "task2"}})
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)
}

// TestTaskManager_GetTask_按SessionID 验证按会话ID查询
func TestTaskManager_GetTask_按SessionID(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess2", "task2", "test", schema.TaskWorking))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task3", "test", schema.TaskCompleted))

	tasks, err := tm.GetTask(context.Background(), &TaskFilter{SessionID: "sess1"})
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)
	for _, task := range tasks {
		assert.Equal(t, "sess1", task.SessionID)
	}
}

// TestTaskManager_GetTask_按Status 验证按状态查询
func TestTaskManager_GetTask_按Status(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task3", "test", schema.TaskCompleted))

	tasks, err := tm.GetTask(context.Background(), &TaskFilter{Status: schema.TaskSubmitted})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, schema.TaskSubmitted, tasks[0].Status)
}

// TestTaskManager_GetTask_按优先级 验证按优先级查询
func TestTaskManager_GetTask_按优先级(t *testing.T) {
	tm := newTestTaskManager()
	task1 := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task1.Priority = 1
	task2 := newTestTask("sess1", "task2", "test", schema.TaskWorking)
	task2.Priority = 2

	_ = tm.AddTask(context.Background(), task1)
	_ = tm.AddTask(context.Background(), task2)

	tasks, err := tm.GetTask(context.Background(), &TaskFilter{Priority: 1})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, 1, tasks[0].Priority)
}

// TestTaskManager_GetTask_按IsRoot 验证只查根任务
func TestTaskManager_GetTask_按IsRoot(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child)

	tasks, err := tm.GetTask(context.Background(), &TaskFilter{IsRoot: true})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "parent", tasks[0].TaskID)
}

// TestTaskManager_GetTask_带子任务 验证 WithChildren 包含子任务
func TestTaskManager_GetTask_带子任务(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child)

	tasks, err := tm.GetTask(context.Background(), &TaskFilter{TaskID: "parent", WithChildren: true})
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)

	taskIDs := make(map[string]struct{})
	for _, task := range tasks {
		taskIDs[task.TaskID] = struct{}{}
	}
	_, hasParent := taskIDs["parent"]
	_, hasChild := taskIDs["child"]
	assert.True(t, hasParent)
	assert.True(t, hasChild)
}

// TestTaskManager_GetTask_filter为nil返回全部 验证 filter 为 nil 时返回全部任务
func TestTaskManager_GetTask_filter为nil返回全部(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))

	tasks, err := tm.GetTask(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)
}

// TestTaskManager_GetTask_priorityHighest报错 验证 GetTask 不支持 priority=highest
func TestTaskManager_GetTask_priorityHighest报错(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))

	_, err := tm.GetTask(context.Background(), &TaskFilter{Priority: "highest"})
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestTaskManager_PopTask_正常 验证正常弹出任务
func TestTaskManager_PopTask_正常(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))

	tasks, err := tm.PopTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task1", tasks[0].TaskID)

	// 验证 task1 已移除
	remaining, err := tm.GetTask(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "task2", remaining[0].TaskID)
}

// TestTaskManager_PopTask_filter为nil报错 验证 PopTask 的 filter 为 nil 报错
func TestTaskManager_PopTask_filter为nil报错(t *testing.T) {
	tm := newTestTaskManager()

	_, err := tm.PopTask(context.Background(), nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestTaskManager_PopTask_priorityHighest 验证 PopTask priority=highest 取最大优先级
func TestTaskManager_PopTask_priorityHighest(t *testing.T) {
	tm := newTestTaskManager()
	task1 := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task1.Priority = 1
	task2 := newTestTask("sess1", "task2", "test", schema.TaskWorking)
	task2.Priority = 5
	task3 := newTestTask("sess1", "task3", "test", schema.TaskCompleted)
	task3.Priority = 3

	_ = tm.AddTask(context.Background(), task1)
	_ = tm.AddTask(context.Background(), task2)
	_ = tm.AddTask(context.Background(), task3)

	tasks, err := tm.PopTask(context.Background(), &TaskFilter{Priority: "highest"})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, 5, tasks[0].Priority)
	assert.Equal(t, "task2", tasks[0].TaskID)
}

// TestTaskManager_UpdateTask_正常 验证正常更新任务
func TestTaskManager_UpdateTask_正常(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))

	// 获取并修改
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	task := tasks[0]
	task.Status = schema.TaskWorking

	updated := tm.UpdateTask(context.Background(), task)
	assert.True(t, updated)

	// 验证更新
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.Equal(t, schema.TaskWorking, tasks[0].Status)
}

// TestTaskManager_UpdateTask_不存在返回false 验证更新不存在的任务返回 false
func TestTaskManager_UpdateTask_不存在返回false(t *testing.T) {
	tm := newTestTaskManager()
	task := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)

	updated := tm.UpdateTask(context.Background(), task)
	assert.False(t, updated)
}

// TestTaskManager_UpdateTask_优先级变更更新索引 验证优先级变更时索引同步更新
func TestTaskManager_UpdateTask_优先级变更更新索引(t *testing.T) {
	tm := newTestTaskManager()
	task := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task.Priority = 1
	_ = tm.AddTask(context.Background(), task)

	// 修改优先级
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	taskUpd := tasks[0]
	taskUpd.Priority = 5
	_ = tm.UpdateTask(context.Background(), taskUpd)

	// 验证新优先级索引
	p5Tasks, err := tm.GetTask(context.Background(), &TaskFilter{Priority: 5})
	assert.NoError(t, err)
	assert.Len(t, p5Tasks, 1)

	// 旧优先级索引应为空
	p1Tasks, err := tm.GetTask(context.Background(), &TaskFilter{Priority: 1})
	assert.NoError(t, err)
	assert.Len(t, p1Tasks, 0)
}

// TestTaskManager_UpdateTask_父任务变更更新层级 验证父任务变更时层级索引同步更新
func TestTaskManager_UpdateTask_父任务变更更新层级(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskWorking))

	// task1 原来没有父任务（根任务），现在设 parent 为父任务
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	task := tasks[0]
	task.ParentTaskID = "parent"
	_ = tm.UpdateTask(context.Background(), task)

	// 验证 task1 不再是根任务
	rootTasks, err := tm.GetTask(context.Background(), &TaskFilter{IsRoot: true})
	assert.NoError(t, err)
	assert.Len(t, rootTasks, 1)
	assert.Equal(t, "parent", rootTasks[0].TaskID)

	// 验证 parent 有子任务
	children, err := tm.GetChildTask(context.Background(), "parent", false)
	assert.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "task1", children[0].TaskID)
}

// TestTaskManager_RemoveTask_正常 验证正常删除任务
func TestTaskManager_RemoveTask_正常(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))

	err := tm.RemoveTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.NoError(t, err)

	// 验证 task1 已移除
	tasks, err := tm.GetTask(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task2", tasks[0].TaskID)
}

// TestTaskManager_RemoveTask_父任务删除子任务提升为根任务 验证删除父任务时子任务提升为根任务
func TestTaskManager_RemoveTask_父任务删除子任务提升为根任务(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child)

	// 删除父任务
	err := tm.RemoveTask(context.Background(), &TaskFilter{TaskID: "parent"})
	assert.NoError(t, err)

	// 验证子任务被提升为根任务
	rootTasks, err := tm.GetTask(context.Background(), &TaskFilter{IsRoot: true})
	assert.NoError(t, err)
	assert.Len(t, rootTasks, 1)
	assert.Equal(t, "child", rootTasks[0].TaskID)
}

// TestTaskManager_UpdateTaskStatus_正常 验证正常更新任务状态
func TestTaskManager_UpdateTaskStatus_正常(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))

	err := tm.UpdateTaskStatus(context.Background(), "task1", schema.TaskWorking)
	assert.NoError(t, err)

	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.Equal(t, schema.TaskWorking, tasks[0].Status)
}

// TestTaskManager_UpdateTaskStatus_FAILED设置ErrorMessage 验证 FAILED 状态设置 ErrorMessage
func TestTaskManager_UpdateTaskStatus_FAILED设置ErrorMessage(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))

	err := tm.UpdateTaskStatus(context.Background(), "task1", schema.TaskFailed, WithErrorMessage("something went wrong"))
	assert.NoError(t, err)

	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.Equal(t, schema.TaskFailed, tasks[0].Status)
	assert.Equal(t, "something went wrong", tasks[0].ErrorMessage)
}

// TestTaskManager_UpdateTaskStatus_触发提交通知 验证 SUBMITTED 状态触发通知回调
func TestTaskManager_UpdateTaskStatus_触发提交通知(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskWorking))

	var called int32
	tm.SetOnTaskSubmitted(func() {
		atomic.AddInt32(&called, 1)
	})

	// 更新为 SUBMITTED 状态触发回调
	err := tm.UpdateTaskStatus(context.Background(), "task1", schema.TaskSubmitted)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}

// TestTaskManager_UpdateTaskStatus_不存在报错 验证更新不存在的任务状态报错
func TestTaskManager_UpdateTaskStatus_不存在报错(t *testing.T) {
	tm := newTestTaskManager()

	err := tm.UpdateTaskStatus(context.Background(), "nonexistent", schema.TaskWorking)
	assert.Error(t, err)
}

// TestTaskManager_UpdateTaskStatus_更新子任务 验证 WithChildren 更新子任务状态
func TestTaskManager_UpdateTaskStatus_更新子任务(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskWorking))
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child)

	err := tm.UpdateTaskStatus(context.Background(), "parent", schema.TaskCompleted, WithChildren(true))
	assert.NoError(t, err)

	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "child"})
	assert.Equal(t, schema.TaskCompleted, tasks[0].Status)
}

// TestTaskManager_UpdateTaskStatus_递归更新子任务 验证 IsRecursive 递归更新子任务状态
func TestTaskManager_UpdateTaskStatus_递归更新子任务(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "root", "test", schema.TaskWorking))
	child1 := newTestTaskWithParent("sess1", "child1", "test", schema.TaskWorking, "root")
	_ = tm.AddTask(context.Background(), child1)
	child2 := newTestTaskWithParent("sess1", "child2", "test", schema.TaskWorking, "child1")
	_ = tm.AddTask(context.Background(), child2)

	err := tm.UpdateTaskStatus(context.Background(), "root", schema.TaskPaused, IsRecursive(true))
	assert.NoError(t, err)

	// 验证所有子任务状态已更新
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "child1"})
	assert.Equal(t, schema.TaskPaused, tasks[0].Status)
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: "child2"})
	assert.Equal(t, schema.TaskPaused, tasks[0].Status)
}

// TestTaskManager_SetPriority_正常 验证正常设置优先级
func TestTaskManager_SetPriority_正常(t *testing.T) {
	tm := newTestTaskManager()
	task := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task.Priority = 1
	_ = tm.AddTask(context.Background(), task)

	err := tm.SetPriority(context.Background(), "task1", 10)
	assert.NoError(t, err)

	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.Equal(t, 10, tasks[0].Priority)
}

// TestTaskManager_SetPriority_更新索引 验证优先级索引同步更新
func TestTaskManager_SetPriority_更新索引(t *testing.T) {
	tm := newTestTaskManager()
	task := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task.Priority = 1
	_ = tm.AddTask(context.Background(), task)

	err := tm.SetPriority(context.Background(), "task1", 5)
	assert.NoError(t, err)

	// 新优先级应有任务
	p5Tasks, err := tm.GetTask(context.Background(), &TaskFilter{Priority: 5})
	assert.NoError(t, err)
	assert.Len(t, p5Tasks, 1)

	// 旧优先级应无任务
	p1Tasks, err := tm.GetTask(context.Background(), &TaskFilter{Priority: 1})
	assert.NoError(t, err)
	assert.Len(t, p1Tasks, 0)
}

// TestTaskManager_SetPriority_不存在报错 验证设置不存在的任务优先级报错
func TestTaskManager_SetPriority_不存在报错(t *testing.T) {
	tm := newTestTaskManager()

	err := tm.SetPriority(context.Background(), "nonexistent", 10)
	assert.Error(t, err)
}

// TestTaskManager_SetPriority_更新子任务 验证 WithChildrenPriority 同时更新子任务优先级
func TestTaskManager_SetPriority_更新子任务(t *testing.T) {
	tm := newTestTaskManager()
	parent := newTestTask("sess1", "parent", "test", schema.TaskWorking)
	parent.Priority = 1
	_ = tm.AddTask(context.Background(), parent)
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "parent")
	child.Priority = 1
	_ = tm.AddTask(context.Background(), child)

	err := tm.SetPriority(context.Background(), "parent", 10, WithChildrenPriority(true))
	assert.NoError(t, err)

	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "child"})
	assert.Equal(t, 10, tasks[0].Priority)
}

// TestTaskManager_GetChildTask_直接子任务 验证获取直接子任务
func TestTaskManager_GetChildTask_直接子任务(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	child1 := newTestTaskWithParent("sess1", "child1", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child1)
	child2 := newTestTaskWithParent("sess1", "child2", "test", schema.TaskWorking, "child1")
	_ = tm.AddTask(context.Background(), child2)

	children, err := tm.GetChildTask(context.Background(), "parent", false)
	assert.NoError(t, err)
	assert.Len(t, children, 1)
	assert.Equal(t, "child1", children[0].TaskID)
}

// TestTaskManager_GetChildTask_递归子任务 验证获取递归子任务
func TestTaskManager_GetChildTask_递归子任务(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	child1 := newTestTaskWithParent("sess1", "child1", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child1)
	child2 := newTestTaskWithParent("sess1", "child2", "test", schema.TaskWorking, "child1")
	_ = tm.AddTask(context.Background(), child2)

	children, err := tm.GetChildTask(context.Background(), "parent", true)
	assert.NoError(t, err)
	assert.Len(t, children, 2)

	childIDs := make(map[string]struct{})
	for _, c := range children {
		childIDs[c.TaskID] = struct{}{}
	}
	_, has1 := childIDs["child1"]
	_, has2 := childIDs["child2"]
	assert.True(t, has1)
	assert.True(t, has2)
}

// TestTaskManager_GetChildTask_不存在报错 验证获取不存在任务的子任务报错
func TestTaskManager_GetChildTask_不存在报错(t *testing.T) {
	tm := newTestTaskManager()

	_, err := tm.GetChildTask(context.Background(), "nonexistent", false)
	assert.Error(t, err)
}

// TestTaskManager_状态持久化_GetStateLoadState 验证 GetState + LoadState 完整往返
func TestTaskManager_状态持久化_GetStateLoadState(t *testing.T) {
	tm := newTestTaskManager()
	task1 := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task1.Priority = 3
	_ = tm.AddTask(context.Background(), task1)
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))
	child := newTestTaskWithParent("sess1", "child1", "test", schema.TaskWorking, "task1")
	_ = tm.AddTask(context.Background(), child)

	// 获取状态快照
	state, err := tm.GetState(context.Background())
	require.NoError(t, err)
	assert.Len(t, state.Tasks, 3)
	assert.Contains(t, state.RootTasks, "task2")
	assert.Contains(t, state.ChildrenToParent, "child1")
	assert.Equal(t, "task1", state.ChildrenToParent["child1"])

	// 创建新 TaskManager 并加载状态
	tm2 := newTestTaskManager()
	err = tm2.LoadState(context.Background(), state)
	require.NoError(t, err)

	// 验证恢复后数据一致
	tasks, err := tm2.GetTask(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, tasks, 3)

	tasks1, err := tm2.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.NoError(t, err)
	assert.Equal(t, 3, tasks1[0].Priority)
}

// TestTaskManager_状态持久化_ClearState 验证 ClearState 清空所有状态
func TestTaskManager_状态持久化_ClearState(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))

	err := tm.ClearState(context.Background())
	assert.NoError(t, err)

	tasks, err := tm.GetTask(context.Background(), nil)
	assert.NoError(t, err)
	assert.Len(t, tasks, 0)
}

// TestTaskManager_提交通知回调 验证添加 SUBMITTED 任务时触发回调
func TestTaskManager_提交通知回调(t *testing.T) {
	tm := newTestTaskManager()

	var called int32
	tm.SetOnTaskSubmitted(func() {
		atomic.AddInt32(&called, 1)
	})

	// 添加 SUBMITTED 任务触发回调
	err := tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))

	// 添加非 SUBMITTED 任务不触发回调
	err = tm.AddTask(context.Background(), newTestTask("sess1", "task2", "test", schema.TaskWorking))
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}

// TestTaskManager_GetTask_返回深拷贝 验证 GetTask 返回深拷贝，外部修改不影响内部状态
func TestTaskManager_GetTask_返回深拷贝(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "task1", "test", schema.TaskSubmitted))

	tasks, err := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.NoError(t, err)

	// 修改返回的任务
	tasks[0].Status = schema.TaskFailed
	tasks[0].Priority = 999

	// 内部状态不应改变
	internal, err := tm.GetTask(context.Background(), &TaskFilter{TaskID: "task1"})
	assert.NoError(t, err)
	assert.Equal(t, schema.TaskSubmitted, internal[0].Status)
	assert.Equal(t, 0, internal[0].Priority)
}

// TestTaskManager_Config 验证 Config 和 SetConfig 正常工作
func TestTaskManager_Config(t *testing.T) {
	tm := newTestTaskManager()
	cfg := tm.Config()
	assert.NotNil(t, cfg)

	newCfg := config.DefaultControllerConfig()
	newCfg.DefaultTaskPriority = 10
	tm.SetConfig(newCfg)

	updatedCfg := tm.Config()
	assert.Equal(t, 10, updatedCfg.DefaultTaskPriority)
}

// TestTaskManager_LoadState_nil报错 验证 LoadState 传入 nil 报错
func TestTaskManager_LoadState_nil报错(t *testing.T) {
	tm := newTestTaskManager()

	err := tm.LoadState(context.Background(), nil)
	assert.Error(t, err)
}

// TestTaskManager_PopTask_空任务管理器 验证 PopTask 在空管理器上正常返回
func TestTaskManager_PopTask_空任务管理器(t *testing.T) {
	tm := newTestTaskManager()

	tasks, err := tm.PopTask(context.Background(), &TaskFilter{Status: schema.TaskSubmitted})
	assert.NoError(t, err)
	assert.Len(t, tasks, 0)
}

// TestTaskManager_UpdateTask_父任务ID从有到无 验证从子任务变为根任务
func TestTaskManager_UpdateTask_父任务ID从有到无(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "parent", "test", schema.TaskSubmitted))
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "parent")
	_ = tm.AddTask(context.Background(), child)

	// 将子任务的 parentTaskID 清空，变为根任务
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "child"})
	task := tasks[0]
	task.ParentTaskID = ""
	updated := tm.UpdateTask(context.Background(), task)
	assert.True(t, updated)

	// 验证 child 现在是根任务
	rootTasks, err := tm.GetTask(context.Background(), &TaskFilter{IsRoot: true})
	assert.NoError(t, err)
	rootIDs := make(map[string]struct{})
	for _, t := range rootTasks {
		rootIDs[t.TaskID] = struct{}{}
	}
	_, hasParent := rootIDs["parent"]
	_, hasChild := rootIDs["child"]
	assert.True(t, hasParent)
	assert.True(t, hasChild)

	// parent 不再有子任务
	children, err := tm.GetChildTask(context.Background(), "parent", false)
	assert.NoError(t, err)
	assert.Len(t, children, 0)
}

// TestTaskManager_PopTask_最高优先级 验证 priority="highest" 取最大优先级
func TestTaskManager_PopTask_最高优先级(t *testing.T) {
	tm := newTestTaskManager()
	task1 := newTestTask("sess1", "task1", "test", schema.TaskSubmitted)
	task1.Priority = 10
	task2 := newTestTask("sess1", "task2", "test", schema.TaskWorking)
	task2.Priority = 3

	_ = tm.AddTask(context.Background(), task1)
	_ = tm.AddTask(context.Background(), task2)

	// 弹出最高优先级任务
	tasks, err := tm.PopTask(context.Background(), &TaskFilter{Priority: "highest"})
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task1", tasks[0].TaskID)
	assert.Equal(t, 10, tasks[0].Priority)

	// task1 已移除
	remaining, _ := tm.GetTask(context.Background(), nil)
	assert.Len(t, remaining, 1)
}

// TestTaskManager_SetPriority_带子任务 验证 WithChildrenPriority option
func TestTaskManager_SetPriority_带子任务(t *testing.T) {
	tm := newTestTaskManager()
	parent := newTestTask("sess1", "parent", "test", schema.TaskWorking)
	parent.Priority = 1
	_ = tm.AddTask(context.Background(), parent)
	child1 := newTestTaskWithParent("sess1", "child1", "test", schema.TaskWorking, "parent")
	child1.Priority = 1
	_ = tm.AddTask(context.Background(), child1)
	child2 := newTestTaskWithParent("sess1", "child2", "test", schema.TaskWorking, "child1")
	child2.Priority = 1
	_ = tm.AddTask(context.Background(), child2)

	// 设置优先级，WithChildrenPriority 只更新直接子任务
	err := tm.SetPriority(context.Background(), "parent", 20, WithChildrenPriority(true))
	assert.NoError(t, err)

	// parent 和直接子任务 child1 应更新
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "parent"})
	assert.Equal(t, 20, tasks[0].Priority)
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: "child1"})
	assert.Equal(t, 20, tasks[0].Priority)

	// child2 不是直接子任务，不应被 WithChildrenPriority 更新
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: "child2"})
	assert.Equal(t, 1, tasks[0].Priority)
}

// TestTaskManager_SetPriority_递归子任务 验证 IsRecursivePriority option
func TestTaskManager_SetPriority_递归子任务(t *testing.T) {
	tm := newTestTaskManager()
	parent := newTestTask("sess1", "parent", "test", schema.TaskWorking)
	parent.Priority = 1
	_ = tm.AddTask(context.Background(), parent)
	child1 := newTestTaskWithParent("sess1", "child1", "test", schema.TaskWorking, "parent")
	child1.Priority = 1
	_ = tm.AddTask(context.Background(), child1)
	child2 := newTestTaskWithParent("sess1", "child2", "test", schema.TaskWorking, "child1")
	child2.Priority = 1
	_ = tm.AddTask(context.Background(), child2)

	// 设置优先级，IsRecursivePriority 递归更新所有子任务
	err := tm.SetPriority(context.Background(), "parent", 30, IsRecursivePriority(true))
	assert.NoError(t, err)

	// 所有任务都应更新
	tasks, _ := tm.GetTask(context.Background(), &TaskFilter{TaskID: "parent"})
	assert.Equal(t, 30, tasks[0].Priority)
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: "child1"})
	assert.Equal(t, 30, tasks[0].Priority)
	tasks, _ = tm.GetTask(context.Background(), &TaskFilter{TaskID: "child2"})
	assert.Equal(t, 30, tasks[0].Priority)
}

// TestTaskManager_RemoveTask_删除父任务子任务提升为根 验证孤儿子任务提升
func TestTaskManager_RemoveTask_删除父任务子任务提升为根(t *testing.T) {
	tm := newTestTaskManager()
	_ = tm.AddTask(context.Background(), newTestTask("sess1", "grandparent", "test", schema.TaskSubmitted))
	child := newTestTaskWithParent("sess1", "child", "test", schema.TaskWorking, "grandparent")
	_ = tm.AddTask(context.Background(), child)
	grandchild := newTestTaskWithParent("sess1", "grandchild", "test", schema.TaskWorking, "child")
	_ = tm.AddTask(context.Background(), grandchild)

	// 删除 grandparent
	err := tm.RemoveTask(context.Background(), &TaskFilter{TaskID: "grandparent"})
	assert.NoError(t, err)

	// child 被提升为根任务
	rootTasks, _ := tm.GetTask(context.Background(), &TaskFilter{IsRoot: true})
	rootIDs := make(map[string]struct{})
	for _, t := range rootTasks {
		rootIDs[t.TaskID] = struct{}{}
	}
	_, hasChild := rootIDs["child"]
	assert.True(t, hasChild)

	// grandchild 仍是 child 的子任务
	children, _ := tm.GetChildTask(context.Background(), "child", false)
	assert.Len(t, children, 1)
	assert.Equal(t, "grandchild", children[0].TaskID)
}
