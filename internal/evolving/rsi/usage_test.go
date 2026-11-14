package rsi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// TestUsageTokens_多键回退 归一化按 Python 键序回退且不估算
func TestUsageTokens_多键回退(t *testing.T) {
	// OpenAI 风格 + prompt_tokens_details.cached_tokens 回退
	got := UsageTokens(map[string]any{
		"prompt_tokens":         100,
		"completion_tokens":     30,
		"prompt_tokens_details": map[string]any{"cached_tokens": 40},
	})
	if got.Input() == nil || *got.Input() != 100 || got.Output() == nil || *got.Output() != 30 {
		t.Fatalf("input/output 回退不符: %+v", got)
	}
	if got.CacheHit() == nil || *got.CacheHit() != 40 {
		t.Fatalf("cache_hit 应回退到 cached_tokens: %+v", got)
	}
}

// TestUsageTokens_核心直接键 直接键（input_tokens/cache_read_tokens）优先于回退键
func TestUsageTokens_核心直接键(t *testing.T) {
	got := UsageTokens(map[string]any{
		"input_tokens":      10,
		"output_tokens":     20,
		"cache_read_tokens": 3,
	})
	if *got.Input() != 10 || *got.Output() != 20 || *got.CacheHit() != 3 {
		t.Fatalf("直接键不符: %+v", got)
	}
}

// TestUsageTokens_全缺失不估算 缺失计数全 nil，不得估算
func TestUsageTokens_全缺失不估算(t *testing.T) {
	empty := UsageTokens(map[string]any{})
	if empty.Input() != nil || empty.Output() != nil || empty.CacheHit() != nil {
		t.Fatal("缺失计数应全 nil")
	}
}

// TestUsageTokens_非负整数校验 bool/负数不是合法计数器（Python: int 且非 bool 且 >=0）
func TestUsageTokens_非负整数校验(t *testing.T) {
	bad := UsageTokens(map[string]any{"input_tokens": true, "output_tokens": -5})
	if bad.Input() != nil || bad.Output() != nil {
		t.Fatal("bool/负数不应计入计数器")
	}
}

// TestUsageTokens_UsageMetadata输入 llmschema.UsageMetadata 归一化（model_dump 等价）
func TestUsageTokens_UsageMetadata输入(t *testing.T) {
	cached := 9
	md := &llmschema.UsageMetadata{
		InputTokens:     100,
		OutputTokens:    30,
		CacheReadTokens: &cached,
	}
	got := UsageTokens(md)
	if got.Input() == nil || *got.Input() != 100 || got.Output() == nil || *got.Output() != 30 {
		t.Fatalf("UsageMetadata input/output 不符: %+v", got)
	}
	if got.CacheHit() == nil || *got.CacheHit() != 9 {
		t.Fatalf("UsageMetadata cache 不符: %+v", got)
	}
}

// TestUsageTokens_嵌套details缓存 无直接缓存键时回退 prompt_tokens_details.cached_tokens
func TestUsageTokens_嵌套details缓存(t *testing.T) {
	got := UsageTokens(map[string]any{
		"input_tokens":          5,
		"prompt_tokens_details": map[string]any{"cached_tokens": 2},
	})
	if got.CacheHit() == nil || *got.CacheHit() != 2 {
		t.Fatalf("嵌套缓存回退不符: %+v", got)
	}
	// 旧 cache_tokens 可能指写入：不得标为命中
	legacy := UsageTokens(map[string]any{"cache_tokens": 7})
	if legacy.CacheHit() != nil {
		t.Fatal("legacy cache_tokens 不得标为命中")
	}
}

// TestUsageSnapshot_从持久化状态装载 snapshot 仅从 Mapping 装载
func TestUsageSnapshot_从持久化状态装载(t *testing.T) {
	if UsageSnapshot(nil) != nil {
		t.Fatal("非 Mapping 返回 nil")
	}
	snap := UsageSnapshot(map[string]any{
		"tokens":     map[string]any{"input_tokens": 5},
		"call_count": 3,
	})
	if snap == nil || snap.Tokens().Input() == nil || *snap.Tokens().Input() != 5 {
		t.Fatal("快照装载不符")
	}
	if snap.CallCount() != 3 {
		t.Fatal("call_count 不符")
	}
	if snap.CostEstimate() != nil {
		t.Fatal("cost_estimate 恒 nil")
	}
	// call_count 缺省为 0
	snap2 := UsageSnapshot(map[string]any{"tokens": map[string]any{}})
	if snap2.CallCount() != 0 {
		t.Fatal("call_count 缺省应为 0")
	}
}

// TestObserverRecord_去重与累计 同 call_id 二次记录幂等；缺计数不清零
func TestObserverRecord_去重与累计(t *testing.T) {
	dir := t.TempDir()
	o := NewModelUsageObserver(nil)
	state := map[string]any{"task_id": "t1"}
	if err := o.Bind(state, dir); err != nil {
		t.Fatal(err)
	}
	if err := o.Record(context.Background(), "m1", "c1", map[string]any{"input_tokens": 10, "output_tokens": 20}, "succeeded", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// 同 call_id 二次记录幂等（Python: call_id in self.seen → return）
	if err := o.Record(context.Background(), "m1", "c1", map[string]any{"input_tokens": 10, "output_tokens": 20}, "succeeded", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	totals := o.Totals()
	if totals.CallCount() != 1 || totals.Tokens().Input() == nil || *totals.Tokens().Input() != 10 {
		t.Fatalf("去重后应只计 1 次: %+v", totals)
	}
	// 缺计数保留累计：第二次调用只报 output
	if err := o.Record(context.Background(), "m1", "c2", map[string]any{"output_tokens": 5}, "succeeded", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if o.Totals().Tokens().Input() == nil || *o.Totals().Tokens().Input() != 10 {
		t.Fatal("缺 input 计数不得清零既有累计")
	}
	if o.Totals().Tokens().Output() == nil || *o.Totals().Tokens().Output() != 25 {
		t.Fatal("output 累计应为 25")
	}
	// state 原地更新（D7）
	if state["usage"] == nil {
		t.Fatal("state['usage'] 应被更新")
	}
	if state["model_calls_path"] == "" {
		t.Fatal("state['model_calls_path'] 应被更新")
	}
}

// TestObserverBind_流水账重放 账本文件恢复 sequence/seen/totals；task_id 不匹配报错
func TestObserverBind_流水账重放(t *testing.T) {
	dir := t.TempDir()
	o1 := NewModelUsageObserver(nil)
	if err := o1.Bind(map[string]any{"task_id": "t1"}, dir); err != nil {
		t.Fatal(err)
	}
	if err := o1.Record(context.Background(), "m1", "c1", map[string]any{"input_tokens": 7}, "succeeded", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	o2 := NewModelUsageObserver(nil)
	if err := o2.Bind(map[string]any{"task_id": "t1"}, dir); err != nil {
		t.Fatal(err)
	}
	if o2.Totals().CallCount() != 1 || o2.Totals().Tokens().Input() == nil || *o2.Totals().Tokens().Input() != 7 {
		t.Fatalf("重放累计不符: %+v", o2.Totals())
	}
	// 重放后同 call_id 不再计（幂等）
	if err := o2.Record(context.Background(), "m1", "c1", map[string]any{"input_tokens": 100}, "succeeded", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if o2.Totals().CallCount() != 1 {
		t.Fatal("重放后同 call_id 应幂等")
	}
	// task_id 不匹配必须报错（Python: ValueError）
	o3 := NewModelUsageObserver(nil)
	if err := o3.Bind(map[string]any{"task_id": "other"}, dir); err == nil {
		t.Fatal("task_id 不匹配应报错")
	}
}

// TestObserverRecord_参数校验 空 model/call_id 或非法 status 报错
func TestObserverRecord_参数校验(t *testing.T) {
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := o.Record(context.Background(), "", "c1", nil, "succeeded", nil, nil, nil); err == nil {
		t.Fatal("空 model 应报错")
	}
	if err := o.Record(context.Background(), "m", "", nil, "succeeded", nil, nil, nil); err == nil {
		t.Fatal("空 call_id 应报错")
	}
	if err := o.Record(context.Background(), "m", "c1", nil, "bogus", nil, nil, nil); err == nil {
		t.Fatal("非法 status 应报错")
	}
}

// TestObserverRecord_未绑定静默跳过 无 state/path 时记录为 no-op（Python: return）
func TestObserverRecord_未绑定静默跳过(t *testing.T) {
	o := NewModelUsageObserver(nil)
	if err := o.Record(context.Background(), "m", "c", nil, "succeeded", nil, nil, nil); err != nil {
		t.Fatal("未绑定应静默")
	}
}

// TestObserverRecord_落盘格式 账本 JSONL 每行含 event_id/task_id/call_id
func TestObserverRecord_落盘格式(t *testing.T) {
	dir := t.TempDir()
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, dir); err != nil {
		t.Fatal(err)
	}
	if err := o.Record(context.Background(), "m1", "c1", map[string]any{"input_tokens": 3}, "succeeded", intPtr(120), nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "model_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(firstJSONLine(t, data), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["task_id"] != "t1" || rec["call_id"] != "c1" || rec["event_id"].(float64) != 1 {
		t.Fatalf("账本字段不符: %s", data)
	}
}

// TestObserverDeliveryError_回调失败暂存 emit 错误暂存、CheckDelivery 抛出
func TestObserverDeliveryError_回调失败暂存(t *testing.T) {
	boom := context.DeadlineExceeded
	o := NewModelUsageObserver(func(ctx context.Context, e EngineEvent) error { return boom })
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// 记账成功、emit 失败：错误暂存不吞（Python: self.delivery_error = exc）
	if err := o.Record(context.Background(), "m", "c", map[string]any{}, "succeeded", nil, nil, nil); err != nil {
		t.Fatalf("emit 失败不应从 Record 返回: %v", err)
	}
	if err := o.CheckDelivery(); !errors.Is(err, boom) {
		t.Fatalf("CheckDelivery 应抛出暂存错误，得到 %v", err)
	}
}

// TestObserverCheckDelivery_无错误为nil 正常路径 CheckDelivery 为 nil
func TestObserverCheckDelivery_无错误为nil(t *testing.T) {
	o := NewModelUsageObserver(nil)
	if o.CheckDelivery() != nil {
		t.Fatal("无投递错误应返回 nil")
	}
}

// firstJSONLine 取数据中第一个非空行。
func firstJSONLine(t *testing.T, data []byte) []byte {
	t.Helper()
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			return line
		}
	}
	t.Fatal("无 JSON 行")
	return nil
}

// ──────────────────────────── Task 5：ctx 标签/公共钩子/observe ────────────────────────────

// TestSetUsageNode_无观察者忽略 无 observer 时 set_usage_node 静默不改 ctx
func TestSetUsageNode_无观察者忽略(t *testing.T) {
	ctx := context.Background()
	got := SetUsageNode(ctx, "h0")
	if NodeRefFromContext(got) != nil {
		t.Fatal("无 observer 时不得设置节点标签")
	}
}

// TestSetUsageNode_有观察者生效 observer 在场时节点标签生效
func TestSetUsageNode_有观察者生效(t *testing.T) {
	o := NewModelUsageObserver(nil)
	ctx := withObserverTag(context.Background(), o)
	got := SetUsageNode(ctx, "h1")
	if NodeRefFromContext(got) == nil || *NodeRefFromContext(got) != "h1" {
		t.Fatal("节点标签应生效")
	}
}

// TestRecordModelUsage_无活动运行无操作 未挂 observer 时公共钩子 no-op
func TestRecordModelUsage_无活动运行无操作(t *testing.T) {
	if err := RecordModelUsage(context.Background(), "m", "c", nil, "succeeded", nil, nil); err != nil {
		t.Fatal("无活动 run 应 no-op")
	}
}

// TestRecordModelUsage_经钩子入账 observer 在场时公共钩子正常入账（带 ctx 节点归属）
func TestRecordModelUsage_经钩子入账(t *testing.T) {
	dir := t.TempDir()
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, dir); err != nil {
		t.Fatal(err)
	}
	ctx := withObserverTag(context.Background(), o)
	ctx = SetUsageNode(ctx, "h2")
	if err := RecordModelUsage(ctx, "m1", "c9", map[string]any{"input_tokens": 4}, "succeeded", nil, nil); err != nil {
		t.Fatal(err)
	}
	if o.Totals().CallCount() != 1 {
		t.Fatal("钩子应入账")
	}
	// 幂等：同 call_id 重复导入不再计
	if err := RecordModelUsage(ctx, "m1", "c9", map[string]any{"input_tokens": 4}, "succeeded", nil, nil); err != nil {
		t.Fatal(err)
	}
	if o.Totals().CallCount() != 1 {
		t.Fatal("钩子重复导入应幂等")
	}
}

// TestModelUsageStage_阶段归属 阶段作用域进出场（Python model_usage_stage 装饰器）
func TestModelUsageStage_阶段归属(t *testing.T) {
	ctx, done := ModelUsageStage(context.Background(), "stage1")
	if StageRefFromContext(ctx) == nil || *StageRefFromContext(ctx) != "stage1" {
		t.Fatal("阶段 ctx 应携带 stage_ref")
	}
	done()
	if StageRefFromContext(context.Background()) != nil {
		t.Fatal("外层不应携带 stage_ref")
	}
	// 无 observer 时不覆盖节点（Python: 装饰器只归属操作，节点仍由 set_usage_node 管理）
	if NodeRefFromContext(ctx) != nil {
		t.Fatal("阶段作用域不得设置节点")
	}
}

// TestObserverObserve_回调记账闭环 五类 LLM 事件经回调框架触发后全部入账
func TestObserverObserve_回调记账闭环(t *testing.T) {
	fw := callback.GetCallbackFramework()
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := o.Observe(context.Background())
	defer cancel()
	// 真实链路中 Model.Invoke 已开调用作用域（17.1a），测试同构
	ctx, _ = llm.EnterCallScope(ctx, true)
	// ① invoke input（进 pending）② invoke output（完成，usage 来自 UsageMetadata）
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMInvokeInput, ModelName: "m1"})
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMInvokeOutput, ModelName: "m1", Response: invokeResultWithUsage()})
	if o.Totals().CallCount() != 1 {
		t.Fatalf("invoke 链应记 1 笔: %d", o.Totals().CallCount())
	}
	if o.Totals().Tokens().Input() == nil || *o.Totals().Tokens().Input() != 11 {
		t.Fatalf("invoke 链 input 计数不符: %+v", o.Totals())
	}
	// ③ error 链：新一次 invoke 有新 call_id（Python LlmCallScope 每次生成新 uuid）
	ctx2, _ := llm.EnterCallScope(ctx, true)
	fw.TriggerLLM(ctx2, &callback.LLMCallEventData{Event: callback.LLMInvokeInput, ModelName: "m1"})
	fw.TriggerLLM(ctx2, &callback.LLMCallEventData{Event: callback.LLMCallError, ModelName: "m1", Error: context.DeadlineExceeded})
	if o.Totals().CallCount() != 2 {
		t.Fatal("error 链应补记 1 笔")
	}
	if err := o.CheckDelivery(); err != nil {
		t.Fatal(err)
	}
}

// TestObserverObserve_LLMOutput链 output 事件（usage 直接在 data.Usage）与去重
func TestObserverObserve_LLMOutput链(t *testing.T) {
	fw := callback.GetCallbackFramework()
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := o.Observe(context.Background())
	defer cancel()
	ctx, _ = llm.EnterCallScope(ctx, true)
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMStreamInput, ModelName: "m1"})
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMOutput, ModelName: "m1", Usage: usageMetadataOf(6, 2)})
	// 重复回调（native 客户端 + Model 包装双触发）被 seen 去重吸收
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMOutput, ModelName: "m1", Usage: usageMetadataOf(6, 2)})
	if o.Totals().CallCount() != 1 {
		t.Fatalf("output 链应记 1 笔: %d", o.Totals().CallCount())
	}
}

// TestObserverFinishPending_中断补记 未完成调用补记 incomplete（不编造成功）
func TestObserverFinishPending_中断补记(t *testing.T) {
	fw := callback.GetCallbackFramework()
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := o.Observe(context.Background())
	ctx, _ = llm.EnterCallScope(ctx, true)
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMStreamInput, ModelName: "m1"})
	if err := o.FinishPending(); err != nil {
		t.Fatal(err)
	}
	if o.Totals().CallCount() != 1 {
		t.Fatal("finish_pending 应补记中断调用")
	}
	cancel()
}

// TestObserve_注销后不再记账 cancel 后触发事件不入账（Python finally unregister）
func TestObserve_注销后不再记账(t *testing.T) {
	fw := callback.GetCallbackFramework()
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := o.Observe(context.Background())
	cancel()
	fw.TriggerLLM(ctx, &callback.LLMCallEventData{Event: callback.LLMInvokeOutput, ModelName: "m1", Response: invokeResultWithUsage()})
	if o.Totals().CallCount() != 0 {
		t.Fatal("注销后不得记账")
	}
}

// invokeResultWithUsage 构造带用量元数据的 AssistantMessage（invoke output 载荷）。
func invokeResultWithUsage() *llmschema.AssistantMessage {
	msg := &llmschema.AssistantMessage{}
	msg.UsageMetadata = usageMetadataOf(11, 4)
	return msg
}

// usageMetadataOf 构造指定计数的用量元数据。
func usageMetadataOf(input, output int) *llmschema.UsageMetadata {
	return &llmschema.UsageMetadata{InputTokens: input, OutputTokens: output}
}

// TestObserverTotals_并发读无竞争 D7：记录方单写、控制器并发只读 Totals（-race 固化）
func TestObserverTotals_并发读无竞争(t *testing.T) {
	o := NewModelUsageObserver(nil)
	if err := o.Bind(map[string]any{"task_id": "t1"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				snap := o.Totals() // atomic 读，与写入并发
				if snap == nil {
					t.Error("快照不得为 nil")
					return
				}
			}
			done <- struct{}{}
		}()
	}
	for j := 0; j < 20; j++ {
		if err := o.Record(context.Background(), "m", "c"+strconv.Itoa(j), map[string]any{"input_tokens": 1}, "succeeded", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if o.Totals().CallCount() != 20 {
		t.Fatalf("并发读下记账数应完整: %d", o.Totals().CallCount())
	}
}
