package toolguard

import (
	"strings"

	"github.com/dlclark/regexp2"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// cmdLauncherRe 匹配 `cmd /c <payload>` 形式的 launcher。
	//
	// 对应 Python _CMD_LAUNCHER_RE。正则原文照抄，仅把命名组语法从
	// Python 的 (?P<name>...) 换成 regexp2 的 (?<name>...)（.NET 风格）。
	cmdLauncherRe = regexp2.MustCompile(
		`(?ix)`+
			`\b(?:cmd(?:\.exe)?)\s+`+
			`(?://c|/c)\s+`+
			`(?:`+
			`"(?<dq>(?:\\.|[^"\\])*)"`+
			`|'(?<sq>(?:\\.|[^'\\])*)'`+
			`|(?<bare>\S+)`+
			`)`, 0)

	// psLauncherRe 匹配 `pwsh|powershell [-开关]* -Command|-c <payload>`。
	//
	// 对应 Python _PS_LAUNCHER_RE。
	psLauncherRe = regexp2.MustCompile(
		`(?ix)`+
			`\b(?:pwsh|powershell(?:\.exe)?)`+
			`(?:\s+-{1,2}[A-Za-z][A-Za-z-]*)*`+
			`\s+(?:-Command|-c)\s+`+
			`(?:`+
			`"(?<dq>(?:\\.|[^"\\])*)"`+
			`|'(?<sq>(?:\\.|[^'\\])*)'`+
			`|(?<bare>\S+)`+
			`)`, 0)

	// shLauncherRe 匹配 POSIX shell launcher（bash|sh|zsh|dash|ash + -c 类开关）。
	//
	// 对应 Python _SH_LAUNCHER_RE。Python 注释里说「variable-width look-behind
	// 在 re 里不编译」所以用了显式交替 (?:^|(?<=[;&|()]))；regexp2 支持定长
	// lookbehind，正则原样可用，无需改写。
	//
	// 前导锚点是行首或分隔符（; & | ( )），分隔符与 launcher 之间的空白由
	// \s* 吃掉。纯空白前缀被有意排除：`echo bash -c 'rm'` 不得被当作 launcher
	// 调用，否则无害的 echo 串会命中 deny/ask 规则。
	shLauncherRe = regexp2.MustCompile(
		`(?ix)`+
			`(?:^|(?<=[;&|()]))`+
			`\s*`+
			`(?:`+
			`"(?:[^"]*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?"`+
			`|'(?:[^']*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?'`+
			`|(?:[^\s;&|()]*[\\/])?(?:bash|sh|zsh|dash|ash)(?:\.exe)?`+
			`)`+
			`(?:\s+-{1,2}[A-Za-z][A-Za-z-]*)*`+
			`\s+-[A-Za-z]*c[A-Za-z]*\s+`+
			`(?:`+
			`"(?<dq>(?:\\.|[^"\\])*)"`+
			`|'(?<sq>(?:\\.|[^'\\])*)'`+
			`|(?<bare>\S+)`+
			`)`, 0)

	// fdAliasTokenRe 匹配 fd 复制标记（如 2>&1、>&2、<3、&1）。
	//
	// 对应 Python _FD_ALIAS_TOKEN_RE。前后各用 (?<!\S) / (?!\S) 确保
	// token 边界——regexp2 支持 lookaround，原文照抄。
	fdAliasTokenRe = regexp2.MustCompile(`(?<!\S)(?:\d+>&\d+|>&\d+|<\d+|&\d+)(?!\S)`, 0)

	// fdAliasFullRe 全匹配版本的 fd 标记（对应 Python 的 re.fullmatch 单模式）。
	fdAliasFullRe = regexp2.MustCompile(`^(?:\d+>&\d+|>&\d+|<\d+|&\d+)$`, 0)

	// leadingSeparatorsRe 匹配行首的空白与分隔符。
	//
	// 对应 Python _LEADING_SEPARATORS_RE。脱壳后残留的 `& ` 会破坏用户规则的
	// 锚定全串通配符匹配，必须去掉。
	leadingSeparatorsRe = regexp2.MustCompile(`^[\s;&|()]+`, 0)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CanonicalizeShellCommandForPermission 返回权限视图的命令副本：脱掉一层 launcher。
//
// 识别三类 launcher：cmd /c、pwsh|powershell -Command|-c、POSIX shell
// （bash/sh/zsh/dash/ash）带 -c 类开关。残留 launcher 会让锚定通配符规则
// （如 `rm *`）与 shell 正则前缀匹配不到内层命令，因此权限视图必须穿透它。
// 脱壳后残留的行首分隔符会被去掉；含真实命令的前缀（如 `cd d && ...`）保留。
//
// 不修改用于执行的 tool_args。不为已损坏的路径补反斜杠。
//
// Python: canonicalize_shell_command_for_permission(command)
func CanonicalizeShellCommandForPermission(command string) string {
	text := strings.TrimSpace(command)
	if text == "" {
		return text
	}

	// 顺序固定：cmd → pwsh → sh。任一命中即返回，只脱一层（count=1）。
	for _, re := range []*regexp2.Regexp{cmdLauncherRe, psLauncherRe, shLauncherRe} {
		if unwrapped, ok := substituteFirstLauncher(re, text); ok {
			return stripLeadingSeparators(strings.TrimSpace(unwrapped))
		}
	}
	return text
}

// StripFdAliasTokens 移除 stderr/stdout 的 fd 复制标记（如 2>&1）。
//
// Python: strip_fd_alias_tokens(command)
func StripFdAliasTokens(command string) string {
	if command == "" {
		return command
	}
	out, err := fdAliasTokenRe.Replace(command, "", -1, -1)
	if err != nil {
		return command
	}
	return out
}

// IsFdAliasToken 判定一个 token 是否为 fd 复制标记。
//
// Python: is_fd_alias_token(token)
func IsFdAliasToken(token string) bool {
	text := strings.TrimSpace(token)
	if text == "" {
		return false
	}
	ok, err := fdAliasFullRe.MatchString(text)
	if err != nil {
		return false
	}
	return ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// substituteFirstLauncher 用 launcherInner 替换正则的首次匹配。
//
// 对应 Python 的 `re.subn(_launcher_inner, text, count=1)`：只替换第一处，
// 第二个返回值表示是否发生过替换。
//
// ⚠️ regexp2 的 Match.Index / Length 是 **rune 索引**，不是字节索引
// （见其 Capture 结构体注释：Even if you pass in a string this will be in
// Runes）。含非 ASCII 的命令（如中文路径）若直接拿 Index 切字符串会错位，
// 因此这里先转 []rune 再切片。
func substituteFirstLauncher(re *regexp2.Regexp, text string) (string, bool) {
	m, err := re.FindStringMatch(text)
	if err != nil || m == nil {
		return "", false
	}
	replacement := launcherInner(m)
	runes := []rune(text)
	start := m.Index
	end := m.Index + m.Length
	if start < 0 || end > len(runes) || start > end {
		return "", false
	}
	return string(runes[:start]) + replacement + string(runes[end:]), true
}

// launcherInner 取匹配里的 payload（双引号 > 单引号 > bare）并去首尾空白。
//
// 若匹配起点是空格/制表符，还原一个空格：shLauncherRe 用 \s* 消耗了分隔符
// 后的空白，不还原会粘成 `cd d &&rm x`。另两条正则锚定 word boundary 不会
// 消耗前导空白，此分支对其为 no-op。
//
// Python: _launcher_inner(match)
func launcherInner(m *regexp2.Match) string {
	inner := ""
	for _, name := range []string{"dq", "sq", "bare"} {
		if g := m.GroupByName(name); g != nil && len(g.Captures) > 0 {
			inner = g.String()
			break
		}
	}
	stripped := strings.TrimSpace(inner)

	whole := m.String()
	if len(whole) > 0 && (whole[0] == ' ' || whole[0] == '\t') {
		return " " + stripped
	}
	return stripped
}

// stripLeadingSeparators 去掉行首的空白与分隔符；全为空时返回原串。
//
// Python: _strip_leading_separators(command)
func stripLeadingSeparators(command string) string {
	stripped, err := leadingSeparatorsRe.Replace(command, "", -1, -1)
	if err != nil || stripped == "" {
		return command
	}
	return stripped
}
