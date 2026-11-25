package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolOutput_ToMap 测试 ToolOutput 转 map 形态。
// Python: ToolOutput(success, data, error) 三字段语义。
func TestToolOutput_ToMap(t *testing.T) {
	out := ToolOutput{Success: true, Data: map[string]any{"k": 1}}
	m := out.ToMap()
	// Data 原样透传，禁止 json 往返（int 不得漂移为 float64）
	require.Contains(t, m, "success")
	assert.Equal(t, true, m["success"])
	data, ok := m["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, data["k"]) // int 保持 int
}

// TestToolOutput_ToMap_失败带错误 测试失败输出携带 error 字段。
// Python: ToolOutput(success=False, data=result, error=...)。
func TestToolOutput_ToMap_失败带错误(t *testing.T) {
	out := ToolOutput{Success: false, Data: map[string]any{"status": "partial"}, Error: "boom"}
	m := out.ToMap()
	assert.Equal(t, false, m["success"])
	assert.Equal(t, "boom", m["error"])
	data, ok := m["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "partial", data["status"])
}

// TestRenderFields 测试扁平记录渲染为 key: value 行序列。
// Python: render_fields (harness/tools/base_tool.py:10-30)——
// None/空串跳过、字符串原样、其余 JSON 编码、默认 \n 分隔。
func TestRenderFields(t *testing.T) {
	got := RenderFields(map[string]any{"a": "x", "b": "", "c": nil, "d": []any{1, 2}})
	assert.Equal(t, "a: x\nd: [1,2]", got)
}

// TestRenderFields_全空 测试全空记录渲染为空串。
func TestRenderFields_全空(t *testing.T) {
	assert.Empty(t, RenderFields(map[string]any{"a": nil, "b": ""}))
}
