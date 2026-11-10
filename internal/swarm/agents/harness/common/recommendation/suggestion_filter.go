package recommendation

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// singleWords 单独成句也算完整指令的词表。
// Python: prompt_suggestion.py:82-88 _SINGLE_WORDS
var singleWords = map[string]struct{}{
	"yes": {}, "yeah": {}, "yep": {}, "yea": {}, "yup": {}, "sure": {}, "ok": {}, "okay": {},
	"push": {}, "commit": {}, "deploy": {}, "stop": {}, "continue": {}, "check": {}, "exit": {}, "quit": {}, "no": {},
	"是的": {}, "当然": {}, "好": {}, "好的": {}, "推送": {}, "提交": {}, "部署": {}, "停止": {},
	"继续": {}, "检查": {}, "退出": {}, "不": {}, "不要": {}, "继续吧": {}, "试试看": {},
}

var (
	// cjkRe 中文字符匹配
	// Python: _CJK_RE = re.compile(r"[㐀-鿿]")
	cjkRe = regexp.MustCompile(`[\x{3400}-\x{9FFF}]`)
	// metaTextRe 模型把「保持沉默」说出来的措辞
	// Python: _META_TEXT_RE
	metaTextRe = regexp.MustCompile(`(?i)^nothing found\.?$|^(?:nothing to suggest|no suggestion)|\bsilence is\b|\bstay(?:s|ing)? silent\b|^\W*silence\W*$|^\W*(?:不出|无|无建议|没有建议|沉默)\W*$|保持沉默|^(?:什么[都也])?(?:无输出|不输出)\W*$|^(?:本轮)?不预测(?:下一句)?\W*$`)
	// metaWrappedRe 整体被括号包裹的元信息
	// Python: _META_WRAPPED_RE
	metaWrappedRe = regexp.MustCompile(`^\(.*\)$|^\[.*\]$|^（.*）$`)
	// nonspecificRe 泛泛的「继续下一步」
	// Python: filter_suggestion 的 nonspecific 分支
	nonspecificRe = regexp.MustCompile(`^继续下一步[，。！？；：、,.!?;:…]*$`)
	// errorMessageRe provider 错误文本
	// Python: _ERROR_MESSAGE_RE
	errorMessageRe = regexp.MustCompile(`(?i)^(?:api error:|prompt is too long|request timed out|invalid api key|image was too large)`)
	// prefixedLabelRe 英文标签前缀
	// Python: _PREFIXED_LABEL_RE
	prefixedLabelRe = regexp.MustCompile(`^\w+:\s`)
	// chineseLabelRe 中文标签前缀
	// Python: _CHINESE_LABEL_RE
	chineseLabelRe = regexp.MustCompile(`^(?:建议|用户|下一句|回复|预测)\s*[:：]`)
	// sentenceEndRe 句末标点
	// Python: _SENTENCE_END_RE = re.compile(r"[。！？!?]|[.](?=\s*(?:[A-Z]|$))")
	sentenceEndRe = regexp.MustCompile(`[。！？!?]|[.](?:\s*(?:[A-Z]|$))`)
	// formattingRe 列表/代码块等格式痕迹
	// Python: _FORMATTING_RE = re.compile(r"[\n*]|```|^\s*(?:[-•]|\d+[.)])\s")
	formattingRe = regexp.MustCompile("[\n*]|```|^\\s*(?:[-•]|\\d+[.)])\\s")
	// linePrefixRe 行首列表符号
	// Python: _LINE_PREFIX_RE = re.compile(r"^(?:[-•*]|\d+[.)、])\s*")
	linePrefixRe = regexp.MustCompile(`^(?:[-•*]|\d+[.)、])\s*`)
	// evaluativeRe 评价性收尾（不是下一句话）。
	// Python: _EVALUATIVE_RE 含 (?!过) 负向先行断言；Go 的 RE2 不支持前瞻，
	// 故把「不错」从正则里摘出，由 hasEvaluative 单独判定（等价手写）。
	evaluativeRe = regexp.MustCompile(`(?i)thanks|thank you|looks good|sounds good|that works|that worked|that's all|nice|great|perfect|makes sense|awesome|excellent|谢谢|感谢|多谢|很好|完美|明白了|有道理|很有帮助`)
	// claudeVoiceRe 助手口吻（英文前缀 + 「以下是/下面是」）。
	// Python: _CLAUDE_VOICE_RE 的前半；中文的「我来…」分支因含正/负向先行断言
	// （RE2 不支持）改由 hasClaudeVoice 手写判定（等价）。
	claudeVoiceRe = regexp.MustCompile(`(?i)^(?:let me|i'll|i've|i'm|i can|i would|i think|i notice|here's|here is|here are|that's|this is|this will|you can|you should|you could|sure,|of course|certainly)|^(?:以下是|下面是)`)
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// minChineseChars 中文建议的最少字符数
	// Python: _MIN_CHINESE_CHARS = 4
	minChineseChars = 4
	// maxSuggestionChars 建议的最大字符数
	// Python: _MAX_SUGGESTION_CHARS = 240
	maxSuggestionChars = 240
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SelectSuggestions 返回要展示的建议、无建议时的账本 outcome 与原因。
// Python: prompt_suggestion.py:164 select_suggestions
func SelectSuggestions(text string) (suggestions []string, outcome string, reason string) {
	// Python: 1. text = text.strip()
	text = strings.TrimSpace(text)
	// Python: 2. if text.lower() in {"null", "none"}: return [], "suppressed", "empty"
	lower := strings.ToLower(text)
	if lower == "null" || lower == "none" {
		return nil, "suppressed", "empty"
	}
	// Python: 3. json_suggestion = _json_suggestion(text)
	if jsonSuggestion, ok := jsonSuggestionOf(text); ok {
		text = jsonSuggestion
		// Python: 4. if not text: return [], "suppressed", "empty"
		if text == "" {
			return nil, "suppressed", "empty"
		}
	}
	// Python: 5. lines = [_suggestion_line(raw) for raw in text.splitlines() if 非空]
	var lines []string
	for _, raw := range splitLines(text) {
		if suggestion := suggestionLine(raw); suggestion != "" {
			lines = append(lines, suggestion)
		}
	}
	// Python: 6. if not lines: return [], "suppressed", "empty"
	if len(lines) == 0 {
		return nil, "suppressed", "empty"
	}
	// Python: 7. if len(lines) == 1: 单行直接过滤判定
	if len(lines) == 1 {
		if r := FilterSuggestion(lines[0]); r != "" {
			return nil, "filtered", r
		}
		return []string{lines[0]}, "candidate", ""
	}
	// Python: 8. 多行：逐行过滤，保留通过的行，记录首个原因
	kept := make([]string, 0, len(lines))
	firstReason := ""
	for _, line := range lines {
		if r := FilterSuggestion(line); r != "" {
			if firstReason == "" {
				firstReason = r
			}
			continue
		}
		kept = append(kept, line)
		if len(kept) == MaxSuggestions {
			break
		}
	}
	// Python: 9. if not kept: return [], "filtered", first_reason
	if len(kept) == 0 {
		return nil, "filtered", firstReason
	}
	// Python: 10. return kept, "candidate", None
	return kept, "candidate", ""
}

// FilterSuggestion 返回不该展示该建议的原因；空串表示可以展示。
// Python: prompt_suggestion.py:201 filter_suggestion（12 项检查，顺序不可变）
func FilterSuggestion(text string) string {
	lower := strings.ToLower(text)
	chinese := cjkRe.MatchString(text)
	words := len(strings.Fields(text))
	characters := 0
	for _, character := range text {
		if !unicode.IsSpace(character) {
			characters++
		}
	}
	sentenceEnds := sentenceEndRe.FindAllStringIndex(text, -1)

	// 顺序与上游 checks 元组一一对应，短路语义相同
	if lower == "done" {
		return "done"
	}
	if metaTextRe.MatchString(text) {
		return "meta_text"
	}
	if metaWrappedRe.MatchString(text) {
		return "meta_wrapped"
	}
	if nonspecificRe.MatchString(text) {
		return "nonspecific"
	}
	if errorMessageRe.MatchString(text) {
		return "error_message"
	}
	if prefixedLabelRe.MatchString(text) || chineseLabelRe.MatchString(text) {
		return "prefixed_label"
	}
	if _, ok := singleWords[lower]; !ok {
		// Python: characters < 4 if chinese else words < 2 and not text.startswith("/")
		if chinese {
			if characters < minChineseChars {
				return "too_few_words"
			}
		} else if words < 2 && !strings.HasPrefix(text, "/") {
			return "too_few_words"
		}
	}
	if len([]rune(text)) > maxSuggestionChars {
		return "too_long"
	}
	if len(sentenceEnds) > 1 {
		return "multiple_sentences"
	}
	for _, index := range sentenceEnds {
		if index[0] < len(text)-1 {
			return "multiple_sentences"
		}
	}
	if formattingRe.MatchString(text) {
		return "has_formatting"
	}
	if hasEvaluative(text) {
		return "evaluative"
	}
	if hasClaudeVoice(text) {
		return "claude_voice"
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// suggestionLine 去掉行首列表符号与包裹引号。
// Python: prompt_suggestion.py:144 _suggestion_line
func suggestionLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, "「」\"'“”")
	return strings.TrimSpace(linePrefixRe.ReplaceAllString(trimmed, ""))
}

// jsonSuggestionOf 取 JSON 回复里的 suggestion 值。
// 第二个返回值 false 表示「不是该形状」。
// Python: prompt_suggestion.py:149 _json_suggestion
func jsonSuggestionOf(text string) (string, bool) {
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return "", false
	}
	object, ok := decoded.(map[string]any)
	if !ok || len(object) != 1 {
		return "", false
	}
	raw, ok := object["suggestion"]
	if !ok {
		return "", false
	}
	if raw == nil {
		return "", true
	}
	if suggestion, ok := raw.(string); ok {
		return strings.TrimSpace(suggestion), true
	}
	return "", false
}

// splitLines 按 Python str.splitlines 的语义切行（\r\n / \r / \n 均视为换行）。
func splitLines(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
}

// hasEvaluative 判断是否为评价性收尾。
// Python: _EVALUATIVE_RE 的 `不错(?!过)` 分支——RE2 不支持负向先行断言，
// 此处先找「不错」再排除其后紧跟「过」的情形（等价手写）。
func hasEvaluative(text string) bool {
	if evaluativeRe.MatchString(text) {
		return true
	}
	for index := strings.Index(text, "不错"); index >= 0; {
		rest := text[index+len("不错"):]
		if !strings.HasPrefix(rest, "过") {
			return true
		}
		next := strings.Index(text[index+len("不错"):], "不错")
		if next < 0 {
			return false
		}
		index = index + len("不错") + next
	}
	return false
}

// hasClaudeVoice 判断是否为助手口吻。
// Python: _CLAUDE_VOICE_RE 的中文分支：
//
//	(?<![对帮给让替和跟同])我来(?!自)        —— 「我来…」但排除「我来」作宾语、以及「我来自」
//	我(?:可以|会|能|来|将|这就)?(?:帮|替|为)[你您]
//
// RE2 不支持前后向断言，故手写等价：先匹配英文正则，再逐位置检查中文规则。
func hasClaudeVoice(text string) bool {
	if claudeVoiceRe.MatchString(text) {
		return true
	}
	// 「我…帮你/替你/为你」：中间可插 可以/会/能/来/将/这就
	for index := 0; index < len(text); {
		next := strings.Index(text[index:], "我")
		if next < 0 {
			break
		}
		position := index + next
		rest := text[position+len("我"):]
		for _, prefix := range []string{"可以", "会", "能", "来", "将", "这就"} {
			rest = strings.TrimPrefix(rest, prefix)
		}
		for _, verb := range []string{"帮你", "替你", "为你", "帮您", "替您", "为您"} {
			if strings.HasPrefix(rest, verb) {
				return true
			}
		}
		index = position + len("我")
	}
	// 「我来…」但前面不是 对/帮/给/让/替/和/跟/同，且后面不是「自」
	for index := 0; index < len(text); {
		next := strings.Index(text[index:], "我来")
		if next < 0 {
			break
		}
		position := index + next
		var before rune
		if position > 0 {
			for _, r := range text[:position] {
				before = r
			}
		}
		after := text[position+len("我来"):]
		excluded := before == '对' || before == '帮' || before == '给' || before == '让' ||
			before == '替' || before == '和' || before == '跟' || before == '同'
		if !excluded && !strings.HasPrefix(after, "自") {
			return true
		}
		index = position + len("我来")
	}
	return false
}
