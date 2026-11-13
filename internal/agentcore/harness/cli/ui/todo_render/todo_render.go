package todo_render

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Todo 复选框渲染，供 CLI 输出。
//
// 带可视化复选框与进度摘要地渲染 todo 项：
//
//	☑ task        — completed（绿）
//	◐ task        — in_progress（黄）
//	☐ task        — pending（暗）
//	✓2 ◐1 ☐3     — 进度摘要
//
// Python: openjiuwen/harness/cli/ui/todo_render.py（392 行）

// ──────────────────────────── 常量 ────────────────────────────

// statusStyle 状态 → (图标, Rich 样式)。
// Python: todo_render.py:15-20 _STATUS_STYLE
var statusStyle = map[string][2]string{
	"completed":   {"☑", "green"},
	"in_progress": {"◐", "yellow"},
	"pending":     {"☐", "dim"},
	"cancelled":   {"☒", "dim strike"},
}

// sdkIconToStatus SDK STATUS_ICONS → 状态名。
// Python: todo_render.py:23-28 _SDK_ICON_TO_STATUS
var sdkIconToStatus = map[string]string{
	"[>]": "in_progress",
	"[ ]": "pending",
	"[√]": "completed",
	"[×]": "cancelled",
}

// 两个解析正则（RE2 语法，与 Python re 等价）。
// Python: todo_render.py:176-178 create_pattern、:199-201 section_pattern
var (
	createPattern  = regexp.MustCompile(`\[([>√× ])\]\s+task_id:\s*\S+\s*,\s*content:\s*(.+)`)
	sectionPattern = regexp.MustCompile(`^\[([>√× ])\]\s+(?:In Progress|Pending|Completed|Cancelled)`)
	itemPattern    = regexp.MustCompile(`^\s+\[[\w-]+\]\s+(.+)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderTodoItem 用复选框渲染单个 todo 项。
// Python: todo_render.py:33-49 render_todo_item
//
// 返回 Rich 格式串，如 [green]☑ task[/green]。
func RenderTodoItem(content, status string) string {
	entry, exists := statusStyle[status]
	if !exists {
		entry = [2]string{"☐", "dim"}
	}
	return "[" + entry[1] + "]" + entry[0] + " " + content + "[/" + entry[1] + "]"
}

// RenderTodoList 把 todo 项列表渲染成复选框行。
// Python: todo_render.py:51-72 render_todo_list
//
// 每行前缀 `  ⎿  `（Claude Code 风格）。content 缺省取 activeForm。
func RenderTodoList(items []map[string]any) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		content := stringField(item, "content")
		if content == "" {
			content = stringField(item, "activeForm")
		}
		status := stringOr(stringField(item, "status"), "pending")
		lines = append(lines, "  ⎿  "+RenderTodoItem(content, status))
	}
	return lines
}

// RenderTodoSummary 渲染紧凑进度摘要。
// Python: todo_render.py:74-105 render_todo_summary
//
// 形如 ✓2 ◐1 ☐3；全部为 0 时返回 "No tasks"。
func RenderTodoSummary(items []map[string]any) string {
	counts := map[string]int{
		"completed": 0, "in_progress": 0, "pending": 0, "cancelled": 0,
	}
	for _, item := range items {
		status := stringOr(stringField(item, "status"), "pending")
		if _, known := counts[status]; known {
			counts[status]++
		}
	}
	var parts []string
	if counts["completed"] > 0 {
		parts = append(parts, "✓"+itoa(counts["completed"]))
	}
	if counts["in_progress"] > 0 {
		parts = append(parts, "◐"+itoa(counts["in_progress"]))
	}
	if counts["pending"] > 0 {
		parts = append(parts, "☐"+itoa(counts["pending"]))
	}
	if len(parts) == 0 {
		return "No tasks"
	}
	return strings.Join(parts, " ")
}

// ParseTodoResult 把 todo 工具结果解析成项列表。
// Python: todo_render.py:232-270 parse_todo_result
//
// 两种策略：1) JSON（结构化数据）；2) SDK 文本格式（[>]/[ ] 图标）。
// 无法解析时返回 nil。
func ParseTodoResult(toolResult string) []map[string]any {
	if toolResult == "" {
		return nil
	}
	// 策略 1：JSON
	var data any
	if json.Unmarshal([]byte(toolResult), &data) == nil {
		if items, isList := data.([]any); isList {
			return normalizeList(items)
		}
		if obj, isMap := data.(map[string]any); isMap {
			for _, key := range []string{"items", "tasks", "todos", "result"} {
				if value, exists := obj[key]; exists {
					if items, isList := value.([]any); isList {
						return normalizeList(items)
					}
				}
			}
			if _, hasContent := obj["content"]; hasContent {
				return []map[string]any{obj}
			}
			if _, hasStatus := obj["status"]; hasStatus {
				return []map[string]any{obj}
			}
		}
	}
	// 策略 2：SDK 文本格式。
	// 解析前清理尾部的 dict repr 残留（如 "'}"）。
	cleaned := strings.TrimRight(toolResult, " \t\n\r")
	for _, suffix := range []string{"'}", `"}`, "}", "'", `"`} {
		if strings.HasSuffix(cleaned, suffix) {
			cleaned = strings.TrimSuffix(cleaned, suffix)
		}
	}
	return parseTodoText(cleaned)
}

// ParseTodoToolArgs 把原始工具参数解析成 map。
// Python: todo_render.py:272-285 parse_todo_tool_args
func ParseTodoToolArgs(toolArgs any) map[string]any {
	if obj, ok := toolArgs.(map[string]any); ok {
		return obj
	}
	if text, ok := toolArgs.(string); ok {
		var data map[string]any
		if json.Unmarshal([]byte(text), &data) == nil {
			return data
		}
	}
	return map[string]any{}
}

// ApplyTodoModifyArgs 把一次 todo_modify 调用套到已渲染的 todo 项上。
// Python: todo_render.py:287-370 apply_todo_modify_args
//
// 让 CLI 在工具结果只有 "Successfully updated" 这类摘要时也能刷新
// 可见的 todo 清单。动作：update / delete / cancel / append /
// insert_after / insert_before；无法应用时返回 nil。
func ApplyTodoModifyArgs(items []map[string]any, toolArgs any) []map[string]any {
	args := ParseTodoToolArgs(toolArgs)
	action := stringField(args, "action")
	if action == "" {
		return nil
	}

	// 深拷贝当前项
	current := make([]map[string]any, len(items))
	for i, item := range items {
		current[i] = cloneMap(item)
	}

	switch action {
	case "update":
		updates, ok := args["todos"].([]any)
		if !ok {
			return nil
		}
		itemMap := map[string]map[string]any{}
		for _, item := range current {
			if id := stringField(item, "id"); id != "" {
				itemMap[id] = item
			}
		}
		if len(itemMap) == 0 {
			return nil
		}
		for _, raw := range updates {
			update, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := stringField(update, "id")
			item, exists := itemMap[id]
			if !exists {
				continue
			}
			for key, value := range update {
				item[key] = value
			}
			syncDisplayContent(item)
		}
		return current

	case "delete":
		ids, ok := args["ids"].([]any)
		if !ok {
			return nil
		}
		deleteSet := map[string]bool{}
		for _, id := range ids {
			if text, isText := id.(string); isText {
				deleteSet[text] = true
			}
		}
		kept := make([]map[string]any, 0, len(current))
		for _, item := range current {
			if !deleteSet[stringField(item, "id")] {
				kept = append(kept, item)
			}
		}
		return kept

	case "cancel":
		ids, ok := args["ids"].([]any)
		if !ok {
			return nil
		}
		cancelSet := map[string]bool{}
		for _, id := range ids {
			if text, isText := id.(string); isText {
				cancelSet[text] = true
			}
		}
		for _, item := range current {
			if cancelSet[stringField(item, "id")] {
				item["status"] = "cancelled"
				syncDisplayContent(item)
			}
		}
		return current

	case "append":
		todos, ok := args["todos"].([]any)
		if !ok {
			return nil
		}
		for _, raw := range todos {
			if todo, isMap := raw.(map[string]any); isMap {
				current = append(current, normalizeTodoItem(todo))
			}
		}
		return current

	case "insert_after", "insert_before":
		todoData, ok := args["todo_data"].([]any)
		if !ok || len(todoData) != 2 {
			return nil
		}
		targetID, isText := todoData[0].(string)
		if !isText {
			return nil
		}
		insertRaw, isList := todoData[1].([]any)
		if !isList {
			return nil
		}
		targetIndex := -1
		for idx, item := range current {
			if stringField(item, "id") == targetID {
				targetIndex = idx
				break
			}
		}
		if targetIndex < 0 {
			return nil
		}
		newItems := make([]map[string]any, 0, len(insertRaw))
		for _, raw := range insertRaw {
			if todo, isMap := raw.(map[string]any); isMap {
				newItems = append(newItems, normalizeTodoItem(todo))
			}
		}
		insertIndex := targetIndex
		if action == "insert_after" {
			insertIndex = targetIndex + 1
		}
		result := make([]map[string]any, 0, len(current)+len(newItems))
		result = append(result, current[:insertIndex]...)
		result = append(result, newItems...)
		result = append(result, current[insertIndex:]...)
		return result
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseTodoText 从 SDK 的人类可读文本格式解析 todo 项。
// Python: todo_render.py:107-230 _parse_todo_text
//
// 两种形态：
//
//	TodoCreateTool:  [>] task_id: uuid , content: description
//	TodoListTool:    [>] In Progress Task 段落头 +  [uuid] description 项
func parseTodoText(toolResult string) []map[string]any {
	var items []map[string]any
	// 归一化：\\n 转真换行（dict 的 str() 产生带字面 \\n 的 Python repr）
	normalized := strings.ReplaceAll(toolResult, "\\n", "\n")

	// 形态 1：TodoCreateTool
	for _, match := range createPattern.FindAllStringSubmatch(normalized, -1) {
		iconKey := "[" + match[1] + "]"
		status := stringOr(sdkIconToStatus[iconKey], "pending")
		items = append(items, map[string]any{
			"content": strings.TrimSpace(match[2]),
			"status":  status,
		})
	}
	if len(items) > 0 {
		return items
	}

	// 形态 2：TodoListTool（段落头 + 段内项）
	currentStatus := "pending"
	for _, line := range strings.Split(normalized, "\n") {
		if sectionMatch := sectionPattern.FindStringSubmatch(line); sectionMatch != nil {
			iconKey := "[" + sectionMatch[1] + "]"
			currentStatus = stringOr(sdkIconToStatus[iconKey], "pending")
			continue
		}
		if itemMatch := itemPattern.FindStringSubmatch(line); itemMatch != nil {
			items = append(items, map[string]any{
				"content": strings.TrimSpace(itemMatch[1]),
				"status":  currentStatus,
			})
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// normalizeTodoItem 归一化新项（同步展示 content）。
// Python: todo_render.py:372-376 _normalize_todo_item
func normalizeTodoItem(item map[string]any) map[string]any {
	normalized := cloneMap(item)
	syncDisplayContent(normalized)
	return normalized
}

// syncDisplayContent 让渲染的 content 与 todo 状态对齐。
// Python: todo_render.py:378-391 _sync_display_content
//
// in_progress 且有 activeForm 时 content 换成 activeForm。
func syncDisplayContent(item map[string]any) {
	status := stringOr(stringField(item, "status"), "pending")
	content := stringField(item, "content")
	activeForm := stringField(item, "activeForm")
	if status == "in_progress" && activeForm != "" {
		item["content"] = activeForm
	} else if content != "" {
		item["content"] = content
	}
}

// stringField 取字符串字段（缺省/类型不符返回空串）。
func stringField(item map[string]any, key string) string {
	if value, ok := item[key].(string); ok {
		return value
	}
	return ""
}

// stringOr 空串回退。
func stringOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// cloneMap 浅拷贝 map（Python dict(item)）。
func cloneMap(item map[string]any) map[string]any {
	cloned := make(map[string]any, len(item))
	for key, value := range item {
		cloned[key] = value
	}
	return cloned
}

// normalizeList 把 []any 转成 []map[string]any（非对象项跳过）。
func normalizeList(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if obj, ok := raw.(map[string]any); ok {
			result = append(result, obj)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// itoa int → string。
func itoa(n int) string {
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = digits[n%10]
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
