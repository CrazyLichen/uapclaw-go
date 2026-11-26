package reinjection

// 回注内容构建器：plan / skill / file / task_status / todo / plan_mode / tool_result_hint。
// Python: openjiuwen/core/context_engine/processor/forked/compressor/reinjection/builders.py

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	ceprocessor "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/support"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReadFileSnapshot read_file 结果快照
// Python: builders.py:21 (ReadFileSnapshot)
type ReadFileSnapshot struct {
	// FilePath 文件路径
	FilePath string
	// Content 文件内容
	Content string
	// LineCount 行数（未知为 nil）
	LineCount *int
	// Partial 是否只读入了部分内容
	Partial bool
}

// ──────────────────────────── 常量 ────────────────────────────

// TeamToolCallNames 团队协作工具名集合
// Python: builders.py:129 (TEAM_TOOL_CALL_NAMES)
var TeamToolCallNames = []string{"send_message", "view_task", "claim_task", "member_complete_task"}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildPlanReinjectedContent 回注当前 plan 文件内容
// Python: builders.py:29 (build_plan_reinjected_content)
func BuildPlanReinjectedContent(ctx *ReinjectContext) any {
	planMode := getPlanMode(ctx.SessionState)
	var planSlug string
	if m, ok := planMode.(map[string]any); ok {
		if s, ok := m["plan_slug"].(string); ok {
			planSlug = s
		}
	}
	if planSlug == "" {
		return ""
	}
	planPath := resolvePlanFilePath(ctx, planSlug)
	if planPath == "" {
		return ""
	}
	planText, err := os.ReadFile(planPath)
	if err != nil {
		return ""
	}
	return ctx.Truncate("Current plan file: " + planPath + "\n\n" + string(planText))
}

// resolvePlanFilePath 解析 plan 文件路径：workspace/.plans/{slug}.md 优先，
// 其次 enter_plan_mode 工具结果中登记的路径。
// Python: builders.py:44 (_resolve_plan_file_path)
func resolvePlanFilePath(ctx *ReinjectContext, planSlug string) string {
	var candidatePaths []string
	if ctx.WorkspaceRoot != "" {
		candidatePaths = append(candidatePaths, filepath.Join(ctx.WorkspaceRoot, ".plans", planSlug+".md"))
	}
	candidatePaths = append(candidatePaths, iterEnterPlanModePaths(ctx.SourceMessages, planSlug)...)
	for _, path := range candidatePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// iterEnterPlanModePaths 从 enter_plan_mode 工具调用结果中收集 plan 路径
// Python: builders.py:56 (_iter_enter_plan_mode_paths)
func iterEnterPlanModePaths(messages []llm_schema.BaseMessage, planSlug string) []string {
	var paths []string
	for _, toolCall := range IterToolCalls(messages, "enter_plan_mode") {
		resultText := findToolResultText(messages, toolCall.ID)
		re := regexp.MustCompile(`Plan file created at:\s*(?P<path>.+?\.md)(?:\r?\n|$)`)
		m := re.FindStringSubmatch(resultText)
		if m == nil {
			continue
		}
		planPath := strings.TrimSpace(m[1])
		if strings.TrimSuffix(filepath.Base(planPath), filepath.Ext(planPath)) == planSlug {
			paths = append(paths, planPath)
		}
	}
	return paths
}

// BuildSkillReinjectedContent 回注最近读取过的技能内容（多消息形态）
// Python: builders.py:67 (build_skill_reinjected_content)
func BuildSkillReinjectedContent(ctx *ReinjectContext) any {
	keepSignatures := map[string]bool{}
	for _, message := range ctx.MessagesToKeep {
		keepSignatures[messageSignature(message)] = true
	}
	var selectedRounds [][]llm_schema.BaseMessage
	seenRoundSignatures := map[string]bool{}
	maxSkills := configInt(ctx.Config, "reinject_recent_skills", 3)
	if maxSkills <= 0 {
		return []llm_schema.BaseMessage{}
	}

	rounds := groupCompletedAPIRounds(ctx.SourceMessages)
	for i := len(rounds) - 1; i >= 0; i-- {
		roundMessages := rounds[i]
		sigParts := make([]string, len(roundMessages))
		for j, message := range roundMessages {
			sigParts[j] = messageSignature(message)
		}
		roundSignatures := strings.Join(sigParts, "\x00")
		if seenRoundSignatures[roundSignatures] {
			continue
		}
		retained := false
		for _, sig := range sigParts {
			if keepSignatures[sig] {
				retained = true
				break
			}
		}
		if retained {
			continue
		}
		snapshot := ExtractSkillToolSnapshot(roundMessages)
		if snapshot == "" {
			snapshot = ExtractReadFileSkillSnapshot(roundMessages)
		}
		if snapshot == "" {
			continue
		}
		selectedRounds = append(selectedRounds, []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(ctx.StateMarker + "\n[SKILLS]\n" + ctx.Truncate(snapshot)),
		})
		seenRoundSignatures[roundSignatures] = true
		if len(selectedRounds) >= maxSkills {
			break
		}
	}

	// 逆序收集后反转为时间序
	for i, j := 0, len(selectedRounds)-1; i < j; i, j = i+1, j-1 {
		selectedRounds[i], selectedRounds[j] = selectedRounds[j], selectedRounds[i]
	}
	var messages []llm_schema.BaseMessage
	for _, round := range selectedRounds {
		messages = append(messages, round...)
	}
	return messages
}

// BuildFileReinjectedContent 回注最近读取过的文件内容
// Python: builders.py:93 (build_file_reinjected_content)
func BuildFileReinjectedContent(ctx *ReinjectContext) any {
	preservedPaths := collectReadFilePaths(ctx.MessagesToKeep)
	maxFiles := configInt(ctx.Config, "reinject_read_file_max_files", 5)
	perFileChars := configInt(ctx.Config, "reinject_read_file_max_chars_per_file", 15000)
	totalChars := configInt(ctx.Config, "reinject_read_file_total_chars", 50000)
	if maxFiles <= 0 || totalChars <= 0 {
		return ""
	}

	var snapshots []ReadFileSnapshot
	rounds := groupCompletedAPIRounds(ctx.SourceMessages)
	for i := len(rounds) - 1; i >= 0; i-- {
		roundMessages := rounds[i]
		for _, toolCall := range IterToolCalls(roundMessages, "read_file") {
			resultText := findToolResultText(roundMessages, toolCall.ID)
			snapshot := parseReadFileResult(toolCall, resultText)
			if snapshot == nil {
				continue
			}
			if _, ok := preservedPaths[snapshot.FilePath]; ok {
				continue
			}
			if isExcludedFromReinject(snapshot.FilePath) {
				continue
			}
			snapshots = append(snapshots, *snapshot)
		}
	}

	var selected []ReadFileSnapshot
	seenPaths := map[string]bool{}
	remaining := totalChars
	for _, snapshot := range snapshots {
		if seenPaths[snapshot.FilePath] {
			continue
		}
		seenPaths[snapshot.FilePath] = true
		selected = append(selected, snapshot)
		contentLen := len(snapshot.Content)
		if contentLen > perFileChars {
			contentLen = perFileChars
		}
		remaining -= contentLen
		if len(selected) >= maxFiles || remaining <= 0 {
			break
		}
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return renderReadFileSnapshots(selected, ctx.Truncate, perFileChars)
}

// BuildTaskStatusReinjectedContent 回注任务状态（团队态/普通态分派）
// Python: builders.py:132 (build_task_status_reinjected_content)
func BuildTaskStatusReinjectedContent(ctx *ReinjectContext) any {
	var lines []string
	teamStatus, ok := ctx.SessionState["team_task_status"].(map[string]any)
	if ok {
		lines = append(lines, renderTeamToolCallsSection(ctx, teamStatus)...)
	} else {
		// 普通模式：回注会话状态中的后台（子 agent）任务状态
		lines = append(lines, renderBackgroundTasksSection(ctx)...)
	}
	return ctx.Truncate(strings.Join(lines, "\n"))
}

// renderBackgroundTasksSection 渲染后台任务状态段
// Python: builders.py:145 (_render_background_tasks_section)
func renderBackgroundTasksSection(ctx *ReinjectContext) []string {
	var lines []string
	backgroundTasks, _ := ctx.SessionState["background_tasks"].([]any)
	for _, item := range backgroundTasks {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		description := anyToStrOr(task["description"], "background task")
		taskID := anyToStrOr(task["task_id"], "unknown")
		status := anyToStrOr(task["status"], "unknown")
		switch status {
		case "running":
			lines = append(lines, `Background agent "`+description+`" (`+taskID+`) is still running. Do NOT spawn a duplicate.`)
		case "completed", "error", "canceled":
			lines = append(lines, `Background agent "`+description+`" (`+taskID+`) status=`+status+`. Check the stored result/error before spawning another task.`)
		}
	}
	return lines
}

// renderTeamToolCallsSection 团队模式：静态看板 + 恢复的协作调用，合并一个 section
// Python: builders.py:167 (_render_team_tool_calls_section)
func renderTeamToolCallsSection(ctx *ReinjectContext, teamStatus map[string]any) []string {
	if !configBool(ctx.Config, "reinject_team_tool_calls", true) {
		return []string{}
	}

	keepSignatures := map[string]bool{}
	for _, message := range ctx.MessagesToKeep {
		keepSignatures[messageSignature(message)] = true
	}
	maxCalls := configInt(ctx.Config, "reinject_team_tool_call_max", 20)

	teamName := anyToStrOr(teamStatus["team_name"], "unknown")
	lines := []string{"Team collaboration state:", "- Team: " + teamName}

	lines = append(lines, renderTeamStaticSnapshot(teamStatus)...)

	collected := collectTeamToolCalls(ctx.SourceMessages, keepSignatures, maxCalls)
	if len(collected) > 0 {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "Recovered collaboration history:")
		for _, entry := range collected {
			rendered := renderTeamToolCallLine(entry.ToolCall, entry.ResultText)
			if rendered != "" {
				lines = append(lines, rendered)
			}
		}
	}

	if len(lines) == 2 {
		// 只有标题与团队名——看板为空且无可恢复调用，宁缺毋滥
		return []string{}
	}
	return lines
}

// renderTeamStaticSnapshot 渲染当前团队看板
// Python: builders.py:217 (_render_team_static_snapshot)
func renderTeamStaticSnapshot(teamStatus map[string]any) []string {
	var lines []string
	members, _ := teamStatus["members"].([]any)
	if len(members) > 0 {
		lines = append(lines, "", "Current members:")
		for _, item := range members {
			member, ok := item.(map[string]any)
			if !ok {
				continue
			}
			lines = append(lines, "- "+anyToStrOr(member["member_name"], "unknown")+
				": role="+anyToStrOr(member["role"], "")+
				", status="+anyToStrOr(member["status"], "unknown"))
		}
	}
	openTasks, _ := teamStatus["open_tasks"].([]any)
	if len(openTasks) > 0 {
		lines = append(lines, "", "Open tasks:")
		for _, item := range openTasks {
			task, ok := item.(map[string]any)
			if !ok {
				continue
			}
			assignee := anyToStrOr(task["assignee"], "unassigned")
			lines = append(lines, "- #"+anyToStrOr(task["task_id"], "unknown")+
				" ["+anyToStrOr(task["status"], "unknown")+"] "+
				anyToStrOr(task["title"], "")+" (assignee: "+assignee+")")
		}
	}
	if hasUnread, ok := teamStatus["has_unread_messages"].(bool); ok && hasUnread {
		lines = append(lines, "", "Team signals:", "- Unread team messages exist.")
	}
	return lines
}

// teamToolCallEntry 收集到的团队工具调用三元组
// Python: builders.py:248 返回的 (tool_call, owner_message, result_text)
type teamToolCallEntry struct {
	ToolCall   *llm_schema.ToolCall
	OwnerMsg   *llm_schema.AssistantMessage
	ResultText string
}

// collectTeamToolCalls 按序收集未保留在窗口中的团队工具调用
// Python: builders.py:244 (_collect_team_tool_calls)
func collectTeamToolCalls(messages []llm_schema.BaseMessage, keepSignatures map[string]bool, maxCalls int) []teamToolCallEntry {
	var collected []teamToolCallEntry
	if maxCalls <= 0 {
		return collected
	}
	for _, message := range messages {
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		if keepSignatures[messageSignature(assistant)] {
			continue
		}
		for _, toolCall := range assistant.ToolCalls {
			if !containsStr(TeamToolCallNames, toolCall.Name) {
				continue
			}
			resultText := findToolResultText(messages, toolCall.ID)
			collected = append(collected, teamToolCallEntry{ToolCall: toolCall, OwnerMsg: assistant, ResultText: resultText})
			if len(collected) >= maxCalls {
				return collected
			}
		}
	}
	return collected
}

// renderTeamToolCallLine 渲染单条团队工具调用（含单行压缩结果预览）
// Python: builders.py:275 (_render_team_tool_call_line)
func renderTeamToolCallLine(toolCall *llm_schema.ToolCall, resultText string) string {
	args := parseToolArguments(toolCall.Arguments)
	actionDesc := describeTeamToolAction(toolCall.Name, args)
	line := strings.TrimRight("- "+actionDesc+" ["+toolCall.Name+"]", " ")

	resultText = strings.TrimSpace(resultText)
	if resultText != "" {
		// 多行结果压缩为单行预览，保持 section 可扫读
		var parts []string
		for _, part := range strings.Split(resultText, "\n") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		compacted := strings.Join(parts, " ")
		if len(compacted) > 200 {
			compacted = compacted[:200] + "..."
		}
		line += "\n  Result: " + compacted
	}
	return line
}

// describeTeamToolAction 团队工具调用的简明英文描述
// Python: builders.py:294 (_describe_team_tool_action)
func describeTeamToolAction(toolName string, args map[string]any) string {
	switch toolName {
	case "send_message":
		toRaw := args["to"]
		var base string
		if list, ok := toRaw.([]any); ok {
			var targets []string
			for _, item := range list {
				if s := fmtStr(item); s != "" {
					targets = append(targets, s)
				}
			}
			if len(targets) > 0 {
				base = "Sent message to " + strings.Join(targets, ", ")
			} else {
				base = "Sent message"
			}
		} else if s, ok := toRaw.(string); ok && s == "*" {
			base = "Broadcast message to the team"
		} else if s := fmtStr(toRaw); s != "" {
			base = "Sent message to " + s
		} else {
			base = "Sent message"
		}
		summary := strings.TrimSpace(fmtStr(args["summary"]))
		if summary == "" {
			content := fmtStr(args["content"])
			if len(content) > 80 {
				summary = content[:80] + "..."
			} else {
				summary = content
			}
		}
		if summary != "" {
			return base + ": " + summary
		}
		return base

	case "view_task":
		action := fmtStr(args["action"])
		if action == "" {
			action = "list"
		}
		if action == "get" {
			taskID := fmtStr(args["task_id"])
			if taskID != "" {
				return "Viewed task #" + taskID
			}
			return "Viewed task details"
		}
		if action == "claimable" {
			return "Listed claimable tasks"
		}
		status := fmtStr(args["status"])
		if status != "" {
			return "Listed tasks with status=" + status
		}
		return "Listed tasks"

	case "claim_task":
		taskID := fmtStr(args["task_id"])
		if taskID == "" {
			taskID = "?"
		}
		status := fmtStr(args["status"])
		switch status {
		case "completed":
			return "Marked task #" + taskID + " as completed"
		case "claimed":
			return "Claimed task #" + taskID
		}
		if status == "" {
			status = "?"
		}
		return "Updated task #" + taskID + " with status=" + status

	case "member_complete_task":
		taskID := fmtStr(args["task_id"])
		if taskID == "" {
			taskID = "?"
		}
		note := strings.TrimSpace(fmtStr(args["note"]))
		if len(note) > 80 {
			note = note[:80] + "..."
		}
		if note != "" {
			return "Completed own task #" + taskID + ": " + note
		}
		return "Completed own task #" + taskID
	}
	return "Called " + toolName
}

// BuildToolResultHintReinjectedContent 预留空实现
// Python: builders.py:343 (build_tool_result_hint_reinjected_content)
func BuildToolResultHintReinjectedContent(ctx *ReinjectContext) any {
	_ = ctx
	return ""
}

// BuildTodoReinjectedContent 回注活跃 todo 清单
// Python: builders.py:348 (build_todo_reinjected_content)
func BuildTodoReinjectedContent(ctx *ReinjectContext) any {
	todos := readTodoFile(ctx)
	if len(todos) == 0 {
		return ""
	}

	activeStatuses := map[string]bool{"pending": true, "in_progress": true}
	counts := map[string]int{}
	var active []map[string]any
	for _, todo := range todos {
		status := anyToStrOr(todo["status"], "pending")
		counts[status]++
		if activeStatuses[status] {
			active = append(active, todo)
		}
	}
	if len(active) == 0 {
		return ""
	}

	lines := []string{"Active todos:"}
	for _, todo := range active {
		todoID := anyToStrOr(todo["id"], "unknown")
		status := anyToStrOr(todo["status"], "pending")
		content := strings.TrimSpace(anyToStrPreferring(todo, "content", "description"))
		if content == "" {
			content = "(no content)"
		}
		activeForm := strings.TrimSpace(fmtStr(todo["activeForm"]))
		suffix := ""
		if activeForm != "" && activeForm != content {
			suffix = " (" + activeForm + ")"
		}
		lines = append(lines, "- ["+status+"] "+todoID+": "+content+suffix)
	}

	// 状态计数按状态名排序（PY sorted(counts.items())）
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	var countParts []string
	for _, status := range statuses {
		countParts = append(countParts, status+": "+itoaStr(counts[status]))
	}
	summary := strings.Join(countParts, ", ")
	if summary != "" {
		lines = append(lines, "Status counts: "+summary)
	}
	return ctx.Truncate(strings.Join(lines, "\n"))
}

// BuildPlanModeReinjectedContent 回注 plan 模式状态与约束
// Python: builders.py:382 (build_plan_mode_reinjected_content)
func BuildPlanModeReinjectedContent(ctx *ReinjectContext) any {
	planMode, ok := getPlanMode(ctx.SessionState).(map[string]any)
	if !ok {
		return ""
	}

	mode := anyToStrOr(planMode["mode"], "normal")
	prePlanMode := fmtStr(planMode["pre_plan_mode"])
	planSlug := fmtStr(planMode["plan_slug"])
	if mode == "normal" && planSlug == "" && (prePlanMode == "" || prePlanMode == "normal") {
		return ""
	}

	lines := []string{
		"Current plan-mode status for this session:",
		"- Active mode: " + mode + ".",
	}
	if prePlanMode != "" {
		lines = append(lines, "- Previous mode before entering plan mode: "+prePlanMode+".")
	}
	if planSlug != "" {
		lines = append(lines, "- Active plan identifier: "+planSlug+".")
	}
	if mode == "plan" {
		lines = append(lines,
			"",
			"Plan-mode constraints:",
			"- Only planning is allowed; do not implement the plan yet.",
			"- Do not modify files except the active plan file.",
			"- Use read-only exploration tools unless editing the plan file.",
			"- End planning through exit_plan_mode when the plan is ready for user approval.",
			"- Use ask_user only for clarification or choosing between approaches, not for plan approval.",
		)
	}
	return ctx.Truncate(strings.Join(lines, "\n"))
}

// getPlanMode 从会话状态字典中提取 plan_mode 子字典
// Python: builders.py:416 (_get_plan_mode)
func getPlanMode(sessionState map[string]any) any {
	if sessionState == nil {
		return nil
	}
	if planMode, ok := sessionState["plan_mode"].(map[string]any); ok {
		return planMode
	}
	for _, key := range []string{"deepagent", "task_state"} {
		if nested, ok := sessionState[key].(map[string]any); ok {
			if planMode, ok := nested["plan_mode"].(map[string]any); ok {
				return planMode
			}
		}
	}
	for _, key := range []string{"global_state", "agent_state", "trace_state"} {
		if nested, ok := sessionState[key].(map[string]any); ok {
			if nestedPlanMode := getPlanMode(nested); nestedPlanMode != nil {
				if _, isMap := nestedPlanMode.(map[string]any); isMap {
					return nestedPlanMode
				}
			}
		}
	}
	return nil
}

// readTodoFile 读取 todo 文件（workspace/{session}/todo.json 或 workspace/todo/{session}/todo.json）
// Python: builders.py:439 (_read_todo_file)
func readTodoFile(ctx *ReinjectContext) []map[string]any {
	workspaceRoot := ctx.WorkspaceRoot
	if mc, ok := ctx.Context.(interface{ WorkspaceDir() string }); ok && workspaceRoot == "" {
		workspaceRoot = mc.WorkspaceDir()
	}
	if workspaceRoot == "" {
		return []map[string]any{}
	}

	sessionID := ""
	if mc, ok := ctx.Context.(interface{ SessionID() string }); ok {
		sessionID = mc.SessionID()
	}
	if sessionID == "" {
		if sess, ok := ctx.Context.(interface{ GetSessionRef() sessionRefLike }); ok {
			if s := sess.GetSessionRef(); s != nil {
				sessionID = s.GetSessionID()
			}
		}
	}
	if sessionID == "" {
		return []map[string]any{}
	}

	candidatePaths := []string{
		filepath.Join(workspaceRoot, sessionID, "todo.json"),
		filepath.Join(workspaceRoot, "todo", sessionID, "todo.json"),
	}
	var todoPath string
	for _, path := range candidatePaths {
		if _, err := os.Stat(path); err == nil {
			todoPath = path
			break
		}
	}
	if todoPath == "" {
		return []map[string]any{}
	}
	data, err := os.ReadFile(todoPath)
	if err != nil {
		return []map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return []map[string]any{}
	}
	return normalizeTodos(parsed)
}

// sessionRefLike 会话引用窄面（GetSessionRef 返回值）
type sessionRefLike interface {
	GetSessionID() string
}

// normalizeTodos 收敛 todo 列表为 dict 列表
// Python: builders.py:433 (_normalize_todos)
func normalizeTodos(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := []map[string]any{}
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// ExtractSkillToolSnapshot 抽取 skill_tool 调用的技能快照
// Python: builders.py:488 (extract_skill_tool_snapshot)
func ExtractSkillToolSnapshot(messages []llm_schema.BaseMessage) string {
	for _, toolCall := range IterToolCalls(messages, "skill_tool") {
		args := parseToolArguments(toolCall.Arguments)
		relativePath := anyToStrOr(args["relative_file_path"], "SKILL.md")
		if relativePath != "" && relativePath != "SKILL.md" {
			continue
		}
		result := parseJSONishToolResult(findToolResultText(messages, toolCall.ID))
		resultDict, _ := result.(map[string]any)
		var content, directory any
		if resultDict != nil {
			content = resultDict["skill_content"]
			directory = resultDict["skill_directory"]
		}
		skillName := fmtStr(args["skill_name"])
		if skillName == "" {
			skillName = filepath.Base(anyToStrOr(directory, ""))
		}
		if contentStr, ok := content.(string); ok && strings.TrimSpace(contentStr) != "" {
			return "Skill: " + skillName + "\nPath: " + anyToStrOr(directory, "") + "/SKILL.md\n\n" + strings.TrimSpace(contentStr)
		}
	}
	return ""
}

// ExtractReadFileSkillSnapshot 抽取 read_file(skill.md) 的技能快照
// Python: builders.py:503 (extract_read_file_skill_snapshot)
func ExtractReadFileSkillSnapshot(messages []llm_schema.BaseMessage) string {
	for _, toolCall := range IterToolCalls(messages, "read_file") {
		argsText := toolCall.Arguments
		args := parseToolArguments(argsText)
		filePath := extractArgumentValue(args, argsText, []string{"file_path", "path"})
		if !isSkillFilePathLocal(filePath) {
			continue
		}
		resultText := findToolResultText(messages, toolCall.ID)
		result := parseJSONishToolResult(resultText)
		var content any = result
		if resultDict, ok := result.(map[string]any); ok {
			content = resultDict["content"]
		} else {
			content = resultText
		}
		if contentStr, ok := content.(string); ok && strings.TrimSpace(contentStr) != "" {
			return "Skill: " + filepath.Base(filepath.Dir(filePath)) + "\nPath: " + filePath + "\n\n" + strings.TrimSpace(contentStr)
		}
	}
	return ""
}

// IterToolCalls 按序遍历（可按名过滤）工具调用
// Python: builders.py:531 (iter_tool_calls)
func IterToolCalls(messages []llm_schema.BaseMessage, name string) []*llm_schema.ToolCall {
	var out []*llm_schema.ToolCall
	for _, message := range messages {
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, toolCall := range assistant.ToolCalls {
			if name != "" && toolCall.Name != name {
				continue
			}
			out = append(out, toolCall)
		}
	}
	return out
}

// CollectReadFilePaths 收集保留消息中已读取的文件路径
// Python: builders.py:551 (collect_read_file_paths)
func collectReadFilePaths(messages []llm_schema.BaseMessage) map[string]bool {
	paths := map[string]bool{}
	for _, message := range messages {
		toolMessage, ok := message.(*llm_schema.ToolMessage)
		if !ok {
			continue
		}
		result := parseJSONishToolResult(messageToText(toolMessage))
		if resultDict, ok := result.(map[string]any); ok {
			if filePath, ok := resultDict["file_path"].(string); ok {
				paths[filePath] = true
			}
		}
	}
	return paths
}

// ParseReadFileResult 解析 read_file 调用与结果为快照
// Python: builders.py:562 (parse_read_file_result)
func parseReadFileResult(toolCall *llm_schema.ToolCall, resultText string) *ReadFileSnapshot {
	argsText := toolCall.Arguments
	args := parseToolArguments(argsText)
	result := parseJSONishToolResult(resultText)
	filePath := ""
	var content any
	var lineCount any
	if resultDict, ok := result.(map[string]any); ok {
		if v := fmtStr(resultDict["file_path"]); v != "" {
			filePath = v
		}
		content = resultDict["content"]
		lineCount = resultDict["line_count"]
	} else {
		content = resultText
	}
	if filePath == "" {
		filePath = extractArgumentValue(args, argsText, []string{"file_path", "path"})
	}
	if _, isStr := content.(string); !isStr && resultText != "" {
		content = resultText
	}
	contentStr, contentIsStr := content.(string)
	filePathIsStr := filePath != ""
	if !contentIsStr || !filePathIsStr {
		return nil
	}
	if lineCount == nil {
		n := len(strings.Split(contentStr, "\n"))
		lineCount = n
	}
	offset := args["offset"]
	limit := args["limit"]
	partial := offset != nil || limit != nil
	snapshot := &ReadFileSnapshot{FilePath: filePath, Content: contentStr, Partial: partial}
	switch n := lineCount.(type) {
	case int:
		snapshot.LineCount = &n
	case int64:
		iv := int(n)
		snapshot.LineCount = &iv
	case float64:
		iv := int(n)
		snapshot.LineCount = &iv
	}
	return snapshot
}

// IsExcludedFromReinject 判断路径是否排除出回注（.git / node_modules / __pycache__）
// Python: builders.py:593 (is_excluded_from_reinject)
func isExcludedFromReinject(filePath string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	for _, part := range []string{"/.git/", "/node_modules/", "/__pycache__/"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// renderReadFileSnapshots 渲染文件快照块（超长头尾截断）
// Python: builders.py:598 (render_read_file_snapshots)
func renderReadFileSnapshots(snapshots []ReadFileSnapshot, truncate func(string) string, perFileChars int) string {
	var blocks []string
	for _, snapshot := range snapshots {
		content := snapshot.Content
		if len(content) > perFileChars {
			head := content[:perFileChars/2]
			tailChars := perFileChars - len(head)
			if tailChars < 0 {
				tailChars = 0
			}
			tail := content[len(content)-tailChars:]
			content = head + "\n...[READ_FILE_TRUNCATED]...\n" + tail
		}
		lineCountText := "unknown"
		if snapshot.LineCount != nil {
			lineCountText = itoaStr(*snapshot.LineCount)
		}
		lines := []string{
			"Recently read file: " + snapshot.FilePath,
			"Lines returned: " + lineCountText,
		}
		if snapshot.Partial {
			lines = append(lines, "Note: only part of this file was available.")
		}
		lines = append(lines, "", content)
		blocks = append(blocks, truncate(strings.Join(lines, "\n")))
	}
	return strings.Join(blocks, "\n\n")
}

// parseJSONishToolResult 解析工具结果文本：JSON 优先，失败回退 data= 字段
// Python: builders.py:632 (parse_jsonish_tool_result)
func parseJSONishToolResult(resultText string) any {
	if resultText == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(resultText), &parsed); err != nil {
		return parseDataField(resultText)
	}
	return parsed
}

// parseDataField 解析 `data={...}` 字段。
// Python 用 ast.literal_eval（Python 字面量）；Go 无等价物，仅支持 JSON 形态，
// 解析失败返回空 dict。偏差 D-24.15-c1（回收条件=无，Python 字面量解析不可移植）。
// Python: builders.py:641 (_parse_data_field)
func parseDataField(resultText string) map[string]any {
	re := regexp.MustCompile(`\bdata=(?P<data>\{.*?\})(?:\s+\w+=|$)`)
	m := re.FindStringSubmatch(resultText)
	if m == nil {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(m[1]), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// configInt 读配置 int（getattr(config, key, default) 语义）
func configInt(config any, key string, defaultValue int) int {
	if m, ok := config.(map[string]any); ok {
		if v, ok := m[key].(int); ok {
			return v
		}
		if v, ok := m[key].(float64); ok {
			return int(v)
		}
	}
	return defaultValue
}

// configBool 读配置 bool
func configBool(config any, key string, defaultValue bool) bool {
	if m, ok := config.(map[string]any); ok {
		if v, ok := m[key].(bool); ok {
			return v
		}
	}
	return defaultValue
}

// fmtStr 任意值转字符串（nil → 空串）
func fmtStr(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// anyToStrOr 任意值转字符串带默认值
func anyToStrOr(value any, fallback string) string {
	if s := fmtStr(value); s != "" {
		return s
	}
	return fallback
}

// anyToStrPreferring 依次取首选键的字符串值
func anyToStrPreferring(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := fmtStr(m[key]); s != "" {
			return s
		}
	}
	return ""
}

// containsStr 判断切片包含
func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// itoaStr int 转字符串
func itoaStr(n int) string {
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ───────────────── 本地复刻助手（PY builders.py 为避环本地复刻 util 同名符号） ─────────────────

// isSkillFilePath 判断 skill.md 路径（PY builders.py:481 本地复刻）
func isSkillFilePathLocal(filePath string) bool {
	if filePath == "" {
		return false
	}
	normalized := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	return strings.HasSuffix(normalized, "/skill.md") || strings.HasSuffix(normalized, "skill.md")
}

// groupCompletedAPIRounds 按已完成 API 轮次分组（PY builders.py:518 本地复刻）
func groupCompletedAPIRounds(messages []llm_schema.BaseMessage) [][]llm_schema.BaseMessage {
	ranges := ceprocessor.GroupCompletedAPIRounds(messages)
	out := make([][]llm_schema.BaseMessage, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, append([]llm_schema.BaseMessage(nil), messages[r[0]:r[1]]...))
	}
	return out
}

// messageSignature 消息签名（PY builders.py:522 本地复刻）
func messageSignature(message llm_schema.BaseMessage) string {
	toolCallIDs := []string{}
	if assistant, ok := message.(*llm_schema.AssistantMessage); ok {
		for _, toolCall := range assistant.ToolCalls {
			toolCallIDs = append(toolCallIDs, toolCall.ID)
		}
	}
	return messageRoleText(message) + "|" + messageToText(message) + "|" + strings.Join(toolCallIDs, "|")
}

// messageRoleText 取角色字符串
func messageRoleText(message llm_schema.BaseMessage) string {
	switch message.(type) {
	case *llm_schema.UserMessage:
		return "user"
	case *llm_schema.AssistantMessage:
		return "assistant"
	case *llm_schema.SystemMessage:
		return "system"
	case *llm_schema.ToolMessage:
		return "tool"
	default:
		return ""
	}
}

// findToolResultText 按 tool_call_id 逆序查找（PY builders.py:542 本地复刻）
func findToolResultText(messages []llm_schema.BaseMessage, toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if toolMessage, ok := messages[i].(*llm_schema.ToolMessage); ok && toolMessage.ToolCallID == toolCallID {
			return messageToText(toolMessage)
		}
	}
	return ""
}

// parseToolArguments 解析工具参数 JSON（PY builders.py:622 本地复刻）
func parseToolArguments(argumentsText string) map[string]any {
	if argumentsText == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argumentsText), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// extractArgumentValue 依次按键取值 + 正则兜底（PY builders.py:652 本地复刻）
func extractArgumentValue(parsedArguments map[string]any, argumentsText string, keys []string) string {
	for _, key := range keys {
		if value, ok := parsedArguments[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for _, key := range keys {
		re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]+)"`)
		if m := re.FindStringSubmatch(argumentsText); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// messageToText 消息文本清洗（PY builders.py:664 本地复刻）
func messageToText(message llm_schema.BaseMessage) string {
	if message == nil {
		return ""
	}
	content := message.GetContent()
	if content.IsText() {
		// PY：str content 原样返回；Go MessageContent 包装形态需先取 Text
		return cesupport.SanitizeContentForText(content.Text())
	}
	return cesupport.SanitizeContentForText(content)
}
