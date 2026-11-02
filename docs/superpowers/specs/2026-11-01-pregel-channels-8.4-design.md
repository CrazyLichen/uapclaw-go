# 8.4 ChannelManager / TriggerChannel / BarrierChannel 设计文档

> 对应实现计划：领域八 8.4（IMPLEMENTATION_PLAN.md L504）
> Python 源码：`openjiuwen/core/graph/pregel/channels.py` 全文 189 行（`ChannelManager` / `TriggerChannel` / `BarrierChannel`）+ `pregel/base.py:53-84`（`Channel` ABC）
> 前置依赖：8.6（`Message` / `MessageKind` / `Kind()`，已 ✅）、8.3d（`pregel.End` 常量，已 ✅）、8.5（`Router` 产出的消息形状，已 ✅）
> 前置依赖（原计划）：8.3 的 `Channel` ABC —— **本方案改为由本节自己提供**（方案 B，见 §5）
> 后续消费方：8.3a（`PregelBuilder.add_node` 造 `TriggerChannel`、`add_edge` 的 N→1 造 `BarrierChannel` 并把 `Key()` 转手给 `BarrierRouter`）、8.3c（`PregelLoop` 持有 `ChannelManager`，逐超步调 `GetReadyNodes`/`Consume`/`BufferMessage`/`Flush`/`Snapshot`/`Restore`）、8.9b（`GraphState.channel_values` 的序列化）
> 章节归属决策：**`Channel` 接口从 8.3 移入 8.4**（已拍板，见 §5）

## 1. 流程位置与作用

### 1.1 在整体会话流中的位置

Pregel 是「超步（superstep）」调度模型。通道是**边在运行时的化身**，`ChannelManager` 是**超步之间唯一的调度状态机**：它把 Router 产出的 `Message` 落进通道，并回答「下一个超步该跑哪些节点」。没有它，消息无处落地、节点永不被激活。

```
Gateway / IM ──E2A──▶ AgentServer ──▶ AgentSession / Workflow（8.14）
  └─ CompiledGraph._invoke ──▶ Pregel.run(config{ns, session_id})          graph.py:305 / engine.py:222
       └─ PregelLoop.__init__: manager = ChannelManager(graph.channels)    engine.py:26        ← 本节
       └─ PregelLoop.init()                                               engine.py:37
            ├─ 续跑：manager.restore(state.channel_values)                  engine.py:45        ← 本节
            │        for msg in state.pending_buffer: buffer_message(msg)   engine.py:52-53
            └─ 首跑：buffer_message(TriggerMessage(__start__→__start__))
                     manager.flush()                                       engine.py:59-60     ← 本节
       └─ while loop.run_step():                                           engine.py:88
            ① ready = manager.get_ready_nodes()                            engine.py:111       ← 本节
               active = [n for n in ready if n in graph.nodes and n != END] engine.py:114-117
            ② 无 active 且 buffer 空 → 结束；否则只 flush 推进超步            engine.py:119-126   ← 本节
               step > max_step → RecursionError                            engine.py:128-131
            ③ for n in active: manager.consume(n)                         engine.py:133-135   ← 本节
            ④ executor.submit(node) → NodeTask → func + routers.Dispatch   （8.3b / 已完成的 8.5）
            ⑤ for msg in succeed_messages: buffer_message(msg)             engine.py:150-151   ← 本节
               manager.flush()   ← 决定下一超步就绪节点                      engine.py:152       ← 本节
            ⑥ step++（after_step 钩子）                                     engine.py:156-163
       出错：manager.snapshot() → GraphState.channel_values                  engine.py:180-188   ← 本节 → 8.9
```

### 1.2 作用（三个身份）

1. **就绪判定器**：`IsReady`/`GetReadyNodes` 给出超步输入集合，`Consume` 是「读后即清」语义（Trigger 清消息列表、Barrier 清 received）。Python 用 `test_trigger_channel_reset` 专门守这一点——若 consume 不清空，同一节点会在每个超步被反复激活，形成无限循环。
2. **两类边语义的载体**：静态边 = `TriggerChannel`（收到即激活）；汇聚边 = `BarrierChannel`（CNF：AND-of-OR 计数，收满才激活）。`BarrierChannel.Key()` 是 `barrier:(A|B)&C->end` 合成键的**唯一生产者**（`builder.py:44-52` 只把它转手给 `BarrierRouter`）。
3. **检查点对齐锚点**：`Snapshot`/`Restore` 是「中断后能续跑」的关键——通道里「已收到但未消费」的状态必须进 `GraphState.channel_values`，否则恢复后节点永不重激活（Python 的 `test_checkpoint_recovery_deadlock_fix` 守这个死锁）。

### 1.3 Python 事实基线（已逐条核实，勿重查）

| 事实 | 证据 |
|------|------|
| `channels.py` 全文**零** `logger.` 调用 | `grep -c "logger\." channels.py` = 0 ⇒ 本节零日志是正确对齐，不是遗漏 |
| `Channel` ABC 声明 `accept → None`、`consume → Any`，但**两个实现**分别返回 `bool` 与 `None` | `base.py:69-76` vs `channels.py:112-116` / `:181-183`；Go 按实现取 `Accept() bool` / `Consume()` |
| `ChannelManager` 构造时即把「已就绪」的通道节点放入就绪集 | `channels.py:29-31`（注释 `# recover`）；新建空的通道不会命中，只影响预置数据的场景 |
| `flush` 对找不到目标通道**抛** `ValueError` | `channels.py:43-45` |
| `flush` 的 `buffer.clear()` 在循环**之后** | `channels.py:52` ⇒ 抛错时 buffer 原样保留 |
| `flush` 只对 `changed == True` 的节点重算就绪 | `channels.py:47-58`（重复的 barrier 信号 `changed=False`，不会把节点重新加入） |
| `get_ready_nodes` 返回 `list(set)` | `channels.py:60-61` ⇒ 顺序无契约（CPython `str` hash 带 per-process 随机盐） |
| `snapshot` 跳过 `END` 节点 | `channels.py:79-80` |
| `snapshot` 一旦该节点**有任一**通道非空，就存**整串**通道快照（**空占位也保留**） | `channels.py:81-84` ⇒ restore 靠**下标对齐**，占位不可省 |
| `snapshot` 判定空用 `None / [] / {}` 三态 | `channels.py:82`；两个实现只会返回 list，故空 == `len()==0` |
| `restore` 三类**静默**忽略：节点不存在、`len(channels) != len(states)`、单条状态为空 | `channels.py:90-98` |
| `restore` 后若通道 ready 则补进就绪集 | `channels.py:100-101` |
| `restore` 前有 `hasattr(channel, 'restore')` 检查 | `channels.py:97`；ABC 已声明该抽象方法，Go 接口天然保证 ⇒ 该分支**不可达**，不复刻 |
| `BarrierChannel.key` 覆写基类 property，返回合成键 | `channels.py:147-153` |
| 路由键格式：单元素组直出名字，多元素组 `(A|B)` 且**组内排序**，组间 `&`，整体 `barrier:{senders}->{name}` | `channels.py:155-164` |
| 空组 → 该组渲染成 `()`，且 `is_ready` 恒假（`not (∅ & received)`）⇒ 永久不就绪 | `channels.py:162` + `:169-171`；Python 无用例覆盖，属构图错误 |
| `BarrierChannel.accept` 按 sender **去重**（同超步重复信号幂等） | `channels.py:174-179` + 用例 `test_barrier_duplicate_signals` |
| 重复通道 key：`map_key_to_channel` 后者覆盖，`map_node_to_channels` 两者都保留 | `channels.py:26-28`（两个索引独立写） |
| 全链无锁 | `channels.py` 无任何 Lock/Queue；唯一使用者 `PregelLoop` 在单协程里串行调用（`engine.py:111/134/150-152`） |
| `expected_groups` 是 `List[Set[str]]`，构造时**不复制**（存引用） | `channels.py:143` |

## 2. 文件结构

```
internal/agentcore/graph/pregel/
├── doc.go            # 更新：目录树补 channels.go，概述补「通道管理」，Python 路径补 channels.py
├── base.go           # 8.6/8.7 已有（Message / Interrupt / GraphInterrupt）
├── base_test.go      # 8.6/8.7 已有
├── constants.go      # 8.3d 已有（End 常量）
├── config.go         # 8.3d 已有
├── router.go         # 8.5 已有
├── channels.go       # ★ 本节：Channel 接口 + ChannelSnapshot + 两个实现 + ChannelManager
└── channels_test.go  # ★ 本节：17 个 Python 等价用例 + Go 追加用例
```

`channels.go` 内部按项目规范排列：结构体（接口在最前）→ 导出函数 → 非导出函数；**无枚举/常量/全局变量**（除编译期接口断言，按 8.5 先例置于「全局变量」区）。

## 3. API 设计

### 3.1 类型

```go
// ──────────────────────────── 结构体 ────────────────────────────

// Channel 通道接口：节点之间的调度边在运行时的化身。
//
// 定位纪律：本接口由 8.4 提供（方案 B，原计划归 8.3 的 base.py），
// 与两个实现同文件、同测试；8.3 只保留 PregelNode。
// Python: pregel/base.py:53-84 Channel（ABC + key/node_name 两个 property）
type Channel interface {
	// Key 写侧路由键：TriggerChannel 即节点名；BarrierChannel 为 "barrier:...->name" 合成键
	Key() string
	// NodeName 读侧节点名
	NodeName() string
	// IsReady 是否已可激活目标节点
	IsReady() bool
	// Accept 收下消息，返回内部状态是否改变
	// （Python ABC 注解 None，两个实现均返回 bool，Go 按实现取值，见 D-1）
	Accept(msg Message) bool
	// Consume 消费并复位内部状态
	// （Python ABC 注解 Any 且文档称返回可消费输入，但两个实现均无返回值，见 D-1）
	Consume()
	// Snapshot 导出快照（D-2：单结构体替代 Python 的异构 List[Any]）
	Snapshot() ChannelSnapshot
	// Restore 从快照恢复。Python 无返回值、无校验，Go 忠实复刻为无返回值（D-5）
	Restore(state ChannelSnapshot)
}

// ChannelSnapshot 通道快照。
//
// 建模依据：Python 两个通道的快照是异构的（TriggerChannel → List[TriggerMessage]，
// BarrierChannel → List[str]），ChannelManager.snapshot 因此是 Dict[str, List[Any]]。
// Go 合并为单结构体，每个通道只填自己那一半，空判定 = 两字段皆空：
//   - 与 8.6 把 Message/TriggerMessage/BarrierMessage 合并为单结构体 + 判别式的先例一致
//     （那里零行为纯数据，这里同理）；
//   - 8.9b 序列化零特判（接口多态要额外 discriminator）；
//   - 消费侧零类型断言。
//
// 对齐纪律：切片顺序语义 —— Messages 保序；Received 升序（D-4）。
type ChannelSnapshot struct {
	// Messages TriggerChannel 的「已接收未消费」触发消息
	Messages []Message
	// Received BarrierChannel 的「已到达 sender」集合（升序）
	Received []string
}
```

```go
// TriggerChannel 触发通道（1→N 静态边的读侧）：收到触发消息即激活目标节点。
// Python: pregel/channels.py:104-126
type TriggerChannel struct {
	// name 节点名，同时用作 Key 与 NodeName
	name string
	// messages 已接收未消费的触发消息
	messages []Message
}

// BarrierChannel 汇聚通道（N→1）：CNF（AND-of-OR）计数，收满才激活目标节点。
//
// expectedGroups 每个元素是一个 OR 组：每组的任意一个 sender 到达即算该组满足，
// 所有组都满足才算 ready。
//
//	[][]string{{"a"}, {"b"}}      → a AND b（向后兼容）
//	[][]string{{"a", "b"}}        → a OR b
//	[][]string{{"a", "b"}, {"c"}} → (a OR b) AND c
//
// Python: pregel/channels.py:129-189
type BarrierChannel struct {
	// name 目标节点名（NodeName 返回它）
	name string
	// groups OR 组集合（构造期按 Python Set 语义去重，D-9）
	groups []map[string]struct{}
	// received 已到达的 sender 集合（Set 语义，去重）
	received map[string]struct{}
	// routerKey 构造期算好的合成路由键，对齐 Python 在 __init__ 里算 key
	routerKey string
}

// ChannelManager 超步调度的通道管理器：写侧按 msg.Target 路由、读侧按节点名聚合。
//
// 并发纪律：无锁（D-8）。Python 侧无锁，唯一使用者 PregelLoop 在单协程串行调用；
// Go 侧 8.3c 的 PregelLoop 同样在单 goroutine 内调用本类型全部方法，
// 节点任务在别的 goroutine 里执行，但不直接触碰 ChannelManager。
// Python: pregel/channels.py:13-101
type ChannelManager struct {
	// keyToChannel 写侧索引：路由键 → 通道（重复键后者覆盖，D-7）
	keyToChannel map[string]Channel
	// nodeToChannels 读侧索引：节点名 → 通道列表（注册顺序，D-2 下标对齐依赖它）
	nodeToChannels map[string][]Channel
	// readyNodes 就绪节点集合
	readyNodes map[string]struct{}
	// buffer 待下一超步 flush 的消息
	buffer []Message
}
```

### 3.2 构造函数与方法

| 签名 | Python | 语义要点 |
|------|--------|----------|
| `NewTriggerChannel(name string) *TriggerChannel` | `channels.py:105-107` | `name` 同时作 Key/NodeName；`messages` 初始化为空切片 |
| `NewBarrierChannel(nodeName string, expectedGroups [][]string) *BarrierChannel` | `channels.py:141-145` | **深复制**入参并**逐组去重**（Python 存 `Set`，重复元素天然消失，D-9）；`received` 初始化空 map；`routerKey` 构造期算好 |
| `NewChannelManager(channels []Channel) *ChannelManager` | `channels.py:14-31` | 建两张索引；`IsReady()` 为真的通道其节点加入就绪集（`# recover` 分支）；`channels` 为 nil 时得可用空管理器（Python `[]` 等价） |
| `(*ChannelManager) BufferMessage(msg Message)` | `:33-34` | 追加到 buffer（不清空、不去重） |
| `(*ChannelManager) IsEmpty() bool` | `:36-37` | `len(buffer)==0`（只看 buffer，不看 ready） |
| `(*ChannelManager) Flush() error` | `:39-58` | 见 §3.3 |
| `(*ChannelManager) GetReadyNodes() []string` | `:60-61` | **升序**返回（D-4） |
| `(*ChannelManager) Consume(nodeName string)` | `:63-74` | 未知节点直接 return；对每个 `IsReady()` 通道调 `Consume()`；从就绪集删除 |
| `(*ChannelManager) Snapshot() map[string][]ChannelSnapshot` | `:76-86` | 跳过 `pregel.End`；每节点按 `nodeToChannels` 顺序取**全部**通道快照（空占位保留）；整节点全空则不存 |
| `(*ChannelManager) Restore(snapshot map[string][]ChannelSnapshot)` | `:88-101` | 三类静默忽略（D-5）；长度相等才按下标 zip；跳过空快照；恢复后 ready 则加就绪集 |
| `(*ChannelManager) Buffer() []Message` | 直接读 `self.buffer` | 返回**内容副本**（D-9 实施后修订：Go 切片无法复刻 Python list 的活视图别名，交出去会产出「len 还在、元素全零」的幽灵快照）；8.3c 直接 `append(m.Buffer(), extras...)` 即可 |
| `(*TriggerChannel) Accept/Consume/Snapshot/Restore/IsReady/Key/NodeName` | `:109-126` | `Accept` 仅认 `Kind()==KindTrigger` |
| `(*BarrierChannel) Accept/Consume/Snapshot/Restore/IsReady/Key/NodeName` | `:147-189` | `Accept` 仅认 `Kind()==KindBarrier` 且 sender 首次出现；`Key()` 返回合成键、`NodeName()` 返回 name |

### 3.3 `Flush` 的精确语义（出错路径最易错）

```go
func (m *ChannelManager) Flush() error {
	updated := make(map[string]struct{}, len(m.buffer))
	for _, msg := range m.buffer {
		ch, ok := m.keyToChannel[msg.Target]
		if !ok {
			// 对齐 channels.py:43-45 的 ValueError：返回前不清 buffer（clear 在循环之后）
			return fmt.Errorf("通道未找到，目标路由键=%q", msg.Target)
		}
		if ch.Accept(msg) {
			updated[ch.NodeName()] = struct{}{}
		}
	}
	m.buffer = m.buffer[:0] // 对齐 self.buffer.clear()：保留容量、长度归零
	for nodeName := range updated {
		for _, ch := range m.nodeToChannels[nodeName] {
			if ch.IsReady() {
				m.readyNodes[nodeName] = struct{}{}
				break // 对齐 any(...)：任一就绪即可
			}
		}
	}
	return nil
}
```

注意三点：① 出错**不清 buffer**（Python 的 clear 在异常路径之后，不可达）；② `updated` 遍历顺序不影响结果（只是 map 写入就绪集）；③ 只有 `Accept` 返 true 的节点才重算就绪——重复的 barrier 信号不会把节点重新激活。

### 3.4 路由键生成（与 8.5 的 `BarrierRouter` 契约）

```go
// makeBarrierRouterKey 生成合成路由键 "barrier:(a|b)&c->target"。
// 唯一生产者：8.3a builder 只把本方法的返回值转手给 NewBarrierRouter（见 8.5 spec §9）。
// Python: channels.py:155-164
func makeBarrierRouterKey(name string, groups []map[string]struct{}) string
```

规则：单元素组直出该元素；多元素组 `(` + 组内**升序**用 `|` 连接 + `)`；组间 `&`；整体 `barrier:{senders}->{name}`。
边界：`groups` 为空 → `barrier:->name`；空组 → `()` 参与拼接（且该组永不满足，D-6）。
测试须锁死 Python 用例里的三个字面量：`barrier:A&B&C->end`、`barrier:(A|B)&C->end`、`barrier:(A|B)&(C|D)->end`。

## 4. 偏差清单

| 编号 | 偏差 | 依据 | 落地约束 |
|------|------|------|----------|
| D-1 | `Accept() bool` / `Consume()` 无返回值 | Python ABC 注解 `accept → None`、`consume → Any`，但**两个实现**分别返回 `bool` / `None`。Go 按实现取值，接口注释标明分歧 | 若将来出现第三种通道需要 consume 返回值，须重开接口而非改这两处 |
| D-2 | 快照单结构体 `ChannelSnapshot` 替代 `List[Any]` | 对齐 8.6「零行为纯数据合并单结构体 + 判别式」先例；8.9b JSON 零特判；消费侧零断言 | 空判定 = 两字段皆空；`Messages` 保序、`Received` 升序 |
| D-3 | `Flush() error` 替代 `raise ValueError` | Go 无异常；传播路径与 Python 一致（穿出 `RunStep` → `_save_state_on_error` → 重抛） | 出错时**不清 buffer**；错误文案不含节点名之外的上下文（Python 只给 target） |
| D-4 | 顺序确定化：`GetReadyNodes` 升序 + `Received` 快照升序 | Python 返回 `list(set)`，CPython `str` hash 带 per-process 随机盐 ⇒ 跨进程无契约；Go map 迭代 **per-iteration** 随机，只能二选一，选稳的一侧 | 顺序**不携带语义**：同一超步内所有就绪节点都会被提交；顺序只影响提交先后与日志/存档键序 |
| D-5 | `Restore` 无返回值，三类静默忽略 | 用户 2026-11-01 拍板「都先按照 Python 实现」 | 注释须逐条标明「有意复刻静默」，避免后来者当 bug 修 |
| D-6 | `BarrierChannel` 空组保留（永久不就绪，不 panic） | 同上。与 8.3 的 nil-router panic 纪律不同：那里是 typed-nil 必然 panic 的假防御，这里是构图错误导致的确定性死锁，属 Python 语义 | 注释标注「构图错误 ⇒ 该组永不满足」；8.1/8.3a 构造路径不会产生空组 |
| D-7 | 重复通道 key 后者覆盖 | 忠实复刻（`channels.py:26-28` 两个索引独立写） | 注释标注「前者在 nodeToChannels 中成僵尸」 |
| D-8 | 无锁 | Python 无锁；唯一使用者单协程串行调用 | 包文档 + 类型注释写明并发假设；测试用 `-race` 跑但不加并发用例 |
| D-9 | 构造期深复制 + 逐组去重；`Buffer()` 返回**副本**（实施后修订） | Python `expected_groups` 存引用（无消费者依赖可变性，同 8.5 D-5）；Python 用 `Set` 故重复元素天然消失，Go 用 `[]string` 入参须显式去重。`Buffer()` 原拟对齐 `self.buffer` 的别名语义，**实测不可行**：Go 切片是「冻结的 len + 共享底层数组」，`Flush` 的 `clear` 会清零旧视图（旧视图 len 仍在、元素全零 ⇒ 幽灵消息），`append` 是否互相覆盖取决于 cap | `Buffer()` 内部 `append(make([]Message,0,len), m.buffer...)`；8.3c 直接 `append(m.Buffer(), succeedMessages...)`。同包内需活视图时直读 `m.buffer` 并立即复制 |
| D-10 | 快照往返保真推迟 8.9b | `Message.kind` 非导出，JSON 反序列化得到零值 kind，任何通道都会拒收 | 本节只处理内存对象；8.9b 序列化须用 `NewTriggerMessage`/`NewBarrierMessage` 重建 kind（已记 ⤵️ 回填点） |
| D-11 | 不复刻 `hasattr(channel, 'restore')` 分支 | `channels.py:97`；`Channel` ABC 已声明该抽象方法，Go 接口天然保证实现齐全 ⇒ 该分支在 Python 侧也不可达 | 无需注释 |

## 5. 章节归属调整（`Channel` 接口从 8.3 移入 8.4，已拍板）

### 5.1 决策

**方案 B**：`Channel` 接口（Python `pregel/base.py:53-84`）由本节 `channels.go` 提供，计划表 8.3 只保留 `PregelNode`。

### 5.2 依据

1. **接口与实现同文件同测试**：`Channel` 的 7 个方法只有两个实现，且 `Accept() bool` / `Consume()` 无返回值这些形状**只有看完实现才能定**（见 D-1）。把接口单独放在前一节，等于在信息最少的时候锁签名。
2. **8.4 不再阻塞**：本轮即可开工，不必先做 8.3。
3. **两节从此互不依赖**：8.4 只用 8.6 的 `Message` + 8.3d 的 `End`；8.3 只用 8.5 的 `Router`。两者顺序自由。

### 5.3 顺序变化

原注记（8.5 spec §5）：

```
8.5(routers) → 8.3(PregelNode/Channel ABC) → 8.8(GraphState) → 8.4(channels) → 8.3a(builder) → 8.9a → 8.3b → 8.3c → 8.9b
```

本方案下实际实施序（8.4 与 8.3 解除依赖后前移）：

```
8.5 ✅ → 8.4(channels + Channel 接口) → 8.3(PregelNode) → 8.8(GraphState) → 8.3a(builder) → 8.9a → 8.3b → 8.3c → 8.9b
```

8.8 与 8.3/8.4 都无依赖关系，保持原相对次序不动。

### 5.4 代价

计划表 8.3 行的「`PregelNode` / `Channel ABC`」需追加归属说明（不删原文）；8.5 spec §9 与 8.6 spec 首段关于「`Channel` ABC 的 `Accept` 依赖 8.6 `Kind()`」的表述需追加去向。均属 §6 回填清单。已兑现（2026-11-01）：8.3 只落 `PregelNode`（`pregel/node.go`），`Channel` 接口由本节提供。

## 6. 回填清单（只追加，不删旧内容）

1. **`IMPLEMENTATION_PLAN.md` L504（8.4 行）**：`☐` → `✅`。保留原文「就绪节点集合 + `Buffer/Flush/GetReadyNodes/Consume` + `Snapshot`/`Restore`（跳过 END）+ BarrierChannel 的 CNF(AND-of-OR) 判定与 `barrier:...->name` 路由键」，追加 Go 事实：`channels.go`、`Channel` 接口落本节（方案 B）、`ChannelSnapshot` 单结构体、11 条偏差 D-1..D-11、覆盖率、设计文档路径。
2. **`IMPLEMENTATION_PLAN.md` L503（8.3 行）**：追加「`Channel` ABC 已移入 8.4（2026-11-01 方案 B），本节只留 `PregelNode`」。**已兑现（2026-11-01）**：8.3 行已追加该说明并置 ✅。
3. **`IMPLEMENTATION_PLAN.md` 领域八表格后的顺序注记行**（L542）：追加新行，不改原注记——
   `> **8.x 实施顺序（2026-11-01 二次调整）**：8.5 → 8.4 → 8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b（`Channel` 接口移入 8.4 后两节互不依赖，依据见 8.4 spec §5）。原注记保留。`
4. **`docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md`**：
   - 首段「后续消费方：… 8.4（`ChannelManager.flush` 按 `Kind()` 过滤）」——追加「已兑现（8.4 落地）」；
   - §9「**8.3**：`Channel` ABC 的 `Accept(msg Message) bool` 依赖 8.6 的 `Kind()`」——追加「该接口已移入 8.4（方案 B）」；
   - §9「`barrier:...` 合成键由 8.4 的 `BarrierChannel` 生成，builder 只做转手」——追加「已兑现：`BarrierChannel.Key()`（`channels.go`），键格式见 8.4 spec §3.4」。
5. **`docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md`**：首段「后续消费方：8.3（`Channel.Accept(msg Message)`）、8.4（…`Kind()` 过滤…）」与 R2 行——追加「8.4 已落地，`Kind()` 的过滤消费方兑现；`Channel` 接口最终归属 8.4」。
6. **`docs/superpowers/specs/2026-10-31-pregel-config-8.3d-design.md`**：首段「8.4（channels 跳过 END）」——追加「已兑现：`ChannelManager.Snapshot` 跳过 `pregel.End`」。
7. **`internal/agentcore/graph/pregel/doc.go`**：目录树补 `├── channels.go  # Channel 接口 + ChannelSnapshot + Trigger/Barrier 通道 + ChannelManager`；包概述补「通道管理（超步就绪判定 + CNF 汇聚 + 检查点快照）」；`对应 Python 代码` 行补 `pregel/channels.py`。
8. **本节 ⤵️ 8.9b 回填点**：快照序列化保真（`Message.kind` 非导出，反序列化须重建 kind，见 D-10）——写入 8.9 行或 8.9b 目标行。
9. **`internal/agentcore/graph/pregel/base_test.go:89`** 的注释「8.4 通道的过滤条件形式」——追加指向 `channels_test.go` 的真实用例名。

## 7. 测试设计

目标：`channels.go` 覆盖率 **100%**（逐函数），`-race` 干净。测试文件 `channels_test.go`，命名遵循 `TestXxx_场景描述`（中文场景）。

### 7.1 Python 用例 1:1 迁移（17 例）

| Python 用例 | Go 用例 | 断言 |
|---|---|---|
| `test_trigger_channel_reset` | `TestChannelManager_触发通道消费后复位` | consume 后 `IsReady()==false` 且节点不在就绪集；再 flush 只激活新目标，不重新激活已消费节点（防无限循环） |
| `test_barrier_lifecycle` | `TestChannelManager_汇聚通道全生命周期` | 等待→部分到达（不 ready）→全到（ready + 节点入集）→consume（received 空、节点出集）→再单到达（不 ready） |
| `test_barrier_duplicate_signals` | `TestBarrierChannel_同超步重复信号幂等` | 同 sender 两条消息 → `len(received)==1`、不 ready |
| `test_backward_compatible_all_and` | `TestBarrierChannel_全AND` | 3 个单元素组逐个到达，第 3 个才 ready |
| `test_or_group_any_one_suffices` | `TestBarrierChannel_单OR组任一满足` | `{{LLM_1,LLM_2}}` 只到 LLM_1 即 ready |
| `test_cnf_mixed_or_and_and` | `TestBarrierChannel_CNF混合` | `(LLM_1\|LLM_2) AND LLM_3`：只到 LLM_1 不 ready，再到 LLM_3 ready |
| `test_cnf_or_group_alternative_path` | `TestBarrierChannel_CNF替代路径` | 走 LLM_2 而非 LLM_1，再 LLM_3 → ready |
| `test_multiple_independent_branches` | `TestBarrierChannel_多独立分支` | `(A\|B) AND (C\|D)`：A+D → ready |
| `test_empty_received_not_ready` | `TestBarrierChannel_未收到不就绪` | 空 received，即使 groups 非空也不 ready |
| `test_all_senders_in_or_group_complete` | `TestBarrierChannel_OR组全到仍就绪` | `(A\|B) AND C`：A、B 都到后仍不 ready，C 到才 ready |
| `test_consume_resets_state` | `TestBarrierChannel_消费复位` | consume 后不 ready、`len(received)==0` |
| `test_snapshot_restore_cnf` | `TestBarrierChannel_快照恢复CNF` | snapshot 集合 == {A,C}；consume 后不 ready；restore 后 ready |
| `test_router_key_format_single_groups` | `TestBarrierChannel_路由键单元素组` | `barrier:A&B&C->end` |
| `test_router_key_format_or_groups` | `TestBarrierChannel_路由键OR组` | `barrier:(A\|B)&C->end` |
| `test_router_key_format_multiple_or_groups` | `TestBarrierChannel_路由键多OR组` | `barrier:(A\|B)&(C\|D)->end` |
| `test_channel_manager_with_cnf_barrier` | `TestChannelManager_CNF汇聚全链路` | 只到 LLM_1 → 通道 ready 且节点入集 |
| `test_checkpoint_recovery_deadlock_fix` | `TestBarrierChannel_检查点恢复不死锁` | `groups={{LLM_3},{LLM_1,LLM_2}}`，restore `["LLM_1","LLM_3"]` 后 ready |

### 7.2 Go 追加用例（覆盖 Python 无用例的分支）

| 用例 | 覆盖点 |
|---|---|
| `TestChannelManager_Flush目标无通道` | 返回 error、`err != nil`、**buffer 未被清空**（长度不变） |
| `TestChannelManager_构造期恢复就绪` | 构造前先 `Accept` 一条触发消息 → `NewChannelManager` 后节点立即在就绪集（`# recover` 分支） |
| `TestChannelManager_空通道列表` | `NewChannelManager(nil)` 可用：`IsEmpty()`、`GetReadyNodes()` 为空、`Flush()` 无消息时 nil error |
| `TestChannelManager_Snapshot跳过END` | 节点名 `pregel.End` 的通道有数据也不进快照 |
| `TestChannelManager_Snapshot空节点不存` | 全空节点整条不出现于快照 |
| `TestChannelManager_Snapshot保留空占位` | 同节点 Trigger 空 + Barrier 有 `["A"]` → 快照为 `[{}, {Received:["A"]}]`（**下标对齐**，长度 2） |
| `TestChannelManager_Restore长度不匹配静默` | 快照长度 ≠ 通道数 → 不恢复、不 panic、不就绪（D-5） |
| `TestChannelManager_Restore未知节点静默` | 快照含图中不存在的节点 → 忽略其余正常（D-5） |
| `TestChannelManager_Restore空快照跳过` | 空 `ChannelSnapshot` 不调用 Restore、不就绪（D-5） |
| `TestChannelManager_Consume未知节点` | 无操作、不 panic |
| `TestChannelManager_GetReadyNodes升序` | 多节点就绪，返回严格升序（D-4） |
| `TestChannelManager_重复键后者覆盖` | 两个同 Key 通道 → `keyToChannel` 指向后者；`nodeToChannels` 保留两个（D-7） |
| `TestChannelManager_Buffer返回副本` | `Buffer()` 返回副本（改副本不污染内部、内部增长不回灌、Flush 后旧副本完好），锁死 D-9 交棒约束。<br/>（2026-11-02 同步：`ab93f705` 已由"返回内部切片"改为"返回副本"，用例名随之改） |
| `TestTriggerChannel_拒绝汇聚消息` | `Kind()==KindBarrier` → `Accept` 返 false、不 ready |
| `TestBarrierChannel_拒绝触发消息` | `Kind()==KindTrigger` → false、不 ready |
| `TestTriggerChannel_零值消息被拒` | 零值 `Message{}`（kind 空串）→ 两通道都拒收（承接 8.6 的判别式纪律） |
| `TestNewBarrierChannel_深复制入参` | 构造后调用方改 `[][]string` 元素 → 实例行为不变（D-9） |
| `TestNewBarrierChannel_组内去重` | `{{"a","a"}}` 等价 `{{"a"}}`：键为 `barrier:a->x` 且单个 a 到达即 ready（D-9） |
| `TestBarrierChannel_空组永不就绪` | `[][]string{{}}` → 键含 `()`、任意 sender 到达都不 ready（D-6） |
| `TestBarrierChannel_零组` | `nil` groups → 键 `barrier:->x`；收到任一 sender 后 ready（`is_ready` 空循环为真，忠实复刻） |
| `TestChannel_接口断言` | 编译期 `var _ Channel = (*TriggerChannel)(nil)` / `(*BarrierChannel)(nil)` |

### 7.3 覆盖率与回归

```bash
go test ./internal/agentcore/graph/pregel/ -cover -run 'TestChannelManager|TestBarrierChannel|TestTriggerChannel|TestNewBarrierChannel|TestChannel_接口断言'
go tool cover -func=<profile> | grep channels.go   # 逐函数 100%
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./...   # 全量 294 包零 FAIL
```

## 8. 实施顺序建议（供 writing-plans 展开）

1. `channels.go`：`Channel` 接口 + `ChannelSnapshot` + `TriggerChannel` + `BarrierChannel` + `makeBarrierRouterKey` + `ChannelManager` + 编译期断言
2. `channels_test.go`：§7.1 的 17 例 + §7.2 的 21 例；确认 `channels.go` 逐函数 100%
3. `doc.go` 更新（目录树 + 概述 + Python 路径 + 并发纪律说明）
4. 计划表：8.4 行 `☐→✅`（追加 Go 事实）、8.3 行追加归属说明、顺序注记追加新行
5. 三份 spec 追加兑现/去向说明（8.5 / 8.6 / 8.3d）
6. `gofmt` 走 GOROOT 下的二进制（`$(go env GOROOT)/bin/gofmt -l -w`）
7. 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./...`

## 9. 后续衔接提示（本轮不实施）

- **8.3**（`PregelNode`）：字段全非导出 + `NewPregelNode(name string, fn func(ctx context.Context, in any) (any, error), routers ...Router)`，构造时 nil 元素 **panic**（已拍板，见 8.5 spec §9）；`Channel` 接口不再来自本节。
- **8.3a**（`PregelBuilder`）：
  - `addNode(name)` → `NewTriggerChannel(name)` 追加进 `channels`；
  - `addEdge` 的 N→1 → `NewBarrierChannel(target, groups)` 追加，并把 `Key()` 交给每个 sender 的 `NewBarrierRouter(barrierKey)`；
  - START/END 虚拟节点同样各有 `TriggerChannel`（`builder.py:18-19` + `:25`），END 的通道只被 `Snapshot` 跳过、不特殊构造。
- **8.3c**（`PregelLoop`）：
  - `NewChannelManager(graph.channels)` 在 loop 构造期调用；
  - `Flush()` 的 error 须穿出 `RunStep` 并触发 `_save_state_on_error`（Python 是 ValueError 自然上抛）；
  - `_save_state_on_error` 里 `pending := append(m.Buffer(), succeedMessages...)` 即可（`Buffer()` 已返回副本，不会再碰内部切片；D-9 实施后修订）；
  - 首跑必须 `BufferMessage(NewTriggerMessage(Start, Start))` + `Flush()`（`engine.py:59-60`），否则 START 节点永不被激活。
- **8.9 / 8.9b**（`GraphState` / serde）：`channel_values` 的类型为 `map[string][]ChannelSnapshot`，序列化时 `Message.kind` 须用构造函数重建（D-10，本轮 ⤵️ 回填点）。
- **8.1**（`PregelGraph.compile`）：barrier 的 `expected_groups` 由 `_resolve_barrier_groups`（`graph.py:159-215`）算出，本节只消费结果，不参与 CNF 分组推导。
