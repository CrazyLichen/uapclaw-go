# 9.90a-2 设计：auto_permission models + tool_binding

> 对应 Python：`jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py`（101 行）
> 与 `jiuwenswarm/agents/harness/common/rails/permissions/tool_binding.py`（25 行）
> 目标 Go 包：`internal/swarm/agents/harness/common/rails/permissions/auto_permission/`
> 制定日期：2026-11-07
> 遵循规则：2（计划同步）、8（删除清单）、9（结构忠实）、10（接口归属）、11（CI 不主动关注）、12（不改层次/步骤/命名）

---

## 一、定位与作用

### 1.1 在 agent 会话中的流程位置

```
用户提问
   │
   ▼
DeepAgent.run → LLM 返回一批 tool_calls（可能 5 个并行）
   │
   ├─ 逐个执行工具
   │     │
   │     └─ SecurityRail / PermissionEngine 规则判定
   │            ├─ ALLOW → 放行
   │            ├─ DENY  → 拒绝
   │            └─ ASK   → 需要用户确认   ←── 权限体系从这里介入
   │
   ├─【9.90a  基础类型层 ← 本批】身份与无状态契约
   │     ├ ToolInvocationKeyV1       逻辑调用号码牌（9.90c 已落地）
   │     ├ CommandExecutionBinding   请求级 sys operation 归属（9.90c 已落地）
   │     ├ protected_paths           仓内保护路径（9.90c 已落地）
   │     ├ tool_permission_context   三个 ContextVar（9.90c 已落地）
   │     ├ MAX_STRUCTURED_QUESTIONS  反问上限（9.90c 已落地）
   │     ├ models.go                 本批 ★ 三个数据契约 + 11 常量
   │     └ tool_binding.go           本批 ★ 实现身份背书
   │
   ├─【9.90b  决策事实层】把工具调用 args 翻译成「事实」
   ├─【9.90c  root 五文件】✅ 已完成：串行队列 + 意图窗口 + 中断扩字段
   ├─【9.90d  自动审查层】消费 9.90a/b/c 做自动放行判定
   └─【9.90e  护栏与审计】URL 安全、send_file 路径守卫、审计落盘
```

### 1.2 一句话作用

**本批不产生任何决策，只提供"词汇表"与"身份背书"**：

- `models.go` 提供自动审查层通用的三个数据契约（工具调用快照、处理结果、权限中断请求）与 11 个共享常量；
- `tool_binding.go` 提供**实现身份校验**——回答"我要放行的这个工具，是不是我以为的那个实现"，是 9.90d 所有「按工具名自动放行」规则的前置条件。

### 1.3 为什么 `tool_binding` 不可省

自动放行的判定依据是工具语义（如「Read 工具是只读的，不必问用户」）。若只看工具名，存在被替换实现绕过的风险：

```
LLM 请求调用 read_file
   ↓
PermissionEngine 查表：read_file 是只读工具 → 自动放行
   ↓
但注册表里的 read_file 已被换成别的实现（子类覆盖 / 同名注册 / 后改卡片字段）
   ↓
被放行的是那个被换过的实现 → 自动放行了一次写操作
```

`ResolveToolBinding` 就是这道闸：实现类必须是**精确**那个类（子类也算不合格，因为可重写 `Invoke`），且卡片必须是**同一个对象**（不是字段相等的另一张卡）。任一不满足返回 `(nil, nil)`（判定为不合格），调用方据此拒绝自动放行、改走问用户；注册表自相矛盾（卡在而工具不在）则返回 `(nil, err)` 由调用方记日志。

Python 源码注释写明这一点：

> `# Subclasses can replace execution; this is implementation identity, not assignability.`

---

## 二、Go 侧现状盘点（规则 13.4）

### 2.1 9.90a 全量对照

计划表 9.90a 行列出 7 个文件。逐项核对 Go 现状：

| # | Python 文件 | 行数 | Go 现状 | 落点 | 判定 |
|---|---|---|---|---|---|
| 1 | `permissions/tool_invocation_key.py` | 94 | ✅ 已完成 | `permissions/tool_invocation_key.go`（213 行） | 完整 |
| 2 | `rails/ask_user_contract.py` | 4 | ✅ 已完成 | `rails/ask_user_contract.go` | 完整（跨包，对齐 Python 目录） |
| 3 | `permissions/tool_permission_context.py` | 40 | ✅ 已完成 | `swarm/schema/permission.go` | 完整（跨包，见 2.3 偏差） |
| 4 | `common/tools/command_execution_context.py` | 59 | ✅ 已完成 | `common/tools/command_execution_context.go` | 完整（跨包，见 2.3 偏差） |
| 5 | `permissions/protected_paths.py` | 26 | ✅ 已完成 | `permissions/protected_paths.go` | 完整 |
| 6 | `permissions/tool_binding.py` | 25 | ❌ **完全缺失** | 本批新建 | **本批** |
| 7 | `permissions/_auto_permission/models.py` | 101 | ❌ **完全缺失** | 本批新建 | **本批** |

**已完成 5 项来源**（计划表需据此回填 ✅）：

- commit `337007d0 feat(permissions): 补 9.90c 前置件（tool_invocation_key/command_execution/常量）` —— 覆盖 #1/#2/#3/#4/#5
- commit `bce0bd92 fix(permissions): 修正 resolveSandboxed 的 mode 判定` —— 修正 #4 的消费侧

### 2.2 本批实际工作量

**2 个实现文件、约 250 行实现 + 约 300 行测试**，不是计划表写的「约 400 行、全未开始」。

### 2.3 已登记的跨包偏差（不属本批，仅记录）

`tool_permission_context.py`（40 行 / 3 个 ContextVar）与 `command_execution_context.py`（59 行）在 Python 侧是独立模块，Go 侧分别落到了 `swarm/schema/permission.go` 与 `common/tools/`：

| Python | Go | 性质 | 理由 |
|---|---|---|---|
| `tool_permission_context.py` 的 3 个 ContextVar | `swarm/schema/permission.go` 的 6 个 ctx 函数 | 文件归属漂移 | Go 用 `context.WithValue`，无 ContextVar 的 set/reset token 语义，同族 ctx 键集中在 schema 包便于发现 |
| `command_execution_context.py` | `common/tools/command_execution_context.go` | 无偏差 | 与 Python 目录一致 |

**`tool_permission_context.py` 的文件归属漂移已在本 spec 登记，但不在本批修正范围内。** 若要严格对齐，需在 `permissions/` 包内新建同名文件并迁走 `swarm/schema/permission.go` 的 6 个函数——这会影响 `root_context_rail.go` 等现有调用方，属独立重构批次。

---

## 三、结构对照（规则 9.4 方法清单）

### 3.1 `models.py` → `auto_permission/models.go`

| # | Python 符号 | 类型 | Go 对应 | 说明 |
|---|---|---|---|---|
| 1 | `_REVIEWER_ACTION_TARGET_MAX_LENGTH` = 160 | 常量 | `ReviewerActionTargetMaxLength` | 私有转导出（Go 包级无「同模块私有」概念，对齐既有 `permissions` 包做法） |
| 2 | `_REVIEWER_UI_TEXT_MAX_LENGTH` = 1024 | 常量 | `ReviewerUITextMaxLength` | 同上 |
| 3 | `_REVIEWER_UI_SHORT_TEXT_MAX_LENGTH` = 240 | 常量 | `ReviewerUIShortTextMaxLength` | 同上 |
| 4 | `_REVIEWER_DENIAL_GUIDANCE` | 常量 | `ReviewerDenialGuidance` | 字符串逐字照抄 |
| 5 | `_SHELL_DISPLAY_PATH_PATTERN` | `re.Pattern` | `ShellDisplayPathPattern *regexp.Regexp` | 模式串逐字照抄；RE2 兼容性已实测 |
| 6 | `_SHELL_DISPLAY_PRE_SPLIT_PATH_PATTERN` | `re.Pattern` | `ShellDisplayPreSplitPathPattern *regexp.Regexp` | 同上 |
| 7 | `_SENSITIVE_DISPLAY_PATH_PATTERN` | `re.Pattern` | `SensitiveDisplayPathPattern *regexp.Regexp` | 同上 |
| 8 | `PROHIBITED_FILE_DELIVERY_REASON` | 常量 | `ProhibitedFileDeliveryReason` | `"file_delivery_prohibited_by_user"` |
| 9 | `DETERMINISTIC_BOUNDED_SCOPE_DECISION_SOURCE` | 常量 | `DeterministicBoundedScopeDecisionSource` | `"deterministic_bounded_scope"` |
| 10 | `DETERMINISTIC_READONLY_PUBLIC_WEB_REASON` | 常量 | `DeterministicReadonlyPublicWebReason` | `"deterministic_readonly_public_web"` |
| 11 | `DETERMINISTIC_READONLY_PUBLIC_WEB_FETCH_SOURCE_KINDS` | `frozenset` | `DeterministicReadonlyPublicWebFetchSourceKinds map[string]struct{}` | 只读语义，见 3.1.1 |
| 12 | `ToolInvocation` | `@dataclass(frozen=True)` | `ToolInvocation` struct | 字段类型见 3.1.2 |
| 13 | `PermissionHandlingResult` | `@dataclass(frozen=True)` | `PermissionHandlingResult` struct + `NewPermissionHandlingResult` | 构造校验见 3.1.3 |
| 14 | `PermissionInterruptRequest` | `class(InterruptRequest)` | `PermissionInterruptRequest` struct（嵌入） | 见 3.1.4 |

#### 3.1.1 `frozenset` → `map[string]struct{}`

Python 的 `frozenset({"recent_search_result"})` 只用于成员判断（`in`），Go 侧用 `map[string]struct{}` 表达。**不提供写方法**，并把变量声明为包级 `var`（Go 无法声明真正的不可变 map）。约定：本包内不对此 map 赋值；如需进一步保障，后续可改为 `func` + `switch`，但那会偏离 Python 的集合语义，暂不做。

#### 3.1.2 `ToolInvocation` 字段类型（D1 决策）

Python 四个字段全是 `Any` / `str`：

```python
@dataclass(frozen=True)
class ToolInvocation:
    ctx: Any | None
    tool_call: Any | None
    tool_name: str
    tool_args: Any
```

Go 侧的类型不能一律 `any`——项目惯例是把 Python 的 `Any` **收窄到运行时实际类型**（依据：`agentcore/single_agent/interfaces/callback.go` 的 `ToolCallInputs` 定义，其 `ToolCall` 为 `*llmschema.ToolCall`、`ToolArgs` 为 `map[string]any`，注释明确写「对齐 Python `tool_args: Any`，运行时 dict」）。

| 字段 | Python | Go 类型 | 收窄依据 |
|---|---|---|---|
| `ctx` | `Any \| None` | `*agentinterfaces.AgentCallbackContext` | 消费方全部直接使用 `invocation.ctx.agent` / `.session` / 作为 `plan_hard_rejected(ctx)` 实参；与 `ToolCallInputs` 的 ctx 同源 |
| `tool_call` | `Any \| None` | **`any`（必须保留）** | 见下方说明 |
| `tool_name` | `str` | `string` | 直接对应 |
| `tool_args` | `Any` | `map[string]any` | 对齐 `ToolCallInputs.ToolArgs`；`invocation_context.py` 三个构造点传入的全是 dict |

**`tool_call` 为何必须保留 `any`**：`invocation_context.py:240` 构造时传的是 `SimpleNamespace(name=..., arguments=...)`，不是真正的 `ToolCall` 对象：

```python
normalized_tool_call = tool_call or SimpleNamespace(
    name=normalized_tool_name, arguments=normalized_tool_args
)
```

消费方只用 `getattr(tc, "name")` / `getattr(tc, "arguments")` 的鸭子类型读取。若 Go 侧收窄为 `*llmschema.ToolCall`，9.90d 的 `invocation_context` 将无法表达这个替身。**这属于 Python 自身设计的鸭子类型，不是 Go 侧偷懒。**

#### 3.1.3 `PermissionHandlingResult` 的 `__post_init__` 校验

Python 有一条构造期校验：

```python
def __post_init__(self) -> None:
    if self.handled and not self.decision_source:
        raise ValueError("handled permission result requires a decision source")
```

Go 无 `__post_init__`，用构造函数承载：

```go
// NewPermissionHandlingResult 构造并校验权限处理结果。
// Python: PermissionHandlingResult.__post_init__
func NewPermissionHandlingResult(handled bool, result any, decisionSource string) (*PermissionHandlingResult, error)
```

校验条件与顺序逐条照抄：`handled && decisionSource == ""` → 返回 `errors.New("handled permission result requires a decision source")`。

#### 3.1.4 `PermissionInterruptRequest`

Python：

```python
class PermissionInterruptRequest(InterruptRequest):
    model_config = {"extra": "allow"}
    interrupt_kind: str = InterruptKind.PERMISSION
```

Go 用嵌入 + 构造期赋值表达（**不引入工厂函数**，因为 Python 的类级默认值等价于构造时填默认参数）：

```go
// PermissionInterruptRequest 保留权限元数据供 UI 渲染的中断请求。
//
// InterruptKind 在构造时置为 saschema.InterruptKindPermission。
// Python: class PermissionInterruptRequest(InterruptRequest)
//   interrupt_kind: str = InterruptKind.PERMISSION
type PermissionInterruptRequest struct {
    saschema.InterruptRequest
}
```

构造约定（写入 doc 注释，不强制）：

```go
req := &PermissionInterruptRequest{
    InterruptRequest: saschema.InterruptRequest{
        InterruptKind: saschema.InterruptKindPermission,
    },
}
```

`:63` 已确认 `InterruptRequest.InterruptKind` 字段存在，且 `saschema.InterruptKindPermission = "permission"`（`response.go:105`）。

Python 的 `model_config = {"extra": "allow"}` 表示允许额外字段。Go 结构体天然允许嵌入扩展，**不需要额外表达**；但需注意 9.90d 若要从 `PermissionInterruptRequest` 动态挂字段，应通过 `InterruptRequest.Metadata`（已存在）而非新字段——这一点写进 doc 注释提醒。

### 3.2 `tool_binding.py` → `auto_permission/tool_binding.go`

Python 原文件（25 行）：

```python
def matches_bound_method(method: object, *, expected_owner: object, expected_func: object) -> bool:
    return (
        expected_owner is not None
        and expected_func is not None
        and getattr(method, "__self__", None) is expected_owner
        and getattr(method, "__func__", None) is expected_func
    )


def resolve_tool_binding(agent, tool_name: str, expected_type: type):
    card = agent.ability_manager.get(tool_name)
    resource = Runner.resource_mgr.get_tool(card.id, session=None) if card else None
    exact_type = type(resource) is expected_type
    if exact_type and resource.card is card:
        return resource
    return None
```

| # | Python 符号 | Go 对应 | 说明 |
|---|---|---|---|
| 1 | `matches_bound_method` | `MatchesBoundMethod` | 签名调整，见 3.2.1 + 偏差 D1 |
| 2 | `resolve_tool_binding` | `ResolveToolBinding` | 签名调整，见 3.2.2 + 偏差 D2 |

#### 3.2.1 `MatchesBoundMethod` 的签名偏差

Python 的 `method.__self__ is expected_owner` 校验「方法绑定的接收者就是那个对象」。Go 的函数值**不携带 receiver 信息**，`a.Method` 与 `b.Method` 取出的代码指针完全相同（既有先例注释明确记载：`agentcore/harness/rails/model_anomaly.go:770` 的 `funcPtrEqual` 注释「同一方法的不同 receiver 代码指针相同」）。

因此 Go 侧必须把 receiver 作为**显式参数**传入：

```go
// MatchesBoundMethod 比较调用方提供的两个绑定身份。
// Python: matches_bound_method(method, *, expected_owner, expected_func)
func MatchesBoundMethod(receiver any, method any, expectedOwner any, expectedFunc any) bool
```

- `receiver` ↔ Python 的 `method.__self__`
- `method` ↔ Python 的 `method.__func__`
- `expectedOwner` / `expectedFunc` ↔ 同名参数

判定顺序逐条照抄：

1. `expectedOwner == nil` → false
2. `expectedFunc == nil` → false
3. `receiver` 与 `expectedOwner` **指针同一性**不成立 → false
4. `method` 与 `expectedFunc` **代码指针**不相同 → false
5. 否则 true

第 3 步用 `reflect.ValueOf(x).Kind()` 分类后比较：指针 / map / slice / func 用 `Pointer()`，其他可比较类型用 `==`（Python 的 `is` 在非单例对象上等价于身份，Go 用手写 `identityEqual` 助手承载）。
第 4 步用 `reflect.ValueOf(method).Pointer() == reflect.ValueOf(expectedFunc).Pointer()`，与既有 `funcPtrEqual`（`agentcore/harness/rails/model_anomaly.go:774`）同款。

**实际调用点形态印证三参数可行**：`readonly_tool_bindings.py:56-62` 的 `_method_matches(owner, expected, name)` 中 `owner` 与 `expected` 都已在手：

```python
def _method_matches(owner, expected, name):
    method = getattr(owner, name, None)
    exact_owner = type(owner) is expected
    return exact_owner and matches_bound_method(
        method, expected_owner=owner, expected_func=getattr(expected, name)
    )
```

外层 `type(owner) is expected` 做类型校验，内层 `matches_bound_method` 做绑定校验——职责分工清晰，Go 迁移时 receiver 直接取 `owner`。

#### 3.2.2 `ResolveToolBinding` 的签名偏差

```go
// ResolveToolBinding 返回确切注册的实现；查找失败与身份校验失败均返回 error。
// Python: resolve_tool_binding(agent, tool_name, expected_type)
func ResolveToolBinding(
    am agentinterfaces.AbilityManagerInterface,
    toolName string,
    expectedType reflect.Type,
) (tool.Tool, error)
```

三处调整：

1. `agent` → `am agentinterfaces.AbilityManagerInterface`（D2 决策）。Python 侧 `agent` 只被读一个属性 `agent.ability_manager`，Go 侧直接收 manager。按规则 10.2 判定口径：「这个接口是别人用来调用 DeepAgent 的吗？」——此处是 `auto_permission` 作为**使用方**去读 agent 的能力，属「DeepAgent 用来调用别人」的正向场景，可以窄接口。直接收 `AbilityManagerInterface` 是把「从 agent 取 manager」这一步**上提到调用方**，属方法归属漂移，登记偏差 D2。
2. `expected_type: type` → `expectedType reflect.Type`（D3 决策，用反射）。
3. 返回值 `Tool | None` → `(tool.Tool, error)`（D10 决策）。**理由**：Python 在「卡片存在但工具不在 resource_mgr」时由 `get_tool` 抛异常冒泡（`_inner_get_resources` 中 `exact_match=True` 时 `raise`，resource_manager.py:1577-1578），`resolve_tool_binding` 自身无 try/except；Go 的 `ResourceMgr.GetTool` 找不到时返回空切片 + nil error，若只靠 nil 区分就丢失了「异常」这一档语义。改为返回 error 可保住 Python 的「找不到 = 异常」契约。

`get_tool(card.id, session=None)` → `runner.GetResourceMgr().GetTool([]string{id})`，**不传 `WithSession`**，等价于 `session=None`——不加 trace 装饰，拿到的就是裸实现（若加了装饰会变成 `*decorator.TracedTool`，第 4 步精确类型校验必然失败）。

**返回语义对照（必须逐档分清）**：

| 情形 | Python 行为 | Go 行为 |
|---|---|---|
| 能力不存在（`card is None`） | 返回 `None`（正常返回） | `(nil, nil)` |
| 工具查找报错 / 系统异常 | 异常冒泡 | `(nil, err)` |
| 工具不存在（卡在、工具不在） | `get_tool` 抛异常冒泡 | `(nil, err)` |
| 实现类型不符 | 返回 `None` | `(nil, nil)` |
| 卡片不同一 | 返回 `None` | `(nil, nil)` |
| 全部通过 | 返回 resource | `(resource, nil)` |

即：**「否」是 `(nil, nil)`（判定为不合格），「异常」是 `(nil, err)`（本该不可能发生）。** 调用方对两者都按「不自动放行」处理，但 error 分支应记录日志，因为它意味着注册表自相矛盾（卡片在 ability_manager 而工具不在 resource_mgr）。

实现步骤严格按 Python 顺序（规则 9.3）：

```
1. card := am.Get(toolName)
2. if card == nil → return nil, nil                 （Python: if card else None）
3. resource, err := resourceMgr.GetTool([]string{card.AbilityID()})
4. if err != nil → return nil, err                  （Python: 异常冒泡）
5. if len(resource) == 0 → return nil, err          （工具不存在，对齐 Python 抛异常）
6. resource0 := resource[0]
7. exactType := reflect.TypeOf(resource0) == expectedType
8. if !exactType → return nil, nil
9. cardPtr, ok := card.(*tool.ToolCard); if !ok → return nil, nil
10. if resource0.Card() != cardPtr → return nil, nil  （Python: resource.card is card）
11. return resource0, nil
```

步骤 5 的 error 文案：`fmt.Errorf("tool %q registered in ability manager but missing from resource manager", card.AbilityID())`。日志文案用英文（对齐 `feedback-logs-english`），若本批引入日志则记 `logger.Warn`。

步骤 9 说明：Python 的 `card` 是 `AbilityCard`（有 `.id` 与 `.card` 同一性），Go 的 `schema.Ability` 接口只暴露 `AbilityName()` / `AbilityID()` / `AbilityKind()`。已确认 `*tool.ToolCard` 实现该接口（`foundation/tool/base.go:243-249`）且 `ToolCard` 内嵌 `schema.BaseCard`——`AbilityManager.Get()` 存的正是工具注册时的 `*tool.ToolCard` 指针，因此 `card.(*tool.ToolCard)` 后与 `resource.Card()` 比指针即可得到 `is` 语义。

若 `card` 不是 `*tool.ToolCard`（例如 workflow / agent 类型的能力），步骤 9 返回 `(nil, nil)`。这与 Python 一致：Python 中 workflow 能力在 `get_tool`（`resource_type="tool"`）查不到，走的是「工具不存在」分支——即 Python 会**抛异常**，而非返回 None。**这是唯一一处 Go 与 Python 不严格等价的地方**：Python 抛异常，Go 在步骤 9 提前返回 `(nil, nil)`。因为 `card.(*tool.ToolCard)` 断言失败说明该能力根本不是 tool，属「合格性判定为否」而非「系统异常」，返回 `(nil, nil)` 更合理。登记为偏差 D11。

---

## 四、包结构与环境

### 4.1 新建子包

```
internal/swarm/agents/harness/common/rails/permissions/
├── auto_permission/          ← 本批新建
│   ├── doc.go                # 包文档
│   ├── models.go             # ToolInvocation / PermissionHandlingResult / PermissionInterruptRequest + 11 常量
│   ├── models_test.go
│   ├── tool_binding.go       # MatchesBoundMethod / ResolveToolBinding
│   └── tool_binding_test.go
```

对齐 Python 的 `_auto_permission/` 子目录（规则 9.1）。Go 侧目录名去掉下划线前缀：Python 的 `_auto_permission` 下划线表示「内部子包」，Go 无此约定，且导出包名不应以下划线开头。

**9.90d 的 20 个文件后续全部进入此子包**，与 Python 目录一一对应。

### 4.2 import 与循环依赖

| 依赖 | 用途 | 环风险评估 |
|---|---|---|
| `agentcore/single_agent/interfaces`（别名 `agentinterfaces`） | `AgentCallbackContext`、`AbilityManagerInterface` | 已实测 `agentcore` 无一文件 import `internal/swarm`，**无环** |
| `agentcore/single_agent/schema`（别名 `saschema`） | `InterruptRequest`、`InterruptKindPermission` | 同上 |
| `agentcore/foundation/tool` | `tool.Tool`、`tool.ToolCard` | 同上 |
| `agentcore/runner` | `GetResourceMgr()` | 同上 |
| `common/logger` | 日志（如需） | 无环 |

### 4.3 日志

Python `models.py` 定义了 `logger = logging.getLogger(__name__)` 但**全文无一处 `logger.` 调用**（仅定义）。`tool_binding.py` 完全无日志。

**但 `models.go` 不引入日志；`tool_binding.go` 引入一处**：因为 D10 把「工具不存在」做成 error 后，该分支需要留痕（规则 3.4 异常路径日志），否则注册表自相矛盾时无从排查。

- `models.go`：无日志
- `tool_binding.go`：`var logComponent = logger.ComponentPermissions`（对齐父包 `owner_scopes.go:55`），仅步骤 5 一处 `logger.Warn`

---

## 五、测试设计

### 5.1 覆盖率目标

按规则 3 要求 ≥ 85%。本批两个文件目标：

- `models.go` ≥ 90%（大部分是常量，测试即断言取值）
- `tool_binding.go` ≥ 95%（逻辑分支少，可全覆盖）

### 5.2 models_test.go

| 用例 | 断言 |
|---|---|
| `TestReviewerActionTargetMaxLength_取值` | 等于 160 |
| `TestReviewerUITextMaxLength_取值` | 等于 1024 |
| `TestReviewerUIShortTextMaxLength_取值` | 等于 240 |
| `TestReviewerDenialGuidance_内容` | 与 Python 原文逐字相等 |
| `TestShellDisplayPathPattern_编译与匹配` | 非 nil；对 `/Users/a/b.txt`、`/home/bob/x`、`~/work/f`、`/private/tmp/z`、`C:\Users\me\a.txt` 均匹配 |
| `TestShellDisplayPreSplitPathPattern_编译与匹配` | 非 nil；对 Windows 盘符路径匹配，对 Unix 路径不匹配 |
| `TestSensitiveDisplayPathPattern_编译与匹配` | 非 nil；`/a/.env`、`/a/credentials`、`/a/id_rsa`、`/a/.ssh/config`、`/a/my_secret.txt` 匹配；`/a/normal.txt` 不匹配 |
| `TestProhibitedFileDeliveryReason_取值` | `"file_delivery_prohibited_by_user"` |
| `TestDeterministicBoundedScopeDecisionSource_取值` | `"deterministic_bounded_scope"` |
| `TestDeterministicReadonlyPublicWebReason_取值` | `"deterministic_readonly_public_web"` |
| `TestDeterministicReadonlyPublicWebFetchSourceKinds_内容` | 长度为 1，含 `"recent_search_result"` |
| `TestNewPermissionHandlingResult_正常` | handled=false + 空 source 可构造 |
| `TestNewPermissionHandlingResult_handled无来源报错` | handled=true + source="" → error |
| `TestNewPermissionHandlingResult_handled有来源` | handled=true + source 非空 → 成功 |
| `TestPermissionInterruptRequest_嵌入字段可见` | 构造后 `InterruptKind == saschema.InterruptKindPermission` |
| `TestToolInvocation_字段` | 四个字段可赋值、可读 |

正则用例的样例字符串取自 Python 侧语义推导（三个正则在 Python 全仓零消费，无现成测试样例可对照），并在 spec 中记录该事实。

### 5.3 tool_binding_test.go

需要 fake 设施：

- `fakeAbilityManager`：实现 `agentinterfaces.AbilityManagerInterface` 的 `Get`（其余方法返回零值）。**注意**：该接口共 **10 个方法**（`Add` / `AddMany` / `Remove` / `RemoveMany` / `Get` / `List` / `ListToolInfo` / `Execute` / `SetContextEngine` / `ReorderTools`），本批只用到 `Get`。按规则 10.1-2「测试替身自己补齐」，逐个返回零值即可，不另建基类。
- `fakeTool`：实现 `tool.Tool`（`Card() *tool.ToolCard` / `Invoke` / `Stream` 三个方法），用于精确类型校验。
- `fakeToolSub`：嵌入 `fakeTool`，用于验证「子类不算合格」。

**关于 `ResolveToolBinding` 的测试可行性**：它内部调用 `runner.GetResourceMgr()` 全局单例，无法在单测中注入替身。这违反规则 3.3「可 mock 的代码禁用 build tag 逃避」。两种处理：

- (a) 包级变量注入：`var resolveToolByID = func(id string) tool.Tool { ... }`，测试时替换。属「为测试改生产代码」，但项目已有同类先例（`common/tools/send_file.go` 的 `SetSendFilePushFunc` / `SetSendFileHistoryFunc` 成对 setter 注入）。
- (b) `//go:build integration` 隔离整个 `ResolveToolBinding` 测试。

**选择 (a)**，理由：`GetResourceMgr()` 是包级函数且底层是全局单例，用包级变量注入是 Go 中测试全局依赖的既有做法；且 `ResourceMgr` 可真实构造（`resources_manager.NewResourceMgr()`），测试中能真实注册工具后走完整链路，比 build tag 隔离更有价值。

注入形式对齐既有先例（`send_file.go` 用 `var (...) + SetXxxFunc` 成对导出 setter），本批采用同样的成对形态：

```go
// resolveToolByID 按 ID 取工具实例，默认走全局 ResourceMgr。
// Python: Runner.resource_mgr.get_tool(tool_id, session=None)
//
// 返回 (nil, nil) 表示工具不存在；(nil, err) 表示查找过程报错。
var resolveToolByID = func(id string) (tool.Tool, error) {
    tools, err := runner.GetResourceMgr().GetTool([]string{id})
    if err != nil {
        return nil, err
    }
    if len(tools) == 0 {
        return nil, nil
    }
    return tools[0], nil
}

// SetResolveToolByIDFunc 替换工具查找实现，仅供测试注入。
// 对齐 common/tools/send_file.go 的 SetXxxFunc 成对注入惯例。
func SetResolveToolByIDFunc(fn func(id string) (tool.Tool, error)) {
    if fn != nil {
        resolveToolByID = fn
    }
}
```

**日志**：因为本批引入了 error 分支，按规则 3.4 需在「注册表自相矛盾」处记录日志。本批因此需要 `logComponent`：

```go
// logComponent 日志组件
var logComponent = logger.ComponentPermissions
```

对齐父包 `permissions/owner_scopes.go:55` 同款声明。日志点仅一处：步骤 5 的「工具不存在」分支记 `logger.Warn(logComponent).Str("event_type", "PERMISSION_TOOL_BINDING_MISSING").Str("tool_id", ...).Msg("tool registered in ability manager but missing from resource manager")`（Msg 用英文，对齐 `feedback-logs-english`）。

测试覆盖：

| 用例 | 断言 |
|---|---|
| `TestResolveToolBinding_正常` | 注册真工具、card 同一 → 返回该实例，err 为 nil |
| `TestResolveToolBinding_能力不存在` | `am.Get` 返回 nil → `(nil, nil)` |
| `TestResolveToolBinding_类型不匹配` | `expectedType` 换成别的类型 → `(nil, nil)` |
| `TestResolveToolBinding_子类不算合格` | 注册 `fakeToolSub` 但 `expectedType` 为 `fakeTool` → `(nil, nil)` |
| `TestResolveToolBinding_卡片不同一` | 同 ID 但换一张卡 → `(nil, nil)` |
| `TestResolveToolBinding_非ToolCard能力` | `am.Get` 返回非 `*tool.ToolCard` → `(nil, nil)`（偏差 D11） |
| `TestResolveToolBinding_工具不存在返回error` | card 在 ability_manager 但 `resolveToolByID` 返回 nil → `(nil, err)`，err 非空 |
| `TestResolveToolBinding_GetTool报错透传` | `resolveToolByID` 内部 err 透传 → `(nil, err)` |
| `TestMatchesBoundMethod_正常` | receiver 同、函数同 → true |
| `TestMatchesBoundMethod_owner为nil` | → false |
| `TestMatchesBoundMethod_func为nil` | → false |
| `TestMatchesBoundMethod_receiver不同` | 不同实例的同名方法 → false |
| `TestMatchesBoundMethod_函数不同` | 同一 receiver 的两个不同方法 → false |

### 5.4 不测项

- 正则的**反向**用例（不匹配样例）只覆盖 `SensitiveDisplayPathPattern` 的 `/a/normal.txt` 一例，不穷举边界——Python 侧无对应测试可参照，且这些正则当前零消费。

---

## 六、已知偏差汇总（需确认）

| # | Python | Go | 性质 | 理由 | 回收条件 |
|---|---|---|---|---|---|
| D1 | `matches_bound_method(method, *, expected_owner, expected_func)` 2 参数，靠 `__self__` 取 receiver | `MatchesBoundMethod(receiver, method, expectedOwner, expectedFunc)` 4 参数 | 签名调整（规则 12.1-3 方法归属/参数） | Go 函数值不携带 receiver，`a.M` 与 `b.M` 代码指针相同，无法从 method 反查 receiver | Go 若引入反射获取 receiver 的能力（当前不存在） |
| D2 | `resolve_tool_binding(agent, tool_name, expected_type)`，内部读 `agent.ability_manager` | `ResolveToolBinding(am AbilityManagerInterface, toolName, expectedType)`，agent→manager 上提到调用方 | 层次位移（规则 12.2 禁止上提） | 规则 10.2 判定为「使用方依赖协作方」，可窄接口；且 Python 的 `agent` 参数只被读一个属性 | 若后续需要读 agent 的其他属性，须改回收 agent 或新增窄接口 |
| D3 | `expected_type: type` | `expectedType reflect.Type` | 类型表达 | Go 的 `type` 值即 `reflect.Type`，无其他表达方式 | 无 |
| D4 | 模块级私有常量 `_REVIEWER_*`（下划线前缀） | 导出 `Reviewer*` | 可见性 | Go 包级无「同模块私有」概念；同包外需要跨文件引用；对齐既有 `permissions` 包做法 | 无 |
| D5 | `frozenset` 不可变 | `map[string]struct{}` var，约定不改写 | 语言差异 | Go 无不可变 map | 无 |
| D6 | `model_config = {"extra": "allow"}` 允许动态字段 | 结构体天然允许嵌入扩展，不加表达 | 无实质偏差 | Go 结构体无「额外字段」概念；动态信息应走已存在的 `InterruptRequest.Metadata` | 无 |
| D7 | `@dataclass(frozen=True)` | 普通 struct，无只读保护 | 语言差异 | Go 无 frozen | 无 |
| D8 | `__post_init__` 校验 | `NewPermissionHandlingResult` 构造函数校验 | 语言差异 | Go 无 `__post_init__` | 无 |
| D9 | 3 个正则在 Python 全仓零消费 | 照抄并包级 `MustCompile` | 死代码保留 | 规则 9.2-4「不得裁剪成员」+ 既有「零调用死代码保留」惯例；RE2 兼容性已实测（3 个均编译通过且匹配正确） | 上游启用后自然成为活跃代码 |
| D10 | 返回 `Tool \| None`，查找失败时异常冒泡 | 返回 `(tool.Tool, error)`，找不到做成 error | 签名调整（返回值） | Go 的 `ResourceMgr.GetTool` 找不到时返回空切片 + nil error，只靠 nil 会丢失 Python「异常」这一档语义；改返回 error 可保住「找不到 = 异常」契约 | 无（这是对 Python 异常语义的忠实表达，不需要回收） |
| D11 | workflow/agent 类能力在 `get_tool` 查不到 → **抛异常** | 步骤 9 断言 `*tool.ToolCard` 失败 → 返回 `(nil, nil)` | 语义微调 | `card` 不是 `*tool.ToolCard` 说明该能力根本不是 tool，属「合格性判定为否」而非「系统异常」；返回 `(nil, nil)` 让调用方走「不自动放行」而非报错更合理 | 若上游把此场景明确为错误，改为返回 err |

**D1、D2、D10、D11 需在实施前确认**（规则 12.3 要求）。D3-D9 已在本文档登记。

---

## 七、提交分层

| # | commit | 内容 | 前置 |
|---|---|---|---|
| 1 | `feat(permissions): 建立 auto_permission 子包 + doc.go` | `auto_permission/doc.go` | — |
| 2 | `feat(permissions): models.py 三个数据契约 + 11 常量` | `models.go` + `models_test.go` | 1 |
| 3 | `feat(permissions): tool_binding 实现身份背书` | `tool_binding.go` + `tool_binding_test.go` | 1、2 |
| 4 | `docs(permissions): 9.90a 计划表回填（已完成 5 项标 ✅ + 本批收口）` | `IMPLEMENTATION_PLAN.md` + 父包 `doc.go` | 2、3 |

**第 4 项内容明确**：
- 9.90a 行状态 `☐` → `✅`
- 注明已完成 5 项来源：`337007d0`（五项前置件）+ `bce0bd92`（resolveSandboxed 修正）
- 本批新增两项：`models` + `tool_binding`
- 4 个零消费常量挂 ⤵️ 到 9.90d：`ShellDisplayPathPattern` / `ShellDisplayPreSplitPathPattern` / `SensitiveDisplayPathPattern` / `DeterministicReadonlyPublicWebFetchSourceKinds`
- 父包 `permissions/doc.go` 的文件目录补充 `auto_permission/` 子包条目 + ⤵️ 标注

---

## 八、删除清单（规则 8）

**本批为纯新增，无删除项。**

需核对两点：

1. `permissions` 包内是否已有 `ToolInvocation` / `PermissionHandlingResult` / `PermissionInterruptRequest` 的等价实现 → 已 grep 确认**不存在**（`grep -rn "PermissionInterruptRequest\|PermissionHandlingResult\|ToolInvocation{"` 仅命中 `MarkPermissionInterruptRequest` 这个无关函数名与 `PermissionInterruptRequestAttribute` 常量）
2. `tool_binding` 的等价能力是否已在别处 → 已 grep 确认 `ResolveToolBinding` / `MatchesBoundMethod` **零命中**

---

## 九、验收标准

| 项 | 标准 |
|---|---|
| 单元测试覆盖率 | `models.go` ≥ 90%，`tool_binding.go` ≥ 95% |
| 正则 | 3 个模式串与 Python 源码逐字一致；编译成功；样例匹配通过 |
| 常量取值 | 11 个常量与 Python 逐字一致（含 `_REVIEWER_DENIAL_GUIDANCE` 的换行与空格） |
| 类型校验 | `ResolveToolBinding` 对「子类」「换卡」「换类型」三种情形均返回 `(nil, nil)`；「卡在工具不在」返回 `(nil, err)` |
| 结构对照 | §3 方法清单逐条落实，无「Go 侧多出/少了」未说明项 |
| 构建 | `go build ./...`、`go vet ./...` 通过 |
| 全量回归 | `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL |
| 交付 | 推送到远端即完成，不追踪 CI 结果（规则 11） |

---

## 十、验收自检

- [ ] §3 逐符号对照完成，14 个 models 符号 + 2 个 tool_binding 符号全部落实
- [ ] 无「Go 侧多出来的符号」未说明
- [ ] 无「Go 侧少了的符号」未说明
- [ ] 命名全部 `snake_case → CamelCase`，无自行改名（`_REVIEWER_*` → `Reviewer*` 的可见性变更是 D4，已登记）
- [ ] 步骤顺序与 Python 一致（`ResolveToolBinding` 11 步逐条对应）
- [ ] 偏差 D1-D11 全部登记，D1/D2/D10/D11 经确认
- [ ] 返回语义六档（能力不存在 / 查找报错 / 工具不存在 / 类型不符 / 卡片不同一 / 通过）逐档有测试
- [ ] 4 个零消费常量在 doc.go 挂 ⤵️ 到 9.90d
- [ ] 计划表 9.90a 行回填 ✅ 并注明 `337007d0` / `bce0bd92` 来源
