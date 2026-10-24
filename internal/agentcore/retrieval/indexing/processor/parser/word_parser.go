package parser

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WordParser DOCX 文件解析器。
//
// 仅覆写 ParseContent（对齐 Python WordParser 仅覆写 _parse）；
// Supports 继承 BaseParser 默认 false（Python word_parser.py 无 supports
// 覆写——路由靠 registry init 注册 .docx/.DOCX）。LazyParse/Process 经
// self 动态派发继承。
//
// 软失败语义（对齐 Python 整体 except → return None）：任何内部错误 →
// Error 日志 + 空串返回 → BaseParser.Parse 包装为空切片。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/word_parser.py (WordParser)
type WordParser struct {
	// BaseParser 默认适配器
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 WordParser 实现 Parser
var _ Parser = (*WordParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWordParser 构造 WordParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）。
func NewWordParser() (*WordParser, error) {
	p := &WordParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ParseContent 对齐 Python _parse：解析 DOCX 为 markdown 文本。
//
// 链路：构造 captioner → loadDocxModel → parseBlocks 保序遍历 →
// 逐块 parseWordBlock（段落 markdown/图片 caption + 表格 markdown）→
// "\n".join(strip 过滤行) → 空串对齐 None。
//
// 整体软失败：内部 error → Error 日志 + 空串（对齐 except → return None）。
//
// Python: word_parser.py WordParser._parse
func (p *WordParser) ParseContent(ctx context.Context, filePath string, llmClient *llm.Model) (string, error) {
	captioner := NewImageCaptioner(llmClient)

	result, err := parseWordDocx(ctx, filePath, filepath.Base(filePath), captioner)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse DOCX {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", filePath).
			Err(err).
			Msg("failed to parse DOCX")
		return "", nil
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseWordDocx DOCX 解析主体（非导出，便于单测直接驱动）。
//
// Python: word_parser.py WordParser._parse 的 try 块主体（不含软失败包装）。
func parseWordDocx(ctx context.Context, filePath, filename string, captioner *ImageCaptioner) (string, error) {
	dm, err := loadDocxModel(filePath)
	if err != nil {
		return "", err
	}
	blocks := dm.parseBlocks()

	var content []string
	for paragraphNum, blk := range blocks {
		lines, err := parseWordBlock(ctx, dm, blk, paragraphNum, filename, captioner)
		if err != nil {
			return "", err
		}
		content = append(content, lines...)
	}
	// 对齐 Python: result = "\n".join([line for line in content if line.strip()])
	var kept []string
	for _, line := range content {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n"), nil
}

// parseWordBlock 对齐 Python _parse_block：段落 → markdown + 图片 caption；表格 → markdown。
//
// Python: word_parser.py WordParser._parse_block
func parseWordBlock(ctx context.Context, dm *docxModel, blk docxBlock, paragraphNum int, filename string, captioner *ImageCaptioner) ([]string, error) {
	if blk.kind == "para" {
		var paraText []string
		if md := paragraphToMarkdown(blk.text, blk.styleUI); md != "" {
			paraText = append(paraText, md)
		}
		// 图片提取（坏图炸整篇：error 向上传播）
		images, err := dm.extractImages(blk.paraXML, filename, paragraphNum, savedImageDir)
		if err != nil {
			return nil, err
		}
		if captioner != nil && len(images) > 0 {
			// 对齐 Python: captions = await image_captioner.caption_images(extracted_images)
			captions := captioner.CaptionImages(ctx, images)
			for _, caption := range captions {
				if caption != "" {
					paraText = append(paraText, caption)
				}
			}
		}
		return paraText, nil
	}
	if blk.kind == "table" {
		if md := tableToMarkdown(blk.rows); md != "" {
			return []string{md}, nil
		}
	}
	return nil, nil
}

// paragraphToMarkdown 对齐 Python _paragraph_to_markdown。
//
// Title → "#"、Heading N → N+1 个 "#"（1≤N≤9）；解析失败 Error 日志回落原文
// （对齐 except (IndexError, ValueError) 分支）。
//
// Python: word_parser.py _paragraph_to_markdown
func paragraphToMarkdown(text, styleName string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if styleName == "" {
		return text
	}
	styleName = strings.TrimSpace(styleName)
	if styleName == "Title" {
		return "# " + text
	}
	if strings.HasPrefix(styleName, "Heading ") {
		parts := strings.Fields(styleName)
		if len(parts) >= 2 {
			if level, err := strconv.Atoi(parts[1]); err == nil && level >= 1 && level <= 9 {
				return strings.Repeat("#", level+1) + " " + text
			}
		}
		// 对齐 Python: logger.error("Error while parsing docx paragraph with style %s: %r", ...)
		logger.Error(logComponent).
			Str("style_name", styleName).
			Msg("error while parsing docx paragraph style level")
	}
	return text
}

// tableToMarkdown 对齐 Python _table_to_markdown。
//
// 首行表头 + "---" 分隔 + cell strip + "\|" 转义 + 尾空行。
//
// Python: word_parser.py _table_to_markdown
func tableToMarkdown(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	esc := func(c string) string {
		return strings.ReplaceAll(strings.TrimSpace(c), "|", "\\|")
	}
	var lines []string
	head := make([]string, len(rows[0]))
	for i, c := range rows[0] {
		head[i] = esc(c)
	}
	lines = append(lines, "| "+strings.Join(head, " | ")+" |")
	dashes := make([]string, len(rows[0]))
	for i := range dashes {
		dashes[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(dashes, " | ")+" |")
	for _, row := range rows[1:] {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = esc(c)
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	lines = append(lines, "") // Markdown table need a one-line separation
	return strings.Join(lines, "\n")
}

// init 注册到 registry（对齐 Python @register_parser([".docx", ".DOCX"]) import 时执行）。
func init() {
	RegisterParser([]string{".docx", ".DOCX"}, func() Parser {
		p, _ := NewWordParser()
		return p
	})
}
