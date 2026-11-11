package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProtocolCapabilities_十七feature全true
// Python: capabilities.py:19-40 features = dict.fromkeys((17 项), True)
func TestProtocolCapabilities_十七feature全true(t *testing.T) {
	c := ProtocolCapabilities()
	features, ok := c["features"].(map[string]any)
	require.True(t, ok)
	require.Len(t, features, 17)
	for k, v := range features {
		require.Equal(t, true, v, "feature %s 应为 true", k)
	}
}

// TestProtocolCapabilities_关键字段
// Python: capabilities.py:41-63
func TestProtocolCapabilities_关键字段(t *testing.T) {
	c := ProtocolCapabilities()
	require.Equal(t, "0.1", c["schema_version"])
	require.EqualValues(t, 1, c["protocol_revision"])
	require.Equal(t, "utf-8", c["input_encoding"])
	require.Equal(t, "utf-8", c["output_encoding"])
	require.Equal(t, "jsonl", c["output_format"])
	require.Equal(t, MaxRunInputBytes, c["max_input_bytes"])

	ct, ok := c["control_types"].([]any)
	require.True(t, ok)
	require.ElementsMatch(t, []any{"answer", "tool_result", "cancel"}, ct)

	st, ok := c["stable_event_types"].([]any)
	require.True(t, ok)
	require.Len(t, st, 6)
}

// TestProtocolCapabilities_runFields来自schema
// Python: capabilities.py:43 "run_fields": list(schema["$defs"]["run"]["properties"])
func TestProtocolCapabilities_runFields来自schema(t *testing.T) {
	c := ProtocolCapabilities()
	rf, ok := c["run_fields"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, rf)
	require.Contains(t, rf, "input")
	require.Contains(t, rf, "session_id")
}

// TestProtocolCapabilities_queryOperations来自schema
// Python: capabilities.py:44 schema["$defs"]["query"]["properties"]["operation"]["enum"]
func TestProtocolCapabilities_queryOperations来自schema(t *testing.T) {
	c := ProtocolCapabilities()
	qo, ok := c["query_operations"].([]any)
	require.True(t, ok)
	require.Len(t, qo, 10)
	require.Contains(t, qo, "protocol.capabilities")
	require.Contains(t, qo, "session.list")
}

// TestProtocolCapabilities_compatibility五条
// Python: capabilities.py:52-63
func TestProtocolCapabilities_compatibility五条(t *testing.T) {
	c := ProtocolCapabilities()
	compat, ok := c["compatibility"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "reject", compat["unknown_input_fields"])
	require.Equal(t, "ignore", compat["unknown_output_fields"])
	require.Equal(t, "ignore", compat["unknown_event_types"])
}
