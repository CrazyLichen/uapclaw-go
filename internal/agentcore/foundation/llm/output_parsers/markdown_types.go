package output_parsers

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// MarkdownElement 单个 Markdown 元素。
//
// 对应 Python: MarkdownElement (dataclass)
type MarkdownElement struct {
	// Type 元素类型
	Type MarkdownElementType `json:"type"`
	// Content 元素内容（不同类型有不同的键集合）
	Content map[string]any `json:"content"`
	// StartPos 在原始文本中的起始位置
	StartPos int `json:"start_pos"`
	// EndPos 在原始文本中的结束位置
	EndPos int `json:"end_pos"`
	// Raw 原始文本
	Raw string `json:"raw"`
}

// MarkdownContent 结构化的 Markdown 内容表示。
//
// 对应 Python: MarkdownContent (dataclass)
type MarkdownContent struct {
	// RawContent 原始文本内容
	RawContent string `json:"raw_content"`
	// Elements 所有元素，按原始位置排序
	Elements []*MarkdownElement `json:"elements"`
	// Headers 标题列表
	Headers []map[string]string `json:"headers"`
	// CodeBlocks 代码块列表（含行内代码）
	CodeBlocks []map[string]string `json:"code_blocks"`
	// Links 链接列表
	Links []map[string]string `json:"links"`
	// Images 图片列表
	Images []map[string]string `json:"images"`
	// Tables 表格列表
	Tables []string `json:"tables"`
	// Lists 列表列表
	Lists []string `json:"lists"`
}

// MarkdownElementType Markdown 元素类型常量。
//
// 对应 Python: MarkdownElementType
type MarkdownElementType string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// MarkdownHeaderType 标题元素
	MarkdownHeaderType MarkdownElementType = "header"
	// MarkdownCodeBlockType 代码块元素
	MarkdownCodeBlockType MarkdownElementType = "code_block"
	// MarkdownInlineCodeType 行内代码元素
	MarkdownInlineCodeType MarkdownElementType = "inline_code"
	// MarkdownLinkType 链接元素
	MarkdownLinkType MarkdownElementType = "link"
	// MarkdownImageType 图片元素
	MarkdownImageType MarkdownElementType = "image"
	// MarkdownTableType 表格元素
	MarkdownTableType MarkdownElementType = "table"
	// MarkdownListType 列表元素
	MarkdownListType MarkdownElementType = "list"
	// MarkdownTextType 纯文本元素
	MarkdownTextType MarkdownElementType = "text"
)
