# 13.4 ComputeChunkEmbeddings 设计

> 共享嵌入逻辑：文本 embed_documents + 多模态 embed_multimodal + image_path 检测 + use_caption_for_images 分支

## 概述

`ComputeChunkEmbeddings` 是文档索引管线中"分块 → **嵌入** → 入库"环节的共享函数，被 ChromaIndexer（13.2）和 MilvusIndexer（13.3）的 `BuildIndex`/`UpdateIndex` 共同调用，负责将 `[]TextChunk` 的 `Embedding` 字段原地填充。

Python 参考：`openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py`

## 函数签名

```go
// ComputeChunkEmbeddings 计算分块嵌入向量，原地修改 chunks 的 Embedding 字段。
//
// 三条路径：
//   - 纯文本路径：模型不支持 MultimodalEmbedder 或 useCaptionForImages=true，
//     所有 chunks 批量 EmbedDocuments
//   - 图片路径：metadata 中有有效 image_path 的 chunks 逐个 EmbedMultimodal
//   - 文本路径：其余 chunks 批量 EmbedDocuments
//
// Python: openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py (compute_chunk_embeddings)
func ComputeChunkEmbeddings(
    ctx context.Context,
    chunks []common.TextChunk,
    embedModel embedding.BaseEmbedding,
    useCaptionForImages bool,
    opts ...embedding.EmbedOption,
) error
```

**文件位置**：`internal/agentcore/retrieval/indexing/indexer/embed_chunks.go`

**选择理由**：纯函数式，与 Python 模块级 `async def compute_chunk_embeddings` 完全对齐；无额外类型/接口开销；参数数量与 Python 对等（4 个必选 + 可选 opts）。

## 核心逻辑流程

```
1. 接口断言：multimodal, ok := embedModel.(embedding.MultimodalEmbedder)
2. if !ok || useCaptionForImages:
     → 纯文本路径：所有 chunks 一次性 EmbedDocuments，填 Embedding
     → return
3. 遍历 chunks，分类：
     imageIndices []int         — metadata["image_path"] 存在且文件存在
     textOnly     []struct{i int; c *common.TextChunk}  — 其余
4. 图片路径：for _, idx := range imageIndices:
     → 构造 MultimodalDocument：AddField("text", chunk.Text) + AddField("image", "", FieldFilePath(imgPath))
     → multimodal.EmbedMultimodal(ctx, doc, ...)
     → chunks[idx].Embedding = result
5. 文本路径：if textOnly 非空:
     → EmbedDocuments(ctx, texts, opts...)
     → 逐个填 Embedding
6. return nil
```

## image_path 检测细节

从 `TextChunk.Metadata`（`map[string]any`）中提取 `image_path` 并校验，对齐 Python `os.path.isfile(img_path)`：

```go
rawPath, ok := chunk.Metadata["image_path"]
if !ok { /* 归入 textOnly */ }
imgPath, ok := rawPath.(string)
if !ok || imgPath == "" { /* 归入 textOnly */ }
info, err := os.Stat(imgPath)
if err != nil || info.IsDir() { /* 归入 textOnly */ }
// 否则归入 imageIndices
```

三级防御：key 不存在 → 类型断言失败 → 文件不存在/是目录，任一失败均降级到纯文本嵌入。

## MultimodalDocument 构造

对齐 Python 的 `.add_field("text", chunk.text or "").add_field("image", file_path=path)`：

```go
doc := common.NewMultimodalDocument()
if chunk.Text != "" {
    if _, err := doc.AddField(common.ModalityText, chunk.Text); err != nil {
        return err
    }
}
if _, err := doc.AddField(common.ModalityImage, "", common.FieldFilePath(imgPath)); err != nil {
    return err
}
```

Go 的 `AddField` 返回 `(*MultimodalDocument, error)`，需处理 error。

## 错误处理

| 场景 | 行为 |
|------|------|
| 空 chunks 列表 | 返回 nil（无操作，无错误） |
| EmbedDocuments 失败 | 直接返回 error |
| 单个 EmbedMultimodal 失败 | 直接返回 error（对齐 Python：任一失败即中断） |
| AddField 构造 MultimodalDocument 失败 | 直接返回 error |

无部分成功语义，与 Python 行为一致。

## 日志同步

Python `embed_chunks.py` 无 logger 调用（纯逻辑函数），但按项目日志规则在关键分支点添加防御性日志：

| 日志点 | 级别 | 字段 |
|--------|------|------|
| 多模态能力检测结果 | Info | `multimodal_supported`, `use_caption_for_images` |
| 图片/文本分块数量 | Info | `image_chunk_count`, `text_chunk_count`, `total_chunk_count` |
| EmbedDocuments 失败 | Error | `event_type=INDEXING_EMBED_ERROR`, `method=EmbedDocuments`, `chunk_count` |
| EmbedMultimodal 失败 | Error | `event_type=INDEXING_EMBED_ERROR`, `method=EmbedMultimodal`, `chunk_index`, `image_path` |

## 实现顺序调整

在 `IMPLEMENTATION_PLAN.md` 中将 13.4 调整到 13.2 之前：

```
13.1 ✅ → 13.4 ☐ → 13.2 ☐ → 13.3 ☐ → 13.5 ...
```

理由：13.4 是 13.2/13.3 的共享依赖，两个 Indexer 的 BuildIndex 都需先调用 ComputeChunkEmbeddings 填充 embedding。

## 测试策略

| 测试场景 | 方法 |
|---------|------|
| 模型不支持 MultimodalEmbedder → 走纯文本路径 | fakeBaseEmbedding（不实现 MultimodalEmbedder） |
| useCaptionForImages=true → 走纯文本路径 | fakeMultimodalEmbedder + flag=true |
| 图片+文本混合 → 分别走对应路径 | fakeMultimodalEmbedder |
| image_path 文件不存在 → 降级到文本 | 同上 + 无效路径 |
| image_path 为非 string 类型 → 降级到文本 | metadata 填 int |
| metadata 中无 image_path 键 → 走文本 | 无 image_path 的 metadata |
| image_path 为空字符串 → 降级到文本 | metadata 填空串 |
| 空 chunks → 返回 nil | 空切片 |
| EmbedDocuments 失败 → 返回 error | fake 返回 error |
| EmbedMultimodal 失败 → 返回 error | fake 返回 error |
| AddField 构造失败 → 返回 error | 无效文件路径触发 |

所有 mock 用同包 fake 结构体，不依赖外部服务。目标覆盖率 ≥ 85%。

## 依赖组件（已就绪）

| 组件 | 位置 | 状态 |
|------|------|------|
| TextChunk（含 Embedding 字段） | `retrieval/common/document.go` | ✅ |
| IndexConfig（含 UseCaptionForImages） | `retrieval/common/config.go` | ✅ |
| BaseEmbedding 接口 | `foundation/store/embedding/base.go` | ✅ |
| MultimodalEmbedder 接口 | `retrieval/embedding/common.go` | ✅ |
| MultimodalDocument + AddField + FieldFilePath | `retrieval/common/document.go` | ✅ |
| EmbedOption / WithCallback / WithBatchSize | `foundation/store/embedding/base.go` | ✅ |
