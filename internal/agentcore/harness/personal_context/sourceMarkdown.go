package personal_context

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// 源文档与生成 Context 链接的隔离层：采集侧把非 web 目标降级为纯文本。
// Python: openjiuwen/harness/personal_context/source_markdown.py（一比一移植）。
// RE2 不支持 lookahead/lookbehind，`_code_end` 的闭围栏定位与 `_destination`
// 的标题切分改为手写扫描（语义等价，见各自注释）。

// ──────────────────────────── 全局变量 ────────────────────────────

// definitionPattern 链接定义行（标题 + 目的地 + 可选标题）。
// Python: source_markdown.py:9 _DEFINITION（re.MULTILINE）
var definitionPattern = regexp.MustCompile(
	`(?m)^ {0,3}\[([^\]\n]+)\]:[ \t]*(?:\n {0,3})?(<[^<>\n]*>|[^\s]+)` +
		`(?:[ \t]+(?:"[^"\n]*"|'[^'\n]*'|\([^\n]*\)))?[ \t]*$`)

// htmlTagPattern HTML 注释或标签。
// Python: source_markdown.py:12 _HTML_TAG（re.DOTALL）
var htmlTagPattern = regexp.MustCompile(
	`(?s)<!--.*?-->|</?[A-Za-z](?:[^<>"']|"[^"]*"|'[^']*')*>`)

// htmlTargetPattern HTML 标签内的目标属性。
// Python: source_markdown.py:13 _HTML_TARGET
var htmlTargetPattern = regexp.MustCompile(
	`\b(?:href|src|poster|action|data|xlink:href|srcset)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

// autolinkPattern 尖括号自动链接。
// Python: source_markdown.py 内联 re.match(r"<([^<>\s]+)>", ...)
var autolinkPattern = regexp.MustCompile(`<([^<>\s]+)>`)

// autolinkSchemePattern 自动链接目标需带 scheme 前缀。
var autolinkSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// listMarkerPattern 列表行标记（用于缩进代码判定）。
// Python: source_markdown.py 内联 re.match(r"^ {0,3}(?:[-+*]|\d+[.)])\s", line)
var listMarkerPattern = regexp.MustCompile(`^ {0,3}(?:[-+*]|\d+[.)])\s`)

// fenceMarkerPattern 围栏开头标记。
// Python: source_markdown.py 内联 re.match(r"^ {0,3}(`{3,}|~{3,})", line)
var fenceMarkerPattern = regexp.MustCompile(`^ {0,3}(` + "`{3,}|~{3,})")

// backtickRunPattern 反引号连续段。
var backtickRunPattern = regexp.MustCompile("^`+")

// ──────────────────────────── 导出函数 ────────────────────────────

// MarkdownReferenceText 返回用于引用校验的散文文本（剔除字面代码示例）。
// Python: source_markdown.py:226 markdown_reference_text
func MarkdownReferenceText(markdown string) string {
	var result []string
	for _, segment := range sourceSegments(markdown) {
		if segment.protected {
			result = append(result, "\n")
			continue
		}
		text := segment.text
		index := 0
		for index < len(text) {
			if text[index] == '`' {
				end := codeEnd(text, index)
				if end != index {
					result = append(result, " ")
					index = end
					continue
				}
			}
			result = append(result, text[index:index+1])
			index++
		}
	}
	return strings.Join(result, "")
}

// RewriteMarkdownProse 重写生效散文并逐字节保留字面代码示例。
// Python: source_markdown.py:246 rewrite_markdown_prose
func RewriteMarkdownProse(markdown string, transform func(string) string) string {
	var result []string
	for _, segment := range sourceSegments(markdown) {
		if segment.protected {
			result = append(result, segment.text)
			continue
		}
		text := segment.text
		start := 0
		index := 0
		for index < len(text) {
			end := index
			if text[index] == '`' {
				end = codeEnd(text, index)
			}
			if end != index {
				result = append(result, transform(text[start:index]), text[index:end])
				start = end
				index = end
			} else {
				index++
			}
		}
		result = append(result, transform(text[start:]))
	}
	return strings.Join(result, "")
}

// NormalizeSourceLinks 把非 web 源目标渲染为文本而不跟踪任何目标。
// 只对新建采集的源 Markdown 调用，不得用于生成的 Context 或已登记源引用。
// Python: source_markdown.py:265 normalize_source_links（render_link 可为 nil）
func NormalizeSourceLinks(markdown string, renderLink func(string, string) string) string {
	segments := sourceSegments(markdown)
	definitions := map[string]string{}
	for _, segment := range segments {
		if !segment.protected {
			for _, match := range definitionPattern.FindAllStringSubmatch(segment.text, -1) {
				if _, exists := definitions[labelKey(match[1])]; !exists {
					definitions[labelKey(match[1])] = destination(match[2])
				}
			}
		}
	}
	replaceDefinition := func(match []string) string {
		target := destination(match[2])
		if renderLink == nil && webTarget(target) {
			return match[0]
		}
		return linkText(match[1], target)
	}
	var parts []string
	for _, segment := range segments {
		if segment.protected {
			parts = append(parts, segment.text)
			continue
		}
		replaced := definitionPattern.ReplaceAllStringFunc(segment.text, func(raw string) string {
			return replaceDefinition(definitionPattern.FindStringSubmatch(raw))
		})
		parts = append(parts, rewriteInline(replaced, definitions, renderLink))
	}
	return strings.Join(parts, "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// webTarget 目标是否为 http(s) web 链接。
// Python: source_markdown.py:22 _web_target
func webTarget(target string) bool {
	target = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(htmlUnescape(target)), "<"), ">")
	target = escapeSequenceUnescape(target)
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https") &&
		parsed.Hostname() != ""
}

// htmlUnescape 等价 Python html.unescape（Go 标准库同语义）。
func htmlUnescape(value string) string {
	return html.UnescapeString(value)
}

// literal 对 label/target 做 HTML 转义但不转义引号（Python html.escape(quote=False)），
// 并把 Markdown 结构字符替换为数字实体。
// Python: source_markdown.py:32 _literal
func literal(value string) string {
	escaped := escapeHTMLQuoteFalse(value)
	for _, character := range "[]`\\*_" {
		escaped = strings.ReplaceAll(escaped, string(character), fmt.Sprintf("&#%d;", character))
	}
	return escaped
}

// labelKey 标签归一键：折叠空白并小写。
// Python: source_markdown.py:39 _label_key
func labelKey(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

// linkText 渲染为"原文链接"文本形态。
// Python: source_markdown.py:43 _link_text
func linkText(label string, target string) string {
	return fmt.Sprintf("%s（原文链接：%s）", literal(label), literal(target))
}

// substring 等价 Python text[start:stop]（stop 为 -1 表示到结尾）。
// Python: source_markdown.py:47 _substring
func substring(text string, start int, stop int) string {
	if stop < 0 || stop > len(text) {
		return text[start:]
	}
	return text[start:stop]
}

// codeEnd 返回 index 处反引号围栏的结束位置（不含）。
// Python: source_markdown.py:51 _code_end —— 闭围栏定位因 RE2 无 lookbehind 改手写：
// 找到长度恰好等于开栏长度、且前后都不是反引号的连续段。
func codeEnd(text string, start int) int {
	run := backtickRunPattern.FindString(text[start:])
	if run == "" {
		return start
	}
	length := len(run)
	closing := closingFenceEnd(substring(text, start+length, -1), length)
	if closing < 0 {
		return start
	}
	return start + length + closing
}

// closingFenceEnd 在 text 中查找长度恰为 length 的闭围栏，返回其结束下标（相对 text）。
// 等价 Python rf"(?<!`)`{{{length}}}(?!`)" 的 search().end()。
func closingFenceEnd(text string, length int) int {
	for index := 0; index+length <= len(text); index++ {
		if text[index] != '`' {
			continue
		}
		if index+length < len(text) && text[index+length] == '`' {
			continue // 后随更多反引号 → 不是恰好 length
		}
		if index > 0 && text[index-1] == '`' {
			continue // 前随反引号 → 属于更长段
		}
		ok := true
		for offset := 0; offset < length; offset++ {
			if text[index+offset] != '`' {
				ok = false
				break
			}
		}
		if ok {
			return index + length
		}
	}
	return -1
}

// balancedEnd 平衡括号/方括号扫描，返回闭合位置之后（Python 返回 index+1）。
// Python: source_markdown.py:60 _balanced_end
func balancedEnd(text string, start int, opening string, closing string) int {
	depth := 1
	index := start + 1
	for index < len(text) {
		character := text[index]
		if character == '\\' {
			index += 2
			continue
		}
		if opening == "(" {
			var delimiter string
			if character == '<' {
				delimiter = ">"
			} else if (character == '"' || character == '\'') && index > 0 &&
				isSpaceByte(text[index-1]) {
				delimiter = string(character)
			}
			if delimiter != "" {
				end := strings.Index(text[index+1:], delimiter)
				if end != -1 {
					index = index + 1 + end + 1
					continue
				}
			}
		}
		if string(character) == opening {
			depth++
		} else if string(character) == closing {
			depth--
			if depth == 0 {
				return index + 1
			}
		}
		index++
	}
	return start
}

// isSpaceByte 单字节空白判断（Python text[index-1].isspace() 的 ASCII 等价）。
func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// destination 从链接目的地剥离尖括号与可选标题。
// Python: source_markdown.py:89 _destination —— 标题切分 `\s+(?=["'])` 因 RE2 无
// lookahead 改手写：找第一段后随引号的空白。
func destination(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "<") && strings.Contains(value, ">") {
		return substring(value, 1, strings.Index(value, ">"))
	}
	cut := splitBeforeQuotedTitle(value)
	return strings.TrimSpace(value[:cut])
}

// splitBeforeQuotedTitle 返回标题切分点：第一段后随引号的空白起点。
func splitBeforeQuotedTitle(value string) int {
	for index := 0; index < len(value); index++ {
		if isSpaceByte(value[index]) {
			end := index
			for end < len(value) && isSpaceByte(value[end]) {
				end++
			}
			if end < len(value) && (value[end] == '"' || value[end] == '\'') {
				return index
			}
		}
	}
	return len(value)
}

// rewriteLink 重写单个链接。
// Python: source_markdown.py:97 _rewrite_link
func rewriteLink(text string, start int, definitions map[string]string, renderLink func(string, string) string) (string, int) {
	bracket := start
	if strings.HasPrefix(text[start:], "![") {
		bracket = start + 1
	}
	labelEnd := balancedEnd(text, bracket, "[", "]")
	if labelEnd == bracket {
		return text[start : start+1], start + 1
	}
	label := substring(text, bracket+1, labelEnd-1)
	end := labelEnd
	target, hasTarget := definitions[labelKey(label)]
	if strings.HasPrefix(text[end:], "(") {
		destinationEnd := balancedEnd(text, end, "(", ")")
		if destinationEnd == end {
			return text[start : start+1], start + 1
		}
		target = destination(substring(text, end+1, destinationEnd-1))
		hasTarget = true
		end = destinationEnd
	} else if strings.HasPrefix(text[end:], "[") {
		referenceEnd := balancedEnd(text, end, "[", "]")
		if referenceEnd != end {
			reference := substring(text, end+1, referenceEnd-1)
			if reference == "" {
				reference = label
			}
			target, hasTarget = definitions[labelKey(reference)]
			end = referenceEnd
		}
	}
	if !hasTarget {
		// Python: target is None → 原样返回区间
		return text[start:end], end
	}
	if renderLink != nil {
		return renderLink(label, target), end
	}
	if webTarget(target) {
		// web 链接内部可能还有本地目标图片：内层继续重写。
		rewrittenLabel := rewriteInline(label, definitions, renderLink)
		prefix := substring(text, start, bracket+1)
		suffix := substring(text, labelEnd-1, end)
		return prefix + rewrittenLabel + suffix, end
	}
	return linkText(label, target), end
}

// rewriteInline 重写行内链接/自动链接/HTML 标签目标。
// Python: source_markdown.py:132 _rewrite_inline
func rewriteInline(text string, definitions map[string]string, renderLink func(string, string) string) string {
	var result []string
	index := 0
	for index < len(text) {
		if text[index] == '\\' {
			result = append(result, substring(text, index, index+2))
			index += 2
			continue
		}
		if text[index] == '`' {
			end := codeEnd(text, index)
			if end != index {
				result = append(result, text[index:end])
				index = end
				continue
			}
		}
		if text[index] == '[' || strings.HasPrefix(text[index:], "![") {
			replacement, next := rewriteLink(text, index, definitions, renderLink)
			result = append(result, replacement)
			index = next
			continue
		}
		if text[index] == '<' {
			autolink := autolinkPattern.FindStringSubmatch(text[index:])
			if autolink != nil && autolinkSchemePattern.MatchString(autolink[1]) {
				target := autolink[1]
				if renderLink != nil {
					result = append(result, renderLink("", target))
				} else if webTarget(target) {
					result = append(result, autolink[0])
				} else {
					result = append(result, linkText("", target))
				}
				index += len(autolink[0])
				continue
			}
			tag := htmlTagPattern.FindStringIndex(text[index:])
			if tag != nil {
				raw := text[index : index+tag[1]]
				var targets []string
				for _, match := range htmlTargetPattern.FindAllStringSubmatch(raw, -1) {
					for _, value := range match[1:] {
						if value != "" {
							targets = append(targets, value)
							break
						}
					}
				}
				if renderLink != nil && len(targets) > 0 {
					parts := make([]string, 0, len(targets))
					for _, target := range targets {
						parts = append(parts, renderLink(raw, target))
					}
					result = append(result, strings.Join(parts, " "))
				} else if len(targets) > 0 && anyNonWeb(targets) {
					result = append(result, linkText("", raw))
				} else {
					result = append(result, raw)
				}
				index += tag[1]
				continue
			}
			if autolink != nil {
				target := autolink[1]
				if renderLink != nil {
					result = append(result, renderLink("", target))
				} else if webTarget(target) {
					result = append(result, autolink[0])
				} else {
					result = append(result, linkText("", target))
				}
				index += len(autolink[0])
				continue
			}
		}
		result = append(result, text[index:index+1])
		index++
	}
	return strings.Join(result, "")
}

// anyNonWeb 任一目标非 web。
func anyNonWeb(targets []string) bool {
	for _, value := range targets {
		if !webTarget(value) {
			return true
		}
	}
	return false
}

// markdownSegment 散文/受保护段标记。
type markdownSegment struct {
	protected bool
	text      string
}

// sourceSegments 在收集链接定义前分离源代码块。
// Python: source_markdown.py:190 _source_segments
func sourceSegments(markdown string) []markdownSegment {
	var segments []markdownSegment
	var prose []string
	fence := ""
	previousBlank := true
	indentedCode := false
	inList := false
	for _, line := range strings.SplitAfter(markdown, "\n") {
		marker := fenceMarkerPattern.FindStringSubmatch(line)
		if fence == "" && listMarkerPattern.MatchString(line) {
			inList = true
		} else if strings.TrimSpace(line) != "" && !isSpaceByte(line[0]) && fence == "" {
			inList = false
		}
		indentedCode = !inList && (previousBlank || indentedCode) &&
			(strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") || strings.TrimSpace(line) == "")
		protected := fence != "" || marker != nil || indentedCode
		if protected {
			if len(prose) > 0 {
				segments = append(segments, markdownSegment{protected: false, text: strings.Join(prose, "")})
				prose = nil
			}
			segments = append(segments, markdownSegment{protected: true, text: line})
			if fence != "" {
				if closingFenceLine(line, fence) {
					fence = ""
				}
			} else if marker != nil {
				fence = marker[1]
			}
		} else {
			prose = append(prose, line)
		}
		previousBlank = strings.TrimSpace(line) == ""
	}
	if len(prose) > 0 {
		segments = append(segments, markdownSegment{protected: false, text: strings.Join(prose, "")})
	}
	return segments
}

// closingFenceLine 判断行是否闭合当前围栏。
// Python: rf" {{0,3}}{re.escape(fence[0])}{{{len(fence)},}}[ \t\r\n]*" fullmatch。
func closingFenceLine(line string, fence string) bool {
	pattern := regexp.MustCompile(
		`^ {0,3}` + regexp.QuoteMeta(string(fence[0])) + `{` + strconv.Itoa(len(fence)) + `,}[ \t\r\n]*$`)
	return pattern.MatchString(line)
}

// escapeHTMLQuoteFalse 等价 Python html.escape(value, quote=False)：只转义 & < >。
func escapeHTMLQuoteFalse(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
}

// escapeSequenceUnescape 等价 Python re.sub(r"\\([!\"#$%&'()*+,\-./:;<=>?@\[\]\\^_`{|}~])", r"\1", ...)。
func escapeSequenceUnescape(target string) string {
	var b strings.Builder
	for index := 0; index < len(target); index++ {
		if target[index] == '\\' && index+1 < len(target) && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(target[index+1])) {
			b.WriteByte(target[index+1])
			index++
			continue
		}
		b.WriteByte(target[index])
	}
	return b.String()
}
