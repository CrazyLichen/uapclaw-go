# Web WS 层 RPC 全量对齐与结构归位设计（27-A B0/B1）

> 日期：2026-11-05
> 上游基线：`/home/opensource/jiuwenswarm-develop`（`jiuwenswarm` 0.2.5.beta1）
> 关联计划：`IMPLEMENTATION_PLAN.md` 27-A（B0-B8）、27.1-27.10；审查 `docs/review/2026-11-05-48h-logic-review.md` M-07/M-13
> 前序设计：`2026-11-04-mode-default-alignment-design.md`（24.13，已落地）

---

## 0. 摘要

Web WS 层现在的问题不是"少几个 handler"，而是**两层同时失配**：

1. **覆盖**：上游 `gateway/channel_manager/web/` 注册 **154** 个 RPC 方法（`register_method` + `_register_config_proxy` + 前端引用并集），本仓同名命中 **45**，缺 **109**；另有 **20** 个本仓自造/漂移名（上游零命中或走 HTTP 路由）。
2. **结构**：上游该目录 **38 个 py / 19,789 行**，本仓 **5 个 go / 4,565 行（非测试）**，且带两个上游不存在的抽象（`frame` DTO、`RPCDispatcher`）挡住后续"照抄式"对齐。

本设计分三步：**B0 生成契约矩阵并设 CI 门禁（无白名单）→ B1a 结构归位（不加功能）→ B1b 九批补齐 109 个方法**。每批单独提交、单独 CI 绿。

---

## 1. 事实基线（2026-11-05 实测，`cmd/rpc-matrix` 生成）

> 数据来源：`docs/sync/web-rpc-matrix.md`（一次扫描即入库，CI 校验不漂移）。

| 口径 | 数量 | 说明 |
|---|---|---|
| 三方方法总数 | **196** | 前端引用 ∪ 本仓注册 ∪ 上游 Web 通道注册（含直接/间接助手/表驱动三种写法） |
| ✅ 一致 | **50** | 上游有、本仓有 |
| ⚠️ 漂移 | **0** | 本批归零（原 16 个见 §1.2：删 15、`session.delete` 待 B2 改 HTTP） |
| ❓ 前端独有 | **8** | 前端在调、两侧都没注册（`session.switch` 由转发路径处理，见 §1.2） |
| ❌ 缺 | **138** | 见下方下游分布 |
| 缺口·`local` | **47** | 上游在网关本地实现，**本层补齐即可真跑** |
| 缺口·`agent:ok` | **7** | `harness.packages/scan/activate/deactivate/delete`（5）+ `session.rename`/`session.plan_status`（2） |
| 缺口·`agent:const-only` | **2** | `harness.import`/`harness.export`：有常量，AgentServer 无分支 |
| 缺口·`agent:missing` | **65** | project 20、rsi 18、topic_starters 8、session 3、prompt_suggestion 2、path 2、media 2、document 2、file 1、其余 |
| 缺口·`dep` | **17** | heartbeat.job 10、openai_account 6、task.asr 1 |
| 上游行数 | 19,789 行 / 38 py | `web_connect.py` 2,366、`app_web_handlers.py` 6,580 为大头 |
| 本仓行数 | 4,565 行 / 5 go（非测试） | `web_connect` 629、`web_normalize` 316、`frame` 209、`web_handlers` 2,271、`config_apply` 1,024、`memory_compute` 89、`doc` 27；另有测试 3,959 行 |

**本期范围判定（2026-11-05 用户决定）**：本计划**只做 web_channel 层**，即 47 个 `local` + 7 个 `agent:ok` = **54 个方法**；其余 **84 个**（const-only 2 / agent:missing 65 / dep 17）**不接线**，只登记在 `docs/sync/web-rpc-downstream-gaps.md` 作为后续章节输入。因此本层"覆盖全绿"不等于"功能可用"，矩阵必须保留下游列以示区分。

> 计划 27-A 原写"8 个 go / 8,514 行"把 `_test.go` 计入了，本设计按非测试口径核算。

### 1.1 本仓自造抽象（违反规则 9.1/9.2）

| 本仓 | 上游 | 处置 |
|---|---|---|
| `frame.go`（`ReqFrame`/`ResFrame`/`EventFrame`/`Encode`） | 无此类，全程 `dict` | 删除（唯一消费方是 `frame_test.go`，生产代码零引用） |
| `RPCDispatcher`（`Register`/`Dispatch`） | `WebChannel._method_handlers` + `register_method(local_only=)`（`web_connect.py:371-400`）+ `handle_connection` 内联分发（`:2180-2231`） | 删除类型，注册表/调用回挂 `WebChannel` |
| `web_normalize.go`（位于 web 包） | `_normalize_gateway_message`/`_norm_and_forward` 在 `app_gateway.py:314/:353/:2508` | 迁 `gateway` 包 |
| `config_apply.go` / `memory_compute.go` 独立文件 | 同属 `app_web_handlers.py` | 合并回 `app_web_handlers.go` |

### 1.2 漂移方法（26 个，按来源三分）

> 判定口径：漂移 = **本仓有、上游 Web 通道注册表里没有**。逐条对照上游 0.2.0 基线
> （`/home/opensource/backup-20261104/jiuwenswarm-develop`）与 0.2.5。
> `permissions.*` 曾被误判为漂移——上游用 `_register_perm` 间接注册（`app_web_handlers.py:6260-6275`），
> 工具已支持"间接助手注册"后归入一致。

| 来源 | 数量 | 方法 | 处置（2026-11-05 用户决定：全部对齐上游） | 状态 |
|---|---|---|---|---|
| 上游 0.2.0 有、0.2.5 **已删除** | **1** | `heartbeat.get_path` | **删除**（连带 `handleHeartbeatGetPath` 与测试） | ✅ 已删 |
| 上游 Web 侧无此方法，但 HTTP 路由有 | **1** | `session.delete`（上游 `DELETE /sessions/{session_id}`，`http_routes/mapped.py:877`） | WS 注册**已删**；HTTP 版本等 27-A B2 移植 `http_routes/` 框架时补 | ✅ 已删（HTTP 待 B2） |
| 上游只有 TUI 本地 handler，Web 侧完全没有 | **1** | `session.switch` | WS stub **删除**；该请求由 `ForwardReqMethods`（`web_normalize.go:35`）转发给 AgentServer，仍可用 | ✅ 已删 |
| 两版上游 Web 侧**都没有**（本仓自造） | **13** | `harness.{list,install,uninstall,enable,disable,get_status,rebuild}`（7）、`cron.job.{disable,enable,run,status}`（4）、`updater.{cancel,install}`（2） | **全部删除**；对应能力改用上游名：`harness.{packages,packages.scan,activate,deactivate,delete,import,export}`、`cron.job.{toggle,run_now,meta}`、`updater.{upgrade,get_conf,set_conf,reset_source}` | ✅ 已删 |

**结论：16 个漂移里 15 个删、1 个（`session.delete`）删 WS 注册后待 B2 补 HTTP。** 只有 `heartbeat.get_path` 是"上游更新后删除"，其余是本仓命名/通道与上游不一致。

**门禁**：`-verify` 现在同时要求「本层承诺范围零缺口」和「漂移为 0」——再出现上游没有的方法名会直接失败（对应规则 9.2 的反向约束）。

---

## 2. 不变量（每批验收都检查）

- **I1 无自造抽象**：不得新增上游不存在的类型/文件。文件数、文件名、归属与上游 1:1。
- **I2 函数名 = Python 名**：`_config_get` → `configGet`；禁止 `handleXxx` 这类自创前缀（现有 71 个全部改名）。
- **I3 一个上游 py ↔ 一个 Go 文件**：`web_connect.py`↔`web_connect.go`、`app_web_handlers.py`↔`app_web_handlers.go`，不再拆。
- **I4 无白名单**：矩阵不得有"本仓不提供"这一豁免态；集成测试不得用白名单/`Skip` 绕过缺失方法。做不到的必须在 `IMPLEMENTATION_PLAN.md` 落 ⤵️ 行并写明回收条件（规则 9.6）。

---

## 3. B0：契约矩阵与 CI 门禁（0.5 人日）

新增 `cmd/rpc-matrix`（Go 工具，风格对齐既有 `cmd/integration-audit`）。

**三方输入**

| 源 | 取法 |
|---|---|
| 前端 | `internal/swarm/channels/web/frontend/src/**/*.ts(x)`：RPC 包装器（`call`/`request`/`invoke`/`send`…）首个字符串字面量，以及常量表里的方法名 |
| 本仓 | `internal/swarm/**` 的 `Register("…")`、`RegisterLocalHandler(…, "…", …)` 字面量（AST） |
| 上游 | `gateway/**/*.py` 的 `register_method("…")`、`_register_config_proxy("…")`，加 `channel_manager/web/http_routes/mapped.py` 的 `(METHOD, path, req_method)` 表 |

**输出**（两份，均入库、均幂等）

1. `docs/sync/web-rpc-matrix.md`：每行 `方法名 | 覆盖状态 | 下游状态 | 下游说明 | 前端引用 | 本仓注册 | 上游注册`
2. `docs/sync/web-rpc-downstream-gaps.md`：只列"网关层补齐也不能真正工作"的方法，按下游缺口分三组

**覆盖状态**仅三值（对应 I4）：
- `✅ 一致`
- `⚠️ 漂移`（本仓有此名但上游无 / 上游走 HTTP 映射）
- `❌ 缺`（上游有、本仓无）

**下游状态**（`cmd/rpc-matrix` 自动判定，判定依据见 §3.1）：
- `local` —— 上游 handler 无代理调用，网关本地实现
- `agent:ok` —— 薄代理，本仓已有 req method 常量 + AgentServer 分支
- `agent:const-only` —— 有常量，AgentServer 无分支
- `agent:missing` —— 常量与分支皆无
- `dep` —— 依赖未迁移子系统（heartbeat job store / OAuth / ASR / web_proxy）

### 3.1 下游状态判定算法

1. 从 `channel.register_method("x", handler)` 取 handler 符号；`_register_config_proxy("x", …)` 无 handler → `local`（单用户走本地配置读写）。
2. 在 `gateway/**/*.py` 里定位该 handler 的函数体窗口（`def` 起到下一个 `def`/工厂赋值，上限 80 行）。
3. 函数体含以下任一即判为薄代理（四类上游写法，缺一不可）：
   - `proxy_unary_request(`
   - `fetch_agent_unary(`
   - `_forward_recommendation(`
   - `_make_*proxy*` 工厂调用（git 族）
4. 从函数体取第一个 `ReqMethod.XXX`；`_forward_recommendation` 固定为 `RECOMMENDATION`。
5. 常量名转本仓标识符：`SESSION_GET_METADATA` → `ReqMethodSessionGetMetadata`；查 `schema/req_method.go` 是否有该常量、`server/handle_message.go` 是否有 `case schema.<ident>`。
6. `depOverrides` 表按方法前缀覆盖（`heartbeat.job.` / `openai_account.` / `task.asr.` / `3rdagent.`）。

**门禁**：`-verify` 模式下，出现任何 `❌` 即退出非 0。进 CI **Build** job（前端产物已构建，扫描成本可忽略）。**不提供豁免文件**；`agent:*` 缺口不是豁免——本期只承诺 `local` + `agent:ok`，其余登记在缺口清单。

---

## 4. B1a：结构归位（2 人日，不新增功能）

一次提交完成 5 件事：

1. 删除 `frame.go` + `frame_test.go`。
2. `web_handlers.go`（2,271）+ `config_apply.go`（1,024）+ `memory_compute.go`（89）→ 合成 `app_web_handlers.go`（≈3,384 行），对应上游同名 6,580 行文件。
3. 删除 `RPCDispatcher`：`methodHandlers`/`Register`/`invokeMethodHandler`/`localOnly`/handler-before-callback 顺序回挂 `WebChannel`，逐条对齐 `web_connect.py:371-400`（`register_method`/`unregister_method`）、`:103-105`（`_HANDLER_BEFORE_CALLBACK_METHODS`/`_LOCAL_ONLY_METHODS`）、`:2180-2231`（分发顺序）。
4. `web_normalize.go` 拆分归位：
   - `NormalizeGatewayMessage` / `MakeNormAndForward` / `ForwardReqMethods` / `ForwardNoLocalHandlerMethods` → `gateway` 包（`app_gateway.py:314`、`:353`、`:2508`）
   - `BuildUserMessage` / `parseMode` → `web_connect.go`（上游是 `handle_connection` 内联构造 + `web_connect.py:2356`）
5. 71 个 handler 改名到 Python 名，签名逐参对齐上游
   `def _config_get(ws, req_id, params, session_id)` → `func (wc *WebChannel) configGet(ws *wsConn, reqID string, params map[string]any, sessionID string)`。
   不引入 `rpcRequest` 之类的打包结构（属上游没有的抽象，违反 I1）。

**不做**：不动缺失方法、不动业务逻辑、不动 HTTP 路由、不动 `channels/web`。

---

## 5. B1b：七批补齐 49 个方法（只做 `local` + `agent:ok`）

范围 = 47 个 `local` + 2 个 `agent:ok`。排序依据：前端"打开就报错"的路径优先；同上游行区相邻的合并一批。

| 批 | 域 | 数 | 目标方法 | 下游 | 上游位置（`app_web_handlers.py` 除注明） |
|---|---|---|---|---|---|
| **1** | config 代理族 | 10 | `models.get/upsert/delete/references`、`model_groups.list/upsert/delete`、`vendors.list/fetch_models`、`session.selection.set` | local | `5643-5659`（`_register_config_proxy`） |
| **2** | IM 渠道配置 | 18 | `channel.<feishu/xiaoyi/telegram/dingtalk/whatsapp/discord/slack/wecom/wechat>.{get_conf,set_conf}` | local | `5844-5861` |
| **3** | health_check / cron / updater | 7 | `health_check.get_conf/set_conf`、`cron.job.meta`、`updater.upgrade/get_conf/set_conf/reset_source` | local | `5839-5840`、`5865`、`5835-5838` |
| **4** | 杂项本地方法 | 4 | `connection.status`、`commands.list`、`config.moa_status`、`chat.swarmflow_reply` | local | `5676`、`5775`、`5637`、`5818` |
| **5** | external_cli | 7 | `external_cli.detect/validate_paths/prune_path_histories/codex_install_status/install_status/install/custom_agent.remove` | local | `5496-5502` |
| **6** | 路径选择 | 1 | `path.select_file`（`select_files`/`select_directory` 是代理，属下游缺口） | local | `5734` |
| **7** | session 薄代理 | 2 | `session.rename`、`session.plan_status` | agent:ok | `5693`、`5692` |

合计 **49**。每批产出：方法注册 + handler 实现 + 单测 + 集成用例 + 矩阵更新，单独提交、单独 CI 四绿。

漂移清理随对应批执行（§1.2）：批 3 删 `cron.job.disable/enable/run/status`、`updater.cancel/install`；批 6 删除 `path.get/path.set` 之外的自造项（若有）；`heartbeat.get_path` 随批 3 一并删除。

**不在本期实现（59 个）**：`agent:const-only` 2、`agent:missing` 40、`dep` 17。它们的清单在 `docs/sync/web-rpc-downstream-gaps.md`，作为以下章节的输入：

| 缺口组 | 数量 | 归属章节 |
|---|---|---|
| `project.*`（含 git 9） | 20 | 27.x 项目/工作区 + AgentServer `PROJECT_*` 适配 |
| `topic_starters.*` / `prompt_suggestion.*` | 10 | 27.3/27.x recommendation 链路（AgentServer `RECOMMENDATION`） |
| `session.get_metadata/pin/message.continue_queued` | 3 | 27.6 会话导航（AgentServer SessionAdapter） |
| `path.select_files/select_directory`、`media.*`、`document.*`、`file.upload_chunk` | 7 | 27.8/27.x 富媒体与文件（WorkspaceFileAdapter） |
| `harness.import/export` | 2 | AgentServer `HARNESS_PACKAGES_*` 分支 |
| `heartbeat.job.*` | 10 | 24.x/11.11 心跳任务存储 |
| `openai_account.*` | 6 | 23.6 |
| `task.asr.transcribe` | 1 | 23.9 / 27.x ASR |

---

## 6. 删除清单（规则 8，随 B1a/对应批落地）

| 对象 | 原因 |
|---|---|
| `frame.go`、`frame_test.go` | 上游无此抽象，生产零引用 |
| `RPCDispatcher`、`NewRPCDispatcher` | 回挂 `WebChannel` |
| `web_normalize.go` 中 `NormalizeGatewayMessage`/`MakeNormAndForward`/`ForwardReqMethods`/`ForwardNoLocalHandlerMethods` | 迁 `gateway` 包 |
| 7 个自造 `harness.*`、4 个 `cron.job.*` 旧名、`heartbeat.get_path`、`updater.cancel/install` | 上游零命中（§1.2） |
| `gateway/channel_manager/web/doc.go` 对应条目 | 随文件增删同步（规则 1） |
| 各 `*_test.go` 中对上述符号的引用 | 随改名/删除同步改写 |

grep 锚点验收：`frame\.`、`RPCDispatcher`、`handleConfigGet` 在 `internal/`、`tests/`、`cmd/` 中 0 命中；
`ForwardReqMethods`/`ForwardNoLocalHandlerMethods` 在 `web` 包中 0 命中（已迁 `gateway` 包）。

---

## 7. 测试矩阵

**单元**（每批）
- 每个新增 handler 至少 1 个用例：正常 → 返回 payload 形状；参数非法 → 错误 code；依赖未就绪 → 明确错误（不是 panic）。
- 结构归位批（B1a）：不新增行为，靠既有 3,959 行测试守护；改名后测试文件同步改指向。

**集成**（每批 ≥1 例，`tests/integration/swarm/web/`）
- 走真实 WS 帧（`ReqFrame` 已删，改用 `map[string]any` + JSON 编解码），装配 `WebChannel + GatewayServer + AgentServer`（复用 `CliRouteFullChainSuite` 同款多子系统装配）。
- 断言：注册表命中（`connection.status`/批内代表方法）+ 响应 `ok`/`code` + 事件推送顺序。
- **禁止**：白名单、`t.Skip("依赖未就绪")`、只断言 `err == nil`。

**门禁**
- `go run ./cmd/rpc-matrix -verify`：本层承诺范围（`local` + `agent:ok`）内 0 个 `❌`。
- `go run ./cmd/rpc-matrix -verify-all`：任何 `❌` 都失败（下游缺口清零后再启用）。
- `go run ./cmd/integration-audit -verify`：目录跨度 + 覆盖交叉验证。

---

## 8. 覆盖率与改动行门禁

- 改动行覆盖率 ≥ 85%（含 5% 容差）。
- **B1a 的搬移风险**：纯声明搬移会命中"改动行"门禁，且行级豁免在该路径无效（历史两次踩坑）。处置：**搬移与测试同批**——测试文件跟着改名改指向，让覆盖行仍由真实测试产生；仅 `main`/装配行做小范围登记。
- **不得整段豁免**：审查 M-13 已点名"把整段入口代码登记掉（1382 行）"。B1b 每批新增代码必须由该批新增测试覆盖，不靠豁免。

---

## 9. 风险

| 风险 | 影响 | 处置 |
|---|---|---|
| `app_web_handlers.go` 单文件 ≈3,400 行 | 可读性差 | 与上游 6,580 行单文件 1:1，符合 I3；不做人为拆分 |
| 71 个 handler 改名一次提交 | 波及大量测试 | 改名机械可批量；E2E 用 `go build` + 全包测试兜底；分批提交不给中间态留窗口 |
| 批 2（18 个 IM 配置）机械重复 | 容易抄错平台名 | 用上游同名函数逐一对照；单测表驱动覆盖 9 平台 × 2 |
| 59 条下游缺口不在本期 | 前端调到会拿到上游同形错误，但功能不完整 | 清单落 `docs/sync/web-rpc-downstream-gaps.md` 并按章节归属排期；矩阵保留下游列，避免"绿但不通" |

---

## 10. 已知偏差与依赖（规则 9.6）

| 项 | 上游 | 现状 | 处置 |
|---|---|---|---|
| 59 条下游缺口 | 见 `docs/sync/web-rpc-downstream-gaps.md`（const-only 2 / agent:missing 40 / dep 17） | 本仓 AgentServer 无对应 req method 或分支 | **本期不接线**；缺口清单作为 27.x / 23.x / 24.x 的输入，逐组排期 |
| `permissions.*`（7） | 上游走 `http_routes/mapped.py`，不注册为 WS 方法 | 本仓注册为 WS 方法 | 保留现状（前端已依赖），矩阵标"上游 HTTP 映射"；是否收敛到 HTTP 另议 |
| `wsSender` / `channelConfSource` / `modelsConfigStore` 等窄接口 | Python 为 duck typing，无静态类型 | Go 需显式接口 | 仅为等价的形参形状，不改语义；生产实现即既有类型 |
| handler 的闭包形态 | 上游 handler 是 `_register_web_handlers(bind)` 内的嵌套闭包，直接捕获 `channel` | Go 无嵌套函数，写成「工厂函数 `xxx(wc, deps) methodHandler` 返回闭包」 | 语言层差异：工厂返回值即闭包，捕获的 `wc` 等价上游 `channel`；注册处仍是一行 `wc.registerMethod(...)`，可逐行对照 |
| 错误文案语言 | 上游统一英文（`params must be object` / `job not found` / `handler error: ...`） | 本仓历史 handler 多为中文（`会话已存在` / `api_base 是必填项`） | 本期只对**新改到的分支**回填上游文案；剩余中文文案随各自属主批次收敛，不单独开批 |
| `permissions.*` 的本地回退 | `_forward_permissions_to_agent` 明确 "Gateway never falls back to local state"，E2A 失败即回错 | 本仓在 AgentClient 未就绪/转发失败时回退 `permrpc.DispatchPermissionsConfigRequest` 本地状态 | 与上一条 `permissions.*` 同源；本期保留（单机无 AgentServer 时前端仍可用），多用户形态落地时一并收敛 |
| `models.get/upsert/delete/references`、`model_groups.*`、`session.selection.set`、`vendors.*`（10） | `_register_config_proxy`：个人版恒走 E2A，企业版才用 local_handler（`app_web_handlers.py:5582-5610`） | 本仓无这些 `ReqMethod`（`internal/` 0 命中），E2A 转发是断路；本地实现需先移植 `model_catalog`(238) + `model_selection`(76) + `model_errors`(30) + config upsert/delete(~150) + `model_vendor_registry`(593) ≈1,200 行 | **本期跳过**（2026-11-05 裁定，见计划 Task 8 节），矩阵保留 `❌`；恢复时拆 8a（catalog 族）/8b（vendors 族），不得用 E2A 假接线 |
| `updater.*`（7） | `gateway/common/updater.py`(578 行)：源配置读取/校验、release API 拉取、下载、pip/desktop 双模式升级、状态机、token 掩码 | 本仓全部为 stub：`check/download/get_status` 恒回 `{"status":"up_to_date"}`，`get_conf/set_conf/reset_source` 回默认运行时配置、`upgrade` 回 `{"state":"idle"}` | **按 2026-11-05 裁定只保留 handler 回默认**（前端 UpdatePanel 可渲染，但不真正检查/下载/升级）；恢复需移植 `updater.py` 并把 7 个方法一起换真 |
| 探活配置的旧键（残留） | 上游 `migrate_legacy_heartbeat_probe_config` 迁移后 `heartbeat` 段搬空即 `pop` 整段 | 本仓 `config.MigrateLegacyHeartbeatProbeConfig()` 已按同一语义迁移（目标段 setdefault 优先、只搬三个探活键、jobs 保留），搬空时写**空 mapping** 而非删除节点；启动读侧仍保留「health_check 优先、heartbeat 兜底」 | 空 mapping 与缺失对读取等价（都得到空配置）；保留兜底是为了迁移失败时仍能用旧值 |
| 矩阵校验的权威上游 | 计划以本地快照 `/home/opensource/jiuwenswarm-develop`（`gateway/…`，6580 行）为准；上游 gitcode HEAD 已无 `gateway/`（代码在 `jiuwenswarm/…`），tag `0.2.5.beta1` 的同一文件为 6380 行 —— 三者不是同一份 | 矩阵与缺口清单由 `cmd/rpc-matrix` 以本地快照生成 | **CI 的「校验 Web RPC 契约矩阵」步骤已删除**（2026-11-05 用户裁定，避免拿降级矩阵误报过期）；漂移改由本地命令核对，后续如需门禁须先 pin 一份可复现快照 |

---

## 11. 验收命令

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/...
go run ./cmd/rpc-matrix -verify
go run ./cmd/integration-audit -verify
```

- 每批一个提交，远端 CI 四 job 全绿（Build / Lint / Unit / Integration）
- `docs/sync/web-rpc-matrix.md` 中**本层承诺范围**（`local` + `agent:ok`）的 `❌` 归零；下游缺口保留并登记在 `docs/sync/web-rpc-downstream-gaps.md`
