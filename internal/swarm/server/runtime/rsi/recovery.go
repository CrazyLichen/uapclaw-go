package rsi

// RsiWorkspaceRecovery：进程重启后的持久任务生命周期对账。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/recovery.py（140 行）。
// 内存 worker 随进程丢失后只对账，不重排队、不重启引擎。

import (
	"strings"
)

// providerTerminalStatus Provider 快照终态 → 服务状态映射（含拼写变体）。
var providerTerminalStatus = map[string]TaskStatus{
	"COMPLETED":  TaskStatusCompleted,
	"FAILED":     TaskStatusFailed,
	"TERMINATED": TaskStatusTerminated,
	"CANCELLED":  TaskStatusTerminated,
	"CANCELED":   TaskStatusTerminated,
}

// ──────────────────────────── 结构体 ────────────────────────────

// WorkspaceRecovery 重启对账器。
type WorkspaceRecovery struct {
	store           *TaskStore
	projector       *Projector
	adapterResolver func(scenario, artifactType string) ProviderStateReader
}

// ProviderStateReader adapter 的 read_state 缝（对齐 Python getattr(adapter,"read_state")）。
type ProviderStateReader interface {
	// ReadState 读任务盘上快照（state.json）；无快照返回 nil。
	ReadState(taskID string) map[string]any
}

// RecoverySummary 对账摘要（字段逐字对齐 Python recover() 返回）。
type RecoverySummary struct {
	// Scanned 活跃态扫描数。
	Scanned int `json:"scanned"`
	// Recovered 对账成功数。
	Recovered int `json:"recovered"`
	// Paused 转暂停数。
	Paused int `json:"paused"`
	// Completed 收敛完成数。
	Completed int `json:"completed"`
	// Failed 收敛失败数。
	Failed int `json:"failed"`
	// Terminated 收敛终止数。
	Terminated int `json:"terminated"`
	// Errors 单任务损坏隔离明细。
	Errors []map[string]string `json:"errors"`
}

// NewWorkspaceRecovery 构造对账器。
func NewWorkspaceRecovery(store *TaskStore, projector *Projector,
	adapterResolver func(scenario, artifactType string) ProviderStateReader) *WorkspaceRecovery {
	return &WorkspaceRecovery{store: store, projector: projector, adapterResolver: adapterResolver}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Recover 对账全部持久任务：仅收敛状态，不重新入队、不调用引擎。
func (r *WorkspaceRecovery) Recover() RecoverySummary {
	summary := RecoverySummary{Errors: []map[string]string{}}
	for _, task := range r.store.List("", "") {
		if task.Status != string(TaskStatusQueued) && task.Status != string(TaskStatusRunning) {
			continue
		}
		summary.Scanned++
		// 单任务损坏隔离：失败不影响整体（对齐 Python try/except per task）。
		r.projector.LoadFromDisk(task.TaskID)
		r.projector.RegisterRoot(task.TaskID, nil, "基线")
		target, cause := r.targetStatus(&task)
		if _, err := r.store.UpdateStatus(task.TaskID, []string{task.Status}, target, cause); err != nil {
			summary.Errors = append(summary.Errors, map[string]string{
				"task_id": task.TaskID, "error": err.Error(),
			})
			continue
		}
		summary.Recovered++
		switch TaskStatus(target) {
		case TaskStatusPaused:
			summary.Paused++
		case TaskStatusCompleted:
			summary.Completed++
		case TaskStatusFailed:
			summary.Failed++
		case TaskStatusTerminated:
			summary.Terminated++
		}
	}
	return summary
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// targetStatus 单任务对账目标（对齐 Python _target_status 全分支）：
// QUEUED → PAUSED（队列随进程丢失）；RUNNING → 读 Provider 盘上快照，
// 有终态收敛为终态（FAILED 带失败原因），读不到/非终态 → PAUSED。
func (r *WorkspaceRecovery) targetStatus(task *RsiTask) (string, string) {
	if task.Status == string(TaskStatusQueued) {
		return string(TaskStatusPaused), "agentserver_restart.queue_lost"
	}
	artifactType := ptrStr(task.ArtifactType)
	adapter := r.adapterResolver(task.Scenario, artifactType)
	state := readProviderState(adapter, task.TaskID)
	providerStatus := providerStatusOf(state)
	terminal, ok := providerTerminalStatus[providerStatus]
	if ok {
		if terminal == TaskStatusFailed {
			if reason := FailureReason(state); reason != "" {
				return string(terminal), reason
			}
		}
		return string(terminal), "provider_snapshot." + strings.ToLower(string(terminal))
	}
	return string(TaskStatusPaused), "agentserver_restart.execution_detached"
}

// readProviderState 读 Provider 快照：reader 缺失/异常/nil 一律 nil（best effort）。
func readProviderState(adapter ProviderStateReader, taskID string) map[string]any {
	if adapter == nil {
		return nil
	}
	return adapter.ReadState(taskID)
}

// providerStatusOf 快照状态归一：TASK_NOT_FOUND 错误码视作无快照；
// status 大写化去空白（对齐 Python _provider_status）。
func providerStatusOf(state map[string]any) string {
	if state == nil {
		return ""
	}
	if strings.ToUpper(valueToString(state["error_code"])) == "TASK_NOT_FOUND" {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(valueToString(state["status"])))
}
