# 代码审查报告：领域四 存储层（4.18-4.28）

> **审查日期**: 2025-06-15
> **审查范围**: 24小时内提交记录覆盖的领域四功能（4.18 SimpleMemoryIndex → 4.28 Query Builder）
> **Python 参考项目**: openjiuwen (agent-core) + jiuwenswarm
> **审查方法**: 逐模块对照 Python 参考实现，检查功能符合度、逻辑缺陷、日志对齐、错误处理

---

## 审查摘要

| 模块 | 严重 | 一般 | 提示 | 总计 |
|------|------|------|------|------|
| 4.18 SimpleMemoryIndex + retrieval/common | 1 | 5 | 4 | 10 |
| 4.19-4.22 Embedding | 3 | 6 | 8 | 17 |
| 4.23-4.25 Reranker | 2 | 8 | 5 | 15 |
| 4.26 Graph Store | 4 | 10 | 7 | 21 |
| 4.27 Object Store (S3) | 1 | 3 | 3 | 7 |
| 4.28 Query Builder | 1 | 3 | 4 | 8 |
| **合计** | **12** | **35** | **31** | **78** |

---

## 一、严重问题（12 项）

### S-01. Graph Store BM25 搜索使用了错误输入 — dense 向量代替查询文本

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:229`
- **Python 参考**: `milvus_support.py:689-695`
- **问题**: Go 实现中 BM25 搜索通道 (`content_bm25`) 使用 `vectors`（dense embedding 向量）作为搜索输入，但 Python 中 BM25 通道使用原始查询文本 `[query]`。BM25 是稀疏搜索，其输入应该是文本字符串而非 dense 向量。Milvus BM25 Function 需要文本输入来生成分词稀疏向量。
- **影响**: BM25 稀疏搜索完全无法正常工作或返回错误结果。

```go
// 当前错误实现
sparseReq := milvusclient.NewAnnRequest("content_bm25", k, vectors...)  // ← 错误使用 vectors
// 应改为使用查询文本
```

### S-02. Graph Store BFS 搜索逻辑与 Python 严重不一致

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:103-157`
- **Python 参考**: `milvus_support.py:215-277`
- **问题**: BFS 实现与 Python 差异巨大：
  1. 对 Entity 扩展：Go 通过 Entity 集合的 `relations`/`episodes` 字段，**缺少通过 Relation 集合按 `lhs/rhs` 扩展的步骤**
  2. 对 Relation 扩展：Go 只查 Relation 集合的 `lhs`/`rhs`，**缺少查 Entity 集合的 `relations` 字段**
  3. **缺少 `bfs_k` 裁剪逻辑**（Python 中按 distance 排序只保留 top-k）
  4. **缺少 `skip_ranking` 优化**（Python 每轮搜索用 skip_ranking=True，最后做排序）
- **影响**: 图搜索召回率不足，搜索质量与 Python 版本差距大。

### S-03. Graph Store `combinedRerank` 逻辑不完整 — 缺少关系增强

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:398-433`
- **Python 参考**: `milvus_support.py:477-498`
- **问题**: Python 的 `_combined_rerank` 有利用关系信息增强实体排序的关键逻辑：遍历每个 Entity 的 `relations`，将关联 Relation 的 content 拼接到 Entity 的 content 中再 rerank。Go 只按集合独立 rerank，完全缺少关系增强逻辑。
- **影响**: 实体排序质量低，相关实体可能排在无关实体后面。

### S-04. Graph Store Relation EmbedTasks 与 Schema 不匹配

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/schema.go:141-190` + `foundation/store/graph/graph_object.go:192-197`
- **问题**: `Relation.EmbedTasks()` 返回 `content_embedding` + `name_embedding` 两个任务，但 Relation Schema 中没有 `name_embedding` 向量字段。Python 中虽然 Relation 也继承 `NamedGraphObject` 返回 name_embedding 任务，但 `ToMap()` 时 `name_embedding=None` 被 `{k:v for k,v in ... if v is not None}` 过滤。Go 的 `ToMap()` 在 `name_embedding==nil` 时不序列化，所以不会报错，但**浪费嵌入计算资源**。
- **影响**: 浪费 LLM 调用资源计算不会被写入的 name_embedding。

### S-05. DashScope Embedding 5xx 错误不可重试

- **模块**: 4.21 DashScopeEmbedding
- **文件**: `retrieval/embedding/dashscope.go:237` + `dashscope.go:295-300`
- **Python 参考**: `dashscope_embedding.py` — 所有网络错误（含 5xx）都可重试
- **问题**: `DashscopeEmbedding.callAPI` 使用 `RetryWithBackoff`（内部 `defaultIsRetryable`），但 `handleDashscopeAPIResp` 在非 200 时返回 `BuildError`，按 keyword 规则 `CALL → Framework → recoverable=false → 不可重试`。5xx 服务端错误也不可重试，与 Python 严重不一致。
- **影响**: DashScope API 服务端临时故障时无法自动恢复。

### S-06. DashScope Embedding 非 200 响应在非最后一次重试时返回 (nil, nil)

- **模块**: 4.21 DashScopeEmbedding
- **文件**: `retrieval/embedding/dashscope.go:329-344`
- **Python 参考**: `dashscope_embedding.py:220-261`
- **问题**: 当 `resp.StatusCode != 200` 且 `attempt < maxRetries-1` 时返回 `(nil, nil)`，`RetryWithBackoff` 认为 err==nil 返回成功，结果为 nil。上层 `EmbedQuery`/`EmbedDocuments` 会因 `len(embeddings)==0` 报 `RETRIEVAL_EMBEDDING_RESPONSE_INVALID`，而非正确的 `RETRIEVAL_EMBEDDING_REQUEST_CALL_FAILED`。
- **影响**: 错误类型不一致，且跳过了 warning 日志。

### S-07. APIEmbedding 429 (Rate Limit) 不可重试

- **模块**: 4.22 APIEmbedding
- **文件**: `retrieval/embedding/api.go:287-292`
- **Python 参考**: Python 中 `requests.exceptions.RequestException` 捕获网络层错误，429 在 `raise_for_status()` 时抛出 `HTTPError`，属于 `RequestException` 子类，可重试
- **问题**: HTTP 429 是 4xx 状态码，按当前逻辑被 `ValidateError` 标记为不可重试。但 429 通常应该重试（服务端告知稍后重试），Python 版本 429 是可重试的。
- **影响**: 遇到 API 限流时无法自动恢复。

### S-08. ChatReranker `parseResponse` 中 BuildError 被丢弃

- **模块**: 4.24 ChatReranker
- **文件**: `retrieval/reranker/chat.go:188-195`
- **Python 参考**: `chat_reranker.py:89-93`
- **问题**: Python 在 logprobs 不支持时 `raise build_error`，Go 版本 `exception.BuildError` 的返回值被 `_` 丢弃，静默返回 `map[string]float64{firstDocID(docIDs): 0.0}`。调用方无法区分"服务不支持"和"分数确实为 0"。
- **影响**: 严重的逻辑偏差，错误被隐藏。

### S-09. ChatReranker `assembleParams` 传入空 query

- **模块**: 4.24 ChatReranker
- **文件**: `retrieval/reranker/chat.go:122`
- **Python 参考**: `chat_reranker.py:122`
- **问题**: Go 的 `assembleParams` 调用 `c.requestParams("", texts, 1, opt)`，query 参数为空字符串。Python 传入原始 query。Go 的 ChatReranker 发出的请求中 query 部分永远为空，用户查询内容被丢弃。
- **影响**: ChatReranker 完全无法正常工作。

### S-10. MultimodalDocument `DashscopeInput` 使用 panic 而非返回 error

- **模块**: 4.18 retrieval/common
- **文件**: `retrieval/common/document.go:128-131, 142-145, 149-153`
- **Python 参考**: Python 使用 `_raise_validation_error_with_info` 抛出 `ValidationError`
- **问题**: 多处使用 `panic` 处理验证错误（重复模态字段、base64 视频、不支持的模态），不符合 Go 惯用错误处理模式。调用方如果没有 `recover`，程序会崩溃。
- **影响**: 运行时 panic 可能导致整个进程崩溃。

### S-11. S3 客户端缺失 AWS 校验和环境变量

- **模块**: 4.27 Object Store
- **文件**: `retrieval/store/object/s3/s3.go:64-107`
- **Python 参考**: `aioboto_storage_client.py:32-33`
- **问题**: Python 在 `__init__` 中设置 `AWS_REQUEST_CHECKSUM_CALCULATION=WHEN_REQUIRED` 和 `AWS_RESPONSE_CHECKSUM_VALIDATION=WHEN_REQUIRED`，兼容新版 SDK 校验和计算行为变更。Go 端缺失此设置，与新版 S3 兼容服务交互时可能 checksum 校验失败。
- **影响**: 与 OBS/新版 S3 服务交互可能失败。

### S-12. Query Builder Chroma logical filter 错误信息不准确

- **模块**: 4.28 Query Builder
- **文件**: `foundation/store/query/chroma_query_func.go:130-135`
- **Python 参考**: `chroma_query_func.py:107-154`
- **问题**: Go 端先检查 `Right==nil` 直接报 `"and operator requires both left and right operands"`，但 Python 对 `not` 操作符报 `"Unsupported logical operator: not"`。Go 对 `not` 操作符（`Right==nil`）报了不正确的错误信息。
- **影响**: 错误提示误导，调试困难。

---

## 二、一般问题（35 项）

### G-01. OpenAIEmbedding/DashscopeEmbedding `Dimension()` 缺少并发保护

- **模块**: 4.20/4.21 Embedding
- **文件**: `retrieval/embedding/openai.go:243-260`, `dashscope.go:214-231`
- **问题**: `Dimension()` 直接读写 dimension 字段，无互斥锁。对比 `APIEmbedding.Dimension()` 使用了 `sync.Mutex`。并发调用可能触发多次探测请求或数据竞争。

### G-02. OpenAIEmbedding 缺少 base64 embedding 响应处理

- **模块**: 4.20 OpenAIEmbedding
- **文件**: `retrieval/embedding/openai.go:317-350`
- **Python 参考**: `openai_embedding.py:94-128`
- **问题**: `parseOpenAIResponse` 完全没有 base64 解码逻辑。Python 明确支持 `encoding_format=base64`。当用户请求 base64 格式时可能解析错误。

### G-03. VLLMEmbedding 重试缺少 isRetryable 判断

- **模块**: 4.22 VLLMEmbedding
- **文件**: `retrieval/embedding/vllm.go:163-194`
- **问题**: `retryVLLMWithBackoff` 对所有错误都重试，没有判断是否可重试。不可恢复的错误（如认证失败）也会重试，浪费请求。

### G-04. DashScope EmbedQuery input 格式与 Python 可能不一致

- **模块**: 4.21 DashScopeEmbedding
- **文件**: `retrieval/embedding/dashscope.go:139`
- **问题**: Go 将文本包装为 `[]map[string]any{{"text": text}}`，Python 纯文本传入字符串而非字典。需确认 DashScope API 是否同时接受两种格式。

### G-05. EmbedQuery 接口签名缺少 opts 参数

- **模块**: 4.19 Embedding 接口
- **文件**: `foundation/store/embedding/base.go:22-23`
- **问题**: Go 的 `EmbedQuery` 不支持可选参数，Python `embed_query(self, text, **kwargs)` 可透传 `dimensions`、`encoding_format` 等参数。

### G-06. DashScope 缺少 extra_headers 支持

- **模块**: 4.21 DashScopeEmbedding
- **文件**: `retrieval/embedding/dashscope.go:22-106`
- **问题**: Python 构造函数接收 `extra_headers` 参数，Go 完全没有此支持。

### G-07. VLLMEmbedding `parseMultimodalInput` 是死代码

- **模块**: 4.22 VLLMEmbedding
- **文件**: `retrieval/embedding/vllm.go:196-222`
- **问题**: 该函数只在测试中被调用，生产代码未使用。`EmbedMultimodal` 内联构造了 messages。

### G-08. RerankerConfig timeout 校验允许 0 值

- **模块**: 4.23 Reranker 接口
- **文件**: `foundation/store/reranker/base.go:130-133`
- **问题**: Python `timeout: float = Field(default=10, gt=0)` 要求严格大于 0，Go `if config.Timeout < 0` 允许 0 值。timeout=0 时 HTTP Client 无超时。

### G-09. StandardReranker 缺少 removesuffix("/") 处理

- **模块**: 4.24 StandardReranker
- **文件**: `retrieval/reranker/standard.go:84`
- **问题**: Python 先 removesuffix("/") 再 removesuffix(end_point)，Go 只 removesuffix(end_point)。带尾部斜杠的 api_base 会拼接出双斜杠 URL。

### G-10. validateStandardConfig 已定义但从未调用

- **模块**: 4.24 StandardReranker
- **文件**: `retrieval/reranker/standard.go:238-250`
- **问题**: `validateStandardConfig` 函数存在但未被调用，无效输入不会返回错误。

### G-11. StandardReranker assembleParams 中 default 分支不报错

- **模块**: 4.24 StandardReranker
- **文件**: `retrieval/reranker/standard.go:157-166`
- **问题**: 不支持的文档类型被静默忽略，Python 会 `raise build_error`。

### G-12. ChatReranker requestParams 未合并 ExtraParams

- **模块**: 4.24 ChatReranker
- **文件**: `retrieval/reranker/chat.go:130-167`
- **问题**: `requestParams` 只合并了 `ExtraBody`，没有合并 `opt.ExtraParams`。对比 `RerankerBase.requestParams` 有 ExtraParams 合并逻辑。

### G-13. DashScopeReranker 不支持 MultimodalDocument 类型的 query

- **模块**: 4.25 DashScopeReranker
- **文件**: `retrieval/reranker/dashscope.go:222`
- **问题**: Python 中 query 参数可以是 `MultimodalDocument`，Go 的 `assembleParams` 方法的 query 参数类型是 `string`。

### G-14. DashScopeReranker ExtraParams 合并到顶层 params 而非 parameters 内

- **模块**: 4.25 DashScopeReranker
- **文件**: `retrieval/reranker/dashscope.go:306-311`
- **问题**: Python 将额外 kwargs 合并到 `parameters` 字典内（嵌套），Go 将 ExtraParams 合并到顶层 params。

### G-15. DashScopeReranker 同样缺少 removesuffix("/") 处理

- **模块**: 4.25 DashScopeReranker
- **文件**: `retrieval/reranker/dashscope.go:84`
- **问题**: 同 G-09。

### G-16. Graph Store 搜索 limit 缺少 `min(k*3, 20)` 优化

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:212-233`
- **问题**: Python 每个 `AnnSearchRequest` 的 `limit=min(k*3, 20)`，Go 直接使用 `k`。混合搜索召回率不足。

### G-17. Graph Store `AttachEmbedder` 缺少维度校验

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus.go:220-232`
- **问题**: Python 校验 `config.embed_dim != embedder.dimension`，Go 完全缺少此校验。

### G-18. Graph Store `Rebuild` 逻辑与 Python 不一致

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus.go:61-87`
- **问题**: Python 删除数据库再重建，Go 只删除集合。另外 Python 先尝试 `load_collection`，加载失败才 rebuild。

### G-19. Graph Store `Refresh` 缺少 compact 操作

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus.go:89-103`
- **问题**: Python 有 `flush` + 可选 `compact`，Go 只做了 `flush`。

### G-20. Graph Store Schema 中 offset_since/offset_until 类型不一致

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/schema.go:172-179`
- **问题**: Python 使用 `DataType.INT8`，Go 使用 `entity.FieldTypeInt32`。

### G-21. Graph Store `searchAll` 中搜索失败的集合未记录到 results

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:81-101`
- **问题**: Python 即使搜索失败也初始化 `output_dict[col] = []`，Go 不包含该集合的 key。

### G-22. Graph Store `inferGraphColumn` 不支持 JSON 类型

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_writer.go:412-494`
- **问题**: `metadata` 和 `attributes` 字段类型是 JSON，但 `inferGraphColumn` 不支持 `map[string]any` 类型的推断，JSON 字段被静默跳过。

### G-23. Graph Store `filterByScore` 使用 content 作为 key 可能冲突

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:436-468`
- **问题**: 使用 `content` 字符串作为 scoreMap 的 key，多个相同 content 的文档只会匹配到第一个。Python 使用 `uuid` 作为唯一标识。

### G-24. Graph Store `Query` 在 IDs 和 Expr 都为空时不报错

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus.go:147-187`
- **问题**: Python 在 `expr` 和 `ids` 都为 None 且没有 `limit` 时报错，Go 允许空查询。

### G-25. SimpleMemoryIndex `AddField` 对 Text 字段赋值与 Python 不一致

- **模块**: 4.18 retrieval/common
- **文件**: `retrieval/common/document.go:77-79`
- **问题**: Go 在 `AddField(ModalityText, ...)` 时更新 `d.Text`，Python 的 `add_field` 不会更新 `text` 字段（始终保持默认空字符串）。

### G-26. SimpleMemoryIndex `AddField` 不支持自定义 data_id

- **模块**: 4.18 retrieval/common
- **文件**: `retrieval/common/document.go:70-81`
- **问题**: Python `add_field` 接受 `data_id` 参数（长度 ≤ 32），Go 不支持外部传入自定义 `data_id`。

### G-27. SimpleMemoryIndex 向量删除失败静默忽略

- **模块**: 4.18 SimpleMemoryIndex
- **文件**: `foundation/store/index/simple.go:366-369`
- **问题**: 向量删除失败只记录日志继续，Python 会直接抛出异常。可能导致 KV 已删除但向量残留。

### G-28. SimpleMemoryIndex DeleteCollection 失败静默忽略

- **模块**: 4.18 SimpleMemoryIndex
- **文件**: `foundation/store/index/simple.go:392-395, 435-437, 463-465`
- **问题**: `DeleteCollection` 失败记录日志并 continue，Python 会直接抛出异常。

### G-29. S3 客户端签名配置未完全对齐 Python

- **模块**: 4.27 Object Store
- **文件**: `retrieval/store/object/s3/s3.go:96-100`
- **问题**: `PayloadSigningEnabled` 和 `SignatureVersion` 字段定义但从未使用（死代码），`payload_signing_enabled=False` 未实现。

### G-30. S3 `UploadFile` 使用 PutObject 而非分段上传

- **模块**: 4.27 Object Store
- **文件**: `retrieval/store/object/s3/s3.go:168-214`
- **问题**: Python 使用 `upload_fileobj`（自动分段上传），Go 使用 `PutObject`（单次上传）。大文件可能超时或内存问题。

### G-31. S3 `UploadFile` 中 `file.Stat()` 错误被忽略

- **模块**: 4.27 Object Store
- **文件**: `retrieval/store/object/s3/s3.go:186`
- **问题**: `fileInfo, _ := file.Stat()` 忽略错误，`fileInfo` 为 nil 时 `fileInfo.Size()` 会 panic。

### G-32. Query Builder `FilterUser` 参数类型为 any 缺乏类型安全

- **模块**: 4.28 Query Builder
- **文件**: `foundation/store/query/factory.go:96-115`
- **问题**: Python 类型标注 `str | List[str]`，Go 使用 `any`。传入非预期类型不会编译报错。

### G-33. Query Builder `milvusArithmeticFilter` 格式 `10>` 紧挨

- **模块**: 4.28 Query Builder
- **文件**: `foundation/store/query/milvus_query_func.go:77-82`
- **问题**: 输出 `price + 10> 100`（`10>` 紧挨），可能是 Python 端 bug 被复刻。

### G-34. Graph Store `searchAll` 缺少并发搜索

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:81-101`
- **问题**: Python 使用 `asyncio.create_task` + `as_completed` 并发搜索三集合，Go 使用串行。性能影响较大。

### G-35. Graph Store `delete` 在 IDs 和 Expr 都为空时静默返回 nil

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_writer.go:90-95`
- **问题**: Python 的 `delete` 在 ids 和 expr 都为 None 时会抛出错误，Go 静默返回 nil。

---

## 三、提示问题（31 项）

### T-01. APIEmbedding `Dimension()` 锁保护范围不完整

- **模块**: 4.22 APIEmbedding
- **文件**: `retrieval/embedding/api.go:193-216`
- **问题**: 检查-探测-写入流程不是原子的，可能触发重复探测请求。

### T-02. APIEmbedding `getEmbeddings` 使用 `interface{}` 类型

- **模块**: 4.22 APIEmbedding
- **文件**: `retrieval/embedding/api.go:233`
- **问题**: `input` 参数使用 `interface{}`，应为 `any` 或使用具体类型联合。

### T-03. `ParseEmbeddingResponse` 中 `embedding` 字段缺失不报错

- **模块**: 4.22 Embedding 通用
- **文件**: `retrieval/embedding/common.go:254-274`
- **问题**: 如果 `item.Embedding` 为空 JSON，`json.Unmarshal` 返回空 slice，不报错。部分 item 缺失 embedding 不会报错。

### T-04. OpenAI `Dimension()` 使用 `context.Background()` 可能意外阻塞

- **模块**: 4.20 OpenAIEmbedding
- **文件**: `retrieval/embedding/openai.go:248`
- **问题**: 远程服务不可达时调用会一直阻塞直到超时（默认 60s），无法取消。

### T-05. OpenAI 重试日志缺少 model_name 字段

- **模块**: 4.20 OpenAIEmbedding
- **文件**: `retrieval/embedding/openai.go:287-294`
- **问题**: 按规则 3.5，Python 日志中的变量必须在 Go 日志中以结构化字段等价体现。缺少 `model_name` 字段。

### T-06. DashScope response 缺少 request_id 字段

- **模块**: 4.21 DashScopeEmbedding
- **文件**: `retrieval/embedding/dashscope.go:50-60`
- **问题**: 排查问题时 `request_id` 是关键信息，建议添加。

### T-07. EmbedOption 使用值类型而非函数选项模式

- **模块**: 4.19 Embedding 接口
- **文件**: `foundation/store/embedding/base.go:35-40`
- **问题**: 与 retrieval/embedding 包内函数选项风格不一致。

### T-08. NewEmbeddingHTTPClient 不支持自定义超时

- **模块**: 4.22 Embedding 通用
- **文件**: `retrieval/embedding/common.go:368-406`
- **问题**: HTTP Client 超时硬编码为 60s，无法被调用方覆盖。

### T-09. StandardReranker 缺少多模态文档 warning 日志

- **模块**: 4.24 StandardReranker
- **文件**: `retrieval/reranker/standard.go`
- **Python 参考**: `standard_reranker.py:110-113`
- **问题**: Python 有 `logger.warning("Reranker received a multimodal reranking request, not supported")` 日志，Go 缺失。违反规则 3。

### T-10. ChatReranker logprobs 不支持时无错误日志

- **模块**: 4.24 ChatReranker
- **文件**: `retrieval/reranker/chat.go:190-193`
- **问题**: `BuildError` 被丢弃，既没有返回错误也没有记录日志。违反规则 3。

### T-11. Reranker 枚举区块标题写错为 "枚"

- **模块**: 4.23 Reranker
- **文件**: `retrieval/reranker/reranker_base.go:31`
- **问题**: 应为 `// ──────────────────────────── 枚举 ────────────────────────────`，违反规范 2。

### T-12. ChatReranker 注释乱码

- **模块**: 4.24 ChatReranker
- **文件**: `retrieval/reranker/chat.go:244`
- **问题**: 注释 `doRerank 执行异步重排序` 中有乱码字符。

### T-13. Graph Store Schema 缺少 enable_analyzer 和 enable_match 配置

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/schema.go:90-125`
- **问题**: Python 中 `name` 和 `obj_type` 字段配置了全文搜索参数，Go 完全缺少。

### T-14. Graph Store Schema 缺少 BM25 bm25_config 参数

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/schema.go:246`
- **问题**: Python BM25 索引包含 `bm25_config` 参数（`b` 和 `k1`），Go 硬编码 `dropRatio=0.2`。

### T-15. Graph Store NewGraphConfig 缺少 embedding_model 字段

- **模块**: 4.26 Graph Store
- **文件**: `foundation/store/graph/config.go:13-38`
- **问题**: Python 的 `GraphConfig` 有 `embedding_model` 字段。

### T-16. Graph Store `EnsureUniqueUUIDs` 逻辑与 Python 差异大

- **模块**: 4.26 Graph Store
- **文件**: `foundation/store/graph/utils.go:72-93`
- **问题**: Python 循环检测重复并重新生成 UUID 直到没有重复，Go 只过滤已存在的 UUID。

### T-17. Graph Store `_add_data` 缺少耗时日志

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_writer.go:145-204`
- **问题**: Python 记录了操作耗时，Go 完全没有。违反规则 3。

### T-18. Graph Store `close` 日志级别应为 Error

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus.go:106-120`
- **问题**: Python 用 `logger.error`，Go 用 `logger.Warn`。

### T-19. Graph Store `searchAll` 搜索失败日志缺少查询文本

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_searcher.go:89`
- **问题**: 日志只有 `collection`，缺少 `query` 文本。

### T-20. Graph Store `Rebuild` 创建集合失败时不回滚

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus.go:61-87`
- **问题**: 先删除所有集合再创建新集合，如果创建失败旧数据已丢失。

### T-21. S3 `CreateBucket` 在 location 为空时仍传递 CreateBucketConfiguration

- **模块**: 4.27 Object Store
- **文件**: `retrieval/store/object/s3/s3.go:113-138`
- **问题**: AWS S3 在 `us-east-1` 区域创建桶时不应传递 `CreateBucketConfiguration`。

### T-22. S3 ListObjects 缺少逐对象日志

- **模块**: 4.27 Object Store
- **文件**: `retrieval/store/object/s3/s3.go:310-351`
- **问题**: Python 对每个列出的对象记录 JSON 日志，Go 只记录汇总。违反规则 3。

### T-23. Object Store 日志组件应为 ComponentAgentCore

- **模块**: 4.27 Object Store
- **文件**: `foundation/store/object/base.go:74`, `retrieval/store/object/s3/s3.go:48`
- **问题**: agentcore 下的包应使用 `logger.ComponentAgentCore`，当前使用 `ComponentCommon`。

### T-24. Query Builder Milvus 不支持 xor 但提供了 Xor() 函数

- **模块**: 4.28 Query Builder
- **文件**: `foundation/store/query/milvus_query_func.go:132-160`
- **问题**: 用户可能误以为 Milvus 支持 xor。

### T-25. Query Builder `WildcardMatch` 缺少 operator 参数

- **模块**: 4.28 Query Builder
- **文件**: `foundation/store/query/factory.go:60-62`
- **问题**: Python 端 `wildcard_match` 有第三个参数 `operator`，Go 硬编码 `"wildcard"`。

### T-26. Query Builder `init()` 中注册错误被静默忽略

- **模块**: 4.28 Query Builder
- **文件**: `foundation/store/query/chroma_query_func.go:226`, `milvus_query_func.go:222`
- **问题**: `_ = RegisterDatabaseQueryLanguage(...)` 忽略了注册错误。

### T-27. MultimodalDocument `Content()` 中 audio 匹配失败静默跳过

- **模块**: 4.18 retrieval/common
- **文件**: `retrieval/common/document.go:101-109`
- **问题**: Python 匹配失败会报 AttributeError，Go 选择 `continue` 静默跳过，隐藏数据格式错误。

### T-28. MultimodalDocument `Content()` 每次调用编译正则

- **模块**: 4.18 retrieval/common
- **文件**: `retrieval/common/document.go:101`
- **问题**: 应提取为包级变量避免重复编译。

### T-29. MultimodalDocument 缺少 content_cache 和 dashscope_cache

- **模块**: 4.18 retrieval/common
- **文件**: `retrieval/common/document.go:86-116`
- **问题**: Python 有缓存机制避免重复计算，Go 每次重新计算。

### T-30. SimpleMemoryIndex Search 返回 nil vs Python 返回 []

- **模块**: 4.18 SimpleMemoryIndex
- **文件**: `foundation/store/index/simple.go:194-199`
- **问题**: Go 返回 `(nil, nil)`，Python 返回 `[]`。语义上都是无结果但类型不同。

### T-31. Graph Store 批量写入失败日志缺少 batch_size

- **模块**: 4.26 Graph Store
- **文件**: `retrieval/store/graph/milvus/milvus_writer.go:176-192`
- **问题**: Python 记录 `batching with size of %d`，Go 只记录"逐条写入"。

---

## 四、修复优先级建议

### P0 — 必须立即修复（影响核心功能正确性）

| 编号 | 模块 | 描述 | 修复建议 |
|------|------|------|---------|
| S-01 | Graph Store | BM25 搜索用错输入 | 改用查询文本代替 vectors |
| S-02 | Graph Store | BFS 逻辑与 Python 不一致 | 对齐 Python 的 Entity/Relation 双向扩展 + bfs_k 裁剪 |
| S-03 | Graph Store | combinedRerank 缺少关系增强 | 添加关系信息增强实体排序逻辑 |
| S-08 | ChatReranker | BuildError 被丢弃 | 返回 error 而非静默返回 0.0 |
| S-09 | ChatReranker | query 被丢弃为空 | 传入实际 query 而非空字符串 |
| S-10 | MultimodalDoc | panic 替代 error | 改为返回 error |

### P1 — 尽快修复（影响可靠性和兼容性）

| 编号 | 模块 | 描述 |
|------|------|------|
| S-04 | Graph Store | Relation EmbedTasks 与 Schema 不匹配 |
| S-05 | DashScope Embedding | 5xx 错误不可重试 |
| S-06 | DashScope Embedding | 非 200 返回 (nil, nil) |
| S-07 | APIEmbedding | 429 不可重试 |
| S-11 | Object Store | 缺失 AWS 校验和环境变量 |
| S-12 | Query Builder | Chroma logical filter 错误信息不准确 |
| G-01 | Embedding | Dimension() 并发保护 |
| G-02 | OpenAI Embedding | base64 响应未处理 |
| G-03 | VLLM Embedding | 重试缺少 isRetryable |
| G-31 | S3 | file.Stat() panic 风险 |

### P2 — 计划修复（功能对齐和代码质量）

其余一般问题和提示问题归入此级别，按模块分批修复。

---

## 五、整体评价

### 功能符合度

- **4.18 SimpleMemoryIndex**: 基本符合 Python 实现，但 MultimodalDocument 使用 panic 不符合 Go 惯例
- **4.19-4.22 Embedding**: 接口层对齐度约 85%，具体实现对齐度约 80%。DashScope 客户端的重试逻辑与 Python 差距最大
- **4.23-4.25 Reranker**: 接口层对齐度约 90%，StandardReranker 对齐度约 80%，ChatReranker 存在致命 bug（query 丢失），DashScopeReranker 约 75%
- **4.26 Graph Store**: 整体对齐度约 60%，搜索核心逻辑（BFS + rerank + BM25）与 Python 差距最大
- **4.27 Object Store**: 对齐度约 85%，S3 客户端核心方法基本正确，缺少 checksum 环境变量
- **4.28 Query Builder**: 对齐度约 95%，表达式类型和转换函数全面对齐

### 最大风险

1. **Graph Store 搜索质量**：BM25 输入错误 + BFS 逻辑不完整 + combinedRerank 缺少关系增强，三者叠加导致图搜索结果与 Python 版本差距极大
2. **ChatReranker 完全不可用**：query 被丢弃为空字符串 + logprobs 不支持时静默返回 0.0
3. **DashScope 客户端重试机制**：5xx 和 429 都不可重试，生产环境可靠性风险高
