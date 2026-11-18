# 8.1 PregelGraph 门面层 设计（spec）

> 日期：2026-11-16
> 状态：已与用户讨论定稿（五项拍板见 §4）
> 对应计划：IMPLEMENTATION_PLAN.md 领域八 8.1
> 上游：`openjiuwen/core/graph/base.py`（58 行）· `graph.py`（409 行）

## 1. 定位

8.1 是 Pregel 图引擎的用户侧门面（Facade）：把声明式构图（DAG 描述）翻译成 Pregel 引擎可执行结构。在整个领域八的层次中，它是 workflow 层（8.14 起）与 pregel 引擎层（8.3~8.9，已收口）之间的接线：

```
用户/编排代码
    ▼
Workflow 层（8.14 起，真实使用面）
    ▼
PregelGraph（8.1 本节）→ PregelBuilder.Build → Pregel 引擎（8.3a/8.3c）
    ▼
Vertex（8.11 已完成）→ Session/Checkpointer
```

前置依赖全部就绪（2026-11-16 核实）：8.2 `BaseExecutable`/`ExecutableGraph`（graph/executable.go、executable_graph.go）、8.10 AtomicNode、8.11 Vertex（含 `Init`/`SetEndNode`/`Reset`）、8.17 前置件 `ComponentAbility`（workflow/components/base.go）、session 接口面（InnerSession/Checkpointer/WorkflowIDProvider）、pregel 全套（Builder/Pregel/Config/Store）。**维持编号序直接实施，无需换章。**

本节为纯新增三文件，无被替代的旧实现（删除清单为空，见 §8）。

## 2. 产出与文件归属

全部落在 `internal/agentcore/graph` 根包（拍板 A，与 Python 目录一一对应；vertex.go/atomic_node.go 等已在此包）：

| Go 文件 | 对应 Python | 内容 |
|---|---|---|
| `base.go` | `base.py` | `Graph` 接口（7 方法）+ `Router` 类型别名 |
| `graph.go` | `graph.py` | `Branch` + `PregelGraph` + `CompiledGraph` + `_getCallableName` |
| `graph_test.go`（或按惯例分 base_test.go/graph_test.go） | — | 覆盖率 ≥85%，目标 100% |

`executable_graph.go`（8.2 已落地）不重做：`CompiledGraph` 实现 `ExecutableGraph` 接口的 `Invoke` 及空方法面。

### 2.1 base.go

```go
// Graph 声明式构图接口
// Python: base.py:39-58 Graph(ABC)
type Graph interface {
    StartNode(nodeID string) *PregelGraph      // Self 返回类型收窄为 *PregelGraph（唯一实现者，偏差 D-1）
    EndNode(nodeID string) *PregelGraph
    AddNode(nodeID string, node Executable, waitForAll bool) *PregelGraph
    AddEdge(sourceNodeID any, targetNodeID string) *PregelGraph
    AddConditionalEdges(sourceNodeID string, router Router) *PregelGraph
    Compile(session session.BaseSession, kwargs map[string]any) ExecutableGraph
    GetNodes() map[string]*Vertex
}

// Router 条件路由选择器。Python: base.py:33-36 Router 类型别名
// （Callable 同步/async 双形态）。Go 无 async，用户函数统一同步返回；
// selector 的真实语义见 §5.3（state 恒 nil，运行时数据靠捕获 session 引用）。
type Router = pregel.Selector
```

注意两个 `Router` 同名不同物：本节的 `graph.Router`（用户 callable 别名）与 8.5 的 `pregel.Router`（IRouter 接口，Dispatch）不同域；Go 侧靠包名区分，doc 注释中说明。

`wait_for_all` 仅 `AddNode` 有此参数：Python 是 keyword-only 默认 False，Go 采用显式 bool 形参（偏差 D-2：Go 无关键字默认值，调用方显式传 false）。

### 2.2 graph.go — Branch / PregelGraph

```go
// Branch 条件分支载体。Python: graph.py:52-54 @dataclass Branch
type Branch struct {
    Condition Router   // Python: condition callable，经 Router 别名收窄
}

// PregelGraph 声明式构图实现
// Python: graph.py:57-308
type PregelGraph struct {
    pregel        *pregel.Pregel            // Python: pregel | None
    edges         []edgeTuple               // (source any, target string) 二元组，对应 Python list[Tuple]
    waits         map[string]struct{}       // set 语义
    nodes         map[string]*Vertex
    branches      map[string]map[string]Branch  // defaultdict(dict)：src → name → Branch
    branchTargets map[string]map[string]struct{}// set 语义，对应 branch_targets
    checkpointer  session.Checkpointer
    sessionRef    session.BaseSession       // Python: _session
}
```

构造函数 `NewPregelGraph()`：Python `__init__` 无参，Go 返回 `*PregelGraph`，`branches`/`waits` 等 map/slice 就地初始化。

方法逐一对应（步骤顺序保持 Python 原序）：

- `StartNode(nodeID)`：`_validateNodeID` → `addEdge([]string{START}, nodeID)`（Python 传 `[START]` 列表形态，保留 list 源形态）；
- `validateNodeID`：空串 → build_error 对齐 `StatusCode.PREGEL_GRAPH_NODE_ID_INVALID`（沿用项目已有错误码体系，若 8.1 前无此码则按 exceptions 包惯例新增）；
- `EndNode(nodeID)`：校验 → `nodes.get` 命中则 `vertex.SetEndNode(true)`（8.11 已有）→ `addEdge([]string{nodeID}, END)`；
- `AddNode(nodeID, node, waitForAll)`：校验 id/节点非 nil → 重复注册 error（对齐 `PREGEL_GRAPH_NODE_ID_INVALID` "already exist"）→ 存 `Vertex`（8.11 构造）→ waitForAll 时入 waits；
- `GetNodes()`：返回副本 dict（Python `{k: v for ...}` 浅拷贝，同构）；
- `AddEdge(sourceNodeID any, targetNodeID)`：校验 source 非 nil/非空、list 逐项非空、target 非空（四个 error 分支顺序照抄 graph.py:103-120）→ append edgeTuple。**只登记不建图**；
- `AddConditionalEdges(sourceNodeID, router)`：校验 id/router → `name := getCallableName(router)` → `branches[src][name] = Branch{router}`（map 覆盖语义保留 dict 行为）；
- `RegisterBranchTargets(branchNodeID, targets)`：`branch_node_id && targets && len>1` 才登记（照抄 graph.py:134-143 的守卫）；
- 私有方法一比一：`forwardReachable`（BFS，`queue.pop(0)` → Go 切片头取）、`buildBranchParent`、`branchRoot`（静态函数）、`resolveBarrierGroups`（五步 CNF 解析，步骤注释编号保留）；
- `Compile(session, kwargs)`：每个 node `vertex.Init(session, kwargs)` → 定义 `afterStep` 闭包（sessionRef 非空时 `state().commit()` + GRAPH_SUPER_STEP_END 日志，字段 graph_id/session_id/ns/step/active_nodes 对齐 graph.py:243-256）→ pregel==nil 时取 checkpointer/graphStore、`_compile(graphStore, afterStep)`、记 sessionRef；否则只更新 sessionRef → 返回 `CompiledGraph{pregel, checkpointer}`；
- `Compile` 的私有对应 `_compile(graphStore, stepCallback)`：三步（Step1 waits 收集 sources 单元素组、Step2 resolveBarrierGroups、Step3 转 builder）逐行对应 graph.py:267-303，`builder.AddEdge`/`AddBranch` 用 8.3a/8.3c 已落签名（`AddEdge(start, end any)`、`AddBranch(src, selector)`、`Build(store, afterStep)`）；
- `Reset()`：遍历 nodes 调 `vertex.Reset()`（8.11 已有）。

### 2.3 graph.go — CompiledGraph

```go
// CompiledGraph 编译产物，ExecutableGraph 的图实现
// Python: graph.py:310-400
type CompiledGraph struct {
    pregel       *pregel.Pregel
    checkpointer session.Checkpointer
}
```

`Invoke`（对应 Python `_invoke`，graph.py:315-394；`invoke` 包装层 `inputs.get(INPUTS_KEY/CONFIG_KEY)` 的解包已在 ExecutableGraph 契约中，按 8.2 已落形状对接）步骤照抄：

1. `isMain=false`；取 sessionID；workflowID 经 `session.WorkflowIDProvider` 类型断言（对齐 Python `session.workflow_id()`，hasattr 模式）；
2. config 为 nil → isMain=true，构造 `pregel.NewPregelConfig(sessionID, workflowID, pregel.MaxRecursiveLimit)`；
3. isMain → `checkpointer.PreWorkflowExecute(ctx, session, inputs)`；
4. inputs 非 `InteractiveInput` → `session.State().CommitUserInputs(inputs)`（session state 已有对应能力，缺则同改动补齐）；
5. `pregel.Run(config)`（8.3c 已落，返回 `(map[string]any, error)`）：error 分三类——
   - `context.Canceled`/`errors.Is(err, context.Canceled)`：对应 Python `asyncio.CancelledError` 分支（graph.py:335-341 先记 GRAPH_END 日志再 re-raise → Go 记日志后原样上抛，进入第 7 步取消分流）；判定用 `errors.Is`，8.3b D5 有先例；
   - 其他 error：exception = err；
   - 无 error：result = 返回值；
6. isMain → `checkpointer.PostWorkflowExecute(ctx, session, result, exception)`；非 isMain 且 exception 非 nil → 上抛 exception；
7. **取消分流**（graph.py:349-394，HITL 快照保留，中文注释语义逐条复刻）：
   - `checkpointer.GraphStore().Get(ctx, sessionID, workflowID)` 查图侧快照；
   - 查询 err != nil → 快照按 nil 处理（不落盘不清除），记 warning 日志（Msg 英文化："Failed to inspect graph checkpoint on workflow cancellation"，字段 exception/session_id/workflow_id）；
   - 快照非 nil 且 `IsResumable()`（pregel/state.go:48，与引擎 isResume 同口径共享）→ `PostWorkflowExecute(ctx, session, {}, canceledError)` 带非 nil 异常走保存分支保留现场；保存返回的 canceled 类错误吞掉（对齐 checkpointer 保存分支 re-raise 语义）；其他保存错误记 Error 日志（"Failed to preserve checkpoint on workflow cancellation"）但不顶替原始取消链；
   - 快照 nil（全新执行中途取消）→ 不落盘，保留可重跑语义；
   - 最后 re-raise 原始取消错误。

`Stream`/`Interrupt` 复刻 Python 的 pass 空方法（stream 返回空迭代/interrupt 直接 return），注释标注「Python 为 pass 占位」（用户 2026-11-16 拍板：空方法照实保留）。

### 2.4 getCallableName

方案 A（2026-11-16 拍板）：反射取名 + 保留覆盖键 + 登记匿名差异。

```go
// getCallableName 反射取名，对应 Python graph.py:403-409 三级回退
func getCallableName(r Router) string
```

三级实现：
1. Router 底层为 func（`reflect.ValueOf(r).Kind() == reflect.Func`，含 `SelectorFunc`）→ `runtime.FuncForPC(reflect.ValueOf(r).Pointer()).Name()`，去包路径取短名（对齐 Python 裸函数名 `func.__name__`）；
2. 非 func 类型 → `reflect.TypeOf(r).Name()`（对齐 `func.__class__.__name__`）；
3. 兜底 `fmt.Sprintf("%v", r)`（对齐 `repr(func)`；Go 中实际不可达，留档）。

**消费点唯一**：`branches[src]` 的 map key（同源同名覆盖、异名并存）；`_compile` 遍历不读 name。

**已知偏差 D-3（匿名函数覆盖语义）**：Python 两个 lambda 注册到同一 source 均名 `"<lambda>"`，后者覆盖前者；Go 匿名闭包经 FuncForPC 得 `pkg.funcN` 各不相同，不触发覆盖。理由：Go 无 lambda、funcN 为编译器行为，强行归一引入编译顺序敏感的脆弱语义；且该语义仅在「同源注册两个匿名 selector 且期望覆盖」的罕见场景暴露。回收条件：上游给 Selector 增加显式命名机制时对齐。已获用户 2026-11-16 拍板。

## 3. 依赖与数据流

- pregel →（已存在）：`Pregel`/`PregelBuilder`/`PregelConfig`/`START`/`END`/`MaxRecursiveLimit`/`Selector`/`Store`/`IsResumable`；
- graph 根包 →（已存在）：`Vertex`（Init/SetEndNode/Reset）/`ExecutableGraph`/`Executable`；
- graph 根包 → session：经 `session.BaseSession`/`Checkpointer`/`WorkflowIDProvider`（包环核实：session/interfaces 已 import pregel，graph 根包 import session 不构成环——pregel 不 import graph 根包，8.3c B5 已把共享类型沉到 pregel/state.go）；
- store：`checkpointer.GraphStore()` 返回 `pregel.Store`（8.9a 收窄先例），`_compile` 直传 `builder.Build(store, afterStep)`。

## 4. 已拍板决策（2026-11-16 讨论）

| # | 决策 |
|---|---|
| 1 | `type Router = pregel.Selector` 纯别名（方案 A），用户裸函数经 8.5 `SelectorFunc` 适配（既定惯例，非新偏差） |
| 2 | 全部进 `internal/agentcore/graph` 根包（方案 A） |
| 3 | CompiledGraph 取消分流按 §2.3 第 7 步三分支复刻，`IsResumable()` 复用 pregel/state.go 同口径方法 |
| 4 | `getCallableName` 反射取名方案 A，匿名覆盖差异登记偏差 D-3 |
| 5 | `AddConditionalEdges` 只收 `Router`，零新增便利 API（方案 A） |
| 6 | `ExecutableGraph` 空方法（stream/collect/transform/interrupt）照实保留 pass 空方法 |

## 5. 语义澄清记录（讨论结论）

1. **selector 运行时数据路径**：上游引擎层 dispatch 只传 `state=None`（router.py:37，全仓唯一带 state 形参的包装器收下即丢），selector 看不到节点输出；真实运行时判断靠 workflow 层（`_workflow.py:277-281` BranchRouter.set_session / 闭包捕获 session）在构图期捕获 session 引用、dispatch 期对 session 求值。Go 闭包/结构体捕获同构；`state any` 恒 nil 是忠实复刻（8.5 spec §3.2 已核对），形参嗅探不可行属 8.5 D2 同源已拍板事项。
2. **selector 的真实用户是 workflow 层（8.14/8.19）**：用户条件（表达式/函数）由 BranchRouter/Condition 翻译成 selector，端上用户不感知。8.1 API 面完全由 Python 对应物决定，不加便利设计。
3. **str 归一化与 async 分派不归 8.1**：str→[]string 归一化按 8.5 D3 上移调用方（用户返回 []string）；async 分派 Go 侧不存在。

## 6. 测试方案

- 单测同包 `graph_test.go`：构图方法（校验分支全覆盖）、CNF 解析（互斥前驱 OR 组、嵌套 branch 提升、standalone 前驱）、Compile 三段流程、CompiledGraph invoke 全生命周期（fake session/checkpointer 注入）、取消三分支（可续跑保留/不可续跑不落盘/查询失败 warning）、getCallableName 三级 + 覆盖键语义；
- 覆盖率目标：包整体 ≥85%，新文件 100%；
- 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL。

## 7. 已知偏差汇总

| 编号 | 内容 | 理由 | 回收条件 |
|---|---|---|---|
| D-1 | `Graph` 接口 Self 返回类型收窄为 `*PregelGraph` | Python `typing.Self` 唯一实现者即 PregelGraph，Go 泛型 Self 无对应物 | 出现第二个 Graph 实现者时改泛型接口 |
| D-2 | `AddNode` 的 `wait_for_all` 为显式 bool 形参 | Go 无关键字默认参数 | 无（语言差异，永久） |
| D-3 | 匿名闭包 selector 不触发同名覆盖 | funcN 为编译器命名，见 §2.4 | 上游增加显式命名机制时对齐 |

## 8. 删除清单

本节为纯新增（base.go/graph.go/测试），无被替代的旧实现、无兼容壳、无残留清理项；grep 锚点验证随实施进行。
