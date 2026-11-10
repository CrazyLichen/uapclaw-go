package harness_protocol

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreezeJsonValue_标量透传 验证 None/string/bool/int 原样返回。
// Python: models.py:30-34（None/str/bool/int 直返）
func TestFreezeJsonValue_标量透传(t *testing.T) {
	for _, in := range []any{nil, "s", true, false, 7, int64(9)} {
		got, err := FreezeJsonValue(in)
		require.NoError(t, err)
		assert.Equal(t, in, got)
	}
}

// TestFreezeJsonValue_拒绝非有限浮点 验证 NaN/Inf 被拒。
// Python: models.py:35-38 "JSON numbers must be finite"
func TestFreezeJsonValue_拒绝非有限浮点(t *testing.T) {
	for _, in := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := FreezeJsonValue(in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "JSON numbers must be finite")
	}
}

// TestFreezeJsonValue_有限浮点通过 验证普通浮点放行。
func TestFreezeJsonValue_有限浮点通过(t *testing.T) {
	got, err := FreezeJsonValue(1.5)
	require.NoError(t, err)
	assert.Equal(t, 1.5, got)
}

// TestFreezeJsonValue_切片深拷贝为不共享 验证 list/tuple 递归且结果不与入参共享底层数组。
// Python: models.py:39-40（list/tuple → tuple 递归，不可变）
func TestFreezeJsonValue_切片深拷贝为不共享(t *testing.T) {
	src := []any{1, []any{"a"}}
	got, err := FreezeJsonValue(src)
	require.NoError(t, err)
	src[0] = 999
	// 冻结结果不受外部修改影响
	assert.Equal(t, []any{1, []any{"a"}}, got)
}

// TestFreezeJsonValue_映射深拷贝 验证 map 递归深拷贝，改源不影响结果。
// Python: models.py:41-47（Mapping → MappingProxyType 递归）
func TestFreezeJsonValue_映射深拷贝(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	got, err := FreezeJsonValue(src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	obj := got.(map[string]any)
	assert.Equal(t, []any{1}, obj["k"])
}

// TestFreezeJsonValue_拒绝非字符串键 验证 map 键必须是 string。
// Python: models.py:44-45 "JSON object keys must be strings"
func TestFreezeJsonValue_拒绝非字符串键(t *testing.T) {
	_, err := FreezeJsonValue(map[int]any{1: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON object keys must be strings")
}

// TestFreezeJsonValue_拒绝非JSON类型 验证 struct/chan 等被拒。
// Python: models.py:48 "value of type X is not JSON-compatible"
func TestFreezeJsonValue_拒绝非JSON类型(t *testing.T) {
	_, err := FreezeJsonValue(struct{ A int }{1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not JSON-compatible")
}

// TestFreezeJsonObject_非映射报错 验证入参不是映射时失败。
// Python: models.py:51-57 "JSON object must be a mapping"
func TestFreezeJsonObject_非映射报错(t *testing.T) {
	_, err := FreezeJsonObject(nil)
	require.Error(t, err)
}

// TestFreezeJsonObject_空映射返回空非nil 验证 nil 入参返回空 map。
func TestFreezeJsonObject_空映射(t *testing.T) {
	got, err := FreezeJsonObject(map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestJsonValueToBuiltin_还原为可序列化容器 验证冻结值可转回标准容器。
// Python: models.py:60-67 json_value_to_builtin
func TestJsonValueToBuiltin_还原为可序列化容器(t *testing.T) {
	frozen, err := FreezeJsonValue(map[string]any{"a": []any{1, "b"}})
	require.NoError(t, err)
	got := JsonValueToBuiltin(frozen)
	assert.Equal(t, map[string]any{"a": []any{1, "b"}}, got)
}
