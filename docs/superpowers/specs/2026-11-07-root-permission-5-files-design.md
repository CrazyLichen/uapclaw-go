# Root 权限五文件对齐设计（root_context / root_permission_queue / 两个 rail / root_ask_user）

> 对应 Python：`jiuwenswarm/agents/harness/common/rails/permissions/`
> 目标 Go 包：`internal/swarm/agents/harness/common/rails/permissions/`
> 制定日期：2026-11-07
> 遵循规则：9（结构忠实）、11（不改层次/步骤/命名）、12（旧逻辑同改动删除、缺失项提出讨论）

---

## 一、背景与定位

### 1.1 这套机制在会话流程中的位置

```
用户提问
   │
   ▼
DeepAgent.run（第 1 次 invoke）
   │
   ├─ LLM 返回一批 tool_calls（可能 5 个并行）
   │
   └─ 逐个执行工具
         │
         ├─ SecurityRail / PermissionEngine 规则判定 → ASK
         │
         ▼
    ┌────────────────────────────────────┐
    │ root_context_rail                  │  组装 RootDecisionContext
    │  ├ 从 PermissionContext 取 principal│  并 bind 到 ContextVar
    │  └ 组装意图窗口 trusted_turns       │
    └───────────────┬────────────────────┘
                    ▼
    ┌────────────────────────────────────┐
    │ root_permission_queue_rail         │  发号码牌 ToolInvocationKeyV1
    │  ├ queue.begin(key) → active       │  抛 ToolInterruptException
    │  └ 抛中断                          │
    └───────────────┬────────────────────┘
                    ▼
    ┌────────────────────────────────────┐
    │ root_permission_queue              │  mark_pending
    │  card: active → pending            │  冻结中断请求 + 冻结上下文
    └───────────────┬────────────────────┘
                    ▼
        只把队首一张卡抛给前端（串行化）
                    │
                    ▼
        用户点「允许 / 拒绝 / 反问」
                    │
                    ▼
DeepAgent.run（第 2 次 invoke，全新调用，不是协程挂起）
   │
   ├─ load interruption_state → _handle_resume
   │
   ├─ 答案归属：按 interrupt_owner 路由到对应 rail
   │
   └─ 重放工具调用（deepcopy tool_call 再执行一遍）
         │
         ├─ rail 第二次被调用 ← 拦截发生的真实时刻
         │
         ├─ queue.claim_answer_for_call() → resuming → active
         │
         ├─ resume.card.root_context 解冻上下文
         │
         └─ 引擎读到答案 → 放行或拒绝
```

### 1.2 一句话作用

把 LLM 一批并行工具调用产生的**多个权限中断，收敛成一条串行队列**，保证一次只问用户一张卡、答案精确不串台、任何对不齐的情况直接 fail-closed 冻结会话。

### 1.3 五个文件的分工

| 文件 | Python 行数 | 角色 | 大白话 |
|------|------------|------|--------|
| `root_context.py` | 528 | 数据结构 + ContextVar 载体 | 记决策背景的本子 |
| `root_permission_queue.py` | 635 | 队列状态机 | 排队叫号，答错就冻结 |
| `root_permission_queue_rail.py` | 536 | 身份 rail | 给每次调用发号码牌 |
| `root_context_rail.py` | 403 | 上下文绑定 rail | 把背景摆到桌上给引擎看 |
| `root_ask_user.py` | 274 | 反问续接 | 用户反问时记下来 |
| **合计** | **2376** | | |

---

## 二、能力边界（重要）

### 2.1 本批做完后的状态

```
✅ RootIntentTurn / RootDecisionContext     能存能取
✅ RootPermissionQueue                       能排队能校验能隔离
✅ 两个 rail 能 bind/unbind
✅ InterruptRequest 带 Metadata/Kind/Owner  能区分能路由

❌ 无任何代码调用 _intent_view()（意图视图投影）
❌ 无任何代码调用 original_user_intent()
❌ 无任何代码调用 has_valid_ask_user_clarification()
❌ 无 LLM 审查器读这个窗口
❌ 用户可见行为完全不变
```

**本批是"管道铺设完成，还没接水源"。**

### 2.2 为什么仍然值得做

1. **它是地基**：Python 侧 `root_context` / `root_permission_queue` 被 20+ 文件引用（`auto_reviewer.py`、`permission_interrupt_rail.py`、`auto_decision.py`、`_auto_permission/*`、`stream_event_rail.py`）。不做这批，后续 `auto_authorize`、`auto_reviewer`、`permission_continuation` 全都无法落地。
2. **中断字段是全局资产**：`Metadata` / `InterruptKind` / `InterruptOwner` 加上后，`ask_user` 等其他中断场景也能受益。
3. **可独立测试**：队列的排队、隔离、答案归属全部可用 fake 验证，不需要真实 Reviewer。

### 2.3 缺口登记（⤵️）

在五个文件各自的 doc.go 中标注：

```
⤵️ 意图窗口的消费方待 _auto_permission 批次回填（auto_reviewer.py 的 _intent_view，
   以及 before_tool.py 的 resolve_original_user_intent / url_safety.py 的
   has_valid_ask_user_clarification）。在此之前 RootDecisionContext 只被
   两个 root rail 读写，无判定逻辑消费。
```

对应计划表目标章节见 §八。

---

## 三、Go 侧现状盘点（规则 12.4）

### 3.1 permissions 包整体差距

| 维度 | Python | Go | 差距 |
|------|--------|-----|------|
| 文件数 | 38 个 .py | 7 个（4 实现 + 3 测试） | 缺 35 个 |
| 总行数 | 11,463 | 2,718（含测试） | — |
| 实现行数 | 11,463 | 约 1,450 | **约 10,000 行** |

### 3.2 已有三个文件的偏差（本批只登记不修，见 §七）

| Python | Go | 偏差 |
|--------|----|------|
| `permissions_persist.py`（14 函数） | `permissions_persist.go`（11 函数） | **缺 4 个**：`persist_exact_permission_allow_rule`、`_check_approval_accesses`、`_merge_file_guard_path_into_permissions`、`build_command_allow_pattern`、`_normalize_exact_accesses`（5 项，其中 2 个私有）；`PermissionApprovalInvalidated` 异常类缺失 |
| `permissions_config_rpc.py`（4 函数） | `permissions_config_rpc.go`（23 函数） | Go 侧多出 19 个实现函数，Python 只有 `dispatch_permissions_config_request` 一个入口 + 3 个私有。**疑似把 Python 上游 service 层的逻辑下沉到了这里**，违反规则 11.1 架构层次 |
| `owner_scopes.py`（7 个符号） | `owner_scopes.go`（10 个符号） | 缺 `setup_permission_context` / `cleanup_permission_context` / `current_permission_owner_scope`（Go 用 `PermissionContextFromCtx` 替代，命名已改，违反规则 11.1-4）；多出 `CheckAvatarPermission` |

**结论：这三个文件不能称为"已对齐"，属"部分对齐 + 层次漂移"。本批不动它们，登记为独立批次处理。**

### 3.3 本批所需前置件（Python → Go 状态）

| # | 前置件 | Python 源 | Go 现状 | 规模 |
|---|--------|----------|---------|------|
| 1 | `InterruptRequest` 扩字段 | `single_agent/interrupt/response.py:51-58` | 缺 `Metadata`/`InterruptID`/`InterruptKind`/`InterruptOwner` | 4 字段 + 7 值枚举 |
| 2 | `ToolInvocationKeyV1` | `tool_invocation_key.py`（94 行） | 完全缺失 | 小 |
| 3 | `MAX_STRUCTURED_QUESTIONS = 4` | `rails/ask_user_contract.py` | 完全缺失 | 1 常量 |
| 4 | `TOOL_PERMISSION_REQUEST_ID` | `tool_permission_context.py` | 缺失（`CHANNEL_ID` 已有） | 1 context key |
| 5 | `SKILLS_REBUILD_SILENT` | `tool_permission_context.py` | 缺失 | 1 context key |
| 6 | `CommandExecutionBinding` + `bind_command_execution` | `common/tools/command_execution_context.py` | 完全缺失 | 小包 |
| 7 | `PROTECTED_PATH` 常量 | `protected_paths.py`（26 行） | 待核 | 1 常量 |
| 8 | `InterruptKind` 消费方适配 | 5+ 处 | 需逐个适配 | 中 |

---

## 四、结构对照（规则 11.4 方法清单）

### 4.1 `root_context.py` → `root_context.go`

| Python 符号 | 类型 | Go 对应 | 备注 |
|------------|------|---------|------|
| `ROOT_CONTEXT_KEY` | 常量 | 同名 | `"jiuwenswarm.root_permission_context.v1"` |
| `HOST_USER_MESSAGE_SOURCE` | 常量 | 同名 | |
| `ROOT_INTENT_MAX_TURNS` | 常量 | 同名 | 20 |
| `ROOT_INTENT_MAX_TURN_CHARS` | 常量 | 同名 | 8000 |
| `ROOT_INTENT_MAX_TOTAL_CHARS` | 常量 | 同名 | 48000 |
| `AUTO_REVIEW_BLOCK_INTENT_INPUT_TOO_LARGE` | 常量 | 同名 | |
| `AUTO_REVIEW_BLOCK_HISTORY_WINDOW_TRUNCATED` | 常量 | 同名 | |
| `RootIntentTurnKind` | 类（4 值） | `RootIntentTurnKind` 字符串常量组 | `HISTORY`/`FRESH`/`STEER`/`ASK_USER_CLARIFICATION` |
| `RootAskUserOption` | dataclass | `RootAskUserOption` | `ToMapping` / `FromMapping` |
| `RootAskUserClarification` | dataclass | `RootAskUserClarification` | 校验规则逐条照抄 |
| `RootIntentTurn` | dataclass | `RootIntentTurn` | `ToMapping` / `FromMapping` |
| `root_intent_turn_text` | 模块函数 | `RootIntentTurnText` | 保持包级函数 |
| `RootDecisionContext` | dataclass | `RootDecisionContext` | `ToMapping` / `FromMapping` |
| `UserIntentSource` | 类 | `UserIntentSource` 常量 | |
| `OriginalUserIntentEvidence` | dataclass | `OriginalUserIntentEvidence` | 含 2 个 property |
| `_CURRENT_ROOT_CONTEXT` | ContextVar | context key | 见 §五 |
| `current_root_decision_context` | 模块函数 | `CurrentRootDecisionContext(ctx)` | |
| `bind_root_decision_context` | 模块函数 | `BindRootDecisionContext` | token 语义见 §五 |
| `reset_root_decision_context` | 模块函数 | `ResetRootDecisionContext` | |
| `root_decision_context_from_extra` | 模块函数 | `RootDecisionContextFromExtra` | |
| `put_root_decision_context_in_inputs` | 模块函数 | `PutRootDecisionContextInInputs` | |
| `append_root_clarification` | 模块函数 | `AppendRootClarification` | |
| `has_valid_ask_user_clarification` | 模块函数 | `HasValidAskUserClarification` | ⤵️ 无消费者 |
| `original_user_intent` | 模块函数 | `OriginalUserIntent` | ⤵️ 无消费者 |
| `file_delivery_constraint_text` | 模块函数 | `FileDeliveryConstraintText` | ⤵️ 无消费者 |
| `HOST_USER_PROMPT_PREFIX_ZH/EN` | 常量 | 同名 | |
| `HOST_USER_ORIGIN_EXTERNAL/INTERNAL` | 常量 | 同名 | |
| `_REQUIRED_ENVELOPE_KEYS` / `_OPTIONAL_ENVELOPE_KEYS` | 常量 | 同名 | |

### 4.2 `root_permission_queue.py` → `root_permission_queue.go`

| Python 符号 | Go 对应 |
|------------|---------|
| `MAX_ROOT_PERMISSION_CARDS` (64) | 同名常量 |
| `RootPermissionCardState` | 字符串常量 3 值 |
| `RootPermissionQueueError` | `RootPermissionQueueError` struct |
| `RootPermissionCard` | 同名 struct |
| `RootPermissionSnapshot` | 同名 struct |
| `RootPermissionAnswer` | 同名 struct |
| `RootPermissionClaim` | 同名 struct |
| `RootPermissionQueue.__init__` | `NewRootPermissionQueue` |
| `begin` | `Begin` |
| `mark_pending` | `MarkPending` |
| `pending_for_call` | `PendingForCall` |
| `reserve_answer` | `ReserveAnswer` |
| `release_answer` | `ReleaseAnswer` |
| `claim_answer_for_call` | `ClaimAnswerForCall` |
| `consume_accepted_answer_if_quarantined` | `ConsumeAcceptedAnswerIfQuarantined` |
| `reconcile` | `Reconcile` |
| `get` | `Get` |
| `finish` | `Finish` |
| `finish_active` | `FinishActive` |
| `has_live` | `HasLive` |
| `snapshot_scope` | `SnapshotScope` |
| `begin_cutover` | `BeginCutover` |
| `cutover_continuation_scope` | `CutoverContinuationScope` |
| `cancel_snapshot` | `CancelSnapshot` |
| `discard_continuation` | `DiscardContinuation` |
| `quarantine` | `Quarantine` |
| `raise_if_quarantined` | `RaiseIfQuarantined` |
| `_raise_if_quarantined_locked` | `raiseIfQuarantinedLocked` |
| `_exact_card_locked` | `exactCardLocked` |
| `_remove_locked` | `removeLocked` |
| `_active_key` | `activeKey` |
| `_text` | `text` |
| `_normalize_answer` | `normalizeAnswer` |
| `_interaction` | `interaction` |
| `_request_key` | `requestKey` |

**注意**：`_locked` 后缀的私有方法保留，因为 Python 本身就有这个分层（规则 11.2 允许）。

### 4.3 `root_permission_queue_rail.py` → `root_permission_queue_rail.go`

见 Python 文件符号清单（常量 9 个 + 类与函数），逐条对应，命名仅做 `snake_case → CamelCase`。

### 4.4 `root_context_rail.py` → `root_context_rail.go`

同 §4.3。

### 4.5 `root_ask_user.py` → `root_ask_user.go`

| Python 符号 | Go 对应 |
|------------|---------|
| `ASK_USER_TOOL_NAME` | 同名常量 `"ask_user"` |
| `ASK_USER_CONTINUATION_METADATA_KEY` | 同名 |
| `ASK_USER_RESUME_DTO_KEY` | 同名 |
| `_MAX_ANSWERS` (8) | 同名 |
| `AskUserQuestionReference` | 同名 struct |
| `RootAskUserContinuation` | 同名 struct |
| `RootAskUserResume` | 同名 struct |
| `put_ask_user_resume_in_inputs` | `PutAskUserResumeInInputs` |
| `build_ask_user_metadata` | `BuildAskUserMetadata` |
| `ask_user_continuation` | `AskUserContinuation` |
| `prepare_ask_user_resume` | `PrepareAskUserResume` |
| `apply_ask_user_resume` | `ApplyAskUserResume` |
| `_questions` | `questions` |
| `_options` | `options` |
| `_answer_values` | `answerValues` |

---

## 五、ContextVar 三态语义设计

### 5.1 Python 原语义

```python
_UNBOUND = object()   # 哨兵
_ROOT_BINDING: ContextVar[Binding | None | object] = ContextVar(
    "jiuwenswarm_root_permission_request", default=_UNBOUND,
)
```

三态：

| 状态 | 表示 | `current_root_permission_queue()` 行为 |
|------|------|--------------------------------------|
| `_UNBOUND` | 从未绑定 | 抛 `root_permission_request_unbound` |
| `None` | 明确禁止（`enabled=False`） | 返回 `None` |
| `Binding` | 已绑定 | 返回 `binding.queue` |

### 5.2 Go 方案：哨兵值类型

```go
// unboundMarker 表示"从未绑定"，与"绑定 nil"区分。
// Python: _UNBOUND = object()
type unboundMarker struct{}

var unbound = unboundMarker{}

type rootBindingKey struct{}

type rootPermissionRequestBinding struct {
    rootSessionID string
    requestID     string
    queue         *RootPermissionQueue
}

// BindRootPermissionRequest 绑定，返回可用于恢复的 token。
// Python: bind_root_permission_request(...) -> Token
func BindRootPermissionRequest(ctx context.Context, b *rootPermissionRequestBinding) (context.Context, func()) {
    prev := ctx
    holder := &bindingHolder{}   // 三态承载
    if b != nil {
        holder.value, holder.bound = b, true
    } else {
        holder.bound = true      // 绑定 nil
    }
    newCtx := context.WithValue(ctx, rootBindingKey{}, holder)
    return newCtx, func() { _ = prev }   // reset = 丢弃派生 ctx
}

// CurrentRootPermissionQueue 取队列，三态语义。
// Python: current_root_permission_queue()
func CurrentRootPermissionQueue(ctx context.Context) (*RootPermissionQueue, error) {
    holder, ok := ctx.Value(rootBindingKey{}).(*bindingHolder)
    if !ok {
        return nil, &RootPermissionQueueError{Code: "root_permission_request_unbound"}
    }
    if !holder.bound || holder.value == nil {
        return nil, nil
    }
    return holder.value.queue, nil
}

// OptionalRootPermissionQueue 无绑定或绑定 nil 都返回 nil，不报错。
// Python: optional_root_permission_queue()
func OptionalRootPermissionQueue(ctx context.Context) *RootPermissionQueue {
    holder, ok := ctx.Value(rootBindingKey{}).(*bindingHolder)
    if !ok || !holder.bound || holder.value == nil {
        return nil
    }
    return holder.value.queue
}
```

### 5.3 偏差登记

| 项 | Python | Go | 性质 | 理由 | 回收条件 |
|----|--------|----|------|------|---------|
| token reset | `ContextVar.reset(token)` 精确回滚 | 返回 no-op func，靠 ctx 派生天然隔离 | 语言差异 | Go context 不可变，丢弃派生 ctx 即等价于 reset | 无（Go 无更优方案） |
| 三态承载 | 单值 `_UNBOUND` | `bindingHolder{bound bool, value *binding}` | 实现等价 | Go 无单值多态，需双字段表达"绑定 nil" vs "未绑定" | 无 |

**这两条偏差需你在实施前确认。**

---

## 六、InterruptRequest 扩字段方案

### 6.1 Python 原定义（`single_agent/interrupt/response.py:51-58`）

```python
class InterruptRequest(BaseModel):
    message: str = ""
    payload_schema: dict = Field(default_factory=dict)
    auto_confirm_key: str = ""
    ui_options: list[dict] | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)          # 缺
    interrupt_id: str = ""                                          # 缺
    interrupt_kind: str = ""                                        # 缺
    interrupt_owner: str = ""                                       # 缺
```

### 6.2 `InterruptKind` 七值

```python
UNDECLARED = ""
PERMISSION = "permission"
CONFIRM = "confirm"
ASK_USER = "ask_user"
SKILL_EVOLUTION_APPROVAL = "skill_evolution_approval"
TEAM_TOOL_APPROVAL = "team_tool_approval"
CLIENT_TOOL = "client_tool"
```

### 6.3 Go 改动

```go
// internal/agentcore/single_agent/schema/response.go

type InterruptRequest struct {
    Message        string
    PayloadSchema  map[string]any
    AutoConfirmKey string
    UIOptions      []map[string]any
    Metadata       map[string]any   // 新增
    InterruptID    string           // 新增
    InterruptKind  string           // 新增
    InterruptOwner string           // 新增
}

// InterruptKind 中断类型常量
const (
    InterruptKindUndeclared            = ""
    InterruptKindPermission            = "permission"
    InterruptKindConfirm               = "confirm"
    InterruptKindAskUser               = "ask_user"
    InterruptKindSkillEvolutionApproval = "skill_evolution_approval"
    InterruptKindTeamToolApproval      = "team_tool_approval"
    InterruptKindClientTool            = "client_tool"
)

// InterruptKindOf 返回 kind，兼容旧数据把 kind 存在 metadata 里的情况。
// Python: interrupt_kind_of(request)
func InterruptKindOf(req *InterruptRequest) string

// ClientToolInterruptOwner 客户端工具中断的 owner 常量。
// Python: CLIENT_TOOL_INTERRUPT_OWNER = "openjiuwen.client_tool"
const ClientToolInterruptOwner = "openjiuwen.client_tool"
```

### 6.4 波及面（必须同改动适配完）

| 消费方 | 位置 | 改动 |
|--------|------|------|
| `InterruptRequester` 接口 | `schema/response.go` | 新接口方法是否暴露新字段待定 |
| `handler.go` 答案归属 | `single_agent/interrupt/handler.go` | 需按 `interrupt_owner` 路由（当前按 innerID） |
| `interrupt_base.go` | `harness/rails/interrupt/` | `stamp_interrupt` 等价物 |
| `base_security_rail.go` | `harness/rails/security/` | 抛中断时盖 owner/kind |
| `tool_security_rail.go` | 同上 | 同上 |
| `ask_user_rail.go` | `harness/rails/interrupt/` | 同上 |
| `team_harness.go` | `agent_teams/harness/` | `InterruptRequests` 遍历 |

**`handler.go` 的答案路由改动是本批最重的一处**：当前 `saveAutoConfirmFromState` 按 `interactiveInput.UserInputs` 的 innerID 遍历，Python 的 `_attribute_answers` 生成 `{tool_call_id: {owner: InterruptAnswer}}` 并支持 `carried` / `unaddressed` 标记。

---

## 七、本批不做、登记为后续批次

### 7.1 已对齐三文件的偏差（不修）

| 文件 | 偏差 | 建议批次 |
|------|------|---------|
| `permissions_persist.go` | 缺 5 个函数 + 1 个异常类 | 批次 5（护栏与审计） |
| `permissions_config_rpc.go` | Go 多出 19 个函数，Python 只 1 个入口 | 批次 1 完成后单独重构 |
| `owner_scopes.go` | 缺 3 个符号；3 个函数命名被改 | 批次 1 完成后单独重构 |

### 7.2 `PermissionEngine.llm` / `modelName` 字段

**决定：保留并挂 ⤵️。**

理由：
- Python 的 `PermissionEngine` 同样持有 `_llm` 和 `_model_name`（`harness/security/core.py`）
- 规则 11 要求结构忠实，删掉字段反而造成结构偏离
- 规则 8 禁止的是"兼容壳"（为切换新实现而留的旧路径），这两个字段不是兼容壳，是对齐 Python 结构的占位

⤵️ 标记：`llm` / `modelName` 的消费方为 `auto_reviewer`（批次 4），在此之前只赋值不读取。

### 7.3 root 五文件的三个导出函数

`HasValidAskUserClarification` / `OriginalUserIntent` / `FileDeliveryConstraintText` 在 Go 侧无消费者。

**决定：照常实现（结构忠实），挂 ⤵️ 到批次 4。**

理由：它们是 `root_context.py` 的导出符号，Python 侧被 `auto_reviewer` 和 `before_tool` 使用。不实现会造成方法清单不完整，违反规则 11.4 的对照要求。

---

## 八、计划表登记

### 8.1 本批（root 五文件）

在 `IMPLEMENTATION_PLAN.md` 领域九新增一行，回填点在 9.19-23 Rails 之后。

### 8.2 后续批次（⤴️ 回填来源）

按依赖顺序登记四批，写入领域九新增子节。

---

## 九、实施计划

### 9.1 提交分层（按依赖顺序）

| # | commit | 内容 | 前置 |
|---|--------|------|------|
| 1 | `feat(interrupt): InterruptRequest 扩展 Metadata/ID/Kind/Owner + InterruptKind 七值` | `schema/response.go` + 7 处消费方适配 | — |
| 2 | `feat(interrupt): 答案按 interrupt_owner 归属` | `handler.go` 改造 | 1 |
| 3 | `feat(permissions): ToolInvocationKeyV1 + normalize_tool_invocation_text` | `tool_invocation_key.go` | — |
| 4 | `feat(rails): ask_user_contract MAX_STRUCTURED_QUESTIONS` | 常量 | — |
| 5 | `feat(schema): TOOL_PERMISSION_REQUEST_ID / SKILLS_REBUILD_SILENT` | `permission.go` | — |
| 6 | `feat(tools): CommandExecutionBinding + bind/reset` | 新小包 | — |
| 7 | `feat(permissions): protected_paths 常量` | 常量 | — |
| 8 | `feat(permissions): root_context 数据结构与 ContextVar` | `root_context.go` | 3,4,5 |
| 9 | `feat(permissions): root_permission_queue 状态机` | `root_permission_queue.go` | 3,8 |
| 10 | `feat(permissions): root_ask_user 反问续接` | `root_ask_user.go` | 4,8 |
| 11 | `feat(permissions): root_permission_queue_rail 身份 rail` | 同 | 3,8,9 |
| 12 | `feat(permissions): root_context_rail 上下文绑定 rail` | 同 | 6,8,9,10,11 |
| 13 | `docs(permissions): doc.go 更新 + ⤵️ 缺口标注` | doc.go | 全部 |

### 9.2 验收标准

| 项 | 标准 |
|----|------|
| 单元测试 | 五个文件 + 8 项前置件各自覆盖率 ≥ 85% |
| 队列状态机 | `active` / `pending` / `resuming` 三种状态转换全部覆盖，含非法转换报错 |
| 隔离 | `quarantine` 触发后所有操作报错的路径覆盖 |
| 上下文 | 三态（未绑定 / 绑定 nil / 绑定值）各一例 |
| 意图窗口 | 追加澄清、20 轮上限、48000 字符上限、超限 fail-closed 各一例 |
| 构建 | `go build ./...` / `go vet ./...` / golangci-lint 通过 |
| 全量回归 | `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL |
| **不要求** | 端到端行为验证（无消费方，无法验证） |

### 9.3 删除清单

本批为纯新增，无删除项。但需核对：

- 若 `tool_permission_context` 相关内容已在 `internal/swarm/schema/permission.go` 部分实现，新增 `TOOL_PERMISSION_REQUEST_ID` 时必须加到同一文件，不得新建平行文件
- 若 `CommandExecutionBinding` 已有等价实现，按规则 8 删除旧实现

---

## 十、已知偏差汇总（需确认）

| # | Python | Go | 性质 | 理由 | 回收条件 |
|---|--------|----|------|------|---------|
| D1 | `ContextVar.set() → Token`，`reset(token)` 精确回滚 | 返回 no-op reset func，靠 ctx 派生隔离 | 语言差异 | Go context 不可变 | 无更好方案 |
| D2 | `_UNBOUND` 单值哨兵 | `bindingHolder{bound, value}` 双字段 | 实现细节 | Go 无单值多态 | 无 |
| D3 | `deepcopy(request)` 冻结中断快照 | 手写 clone 函数 | 语言差异 | Go 无内建 deepcopy | 无 |
| D4 | dataclass 自动 `__eq__` | 手写 `Equal` 方法或 `reflect.DeepEqual` | 语言差异 | Go 无自动生成 | 无 |
| D5 | `zip(..., strict=True)` | 手写长度校验 | 语言差异 | Go 无 zip | 无 |
| D6 | 模块级函数（`root_intent_turn_text` 等） | 包级函数 | 无偏差 | 规则 11.1-3 已要求 | — |
| D7 | `@dataclass(frozen=True, slots=True)` | struct + 私有字段 + 只读方法 | 语言差异 | Go 无 frozen | 无 |

---

## 十一、验收自检

- [ ] 与 Python 逐文件方法清单对照完成，产物见 §四
- [ ] 无「Go 侧多出来的方法」未说明
- [ ] 无「Go 侧少了的方法」未说明
- [ ] 命名全部 `snake_case → CamelCase`，无自行改名
- [ ] 层次全部对应，无上提/下沉
- [ ] 步骤顺序对应，无条件分支拍平
- [ ] 偏差全部登记并经确认
