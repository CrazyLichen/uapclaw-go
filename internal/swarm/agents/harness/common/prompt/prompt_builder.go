package prompt

import (
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// responsePriority 响应节优先级
// 对齐 Python: PromptPriority.RESPONSE = 60 (prompt_builder.py L29)

// ──────────────────────────── 全局变量 ────────────────────────────

const responsePriority = 60

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildResponseSection 构建响应节（消息格式说明）。
// 对齐 Python: _response_prompt(language) (prompt_builder.py L36-105)
//
// 描述用户消息和系统消息的 JSON 格式，告知 LLM 如何按来源和类型分别处理消息。
// 此节由 ResponsePromptRail 在 BeforeModelCall 中动态注入（10.6.3-10 实现）。
func BuildResponseSection(language string) saprompt.PromptSection {
	// 对齐 Python L37-67: if language == "cn"
	// 提示词文本从 Python 源码逐行复制，禁止自己翻译
	responseCN := "# 消息说明\n\n" +
		"你会收到用户消息和系统消息，需按来源和类型分别处理。\n\n" +
		"## 用户消息\n\n" +
		"```json\n" +
		"{\n" +
		"  \"channel\": \"【频道来源，如 feishu / telegram / web】\",\n" +
		"  \"preferred_response_language\": \"【en 或 zh】\",\n" +
		"  \"content\": \"【用户消息内容】\",\n" +
		"  \"source\": \"user\"\n" +
		"}\n" +
		"```\n\n" +
		"## 系统消息\n\n" +
		"```json\n" +
		"{\n" +
		"  \"type\": \"【cron 或 heartbeat 或 notify】\",\n" +
		"  \"preferred_response_language\": \"【en 或 zh】\",\n" +
		"  \"content\": \"【任务信息】\",\n" +
		"  \"source\": \"system\"\n" +
		"}\n" +
		"```\n\n" +
		"- **cron**：定时任务，如「每日提醒」「周报汇总」。\n" +
		"- **heartbeat**：心跳任务，如「检查待办」「同步状态」。\n\n" +
		"系统任务完成后，以回复形式通知用户。"

	// 对齐 Python L70-99: else (english)
	responseEN := "# Message Format\n\n" +
		"You receive user messages and system messages; handle each by source and type.\n\n" +
		"## User Message\n\n" +
		"```json\n" +
		"{\n" +
		"  \"channel\": \"【channel source, e.g. feishu / telegram / web】\",\n" +
		"  \"preferred_response_language\": \"【en or zh】\",\n" +
		"  \"content\": \"【user message content】\",\n" +
		"  \"source\": \"user\"\n" +
		"}\n" +
		"```\n\n" +
		"## System Message\n\n" +
		"```json\n" +
		"{\n" +
		"  \"type\": \"【cron or heartbeat or notify】\",\n" +
		"  \"preferred_response_language\": \"【en or zh】\",\n" +
		"  \"content\": \"【task info】\",\n" +
		"  \"source\": \"system\"\n" +
		"}\n" +
		"```\n\n" +
		"- **cron**: Scheduled tasks, e.g. \"daily reminder\", \"weekly summary\".\n" +
		"- **heartbeat**: Heartbeat tasks, e.g. \"check todos\", \"sync status\".\n\n" +
		"After completing a system task, notify the user via a reply."

	return saprompt.PromptSection{
		Name:     sections.SectionResponse,
		Content:  map[string]string{"cn": responseCN, "en": responseEN},
		Priority: responsePriority,
	}
}

// BuildAgentIdentityPrompt 构建 Agent 身份提示词。
// 对齐 Python: build_agent_identity_prompt(language) (prompt_builder.py L248-259)
//
// Python 执行步骤：
//
//  1. resolved_language = resolve_language(language)
//  2. builder = SystemPromptBuilder(language=resolved_language)
//  3. builder.add_section(_identity_prompt(resolved_language))
//  4. return builder.build()
func BuildAgentIdentityPrompt(language string) string {
	// 步骤 1: 对齐 Python: resolved_language = resolve_language(language)
	resolvedLanguage := prompts.ResolveLanguage(language)

	// 步骤 2: 对齐 Python: builder = SystemPromptBuilder(language=resolved_language)
	// 使用 PromptModeNone，因为 build_agent_identity_prompt 只包含 identity 节
	builder := prompts.NewSystemPromptBuilder(resolvedLanguage, hschema.PromptModeNone)

	// 步骤 3: 对齐 Python: builder.add_section(_identity_prompt(resolved_language))
	builder.AddSection(sections.BuildIdentitySection())

	// 步骤 4: 对齐 Python: return builder.build()
	return builder.Build()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readWorkspaceFile 读取工作空间文件内容。
// 对齐 Python: _read_file(file_path) (prompt_builder.py L265-281)
//
// 当前 BuildAgentIdentityPrompt 未使用此函数，但为与 Python 对齐保留。
func readWorkspaceFile(filePath string) string {
	if filePath == "" {
		return ""
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// T10.6.1: 添加 Debug 日志（对齐 Python: logger.debug 文件不存在）
			logger.Debug(logComponent).
				Str("file_path", filePath).
				Msg("文件不存在")
		} else {
			logger.Error(logComponent).
				Str("event_type", "read_workspace_file_error").
				Str("file_path", filePath).
				Err(err).
				Msg("读取工作空间文件失败")
		}
		return ""
	}
	content := string(data)
	if content == "" {
		return ""
	}
	// T10.6.2: 添加 TrimSpace（对齐 Python: content.strip()）
	content = strings.TrimSpace(content)
	return content
}
