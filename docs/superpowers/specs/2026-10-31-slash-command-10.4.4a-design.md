# 10.4.4a Gateway Slash 命令处理 —— 对账收口 + command.model 全量实现

> 状态：待评审
> 日期：2026-10-31
> 范围：IMPLEMENTATION_PLAN.md 10.4.4 拆分为 10.4.4a（本节交付）与 10.4.4b（`/sandbox` 命令面，⤵️ 10.3.24）
> Python 参考：
> - `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py`（337 行）
> - `jiuwenswarm/gateway/message_handler/message_handler.py` L347-379、L575-1145
> - `jiuwenswarm/server/agent_ws_server.py` L2277-2352（`_handle_command_model`）
> 决策记录：用户拍板「C1 拆章 + 已有命令逐条对齐 Python，不简化」

## 一、背景与目标

10.4.4 的 Gateway 侧实现是随 11.7+11.13 集成点③（`e4a87673`）搭便车落地的，随后 `24aff49b` 审查修复过一轮。当前状态不是「未实现」，而是：

1. 解析器 `command_parser/slash_command.go`（552 行）覆盖率 94.4%，功能完整；
2. 分发器 `message_handler/slash_cmd.go`（579 行）13 个 action 全部分支在场，但所在包覆盖率仅 **56.8%**，其中三个真正发 E2A 的函数几乎裸奔（`skillsSlashNotice` 11.1%、`branchSlashNotice` 21.1%、`rewindSlashNotice` 0.0%）；
3. 与 Python 逐行比对后存在 **9 处行为差异或简化**，其中 1 处是真实的时序简化（`/branch` 异步取消）；
4. `command.model` 是 stub（`handle_command.go:246` 只回 `{"ok": true}`），而 `command.sandbox` 是 `notImplementedResponse`；

本节目标：

- **逐条对账**：把 9 处差异按 Python 行为修正，不做「差不多就行」的等价替代；
- **补齐测试**：`message_handler` 包覆盖率 56.8% → ≥85%，重点是三个 E2A 函数；
- **全量实现 `/model`**：`add_model` / `switch_model` / 默认查询三分支 1:1 移植，含 `API_KEY` 掩码与 `os.environ` 类型严格性；
- **拆章落地**：`/sandbox` 命令面挂到 10.3.24，本节不碰（理由见第三节）。

## 二、流程位置与作用（本节固定语义，后续章节不得改动）

### 2.1 位置

```
Channel(feishu/xiaoyi/dingtalk/whatsapp/wecom/wechat)
  → MessageHandler.userMessages 队列
  → forwardLoop（message_handler/forward_loop.go:90）
      步骤1  handleChannelControl(msg)   ← 10.4.4a 唯一入口（forward_loop.go:117）
             true  → return，跳过步骤 2~11
             false → 继续
      步骤2  ApplyChannelState
      步骤3  Gateway hook: UserPromptSubmit
      步骤4  CHAT_ANSWER 分支    ← 按 req_method 短路
      步骤5  CHAT_CANCEL 分支    ← 按 req_method 短路
      步骤6  Inbound Pipeline
      步骤7  resolveInboundReferences
      步骤8  prepareAgentDispatchMessage
      步骤9  before_chat_request hook
      步骤10 handleChatSend → E2A → AgentServer
```

**这是整条链路上唯一按「整行文本内容」做短路决策的点**：步骤 4/5 按协议方法短路，步骤 1 按用户文本短路。命中即终止，消息永不进入 Agent 对话链路。

### 2.2 三条职责

| 类别 | 命令 | 作用 |
|---|---|---|
| 会话生命周期 | `/new_session`、`/branch [name]`、`/rewind N`（含 `confirm`/`cancel` 两步确认） | SessionMap 身份轮转 + SessionStart hook；`session.fork` 分叉；`session.rewind` 回退（失败回落本地截断 `history.json`） |
| 模式路由 | `/mode agent\|code\|team\|agent.plan\|agent.fast\|code.plan\|code.normal\|code.team`、`/switch plan\|fast\|normal\|team` | 改写 `ChannelControlState.Mode` → 步骤 2 注入 `params.mode` → 决定 AgentServer 侧 adapter 家族与 plan/fast 子模式 |
| 只读查询 | `/skills list` | 代理 `skills.list` RPC，以 CHAT_FINAL 通知回复 |

旁路谓词 `IsControlLikeForIMBatching`：供飞书/企微的消息合并窗口判断，控制类消息不参与合并。

受控渠道白名单固定 6 个（`channel_state.go:310`，对齐 Python `_control_channel_types`）：feishu / xiaoyi / dingtalk / whatsapp / wecom / wechat。Web、ACP、TUI 渠道一律不解析。

## 三、范围界定

### 3.1 本节做

| 类别 | 内容 |
|---|---|
| 对齐修正 | 第四节 A1-A8 共 8 项对账：**A1/A2/A5/A6/A7/A8 改代码（6 项）**，A3/A4 核实等价、仅补测试（2 项） |
| 新实现 | `handleCommandModel` 从 stub 换成真实实现（第六节） |
| 测试 | 第七节 |
| 文档 | 第八节 |

### 3.2 本节不做，且已核实无需做

| 命令 | 结论 | 依据 |
|---|---|---|
| `/evolve`、`/evolve_list`、`/evolve_simplify`、`/evolve_rebuild`、`/evolve_rollback` | **已完整实现**，非「骨架」 | `server/adapter/deep_adapter_slash.go` 563 行，5 个 handler 全在，含 S-11 信号检测链、M-14 返回值修复 |
| `/skills use` | 已实现，且不属命令分发（prompt 构建期解析） | `server/runtime/build_user_prompt.go:126` |
| `/new` | 即 `/new_session`，Gateway 侧已完成 | 计划行原描述用了简写，需订正 |
| `/sandbox` | **挂起**，见 3.3 | `handle_command.go:283` 返回 NOT_IMPLEMENTED |

### 3.3 为什么 `/sandbox` 不能塞进本节

`command.sandbox` 的依赖链是三层，每层都在本节之外：

```
command.sandbox 命令面（agent_ws_server.py L2853-3510，约 660 行；另有 L247 平台守卫）
  9 个 sub：status / enable / disable / exclude.add / exclude.remove / exclude.list
            files.allow / files.deny / files.remove / files.list
  8 个子处理器 + 6 个辅助函数（host:port 解析、端口可绑定性探测、空闲端口分配、
       effective files 挂载、runtime patch 热更）
  └─ 10.3.24 Sandbox 包（计划标 ☐ 延后）
       ├─ server/sandbox/jiuwenbox_runner.py  479 行（子进程拉起/健康检查/stderr 尾部）
       ├─ common/config.py 的 sandbox runtime 段（get/update_sandbox_runtime、
       │    files 三桶策略、excluded_commands）
       ├─ AgentManager.recreate_agent（sys_operation 类型重建）
       ├─ DeepAdapter.apply_sandbox_runtime_patch（运行时热更）
       └─ jiuwenbox 守护进程本体：46 个 .py / 21,095 行
```

另有平台守卫 `_require_sandbox_supported`：全家桶仅 Linux 可用。

**结论**：这不是「换个章节顺序」能绕开的量级，而是「移植 21k 行 Python 守护进程」或「把它当外部可执行依赖」的二选一。本节点到为止：10.4.4b 只保留命令面契约，⤵️ 10.3.24。

## 四、对账结果：8 项（6 改 + 2 等价确认）

行号 Python = `message_handler/message_handler.py`，Go = `message_handler/slash_cmd.go`（除注明外）。

### A1 `/branch` 的取消改为同步（真实时序简化）

| 项 | 内容 |
|---|---|
| Python | L994 `await self._cancel_agent_work_for_session(msg, old_sid)`，`publish_interrupt_result` 取默认 `True` |
| Go 现状 | L406 `go mh.CancelAgentWorkForSession(ctx, msg, oldSID, true)`——异步 |
| 问题 | 通知在取消完成前发出，顺序与 Python 相反。与 M-05 同类问题，`/new_session`、`/mode` 当初都改同步了，`/branch` 漏改 |
| 改法 | 去掉 `go`，改同步调用。`CancelAgentWorkForSession` 内部 `SendRequest` 有超时，不会永久阻塞 |
| 风险 | 低。同步后 forwardLoop 在该消息上多停留一次 RPC 超时时长（最坏为 `SendRequest` 超时），与 `/new_session` 现状一致 |

### A2 `/branch` 补日志

| 项 | 内容 |
|---|---|
| Python | 成功路径 L1001-1004 `logger.info("/branch 完成: source=%s fork=%s title=%s")`；`except ValueError` L1005 与兜底 `except` L1010-1011 分别发通知并 `logger.exception` |
| Go 现状 | 仅在 `SendRequest` 返回 err 时记 Error，成功无日志；`resp.OK=false` 路径无日志 |
| 改法 | ① 成功分支加 `logger.Info`，字段 `source_session_id` / `fork_session_id` / `title`；② `resp.OK=false` 分支加 `logger.Warn`（`event_type=branch_slash_rejected`）；③ 保持发通知文案不变 |
| 风险 | 无 |

### A3 `/skills list` 非 dict payload：核实为结构差异，不改代码

| 项 | 内容 |
|---|---|
| Python | L918-921：payload 为 dict 则逐键透传，否则 `{"data": resp.payload}` 透传原值 |
| Go 现状 | L307-310：payload 非 nil 则逐键复制，nil 则 `{"data": nil}` |
| 核实结论 | Go 的 `schema.AgentResponse.Payload` 类型是 `map[string]any`（`schema/agent.go:64`），**非 dict payload 在 Go 侧结构上不可达**——wire 解码阶段就失败并走 `SendRequest` 错误路径。因此 `{"data": nil}` 恰好等价于 Python 的 `payload is None` 分支 |
| 改法 | **不改代码**。差异登记为 D6：AgentServer 若真返回非 dict payload，Python 透传原文、Go 走错误路径回 `获取技能列表失败：<err>`。根因是 E2A 响应 schema 在 Go 侧强类型化，改它等于改整个 wire 层，不在本节范围 |
| 风险 | 无 |
| 实施期补充 | 空 payload 经 E2A 线格式往返后是 `{}` 而非 `nil`，Python `isinstance(dict)` 同样命中并透传空 dict（不带 `data` 键）。因此 Go 的 `{"data": nil}` 分支在真实链路上不可达，保留为 Python `payload is None` 语义的占位，不为其写用例 |

### A4 `/skills list` 五种文案形态：核实等价，不改代码

| 项 | 内容 |
|---|---|
| Python 五形态 | ① payload 为 dict → 透传；② payload 非 dict → `{"data": payload}`；③ **无 err** → `"获取技能列表失败"`；④ **有 err** → `"获取技能列表失败: " + err`（ASCII 冒号）；⑤ **异常** → `"获取技能列表失败：{exc}"`（全角冒号） |
| Go 现状 | ① 透传 ✅；② 见 A3；③ `"获取技能列表失败"` ✅；④ `"获取技能列表失败: %s"` ✅；⑤ 由 L263-266 的连接守卫替代（Go 无异常机制，必须有短路），文案 `"获取技能列表失败：AgentClient 未连接"`，与 Python 异常形态同为全角冒号前缀 ✅ |
| 改法 | **不改代码**。Go 的连接守卫就是 Python ⑤ 的等价物，形态已一致；仅补测试锁定 ③④⑤ 三种文案，防止后续被「优化」成统一文案 |
| 风险 | 无 |

> 注：Go 的 `err != nil` 分支对应 Python ⑤（全角冒号），`resp.OK=false` 对应 Python ④（ASCII 冒号）。两边冒号宽度不同是 Python 原文如此，**不是笔误，不得统一**。

### A5 `/rewind` 的 preview 删除兜底

| 项 | 内容 |
|---|---|
| Python | L1093（E2A）/ L1117（fallback）：`preview = ...get("content_preview", "")`，直接 `preview[:50]`（L1124），**无兜底** |
| Go 现状 | L512-515：`preview` 为空且 `result.Content` 非空时回填 `truncRunes(result.Content, 50)` |
| 改法 | 删除兜底，只用 `result.ContentPreview` |
| 风险 | 低。兜底是「更友好」但非 Python 行为；删掉后若 sessionops 未填 ContentPreview，通知里引号内为空 |
| 实施期补充 | `/rewind`、`/branch` 的「当前无活跃会话」早退分支**不可达**：Python `_get_channel_default_state`（L247-270）与 Go `getChannelDefaultState` 在 config 未指定 `default_session_id` 时都会 `GenerateChannelSessionID`，`state.session_id` 永不为空。属 Python 同源防御性代码，不写用例 |

### A6 `/rewind` 补日志与 `rewind_context` 读取

| 项 | 内容 |
|---|---|
| Python | E2A 成功 L1104-1107 `logger.info("/rewind 完成(E2A): session=%s turn=%s context=%s")`；fallback 成功 L1128-1131 `logger.info("/rewind 完成: session=%s turn=%s remaining=%s removed=%s")`；且 L1096 读取 `rewind_context` |
| Go 现状 | 两条成功路径均无 info 日志，且未读 `rewind_context` |
| 改法 | ① E2A 成功分支读 `resp.Payload["rewind_context"]`（bool，缺省 false），记 info 带 `session_id` / `turn_index` / `rewind_context`；② fallback 成功分支记 info 带 `remaining_records` / `removed_records`；③ 保留现有 Warn 日志（`rewind_e2a_error` / `rewind_e2a_rejected`），文案不动 |
| 风险 | 无 |

### A7 `handleChannelControl` 顺序、提前返回与日志点位

| 项 | 内容 |
|---|---|
| Python | L626 渠道白名单 → L630 取 text → L634 解析 → L639 **记 info(channel, text, action)** → L645 `SKILLS_OK` 提前 return（**不建 state**）→ L652 建 state → 分发 |
| Go 现状 | L45 取 text → L51 解析 → L57 白名单 → **L65 先建 state** → 分发 → L111 末尾记 info（只有 action 整型） |
| 改法 | ① 白名单判断上移到解析之前；② `ActionSkillsOK` 在 `GetOrCreateChannelState` 之前单独 return（`switch` 前加一条 `if`）；③ 日志上移到解析成功后、分发之前，字段改为 `channel_type`（字符串）+ `text` + `action`（字符串名，非整型）；④ 末尾那条 `channel_control_handled` 日志删除 |
| 风险 | 中。②会改变 `ActionSkillsOK` 路径的副作用（不再创建 channel state）。需用测试证明 `/skills list` 不写 `channel_states`。③改日志字段会影响按 `action` 整型检索的现有排障习惯——按 Python 的 `action.value` 字符串更可用 |

### A8 `sendChannelNotice` 补 `params={}`

| 项 | 内容 |
|---|---|
| Python | L374 `Message(..., params={}, ...)`——`params` 是必填字段 |
| Go 现状 | L243-254 未设 `Params`，出 wire 时是 `null` |
| 改法 | 设 `Params: json.RawMessage("{}")` |
| 风险 | 低。下游 Channel 若对 `params` 做 nil 判断，`{}` 与 `null` 都需要能处理；`{}` 与 Python 一致，风险方向正确 |

### 已核实「一致，无需改」的点（避免重复排查）

| 点 | 结论 |
|---|---|
| `enable_streaming` | Python `Message` 默认 `True`（`common/schema/message.py:264`），Go 显式 `true`，一致 |
| `metadata` 传递 | Python `metadata=user_infos['meta_data']`，Go 复制 `msg.Metadata`，一致 |
| `/rewind` 请求 id | Python `f"rewind-{int(time*1000):x}-{token_hex(3)}"`，Go `rewind-%x-%s`，一致 |
| `skills.list` 请求 id | Python `skills_slash_{hex(ms)}_{token_hex(3)}`，Go 同形，一致 |
| `/branch` 的 `channel_id` 字段 | Python 用 `channel_type` 而非裸 `channel_id`，Go L357 已对齐 |
| `_new_session_cancel_and_notice` 同步性 | Python `await`，Go 已同步（M-05 修复），一致 |
| `_mode_change_cancel_and_notice` 的空 sid | Python 无条件调用，但内部对空 sid 早退；Go 用 `if oldSID != ""` 前置，等价 |
| 两个 E2A 的 `env` 构造 | Python `message_to_e2a_or_fallback` / `e2a_from_agent_fields`，Go 用同名函数（`e2a.MessageToE2AOrFallback` / `e2a.E2AFromAgentFields`），一致 |

## 五、有意偏差登记（不改，防止今后被当 bug 修回去）

| # | 偏差 | 理由 |
|---|---|---|
| D1 | `handleChannelControl` 保留 `Type != req` 与 `ReqMethod != chat.send` 双重前置守卫，Python 无 | Python 的三个发布方（web `publish_user_messages`、cron `cron_runtime.py:155-166`、evolution autoAnswer）均发 `type=req`；slash 文本只出现在 `query`/`content`。Go 的 `userMessages` 队列被 cron 与 evolution autoAnswer 复用，双重守卫等价且省一次解析。已核实 `autoAnswer` 的 params 不含 `query`/`content`（`evolution.go:289`） |
| D2 | `modeChangeCancelAndNotice` 把 `MODE_OK` 与 `SWITCH_OK` 两段内联合并成一个函数，Python 是两段独立代码 | 语义等价：两段都是「算出 target mode → 更新 state → old!=new 则取消+通知，否则只通知」。合并后由 `switch parsed.Action` 分叉 |
| D3 | 解析器用 `map[ModeSubcommand]string` 等查表，Python 用 `frozenset` 推导 | 等价，Go 侧另有 `ValidModeLines` 等导出变量供测试与外部只读引用 |
| D4 | `strconv.Atoi` 比 Python `int()` 严格 | Python `int("1_0")==10`、`int("٥")==5`；Go 侧返回 `rewind_bad`。**Go 更严，方向安全**，不为对齐而实现 Python 的 `int()` 全语义 |
| D5 | `/branch` 的 `resp.OK=false` 走「解析 payload.error 拼文案」而非 Python 的 `raise ValueError` | Go 无异常，等价；文案已对齐为 `分叉失败：<err>` |
| D6 | `/skills list` 非 dict payload 无法透传（见 A3） | `schema.AgentResponse.Payload` 是 `map[string]any`，非 dict 不可达；要透传必须把整个 E2A 响应 payload 改成 `any`，影响面远超本节 |
| D7 | `command.model` 的 `target`/`model` 为 JSON `null` 时 Go 得空串，Python `str(None)` 得 `"None"` | 无实际调用方会传 null；为对齐需实现 Python `str()` 语义，收益为零 |

## 六、`command.model` 全量实现

### 6.1 当前状态

`handle_command.go:246` 的 `handleCommandModel` 只解析 `action`/`model` 两个字段并返回 `{"ok": true}`，三个分支全空。

### 6.2 逐分支映射（Python `_handle_command_model` L2277-2352）

| 分支 | Python 行为 | Go 落点 |
|---|---|---|
| `action == "add_model"` | `target = str(params.get("target","")).strip()`；`logger.info("add_model: target=%s")`；`ok=true`，payload `{"type":"model_added","name":target}` | 同，`strings.TrimSpace` 对齐 `str().strip()` |
| `action == "switch_model"` | `target = str(params.get("model","")).strip()`；`env_updates = params.get("env_updates", {})`；`logger.info` 先打 target 与**掩码后**的 env（`API_KEY` → `***`） | 同。掩码必须在打日志前完成，不能打原文 |
| `switch_model` 且 `env_updates` 为空 | `ok=false`，payload `{"error":"No env_updates provided"}` | `schema.WithResponseOK(false)` + `WithPayload`。**返回 nil error**，不能用 error 返回走 `writeErrorResponse`（那会换成框架错误帧） |
| `switch_model` 正常 | 逐键 `os.environ[k] = v`；info `MODEL_NAME=%s`；`clear_config_cache()`（失败仅 debug）；`await reload_agents_config(None, env_updates)`（失败仅 debug）；`ok=true`，payload `{"current":getenv("MODEL_NAME","unknown"),"requested":target,"type":"switched","applied":true}`；info `切换完成` | `os.Setenv` 逐键 → `commmem.ClearConfigCache()`（失败 Debug）→ `s.agentManager.ReloadAgentsConfig(ctx, nil, env(map[string]string))`（失败 Debug）→ 同上 payload |
| 其它 / 缺省 action | `ok=true`，payload `{"current":getenv("MODEL_NAME","unknown"),"available":["default-model"]}` | 同 |
| 任何异常 | `logger.exception("command.model failed: %s")`；`ok=false`，payload `{"error": str(e)}` | `logger.Error(...).Err(err)`；`WithResponseOK(false)` |

### 6.3 两个必须复刻的 Python 语义

1. **`current` 取自 `os.Getenv("MODEL_NAME")`，缺省字面量 `"unknown"`**。
   不要抄 `handleCommandStatus` 的 `config.Get("model.name")`——那是另一套来源，混用会让 `current` 与实际 env 不一致。
2. **`os.environ[k] = v` 对非字符串 `v` 抛 `TypeError`**。
   Python 的 `for k, v in env_updates.items(): os.environ[k] = v` 在 `v` 为 `int`/`None`/嵌套对象时抛 `TypeError("str expected, not int")`，被外层 `except` 捕获成 `ok=false, {"error": "..."}`。
   Go 侧**不得**用 `fmt.Sprintf("%v")` 宽容转换——那会把 Python 的报错路径变成成功路径。落地方式：`env_updates` 按 `map[string]any` 解析，逐值断言 `string`，任一失败即返回 `ok=false`，`error` 文案取 `fmt.Sprintf("str expected, not %T", v)` 形态。

### 6.4 依赖可用性（已核实）

| 依赖 | 位置 | 状态 |
|---|---|---|
| `commmem.ClearConfigCache()` | `agents/harness/common/memory/config.go:131` | ✅ 存在；`go list -deps` 确认不依赖 `server` 包，无循环 |
| `AgentManager.ReloadAgentsConfig(ctx, map[string]any, map[string]string) error` | `server/runtime/agent_manager.go:361` | ✅ 存在，`server` 包已通过 `handle_config.go:59` 使用 |
| `schema.WithResponseOK(bool)` | `swarm/schema/agent.go:212` | ✅ 存在 |

### 6.5 文件落点

留在 `internal/swarm/server/handle_command.go`（现 331 行，改后约 420 行），不新建文件：该函数的 `switch` 分支已在此文件，搬走反而割裂 `handle_envelope.go:160` 的对应关系。无新增非测试文件，故 `doc.go` 不改。

## 七、测试计划

### 7.1 `command_parser`（保持 94.4%）

新增用例，全部对齐 Python 语义：

1. `/mode  agent`（双空格）→ `mode_bad`（因 `ValidModeLines` 是 `"/mode "+sub` 单空格键，Python `t in _VALID_MODE_LINES` 同样不命中）
2. `/skills  list`（双空格）→ `skills_ok`（Python 用 `normalized` 比较）
3. `/rewind 1_0` → `rewind_bad`（D4 偏差锁定）
4. `/rewind 0` / `/rewind -1` → `rewind_bad`
5. `/rewind confirm 0` → `rewind_bad`
6. `/branch  `（尾随空格）→ `branch_ok` 且 `BranchName == ""`
7. 含 `\n` 的任意控制文本 → `none`
8. `IsControlLikeForIMBatching`：`/switchx` → true（裸 `/switch` 前缀命中，Python L201 无空格）、`/skill` → false（只有 `normalized == "/skills list"` 精确命中，无 `/skill` 前缀）

### 7.2 `message_handler`（56.8% → ≥85%）

**测试替身方案（不改生产结构）**：`routing.NewAgentClient(transport.AgentTransport)` 接受接口注入（`agent_client.go:80`）。用假 transport：

- `Send` 记录请求帧，按预设脚本把响应帧推入 `Recv` 通道；
- `Connect` 前先推 `connection.ack` 使 `IsConnected()` 为 true（`agent_client.go:191` 检查 `running`）。

需补的用例矩阵：

| 函数 | 用例 |
|---|---|
| `skillsSlashNotice` | ① 未连接（A4 形态）② `resp.OK=true` 且 payload 非空（A3：断言逐键透传）③ `resp.OK=true` 且 payload nil（A3：断言 `{"data": nil}`）④ `resp.OK=false` 带 error ⑤ `resp.OK=false` 无 error ⑥ `Send` 返回 err |
| `branchSlashNotice` | ① 无活跃会话 ② 未连接 ③ `resp.OK=false` 带 error（A2 日志）④ 成功且 payload 回填 session_id/title ⑤ 成功且 payload 缺字段（回落到 newSID/branchName）⑥ `Send` err ⑦ 成功路径取消为**同步**（A1：断言取消完成先于通知发布） |
| `rewindSlashNotice` | ① 无活跃会话 ② E2A 成功（断言 preview 无兜底、A5；断言 info 含 rewind_context、A6）③ `resp.OK=false` → fallback ④ `Send` err → fallback ⑤ fallback 返回 err ⑥ fallback 成功（断言不再兜底 content） |
| `rewindSlashConfirmPrompt` | 已有四行模板用例，补 `turn==0` 回落 1 |
| `handleChannelControl` | ① `skills_ok` **不创建** channel state（A7②）② 非受控渠道不解析 ③ 非 req / 非 chat.send 早退（D1） |
| `sendChannelNotice` | 断言 `Params == {}`（A8）、`EnableStreaming == true`、`EventType == chat_final` |

### 7.3 `server` 包 `handle_command_model_test.go`（新建）

| 用例 | 断言 |
|---|---|
| `add_model` | payload `type=model_added`、`name` 去空格 |
| `switch_model` 空 env_updates | `OK=false`、`error=No env_updates provided` |
| `switch_model` 正常 | `OK=true`、`current` 来自 `MODEL_NAME`、`requested`、`type=switched`、`applied=true`；`t.Setenv("MODEL_NAME","x")` 后断言 `current=="x"` |
| `switch_model` 非字符串 env 值 | `OK=false`，error 非空（复刻 §6.3 第 2 点的 Python `TypeError` 路径） |
| 缺省 action | `available == ["default-model"]` |
| `MODEL_NAME` 未设 | `current == "unknown"` |
| `API_KEY` 掩码 | 捕获日志或抽出掩码函数单测（建议抽 `maskEnvForLog(map[string]string) map[string]string`，避免断言日志内容） |

## 八、文档与计划同步（回填不丢失）

### 8.1 `IMPLEMENTATION_PLAN.md`

10.4.4 行**保留原文**，之后插入两行：

```
| 10.4.4a | ☐ | Slash 命令处理（对账收口 + /model） | 8 项对账（A1/A2/A5/A6/A7/A8 改代码，A3/A4 核实等价）+ command.model 全量实现；解析器 94.4%/分发器 ≥85%；/evolve 5 个命令已完整实现（deep_adapter_slash.go 563 行，非骨架） | `command_parser/slash_command.py` · `message_handler.py` L347-1145 · `agent_ws_server.py` L2277-2352 |
| 10.4.4b | ⤵️ | Slash 命令处理（/sandbox 命令面） | 9 个 sub + Linux 平台守卫 + SANDBOX_BAD_REQUEST/SANDBOX_INTERNAL 双错误码；依赖 10.3.24 Sandbox 包与 jiuwenbox（21k 行 Python 守护进程） | `agent_ws_server.py` L2853-3510 |
```

10.3.24 行末尾追加 `；⤴️ /sandbox 命令面来源 10.4.4b`。

### 8.2 原有内容保留清单

- 10.4.4 行原文**一个字不改**；「/evolve 是骨架 → 实际已完整实现（563 行）」的订正写进新增的 10.4.4a 行，不动原始行；
- 11.7/11.13 已落地的三块（SessionMap 轮转、SessionStart hook、M-01/M-03/M-05 审查注释）在 `slash_cmd.go` 中**原样保留**，A1-A8 只做增量修改；
- `doc.go` 无新增非测试文件，不改。

## 九、验收标准

1. `go test -cover ./internal/swarm/gateway/message_handler/...`：`message_handler` ≥85%，`command_parser` 不低于 94.4%；
2. `go test -cover ./internal/swarm/server/`：`handleCommandModel` 行覆盖 100%（含掩码辅助函数）；
3. 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL；
4. `go vet ./...` 干净，`gofmt -l` 为空；
5. A1-A8 每项有对应测试断言（A1 断言取消先于通知、A2 断言成功有 info、A3/A4 断言既有形态不被改动、A5 断言 preview 不兜底、A6 断言 info 含 `rewind_context`、A7 断言 state 未创建、A8 断言 `params` 为 `{}`）。

## 十、风险与回滚

| 风险 | 影响 | 处置 |
|---|---|---|
| A7② 改变 `skills_ok` 路径不再建 state | 若下游依赖 `channel_states` 中存在该渠道条目，`/skills list` 后会缺失 | 测试断言「不创建」；这是 Python 行为，回归时按 Python 为准 |
| A1 改同步后 `/branch` 阻塞 forwardLoop | 最坏为一次 `SendRequest` 超时 | 与 `/new_session`、`/mode` 现状同构，已被接受 |
| A8 把 `params` 从 `null` 改为 `{}` | 下游 Channel 若对 nil 有特殊分支 | 已检索 6 处 `Params == nil` / `len(Params)==0` 消费点：5 处用 `len()==0`（`{}` 与 `null` 等价），1 处严格判 nil（`acp_channel.go:905`）位于 ACP 出站 init 请求且其自身也把 nil 归一为 `{}`，均不依赖 `null`。风险可忽略 |
| `/model` 写 `os.Environ` 是进程级副作用 | 影响同进程其他 Agent 实例 | Python 同为进程级 `os.environ` 写入，属对齐行为；不引入按实例隔离，仅在代码注释中标注该副作用 |

回滚：单节改动集中在 `slash_cmd.go`、`handle_command.go` 与两个测试文件，无跨包接口变更，`git revert` 即可。
