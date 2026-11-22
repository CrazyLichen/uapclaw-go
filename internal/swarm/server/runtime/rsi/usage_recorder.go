package rsi

// UsageRecorder：progress.usage 事件聚合（内部 v3 §4.6）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/usage_recorder.py（277 行）。
//
// - Record：每次模型调用一条；服务侧按节点/任务聚合。
// - Get：出参 = web §3.4 usage 统一结构 + per_iteration/usage_by_node（§8.2）。
// - 费用单价算法（C1）为中优先级预留：CostEstimate 默认 0.0 占位。

import (
	"sort"
	"sync"
)

// usageRootNode 根节点归属 ID（Python _ROOT_NODE）。
const usageRootNode = "ROOT"

// ──────────────────────────── 结构体 ────────────────────────────

// UsageRecorder 用量归属与聚合（内存态；任务级持久化由 TaskStore 留后续版本）。
// 约束：接口返回统一 usage 结构（§3.4）；仅做归属与聚合，保证字段形状可用。
type UsageRecorder struct {
	lock sync.Mutex
	// byNode task_id → {node_id: Usage}。
	byNode map[string]map[string]Usage
	// nodeSequence task_id → 派生节点序。
	nodeSequence map[string][]string
	// cumulative Provider 累计快照流（与逐调用流分开，避免重复 P2 重复累加）。
	cumulative map[string]Usage
	// cumulativeByIteration task_id → {iteration: Usage}。
	cumulativeByIteration map[string]map[int]Usage
	// callsBySequence EventUsage.event_id 为调用序号：只累加快照水位之后的调用。
	callsBySequence map[string]map[int]Usage
	// seenCallSequence 快照推进后保留已见序号：重放调用幂等。
	seenCallSequence map[string]map[int]bool
	// afterCumulative 快照后收到的无序号调用按增量折叠进下一次变化快照。
	afterCumulative map[string]Usage
}

// NewUsageRecorder 构造记账器。
func NewUsageRecorder() *UsageRecorder {
	return &UsageRecorder{
		byNode:                map[string]map[string]Usage{},
		nodeSequence:          map[string][]string{},
		cumulative:            map[string]Usage{},
		cumulativeByIteration: map[string]map[int]Usage{},
		callsBySequence:       map[string]map[int]Usage{},
		seenCallSequence:      map[string]map[int]bool{},
		afterCumulative:       map[string]Usage{},
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Record 单次模型调用记录（对齐 Python record 全分支：序号幂等/水位判定/增量折叠）。
func (r *UsageRecorder) Record(taskID string, nodeRef *string, modelCall RsiModelCall, callSequence *int) {
	r.lock.Lock()
	defer r.lock.Unlock()
	var sequence *int
	if callSequence != nil && *callSequence > 0 {
		sequence = callSequence // _safe_positive_int
	}
	includedInCumulative := false
	if sequence != nil {
		cumulative, ok := r.cumulative[taskID]
		includedInCumulative = ok && *sequence <= cumulative.CallCount
		seen := r.seenCallSequence[taskID]
		if seen == nil {
			seen = map[int]bool{}
			r.seenCallSequence[taskID] = seen
		}
		if seen[*sequence] {
			return
		}
		seen[*sequence] = true
	}
	nodeID := usageRootNode
	if nodeRef != nil && *nodeRef != "" {
		nodeID = *nodeRef
	}
	if r.byNode[taskID] == nil {
		r.byNode[taskID] = map[string]Usage{}
	}
	usage := r.byNode[taskID][nodeID]
	if nodeID != usageRootNode {
		seq := r.nodeSequence[taskID]
		found := false
		for _, id := range seq {
			if id == nodeID {
				found = true
				break
			}
		}
		if !found {
			r.nodeSequence[taskID] = append(seq, nodeID)
		}
	}
	recorded := Usage{Tokens: modelCall.Tokens, CallCount: modelCall.CallCount}
	usage.Merge(recorded)
	r.byNode[taskID][nodeID] = usage
	if sequence != nil && !includedInCumulative {
		if r.callsBySequence[taskID] == nil {
			r.callsBySequence[taskID] = map[int]Usage{}
		}
		r.callsBySequence[taskID][*sequence] = recorded
	} else if _, hasCumulative := r.cumulative[taskID]; hasCumulative && sequence == nil {
		after := r.afterCumulative[taskID]
		after.Merge(recorded)
		r.afterCumulative[taskID] = after
	}
}

// RecordEngineEvent 从 progress.usage 事件载荷归一记录（内部 v3 §3.3）。
func (r *UsageRecorder) RecordEngineEvent(taskID string, payload map[string]any, callSequence *int) {
	modelCallRaw, _ := payload["model_call"].(map[string]any)
	if modelCallRaw == nil {
		return
	}
	tokensRaw, _ := modelCallRaw["tokens"].(map[string]any)
	if tokensRaw == nil {
		tokensRaw = map[string]any{}
	}
	modelCall := RsiModelCall{
		Model:     valueToString(modelCallRaw["model"]),
		CallCount: safeInt(modelCallRaw["call_count"]),
		Tokens: Tokens{
			Input:    safeInt(tokensRaw["input"]),
			Output:   safeInt(tokensRaw["output"]),
			CacheHit: safeInt(tokensRaw["cache_hit"]),
		},
	}
	if modelCall.CallCount == 0 {
		modelCall.CallCount = 1 // Python int(... or 1)
	}
	var nodeRef *string
	if payload["node_ref"] != nil {
		ref := valueToString(payload["node_ref"])
		nodeRef = &ref
	}
	r.Record(taskID, nodeRef, modelCall, callSequence)
}

// RecordCumulative 记录 Provider 累计快照（幂等；对齐 Python record_cumulative）。
func (r *UsageRecorder) RecordCumulative(taskID string, usage map[string]any, iteration *int) {
	parsed := usageFromValue(usage)
	if parsed == nil {
		return
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	previous, hasPrevious := r.cumulative[taskID]
	if !hasPrevious {
		r.cumulative[taskID] = *parsed
		r.afterCumulative[taskID] = Usage{}
	} else if *parsed != previous {
		// 变化快照可能吸收部分/全部无序号增量：保留已可见总量再推进快照，
		// 然后开新增量窗口。未变化的快照不得清空窗口（可能滞后）。
		current := previous
		current.Merge(r.afterCumulative[taskID])
		r.cumulative[taskID] = maxUsage(current, *parsed)
		r.afterCumulative[taskID] = Usage{}
	}
	watermark := r.cumulative[taskID].CallCount
	calls := r.callsBySequence[taskID]
	if calls != nil {
		filtered := map[int]Usage{}
		for seq, callUsage := range calls {
			if seq > watermark {
				filtered[seq] = callUsage
			}
		}
		r.callsBySequence[taskID] = filtered
	}
	if iteration != nil && *iteration > 0 {
		if r.cumulativeByIteration[taskID] == nil {
			r.cumulativeByIteration[taskID] = map[int]Usage{}
		}
		r.cumulativeByIteration[taskID][*iteration] = *parsed
	}
}

// Get “rsi.usage.get“ 出参（web §8.2）。
func (r *UsageRecorder) Get(taskID string) (map[string]any, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	nodes := r.byNode[taskID]
	_, hasCumulative := r.cumulative[taskID]
	if nodes == nil && !hasCumulative {
		return nil, NewTaskNotFound(taskID)
	}
	var total Usage
	if hasCumulative {
		total = r.cumulative[taskID]
		total.Merge(r.afterCumulative[taskID])
		watermark := r.cumulative[taskID].CallCount
		for seq, usage := range r.callsBySequence[taskID] {
			if seq > watermark {
				total.Merge(usage)
			}
		}
	} else {
		for _, usage := range nodes {
			total.Merge(usage)
		}
	}
	var perIteration []map[string]any
	if hasCumulative {
		indexes := make([]int, 0, len(r.cumulativeByIteration[taskID]))
		for idx := range r.cumulativeByIteration[taskID] {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		perIteration = []map[string]any{}
		for _, idx := range indexes {
			perIteration = append(perIteration, map[string]any{
				"iteration": idx, "usage": r.cumulativeByIteration[taskID][idx].ToDict(),
			})
		}
	} else {
		perIteration = []map[string]any{}
		seq := r.nodeSequence[taskID]
		for i, nodeID := range seq {
			u := nodes[nodeID]
			perIteration = append(perIteration, map[string]any{
				"iteration": i + 1, "usage": u.ToDict(),
			})
		}
	}
	usageByNode := map[string]any{}
	for nodeID, usage := range nodes {
		usageByNode[nodeID] = usage.ToDict()
	}
	return map[string]any{
		"usage":         total.ToDict(),
		"per_iteration": perIteration,
		"usage_by_node": usageByNode,
	}, nil
}

// UsageSummary “rsi.task.get“ 的 usage 摘要（仅当有记录；未记录返回 nil）。
func (r *UsageRecorder) UsageSummary(taskID string) map[string]any {
	out, err := r.Get(taskID)
	if err != nil {
		return nil
	}
	usage, _ := out["usage"].(map[string]any)
	return usage
}

// ──────────────────────────── 接口适配（事件消费缝） ────────────────────────────

// RecordCumulativeSink 适配 EventConsumer.UsageRecorderSink。
func (r *UsageRecorder) RecordCumulativeSink(taskID string, usage map[string]any, iteration *int) {
	r.RecordCumulative(taskID, usage, iteration)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// usageFromValue Provider 数据/dict 宽松转换（对齐 Python _usage_from_value；
// Go 侧统一 dict 形态，pydantic/dataclass 分支不适用）。
func usageFromValue(value map[string]any) *Usage {
	if value == nil {
		return nil
	}
	tokensRaw, _ := value["tokens"].(map[string]any)
	if tokensRaw == nil {
		tokensRaw = map[string]any{}
	}
	return &Usage{
		Tokens: Tokens{
			Input:    safeInt(tokensRaw["input"]),
			Output:   safeInt(tokensRaw["output"]),
			CacheHit: safeInt(tokensRaw["cache_hit"]),
		},
		CostEstimate: safeFloatVal(value["cost_estimate"]),
		CallCount:    safeInt(value["call_count"]),
	}
}

// maxUsage 快照推进时保持累计计数单调（对齐 Python _max_usage）。
func maxUsage(left, right Usage) Usage {
	return Usage{
		Tokens: Tokens{
			Input:    maxInt(left.Tokens.Input, right.Tokens.Input),
			Output:   maxInt(left.Tokens.Output, right.Tokens.Output),
			CacheHit: maxInt(left.Tokens.CacheHit, right.Tokens.CacheHit),
		},
		CostEstimate: maxFloat(left.CostEstimate, right.CostEstimate),
		CallCount:    maxInt(left.CallCount, right.CallCount),
	}
}

// maxFloat 取大。
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// safeFloatVal 宽松转 float（nil/bool → 0.0，对齐 Python _safe_float）。
func safeFloatVal(value any) float64 {
	switch v := value.(type) {
	case nil, bool:
		return 0.0
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0.0
	}
}
