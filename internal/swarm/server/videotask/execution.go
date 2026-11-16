package videotask

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskExecutionBinding 单次受管执行的检查点 + 原生句柄。
// 对齐 execution.py:16-23 TaskExecutionBinding(TaskCheckpoint)：继承 Python 的
// TaskCheckpoint 能力（check/before_model/after_model），叠加本次执行的绑定态。
// Go 无继承（memory 嵌入虚方法陷阱）：checkpoint 判定逻辑内联为方法，动作经
// 注入的 endpoint 表达（偏差 O-10 修订：装配方为 RemoteTaskEndpoint）。
type TaskExecutionBinding struct {
	// endpoint 网关账本端点（RemoteTaskEndpoint.Call）
	endpoint *RemoteTaskEndpoint
	// TaskID 任务 ID
	TaskID string
	// RequestID 绑定请求 ID
	RequestID string
	// Root 根 Agent（Python self.root；双侧构造后零读取，登记为死字段不移植）
	// closed 绑定是否已关闭（对齐 checkpoint closed）
	closed bool

	// Harness 当次执行的 harness（Python self.harness；capture_execution 用）
	Harness TaskHarness
	// nativeExecutions 原生执行句柄集合（asyncio task → Go 完成信号通道）
	mu                   sync.Mutex
	nativeExecutions     []<-chan struct{}
	cancelledFlags       map[<-chan struct{}]bool
	settlementUnobserved bool
	outputStream         TaskOutputStream
	invocationCancelled  bool
}

// TaskHarness 受管执行所需的最小 harness 面（对齐 capture_execution 对
// harness.active_round/loop_controller.task_scheduler._running_tasks 的鸭子访问）。
// 偏差 O-14：Python 穿透三层私有属性抓调度器句柄，Go 由装配层直接给出
// 「当前执行完成信号」提供者，语义等价（不推断、只登记确切句柄）。
type TaskHarness interface {
	// CurrentExecutionDone 取当前轮次的原生执行完成信号；无活跃轮次返回 nil。
	CurrentExecutionDone() <-chan struct{}
	// HasActiveRound 当次调用是否仍有活跃轮次。
	HasActiveRound() bool
}

// TaskOutputStream 输出租约的最小面（对齐 bind/close_task_output 的 stream.close）。
type TaskOutputStream interface {
	// Close 关闭输出流；abortActiveRound 对齐 stream.close(abort_active_round=...)。
	Close(abortActiveRound bool) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Check 绑定与运行状态校验。
// 对齐 checkpoint.py:12-22 check（经 TaskCheckpoint 基类）。
func (b *TaskExecutionBinding) Check(runExtra map[string]any) error {
	if b.closed {
		return errors.New("TASK_EXECUTION_BINDING_STALE")
	}
	run := toMap(runExtra["run_context"])
	runExtraMap := toMap(run["extra"])
	if strOf(runExtraMap["managed_task_request"]) != b.RequestID {
		return errors.New("TASK_EXECUTION_BINDING_STALE")
	}
	// Python check 调 call("status") 无 data：身份全靠 endpoint identity
	result, err := b.endpoint.Call("status", nil)
	if err != nil {
		return err
	}
	status := strOf(result["status"])
	if status != "running" && status != "waiting_user" {
		return errors.New("TASK_EXECUTION_NOT_RUNNING")
	}
	return nil
}

// BeforeModel 模型前：认领变更并经 endpoint 写入上下文回执。
// 对齐 checkpoint.py:23-49 before_model（消息注入由装配层 hook 完成，见 rail 分派）。
func (b *TaskExecutionBinding) BeforeModel(runExtra map[string]any, addChange func(changeID, instruction string) error) error {
	if err := b.Check(runExtra); err != nil {
		return err
	}
	result, err := b.endpoint.Call("claim", nil)
	if err != nil {
		return err
	}
	changes, _ := result["changes"].([]any)
	for _, change := range changes {
		changeMap := toMap(change)
		// SQLite 与模型上下文不是一个事务：丢失的 ACK 状态未知（对齐 :31 注释）
		if err := addChange(strOf(changeMap["id"]), strOf(changeMap["instruction"])); err != nil {
			return err
		}
		if _, err := b.endpoint.Call("context_written", map[string]any{
			"change_ids": []any{changeMap["id"]},
		}); err != nil {
			return err
		}
	}
	return b.Check(runExtra)
}

// AfterModel 模型后：观察最终请求中的变更回执（不校验语义合规）。
// 对齐 checkpoint.py:50-71 after_model（observed 消息由装配层 hook 提供）。
func (b *TaskExecutionBinding) AfterModel(runExtra map[string]any, observedContents []string) error {
	if err := b.Check(runExtra); err != nil {
		return err
	}
	observedSet := map[string]bool{}
	observed := []any{}
	for _, content := range observedContents {
		value := parseJSONMap(content)
		if value == nil {
			continue
		}
		id := strOf(value["managed_task_change"])
		if id != "" && !observedSet[id] {
			observedSet[id] = true
			observed = append(observed, id)
		}
	}
	if len(observed) > 0 {
		if _, err := b.endpoint.Call("observed", map[string]any{
			"change_ids": observed,
		}); err != nil {
			return err
		}
	}
	return nil
}

// CaptureExecution 登记确切的原生执行句柄。
// 对齐 execution.py:24-46 capture_execution（绝不从终态/缺席推断结算）。
// Python 抓 asyncio task；Go 抓当前轮次完成信号（TaskHarness 注入，偏差 O-14）。
func (b *TaskExecutionBinding) CaptureExecution(inCallback bool) {
	if b.Harness == nil {
		return
	}
	done := b.Harness.CurrentExecutionDone()
	b.mu.Lock()
	defer b.mu.Unlock()
	if done != nil {
		if !b.contains(done) {
			b.nativeExecutions = append(b.nativeExecutions, done)
		}
		b.settlementUnobserved = false
		return
	}
	if inCallback && b.Harness.HasActiveRound() {
		b.settlementUnobserved = true
	} else if b.Harness.HasActiveRound() && len(b.nativeExecutions) == 0 {
		b.settlementUnobserved = true
	}
}

// contains 去重（capture 幂等）。
func (b *TaskExecutionBinding) contains(done <-chan struct{}) bool {
	for _, existing := range b.nativeExecutions {
		if existing == done {
			return true
		}
	}
	return false
}

// Closed 标记绑定关闭。
func (b *TaskExecutionBinding) markClosed() { b.closed = true }

// SetOutputStream 绑定输出租约（对齐 bind_task_output 的 stream 挂载）。
func (b *TaskExecutionBinding) SetOutputStream(stream TaskOutputStream) {
	b.mu.Lock()
	b.outputStream = stream
	b.mu.Unlock()
}

// OutputStream 读输出租约。
func (b *TaskExecutionBinding) OutputStream() TaskOutputStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.outputStream
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BindTaskExecution 绑定一次受管执行（asynccontextmanager 的显式 Bind/Unbind 对；
// 偏差 O-12：Python async with → Go Bind + 返回的 unbind 闭包 defer 执行）。
// 对齐 execution.py:48-141 bind_task_execution。
func BindTaskExecution(requestParams map[string]any, requestID, sessionID, userID string,
	harness TaskHarness, inputs map[string]any,
) (*TaskExecutionBinding, func(), error) {
	// 对齐 :49-53：channel_id==video_tool 且带绑定或 managed-task- 前缀才受管；
	// 渠道判定由适配器完成（channelID 由调用方校验后传入），此处仅判绑定证据
	bindingParams, _ := requestParams["managed_task_binding"].(map[string]any)
	if bindingParams == nil && !hasManagedTaskPrefix(sessionID) {
		return nil, func() {}, nil
	}
	endpoint, err := NewRemoteTaskEndpoint(requestParams, requestID, sessionID, userID)
	if err != nil {
		return nil, nil, err
	}
	rail := CurrentVoiceRail()
	if rail == nil {
		return nil, nil, errors.New("Managed task callback rail is unavailable")
	}
	bindings := rail.managedTasks
	if _, exists := bindings[sessionID]; exists {
		return nil, nil, errors.New("Task execution already bound")
	}
	if _, err := endpoint.Call("bind", nil); err != nil {
		return nil, nil, err
	}
	task := map[string]any{"id": endpoint.identity["task_id"], "request_id": requestID}
	checkpoint := &TaskExecutionBinding{
		endpoint: endpoint, TaskID: strOf(task["id"]), RequestID: requestID,
		Harness: harness,
	}
	bindings[sessionID] = checkpoint
	// 对齐 :69-77：inputs.run.context.extra.managed_task_request 注入
	previous := toMap(inputs["run"])
	run := map[string]any{}
	for k, v := range previous {
		run[k] = v
	}
	context := toMap(run["context"])
	extra := toMap(context["extra"])
	extra["managed_task_request"] = requestID
	context["extra"] = extra
	run["context"] = context
	inputs["run"] = run

	unbind := func() {
		// 对齐 finally：setup 失败也可能未触达任何模型回调
		checkpoint.CaptureExecution(false)
		checkpoint.markClosed()
		delete(bindings, sessionID)
		if previous == nil {
			delete(inputs, "run")
		} else {
			inputs["run"] = previous
		}
		// 结算上报（对齐 report_settlement；_settlements 集合 → WaitGroup）
		settleWG.Add(1)
		go func() {
			defer settleWG.Done()
			_, _ = endpoint.Call("close", nil)
			checkpoint.mu.Lock()
			unobserved := checkpoint.settlementUnobserved
			natives := append([](<-chan struct{})(nil), checkpoint.nativeExecutions...)
			checkpoint.mu.Unlock()
			if unobserved {
				return
			}
			for _, done := range natives {
				<-done // 对齐 asyncio.gather(shield(t))
			}
			cancelled := checkpoint.invocationCancelled
			_, _ = endpoint.Call("settle", map[string]any{"cancelled": cancelled})
		}()
	}
	return checkpoint, unbind, nil
}

// hasManagedTaskPrefix 会话 ID 是否为受管核心会话（对齐 session_id.startswith("managed-task-")）。
func hasManagedTaskPrefix(sessionID string) bool {
	return len(sessionID) >= len("managed-task-") && sessionID[:len("managed-task-")] == "managed-task-"
}

// settleWG 在途结算上报登记（对齐 execution.py:11 _settlements 集合的等待语义）。
var settleWG sync.WaitGroup

// WaitSettlements 等待全部在途结算上报完成（测试与优雅停机用；Python 由
// add_done_callback 记录失败，Go 以 WaitGroup 提供确定性等待点）。
func WaitSettlements() { settleWG.Wait() }

// BindTaskOutput 保持确切输出租约以便取消唤醒空闲消费者。
// 对齐 execution.py:144-154 bind_task_output。
func BindTaskOutput(rail *VoiceAgentTaskRail, sessionID, requestID string, stream TaskOutputStream) {
	rail.mu.Lock()
	binding := rail.managedTasks[sessionID]
	rail.mu.Unlock()
	if binding == nil || binding.RequestID != requestID {
		return
	}
	binding.CaptureExecution(false)
	binding.SetOutputStream(stream)
	result, err := binding.endpoint.Call("status", nil)
	if err != nil {
		return
	}
	if strOf(result["status"]) == "cancelling" {
		_ = stream.Close(false)
	}
}

// CloseTaskOutput 取消时闭合输出租约（cancel_round 已发信号，关租约另行）。
// 对齐 execution.py:156-169 close_task_output。
func CloseTaskOutput(rail *VoiceAgentTaskRail, sessionID, expectedRequestID string) {
	rail.mu.Lock()
	binding := rail.managedTasks[sessionID]
	rail.mu.Unlock()
	if binding == nil || expectedRequestID == "" || binding.RequestID != expectedRequestID {
		return
	}
	result, err := binding.endpoint.Call("status", nil)
	if err != nil {
		return
	}
	if strOf(result["status"]) != "cancelling" {
		return
	}
	if stream := binding.OutputStream(); stream != nil {
		_ = stream.Close(false)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toMap nil 安全 map 归一。
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// strOf nil 安全字符串化。
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// parseJSONMap 容错解析 JSON 对象（对齐 after_model 的 json.loads 守卫）。
func parseJSONMap(content string) map[string]any {
	var value map[string]any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return nil
	}
	return value
}
