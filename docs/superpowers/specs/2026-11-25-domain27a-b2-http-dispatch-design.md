# 27-A B2 设计：Web HTTP 路由/鉴权/超时/分发 + workspace 面板（2026-11-25）

> 对应 `IMPLEMENTATION_PLAN.md` 领域二十七「27-A Web 后端结构对齐批次」B2 行。
> 目标：为前端 `webHttpClient` http 档（企业默认传输、个人版可选）提供 `/api/v1/*` HTTP 入口面，
> 并回收 27.8 五件回填与 workspace 面板（原 27.11 挂账）。

## 1. 背景与流程位置

上游 Python 中 B2 是 WebChannel 的 HTTP 入口面：

```
WebChannel.start()
 └─ _start_uvicorn_server()                    # web_connect.py:819 —— 同端口承载 WS + HTTP
     └─ build_web_channel_app()
         └─ register_web_http_api_routes()     # ★ B2 主体
             ├─ CORE 手写路由 + mapped 表路由
             └─ _unary/_stream/_history_json
                 └─ dispatch_http_request()    # ★ 分发面
                     └─ invoke.dispatch_web_request()  # 复用 WS 的 RPC 管线
```

Go 现状：`app_gateway.go` chi 只有 `/ws` + `/file-api` + SPA fallback，**无 `/api/v1` 面**；
27.8 富媒体全部走 WS 分流档。B1 已收口 WS 侧 methodHandlers 管线，B2 复用它。

## 2. 裁决记录（2026-11-25 brainstorming 定稿）

| # | 裁决点 | 结论 |
|---|--------|------|
| 1 | mapped 表范围 | **全量照搬**（110+ 条），目标 RPC 缺失回 `METHOD_NOT_FOUND`；后续批次只补 handler 不动路由层 |
| 2 | `/api/v1/auth/*` 华为登录组 | **不移植，挂账**（依赖 gateway/auth/* 子系统：华为云 OAuth/APIG/remote_config，Go 单机无此部署形态）；mapped 表本不含 auth 路由，不阻塞路由层 |
| 3 | outbound 三符号 | **前置进本批**一比一移植（HTTP 分发必需件）；plan B3 行标注"已随 B2 回收" |
| 4 | AgentOS 上传监听器（WS 端口+1） | **不移植，挂账**（容器部署形态专用；个人版上传走 WS 分块档 + HTTP multipart mapped 路由，对齐 `harness_import_file` 的 multipart→base64 模式） |
| 5 | container_file 下载半区 | **进本批**：`/download` 路由 + token 验签 + `serve_verified_download` + `serve_file_chunks`（HEAD/Range/416）；`download_container_file` 容器分支不移植，回 BAD_REQUEST |
| 6 | workspace.* 四 handler | **并入本批**（单机版：zones 白名单 + tree/preview/entries_delete + usage；租户隔离与 quota.py 774 行不移植）；plan 27.11 行改判"个人版功能，随 B2 交付" |

### 安全模型（无全局鉴权闸，三层防）

个人版对齐 Python = 不做登录鉴权，但三层缺一不可：

1. **网络边界**：chi server 绑定行为对齐（host 可配 `UAPCLAW_WEB_HOST`，默认 127.0.0.1）；
2. **信任边界**：`trustClientTenantHeaders(remoteAddr)`（对齐 `web_http_dispatch.py:48`）——
   仅回环来源（127.0.0.1/::1/localhost）信任 `X-User-Id/X-Group-Id/X-Bot-Id/X-Gateway-Id`；
   非回环 identity 置空 → 匿名本地身份；
3. **请求级校验**：① chat.send/resume/user_answer 三方法头体 identity 一致性
   （`_chat_identity_error`，`web_http_routes.py:640-666`：header 非空 && body 值非空 && 不等 →
   400 `IDENTITY_CONFLICT`，`details.field` 标明字段；missing/blank 视为 unset 放行）；
   ② download token HMAC 验签（27.8 已签发侧，本批补验签消费侧）。

WS 侧既有握手闸（`_reject_unauthorized_handshake` 对齐物）不动。

## 3. 文件映射与落点（规则 9：Python 一文件 → Go 一文件）

Python `gateway/channel_manager/web/` → Go `internal/swarm/gateway/channel_manager/web/`：

| Python | Go 新文件 | 内容与行号锚点 |
|---|---|---|
| `web_http_routes.py` (925) | `web_http_routes.go` | `registerWebHTTPAPIRoutes`（:216）；CORE 路由：health（:234）、connection_status（:242）、cron_runs_live SSE（:246）、sessions CRUD（:281-341）、history_get JSON+SSE 双形态（:343，cursor 协议 `_history_params_from_request` :131）、session_live（:359）、chat_send SSE（:410，兼容路径 /chat/send）、chat_resume（:431）、chat_interrupt（:437）、chat_user_answer（:449）、catalog（:467）、harness_import_file multipart 50MB（:487）；`_unary`（:669）、`_stream`（:743）、`_history_json`（:522）；`_envelope_from_res`（:177，error code→HTTP status 映射：BAD_REQUEST 400/UNAUTHORIZED 401/FORBIDDEN 403/NOT_FOUND·METHOD_NOT_FOUND 404/CONFLICT 409/TIMEOUT 504/SERVICE_UNAVAILABLE 503/WORKSPACE_QUOTA_EXCEEDED 403/其余 500）；`_coerce_query_value`（:823，"true"/"yes"→bool、isdigit→int，"1"/"0" 不截胡成 bool）；`_merge_header_params`（:840，`_HEADER_TO_PARAM` :36）；`_register_mapped_routes`（:880，静态路径先于 `{param}`） |
| `http_routes/mapped.py` (965) | `http_routes_mapped.go` | `WebHttpMappedRoute` 结构（:16，`__post_init__` 校验 :32）；全量路由表：`_CONFIG_ROUTES`/`_A2A_INGRESS_ROUTES`/`_MODELS_ROUTES`/`_LOCALE_ROUTES`/`_CRON_ROUTES`/`_IM_HOSTING_ROUTES`/`_PERMISSIONS_ROUTES`/`_SESSION_LIFECYCLE_ROUTES`/`_PROJECT_ROUTES`/`_SKILLS_ROUTES`(60+条)/`_HARNESS_ROUTES`/`WORKSPACE_FILE_ROUTES`(:794-837)；`mapped_routes_for_edition`（:855，Go 无企业平面→恒个人版表，偏差 D-5）；`validate_mapped_routes`（:886，防重复面，path 参数名归一）；`catalog_entries`（:918，含 CORE_ROUTE_CATALOG :870 与 multipart 专行 :953） |
| `web_http_dispatch.py` (228) | `web_http_dispatch.go` | `dispatchHTTPRequest`（:97：session 绑定→outbound 注册→Message 构造 :202→`invoke_web_request`）；`_trust_client_tenant_headers`（:48，env 显式开关 + 企业恒真 + 回环判定；Go 恒个人版）；`resolve_http_routing_identity`（:82）；`_build_http_ext`（:63，请求头/query 白名单入 ext，企业 X-User-Token 段不移植）；session.create 的 http_resolve_identity 审计段（:148）对齐为日志 |
| `web_http_timeouts.py` (50) | `web_http_timeouts.go` | 五档一比一：`GATEWAY_WEB_HTTP_SSE_TIMEOUT`（默认 0=不限生命周期）、`..._SSE_IDLE_TIMEOUT`（0=禁用）、`..._SSE_KEEPALIVE`（30s）、`..._UNARY_TIMEOUT`（120s）、`..._HISTORY_TIMEOUT`（60s） |
| `outbound.py` HTTP 段 | `web_outbound_http.go` | `HttpJsonOutbound`（wait_response）、`HttpSseOutbound`（iter_sse_frames：timeout/idle_timeout/keepalive）、`bind_http_session`——B3 前置件，一比一移植 outbound.py 对应行；plan B3 行标注回收 |
| `container_file_http.py` 下载半区 | `container_file_http_download.go` | `serve_verified_download`（:483：token payload.sid → E2A `fetch_agent_unary` 调 `file.download_verified_chunk` → `_decode_verified_chunk` 校验 offset/limit）；`serve_file_chunks`（:527：HEAD/Range 单区间解析、416 `Content-Range: bytes */total`、流式分块、Content-Disposition）；`/download` 路由（:838：token 验签分流，verified 走本批，location 分支不移植）；`_decode_download_token_payload` 等验签函数 |

server 侧（workspace 面板）落 `internal/swarm/server/runtime/gateway_adapter/`：

| Python | Go 新文件 | 内容 |
|---|---|---|
| `enterprise/capabilities/workspace/service.py` 核心 | `workspace_panel_service.go` | `list_tree`（zone 分区 + deletable 标记）、`preview_file`（max_bytes 上限 64KiB 默认/1MiB 上限，二进制 content=null，目录 400）、`delete_entries`（`_DEFAULT_MAX_DELETE_DEPTH=32`/`_DEFAULT_MAX_DELETE_ENTRIES=5000` 上限）、单机 usage（du 快照，无 quota 策略） |
| `enterprise/capabilities/workspace/zones.py` | `workspace_panel_zones.go` | DELETABLE_ZONES 白名单（仅 session_workspace/session_todo/artifact）、classify_zone/is_deletable/normalize_relative_path（防路径逃逸） |
| adapter 分发 | `workspace_file_adapter` 增补 | `_handle_workspace`（:199）四方法分发对齐 |

挂载点：`app_gateway.go setupRouter()` 增 `r.Route("/api/v1", ...)`——与 /ws 同端口同进程（对齐 Python uvicorn 同端口承载，不引入第二个 listener）。

## 4. 行为要点（防漂移细节）

1. **SSE 帧语义**（`_stream`）：`res` 帧 ok→`web.response` 事件、!ok→`chat.error` 事件后终止；`event` 帧按事件名透传；keepalive 注释帧 `: keepalive\n\n`；断连即 break 并 unregister outbound。
2. **history JSON 收集**（`_history_json`）：SSE 流收集 `history.message`；`status=done` 结束；`chat.error` → 404 `NOT_FOUND`；游标协议：query 有 `cursor` 或（有 `limit` 无 `page_idx`）即按游标转发，**空白 cursor 必须转发为 null 首页**（空串会被 agentserver 判 `INVALID_HISTORY_CURSOR`）。
3. **session_create**：body 全可选、`created=True` 时 ok→201；`create_token` 省略由 Gateway 生成。
4. **session_patch**：有 `pinned/pin` 字段 → `session.pin`，否则 `session.rename`（同 handler 双 RPC 分流）。
5. **chat_send**：`Accept: text/event-stream` 或 `enable_streaming=True` → SSE，否则 unary。
6. **multipart**：仅 `harness_import_file` 一处（50MB 上限→400）；media/document 上传的 mapped 行 accept_body 走 JSON（27.8 WS 分块档仍为主路径）。
7. **envelope**：`X-Request-Id`/`X-Web-RPC-Method` 响应头恒带；`metadata={rpc_method, transport:"web-http"}`。
8. **dispatch**：`bind_session_param=False` 的行不把 X-Session-Id 注入 params；`X-Channel-Id` 缺省 "web"、`X-App-Id` 缺省 "default"；outbound 用后必须 unregister。

## 5. 回填保全清单（逐项销账，一条不丢）

| 回填件 | 来源 | 处置 |
|---|---|---|
| `WORKSPACE_FILE_ROUTES` 映射路由（mapped.py:794-811） | 27.8（2026-11-24 登记） | ✅ 随本批 mapped 表 + workspace handler 闭合 |
| AgentOS 上传监听器（WS 端口+1 绑 127.0.0.1） | 27.8 | ❌ 不移植，偏差 D-B2-3（回收条件=AgentOS 容器形态） |
| token 验签段 | 27.8 / B2 本体 | ✅ 随下载半区闭合 |
| HTTP 传输模式分发面（webHttpClient http 档） | 27.8 | ✅ 随本批 dispatch + routes 闭合 |
| `file.download_verified_chunk` 容器文件路由 | 27.8 D-27.8-7 | ✅ 随下载半区闭合；D-27.8-7 销账 |

## 6. 已知偏差（规则 9.6/12.3，逐条登记）

| 编号 | 偏差 | 理由 | 回收条件 |
|---|---|---|---|
| D-B2-1 | `/api/v1/auth/*` 华为账号登录组（web_http_auth.py 349 行 + gateway/auth/{service,account_kit,model_catalog,session_store,apig,remote_config}）不移植 | 依赖华为云 OAuth/APIG 外部服务，Go 单机无此部署形态；service.enabled=false 时 Python 亦整组 404，前端登录入口行为降级为路由不存在 | 出现华为账号登录部署形态需求 |
| D-B2-2 | `download_container_file` 普通 location 分支不移植，`/download` 对非 verified token 回 BAD_REQUEST | 依赖 AgentOS 容器客户端（跨容器文件读取） | AgentOS 容器形态 |
| D-B2-3 | AgentOS 上传监听器（WS 端口+1 绑 127.0.0.1）不移植 | 容器部署形态专用；个人版上传语义由 WS 分块档（27.8）+ mapped multipart 路由覆盖 | AgentOS 容器形态 |
| D-B2-4 | workspace usage 无 quota 策略层（quota.py 774 行、TenantContext 租户隔离不移植），仅单机 du 快照 | Go 单机无租户/配额概念；`WorkspaceQuotaExceeded` 错误码保留在 envelope 映射表（403）但单机不产生 | 27.11 若裁决补配额 |
| D-B2-5 | `mapped_routes_for_edition` 无企业分支（恒个人版表：不挂 northbound_routes、保留 a2a ingress 组） | Go 无企业平面 | 企业平面落地节 |
| D-B2-6 | 企业版 X-User-Token federation 段（`_build_http_ext` :75-79）不移植 | 无企业登录集成 | 同 D-B2-5 |
| D-B2-7 | Go 无 `jiuwenswarm_auth` cookie / `X-Auth-Session` 会话体系 | 依赖 D-B2-1 | 同 D-B2-1 |
| D-B2-8 | enterprise_files.py 的 `/file-api/raw-file` OBS 分支、share_jobs 路由（B6/B4 范围）不随本批 | 归属 B4/B6 | 各自批次 |
| D-B2-9 | `cron.runs/live` 与 `sessions/{id}/live` 两条 SSE 路由只落所有权校验 + SSE 形状（keepalive），无帧源 | Go 侧 cron live_bus 未对齐到可订阅形态（领域二十 cron 事件总线缺口），本批无法接帧 | cron live bus 对齐节（⤵️ 挂账） |
| D-B2-10 | requestext 请求级 ext 载体：Python contextvars.ContextVar（Token set/reset）→ Go context.Context 值载体（WithRequestExt/RequestExtFromContext） | Go 无 contextvars；语义差异：ext 需调用方显式随 ctx 传递，AgentServer 入口抬升时漏调则静默丢失 | 出现对齐 contextvars 语义的统一载体 |
| ~~D-B2-11~~ | ✅ 已对齐（2026-11-25 复核裁决）：Message.AgentRef 字段补入 schema.Message + E2AEnvelope.AgentRef + E2AToAgentRequest/MessageToE2A 双向映射（对齐 Python agent_compat.py:87-88 链） | 原落 metadata.agent_ref 为保数据不丢的过渡形 | — |
| ~~D-B2-12~~ | ✅ 已对齐：workspaceMimeHint 改用标准库 mime.TypeByExtension（与 Python mimetypes 同读系统 mime 数据库），删除手写小表 | 原手写 12 项小表覆盖不足 | — |
| ~~D-B2-14~~ | ✅ 已对齐：ctime_ms 经 syscall.Stat_t.Ctim 取真实 st_ctime（仓库先例 upload_storage.go Nlink 同法），非 Linux 回退 mtime | 原用 mtime 顶替 | — |
| ~~D-B2-15~~ | ✅ 已对齐：MeasureUsedBytes 先跑 /usr/bin/du -s -B1（120s 超时）失败走 walk 兜底，与 Python 一比一 | 原 walk 直取 | — |
| ~~D-B2-16~~ | ✅ 已对齐（2026-11-25 复核裁决）：下载半区实现落 gateway/channel_manager/web/container_file_http_download.go；分块经 E2A AgentClient.SendRequest 跨缝取（对齐 fetch_agent_unary）；channels/web 仅保留路由入口 + AgentClient 注入缝（SetFileDownloadAgentClient，app_gateway 装配） | 原直调 server/adapter 无跨缝 | — |
| ~~D-B2-17~~ | ✅ 已对齐（json tag 裁决）：sessionCreateBody/sessionPatchBody/chatSendBody 类型化（json tag + 指针 omitempty = exclude_none）+ 未声明字段 extra 透传（对齐 extra="allow" + model_dump）；chat.send session_id/query 必填校验（pydantic 422 → Go 统一 400 BAD_REQUEST，形状偏差见上） | 原裸 map 直透传 | — |

## 7. 测试策略

### 7.1 单测（包内 `_test.go`）

- envelope 映射表全 code 分支（含缺省 500、`details` 非 dict 置空）；
- `_chatIdentityConflict` 四字段 × header 空/body 空/相等/不等表驱动；
- `trustClientTenantHeaders`：回环三形态、非回环、env 显式开关；
- download token 验签：合法/签名篡改/payload 缺 sid/过期；
- Range 解析：合法单区间/多区间拒绝/越界/416 头形状；HEAD 语义；
- zones：三类可删 zone、禁删 zone、`normalize_relative_path` 逃逸用例（`..`、绝对路径、symlink 语义）、删除上限（深度/条目数）；
- 超时五档：env 缺省/合法值/0/负数/非数字；
- `_coerce_query_value`："1"→int(1) 非 true、"true"→true、"abc"→str；
- `validateMappedRoutes`：重复面 fail-fast；catalog 与表一致性。

### 7.2 集成测试（`tests/integration/swarm/web/`，suite.Run + 审计）

- **HTTP 全链会话**：`POST /api/v1/sessions` 201 → `POST /chat/completions` SSE 流式多轮（MockLLM）→ `GET /sessions/{id}/history` JSON + cursor 协议 → `POST .../actions/interrupt` → session CRUD 全套（list/get_metadata/rename/pin/delete）；
- **带附件**：multipart `POST /sessions/{id}/media` 落盘回读 → chat 消息引用该素材（attachment 语义对齐 27.8）；
- **workspace 操作**：tree → preview（文本/二进制/目录 400）→ entries_delete（白名单内 200/白名单外 403）→ usage；
- **下载链**：27.8 sealed 资产 → `GET /download?token=` 全量流 → Range 断点续传 → 416 → 篡改 token 400；
- **e2e（真实网关进程）**：对齐 27.8 `media_gateway_e2e_test.go` 模式，`/api/v1` 全链 + `GET /api/v1/catalog` 与 Go 表逐条对拍（catalog 为契约真相源）；
- **METHOD_NOT_FOUND 语义**：mapped 表中目标 RPC 未实现的行返回 404 envelope（非 500）。

### 7.3 命令与门禁

`go build ./...` / `go vet ./...` / `go run ./cmd/integration-audit -verify` /
全量 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`；本地全绿后推送（规则 6/11）。

## 8. plan 同步

- **B2 行** → ✅，含上表五件回填销账描述；
- **B3 行** → 标注"outbound 三符号已随 B2 回收"，剩 lifecycle_handlers/web_delivery_registry/ws_connection_adapter/transport_stall_guard；
- **B4 行** → 标注"下载半区已随 B2 回收"，剩 file_picker/directory_picker/enterprise_files/container_file 上传半区；
- **27.11 行** → 改判"个人版工作区面板，随 B2 交付"（租户/配额部分按 D-B2-4 挂账）；
- **27.8 行** → D-27.8-7 与五件回填状态更新；
- 同步更新 `doc.go` 文件树（新文件三份 + server 侧两份）。

## 9. 方法清单对照验收

实施完成后逐 Python 文件做方法清单对照（规则 12.4），产物补入本节；任何 Go 侧多出/缺失的函数逐条列偏差或说明。

## 9. 方法清单对照（实施完结，2026-11-25 逐文件核对）

### 9.1 web_http_routes.py → web_http_routes.go（925 行一文件一文件）

| Python | Go | 备注 |
|---|---|---|
| `_HEADER_TO_PARAM` :36 | `headerToParam` | 一比一 |
| `_STREAM_RPC_METHODS` :54 | `streamRPCMethods` | 一比一 |
| `_request_client_host` :47 | `requestClientHost` | 一比一 |
| `_body_dict` :119 | `readJSONBodyOptional` | 一比一 |
| `_history_params` :127 / `_history_params_from_request` :131 | `historyParamsFromRequest` | 两函数合一（Python _history_params 仅单行分页构造，Go 内联同分支） |
| `_web_http_metadata` :166 | `webHTTPMetadata` | 一比一 |
| `_response_headers` :170 | `responseHeaders` | 一比一 |
| `_envelope_from_res` :177 | `envelopeFromRes` | 一比一（映射表全分支） |
| `_sse_pack` :211 | `ssePack` | 一比一 |
| `register_web_http_api_routes` :216 | `RegisterWebHTTPAPIRoutes` | 路由逐条对齐；cron.runs_live/live 的 live_bus 订阅段为占位 SSE（D-B2-9） |
| `_history_json` :522 | `handleHistoryJSON` | 一比一 |
| `_chat_identity_error` :640 | `chatIdentityError` | 一比一 |
| `_unary` :669 | `runUnaryInternal`+`handleUnary` | 一比一（audit 异常段对齐为 Error 日志） |
| `_stream` :743 | `handleStream` | 一比一（帧语义/响应头/断连 break） |
| `_coerce_query_value` :823 | `coerceQueryValue` | 一比一（"1"→int 不截胡） |
| `_merge_header_params` :840 | `mergeHeaderParams` | 一比一 |
| `_params_from_mapped_route` :851 | `paramsFromMappedRoute` | 一比一 |
| `_register_mapped_routes` :880 | `registerMappedRoutes`+`mappedHandler` | 一比一 |
| SessionCreateBody/SessionPatchBody/ChatSendBody pydantic | 直接 map params | 偏差 D-B2-17：Go 无 pydantic，body 字段透传（校验语义由下游等价承担） |

### 9.2 http_routes/mapped.py → http_routes_mapped.go

| Python | Go |
|---|---|
| `WebHttpMappedRoute` :16-41（含 `__post_init__`） | `webHTTPMappedRoute` + `newWebHTTPMappedRoute`（三条校验一比一） |
| 12 组路由表（config/a2a/models/locale/cron/im_hosting/permissions/session_lifecycle/projects/skills/harness/workspace_file，130 条） | `buildMappedRoutes`（生成自 mapped.py 机器对拍 JSON，行序一致） |
| `mapped_routes_for_edition` :855 | `mappedRoutesForEdition`（恒个人版，D-B2-5） |
| `CORE_ROUTE_CATALOG` :870 | `coreRouteCatalog` |
| `validate_mapped_routes` + `_normalize_path_key` :886-903 | `validateMappedRoutes` + `normalizePathKey`+`splitPath` |
| `catalog_entries` :918-962 | `catalogEntries`（含 multipart 专行） |

### 9.3 web_http_dispatch.py → web_http_dispatch.go

| Python :行 | Go |
|---|---|
| `_header_map` :28 / `_get_header` :40 | `flattenHeaders` / `headerGet` |
| `_trust_client_tenant_headers` :48 | `trustClientTenantHeaders`（企业段恒 false，D-B2-5） |
| `_build_http_ext` :63 | `buildHTTPExt`（企业 X-User-Token 段不移植，D-B2-6） |
| `resolve_http_routing_identity` :82 | `resolveHTTPRoutingIdentity` |
| `dispatch_http_request` :97 | `dispatchHTTPRequest`（逐段对齐；outbound 由调用方构造、sessionID 回填等价 Python 构造期传入；Message.agent_ref 落 metadata.agent_ref，D-B2-11；session.create 审计段对齐为 logger.Info） |

### 9.4 outbound.py + web_delivery_registry.py + invoke.py

| Python | Go | 备注 |
|---|---|---|
| `_shared_user_route_session_id` :21 / `bind_http_session` :32 | `sharedUserRouteSessionID` / `bindHTTPSession` | 一比一（`_METHODS_WITHOUT_PARAM_SESSION_ID`={session.create}） |
| `_is_sse_end_frame` :73 | `isSSEEndFrame` | 个人版分支（D-B2-5） |
| `_normalize_frame` :96 | `normalizeFrame` | 一比一 |
| `_HttpOutboundBase` :128-199 | `httpOutboundBase`（reply/emit/send/close + WriteJSON wsSender 适配） | 一比一 |
| `HttpJsonOutbound` :201-278 | `httpJSONOutbound`（waitResponse 全分支：TIMEOUT 帧/chat.error 转换） | 一比一 |
| `HttpSseOutbound` :281-374 | `httpSSEOutbound`（iterSSEFrames：total/idle/keepalive/idle 超窗 chat.error） | 一比一 |
| `WebDeliveryRegistry` :15-122 | `webDeliveryRegistry`（register/unregister/clear/lookup/peersForSession/prune） | 一比一 |
| `is_enterprise_write_forbidden` :62-75 | `isEnterpriseWriteForbidden` | 恒 false（D-B2-5），企业封禁名单不移植 |
| `_LOCAL_ROUTING_IDENTITY_*` :26-32 | `localRoutingIdentityPrefixes/Methods` | 一比一 |
| `dispatch_web_request` :114-211 | `(*WebChannel).dispatchWebRequest` | 一比一（guard 调用位/路由身份读取/本地 handler 合并/HANDLER_BEFORE/回调/ METHOD_NOT_FOUND） |

### 9.5 web_http_timeouts.py → web_http_timeouts.go

五个 resolve 函数 + `_env_float` 一比一（50 行）。

### 9.6 container_file_http.py 下载半区 → channels/web/file_download_verified.go

| Python | Go | 备注 |
|---|---|---|
| `_error_json` | `writeFileAPIError`+`fileAPIError` | 形状对齐 |
| `_resolve_user_id` :845 | `resolveDownloadUserID` | 一比一 |
| `serve_verified_download` :483 | `serveVerifiedDownload` | E2A fetch_agent_unary → 直接调 server/adapter（D-B2-16）；`_decode_verified_chunk` 校验并入 `fetchVerifiedChunk` |
| `serve_file_chunks` :527 | `serveFileChunks`+`streamRange` | HEAD/Range/416/206/Content-Disposition 一比一 |
| `/download` 路由 :838 分流 | `HandleFileDownload` verified 分支 | 挂载点差异 D-B2-16（Go 路由在 channels/web/app_web.go） |
| `_parse_single_byte_range` | `parseSingleByteRange` | 一比一（多区间不可满足） |
| `download_container_file` 普通 location 分支 | 不移植（D-B2-2） | verified 走本批；location 分支回 BAD_REQUEST 常态 |

### 9.7 workspace 面板（zones.py + service.py + adapter）

| Python | Go | 备注 |
|---|---|---|
| zones.py 全量（WorkspaceZone 9 成员/DELETABLE_ZONES/_ROLE_MD_NAMES/_PREFIX_RULES/normalize/classify/is_deletable） | `workspace_panel_zones.go` 全量 | 一比一；mimetypes guess 用内置小表（D-B2-12） |
| service.py WorkspaceError/WorkspaceService/resolve/list_tree/delete_entries/_delete_one/_assert_dir_delete_budget/_rmtree_nofollow/preview_file/_looks_binary/measure_used_bytes | `workspace_panel_service.go` | 一比一；download_payload 不移植（D-B2-13）；du 子进程不移植直接 walk（D-B2-15）；st_ctime 用 mtime（D-B2-14） |
| workspace_file_adapter.py `_handle_workspace` :199-238 | `workspace_panel_adapter.go handleWorkspacePanel` | 一比一（max_bytes min(·,1MiB)）；quota 快照段单机化（D-B2-4） |
| schema ReqMethod WORKSPACE_* | `ReqMethodWorkspaceTree/Preview/Usage/EntriesDelete` | canonical 值对齐 jiuwenswarm/common/schema/message.py:83-86（entries.delete 带点） |

### 9.8 Go 侧多出（Python 无对应物）

| 符号 | 理由 |
|---|---|
| `httpOutboundAdapter`/`outboundSenderOf`/`outboundBaseOf` | Go 无 duck typing，路由面/发送面显式适配 |
| `requestidentity` 包 | Python 在 openjiuwen_protocol.identity（wheel），Go 缺件补齐（D-B2-10） |
| `requestext` 包 | gateway/common/request_ext.py 一比一移植（B2 分发前置件） |
| `WriteJSON`（outbound） | wsSender 接口适配（对齐 Python _enqueue_send http 分支） |
