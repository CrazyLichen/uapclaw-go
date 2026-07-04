package sections

import (
	"regexp"
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 常量 ────────────────────────────

// 注意：Python 源码中模板使用反引号包裹工具名（如 `HEARTBEAT_OK`），
// Go 双引号字符串中反引号无法直接表达，因此使用 \u0060 转义。
const (
	heartbeatSystemPromptCN = "\n" +
		"## 心跳检测\n" +
		"<heartbeat_user_task>\n" +
		"{heartbeat_section}\n" +
		"</heartbeat_user_task>\n" +
		"\n" +
		"判定规则：\n" +
		"1. 若 `<heartbeat_user_task>` 与 `</heartbeat_user_task>` 之间仅有空白（含空行）或完全为空：视为**无心跳用户任务**。你必须且仅能输出一行，内容**精确**为 `\u0060HEARTBEAT_OK\u0060`（不要解释、不要前后缀、不要 Markdown、不要工具调用说明）。\n" +
		"2. 若 `<heartbeat_user_task>` 与 `</heartbeat_user_task>` 之间存在**任意非空白字符**：该段即用户下发的心跳任务正文。你必须**完整阅读并执行**其中的指令并给出**直接回答**；**禁止**在回复中出现 `\u0060HEARTBEAT_OK\u0060` 四字（含单独一行、前缀、后缀、用标点或破折号拼接，例如 `\u0060HEARTBEAT_OK \u2014 \u2026\u0060` 一律视为违规）。\n" +
		"\n" +
		"系统**仅在**满足上一条规则 1（标签内无任务）时，才把单独一行的 `\u0060HEARTBEAT_OK\u0060` 视为心跳确认；有任务时**不得**为了\u201c确认心跳\u201d而输出或附带 `\u0060HEARTBEAT_OK\u0060`。\n" +
		"**禁止**用\u201c心跳任务已完成\u201d\u201c任务已完成 \u2713\u201d\u201c已处理\u201d\u201c无新内容\u201d\u201c安静待着\u201d等**状态话术**代替任务正文所要求的**具体可核验输出**（若正文要求输出某文本，回复中必须出现该文本本身，而不是完成声明）。\n" +
		"\n" +
		"重要约束：\n" +
		"- 每一轮心跳调用都是独立调度，只要 `<heartbeat_user_task>` 标签内有非空白任务正文，你就必须**当场**按正文完成指令所要求的动作或输出。**禁止**以\u201c上一轮刚执行过\u201d等理由省略执行或把\u201c记录\u201d当成完成——**记录不等于执行**。\n" +
		"- 若需修改 HEARTBEAT.md 文件，禁止给原本没有 <!-- --> 注释的内容添加注释标记\n" +
		"- 非注释文本仅可在用户明确要求时修改或删除，否则必须保持原样\n" +
		"- 心跳执行结果必须直接返回；除非心跳内容明确要求更新 HEARTBEAT.md，不要写入 daily memory 或其他记忆文件\n"

	heartbeatSystemPromptEN = "\n" +
		"## Heartbeat\n" +
		"<heartbeat_user_task>\n" +
		"{heartbeat_section}\n" +
		"</heartbeat_user_task>\n" +
		"\n" +
		"Decision rules:\n" +
		"1. If between `<heartbeat_user_task>` and `</heartbeat_user_task>` there is only whitespace (including blank lines) or nothing: treat as **no heartbeat user task**. You MUST output exactly one line whose content is **precisely** `\u0060HEARTBEAT_OK\u0060` (no explanation, no prefix/suffix, no Markdown, no tool narration).\n" +
		"2. If there is **any** non-whitespace character between `<heartbeat_user_task>` and `</heartbeat_user_task>`: that span is the **user-issued heartbeat task body**. You MUST read and carry out the instructions in full and reply **directly**; the substring `\u0060HEARTBEAT_OK\u0060` MUST NOT appear anywhere in your reply (not alone, not as a prefix/suffix, not glued with punctuation or em dashes\u2014e.g. `\u0060HEARTBEAT_OK \u2014 \u2026\u0060` is forbidden).\n" +
		"\n" +
		"The system treats a single line of exactly `\u0060HEARTBEAT_OK\u0060` **only** under rule 1 (empty task) as the heartbeat acknowledgment; when there is a task body, you MUST NOT emit or append `\u0060HEARTBEAT_OK\u0060` \u201cto confirm the heartbeat\u201d.\n" +
		"You MUST NOT replace substantive output with status-only phrases such as \u201cheartbeat task completed\u201d, \u201ctask done \u2713\u201d, \u201cnothing new\u201d, \u201cstay quiet\u201d, etc. If the body asks for specific text, that text MUST appear in the reply itself, not a declaration that you completed it.\n" +
		"\n" +
		"Important Constraints:\n" +
		"- Each heartbeat invocation is scheduled independently; whenever the `<heartbeat_user_task>` tags contain a non-empty task body, you MUST **on the spot** complete whatever actions or outputs the body requires. You MUST NOT skip execution or treat \u201clogging/recording\u201d as completion with excuses such as \u201calready executed last round\u201d\u2014**recording is not execution**.\n" +
		"- When modifying HEARTBEAT.md, DO NOT add <!-- --> comment markers to content that originally had no such markers\n" +
		"- Non-commented text may only be modified or deleted when explicitly requested by the user; otherwise preserve it as-is\n" +
		"- Return heartbeat execution results directly; unless heartbeat content explicitly asks you to update HEARTBEAT.md, do not write them to daily memory or other memory files\n"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// htmlCommentRegexp 匹配 HTML 注释行（保留，暂未使用）
	_ = regexp.MustCompile(`<!--.*?-->`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildHeartbeatSection 构建心跳节（Priority 80）
//
// heartbeatContent 为 HEARTBEAT.md 文件的原始内容（可为空）。
func BuildHeartbeatSection(heartbeatContent string, lang string) saprompt.PromptSection {
	var template string
	if lang == "en" {
		template = heartbeatSystemPromptEN
	} else {
		template = heartbeatSystemPromptCN
	}

	cleanedContent := cleanHeartbeatContent(heartbeatContent)
	heartbeatSection := cleanedContent

	content := strings.ReplaceAll(template, "{heartbeat_section}", heartbeatSection)

	return saprompt.PromptSection{
		Name:     SectionHeartbeat,
		Content:  map[string]string{lang: content},
		Priority: 80,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cleanHeartbeatContent 清理 HEARTBEAT.md 内容：移除 HTML 注释和空行
func cleanHeartbeatContent(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var cleanedLines []string
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "<!--") && strings.HasSuffix(stripped, "-->") {
			continue
		}
		if stripped != "" {
			cleanedLines = append(cleanedLines, stripped)
		}
	}
	return strings.Join(cleanedLines, "\n")
}
