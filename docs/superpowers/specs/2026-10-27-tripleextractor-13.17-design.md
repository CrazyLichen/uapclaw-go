# 13.17 TripleExtractor 三元组抽取设计文档

- 日期：2026-10-27
- 计划条目：IMPLEMENTATION_PLAN.md 13.17（TripleExtractor）
- Python 源：`openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py + extractor/base.py + common/triple.py`（339+49+20 行）
- Python 测试基线：`tests/unit_tests/core/retrieval/indexing/processor/extractor/test_triple_extractor.py + test_base.py + tests/unit_tests/core/retrieval/common/test_triple.py`（187+64+65 行，23 用例）
- Go 目标：`internal/agentcore/retrieval/common/triple.go`（新增）+ `internal/agentcore/retrieval/indexing/processor/extractor/`（新包 4 文件）
- 前置事实：TextChunk（common/document.go ✅）/ `*llm.Model` + `NewMessagesParam` + `WithInvokeTemperature`（13.15 captioner 先例 ✅）/ json-repair v0.0.18（go.mod 已有，stream_event_context.go 已用 ✅）/ 错误码 155507 StatusRetrievalKbTripleExtractionProcessError（codes_context.go 已预埋 ✅）——**零前置依赖，本节可独立实施**

## 1. 背景与定位

领域13 Extractor 层唯一成员：OpenIE（开放信息抽取）入口——把非结构化 chunk 文本经 LLM 抽成 RDF 风格三元组 `(subject, predicate, object)`，是 GraphRAG 图谱索引的写入原料。

Agent 会话流程位置（**不在会话主链路**，知识库构建期调用）：

```
【写入侧：文档索引管线】（领域13）
文件/URL → AutoParser(13.11 ✅) → Chunker(13.5-13.9 ✅) → ComputeChunkEmbeddings(13.4 ✅)
  → Indexer.BuildIndex(13.2/13.3 ✅) → kb_{id}_chunks
  → [use_graph=true] ★13.17 TripleExtractor★ → kb_{id}_triples（ triples 转 TextChunk 再走同一 Indexer）

【读取侧：检索查询管线】（13.19-13.28，2026-10-27 已登记扩充）
GraphRetriever(13.23) beam search 消费 kb_{id}_triples；AgenticRetriever(13.25) 多跳；
GraphKnowledgeBase(13.28) 是 13.17 的真实消费方（add_documents 中 extractor.extract(chunks)）
```

Python 侧生产消费方仅 GraphKnowledgeBase 一处（L130）；`Extractor.process` 生产零调用（唯一触达为 test_base.py 1 用例）。

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | 消费方缺口处置 | **C：补登记完整检索管线**——计划表扩充 13.19-13.28 十步（Retriever 层+QueryRewriter+KB 编排层，~4600 行源码+~4700 行测试）；本 spec 仅覆盖 13.17 本体 | 否决 A（仅 13.17 零调用交付：13.28 消费方悬空）；否决 B（带出 KB 骨架：范围膨胀）；否决 D（暂缓：领域13 留尾巴） |
| 2 | 章节组织 | **A：扩充领域13**（13.19-13.28），非新领域、非混合 | 否决 B/C（写入侧读取侧分域/混合：Triple 系类型横跨两域回填标记更多；领域13 验证点就地闭环） |
| 3 | 聚焦范围 | **13.17 先行独立实施**（零前置依赖已验证）；13.19-13.28 仅登记（产出列 Python 路径+简述），后续各走 spec→plan→实施循环 | — |
| 4 | Extractor 接口形态 | **B：Extract + Process 双方法**——逐字对齐 Python Extractor ABC 两公开方法；Process 转发 Extract（Chunker/Parser 的"Process 兼容入口"风格统一） | 否决 A（仅 Extract：process 生产零调用属实，但用户拍板完整对齐契约；test_base.py 3 用例原样可转 2 例） |
| 5 | 并发模型 | **A：预分配槽位+收齐扫描**——goroutine 各写 results[i] 槽位 + buffered chan 信号量（对齐 asyncio.Semaphore）+ wg 收齐后按 chunk 序扫描 | 否决 B（errgroup fail-fast：首失败即取消，Python 是全跑完，语义偏差）；否决 C（errgroup 仅当 wg 用冗余）。**复用资产**：gather(return_exceptions=True) 模式在 13.23/13.25/13.27 共 5 处再现 |
| 6 | llmClient 类型 | **`*llm.Model` 具体化**（Python Any；13.15 captioner 先例）；mock 走 `llm.WithClient` 注入 | — |
| 7 | json-repair 调用 | **`RepairWithOptions` 一步取 `result.Value`**（讨论期撤销初案两步方案）——`RepairResult.Value any` 直接返回解析后对象，含 recover 兜底，语义对应 `repair_json(return_objects=True)` | 否决 RepairJSON+二次 Unmarshal 两步（多余；Value 已是对象） |
| 8 | pyStr 归属 | **extractor 包内独立实现**（~15 行）——float64→pyFloatStr 语义、bool→"True"/"False"、其余 fmt.Sprint | 否决复用 13.13 excel cellStr（cellValue 密封类型体系与 []any 输入不匹配，跨包引入不值得） |

## 3. 依赖验证结论（讨论期间核实）

- `jsonrepair.RepairWithOptions(src, RepairOptions{}) (RepairResult, error)`：`RepairResult.Value any` / `.JSON string` / `.Log []RepairLogEntry`——一步拿到解析后对象（v0.0.18 options.go:41-49，recover 兜底 L50-51）
- `llm.Model.Invoke(ctx, messages, ...InvokeOption)` 存在；`model_clients.WithInvokeTemperature(t float64)`（invoke_params.go:257）；`llmschema.NewUserMessage(prompt)` 单文本消息；`resp.Content.Text()` 取文本
- `llm.WithClient(client BaseModelClient) ModelOption` 测试注入先例（model_test.go:55 mockModelClient）
- common 包现覆盖率 91.8%（triple.go 少量代码并入后维持 ≥90% 预期）
- 错误码 155507 已定义：`StatusRetrievalKbTripleExtractionProcessError "RETRIEVAL_KB_TRIPLE_EXTRACTION_PROCESS_ERROR"`（codes_context.go:224-227）
- Python `gather(return_exceptions=True)` 全跑完语义：asyncio 文档 + triple_extractor.py L117-134 逐行核对（首错按 chunk 序非完成序；成功结果 extend 后因 raise 丢弃）

## 4. 类型与接口设计

### 4.1 common/triple.go

```go
// Triple 三元组数据模型。
//
// Python: openjiuwen/core/retrieval/common/triple.py (Triple)
type Triple struct {
    // Subject 主语
    Subject string `json:"subject"`
    // Predicate 谓语
    Predicate string `json:"predicate"`
    // Object 宾语
    Object string `json:"object"`
    // Metadata 元数据（对齐 Python default_factory=dict；无 omitempty——序列化恒输出 metadata 键）
    Metadata map[string]any `json:"metadata"`
}
```

- 放 common 包（非 extractor 包）：对齐 Python 文件位置 + 13.22（TripleBeam）/13.28（GraphKB）读取侧复用
- pydantic 必填校验（ValidationError）无 Go 显式对应物——`str(x).strip()` 构造点保证非空字段天然满足，归档 §8-4

### 4.2 extractor/base.go

```go
// Extractor 抽取器抽象接口，用于三元组等信息抽取。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/base.py (Extractor)
// 注意：不建 Processor 基接口（13.5/13.10 既定决策），Process 作为
// Processor 契约兼容入口直接挂在接口上（对齐 Chunker/Parser 风格）。
type Extractor interface {
    // Extract 从文本分块中抽取信息（如三元组）。
    // Python **kwargs 由 opts 收纳（当前无消费者传参，预留）。
    Extract(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)
    // Process 处理文本分块（Processor 契约兼容入口），默认转发 Extract。
    // 对齐 Python Extractor.process → self.extract(chunks, **kwargs)。
    Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)
}

// ExtractOption Extract 的可选参数（对齐 Python **kwargs 收纳）。
type ExtractOption struct {
    // Extra 透传给实现的额外参数
    Extra map[string]any
}
```

### 4.3 extractor/triple_extractor.go

```go
// TripleExtractor 基于 LLM 的 OpenIE 三元组抽取器。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py
type TripleExtractor struct {
    // llmClient LLM 客户端实例（Python: llm_client: Any → Go 具体类型，13.15 先例）
    llmClient *llm.Model
    // modelName 模型名（Python 死参数：存字段但 invoke 从不使用——偏差 §8-1）
    modelName string
    // temperature 温度参数（invoke 时透传）
    temperature float64
    // limiter 并发信号量（对齐 asyncio.Semaphore(max_concurrent)）
    limiter chan struct{}
}

// NewTripleExtractor 构造。默认 temperature=0.0 / maxConcurrent=50。
// WithTemperature(float64) / WithMaxConcurrent(int) 选项函数。
func NewTripleExtractor(llmClient *llm.Model, modelName string, opts ...TripleExtractorOption) *TripleExtractor
```

## 5. 核心流程设计

### 5.1 Extract 主编排（方案 A）

```go
func (e *TripleExtractor) Extract(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error) {
    results := make([]chunkResult, len(chunks))          // 预分配槽位（空 chunks → 空 make，直接走扫描零循环）
    var wg sync.WaitGroup
    for i := range chunks {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            e.limiter <- struct{}{}                       // 对齐 async with self.limiter（包住调用+解析）
            defer func() { <-e.limiter }()
            results[i] = e.extractChunk(ctx, chunks[i])  // 各写各的槽位，无竞争
        }(i)
    }
    wg.Wait()                                             // 收齐全部（gather 语义：全跑完再扫描）

    allTriples := make([]common.Triple, 0)
    var firstErr error
    var firstErrChunkID string
    for i := range chunks {                               // 按 chunk 序扫描（对齐 for idx, result in enumerate）
        if results[i].err != nil {
            logger.Error(logComponent).Str("chunk_id", chunks[i].ID).Err(results[i].err).
                Msg("Task failed for chunk")              // 对齐 f"Task failed for chunk {chunk_id}: {result}"
            if firstErr == nil {
                firstErr = results[i].err
                firstErrChunkID = chunks[i].ID
            }
            continue
        }
        allTriples = append(allTriples, results[i].triples...)
    }
    if firstErr != nil {
        var be *exception.BaseError
        if errors.As(firstErr, &be) {                     // isinstance(first_error, BaseError): raise 原样
            return nil, firstErr
        }
        return nil, exception.BuildError(                // wrap 155507（防御分支：extractChunk 出错恒为 BaseError）
            exception.StatusRetrievalKbTripleExtractionProcessError,
            exception.WithParam("error_msg", firstErrChunkID+": "+firstErr.Error()),
            exception.WithCause(firstErr),
        )
    }
    return allTriples, nil                                // 注意：Python 首错时成功结果已 extend 但随 raise 丢弃——Go 对齐返回 nil
}
```

### 5.2 extractChunk（chunk 级）

```go
type chunkResult struct {
    triples []common.Triple
    err     error
}

func (e *TripleExtractor) extractChunk(ctx context.Context, chunk common.TextChunk) chunkResult {
    if e.llmClient == nil {                               // Python None.invoke AttributeError→wrap 的等价路径（§8-5）
        return chunkResult{err: exception.BuildError(155507, WithParam("error_msg",
            chunk.ID+": llm client is nil"))}
    }
    prompt := e.buildPrompt(chunk.Text, metadataTitle(chunk.Metadata))   // metadata.get("title","")
    messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))
    resp, err := e.llmClient.Invoke(ctx, messages,
        model_clients.WithInvokeTemperature(e.temperature))
    if err != nil {
        logger.Error(logComponent).Str("chunk_id", chunk.ID).Err(err).
            Msg("Failed to extract triples from chunk")   // 对齐 f"Failed to extract triples from chunk {id}: {e}"
        return chunkResult{err: exception.BuildError(155507,
            WithParam("error_msg", chunk.ID+": "+err.Error()), WithCause(err))}
    }
    triples, ok := e.parseTriples(resp.Content.Text(), chunk.DocID, chunk.ID)
    if !ok {
        return chunkResult{err: exception.BuildError(155507, WithParam("error_msg",
            chunk.ID+": LLM response could not be parsed as valid triple JSON"))}
    }
    return chunkResult{triples: triples}
}

// Process 转发 Extract（对齐 Python 基类一行转发）。
func (e *TripleExtractor) Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error) {
    return e.Extract(ctx, chunks, opts...)
}
```

### 5.3 buildPrompt（提示词）

- `tripleExtractionPrompt` 常量：Python 模板**逐字复制**（含 Magic Johnson / Elden Ring 两个 Demonstration 与全部 Requirements；提示词铁律：禁止翻译改写），`{{`/`}}` 归一为 `{`/`}`（Python .format 转义字面花括号；归一后 `{title}/{passage}` 为唯二命名槽——Python 源模板已逐字核对无其他 `{word}` 形态）
- `buildPrompt(passage, title)`：`title == "" → "Untitled"`（对齐 `title or "Untitled"`）；`strings.NewReplacer("{title}", title, "{passage}", passage)` 替换——**不用 fmt.Sprintf**（正文大量花括号会炸）
- 渲染结果与 Python `.format()` 逐字节等价（few-shot Output 块 Python 渲染后同为单花括号）

### 5.4 parseTriples（JSON 解析）

```go
// parseTriples 解析 LLM 输出为 Triple 列表。
// 返回 (triples, parseSuccess)：硬解析失败 false；空三元组列表 true（Python ([], True)）。
func (e *TripleExtractor) parseTriples(content, docID, chunkID string) ([]common.Triple, bool)
```

流程（对齐 `_parse_triples` L265-338）：

1. `strings.TrimSpace(content)`（content.strip()）
2. fence 剥离：`strings.HasPrefix(content, "```")` → `lines := strings.Split(content, "\n")`；`len(lines) > 2` 才取 `lines[1:len(lines)-1]` join 回（否则原文）——`Split` 与 Python `split("\n")` 行界一致
3. `jsonrepair.RepairWithOptions(content, RepairOptions{})`——err 时 Error 日志（含 `content[:200]` 截断，对齐 `"Failed to parse triples from content: %s. Content: %s"`）返回 `(nil, false)`
4. `result.Value`（any）双形态分发：`map[string]any`（须含 `triples` 且为 `[]any`，否则 false）/ `[]any`（顶层即列表）/ 其他 false
5. 空列表 → `([]common.Triple{}, true)`
6. 逐条校验：非 `[]any`（isinstance (list,tuple)）→ invalid++；`len < 3` → invalid++；`head := arr[:3]` 含嵌套容器（`[]any`/`map[string]any`）或 `nil` → invalid++
7. 合格项 `Triple{Subject: pyStr(head[0]), Predicate: pyStr(head[1]), Object: pyStr(head[2]), Metadata: map[string]any{"doc_id": docID, "chunk_id": chunkID}}`——对齐 `str(head[i]).strip()` 两步语义：pyStr 只做类型字符串化，**strip 由本步骤统一 TrimSpace 三字段**
8. `invalidCount > 0` → Warn `Ignored {invalid_count} invalid triples for chunk {chunk_id} during parsing`
9. 返回 `(triples, len(triples) > 0)`（bool(triples)：全 invalid → (空, false) 触发上层 155507）

### 5.5 pyStr（包内辅助）

```go
// pyStr 对齐 Python str() 的字符串化（13.13 cellStr 同源语义，包内独立实现）。
// 仅做类型字符串化，不做 strip（对齐 str() 单步；strip 由 parseTriples 统一做）。
// float64 → pyFloatStr 语义（strconv -1 最短往返：1.0→"1.0"、1e16 以上 e 记法）；
// bool → "True"/"False"；string 原样；其余 fmt.Sprint。
// nil 分支不可达（head 校验已滤 None）——防御保留返回 "None"。
func pyStr(v any) string
```

## 6. 错误处理与日志

| Python 日志 | Go 对齐 | 级别 |
|---|---|---|
| `f"Failed to extract triples from chunk {chunk.id_}: {e}"`（L110，invoke 失败） | `.Str("chunk_id",...).Err(err).Msg("Failed to extract triples from chunk")` | Error |
| `f"Task failed for chunk {chunk_id}: {result}"`（L129，gather 扫描） | `.Str("chunk_id",...).Err(err).Msg("Task failed for chunk")` | Error |
| `"Failed to parse triples from content: %s. Content: %s"`（L289，repair 异常） | `.Err(err).Str("content", truncatePrefix(content,200)).Msg(...)` | Error |
| `"Failed to parse triples: %s"`（L337，外层兜底 except） | Go 无 panic 路径；`RepairWithOptions` 已含 recover——**零 Go 对应物归档**（§8-6） | — |
| `"Ignored %d invalid triples for chunk %s during parsing"`（L328-332） | `.Int("invalid_count",...).Str("chunk_id",...).Msg(...)` | Warn |

Msg 英文规则（feedback 记忆）：Python 原文本已英文，直取。构造无日志（Python 同）。

## 7. 测试设计

### 7.1 Python 基线 1:1（23 例）

| 文件 | 用例 → Go |
|---|---|
| test_triple_extractor.py（16） | test_init / test_init_with_defaults / TestExtract_多分块（mock invoke 计数=2）/ TestExtract_invoke异常（断言 155507 + "429 too many requests" in msg）/ TestExtract_invalidJSON（断言 "parsed" in msg）/ TestExtract_空chunks（返回空且 invoke 未调）/ parse 7 例（顶层数组 / 多余字段截取 / 包裹 dict / 含 named_entities / 缺 triples key → (nil,false) / invalid 混合 → 1 合格 / 全 invalid → (空,false)） |
| test_base.py（3） | concreteExtractor 测试结构体：TestExtractor_Extract / TestExtractor_Process（转发）；cannot_instantiate → 归档 §8-4 |
| test_triple.py（4） | TestTriple_创建（默认 Metadata 空断言）/ 带metadata / 字段断言×2 |

mock：`llm.NewModel(WithClient(mockModelClient))`——mockModelClient 实现 `Invoke(ctx, messages, opts...)`，可编程返回 content / error / 记录 InvokeParams（temperature 断言用）。

### 7.2 Go 增强（8 项，覆盖归档偏差路径）

1. nil llmClient → 155507（§8-5 路径）
2. 首错按 chunk 序：chunk[0] 慢成功 + chunk[1] 快失败 → 首错取 [1]（锁语义下无竞争，确定性构造）
3. temperature 透传断言（mock 内 NewInvokeParams(opts...).Temperature == 0.0 / 自定义 0.7）
4. prompt 渲染：title 空→Untitled / 非空 / passage 含 `{` 特殊字符不破坏 Replacer
5. pyStr 边界：1.0 / 1e-4 / 1e16 / True / False / 42 / "abc"
6. fence 边界：3 行 fence 剥离 / 2 行不剥
7. repair 极坏输入（空串/纯文本）→ (nil,false)
8. maxConcurrent=1 串行性（信号量语义：mock 计数并发峰值=1）

### 7.3 覆盖率目标

- extractor 新包 ≥90%（parser 89-90% / chunker 92-94% 先例区间）
- common 包维持 ≥90%（基线 91.8% + triple.go 少量代码）
- 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（CI 同款标签，testing-build-tags 记忆）

## 8. 已知偏差归档（Go vs Python）

| # | 偏差 | 说明 |
|---|---|---|
| 1 | modelName 死参数 | Python 同样存字段不用于 invoke；Go 保留字段对齐 API 形状（13.28 构造传参用） |
| 2 | CancelledError 分支省略 | Python `isinstance(result, BaseException) and not isinstance(result, Exception)` 直接 raise 不记首错；Go ctx 取消走普通 error → wrap 155507 路径 |
| 3 | pyStr 语言差异 | `str(True)→"True"`、`str(1.0)→"1.0"`（13.13 先例）；`pyStr(nil)→"None"` 不可达防御。json.Number 分支超 int64 纯整数字面原样透传（2026-10-27 审查 S-02 修复：初版 `Atoi 失败→Float64` 把 `12345678901234567890` e 记法化 `"1.2345678901234568e+19"`；Python `str(int)` 无界原样。补 `isPureIntegerLiteral` 纯数字判定透传，超界测试用例 3 例） |
| 4 | 抽象实例化/pydantic 校验零实现 | cannot_instantiate 用例与 ValidationError 必填校验：Go 接口编译期防实现外实例化 / 结构体无校验机制，天然等价 |
| 5 | nil llmClient → 155507 | Python None.invoke AttributeError 被 except wrap；Go 显式判 nil 返回 155507（硬失败，区别于 13.15 captioner 的禁用态软失败） |
| 6 | 外层兜底 except 无对应 | Python L336-338 全函数 try/except 兜底（防 repair_json 之外意外异常）；Go 无 panic 路径（RepairWithOptions 自带 recover），零代码归档 |
| 7 | 成功结果随首错丢弃 | Python `all_triples.extend` 先于 raise——最终错误返回时成功结果不返回；Go 显式 `return nil, firstErr`（allTriples 计算后丢弃，注释说明对齐意图） |
| 8 | json-repair 库深修复路径 float32 截断（2026-10-27 审查 S-03，拍板跳过） | 库 v0.0.18 `jsonrepair.go:1081` 用 `ParseFloat(...,32)` 解析数字（float32 24 位尾数）：深修复路径（LLM 输出不合规 JSON 高频路径）`1e-4`→`"9.9999997e-05"`、`1e15`→`"999999986991104.0"`；Python json_repair 0.64.2 双精度 `"0.0001"`/`"1000000000000000.0"`。合法 JSON 短路路径（json.Valid+UseNumber）无损。库级根因，等上游修复（ParseFloat 64），暂不包装层绕过 |
| 9 | json-repair 库深修复路径超 int64 截断（2026-10-27 审查 S-04，拍板跳过与 S-03 一致） | 库 v0.0.18 `jsonrepair.go:1088` `Atoi` 忽略 ErrRange：超大整数截断为 MaxInt64（`12345678901234567890`→`"9223372036854775807"`）；Python int 无界原样。与 S-03 同根因同代码区域，一并等上游 |
| 10 | metadata title 非字符串回落 Untitled（2026-10-27 审查 M-01(13.17)，拍板归档有意防御） | Python `title or "Untitled"` 对 42/true 等 truthy 值 str 化渲染（"42"）；Go metadataTitle 类型断言失败返回空串回落 "Untitled"——上游脏数据回落更安全 |
| 11 | ~~normalizeActiveHours 对 bool 无分支~~（条目错位，2026-10-28 移除） | 该项属 heartbeat 域，已在 11.11 spec §8-⑱ 正确归档，本表误收录 |
| 12 | repair 失败日志 content 截断不加 "..." 后缀（2026-10-27 审查 T-09 修复） | Python `logger.debug(f"...content={content[:100]}")` 纯截断；Go 初版 `truncateContent` 追加 "..." 后缀——L100 边界输出超 100 字符，已删 |
| 13 | WithTripleMaxConcurrent n≤0 钳制到默认 50（2026-10-27 审查 T-10 修复） | Python `asyncio.Semaphore(-1)` 构造期 ValueError fail-fast（triple_extractor.py 测试即有该用例）；Go 初版 `make(chan struct{}, n)` 负数 panic、0 容量永久阻塞。用户拍板钳制 `defaultTripleMaxConcurrent = 50`（构造器/NewTripleExtractor 默认值统一走该常量），非法输入静默修正不炸启动 |

## 9. 实施完成回填清单

1. IMPLEMENTATION_PLAN.md 13.17 行 `☐`→`✅` + 产出描述 + 本 spec 路径链接（**不触碰 13.19-13.28 十行已登记内容**，也不触碰 13.1-13.16/13.18 既有行）
2. 7.36 行 `⤵️` 表述补“含 2026-10-27 扩充的检索查询管线 13.19-13.28”（领域13 缺口清单完整指向）
3. extractor 包 doc.go（新包首建即含）+ common 包 doc.go 文件目录树加 triple.go 条目
4. 领域13 验证点行不回填（读取侧 ☐ 待 13.19-13.28 完成后闭环——2026-10-27 扩充时已加 ☐ 项）

## 10. 文件清单

```
internal/agentcore/retrieval/common/
├── triple.go                  # 新增：Triple 数据模型
└── triple_test.go              # 新增：test_triple.py 4 例

internal/agentcore/retrieval/indexing/processor/extractor/
├── doc.go                      # 新增：包文档（模板同 chunker/parser）
├── base.go                     # 新增：Extractor 接口（Extract+Process）
├── base_test.go                # 新增：test_base.py 2 例（cannot_instantiate 归档）
├── triple_extractor.go         # 新增：TripleExtractor 主体
└── triple_extractor_test.go    # 新增：16 例 1:1 + 8 项 Go 增强
```

doc.go 文件目录同步：common 包 doc.go 加 triple.go 条目；extractor 包 doc.go 新建（含对齐 Python extractor/ 路径说明）。
