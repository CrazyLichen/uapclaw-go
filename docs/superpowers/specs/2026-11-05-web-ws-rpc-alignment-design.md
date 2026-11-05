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

## 1. 事实基线（2026-11-05 实测）

| 口径 | 数量 | 说明 |
|---|---|---|
| 上游 Web 侧注册方法（并集） | **154** | `register_method(` 149 + `_register_config_proxy(` 补 5；含 `http_routes/mapped.py` 里同时暴露的 HTTP 版本 |
| 前端 `frontend/src` 引用的已知方法 | 122 | 正则扫 RPC 包装器首参；计划的"163"含历史/未引用项，以 B0 脚本为准 |
| 本仓 `Register(` / `RegisterLocalHandler(` | 71 | 含 4 个 `test.*`，功能 67；**同名命中上游仅 45** |
| 缺口 | **109** | 108 个在 `app_web_handlers.py`，1 个（`3rdagent.web`）在 `web_proxy_listen.py:883` |
| 本仓自造/漂移名 | **21** | 见 §1.2 |
| 上游行数 | 19,789 行 / 38 py | `web_connect.py` 2,366、`app_web_handlers.py` 6,580 为大头 |
| 本仓行数 | 4,565 行 / 5 go（非测试） | `web_connect` 629、`web_normalize` 316、`frame` 209、`web_handlers` 2,271、`config_apply` 1,024、`memory_compute` 89、`doc` 27；另有测试 3,959 行 |

> 计划 27-A 原写"8 个 go / 8,514 行"把 `_test.go` 计入了，本设计按非测试口径核算。

### 1.1 本仓自造抽象（违反规则 9.1/9.2）

| 本仓 | 上游 | 处置 |
|---|---|---|
| `frame.go`（`ReqFrame`/`ResFrame`/`EventFrame`/`Encode`） | 无此类，全程 `dict` | 删除（唯一消费方是 `frame_test.go`，生产代码零引用） |
| `RPCDispatcher`（`Register`/`Dispatch`） | `WebChannel._method_handlers` + `register_method(local_only=)`（`web_connect.py:371-400`）+ `handle_connection` 内联分发（`:2180-2231`） | 删除类型，注册表/调用回挂 `WebChannel` |
| `web_normalize.go`（位于 web 包） | `_normalize_gateway_message`/`_norm_and_forward` 在 `app_gateway.py:314/:353/:2508` | 迁 `gateway` 包 |
| `config_apply.go` / `memory_compute.go` 独立文件 | 同属 `app_web_handlers.py` | 合并回 `app_web_handlers.go` |

### 1.2 漂移/自造方法（21 个）

| 方法 | 上游是否注册 | 判定 |
|---|---|---|
| `harness.list/install/uninstall/enable/disable/get_status/rebuild`（7） | **零命中** | 上游只有 `harness.import`/`harness.export` → 删除自造 7 个，补 2 个 |
| `cron.job.disable/enable/run/status`（4） | **零命中** | 上游是 `cron.job.toggle`/`run_now`/`meta` → 删除改名 |
| `heartbeat.get_path`（1） | **零命中** | 删除 |
| `updater.cancel`/`updater.install`（2） | **零命中** | 删除（上游为 `updater.upgrade`/`reset_source`） |
| `permissions.rules.*`/`permissions.tools.set`/`permissions.approval_overrides.*`（7） | 有，但走 `http_routes/mapped.py` + `app_web_handlers.py` | 保留；矩阵标注"上游 HTTP 映射" |

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

**输出** `docs/sync/web-rpc-matrix.md`：每行 `方法名 | 前端引用位置 | 本仓注册点 | 上游注册点 | 状态`

状态仅三值（对应 I4）：
- `✅ 一致`
- `⚠️ 漂移`（本仓有此名但上游无 / 上游走 HTTP 映射）
- `❌ 缺`（上游有、本仓无）

**门禁**：`-verify` 模式下，出现任何 `❌` 或未登记的 `⚠️` 即退出非 0。进 CI **Build** job（此时前端产物已构建，扫描成本可忽略）。**不提供豁免文件**。

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

**不做**：不动 109 个缺失方法、不动业务逻辑、不动 HTTP 路由、不动 `channels/web`。

---

## 5. B1b：九批补齐 109 个方法

排序依据：前端"打开就报错"的路径优先；同上游行区相邻的合并一批（一次读懂、一次对齐）。

| 批 | 域 | 数 | 目标方法 | 上游位置（`app_web_handlers.py` 除注明） |
|---|---|---|---|---|
| **1** | config 代理族 | 10 | `models.get/upsert/delete/references`、`model_groups.list/upsert/delete`、`vendors.list/fetch_models`、`session.selection.set` | `5643-5659`（`_register_config_proxy`） |
| **2** | heartbeat | 10 | `heartbeat.job.list/get/meta/create/update/delete/toggle/preview/run_now/cancel` | `6196-6205`（`_hb_job_*`） |
| **3** | cron / health_check / updater | 7 | `cron.job.meta`、`health_check.get_conf/set_conf`、`updater.upgrade/get_conf/set_conf/reset_source` | `5865`、`5839-5840`、`5835-5838` |
| **4** | session / 杂项 | 9 | `session.get_metadata/pin/rename/plan_status/message.continue_queued`、`chat.swarmflow_reply`、`commands.list`、`connection.status`、`config.moa_status` | `5690-5694`、`5818`、`5775`、`5676`、`5637` |
| **5** | IM 渠道配置 | 18 | `channel.<feishu/xiaoyi/telegram/dingtalk/whatsapp/discord/slack/wecom/wechat>.{get_conf,set_conf}` | `5844-5861`（九个平台各一对，`_channel_<p>_get_conf/set_conf`） |
| **6** | project | 20 | `project.list/info/get_sessions/get_cron_sessions/create/rename/pin/remove/restore/pinned_sessions` + `project.git.status/probe/init/switch_branch/create_branch/commit/push/diff_status/turn_diff_list/turn_diff` | `5701-5728`；实现落 `project_git.go`（上游 `project_git.py`）+ `git_ws_handler.go` |
| **7** | 文件/媒体/文档/路径 | 8 | `file.upload_chunk`、`media.persist/discard`、`document.persist/formats`、`path.select_file/select_files/select_directory` | `5802`、`5811-5814`、`5732-5734` |
| **8** | harness / external_cli | 9 | `harness.import/export`、`external_cli.detect/validate_paths/prune_path_histories/codex_install_status/install_status/install/custom_agent.remove` | `6555`、`6567`、`5496-5502` |
| **9** | openai_account / topic_starters / prompt_suggestion / asr / 3rdagent | 18 | `openai_account.auth.{status,start_login,pending_login,poll_login,logout}`、`openai_account.models.list`、`topic_starters.{profile,restore_direction,control,status,select_model,get,foreground,event}`、`prompt_suggestion.{event,get}`、`task.asr.transcribe`、`3rdagent.web` | `5681-5686`、`5820-5827`、`5828-5829`、`5680`；`3rdagent.web` 在 `protocol/web_proxy/web_proxy_listen.py:883` |

合计 **109**。每批产出：方法注册 + handler 实现 + 单测 + 集成用例 + 矩阵更新（`✅`），单独提交、单独 CI 四绿。漂移清理随对应批执行（§1.2）：批 2 删 `heartbeat.get_path`；批 3 删 `cron.job.disable/enable/run/status`、`updater.cancel/install`；批 8 删 7 个自造 `harness.*`。

依赖顺序：批 1 必须最先（config 代理是 model/vendor/session 选择的地基，其余批会复用 `_proxy_config_request` 的 Go 等价物）；批 6 依赖 git 侧 `project_git` 的 port。

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
- `go run ./cmd/rpc-matrix -verify`：0 个 `❌`、`⚠️` 全部显式登记。
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
| 批 5（18 个 IM 配置）机械重复 | 容易抄错平台名 | 用上游同名函数逐一对照；单测表驱动覆盖 9 平台 × 2 |
| 批 9 依赖未迁移子系统 | 无法真实实现 | 见 §10，落 ⤵️ 计划行，不设测试白名单 |

---

## 10. 已知偏差与依赖（规则 9.6）

| 项 | 上游 | 现状 | 处置 |
|---|---|---|---|
| `openai_account.*`（6） | `app_web_handlers.py:5681-5686` → `_openai_account_*` → `openai_account_auth` 模块（设备码 OAuth） | 该模块属计划 23.6，未迁移 | 批 9 内：网关侧 handler 按上游结构实现，底层 OAuth 随 23.6 落地；计划落 ⤵️ 行，回收条件"23.6 完成" |
| `3rdagent.web`（1） | `protocol/web_proxy/web_proxy_listen.py:883` | `web_proxy` 子系统未迁移 | 同上，随 `web_proxy` 迁移回填；不设测试白名单 |
| `permissions.*`（7） | 上游走 `http_routes/mapped.py`，不注册为 WS 方法 | 本仓注册为 WS 方法 | 保留现状（前端已依赖），矩阵标 `⚠️ 上游 HTTP 映射`；是否收敛到 HTTP 另议 |

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
- `docs/sync/web-rpc-matrix.md` 中 `❌` 归零
