# 8.10 AtomicNode 设计（含 ComponentAbility 前置件）

> 状态：设计已获用户批准（2026-11-16 会话，方案 B+① 拍板）
> Python 参考：`openjiuwen/core/graph/atomic_node.py`（48 行）、`openjiuwen/core/workflow/components/base.py`（ComponentAbility 段）

## 1. 背景与流程位置

AtomicNode 位于图执行链路的最内层包装：

```
PregelLoop.RunStep (8.3c) → TaskExecutorPool (8.3b) → Vertex (8.11)
    → Vertex.invoke 内部 → atomic_invoke(config, session)   ← 8.10
        → 校验 session 及其 state 是 CommitState
        → self._atomic_invoke(...)（子类真正的节点执行逻辑）
        → session.state().commit_cmp()（提交 comp_state / io_state 暂存更新）
```

消费方（Python）：

| 基类 | 消费者 | 运行时特征 |
|------|--------|-----------|
| `AsyncAtomicNode` | `Vertex`（8.11，`class Vertex(AsyncAtomicNode, StreamConsumer)`） | asyncio 管线：`__call__` → `atomic_invoke` → `_atomic_invoke → await self.call(config)`（trace/装配输入/stream/事件 trigger）→ `commit_cmp` |
| `AtomicNode`（同步） | `Condition`（8.19 谓词）、`AdvancedLoopComponent`（8.20 selector 身份） | 条件边 dispatch 阶段同步直调：Pregel 引擎 submit → 节点任务执行 node.func → 任务内 `r.dispatch`（task.py:157-160）→ ConditionalRouter.selector → `BranchRouter.__call__` → `Branch.evaluate(session)` → `Condition.__call__` → `atomic_invoke` |

同步/异步之分不是功能区别（两个基类包装逻辑逐字相同），而是被包装业务逻辑自身是否需要 `await`：Vertex 管线全 async；Condition/LoopComp 的条件求值是纯同步业务（读输入算 bool）。Python 双基类源于 asyncio 限制。

关键时序保证：`commit_cmp`（节点任务内）先于 `r.dispatch`（同一任务内 task.py:157），故 selector 求值时上游节点输出必然已提交（`set_outputs` → `commit_cmp` → `get_inputs`/`get_global` 读取），数据经 session 状态间接流转，无参数直传。Go 侧 task.go:302-304 已按 Python 复刻任务内 dispatch，8.10 无需改动 engine/task。

## 2. 拍板记录

- **方案 B**：8.10 保持最小化，同批提前抽取 `ComponentAbility` 枚举（8.11 的前置件）并注销 8.11 的 ⤴️；`StreamConsumer` 留给 8.12。
- **方案 ①**：Python `AtomicNode`/`AsyncAtomicNode` 双基类合并为单个可嵌入 Go 结构体；async 语义由 8.11 Vertex 自身管线承载。

实施序落点：领域八既有序列注记追加——8.10 → 8.12（StreamActor，含 session ActorManager ⤴️ 回填）→ 8.11（Vertex）→ 8.1/8.2 → 8.13-8.26 组件。

## 3. 8.10 Go 形状

位置：`internal/agentcore/graph/atomic_node.go`（Python `graph/atomic_node.py` 一文件对一文件；graph 包根已由 8.8 建立，doc.go 同步文件树）。

```go
// AtomicNode 原子节点：节点级状态提交的原子性包装。
// Python: openjiuwen/core/graph/atomic_node.py（AtomicNode/AsyncAtomicNode 合并，偏差 D1）
type AtomicNode struct {
    // atomicInvokeFunc 原子执行回调（对应 Python 抽象方法 _atomic_invoke，
    // 由嵌入方在构造时注入——Go 跨包无法要求实现非导出方法，偏差 D3）
    atomicInvokeFunc func(ctx context.Context, kwargs map[string]any) (any, error)
}

func NewAtomicNode(atomicInvokeFunc ...) AtomicNode   // nil 即 panic（对齐 ABC 抽象方法必须实现）
func (a *AtomicNode) AtomicInvoke(ctx context.Context, kwargs map[string]any) (any, error)
```

- `AtomicInvoke` 步骤一比一复刻 Python `atomic_invoke`（atomic_node.py:26-31）：①kwargs 取 session → ②校验 → ③执行回调 → ④`CommitCmp()` → ⑤返回结果。任一步失败不提交、错误原样上抛。
- `kwargs map[string]any` 对齐 Python `**kwargs`；session 为其中一个键，其余键原样透传给回调。
- `AsyncAtomicNode` 不复刻（D1）。
- 注释全中文；文件声明排列按 go-code-conventions 规范 2。

## 4. 校验三分支（atomic_node.py:13-22 一比一）

`_validateSessionAndState` 非导出函数，三道检查顺序不可调换：

1. `session == nil` → `GRAPH_STATE_COMMIT_ERROR, reason="session is None"`
2. `isinstance(session, BaseSession)` → Go 侧接口类型编译期保证，仅保留 nil 判断（偏差 D2，注销）
3. `session.State()` 断言 `state.WorkflowState`（= Python `isinstance(state, CommitState)`；`internal/agentcore/session/state/state.go` 的 `WorkflowState` 接口注释已明确对应关系，含 `CommitCmp()`）→ 失败报 `GRAPH_STATE_COMMIT_ERROR, reason="session is not support commit state"`

错误码：`internal/common/exception/codes_graph.go` 已有 `GRAPH_STATE_COMMIT_ERROR = 112030`，直接使用。
异常路径按项目规则 3 补 Error 日志（`.Err(err)` + 上下文字段），Msg 英文化（feedback-logs-english）。

## 5. ComponentAbility 前置件

位置：`internal/agentcore/workflow/components/base.go`，新建 components 子包（对应 Python `workflow/components/base.py`；同文件其余类型 `WorkflowComponentMetadata/ComponentConfig/ComponentState` 属 8.17 段，本节不抽）。

- 四成员：`ComponentAbilityInvoke/Stream/Collect/Transform`。
- Python 元组值 `("invoke","batch in, batch out")` / `("stream","batch in, stream out")` / `("collect","stream in, batch out")` / `("transform","stream in, stream out")` → Go iota + `Name()`/`Desc()` 方法（映射表承载）。
- 成员集合完整，不得裁剪（规则 9.2.4）。
- IMPLEMENTATION_PLAN 8.11 行 ⤴️「依赖 8.17 段 ComponentAbility 提前抽取」更正为已兑现。

## 6. 已知偏差

| 编号 | 内容 | 理由 | 回收条件 |
|------|------|------|---------|
| D1 | `AtomicNode`/`AsyncAtomicNode` 双基类合并为单结构体，无 async 入口 | 双基类逐字相同，源于 Python asyncio 限制；Go 无此分裂；与"Python 冗余执行去重"既往决策同精神 | 上游合并两基类则同步 |
| D2 | 第二道 `isinstance(session, BaseSession)` 检查注销，收敛为 `session.Session` 接口参数 | Go 编译期类型保证 ≙ Python 运行时 fail-fast 意图 | 无（永不回收） |
| D3 | `_atomic_invoke` 抽象方法 → 构造期注入回调函数字段（`NewAtomicNode` nil panic 对齐抽象方法必须实现的约束） | Go 跨包无法要求嵌入方实现非导出接口方法 | 无（语言层差异） |

## 7. 测试与验收

- `atomic_node_test.go`：
  - 三道校验各一例（nil session / state 非 WorkflowState）+ reason 文案锁定
  - 成功路径：回调返回值透传 + fake `WorkflowState` 断言 `CommitCmp` 确被调用且在回调之后
  - 回调返回 error：不上抛包装、不 commit
  - 校验顺序锁定用例（session 为 nil 时不触碰 State()）
  - `NewAtomicNode(nil)` panic
  - 覆盖率 ≥85%
- `components` 枚举：四成员遍历断言 `Name()`/`Desc()` 与 Python 元组全量一致。
- 本地门槛（规则 6/11）：`go build ./...` + `go vet ./...` + 全量 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL，推送即交付，不追 CI。
- IMPLEMENTATION_PLAN：8.10 打 ✅（产出描述 + 偏差归档指向本 spec §6）；8.11 ⤴️ 注销；追加"十次记录"实施序注（8.10 → 8.12 → 8.11 → 8.1/8.2 → 8.13-8.26）。

## 8. 回填与悬挂点盘点（不丢失确认）

- 全仓搜索确认：无任何 ⤵️/⤴️ 指向 8.10 本身，8.10 为干净章节。
- 本批注销：8.11 行 ⤴️ ComponentAbility 前置件（本批兑现）。
- 保留悬挂（后续章节处理，不在本批）：
  - 8.11 Vertex 还依赖 `stream_actor/base.py` 的 `StreamConsumer`（8.12 范围）
  - 8.12 ⤴️ 回填 session 的 `ActorManager any` 占位
  - 8.8 spec D1（GraphState reducer 注解）回收条件挂在 8.11 Vertex 真实消费时
