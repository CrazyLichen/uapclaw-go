# 8.14b SubWorkflowComponent 设计

> 日期：2026-11-21
> 状态：已批准（brainstorming 拍板见 §2）
> Python 参考：`openjiuwen/core/workflow/components/flow/workflow_comp.py`（96 行）
> 前置：8.14 Workflow 类（dcc06aab0）/ 8.15 / 8.16 / 8.17 均已完成

## 1. 定位与作用

SubWorkflowComponent 是**子工作流嵌套的唯一入口**：把一个完整 `Workflow` 实例包装成父图上的普通节点。

运行时链路：

- `_workflow.py:109`（ExecuteSingleComponent 第 7 步）读 `executable.graph_invoker()` → True 时给 Vertex 递 `InnerPregelConfig`（ns=component_id、parent_ns=component_id，形成命名空间嵌套）；
- `vertex.py:666` `call` 读 `graph_invoker()==True` → is_subgraph → 子图执行语义；
- `invoke/stream` 转调 `sub_workflow.invoke/stream(..., is_sub=True)` → Workflow 的 `_sub_invoke/_sub_stream` 分支 → `createWorkflowSession(is_sub=True)` → compile 子图执行；
- 由此实现 ns 嵌套、checkpointer 分层快照、中断跨层恢复（HITL）。

三层消费闭环（讨论中实证澄清）：

1. **流式下发**：stream 帧经 Vertex `_process_chunk` → StreamActor 下发给下游流式消费方；
2. **批式回拉**：Vertex postStream 末尾鸭子探测 `get_stream_output()`（vertex.py:624-628，Go vertex.go:1486-1491 已兑现）回拉合并缓存写 `SetOutputs`——流腿本不写 state，这一笔让批式消费方（End 批式模板 `${ref}`、Loop `get_outputs`、单组件直调结果读取）在"节点只流过"时也有批式视图；
3. **invoke 短路**：`cache_stream=True` 且缓存非空时 invoke 不重跑子工作流，直接回 `{'output': cached}`。

## 2. 拍板记录（2026-11-21 brainstorming）

| # | 决策 |
|---|---|
| 1 | 8.14 审查遗留 ⤵️8.14b 三项回填本批收口：元类装饰四事件对断言、callback 触发序列集成断言、12.4 方法对照表 |
| 2 | `build_final_result` 合并语义忠实复刻（str 拼接/数字求和/map 并集/异类型对齐 TypeError） |
| 3 | **不加锁**：happens-before 由「生产侧 append 先于 channel send，消费侧通道耗尽后才读」的通道序保证；invoke 腿与 stream 腿同 goroutine 串行 |
| 4 | **复刻 Quirk A（用户 2026-11-21 二次拍板：「对齐要按照上游真实实现来」，撤销此前修复决定）**：Python workflow_comp.py:81-84 空缓存返回 `{}`（非 None）触发短路 → cache_stream 开启时批入腿永不执行子工作流——上游真实行为逐字承载，含 build_final_result 异类型 TypeError 直接上抛（:81 调用点无捕获） |
| 5 | **复刻 Quirk B（已知行为）**：中断恢复后 invoke 返回中断前已累积帧的部分合并结果（不全）——机制是组件实例字段生存期跨单次调用，恢复时实例仍存活 |
| 6 | 实施序维持 8.14b → 8.18-8.26，无换序（依赖 8.14/8.15/8.16/8.17 全就绪，无缺失前置件） |

## 3. 结构忠实对照（规则 9）

Python 文件 → Go 文件一比一：`workflow_comp.py` → `internal/agentcore/workflow/components/flow/sub_workflow_comp.go`。

| Python 符号 | Go 对应 | 说明 |
|---|---|---|
| `SUB_WORKFLOW_COMPONENT = "sub_workflow"` | `SubWorkflowComponentType` 常量 | |
| `SubWorkflowStreamState.__init__` | `NewSubWorkflowStreamState` 或零值可用 | 字段非导出 |
| `.accumulate(output)` | `Accumulate(output any)` | |
| `.build_final_result()` | `BuildFinalResult() map[string]any` | 合并语义见 §4 |
| `.clear()` | `Clear()` | |
| `SubWorkflowComponent.__init__(sub_workflow, *, cache_stream=False)` | `NewSubWorkflowComponent(subWorkflow *workflow.Workflow, opts ...SubWorkflowOption)` | nil → `StatusComponentSubWorkflowParamInvalid(101150)` 文案复刻；`WithCacheStream(bool)` 选项 ≙ keyword-only |
| `.invoke(inputs, session, context)` | 构造期内 `InvokeFn` 槽赋值 | 包装序（NewComponentSession+Trigger+TransformIO）由基类 OnInvoke 承担；逻辑见 §5 |
| `.stream(inputs, session, context)` | 构造期内 `StreamFn` 槽赋值 | 同上；逐帧 Accumulate 见 §5 |
| `.get_stream_output()` | `GetStreamOutput() map[string]any` | cache_stream 开 → BuildFinalResult；关 → nil。Vertex 接口断言自动接通，零改动 |
| `.graph_invoker()` | 方法提升遮蔽：`GraphInvoker() bool { return true }` | 深度 1 遮蔽 BaseExecutable 默认 false |
| `.component_type()` | `ComponentType() string { return SubWorkflowComponentType }` | 同上 |
| `.sub_workflow` property | `SubWorkflow()` 访问器 | |
| （不覆写 collect/transform） | CollectFn/TransformFn 槽不赋值 | 走基类 NotImplementedError，对齐 |
| （不覆写 add_component） | 组合面继承合成体默认 AddComponent | 对齐 |

Go 侧多出：`SubWorkflowOption`/`WithCacheStream`（Python keyword-only 参数的 Go 惯用法承载）。

## 4. BuildFinalResult 合并语义（忠实复刻，决策 #2）

Python workflow_comp.py:41-63：

1. 无累积 → 返回 `{}`（注意：配合 Quirk A 修复，空 map 仅表示"无结果"，不再触发 invoke 短路——短路判定改为调用方检查非空）；
2. 逐帧：只认 dict 帧；嵌套取 `output.get('output', output)`；inner 为 dict 才合并；
3. 合并规则：key 已存在 → `merged[key] + value`。Python `+` 语义按运行时类型分派复刻：
   - string + string → 拼接
   - 数值 + 数值 → 求和（int/float 混合按 Python 数字提升）
   - dict/map + 任何类型（含 dict+dict）→ Python `+` 抛 TypeError，Go 复刻为返回 error（同 key 值只要有一侧非 str/数字即报错，不做 map 深合并）
   - 其余异类型 → 同上，对齐 Python TypeError 语义，返回 error

## 5. invoke / stream 逻辑

**invoke**（workflow_comp.py:78-84，含 Quirk A 修复）：

```
if cacheStream && 缓存非空:
    return {"output": cached}          // 短路，不重跑子工作流
inputs = 信封取 INPUTS_KEY
config = 信封取 CONFIG_KEY
return subWorkflow.Invoke(ctx, inputs, sess, context, kwargs{CONFIG_KEY: config, "is_sub": true})
```

**stream**（workflow_comp.py:76-81）：

```
ch = subWorkflow.Stream(ctx, inputs, sess, context, kwargs{CONFIG_KEY: config, "is_sub": true})
for frame := range ch:
    if cacheStream: state.Accumulate(frame)
    yield frame
```

`is_sub=true` 经 kwargs 传入 → Workflow.Invoke/Stream 的 is_sub 分派（8.14 已实现）→ `NewSubWorkflowSessionFacade`（8.14 已实现）→ 子图执行。

**并发边界（决策 #3 配套登记，不修）**：

- 定义复用（构图一次多次 Invoke）+ 并发调用：缓存跨运行混淆——与 Python 行为一致（asyncio 交错同样会脏），已知限制；
- 取消后僵尸生产者：ctx 协作式取消，理论窗口——Python asyncio cancel 同为 await 点生效，行为对齐。

## 6. 消费接通（零改动确认）

三处地基 8.14 已全部就绪，本节纯消费：

1. `base_workflow.go:773 isGraphInvoker`（`interface{ GraphInvoker() bool }` 探测）——SubWorkflowComponent 定义方法即接通；
2. `vertex.go:1487 GetStreamOutput`（`interface{ GetStreamOutput() map[string]any }` 断言）→ `SetOutputs`；
3. Workflow.Invoke/Stream `is_sub` kwargs 分派 + `NewSubWorkflowSessionFacade`。

## 7. 偏差与已知行为登记

| 编号 | 性质 | 内容 | 回收条件 |
|---|---|---|---|
| D-14b-1 | 已知行为（复刻，用户 2026-11-21 拍板撤销修复） | Quirk A：空缓存 `{}` 短路 → cache_stream 开启时批入腿不执行子工作流（workflow_comp.py:81-84 逐字承载；异类型 TypeError 上抛） | 上游修复后跟齐 |
| D-14b-2 | 已知行为（复刻） | Quirk B：中断恢复返回部分合并缓存（workflow_comp.py:41-48 + 组件实例字段跨调用生存期） | 不回收（对齐上游） |
| D-14b-3 | 已知限制（复刻） | 定义复用 + 并发 Invoke 缓存混淆；取消僵尸生产者理论窗口 | 不回收（与 Python 一致） |
| D-14b-4 | 结构承载 | Python keyword-only 参数 → Go 变参选项；方法遮蔽三钩子（GraphInvoker/ComponentType）+ 两槽赋值（invoke/stream） | 终态 |
| D-14b-5 | 已知行为（复刻，Python 同为死路径） | 嵌套流式 + 流式引用子图帧内部字段（`${sub.response}`）：Python payload 为 OutputSchema 对象、pydantic 无 `__getitem__` → 路径提取 None → 同样断链挂死；上游无嵌套流式测试故从未暴露。Go 以 dict 载荷形态走通（见 D-14b-6） | 不回收（Go 比 Python 健壮，登记防误判为回归） |
| D-14b-6 | 有意偏差（Go 槽承载约束） | 转发/缓存形态分离：转发取 Schema 载荷 dict（对齐官方 StreamCompNode 产出形态，流边路径提取可用）；缓存累积原始 Schema 帧（isinstance-dict 过滤语义对齐 Python，缓存保持空 → Quirk A 短路行为一致）。Python 两腿同为 chunk 对象；另 END_FRAME 恢复入队哨兵（撤 8.14 D-14-3 close 等价，close 会 forceClear 丢帧）+ AsyncStreamQueue closed 先排空缓冲 + 流式槽帧元素 any 化（D-8 延伸：Executable/ExecutableGraph/组件槽 StreamChunk[O]→StreamChunk[any]，2026-11-21 用户拍板「返回类型不能固定死」） | D-17-1 信封在单层 end 出口的承载保留（8.17 已验收形态）；若后续撤封按本节五层断裂链逐出口核对 |

## §12 方法对照表（12.4 回填③，实施后实测行号）

| Python workflow_comp.py | Go sub_workflow_comp.go | 一致性 |
|---|---|---|
| SUB_WORKFLOW_COMPONENT (:19) | SubWorkflowComponentType (:59) | ✅ |
| SubWorkflowStreamState（状态字段 :23-25） | SubWorkflowStreamState 结构体 (:25) | ✅ |
| .accumulate (:27-29) | Accumulate (:244) | ✅（累积原始帧，isinstance-dict 过滤对齐） |
| .build_final_result (:31-60) | BuildFinalResult (:251) | ✅（合并四态 + 异类型 TypeError 上抛） |
| .clear (:62-65) | Clear (:285) | ✅ |
| SubWorkflowComponent.__init__ (:66-75) | NewSubWorkflowComponent (:65) | ✅（D-14b-1 已知行为登记） |
| .invoke (:78-84) | InvokeFn 槽 (:142) + invokeSubWorkflow (:213) | ✅（短路/转调双态；CONFIG_KEY 透传修复） |
| .stream (:76-81) | StreamFn 槽 (:148) | ✅（累积原始帧 + 转发载荷 dict，D-14b-6） |
| .get_stream_output (:83-85) | GetStreamOutput (:105) | ✅ |
| .graph_invoker (:87-88) | GraphInvoker (:76) | ✅ |
| .component_type (:90-91) | ComponentType (:98) | ✅ |
| .sub_workflow property (:93-94) | SubWorkflow 访问器 (:101) | ✅ |
| （上游无）ToExecutable/AddComponent override | (:82-95 一带) | Go 静态解析必需（Python self 动态分派等价物），偏差 D-14b-4 延伸 |

实施期超额修复（Python 源码实证的四项移植缺陷 + 嵌套流式三层数据断裂，均已提交）：
①createParentID 对 *SubWorkflowSession 断言失效 ②subInvoke/subStream CONFIG_KEY 透传缺失 ③DefaultGraphInvoke 信封 config 双形态 ④AsyncStreamQueue closed 丢缓冲帧 + END_FRAME 入队哨兵恢复（撤 D-14-3）⑤end-sub 分支槽信封剥离 ⑥流式槽帧元素 any 化（D-8 延伸，用户拍板）。

## 8. 测试要求（用户明确要求：讨论过的点全部有覆盖，中断恢复场景必须真实输入输出）

### 8.1 单测（sub_workflow_comp_test.go）

1. **状态机合并矩阵**：空累积 → `{}`；单 dict 帧；嵌套 `{'output': {...}}` 展开；同 key string 拼接；同 key 数字求和（int/int、float/float、int/float）；异类型 → error（对齐 TypeError）；非 dict 帧跳过；Clear 后重置；
2. **构造守卫**：nil subWorkflow → 101150 错误码；WithCacheStream 开/关；
3. **三 override**：GraphInvoker()==true、ComponentType()=="sub_workflow"、SubWorkflow() 返回注入实例；
4. **invoke 短路两形态**（Quirk A 修复锁定）：cache_stream 开 + 缓存非空 → 不调 subWorkflow（fake Workflow 断言零调用）返回 `{"output": cached}`；cache_stream 开 + 缓存空 → **真实转调** subWorkflow.Invoke 且 kwargs 含 `is_sub=true`/CONFIG_KEY 透传（D-14b-1 用例）；cache_stream 关 → 恒转调；
5. **stream 累积**：cache_stream 开逐帧 Accumulate 且帧原样转发；关则不累积；
6. **get_stream_output 两态**：开 → 合并结果；关 → nil；
7. **回填①元类装饰四事件对断言**：本组件经基类 OnInvoke/OnStream 时 ComponentBatchInput/ComponentBatchOutput（及 stream 轨 ComponentStreamOutput 逐帧）事件成对触发（Trigger 序 = 8.17 D-17-5 装饰序等价）。

### 8.2 集成（tests/integration/agentcore/workflow_sub_workflow，真实链路、真实输入输出）

1. **两层嵌套批式**：父 Workflow（Start→sub→End）invoke，真实输入 map 贯穿：断言子工作流收到经信封 INPUTS_KEY 转发的输入、父 End 输出含子工作流结果、ns 嵌套生效（InnerPregelConfig ns=component_id）；
2. **两层嵌套流式**：父 stream，断言子工作流帧经两层转发到达父侧输出（帧序列逐帧断言）；
3. **cache_stream 短路**：先流式跑完（缓存积累）→ 再批式 invoke 同组件 → 断言子工作流未重跑（fake 计数）且返回合并缓存；
4. **中断恢复（重点，真实输入输出）**：
   - 父 Workflow 嵌子工作流，子工作流内构造中断点（InteractiveInput 形态）；
   - 第一次运行：真实输入进入 → 中断发生 → 断言中断返回值（TASK_STATUS_INTERRUPT 形态）与中断前已下发的帧内容；
   - resume（InteractiveInput 真实提交）：断言恢复后 invoke 腿读到中断前部分缓存（Quirk B 复刻锁定，D-14b-2 用例）、最终输出完整、ns/快照现场经 checkpointer 重建正确；
5. **回填②callback 触发序列集成断言**：真实链路中断言 ComponentBatchInput → （流轨逐帧 ComponentStreamOutput）→ ComponentBatchOutput 的触发顺序；
6. 审计要求：`//go:build integration` + `suite.Run(`，真实跨 ≥3 子系统（workflow/graph/session），过 `go run ./cmd/integration-audit -verify` 两层门禁。

## 9. 删除清单（规则 8）

本节为新增组件，无被替代旧实现；回填三项完成后删除 8.14 spec/计划表中对应 ⤵️8.14b 钩子标注与 IMPLEMENTATION_PLAN.md 8.14 行的三处回填标记（改为 ✅ 已回填）。

## 10. 验收口径

- `go build ./...` / `go vet ./...` 零错误；
- `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全量回归零 FAIL；
- workflow 包与 components/flow 包覆盖率 ≥85%；
- 集成审计两层门禁通过；
- 推送 main（规则 6/11：不追踪远端 CI）。
