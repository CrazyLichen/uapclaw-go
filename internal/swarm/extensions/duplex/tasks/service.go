package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskModification 一次修订请求。
// 对齐 tasks/service.py:21-24 TaskModification。
type TaskModification struct {
	// Revision 期望的任务版本
	Revision int
	// Instruction 新指令
	Instruction string
}

// Executor 注入的 Agent 执行器（鸭子类型 → 窄接口，偏差 O-3）。
type Executor interface {
	// Run 执行一个任务；progress 上报进度条目
	Run(ctx context.Context, task map[string]any, progress func(map[string]any) error) (map[string]any, error)
	// Cancel 取消执行
	Cancel(ctx context.Context, task map[string]any) error
	// Answer 向仍在输出的执行投递答案
	Answer(ctx context.Context, task map[string]any) error
	// Close 关闭执行器（可选能力，对齐 getattr(self.executor,"close",None)）
	Close() error
}

// TaskService 注入 Agent 执行器的持久用户任务服务（独立于媒体/RPC）。
// 对齐 tasks/service.py:34-792 TaskService。
type TaskService struct {
	store              *TaskStore
	executor           Executor
	onChange           func(map[string]any) error
	concurrency        int
	notificationTimout time.Duration

	writeMu sync.Mutex // 服务级写互斥：恢复 Python 单线程 asyncio 的变更串行语义（偏差 O-9）

	workers              map[string]*workerEntry
	cancellations        map[string]*workerEntry
	notifications        map[string]*workerEntry
	pendingNotifications map[string]map[string]any

	closed  bool
	started bool
	leaseFd int

	mu sync.Mutex
}

// workerEntry 一个后台协程的取消入口。
// Python 侧无对应实现：asyncio.Task → goroutine + cancel func + token。
type workerEntry struct {
	token  int64
	cancel context.CancelFunc
	done   chan struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskService 构造服务（不立即启动）。
// 对齐 tasks/service.py:35-48 __init__。
func NewTaskService(store *TaskStore, executor Executor, onChange func(map[string]any) error, concurrency int, notificationTimeout time.Duration) (*TaskService, error) {
	if concurrency < 1 {
		return nil, errors.New("Task concurrency must be a positive integer")
	}
	return &TaskService{
		store:                store,
		executor:             executor,
		onChange:             onChange,
		concurrency:          concurrency,
		notificationTimout:   notificationTimeout,
		workers:              map[string]*workerEntry{},
		cancellations:        map[string]*workerEntry{},
		notifications:        map[string]*workerEntry{},
		pendingNotifications: map[string]map[string]any{},
		leaseFd:              -1,
	}, nil
}

// Start 抢租约并恢复中断任务。
// 对齐 tasks/service.py:50-77 start。
func (s *TaskService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("Task service is closed")
	}
	if s.started {
		return nil
	}
	// 偏差 D-1：portalocker.Lock(timeout=0) → syscall.Flock 非阻塞独占
	fd, err := openLeaseFile(s.store.Path + ".owner")
	if err != nil {
		return err
	}
	if err := tryLockLease(fd); err != nil {
		_ = closeLease(fd)
		return errors.New("another task service owns this ledger")
	}
	s.leaseFd = fd
	err = s.writeTx(func(db *sqlTx) error {
		rows, err := txRows(db, nil, nil)
		if err != nil {
			return err
		}
		for _, task := range rows {
			status := toString(task["status"])
			resumeAnswer := truthy(task["resume_answer"])
			needsReset := status == "running" || status == "cancelling" || status == "waiting_user" ||
				(status == "queued" && resumeAnswer)
			if !needsReset {
				continue
			}
			task["status"] = "unknown"
			task["error"] = "Execution ownership lost; tools were not replayed"
			if interaction, ok := task["interaction"].(map[string]any); ok {
				interaction["state"] = "expired"
			}
			changes, _ := task["changes"].([]any)
			for _, c := range changes {
				change, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if change["state"] == "claimed" {
					change["state"] = "unknown"
				}
			}
			task["sequence"] = numAdd(task["sequence"], 1)
			if err := txPut(db, task); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = unlockLease(s.leaseFd)
		_ = closeLease(s.leaseFd)
		s.leaseFd = -1
		return err
	}
	s.started = true
	s.kickLocked()
	return nil
}

// Kick 就绪任务补位调度。
// 对齐 tasks/service.py:79-113 kick。
func (s *TaskService) Kick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.started {
		return
	}
	s.kickLocked()
}

// Submit 提交任务（幂等命令）。
// 对齐 tasks/service.py:192-245 submit。
func (s *TaskService) Submit(owner, session, commandID, instruction string, request map[string]any) (map[string]any, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	// None 表 owner 为空 → 空作用域精确持久化；非字符串仍非法（对齐 :196-198 注释）
	if owner == "" && request == nil {
		return nil, errors.New("Task owner must be a string and conversation is required")
	}
	if session == "" {
		return nil, errors.New("Task owner must be a string and conversation is required")
	}
	if strings.TrimSpace(instruction) == "" {
		return nil, errors.New("Task instruction is required")
	}
	if len(instruction) > 16000 {
		return nil, errors.New("Task instruction exceeds 16000 characters")
	}
	if request == nil {
		request = map[string]any{}
	}
	if _, ok := request["independent"].(bool); !ok {
		if request["independent"] != nil {
			return nil, errors.New("independent must be a boolean")
		}
		request["independent"] = false
	}
	for _, key := range []string{"depends_on", "resources"} {
		items, ok := request[key].([]any)
		if !ok && request[key] != nil {
			return nil, fmt.Errorf("Invalid %s", key)
		}
		if ok && len(items) > 32 {
			return nil, fmt.Errorf("Invalid %s", key)
		}
		for _, item := range items {
			v, isStr := item.(string)
			if !isStr || strings.TrimSpace(v) == "" || len(v) > 256 {
				return nil, fmt.Errorf("Invalid %s", key)
			}
		}
	}
	if _, present := request["resources"]; present {
		set := map[string]bool{}
		normalized := []string{}
		for _, v := range request["resources"].([]any) {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v.(string)), "\\", "/"))
			if !set[key] {
				set[key] = true
				normalized = append(normalized, key)
			}
		}
		sort.Strings(normalized)
		anyList := make([]any, 0, len(normalized))
		for _, v := range normalized {
			anyList = append(anyList, v)
		}
		request["resources"] = anyList
	}
	var result map[string]any
	var kicked bool
	err := s.writeTx(func(db *sqlTx) error {
		replay, fingerprint, err := txReplay(db, owner, session, commandID,
			[]any{"submit", instruction, request})
		if err != nil {
			return err
		}
		if replay != nil {
			task, err := txGet(db, toString(replay["task_id"]), owner, session)
			if err != nil {
				return err
			}
			task["reused"] = true
			result = task
			return nil
		}
		for _, dependency := range toStringSlice(request["depends_on"]) {
			if _, err := txGet(db, dependency, owner, session); err != nil {
				return err
			}
		}
		task := s.newTask(owner, session, strings.TrimSpace(instruction), request, "")
		if err := txPut(db, task); err != nil {
			return err
		}
		if err := txCommand(db, task, commandID, fingerprint,
			map[string]any{"task_id": task["id"], "state": "accepted"}); err != nil {
			return err
		}
		result = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	kicked = s.started && !s.closed
	s.mu.Unlock()
	if kicked {
		s.Kick()
	}
	return result, nil
}

// Get 读取单任务。
// 对齐 tasks/service.py:247-249 get。
func (s *TaskService) Get(owner, session, taskID string) (map[string]any, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	return s.store.read(taskID, owner, session)
}

// List 分页列出任务。
// 对齐 tasks/service.py:251-263 list。
func (s *TaskService) List(owner, session string, query, status string, offset, limit int) ([]map[string]any, any, error) {
	if err := s.Start(); err != nil {
		return nil, nil, err
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, nil, errors.New("Invalid pagination")
	}
	var tasks []map[string]any
	if err := s.writeTx(func(db *sqlTx) error {
		var err error
		tasks, err = txRows(db, owner, session)
		return err
	}); err != nil {
		return nil, nil, err
	}
	if status != "" {
		filtered := tasks[:0]
		for _, task := range tasks {
			if toString(task["status"]) == status {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}
	if query != "" {
		queryLower := strings.ToLower(query)
		filtered := tasks[:0]
		for _, task := range tasks {
			if strings.Contains(strings.ToLower(toString(task["instruction"])), queryLower) {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}
	end := offset + limit
	var next any
	if len(tasks) > end {
		next = end
	}
	if offset > len(tasks) {
		offset = len(tasks)
	}
	if end > len(tasks) {
		end = len(tasks)
	}
	return tasks[offset:end], next, nil
}

// Modify 修订任务需求。
// 对齐 tasks/service.py:265-316 modify。
func (s *TaskService) Modify(owner, session, taskID, commandID string, change TaskModification) (map[string]any, error) {
	revision, instruction := change.Revision, change.Instruction
	if err := s.Start(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(instruction) == "" || len(instruction) > 4000 {
		return nil, errors.New("Modification must contain 1–4000 characters")
	}
	var receipt map[string]any
	err := s.writeTx(func(db *sqlTx) error {
		task, err := txGet(db, taskID, owner, session)
		if err != nil {
			return err
		}
		replay, fingerprint, err := txReplay(db, owner, session, commandID,
			[]any{"modify", taskID, revision, instruction})
		if err != nil {
			return err
		}
		if replay != nil {
			receipt = replay
			return nil
		}
		if revision != numInt(task["revision"]) || task["successor_id"] != nil {
			return fmt.Errorf("Task revision changed; query the current task: %w", TaskRevisionConflict)
		}
		status := toString(task["status"])
		if status != "queued" && status != "running" && status != "completed" {
			return errors.New("This task cannot accept modifications")
		}
		entry := map[string]any{"id": commandID, "instruction": strings.TrimSpace(instruction), "state": "pending"}
		if status == "queued" {
			// 与派发认领同一事务：准入处无丢失更新（对齐 :292 注释）
			if len(toString(task["instruction"]))+len(instruction) > 15970 {
				return errors.New("Accumulated task instruction exceeds 16000 characters")
			}
			task["instruction"] = toString(task["instruction"]) + "\nUser modification: " + strings.TrimSpace(instruction)
			entry["state"] = "queued_input_updated"
		} else if status == "completed" {
			child, err := s.successor(db, task, strings.TrimSpace(instruction))
			if err != nil {
				return err
			}
			entry["state"] = "followup"
			entry["successor_id"] = child["id"]
		}
		task["changes"] = append(toAnySlice(task["changes"]), entry)
		task["revision"] = numAdd(task["revision"], 1)
		task["sequence"] = numAdd(task["sequence"], 1)
		if err := txPut(db, task); err != nil {
			return err
		}
		receipt = map[string]any{
			"task_id": taskID, "operation_id": commandID, "state": entry["state"],
			"revision": numInt(task["revision"]),
		}
		if sid, ok := entry["successor_id"]; ok {
			receipt["successor_id"] = sid
		}
		return txCommand(db, task, commandID, fingerprint, receipt)
	})
	if err != nil {
		return nil, err
	}
	s.Kick()
	return receipt, nil
}

// Cancel 取消任务（两阶段：受理 → 执行停止确认）。
// 对齐 tasks/service.py:345-420 cancel。
func (s *TaskService) Cancel(ctx context.Context, owner, session, taskID, commandID string) (map[string]any, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	var receipt map[string]any
	var cancelling bool
	err := s.writeTx(func(db *sqlTx) error {
		task, err := txGet(db, taskID, owner, session)
		if err != nil {
			return err
		}
		replay, fingerprint, err := txReplay(db, owner, session, commandID, []any{"cancel", taskID})
		if err != nil {
			return err
		}
		if replay != nil {
			receipt = replay
			return nil
		}
		status := toString(task["status"])
		if status == "queued" && !truthy(task["resume_answer"]) {
			task["status"] = "cancelled"
		} else if !isTerminal(status) {
			task["status"] = "cancelling"
		}
		task["revision"] = numAdd(task["revision"], 1)
		task["sequence"] = numAdd(task["sequence"], 1)
		changes, _ := task["changes"].([]any)
		for _, c := range changes {
			change, ok := c.(map[string]any)
			if ok && change["state"] == "pending" {
				change["state"] = "rejected"
			}
		}
		if interaction, ok := task["interaction"].(map[string]any); ok {
			interaction["state"] = "expired"
		}
		newStatus := toString(task["status"])
		if err := txPut(db, task); err != nil {
			return err
		}
		message := "任务已经结束，未执行新的取消操作。"
		if newStatus == "cancelling" {
			message = "取消已受理，正在等待执行停止；尚未确认停止。"
		} else if newStatus == "cancelled" {
			message = "任务已取消。"
		}
		receipt = map[string]any{
			"task_id": taskID, "operation_id": commandID,
			"state":       map[bool]string{true: "cancelling", false: newStatus}[newStatus == "cancelling"],
			"task_status": newStatus,
			"stopped":     newStatus == "cancelled",
			"message":     message,
		}
		if err := txCommand(db, task, commandID, fingerprint, receipt); err != nil {
			return err
		}
		cancelling = newStatus == "cancelling"
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if cancelling {
		if _, exists := s.cancellations[taskID]; !exists {
			runCtx, cancel := context.WithCancel(context.Background())
			entry := &workerEntry{token: time.Now().UnixNano(), cancel: cancel, done: make(chan struct{})}
			s.cancellations[taskID] = entry
			task, _ := s.store.read(taskID, nil, nil)
			go func() {
				defer close(entry.done)
				defer func() {
					s.mu.Lock()
					delete(s.cancellations, taskID)
					s.mu.Unlock()
				}()
				s.cancelWork(runCtx, task)
			}()
		}
	}
	s.mu.Unlock()
	current, _ := s.store.read(taskID, nil, nil)
	if current != nil {
		_ = s.notify(current)
	}
	return receipt, nil
}

// Reorder 重排等待队列。
// 对齐 tasks/service.py:422-476 reorder。
func (s *TaskService) Reorder(owner, session, taskID string, revision int, beforeID, commandID string) (map[string]any, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	var receipt map[string]any
	err := s.writeTx(func(db *sqlTx) error {
		task, err := txGet(db, taskID, owner, session)
		if err != nil {
			return err
		}
		var fingerprint string
		if commandID != "" {
			replay, fp, err := txReplay(db, owner, session, commandID,
				[]any{"reorder", taskID, revision, beforeID})
			if err != nil {
				return err
			}
			if replay != nil {
				receipt = replay
				return nil
			}
			fingerprint = fp
		}
		if toString(task["status"]) != "queued" {
			return errors.New("Task is no longer waiting; running tasks cannot be reordered")
		}
		version, err := queueVersion(db, owner, session)
		if err != nil {
			return err
		}
		if version != revision {
			return fmt.Errorf("Queue changed; query before reordering: %w", QueueVersionConflict)
		}
		queued, err := txRows(db, owner, session)
		if err != nil {
			return err
		}
		waiting := []map[string]any{}
		for _, t := range queued {
			if toString(t["status"]) == "queued" {
				waiting = append(waiting, t)
			}
		}
		sort.Slice(waiting, func(i, j int) bool { return numInt(waiting[i]["position"]) < numInt(waiting[j]["position"]) })
		if beforeID != "" {
			found := false
			for _, t := range waiting {
				if toString(t["id"]) == beforeID {
					found = true
				}
			}
			if !found {
				return errors.New("Queue target is no longer waiting")
			}
		}
		var ordered []map[string]any
		if beforeID == taskID {
			ordered = []map[string]any{}
		} else {
			for _, t := range waiting {
				if toString(t["id"]) != taskID {
					ordered = append(ordered, t)
				}
			}
			index := 0
			for i, t := range ordered {
				if toString(t["id"]) == beforeID {
					index = i
					break
				}
			}
			// insert task at index
			ordered = append(ordered, nil)
			copy(ordered[index+1:], ordered[index:])
			ordered[index] = task
		}
		for index, item := range ordered {
			item["position"] = index
			item["revision"] = numAdd(item["revision"], 1)
			item["sequence"] = numAdd(item["sequence"], 1)
			if err := txPut(db, item); err != nil {
				return err
			}
		}
		newVersion, err := queueVersion(db, owner, session)
		if err != nil {
			return err
		}
		receipt = map[string]any{"task_id": taskID, "state": "reordered", "queue_version": newVersion}
		if commandID != "" {
			return txCommand(db, task, commandID, fingerprint, receipt)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Kick()
	return receipt, nil
}

// Answer 把答案绑定到确切的观察提问与执行。
// 对齐 tasks/service.py:482-545 answer。
func (s *TaskService) Answer(ctx context.Context, owner, session, taskID, commandID, interactionID string, answers []any, nativeUI bool) (map[string]any, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	var receipt map[string]any
	var suspended, needExecutorAnswer bool
	var executorTask map[string]any
	err := s.writeTx(func(db *sqlTx) error {
		task, err := txGet(db, taskID, owner, session)
		if err != nil {
			return err
		}
		replay, fingerprint, err := txReplay(db, owner, session, commandID,
			[]any{"answer", taskID, interactionID, answers})
		if err != nil {
			return err
		}
		if replay != nil {
			receipt = replay
			return nil
		}
		interaction, _ := task["interaction"].(map[string]any)
		if toString(task["status"]) != "waiting_user" ||
			toString(interaction["state"]) != "pending" ||
			toString(interaction["id"]) != interactionID {
			return errors.New("Question is no longer pending for this task")
		}
		source := toString(interaction["source"])
		_, isNative := NATIVEApprovalSources[source]
		if isNative {
			if !nativeUI {
				return errors.New("Approval requires the native user interface")
			}
			validated, err := ValidateNativeApprovalAnswers(interaction, answers)
			if err != nil {
				return err
			}
			answers = toAnySliceFromMaps(validated)
		} else {
			validated, err := ValidateAnswers(interaction, answers)
			if err != nil {
				return err
			}
			answers = toAnySliceFromMaps(validated)
		}
		interaction["state"] = "accepted"
		interaction["answers"] = answers
		interaction["operation_id"] = commandID
		suspended = source == "ask_user_interrupt" || isNative
		if suspended && truthy(task["output_closed"]) && truthy(task["execution_settled"]) {
			// 保留挂起的执行身份直到派发胜过取消（对齐 :477-479 注释）
			task["status"] = "queued"
			task["resume_answer"] = true
		} else if !suspended {
			if truthy(task["output_closed"]) {
				return errors.New("The original answer channel has closed")
			}
			task["status"] = "running"
		}
		task["sequence"] = numAdd(task["sequence"], 1)
		if err := txPut(db, task); err != nil {
			return err
		}
		receipt = map[string]any{"task_id": taskID, "operation_id": commandID, "state": "accepted"}
		if err := txCommand(db, task, commandID, fingerprint, receipt); err != nil {
			return err
		}
		needExecutorAnswer = !suspended
		executorTask = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	if needExecutorAnswer {
		// 这是仍持有输出流的控制输入，不是新任务（对齐 :536 注释）
		if err := s.executor.Answer(ctx, executorTask); err != nil {
			errText := err.Error()
			_, _ = s.writeUpdate(taskID, func(current map[string]any) {
				if toString(current["status"]) == "running" &&
					toString(current["request_id"]) == toString(executorTask["request_id"]) {
					current["status"] = "unknown"
					current["error"] = errText
				}
			})
			return nil, err
		}
	}
	current, _ := s.store.read(taskID, nil, nil)
	if current != nil {
		_ = s.notify(current)
	}
	s.Kick()
	return receipt, nil
}

// Snapshot 任务快照与队列版本。
// 对齐 tasks/service.py:556-577 snapshot。
func (s *TaskService) Snapshot(owner, session string) ([]map[string]any, int, error) {
	if err := s.Start(); err != nil {
		return nil, 0, err
	}
	var tasks []map[string]any
	var version int
	err := s.writeTx(func(db *sqlTx) error {
		var err error
		tasks, err = txRows(db, owner, session)
		if err != nil {
			return err
		}
		allTasks, err := txRows(db, nil, nil)
		if err != nil {
			return err
		}
		byID := map[string]map[string]any{}
		for _, t := range allTasks {
			byID[toString(t["id"])] = t
		}
		for _, task := range tasks {
			if toString(task["status"]) != "queued" {
				continue
			}
			deps := []map[string]any{}
			for _, key := range toStringSlice(task["request"].(map[string]any)["depends_on"]) {
				if dep, ok := byID[key]; ok {
					deps = append(deps, dep)
				} else {
					deps = append(deps, map[string]any{})
				}
			}
			waitReason := "capacity_order_or_resource"
			anyFailed, anyPending := false, false
			for _, dep := range deps {
				ds := toString(dep["status"])
				if ds == "failed" || ds == "cancelled" {
					anyFailed = true
				}
				if ds != "completed" {
					anyPending = true
				}
			}
			if anyFailed {
				waitReason = "dependency_failed_or_cancelled"
			} else if anyPending {
				waitReason = "dependency_pending"
			}
			task["wait_reason"] = waitReason
		}
		version, err = queueVersion(db, owner, session)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return tasks, version, nil
}

// Preempt 抢占：重排到队首并取消唯一运行任务。
// 对齐 tasks/service.py:579-589 preempt。
func (s *TaskService) Preempt(ctx context.Context, owner, session, taskID string, revision int, commandID string) error {
	tasks, _, err := s.Snapshot(owner, session)
	if err != nil {
		return err
	}
	running := 0
	for _, t := range tasks {
		if toString(t["status"]) == "running" {
			running++
		}
	}
	if running > 1 {
		return errors.New("Multiple tasks are running; cancel an exact task instead")
	}
	if _, err := s.Reorder(owner, session, taskID, revision, "", ""); err != nil {
		return err
	}
	for _, active := range tasks {
		if toString(active["status"]) == "running" {
			_, err := s.Cancel(ctx, owner, session, toString(active["id"]), commandID)
			return err
		}
	}
	return nil
}

// Close 关停并释放租约。
// 对齐 tasks/service.py:792-806 close。
func (s *TaskService) Close() error {
	s.mu.Lock()
	s.closed = true
	pending := make([]*workerEntry, 0, len(s.workers)+len(s.cancellations)+len(s.notifications))
	for _, e := range s.workers {
		pending = append(pending, e)
	}
	for _, e := range s.cancellations {
		pending = append(pending, e)
	}
	for _, e := range s.notifications {
		pending = append(pending, e)
	}
	s.workers = map[string]*workerEntry{}
	s.pendingNotifications = map[string]map[string]any{}
	started := s.started
	leaseFd := s.leaseFd
	s.mu.Unlock()
	for _, entry := range pending {
		entry.cancel()
		<-entry.done
	}
	if s.executor != nil {
		_ = s.executor.Close()
	}
	if started && leaseFd >= 0 {
		_ = unlockLease(leaseFd)
		_ = closeLease(leaseFd)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kickLocked 就绪任务补位调度（须持锁调用）。
// 对齐 tasks/service.py:79-113 kick。
// 偏差 O-8：SQLite 读移出 s.mu（ReadDirect 直读），避免锁内 IO 与派发事务互相饥饿。
func (s *TaskService) kickLocked() {
	tasks, err := s.store.RowsDirect(nil, nil)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("任务快照读取失败")
		return
	}
	active := []map[string]any{}
	for _, task := range tasks {
		status := toString(task["status"])
		_, hasWorker := s.workers[toString(task["id"])]
		if status == "running" || status == "cancelling" || status == "unknown" || status == "waiting_user" || hasWorker {
			active = append(active, task)
		}
	}
	activeCount := 0
	for _, t := range active {
		status := toString(t["status"])
		if status != "unknown" && !(status == "waiting_user" && truthy(t["execution_settled"])) {
			activeCount++
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return numInt(tasks[i]["position"]) < numInt(tasks[j]["position"]) })
	for _, task := range tasks {
		if activeCount >= s.concurrency {
			break
		}
		taskID := toString(task["id"])
		if toString(task["status"]) != "queued" {
			continue
		}
		if _, hasWorker := s.workers[taskID]; hasWorker {
			continue
		}
		if !readySingle(task, tasks, active) {
			continue
		}
		runCtx, cancel := context.WithCancel(context.Background())
		entry := &workerEntry{token: time.Now().UnixNano(), cancel: cancel, done: make(chan struct{})}
		s.workers[taskID] = entry
		active = append(active, task)
		activeCount++
		capturedTask := task
		go func() {
			defer close(entry.done)
			defer func() {
				s.mu.Lock()
				delete(s.workers, taskID)
				s.mu.Unlock()
				// 观察失败：持久化的尝试保持 unknown 而非重放（对齐 :154-157 注释）
				s.Kick()
			}()
			s.drain(runCtx, capturedTask)
		}()
	}
}

// readySingle 单任务就绪判定。
// 对齐 tasks/service.py:116-152 _ready（静态方法 → 包内函数）。
func readySingle(task map[string]any, tasks, active []map[string]any) bool {
	request, _ := task["request"].(map[string]any)
	if request == nil {
		request = map[string]any{}
	}
	byID := map[string]map[string]any{}
	for _, t := range tasks {
		byID[toString(t["id"])] = t
	}
	for _, key := range toStringSlice(request["depends_on"]) {
		dep, ok := byID[key]
		if !ok || toString(dep["status"]) != "completed" {
			return false
		}
	}
	sameScope := func(t map[string]any) bool {
		return toString(t["owner"]) == toString(task["owner"]) && toString(t["session"]) == toString(task["session"])
	}
	inActive := func(t map[string]any) bool {
		for _, a := range active {
			if toString(a["id"]) == toString(t["id"]) {
				return true
			}
		}
		return false
	}
	// 保存的会话独占一份 Agent/上下文/输出租约，独立任务也不例外
	if strings.HasPrefix(toString(task["session"]), "task-duplex:") {
		for _, t := range tasks {
			if sameScope(t) && toString(t["id"]) != toString(task["id"]) && inActive(t) {
				return false
			}
		}
	}
	if !truthy(request["independent"]) {
		for _, t := range tasks {
			if sameScope(t) && toString(t["id"]) != toString(task["id"]) &&
				(inActive(t) || (numInt(t["position"]) < numInt(task["position"]) && !isTerminal(toString(t["status"])))) {
				return false
			}
		}
	}
	// 缺声明 = 未知资源，不是已证实的冲突（对齐 :142 注释）
	resources := map[string]bool{}
	for _, r := range toStringSlice(request["resources"]) {
		resources[r] = true
	}
	for _, other := range active {
		held := map[string]bool{}
		otherReq, _ := other["request"].(map[string]any)
		for _, r := range toStringSlice(otherReq["resources"]) {
			held[r] = true
		}
		if len(resources) == 0 || len(held) == 0 {
			continue
		}
		if resources["*"] || held["*"] || setsIntersect(resources, held) {
			return false
		}
	}
	return true
}

// newTask 构造新任务体。
// 对齐 tasks/service.py:162-190 _new（静态方法 → 包内函数）。
func (s *TaskService) newTask(owner, session, instruction string, request map[string]any, parent string) map[string]any {
	taskID := newHexID()
	coreSessionID := "managed-task-" + taskID
	if strings.HasPrefix(session, "task-duplex:") {
		coreSessionID = strings.TrimPrefix(session, "task-duplex:")
	}
	return map[string]any{
		"id": taskID, "owner": owner, "session": session,
		"instruction": instruction, "request": request,
		"status": "queued", "revision": 1, "sequence": 1,
		"position":        time.Now().UnixNano(),
		"created_at":      float64(time.Now().UnixMicro()) / 1e6,
		"core_session_id": coreSessionID,
		"request_id":      "task-" + newHexID(),
		"changes":         []any{}, "progress": []any{},
		"result": nil, "error": "",
		"parent_id": parent, "successor_id": nil,
		"checkpoint_open": false, "execution_settled": false,
		"execution_cancelled": false, "execution_bound": false, "output_closed": false,
	}
}

// successor 为已完成的任务创建修订后继任务。
// 对齐 tasks/service.py:318-343 _successor。
func (s *TaskService) successor(db *sqlTx, task map[string]any, instruction string) (map[string]any, error) {
	if task["successor_id"] != nil {
		return txGet(db, toString(task["successor_id"]), nil, nil)
	}
	adopted := []string{}
	changes, _ := task["changes"].([]any)
	for _, c := range changes {
		change, ok := c.(map[string]any)
		if !ok {
			continue
		}
		state := toString(change["state"])
		if state == "context_written" || state == "model_input_observed" {
			adopted = append(adopted, toString(change["instruction"]))
		}
	}
	request := copyMap(toMap(task["request"]))
	request["prior_result"] = task["result"]
	request["depends_on"] = []any{task["id"]}
	// 后继是新执行，不是原 call_id 的重放（对齐 :335 注释）
	for _, key := range []string{"tool_call_id", "turn_id", "frame_data_url"} {
		delete(request, key)
	}
	child := s.newTask(toString(task["owner"]), toString(task["session"]),
		toString(task["instruction"])+"\n"+strings.Join(adopted, "\n")+"\nUser revision: "+instruction,
		request, toString(task["id"]))
	task["successor_id"] = child["id"]
	if err := txPut(db, child); err != nil {
		return nil, err
	}
	return child, nil
}

// cancelWork 执行取消直至确认停止。
// 对齐 tasks/service.py:385-419 _cancel。
func (s *TaskService) cancelWork(ctx context.Context, task map[string]any) {
	var current map[string]any
	err := s.executor.Cancel(ctx, task)
	if err == nil {
		current, _ = s.store.read(toString(task["id"]), nil, nil)
		if truthy(current["execution_cancelled"]) && truthy(current["execution_settled"]) {
			s.mu.Lock()
			observer := s.workers[toString(task["id"])]
			s.mu.Unlock()
			if observer != nil {
				observer.cancel()
				<-observer.done
			}
		}
		// 让输出观察者发布任何赢得竞态的完成
		for i := 0; i < 600; i++ {
			current, _ = s.store.ReadDirect(toString(task["id"]), nil, nil)
			if truthy(current["output_closed"]) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		current, _ = s.store.ReadDirect(toString(task["id"]), nil, nil)
		if !truthy(current["execution_settled"]) {
			err = errors.New("Stop accepted; execution settlement is not confirmed")
		}
	}
	if err == nil {
		current, _ = s.writeUpdate(toString(task["id"]), func(t map[string]any) {
			if toString(t["status"]) == "cancelling" {
				t["status"] = "cancelled"
				t["error"] = ""
			}
		})
	} else {
		message := err.Error()
		if message == "" {
			message = "error"
		}
		logger.Warn(logComponent).Err(err).Str("task_id", toString(task["id"])).Msg("任务取消执行失败")
		current, _ = s.writeUpdate(toString(task["id"]), func(t map[string]any) {
			if toString(t["status"]) == "cancelling" {
				t["error"] = message
			}
		})
	}
	if current != nil {
		_ = s.notify(current)
	}
	s.Kick()
}

// queueVersion 队列版本（调度事实的不透明 token）。
// 对齐 tasks/service.py:547-554 queue_version。
func queueVersion(db *sqlTx, owner, session any) (int, error) {
	rows, err := txRows(db, owner, session)
	if err != nil {
		return 0, err
	}
	facts := []any{}
	for _, t := range rows {
		request, _ := t["request"].(map[string]any)
		if request == nil {
			request = map[string]any{}
		}
		facts = append(facts, []any{
			t["id"], t["status"], t["position"],
			request["independent"], request["depends_on"], request["resources"],
		})
	}
	factsJSON, _ := json.Marshal(facts)
	sum := sha256Hex(string(factsJSON))
	value := 0
	for i := 0; i < 12; i++ {
		value = value*16 + int(hexDigit(sum[i]))
	}
	return value, nil
}

// notify 投递变更通知（每任务一个发送者与最新快照）。
// 对齐 tasks/service.py:591-600 _notify。
func (s *TaskService) notify(task map[string]any) error {
	s.mu.Lock()
	if s.onChange == nil || s.closed {
		s.mu.Unlock()
		return nil
	}
	key := toString(task["id"])
	s.pendingNotifications[key] = task
	_, running := s.notifications[key]
	if !running {
		runCtx, cancel := context.WithCancel(context.Background())
		entry := &workerEntry{token: time.Now().UnixNano(), cancel: cancel, done: make(chan struct{})}
		s.notifications[key] = entry
		go func() {
			defer close(entry.done)
			defer func() {
				s.mu.Lock()
				delete(s.notifications, key)
				s.mu.Unlock()
			}()
			s.sendNotifications(runCtx, key)
		}()
	}
	s.mu.Unlock()
	return nil
}

// sendNotifications 通知发送循环（失败重试上限 3 次）。
// 对齐 tasks/service.py:601-622 _send_notifications。
func (s *TaskService) sendNotifications(ctx context.Context, key string) {
	failures := 0
	for {
		s.mu.Lock()
		task, ok := s.pendingNotifications[key]
		if ok {
			delete(s.pendingNotifications, key)
		}
		onChange := s.onChange
		timeout := s.notificationTimout
		closed := s.closed
		s.mu.Unlock()
		if !ok || closed {
			return
		}
		delivered := false
		if onChange != nil {
			done := make(chan error, 1)
			go func() { done <- onChange(task) }()
			select {
			case <-done:
				delivered = true
			case <-time.After(timeout):
				// 超时
			case <-ctx.Done():
				return
			}
		} else {
			delivered = true
		}
		if delivered {
			failures = 0
		} else {
			s.mu.Lock()
			_, newer := s.pendingNotifications[key]
			s.mu.Unlock()
			// 投递期间来了新快照则优先新快照（对齐 :608 注释）
			if newer {
				failures = 0
			} else {
				failures++
				if failures < 3 {
					s.mu.Lock()
					s.pendingNotifications[key] = task
					s.mu.Unlock()
				} else {
					logger.Warn(logComponent).Str("task_id", key).Msg("Task notification retries exhausted")
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// drain 派发执行直至终态。
// 对齐 tasks/service.py:624-790 _drain。
func (s *TaskService) drain(ctx context.Context, admitted map[string]any) {
	taskID := toString(admitted["id"])
	var task map[string]any
	err := s.writeTx(func(db *sqlTx) error {
		current, err := txGet(db, taskID, nil, nil)
		if err != nil {
			return err
		}
		tasks, err := txRows(db, nil, nil)
		if err != nil {
			return err
		}
		active := []map[string]any{}
		for _, other := range tasks {
			if toString(other["id"]) == taskID {
				continue
			}
			status := toString(other["status"])
			if status == "running" || status == "cancelling" || status == "unknown" || status == "waiting_user" {
				active = append(active, other)
			}
		}
		if toString(current["status"]) != "queued" || !readySingle(current, tasks, active) {
			return errSkipDrain
		}
		// 准入后复查：排队中的取消/修订/重排可能先于本执行生效（对齐 :625 注释）
		for _, t := range tasks {
			if toString(t["status"]) == "queued" &&
				numInt(t["position"]) < numInt(current["position"]) &&
				toString(t["owner"]) == toString(current["owner"]) &&
				toString(t["session"]) == toString(current["session"]) &&
				readySingle(t, tasks, active) {
				return errSkipDrain
			}
		}
		if truthy(current["resume_answer"]) {
			// 一个业务任务可能在多轮中断里产出文件；派发下一个执行 ID 前保留每个已落定的请求
			prior, _ := current["prior_request_ids"].([]any)
			current["prior_request_ids"] = append(prior, current["request_id"])
			current["request_id"] = "task-" + newHexID()
			current["execution_bound"] = false
			current["execution_settled"] = false
			current["execution_cancelled"] = false
			current["output_closed"] = false
			if interaction, ok := current["interaction"].(map[string]any); ok {
				interaction["state"] = "submitted"
			}
		}
		current["status"] = "running"
		current["checkpoint_open"] = true
		current["sequence"] = numAdd(current["sequence"], 1)
		progress, _ := current["progress"].([]any)
		current["progress"] = append(progress, map[string]any{
			"stage": "started", "title": "Agent execution started", "status": "running",
			"sequence": len(progress) + 1, "timestamp": float64(time.Now().UnixMicro()) / 1e6,
		})
		return txPut(db, current)
	})
	if err == errSkipDrain {
		return
	}
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("任务派发前置事务失败")
		return
	}
	task, _ = s.store.read(taskID, nil, nil)
	if task == nil {
		return
	}
	_ = s.notify(task)

	// 进度持久化闭包（对齐 :672-717 persist_progress/progress/flush_progress）
	var reasoning map[string]any
	lastFlush := time.Now()
	var progressMu sync.Mutex
	persistProgress := func(entry map[string]any) {
		requestID := toString(task["request_id"])
		_, _ = s.writeUpdate(taskID, func(current map[string]any) {
			if toString(current["request_id"]) != requestID {
				return
			}
			status := toString(current["status"])
			if status != "running" && status != "waiting_user" && status != "cancelling" {
				return
			}
			if entry["interaction"] != nil && status != "cancelling" {
				interaction := toMap(entry["interaction"])
				old, _ := current["interaction"].(map[string]any)
				if toString(old["request_id"]) != toString(interaction["request_id"]) ||
					toString(old["execution_request_id"]) != requestID {
					interaction["id"] = newHexID()
					interaction["execution_request_id"] = requestID
					current["interaction"] = interaction
					current["status"] = "waiting_user"
				}
			}
			progress, _ := current["progress"].([]any)
			merged := copyMap(entry)
			merged["sequence"] = len(progress) + 1
			merged["timestamp"] = float64(time.Now().UnixMicro()) / 1e6
			current["progress"] = append(progress, merged)
		})
	}
	flushProgress := func() {
		progressMu.Lock()
		defer progressMu.Unlock()
		if reasoning != nil {
			entry := reasoning
			reasoning = nil
			persistProgress(entry)
		}
		lastFlush = time.Now()
	}
	progressFn := func(entry map[string]any) error {
		if toString(entry["stage"]) == "reasoning" {
			progressMu.Lock()
			if reasoning == nil {
				reasoning = copyMap(entry)
			} else {
				reasoning["content"] = toString(reasoning["content"]) + toString(entry["content"])
			}
			shouldFlush := time.Since(lastFlush) >= 250*time.Millisecond ||
				len(toString(reasoning["content"])) >= 4096
			progressMu.Unlock()
			if shouldFlush {
				flushProgress()
			}
			return nil
		}
		flushProgress()
		persistProgress(entry)
		return nil
	}

	outcome, result, errMsg := "completed", map[string]any(nil), ""
	runErr := func() (runErr error) {
		defer RecoverDuplexError(&runErr)
		result, runErr = s.executor.Run(ctx, task, progressFn)
		return runErr
	}()
	if runErr != nil {
		var pending *InteractionPending
		var uncertain *ExecutionUncertain
		switch {
		case errors.As(runErr, &pending):
			outcome = "waiting_user"
		case errors.As(runErr, &uncertain):
			outcome, errMsg = "unknown", runErr.Error()
		case errors.Is(runErr, context.Canceled):
			outcome, errMsg = "unknown", "Execution detached; result is unknown"
		default:
			outcome, errMsg = "failed", runErr.Error()
		}
	}
	flushProgress()
	var current map[string]any
	_ = s.writeTx(func(db *sqlTx) error {
		current, _ = txGet(db, taskID, nil, nil)
		if current == nil {
			return nil
		}
		current["checkpoint_open"] = false
		current["output_closed"] = true
		status := toString(current["status"])
		if truthy(current["execution_cancelled"]) && truthy(current["execution_settled"]) {
			// 取消冲刷可能携带 chat.final/部分文本；确切执行的停止事实优先（对齐 :726-728 注释）
			current["status"] = "cancelled"
			current["result"] = nil
			current["partial_result"] = result
			current["error"] = ""
		} else if status == "running" || status == "waiting_user" {
			current["status"] = outcome
			if result != nil {
				current["result"] = result
			}
			current["error"] = errMsg
		} else if status == "cancelling" && result != nil {
			// 完成赢得了竞态；不丢弃实际结果（对齐 :733 注释）
			current["status"] = "completed"
			current["result"] = result
			current["error"] = ""
		}
		currentStatus := toString(current["status"])
		if currentStatus == "waiting_user" {
			interaction, _ := current["interaction"].(map[string]any)
			if interaction != nil && toString(interaction["state"]) == "accepted" &&
				truthy(current["execution_settled"]) {
				current["status"] = "queued"
				current["resume_answer"] = true
			}
		} else if current["interaction"] != nil && (isTerminal(currentStatus) || currentStatus == "unknown") {
			toMap(current["interaction"])["state"] = "expired"
		}
		var pendingChanges []map[string]any
		changes, _ := current["changes"].([]any)
		for _, c := range changes {
			change, ok := c.(map[string]any)
			if ok && toString(change["state"]) == "pending" {
				pendingChanges = append(pendingChanges, change)
			}
		}
		if len(pendingChanges) > 0 && toString(current["status"]) == "completed" {
			instructions := []string{}
			for _, change := range pendingChanges {
				instructions = append(instructions, toString(change["instruction"]))
			}
			child, err := s.successor(db, current, strings.Join(instructions, "\n"))
			if err == nil {
				for _, change := range pendingChanges {
					change["state"] = "followup"
					change["successor_id"] = child["id"]
				}
			}
		}
		changes, _ = current["changes"].([]any)
		for _, c := range changes {
			change, ok := c.(map[string]any)
			if !ok {
				continue
			}
			state := toString(change["state"])
			if state == "claimed" {
				change["state"] = "unknown"
			} else if state == "pending" {
				cs := toString(current["status"])
				if cs != "waiting_user" && cs != "queued" {
					change["state"] = "rejected"
				}
			}
		}
		current["sequence"] = numAdd(current["sequence"], 1)
		return txPut(db, current)
	})
	if current != nil {
		_ = s.notify(current)
	}
}

// errSkipDrain 准入后复查未通过。
// Python 侧无对应实现：Python return 即退出，Go 需哨兵错误。
var errSkipDrain = errors.New("drain skipped")

// isTerminal 终态判定。
// 对齐 tasks/service.py:18 TERMINAL。
func isTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

// LockedDelete 持锁场景外的删除辅助（调用方须已持锁）。
// Python 侧无对应实现。
func (s *TaskService) LockedDelete(m map[string]*workerEntry, key string) {
	delete(m, key)
}

// ──────────────────────────── 通用辅助 ────────────────────────────

// toAnySliceFromMaps map 切片 → any 切片。
func toAnySliceFromMaps(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// truthy truth 判定（JSON 往返 bool）。
func truthy(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

// hexDigit 十六进制字符 → 数值。
func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return 0
}

// setsIntersect 集合相交。
func setsIntersect(a, b map[string]bool) bool {
	for key := range a {
		if b[key] {
			return true
		}
	}
	return false
}

// newHexID 32 位 hex 标识。
// 对齐 uuid.uuid4().hex。
func newHexID() string {
	var b [16]byte
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (uint(i) % 8) * 8)
		b[i] ^= byte(os.Getpid() + i)
	}
	return fmt.Sprintf("%x%x%x%x%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// sqlTx database/sql 事务别名（store 的 tx 形参类型）。
type sqlTx = sql.Tx

var _ = math.Round

// toString any → string（非字符串回空）。
func toString(value any) string {
	s, _ := value.(string)
	return s
}

// toStringSlice any → []string（缺失回空）。
func toStringSlice(value any) []string {
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, toString(item))
	}
	return out
}

// toAnySlice any → []any。
func toAnySlice(value any) []any {
	list, _ := value.([]any)
	return list
}

// toMap any → map（nil 安全）。
func toMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

// copyMap 浅拷贝。
func copyMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}

// RecoverDuplexError 在执行边界捕获 panic(error)。
// Python 侧无对应实现：等价 Python 异常传播语义（duplex 包同名函数的任务包镜像）。
func RecoverDuplexError(errPtr *error) {
	if r := recover(); r != nil {
		if e, ok := r.(error); ok {
			*errPtr = e
			return
		}
		panic(r)
	}
}

// writeTx 持服务写互斥执行事务。
// Python 侧无对应实现：Python 单线程 asyncio 天然串行变更，Go 显式互斥等价（偏差 O-9）。
func (s *TaskService) writeTx(fn func(db *sqlTx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.store.transaction(fn)
}

// writeUpdate 持服务写互斥执行任务变更。
func (s *TaskService) writeUpdate(taskID string, change func(map[string]any)) (map[string]any, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.store.update(taskID, change)
}
