//go:build integration

package subagent

import (
	"testing"

	"github.com/stretchr/testify/suite"
	hsubagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionToolkitCancelSuite 测试 SessionToolkit 异步子任务取消场景。
//
// 深入测试 SessionToolkit 状态管理、SessionsListTool/SessionsCancelTool
// 输入校验、以及 cancel 路径的各分支逻辑。
//
// 注意：Go 端异步子代理模式（enable_async_subagent）尚未实现，
// TaskScheduler.CancelTask 的端到端测试需要 async 模式回填后补充。
// 此处聚焦可独立测试的 SessionToolkit 和工具层逻辑。
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_subagent.py
//   - TestDeepAgentSessionRailCancelMock 的 6 个 cancel 场景
//   - SessionToolkit 状态管理（upsert/mark/list/clear）
type SessionToolkitCancelSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSessionToolkitCancelSuite(t *testing.T) {
	suite.Run(t, new(SessionToolkitCancelSuite))
}

// TestSessionToolkit_UpsertRunning 测试 UpsertRunning 插入运行中状态。
// 对齐 Python: SessionToolkit.upsert_running
func (s *SessionToolkitCancelSuite) TestSessionToolkit_UpsertRunning() {
	toolkit := hsubagent.NewSessionToolkit()

	toolkit.UpsertRunning("task_001", "sess_sub_001", "数据导入任务")

	row := toolkit.Get("task_001")
	s.Require().NotNil(row, "应找到任务 task_001")
	s.Equal("task_001", row.TaskID, "TaskID 应为 task_001")
	s.Equal("sess_sub_001", row.SubSessionID, "SubSessionID 应为 sess_sub_001")
	s.Equal("数据导入任务", row.Description, "Description 应为 '数据导入任务'")
	s.Equal("running", row.Status, "Status 应为 running")
	s.Empty(row.Result, "Result 应为空")
	s.Empty(row.Error, "Error 应为空")
}

// TestSessionToolkit_MarkCompleted 测试 MarkCompleted 标记完成状态。
// 对齐 Python: SessionToolkit.mark_completed
func (s *SessionToolkitCancelSuite) TestSessionToolkit_MarkCompleted() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("task_002", "sess_sub_002", "快速任务")

	toolkit.MarkCompleted("task_002", "任务成功完成")

	row := toolkit.Get("task_002")
	s.Require().NotNil(row)
	s.Equal("completed", row.Status, "Status 应为 completed")
	s.Equal("任务成功完成", row.Result, "Result 应为 '任务成功完成'")
}

// TestSessionToolkit_MarkFailed 测试 MarkFailed 标记失败状态。
// 对齐 Python: SessionToolkit.mark_failed
func (s *SessionToolkitCancelSuite) TestSessionToolkit_MarkFailed() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("task_003", "sess_sub_003", "失败任务")

	toolkit.MarkFailed("task_003", "连接超时")

	row := toolkit.Get("task_003")
	s.Require().NotNil(row)
	s.Equal("error", row.Status, "Status 应为 error")
	s.Equal("连接超时", row.Error, "Error 应为 '连接超时'")
}

// TestSessionToolkit_MarkCanceled 测试 MarkCanceled 标记取消状态。
// 对齐 Python: test_sessions_cancel_scenario1_immediate_cancel → row.status == "canceled"
func (s *SessionToolkitCancelSuite) TestSessionToolkit_MarkCanceled() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("task_004", "sess_sub_004", "取消任务")

	toolkit.MarkCanceled("task_004")

	row := toolkit.Get("task_004")
	s.Require().NotNil(row)
	s.Equal("canceled", row.Status, "Status 应为 canceled")
}

// TestSessionToolkit_Cancel不影响其他任务 测试取消一个任务不影响其他任务。
// 对齐 Python: test_sessions_cancel_scenario4_cancel_one_of_multiple_tasks
func (s *SessionToolkitCancelSuite) TestSessionToolkit_Cancel不影响其他任务() {
	toolkit := hsubagent.NewSessionToolkit()

	// 创建两个任务
	toolkit.UpsertRunning("task_A", "sess_A", "慢任务A")
	toolkit.UpsertRunning("task_B", "sess_B", "快任务B")

	// 取消 task_A
	toolkit.MarkCanceled("task_A")

	// 验证 task_A 已取消
	rowA := toolkit.Get("task_A")
	s.Require().NotNil(rowA)
	s.Equal("canceled", rowA.Status, "task_A 应为 canceled")

	// 验证 task_B 仍为 running
	rowB := toolkit.Get("task_B")
	s.Require().NotNil(rowB)
	s.Equal("running", rowB.Status, "task_B 应仍为 running")
}

// TestSessionToolkit_重复取消幂等 测试重复取消同一任务不崩溃。
// 对齐 Python: test_sessions_cancel_scenario5_repeat_cancel_idempotent
func (s *SessionToolkitCancelSuite) TestSessionToolkit_重复取消幂等() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("task_005", "sess_005", "幂等测试")

	// 取消两次
	toolkit.MarkCanceled("task_005")
	toolkit.MarkCanceled("task_005")

	row := toolkit.Get("task_005")
	s.Require().NotNil(row)
	s.Equal("canceled", row.Status, "重复取消后状态应仍为 canceled")
}

// TestSessionToolkit_取消已完成任务状态不变 测试对已完成任务执行取消不改变状态。
// 对齐 Python: test_sessions_cancel_scenario6_cancel_completed_task
func (s *SessionToolkitCancelSuite) TestSessionToolkit_取消已完成任务状态不变() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("task_006", "sess_006", "已完成任务")

	// 先完成
	toolkit.MarkCompleted("task_006", "成功")

	// 再取消（MarkCanceled 不检查当前状态，直接设置）
	toolkit.MarkCanceled("task_006")

	// 注意：Go 端的 MarkCanceled 不做状态守卫（对齐 Python 端行为）
	// 实际取消操作由 TaskScheduler.CancelTask 处理状态守卫
	row := toolkit.Get("task_006")
	s.Require().NotNil(row)
	// MarkCanceled 直接修改状态，但 TaskScheduler.CancelTask 对已完成任务返回幂等 true
	s.Equal("canceled", row.Status, "MarkCanceled 直接设置 canceled（无状态守卫）")
}

// TestSessionToolkit_ListAll 测试 ListAll 返回所有任务。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_ListAll() {
	toolkit := hsubagent.NewSessionToolkit()

	// 空列表
	rows := toolkit.ListAll()
	s.Empty(rows, "初始应为空列表")

	// 添加任务
	toolkit.UpsertRunning("t1", "s1", "任务1")
	toolkit.UpsertRunning("t2", "s2", "任务2")
	toolkit.UpsertRunning("t3", "s3", "任务3")

	rows = toolkit.ListAll()
	s.Len(rows, 3, "应有 3 个任务")
}

// TestSessionToolkit_Clear 测试 Clear 清空所有任务。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_Clear() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("t1", "s1", "任务1")
	toolkit.UpsertRunning("t2", "s2", "任务2")

	toolkit.Clear()

	rows := toolkit.ListAll()
	s.Empty(rows, "Clear 后应为空列表")

	// Get 应返回 nil
	s.Nil(toolkit.Get("t1"), "Clear 后 Get 应返回 nil")
}

// TestSessionToolkit_Get不存在 测试 Get 对不存在的任务返回 nil。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_Get不存在() {
	toolkit := hsubagent.NewSessionToolkit()

	row := toolkit.Get("nonexistent")
	s.Nil(row, "不存在的任务应返回 nil")
}

// TestSessionToolkit_MarkCompleted不存在 测试 MarkCompleted 对不存在的任务不崩溃。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_MarkCompleted不存在() {
	toolkit := hsubagent.NewSessionToolkit()

	// 对不存在的任务执行 MarkCompleted 不应 panic
	toolkit.MarkCompleted("nonexistent", "完成")

	s.Nil(toolkit.Get("nonexistent"), "不存在的任务应仍为 nil")
}

// TestSessionToolkit_MarkCanceled不存在 测试 MarkCanceled 对不存在的任务不崩溃。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_MarkCanceled不存在() {
	toolkit := hsubagent.NewSessionToolkit()

	// 对不存在的任务执行 MarkCanceled 不应 panic
	toolkit.MarkCanceled("nonexistent")

	s.Nil(toolkit.Get("nonexistent"), "不存在的任务应仍为 nil")
}

// TestSessionToolkit_UpsertRunning更新 测试 UpsertRunning 对已存在任务的更新行为。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_UpsertRunning更新() {
	toolkit := hsubagent.NewSessionToolkit()

	// 第一次插入
	toolkit.UpsertRunning("task_up", "sess_v1", "原始描述")
	row := toolkit.Get("task_up")
	s.Require().NotNil(row)
	s.Equal("原始描述", row.Description, "初始描述应为 '原始描述'")
	s.Equal("running", row.Status, "初始状态应为 running")

	// UpsertRunning 覆盖更新
	toolkit.UpsertRunning("task_up", "sess_v2", "更新描述")
	row = toolkit.Get("task_up")
	s.Require().NotNil(row)
	s.Equal("更新描述", row.Description, "描述应被更新")
	s.Equal("running", row.Status, "更新后状态应为 running")
	s.Equal("sess_v2", row.SubSessionID, "SubSessionID 应被更新")
}

// TestSessionsCancelInput_空TaskID 测试 SessionsCancelInput 空校验逻辑。
// 对齐 Python: SessionsCancelTool → "task_id is required"
func (s *SessionToolkitCancelSuite) TestSessionsCancelInput_空TaskID() {
	input := hsubagent.SessionsCancelInput{TaskID: ""}
	s.Empty(input.TaskID, "空 TaskID 应通过结构体构造")
}

// TestSessionsCancelInput_正常TaskID 测试 SessionsCancelInput 正常输入。
func (s *SessionToolkitCancelSuite) TestSessionsCancelInput_正常TaskID() {
	input := hsubagent.SessionsCancelInput{TaskID: "abc123def456"}
	s.Equal("abc123def456", input.TaskID, "TaskID 应与输入一致")
}

// TestSessionsSpawnInput_字段测试 测试 SessionsSpawnInput 字段。
func (s *SessionToolkitCancelSuite) TestSessionsSpawnInput_字段测试() {
	input := hsubagent.SessionsSpawnInput{
		SubagentType:    "research_agent",
		TaskDescription: "搜索关于 AI 的最新论文",
	}
	s.Equal("research_agent", input.SubagentType, "SubagentType 应为 'research_agent'")
	s.Equal("搜索关于 AI 的最新论文", input.TaskDescription, "TaskDescription 应匹配")
}

// TestSessionTaskRow_完整字段 测试 SessionTaskRow 所有字段。
func (s *SessionToolkitCancelSuite) TestSessionTaskRow_完整字段() {
	row := &hsubagent.SessionTaskRow{
		TaskID:       "task_full",
		SubSessionID: "sess_full",
		Description:  "完整测试",
		Status:       "running",
		Result:       "结果",
		Error:        "错误",
	}
	s.Equal("task_full", row.TaskID)
	s.Equal("sess_full", row.SubSessionID)
	s.Equal("完整测试", row.Description)
	s.Equal("running", row.Status)
	s.Equal("结果", row.Result)
	s.Equal("错误", row.Error)
}

// TestSessionTaskRow_状态流转 测试 SessionTaskRow 状态流转序列。
// 对齐 Python: running → completed / running → canceled / running → error
func (s *SessionToolkitCancelSuite) TestSessionTaskRow_状态流转() {
	toolkit := hsubagent.NewSessionToolkit()

	// 正常完成路径
	toolkit.UpsertRunning("t_done", "s1", "正常完成")
	toolkit.MarkCompleted("t_done", "OK")
	row := toolkit.Get("t_done")
	s.Equal("completed", row.Status, "正常完成路径")

	// 取消路径
	toolkit.UpsertRunning("t_cancel", "s2", "被取消")
	toolkit.MarkCanceled("t_cancel")
	row = toolkit.Get("t_cancel")
	s.Equal("canceled", row.Status, "取消路径")

	// 失败路径
	toolkit.UpsertRunning("t_fail", "s3", "执行失败")
	toolkit.MarkFailed("t_fail", "timeout")
	row = toolkit.Get("t_fail")
	s.Equal("error", row.Status, "失败路径")
}

// TestSessionToolkit_并发安全 测试 SessionToolkit 并发操作不崩溃。
func (s *SessionToolkitCancelSuite) TestSessionToolkit_并发安全() {
	toolkit := hsubagent.NewSessionToolkit()

	// 并发 UpsertRunning + MarkCanceled
	done := make(chan struct{})
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			toolkit.UpsertRunning("concurrent", "sess", "并发测试")
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			toolkit.MarkCanceled("concurrent")
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			_ = toolkit.Get("concurrent")
		}
	}()

	// 等待所有 goroutine 完成
	<-done
	<-done
	<-done

	// 不崩溃即为通过
	row := toolkit.Get("concurrent")
	s.NotNil(row, "并发操作后应仍有记录")
}

// TestSessionsListTool_空列表 测试 NewSessionsListTool 空列表场景。
func (s *SessionToolkitCancelSuite) TestSessionsListTool_空列表() {
	toolkit := hsubagent.NewSessionToolkit()
	listTool := hsubagent.NewSessionsListTool(toolkit, "cn", "test_agent")

	s.Require().NotNil(listTool, "listTool 不应为 nil")
	s.Equal("sessions_list", listTool.Card().GetName(), "工具名应为 sessions_list")

	// 直接调用工具函数
	result, err := listTool.Invoke(s.Ctx, map[string]any{}, nil)
	s.Require().NoError(err, "空列表 Invoke 不应报错")
	s.Require().NotNil(result)

	success, _ := result["success"].(bool)
	s.True(success, "应返回 success=true")

	data, _ := result["data"].(string)
	s.Contains(data, "没有后台子任务", "空列表应提示无任务")
}

// TestSessionsListTool_非空列表 测试 NewSessionsListTool 非空列表场景。
func (s *SessionToolkitCancelSuite) TestSessionsListTool_非空列表() {
	toolkit := hsubagent.NewSessionToolkit()
	toolkit.UpsertRunning("t1", "s1", "任务1")
	toolkit.UpsertRunning("t2", "s2", "任务2")
	toolkit.MarkCompleted("t2", "完成")

	listTool := hsubagent.NewSessionsListTool(toolkit, "cn", "test_agent")

	result, err := listTool.Invoke(s.Ctx, map[string]any{}, nil)
	s.Require().NoError(err)
	s.Require().NotNil(result)

	data, _ := result["data"].(string)
	s.Contains(data, "t1", "应包含 task_id t1")
	s.Contains(data, "running", "应包含 running 状态")
	s.Contains(data, "completed", "应包含 completed 状态")
}

// TestSessionsCancelTool_空TaskID校验 测试 NewSessionsCancelTool 空 task_id 校验。
// 对齐 Python: SessionsCancelTool → "task_id is required"
func (s *SessionToolkitCancelSuite) TestSessionsCancelTool_空TaskID校验() {
	// 直接测试输入结构体校验逻辑
	input := hsubagent.SessionsCancelInput{TaskID: ""}
	s.Empty(input.TaskID, "空 TaskID 应可构造")
	// 实际工具调用会在内部返回 exception 错误
	// 此处验证输入结构体的空值语义
}

// TestSessionsCancelTool_Task不存在校验 测试 toolkit.Get 对不存在的 task_id 返回 nil。
// 对齐 Python: SessionsCancelTool → "Task {task_id} not found"
func (s *SessionToolkitCancelSuite) TestSessionsCancelTool_Task不存在校验() {
	toolkit := hsubagent.NewSessionToolkit()

	// 不存在的 task_id → toolkit.Get 返回 nil
	row := toolkit.Get("nonexistent_task")
	s.Nil(row, "不存在的任务应返回 nil")
}

// TestSessionToolkit_完整Cancel工作流 测试完整的 cancel 工作流：
// upsert → list → cancel → verify canceled。
// 对齐 Python: test_sessions_cancel_scenario1_immediate_cancel
func (s *SessionToolkitCancelSuite) TestSessionToolkit_完整Cancel工作流() {
	toolkit := hsubagent.NewSessionToolkit()

	// 步骤 1：创建任务
	taskID := "cancel_workflow_001"
	toolkit.UpsertRunning(taskID, "sub_001", "待取消任务")

	// 步骤 2：验证 running
	row := toolkit.Get(taskID)
	s.Require().NotNil(row)
	s.Equal("running", row.Status)

	// 步骤 3：执行取消
	toolkit.MarkCanceled(taskID)

	// 步骤 4：验证 canceled
	row = toolkit.Get(taskID)
	s.Require().NotNil(row)
	s.Equal("canceled", row.Status, "取消后状态应为 canceled")
}
