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
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
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
	if md, ok := raw.(*llmschema.UsageMetadata); ok {
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
	path          string
	seen          map[string]bool
	pending       map[string]*pendingCall
	lock          sync.Mutex // Python asyncio.Lock 等价（记账路径内串行，顺序与单协程一致）
	deliveryError error
	sequence      int
	totals        *RsiUsage
	// D7：控制器侧只读快照（atomic），写入方仍为观察者单点
	usageSnap *RsiUsage // 仅在 lock 内读写，外部经 Totals() 取快照
}

// NewModelUsageObserver 创建观察者；totals 初始为 RsiUsage(RsiUsageTokens(0,0,0), None, 0)。
func NewModelUsageObserver(onEvent OnEvent) *ModelUsageObserver {
	return &ModelUsageObserver{
		onEvent: onEvent,
		seen:    map[string]bool{},
		pending: map[string]*pendingCall{},
		totals:  NewRsiUsage(NewRsiUsageTokens(nil, nil, nil), nil, 0),
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Bind 将观察者绑定到控制器已校验身份的 state 与输出目录。
// Python: bind()——账本文件存在时逐行重放：task_id 不匹配即报错、
// sequence 取历史最大 event_id、按 call_id 去重累计。
func (o *ModelUsageObserver) Bind(state map[string]any, outputDir string) error {
	o.path = filepath.Join(outputDir, "model_calls.jsonl")
	if info, err := os.Stat(o.path); err == nil && !info.IsDir() {
		stream, err := os.Open(o.path)
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
				o.accumulate(UsageTokens(tokens))
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	o.state = state
	o.updateState()
	return nil
}

// Record 记一笔模型调用。
// Python: record()——校验 model/call_id/合法 status → call_id 去重 →
// 造 EventUsage → 账本逐行追加 → sequence/seen/累计/state 更新 →
// 以解除 observer 标签的 ctx 发出事件（防服务侧工作递归入账）→
// emit 失败存 deliveryError（不经返回值抛出）。
func (o *ModelUsageObserver) Record(ctx context.Context, model, callID string, usage any, status string, durationMs *int, nodeRef, stageRef *string) error {
	// Python: if self.state is None or self.path is None: return（无活动 run 为 no-op）
	if o.state == nil || o.path == "" {
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
	stream, err := os.OpenFile(o.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	o.accumulate(event.ModelCall().Tokens())
	o.updateState()
	o.lock.Unlock()
	// Python: token = _observer.set(None) → emit → finally reset：
	// 服务侧 sink 工作不是引擎模型调用，不得在持锁期间递归进入本观察者。
	if err := Emit(withoutObserverTag(ctx), o.onEvent, event); err != nil {
		o.deliveryError = err
	}
	return nil
}

// Totals 当前累计快照（D7：控制器只读；写入方为观察者单点）。
func (o *ModelUsageObserver) Totals() *RsiUsage {
	o.lock.Lock()
	defer o.lock.Unlock()
	return o.totals
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
func (o *ModelUsageObserver) accumulate(tokens *RsiUsageTokens) {
	prev := o.totals.Tokens()
	summed := NewRsiUsageTokens(
		addCounter(prev.Input(), tokens.Input()),
		addCounter(prev.Output(), tokens.Output()),
		addCounter(prev.CacheHit(), tokens.CacheHit()),
	)
	o.totals = NewRsiUsage(summed, nil, o.totals.CallCount()+1)
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
func (o *ModelUsageObserver) updateState() {
	if o.state == nil {
		return
	}
	if o.totals.CallCount() > 0 {
		o.state["usage"] = o.totals
	} else {
		o.state["usage"] = nil
	}
	o.state["model_calls_path"] = o.path
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
