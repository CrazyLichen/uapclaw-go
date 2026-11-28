package common

// ──────────────────────────── 枚举 ────────────────────────────

// SourceLocator 类型常量（对齐 WeKnora types.SourceLocator 语义子集）
const (
	// LocatorTypeText pdf 文本锚（页码+行级 bbox）
	LocatorTypeText = "text"
	// LocatorTypeImage 图片锚（caption 块宿主：pdf=图片 placement bbox）
	LocatorTypeImage = "image"
	// LocatorTypeParagraph docx 段落号锚
	LocatorTypeParagraph = "paragraph"
	// LocatorTypeCharRange 字符区间锚（txt/md）
	LocatorTypeCharRange = "char_range"
)

// MetaKeySourcePageMap Document.Metadata 内部传递键：pdf 页-词映射 sidecar。
// 仅 parser→chunker 内部传递用；chunker 消费后必须从 chunk metadata 剔除，
// 坐标数据不进向量库（30.3 spec §4 拍板）。
const MetaKeySourcePageMap = "source_page_map"

// MetaKeySourceParagraphMap Document.Metadata 内部传递键：docx 段落映射 sidecar。
// 消费纪律同 MetaKeySourcePageMap。
const MetaKeySourceParagraphMap = "source_paragraph_map"

// MetaKeySourceCharRange Document.Metadata 标记键：值为 true 时 chunker
// 产 char_range 型 locator（txt/md 专用）。
const MetaKeySourceCharRange = "source_char_range"

// ──────────────────────────── 结构体 ────────────────────────────

// BBox 页面矩形 [x0, y0, x1, y1]，PDF 原生坐标（1/72 inch）
type BBox [4]float64

// PageWord 页内单词：文本 + 词框（gomupdf page.Words() 对应物）
type PageWord struct {
	// Text 单词文本
	Text string
	// BBox 词框
	BBox BBox
	// Line 词所属行号（gomupdf Word.Line，行级合并直接分组用）
	Line int
}

// PageImage 页内展示图片：placement bbox + 提取字节
type PageImage struct {
	// BBox 页面 placement 矩形
	BBox BBox
	// Data 原始编码字节（PNG/JPEG）
	Data []byte
	// Ext 图片编码扩展名（png/jpeg/…）
	Ext string
}

// CaptionAnchor caption 文本块锚（caption 非原文，锚到宿主图）
type CaptionAnchor struct {
	// Start/End 全文字符区间（半开）
	Start, End int
	// ImageIndex 宿主图在本页 Images 的下标
	ImageIndex int
}

// PageMap 单页映射（pdf sidecar 元素）
type PageMap struct {
	// PageNo 页号（1 起）
	PageNo int
	// PageText 该页拼装文本（与全文组装同语义）
	PageText string
	// Words 页内单词（含 bbox 与行号）
	Words []PageWord
	// Images 页内展示图片
	Images []PageImage
	// CaptionAnchors caption 锚（全文区间坐标）
	CaptionAnchors []CaptionAnchor
}

// ParagraphMap docx 段落映射 sidecar 元素
type ParagraphMap struct {
	// Paragraph 段落号（word_parser 既有 paragraphNum 语义）
	Paragraph int
	// Text 段落拼装文本（含 caption 追加段）
	Text string
}

// SourceLocator 单条原文定位
type SourceLocator struct {
	// Type 定位类型（LocatorType* 常量）
	Type string `json:"type"`
	// PageNo pdf 页号（1 起），其余类型 0
	PageNo int `json:"page_no,omitempty"`
	// LineBBoxes pdf 锚：行级合并矩形
	LineBBoxes []BBox `json:"line_bboxes,omitempty"`
	// Paragraph docx 段落号
	Paragraph int `json:"paragraph,omitempty"`
	// CharStart/CharEnd txt/md 全文字符区间（半开）
	CharStart int `json:"char_start,omitempty"`
	CharEnd   int `json:"char_end,omitempty"`
	// Quote 引用原文（WeKnora 同款：前端高亮时页内定位句子）
	Quote string `json:"quote,omitempty"`
}
