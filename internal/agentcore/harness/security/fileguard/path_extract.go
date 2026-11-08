package fileguard

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/anmitsu/go-shlex"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/toolguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FileAccess 一次路径访问的抽取结果。
//
// Python: extract_accesses_native 返回的 (path, action, source) 三元组
type FileAccess struct {
	// Path 归整后的绝对路径
	Path string
	// Action 访问动作
	Action FileAction
	// Source 抽取来源："tool_arg"（来自注册 spec 或 PathTools）或 "shlex"（shell 解析）
	Source string
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// separatorResidueRe 匹配 token 末尾的 shell 分隔符残留
	separatorResidueRe = regexp.MustCompile(`[;|&]+$`)
	// ntCmdSwitchBodyRe 匹配 cmd.exe 开关体（如 b / s/q）
	ntCmdSwitchBodyRe = regexp.MustCompile(`^[A-Za-z]{1,2}(?::[^\s/\\]+)?$`)
	// fdAliasRe 匹配 `&N` 形式的 fd 别名
	fdAliasRe = regexp.MustCompile(`^&\d+$`)
	// unexpandedVarRe 匹配未展开的 shell 变量（$VAR / ${VAR} / $env:VAR）
	unexpandedVarRe = regexp.MustCompile(`(?i)\$(\w+|\{[^}]+\}|env:\w+)`)
	// quotedSpanRe 匹配完整的一对单/双引号（含转义）
	quotedSpanRe = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'`)
	// winAbsRe 匹配 Windows 盘符绝对路径
	winAbsRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	// winUserProfileRe 匹配 %USERPROFILE%
	winUserProfileRe = regexp.MustCompile(`(?i)%USERPROFILE%`)
	// winHomeDrivePathRe 匹配 %HOMEDRIVE%%HOMEPATH%
	winHomeDrivePathRe = regexp.MustCompile(`(?i)%HOMEDRIVE%%HOMEPATH%`)
	// redirectTargetRe 匹配重定向运算符与目标
	redirectTargetRe = regexp.MustCompile(`(?:^|[\s;|&])(\d*>>?|\d*<|&>)\s*([^\s;|&<>]+)`)

	// pathAwareCommands 会访问路径的命令
	pathAwareCommands = map[string]struct{}{
		"cd": {}, "rm": {}, "cp": {}, "mv": {}, "mkdir": {}, "touch": {},
		"chmod": {}, "chown": {}, "cat": {}, "ls": {}, "dir": {}, "type": {},
		"del": {}, "erase": {}, "rd": {}, "rmdir": {}, "copy": {}, "move": {},
		"md": {}, "head": {}, "tail": {}, "more": {}, "less": {}, "vim": {},
		"nano": {}, "gedit": {}, "notepad": {},
		"get-content": {}, "gc": {},
		"set-content": {}, "add-content": {}, "out-file": {}, "tee-object": {},
		"sc": {}, "remove-item": {}, "ri": {}, "new-item": {}, "ni": {},
	}
	// interpreterBasenames 解释器类命令（脚本首参需按 exec 记）
	interpreterBasenames = map[string]struct{}{
		"python": {}, "python3": {}, "pythonw": {}, "py": {},
		"node": {}, "nodejs": {}, "bash": {}, "sh": {}, "dash": {},
		"zsh": {}, "fish": {}, "pwsh": {}, "powershell": {},
	}
	// ntCmdSwitchCommands 在 POSIX 上仍按 cmd.exe 语义解释开关的命令
	ntCmdSwitchCommands = map[string]struct{}{
		"cd": {}, "dir": {}, "del": {}, "erase": {}, "copy": {}, "move": {},
		"rd": {}, "rmdir": {}, "md": {}, "type": {}, "ren": {}, "rename": {},
		"xcopy": {}, "attrib": {}, "cmd": {}, "notepad": {},
	}
	// readCmds 只读类命令
	readCmds = map[string]struct{}{
		"cat": {}, "ls": {}, "dir": {}, "type": {}, "head": {}, "tail": {},
		"more": {}, "less": {}, "get-content": {}, "gc": {},
	}
	// writeCmds 写入类命令
	writeCmds = map[string]struct{}{
		"rm": {}, "mkdir": {}, "touch": {}, "chmod": {}, "chown": {},
		"del": {}, "erase": {}, "rd": {}, "rmdir": {}, "md": {},
		"set-content": {}, "add-content": {}, "out-file": {}, "tee-object": {},
		"sc": {}, "remove-item": {}, "ri": {}, "new-item": {}, "ni": {},
	}
	// psPathFlags PowerShell 的路径参数开关
	psPathFlags = map[string]struct{}{"-path": {}, "-literalpath": {}, "-filepath": {}}
	// transferCmds 传输类命令（首参读、其余写）
	transferCmds = map[string]struct{}{"cp": {}, "copy": {}, "mv": {}, "move": {}}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractAccessesNative 抽取一次工具调用的全部路径访问。
//
// 三个分支按顺序判定（顺序不可调换）：
//  1. shell 工具 → 解析命令行（source="shlex"）
//  2. 有注册 spec → 按 spec 的 arg_name/action 逐项解析（source="tool_arg"）
//  3. 是 PathTools 成员 → 遍历路径字符串，读写由 WritePathTools 决定
//
// Python: extract_accesses_native(tool_name, tool_args, workspace, permission_config=None)
func ExtractAccessesNative(toolName string, toolArgs map[string]any, workspace string, permissionConfig map[string]any) []FileAccess {
	out := make([]FileAccess, 0, 4)

	// 1. shell 工具
	if isShellTool(toolName, permissionConfig) {
		workdir := resolveShellWorkdir(toolArgs, workspace)
		cmd := commandFromArgs(toolArgs)
		for _, acc := range extractShellPathAccesses(cmd, workdir, true) {
			acc.Source = "shlex"
			out = append(out, acc)
		}
		return out
	}

	// 2. 注册 spec
	if specs := specsForTool(toolName); len(specs) > 0 {
		for _, spec := range specs {
			raw, ok := toolArgs[spec.ArgName].(string)
			if !ok || strings.TrimSpace(raw) == "" {
				continue
			}
			resolved := resolvePathStr(raw, workspace)
			if resolved == "" {
				continue
			}
			out = append(out, FileAccess{Path: resolved, Action: spec.Action, Source: "tool_arg"})
		}
		return out
	}

	// 3. PathTools 兜底
	if security.PathTools[toolName] {
		action := FileActionRead
		if security.WritePathTools[toolName] {
			action = FileActionWrite
		}
		for _, s := range security.IterPathStrings(toolName, toolArgs) {
			resolved := resolvePathStr(s, workspace)
			if resolved == "" {
				continue
			}
			out = append(out, FileAccess{Path: resolved, Action: action, Source: "tool_arg"})
		}
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isShellTool 判定工具是否属 shell 类。
//
// Python: is_shell_tool(tool_name, shell_tools_from_config(permission_config))
func isShellTool(toolName string, permissionConfig map[string]any) bool {
	// 宿主可通过 permissions.categories.shell 覆盖默认集合
	if cfg, ok := permissionConfig["categories"].(map[string]any); ok {
		if names, ok := cfg["shell"].([]any); ok && len(names) > 0 {
			for _, n := range names {
				if s, ok := n.(string); ok && strings.TrimSpace(s) == toolName {
					return true
				}
			}
			return false
		}
	}
	return security.ShellTools[toolName] || toolName == "shell"
}

// resolveShellWorkdir 解析 shell 命令的工作目录。
//
// workdir 参数优先；绝对路径（含 Windows 盘符）直接采用，相对路径按
// workspace 拼接；无 workdir 时用 workspace。
//
// Python: extract_accesses_native 的 workdir 处理段
func resolveShellWorkdir(toolArgs map[string]any, workspace string) string {
	if raw, ok := toolArgs["workdir"].(string); ok && strings.TrimSpace(raw) != "" {
		if isWindowsAbsText(raw) || filepath.IsAbs(raw) {
			return resolveExtractPath(raw)
		}
		return resolveExtractPath(filepath.Join(workspace, raw))
	}
	return resolveExtractPath(workspace)
}

// commandFromArgs 从 args 取命令行（command 优先，其次 cmd）。
//
// Python: str(tool_args.get("command", "") or tool_args.get("cmd", ""))
func commandFromArgs(toolArgs map[string]any) string {
	if v, ok := toolArgs["command"].(string); ok && v != "" {
		return v
	}
	if v, ok := toolArgs["cmd"].(string); ok {
		return v
	}
	return ""
}

// specsForTool 查询工具的参数约定。
//
// Python: _specs_for_tool(tool_name)
func specsForTool(toolName string) []FileToolSpec {
	return LookupFileToolSpecs(toolName)
}

// isWindowsAbsText 判定是否为 Windows 绝对路径文本（盘符或 UNC）。
//
// Python: _is_windows_abs_text(text)
func isWindowsAbsText(text string) bool {
	s := strings.Trim(strings.TrimSpace(text), `"`)
	s = strings.Trim(s, `'`)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, `\\`) {
		return true
	}
	return winAbsRe.MatchString(s)
}

// resolveExtractPath 把路径解析为绝对路径；POSIX 上保留 Windows 盘符路径原样。
//
// Python: _resolve_extract_path(path)
func resolveExtractPath(p string) string {
	if runtime.GOOS != "windows" && isWindowsAbsText(p) {
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(p)
}

// pathIsAbs 判定是否为绝对路径（含 Windows 盘符形式）。
//
// 与 filepath.IsAbs 的差别：Linux 上 filepath.IsAbs("C:/x") 为 false，
// 而 Windows 盘符路径在语义上是绝对的，需单独识别。
func pathIsAbs(p string) bool {
	if isWindowsAbsText(p) {
		return true
	}
	return filepath.IsAbs(p)
}

// resolvePathStr 把一个原始路径字符串解析为绝对路径，失败返回空串。
//
// Python: _resolve_path_str(raw, workspace)
func resolvePathStr(raw string, workspace string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, `"`)
	text = strings.Trim(text, `'`)
	if text == "" {
		return ""
	}
	expanded := expandHomeToken(text)
	if !pathIsAbs(expanded) {
		expanded = filepath.Join(workspace, expanded)
	}
	return resolveExtractPath(expanded)
}

// ntCmdExeSwitchToken 判定是否为 cmd.exe 开关（如 /b、/d、/s/q）。
//
// Linux 上仍会见到 Windows 的 dir /b、cd /d（经 cmd //c 传入），对这些
// cmd 内建命令把 token 当开关处理；POSIX 工具如 ls /b、cat /tmp 仍按路径。
//
// Python: _nt_cmd_exe_switch_token(stripped, *, cmd0)
func ntCmdExeSwitchToken(stripped, cmd0 string) bool {
	if !strings.HasPrefix(stripped, "/") || strings.HasPrefix(stripped, "//") {
		return false
	}
	if strings.Contains(stripped, `\`) {
		return false
	}
	parts := make([]string, 0, 2)
	for _, p := range strings.Split(stripped, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !ntCmdSwitchBodyRe.MatchString(p) {
			return false
		}
	}
	if runtime.GOOS == "windows" {
		return true
	}
	_, hit := ntCmdSwitchCommands[cmd0]
	return hit
}

// looksLikePath 判定一个 token 是否像路径。
//
// Python: _looks_like_path(token)
func looksLikePath(token string) bool {
	t := strings.Trim(strings.TrimSpace(token), `"`)
	t = strings.Trim(t, `'`)
	if ntCmdExeSwitchToken(t, "") {
		return false
	}
	if t == "." || t == ".." || t == "~" ||
		strings.HasPrefix(t, "~/") || strings.HasPrefix(t, `~\`) {
		return true
	}
	if strings.HasPrefix(t, `\\`) || strings.HasPrefix(t, "./") || strings.HasPrefix(t, "../") {
		return true
	}
	if winAbsRe.MatchString(t) {
		return true
	}
	return strings.Contains(t, `\`) || strings.Contains(t, "/")
}

// joinHomeRest 把 home 目录与剩余路径拼接。
//
// Python: _join_home_rest(rest)
func joinHomeRest(rest string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return rest
	}
	normalized := strings.ReplaceAll(rest, `\`, "/")
	normalized = strings.TrimLeft(normalized, "/")
	parts := make([]string, 0, 4)
	for _, p := range strings.Split(normalized, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return home
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

// unglueHomeHidden 处理 home 与隐藏目录名粘连（丢失分隔符）的情况。
//
// Python: _unglue_home_hidden(path_text)
func unglueHomeHidden(pathText string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return pathText
	}
	home = strings.TrimSuffix(filepath.ToSlash(home), "/")
	text := strings.ReplaceAll(pathText, `\`, "/")
	prefix := home + "."
	if strings.HasPrefix(text, prefix) {
		return home + "/" + text[len(home):]
	}
	return text
}

// expandHomeToken 展开 ~ / ~/ / ~\ 与 Windows 的 home 环境变量。
//
// Python: _expand_home_token(tok)
func expandHomeToken(tok string) string {
	t := strings.Trim(strings.TrimSpace(tok), `"`)
	t = strings.Trim(t, `'`)
	if t == "" {
		return t
	}
	if t == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return t
	}
	if strings.HasPrefix(t, "~/") || strings.HasPrefix(t, `~\`) || strings.HasPrefix(t, "~.") {
		return joinHomeRest(t[1:])
	}
	expanded := t
	if home, err := os.UserHomeDir(); err == nil {
		homePosix := filepath.ToSlash(home)
		expanded = winHomeDrivePathRe.ReplaceAllString(expanded, homePosix)
		expanded = winUserProfileRe.ReplaceAllString(expanded, homePosix)
	}
	if expanded == t {
		return t
	}
	return unglueHomeHidden(strings.ReplaceAll(expanded, `\`, "/"))
}

// looksLikeRedirectTarget 判定是否为可用的重定向目标（排除 fd 别名与变量）。
//
// Python: _looks_like_redirect_target(token)
func looksLikeRedirectTarget(token string) bool {
	t := strings.Trim(strings.TrimSpace(token), `"`)
	t = strings.Trim(t, `'`)
	if t == "" || fdAliasRe.MatchString(t) {
		return false
	}
	if strings.HasPrefix(t, "$") || unexpandedVarRe.MatchString(t) {
		return false
	}
	return true
}

// isShellFlagToken 判定是否为命令开关（-x / --long）。
//
// Python: _is_shell_flag_token(tok, *, cmd0)
func isShellFlagToken(tok, cmd0 string) bool {
	t := strings.Trim(strings.TrimSpace(tok), `"`)
	t = strings.Trim(t, `'`)
	if t == "" {
		return false
	}
	if ntCmdExeSwitchToken(t, cmd0) {
		return true
	}
	if !strings.HasPrefix(t, "-") {
		return false
	}
	// 单独的 "-" 是 stdin 占位，不是开关
	return t != "-"
}

// stripSeparatorResidue 去掉粘连在路径上的 shell 分隔符（如 `file;`）。
//
// 引号内或反斜杠转义的分隔符属于文件名，保留。
//
// Python: _strip_separator_residue(token)
func stripSeparatorResidue(token string) string {
	text := strings.TrimSpace(token)
	if len(text) >= 2 && text[0] == text[len(text)-1] && (text[0] == '\'' || text[0] == '"') {
		return text[1 : len(text)-1]
	}
	if len(text) >= 2 {
		last := text[len(text)-1]
		if (last == ';' || last == '|' || last == '&') && text[len(text)-2] == '\\' {
			return text[:len(text)-2] + string(last)
		}
	}
	stripped := separatorResidueRe.ReplaceAllString(text, "")
	if stripped == "" {
		return text
	}
	return stripped
}

// basenameLower 取命令名并小写去路径去 .exe 后缀。
//
// Python: _basename_lower(cmd)
func basenameLower(cmd string) string {
	text := strings.TrimSpace(cmd)
	if text == "" {
		return ""
	}
	if fields := strings.Fields(text); len(fields) > 0 {
		text = fields[0]
	}
	text = strings.ReplaceAll(text, `\`, "/")
	if idx := strings.LastIndex(text, "/"); idx >= 0 {
		text = text[idx+1:]
	}
	return strings.TrimSuffix(strings.ToLower(text), ".exe")
}

// quotedSpans 返回命令里所有完整引号对的 [start, end) 区间。
//
// Python: _quoted_spans(command)
func quotedSpans(command string) [][2]int {
	matches := quotedSpanRe.FindAllStringIndex(command, -1)
	out := make([][2]int, 0, len(matches))
	for _, m := range matches {
		out = append(out, [2]int{m[0], m[1]})
	}
	return out
}

// indexInQuotedSpan 判定下标是否严格落在某个引号区间内（不含端点）。
//
// Python: _index_in_quoted_span(index, spans)
func indexInQuotedSpan(index int, spans [][2]int) bool {
	for _, sp := range spans {
		if index > sp[0] && index < sp[1] {
			return true
		}
	}
	return false
}

// ampersandIsRedirect 判定 `&` 是重定向（>& / <& / &>）而非命令分隔符。
//
// Python: _ampersand_is_redirect(command, index)
func ampersandIsRedirect(command string, index int) bool {
	prev := byte(0)
	if index > 0 {
		prev = command[index-1]
	}
	next := byte(0)
	if index+1 < len(command) {
		next = command[index+1]
	}
	return prev == '<' || prev == '>' || next == '>'
}

// splitUnquotedCommandSegments 按未在引号内的 ; & | 分段，`\;` 保段内。
//
// Python: _split_unquoted_command_segments(command)
func splitUnquotedCommandSegments(command string) []string {
	spans := quotedSpans(command)
	var parts []string
	var buf strings.Builder
	index := 0
	length := len(command)
	for index < length {
		if indexInQuotedSpan(index, spans) {
			buf.WriteByte(command[index])
			index++
			continue
		}
		char := command[index]
		if char == '\\' && index+1 < length &&
			(command[index+1] == ';' || command[index+1] == '|' || command[index+1] == '&') {
			buf.WriteString(command[index : index+2])
			index += 2
			continue
		}
		if char == '&' && ampersandIsRedirect(command, index) {
			buf.WriteByte(char)
			index++
			continue
		}
		if char == ';' || char == '|' || char == '&' {
			// || 与 && 是单个运算符，跳过一个重复字符
			if index+1 < length && command[index+1] == char && char != ';' {
				index++
			}
			parts = append(parts, buf.String())
			buf.Reset()
			index++
			continue
		}
		buf.WriteByte(char)
		index++
	}
	parts = append(parts, buf.String())

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// pathAwareOneSegment 抽取单条命令段的路径访问，并返回新的 cwd（仅 cd）。
//
// 命令分派顺序（对齐 Python，不可调换）：
//  1. 传输类（cp/copy/mv/move）且 >= 2 个路径 → 首参 read，其余 write
//  2. 写入类 → 全部 write
//  3. 读取类 → 全部 read；无路径的 ls/dir 记 cwd
//  4. cd → 路径 read 且更新 cwd；无参时用 home
//  5. 其他 → 全部 write
//
// Python: _path_aware_one_segment(segment, cwd, *, include_cd_reads=True)
func pathAwareOneSegment(segment, cwd string, includeCDReads bool) ([]FileAccess, string) {
	tokens := shlexSplit(segment)
	if len(tokens) == 0 {
		return nil, ""
	}
	cmd0 := basenameLower(tokens[0])
	if _, hit := pathAwareCommands[cmd0]; !hit {
		return nil, ""
	}
	base := resolveExtractPath(cwd)
	pathTokens := make([]string, 0, len(tokens))

	appendPathToken := func(tok string, requirePathShape bool) {
		tok = stripSeparatorResidue(tok)
		if tok == "" || unexpandedVarRe.MatchString(tok) {
			return
		}
		if requirePathShape && !looksLikePath(tok) {
			return
		}
		tok = expandHomeToken(tok)
		winAbs := isWindowsAbsText(tok)
		resolved := tok
		if !pathIsAbs(tok) {
			resolved = filepath.Join(base, tok)
		}
		if winAbs && runtime.GOOS != "windows" {
			pathTokens = append(pathTokens, resolved)
			return
		}
		pathTokens = append(pathTokens, resolveExtractPath(resolved))
	}

	for _, tok := range tokens[1:] {
		if strings.TrimSpace(tok) == "" || isShellFlagToken(tok, cmd0) {
			continue
		}
		appendPathToken(tok, true)
	}

	// PowerShell 特有：get-content / set-content 等用 -Path 显式给路径
	if psCoversCommand(cmd0) {
		for _, extra := range collectPSPathTokens(tokens) {
			appendPathToken(extra, false)
		}
	}

	var results []FileAccess
	newCwd := ""

	switch {
	case isTransferCmd(cmd0) && len(pathTokens) >= 2:
		results = append(results, FileAccess{Path: pathTokens[0], Action: FileActionRead})
		for _, p := range pathTokens[1:] {
			results = append(results, FileAccess{Path: p, Action: FileActionWrite})
		}
	case isWriteCmd(cmd0):
		for _, p := range pathTokens {
			results = append(results, FileAccess{Path: p, Action: FileActionWrite})
		}
	case isReadCmd(cmd0):
		if len(pathTokens) == 0 && (cmd0 == "dir" || cmd0 == "ls") {
			results = append(results, FileAccess{Path: resolveExtractPath(base), Action: FileActionRead})
		} else {
			for _, p := range pathTokens {
				results = append(results, FileAccess{Path: p, Action: FileActionRead})
			}
		}
	case cmd0 == "cd":
		if len(pathTokens) > 0 {
			if includeCDReads {
				for _, p := range pathTokens {
					results = append(results, FileAccess{Path: p, Action: FileActionRead})
				}
			}
			newCwd = pathTokens[len(pathTokens)-1]
		} else if home, err := os.UserHomeDir(); err == nil {
			resolved := resolveExtractPath(home)
			results = append(results, FileAccess{Path: resolved, Action: FileActionRead})
			newCwd = resolved
		}
	default:
		for _, p := range pathTokens {
			results = append(results, FileAccess{Path: p, Action: FileActionWrite})
		}
	}
	return results, newCwd
}

// psCoversCommand 判定命令是否走 PowerShell 的 -Path 参数抽取。
//
// Python: _path_aware_one_segment 内的 cmd0 in (...) 判断
func psCoversCommand(cmd0 string) bool {
	switch cmd0 {
	case "get-content", "gc", "set-content", "add-content", "out-file",
		"tee-object", "sc", "remove-item", "ri", "new-item", "ni":
		return true
	}
	return false
}

// isTransferCmd 判定是否为传输类命令。
func isTransferCmd(cmd0 string) bool {
	_, hit := transferCmds[cmd0]
	return hit
}

// isWriteCmd 判定是否为写入类命令。
func isWriteCmd(cmd0 string) bool {
	_, hit := writeCmds[cmd0]
	return hit
}

// isReadCmd 判定是否为只读类命令。
func isReadCmd(cmd0 string) bool {
	_, hit := readCmds[cmd0]
	return hit
}

// extractPathAwareCommandAccesses 逐段抽取路径访问，cd 会切换后续段的 cwd。
//
// Python: extract_path_aware_command_accesses(command, workdir, *, include_cd_reads=True)
func extractPathAwareCommandAccesses(command, workdir string, includeCDReads bool) []FileAccess {
	if command == "" {
		return nil
	}
	command = toolguard.CanonicalizeShellCommandForPermission(command)
	cwd := resolveExtractPath(workdir)
	var combined []FileAccess
	for _, seg := range splitUnquotedCommandSegments(command) {
		part, newCwd := pathAwareOneSegment(seg, cwd, includeCDReads)
		combined = append(combined, part...)
		if newCwd != "" {
			cwd = newCwd
		}
	}
	return combined
}

// extractShellPathAccesses shell 抽取总入口：脱壳 → 分段抽取 → 重定向 → 解释器脚本。
//
// 三个来源按顺序追加（对齐 Python）：
//  1. extractPathAwareCommandAccesses 的逐段结果
//  2. 重定向目标（`>` 写、`<` 读），跳过引号内的运算符
//  3. 解释器的首个脚本参数（exec）
//
// Python: extract_shell_path_accesses(command, workdir, *, include_cd_reads=True)
func extractShellPathAccesses(command, workdir string, includeCDReads bool) []FileAccess {
	if command == "" {
		return nil
	}
	command = toolguard.CanonicalizeShellCommandForPermission(command)
	base := resolveExtractPath(workdir)
	var results []FileAccess

	resolve := func(tok string, redirect bool) string {
		tok = stripSeparatorResidue(tok)
		if redirect {
			if !looksLikeRedirectTarget(tok) {
				return ""
			}
		} else if tok == "" || !looksLikePath(tok) {
			return ""
		}
		tok = expandHomeToken(tok)
		winAbs := isWindowsAbsText(tok)
		resolved := tok
		if !pathIsAbs(tok) {
			resolved = filepath.Join(base, tok)
		}
		if winAbs && runtime.GOOS != "windows" {
			return resolved
		}
		return resolveExtractPath(resolved)
	}

	// 1. 逐段路径抽取
	results = append(results, extractPathAwareCommandAccesses(command, workdir, includeCDReads)...)

	// 2. 重定向目标
	spans := quotedSpans(command)
	for _, m := range redirectTargetRe.FindAllStringSubmatchIndex(command, -1) {
		if indexInQuotedSpan(m[2], spans) {
			continue
		}
		op := command[m[2]:m[3]]
		target := command[m[4]:m[5]]
		resolved := resolve(target, true)
		if resolved == "" {
			continue
		}
		if strings.Contains(op, "<") && !strings.Contains(op, ">") {
			results = append(results, FileAccess{Path: resolved, Action: FileActionRead})
		} else {
			results = append(results, FileAccess{Path: resolved, Action: FileActionWrite})
		}
	}

	// 3. 解释器的脚本参数按 exec 记
	tokens := shlexSplit(command)
	if len(tokens) >= 2 {
		cmd0 := basenameLower(tokens[0])
		if _, isInterp := interpreterBasenames[cmd0]; isInterp {
			scriptTok := strings.TrimSpace(tokens[1])
			if scriptTok != "" && !strings.HasPrefix(scriptTok, "-") {
				if resolved := resolve(scriptTok, false); resolved != "" {
					results = append(results, FileAccess{Path: resolved, Action: FileActionExec})
				}
			}
		}
	}
	return results
}

// collectPSPathTokens 收集 PowerShell 的 -Path / -LiteralPath / -FilePath 参数值。
//
// Python: _collect_ps_path_tokens(tokens)
func collectPSPathTokens(tokens []string) []string {
	var out []string
	for idx := 1; idx < len(tokens); idx++ {
		raw := strings.Trim(strings.TrimSpace(tokens[idx]), `"`)
		raw = strings.Trim(raw, `'`)
		flag := strings.ToLower(strings.SplitN(raw, ":", 2)[0])
		if _, hit := psPathFlags[flag]; hit && idx+1 < len(tokens) {
			out = append(out, tokens[idx+1])
		}
	}
	return out
}

// shlexSplit 按 posix=false 语义切分（保留引号），失败时退化为空白切分。
//
// Python: shlex.split(segment.strip(), posix=False)
func shlexSplit(s string) []string {
	tokens, err := shlex.Split(s, false)
	if err != nil {
		return strings.Fields(s)
	}
	return tokens
}
