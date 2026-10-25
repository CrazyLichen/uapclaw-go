package parser

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AutoParser 顶层路由解析器：URL → AutoLinkParser，否则 → AutoFileParser。
//
// 组合解析器（13.10 spec 预告模式）：嵌入 BaseParser + 构造注入 self +
// 覆写整体 Parse/Supports。单入口 parse(path_or_url)——混源输入无需
// 区分 parse_urls / parse_files。
//
// Python: auto_parser.py AutoParser
type AutoParser struct {
	// BaseParser 模板适配器
	*BaseParser
	// linkParser URL 路由解析器
	linkParser Parser
	// fileParser 文件路由解析器
	fileParser Parser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewAutoParser 构造 AutoParser。
//
// linkParser/fileParser 为 nil 时各自默认构造（对齐 Python None 默认参数）。
//
// Python: auto_parser.py AutoParser.__init__
func NewAutoParser(linkParser, fileParser Parser) (*AutoParser, error) {
	if linkParser == nil {
		lp, err := NewAutoLinkParser(nil)
		if err != nil {
			return nil, err
		}
		linkParser = lp
	}
	if fileParser == nil {
		fp, err := NewAutoFileParser()
		if err != nil {
			return nil, err
		}
		fileParser = fp
	}
	p := &AutoParser{linkParser: linkParser, fileParser: fileParser}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// Parse 解析文档源：URL 走 link 路由，否则走 file 路由。
//
// isLikelyURL && linkParser.Supports → Debug 日志 → linkParser.Parse
// 全量透传（含 LLMClient——对齐 Python 传递，与 AutoLinkParser 内部剔除
// 形成链路分层：顶层传、路由层丢弃）；
// fileParser.Supports → Debug 日志 → fileParser.Parse 全量透传；
// 都不中 → 空切片。URL-but-unsupported fall-through 到 file 分支
//（Python 真实控制流字面保留：is_likely_url 为真但 link 不支持时
// 不提前返回，落到 file 分支判定）。
//
// Python: auto_parser.py AutoParser.parse
func (p *AutoParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	if isLikelyURL(doc) && p.linkParser.Supports(doc) {
		// 对齐 Python: logger.debug("AutoParser delegating to link parser")
		logger.Debug(logComponent).Msg("AutoParser delegating to link parser")
		return p.linkParser.Parse(ctx, doc, opts...)
	}
	if p.fileParser.Supports(doc) {
		// 对齐 Python: logger.debug("AutoParser delegating to file parser")
		logger.Debug(logComponent).Msg("AutoParser delegating to file parser")
		return p.fileParser.Parse(ctx, doc, opts...)
	}
	return []reranker.Document{}, nil
}

// Supports 检查是否支持该文档源：URL 判定后委托对应子解析器。
//
// Python: auto_parser.py AutoParser.supports
func (p *AutoParser) Supports(doc string) bool {
	if isLikelyURL(doc) {
		return p.linkParser.Supports(doc)
	}
	return p.fileParser.Supports(doc)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isLikelyURL 判定文档源是否像 http(s) URL（复用 13.14 httpURLPattern）。
//
// Python: auto_parser.py _is_likely_url
//（bool(doc and HTTP_URL_PATTERN.match(doc.strip()))）
func isLikelyURL(doc string) bool {
	return doc != "" && httpURLPattern.MatchString(strings.TrimSpace(doc))
}
