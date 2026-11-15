package harnessrsi

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ──────────────────────────── 常量 ────────────────────────────

// mojibakeMarkers GBK 误解码 UTF-8 时高频出现的乱码字符集合（对齐 Python _MOJIBAKE_MARKERS）
var mojibakeMarkers = map[rune]bool(buildMojibakeMarkers())

// latinMojibakeMarkers Latin 系误解码标记（对齐 _LATIN_MOJIBAKE_MARKERS）
const latinMojibakeMarkers = "\u00c3\u00c2\u00e2\u20ac\ufffd"

// mojibakeSequences 强特征乱码序列（对齐 _MOJIBAKE_SEQUENCES）
var mojibakeSequences = []string{
	"\u7487\u8702", "\u6d93\u20ac", "\u9286", "\u9225", "\u934f",
	"\u93c2", "\u9428", "\u6d63", "\u4e47",
}

// suspiciousMojibakeBlocks 可疑 Unicode 区块（对齐 _SUSPICIOUS_MOJIBAKE_BLOCKS）
const suspiciousMojibakeBlocks = "ARMENIAN LATIN EXTENDED IPA EXTENSIONS"

// markerMinCount dense 评分触发的最小 marker 数
const markerMinCount = 4

// markerMinDensity dense 评分触发的最小 marker/CJK 密度
const markerMinDensity = 0.18

// ──────────────────────────── 导出函数 ────────────────────────────

// RepairTextMojibake 修复常见乱码，正常文本保持不变。
func RepairTextMojibake(value string) string {
	if value == "" {
		return value
	}

	originalScore := mojibakeScore(value)
	if originalScore <= 0 {
		return value
	}

	best := value
	bestScore := originalScore
	for _, encoding := range repairEncodings() {
		for _, encodeErrors := range []string{"strict", "ignore"} {
			raw, ok := encodeString(value, encoding, encodeErrors)
			if !ok {
				continue
			}
			for _, decodeErrors := range []string{"strict", "ignore"} {
				candidate, ok := decodeUTF8(raw, decodeErrors)
				if !ok {
					continue
				}
				candidate = strings.TrimSpace(candidate)
				if candidate == "" || candidate == value {
					continue
				}
				candidateScore := mojibakeScore(candidate)
				if betterRepair(value, candidate, bestScore, candidateScore) {
					best = candidate
					bestScore = candidateScore
				}
			}
		}
	}
	return best
}

// RepairPayloadMojibake 递归修复 JSON/YAML 载荷内的字符串。
func RepairPayloadMojibake(value any) any {
	switch typed := value.(type) {
	case string:
		return RepairTextMojibake(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = RepairPayloadMojibake(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = RepairPayloadMojibake(item)
		}
		return out
	default:
		return value
	}
}

// HasUnrepairedMojibake 返回文本在尽力修复后是否仍像乱码。
func HasUnrepairedMojibake(value string) bool {
	if value == "" {
		return false
	}
	text := value
	if repaired := RepairTextMojibake(text); repaired != text {
		text = repaired
	}
	return unrepairedMojibakeScore(text) >= 4
}

// ModelOutputHasMojibake 返回原始模型文本是否明显是乱码。
func ModelOutputHasMojibake(raw string) bool {
	if raw == "" {
		return false
	}
	return RepairTextMojibake(raw) != raw || HasUnrepairedMojibake(raw)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMojibakeMarkers 构造乱码字符集合（逐字复制 Python _MOJIBAKE_MARKERS）
func buildMojibakeMarkers() map[rune]bool {
	const markers = "\u7487\u8702\u8d1f\u93c2\u62cc\u5158\u5a67\u612c\u504d\u9473" +
		"\u6212\u7d12\u6d93\u6c2c\u57d7\u6d63\u6ec5\u7d89\u6924\u70b9" +
		"\u6d63\u95c2\u820d\u67ca\u9352\u6735\u7d94\u9462\u4ecb\u7d12" +
		"\u701a\u8235\u7c2e\u934c\u3128\u6d7c\u4f77\u7b1f\u95c8\u9598" +
		"\u9428\u509b\u878d\u8d44\u94bb\u70b9\u94fe"
	out := make(map[rune]bool, len(markers))
	for _, r := range markers {
		out[r] = true
	}
	return out
}

// repairEncodings 修复链编码清单（对齐 Python ("gbk","gb18030","latin1","cp1252")）
func repairEncodings() []string {
	return []string{"gbk", "gb18030", "latin1", "cp1252"}
}

// encodeString 按指定编码与错误策略编码；errors=ignore 时不可编码字符逐个跳过。
func encodeString(text, encoding, encodeErrors string) ([]byte, bool) {
	enc := encodingFor(encoding)
	if enc == nil {
		return nil, false
	}
	raw, err := enc.NewEncoder().Bytes([]byte(text))
	if err == nil {
		return raw, true
	}
	if encodeErrors != "ignore" {
		return nil, false
	}
	// ignore：逐 rune 编码，跳过不可编码字符（对齐 Python errors="ignore"）
	var buf []byte
	for _, r := range text {
		part, err := enc.NewEncoder().Bytes([]byte(string(r)))
		if err != nil {
			continue
		}
		buf = append(buf, part...)
	}
	return buf, true
}

// decodeUTF8 按 UTF-8 与错误策略解码；errors=ignore 时无效字节替换为 U+FFFD 语义上跳过。
func decodeUTF8(raw []byte, decodeErrors string) (string, bool) {
	if utf8.Valid(raw) {
		return string(raw), true
	}
	if decodeErrors != "ignore" {
		return "", false
	}
	// ignore：逐字节重织，丢弃无效序列（对齐 Python errors="ignore"）
	var sb strings.Builder
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		sb.WriteRune(r)
		i += size
	}
	return sb.String(), true
}

// encodingFor 返回修复链编码对应的 x/text 编码器
func encodingFor(encoding string) encoding.Encoding {
	switch encoding {
	case "gbk":
		return simplifiedchinese.GBK
	case "gb18030":
		return simplifiedchinese.GB18030
	case "latin1":
		return charmap.ISO8859_1
	case "cp1252":
		return charmap.Windows1252
	default:
		return nil
	}
}

// betterRepair 判定候选修复是否优于当前最优（对齐 _better_repair）
func betterRepair(original, candidate string, originalScore, candidateScore int) bool {
	if candidateScore >= originalScore {
		return false
	}
	if cjkCount(candidate) < 2 {
		return false
	}
	minLen := len(original) / 4
	if minLen < 2 {
		minLen = 2
	}
	return len(candidate) >= minLen
}

// mojibakeScore 综合乱码评分（对齐 _mojibake_score）
func mojibakeScore(text string) int {
	score := denseMarkerScore(text)
	score += strings.Count(text, "\ufffd") * 3
	questionCount := strings.Count(text, "?")
	if questionCount >= 2 && cjkCount(text) > 0 {
		score += questionCount
	}
	for _, marker := range []string{"\u00c3", "\u00c2", "\u00e2\u20ac"} {
		score += strings.Count(text, marker) * 2
	}
	for _, marker := range mojibakeSequences {
		score += strings.Count(text, marker) * 4
	}
	score += suspiciousScriptScore(text)
	return score
}

// unrepairedMojibakeScore 未修复残留学分（对齐 _unrepaired_mojibake_score）
func unrepairedMojibakeScore(text string) int {
	score := 0
	for _, marker := range mojibakeSequences {
		score += strings.Count(text, marker) * 4
	}
	score += strings.Count(text, "\ufffd") * 3
	score += suspiciousScriptScore(text)
	return score
}

// denseMarkerScore 高密度 marker 评分（对齐 _dense_marker_score）
func denseMarkerScore(text string) int {
	markerCount := 0
	for _, r := range text {
		if mojibakeMarkers[r] {
			markerCount++
		}
	}
	if markerCount < markerMinCount {
		return 0
	}
	cjkCountValue := cjkCount(text)
	if cjkCountValue == 0 {
		return markerCount
	}
	if float64(markerCount)/float64(cjkCountValue) < markerMinDensity {
		return 0
	}
	return markerCount
}

// suspiciousScriptScore 可疑脚本区字符评分（对齐 _suspicious_script_score）；
// Python 用 unicodedata.name 前缀匹配，Go 用等价码点区间（偏差 D-1）。
func suspiciousScriptScore(text string) int {
	total := 0
	suspicious := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if r < 0x80 {
			continue
		}
		if inSuspiciousBlock(r) {
			suspicious++
		}
	}
	if total == 0 || suspicious < 4 {
		return 0
	}
	if float64(suspicious)/float64(total) < 0.08 {
		return 0
	}
	return suspicious
}

// inSuspiciousBlock 判断字符是否属于可疑区块（ARMENIAN/LATIN EXTENDED/IPA EXTENSIONS）
func inSuspiciousBlock(r rune) bool {
	switch {
	// ARMENIAN
	case r >= 0x0530 && r <= 0x058F:
		return true
	// IPA EXTENSIONS
	case r >= 0x0250 && r <= 0x02AF:
		return true
	// LATIN EXTENDED-A / EXTENDED-B
	case r >= 0x0100 && r <= 0x024F:
		return true
	default:
		return false
	}
}

// cjkCount 统计 CJK 统一表意文字数量（对齐 _cjk_count）
func cjkCount(text string) int {
	count := 0
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			count++
		}
	}
	return count
}
