package filesystem

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// ImageExtensions 图片文件扩展名集合
// 对齐 Python: ReadFileTool._IMAGE_EXTENSIONS (filesystem.py L279)
var ImageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
	".tif":  true,
	".tiff": true,
}

// PDFExtensions PDF 文件扩展名集合
var PDFExtensions = map[string]bool{
	".pdf": true,
}

// NotebookExtensions Notebook 文件扩展名集合
var NotebookExtensions = map[string]bool{
	".ipynb": true,
}

// blockedDevicePaths 被阻止的设备路径集合。
// 对齐 Python: _BLOCKED_DEVICE_PATHS (filesystem.py L32-37)
var blockedDevicePaths = map[string]bool{
	"/dev/zero":    true,
	"/dev/random":  true,
	"/dev/urandom": true,
	"/dev/full":    true,
	"/dev/stdin":   true,
	"/dev/tty":     true,
	"/dev/console": true,
	"/dev/stdout":  true,
	"/dev/stderr":  true,
	"/dev/fd/0":    true,
	"/dev/fd/1":    true,
	"/dev/fd/2":    true,
}

// binaryExtensions 二进制文件扩展名集合。
// 对齐 Python: _BINARY_EXTENSIONS (filesystem.py L41-46)
var binaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".bin": true, ".obj": true, ".o": true, ".a": true, ".lib": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
	".pyc": true, ".pyo": true, ".class": true, ".wasm": true,
	".db": true, ".sqlite": true, ".sqlite3": true,
}

// mdExtensions Markdown 文件扩展名集合。
// 对齐 Python: EditFileTool._MD_EXTENSIONS (filesystem.py L1006)
var mdExtensions = map[string]bool{
	".md":  true,
	".mdx": true,
}

// destructivePatterns 破坏性命令正则模式及警告。
// 对齐 Python: get_destructive_warning (bash/_security.py L54-68)
var destructivePatterns = []struct {
	pattern *regexp.Regexp
	warning string
}{
	{regexp.MustCompile(`\bgit\s+reset\s+--hard\b`), "May discard uncommitted changes"},
	{regexp.MustCompile(`\bgit\s+push\b[^\n]*(?:--force|-f)\b`), "May overwrite remote history"},
	{regexp.MustCompile(`\bgit\s+clean\s+-[a-zA-Z]*f`), "May permanently delete untracked files"},
	{regexp.MustCompile(`\bgit\s+checkout\s+--\s+\.`), "May discard all unstaged changes"},
	{regexp.MustCompile(`\bgit\s+stash\s+(?:drop|clear)\b`), "May permanently discard stashed changes"},
	{regexp.MustCompile(`\bgit\s+branch\s+-D\b`), "May force-delete a branch"},
	{regexp.MustCompile(`\bgit\s+commit\s+--amend\b`), "May rewrite the last commit"},
	{regexp.MustCompile(`\bgit\s+(?:push|commit|merge)\b[^\n]*--no-verify\b`), "May skip safety hooks"},
	{regexp.MustCompile(`(?i)\bDROP\s+(?:TABLE|DATABASE)\b`), "May drop database objects"},
	{regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`), "May truncate database table"},
	{regexp.MustCompile(`\bkubectl\s+delete\b`), "May delete Kubernetes resources"},
	{regexp.MustCompile(`\bterraform\s+destroy\b`), "May destroy Terraform infrastructure"},
	{regexp.MustCompile(`\bsudo\b`), "sudo may require a password in non-interactive mode; configure NOPASSWD or run as root"},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExpandBracePattern 展开花括号模式。
// 对齐 Python: GlobTool._expand_brace_pattern (filesystem.py L1428-1444)
// 例如: "*.{py,js}" → ["*.py", "*.js"]
func ExpandBracePattern(pattern string) []string {
	if !strings.ContainsRune(pattern, '{') || !strings.ContainsRune(pattern, '}') {
		return []string{pattern}
	}
	return expandGroup(pattern)
}

// CatN 添加行号格式化（cat -n 风格）。
// 对齐 Python: ReadFileTool._cat_n (filesystem.py L386-391)
func CatN(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%6d\t%s", i+1, line)
	}
	return b.String()
}

// IsBlockedDevice 检查是否为设备路径。
// 对齐 Python: ReadFileTool._is_blocked_device (filesystem.py L311-317)
func IsBlockedDevice(path string) bool {
	if blockedDevicePaths[path] {
		return true
	}
	// Linux /proc/<pid>/fd/0-2 aliases for stdio
	if strings.HasPrefix(path, "/proc/") {
		for _, suffix := range []string{"/fd/0", "/fd/1", "/fd/2"} {
			if strings.HasSuffix(path, suffix) {
				return true
			}
		}
	}
	return false
}

// IsBinaryCandidate 检查是否为二进制文件扩展名。
// 对齐 Python: ReadFileTool._is_binary (filesystem.py L320-322)
// 以及 _is_plain_text_candidate (filesystem.py L408-412)
// 排除图片、PDF、Notebook 后，判断是否为二进制扩展名。
func IsBinaryCandidate(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return binaryExtensions[ext]
}

// StripTrailingWhitespace 去除行尾空白（Markdown 文件除外）。
// 对齐 Python: EditFileTool._strip_trailing_whitespace (filesystem.py L1057-1074)
func StripTrailingWhitespace(content string, isMarkdown bool) string {
	if isMarkdown {
		return content
	}

	if content == "" {
		return ""
	}

	// 按 keepends 方式拆分行
	var parts []string
	var line strings.Builder
	for _, r := range content {
		line.WriteRune(r)
		if r == '\n' {
			parts = append(parts, line.String())
			line.Reset()
		}
	}
	// 最后一个可能没有换行符
	if line.Len() > 0 {
		parts = append(parts, line.String())
	}

	if len(parts) == 0 {
		return strings.TrimRight(content, " \t\r\n")
	}

	var strippedParts []string
	for _, part := range parts {
		body := part
		lineEnding := ""
		if strings.HasSuffix(part, "\r\n") {
			body = part[:len(part)-2]
			lineEnding = "\r\n"
		} else if strings.HasSuffix(part, "\n") {
			body = part[:len(part)-1]
			lineEnding = "\n"
		} else if strings.HasSuffix(part, "\r") {
			body = part[:len(part)-1]
			lineEnding = "\r"
		}
		strippedParts = append(strippedParts, strings.TrimRight(body, " \t")+lineEnding)
	}
	return strings.Join(strippedParts, "")
}

// DetectEOL 检测文件的行尾风格。
// 对齐 Python: EditFileTool._detect_eol (filesystem.py L1043-1044)
func DetectEOL(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// RelativizePaths 将绝对路径列表转为相对于 base 的相对路径。
// 对齐 Python: GlobTool._relativize_paths (filesystem.py L1413-1425)
func RelativizePaths(base string, paths []string) []string {
	// 解析 base 以处理符号链接（如 macOS 的 /var → /private/var）
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		resolvedBase = base
	}

	relativePaths := make([]string, 0, len(paths))
	for _, item := range paths {
		rel, err := filepath.Rel(resolvedBase, item)
		if err != nil {
			relativePaths = append(relativePaths, item)
		} else {
			relativePaths = append(relativePaths, rel)
		}
	}
	return relativePaths
}

// GetDestructiveWarning 检测破坏性命令并返回警告。
// 对齐 Python: get_destructive_warning (bash/_security.py L71-81)
// 纯信息性质，不阻止执行。
func GetDestructiveWarning(command string) string {
	for _, dp := range destructivePatterns {
		if dp.pattern.MatchString(command) {
			return dp.warning
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// braceRe 匹配花括号组内容的正则。
var braceRe = regexp.MustCompile(`\{([^{}]*)\}`)

// expandGroup 递归展开花括号组。
// 对齐 Python: GlobTool._expand_brace_pattern 内的 expand_group (filesystem.py L1433-1443)
func expandGroup(s string) []string {
	match := braceRe.FindStringSubmatchIndex(s)
	if match == nil {
		return []string{s}
	}
	prefix := s[:match[2]]
	suffix := s[match[3]:]
	inner := s[match[2]:match[3]]
	opts := strings.Split(inner, ",")

	var results []string
	for _, opt := range opts {
		results = append(results, expandGroup(prefix+strings.TrimSpace(opt)+suffix)...)
	}
	return results
}
