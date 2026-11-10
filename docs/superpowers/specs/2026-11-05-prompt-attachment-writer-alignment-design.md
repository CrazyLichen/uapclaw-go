# 提示词附件投递对齐设计（ContextWriter + 9 处 rail 统一）

> 日期：2026-11-05
> 触发：2026-11-04 晚审查列出的 7 项偏差中的 **第 4 项**（动态内容改走附件后，断言与调用方没跟上 / 投递失败静默丢弃）与 **第 7 项**（改动行覆盖率门禁用行级豁免兜底，且豁免区间已漂移）。
> 上游基线：`/home/opensource/agent-core`（`openjiuwen`，`openjiuwen/harness/prompts/prompt_attachment_manager.py` 923 行）+ `/home/opensource/jiuwenswarm-develop`（团队 rail 侧）。
> 关联：`docs/superpowers/specs/2026-11-04-prompt-attachment-foundation-design.md`（S1 地基）、`2026-11-04-s2-rail-and-mode-design.md`（S2）。

---

## 1. 问题：Go 缺 `PromptAttachmentContextWriter`，9 处 rail 各写一套

Python 侧所有 rail 的附件投递都是同两行：

```python
writer = self.attachment_manager.bind_context(ctx)      # 构造期解析会话，只此一次
try:
    await writer.add_section(section=..., content=..., kind=..., source=..., priority=..., ...)
except ValueError as exc:
    logger.warning("... skip ... attachment: %s", exc)
```

`bind_context` 返回 `PromptAttachmentContextWriter`（`prompt_attachment_manager.py:191-265`），它**不增加任何能力**，只做三件事：

1. 构造期调 `_resolve_session_id_from_context(ctx)`（`:141-158`）解析一次会话：`ctx.session.get_session_id()` → `ctx.session.session_id` → `ctx.inputs["session_id"]/["_session_id"]` → `ctx.extra[...]` → `None`；
2. `add_section` / `add_from_prompt_section` / `clear_section` 三个方法**原样转调** `Manager` 对应方法，只是把 `session_id` 换成 `self._require_session_id()`；
3. `_require_session_id()`（`:261-264`）在会话为空时 `raise ValueError`——所以 rail 侧必须 `except ValueError`。**"缺会话"是在"用"的时候才失败，不是构造时失败。**

Go 侧没有这个类型，会话解析责任被摊到每个 rail，导致三类问题：

| 类别 | 表现 |
| --- | --- |
| 静默丢弃 | `Deliver/DeliverText` 返回 `bool`，调用方丢弃返回值；`sessionID == ""` 时既不投递也不告警 |
| 契约反了 | `external_memory_rail` 在投递失败时回落 `systemPromptBuilder.AddSection`——Python 两个记忆 rail 都**不回落**，只有 `AgentModeRail` 允许 |
| 会话解析各写一份 | `coding_memory_rail`/`external_memory_rail` 只取 `cbc.Session()`；`skill_use_rail`/`task_completion`/`team_policy` 手拼 `sessionID` 传 `Manager.AddSection`；`progressive` 另造 `managerAdapter` 窄接口；`agent_mode` 自写 `attachmentManager()` |

## 2. 现状盘点（HEAD `b07aa4a3a`）

9 处 rail 与附件的关系，按"是否已投递"和"调用姿势"两维：

| # | rail | 投递现状 | 调用姿势 | 本轮动作 |
| --- | --- | --- | --- | --- |
| 1 | `harness/rails/agent_mode.go` | ✅ `mode_instructions`（`:267` 清 / `:298` 挂） | 自有 `attachmentManager()`（`:249`）+ `mgr.ClearSection/AddFromPromptSection` | 存字段 + `BindContext` |
| 2 | `harness/rails/memory/coding_memory_rail.go` | ✅ `coding_memory_context`（`:398` / `:412` 清） | `hprompts.DeliverText` / `ClearDeliveredSection`，返回值丢弃 | `BindContext` + 告警 |
| 3 | `harness/rails/memory/external_memory_rail.go` | ✅ `external_memory_prefetch`（`:695`） | `hprompts.Deliver` + **`AddSection` 回落**（`:696`） | `BindContext`，**删回落** |
| 4 | `harness/rails/skills/skill_use_rail.go` | ✅ `skills.runtime_changes`（`:969` / `:964` 清） | `mgr.AddSection(sessionID, ...)` 手拼 | `BindContext` |
| 5 | `harness/rails/task_completion.go` | ✅ `goal_protocol`（`:606` 挂 / `:618` 清） | `mgr.ClearSection` 手拼 | `BindContext` |
| 6 | `harness/rails/progressive.go` | ✅ `progressive_deferred_tools`（`:274` → `writeCatalogAttachment`） | 自造 `managerAdapter`（`:82`）+ `attachmentManager` 窄接口（`:72`）+ `Find`/`HasHistorySnapshot` | 去窄接口，直存 `*prompts.Manager` |
| 7 | `agent_teams/rails/team_policy_rail.go` | ✅ `team.context`（`:442`） | `hprompts.DeliverText` | `BindContext` |
| 8 | `agent_teams/rails/team_plan_mode_rail.go` | ❌ 只有 `AddSection`（`:156`）/`RemoveSection`（`:87`/`:111-121`） | 无 | **补附件投递** + 回落 |
| 9 | `harness/rails/context_engineer/context_assemble_rail.go` | ❌ `context.heartbeat` 走 system（`:170-176`） | 无 | **改投附件** + 补 clear |

## 3. 设计

### 3.1 新增 `ContextWriter`（`internal/agentcore/harness/prompts/attachment_writer.go`）

```go
// ErrSessionRequired 附件写入缺少会话标识。
// Python: raise ValueError("prompt attachment context requires session_id")（:261-264）
var ErrSessionRequired = errors.New("prompt attachment context requires session_id")

// ContextWriter 已绑定会话的附件写入器。
// Python: PromptAttachmentContextWriter（prompt_attachment_manager.py:191-265）
type ContextWriter struct {
    manager   *Manager
    sessionID string            // 构造期解析一次
}

func (m *Manager) BindContext(cbc *interfaces.AgentCallbackContext) *ContextWriter   // Python: :276-279，不报错
func (w *ContextWriter) SessionID() string                                           // Python: writer.session_id
func (w *ContextWriter) AddSection(section, content string, kind AttachmentKind, source string, opts ...AttachmentOption) (*Attachment, error)
func (w *ContextWriter) AddFromPromptSection(p *saprompt.PromptSection, kind AttachmentKind, source string, opts ...AttachmentOption) (*Attachment, error)
func (w *ContextWriter) ClearSection(section string) (int, error)
func resolveSessionIDFromContext(cbc *interfaces.AgentCallbackContext) string         // Python: _resolve_session_id_from_context（:141-158）
```

三个方法在 `sessionID == ""` 时返回 `ErrSessionRequired`，其余逻辑逐字转调 `Manager`。`AddFromPromptSection` 保持 Python 的两处短路：`p == nil` → `(nil, nil)`；渲染后空白 → `(nil, nil)`（`:238-244`）。渲染用的 `Render(language)` 与 `Priority` 覆盖顺序同 Python。

声明顺序按规范 2：结构体 → 全局变量 → 导出函数 → 非导出函数。

**`resolveSessionIDFromContext` 的回落链**（对齐 `:141-158`）：

1. `cbc.Session().GetSessionID()`（非空即返回）
2. `InvokeInputs.Extra["session_id"]` → `["_session_id"]`
3. `cbc.Extra()["session_id"]` → `["_session_id"]`
4. 空串

### 3.2 9 处 rail 统一形状

每处改成 Python 的两句半：

```go
// Init 里取一次（Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)）
r.attachmentManager = hprompts.ManagerFor(agent)

// 用的时候
if r.attachmentManager == nil { /* 按 3.3 的差异表跳过或回退 */ }
writer := r.attachmentManager.BindContext(cbc)
if _, err := writer.AddSection(...); err != nil {
    logger.Warn(logComponent).Err(err).Str("event_type", "...").Msg("... skip ... attachment")
}
```

`ManagerFor` 保留：它是 Go 里 `getattr(agent, "prompt_attachment_manager", None)` 的等价物（`BaseAgent` 接口不能声明 `PromptAttachments()`，否则 `single_agent/interfaces` ↔ `harness/prompts` 成环，已用 `go list -deps` 双向验证）。

### 3.3 各 rail 的 manager 缺失 / 会话缺失行为（逐条对齐 Python）

| rail | manager 缺失 | 会话缺失 / 写入失败 | Python 依据 |
| --- | --- | --- | --- |
| `agent_mode` | 回落 `systemPromptBuilder.AddSection` | 回落 `AddSection` + warn | `agent_mode_rail.py:405-429`（唯一允许回落的 rail） |
| `coding_memory` | **warn** + return（Python 静默，Go 增强，见 §7 偏差 K1） | warn + return，**不回落** | `coding_memory_rail.py:364-392` |
| `external_memory` | warn + return，**不回落** | warn，**不回落** | `external_memory_rail.py:181-208` |
| `skill_use` | 静默 return | 会话空 return；内容空 → clear | `skill_use_rail.py:752-781` |
| `task_completion` | `want_goal` 时 warn + return，否则静默 | warn + return | `task_completion_rail.py:341-370` |
| `progressive` | 静默 return | 会话空 return | `progressive_tool_rail.py:1141-1160` |
| `team_policy` | return `false`（回落到 in-history 消息通道，Python 同） | warn + `false` | `team_policy_rail.py:407-452` |
| `team_plan_mode` | 回落 `AddSection` | warn + `AddSection` | `team_plan_mode_rail.py:74-117` |
| `context_assemble` | **warn + 继续 system 分支**（heartbeat 不投附件） | warn，heartbeat 不投 | `context_assemble_rail.py:105-170` |

### 3.4 `progressive` 去窄接口

| 现状 | 改为 | Python |
| --- | --- | --- |
| `attachmentManager` 接口（`:72-79`）：`AddSection` / `Find` / `HasHistorySnapshot` | **删除** | — |
| `managerAdapter`（`:82-88`）+ 3 个方法（`:960`/`:965`/`:970`） | **删除** | — |
| `r.attachments(cbc)`（`:849-862`）每次包一层 | `r.attachmentManager *prompts.Manager`，`Init` 取一次 | `init` 存字段 |
| `manager.Find(sessionID, section, source)` | `r.attachmentManager.ListByFilter(...)` 取首条 | `_get_deferred_tool_attachment`（`:1141` 附近） |
| `manager.HasHistorySnapshot(sessionID)` | `r.attachmentManager.HasHistorySnapshot(cbc.ModelContext(), sessionID)` | `has_history_snapshot(context, session_id)`（`:1031`） |
| `writeCatalogAttachment(manager, sessionID, ...)` | `writeCatalogAttachment(w *prompts.ContextWriter, ...)` | `add_section(session_id=writer.session_id, ...)` |

注：`HasHistorySnapshot` 的 context 从「构造期存」变「调用期传」，与 Python 一致；`cbc.ModelContext()` 为 nil 时 Python 跳过判定（`has_snapshot=False`），Go 侧现有 `Manager.HasHistorySnapshot` 在 history 为 nil 时返回 false，行为等价。

### 3.5 `syncDeferredToolAttachment` 的 4 条修复（metadata / 指纹基线）

顺带核对 `progressive_deferred_tools` 附件 metadata，**键名与取值全部与 Python 一致**（`catalog_version`/`catalog_mode`/`catalog_language`/`delta_count`/`catalog_tools`/`catalog_fingerprints`/`registry_revision`、`priority=75`、`kind=TOOL`、`content_kind=text/markdown`、section/source 字面值），但有 4 处恢复逻辑不等价：

| # | Python | Go 现状 | 后果 | 修法 |
| --- | --- | --- | --- | --- |
| D1 | `if attachment_delta_count: delta_count = ...`（`:1027-1028`，**非零才覆盖**） | `if value, ok := ...(int); ok { deltaCount = value }`（`:1019-1021`） | 附件 `delta_count=0`（刚发过 snapshot）把 session 计数清 0 → "连续 10 次 delta 强制 snapshot"永不触发 | 加 `&& value != 0` |
| D2 | `if attachment_fingerprints is not None: previous_fingerprints = attachment_fingerprints`（`:1000-1001`） | `if fingerprints, ok := ...; ok { _ = fingerprints }`（`:1011-1014`） | 热重载后指纹基线丢失 | 真正赋值（删掉 `_ =`） |
| D3 | `baseline_fingerprints = previous if initialized else initial`（`:1042-1044`）+ `updated` 判定含指纹比较（`:1054-1055`） | `baseline` 只有 description，`updated` 只比 description（`:1036-1040`） | **schema 指纹变了但 description 没变的工具不被判 updated** → 模型拿到过期卡片 | 新增 `baselineFingerprints` 并参与 `updated` 判定 |
| D4 | `initial_fingerprints` 状态键（`:44`、`:942-946`），缺失时用当前指纹播种 | 无该状态键，只有 `fingerprints` | 首版基线记忆缺失（D3 的配套） | 新增 `deferredToolCatalogInitialFingerprintsStateKey`，`saveCatalogState` 同时写两键 |

同时按 Python `:45` 把 Go 的裸字符串 `"fingerprints"`（`:1125`）常量化为 `deferredToolCatalogFingerprintsStateKey`。

## 4. 偏差 7：用真集成覆盖替换行级豁免

`.integration-coverage-exempt:190-194` 是为「`mode` 缺省 → `agent`」分支申请的，注释写 `agent_manager.go#L802-L815`。但该区间现在是 `AgentManager.GetChatAgent`，真正的缺省分支早已搬到 `runtime/request.go:196-198`（`if mode == "" { mode = "agent" }`）。**该豁免的没豁免到，且以后谁改 `GetChatAgent` 都会被静默放过。**

修法（不靠豁免）：

- 新增 `tests/integration/swarm/server/command_context_default_mode_test.go`，用现成 `testhelpers.NewPlanEnv`（内部 `NewServerEnv` + 真 `UapClaw` 工厂 + MockLLM）；
- 发 `command.context` 且 `params` **不带 mode**：`handleMessage`（`handle_message.go:147`）→ `handleCommandContext`（`handle_command.go:212`）→ `ApplyResolvedModeToRequest(request, "")` → 命中 `mode==""` → `AgentManager.GetAgent` 真装配 → 真 adapter；
- 断言：响应 `ok=true`、payload 含上下文占用字段、同会话 `plan_status` 为 `normal`（证明缺省走 `agent` 而非 `plan`）；
- **先** `-coverpkg` 实跑确认 `runtime/request.go` 那三行命中，**再**删豁免条目（不做"先删再看 CI"）。

附带收益：`/context`、`/compact` 这两条 slash 命令此前零集成覆盖。

## 5. 删除清单（规则 8）

| 对象 | 位置 | 原因 |
| --- | --- | --- |
| `Deliver` / `DeliverText` / `ClearDeliveredSection` | `prompts/attachment_rail.go:33/54/82` | 由 `ContextWriter` 取代，改完零调用方 |
| `RailAttachmentProvider` 之外的旧测试用例 | `prompts/attachment_rail_test.go`（3 例） | 随函数删除 |
| `attachmentManager` 窄接口 | `rails/progressive.go:72-79` | 去窄接口 |
| `managerAdapter` + 3 方法 | `rails/progressive.go:82-88`、`:960/:965/:970` | 同上 |
| `promptAttachmentProvider`（本文件内） | `rails/progressive.go:63-68` | 统一改 `hprompts.ManagerFor` |
| `attachmentManager()` 方法 | `rails/agent_mode.go:241-247` | 与 `ManagerFor` 重复 |
| `_ = fingerprints` 丢弃行 | `rails/progressive.go:1011` | D2 修复 |
| 行级豁免条目 | `.integration-coverage-exempt:190-194` | 偏差 7 修法 |

**验收锚点**（`internal/`、`tests/`、`cmd/` 内 0 命中）：

```
grep -rn "prompts\.Deliver\|hprompts\.Deliver\|DeliverText\|ClearDeliveredSection" internal tests cmd
grep -rn "managerAdapter\|attachmentManager interface" internal
grep -rn "func (r \*AgentModeRail) attachmentManager" internal
grep -n "agent_manager.go#L802-L815" .integration-coverage-exempt
```

## 6. 文档同步（规则 1）

| 文件 | 改动 |
| --- | --- |
| `prompts/doc.go` | 文件树加 `attachment_writer.go`；`attachment_rail.go` 的描述改为「`ManagerFor`（`getattr` 等价物）」 |
| `rails/memory/doc.go` | 「在 BeforeModelCall 中注入记忆 section 到系统提示词」→「投递为历史附件」 |
| `tests/integration/swarm/server/doc.go` | 文件树加 `command_context_default_mode_test.go` |

## 7. 已知偏差（规则 9.6）

| # | 偏差 | Python 依据 | Go 实现 | 理由 | 回收条件 |
| --- | --- | --- | --- | --- | --- |
| K1 | `coding_memory_rail` 在 manager 缺失时**多一条 warn** | `coding_memory_rail.py:369-370` 静默 `return` | `logging.Warn` 后 return | Go 侧 manager 为 nil 通常意味着装配缺件（Python 侧由 DeepAgent 构造期保证非 nil），静默会让故障难定位 | 上游补同样日志时删除 |
| K2 | 会话解析用 `ManagerFor` 类型断言取 manager | `getattr(agent, "prompt_attachment_manager", None)` | `RailAttachmentProvider` + 类型断言 | `BaseAgent` 声明 `PromptAttachments()` 会让 `single_agent/interfaces` ↔ `harness/prompts` 成环（`go list -deps` 已双向验证） | 附件类型下沉到 `foundation/` 后 |
| K3 | 一个 Python 文件对应 8 个 Go 文件 | `prompt_attachment_manager.py`（923 行）单文件 | `attachment_types/digest/store/render/delivery/mutator/rail` + 新 `writer` | S1 地基时的拆法，本轮不重排 | 后续单独做文件合并 |
| K4 | `team_plan_mode_rail` 的 `mode_instructions` 与 `agent_mode_rail` 同 section 名 | 两边都是 `SectionName.MODE_INSTRUCTIONS` | 同 | 无偏差，仅记录共享 section 的语义 | — |
| K5 | ~~`WithLanguage` 用 `Metadata["render_language"]` 传渲染语言~~ **已消除** | `add_from_prompt_section(..., language: str = "cn")`（`:231`）是显式关键字参数 | `ContextWriter.AddFromPromptSection(section, kind, source, language, opts...)`，`language` 为形参 | 原实现把语言塞进选项再剥离，同一语义两套通道且 `AddSection` 路径会泄漏 metadata 键 | 已按上游改回形参，`WithLanguage`/`render_language` 零残留 |

## 7.1 附件快照基线的时序约束（实现期核实）

`Sync` 在 ReAct 内层 `before_model_call` 里执行（`react_model_call.go:156`），晚于 rail 的
`BeforeModelCall`。首轮目录无变化时 rail 不写附件，`Sync` 遇到空集合直接返回
（`attachment_delivery.go:48-50`，与上游 `sync_to_context` 同为「空集不建快照」），
于是**首轮不会产生历史快照**；第二轮 `has_history_snapshot` 仍为 false，按上游语义
补发一次全量 snapshot，第三轮起才稳定。

因此「同会话复用不重复推送」的验收前提是**先有快照基线**：
`tests/integration/agentcore/harness/rails/progressive/progressive_rail_test.go` 的
`TestProgressive_SessionReuseNoDuplicate` 在首轮后手动注册一个无关 section 触发首次同步，
再断言第二轮 catalog 附件为空——与上游 `test_deferred_catalog_does_not_repeat_after_session_state_reset`
（`tests/unit_tests/harness/test_progressive_tool_rail.py:285`）的写法一致。

## 8. 测试矩阵

### 8.1 单元

| 文件 | 断言 |
| --- | --- |
| `prompts/attachment_writer_test.go` | `BindContext` 拿 session；`sessionID==""` 时三个方法返回 `ErrSessionRequired`；`resolveSessionIDFromContext` 四级回落（session → inputs → extra → 空）；`AddFromPromptSection` 的 nil / 空白短路；`SessionID()` |
| `rails/progressive_test.go`（改） | 去掉 fake managerAdapter，构造真 `prompts.NewManager()`；`HasHistorySnapshot` 走 `cbc.ModelContext()`；`TestSyncDeferredToolAttachment_DeltaCount零值不覆盖`、`_指纹恢复参与基线`、`_初始指纹播种` |
| `rails/memory/*_test.go` | 无 manager → 不写 system prompt 且不 panic；无 session → 投递跳过、附件数 0 |
| `rails/agent_mode_test.go`（改） | manager 缺失 → 回落 `AddSection`；写入失败 → 回落 + warn |

### 8.2 集成

| 用例 | 覆盖 | 断言 |
| --- | --- | --- |
| `server/command_context_default_mode_test.go` | 偏差 7 | 不带 mode 的 `command.context` 全链路成功；`plan_status` 为 `normal`；覆盖 `runtime/request.go:196-198` |
| `agentcore/attachment/*`（现有） | 回归 | `BindContext` 改造后快照/增量/delta 渲染不变 |
| `adapter/attachment_mode/*`（现有） | 回归 | `mode_instructions` 挂/清经 Writer 后行为不变 |

审计：`go run ./cmd/integration-audit -verify`（新用例放在已通过审计的 `tests/integration/swarm/server/`，不新开目录）。

## 9. 实施顺序

1. `attachment_writer.go` + 单测（不接任何 rail，先可编译）
2. 9 处 rail 逐处改（每处单独提交，可独立回滚）
3. `progressive` 去窄接口 + D1~D4 修复 + 3 个单测
4. 偏差 7 的集成用例 + 验证覆盖 + 删豁免
5. 三个 doc.go 同步 + 删除清单验收（grep 锚点）

每步 `go build -tags sqlite_fts5 ./...`、`go vet`、受影响单测、受影响集成套件全绿后再进下一步。
