package acp

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// acpToolDescriptorConfig descriptor 构造可选项聚合。
type acpToolDescriptorConfig struct {
	ToolCallID string
	Status     string
	RawOutput  any
	Title      string
	Kind       string
}

// AcpToolDescriptorOption descriptor 构造选项。
type AcpToolDescriptorOption func(*acpToolDescriptorConfig)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// toolNameAliases 工具名别名映射（Python _TOOL_NAME_ALIASES）
	toolNameAliases = map[string]string{
		"free_search":   "mcp_free_search",
		"paid_search":   "mcp_paid_search",
		"fetch_webpage": "mcp_fetch_webpage",
		"exec_command":  "mcp_exec_command",
	}
	// listToolAliases 列目录类工具名集合（Python _LIST_TOOL_ALIASES）
	listToolAliases = stringSet("list", "list_dir", "list_directories",
		"list_directory", "list_files", "list_paths", "ls", "glob")
	// searchToolNames 搜索类（Python _SEARCH_TOOL_NAMES）
	searchToolNames = stringSet("grep", "glob", "glob_file_search",
		"mcp_free_search", "mcp_paid_search", "search")
	// readToolNames 读取类（Python _READ_TOOL_NAMES）
	readToolNames = stringSet("list", "list_dir", "list_directories",
		"list_directory", "list_files", "list_paths", "ls", "memory_get",
		"read", "read_file", "read_memory", "read_terminal_output",
		"read_text_file", "view")
	// editToolNames 编辑类（Python _EDIT_TOOL_NAMES）
	editToolNames = stringSet("apply_patch", "edit", "edit_file",
		"search_replace", "write", "write_file", "write_memory",
		"write_text_file")
	// deleteToolNames 删除类（Python _DELETE_TOOL_NAMES）
	deleteToolNames = stringSet("delete", "remove", "remove_file", "rm")
	// moveToolNames 移动类（Python _MOVE_TOOL_NAMES）
	moveToolNames = stringSet("move", "move_file", "rename", "rename_file")
	// executeToolNames 执行类（Python _EXECUTE_TOOL_NAMES）
	executeToolNames = stringSet("bash", "create_terminal", "exec",
		"exec_command", "mcp_exec_command", "release_terminal", "run",
		"shell", "terminal_create", "wait_for_terminal_exit")
	// fetchToolNames 拉取类（Python _FETCH_TOOL_NAMES）
	fetchToolNames = stringSet("fetch", "fetch_webpage", "mcp_fetch_webpage")
	// terminalCreateToolNames 终端创建类（Python _TERMINAL_CREATE_TOOL_NAMES）
	terminalCreateToolNames = stringSet("create_terminal", "terminal_create")
	// terminalWaitExitToolNames 终端等待类（Python _TERMINAL_WAIT_EXIT_TOOL_NAMES）
	terminalWaitExitToolNames = stringSet("wait_for_terminal_exit")
	// pathKeys 路径参数键序（Python _PATH_KEYS，顺序敏感）
	pathKeys = []string{"path", "file_path", "file", "dir_path",
		"target_path", "destination", "source", "cwd", "root"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsReasoningEvent 判定事件是否为推理类。
// Python: is_reasoning_event (acp_tool_updates.py L104-110)
func IsReasoningEvent(eventType any, payload map[string]any) bool {
	eventName, _ := eventType.(string)
	if eventName == "chat.reasoning" {
		return true
	}
	scType, _ := payload["source_chunk_type"].(string)
	peType, _ := payload["event_type"].(string)
	return scType == "llm_reasoning" || peType == "chat.reasoning"
}

// NormalizeToolName 归一化工具名（别名映射）。
// Python: normalize_tool_name (L113-115)
func NormalizeToolName(toolName string) string {
	normalized := strings.TrimSpace(toolName)
	if alias, ok := toolNameAliases[normalized]; ok {
		return alias
	}
	return normalized
}

// WithToolCallID 设置 toolCallId。
func WithToolCallID(id string) AcpToolDescriptorOption {
	return func(c *acpToolDescriptorConfig) { c.ToolCallID = id }
}

// WithStatus 设置状态。
func WithStatus(status string) AcpToolDescriptorOption {
	return func(c *acpToolDescriptorConfig) { c.Status = status }
}

// WithRawOutput 设置原始输出。
func WithRawOutput(rawOutput any) AcpToolDescriptorOption {
	return func(c *acpToolDescriptorConfig) { c.RawOutput = rawOutput }
}

// WithTitle 设置标题。
func WithTitle(title string) AcpToolDescriptorOption {
	return func(c *acpToolDescriptorConfig) { c.Title = title }
}

// WithKind 设置类别。
func WithKind(kind string) AcpToolDescriptorOption {
	return func(c *acpToolDescriptorConfig) { c.Kind = kind }
}

// BuildAcpToolDescriptor 构造 ACP toolCall descriptor。
// Python: build_acp_tool_descriptor (L118-147)
func BuildAcpToolDescriptor(toolName string, arguments any, opts ...AcpToolDescriptorOption) map[string]any {
	cfg := &acpToolDescriptorConfig{}
	for _, o := range opts {
		o(cfg)
	}
	rawInput := normalizeArguments(arguments)
	normalized := NormalizeToolName(toolName)
	resolvedKind := strings.TrimSpace(cfg.Kind)
	if resolvedKind == "" {
		resolvedKind = inferToolKind(normalized)
	}
	if resolvedKind == "" {
		resolvedKind = "other"
	}
	resolvedTitle := strings.TrimSpace(cfg.Title)
	if resolvedTitle == "" {
		resolvedTitle = buildToolTitle(normalized, rawInput)
	}

	descriptor := map[string]any{
		"toolCallId": strings.TrimSpace(cfg.ToolCallID),
		"title":      resolvedTitle,
		"kind":       resolvedKind,
	}
	if cfg.Status != "" {
		descriptor["status"] = cfg.Status
	}
	if len(rawInput) > 0 {
		descriptor["rawInput"] = rawInput
	}
	locations := extractLocations(rawInput)
	if len(locations) > 0 {
		descriptor["locations"] = locations
	}
	if cfg.RawOutput != nil && !isListLikeToolName(normalized) {
		descriptor["rawOutput"] = cfg.RawOutput
	}
	return descriptor
}

// BuildAcpToolCallUpdate 构造 tool_call session/update。
// Python: build_acp_tool_call_update (L150-189)
func BuildAcpToolCallUpdate(payload map[string]any, cache map[string]map[string]any) map[string]any {
	toolCall, _ := payload["tool_call"].(map[string]any)
	if toolCall == nil {
		return nil
	}
	toolCallID := resolveToolCallID(toolCall)
	if toolCallID == "" {
		return nil
	}
	originalName, _ := toolCall["name"].(string)
	arguments := toolCall["arguments"]
	if arguments == nil {
		arguments = map[string]any{}
	}
	status, _ := toolCall["status"].(string)
	if status == "" {
		status, _ = payload["status"].(string)
	}
	if status == "" {
		status = "pending"
	}
	title := firstNonEmptyStr(toolCall["title"], payload["title"])
	kind := firstNonEmptyStr(toolCall["kind"], payload["kind"])
	descriptor := BuildAcpToolDescriptor(originalName, arguments,
		WithToolCallID(toolCallID), WithStatus(status),
		WithTitle(title), WithKind(kind))

	update := map[string]any{
		"sessionUpdate": "tool_call",
		"toolCall": map[string]any{
			"id":        toolCallID,
			"name":      originalName,
			"arguments": legacyArguments(arguments),
		},
	}
	for k, v := range descriptor {
		update[k] = v
	}
	if cache != nil {
		cached := map[string]any{"toolName": originalName}
		for k, v := range descriptor {
			cached[k] = v
		}
		cache[toolCallID] = cached
	}
	return update
}

// BuildAcpToolResultUpdate 构造 tool_call_update session/update。
// Python: build_acp_tool_result_update (L192-263)
func BuildAcpToolResultUpdate(payload map[string]any, cache map[string]map[string]any) map[string]any {
	result := payload["result"]
	if result == nil {
		result = payload["content"]
		if result == nil {
			result = ""
		}
	}
	toolCallID := resolveToolCallID(payload)
	var cached map[string]any
	if cache != nil && toolCallID != "" {
		cached = cache[toolCallID]
		if cached == nil {
			cached = map[string]any{}
		}
	}
	toolName := firstNonEmptyStr(payload["tool_name"], payload["name"])
	if toolName == "" {
		if tc, ok := payload["toolCall"].(map[string]any); ok {
			toolName, _ = tc["name"].(string)
		}
	}
	if toolName == "" {
		toolName, _ = cached["toolName"].(string)
	}
	arguments := firstNonNil(payload["arguments"], payload["rawInput"])
	if arguments == nil || arguments == "" {
		if tc, ok := payload["toolCall"].(map[string]any); ok {
			arguments = tc["arguments"]
		}
	}
	rawOutput := firstNonNil(payload["rawOutput"], payload["raw_output"])
	terminalID := extractTerminalID(rawOutput)

	update := map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    toolCallID,
	}
	if result != nil && result != "" && terminalID == "" {
		update["result"] = result
	}
	if toolName != "" {
		update["toolName"] = toolName
	}
	for _, key := range []string{"title", "kind", "rawInput", "locations"} {
		if v := cached[key]; v != nil && v != "" {
			update[key] = v
		}
	}
	title := firstNonEmptyStr(payload["title"], strAny(update["title"]))
	kind := firstNonEmptyStr(payload["kind"], strAny(update["kind"]))
	descriptor := BuildAcpToolDescriptor(toolName,
		firstNonNil(arguments, update["rawInput"], map[string]any{}),
		WithToolCallID(toolCallID),
		WithStatus(resolveToolResultStatus(payload, result, toolName, rawOutput)),
		WithRawOutput(rawOutput), WithTitle(title), WithKind(kind))
	for _, key := range []string{"title", "kind", "status", "rawOutput"} {
		if v := descriptor[key]; v != nil && v != "" {
			update[key] = v
		}
	}
	content := buildTerminalContentBlocks(terminalID)
	if len(content) == 0 {
		content = buildContentBlocks(result)
	}
	if len(content) > 0 {
		update["content"] = content
	}
	if cache != nil && toolCallID != "" {
		merged := map[string]any{}
		for k, v := range cached {
			merged[k] = v
		}
		for _, key := range []string{"title", "kind", "rawInput", "locations", "rawOutput"} {
			if v := descriptor[key]; v != nil {
				merged[key] = v
			}
		}
		merged["toolName"] = toolName
		if merged["toolName"] == "" {
			merged["toolName"] = strAny(cached["toolName"])
		}
		cache[toolCallID] = merged
	}
	return update
}

// BuildAcpTodoUpdate 构造 todo_update session/update。
// Python: build_acp_todo_update (L266-281)
func BuildAcpTodoUpdate(payload map[string]any) map[string]any {
	todos, ok := payload["todos"].([]any)
	if !ok {
		return nil
	}
	return map[string]any{
		"sessionUpdate": "todo_update",
		"todos":         append([]any{}, todos...),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringSet 从字符串列表构造集合（frozenset 等价）。
func stringSet(items ...string) map[string]struct{} {
	s := make(map[string]struct{}, len(items))
	for _, item := range items {
		s[item] = struct{}{}
	}
	return s
}

// containsSet 集合包含判定。
func containsSet(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// resolveToolCallID 解析工具调用 ID（三键回退）。
// Python: _resolve_tool_call_id (L284-290)
func resolveToolCallID(payload map[string]any) string {
	return strings.TrimSpace(firstNonEmptyStr(
		payload["tool_call_id"], payload["toolCallId"], payload["id"]))
}

// legacyArguments 保留原始参数形态（dict 原样，其余尽量归一为 dict）。
// Python: _legacy_arguments (L293-297)
func legacyArguments(arguments any) any {
	if _, ok := arguments.(map[string]any); ok {
		return arguments
	}
	parsed := normalizeArguments(arguments)
	if len(parsed) > 0 {
		return parsed
	}
	if arguments != nil {
		return arguments
	}
	return map[string]any{}
}

// normalizeArguments 参数归一为 map：JSON 字符串解析（非 dict JSON 转 {input:...}），
// 非 JSON 文本转 {input:...}。
// Python: _normalize_arguments (L300-309)
func normalizeArguments(arguments any) map[string]any {
	switch v := arguments.(type) {
	case map[string]any:
		return v
	case string:
		var parsed map[string]any
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			return parsed
		}
		// JSON 但非 dict（数组/标量）或非 JSON 文本 → {input: 原串}
		if strings.TrimSpace(v) != "" {
			return map[string]any{"input": v}
		}
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

// inferToolKind 推断工具类别。
// Python: _infer_tool_kind (L312-328)
func inferToolKind(toolName string) string {
	normalized := strings.ToLower(NormalizeToolName(toolName))
	switch {
	case containsSet(deleteToolNames, normalized):
		return "delete"
	case containsSet(moveToolNames, normalized):
		return "move"
	case containsSet(editToolNames, normalized):
		return "edit"
	case containsSet(executeToolNames, normalized):
		return "execute"
	case containsSet(fetchToolNames, normalized):
		return "fetch"
	case containsSet(searchToolNames, normalized):
		return "search"
	case containsSet(readToolNames, normalized) || isListLikeToolName(normalized):
		return "read"
	default:
		return "other"
	}
}

// buildToolTitle 构造人类可读标题。
// Python: _build_tool_title (L331-378)——分支链照抄
func buildToolTitle(toolName string, arguments map[string]any) string {
	normalized := strings.ToLower(NormalizeToolName(toolName))
	path := summarizePath(firstStringStrict(arguments, pathKeys...))
	query := firstStringStrict(arguments, "pattern", "query", "term", "text")
	url := firstStringStrict(arguments, "url")
	command := firstStringStrict(arguments, "command", "cmd")
	terminalID := firstStringStrict(arguments, "terminalId", "terminal_id")

	if containsSet(executeToolNames, normalized) {
		switch normalized {
		case "read_terminal_output", "wait_for_terminal_exit", "release_terminal":
			if terminalID != "" {
				verb := map[string]string{
					"read_terminal_output":   "Reading terminal output",
					"wait_for_terminal_exit": "Waiting for terminal exit",
					"release_terminal":       "Releasing terminal",
				}[normalized]
				return verb + " " + terminalID
			}
		}
		if command != "" {
			return "Running " + command
		}
		if path != "" {
			return "Running in " + path
		}
		return "Running command"
	}
	if containsSet(fetchToolNames, normalized) && url != "" {
		return "Fetching " + url
	}
	if containsSet(searchToolNames, normalized) {
		switch {
		case query != "" && path != "":
			return "Searching " + query + " in " + path
		case query != "":
			return "Searching " + query
		case path != "":
			return "Searching in " + path
		default:
			return "Searching"
		}
	}
	if containsSet(editToolNames, normalized) {
		if path != "" {
			return "Editing " + path
		}
		return "Editing files"
	}
	if containsSet(deleteToolNames, normalized) {
		if path != "" {
			return "Deleting " + path
		}
		return "Deleting files"
	}
	if containsSet(moveToolNames, normalized) {
		destination := summarizePath(
			firstStringStrict(arguments, "destination", "dest", "target_path"))
		if path != "" && destination != "" {
			return "Moving " + path + " to " + destination
		}
		if path != "" {
			return "Moving " + path
		}
		return "Moving files"
	}
	if isListLikeToolName(normalized) {
		if path != "" {
			return "Listing " + path
		}
		return "Listing files"
	}
	if containsSet(readToolNames, normalized) {
		if path != "" {
			return "Reading " + path
		}
		return "Reading data"
	}
	return humanizeToolName(toolName)
}

// buildContentBlocks 构造 text content block。
// Python: _build_content_blocks (L381-393)
func buildContentBlocks(result any) []map[string]any {
	text := strings.TrimSpace(strAny(result))
	if text == "" {
		return nil
	}
	return []map[string]any{
		{
			"type": "content",
			"content": map[string]any{
				"type": "text",
				"text": text,
			},
		},
	}
}

// buildTerminalContentBlocks 构造 terminal content block。
// Python: _build_terminal_content_blocks (L396-399)
func buildTerminalContentBlocks(terminalID string) []map[string]any {
	if terminalID == "" {
		return nil
	}
	return []map[string]any{{"type": "terminal", "terminalId": terminalID}}
}

// extractLocations 提取位置列表（_PATH_KEYS 顺序 + paths 前 5 + 去重）。
// Python: _extract_locations (L402-427)
func extractLocations(arguments map[string]any) []map[string]any {
	locations := make([]map[string]any, 0)
	seen := map[[2]any]struct{}{}
	line := resolveLine(arguments)

	for _, key := range pathKeys {
		location := locationFromValue(arguments[key], line)
		if location == nil {
			continue
		}
		marker := [2]any{location["path"], location["line"]}
		if _, dup := seen[marker]; !dup {
			seen[marker] = struct{}{}
			locations = append(locations, location)
		}
	}

	pathsValue, ok := arguments["paths"].([]any)
	if ok {
		for i, item := range pathsValue {
			if i >= 5 {
				break
			}
			location := locationFromValue(item, line)
			if location == nil {
				continue
			}
			marker := [2]any{location["path"], location["line"]}
			if _, dup := seen[marker]; !dup {
				seen[marker] = struct{}{}
				locations = append(locations, location)
			}
		}
	}
	return locations
}

// locationFromValue 构造单个位置（非空字符串 + 可选行号）。
// Python: _location_from_value (L430-436)
func locationFromValue(value any, line int) map[string]any {
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	location := map[string]any{"path": strings.TrimSpace(s)}
	if line > 0 {
		location["line"] = line
	}
	return location
}

// resolveLine 解析行号（line/lineno/line_number，正整数）。
// Python: _resolve_line (L439-444)
func resolveLine(arguments map[string]any) int {
	for _, key := range []string{"line", "lineno", "line_number"} {
		if v, ok := arguments[key].(int); ok && v > 0 {
			return v
		}
		if v, ok := arguments[key].(float64); ok && v > 0 {
			return int(v)
		}
	}
	return 0
}

// firstStringStrict 依序取第一个非空白字符串值。
// Python: _first_string_value (L447-452)
func firstStringStrict(arguments map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := arguments[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// summarizePath 路径摘要（取末 3 段）。
// Python: _summarize_path (L455-465)
func summarizePath(path string) string {
	if path == "" {
		return ""
	}
	// 按两种分隔符切分（PurePath 语义近似），过滤空段与根段
	parts := make([]string, 0)
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if seg != "" && seg != "." {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return path
	}
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, "/")
}

// humanizeToolName 工具名人类可读化。
// Python: _humanize_tool_name (L468-472)
func humanizeToolName(toolName string) string {
	replaced := strings.ReplaceAll(toolName, ".", "_")
	parts := make([]string, 0)
	for _, part := range strings.Split(replaced, "_") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "Using tool"
	}
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

// extractTerminalID 从 rawOutput 提取终端 ID。
// Python: _extract_terminal_id (L475-478)
func extractTerminalID(rawOutput any) string {
	m, ok := rawOutput.(map[string]any)
	if !ok {
		return ""
	}
	return firstStringStrict(m, "terminalId", "terminal_id")
}

// resolveToolResultStatus 推断工具结果状态。
// Python: _resolve_tool_result_status (L481-504)
func resolveToolResultStatus(payload map[string]any, result any, toolName string, rawOutput any) string {
	if status, _ := payload["status"].(string); strings.TrimSpace(status) != "" {
		return strings.TrimSpace(status)
	}
	terminalID := extractTerminalID(rawOutput)
	normalizedToolName := strings.ToLower(strings.TrimSpace(NormalizeToolName(toolName)))
	if terminalID != "" && containsSet(terminalCreateToolNames, normalizedToolName) {
		return "in_progress"
	}
	if m, ok := rawOutput.(map[string]any); ok {
		if timedOut, _ := m["timedOut"].(bool); timedOut &&
			containsSet(terminalWaitExitToolNames, normalizedToolName) {
			return "in_progress"
		}
	}
	if v := payload["error"]; v != nil && v != "" {
		return "failed"
	}
	if result != nil && result != "" {
		return "completed"
	}
	return "in_progress"
}

// isListLikeToolName 判定列目录类工具（Python _is_list_like_tool_name L507-509）。
func isListLikeToolName(toolName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(NormalizeToolName(toolName)))
	return containsSet(listToolAliases, normalized)
}

// firstNonEmptyStr 依序取第一个非空字符串。
func firstNonEmptyStr(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// firstNonNil 依序取第一个非 nil 值。
func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// strAny 安全字符串化（nil→""）。
func strAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
