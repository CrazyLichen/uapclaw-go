# 13.6 CharChunker + CharSplitter（带出）设计

> 对应 Python：`openjiuwen/core/retrieval/indexing/processor/chunker/char_chunker.py` + `text_splitter.py`（CharSplitter 部分带出，13.9 归属不变）
> 实施计划：领域十三 13.6（Chunker 层第二步）

## 背景与流程位置

```
【知识库建设链路】（文档入库）
用户上传文档 / 配置知识库
  └─ workflow: KnowledgeRetrievalComp._create_knowledge_bases()
       └─ SimpleKnowledgeBase.add_documents(documents)
            ├─ chunker.chunk_documents(documents)      ← 13.5 接口 ✅ + 【13.6 CharChunker 默认算法】
            │    └─ CharChunker.chunk_text
            │         └─ CharSplitter.split            ← 13.6 带出（13.5 决策 4 既定）
            └─ index_manager.build_index(chunks)       ← 13.2/13.3 ✅
                 └─ ComputeChunkEmbeddings             ← 13.4 ✅
```

13.6 的作用：

1. **默认分块算法落地**——registry 里第一个真实算法，`GetChunker("char")` 可真实跑通
2. **下游硬依赖解锁**——13.8 HybridChunker 默认 inner；13.9 TextChunker 组合单元；
   registry.go 两处 `⤴️ 13.6` 占位错误由本步解除
3. **检索粒度控制**——512 字符 / 50 重叠滑窗，知识检索效果基线
4. **溯源元数据生产**——doc_id/chunk_index/total_chunks/chunk_id（13.5 已实现）

## 已确认决策（3 项 + 章节顺序，2026-10-19）

| # | 决策点 | 结论 |
|---|--------|------|
| 0 | 章节顺序 | **按编号 13.6 → 13.7 → 13.8**，不换章节（13.7 的 tiktoken/pysbd 选型届时单独攻关） |
| 1 | TextSplitter 接口形态 | **`Split(doc *reranker.Document) ([]common.TextChunk, error)` 单一类型**。生产调用方只传 Document；TextChunk 入参路径仅测试覆盖，13.7 IndexSentenceSplitter 的 TextChunk→Document 转换留在实现内部，不进接口。error 为 13.7 预留（CharSplitter 恒 nil） |
| 2 | CharSplitter 构造语义 | **Python 原样：钳制不报错**。`NewCharSplitter` 返回单一值：size≤0→1、overlap→clamp [0, size-1]。与 BaseChunker"越界报错"风格相反是有意为之——真实路径 CharChunker 已过基类校验，钳制是防御层；Python 单独 `CharSplitter(100, 150)` 也不抛异常 |
| 3 | 空文本语义 | `ChunkText("")` → `[]`（Python `None`/`""` 用例并入，Go 无法表达 None） |

## Python 实现盘点（对齐依据）

| 元素 | Python 语义 |
|------|------------|
| `CharChunker.__init__` | `chunk_size=512, chunk_overlap=50`，调 super() 走基类校验（155100/155101），`**kwargs` 透传 |
| `CharChunker.chunk_text` | 空文本→`[]`；**每次调用内联新建** `CharSplitter(chunk_size, chunk_overlap)`；构造 `Document(text, metadata={})` → `split()` → 收集 `node.text`；`logger.info("Character chunking completed: generated %d chunks", len(chunks))` |
| `CharSplitter.__init__` | `None→默认 200/40`（注意与 CharChunker 的 512/50 是两套默认值）；`overlap = clamp(0, overlap, size-1)`；`size = max(1, size)`——钳制而非报错 |
| `CharSplitter.split` | 滑窗：`step = size-overlap`（`size≤overlap` 时退化 `step=size`）；`text[start:end]` 切片产出 `TextChunk{uuid, doc_id, metadata}`；metadata 原样传递 |
| `TextSplitter` ABC | 仅 `split(doc)` 一个抽象方法；`IndexSentenceSplitter` 留 13.7（依赖 splitter 包 + tokenizer） |
| 注册 | `__init__.py` 尾部 `register_chunker("char", CharChunker)` |

## 包结构

```
internal/agentcore/retrieval/indexing/processor/chunker/
├── doc.go              # 更新文件目录（新增两文件条目）
├── base.go             # 13.5 交付，不动
├── registry.go         # init() 注册 "char" + hybridFactory inner 缺省分支回填
├── char_chunker.go     # 新增：CharChunker + charFactory
└── text_splitter.go    # 新增：TextSplitter 接口 + CharSplitter + 默认值常量
```

- `DEFAULT_CHUNK_SIZE`/`DEFAULT_SAFE_ENCODE_MAX_LENGTH` 不在本步落（仅 13.7 IndexSentenceSplitter 消费，随 13.7）
- `IndexSentenceSplitter` 留 13.7（13.9 text_splitter 收尾时本文件才完整）

## 类型与签名

```go
// TextSplitter 文本分割器抽象接口（对齐 Python TextSplitter ABC）
type TextSplitter interface {
    // Split 分割文档为文本块列表。error 为 13.7 预留，CharSplitter 恒返回 nil。
    Split(doc *reranker.Document) ([]common.TextChunk, error)
}

// CharSplitter 基于字符长度的分割器，不依赖 tokenizer
type CharSplitter struct {
    ChunkSize    int
    ChunkOverlap int
}

// NewCharSplitter 构造并钳制参数（不报错，决策 2）
func NewCharSplitter(chunkSize, chunkOverlap int) *CharSplitter

// CharChunker 字符级分块器
type CharChunker struct{ *BaseChunker }

// NewCharChunker 构造（复用 13.5 选项系统，默认 512/50）
func NewCharChunker(opts ...ChunkerOption) (*CharChunker, error)
```

构造链：`NewChunkerOptions`（默认 512/50）→ `NewBaseChunker`（校验 155100/155101）→
`SetSelf(c)`（否则 ChunkDocuments 报 standalone 错，13.5 既定钩子）。

选 `opts ...ChunkerOption` 而非位置参数的理由：默认值统一走 13.5 选项系统；
charFactory 零成本包装（Go 无协变返回，注册时闭包适配一层）。

## 关键实现语义（4 个对齐要点）

1. **rune 切片（Go 特有陷阱）**：Python `text[start:end]` 按字符索引，Go 必须先
   `runes := []rune(doc.Text)` 再 `string(runes[start:end])`，否则 UTF-8 多字节
   字符（中文）被拦腰切断。配专项中文测试。
2. **0 值语义映射（不对称缺省）**：Python `size = chunk_size or 200`（0→默认 200），
   但 `overlap if chunk_overlap is not None else 40`（0 是合法显式值）。Go 映射：
   `chunkSize==0 → 200`；`chunkOverlap` 零值无 None 对应物，调用方显式传 40 得默认
   （doc 注释注明）。真实路径 CharChunker 总是显式传两值，不受影响。
3. **每次 ChunkText 内联新建 CharSplitter**：Python 原样保留（splitter 无状态，
   等价但保对齐；Python 测试断言了 `assert_called_once_with`）。
4. **Extra 吞掉 + 日志**：charFactory 忽略 `Extra`（对齐 Python `**kwargs` 透传后
   无消费即吞掉）；完成日志
   `logger.Info(logComponent).Int("chunk_count", n).Msg("character chunking completed")`
   （Python `%d` → 结构化字段，规则 3.3；chunker 包新增 `logComponent = logger.ComponentAgentCore`
   常量，对齐 indexer 层先例 chroma.go:263）。

## hybridFactory 回填（⤴️ 13.6 解除点）

```go
func init() {
    _ = RegisterChunker("char", func(opts ...ChunkerOption) (Chunker, error) {
        return NewCharChunker(opts...)
    }, false)
    _ = RegisterChunker("hybrid", hybridFactory, false)
}

func hybridFactory(opts ...ChunkerOption) (Chunker, error) {
    o := NewChunkerOptions(opts...)
    if o.InnerChunker == nil {
        // 回填：参数先过 CharChunker 构造校验（155100/155101），多余 Extra 吞掉（前向兼容）
        if _, err := NewCharChunker(opts...); err != nil {
            return nil, err
        }
    } else if len(o.Extra) > 0 {
        // 原报错不变（TypeError 语义）
    }
    return nil, fmt.Errorf("HybridChunker not implemented until 13.8")  // 本体仍留 13.8
}
```

行为变化：hybrid 无 inner + 非法参数 → 155100/155101；合法参数 → `not implemented
until 13.8`。registry_test.go 两处 `⤴️ 13.6` 占位断言（`not implemented until 13.6`）
同步改写为校验穿透断言。

## 测试映射

| Go 测试文件 | 覆盖内容 |
|------------|---------|
| char_chunker_test.go | 默认值 512/50；自定义 256/25；真实分块行为断言（Python mock 验证的调用参数 → Go 直接断言分块文本，不做类 patch 等价物）；空串→`[]`；ChunkText→ChunkDocuments→Process 全链（SetSelf 注入后）；hybrid inner 缺省走 CharChunker 参数校验穿透 |
| text_splitter_test.go | Python TestCharSplitter 7 用例全对齐：默认 200/40、自定义、overlap 越界钳制、负 overlap 钳制、size 最小值抬 1、短文本单块、长文本多块、metadata 保留；+ 中文 rune 专项（不切断多字节字符）；split nil doc 防御 |

覆盖率 ≥85%（规范 3 硬性要求）。

## 实施计划表回填约定（13.5 定稿沿用）

- 13.6 行：补产出描述 + ✅
- 13.9 行：⚠️ 表述更新为"text_splitter 的 CharSplitter 已由 13.6 带出，本步骤补
  IndexSentenceSplitter + chunking + text_preprocessor 收尾"
- 13.18 行：⤴️ 表述不变（SentenceSplitter 仍由 13.7 带出）
- **所有行原有内容一字不动，只追加**（用户强调"回填之前的内容，不能丢失"）

## 不做的事（YAGNI）

- 不实现 IndexSentenceSplitter（13.7）
- 不引入 tiktoken 依赖（13.7 选型）
- 不落 DEFAULT_CHUNK_SIZE / DEFAULT_SAFE_ENCODE_MAX_LENGTH 常量（13.7）
- 不实现 HybridChunker 本体（13.8，仅回填 inner 缺省分支的默认 inner 构造）
- 不建 Processor 接口（13.5 决策 2）
