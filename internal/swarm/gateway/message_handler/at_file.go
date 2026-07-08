package message_handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultInlineFileSizeLimit 默认内联文件大小限制（128KB）
const defaultInlineFileSizeLimit = 128 * 1024

// ──────────────────────────── 全局变量 ────────────────────────────

// atFilePattern 匹配 @path 或 @"quoted path" 的正则
// 对齐 Python re.compile(r'(?P<prefix>(?:^|(?<=\s)))@(?:"(?P<quoted>[^"]+)"|(?P<plain>[^\s#]+))(?:#[^#\s]*)?')
// 使用 regexp2 支持 lookbehind (?<=\s)，命名组用 .NET 语法 (?<name>...) 而非 Python (?P<name>...)
var atFilePattern = regexp2.MustCompile(
	`(?<prefix>(?:^|(?<=\s)))@(?:"(?<quoted>[^"]+)"|(?<plain>[^\s#]+))(?:#[^#\s]*)?`,
	0,
)

// agentMentionQuotedPattern 匹配 @"<type> (agent)" 格式
var agentMentionQuotedPattern = regexp2.MustCompile(`(?<prefix>(?:^|(?<=\s)))@"(?<name>[\w:.@-]+)\s+\(agent\)"`, 0)

// agentMentionPlainPattern 匹配 @agent-<type> 格式
var agentMentionPlainPattern = regexp2.MustCompile(`(?<prefix>(?:^|(?<=\s)))@(?<name>agent-[\w:.@-]+)`, 0)

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

	result, _ := atFilePattern.ReplaceFunc(content, func(m regexp2.Match) string {
		// 提取路径：尝试 quoted 组，否则 plain 组
		raw := ""
		if g := m.GroupByName("quoted"); g != nil && g.String() != "" {
			raw = g.String()
		} else if g := m.GroupByName("plain"); g != nil && g.String() != "" {
			raw = g.String()
		}
		if raw == "" {
			return m.String()
		}

		// 跳过 @agent-xxx / @agent:xxx 提及（不是文件引用）
		if strings.HasPrefix(raw, "agent-") || strings.HasPrefix(raw, "agent:") {
			return m.String()
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
			return m.String()
		}

		fileSize := info.Size()
		var data []byte
		truncated := false

		if maxFileSize <= 0 {
			data, err = os.ReadFile(resolved)
		} else {
			f, ferr := os.Open(resolved)
			if ferr != nil {
				return m.String()
			}
			defer func() {
				if closeErr := f.Close(); closeErr != nil {
					logger.Warn(logComponent).Err(closeErr).Str("file", resolved).Msg("关闭文件失败")
				}
			}()

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
			return m.String()
		}

		text := string(data)
		if truncated {
			text = fmt.Sprintf("%s\n... (truncated, original_size=%d bytes)", text, fileSize)
		}

		return fmt.Sprintf("\n<file-content path=\"%s\">\n%s\n</file-content>\n", raw, text)
	}, 0, -1)

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
	mQuoted, _ := agentMentionQuotedPattern.FindStringMatch(content)
	for mQuoted != nil {
		if g := mQuoted.GroupByName("name"); g != nil && g.String() != "" {
			results = append(results, g.String())
		}
		mQuoted, _ = agentMentionQuotedPattern.FindNextMatch(mQuoted)
	}

	// 匹配非引号格式：@agent-<type>
	mPlain, _ := agentMentionPlainPattern.FindStringMatch(content)
	for mPlain != nil {
		if g := mPlain.GroupByName("name"); g != nil && g.String() != "" {
			name := g.String()
			results = append(results, name[len("agent-"):])
		}
		mPlain, _ = agentMentionPlainPattern.FindNextMatch(mPlain)
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
