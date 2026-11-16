# 8.3c Pregel 调度引擎 设计文档

- 日期：2026-11-15
- 章节：领域八 / 8.3c（实施序 8.3a → 8.9a → 8.3b → **8.3c** → 8.9a → 8.8 → 8.9b）
- Python 参考：`openjiuwen/core/graph/pregel/engine.py`（285 行）
- Go 包：`internal/agentcore/graph/pregel`（`engine.go` 新增）

## 1. 定位与作用

8.3c 是图引擎的调度心脏，在 agent 会话流程中的位置：

```
Workflow 层（8.1/8.2 PregelGraph/CompiledGraph）
  └─ PregelBuilder.Build() → Pregel          ← 8.3a 已有 builder，Build() 本节回填
       └─ Pregel.Run(config)                 ← ★ 本节
            └─ PregelLoop（init / run_step 循环）
                 ├─ ChannelManager（8.4 ✅）
                 ├─ TaskExecutorPool（8.3b ✅）
                 ├─ Router（8.5 ✅）
                 └─ GraphStore（8.9 ✅）
```

职责：把通道、任务池、路由、检查点存储串成超级步循环
`init（resume 恢复或触发 START）→ ready→submit→wait_all→buffer→flush→after_step → step+1`，
直到无活跃节点或递归上限；顶层把 GraphInterrupt 转 `{"__interrupt__": value}` 返回。

## 2. 已拍板决策（用户逐项确认）

| # | 决策 | 结论 |
|---|------|------|
| D1 | ChannelManager 并发锁 | **方案 A**：各公开方法加 `sync.Mutex` + `-race` 并发用例；登记偏差（Python asyncio 单线程无需锁，Go 防御性加锁） |
| D2 | 错误保存时机 | **方案 ①**：`runStep` 同步捕获 error → 同步 `saveStateOnError`（内部同步调 Store.Save）→ 再分类上抛，步骤归属与 Python 一比一 |
| D3 | RecursionError 表达 | **方案 ①**：新增 `RecursionError` 类型（携带 Limit/Step，`errors.As` 分类，文案复刻 `"Recursion limit of %d reached at step %d"`） |
| D4 | Build 回填签名 | **方案 ①**：`Build(store *store.Store, afterStep AfterStepFunc) *Pregel`；store 可 nil（loop 内三重判空对齐）；回调同步形态带 ctx；`iscoroutinefunction` 双分支登记偏差（见 §6） |
| D5 | Run 返回值 | **方案 1+a**：`map[string]any`，正常 `{}`，顶层中断 `{TaskStatusInterrupt: *GraphInterrupt}`，非顶层 re-raise |
| D6 | PregelLoop 字段可见性 | **方案 ①**：Python 公开属性全导出（Graph/Manager/Step/MaxStep/Config/Saver/ActiveNodes/Executor/NodeVersion），`_retry_pending_nodes` 非导出 |

## 3. 范围与回填清单（承诺不丢失）

| 项 | 处置 |
|---|---|
| `builder.go` 的 `Build()` | 本节回填兑现（8.3a 方案 A 承诺）；IMPLEMENTATION_PLAN.md 8.3a 行 ⤴️ 注记追加完成行，原注记逐字保留 |
| serde json 上游核对 | 已核对 `serde.py:46` `create_serializer("json")` 仍 raise `"json is not yet supported"` → 8.9a D-1 维持，无需回填；本 spec 记录核对结论 |
| 8.9a D-8 error→string | `PendingNode` 已有 D-8' 适配，`_save_state_on_error` 直接复用，不新做 |
| ChannelManager 前置警告 | 计划表 8.3c 行要求补锁 → 本节以 D1 方案 A 兑现 |
| 8.8 GraphState | engine.py 不依赖（用的是 store 的 GraphState，已实现），后移决策不受影响 |

## 4. 新增/修改文件清单

| 文件 | 动作 | 内容 |
|---|---|---|
| `internal/agentcore/graph/pregel/engine.go` | 新增 | `PregelLoop` + `Pregel`（一比一映射 engine.py） |
| `internal/agentcore/graph/pregel/engine_test.go` | 新增 | 单元测试（见 §7） |
| `internal/agentcore/graph/pregel/builder.go` | 修改 | 回填 `Build()` |
| `internal/agentcore/graph/pregel/channels.go` | 修改 | 加 `sync.Mutex`（D1） |
| `internal/agentcore/graph/pregel/doc.go` | 修改 | 文件树同步 |
| `internal/common/exception/codes_graph.go` | 修改 | 补 `StatusPregelGraphSuperStepExecutionError`（112104，模板 `graph super-step failed, step={step}, error='{reason}'`） |
| `IMPLEMENTATION_PLAN.md` | 修改 | 8.3c 行打 ✅ + 8.3a 行 ⤴️ 追加完成注记 |

## 5. 步骤复刻要点（规则 9/12 逐条对应）

### 5.1 PregelLoop

- `__init__` → `NewPregelLoop(graph *Pregel, config *PregelConfig)`：Manager=NewChannelManager(graph.Channels)、Step/MaxStep 零值、Saver=graph.Store、NodeVersion 用 `defaultdict(int)` 语义（读零值自动补 0）
- `init()`：executor 构造（传 config）→ max_step=RecursionLimit → sessionID+ns+saver 三者齐才 `Saver.Get` → `isResume(state)`（复刻 store `is_resumable`，state 非 nil 且 IsResumable）：
  - resume：Manager.Restore(channel_values)；node_version=state.node_version；step=state.step；max_step=step+limit；pending_buffer 逐条 BufferMessage；pending_node 存 `_retryPendingNodes`
  - 否则：BufferMessage(NewTriggerMessage(initial, initial)) + Flush
- `run_step()`：两层结构——外层捕获 error：非 GraphInterrupt 打 GRAPH_SUPER_STEP_ERROR（含 ns/session_id/metadata{ns, step, active_nodes}）→ 同步 `saveStateOnError(e)`（D2）→ 分类：`errors.As` 判 `*GraphInterrupt`/`*RecursionError`/`*exception.BaseError` 原样上抛；其余 `BuildError(112104, step, reason)` 上抛
- `_run_step()`（私有 `runStep`，步骤顺序逐条对应 engine.py:98-171）：
  1. debug 日志 GRAPH_SUPER_STEP_START
  2. retry 队列优先：`_retryPendingNodes` 非空 → active_nodes=其 keys、清空；否则 getReadyNodes → 过滤（在 graph.nodes 且 ≠END）→ active_nodes + node_version[n]++
  3. active 空且 manager.IsEmpty → return false；空且非空 buffer → 仅 Flush、step++、return true
  4. step > max_step → panic/返回 `&RecursionError{...}`（engine.py:139）
  5. 逐节点 Consume + 收集 tasks
  6. 逐节点 Executor.Submit(node, version)
  7. `WaitAll` 错误且为 context.Canceled（asyncio.CancelledError 对应）→ `CancelAll()` 后上抛
  8. succeed_messages 逐条 BufferMessage → Flush → Executor.Clear
  9. afterStep 回调（同步，传 loop 自身）
  10. step++ → return true
- `_save_state_on_error()`：debug 日志 → sessionID/ns/saver 缺一即 return → pending_buffer=manager.Buffer + executor.succeedMessages、pending_node=executor.failed → `store.CreateState(ns, step, snapshot, pending_buffer, pending_node, node_version)` → `Saver.Save`

### 5.2 Pregel

- `NewPregel(nodes, channels, initial=Start, store, afterStep)`
- `Run(ctx, config)`：`CreateInnerConfig(config or DefaultPregelConfig())` → is_top_level=ParentNs 空，顶层则 ParentNs=ns → info 日志 GRAPH_START{is_top_level} → Loop.Init → Trigger(LoopStarted, graph_id) → `for runStep()` → Trigger(LoopFinished, graph_id, total_steps) → info 日志 GRAPH_END → 返回 `{}`
- 中断路径：catch `*GraphInterrupt` → Trigger(LoopFinished) → info 日志 GRAPH_END（interrupted）→ 顶层返回 `{TaskStatusInterrupt: e}`（D5，value 为 `*GraphInterrupt`），非顶层 re-raise

### 5.3 日志

- 组件 `logger.ComponentAgentCore`；Msg 英文化（feedback-logs-english），event_type 结构化字段（GRAPH_SUPER_STEP_START/ERROR、GRAPH_START/END）对齐；f-string 变量 → `.Str/.Int` 字段（graph_id/session_id/ns/step/active_nodes/total_steps/is_top_level）

## 6. 已知偏差

| 编号 | 偏差 | 理由 | 回收条件 |
|---|---|---|---|
| B1 | afterStep 单形态（同步函数），不复刻 `iscoroutinefunction` 双分支 | Go 无协程检测惯用法，接口/反射嗅探属自行设计（规则 9.4） | 上游改变回调契约时 |
| B2 | ChannelManager 加 `sync.Mutex`，Python 无锁 | Go 侧防御性并发安全（用户拍板 D1） | 无 |
| B3 | `asyncio.CancelledError` → `context.Canceled`（2026-11-15 审查收窄）：外层取消以 `ctx.Err()` 判定并触发 `cancel_all`；`context.Canceled` 在 RunStep 透传分支原样上抛（Python `CancelledError` 继承 `BaseException`，`except Exception` 不捕获，两者均不打日志/不落盘/不包装，差异仅在 cancel_all 是否触发） | 语言层等价物，8.3b D5 同源先例；收窄原因：WaitAll 返回的 Canceled 可能是节点自身错误，不得误判为外层取消 | 无 |
| B4 | `defaultdict(int)` → Go map 直读（零值即 0） | Go map 零值语义天然等价 | 无 |
| B5 | **类型迁移**（2026-11-15 用户拍板方案 Ⅰ）：`GraphState`/`Store` 接口/`CreateState`（含 `IsResumable`/`Clone` 方法随类型走）从 store 包迁入 pregel 包（新文件 `state.go`），store 包保留 `GraphStore`/`InMemory`/serde 改引用 `pregel.GraphState`；消费方 `session/checkpointer/{persistence,inmemory,serializer}.go`、`session/interfaces/interfaces.go` 的 `store.Store`→`pregel.Store` | Go 编译期环：engine.py:20 直接消费 store 四符号，而 store→pregel 已有依赖（GraphState 字段用 Message/ChannelSnapshot/PendingNode）；Python 同款环靠子模块延迟加载容忍，Go 以类型迁移为忠实等价物，与 8.3b D6（PendingNode 迁移）同源 | 无 |
| B6 | `Run` Go 签名 `(map[string]any, error)`：Python 单返回 dict + raise，Go 以 error 表达 re-raise（正常 `({}, nil)`、顶层中断 `({__interrupt__}, nil)`、其余 `(nil, err)`） | Go 惯用法承载 Python raise 语义 | 无 |

## 7. 测试与验收

- 覆盖率 ≥85%（对齐先例目标 95%+）
- 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` + `-race`（channels 锁并发用例：多 goroutine Buffer/Flush/Snapshot 无竞态）
- 场景：START→A→END happy path / barrier 汇聚 / 中断→resume（fake store）/ 递归上限 / 错误保存后 resume 重试 pending_node / 非顶层中断 re-raise / Build(nil store) 容忍
- 回填验证：`go build ./... && go vet ./...` 通过；grep `⤴️ 8.3c` 仅剩计划表历史注记

## 8. 换章节评估结论

前置件（8.3/8.3a/8.3b/8.3d/8.4/8.5/8.6/8.7/8.9）全部 ✅，唯一缺口 ChannelManager 加锁按 D1 同批处理；`runner/callback`（LoopStarted/LoopFinished/Trigger 已存在）经核实无反向依赖 pregel，不构成前置件。**无换章节需要。**
