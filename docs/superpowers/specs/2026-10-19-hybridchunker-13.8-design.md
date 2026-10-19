# HybridChunker（13.8）设计文档

- 日期：2026-10-19
- 计划条目：IMPLEMENTATION_PLAN.md 13.8（HybridChunker）
- Python 源：`openjiuwen/core/retrieval/indexing/processor/chunker/hybrid_chunker.py`（89 行）+ `chunker/__init__.py` 的 `_hybrid_factory`
- Go 目标包：`internal/agentcore/retrieval/indexing/processor/chunker/`

## 1. 背景与定位

HybridChunker 是混合分块器：命中 `no_split_when(doc)` 谓词的文档整体保留为单块（默认谓词把表格行/列 `metadata.source_type ∈ {"row","column"}` 视为单块，保护 Excel 语义完整性），其余文档委托 inner chunker（默认 CharChunker）正常分块。它解决知识库中 Word 长文 + Excel 表格混合文档的分块策略分裂问题——"hybrid" 指分块策略混合，与检索层的 hybrid search 无关。

Agent 会话流程位置（仅入库期被调用）：

```
AutoParser（13.11 未实现）→ ExcelParser（13.13 未实现，产出带 source_type 的 Document）
  → SimpleKnowledgeBase.add_documents（chunker 由外部注入）
  → HybridChunker.ChunkDocuments          ← ★ 13.8 本体
  → ComputeChunkEmbeddings（13.4 ✅）→ Chroma/MilvusIndexer.BuildIndex（13.2/13.3 ✅）
```

13.8 无未完成依赖（ExcelParser 是 source_type 生产者，但测试可手工构造 metadata 替代；13.9 TextChunker 与其平行无依赖）。

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | 章节顺序 | 直接做 13.8，不提前实现其他章节 | 13.13 体量大且可手工构造替代；13.9 平行无依赖 |
| 2 | inner 尺寸提取 | **DimsProvider 命名接口 + 构造函数内部一处断言**，断言失败 fallback 512/50 | Python `getattr(inner,"chunk_size",512)` 反射的 Go 等价物。否决固定 512/50（字段失真）；否决收窄参数为带 ChunkDims 的接口（能力从可选变强制，semantic chunker 类实现无法作 inner，且断言转移到所有调用点）；否决加进 Chunker 主接口（偏离三方法契约） |
| 3 | Process 动态分发 | **HybridChunker 局部覆写 Process**（三行转发到自身 ChunkDocuments） | BaseChunker.Process 静态解析无法路由到子类覆写的 ChunkDocuments（Go 嵌入是组合非继承）。用户选择不动 13.5 交付物的 self 路由；代价是未来每个覆写 ChunkDocuments 的子类都需手动覆写 Process（已记录，无编译期提示） |
| 4 | hybridFactory 回填 | **inner 缺省分支构造并保留**：`NewCharChunker(opts...)` 实例直接作为 inner，Extra 随 opts 透传被 CharChunker 忽略（对齐 Python `**params` 前向兼容） | 否决两段式（校验后丢弃再重建：构造两次、Extra 丢失） |
| 5 | 实体结构 | **嵌入 BaseChunker**（与 CharChunker/TokenizerChunker 同构） | 否决纯接口实现（同构性断裂、校验/字段/ChunkDims 手写重复、无法 nested hybrid） |

## 3. 文件布局与变更面

```
chunker/
├── doc.go                  # 更新：文件目录补 hybrid_chunker.go 条目
├── base.go                 # 追加：DimsProvider 接口 + ChunkDims() 方法（+12 行）
├── registry.go             # 回填：hybridFactory 尾部占位 → 真实构造（~15 行改动）
├── hybrid_chunker.go       # ★ 新文件：HybridChunker 实体
└── hybrid_chunker_test.go  # ★ 新测试文件
```

- 零新增外部依赖，纯包内闭环。
- registry_test.go 的 4 个占位断言测试改写为真实行为断言。
- IMPLEMENTATION_PLAN.md：13.8 ☐→✅；13.5 行内 ⤴️ 闭环说明。
- 不动 splitter 包、不动 13.9 归属文件（chunking.py / text_preprocessor.py）。

## 4. base.go 追加内容（13.5 交付物最小侵入）

```go
// DimsProvider 可选尺寸提供者（结构体区块，接口排在 BaseChunker 之前）。
//
// 嵌入 BaseChunker 的分块器自动满足本接口（方法提升），
// 未嵌入的纯接口实现不满足——对齐 Python getattr(inner, "chunk_size", 512)
// 取不到属性时走默认值的语义。
type DimsProvider interface {
    // ChunkDims 返回 (chunkSize, chunkOverlap)。
    ChunkDims() (int, int)
}
```

BaseChunker 追加导出方法：

```go
// ChunkDims 返回 (ChunkSize, ChunkOverlap)。使嵌入方满足 DimsProvider。
func (b *BaseChunker) ChunkDims() (int, int) {
    return b.ChunkSize, b.ChunkOverlap
}
```

纯增量，不改既有行为，13.5/13.6/13.7 既有测试不受影响。

## 5. hybrid_chunker.go 实体设计

### 5.1 结构体与构造

```go
// HybridChunker 混合分块器。
type HybridChunker struct {
    *BaseChunker                                  // 尺寸字段 + 校验 + ChunkText 路由能力
    inner       Chunker                           // 委托目标
    noSplitWhen func(*reranker.Document) bool     // nil → defaultNoSplit
}
```

构造签名（inner 必填，谓词可变参数）：

```go
func NewHybridChunker(inner Chunker, noSplitWhen ...func(*reranker.Document) bool) (*HybridChunker, error)
```

构造流程（一比一对齐 Python `__init__` L49-61）：

1. `inner == nil` → 错误 `inner chunker is required for HybridChunker`（Go 侧防御，Python 由类型标注保证）
2. 尺寸提取：`size, overlap := 512, 50`；`if p, ok := inner.(DimsProvider); ok { size, overlap = p.ChunkDims() }`
3. `NewBaseChunker(size, overlap)` 过 155100/155101 校验（inner 尺寸已合法时恒通过、fallback 512/50 也合法，此校验在 hybrid 路径实际不可失败，保留为构造结构对称，与 Python `super().__init__` 一致）
4. `noSplitWhen` 空切片 → nil → `defaultNoSplit`
5. `SetSelf(hybrid)` 注入——保持与 CharChunker/TokenizerChunker 构造模式一致。注意：对 hybrid 而言 self 路由实际不被消费（ChunkText 由 hybrid 自实现、ChunkDocuments 为完全覆写，二者均不经过 BaseChunker 默认路径），注入仅防御"直接调用 hybrid.BaseChunker.ChunkDocuments"这类非常规路径

### 5.2 默认谓词

```go
// defaultNoSplit 表格行/列视为单块（对齐 Python _default_no_split）。
func defaultNoSplit(doc *reranker.Document) bool {
    return doc.Metadata["source_type"] == "row" || doc.Metadata["source_type"] == "column"
}
```

注意：Go 侧 map 取值 nil 时 `==` 比较安全（metadata 为 nil map 或缺键均返回零值），等价 Python `(doc.metadata or {}).get("source_type") in ("row","column")`。

### 5.3 三方法实现

| 方法 | Python 行为 | Go 实现 |
|------|------------|---------|
| ChunkText | `return self._inner.chunk_text(text)` | `return h.inner.ChunkText(text)` 纯委托（不经过 BaseChunker 的 chunkTextOf/self 路由，hybrid 直接持有 inner 引用） |
| ChunkDocuments | 完全覆写（L66-86） | 见 5.4 |
| Process | 基类默认 → self 动态分发到覆写版 | **局部覆写**（决策 3）：`return h.ChunkDocuments(ctx, docs)` 三行转发 |

### 5.4 ChunkDocuments 核心逻辑（4 分支）

```go
for _, doc := range docs {
    if h.noSplitWhen(&doc) && strings.TrimSpace(doc.Text) != "" {
        uid := uuid.NewString()
        text := strings.TrimSpace(doc.Text)   // ★ Python doc.text.strip()
        meta := mergeChunkMetadata(doc.Metadata, 0, 1, uid)
        chunks = append(chunks, common.TextChunk{ID: uid, Text: text, DocID: doc.ID, Metadata: meta})
    } else {
        innerChunks, err := h.inner.ChunkDocuments(ctx, []reranker.Document{doc})  // 单元素委托
        if err != nil { return nil, err }
        chunks = append(chunks, innerChunks...)
    }
}
```

**两个易漏细节（Python 对齐审查要点）：**

1. 谓词命中的前提是文本非空——`(doc.text or "").strip()`：即使谓词命中，空文本/纯空白文档也走 inner 委托（Python 里交给 CharChunker 后产出 0 块，Go 同）。
2. 单块文本是 **strip 后的**，而 inner 委托路径的文本**不 strip**（原样进 inner）——两条路径空白处理不对称，这是 Python 原样行为，必须保留。

### 5.5 metadata 合并提取

base.go 原 ChunkDocuments 内联的"复制 doc.Metadata + chunk_index/total_chunks/chunk_id 三键"逻辑（L114-120）提取为私有函数 `mergeChunkMetadata`，base.go 原 ChunkDocuments 与 hybrid 共用（三处同型代码不留重复，行为不变，base.go 多动约 10 行）。

## 6. registry.go 回填（决策 4 落地）

```go
func hybridFactory(opts ...ChunkerOption) (Chunker, error) {
    o := NewChunkerOptions(opts...)
    var inner Chunker
    if o.InnerChunker == nil {
        c, err := NewCharChunker(opts...)   // 构造并保留（含 Extra 透传吞掉）
        if err != nil { return nil, err }
        inner = c
    } else {
        if len(o.Extra) > 0 {               // 多余 kwargs 报错（TypeError 语义）
            return nil, fmt.Errorf("Unknown kwargs for 'hybrid' chunker: %s", ...)
        }
        inner = o.InnerChunker
    }
    return NewHybridChunker(inner, o.NoSplitWhen)
}
```

对齐要点：

- **Extra 透传**：`NewCharChunker(opts...)` 接收完整 opts（含 Extra），其构造忽略 Extra——对齐 Python `CharChunker(chunk_size, chunk_overlap, **params)` 收下剩余 params 不消费。
- **`WithInnerChunker(nil)` 显式 nil**：走缺省分支（nil == nil），与既有测试 `TestHybridFactory_inner显式nil走缺省分支` 语义一致。
- **NoSplitWhen 透传**：`o.NoSplitWhen` 为 nil 时可变参数空 → 默认谓词——对齐 Python `no_split_when or _default_no_split`。
- **Python 的 `isinstance(inner_chunker, Chunker)` 校验**：Go 由参数类型静态保证，无需运行时断言（13.5 设计已确立，延续）。

## 7. 错误处理与日志

| 场景 | Python | Go |
|------|--------|-----|
| inner 为 nil（构造） | 类型系统保证，运行时无此路径 | `fmt.Errorf("inner chunker is required for HybridChunker")`（Go 新增防御） |
| inner 分块失败（ChunkDocuments 委托） | 异常上抛 | 透传 inner 的 error，立即中断返回 |
| 尺寸校验失败（构造） | build_error 155100/155101 | NewBaseChunker 透传（理论不可触发，见 5.1 说明） |

**日志：hybrid_chunker.py 全文件 0 条 logger 调用**——HybridChunker 不加任何日志（Python 无日志则 Go 不加，不自行发明）。委托路径中 inner 自己的日志（如 CharChunker 的 chunk_count 完成日志）自然产生。

## 8. 测试设计（目标覆盖率 ≥85%，参照 13.6/13.7 的 92%+ 基线）

| 测试组 | 用例 |
|--------|------|
| 构造 | 默认尺寸继承（inner=CharChunker(2048,100) → hybrid.ChunkSize==2048）；纯接口实现 fallback 512/50（伪造不嵌 BaseChunker 的 fake）；inner==nil 报错；noSplitWhen 可变参数空 → 默认谓词生效 |
| defaultNoSplit | source_type=row / column / 其他 / metadata 为 nil / metadata 无 source_type 键 |
| ChunkText | 纯委托 inner（同输入同输出断言） |
| ChunkDocuments | ① 谓词命中+文本非空 → 1 块且 text 为 strip 后值、metadata 含 chunk_index:0/total_chunks:1/chunk_id ② 谓词命中+纯空白文本 → 走 inner（产出 0 块）③ 谓词不命中 → inner 正常分块（多块+inner 的 metadata 规范）④ 空 metadata 的 doc ⑤ 混合文档列表（row+长文，验证分支交错） |
| Process | 局部覆写路由正确：谓词命中的 doc 经 Process 入口仍单块（决策 3 回归测试） |
| registry 集成 | `GetChunker("hybrid")` 默认 → inner 为 CharChunker(512,50) + 默认谓词；`GetChunker("hybrid", WithChunkSize(2048))` → 尺寸穿透；WithInnerChunker + WithNoSplitWhen 全参路径 |
| 改写既有占位测试 | registry_test.go 的 4 个 `not implemented until 13.8` 断言 → 真实行为断言（inner 缺省默认构造成功、inner 注入构造成功、显式 nil 走缺省、多余参数仍报错——最后一个断言目标不变仅名称微调） |
| base.go 增量测试 | ChunkDims() 返回值正确 |

**mock 策略**：inner 用真实 CharChunker（非 mock——已交付轻量依赖）+ 一个手写 fake（记录调用、可控返回错误）覆盖错误传播路径。metadata 断言逐键核对（chunk_index/total_chunks/chunk_id 三键必须在）。

## 9. 实施顺序

1. base.go：DimsProvider + ChunkDims + mergeChunkMetadata 提取（含增量测试）
2. hybrid_chunker.go + hybrid_chunker_test.go
3. registry.go hybridFactory 回填 + registry_test.go 4 个占位测试改写
4. doc.go 目录更新；IMPLEMENTATION_PLAN.md：13.8 状态 ☐→✅（产出栏补 `dimsProvider 断言+局部覆写 Process+工厂构造并保留` 摘要），13.5 行追加 `13.8 已回填 hybridFactory ⤴️` 闭环说明
5. `go test -cover ./internal/agentcore/retrieval/indexing/processor/chunker/...` ≥85% 验证

## 10. 验证点

- ✅ 谓词命中（表格行/列）文档整体单块，metadata 规范三键齐全
- ✅ 非命中文档经 inner 正常分块，与直接使用 inner 行为一致
- ✅ `GetChunker("hybrid")` / `GetChunker("hybrid", WithInnerChunker(...), WithNoSplitWhen(...))` 全路径可用
- ✅ Process 入口不绕过谓词判定（决策 3 回归）
- ✅ 包整体覆盖率 ≥85%，既有 13.5/13.6/13.7 测试全绿
