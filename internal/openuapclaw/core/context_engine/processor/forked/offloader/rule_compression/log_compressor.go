// 日志压缩器。
// 对应 Python 代码：offloader/rule_compression/compressors/log_compressor.py（318 行）
package rulecompression

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// LogLevel 日志级别枚举。
// Python: log_compressor.py:12 (LogLevel(str, Enum))
type LogLevel string

const (
	LogLevelError   LogLevel = "error"
	LogLevelFail    LogLevel = "fail"
	LogLevelWarn    LogLevel = "warn"
	LogLevelInfo    LogLevel = "info"
	LogLevelDebug   LogLevel = "debug"
	LogLevelTrace   LogLevel = "trace"
	LogLevelUnknown LogLevel = "unknown"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LogLine 分类后的日志行。
// Python: log_compressor.py:22 (LogLine, @dataclass(frozen=True))
type LogLine struct {
	Position   int
	Content    string
	Level      LogLevel
	StackTrace bool
	Summary    bool
	Score      int
}

// levelPatterns 级别模式（顺序即优先级）。
// Python: log_compressor.py:31 (_LEVEL_PATTERNS)
var levelPatterns = []struct {
	level   LogLevel
	pattern *regexp.Regexp
}{
	{LogLevelError, regexp.MustCompile(`(?i)\b(?:error|fatal|critical|exception)\b`)},
	{LogLevelFail, regexp.MustCompile(`(?i)\b(?:fail|failed|failure)\b`)},
	{LogLevelWarn, regexp.MustCompile(`(?i)\b(?:warn|warning)\b`)},
	{LogLevelInfo, regexp.MustCompile(`(?i)\binfo\b`)},
	{LogLevelDebug, regexp.MustCompile(`(?i)\bdebug\b`)},
	{LogLevelTrace, regexp.MustCompile(`(?i)\btrace\b`)},
}

// summaryRe 汇总行模式。
// Python: log_compressor.py:39 (_SUMMARY_RE)
var summaryRe = regexp.MustCompile(`^(?:={3,}|-{3,}|\d+\s+(?:passed|failed|skipped|errors?|warnings?)\b|` +
	`(?:Tests?|Suites?):?\s+\d+|(?:TOTAL|Total|Summary)\b|` +
	`(?:Build|Compile|Test).*(?:succeeded|failed|complete))`)

// formatMarkers 构建工具格式标记。
// Python: log_compressor.py:44 (_FORMAT_MARKERS)
var formatMarkers = map[string][]string{
	"pytest": {"=== FAILURES", "=== ERRORS", "test session", "collected ", " passed", " failed"},
	"npm":    {"npm ERR!", "npm WARN", "npm info", "npm http"},
	"cargo":  {"Compiling ", "Finished ", "Running ", "warning: ", "error[E"},
	"jest":   {"PASS ", "FAIL ", "Test Suites:"},
	"make":   {"make[", "make:", "gcc ", "g++ ", "clang "},
}

// levelScores 级别基础分。
// Python: log_compressor.py:129 (_classify_lines score map)
var levelScores = map[LogLevel]int{
	LogLevelError: 90, LogLevelFail: 90, LogLevelWarn: 45,
	LogLevelInfo: 12, LogLevelDebug: 4, LogLevelTrace: 2, LogLevelUnknown: 15,
}

// ──────────────────────────── 结构体 ────────────────────────────

// LogCompressor 日志压缩器（级别分类 + 栈回溯分组 + 上下文保留）。
// Python: log_compressor.py:52 (LogCompressor)
type LogCompressor struct{}

// Compress 入口。
// Python: log_compressor.py:55 (compress)
func (LogCompressor) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	rawLines := splitLines(content)
	if len(rawLines) < ctx.LogMinLines {
		return RuleCompressionResult{Content: content, ContentType: ContentTypeLog, Modified: false}
	}

	lines, stackCount := classifyLogLines(rawLines, ctx.LogStackTraceMaxLines)
	selected, warningsDeduplicated := selectLogLines(lines, ctx)
	if len(selected) == 0 {
		selected = headTailLogLines(lines, min(ctx.LogMaxTotalLines, 20))
	}
	selected = addLogContext(lines, selected, ctx.LogErrorContextLines)
	selected = applyLogCap(selected, ctx.LogMaxTotalLines)
	selectedPositions := map[int]bool{}
	for _, line := range selected {
		selectedPositions[line.Position] = true
	}
	omitted := len(lines) - len(selected)
	counts := levelCounts(lines)
	formatDetected := detectLogFormat(rawLines)
	output := []string{
		fmt.Sprintf("[LOG compressed: format=%s, kept=%d/%d, omitted=%d]", formatDetected, len(selected), len(lines), omitted),
	}
	for _, line := range selected {
		output = append(output, line.Content)
	}
	if omitted != 0 {
		labels := nonzeroLevelLabels(counts)
		labelText := ""
		if len(labels) > 0 {
			labelText = ": " + strings.Join(labels, ", ")
		}
		output = append(output, fmt.Sprintf("[LOG omitted: %d lines omitted%s]", omitted, labelText))
	}
	candidate := strings.Join(output, "\n")
	selectedLineNumbers := make([]int, 0, len(selectedPositions))
	for position := range selectedPositions {
		selectedLineNumbers = append(selectedLineNumbers, position+1)
	}
	sort.Ints(selectedLineNumbers)
	details := map[string]any{
		"format_detected":       formatDetected,
		"total_lines":           len(lines),
		"selected_lines":        len(selected),
		"omitted_lines":         omitted,
		"errors":                counts[LogLevelError],
		"fails":                 counts[LogLevelFail],
		"warnings":              counts[LogLevelWarn],
		"info":                  counts[LogLevelInfo],
		"stack_traces_seen":     stackCount,
		"stack_traces_retained": len(groupTraces(selected)),
		"warnings_deduplicated": warningsDeduplicated,
		"selected_line_numbers": selectedLineNumbers,
	}
	if omitted > 0 && MeetsSavingsRatio(content, candidate, ctx) {
		return RuleCompressionResult{Content: candidate, ContentType: ContentTypeLog, Modified: true, Lossy: true, Details: details}
	}
	return RuleCompressionResult{Content: content, ContentType: ContentTypeLog, Modified: false, Lossy: false, Details: details}
}

// selectLogLines 选行（错误/失败/去重警告/栈回溯/汇总）。
// Python: log_compressor.py:92 (_select)
func selectLogLines(lines []LogLine, ctx *RuleContext) ([]LogLine, int) {
	var errors, fails, warnings []LogLine
	for _, line := range lines {
		switch line.Level {
		case LogLevelError:
			errors = append(errors, line)
		case LogLevelFail:
			fails = append(fails, line)
		case LogLevelWarn:
			warnings = append(warnings, line)
		}
	}
	dedupedWarnings := dedupeWarnings(warnings)
	traces := groupTraces(lines)
	var summaries []LogLine
	for _, line := range lines {
		if line.Summary {
			summaries = append(summaries, line)
		}
	}
	selected := firstLastTop(errors, ctx.LogMaxErrors)
	selected = append(selected, firstLastTop(fails, ctx.LogMaxErrors)...)
	selected = append(selected, dedupedWarnings[:min(ctx.LogMaxWarnings, len(dedupedWarnings))]...)
	for _, trace := range traces[:min(ctx.LogMaxStackTraces, len(traces))] {
		selected = append(selected, trace[:min(ctx.LogStackTraceMaxLines, len(trace))]...)
	}
	selected = append(selected, summaries...)
	return dedupeByPosition(selected), len(warnings) - len(dedupedWarnings)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nonzeroLevelLabels 非零级别标签。
// Python: log_compressor.py:110 (_nonzero_level_labels)
func nonzeroLevelLabels(counts map[LogLevel]int) []string {
	var labels []string
	for _, pair := range []struct {
		level LogLevel
		label string
	}{{LogLevelError, "ERROR"}, {LogLevelFail, "FAIL"}, {LogLevelWarn, "WARN"}, {LogLevelInfo, "INFO"}} {
		if counts[pair.level] != 0 {
			labels = append(labels, fmt.Sprintf("%d %s", counts[pair.level], pair.label))
		}
	}
	return labels
}

// classifyLogLines 行分类（含栈回溯状态机）。
// Python: log_compressor.py:120 (_classify_lines)
func classifyLogLines(lines []string, maxTraceLines int) ([]LogLine, int) {
	classified := make([]LogLine, 0, len(lines))
	traceFlavor := ""
	traceLines := 0
	tracesSeen := 0
	for position, content := range lines {
		opened := traceFlavorOf(content)
		if opened != "" && traceFlavor == "" {
			traceFlavor = opened
			traceLines = 0
			tracesSeen++
		} else if traceFlavor != "" && terminatesTrace(traceFlavor, content) {
			traceFlavor = ""
			traceLines = 0
			opened = traceFlavorOf(content)
			if opened != "" {
				traceFlavor = opened
				tracesSeen++
			}
		}
		inTrace := traceFlavor != ""
		if inTrace {
			traceLines++
			if traceLines >= maxTraceLines {
				traceFlavor = ""
			}
		}
		level := classifyLogLevel(content)
		summary := summaryRe.MatchString(content)
		score := levelScores[level]
		if inTrace {
			score += 35
		}
		if summary {
			score += 35
		}
		classified = append(classified, LogLine{
			Position: position, Content: content, Level: level,
			StackTrace: inTrace, Summary: summary, Score: min(score, 100),
		})
	}
	return classified, tracesSeen
}

// traceFlavorOf 栈回溯风格判定。
// Python: log_compressor.py:160 (_trace_flavor)
func traceFlavorOf(line string) string {
	stripped := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(stripped, "Traceback (most recent call last)") || strings.HasPrefix(stripped, `File "`) {
		return "python"
	}
	if regexp.MustCompile(`^at .+\(.+:\d+:\d+\)`).MatchString(stripped) {
		return "js"
	}
	if regexp.MustCompile(`^at [\w.$]+\(`).MatchString(stripped) {
		return "java"
	}
	if strings.HasPrefix(stripped, "--> ") && regexp.MustCompile(`:\d+:\d+`).MatchString(stripped) {
		return "rust"
	}
	if regexp.MustCompile(`(?i)^\d+:\s+0x[0-9a-f]+`).MatchString(stripped) {
		return "go"
	}
	return ""
}

// terminatesTrace 栈回溯终止判定。
// Python: log_compressor.py:176 (_terminates_trace)
func terminatesTrace(flavor string, line string) bool {
	stripped := strings.TrimLeft(line, " \t")
	switch flavor {
	case "python":
		if line == "" || line[:1] == " " || line[:1] == "\t" {
			return false
		}
		if strings.HasPrefix(stripped, "Traceback") || strings.HasPrefix(stripped, "During handling") ||
			strings.HasPrefix(stripped, "The above exception") {
			return false
		}
		if regexp.MustCompile(`^[A-Za-z_][\w.]*(?:Error|Exception)?:`).MatchString(stripped) {
			return false
		}
		return true
	case "js", "java":
		return line != "" && !strings.HasPrefix(stripped, "at ")
	case "rust":
		return line != "" && !strings.HasPrefix(stripped, "--> ")
	default:
		return line != "" && !regexp.MustCompile(`(?i)^\d+:\s+0x`).MatchString(stripped)
	}
}

// classifyLogLevel 级别分类。
// Python: log_compressor.py:197 (_classify_level)
func classifyLogLevel(line string) LogLevel {
	for _, pair := range levelPatterns {
		if pair.pattern.MatchString(line) {
			return pair.level
		}
	}
	return LogLevelUnknown
}

// detectLogFormat 构建工具格式探测。
// Python: log_compressor.py:203 (_detect_format)
func detectLogFormat(lines []string) string {
	sample := lines
	if len(sample) > 100 {
		sample = sample[:100]
	}
	best, bestScore := "", 0
	for name, markers := range formatMarkers {
		score := 0
		for _, line := range sample {
			for _, marker := range markers {
				if strings.Contains(line, marker) {
					score++
					break
				}
			}
		}
		if score > bestScore {
			best, bestScore = name, score
		}
	}
	if bestScore == 0 {
		return "generic"
	}
	return best
}

// groupTraces 连续栈回溯分组。
// Python: log_compressor.py:218 (_group_traces)
func groupTraces(lines []LogLine) [][]LogLine {
	var groups [][]LogLine
	var current []LogLine
	for _, line := range lines {
		if line.StackTrace {
			current = append(current, line)
		} else if len(current) > 0 {
			groups = append(groups, current)
			current = nil
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

// firstLastTop 首尾必保 + 高分补位。
// Python: log_compressor.py:228 (_first_last_top)
func firstLastTop(lines []LogLine, limit int) []LogLine {
	if len(lines) <= limit {
		return lines
	}
	selected := map[int]LogLine{lines[0].Position: lines[0], lines[len(lines)-1].Position: lines[len(lines)-1]}
	sortedByScore := append([]LogLine(nil), lines...)
	sort.Slice(sortedByScore, func(i, j int) bool {
		if sortedByScore[i].Score != sortedByScore[j].Score {
			return sortedByScore[i].Score > sortedByScore[j].Score
		}
		return sortedByScore[i].Position < sortedByScore[j].Position
	})
	for _, line := range sortedByScore {
		if len(selected) >= limit {
			break
		}
		if _, ok := selected[line.Position]; !ok {
			selected[line.Position] = line
		}
	}
	return byPosition(selected)
}

// dedupeWarnings 警告去重（数值/地址/路径归一）。
// Python: log_compressor.py:241 (_dedupe_warnings)
func dedupeWarnings(lines []LogLine) []LogLine {
	seen := map[string]bool{}
	var result []LogLine
	for _, line := range lines {
		prefix, separator, suffix := partition(line.Content, ":")
		if separator == "" {
			prefix, separator, suffix = partition(line.Content, "=")
		}
		normalizedSuffix := regexp.MustCompile(`(?i)0x[0-9a-f]+`).ReplaceAllString(suffix, "ADDR")
		normalizedSuffix = regexp.MustCompile(`\d+`).ReplaceAllString(normalizedSuffix, "N")
		normalizedSuffix = regexp.MustCompile(`[/\\][\w./\\-]+`).ReplaceAllString(normalizedSuffix, "/PATH/")
		key := prefix + separator + normalizedSuffix
		if !seen[key] {
			seen[key] = true
			result = append(result, line)
		}
	}
	return result
}

// addLogContext 保留错误上下文半径内行。
// Python: log_compressor.py:256 (_add_context)
func addLogContext(lines []LogLine, selected []LogLine, radius int) []LogLine {
	positions := map[int]bool{}
	for _, line := range selected {
		positions[line.Position] = true
	}
	for position := range positions {
		for i := max(0, position-radius); i < min(len(lines), position+radius+1); i++ {
			positions[i] = true
		}
	}
	var out []LogLine
	for _, line := range lines {
		if positions[line.Position] {
			out = append(out, line)
		}
	}
	return out
}

// applyLogCap 总量上限（超限按分数留）。
// Python: log_compressor.py:265 (_apply_cap)
func applyLogCap(lines []LogLine, limit int) []LogLine {
	if len(lines) <= limit {
		return lines
	}
	sortedByScore := append([]LogLine(nil), lines...)
	sort.Slice(sortedByScore, func(i, j int) bool {
		if sortedByScore[i].Score != sortedByScore[j].Score {
			return sortedByScore[i].Score > sortedByScore[j].Score
		}
		return sortedByScore[i].Position < sortedByScore[j].Position
	})
	selected := sortedByScore[:limit]
	m := map[int]LogLine{}
	for _, line := range selected {
		m[line.Position] = line
	}
	return byPosition(m)
}

// headTail 头尾兜底选择。
// Python: log_compressor.py:273 (_head_tail)
func headTailLogLines(lines []LogLine, limit int) []LogLine {
	if limit <= 0 {
		return nil
	}
	if len(lines) <= limit {
		return lines
	}
	headCount := max(limit/2, 1)
	tailCount := max(limit-headCount, 0)
	combined := append(append([]LogLine(nil), lines[:headCount]...), lines[len(lines)-tailCount:]...)
	return dedupeByPosition(combined)
}

// dedupeByPosition 位置去重（保序）。
// Python: log_compressor.py:281 (_dedupe_by_position)
func dedupeByPosition(lines []LogLine) []LogLine {
	m := map[int]LogLine{}
	for _, line := range lines {
		m[line.Position] = line
	}
	return byPosition(m)
}

// levelCounts 级别计数。
// Python: log_compressor.py:285 (_level_counts)
func levelCounts(lines []LogLine) map[LogLevel]int {
	counts := map[LogLevel]int{}
	for _, level := range []LogLevel{LogLevelError, LogLevelFail, LogLevelWarn, LogLevelInfo, LogLevelDebug, LogLevelTrace, LogLevelUnknown} {
		n := 0
		for _, line := range lines {
			if line.Level == level {
				n++
			}
		}
		counts[level] = n
	}
	return counts
}

// partition strings.Cut 语义（Python str.partition）。
func partition(s, sep string) (string, string, string) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], sep, s[i+len(sep):]
	}
	return s, "", ""
}

func byPosition(m map[int]LogLine) []LogLine {
	positions := make([]int, 0, len(m))
	for p := range m {
		positions = append(positions, p)
	}
	sort.Ints(positions)
	out := make([]LogLine, 0, len(positions))
	for _, p := range positions {
		out = append(out, m[p])
	}
	return out
}

// splitLines splitlines 等价（\r\n 归一）。
func splitLines(content string) []string {
	return strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
}
