package harness_providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// 本文件对应 Python tests 里 harness_input_text 的三条分支
// （inputs.py:14-27 的字符串 / 文本块列表 / 其余 JSON）。

// newInput 用给定 content 构造一个 HarnessInput（构造期冻结校验）。
func newInput(t *testing.T, content any) harnessprotocol.HarnessInput {
	t.Helper()
	input, err := harnessprotocol.NewHarnessInput(content, nil)
	require.NoError(t, err)
	return input
}

// TestHarnessInputText_字符串直通 对齐 inputs.py:23-24。
func TestHarnessInputText_字符串直通(t *testing.T) {
	assert.Equal(t, "直接文本", HarnessInputText(newInput(t, "直接文本")))
}

// TestHarnessInputText_空字符串直通 空串也是字符串，原样返回空串。
func TestHarnessInputText_空字符串直通(t *testing.T) {
	assert.Equal(t, "", HarnessInputText(newInput(t, "")))
}

// TestHarnessInputText_文本块以空行拼接 对齐 inputs.py:25-26。
func TestHarnessInputText_文本块以空行拼接(t *testing.T) {
	content := []any{
		map[string]any{"type": "text", "text": "第一段"},
		map[string]any{"type": "text", "text": "第二段"},
	}
	assert.Equal(t, "第一段\n\n第二段", HarnessInputText(newInput(t, content)))
}

// TestHarnessInputText_单文本块 只有一个文本块时无分隔符。
func TestHarnessInputText_单文本块(t *testing.T) {
	content := []any{map[string]any{"type": "text", "text": "只有一段"}}
	assert.Equal(t, "只有一段", HarnessInputText(newInput(t, content)))
}

// TestHarnessInputText_混合块走紧凑JSON 非全 text 块时整体序列化。
func TestHarnessInputText_混合块走紧凑JSON(t *testing.T) {
	content := []any{
		map[string]any{"type": "text", "text": "第一段"},
		map[string]any{"type": "image", "url": "http://x/y.png"},
	}
	got := HarnessInputText(newInput(t, content))
	// 紧凑分隔符：无空格
	assert.Contains(t, got, `"type":"image"`)
	// 不转义非 ASCII（ensure_ascii=False 的等价物）
	assert.Contains(t, got, "第一段")
	assert.NotContains(t, got, `\u`)
}

// TestHarnessInputText_文本块缺text字段走JSON text 不是字符串的块不算文本块。
func TestHarnessInputText_文本块缺text字段走JSON(t *testing.T) {
	content := []any{map[string]any{"type": "text"}}
	got := HarnessInputText(newInput(t, content))
	assert.Contains(t, got, `"type":"text"`)
}

// TestHarnessInputText_空列表走JSON 空列表不满足「非空且全 text 块」。
func TestHarnessInputText_空列表走JSON(t *testing.T) {
	assert.Equal(t, "[]", HarnessInputText(newInput(t, []any{})))
}

// TestHarnessInputText_对象走紧凑JSON 对齐 inputs.py:27 的兜底分支。
func TestHarnessInputText_对象走紧凑JSON(t *testing.T) {
	got := HarnessInputText(newInput(t, map[string]any{"key": "值"}))
	assert.Equal(t, `{"key":"值"}`, got)
}

// TestHarnessInputText_非map块走JSON 列表元素不是对象时不算文本块。
func TestHarnessInputText_非map块走JSON(t *testing.T) {
	got := HarnessInputText(newInput(t, []any{"裸字符串"}))
	assert.Equal(t, `["裸字符串"]`, got)
}

// TestHarnessInputText_整数走紧凑JSON 数字不是字符串。
func TestHarnessInputText_整数走紧凑JSON(t *testing.T) {
	assert.Equal(t, "42", HarnessInputText(newInput(t, 42)))
}
