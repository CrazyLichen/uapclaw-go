// Package tools 提供检索类只读工具的目录与工具包实现。
//
// 本文件对应 Python tool_discovery_catalog.py（187 行）：内置工具的
// 语言无关检索投影元数据（display_en + 中文别名），供 tool_search 的
// BM25 检索与双语目录渲染使用；注册卡仍是调用说明与 schema 的权威。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/tool_discovery_catalog.py
package tools

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	mcpTypes "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolDiscoveryMetadata 内置工具的检索投影文本（仅用于展示与检索）。
// Python: ToolDiscoveryMetadata dataclass。
type ToolDiscoveryMetadata struct {
	// DisplayEn 英文目录显示描述
	DisplayEn string
	// AliasesZh 中文检索别名
	AliasesZh []string
}

// ──────────────────────────── 常量 ────────────────────────────

// ProgressiveDiscoveryProperty ToolCard.Properties 中渐进披露元数据的键名。
// Python: PROGRESSIVE_DISCOVERY_PROPERTY。
const ProgressiveDiscoveryProperty = "progressive_discovery"

// ──────────────────────────── 全局变量 ────────────────────────────

// builtinToolDiscovery 内置工具的静态检索投影表。
// Python: BUILTIN_TOOL_DISCOVERY（93 条逐条复刻，禁止裁剪条目）。
var builtinToolDiscovery = map[string]ToolDiscoveryMetadata{
	"coding_memory_edit":             {DisplayEn: "Edit an exact passage in project coding memory.", AliasesZh: strings.Fields("编辑 修改 编码记忆 项目记忆")},
	"coding_memory_read":             {DisplayEn: "Read persistent project coding memory.", AliasesZh: strings.Fields("读取 查看 编码记忆 项目记忆")},
	"coding_memory_write":            {DisplayEn: "Write facts, decisions, and lessons to project coding memory.", AliasesZh: strings.Fields("写入 记录 编码记忆 项目记忆")},
	"read_pdf":                       {DisplayEn: "Read PDF text by page or in chunks.", AliasesZh: strings.Fields("读取 查看 PDF 文档 分页")},
	"acp_chat":                       {DisplayEn: "Exchange messages with an external ACP-compatible coding agent.", AliasesZh: strings.Fields("外部智能体 ACP 对话 Codex Gemini")},
	"list_files":                     {DisplayEn: "List files and directories in a local path.", AliasesZh: strings.Fields("列出 查看 文件 目录 文件夹")},
	"get_current_goal":               {DisplayEn: "Inspect the current persistent goal and its status.", AliasesZh: strings.Fields("查看 查询 当前目标 持续目标 状态")},
	"submit_goal_report":             {DisplayEn: "Report progress, completion, or a blocker for the current goal.", AliasesZh: strings.Fields("提交 汇报 目标进度 完成 受阻")},
	"image_ocr":                      {DisplayEn: "Extract text from an image with OCR.", AliasesZh: strings.Fields("图片识字 OCR 提取文字")},
	"visual_question_answering":      {DisplayEn: "Answer questions about an image.", AliasesZh: strings.Fields("图片问答 看图 识图 分析图片")},
	"audio_metadata":                 {DisplayEn: "Read metadata from an audio file.", AliasesZh: strings.Fields("音频元数据 时长 格式")},
	"audio_question_answering":       {DisplayEn: "Answer questions about audio content.", AliasesZh: strings.Fields("音频问答 分析音频")},
	"audio_transcription":            {DisplayEn: "Transcribe speech in an audio file to text.", AliasesZh: strings.Fields("音频转写 语音转文字 听写")},
	"video_understanding":            {DisplayEn: "Analyze video content and answer questions about it.", AliasesZh: strings.Fields("视频理解 分析视频 视频问答")},
	"generate_image":                 {DisplayEn: "Generate an image from a text prompt.", AliasesZh: strings.Fields("生成图片 文生图 画图")},
	"generate_visual":                {DisplayEn: "Generate a visual artifact from a text prompt.", AliasesZh: strings.Fields("生成视觉图 画图 可视化")},
	"generate_video":                 {DisplayEn: "Start a video generation task from a text prompt.", AliasesZh: strings.Fields("生成视频 文生视频")},
	"check_video_status":             {DisplayEn: "Check a video generation task and download its result.", AliasesZh: strings.Fields("查询 视频生成 进度 下载结果")},
	"lsp":                            {DisplayEn: "Use a language server for definitions, references, and diagnostics.", AliasesZh: strings.Fields("代码定义 查找引用 代码诊断 LSP")},
	"switch_mode":                    {DisplayEn: "Switch the current session between normal and plan modes.", AliasesZh: strings.Fields("切换模式 计划模式 普通模式")},
	"cron":                           {DisplayEn: "Manage standalone scheduled tasks and reminders.", AliasesZh: strings.Fields("定时任务 提醒 闹钟 周期通知")},
	"cron_create_job":                {DisplayEn: "Create a standalone scheduled task, one-time reminder, daily notification, or periodic notification.", AliasesZh: strings.Fields("创建 定时任务 提醒我 一次性提醒 定时提醒 通知我 定时通知 闹钟 周期通知")},
	"cron_delete_job":                {DisplayEn: "Delete a standalone scheduled task or reminder.", AliasesZh: strings.Fields("删除 取消 定时任务 提醒 闹钟")},
	"cron_get_job":                   {DisplayEn: "Get one standalone scheduled task or reminder.", AliasesZh: strings.Fields("查看 查询 单个 定时任务 提醒")},
	"cron_list_jobs":                 {DisplayEn: "List standalone scheduled tasks and reminders.", AliasesZh: strings.Fields("列出 查看 全部 定时任务 提醒")},
	"cron_preview_job":               {DisplayEn: "Preview upcoming run times for a scheduled task.", AliasesZh: strings.Fields("预览 定时任务 下次执行时间")},
	"cron_toggle_job":                {DisplayEn: "Enable or disable a scheduled task.", AliasesZh: strings.Fields("启用 禁用 暂停 恢复 定时任务")},
	"cron_update_job":                {DisplayEn: "Update a standalone scheduled task or reminder.", AliasesZh: strings.Fields("修改 更新 定时任务 提醒")},
	"heartbeat_cancel_run":           {DisplayEn: "Cancel a running session follow-up.", AliasesZh: strings.Fields("取消 正在运行 心跳 跟进任务")},
	"heartbeat_create_job":           {DisplayEn: "Resume this session's existing task later; use cron_create_job for independent schedules.", AliasesZh: strings.Fields("创建 心跳 跟进任务 稍后继续 检查进展")},
	"heartbeat_delete_job":           {DisplayEn: "Delete a session follow-up schedule.", AliasesZh: strings.Fields("删除 取消 心跳 跟进任务")},
	"heartbeat_get_job":              {DisplayEn: "Get one session follow-up schedule.", AliasesZh: strings.Fields("查看 查询 单个 心跳 跟进任务")},
	"heartbeat_list_jobs":            {DisplayEn: "List session follow-up schedules.", AliasesZh: strings.Fields("列出 查看 心跳 跟进任务")},
	"heartbeat_preview_job":          {DisplayEn: "Preview upcoming times for a session follow-up.", AliasesZh: strings.Fields("预览 心跳 跟进任务 下次时间")},
	"heartbeat_run_now":              {DisplayEn: "Run a session follow-up immediately.", AliasesZh: strings.Fields("立即运行 马上执行 心跳 跟进任务")},
	"heartbeat_toggle_job":           {DisplayEn: "Enable or disable a session follow-up.", AliasesZh: strings.Fields("启用 禁用 暂停 恢复 心跳 跟进任务")},
	"heartbeat_update_job":           {DisplayEn: "Update a session follow-up schedule.", AliasesZh: strings.Fields("修改 更新 心跳 跟进任务")},
	"session_continue_queued":        {DisplayEn: "Resume queued work in another persistent session.", AliasesZh: strings.Fields("继续 恢复 其他会话 排队任务")},
	"session_list":                   {DisplayEn: "List other persistent sessions and their status.", AliasesZh: strings.Fields("列出 查看 其他会话 会话状态")},
	"session_message_list":           {DisplayEn: "Inspect cross-session messages and delivery status.", AliasesZh: strings.Fields("查看 跨会话消息 投递状态 消息列表")},
	"session_message_resolve":        {DisplayEn: "Resolve an unknown cross-session message state.", AliasesZh: strings.Fields("解决 跨会话消息 未知消息状态")},
	"session_read":                   {DisplayEn: "Read another session's conversation, progress, and results.", AliasesZh: strings.Fields("读取 查看 其他会话 对话 进展 结果")},
	"session_send_message":           {DisplayEn: "Send a message to another persistent session.", AliasesZh: strings.Fields("发送消息 通知 其他会话 跨会话")},
	"evolve_review_task":             {DisplayEn: "Create a review task for a local skill evolution proposal.", AliasesZh: strings.Fields("创建 技能演进 评审任务")},
	"evolve_skill_experiences":       {DisplayEn: "Run a constrained review of skill experience changes.", AliasesZh: strings.Fields("执行 技能经验 演进 评审")},
	"list_skill_experiences":         {DisplayEn: "List recorded skill experiences.", AliasesZh: strings.Fields("列出 查看 技能经验")},
	"prepare_skill_evolution":        {DisplayEn: "Prepare a skill evolution proposal for review.", AliasesZh: strings.Fields("准备 技能演进 提案")},
	"read_skill_experiences":         {DisplayEn: "Read recorded skill experiences.", AliasesZh: strings.Fields("读取 技能经验")},
	"simplify_skill_experiences":     {DisplayEn: "Simplify and consolidate recorded skill experiences.", AliasesZh: strings.Fields("整理 精简 合并 技能经验")},
	"install_skill":                  {DisplayEn: "Install a skill from the online skill registry.", AliasesZh: strings.Fields("安装 技能")},
	"search_skill":                   {DisplayEn: "Search the online skill registry.", AliasesZh: strings.Fields("搜索 查找 找技能 找一个技能 技能")},
	"uninstall_skill":                {DisplayEn: "Uninstall a local skill.", AliasesZh: strings.Fields("卸载 删除 技能")},
	"powershell":                     {DisplayEn: "Run terminal commands and scripts with PowerShell.", AliasesZh: strings.Fields("终端 命令行 执行命令 PowerShell 脚本")},
	"user_todos":                     {DisplayEn: "Manage the user's personal tasks, meetings, reminders, and plans.", AliasesZh: strings.Fields("待办 日程 会议 提醒 计划")},
	"enter_worktree":                 {DisplayEn: "Create or resume an isolated Git worktree and enter it.", AliasesZh: strings.Fields("创建 进入 恢复 Git worktree 工作树 隔离分支")},
	"exit_worktree":                  {DisplayEn: "Exit the current Git worktree and restore the previous directory.", AliasesZh: strings.Fields("退出 Git worktree 工作树 恢复目录")},
	"read_text_file":                 {DisplayEn: "Read a local file through the IDE ACP connection.", AliasesZh: strings.Fields("读取 文件 IDE ACP")},
	"write_text_file":                {DisplayEn: "Write a local file through the IDE ACP connection.", AliasesZh: strings.Fields("写入 修改 文件 IDE ACP")},
	"create_terminal":                {DisplayEn: "Create an IDE terminal and execute a command through ACP.", AliasesZh: strings.Fields("创建 终端 执行命令 IDE ACP")},
	"read_terminal_output":           {DisplayEn: "Read output from an IDE terminal.", AliasesZh: strings.Fields("读取 查看 终端输出 IDE")},
	"wait_for_terminal_exit":         {DisplayEn: "Wait for an IDE terminal command to finish.", AliasesZh: strings.Fields("等待 终端命令 完成 退出")},
	"release_terminal":               {DisplayEn: "Release an IDE terminal after its command finishes.", AliasesZh: strings.Fields("释放 关闭 终端")},
	"memory_search":                  {DisplayEn: "Search long-term memory for prior work and decisions.", AliasesZh: strings.Fields("搜索 长期记忆 历史工作 决策 偏好")},
	"session_new":                    {DisplayEn: "Start asynchronous subagent sessions for a list of tasks.", AliasesZh: strings.Fields("创建 启动 子智能体 子任务 会话")},
	"session_cancel":                 {DisplayEn: "Cancel a running subagent session.", AliasesZh: strings.Fields("取消 停止 子智能体 子任务 会话")},
	"send_file_to_user":              {DisplayEn: "Deliver a generated file or report to the user.", AliasesZh: strings.Fields("发送 交付 文件 报告 给用户")},
	"create_alarm":                   {DisplayEn: "Create an alarm on the user's phone.", AliasesZh: strings.Fields("创建 手机闹钟 提醒")},
	"search_alarms":                  {DisplayEn: "Search alarms on the user's phone.", AliasesZh: strings.Fields("搜索 查找 手机闹钟")},
	"modify_alarm":                   {DisplayEn: "Modify an alarm on the user's phone.", AliasesZh: strings.Fields("修改 手机闹钟")},
	"delete_alarm":                   {DisplayEn: "Delete alarms on the user's phone.", AliasesZh: strings.Fields("删除 取消 手机闹钟")},
	"create_calendar_event":          {DisplayEn: "Create an event in the user's phone calendar.", AliasesZh: strings.Fields("创建 手机日历 日程 会议")},
	"search_calendar_event":          {DisplayEn: "Search events in the user's phone calendar.", AliasesZh: strings.Fields("搜索 查找 手机日历 日程 会议")},
	"search_contact":                 {DisplayEn: "Search the user's phone contacts.", AliasesZh: strings.Fields("搜索 查找 手机联系人 通讯录")},
	"search_file":                    {DisplayEn: "Search files on the user's phone.", AliasesZh: strings.Fields("搜索 查找 手机文件")},
	"upload_file":                    {DisplayEn: "Upload a file from the user's phone.", AliasesZh: strings.Fields("上传 手机文件")},
	"image_reading":                  {DisplayEn: "Understand an image supplied by the user.", AliasesZh: strings.Fields("看图 识图 分析图片")},
	"get_user_location":              {DisplayEn: "Get the user's current geographical location.", AliasesZh: strings.Fields("获取 查询 当前位置 定位")},
	"send_message":                   {DisplayEn: "Send an SMS message through the user's phone.", AliasesZh: strings.Fields("发送 短信 手机消息")},
	"search_message":                 {DisplayEn: "Search text messages on the user's phone.", AliasesZh: strings.Fields("搜索 查找 手机短信 消息")},
	"create_note":                    {DisplayEn: "Create a note on the user's phone.", AliasesZh: strings.Fields("创建 手机笔记 备忘录")},
	"search_notes":                   {DisplayEn: "Search notes on the user's phone.", AliasesZh: strings.Fields("搜索 查找 手机笔记 备忘录")},
	"modify_note":                    {DisplayEn: "Append content to a note on the user's phone.", AliasesZh: strings.Fields("修改 追加 手机笔记 备忘录")},
	"call_phone":                     {DisplayEn: "Make a phone call using the user's phone.", AliasesZh: strings.Fields("拨打 电话 手机通话")},
	"search_photo_gallery":           {DisplayEn: "Search the user's phone photo gallery.", AliasesZh: strings.Fields("搜索 查找 手机相册 照片")},
	"upload_photo":                   {DisplayEn: "Upload a photo from the user's phone gallery.", AliasesZh: strings.Fields("上传 手机相册 照片")},
	"view_push_result":               {DisplayEn: "Read scheduled-task results or pushed notifications.", AliasesZh: strings.Fields("查看 推送通知 定时任务结果")},
	"save_media_to_gallery":          {DisplayEn: "Save an image or video to the user's phone gallery.", AliasesZh: strings.Fields("保存 图片 视频 到手机相册")},
	"save_file_to_file_manager":      {DisplayEn: "Save a file to the user's phone file manager.", AliasesZh: strings.Fields("保存 文件 到手机 文件管理")},
	"convert_timestamp_to_utc8_time": {DisplayEn: "Convert a timestamp to UTC+8 time.", AliasesZh: strings.Fields("转换 时间戳 北京时间 UTC8")},
	"query_collection":               {DisplayEn: "Search the user's saved knowledge collections.", AliasesZh: strings.Fields("搜索 查询 收藏 知识库")},
	"delete_collection":              {DisplayEn: "Delete items from the user's saved knowledge collections.", AliasesZh: strings.Fields("删除 收藏 知识")},
	"add_collection":                 {DisplayEn: "Save public knowledge to the user's personal collection.", AliasesZh: strings.Fields("添加 保存 收藏 知识")},
	"xiaoyi_gui_agent":               {DisplayEn: "Operate phone apps through screen interaction.", AliasesZh: strings.Fields("操作 手机应用 点击 屏幕 GUI")},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DiscoveryMetadata 返回单个内置名字的检索投影元数据；未知返回 nil。
// Python: discovery_metadata。
func DiscoveryMetadata(name string) *ToolDiscoveryMetadata {
	metadata, ok := builtinToolDiscovery[strings.TrimSpace(name)]
	if !ok {
		return nil
	}
	return &metadata
}

// DiscoveryDescription 返回本地化目录摘要，不修改投影对象。
// Python: discovery_description（:120-133）——zh（及一切非 en）一律返回
// 原始描述；en 且命中内置表返回 display_en；McpToolInfo 恒返回原始描述。
func DiscoveryDescription(info schema.ToolInfoInterface, language string) string {
	raw := info.GetDescription()
	if isMcpToolInfo(info) || language != "en" {
		return raw
	}
	metadata := DiscoveryMetadata(info.GetName())
	if metadata == nil {
		return raw
	}
	return metadata.DisplayEn
}

// DiscoverySearchText 返回已知内置工具的隐藏双语检索词。
// Python: discovery_search_text（:136-146）——McpToolInfo 恒空串；未知工具空串。
func DiscoverySearchText(info schema.ToolInfoInterface) string {
	if isMcpToolInfo(info) {
		return ""
	}
	metadata := DiscoveryMetadata(info.GetName())
	if metadata == nil {
		return ""
	}
	return strings.Join(append([]string{metadata.DisplayEn}, metadata.AliasesZh...), " ")
}

// ApplyDiscoveryMetadata 为内置 ToolCard 附加渐进披露元数据属性。
// Python: apply_discovery_metadata（:149-163）——MCP 卡与未知工具原样返回。
func ApplyDiscoveryMetadata(card *tool.ToolCard) *tool.ToolCard {
	if _, ok := any(card).(*mcpTypes.McpToolCard); ok {
		return card
	}
	metadata := DiscoveryMetadata(card.GetName())
	if metadata == nil {
		return card
	}
	properties := card.Properties
	if properties == nil {
		properties = make(map[string]any)
	}
	aliases := make([]string, len(metadata.AliasesZh))
	copy(aliases, metadata.AliasesZh)
	properties[ProgressiveDiscoveryProperty] = map[string]any{
		"display": map[string]any{"en": metadata.DisplayEn},
		"aliases": aliases,
	}
	card.Properties = properties
	return card
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isMcpToolInfo 类型断言判定 MCP 投影（Python isinstance McpToolInfo 等价物）。
func isMcpToolInfo(info schema.ToolInfoInterface) bool {
	if info == nil {
		return false
	}
	_, ok := info.(*schema.McpToolInfo)
	return ok
}
