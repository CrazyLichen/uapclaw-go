# 2026-11-04 48 小时上库质量审查

> 审查范围：`f09f3d22~1 .. 3c0e3ad4`（2026-11-02 07:39 ~ 2026-11-04 01:38，非合并提交 269 个）
> 代码规模：507 文件 / +51,816 / −4,499（其中 `internal/` 生产代码 247 文件）
> 审查对象：提交原因与实现正确性；对齐 Python（`jiuwenswarm-develop/jiuwenswarm`、`agent-core/openjiuwen`）；桩/回填完整性；稳定性、性能、可维护性、并发安全
> 审查方式：逐条读生产代码 + 对照 Python 同位置源码；关键结论均在本地读码复核（下文 `文件:行号` 为 HEAD `3c0e3ad4` 基线）
> 只读审查，未修改任何文件
> 基线状态：`gh run list` 最新一次 CI 四绿（run 41686207313）

---

## 零、结论摘要

| 级别 | 数量 | 一句话 |
|---|---|---|
| 严重 | 8 | 记忆索引「重建」与「维度自愈」两条地基是空转；Windows 输出竞态修复未生效并引入挂死；coding/team 记忆丢 ctx 仍复现 P0；Pulsar 生命周期无同步；shell 非零退出码语义整体走反 |
| 一般 | 18 | 热重载并发无锁、演进 watcher 崩溃路径、web 通道桩回填缺口、Pulsar 订阅竞态与泄漏、编码覆盖不全、内存占用口径不自洽等 |
| 提示 | 26 | 验收链缺口、序列化宽度、可观测性、提交纪律、文档漂移、双实现分叉等 |

本次 48 小时的工作方向（中文分词、记忆检索重建判定、模型 token 可配化、热重载 rail 复用、Pulsar 骨架、skilldev 阶段 Agent、规划模式状态置位）都是正确的，且多数提交带有清晰根因说明与测试。但出现一个反复的模式：**「用新机制兜底」的前提没有验证**——`runReindex` 被当作分词器/模型变更的迁移手段，实际因 hash 跳过而不重写；`ensureVectorTable` 被当作维度自愈手段，实际只 DROP 不回灌；Invoke 的「等读完再 Wait」注释与代码相反。这类缺陷的共同特征是**注释/提交信息声称的行为与代码行为不一致**，单测和集成用例都只断言了「是否触发」而没有断言「是否真的生效」，因此 CI 全绿也发现不了。

---

## 一、严重（8 项）

### S-01 记忆索引「全量重建」不重建：分词器 / embedding 模型变更的迁移全部空转

- **归属流程**：记忆索引流程（`Sync` → `needsReindex` → `runReindex` → `syncMemoryFiles` → `indexFile`）
- **相关提交**：`a0751420`、`ff0690f6`、`14d98268`、`3fe6aede`、`71e0a523`（`docs/plans/2026-11-02-memory-retrieval-fixes*`）
- **证据**：

  `internal/agentcore/memory/lite/manager_impl.go:824-859`（`runReindex`）除了落 meta，只调 `syncMemoryFiles`：

  ```go
  if containsSource(m.settings.Sources, "memory") {
      if err := m.syncMemoryFiles(ctx); err != nil { return err }
      m.dirty = false
  }
  ```

  `manager_impl.go:878-883`（`syncMemoryFiles`）对内容未变的文件直接跳过，旧 `chunks` / `chunks_fts` 原样保留：

  ```go
  row := m.db.QueryRow("SELECT hash FROM files WHERE path = ? AND source = ?", entry.Path, "memory")
  if err := row.Scan(&hash); err == nil && hash == entry.Hash {
      continue        // ← indexFile 不会被调用，旧索引不重写
  }
  ```

  真正删除旧索引的 `DELETE FROM chunks/chunks_fts` 只在 `indexFile` 内（`manager_impl.go:1020-1023`）；随后 `runReindex` 照常把新 meta（含 `segmenter`）写下去（`:847-859`），下次判定即认为「已重建」。

- **影响与触发**：
  1. **升级存量 `memory.db`**：`3fe6aede` 把 FTS 文本从原文改成 `joinSegments(segmentForFTS(...))`。升级时 meta 无 `segmenter` 键 → 判 `segmenter_changed` → `runReindex` → 所有文件 hash 未变 → 全部跳过 → meta 写入新分词器标识。FTS 文本仍是旧原文，查询侧却已按分词生成 AND 表达式。是否还能命中完全取决于 sqlite `unicode61` 对 CJK 的切分方式（`128ab301` 自身就记录了「本地按整段、CI 上按字」不一致）；按整段切分的构建上，中文关键词检索会**永久零命中**，`force=true` 也只是再走一遍 no-op。
  2. 分词器 `none` → 可用（或开关切回）同样不重写 FTS。
  3. 换 embedding model / 补 key（mock → 真模型）同样不重算向量（叠加 S-02）。

- **Python 对照**：`openjiuwen/core/memory/lite/manager.py:616-635`（`_run_reindex`）与 `:650-667`（`_sync_memory_files` 的 hash 跳过）同构——即该缺陷是移植继承的。但本批提交把它当作「分词器/维度变化 → 全量重建」的兜底，**语义前提不成立**，新功能的地基是空的。
- **测试缺口**：`manager_reindex_test.go` 只断言 `needsReindex()` 返回值，无「老库 + 未改文件 + 分词器变化后 `chunks_fts` 真被重写」的用例。
- **建议**：`runReindex` 需要走「忽略 hash 的强制重写」路径（或先清 `chunks/chunks_fts` 再走 hash 判定），并补上述迁移用例。

### S-02 vec0 维度重建只 DROP 不回灌 + `Dims()` 硬编码 1024 → 向量索引永久为空，且每次 Sync 重复判定重建

- **归属流程**：记忆索引流程（`ensureVectorTable`）× 检索流程（`searchVector`）
- **相关提交**：`71e0a523`（维度自愈）、`a0751420`
- **证据**：

  `manager_impl.go:1237-1256` 解析 `sqlite_master` 得到真实维度，不一致即 DROP + CREATE，**没有任何回填动作**：

  ```go
  if needVecTableRebuild(existingSQL, dims) {
      if existingSQL != "" {
          _, _ = m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", vectorTable))
          // 日志写 "recreating"，但无任何 INSERT 回灌
      }
      m.db.Exec(fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])", vectorTable, dims))
  }
  ```

  vec 行只在 `indexChunkWithTx` 写（`:1084-1092`），而按 S-01 重建时所有文件被 hash 跳过 → 新表没有任何行。`searchVector`（`:1265`）只查 vec0，`vectorAvailable` 仍为 true，因此返回 0 行而**不会**触发 `searchVectorFallback`（`:1368-1370` 还会用 `len(vec) != len(queryVec)` 丢掉旧维度 BLOB）。

  同提交引入的维度初筛 `manager_impl.go:805`：

  ```go
  if dims, ok := metaInt(meta, "vectorDims"); ok && dims != m.provider.Dims() {
      return true, "vector_dims_changed"
  }
  ```

  而 `baseEmbeddingAdapter.Dims()` 恒返回构造时硬编码的 1024（`embeddings.go:143`、`:171`）。

- **影响与触发**：
  1. 配置先无 embed key → mock provider（128 维）建表；之后补 key → 1024 维 → `ensureVectorTable` DROP 128 维表并建 1024 维表 → 老文件向量全部消失且永不回灌。`chunks.embedding` 里其实还留着旧 BLOB，可恢复但无代码恢复。
  2. **任何非 1024 维模型**（含默认 mock 128 维）：`meta.vectorDims(实际 128) != provider.Dims()(1024)` 恒成立 → 每次 `Sync` 都判 `vector_dims_changed` 并跑一次全量扫描 + 写 meta（`Info` 级日志），把增量同步彻底废掉。search 前 `OnSearch` 预同步会让这个循环按消息粒度触发。
  3. `manager_impl.go:781-787` 的 `provider_unavailable` 分支声称「reindexing text only」，但 `indexChunkWithTx:1060` 直接 `m.provider.Model()`，真进入会 nil 解引用（当前不可达，属埋雷）。

- **Python 对照**：`manager.py:395-421` 的 `_ensure_vector_table` 同样 DROP 不重灌；但 Python 在新进程 `vector_dims is None` 时不 DROP（只是写入失败），Go 换成解析 `sqlite_master` 后，**把「静默写不进」升级成「主动丢表」**。
- **建议**：DROP 后强制重新 embed 全部 chunk（`chunks.embedding` 已有旧 BLOB 可先复用同维度场景）；`Dims()` 改为首次调用后按实际返回长度更新（参考 `internal/swarm/agents/harness/common/memory/embeddings.go:278-297` 的实现）。

### S-03 coding memory / team 成员记忆初始化仍丢 ctx → 沙箱下索引恒为空（同类 P0 只修了一条路径，提交信息与 diff 不符）

- **归属流程**：记忆初始化流程（`InitMemoryManagerAsync` / `InitCodingMemoryManagerAsync` / `Initialize`）
- **相关提交**：`833ae744`（标题即「manager 初始化改用调用方 ctx，修复沙箱拒绝导致索引永远为空」）
- **证据**：`833ae744` 提交信息写「`InitMemoryManagerAsync` / `InitCodingMemoryManagerAsync` 传调用方 ctx」，实际 diff 只给前者加了 `Ctx: ctx`（`internal/agentcore/memory/lite/tools.go:35-43`）。后者 `tools.go:64-72` 有 ctx 形参却未落字段：

  ```go
  func InitCodingMemoryManagerAsync(ctx context.Context, ...) (MemoryIndexManager, error) {
      params := MemoryManagerParams{ ..., NodeName: "coding_memory", LLM: llmModel }  // 缺 Ctx
  ```

  team 侧同理：`internal/agent_teams/memory/member_memory_toolkit.go:109-118` 所在方法有 `ctx` 却未传。兜底逻辑 `manager_impl.go:613-617` 于是回落到 `context.Background()`，即修复前的行为。调用方确实带着 ctx：`internal/agentcore/harness/rails/memory/coding_memory_rail.go:521-528`。

- **影响与触发**：`RestrictToSandbox=true` 且 `SysOperation != nil` 时，coding memory 与 team 成员记忆的首次全量索引读文件全部被判「路径在沙箱根目录之外」，`chunks/files` 恒为 0，只留 Error 日志，`memory_search` 永远返回空。与 `833ae744` 修的是同一个 bug 的同一条路径。
- **系统性风险**：`LiteMemoryToolContextBase.EnsureManager()`（`tool_context_base.go:81-112`）没有 ctx 形参，懒重建（`tool_ops.go:74/131`）永远带不上 `CwdState`；当前 lite 的 `Close` 只在一处调用，这条路径暂时基本不可达，一旦放开会复现同一 P0。
- **测试缺口**：`TestInitMemoryManagerAsync_透传ctx并建索引` 只覆盖非 coding 版本。
- **建议**：两处补 `Ctx`；把「ctx 必传」做成 `MemoryManagerParams` 的构造期校验（缺 ctx 直接报错而非静默 Background），避免第三次复发。

### S-04 Windows 输出丢失竞态「已修」但实际未修：`cmd.Wait()` 仍与管道读取并发 → >64KB 输出稳定截断

- **归属流程**：系统操作层 Invoke 流程（`AsyncProcessHandler.Invoke`）
- **相关提交**：`27cd2fde`（标题「修复 GBK 乱码 + Invoke 输出丢失竞态」）
- **证据**：`internal/agentcore/sys_operation/local/utils.go:196-217`，注释与代码互相矛盾：

  ```go
  // cmd.Wait() 会在进程退出后关闭管道，必须等收集协程读完再调，
  // 否则读协程未读走的尾部输出会被截断（竞态：间歇性空输出/丢尾部）。
  waitCh := make(chan error, 1)
  go func() { waitCh <- h.cmd.Wait() }()   // ← 收集协程刚起就立刻并发 Wait
  ...
  select {
  case <-collectDone:
  case <-ctx.Done():
  ```

  `os/exec` 的 `Wait()` 会 `closeDescriptors(c.parentIOPipes)`：进程一退出就关掉父进程读端，内核管道里未读字节直接丢弃。

- **影响与触发**：任何 >64KB（管道容量）的输出（`cat` 大文件、构建日志、`ls -R`、`base64 -w0`）尾部稳定丢失，提交描述的「间歇性」实际是**大输出必现**。用户可见输出缺失 / 后续 JSON 解析失败。现有单测只输出个位数字节（`encoding_test.go:95`），永远测不到。
- **Python 对照**：`openjiuwen/core/sys_operation/local/utils.py:160-193` 先 `await self._process.wait()` 再 `gather(readers)`；asyncio 的管道由 StreamReader 持有，wait 不会关它，顺序安全。
- **建议**：不要用 `StdoutPipe` + 并发 Wait；改用 `cmd.Stdout = 自定义 Writer`（exec 内部 copy goroutine，Wait 会等它），或严格「先等 collectDone 再 Wait」。

### S-05 收集协程提前退出后裸等 `Wait` 无超时 → 工具永久挂起（本轮重排引入的回归）

- **归属流程**：系统操作层 Invoke 流程（超时/取消路径）
- **相关提交**：`27cd2fde`
- **证据**：`utils.go:204-217`——超时 `select` 只覆盖「等 `collectDone`」阶段；一旦 `collectDone` 先关闭，最后一行没有任何超时/ctx 监听：

  ```go
  select {
  case <-collectDone:
  case <-ctx.Done():            // 只在等 collectDone 期间生效
      ...
  }
  waitErr := <-waitCh           // ← 无超时，永久阻塞点
  ```

  `cmd` 用的是外层 ctx（`shell_operation.go:186` 的 `exec.CommandContext`），Invoke 内部派生的 `WithTimeout` ctx 此时无人监听。

- **影响与触发**：任何单行 >64KB 的输出（`base64 -w0`、压缩后的单行 JSON/JS）：`bufio.Scanner` 默认上限 64KB，`Scan` 返回 false，且 `utils.go:171-194` **不检查 `scanner.Err()`** → 读协程退出 → `collectDone` 关闭 → 子进程因管道写满而阻塞 → `Wait` 永不返回 → **bash/powershell 工具永久挂起**。同样影响 `exec 1>&-` 这类「先关 stdout 再长跑」的命令。改之前 `waitProcess` 的 select 会监听 ctx，到点 kill，30s 内收尾 —— 超时语义被本次重排旁路。
- **Python 对照**：`utils.py:171-185` 用 `asyncio.wait_for(process.wait(), timeout)` 强制超时，kill 后 `_finish_readers(30)`。
- **建议**：把「等进程退出」放进同一个带超时的 select；`scanner.Buffer` 提高上限或改按字节读，并检查 `scanner.Err()`。

### S-06 非零退出码被折叠成「执行错误」→ bash 工具的退出码语义整体失效（与 Python 相反）

- **归属流程**：系统操作层 → shell 工具链路（`ExecuteCmd` → `bash`/`bash_stream`）
- **相关提交**：与 `27cd2fde`/`677846ab` 同文件域（`local/utils.go` 行为被重排），非本轮新写但本轮重写了该函数
- **证据**：
  - `utils.go:232-236`：非零退出的 `*exec.ExitError` 进 `data.Exception`，`data.ExitCode` 保持 0。
  - `shell_operation.go:232-241`：`invokeData.Exception != nil` → `createErrResult`（`BuildOperationErrorResult`，`Code != 0`）。
  - `internal/agentcore/harness/tools/shell/bash.go:213-240`：`if !res.IsSuccess()` 直接走失败路径返回 `success:false`，**永远到不了** `InterpretBashExitCode`（`bash.go:258`）。`bash_stream.go:221` 同理。
- **影响**：`grep` 无匹配（1）、`diff` 有差异（1）、`go test` 失败（1）等**语义性非零退出**全部上报为工具失败，模型拿到 `success:false` 而非「正常执行 + exit_code=1 + 语义解释」，`semantics.go` 整个模块对非零退出成为死代码。这是与 Python 的行为级偏差，影响面覆盖所有命令。
- **Python 对照**：`openjiuwen/core/sys_operation/local/shell_operation.py:519-535` 只把 `asyncio.TimeoutError` 当错误，非零退出返回 `SUCCESS` + 真实 `exit_code`，由 `_interpret_bash_exit_code` 判定语义。
- **建议**：`Invoke` 只用 `ProcessState.ExitCode()` 填充 `ExitCode`，非零退出不设 `Exception`；仅超时/取消设 `Exception`。并补「`grep` 无匹配 → success + exit 1」用例。

### S-07 Pulsar 队列生命周期无同步：Start/Stop 与 Produce/Subscribe 竞态 → nil client panic 与 client 泄漏

- **归属流程**：9.83 消息队列生命周期（`Start` / `Stop` / `Produce` / `Subscribe` 共享 `q.client`）
- **相关提交**：`926c8be3`、`ee88d1f3`、`c9ee0556`、`59ede8bd`、`7e5094a1`（已标 ✅）
- **证据**：`internal/agentcore/message_queue/pulsar.go`
  - `:133-149` `Start` 无锁写 `q.client` 后 `running.Store(true)`；
  - `:159-204` `Stop` 先 `running.Store(false)`，随后在 `q.mu` 内 `client := q.client; q.client = nil`，出锁后 `client.Close()`；
  - `:466-470` `produceMessage` 只在 `q.mu` **外**检查一次 `running`，之后进 `getOrCreateProducer`；
  - `:530` `producer, err := q.client.CreateProducer(topic)`（此时 `q.client` 可能已被 Stop 置 nil → nil 接口调用 panic）；
  - `:233` `Subscribe` 无锁读 `q.client`。
- **影响与触发**：
  1. Produce 通过 running 检查后阻塞在 `q.mu`，Stop 完成置 nil → `q.client.CreateProducer` **nil panic**（首次向该 topic 生产时）。
  2. Start 与 Stop 并发：终态可能 `running=true, client=nil`，此后每次 Produce/Subscribe 必 panic。
  3. 两个 Start 并发：先建的 client 永不 Close → 连接泄漏。
  4. `q.client` 在 Start 无锁写、Subscribe 无锁读、Stop/建 producer 持锁读写，锁使用不一致，`-race` 可检出。
  5. Stop 不等待在途 `Send`（Python `executor.shutdown(wait=True)` 会等），关闭期出现假失败。
- **当前可达性**：`NewMessageQueuePulsar` 在 `internal/`/`cmd/` 下**零生产引用**（仅测试），`runner.go` 未接线（9.84）。属**潜伏缺陷**，9.84 接线即暴露。
- **Python 对照**：`message_queue_pulsar.py:101-124` 单事件循环串行 + executor 排空。
- **建议**：9.84 接线前把生命周期收敛到一把锁/状态机（`client` 与 `running` 同锁读写），Stop 用 `sync.WaitGroup` 等在途发送。

### S-08 流中途 `DeadlineExceeded` 仍把半截答复当作最终答复（`b71f04ab` 只判 `Canceled`）

- **归属流程**：取消判定 / LLM 流收尾（`19549e67` + `b71f04ab` + `f638df34` + `c6cd13aa`）
- **证据**：
  - `internal/common/utils/ctxutil.go:29-34`：`Canceled` 只在 `errors.Is(ctx.Err(), context.Canceled)` 时为真，`DeadlineExceeded` 恒为 false。
  - `internal/agentcore/foundation/llm/model_clients/openai/client.go:535-539`（`FailStream`）：

    ```go
    select {
    case chunkChan <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkStreamError(c.WrapError("stream", err))):
    case <-ctx.Done():      // deadline 已触发 → 错误哨兵可能被丢弃
    }
    ```

    两个 case 同时就绪时 Go 随机选择，因此哨兵**间歇性丢失**。
  - `react_model_call.go:299-307`：通道干净关闭后只判 `utils.Canceled(ctx)` → deadline 场景为 false → 继续把 `accumulated.ToAssistantMessage()` 当最终答复返回；`react_invoke.go:260-265` 写流前同样只判 `Canceled`。
- **触发条件（真实存在）**：team 成员单轮 `context.WithTimeout`（`agent_teams/agent/stream_controller.go:708`，默认 600s）、auto-invoke 10 分钟（`harness/deep_agent.go:662`）、任何上游 deadline；HTTP 请求绑定 ctx（`openai/client.go:267-276`）。
- **影响**：超时被切断的半截回答会作为 `chat.final` 下发前端并落 history，用户看到「答到一半就结束」且无错误提示；`b71f04ab` 的修复目标（避免半截回答被当成最终答复）在 deadline 路径未达成。
- **Python 对照**：`openjiuwen/core/single_agent/agents/react_agent.py:764-777` 在 `async for` 内 await，`TimeoutError` 直接抛出 → `:1549` `except Exception` 走 error 结果。
- **测试缺口**：`react_model_call_test.go:37-50` 在调用**之前**就 cancel，且 fake client 忽略 ctx，无法复现「流进行中 deadline → 哨兵丢失 → 干净关闭」。
- **建议**：守卫放宽为 `ctx.Err() != nil`（返回 `ctx.Err()`），deadline 由 `shouldReportStreamError` 正常上报；补「流中途 deadline」用例。

---

## 二、一般（18 项）

### M-01 热重载把新配置写进已存在会话上下文时完全无锁 → 与并发会话的数据竞争

- **归属流程**：配置热重载流程 × 会话上下文读写流程
- **相关提交**：`830e5bd8`（新增 `ApplyRuntimeConfig` 推送）
- **证据**：`internal/agentcore/context_engine/context/session_model_context.go:194-215`

  ```go
  func (mc *SessionModelContext) ApplyRuntimeConfig(config ceschema.ContextEngineConfig, processors []iface.ContextProcessor) {
      mc.modelName = config.ModelName
      mc.contextWindowTokens = config.ContextWindowTokens
      ...
      mc.messageBuffer.SetMaxSize(config.MaxContextMessageNum)
      mc.processors = processors          // 无任何锁
  }
  ```

  而其他路径读 `mc.processors` 时是持 `processorLock` 的（`AddMessages:353/369`、`CompressContext:425`、`:778`），读 `mc.modelName/contextWindowTokens`（`resolveContextMax`）则完全不持锁。调用方 `engine.go:431-437`（`applyPendingLocked`）持的是 `ce.mu`，与 `processorLock` 不是同一把锁。
- **影响**：热重载（用户改配置）与另一个会话正在跑（`AddMessages` / 压缩 / `ContextMaxTokens`）并发时：slice 头三字非原子写，`-race` 必报；极端时序下 range 到撕裂的 slice 头可能越界 panic。功能上也可能出现「新处理器实例与旧实例混用一回合」。
- **补充**：`Processors()`（`:226`）与 `ContextMaxTokens()`（`:236`）作为「诊断接口」同样无锁返回。
- **建议**：`ApplyRuntimeConfig` 持 `processorLock`（或改用 `atomic.Pointer` 存 processor 切片），派生字段用 `atomic.Int64` 或在一把 RWMutex 下读写。

### M-02 演进 watcher 后台 goroutine 无锁读 `skillEvolutionRail` + 无 recover → 模式切换时 TOCTOU nil 崩溃

- **归属流程**：实例创建/每轮请求的 rail 模式切换流程 × 被动演进监视流程
- **相关提交**：`c16f12af`、`696090b4`、`7d30dfd5`（确立「字段非 nil ⇒ 已注册」不变量，并在 `updateAgentModeRails` 中置 nil）
- **证据**：
  - `deep_adapter.go:1428-1433`：`go func(){ _ = d.watchEvolutionAndPush(...) }()`，goroutine 内**无 recover**。
  - `deep_adapter_evolution.go:176-181、231-254`：循环里先判 `if d.skillEvolutionRail == nil { return }`，随后多次直接解引用（`AutoScan()`、`DrainPendingApprovalEvents`）。
  - 写侧 `deep_adapter_rails.go:1189`（`updateAgentModeRails` 置 nil）、`:1111`（plan 模式注册）、`deep_adapter_slash.go:97` 全部**无锁**（`deep_adapter.go` 只有 `stateMu`/`sessionMu`，rail 字段不在保护范围）。
- **影响与触发**：会话 A 在 plan 模式触发 watcher，会话 B（同实例）以 normal 模式发消息 → `updateAgentModeRails` 把字段置 nil → A 的 watcher 在判空与解引用之间命中 nil → **后台 goroutine panic 直接终止进程**。窗口窄但路径真实；`-race` 亦可检出。
- **建议**：rail 字段统一走 `stateMu`（读侧在循环内取一次快照）；后台 goroutine 一律加 recover（本项目 `ability` 层已确立同款原则）。

### M-03 `SkillCreateRail.proposalSent` 跨会话共享且无同步

- **归属流程**：技能创建提议流程（新移植）
- **相关提交**：`a2f63473`、`06e1ba7a`
- **证据**：`internal/agentcore/harness/rails/evolution/skill_create_rail.go:38` `proposalSent bool`；`:127-129` `OnBeforeInvoke` 置 false；`:180-183` 读；`:221` 置 true。rail 实例挂在 adapter 上（`deep_adapter_rails.go:1138`），**多会话共用同一实例**。
- **影响**：会话 A 的 `OnBeforeInvoke` 会清掉会话 B 的「已提议」标记 → B 可能重复注入 follow-up；两请求并发时是普通 bool 读写竞态（`-race` 可检出）。Python 同为共享实例（`skill_create_rail.py:73,77,85`），但 Python 单线程无 UB。
- **建议**：按 `sessionID` 分桶（map + mutex），或把「已提议」下沉到 round 级上下文。

### M-04 web 通道 `path.get/set`、`locale.get/set`、`hooks.list` 仍是桩，且 `path.get` 键名与前端契约不符

- **归属流程**：Web 通道 RPC（11.14 已标 ✅）
- **相关提交**：非本轮引入（`519feede`，2026-07-07），但依赖章节此后已全部就绪（11.10 Cron ✅ / 11.11 Heartbeat ✅ / 11.14 config round-trip ✅ / hooks loader 已在 `common/hooks`），属**该回填未回填**
- **证据**：`internal/swarm/gateway/channel_manager/web/web_handlers.go:313-314、321-322、347`

  ```go
  d.Register("path.get", stubHandler("path.get", map[string]any{"path": ""}))
  d.Register("path.set", stubHandler("path.set", map[string]any{"ok": true}))
  d.Register("locale.get_conf", stubHandler("locale.get_conf", map[string]any{"preferred_language": "zh"}))
  d.Register("hooks.list", stubHandler("hooks.list", map[string]any{"hooks": []any{}}))
  ```

  前端实际消费：`frontend/src/components/BrowserPanel/index.tsx:4-6,19-20`（读 `chrome_path`，Go 返回 `path` → 永远空）、`:91`（`path.set` 不落盘）、`frontend/src/App.tsx:710`（读 `preferred_language`，恒 `zh`）。
- **Python 对照**：`app_web_handlers.py:1366-1412`（`_path_get/_path_set` 校验 + `update_browser_in_config` 写回 + 清 agent 缓存）、`:1468-1500`（`_locale_get_conf/_set_conf` 读/写 `preferred_language`，非法值 `BAD_REQUEST`）、`:2211-2225`（`_hooks_list` 返回 `{events, disable_all_hooks, source}`）。
- **影响**：浏览器路径设置「填了不生效、重连即丢」；语言切换不持久化；hooks 面板永远空（返回形状也与 Python 不同，未来接前端会踩坑）。三条都是「返回成功但什么也没做」的静默谎言。
- **建议**：按 Python 三处实现回填（read 侧 `browser.chrome_path` 已被 `deep_adapter_config.go:876` 消费，写侧 `config.SetRoundTrip` 已具备）。

### M-05 Pulsar 订阅「先查后建」竞态 + 未激活订阅的 consumer 永不关闭

- **归属流程**：9.83 订阅管理 / 订阅生命周期
- **证据**：`pulsar.go:226-246` 锁在创建 consumer 前释放：

  ```go
  q.mu.Lock()
  if sub, ok := q.subs[topic]; ok { q.mu.Unlock(); return sub, nil }
  q.mu.Unlock()                      // ← 先放锁
  consumer, err := q.client.Subscribe(topic, DefaultSubscriptionName)
  ...
  q.mu.Lock(); q.subs[topic] = sub; q.mu.Unlock()
  ```

  `pulsar.go:334-339` `Deactivate` 首行 `if !s.active.Load() { return }` → 未激活就退订时**不 Close consumer**；而 `pulsar_api.go:114` 的 SDK `Client.Close` 只关 handlers/cnxPool，不遍历 consumer（consumer 自己的 `Close()` 才 `closeCh`/`handlers.Del`）。
- **影响**：同 topic 并发订阅 → 两个 consumer，后写覆盖，先建者永不关闭（泄漏）；未 `Activate` 就 `Unsubscribe`/`Stop` → consumer 泄漏到进程退出（goroutine + 连接）。
- **Python 对照**：`message_queue_pulsar.py:126-139` 单线程无交错；`:41-43` 同样早退（Python client.close 是否兜底**待核**）。
- **建议**：订阅创建用 per-topic single-flight；`Deactivate` 在未激活时也关闭 consumer。

### M-06 Pulsar `Deactivate` 同步等 handler → 退订/Stop 可无限阻塞（`Stop` 的 ctx 未使用）

- **证据**：`pulsar.go:334-352` `cancel(); s.consumer.Close(); <-done`；消费循环 `:429` 同步调 handler；`Stop(ctx)` 签名收 ctx，函数体无引用（`:159-204`）。注释「最长等待一个 receive 超时周期」只在无 handler 执行时成立。
- **影响**：handler 卡在不可取消的外部 IO → `Unsubscribe` 永久阻塞 → `Stop` 永久阻塞 → 进程优雅退出挂死。
- **Python 对照**：`message_queue_pulsar.py:46-57` `task.cancel()` 会把 CancelledError 抛进 `await handler`，可中断。
- **建议**：`done` 等待加超时（并记日志），或给 handler 传可取消 ctx 后强制返回。

### M-07 Pulsar 消费回调无 recover → handler panic 直接终止进程

- **证据**：`pulsar.go:429` → `callHandler:452-462` 直接 `handler(ctx, payload)`；包内 `grep recover()` 为 0。
- **影响**：handler 内 nil map 写 / 类型断言 / 越界 → 整个 agent 进程退出，而不是丢一条消息。
- **Python 对照**：`message_queue_pulsar.py:77-83` handler 异常落入 `except Exception` 记日志后继续。
- **建议**：消费循环内 `defer recover()`，记 Error + 不 ack。

### M-08 dmq `payload` 收窄为 `map[string]any`，与 Python `Any | list` 的 wire 不互通

- **证据**：`internal/agentcore/runner/message_queue/base.go:72-81` `Payload map[string]any`；`dmessage_queue/message.go:14-55` 内嵌该字段；`message_serializer.go:101-133` 直接 `json.Unmarshal` 到该结构。
- **影响**：对端（Python）发来数组/标量 payload 时 `json.Unmarshal` 报 `cannot unmarshal array into map[string]interface{}`，消息既不处理也不 ack（可能重投/占位）。主链路 dict 场景暂不触发。
- **Python 对照**：`core/runner/message_queue_base.py:16` `payload: Any`；`dmessage_queue/message.py:41` `Union[Any, list]`；`server_adapter/mq_message_utils.py:23-32` 把任意返回值塞进 payload。
- **建议**：payload 改 `any`，序列化侧按类型分派。

### M-09 Windows 解码覆盖不全（cp932/cp1252 仍乱码）+ 解码失败整行回退（Python 用 `errors="replace"`）

- **证据**：`local/utils.go:551-573` 每个失败分支都 `return string(raw)`（整行原样，含非法 UTF-8）；`encoding_windows.go:14-15` 用 `"cp"+GetACP()` 交给 `ianaindex` 解析——实测 `cp936`→GBK、`cp950`→nil（走直映射）、**`cp932`/`cp1252`/`cp65001` 均无法解析** → 回退原文。
- **影响**：日文 Windows(932)、西欧(1252) 依旧乱码（本次修复只覆盖简中/Big5）；GBK 行内混少量坏字节时 Go 整行乱、Python 只坏几个字符。
- **Python 对照**：`utils.py:149-150,354` `decode(encoding, errors="replace")`。
- **补充**：`GetACP`（ANSI）与 cmd/PowerShell 实际输出用的 OEM 代码页（`GetConsoleOutputCP`）在非中日韩系统上不同，需要决定取哪个（Python `locale.getpreferredencoding` 取 ACP）。
- **建议**：补 `charmap.Windows1252/Japanese` 显式映射；解码失败改逐字节替换而非整行回退。

### M-10 `fs.ReadFileStream` 文本模式 rune 边界回退可能死循环/负索引

- **证据**：`local/fs_operation.go:221-233`：

  ```go
  end := offset + chunkSize
  for end < len(data) && (data[end]&0xC0) == 0x80 { end-- }
  chunk := data[offset:end]; ...; offset = end
  ```

  `chunkSize ≤ 3` 且块首是 3 字节汉字时 `end` 回退到 `offset` → 空块、`offset` 不变 → 死循环（channel 满 64 块后消费者永久等待，且 goroutine 内无 ctx 检查）；数据首字节是续字节时 `end < offset` → 负索引 panic。
- **可达性**：当前 `chunk_size` 未从工具层透传（见 M-11），仅程序化 `WithFsChunkSize(1..3)` 触发；测试只覆盖 `chunkSize=5`（`encoding_test.go:235`）。
- **建议**：入口钳制 `chunkSize >= 4`，回退循环加 `end > offset` 下界。

### M-11 `read_file` 工具未透传 `encoding`/`chunk_size` 等参数 → 编码修复对真实工具调用不生效

- **证据**：`internal/agentcore/harness/tools/filesystem/tool_adapter.go:211-219` 只透传 `mode`，丢弃 `encoding/head/tail/line_range/chunk_size`；而 schema（`fs_operation.go:783-791`）已向模型声明这些参数。
- **影响**：模型传 `encoding="gbk"` 仍走 utf-8 默认；`27cd2fde` 新增的 `ReadFile` 解码只有直接调 API 的路径（单测/集成）能验证，工具链路无感。属「修了解码但没接线」。
- **建议**：补齐参数透传，并补一条经工具层的 GBK 读文件用例。

### M-12 Shell 进程在 Invoke **返回之后**才登记 → 取消/KillProcess 打不到运行中的命令

- **证据**：`local/shell_operation.go:216-222`

  ```go
  invokeData, err := handler.Invoke(ctx)
  if handler.cmd.Process != nil {
      sid := trackShellProcess(ctx, handler.cmd.Process)   // ← 进程已结束才登记
      defer untrackShellProcess(sid, handler.cmd.Process)
  }
  ```

  `KillProcess`（`:626-642`）与 `KillShellProcessesForSession` 全靠该注册表。
- **Python 对照**：`shell_operation.py:511`（invoke **前** `_track_shell_process`）↔ `:512-514`（`finally` untrack）。
- **影响**：会话取消/中断时打不到正在跑的前台命令（长命令杀不掉）；注册表里实际只有一个已死进程，`ListProcesses` 语义也变得无意义。
- **建议**：`ExecuteCmd` 拆出 Start 步骤，Start 成功后立即登记。

### M-13 记忆 embedding 失败静默吞掉（无日志）且 hash 照常提交 → 该 chunk 永久无向量

- **证据**：`manager_impl.go:1050-1054`

  ```go
  emb, err := m.getEmbeddingWithTx(ctx, db, chunk.Text)
  if err == nil && emb != nil { embBlob = vectorToBlob(emb) }   // err 直接丢弃，无日志
  ```

  `indexChunkWithTx` 仍返回 nil，`indexFile` 继续 `INSERT OR REPLACE INTO files`（`:1031`）提交 hash → 该文件永不重试。
- **Python 对照**：`manager.py:849-852` `logger.error("Failed to get embedding: %s")`。
- **影响**：embedding 服务瞬时故障 → 这批 chunk 永久只存在于 FTS，磁盘上无任何线索。
- **建议**：至少补 Error 日志；或「embedding 失败则不提交该文件 hash」以便下次重试。

### M-14 文档/提交声明与实际实现不符（两处）

1. `833ae744` 声称两个 init 函数都传 ctx，实际只改一个（见 S-03）。
2. `27cd2fde` 声称修复 Invoke 输出丢失竞态，实际代码注释与实现相反（见 S-04）。
- **影响**：后续读者会按提交信息认为该问题已关闭，掩盖真实缺陷；本次审查即因此需要逐行复核。
- **建议**：修完把结论写成「已通过 <用例名> 验证」的形式，并把该用例作为提交的一部分。

### M-15 `memory.compute` 的 `used_percent` 与同帧 `available_mb` 口径不一致（Linux 实测差约 4 个百分点）

- **归属流程**：Web 通道 `memory.compute`（Go 扩展字段）
- **相关提交**：`626c8d3a`、`c0a236cd`
- **证据**：`internal/swarm/gateway/channel_manager/web/memory_compute.go:70` 直接透传 `vm.UsedPercent`。gopsutil v3 `mem/mem_linux.go:323,333-334`：

  ```go
  ret.Cached += ret.Sreclaimable
  ret.Used = ret.Total - ret.Free - ret.Buffers - ret.Cached
  ret.UsedPercent = float64(ret.Used) / float64(ret.Total) * 100.0
  ```

  即「扣除缓存」口径；而同一 payload 的 `available_mb` 取 `vm.Available`（≈ psutil `available`，即 `(total-available)/total` 口径）。Windows 上 gopsutil 走 `dwMemoryLoad`（available 口径），同一 UI 指标在 Linux/Windows 含义不同。
- **影响**：前端 `ToolPanel/index.tsx:244` 以 `(xx.x%)` 展示，用户看到「占用 24%」而同帧 `available_mb` 隐含「已用 28%」，两字段自相矛盾。
- **Python 对照**：`app_web_handlers.py:1420-1423` 取 `psutil.virtual_memory().percent` = `(total-available)/total`，但**未放进 payload**（本字段是 Go 补的超集，无逐字对齐义务），因此这里应按「自身口径自洽」定标准。
- **建议**：`used_percent = (Total-Available)/Total*100`，与 `available_mb` 及 Windows 分支统一。

### M-16 规划模式把只读的 `todo_get` 一起隐藏，与 Python `_TODO_TOOL_NAMES` 不一致（提交称「完全对齐」）

- **归属流程**：规划模式 todo（`08225dfb` revert 方案 A）
- **证据**：`internal/agentcore/harness/rails/agent_mode.go:80-82`

  ```go
  // Python: L44: _TODO_TOOL_NAMES
  var todoToolNames = map[string]struct{}{
      "todo_create": {}, "todo_list": {}, "todo_get": {}, "todo_modify": {},
  }
  ```

  该集合驱动 `BeforeModelCall` 的工具过滤（`:294`）与 `BeforeToolCall` 的硬拦（`:344-353`）。
- **Python 对照**：`openjiuwen/harness/rails/agent_mode_rail.py:44` 仅 `todo_create/todo_list/todo_modify` → Python 规划模式仍把只读的 `todo_get` 暴露给模型。
- **影响**：规划模式下模型看不到也调不到 `todo_get`（本可用于读取现有任务清单），与 Python 行为不同；若自定义 `allowedTools` 含 `todo_get`，Go 会在白名单判定之前就被硬拦。
- **建议**：去掉 `todo_get`；若确需额外隐藏，请在注释与提交说明中标注为 Go 侧偏离。

### M-17 `PyText` 用 `json.Marshal` 默认转义 → 工具结果里的 `< > &` 变成 `\u003c`（项目已有专用不转义助手）

- **归属流程**：工具结果展示（`b17c8de4` 引入、`60a924dc` 统一使用）
- **证据**：`internal/common/utils/pystr.go:41-44`

  ```go
  if b, err := json.Marshal(v); err == nil { return string(b) }
  ```

  仓库内已有 `internal/common/utils/pyjson.go:26 MarshalNoEscape`（`SetEscapeHTML(false)`，注释明确说这是坑，`history.json` 写盘已因同一问题修过）。
- **影响**：读取 HTML/JS、`grep`/`diff` 结果、代码片段时，工具卡片与历史回放显示 `\u003cdiv\u003e` 一类转义串；A2A 侧把该串当文本直出（`protocol/a2a/a2a_parts.go:183,201`）。本次「消除 `map[...]` 泄漏」的展示改造因此只做了一半。叠加超长结果在 60000 字符处截断（`stream_event_emit.go:70`），截断后既不是合法 JSON 也不可读。
- **建议**：`PyText` 改用 `MarshalNoEscape`；超长结果不要从 JSON 中间切，改为截断到摘要字段。

### M-18 `StreamController.cancelRound` 跨 goroutine 无锁读写（既有，文件本轮被改）

- **归属流程**：team 取消（`CoroperativeCancel` 两阶段取消）
- **证据**：写 `agent_teams/agent/stream_controller.go:545`（`startRound`）与 `:601`（`runOneRound` defer 置 nil），读 `:408-409`（`CooperativeCancel` 2s 超时后强制 `cancelRound()`）；而同一结构体的 `mu` 注释（`:33`）声称保护共享字段，`roundDone` 也确实持锁写（`:547-549`）。
- **影响**：`-race` 可红；最坏情况下读到上一轮遗留的 `cancelRound`，取消掉错误的轮次。
- **建议**：`cancelRound` 与 `roundDone` 一起在 `sc.mu` 内快照/清空。

---

## 三、提示（26 项）

| 编号 | 主题 | 证据与说明 |
|---|---|---|
| T-01 | 9.83 验收链缺口 | `message_queue/pulsar_integration_test.go:1,22-25` 需 `PULSAR_URL` 否则 `t.Skip`；CI 集成 job 只跑 `./tests/integration/...`（`.github/workflows/ci.yml:329-332`）且未设 `PULSAR_URL` → 该用例**永不执行**。`IMPLEMENTATION_PLAN.md` 已标 ✅，设计 §1.2 也声明「不做真实 broker 验证」，故不判缺陷，但 ✅ 只代表 fake 单测 + 覆盖率，不代表传输层可用 |
| T-02 | 失败消息只「不 ack」不 nack | `pulsar.go:435-436` 注释「触发重投」，但 `pulsar_api.go:101-106` 未配 AckTimeout/RetryEnable/DLQ，无 Nack 接口；坏 JSON 会永久占住一条消息份额。Python 同样只记录不 ack（两边都缺） |
| T-03 | ConsumerClosed 无退避热循环 | `pulsar.go:385-398` 非超时错误直接 `continue`；`Activate→Deactivate→Activate` 后 Receive 立即返回 ConsumerClosed 且 `ctx.Err()==nil` → 100% CPU 日志风暴 |
| T-04 | dmq 序列化宽度 | `message_serializer.go:142-177` 只处理 nil/`time.Time`/`map[string]any`/`[]any`，`default` 原样返回；Python `message_serializer.py:46-82` 处理任意 Enum / `model_dump` 结构体并加 `__class__`；`:150-154` `Format(RFC3339)` 丢亚秒精度（Python `isoformat()` 保留微秒） |
| T-05 | Pulsar 可观测小项 | `pulsar.go:283-295` 透传 BaseError 时丢 topic；`message_serializer.go:75-81` 硬编码 `WithParam("topic","")` 导致消费侧打出「produce message error, topic=''」；`:197-200` 「closing producers」日志打在关闭之后；`Start:138-146` 建 client 失败只记日志（Go 接口无返回值），9.84 无法感知启动失败 |
| T-06 | `sdkConsumer.Ack` 类型不符静默成功 | `pulsar_api.go:126-132` `if !ok { return nil }`，未来换适配器会静默不 ack，且被 `TestSDKConsumer_Ack非sdkMessage` 固化为期望 |
| T-07 | 关键词-only 命中被 `MinScore` 过滤 | `config.go:137` `MinScore=0.3`、`TextWeight=0.3`；`manager_impl.go:1495` 加权后 `:334-337` 过滤 → 纯 FTS 命中得分恒 <0.3（BM25 rank 为负）；query embedding 失败降级后 Search 直接返回空。与 Python 一致（非回归），但「本批把关键词检索改成 AND 精确命中」的收益在默认配置下无法体现，建议产品确认 |
| T-08 | `min_score=0` / `max_results=0` 被零值吞掉 | `manager_impl.go:237-245` 显式 0 被替换成默认值；Python `manager.py:876-877` 用 `"min_score" in opts` 保留显式 0。工具参数 `min_score=0`（要全部）实际仍按 0.3 过滤 |
| T-09 | contentless FTS `DELETE ... WHERE path=?` 可能删不掉（**待核**，存量） | `manager_impl.go:682-684` 建 `content='' contentless_delete=1` 表，`:1021-1023` 按 `path` 删且忽略错误；contentless 表不保存列值，按非 rowid 过滤匹配不到行 → 文件更新后 FTS 死行无界增长，挤占 `ORDER BY rank LIMIT candidates` 窗口 |
| T-10 | gse 分词器并发与预热 | `segmenter.go:39-49` 全局单例 + `sync.Once`，`Cut` 被所有 manager 共用；读源码看当前路径只读字典（大概率安全），但 gse 未声明并发安全，建议补一个 `-race` 并发 Cut 用例。`memory_rail.go:91` 无条件预热（35.9MB 词典），`MEMORY_ENABLED=false` 时也加载；coding memory 无预热，首次索引在事务内同步加载。kill switch 注释「无需重启」与实现（`segOnce` 一次性）冲突 |
| T-11 | 两份 memory 实现语义分叉 | `internal/agentcore/memory/lite`（本轮修好：分词 + AND + 5 token）vs `internal/swarm/agents/harness/common/memory/internal.go:221-233`（仍 `\w+` + 前 10 OR + 原文索引），而 `deep_adapter.go:711` 在用的是后者 → 同一产品不同 agent 检索表现不一致，需确认主路径 |
| T-12 | `buildRenderedSystemPrompt` 缺占位符渲染 | `react_invoke.go:762-782` 只拼接 `config.PromptTemplate` 里 `role=system` 的原始 content；Python `react_agent.py:582-601` + `:813-844` 先用 `PromptTemplate(content).format(render_fields)` 渲染 `inputs`/`query` 占位符。当前模板多为静态文本（`deep_agent.go:1795` 的快照、skilldev 静态 prompt）故未暴露；一旦内容含 `{var}` 会原样泄漏。另：本次修复靠「渲染源换成固定快照」消除自我嵌套，语义正确但对 deep 主链路而言写入的是创建期快照，建议在注释里点明 |
| T-13 | 每次 LLM 请求新建 `http.Transport`（既有，本轮影响放大） | `openai/request_builder.go:123-158` 每次 `BuildHTTPRequest` 都 `&http.Transport{}` + 新 `http.Client`：无连接复用（每轮多次 LLM 调用都要重新 TCP+TLS 握手）、`ForceAttemptHTTP2` 失效、idle 连接与 readLoop 只能等 GC。`c6cd13aa` 让配置 timeout(1800s) 真正生效后，长流的总超时语义与 Python 的 read-idle 语义也不同（建议用 `ResponseHeaderTimeout` + 逐块读超时） |
| T-14 | 每条消息都跑一次 session 生命周期与 rail 装卸 | `227895fb` 使 `updateRailsForMode` 每请求调用 `SwitchMode`（`deep_adapter_rails.go:1002-1012`）→ `Session.PreRun/PostRun`（`session/agent.go`，含 checkpointer 与 Commit）。这是对 Python 的有意偏离（注释已说明），但请注意 plan/normal 反复切换时的 I/O 与状态覆盖成本，以及 `PrePlanMode` 记账被逐条消息重置的语义变化 |
| T-15 | 覆盖豁免文件高频增长 | 48h 内 23 次改动 `.coverage-exempt` / `.integration-coverage-exempt`，现为 219 + 181 行。抽查（`3f07b835` 的 `manager.go`/`vec_table.go`）理由充分、可复核，但「改动行覆盖」门禁的有效性正被逐轮削弱，建议定期复核整文件级豁免（尤其 `vec_table.go` 这类可单测文件） |
| T-16 | `resolveProjectDir` 回退语义与 Python 不同 | `tui_memory_handlers.go:117-125` 在 `trusted_dirs[0]` 为空串/非字符串时**继续回退 cwd**；Python `tui_connect.py:1835-1837` 只要是非空 list 就直接 `return str(trusted_dirs[0])`（可为空）。差异后果：`memory.list/status` 可能扫到 cwd 而非 workspace，且 `memory.edit` 白名单会因 `getAllowedDirs` 追加 cwd（`memory_rpc.go:476-479`）而放宽。Go 方向更"有结果"，但属读错目录类风险，建议按 Python early-return 收敛 |
| T-17 | 文档漂移：仍写「5 个本地 handler」 | `/tui` 现注册 10 个（5 chat/history + 5 memory，`1b40d1d0`），但 `tui/doc.go:4`、`tui_connect.go:232,253`、`app_gateway.go:446-448`（还写着「memory.* 待 11.15 补齐」）、`gateway_server.go:228` 未同步。违反项目规则 1「修改包功能时须更新包功能概述」 |
| T-18 | 集成用例触发全局 MemoryIndexManager 未回收 | `tests/integration/swarm/gateway/tui_cli_route_test.go:522` 发 `memory.status{detailed:true}` → `commmem.GetMemoryManager`；默认 `Sync.Watch=true` 会起 watcher + sqlite，`TearDownSuite` 无 `ClearMemoryManagerCache`/`Close`。TempDir 被删后定时器仍可能刷错误日志（`memory/manager_test.go:49-66` 有回收先例） |
| T-19 | web 失败帧 `error` 缺 `handler error: ` 前缀且重复记日志 | `web_connect.go:260-266` 直接 `err.Error()`，Python `web_connect.py:532-537` 为 `f"handler error: {e}"`（键名/类型/错误码一致，仅文案）；同时 `Dispatch`（`web_handlers.go:580-585`）与本 handler 各记一次 Warn。建议对齐文案并去掉重复日志 |
| T-20 | skilldev 阶段工具「隔离」在 ID 冲突时静默退化 | `skilldev/context.go:296-306`：`rm.AddTool(stageTool)` 失败时**只记 Debug**，随后仍 `agent.AbilityManager().Add(stageTool.Card())` —— 卡片指向的 ID 在 ResourceMrg 里可能是**另一个阶段**的实例（不同 `SysOperation`/workspace），工具隔离静默失效且无告警。另 `cfg.SysOperationID = sysOpID`（`:238`）在 `NewReActAgent(card, cfg)` 之后才赋值，依赖 cfg 指针别名生效（`:206-210` 构造 → `:228` 注入 CE → `:238` 补 SysOperationID），建议构造前一次填全。上游 `create_stage_agent` 目前是 `NotImplementedError` 桩（`context.go:158` 注释），本实现为 Go 先行，后续需按上游实际实现回归 |
| T-21 | `context.usage` 的 `rate` 无上界 | `stream_event_emit.go:222-231` `rate = tokens_used / context_max * 100`，未夹紧：单次响应大于窗口时 `rate > 100%`，前端直接按百分比展示。口径本身（`total_tokens` 含输出，`39eef1de`）已论证充分且同步了设计稿修订说明，仅建议展示层夹紧或加「>100%」文案 |

**另附提交纪律（不计级别）**：

- `58e81205`（注释整理 + 消除重复实现）删/改了 `formatTodosForFrontend`、`formatTodosForFrontendLocal` 但测试引用未同步提交 → main 红灯（CI 41683132472 Lint + Unit Test `undefined: formatTodosForFrontend`），由 `7b1a3ae2` 补齐。根因是把「重命名另一半」留在工作区未提交。
- `bcbfa0b2`→`98292369` 的 gofmt 顺序提交同样触发了 main 红灯（41682480785）。建议推送前跑一次全量 `go build ./... && go vet ./...`（本轮 269 提交里两次红灯都是编译错误，成本极低可拦截）。
| T-22 | 取消仍以 Error 记录并包装成「模型调用失败」 | `react_invoke.go:66-73` 对任意 err 记 `logger.Error "Agent invoke error"`，`:685` 包成 `迭代 %d 模型调用失败: context canceled`。与「取消不是错误」的口径冲突，排障会把正常打断当故障 |
| T-23 | ACP 把用户取消报成正常结束 | 抑制 `chat.error` 后取消轮次只剩 terminal `is_complete`（`uapclaw.go:419`）→ `e2a/gateway_normalize.go:376-392` 空 result → `acp_channel.go:1235-1244` 以 `end_turn` 收尾。若产品需要 `stopReason=cancelled`，需单独补帧（Python 侧是否同样**待核**） |
| T-24 | 非流式 `history.get` 与 Python 不一致 | `handle_history.go:176-186` 返回 ok=true + 空 messages；Python `agent_ws_server.py:1830-1841` 返回 `ok=False` + 同一协议串。`5e742b78` 的修复只覆盖流式入口，新用例也只锁流式 |
| T-25 | `SerializeValue` 非递归，与注释不符 | `session_history.go:135-141` 注释称「SerializeValue 递归序列化」，实际 `stream_utils.go:699-705` 只转顶层 `time.Time`；Python `session_history.py:22-32` 是真递归（datetime/date/list）。当前未找到必然不可序列化的生产者（**待核**），但 team `event` 整块入盘放大了暴露面；`writeHistoryFile` 失败会让 `writeHistoryItem` 丢掉整条记录（`session_history.go:391-402,440-445`） |
| T-26 | `ExtractAnySliceFromPayload` 名不副实 | `stream_utils.go:678-706` 只接受 `[]any` / `[]map[string]any`，不支持 `[]map[string]string`（skilldev `ComputeTodos` 实际产出，`skilldev/schema.go:674`）与 `json.RawMessage`。当前 skilldev 走独立 chunk 路径（`skilldev/service.go:476-482`）故无触发，但「统一 payload 列表读取」的入口若被复用会再次吞成空列表 |


---

## 四、桩与回填核对（用户关注点 2）

对 `internal/` 全量 grep `⤵️/TODO/stub/未实现`，逐条核对依赖章节状态：

| 位置 | 标记 | 依赖章节状态 | 结论 |
|---|---|---|---|
| `team_manager_distributed.go:34-89`、`team_manager_lifecycle.go:63/225/450/668/691` | ⤵️ 10.6.20/10.6.21/10.6.22 | ☐ 未实现 | ✅ 桩合法 |
| `deep_adapter.go` A2X 字段（`a2xClient any` 等）、`tui_connect.go:258` | ⤵️ 10.6.22 / 11.15 面板 | ☐ / 🔄 | ✅ 桩合法 |
| `deep_adapter.go` `autoHarnessService any` | ⤵️ 10.6.11 | ☐ | ✅ 桩合法 |
| `web_handlers.go:339-344` updater.* | ⤵️ 11.17 | ☐ | ✅ 桩合法 |
| `web_handlers.go:297/304/313-314/321-322/347` config.validate_model、session.switch、path.*、locale.*、hooks.list | 无 ⤵️ 标记，仅有 `stubHandler` | 依赖**已就绪**（11.14 config round-trip ✅、hooks loader ✅、browser.chrome_path 读侧 ✅） | ❌ **应回填未回填** → 见 M-04 |

补充：`internal/swarm/server/utils/stream_utils.go:721-723` 的 `FindInteractionPayloads` 保留为 workflow 层移植后的对齐点，已注明「不要当作死代码删除」，核对 Python 确有该生产者（`workflow_task_executor.py:192-235`），属**合规保留**。

---

## 五、对齐性复核（抽查结论，非缺陷）

以下为逐项比对后确认**与 Python 一致**的部分，供后续复用，避免重复审查：

- **rail 注册链路（`c16f12af`/`696090b4`/`7d30dfd5`/`12139bd7`/`12139bd7`）**：冷启动不构建 evolution/create rail、只在注册成功时写字段（「字段非 nil ⇒ 已注册」不变量）与 Python `interface_deep.py:2158,2809-2845` 一致；`getCurrentAgentRails` 拆分为「就地更新演进 rail + 只重建 skillRail + 复用 5 个有状态 rail」与 `:2304-2352` 逐条对应；Python 在 `_get_current_agent_rails` 内做的 `filesystemRail` 复位与 `_update_permission_rail` 在 Go 侧落在 `ReloadAgentConfig:793-808`，等价。
- **`getSkillCreateEnabled`**（`deep_adapter_rails.go:472-484`）与 `interface_deep.py:268-280` 顺序（env `SKILL_CREATE` → `react.evolution.skill_create` → false）一致。
- **单 Agent SkillCreateRail**（`skill_create_rail.go`）：priority 85、阈值 10/5、`after_task_iteration`、`before_invoke` 复位、CN/EN 文案（含原文的 `，，` 双逗号）与 `openjiuwen/harness/rails/skills/skill_create_rail.py` 逐字一致；`EvolutionRail.ext` 多态分派正确（未踩 Go 嵌入不派发的坑）。
- **默认模型选择**（`ea3c2ca9`）：按 `models.defaults` 顺序取首个 `is_default`，与 `interface_deep.py:1576-1590` 的 dict 插入序等价。
- **`factory` 不再重复添加 TaskPlanningRail**（`cf28d4e3`）：修掉了「adapter 注销 A、factory 的 B 无人注销」的双实例缺陷，与 Python `alreadyProvidedByType` 语义现在一致。
- **`memory.compute`**（`626c8d3a`/`c0a236cd`）：`rss_mb/total_mb/available_mb` 键名与单位（字节/1048576）对齐 `app_web_handlers.py:1414-1427`，并补 `used_percent` 为超集（Python 算了但没放进 payload）。
- **`memory.compute` 之外的内存口径**：`gopsutil.VirtualMemory().Available/UsedPercent` 与 psutil 对 cached/buffers 的处理差异未在本项目内被消费（前端只展示），暂不构成偏差。
- **`58e81205` 的三处「消除重复实现」**：`BaseMessageToMap`（`foundation/llm/schema/message.go:269-304`）与两份旧实现语义等价；`extractToolContentFromInputs`（`team_skill_evolution_rail.go:1263-1304`）与旧 skill 版逐行等价；`buildAudioInputParams`（`prompts/tools/audio.go`）保持参数名/必填顺序/语言回落一致。
- **`stream_utils.go:305-315` 的 `todo.updated`**：`ExtractAnySliceFromPayload` 对缺失键返回空列表，与 `interface_deep.py:5134` `payload.get("todos", [])` 一致；`[]any` 与 `[]map[string]any` 双形态覆盖了 Go 侧两种来源。
- **`chat.error` 恢复英文协议串**（`5e742b78`）：符合「协议字符串不得本地化」的既有约定，抽查本窗口新增代码未再出现本地化协议串。
- **TUI 记忆 RPC 接线（`c3e11df6`/`b477e92f`/`1b40d1d0`）**：5 个 handler 名与 `tui_connect.py:1903-1907` 一致；本地 handler 自持 res 帧、恒返回 nil（避开 `gateway_server.go:750` 的二次包装，与 `tui_connect.py:1843-1901` 同构）；recover 在参数解析与 handler 调用之前注册，覆盖全程；所有写帧经 `internal/common/wswrite` 连接级互斥（多 handler 并发写同一 conn 安全）；注册数断言同时枚举方法名 + 精确计数（`tui_connect_test.go:117-130`），改 10 不掩盖漏注册；`TestMemoryStatusFullChain` 走真实 WS → `/tui` → 本地 handler 并断言磁盘计数，非假绿。
- **`memory.compute` 量纲与键名**：`rss_mb/total_mb/available_mb` 均为 `bytes/(1024*1024)`，与 `app_web_handlers.py:1416-1422` 一致（`used_percent` 口径问题见 M-15）。

---

## 六、建议处置顺序

1. **S-04 / S-05 / S-06（sys_operation 三连）**：同一函数、同一次重排，一起修；补「>64KB 输出」「单行 >64KB 挂起」「`grep` 无匹配 → success+exit 1」三条用例，否则这三类问题会再次"修好"又"没修好"。
2. **S-01 / S-02（记忆索引地基）**：不修则本轮的「中文分词 + 检索修复」在存量库和非 1024 维模型上全部失效，建议作为发布阻塞项。
3. **S-03（coding/team 记忆 ctx）**：一行修复 + 一个用例，但属 P0 同类，建议同步修。
4. **S-07（Pulsar 生命周期）**：9.84 接线前必须收敛，否则接线即引入 panic。
5. **M-01 / M-02 / M-03（并发安全三处）**：热重载与多会话是常态场景，建议本轮内用 `-race` 用例钉住。
6. **M-04（web 桩回填）**：用户可见的"设置不生效"，且依赖已齐备，成本低。
7. 其余按 M→T 顺序常规消化。

---

## 七、待核项（需真实环境或上游确认）

1. Python `pulsar.Client.close()` 是否兜底关闭未显式 close 的 consumer（本机未安装 `pulsar` 模块）。
2. 未 ack 消息在真实 broker 上的重投时机（需真实 Pulsar）。
3. contentless FTS 表按非 rowid 列 `DELETE` 是否真的无效（需 sqlite CLI 或运行时验证）。
4. `test(integration)` 中 Windows 分支的 GBK 断言（`shell_tools_test.go:132`）在 Windows 上是否可过——CI 只跑 linux，无法覆盖。
5. `InvokeStageAgent` 复用同一 agent 连续 `Invoke` 是否会累积会话历史（skilldev 多文件生成场景），需 runtime 验证。
6. TUI 客户端 `getTrustedDirs()` 是否可能返回含空串/None 的列表（决定 T-16 的真实触发面）。
7. gopsutil v3 在非 Linux/旧内核上 `Available` 是否恒取 `MemAvailable`（决定 `available_mb` 与 Python 的对齐程度）。
