package rsi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// usage.go —— 运行级模型用量观察与持久化、无内容的调用账本。
// Python: openjiuwen/rsi/usage.py

// ──────────────────────────── 导出函数 ────────────────────────────

// UsageTokens 归一化 core/OpenAI 计数器，缺失的提供方数据不做估算。
// Python: usage_tokens()。输入含缓存命中（缓存命中是输入的子集而非额外
// 输入）；推理 token（若提供）已含在输出 token 中。
// 键回退序：input: input_tokens→prompt_tokens→input；output:
// output_tokens→completion_tokens→output；cache: cache_read_tokens→
// prompt_cache_hit_tokens→cache_hit→prompt_tokens_details.cached_tokens。
// 旧 cache_tokens 可能指缓存写入，不得标为命中。
func UsageTokens(raw any) *RsiUsageTokens {
	// Python: usage.model_dump(exclude_unset=True) 分支 → *UsageMetadata 归一
	if md, ok := raw.(*llmschema.UsageMetadata); ok && md != nil {
		raw = usageMetadataToMap(md)
	}
	// 非 Mapping 输入：计数器全缺失（Python getattr 路径在 Go 侧仅覆盖
	// 结构化两种形态，见 spec 已知偏差说明）
	m, _ := raw.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	// Python: cache = _counter(usage, "cache_read_tokens", "prompt_cache_hit_tokens", "cache_hit")
	cache := counter(m, "cache_read_tokens", "prompt_cache_hit_tokens", "cache_hit")
	if cache == nil {
		// Python: cache = _counter(_field(usage, "prompt_tokens_details"), "cached_tokens")
		cache = counter(fieldOf(m, "prompt_tokens_details"), "cached_tokens")
	}
	return NewRsiUsageTokens(
		counter(m, "input_tokens", "prompt_tokens", "input"),
		counter(m, "output_tokens", "completion_tokens", "output"),
		cache,
	)
}

// UsageSnapshot 从持久化 state 装载共享累计投影。
// Python: usage_snapshot()——仅从 Mapping 装载；cost_estimate 恒为 nil。
func UsageSnapshot(raw any) *RsiUsage {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return NewRsiUsage(UsageTokens(m["tokens"]), nil, intOf(m["call_count"], 0))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// usageMetadataToMap 将 *UsageMetadata 展开为 Python model_dump 等价映射。
// 指针字段（CacheReadTokens）nil 视为未设置（对齐 exclude_unset）；
// 非指针 int 字段恒存在（Go 无 unset 概念，取值 0 时 counter 仍合法返回 0）。
func usageMetadataToMap(md *llmschema.UsageMetadata) map[string]any {
	m := map[string]any{
		"input_tokens":  md.InputTokens,
		"output_tokens": md.OutputTokens,
	}
	if md.CacheReadTokens != nil {
		m["cache_read_tokens"] = *md.CacheReadTokens
	}
	return m
}

// fieldOf 读取 Mapping 字段（Python: _field 的 Mapping 分支；结构体属性分支
// 由 *UsageMetadata 展开覆盖）。
func fieldOf(value any, key string) any {
	if m, ok := value.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// counter 按键序取第一个合法计数器：非 bool 的非负整数（Python:
// isinstance(raw, int) and not isinstance(raw, bool) and raw >= 0）。
// JSON 反序列化数字为 float64，取整数值等价接受（Python json.loads 得 int）。
func counter(value any, keys ...string) *int {
	m, _ := value.(map[string]any)
	if m == nil {
		return nil
	}
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case bool:
			continue
		case int:
			if v >= 0 {
				return &v
			}
		case int64:
			if v >= 0 {
				iv := int(v)
				return &iv
			}
		case float64:
			// 整数值才视为计数器（Python json.loads 的 int 形态）
			if v >= 0 && v == math.Trunc(v) {
				iv := int(v)
				return &iv
			}
		}
	}
	return nil
}

// intOf 以缺省值读取整数（Python: int(raw.get(key, default))）。
func intOf(raw any, defaultValue int) int {
	if raw == nil {
		return defaultValue
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}

// ──────────────────────────── 结构体 ────────────────────────────

// pendingCall 一次在途请求的登记（call_id → 模型/起始时刻/归属）。
// Python: self.pending dict 值元组。
type pendingCall struct {
	model   string
	started time.Time
	node    *string
	stage   *string
}

// ModelUsageObserver 运行级模型用量观察者与持久化账本。
// Python: usage.py ModelUsageObserver——只观察本 run 异步上下文继承的回调。
// 回调注册表是全局的：ctx 标签过滤防止两个并发 RSI 任务互相记账。
// 回调不变换模型输入/输出；账本重放恢复累计而不重发增量。
type ModelUsageObserver struct {
	onEvent       OnEvent
	state         map[string]any
	seen          map[string]bool
	pending       map[string]*pendingCall
	lock          sync.Mutex // Python asyncio.Lock 等价：仅护去重/账本写入/累计（记账路径内串行，顺序与单协程一致）
	deliveryError error
	sequence      int
	// D7：控制器侧只读快照走 atomic，写入方为观察者单点（锁内写、读侧零锁）
	totals    atomic.Pointer[RsiUsage]
	usagePath atomic.Pointer[string]
}

// path 返回账本路径（atomic 读）。
func (o *ModelUsageObserver) path() string {
	if p := o.usagePath.Load(); p != nil {
		return *p
	}
	return ""
}

// setPath 设置账本路径（atomic 写）。
func (o *ModelUsageObserver) setPath(p string) { o.usagePath.Store(&p) }

// NewModelUsageObserver 创建观察者；totals 初始为 RsiUsage(RsiUsageTokens(0,0,0), None, 0)。
func NewModelUsageObserver(onEvent OnEvent) *ModelUsageObserver {
	o := &ModelUsageObserver{
		onEvent: onEvent,
		seen:    map[string]bool{},
		pending: map[string]*pendingCall{},
	}
	o.totals.Store(NewRsiUsage(NewRsiUsageTokens(nil, nil, nil), nil, 0))
	return o
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Bind 将观察者绑定到控制器已校验身份的 state 与输出目录。
// Python: bind()——账本文件存在时逐行重放：task_id 不匹配即报错、
// sequence 取历史最大 event_id、按 call_id 去重累计。
func (o *ModelUsageObserver) Bind(state map[string]any, outputDir string) error {
	o.setPath(filepath.Join(outputDir, "model_calls.jsonl"))
	if info, err := os.Stat(o.path()); err == nil && !info.IsDir() {
		stream, err := os.Open(o.path())
		if err != nil {
			return err
		}
		defer func() { _ = stream.Close() }()
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				return err
			}
			if record["task_id"] != toString(state["task_id"]) {
				return errors.New("model usage ledger task_id does not match this run")
			}
			// Python: self.sequence = max(self.sequence, int(record["event_id"]))
			if seq := intOf(record["event_id"], 0); seq > o.sequence {
				o.sequence = seq
			}
			callID := toString(record["call_id"])
			if !o.seen[callID] {
				o.seen[callID] = true
				// Python: self._accumulate(usage_tokens(record["model_call"]["tokens"]))
				modelCall, _ := record["model_call"].(map[string]any)
				var tokens any
				if modelCall != nil {
					tokens = modelCall["tokens"]
				}
				o.accumulateLocked(UsageTokens(tokens))
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	o.state = state
	o.updateStateLocked()
	return nil
}

// Record 记一笔模型调用。
// Python: record()——校验 model/call_id/合法 status → call_id 去重 →
// 造 EventUsage → 账本逐行追加 → sequence/seen/累计/state 更新 →
// 以解除 observer 标签的 ctx 发出事件（防服务侧工作递归入账）→
// emit 失败存 deliveryError（不经返回值抛出）。
func (o *ModelUsageObserver) Record(ctx context.Context, model, callID string, usage any, status string, durationMs *int, nodeRef, stageRef *string) error {
	// Python: if self.state is None or self.path is None: return（无活动 run 为 no-op）
	if o.state == nil || o.path() == "" {
		return nil
	}
	// Python: 校验 model/call_id 非空且 status 合法
	if model == "" || callID == "" || (status != string(ModelCallStatusSucceeded) && status != string(ModelCallStatusFailed) && status != string(ModelCallStatusIncomplete)) {
		return errors.New("model usage requires model, stable call_id and valid status")
	}
	o.lock.Lock()
	// Python: if call_id in self.seen: return（重放/重复导入幂等）
	if o.seen[callID] {
		o.lock.Unlock()
		return nil
	}
	// Python: EventUsage(event_id=self.sequence+1, ts=datetime.now(UTC).isoformat(), ...)
	event := NewEventUsage(
		o.sequence+1,
		toString(o.state["task_id"]),
		nowISOFormat(),
		callID,
		NewRsiModelCall(model, 1, UsageTokens(usage), ModelCallStatus(status), durationMs),
		nodeRef,
		stageRef,
	)
	// Python: aiofiles 追加一行（append 模式、无 fsync——Go 同步 append 同语义，spec P4）
	line, err := json.Marshal(event)
	if err != nil {
		o.lock.Unlock()
		return err
	}
	stream, err := os.OpenFile(o.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		o.lock.Unlock()
		return err
	}
	_, writeErr := stream.Write(append(line, '\n'))
	closeErr := stream.Close()
	if writeErr != nil {
		o.lock.Unlock()
		return writeErr
	}
	if closeErr != nil {
		o.lock.Unlock()
		return closeErr
	}
	o.sequence = event.EventID()
	o.seen[callID] = true
	o.accumulateLocked(event.ModelCall().Tokens())
	o.updateStateLocked()
	o.lock.Unlock()
	// Python: token = _observer.set(None) → emit → finally reset：
	// 服务侧 sink 工作不是引擎模型调用，不得在持锁期间递归进入本观察者。
	if err := Emit(withoutObserverTag(ctx), o.onEvent, event); err != nil {
		o.deliveryError = err
	}
	return nil
}

// Totals 当前累计快照（D7：atomic 读，零锁；写入方为观察者单点）。
func (o *ModelUsageObserver) Totals() *RsiUsage {
	return o.totals.Load()
}

// CheckDelivery 在观察周期外暴露投递失败。
// Python: check_delivery()——persisted calls can be reconciled。
func (o *ModelUsageObserver) CheckDelivery() error {
	if o.deliveryError != nil {
		return fmt.Errorf("RSI model usage delivery failed; persisted calls can be reconciled: %w", o.deliveryError)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// accumulate 累计一笔 token 计数：未上报的计数器不贡献也不清零既有累计
// （Python: _accumulate——summed 逐键 previous+value if value is not None）。
// call_count 恒 +1（无论计数器是否上报）。
func (o *ModelUsageObserver) accumulateLocked(tokens *RsiUsageTokens) {
	prev := o.totals.Load().Tokens()
	summed := NewRsiUsageTokens(
		addCounter(prev.Input(), tokens.Input()),
		addCounter(prev.Output(), tokens.Output()),
		addCounter(prev.CacheHit(), tokens.CacheHit()),
	)
	o.totals.Store(NewRsiUsage(summed, nil, o.totals.Load().CallCount()+1))
}

// addCounter 单键累计：value 为 nil 时保留 previous（不清零）。
func addCounter(previous, value *int) *int {
	if value == nil {
		return previous
	}
	if previous == nil {
		return value
	}
	sum := *previous + *value
	return &sum
}

// updateState 原地更新共享 state：usage（call_count 为 0 时置 nil）与账本路径。
// Python: _update_state()——state["usage"] = asdict(self.totals) if call_count else None。
func (o *ModelUsageObserver) updateStateLocked() {
	if o.state == nil {
		return
	}
	if o.totals.Load().CallCount() > 0 {
		o.state["usage"] = o.totals.Load()
	} else {
		o.state["usage"] = nil
	}
	o.state["model_calls_path"] = o.path()
}

// toString 把任意值转字符串（state/账本字段读取）。
func toString(raw any) string {
	if s, ok := raw.(string); ok {
		return s
	}
	return ""
}

// nowISOFormat 当前 UTC 时间，微秒精度，与 Python datetime.now(UTC).isoformat() 对齐。
func nowISOFormat() string {
	return time.Now().UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000+00:00")
}

// ──────────────────────────── 枚举 ────────────────────────────

// usageCtxKey 用量观察 ctx 键类型（非导出）。
type usageCtxKey int

const (
	// usageCtxKeyObserver 当前活动观察者（无活动 run 时缺省 nil）。
	usageCtxKeyObserver usageCtxKey = iota
	// usageCtxKeyNode 当前迭代的节点归属（仅活动 run 内有效）。
	usageCtxKeyNode
	// usageCtxKeyStage 异步引擎操作归属的公开阶段。
	usageCtxKeyStage
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// withoutObserverTag 返回解除 observer 标签的 ctx（Python: _observer.set(None)）。
// 用于 emit 阶段：sink 侧工作不得递归进入本观察者。
func withoutObserverTag(ctx context.Context) context.Context {
	return context.WithValue(ctx, usageCtxKeyObserver, nil)
}

// ──────────────────────────── 导出函数（ctx 标签与公共钩子） ────────────────────────────

// SetUsageNode 选择活动 run 内的当前迭代节点。
// Python: set_usage_node()——无活动观察者时静默忽略。
func SetUsageNode(ctx context.Context, nodeRef string) context.Context {
	if observerFromContext(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, usageCtxKeyNode, nodeRef)
}

// NodeRefFromContext 读取当前节点归属（无标签时为 nil）。
func NodeRefFromContext(ctx context.Context) *string {
	if v, ok := ctx.Value(usageCtxKeyNode).(string); ok {
		return &v
	}
	return nil
}

// StageRefFromContext 读取当前阶段归属（无标签时为 nil）。
func StageRefFromContext(ctx context.Context) *string {
	if v, ok := ctx.Value(usageCtxKeyStage).(string); ok {
		return &v
	}
	return nil
}

// ModelUsageStage 把异步引擎操作发起的调用归属到其公开阶段。
// Python: model_usage_stage 装饰器 → 包装函数：进入时设置 stage 标签，
// 退出还原（Go ctx 不可变，还原表现为调用方改用外层 ctx，spec P1）。
func ModelUsageStage(ctx context.Context, stageRef string) (context.Context, func()) {
	next := context.WithValue(ctx, usageCtxKeyStage, stageRef)
	return next, func() { _ = ctx.Value(usageCtxKeyStage) }
}

// RecordModelUsage 公共钩子：core Model 之外的调用（含子进程）记账入口。
// Python: record_model_usage()——在父 run 中以外部调用的稳定 id 与提供方
// usage 调用；禁止把试验聚合总量当作单笔传入；恢复时重复导入幂等；
// 无活动 run 为 no-op。
func RecordModelUsage(ctx context.Context, model, callID string, usage any, status string, durationMs *int, stageRef *string) error {
	observer := observerFromContext(ctx)
	if observer == nil {
		return nil
	}
	// Python: stage_ref or _stage.get()；node 取 _node.get()
	if stageRef == nil {
		stageRef = StageRefFromContext(ctx)
	}
	return observer.Record(ctx, model, callID, usage, status, durationMs, NodeRefFromContext(ctx), stageRef)
}

// ──────────────────────────── 导出函数（observe 订阅） ────────────────────────────

// Observe 安装作用域订阅，取消时即使有并发触发也移除（Python: observe()
// asynccontextmanager——注册五回调 + ctx 绑 observer 与 node="h0"；
// finally 注销 + 还原标签）。
// 注册侧投递失败经 CheckDelivery 在观察周期外暴露。
func (o *ModelUsageObserver) Observe(ctx context.Context) (context.Context, context.CancelFunc) {
	framework := callback.GetCallbackFramework()
	// Python: bindings = ((LLM_INVOKE_INPUT, _input), (LLM_STREAM_INPUT, _input),
	// (LLM_OUTPUT, _complete), (LLM_INVOKE_OUTPUT, _invoke_output), (LLM_CALL_ERROR, _complete))
	bindings := []struct {
		event callback.LLMCallEventType
		fn    callback.LLMCallbackFunc
	}{
		{callback.LLMInvokeInput, o.inputCallback},
		{callback.LLMStreamInput, o.inputCallback},
		{callback.LLMOutput, o.completeCallback},
		{callback.LLMInvokeOutput, o.invokeOutputCallback},
		{callback.LLMCallError, o.completeCallback},
	}
	scoped := context.WithValue(
		context.WithValue(ctx, usageCtxKeyObserver, o),
		usageCtxKeyNode, "h0", // Python: node_token = _node.set("h0")
	)
	for _, b := range bindings {
		framework.OnLLM(b.event, b.fn)
	}
	return scoped, func() {
		// Python: finally——逐对注销 + 还原标签（Go ctx 不可变，调用方回到外层 ctx）
		for _, b := range bindings {
			framework.OffLLM(b.event, b.fn)
		}
	}
}

// FinishPending 把中断/未上报的请求计为 incomplete（不编造成功或 token）。
// Python: finish_pending()——遍历 pending 逐笔记账后清空。
func (o *ModelUsageObserver) FinishPending() error {
	o.lock.Lock()
	pending := make(map[string]*pendingCall, len(o.pending))
	for k, v := range o.pending {
		pending[k] = v
	}
	o.lock.Unlock()
	for callID, p := range pending {
		durationMs := intPtr(elapsedMs(p.started))
		// Python: record(model, call_id, None, "incomplete", elapsed, node, stage)
		if err := o.Record(context.Background(), p.model, callID, nil, string(ModelCallStatusIncomplete), durationMs, p.node, p.stage); err != nil {
			return err
		}
		o.lock.Lock()
		delete(o.pending, callID)
		o.lock.Unlock()
	}
	return nil
}

// ──────────────────────────── 非导出函数（回调实现） ────────────────────────────

// observerFromContext 读取 ctx 上的活动观察者标签。
func observerFromContext(ctx context.Context) *ModelUsageObserver {
	o, _ := ctx.Value(usageCtxKeyObserver).(*ModelUsageObserver)
	return o
}

// withObserverTag 测试与钩子侧装配观察者标签。
func withObserverTag(ctx context.Context, o *ModelUsageObserver) context.Context {
	return context.WithValue(ctx, usageCtxKeyObserver, o)
}

// inputCallback 发起侧回调：登记在途请求。
// Python: _input()——observer 标签非本实例即返回；call_id 缺失不登记；
// model 取事件模型名，缺省 unknown。
func (o *ModelUsageObserver) inputCallback(ctx context.Context, data *callback.LLMCallEventData) any {
	if observerFromContext(ctx) != o {
		return nil
	}
	callID := llm.GetCurrentLLMCallID(ctx)
	if callID == "" {
		return nil
	}
	o.lock.Lock()
	defer o.lock.Unlock()
	// Python: model = kwargs.get("model") or _field(model_config, "model_name") or "unknown"
	model := data.ModelName
	if model == "" {
		model = "unknown"
	}
	o.pending[callID] = &pendingCall{
		model:   model,
		started: time.Now(),
		node:    NodeRefFromContext(ctx),
		stage:   StageRefFromContext(ctx),
	}
	return nil
}

// completeCallback 完成侧回调：出账。
// Python: _complete()——observer 标签校验；call_id 缺失或已 seen 返回；
// pending 存在时才计算耗时；错误转 deliveryError 不吞。
func (o *ModelUsageObserver) completeCallback(ctx context.Context, data *callback.LLMCallEventData) any {
	if observerFromContext(ctx) != o {
		return nil
	}
	callID := llm.GetCurrentLLMCallID(ctx)
	if callID == "" || o.seen[callID] {
		return nil
	}
	pending := o.popPending(callID)
	// Python: model, started, node, stage = pending or (model_name or "unknown", now, node, stage)
	model := data.ModelName
	node := NodeRefFromContext(ctx)
	stage := StageRefFromContext(ctx)
	var durationMs *int
	if pending != nil {
		if model == "" {
			model = pending.model
		}
		if node == nil {
			node = pending.node
		}
		if stage == nil {
			stage = pending.stage
		}
		durationMs = intPtr(elapsedMs(pending.started))
	}
	if model == "" {
		model = "unknown"
	}
	// Python: "failed" if error is not None else "succeeded"
	status := string(ModelCallStatusSucceeded)
	if data.Error != nil {
		status = string(ModelCallStatusFailed)
	}
	// Python: try: await self.record(...) except Exception as exc: self.delivery_error = exc
	if err := o.Record(ctx, model, callID, data.Usage, status, durationMs, node, stage); err != nil {
		o.deliveryError = err
	}
	return nil
}

// invokeOutputCallback 自定义客户端缺 LLM_OUTPUT 时的兜底：从 invocation
// 结果取用量后转完成链。Python: _invoke_output()。
func (o *ModelUsageObserver) invokeOutputCallback(ctx context.Context, data *callback.LLMCallEventData) any {
	usage := data.Usage
	if usage == nil {
		// Python: usage = _field(result, "usage_metadata")；native 客户端的
		// 重复回调由 seen 去重吸收
		if msg, ok := data.Response.(*llmschema.AssistantMessage); ok {
			usage = msg.UsageMetadata
		} else if chunk, ok := data.Response.(*llmschema.AssistantMessageChunk); ok {
			usage = chunk.UsageMetadata
		}
	}
	// Python: await self._complete(usage=...)——无 error 信息
	shallow := &callback.LLMCallEventData{ModelName: data.ModelName, Usage: usage, Response: data.Response}
	return o.completeCallback(ctx, shallow)
}

// popPending 弹出在途登记（Python: self.pending.pop(call_id, None)）。
func (o *ModelUsageObserver) popPending(callID string) *pendingCall {
	o.lock.Lock()
	defer o.lock.Unlock()
	p := o.pending[callID]
	delete(o.pending, callID)
	return p
}

// elapsedMs 计算自 started 起的毫秒数，钳制非负（Python: max(0, round(...))）。
func elapsedMs(started time.Time) int {
	ms := int(time.Since(started).Milliseconds())
	if ms < 0 {
		return 0
	}
	return ms
}

// intPtr 整数指针助手（pending 计时与测试共用）。
func intPtr(v int) *int { return &v }
