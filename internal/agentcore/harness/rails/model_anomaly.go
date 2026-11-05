package rails

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolLoopCompactConfig 连续相同工具调用（名称+入参）的压缩配置。
//
// 返回值不参与匹配；折叠时只把最新一轮的调用与结果写进警告文本。
// Python: model_anomaly_detection_rail.py ToolLoopCompactConfig (L281-306)
type ToolLoopCompactConfig struct {
	// Enabled 是否启用压缩，默认 false（上游默认关闭）
	Enabled bool
	// ConsecutiveThreshold 连续多少轮相同即触发压缩，默认 4（>=2）
	ConsecutiveThreshold int
	// BailoutThreshold 第几次命中时兜底中止，默认 3；0 表示不中止
	BailoutThreshold int
}

// transientDecision 一次瞬时失败的分类结论。
//
// Python: _TransientDecision (L56-60)
type transientDecision struct {
	// retryable 是否值得整调用重试
	retryable bool
	// statusCode provider 返回的 HTTP 状态码
	statusCode *int
	// retryAfterSeconds 服务端建议的退避时长
	retryAfterSeconds *float64
}

// toolFingerprintItem 单次工具调用的匹配指纹（名称 + 规范化入参）。
type toolFingerprintItem struct {
	// name 工具名
	name string
	// args 规范化后的入参 JSON 文本
	args string
}

// toolRound 一轮完整的工具调用区间 [start, end)。
//
// Python: _ToolRound (L309-314)
type toolRound struct {
	// start 轮次起始消息下标（assistant 消息）
	start int
	// end 轮次结束消息下标（最后一个 tool 消息的下一条）
	end int
	// fingerprint 排序后的 (名称, 入参) 指纹
	fingerprint []toolFingerprintItem
}

// anomalyState 单次模型调用的重复检测尾窗（两个字段各自独立累计）。
//
// Python: ctx.extra["_model_anomaly_detection_state"] 的 dict（L430-433）
type anomalyState struct {
	// reasoningContent 思维链尾窗
	reasoningContent string
	// content 正文尾窗
	content string
}

// retryCounters 单个会话的三类重试计数。
type retryCounters struct {
	// repeat 重复输出重试计数
	repeat int
	// streamTimeout 流帧超时重试计数
	streamTimeout int
	// transient 瞬时失败重试计数
	transient int
}

// ModelAnomalyDetectionRail 模型异常检测 Rail。
//
// 处理四类故障：流式退化重复输出、流帧超时、瞬时 provider 失败、连续相同工具调用。
// 重试复用 RailExecutor 的整调用重试（on_model_exception → RequestRetry）。
//
// Python: ModelAnomalyDetectionRail（priority 70，L317-392）
type ModelAnomalyDetectionRail struct {
	DeepAgentRail

	// maxRetries 重复输出/流超时的最大重试次数
	maxRetries int
	// repeatMinPatternChars 重复片段最短长度（字符）
	repeatMinPatternChars int
	// repeatMaxPatternChars 重复片段最长长度（字符）
	repeatMaxPatternChars int
	// repeatMinCount 判定循环所需的最少连续重复次数
	repeatMinCount int
	// repeatMinTotalChars 多字符重复的最小总长度
	repeatMinTotalChars int
	// repeatWindowChars 重复检测滑动窗口大小（字符）
	repeatWindowChars int
	// singleCharRepeatCount 单字符连续重复阈值
	singleCharRepeatCount int
	// backoffSeconds 重复输出重试的退避表（越界取末项）
	backoffSeconds []float64
	// transientMaxRetries 瞬时失败最大重试次数
	transientMaxRetries int
	// transientBaseDelay 瞬时失败指数退避基数（秒）
	transientBaseDelay float64
	// toolLoopCompact 工具循环压缩配置
	toolLoopCompact ToolLoopCompactConfig

	// mu 保护两张会话表：同一 rail 实例会被同一 channel 下的多个会话共享
	mu sync.RWMutex
	// counters 会话 id → 三类重试计数（只存正计数，归零即删）
	counters map[string]*retryCounters
	// compactCounts 会话 id → 压缩命中计数
	compactCounts map[string]int
}

// ModelAnomalyOption Rail 构造选项。
type ModelAnomalyOption func(*ModelAnomalyDetectionRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// modelAnomalyRailPriority 与上游 priority=70 一致
	modelAnomalyRailPriority = 70
	// modelAnomalyStateKey 重复检测尾窗的 extra 键
	modelAnomalyStateKey = "_model_anomaly_detection_state"
	// repeatErrorMarker 重复输出错误消息前缀，也是识别"是否重复异常"的判据
	repeatErrorMarker = "LLM repeated stream output detected"
	// maxRetryAfterSeconds 服务端建议退避超过该值则放弃重试
	maxRetryAfterSeconds = 60.0
	// defaultTransientMaxRetries 瞬时失败默认重试次数
	defaultTransientMaxRetries = 3
	// defaultTransientBaseDelay 瞬时失败指数退避基数（秒）
	defaultTransientBaseDelay = 2.0
	// toolLoopBailoutMessage 兜底中止文案（与上游逐字一致，L270-273）
	toolLoopBailoutMessage = "Detected that the model fell into repeated tool calls; this task has been automatically aborted."
	// toolLoopWarningTemplateCN 中文警告模板（对齐上游 L244-255）
	toolLoopWarningTemplateCN = `检测到连续多轮调用了相同的工具集，且每个工具的入参也相同。
最后一轮工具调用命令如下：
---
{tool_calls}
---
最后一轮工具执行结果如下：
---
{tool_results}
---
**请跳出重复工具调用**`
	// toolLoopWarningTemplateEN 英文警告模板（对齐上游 L257-268）
	toolLoopWarningTemplateEN = `Detected consecutive turns calling the same tool set with identical arguments.
Latest-round tool calls:
---
{tool_calls}
---
Latest-round tool results:
---
{tool_results}
---
**Please break out of the repeated tool calls**`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// streamTimeoutMarkers 流帧超时错误标记（上游 Model.stream 产出的文案）
var streamTimeoutMarkers = []string{"LLM stream timeout", "stream frame timeout"}

// defaultBackoffSeconds 重复输出/流超时的退避表（秒）
var defaultBackoffSeconds = []float64{0.5, 1.0, 2.0}

// retryableStatusCodes 可重试的 HTTP 状态码
var retryableStatusCodes = map[int]bool{408: true, 409: true, 429: true}

// nonRetryableTextMarkers 命中即不重试的文本标记（对齐上游 L45-53）
var nonRetryableTextMarkers = []string{
	"insufficient_quota", "out of budget", "quota exceeded", "billing",
	"invalid api key", "context length", "maximum context",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelAnomalyDetectionRail 创建 Rail 并校验参数。
//
// Python: ModelAnomalyDetectionRail.__init__（L329-391，10 条 ValueError → 此处返回 error）
func NewModelAnomalyDetectionRail(opts ...ModelAnomalyOption) (*ModelAnomalyDetectionRail, error) {
	r := &ModelAnomalyDetectionRail{
		DeepAgentRail:         *NewDeepAgentRail(),
		maxRetries:            2,
		repeatMinPatternChars: 2,
		repeatMaxPatternChars: 64,
		repeatMinCount:        6,
		repeatMinTotalChars:   160,
		repeatWindowChars:     1024,
		singleCharRepeatCount: 100,
		backoffSeconds:        append([]float64(nil), defaultBackoffSeconds...),
		transientMaxRetries:   defaultTransientMaxRetries,
		transientBaseDelay:    defaultTransientBaseDelay,
		toolLoopCompact:       ToolLoopCompactConfig{ConsecutiveThreshold: 4, BailoutThreshold: 3},
		counters:              make(map[string]*retryCounters),
		compactCounts:         make(map[string]int),
	}
	for _, opt := range opts {
		opt(r)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	r.WithPriority(modelAnomalyRailPriority)
	return r, nil
}

// WithMaxRetries 设置重复输出/流超时的最大重试次数。
func WithMaxRetries(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.maxRetries = n }
}

// WithRepeatParams 设置重复片段检测的上下限、次数与窗口。
func WithRepeatParams(minPattern, maxPattern, minCount, minTotal, window int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) {
		r.repeatMinPatternChars = minPattern
		r.repeatMaxPatternChars = maxPattern
		r.repeatMinCount = minCount
		r.repeatMinTotalChars = minTotal
		r.repeatWindowChars = window
	}
}

// WithRepeatMinPatternChars 设置重复片段最短长度。
func WithRepeatMinPatternChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMinPatternChars = n }
}

// WithRepeatMaxPatternChars 设置重复片段最长长度。
func WithRepeatMaxPatternChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMaxPatternChars = n }
}

// WithRepeatMinCount 设置判定循环所需的最少连续重复次数。
func WithRepeatMinCount(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMinCount = n }
}

// WithRepeatMinTotalChars 设置多字符重复的最小总长度。
func WithRepeatMinTotalChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatMinTotalChars = n }
}

// WithRepeatWindowChars 设置重复检测滑动窗口大小。
func WithRepeatWindowChars(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.repeatWindowChars = n }
}

// WithSingleCharRepeatCount 设置单字符连续重复阈值。
func WithSingleCharRepeatCount(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.singleCharRepeatCount = n }
}

// WithBackoffSeconds 设置重复输出重试的退避表（越界取末项）。
func WithBackoffSeconds(seconds []float64) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) {
		r.backoffSeconds = append([]float64(nil), seconds...)
	}
}

// WithTransientMaxRetries 设置瞬时失败的最大重试次数。
func WithTransientMaxRetries(n int) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.transientMaxRetries = n }
}

// WithTransientBaseDelaySeconds 设置瞬时失败指数退避基数（秒）。
func WithTransientBaseDelaySeconds(seconds float64) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.transientBaseDelay = seconds }
}

// WithToolLoopCompact 设置工具循环压缩配置。
func WithToolLoopCompact(cfg ToolLoopCompactConfig) ModelAnomalyOption {
	return func(r *ModelAnomalyDetectionRail) { r.toolLoopCompact = cfg }
}

// BackoffDelay 返回第 retryIndex 次重试前的等待秒数，越界取末项。
//
// Python: backoff_delay (L408-415)
func (r *ModelAnomalyDetectionRail) BackoffDelay(retryIndex int) float64 {
	if len(r.backoffSeconds) == 0 {
		return 0
	}
	if retryIndex < 0 {
		retryIndex = 0
	}
	if retryIndex >= len(r.backoffSeconds) {
		retryIndex = len(r.backoffSeconds) - 1
	}
	return r.backoffSeconds[retryIndex]
}

// InspectStreamChunk 逐分片检查 reasoning_content / content 是否退化重复。
//
// Python: inspect_stream_chunk (L445-452) + _append_and_check (L483-497)
func (r *ModelAnomalyDetectionRail) InspectStreamChunk(_ context.Context, cbc *agentinterfaces.AgentCallbackContext, chunk *llmschema.AssistantMessageChunk) error {
	if chunk == nil {
		return nil
	}
	state := r.anomalyState(cbc)
	if chunk.ReasoningContent != "" {
		if err := r.appendAndCheck(state, "reasoning_content", chunk.ReasoningContent); err != nil {
			return err
		}
	}
	if text := chunk.Content.Text(); text != "" {
		if err := r.appendAndCheck(state, "content", text); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// anomalyState 取（或创建）本次模型调用的尾窗状态。
func (r *ModelAnomalyDetectionRail) anomalyState(cbc *agentinterfaces.AgentCallbackContext) *anomalyState {
	if v, ok := cbc.GetExtra(modelAnomalyStateKey); ok {
		if state, ok := v.(*anomalyState); ok && state != nil {
			return state
		}
	}
	state := &anomalyState{}
	cbc.SetExtra(modelAnomalyStateKey, state)
	return state
}

// appendAndCheck 追加文本、截断尾窗并做重复检测。
//
// Python: _append_and_check (L483-497)：窗口按字符截断，错误文案带 repr(unit)。
func (r *ModelAnomalyDetectionRail) appendAndCheck(state *anomalyState, field, text string) error {
	var tail string
	if field == "reasoning_content" {
		tail = lastRunes(state.reasoningContent+text, r.repeatWindowChars)
		state.reasoningContent = tail
	} else {
		tail = lastRunes(state.content+text, r.repeatWindowChars)
		state.content = tail
	}

	unit, count, ok := r.detectRepeatedSuffix(tail)
	if !ok {
		return nil
	}
	return exception.BuildError(exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("%s: field=%s, unit=%s, repeat_count=%d",
			repeatErrorMarker, field, pyReprString(formatUnit(unit)), count)))
}

// detectRepeatedSuffix 检测文本尾部的退化重复（先单字符，再多字符）。
//
// Python: _detect_repeated_suffix (L499-523)
func (r *ModelAnomalyDetectionRail) detectRepeatedSuffix(text string) (string, int, bool) {
	runes := []rune(text)
	if len(runes) == 0 {
		return "", 0, false
	}
	if unit, count, ok := r.detectSingleCharSuffix(runes); ok {
		return unit, count, true
	}

	maxUnitLen := r.repeatMaxPatternChars
	if limit := len(runes) / r.repeatMinCount; limit < maxUnitLen {
		maxUnitLen = limit
	}
	for unitLen := r.repeatMinPatternChars; unitLen <= maxUnitLen; unitLen++ {
		unit := string(runes[len(runes)-unitLen:])
		if strings.TrimSpace(unit) == "" || isSingleCharPattern(unit) {
			continue
		}
		required := (r.repeatMinTotalChars + unitLen - 1) / unitLen
		if required < r.repeatMinCount {
			required = r.repeatMinCount
		}
		count := 1
		for pos := len(runes) - unitLen*2; pos >= 0; pos -= unitLen {
			if string(runes[pos:pos+unitLen]) != unit {
				break
			}
			count++
		}
		if count >= required {
			return unit, count, true
		}
	}
	return "", 0, false
}

// detectSingleCharSuffix 检测末尾同字符连续重复。
//
// Python: _detect_single_char_suffix (L525-539)
func (r *ModelAnomalyDetectionRail) detectSingleCharSuffix(runes []rune) (string, int, bool) {
	last := runes[len(runes)-1]
	if strings.TrimSpace(string(last)) == "" {
		return "", 0, false
	}
	count := 1
	for i := len(runes) - 2; i >= 0; i-- {
		if runes[i] != last {
			break
		}
		count++
		if count >= r.singleCharRepeatCount {
			return string(last), count, true
		}
	}
	return "", 0, false
}

// lastRunes 取末尾 n 个字符（按 rune，与上游按字符切片一致）。
func lastRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// isSingleCharPattern 判断片段是否由同一个字符组成。
//
// Python: _is_single_char_pattern (L541-543)
func isSingleCharPattern(unit string) bool {
	runes := []rune(unit)
	for _, ch := range runes[1:] {
		if ch != runes[0] {
			return false
		}
	}
	return true
}

// formatUnit 转义换行并截断到 80 个字符。
//
// Python: _format_unit (L545-550)
func formatUnit(unit string) string {
	const limit = 80
	formatted := strings.ReplaceAll(strings.ReplaceAll(unit, "\r", `\r`), "\n", `\n`)
	runes := []rune(formatted)
	if len(runes) <= limit {
		return formatted
	}
	return string(runes[:limit]) + "..."
}

// pyReprString 按 Python str.__repr__ 的常用分支渲染（单引号 + 转义）。
//
// 用于错误文案与上游逐字一致（上游用 {unit!r}）。非可打印控制字符不展开为
// \xNN——入参已由 formatUnit 处理过 \r/\n，其余控制字符在模型输出中极罕见。
func pyReprString(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, ch := range s {
		switch ch {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// funcPtrEqual 比较两个检查器的代码指针。
//
// Go 函数值不可 == 比较，只能取代码指针；同一方法的不同 receiver 代码指针相同，
// 因此"同一 rail 双实例"会被判相等（可接受：双实例已被 factory 去重禁止）。
// 先例：agentbuilder/utils/progress.go funcPtrEqual、sys_operation/registry.go:73
func funcPtrEqual(a, b agentinterfaces.StreamChunkInspectorFunc) bool {
	if a == nil || b == nil {
		return false
	}
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// validate 对齐上游 __init__ 的 10 条参数校验（L345-372）。
func (r *ModelAnomalyDetectionRail) validate() error {
	if r.maxRetries < 0 {
		return fmt.Errorf("max_retries must be >= 0")
	}
	if r.transientMaxRetries < 0 {
		return fmt.Errorf("transient_max_retries must be >= 0")
	}
	if r.transientBaseDelay < 0 {
		return fmt.Errorf("transient_base_delay_seconds must be >= 0")
	}
	for _, delay := range r.backoffSeconds {
		if delay < 0 {
			return fmt.Errorf("backoff_seconds entries must be >= 0")
		}
	}
	if r.repeatMinPatternChars < 1 {
		return fmt.Errorf("repeat_min_pattern_chars must be >= 1")
	}
	if r.repeatMaxPatternChars < r.repeatMinPatternChars {
		return fmt.Errorf("repeat_max_pattern_chars must be >= repeat_min_pattern_chars")
	}
	if r.repeatMinCount < 2 {
		return fmt.Errorf("repeat_min_count must be >= 2")
	}
	if r.repeatMinTotalChars < 1 {
		return fmt.Errorf("repeat_min_total_chars must be >= 1")
	}
	if r.singleCharRepeatCount < 2 {
		return fmt.Errorf("single_char_repeat_count must be >= 2")
	}

	minWindow := r.repeatMaxPatternChars * r.repeatMinCount
	if r.repeatMinTotalChars > minWindow {
		minWindow = r.repeatMinTotalChars
	}
	if r.singleCharRepeatCount > minWindow {
		minWindow = r.singleCharRepeatCount
	}
	if r.repeatWindowChars < minWindow {
		return fmt.Errorf("repeat_window_chars is too small for the configured repetition thresholds")
	}

	if r.toolLoopCompact.ConsecutiveThreshold < 2 {
		r.toolLoopCompact.ConsecutiveThreshold = 2
	}
	if r.toolLoopCompact.BailoutThreshold < 0 {
		r.toolLoopCompact.BailoutThreshold = 0
	}
	return nil
}
