package output_parsers

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent markdown output_parser 包日志组件标识（AgentCore 层）。
const mdLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 正则 ────────────────────────────

var (
	// headerRegexp 匹配 Markdown 标题。
	// 对齐 Python: r'^(#{1,6})\s+(.+)$' (MULTILINE)
	headerRegexp = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

	// codeBlockRegexp 匹配 Markdown 代码块。
	// 对齐 Python: r'```(\w*)\n(.*?)\n```' (DOTALL)
	codeBlockRegexp = regexp.MustCompile("(?s)```(\\w*)\n(.*?)\n```")

	// inlineCodeRegexp 匹配行内代码。
	// 对齐 Python: r'`([^`\n]+)`'
	inlineCodeRegexp = regexp.MustCompile("`([^`\n]+)`")

	// imageRegexp 匹配图片。
	// 对齐 Python: r'!\[([^\]]*)\]\(([^)]+)\)'
	imageRegexp = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

	// linkRegexp 匹配链接。
	// 注意：Go regexp 不支持 lookbehind，改用匹配 [text](url) 的简单模式，
	// 然后在提取逻辑中排除以 ! 开头的图片。
	linkRegexp = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// unorderedListRegexp 匹配无序列表项。
	unorderedListRegexp = regexp.MustCompile(`^\s*[-*+]\s+`)

	// orderedListRegexp 匹配有序列表项。
	orderedListRegexp = regexp.MustCompile(`^\s*\d+\.\s+`)
)

// ──────────────────────────── 结构体 ────────────────────────────

// MarkdownOutputParser Markdown 输出解析器，将 LLM 输出解析为结构化 Markdown 元素。
//
// 支持提取的元素类型：headers, code_blocks, inline_code, links, images, tables, lists
//
// 对应 Python: openjiuwen/core/foundation/llm/output_parsers/markdown_output_parser.py (MarkdownOutputParser)
type MarkdownOutputParser struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMarkdownOutputParser 创建 Markdown 输出解析器。
func NewMarkdownOutputParser() *MarkdownOutputParser {
	return &MarkdownOutputParser{}
}

// Parse 解析 LLM 输出中的 Markdown，返回结构化内容。
//
// 输入可以是 string 或 *AssistantMessage。
// 解析成功返回 *MarkdownContent，解析失败返回 nil, nil。
//
// 对应 Python: MarkdownOutputParser.parse()
func (p *MarkdownOutputParser) Parse(input any) (any, error) {
	text := ExtractText(input)
	if text == "" {
		return nil, nil
	}

	content := &MarkdownContent{
		RawContent: text,
		Elements:   make([]*MarkdownElement, 0),
		Headers:    make([]map[string]string, 0),
		CodeBlocks: make([]map[string]string, 0),
		Links:      make([]map[string]string, 0),
		Images:     make([]map[string]string, 0),
		Tables:     make([]string, 0),
		Lists:      make([]string, 0),
	}

	p.extractAllElements(text, content)
	p.populateCategorizedLists(content)

	return content, nil
}

// StreamParse 流式解析 LLM 输出中的 Markdown。
//
// 对齐 Python: MarkdownOutputParser.stream_parse() — 全量重解析模式。
// 每次 buffer 增长就对整个 buffer 重新提取所有元素。
func (p *MarkdownOutputParser) StreamParse(chunks <-chan *llmschema.AssistantMessageChunk) <-chan model_clients.StreamParsedResult {
	out := make(chan model_clients.StreamParsedResult, 8)

	go func() {
		defer close(out)

		buffer := ""
		lastParsedLength := 0

		for chunk := range chunks {
			if chunk == nil {
				continue
			}

			content := chunk.Content.Text()
			if content != "" {
				buffer += content
			}

			// buffer 增长时重新解析
			if len(buffer) > lastParsedLength {
				mdContent := &MarkdownContent{
					RawContent: buffer,
					Elements:   make([]*MarkdownElement, 0),
					Headers:    make([]map[string]string, 0),
					CodeBlocks: make([]map[string]string, 0),
					Links:      make([]map[string]string, 0),
					Images:     make([]map[string]string, 0),
					Tables:     make([]string, 0),
					Lists:      make([]string, 0),
				}

				p.extractAllElements(buffer, mdContent)
				p.populateCategorizedLists(mdContent)

				out <- model_clients.StreamParsedResult{Content: mdContent}
				lastParsedLength = len(buffer)
			}
		}

		// 流结束后最终解析
		if strings.TrimSpace(buffer) != "" {
			mdContent := &MarkdownContent{
				RawContent: buffer,
				Elements:   make([]*MarkdownElement, 0),
				Headers:    make([]map[string]string, 0),
				CodeBlocks: make([]map[string]string, 0),
				Links:      make([]map[string]string, 0),
				Images:     make([]map[string]string, 0),
				Tables:     make([]string, 0),
				Lists:      make([]string, 0),
			}

			p.extractAllElements(buffer, mdContent)
			p.populateCategorizedLists(mdContent)

			out <- model_clients.StreamParsedResult{Content: mdContent}
		}
	}()

	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractAllElements 从文本中提取所有 Markdown 元素。
//
// 对齐 Python: MarkdownOutputParser._extract_all_elements()
func (p *MarkdownOutputParser) extractAllElements(text string, content *MarkdownContent) {
	elements := make([]*MarkdownElement, 0)

	// 提取标题
	for _, match := range headerRegexp.FindAllStringSubmatchIndex(text, -1) {
		level := len(text[match[2]:match[3]])
		title := strings.TrimSpace(text[match[4]:match[5]])
		elements = append(elements, &MarkdownElement{
			Type:    MarkdownHeaderType,
			Content: map[string]any{"level": level, "title": title},
			Raw:     text[match[0]:match[1]],
		})
	}

	// 提取代码块
	for _, match := range codeBlockRegexp.FindAllStringSubmatchIndex(text, -1) {
		language := text[match[2]:match[3]]
		if language == "" {
			language = "text"
		}
		code := text[match[4]:match[5]]
		elements = append(elements, &MarkdownElement{
			Type:    MarkdownCodeBlockType,
			Content: map[string]any{"language": language, "code": code},
			Raw:     text[match[0]:match[1]],
		})
	}

	// 提取行内代码
	for _, match := range inlineCodeRegexp.FindAllStringSubmatchIndex(text, -1) {
		code := text[match[2]:match[3]]
		elements = append(elements, &MarkdownElement{
			Type:    MarkdownInlineCodeType,
			Content: map[string]any{"code": code},
			Raw:     text[match[0]:match[1]],
		})
	}

	// 提取图片
	for _, match := range imageRegexp.FindAllStringSubmatchIndex(text, -1) {
		alt := text[match[2]:match[3]]
		url := text[match[4]:match[5]]
		elements = append(elements, &MarkdownElement{
			Type:    MarkdownImageType,
			Content: map[string]any{"alt": alt, "url": url},
			Raw:     text[match[0]:match[1]],
		})
	}

	// 提取链接（排除图片：![alt](url) 中的链接不算）
	// 对齐 Python: r'(?<!\!)\[([^\]]+)\]\(([^)]+)\)' 使用 lookbehind 排除图片
	// Go regexp 不支持 lookbehind，先匹配所有 [text](url)，再排除前面紧跟 ! 的
	for _, match := range linkRegexp.FindAllStringSubmatchIndex(text, -1) {
		// 检查匹配位置前一个字符是否为 '!'（图片标记）
		matchStart := match[0]
		if matchStart > 0 && text[matchStart-1] == '!' {
			continue // 这是图片，跳过
		}
		linkText := text[match[2]:match[3]]
		url := text[match[4]:match[5]]
		elements = append(elements, &MarkdownElement{
			Type:    MarkdownLinkType,
			Content: map[string]any{"text": linkText, "url": url},
			Raw:     text[match[0]:match[1]],
		})
	}

	// 提取表格和列表（逐行扫描）
	elements = extractMultilineElements(text, elements)

	// 按出现位置排序
	sort.Slice(elements, func(i, j int) bool {
		return elements[i].StartPos < elements[j].StartPos
	})

	content.Elements = elements
}

// populateCategorizedLists 按 type 分发到分类列表。
//
// 对齐 Python: MarkdownOutputParser._populate_categorized_lists()
func (p *MarkdownOutputParser) populateCategorizedLists(content *MarkdownContent) {
	for _, elem := range content.Elements {
		switch elem.Type {
		case MarkdownHeaderType:
			levelStr := ""
			if lv, ok := elem.Content["level"].(int); ok {
				levelStr = fmt.Sprintf("%d", lv)
			} else if lv, ok := elem.Content["level"].(string); ok {
				levelStr = lv
			}
			content.Headers = append(content.Headers, map[string]string{
				"level": levelStr,
				"title": elem.Content["title"].(string),
				"raw":   elem.Raw,
			})
		case MarkdownCodeBlockType:
			content.CodeBlocks = append(content.CodeBlocks, map[string]string{
				"language": elem.Content["language"].(string),
				"code":     elem.Content["code"].(string),
				"raw":      elem.Raw,
			})
		case MarkdownInlineCodeType:
			content.CodeBlocks = append(content.CodeBlocks, map[string]string{
				"language": "inline",
				"code":     elem.Content["code"].(string),
				"raw":      elem.Raw,
			})
		case MarkdownLinkType:
			content.Links = append(content.Links, map[string]string{
				"text": elem.Content["text"].(string),
				"url":  elem.Content["url"].(string),
				"raw":  elem.Raw,
			})
		case MarkdownImageType:
			content.Images = append(content.Images, map[string]string{
				"alt": elem.Content["alt"].(string),
				"url": elem.Content["url"].(string),
				"raw": elem.Raw,
			})
		case MarkdownTableType:
			content.Tables = append(content.Tables, elem.Content["table"].(string))
		case MarkdownListType:
			content.Lists = append(content.Lists, elem.Content["list"].(string))
		}
	}
}

// extractMultilineElements 逐行扫描提取表格和列表元素。
//
// 对齐 Python: MarkdownOutputParser._extract_multiline_elements()
// 返回追加后的 elements 切片。
func extractMultilineElements(text string, elements []*MarkdownElement) []*MarkdownElement {
	lines := strings.Split(text, "\n")
	currentPos := 0

	var tableLines []string
	var listLines []string
	tableStartPos := -1
	listStartPos := -1

	for _, line := range lines {
		lineStartPos := currentPos
		currentPos += len(line) + 1 // +1 for \n

		// 表格识别
		if strings.Contains(line, "|") && strings.TrimSpace(line) != "" {
			if len(tableLines) == 0 {
				tableStartPos = lineStartPos
			}
			tableLines = append(tableLines, line)
		} else {
			if len(tableLines) > 0 {
				tableContent := strings.Join(tableLines, "\n")
				elements = append(elements, &MarkdownElement{
					Type:      MarkdownTableType,
					Content:   map[string]any{"table": tableContent},
					StartPos:  tableStartPos,
					EndPos:    lineStartPos - 1,
					Raw:       tableContent,
				})
				tableLines = nil
			}
		}

		// 列表识别
		if unorderedListRegexp.MatchString(line) || orderedListRegexp.MatchString(line) {
			if len(listLines) == 0 {
				listStartPos = lineStartPos
			}
			listLines = append(listLines, line)
		} else if strings.TrimSpace(line) == "" && len(listLines) > 0 {
			// 空行在列表上下文中保留
			listLines = append(listLines, line)
		} else {
			if len(listLines) > 0 {
				listContent := strings.TrimSpace(strings.Join(listLines, "\n"))
				if listContent != "" {
					elements = append(elements, &MarkdownElement{
						Type:      MarkdownListType,
						Content:   map[string]any{"list": listContent},
						StartPos:  listStartPos,
						EndPos:    lineStartPos - 1,
						Raw:       listContent,
					})
				}
				listLines = nil
			}
		}
	}

	// 处理末尾未结束的表格
	if len(tableLines) > 0 {
		tableContent := strings.Join(tableLines, "\n")
		elements = append(elements, &MarkdownElement{
			Type:      MarkdownTableType,
			Content:   map[string]any{"table": tableContent},
			StartPos:  tableStartPos,
			EndPos:    len(text),
			Raw:       tableContent,
		})
	}

	// 处理末尾未结束的列表
	if len(listLines) > 0 {
		listContent := strings.TrimSpace(strings.Join(listLines, "\n"))
		if listContent != "" {
			elements = append(elements, &MarkdownElement{
				Type:      MarkdownListType,
				Content:   map[string]any{"list": listContent},
				StartPos:  listStartPos,
				EndPos:    len(text),
				Raw:       listContent,
			})
		}
	}

	return elements
}
