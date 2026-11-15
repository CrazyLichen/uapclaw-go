# 8.3b TaskExecutorPool / NodeTask 设计

- 日期：2026-11-19
- 章节：领域八 8.3b（IMPLEMENTATION_PLAN.md）
- Python 参考：`openjiuwen/core/graph/pregel/task.py`（174 行）
- Go 位置：`internal/agentcore/graph/pregel/task.go` + `task_test.go`

## 1. 流程位置与作用

Pregel 调度链路中「引擎 → 节点执行」的中间层。一次 `Pregel.Run`（8.3c，未实施）的每个 step 内：

```
引擎 RunStep (8.3c)
  ├─ ChannelManager.GetReadyNodes() → 本轮就绪节点 {B, C}
  ├─ pool.submit(B); pool.submit(C)        ← 一个任务 = 一个节点本轮的一次执行
  ├─ pool.wait_all()                       ← 任务内部: fn → dispatch → 返回消息
  ├─ engine 取 pool 成功消息 → ChannelManager.Buffer() → Flush()
  └─ flush 后新就绪节点进入下一轮
```

Pregel 是 BSP 超步模型：同一轮就绪的兄弟节点**并发**执行，轮与轮之间由 flush 形成屏障。边/路由不是被调度执行的对象——路由发生在每个任务内部（`dispatch` 产出消息），边的效果由消息写通道、改变下一轮就绪集合体现。

职责：

1. **`TaskExecutorPool`**：单 step 任务池——`submit` / `wait_all`（FIRST_EXCEPTION 等待 + 失败记账 + 优先级上抛）/ `cancel_all` / `clear`
2. **`NodeTask`**（Go `nodeTask`）：单节点执行——ns 拼接与 config 注入、调用节点函数、路由 dispatch 收集消息、EDGE_TRAVERSED 回调、GraphInterrupt 降级为返回值

## 2. 前置件核对（全齐，无需换章节）

| 依赖 | 状态 |
|------|------|
| `pregel.Message` / `GraphInterrupt`（8.6/8.7） | ✅ |
| `Router.Dispatch`（8.5） | ✅ |
| `PregelNode`（8.3，D1 固定签名） | ✅ |
| `CreateInnerConfig` / NS / PARENT_NS 常量（8.3d） | ✅ |
| `store.PendingNode`（8.9a，Exception []error + D-8' string 适配已落） | ✅ |
| `runner/callback.Trigger` + `WorkflowEvents.EdgeTraversed` | ✅ |
| `ChannelManager`（8.4） | 本节不直接依赖 |

- 章节位置维持现序 8.3a → 8.9a → **8.3b** → 8.3c，不换。
- 回填欠账：零。8.9a D-8' 已落；无新增 ⤵️/⤴️。

## 3. 类型设计（结构忠实，同文件 task.go）

Python 两个类 → Go 两个类型。

### TaskExecutorPool

| Python | Go | 说明 |
|--------|-----|------|
| `config` | `config *PregelConfig` | |
| `succeed_messages: List[Message]` | `succeedMessages []Message` | |
| `failed: Dict[str, PendingNode]` | `failed map[string]store.PendingNode` | |
| `running_tasks: Dict[asyncio.Task, PregelNode]` | `runningTasks map[*nodeTask]*nodeTask` 键值组合见下 | 偏差 D2 |

`running_tasks` 以 task 对象为键——Go 无此机制。方案 B 下每任务持单容量 done channel，用 `map[*nodeTask]*PregelNode` 保留「任务→节点」反查表；Python `wait_all`/`cancel_all` 中 `pop(t)` 语义用 `delete` 复刻。

### nodeTask

| Python NodeTask | Go nodeTask |
|------|------|
| `node/config/messages/version` | 同名小写字段 + `done chan nodeResult` + `ctx`（可取消派生） |
| `run() -> Union[List[Message], GraphInterrupt]` | `run() ([]Message, error)`；`*GraphInterrupt` 以 error 形态返回 ≡ Python「中断降级为返回值」 |

### 方法清单对照

| Python | Go | 备注 |
|--------|-----|------|
| `submit(node, version)` | `Submit(node, version)` | 起 goroutine，注册 runningTasks |
| `wait_all()` | `WaitAll(ctx)` | 返回 error 承载 Python `raise` |
| `_commit_failure(node, exc)` | `commitFailure(node, err)` | 非导出，name 首次才记 |
| `clear()` | `Clear()` | |
| `cancel_all()` | `CancelAll(ctx)` | 不上抛、清 runningTasks |
| `NodeTask.run()` | `(*nodeTask).run(ctx)` | 步骤见 §5 |

## 4. 并发模型（方案 B，已拍板）

1. `Submit` 每 node 起一个 goroutine 跑 `run()`，结果写自己的 done channel。
2. `WaitAll`：
   - **FIRST_EXCEPTION 语义**：循环 select 各 done channel；收到任一 error 结果（含中断）即转入取消路径（cancel 派生 ctx），否则收满全部结果自然结束。
   - **取消收尾**：被取消任务以 `context.Canceled` 记 `PendingNode(status=TaskStatusError, exception=[ctx.Err()])`（复刻 Python `asyncio.CancelledError` 记账）；取消产生的 warning 日志带 `event_type=GRAPH_VERTEX_CALL_ERROR` + `error` 字段，逐条对齐 task.py:74-84。
   - **优先级上抛**：`firstErr != nil` → 返回 `firstErr`；否则中断 → 返回中断（Python task.py:86-90 的「普通异常 > 中断」）。
3. `CancelAll`：对未完成任务 cancel → gather 收尾（错误仅 warning 日志）→ 逐个记 `CancelledError` 等价 → 清空 runningTasks。不上抛。

## 5. nodeTask.run 步骤（按 Python task.py:125-174 逐行）

1. `CreateInnerConfig(config)` → 读 `ParentNs` → 非空则拼 `parent:node_name:version` **双写** NS/PARENT_NS（task.py:139-147）。
   - 无条件填进 `NodeInput.Config`：Python「形参含 config 才注入」的反射已被 8.3 D1 固定签名消解（见偏差 D1）。
   - `State` 恒 nil（8.3 拍板，task.py:148-149）。
2. 直接调 `node.Func(ctx, input)`（Python sync/coroutine 双路调用由 Go 静态签名消解，偏差 D4）。
3. 依次 `r.Dispatch(sourceNode=node.name)` 收集 messages（task.py:157-160）。
4. messages 非空时逐条 `Trigger(EdgeTraversed, {source_node: msg.sender, target_node: msg.target, graph_id: config.Get(NS)})`（task.py:162-168）。
5. `fn` 返回 `*GraphInterrupt` → 作为 run 的 error 返回（≡ `except GraphInterrupt: return e`，task.py:172-174）。

## 6. 中断判定方式

Python 靠 `isinstance(exc, GraphInterrupt)`。Go 用 `errors.As` 判 `*pregel.GraphInterrupt`，容忍 fn 内部包装，语义等价。

与 8.7 偏差 D3（禁止 `r.(error)` 判中断）的边界：D3 约束的是 **tracer 场景**（error 接口兜底分支误吞中断导致 span 状态错标）；本节 NodeTask 是**主动捕获点**（对齐 Python 显式 except），不冲突。

## 7. 已知偏差

| 编号 | 偏差 | 理由 | 回收条件 |
|------|------|------|----------|
| D1 | 无条件注入 config（Python 按形参反射注入） | 承 8.3 偏差 D1 固定签名拍板；多塞不读零成本 | 上游改签名约定时同步 |
| D2 | `map[*nodeTask]*PregelNode` 替代 `Dict[asyncio.Task, PregelNode]`；goroutine+done channel 替代 asyncio.Task | Go 无 asyncio | 无（平台差异终态） |
| D3 | `context.Canceled` 复刻 `asyncio.CancelledError` | Go 取消机制等价物 | 无（平台差异终态） |
| D4 | Python sync/coroutine 双路调用消解为单路直调 | Go 静态签名 | 无（平台差异终态） |
| D5 | Python `wait_all` 无参 → Go `WaitAll(ctx)`；取消依赖 ctx 派生 | 方案 B 实现所需 | 无 |

## 8. 测试计划

`task_test.go`，包覆盖率 ≥85%，全量回归带 CI 同款标签（`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`）。

用例清单：

1. 单节点成功：succeedMessages 收齐、dispatch 消息正确
2. 单节点异常：failed 记 TaskStatusError、WaitAll 上抛该 error
3. 单节点中断：failed 记 TaskStatusInterrupt、WaitAll 返回 *GraphInterrupt
4. 兄弟并发一异常一成功（-race）：异常任务触发取消、被取消任务记 context.Canceled、优先级上抛异常
5. 同轮异常+中断：异常优先上抛
6. CancelAll：未完成任务取消记账、runningTasks 清空、不上抛
7. Clear：三字段清空
8. ns 拼接：有/无 ParentNs 两分支、双写断言
9. EDGE_TRAVERSED：Trigger 调用字段断言（source/target/graph_id）
10. FIRST_EXCEPTION 语义：无异常时收满全部结果才返回
11. commitFailure 首记语义：同名节点已有 failed 记录不覆盖

## 9. 完成标准

- `go build ./...` / `go vet ./...` 通过
- 单包覆盖率 ≥85%，全量回归（CI 同款标签）零 FAIL
- IMPLEMENTATION_PLAN.md 8.3b 状态 ✅ 并写入 Go 事实
- spec 本文件提交
