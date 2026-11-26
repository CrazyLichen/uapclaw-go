// git diff 压缩器。
// 对应 Python 代码：offloader/rule_compression/compressors/diff_compressor.py（341 行）
package rulecompression

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DiffHunk 单个 hunk。
// Python: diff_compressor.py:13 (DiffHunk, @dataclass(frozen=True))
type DiffHunk struct {
	Header   string
	Lines    []string
	Position int
}

// Additions 新增行数。Python: diff_compressor.py:17 (additions)
func (h *DiffHunk) Additions() int { return countIf(h.Lines, isAdditionLine) }

// Deletions 删除行数。Python: diff_compressor.py:20 (deletions)
func (h *DiffHunk) Deletions() int { return countIf(h.Lines, isDeletionLine) }

// ChangeCount 变更行数。Python: diff_compressor.py:23 (change_count)
func (h *DiffHunk) ChangeCount() int { return h.Additions() + h.Deletions() }

// DiffFile 单文件 diff。
// Python: diff_compressor.py:27 (DiffFile)
type DiffFile struct {
	Header   []string
	Hunks    []*DiffHunk
	Position int
}

// Label 文件标签。Python: diff_compressor.py:33 (label)
func (f *DiffFile) Label() string {
	if len(f.Header) > 0 {
		return f.Header[0]
	}
	return fmt.Sprintf("diff-file-%d", f.Position)
}

// Additions 新增行数。Python: diff_compressor.py:37
func (f *DiffFile) Additions() int {
	return sumBy(f.Hunks, func(h *DiffHunk) int { return h.Additions() })
}

// Deletions 删除行数。Python: diff_compressor.py:40
func (f *DiffFile) Deletions() int {
	return sumBy(f.Hunks, func(h *DiffHunk) int { return h.Deletions() })
}

// ChangeCount 变更行数。Python: diff_compressor.py:43
func (f *DiffFile) ChangeCount() int {
	return sumBy(f.Hunks, func(h *DiffHunk) int { return h.ChangeCount() })
}

// ──────────────────────────── 常量 ────────────────────────────

// priorityRe 高优词匹配。
// Python: diff_compressor.py:48 (_PRIORITY_RE)
var priorityRe = regexp.MustCompile(`(?i)\b(error|exception|fail|failed|failure|fatal|critical|crash|panic|` +
	`important|note|todo|fixme|hack|xxx|bug|fix|` +
	`security|auth|password|secret|token)\b`)

// ccrRatioThreshold should_offload_original 阈值。
// Python: diff_compressor.py:52 (_CCR_RATIO_THRESHOLD)
const ccrRatioThreshold = 0.8

// ──────────────────────────── 结构体 ────────────────────────────

// DiffCompressor diff 压缩器（文件/hunk/行三层截断）。
// Python: diff_compressor.py:56 (DiffCompressor)
type DiffCompressor struct{}

// Compress 入口。
// Python: diff_compressor.py:60 (compress)
func (DiffCompressor) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	originalLineCount := len(splitLines(content))
	if originalLineCount < ctx.DiffMinLines {
		return RuleCompressionResult{Content: content, ContentType: ContentTypeGitDiff, Modified: false}
	}
	preamble, files := parseDiff(content)
	if len(files) == 0 {
		return RuleCompressionResult{Content: content, ContentType: ContentTypeGitDiff, Modified: false}
	}

	selectedFiles := selectDiffFiles(files, ctx)
	output := append([]string(nil), preamble...)
	hunksRetained, hunksOmitted := 0, 0
	contextLinesRetained, contextLinesOmitted := 0, 0
	changedLinesRetained, changedLinesOmitted := 0, 0
	changedLinesRemaining := max(ctx.DiffMaxChangedLinesTotal, 0)
	additions := sumBy(files, func(f *DiffFile) int { return f.Additions() })
	deletions := sumBy(files, func(f *DiffFile) int { return f.Deletions() })

	sortedSelected := append([]*DiffFile(nil), selectedFiles...)
	sort.Slice(sortedSelected, func(i, j int) bool { return sortedSelected[i].Position < sortedSelected[j].Position })
	for _, file := range sortedSelected {
		output = append(output, file.Header...)
		selectedHunks := selectDiffHunks(file.Hunks, ctx)
		selectedPositions := map[int]bool{}
		for _, hunk := range selectedHunks {
			selectedPositions[hunk.Position] = true
		}
		omittedForFile := len(file.Hunks) - len(selectedHunks)
		for _, hunk := range file.Hunks {
			if !selectedPositions[hunk.Position] {
				continue
			}
			changedLimit := min(max(ctx.DiffMaxChangedLinesPerHunk, 0), changedLinesRemaining)
			reduced, retained, omitted, changedRetained, changedOmitted :=
				reduceHunkLines(hunk.Lines, ctx.DiffMaxContextLines, changedLimit)
			changedLinesRemaining = max(changedLinesRemaining-changedRetained, 0)
			output = append(output, hunk.Header)
			output = append(output, reduced...)
			if omitted != 0 {
				output = append(output, fmt.Sprintf("[%d unchanged/context diff lines omitted]", omitted))
			}
			if changedOmitted != 0 {
				output = append(output, fmt.Sprintf("[%d changed diff lines omitted]", changedOmitted))
			}
			hunksRetained++
			contextLinesRetained += retained
			contextLinesOmitted += omitted
			changedLinesRetained += changedRetained
			changedLinesOmitted += changedOmitted
		}
		if omittedForFile != 0 {
			hunksOmitted += omittedForFile
		}
	}

	filesOmitted := len(files) - len(selectedFiles)
	output = append(output, fmt.Sprintf(
		"[%d files changed, +%d -%d lines, %d hunks omitted, %d files omitted]",
		len(files), additions, deletions, hunksOmitted, filesOmitted,
	))

	candidate := strings.Join(output, "\n")
	compressedLineCount := len(splitLines(candidate))
	details := map[string]any{
		"files_affected":          len(files),
		"files_retained":          len(selectedFiles),
		"files_omitted":           filesOmitted,
		"hunks_affected":          sumBy(files, func(f *DiffFile) int { return len(f.Hunks) }),
		"hunks_retained":          hunksRetained,
		"hunks_omitted":           hunksOmitted,
		"additions":               additions,
		"deletions":               deletions,
		"context_lines_retained":  contextLinesRetained,
		"context_lines_omitted":   contextLinesOmitted,
		"changed_lines_retained":  changedLinesRetained,
		"changed_lines_omitted":   changedLinesOmitted,
		"original_line_count":     originalLineCount,
		"compressed_line_count":   compressedLineCount,
		"should_offload_original": float64(compressedLineCount) < float64(originalLineCount)*ccrRatioThreshold,
	}
	lossy := filesOmitted > 0 || hunksOmitted > 0 || contextLinesOmitted > 0 || changedLinesOmitted > 0
	if candidate != content && lossy && MeetsSavingsRatio(content, candidate, ctx) {
		return RuleCompressionResult{Content: candidate, ContentType: ContentTypeGitDiff, Modified: true, Lossy: true, Details: details}
	}
	return RuleCompressionResult{Content: content, ContentType: ContentTypeGitDiff, Modified: false, Lossy: false, Details: details}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// selectDiffFiles 文件数超限按变更量截断。
// Python: diff_compressor.py:115 (_select_files)
func selectDiffFiles(files []*DiffFile, ctx *RuleContext) []*DiffFile {
	if len(files) <= ctx.DiffMaxFiles {
		return files
	}
	sortedByChange := append([]*DiffFile(nil), files...)
	sort.Slice(sortedByChange, func(i, j int) bool {
		if sortedByChange[i].ChangeCount() != sortedByChange[j].ChangeCount() {
			return sortedByChange[i].ChangeCount() > sortedByChange[j].ChangeCount()
		}
		return sortedByChange[i].Position < sortedByChange[j].Position
	})
	return sortedByChange[:ctx.DiffMaxFiles]
}

// selectDiffHunks hunk 数超限按分截断（首尾必保）。
// Python: diff_compressor.py:124 (_select_hunks)
func selectDiffHunks(hunks []*DiffHunk, ctx *RuleContext) []*DiffHunk {
	limit := ctx.DiffMaxHunksPerFile
	if len(hunks) <= limit {
		return append([]*DiffHunk(nil), hunks...)
	}
	selected := map[int]*DiffHunk{}
	for _, hunk := range []*DiffHunk{hunks[0], hunks[len(hunks)-1]} {
		if len(selected) < limit {
			selected[hunk.Position] = hunk
		}
	}
	sortedByScore := append([]*DiffHunk(nil), hunks...)
	sort.Slice(sortedByScore, func(i, j int) bool {
		text := strings.Join(append([]string{sortedByScore[i].Header}, sortedByScore[i].Lines...), "\n")
		textJ := strings.Join(append([]string{sortedByScore[j].Header}, sortedByScore[j].Lines...), "\n")
		si := scoreDiffText(text, sortedByScore[i].ChangeCount(), ctx.QueryTerms)
		sj := scoreDiffText(textJ, sortedByScore[j].ChangeCount(), ctx.QueryTerms)
		if si != sj {
			return si > sj
		}
		return sortedByScore[i].Position < sortedByScore[j].Position
	})
	for _, hunk := range sortedByScore {
		if len(selected) >= limit {
			break
		}
		if _, ok := selected[hunk.Position]; !ok {
			selected[hunk.Position] = hunk
		}
	}
	return byHunkPosition(selected)
}

func byHunkPosition(m map[int]*DiffHunk) []*DiffHunk {
	positions := make([]int, 0, len(m))
	for p := range m {
		positions = append(positions, p)
	}
	sort.Ints(positions)
	out := make([]*DiffHunk, 0, len(positions))
	for _, p := range positions {
		out = append(out, m[p])
	}
	return out
}

// parseDiff 解析 diff 为文件/hunk 结构。
// Python: diff_compressor.py:154 (_parse_diff)
func parseDiff(content string) ([]string, []*DiffFile) {
	var preamble []string
	var files []*DiffFile
	var fileHeader []string
	var hunks []*DiffHunk
	hunkHeader := ""
	var hunkLines []string
	finishHunk := func() {
		if hunkHeader != "" {
			hunks = append(hunks, &DiffHunk{Header: hunkHeader, Lines: hunkLines, Position: len(hunks)})
		}
		hunkHeader = ""
		hunkLines = nil
	}
	finishFile := func() {
		if fileHeader != nil {
			finishHunk()
			files = append(files, &DiffFile{Header: fileHeader, Hunks: hunks, Position: len(files)})
		}
		fileHeader = nil
		hunks = nil
	}
	for _, line := range splitLines(content) {
		switch {
		case isFileHeader(line):
			finishFile()
			fileHeader = []string{line}
		case fileHeader == nil:
			preamble = append(preamble, line)
		case isHunkHeader(line):
			finishHunk()
			hunkHeader = line
		case hunkHeader == "":
			fileHeader = append(fileHeader, line)
		default:
			hunkLines = append(hunkLines, line)
		}
	}
	finishFile()
	return preamble, files
}

// reduceHunkLines 行级收缩（上下文半径 + 变更行上限 + 高优必保）。
// Python: diff_compressor.py:194 (_reduce_hunk_lines)
func reduceHunkLines(lines []string, maxContext int, maxChanged int) ([]string, int, int, int, int) {
	var changePositions []int
	for index, line := range lines {
		if isChangeLine(line) {
			changePositions = append(changePositions, index)
		}
	}
	var priorityChanges []int
	for _, index := range changePositions {
		if priorityRe.MatchString(lines[index]) {
			priorityChanges = append(priorityChanges, index)
		}
	}
	selectedChanges := map[int]bool{}
	for _, index := range priorityChanges[:min(maxChanged, len(priorityChanges))] {
		selectedChanges[index] = true
	}
	for _, index := range changePositions {
		if len(selectedChanges) >= maxChanged {
			break
		}
		selectedChanges[index] = true
	}

	var kept []string
	contextRetained, contextOmitted, changedRetained, changedOmitted := 0, 0, 0, 0
	for index, line := range lines {
		switch {
		case isContextLine(line):
			near := false
			for change := range selectedChanges {
				if absInt(index-change) <= maxContext {
					near = true
					break
				}
			}
			if near {
				kept = append(kept, line)
				contextRetained++
			} else {
				contextOmitted++
			}
		case isChangeLine(line):
			if selectedChanges[index] {
				kept = append(kept, line)
				changedRetained++
			} else {
				changedOmitted++
			}
		default:
			kept = append(kept, line)
		}
	}
	return kept, contextRetained, contextOmitted, changedRetained, changedOmitted
}

// scoreDiffText hunk 打分（变更量/词项命中/高优词）。
// Python: diff_compressor.py:246 (_score_text)
func scoreDiffText(text string, changeCount int, queryTerms map[string]bool) int {
	lowered := strings.ToLower(text)
	score := min(0.3, float64(changeCount)*0.03)
	hits := 0
	for term := range queryTerms {
		if strings.Contains(lowered, strings.ToLower(term)) {
			hits++
		}
	}
	score += 0.2 * float64(hits)
	if priorityRe.MatchString(text) {
		score += 0.3
	}
	return int(min(score, 1.0) * 1000)
}

// isFileHeader 文件头判定。
// Python: diff_compressor.py:255 (_is_file_header)
func isFileHeader(line string) bool {
	return strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "diff --cc ") || strings.HasPrefix(line, "diff --combined ")
}

// isHunkHeader hunk 头判定。
// Python: diff_compressor.py:259 (_is_hunk_header)
func isHunkHeader(line string) bool {
	return strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "@@@ ")
}

// isChangeLine 变更行。
// Python: diff_compressor.py:262 (_is_change_line)
func isChangeLine(line string) bool { return isAdditionLine(line) || isDeletionLine(line) }

// isAdditionLine 新增行。
// Python: diff_compressor.py:265 (_is_addition_line)
func isAdditionLine(line string) bool {
	return strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ")
}

// isDeletionLine 删除行。
// Python: diff_compressor.py:268 (_is_deletion_line)
func isDeletionLine(line string) bool {
	return strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "--- ")
}

// isContextLine 上下文行。
// Python: diff_compressor.py:271 (_is_context_line)
func isContextLine(line string) bool {
	return (strings.HasPrefix(line, " ") || line == "") && !isChangeLine(line)
}

func absInt(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func countIf(lines []string, pred func(string) bool) int {
	n := 0
	for _, line := range lines {
		if pred(line) {
			n++
		}
	}
	return n
}

func sumBy[T any](items []T, key func(T) int) int {
	total := 0
	for _, item := range items {
		total += key(item)
	}
	return total
}
