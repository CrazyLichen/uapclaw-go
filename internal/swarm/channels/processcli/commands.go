package processcli

import "strings"

// 本文件是 channels/process_cli/commands.py 的 Go 对应物。
//
// Python: channels/process_cli/commands.py（166 行）
//
// 职责：本地 slash 命令注册表与解析。这些命令由 REPL 自己处理，
// 不经过 Runtime（例外：需要查 Runtime 的走 fork 子进程）。

// ──────────────────────────── 结构体 ────────────────────────────

// SlashCommand 补全菜单展示、本地 REPL 接受的命令。
// Python: commands.py:11-18 SlashCommand
type SlashCommand struct {
	// Name 规范命令名（含前导 /）
	Name string
	// Description 中文描述
	Description string
	// Aliases 别名（含前导 /）
	Aliases []string
}

// ParsedSlashCommand 一条已识别命令及其未解析的参数文本。
// Python: commands.py:21-27 ParsedSlashCommand
type ParsedSlashCommand struct {
	// Name 规范命令名
	Name string
	// Arguments 未解析的参数原文
	Arguments string
}

// SlashCommandArgument 补全提供的单个参数候选。
// Python: commands.py:30-35 SlashCommandArgument
type SlashCommandArgument struct {
	// Value 参数值
	Value string
	// Description 中文描述
	Description string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// modeArguments /mode 的参数候选。
// Python: commands.py:38-43 MODE_ARGUMENTS
var modeArguments = []SlashCommandArgument{
	{Value: "agent.work", Description: "单 Agent 通用模式"},
	{Value: "agent.code", Description: "单 Agent 代码模式"},
	{Value: "team.work", Description: "Team 通用模式"},
	{Value: "team.code", Description: "Team 代码模式"},
}

// modeTargets 非 plan 的 Process CLI 模式 → Runtime 规范值。
// Python: commands.py:46-63 _MODE_TARGETS
var modeTargets = map[string]string{
	"agent":             "agent.work.normal",
	"code":              "agent.code.normal",
	"team":              "team.work.normal",
	"team.normal":       "team.work.normal",
	"code.normal":       "agent.code.normal",
	"code.team":         "team.code.normal",
	"agent.work":        "agent.work.normal",
	"agent.code":        "agent.code.normal",
	"team.work":         "team.work.normal",
	"team.code":         "team.code.normal",
	"agent.work.normal": "agent.work.normal",
	"agent.work.plan":   "agent.work.plan",
	"agent.code.normal": "agent.code.normal",
	"agent.code.plan":   "agent.code.plan",
	"team.work.normal":  "team.work.normal",
	"team.code.normal":  "team.code.normal",
}

// skillsArguments /skills 的参数候选。
// Python: commands.py:65-67 _SKILLS_ARGUMENTS
var skillsArguments = []SlashCommandArgument{
	{Value: "list", Description: "列出当前可用技能"},
}

// newArguments /new 的参数候选。
// Python: commands.py:69-72 _NEW_ARGUMENTS
var newArguments = []SlashCommandArgument{
	{Value: "--persist", Description: "为新会话启用持久记忆"},
	{Value: "--persist-session", Description: "为新会话启用持久记忆"},
}

// planArguments /plan 的参数候选。
// Python: commands.py:74-78 _PLAN_ARGUMENTS
var planArguments = []SlashCommandArgument{
	{Value: "on", Description: "开启规划模式"},
	{Value: "off", Description: "关闭规划模式"},
	{Value: "status", Description: "查看规划模式"},
}

// slashCommands 全部本地命令（顺序即 /help 的展示顺序）。
// Python: commands.py:81-98 SLASH_COMMANDS
var slashCommands = []SlashCommand{
	{Name: "/help", Description: "查看所有命令"},
	{Name: "/sessions", Description: "搜索并选择进程式 CLI 会话"},
	{Name: "/mode", Description: "查看或切换运行模式"},
	{Name: "/model", Description: "查看或选择下一轮模型"},
	{Name: "/plan", Description: "切换单 Agent 规划模式"},
	{Name: "/status", Description: "查看当前状态"},
	{Name: "/skills", Description: "查看可用技能"},
	{Name: "/permissions", Description: "查看有效权限（只读）"},
	{Name: "/new", Description: "创建并切换到新会话"},
	{Name: "/resume", Description: "选择会话或按 ID 恢复", Aliases: []string{"/continue"}},
	{Name: "/branch", Description: "从当前会话创建分支", Aliases: []string{"/fork"}},
	{Name: "/delete", Description: "删除指定会话"},
	{Name: "/session", Description: "查看当前会话"},
	{Name: "/cancel", Description: "中断当前任务"},
	{Name: "/exit", Description: "退出 UAPClaw", Aliases: []string{"/quit"}},
}

// commandNames 命令名或别名 → 规范命令名。
// Python: commands.py:100-105 _COMMAND_NAMES
var commandNames = buildCommandNames()

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveSlashCommand 把精确命令名或别名解析为规范命令名。
// Python: commands.py:108-110 resolve_slash_command
func ResolveSlashCommand(value string) (string, bool) {
	name, ok := commandNames[strings.ToLower(strings.TrimSpace(value))]
	return name, ok
}

// ParseSlashCommand 解析一条已识别的本地命令，不解释其参数。
// Python: commands.py:113-126 parse_slash_command
func ParseSlashCommand(value string) *ParsedSlashCommand {
	parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
	if len(parts) == 0 || parts[0] == "" {
		return nil
	}
	name, ok := commandNames[strings.ToLower(parts[0])]
	if !ok {
		return nil
	}
	arguments := ""
	if len(parts) > 1 {
		arguments = strings.TrimSpace(parts[1])
	}
	return &ParsedSlashCommand{Name: name, Arguments: arguments}
}

// ResolveModeTarget 把一个非 plan 的 Process CLI 模式解析为 Runtime 规范值。
// Python: commands.py:129-131 resolve_mode_target
func ResolveModeTarget(value string) (string, bool) {
	target, ok := modeTargets[strings.ToLower(strings.TrimSpace(value))]
	return target, ok
}

// MatchingSlashCommands 返回以斜杠开头前缀匹配的规范命令。
// Python: commands.py:134-141 matching_slash_commands
func MatchingSlashCommands(prefix string) []SlashCommand {
	normalized := strings.ToLower(prefix)
	if !strings.HasPrefix(normalized, "/") || strings.ContainsFunc(normalized, isSpaceRune) {
		return nil
	}
	matched := make([]SlashCommand, 0, len(slashCommands))
	for _, command := range slashCommands {
		if strings.HasPrefix(command.Name, normalized) {
			matched = append(matched, command)
		}
	}
	return matched
}

// MatchingSlashArguments 返回首个命令参数的补全建议。
// Python: commands.py:144-163 matching_slash_arguments
func MatchingSlashArguments(prefix string) []SlashCommandArgument {
	normalized := strings.ToLower(prefix)
	commandName, fragment, found := strings.Cut(normalized, " ")
	if !found || strings.ContainsFunc(fragment, isSpaceRune) {
		return nil
	}
	var options []SlashCommandArgument
	switch commandName {
	case "/mode":
		options = modeArguments
	case "/skills":
		options = skillsArguments
	case "/new":
		options = newArguments
	case "/plan":
		options = planArguments
	default:
		return nil
	}
	matched := make([]SlashCommandArgument, 0, len(options))
	for _, option := range options {
		if strings.HasPrefix(option.Value, fragment) {
			matched = append(matched, option)
		}
	}
	return matched
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildCommandNames 构建「命令名或别名 → 规范名」查找表。
func buildCommandNames() map[string]string {
	names := make(map[string]string, len(slashCommands)*2)
	for _, command := range slashCommands {
		names[command.Name] = command.Name
		for _, alias := range command.Aliases {
			names[alias] = command.Name
		}
	}
	return names
}

// isSpaceRune 判断是否为空白字符。
func isSpaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}
