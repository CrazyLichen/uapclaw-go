package sections

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	wscontent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/workspace_content"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInfo 工具信息（名称→描述映射条目）
type ToolInfo struct {
	// Name 工具名称
	Name string
	// Description 工具描述
	Description string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// contextEmptyFileHintCN 上下文空文件提示（中文）
	contextEmptyFileHintCN = "[以下文件仅在有实际内容时注入，空文件跳过]\n\n"
	// contextEmptyFileHintEN 上下文空文件提示（英文）
	contextEmptyFileHintEN = "[The following files are injected only when they contain real content; empty files are skipped]\n\n"

	// maxTemplateLen 模板检测最大长度，超过此长度不视为模板
	maxTemplateLen = 500
)

// ──────────────────────────── 全局变量 ────────────────────────────

// templateMarkers 未填充模板标记短语
var templateMarkers = []string{
	"此处应保存的内容",
	"What should be saved here",
	"在你们的第一次对话中填写",
	"Fill this in during your first",
	"在这里添加你需要",
	"Add your periodic tasks here",
}

// htmlCommentRegexp HTML 注释正则
var htmlCommentRegexp = regexp.MustCompile(`(?s)<!--.*?-->`)

// markdownHeadingRegexp Markdown 标题正则
var markdownHeadingRegexp = regexp.MustCompile(`(?m)^#{1,6}\s+.*$`)

// hiddenTools 隐藏工具集合（不在工具列表中展示）
var hiddenTools = map[string]bool{
	"cron_list_jobs":   true,
	"cron_get_job":     true,
	"cron_create_job":  true,
	"cron_update_job":  true,
	"cron_delete_job":  true,
	"cron_toggle_job":  true,
	"cron_preview_job": true,
}

// summaryOverridesCN 中文工具摘要覆盖
var summaryOverridesCN = map[string]string{
	"paid_search":               "付费联网搜索（配置 API 时优先使用）",
	"free_search":               "免费搜索（DuckDuckGo 等）",
	"fetch_webpage":             "抓取网页文本内容",
	"image_ocr":                 "读取图片中的文字",
	"visual_question_answering": "理解图片内容并回答问题",
	"audio_transcription":       "转写音频文件",
	"audio_question_answering":  "理解音频内容并回答",
	"audio_metadata":            "识别音频时长和歌曲信息",
	"video_understanding":       "分析视频内容",
	"session_new":               "创建多个协程任务（子 agent 异步运行）",
	"session_cancel":            "取消正在运行的协程",
	"session_list":              "查看所有协程状态",
	"cron":                      "管理定时任务与提醒",
	"bash":                      "执行 Shell 命令",
	"code":                      "执行 Python 或 JavaScript 代码",
	"list_skill":                "列出可用技能",
	"task_tool":                 "启动临时子代理处理复杂任务",
}

// summaryOverridesEN 英文工具摘要覆盖
var summaryOverridesEN = map[string]string{
	"paid_search":               "Paid web search (preferred when configured)",
	"free_search":               "Free web search",
	"fetch_webpage":             "Fetch webpage text",
	"image_ocr":                 "Read text from images",
	"visual_question_answering": "Understand images and answer questions",
	"audio_transcription":       "Transcribe audio",
	"audio_question_answering":  "Understand audio and answer questions",
	"audio_metadata":            "Identify audio duration and song metadata",
	"video_understanding":       "Analyze video content",
	"session_new":               "Create async sub-agent sessions",
	"session_cancel":            "Cancel a running sub-agent session",
	"session_list":              "List sub-agent session status",
	"cron":                      "Manage scheduled jobs and reminders",
	"bash":                      "Run shell commands",
	"code":                      "Run Python or JavaScript code",
	"list_skill":                "List available skills",
	"task_tool":                 "Launch a temporary sub-agent for complex work",
}

// preferredToolOrder 优先工具顺序
var preferredToolOrder = []string{
	"paid_search",
	"free_search",
	"fetch_webpage",
	"image_ocr",
	"visual_question_answering",
	"audio_transcription",
	"audio_question_answering",
	"audio_metadata",
	"video_understanding",
	"session_new",
	"session_cancel",
	"session_list",
	"cron",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsUnfilledTemplate 判断内容是否为未填充的工作区模板
//
// 规则（按顺序应用）：
// 1. 超过 maxTemplateLen 的文件不视为模板
// 2. 去除 HTML 注释后若无内容 → 模板
// 3. 包含任何 templateMarkers 标记短语 → 模板
// 4. 去除 Markdown 标题后若无内容 → 模板
func IsUnfilledTemplate(content string) bool {
	return isUnfilledTemplate(content, maxTemplateLen)
}

// BuildToolsContent 根据工具列表构建工具内容字符串
//
// tools 为工具名称→描述的映射；lang 为语言标识。
// 返回格式化的工具列表内容，若无工具则返回空字符串。
func BuildToolsContent(tools map[string]string, lang string) string {
	if len(tools) == 0 {
		return ""
	}

	summaryOverrides := summaryOverridesCN
	if lang == "en" {
		summaryOverrides = summaryOverridesEN
	}

	toolSummary := func(name string) string {
		if override, ok := summaryOverrides[name]; ok {
			return override
		}
		return strings.TrimSpace(tools[name])
	}

	var lines []string
	if lang == "cn" {
		lines = []string{"# 可用工具", ""}
	} else {
		lines = []string{"# Available Tools", ""}
	}
	renderedNames := make(map[string]bool)

	// 优先顺序工具
	for _, name := range preferredToolOrder {
		if _, hasTool := tools[name]; hasTool && !hiddenTools[name] {
			lines = append(lines, fmt.Sprintf("- %s: %s", name, toolSummary(name)))
			renderedNames[name] = true
		}
	}

	// 分组工具
	type toolGroup struct {
		names []string
		label string
		desc  string
	}
	groupedLabels := []toolGroup{
		{
			names: []string{"read_file", "write_file", "edit_file"},
			label: "read_file / write_file / edit_file",
			desc:  "文件读写编辑",
		},
		{
			names: []string{"glob", "list_files", "grep"},
			label: "glob / list_files / grep",
			desc:  "文件搜索",
		},
	}
	if lang == "en" {
		groupedLabels[0].desc = "Read, write, and edit files"
		groupedLabels[1].desc = "Search files and file contents"
	}

	for _, group := range groupedLabels {
		existing := 0
		for _, name := range group.names {
			if _, hasTool := tools[name]; hasTool && !hiddenTools[name] {
				existing++
			}
		}
		if existing == len(group.names) {
			lines = append(lines, fmt.Sprintf("- %s: %s", group.label, group.desc))
			for _, name := range group.names {
				renderedNames[name] = true
			}
		} else {
			for _, name := range group.names {
				if _, hasTool := tools[name]; hasTool && !hiddenTools[name] && !renderedNames[name] {
					lines = append(lines, fmt.Sprintf("- %s: %s", name, toolSummary(name)))
					renderedNames[name] = true
				}
			}
		}
	}

	// bash 和 code 工具
	for _, name := range []string{"bash", "code"} {
		if _, hasTool := tools[name]; hasTool && !hiddenTools[name] && !renderedNames[name] {
			lines = append(lines, fmt.Sprintf("- %s: %s", name, toolSummary(name)))
			renderedNames[name] = true
		}
	}

	// 技能列表
	if _, hasTool := tools["list_skill"]; hasTool && !hiddenTools["list_skill"] && !renderedNames["list_skill"] {
		lines = append(lines, fmt.Sprintf("- list_skill: %s", toolSummary("list_skill")))
		renderedNames["list_skill"] = true
	}

	// 记忆系统分组
	memoryGroup := toolGroup{
		names: []string{"memory_search", "memory_get", "write_memory", "edit_memory", "read_memory"},
		label: "memory_search / memory_get / write_memory / edit_memory / read_memory",
		desc:  "记忆系统",
	}
	if lang == "en" {
		memoryGroup.desc = "Memory system"
	}
	existing := 0
	for _, name := range memoryGroup.names {
		if _, hasTool := tools[name]; hasTool && !hiddenTools[name] {
			existing++
		}
	}
	if existing == len(memoryGroup.names) {
		lines = append(lines, fmt.Sprintf("- %s: %s", memoryGroup.label, memoryGroup.desc))
		for _, name := range memoryGroup.names {
			renderedNames[name] = true
		}
	} else {
		for _, name := range memoryGroup.names {
			if _, hasTool := tools[name]; hasTool && !hiddenTools[name] && !renderedNames[name] {
				lines = append(lines, fmt.Sprintf("- %s: %s", name, toolSummary(name)))
				renderedNames[name] = true
			}
		}
	}

	// 任务工具
	if _, hasTool := tools["task_tool"]; hasTool && !hiddenTools["task_tool"] && !renderedNames["task_tool"] {
		lines = append(lines, fmt.Sprintf("- task_tool: %s", toolSummary("task_tool")))
		renderedNames["task_tool"] = true
	}

	// bash 使用原则
	if renderedNames["bash"] {
		lines = append(lines, "")
		if lang == "cn" {
			lines = append(lines,
				"## bash 使用原则",
				"",
				"- 优先使用专用工具完成文件搜索、内容搜索、读取、编辑和写入，不要用 bash 替代 `glob` / `grep` / `read_file` / `edit_file` / `write_file`",
				"- 独立命令尽量并行调用；多步依赖命令才在单次调用里用 `&&` 串联，仅在不关心前序失败时才用 `;`",
				"- 长时间运行命令使用 `background: true`，不要用 `sleep` 轮询等待",
				"- 尽量使用绝对路径并避免频繁 `cd`；路径包含空格时使用双引号",
				"- 执行破坏性 Git 操作前先考虑更安全的替代方案",
			)
		} else {
			lines = append(lines,
				"## bash Guidelines",
				"",
				"- Prefer dedicated tools for file search, content search, reading, editing, and writing instead of using bash as a substitute for `glob` / `grep` / `read_file` / `edit_file` / `write_file`",
				"- Run independent commands in parallel; only chain dependent commands with `&&`, and use `;` only when earlier failures do not matter",
				"- Use `background: true` for long-running commands instead of polling with `sleep`",
				"- Prefer absolute paths and avoid frequent `cd`; quote paths with spaces using double quotes",
				"- Consider safer alternatives before destructive Git operations",
			)
		}
	}

	// task_tool 使用原则
	if renderedNames["task_tool"] {
		lines = append(lines, "")
		if lang == "cn" {
			lines = append(lines,
				"## task_tool 使用原则",
				"",
				"- 任务复杂、多步骤、可独立执行时使用",
				"- 独立任务尽量并行执行",
				"- 简单任务直接执行，不使用子代理",
			)
			agentLines := extractTaskToolAgentLines(tools["task_tool"], lang)
			if len(agentLines) > 0 {
				lines = append(lines, "", "可用代理类型：")
				lines = append(lines, agentLines...)
			}
		} else {
			lines = append(lines,
				"## task_tool Guidelines",
				"",
				"- Use it for complex, multi-step, independent tasks",
				"- Run independent tasks in parallel when possible",
				"- Execute simple tasks directly without spawning a sub-agent",
			)
			agentLines := extractTaskToolAgentLines(tools["task_tool"], lang)
			if len(agentLines) > 0 {
				lines = append(lines, "", "Available agent types:")
				lines = append(lines, agentLines...)
			}
		}
	}

	// 剩余未渲染的工具
	sortedNames := make([]string, 0)
	for name := range tools {
		if !renderedNames[name] && !hiddenTools[name] {
			sortedNames = append(sortedNames, name)
		}
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		compactDesc := strings.TrimSpace(tools[name])
		if idx := strings.IndexByte(compactDesc, '\n'); idx >= 0 {
			compactDesc = compactDesc[:idx]
		}
		if override, ok := summaryOverrides[name]; ok {
			compactDesc = override
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", name, compactDesc))
	}

	return strings.Join(lines, "\n") + "\n"
}

// BuildToolsSection 构建工具节（Priority 30）
//
// tools 为工具名称→描述的映射；lang 为语言标识。
func BuildToolsSection(tools map[string]string, lang string) *saprompt.PromptSection {
	content := BuildToolsContent(tools, lang)
	if content == "" {
		return nil
	}
	section := saprompt.PromptSection{
		Name:     SectionTools,
		Content:  map[string]string{lang: content},
		Priority: 30,
	}
	return &section
}

// BuildContextSection 构建上下文节（Priority 80）
//
// files 为文件名→内容的映射；dailyContent 为每日记忆内容（可为空）。
func BuildContextSection(files map[string]string, lang string) saprompt.PromptSection {
	var header string
	var titles map[string]string
	var dailyTitleTpl string

	if lang == "en" {
		header = wscontent.ContextHeaderEN
		titles = wscontent.ContextFileTitlesEN
		dailyTitleTpl = wscontent.DailyMemoryTitleEN
	} else {
		header = wscontent.ContextHeaderCN
		titles = wscontent.ContextFileTitlesCN
		dailyTitleTpl = wscontent.DailyMemoryTitleCN
	}

	var sb strings.Builder
	sb.WriteString(header)

	for _, fileKey := range wscontent.ContextFiles {
		content, ok := files[fileKey]
		if !ok || content == "" {
			continue
		}
		// 模板检测：跳过未填充模板文件
		if IsUnfilledTemplate(content) {
			continue
		}
		title, exists := titles[fileKey]
		if !exists {
			title = fmt.Sprintf("## %s", fileKey)
		}
		sb.WriteString(title)
		sb.WriteString("\n\n")
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}

	if lang == "cn" {
		sb.WriteString(contextEmptyFileHintCN)
	} else {
		sb.WriteString(contextEmptyFileHintEN)
	}

	// 每日记忆内容（由调用方通过 files["daily_memory"] 传入）
	if dailyContent, ok := files["daily_memory"]; ok && dailyContent != "" {
		dateStr := ""
		if d, ok2 := files["daily_memory_date"]; ok2 {
			dateStr = d
		}
		title := strings.ReplaceAll(dailyTitleTpl, "{date}", dateStr)
		sb.WriteString(title)
		sb.WriteString("\n\n")
		sb.WriteString(dailyContent)
		sb.WriteString("\n\n")
	}

	return saprompt.PromptSection{
		Name:     SectionContext,
		Content:  map[string]string{lang: sb.String()},
		Priority: 80,
	}
}

// ReadContextFiles 从工作空间读取上下文配置文件内容。
//
// 遍历 wscontent.ContextFiles 列表，通过 fsOp.ReadFile 读取每个文件。
// 对 MEMORY.md 特殊处理：从 WorkspaceNodeMemory 目录下读取。
// 过滤掉空模板文件（IsUnfilledTemplate）。
//
// 对齐 Python: _read_context_file(sys_operation, workspace, file_key)
func ReadContextFiles(ctx context.Context, fsOp sysop.FsOperation, ws *hworkspace.Workspace) map[string]string {
	if fsOp == nil || ws == nil {
		return nil
	}

	files := make(map[string]string)
	for _, fileKey := range wscontent.ContextFiles {
		var fullPath string
		if fileKey == "MEMORY.md" {
			// 对齐 Python: memory_dir = workspace.get_node_path(WorkspaceNode.MEMORY)
			// full_path = memory_dir / WorkspaceNode.MEMORY_MD.value
			memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
			if memoryDir == nil {
				continue
			}
			fullPath = *memoryDir + "/" + fileKey
		} else {
			nodePath := ws.GetNodePath(hworkspace.WorkspaceNode(fileKey))
			if nodePath == nil {
				continue
			}
			fullPath = *nodePath
		}

		result, err := fsOp.ReadFile(ctx, fullPath)
		if err != nil || result == nil || result.Data == nil || result.Data.Content == "" {
			continue
		}
		if IsUnfilledTemplate(result.Data.Content) {
			continue
		}
		files[fileKey] = result.Data.Content
	}
	return files
}

// ReadDailyMemory 读取当日每日记忆文件内容。
//
// 返回 (content, dateStr)，如果当日文件不存在则返回 ("", "")。
//
// 对齐 Python: _read_daily_memory(sys_operation, workspace, timezone)
func ReadDailyMemory(ctx context.Context, fsOp sysop.FsOperation, ws *hworkspace.Workspace, timezone string) (string, string) {
	if fsOp == nil || ws == nil {
		return "", ""
	}

	if timezone == "" {
		timezone = "Asia/Shanghai"
	}

	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	if memoryDir == nil {
		return "", ""
	}

	dailyMemoryDir := *memoryDir + "/" + string(hworkspace.WorkspaceNodeDailyMemory)

	listResult, err := fsOp.ListFiles(ctx, dailyMemoryDir)
	if err != nil || listResult == nil || listResult.Data == nil || len(listResult.Data.ListItems) == 0 {
		return "", ""
	}

	tz, _ := time.LoadLocation(timezone)
	date := time.Now().In(tz).Format("2006-01-02")
	todayFile := date + ".md"

	found := false
	for _, item := range listResult.Data.ListItems {
		if item.Name == todayFile {
			found = true
			break
		}
	}
	if !found {
		return "", ""
	}

	fullPath := dailyMemoryDir + "/" + todayFile
	result, err := fsOp.ReadFile(ctx, fullPath)
	if err != nil || result == nil || result.Data == nil || result.Data.Content == "" {
		return "", ""
	}

	return result.Data.Content, date
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isUnfilledTemplate 判断内容是否为未填充的工作区模板
func isUnfilledTemplate(content string, maxLen int) bool {
	if len(content) > maxLen {
		return false
	}
	text := strings.TrimSpace(htmlCommentRegexp.ReplaceAllString(content, ""))
	if text == "" {
		return true
	}
	for _, marker := range templateMarkers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	noHeadings := strings.TrimSpace(markdownHeadingRegexp.ReplaceAllString(text, ""))
	return noHeadings == ""
}

// extractTaskToolAgentLines 从 task_tool 描述中提取可用代理类型行
func extractTaskToolAgentLines(description string, lang string) []string {
	if description == "" {
		return nil
	}

	var marker, stopMarker string
	if lang == "cn" {
		marker = "可用代理类型及对应工具："
		stopMarker = "重要："
	} else {
		marker = "Available agent types and the tools they have access to:"
		stopMarker = "Important:"
	}

	if !strings.Contains(description, marker) {
		return nil
	}

	body := description
	if idx := strings.Index(description, marker); idx >= 0 {
		body = description[idx+len(marker):]
	}
	if strings.Contains(body, stopMarker) {
		if idx := strings.Index(body, stopMarker); idx >= 0 {
			body = body[:idx]
		}
	}

	var lines []string
	for _, rawLine := range strings.Split(strings.TrimSpace(body), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			lines = append(lines, line)
		} else {
			lines = append(lines, "- "+line)
		}
	}
	return lines
}
