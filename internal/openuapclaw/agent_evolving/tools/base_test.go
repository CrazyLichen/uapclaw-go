package tools

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessTools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools"
)

// newTestBaseTool 构造带默认字段的嵌入基类。
func newTestBaseTool() *BaseEvolutionTool {
	return &BaseEvolutionTool{
		ToolName:       "test_tool",
		ToolID:         "TestTool",
		FailureMessage: "evolution tool failed",
	}
}

// TestInputsDict 测试入参归一。
// Python tools/base.py:26-27。
func TestInputsDict(t *testing.T) {
	assert.Equal(t, map[string]any{"a": 1}, InputsDict(map[string]any{"a": 1}))
	assert.Empty(t, InputsDict("not a dict"))
	assert.Empty(t, InputsDict(nil))
}

// TestToOutput_成功 测试成功透传。
// Python tools/base.py:29-32。
func TestToOutput_成功(t *testing.T) {
	base := newTestBaseTool()
	out := base.ToOutput(map[string]any{"success": true, "operation": "list"})
	assert.Equal(t, true, out.Success)
	require.NotNil(t, out.Data)
	assert.Equal(t, "list", out.Data["operation"])
	assert.Empty(t, out.Error)
}

// TestToOutput_失败ErrorsJoin 测试失败时 errors 分号连接。
// Python tools/base.py:33-35。
func TestToOutput_失败ErrorsJoin(t *testing.T) {
	base := newTestBaseTool()
	out := base.ToOutput(map[string]any{
		"success": false,
		"errors":  []any{"e1", "e2"},
	})
	assert.Equal(t, false, out.Success)
	assert.Equal(t, "e1; e2", out.Error)
	require.NotNil(t, out.Data) // payload 保留（partial 语义）
}

// TestToOutput_失败兜底文案 测试 errors 缺省时 failure_message 兜底。
func TestToOutput_失败兜底文案(t *testing.T) {
	base := newTestBaseTool()
	out := base.ToOutput(map[string]any{"success": false})
	assert.Equal(t, "evolution tool failed", out.Error)
}

// TestFailure 测试异常兜底。
// Python tools/base.py:37-39。
func TestFailure(t *testing.T) {
	out := Failure(errors.New("boom"))
	assert.Equal(t, false, out.Success)
	assert.Equal(t, "boom", out.Error)
}

// TestRenderForLLM_失败保留Payload 测试失败渲染 error + payload 文本。
// Python tools/base.py:41-49——partial 提交的 status/counts/retry ids 必须可见。
func TestRenderForLLM_失败保留Payload(t *testing.T) {
	base := newTestBaseTool()
	out := harnessTools.ToolOutput{
		Success: false,
		Data:    map[string]any{"status": "partial", "retry_request_id": "req_1"},
		Error:   "evolution tool failed",
	}
	text := base.RenderForLLM(out)
	assert.Contains(t, text, "evolution tool failed")
	assert.Contains(t, text, "partial")
	assert.Contains(t, text, "req_1")
}

// TestRenderForLLM_成功默认渲染 测试成功时 payload 默认 JSON 渲染。
func TestRenderForLLM_成功默认渲染(t *testing.T) {
	base := newTestBaseTool()
	out := harnessTools.ToolOutput{Success: true, Data: map[string]any{"status": "applied"}}
	text := base.RenderForLLM(out)
	assert.Contains(t, text, "applied")
}

// TestInjectMappedContent 测试模型可见文本注入。
// Go 侧 render_for_llm 等价通道（先例 D-15.16-b8）。
func TestInjectMappedContent(t *testing.T) {
	base := newTestBaseTool()
	out := base.ToOutput(map[string]any{"success": false, "errors": []any{"e1"}})
	result := out.ToMap()
	InjectMappedContent(result, out)
	text, ok := result[MappedContentKey].(string)
	require.True(t, ok)
	assert.Contains(t, text, "e1")
}

// TestBaseEvolutionCard 测试配置卡访问（tool.Tool 接口面）。
func TestBaseEvolutionCard(t *testing.T) {
	base := newTestBaseTool()
	assert.Nil(t, base.Card())
}
