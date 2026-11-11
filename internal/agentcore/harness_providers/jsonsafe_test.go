package harness_providers

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// TestToJsonSafe_基础类型原样 验证 None/str/bool/int 直接返回。
// Python: jsonsafe.py:24-25
func TestToJsonSafe_基础类型原样(t *testing.T) {
	assert.Nil(t, ToJsonSafe(nil))
	assert.Equal(t, "x", ToJsonSafe("x"))
	assert.Equal(t, true, ToJsonSafe(true))
	assert.Equal(t, 3, ToJsonSafe(3))
}

// TestToJsonSafe_非有限浮点转nil 验证 NaN/Inf 洗成 nil。
// 这一步是必需的：FreezeJsonValue 会拒绝 NaN，provider 数据里 NaN 常见。
// Python: jsonsafe.py:26-27
func TestToJsonSafe_非有限浮点转nil(t *testing.T) {
	assert.Nil(t, ToJsonSafe(math.NaN()), "NaN 必须洗成 nil")
	assert.Nil(t, ToJsonSafe(math.Inf(1)), "+Inf 必须洗成 nil")
	assert.Nil(t, ToJsonSafe(math.Inf(-1)), "-Inf 必须洗成 nil")
	assert.Equal(t, 1.5, ToJsonSafe(1.5), "有限浮点原样")

	// float32 分支
	assert.Nil(t, ToJsonSafe(float32(math.NaN())), "float32 NaN 也必须洗成 nil")
	assert.Equal(t, float32(1.5), ToJsonSafe(float32(1.5)))
}

// TestToJsonSafe_枚举取字符串值 验证 type X string 返回其字符串值。
// spec Q3：并入 reflect.Kind()==String 分派，等价于 Python 的 str,Enum 取 .value。
// Python: jsonsafe.py:28-29
func TestToJsonSafe_枚举取字符串值(t *testing.T) {
	type outputKind string
	var kind outputKind = "text"
	got := ToJsonSafe(kind)
	assert.Equal(t, "text", got, "具名 string 类型必须取其字符串值，不能是 Go 类型名")
}

// TestToJsonSafe_bytes解码 验证 []byte 转字符串且非法 UTF-8 替换。
// Python: jsonsafe.py:32-33（decode("utf-8", errors="replace")）
func TestToJsonSafe_bytes解码(t *testing.T) {
	assert.Equal(t, "abc", ToJsonSafe([]byte("abc")))
	// 非法 UTF-8 替换成 U+FFFD
	got := ToJsonSafe([]byte{0xff, 0xfe})
	s, ok := got.(string)
	require.True(t, ok)
	assert.Contains(t, s, "\ufffd", "非法字节必须替换为 U+FFFD")
}

// TestToJsonSafe_bytes先于slice 验证 []byte 不被展开成数字数组。
// Python: jsonsafe.py:32（bytes）早于 :41（list）
func TestToJsonSafe_bytes先于slice(t *testing.T) {
	got := ToJsonSafe([]byte{1, 2, 3})
	assert.IsType(t, "", got, "[]byte 必须走 bytes 分支，不能是 []any 数字数组")
}

// TestToJsonSafe_映射递归 验证 map 递归转换，key 转字符串。
// Python: jsonsafe.py:39-40
func TestToJsonSafe_映射递归(t *testing.T) {
	got := ToJsonSafe(map[string]any{
		"nested": map[string]any{"n": math.NaN()},
		"list":   []any{math.NaN(), "x"},
	})
	obj, ok := got.(map[string]any)
	require.True(t, ok)
	nested, ok := obj["nested"].(map[string]any)
	require.True(t, ok)
	assert.Nil(t, nested["n"], "嵌套 NaN 也必须洗成 nil")
	list, ok := obj["list"].([]any)
	require.True(t, ok)
	assert.Nil(t, list[0])
}

// TestToJsonSafe_非字符串key转字符串 验证 map key 用 fmt.Sprint。
// Python: jsonsafe.py:40（str(key)）
func TestToJsonSafe_非字符串key转字符串(t *testing.T) {
	got := ToJsonSafe(map[int]any{1: "a"})
	obj, ok := got.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "a", obj["1"])
}

// TestToJsonSafe_切片递归 验证 slice/array 递归成 []any。
// Python: jsonsafe.py:41-42
func TestToJsonSafe_切片递归(t *testing.T) {
	got := ToJsonSafe([]int{1, 2})
	arr, ok := got.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
	assert.Equal(t, 1, arr[0])

	// 数组（非切片）
	got = ToJsonSafe([2]string{"a", "b"})
	arr, ok = got.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
}

// TestToJsonSafe_指针解引用 验证指针先解引用再递归，nil 指针转 nil。
// Python 无指针概念；Go 侧为对齐「live object 不外泄」加的分支。
func TestToJsonSafe_指针解引用(t *testing.T) {
	n := 5
	assert.Equal(t, 5, ToJsonSafe(&n))
	assert.Nil(t, ToJsonSafe((*int)(nil)))
}

// TestToJsonSafe_ModelDump接缝 验证可选接口 ModelDump 被调用。
// spec D3：本批无生产实现者，接缝可用即可。
// Python: jsonsafe.py:36-38
func TestToJsonSafe_ModelDump接缝(t *testing.T) {
	got := ToJsonSafe(fakeModel{value: map[string]any{"k": math.NaN()}})
	obj, ok := got.(map[string]any)
	require.True(t, ok, "ModelDump 结果必须被递归归一化")
	assert.Nil(t, obj["k"], "ModelDump 返回的 NaN 也要洗成 nil")
}

// fakeModel 测试用：实现 ModelDump 可选接口。
type fakeModel struct {
	value any
}

// ModelDump 返回固定值。
func (f fakeModel) ModelDump(mode string, byAlias bool) (any, error) {
	_ = mode
	_ = byAlias
	return f.value, nil
}

// fakeModelErr 测试用：ModelDump 返回错误，应走兜底。
type fakeModelErr struct{}

// ModelDump 恒返回错误。
func (fakeModelErr) ModelDump(string, bool) (any, error) {
	return nil, assert.AnError
}

// TestToJsonSafe_ModelDump出错走兜底 验证 ModelDump 失败时降级为字符串。
// Python: jsonsafe.py:36-38 无对应失败路径（model_dump 不返回 error），
// 是 Go 侧接口带 error 的必要处理。
func TestToJsonSafe_ModelDump出错走兜底(t *testing.T) {
	got := ToJsonSafe(fakeModelErr{})
	_, ok := got.(string)
	assert.True(t, ok, "ModelDump 失败必须降级为字符串，实际 %T", got)
}

// TestToJsonSafe_兜底转字符串 验证无法识别的类型走 fmt.Sprint。
// 这是本函数的灵魂：provider payload 绝不把 live SDK 对象泄漏进事件流。
// Python: jsonsafe.py:43
func TestToJsonSafe_兜底转字符串(t *testing.T) {
	type sdkObject struct{ Name string }
	got := ToJsonSafe(sdkObject{Name: "x"})
	s, ok := got.(string)
	require.True(t, ok, "未知类型必须走 str() 兜底得到字符串，实际 %T", got)
	assert.Contains(t, s, "x")
}

// TestToJsonSafe_结果可被FreezeJsonValue接受 验证归一化后不再有 NaN。
// 这是 to_json_safe 存在的理由：与 FreezeJsonValue 串联。
func TestToJsonSafe_结果可被FreezeJsonValue接受(t *testing.T) {
	raw := map[string]any{
		"nan":  math.NaN(),
		"inf":  math.Inf(1),
		"nest": []any{math.NaN(), map[string]any{"deep": math.Inf(-1)}},
	}
	// 直接冻结必然失败
	_, err := harness_protocol.FreezeJsonObject(raw)
	require.Error(t, err, "原始数据含 NaN，FreezeJsonValue 必须拒绝")

	// 归一化后可以冻结
	safe := ToJsonSafe(raw)
	obj, ok := safe.(map[string]any)
	require.True(t, ok)
	_, err = harness_protocol.FreezeJsonObject(obj)
	require.NoError(t, err, "ToJsonSafe 归一化后必须可被冻结")
}

// TestToJsonObject 验证非 map 值被包成 {"value": ...}。
// Python: jsonsafe.py:46-52
func TestToJsonObject(t *testing.T) {
	assert.Equal(t, map[string]any{"a": 1}, ToJsonObject(map[string]any{"a": 1}))
	assert.Equal(t, map[string]any{"value": "x"}, ToJsonObject("x"))
	assert.Equal(t, map[string]any{"value": nil}, ToJsonObject(math.NaN()))
}
