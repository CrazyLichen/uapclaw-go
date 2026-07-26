package shell

import (
	"regexp"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SecurityCheckResult 安全检查结果
type SecurityCheckResult struct {
	// Blocked 是否被拦截
	Blocked bool
	// Reason 拦截原因
	Reason string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// bash 注入检测正则（不含 lookbehind，Go RE2 不支持）
	// 反引号命令替换: `...` — 需在代码中排除被单引号包裹的
	bashBacktickRe    = regexp.MustCompile("`[^`]+`")
	bashDollarParenRe = regexp.MustCompile(`\$\(`)
	bashProcSubstRe   = regexp.MustCompile(`[<>]\(`)

	// powershell 注入检测正则
	psInvokeExprRe  = regexp.MustCompile(`(?i)\b(?:invoke-expression|iex)\b`)
	psEncodedCmdRe  = regexp.MustCompile(`(?i)\b(?:powershell|powershell\.exe|pwsh|pwsh\.exe)\b[^\n]*-encodedcommand\b`)
	psDynamicCallRe = regexp.MustCompile(`(^|[\s;(])&\s*(?:\(|\$)`)
	psScriptBlockRe = regexp.MustCompile(`(?i)\[scriptblock\]::create\s*\(`)

	// bash 破坏性命令正则
	// 对齐 Python: _DESTRUCTIVE_PATTERNS (bash/_security.py L54-68)
	bashDestructivePatterns = []struct {
		pattern *regexp.Regexp
		warning string
	}{
		{regexp.MustCompile(`\bgit\s+reset\s+--hard\b`), "可能丢弃未提交的更改"},
		{regexp.MustCompile(`\bgit\s+push\b[^\n]*(?:--force|-f)\b`), "可能覆盖远程历史"},
		{regexp.MustCompile(`\bgit\s+clean\s+-[a-zA-Z]*f`), "可能永久删除未跟踪的文件"},
		{regexp.MustCompile(`\bgit\s+checkout\s+--\s+\.`), "可能丢弃所有未暂存的更改"},
		{regexp.MustCompile(`\bgit\s+stash\s+(?:drop|clear)\b`), "可能永久丢弃暂存的更改"},
		{regexp.MustCompile(`\bgit\s+branch\s+-D\b`), "可能强制删除分支"},
		{regexp.MustCompile(`\bgit\s+commit\s+--amend\b`), "可能重写最近一次提交"},
		{regexp.MustCompile(`\bgit\s+(?:push|commit|merge)\b[^\n]*--no-verify\b`), "可能跳过安全钩子"},
		{regexp.MustCompile(`(?i)\bDROP\s+(?:TABLE|DATABASE)\b`), "可能删除数据库对象"},
		{regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`), "可能截断数据库表"},
		{regexp.MustCompile(`\bkubectl\s+delete\b`), "可能删除 Kubernetes 资源"},
		{regexp.MustCompile(`\bterraform\s+destroy\b`), "可能销毁 Terraform 基础设施"},
		{regexp.MustCompile(`\bsudo\b`), "sudo 在非交互模式下可能需要密码；请配置 NOPASSWD 或以 root 运行"},
	}

	// powershell 破坏性命令正则
	// 对齐 Python: _DESTRUCTIVE_PATTERNS (powershell/_security.py L36-45)
	psDestructivePatterns = []struct {
		pattern *regexp.Regexp
		warning string
	}{
		{regexp.MustCompile(`(?i)\bremove-item\b[^\n]*-(?:recurse|force)\b`), "可能永久删除文件或目录"},
		{regexp.MustCompile(`(?i)\bclear-content\b`), "可能删除文件内容"},
		{regexp.MustCompile(`(?i)\bset-content\b`), "可能覆盖文件内容"},
		{regexp.MustCompile(`(?i)\brename-item\b`), "可能重命名或替换文件"},
		{regexp.MustCompile(`(?i)\bmove-item\b`), "可能移动或覆盖文件"},
		{regexp.MustCompile(`\bgit\s+reset\s+--hard\b`), "可能丢弃未提交的更改"},
		{regexp.MustCompile(`\bgit\s+push\b[^\n]*(?:--force|-f)\b`), "可能覆盖远程历史"},
		{regexp.MustCompile(`\bgit\s+commit\s+--amend\b`), "可能重写最近一次提交"},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CheckBashInjection 检测 bash shell 注入。
// 对齐 Python: check_injection (bash/_security.py L40-49)
// 检测 3 种模式:
// 1. 反引号命令替换 `...` (排除被单引号包裹的)
// 2. $() 命令替换
// 3. 进程替换 <() 或 >()
func CheckBashInjection(command string) (bool, string) {
	if hasBacktickSubstitution(command) {
		return true, "检测到 Shell 注入：反引号命令替换"
	}
	if bashDollarParenRe.MatchString(command) {
		return true, "检测到 Shell 注入：$() 命令替换"
	}
	if bashProcSubstRe.MatchString(command) {
		return true, "检测到 Shell 注入：进程替换 <() 或 >()"
	}
	return false, ""
}

// CheckPowerShellInjection 检测 PowerShell 注入。
// 对齐 Python: check_injection (powershell/_security.py L28-33)
// 检测 4 种模式:
// 1. Invoke-Expression / iex（调用表达式）
// 2. powershell/pwsh -EncodedCommand（编码命令执行）
// 3. & ( 或 & $（动态调用运算符）
// 4. [ScriptBlock]::Create(（脚本块创建）
func CheckPowerShellInjection(command string) (bool, string) {
	if psInvokeExprRe.MatchString(command) {
		return true, "检测到 PowerShell 注入：Invoke-Expression"
	}
	if psEncodedCmdRe.MatchString(command) {
		return true, "检测到 PowerShell 注入：嵌套编码命令"
	}
	if psDynamicCallRe.MatchString(command) {
		return true, "检测到 PowerShell 注入：动态调用运算符"
	}
	if psScriptBlockRe.MatchString(command) {
		return true, "检测到 PowerShell 注入：动态 ScriptBlock 创建"
	}
	return false, ""
}

// GetBashDestructiveWarning 返回 bash 破坏性命令警告。
// 对齐 Python: get_destructive_warning (bash/_security.py L71-81)
// 纯信息性，不阻止执行。
func GetBashDestructiveWarning(command string) string {
	for _, dp := range bashDestructivePatterns {
		if dp.pattern.MatchString(command) {
			return dp.warning
		}
	}
	return ""
}

// GetPSDestructiveWarning 返回 PowerShell 破坏性命令警告。
// 对齐 Python: get_destructive_warning (powershell/_security.py L48-53)
// 纯信息性，不阻止执行。
func GetPSDestructiveWarning(command string) string {
	for _, dp := range psDestructivePatterns {
		if dp.pattern.MatchString(command) {
			return dp.warning
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasBacktickSubstitution 检测反引号命令替换，排除被单引号包裹的
// 对齐 Python: _BACKTICK_RE = re.compile(r"(?<!')`[^`]+`")
// 由于 Go RE2 不支持 lookbehind，手动实现：遍历命令，跟踪单引号状态
func hasBacktickSubstitution(command string) bool {
	inSingleQuote := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if ch == '\'' {
			inSingleQuote = !inSingleQuote
		} else if ch == '`' && !inSingleQuote {
			// 找到不在单引号内的开反引号，寻找匹配的闭反引号
			for j := i + 1; j < len(command); j++ {
				if command[j] == '`' {
					// 找到匹配的闭反引号，且中间有内容 → 检测到命令替换
					if j > i+1 {
						return true
					}
					// 空反引号 `` → 继续搜索
					i = j
					break
				}
			}
		}
	}
	return false
}
