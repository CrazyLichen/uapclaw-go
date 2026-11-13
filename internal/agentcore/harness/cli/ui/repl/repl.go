package repl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// REPL 交互循环。
//
// Python: openjiuwen/harness/cli/ui/repl.py（1316 行）
//
// ⏵ 实施范围（2026-11-27，spec §7.7 预告）：本文件只落可分离的纯函数
// 部分——slash 命令表、_answered_as_typed、技能扫描与 shell 透传。
// REPL 主循环（PromptSession 的行编辑 / 历史浏览 / slash 补全渲染 /
// 交互应答渲染）依赖真实 TTY，⏵ PTY 测试（见 pty-isatty-testing-traps
// 记忆的三个坑），.coverage-exempt 登记整文件。

// ──────────────────────────── 常量 ────────────────────────────

// SlashCommands 内置 slash 命令集合。
// Python: repl.py:656-666 SLASH_COMMANDS
var SlashCommands = []string{
	"/help", "/exit", "/quit", "/clear", "/status",
	"/cost", "/compact", "/sessions", "/auto-harness",
}

// SlashDescriptions 人类可读的补全描述。
// /quit 刻意省略（隐藏别名）。
// Python: repl.py:668-678 _SLASH_DESCRIPTIONS
var SlashDescriptions = map[string]string{
	"/help":         "Show available commands",
	"/exit":         "Exit the REPL",
	"/clear":        "Clear the screen",
	"/status":       "Show model and token usage",
	"/cost":         "Show token cost breakdown",
	"/compact":      "Compact conversation history",
	"/sessions":     "List saved sessions",
	"/config":       "Show current configuration",
	"/auto-harness": "Run auto-harness optimization",
}

// skillCommands 启动期动态注册的技能命令：/skill-name → 描述。
// Python: repl.py:680-682 _SKILL_COMMANDS
var skillCommands = map[string]string{}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsSlashCommand 输入是否是已注册的 slash 命令（内置或技能）。
//
// 内置判定用 SlashCommands（含隐藏别名 /quit），而非
// SlashDescriptions——描述表刻意省略 /quit，但它真实可执行。
func IsSlashCommand(input string) bool {
	for _, name := range SlashCommands {
		if name == input {
			return true
		}
	}
	_, skill := skillCommands[input]
	return skill
}

// SkillCommands 返回已注册技能命令（名字 → 描述）的副本。
func SkillCommands() map[string]string {
	out := make(map[string]string, len(skillCommands))
	for name, description := range skillCommands {
		out[name] = description
	}
	return out
}

// RegisterSkillCommand 注册一个技能 slash 命令。
// Python: repl.py:821-846 _register_skill_commands（简化：名字+描述）
func RegisterSkillCommand(name, description string) {
	skillCommands[name] = description
}

// ResetSkillCommands 清空技能命令（测试隔离用）。
func ResetSkillCommands() {
	skillCommands = map[string]string{}
}

// AnsweredAsTyped CLI 是否没有该 kind 的应答形态：答案是打字输入。
// Python: repl.py:960-966 _answered_as_typed
//
// 非审批 kind 且不是 ask_user 时答案是自由打字。
func AnsweredAsTyped(kind string) bool {
	return !interruptpkg.IsApprovalKind(kind) && kind != saschema.InterruptKindAskUser
}

// HandleShell 透传执行 shell 命令。
// Python: repl.py:725-751 _handle_shell
//
// 空命令或仅 `!` 时打印提示；退出码非 0 时返回错误。
func HandleShell(cmd string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(cmd, "!"))
	if trimmed == "" {
		return fmt.Errorf("usage: ! <command>")
	}
	command := exec.Command("sh", "-c", trimmed)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// ReadSkillDescription 读 SKILL.md 的第一段非标题正文作描述。
// Python: repl.py:752-777 _read_skill_description
//
// 跳过 frontmatter（--- 包裹）与标题行，取第一个非空段的首行。
func ReadSkillDescription(skillMdPath string) string {
	content, err := os.ReadFile(skillMdPath)
	if err != nil {
		return ""
	}
	inFrontmatter := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

// ScanSkillDirs 扫描 skill 目录并注册 slash 命令。
// Python: repl.py:778-820 _scan_skill_dirs + :876-886 _scan_skills
//
// 每个目录的子目录若含 SKILL.md，注册为 /<子目录名>；返回注册数。
func ScanSkillDirs(dirs []string) int {
	count := 0
	home, homeErr := os.UserHomeDir()
	for _, dir := range dirs {
		expanded := dir
		if strings.HasPrefix(dir, "~") && homeErr == nil {
			if dir == "~" {
				expanded = home
			} else if strings.HasPrefix(dir, "~/") {
				expanded = filepath.Join(home, dir[2:])
			}
		}
		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue // 不存在的目录静默跳过
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillMd := filepath.Join(expanded, entry.Name(), "SKILL.md")
			if _, statErr := os.Stat(skillMd); statErr != nil {
				continue
			}
			description := ReadSkillDescription(skillMd)
			RegisterSkillCommand("/"+entry.Name(), description)
			count++
		}
	}
	return count
}

// SortedSkillNames 补全用：排序后的全部 slash 命令名（内置 + 技能）。
func SortedSkillNames() []string {
	names := make([]string, 0, len(SlashDescriptions)+len(skillCommands))
	for name := range SlashDescriptions {
		names = append(names, name)
	}
	for name := range skillCommands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
