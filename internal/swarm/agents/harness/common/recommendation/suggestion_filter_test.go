package recommendation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFilterSuggestion_十二项规则 逐条对齐 prompt_suggestion.py:201-229。
func TestFilterSuggestion_十二项规则(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"done", "done", "done"},
		{"meta_text 英文", "Nothing to suggest", "meta_text"},
		{"meta_text 中文", "保持沉默", "meta_text"},
		// Python 的 checks 顺序是 meta_text 在 meta_wrapped 之前，
		// 故「（无建议）」先命中 meta_text 的「无建议」分支。
		{"meta_wrapped 圆括号", "（无建议）", "meta_text"},
		{"meta_wrapped 方括号", "[nothing]", "meta_wrapped"},
		{"nonspecific", "继续下一步。", "nonspecific"},
		{"error_message", "API error: quota exceeded", "error_message"},
		{"prefixed_label 英文", "Suggestion: do the thing", "prefixed_label"},
		{"prefixed_label 中文", "建议：帮我看看日志", "prefixed_label"},
		{"too_few_words 英文单词", "okayish", "too_few_words"},
		{"too_few_words 中文短句", "看一下", "too_few_words"},
		// Python 实测：单块连续字符会先被 too_few_words 拦（words<2），
		// 故 too_long 用例必须带空格，让 words>=2。
		{"too_long", longText(), "too_long"},
		{"multiple_sentences", "先跑测试。再看结果。", "multiple_sentences"},
		{"has_formatting", "- 列出所有文件并说明用途", "has_formatting"},
		{"evaluative 英文", "Looks good to me", "evaluative"},
		{"evaluative 中文", "这个方案很好", "evaluative"},
		{"claude_voice 英文", "let me check the logs", "claude_voice"},
		{"claude_voice 中文", "我来帮你改这个函数", "claude_voice"},
		{"pass", "把日志级别调到 debug 再跑一次", ""},
		{"pass 单字白名单", "push", ""},
		{"pass 斜杠命令", "/config", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, FilterSuggestion(c.text), "text=%q", c.text)
		})
	}
}

// TestFilterSuggestion_不错过是正常建议 验证 (?!过) 的等价手写。
// Python: _EVALUATIVE_RE 的 `不错(?!过)`——「不错过」不该被判为 evaluative。
func TestFilterSuggestion_不错过是正常建议(t *testing.T) {
	assert.NotEqual(t, "evaluative", FilterSuggestion("这次不要不错过这个截止日期"),
		"不错过 中的 不错 后面紧跟 过，不应命中 evaluative")
	assert.Equal(t, "evaluative", FilterSuggestion("这个设计不错"),
		"单独的 不错 应命中 evaluative")
}

// TestSelectSuggestions_四情形 对齐 :164-198。
func TestSelectSuggestions_四情形(t *testing.T) {
	t.Run("null 视为无建议", func(t *testing.T) {
		got, outcome, reason := SelectSuggestions("null")
		assert.Empty(t, got)
		assert.Equal(t, "suppressed", outcome)
		assert.Equal(t, "empty", reason)
	})
	t.Run("JSON 形状 {suggestion}", func(t *testing.T) {
		got, outcome, _ := SelectSuggestions(`{"suggestion": "把超时调到 30 秒"}`)
		assert.Equal(t, []string{"把超时调到 30 秒"}, got)
		assert.Equal(t, "candidate", outcome)
	})
	t.Run("JSON null", func(t *testing.T) {
		got, outcome, _ := SelectSuggestions(`{"suggestion": null}`)
		assert.Empty(t, got)
		assert.Equal(t, "suppressed", outcome)
	})
	t.Run("多行只保留首条通过的", func(t *testing.T) {
		// Python 实测：两行都能通过过滤，取首条（MAX_SUGGESTIONS=1 时 break）
		got, outcome, _ := SelectSuggestions("先跑测试再看结果\n把日志调到 debug 重跑")
		assert.Equal(t, []string{"先跑测试再看结果"}, got)
		assert.Equal(t, "candidate", outcome)
	})
	t.Run("单行被过滤保留原因", func(t *testing.T) {
		got, outcome, reason := SelectSuggestions("done")
		assert.Empty(t, got)
		assert.Equal(t, "filtered", outcome)
		assert.Equal(t, "done", reason)
	})
	t.Run("去列表符号与包裹引号", func(t *testing.T) {
		// ★ 严格对齐上游实测行为：先 strip 引号（此时首字符是 '-'，引号剥不掉），
		// 再剥列表符，于是首引号残留在正文里。Python 输出同为
		// '「把缓存时间延长到十分钟'，此处照抄而非「修正」。
		got, _, _ := SelectSuggestions("- 「把缓存时间延长到十分钟」")
		assert.Equal(t, []string{"「把缓存时间延长到十分钟"}, got)
	})
}

// longText 构造一条超过 240 字符、且 word 数 >= 2 的建议。
// Python 实测：纯连写字符会先命中 too_few_words，故中间插一个空格。
func longText() string {
	words := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		text := make([]rune, 0, 130)
		for j := 0; j < 130; j++ {
			text = append(text, 'a')
		}
		words = append(words, string(text))
	}
	return words[0] + " " + words[1]
}
