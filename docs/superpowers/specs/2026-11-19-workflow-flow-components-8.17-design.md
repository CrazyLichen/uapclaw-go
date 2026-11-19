# 8.17 StartComp / EndComp 设计文档（含组件基类体系 + CallbackFramework 装饰器扩展）

- 日期：2026-11-19
- 对应计划节：8.17（StartComp / EndComp，`openjiuwen/core/workflow/components/`）
- 上游章节：8.10 AtomicNode ✅ / 8.11 Vertex ✅ / 8.12 StreamActor ✅ / 8.15 WorkflowConfig ✅ / 8.16 WorkflowCard ✅ / 8.2 Executable ✅ / 5.5 SessionNode ✅
- 下游消费方：8.14 Workflow 类（add_node 编排）、8.18-8.26 业务组件（继承同一基类）

## 1. 范围（方案 A，用户 2026-11-19 拍板）

一次完整收口以下四件，不留悬空基类：

1. **组件基类体系**（`components/component.py` 306 行 → Go `components/component.go`）
2. **Start 组件**（`flow/start_comp.py` 14 行 → Go `components/flow/start_comp.go`）
3. **End 组件全家**（`flow/end_comp.py` 486 行 → Go `components/flow/end_comp.go`：EndConfig / End / TemplateProcessor / TemplateBatchProcessor / TemplateUtils）
4. **CallbackFramework 装饰器扩展**（选项 2 拍板：随本节扩展 runner/callback 支持 `emit_before` / `emit_after` / `transform_io` 三装饰器 + `WorkflowEvents` 组件事件族 4 常量）

理由：End 是四钩子模式唯一的全消费者（INVOKE/STREAM/COLLECT/TRANSFORM 四条路径全用到），基类与首个真实消费同批落地可当场验证接口形状；拆两步会重蹈 8.11 D-3「悬空接口」覆辙。

## 2. 拍板记录（2026-11-19）

| # | 事项 | 拍板 |
|---|------|------|
| 1 | 实施范围 | 方案 A：基类 + Session 包装（复用 NodeSessionFacade，见 §5.1）+ Start + End 一次收口 |
| 2 | 回调装饰器 | 选项 2：随本节扩展 CallbackFramework 装饰器能力，一比一复刻 |
| 3 | SubWorkflowComponent（计划表缺失条目） | 挂 8.14 之后新条目 **8.14b**（构造参数即 Workflow 实例、`_workflow.py:109` 消费 graph_invoker，强依赖 8.14） |
| 4 | 推进顺序 | 8.17 → 8.14 → 8.18-8.26，8.13 Graph Visualization 继续压后 |

## 3. Python 事实基线

| Python 文件 | 行数 | 符号 | Go 目标 |
|---|---|---|---|
| `components/component.py` | 306 | `_apply_callback_framework_to_executable`（模块级函数）/ `ComponentExecutable`（on_invoke/on_stream/on_collect/on_transform/trace_metadata）/ `ComponentComposable`（add_component/to_executable）/ `WorkflowComponent` | `internal/agentcore/workflow/components/component.go` |
| `components/base.py` | 57 | ComponentAbility（✅ 8.10 已有）/ WorkflowComponentMetadata / ComponentConfig / ComponentState | `base.go` 补齐后三符号 |
| `components/flow/start_comp.py` | 14 | `Start`（invoke 原样透传） | `components/flow/start_comp.go` |
| `components/flow/end_comp.py` | 486 | `EndConfig` / `End`（set_mix/invoke/stream/transform/collect/_render）/ `TemplateProcessor`（分段状态机）/ `TemplateBatchProcessor` / `TemplateUtils` | `components/flow/end_comp.go` |
| `runner/callback/decorator.py` | — | `emit_before` / `emit_after` / `transform_io` / `wrap`（本节只做前三者，wrap 无组件消费方） | runner/callback 包扩展 |
| `runner/callback/events.py` | 118-145 | `WorkflowEvents.COMPONENT_BATCH_INPUT/BATCH_OUTPUT/STREAM_INPUT/STREAM_OUTPUT` | callback/events.go 补 4 常量 |

组件协议方法分布（一比一归属纪律）：
- `trace_metadata()` 归 ComponentExecutable（component.py:124）
- `graph_invoker()` / `component_type()` / `skip_trace()` / `post_commit()` 归 **graph 层 Executable 协议**（`graph/executable.py:47-64`）——✅ 8.2 Go `Executable` 接口是否已含这四个方法需实施时核对，缺则属 8.2 回填而非新设计

## 4. Go 资产盘点（复用，不重造）

| 资产 | 位置 | 状态 |
|---|---|---|
| `Executable[I,O]` 四钩子接口 + `BaseExecutable` + `StreamChunk` | `internal/agentcore/graph/executable.go`（8.2） | ✅ |
| `NodeSessionFacade`（= Python `session/node.py Session` 包装：streamMode/Interact/GetEnv/`[wf_id=,comp_id=]` 描述串） | `internal/agentcore/session/node.go`（5.5） | ✅ **本节不新建 Session 包装类型**，组件层直接依赖它；实施时逐方法对照 Python Session（101 行）核对缺失项补齐 |
| `ComponentAbility` 枚举 | `components/base.go`（8.10） | ✅ |
| `EndCompTemplateRenderPositionTimeoutKey` / `EndCompTemplateBranchRenderTimeoutKey` | `session/constants/constants.go:41-43` | ✅（注意：Python 键名 `END_COMP_TEMPLATE_BATCH_READER_TIMEOUT_KEY` 对应 Go 哪个键实施时核对，可能需补第三个键） |
| config 默认值 5（session/config/base.py:158-159） | Go session/config | ⚠️ 实施时核对补齐 |
| `StatusComponentEndParamInvalid`(100010) | `common/exception/codes_workflow.go:74` | ✅ |
| `GetValueByNestedPath` | `session/utils/path.go`（8.12） | ✅ |
| `ExtractLeafNodes` / `FormatPath` | `common/utils/dict.go:77,252` | ✅ |
| `stream.OutputSchema`（8.7 D2）/ `constants.EndNodeStream`（8.11） | session/stream、session/constants | ✅ |
| Vertex 四策略分派表（INVOKE/STREAM/COLLECT/TRANSFORM） | `graph/vertex.go`（8.11） | ✅ 消费侧已通，本节只供组件 |
| CallbackFramework 最小子集（On/Off/Trigger/TriggerContext） | `internal/agentcore/runner/callback/` | ✅ 本节扩展（§5.3）：**核对结论（2026-11-19 二轮）**——①4 个组件事件常量中 3 个已存在（`ComponentBatchInput/ComponentBatchOutput/ComponentStreamInput`，events.go:457-463），仅缺 `ComponentStreamOutput`；②`TransformLLMIOInput/Output` + `RegisterAgentTransformIO` 先例已在（framework.go:649-668、670+），TransformWorkflowIO 族按同款迁移；③方法体内 Trigger 序列先例已在（memory/ltm 的 emit_before 模式）；④env 两键 + BuiltinDefaults 默认值 5 均已存在（constants.go:41-43、136-137），D-17-6 关闭 |

## 5. 设计

### 5.1 组件基类（component.go）

**分层语义**（对齐讨论结论）：

- `Executable[I,O]`（graph 层，8.2 已有）是引擎面协议；`ComponentExecutable`（workflow 层，本节）是组件作者面适配器，中间隔一层签名转换。
- Python 双 mixin 结构映射：`ComponentExecutable` 承担运行时面（四钩子适配 + NodeSession 校验 + Session 包装），`ComponentComposable` 承担构图面（add_component/to_executable），`WorkflowComponent` 组合两者。

Go 布局（单文件 component.go，一比一 Python 单文件纪律）：

```go
// ComponentExecutable 组件运行时基类（嵌入 BaseExecutable[I,O] 复用默认四钩子报错语义）
type ComponentExecutable[I, O any] struct {
    graph.BaseExecutable[I, O]
}

// OnInvoke 对齐 component.py:95-99：校验 session 必须是 NodeSessionFacade，
// 非法则 StatusWorkflowInnerOrchestrationError；合法则包一层组件级会话后转调 invoke
// OnStream/OnCollect/OnTransform 同理（collect/transform 传 streamMode=true）
// traceMetadata 对齐 component.py:124-135，默认 nil
```

组件级会话（Python `Session(session, stream_mode)`）：**复用 `NodeSessionFacade`**——它就是 5.5 落地的同名等价物（构造参数 streamMode、Interact 流式拒绝语义、GetEnv、`[wf_id=,comp_id=]` 描述串逐字对齐）。Python 四钩子内部包的是 `Session(NodeSession, False/True)`，Go 对应为 `NewNodeSessionFacade(inner, false/true)`。

抽象方法形状（子类覆写，Go 用接口约定）：

```go
// WorkflowComponent[I, O any] 组件作者面（Python WorkflowComponent 组合类）
// invoke/stream/collect/transform 四方法签名与 Executable 四钩子同形，
// 但 session 形参为 *session.NodeSessionFacade（组件级包装）而非 BaseSession
```

Go 泛型约束下的具体形状（**偏差 D-17-1 预登记**）：Python 用 `Input/Output` TypeVar 的非泛型鸭子协议，Go 需定 `WorkflowComponent[I, O any]` 具体类 + 四个可覆写方法槽（函数字段或接口嵌入），实施时按 8.2 `BaseExecutable` 既有惯例选择（倾向函数字段槽，与 AtomicNode 回调字段风格一致），最终形状在 spec 定稿前以最小示例编译验证后写入。

### 5.2 Start / End（flow/）

文件归属：Go 新建 `internal/agentcore/workflow/components/flow/` 子目录，`start_comp.go` + `end_comp.go` 与 Python `flow/` 一比一。`__init__.py`（5 行空导出）→ Go 无对应物，不建。

**Start**（start_comp.py:10-15）：仅覆写 invoke 原样返回 inputs。stream/collect/transform 继承基类默认 NotImplementedError 语义（BaseExecutable 默认实现）。

**End**（end_comp.go，486 行核心）：

- `EndConfig`：pydantic BaseModel → Go 结构体 + `NewEndConfig`；`response_template`（json tag `responseTemplate`，min_length=1 校验）；构造校验失败 → `StatusComponentEndParamInvalid`（reason 拼接对齐 `f"conf is invalid, {str(e)}"`）
- `End` 构造（end_comp.py:39-51）：conf 非 nil → 验证 + 建 TemplateProcessor；nil → conf/template 双 nil；`_batch_template`/`_mix` 零值
- `SetMix`（:53-57）：mix 置真 + template 非 nil 时 `set_data_source_count(2)` + `reset()`
- `Invoke`（:59-89）：有模板 → nil inputs 补 `{}` → `_render`（batch reader 超时键取 env）；无模板 → dict 过滤 None 值 → `{"output": ...}` 信封（output 为 nil 时结果 nil）；前后 debug 日志全字段对齐（has_template/mix metadata）
- `Stream`（:91-142）：有模板 → `render_stream` 逐帧包 `OutputSchema{Type=EndNodeStream, Index=frame.index, Payload={response: frame.data}}`；无模板 → dict 逐 key 吐 `{"output": {k:v}}`、非 dict 单帧；frame_count 计数 + 成功/失败日志
- `Transform`（:144-179）/ `Collect`（:181-212）：无模板路径走 `ExtractLeafNodes`，叶子为流（Go `<-chan` 或迭代器形态，偏差 D-17-3 见 §6）则逐帧吐 `{FormatPath(path): frame}`；Collect 聚成 `{"output": chunks}`
- `_render`（:214-245）：mix 模式条件等待 + 超时降级（未渲染过→同步渲染兜底返回 `{"response": answer}`；已渲染→返回 nil）；Go 并发原语映射见 §6 D-17-2

**TemplateProcessor**（:250-444）分段状态机：

- `render_template_to_list`：`re.split(r'(\{\{[^{}]+\}\})'` + filter None → Go regexp 同款（RE2 语法兼容此模式，8.12 RE2 并集先例）
- `{{var}}` 段剥壳存 `_variables_positions`（set[int]→map[int]struct{}）
- `_need_render` / `should_render` / `advance_position` / `is_finished` / `reset` / `current_position` / `get_current_segment` 逐方法一比一
- `render`：`TemplateUtils.render_template` → `string.Template("$...").safe_substitute` 语义（`{{`→`$`、`}}`删除）——Go 手写 safe_substitute（缺键保留原样），偏差 D-17-4
- `render_stream`（:356-442）：锁 + 条件变量 + 位置推进主循环；None 值三态（mix 等待 / 单源跳过 debug 日志 / has_any_value 等待）；AsyncGenerator 叶子逐帧转发；TimeoutError → warning + advance；CancelledError → break；finally 消费残留迭代器 + `_count==_data_source_count` 时 reset
- `_consume_all_iterators`（:310-354）：递归收集 dict/list 中所有迭代器并耗尽，异常仅 debug 日志（路径字段）

**TemplateBatchProcessor**（:447-467）：condition + is_rendered + render（inputs 合并 `self._inputs | inputs` → Go map 逐键合并）。

### 5.3 CallbackFramework 装饰器扩展（runner/callback）

Python 机制（component.py:22-73 + decorator.py）：`__init_subclass__` 钩子对每个子类检查四个 on_* 方法是否在自身 `__dict__`（即子类真的覆写了），是则用框架装饰器包装替换。装饰器语义：

- `emit_before(event)(fn)`：调用前 Trigger 事件
- `emit_after(event)(fn)`：调用后 Trigger 事件
- `transform_io(input_event, output_event)(fn)`：触发事件并把**最后一个回调的返回值**替换为实际入参/出参（无回调注册时为 noop 哨兵）

Go 落地（选项 2 一比一；**核对修订（2026-11-19 二轮）**：大部分地基已存在，实际增量极小）：

1. **事件常量**：仅补 `ComponentStreamOutput WorkflowEventType = "_framework:component_stream_output"`（其余 3 个已存在）
2. **transform_io 等价**：按 `TransformLLMIOInput/Output`（framework.go:649-668）+ `RegisterAgentTransformIO`（670+）先例，新增 `TransformWorkflowIOInput/Output` 方法 + `RegisterWorkflowTransformIO(inputEvent, outputEvent WorkflowEventType, inputFn, outputFn TransformWorkflowIOFunc)`——无注册回调时原样返回（noop）
3. **emit_before/emit_after 等价**：按 memory/ltm 先例，在 `ComponentExecutable` 四钩子方法体内 Trigger 序列（前 Trigger → TransformIOInput → 原逻辑 → TransformIOOutput → 后 Trigger）
4. `__init_subclass__` 的 Go 等价：Go 无类继承钩子。**偏差 D-17-5**：改为在 `ComponentExecutable` 四钩子方法体内显式执行上述 Trigger 序列（与 2.14 Model 门面「前后触发回调」先例一致），语义等价（回调仍触发、transform 改写生效）。回收条件：CallbackFramework 未来引入方法级包装机制时迁移。

### 5.4 base.go 补齐（Python base.py 后三符号）

`WorkflowComponentMetadata{NodeID,NodeType,NodeName}` / `ComponentConfig{Metadata *WorkflowComponentMetadata}` / `ComponentState{CompID string; Status 枚举}`——dataclass → Go 结构体，一比一。

## 6. 已知偏差（初稿，定稿时编号复核）

| 编号 | 偏差 | 理由 | 回收条件 |
|---|---|---|---|
| D-17-1 | Python 非泛型鸭子协议四钩子 → Go `WorkflowComponent[I,O any]` 具体泛型类 + 方法槽 | Go 无 TypeVar 鸭子类型 | 语言能力缺口，长期保留 |
| D-17-2 | `asyncio.Lock`/`asyncio.Condition` + `anyio.fail_after` → Go `sync.Mutex`/`sync.Cond` + `context.WithTimeout`（或 channel 门铃） | 语言原语映射；asyncio.Condition 支持超时等待，sync.Cond 不支持，需 deadline 轮询或 channel 广播替代——实施时选型并记录选型理由 | 长期保留 |
| D-17-3 | Python 叶子值 `AsyncGenerator/Generator` isinstance 双态 → Go 流叶形态为 **`*stream_actor.StreamReader`**（引擎 preStream 真实递出物，Next 拉取式，END_ 哨兵/超时终止；`_consume_all_iterators` 收尾耗尽依赖 StreamReader 消费 EndFrame 后置持久关闭态的幂等性，base.go Next 2026-11-19 修正）。**修正记录（2026-11-19 实施后审查）**：早期登记"统一 `<-chan any`"与引擎真实形态不符（单测假形态漏接集成缝），审查会话发现后已修正：renderStreamLoop/consumeAllIterators 补 StreamReader 分支 + 测试改真实 StreamReader 桩；`<-chan any` 分支保留仅为测试形态兼容 | 8.2 定的流形态与 8.11 引擎递出物不一致，单测假桩未暴露 | 引擎流形态重构时同步 |
| D-17-4 | `string.Template.safe_substitute` → Go 手写等价（缺键保留 `$name` 原样） | Go 标准库无 Template | 长期保留 |
| D-17-5 | `__init_subclass__` 自动装饰 → ComponentExecutable 四钩子方法体内 Trigger 等价序 | Go 无类继承钩子；语义等价（回调仍触发、transform_io 改写生效需回调注册方配合） | CallbackFramework 引入方法级包装时迁移 |
| D-17-8 | **实施中新增（2026-11-19）**：①ComponentAbility 枚举本体沉 `components/ability` 叶包（stream_actor/workflow_config 经 components 间接引用在 components 引入 session/graph 后成环）；②Executable 契约（StreamChunk/BaseExecutable/Executable/GeneralExecutor）沉 `graph/executable` 叶包（components 需实现契约但引 graph 根与 graph→components 成环）；两处均以**类型别名**（Go 1.24+ 泛型别名）在 components/base.go、graph/executable.go 保持原锚点不变；stream_actor/manager、workflow_config 及 5 个测试文件改直引叶包 | Go 包环硬约束（Python 无此问题）；PendingNode 迁 pregel（8.3b D6）/AgentsBridge（17.4c1）破环先例同款 | 锚点别名长期保留；若上游包结构重组可回收 |
| D-17-6 || D-17-6 | ~~Python `session.get_env` 键名核对~~ **已关闭（2026-11-19 核对）**：Python `END_COMP_TEMPLATE_BATCH_READER_TIMEOUT_KEY` 的键值就是 `"_end_comp_template_branch_render_timeout"`（Python 键名与值本就不一致），Go `EndCompTemplateBranchRenderTimeoutKey` 逐字对应；两键 + BuiltinDefaults 默认值 5 全部已存在（constants.go:41-43、136-137） | — | — |

## 7. 测试与覆盖率

- 每文件配 `_test.go`，包覆盖率 ≥ 85%
- 重点用例：模板分段解析（静态/变量/嵌套混排）、render_stream 三态 None 处理、mix 双源条件等待与超时降级、`_consume_all_iterators` 递归耗尽、safe_substitute 缺键保留、End 四钩子 × 有无模板矩阵、Start 透传、装饰器 transform_io 有/无回调、NodeSession 类型校验拒绝分支
- 并发用例：render_stream 多 goroutine 竞争（-race）
- 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`

## 8. 计划表回填清单

1. 8.17 行 ☐→✅ 时附 Go 事实描述
2. **新增 8.14b 条目**（SubWorkflowComponent + SubWorkflowStreamState，`flow/workflow_comp.py` 95 行）
3. 8.10/8.11/8.15 各自的 ⤴️/⤵️ 钩子在本节被消费的，实施后逐条注销核对
4. 实施顺序注记追加「十五次记录」（8.17 → 8.14 → 8.14b → 8.18-8.26）

## 9. 删除清单

无被替代旧实现（components 包现仅 base.go 枚举，全部保留复用）；新增文件须同步 doc.go 文件树（components/doc.go 补 component.go/flow/ 条目，新建 flow/doc.go）。
