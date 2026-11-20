// Package detectors 重复/循环工具调用检测（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/detectors/repeat_tool.py
package detectors

import (
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// callOutcome 一次已完成调用的（调用键，结果键）对。
//
// Python: repeat_tool.py history 元素 tuple[str, str]。
type callOutcome struct {
	callKey    string
	outcomeKey string
}

// RepeatToolCallDetector 在滑动历史上检测重复或循环的工具调用。
//
// Python: repeat_tool.py RepeatToolCallDetector。记录最近 history_size 次已完成
// 调用的 (call_key, outcome_key) 对。call_key 是工具名 + 递归键排序参数的稳定
// 哈希（BEFORE_TOOL_CALL 时计算并持有到调用完成）；outcome_key 成功时是结果的
// 稳定哈希（相同结果才算循环、结果变化不算）、失败时是错误文本。
// 四档递进，边沿触发防刷屏（仅严重度上升时发射）：
//
//   - 泛重复：同一 call_key 出现 ≥ repeat_warn 次 → LOW
//   - 交替（A-B-A-B 且结果稳定）≥ pingpong_warn → MEDIUM（封顶 MEDIUM：
//     结果哈希已削减误报，但时间戳/ID 等非确定内容仍可能干扰，leader 终裁）
//   - 无进展：尾部完全相同的 (call_key, outcome) ≥ loop_block → HIGH
//   - 全局熔断：尾部完全相同 ≥ global_stop → CRITICAL
type RepeatToolCallDetector struct {
	// repeatWarn 泛重复告警阈值
	repeatWarn int
	// pingpongWarn 交替告警阈值
	pingpongWarn int
	// loopBlock 无进展循环阈值
	loopBlock int
	// globalStop 全局熔断阈值
	globalStop int
	// history 最近 history_size 个已完成调用
	history []callOutcome
	// historySize 历史上限
	historySize int
	// pendingCallKey BEFORE 时持有、完成时落账的调用键
	pendingCallKey *string
	// firedSeverity 已发射最高严重度（nil 未发射）
	firedSeverity *core.Severity
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRepeatToolCallDetector 构造重复调用检测器。
//
// Python: repeat_tool.py RepeatToolCallDetector.__init__（默认 30/10/10/20/30）。
func NewRepeatToolCallDetector(historySize, repeatWarn, pingpongWarn, loopBlock, globalStop int) *RepeatToolCallDetector {
	return &RepeatToolCallDetector{
		repeatWarn:   repeatWarn,
		pingpongWarn: pingpongWarn,
		loopBlock:    loopBlock,
		globalStop:   globalStop,
		historySize:  historySize,
	}
}

// Name 返回 detector 稳定标识。
//
// Python: repeat_tool.py RepeatToolCallDetector.name。
func (d *RepeatToolCallDetector) Name() string { return "repeat_tool_call" }

// Observe BEFORE 时持有调用键，完成时落账并分类。
//
// Python: repeat_tool.py RepeatToolCallDetector.observe。
func (d *RepeatToolCallDetector) Observe(signal *core.Signal) *core.Anomaly {
	if signal.Kind == core.SignalKindBeforeToolCall {
		key := core.StableCallHash(ptrStr(signal.ToolName), signal.ToolArgs)
		d.pendingCallKey = &key
		return nil
	}
	if signal.Kind == core.SignalKindAfterToolCall {
		return d.recordAndClassify(signal.MemberName, core.StableResultHash(signal.ToolResult))
	}
	if signal.Kind == core.SignalKindToolException {
		errText := "error"
		if signal.Error != nil {
			errText = *signal.Error
		}
		return d.recordAndClassify(signal.MemberName, errText)
	}
	return nil
}

// Reset 清空历史与挂起状态。
//
// Python: repeat_tool.py RepeatToolCallDetector.reset。
func (d *RepeatToolCallDetector) Reset() {
	d.history = d.history[:0]
	d.pendingCallKey = nil
	d.firedSeverity = nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordAndClassify 落账已完成调用并跑分档分类器。
//
// Python: repeat_tool.py RepeatToolCallDetector._record_and_classify。
func (d *RepeatToolCallDetector) recordAndClassify(member, outcome string) *core.Anomaly {
	if d.pendingCallKey == nil {
		return nil
	}
	callKey := *d.pendingCallKey
	d.pendingCallKey = nil
	d.history = append(d.history, callOutcome{callKey, outcome})
	// Python deque(maxlen=history_size)：超长丢头
	if len(d.history) > d.historySize {
		d.history = d.history[len(d.history)-d.historySize:]
	}
	severity, kind, evidence := d.classify(callKey)
	if severity == "" {
		return nil
	}
	if d.firedSeverity != nil && severity.Rank() <= d.firedSeverity.Rank() {
		return nil
	}
	d.firedSeverity = &severity
	return &core.Anomaly{
		Detector:   "repeat_tool_call",
		Kind:       kind,
		Severity:   severity,
		MemberName: member,
		Summary:    string(kind) + " detected: " + formatEvidence(evidence),
		Evidence:   evidence,
	}
}

// classify 跑四档检查，返回命中的最高严重度。
//
// Python: repeat_tool.py RepeatToolCallDetector._classify。
// severity 以 "" 表示未命中（对齐返回 None）。
func (d *RepeatToolCallDetector) classify(callKey string) (core.Severity, core.AnomalyKind, map[string]any) {
	identical := d.trailingIdentical()
	if identical >= d.globalStop {
		return core.SeverityCritical, core.AnomalyKindToolCallLoop, map[string]any{"trailing_identical": identical}
	}
	if identical >= d.loopBlock {
		return core.SeverityHigh, core.AnomalyKindToolCallLoop, map[string]any{"trailing_identical": identical}
	}
	alternation := d.trailingAlternation()
	if alternation >= d.pingpongWarn {
		return core.SeverityMedium, core.AnomalyKindToolCallLoop, map[string]any{"trailing_alternation": alternation}
	}
	repeats := 0
	for _, rec := range d.history {
		if rec.callKey == callKey {
			repeats++
		}
	}
	if repeats >= d.repeatWarn {
		return core.SeverityLow, core.AnomalyKindRepeatToolCall, map[string]any{"call_repeats": repeats}
	}
	return "", core.AnomalyKindRepeatToolCall, map[string]any{}
}

// trailingIdentical 统计尾部与最近一条完全相同的连续记录数。
//
// Python: repeat_tool.py RepeatToolCallDetector._trailing_identical。
func (d *RepeatToolCallDetector) trailingIdentical() int {
	if len(d.history) == 0 {
		return 0
	}
	last := d.history[len(d.history)-1]
	count := 0
	for i := len(d.history) - 1; i >= 0; i-- {
		if d.history[i] == last {
			count++
		} else {
			break
		}
	}
	return count
}

// trailingAlternation 统计尾部 A-B-A-B 交替游程长度（结果必须稳定）。
//
// Python: repeat_tool.py RepeatToolCallDetector._trailing_alternation。
func (d *RepeatToolCallDetector) trailingAlternation() int {
	if len(d.history) < 2 {
		return 0
	}
	// 逆序遍历等价 Python reversed(sequence)
	first := d.history[len(d.history)-1]
	second := d.history[len(d.history)-2]
	if first == second || first.callKey == second.callKey {
		return 0
	}
	count := 0
	for i := 0; i < len(d.history); i++ {
		// 逆序下标 idx = len-1-i；偶数 idx 期望 first、奇数期望 second
		idx := len(d.history) - 1 - i
		expected := first
		if i%2 == 1 {
			expected = second
		}
		if d.history[idx] == expected {
			count++
		} else {
			break
		}
	}
	return count
}

// ──────────────────────────── 非导出函数（包级） ────────────────────────────

// ptrStr 解引用字符串指针；nil 返回空串（对齐 signal.tool_name or ""）。
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formatEvidence 把 evidence map 渲染进 summary。
//
// Python: repeat_tool.py summary=f"{kind.value} detected: {evidence}"——evidence
// 是 dict，f-string 展开为 Python repr 形如 "{'call_repeats': 3}"（单引号键、
// int 值不加引号）。上游 _classify 的 evidence 恒为单键 int map，按键名固定
// 渲染顺序对齐 repr 单键形态（golden 对拍锚定，见 golden_test.go）。
func formatEvidence(evidence map[string]any) string {
	for _, key := range []string{"trailing_identical", "trailing_alternation", "call_repeats"} {
		if v, ok := evidence[key]; ok {
			return "{'" + key + "': " + strconv.Itoa(v.(int)) + "}"
		}
	}
	return ""
}
