package parser

import (
	"context"
	"os"
	"strings"

	"github.com/saintfish/chardet"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TxtMdParser 文本/Markdown 解析器。
//
// 嵌入 BaseParser 仅覆写 ParseContent。编码统计检测用 chardet
// （对齐 Python charset_normalizer.detect），经 x/net Lookup 映射
// x/text encoding 解码。软失败语义同 JSONParser。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/txt_md_parser.py (TxtMdParser)
type TxtMdParser struct {
	// BaseParser 默认适配器
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 编译期断言 TxtMdParser 实现 Parser
	_ Parser = (*TxtMdParser)(nil)
	// textDetector 编码检测器（chardet 探测器无状态可复用）
	textDetector = chardet.NewTextDetector()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTxtMdParser 构造 TxtMdParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）。
//
// Python: txt_md_parser.py TxtMdParser.__init__
func NewTxtMdParser() (*TxtMdParser, error) {
	p := &TxtMdParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ParseContent 解析 TXT/MD 文件内容。
//
// 对齐 Python _parse 步骤顺序：
//  1. 读原始字节（对齐 aiofiles.open(file_path, "rb")）
//  2. chardet 统计检测编码；检测失败/空输入 → utf-8
//     （对齐 detect 返回 None 分支）
//  3. 编码名映射 x/text encoding 解码（对齐 open(encoding=..., errors="ignore")；
//     GB-18030 手工归一——htmlindex 不认带连字符名，探针实测）
//  4. TrimSpace；空串 → ("", nil)（对齐 content.strip() if content else None）
//  5. 整体失败 → ("", nil) 软失败 + Error 日志（对齐外层 except return None）
//
// Python: txt_md_parser.py TxtMdParser._parse
func (p *TxtMdParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	_ = ctx // 对齐 Python 无 ctx 消费
	_ = llmClient
	raw, err := os.ReadFile(doc)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse TXT/MD {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse TXT/MD")
		return "", nil
	}
	content, err := decodeDetected(raw)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse TXT/MD {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse TXT/MD")
		return "", nil
	}
	if content == "" {
		return "", nil
	}
	return strings.TrimSpace(content), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decodeDetected 统计检测编码并解码字节流。
//
// 检测/映射失败回落 utf-8 容错解码（对齐 Python detect 返回 None 或
// 未知编码名时 open 回落语义）。UTF-8 BOM 前缀剥离（对齐 Python
// strip 对 \ufeff 兜底处理的最终输出）。
func decodeDetected(raw []byte) (string, error) {
	enc := detectEncoding(raw)
	if enc == nil {
		return strings.TrimPrefix(string(raw), "\uFEFF"), nil
	}
	decoded, err := enc.NewDecoder().Bytes(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(string(decoded), "\uFEFF"), nil
}

// detectEncoding 检测字节流编码，返回 x/text encoding（nil 表示 UTF-8 原样）。
//
// chardet.DetectBest 空输入返回 (nil, nil)——result 为 nil 且 err 为 nil，
// 双重防御（探针实测）。UTF-8 检出时返回 nil 走零转换路径。
// chardet 检出 GBK 系编码名带连字符（"GB-18030"），htmlindex 不认，
// 手工映射 simplifiedchinese.GB18030（GB18030 是 GBK 超集，GBK 字节流
// 按 GB18030 解码结果正确——探针验证中文完整还原）。
func detectEncoding(raw []byte) encoding.Encoding {
	if len(raw) == 0 {
		return nil
	}
	result, err := textDetector.DetectBest(raw)
	if err != nil || result == nil {
		return nil
	}
	name := strings.TrimSpace(result.Charset)
	switch strings.ToLower(name) {
	case "utf-8":
		return nil // 原样路径（BOM 由 decodeDetected 剥离）
	case "gb-18030", "gb18030":
		return simplifiedchinese.GB18030
	default:
		e, _ := charset.Lookup(name)
		return e
	}
}

// init 注册到 registry（对齐 Python @register_parser([".txt", ".TXT", ".md", ".MD", ".markdown", ".MARKDOWN"])）。
func init() {
	RegisterParser([]string{".txt", ".TXT", ".md", ".MD", ".markdown", ".MARKDOWN"}, func() Parser {
		p, _ := NewTxtMdParser()
		return p
	})
}
