package infra

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	autoharness "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/auto_harness"
)

// allowedPRKinds 允许的 PR kind 集合（对齐 _ALLOWED_PR_KINDS）
var allowedPRKinds = map[string]bool{
	"bug": true, "task": true, "feature": true,
	"refactor": true, "clean_code": true,
}

// jsonFenceRe ```json ... ``` 围栏正则
var jsonFenceRe = regexp.MustCompile(`(?s)` + "```json\\s*(.*?)\\s*```")

// jsonFenceOpenRe 围栏开始位置正则（对齐 _decode_json_object_from_markdown 的 fence_match）
var jsonFenceOpenRe = regexp.MustCompile("```(?:json|JSON)?\\s*")

// bareArrayRe 裸 JSON 数组正则
var bareArrayRe = regexp.MustCompile(`(?s)\[.*]`)

// bareObjectRe 裸 JSON 对象正则
var bareObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// bareObjectRe2 裸 JSON 对象正则（parse_pipeline_selection 用的 `\{.*}` 贪婪到首个 `}`）
var bareObjectGreedy1Re = regexp.MustCompile(`(?s)\{.*}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseTasks 从 agent 输出中解析 JSON 任务列表（对齐 parse_tasks）。
//
// 支持 ```json ... ``` 包裹和裸 JSON 数组。
func ParseTasks(raw string) []autoharness.OptimizationTask {
	jsonStr, ok := extractFencedOrArray(raw)
	if !ok {
		return nil
	}
	var items []any
	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		logger.Warn(logComponent).Msg("Failed to parse plan JSON")
		return nil
	}

	var tasks []autoharness.OptimizationTask
	for _, it := range items {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		topic, ok := item["topic"]
		if !ok {
			continue
		}
		tasks = append(tasks, autoharness.OptimizationTask{
			Topic:          jsonStrOf(topic),
			Description:    jsonStrOf(item["description"]),
			Files:          jsonStrSlice(item["files"]),
			ExpectedEffect: jsonStrOf(item["expected_effect"]),
			PipelineName:   autoharness.NormalizePipelineName(jsonStrOf(item["pipeline_name"])),
			// dataclass 默认 status=TaskStatus.PENDING
			Status: autoharness.TaskStatusPending,
		})
	}
	return tasks
}

// ParseLearnings 从 learnings agent 输出中解析 JSON 经验列表（对齐 parse_learnings）。
//
// 返回字典列表，每个包含 type/topic/summary/details。
func ParseLearnings(raw string) []map[string]any {
	jsonStr, ok := extractFencedOrArray(raw)
	if !ok {
		return nil
	}
	var items []any
	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		logger.Warn(logComponent).Msg("Failed to parse learnings JSON")
		return nil
	}
	var out []map[string]any
	for _, it := range items {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := item["topic"]; !ok {
			continue
		}
		out = append(out, item)
	}
	return out
}

// ParsePRDraftWithError 解析 PR 草稿 JSON 响应并给出详细错误
// （对齐 parse_pr_draft_with_error）。
func ParsePRDraftWithError(raw string) (*autoharness.PullRequestDraft, string) {
	item, errStr := decodeJSONObjectFromMarkdown(raw)
	if item == nil {
		item = parsePRDraftFieldsLoose(raw)
	}
	if item == nil {
		if errStr == "" {
			errStr = "JSON 解析失败"
		}
		logger.Warn(logComponent).
			Str("error", errStr).
			Msg("Failed to parse PR draft JSON")
		return nil, errStr
	}

	title := strings.TrimSpace(jsonStrOf(item["title"]))
	body := strings.TrimSpace(jsonStrOf(item["body"]))
	kind := strings.TrimSpace(jsonStrOf(item["kind"]))
	if kind == "" && body != "" {
		kindRe := regexp.MustCompile(`(?m)^/kind\s+([a-z_]+)\s*$`)
		if m := kindRe.FindStringSubmatch(body); m != nil {
			kind = strings.TrimSpace(m[1])
		}
	}
	if title == "" || body == "" {
		return nil, "缺少 title 或 body"
	}
	if !allowedPRKinds[kind] {
		return nil, "kind 必须是 bug/task/feature/refactor/clean_code 之一"
	}
	return &autoharness.PullRequestDraft{Title: title, Body: body, Kind: kind}, ""
}

// ParsePRDraft 解析 PR 草稿 JSON 响应（对齐 parse_pr_draft）
func ParsePRDraft(raw string) *autoharness.PullRequestDraft {
	draft, _ := ParsePRDraftWithError(raw)
	return draft
}

// ParsePipelineSelection 解析 selector agent JSON 响应（对齐 parse_pipeline_selection）
func ParsePipelineSelection(raw string) *autoharness.PipelineSelectionArtifact {
	jsonStr, ok := extractFencedOrObjectGreedy1(raw)
	if !ok {
		return nil
	}
	var item map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &item); err != nil {
		logger.Warn(logComponent).Msg("Failed to parse pipeline selection JSON")
		return nil
	}
	if item == nil {
		return nil
	}
	pipelineName := autoharness.NormalizePipelineName(strings.TrimSpace(jsonStrOf(item["pipeline_name"])))
	if pipelineName == "" {
		return nil
	}
	alternatives := itemList(item["alternatives"])
	requiredInputs := itemList(item["required_inputs"])
	confidence := 0.0
	if v, ok := toAnyFloat(item["confidence"]); ok {
		confidence = v
	}
	altNames := make([]string, 0, len(alternatives))
	for _, v := range alternatives {
		altNames = append(altNames, autoharness.NormalizePipelineName(jsonStrOf(v)))
	}
	inputNames := make([]string, 0, len(requiredInputs))
	for _, v := range requiredInputs {
		inputNames = append(inputNames, jsonStrOf(v))
	}
	return &autoharness.PipelineSelectionArtifact{
		PipelineName:   pipelineName,
		Reason:         jsonStrOf(item["reason"]),
		Alternatives:   altNames,
		Confidence:     confidence,
		RiskLevel:      jsonStrOf(item["risk_level"]),
		RequiredInputs: inputNames,
		FallbackPipeline: autoharness.NormalizePipelineName(
			jsonStrOf(item["fallback_pipeline"])),
	}
}

// ExtractText 从 OutputSchema chunk 中提取文本内容（对齐 extract_text）。
//
// chunk 为 map 形态（Go 侧 OutputSchema 惯例）；
// 返回提取的文本，无内容时返回空字符串。
func ExtractText(chunk map[string]any) string {
	payload, ok := chunk["payload"]
	if !ok {
		return ""
	}
	switch p := payload.(type) {
	case map[string]any:
		content := jsonStrOf(p["content"])
		if content == "" {
			content = jsonStrOf(p["output"])
		}
		return content
	case string:
		return p
	default:
		return ""
	}
}

// ParseGaps 将结构化文本解析为 Gap 列表（对齐 parse_gaps）。
//
// 接受 markdown 表格，每行 8 列：
// competitor | feature | current_state | gap_description |
// impact | feasibility | suggested_approach | target_files。
// 返回按 priority 降序的 Gap。
func ParseGaps(rawText string) []autoharness.Gap {
	var gaps []autoharness.Gap
	lines := strings.Split(strings.TrimSpace(rawText), "\n")

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "|--") {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		// 去掉首尾（split 后 [0] 和 [len-1] 是边界空串）
		if len(parts) < 10 {
			continue
		}
		cells := make([]string, 0, len(parts)-2)
		for _, p := range parts[1 : len(parts)-1] {
			cells = append(cells, strings.TrimSpace(p))
		}
		if len(cells) < 8 {
			continue
		}
		// 跳过表头行
		lower := strings.ToLower(cells[0])
		if lower == "competitor" || cells[0] == "竞品" {
			continue
		}
		if gap, ok := rowToGap(cells); ok {
			gaps = append(gaps, gap)
		}
	}

	sort.SliceStable(gaps, func(i, j int) bool {
		return gaps[i].Priority() > gaps[j].Priority()
	})
	logger.Info(logComponent).
		Int("count", len(gaps)).
		Msg("Parsed gaps from raw text")
	return gaps
}

// ParseExtensionDesigns 从 agent 输出中解析 ExtensionDesign JSON 列表
// （对齐 parse_extension_designs）。
//
// 支持两种格式（向后兼容）：
//   - 新格式：{"package_name": "...", "designs": [...]}
//   - 旧格式：[{"gap_id": ...}, ...] 或单个 design 对象
//
// 返回 (package_name, designs)；旧格式 package_name 为空串。
func ParseExtensionDesigns(raw string) (string, []autoharness.ExtensionDesign) {
	jsonStr, ok := extractFencedOrObjectOrArray(raw)
	if !ok {
		return "", nil
	}
	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		logger.Warn(logComponent).Msg("Failed to parse extension designs JSON")
		return "", nil
	}

	// 判断格式类型
	packageName := ""
	var items []any
	switch v := data.(type) {
	case map[string]any:
		if _, hasDesigns := v["designs"]; hasDesigns {
			// 新格式：顶层对象包含 package_name 和 designs
			if s, ok := v["package_name"].(string); ok && s != "" {
				packageName = strings.TrimSpace(s)
			}
			items = itemList(v["designs"])
		} else {
			// 兼容：单个 design 对象（无 package_name）
			items = []any{v}
		}
	case []any:
		// 旧格式：裸数组
		items = v
	default:
		return "", nil
	}

	var designs []autoharness.ExtensionDesign
	for _, it := range items {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		extensionName := strings.TrimSpace(jsonStrOf(item["extension_name"]))
		if extensionName == "" {
			continue
		}
		kind := strings.TrimSpace(jsonStrOf(item["kind"]))
		if kind == "" {
			kind = "capability"
		}
		if kind != "capability" && kind != "constraint" {
			kind = "capability"
		}
		dependsOn := jsonStrSliceOrEmpty(item["depends_on"])
		appliesTo := jsonStrSliceOrEmpty(item["applies_to"])
		designs = append(designs, autoharness.ExtensionDesign{
			GapID:              jsonStrOf(item["gap_id"]),
			ExtensionName:      extensionName,
			Kind:               kind,
			DependsOn:          dependsOn,
			AppliesTo:          appliesTo,
			Components:         jsonStrSlice(item["components"]),
			FilePlan:           strMap(item["file_plan"]),
			HarnessConfigPatch: anyMap(item["harness_config_patch"]),
			SkillSource:        jsonStrOf(item["skill_source"]),
		})
	}
	return packageName, designs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rawDecodeJSONObject 解码 start 之后第一个 JSON 对象
// （对齐 _raw_decode_json_object；刻意避免正则围栏截断——PR 正文可能
// 含 Markdown 围栏，非贪婪 regex 会在 JSON 字符串内截断）。
func rawDecodeJSONObject(text string, start int) (map[string]any, string) {
	index := start
	var lastError error
	for index < len(text) {
		braceIndex := strings.Index(text[index:], "{")
		if braceIndex < 0 {
			if lastError != nil {
				return nil, fmt.Sprintf("JSON 解析失败: %v", lastError)
			}
			return nil, "未找到 JSON 对象"
		}
		braceIndex += index
		// decoder.raw_decode 等价：从 braceIndex 解码第一个 JSON 值
		dec := json.NewDecoder(strings.NewReader(text[braceIndex:]))
		var item any
		err := dec.Decode(&item)
		if err != nil {
			lastError = err
			index = braceIndex + 1
			continue
		}
		if obj, ok := item.(map[string]any); ok {
			return obj, ""
		}
		index = braceIndex + 1
	}
	if lastError != nil {
		return nil, fmt.Sprintf("JSON 解析失败: %v", lastError)
	}
	return nil, "未找到 JSON 对象"
}

// decodeJSONObjectFromMarkdown 从围栏 Markdown 或上下文中解码 JSON 对象
// （对齐 _decode_json_object_from_markdown）
func decodeJSONObjectFromMarkdown(raw string) (map[string]any, string) {
	if loc := jsonFenceOpenRe.FindStringIndex(raw); loc != nil {
		item, _ := rawDecodeJSONObject(raw, loc[1])
		if item != nil {
			return item, ""
		}
		// 回落全文扫描：模型可能在围栏内先写说明文字，或正文嵌套围栏
	}
	return rawDecodeJSONObject(raw, 0)
}

// unescapeLooseJSONString 对"近似 JSON"的模型文本尽力反转义
// （对齐 _unescape_loose_json_string）
func unescapeLooseJSONString(value string) string {
	value = strings.ReplaceAll(value, "\\r\\n", "\n")
	value = strings.ReplaceAll(value, "\\n", "\n")
	value = strings.ReplaceAll(value, "\\t", "\t")
	value = strings.ReplaceAll(value, "\\\"", "\"")
	value = strings.ReplaceAll(value, "\\\\", "\\")
	return value
}

// parsePRDraftFieldsLoose 从畸形 JSON-like 文本中提取 PR 草稿字段
// （对齐 _parse_pr_draft_fields_loose；PR 创建只需要 title/kind/body）
func parsePRDraftFieldsLoose(raw string) map[string]any {
	titleRe := regexp.MustCompile(`(?s)"title"\s*:\s*"((?:\\.|[^"\\])*)"`)
	titleMatch := titleRe.FindStringSubmatch(raw)
	kindRe := regexp.MustCompile(`(?s)"kind"\s*:\s*"([a-z_]+)"`)
	kindMatch := kindRe.FindStringSubmatch(raw)
	bodyStartRe := regexp.MustCompile(`(?s)"body"\s*:\s*"`)
	bodyStart := bodyStartRe.FindStringIndex(raw)
	if titleMatch == nil || bodyStart == nil {
		return nil
	}
	bodyText := raw[bodyStart[1]:]
	bodyEndRe := regexp.MustCompile(`(?s)"\s*\}\s*(?:` + "```" + `)?`)
	bodyEnd := bodyEndRe.FindStringIndex(bodyText)
	if bodyEnd == nil {
		return nil
	}
	body := bodyText[:bodyEnd[0]]
	return map[string]any{
		"title": strings.TrimSpace(unescapeLooseJSONString(titleMatch[1])),
		"kind": func() string {
			if kindMatch != nil {
				return strings.TrimSpace(kindMatch[1])
			}
			return ""
		}(),
		"body": strings.TrimSpace(unescapeLooseJSONString(body)),
	}
}

// rowToGap 表格行（8 列）转 Gap（对齐 _row_to_gap；失败返回 false）
func rowToGap(cells []string) (autoharness.Gap, bool) {
	impact, errI := parsePythonFloat(cells[4])
	feasibility, errF := parsePythonFloat(cells[5])
	if errI != nil || errF != nil {
		logger.Warn(logComponent).
			Str("row", strings.Join(cells[:4], " | ")).
			Msg("Skipping malformed gap row")
		return autoharness.Gap{}, false
	}
	var targetFiles []string
	for _, f := range strings.Split(cells[7], ",") {
		if s := strings.TrimSpace(f); s != "" {
			targetFiles = append(targetFiles, s)
		}
	}
	return autoharness.Gap{
		ID:                shortID8(),
		Competitor:        cells[0],
		Feature:           cells[1],
		CurrentState:      cells[2],
		GapDescription:    cells[3],
		Impact:            impact,
		Feasibility:       feasibility,
		SuggestedApproach: cells[6],
		TargetFiles:       targetFiles,
	}, true
}

// extractFencedOrArray 围栏或裸数组提取（parse_tasks/parse_learnings 共用）
func extractFencedOrArray(raw string) (string, bool) {
	if m := jsonFenceRe.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	if m := bareArrayRe.FindString(raw); m != "" {
		return m, true
	}
	return "", false
}

// extractFencedOrObjectGreedy1 围栏或裸对象提取（parse_pipeline_selection 用）
func extractFencedOrObjectGreedy1(raw string) (string, bool) {
	if m := jsonFenceRe.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	if m := bareObjectGreedy1Re.FindString(raw); m != "" {
		return m, true
	}
	return "", false
}

// extractFencedOrObjectOrArray 围栏/对象/数组提取（parse_extension_designs 用）
func extractFencedOrObjectOrArray(raw string) (string, bool) {
	if m := jsonFenceRe.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	if m := bareObjectRe.FindString(raw); m != "" {
		return m, true
	}
	if m := bareArrayRe.FindString(raw); m != "" {
		return m, true
	}
	return "", false
}

// jsonStrOf JSON 值转字符串（对齐 str(item.get(key, ""))）
func jsonStrOf(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case bool:
		if s {
			return "true"
		}
		return "false"
	case float64:
		if s == float64(int64(s)) {
			return fmt.Sprintf("%d", int64(s))
		}
		return fmt.Sprintf("%v", s)
	default:
		return fmt.Sprintf("%v", s)
	}
}

// jsonStrSlice JSON 数组转字符串切片（对齐 item.get("files", [])）
func jsonStrSlice(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, jsonStrOf(it))
	}
	return out
}

// itemList JSON 数组转 any 切片；非数组返回空
func itemList(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
}

// strMap JSON 对象转 map[string]string
func strMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, val := range m {
		out[k] = jsonStrOf(val)
	}
	return out
}

// anyMap JSON 对象转 map[string]any
func anyMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// toAnyFloat 尽力转 float（对齐 float(...) 的 try/except）
func toAnyFloat(v any) (float64, bool) {
	switch s := v.(type) {
	case float64:
		return s, true
	case int:
		return float64(s), true
	case string:
		f, err := parsePythonFloat(s)
		return f, err == nil
	default:
		return 0, false
	}
}

// parsePythonFloat 解析 Python float() 语义的字符串
func parsePythonFloat(s string) (float64, error) {
	var f float64
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty float")
	}
	_, err := fmt.Sscanf(trimmed, "%g", &f)
	return f, err
}

// shortID8 8 位随机 hex（对齐 uuid.uuid4().hex[:8]）
func shortID8() string {
	return autoharness.NewShortID(8)
}

// jsonStrSliceOrEmpty JSON 数组转字符串切片（空值/非数组返回空切片）
func jsonStrSliceOrEmpty(v any) []string {
	if out := jsonStrSlice(v); out != nil {
		return out
	}
	return []string{}
}
