package processcli

import (
	"testing"

	"github.com/stretchr/testify/require"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// strPtr 字符串取地址（便于传 ParseStructuredOutput 的 *string）。
func strPtr(v string) *string { return &v }

// usageEvent 构造一条带 usage_metadata 的用量事件。
func usageEvent(cost float64) *swruntime.RuntimeEvent {
	return evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{"usage_metadata": map[string]any{
			"total_cost": cost,
		}},
	})
}

// TestStructuredPrompt_逐字复刻
// Python: machine_policy.py:27-35 structured_prompt
func TestStructuredPrompt_逐字复刻(t *testing.T) {
	got := StructuredPrompt("do it", map[string]any{"type": "object"})
	want := "do it\n\n" +
		"Return exactly one JSON object conforming to this JSON Schema. " +
		"Do not use Markdown fences or text outside the JSON object.\n" +
		`JSON Schema: {"type":"object"}`
	require.Equal(t, want, got, "提示词必须逐字复刻，不翻译")
}

// TestStructuredPrompt_schema紧凑序列化
// Python: machine_policy.py:29 separators=(",",":"), ensure_ascii=False
func TestStructuredPrompt_schema紧凑序列化(t *testing.T) {
	got := StructuredPrompt("x", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
		},
	})
	require.Contains(t, got, `{"properties":{"a":{"type":"string"}},"type":"object"}`,
		"必须是紧凑分隔符且 key 有序（Go map 序列化按字典序）")
	require.NotContains(t, got, ", ")
}

// TestParseStructuredOutput_非JSON报错
// Python: machine_policy.py:42-47
func TestParseStructuredOutput_非JSON报错(t *testing.T) {
	_, err := ParseStructuredOutput(strPtr("not json"), map[string]any{"type": "object"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a JSON object")
}

// TestParseStructuredOutput_非对象报错
// Python: machine_policy.py:46-47 if not isinstance(value, dict): raise
func TestParseStructuredOutput_非对象报错(t *testing.T) {
	_, err := ParseStructuredOutput(strPtr("[1,2]"), map[string]any{"type": "object"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a JSON object")
}

// TestParseStructuredOutput_不符合schema报错
// Python: machine_policy.py:48-52
func TestParseStructuredOutput_不符合schema报错(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"required":   []any{"name"},
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
	}
	_, err := ParseStructuredOutput(strPtr(`{"other":1}`), schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match output_schema")
}

// TestParseStructuredOutput_合法通过
func TestParseStructuredOutput_合法通过(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"name"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	got, err := ParseStructuredOutput(strPtr(`{"name":"ok"}`), schema)
	require.NoError(t, err)
	require.Equal(t, "ok", got["name"])
}

// TestParseStructuredOutput_空输入报错
// Python: machine_policy.py:43 json.loads(output or "")
func TestParseStructuredOutput_空输入报错(t *testing.T) {
	_, err := ParseStructuredOutput(strPtr(""), map[string]any{"type": "object"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a JSON object")
}

// TestValidateOutputSchema_非法schema报错
// Python: machine_policy.py:19-24 check_schema
func TestValidateOutputSchema_非法schema报错(t *testing.T) {
	err := ValidateOutputSchema(map[string]any{"type": "not-a-type"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a valid JSON Schema")
}

// TestValidateOutputSchema_合法schema通过
func TestValidateOutputSchema_合法schema通过(t *testing.T) {
	require.NoError(t, ValidateOutputSchema(map[string]any{"type": "object"}))
}

// TestRunLimits_超turn上限
// Python: machine_policy.py:83-84 model_calls > max_turns
func TestRunLimits_超turn上限(t *testing.T) {
	maxTurns := 2
	l := NewRunLimits(&maxTurns, nil)
	for i := 0; i < 2; i++ {
		require.Nil(t, l.Observe(usageEvent(1.0)))
	}
	got := l.Observe(usageEvent(1.0))
	require.NotNil(t, got)
	require.Equal(t, "TURN_LIMIT_EXCEEDED", *got)
}

// TestRunLimits_超预算
// Python: machine_policy.py:74-82 total_cost > max_budget_usd
func TestRunLimits_超预算(t *testing.T) {
	budget := 1.0
	l := NewRunLimits(nil, &budget)
	require.Nil(t, l.Observe(usageEvent(0.6)))
	got := l.Observe(usageEvent(0.6))
	require.NotNil(t, got)
	require.Equal(t, "BUDGET_EXCEEDED", *got)
}

// TestRunLimits_缺cost时报计量不可用
// Python: machine_policy.py:76-79 not isinstance(raw, (int,float)) → UNAVAILABLE
func TestRunLimits_缺cost时报计量不可用(t *testing.T) {
	budget := 1.0
	l := NewRunLimits(nil, &budget)
	got := l.Observe(evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{"usage_metadata": map[string]any{}},
	}))
	require.NotNil(t, got)
	require.Equal(t, "BUDGET_METER_UNAVAILABLE", *got)
}

// TestRunLimits_缺usage整段时报计量不可用
// Python: machine_policy.py:71-72 not isinstance(usage, Mapping)
func TestRunLimits_缺usage整段时报计量不可用(t *testing.T) {
	budget := 1.0
	l := NewRunLimits(nil, &budget)
	got := l.Observe(evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{},
	}))
	require.NotNil(t, got)
	require.Equal(t, "BUDGET_METER_UNAVAILABLE", *got)
}

// TestRunLimits_未设预算时缺usage不报错
// Python: machine_policy.py:72 "UNAVAILABLE" if max_budget_usd is not None else None
func TestRunLimits_未设预算时缺usage不报错(t *testing.T) {
	maxTurns := 5
	l := NewRunLimits(&maxTurns, nil)
	got := l.Observe(evt("chat.usage_metadata", map[string]any{
		"metadata": map[string]any{},
	}))
	require.Nil(t, got, "未设预算时缺 usage 不算错误")
}

// TestRunLimits_非usage事件忽略
// Python: machine_policy.py:66-67 if event_type != "chat.usage_metadata": return None
func TestRunLimits_非usage事件忽略(t *testing.T) {
	maxTurns := 1
	l := NewRunLimits(&maxTurns, nil)
	require.Nil(t, l.Observe(evt("chat.delta", map[string]any{"delta": "x"})))
	require.Equal(t, 0, l.ModelCalls(), "非 usage 事件不计数")
}

// TestRunLimits_addUsage带modelCalls
// Python: machine_policy.py:87-91 add_usage
func TestRunLimits_addUsage带modelCalls(t *testing.T) {
	maxTurns := 5
	l := NewRunLimits(&maxTurns, nil)
	_ = l.Observe(usageEvent(0.1))
	out := l.AddUsage(map[string]any{"input_tokens": float64(10)})
	require.Equal(t, 1, out["model_calls"])
	require.Equal(t, float64(10), out["input_tokens"])
}

// TestRunLimits_addUsage未设限额时不加modelCalls
// Python: machine_policy.py:89 if max_turns is not None or max_budget_usd is not None
func TestRunLimits_addUsage未设限额时不加modelCalls(t *testing.T) {
	l := NewRunLimits(nil, nil)
	out := l.AddUsage(map[string]any{"input_tokens": float64(10)})
	require.NotContains(t, out, "model_calls")
}

// TestRunLimits_addUsage不改入参
// Python: machine_policy.py:88 deepcopy(dict(usage))
func TestRunLimits_addUsage不改入参(t *testing.T) {
	maxTurns := 5
	l := NewRunLimits(&maxTurns, nil)
	in := map[string]any{"input_tokens": float64(10)}
	out := l.AddUsage(in)
	out["input_tokens"] = float64(999)
	require.Equal(t, float64(10), in["input_tokens"], "不得改动入参")
}

// TestRunLimits_负cost报计量不可用
// Python: machine_policy.py:78-79 if not isfinite or raw < 0 → UNAVAILABLE
func TestRunLimits_负cost报计量不可用(t *testing.T) {
	budget := 1.0
	l := NewRunLimits(nil, &budget)
	got := l.Observe(usageEvent(-1))
	require.NotNil(t, got)
	require.Equal(t, "BUDGET_METER_UNAVAILABLE", *got)
}
