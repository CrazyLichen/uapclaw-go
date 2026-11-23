package rsi

// RsiWorker：单协程队列（并发=1）+ 事件链路装配（内部 v3 §4.2）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/worker.py（932 行）。
//
// - Enqueue：队列空 → RUNNING 直接启动；有运行任务 → QUEUED（全局并发=1）。
// - 启动时：adapter.build_request(task) → 装配事件链路（sink→有界队列→消费）
//   → adapter.run/resume；正常返回 → COMPLETED；异常 → FAILED。
// - 终态后排空最后事件再关消费协程。
// - cancel(pause/terminate)/resume：中优先级接口 + 状态机校验；
//   harness 无 terminate hook 时取消实际 runner（ctx cancel → 引擎取消分支）。

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
)

// ──────────────────────────── 常量 ────────────────────────────

// queueMaxSize 事件队列上限（对齐 Python _QUEUE_MAXSIZE）。
const queueMaxSize = 128

// providerPollInterval Provider 快照轮询间隔。
const providerPollInterval = 100 * time.Millisecond

// providerHandoffRetry 快照交接短重试间隔。
const providerHandoffRetry = 50 * time.Millisecond

// providerTerminateTimeout 超时清理 terminate 上限。
const providerTerminateTimeout = 5 * time.Second

// providerInProgress 快照未终态集合（对齐 Python _PROVIDER_IN_PROGRESS）。
var providerInProgress = map[string]bool{"CREATED": true, "QUEUED": true, "RUNNING": true}

// ──────────────────────────── 结构体 ────────────────────────────

// ScenarioAdapter worker 公共契约（HarnessEngineAdapter 满足）。
type ScenarioAdapter interface {
	ValidateInput(path *string, scenario string, artifactType *string) (RsiDatasetResult, error)
	BuildRequest(task RsiTaskView, resume bool) (HarnessEngineRequest, error)
	Run(ctx context.Context, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error)
	Resume(ctx context.Context, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error)
	Pause(taskID string) (map[string]any, error)
	Terminate(taskID string) (map[string]any, error)
	ReadState(taskID string) map[string]any
	ReadPublicationState(taskID string) map[string]any
	ReadReport(taskID string) map[string]any
	GetTree(taskID string) map[string]any
	LocateArtifact(taskID string, artifactID *string) map[string]any
	SupportsPause() bool
	SupportsResume() bool
	SupportsTerminate() bool
}

// Worker 全局单队列 worker（并发=1），跨场景共享。
type Worker struct {
	store           *TaskStore
	adapters        map[string]ScenarioAdapter
	usageRecorder   UsageRecorderSink
	projector       *Projector
	artifactService ArtifactSink
	pushCallbacks   map[string]func(eventType, taskID string, payload map[string]any) error

	queue         chan string
	mu            sync.Mutex
	runningTaskID string
	runStarted    bool
	resumeTaskIDs map[string]bool
	lastEnqueued  string
	// executionCtxs 任务执行 ctx（terminate 取消路径）。
	executionCtxs map[string]context.CancelFunc
	// terminationRequested HARNESS 无 hook 时置位，执行边界消费。
	terminationRequested map[string]bool
	// providerPollTimeout 显式轮询预算（nil=无墙钟上限）。
	providerPollTimeout *float64
	// wg 追踪 runner goroutine（测试等待）。
	wg sync.WaitGroup
}

// NewWorker 构造 worker。
func NewWorker(store *TaskStore, adapters map[string]ScenarioAdapter,
	usageRecorder UsageRecorderSink, projector *Projector, artifactService ArtifactSink,
	pushCallbacks map[string]func(eventType, taskID string, payload map[string]any) error) *Worker {
	if pushCallbacks == nil {
		pushCallbacks = map[string]func(string, string, map[string]any) error{}
	}
	return &Worker{
		store:                store,
		adapters:             adapters,
		usageRecorder:        usageRecorder,
		projector:            projector,
		artifactService:      artifactService,
		pushCallbacks:        pushCallbacks,
		queue:                make(chan string, queueMaxSize),
		resumeTaskIDs:        map[string]bool{},
		executionCtxs:        map[string]context.CancelFunc{},
		terminationRequested: map[string]bool{},
	}
}

// RegisterPushCallbacks 增补传输回调（不暴露 worker 内部）。
func (w *Worker) RegisterPushCallbacks(cb map[string]func(eventType, taskID string, payload map[string]any) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, v := range cb {
		w.pushCallbacks[k] = v
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Enqueue 入队（内部 v3 §4.2 语义）。返回 QUEUED。
// 单一路径：任何非 PAUSED/非 QUEUED 任务统一 CREATED→QUEUED 并入队；
// 运行中时 runner 常驻自动消费（并发=1）。已排队任务幂等短路。
func (w *Worker) Enqueue(taskID string) (string, error) {
	task, err := w.store.Get(taskID)
	if err != nil {
		return "", err
	}
	if task.Status == string(TaskStatusPaused) {
		return "", NewTaskStateConflict("PAUSED 任务请走 resume")
	}
	w.mu.Lock()
	if task.Status == string(TaskStatusQueued) {
		w.ensureRunnerLocked()
		w.mu.Unlock()
		return string(TaskStatusQueued), nil
	}
	if _, err := w.store.UpdateStatus(taskID, []string{task.Status}, string(TaskStatusQueued), "enqueue"); err != nil {
		w.mu.Unlock()
		return "", err
	}
	w.lastEnqueued = taskID
	w.queue <- taskID
	w.ensureRunnerLocked()
	w.mu.Unlock()
	return string(TaskStatusQueued), nil
}

// Cancel pause/terminate 并把运行控制委托 Provider（对齐 Python cancel）。
func (w *Worker) Cancel(taskID string, mode string) (string, error) {
	mode = lower(mode)
	task, err := w.store.Get(taskID)
	if err != nil {
		return "", err
	}
	adapter := w.adapterFor(task.Scenario, ptrStr(task.ArtifactType))
	if mode == "pause" {
		if task.Scenario == "ARTIFACT" && (adapter == nil || !adapter.SupportsPause()) {
			return "", NewScenarioNotSupported("当前产物场景不支持 pause")
		}
		if task.Status != string(TaskStatusRunning) && task.Status != string(TaskStatusQueued) {
			return "", NewTaskStateConflict("状态 " + task.Status + " 不可 pause")
		}
		if task.Status == string(TaskStatusQueued) {
			w.dequeueLocked(taskID)
			result, err := w.store.UpdateStatus(taskID, []string{task.Status}, string(TaskStatusPaused), "cancel("+mode+")")
			if err != nil {
				return "", err
			}
			return result.Status, nil
		}
		// 运行中任务保持 RUNNING 直到 Provider 确认暂停；控制任务持有最终迁移。
		if err := w.scheduleProviderControl(taskID, adapter, "pause"); err != nil {
			return "", err
		}
		w.releaseSlot(taskID)
		return task.Status, nil
	}
	if mode == "terminate" {
		if task.Status != string(TaskStatusCreated) && task.Status != string(TaskStatusRunning) &&
			task.Status != string(TaskStatusQueued) && task.Status != string(TaskStatusPaused) {
			return "", NewTaskStateConflict("状态 " + task.Status + " 不可 terminate")
		}
		if task.Status == string(TaskStatusQueued) {
			w.dequeueLocked(taskID)
			result, err := w.store.UpdateStatus(taskID, []string{task.Status}, string(TaskStatusTerminated), "cancel("+mode+")")
			if err != nil {
				return "", err
			}
			return result.Status, nil
		}
		if (task.Status == string(TaskStatusRunning) || task.Status == string(TaskStatusPaused)) && adapter != nil && adapter.SupportsTerminate() {
			// Provider 确认终止前公开状态不变（含 PAUSED→TERMINATED 路径）。
			if err := w.scheduleProviderControl(taskID, adapter, "terminate"); err != nil {
				return "", err
			}
			w.releaseSlot(taskID)
			return task.Status, nil
		}
		// 无 terminate hook 的 Provider（生产 harness 引擎）：取消运行中 runner。
		if task.Status == string(TaskStatusRunning) {
			w.mu.Lock()
			w.terminationRequested[taskID] = true
			w.mu.Unlock()
		}
		w.mu.Lock()
		if cancel := w.executionCtxs[taskID]; cancel != nil {
			cancel()
		}
		w.mu.Unlock()
		w.waitRunnerDrained(taskID)
		w.markTerminated(taskID)
		current, err := w.store.Get(taskID)
		if err != nil {
			return "", err
		}
		return current.Status, nil
	}
	return "", NewTaskStateConflict("未知 mode: " + mode)
}

// Resume 校验 + 入队，材料与引擎 fingerprint 由执行路径确认。
// 暂停来源决定恢复后的执行方式：运行中被暂停（真实暂停）→ 执行走
// adapter.resume() 续跑；排队中被暂停（从未真实执行，含重启丢队列）→
// 恢复只是重新入队，执行走全新 run()。
func (w *Worker) Resume(taskID string) (string, error) {
	task, err := w.store.Get(taskID)
	if err != nil {
		return "", err
	}
	if task.Status != string(TaskStatusPaused) {
		return "", NewTaskStateConflict("仅 PAUSED 可 resume")
	}
	adapter := w.adapterFor(task.Scenario, ptrStr(task.ArtifactType))
	resumeSupported := task.Scenario != "ARTIFACT" || (adapter != nil && adapter.SupportsResume())
	resumedFromRun := pausedFromRun(task)
	if resumedFromRun && !resumeSupported {
		return "", NewScenarioNotSupported("当前产物场景不支持运行中暂停后的 resume")
	}
	// fingerprint 校验由 Provider/引擎执行路径负责；worker 只入队。
	if _, err := w.store.UpdateStatus(taskID, []string{string(TaskStatusPaused)}, string(TaskStatusQueued), "resume"); err != nil {
		return "", err
	}
	w.mu.Lock()
	if resumedFromRun || resumeSupported {
		// 运行中暂停（或 Provider 支持 resume）→ 执行时走 adapter.resume() 续跑。
		w.resumeTaskIDs[taskID] = true
	}
	w.lastEnqueued = taskID
	w.queue <- taskID
	w.ensureRunnerLocked()
	w.mu.Unlock()
	return string(TaskStatusQueued), nil
}

// WaitIdle 等待队列排空且当前任务结束（对齐 Python q.join 语义；
// runner 常驻不退出，等待以空闲轮询实现）。
func (w *Worker) WaitIdle() {
	for i := 0; i < 2000; i++ {
		w.mu.Lock()
		idle := len(w.queue) == 0 && w.runningTaskID == ""
		w.mu.Unlock()
		if idle {
			// 双次确认：避开取任务与置位间的窗口。
			time.Sleep(20 * time.Millisecond)
			w.mu.Lock()
			idle = len(w.queue) == 0 && w.runningTaskID == ""
			w.mu.Unlock()
			if idle {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureRunnerLocked 常驻 runner 单例（对齐 Python _ensure_runner）。
func (w *Worker) ensureRunnerLocked() {
	if w.runStarted {
		return
	}
	w.runStarted = true
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runLoop()
	}()
}

// runLoop 主循环：逐任务占住执行位直到终态（对齐 Python _run_loop）。
func (w *Worker) runLoop() {
	for taskID := range w.queue {
		w.mu.Lock()
		w.runningTaskID = taskID
		w.mu.Unlock()
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[RSI] 任务执行异常 task=%s panic=%v，跳过继续取下一个", taskID, r)
				}
				w.mu.Lock()
				delete(w.terminationRequested, taskID)
				w.runningTaskID = ""
				w.mu.Unlock()
			}()
			w.store.UpdateStatus(taskID,
				[]string{string(TaskStatusQueued), string(TaskStatusPaused)},
				string(TaskStatusRunning), "worker.start")
			w.mu.Lock()
			resume := w.resumeTaskIDs[taskID]
			delete(w.resumeTaskIDs, taskID)
			ctx, cancel := context.WithCancel(context.Background())
			w.executionCtxs[taskID] = cancel
			terminateRequested := w.terminationRequested[taskID]
			w.mu.Unlock()
			if terminateRequested {
				// terminate 请求可能先于 runner 注册到达：在真实执行边界消费。
				cancel()
			}
			w.executeTask(ctx, taskID, resume)
		}()
	}
}

// executeTask 执行链（对齐 Python _execute_task + finally 语义）。
func (w *Worker) executeTask(ctx context.Context, taskID string, resume bool) {
	taskView, err := w.store.GetView(taskID)
	if err != nil {
		return
	}
	adapter := w.adapterFor(taskView.Scenario, ptrStr(taskView.ArtifactType))
	if adapter == nil {
		w.store.UpdateStatus(taskID, []string{string(TaskStatusRunning)}, string(TaskStatusFailed),
			"scenario adapter not registered: "+taskView.Scenario)
		return
	}
	// 事件链路：sink → 有界队列 → 消费协程。
	queue := make(chan map[string]any, queueMaxSize)
	consumer := NewEventConsumer(taskID, w.usageRecorder, w.projector, w.artifactService, nil)
	consumer.BindPush(w.pushOf("rsi.training.progress"), w.pushOf("rsi.training.tree.delta"))
	consumeDone := make(chan struct{})
	go ConsumeQueue(queue, consumer, consumeDone)
	sink := func(_ context.Context, event rsi.EngineEvent) error {
		queue <- eventToDict(event)
		return nil
	}
	var result map[string]any
	providerResultReady := false
	cancelled := false
	var execErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = &RsiError{Code: "INTERNAL_ERROR", Message: "任务执行失败"}
				log.Printf("[RSI] 任务执行失败 task=%s: %v", taskID, r)
				w.markFailedIfRunning(taskID, "任务执行失败")
			}
		}()
		// 输入校验（对齐 Python hasattr validate_input 分支）。
		var inputPath *string
		if taskView.Scenario == "ARTIFACT" {
			p := ptrStr(taskView.ArtifactPath)
			inputPath = &p
		} else {
			inputPath = taskView.InputFile
		}
		validation, verr := adapter.ValidateInput(inputPath, taskView.Scenario, taskView.ArtifactType)
		if verr == nil && !validation.Valid {
			w.markFailedIfRunning(taskID, "Provider 输入校验失败")
			execErr = NewDatasetInvalid("Provider 输入校验失败", validation.Errors)
			return
		}
		request, rerr := adapter.BuildRequest(taskView, resume)
		if rerr != nil {
			execErr = rerr
			w.markFailedIfRunning(taskID, FailureReason(rerr, "任务执行失败"))
			return
		}
		if resume {
			result, execErr = adapter.Resume(ctx, request, sink)
		} else {
			result, execErr = adapter.Run(ctx, request, sink)
		}
		if execErr != nil {
			if ctx.Err() != nil {
				// 取消（worker terminate 路径）不算失败。
				return
			}
			log.Printf("[RSI] 任务执行失败 task=%s: %v", taskID, execErr)
			w.markFailedIfRunning(taskID, FailureReason(execErr, "任务执行失败"))
			return
		}
		// Provider 提前返回未终态快照 → 轮询等待（对齐 Python poll 分支）。
		if providerInProgress[providerStatusOf(result)] {
			result, execErr = w.waitForProviderTerminal(ctx, taskID, adapter, result)
			if execErr != nil {
				return
			}
		}
		providerResultReady = true
	}()
	// finally：取消路径立即丢弃余事件；正常路径排空后再关消费协程。
	if cancelled || ctx.Err() != nil {
		queue <- nil
		<-consumeDone
		w.markTerminated(taskID)
	} else {
		for len(queue) > 0 {
			time.Sleep(time.Millisecond)
		}
		queue <- nil
		<-consumeDone
		if providerResultReady {
			w.applyResultStatus(taskID, result, adapter)
		}
	}
	w.persistResults(taskID, result, adapter)
}

// pushOf 事件类型推送闭包（对齐 Python _push）。
func (w *Worker) pushOf(eventType string) func(taskID string, payload map[string]any) error {
	return func(taskID string, payload map[string]any) error {
		w.mu.Lock()
		callback := w.pushCallbacks[eventType]
		w.mu.Unlock()
		if callback == nil {
			return nil
		}
		return callback(eventType, taskID, payload)
	}
}

// waitForProviderTerminal 等待 Provider 快照终态（对齐 Python _wait_for_provider_terminal）。
func (w *Worker) waitForProviderTerminal(ctx context.Context, taskID string, adapter ScenarioAdapter, initial map[string]any) (map[string]any, error) {
	var hasDeadline bool
	var deadline time.Time
	if w.providerPollTimeout != nil {
		hasDeadline = true
		deadline = time.Now().Add(time.Duration(*w.providerPollTimeout * float64(time.Second)))
	}
	for {
		if hasDeadline && !time.Now().Before(deadline) {
			break
		}
		state := adapter.ReadState(taskID)
		status := providerStatusOf(state)
		if status != "" && !providerInProgress[status] {
			return map[string]any{
				"status":        lower(status),
				"error_code":    state["error_code"],
				"error_message": state["error_message"],
			}, nil
		}
		if !hasDeadline {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(providerPollInterval):
			}
			continue
		}
		time.Sleep(providerPollInterval)
	}
	// 超时：fail-safe 收尾（对齐 Python PROVIDER_TIMEOUT 形状）。
	log.Printf("[RSI] Provider 未在预算内进入终态，task=%s", taskID)
	w.terminateProviderAfterTimeout(taskID, adapter)
	return map[string]any{
		"task_id":       taskID,
		"status":        "failed",
		"error_code":    "PROVIDER_TIMEOUT",
		"error_message": "Provider 超时未进入终态",
	}, nil
}

// terminateProviderAfterTimeout 超时 Provider 尽力清理。
func (w *Worker) terminateProviderAfterTimeout(taskID string, adapter ScenarioAdapter) {
	if adapter == nil || !adapter.SupportsTerminate() {
		log.Printf("[RSI] Provider 超时但不支持 terminate，task=%s", taskID)
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = adapter.Terminate(taskID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(providerTerminateTimeout):
		log.Printf("[RSI] Provider 超时清理失败，task=%s", taskID)
	}
}

// applyResultStatus Provider 结果 → 公开状态（对齐 Python _apply_result_status）。
func (w *Worker) applyResultStatus(taskID string, result map[string]any, adapter ScenarioAdapter) {
	status := providerStatusDefault(result, "COMPLETED")
	if providerInProgress[status] {
		if result["error_code"] == nil {
			return
		}
		status = "FAILED"
	}
	target := map[string]TaskStatus{
		"COMPLETED": TaskStatusCompleted, "FAILED": TaskStatusFailed,
		"PAUSED": TaskStatusPaused, "TERMINATED": TaskStatusTerminated,
	}[status]
	if target == "" {
		target = TaskStatusFailed
	}
	current, err := w.store.Get(taskID)
	if err != nil || current.Status != string(TaskStatusRunning) {
		return
	}
	cause := "provider." + lower(status)
	if target == TaskStatusFailed {
		cause = w.providerFailureReason(taskID, result, adapter,
			"Provider 执行失败 ("+firstNonEmpty(valueToString(result["error_code"]), lower(status))+")")
	}
	w.store.UpdateStatus(taskID, []string{string(TaskStatusRunning)}, string(target), cause)
}

// providerFailureReason 失败原因：结果显式消息 > 持久快照 > fallback。
func (w *Worker) providerFailureReason(taskID string, result map[string]any, adapter ScenarioAdapter, fallback string) string {
	if msg := strings_TrimSpace(valueToString(result["error_message"])); msg != "" {
		return FailureReason(result, fallback)
	}
	if adapter != nil {
		state := adapter.ReadState(taskID)
		if reason := FailureReason(state); reason != "" {
			return reason
		}
	}
	return FailureReason(result, fallback)
}

// markTerminated 运行中任务落 TERMINATED（冲突时由其它控制路径持有终态）。
func (w *Worker) markTerminated(taskID string) {
	current, err := w.store.Get(taskID)
	if err != nil {
		return
	}
	if current.Status == string(TaskStatusTerminated) {
		return
	}
	switch current.Status {
	case string(TaskStatusCreated), string(TaskStatusQueued), string(TaskStatusRunning), string(TaskStatusPaused):
		w.store.UpdateStatus(taskID, []string{current.Status}, string(TaskStatusTerminated), "worker.cancelled")
	}
}

// markFailedIfRunning 仅 RUNNING 时落 FAILED（并发 pause/terminate 拥有终态时不覆盖）。
func (w *Worker) markFailedIfRunning(taskID string, cause string) {
	current, err := w.store.Get(taskID)
	if err != nil || current.Status != string(TaskStatusRunning) {
		return
	}
	w.store.UpdateStatus(taskID, []string{string(TaskStatusRunning)}, string(TaskStatusFailed), cause)
}

// adapterFor scenario/artifact_type → adapter 键查找（对齐 Python _adapter_for；
// ARTIFACT 轨 D-9-1 裁剪后仅 HARNESS 键实际可命中）。
func (w *Worker) adapterFor(scenario, artifactType string) ScenarioAdapter {
	scenarioKey := upper(strings_TrimSpace(scenario))
	if scenarioKey == "" {
		return nil
	}
	return w.adapters[scenarioKey]
}

// persistResults 引擎结果落盘 task.json.config.results（对齐 Python _persist_results）。
func (w *Worker) persistResults(taskID string, result map[string]any, adapter ScenarioAdapter) {
	if result == nil {
		return
	}
	results := map[string]any{}
	for _, key := range []string{
		"state_path", "report_path", "current_harness_refs_path",
		"best_harness_refs_path", "published_harness_refs_path",
		"best_score", "best_artifact_path", "published_artifact_path",
		"final_node_id", "error_code", "error_message",
	} {
		if v, ok := result[key]; ok && v != nil {
			results[key] = v
		}
	}
	if _, has := results["published_harness_refs_path"]; !has && adapter != nil {
		state := adapter.ReadPublicationState(taskID)
		for _, key := range []string{"current_harness_refs_path", "best_harness_refs_path", "published_harness_refs_path", "publication_status"} {
			if v := state[key]; v != nil {
				results[key] = v
			}
		}
	}
	if err := w.store.MergeResults(taskID, results); err != nil {
		log.Printf("[RSI] 持久化引擎结果失败 task=%s err=%v", taskID, err)
	}
}

// scheduleProviderControl Provider 控制指令（串行排队，回调式收敛状态）。
func (w *Worker) scheduleProviderControl(taskID string, adapter ScenarioAdapter, mode string) error {
	if adapter == nil {
		return NewScenarioNotSupported("当前 Provider 不支持 " + mode)
	}
	var invoke func() (map[string]any, error)
	switch mode {
	case "pause":
		invoke = func() (map[string]any, error) { return adapter.Pause(taskID) }
	case "terminate":
		invoke = func() (map[string]any, error) { return adapter.Terminate(taskID) }
	default:
		return NewScenarioNotSupported("当前 Provider 不支持 " + mode)
	}
	// Go 侧控制指令同步后台执行并在返回后收敛状态（对齐 Python 控制任务）。
	go func() {
		result, err := invoke()
		if err != nil {
			log.Printf("[RSI] Provider.%s failed task=%s: %v", mode, taskID, err)
			return
		}
		w.applyControlResult(taskID, mode, result)
	}()
	return nil
}

// applyControlResult Provider 控制返回后才提交公开状态。
func (w *Worker) applyControlResult(taskID string, mode string, result map[string]any) {
	rawStatus := valueToString(result["status"])
	if rawStatus == "" {
		rawStatus = valueToString(result)
	}
	status := upper(rawStatus)
	target := map[string]TaskStatus{
		"COMPLETED": TaskStatusCompleted, "FAILED": TaskStatusFailed,
		"PAUSED": TaskStatusPaused, "TERMINATED": TaskStatusTerminated,
	}[status]
	if target == "" {
		log.Printf("[RSI] Provider.%s returned an unusable status task=%s status=%q", mode, taskID, rawStatus)
		return
	}
	task, err := w.store.Get(taskID)
	if err != nil {
		return
	}
	current := task.Status
	if current == string(target) {
		return
	}
	allowedTargets := map[string]map[TaskStatus]bool{
		string(TaskStatusRunning): {TaskStatusCompleted: true, TaskStatusFailed: true, TaskStatusPaused: true, TaskStatusTerminated: true},
		string(TaskStatusPaused):  {TaskStatusTerminated: true},
	}
	if !allowedTargets[current][target] {
		return
	}
	cause := "provider." + lower(status)
	if target == TaskStatusFailed {
		cause = w.providerFailureReason(taskID, result, w.adapterFor(task.Scenario, ptrStr(task.ArtifactType)),
			"Provider."+mode+" 执行失败 ("+firstNonEmpty(valueToString(result["error_code"]), lower(status))+")")
	}
	w.store.UpdateStatus(taskID, []string{current}, string(target), cause)
}

// releaseSlot 让出执行位（对齐 Python _release_slot；HARNESS-only 批内
// supports_pause provider 不存在，保留缝供 ARTIFACT 轨回填）。
func (w *Worker) releaseSlot(taskID string) {}

// waitRunnerDrained 等待任务执行 goroutine 退出（对齐 Python await runner 清理）。
func (w *Worker) waitRunnerDrained(taskID string) {
	for i := 0; i < 200; i++ {
		w.mu.Lock()
		running := w.runningTaskID == taskID
		w.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// dequeueLocked 排队中出队：从队列移除该 task（保持其余顺序）。
func (w *Worker) dequeueLocked(taskID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var remaining []string
	for {
		select {
		case item := <-w.queue:
			if item != taskID {
				remaining = append(remaining, item)
			}
		default:
			for _, item := range remaining {
				w.queue <- item
			}
			return
		}
	}
}

// ──────────────────────────── 模块级助手 ────────────────────────────

// pausedFromRun 最近一次进入 PAUSED 的来源是否为 RUNNING（真实暂停）。
func pausedFromRun(task RsiTask) bool {
	for i := len(task.StatusHistory) - 1; i >= 0; i-- {
		item := task.StatusHistory[i]
		if valueToString(item["to"]) == string(TaskStatusPaused) {
			return valueToString(item["from"]) == string(TaskStatusRunning)
		}
	}
	return false
}

// providerStatusDefault 结果状态归一（对齐 Python _provider_status）。
func providerStatusDefault(value map[string]any, def string) string {
	status := valueToString(value["status"])
	if status == "" {
		return def
	}
	return upper(status)
}

// eventToDict 引擎事件 → 消费方 dict 形态（按 EventType 分派，访问器导出）。
func eventToDict(event rsi.EngineEvent) map[string]any {
	switch e := event.(type) {
	case *rsi.EventProgress:
		payload := map[string]any{
			"event_type":       "progress",
			"iteration":        e.Iteration(),
			"total_iterations": e.TotalIterations(),
			"score":            anyFloat(e.Score()),
			"baseline":         anyFloat(e.Baseline()),
		}
		if e.Usage() != nil {
			payload["usage"] = usageToDict(e.Usage())
		}
		return payload
	case *rsi.EventNode:
		// RsiTreeNode 私有字段 → 访问器导出（provider 完整节点投影形状）。
		node := map[string]any{}
		if n := e.Node(); n != nil {
			nodeID := n.NodeID()
			var parentID any
			if n.ParentID() != nil {
				parentID = *n.ParentID()
			}
			extra := map[string]any{}
			for k, v := range n.Extra() {
				extra[k] = v
			}
			changes := []any{}
			for _, c := range n.Changes() {
				changes = append(changes, c)
			}
			node = map[string]any{
				"node_id": nodeID, "iteration": n.Iteration(), "parent_id": parentID,
				"type": n.Type(), "adopted": n.Adopted(), "score": anyFloat(n.Score()),
				"description": nullableAny(n.Summary()), "snapshot_artifact_id": nullableAny(n.SnapshotArtifactID()),
				"failure_reason": nullableAny(n.Reason()), "failure_class": nullableAny(n.FailureClass()),
				"changes": changes, "extra": extra,
			}
		}
		artifacts := []any{}
		for _, a := range e.Artifacts() {
			artifacts = append(artifacts, map[string]any{"role": a["role"], "path": a["path"], "format": a["format"]})
		}
		return map[string]any{"event_type": "node", "node": node, "artifacts": artifacts}
	case *rsi.EventUsage:
		var modelCall any
		if mc := e.ModelCall(); mc != nil {
			var tokens any
			if t := mc.Tokens(); t != nil {
				tokens = map[string]any{"input": t.Input, "output": t.Output, "cache_hit": t.CacheHit}
			}
			modelCall = map[string]any{"model": mc.Model(), "call_count": mc.CallCount(), "tokens": tokens}
		}
		payload := map[string]any{
			"event_type": "progress.usage",
			"event_id":   e.EventID(),
			"model_call": modelCall,
		}
		if e.NodeRef() != nil {
			payload["node_ref"] = *e.NodeRef()
		}
		return payload
	case *rsi.NodeStageEvent:
		payload := map[string]any{
			"event_type": "node.stage",
			"node_ref":   e.NodeRef(),
			"stage":      e.Stage(),
		}
		if e.Note() != nil {
			payload["note"] = *e.Note()
		}
		return payload
	case *rsi.EventStatus:
		return map[string]any{"event_type": "status"}
	default:
		return map[string]any{"event_type": event.EventType()}
	}
}

// usageToDict RsiUsage → §3.4 dict（私有字段访问器导出）。
func usageToDict(u *rsi.RsiUsage) map[string]any {
	if u == nil {
		return nil
	}
	tokens := map[string]int{"input": 0, "output": 0, "cache_hit": 0}
	if t := u.Tokens(); t != nil {
		if v := t.Input(); v != nil {
			tokens["input"] = *v
		}
		if v := t.Output(); v != nil {
			tokens["output"] = *v
		}
		if v := t.CacheHit(); v != nil {
			tokens["cache_hit"] = *v
		}
	}
	cost := 0.0
	if c := u.CostEstimate(); c != nil {
		cost = *c
	}
	return map[string]any{
		"tokens": tokens, "cost_estimate": cost, "call_count": u.CallCount(),
	}
}

// providerStatusOfMap map 形态快照状态（recovery 同源助手）。
func providerStatusOfMap(state map[string]any) string {
	return providerStatusOf(state)
}

// lower ASCII 小写。
func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

// upper ASCII 大写。
func upper(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'a' && out[i] <= 'z' {
			out[i] -= 'a' - 'A'
		}
	}
	return string(out)
}

// RegisteredPushCallbacksForTest 测试观测缝（返回推送回调快照）。
func (w *Worker) RegisteredPushCallbacksForTest() map[string]func(eventType, taskID string, payload map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]func(string, string, map[string]any) error{}
	for k, v := range w.pushCallbacks {
		out[k] = v
	}
	return out
}
