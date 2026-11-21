# 8.19 BranchComp / BranchRouter 设计

> 日期：2026-11-21
> 状态：已批准（brainstorming 三拍板，见 §3）
> Python 真相源：`openjiuwen/core/workflow/components/flow/branch_comp.py`（53 行）、
> `flow/branch_router.py`（112 行）、`condition/condition.py`（52 行）、`condition/expression.py`（566 行）

## 1. 流程位置与作用

BranchComp 是工作流的**排他网关**组件：

- 构图期：`add_branch` 收集分支（条件 + 目标）；`add_component` 把自身注册为普通节点 + 条件边，
  并在目标数 >1 时调用 `graph.register_branch_targets`（8.1 CNF 屏障解析的编译期输入）。
- 运行期链路：`Pregel 引擎 submit → NodeTask.run ①节点体（invoke 只做 set_session，返回 {}，
  返回值丢弃 task.py:152）→ ②节点 routers 循环 dispatch（task.py:157-160）→ ConditionalRouter.selector
  → BranchRouter.__call__ → Branch.evaluate → Condition.__call__ → atomic_invoke（8.10）`。
- 分支评估发生在**条件边 dispatch 阶段**，同步直调，不经过节点执行链的四个 On* 钩子；
  组件 `skip_trace()=True`。
- session 注入两条路径并存：①节点体 `invoke` 内 `set_session`（运行期）+
  `_workflow.py:283` 构图期 `add_conditional_edges` 的 isinstance(BranchRouter) 特判内 `set_session`。
- 8.10 AtomicNode 落地后的**首个真实串联消费者**（8.10 spec D1 回收条件）；
  `NumberCondition`/`ArrayCondition`（number.py/array.py）不在本节范围，留 8.20。

## 2. 范围与包结构（规则 9.1 一比一）

| Python | Go |
|---|---|
| `components/flow/branch_comp.py` | `internal/openuapclaw/core/workflow/components/flow/branch_comp.go` |
| `components/flow/branch_router.py` | `internal/openuapclaw/core/workflow/components/flow/branch_router.go` |
| `components/condition/condition.py` | `internal/openuapclaw/core/workflow/components/condition/condition.go`（新包） |
| `components/condition/expression.py` | `internal/openuapclaw/core/workflow/components/condition/expression.go` |
| `condition/number.py` / `array.py` | ☐ 不在本节（拍板 A：留 8.20 与 LoopComp 一起） |

新包 `condition` 含 `doc.go`（规则 1）。三个包均无需动 graph 包。

## 3. 拍板记录

1. **拍板 A（范围切分）**：方案 A——8.19 只做 branch_comp/branch_router/condition.py/expression.py；
   number.py/array.py 留 8.20。
2. **拍板 ①（求值器）**：方案 ①——手写词法 + 递归下降 parser + evaluator，DSL 语义一比一，
   节点对照表进本 spec；不引 expr-lang/expr、cel-go 等第三方表达式库。
   定位澄清：该表达式是**自包含小 DSL**，与宿主实现语言无关；`ast.parse` 只是 Python 侧实现载体，
   Go 复刻的是 DSL 可观测语义（同表达式+同变量值→同结果），对齐审计看语义等价+步骤结构。
   表达式全程以 string 传递（Card/配置 → add_branch → Branch → Condition），
   运行期每次评估时解析；构造期仅做变量名 findall 与长度护栏。
3. **拍板 ②（drawable 缝）**：方案 ②——branch_router 构造函数的 `WORKFLOW_DRAWABLE` env 开关、
   `DrawableBranchRouter`、`add_branch` 内 drawable 填充三处整段跳过，偏差 D-19-1 登记，
   回收条件挂 8.13；**须在 8.13 的计划文档/IMPLEMENTATION_PLAN 注记中标注
   「⤴️ 8.19 跳过 drawable 缝，待 8.13 回填」**。

## 4. 类型与方法映射

### 4.1 condition 包（condition.go）

Python `Condition(AtomicNode)` 抽象基类 → Go 接口 + 嵌入 8.10 AtomicNode 校验链：

- `Condition` 接口：
  - `Call(session) bool`（≙ `__call__` → `atomic_invoke(session=session)`，
    内部走 8.10 `AtomicInvoke` 四步：取 session → 校验（session 非空 + state 为 WorkflowCommitState）
    → 调 `Invoke` → `WorkflowState.CommitCmp` → 返回。**8.10 D1 回收在此兑现。**）
  - `Invoke(inputs, session)`（子类实现，Python 抽象方法）
  - `TraceInfo(session) string`（Python 返回 str 或 dict，Go 侧 string/结构体于 §4.2 定）
- `FuncCondition`：构造收 `func() bool`；`Invoke` 直调；`TraceInfo` 返回函数名（Python `__name__`，
  Go 用 `runtime.FuncForPC`，与 8.1 D-3 getCallableName 惯例一致）。
- `AlwaysTrue`：`Invoke` 恒 `true`；`TraceInfo` 返回 `"True"`。
- `ExpressionCondition`（§4.2）。

注意：Python `Condition._atomic_invoke` 里有元组分支（`isinstance(result, tuple)` → set_outputs），
该分支的实际生产者是 array.py 的 ArrayCondition（不在本节），Go 侧接口仅保留 bool 返回，
元组分支随 8.20 number/array 一并落（spec §8 缺失项登记，规则 13.2）。

### 4.2 expression.go 求值器

**预处理链（一比一）**：
1. 构造：`NewExpressionCondition(expr string)`——存字段、`re.findall(r"\$\{[^}]*\}")` 抽变量名列表、
   长度 > `MAX_EXPRESSION_LENGTH` 即 build_error(EXPRESSION_EVAL_ERROR)。
2. `_get_inputs(session)`：空表达式或 session nil 返回 `{}`；否则对每个 match 取
   `session.state().get_global(match[2:-1])`。
3. `convertCondition`：字符串字面量保护（`"(?:[^"\\]|\\.)*"` / `'...'` 含转义，占位符
   `__STRING_LITERAL_{i}__`）→ 8 条 RULES 逐条复刻（`&&→and`、`\|\|→or`、`\btrue\b→True`、
   `\bfalse\b→False`（**词边界保留**，防误伤 `is_false_alarm` 类标识符）、`length\(→len(`、
   `not_in→not in`、`is_empty\(→_safe_is_empty(`、`is_not_empty\(→_safe_is_not_empty(`）→ 还原字面量。
4. 变量改名：`\$\{([^{}]*)\}` → `var_{i}`（按长度降序替换）。
5. runtime 环境：`len=_safeLen / bool / not / and / or / in(contains) / sum / inputs /
   _safe_is_empty / _safe_is_not_empty / is_empty / is_not_empty`。

**parser（手写词法 + 递归下降）**——Python AST 节点 ↔ Go 节点对照表：

| Python ast | Go node | 语义 |
|---|---|---|
| `BoolOp(And/Or)` | boolOpNode | all/any（Python truthiness，短路） |
| `Compare`（多链） | compareNode | 逐对比较，任一 False 即 False |
| `BinOp`（Add/Sub/Mult/Div/Mod/Pow/FloorDiv/LShift/RShift/BitOr/BitXor/BitAnd） | binOpNode | Mult/集合尺寸护栏、Pow 指数>100 封禁 |
| `UnaryOp`（USub/UAdd/Not/Invert） | unaryOpNode | |
| `Name` | nameNode | 四级解析：runtime→inputs→int→float→True/False/None→EXPRESSION_EVAL_ERROR "name 'x' is not defined" |
| `Constant` | constantNode | |
| `Subscript`（Index / Slice 含 lower/upper/step） | subscriptNode | 集合尺寸护栏、切片跨度护栏 |
| `Attribute` | attributeNode | dunder 封禁 + 敏感属性表（`__class__` 等 5 个）+ getattr→dict 回落 |
| `Call` | callNode | func 可调用性校验 |
| `List` / `Tuple` / `Dict` | listNode/tupleNode/dictNode | 元素数护栏 |
| `Expression`（mode='eval' 包裹） | 隐式（parser 根节点） | |

安全护栏逐条复刻：`MAX_COLLECTION_SIZE` / `MAX_AST_DEPTH`（parser 深度即对应，
`_check_ast_depth` 的逐节点类型递归在 Go parser 的构造深度上等价承载）/ `MAX_EXPRESSION_LENGTH` /
Pow 指数>100 / 集合乘法尺寸 / 切片跨度 / dunder 与敏感属性封禁 / `_safe_len` 对 None 返 0、
非序列报错 / `_safe_is_empty` 对 int/float/bool 报错。

**求值语义关键点（对齐审计锚点）**：
- `in` / `not in` 遇 TypeError 返 False（不抛）。
- BoolOp 是 `all([逐个求值])`（Python `any`/`all` 短路语义由逐个求值承载——注意 Python 源码
  `_evaluate_bool_op` 是**先全量求值再 all/any**，Go 逐字复刻该行为，不做短路优化）。
- 错误分类：SyntaxError→EXPRESSION_SYNTAX_ERROR；NameError/其他→EXPRESSION_EVAL_ERROR；
  BaseError 原样上抛。
- trace_info：返回 `{"bool_expression": expr, "inputs": {...}}`（dict 形态，Go 侧以结构体或
  map 承载，实施时与 BranchRouter 的 trace inputs 段统一）。

**常量核对**：实施时核对 Go 侧 `MAX_EXPRESSION_LENGTH`/`MAX_COLLECTION_SIZE`/`MAX_AST_DEPTH`
与 Python `common/constants/constant.py` 取值一致，缺则同批补注册；
错误码 `EXPRESSION_EVAL_ERROR`/`EXPRESSION_SYNTAX_ERROR` 核对注册状态，缺则同批注册。

### 4.3 flow 包（branch_router.go + branch_comp.go）

**Branch**（branch_router.go 内，Python 同文件）：
- `Branch{BranchID string; Target []string; condition Condition}`
- `NewBranch(condition, target, branchID)`：三态归一——`Condition` 实例直用 /
  string→`NewExpressionCondition` / func→`NewFuncCondition`；其余
  `ValueError("branch condition type does not meet the requirements")`（add_branch 捕获转 101020）。
- `Evaluate(session) bool`、`TraceInfo(session)`。

**BranchRouter**：
- `BranchRouter{branches []Branch; session; reportTrace bool}`，`NewBranchRouter(reportTrace bool)`。
- `AddBranch(condition, target, branchID)`：nil 校验（"condition is None or target is None"）→
  str target 归一 `[t]` → 构造 Branch（ValueError→101020 转译）。
  drawable 分支三处跳过（拍板 ②）。
- `AllTargets() map[string]struct{}`（Python 返回 set[str]；语义=去重目标全集）。
- `SetSession(session)`：双类型分支（Session 内层取 `_inner` / BaseSession 直取 /
  其余 "session type is wrong"）。
- `Call()`（≙ `__call__`）：report_trace 时先收集 branches 的 `{"branch_id", "condition"(trace_info)}`
  → `trace_component_begin` + `trace_component_inputs({"branches": ...})`；顺序 `Evaluate`，
  首个命中：report_trace 时 `trace_component_outputs({"branch_id"})` + `trace_component_done`，
  返回该 target；全不命中 build_error(COMPONENT_BRANCH_EXECUTION_ERROR,
  "branch meeting the condition was not found")（101021）。
- **实现 8.5 `pregel.Selector` 形状**（`Select(ctx, state any)`，state 恒 nil 不读——8.5 D2
  惯例，session 走实例字段捕获）。包内用私有适配或直接让 BranchRouter 实现 Selector 接口，
  实施时按 8.1 `Router = pregel.Selector` 别名口径落。

**BranchComponent**（branch_comp.go，8.17 WorkflowComponent 槽位模式）：
- `NewBranchComponent()`：内嵌 WorkflowComponent 基座 + `router := NewBranchRouter(true)`。
- `AddBranch(condition, target, branchID)`：condition falsy 校验
  （"condition is None or empty"）→ target falsy 校验 → list 逐项空串校验（带 index）
  → 转调 `router.AddBranch`（三处均 COMPONENT_BRANCH_PARAM_INVALID，101020 系）。
- `Router()`：返回 `self._router`（供构图方传入 AddConditionalEdges）。
- `InvokeFn`：`set_session(session)` → 返回 `{}`（空 map，一比一）。
- `AddComponent(g, nodeID, waitForAll)`：`g.AddNode(nodeID, ToExecutable(), waitForAll)` +
  `g.AddConditionalEdges(nodeID, Router())` + `len(router.AllTargets()) > 1` 时
  `g.RegisterBranchTargets(nodeID, targets)`（8.1 PregelGraph 特有方法由消费方断言的兑现点；
  Python 的 `hasattr` 守卫 Go 以 PregelGraph 具体类型承载，无需反射）。
- `SkipTrace() bool` → 恒 true。
- ToExecutableFn：走 8.17 槽位默认组装（invoke 槽）。

## 5. 接缝核对清单（实施中盘点）

1. **8.14 base_workflow.go 的 AddConditionalEdges**：Python `_workflow.py:275-285` 有
   isinstance(BranchRouter) 特判（set_session + register_branch_targets）。实施时核对 Go 8.14
   是否已覆盖该路径；缺失则本节补齐并登记偏差/回填（规则 13.2，不许默默跳过）。
2. **8.10 atomic_node.go**：初核 `AtomicInvoke` 导出函数可复用；实施时按 Condition.Call
   嵌入形状定稿（AtomicNode D1/D2 校验链：session 非空 + state WorkflowCommitState 断言）。
3. **tracer**：`trace_component_begin/inputs/outputs/done` 四段对齐 8.14 既有工具
   （TracerWorkflowUtils 等价面）。
4. **错误码**：COMPONENT_BRANCH_PARAM_INVALID / COMPONENT_BRANCH_EXECUTION_ERROR（101020 系）、
   EXPRESSION_EVAL_ERROR / EXPRESSION_SYNTAX_ERROR——核对 Go 注册状态。

## 6. 测试

- 单测：
  - expression 全语料：每类 AST 节点、8 条 RULES（含词边界反例 `is_false_alarm`）、
    字符串字面量保护、Name 四级解析、`in` TypeError→False、三重护栏触达、Pow 上限、
    dunder/敏感属性封禁、_safe_len/_safe_is_empty 分支。
  - router：首命中返回/顺序敏感、全不命中 101021、report_trace 四段 trace、SetSession 双类型、
    AddBranch nil/str 归一/ValueError 转译。
  - component：AddBranch 三段校验、AddComponent 守卫（≤1 目标不登记 RegisterBranchTargets）、
    InvokeFn 返回 `{}`、SkipTrace=true。
- 集成：真实 workflow `start → branch → end_a/end_b` 双出口（表达式条件 + AlwaysTrue 兜底），
  断言命中分支、CNF barrier 编译通过、trace 段落正确（审计跨度 ≥3 子系统）。

## 7. 偏差登记

| 编号 | 内容 | 理由 | 回收条件 |
|---|---|---|---|
| D-19-1 | drawable 三处整段跳过（env 读取 / DrawableBranchRouter / add_branch 填充） | 拍板 ②；8.13 未实施，消费面为零 | 8.13 实施时回填消费面；须在 8.13 计划文档标注待回填 |
| D-19-2 | `Condition._atomic_invoke` 元组分支（set_outputs）不落 | 生产者 ArrayCondition 属 8.20；接口仅 bool 返回 | 8.20 number/array 实施时随生产者一并落 |

## 8. 缺失项与回填清单（规则 13，不丢失）

- ✅ 兑现：8.10 spec D1 回收（AtomicNode 首个真实消费断言）；8.1「RegisterBranchTargets
  等特有方法由消费方断言」兑现（AddComponent 内）。
- ⤵️ 待回填：8.13 drawable 消费面（D-19-1，须在 8.13 计划文档标注）；8.20 number/array
  （含 _atomic_invoke 元组分支 D-19-2）。
- 🔍 实施中盘点：§5 四项接缝，发现缺失按规则 13.2 提出讨论（三档建议），不许默默跳过。

## 9. 完成标准

- 单测 + 集成全绿；`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全量回归零 FAIL。
- 包覆盖率 ≥85% 基线（condition/flow 两包）。
- IMPLEMENTATION_PLAN 8.19 行写 Go 事实（含偏差 D-19-1/2）+ 十八次换序注记
  （下一节 8.20 含 number.py/array.py，8.13 继续压后并标注 drawable 缝待回填）。
