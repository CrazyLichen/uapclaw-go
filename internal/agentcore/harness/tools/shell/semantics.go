package shell

import (
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExitCodeMeaning 退出码含义
type ExitCodeMeaning struct {
	// IsError 是否为错误
	IsError bool
	// Message 语义解释信息
	Message string
}

// ──────────────────────────── 枚举 ────────────────────────────

// CommandKind 命令分类枚举
type CommandKind int

const (
	// CommandKindSearch 搜索命令
	CommandKindSearch CommandKind = iota
	// CommandKindRead 读取命令
	CommandKindRead
	// CommandKindList 列表命令
	CommandKindList
	// CommandKindNeutral 中性命令
	CommandKindNeutral
	// CommandKindSilent 静默命令
	CommandKindSilent
	// CommandKindOther 其他命令
	CommandKindOther
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// bash 命令集
// 对齐 Python: _SEARCH_COMMANDS (bash/_semantics.py L33-36)
var bashSearchCommands = map[string]bool{
	"find": true, "grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true, "ack": true,
	"locate": true, "which": true, "whereis": true, "type": true, "command": true, "findstr": true,
}

// 对齐 Python: _READ_COMMANDS (bash/_semantics.py L38-44)
var bashReadCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true, "wc": true, "stat": true,
	"file": true, "strings": true, "jq": true, "yq": true, "awk": true, "gawk": true, "cut": true,
	"sort": true, "uniq": true, "tr": true, "tee": true, "od": true, "xxd": true, "hexdump": true,
	"sha256sum": true, "sha1sum": true, "md5sum": true, "md5": true, "shasum": true,
	"get-content": true, "get-item": true, "test-path": true, "select-object": true, "where-object": true,
}

// 对齐 Python: _LIST_COMMANDS (bash/_semantics.py L46-48)
var bashListCommands = map[string]bool{
	"ls": true, "dir": true, "tree": true, "du": true, "df": true, "lsof": true, "lsblk": true, "get-childitem": true,
}

// 对齐 Python: _NEUTRAL_COMMANDS (bash/_semantics.py L50-52)
var bashNeutralCommands = map[string]bool{
	"echo": true, "printf": true, "true": true, "false": true, ":": true, "test": true, "[": true,
}

// 对齐 Python: _SILENT_COMMANDS (bash/_semantics.py L54-58)
var bashSilentCommands = map[string]bool{
	"mv": true, "cp": true, "rm": true, "mkdir": true, "rmdir": true, "chmod": true, "chown": true,
	"chgrp": true, "touch": true, "ln": true, "cd": true, "export": true, "unset": true,
	"source": true, ".": true, "wait": true, "pushd": true, "popd": true,
}

// powershell 命令集
// 对齐 Python: _SEARCH_COMMANDS (powershell/_semantics.py L30-32)
var psSearchCommands = map[string]bool{
	"select-string": true, "sls": true, "findstr": true, "get-command": true, "where-object": true, "where": true,
}

// 对齐 Python: _READ_COMMANDS (powershell/_semantics.py L34-38)
var psReadCommands = map[string]bool{
	"get-content": true, "gc": true, "type": true, "get-item": true, "gi": true, "test-path": true, "resolve-path": true, "get-filehash": true,
	"select-object": true, "select": true, "sort-object": true, "sort": true, "format-table": true, "ft": true, "format-list": true, "fl": true,
	"format-wide": true, "fw": true, "foreach-object": true, "foreach": true, "measure-object": true,
}

// 对齐 Python: _LIST_COMMANDS (powershell/_semantics.py L40-42)
var psListCommands = map[string]bool{
	"get-childitem": true, "gci": true, "dir": true, "ls": true,
}

// 对齐 Python: _NEUTRAL_COMMANDS (powershell/_semantics.py L44-46)
var psNeutralCommands = map[string]bool{
	"write-output": true, "echo": true, "write-host": true, "out-host": true,
}

// 对齐 Python: _SILENT_COMMANDS (powershell/_semantics.py L48-54)
var psSilentCommands = map[string]bool{
	"set-location": true, "cd": true, "sl": true, "push-location": true, "pop-location": true,
	"new-item": true, "ni": true, "remove-item": true, "ri": true, "rm": true,
	"move-item": true, "mi": true, "mv": true, "copy-item": true, "cp": true, "cpi": true,
	"rename-item": true, "rni": true, "set-content": true, "sc": true, "add-content": true, "ac": true,
	"clear-content": true, "clc": true,
}

// 对齐 Python: _GET_CHILD_ITEM_COMMANDS (powershell/_semantics.py L56-58)
var psGetChildItemCommands = map[string]bool{
	"get-childitem": true, "gci": true, "dir": true, "ls": true,
}

// 对齐 Python: _SEARCH_EXIT_ONE_COMMANDS (powershell/_semantics.py L60-62)
var psSearchExitOneCommands = map[string]bool{
	"select-string": true, "sls": true, "findstr": true,
}

// bash 命令分类查找表
var bashKindLookup = buildKindLookup(bashSearchCommands, bashReadCommands, bashListCommands, bashNeutralCommands, bashSilentCommands)

// powershell 命令分类查找表
var psKindLookup = buildKindLookup(psSearchCommands, psReadCommands, psListCommands, psNeutralCommands, psSilentCommands)

// 只读命令分类集合
var readKinds = map[CommandKind]bool{
	CommandKindSearch: true,
	CommandKindRead:   true,
	CommandKindList:   true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// InterpretBashExitCode 解释 bash 命令退出码。
// 对齐 Python: interpret_exit_code (bash/_semantics.py L210-230)
func InterpretBashExitCode(command string, exitCode int, stdout, stderr string) ExitCodeMeaning {
	if exitCode == 0 {
		return ExitCodeMeaning{IsError: false}
	}

	parts := splitBashPipeline(command)
	if len(parts) == 0 {
		return ExitCodeMeaning{IsError: true}
	}
	base := extractBashBaseCommand(parts[len(parts)-1])
	return interpretBashBase(base, exitCode, stdout, stderr)
}

// InterpretPowerShellExitCode 解释 PowerShell 命令退出码。
// 对齐 Python: interpret_exit_code (powershell/_semantics.py L188-214)
func InterpretPowerShellExitCode(command string, exitCode int, stdout, stderr string) ExitCodeMeaning {
	if exitCode == 0 {
		return ExitCodeMeaning{IsError: false}
	}

	parts := splitPSPipeline(command)
	if len(parts) == 0 {
		return ExitCodeMeaning{IsError: true}
	}

	base := extractPSBaseCommand(parts[len(parts)-1])

	// 检查 Get-ChildItem 族
	if psGetChildItemCommands[base] {
		return psGetChildItemSemantics(exitCode, stdout, stderr)
	}

	// 检查搜索命令族 (select-string/sls/findstr)
	if psSearchExitOneCommands[base] {
		return psSearchSemantics(exitCode, stdout, stderr)
	}

	// 通用回退: exit 1 有stdout无stderr 且 is_read_only → 非错误
	if exitCode == 1 && stdout != "" && stderr == "" && isReadOnly(command, true) {
		return ExitCodeMeaning{
			IsError: false,
			Message: "PowerShell returned exit code 1 after producing output; treating output as partial result",
		}
	}

	return ExitCodeMeaning{IsError: true}
}

// ClassifyCommand 分类命令
// 对齐 Python: classify_command (bash/_semantics.py L102-112)
func ClassifyCommand(command string, isPowerShell bool) CommandKind {
	var parts []string
	var lookup map[string]CommandKind
	if isPowerShell {
		parts = splitPSPipeline(command)
		lookup = psKindLookup
	} else {
		parts = splitBashPipeline(command)
		lookup = bashKindLookup
	}
	if len(parts) == 0 {
		return CommandKindOther
	}
	base := extractBaseCommand(parts[len(parts)-1], isPowerShell)
	if kind, ok := lookup[base]; ok {
		return kind
	}
	return CommandKindOther
}

// IsSilent 判断命令是否为静默命令
// 对齐 Python: is_silent (bash/_semantics.py L133-145 / powershell/_semantics.py L173-185)
func IsSilent(command string, isPowerShell bool) bool {
	var parts []string
	var lookup map[string]CommandKind
	if isPowerShell {
		parts = splitPSPipeline(command)
		lookup = psKindLookup
	} else {
		parts = splitBashPipeline(command)
		lookup = bashKindLookup
	}
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		base := extractBaseCommand(part, isPowerShell)
		kind, ok := lookup[base]
		if !ok {
			kind = CommandKindOther
		}
		if kind == CommandKindNeutral {
			continue
		}
		if kind != CommandKindSilent {
			return false
		}
	}
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isReadOnly 判断命令是否为只读命令
// 对齐 Python: is_read_only (bash/_semantics.py L118-130 / powershell/_semantics.py L158-170)
func isReadOnly(command string, isPowerShell bool) bool {
	var parts []string
	var lookup map[string]CommandKind
	if isPowerShell {
		parts = splitPSPipeline(command)
		lookup = psKindLookup
	} else {
		parts = splitBashPipeline(command)
		lookup = bashKindLookup
	}
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		base := extractBaseCommand(part, isPowerShell)
		kind, ok := lookup[base]
		if !ok {
			kind = CommandKindOther
		}
		if kind == CommandKindNeutral {
			continue
		}
		if !readKinds[kind] {
			return false
		}
	}
	return true
}

// interpretBashBase 根据 base 命令解释 bash 退出码
// 对齐 Python: _SEMANTICS_TABLE (bash/_semantics.py L190-207)
func interpretBashBase(base string, exitCode int, stdout, stderr string) ExitCodeMeaning {
	switch base {
	case "grep", "egrep", "fgrep", "rg", "ag", "ack", "findstr":
		return grepSemantics(exitCode)
	case "find":
		return findSemantics(exitCode)
	case "diff":
		return diffSemantics(exitCode)
	case "test", "[":
		return testSemantics(exitCode)
	case "get-content", "get-item", "get-childitem", "select-object", "where-object":
		return psReadSemantics(exitCode, stderr)
	default:
		return ExitCodeMeaning{IsError: true}
	}
}

// grepSemantics grep 族退出码语义
// 对齐 Python: _grep_semantics (bash/_semantics.py L150-155)
func grepSemantics(code int) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false}
	}
	if code == 1 {
		return ExitCodeMeaning{IsError: false, Message: "No matches found"}
	}
	return ExitCodeMeaning{IsError: true, Message: "grep error (exit " + itoa(code) + ")"}
}

// findSemantics find 命令退出码语义
// 对齐 Python: _find_semantics (bash/_semantics.py L158-163)
func findSemantics(code int) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false}
	}
	if code == 1 {
		return ExitCodeMeaning{IsError: false, Message: "Some directories inaccessible"}
	}
	return ExitCodeMeaning{IsError: true, Message: "find error (exit " + itoa(code) + ")"}
}

// diffSemantics diff 命令退出码语义
// 对齐 Python: _diff_semantics (bash/_semantics.py L166-171)
func diffSemantics(code int) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false, Message: "Files are identical"}
	}
	if code == 1 {
		return ExitCodeMeaning{IsError: false, Message: "Files differ"}
	}
	return ExitCodeMeaning{IsError: true, Message: "diff error (exit " + itoa(code) + ")"}
}

// testSemantics test/[ 命令退出码语义
// 对齐 Python: _test_semantics (bash/_semantics.py L174-179)
func testSemantics(code int) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false, Message: "Condition is true"}
	}
	if code == 1 {
		return ExitCodeMeaning{IsError: false, Message: "Condition is false"}
	}
	return ExitCodeMeaning{IsError: true, Message: "test error (exit " + itoa(code) + ")"}
}

// psReadSemantics PowerShell 读取命令退出码语义
// 对齐 Python: _powershell_read_semantics (bash/_semantics.py L182-187)
func psReadSemantics(code int, stderr string) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false}
	}
	if code == 1 && strings.TrimSpace(stderr) == "" {
		return ExitCodeMeaning{IsError: false, Message: "No output returned"}
	}
	return ExitCodeMeaning{IsError: true, Message: "PowerShell read command error (exit " + itoa(code) + ")"}
}

// psGetChildItemSemantics Get-ChildItem 命令退出码语义
// 对齐 Python: _get_child_item_semantics (powershell/_semantics.py L217-222)
func psGetChildItemSemantics(code int, stdout, stderr string) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false}
	}
	if code == 1 && stdout != "" && stderr == "" {
		return ExitCodeMeaning{IsError: false, Message: "Partial results produced; some items may be inaccessible"}
	}
	return ExitCodeMeaning{IsError: true, Message: "Get-ChildItem error (exit " + itoa(code) + ")"}
}

// psSearchSemantics 搜索命令退出码语义
// 对齐 Python: _search_semantics (powershell/_semantics.py L225-230)
func psSearchSemantics(code int, stdout, stderr string) ExitCodeMeaning {
	if code == 0 {
		return ExitCodeMeaning{IsError: false}
	}
	if code == 1 && stdout == "" && stderr == "" {
		return ExitCodeMeaning{IsError: false, Message: "No matches found"}
	}
	return ExitCodeMeaning{IsError: true, Message: "Search command error (exit " + itoa(code) + ")"}
}

// buildKindLookup 构建命令分类查找表
func buildKindLookup(search, read, list, neutral, silent map[string]bool) map[string]CommandKind {
	lookup := make(map[string]CommandKind)
	for cmd := range search {
		lookup[cmd] = CommandKindSearch
	}
	for cmd := range read {
		lookup[cmd] = CommandKindRead
	}
	for cmd := range list {
		lookup[cmd] = CommandKindList
	}
	for cmd := range neutral {
		lookup[cmd] = CommandKindNeutral
	}
	for cmd := range silent {
		lookup[cmd] = CommandKindSilent
	}
	return lookup
}

// itoa 整数转字符串（避免导入 strconv 的轻量替代）
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
