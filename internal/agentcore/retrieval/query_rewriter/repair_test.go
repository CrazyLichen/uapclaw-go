package query_rewriter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFillTemplate 占位符替换（Python TestFillTemplate 2 例）
func TestFillTemplate(t *testing.T) {
	// 占位符替换
	assert.Equal(t, "a=1 b=2", fillTemplate("a={a} b={b}", map[string]string{"a": "1", "b": "2"}))
	// JSON 示例花括号不误伤
	assert.Equal(t, `output: hi, example: {"x":1}`,
		fillTemplate(`output: {history}, example: {"x":1}`, map[string]string{"history": "hi"}))
}

// TestExtractJSON 首尾花括号提取（Python TestExtractJson 4 例）
func TestExtractJSON(t *testing.T) {
	assert.Equal(t, `{"a":1}`, extractJSON(`prefix {"a":1} suffix`)) // 单对象
	assert.Equal(t, "", extractJSON("no json here"))                 // 无括号
	assert.Equal(t, "", extractJSON("{"))                            // 只有开括号
	assert.Equal(t, `{"outer":{"inner":1}}`, extractJSON(` {"outer":{"inner":1}} `)) // 首{到尾}
}

// TestParseLLMJSON JSON 解析与修复（Python TestParseLlmJson 5 例）
func TestParseLLMJSON(t *testing.T) {
	// 合法 JSON 返回 dict
	m, ok := parseLLMJSON(`{"a":1}`)
	assert.True(t, ok)
	assert.Equal(t, float64(1), m["a"])

	// 空串/空白 → 不 ok（Python: not json_str or not json_str.strip()）
	_, ok = parseLLMJSON("")
	assert.False(t, ok)
	_, ok = parseLLMJSON("   ")
	assert.False(t, ok)

	// 纯文本（无括号）→ 不 ok
	_, ok = parseLLMJSON("not json")
	assert.False(t, ok)

	// 尾逗号由 json-repair 修复
	m, ok = parseLLMJSON(`{"theme":["a"], "summary":"b",}`)
	assert.True(t, ok)
	assert.Equal(t, []any{"a"}, m["theme"])
	assert.Equal(t, "b", m["summary"])

	// 非字典根（数组/null）→ 不 ok（Python: loads 成功但 isinstance dict 失败，
	// 直接 return None 不再尝试 repair；Go null Unmarshal 进 map 成功但 out=nil，
	// 需 nil 守卫对齐）
	_, ok = parseLLMJSON("[1,2,3]")
	assert.False(t, ok)
	_, ok = parseLLMJSON("null")
	assert.False(t, ok)
}
