package recommendation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPythonTruthy_Python真值语义
// Python: topic_starter.py:486 event_key_of 的 `if event.get("feedback_record_id"):`
func TestPythonTruthy_Python真值语义(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil", nil, false},
		{"空串", "", false},
		{"非空串", "x", true},
		{"零整数", 0, false},
		{"非零整数", 1, true},
		{"零浮点", 0.0, false},
		{"非零浮点", 0.5, true},
		{"false", false, false},
		{"true", true, true},
		{"空切片", []any{}, false},
		{"非空切片", []any{1}, true},
		{"空 map", map[string]any{}, false},
		{"非空 map", map[string]any{"a": 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, pythonTruthy(c.value))
		})
	}
}

// TestPyStr_Python的str语义
// Python: topic_starter.py:483 `str(event.get("session_id") or "")`
func TestPyStr_Python的str语义(t *testing.T) {
	assert.Equal(t, "", pyStr(nil))
	assert.Equal(t, "", pyStr(""))
	assert.Equal(t, "abc", pyStr("abc"))
	assert.Equal(t, "1", pyStr(1))
	assert.Equal(t, "1", pyStr(int64(1)))
	assert.Equal(t, "1", pyStr(float64(1)))
	assert.Equal(t, "1.5", pyStr(1.5))
	assert.Equal(t, "True", pyStr(true))
	assert.Equal(t, "False", pyStr(false))
}

// TestMarshalNoEscape_不转义HTML且无尾换行
// Python: json.dumps(value, ensure_ascii=False, separators=(",", ":"))
func TestMarshalNoEscape_不转义HTML且无尾换行(t *testing.T) {
	out, err := marshalNoEscape(map[string]any{"k": "<中文&>"})
	assert.NoError(t, err)
	assert.Equal(t, `{"k":"<中文&>"}`, string(out))
	assert.NotContains(t, string(out), "\n")
}

// TestRetentionDays_16项与上游一致
// Python: topic_starter.py:80-95 _RETENTION_DAYS
func TestRetentionDays_16项与上游一致(t *testing.T) {
	assert.Len(t, retentionDays, 16)
	assert.Equal(t, 30, retentionDays["dynamic_exposure"])
	assert.Equal(t, 45, retentionDays["starter_slate_manifest"])
	assert.Equal(t, 45, retentionDays["starter_card"])
	assert.Equal(t, 180, retentionDays["provider_invocation"])
	assert.Equal(t, 90, retentionDays["session_summary_coverage"])
	assert.Equal(t, 30, retentionDays["store_pruned"])
	// session_summary 不在表内，永不按龄删除
	_, has := retentionDays["session_summary"]
	assert.False(t, has)
}

// TestEventPayloadQueryFields_13项与上游一致
// Python: topic_starter.py:57-71 _EVENT_PAYLOAD_QUERY_FIELDS
func TestEventPayloadQueryFields_13项与上游一致(t *testing.T) {
	assert.Len(t, eventPayloadQueryFields, 13)
	for _, field := range []string{
		"card_id", "context_scope", "generation_trigger_key", "negative_preference_version",
		"project_id", "serving_compatibility_key", "short_intent_version", "source_starter_id",
		"model", "provider_response_id", "status",
		"generation_trigger_key.context_scope", "generation_trigger_key.model",
	} {
		_, ok := eventPayloadQueryFields[field]
		assert.True(t, ok, "缺少字段 %s", field)
	}
}
