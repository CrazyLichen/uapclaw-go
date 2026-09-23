package extraction

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// regexFindJSONStart 匹配 JSON 起始字符（{ 或 [）
	//
	// Python: REGEX_FIND_JSON_START = re.compile(r"[\[\{]")
	regexFindJSONStart = regexp.MustCompile(`[\[\{]`)

	// regexFindCodeBlock 匹配 Markdown 代码块（含语言标识）
	//
	// Python: REGEX_FIND_CODE_BLOCK = re.compile(r"(?s)```([A-Za-z]*)\s*\n(.*?)```")
	regexFindCodeBlock = regexp.MustCompile("(?s)```([A-Za-z]*)\\s*\\n(.*?)```")

	// wordPattern 匹配单词字符，用于 key 规范化
	//
	// Python: re.compile(r"\w+")
	wordPattern = regexp.MustCompile(`\w+`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSON 尝试从 LLM 响应中解析 JSON
//
// 依次尝试：
//  1. 从 Markdown 代码块中提取 JSON（空类型或 json 类型）
//  2. 从原始文本中 raw decode JSON（支持截断修复）
//
// 如果 outputSchema 提供了 required 字段，则对 dict 结果做模糊 key 匹配过滤。
//
// Python: parse_json(resp, output_schema)
func ParseJSON(resp string, outputSchema map[string]any) JSONLike {
	var mustContainKey []string
	if outputSchema != nil {
		// Python: output_schema.get("json_schema", output_schema).get("required")
		if jsonSchema, ok := outputSchema["json_schema"]; ok {
			if schema, ok := jsonSchema.(map[string]any); ok {
				if req, ok := schema["required"]; ok {
					mustContainKey = toStringSlice(req)
				}
			}
		} else {
			if req, ok := outputSchema["required"]; ok {
				mustContainKey = toStringSlice(req)
			}
		}
	}

	// 第一步：尝试从代码块中提取
	for _, match := range regexFindCodeBlock.FindAllStringSubmatch(resp, -1) {
		codeBlockType := strings.ToLower(match[1])
		if codeBlockType != "" && codeBlockType != "json" {
			continue
		}
		var result any
		if err := json.Unmarshal([]byte(match[2]), &result); err != nil {
			continue
		}
		if mustContainKey != nil {
			if m, ok := result.(map[string]any); ok {
				filtered := make(map[string]any)
				for _, key := range mustContainKey {
					fuzzyMatch := TryGetKey(key, m)
					if fuzzyMatch != "" {
						filtered[key] = m[fuzzyMatch]
					}
				}
				return filtered
			}
			continue
		}
		return result
	}

	// 第二步：raw decode
	return rawDecodeJSON(resp, mustContainKey)
}

// TryGetKey 在 src 字典中模糊匹配 key
//
// 先对 key 和 src 中的键做规范化（去除非单词字符、小写），
// 再用字符串相似度找到最接近的键。
//
// Python: try_get_key(key, src, pattern)
func TryGetKey(key string, src map[string]any) string {
	// 规范化目标 key
	normKey := normalizeKey(key)

	// 构建 规范化key → 原始key 的映射
	norm2key := make(map[string]string)
	for k := range src {
		norm2key[normalizeKey(k)] = k
	}

	// 对齐 Python difflib.get_close_matches(word, possibilities, n=1, cutoff=0.85)
	// 找到相似度 >= 0.85 的最佳匹配
	bestOrigKey := ""
	bestScore := 0.85 // cutoff
	for nk, origK := range norm2key {
		score := sequenceMatcherRatio(normKey, nk)
		if score >= bestScore {
			if bestOrigKey == "" || score > bestScore {
				bestScore = score
				bestOrigKey = origK
			}
		}
	}
	if bestOrigKey != "" {
		return bestOrigKey
	}
	return ""
}

// EnsureList 确保返回对象为列表
//
// 如果 obj 是列表则直接返回；如果 obj 是单元素字典则返回其唯一的值（若为列表）；
// 否则将 obj 包装为单元素列表返回。
//
// Python: ensure_list(obj)
func EnsureList(obj any) []any {
	if list, ok := obj.([]any); ok {
		return list
	}
	if m, ok := obj.(map[string]any); ok && len(m) == 1 {
		for _, v := range m {
			if list, ok := v.([]any); ok {
				return list
			}
		}
	}
	return []any{obj}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rawDecodeJSON 尝试从容错解析 JSON（支持截断修复）
//
// 构建候选文本列表（原始 + 截断修复），然后从每个 JSON 起始位置尝试解析。
//
// Python: _raw_decode_json(resp, must_contain_key)
func rawDecodeJSON(resp string, mustContainKey []string) JSONLike {
	// 构建候选文本列表
	possibleResp := []string{resp}

	// 截断修复：如果响应中存在 "}," 则尝试截断到最后一个 "}," 处并补上 "}]"
	// Python: last_relation_idx = resp.rfind("},")
	lastRelationIdx := strings.LastIndex(resp, "},")
	if lastRelationIdx > 0 {
		possibleResp = append(possibleResp, resp[:lastRelationIdx]+"}]")
	}

	// 对每个 JSON 起始位置尝试解析
	for _, startMatch := range regexFindJSONStart.FindAllStringIndex(resp, -1) {
		startIdx := startMatch[0]
		for _, candidate := range possibleResp {
			var result any
			if err := rawDecodeAt(candidate, startIdx, &result); err != nil {
				continue
			}
			if mustContainKey != nil {
				if m, ok := result.(map[string]any); ok {
					filtered := make(map[string]any)
					for _, key := range mustContainKey {
						fuzzyMatch := TryGetKey(key, m)
						if fuzzyMatch != "" {
							filtered[key] = m[fuzzyMatch]
						}
					}
					return filtered
				}
				continue
			}
			return result
		}
	}
	return nil
}

// rawDecodeAt 从 candidate 的 startIdx 位置开始容错解析 JSON
//
// 对齐 Python json.JSONDecoder.raw_decode(candidate, idx) 的行为：
// 从指定位置开始解析一个完整的 JSON 值。
//
// Go 的 encoding/json.Decoder 无法直接指定起始偏移，
// 因此先截取子串再用 Decoder 解析。
func rawDecodeAt(candidate string, startIdx int, result *any) error {
	substr := candidate[startIdx:]
	dec := json.NewDecoder(bytes.NewReader([]byte(substr)))
	if !dec.More() {
		return &json.SyntaxError{}
	}
	if err := dec.Decode(result); err != nil {
		return err
	}
	return nil
}

// normalizeKey 规范化 key：提取单词字符并转小写
//
// Python: "".join(pattern.findall(key.casefold()))
func normalizeKey(key string) string {
	parts := wordPattern.FindAllString(strings.ToLower(key), -1)
	return strings.Join(parts, "")
}

// sequenceMatcherRatio 对齐 Python difflib.SequenceMatcher.ratio()
//
// 使用 Ratcliff/Obershelp 算法：ratio = 2.0 * M / T
// 其中 M 是匹配字符数（递归查找最长公共子串后左右递归），
// T 是两个字符串长度之和。
func sequenceMatcherRatio(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	matched := countMatchingChars(a, b)
	return 2.0 * float64(matched) / float64(len(a)+len(b))
}

// countMatchingChars 递归计算 Ratcliff/Obershelp 匹配字符数
//
// 1. 找到最长公共子串
// 2. 将匹配部分左右分别递归计算
// 3. 累加匹配长度
func countMatchingChars(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// 找最长公共子串
	iStart, jStart, length := longestCommonSubstring(a, b)
	if length == 0 {
		return 0
	}
	// 递归计算左右部分
	left := countMatchingChars(a[:iStart], b[:jStart])
	right := countMatchingChars(a[iStart+length:], b[jStart+length:])
	return length + left + right
}

// longestCommonSubstring 找到两个字符串的最长公共子串
//
// 返回 (a中起始位置, b中起始位置, 长度)
func longestCommonSubstring(a, b string) (int, int, int) {
	ra := []rune(a)
	rb := []rune(b)
	m, n := len(ra), len(rb)

	if m == 0 || n == 0 {
		return 0, 0, 0
	}

	// 用一维 DP，记录最大值位置
	prev := make([]int, n+1)
	bestLen := 0
	bestI := 0
	bestJ := 0

	for i := 1; i <= m; i++ {
		curr := make([]int, n+1)
		for j := 1; j <= n; j++ {
			if ra[i-1] == rb[j-1] {
				curr[j] = prev[j-1] + 1
				if curr[j] > bestLen {
					bestLen = curr[j]
					bestI = i - bestLen
					bestJ = j - bestLen
				}
			}
		}
		prev = curr
	}
	return bestI, bestJ, bestLen
}

// toStringSlice 将 any 转换为 []string，支持 []any 和 []string
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	if slice, ok := v.([]any); ok {
		result := make([]string, 0, len(slice))
		for _, item := range slice {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
