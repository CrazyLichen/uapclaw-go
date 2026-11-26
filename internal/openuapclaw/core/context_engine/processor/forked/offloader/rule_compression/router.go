// 内容类型检测与压缩分派路由。
// 对应 Python 代码：offloader/rule_compression/router.py（161 行）
package rulecompression

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuleCompressor 规则压缩器协议（Go 界面）。
// Python: router.py:14 (RuleCompressor Protocol)
type RuleCompressor interface {
	Compress(content string, ctx *RuleContext) RuleCompressionResult
}

// RuleContentRouter 内容类型检测并分派到确定性压缩器。
// Python: router.py:18 (RuleContentRouter)
type RuleContentRouter struct {
	compressors map[ContentType]RuleCompressor
}

// ContentRouter 别名（Python: ContentRouter = RuleContentRouter）。
type ContentRouter = RuleContentRouter

// ──────────────────────────── 常量 ────────────────────────────

// htmlStructuralTagRe HTML 结构标签。
// Python: router.py:19 (_HTML_STRUCTURAL_TAG_RE)
var htmlStructuralTagRe = regexp.MustCompile(`(?i)</?(?:html|head|body|article|main|section|div|script|style|nav|footer|aside|table|thead|tbody|tr|th|td|ul|ol|li|p|pre|code|strong|span|h[1-6])\b`)

// searchLineRe 搜索结果行。
// Python: router.py:22 (_SEARCH_LINE_RE)
var searchLineRe = regexp.MustCompile(`^.+?:\d+[:\-].+$`)

// timestampLogLineRe 时间戳日志行。
// Python: router.py:23 (_TIMESTAMP_LOG_LINE_RE)
var timestampLogLineRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}\b`)

// logRe 日志特征行。
// Python: router.py:24 (_LOG_RE)
var logRe = regexp.MustCompile(`(?im)(` +
	`^=+ (?:test session starts|failures|errors|short test summary info) =+$|` +
	`^collected \d+ items\b|` +
	`^Planning next action for user request:|` +
	`^Tool call requested:|` +
	`^Tool call completed:|` +
	`^Tool result received:|` +
	`^(?:FAILED|ERROR) .+::.+(?: - |$)|` +
	`^\d+\s+(?:failed|passed|skipped|errors?|warnings?)\b|` +
	`\b\d+\s+failed,\s+\d+\s+passed\b|` +
	`^npm (?:ERR!|WARN|info)\b|` +
	`^(?:PASS|FAIL) \S+|` +
	`^Test Suites:|` +
	`^(?:Compiling|Finished|Running) \S+|` +
	`^(?:error|warning)(?:\[[A-Z]\d+\])?:|` +
	`^(?:\[[A-Z]+\]\s*)?(?:ERROR|WARN|WARNING|INFO|DEBUG|TRACE|FATAL|CRITICAL)\b|` +
	`^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}.*\b(?:ERROR|WARN|WARNING|INFO|DEBUG|TRACE|FATAL|CRITICAL)\b|` +
	`^.+?:\d+(?::\d+)?:\s*(?:error|warning):|` +
	`^make(?:\[\d+\])?:|` +
	`^Traceback \(most recent call last\)|` +
	`^\s*File ".+", line \d+|` +
	`^\s*at .+\(.+:\d+:\d+\)|` +
	`^-->\s+.+:\d+:\d+` +
	`)`)

// diffMarkerRe diff 标记行。
var diffGitRe = regexp.MustCompile(`(?m)^diff --git `)
var diffHunkRe = regexp.MustCompile(`(?m)^@@ .+ @@`)

// NewRuleContentRouter 创建路由（七个确定性压缩器）。
// Python: router.py:55 (__init__)
func NewRuleContentRouter() *RuleContentRouter {
	return &RuleContentRouter{compressors: map[ContentType]RuleCompressor{
		ContentTypeJSONArray:     JsonArrayCompressor{},
		ContentTypeGitDiff:       DiffCompressor{},
		ContentTypeHTML:          HtmlCompressor{},
		ContentTypeSearchResults: SearchResultsCompressor{},
		ContentTypeLog:           LogCompressor{},
		ContentTypePlainText:     PlainTextCompressor{},
		ContentTypeSourceFile:    SourceFileCompressor{},
	}}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Detect 内容类型检测。
// Python: router.py:64 (detect)
func (r *RuleContentRouter) Detect(content string, ctx *RuleContext) ContentType {
	// B-11 判定首闸：按路径读的文件以文件名定类型，不做内容嗅探
	if ctx != nil && ctx.FilePath != "" && IsSourceFilePath(ctx.FilePath) {
		return ContentTypeSourceFile
	}
	text := strings.TrimSpace(content)
	if text == "" {
		return ContentTypePlainText
	}
	displayText := StripDisplayLinePrefixes(text)
	jsonText := jsonDetectionText(text)
	var parsed any
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		// 偏差 D-24.15-b6：json_repair 无 Go 对应，修复解析跳过
		parsed = repairJSONArray(jsonText)
	}
	if _, ok := parsed.([]any); ok {
		return ContentTypeJSONArray
	}
	htmlDetection := htmlDetectionText(text)
	lowered := strings.ToLower(htmlDetection)
	if len(lowered) > 20000 {
		lowered = lowered[:20000]
	}
	if r.looksLikeHTML(lowered) {
		return ContentTypeHTML
	}
	if logRe.MatchString(displayText) {
		return ContentTypeLog
	}
	if diffGitRe.MatchString(displayText) || diffHunkRe.MatchString(displayText) {
		return ContentTypeGitDiff
	}
	var lines []string
	for _, line := range strings.Split(displayText, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		matches := 0
		for _, line := range lines {
			if searchLineRe.MatchString(line) && !timestampLogLineRe.MatchString(line) {
				matches++
			}
		}
		if float64(matches)/float64(len(lines)) >= 0.3 {
			return ContentTypeSearchResults
		}
	}
	return ContentTypePlainText
}

// Compress 检测 + 按类型预处理前缀 + 分派。
// Python: router.py:101 (compress)
func (r *RuleContentRouter) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	contentType := r.Detect(content, ctx)
	var routedContent string
	if contentType == ContentTypeGitDiff {
		routedContent = StripDisplayLinePrefixesPreservingBodyWhitespace(content)
	} else {
		switch contentType {
		case ContentTypeHTML, ContentTypeJSONArray, ContentTypeSearchResults, ContentTypeLog:
			routedContent = StripDisplayLinePrefixes(content)
		default:
			routedContent = content
		}
	}
	return r.compressors[contentType].Compress(routedContent, ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jsonDetectionText JSON 检测文本。
// Python: router.py:127 (_json_detection_text)
func jsonDetectionText(content string) string {
	stripped := StripDisplayLinePrefixes(content)
	if stripped != content {
		return stripped
	}
	return content
}

// repairJSONArray 修复解析（偏差 D-24.15-b6：无 json_repair，严格解析失败返回 nil）。
// Python: router.py:133 (_repair_json_array)
func repairJSONArray(content string) any {
	trimmed := strings.TrimLeft(content, " \t\n")
	if !strings.HasPrefix(trimmed, "[") {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil
	}
	return parsed
}

// htmlDetectionText HTML 检测文本。
// Python: router.py:143 (_html_detection_text)
func htmlDetectionText(content string) string {
	stripped := StripDisplayLinePrefixes(content)
	if stripped != content {
		return stripped
	}
	return content
}

// looksLikeHTML HTML 形态判定。
// Python: router.py:148 (_looks_like_html)
func (r *RuleContentRouter) looksLikeHTML(content string) bool {
	htmlPrefixes := []string{"<!doctype html", "<html", "<head", "<body"}
	for _, prefix := range htmlPrefixes {
		if strings.HasPrefix(content, prefix) {
			return true
		}
	}
	return len(htmlStructuralTagRe.FindAllString(content, -1)) >= 2
}
