package subagent

import (
	"sync"
)

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
