# 13.9 分块工具（TextChunker + text_preprocessor）设计文档

- 日期：2026-10-20
- 计划条目：IMPLEMENTATION_PLAN.md 13.9（分块工具）
- Python 源：`openjiuwen/core/retrieval/indexing/processor/chunker/chunking.py`（126 行）+ `text_preprocessor.py`（201 行）
- Go 目标包：`internal/agentcore/retrieval/indexing/processor/chunker/`
- 备注：13.6 已带出 CharSplitter + TextSplitter 接口、13.7 已带出 IndexSentenceSplitter + splitter 包全部、13.8 已带出 mergeChunkMetadata——本步骤仅剩 chunking.py TextChunker + text_preprocessor.py（计划表行内既有 ⚠️ 标注，一字不动）

## 1. 背景与定位

TextChunker 是 SDK 门面分块器：把"预处理管线 + 单位路由"组合成一站式分块入口。SDK 用户不直接挑 CharChunker/TokenizerChunker，只传 `chunk_unit`（char/token）；token 路径自动从 embed_model 提取 tokenizer（无则 tiktoken fallback）并对超限 chunk_size 自动下调。`text_preprocessor.py` 提供四个文本清洗组件（WhitespaceNormalizer / URLEmailRemover / SpecialCharacterNormalizer / PreprocessingPipeline），入库前清洗文本提升嵌入质量。

Agent 会话流程位置（仅入库期被调用，不在会话主链路）：

```
【入库管线】文件/链接 → Parser 解析（13.10-13.16 未实现）产出 Document[]
  → SimpleKnowledgeBase.add_documents（chunker 由外部注入）
  → TextChunker.ChunkDocuments                    ← ★ 13.9 本体
      ├─ PreprocessingPipeline 文本清洗（去 URL/邮箱、空白归一化）
      ├─ 路由 inner：chunk_unit="char" → CharChunker(13.6✅)
      │   否则 → embed_model.tokenizer 提取 / tiktoken fallback(13.7✅)
      │   + chunk_size 超 tokenizer.model_max_length 自动下调
      └─ 组块：TextChunk{doc_id, chunk_index, total_chunks, chunk_id}
  → ComputeChunkEmbeddings（13.4✅）→ Chroma/MilvusIndexer.BuildIndex（13.2/13.3✅）

【查询管线】agent 会话内 knowledge_retrieval_comp → retrieve → 向量检索 → rerank → 注入 prompt
  （查询路径不经过 TextChunker，但检索粒度由入库分块质量决定）
```

Python 生产代码中 TextChunker **零调用**（仅 `retrieval/__init__.py` 导出 + 单元测试）；KnowledgeBase 层收 `Chunker` 接口。13.9 完成后 Chunker 层（13.5-13.9）收尾，`processor/` 只剩 Parser 层（13.10+）与 Extractor（13.17）。

**章节顺序结论**：依赖（BaseChunker/mergeChunkMetadata 13.5、CharChunker 13.6、TokenizerChunker+tiktoken 13.7）全部就绪零缺口；Parser 层与 Extractor 均不依赖 TextChunker。按编号 13.9 → 13.10 推进最优。

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | 章节顺序 | 按编号 13.9 → 13.10，不换章节 | 依赖零缺口、Chunker 层收尾、体量与 13.8 相当；Parser 层产出 Document 不依赖 13.9 |
| 2 | TextChunker 结构（A） | **A1：嵌入 BaseChunker + 三处覆写**——ChunkText 返回显式错误（"不支持直接调用"）、ChunkDocuments 完全覆写（pipeline→inner→mergeChunkMetadata）、Process 局部覆写转发 | 否决 A2（ChunkText 实现 pipeline+inner 组合：Python 继承基类返回 None 事实不可用，实现它会多出一条 Python 不存在的路径）；否决 A3（不嵌入 BaseChunker：丢失校验复用且与兄弟 chunker 不同构） |
| 3 | 正则引擎（D） | **D1'：regexp2 + 显式 Python 语义集**——`\w`→`[\p{L}\p{Nd}\p{Nl}\p{No}_]`、`\s`→`[\s\x{1c}-\x{1f}]`（补偿 .NET 缺口）、负向前瞻直接可用、`\b` 保留原生并标注偏差 | 否决手写扫描器（~200 行手写正确性自负）；否决 D2 直接翻译（实测 `①②③` 被 REDUNDANT_SYMBOLS 误删，违背对齐不丢失）。regexp2 v1.12.0 已在 go.mod（at_file.go 先例），实测 `\p{No}/\p{Nl}/\p{Sc}` 类别语法全支持、REDUNDANT_SYMBOLS 完整体 8 样本对拍一致 |
| 4 | `\b` 词边界 | **保留原生 regexp2 `\b` + 设计文档标注已知偏差** | 差异仅 No/Nl/Mn/Pc（①Ⅷ 组合记号等罕见字符）紧贴邮箱/域名时出现；Python 测试不覆盖此场景；显式环视重写模式可读性下降 |
| 5 | embed_model 映射（B） | **WithEmbedModel(any) + TokenizerProvider 可选接口断言**：`type TokenizerProvider interface { Tokenizer() Tokenizer }`，构造时断言提取 | 精确等价 Python `getattr(embed_model, "tokenizer", None)`；否决参数直接收 Tokenizer（把 getattr 语义转嫁给每个调用点） |
| 6 | 构造签名（C） | **NewTextChunker(chunkSize, chunkOverlap int, opts ...TextChunkerOption)**：WithChunkUnit / WithEmbedModel / WithPreprocessOptions | 与 registry ChunkerOption 风格一致；preprocess_options dict 只消费 2 已知键 → `PreprocessOptions{NormalizeWhitespace, RemoveURLEmail bool}` 结构体替代（未知键 Python 本就静默忽略） |
| 7 | registry 注册（E） | **不注册 "text"**（Python `__init__.py` 仅注册 char/hybrid） | TextChunker 是 SDK 门面非底层策略；13.7 tokenizer 注册是唯一主动增强例外，无必要重复 |
| 8 | CharsToReplace 类型（F） | **[]CharReplacement 有序切片**：`type CharReplacement struct{ Old, New string }` | Python dict 按插入序迭代（链式替换 a→b, b→c 依赖顺序），Go map 无序会破坏链式替换语义 |
| 9 | 155102 可测性 | **不测 get_encoding 失败分支**（覆盖率缺口留白） | 用户拍板；tiktoken 是编译期依赖，失败分支依赖 tiktoken 内部错误 |
| 10 | token 路径测试 | **真实 tiktoken 内置即测**（无 build tag） | tiktoken-go 编译期依赖非外部服务，cl100k_base 词表本地计算不联网；13.7 TokenizerChunker 测试同模式 |

## 3. 文件布局与变更面

```
chunker/
├── doc.go                    # 更新：文件目录补 text_preprocessor.go + text_chunker.go 条目
├── text_preprocessor.go      # ★ 新文件：TextPreprocessor 接口 + 3 预处理器 + Pipeline + 语义集常量
├── text_preprocessor_test.go # ★ 新测试文件
├── text_chunker.go           # ★ 新文件：TextChunker 门面 + TokenizerProvider + getChunker 路由
└── text_chunker_test.go      # ★ 新测试文件
```

- 新增外部依赖：`github.com/dlclark/regexp2`（v1.12.0 已在 go.mod，非新增）
- 其余 7 个既有文件零改动（base.go / registry.go / char_chunker.go / tokenizer_chunker.go / hybrid_chunker.go / text_splitter.go 及测试）
- IMPLEMENTATION_PLAN.md：13.9 ☐→✅，产出列按 13.8 风格追加摘要
- 章节顺序决策已回填：13.9 依赖零缺口，按编号推进

## 4. text_preprocessor.go 设计

### 4.1 类型定义

```go
// TextPreprocessor 文本预处理器抽象接口（对齐 Python TextPreprocessor ABC）。
// Python __call__ 语法糖由 Go 方法调用天然替代，不单列。
type TextPreprocessor interface {
    // Process 处理输入文本并返回清洗结果。
    Process(text string) string
}

// CharReplacement 单字符替换规则（对齐 Python chars_to_replace dict 单项）。
// 有序切片替代 dict：Python dict 按插入序迭代，链式替换依赖顺序。
type CharReplacement struct {
    // Old 被替换字符
    Old string
    // New 替换为的字符
    New string
}

// WhitespaceNormalizer 空白归一化器（对齐 Python WhitespaceNormalizer）。
type WhitespaceNormalizer struct{}

// URLEmailRemover URL/邮箱移除器（对齐 Python URLEmailRemover）。
type URLEmailRemover struct {
    // RemoveURLs 是否移除 URL
    RemoveURLs bool
    // RemoveEmails 是否移除邮箱
    RemoveEmails bool
    // Replacement 替换字符串（默认 ""）
    Replacement string
}

// SpecialCharacterNormalizer 特殊字符归一化器（对齐 Python SpecialCharacterNormalizer）。
type SpecialCharacterNormalizer struct {
    // CharsToRemove 待移除字符集（字符串形式）
    CharsToRemove string
    // CharsToReplace 有序替换规则切片（F 决策）
    CharsToReplace []CharReplacement
}

// PreprocessingPipeline 预处理管线（对齐 Python PreprocessingPipeline）。
type PreprocessingPipeline struct {
    // preprocessors 顺序应用的预处理器列表
    preprocessors []TextPreprocessor
}
```

### 4.2 正则模式与显式语义集（D1' 决策落地）

```go
// ──────────────────────────── 常量 ────────────────────────────

// pyWordClassStr Python \w 精确集：[\p{L}\p{Nd}\p{Nl}\p{No}_]
//（Python \w 含 No/Nl 类字符如 ①½Ⅷ，.NET \w 含 Mn/Pc 但缺 No/Nl——
// 实测差异：regexp2 原生 \w 下 "①②③序号" 被 REDUNDANT_SYMBOLS 误删）
const pyWordClassStr = `[\p{L}\p{Nd}\p{Nl}\p{No}_]`

// pySpaceClassStr Python \s 补偿集：\s 之外补 \x{1c}-\x{1f}
//（.NET \s 与 Python \s 在 U+0085/U+00A0/U+1680/U+2028 等已一致，
// 唯缺文件分隔符组 \x1c-\x1f）
const pySpaceClassStr = `[\s\x{1c}-\x{1f}]`
```

四个模式（regexp2.MustCompile，包级 var，对齐 Python 类属性正则编译一次）：

```go
var (
    // wCore/sCore 语义集"内芯"（去掉外层方括号），供字符类拼接：
    // wCore = `\p{L}\p{Nd}\p{Nl}\p{No}_`
    // sCore = `\s\x{1c}-\x{1f}`
    //（实施时定义为常量或由辅助函数从 pyWordClassStr/pySpaceClassStr 提取）

    // pySpaceSeqPattern Python r"\s+"：连续空白 → 单空格（pySpaceClassStr + "+"）
    pySpaceSeqPattern = regexp2.MustCompile(pySpaceClassStr+`+`, regexp2.None)
    // urlPattern Python URL_PATTERN r"https?://\S+|www\.\S+"
    //（\S → [^sCore]，negatedPySpace() 辅助函数生成）
    urlPattern = regexp2.MustCompile(`https?://`+negatedPySpace()+`+|www\.`+negatedPySpace()+`+`, regexp2.None)
    // comPattern Python COM_PATTERN：`\S+?` 同上替换；`\b` 保留原生（偏差 4）
    comPattern = regexp2.MustCompile(`(?:https?://|www\.)?`+negatedPySpace()+`+?\.(?:com|net|org|cn)(?:[/?#]`+negatedPySpace()+`*)?\b`, regexp2.None)
    // emailPattern Python EMAIL_PATTERN：`\w` → pyWordClassStr，`\b` 原生（偏差 4）
    //（实测对拍：`联系①user@example.com` → Go/Python 同为吞掉 ①，偏差 4 场景两侧行为一致）
    emailPattern = regexp2.MustCompile(`\b[`+wCore+`\.-]+@[`+wCore+`\.-]+\.`+pyWordClassStr+`+\b`, regexp2.None)
    // controlCharPattern Python CONTROL：[\x00-\x1F\x7F]（原样直译，无语义差）
    controlCharPattern = regexp2.MustCompile(`[\x00-\x1F\x7F]`, regexp2.None)
    // redundantSymbolsPattern Python REDUNDANT_SYMBOLS 双负向前瞻原样直译
    //（[^\w\s#*_\-\|] → [^wCore+sCore+#*_\-\|]，两组前瞻保持原样）
    redundantSymbolsPattern = regexp2.MustCompile(
        `(?![\x{4e00}-\x{9fa5}，。！？；：“”‘’（）【】《》、…·])(?![#*\-_]{2,})([^`+
            wCore+sCore+`#*_\-\|]){2,}`, regexp2.None)
)

// negatedPySpace 生成 Python \S 等价的字符类（含方括号）：
// "[^" + sCore + "]"，供 URL/COM 模式拼接。
func negatedPySpace() string {
    return `[^` + sCore + `]`
}
```

**模式字符串最终形态必须与 Python 原文逐字符比对**（实施时以 `text_preprocessor.py` L66-L115 原文为准）：`\w`→`[\p{L}\p{Nd}\p{Nl}\p{No}_]`、`\s`→`[\s\x{1c}-\x{1f}]`、`\S`→`[^\s\x{1c}-\x{1f}]`；其余结构（前瞻、量词、分组、`\b`）原样保留。拼接辅助函数（negatedPySpace / wCore / sCore）仅消除重复，不改变语义。Python 原文 `(?![#*\-_]{2,})` 的 `\-` 转义与字符类内容保持原样。

**模式对拍验证结论（2026-10-20 实测，14 样本）**：上述最终形态在 Go regexp2 下与 Python `re` 基准对拍 14/14 一致——URL/COM/EMAIL 移除（`访问 https://example.com/page?q=1 查看`→`访问  查看` 等 5 例）、REDUNDANT_SYMBOLS（`hello!!!world`→`helloworld`、`中文。。。句子`/`###标题###`/`单一&符号`/`a-b-c正常`/`a@b@c` 不动、`￥￥￥三个`→`三个`、**`①②③序号` 保留**——显式 \w 集回归通过）、CONTROL（`a\x00b\x1f控制`→`ab控制`）、whitespace 归一（`This\nis\ta   test　全角`→`This is a test 全角`）。EMAIL 模式注意：尾部 `\.\w+\b` 的 `+` 量词不可丢（拼接时曾遗漏致 2 样本失败）。

### 4.3 Process 实现（三预处理器 + Pipeline）

**WhitespaceNormalizer**：空文本直返；`pySpaceSeqPattern.Replace(text, " ", 0, -1)` → `strings.TrimFunc` 两端（用同一 pySpace rune 集）。Python `text.strip()` 是 Unicode 空白 strip，与 `\s` 集合一致。

**URLEmailRemover**：空文本直返；RemoveURLs → urlPattern.Replace → comPattern.Replace；RemoveEmails → emailPattern.Replace。顺序对齐 Python：URL_PATTERN → COM_PATTERN → EMAIL_PATTERN。

**SpecialCharacterNormalizer**：空文本直返；controlCharPattern.Replace("") → redundantSymbolsPattern.Replace("") → 遍历 CharsToReplace 依序 `strings.ReplaceAll`（链式语义）→ CharsToRemove 非空时构造 `[\Q字符集\E]` 转义模式 Replace("")（Python `re.escape` 对应 regexp2 的字面量转义，实施时以行为对拍为准）。

**PreprocessingPipeline**：NewPreprocessingPipeline(preprocessors ...TextPreprocessor)（nil 切片 → 空管线）、AddPreprocessor、Process（顺序循环）、Len()（对齐 Python `__len__`）。

## 5. text_chunker.go 设计

### 5.1 结构体与构造

```go
// TokenizerProvider 可选 tokenizer 提供者（对齐 Python getattr(embed_model, "tokenizer", None)）。
type TokenizerProvider interface {
    // Tokenizer 返回嵌入模型关联的分词器；nil 表示未提供。
    Tokenizer() Tokenizer
}

// TextChunker 分块工具门面（对齐 Python chunking.py TextChunker）。
type TextChunker struct {
    *BaseChunker                        // 尺寸字段 + 155100/155101 校验
    pipeline      *PreprocessingPipeline // nil 安全：空管线 Process 直通
    inner         Chunker                // 路由产物
}

// PreprocessOptions 预处理开关（对齐 Python preprocess_options dict 的 2 个已知键）。
type PreprocessOptions struct {
    // NormalizeWhitespace 是否启用空白归一化
    NormalizeWhitespace bool
    // RemoveURLEmail 是否启用 URL/邮箱移除
    RemoveURLEmail bool
}

// TextChunkerOption 门面构造选项。
type TextChunkerOption func(*textChunkerOptions)
// 内部 textChunkerOptions：chunkUnit(默认"char") / embedModel any / preprocessOptions *PreprocessOptions
// 选项：WithChunkUnit / WithEmbedModel / WithPreprocessOptions
```

构造链（严格对齐 Python `__init__` 执行序）：

1. `NewBaseChunker(chunkSize, chunkOverlap)` 过 155100/155101（Python `super().__init__`）
2. 组装 preprocessors：`preprocessOptions.Get("normalize_whitespace")` → append WhitespaceNormalizer；`Get("remove_url_email")` → append URLEmailRemover（顺序：whitespace 先、url 后，对齐 Python if 序）；`NewPreprocessingPipeline(preprocessors...)`
3. `getChunker(chunkSize, chunkOverlap, chunkUnit, embedModel)` 路由（5.2）
4. `SetSelf(textChunker)` 注入（模式一致性；self 路由实际不被消费——ChunkDocuments 为完全覆写，见 5.3 说明）

### 5.2 getChunker 路由（对齐 Python 四分支）

```go
func getChunker(chunkSize, chunkOverlap int, chunkUnit string, embedModel any) (Chunker, error) {
    if chunkUnit == "char" {
        return NewCharChunker(WithChunkSize(chunkSize), WithChunkOverlap(chunkOverlap))
    }
    // 分支2：embedModel 断言 TokenizerProvider 提取 tokenizer
    var tk Tokenizer
    if embedModel != nil {
        if p, ok := embedModel.(TokenizerProvider); ok {
            tk = p.Tokenizer()
        }
    }
    // 分支3：nil → tiktoken fallback（155102 失败分支保留）
    if tk == nil {
        tt, err := NewTiktokenTokenizer()   // 13.7 既有函数
        if err != nil {
            return nil, exception.BuildError(exception.StatusRetrievalIndexingTokenizerProcessError,
                exception.WithParam("error_msg", "Failed to load tokenizer for token-based chunking: "+err.Error()))
        }
        tk = tt
        logger.Info(logComponent).Msg("Using tiktoken(cl100k_base) as tokenizer")
    }
    // 分支4：chunk_size 下调（对齐 hasattr(tokenizer, "model_max_length") 探测）
    if p, ok := tk.(MaxLengthProvider); ok {
        if maxLen := p.MaxLength(); maxLen > 0 && chunkSize > maxLen {
            original := chunkSize
            chunkSize = maxLen
            logger.Warn(logComponent).
                Int("original_size", original).
                Int("chunk_size", chunkSize).
                Msg("chunk_size exceeds tokenizer limit, automatically adjusted")
        }
    }
    return NewTokenizerChunker(chunkSize, chunkOverlap, tk, "auto", nil)
}
```

对齐说明：
- Python `tiktoken is None`（未安装）分支 Go 不可达（编译期依赖），偏差 3
- Python `model_max_length < inf` 判定 → MaxLengthProvider.MaxLength() > 0（13.7 三态接口语义：0=未提供）
- NewTokenizerChunker 的 language/splitterConfig 用 Python 默认值（"auto"/None→nil）
- 字符串参数 `"cl100k_base"` 与 Python 日志文案 `Using tiktoken(cl100k_base) as tokenizer` 逐字符对齐

### 5.3 三方法覆写（A1 决策落地）

| 方法 | Python 行为 | Go 实现 |
|------|------------|---------|
| ChunkText | 继承基类 `return None`（事实不可用） | **返回显式错误**：`fmt.Errorf("TextChunker does not support ChunkText; use ChunkDocuments")`（对齐 base.go chunkTextOf standalone 错误先例） |
| ChunkDocuments | 完全覆写（L97-125） | 见下方代码 |
| Process | 基类默认 → 动态分发到覆写版 | **局部覆写**（13.8 决策 3 同款预警）：三行转发 `return c.ChunkDocuments(ctx, docs)` |

```go
func (c *TextChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
    _ = ctx // 对齐 Python 同步签名
    chunks := make([]common.TextChunk, 0)
    for _, doc := range docs {
        docText := doc.Text
        if c.pipeline != nil {
            docText = c.pipeline.Process(docText)
        }
        texts, err := c.inner.ChunkText(docText)
        if err != nil {
            return nil, err
        }
        for i, text := range texts {
            uid := uuid.NewString()
            meta := mergeChunkMetadata(doc.Metadata, i, len(texts), uid)
            chunks = append(chunks, common.TextChunk{ID: uid, Text: text, DocID: doc.ID, Metadata: meta})
        }
    }
    return chunks, nil
}
```

对齐说明：
- 预处理内嵌在 chunk_documents 循环（`doc_text = self.pipeline(doc.text)` 后再调 inner）——Python 原样结构
- 组块逻辑与 BaseChunker.ChunkDocuments 同型 → 复用 mergeChunkMetadata（metadata 三键一致：chunk_index/total_chunks/chunk_id）
- 调用的是 `inner.ChunkText` 而非 `self.chunk_text`——Python 调 `self.chunker.chunk_text`（inner 的），非覆写版
- 空 pipeline（无预处理选项）直通不清洗——对齐 Python 空 preprocessors 列表行为

## 6. 错误处理与日志

| 场景 | Python | Go |
|------|--------|-----|
| 尺寸校验失败（构造） | build_error 155100/155101 | NewBaseChunker 透传 |
| tiktoken 加载失败（token 路径） | build_error 155102 + cause | BuildError(155102) + error_msg 含原始错误（决策 9：不测该分支） |
| chunk_size 超限 | logger.warning（%d/%d 两值） | logger.Warn + original_size/chunk_size 双字段（规则 3.3） |
| tiktoken fallback 启用 | logger.info("Using tiktoken(cl100k_base) as tokenizer") | logger.Info 同文案 |
| inner 分块失败 | 异常上抛 | 透传 inner error 立即中断 |
| ChunkText 误用 | 返回 None（调用者崩） | 显式错误（A1 决策） |

日志组件：`logComponent = logger.ComponentAgentCore`（chunker 包既有，13.6 引入）。

## 7. 与 Python 的已确认偏差清单

| # | 偏差 | 理由 |
|---|------|------|
| 1 | registry 不注册 "text" | Python `__init__.py` 仅注册 char/hybrid，无 text 注册项 |
| 2 | PreprocessOptions 结构体替代 dict | 只消费 normalize_whitespace/remove_url_email 2 已知键；未知键 Python 静默忽略无损失 |
| 3 | tiktoken "未安装" 分支不可达 | Go 编译期依赖，类型系统吸收（13.7 同类先例）；get_encoding 失败分支保留（155102） |
| 4 | `\b` 用 regexp2 原生语义 | 与 Python \b 在 No/Nl/Mn/Pc 字符紧贴邮箱/域名边界时判定分叉（如 `联系①user@example.com` 场景）；Python 测试不覆盖；用户拍板保留原生 + 标注 |
| 5 | CharsToReplace 有序切片 | Python dict 插入序迭代；Go map 无序会破坏链式替换（a→b, b→c 需 a 变 c） |
| 6 | TokenizerChunker language="auto"/splitterConfig=nil 固定默认 | Python TextChunker.get_chunker 未暴露这两个参数，用默认值构造 |
| 7 | TextChunker.ChunkText 显式错误 | Python 继承基类返回 None；Go 无 None，显式错误是最贴近表达（A1） |

## 8. 测试设计（目标覆盖率 ≥85%，参照 13.6/13.7/13.8 的 92%+ 基线）

### 8.1 text_preprocessor_test.go（对齐 Python 38 用例）

| 测试组 | 用例（断言值全部从 Python 对拍基准推导） |
|--------|------|
| TextPreprocessor 接口 | 具体实现 Process 可调（Python __call__ 并入方法调用测试） |
| WhitespaceNormalizer | 正常文本不变 / 多空格→单空格 / 换行→空格 / Tab→空格 / 混合空白归一 / 两端空白 strip / 空文本直返 / 全角空格 U+3000 归一 / `\x1c` 补偿集命中 |
| URLEmailRemover | http URL 移除（`访问 https://example.com/page?q=1 查看`→`访问  查看`）/ www URL / 无链接不变 / 裸域名 example.com（COM 模式）/ example.cn/next / www.baidu.com / example.org#frag / 邮箱 user@example.com / 多点域名 a.b-c@sub.domain.org / 数字 TLD user@domain.123 / remove_urls=false 只删邮箱 / remove_emails=false 只删 URL / replacement 自定义 / 空文本直返 / URL+邮箱混合 |
| SpecialCharacterNormalizer | 正常中文不变 / 控制字符删除（`a\x00b\x1f`→`ab`）/ `hello!!!world`→`helloworld` / `中文。。。句子` 不动（protected CJK 标点）/ `###标题###` 不动（双标记符前瞻）/ `￥￥￥三个`→`三个` / `单一&符号` 不动（{2,} 需连续2+）/ `①②③序号` **保留**（\w 显式集回归——regexp2 原生 \w 会误删）/ `a-b-c正常` 不动 / `a@b@c` 不动 / mixed 中英文不动 / 链式替换插入序（`{'a':'b','b':'c'}`：`链式a→b→c`→`链式c→c→c`）/ chars_to_remove / 组合记号 a\u0301b 不删（No/Mn 差异回归）|
| PreprocessingPipeline | 空管线直通 / 单预处理器 / 多预处理器顺序应用 / AddPreprocessor 动态追加 / Len() |

### 8.2 text_chunker_test.go（对齐 Python 12 用例 + Go 专项）

| 测试组 | 用例 |
|--------|------|
| 构造 char 路径 | 默认参数（512/50/char）/ pipeline 空（无 preprocessOptions）/ preprocessOptions 全开 → pipeline.Len()==2 / 只开 whitespace → Len()==1 / 只开 url → Len()==1 / chunk_size 校验（-1→155100）|
| 构造 token 路径 | 无 embedModel → tiktoken fallback（真实 tiktoken，决策 10）/ embedModel 实现 TokenizerProvider → 提取（fake tokenizer）/ embedModel 无 tokenizer → fallback / MaxLengthProvider fake（MaxLength=256 < 512）→ 下调至 256 / MaxLength=0（未提供）→ 不下调 |
| ChunkDocuments | 预处理生效（`This   is   doc` → chunk 内无三连空格）/ 无预处理不变 / metadata 保留（source/author 透传 + 三键）/ 多文档 doc_id 分组 / 预处理后空文本 → 0 块 / inner 错误透传（fake inner 返回 error）|
| ChunkText | 返回显式错误（A1 回归）|
| Process | 局部覆写转发（预处理经 Process 入口仍生效——决策 A1 回归）|
| getChunker 路由单测 | char → CharChunker 类型断言 / token+tiktoken → TokenizerChunker 类型断言 / 下调日志字段（可选）|

**mock 策略**：embedModel 用手写 fake（实现/不实现 TokenizerProvider 两个变体 + MaxLengthProvider 变体）；inner 错误路径用手写 fake chunker；真实 CharChunker/TokenizerChunker 用于主路径（已交付轻量依赖）。中文正则样本全部从 Python 对拍基准推导（§8.1 表内值即基准）。

### 8.3 覆盖率缺口（决策 9）

`getChunker` tiktoken 加载失败分支（155102）不测——依赖 tiktoken 内部错误不可注入。预估包覆盖率仍 ≥85%（该分支 4 行）。

## 9. 实施顺序

1. `text_preprocessor.go`：接口 + 语义集常量 + 4 模式 + 三预处理器 + Pipeline（含测试）
2. `text_chunker.go`：TokenizerProvider + TextChunker 门面 + getChunker 路由（含测试）
3. `doc.go` 目录更新；`IMPLEMENTATION_PLAN.md` 13.9 ☐→✅（产出列追加摘要，既有行一字不动）
4. `go test -cover ./internal/agentcore/retrieval/indexing/processor/chunker/...` ≥85% 验证 + 既有 13.5-13.8 测试全绿

## 10. 验证点

- ✅ TextChunker(char) 路径：预处理 + CharChunker 分块 + metadata 三键齐全
- ✅ TextChunker(token) 路径：TokenizerProvider 提取 / tiktoken fallback / chunk_size 下调全链路
- ✅ 四个预处理器行为与 Python 对拍基准逐样本一致（含 ①②③ 保留、链式替换序、补偿空白集）
- ✅ ChunkText 显式错误 / Process 局部覆写路由正确
- ✅ registry 零改动（"text" 不注册）
- ✅ 包覆盖率 ≥85%，既有测试全绿
