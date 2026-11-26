// JSON 数组压缩器 + plain_text/source_file 压缩器。
// 对应 Python 代码：offloader/rule_compression/compressors/{plain_text,source_file,json_array}_compressor.py
// （compressors/ 子包合并进本包，偏差 D-24.15-b7——Go 包环不可行，与 a5 同族）
package rulecompression

import (
	"encoding/json"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// jsonCompact JSON 紧凑序列化（ensure_ascii=False + separators=(",", ":") 等价）。
// 偏差 D-24.15-b5：Python dict 序列化保插入序，Go map marshal 键为字典序（语义等价）。
func jsonCompact(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// unchanged 未修改结果。
func unchanged(content string, ct ContentType) RuleCompressionResult {
	return RuleCompressionResult{Content: content, ContentType: ct, Modified: false}
}

// ──────────────────────────── 结构体 ────────────────────────────

// PlainTextCompressor 纯文本压缩器（去重 + 空行收敛）。
// Python: plain_text_compressor.py:9 (PlainTextCompressor)
type PlainTextCompressor struct{}

// Compress 逐行归一去重，空行收敛为单空行。
// Python: plain_text_compressor.py:11 (compress)
func (PlainTextCompressor) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	seen := map[string]bool{}
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		normalized := strings.Join(strings.Fields(line), " ")
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			lines = append(lines, normalized)
		} else if normalized == "" && (len(lines) == 0 || lines[len(lines)-1] != "") {
			lines = append(lines, "")
		}
	}
	compressed := strings.Trim(strings.Join(lines, "\n"), "\n")
	if compressed == content || !MeetsSavingsRatio(content, compressed, ctx) {
		return unchanged(content, ContentTypePlainText)
	}
	return RuleCompressionResult{Content: compressed, ContentType: ContentTypePlainText, Modified: true}
}

// SourceFileCompressor 源文件压缩器：拒绝规则压缩（B-11 消费半区）。
// Python: source_file_compressor.py:10 (SourceFileCompressor)
type SourceFileCompressor struct{}

// Compress 原样返回（offloader 回落 head/tail 预览，经 offload 句柄可召回原文）。
// Python: source_file_compressor.py:19 (compress)
func (SourceFileCompressor) Compress(content string, _ *RuleContext) RuleCompressionResult {
	return RuleCompressionResult{Content: content, ContentType: ContentTypeSourceFile, Modified: false}
}

// ──────────────────────────── JSON 数组压缩器 ────────────────────────────

// JsonArrayCompressor JSON 数组压缩器（CSV/JSONL/有损抽样）。
// Python: json_array_compressor.py:19 (JsonArrayCompressor)
type JsonArrayCompressor struct{}

// Compress 入口。
// Python: json_array_compressor.py:20 (compress)
func (JsonArrayCompressor) Compress(content string, ctx *RuleContext) RuleCompressionResult {
	compressed, lossy := compressJSONInner(content, ctx)
	return RuleCompressionResult{
		Content:     compressed,
		ContentType: ContentTypeJSONArray,
		Modified:    compressed != content,
		Lossy:       lossy,
	}
}

// compressJSONInner 分派四种数组形态。
// Python: json_array_compressor.py:27 (_compress)
func compressJSONInner(content string, ctx *RuleContext) (string, bool) {
	rows := loadJSONArray(content)
	if rows == nil || len(rows) == 0 {
		return content, false
	}
	if allObjects(rows) {
		return compressObjectArray(content, toObjectRows(rows), ctx)
	}
	if allStrings(rows) {
		return compressStringArray(content, rows, ctx)
	}
	if allNumbers(rows) {
		return compressNumberArray(content, rows, ctx)
	}
	return compressMixedArray(content, rows, ctx)
}

// compressObjectArray 对象数组：密度达标转 CSV，否则 JSONL；超预算走有损抽样。
// Python: json_array_compressor.py:39 (_compress_object_array)
func compressObjectArray(content string, rows []map[string]any, ctx *RuleContext) (string, bool) {
	keys := sortedObjectKeys(rows)
	if len(keys) == 0 {
		return content, false
	}
	var candidate string
	if schemaDensity(rows, keys) >= ctx.JSONCSVMinDensity {
		candidate = toCSV(rows, keys)
	} else {
		candidate = toJSONL(rows)
	}
	if FitsBudgetAndSaves(content, candidate, ctx) {
		return candidate, false
	}
	selected := selectSalientRows(rows)
	candidate = formatLossyArray(toAny(selected), len(rows)-len(selected), buildObjectSummary(rows))
	if FitsBudgetAndSaves(content, candidate, ctx) {
		return candidate, len(selected) < len(rows)
	}
	return content, false
}

// compressStringArray 字符串数组：紧凑化，超预算抽首尾+错误+最长。
// Python: json_array_compressor.py:66 (_compress_string_array)
func compressStringArray(content string, rows []any, ctx *RuleContext) (string, bool) {
	compact := jsonCompact(rows)
	if FitsBudgetAndSaves(content, compact, ctx) {
		return compact, false
	}
	if len(rows) <= 6 {
		return content, false
	}
	selectedIndexes := map[int]bool{0: true, len(rows) - 1: true}
	for index, value := range rows {
		if text, ok := value.(string); ok && errorRe.MatchString(text) {
			selectedIndexes[index] = true
		}
	}
	longest := argMax(len(rows), func(i int) int { return len(rows[i].(string)) })
	selectedIndexes[longest] = true
	selected := pickIndexes(rows, selectedIndexes)
	candidate := formatLossyArray(selected, len(rows)-len(selected), nil)
	if FitsBudgetAndSaves(content, candidate, ctx) {
		return candidate, true
	}
	return content, false
}

// compressNumberArray 数值数组：紧凑化，超预算抽首尾/最小/最大/最大跳变邻对。
// Python: json_array_compressor.py:91 (_compress_number_array)
func compressNumberArray(content string, rows []any, ctx *RuleContext) (string, bool) {
	compact := jsonCompact(rows)
	if FitsBudgetAndSaves(content, compact, ctx) {
		return compact, false
	}
	if len(rows) <= 6 {
		return content, false
	}
	selectedIndexes := map[int]bool{0: true, len(rows) - 1: true}
	minIdx := argMaxF(len(rows), func(i int) float64 { return -numValue(rows[i]) })
	maxIdx := argMaxF(len(rows), func(i int) float64 { return numValue(rows[i]) })
	selectedIndexes[minIdx] = true
	selectedIndexes[maxIdx] = true
	changeIdx := argMaxRange(len(rows), 1, func(i int) float64 {
		return absFloat(numValue(rows[i]) - numValue(rows[i-1]))
	})
	selectedIndexes[changeIdx-1] = true
	selectedIndexes[changeIdx] = true
	selected := pickIndexes(rows, selectedIndexes)
	candidate := formatLossyArray(selected, len(rows)-len(selected), nil)
	if FitsBudgetAndSaves(content, candidate, ctx) {
		return candidate, true
	}
	return content, false
}

// compressMixedArray 混合数组：紧凑化，超预算抽首尾+类型首现+错误项。
// Python: json_array_compressor.py:118 (_compress_mixed_array)
func compressMixedArray(content string, rows []any, ctx *RuleContext) (string, bool) {
	compact := jsonCompact(rows)
	if FitsBudgetAndSaves(content, compact, ctx) {
		return compact, false
	}
	if len(rows) <= 6 {
		return content, false
	}
	selectedIndexes := map[int]bool{0: true, len(rows) - 1: true}
	seenTypes := map[string]bool{}
	for index, value := range rows {
		typeName := jsonValueType(value)
		if !seenTypes[typeName] {
			seenTypes[typeName] = true
			selectedIndexes[index] = true
		}
		if errorRe.MatchString(jsonCompact(value)) {
			selectedIndexes[index] = true
		}
	}
	selected := pickIndexes(rows, selectedIndexes)
	candidate := formatLossyArray(selected, len(rows)-len(selected), nil)
	if FitsBudgetAndSaves(content, candidate, ctx) {
		return candidate, true
	}
	return content, false
}

// loadJSONArray 解析 JSON 数组（repair 半区偏差 D-24.15-b6：json_repair 库无 Go 对应，
// 解析失败直接返回 nil）。
// Python: json_array_compressor.py:259 (_load_json_array)
func loadJSONArray(content string) []any {
	var rows []any
	if err := json.Unmarshal([]byte(content), &rows); err != nil {
		return nil
	}
	return rows
}

// ──────────────────────────── 非导出助手 ────────────────────────────

func toObjectRows(rows []any) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = row.(map[string]any)
	}
	return out
}

func allObjects(rows []any) bool {
	for _, row := range rows {
		if _, ok := row.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func allStrings(rows []any) bool {
	for _, row := range rows {
		if _, ok := row.(string); !ok {
			return false
		}
	}
	return true
}

func allNumbers(rows []any) bool {
	for _, row := range rows {
		switch row.(type) {
		case float64, json.Number:
		default:
			return false
		}
	}
	return true
}

func numValue(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func argMaxF(n int, key func(int) float64) int {
	best := 0
	for i := 1; i < n; i++ {
		if key(i) > key(best) {
			best = i
		}
	}
	return best
}

func argMax(n int, key func(int) int) int {
	best := 0
	for i := 1; i < n; i++ {
		if key(i) > key(best) {
			best = i
		}
	}
	return best
}

func argMaxRange(n, start int, key func(int) float64) int {
	best := start
	for i := start + 1; i < n; i++ {
		if key(i) > key(best) {
			best = i
		}
	}
	return best
}

func pickIndexes(rows []any, indexes map[int]bool) []any {
	order := make([]int, 0, len(indexes))
	for i := range indexes {
		order = append(order, i)
	}
	sort.Ints(order)
	out := make([]any, 0, len(order))
	for _, i := range order {
		out = append(out, rows[i])
	}
	return out
}

func toAny(rows []map[string]any) []any {
	out := make([]any, len(rows))
	for i, row := range rows {
		out[i] = row
	}
	return out
}

func sortedObjectKeys(rows []map[string]any) []string {
	set := map[string]bool{}
	for _, row := range rows {
		for key := range row {
			set[key] = true
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func schemaDensity(rows []map[string]any, keys []string) float64 {
	if len(rows) == 0 || len(keys) == 0 {
		return 0
	}
	total := 0
	for _, row := range rows {
		total += len(row)
	}
	return float64(total) / float64(len(rows)*len(keys))
}

func toCSV(rows []map[string]any, keys []string) string {
	var b strings.Builder
	b.WriteString(strings.Join(keys, ","))
	b.WriteString("\n")
	for _, row := range rows {
		cells := make([]string, len(keys))
		for i, key := range keys {
			cells[i] = serializeCSVValue(row[key])
		}
		b.WriteString(strings.Join(cells, ","))
		b.WriteString("\n")
	}
	return "[JSON_ARRAY compressed to CSV]\n" + strings.TrimRight(b.String(), "\n")
}

func serializeCSVValue(value any) string {
	switch v := value.(type) {
	case map[string]any, []any:
		return jsonCompact(v)
	case string:
		if strings.ContainsAny(v, ",\"\n") {
			return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
		}
		return v
	default:
		return jsonCompact(v)
	}
}

func toJSONL(rows []map[string]any) string {
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = jsonCompact(row)
	}
	return "[JSON_ARRAY compressed to JSONL]\n" + strings.Join(lines, "\n")
}

func formatLossyArray(selected []any, omitted int, summary map[string]any) string {
	output := append([]any{}, selected...)
	if omitted > 0 {
		marker := map[string]any{"_omitted": omitted}
		if summary != nil {
			marker["_summary"] = summary
		}
		output = append(output, marker)
	}
	return jsonCompact(output)
}

func buildObjectSummary(rows []map[string]any) map[string]any {
	valueCounts := map[string]map[string]int{}
	minimumCoverage := max(int(float64(len(rows))*0.8), 1)
	for _, key := range sortedObjectKeys(rows) {
		var values []any
		for _, row := range rows {
			if value, ok := row[key]; ok && isScalar(value) {
				values = append(values, value)
			}
		}
		if len(values) < minimumCoverage {
			continue
		}
		counts := map[string]int{}
		for _, value := range values {
			keyText := value
			if _, ok := value.(string); !ok {
				keyText = jsonCompact(value)
			}
			counts[keyText.(string)]++
			if _, ok := keyText.(string); !ok {
				counts[jsonCompact(value)]++
			}
		}
		if len(counts) <= 10 {
			valueCounts[key] = counts
		}
	}
	summary := map[string]any{"total_rows": len(rows)}
	if len(valueCounts) > 0 {
		summary["value_counts"] = valueCounts
	}
	return summary
}

func isScalar(value any) bool {
	switch v := value.(type) {
	case nil, string, bool, float64:
		return true
	case json.Number:
		_ = v
		return true
	}
	return false
}

func jsonValueType(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64, json.Number:
		return "number"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		_ = v
		return "unknown"
	}
}

func selectSalientRows(rows []map[string]any) []map[string]any {
	if len(rows) <= 12 {
		return rows
	}
	type scored struct {
		score int
		index int
		row   map[string]any
	}
	var scoredRows []scored
	for index, row := range rows {
		text := strings.ToLower(jsonCompact(row))
		score := 0
		if index < 5 || index >= len(rows)-5 {
			score += 10
		}
		if errorRe.MatchString(text) {
			score += 20
		}
		scoredRows = append(scoredRows, scored{score, index, row})
	}
	sort.Slice(scoredRows, func(i, j int) bool {
		if scoredRows[i].score != scoredRows[j].score {
			return scoredRows[i].score > scoredRows[j].score
		}
		return scoredRows[i].index < scoredRows[j].index
	})
	if len(scoredRows) > 12 {
		scoredRows = scoredRows[:12]
	}
	sort.Slice(scoredRows, func(i, j int) bool { return scoredRows[i].index < scoredRows[j].index })
	out := make([]map[string]any, 0, len(scoredRows))
	for _, item := range scoredRows {
		out = append(out, item.row)
	}
	return out
}
