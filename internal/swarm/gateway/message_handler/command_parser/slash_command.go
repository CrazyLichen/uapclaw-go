package command_parser

import (
	"fmt"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ParsedChannelControl 斜杠命令解析结果
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

// SlashCommandEntry 斜杠命令条目
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

// GatewaySlashCommand 网关斜杠命令枚举
type GatewaySlashCommand int

// ModeSubcommand 模式子命令枚举
type ModeSubcommand int

// SwitchSubcommand 切换子命令枚举
type SwitchSubcommand int

// ParsedControlAction 解析后的控制动作枚举
type ParsedControlAction int

// ──────────────────────────── 常量 ────────────────────────────

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

const (
	// ModeAgent 代理模式
	ModeAgent ModeSubcommand = iota
	// ModeCode 代码模式
	ModeCode
	// ModeTeam 团队模式
	ModeTeam
	// ModeAgentWorkPlan 代理规划模式
	ModeAgentWorkPlan
	// ModeAgentWorkNormal 代理快速模式
	ModeAgentWorkNormal
	// ModeCodePlan 代码规划模式
	ModeCodePlan
	// ModeCodeNormal 代码普通模式
	ModeCodeNormal
	// ModeCodeTeam 代码团队模式
	ModeCodeTeam
	// ModeAgentPlan 代理规划（历史值）
	ModeAgentPlan
	// ModeAgentFast 代理快速（历史值）
	ModeAgentFast
	// ModeTeamPlan 团队规划别名（历史值）
	ModeTeamPlan
	// ModeTeamPlanNormal 团队规划（历史值）
	ModeTeamPlanNormal
	// ModeTeamPlanCode 团队代码规划（历史值）
	ModeTeamPlanCode
	// ModeCodePlanFull code 规划 canonical（agent.code.plan）
	ModeCodePlanFull
	// ModeCodeNormalFull code 常态 canonical（agent.code.normal）
	ModeCodeNormalFull
	// ModeTeamWorkNormal 团队工作常态 canonical
	ModeTeamWorkNormal
	// ModeTeamWorkPlan 团队工作规划 canonical
	ModeTeamWorkPlan
	// ModeTeamCodeNormal 团队代码常态 canonical
	ModeTeamCodeNormal
	// ModeTeamCodePlan 团队代码规划 canonical
	ModeTeamCodePlan
)

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

// ──────────────────────────── 全局变量 ────────────────────────────

var gatewaySlashCommandStrings = map[GatewaySlashCommand]string{
	SlashNewSession: "/new_session",
	SlashMode:       "/mode",
	SlashSwitch:     "/switch",
	SlashSkills:     "/skills",
	SlashSkillsList: "/skills list",
	SlashBranch:     "/branch",
	SlashRewind:     "/rewind",
}

// modeSubcommandStrings /mode 子命令词表（19 项）。
// Python: ModeSubcommand（slash_command.py:39-61）——旧成员保留 + 8 个新三段
// canonical 成员，`_VALID_MODE_LINES` 由整表派生。Go 逐项对齐，避免
// `/mode team.work.normal` 之类被误判「非法指令」。
var modeSubcommandStrings = map[ModeSubcommand]string{
	ModeAgent:           "agent",
	ModeCode:            "code",
	ModeTeam:            "team",
	ModeAgentPlan:       "agent.plan",
	ModeAgentFast:       "agent.fast",
	ModeCodePlan:        "code.plan",
	ModeCodeNormal:      "code.normal",
	ModeCodeTeam:        "code.team",
	ModeTeamPlan:        "team.plan",
	ModeTeamPlanNormal:  "team.plan.normal",
	ModeTeamPlanCode:    "team.plan.code",
	ModeAgentWorkPlan:   "agent.work.plan",
	ModeAgentWorkNormal: "agent.work.normal",
	ModeCodePlanFull:    "agent.code.plan",
	ModeCodeNormalFull:  "agent.code.normal",
	ModeTeamWorkNormal:  "team.work.normal",
	ModeTeamWorkPlan:    "team.work.plan",
	ModeTeamCodeNormal:  "team.code.normal",
	ModeTeamCodePlan:    "team.code.plan",
}

var switchSubcommandStrings = map[SwitchSubcommand]string{
	SwitchPlan:   "plan",
	SwitchFast:   "fast",
	SwitchNormal: "normal",
	SwitchTeam:   "team",
}

// ValidModeLines 合法的模式命令文本集合
var ValidModeLines = buildValidModeLines()

// ValidModeSubcommands 合法的模式子命令值集合
var ValidModeSubcommands = buildModeSubcommandValues()

// ValidSwitchLines 合法的切换命令文本集合
var ValidSwitchLines = buildValidSwitchLines()

// ValidSwitchSubcommands 合法的切换子命令值集合
var ValidSwitchSubcommands = buildSwitchSubcommandValues()

// ControlMessageTexts 控制消息文本映射
// builtinCommandsMeta 快捷面板展示的内置命令元数据（静态，无 IO）。
// Python: BUILTIN_COMMANDS_META (gateway/message_handler/command_parser/slash_command.py:483-572)。
// description 由 description_i18n["zh"] 派生（对齐 list_builtin_commands 的旧客户端兼容）。
var builtinCommandsMeta = []map[string]any{
	{
		"name": "fork",
		"description_i18n": map[string]any{
			"zh": "分叉当前会话并切换到副本",
			"en": "Fork the current conversation and switch to the copy",
		},
		"usage":            "/fork",
		"example":          nil,
		"kind":             "built-in",
		"takesArgs":        false,
		"scope":            "agent",
		"execution":        "rpc",
		"req_method":       "session.fork",
		"requires_session": true,
		"available_modes":  nil,
	},
	{
		"name": "compact",
		"description_i18n": map[string]any{
			"zh": "压缩对话历史，保留摘要以节省上下文",
			"en": "Compress conversation history into a summary to save context space",
		},
		"usage":            "/compact",
		"example":          nil,
		"kind":             "built-in",
		"takesArgs":        false,
		"scope":            "agent",
		"execution":        "rpc",
		"req_method":       "command.compact",
		"requires_session": true,
		"available_modes":  nil,
	},
	{
		// Web 侧复用现有 Plan 开关：面板选中后立即翻转，不向输入框插入命令。
		"name": "plan",
		"description_i18n": map[string]any{
			"zh": "切换计划模式（只读规划 → 审批 → 执行）",
			"en": "Toggle plan mode (read-only planning → approval → execution)",
		},
		"usage":             "/plan",
		"example":           nil,
		"kind":              "built-in",
		"takesArgs":         false,
		"scope":             "agent",
		"execution":         "chat.send_with_mode",
		"mode":              "agent.plan",
		"plan_entry_source": "slash_command",
		"requires_session":  false,
		"available_modes":   nil,
	},
	{
		"name": "goal",
		"description_i18n": map[string]any{
			"zh": "设置、查看、暂停、恢复或清除持续目标",
			"en": "Set, view, pause, resume, or clear a persistent goal",
		},
		"usage":            "/goal [set <目标>|pause|resume|clear]",
		"example":          "/goal 持续修复测试直到全部通过",
		"kind":             "built-in",
		"takesArgs":        true,
		"scope":            "client",
		"execution":        "rpc",
		"req_method":       "command.goal",
		"requires_session": false,
		"available_modes":  nil,
	},
	{
		"name": "persist",
		"description_i18n": map[string]any{
			"zh": "开启永续会话并开始任务（仅限新会话，创建后不可更改）",
			"en": "Start a persistent conversation and begin a task " +
				"(new conversations only; cannot be changed after creation)",
		},
		"usage":            "/persist <任务>",
		"example":          "/persist 帮我持续跟进这次产品发布",
		"kind":             "built-in",
		"takesArgs":        true,
		"scope":            "client",
		"execution":        "session.create",
		"requires_session": false,
		"available_modes":  nil,
	},
}

// ControlMessageTexts 根据当前语言构建的控制类消息提示文案表。
var ControlMessageTexts = buildControlMessageTexts()

// FirstBatchRegistry 首批注册表
var FirstBatchRegistry = buildFirstBatchRegistry()

// parsedControlActionStrings ParsedControlAction 到 Python 枚举值的映射
var parsedControlActionStrings = map[ParsedControlAction]string{
	ActionNone:          "none",
	ActionNewSessionOK:  "new_session_ok",
	ActionNewSessionBad: "new_session_bad",
	ActionModeOK:        "mode_ok",
	ActionModeBad:       "mode_bad",
	ActionSwitchOK:      "switch_ok",
	ActionSwitchBad:     "switch_bad",
	ActionSkillsOK:      "skills_ok",
	ActionBranchOK:      "branch_ok",
	ActionRewindOK:      "rewind_ok",
	ActionRewindBad:     "rewind_bad",
	ActionRewindConfirm: "rewind_confirm",
	ActionRewindCancel:  "rewind_cancel",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsedControlActionString 返回 ParsedControlAction 的 Python 枚举值字符串。
// Python: ParsedControlAction.value（"new_session_ok" 等），用于日志字段对齐。
func ParsedControlActionString(a ParsedControlAction) string {
	if s, ok := parsedControlActionStrings[a]; ok {
		return s
	}
	return ""
}

// GatewaySlashCommandString 返回 GatewaySlashCommand 的字符串表示
func GatewaySlashCommandString(cmd GatewaySlashCommand) string {
	if s, ok := gatewaySlashCommandStrings[cmd]; ok {
		return s
	}
	return ""
}

// ModeSubcommandString 返回 ModeSubcommand 的字符串表示
func ModeSubcommandString(sub ModeSubcommand) string {
	if s, ok := modeSubcommandStrings[sub]; ok {
		return s
	}
	return ""
}

// SwitchSubcommandString 返回 SwitchSubcommand 的字符串表示
func SwitchSubcommandString(sub SwitchSubcommand) string {
	if s, ok := switchSubcommandStrings[sub]; ok {
		return s
	}
	return ""
}

// ParseChannelControlText 解析通道控制文本为结构化结果
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

// IsControlLikeForIMBatching 判断文本是否类似控制命令（用于 IM 批处理判断）
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

// FormatSkillsListForNotice 格式化技能列表用于通知
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

// normalizeSpaces 把连续空白压成一个空格并去掉首尾空白，便于比较命令文本。
func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// buildValidModeLines 拼出所有合法的 /mode 子命令文本集合。
func buildValidModeLines() map[string]bool {
	result := make(map[string]bool)
	for _, sub := range allModeSubcommands() {
		result["/mode "+sub] = true
	}
	return result
}

// buildModeSubcommandValues 返回所有 /mode 子命令取值列表。
func buildModeSubcommandValues() []string {
	return allModeSubcommands()
}

// buildValidSwitchLines 拼出所有合法的 /switch 子命令文本集合。
func buildValidSwitchLines() map[string]bool {
	result := make(map[string]bool)
	for _, sub := range allSwitchSubcommands() {
		result["/switch "+sub] = true
	}
	return result
}

// buildSwitchSubcommandValues 返回所有 /switch 子命令取值列表。
func buildSwitchSubcommandValues() []string {
	return allSwitchSubcommands()
}

// buildControlMessageTexts 汇总被当作控制指令而不是普通消息的文本集合。
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

// buildFirstBatchRegistry 构建首批内置斜杠命令的登记表。
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

// allModeSubcommands 返回所有 /mode 子命令名称。
func allModeSubcommands() []string {
	result := make([]string, 0, len(modeSubcommandStrings))
	for _, v := range modeSubcommandStrings {
		result = append(result, v)
	}
	return result
}

// allSwitchSubcommands 返回所有 /switch 子命令名称。
func allSwitchSubcommands() []string {
	result := make([]string, 0, len(switchSubcommandStrings))
	for _, v := range switchSubcommandStrings {
		result = append(result, v)
	}
	return result
}

// ListBuiltinCommands 返回快捷面板要展示的命令清单（静态元数据，无 IO）。
// Python: list_builtin_commands (slash_command.py:574-592)。params.work_mode 用于按
// available_modes 过滤；description 保留中文供旧客户端，description_i18n 供新客户端选语言。
func ListBuiltinCommands(params map[string]any) map[string]any {
	workMode := ""
	if params != nil {
		if v, ok := params["work_mode"].(string); ok {
			workMode = v
		}
	}
	out := make([]any, 0, len(builtinCommandsMeta))
	for _, cmd := range builtinCommandsMeta {
		if modes, has := cmd["available_modes"].([]any); has && len(modes) > 0 && workMode != "" {
			allowed := false
			for _, m := range modes {
				if s, ok := m.(string); ok && s == workMode {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		entry := make(map[string]any, len(cmd)+1)
		for k, v := range cmd {
			entry[k] = v
		}
		descriptions, _ := cmd["description_i18n"].(map[string]any)
		if descriptions != nil {
			if zh, ok := descriptions["zh"].(string); ok {
				entry["description"] = zh
			}
		}
		out = append(out, entry)
	}
	return map[string]any{"commands": out}
}
