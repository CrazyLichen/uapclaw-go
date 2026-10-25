package parser

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AutoFileParser 扩展名自动路由解析器。
//
// 组合解析器（13.10 spec 预告模式）：嵌入 BaseParser + 构造注入 self +
// 覆写整体 Parse/Supports（Python 覆写 parse/supports 非私有 _parse）。
// 按文件扩展名从 parserRegistry 取工厂实例路由（registry 骨架由 13.16
// 带出，各具体解析器 init 注册；Python ensure_parsers_loaded 的动态 import
// 在 Go 由同包 init 天然完成——ImportError 容错编译期不可能，零代码归档）。
//
// Python: auto_file_parser.py AutoFileParser
type AutoFileParser struct {
	// BaseParser 模板适配器（组合解析器复用 LazyParse/Process/ParseContent 默认行为）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewAutoFileParser 构造 AutoFileParser。
//
// Python: auto_file_parser.py AutoFileParser.__init__
// （self._ensure_parsers_loaded() 对应物为 Go 同包 init 注册，构造期天然完成）
func NewAutoFileParser() (*AutoFileParser, error) {
	p := &AutoFileParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// Parse 解析文件：按扩展名路由到注册的具体解析器并增强 metadata。
//
// 五步对齐 Python AutoFileParser.parse：
//  1. 文件不存在 → 155103（error_msg "File {doc} does not exist"）
//  2. 扩展名小写归一后未注册 → 155104（error_msg 含扩展名与已注册列表——
//     sort+Join 格式，决策 4，chunker GetChunker 13.5 先例）
//  3. 工厂新实例 + Info 日志（parserTypeName 实例类型名——13.16 已知偏差延续）
//  4. 底层 parser.Parse 原样全量转发（DocID/LLMClient/Extra/四网页字段，
//     对齐 Python doc_id/llm_client/kwargs 全透传；工厂每次新实例）
//  5. 空结果→空切片；否则 Extra["file_name"] 键存在即原值透传（any 含
//     非字符串，决策 5 字面对齐 Python dict.get 原样返回），键缺失兜底
//     filepath.Base(doc)；逐文档 Metadata 写四键（决策 6 nil 先建——
//     防御第三方解析器契约违背）：doc_id（字面含空串）/ title / file_path / file_ext
//
// Python: auto_file_parser.py AutoFileParser.parse
func (p *AutoFileParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	_ = ctx // 对齐 Python 同步语义（async 协程内的直调为 Go 直接调用）
	o := NewParseOptions(opts...)

	// 对齐 Python: if not os.path.exists(doc)（exists 吞一切 OSError 含
	// ENOTDIR/权限错均返回 False，故 err != nil 全判字面等价——偏差 #11 修复）
	if _, err := os.Stat(doc); err != nil {
		// 对齐 Python: raise build_error(RETRIEVAL_INDEXING_FILE_NOT_FOUND,
		//   error_msg=f"File {doc} does not exist")
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFileNotFound,
			exception.WithMsg("File "+doc+" does not exist"),
		)
	}

	// 对齐 Python: file_ext = os.path.splitext(doc)[-1].lower()
	fileExt := strings.ToLower(filepath.Ext(doc))
	factory, ok := getParserFactory(fileExt)
	if !ok {
		// 对齐 Python: raise build_error(RETRIEVAL_INDEXING_FORMAT_NOT_SUPPORT,
		//   error_msg=f"Unsupported format: {file_ext}, only {list(_PARSER_REGISTRY.keys())} are supported")
		exts := snapshotRegistry()
		sort.Strings(exts)
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFormatNotSupport,
			exception.WithMsg("Unsupported format: "+fileExt+", only "+strings.Join(exts, ", ")+" are supported"),
		)
	}

	// 对齐 Python: parser = _PARSER_REGISTRY[file_ext]()
	parser := factory()
	// 对齐 Python: logger.info(f"Using {parser.__class__.__name__} to parse {doc}")
	logger.Info(logComponent).
		Str("parser_name", parserTypeName(parser)).
		Str("doc", doc).
		Msg("using parser to parse doc")

	// 对齐 Python: documents = await parser.parse(doc, doc_id, llm_client=llm_client, **kwargs)
	documents, err := parser.Parse(ctx, doc, opts...)
	if err != nil {
		return nil, err
	}
	// 对齐 Python: if not documents: return []
	if len(documents) == 0 {
		return []reranker.Document{}, nil
	}

	// 对齐 Python: file_name = kwargs.get("file_name", os.path.basename(doc))
	// 决策 5 字面透传：键存在即原值 any（含非字符串），仅键缺失兜底
	var fileName any
	if v, ok := o.Extra["file_name"]; ok {
		fileName = v
	} else {
		fileName = filepath.Base(doc)
	}

	// 对齐 Python: for document in documents: document.metadata.update({...四键})
	for i := range documents {
		if documents[i].Metadata == nil {
			// 决策 6 防御：第三方解析器可能返回 nil Metadata；
			// Python 同场景 AttributeError 被 KB 层 except 吞，Go panic 不可恢复
			documents[i].Metadata = map[string]any{}
		}
		documents[i].Metadata["doc_id"] = o.DocID
		documents[i].Metadata["title"] = fileName
		documents[i].Metadata["file_path"] = doc
		documents[i].Metadata["file_ext"] = fileExt
	}

	return documents, nil
}

// Supports 检查是否支持该文档源：文件存在且扩展名已注册。
//
// 对齐 Python os.path.exists 吞一切 OSError→False（偏差 #11：err != nil 全判）。
//
// Python: auto_file_parser.py AutoFileParser.supports
func (p *AutoFileParser) Supports(doc string) bool {
	if _, err := os.Stat(doc); err != nil {
		return false
	}
	_, ok := getParserFactory(strings.ToLower(filepath.Ext(doc)))
	return ok
}

// RegisterNewParser 运行时动态注册新解析器工厂。
//
// last-wins 无条件覆盖 + Info 日志（Python register_new_parser classmethod
// 对应物）。与 RegisterParser（first-wins 静默跳过）并存——Python 两入口
// 语义本就不同，各自忠实对齐（13.16 spec 决策 5 兑现）。
//
// Python: auto_file_parser.py AutoFileParser.register_new_parser
func RegisterNewParser(fileExtension string, factory func() Parser) {
	normalizedExt := strings.ToLower(fileExtension)
	parserRegistryMu.Lock()
	parserRegistry[normalizedExt] = factory
	parserRegistryMu.Unlock()
	// 对齐 Python: logger.info(f"Dynamically registered parser for {normalized_ext}")
	logger.Info(logComponent).
		Str("ext", normalizedExt).
		Msg("dynamically registered parser")
}

// GetSupportedFormats 返回全部已注册扩展名列表。
//
// Python: auto_file_parser.py AutoFileParser.get_supported_formats
func GetSupportedFormats() []string {
	return snapshotRegistry()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
