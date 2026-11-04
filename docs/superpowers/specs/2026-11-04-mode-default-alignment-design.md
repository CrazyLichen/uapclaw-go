# mode 缺省语义对齐设计（0.2.5 语义 + 规则 9 结构忠实）

> 日期：2026-11-04
> 触发：`TestAcpBridgeE2E_TimerSuperseded` 30s 等不到 `fs/read_text_file` 帧，工具被 `AgentModeRail` 以「不在 plan 模式允许列表中」拒绝。
> 上游基线：`/home/opensource/jiuwenswarm-develop`（`jiuwenswarm` 0.2.5.beta1，`a2abceccf`）；对照旧基线 `/home/opensource/backup-20261104/jiuwenswarm-develop`（0.2.0，`1ea114cf`）。
> 关联：`09f5113f`（默认值从 plan 改 normal 的止血提交）、`docs/superpowers/specs/2026-11-04-plan-runtime-layer-alignment-design.md`。

---

## 1. 根因：不是「默认值写错」，是两个上游版本被拼在一起

| 事实 | 证据 |
| --- | --- |
| 0.2.0 网关默认 **plan**，且注释「对齐 L645」当时正确 | `backup .../gateway/app_gateway.py:645` `Mode.AGENT_PLAN` + `:650 setdefault` |
| 0.2.0 的 `agent.plan` 只影响 rail 装配，**不动执行态** | `backup .../agent-core/openjiuwen/harness/schema/state.py:27` `mode: str = "normal"`；`deep_agent.py:1861 switch_mode` 只由工具调用 |
| 0.2.5 引入「request mode → plan 执行态」同步 | `jiuwenswarm/runtime/plan.py:200-250` `target_state = "plan" if resolved.is_plan else "normal"` |
| 0.2.5 同步把默认值挪离 plan（两者是配套改动） | `gateway/app_gateway.py:1650` `Mode.AGENT`；`gateway/channel_manager/web/web_connect.py:2357` `default=Mode.AGENT_WORK_NORMAL` |
| Go 的 mode 解析层已是 0.2.5 形状 | `internal/swarm/runtime/request.go` 含 `auto_harness`、`ResolveNewCanonicalModeText`、`plan_entry_source` |
| Go 的默认值仍停在 0.2.0 | `channel_state.go:211/230`、`acp_bridge.go:215`、`web_normalize.go:310` |

结论：Go 把 0.2.5 的 plan 状态机接进了 0.2.0 的默认值生态，`agent.plan` 从「无害字符串」变成「工具白名单锁」。ACP 只是第一个暴露面；`getChannelDefaultState` 是同一族里**仍然存活**的 P0（六类 IM 渠道入站不带 mode → 注入 plan）。

## 2. 判定：`params.mode` 回填 canonical，不写字面 `"agent"`

三条依据：

1. **在途即被覆写**：Python `runtime/request.py:357` `request.params["mode"] = resolved.canonical_mode`；Go `runtime/request.go:242` 同一句。历史、session metadata、事件里的 mode 两边都是 canonical，网关那次回填只是在途帧。
2. **上游边缘自己就写 canonical**：`web_connect.py:2357` 的默认值是 `Mode.AGENT_WORK_NORMAL`。`"agent"` 不是契约。
3. **canonical 是不动点**：`NormalizeMode(canonical) == canonical`；`"agent"` 依赖别名表完整性。上游为此付过代价——`interface_deep.py:13045-13056` 明确把 canonical 归一二元 token，注释原文「保证下游 sweeper 的 `== "agent"` 判定正确」。

**不变量（本设计钉死）**

- I1 任何缺省/兜底 mode 满足 `!schema.IsPlanMode(v)`
- I2 回填值是 canonical：`NormalizeMode(v) == v`
- I3 `params.mode` 非字符串时**用解析结果覆盖**（不是"有键就留"）

## 3. 结构忠实（规则 9）后的归属对照

| Python | Go 现状 | 本设计 |
| --- | --- | --- |
| `class Mode(str, Enum)` + `from_raw` + `to_runtime_mode` | `schema.Mode`（10 成员）+ `ParseMode` + 恒等 `ToRuntimeMode` | 补 8 个 legacy 成员（合计 9）；`ModeFromRaw(raw any, default)` 取代 `ParseMode`；`ToRuntimeMode` 按 legacy 折叠表 |
| `class ChannelMode(str, Enum)`（18 成员） | `type ChannelMode int` + `channelModeStrings` + `ChannelModeString` + 6 成员 | 改 `type ChannelMode string`，18 成员，删除映射表与 `ChannelModeString` |
| `channel_mode_from_str`（`:233-263`） | `ParseChannelMode`（无日志、兜底 plan） | `channelModeFromStr`，兜底 `ChannelModeAgent`，补 debug×2 + warn×1 |
| `_get_channel_default_state`（`:889-907`） | `getChannelDefaultState`（分支结构不同、默认 plan） | 步骤按 Python 顺序重排，`default_mode or "agent"` → `channelModeFromStr` |
| `handle_mode_switch`（`:1902-1982`） | 与 `/switch` 合并在 `modeChangeCancelAndNotice` | 拆出 `handleModeSwitch`，职责同 Python |
| `_handle_channel_control` 的 SWITCH_OK 内联块（`:2163-2210`） | 落在 `modeChangeCancelAndNotice` 的 switch 分支 | 内联回 `handleChannelControl` |
| `_mode_change_cancel_and_notice`（`:1880`） | 与模式判定合并 | 收敛为「取消 + 通知」两件事 |
| `WebChannel._parse_mode`（`web_connect.py:2357`） | `parseMode`：裸类型转换 + 默认 plan | `schema.ModeFromRaw(raw, ModeAgentWorkNormal)` |
| ACP `mode=Mode.AGENT`（`acp_connect.py:329`） | `Mode: schema.ModeAgentWorkPlan` | `Mode: schema.ModeAgent`（值 `"agent"`），`ToRuntimeMode()` 恒等即正确 |
| `Mode.from_raw(params.get("mode"), default=...)` + `isinstance` 回填（`app_gateway.py:1650-1658`） | `ParseMode(strVal(...))` + `if _, has` | `ModeFromRaw(params["mode"], default)` + 非字符串覆盖 |
| 未知 `team.*` 守卫（`message_handler.py:755-785`） | 缺失 | 补齐：`chat.error` + `code=UNKNOWN_TEAM_MODE` |

## 4. 改动清单

### A. 缺省值（活路径）

| # | 位置 | 改动 | 上游 |
| --- | --- | --- | --- |
| A1 | `message_handler/channel_state.go:211` | config 分支默认 → `ChannelModeAgentWorkNormal` | `message_handler.py:899` |
| A2 | `channel_state.go:230` | 无 config 分支默认 → 同上 | 同上 |
| A3 | `channel_state.go:82` | `channelModeFromStr` 兜底 → `ChannelModeAgent`（值 `"agent"`） | `:263` `return ChannelMode.AGENT` |
| A4 | `channel_state.go:74` | 删除 `ChannelModeString` 及其兜底串，调用点改 `string(mode)` | `state.mode.value` |
| A5 | `channel_manager/web/web_normalize.go:304-310` | `parseMode(raw any)` = `schema.ModeFromRaw(raw, ModeAgentWorkNormal)` | `web_connect.py:2356-2358` |
| A6 | `gateway/acp_bridge.go:215` | `Mode: schema.ModeAgent` | `acp_connect.py:329` |
| A7 | `gateway/gateway_server.go:677-686` | `ModeFromRaw(params["mode"], defaultMode)`；回填条件改「非字符串即覆盖」 | `app_gateway.py:1650-1658` |

### B. 命令语义

| # | 位置 | 改动 | 上游 |
| --- | --- | --- | --- |
| B1 | `slash_cmd.go` 新增 `handleModeSwitch` | `/mode` 白名单校验 → `channelModeFromStr` → 取消/通知 | `:1902-1982` |
| B2 | `slash_cmd.go` SWITCH_OK 内联 | agent 族 `plan`/`fast` → `ChannelModeAgentWorkNormal`；code 族 `plan`→`AgentCodePlan`、`normal`→`AgentCodeNormal`、`team`→`TeamCodeNormal`；其余「非法指令」 | `:2163-2210` |
| B3 | `/mode` 白名单 | 25 项：`NEW_CANONICAL_MODES(9) ∪ DEPRECATION_MAP(10) ∪ MODE_ALIASES(6)` | `:99-101` |
| B4 | 未知 `team.*` chat.send 守卫 | `chat.error` + `code=UNKNOWN_TEAM_MODE` + 合法值列表 | `:755-785` |

> B2 的既有语义副作用（**按上游保留**）：`_SWITCH_AGENT_WORK_MODES` 同时含 `agent.work.normal` 与 `agent.work.plan`，故当前态为 `agent.work.plan` 时 `/switch plan` 会退回 normal，不是 no-op。用例钉住，不视为缺陷。

### C. 枚举与集合（D1-D3）

- `schema/mode.go`：补 8 个 legacy 成员（`agent`/`agent.plan`/`agent.fast`/`code.plan`/`code.normal`/`code.team`/`team.plan.normal`/`team.plan.code`；`team` 已有 → 合计 9 个 legacy，`AllModes` 由 10 变 18）。`ToRuntimeMode` 折叠表按 `tests/unit_tests/evolution/test_message.py:136-158` 契约：`agent.*` → `"agent"`，其余 legacy 原样，canonical 原样。
- `message_handler.ChannelMode`：上游 18 成员全量映射（Go 现 6，补 12，含 `TEAM_PLAN`/`TEAM_PLAN_NORMAL` 同值 `team.plan.normal`，与 Python 一致）。
- `/mode` 白名单 25 项（B3）。

### D. 日志（规则 3）

| 位置 | 补什么 | 上游 |
| --- | --- | --- |
| `channelModeFromStr` | 命中 canonical → `Debug`；经别名归一 → `Debug`；构造失败/兜底 → `Warn`（带 raw） | `:253-263` |
| `handleModeSwitch` | 非法 → `Warn`（mode_str/channel/sid）；成功 → `Info`（channel、old→new、old_sid） | `:1931-1963` |

## 5. 删除与同步清单（规则 8）

| 对象 | 原因 |
| --- | --- |
| `channel_state.go` `channelModeStrings`、`channelModeLookup`、`init()` 中的映射构建 | 字符串枚举后无消费方（Python 用 `.value`） |
| `channel_state.go` `ChannelModeString` | 同上；5 个调用点改 `string(...)` |
| `schema/mode.go` `ParseMode` | 由 `ModeFromRaw(any, Mode)` 取代 |
| `slash_cmd.go` `modeChangeCancelAndNotice` 的模式判定分支 | 拆回 `handleModeSwitch` + `/switch` 内联，函数只剩 cancel+notice |
| `message_handler/doc.go:17,20` | 文件职责说明随 `ChannelMode` 字符串枚举与 `handleModeSwitch` 拆分同步（规则 1） |
| 注释残留 | `slash_cmd.go:189-190`「原实现…回落 AgentPlan」、`channel_state.go:33-36`「agent.plan 模式/agent.fast 模式」等过时说明 |
| `message_handler_extra_test.go:80-83` | 断言 `ChannelModeString(ChannelMode(99)) == "agent.work.plan"`，随枚举删除改写 |
| `IMPLEMENTATION_PLAN.md` 24.12 行（同步） | 补记本次默认值/命令语义对齐（规则 2） |
| `docs/superpowers/specs/2026-11-04-plan-runtime-layer-alignment-design.md` §13（同步，非删除） | 增两条偏差：回填 canonical；ChannelMode 枚举曾窄于上游（本设计已修） |

## 6. 测试矩阵

### 6.1 单元

| 文件 | 断言 |
| --- | --- |
| `schema/mode_test.go` | `ModeFromRaw` 非字符串/空/非法 → default；legacy 串归一；`ToRuntimeMode` 折叠表（`agent.plan`→`"agent"`）；`AllModes` 18 |
| `message_handler/channel_state_test.go` | 默认态 `agent.work.normal`；`channelModeFromStr` 未知 → `"agent"`；`/mode` 25 项全部可解析 |
| `message_handler/slash_cmd_test.go` | `/mode agent`→normal；`/switch plan` 两态；`/switch fast` code 族非法；`/mode team.work.normal` 接受；`/mode nonsense` 非法 |
| `web/web_normalize_test.go` | `parseMode` 缺省→normal、`agent.plan`→`agent.work.plan`、数组→normal（I3） |
| `gateway/acp_bridge_test.go` | `msg.Mode == schema.ModeAgent` |
| `gateway/gateway_server_test.go` | 缺 mode→`agent.work.normal`；`mode:["x"]`→被覆盖（I3） |

### 6.2 集成（`tests/integration/swarm/gateway/`，真实跨 ≥3 二级子系统）

复用 `CliRouteFullChainSuite` 装配（`MessageHandler + ChannelManager + GatewayServer + AgentServer + modeRecorder`），新增 `ModeDefaultAlignmentSuite`：

| 用例 | 覆盖 | 断言 |
| --- | --- | --- |
| `TestIMChannelDefaultMode` | A1-A3 | feishu 入站无 mode → 工厂收到 `agent`；落盘 params/metadata `agent.work.normal`；工具调用不被 plan 白名单拒绝 |
| `TestWebParseMode` | A5 | web 路由真实帧三态：缺省/`agent.plan`/非法 |
| `TestACPPromptDefaultMode` | A6 | `session/prompt` 无 mode → 帧内 `params.mode=agent`，下游解析 canonical（并入 `e2e_acp_bridge_test.go`） |
| `TestModeSlashAgent` | B1 | `/mode agent` → 状态 normal + 回执 + 下一轮工厂 mode |
| `TestSwitchPlanTwoStates` | B2 | normal 态 no-op 且回执；plan 态退回 normal |
| `TestModeWhitelist` | B3 | `team.work.normal` 接受并回执；`nonsense` → 非法指令 |
| `TestUnknownTeamModeRejected` | B4 | `mode=team.nonsense` 的 chat.send → `chat.error` + `code=UNKNOWN_TEAM_MODE`，且**不**转发到 AgentServer |

审计：`go run ./cmd/integration-audit -verify`（目录跨度、有效测试密度、覆盖交叉验证）。

## 7. 已知偏差（规则 9.6，需确认）

| # | 偏差 | Python 依据 | Go 现状 | 处置 |
| --- | --- | --- | --- | --- |
| D1 | `_get_channel_default_state` 的 session_id 生成位置 | 0.2.5 已移出：`_get_channel_default_state` 返回 `None`，由 `_allocate_channel_session`（`message_handler.py:950+`）分配并落库 | Go 仍在 `getChannelDefaultState` 内 `GenerateChannelSessionID`（0.2.0 形状） | **本期保留**。移植 `_allocate_channel_session` 属渠道会话分配专项（含 persist_session/项目绑定），单独任务；回收条件＝该专项落地 |
| D2 | `send_channel_notice` 入参形状 | Python `send_channel_notice(user_infos, channel_id, reply_session_id, text)`（文本） | Go `sendChannelNotice(msg, payload)`（结构化 payload） | 本期不改（跨 30+ 调用点）；`ModeChangeCancelParams` 的 `user_infos/channel_id` 字段在 Go 侧由 msg 派生 |
| D3 | 文件级拆分（既有） | `WebChannel` 单类内含 `_handle_raw_message` / `_parse_mode` 等 | `web.BuildUserMessage`、`web.HandleMessage` 为包级导出函数 | 本期不重构，登记结构债；`parseMode` 因本次触及，按 `_parse_mode` 对齐 |

## 8. 验收

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/schema/ ./internal/swarm/gateway/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/gateway/...
go run ./cmd/integration-audit -verify
```

- 改动行覆盖率 ≥ 85%（含 5% 容差），不得行级豁免
- `grep -r "ParseMode\|ChannelModeString\|channelModeStrings\|ModeAgentWorkPlan" internal/swarm/gateway/message_handler/` 中，`ModeAgentWorkPlan` 仅允许出现在「显式进入 plan」的写入点，不得出现在任何默认/兜底位置
- 远端 CI 四 job 全绿（Build / Unit / Integration / Lint）

## 9. 风险

1. **`ChannelMode` int→string 的序列化面**：已确认不参与 JSON；`channel_control_handled` 事件的 `action` 字段是 `ParsedControlAction`，不受影响。
2. **`/switch plan` 语义变更**：IM 渠道用户从「可进 plan」变为「进 plan 需显式入口」，回执文案沿用上游 `[收到 CLI 指令], mode 已变更为 <label>`。
3. **`schema.Mode` 扩容**：`AllModes` 供给 `modeLookup` 初始化，legacy 成员经 `CanonicalizeMode` 短路后不可达，不影响 `ParseMode`/`IsPlanMode` 判定；`mode_test` 的 `len==10` 断言需改 18。
