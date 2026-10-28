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
	assert.Equal(t, `{"a":1}`, extractJSON(`prefix {"a":1} suffix`))                 // 单对象
	assert.Equal(t, "", extractJSON("no json here"))                                 // 无括号
	assert.Equal(t, "", extractJSON("{"))                                            // 只有开括号
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

// TestForceString 值转字符串（Python TestForceHelpers.string）
func TestForceString(t *testing.T) {
	assert.Equal(t, "x", forceString("x"))
	// dict → JSON 序列化（空格差异容忍：Python '{"a": 1}'，Go '{"a":1}'）
	assert.Contains(t, forceString(map[string]any{"a": 1}), `"a":1`)
	// 非 dict 非 str → fmt.Sprint（Python str(raw)）
	assert.Equal(t, "123", forceString(123))
}

// TestForceList 值转列表（Python TestForceHelpers.list）
func TestForceList(t *testing.T) {
	assert.Equal(t, []any{1, 2}, forceList([]any{1, 2}))
	assert.Equal(t, []any{"x"}, forceList("x"))
}

// TestForceJSON 值转 dict（Python TestForceHelpers.json + Go 增强四分支）
func TestForceJSON(t *testing.T) {
	// dict 直返
	assert.Equal(t, map[string]any{"a": 1}, forceJSON("k", map[string]any{"a": 1}))
	// JSON 字符串解析成 dict
	assert.Equal(t, map[string]any{"a": float64(1)}, forceJSON("k", `{"a":1}`))
	// 普通字符串（解析失败）→ {key: raw}
	assert.Equal(t, map[string]any{"k": "plain"}, forceJSON("k", "plain"))
	// Go 增强：合法 JSON 但非对象根 → {key: parsed}（Python L105；
	// json.Unmarshal 数字统一 float64）
	assert.Equal(t, map[string]any{"k": []any{float64(1)}}, forceJSON("k", `[1]`))
	// Go 增强：list → {key: raw}（Python L108-109）
	assert.Equal(t, map[string]any{"k": []any{1, 2}}, forceJSON("k", []any{1, 2}))
	// Go 增强：其他类型（数字）→ {key: raw}（Python L110-111）
	assert.Equal(t, map[string]any{"k": 42}, forceJSON("k", 42))
}

// TestSchemaRepair schema 校验修复（Python TestSchemaRepair 5 例 + typo 特判）
func TestSchemaRepair(t *testing.T) {
	compressSchema := map[string]schemaFieldType{"theme": schemaFieldList, "summary": schemaFieldString}

	// compress schema 原样通过
	out := schemaRepair(map[string]any{"theme": []any{"a"}, "summary": "b"}, compressSchema)
	assert.Equal(t, map[string]any{"theme": []any{"a"}, "summary": "b"}, out)

	// None/缺失字段填默认（Python: output.get(field) 缺失或 None 同路径）
	out = schemaRepair(map[string]any{"theme": nil, "summary": nil}, compressSchema)
	assert.Equal(t, []any{}, out["theme"])
	assert.Equal(t, "", out["summary"])

	// rewrite schema 全字段
	rewriteSchema := map[string]schemaFieldType{
		"before": schemaFieldString, "intention": schemaFieldString,
		"standalone_query": schemaFieldString, "references": schemaFieldDict,
		"missing": schemaFieldList, "typo": schemaFieldList,
		"gibberish": schemaFieldList, "from_history": schemaFieldString,
	}
	raw := map[string]any{
		"before": "那运费呢？", "intention": "咨询运费",
		"standalone_query": "淘美乐退货的运费谁出？",
		"references":       map[string]any{"那": "退货运费"},
		"missing":          []any{}, "typo": []any{}, "gibberish": []any{},
		"from_history": "history",
	}
	out = schemaRepair(raw, rewriteSchema)
	assert.Equal(t, "淘美乐退货的运费谁出？", out["standalone_query"])
	assert.Equal(t, []any{}, out["typo"])

	// typo 子结构修复（合法 dict 元素三键收敛）
	out = schemaRepair(
		map[string]any{"typo": []any{map[string]any{"original": "teh", "corrected": "the", "reason": "typo"}}},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	typoList := out["typo"].([]any)
	assert.Equal(t, "teh", typoList[0].(map[string]any)["original"])
	assert.Equal(t, "the", typoList[0].(map[string]any)["corrected"])

	// Go 增强：typo 非 dict 元素 forceJSON 包裹后三键白名单收敛——
	// 字符串 "teh" 解析失败包成 {"typo":"teh"}，白名单键全缺失 → 三键皆 ""
	// （Python L160-170 同款：item.get(key) 拿不到白名单键填 ""）
	out = schemaRepair(
		map[string]any{"typo": []any{"teh"}},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	typoList = out["typo"].([]any)
	assert.Equal(t, map[string]any{"original": "", "corrected": "", "reason": ""},
		typoList[0].(map[string]any))

	// Go 增强：typo 元素是 JSON 字符串（含白名单键）→ forceJSON 解析后键存活
	out = schemaRepair(
		map[string]any{"typo": []any{`{"original":"teh","corrected":"the","reason":"spelling"}`}},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	typoList = out["typo"].([]any)
	assert.Equal(t, "teh", typoList[0].(map[string]any)["original"])
	assert.Equal(t, "the", typoList[0].(map[string]any)["corrected"])
	assert.Equal(t, "spelling", typoList[0].(map[string]any)["reason"])

	// Go 增强：typo dict 元素缺键/None 值/非 str 值三态收敛（Python L164-170）
	out = schemaRepair(
		map[string]any{"typo": []any{map[string]any{"original": 123, "corrected": nil}}},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	item := out["typo"].([]any)[0].(map[string]any)
	assert.Equal(t, "123", item["original"]) // 非 str → forceString
	assert.Equal(t, "", item["corrected"])   // None → ""
	assert.Equal(t, "", item["reason"])      // 缺失 → ""

	// Go 增强：typo 非 list 输入先 forceList 化（Python L156-157）
	out = schemaRepair(
		map[string]any{"typo": "teh"},
		map[string]schemaFieldType{"typo": schemaFieldList},
	)
	assert.Len(t, out["typo"], 1)

	// 正常空输入不报错（Python "非 dict → 155604" 在 Go 编译期消解，spec §8）
	schemaRepair(map[string]any{}, compressSchema)

	// 类型不匹配 force 转换：string 字段给数字（Python L175-177）
	out = schemaRepair(map[string]any{"summary": 123}, compressSchema)
	assert.Equal(t, "123", out["summary"])

	// 类型不匹配 force 转换：list 字段给字符串（Python L178-179）
	out = schemaRepair(map[string]any{"theme": "a"}, compressSchema)
	assert.Equal(t, []any{"a"}, out["theme"])

	// 类型不匹配 force 转换：dict 字段给 JSON 字符串（Python L180-181）
	out = schemaRepair(map[string]any{"references": `{"k":"v"}`},
		map[string]schemaFieldType{"references": schemaFieldDict})
	assert.Equal(t, map[string]any{"k": "v"}, out["references"])
}
