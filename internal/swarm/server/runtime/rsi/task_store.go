package rsi

// RsiTaskStore：任务存储 + 状态机（内部 v3 §4.1 + 一致性规则 §8）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/task_store.py（231 行）。
//
// - JSON 文件存储：``.uapclawswarm/rsi/tasks/<task_id>/task.json``
//   （内部 v3 §1 边界：不新增 DB）。
// - 状态机唯一入口 UpdateStatus(fromStates, to, cause)；非法迁移报
//   TASK_STATE_CONFLICT。
// - P1 钩子：成功迁移后触发 onStatusChanged(task_id, old, new)（服务侧
//   权威，不依赖引擎事件），锁外执行。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// statusTransitions 合法状态迁移表（内部 v3 §6）。key=当前状态；value=允许迁移到的状态。
var statusTransitions = map[TaskStatus]map[TaskStatus]bool{
	TaskStatusCreated: {TaskStatusQueued: true, TaskStatusTerminated: true},
	TaskStatusQueued:  {TaskStatusRunning: true, TaskStatusPaused: true, TaskStatusTerminated: true},
	TaskStatusRunning: {TaskStatusCompleted: true, TaskStatusFailed: true, TaskStatusPaused: true, TaskStatusTerminated: true},
	TaskStatusPaused:  {TaskStatusQueued: true, TaskStatusTerminated: true},
	// 三个终态封闭。
	TaskStatusCompleted:  {},
	TaskStatusFailed:     {},
	TaskStatusTerminated: {},
}

// nonDeletableStates 不可删除的状态（一致性规则 §8.2）；
// 排队/暂停任务由服务层先转为 TERMINATED。
var nonDeletableStates = map[TaskStatus]bool{TaskStatusRunning: true, TaskStatusPaused: true}

// storeLock 进程内文件互斥（对齐 Python 模块级 RLock，D-9-5）。
var storeLock sync.Mutex

// ──────────────────────────── 结构体 ────────────────────────────

// TaskStore 任务存储 + 状态机（JSON 文件，进程内锁保护）。
type TaskStore struct {
	// tasksRoot 任务根目录。
	tasksRoot string
	// onStatusChanged P1 状态钩子（SetStatusChangedCallback 可替换）。
	onStatusChanged func(taskID, oldStatus, newStatus string)
}

// NewTaskStore 构造存储（tasksRoot 不存在则创建）；onStatusChanged 可为 nil。
func NewTaskStore(tasksRoot string, onStatusChanged func(taskID, oldStatus, newStatus string)) *TaskStore {
	if err := os.MkdirAll(tasksRoot, 0o755); err != nil {
		// Python mkdir 同样在构造期发生异常；Go 侧以 panic 显式暴露配置错误。
		panic(fmt.Sprintf("rsi task store 根目录创建失败: %v", err))
	}
	return &TaskStore{tasksRoot: tasksRoot, onStatusChanged: onStatusChanged}
}

// SetStatusChangedCallback 公开注入状态变更 P1 钩子。
func (s *TaskStore) SetStatusChangedCallback(callback func(taskID, oldStatus, newStatus string)) {
	s.onStatusChanged = callback
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskDir 任务目录路径。
func TaskDir(tasksRoot, taskID string) string {
	return filepath.Join(tasksRoot, taskID)
}

// TasksRoot 返回任务根目录（provider/装配根使用）。
func (s *TaskStore) TasksRoot() string {
	return s.tasksRoot
}

// Create 写盘新任务（状态恒 CREATED；task_id 由服务层生成后传入）。
func (s *TaskStore) Create(task RsiTask) (RsiTask, error) {
	if task.TaskID == "" {
		return RsiTask{}, errors.New("task_id must be provided by service layer")
	}
	taskDir := TaskDir(s.tasksRoot, task.TaskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return RsiTask{}, err
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	if err := writeTask(taskDir, task); err != nil {
		return RsiTask{}, err
	}
	return task, nil
}

// Get 读取任务；不存在报 TASK_NOT_FOUND。
func (s *TaskStore) Get(taskID string) (RsiTask, error) {
	payload, err := s.readTask(taskID)
	if err != nil {
		return RsiTask{}, err
	}
	if payload == nil {
		return RsiTask{}, NewTaskNotFound(taskID)
	}
	return *payload, nil
}

// GetView 读取任务并生成 adapter 只读视图。
func (s *TaskStore) GetView(taskID string) (RsiTaskView, error) {
	task, err := s.Get(taskID)
	if err != nil {
		return RsiTaskView{}, err
	}
	return task.ToTaskView(), nil
}

// List 纯存储投影（web §6.2）。scenario/artifactType 可选过滤（空串不过滤）。
func (s *TaskStore) List(scenario, artifactType string) []RsiTask {
	tasks := []RsiTask{}
	storeLock.Lock()
	defer storeLock.Unlock()
	entries, err := os.ReadDir(s.tasksRoot)
	if err != nil {
		return tasks
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		payload := readTaskDir(TaskDir(s.tasksRoot, name))
		if payload == nil {
			continue
		}
		if scenario != "" && payload.Scenario != scenario {
			continue
		}
		if artifactType != "" && ptrStr(payload.ArtifactType) != artifactType {
			continue
		}
		tasks = append(tasks, *payload)
	}
	// 按 created_at 逆序（对齐 Python sort reverse=True）。
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt > tasks[j].CreatedAt })
	return tasks
}

// Delete 删除任务：先校验守卫，再尽力清理物理目录。
// 一致性规则 §8.2 仍禁止删除运行中/暂停/在用产物；排队/暂停任务由服务层先
// 通过状态机转为 TERMINATED；task.json 缺失视为删除成功（幂等）。
func (s *TaskStore) Delete(taskID string, forbidRunning, forbidActiveArtifact bool) error {
	task, err := s.Get(taskID)
	if err != nil {
		return err
	}
	state := TaskStatus(task.Status)
	if forbidRunning && nonDeletableStates[state] {
		return NewTaskStateConflict(fmt.Sprintf("任务 %s 状态 %s 不可删除", taskID, state))
	}
	if forbidActiveArtifact {
		active, _ := task.Config["harness_refs_path"].(string)
		if active != "" {
			// 快照已消费（发布 refs）时仍可删：仅当该 refs 仍被标记在用才拦截。
			// 当前无反向注册表，借助 config.active_ref_released 标志（由
			// download/装载路径置位）。
			if task.Config["active_ref_released"] != true {
				return NewTaskStateConflict(fmt.Sprintf("任务 %s 产物仍在生效，不可删除", taskID))
			}
		}
	}
	taskDir := TaskDir(s.tasksRoot, taskID)
	storeLock.Lock()
	defer storeLock.Unlock()
	// 索引删除失败继续上抛；物理清理失败直接忽略（对齐 Python）。
	_ = os.Remove(filepath.Join(taskDir, "task.json"))
	_ = os.RemoveAll(taskDir)
	return nil
}

// UpdateStatus 状态机唯一入口（内部 v3 §4.1）。
//
//   - fromStates 约束合法迁移；当前状态不在其中 → TASK_STATE_CONFLICT。
//   - 成功迁移后 P1 钩子 onStatusChanged(task_id, old, new)（锁外执行）。
func (s *TaskStore) UpdateStatus(taskID string, fromStates []string, toState string, cause string) (RsiTask, error) {
	taskDir := TaskDir(s.tasksRoot, taskID)
	storeLock.Lock()
	current, err := s.Get(taskID) // 锁内重读
	if err != nil {
		storeLock.Unlock()
		return RsiTask{}, err
	}
	old := current.Status
	if !containsStr(fromStates, old) {
		storeLock.Unlock()
		return RsiTask{}, NewTaskStateConflict(fmt.Sprintf(
			"任务 %s 状态 %s 不允许迁移到 %s（允许来源: %v）", taskID, old, toState, sortedCopy(fromStates)))
	}
	if !statusTransitions[TaskStatus(old)][TaskStatus(toState)] {
		storeLock.Unlock()
		return RsiTask{}, NewTaskStateConflict(fmt.Sprintf("任务 %s 状态 %s 不允许迁移到 %s", taskID, old, toState))
	}
	current.Status = toState
	now := UtcnowISO()
	current.UpdatedAt = &now
	if cause != "" {
		if current.StatusHistory == nil {
			current.StatusHistory = []map[string]any{}
		}
		current.StatusHistory = append(current.StatusHistory, map[string]any{
			"from": old, "to": toState, "cause": cause, "ts": now,
		})
	}
	if err := writeTask(taskDir, current); err != nil {
		storeLock.Unlock()
		return RsiTask{}, err
	}
	storeLock.Unlock()
	// P1 钩子放在锁外执行（回调不应持有文件锁）。
	if s.onStatusChanged != nil {
		s.onStatusChanged(taskID, old, toState)
	}
	return current, nil
}

// MergeResults 锁内合并引擎结果到 task.json.config.results（worker 落盘唯一入口）。
func (s *TaskStore) MergeResults(taskID string, results map[string]any) error {
	taskDir := TaskDir(s.tasksRoot, taskID)
	storeLock.Lock()
	defer storeLock.Unlock()
	current, err := s.Get(taskID)
	if err != nil {
		return err
	}
	if current.Config == nil {
		current.Config = map[string]any{}
	}
	existing, _ := current.Config["results"].(map[string]any)
	merged := map[string]any{}
	for k, v := range existing {
		merged[k] = v
	}
	// 对齐 Python：None 值不合并。
	for k, v := range results {
		if v != nil {
			merged[k] = v
		}
	}
	current.Config["results"] = merged
	now := UtcnowISO()
	current.UpdatedAt = &now
	return writeTask(taskDir, current)
}

// MergeConfig 合并服务侧 config 元数据，不替换其他字段。
func (s *TaskStore) MergeConfig(taskID string, values map[string]any) error {
	if values == nil {
		return errors.New("config values must be a mapping")
	}
	taskDir := TaskDir(s.tasksRoot, taskID)
	storeLock.Lock()
	defer storeLock.Unlock()
	current, err := s.Get(taskID)
	if err != nil {
		return err
	}
	if current.Config == nil {
		current.Config = map[string]any{}
	}
	for k, v := range values {
		current.Config[k] = v
	}
	now := UtcnowISO()
	current.UpdatedAt = &now
	return writeTask(taskDir, current)
}

// MarkActiveRefReleased 下载/装载路径消费产物后置位：允许后续 delete。
func (s *TaskStore) MarkActiveRefReleased(taskID string) error {
	if _, err := s.Get(taskID); err != nil {
		return err
	}
	return s.MergeConfig(taskID, map[string]any{"active_ref_released": true})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readTask 读取单任务 task.json；文件缺失返回 nil 不报错。
func (s *TaskStore) readTask(taskID string) (*RsiTask, error) {
	dir := TaskDir(s.tasksRoot, taskID)
	payload := readTaskDir(dir)
	if payload == nil {
		return nil, nil
	}
	return payload, nil
}

// readTaskDir 读取任务目录内 task.json；缺失/损坏返回 nil（对齐 Python 吞错）。
func readTaskDir(taskDir string) *RsiTask {
	data, err := os.ReadFile(filepath.Join(taskDir, "task.json"))
	if err != nil {
		return nil
	}
	var task RsiTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil
	}
	return &task
}

// writeTask 原子写 task.json：先写 .json.tmp 再替换（对齐 Python tmp.replace）。
func writeTask(taskDir string, task RsiTask) error {
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(taskDir, "task.json.tmp")
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(taskDir, "task.json"))
}

// containsStr 列表包含。
func containsStr(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// sortedCopy 排序副本（错误文案对齐 Python sorted(from_states)）。
func sortedCopy(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}

// ptrStr 取指针字符串值（nil → ""）。
func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
