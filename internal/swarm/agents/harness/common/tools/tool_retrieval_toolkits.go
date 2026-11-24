// Package tools 提供检索类只读工具的目录与工具包实现。
//
// 本文件对应 Python tool_retrieval_toolkits.py（1,030 行）：静态分类表
// （frozen）+ ToolRetrievalToolkit——tool_search 服务 list（分类导航，
// 名称+简述）与 get_schema（单工具 {name, description, schema}），渲染
// 静态表对齐 live deferred 注册表——无磁盘树索引、无 LLM 分类、无引擎
// 分页；静态表未覆盖的工具（MCP 连接器工具、迟到新增）经连接器虚拟分支
// 与未归类块露出；新增静态工具是代码变更。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/tool_retrieval_toolkits.py
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	recall "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/compressor/recall"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	tool_discovery "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/tool_discovery"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCategory 目录的一个分类：显示路径、描述、成员工具名。
// Python: ToolCategory NamedTuple（tool_retrieval_toolkits.py:41-51）。
type ToolCategory struct {
	// Path 分类显示路径
	Path string
	// Description 中文描述
	Description string
	// Tools 成员工具名（静态表声明意图；渲染时以 live 注册表为准）
	Tools []string
	// DescriptionEn 英文描述（空则回落 Description）
	DescriptionEn string
}

// DescriptionFor 本地化导航（不改名字与成员）。
// Python: ToolCategory.description_for。
func (c ToolCategory) DescriptionFor(language string) string {
	if language == "en" && c.DescriptionEn != "" {
		return c.DescriptionEn
	}
	return c.Description
}

// FlatToolEntry 未归类目录行的名称+描述对。
// Python: _FlatToolEntry NamedTuple。
type FlatToolEntry struct {
	Name        string
	Description string
}

// ToolCatalogueSnapshot 冻结在 live deferred 注册表之上的目录视图。
// Python: ToolCatalogueSnapshot NamedTuple（:269-285）——categories 只带
// 至少有一个 live 工具的分类，成员与 live 注册表取交集；flatEntries 列出
// 每个 live deferred 工具（未归类兜底与漂移基线）。
type ToolCatalogueSnapshot struct {
	Categories  []ToolCategory
	FlatEntries []FlatToolEntry
}

// McpServerDescription MCP 服务器的双语描述。
type McpServerDescription struct {
	Zh string
	En string
}

// ToolRetrievalToolkit 面向 deferred 工具的会话级 tool_search 结构化工具。
// Python: ToolRetrievalToolkit（:423-537 构造与 live 注册表段）。
type ToolRetrievalToolkit struct {
	// deferredToolsProvider 返回当前 deferred 工具集（延迟调用：注册发生在
	// 本工具包构造之后的 ensure_initialized；每次刷新/调用都取实时集合）
	deferredToolsProvider func() []schema.ToolInfoInterface
	// sessionScope 会话唯一标识（tool_search 工具 id 派生域）
	sessionScope string
	// configBase 应用配置
	configBase map[string]any
	// mcpServersProvider 返回 {server_name: description}；描述可为纯串
	// （双语共用）或 McpServerDescription
	mcpServersProvider func() map[string]any
	// language 运行时检索语言（与用户输入无关）
	language string
	// toolByName live deferred 工具映射（LiveRecords 刷新）
	toolByName map[string]schema.ToolInfoInterface
	// frozenCatalogue 最近一次刷新冻结的目录
	frozenCatalogue ToolCatalogueSnapshot
}

// toolSearchTool session 内 tool_search 的 tool.Tool 实现（LocalFunction 等价物）。
type toolSearchTool struct {
	card    *tool.ToolCard
	toolkit *ToolRetrievalToolkit
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ToolSearchToolName tool_search 元工具名
	ToolSearchToolName = "tool_search"
	// toolIDDomain 工具 id 派生域分隔符
	// Python: _TOOL_ID_DOMAIN = b"openjiuwen.tool-index-tool.v1\0"
	toolIDDomain = "openjiuwen.tool-index-tool.v1\x00"
	// schemaDescChars 列表描述压缩上限
	// Python: _SCHEMA_DESC_CHARS
	schemaDescChars = 200
)

// toolSearchCardDescriptionEn 英文卡描述。
// Python: _TOOL_SEARCH_CARD_DESCRIPTION。
const toolSearchCardDescriptionEn = "Deferred-tool catalogue, read-only. list: tools under [category]; " +
	"add [keyword] to BM25-search within the category or, with no " +
	"category, across all tools. " +
	"get_schema: the {name, description, schema} of the tool named in " +
	"[tool]; results execute via the fixed tool_call tool. " +
	"Never executes tools."

// toolSearchCardDescriptionZh 中文卡描述。
// Python: _TOOL_SEARCH_CARD_DESCRIPTION_CN。
const toolSearchCardDescriptionZh = "只读的 deferred 工具目录。list 列出指定 category 下的工具；可选 keyword 用于 BM25 过滤，" +
	"省略 category 时按 keyword 全局检索。" +
	"get_schema 获取 tool 指定工具的名称、描述和 schema，再通过固定 tool_call 执行。" +
	"本工具永不执行其他工具。"

// objectParamAnchor 附加到 get_schema 输出中所有 object 型参数的直调序列化规则。
// Python: _OBJECT_PARAM_ANCHOR（deferred 工具不在 tools 列表内，调用无 API 级
// schema 锚定——嵌套对象被双重序列化为 JSON 字符串，此处就地标注）。
const objectParamAnchor = " Must be passed as an inline JSON object literal, never a JSON string."

// ──────────────────────────── 全局变量 ────────────────────────────

// staticToolCategories 静态分类表——平铺名，无层级；分类行只有描述
// （工具名经 tool_search 的 list 发现）。成员列稳定内置 deferred 工具；
// live 注册表不存在的名字渲染时跳过。
// Python: _TOOL_CATEGORIES（20 分类逐条复刻）。
var staticToolCategories = []ToolCategory{
	{Path: "coding-memory", Description: "读写项目编码记忆（coding_memory/ 下的 markdown）：跨会话沉淀代码经验、事实与决策，支持分页读取与精确字符串替换。", DescriptionEn: "Read, write and edit project coding memory: persistent facts, decisions and lessons across sessions.", Tools: strings.Fields("coding_memory_edit coding_memory_read coding_memory_write")},
	{Path: "documents", Description: "读取 PDF 文档的文本内容，支持按页与分块读取。", DescriptionEn: "Read PDF text by page or in chunks.", Tools: strings.Fields("read_pdf")},
	{Path: "external-agents", Description: "与外部 ACP 兼容智能体子进程（Codex CLI、Gemini CLI 等）建立会话并收发消息。", DescriptionEn: "Start sessions and exchange messages with external ACP agents such as Codex CLI or Gemini CLI.", Tools: strings.Fields("acp_chat")},
	{Path: "file-system", Description: "列出目录内容。", DescriptionEn: "List directory contents.", Tools: strings.Fields("list_files")},
	{Path: "goals", Description: "查询当前会话的持续目标及其执行状态，提交目标执行结果的结构化报告（继续、完成或受阻）。", DescriptionEn: "Inspect the current persistent goal and report progress, completion or blockers.", Tools: strings.Fields("get_current_goal submit_goal_report")},
	{Path: "image-processing", Description: "使用视觉模型处理图片：OCR 文字提取与图片问答。", DescriptionEn: "Extract text from images with OCR and answer questions about images.", Tools: strings.Fields("image_ocr visual_question_answering")},
	{Path: "audio-processing", Description: "使用音频模型处理音频：语音转写、音频问答与元数据提取。", DescriptionEn: "Transcribe speech, answer questions about audio and read audio metadata.", Tools: strings.Fields("audio_metadata audio_question_answering audio_transcription")},
	{Path: "video-understanding", Description: "使用视频理解模型分析视频内容并回答相关问题。", DescriptionEn: "Analyze video content and answer questions about videos.", Tools: strings.Fields("video_understanding")},
	{Path: "image-generation", Description: "文生图：按文字提示词生成图片并保存到工作区。", DescriptionEn: "Generate images from text prompts and save them to the workspace.", Tools: strings.Fields("generate_image generate_visual")},
	{Path: "video-generation", Description: "文生视频：提交视频生成任务，按任务号轮询进度并下载结果。", DescriptionEn: "Generate videos from text prompts, poll generation status and download results.", Tools: strings.Fields("generate_video check_video_status")},
	{Path: "lsp", Description: "通过语言服务器（LSP）获取代码智能：定义、引用、诊断等。", DescriptionEn: "Use language servers for code definitions, references and diagnostics.", Tools: strings.Fields("lsp")},
	{Path: "modes", Description: "在 normal 与 plan 两种会话模式之间切换当前会话。", DescriptionEn: "Switch the current session between normal and plan modes.", Tools: strings.Fields("switch_mode")},
	{Path: "cron", Description: "cron 定时任务的创建、更新、删除、查询、启停、预览执行时间与手动触发。", DescriptionEn: "Create and manage standalone scheduled tasks, one-time reminders and recurring notifications.", Tools: strings.Fields("cron cron_create_job cron_delete_job cron_get_job cron_list_jobs cron_preview_job cron_toggle_job cron_update_job")},
	{Path: "heartbeat", Description: "绑定当前会话的心跳跟进任务的创建、更新、删除、查询、启停、预览触发时间、立即运行与取消运行。", DescriptionEn: "Schedule follow-ups that return to the current session to continue its existing task.", Tools: strings.Fields("heartbeat_cancel_run heartbeat_create_job heartbeat_delete_job heartbeat_get_job heartbeat_list_jobs heartbeat_preview_job heartbeat_run_now heartbeat_toggle_job heartbeat_update_job")},
	{Path: "sessions", Description: "跨会话协作：列出同一用户的其他持久化会话，向目标会话异步发送消息并跟踪其状态，读取指定会话的对话、进展与结果，处理未知状态的跨会话消息，恢复未执行的排队任务。", DescriptionEn: "List and read other persistent sessions, send messages, inspect delivery and resume queued tasks.", Tools: strings.Fields("session_continue_queued session_list session_message_list session_message_resolve session_read session_send_message")},
	{Path: "skill-evolution", Description: "本地技能与经验的演进：经用户确认创建演进评审、启动受限评审并接受已审查的变更提案，以及演进经验的查询、读取与整理。", DescriptionEn: "Review and apply local skill evolution proposals, and manage recorded skill experiences.", Tools: strings.Fields("evolve_review_task evolve_skill_experiences list_skill_experiences prepare_skill_evolution read_skill_experiences simplify_skill_experiences")},
	{Path: "skill-management", Description: "网络上技能搜索、安装与本地技能卸载。", DescriptionEn: "Search for skills online, install skills and uninstall local skills.", Tools: strings.Fields("install_skill search_skill uninstall_skill")},
	{Path: "terminal", Description: "通过 PowerShell 执行终端命令与脚本（git、npm、docker、python 等），支持超时与后台运行。", DescriptionEn: "Run terminal commands and scripts using PowerShell, with timeouts and background execution.", Tools: strings.Fields("powershell")},
	{Path: "user-todos", Description: "管理用户个人待办与日程：任务、会议、提醒、计划。", DescriptionEn: "Manage the user's personal tasks, meetings, reminders and plans.", Tools: strings.Fields("user_todos")},
	{Path: "worktree", Description: "创建或恢复隔离的 git worktree 并切换当前会话工作目录；退出时还原到原目录。", DescriptionEn: "Create or resume an isolated Git worktree, switch to it and restore the previous directory on exit.", Tools: strings.Fields("enter_worktree exit_worktree")},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CompactSchemaDescription 压缩列表描述；完整文档留在 get_schema。
// Python: _compact_schema_description（:336-342）——空白折叠，超限截断
// （上限-1 字符 + "…"）。
func CompactSchemaDescription(description string) string {
	compact := strings.Join(strings.Fields(description), " ")
	if len([]rune(compact)) <= schemaDescChars {
		return compact
	}
	runes := []rune(compact)
	return strings.TrimRight(string(runes[:schemaDescChars-1]), " \t\n") + "…"
}

// ToolIndexToolID 派生 session 级 tool_search 的稳定工具 id。
// Python: _tool_index_tool_id（:288-293）——sha256(domain + session_scope)
// hexdigest 前 16 字符。
func ToolIndexToolID(sessionScope string) string {
	sum := sha256Of(toolIDDomain + sessionScope)
	return sum[:16]
}

// ──────────────────────────── 构造与非导出辅助 ────────────────────────────

// NewToolRetrievalToolkit 构造工具包。Python: ToolRetrievalToolkit.__init__（:423-470）。
func NewToolRetrievalToolkit(
	deferredToolsProvider func() []schema.ToolInfoInterface,
	sessionScope string,
	configBase map[string]any,
	mcpServersProvider func() map[string]any,
	language string,
) *ToolRetrievalToolkit {
	t := &ToolRetrievalToolkit{
		deferredToolsProvider: deferredToolsProvider,
		sessionScope:          sessionScope,
		configBase:            configBase,
		mcpServersProvider:    mcpServersProvider,
		language:              prompts.ResolveLanguage(language),
		toolByName:            map[string]schema.ToolInfoInterface{},
	}
	// 构造期冻结：deferred 工具此刻常为空（注册在后），首次 before_model_call
	// 再冻结（对齐 Python 注释 :462-466）。
	t.frozenCatalogue = t.buildCatalogue()
	return t
}

// sha256Of 十六进制 sha256。
func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// liveRecords 从 provider 刷新 deferred 工具映射。
// Python: _live_records（:473-478）。
func (t *ToolRetrievalToolkit) liveRecords() {
	tools := []schema.ToolInfoInterface{}
	if t.deferredToolsProvider != nil {
		tools = t.deferredToolsProvider()
	}
	mapping := make(map[string]schema.ToolInfoInterface, len(tools))
	for _, info := range tools {
		mapping[info.GetName()] = info
	}
	t.toolByName = mapping
}

// buildCatalogue 对 live 注册表冻结静态表。
// Python: _build_catalogue（:481-513）——分类行是路由信号，只有成员 ≥2 的
// 分类才值得付目录行；单 live 成员分类折叠进未归类 [tool] 块。
func (t *ToolRetrievalToolkit) buildCatalogue() ToolCatalogueSnapshot {
	t.liveRecords()
	live := t.toolByName
	categories := []ToolCategory{}
	for _, category := range staticToolCategories {
		members := intersectNames(category.Tools, live)
		if len(members) >= 2 {
			categories = append(categories, ToolCategory{
				Path:          category.Path,
				Description:   category.Description,
				DescriptionEn: category.DescriptionEn,
				Tools:         members,
			})
		}
	}
	flatEntries := make([]FlatToolEntry, 0, len(live))
	for name, info := range live {
		flatEntries = append(flatEntries, FlatToolEntry{
			Name:        name,
			Description: DiscoveryDescription(info, t.language),
		})
	}
	sort.Slice(flatEntries, func(i, j int) bool { return flatEntries[i].Name < flatEntries[j].Name })
	return ToolCatalogueSnapshot{Categories: categories, FlatEntries: flatEntries}
}

// intersectNames 过滤出 live 注册表中存在的名字（保持表序）。
func intersectNames(names []string, live map[string]schema.ToolInfoInterface) []string {
	out := []string{}
	for _, name := range names {
		if _, ok := live[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// ──────────────────────────── 访问器 ────────────────────────────

// Language 运行时检索语言。
func (t *ToolRetrievalToolkit) Language() string { return t.language }

// SessionScope 会话唯一标识。
func (t *ToolRetrievalToolkit) SessionScope() string { return t.sessionScope }

// FrozenCatalogue 最近一次刷新冻结的目录。
// Python: frozen_catalogue property。
func (t *ToolRetrievalToolkit) FrozenCatalogue() ToolCatalogueSnapshot {
	return t.frozenCatalogue
}

// DeferredToolNames live deferred 工具名集合（读取时刷新）。
// Python: deferred_tool_names property——支撑 tool_call 执行信封的授权。
func (t *ToolRetrievalToolkit) DeferredToolNames() map[string]struct{} {
	t.liveRecords()
	out := make(map[string]struct{}, len(t.toolByName))
	for name := range t.toolByName {
		out[name] = struct{}{}
	}
	return out
}

// DeferredToolInfos live deferred ToolInfo 映射（读取时刷新）。
// Python: deferred_tool_infos——支撑 prompt rail 的目录漂移检测。
func (t *ToolRetrievalToolkit) DeferredToolInfos() map[string]schema.ToolInfoInterface {
	t.liveRecords()
	out := make(map[string]schema.ToolInfoInterface, len(t.toolByName))
	for name, info := range t.toolByName {
		out[name] = info
	}
	return out
}

// RefreshSnapshot 读取 deferred 工具并重新冻结目录。
// Python: refresh_snapshot（:582-592）——rail.init 与首次 before_model_call
// 各调用一次（注册表此后才完整）。
func (t *ToolRetrievalToolkit) RefreshSnapshot() {
	t.frozenCatalogue = t.buildCatalogue()
}

// McpConnectorGroups 按 connector server 分组 live deferred MCP 工具。
// Python: mcp_connector_groups（:539-600）——已知 server 名（最长优先）精确
// 拆分多下划线工具名前缀，拆不出退化取第一段；mcp_x 裸名不构成 server 限定。
func (t *ToolRetrievalToolkit) McpConnectorGroups() map[string]McpServerGroup {
	t.liveRecords()
	descriptions := map[string]McpServerDescription{}
	if t.mcpServersProvider != nil {
		raw := t.mcpServersProvider()
		for name, value := range raw {
			key := strings.TrimSpace(name)
			if key == "" {
				continue
			}
			switch v := value.(type) {
			case string:
				text := strings.TrimSpace(v)
				descriptions[key] = McpServerDescription{Zh: text, En: text}
			case McpServerDescription:
				descriptions[key] = McpServerDescription{Zh: strings.TrimSpace(v.Zh), En: strings.TrimSpace(v.En)}
			case map[string]any:
				descriptions[key] = McpServerDescription{
					Zh: strings.TrimSpace(stringOf(v["zh"])),
					En: strings.TrimSpace(stringOf(v["en"])),
				}
			}
		}
	}
	// 最长 server 名优先，保证 mcp_github_ci_create 先试 github_ci 再试 github。
	servers := make([]string, 0, len(descriptions))
	for name := range descriptions {
		servers = append(servers, name)
	}
	sort.Slice(servers, func(i, j int) bool { return len(servers[i]) > len(servers[j]) })
	groups := map[string][]string{}
	for name := range t.toolByName {
		if !strings.HasPrefix(name, "mcp_") {
			continue
		}
		rest := name[len("mcp_"):]
		server := ""
		for _, candidate := range servers {
			prefix := candidate + "_"
			if strings.HasPrefix(rest, prefix) && len(rest) > len(prefix) {
				server = candidate
				break
			}
		}
		if server == "" {
			first, restAfter, found := strings.Cut(rest, "_")
			if !found || restAfter == "" {
				continue // "mcp_x" 裸名不构成 server 限定
			}
			server = first
		}
		groups[server] = append(groups[server], name)
	}
	out := map[string]McpServerGroup{}
	for server, names := range groups {
		sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
		desc := descriptions[server]
		out[server] = McpServerGroup{Tools: names, DescriptionZh: desc.Zh, DescriptionEn: desc.En}
	}
	return out
}

// McpServerGroup 单个 connector 分组（Python: groups dict 值形态）。
type McpServerGroup struct {
	Tools         []string
	DescriptionZh string
	DescriptionEn string
}

// DescriptionFor 组描述按语言取值，双方互为回落。
func (g McpServerGroup) DescriptionFor(language string) string {
	zh, en := g.DescriptionZh, g.DescriptionEn
	if language == "en" {
		if en != "" {
			return en
		}
		return zh
	}
	if zh != "" {
		return zh
	}
	return en
}

// connectorServerDescription 组描述空兜底文案。
// Python: _render_connector_lines 兜底（:530-535）。
func connectorServerDescription(server string, language string) string {
	if language == "en" {
		return fmt.Sprintf("Tools from MCP connector %s.", server)
	}
	return fmt.Sprintf("MCP 连接器 %s 提供的工具。", server)
}

// ──────────────────────────── tool_search ────────────────────────────

// ToolSearch 服务一次目录操作（list / get_schema）。
// Python: tool_search（:617-700）——token 有界的两阶段流：list 返回分类下
// 工具名+简述（整类返回，无 schema、无分页）；get_schema 返回模型已选定
// 工具的完整参数 schema，供固定 tool_call 使用。退役字段防御性 pop，
// 旧快照长会话降级不报错。
func (t *ToolRetrievalToolkit) ToolSearch(kwargs map[string]any) (string, error) {
	// Python :636-651 退役字段 pop（call_goal/cursor/query/tools）。
	delete(kwargs, "call_goal")
	delete(kwargs, "cursor")
	hadRetiredQuery := false
	if _, ok := kwargs["query"]; ok {
		hadRetiredQuery = true
		delete(kwargs, "query")
	}
	toolName := stringOf(kwargs["tool"])
	delete(kwargs, "tool")
	if legacyTools, ok := kwargs["tools"].([]any); ok && toolName == "" && len(legacyTools) > 0 {
		toolName = stringOf(legacyTools[0]) // 旧数组拼法的直接调用者降级取首名
	}
	delete(kwargs, "tools")
	rawKeyword := kwargs["keyword"]
	if list, ok := rawKeyword.([]any); ok {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, stringOf(item))
		}
		rawKeyword = strings.Join(parts, " ")
	}
	keyword := strings.TrimSpace(stringOf(rawKeyword))
	delete(kwargs, "keyword")

	operation := strings.ToLower(strings.TrimSpace(stringOf(kwargs["operation"])))
	category := strings.TrimSpace(stringOf(kwargs["category"]))

	if operation == "get_schema" {
		return t.renderSchema(toolName)
	}
	if operation == "list" || operation == "search" || operation == "" {
		// "search" 是检索退役前冻结提示词的别名；空 operation（卡片必填
		// operation，仅直接调用者可达）降级为普通列表。
		if category == "" && keyword == "" {
			if operation == "list" || hadRetiredQuery {
				// Python :683-690 教学文案逐字符。
				return "", fmt.Errorf(
					"list needs \"category\" or \"keyword\": pick a category " +
						"from the catalogue in your system prompt, or pass " +
						"keyword for a global search. e.g. " +
						"{\"operation\":\"list\",\"category\":\"cron\"} or " +
						"{\"operation\":\"list\",\"keyword\":\"图片\"}")
			}
			return t.renderRootListing(), nil
		}
		return t.renderListing(category, keyword)
	}
	return "", fmt.Errorf(
		"tool_search supports (list, get_schema), not %q; e.g. "+
			"{\"operation\":\"list\",\"category\":\"cron\"} or "+
			"{\"operation\":\"get_schema\",\"tool\":\"cron_create_job\"}", operation)
}

// stringOf 任意值宽松转串（Python str() 等价的保守实现）。
func stringOf(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// renderListing 分派一次 list 操作。
// Python: _render_listing（:702-744）。
func (t *ToolRetrievalToolkit) renderListing(category string, keyword string) (string, error) {
	t.liveRecords()
	if category == "" {
		if keyword != "" {
			return t.renderKeywordSearchListing(keyword), nil
		}
		// 裸 list 报错教学（目录已在 system prompt，重复 dump 纯浪费）。
		return "", fmt.Errorf(
			"list needs \"category\" or \"keyword\": pick a category from " +
				"the catalogue in your system prompt, or pass keyword for a " +
				"global search. e.g. {\"operation\":\"list\",\"category\":" +
				"\"cron\"} or {\"operation\":\"list\",\"keyword\":\"图片\"}")
	}
	if server := t.matchConnectorCategory(category); server != "" {
		// 连接器分组只存在于虚拟分支；此处服务其 list 使目录行可导航。
		return t.renderConnectorListing(server, keyword), nil
	}
	return t.renderCategoryListing(category, keyword)
}

// renderRootListing 退役裸 search 拼法的根分类列表。
// Python: _render_root_listing（:746-802）——多成员分类渲染 [category] 行，
// 单成员折叠 [tool] 行；连接器恒 [category] 行（server 描述自带路由信号）。
func (t *ToolRetrievalToolkit) renderRootListing() string {
	lines := []string{"## Tool categories"}
	if t.language == "en" {
		lines = append(lines, "See the system prompt for the full catalogue; this listing supports legacy calls.")
	} else {
		lines = append(lines, "完整目录见 system prompt；本列表仅为旧拼写降级渲染。")
	}
	lines = append(lines, "")
	for _, category := range t.liveCategories() {
		description := strings.Join(strings.Fields(category.DescriptionFor(t.language)), " ")
		if len(category.Tools) >= 2 {
			lines = append(lines, fmt.Sprintf("- [category] %s: %s", category.Path, description))
			continue
		}
		name := category.Tools[0]
		info := t.toolByName[name]
		description = ""
		if info != nil {
			description = CompactSchemaDescription(DiscoveryDescription(info, t.language))
		}
		lines = append(lines, fmt.Sprintf("- [tool] %s: %s", name, description))
	}
	for server, group := range t.McpConnectorGroups() {
		description := group.DescriptionFor(t.language)
		if description == "" {
			description = connectorServerDescription(server, t.language)
		}
		lines = append(lines, fmt.Sprintf("- [category] mcp_%s: %s", server, description))
	}
	return strings.Join(lines, "\n")
}

// rankToolsByQuery BM25 排序工具（引擎索引语义）。
// Python: _rank_tools_by_query（:751-800）——BM25ToolIndex.search 带来排序
// （精确名优先）、score>0 阈值与平局裁决；语料是单分类或 live 注册表，每次
// 调用重建。只与查询共享单字符 token 的结果丢弃（CJK unigram 噪声）。
func (t *ToolRetrievalToolkit) rankToolsByQuery(names []string, keyword string) []string {
	documents := []schema.ToolInfoInterface{}
	extraSearchTexts := []string{}
	for _, name := range names {
		info := t.toolByName[name]
		if info == nil {
			continue
		}
		documents = append(documents, info)
		extraSearchTexts = append(extraSearchTexts, DiscoverySearchText(info))
	}
	if len(documents) == 0 {
		return []string{}
	}
	ranked := tool_discovery.BuildBM25ToolIndex(documents, extraSearchTexts).Search(keyword, len(documents))
	multiChar := map[string]struct{}{}
	for _, token := range recall.Tokenize(keyword) {
		if len([]rune(token)) > 1 {
			multiChar[token] = struct{}{}
		}
	}
	selected := []string{}
	for _, info := range ranked {
		tokens := map[string]struct{}{}
		for _, token := range recall.Tokenize(
			fmt.Sprintf("%s %s %s", info.GetName(), info.GetDescription(), DiscoverySearchText(info))) {
			tokens[token] = struct{}{}
		}
		for token := range multiChar {
			if _, ok := tokens[token]; ok {
				selected = append(selected, info.GetName())
				break
			}
		}
	}
	return selected
}

// liveCategories 成员与 live 注册表相交的静态分类。
// Python: _live_categories（:802-812）。
func (t *ToolRetrievalToolkit) liveCategories() []ToolCategory {
	live := t.toolByName
	out := []ToolCategory{}
	for _, category := range staticToolCategories {
		members := intersectNames(category.Tools, live)
		if len(members) > 0 {
			out = append(out, ToolCategory{
				Path:          category.Path,
				Description:   category.Description,
				DescriptionEn: category.DescriptionEn,
				Tools:         members,
			})
		}
	}
	return out
}

// renderKeywordSearchListing 全量 deferred 工具的 BM25 搜索（根+keyword）。
// Python: _render_keyword_search_listing（:815-838）——覆盖已归类、未归类与
// MCP 连接器工具；名字先排序保证平局裁决与注册顺序无关。
func (t *ToolRetrievalToolkit) renderKeywordSearchListing(keyword string) string {
	names := make([]string, 0, len(t.toolByName))
	for name := range t.toolByName {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	ranked := t.rankToolsByQuery(names, keyword)
	header := fmt.Sprintf("## Tools (keyword: %q)", keyword)
	if len(ranked) == 0 {
		return fmt.Sprintf("%s\n\nNo tools match keyword %q (0 of %d)", header, keyword, len(names))
	}
	lines := []string{header, ""}
	for _, name := range ranked {
		lines = append(lines, fmt.Sprintf("- [tool] %s: %s", name,
			CompactSchemaDescription(DiscoveryDescription(t.toolByName[name], t.language))))
	}
	return strings.Join(lines, "\n")
}

// renderCategoryListing 渲染单分类成员。
// Python: _render_category_listing（:841-886）——名字命中列出该分类工具
// （keyword 给定时 BM25 排序）；未命中抛错附有效分类名便于重试。
func (t *ToolRetrievalToolkit) renderCategoryListing(category string, keyword string) (string, error) {
	requested := strings.ToLower(strings.TrimSpace(category))
	for _, candidate := range t.liveCategories() {
		if strings.ToLower(strings.TrimSpace(candidate.Path)) != requested {
			continue
		}
		names := candidate.Tools
		header := fmt.Sprintf("## Tools (category: %s)", candidate.Path)
		if keyword != "" {
			names = t.rankToolsByQuery(candidate.Tools, keyword)
			if len(names) == 0 {
				return fmt.Sprintf("## Tools (category: %s, keyword: %q)\n\nNo tools match keyword %q (0 of %d)",
					candidate.Path, keyword, keyword, len(candidate.Tools)), nil
			}
			header = fmt.Sprintf("## Tools (category: %s, keyword: %q)", candidate.Path, keyword)
		}
		lines := []string{header, ""}
		for _, name := range names {
			lines = append(lines, fmt.Sprintf("- [tool] %s: %s", name,
				CompactSchemaDescription(DiscoveryDescription(t.toolByName[name], t.language))))
		}
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("list: %q is not a catalogue category. Valid: %s. Or omit category and pass keyword for a global search.",
		category, strings.Join(t.validCategoryNames(), ", "))
}

// validCategoryNames 有效分类名清单（静态 + mcp_{server} 虚拟分支）。
// Python: _valid_category_names（:888-893）。
func (t *ToolRetrievalToolkit) validCategoryNames() []string {
	names := []string{}
	for _, category := range t.liveCategories() {
		names = append(names, category.Path)
	}
	for server := range t.McpConnectorGroups() {
		names = append(names, "mcp_"+server)
	}
	return names
}

// matchConnectorCategory list 分类名对 MCP 连接器分组的解析。
// Python: _match_connector_category（:896-906）。
func (t *ToolRetrievalToolkit) matchConnectorCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return ""
	}
	for server := range t.McpConnectorGroups() {
		if category == "mcp_"+server || category == server {
			return server
		}
	}
	return ""
}

// renderConnectorListing 标准列表格式渲染单连接器分组。
// Python: _render_connector_listing（:909-942）。
func (t *ToolRetrievalToolkit) renderConnectorListing(server string, keyword string) string {
	group := t.McpConnectorGroups()[server]
	memberNames := []string{}
	for _, name := range group.Tools {
		if _, ok := t.toolByName[name]; ok {
			memberNames = append(memberNames, name)
		}
	}
	header := fmt.Sprintf("## Tools (MCP connector: %s)", server)
	if keyword != "" {
		ranked := t.rankToolsByQuery(memberNames, keyword)
		if len(ranked) == 0 {
			return fmt.Sprintf("## Tools (MCP connector: %s, keyword: %q)\n\nNo tools match keyword %q (0 of %d)",
				server, keyword, keyword, len(memberNames))
		}
		memberNames = ranked
		header = fmt.Sprintf("## Tools (MCP connector: %s, keyword: %q)", server, keyword)
	}
	lines := []string{header}
	if description := group.DescriptionFor(t.language); description != "" {
		lines = append(lines, "", description)
	}
	lines = append(lines, "")
	for _, name := range memberNames {
		description := ""
		if info := t.toolByName[name]; info != nil {
			description = info.GetDescription()
		}
		lines = append(lines, fmt.Sprintf("- [tool] %s: %s", name, description))
	}
	return strings.Join(lines, "\n")
}

// ──────────────────────────── schema 渲染 ────────────────────────────

// renderSchema 返回单个 deferred 工具的 get_schema JSON 记录。
// Python: _render_schema（:945-985）。
func (t *ToolRetrievalToolkit) renderSchema(toolName string) (string, error) {
	if toolName == "" {
		return "", fmt.Errorf("get_schema requires \"tool\": the exact tool name whose schema you need, " +
			"e.g. {\"operation\":\"get_schema\",\"tool\":\"cron_create_job\"}")
	}
	t.liveRecords()
	if _, ok := t.toolByName[toolName]; !ok {
		return "", fmt.Errorf("get_schema: %q is not in the deferred catalogue. Use list to find the exact name.", toolName)
	}
	records := t.schemasForMentioned("", 0, false, []string{toolName})
	record := map[string]any{"name": toolName, "description": "", "parameters": map[string]any{}}
	if len(records) > 0 {
		record = records[0]
	}
	payload := map[string]any{
		"operation":   "get_schema",
		"name":        record["name"],
		"description": record["description"],
		"schema":      record["parameters"],
		"usage": ("Execute via the fixed `tool_call` tool: name=<exact tool " +
			"name>, args=<this schema's parameters object>. " +
			"object-typed parameters must be JSON object literals inside " +
			"args, never JSON strings; keep field names and casing " +
			"exactly as in the schema."),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// schemasForMentioned 把工具名解析为全 schema 描述符。
// Python: _schemas_for_mentioned（:988-1028）——requested（get_schema 的
// tool 参数，包成列表）权威：无文本解析、无 cap，模型显式点名。
func (t *ToolRetrievalToolkit) schemasForMentioned(content string, cap int, parseNames bool, requested []string) []map[string]any {
	names := []string{}
	if len(requested) > 0 {
		for _, name := range requested {
			if _, ok := t.toolByName[name]; ok {
				names = append(names, name)
			}
		}
	} else if parseNames {
		for name := range t.toolByName {
			if strings.Contains(content, "[tool] "+name+" ") || strings.Contains(content, "`"+name+"`") {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return []map[string]any{}
	}
	schemas := []map[string]any{}
	seen := map[string]struct{}{}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		info := t.toolByName[name]
		if info == nil {
			continue
		}
		// 完整描述逐字保留：工具描述里嵌入的使用规则（cron 表达式约束、
		// 必须省略的字段、投递渠道策略）是模型正确填 args 所需。
		schemas = append(schemas, map[string]any{
			"name":        info.GetName(),
			"description": info.GetDescription(),
			"parameters":  anchoredParameters(info.GetParameters()),
		})
	}
	if cap > 0 && len(schemas) > cap {
		return []map[string]any{}
	}
	return schemas
}

// anchoredParameters 给 object 型参数附加直调序列化规则（呈现层注解，
// 工具真实 schema 不动）。Python: _anchored_parameters（:345-370）。
func anchoredParameters(parameters any) any {
	raw, ok := parameters.(map[string]any)
	if !ok {
		return parameters
	}
	// 深拷贝（json round-trip 对齐 Python json.loads(json.dumps(...))）。
	data, err := json.Marshal(raw)
	if err != nil {
		return parameters
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return parameters
	}
	walkObjectParams(cloned)
	return cloned
}

// walkObjectParams 递归标注 object 型属性。Python: _walk（:352-367）。
func walkObjectParams(node map[string]any) {
	properties, ok := node["properties"].(map[string]any)
	if !ok {
		return
	}
	for _, value := range properties {
		prop, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if isObjectTyped(prop["type"]) {
			base := ""
			if existing, ok := prop["description"].(string); ok {
				base = strings.TrimRight(existing, " \t\n")
			}
			prop["description"] = strings.TrimLeft(base+objectParamAnchor, " \t\n")
		}
		walkObjectParams(prop)
	}
}

// isObjectTyped type 字段含 "object"（单串或数组）。
func isObjectTyped(value any) bool {
	switch v := value.(type) {
	case string:
		return v == "object"
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "object" {
				return true
			}
		}
	}
	return false
}

// ──────────────────────────── tool_search 工具卡 ────────────────────────────

// GetTools 返回 session 级 tool_search 结构化工具。
// Python: get_tools（:1031-1043）。
func (t *ToolRetrievalToolkit) GetTools() []tool.Tool {
	card := tool.NewToolCardWithID(
		ToolIndexToolID(t.sessionScope),
		ToolSearchToolName,
		toolSearchCardDescription(t.language),
		toolSearchInputParams(t.language),
		nil,
	)
	return []tool.Tool{&toolSearchTool{card: card, toolkit: t}}
}

// toolSearchCardDescription 按语言取卡描述。
func toolSearchCardDescription(language string) string {
	if language == "en" {
		return toolSearchCardDescriptionEn
	}
	return toolSearchCardDescriptionZh
}

// Card 工具卡片。
func (s *toolSearchTool) Card() *tool.ToolCard { return s.card }

// Invoke 一次性执行 tool_search。
func (s *toolSearchTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	text, err := s.toolkit.ToolSearch(inputs)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{
		"success": true,
		// 模型可见文本键（tool_discovery.mappedContentKey 同契约，包外不可见故用字面量）
		"__mapped_content": text,
	}, nil
}

// Stream 不支持流式。
func (s *toolSearchTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// GetCallTimeout 沿用默认外层预算。
func (s *toolSearchTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// toolSearchInputParams tool_search 卡 schema。
// Python: _tool_search_input_params（:345 段；zh/en 双语描述切换）。
func toolSearchInputParams(language string) []*schema.Param {
	descriptions := map[string]string{
		"operation": "list: tools under [category], optionally filtered by [keyword]. get_schema: the schema of the tool named in [tool].",
		"category":  "Exact category name. Omit only for a keyword-only global search.",
		"keyword":   "list only. BM25 keyword search: within [category], or across all tools when category is omitted. Built-in tools accept Chinese or English capability terms. Provider descriptions may use another language.",
		"tool":      "get_schema only. One exact tool name.",
	}
	if language != "en" {
		descriptions = map[string]string{
			"operation": "list 列出 category 下的工具，可用 keyword 过滤；get_schema 获取 tool 指定工具的 schema。",
			"category":  "准确的分类名称；仅在 keyword 全局检索时省略。",
			"keyword":   "仅用于 list。按工具名称、描述和参数做 BM25 过滤；内置工具支持中英文能力词。",
			"tool":      "仅用于 get_schema，填写一个准确的工具名称。",
		}
	}
	return []*schema.Param{
		{Name: "operation", Type: schema.ParamTypeString, Required: true, Description: descriptions["operation"], Enum: []any{"list", "get_schema"}},
		{Name: "category", Type: schema.ParamTypeString, Required: false, Description: descriptions["category"]},
		{Name: "keyword", Type: schema.ParamTypeString, Required: false, Description: descriptions["keyword"]},
		{Name: "tool", Type: schema.ParamTypeString, Required: false, Description: descriptions["tool"]},
	}
}
