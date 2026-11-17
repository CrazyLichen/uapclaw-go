# 8.12 StreamActor 设计（领域八）

- 日期：2026-11-19
- 状态：待用户审阅
- Python 基线：`openjiuwen/core/graph/stream_actor/`（base.py 327 行 + manager.py 179 行）
- Go 落点：`internal/agentcore/graph/stream_actor/` + session 回填 + workflow 前置件

## 1. 背景与流程位置

8.12 是 workflow 流式输出的**数据面中枢**，与 Pregel 控制面（loop/channel/routers）并行：

- **控制面**：loop 收集 ready 节点 → TaskExecutorPool 执行 → 收割消息 → loop 投递 channel。只传控制消息，节点返回值被丢弃（8.3b 已对齐 task.py:152）。
- **数据面**（本节）：producer 节点执行体内 `produce(chunk)` → ActorManager 按 `stream_edges` 路由 → StreamActor（每消费者一个）→ StreamProcessor（每 ability 一个）按 schema 叶子路径分发到路径 chan → 消费者组件从 chan 拉数。

两条面仅在 Vertex 执行体内部交汇（8.11）：`call()` 串行跑 INVOKE/STREAM 策略后等 `_stream_done`；`stream_call()`（由 StreamActor 首帧拉起）并发跑 COLLECT/TRANSFORM 策略，各策略 `_pre_stream(ability)` → `consume()` 换 gen 树 → `on_collect/on_transform` 拉数，产出经 `_post_stream` → `produce()/end_message()` 路由。

**ability 语义**：STREAM=纯产出、COLLECT=纯收集、TRANSFORM=双向（收+转发）。帧广播给消费者的全部 processor，不看 ability 匹配；producer 的 ability 只影响源键（`producerId-ABILITY`）与 source_groups 匹配。

**输入通道分野（2026-11-19 用户确认）**：INVOKE/STREAM 策略的输入走 `_pre_invoke()`（`io_configs.inputs_schema` 经 session state 取静态值），不经过 ActorManager；仅 COLLECT/TRANSFORM 策略经 `_pre_stream(ability)` → `consume()` 走数据面 gen 树。普通 invoke 数据链 = 节点返回值 → `_post_invoke` → `session.state().set_outputs` → 下游 `_pre_invoke` 取值，全程控制面/state，与 8.12 无交集。

## 2. 范围与顺序决策

1. **实施顺序换回 8.12 → 8.11**（十次记录）：8.11 Vertex 依赖 StreamConsumer 属 8.12，先落真类型避免接口占位回填。同步更新 IMPLEMENTATION_PLAN.md 顺序注记（十二次记录）。
2. **窄闭环范围（方案 A）**：落 stream_actor 包 + 回填 session 的 ActorManager any 占位 + EndFrame 多源场景回填。Vertex/Workflow 调用侧属 8.11/8.14，测试用 fake StreamConsumer。
3. **WorkflowSpec 最小前置件（方案 A，沿用 8.10 ComponentAbility 先例）**：本节一比一落 `workflow/workflow_config.go`（Python workflow_config.py 82 行全文 + NodeSpec 最小字段）；IMPLEMENTATION_PLAN.md 8.15 行加 ⤴️「8.12 已前置落地骨架，实施时完整同步补全（含 NodeSpec 裁剪字段）」。
4. **session 直接 import 具体类型（方案 B）**：session/internal/workflow_session 的 any 占位收窄为 `*stream_actor.ActorManager`。依赖方向 session/internal → stream_actor 单向；stream_actor 依赖 session/utils、session/constants、common/utils、workflow/components、workflow/workflow_config，均为叶包，无包环。

## 3. 文件映射（一比一）

| Python | Go |
|---|---|
| `stream_actor/base.py` | `stream_actor/base.go`：StreamConsumer 接口（stream_call/shouldHandleMessage/isDone）、StreamGraph、StreamPayload、StreamActor、StreamProcessor、`isEndMessage`/`getProducerId` |
| `stream_actor/manager.py` | `stream_actor/manager.go`：StreamTransform、ActorManager、`buildReverseGraph` |
| `stream_actor/__init__.py`（空） | `stream_actor/doc.go` |
| `workflow/workflow_config.py` | `workflow/workflow_config.go`（前置件，见 §2.3） |

### 3.1 核心类型契约

- `StreamConsumer`（接口，实现属 8.11 Vertex）：`StreamCall(ctx, done func(error))`、`ShouldHandleMessage() bool`、`IsDone() bool`。asyncio.Event → Go 由 StreamActor 内部用 `chan struct{}`/`sync.WaitGroup` 承载，不进接口。
- `StreamActor.send(message, sourceAbility, firstFrame, producerID)`：should_handle 检查 → task 未启动且未出错时首帧拉起 `vertex.StreamCall` + 等 event → 广播 `payload` 给全部 processor.receive。
- `StreamProcessor.run(ability)`：常驻 goroutine，主 queue 取帧 → end 帧关流判定（source_groups：单源组=AND 等 end frame；组全到齐关全流）→ 非帧按 r_path 摘值投路径 chan → 全组到齐 break。
- `StreamProcessor.generator(schema, streamCallback)`：extract_leaf_nodes 扫 `$ref` 叶子 → 每叶子建 chan 注册进 processor_queues → rebuild_dict 还原 gen 树。拉数 `select { ctx.Done / time.After(timeout) / pathChan }`，EndFrame 或 timeout 退出。
- `ActorManager`：构造期反向图 + consumer abilities ∩ {COLLECT,TRANSFORM} 建 actor + producer abilities ∩ {STREAM,TRANSFORM} 收集源键 + source_groups 默认单源排序拆分；`produce/endMessage/consume/activeProduceAbility/markProducerDone/shouldSanitizeStreamSource/shutdown`；`subWorkflowStream()`（子图 AsyncStreamQueue maxsize=10240）。

## 4. 并发模型（用户拍板：run 启动点挪至 consume 之后）

- goroutine+chan 直译 asyncio；`send` 不阻塞（无界内存队列承载主 queue，见偏差 D2）。
- **processor.run 的启动点从 send() 首帧挪到 consume() 注册完成之后**（偏差 D1，用户 2026-11-19 拍板）。收益：注册→启动串行化，processor_queues 无并发写，省 mutex；仅 run 与 shutdown 之间靠 ctx 取消 + WaitGroup 收割。
- generator 拉数/入队所有阻塞点均 select ctx.Done 与 timeout，保证外层可取消（用户明确要求）。
- **丢帧语义不复刻原竞态**：Python 中 run 先于注册消费帧会静默丢弃（base.py:187-193 注册表为空不投递）；Go 侧因 run 启动于注册后，**注册前的帧滞留主 queue、启动 run 后先补投再跟进新帧**——行为严格优于 Python 且无竞态，作为偏差 D1 的一部分登记。
- timeout：路径 chan 未声明源才限时（`_path_has_declared_source`），值来自 session env `STREAM_INPUT_GEN_TIMEOUT_KEY`（Python 与 Go 默认均 -1=不超时，config/base.py:157 vs constants.go:80，已核对无差异）。

## 5. 回填面

1. **session/internal/workflow_session.go**：两处 `// ⤵️ 后续回填：any → ActorManager` 收窄为 `*stream_actor.ActorManager`；`setActorManager` 幂等语义对齐 Python（非 None 拒绝，internal/workflow.py:46-49）；Close 链路调 `shutdown()`。session/interfaces 的 `ActorManager() any` 同步收窄。
2. **EndFrame 多源场景**（session/utils/doc.go ⤵️ 兑现）：Python `utils.py:391` `@dataclass EndFrame{source: str}`，Go 落 `session/utils`（叶包，无环）：`type EndFrame struct { Source string }`；`Frame = any`（值 or *EndFrame）以 `any` 承载。
3. **工具件**：`common/utils.FormatPath` 导出（原 formatPath 非导出，StreamProcessor.generator 需用）；`getBySchema` 按 manager 需要的 `map[string]any` 版本从 session/state 导出适配（Python `session/utils.py:85`）；`Transformer` Go 定义为 `func(map[string]any) map[string]any`（偏差 D4：Python 注解名不符实）。

## 6. 已知偏差与登记

| 编号 | 内容 | 性质 | 回收条件 |
|---|---|---|---|
| D1 | processor.run 启动点从 send 首帧挪至 consume 注册后；未 consume 的 processor 不启动；注册前帧不丢（滞留补投） | 用户拍板的结构偏离（规则 9/12 例外） | 上游改此结构时跟随；若上游修丢帧语义则行为自然合流 |
| D2 | asyncio 无界队列 → Go 带锁内存队列（slice+Cond 或等价） | 语言能力缺失（chan 无法无界） | 无 |
| D3 | asyncio 单线程免锁 → Go 无锁依赖 D1 串行化，run/shutdown 并发由 ctx 取消兜底 | 伴生 D1 | 同 D1 |
| D4 | `Transformer` 注解：Python `Callable[[ReadableStateLike], Any]` 与运行时用法（消息变换）不符，Go 按 `func(map[string]any) map[string]any` | 修正上游注解错误，行为对齐运行时 | 无 |
| D5 | END 哨兵字符串 `"END_<producer_id>"` 前缀判定一比一复刻（真实内容恰以 END_ 开头会误判，Python 同病） | 结构忠实保留 | 上游改类型化协议时跟随 |
| D6 | WorkflowSpec/NodeSpec 前置件裁剪：仅含 ActorManager 消费字段 + NodeSpec 最小集 | 前置件范围控制 | 8.15 实施时完整同步（IMPLEMENTATION_PLAN ⤴️ 已标） |
| D7 | SubWorkflowStream 非子图错误码复刻 Python 字面量 112030（GRAPH_STREAM_ACTOR_EXECUTION_ERROR 与 GRAPH_STATE_COMMIT_ERROR 撞号——上游 codes.py:394/412 复制粘贴笔误一并复刻），经 exception.BuildError 结构化抛出 | 结构忠实保留上游撞号 | 上游修复撞号时跟随顺延 |
| D8 | Produce/Consume 对「actor 不存在」加防御分支（Python 缺键 KeyError，构造期保证可达性不可触发；Go: Produce continue / Consume 返 error） | Go 侧防御性等价 | 上游改契约时跟随 |
| D9 | MarkProducerDone/Consume 的 workflowSession==nil 守卫保留但必须记 Warn 日志（防装配遗漏静默不写状态，2026-11-19 审查拍板「4 要加日志」） | 防御性日志兜底 | 无 |
| D10 | 子图队列按 Python emitter.py 独立类落地 `session/stream/async_queue.go`（计划原写复用 StreamQueue，实施更结构忠实，审查追认） | 计划偏差追认 | 无 |

## 7. 测试方案

- fake StreamConsumer（三方法可控）+ fake session state（finished_stream_nodes/executed_nodes 读写）驱动 StreamActor/StreamProcessor 全分支。
- **讨论点全覆盖要求（2026-11-19 用户要求）**：本次讨论澄清的每个语义点都必须有对应用例锁定，单元与集成（tests/integration，规则 7 真实跨 ≥3 子系统）双层覆盖，至少包括：
  1. 输入通道分野：INVOKE/STREAM 输入走 state 不进 ActorManager；COLLECT/TRANSFORM 走 consume 换 gen 树（§1 末段）
  2. 帧广播不匹配 ability：producer STREAM 帧 consumer 侧全部 processor 各自收到（§1 ability 语义）
  3. 首帧拉起时序与 event 置位前后窗口、is_done 计数判定
  4. run 启动点在 consume 之后：注册前帧滞留主 queue、启动后补投不丢（D1 新行为）
  5. source_groups：单源组=AND 等 end frame、多源组到齐关全流、组内活跃替代源 sanitize（should_sanitize_stream_source 三分支）
  6. END 哨兵 `END_<producer_id>` 前缀判定与传播（D5，含误判边界用例留档）
  7. finished_stream_nodes 恢复场景：consume 时补发 end_message
  8. timeout：仅未声明源路径限时、-1=不超时、timeout 退出与 ctx 取消可中断所有阻塞点
  9. shutdown 级联取消（-race）：task/processor/running_tasks 三层收割与日志
  10. generator gen 树重建形状（rebuild_dict）+ 每路径多 chan 广播
  11. WorkflowSpec 前置件字段往返与默认值（D6）
- 重点用例：首帧拉起时序（event 置位前后）、source_groups 单源组 vs 多源组关流、END 哨兵传播、补发 end_message（finished_stream_nodes 恢复场景）、timeout 未声明源、shutdown 级联取消（-race）、should_sanitize_stream_source 三分支、generator gen 树重建（rebuild_dict 形状）。
- 前置件 workflow_config.go 覆盖字段往返与默认值。
- 门槛：包覆盖率 ≥85%，`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全量零 FAIL（CI 同款标签）。

## 8. 遗留与后续

- Vertex（8.11）实现 StreamConsumer 三方法与 `_pre_stream/_post_stream` 编排；Workflow（8.14）构造 ActorManager 并 set 进 session。
- `AsyncStreamQueue` 子图队列复用已有 `session/stream.StreamQueue`（maxsize=10240），行为核对 Python `AsyncStreamQueue` 后在 plan 阶段定适配细节。
