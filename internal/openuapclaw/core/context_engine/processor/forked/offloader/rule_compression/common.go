// 计数/阈值判定/文件路径判定/显示前缀剥离。
// 对应 Python 代码：offloader/rule_compression/common.py（161 行）
package rulecompression

import (
	"encoding/json"
	"regexp"
	"strings"
)

// filePathArgumentKeys 字符串值点名单个文件的参数键。
// Python: common.py:13 (_FILE_PATH_ARGUMENT_KEYS)
var filePathArgumentKeys = []string{"file_path", "filepath", "file", "filename", "path"}

// sourceFileExtensions 内容需逐字保留的文件类型集合（源码/标记/模板/配置/文档）。
// Python: common.py:17 (_SOURCE_FILE_EXTENSIONS, frozenset)
var sourceFileExtensions = map[string]bool{}

// sourceFileNames 无扩展名但按约定属源码的文件名。
// Python: common.py:44 (_SOURCE_FILE_NAMES)
var sourceFileNames = map[string]bool{}

func init() {
	for _, ext := range []string{
		// 程序源码
		".py", ".pyi", ".pyx", ".ipynb",
		".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts",
		".go", ".rs", ".java", ".kt", ".kts", ".scala", ".groovy", ".gradle",
		".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx", ".m", ".mm",
		".cs", ".fs", ".vb", ".swift", ".dart", ".rb", ".php", ".pl", ".pm",
		".lua", ".r", ".jl", ".ex", ".exs", ".erl", ".hs", ".clj", ".elm",
		".zig", ".nim", ".v", ".sol", ".proto", ".thrift", ".graphql", ".gql",
		".sql", ".sh", ".bash", ".zsh", ".fish", ".ps1", ".psm1", ".bat", ".cmd",
		// 标记、样式与模板
		".html", ".htm", ".xhtml", ".xml", ".xsd", ".xsl", ".svg",
		".vue", ".svelte", ".astro", ".jinja", ".j2", ".hbs", ".ejs", ".erb",
		".css", ".scss", ".sass", ".less", ".styl",
		// 配置与构建定义
		".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".properties",
		".env", ".tf", ".hcl", ".nix", ".cmake", ".mk", ".bzl",
		// 文档
		".md", ".mdx", ".markdown", ".rst", ".adoc", ".tex",
	} {
		sourceFileExtensions[ext] = true
	}
	for _, name := range []string{
		"makefile", "gnumakefile", "dockerfile", "containerfile", "jenkinsfile",
		"vagrantfile", "gemfile", "rakefile", "podfile", "cmakelists.txt",
	} {
		sourceFileNames[name] = true
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// errorRe 错误/警告词匹配
// Python: common.py:53 (ERROR_RE)
var errorRe = regexp.MustCompile(`(?i)\b(error|failed|failure|traceback|exception|warn|warning)\b`)

// displayLinePrefixRe 行号显示前缀（不保留体空白）
// Python: common.py:58 (_DISPLAY_LINE_PREFIX_RE)
var displayLinePrefixRe = regexp.MustCompile(`(?im)^(?P<prefix>\s*(?:` +
	`\d+[\t ]+|` +
	`[|>:#-]\s*\d+\s*[|:.)\]-]?\s*|` +
	`(?:line|row)\s+\d+\s*[:|.)\]-]\s*` +
	`))(?P<body>\S.*)$`)

// displayLinePrefixPreserveWSRe 行号显示前缀（保留体空白）
// Python: common.py:66 (_DISPLAY_LINE_PREFIX_PRESERVE_WS_RE)
var displayLinePrefixPreserveWSRe = regexp.MustCompile(`(?im)^(?P<prefix>\s*(?:` +
	`\d+\t|` +
	`\d+ +|` +
	`[|>:#-]\s*\d+\s*[|:.)\]-]?\s*|` +
	`(?:line|row)\s+\d+\s*[:|.)\]-]\s*` +
	`))(?P<body>.*\S.*)$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// CountTokens 计数（自定义计数器优先，len//3 兜底，下限 1）。
// Python: common.py:74 (count_tokens)
func CountTokens(text string, ctx *RuleContext) int {
	if ctx.CountTokens != nil {
		return max(ctx.CountTokens(text), 1)
	}
	return max(len(text)/3, 1)
}

// MeetsSavingsRatio 压缩候选是否达到最小节省比例。
// Python: common.py:79 (meets_savings_ratio)
func MeetsSavingsRatio(original string, candidate string, ctx *RuleContext) bool {
	originalTokens := CountTokens(original, ctx)
	if originalTokens <= 0 {
		return false
	}
	candidateTokens := CountTokens(candidate, ctx)
	return 1-float64(candidateTokens)/float64(originalTokens) >= ctx.MinSavingsRatio
}

// FitsBudgetAndSaves 候选既入预算又达标。
// Python: common.py:85 (fits_budget_and_saves)
func FitsBudgetAndSaves(original string, candidate string, ctx *RuleContext) bool {
	if CountTokens(candidate, ctx) > ctx.MaxTokens {
		return false
	}
	return MeetsSavingsRatio(original, candidate, ctx)
}

// ExtractFilePathArgument 从工具参数提取点名的文件路径。
// Python: common.py:90 (extract_file_path_argument)
func ExtractFilePathArgument(toolArguments any) string {
	arguments := toolArguments
	if text, ok := arguments.(string); ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			return ""
		}
		arguments = parsed
	}
	mapping, ok := arguments.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range filePathArgumentKeys {
		if value, ok := mapping[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// IsSourceFilePath 路径是否点名源码/标记/配置/文档文件（POSIX/Windows 皆可）。
// ntpath 语义：按 "/" 与 "\\" 双分隔符切分。Python: common.py:110 (is_source_file_path)
func IsSourceFilePath(filePath string) bool {
	fileName := strings.ToLower(npathBase(filePath))
	if sourceFileNames[fileName] {
		return true
	}
	extension := npathExt(fileName)
	if extension != "" {
		return sourceFileExtensions[extension]
	}
	// ".env"/".bashrc" 类点文件无 splitext 扩展名
	return sourceFileExtensions[fileName]
}

// StripDisplayLinePrefixes 剥离 read_file/grep 等工具加的行显示前缀。
// Python: common.py:130 (strip_display_line_prefixes)
func StripDisplayLinePrefixes(content string) string {
	return stripDisplayLinePrefixes(content, false)
}

// StripDisplayLinePrefixesPreservingBodyWhitespace 剥前缀但保留体首空白。
// Python: common.py:134 (strip_display_line_prefixes_preserving_body_whitespace)
func StripDisplayLinePrefixesPreservingBodyWhitespace(content string) string {
	return stripDisplayLinePrefixes(content, true)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stripDisplayLinePrefixes 匹配占比 <30% 不动。
// Python: common.py:138 (_strip_display_line_prefixes)
func stripDisplayLinePrefixes(content string, preserveBodyWhitespace bool) string {
	if content == "" {
		return content
	}
	pattern := displayLinePrefixRe
	if preserveBodyWhitespace {
		pattern = displayLinePrefixPreserveWSRe
	}
	matches := pattern.FindAllStringIndex(content, -1)
	nonEmptyLines := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines++
		}
	}
	if len(matches) == 0 || nonEmptyLines == 0 {
		return content
	}
	if float64(len(matches))/float64(nonEmptyLines) < 0.3 {
		return content
	}
	return replaceBodyGroup(pattern, content)
}

// replaceBodyGroup 用命名组 body 替换整行匹配。
// Python: pattern.sub(r"\g<body>", content)
func replaceBodyGroup(pattern *regexp.Regexp, content string) string {
	return pattern.ReplaceAllStringFunc(content, func(matched string) string {
		sub := pattern.FindStringSubmatch(matched)
		for i, name := range pattern.SubexpNames() {
			if name == "body" {
				return sub[i]
			}
		}
		return matched
	})
}

// npathBase ntpath.basename 等价：按 "/" 与 "\\" 双分隔符取基名。
func npathBase(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// npathExt ntpath.splitext 等价：取最后一个 "." 后的扩展名（不含点前无扩展名场景）。
func npathExt(fileName string) string {
	dot := strings.LastIndexByte(fileName, '.')
	// Python splitext：点在首位（".env"）不算扩展名分隔
	if dot <= 0 {
		return ""
	}
	// 排除 "name." 空扩展
	if dot == len(fileName)-1 {
		return ""
	}
	return fileName[dot:]
}
