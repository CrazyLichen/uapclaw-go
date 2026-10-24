package parser

import (
	"context"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"
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
// M-13: 解码后剥离 U+FFFD（对齐 Python errors="ignore" 非法字节丢弃语义——
// x/text Decoder 默认替换策略产出 U+FFFD，Python 直接丢弃非法字节；
// 误伤合法 U+FFFD 概率极低，属可接受近似）。所有解码路径统一剥离。
func decodeDetected(raw []byte) (string, error) {
	enc := detectEncoding(raw)
	var decoded []byte
	if enc == nil {
		decoded = raw
	} else {
		var err error
		decoded, err = enc.NewDecoder().Bytes(raw)
		if err != nil {
			return "", err
		}
	}
	s := strings.TrimPrefix(string(decoded), "\uFEFF")
	return strings.ReplaceAll(s, "\uFFFD", ""), nil
}

// detectEncoding 检测字节流编码，返回 x/text encoding（nil 表示 UTF-8 原样）。
//
// chardet.DetectBest 空输入返回 (nil, nil)——result 为 nil 且 err 为 nil，
// 双重防御（探针实测）。UTF-8 检出时返回 nil 走零转换路径。
// chardet 检出 GBK 系编码名带连字符（"GB-18030"），htmlindex 不认，
// 手工映射 simplifiedchinese.GB18030（GB18030 是 GBK 超集，GBK 字节流
// 按 GB18030 解码结果正确——探针验证中文完整还原）。
//
// M-12: ① chardet 检出 "UTF-32LE"/"UTF-32BE" 时 htmlindex.Lookup 返回
// nil（不认该名），原实现静默回落 UTF-8 原样 → UTF-32 文件全乱码——
// 手工映射 unicode.UTF32.LittleEndian/BigEndian；② 查表失败补 Warn
// 日志（可观测性，原静默吞掉）；③ 短文本 GBK 乱码差异（chardet 判
// ISO-8859-7 vs Python charset_normalizer 判 CP949）属检测器选型既定
// 限制——长文本（>30 字符）双端一致，短文本差异为已知可接受偏差。
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
	case "utf-32le":
		return utf32.UTF32(utf32.LittleEndian, utf32.UseBOM)
	case "utf-32be":
		return utf32.UTF32(utf32.BigEndian, utf32.UseBOM)
	case "iso-8859-1", "latin-1", "iso8859-1", "iso_8859-1":
		// M-12: charset.Lookup("ISO-8859-1") 实际返回 Windows-1252（0x80-0x9F
		// 区间解出曲引号/€ 等扩展图形字符）；Python codec 纯 Latin-1 该区间
		// 为 C1 控制符。Latin-1 全 256 字节一一对应码点，手工实现对齐。
		return latin1Encoding{}
	default:
		e, _ := charset.Lookup(name)
		if e == nil {
			// M-12: 查表失败原静默吞掉，补 Warn 日志提升可观测性
			logger.Warn(logComponent).
				Str("detected_charset", name).
				Msg("charset lookup failed, fallback to utf-8 passthrough")
			return nil
		}
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

// latin1Encoding ISO-8859-1（Latin-1）单字节编码：字节值即码点。
//
// M-12: x/net htmlindex 的 "ISO-8859-1" 实为 Windows-1252（0x80-0x9F 区间
// 映射扩展图形字符），Python codec 严格 Latin-1（该区间为 C1 控制符）。
// Latin-1 全 256 字节一一对应码点，手工实现对齐 Python。
type latin1Encoding struct{}

// NewDecoder 构造 Latin-1 解码器。
func (latin1Encoding) NewDecoder() *encoding.Decoder {
	return &encoding.Decoder{Transformer: latin1Decoder{}}
}

// NewEncoder 构造 Latin-1 编码器。
func (latin1Encoding) NewEncoder() *encoding.Encoder {
	return &encoding.Encoder{Transformer: latin1Encoder{}}
}

// latin1Decoder Latin-1 → UTF-8：字节值即 Unicode 码点。
type latin1Decoder struct{}

// Reset 实现 transform.Transformer（无状态，空操作）。
func (latin1Decoder) Reset() {}

// Transform 逐字节转换（ASCII 直通，0x80-0xFF 双字节 UTF-8）。
func (latin1Decoder) Transform(dst, src []byte, atEOF bool) (int, int, error) {
	n := 0
	consumed := 0
	for _, b := range src {
		if b < 0x80 {
			if n+1 > len(dst) {
				return n, consumed, transform.ErrShortDst
			}
			dst[n] = b
			n++
		} else {
			if n+2 > len(dst) {
				return n, consumed, transform.ErrShortDst
			}
			dst[n] = 0xC0 | b>>6
			dst[n+1] = 0x80 | b&0x3F
			n += 2
		}
		consumed++
	}
	return n, consumed, nil
}

// latin1Encoder UTF-8 → Latin-1（超 0xFF 码点转 U+FFFD 由上层清洗）。
type latin1Encoder struct{}

// Reset 实现 transform.Transformer（无状态，空操作）。
func (latin1Encoder) Reset() {}

// Transform 逐 rune 转换。
func (latin1Encoder) Transform(dst, src []byte, atEOF bool) (int, int, error) {
	n := 0
	i := 0
	for i < len(src) {
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size <= 1 {
			if atEOF {
				return n, i, nil
			}
			return n, i, transform.ErrShortSrc
		}
		if n+1 > len(dst) {
			return n, i, transform.ErrShortDst
		}
		if r > 0xFF {
			r = utf8.RuneError
		}
		dst[n] = byte(r)
		n++
		i += size
	}
	return n, i, nil
}
