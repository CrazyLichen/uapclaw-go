package subagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 接口 ────────────────────────────

// SessionToolProvider 会话工具所需的 Agent 能力接口。
// 仅声明工具运行时所需的方法子集，避免 subagent → task_loop 循环依赖。
// task_loop.DeepAgentProvider 隐式满足此接口。
type SessionToolProvider interface {
	// DeepConfig 返回 DeepAgent 配置
	DeepConfig() *hschema.DeepAgentConfig
	// EventHandler 返回事件处理器
	EventHandler() modules.EventHandler
}

// ──────────────────────────── 结构体 ────────────────────────────

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

// SessionsListTool 查看所有后台异步子任务的工具。
// 对齐 Python: SessionsListTool
type SessionsListTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// toolkit 会话任务注册表
	toolkit *SessionToolkit
	// language 语言
	language string
}

// SessionsSpawnTool 创建异步后台子代理任务的工具。
// 对齐 Python: SessionsSpawnTool
type SessionsSpawnTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// provider 深层 Agent 提供者
	provider SessionToolProvider
	// toolkit 会话任务注册表
	toolkit *SessionToolkit
	// language 语言
	language string
}

// SessionsCancelTool 取消后台异步子任务的工具。
// 对齐 Python: SessionsCancelTool
type SessionsCancelTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// provider 深层 Agent 提供者
	provider SessionToolProvider
	// toolkit 会话任务注册表
	toolkit *SessionToolkit
	// language 语言
	language string
}

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
func NewSessionsListTool(toolkit *SessionToolkit, language string) *SessionsListTool {
	desc := (&tools.SessionsListMetadataProvider{}).GetDescription(language)
	card := tool.NewToolCard("sessions_list", desc, buildSessionsListInputParams(), nil)
	return &SessionsListTool{card: card, toolkit: toolkit, language: language}
}

// Card 返回工具卡片。
func (t *SessionsListTool) Card() *tool.ToolCard { return t.card }

// Invoke 列出所有会话子任务。
// 对齐 Python: SessionsListTool.invoke
func (t *SessionsListTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	lines := make([]string, 0)
	tasks := t.toolkit.ListAll()
	for _, task := range tasks {
		lines = append(lines, fmt.Sprintf(
			"task_id=%s | description=%s | status=%s | result=%s | error=%s",
			task.TaskID, task.Description, task.Status, task.Result, task.Error,
		))
	}

	var data string
	if len(lines) > 0 {
		data = joinLines(lines)
	} else {
		if t.language == "cn" {
			data = "当前会话没有后台子任务"
		} else {
			data = "No background tasks for this session"
		}
	}

	return map[string]any{"success": true, "data": data}, nil
}

// Stream 不支持流式调用。
func (t *SessionsListTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported("sessions_list")
}

// NewSessionsSpawnTool 创建异步子代理派生工具。
// 对齐 Python: SessionsSpawnTool.__init__
func NewSessionsSpawnTool(provider SessionToolProvider, toolkit *SessionToolkit, language, availableAgents string) *SessionsSpawnTool {
	desc := (&tools.SessionsSpawnMetadataProvider{}).GetDescription(language)
	if availableAgents != "" {
		desc = fmt.Sprintf(desc, availableAgents)
	}
	card := tool.NewToolCard("sessions_spawn", desc, buildSessionsSpawnInputParams(), nil)
	return &SessionsSpawnTool{card: card, provider: provider, toolkit: toolkit, language: language}
}

// Card 返回工具卡片。
func (t *SessionsSpawnTool) Card() *tool.ToolCard { return t.card }

// Invoke 提交异步子代理任务。
// 对齐 Python: SessionsSpawnTool.invoke
func (t *SessionsSpawnTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 步骤 1：校验 enable_task_loop
	dc := t.provider.DeepConfig()
	if dc == nil || !dc.EnableTaskLoop {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", "enable_task_loop is required for session spawn"),
		)
	}

	// 步骤 2：获取 EventHandler 和 TaskManager
	handler := t.provider.EventHandler()
	if handler == nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", "event_handler not available"),
		)
	}
	tm := handler.GetBase().TaskManager
	if tm == nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", "task_manager not available"),
		)
	}

	// 步骤 3：提取输入参数
	subagentType, _ := inputs["subagent_type"].(string)
	taskDescription, _ := inputs["task_description"].(string)

	// 步骤 4：生成 task_id 和 sub_session_id
	taskID := uuid.New().String()
	var session any
	for _, opt := range opts {
		if opt != nil {
			o := tool.NewToolCallOptions(opt)
			if o.Session != nil {
				session = o.Session
			}
		}
	}
	parentSessionID := ""
	if session != nil {
		if sess, ok := session.(interface{ GetSessionID() string }); ok {
			parentSessionID = sess.GetSessionID()
		}
	}
	subSessionID := fmt.Sprintf("%s_sub_%s", parentSessionID, generateTokenHex(4))

	// 步骤 5：添加任务到 TaskManager
	coreTask := &cschema.Task{
		SessionID:  parentSessionID,
		TaskID:     taskID,
		TaskType:   hschema.SessionSpawnTaskType,
		Description: taskDescription,
		Status:     cschema.TaskSubmitted,
		Metadata: map[string]any{
			"subagent_type":   subagentType,
			"task_description": taskDescription,
			"sub_session_id":  subSessionID,
		},
	}
	if err := tm.AddTask(ctx, coreTask); err != nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", fmt.Sprintf("add_task failed: %s", err.Error())),
		)
	}

	// 步骤 6：更新 SessionToolkit
	t.toolkit.UpsertRunning(taskID, subSessionID, taskDescription)

	// 步骤 7：日志
	logger.Info(logComponent).
		Str("task_id", taskID).
		Str("sub_session_id", subSessionID).
		Str("subagent_type", subagentType).
		Msg("SessionsSpawnTool 提交异步子任务")

	// 步骤 8：返回结果
	var message string
	if t.language == "cn" {
		message = fmt.Sprintf("子任务 %s 已提交后台执行，你可以继续发送其他问题", taskDescription)
	} else {
		message = fmt.Sprintf("Task %s submitted to background, you can continue to send other questions", taskDescription)
	}

	return map[string]any{
		"success": true,
		"status":  "pending",
		"message": message,
	}, nil
}

// Stream 不支持流式调用。
func (t *SessionsSpawnTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported("sessions_spawn")
}

// NewSessionsCancelTool 创建取消子代理任务工具。
// 对齐 Python: SessionsCancelTool.__init__
func NewSessionsCancelTool(provider SessionToolProvider, toolkit *SessionToolkit, language string) *SessionsCancelTool {
	desc := (&tools.SessionsCancelMetadataProvider{}).GetDescription(language)
	card := tool.NewToolCard("sessions_cancel", desc, buildSessionsCancelInputParams(), nil)
	return &SessionsCancelTool{card: card, provider: provider, toolkit: toolkit, language: language}
}

// Card 返回工具卡片。
func (t *SessionsCancelTool) Card() *tool.ToolCard { return t.card }

// Invoke 取消异步子代理任务。
// 对齐 Python: SessionsCancelTool.invoke
func (t *SessionsCancelTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	// 步骤 1：提取 task_id
	taskID, _ := inputs["task_id"].(string)
	if taskID == "" {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", "task_id is required"),
		)
	}

	// 步骤 2：校验任务存在于 toolkit
	task := t.toolkit.Get(taskID)
	if task == nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", fmt.Sprintf("Task %s not found", taskID)),
		)
	}

	// 步骤 3：获取 TaskScheduler
	handler := t.provider.EventHandler()
	if handler == nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", "event_handler not available"),
		)
	}
	scheduler := handler.GetBase().TaskScheduler
	if scheduler == nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", "task_scheduler not available"),
		)
	}

	// 步骤 4：通过 scheduler 取消任务
	success, cancelErr := scheduler.CancelTask(ctx, taskID)
	if cancelErr != nil {
		return nil, exception.BuildError(
			exception.StatusToolSessionToolInvoked,
			exception.WithParam("reason", fmt.Sprintf("cancel_task failed: %s", cancelErr.Error())),
		)
	}
	if !success {
		var msg string
		if t.language == "cn" {
			msg = fmt.Sprintf("任务 %s 取消失败", taskID)
		} else {
			msg = fmt.Sprintf("Task %s cancel failed", taskID)
		}
		return map[string]any{
			"success": false,
			"task_id": taskID,
			"status":  task.Status,
			"message": msg,
		}, nil
	}

	// 步骤 5：更新 toolkit 状态
	t.toolkit.MarkCanceled(taskID)

	// 步骤 6：日志
	logger.Info(logComponent).
		Str("task_id", taskID).
		Msg("SessionsCancelTool 取消任务成功")

	// 步骤 7：返回结果
	var msg string
	if t.language == "cn" {
		msg = fmt.Sprintf("任务 %s 取消成功", taskID)
	} else {
		msg = fmt.Sprintf("Task %s cancel success", taskID)
	}
	return map[string]any{
		"success": true,
		"task_id": taskID,
		"status":  "canceled",
		"message": msg,
	}, nil
}

// Stream 不支持流式调用。
func (t *SessionsCancelTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported("sessions_cancel")
}

// BuildSessionTools 构建会话工具列表（list, spawn, cancel）。
// 对齐 Python: build_session_tools
func BuildSessionTools(
	provider SessionToolProvider,
	toolkit *SessionToolkit,
	language string,
	availableAgents string,
) []tool.Tool {
	return []tool.Tool{
		NewSessionsListTool(toolkit, language),
		NewSessionsSpawnTool(provider, toolkit, language, availableAgents),
		NewSessionsCancelTool(provider, toolkit, language),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSessionsListInputParams 构建 sessions_list 工具的输入参数。
// sessions_list 无必需参数。
func buildSessionsListInputParams() []*commonschema.Param {
	return []*commonschema.Param{}
}

// buildSessionsSpawnInputParams 构建 sessions_spawn 工具的输入参数。
// 对齐 Python: SessionsSpawnTool 的 subagent_type + task_description 参数。
func buildSessionsSpawnInputParams() []*commonschema.Param {
	return []*commonschema.Param{
		commonschema.NewStringParam("subagent_type", "子 agent 类型(如 'general-purpose')", true),
		commonschema.NewStringParam("task_description", "任务描述", true),
	}
}

// buildSessionsCancelInputParams 构建 sessions_cancel 工具的输入参数。
// 对齐 Python: SessionsCancelTool 的 task_id 参数。
func buildSessionsCancelInputParams() []*commonschema.Param {
	return []*commonschema.Param{
		commonschema.NewStringParam("task_id", "要取消的任务 ID", true),
	}
}

// generateTokenHex 生成指定字节数的随机十六进制字符串。
// 对齐 Python: secrets.token_hex(n)
func generateTokenHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// joinLines 将多行文本用换行符连接。
func joinLines(lines []string) string {
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}
	return result
}

// 编译时接口检查
var (
	_ tool.Tool = (*SessionsListTool)(nil)
	_ tool.Tool = (*SessionsSpawnTool)(nil)
	_ tool.Tool = (*SessionsCancelTool)(nil)
)
