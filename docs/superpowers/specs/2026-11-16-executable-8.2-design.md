# 8.2 Executable / ExecutableGraph 设计（方案 B 顺序：8.2 → 8.11 → 8.1 → 8.12）

> 对齐上游：`openjiuwen/core/graph/executable.py`（63 行）+ `openjiuwen/core/graph/base.py`（58 行，其中 ExecutableGraph/Router 部分属本节）
> 日期：2026-11-16　状态：设计已批准（讨论纪要见 §1）

## 1. 背景与流程位置

8.2 是整个组件体系的能力契约层，位于 agent 会话执行链的关键位置：

```
Workflow/Graph → Graph.compile(session) → CompiledGraph（ExecutableGraph 子类，8.1）
    → Pregel 调度引擎逐节点执行（8.3b/8.3c）
    → Vertex（8.11）包装每个节点的 Executable，读取四个策略钩子：
        skip_trace     → 是否记 tracer span（vertex.py:89/576/811）
        graph_invoker  → 是否子图递归入口（vertex.py:666）
        post_commit    → 执行完是否提交 WorkflowState（vertex.py:368）
        component_type → span 组件类型标签（tracer/handler.py:554-579 特殊处理）
    → 具体组件（8.17-8.26）实现 on_invoke/on_stream/on_collect/on_transform 四入口
```

四入口构成"单/流 × 单/流"矩阵：
- `on_invoke`：单进单出，普通调用（LLMComp/ToolComp 等）
- `on_stream`：单进流出，打字机式流式产出（消费方 8.12 StreamActor）
- `on_collect`：**流进单出（聚合）**，上游多块流入、攒齐汇总成一份输出（Reducer 类节点）
- `on_transform`：**流进流出（逐块变换）**，进一块出一块（过滤/格式转换/透传改写）

## 2. 实施顺序调整（第十一次记录）

**拍板：方案 B。新实施序 8.2 → 8.11 → 8.1 → 8.12（原十次序 8.12 → 8.11 → 8.1/8.2 → 8.13-8.26 中 8.2 提前）。**

依据：
- 8.2 零上游依赖（仅依赖 session.BaseSession，Go 已有），是纯契约层；
- 8.11 Vertex 强依赖 8.2 的 Executable 四钩子（vertex.py 六处消费），必须先定型接口形状；
- 8.1 的 CompiledGraph 继承 ExecutableGraph，编译期依赖 8.2 类型，故 8.1 移到 8.2 之后；
- 8.12 StreamActor 与 8.2 无类型依赖，排在 8.1 之后无阻塞。

回填与移交：
- ⤴️ 8.1：`CompiledGraph` 消费 `ExecutableGraph.Invoke_` 的具体实现、`Graph`/`PregelGraph`/`CompiledGraph` 骨架，本节只落契约层；
- ⤴️ 8.11：Vertex 消费四钩子（前置件 `ComponentAbility` 枚举已于 8.10 落在 workflow/components/base.go，已回填）；
- ⤴️ 8.12：session 的 `ActorManager any` 占位回填（占位现状：session/doc.go + checkpointer/base_test.go:144 testSession）。

## 3. Go 实现

### 3.1 文件与包位置

包 `internal/agentcore/graph/`（已建，8.8/8.10 同包）：

| Go 文件 | 对应 Python | 内容 |
|---------|------------|------|
| `executable.go` | `executable.py` | `Executable` 接口、`StreamChunk`、`GeneralExecutor` 别名、`BaseExecutable` 基座 |
| `executable_graph.go` | `base.py`(12-30, 33-36) | `ExecutableGraph` 接口、`InputsKey`/`ConfigKey` 常量 |
| `doc.go` | — | 更新文件树（同包已有 graph_state.go / atomic_node.go 条目） |

### 3.2 Executable 泛型接口

```go
// StreamChunk 流式元素：Val 为正常产出，Err 非 nil 表示流中错误（此后流关闭）
type StreamChunk[O any] struct {
    Val O
    Err error
}

// Executable 可执行节点契约，对齐 Python Executable[Input, Output] 泛型基类
type Executable[I, O any] interface {
    // OnInvoke 单进单出（Python on_invoke）
    OnInvoke(inputs I, sess session.BaseSession, kwargs map[string]any) (O, error)
    // OnStream 单进流出（Python on_stream），返回前先判启动错误；流中错误经 StreamChunk.Err 送达
    OnStream(inputs I, sess session.BaseSession, kwargs map[string]any) (<-chan StreamChunk[O], error)
    // OnCollect 流进单出（Python on_collect），聚合上游多块输入为一份输出
    OnCollect(inputs <-chan I, sess session.BaseSession, kwargs map[string]any) (O, error)
    // OnTransform 流进流出（Python on_transform），逐块变换转发
    OnTransform(inputs <-chan I, sess session.BaseSession, kwargs map[string]any) (<-chan StreamChunk[O], error)
    // SkipTrace 是否跳过 tracer 埋点（默认 false）
    SkipTrace() bool
    // GraphInvoker 是否子图递归入口（默认 false）
    GraphInvoker() bool
    // PostCommit 执行完是否提交 WorkflowState（默认 true，与其他钩子默认值不对称）
    PostCommit() bool
    // ComponentType 组件类型标签，写进 span（默认空串）
    ComponentType() string
}

// GeneralExecutor 最一般的执行器：map 载荷进、map 载荷出
// （对齐 Python 同名别名 executable.py:63，全仓零消费死别名，纯复刻不发明调用方）
type GeneralExecutor = Executable[map[string]any, map[string]any]
```

形状拍板（用户 2026-11-16 确认）：
- 泛型接口（方案 B 顺序讨论中选定），Input/Output 为泛型参数；
- 流式签名 **A'**：`(<-chan StreamChunk[O], error)`——第二返回值承载启动错误，流中错误经 `StreamChunk.Err` 作为最后一个元素送达后关闭 channel；对齐仓内 `Stream(...) (<-chan AssistantMessageChunk, error)` 惯例（AssistantMessageChunk 内嵌 error 字段的先例）；
- Python `**kwargs` → `map[string]any`（对齐 8.10 AtomicNode 先例）；
- Python `AsyncIterator[Input]`（收方向）→ `<-chan I`；输入流异常语义 → ctx 取消 + channel 关闭（偏差 D3）。

### 3.3 BaseExecutable 嵌入基座

Go 接口无方法默认实现，Python 继承的默认行为由嵌入基座承载（偏差 D1，见 §6）：

```go
// BaseExecutable Executable 嵌入基座：提供四钩子默认值与四入口默认 NotImplementedError
// 组件嵌入本基座后按需覆写；对应 Python Executable 基类默认实现
type BaseExecutable[I, O any] struct{}

func (BaseExecutable[I, O]) SkipTrace() bool      { return false }
func (BaseExecutable[I, O]) GraphInvoker() bool   { return false }
func (BaseExecutable[I, O]) PostCommit() bool     { return true }  // ⚠️ 默认 true，与其他钩子不对称
func (BaseExecutable[I, O]) ComponentType() string { return "" }

// 四个 On* 默认返回 NotImplementedError 复刻（偏差 D2：不带实现类名）
```

NotImplementedError 复刻：错误文案对齐 executable.py:16-21 的提示语义（"组件未实现 on_invoke，请在子类覆写提供推理逻辑"四条各自的措辞）。偏差 D2：Python 用 `type(self).__name__` 拼类名，Go 基座经 `reflect` 只能拿到 `BaseExecutable` 而非真实实现类名，故文案不带类名、改为通用提示；回收条件：若后续需要类名，由组件覆写时自带。

### 3.4 ExecutableGraph 与信封拆分

```go
const (
    // InputsKey 顶层调用信封中业务输入的键（Python INPUTS_KEY = "inputs"）
    InputsKey = "inputs"
    // ConfigKey 顶层调用信封中执行配置的键（Python CONFIG_KEY = "config"）
    ConfigKey = "config"
)

// ExecutableGraph 可执行图契约，对齐 Python ExecutableGraph(Executable[I,O])
type ExecutableGraph[I, O any] interface {
    Executable[I, O]
    // Invoke 拆信封：inputs={inputs:..., config:...} → Invoke_(inputs[InputsKey], sess, inputs[ConfigKey])
    Invoke(inputs map[string]any, sess session.BaseSession) (O, error)
    // Stream/Collect/Transform/Interrupt Python 默认 pass（非抛错！）：
    // Go 返回零值 + nil error；流式两方法返回已关闭的空 channel
    Stream(inputs map[string]any, sess session.BaseSession) (<-chan StreamChunk[O], error)
    Collect(inputs <-chan map[string]any, sess session.BaseSession) (O, error)
    Transform(inputs <-chan map[string]any, sess session.BaseSession) (<-chan StreamChunk[O], error)
    Interrupt(message map[string]any) error
    // Invoke_ 抽象 _invoke 的导出对应物（偏差 D4），8.1 CompiledGraph 实现并消费
    Invoke_(inputs I, sess session.BaseSession, config any) (O, error)
}
```

注意：Python `ExecutableGraph.stream/collect/transform/interrupt` 默认是 **pass（静默空实现）**，与父类四个 on_* 的 NotImplementedError 不同——Go 侧默认返回零值+nil error（流式返回已关闭空 channel），**不**走基座抛错。`Invoke/Stream/Collect/Transform/Interrupt` **进接口**（2026-11-17 审查修订：接口声明完整方法面，外层调用方一律走接口方法 `g.Invoke/g.Stream`，覆写经接口派发天然可达）；Python 的方法默认实现（invoke 拆信封体 / stream 等 pass 体）在 Go 接口上无承载点，由 `DefaultGraphXxx` 包级泛型函数（`DefaultGraphInvoke/DefaultGraphStream/DefaultGraphCollect/DefaultGraphTransform/DefaultGraphInterrupt`）单点承载，实现者沿用默认时一行转手、覆写时写真实现——避免"手抄 pass"导致默认体漂移。

信封拆分语义（对齐 base.py:13-14）：
- `inputs.get(INPUTS_KEY)` / `inputs.get(CONFIG_KEY)` 为宽容取值：**config 缺失传 nil 不报错**；
- 业务输入缺失=硬错误的语义在 Python 是消费方行为（loop_comp.py:171 `raise KeyError`），留给 8.20 LoopComp 复刻，本节只做 `.get` 宽容语义；
- 打包只发生在"跨图/跨组件边界"（workflow.py:527/600/633、vertex.py:137/156），节点到节点内部传递不重新打包。

### 3.5 Router 类型别名

base.py:33-36 的 `Router`（同步/异步 Callable 返回 Hashable 或 list）在 Go 侧**不另造类型**——8.5 已落地的 `pregel.Selector`/`SelectorFunc` 即其对应物（8.5 spec D2/D7 已登记 state 恒 nil、目标收窄 string）。spec 层面注明指向关系，代码不重复定义，避免两套 Selector 并存（规则 8 精神）。

## 4. 已知偏差

| 编号 | 性质 | Python 事实 | Go 等价 | 理由 | 回收条件 |
|------|------|------------|---------|------|---------|
| D1 | 继承 → 嵌入 | `Executable` 抽象基类，子类继承获默认实现 | `BaseExecutable[I,O]` 嵌入基座 | Go 无继承；嵌入是标准对应物，非拆接口（规则 10 仅限 DeepAgent，不适用） | 永久（语言差异） |
| D2 | 错误文案去类名 | NotImplementedError 文案含 `type(self).__name__` | 通用提示文案，不带类名 | reflect 拿不到接口真实实现类名 | 若后续需要类名，组件覆写时自带 |
| D3 | 输入流异常语义 | `AsyncIterator[Input]` 迭代中异常沿 async 链上抛 | ctx 取消 + channel 关闭 | Go channel 无异常通道概念 | 永久（语言差异） |
| D4 | `_invoke` 导出 | 抽象方法 `_invoke`（非导出命名） | 导出 `Invoke_`（下划线后缀保留名字痕迹） | Go 接口方法必须导出才能被包外实现/消费 | 永久（可见性规则） |
| D5 | 默认 pass 语义 | 子类 `stream/collect/transform/interrupt` 默认 pass；方法默认实现随类继承 | 接口声明全量方法面 + `DefaultGraphXxx` 包级泛型函数单点承载默认体，实现者一行转手 | Go 接口无方法默认实现；默认体单点避免手抄漂移（2026-11-17 审查修订） | 永久（语言差异） |
| D6 | 死别名复刻 | `GeneralExecutor` 全仓零消费 | 类型别名保留，不发明调用方 | 项目惯例：零调用死代码仍复刻 | — |

## 5. 测试设计

覆盖率目标 ≥85%（契约层争取 100%）：

1. 四钩子默认值（重点断言 **PostCommit=true** 的不对称默认）；
2. 四个 On* 默认 NotImplementedError 文案逐条断言；
3. ExecutableGraph 信封拆分：inputs/config 齐全、仅 config 缺失（传 nil）、仅 inputs 缺失（宽容 nil）、信封为 nil；
4. StreamChunk 契约：错误作为末元素送达后 channel 关闭、正常流关闭不带 Err；
5. GeneralExecutor 可实例化（map 载荷往返）；
6. 默认 pass 分支：Stream/Collect/Transform/Interrupt 空实现返回零值。

## 6. 交付物清单

- `internal/agentcore/graph/executable.go` + `executable_test.go`
- `internal/agentcore/graph/executable_graph.go` + `executable_graph_test.go`
- `doc.go` 文件树更新
- `IMPLEMENTATION_PLAN.md`：8.2 行打 ✅ 并写 Go 事实；追加第十一次顺序调整记录；8.1 行补 ⤴️ 注记（ExecutableGraph/Router 自 8.1 前移、Invoke_ 消费留 8.1）；8.11/8.12 行核对回填注记
- 本 spec 提交
