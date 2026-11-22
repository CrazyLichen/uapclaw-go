// Python 源：jiuwenswarm/common/utils.py:2541-2717 + :2846-2861（G1 缺口，用户 12-07
// 拍板方案①一比一移植）。对外稳定接口 MaskSensitive：对任意文本做敏感信息脱敏，
// 返回脱敏后的字符串；错误消息/日志统一走本函数，避免各调用点硬编码键名匹配漏变体。
//
// 偏差（登记 D-18-7）：Go RE2 不支持 lookbehind/lookahead（(?<!..)/(?!..)），
// Python 正则中的前后边界断言改写为「匹配后在代码中检查相邻字符」，语义等价；
// 其余步骤（JSON 感知扫描、引号 KV 单向扫描、指纹脱敏、PII 纯掩码）逐条对齐。
package utils

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// sensitiveMask 脱敏占位串。
// Python: :2541 _SENSITIVE_MASK
const sensitiveMask = "******"

// safeAuthorizationOutcomes 免脱敏的鉴权结果枚举值。
// Python: :2635-2637 _SAFE_AUTHORIZATION_OUTCOMES
var safeAuthorizationOutcomes = map[string]bool{
	"allow": true, "deny": true, "block": true, "cancel": true,
}

// tokenCountKeys 纯数字计量键免脱敏（指标 18 位数字不被破坏）。
// Python: :2639-2641 _TOKEN_COUNT_KEYS
var tokenCountKeys = map[string]bool{
	"input_tokens": true, "output_tokens": true, "total_tokens": true,
	"cached_tokens": true, "reasoning_tokens": true, "tokens_used": true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// dataImagePattern base64 图片数据整体掩码。
	// Python: :2542-2544 _DATA_IMAGE_PATTERN
	dataImagePattern = regexp.MustCompile(`data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)

	// kvSensitivePattern 常见敏感字段键值对（不要求值带引号）。
	// 分组：1)键名 2)分隔符 3)可选起始引号 4)值本体 5)可选结束引号。
	// Python: :2552-2572 _KV_SENSITIVE_PATTERN；RE2 偏差：前后边界断言改代码检查
	// （捕获组 0 起点前一字符与键名后一字符不得为字母数字）。
	kvSensitivePattern = regexp.MustCompile(
		`(?i)(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|` +
			`secret[_-]?key|access[_-]?token|refresh[_-]?token|authorization|` +
			`auth[_-]?code|auth[_-]?token|` +
			`user[_-]?id|userid|project[_-]?id|` +
			`amap[_-]?key|map[_-]?ak)` +
			`(\s*[:=]\s*)(["']?)([^,\s"'\]\}]+)(["']?)`)

	// namedQuotedKVStartPattern 被引号包裹的通用键值对起点。
	// Python: :2576-2580 _NAMED_QUOTED_KV_START_PATTERN
	namedQuotedKVStartPattern = regexp.MustCompile(
		`(?i)(?P<prefix>["']?(?P<key>[A-Za-z0-9_.-]+)["']?\s*[:=]\s*)` +
			`(?P<quote>["'])`)

	// namedSensitiveKeyPattern 通用敏感键名分类器（引号 KV 值是否脱敏的判据）。
	// Python: :2582-2586 _NAMED_SENSITIVE_KEY_PATTERN（无边界断言，RE2 直译）。
	namedSensitiveKeyPattern = regexp.MustCompile(
		`(?i)(?:token|secret|password|passwd|pwd|api[_-]?key|access[_-]?key|` +
			`secret[_-]?key|authorization|auth[_-]?code|auth[_-]?token|` +
			`credential|private[_-]?key|user[_-]?id|userid|project[_-]?id|` +
			`amap[_-]?key|map[_-]?ak)`)

	// bearerSensitivePattern Authorization Bearer 令牌（保留前缀）。
	// Python: :2588 _BEARER_SENSITIVE_PATTERN
	bearerSensitivePattern = regexp.MustCompile(`(?i)\b(Bearer\s+)([A-Za-z0-9\-._~+/]+=*)`)

	// cliFlagSensitivePattern 命令行 flag 后跟明文凭证值。
	// Python: :2590-2598 _CLI_FLAG_SENSITIVE_PATTERN
	cliFlagSensitivePattern = regexp.MustCompile(
		`(?i)(--(?:[a-z]+[_-])*(?:token|secret|password|passwd|pwd|api[_-]?key|` +
			`access[_-]?key|secret[_-]?key|auth[_-]?code|auth[_-]?token|` +
			`access[_-]?token|refresh[_-]?token|apikey|authorization)` +
			`(?:[a-z0-9_-]*)(?:\s*=|',\s*'|\s+))([A-Za-z0-9\-._~+/]+=*)`)

	// credentialPatterns 凭证类（JWT/sk-/ghp_/glpat-）：掩码并附指纹。
	// Python: :2600-2614 _SENSITIVE_PATTERNS[:4]
	credentialPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9]{8,}\b`),
		regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`),
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
	}

	// piiPatterns PII（邮箱/手机/身份证）：纯掩码不附指纹。
	// Python: :2614-2621 _SENSITIVE_PATTERNS[-3:]；RE2 偏差：前后断言改代码检查。
	piiPatterns = []*regexp.Regexp{
		regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[A-Za-z]{2,}`),
		regexp.MustCompile(`(?:\+?86[-\s]?)?1[3-9][0-9]{9}`),
		regexp.MustCompile(`[0-9]{17}[0-9Xx]`),
	}

	// safeAuthorizationOutcomePattern 免脱敏的鉴权结果 JSON 对。
	// Python: :2622-2624 _SAFE_AUTHORIZATION_OUTCOME_PATTERN
	safeAuthorizationOutcomePattern = regexp.MustCompile(
		`"authorization_outcome":"(?:allow|deny|block|cancel)"`)

	// alreadyMaskedPattern 识别"已是脱敏产物"，避免指纹的指纹。
	// Python: :2670-2672 _ALREADY_MASKED_PATTERN
	alreadyMaskedPattern = regexp.MustCompile(`^\*\*\*\*\*\*\(fp:[0-9a-f]{8}\)?$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MaskSensitive 对任意文本做敏感信息脱敏，返回脱敏后的字符串。
// Python: :2846-2861 mask_sensitive（nil→空串；str() 后走 _sanitize_log_text）。
func MaskSensitive(text string) string {
	return sanitizeLogText(text)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sanitizeLogText JSON 感知的脱敏主链：JSON 段按值递归脱敏，其余走纯文本链。
// Python: :2717-2755 _sanitize_log_text。步骤对齐：单调扫描 {/[ 起点候选
// （每段至多 32 个，成功后重置）→ raw_decode → dict/list 才递归 → 串联。
func sanitizeLogText(text string) string {
	startPattern := regexp.MustCompile(`[\{\[]`)
	var chunks []string
	copyFrom := 0
	searchFrom := 0
	candidates := 0
	for candidates < 32 {
		loc := startPattern.FindStringIndex(text[searchFrom:])
		if loc == nil {
			break
		}
		start := searchFrom + loc[0]
		searchFrom = start + 1
		candidates++
		decoded, end, ok := rawDecodeJSON(text, start)
		if !ok {
			continue
		}
		chunks = append(chunks, sanitizePlainLogText(text[copyFrom:start]))
		chunks = append(chunks, marshalCompact(sanitizeLogValue(decoded, false)))
		copyFrom = end
		searchFrom = end
		candidates = 0
	}
	chunks = append(chunks, sanitizePlainLogText(text[copyFrom:]))
	return strings.Join(chunks, "")
}

// rawDecodeJSON 从 pos 起解码一个 JSON 值，返回值与结束下标。
// Python: decoder.raw_decode(text, start)（ValueError/RecursionError→跳过）。
// UseNumber 保数字精度（Python int 任意精度；float64 会破坏 18 位计量数字）。
func rawDecodeJSON(text string, pos int) (any, int, bool) {
	dec := json.NewDecoder(strings.NewReader(text[pos:]))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, 0, false
	}
	end := pos + int(dec.InputOffset())
	// Python raw_decode 不吃尾随空白；InputOffset 含已读空白，回退之。
	for end > pos && (text[end-1] == ' ' || text[end-1] == '\t' || text[end-1] == '\n' || text[end-1] == '\r') {
		end--
	}
	return value, end, true
}

// marshalCompact 紧凑序列化（json.dumps(ensure_ascii=False, separators=(",", ":"))）。
func marshalCompact(value any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return ""
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// sanitizeLogValue 按值递归脱敏 JSON 结构。
// Python: :2655-2690 _sanitize_log_value。dict/list/str/数字分支逐条对齐。
func sanitizeLogValue(value any, maskValues bool) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			safeOutcome := key == "authorization_outcome" && isPlainStringIn(item, safeAuthorizationOutcomes)
			sensitive := maskValues || namedSensitiveKeyPattern.MatchString(key)
			if tokenCountKeys[key] && isPlainNumber(item) {
				sensitive = false
			}
			result[sanitizePlainLogText(key)] = sanitizeLogValue(item, sensitive && !safeOutcome)
		}
		return result
	case []any:
		sanitized := make([]any, len(typed))
		for index, item := range typed {
			flagValue := false
			if index > 0 {
				if previous, ok := typed[index-1].(string); ok {
					flagValue = cliFlagValueBoundary(previous)
				}
			}
			sanitized[index] = sanitizeLogValue(item, maskValues || flagValue)
		}
		return sanitized
	case string:
		if maskValues {
			return maskedWithFP(typed)
		}
		return sanitizeLogText(typed)
	case json.Number:
		// Python int/float 的等价分支：字符串形态参与指纹/PII 判定，原值保留精度。
		text := string(typed)
		if maskValues {
			return maskedWithFP(text)
		}
		if isPlainIntText(text) && anyPatternFullMatch(piiPatterns, text) {
			return sensitiveMask
		}
		return typed
	case bool, nil:
		return typed
	default:
		return typed
	}
}

// cliFlagValueBoundary 判断前一个列表元素是否是「'--flag'」形态的敏感 CLI flag。
// Python: :2668-2671 _CLI_FLAG_SENSITIVE_PATTERN.fullmatch(previous + " value")——
// 用「flag 名 + 空格」拼接后做全串匹配等价判定。
func cliFlagValueBoundary(previous string) bool {
	trimmed := strings.Trim(previous, `"'`)
	joined := trimmed + " value"
	loc := cliFlagSensitivePattern.FindStringIndex(joined)
	return loc != nil && loc[0] == 0 && loc[1] == len(joined) &&
		strings.HasPrefix(strings.ToLower(trimmed), "--")
}

// isPlainStringIn 判断值是字符串且在集合内。
func isPlainStringIn(value any, set map[string]bool) bool {
	text, ok := value.(string)
	return ok && set[text]
}

// isPlainIntText 纯整数文本（bool 不是整数；True/False 不算计量或身份号）。
// Python: :2692-2694 _is_plain_int（JSON 解码后数字为 json.Number 字符串形态）。
func isPlainIntText(text string) bool {
	_, err := strconv.ParseInt(text, 10, 64)
	return err == nil
}

// isPlainNumber 整数或小数（json.Number 或 float64）。
// Python: :2696-2698 _is_plain_number。
func isPlainNumber(value any) bool {
	switch typed := value.(type) {
	case json.Number:
		_, err := strconv.ParseFloat(string(typed), 64)
		return err == nil
	case float64:
		return true
	default:
		return false
	}
}

// anyPatternFullMatch 任一 pattern 全串匹配。
// Python: :2686-2689 pattern.fullmatch(str(value))。
func anyPatternFullMatch(patterns []*regexp.Regexp, text string) bool {
	for _, pattern := range patterns {
		loc := pattern.FindStringIndex(text)
		if loc != nil && loc[0] == 0 && loc[1] == len(text) {
			return true
		}
	}
	return false
}

// sanitizePlainLogText 纯文本脱敏链。
// Python: :2700-2745 _sanitize_plain_log_text。步骤对齐：免脱敏鉴权结果先占位 →
// base64 图片 → KV 正则 → 引号 KV 扫描 → Bearer → CLI flag → 凭证指纹 → PII → 还原占位。
func sanitizePlainLogText(text string) string {
	if text == "" {
		return text
	}

	protected := text
	var replacements [][2]string
	for index, loc := range safeAuthorizationOutcomePattern.FindAllStringIndex(text, -1) {
		marker := fmt.Sprintf("__JIUWEN_SAFE_OUTCOME_%d__", index)
		for strings.Contains(text, marker) {
			marker += "_"
		}
		match := text[loc[0]:loc[1]]
		protected = strings.Replace(protected, match, marker, 1)
		replacements = append(replacements, [2]string{marker, match})
	}

	masked := protected
	masked = dataImagePattern.ReplaceAllString(masked, "data:image/*;base64,******")
	masked = replaceKVSensitive(masked)
	masked = maskNamedSensitiveKV(masked)
	masked = bearerSensitivePattern.ReplaceAllStringFunc(masked, func(m string) string {
		parts := bearerSensitivePattern.FindStringSubmatch(m)
		return parts[1] + maskedWithFP(parts[2])
	})
	masked = cliFlagSensitivePattern.ReplaceAllStringFunc(masked, func(m string) string {
		parts := cliFlagSensitivePattern.FindStringSubmatch(m)
		return parts[1] + maskedWithFP(parts[2])
	})
	for _, pattern := range credentialPatterns {
		masked = pattern.ReplaceAllStringFunc(masked, maskedWithFP)
	}
	for _, pattern := range piiPatterns {
		masked = replacePII(pattern, masked)
	}
	for _, pair := range replacements {
		masked = strings.Replace(masked, pair[0], pair[1], 1)
	}
	return masked
}

// alnum 判断字节是否字母数字（Python [A-Za-z0-9] 边界判定）。
func alnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// boundaryOK RE2 无 lookbehind/lookahead 的边界补偿：匹配起点前一字符与
// 指定组结束的后一字符不得为字母数字。
func boundaryOK(text string, start, keyEnd int) bool {
	if start > 0 && alnum(text[start-1]) {
		return false
	}
	if keyEnd < len(text) && alnum(text[keyEnd]) {
		return false
	}
	return true
}

// replaceKVSensitive 敏感 KV 正则替换（边界检查在代码中做）。
// Python: :2711-2714 _KV_SENSITIVE_PATTERN.sub(...)；分组 1/2/4 参与产物。
func replaceKVSensitive(text string) string {
	var out strings.Builder
	last := 0
	for _, loc := range kvSensitivePattern.FindAllStringSubmatchIndex(text, -1) {
		// 组 1 键名 [2,3)；分组 4 值 [8,9)。边界：整体起点前一字符 + 键名后一字符。
		if !boundaryOK(text, loc[0], loc[3]) {
			continue
		}
		out.WriteString(text[last:loc[0]])
		out.WriteString(text[loc[2]:loc[3]]) // 键名
		out.WriteString(text[loc[4]:loc[5]]) // 分隔符
		out.WriteString(maskedWithFP(text[loc[8]:loc[9]]))
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

// replacePII PII 纯掩码（前后边界在代码中检查，对齐 lookbehind/lookahead）。
func replacePII(pattern *regexp.Regexp, text string) string {
	var out strings.Builder
	last := 0
	for _, loc := range pattern.FindAllStringIndex(text, -1) {
		if !piiBoundaryOK(text, loc[0], loc[1]) {
			continue
		}
		out.WriteString(text[last:loc[0]])
		out.WriteString(sensitiveMask)
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

// piiBoundaryOK 邮箱：前一字符不得为 local-part 字符；手机/身份证：前后不得为数字。
// Python: :2618-2621 的 (?<![a-zA-Z0-9._%+-])/(?<!\d)/(?!\d)。
func piiBoundaryOK(text string, start, end int) bool {
	precededBy := func(set func(byte) bool) bool {
		return start == 0 || !set(text[start-1])
	}
	localPart := func(b byte) bool {
		return alnum(b) || b == '.' || b == '_' || b == '%' || b == '+' || b == '-'
	}
	if !precededBy(localPart) {
		return false
	}
	// Python 邮箱尾部 \b：后一字符不得为单词字符；手机/身份证 (?!\d)：后一字符不得为数字。
	if end < len(text) && alnum(text[end]) {
		return false
	}
	return true
}

// maskNamedSensitiveKV 键名含敏感关键词的引号值脱敏（单向扫描）。
// Python: :2686-2715 _mask_named_sensitive_kv。未闭合引号只吞当前行。
func maskNamedSensitiveKV(text string) string {
	var chunks []string
	copyFrom := 0
	searchFrom := 0
	for {
		rest := text[searchFrom:]
		loc := namedQuotedKVStartPattern.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		matchStart := searchFrom + loc[0]
		matchEnd := searchFrom + loc[1]
		searchFrom = matchEnd
		// loc 组下标相对 rest；加 searchFrom 即绝对下标（组 2=key、组 3=quote）。
		key := text[matchStart+loc[4]-loc[0] : matchStart+loc[5]-loc[0]]
		if !namedSensitiveKeyPattern.MatchString(key) {
			continue
		}

		quote := text[matchStart+loc[6]-loc[0] : matchStart+loc[7]-loc[0]]
		valueStart := matchEnd
		lineEnd := strings.Index(text[valueStart:], "\n")
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += valueStart
		}
		valueEnd := strings.Index(text[valueStart:lineEnd], quote)
		if valueEnd < 0 {
			// 未闭合引号的敏感值不得泄露：只掩到行尾，下一行恢复扫描。
			chunks = append(chunks, text[copyFrom:valueStart])
			chunks = append(chunks, maskedWithFP(text[valueStart:lineEnd]))
			copyFrom = lineEnd
			searchFrom = lineEnd
			continue
		}
		valueEnd += valueStart
		chunks = append(chunks, text[copyFrom:valueStart])
		chunks = append(chunks, maskedWithFP(text[valueStart:valueEnd]))
		chunks = append(chunks, quote)
		copyFrom = valueEnd + 1
		searchFrom = copyFrom
	}
	if len(chunks) == 0 {
		return text
	}
	chunks = append(chunks, text[copyFrom:])
	return strings.Join(chunks, "")
}

// fingerprint value 的 SHA256 前 4 字节（8 位 hex）指纹。
// Python: :2659-2667 _fingerprint。
func fingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}

// maskedWithFP 脱敏并附指纹：******(fp:xxxxxxxx)；空值退化纯掩码；
// 已是脱敏产物原样返回（避免指纹的指纹）。
// Python: :2680-2690 _masked_with_fp + :2674-2678 _is_already_masked。
func maskedWithFP(value string) string {
	if alreadyMaskedPattern.MatchString(value) {
		return value
	}
	fp := fingerprint(value)
	if fp == "" {
		return sensitiveMask
	}
	return fmt.Sprintf("%s(fp:%s)", sensitiveMask, fp)
}
