# 8.3a PregelBuilder 设计（方案 A：Build ⤴️ 8.3c 回填）

> 日期：2026-11-15
> 章节：领域八 8.3a（`PregelBuilder`）
> Python 参考：`openjiuwen/core/graph/pregel/builder.py`（77 行）
> Go 落点：`internal/agentcore/graph/pregel/builder.go` + `builder_test.go`
> 前置依赖：8.3d ✅（常量 Start/End）、8.6 ✅（Message）、8.5 ✅（Router 三实现）、8.3 ✅（PregelNode/addRouter/NoopNodeFunc）、8.4 ✅（TriggerChannel/BarrierChannel）
> 后续消费方：8.1（`PregelGraph` 构图）、8.3c（`Build` 产出 `*Pregel`）

## 1. 流程位置与作用

8.3a 属 Pregel 引擎**构图期（build-time）**，是 `PregelNode`（8.3）+ 三个 Router（8.5）+ 两个 Channel（8.4）的**唯一组装者**：调用方通过它声明节点、边、分支，最终 `Build()` 产出 8.3c 的 `*Pregel` 引擎实例。构图期与执行期之间唯一的交接物在此生产。

Python 事实基线（builder.py）：

| 方法 | 行为 |
|---|---|
| `__init__` | 建 `nodes={}`、`channels=[]`，并立即注册 START/END 两个虚拟节点（fn=`lambda: None`，routers=`[]`） |
| `add_node(name, fn, routers=None)` | `nodes[name]=PregelNode(...)` + `channels.append(TriggerChannel(name))`，返回 self |
| `add_edge(start, end)` | 三形状分派：N→1 barrier（CNF 组，非法元素 raise ValueError；每源节点 append `BarrierRouter([barrier.key])`）、1→N static、1→1 static；其余 `raise ValueError` |
| `add_branch(src, selector)` | `nodes[src].routers.append(ConditionalRouter(selector))` |
| `build(store=None, after_step_callback=None)` | 返回 `Pregel(nodes, channels, store, after_step)`（engine.py:71-77）——**属 8.3c** |

## 2. 已拍板决策（2026-11-15，用户）

1. **方案 A**：8.3a 本轮只做 `NewPregelBuilder`/`AddNode`/`AddEdge`/`AddBranch` + builder 结构体；`Build()` **不声明**（不留壳），挂 ⤴️ 回填标记，待 8.3c 落地后在 8.3c 章节内回填。`IMPLEMENTATION_PLAN.md` 8.3a 行保持 ☐，注明「Build ⤴️ 8.3c 回填」。
2. **AddEdge 入参形状 = 方案 β**：保留单方法 `AddEdge(start, end any) error`，运行时类型断言按 Python 四形状分派，非法组合返回 error（对齐 Python `raise ValueError`）。**内部类型判断统一收口为 `string` / `[]string`**（Python 的 set/tuple/list 在 Go 侧归一化为 `[]string`，即 8.5 偏差 D7 的 Hashable→string 收窄在本节完成归一化）。
3. **8.8 后移**（方案 2）：8.8（GraphState，全仓零实例化死类型）移至 8.9b 附近实施。新实施序：`8.3a → 8.9a → 8.3b → 8.3c → 8.9a → 8.8 → 8.9b`。

## 3. Go 设计

### 3.1 结构体

```go
// PregelBuilder Pregel 图的构图期组装者
type PregelBuilder struct {
    // nodes 节点名 → 节点（对齐 Python self.nodes dict）
    nodes map[string]*PregelNode
    // channels 构图期顺序追加的通道列表（对齐 Python self.channels list）
    channels []Channel
}
```

字段全非导出：消费方（8.3c 的 `Build`）与本类型同包。`NewPregelBuilder()` 复刻 `__init__`：立即注册 `Start`/`End` 两个虚拟节点（`NoopNodeFunc`、零 routers）并各自 append `NewTriggerChannel(name)`。

### 3.2 AddNode

```go
func (b *PregelBuilder) AddNode(name string, fn NodeFunc, routers ...Router) *PregelBuilder
```

Python `routers=None` 默认值 → Go 变参。行为：`nodes[name] = NewPregelNode(name, fn, routers...)` + `channels = append(channels, NewTriggerChannel(name))`，返回 `b`（链式）。

### 3.3 AddEdge（方案 β）

```go
func (b *PregelBuilder) AddEdge(start, end any) error
```

分派矩阵（顺序对齐 builder.py:34-63）：

| start | end | 分支 |
|---|---|---|
| `[]string` | `string` | barrier：每个元素独占一组（全 AND 形态）；`NewBarrierChannel(end, groups)` append 进 channels；对 start 中每个源节点 `nodes[s].addRouter(NewBarrierRouter(barrier.Key()))` |
| `string` | `[]string` | 1→N static：`nodes[start].addRouter(NewStaticRouter(end))` |
| `string` | `string` | 1→1 static：`nodes[start].addRouter(NewStaticRouter([]string{end}))` |
| 其余组合 | | 返回 error `fmt.Errorf("unsupported edge format: start %T -> end %T", ...)`（日志/错误文案英文，规则 3；Python 原文含类型描述，Go 用 `%T` 呈现） |

**已知偏差 A1（OR 组形态无入参通道）**：Python barrier 分支的 start 元素可为 `set/frozenset`，即支持 `(a OR b) AND c` 形态；Go 侧 β 收口为 `[]string` 后仅支持全 AND（每组单元素）。理由：单层 `[]string` 无法承载 CNF 两层嵌套（一层切片会把 AND 组与 OR 组压到同一层级产生歧义）；消费侧若需 OR 组，可直用 `NewBarrierChannel` + 手动 `addRouter` 挂 BarrierRouter，能力未丢失。回收条件：出现真实 OR 组构图需求时，重议入参形状（如追加 `AddBarrierEdge(end string, groups ...[]string)` 辅助方法）。

barrier 语义要点：合成路由键 `barrier:(a|b)&c->target` 由 8.4 `BarrierChannel.Key()` 生成，builder **只转手**给 `NewBarrierRouter`（8.5 spec §9 承诺）。BarrierChannel 满足判定不靠 key（key 只管写侧投递），而靠 `msg.Sender` 记入 `received` 集合后做 CNF 判定（见 8.4 spec §3.4）。

### 3.4 AddBranch

```go
func (b *PregelBuilder) AddBranch(src string, selector Selector) *PregelBuilder
```

`nodes[src].addRouter(NewConditionalRouter(selector))`，返回 `b`。8.5 偏差 D3 的 str→[]string 归一化落点在本节（selector 返回值的归一化由 ConditionalRouter 语义承接，见 8.5 实现现状）。

### 3.5 Build（⤴️ 8.3c 回填）

不声明方法、不留壳（规则 8：禁止兼容壳与占位代码）。Python 原型：

```python
def build(self, store=None, after_step_callback=None):
    return Pregel(nodes=self.nodes, channels=self.channels,
                  store=store, after_step=after_step_callback)
```

回填点：8.3c 落地 `Pregel` 类型后，在同一改动内补 `Build(store, afterStep)`。届时 8.3a 行改 ✅。

### 3.6 日志与注释

builder.py 零 logger 调用 → Go 零日志，对齐。全部注释中文（规范 1），声明排列按规范 2。

## 4. 测试设计

- START/END 预注册：`NewPregelBuilder()` 后 nodes 含两虚拟节点（fn 为 NoopNodeFunc、routers 空）、channels 含两条 TriggerChannel
- 三形状边：1→1 / 1→N / barrier（N→1）各验证 routers 追加与 channel 追加；barrier 的 router 目标 == `BarrierChannel.Key()`（转手一致性）
- error 路径：非法组合（如 `[]string → []string`、`int → string`）返回 error，文案含 `%T`
- 链式返回：`AddNode/AddEdge/AddBranch` 返回同一 `*PregelBuilder`
- `AddNode` 重复名覆盖语义对齐 Python dict 赋值（后者覆盖，无报错）
- 包内测试直读非导出字段；目标覆盖率 ≥85%

## 5. 计划表与注记变更（不丢失清单）

1. `IMPLEMENTATION_PLAN.md` L542/L544/L551 三条顺序注记**原文逐字保留**，注记区**追加第四行**：
   `> **8.x 实施顺序（2026-11-15 四次调整）**：8.3a 按方案 A 落地（Build ⤴️ 8.3c 回填）；8.8 后移至 8.9b 之后实施；新实施序 8.3a → 8.9a → 8.3b → 8.3c → 8.9a → 8.8 → 8.9b（依据见 8.3a spec §2）。原注记保留。`
2. 8.3a 行：原描述原文保留，状态保持 ☐，追加「Build ⤴️ 8.3c 回填」标记；落地后再回填 Go 事实段。
3. 8.8 行：内容不动，追加 ⤵️ 标记注明后移至 8.9b 之后实施（回填点=新位置）。
4. 历史拍板保留核对：2026-11-01 三次记录（L542/L544/L551）中的「本轮不处理」语义已被 2026-11-15 的显式重开取代，但注记原文不删——按追加制保留决策轨迹。
5. 交棒承诺兑现对照（落地时逐条写入 spec §7）：
   - 8.3 spec：`addRouter` 是 8.3a builder 唯一调用方的非导出方法 ✅
   - 8.4 spec：`BarrierChannel.Key()` 唯一生产者是 builder、只转手给 NewBarrierRouter ✅
   - 8.5 spec §9：barrier 合成键由 8.4 生成、builder 转手；D3 归一化上移落点=本节 ✅
   - 8.3d spec：`Start`/`End` 常量由 8.3a 用于虚拟节点 ✅

## 6. 纪律核对

- 规则 9/12：文件一对一（builder.py → builder.go）；方法归属不变；方法体步骤顺序照 builder.py:13-77；唯一偏差 A1 已登记（§3.3）
- 规则 8：不留 Build 壳、不写 `TODO: 删除`
- 规范 3：builder_test.go 覆盖率 ≥85%
- 验收门槛：`go build ./...`、`go vet ./...`、`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/pregel/...`
