package sections

import (
	"fmt"
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	wscontent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/workspace_content"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// contextEmptyFileHintCN 上下文空文件提示（中文）
	contextEmptyFileHintCN = "[以下文件仅在有实际内容时注入，空文件跳过]\n\n"
	// contextEmptyFileHintEN 上下文空文件提示（英文）
	contextEmptyFileHintEN = "[The following files are injected only when they contain real content; empty files are skipped]\n\n"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildToolsSection 构建工具节（Priority 30）
//
// toolDescs 为预渲染的工具描述内容字符串。
func BuildToolsSection(toolDescs string, lang string) saprompt.PromptSection {
	return saprompt.PromptSection{
		Name:     SectionTools,
		Content:  map[string]string{lang: toolDescs},
		Priority: 30,
	}
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
