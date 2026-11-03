# memory 检索三处修复设计（重建判定 / 维度自愈 / 中文分词）

## 概述

线上排查 `memory_search` 返回 `{"disabled":false,"provider":"openai_compatible","model":"text-embedding-v3","results":[]}` 时发现三处问题，它们共同导致「检索看起来配好了，实际没生效，且无从判断」：

| 编号 | 问题 | 性质 |
|---|---|---|
| A | 重建判定与 meta 写入用的是**配置值**而非**实际 provider 实例** | Go 与 Python 的偏差（Go 错） |
| B | 向量表维度只在**进程内**比较，新进程读不到旧维度 | Go 与 Python 共有的坑，Go 侧补强 |
| C | FTS 分词器对中文不切词（`unicode61` 把连续中文当一个 token） | 功能缺失，Go 与 Python 同样弱 |

**根因链（实测复现）**：`embed_api_key` 未配置 → `CreateEmbeddingProvider` 静默降级为 mock（128 维随机向量）→ `status()` 仍返回配置值 `openai_compatible`/`text-embedding-v3`（A 导致不可见）→ 相似度接近 0，全被 `min_score=0.3` 滤掉 → `results: []`。用户补好 key 后仍不能直接恢复：`needsReindex` 因为配置值没变而不重建（A），且旧库的 `chunks_vec` 是 128 维、真模型是 1024 维（B）。

对应 Python 代码（`openjiuwen/core/memory/lite/manager.py`）：

| Python | 行号 | Go 现状 |
|---|---|---|
| `_should_full_reindex` | 587 | `manager_impl.go:730 shouldFullReindex` |
| `_run_reindex` | 611 | `manager_impl.go:752 runReindex` |
| `_ensure_vector_table` | 395 | `manager_impl.go:1134 ensureVectorTable` |
| `status` | 1192 | `manager_impl.go:406 Status` |
| `_search_keyword` / `build_fts_query` | — | `manager_impl.go:1281 searchKeyword` / `internal.go:110 BuildFTSQuery` |

## 用户决策记录

| 决策点 | 结论 | 理由 |
|---|---|---|
| 中文分词形态 | **gse 默认 embed**（`github.com/go-ego/gse` v1.3.1，`LoadDictEmbed("zh_s")`） | 免维护词典文件；代价是二进制 97MB → ~133MB（实测 embed 四份词典共 35.87MB），已接受 |
| 分词后多 token 连接 | **全部 AND**，超过 5 个 token 截断取前 5 | 文本侧仅 0.3 权重，语义召回由向量侧（0.7）兜底 |
| 分词器不可用 | **降级为不切分 + Error 日志**，连接符退回 OR | 记忆功能不因分词器拖垮；降级必须可见（embed→mock 的教训） |
| 检索结果 hint | 不加（沿用"与 Python 一致"） | 排查靠日志与 `provider`/`model` 字段 |

## 设计

### 1. 统一重建判定 `needsReindex`

A、B、C 都需要"何时全量重建"，若各写一段 if 会再次漂移。收敛为单一入口：

```go
// needsReindex 统一重建判定。返回 (是否需要, 原因)
// 原因取值：meta_missing / provider_unavailable / provider_changed / model_changed
//          / chunking_changed / segmenter_changed / vector_dims_changed
func (m *memoryIndexManager) needsReindex() (bool, string)
```

判定顺序与条件：

| 条件 | 原因值 | 归属 |
|---|---|---|
| meta 行缺失 / JSON 解析失败 | `meta_missing` | — |
| `m.provider == nil` | `provider_unavailable` | — |
| `meta.provider != m.provider.ID()` | `provider_changed` | A |
| `meta.model != m.provider.Model()` | `model_changed` | A |
| `meta.chunkTokens != settings.Chunking.Tokens` | `chunking_changed` | — |
| `meta.segmenter != segmenterID()` | `segmenter_changed` | C |
| `meta.vectorDims` 存在且 `!= m.provider.Dims()` | `vector_dims_changed` | B（初筛） |

`m.provider == nil`（初始化失败）时返回 `(true, "provider_unavailable")` 并打 Error，`runReindex` 跳过 embedding、只重建文本索引，保持记忆可用。

维度判定分两层：`needsReindex` 用 `provider.Dims()` 做**初筛**（覆盖 mock↔真模型这类主要场景，零成本），`runReindex` 再用**真实向量长度**二次校验（兜住 `Dims()` 撒谎的情况，见第 3 节）。

### 2. A：判定与 meta 改用实际 provider

改动点（`manager_impl.go`）：

| 位置 | 现在 | 改为 |
|---|---|---|
| `shouldFullReindex` → `needsReindex` | 比 `m.settings.Provider` / `m.settings.Model` | 比 `m.provider.ID()` / `m.provider.Model()` |
| `runReindex` 写 meta | 写 settings 值 | 写 `m.provider.ID()` / `m.provider.Model()` |

- meta 新增字段 `segmenter`（见 C）、保留 `vectorDims` 作为记录值；`providerKey` 仍只写不判定（与 Python 一致）。
- **兼容旧库**：老 meta 存的是 settings 值（`openai_compatible`）。若当时真在跑真 provider，两者相同、不触发重建；若当时是 mock，`"openai_compatible" != "mock"` → 自动重建。**无需迁移脚本**。
- `status()` 已在前序提交（`ee4f3772`）改为实例值，本节完成同一根因的另一半。

### 3. B：向量维度自愈

现状缺陷：`ensureVectorTable(dims)` 只在 `m.vectorDims != nil` 时比对；新进程该字段为 nil，`CREATE VIRTUAL TABLE IF NOT EXISTS` 对已存在的旧维度表是空操作 → 1024 维向量写入 128 维表失败（`indexfile failed`），且检索侧同样维度不匹配。

设计（两层）：

1. **初筛（判定期）**：`needsReindex` 比 `meta.vectorDims` 与 `m.provider.Dims()`。零成本，覆盖"mock 128 → 真模型 1024"这条主路径。
2. **兜底（写入期）**：`runReindex` 先 `EmbedQuery("sample")` 取 `len(vec)` 作为**真实维度**，与 `meta.vectorDims` 比对；不一致或 `meta.vectorDims` 缺失时 `DROP TABLE IF EXISTS chunks_vec` 后按真实维度建表。这样即便 `provider.Dims()` 撒谎（`baseEmbeddingAdapter` 硬编码 1024，用户换 768 维模型时它会报错），索引仍能被纠正。
3. **正常路径零额外开销**：维度未变时不调用 `EmbedQuery("sample")`；仅在重建路径上取一次。

### 4. C：中文分词

#### 4.1 依赖与初始化（新文件 `segmenter.go`）

```go
// 分词器单例：加载 35 万词词典实测 843ms，全局只付一次，并后台预热
func InitMemorySegmenterAsync()          // 进程启动时调用，结果落在 segInst
func segmenterID() string                // "gse-zh_s@v1.3.1" / "none"
func segmentForFTS(text string) []string // 唯一的分词入口（索引侧与查询侧共用）
```

- `segmentForFTS` 处理链：`gse.Cut(text, true)` → 小写化 → 过滤空白与纯标点 → 过滤停用词（`LoadStopEmbed`）→ 去重保序。
- 实测样本（v1.3.1 + `zh_s`）：
  - `"VDS15800 组网"` → gse 原始输出 `["vds15800", " ", "组网"]` → 过滤后 `["vds15800", "组网"]`
  - `"这个设备的组网方式"` → `["这个","设备","的","组网","方式"]` → 过滤停用词后 `["这个","设备","组网","方式"]`
- 加载失败：`segInst = nil`，打一次 Error（`event_type=memory_segmenter_unavailable`），`segmenterID()` 返回 `"none"`。

#### 4.2 索引侧

| 表/列 | 内容 |
|---|---|
| `chunks.text` | **原文不变**（snippet、`citation`、`memory_get` 展示均依赖它） |
| `chunks_fts.text` | `strings.Join(segmentForFTS(原文), " ")` |

FTS 表是 contentless（`content=''`），文本本就由我们写入，因此两份文本天然可分离。分词结果为空时写原文，避免空文档。

#### 4.3 查询侧（`BuildFTSQuery` 改造）

```go
tokens := segmentForFTS(query)
if len(tokens) == 0 { return "" }             // 不做关键词检索，只走向量（现状行为）
if len(tokens) > 5 { tokens = tokens[:5] }
// 拼接：每个 token 加双引号转义，之间用 AND
"tok1" AND "tok2" AND ...
```

#### 4.4 降级路径（分词器不可用）

- 索引侧写**原文**；查询侧回退到现有正则切分 `[\p{L}\p{N}_]+`，连接符用 **OR**（现状行为）——降级时中文没有整词 token，AND 会让 `vds15800 AND 组网` 完全落空，OR 至少能命中 `vds15800`。
- `meta.segmenter = "none"`；将来分词器恢复可用时 `needsReindex` 命中 `segmenter_changed` → 自动重建。
- 不会抖动：分词器不可用期间，`meta.segmenter("none") == segmenterID()("none")`，每次启动不重复重建。

#### 4.5 老库重建

FTS 文本格式由"原文"变为"分词串"，由 `meta.segmenter` 缺失触发 `segmenter_changed` 全量重建。**不需要手工删 `memory.db`**——前提是 A 先落地。

## 可观测性

| event_type | 级别 | 触发 | 关键字段 |
|---|---|---|---|
| `memory_reindex_triggered` | Info | `needsReindex` 命中 | `reason`、`old`、`new` |
| `memory_vector_table_recreated` | Warn | B 命中 | `old_dims`、`new_dims` |
| `memory_segmenter_unavailable` | Error（只一次） | 分词器加载失败 | `error` |
| `embedding provider initialized` | Info（已有） | — | 改为打实例值 `provider`/`model` |

不扩展 `StatusResult`：`memory_search` 返回字段保持与 Python 一致（`results`/`disabled`/`provider`/`model`/`error`），新增字段无消费者。

## 测试计划

### 单测（`internal/agentcore/memory/lite`）

| 用例 | 断言要点 |
|---|---|
| `TestNeedsReindex_表驱动` | 7 条：meta 缺失 / provider 变 / model 变 / chunking 变 / segmenter 变 / dims 变 / 全一致 |
| `TestRunReindex_meta写实例值` | mock provider 时 `meta.provider == "mock"` |
| `TestEnsureVectorTable_维度变更重建` | 先建 128 维并插入 → 用 1024 维重建 → 旧表被 DROP、新表可插入 |
| `TestSegmentForFTS_过滤规则` | 空格/标点/小写/停用词/空输入，断言 `"VDS15800 组网"→["vds15800","组网"]` |
| `TestBuildFTSQuery_AND与截断` | 多 token→AND；>5 截断；空→`""` |
| `TestBuildFTSQuery_降级回OR` | 分词器不可用时用正则切分 + OR |
| `TestSegmenterUnavailable_meta记none` | 降级后 `meta.segmenter=="none"`；恢复后触发重建 |

关键回归：`provider_changed` 与 `meta写实例值` 两条在**当前代码下会失败**（现在比/写 settings），必须先红后绿。

### 集成（`tests/integration/agentcore/harness/rails/memory`）

1. **中文端到端**：`MEMORY.md` 写入含 `VDS15800 组网` 的内容 → `memory_search {"query":"VDS15800 组网"}` → 命中且 `citation` 为 `MEMORY.md#Lx-Ly`。embedding 用 `httptest` 词袋（先例 `coding_memory_rail_recall_test.go`），不依赖真实 API。
2. **配置变化自愈**：mock provider 建库（128 维）→ 换 1024 维 provider 重启 manager → `chunks_vec` 重建、检索可用、日志含 `vector_dims_changed`。
3. **降级**：注入"分词器不可用"开关 → 英文/数字仍命中、中文仅靠向量、日志含 Error。

### 门禁注意事项

- 新文件 `segmenter.go` 必须登记到 `doc.go` 文件目录。
- 新增纯类型文件（无 func/var/const）改动行永远 0%，需行级豁免（先例：`memory/lite/types.go`）。
- 引入 `go-ego/gse` 后先跑 `golangci-lint`（新依赖常触发 unused/depguard 规则）。
- 集成改动行门槛 5%：`memory/lite` 已在 `-coverpkg` 内，上述 3 条集成用例足够。
- （可选）CI 增加产物体积断言（`>160MB` 报警），防止 gse 未来把 embed 数据引到 `init()` 导致 linker DCE 失效、二进制意外膨胀。

## 实施顺序

```
1. A：needsReindex 归一 + runReindex 写实例值        ← 地基
2. B：维度自愈（真实向量长度 + DROP chunks_vec）
3. C-1/C-2：gse 依赖 + segmenter.go（含降级）
4. C-3/C-4：索引侧分词文本 + 查询侧 AND/截断
5. 可观测性日志 + 附录排查表
6. 集成 3 条 + 门禁复跑 + 推送 CI 四绿
```

## 风险与偏差登记

| 项 | 说明 | 缓解 |
|---|---|---|
| 二进制 +36MB | gse 默认 embed 了 jp/繁/简/idf 四份词典（实测 35.87MB） | 已确认接受；可选 CI 体积断言 |
| 与 Python 分叉 | 分词、AND 连接、维度自愈 Python 均无 | 记录在案；分词仅影响 FTS 文本层，不改 `chunks.text` 与对外契约 |
| `provider.Dims()` 不可信 | `baseEmbeddingAdapter` 硬编码 1024 | 维度一律取真实向量长度 |
| 词典许可 | gse 代码 Apache-2.0，词典来源需确认 | 落地前查 `data/dict/README.md` 并在 NOTICE 注明 |
| 分词器加载耗时 843ms | 首次加载 | 全局 `sync.Once` + 后台预热，不阻塞首次搜索 |

## 附录：排查手册（现象 → 看什么）

| 现象 | 先看 |
|---|---|
| `memory_search` 返回 `provider="mock"` | `embed_api_key` / `embed_base_url` 未配置或没生效 |
| `provider` 看起来正常但 `results: []` | 日志 `embedding query failed`（接口/key 问题）、`Embedding API key not found, using mock provider` |
| 刚补好 key 但结果仍空 | 日志 `memory_reindex_triggered reason=provider_changed`（A）与 `memory_vector_table_recreated`（B）是否出现；未出现说明判定没生效 |
| 中文搜不到、英文能搜到 | 日志 `memory_segmenter_unavailable`；正常时检查 `meta.segmenter` 是否为 `gse-zh_s@v1.3.1` |
| 索引为空 | `sqlite3 memory.db "select count(*) from files; select count(*) from chunks;"`；`files=0` 说明记忆目录没有可索引内容 |
