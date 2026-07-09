package command_parser

import (
	"fmt"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ParsedChannelControl 受控通道用户整行文本解析结果（与 message_handler 原语义一致）
type ParsedChannelControl struct {
	// Action 解析结果动作
	Action ParsedControlAction
	// ModeSubcommand mode_ok 时为 agent|code|team|agent.plan|agent.fast|code.plan|code.normal 之一
	ModeSubcommand string
	// SwitchSubcommand switch_ok 时为 plan|fast|normal 之一
	SwitchSubcommand string
	// BranchName branch_ok 时为用户指定的分支名称（可为空字符串）
	BranchName string
	// RewindTurn rewind_ok/rewind_confirm 时为用户指定的回退轮次编号；0 表示未指定
	RewindTurn int
	// RewindPendingTurn rewind_ok 时记录原始轮次编号，用于 confirm/cancel 两步确认
	RewindPendingTurn int
}

// SlashCommandEntry 第一批命令注册表条目
type SlashCommandEntry struct {
	// ID 命令标识
	ID string
	// CanonicalText 规范文本
	CanonicalText string
	// Scope 作用域（gateway 或 client）
	Scope string
	// ReqMethod 对应的请求方法
	ReqMethod string
	// Notes 说明
	Notes string
}

// ──────────────────────────── 枚举 ────────────────────────────

// GatewaySlashCommand Gateway 当前支持解析的受控通道 slash 指令（A 类）
type GatewaySlashCommand int

const (
	// SlashNewSession 新建会话
	SlashNewSession GatewaySlashCommand = iota
	// SlashMode 模式切换
	SlashMode
	// SlashSwitch 切换
	SlashSwitch
	// SlashSkills 技能
	SlashSkills
	// SlashSkillsList 技能列表
	SlashSkillsList
	// SlashBranch 分支
	SlashBranch
	// SlashRewind 回退
	SlashRewind
)

// ModeSubcommand /mode 支持的子命令
type ModeSubcommand int

const (
	// ModeAgent 代理模式
	ModeAgent ModeSubcommand = iota
	// ModeCode 代码模式
	ModeCode
	// ModeTeam 团队模式
	ModeTeam
	// ModeAgentPlan 代理规划模式
	ModeAgentPlan
	// ModeAgentFast 代理快速模式
	ModeAgentFast
	// ModeCodePlan 代码规划模式
	ModeCodePlan
	// ModeCodeNormal 代码普通模式
	ModeCodeNormal
	// ModeCodeTeam 代码团队模式
	ModeCodeTeam
)

// SwitchSubcommand /switch 支持的子命令
type SwitchSubcommand int

const (
	// SwitchPlan 规划
	SwitchPlan SwitchSubcommand = iota
	// SwitchFast 快速
	SwitchFast
	// SwitchNormal 普通
	SwitchNormal
	// SwitchTeam 团队
	SwitchTeam
)

// ParsedControlAction parseChannelControlText 的判定结果
type ParsedControlAction int

const (
	// ActionNone 无控制动作
	ActionNone ParsedControlAction = iota
	// ActionNewSessionOK new_session 合法
	ActionNewSessionOK
	// ActionNewSessionBad new_session 非法（带后缀）
	ActionNewSessionBad
	// ActionModeOK mode 合法
	ActionModeOK
	// ActionModeBad mode 非法
	ActionModeBad
	// ActionSwitchOK switch 合法
	ActionSwitchOK
	// ActionSwitchBad switch 非法
	ActionSwitchBad
	// ActionSkillsOK skills list 合法
	ActionSkillsOK
	// ActionBranchOK branch 合法
	ActionBranchOK
	// ActionRewindOK rewind 合法
	ActionRewindOK
	// ActionRewindBad rewind 非法
	ActionRewindBad
	// ActionRewindConfirm rewind confirm 确认
	ActionRewindConfirm
	// ActionRewindCancel rewind cancel 取消
	ActionRewindCancel
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// GatewaySlashCommand 的字符串值
var gatewaySlashCommandStrings = map[GatewaySlashCommand]string{
	SlashNewSession: "/new_session",
	SlashMode:       "/mode",
	SlashSwitch:     "/switch",
	SlashSkills:     "/skills",
	SlashSkillsList: "/skills list",
	SlashBranch:     "/branch",
	SlashRewind:     "/rewind",
}

// ModeSubcommand 的字符串值
var modeSubcommandStrings = map[ModeSubcommand]string{
	ModeAgent:      "agent",
	ModeCode:       "code",
	ModeTeam:       "team",
	ModeAgentPlan:  "agent.plan",
	ModeAgentFast:  "agent.fast",
	ModeCodePlan:   "code.plan",
	ModeCodeNormal: "code.normal",
	ModeCodeTeam:   "code.team",
}

// SwitchSubcommand 的字符串值
var switchSubcommandStrings = map[SwitchSubcommand]string{
	SwitchPlan:   "plan",
	SwitchFast:   "fast",
	SwitchNormal: "normal",
	SwitchTeam:   "team",
}

// ValidModeLines 合法的 /mode 整行文本集合
var ValidModeLines = buildValidModeLines()

// ValidModeSubcommands 合法的 /mode 子命令值切片
var ValidModeSubcommands = buildModeSubcommandValues()

// ValidSwitchLines 合法的 /switch 整行文本集合
var ValidSwitchLines = buildValidSwitchLines()

// ValidSwitchSubcommands 合法的 /switch 子命令值切片
var ValidSwitchSubcommands = buildSwitchSubcommandValues()

// ControlMessageTexts 合法控制消息全集（用于 IM 入站管线跳过 LLM 改写等）
var ControlMessageTexts = buildControlMessageTexts()

// FirstBatchRegistry 第一批命令注册表
var FirstBatchRegistry = buildFirstBatchRegistry()

// ──────────────────────────── 导出函数 ────────────────────────────

// GatewaySlashCommandString 返回 GatewaySlashCommand 的字符串值
func GatewaySlashCommandString(cmd GatewaySlashCommand) string {
	if s, ok := gatewaySlashCommandStrings[cmd]; ok {
		return s
	}
	return ""
}

// ModeSubcommandString 返回 ModeSubcommand 的字符串值
func ModeSubcommandString(sub ModeSubcommand) string {
	if s, ok := modeSubcommandStrings[sub]; ok {
		return s
	}
	return ""
}

// SwitchSubcommandString 返回 SwitchSubcommand 的字符串值
func SwitchSubcommandString(sub SwitchSubcommand) string {
	if s, ok := switchSubcommandStrings[sub]; ok {
		return s
	}
	return ""
}

// ParseChannelControlText 解析单条用户文本是否为 /new_session、/mode、/switch、/skills list、/branch、/rewind 控制指令。
//
//   - 含换行则视为非控制（与原 _handle_channel_control 一致）。
//   - /new_session 仅整行精确匹配为合法；带后缀为非法但仍为控制指令。
//   - /mode 仅白名单整行合法；支持 agent|code|team 及四个直达模式值；其它以 /mode 开头且单行非法。
//   - /switch 仅白名单整行合法；其它以 /switch 开头且单行非法。
//   - /skills list 仅整行精确匹配（/skills 本身不再触发）。
//   - /branch [name] 合法；name 为可选自定义分支标题。
//   - /rewind [N] 合法；N 为可选回退轮次编号（正整数）；无参数或非整数参数为非法。
//   - /rewind confirm N 确认执行之前发起的 /rewind N。
//   - /rewind cancel 取消之前发起的 /rewind N。
func ParseChannelControlText(text string) ParsedChannelControl {
	if text == "" {
		return ParsedChannelControl{Action: ActionNone}
	}
	if strings.Contains(text, "\n") {
		return ParsedChannelControl{Action: ActionNone}
	}

	t := strings.TrimSpace(text)
	normalized := normalizeSpaces(t)

	// /new_session 精确匹配
	if t == "/new_session" {
		return ParsedChannelControl{Action: ActionNewSessionOK}
	}
	if strings.HasPrefix(t, "/new_session") {
		return ParsedChannelControl{Action: ActionNewSessionBad}
	}

	// /skills list 精确匹配（使用 normalized 折叠空白）
	if normalized == "/skills list" {
		return ParsedChannelControl{Action: ActionSkillsOK}
	}

	// /mode 白名单匹配
	if ValidModeLines[t] {
		parts := strings.Split(t, " ")
		sub := ""
		if len(parts) >= 2 {
			sub = parts[1]
		}
		return ParsedChannelControl{Action: ActionModeOK, ModeSubcommand: sub}
	}

	// /switch 白名单匹配
	if ValidSwitchLines[t] {
		parts := strings.Split(t, " ")
		sub := ""
		if len(parts) >= 2 {
			sub = parts[1]
		}
		return ParsedChannelControl{Action: ActionSwitchOK, SwitchSubcommand: sub}
	}

	// /mode 非法（前缀匹配）
	if strings.HasPrefix(t, "/mode") {
		return ParsedChannelControl{Action: ActionModeBad}
	}

	// /switch 非法（前缀匹配）
	if strings.HasPrefix(t, "/switch") {
		return ParsedChannelControl{Action: ActionSwitchBad}
	}

	// /branch 精确匹配或带名称
	if t == "/branch" {
		return ParsedChannelControl{Action: ActionBranchOK, BranchName: ""}
	}
	if strings.HasPrefix(t, "/branch ") {
		name := strings.TrimSpace(t[len("/branch"):])
		return ParsedChannelControl{Action: ActionBranchOK, BranchName: name}
	}

	// /rewind 无参数 → 非法
	if t == "/rewind" {
		return ParsedChannelControl{Action: ActionRewindBad}
	}

	// /rewind cancel — 取消之前的 /rewind（须在 /rewind N 前解析）
	if t == "/rewind cancel" {
		return ParsedChannelControl{Action: ActionRewindCancel}
	}

	// /rewind confirm N — 二步确认执行（须在 /rewind N 前解析）
	if strings.HasPrefix(t, "/rewind confirm ") {
		arg := strings.TrimSpace(t[len("/rewind confirm "):])
		turn, err := strconv.Atoi(arg)
		if err != nil || turn < 1 {
			return ParsedChannelControl{Action: ActionRewindBad}
		}
		return ParsedChannelControl{Action: ActionRewindConfirm, RewindTurn: turn}
	}

	// /rewind N — 发起回退
	if strings.HasPrefix(t, "/rewind ") {
		arg := strings.TrimSpace(t[len("/rewind "):])
		turn, err := strconv.Atoi(arg)
		if err != nil || turn < 1 {
			return ParsedChannelControl{Action: ActionRewindBad}
		}
		return ParsedChannelControl{Action: ActionRewindOK, RewindTurn: turn}
	}

	return ParsedChannelControl{Action: ActionNone}
}

// IsControlLikeForIMBatching 飞书/企微等：控制类消息不走合并窗口
//
// 单条文本、且为已知控制句、或以 /mode / /switch / /new_session / /branch / /rewind 为前缀时返回 true。
func IsControlLikeForIMBatching(text string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, "\n") {
		return false
	}
	t := strings.TrimSpace(text)
	normalized := normalizeSpaces(t)

	if ControlMessageTexts[t] {
		return true
	}
	if normalized == "/skills list" {
		return true
	}
	if strings.HasPrefix(t, "/mode ") {
		return true
	}
	if strings.HasPrefix(t, "/switch ") {
		return true
	}
	if strings.HasPrefix(t, "/switch") {
		return true
	}
	if strings.HasPrefix(t, "/new_session") {
		return true
	}
	if strings.HasPrefix(t, "/branch") {
		return true
	}
	if strings.HasPrefix(t, "/rewind") {
		return true
	}
	return false
}

// FormatSkillsListForNotice 将 skills.list 响应 payload 格式化为适合 IM 的纯文本
func FormatSkillsListForNotice(payload map[string]any, maxItems int) string {
	if maxItems <= 0 {
		maxItems = 50
	}
	if payload == nil {
		return "暂无技能数据。"
	}
	errVal, hasErr := payload["error"]
	if hasErr {
		if errStr, ok := errVal.(string); ok && strings.TrimSpace(errStr) != "" {
			return fmt.Sprintf("获取技能列表失败：%s", strings.TrimSpace(errStr))
		}
	}
	skillsVal, hasSkills := payload["skills"]
	if !hasSkills {
		return "当前无可用技能。"
	}
	skillsSlice, ok := skillsVal.([]any)
	if !ok || len(skillsSlice) == 0 {
		return "当前无可用技能。"
	}

	lines := []string{"【技能列表】"}
	limit := len(skillsSlice)
	if limit > maxItems {
		limit = maxItems
	}
	for i := 0; i < limit; i++ {
		item := skillsSlice[i]
		itemMap, isMap := item.(map[string]any)
		if isMap {
			name := "?"
			if n, ok := itemMap["name"]; ok && n != nil {
				s := strings.TrimSpace(fmt.Sprintf("%v", n))
				if s != "" {
					name = s
				}
			}
			if n, ok := itemMap["title"]; ok && n != nil && name == "?" {
				s := strings.TrimSpace(fmt.Sprintf("%v", n))
				if s != "" {
					name = s
				}
			}
			desc := ""
			if d, ok := itemMap["description"]; ok && d != nil {
				desc = strings.TrimSpace(fmt.Sprintf("%v", d))
			}
			src := ""
			if s, ok := itemMap["source"]; ok && s != nil {
				src = strings.TrimSpace(fmt.Sprintf("%v", s))
			}
			suffix := ""
			if src != "" {
				suffix = fmt.Sprintf(" (%s)", src)
			}
			if desc != "" {
				short := desc
				if len(desc) > 200 {
					short = desc[:200] + "…"
				}
				lines = append(lines, fmt.Sprintf("%d. %s%s\n   %s", i+1, name, suffix, short))
			} else {
				lines = append(lines, fmt.Sprintf("%d. %s%s", i+1, name, suffix))
			}
		} else {
			lines = append(lines, fmt.Sprintf("%d. %v", i+1, item))
		}
	}
	if len(skillsSlice) > maxItems {
		lines = append(lines, fmt.Sprintf("... 共 %d 项，仅显示前 %d 项。", len(skillsSlice), maxItems))
	}
	return strings.Join(lines, "\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeSpaces 折叠连续空白为单个空格
func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// buildValidModeLines 构建合法 /mode 整行文本集合
func buildValidModeLines() map[string]bool {
	result := make(map[string]bool)
	for _, sub := range allModeSubcommands() {
		result["/mode "+sub] = true
	}
	return result
}

// buildModeSubcommandValues 构建合法 /mode 子命令值切片
func buildModeSubcommandValues() []string {
	return allModeSubcommands()
}

// buildValidSwitchLines 构建合法 /switch 整行文本集合
func buildValidSwitchLines() map[string]bool {
	result := make(map[string]bool)
	for _, sub := range allSwitchSubcommands() {
		result["/switch "+sub] = true
	}
	return result
}

// buildSwitchSubcommandValues 构建合法 /switch 子命令值切片
func buildSwitchSubcommandValues() []string {
	return allSwitchSubcommands()
}

// buildControlMessageTexts 构建合法控制消息全集
func buildControlMessageTexts() map[string]bool {
	result := make(map[string]bool)
	result["/new_session"] = true
	for k := range ValidModeLines {
		result[k] = true
	}
	for k := range ValidSwitchLines {
		result[k] = true
	}
	result["/skills list"] = true
	result["/branch"] = true
	result["/rewind"] = true
	return result
}

// buildFirstBatchRegistry 构建第一批命令注册表
func buildFirstBatchRegistry() []SlashCommandEntry {
	return []SlashCommandEntry{
		{
			ID:            "new_session",
			CanonicalText: "/new_session",
			Scope:         "gateway",
			ReqMethod:     "",
			Notes:         "受控通道重置 session_id；由 MessageHandler 拦截，不转发 Agent 对话。",
		},
		{
			ID:            "mode",
			CanonicalText: "/mode agent|code|team|agent.plan|agent.fast|code.plan|code.normal|code.team",
			Scope:         "gateway",
			ReqMethod:     "",
			Notes:         "受控通道切换模式：一级模式 agent/code/team（映射到默认子模式）或直达 agent.plan/agent.fast/code.plan/code.normal；写入 params.mode。",
		},
		{
			ID:            "switch",
			CanonicalText: "/switch plan|fast|normal|team",
			Scope:         "gateway",
			ReqMethod:     "",
			Notes:         "受控通道切换二级模式：agent 下 plan/fast，code 下 plan/normal。",
		},
		{
			ID:            "skills",
			CanonicalText: "/skills list",
			Scope:         "gateway",
			ReqMethod:     "skills.list",
			Notes:         "受控通道整行 /skills list 时 Gateway 调 skills.list 并以通知回复；CLI 同路径见 builtins/skills.ts。",
		},
		{
			ID:            "resume",
			CanonicalText: "/resume",
			Scope:         "client",
			ReqMethod:     "command.resume",
			Notes:         "CLI 会话恢复；另用 session.list。IM 受控通道本阶段不解析，后续可扩展。",
		},
		{
			ID:            "workspace_dir",
			CanonicalText: "/workspace_dir [get|set <path>|clear]",
			Scope:         "client",
			ReqMethod:     "",
			Notes:         "TUI 本地保存工作区路径；随 chat.send params.workspace_dir 发往 Gateway/AgentServer。",
		},
		{
			ID:            "branch",
			CanonicalText: "/branch [name]",
			Scope:         "gateway",
			ReqMethod:     "session.fork",
			Notes:         "受控通道分叉当前会话；Gateway 调 session.fork 并以通知回复；CLI 同路径见 builtins/branch.ts。",
		},
		{
			ID:            "rewind",
			CanonicalText: "/rewind <turn_number>",
			Scope:         "gateway",
			ReqMethod:     "session.rewind",
			Notes:         "受控通道回退对话到指定轮次；IM 须带正整数轮次编号；CLI 同路径见 builtins/rewind.ts。",
		},
		{
			ID:            "recap",
			CanonicalText: "/recap",
			Scope:         "client",
			ReqMethod:     "command.recap",
			Notes:         "客户端命令，生成会话快速回顾（read-only）；TUI → Gateway → AgentServer。",
		},
		{
			ID:            "agents",
			CanonicalText: "/agents",
			Scope:         "client",
			ReqMethod:     "agents.list",
			Notes:         "TUI agent 配置管理菜单；TUI 通过 agents.* 方法与后端交互。",
		},
	}
}

// allModeSubcommands 返回所有 ModeSubcommand 的字符串值
func allModeSubcommands() []string {
	result := make([]string, 0, len(modeSubcommandStrings))
	for _, v := range modeSubcommandStrings {
		result = append(result, v)
	}
	return result
}

// allSwitchSubcommands 返回所有 SwitchSubcommand 的字符串值
func allSwitchSubcommands() []string {
	result := make([]string, 0, len(switchSubcommandStrings))
	for _, v := range switchSubcommandStrings {
		result = append(result, v)
	}
	return result
}
