package processcli

import (
	"math"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunSummary 只保留助手文本、用量快照与第一条运行级失败。
// Python: machine_result.py:115-210 RunSummary
//
// Runtime 的完成标记只是观测，不是命令结果。调用方必须先排空/关闭流并
// 清理 Runtime，再发出唯一的终态结果。此处不保留任何事件、传输元数据或工具输出。
type RunSummary struct {
	// deltas 累积的全部正文
	deltas strings.Builder
	// final 最近一次 chat.final 的正文
	final string
	// answerDeltas 最后一次工具调用之后的正文
	answerDeltas strings.Builder
	// answerFinal 最后一次工具调用之后的 final
	answerFinal string
	// usage 用量快照
	usage map[string]any
	// hasUsageSummary 是否已收到 usage_summary（之后忽略 usage_metadata）
	hasUsageSummary bool
	// err 第一条运行级错误
	err *protocol.RuntimeErrorInfo
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// runErrorEvents 视为运行级错误的事件类型。
	// Python: machine_result.py:17 _RUN_ERROR_EVENTS
	runErrorEvents = map[string]struct{}{
		"chat.error":    {},
		"runtime.error": {},
	}
	// usageCounters 参与累加的用量计数器。
	// Python: machine_result.py:18-28 _USAGE_COUNTERS
	usageCounters = []string{
		"input_tokens",
		"output_tokens",
		"total_tokens",
		"cache_tokens",
		"input_cost",
		"output_cost",
		"total_cost",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRunSummary 构造归约器。
// Python: machine_result.py:134-141 RunSummary.__init__
func NewRunSummary() *RunSummary {
	return &RunSummary{usage: map[string]any{}}
}

// Output 拼接 deltas 与 final，不丢掉未 flush 的尾巴。
// Python: machine_result.py:143-151 RunSummary.output
//
// 既有 adapter 可能在 chat.final 里重复整段答案，也可能只 flush 一个尾巴。
// 包含关系匹配它的 run-answer 拼装方式；前缀分支处理「deltas 只发了一部分
// 后补一条完整 final」的情况。
func (s *RunSummary) Output() *string {
	return assembleAnswer(s.deltas.String(), s.final)
}

// FinalAnswer 返回最后一次工具调用之后的答案，排除工具前置说明。
// Python: machine_result.py:153-161 RunSummary.final_answer
//
// 结构化输出把这一段助手文本作为整体校验：不得从散文里抽 JSON 子串，
// 也不得丢弃未 flush 的尾巴。完整文本输出与可观测事件仍然可用。
func (s *RunSummary) FinalAnswer() *string {
	return assembleAnswer(s.answerDeltas.String(), s.answerFinal)
}

// Usage 返回脱离的用量快照，绝不是上下文占用。
// Python: machine_result.py:163-166 RunSummary.usage
func (s *RunSummary) Usage() map[string]any {
	out := make(map[string]any, len(s.usage))
	for k, v := range s.usage {
		out[k] = v
	}
	return out
}

// Error 返回第一条运行级错误，即使后续文本看起来像成功。
// Python: machine_result.py:168-171 RunSummary.error
func (s *RunSummary) Error() *protocol.RuntimeErrorInfo {
	return s.err
}

// Observe 消费一条事件，不改动 Runtime 拥有的 payload。
// Python: machine_result.py:173-210 RunSummary.observe
func (s *RunSummary) Observe(event *swruntime.RuntimeEvent) {
	if event == nil {
		return
	}
	// Python: :175 payload = event.payload if isinstance(event.payload, Mapping) else {}
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	eventType := event.EventType()

	// Python: :177-189 文本归约
	switch eventType {
	case "chat.delta":
		text := resultText(payload)
		s.deltas.WriteString(text)
		s.answerDeltas.WriteString(text)
	case "chat.tool_call":
		// Python: :182-184 清空 answer 栈
		s.answerDeltas.Reset()
		s.answerFinal = ""
	case "chat.final":
		final := resultText(payload)
		if final != "" {
			s.final = final
			s.answerFinal = final
		}
	}

	// Python: :191-207 用量归约
	if eventType == "chat.usage_summary" {
		// Python: :192-195 usage = payload.get("usage")
		if usage, ok := payload["usage"].(map[string]any); ok {
			s.usage = copyAnyMap(usage)
			s.hasUsageSummary = true
		}
	} else if eventType == "chat.usage_metadata" && !s.hasUsageSummary {
		// Python: :196-207 metadata = payload.get("metadata"); usage = metadata.get("usage_metadata")
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata != nil {
			if usage, ok := metadata["usage_metadata"].(map[string]any); ok {
				for _, key := range usageCounters {
					if value, valid := validCounter(usage[key]); valid {
						s.usage[key] = counterValue(s.usage[key]) + value
					}
				}
			}
		}
	}

	// Python: :209-210 if self._error is None and (not event.ok or event_type in _RUN_ERROR_EVENTS)
	if s.err == nil {
		_, isRunError := runErrorEvents[eventType]
		if !event.OK || isRunError {
			s.err = errorInfo(event, payload)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resultText 依次取 delta/content/text/message/answer 的第一个非空字符串。
// Python: machine_result.py:31-36 _text
//
// 注意与 protocol.contracts 的 _text 差别：此处判 `isinstance(value, str) and value`，
// 即空串不算命中（contracts 版不判非空）。
func resultText(payload map[string]any) string {
	for _, key := range []string{"delta", "content", "text", "message", "answer"} {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// assembleAnswer 拼装 deltas 与 final。
// Python: machine_result.py:107-112 _assemble_answer
func assembleAnswer(deltas, final string) *string {
	// Python: :108-109 if final and final.startswith(deltas): return final
	if final != "" && strings.HasPrefix(final, deltas) {
		return &final
	}
	// Python: :110-111 if final and final not in deltas: return deltas + final
	if final != "" && !strings.Contains(deltas, final) {
		combined := deltas + final
		return &combined
	}
	// Python: :112 return deltas or final or None
	if deltas != "" {
		return &deltas
	}
	if final != "" {
		return &final
	}
	return nil
}

// errorInfo 从事件构造传输安全的错误详情。
// Python: machine_result.py:71-96 _error_info
func errorInfo(event *swruntime.RuntimeEvent, payload map[string]any) *protocol.RuntimeErrorInfo {
	// Python: :74-76 raw_error = payload.get("error"); nested = raw_error if Mapping
	rawError := payload["error"]
	nested, _ := rawError.(map[string]any)
	if nested == nil {
		nested = map[string]any{}
	}
	// Python: :73 metadata = event.metadata if isinstance(...) else {}
	// Go 侧 metadata 在 payload 内
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	// Python: :76 sources = (nested, payload, metadata)
	sources := []map[string]any{nested, payload, metadata}

	info := &protocol.RuntimeErrorInfo{
		Code:      errorCode(sources),
		Message:   errorMessage(nested["message"], rawError, payload["message"], payload["content"], metadata["message"]),
		Retryable: errorRetryable(sources),
		Details:   errorDetails(sources),
	}
	// Python: :89-96 构造失败时丢弃 details，不得让可选诊断掩盖执行失败，
	// 也不得把任意 Runtime 对象序列化进机器结果。
	if frozen, err := protocol.FreezeJson("details", info.Details); err != nil {
		info.Details = nil
	} else if obj, ok := frozen.(map[string]any); ok {
		info.Details = obj
	} else {
		info.Details = nil
	}
	return info
}

// errorCode 依次在多个来源里找 code / error_code。
// Python: machine_result.py:39-45 _error_code
func errorCode(sources []map[string]any) string {
	for _, source := range sources {
		for _, key := range []string{"code", "error_code"} {
			if value, ok := source[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return "RUNTIME_ERROR"
}

// errorRetryable 依次在多个来源里找 retryable。
// Python: machine_result.py:48-53 _error_retryable
func errorRetryable(sources []map[string]any) bool {
	for _, source := range sources {
		if value, ok := source["retryable"].(bool); ok {
			return value
		}
	}
	return false
}

// errorDetails 依次在多个来源里找 details。
// Python: machine_result.py:56-61 _error_details
func errorDetails(sources []map[string]any) map[string]any {
	for _, source := range sources {
		if value, ok := source["details"].(map[string]any); ok {
			return value
		}
	}
	return map[string]any{}
}

// errorMessage 取第一个非空字符串候选，否则兜底。
// Python: machine_result.py:64-68 _error_message
func errorMessage(candidates ...any) string {
	for _, value := range candidates {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return "Runtime execution failed"
}

// validCounter 用量计数是否合法（布尔不算数字、必须有限且非负）。
// Python: machine_result.py:99-104 _valid_counter
func validCounter(value any) (float64, bool) {
	switch v := value.(type) {
	case bool:
		// Python: isinstance(value, bool) 优先，bool 是 int 子类但被排除
		return 0, false
	case int:
		if v < 0 {
			return 0, false
		}
		return float64(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return float64(v), true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0, false
		}
		return v, true
	default:
		return 0, false
	}
}

// counterValue 已累积值的数值视图（缺失或非法按 0）。
func counterValue(value any) float64 {
	if v, ok := validCounter(value); ok {
		return v
	}
	return 0
}

// copyAnyMap 浅拷贝 map（顶层即可，用量计数器都是标量）。
func copyAnyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
