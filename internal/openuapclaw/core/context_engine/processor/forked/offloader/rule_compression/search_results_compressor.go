// 搜索结果压缩器。
// 对应 Python 代码：offloader/rule_compression/compressors/search_results_compressor.py（217 行）
package rulecompression

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchMatch 单条搜索匹配。
// Python: search_results_compressor.py:14 (SearchMatch, @dataclass(frozen=True))
type SearchMatch struct {
	FilePath   string
	LineNumber int
	Content    string
	Original   string
	Position   int
	Score      float64
}

// ──────────────────────────── 常量 ────────────────────────────

// 信号正则与打分常量。
// Python: search_results_compressor.py:22-40
var (
	errorSignalRe      = regexp.MustCompile(`(?i)\b(error|failed|failure|traceback|exception|fatal|panic)\b`)
	warningSignalRe    = regexp.MustCompile(`(?i)\b(warn|warning|deprecated)\b`)
	importanceSignalRe = regexp.MustCompile(`(?i)\b(todo|fixme|important|critical)\b`)
)

const (
	queryContentHitScore     = 0.25
	queryContentScoreCap     = 0.60
	queryPathHitScore        = 0.15
	queryPathScoreCap        = 0.30
	errorSignalScore         = 0.50
	warningSignalScore       = 0.35
	importanceSignalScore    = 0.25
	maxSearchMatchScore      = 1.0
	highSignalScoreThreshold = 0.5
	drivePrefixPattern       = `^[A-Za-z]:[\\/]`
	markerPattern            = `([:-])(\d+)([:-])`
)

var (
	drivePrefixRe = regexp.MustCompile(drivePrefixPattern)
	markerRe      = regexp.MustCompile(markerPattern)
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchResultsCompressor 搜索结果压缩器（文件分组 + 打分截断）。
// Python: search_results_compressor.py:43 (SearchResultsCompressor)
type SearchResultsCompressor struct{}

// Compress 入口。
// Python: search_results_compressor.py:45 (compress)
func (SearchResultsCompressor) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	compressed, lossy, details := compressSearch(content, ctx)
	return RuleCompressionResult{
		Content: compressed, ContentType: ContentTypeSearchResults,
		Modified: compressed != content, Lossy: lossy, Details: details,
	}
}

// compressSearch 主链路。
// Python: search_results_compressor.py:54 (_compress)
func compressSearch(content string, ctx *RuleContext) (string, bool, map[string]any) {
	grouped := map[string][]SearchMatch{}
	var order []string
	var unparsedLines []string
	normalizedContent := StripDisplayLinePrefixes(content)
	for position, line := range strings.Split(normalizedContent, "\n") {
		match := ParseSearchMatch(line, position)
		if match == nil {
			if strings.TrimSpace(line) != "" {
				unparsedLines = append(unparsedLines, line)
			}
			continue
		}
		if _, ok := grouped[match.FilePath]; !ok {
			grouped[match.FilePath] = nil
			order = append(order, match.FilePath)
		}
		grouped[match.FilePath] = append(grouped[match.FilePath], *match)
	}
	if len(grouped) == 0 {
		return content, false, map[string]any{}
	}

	scoredGroups := map[string][]SearchMatch{}
	for filePath, matches := range grouped {
		scored := make([]SearchMatch, len(matches))
		for i, match := range matches {
			scored[i] = scoreMatch(match, ctx.QueryTerms)
		}
		scoredGroups[filePath] = scored
	}
	filePositions := map[string]int{}
	for index, filePath := range order {
		filePositions[filePath] = index
	}
	rankedFiles := append([]string(nil), order...)
	sort.Slice(rankedFiles, func(i, j int) bool {
		sumI, sumJ := 0.0, 0.0
		for _, m := range scoredGroups[rankedFiles[i]] {
			sumI += m.Score
		}
		for _, m := range scoredGroups[rankedFiles[j]] {
			sumJ += m.Score
		}
		if sumI != sumJ {
			return sumI > sumJ
		}
		return filePositions[rankedFiles[i]] < filePositions[rankedFiles[j]]
	})
	selectedFiles := rankedFiles
	if len(selectedFiles) > ctx.SearchMaxFiles {
		selectedFiles = selectedFiles[:ctx.SearchMaxFiles]
	}
	originalMatchCount := 0
	for _, matches := range scoredGroups {
		originalMatchCount += len(matches)
	}
	adaptiveLimit := adaptiveLimit(scoredGroups, ctx.SearchMaxTotalMatches)

	lines := []string{"[SEARCH_RESULTS compressed]"}
	lines = append(lines, unparsedLines...)
	selectedCount := 0
	processedFiles := 0
	omittedAny := len(selectedFiles) < len(order)
	for _, filePath := range selectedFiles {
		remaining := adaptiveLimit - selectedCount
		if remaining <= 0 {
			omittedAny = true
			break
		}
		processedFiles++
		matches := scoredGroups[filePath]
		perFileLimit := min(ctx.SearchMaxMatchesPerFile, remaining)
		kept := selectMatches(matches, perFileLimit)
		for _, match := range kept {
			lines = append(lines, match.Original)
		}
		selectedCount += len(kept)
		omitted := len(matches) - len(kept)
		if omitted > 0 {
			omittedAny = true
			lines = append(lines, fmt.Sprintf("[... and %d more matches in %s]", omitted, filePath))
		}
	}

	omittedFiles := len(order) - processedFiles
	if omittedFiles > 0 {
		omittedAny = true
		lines = append(lines, fmt.Sprintf("[... and %d more files omitted]", omittedFiles))
	}

	details := map[string]any{
		"original_match_count": originalMatchCount,
		"retained_match_count": selectedCount,
		"omitted_match_count":  max(originalMatchCount-selectedCount, 0),
		"files_affected":       len(order),
		"files_retained":       processedFiles,
		"unparsed_line_count":  len(unparsedLines),
		"adaptive_match_limit": adaptiveLimit,
	}
	candidate := strings.Join(lines, "\n")
	if omittedAny && MeetsSavingsRatio(content, candidate, ctx) {
		return candidate, true, details
	}
	return content, false, details
}

// adaptiveLimit 多样性/信号兜底的自适应匹配上限。
// Python: search_results_compressor.py:127 (_adaptive_limit)
func adaptiveLimit(grouped map[string][]SearchMatch, hardLimit int) int {
	var matches []SearchMatch
	for _, fileMatches := range grouped {
		matches = append(matches, fileMatches...)
	}
	total := len(matches)
	if total <= 0 {
		return 0
	}
	normalizedContents := map[string]bool{}
	for _, match := range matches {
		normalizedContents[normalizeSearchContent(match.Content)] = true
	}
	diversityRatio := float64(len(normalizedContents)) / float64(total)
	baseLimit := min(5, total)
	diversitySlots := int(math.Round(float64(min(total, hardLimit)-baseLimit) * diversityRatio))
	highSignalCount := 0
	for _, match := range matches {
		if match.Score >= highSignalScoreThreshold {
			highSignalCount++
		}
	}
	signalFloor := min(min(baseLimit+highSignalCount, hardLimit), total)
	return min(max(baseLimit+diversitySlots, signalFloor), min(hardLimit, total))
}

// normalizeSearchContent 数字归一化内容键。
// Python: search_results_compressor.py:151 (_normalize_content)
func normalizeSearchContent(content string) string {
	lowered := regexp.MustCompile(`\b\d+\b`).ReplaceAllString(strings.ToLower(content), "<n>")
	return strings.Join(strings.Fields(lowered), " ")
}

// scoreMatch 相关性打分。
// Python: search_results_compressor.py:157 (_score_match)
func scoreMatch(match SearchMatch, queryTerms map[string]bool) SearchMatch {
	loweredContent := strings.ToLower(match.Content)
	loweredPath := strings.ToLower(match.FilePath)
	contentHits, pathHits := 0, 0
	for term := range queryTerms {
		if strings.Contains(loweredContent, term) {
			contentHits++
		}
		if strings.Contains(loweredPath, term) {
			pathHits++
		}
	}
	score := math.Min(float64(contentHits)*queryContentHitScore, queryContentScoreCap)
	score += math.Min(float64(pathHits)*queryPathHitScore, queryPathScoreCap)
	switch {
	case errorSignalRe.MatchString(match.Content):
		score += errorSignalScore
	case warningSignalRe.MatchString(match.Content):
		score += warningSignalScore
	case importanceSignalRe.MatchString(match.Content):
		score += importanceSignalScore
	}
	score = math.Min(score, maxSearchMatchScore)
	match.Score = score
	return match
}

// selectMatches 截断选择（首尾必保 + 按分排序补位，按位置输出）。
// Python: search_results_compressor.py:185 (_select_matches)
func selectMatches(matches []SearchMatch, limit int) []SearchMatch {
	if limit <= 0 {
		return nil
	}
	if len(matches) <= limit {
		return sortedByPosition(matches)
	}
	selected := map[int]SearchMatch{}
	for _, match := range []SearchMatch{matches[0], matches[len(matches)-1]} {
		if len(selected) < limit {
			selected[match.Position] = match
		}
	}
	sortedByScore := append([]SearchMatch(nil), matches...)
	sort.Slice(sortedByScore, func(i, j int) bool {
		if sortedByScore[i].Score != sortedByScore[j].Score {
			return sortedByScore[i].Score > sortedByScore[j].Score
		}
		return sortedByScore[i].Position < sortedByScore[j].Position
	})
	for _, match := range sortedByScore {
		if len(selected) >= limit {
			break
		}
		if _, ok := selected[match.Position]; !ok {
			selected[match.Position] = match
		}
	}
	values := make([]SearchMatch, 0, len(selected))
	for _, m := range selected {
		values = append(values, m)
	}
	return sortedByPosition(values)
}

func sortedByPosition(matches []SearchMatch) []SearchMatch {
	out := append([]SearchMatch(nil), matches...)
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// ParseSearchMatch 解析 file:line:content 形态的搜索匹配行。
// Python: search_results_compressor.py:205 (parse_search_match)
func ParseSearchMatch(line string, position int) *SearchMatch {
	normalizedLine := StripDisplayLinePrefixes(line)
	drivePrefixEnd := 0
	if drivePrefixRe.MatchString(normalizedLine) {
		drivePrefixEnd = 2
	}
	searchArea := normalizedLine[drivePrefixEnd:]
	for _, loc := range markerRe.FindAllStringSubmatchIndex(searchArea, -1) {
		start := drivePrefixEnd + loc[0]
		end := drivePrefixEnd + loc[1]
		filePath := normalizedLine[:start]
		if filePath == "" {
			continue
		}
		lineNumber := 0
		for _, c := range normalizedLine[start:end] {
			if c >= '0' && c <= '9' {
				lineNumber = lineNumber*10 + int(c-'0')
			}
		}
		return &SearchMatch{
			FilePath:   filePath,
			LineNumber: lineNumber,
			Content:    normalizedLine[end:],
			Original:   normalizedLine,
			Position:   position,
		}
	}
	return nil
}
