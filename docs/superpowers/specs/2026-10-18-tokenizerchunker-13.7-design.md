# 13.7 TokenizerChunker 设计文档

- 日期：2026-10-18
- 状态：已与用户逐节确认定稿
- 对应计划：IMPLEMENTATION_PLAN.md 领域十三 13.7（连带带出 splitter 包，13.18 归属编号不变）
- Python 参考路径：
  - `openjiuwen/core/retrieval/indexing/processor/chunker/tokenizer_chunker.py`
  - `openjiuwen/core/retrieval/indexing/processor/chunker/text_splitter.py`（IndexSentenceSplitter 部分）
  - `openjiuwen/core/retrieval/indexing/processor/splitter/base.py`
  - `openjiuwen/core/retrieval/indexing/processor/splitter/splitter.py`

## 1. 流程位置与作用

知识库入库管线（与 agent 会话主循环解耦）：

```
Parser 解析（13.10-13.16，未实现）→ Document
  → Chunker 分块（13.5-13.9，本节所在层）
      13.5 Chunker 接口+registry      ✅
      13.6 CharChunker（字符滑窗）     ✅
      13.7 TokenizerChunker（本节）    ← 句子边界 + token 预算
      13.8 HybridChunker（表格感知）   ☐
  → ComputeChunkEmbeddings 嵌入（13.4 ✅）→ VectorStore 入库（13.1-13.3 ✅）
```

TokenizerChunker 相对 CharChunker（纯字符滑窗、句子可能被拦腰切断）的三项增强：

1. **句级切分**：sentencizer 按中/英文标点分句（语言自动检测，中文占比 ≥10% 判 zh）
2. **token 预算打包**：按 tokenizer 的 token 数（而非字符数）把多个句子打包成块，保证每块不超过 embedding 模型最大序列长度
3. **长句无损子切分**：单句超预算时按 token 滑窗子切分（decode 回文本，不截断内容）；块间 overlap 通过句子前缀携带实现

注：Python 侧 TokenizerChunker 未注册到 registry（`__init__.py` 只注册 char/hybrid）。Go 侧主动增强：注册 "tokenizer"（见 §6 偏差 5）。

## 2. 已确认决策（brainstorming 定稿）

| # | 决策 | 选择 |
|---|------|------|
| 1 | 分句器 | **sentencizer**（`github.com/sentencizer/sentencizer`，pysbd 启发、zh/en/ja/he/lt/ru 六语言、零传递依赖）；`_detect_chinese` 的 zh/en 路由自写保留（rune 统计替换 numpy）；`Segmenter` 每文档按 `clean=False` 语义重建（默认不开 `Clean()` 选项，天然对齐）；span 直接用 `TextSpans()` 产出 |
| 2 | tokenizer 形态 | **接口三态 + 内置 TiktokenTokenizer 适配器**：`Tokenizer{Encode}` 必选、`DecodeTokenizer{+Decode}` 决定长句可否子切分、`MaxLengthProvider{MaxLength()}` 表达 HF model_max_length 探测；nil → tiktoken cl100k_base fallback；Encode 失败**直接上抛 error**（Go 无 truncation 参数，裸重试场景不存在）；decode 失败跳过窗口（对齐 Python continue） |
| 2c | DEFAULT_SAFE_ENCODE_MAX_LENGTH=65536 | **常量落地仅作文档锚点**（Go 接口无 max_length 参数概念，不做 TruncatingTokenizer 装饰器） |
| 3a | splitter_config 死参数 | **保留参数 + 归一化 + 忽略**（签名一比一对齐，归一化逻辑照写，注释标注"对齐 Python 死参数"；归一化结果无消费者——Python 源码即如此） |
| 4 | tiktoken 失败链 | **完整保留**：Get 失败（编译期嵌入 BPE 实际不可达）→ Warn → strings.Fields 空格分词 proxy（dec=nil） |
| 5 | 章节顺序 | **维持编号 13.7 先做**，13.8 不受影响 |

选型过程备注：用户提示存在 gopysbd/sentencizer 方向的库，实测确认 `pemistahl/gopysbd` 已 404 不存在；`github.com/sentencizer/sentencizer v0.3.0` 存在且实测达标（§10）。neurosnap/sentences（17 种欧洲语言无中文）、go-ego/gse（分词非分句）排除。

## 3. 包布局与依赖方向

```
internal/agentcore/retrieval/indexing/processor/
├── chunker/                        # 现有包（13.5/13.6 已交付部分一字不动）
│   ├── base.go                     # 不动
│   ├── registry.go                 # init() 追加 "tokenizer" 注册行（唯一改动）
│   ├── text_splitter.go            # 追加：Tokenizer/DecodeTokenizer/MaxLengthProvider 接口
│   │                               #   + TiktokenTokenizer 适配器 + IndexSentenceSplitter
│   │                               #   + DEFAULT_CHUNK_SIZE/DEFAULT_SAFE_ENCODE_MAX_LENGTH 常量
│   │                               #   + resolveTokenizers/resolveChunkSize
│   ├── char_chunker.go             # 不动
│   └── tokenizer_chunker.go        # 新增：TokenizerChunker
└── splitter/                       # 新包（13.7 带出，13.18 归属编号不变）
    ├── doc.go                      # 包文档
    ├── base.go                     # Splitter 接口 + BaseSplitter + ChunkSpan
    │                               #   + GetNodesFromDocuments/SplitText/GetTokenCount
    └── splitter.go                 # SentenceSplitter（detectChinese + 打包算法）
```

依赖方向：`chunker → splitter`（单向，对齐 Python；splitter 不 import chunker，无循环）。

职责边界（对齐 Python 语义）：

- `splitter` 纯算法层：收已解析的 `func(string)([]int, error)` enc / `func([]int)(string, error)` dec（可空）函数，不含接口探测
- `chunker` 策略解析层：IndexSentenceSplitter 负责接口三态解析 → 函数对传给 SentenceSplitter；TokenizerChunker 是最外层门面

## 4. chunker 包设计

### 4.1 接口三态（text_splitter.go 追加）

```go
// Tokenizer token 编码器最小接口（Encode 必选）。
// 对齐 Python _resolve_tokenizer 分支②③（tokenize-only / callable）。
type Tokenizer interface {
    Encode(text string) ([]int, error)
}

// DecodeTokenizer 带 Decode 的完整 token 化器。
// 对齐分支①（HF encode+decode）：实现者获得长句子切分能力。
type DecodeTokenizer interface {
    Tokenizer
    Decode(ids []int) (string, error)
}

// MaxLengthProvider 可选长度上限提供者。
// 对齐 _max_length 的 4 属性 hasattr 探测（model_max_length 等），简化为单方法
// ——由实现者决定返回哪个语义的值（0 视为未提供）。
type MaxLengthProvider interface {
    MaxLength() int
}
```

Python 四分支映射：分支①（encode+decode）→ 实现 `DecodeTokenizer`；②③（tokenize-only / callable）→ 只实现 `Tokenizer`（**Go 类型系统天然合并②③**，接口本身就是 callable 语义）；④（nil）→ 内置 fallback。

### 4.2 TiktokenTokenizer 适配器

```go
// TiktokenTokenizer tiktoken-go Codec 适配器（分支④ nil → cl100k_base fallback）。
// Encode/Decode 薄转发 + []uint↔[]int 转换。
// 不实现 MaxLengthProvider（偏差 1：max_token_value 不模拟）。
type TiktokenTokenizer struct{ codec tokenizer.Codec }
```

- `NewTiktokenTokenizer()`：`tokenizer.Get(tokenizer.Cl100kBase)`；失败进失败链（§4.3 步骤 5）
- 依赖 `github.com/tiktoken-go/tokenizer v0.8.0`（go.mod 已有，context_engine/token 在用）
- safe_encode 的空白→`[]` 语义在 resolve 层统一处理，适配器保持薄转发

### 4.3 resolveTokenizers（_resolve_tokenizer 的 Go 等价）

```go
// 入参：nil 或任意 Tokenizer 实现
// 返回：(enc, dec, maxLen)
func resolveTokenizers(t Tokenizer) (enc func(string) ([]int, error), dec func([]int) (string, error), maxLen int)
```

执行顺序严格对齐 Python：

| 步骤 | Python | Go |
|---|---|---|
| 1 | `_max_length(tokenizer)` 4 属性 hasattr | `t.(MaxLengthProvider)` 断言成功 → maxLen |
| 2 | encode+decode → safe_encode/safe_decode | `t.(DecodeTokenizer)` 成功 → wrapSafeEncode(t.Encode) + t.Decode |
| 3 | tokenize-only | 只实现 Tokenizer → wrapSafeEncode，dec=nil |
| 4 | callable | 并入步骤 3（类型系统合并） |
| 5 | nil → lazy import tiktoken | `NewTiktokenTokenizer()`：成功→enc/dec 都给；失败→Warn→strings.Fields proxy（每词一个"token"，dec=nil） |

**wrapSafeEncode**：`strings.TrimSpace(text)==""` → `[]int{}, nil`（对齐 `if not (text and text.strip()): return []`）；其余直调 `t.Encode(text)`，error 上抛（决策 2b）。

### 4.4 resolveChunkSize（_resolve_chunk_size 一比一）

```
chunkSize==0（未设）&& maxLen>0 → maxLen
chunkSize>0 && maxLen>0         → min(chunkSize, maxLen)
否则                             → chunkSize（0 则 DEFAULT_CHUNK_SIZE=200）
```

Go 无 None：`chunkSize int` 用 0 表示未设。`NewTokenizerChunker` 直接构造参数风格（不走 GetChunker 的 512 默认，见 §4.6）。

### 4.5 IndexSentenceSplitter（text_splitter.go 追加）

```go
type IndexSentenceSplitter struct {
    splitter *splitter.SentenceSplitter
}
```

`NewIndexSentenceSplitter(tokenizer Tokenizer, chunkSize, chunkOverlap int, splitterConfig map[string]any, language string)`，执行顺序严格对齐 Python `__init__`：

1. splitterConfig 非 map 归一化 `{"paragraph_separator": "\n"}` → **赋值局部变量后丢弃**（偏差 3a：Python 死参数，签名+归一化保留、无消费者）
2. `resolveTokenizers(tokenizer)` → (enc, dec, maxLen)
3. `resolveChunkSize(chunkSize, maxLen)` → 最终 chunkSize
4. `splitter.NewSentenceSplitter(SplitterParams{...ChunkSize: 最终值, ChunkOverlap: chunkOverlap or chunkSize/5, Enc: enc, Dec: dec, Lan: language})`

⚠️ `chunkOverlap or chunkSize/5`：overlap==0 → chunkSize/5（Python `or` 语义，0 是 falsy）。与 CharSplitter 13.6 的"0 是合法显式值"相反——**两个组件语义不同是有意为之**，Python 源码即如此（`chunk_overlap or chunk_size // 5` vs `chunk_overlap if chunk_overlap is not None else ...`）。

**Split 方法**（实现 13.6 定型的 TextSplitter 接口）：入参 `*reranker.Document`（Python 的 TextChunk→Document isinstance 分支消失——Go 入参恒为 Document）；返回 `self.splitter.GetNodesFromDocuments([]reranker.Document{*doc})`。

### 4.6 TokenizerChunker（tokenizer_chunker.go 新文件）

```go
type TokenizerChunker struct {
    *BaseChunker                    // 嵌入获得 ChunkDocuments/Process 默认实现
    splitter *IndexSentenceSplitter
}
```

`NewTokenizerChunker(chunkSize, chunkOverlap int, tokenizer Tokenizer, language string, splitterConfig map[string]any)`：
1. `NewBaseChunker(chunkSize, chunkOverlap)` — 基类校验（155100/155101）
2. `NewIndexSentenceSplitter(...)` — 解析链
3. `SetSelf` 注入

**ChunkText**：`text==""` → `[]string{}, nil`；构造 `&reranker.Document{Text, Metadata: {}}` → `splitter.Split(doc)` → 收集 `node.Text` → 完成日志 `"Token chunking completed"`（`.Int("chunk_count", n)`）。

### 4.7 registry 注册（主动增强）

`registry.go` 的 `init()` 追加：

```go
_ = RegisterChunker("tokenizer", func(opts ...ChunkerOption) (Chunker, error) {
    // 从 ChunkerOptions 取 ChunkSize/ChunkOverlap；
    // tokenizer/language/splitter_config 从 Extra 提取（key: "tokenizer"/"language"/"splitter_config"），
    // 类型断言失败返回 fmt.Errorf
}, false)
```

⚠️ 与 Python 差异：Python 不注册 TokenizerChunker（SDK 直接实例化）。Go 侧主动增强注册，使 `GetChunker("tokenizer", ...)` 可用（配置驱动场景完整性）。设计文档即此标注。

## 5. splitter 包设计

### 5.1 base.go — Splitter 基类

```go
// Splitter 文本分割器抽象接口。
type Splitter interface {
    // SplitDoc 分割文档文本，返回分句片段列表。
    // 对齐 Python __call__ 抽象方法。
    SplitDoc(doc string) ([]ChunkSpan, error)
}

// ChunkSpan 分句片段（文本 + 原文坐标）。
type ChunkSpan struct {
    Text  string
    Start int
    End   int
}

// BaseSplitter 基类适配器（构造校验 + tokenizer 函数 + 共享方法）。
type BaseSplitter struct {
    ChunkSize    int
    ChunkOverlap int
    tokenizerEnc func(string) ([]int, error)
    tokenizerDec func([]int) (string, error)  // 可为 nil
}
```

**NewBaseSplitter 构造校验**（一比一对齐）：
- size≤0 → `exception.BuildError(StatusRetrievalIndexingChunkSizeInvalid)`（155100）
- overlap<0 / overlap≥size → `StatusRetrievalIndexingChunkOverlapInvalid`（155101）
- ⚠️ Python 的 tokenizer 校验（无 encode 且不可调用→155102）**不可达省略**：Go 侧 enc 函数由 chunker 包 resolve 后传入，接口静态保证必为函数（类型系统吸收这层防御）

**GetNodesFromDocuments**（一比一）：空文档（nil 或 Text==""）→ Warn `"Skipping empty document"` + 跳过；`SplitDoc(doc.Text)` → 每 span 调 `common.NewTextChunkFromDocument(&doc, span.Text, "")`；汇总日志 `"Generated %d text chunks from %d documents"`。

**SplitText / GetTokenCount** 照搬。GetTokenCount 的 enc==nil 按字符计数分支保留为防御性（resolve 层保证 enc 恒非 nil）。

### 5.2 splitter.go — SentenceSplitter

```go
type SentenceSplitter struct {
    *BaseSplitter
    defaultLan string                  // "auto"→""，其他原样透传 sentencizer
    seg        *sentencizer.Segmenter  // 每文档重建（对齐 Python self.seg）
}
```

**detectChinese(text, threshold=0.1)**（静态函数）：
- rune 遍历统计 CJK（0x4E00-0x9FFF）；**省略 numpy UTF-32 向量化与 5000 万字符分块早退**（numpy 特有内存优化，Go 单循环足够快）；**保留语义早退**：计满 `threshold*total` 即返回 zh
- 标点启发 fallback 照搬：`？`>`?` 且 `！`>`!` → zh

**SplitDoc(doc)**（`__call__` 一比一）：

```
空文本（strip 后空）→ []
lan := defaultLan 或 detectChinese(doc)
seg := sentencizer.NewSegmenter(lan)   // 每文档重建；默认无 Clean()（clean=False 对齐）
spans := seg.TextSpans(doc)            // 偏差 2：原生产出，find 恢复省略
  → 过滤空白句子 + 计算 tokenLen（调 enc）
打包循环（严格对齐）：
  单句 tokenLen > chunkSize：
    flush 缓冲
    dec != nil → splitLongSegment 子切分窗口
    否则       → 整句保留一块
  短句累加：
    curTokenCount + sentLen <= chunkSize → 入缓冲
    否则 → flush + 当前句留作下块开头（overlap 修复语义）
尾部 flush
完成日志 "Computed the following sentence-level chunks: %d chunks"
```

**splitLongSegment**（一比一）：整句 encode（空→整句单块）→ `step = max(1, chunkSize-chunkOverlap)` 滑窗 → 每窗口 decode（失败或空白 → 跳过，对齐 safe_decode→""→continue）→ span 沿用父句坐标 → 全空则整句 fallback。

**flush**（一比一）：无分隔符拼接（`strings.Join(s, "")`）+ span 取首尾 + overlap 尾缀（reversed 累加 ≤ overlap 的句子前缀、倒序累加正序放回——Go 切片反转实现）。

### 5.3 并发说明

`SentenceSplitter` 的 `seg` 字段每文档覆盖（对齐 Python self.seg），非并发安全——与 Python 一致，文档标注"非并发安全，按文档使用"。

## 6. 有意偏差清单（全部经用户确认）

| # | 偏差 | 理由 |
|---|------|------|
| 1 | max_token_value 不模拟 | Python tiktoken fallback 把词表大小（100277）当 max_length（无意识行为）；Go Codec 无该属性，fallback maxLen=0 → 未传 chunkSize 用 DEFAULT_CHUNK_SIZE=200 |
| 2 | find-span 恢复省略 | sentencizer TextSpans 原生产出 (start, end, sentence)；Python 的 find 循环 + used_spans 去重 + "Span recovery failed" warning 不可达；死字段 `_span_recovery_failures` 一并省略 |
| 3a | splitter_config 保留但归一化后忽略 | Python 死参数（归一化结果无消费者）；签名对齐优先，用户迁移无感 |
| 4 | tiktoken 失败链完整保留 | Get 失败实际不可达（编译期嵌入），但保留 Warn + strings.Fields proxy 换 100% 行为对齐 |
| 5 | "tokenizer" 注册为主动增强 | Python 不注册（SDK 直接实例化）；Go 侧注册使 GetChunker("tokenizer") 可用 |
| 6 | 155102 tokenizer 校验省略 | Go enc 函数静态保证必为函数（类型系统吸收 hasattr 防御） |
| 7 | numpy 分块统计省略（语义早退保留） | numpy 特有内存优化；Go rune 单循环足够快 |
| 8 | 两处 Python 日志消失 | "Failed to get max length"（hasattr 探测不存在）、"Span recovery failed"（偏差 2）——doc.go 注释标注 |

## 7. 错误与日志对照

### 7.1 错误风格（沿用 13.5/13.6 两类约定）

| 场景 | 风格 |
|---|---|
| BaseSplitter 构造校验 155100/155101 | `exception.BuildError` |
| encode 运行时失败 | 透传原始 error（决策 2b） |
| registry "tokenizer" 工厂 Extra 类型断言失败 | `fmt.Errorf`（registry 系原生异常风格） |

### 7.2 日志对照清单

| Python 日志点 | Go 落点 |
|---|---|
| info "Token chunking completed: generated %d chunks" | TokenizerChunker.ChunkText 尾部 `.Int("chunk_count", n)` |
| warning "Failed to load tiktoken fallback tokenizer: %s" | resolveTokenizers 分支④失败链 `.Err(err)` |
| info "Computed the following sentence-level chunks: %d chunks" | SentenceSplitter.SplitDoc 尾部 |
| warning "Skipping empty document: %s" | GetNodesFromDocuments |
| info "Generated %d text chunks from %d documents" | GetNodesFromDocuments 尾部 `.Int("chunk_count", n).Int("doc_count", m)` |

## 8. 测试策略（覆盖率 ≥85%）

**直接测（无 build tag）**：
- fakeTokenizer（encode-only）/ fakeDecodeTokenizer（可注入 decode 失败）/ fakeMaxLengthProvider——三 fake 覆盖接口三态
- sentencizer 真实调用（中/英/混排/空文本/省略号引号边界）——纯 Go 零外部依赖
- tiktoken-go 真实调用（cl100k_base 编解码往返）——编译期嵌入 BPE
- detectChinese 表驱动（纯中文/纯英文/10% 阈值边界/标点启发 fallback）
- 打包循环：多短句打包、溢出 flush、overlap 尾缀、长句 splitLongSegment（decode 失败跳过、全空白窗口 fallback、step=1 退化）
- resolveTokenizers 四分支 + resolveChunkSize 三分支
- registry "tokenizer" 注册可获取
- GetNodesFromDocuments 空文档跳过
- BaseSplitter 构造校验错误码（155100/155101）

**fake 设计**：fakeTokenizer.Encode 按 1 字符=1 token 简化，token 预算断言可手算；真实 tiktoken 用例断言往返一致性（对齐 13.6"含 uuid 产出断言确定性行为"风格）。

## 9. 计划表回填策略（遵守"回填不丢失"约定）

- **13.1-13.6 已完成行一字不动**
- 13.7 完成后：`☐ → ✅`，产出列更新为实际交付
- **13.18 一并标 ✅**（产出列注明"全部由 13.7 带出，本步骤无剩余"——Python splitter 目录仅 base.py/splitter.py/空 __init__.py，13.7 带出后无剩余内容）
- 13.9 行描述同步微调（text_splitter 收尾范围缩小，仅剩 chunking.py + text_preprocessor.py）
- doc.go 两个包新增/更新条目按规则 1 同步维护

## 10. sentencizer 选型实测记录（2026-10-18）

| 场景 | 结果 |
|---|---|
| 中文基础分句（。！？） | ✅ |
| 中英混排（zh 模式下英文句号） | ✅ |
| 英文缩写 Dr./Mr./p.m./U.S. + 小数 3.5/$3.50 | ✅ |
| 省略号 ……、引号内 ！？ 不切分 | ✅ |
| TextSpans() 原生产出 (start, end, sentence) | ✅ |
| 换行段落切分 | ✅ |
| 空文本 → nil | ✅ |
| 性能：1400 字符中文 ~7.4ms / 60 字符 ~0.3ms / 1.4 万字符 ~500ms | 入库侧一次性成本，单文档常见规模可接受 |

版本 v0.3.0（v0.1.0→v0.3.0 共 10 版本），零传递依赖，默认行为对齐 pysbd `clean=False`（Clean() 选项对应 clean=True，不开启）。
