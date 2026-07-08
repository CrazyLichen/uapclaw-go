package message_handler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultInlineFileSizeLimit 默认内联文件大小限制（128KB）
const defaultInlineFileSizeLimit = 128 * 1024

// ──────────────────────────── 全局变量 ────────────────────────────

// atFilePattern 匹配 @path 或 @"quoted path" 的正则
// Go regexp 不支持 lookbehind，改用前导空白/行首捕获组
var atFilePattern = regexp.MustCompile(
	`(?:^|\s)@(?:"([^"]+)"|([^\s#]+))(?:#[^#\s]*)?`,
)

// agentMentionQuotedPattern 匹配 @"<type> (agent)" 格式
var agentMentionQuotedPattern = regexp.MustCompile(`(?:^|\s)@"([\w:.@-]+)\s+\(agent\)"`)

// agentMentionPlainPattern 匹配 @agent-<type> 格式
var agentMentionPlainPattern = regexp.MustCompile(`(?:^|\s)@(agent-[\w:.@-]+)`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveAtFileReferences 解析 content 中的 @path 引用并内联文件文本。
//
// 支持形式：
//   - @relative/path / @/absolute/path — 基于 cwd 解析
//   - @"path with spaces" — 引号路径
//   - @path#L10-20 — 行范围后缀（当前忽略，读取整个文件）
//
// 对齐 Python resolve_at_file_references
func ResolveAtFileReferences(content string, cwd string, maxFileSize int) string {
	if content == "" {
		return content
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if maxFileSize <= 0 {
		maxFileSize = defaultInlineFileSizeLimit
	}

	result := atFilePattern.ReplaceAllStringFunc(content, func(match string) string {
		// 提取路径：尝试 quoted 组，否则 plain 组
		submatches := atFilePattern.FindStringSubmatch(match)
		raw := ""
		if len(submatches) >= 2 && submatches[1] != "" {
			raw = submatches[1] // quoted
		} else if len(submatches) >= 3 && submatches[2] != "" {
			raw = submatches[2] // plain
		}
		if raw == "" {
			return match
		}

		// 跳过 @agent-xxx / @agent:xxx 提及（不是文件引用）
		if strings.HasPrefix(raw, "agent-") || strings.HasPrefix(raw, "agent:") {
			return match
		}

		// 解析路径
		var resolved string
		if strings.HasPrefix(raw, "~/") {
			home, _ := os.UserHomeDir()
			resolved = filepath.Join(home, raw[2:])
		} else if filepath.IsAbs(raw) {
			resolved = raw
		} else {
			resolved = filepath.Join(cwd, raw)
		}

		// 读取文件
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return match
		}

		fileSize := info.Size()
		var data []byte
		truncated := false

		if maxFileSize <= 0 {
			data, err = os.ReadFile(resolved)
		} else {
			f, ferr := os.Open(resolved)
			if ferr != nil {
				return match
			}
			defer f.Close()

			buf := make([]byte, maxFileSize+1)
			n, _ := f.Read(buf)
			data = buf[:n]

			if fileSize > int64(maxFileSize) || len(data) > maxFileSize {
				truncated = true
			}
			if len(data) > maxFileSize {
				data = data[:maxFileSize]
			}
		}

		if err != nil {
			return match
		}

		text := string(data)
		if truncated {
			text = fmt.Sprintf("%s\n... (truncated, original_size=%d bytes)", text, fileSize)
		}

		return fmt.Sprintf("\n<file-content path=\"%s\">\n%s\n</file-content>\n", raw, text)
	})

	return result
}

// ExtractAgentMentions 解析 content 中的 @agent-xxx 和 @"xxx (agent)" 提及。
//
// 返回智能体类型名称列表（不含 "agent-" 前缀），去重保序。
//
// 对齐 Python extract_agent_mentions
func ExtractAgentMentions(content string) []string {
	if content == "" {
		return nil
	}

	var results []string

	// 匹配引号格式：@"<type> (agent)"
	for _, m := range agentMentionQuotedPattern.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 && m[1] != "" {
			results = append(results, m[1])
		}
	}

	// 匹配非引号格式：@agent-<type>
	for _, m := range agentMentionPlainPattern.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 && m[1] != "" {
			name := m[1]
			results = append(results, name[len("agent-"):])
		}
	}

	// 去重保序
	seen := make(map[string]bool)
	unique := make([]string, 0, len(results))
	for _, name := range results {
		if !seen[name] {
			seen[name] = true
			unique = append(unique, name)
		}
	}
	return unique
}

// NormalizeStructuredAttachments 规范化结构化附件列表。
//
// 对齐 Python _normalize_structured_attachments：
// 去重（按 resolved path），填充默认 type/filename。
func NormalizeStructuredAttachments(attachments []map[string]any, cwd string) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	var normalized []map[string]any
	seen := make(map[string]bool)

	for _, item := range attachments {
		rawPath := ""
		if p, ok := item["path"]; ok {
			rawPath = strings.TrimSpace(fmt.Sprintf("%v", p))
		}
		if rawPath == "" {
			continue
		}

		resolvedPath := resolveReferencePath(rawPath, cwd)
		if seen[resolvedPath] {
			continue
		}
		seen[resolvedPath] = true

		typ := "file"
		if t, ok := item["type"]; ok {
			s := strings.TrimSpace(fmt.Sprintf("%v", t))
			if s != "" {
				typ = s
			}
		}

		filename := filepath.Base(resolvedPath)
		if fn, ok := item["filename"]; ok {
			s := strings.TrimSpace(fmt.Sprintf("%v", fn))
			if s != "" {
				filename = s
			}
		}

		normalized = append(normalized, map[string]any{
			"path":     resolvedPath,
			"type":     typ,
			"filename": filename,
		})
	}
	return normalized
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveReferencePath 解析引用路径（相对/绝对/~/）
func resolveReferencePath(rawPath, cwd string) string {
	if strings.HasPrefix(rawPath, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rawPath[2:])
	}
	if filepath.IsAbs(rawPath) {
		return rawPath
	}
	return filepath.Join(cwd, rawPath)
}
