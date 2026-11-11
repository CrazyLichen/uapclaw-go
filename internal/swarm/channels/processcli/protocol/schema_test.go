package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProtocolSchema_可解析且含关键字段
// Python: schema.py:15-18 protocol_schema() 从包内读 schema.json
func TestProtocolSchema_可解析且含关键字段(t *testing.T) {
	s := ProtocolSchema()
	require.NotNil(t, s)
	require.Equal(t, "urn:jiuwenswarm:process-cli:0.1:revision:1", s["$id"])
	require.EqualValues(t, 1, s["x-protocol-revision"])

	defs, ok := s["$defs"].(map[string]any)
	require.True(t, ok, "$defs 必须存在")
	for _, name := range []string{
		"run", "query", "answer", "cancel", "tool_result",
		"event", "result", "query_result",
	} {
		require.Contains(t, defs, name, "$defs 缺少 %s", name)
	}
}

// TestProtocolSchema_每次返回新副本 验证调用方改不污染内部
func TestProtocolSchema_每次返回新副本(t *testing.T) {
	a := ProtocolSchema()
	a["$id"] = "tampered"
	b := ProtocolSchema()
	require.NotEqual(t, "tampered", b["$id"], "应返回新副本，不能共享内部状态")
}

// TestInputErrorDetails_类型错误
// Python: schema.py:38-63
func TestInputErrorDetails_类型错误(t *testing.T) {
	got := InputErrorDetails(map[string]any{
		"schema_version": "0.1", "type": "run", "input": 12345,
	}, "run")
	require.Equal(t, "invalid_type", got["reason"])
	require.Equal(t, "/input", got["field"])
}

// TestInputErrorDetails_缺必填字段
// Python: schema.py:53-56 required → 把缺失的第一个 key 追加到 path
func TestInputErrorDetails_缺必填字段(t *testing.T) {
	got := InputErrorDetails(map[string]any{"type": "run"}, "run")
	require.Equal(t, "required", got["reason"])
	require.True(t, len(got["field"].(string)) > 1, "field 应包含缺失字段名")
}

// TestInputErrorDetails_未知字段
// Python: schema.py:60-61 additionalProperties → unknown_field
func TestInputErrorDetails_未知字段(t *testing.T) {
	got := InputErrorDetails(map[string]any{
		"schema_version": "0.1", "type": "run", "input": "hi", "bogus": 1,
	}, "run")
	require.Equal(t, "unknown_field", got["reason"])
}

// TestInputErrorDetails_嵌套字段路径 验证 JSON Pointer 形式。
func TestInputErrorDetails_嵌套字段路径(t *testing.T) {
	got := InputErrorDetails(map[string]any{
		"schema_version": "0.1", "type": "run", "input": "hi",
		"workspace": map[string]any{"cwd": 123},
	}, "run")
	require.Equal(t, "/workspace/cwd", got["field"])
}

// TestInputErrorDetails_合法输入兜底 验证通过时不 panic。
func TestInputErrorDetails_合法输入兜底(t *testing.T) {
	got := InputErrorDetails(map[string]any{
		"schema_version": "0.1", "type": "run", "input": "hi",
	}, "run")
	require.NotEmpty(t, got["field"])
	require.NotEmpty(t, got["reason"])
}

// TestEscapeJSONPointer 验证 ~ 与 / 转义。
// Python: schema.py:58-59 replace("~","~0").replace("/","~1")
func TestEscapeJSONPointer(t *testing.T) {
	require.Equal(t, "a~0b", escapeJSONPointer("a~b"))
	require.Equal(t, "a~1b", escapeJSONPointer("a/b"))
	require.Equal(t, "a~0~1b", escapeJSONPointer("a~/b"))
}
