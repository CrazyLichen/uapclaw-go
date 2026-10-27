# 13.22 读取侧数据模型（TripleBeam + TripleMemory + MultiKBRetrievalResult）设计文档

- 日期：2026-10-28
- 计划条目：IMPLEMENTATION_PLAN.md 13.22（读取侧数据模型——TripleBeam + TripleMemory + MultiKBRetrievalResult）
- Python 源：`openjiuwen/core/retrieval/common/triple_beam.py`（36 行）+ `triple_memory.py`（45 行）+ `retrieval_result.py`（MultiKBRetrievalResult 类，~10 行）
- Python 测试基线：**0 专属用例**（tests/unit_tests/core/retrieval/common/ 无 test_triple_beam.py / test_triple_memory.py；test_retrieval_result.py 只测 SearchResult/RetrievalResult 两类——Go 用例全部为增强型）
- Go 目标：`internal/agentcore/retrieval/common/`（既有包，新增 2 文件 + 2 测试 + result.go 追加 + doc.go 修订）
- 前置依赖（全部就绪）：RetrievalResult（13.19 ✅，result.go 已交付）；common 包现覆盖率 91.8%（13.17 交付基线）
- 换序结论（2026-10-28 拍板）：**方案 1 按编号，13.22 单独先行**——三消费方（13.23 Beam/13.25 Memory/13.27 MultiKB）三向发散，并入 13.23 会让另两节前置悬空；13.23（813 行）开工前先钉死数据地基。13.24 QueryRewriter 域内零消费（核实：agentic_retriever.py 不 import，自带内联 _rewrite），保持编号序但保留届时按需插队当缓冲的弹性

## 1. 背景与定位

领域 13 读取侧第四节，纯数据容器三件套（零 IO、零 logger、零错误码），为剩余三节提供共同数据地基。

Agent 会话流程位置（不在 DeepAgent ReAct 主循环内，工作流组件链入口）：

```
用户查询 → DeepAgent / 工作流图
  └→ [工作流组件] KnowledgeRetrievalComponent (8.25 ⤴️ 依赖 13.26-13.28)
      └→ retrieve_multi_kb_with_source (13.27，多 KB 并行检索聚合)
          ├─ 产出 []MultiKBRetrievalResult ★③
          │    （8.25 _format_output 只取 r.text 拼 context 喂下游 LLM）
          ├→ SimpleKnowledgeBase.retrieve (13.27，按 index_type 路由
          │    vector/bm25/hybrid → 13.20/13.21 已交付 ✅)
          └→ GraphKnowledgeBase.retrieve (13.28) → GraphRetriever (13.23)
                ├─ retrieve → graph_expansion
                │    └→ TripleBeamSearch.beam_search → []TripleBeam ★①
                │         （beam 打分/展开/防环去重/列对齐抽取）
                └─ （retrieval_config.agentic 时外包）
                    AgenticRetriever (13.25) 多跳循环
                      └→ TripleMemory ★②
                           （每轮 _read 抽事实 → 去重累积 →
                            triples_str 注入 _rewrite 提示词 →
                            终轮 memory.memory 传 _link_passages 回链）
```

三类型作用：

| 类型 | 角色 | 核心机制 |
|------|------|---------|
| ① TripleBeam | GraphRetriever beam search 的**路径载体**（13.23 消费） | 一条 beam = 候选三元组路径 `[]RetrievalResult` + 累积分数；构造时建 existTriples text 集合使 `x in beam` O(1) 判重（展开防环）；支持负索引（生产用 `beam[-1]` 取尾）和列对齐访问（`beam[col]`，graph_expansion 按位置抽取） |
| ② TripleMemory | AgenticRetriever 多跳迭代的**事实记忆**（13.25 消费） | includedTriples 按小写化 join 字符串跨轮去重；memory 保序累积；TriplesStr 格式化为 `(s p o)` 多行注入 _REWRITE_PROMPT 判断"知识是否足够、要不要续问" |
| ③ MultiKBRetrievalResult | 多 KB 聚合检索的**输出契约**（13.27 产出 / 8.25 消费） | text 聚合键 + 融合 score + 保真 raw_score/raw_score_scaled + kb_ids 多源归并——读取管线最末端的出口模型 |

三元组元素流转链（讨论期间核实，TripleFact 类型决策依据）：读取侧三元组从头到尾是 **JSON 字符串**形态——13.28 写入 `metadata["triple"] = json.dumps([s,p,o])`（graph_knowledge_base.py L153）→ 13.23 `json.loads(beam[-1].metadata["triple"])`（graph_retriever.py L176/L213）→ 13.25 `_read` LLM 输出 `tuple(map(str, triple))` 且 `len(triple) == 3` 过滤（agentic_retriever.py L411-412）→ `memory.batch_extend_memory(triples)` → `_link_triples`/`_link_passages` `" ".join(map(str, triple))` 拼回查询串。

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | Triples()/Memory() 返回语义 | **C2 浅拷贝密封**（slices.Clone）：消费方 `append(beam.Triples(), t)` 随便写不伤内部状态；beam 长度 ≤ max_length（典型 2）拷贝成本可忽略；元素 Metadata map 仍共享（Python 引用列表语义一致，浅拷贝不算新偏差）。归档偏差 §8-1 | 否决 C1 返回内部引用（字面复刻零偏差，但 Go append 在空闲容量时静默写穿 beam——Python list concat 恒新建无此问题，隐性脚枪留给 13.23 人工规避不可取；Python property 无 setter 有隐式防护，C2 是显式化）；否决 C3 WithAppended 新 beam（发明 Python 不存在的 API，迭代/列对齐消费绕路，YAGNI） |
| 2 | TripleMemory 元素类型 | **A `[3]string` 定长数组**，命名 `TripleFact` 类型别名（归枚举区块），定义放 triple_memory.go：`_read` 过滤后恒 3 元，编译期钉死；13.25 签名 `[][3]string` 自文档。归档偏差 §8-2 | 否决 B []string 变长（零消费方需要变长——join 全位置遍历不感知长度，纯增 nil/空/长度漂移心智负担）；否决 C 复用 common.Triple（读取侧 LLM 数组拆装两次纯开销；Triple 带 Metadata 是写入侧模型 13.17/13.28 用，混用模糊两侧管线边界）。TripleFact 全仓无命名冲突（grep 核实） |
| 3 | MultiKBRetrievalResult None 边界 | **float64 直装非指针**：raw_score/raw_score_scaled 用 float64，构造点等价回落语义留 13.27（`meta.get(k, score)` → metaKeyOr 辅助），"metadata 显式 None"理论路径归档偏差 §8-3（零调用死代码不预防，13.17 §8-5 先例） | 否决 *float64（实际数据流恒非 None——13.27 构造点默认值回落，指针引入 nil 检查负担到 8.25 消费方） |
| 4 | KbIDs 类型与 omitempty | `KbIDs []string`（Go 首字母缩略词惯例，与 DocID/ChunkID 命名链一致）**无 omitempty**——Python `kb_ids: list = Field(default_factory=list)` 属 default_factory 集合，Triple.Metadata 无 omitempty 先例链（triple.go 注释钉死）；nil 序列化为 null 由构造方初始化保证（同 Metadata 契约）。Metadata 同款无 omitempty | 否决 omitempty（与 default_factory 先例链不一致，需归档第二条偏差） |
| 5 | MultiKB 文件放置 | **追加进既有 result.go**：Python 同文件 retrieval_result.py 三类同居，13.19 已交付 SearchResult/RetrievalResult 在此 | 不新建 multi_kb_result.go（同源三类型拆文件违背同居先例） |
| 6 | 章节顺序 | **方案 1 按编号，13.22 单独先行**；13.24 保持编号序但保留届时插队弹性（域内零消费时机自由，现在不决定）；13.25 不依赖 13.24（agentic 自带内联 _rewrite，与 QueryRewriter 两套独立实现——域 13 剩余链条为 13.22→13.23→13.25→13.27→13.28 主链 + 13.24/13.26 自由节点） | 否决 13.22 并入 13.23（三向发散消费方使"随出"归属不清，且 13.23 已 813 行最大节 review 面更宽）；否决 13.24 提前（推迟 13.22→13.23 前置链，13.24 零消费晚做无损） |
| 7 | 并发防护 | **不加 mutex**：Python 无锁（单协程使用），13.25 消费同样单 goroutine 迭代循环——真要并发是 13.25 的设计问题 | — |
| 8 | 日志 | Python 三文件零 logger → Go 零日志（13.20/13.21 同款结论，规范 3 无同步点） | — |

## 3. 依赖验证结论（讨论期间核实）

- triple_beam.py（36 行）逐行核对：`__init__`（beam 列表 + `{x.text for x in beam}` 集合 + score）/ `__getitem__`（支持负索引）/ `__len__` / `__contains__`（按 text 查集合）/ `__iter__` / `triples` property（返回内部引用）/ `score` property
- triple_memory.py（45 行）逐行核对：`memory` 元素 `tuple[str, ...]` 变长（`_read` 过滤 `len==3` 后恒 3，类型层面不设限）；`_tuple2str` 是**逐项 lower 后 join**（非整体 lower）；**TriplesStr 的 join 不做小写化**（与 _tuple2str 两套逻辑——去重键小写、展示串保留原大小写）；extendMemory 按字符串去重后 append；batchExtendMemory 循环委托
- MultiKBRetrievalResult（retrieval_result.py L33-41）：pydantic 六字段；`kb_ids: list` 无元素类型参数（13.27 构造点 `sorted(list(v["kb_ids"]))` 恒 str，8.25 只读不写）
- None 边界数据流核实：13.27 构造点 `meta.get("raw_score", score)` 默认值是 score（非 None）→ merged dict 恒含键 → `v.get("raw_score")` 恒非 None → **实际数据流无 None**；仅 metadata 显式含 `raw_score: None` 时 pydantic 严格模式炸 ValidationError（理论路径，§8-3 归档）
- TripleBeam 生产消费面（graph_retriever.py 逐处）：构造 L98/L167 / 负索引取尾 `beam[-1]` L176 / `len(beam)` L173/L504/L508 / `x in beam` L210 / 迭代 L118 / `beam.triples + [triple]` L152（concat 恒新建列表）/ 列对齐 `beam[col]` L510——**零切片、零增删改**，beam 构造后事实不可变（防环靠新建对象而非原地修改）
- QueryRewriter 域内消费核实：grep 全仓仅 `__init__.py`/`lazy_load.py` re-export，无生产消费方（agentic_retriever.py 不 import，自带内联 _REWRITE_PROMPT）
- Python 测试基线核实：tests/unit_tests/core/retrieval/common/ 下无 test_triple_beam.py/test_triple_memory.py；test_retrieval_result.py 8 用例全测 SearchResult/RetrievalResult；test_graph_retriever.py 有 TripleBeam 间接构造（7 处，属 13.23 测试基线）
- Go 侧现状核实：result.go/triple.go 已交付（13.19/13.17）；`TripleFact`/`[3]string` 全仓无冲突；Go 1.26 slices.Clone 可用
- 155203/155204 错误码（RETRIEVAL_RETRIEVER_INDEX_TYPE_NOT_SUPPORT/RETRIEVAL_RETRIEVER_MODE_INVALID）Go 侧已注册零消费，消费方为 13.23（_ensure_mode_allowed/max_length 校验）——本节零错误码，不动台账

## 4. 类型与接口设计

### 4.1 triple_beam.go（~60 行）

```go
// TripleBeam beam search 路径载体（GraphRetriever 的 TripleBeamSearch 消费）。
//
// 一条 beam = 候选三元组路径 + 累积分数；构造时一次性建立 text 去重集合，
// Contains 按 text O(1) 判重（对齐 Python __contains__ 查 _exist_triples）。
// 构造后事实不可变——防环靠新建 beam 而非原地修改（对齐 Python：展开
// 恒 beam.triples + [triple] 新建列表，零突变路径）。
//
// Python: openjiuwen/core/retrieval/common/triple_beam.py
type TripleBeam struct {
    // beam 路径节点（RetrievalResult 列表）
    beam []RetrievalResult
    // existTriples text 去重集合（构造时建立）
    existTriples map[string]struct{}
    // score 累积分数
    score float64
}

// NewTripleBeam 创建 beam 路径。
func NewTripleBeam(nodes []RetrievalResult, score float64) *TripleBeam

// Get 按索引取节点，负索引从尾部数（对齐 Python __getitem__；生产用
// Get(-1) 取尾）。越界 delegate slice 下标自然 panic（IndexError 对应物）。
func (b *TripleBeam) Get(idx int) RetrievalResult

// Len 返回路径长度（对齐 __len__）。
func (b *TripleBeam) Len() int

// Contains 判断节点是否已在 beam 中——按 text 判重而非全字段比较
// （对齐 __contains__：triple.text in self._exist_triples）。
func (b *TripleBeam) Contains(triple RetrievalResult) bool

// Triples 返回路径节点副本（浅拷贝密封：元素 Metadata map 仍共享，与
// Python 引用列表语义一致；返回新 slice 使消费方 append 不写穿内部
// 状态——偏差归档 spec §8-1）。
func (b *TripleBeam) Triples() []RetrievalResult

// Score 返回累积分数（对齐 score property）。
func (b *TripleBeam) Score() float64
```

不提供：切片方法（生产零消费）、setter/append API（不可变设计，编译期钉死）、Python `__iter__` 对应物（Go 直接 `for _, x := range beam.Triples()`，13.23 消费形态）。

### 4.2 triple_memory.go（~55 行）

```go
// TripleFact 读取侧三元组事实（定长 3 元：主语/谓语/宾语）。
//
// Python 对应物是 tuple[str, ...] 变长元组——_read LLM 输出经
// len(triple) == 3 过滤后恒为 3 元，Go 定长数组编译期收窄（偏差
// 归档 spec §8-2）。写入侧结构化模型见 Triple（13.17，带 Metadata），
// 两侧管线分立不复用。
type TripleFact = [3]string

// TripleMemory 多跳检索的事实记忆（AgenticRetriever 消费）。
//
// includedTriples 以小写化 join 字符串为键跨轮去重；memory 保序累积；
// TriplesStr 渲染为提示词注入串。单 goroutine 使用（Python 无锁，
// 13.25 消费同为单协程迭代循环），不加 mutex。
//
// Python: openjiuwen/core/retrieval/common/triple_memory.py
type TripleMemory struct {
    // includedTriples 去重键集合（tuple2str：逐项 lower 后空格 join）
    includedTriples map[string]struct{}
    // memory 保序累积的事实列表
    memory []TripleFact
}

// NewTripleMemory 创建空记忆（includedTriples map 初始化）。
func NewTripleMemory() *TripleMemory

// Len 返回已累积事实数（对齐 __len__）。
func (m *TripleMemory) Len() int

// Memory 返回事实列表副本（浅拷贝密封，§8-1 同链）。
func (m *TripleMemory) Memory() []TripleFact

// TriplesStr 渲染记忆为多行字符串，每行 "(s p o)"（空格 join 三元素 +
// 圆括号包裹 + 换行连接）。不做小写化——与去重键 _tuple2str 两套逻辑，
// 展示串保留原大小写（Python 同款不对称，测试钉死）。
func (m *TripleMemory) TriplesStr() string

// ExtendMemory 追加单条事实：小写化 join 键已存在则跳过（去重），
// 否则入集合并保序追加（对齐 extend_memory）。
func (m *TripleMemory) ExtendMemory(newTriple TripleFact)

// BatchExtendMemory 批量追加（循环委托 ExtendMemory，对齐 batch_extend_memory）。
func (m *TripleMemory) BatchExtendMemory(newTriples []TripleFact)
```

### 4.3 result.go 追加（~20 行）

```go
// MultiKBRetrievalResult 多知识库聚合检索结果数据模型。
//
// 多 KB 并行检索按 text 聚合去重后的出口契约：score 为跨 KB 融合
// 最大值，raw_score/raw_score_scaled 保真源库分数，kb_ids 记录命中
// 的全部知识库。
//
// Python: openjiuwen/core/retrieval/common/retrieval_result.py (MultiKBRetrievalResult)
// raw_score/raw_score_scaled 用 float64 非指针——13.27 构造点默认值
// 回落使实际数据流恒非 None；metadata 显式 None 理论路径归档 spec §8-3。
type MultiKBRetrievalResult struct {
    // Text 文本内容（聚合键）
    Text string `json:"text"`
    // Score 跨 KB 融合分数（最大值）
    Score float64 `json:"score"`
    // RawScore 源库原始分数
    RawScore float64 `json:"raw_score"`
    // RawScoreScaled 缩放到 [0,1] 的原始分数
    RawScoreScaled float64 `json:"raw_score_scaled"`
    // KbIDs 命中的知识库 ID 列表（无 omitempty——default_factory 先例，
    // nil 序列化为 null 由构造方初始化保证，同 Metadata 契约）
    KbIDs []string `json:"kb_ids"`
    // Metadata 元数据（同上无 omitempty）
    Metadata map[string]any `json:"metadata"`
}
```

## 5. 声明排列与文件组织

- triple_beam.go / triple_memory.go 按规范 2 排列：结构体 → 枚举（TripleMemory 含类型别名 TripleFact 归枚举区块）→ 导出函数（New）→ 非导出函数（无）
- 命名细节：`KbIDs` 首字母缩略词大写惯例（与既有 DocID/ChunkID 命名链一致）；私有字段 beam/existTriples/score/includedTriples/memory 对齐 Python _beam/_exist_triples/_score/_included_triples/_memory

## 6. 测试设计（Python 0 专属用例 → Go 19 例全增强）

### 6.1 triple_beam_test.go（10 例）

| # | 用例 | 断言要点 |
|---|------|---------|
| B1 | TestNewTripleBeam | 构造后 Len/Score/元素一致；existTriples 内建（经 Contains 间接验证） |
| B2 | TestTripleBeam_Get正索引 | Get(0)/Get(len-1) 返回对应元素 |
| B3 | TestTripleBeam_Get负索引 | Get(-1) 取尾（生产形态 graph_retriever L176 对应物） |
| B4 | TestTripleBeam_Get越界panic | Get(len) panic（IndexError 对应物，assert.Panics） |
| B5 | TestTripleBeam_Len | 空路径 0 / 多节点 n |
| B6 | TestTripleBeam_Contains按text判重 | 同 text 异 score/metadata → true（钉"按 text 非全字段比较"） |
| B7 | TestTripleBeam_Contains不存在 | text 不同 → false |
| B8 | TestTripleBeam_Triples密封 | 对返回值 append 后再取 Triples() 内部不变（C2 决策钉死） |
| B9 | TestTripleBeam_Triples元素一致 | 返回值逐元素 DeepEqual 原构造节点 |
| B10 | TestTripleBeam_Score | 构造分数透传 |

### 6.2 triple_memory_test.go（9 例）

| # | 用例 | 断言要点 |
|---|------|---------|
| M1 | TestNewTripleMemory | 零值状态 Len 0 / TriplesStr 空串 |
| M2 | TestTripleMemory_ExtendMemory首添 | 首条入列，Len 1 |
| M3 | TestTripleMemory_同串去重 | 相同事实二次 Extend 不重复入列 |
| M4 | TestTripleMemory_大小写不同视为重复 | ("A","R","B") 与 ("a","r","b") 互为重复（_tuple2str 逐项 lower 语义钉死） |
| M5 | TestTripleMemory_BatchExtendMemory含重复 | 批量 [x,y,x] → memory [x,y] |
| M6 | TestTripleMemory_TriplesStr格式 | 单条 → "(s p o)"; 多条换行连接 |
| M7 | TestTripleMemory_TriplesStr保留原大小写 | ("Foo","Is","Bar") → "(Foo Is Bar)"（与去重键 lower 两套逻辑不对称钉死） |
| M8 | TestTripleMemory_Memory密封 | 对返回值 append 后再取 Memory() 内部不变 |
| M9 | TestTripleMemory_Len | 随去重累积计数 |

### 6.3 result_test.go 追加（~4 例）

| # | 用例 | 断言要点 |
|---|------|---------|
| R1 | TestMultiKBRetrievalResult_全字段构造 | 六字段读写一致 |
| R2 | TestMultiKBRetrievalResult_JSON字段名 | 序列化含 "raw_score"/"raw_score_scaled"/"kb_ids" 等六键名 |
| R3 | TestMultiKBRetrievalResult_KbIDs无omitempty | nil KbIDs 序列化为 `"kb_ids":null` 而非省略（default_factory 先例，Triple.Metadata 同款） |
| R4 | TestMultiKBRetrievalResult_Metadata无omitempty | 同上注记 |

### 6.4 覆盖率目标

- common 包维持 ≥90% 水位（13.17 交付基线 91.8%；本节纯增量无死分支，预期小升）
- 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（CI 同款标签，memory 包 FTS 假失败教训）

## 7. 错误处理与日志

- 零错误码：三文件无 build_error 调用，错误码台账零变更
- 零日志：三文件零 logger（规范 3 无同步点，13.20/13.21 同款结论）
- Get 越界 panic 为设计行为（IndexError 对应物），非错误处理范畴

## 8. 已知偏差归档（Go vs Python）

| # | 偏差 | 说明 |
|---|------|------|
| 1 | Triples()/Memory() 返回浅拷贝 | Python property 返回内部引用 → Go slices.Clone 密封。防 append 写穿（Python list concat 恒新建 + property 无 setter 的双重隐式防护显式化）；元素内 Metadata map 仍共享（浅拷贝语义与 Python 引用列表一致，不算新偏差） |
| 2 | TripleFact [3]string 定长 | Python tuple[str, ...] 变长理论可装任意长度 → Go 编译期拒绝。_read 已 len==3 过滤，防御性收窄（13.25 签名 [][3]string 自文档） |
| 3 | raw_score/raw_score_scaled float64 非指针 | Python metadata 显式 None → pydantic ValidationError；Go 零值 0.0。实际数据流恒非 None（13.27 构造点默认值回落），零调用死代码不预防（13.17 §8-5 先例） |
| 4 | KbIDs []string | Python 裸 list 无元素类型参数 → Go 按构造点事实收窄（13.27 sorted(list(...)) 恒 str，8.25 只读） |
| 5 | Get 越界自然 panic | Python __getitem__ 越界 IndexError → Go slice 下标越界 panic，语义等价自然对应 |
| 6 | pydantic 必填校验无 Go 对应物 | MultiKBRetrievalResult 六必填字段的 ValidationError 路径 Go 编译期天然消解（13.17 §8-4 先例延续） |

## 9. 实施完成回填清单（防丢失，逐项核对）

1. IMPLEMENTATION_PLAN.md 13.22 行 `☐`→`✅` + 产出描述（**不触碰 13.23-13.28 已登记行**）
2. common/doc.go：文件目录 +2 条目（triple_beam.go / triple_memory.go）+ 核心类型索引 +4（TripleBeam / TripleFact / TripleMemory / MultiKBRetrievalResult）+ 对应 Python 代码段补两行
3. 验证点行保持 ☐ 不动（13.19-13.28 全链路未完）；跨域联动行（8.25 ⤴️ / 7.36 ⤵️）原样保留
4. 零错误码零日志——错误码台账与日志无回填项
5. git 提交显式 add 具体文件（**禁 -am**——工作区有 10.6.24a 未提交改动 IMPLEMENTATION_PLAN.md + swarm-tools spec，防搭便车；提交后 `git stat` 核对）
6. 记忆更新：13.22 完成状态（三拍板决策 C2/A/推荐打包 + 方案 1 顺序 + 13.24 插队弹性 + 13.25 不依赖 13.24 的事实）
7. 13.23 开工时消费指引：TripleBeam 经 NewTripleBeam 构造/Triples() 可安全 append；TripleMemory 经 NewTripleMemory（零值不可用）；TripleFact 为读取侧类型与写入侧 Triple 分立

## 10. 文件清单

```
internal/agentcore/retrieval/common/           ← 既有包
├── doc.go                 # 修改：文件目录 +2 条目 + 索引 +4 + Python 对应段
├── result.go              # 修改：追加 MultiKBRetrievalResult（~20 行）
├── triple_beam.go         # ★ 新增（~60 行）
├── triple_beam_test.go    # ★ 新增（10 例）
├── triple_memory.go       # ★ 新增（~55 行，含 TripleFact 别名）
└── triple_memory_test.go  # ★ 新增（9 例）

IMPLEMENTATION_PLAN.md                        # §9-1 回填（13.22 行）
```

- result_test.go 追加 R1-R4（~60 行，13.19 交付文件增量）
- 三源文件对应 Python 91 行（36+45+10），Go 预计 ~135 行 + 测试 ~290 行
