package subagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/google/uuid"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionsListInput sessions_list 工具的输入参数（无参数）。
type SessionsListInput struct{}

// SessionsSpawnInput sessions_spawn 工具的输入参数。
type SessionsSpawnInput struct {
	// SubagentType 子 agent 类型
	SubagentType string `json:"subagent_type"`
	// TaskDescription 任务描述
	TaskDescription string `json:"task_description"`
}

// SessionsCancelInput sessions_cancel 工具的输入参数。
type SessionsCancelInput struct {
	// TaskID 要取消的任务 ID
	TaskID string `json:"task_id"`
}

// SessionTaskRow 会话任务行（业务视图）。
// 对齐 Python: SessionTaskRow
type SessionTaskRow struct {
	// TaskID 任务标识
	TaskID string `json:"task_id"`
	// SubSessionID 子会话标识
	SubSessionID string `json:"sub_session_id"`
	// Description 任务描述
	Description string `json:"description"`
	// Status 任务状态
	Status string `json:"status"`
	// Result 执行结果
	Result string `json:"result,omitempty"`
	// Error 错误信息
	Error string `json:"error,omitempty"`
}

// SessionToolkit 会话任务注册表，跟踪异步子任务状态。
// 对齐 Python: SessionToolkit
type SessionToolkit struct {
	// rows 任务行映射
	rows map[string]*SessionTaskRow
	// mu 读写互斥锁
	mu sync.RWMutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionToolkit 创建会话任务注册表。
func NewSessionToolkit() *SessionToolkit {
	return &SessionToolkit{
		rows: make(map[string]*SessionTaskRow),
	}
}

// UpsertRunning 插入或更新任务为运行中状态。
// 对齐 Python: SessionToolkit.upsert_running
func (t *SessionToolkit) UpsertRunning(taskID, subSessionID, description string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows[taskID] = &SessionTaskRow{
		TaskID:       taskID,
		SubSessionID: subSessionID,
		Description:  description,
		Status:       "running",
	}
}

// MarkCompleted 标记任务为已完成。
// 对齐 Python: SessionToolkit.mark_completed
func (t *SessionToolkit) MarkCompleted(taskID, result string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if row, ok := t.rows[taskID]; ok {
		row.Status = "completed"
		row.Result = result
	}
}

// MarkFailed 标记任务为已失败。
// 对齐 Python: SessionToolkit.mark_failed
func (t *SessionToolkit) MarkFailed(taskID, err string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if row, ok := t.rows[taskID]; ok {
		row.Status = "error"
		row.Error = err
	}
}

// MarkCanceled 标记任务为已取消。
// 对齐 Python: SessionToolkit.mark_canceled
func (t *SessionToolkit) MarkCanceled(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if row, ok := t.rows[taskID]; ok {
		row.Status = "canceled"
	}
}

// ListAll 返回所有任务行。
// 对齐 Python: SessionToolkit.list_all
func (t *SessionToolkit) ListAll() []*SessionTaskRow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]*SessionTaskRow, 0, len(t.rows))
	for _, row := range t.rows {
		result = append(result, row)
	}
	return result
}

// Get 按 ID 获取任务行。
// 对齐 Python: SessionToolkit.get
func (t *SessionToolkit) Get(taskID string) *SessionTaskRow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rows[taskID]
}

// Clear 清空所有任务行。
// 对齐 Python: SessionToolkit.clear
func (t *SessionToolkit) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = make(map[string]*SessionTaskRow)
}

// NewSessionsListTool 创建查看子任务列表工具。
// 对齐 Python: SessionsListTool.__init__
func NewSessionsListTool(toolkit *SessionToolkit, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("sessions_list", "sessions_list", language, nil, agentID)

	fn := func(_ context.Context, _ SessionsListInput, _ ...tool.ToolOption) (map[string]any, error) {
		lines := make([]string, 0)
		tasks := toolkit.ListAll()
		for _, task := range tasks {
			lines = append(lines, fmt.Sprintf(
				"task_id=%s | description=%s | status=%s | result=%s | error=%s",
				task.TaskID, task.Description, task.Status, task.Result, task.Error,
			))
		}

		var data string
		if len(lines) > 0 {
			data = joinLines(lines)
		} else if language == "cn" {
			data = "当前会话没有后台子任务"
		} else {
			data = "No background tasks for this session"
		}

		return map[string]any{"success": true, "data": data}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}

// NewSessionsSpawnTool 创建异步子代理派生工具。
// 对齐 Python: SessionsSpawnTool.__init__
func NewSessionsSpawnTool(provider interfaces.DeepAgentInterface, toolkit *SessionToolkit, language, availableAgents, agentID string) tool.Tool {
	var formatArgs map[string]string
	if availableAgents != "" {
		formatArgs = map[string]string{"available_agents": availableAgents}
	}
	card, _ := tools.BuildToolCard("sessions_spawn", "sessions_spawn", language, formatArgs, agentID)

	fn := func(ctx context.Context, input SessionsSpawnInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 步骤 1：校验 enable_task_loop
		dc := provider.DeepConfig()
		if dc == nil || !dc.EnableTaskLoop {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "会话 spawn 需要启用 task_loop"),
			)
		}

		// 步骤 2：获取 LoopController 和 TaskManager
		loopCtrl := provider.LoopController()
		if loopCtrl == nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "loop_controller 不可用"),
			)
		}
		tm := loopCtrl.TaskManager()
		if tm == nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "task_manager 不可用"),
			)
		}

		// 步骤 3：生成 task_id 和 sub_session_id
		taskID := uuid.New().String()
		callOpts := tool.NewToolCallOptions(opts...)
		session := callOpts.Session
		if session == nil {
			return nil, exception.BuildError(exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "SessionsSpawnTool 需要有效的会话"))
		}
		parentSessionID := ""
		if sess, ok := session.(interface{ GetSessionID() string }); ok {
			parentSessionID = sess.GetSessionID()
		}
		subSessionID := fmt.Sprintf("%s_sub_%s", parentSessionID, generateTokenHex(4))

		// 步骤 4：添加任务到 TaskManager
		coreTask := &cschema.Task{
			SessionID:   parentSessionID,
			TaskID:      taskID,
			TaskType:    hschema.SessionSpawnTaskType,
			Description: input.TaskDescription,
			Status:      cschema.TaskSubmitted,
			Metadata: map[string]any{
				"subagent_type":    input.SubagentType,
				"task_description": input.TaskDescription,
				"sub_session_id":   subSessionID,
			},
		}
		if err := tm.AddTask(ctx, coreTask); err != nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", fmt.Sprintf("添加任务失败: %s", err.Error())),
			)
		}

		// 步骤 5：更新 SessionToolkit
		toolkit.UpsertRunning(taskID, subSessionID, input.TaskDescription)

		// 步骤 6：日志
		logger.Info(logComponent).
			Str("task_id", taskID).
			Str("sub_session_id", subSessionID).
			Str("subagent_type", input.SubagentType).
			Msg("SessionsSpawnTool 提交异步子任务")

		// 步骤 7：返回结果（嵌套结构，对齐 Python: ToolOutput(success=True, data={...})）
		var message string
		if language == "cn" {
			message = fmt.Sprintf("子任务 %s 已提交后台执行，你可以继续发送其他问题", input.TaskDescription)
		} else {
			message = fmt.Sprintf("Task %s submitted to background, you can continue to send other questions", input.TaskDescription)
		}

		return map[string]any{
			"success": true,
			"data": map[string]any{
				"status":  "pending",
				"message": message,
			},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}

// NewSessionsCancelTool 创建取消子代理任务工具。
// 对齐 Python: SessionsCancelTool.__init__
func NewSessionsCancelTool(provider interfaces.DeepAgentInterface, toolkit *SessionToolkit, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("sessions_cancel", "sessions_cancel", language, nil, agentID)

	fn := func(ctx context.Context, input SessionsCancelInput, _ ...tool.ToolOption) (map[string]any, error) {
		// 步骤 1：校验 task_id 非空（对齐 Python: if not task_id）
		if input.TaskID == "" {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "task_id 为必填项"),
			)
		}

		// 步骤 2：校验任务存在于 toolkit
		task := toolkit.Get(input.TaskID)
		if task == nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", fmt.Sprintf("任务 %s 未找到", input.TaskID)),
			)
		}

		// 步骤 2：获取 LoopController 和 TaskScheduler
		loopCtrl := provider.LoopController()
		if loopCtrl == nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "loop_controller 不可用"),
			)
		}
		scheduler := loopCtrl.TaskScheduler()
		if scheduler == nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", "task_scheduler 不可用"),
			)
		}

		// 步骤 3：通过 scheduler 取消任务
		success, cancelErr := scheduler.CancelTask(ctx, input.TaskID)
		if cancelErr != nil {
			return nil, exception.BuildError(
				exception.StatusToolSessionToolInvoked,
				exception.WithParam("reason", fmt.Sprintf("取消任务失败: %s", cancelErr.Error())),
			)
		}
		if !success {
			var msg string
			if language == "cn" {
				msg = fmt.Sprintf("任务 %s 取消失败", input.TaskID)
			} else {
				msg = fmt.Sprintf("Task %s cancel failed", input.TaskID)
			}
			return map[string]any{
				"success": false,
				"data": map[string]any{
					"task_id": input.TaskID,
					"status":  task.Status,
					"message": msg,
				},
			}, nil
		}

		// 步骤 4：更新 toolkit 状态
		toolkit.MarkCanceled(input.TaskID)

		// 步骤 5：日志
		logger.Info(logComponent).
			Str("task_id", input.TaskID).
			Msg("SessionsCancelTool 取消任务成功")

		// 步骤 6：返回结果（嵌套结构，对齐 Python: ToolOutput(success=True, data={...})）
		var msg string
		if language == "cn" {
			msg = fmt.Sprintf("任务 %s 取消成功", input.TaskID)
		} else {
			msg = fmt.Sprintf("Task %s cancel success", input.TaskID)
		}
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"task_id": input.TaskID,
				"status":  "canceled",
				"message": msg,
			},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}

// BuildSessionTools 构建会话工具列表（list, spawn, cancel）。
// 对齐 Python: build_session_tools
func BuildSessionTools(
	provider interfaces.DeepAgentInterface,
	toolkit *SessionToolkit,
	language string,
	availableAgents string,
	agentID string,
) []tool.Tool {
	return []tool.Tool{
		NewSessionsListTool(toolkit, language, agentID),
		NewSessionsSpawnTool(provider, toolkit, language, availableAgents, agentID),
		NewSessionsCancelTool(provider, toolkit, language, agentID),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateTokenHex 生成指定字节数的随机十六进制字符串。
// 对齐 Python: secrets.token_hex(n)
func generateTokenHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// joinLines 将多行文本用换行符连接。
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}
	return result
}
