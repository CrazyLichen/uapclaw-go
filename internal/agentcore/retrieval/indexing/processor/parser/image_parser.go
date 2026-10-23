package parser

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ImageParser 图片文件解析器。
//
// 嵌入 BaseParser 但覆写整体 Parse（非仅 ParseContent）——因为要产出
// metadata["image_path"]，BaseParser 模板的"非空内容才包 Document"逻辑
// 不适用（对齐 Python ImageParser 覆写整体 parse）。LazyParse/Process/
// Supports 继承 BaseParser 默认（经 self 动态派发，覆写 Parse 即生效）。
//
// 归档路径策略（B1，spec 决策 4）：Parse 单次 CpImage 后把归档路径传给
// ParseContent，caption 全程读副本（语义等价：caption 读文件内容，
// 副本≡原件）。Python _parse 内返回值丢弃的第二次 cp_image 按去重决策
// 移除（spec 偏差 §8-1/8-5）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/image_parser.py (ImageParser)
type ImageParser struct {
	// BaseParser 默认适配器（LazyParse/Process/Supports 默认实现）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 ImageParser 实现 Parser
var _ Parser = (*ImageParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewImageParser 构造 ImageParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）；NewBaseParser(self)
// 注入使模板方法动态派发到本类覆写。
//
// Python: image_parser.py ImageParser.__init__
func NewImageParser() (*ImageParser, error) {
	p := &ImageParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// Parse 解析图片文件：归档 + 生成 caption + 返回带 image_path 元数据的文档。
//
// 与 BaseParser 默认实现的差异（覆写原因）：
//  1. caption 为空也返回单个 Document（禁用态≠解析失败——空文本 chunk
//     流到 13.4 ComputeChunkEmbeddings 时 isImageChunk 靠 image_path 命中，
//     走 EmbedMultimodal 纯图片嵌入路径）
//  2. metadata 固定 {"image_path": 归档路径}（13.4 isImageChunk 的消费键）
//
// 软失败：CpImage/内部失败 → Error 日志 + 空切片（对齐 Python except → []）。
//
// Python: image_parser.py ImageParser.parse
func (p *ImageParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)

	savedPath, err := CpImage(doc)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse image {doc}: {e}")
		logger.Error(logComponent).
			Str("doc", doc).
			Err(err).
			Msg("failed to parse image")
		return []reranker.Document{}, nil
	}

	content, err := p.ParseContent(ctx, savedPath, o.LLMClient)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse image {doc}: {e}")
		logger.Error(logComponent).
			Str("doc", doc).
			Err(err).
			Msg("failed to parse image")
		return []reranker.Document{}, nil
	}

	return []reranker.Document{{
		ID:   o.DocID,
		Text: content,
		Metadata: map[string]any{
			"image_path": savedPath,
		},
	}}, nil
}

// ParseContent 对齐 Python _parse：构造 captioner 对归档路径生成 caption。
//
// B1 落点：doc 参数为调用方（Parse）传入的归档路径；单独调用时视为
// 任意图片路径（自含：构造 captioner + caption，不再重复归档）。
// 空 caption → 返回空串（对齐 Python return None 的 falsy 语义）。
//
// Python: image_parser.py ImageParser._parse
func (p *ImageParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	captioner := NewImageCaptioner(llmClient)
	captions := captioner.CaptionImages(ctx, []string{doc})

	// 对齐 Python: "\n".join([caption for caption in captions if caption]) if captions else None
	var nonEmpty []string
	for _, c := range captions {
		if c != "" {
			nonEmpty = append(nonEmpty, c)
		}
	}
	return strings.Join(nonEmpty, "\n"), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册到 registry（对齐 Python @register_parser([".png", ".jpg",
// ".jpeg", ".webp", ".gif", ".jfif"]) import 时执行）。
func init() {
	RegisterParser([]string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".jfif"}, func() Parser {
		p, _ := NewImageParser()
		return p
	})
}
