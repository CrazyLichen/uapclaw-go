# 48h 逻辑审查报告

> 审查时间：2026-10-19
> 审查范围：最近 48 小时（2026-10-17 04:00 ~ 2026-10-19 04:00，68 个提交）完成的实现计划章节
> 涉及章节：13.3（MilvusIndexer + MilvusVectorStore）、13.5（Chunker 抽象 + registry）、13.6（CharChunker + CharSplitter）、10.6.15（Swarm Memory Embeddings + MemoryIndexManager）、10.6.16（External Memory）、10.6.18（memoryrpc）
> 参考项目：`/home/opensource/agent-core/openjiuwen`（retrieval 章节）、`/home/opensource/jiuwenswarm-develop/jiuwenswarm`（swarm memory 章节）
> 审查方式：三路并行逐方法对比（非抽样）+ team-lead 对全部 S/M 级发现独立源码验证

---

## 统计摘要

| 严重程度 | 数量 |
|---------|------|
| 严重 (S) | 4 |
| 一般 (M) | 6 |
| 提示 (T) | 15 |
| **合计** | **25** |

> 编号说明：S-01~S-02 + M-01~M-03 + T-01~T-02 来自 13.3/13.5/13.6 审查；S-03 + M-04~M-05 + T-03~T-12 来自 10.6.18 memoryrpc 审查；S-04 + M-06 + T-13~T-15 来自 team-lead 独立验证（10.6.15 watcher 递归问题、跨包 CJK 计数、safeStat 量纲已在 memoryrpc 项中合并描述）。

**说明**：Agent 1 曾提交两份结论冲突的报告（第一轮 2S+3M+2T，第二轮 0S+0M+3T 并将 SCANN→IVF_PQ 视为"SDK 限制的合规占位"）。team-lead 直接查证 SDK v2.6.5 源码（`$(go env GOMODCACHE)/github.com/milvus-io/milvus/client/v2@v2.6.5/index/scann.go:44` 存在 `NewSCANNIndex(metricType, nlist, withRawData)`，`flat.go:32` 存在 `NewFlatIndex(metricType)`），确认第二轮报告的"SDK 缺构造器"前提不成立，**本报告以第一轮结论为准**（S-01/S-02 均经源码复核确认）。

---

## 已验证误报（不计入）

- ~~Add 进度日志 `processed%100` 与 Python 语义不同~~ — Python 同样是 `processed % 100 == 0`（milvus_store.py L178），batch_size=128 时两边行为一致（均需 6400 条公共触发），非移植偏差。
- ~~Go `nodes := cache[:batchSize]; cache = cache[batchSize:]` 与 Python `cache = []` 不等价~~ — 循环内 `len(cache)==batchSize` 时两者等价。
- ~~CheckVectorField 的 returnedType 三级回退与 Python 不一致~~ — v2.6.5 SDK `IndexDescription.Params()` 返回 `index.Params()` 输出（含 IndexTypeKey），与 Python `actual.get("index_type")` 对齐。
- ~~hybridFactory 13.8 占位是问题~~ — 计划内合规占位（registry.go L188-191，⤴️ 13.8 回填标记）。
- ~~text_splitter.go error 返回为缺失实现~~ — 13.7 IndexSentenceSplitter 预留，注释明示，计划内。
- ~~defaultCharChunkOverlap=40 常量参与运行时~~ — Go 无 None 语义不参与运行时，CharChunker 路径总是显式传参，文档锚点已说明。
- ~~10.6.15 MemoryIndexManager 有方法缺失~~ — 全方法清单核对（35 方法）完整对齐 Python swarm 版 manager.py。
- ~~10.6.16 External Memory builder 分发有缺失~~ — 四路分发（openjiuwen/mem0/openviking/插件）+ env 回退链 + 品牌映射逐项对齐（team-lead 复核确认）。

---

## 一、审查范围确认（步骤 1）

48h 窗口内完成的计划章节（依据 git log 与 IMPLEMENTATION_PLAN.md 交叉核对）：

| 章节 | 主要提交 | Python 参照 |
|------|---------|------------|
| 13.3 MilvusIndexer | 10-18 09:25~10:06（8 commits） | openjiuwen/core/retrieval/indexing/indexer/milvus_indexer.py + vector_store/milvus_store.py |
| 13.5 Chunker 抽象+registry | 10-18 18:15~18:43（6 commits） | openjiuwen/core/retrieval/indexing/processor/chunker/{base,char_chunker,__init__}.py |
| 13.6 CharChunker+CharSplitter | 10-18 19:55~20:03（4 commits） | 同上 + text_splitter.py |
| 10.6.15 Swarm Memory | 10-18 11:02~11:48（4 commits） | jiuwenswarm/agents/harness/common/memory/{manager,internal,embeddings,config}.py |
| 10.6.16 External Memory | 10-18 19:11~20:09（7 commits） | 同目录 external_memory_{builder,config}.py |
| 10.6.18 memoryrpc | 10-18 21:56~22:57（10 commits） | jiuwenswarm/agents/harness/common/memory_rpc.py |

窗口内另含 3 个审查修复 commit（13:49 hybrid 降级 ID 补偿 / 20:09 10.6.16 审查修复 / 22:57 splitlines 对齐），本报告已扣除这些已知修复项避免重复报告。

---

## 二、严重问题 (S)

### S-01: buildDenseIndex 用 IVF_PQ/IVF_FLAT 冒充 SCANN/FLAT，TODO 前提不成立且引发建库→校验链路自毁

**Python 参考** (`milvus_indexer.py:433-446`)：
```python
# Add dense vector index
index_type = self.vector_field.index_type
if index_type == "auto":
    index_type = "AUTOINDEX"
else:
    index_type = self.vector_field.index_type.upper()
    index_variant = getattr(self.vector_field, "variant", None)
    if index_variant is not None:
        index_type = index_type + "_" + index_variant.upper()
index_params.add_index(
    field_name=self.vector_field.vector_field,
    index_type=index_type,
    **self._construct_config,
)
```
用户配置 SCANN 时服务器创建的就是 SCANN 物理索引，配置参数（nlist/with_raw_data/metric）经 `_construct_config` 整体下发。

**Go 问题**（两处同源）：
- `internal/agentcore/retrieval/indexing/indexer/milvus.go:663-673`：
```go
case *vector_fields.MilvusFLAT:
    // 对齐 foundation 层：FLAT 等价 nlist=1 的 IVF_FLAT
    return index.NewIvfFlatIndex(metricType, 1), nil
case *vector_fields.MilvusSCANN:
    // TODO(#通用): 新 SDK 暂无 SCANN 构造器，用 IVF_PQ 近似（对齐 foundation 层做法）；
    // SDK 补充 SCANN 构造器后回填真实实现（与 foundation/store/vector_fields 的 SCANN TODO 联动）
    return index.NewIvfPQIndex(metricType, vf.Nlist, 8, 8), nil
```
- `internal/agentcore/foundation/store/vector/milvus.go:1344-1352`：同样的 `NewIvfFlatIndex(metricType, 1)` / `NewIvfPQIndex(metricType, vf.Nlist, 8, 8)`。

TODO 注释声称"新 SDK 暂无 SCANN 构造器"，但 go.mod 锁定的 milvus client v2.6.5 **实际有**这两个构造器（team-lead 已查证 SDK 源码）：
- `index/scann.go:44`：`func NewSCANNIndex(metricType MetricType, nlist int, withRawData bool) Index`
- `index/flat.go:32`：`func NewFlatIndex(metricType MetricType) Index`

**影响**（具体流程示例）：
1. 用户配置 `vector_field = MilvusSCANN(nlist=128)` → Go 调 `BuildIndex` → `ensureCollection` → `buildDenseIndex` 返回 `IvfPQIndex(nlist=128, m=8, nbits=8)` → Milvus 服务器创建的物理索引 index_type = **IVF_PQ**；
2. 同一配置随后调 `MilvusVectorStore.CheckVectorField`（vector_store/milvus.go:701）：
   `strings.HasPrefix(returnedType /* "IVF_PQ" */, "SCANN")` → false → 抛 1555xx `KB_DATABASE_CONFIG_INVALID`："MilvusVectorStore has index_type of scann while actual database has index_type of IVF_PQ"
3. 结果：**建库成功后同一配置永远无法通过校验**，SCANN 用户被锁死（只能删库重建到 IVF_PQ）。FLAT → IVF_FLAT 同理（"FLAT" 前缀不匹配 "IVF_FLAT"）。
4. IVF_PQ 还凭空引入 m=8/nbits=8 参数（PQ 量化有损），reorder_k/with_raw_data 丢失，检索精度语义与用户配置完全脱钩。

**修复方案**：
```go
// indexer/milvus.go buildDenseIndex
case *vector_fields.MilvusFLAT:
    return index.NewFlatIndex(metricType), nil
case *vector_fields.MilvusSCANN:
    return index.NewSCANNIndex(metricType, vf.Nlist, vf.WithRawData), nil
```
foundation/store/vector/milvus.go:1344-1352 同步修正。同步删除已失效的 TODO 注释；vector_fields variant 字段 TODO（见 T-13）是另一独立项，不随本项修复。

---

### S-02: ensureCollection dense 索引丢弃 constructConfig 参数（HNSW M/efConstruction、IVF/SCANN nlist、SCANN with_raw_data 未下发）

**Python 参考**：`add_index(..., **self._construct_config)`（L442-446）——`_construct_config` 含 metric_type/M/efConstruction/nlist/with_raw_data，索引参数整体下发。

**Go 问题**（`indexer/milvus.go:124-131` 辛苦算出 constructConfig 后，唯一消费方只有 metric_type）：
- L128：`idx.constructConfig = idx.vectorField.ToConstructDict()` — 计算出来
- L622：`denseIndex, err := mi.buildDenseIndex()` — 但 buildDenseIndex 只读 `mi.distanceMetric` 和 `vf.M/vf.EfConstruction/vf.Nlist`
- 构造函数算出的 constructConfig 在 ensureCollection 路径全程无消费。

**实际影响分级**（HNSW/IVF 用了 vf 字段直读，属"半对齐"——参数确实下发但来源绕开了 constructConfig 抽象）：
- **HNSW/IVF**：`NewHNSWIndex(metricType, vf.M, vf.EfConstruction)`（L667）/ `NewIvfFlatIndex(metricType, vf.Nlist)`（L669）参数确实传了，行为等价 Python；
- **SCANN**：`NewIvfPQIndex` 只传了 nlist，with_raw_data 丢失（修复 S-01 后经 `NewSCANNIndex(metricType, nlist, withRawData)` 修复）；
- **FLAT**：无参数，无影响。

**残余风险**（评级 S 的核心理由）：`CheckVectorField` 末尾 `CheckConfigsMatching(s.constructConfig, actualParams)`（vector_store/milvus.go:719）用 constructConfig 作为"期望配置"对比服务器 actual——**建库端（indexer）与校验端（store）的配置口径已脱钩**：建库走 vf 字段直读，校验走 constructConfig。若未来 vf 字段与 constructConfig 键名/取值出现分叉（如 keepzero 语义、variant 键），建库→校验链路将在无代码变更感知的情况下断裂。另外 constructConfig 中含 `with_raw_data`（SCANN construct 键），服务器 DescribeIndex 返回的 params 不含该键时 `actual[attr]` 为 nil → `fmt.Sprintf("%v", nil)` = `"<nil>"` ≠ `"true"` → **SCANN 建库后 CheckVectorField 的 CheckConfigsMatching 必然误报 1555xx**。

**修复方案**：统一口径——ensureCollection 构建索引时显式从 constructConfig 取参（或 buildDenseIndex 直接消费 mi.constructConfig），保证"建库参数"与"校验期望"同源；SCANN 修复 S-01 后确认 `with_raw_data` 在 CheckConfigsMatching 中是否需要像 efSearchFactor 一样跳过（Python 侧 `add_index(**construct_config)` 下发后服务器 params 是否回显该键需实测，若不回显则 Python 也有同样问题，按 Python 实测行为对齐）。

---

### S-03: memoryrpc 字符统计全部用 byte len，CJK 内容 3x 偏差，large_files 阈值误报

**Python 参考**（`len()` 按 Unicode 字符计数）：
- `memory_rpc.py:333`：`total_chars = sum(len(f.content) for f in project_files)`
- `memory_rpc.py:357`：`coding_total_chars += len(text)`
- `memory_rpc.py:372`：`auto_total_chars += len(text)`
- `files.py:378-379`：`char_count = len(f.content)` / `if char_count > threshold:`（threshold=40000 按字符）

**Go 问题**（`internal/swarm/agents/harness/common/memoryrpc/memory_rpc.go`，全部按字节计数）：
- L254-257：`totalChars += len(f.Content)`
- L291：`codingTotalChars += len(data)`
- L307：`autoTotalChars += len(data)`
- 消费方 `rails/project_memory/files.go:386`：`charCount := len(f.Content)`（跨包既有代码）

**影响**（具体示例）：中文记忆文件每字 3 字节（UTF-8）。40000 汉字 = 120000 字节：
- Python：40000 汉字恰好达到阈值边界（不触发，`>` 才触发）；
- Go：约 **13334 汉字即触发** large_files 告警（40000 字节 / 3），三处 total_chars 对 CJK 全部偏大 3 倍，TUI 面板"总字符数"失真。

**项目先例**：`memory/dreaming/sweeper.go:761` 注释明确"Python len() 按 Unicode 字符计数，Go 用 RuneCountInString 对齐（byte 长度对 CJK 有 ~3x 偏差）"；`server/session/session_utils.go:34` 同。历史审查（2026-09-28/09-30/10-01/10-13）CJK 计数/截断均列 P0。

**修复方案**：
```go
// memory_rpc.go 三处
totalChars += utf8.RuneCountInString(f.Content)
codingTotalChars += utf8.RuneCountInString(string(data))
autoTotalChars += utf8.RuneCountInString(string(data))
```
同步修 `rails/project_memory/files.go:386`：`charCount := utf8.RuneCountInString(f.Content)`（跨包联动，L351 merge max_chars 软上限同为字节口径可顺带评估）。测试补充 CJK 用例：13334 汉字文件不应触发 40000 阈值。

---

### S-04: MemoryIndexManager 文件监听非递归，memory/ 子目录与 extraPaths 目录变更不触发增量同步（注释错误声称对齐）

**Python 参考**（`manager.py:386-398`，watchdog Observer）：
```python
for watch_path in watch_paths:
    if os.path.isdir(watch_path):
        self._file_observer.schedule(handler, watch_path, recursive=True)  # ← 递归监听
    else:
        parent_dir = os.path.dirname(watch_path)
        if parent_dir:
            self._file_observer.schedule(handler, parent_dir, recursive=False)
self._file_observer.start()
```
配合 `list_memory_files`（internal.py:45-48）对 memory/ 目录 `os.walk` 递归收集所有 .md——**索引收集是递归的，监听也是递归的，两者一致**。

**Go 问题**（`internal/swarm/agents/harness/common/memory/manager.go:731-742`，fsnotify）：
```go
// Python: isdir → schedule(recursive=True)；文件 → schedule(dirname, recursive=False)
for watchPath := range watchPaths {
    if st, err := os.Stat(watchPath); err == nil && st.IsDir() {
        _ = watcher.Add(watchPath) // fsnotify 目录监听默认递归（Add 递归子目录需手动；此处对齐 watchdog recursive=True 语义）
    } else {
        ...
    }
}
```
**注释声称"fsnotify 目录监听默认递归"——这是错误的**。fsnotify v1.10.1（go.mod 锁定版本）官方文档明确（team-lead 已查证 `$(go env GOMODCACHE)/github.com/fsnotify/fsnotify@v1.10.1/fsnotify.go`）：

> "All files in a directory are monitored, including new files that are created after the watcher is started. **Subdirectories are not watched (i.e. it's non-recursive).**"

**影响**（具体流程示例）：
1. `ListMemoryFiles`（internal.go:62，filepath.Walk **递归**）索引了 `memory/sub/笔记.md`、`memory/2026-10/session.md` 等子目录文件（extraPaths 目录同理，internal.go:73-77 递归 walk）；
2. 用户在会话中通过 `memory.edit` 修改 `memory/sub/笔记.md` → fsnotify 只监听 memory/ 顶层 → **无事件** → `scheduleWatchSync` 不触发 → dirty 不置位；
3. 依赖 watcher 的增量同步静默失效（`Sync` 只在 onSearch 定时/搜索前触发兜底，watchDebounceMs 防抖路径失效）；子目录文件的修改要等下一次全量 reindex 或 onSearch sync 才被索引，与 Python 行为（子目录修改 1s 防抖后立即同步）不一致。

**修复方案**：目录监听改递归——建库时对 watchPath 做一次 `filepath.Walk` 把所有现存子目录 `watcher.Add`，并在 watchLoop 收到 Create 事件时对新目录动态补 Add（Python watchdog recursive=True 的等价语义）。或切换到支持递归的监听方案。注意：`memory/` 下 `ltm/` 子目录（10.6.16 LTM 数据目录）也会被递归监听——Python watchdog 同样递归监听整个 memory/，行为一致无需排除，但 `IsMemoryPath`（internal.go:254-266，按路径段匹配 memory）过滤逻辑保持不动即可。

---

## 三、一般问题 (M)

### M-01: HybridSearch 降级路径吞掉 vector 路错误，且 idMapping Text 键碰撞串 ID

**Python 参考**（`milvus_store.py:362-382`）：
```python
task_vector = (
    asyncio.create_task(self.search(query_vector, top_k, None))
    if query_vector
    else asyncio.create_task(asyncio.sleep(0, result=[]))
)
task_text = asyncio.create_task(self.sparse_search(query_text, top_k, None))
results_vector = await task_vector   # ← search() 异常直接上抛 → 整个 fallback 失败
results_text = await task_text
fused_results = rrf_fusion([results_vector, results_text], k=60)
```
`rrf_fusion`（fusion.py:41-42）`if key not in result_dict: result_dict[key] = result`——同 text 首次出现保留。

**Go 问题**（`vector_store/milvus.go:1054-1061`）：
```go
for i := 0; i < 2; i++ {
    out := <-outCh
    if out.err != nil || len(out.results) == 0 {
        continue   // ← 错误静默吞掉
    }
    ...
    idMapping[sr.Text] = sr.ID   // ← 后到的一路覆盖先到（两路 Text 相同 ID 不同时串 ID）
```
两处偏差：
1. vector 路 `Search` 返回 error 时 Go 静默 continue（仅用 BM25 路融合），Python 中 vector 路异常使整个 fallback 失败上抛——Go 更宽松且不可观测；
2. `idMapping[sr.Text] = sr.ID` 后写覆盖先写，覆盖顺序取决于 goroutine 完成顺序（不确定）——Python result_dict 首次保留语义确定性。

**修复方案**：`out.err != nil` 时先 `logger.Warn` 再 continue（对齐 Python 可观测性；吞异常程度可另行决策，但至少有日志）；idMapping 改为 `if _, exists := idMapping[sr.Text]; !exists { idMapping[sr.Text] = sr.ID }`（首次保留，对齐 fusion.py L41-42）。另注：两 goroutine 写 `idMapping` 实际是先经 channel 汇聚到主 goroutine 串行写（outCh 容量 2），无并发写竞争，仅顺序语义问题。评级 M 因降级路径本身是异常兜底场景。

---

### M-02: memoryrpc safeStat 的 mtime 量纲错误：毫秒 int vs Python float 秒

**Python 参考**（`memory_rpc.py:159-164`）：
```python
s = Path(path).stat()
return {"size": s.st_size, "mtime": s.st_mtime}   # st_mtime = float 秒（如 1763000000.123）
```

**Go 问题**（`memory_rpc.go:634-640`）：
```go
return map[string]any{"size": s.Size(), "mtime": s.ModTime().UnixMilli()}  // int64 毫秒
```
**影响**：mtime 是 RPC payload 字段，List/scanMDFiles 均透传给 TUI。Python 原版 TUI 前端按秒渲染；Go 值大 1000 倍，11.15 TUI 面板移植时"修改时间"显示错乱。

**修复方案**：
```go
return map[string]any{"size": s.Size(), "mtime": float64(s.ModTime().UnixNano()) / 1e9}
```
失败分支 `"mtime": int64(0)` 同步改 `float64(0)`；同步调整 `TestSafeStat_正常与异常` 的断言。

---

### M-03: MilvusVectorStore.Add 尾批 flush 失败仅 warn 不返回 error

**Python 参考**（`milvus_store.py:199-200`）：
```python
# Flush using client API
self._client.flush(self.collection_name)   # ← 无 try/except，异常上抛
```

**Go 问题**（`vector_store/milvus.go:512-516`）：
```go
// 对齐 Python: self._client.flush(self.collection_name)
if err := c.Flush(ctx, milvusclient.NewFlushOption(s.collectionName)); err != nil {
    logger.Warn(logComponent).Err(err).Str("collection_name", s.collectionName).Msg("Flush 失败")
}
return nil   // ← 吞掉错误返回成功
```
**影响**：数据已 insert 但 flush 失败静默通过 → `BuildIndex` 报成功但数据未持久化（Milvus flush 保证数据落盘可搜）；Python 中 flush 异常会沿 `asyncio.to_thread` 上抛 → `add` 失败 → BuildIndex 包装为 ADD_DOC_RUNTIME_ERROR。

**修复方案**：flush 失败返回 error（保留 Warn 日志），调用方 BuildIndex 沿既有错误链路包装。

---

### M-04: memoryrpc HandleMemoryEdit 的 mkdir/touch 失败被静默吞掉，返回假成功

**Python 参考**（`memory_rpc.py:275-278`，mkdir/touch 在 try 块**之外**）：
```python
else:
    parent = Path(resolved).parent
    parent.mkdir(parents=True, exist_ok=True)   # OSError 上抛
    Path(resolved).touch()                       # OSError 上抛
```
异常由注册层外层捕获（tui_connect.py L1864-1866）→ `send_response(ok=False, error=str(exc))`。

**Go 问题**（`memory_rpc.go:75-84`）：mkdir/touch 失败仅 Error 日志，随后照常返回 `{"editable": true, "exists": false, ...}` 成功响应。TUI 误以为文件已创建可进入编辑。另 doc.go L8-9 "handler 从不抛异常" 的前提描述与 Python 实际不符——read_text 有 try/except，但 mkdir/touch 没有，正是靠外层 wrapper 兜底转 ok=False。

**修复方案**（不动 handler 签名，为 11.15 注册层留可感知通道）：
```go
createErr := ""
if err := os.MkdirAll(parent, 0o755); err != nil {
    logger.Error(...)
    createErr = err.Error()
} else if f, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
    logger.Error(...)
    createErr = err.Error()
} else { _ = f.Close() }
result := map[string]any{"path": resolved, "exists": false, "content_preview": "", "kind": kind, "editable": true}
if createErr != "" {
    result["error"] = createErr  // 11.15 注册层检查此字段转 ok=False（对齐 Python 外层 wrapper）
}
return result
```
建议在 11.15 注册层实现时落地 `error` 字段协议约定。

---

### M-05: memoryrpc Toggle 写回用 Load+Set 全量重写，config.yaml 既有注释/格式丢失

**Python 参考**（`memory_rpc.py:448-456`）：ruamel round-trip 保留注释与格式。

**Go 问题**（`memory_rpc.go:714-723`）：D3 决策采用 Load+Set 点路径 + yaml.Marshal 全量重写——用户 config.yaml 中手写的注释、空行、键序一次 toggle 后全部丢失。

**影响**：与 Python 行为差异属于**已知取舍**（对齐 11.14 web 通道先例），但代价未在显眼处声明——若用户配置有注释，数据不可恢复地丢失。

**修复方案**：短期维持 Load+Set 但在 updateModeMemoryConfig 写回前 logger.Warn 提示"配置注释将不保留"；长期可评估 yaml.v3 Node 模式保留注释（yaml.v3 的 Node API 支持注释 round-trip，Go 生态内可实现 ruamel 等价）。**此项需用户确认取舍**。

---

### M-06: memoryrpc DiscoverAndLoadMemoryFiles 两处吞 error 静默返回空

**Python 参考**（`memory_rpc.py:329-332`）：discover 内部对单文件 OSError 已容错，整体异常由注册层转 ok=False。

**Go 问题**（`memory_rpc.go:144、L253`）：`projectFiles, _ := projectmemory.DiscoverAndLoadMemoryFiles(...)` 两处吞 error——Status 统计与 List 展示在发现层故障时静默返回空列表，用户看到"0 个记忆文件"无法区分"真的没有"和"发现失败"。

**修复方案**：至少 Debug/Warn 日志记录 error；若 11.15 注册层有错误通道（见 M-04），Status 的 detailed 分支可将该 error 透传到响应。

---

## 四、提示问题 (T)

### T-01: SparseSearch 日志 query_text 用 byte 截断，CJK 可能截半个字符

`vector_store/milvus.go:365`：`truncateText(queryText, 100)` 是 byte 截断（定义于 chroma.go:936），中文 100 字节处可能截断在多字节字符中间产生乱码。项目已有 rune 截断先例（sweeper.go 等），建议统一 `utf8.RuneCountInString` + rune 切片截断。多出的 query_text 字段本身有排查价值可保留。

### T-02: 新增文件日志中英文混杂

`vector_store/milvus.go:367` "BM25 文本搜索失败"、`443` "混合搜索失败，降级到分别搜索"、`indexer/milvus.go:220` "检查索引存在性失败"、`495/510` "写入进度/写入完成" 为中文；`char_chunker.go:78` "character chunking completed" 为英文。项目规则（log-language-english 记忆）logger.Msg 已完成 700+ 处英文化，本次新增文件应统一英文。memoryrpc 包（L78 "memory.edit mkdir failed" 等）已是英文，无需处理。

### T-03: memoryrpc Edit preview 孤立 \r 残留（已声明的取舍，供确认）

Python `str.splitlines()` 把孤立 \r 也当行界（`"l1\r\nl2\r"` → `["l1","l2"]`）；Go（memory_rpc.go:57-59）只归一 `\r\n`，结尾孤立 \r 残留（`"l1\nl2\r"`）。L56 注释已声明取舍（2026-10-18 22:57 修复 commit 的有意决策），md 记忆文件极罕见。维持现状可接受。

### T-04: memoryrpc countLines 边缘差异两处

`memory_rpc.go:644-656`：
1. Python 文本模式 universal newlines：孤立 \r 也是行界（`"a\rb"` → 2 行），Go Scanner 只认 \n（→1 行）；
2. `bufio.Scanner` 默认 64KB 单行上限，超长行触发 `token too long` 后**静默停止**，行数少计且无错误信号——Status detailed 恰恰扫描 40000+ 字符大文件，若单行超 64KB 会少计。
建议改 `strings.Count(string(data), "\n")` 或 bufio.Reader 循环去行长上限。

### T-05: memoryrpc isForbiddenEnabled 对非 bool 真值语义差异

`memory_rpc.go:418-419`：Python `bool(forbidden.get("enabled", False))`——`enabled: "false"`（YAML 字符串）→ `bool("false")`=True；Go 类型断言失败 → false。仅配置误写场景触发，建议注释标注。

### T-06: memoryrpc expandUser 不支持 ~user 形式

`memory_rpc.go:611-619`：Python `os.path.expanduser` 支持 `~user/path`；Go 只处理 `~` 与 `~/`。白名单目录不含其他用户 home，实际不可达，可忽略。

### T-07: memoryrpc scanMDFiles 对"指向目录的 .md symlink"判定差异

`memory_rpc.go:672`：Python `entry.is_file()` follow symlink（指向目录 → False 排除）；Go `entry.IsDir()` 基于 Lstat 不 follow（symlink→目录 IsDir=false）→ 名为 x.md 的 symlink 指向目录会被误纳入且 countLines=0。极边缘。

### T-08: memoryrpc loadConfigMap 未应用 _normalize_config

`memory_rpc.go:698-708`：Python `get_config()` 含 `_normalize_config`（custom_headers JSON 解析 + web.send_file_allowed 默认）；Go Load 的 normFn 为 nil。对 memory/modes 字段无影响，仅提示存在差异。

### T-09: memoryrpc Status index 块 provider/model 空串 vs Python null

`memory_rpc.go:233-234`：Python `.get("provider")` → None 时 JSON null；Go 返回 ""（空串）。TUI 展示层差异，11.15 移植时注意判空方式（`if (provider)` JS 真值两边等价，`=== null` 不等价）。

### T-10: memoryrpc List 参数类型容错差异

`memory_rpc.go:119-131`：
- `include_project: 0`（非 bool）：Python truthy → False 不含 project 层；Go 断言失败 → 默认 true 包含。
- `additional_directories: ["/a", 123]`：Python `str(raw)` 转换保留；Go 静默丢弃非字符串项。
TUI 端恒传 bool/[]string，实际不可达。

### T-11: memoryrpc updateModeMemoryConfig 对"config.yaml 不存在"行为差异（Go 更健壮）

`memory_rpc.go:714-723`：Python round-trip load 文件不存在 → AttributeError → ok=False；Go Load 返回空 map → Set 自动创建新文件写入。Go 行为更合理，建议注释标注有意增强。

### T-12: memoryrpc 5 handler 未接入 channel 注册层

计划内待回填（11.15 TUI 面板注册，tui_connect.py L1903-1907 等价物）。`_resolve_project_dir` 回退链 / workspace 获取 / mode 默认 "plan" 均属注册层职责，Go handler 签名直接收参数，职责划分正确。**合规占位**。

### T-13: CheckVectorField variant 后缀检查缺失（计划内）

`vector_store/milvus.go:699-700`：Python `returned_type.endswith(variant)`，Go vector_fields 尚无 variant 字段（milvus_fields.go:50-51 foundation 层 TODO）。**合规占位**，跨层联动。

### T-14: hybridFactory 13.8 占位（计划内）

`chunker/registry.go:188-191`：返回显式错误 + ⤴️ 13.8 回填标记。**合规占位**。

### T-15: buildAnnParam SCANN 分支残留近似参数（S-01 联动）

`foundation/store/vector/milvus.go:1375` 附近 buildAnnParam 的 SCANN 搜索参数（nprobe）需在 S-01 修复后一并复核：NewIvfPQIndex 换 NewSCANNIndex 后，搜索侧 AnnParam 应对齐 SCANN 语义（nprobe + 可选 reorder_k，Python `reorder_k` 为 search 阶段参数）。IVF_PQ 的 m/nbits 搜索参数不应残留。

---

## 五、计划内占位确认清单（不算问题）

1. **hybridFactory 13.8**（registry.go L190）——合规，⤴️ 标记齐全，13.6 回填的 inner 缺省校验已落地。
2. **variant 后缀检查**（vector_store/milvus.go L699-700）——合规，依赖 foundation 层 variant 字段。
3. **SCANN→IVF_PQ**（indexer/milvus.go L671-673）——**占位前提已失效**（SDK 有构造器），随 S-01 修复，不是有效占位。
4. **text_splitter.go error 预留**——13.7 计划内。
5. **memoryrpc 5 handler 注册层**——11.15 计划内（T-12）。
6. **D3 Toggle 写回取舍**——已知取舍但代价未声明（M-05）。
7. **D6 写回失败 log + new_value 置 old_value**——已有测试覆盖，对齐决策。

---

## 六、对齐确认无误的关键点（验证 48h 内 commit 修复完整性）

- **13.3**：BuildIndex 五步顺序、ensureCollection BM25 Function（jieba analyzer + SPARSE_INVERTED_INDEX + BM25 metric）、dimension 三级推断、UpdateIndex 先删 flush 后建、DeleteIndex filter 表达式、GetIndexInfo row_count 解析、IndexExists 异常返 False、CreateMilvusAlias（md5 token）、Deduplicate 保序去重、错误码 155100~155112 逐字对齐、RRFFusion rank 从 1 计、CheckConfigsMatching efSearchFactor 跳过 + isclose 容差、自动建库 ListDatabase→CreateDatabase 链路——全部对齐。
- **13.5/13.6**：空文本 []、内联 CharSplitter 构造、155100/155101 构造校验、SetSelf 虚派发、registry 并发保护、rune 滑窗防中文截断、钳制顺序（size==0→200→clamp overlap→size<1→1）、chunk metadata 双写 ID/chunk_id——全部对齐。
- **10.6.15**：MemoryIndexManager 35 方法全清单覆盖（get/initialize/sync/watcher/reindex/search/缓存）；mergeHybridResults 对齐 `_merge_hybrid_results`；searchVectorFallback、buildFTSQuery、shouldFullReindex、EstimateTokens、ChunkMarkdown、HashText、Bm25RankToScore；embeddings 环境变量链（OPENAI_API_KEY/EMBED_API_KEY/MEMORY_EMBED_API_KEY）+降级 Mock——全部对齐。
- **10.6.16**：engine 门禁三函数（builtin/external/both/none 白名单）、GetExternalMemoryConfig 默认值填充（__default__/[]）、BuildOpenJiuwenProviderConfig（kv/vector/db/embedding 四段 + ltm 目录 + EMBED_* env 回退）、builder 四路分发 + is_available 检查 + 品牌映射（jiuwenswarm-user→uapclawswarm-user）——全部对齐（team-lead 独立复核确认）。
- **10.6.18**：17/17 函数全覆盖；白名单校验/特例文件（JIUWENSWARM.md→UAPCLAWSWARM.md）/品牌映射（.jiuwen→.uapclaw）/Status 六块顺序字段/detailed 分支/splitlines 主干语义（CRLF/尾换行/恰好 20 行）/Toggle 写回键路径——全部对齐，覆盖率 91.5%。
- **commmem 迁移**：harness/common/memory 包以 commmem 别名被 avatar_rail/deep_adapter 正确消费，6 个消费方接口完整。

---

## 七、修复优先级建议

1. **P0（一次 PR 修复）**：S-01 + S-02 + T-15——同一根因（buildDenseIndex 未用真实构造器 + constructConfig 口径脱钩），修 foundation/store/vector/milvus.go 与 indexer/milvus.go 两处，含 buildAnnParam 联动复核；
2. **P0（独立 PR）**：S-04——watcher 递归监听，10.6.15 增量同步核心功能；
3. **P1**：S-03（CJK 计数，memoryrpc + project_memory/files.go 跨包 4 处 rune 计数）、M-03（flush 错误传播）、M-04（Edit 假成功，11.15 前定好协议）；
4. **P2**：M-01（降级路径可观测 + idMapping 首次保留）、M-02（mtime 量纲，11.15 前修）、M-05（需用户确认取舍）、M-06（吞 error 加日志）；
5. **T 级**：随手修复（T-01/T-02 日志类），其余记录在案。

---

*本报告由 team-lead 汇总三路并行审查（13.3/13.5/13.6 检索线、10.6.15/10.6.16 swarm memory 线、10.6.18 memoryrpc 线）并独立验证全部 S/M 级发现后生成。10.6.15 MemoryIndexManager 全方法清单、10.6.16 External Memory 逐函数对比经 team-lead 二次复核确认对齐。*
