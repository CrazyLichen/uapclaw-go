package filesystem

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GrepInput grep 工具的输入参数。
// 对齐 Python: GrepTool inputs (filesystem.py L1534)
type GrepInput struct {
	// Pattern 正则表达式（必需）
	Pattern string `json:"pattern"`
	// Path 搜索路径，默认 cwd
	Path string `json:"path"`
	// IgnoreCase 兼容旧字段：大小写不敏感
	IgnoreCase bool `json:"ignore_case"`
	// Glob glob 过滤模式
	Glob string `json:"glob"`
	// OutputMode 输出模式：content/files_with_matches/count，默认 content
	OutputMode string `json:"output_mode"`
	// B 匹配前上下文行数
	B int `json:"-B"`
	// A 匹配后上下文行数
	A int `json:"-A"`
	// C 匹配前后上下文行数
	C int `json:"-C"`
	// Context -C 的别名
	Context int `json:"context"`
	// N 显示行号，默认 true
	N *bool `json:"-n"`
	// I 大小写不敏感
	I bool `json:"-i"`
	// Type 文件类型过滤
	Type string `json:"type"`
	// HeadLimit 前N条结果，默认 250
	HeadLimit int `json:"head_limit"`
	// Offset 跳过前N条结果
	Offset int `json:"offset"`
	// Multiline 多行模式
	Multiline bool `json:"multiline"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// grepDefaultHeadLimit 默认 head_limit。
	// 对齐 Python: GrepTool.DEFAULT_HEAD_LIMIT (filesystem.py L1535)
	grepDefaultHeadLimit = 250

	// grepMaxColumns rg 最大列数。
	// 对齐 Python: GrepTool.MAX_COLUMNS (filesystem.py L1536)
	grepMaxColumns = 500
)

// ──────────────────────────── 全局变量 ────────────────────────────

// vcsDirectoriesToExclude 版本控制目录排除列表。
// 对齐 Python: GrepTool.VCS_DIRECTORIES_TO_EXCLUDE (filesystem.py L1537)
var vcsDirectoriesToExclude = []string{".git", ".svn", ".hg", ".bzr", ".jj", ".sl"}

// grepLineRe 匹配 grep/rg content 模式输出行：filepath:linenum:content
var grepLineRe = regexp.MustCompile(`^(.*?):(\d+|[-]+):(.*)$`)

// braceGlobRe 匹配花括号组内容（用于 Select-String 展开）
var braceGlobRe = regexp.MustCompile(`^(.*)\{([^}]+)\}(.*)$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGrepTool 创建 GrepTool 实例。
// 对齐 Python: GrepTool (filesystem.py L1534)
func NewGrepTool(op sys_operation.SysOperation, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("grep", "GrepTool", language, nil, agentID)

	fn := func(ctx context.Context, input GrepInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 校验 pattern 必需
		// 对齐 Python L1897-1899
		if input.Pattern == "" {
			return map[string]any{
				"success": false,
				"error":   "pattern is required",
			}, nil
		}

		// 解析搜索路径
		// 对齐 Python L1901-1904
		searchPath, err := resolveSearchPath(ctx, input.Path)
		if err != nil {
			return map[string]any{
				"success": false,
				"error":   err.Error(),
			}, nil
		}

		// 解析 output_mode
		// 对齐 Python L1906-1908
		outputMode := input.OutputMode
		if outputMode == "" {
			outputMode = "content"
		}
		validModes := map[string]bool{"content": true, "files_with_matches": true, "count": true}
		if !validModes[outputMode] {
			return map[string]any{
				"success": false,
				"error":   "output_mode must be one of: content, files_with_matches, count",
			}, nil
		}

		// 合并 -i 和 ignore_case
		// 对齐 Python L1910
		caseInsensitive := input.I || input.IgnoreCase

		// 解析 -n，默认 true
		// 对齐 Python L1911
		showLineNumbers := true
		if input.N != nil {
			showLineNumbers = *input.N
		}

		// 解析上下文参数
		// 对齐 Python L1912-1915
		contextBefore := intPtrOrNil(input.B)
		contextAfter := intPtrOrNil(input.A)
		contextC := intPtrOrNil(input.C)
		context := intPtrOrNil(input.Context)

		headLimit := intPtrOrNil(input.HeadLimit)
		offset := input.Offset

		multiline := input.Multiline
		globVal := input.Glob
		fileType := input.Type

		// 非 content 模式时清零上下文参数
		// 对齐 Python L1922-1929
		if outputMode != "content" {
			if contextBefore != nil || contextAfter != nil || contextC != nil || context != nil {
				contextBefore = nil
				contextAfter = nil
				contextC = nil
				context = nil
			}
		}

		// 检测 rg 可用性，构建命令
		// 对齐 Python L1931-1979
		var cmd string
		isWindows := runtime.GOOS == "windows"

		if rgAvailable() {
			cmd = buildRgCommand(
				input.Pattern, searchPath, globVal, outputMode,
				contextBefore, contextAfter, contextC, context,
				showLineNumbers, caseInsensitive, fileType, multiline,
			)
		} else if isWindows {
			if fileType != "" {
				return map[string]any{
					"success": false,
					"error":   "type filter requires ripgrep (rg) to be installed",
				}, nil
			}
			if multiline {
				return map[string]any{
					"success": false,
					"error":   "multiline search requires ripgrep (rg) to be installed",
				}, nil
			}
			cmd = buildSelectStringCommand(
				input.Pattern, searchPath, globVal, outputMode,
				contextBefore, contextAfter, contextC, context,
				caseInsensitive,
			)
		} else {
			if fileType != "" {
				return map[string]any{
					"success": false,
					"error":   "type filter requires ripgrep (rg) to be installed",
				}, nil
			}
			cmd = buildGrepCommand(
				input.Pattern, searchPath, globVal, outputMode,
				contextBefore, contextAfter, contextC, context,
				showLineNumbers, caseInsensitive, multiline,
			)
			if cmd == "" {
				return map[string]any{
					"success": false,
					"error":   "multiline search requires ripgrep (rg) to be installed",
				}, nil
			}
		}

		// 执行命令
		// 对齐 Python L1981-1982
		shellType := sys_operation.ShellTypeAuto
		if isWindows {
			shellType = sys_operation.ShellTypePowerShell
		}

		res, execErr := op.Shell().ExecuteCmd(ctx, cmd,
			sys_operation.WithShellTimeout(30),
			sys_operation.WithShellType(shellType),
		)
		if execErr != nil {
			logger.Error(logComponent).
				Str("command", cmd).
				Err(execErr).
				Msg("GrepTool ExecuteCmd 调用失败")
			return map[string]any{
				"success": false,
				"error":   execErr.Error(),
			}, nil
		}
		if !res.IsSuccess() {
			logger.Error(logComponent).
				Str("command", cmd).
				Str("message", res.Message).
				Msg("GrepTool ExecuteCmd 返回失败")
			return map[string]any{
				"success": false,
				"error":   res.Message,
			}, nil
		}

		// 解析执行结果
		// 对齐 Python L1987-1990
		stdout := ""
		stderr := ""
		exitCode := -1
		if res.Data != nil {
			stdout = res.Data.Stdout
			stderr = res.Data.Stderr
			if res.Data.ExitCode != nil {
				exitCode = *res.Data.ExitCode
			}
		}

		// exit_code ∈ {0, 1} 视为成功
		// 对齐 Python L1990
		success := exitCode == 0 || exitCode == 1

		// 计算 base_path 用于相对路径转换
		// 对齐 Python L2001
		basePath := searchPath
		if fi, statErr := os.Stat(searchPath); statErr == nil && !fi.IsDir() {
			dir := filepath.Dir(searchPath)
			if dir != "" {
				basePath = dir
			} else {
				basePath = "."
			}
		}

		// 构建结构化输出
		// 对齐 Python L1992-2003
		data := buildStructuredOutput(
			stdout, stderr, exitCode, outputMode,
			headLimit, offset, basePath,
		)

		errStr := ""
		if !success {
			errStr = stderr
		}

		return map[string]any{
			"success": success,
			"data":    data,
			"error":   errStr,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rgAvailable 检测 rg 命令是否可用。
// 对齐 Python: shutil.which("rg") (filesystem.py L1931)
func rgAvailable() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

// shellQuote 对值进行 Shell 引号包裹。
// 对齐 Python: GrepTool._shell_quote (filesystem.py L1544-1549)
func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	// POSIX: 单引号包裹，内部 ' 替换为 '\''
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// splitGlobPatterns 拆分 glob 模式字符串。
// 对齐 Python: GrepTool._split_glob_patterns (filesystem.py L1588-1599)
func splitGlobPatterns(globValue string) []string {
	if globValue == "" {
		return nil
	}
	var patterns []string
	for _, rawPattern := range strings.Fields(globValue) {
		if strings.ContainsRune(rawPattern, '{') && strings.ContainsRune(rawPattern, '}') {
			patterns = append(patterns, rawPattern)
		} else {
			for _, part := range strings.Split(rawPattern, ",") {
				if part != "" {
					patterns = append(patterns, part)
				}
			}
		}
	}
	return patterns
}

// intPtrOrNil 将零值 int 转为 nil，非零值转为 *int。
// 对齐 Python: GrepTool._as_int — None/空 对应 nil
func intPtrOrNil(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// buildRgCommand 构建 ripgrep 命令。
// 对齐 Python: GrepTool._build_rg_command (filesystem.py L1606-1668)
func buildRgCommand(
	pattern, path, globVal, outputMode string,
	contextBefore, contextAfter, contextC, context *int,
	showLineNumbers, caseInsensitive bool,
	fileType string, multiline bool,
) string {
	parts := []string{
		"rg",
		"--hidden",
		"--color=never",
		"--max-columns",
		fmt.Sprintf("%d", grepMaxColumns),
	}

	if multiline {
		parts = append(parts, "-U", "--multiline-dotall")
	}
	if caseInsensitive {
		parts = append(parts, "-i")
	}

	switch outputMode {
	case "files_with_matches":
		parts = append(parts, "-l")
	case "count":
		parts = append(parts, "-c")
	default:
		if showLineNumbers {
			parts = append(parts, "-n")
		}
	}

	if outputMode == "content" {
		if context != nil {
			parts = append(parts, "-C", fmt.Sprintf("%d", *context))
		} else if contextC != nil {
			parts = append(parts, "-C", fmt.Sprintf("%d", *contextC))
		} else {
			if contextBefore != nil {
				parts = append(parts, "-B", fmt.Sprintf("%d", *contextBefore))
			}
			if contextAfter != nil {
				parts = append(parts, "-A", fmt.Sprintf("%d", *contextAfter))
			}
		}
	}

	for _, dir := range vcsDirectoriesToExclude {
		parts = append(parts, "--glob", shellQuote("!"+dir))
	}

	if fileType != "" {
		parts = append(parts, "--type", shellQuote(fileType))
	}

	for _, globPattern := range splitGlobPatterns(globVal) {
		parts = append(parts, "--glob", shellQuote(globPattern))
	}

	if strings.HasPrefix(pattern, "-") {
		parts = append(parts, "-e", shellQuote(pattern))
	} else {
		parts = append(parts, shellQuote(pattern))
	}

	parts = append(parts, shellQuote(path))
	return strings.Join(parts, " ")
}

// buildGrepCommand 构建 grep 命令。
// 对齐 Python: GrepTool._build_grep_command (filesystem.py L1748-1796)
// 返回空字符串表示不支持（如 multiline 模式）。
func buildGrepCommand(
	pattern, path, globVal, outputMode string,
	contextBefore, contextAfter, contextC, context *int,
	showLineNumbers, caseInsensitive bool,
	multiline bool,
) string {
	if multiline {
		return ""
	}

	parts := []string{"grep", "-R", "--binary-files=without-match"}

	for _, dir := range vcsDirectoriesToExclude {
		parts = append(parts, fmt.Sprintf("--exclude-dir=%s", shellQuote(dir)))
	}

	if caseInsensitive {
		parts = append(parts, "-i")
	}

	switch outputMode {
	case "files_with_matches":
		parts = append(parts, "-l")
	case "count":
		parts = append(parts, "-c")
	default:
		if showLineNumbers {
			parts = append(parts, "-n")
		}
	}

	if outputMode == "content" {
		if context != nil {
			parts = append(parts, "-C", fmt.Sprintf("%d", *context))
		} else if contextC != nil {
			parts = append(parts, "-C", fmt.Sprintf("%d", *contextC))
		} else {
			if contextBefore != nil {
				parts = append(parts, "-B", fmt.Sprintf("%d", *contextBefore))
			}
			if contextAfter != nil {
				parts = append(parts, "-A", fmt.Sprintf("%d", *contextAfter))
			}
		}
	}

	for _, globPattern := range splitGlobPatterns(globVal) {
		parts = append(parts, fmt.Sprintf("--include=%s", shellQuote(globPattern)))
	}

	parts = append(parts, shellQuote(pattern), shellQuote(path))
	return strings.Join(parts, " ")
}

// buildSelectStringCommand 构建 PowerShell Select-String 命令。
// 对齐 Python: GrepTool._build_select_string_command (filesystem.py L1670-1746)
// 仅在 Windows 且无 rg 时使用。
func buildSelectStringCommand(
	pattern, path, globVal, outputMode string,
	contextBefore, contextAfter, contextC, context *int,
	caseInsensitive bool,
) string {
	sq := shellQuote

	// 展开花括号模式
	globPatterns := splitGlobPatterns(globVal)
	var expandedGlobs []string
	for _, p := range globPatterns {
		m := braceGlobRe.FindStringSubmatch(p)
		if m != nil {
			for _, alt := range strings.Split(m[2], ",") {
				expandedGlobs = append(expandedGlobs, m[1]+alt+m[3])
			}
		} else {
			expandedGlobs = append(expandedGlobs, p)
		}
	}

	// 上下文行：context/contextC 优先于 contextBefore/contextAfter
	effectiveC := context
	if effectiveC == nil {
		effectiveC = contextC
	}
	ctxB := 0
	ctxA := 0
	if effectiveC != nil {
		ctxB = *effectiveC
		ctxA = *effectiveC
	} else {
		if contextBefore != nil {
			ctxB = *contextBefore
		}
		if contextAfter != nil {
			ctxA = *contextAfter
		}
	}

	// VCS 排除正则
	vcsAlts := make([]string, len(vcsDirectoriesToExclude))
	for i, d := range vcsDirectoriesToExclude {
		vcsAlts[i] = strings.ReplaceAll(d, ".", `\\.`)
	}
	vcsPat := sq(`(\\|/)(` + strings.Join(vcsAlts, "|") + `)(\\|/|$)`)

	// 构建管道
	var pipeline []string

	// Stage 1: 文件枚举 + VCS 排除
	isFile := false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		isFile = true
	}
	if isFile {
		pipeline = append(pipeline, fmt.Sprintf("Get-Item -LiteralPath %s", sq(path)))
	} else {
		pipeline = append(pipeline, fmt.Sprintf("Get-ChildItem -LiteralPath %s -Recurse -File", sq(path)))
		pipeline = append(pipeline, fmt.Sprintf("Where-Object { $_.FullName -notmatch %s }", vcsPat))
	}

	// Stage 2: glob 过滤
	if len(expandedGlobs) > 0 && !isFile {
		conds := make([]string, len(expandedGlobs))
		for i, p := range expandedGlobs {
			conds[i] = fmt.Sprintf("$_.Name -like %s", sq(p))
		}
		pipeline = append(pipeline, fmt.Sprintf("Where-Object { %s }", strings.Join(conds, " -or ")))
	}

	// Stage 3: Select-String
	csFlag := " -CaseSensitive"
	if caseInsensitive {
		csFlag = ""
	}
	ctxFlag := ""
	if outputMode == "content" && (ctxB > 0 || ctxA > 0) {
		ctxFlag = fmt.Sprintf(" -Context %d,%d", ctxB, ctxA)
	}
	pipeline = append(pipeline, fmt.Sprintf("Select-String -Pattern %s%s%s", sq(pattern), csFlag, ctxFlag))

	// Stage 4: 输出格式化
	switch outputMode {
	case "files_with_matches":
		pipeline = append(pipeline, "Select-Object -ExpandProperty Path -Unique")
	case "count":
		pipeline = append(pipeline, `Group-Object Path | ForEach-Object { "$($_.Name):$($_.Count)" }`)
	default:
		if ctxB > 0 || ctxA > 0 {
			pipeline = append(pipeline,
				`ForEach-Object { $m=$_; $p=$m.Context.PreContext.Length; for($i=0;$i-lt$p;$i++){ "$($m.Path):$([int]$m.LineNumber-$p+$i):$($m.Context.PreContext[$i])" }; "$($m.Path):$($m.LineNumber):$($m.Line)"; for($i=0;$i-lt$m.Context.PostContext.Length;$i++){ "$($m.Path):$([int]$m.LineNumber+1+$i):$($m.Context.PostContext[$i])" } }`,
			)
		} else {
			pipeline = append(pipeline, `ForEach-Object { "$($_.Path):$($_.LineNumber):$($_.Line)" }`)
		}
	}

	return "$ErrorActionPreference='SilentlyContinue'; " + strings.Join(pipeline, " | ")
}

// applyHeadLimit 应用 head_limit 和 offset 分页。
// 对齐 Python: GrepTool._apply_head_limit (filesystem.py L1575-1586)
func applyHeadLimit(items []string, limit *int, offset int) ([]string, *int) {
	if offset < 0 {
		offset = 0
	}
	if limit != nil && *limit == 0 {
		return items[offset:], nil
	}

	effectiveLimit := grepDefaultHeadLimit
	if limit != nil {
		effectiveLimit = *limit
	}
	end := offset + effectiveLimit
	if end > len(items) {
		end = len(items)
	}
	sliced := items[offset:end]
	wasTruncated := len(items)-offset > effectiveLimit

	if wasTruncated {
		return sliced, &effectiveLimit
	}
	return sliced, nil
}

// extractFilePathFromLine 从输出行提取文件路径。
// 对齐 Python: GrepTool._extract_file_path_from_line (filesystem.py L1798-1812)
func extractFilePathFromLine(line, mode string) string {
	if line == "" {
		return ""
	}
	if mode == "files_with_matches" {
		return line
	}
	if mode == "count" {
		idx := strings.LastIndex(line, ":")
		if idx == -1 {
			return ""
		}
		return line[:idx]
	}

	// content 模式: filepath:linenum:content
	m := grepLineRe.FindStringSubmatch(line)
	if m != nil {
		return m[1]
	}
	if idx := strings.Index(line, ":"); idx != -1 {
		return line[:idx]
	}
	return ""
}

// relativizeLine 将输出行中的绝对路径转为相对路径。
// 对齐 Python: GrepTool._relativize_line (filesystem.py L1814-1831)
func relativizeLine(line, basePath, mode string) string {
	filePath := extractFilePathFromLine(line, mode)
	if filePath == "" {
		return line
	}

	relPath, err := filepath.Rel(basePath, filePath)
	if err != nil {
		return line
	}

	if mode == "files_with_matches" {
		return relPath
	}

	prefix := filePath + ":"
	if strings.HasPrefix(line, prefix) {
		return relPath + ":" + line[len(prefix):]
	}
	return line
}

// buildStructuredOutput 构建结构化输出。
// 对齐 Python: GrepTool._build_structured_output (filesystem.py L1833-1894)
func buildStructuredOutput(
	stdout, stderr string,
	exitCode int,
	outputMode string,
	headLimit *int,
	offset int,
	basePath string,
) map[string]any {
	// 按行拆分并过滤空行
	rawLines := make([]string, 0)
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			rawLines = append(rawLines, line)
		}
	}

	limitedLines, appliedLimit := applyHeadLimit(rawLines, headLimit, offset)

	// 将绝对路径转为相对路径
	finalLines := make([]string, len(limitedLines))
	for i, line := range limitedLines {
		finalLines[i] = relativizeLine(line, basePath, outputMode)
	}

	content := strings.Join(finalLines, "\n")

	data := map[string]any{
		"stdout":  content,
		"stderr":  stderr,
		"exit_code": exitCode,
		"mode":    outputMode,
		"appliedOffset": nil,
		"appliedLimit":  nil,
	}
	if offset > 0 {
		data["appliedOffset"] = offset
	}
	if appliedLimit != nil {
		data["appliedLimit"] = *appliedLimit
	}

	switch outputMode {
	case "content":
		data["content"] = content
		data["filenames"] = []string{}
		data["numFiles"] = 0
		data["numLines"] = len(finalLines)
		data["count"] = len(finalLines)

	case "count":
		totalMatches := 0
		fileCount := 0
		for _, line := range finalLines {
			idx := strings.LastIndex(line, ":")
			if idx == -1 {
				continue
			}
			countStr := line[idx+1:]
			var count int
			if _, err := fmt.Sscanf(countStr, "%d", &count); err == nil {
				totalMatches += count
				fileCount++
			}
		}
		data["content"] = content
		data["filenames"] = []string{}
		data["numFiles"] = fileCount
		data["numMatches"] = totalMatches
		data["count"] = totalMatches

	case "files_with_matches":
		data["filenames"] = finalLines
		data["numFiles"] = len(finalLines)
		data["count"] = len(finalLines)
	}

	return data
}
