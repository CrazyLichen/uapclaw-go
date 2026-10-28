# 13.25 AgenticRetriever 设计文档

> 对应 Python：`openjiuwen/core/retrieval/retriever/agentic_retriever.py`（548 行，含两个提示词常量）
> Python 测试：`tests/unit_tests/core/retrieval/retriever/test_agentic_retriever.py`（23 例）
> 上游依赖：13.19 Retriever 接口 / 13.22 TripleMemory / 13.23 GraphRetriever / 13.24 LLM 调用先例 / utils.RRFFusion+Deduplicate
> 下游消费方：13.27 SimpleKnowledgeBase（`retrieval_config.agentic=true` 分支构造）、13.28 GraphKnowledgeBase（同款分支）

## 0. 在 agent 会话流程中的位置与作用

```
Agent 会话执行（workflow 编排）
 └─ 8.25 KnowledgeRetrievalComp（知识检索组件，领域八）
     └─ retrieve_multi_kb_with_source()（模块级多 KB 聚合，13.27）
         ├─ SimpleKnowledgeBase.Retrieve()   ──┐
         └─ GraphKnowledgeBase.Retrieve()     ──┤ 配置 retrieval_config.agentic=true 时：
                                                │ 底层 retriever 被包成 AgenticRetriever
                  ┌─────────────────────────────┘
                  ▼
          13.25 AgenticRetriever.Retrieve()   ← 本节：多跳 agentic 检索外壳
              ├─ 被包装的底层检索器（任意 Retriever）
              │   ├─ GraphRetriever（13.23）→ 走图专用路径（图扩展+三元组链接）
              │   └─ 其他 Retriever（Vector/Sparse/Hybrid）→ 走通用路径
              ├─ TripleMemory（13.22）：跨轮累积 LLM 抽取的三元组事实，跨轮去重
              ├─ LLM 三件套：
              │   ├─ _read：从检索到的 passages 抽知识三元组（GeAR 读步骤）
              │   ├─ _rewrite：判断事实是否足以作答，不足则生成下一个问题
              │   └─ 循环：检索 → 读 → 改写，最多 max_iter 轮（默认 2）
              └─ RRF 融合多轮结果（utils.RRFFusion 已有）
```

**作用**：在任意底层检索器之上加 LLM 多跳推理循环——每轮检索后用 LLM 抽事实（三元组）存入 TripleMemory，再判断"事实够不够回答问题"，不够就生成新问题再检索，最后多轮结果 RRF 融合。底层是 GraphRetriever 时额外启用图扩展与三元组链接（论文 *Millions of GeAR-s: Extending GraphRAG to Millions of Documents* 的 GeAR 策略）。

**与 13.24 QueryRewriter 的区别**：13.24 是检索前的单发查询改写（指代消解，一次调用）；13.25 是检索中/后的多跳迭代改写（事实充分性判断驱动的循环，最多 max_iter 轮）。

## 1. 架构与数据流

### 1.1 包结构（方案 A：同包单文件）

```
internal/agentcore/retrieval/retriever/
├── agentic_retriever.go       # 本节新增：AgenticRetriever 全量逻辑（~500 行含私有 pyStr 复刻）
└── agentic_retriever_test.go  # 23 例 1:1 映射 + Go 增强
```

同包部署：`isGraph` 用类型断言 `*GraphRetriever`（零接口成本，isinstance 同构）；`GraphExpansion`/`GetRetrieverForMode` 同包直调（签名已就绪）。与 Python 文件结构 1:1。base.go 增加 `GraphExpansion *bool` 字段，doc.go 更新文件目录。

### 1.2 图路径数据流（_retrieve_with_graph）

```
每轮 turn=1..maxIter：
  q = queries 末位
  ① chunkRetriever = g.GetRetrieverForMode(mode, isChunk=true)  ← err 硬传播
  ② chunkResults = chunkRetriever.Retrieve(q, topK, threshold, mode, opts...)  ← err 硬传播
  ③ [graphExpansion 开启时]：
       proximal = _read(q, chunkResults, nil)        ← LLM 软失败返回 []
       linked   = _linkTriples(proximal, mode, opts)  ← gather 并发、失败聚合 Warn
       chunkResults = GraphRetriever.GraphExpansion(q, chunkResults, linked, &topK, mode, opts...)
  ④ triples = _read(query【原查询非 q】, chunkResults, memory.Memory)
  ⑤ memory.BatchExtendMemory(triples)
  ⑥ historyResults 追加
  ⑦ turn < maxIter 时：rewritten = _rewrite(query, memory.TriplesStr, queries)
     rewritten 为空（足够/LLM 失败/解析失败）→ break；否则 queries 追加
末轮后：ret = _linkPassages(memory.Memory, mode, opts)
返回 RRFFusion(append(ret, historyResults...))[:topK]
```

注意 ④：主 `_read` 传原始 query 而非当前轮 q（Python L237 `self._read(query, ...)` 字面如此，与 ③ 的 proximal `_read(q, ...)` 不同）——字面复刻，偏差归档 §8。

### 1.3 通用路径数据流（_retrieve_generic）

同图路径去掉 ③（无图扩展）与末轮 `_linkPassages`，返回 `RRFFusion(historyResults)[:topK]`。参考注释：*Millions of GeAR-s: Extending GraphRAG to Millions of Documents*（arXiv:2507.17399）。

### 1.4 软失败语义（贯穿全文件）

| 环节 | Python 行为 | Go 对应 |
|---|---|---|
| LLM 调用失败 | Warn + None | Warn 日志 + 软降级 |
| _read 解析失败/非 list | Warn + `[]` | 同 |
| _rewrite 解析失败/非 dict | Warn + None | 同 |
| _link 三元组/段落部分失败 | gather 完成后聚合 Warn 计数 | 同（13.17 方案 A 无信号量版） |
| top_k 非法 / retriever/llm_client 缺失 / 非图检索器调图方法 | **硬错误** | 155210/155208/155209/155211 |

## 2. 结构体、构造与并发

### 2.1 结构体

```go
type AgenticRetriever struct {
    retriever   Retriever                    // 底层检索器（nil→155208）
    isGraph     bool                         // 构造期类型断言 *GraphRetriever
    defaultMode RetrievalMode                // IndexTypeCarrier 探测推导
    llm         model_clients.BaseModelClient // LLM 客户端（nil→155209）
    maxIter     int                          // 默认 2；非法值 Warn 回落 2
}
```

导出访问器：`Retriever_()` / `LLM()` / `MaxIter()` / `IsGraphRetriever()` / `DefaultMode()`（对齐 Python 属性 + 测试断言字段）。编译期断言 `var _ Retriever = (*AgenticRetriever)(nil)`。

### 2.2 构造

`NewAgenticRetriever(retriever Retriever, llm model_clients.BaseModelClient, opts ...AgenticRetrieverOption)`：

- 参数式必填：nil → 155208 `"retriever is required for AgenticRetriever"` / 155209 `"llm_client is required for AgenticRetriever"`（error_msg 逐字符）
- `WithAgenticMaxIter(n int)`：非法值（≤0）不报错，Warn `"Invalid max_iter provided, falling back to default of 2"`（含 max_iter 字段）回落 2。Python `isinstance(max_iter, int)` 的非 int 场景 Go 编译期消解，归档
- Python 构造 `**kwargs` 透传（SimpleKB/GraphKB 调用点实际传空）——归档"零额外 kwargs 透传"，不加 Go 字段

### 2.3 defaultMode 推导（getattr 链字面复刻）

```go
indexType := ""
if c, ok := retriever.(IndexTypeCarrier); ok { indexType = c.IndexType() }
if indexType == "" { indexType = "hybrid" }   // None or "hybrid"
switch indexType {
case "vector": → RetrievalModeVector
case "bm25":   → RetrievalModeSparse
default:       → RetrievalModeHybrid
}
```

GraphRetriever 天然实现 IndexTypeCarrier（13.23）；未实现接口的其他 Retriever → 空串 → hybrid（对齐 `getattr(x, "index_type", None) or "hybrid"`）。GraphRetriever 零值 indexType=""（Python None）→ hybrid 同构。

### 2.4 接口实现

- `Retrieve(ctx, query, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption)`：topK ≤ 0 → 155210 `"top_k is invalid, must be a positive integer"`（`top_k=None` 编译期消解归档）；`mode==""` → defaultMode（13.19 零值回落偏差延续）；图/通用路径分派；opts 整体透传底层
- `BatchRetrieve(ctx, queries, topK, opts...)`：裸 errgroup 全启动（13.19 惯例，无 WithContext 不 cancel——gather 语义）；Python 只透传 top_k 不透传 score_threshold/mode，Go 接口签名天然不含，1:1
- `RetrieveSearchResults`：Python 未覆写继承基类空方法返回 None → Go 返回 `nil, nil`（13.19 base.go 接口注释预留语义）
- `Close`：Python hasattr+iscoroutinefunction 双探测 → Go 直接 `retriever.Close()`（编译期消解归档）

### 2.5 RetrieveOptions 新增字段（base.go）

```go
// GraphExpansion 图路径是否启用图扩展（本节真实消费）。
// nil → true（kwargs.pop("graph_expansion", True)）。
// 指针类型：GraphKB（13.28）传 RetrievalConfig.graph_expansion 默认 False，
// bool 零值无法区分"未设"与"显式 false"。
GraphExpansion *bool
```

`WithRetrieveGraphExpansion(v bool) RetrieveOption`。Python `kwargs.pop` 后底层收不到该键 → Go 值传 RetrieveOptions、底层不读该字段即等价（归档）。`graph_expansion` kwargs 的真实生产方是 GraphKB（`retrieval_config.graph_expansion` 默认 False）——与 Agentic 层默认 True 的不一致是 Python 真实行为，字面复刻。

### 2.6 并发语义

- `_linkTriples`/`_linkPassages`：裸 gather（无信号量）——预分配 `results[i]` 槽位、各 goroutine 写各槽、WaitGroup 收齐、三分类聚合（error→failed；空列表→failed+Warn"Empty or Unexpected result type"；非空→取 `[0]`）、聚合计数 Warn。复用 13.17 方案 A gather 骨架去 semaphore
- TripleMemory 单协程（13.22 决策 7 延续；迭代循环单协程消费，`_link*` 的 goroutine 不触碰 memory）

## 3. LLM 交互层

### 3.1 调用形态

每次 `llm.Invoke(ctx, NewMessagesParam(单 user 消息), model_clients.WithInvokeTemperature(0.0))`（13.17/13.24 同款）。`resp.content if hasattr(...) else str(resp)` 的 hasattr 分支编译期消解（归档）——Go `AssistantMessage.Content.Text()`。

### 3.2 提示词（常量 + Replacer，Q2 拍板）

`readPrompt`/`rewritePrompt` 字符串常量**逐字节复制** Python `_READ_PROMPT`/`_REWRITE_PROMPT`（含 `_REWRITE_PROMPT` 内 `{{"sufficient"...}}` 字面双花括号）。填充用 `strings.NewReplacer("{docs}", ..., "{query}", ..., "{facts}", ...)` —— 13.17 buildPrompt 先例，Replacer 命名槽防花括号误伤。

### 3.3 _read

```
docs = "\n\n".join(str(p.text) for p in passages[:5] if p.text)   ← 跳过空文本+截前 5
facts_str：
  existing_facts 为 nil/空 → "None"
  否则 ", ".join(str(fact) for fact in existing_facts if fact)
  str(fact) = Python tuple repr：('s', 'p', 'o')——手拼单引号+逗号空格
```

解析链（软失败全 Warn）：
1. LLM 失败 → `[]`
2. `jsonrepair.RepairWithOptions` 一步取值（`repair_json(return_objects=True)` 同款，13.24 先例）
3. 根非 `[]any` → Warn `"Read response JSON is not a list"` + `[]`
4. 逐项：`isinstance(triple, list) and len(triple)==3` 过滤（非 list/长度≠3 丢弃）→ `tuple(map(str, triple))` 用 pyStr 逐元素转 string → `common.TripleFact`

**Q5 拍板**：pyStr 复用 13.17 语义（nil→"None"、json.Number/float64/int64 记法还原、bool→"True"/"False"、string 直通）。extractor 包 pyStr 是私有——**retriever 包内私有复刻 pyStr+pyFloatStr（~60 行）**，13.23 pyAnyStr 同款先例（读取侧/写入侧管线分立，不动 extractor 公开面）。

### 3.4 _rewrite

```
question_rewriting_history：
  len(history) > 1 → "Rewritten Question {i}: {q}" 逐行 \n join（history[1:] 起，i 从 1）
  否则 → "(No rewriting steps yet)"
填充 rewritePrompt：{query}/{triples}/{question_rewriting_history}
```

解析链（软失败）：
1. LLM 失败 → nil
2. jsonrepair → 根非 `map[string]any` → Warn `"Rewrite response JSON is not an object"` + nil
3. `sufficient = get("sufficient", False)`（truthy 判定）；`next_question = get("next_question")`
4. `sufficient or not next_question` → nil；否则返回 next_question
5. **类型收窄偏差（归档）**：next_question 必须 `.(string)` 成功才返回，非 string 视为 falsy → nil。Python 病态输入（非 str truthy dict）会进入 queries 循环，Go 收窄——合理性：该值只进强类型 API，Python 侧最终也只进 str 上下文

### 3.5 _link_triples 映射

```
triple_str = " ".join(map(str, triple))    ← pyStr join
tripleRetriever = g.GetRetrieverForMode(mode, isChunk=false)
gather: tripleRetriever.RetrieveSearchResults(triple_str, top_k=1, mode, opts...)
三分类聚合 → 取 [0] → Deduplicate(all, key=SearchResult.ID)
for result in deduped:
    metadata = result.metadata or {}       ← nil map 防御
    RetrievalResult{Text, Score, Metadata, DocID: metadata["doc_id"], ChunkID: metadata["chunk_id"]}
```

**类型收窄偏差（归档）**：`metadata.get("doc_id")` 装任意 JSON 值；Go RetrievalResult.DocID/ChunkID 是 string（13.22 定稿）——`s, _ := metadata["doc_id"].(string)` 非 string 落空串，不为此改回 any。

### 3.6 _link_passages

```
chunkRetriever = g.GetRetrieverForMode(mode, isChunk=true)
gather: chunkRetriever.Retrieve(triple_str, top_k=5, mode, opts...)
三分类聚合 → 整列表保留（不取 [0]）
```

返回 `[][]RetrievalResult`（成功项的列表），失败/空聚合计数 Warn。RRF 融合时 `append(ret, historyResults...)` 直接拼接（`List[List[RetrievalResult]]` 展平进 fusion）。

## 4. 日志映射（ComponentAgentCore）

| Python | Go（结构化字段） |
|---|---|
| `Invalid {max_iter=} provided, falling back to default of 2`（Warn） | `.Int("max_iter", n).Msg("Invalid max_iter provided, falling back to default of 2")` |
| `LLM invocation failed: {e}`（Warn） | `.Err(err).Msg("Agentic LLM invocation failed")` |
| `Rewrite response JSON is not an object: {response_json}` | `.Any("response", …).Msg("Rewrite response JSON is not an object")` |
| `Failed to parse rewrite response as JSON: {e}.` | `.Err(err).Msg("Failed to parse rewrite response as JSON")` |
| `Read response JSON is not a list: {response_json}` | `.Any("response", …).Msg("Read response JSON is not a list")` |
| `Failed to parse read response as JSON: {e}.` | `.Err(err).Msg("Failed to parse read response as JSON")` |
| `Empty or Unexpected result type during link triples {type(result)}` | `.Str("type", fmt.Sprintf("%T", r)).Msg("Empty or unexpected result type during link triples")` |
| `Triple linking failed for {n}/{total} triples.` | `.Int("failed", n).Int("total", total).Msg("Triple linking failed for triples")` |
| `Empty or Unexpected result type during link passages {type(result)}` | 同款 |
| `Link passages failed for {n}/{total} triples.` | 同款 |

文案英文（既定规则），变量进结构化字段，`[Agentic]` 前缀并入 Msg。

## 5. 错误码（本节从零消费转已消费）

| 码 | 常量 | 触发点 | error_msg |
|---|---|---|---|
| 155208 | StatusRetrievalRetrieverNotFound | 构造 retriever nil | `retriever is required for AgenticRetriever` |
| 155209 | StatusRetrievalRetrieverLlmClientNotFound | 构造 llm nil | `llm_client is required for AgenticRetriever` |
| 155210 | StatusRetrievalRetrieverTopKInvalid | Retrieve topK ≤ 0 | `top_k is invalid, must be a positive integer` |
| 155211 | StatusRetrievalRetrieverInvalid | _assert_graph_retriever（图路径内部方法被非图检索器调用） | `Underlying retriever must be a GraphRetriever to link passages` |

Go 侧四码已定义零消费（codes_context.go L141-153），本节全部激活。`_assert_graph_retriever` 在 Go 侧：`_retrieve_with_graph`/`_link_triples`/`_link_passages` 仅在 `isGraph==true` 分支可达，类型系统已保证——**保留防御断言**（归档：Python 三处 assert 语义上不可达分支，Go 同样写防御检查返回 155211，覆盖测试走不了就归档不可达）。

## 6. 测试设计

### 6.1 桩基建（Q6 拍板：链路可控桩走真 GraphExpansion）

- **agenticFakeRetriever**（agentic_retriever_test.go 新造，不动 13.23 现有桩）：可编程 `Retrieve` + `RetrieveSearchResults`（闭包注入）+ mu 保护调用记录（lastQuery/lastTopK/lastMode/lastThreshold/calls/rsrCalls）+ 可配置 `indexType` 字段（实现 IndexTypeCarrier，defaultMode 三态测试用；零值 "" → hybrid 路径）
- **图路径构造**：`NewGraphRetriever(WithGraphChunkRetriever(chunk), WithGraphTripleRetriever(triple))`——GetRetrieverForMode 固定优先返回桩；GraphExpansion 真方法在「proximal 空（LLM 返回 `[]`）→ linked 空 → fetchTriples 空链 → 返回原 chunks」路径行为确定，与 Python mock 效果等价；beam search 深路径归 13.23 的 48 测试，13.25 只测外壳编排
- **fakeAgenticLLM**：按 Prompt 内容分派（13.17 dispatchTripleClient 先例——`docs=` 特征识别 _read、`sufficient` 特征识别 _rewrite），响应队列 + mu 保护（BatchRetrieve 并发）
- **fakeRetriever**（通用路径）：复用 agenticFakeRetriever（isGraph=false 场景）

### 6.2 Python 23 例 1:1 映射

| Python 用例 | Go 用例 |
|---|---|
| test_init_success_with_graph_retriever | TestNewAgenticRetriever_图检索器构造 |
| test_init_success_with_base_retriever | TestNewAgenticRetriever_通用检索器构造 |
| test_init_with_defaults | TestNewAgenticRetriever_默认值 |
| test_init_with_invalid_max_iter | TestNewAgenticRetriever_非法maxIter回落 |
| test_init_without_retriever | TestNewAgenticRetriever_缺retriever |
| test_init_without_llm_client | TestNewAgenticRetriever_缺llm客户端 |
| test_default_mode_vector / _bm25 / _hybrid | TestNewAgenticRetriever_默认模式三态（桩实现 IndexTypeCarrier 三返回值） |
| test_retrieve_with_graph_single_iteration | TestRetrieve_图路径单轮 |
| test_retrieve_with_graph_multiple_iterations | TestRetrieve_图路径多轮（chunk 桩 calls==2 断言） |
| test_retrieve_generic_single_iteration | TestRetrieve_通用路径单轮 |
| test_retrieve_generic_multiple_iterations | TestRetrieve_通用路径多轮 |
| test_retrieve_generic_max_iterations | TestRetrieve_通用路径到顶（max_iter=2 边界） |
| test_retrieve_fusion_multiple_results | TestRetrieve_融合多结果 |
| test_retrieve_without_valid_top_k / _negative | TestRetrieve_topK无效（0 与 -1 两场景；Python None 场景编译期消解归档） |
| test_retrieve_with_custom_mode | TestRetrieve_模式透传（桩 lastMode 断言） |
| test_retrieve_with_score_threshold | TestRetrieve_阈值透传（桩 lastThreshold 断言） |
| test_batch_retrieve | TestBatchRetrieve（两 query 各自走完循环，LLM 桩按 prompt 分派解耦调度序） |
| test_close / test_close_sync_close / test_close_no_close_method | TestClose（Go 接口编译期消解三态，1 例断言委托） |

### 6.3 Go 增强测试

1. LLM 顺序响应注入（prompt 内容分派，与调度序解耦——13.17 假失败教训）
2. pyStr 边界：nil→"None"、json.Number/float64/int64 记法还原（13.17 用例形态）
3. facts_str 形态：tuple repr 单引号拼接 + "None" 兜底（`if fact` 对 Go [3]string 零值不过滤——Python 非空 tuple 恒 True，字面复刻归档）
4. 软失败链：LLM err→Warn 降级继续；_read 非 list；_rewrite 非 dict；sufficient=true；next_question 空串
5. _link_triples 部分失败聚合：rsr 桩混排 error/空列表/正常 → 聚合计数、ID 去重、metadata 映射（含非 string doc_id 收窄空串）
6. _link_passages 失败聚合：Retrieve 桩混排
7. graph_expansion 消费：nil（默认 true）/false 两路（false 时断言 GraphExpansion 未改变 chunks 内容）
8. max_iter=1 边界：无 _rewrite 调用（LLM 桩只收 _read 请求）
9. RRF 融合序：图路径 ret 在前+history 在后，text 去重保首见
10. 并发 -race：BatchRetrieve 双 query 并发 + _link gather 并发

### 6.4 覆盖率目标

≥ 85% 硬线，预期 92%+（LLM/循环/聚合分支密集，包基线 91.5%）。全量回归带 CI 同款构建标签（`CGO_ENABLED=1 -tags "sqlite_fts5 test"`）。

## 7. 实施步骤（writing-plans 细化前的骨架）

1. base.go 加 GraphExpansion 字段 + WithRetrieveGraphExpansion + 单测补充
2. agentic_retriever.go：常量（两提示词逐字节）→ 结构体 → 构造/选项 → 访问器 → Retrieve/图路径/通用路径 → _read/_rewrite/_linkTriples/_linkPassages → _llmCall → 私有 pyStr/pyFloatStr 复刻
3. agentic_retriever_test.go：桩基建 → 23 例映射 → 增强测试
4. doc.go 更新文件目录；IMPLEMENTATION_PLAN.md 状态同步
5. 全量回归 + -race + 覆盖率

## 8. 偏差归档

| # | 偏差 | 理由 |
|---|---|---|
| 1 | `mode==""` 等同未设回落 defaultMode | 13.19 既定偏差延续（Python 显式空串走 155200） |
| 2 | `_read` 主抽取传原始 query 非 q | 字面复刻 Python L237 真实行为（本设计明确保留） |
| 3 | top_k=None / isinstance(max_iter,int) 非法类型场景 | Go 强类型编译期消解 |
| 4 | hasattr(resp,"content") / hasattr(retriever,"close")+iscoroutinefunction 双探测 | Go 接口/方法编译期消解 |
| 5 | kwargs.pop("graph_expansion") 后底层收不到键 | Go 值传 RetrieveOptions 底层不读该字段，行为等价 |
| 6 | next_question 非 string 收窄为 nil | 病态输入，Python 侧该值最终只进 str 上下文；Go 强类型无法承载任意 truthy |
| 7 | metadata.get("doc_id")/("chunk_id") 任意值 → Go string 收窄（非 string 落空串） | RetrievalResult.DocID string 是 13.22 定稿，不为此改 any |
| 8 | `if fact` 过滤对 Go [3]string 零值不过滤 | Python 非空 tuple 恒 True；Go 定长数组无"空 tuple"概念 |
| 9 | pyStr 在 retriever 包私有复刻（~60 行） | extractor 包 pyStr 私有；读取侧/写入侧管线分立（13.23 pyAnyStr 先例） |
| 10 | _assert_graph_retriever 防御断言不可达 | Go 类型系统保证仅 isGraph 分支可达；保留防御检查+155211，不可达测试归档 |
| 11 | Python `**kwargs` 构造透传（SimpleKB/GraphKB 调用点传空） | 零额外 kwargs 透传，不加 Go 字段 |

## 9. 拍板决策记录

| # | 决策 | 拍板 |
|---|---|---|
| 1 | 图路径判定：同包类型断言 `*GraphRetriever` + IndexTypeCarrier 推导 defaultMode | 方案 A |
| 2 | 提示词承载：字符串常量 + strings.NewReplacer（13.17 先例） | 方案 A |
| 3 | 章节顺序：顺位实施 13.25（依赖全就绪，13.27/13.28 等接线） | 顺位 |
| 4 | pyStr 复用 13.17 语义，retriever 包私有复刻 | Q5 |
| 5 | 图路径测试：链路可控桩走真 GraphExpansion，深路径归 13.23 | Q6 |
