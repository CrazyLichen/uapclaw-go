package prompt

import (
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// responsePriority 响应节优先级
// Python: PromptPriority.RESPONSE = 60 (prompt_builder.py L29)
const responsePriority = 60

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildResponseSection 构建响应节（消息格式说明）。
// Python: _response_prompt(language) (prompt_builder.py L36-105)
//
// 描述用户消息和系统消息的 JSON 格式，告知 LLM 如何按来源和类型分别处理消息。
// 此节由 ResponsePromptRail 在 BeforeModelCall 中动态注入（10.6.3-10 实现）。
func BuildResponseSection(language string) saprompt.PromptSection {
	// Python: L37-67: if language == "cn"
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

	// Python: L70-99: else (english)
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
// Python: build_agent_identity_prompt(language) (prompt_builder.py L248-259)
//
// Python 执行步骤：
//
//	Python: 1. resolved_language = resolve_language(language)
//	Python: 2. builder = SystemPromptBuilder(language=resolved_language)
//	Python: 3. builder.add_section(_identity_prompt(resolved_language))
//	Python: 4. return builder.build()
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
// Python: _read_file(file_path) (prompt_builder.py L265-281)
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
				Msg("file does not exist")
		} else {
			logger.Error(logComponent).
				Str("event_type", "read_workspace_file_error").
				Str("file_path", filePath).
				Err(err).
				Msg("failed to read workspace file")
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

// ──────────────────────────── 导出函数（15.20 ⑩ 对齐段） ────────────────────────────

// promptPriorityInput / promptPriorityOutput 优先级。
// Python: PromptPriority.INPUT = SystemPromptPriority.INPUT = 60；OUTPUT = 65。
const (
	// promptPriorityInput input 节优先级（Python PromptPriority.INPUT）
	promptPriorityInput = 60
	// promptPriorityOutput output 节优先级（Python PromptPriority.OUTPUT）
	promptPriorityOutput = 65
	// promptPriorityA2UI a2ui 节优先级（Python PromptPriority.A2UI = 61）
	promptPriorityA2UI = 61
)

// LocalSectionNameA2UI 可选 JiuwenSwarm 节名。
// Python: LocalSectionName.A2UI = "a2ui"
const LocalSectionNameA2UI = "a2ui"

// InputPrompt 构建输入说明节。
//
// Python: _input_prompt (prompt_builder.py:92-156)
func InputPrompt(language string) saprompt.PromptSection {
	var content string
	if language == "cn" {
		content = `# 输入说明

## 用户消息

` + "```json" + `
{
  "channel": "【频道来源，如 feishu / telegram / web】",
  "preferred_response_language": "【en 或 zh】",
  "content": "【用户消息内容】",
  "source": "user"
}
` + "```" + `

## 系统消息

` + "```json" + `
{
  "type": "【系统消息类型，如 cron / notify】",
  "preferred_response_language": "【en 或 zh】",
  "content": "【任务信息】",
  "source": "system"
}
` + "```" + `

系统消息类型说明：
- cron：定时任务，如每日提醒，每周周报等；
- notify：系统通知
`
	} else {
		content = `# Input Instructions

## User Messages

` + "```json" + `
{
  "channel": "【channel source, such as feishu / telegram / web】",
  "preferred_response_language": "【en or zh】",
  "content": "【user message content】",
  "source": "user"
}
` + "```" + `

## System Messages

` + "```json" + `
{
  "type": "【system message type, such as cron / notify】",
  "preferred_response_language": "【en or zh】",
  "content": "【task information】",
  "source": "system"
}
` + "```" + `

System message types:
- cron: scheduled tasks such as daily reminders or weekly reports;
- notify: system notifications
`
	}
	return saprompt.PromptSection{
		Name:     "input",
		Content:  map[string]string{language: content},
		Priority: promptPriorityInput,
	}
}

// OutputPrompt 构建输出规则节（含文件投递开关分支）。
//
// Python: _output_prompt (prompt_builder.py:158-315)
func OutputPrompt(language string, sendFileEnabled bool) saprompt.PromptSection {
	var content string
	if language == "cn" {
		fileDeliveryRule := "- 当前渠道不支持文件投递。不要声称可以发送、下载或导出文件；" +
			"需要交付的内容必须完整写入最终回复。"
		rasterDeliveryRule := "- 当前渠道不能回传文件；位图产物直接在最终回复中呈现，" +
			"不要承诺通过文件发送。"
		svgDeliveryRule := "- **词义消歧**：用户说“给我 svg”“用 svg 画”“要矢量图标”指源码而非 " +
			".svg 文件附件；若用户明确要求文件，说明当前渠道不能回传文件，" +
			"改为在最终回复中提供完整的 SVG/XML 源码。" +
			"Mermaid 仅用于标准结构图，超出其表达或用户明确要 SVG 时直接给源码。"
		svgAttachmentRule := "- SVG 卡片源码须在**最后一条无工具调用的消息**里；" +
			"不要为该产物生成附件或承诺文件投递。"
		if sendFileEnabled {
			fileDeliveryRule = "- 任务产生需要交付的文件，或用户明确请求下载、导出、发送文件时，" +
				"调用 `send_file_to_user`，调用时使用服务端可访问的绝对路径，" +
				"如果用户指定了投递 channel 则传入 `target_channels` 参数，" +
				"具体参数、支持的 channel 和默认投递行为以 `send_file_to_user` " +
				"工具 Schema 为准。"
			rasterDeliveryRule = "- 仅当产物本质是位图（照片、AI 生图）或用户明确要 png/jpg/pdf 时" +
				"才 `generate_image` + `send_file_to_user`；用户指定格式时以用户为准。"
			svgDeliveryRule = "- **词义消歧**：用户说“给我 svg”“用 svg 画”“要矢量图标”指源码而非 " +
				".svg 文件附件；仅当明确出现“文件/下载/导出/保存为 .svg”时才生成并\n" +
				"  投递文件。Mermaid 仅用于标准结构图，超出其表达或用户明确要 SVG 时直接给源码。"
			svgAttachmentRule = "- SVG 卡片源码须在**最后一条无工具调用的消息**里；" +
				"同一产物不要既内联又发文件。"
		}
		content = "# 输出规则\n\n" +
			"## 最终回复规则\n\n" +
			"- 系统任务完成后，以回复形式通知用户。\n" +
			"- 用户最终看到的只有最后一条不带工具调用的消息。\n\n" +
			"## 产物或交付件规则\n\n" +
			"- 完整交付物必须放在最后一条不带工具调用的消息中。\n" +
			"- 不要只用“已完成”“详见上文”等状态说明代替完整交付物。\n" +
			fileDeliveryRule + "\n" +
			"- 需要渲染为图片卡片的矢量产物（流程图、架构图、示意图、图标、插画等），默认用 ```svg 围栏包裹完整自包含的 `<svg>...</svg>` 源码写在最终回复正文里；每个围栏必须且只能包含一个顶层 `<svg>`，多个产物分别使用独立围栏。仅展示、讲解、调试或供用户复制修改 SVG 源码时，不得使用 ```svg 围栏：独立 SVG/XML 源码使用 ```xml 围栏，SVG 嵌入 HTML 的示例使用 ```html 围栏，不完整的伪代码或标签片段使用 ```text 围栏。不生成 .svg 文件、不调 `generate_image`、不落盘投递。\n" +
			svgDeliveryRule + "\n" +
			svgAttachmentRule + "\n" +
			rasterDeliveryRule + "\n\n\n" +
			"## 输出语言\n\n" +
			"- 优先使用用户明确指定的回复语言。\n" +
			"- 用户未指定时，默认使用简体中文。\n" +
			"- 技术术语、代码标识符、路径和工具名称保持原本的语言。\n\n" +
			"## 模型名称回答\n\n" +
			"- 用户询问当前模型名称时，使用 `runtime.setting` 中的当前模型值回答，只说明模型名称。\n" +
			"- 用户询问支持或配置了哪些模型时，使用 `runtime.setting` 中的可用模型列表回答。\n"
	} else {
		fileDeliveryRule := "- This channel cannot deliver files. Do not claim that you can send, " +
			"download, or export files. Put the complete deliverable directly in the " +
			"final reply."
		rasterDeliveryRule := "- This channel cannot return files. Present raster artifacts directly in " +
			"the final reply and do not promise file delivery."
		svgDeliveryRule := "- **Lexical disambiguation**: \"give me an svg\", \"draw it in svg\", " +
			"\"I want a vector/icon\" means SVG source, NOT a .svg file attachment. " +
			"If the user explicitly requests a file, explain that this channel cannot " +
			"return files and provide the complete SVG/XML source in the final reply. " +
			"Use Mermaid only for standard structured charts; when the user explicitly " +
			"asks for SVG or the diagram is beyond Mermaid, output SVG source."
		svgAttachmentRule := "- SVG card source MUST go in the **last message with no tool calls**; " +
			"do not create an attachment for the same artifact or promise file delivery."
		if sendFileEnabled {
			fileDeliveryRule = "- When a task produces a file that must be delivered, or the user explicitly " +
				"requests a download, export, or file delivery, call `send_file_to_user` with " +
				"an absolute path accessible to the server. If the user specifies a delivery " +
				"channel, pass `target_channels`; follow the tool schema for parameters, " +
				"supported channels, and default delivery behavior."
			rasterDeliveryRule = "- Call `generate_image` + `send_file_to_user` only for inherently raster " +
				"artifacts (photos, AI image gen) or when the user explicitly requests " +
				"png/jpg/pdf; honor any explicit format the user specifies."
			svgDeliveryRule = "- **Lexical disambiguation**: \"give me an svg\", \"draw it in svg\", " +
				"\"I want a vector/icon\" means SVG source, NOT a .svg file attachment. " +
				"Only generate and deliver a file when the user explicitly says " +
				"\"file/download/export/save as .svg\". Use Mermaid only for standard " +
				"structured charts; when the user explicitly asks for SVG or the diagram " +
				"is beyond Mermaid, output SVG source."
			svgAttachmentRule = "- SVG card source MUST go in the **last message with no tool calls**; " +
				"do not both inline and send a file for the same artifact."
		}
		content = "# Output Rules\n\n" +
			"## Final Response Rules\n\n" +
			"- After completing a system task, notify the user in a reply.\n" +
			"- The user sees only the final message that contains no tool calls.\n\n" +
			"## Artifact and Deliverable Rules\n\n" +
			"- Put the complete deliverable in the final message that contains no tool calls.\n" +
			"- Do not replace the complete deliverable with a status statement such as “done” or “see above.”\n" +
			fileDeliveryRule + "\n" +
			"- Vector artifacts that should render as image cards (flowcharts, architecture diagrams,\n" +
			"  schematics, icons, illustrations, etc.) default to inline SVG source in the final reply body:\n" +
			"  wrap one complete, self-contained top-level `<svg>...</svg>` in each ```svg fenced\n" +
			"  code block, and use separate fences for separate artifacts.\n" +
			"  When SVG source is only being shown, explained, debugged, or provided for the user to copy and\n" +
			"  modify, never use a ```svg fence: use ```xml for standalone SVG/XML source, ```html for examples\n" +
			"  with SVG embedded in HTML, and ```text for incomplete pseudocode or tag fragments. Do NOT\n" +
			"  generate .svg files, call `generate_image`, or save to disk to deliver.\n" +
			svgDeliveryRule + "\n" +
			svgAttachmentRule + "\n" +
			rasterDeliveryRule + "\n\n" +
			"## Output Language\n\n" +
			"- Prefer the response language explicitly requested by the user.\n" +
			"- If the user does not specify one, default to Simplified Chinese.\n" +
			"- Keep technical terms, code identifiers, paths, and tool names in their original language.\n\n" +
			"## Model Name Answers\n\n" +
			"- When asked for the current model name, use the current model value in `runtime.setting` and state only the model name.\n" +
			"- When asked which models are supported or configured, use the available model list in `runtime.setting`.\n"
	}
	return saprompt.PromptSection{
		Name:     "output",
		Content:  map[string]string{language: content},
		Priority: promptPriorityOutput,
	}
}
