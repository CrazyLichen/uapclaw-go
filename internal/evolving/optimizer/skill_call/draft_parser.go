package skill_call

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ParsedExperienceDraft 解析后的 LLM 输出草稿，在持久化为 EvolutionRecord 前的中间形态。
//
// 对应 Python: experience_draft_parser.py ParsedExperienceDraft
type ParsedExperienceDraft struct {
	// Patch 演进补丁
	Patch checkpointing.EvolutionPatch
	// Summary 摘要（可选）
	Summary *string
	// Keywords 关键词（可选）
	Keywords []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// headingRE Markdown 章节标题正则
// 对齐 Python: _HEADING_RE = re.compile(r"^#{1,4}\s+")
var headingRE = regexp.MustCompile(`^#{1,4}\s+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeKeywords 规范化可选关键词列表，从 LLM JSON 输出中提取。
//
// 对齐 Python: normalize_keywords(raw)
//
//	if not isinstance(raw, list): return None
//	keywords = [str(item).strip() for item in raw if str(item).strip()]
//	return keywords or None
func NormalizeKeywords(raw any) []string {
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	var keywords []string
	for _, item := range list {
		s := strings.TrimSpace(fmt.Sprintf("%v", item))
		if s != "" {
			keywords = append(keywords, s)
		}
	}
	if len(keywords) == 0 {
		return nil
	}
	return keywords
}

// NormalizeSummary 规范化可选的单行经验摘要，从 LLM JSON 输出中提取。
//
// 对齐 Python: normalize_summary(raw)
//
//	if not isinstance(raw, str): return None
//	summary = " ".join(raw.split())
//	if not summary or summary.lower() == "null": return None
//	return summary
func NormalizeSummary(raw any) *string {
	if raw == nil {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	summary := strings.Join(strings.Fields(s), " ")
	if summary == "" || strings.ToLower(summary) == "null" {
		return nil
	}
	return &summary
}

// ParseExperienceDraft 将单个 JSON 对象解析为 ParsedExperienceDraft。
//
// 对齐 Python: parse_experience_draft(data)
//
//	action = data.get("action", "append")
//	if action == "skip": return ParsedExperienceDraft(patch=EvolutionPatch(action="skip", ...))
//	section 校验: section not in VALID_SECTIONS → fallback "Troubleshooting"
//	target 解析: EvolutionTarget(raw_target)，失败 → fallback BODY
//	merge_target: "null"/None → nil（空值映射）
func ParseExperienceDraft(data map[string]any) *ParsedExperienceDraft {
	if data == nil {
		return nil
	}
	action := getStr(data, "action", "append")

	// 对齐 Python: if action == "skip": return ParsedExperienceDraft(patch=EvolutionPatch(action="skip", ...))
	if action == "skip" {
		skipReason := "unknown"
		if v, ok := data["skip_reason"]; ok && v != nil {
			skipReason = fmt.Sprintf("%v", v)
		}
		return &ParsedExperienceDraft{
			Patch: checkpointing.EvolutionPatch{
				Section:    "",
				Action:     "skip",
				Content:    "",
				SkipReason: &skipReason,
			},
			Summary:  nil,
			Keywords: nil,
		}
	}

	// 对齐 Python: section = data.get("section", "Troubleshooting")
	//	if section not in VALID_SECTIONS: section = "Troubleshooting"
	section := getStr(data, "section", "Troubleshooting")
	if !schema.ValidSections[section] {
		section = "Troubleshooting"
	}

	// 对齐 Python: target = EvolutionTarget(raw_target)，失败 → fallback BODY
	rawTarget := getStr(data, "target", "body")
	var target signal.EvolutionTarget
	if parsed, err := signal.ParseEvolutionTarget(rawTarget); err != nil {
		target = signal.EvolutionTargetBody
	} else {
		target = parsed
	}

	// 对齐 Python: merge_target = data.get("merge_target")
	//	if merge_target in ("null", None): merge_target = None
	var mergeTarget *string
	if v, ok := data["merge_target"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		if s != "null" && s != "" {
			mergeTarget = &s
		}
	}

	// 对齐 Python: keywords = normalize_keywords(data.get("keywords"))
	//	summary = normalize_summary(data.get("summary"))
	keywords := NormalizeKeywords(data["keywords"])
	summary := NormalizeSummary(data["summary"])

	content := getStr(data, "content", "")

	patch := checkpointing.EvolutionPatch{
		Section:        section,
		Action:         "append",
		Content:        content,
		Target:         target,
		MergeTarget:    mergeTarget,
		ScriptFilename: strPtrFromAny(data["script_filename"]),
		ScriptLanguage: strPtrFromAny(data["script_language"]),
		ScriptPurpose:  strPtrFromAny(data["script_purpose"]),
		Keywords:       keywords,
		Summary:        summary,
	}

	return &ParsedExperienceDraft{
		Patch:    patch,
		Summary:  summary,
		Keywords: keywords,
	}
}

// ParseExperienceDraftsWithError 从原始 LLM JSON 文本中批量解析草稿，并返回解析错误信息。
//
// 对齐 Python: parse_experience_drafts_with_error(raw, extract_json_with_error_fn)
//
//	data, last_error = extract_json_with_error_fn(raw)
//	if data is None: return None, last_error
//	items = data if isinstance(data, list) else [data]
//	逐条 ParseExperienceDraft
func ParseExperienceDraftsWithError(raw string, extractFn func(string) (any, string)) ([]ParsedExperienceDraft, string) {
	data, lastError := extractFn(raw)
	if data == nil {
		return nil, lastError
	}

	var items []any
	switch v := data.(type) {
	case []any:
		items = v
	case map[string]any:
		items = []any{v}
	default:
		return nil, "unexpected data type"
	}

	var drafts []ParsedExperienceDraft
	for _, item := range items {
		dict, ok := item.(map[string]any)
		if !ok {
			continue
		}
		draft := ParseExperienceDraft(dict)
		if draft != nil {
			drafts = append(drafts, *draft)
		}
	}
	return drafts, ""
}

// ExtractJSONWithError 健壮 JSON 提取，同时返回最后的解析错误信息。
//
// 对齐 Python: _extract_json_with_error(raw)
//
// 顺序尝试：
//  1. 直接解析 raw
//  2. FixJSONText 后解析
//  3. 正则提取 [\s\S]* 和 \{[\s\S]*\} 后解析
//  4. 正则提取后 FixJSONText 再解析
//  5. 全部失败 → 返回 (nil, last_error)
func ExtractJSONWithError(raw string) (any, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "empty response"
	}

	lastError := "unknown"

	// 步骤 1: 直接解析
	result := tryParse(raw)
	if result != nil {
		return result, ""
	}

	// 步骤 2: FixJSONText 后解析
	fixed := FixJSONText(raw)
	result = tryParse(fixed)
	if result != nil {
		return result, ""
	}

	// 步骤 3: 正则提取 [ ... ] 或 { ... }
	for _, pattern := range []string{"\\[[\\s\\S]*\\]", "\\{[\\s\\S]*\\}"} {
		re := regexp.MustCompile(pattern)
		matched := re.FindString(fixed)
		if matched != "" {
			result = tryParse(matched)
			if result != nil {
				return result, ""
			}
			refixed := FixJSONText(matched)
			var err error
			result, err = tryParseWithError(refixed)
			if err != nil {
				lastError = err.Error()
			}
			if result != nil {
				return result, ""
			}
		}
	}

	return nil, lastError
}

// LooksTruncated 判断 LLM 输出是否看起来被截断了。
//
// 对齐 Python: _looks_truncated(text)
//
//	opens = text.count("{") + text.count("[")
//	closes = text.count("}") + text.count("]")
//	return opens > closes + 1
func LooksTruncated(text string) bool {
	opens := strings.Count(text, "{") + strings.Count(text, "[")
	closes := strings.Count(text, "}") + strings.Count(text, "]")
	return opens > closes+1
}

// FixJSONText 修复 LLM 输出中常见的 JSON 格式错误。
//
// 对齐 Python: _fix_json_text(text)
//
//	去除代码块标记 (```json)
//	去除注释 (//)
//	去除尾逗号
func FixJSONText(text string) string {
	text = strings.TrimSpace(text)
	// 对齐 Python: re.sub(r"^```(?:json)?\s*", "", text, flags=re.MULTILINE)
	text = regexp.MustCompile("^```(?:json)?\\s*").ReplaceAllString(text, "")
	// 对齐 Python: re.sub(r"```\s*$", "", text, flags=re.MULTILINE)
	text = regexp.MustCompile("```\\s*$").ReplaceAllString(text, "")
	// 对齐 Python: re.sub(r"//[^\n]*", "", text)
	text = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(text, "")
	// 对齐 Python: re.sub(r",\s*([}\]])", r"\1", text)
	text = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(text, "$1")
	return strings.TrimSpace(text)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tryParse 尝试 json.Unmarshal，失败返回 nil。
// 对齐 Python: _try_parse(text)
func tryParse(text string) any {
	var result any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil
	}
	return result
}

// tryParseWithError 尝试 json.Unmarshal，失败返回 (nil, error)。
func tryParseWithError(text string) (any, error) {
	var result any
	err := json.Unmarshal([]byte(text), &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// getStr 从 map 中安全获取字符串值，不存在或类型不匹配时返回 defaultVal。
func getStr(data map[string]any, key string, defaultVal string) string {
	if v, ok := data[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return defaultVal
}

// strPtrFromAny 从 any 类型安全提取 *string，nil 或 "null" 时返回 nil。
func strPtrFromAny(v any) *string {
	if v == nil {
		return nil
	}
	s := fmt.Sprintf("%v", v)
	if s == "null" || s == "" {
		return nil
	}
	return &s
}
