package processcli

import (
	"testing"

	"github.com/stretchr/testify/require"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// deref 解引用可空字符串（nil → 空串，便于断言）。
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// evt 构造一条成功事件。event_type 与业务字段同在 payload。
func evt(eventType string, payload map[string]any) *swruntime.RuntimeEvent {
	merged := map[string]any{"event_type": eventType}
	for k, v := range payload {
		merged[k] = v
	}
	return &swruntime.RuntimeEvent{Payload: merged, OK: true}
}

// evtErr 构造一条失败事件（OK=false）。
func evtErr(eventType string, payload map[string]any) *swruntime.RuntimeEvent {
	e := evt(eventType, payload)
	e.OK = false
	return e
}

// TestRunSummary_delta累积
// Python: machine_result.py:177-180
func TestRunSummary_delta累积(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.delta", map[string]any{"delta": "hello "}))
	s.Observe(evt("chat.delta", map[string]any{"delta": "world"}))
	require.Equal(t, "hello world", deref(s.Output()))
}

// TestRunSummary_final重复全量时不重复拼接
// Python: machine_result.py:107-112 _assemble_answer
//
// deltas="hello"、final="hello world"：final 以 deltas 开头 → 只取 final，
// 不拼成 "hellohello world"（这正是「重复整段答案」不被重复拼接的含义）。
func TestRunSummary_final重复全量时不重复拼接(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.delta", map[string]any{"delta": "hello"}))
	s.Observe(evt("chat.final", map[string]any{"content": "hello world"}))
	require.Equal(t, "hello world", deref(s.Output()), "final 以 deltas 开头只取 final")
}

// TestRunSummary_final不相交时拼接
// Python: machine_result.py:110-111 if final and final not in deltas: return deltas + final
//
// 典型场景：adapter 只 flush 了尾巴，final 是完整答案且不包含该尾巴。
func TestRunSummary_final不相交时拼接(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.delta", map[string]any{"delta": "tail only"}))
	s.Observe(evt("chat.final", map[string]any{"content": "complete different answer"}))
	require.Equal(t, "tail onlycomplete different answer", deref(s.Output()))
}

// TestRunSummary_final以deltas开头则只取final
// Python: machine_result.py:108-109 if final.startswith(deltas): return final
func TestRunSummary_final以deltas开头则只取final(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.delta", map[string]any{"delta": "hello"}))
	s.Observe(evt("chat.final", map[string]any{"content": "hello world"}))
	// 让 deltas 成为 final 的前缀
	s2 := NewRunSummary()
	s2.Observe(evt("chat.delta", map[string]any{"delta": "hello "}))
	s2.Observe(evt("chat.final", map[string]any{"content": "hello world"}))
	require.Equal(t, "hello world", deref(s2.Output()), "final 以 deltas 开头时只取 final")
}

// TestRunSummary_finalAnswer排除工具前的正文
// Python: machine_result.py:181-184 chat.tool_call 清空 answerDeltas
func TestRunSummary_finalAnswer排除工具前的正文(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.delta", map[string]any{"delta": "let me check"}))
	s.Observe(evt("chat.tool_call", map[string]any{"tool_call": map[string]any{"name": "read_file"}}))
	s.Observe(evt("chat.delta", map[string]any{"delta": "the answer is 42"}))
	s.Observe(evt("chat.final", map[string]any{"content": "the answer is 42"}))

	require.Contains(t, deref(s.Output()), "let me check", "output 保留全部正文")
	require.Equal(t, "the answer is 42", deref(s.FinalAnswer()),
		"final_answer 只取最后一次工具调用之后")
}

// TestRunSummary_只记第一条错误
// Python: machine_result.py:209-210 if self._error is None and (...)
func TestRunSummary_只记第一条错误(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evtErr("chat.error", map[string]any{"error": "first"}))
	s.Observe(evtErr("chat.error", map[string]any{"error": "second"}))
	require.NotNil(t, s.Error())
	require.Equal(t, "first", s.Error().Message, "只记第一条")
}

// TestRunSummary_ok为假也算错误
// Python: machine_result.py:209 (not event.ok or event_type in _RUN_ERROR_EVENTS)
func TestRunSummary_ok为假也算错误(t *testing.T) {
	s := NewRunSummary()
	e := evt("chat.delta", map[string]any{"delta": "hi"})
	e.OK = false
	s.Observe(e)
	require.NotNil(t, s.Error(), "OK=false 应记为错误")
}

// TestRunSummary_runtimeError也算错误
// Python: machine_result.py:17 _RUN_ERROR_EVENTS = {"chat.error", "runtime.error"}
func TestRunSummary_runtimeError也算错误(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("runtime.error", map[string]any{"message": "boom"}))
	require.NotNil(t, s.Error())
	require.Equal(t, "boom", s.Error().Message)
}

// TestRunSummary_usageSummary优先于metadata
// Python: machine_result.py:191-207 has_usage_summary
func TestRunSummary_usageSummary优先于metadata(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{"usage_metadata": map[string]any{"input_tokens": float64(10)}},
	}))
	s.Observe(evt("chat.usage_summary", map[string]any{
		"usage": map[string]any{"input_tokens": float64(99)},
	}))
	require.Equal(t, float64(99), s.Usage()["input_tokens"])

	// summary 之后再来的 metadata 不得覆盖
	s.Observe(evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{"usage_metadata": map[string]any{"input_tokens": float64(7)}},
	}))
	require.Equal(t, float64(99), s.Usage()["input_tokens"], "summary 之后 metadata 不再累加")
}

// TestRunSummary_usage计数器累加且过滤非法值
// Python: machine_result.py:196-207 _valid_counter
func TestRunSummary_usage计数器累加且过滤非法值(t *testing.T) {
	s := NewRunSummary()
	for i := 0; i < 2; i++ {
		s.Observe(evt("chat.usage_metadata", map[string]any{
			"metadata": map[string]any{"usage_metadata": map[string]any{
				"input_tokens":  float64(5),
				"output_tokens": "bad", // 非法，应被过滤
			}},
		}))
	}
	require.Equal(t, float64(10), s.Usage()["input_tokens"])
	require.NotContains(t, s.Usage(), "output_tokens")
}

// TestRunSummary_负值与布尔被过滤
// Python: machine_result.py:99-104 _valid_counter（bool 不算数字、必须 >= 0）
func TestRunSummary_负值与布尔被过滤(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{"usage_metadata": map[string]any{
			"input_tokens":  float64(-1),
			"output_tokens": true,
		}},
	}))
	require.NotContains(t, s.Usage(), "input_tokens", "负数应被过滤")
	require.NotContains(t, s.Usage(), "output_tokens", "布尔应被过滤")
}

// TestRunSummary_usage是快照不被外部改
// Python: machine_result.py:164-166 deepcopy(self._usage)
func TestRunSummary_usage是快照不被外部改(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evt("chat.usage_summary", map[string]any{
		"usage": map[string]any{"input_tokens": float64(1)},
	}))
	got := s.Usage()
	got["input_tokens"] = float64(999)
	require.Equal(t, float64(1), s.Usage()["input_tokens"], "每次返回独立副本")
}

// TestRunSummary_空输出返回nil
// Python: machine_result.py:112 return deltas or final or None
func TestRunSummary_空输出返回nil(t *testing.T) {
	s := NewRunSummary()
	require.Nil(t, s.Output())
	require.Nil(t, s.FinalAnswer())
	require.Nil(t, s.Error())
	require.Empty(t, s.Usage())
}

// TestRunSummary_错误码兜底
// Python: machine_result.py:39-45 _error_code 兜底 "RUNTIME_ERROR"
func TestRunSummary_错误码兜底(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evtErr("chat.error", map[string]any{"message": "x"}))
	require.Equal(t, "RUNTIME_ERROR", s.Error().Code)
	require.Equal(t, "x", s.Error().Message)
}

// TestRunSummary_嵌套error对象优先
// Python: machine_result.py:74-86 nested = raw_error if Mapping
func TestRunSummary_嵌套error对象优先(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evtErr("chat.error", map[string]any{
		"error": map[string]any{
			"code":      "TIMEOUT",
			"message":   "nested msg",
			"retryable": true,
		},
		"message": "outer msg",
	}))
	require.Equal(t, "TIMEOUT", s.Error().Code)
	require.Equal(t, "nested msg", s.Error().Message)
	require.True(t, s.Error().Retryable)
}

// TestRunSummary_默认消息兜底
// Python: machine_result.py:64-68 _error_message 兜底 "Runtime execution failed"
func TestRunSummary_默认消息兜底(t *testing.T) {
	s := NewRunSummary()
	s.Observe(evtErr("chat.error", map[string]any{}))
	require.Equal(t, "Runtime execution failed", s.Error().Message)
}
