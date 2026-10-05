package indexer

import (
	"context"
	"os"

	storeEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	retrievalEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkEmbedOptions ComputeChunkEmbeddings 的选项结构。
//
// 对齐 Python compute_chunk_embeddings 的关键字参数：
//   - doc_index_callback=None → DocIndexCallback
//   - use_caption_for_images=False → UseCaptionForImages
//
// Python: openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py
type ChunkEmbedOptions struct {
	// UseCaptionForImages 为 true 时图片分块仅用文本/标题嵌入
	UseCaptionForImages bool
	// DocIndexCallback 嵌入进度回调，对齐 Python doc_index_callback
	// nil 表示不回调（对齐 Python doc_index_callback is None）
	DocIndexCallback common.DocIndexCallback
	// EmbedOpts 透传给 EmbedDocuments 的额外选项
	EmbedOpts []storeEmbedding.EmbedOption
}

// ChunkEmbedOption ComputeChunkEmbeddings 的可选参数函数。
type ChunkEmbedOption func(*ChunkEmbedOptions)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChunkEmbedOptions 从可变参数构建 ChunkEmbedOptions。
func NewChunkEmbedOptions(opts ...ChunkEmbedOption) ChunkEmbedOptions {
	o := ChunkEmbedOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithUseCaptionForImages 设置是否使用标题嵌入图片分块。
// 对齐 Python compute_chunk_embeddings(use_caption_for_images=...)。
func WithUseCaptionForImages(v bool) ChunkEmbedOption {
	return func(o *ChunkEmbedOptions) {
		o.UseCaptionForImages = v
	}
}

// WithDocIndexCallback 设置嵌入进度回调。
// 对齐 Python compute_chunk_embeddings(doc_index_callback=...)。
func WithDocIndexCallback(cb common.DocIndexCallback) ChunkEmbedOption {
	return func(o *ChunkEmbedOptions) {
		o.DocIndexCallback = cb
	}
}

// WithEmbedOpts 设置透传给 EmbedDocuments 的额外选项。
func WithEmbedOpts(opts ...storeEmbedding.EmbedOption) ChunkEmbedOption {
	return func(o *ChunkEmbedOptions) {
		o.EmbedOpts = append(o.EmbedOpts, opts...)
	}
}

// ComputeChunkEmbeddings 计算分块嵌入向量，原地修改 chunks 的 Embedding 字段。
//
// 三条路径：
//   - 纯文本路径：模型不支持 MultimodalEmbedder 或 UseCaptionForImages=true，
//     所有 chunks 批量 EmbedDocuments
//   - 图片路径：metadata 中有有效 image_path 的 chunks 逐个 EmbedMultimodal
//   - 文本路径：其余 chunks 批量 EmbedDocuments
//
// 对齐 Python compute_chunk_embeddings 签名：
//
//	async def compute_chunk_embeddings(chunks, embed_model, *,
//	    doc_index_callback=None, use_caption_for_images=False) -> None
//
// Python: openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py (compute_chunk_embeddings)
func ComputeChunkEmbeddings(
	ctx context.Context,
	chunks []common.TextChunk,
	embedModel storeEmbedding.BaseEmbedding,
	opts ...ChunkEmbedOption,
) error {
	embedOpts := NewChunkEmbedOptions(opts...)
	useCaptionForImages := embedOpts.UseCaptionForImages
	docIndexCallback := embedOpts.DocIndexCallback

	if len(chunks) == 0 {
		return nil
	}

	// 对齐 Python: embed_multimodal = getattr(embed_model, "embed_multimodal", None)
	// 对齐 Python: if not callable(embed_multimodal) or use_caption_for_images
	multimodal, multimodalOK := embedModel.(retrievalEmbedding.MultimodalEmbedder)

	// 构建 EmbedDocuments 的选项列表
	// 对齐 Python: kwargs = {} if doc_index_callback is None else {"callback_cls": doc_index_callback}
	embedDocOpts := embedOpts.EmbedOpts
	if docIndexCallback != nil {
		embedDocOpts = append(embedDocOpts, storeEmbedding.WithCallback(docIndexCallback))
	}

	// 纯文本路径：模型不支持多模态 或 用户要求使用标题
	if !multimodalOK || useCaptionForImages {
		logger.Info(logComponent).
			Bool("multimodal_supported", multimodalOK).
			Bool("use_caption_for_images", useCaptionForImages).
			Int("total_chunk_count", len(chunks)).
			Msg("使用纯文本嵌入路径")

		// 对齐 Python: texts = [chunk.text for chunk in chunks]
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}

		// 对齐 Python: kwargs = {} if doc_index_callback is None else {"callback_cls": doc_index_callback}
		// 对齐 Python: embeddings = await embed_model.embed_documents(texts, **kwargs)
		embeddings, err := embedModel.EmbedDocuments(ctx, texts, embedDocOpts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEXING_EMBED_ERROR").
				Str("method", "EmbedDocuments").
				Int("chunk_count", len(chunks)).
				Err(err).
				Msg("纯文本嵌入失败")
			return err
		}

		// 对齐 Python: for chunk, embedding in zip(chunks, embeddings): chunk.embedding = embedding
		for i, emb := range embeddings {
			chunks[i].Embedding = emb
		}
		return nil
	}

	// 对齐 Python: image_indices: List[int] = []
	// 对齐 Python: text_only: List[tuple[int, TextChunk]] = []
	var imageIndices []int
	type indexedChunk struct {
		index int
		chunk *common.TextChunk
	}
	var textOnly []indexedChunk

	// 对齐 Python: for i, chunk in enumerate(chunks):
	//   img_path = (chunk.metadata or {}).get("image_path")
	//   if img_path and os.path.isfile(img_path):
	//     image_indices.append(i)
	//   else:
	//     text_only.append((i, chunk))
	for i := range chunks {
		if isImageChunk(&chunks[i]) {
			imageIndices = append(imageIndices, i)
		} else {
			textOnly = append(textOnly, indexedChunk{index: i, chunk: &chunks[i]})
		}
	}

	logger.Info(logComponent).
		Bool("multimodal_supported", true).
		Int("image_chunk_count", len(imageIndices)).
		Int("text_chunk_count", len(textOnly)).
		Int("total_chunk_count", len(chunks)).
		Msg("使用混合嵌入路径")

	// 对齐 Python: # multimodal embeddings for image chunks
	// 对齐 Python: for idx in image_indices:
	for _, idx := range imageIndices {
		chunk := &chunks[idx]
		// 对齐 Python: path = Path(chunk.metadata["image_path"])
		imgPath, _ := chunk.Metadata["image_path"].(string)

		// 对齐 Python: multimodal_doc = (
		//   MultimodalDocument()
		//   .add_field("text", chunk.text or "")
		//   .add_field("image", file_path=path)
		// )
		// 注意：Python 用 chunk.text or ""，始终添加 text 字段（text 为空时传空串）
		doc := common.NewMultimodalDocument()
		if _, err := doc.AddField(common.ModalityText, chunk.Text); err != nil {
			return err
		}
		if _, err := doc.AddField(common.ModalityImage, "", common.FieldFilePath(imgPath)); err != nil {
			return err
		}

		// 对齐 Python: chunk.embedding = await embed_model.embed_multimodal(multimodal_doc)
		vec, err := multimodal.EmbedMultimodal(ctx, doc)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEXING_EMBED_ERROR").
				Str("method", "EmbedMultimodal").
				Int("chunk_index", idx).
				Str("image_path", imgPath).
				Err(err).
				Msg("多模态嵌入失败")
			return err
		}
		chunk.Embedding = vec
	}

	// 对齐 Python: # text embeddings for non-image chunks
	// 对齐 Python: if text_only:
	//   texts = [c.text for _, c in text_only]
	//   kwargs = {} if doc_index_callback is None else {"callback_cls": doc_index_callback}
	//   embeddings = await embed_model.embed_documents(texts, **kwargs)
	//   for (idx, chunk), emb in zip(text_only, embeddings):
	//     chunks[idx].embedding = emb
	if len(textOnly) > 0 {
		texts := make([]string, len(textOnly))
		for i, ic := range textOnly {
			texts[i] = ic.chunk.Text
		}

		embeddings, err := embedModel.EmbedDocuments(ctx, texts, embedDocOpts...)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "INDEXING_EMBED_ERROR").
				Str("method", "EmbedDocuments").
				Int("chunk_count", len(textOnly)).
				Err(err).
				Msg("文本分块嵌入失败")
			return err
		}

		for i, ic := range textOnly {
			ic.chunk.Embedding = embeddings[i]
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isImageChunk 判断 TextChunk 是否为有效图片分块。
//
// 三级防御对齐 Python os.path.isfile(img_path)：
//  1. metadata 中无 image_path 键 → false
//  2. image_path 类型不是 string 或为空串 → false
//  3. 文件不存在或是目录 → false
//
// Python: img_path = (chunk.metadata or {}).get("image_path")
// Python: if img_path and os.path.isfile(img_path)
func isImageChunk(chunk *common.TextChunk) bool {
	// 对齐 Python: (chunk.metadata or {}).get("image_path")
	// Go 的 nil map 访问不 panic，直接取值
	rawPath, ok := chunk.Metadata["image_path"]
	if !ok {
		return false
	}
	imgPath, ok := rawPath.(string)
	if !ok || imgPath == "" {
		return false
	}
	// 对齐 Python: os.path.isfile(img_path)
	info, err := os.Stat(imgPath)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
