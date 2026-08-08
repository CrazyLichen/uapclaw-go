package skill_call

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNormalizeKeywords_正常列表(t *testing.T) {
	result := NormalizeKeywords([]any{"a", "b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestNormalizeKeywords_空列表(t *testing.T) {
	result := NormalizeKeywords([]any{})
	assert.Nil(t, result)
}

func TestNormalizeKeywords_非列表(t *testing.T) {
	result := NormalizeKeywords("abc")
	assert.Nil(t, result)
}

func TestNormalizeKeywords_含空字符串(t *testing.T) {
	result := NormalizeKeywords([]any{"a", "", "c"})
	assert.Equal(t, []string{"a", "c"}, result)
}

func TestNormalizeKeywords_nil输入(t *testing.T) {
	result := NormalizeKeywords(nil)
	assert.Nil(t, result)
}

func TestNormalizeSummary_正常字符串(t *testing.T) {
	result := NormalizeSummary("hello  world")
	assert.NotNil(t, result)
	assert.Equal(t, "hello world", *result)
}

func TestNormalizeSummary_空字符串(t *testing.T) {
	result := NormalizeSummary("")
	assert.Nil(t, result)
}

func TestNormalizeSummary_null字符串(t *testing.T) {
	result := NormalizeSummary("null")
	assert.Nil(t, result)
}

func TestNormalizeSummary_Null大写(t *testing.T) {
	result := NormalizeSummary("Null")
	assert.Nil(t, result)
}

func TestNormalizeSummary_非字符串(t *testing.T) {
	result := NormalizeSummary(123)
	assert.Nil(t, result)
}

func TestNormalizeSummary_nil输入(t *testing.T) {
	result := NormalizeSummary(nil)
	assert.Nil(t, result)
}

func TestParseExperienceDraft_append(t *testing.T) {
	data := map[string]any{
		"action":   "append",
		"section":  "Troubleshooting",
		"target":   "body",
		"content":  "增加错误排查步骤",
		"keywords": []any{"error", "debug"},
		"summary":  "排查步骤",
	}
	draft := ParseExperienceDraft(data)
	assert.NotNil(t, draft)
	assert.Equal(t, "append", draft.Patch.Action)
	assert.Equal(t, "Troubleshooting", draft.Patch.Section)
	assert.Equal(t, "增加错误排查步骤", draft.Patch.Content)
	assert.Equal(t, []string{"error", "debug"}, draft.Keywords)
	assert.NotNil(t, draft.Summary)
	assert.Equal(t, "排查步骤", *draft.Summary)
}

func TestParseExperienceDraft_skip(t *testing.T) {
	data := map[string]any{
		"action":      "skip",
		"skip_reason": "no useful information",
	}
	draft := ParseExperienceDraft(data)
	assert.NotNil(t, draft)
	assert.Equal(t, "skip", draft.Patch.Action)
	assert.NotNil(t, draft.Patch.SkipReason)
	assert.Equal(t, "no useful information", *draft.Patch.SkipReason)
}

func TestParseExperienceDraft_invalidSectionFallback(t *testing.T) {
	data := map[string]any{
		"action":  "append",
		"section": "InvalidSection",
		"target":  "body",
		"content": "test",
	}
	draft := ParseExperienceDraft(data)
	assert.NotNil(t, draft)
	assert.Equal(t, "Troubleshooting", draft.Patch.Section)
}

func TestParseExperienceDraft_invalidTargetFallback(t *testing.T) {
	data := map[string]any{
		"action":  "append",
		"section": "Troubleshooting",
		"target":  "invalid_target",
		"content": "test",
	}
	draft := ParseExperienceDraft(data)
	assert.NotNil(t, draft)
	assert.Equal(t, "body", string(draft.Patch.Target))
}

func TestParseExperienceDraft_nil输入(t *testing.T) {
	draft := ParseExperienceDraft(nil)
	assert.Nil(t, draft)
}

func TestParseExperienceDraftsWithError_正常JSON数组(t *testing.T) {
	raw := `[{"action":"append","section":"Troubleshooting","target":"body","content":"step1"}]`
	drafts, errStr := ParseExperienceDraftsWithError(raw, ExtractJSONWithError)
	assert.Equal(t, "", errStr)
	assert.Equal(t, 1, len(drafts))
	assert.Equal(t, "step1", drafts[0].Patch.Content)
}

func TestParseExperienceDraftsWithError_单个JSON对象(t *testing.T) {
	raw := `{"action":"append","section":"Troubleshooting","target":"body","content":"step1"}`
	drafts, errStr := ParseExperienceDraftsWithError(raw, ExtractJSONWithError)
	assert.Equal(t, "", errStr)
	assert.Equal(t, 1, len(drafts))
}

func TestParseExperienceDraftsWithError_解析失败返回Nil(t *testing.T) {
	raw := "not json at all"
	drafts, errStr := ParseExperienceDraftsWithError(raw, ExtractJSONWithError)
	assert.Nil(t, drafts)
	assert.NotEmpty(t, errStr)
}

func TestExtractJSONWithError_正常JSON(t *testing.T) {
	raw := `{"key": "value"}`
	result, errStr := ExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

func TestExtractJSONWithError_修复后成功(t *testing.T) {
	// 含尾逗号
	raw := `{"key": "value",}`
	result, errStr := ExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

func TestExtractJSONWithError_正则提取成功(t *testing.T) {
	raw := `Some text before [{"action":"append"}] and after`
	result, errStr := ExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

func TestExtractJSONWithError_含代码块(t *testing.T) {
	raw := "```json\n{\"key\": \"value\"}\n```"
	result, errStr := ExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

func TestExtractJSONWithError_含注释需换行(t *testing.T) {
	// 注释需要换行才能被 // 正则去除，因为 Go 的正则是 /[^\n]* 这种行内注释
	raw := "{\"key\": \"value\" // comment\n}"
	result, errStr := ExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

func TestExtractJSONWithError_完全失败(t *testing.T) {
	raw := "this is not json and has no brackets"
	result, errStr := ExtractJSONWithError(raw)
	assert.Nil(t, result)
	assert.NotEmpty(t, errStr)
}

func TestExtractJSONWithError_空字符串(t *testing.T) {
	result, errStr := ExtractJSONWithError("")
	assert.Nil(t, result)
	assert.Equal(t, "empty response", errStr)
}

func TestLooksTruncated_截断(t *testing.T) {
	// opens=3 ({ + { + {), closes=0 → 3 > 0+1 = true
	text := `{"outer": {"inner": {"deep": "conte`
	assert.True(t, LooksTruncated(text))
}

func TestLooksTruncated_完整(t *testing.T) {
	text := `{"action": "append", "section": "Troubleshooting"}`
	assert.False(t, LooksTruncated(text))
}

func TestLooksTruncated_平衡括号(t *testing.T) {
	text := `{"key": {"nested": "value"}}`
	assert.False(t, LooksTruncated(text))
}

func TestLooksTruncated_轻微不平衡不算截断(t *testing.T) {
	// opens=1, closes=0 → 1 > 0+1=false，1个未关闭括号不算截断
	text := `{"key": "value"`
	assert.False(t, LooksTruncated(text))
}

func TestFixJSONText_去除代码块(t *testing.T) {
	text := "```json\n{\"key\": \"value\"}\n```"
	result := FixJSONText(text)
	assert.Contains(t, result, "{\"key\": \"value\"}")
	assert.NotContains(t, result, "```json")
}

func TestFixJSONText_去除注释(t *testing.T) {
	text := "{\"key\": \"value\" // this is a comment\n}"
	result := FixJSONText(text)
	assert.NotContains(t, result, "// this is a comment")
}

func TestFixJSONText_去除尾逗号(t *testing.T) {
	text := `{"key": "value",}`
	result := FixJSONText(text)
	assert.Equal(t, "{\"key\": \"value\"}", result)
}

func TestFixJSONText_去除数组尾逗号(t *testing.T) {
	text := `["item1", "item2",]`
	result := FixJSONText(text)
	assert.Equal(t, "[\"item1\", \"item2\"]", result)
}
