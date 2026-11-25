# 27-A B2 实施计划：Web HTTP 路由/鉴权/超时/分发 + workspace 面板

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为前端 webHttpClient http 档提供 `/api/v1/*` HTTP 入口面（CORE + mapped 全量路由 + 分发/超时/下载半区），并交付 workspace 面板 server 侧 handler，回收 27.8 五件回填。

**Architecture:** chi `r.Route("/api/v1", ...)` 与 /ws 同端口同进程；HTTP handler 构造 Message 后复用 B1 的 WebChannel RPC 管线（`invoke.dispatch_web_request` 对应物）；outbound 三符号前置一比一移植。安全模型三层：host 默认 127.0.0.1 / 回环才信租户头 / chat 三方法头体一致性 + download token 验签。

**Tech Stack:** Go 1.22、chi、gorilla/websocket、zerolog；Python 真相源 `/home/opensource/jiuwenswarm-develop/gateway/channel_manager/web/`。

**Spec:** `docs/superpowers/specs/2026-11-25-domain27a-b2-http-dispatch-design.md`（5e11b4a8e）。

**约定**：所有 Go 文件注释中文、声明按规范 2 排序；每处移植注释标 Python 符号与行号；每个 `_test.go`（含 tests/integration）自带 `suite.Run(`。完成标准 = 规则 6/11：本地 build/vet/单测/集成绿 + 推送。

---

### Task 0: 准备与基线

- [ ] 建 worktree：`git worktree add ../uap-claw-go-worktrees/b2-http-dispatch -b worktree-feat/27a-b2-http-dispatch`（并发会话避让，对齐 27.8 惯例）
- [ ] 确认 main 基线红清单（跑 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/...` 记录既有失败，后续不得混入新红）
- [ ] 重读 spec §2 裁决表与 §6 偏差表（实施中不得新增未经裁决的偏差）

### Task 1: 超时档 web_http_timeouts.go

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_http_timeouts.go`
- Test: `internal/swarm/gateway/channel_manager/web/web_http_timeouts_test.go`

Python 真相源：`web_http_timeouts.py`（50 行，全文一比一）。

- [ ] **Step 1: 写失败测试**

```go
func TestResolveWebHTTPTimeouts默认档(t *testing.T) {
	t.Setenv("GATEWAY_WEB_HTTP_SSE_TIMEOUT", "")
	t.Setenv("GATEWAY_WEB_HTTP_SSE_IDLE_TIMEOUT", "")
	t.Setenv("GATEWAY_WEB_HTTP_SSE_KEEPALIVE", "")
	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "")
	t.Setenv("GATEWAY_WEB_HTTP_HISTORY_TIMEOUT", "")
	require.Equal(t, 0.0, resolveWebHTTPSSETimeout())       // 默认 0=不限生命周期
	require.Equal(t, 0.0, resolveWebHTTPSSEIdleTimeout())   // 0=禁用
	require.Equal(t, 30.0, resolveWebHTTPSSEKeepalive())
	require.Equal(t, 120.0, resolveWebHTTPUnaryTimeout())
	require.Equal(t, 60.0, resolveWebHTTPHistoryTimeout())
}

func TestResolveWebHTTPTimeoutsenv覆盖与非法值(t *testing.T) {
	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "5")
	require.Equal(t, 5.0, resolveWebHTTPUnaryTimeout())
	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "abc")
	require.Equal(t, 120.0, resolveWebHTTPUnaryTimeout()) // 非数字回默认
	t.Setenv("GATEWAY_WEB_HTTP_UNARY_TIMEOUT", "-1")
	require.Equal(t, 120.0, resolveWebHTTPUnaryTimeout()) // 非 allow_zero 档 ≤0 回默认
	t.Setenv("GATEWAY_WEB_HTTP_SSE_TIMEOUT", "0")
	require.Equal(t, 0.0, resolveWebHTTPSSETimeout()) // allow_zero 档 0 合法
}
```

- [ ] **Step 2: 跑测试确认失败**（`go test ./internal/swarm/gateway/channel_manager/web/ -run TestResolveWebHTTPTimeouts -v` → undefined）
- [ ] **Step 3: 一比一移植**：`resolveEnvFloat(name, default, allowZero)` 私有助手（:10-20）+ 五个导出函数 `ResolveWebHTTPSSETimeout/SSEIdleTimeout/SSEKeepalive/UnaryTimeout/HistoryTimeout`（:23-49，语义注释逐条搬）
- [ ] **Step 4: 测试通过 + gofmt + commit** `feat(27a-b2): 移植 web_http_timeouts 五档超时`

### Task 2: outbound 三符号 web_outbound_http.go（B3 前置）

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_outbound_http.go`
- Test: `internal/swarm/gateway/channel_manager/web/web_outbound_http_test.go`

Python 真相源：`outbound.py:21-374`。Go 侧已有 WS outbound 通道注册链（`register_request_outbound`/`unregister_request_outbound` 对应物，B1 产物）——先 grep 确认其接口形状再接。

- [ ] **Step 1: 写失败测试**

```go
func TestBindHTTPSession(t *testing.T) {
	// outbound.py:32-70 四分支
	env, out := bindHTTPSession("session.create", map[string]any{}, "", true)
	require.NotEmpty(t, env)                       // webhttp_ 前缀随机
	require.NotEmpty(t, out["create_token"])       // session.create 自动补 token（:50-51）

	env2, out2 := bindHTTPSession("cron.job.list", map[string]any{"session_id": "s1"}, "XSID", true)
	require.Equal(t, "s1", env2)                   // client_sid 优先于 header（:47）
	require.Equal(t, "s1", out2["session_id"])

	_, out3 := bindHTTPSession("config.get", map[string]any{}, "XSID", false)
	_, has := out3["session_id"]                   // bind_param=false 且无 client_sid → 不注入（:60-65）
	require.False(t, has)
}

func TestBindHTTPSessionSharedUserRoute(t *testing.T) {
	// outbound.py:21-29：无 client_sid 且 agent_templates.* / !bind_param → 按用户共用
	env, _ := bindHTTPSession("agent_templates.list", map[string]any{"user_id": "u-1"}, "", true)
	require.Equal(t, "shared_user_u-1", env)
}

func TestHttpJSONOutboundWaitResponse(t *testing.T) {
	ob := newHTTPJSONOutbound("", "")
	go ob.acceptFrame(map[string]any{"type": "res", "id": "r1", "ok": true, "payload": map[string]any{}})
	frame := ob.waitResponse("r1", 1*time.Second)
	require.True(t, frame["ok"].(bool))
}

func TestHttpJSONOutboundChatError不打空等(t *testing.T) {
	// outbound.py:257-269：单次请求被收成 chat.error 事件 → 直接转 res 失败帧
	ob := newHTTPJSONOutbound("", "")
	go ob.acceptFrame(map[string]any{"type": "event", "event": "chat.error",
		"payload": map[string]any{"request_id": "r1", "error": "boom", "code": "BAD_REQUEST"}})
	frame := ob.waitResponse("r1", 1*time.Second)
	require.False(t, frame["ok"].(bool))
	require.Equal(t, "BAD_REQUEST", frame["code"])
}

func TestHttpJSONOutboundTimeout(t *testing.T) {
	ob := newHTTPJSONOutbound("", "")
	frame := ob.waitResponse("r1", 50*time.Millisecond)
	require.Equal(t, "TIMEOUT", frame["code"]) // :227-245 超时帧形状
}

func TestIsSSEEndFrame个人版语义(t *testing.T) {
	// outbound.py:73-93：个人版 chat.final 终止；processing_status(false) 不终止
	require.True(t, isSSEEndFrame(map[string]any{"event": "chat.final"}, ""))
	require.False(t, isSSEEndFrame(map[string]any{"event": "chat.processing_status",
		"payload": map[string]any{"is_processing": false}}, ""))
	require.True(t, isSSEEndFrame(map[string]any{"event": "chat.error"}, ""))
	require.True(t, isSSEEndFrame(map[string]any{"event": "history.message",
		"payload": map[string]any{"status": "done"}}, ""))
}

func TestHttpSSEOutboundIterSSEFramesKeepalive(t *testing.T) {
	// outbound.py:299-366：idle_timeout=0 时每 keepalive 周期产注释帧；收到终止帧后结束
	// （测试用 0.5s keepalive + 1s 截断，断言至少一次 keepalive 帧）
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 一比一移植**：
  - `bindHTTPSession(method string, params map[string]any, headerSessionID string, bindParam bool) (string, map[string]any)`（:32-70）+ `sharedUserRouteSessionID`（:21-29）+ `_METHODS_WITHOUT_PARAM_SESSION_ID` 常量集（grep outbound.py 顶部确认成员表）
  - `httpOutboundBase`：requestHeaders/remoteAddress/path/sessionID/closed/outboundID/`jiuwenWsID`（:128-148）、`reply`（:150-169）、`emit`（:171-184）、`send`（:189-195，`normalizeFrame` :96-110）、`close`
  - `httpJSONOutbound`：256 容量带锁队列、`acceptFrame`、`waitResponse(reqID, timeout)`（:216-269，TIMEOUT 帧/chat.error 转换逐分支）
  - `httpSSEOutbound`：`iterSSEFrames(reqID, timeout, idleTimeout, keepalive)`（:299-366，含 idle>0 时超窗产 `chat.error` 帧、keepalive 帧形状）
  - `isSSEEndFrame`（:73-93，**个人版分支**：`is_enterprise()` 对应物恒 false，偏差 D-B2-5 注释标注）
- [ ] **Step 4: 测试通过 + commit** `feat(27a-b2): 移植 outbound 三符号（B3 前置）`

### Task 3: mapped 路由表 http_routes_mapped.go

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/http_routes_mapped.go`
- Test: `internal/swarm/gateway/channel_manager/web/http_routes_mapped_test.go`

Python 真相源：`http_routes/mapped.py`（965 行）。本任务只做**表与校验**，不做挂载（Task 5 挂）。

- [ ] **Step 1: 写失败测试**

```go
func TestValidateMappedRoutes无重复面(t *testing.T) {
	require.NotPanics(t, func() { validateMappedRoutes(mappedRoutes) })
	// 对齐 Python validate_mapped_routes() 模块加载即校验（:965）
}

func TestMappedRoutes总量与分组对拍Python(t *testing.T) {
	// 数量断言必须机器核对（feedback-计数断言）：
	// python3 -c "from gateway.channel_manager.web.http_routes.mapped import MAPPED_ROUTES; print(len(MAPPED_ROUTES))"
	// Go 侧 require.Len(t, mappedRoutes, <上一步输出>)
}

func TestCatalogEntries含CORE与multipart专行(t *testing.T) {
	rows := catalogEntries(mappedRoutes)
	// catalog_entries（mapped.py:918-962）：core 12 行 + settings 组 + workspace 组 + harness multipart 专行
	require.Len(t, rows, len(mappedRoutes)+12+1)
}

func TestWorkspaceFileRoutes五行(t *testing.T) {
	// mapped.py:794-837：POST/DELETE media、POST documents、GET documents/formats、
	// POST files/import、GET tree/usage、DELETE entries、GET preview
	// 逐条断言 http_method+path+rpc_method+bind_session_param 与 Python 一致
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 移植**：
  - `webHTTPMappedRoute` struct（:16-41，`__post_init__` 校验→构造函数 `newWebHTTPMappedRoute`）
  - 十二组路由表常量（命名对齐 Python：configRoutes/a2aIngressRoutes/…/workspaceFileRoutes），**逐行照搬 Python 字面**（http_method/path/rpc_method/tag/summary/path_to_param/query_keys/accept_body/bind_session_param/created/extra_params）
  - `mappedRoutesForEdition`（:855，恒个人版表，D-B2-5）+ `CORE_ROUTE_CATALOG`（:870-883）+ `validateMappedRoutes`（:886-893，path 参数归一 :896-903）+ `catalogEntries`（:918-962）
- [ ] **Step 4: 用 Python 解释器机器对拍行数与 (method,path,rpc) 三元组集合**（命令见测试内注释），diff 为零
- [ ] **Step 5: 测试通过 + commit** `feat(27a-b2): mapped 路由表全量照搬 + catalog`

### Task 4: 分发面 web_http_dispatch.go

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_http_dispatch.go`
- Test: `internal/swarm/gateway/channel_manager/web/web_http_dispatch_test.go`

Python 真相源：`web_http_dispatch.py`（228 行）。

- [ ] **Step 1: 写失败测试**

```go
func TestTrustClientTenantHeaders(t *testing.T) {
	require.True(t, trustClientTenantHeaders("127.0.0.1", ""))
	require.True(t, trustClientTenantHeaders("::1", ""))
	require.True(t, trustClientTenantHeaders("localhost", ""))
	require.False(t, trustClientTenantHeaders("192.168.1.5", "")) // 非回环不信任
	t.Setenv("GATEWAY_WEB_HTTP_TRUST_CLIENT_HEADERS", "1")
	require.True(t, trustClientTenantHeaders("10.0.0.9", ""))     // env 显式开关（:48-54）
	t.Setenv("GATEWAY_WEB_HTTP_TRUST_CLIENT_HEADERS", "0")
	require.False(t, trustClientTenantHeaders("127.0.0.1", ""))
}

func TestResolveHTTPRoutingIdentity非回环置空(t *testing.T) {
	h := map[string]string{"X-User-Id": "u1", "X-Group-Id": "g1"}
	require.Empty(t, resolveHTTPRoutingIdentity(h, "8.8.8.8"))
	got := resolveHTTPRoutingIdentity(h, "127.0.0.1")
	require.Equal(t, "u1", got["user_id"]) // normalize_routing_identity 归一化
}

func TestDispatchHTTPRequest构造Message(t *testing.T) {
	// 用 fake WebChannel（B1 测试基建）验证：
	// outbound 已注册、Message.req_method/mode/is_stream/channel_id/app_id/user_id/metadata.ext
	// session.bind 后 envelope sid、X-Channel-Id 缺省 web、X-App-Id 缺省 default（:130-131）
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 移植**：`trustClientTenantHeaders`（:48-60）、`resolveHTTPRoutingIdentity`（:82-94，对齐 `request_identity.normalize_routing_identity` 既有 Go 对应物——先 grep `internal/` 确认名字）、`buildHTTPExt`（:63-79，企业段不移植 D-B2-6）、`dispatchHTTPRequest`（:97-228，逐段：header map→reqID→bindHTTPSession→outbound 构造→register→session.create 审计日志（:148-163 对齐为 logger.Info）→query 组装→`apply_routing_metadata` 对应物→Message 构造（:202-217）→`invokeWebRequest` 对应物）
- [ ] **Step 4: 测试通过 + commit** `feat(27a-b2): HTTP 分发面 dispatch + 信任边界`

### Task 5: CORE 路由与 envelope web_http_routes.go（上）

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_http_routes.go`
- Modify: `internal/swarm/gateway/app_gateway.go:673-692`（setupRouter 增 `/api/v1`）
- Test: `internal/swarm/gateway/channel_manager/web/web_http_routes_test.go`

Python 真相源：`web_http_routes.py`。

- [ ] **Step 1: 写失败测试**

```go
func TestEnvelopeFromRes错误码映射(t *testing.T) {
	// web_http_routes.py:177-208 映射表全分支表驱动：
	// BAD_REQUEST 400 / UNAUTHORIZED 401 / FORBIDDEN 403 / NOT_FOUND 404 /
	// METHOD_NOT_FOUND 404 / CONFLICT 409 / TIMEOUT 504 / SERVICE_UNAVAILABLE 503 /
	// WORKSPACE_QUOTA_EXCEEDED 403 / 未知 500 / ok → 200（created→201）
}

func TestChatIdentityConflict(t *testing.T) {
	// :640-666 四字段 × (header空/body空/相等/不等) 表驱动
	// 不等 → 400 IDENTITY_CONFLICT + details.field
	// 仅 chat.send/chat.resume/chat.user_answer 触发，其余方法恒 nil
}

func TestCoerceQueryValue(t *testing.T) {
	// :823-837："true"→true/"yes"→true/"false"→false/"no"→false/"1"→int 1/"-2"→int -2/"abc"→str
}

func TestHTTPSessionCRUD全链(t *testing.T) {
	// httptest + fake channel：POST /api/v1/sessions 201（body 可空，create_token 注入）
	// GET /sessions/{id} → session.get_metadata；PATCH 双分流（pinned→pin 否则 rename）
	// DELETE → session.delete；GET /health → ready；GET /connection/status
}

func TestHistoryGetCursor协议(t *testing.T) {
	// :131-163：有 cursor → 游标转发；有 limit 无 page_idx → 游标；空白 cursor → null 首页
	// JSON 收集：history.message 聚合 + status=done 结束 + chat.error→404 NOT_FOUND（_history_json :522-607）
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 移植**（分两文件内声明块）：
  - `envelopeFromRes`（:177-208）、`responseHeaders`、`webHTTPMetadata`、`chatIdentityError`（:640-666）、`coerceQueryValue`（:823-837）、`mergeHeaderParams`（:840-848，`_HEADER_TO_PARAM` :36-42）、`paramsFromMappedRoute`（:851-877）
  - `registerWebHTTPAPIRoutes`（:216-519）：health、connection_status、cron_runs_live（SSE + `cron_live_bus` 对应物——先 grep Go 侧 cron 通知总线，缺则登记缺口）、sessions CRUD、history_get、session_live、catalog、harness_import_file（multipart 50MB :487-517）、`registerMappedRoutes`（:880-925，静态路径先于参数路径）
  - app_gateway.go：`r.Route("/api/v1", ...)` 挂载 + doc.go 文件树同步
- [ ] **Step 4: 测试通过 + commit** `feat(27a-b2): CORE 路由 + envelope + identity 校验`

### Task 6: _unary/_stream/_history_json 与 chat SSE（下）

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_http_routes.go`
- Test: 同上测试文件追加

- [ ] **Step 1: 写失败测试**

```go
func TestUnaryHTTPChatSend非流式(t *testing.T) {
	// enable_streaming=false → _unary（is_stream=false）；envelope data/metadata 形状
}

func TestStreamChatSendSSE帧序列(t *testing.T) {
	// :743-820：res ok→web.response 事件；res !ok→chat.error 后终止；event 帧原名透传；
	// keepalive 注释帧；响应头 Cache-Control/X-Accel-Buffering/X-Request-Id/X-Web-RPC-Method
}

func TestStreamChatResume与UserAnswer(t *testing.T) {
	// :431-465：resume/user_answer 恒 is_stream=true
}

func TestHistoryGetSSEAccept双形态(t *testing.T) {
	// Accept: text/event-stream → _stream(history.get)；否则 _history_json
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 移植** `unary`（:669-740，含 audit 异常路径→Error 日志）、`stream`（:743-820）、`historyJSON`（:522-637）、`_STREAM_RPC_METHODS`（:54）
- [ ] **Step 4: 测试通过 + commit** `feat(27a-b2): unary/stream/history 三分发通道`

### Task 7: workspace 面板 server 侧 handler

**Files:**
- Create: `internal/swarm/server/runtime/gateway_adapter/workspace_panel_zones.go`
- Create: `internal/swarm/server/runtime/gateway_adapter/workspace_panel_service.go`
- Modify: `internal/swarm/server/runtime/gateway_adapter/workspace_file_adapter.go`（方法表 + `_handle_workspace` 分发）
- Test: `internal/swarm/server/runtime/gateway_adapter/workspace_panel_service_test.go`

Python 真相源：`jiuwenswarm/enterprise/capabilities/workspace/{zones,service}.py` + `workspace_file_adapter.py:199-238`。

- [ ] **Step 1: 写失败测试**

```go
func TestZones白名单与归一(t *testing.T) {
	// zones.py 一比一：DELETABLE_ZONES 仅 session_workspace/session_todo/artifact
	// classifyZone：attachments/uploads/config 等禁删 zone 判定
	// normalizeRelativePath：".." 逃逸/绝对路径/空段 → error（防路径逃逸，27.8 O_NOFOLLOW 先例）
}

func TestWorkspaceTree(t *testing.T) {
	// service.list_tree：TempDir 造 session_workspace/artifact/config 三 zone
	// 断言树项含 zone/deletable；config 项 deletable=false
}

func TestWorkspacePreview(t *testing.T) {
	// 文本 → content；二进制（magic byte 判定，对齐 service.py is_binary）→ content=null
	// max_bytes 默认 64KiB 上限 1MiB；目录 → WorkspaceError 400
}

func TestWorkspaceEntriesDelete(t *testing.T) {
	// 白名单内删除成功；白名单外 → WorkspaceError(403 码)；深度 32/条目 5000 上限（:26-28 常量）
}

func TestWorkspaceUsage单机(t *testing.T) {
	// du 快照形状对齐 snapshot_to_dict（无 tenant 字段值置空）——D-B2-4
}

func TestAdapter分发四方法(t *testing.T) {
	// workspace_file_adapter 方法表增 WORKSPACE_TREE/PREVIEW/ENTRIES_DELETE/USAGE
	// dispatch → _handleWorkspace；WorkspaceError → build_error_response(exc.code)
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 移植**：zones 全量（DELETABLE_ZONES/classify_zone/is_deletable/normalize_relative_path）、service 核心四方法（list_tree/preview_file/delete_entries/usage，先读 Python service.py 对应方法逐段对齐，subprocess du → Go filepath.WalkDir 等价实现并登记偏差）、adapter `_handleWorkspace`（:199-238，含 `asyncio.to_thread` → 直接同步调用登记说明）
- [ ] **Step 4: 测试通过 + commit** `feat(27a-b2): workspace 面板四 method（单机版）`

### Task 8: container_file 下载半区

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/container_file_http_download.go`
- Test: `internal/swarm/gateway/channel_manager/web/container_file_http_download_test.go`

Python 真相源：`container_file_http.py:483-560`（verified）+ :527-836（serve_file_chunks）+ :838-850（/download 路由）+ token 函数。

- [ ] **Step 1: 写失败测试**

```go
func TestDecodeDownloadTokenPayload(t *testing.T) {
	// 合法 27.8 签发 token → payload{sid,kind,...}；篡改/缺字段 → nil → BAD_REQUEST
}

func TestServeFileChunksHEAD与Range(t *testing.T) {
	// HEAD → 200 + Content-Length + Content-Disposition（filename*=UTF-8''）
	// Range: bytes=2-5 → 206 局部；bytes=99999- → 416 + Content-Range: bytes */total
	// 多区间 Range → 416 或取首区间（对齐 _parse_single_byte_range，先读源码定死）
}

func TestServeVerifiedDownloadE2A分块(t *testing.T) {
	// fake agent_client：file.download_verified_chunk 返回块序列
	// 校验 offset/limit 回读一致性（_decode_verified_chunk）；eof 终止；全量拼装与源字节一致
}

func TestDownload路由verified与location分流(t *testing.T) {
	// kind=verified_asset → serveVerifiedDownload
	// location 分支（容器）→ BAD_REQUEST（D-B2-2）
	// 缺 user_id → 400 "user_id is required"（:852-856）
}
```

- [ ] **Step 2: 跑测试确认失败**
- [ ] **Step 3: 移植**：`_decode_download_token_payload`/`_VERIFIED_ASSET_TOKEN_KIND`（对齐 27.8 签发侧 `internal/swarm/server/runtime/attachments/` 的 token 形状——先 grep 签发函数确认 HMAC 参数与字段名）、`serveVerifiedDownload`（:483-524，`fetch_agent_unary` → Go E2A 代理对应物，grep `routing/e2a` 确认）、`serveFileChunks`（:527-700+，HEAD/Range/416/流式）、`/download` 路由注册进 Task 5 的 /api/v1 路由组（Python 挂 `file-api/download` 与 `api/v1` 双前缀——以 attach_container_file_routes 源码为准逐条对齐）
- [ ] **Step 4: 测试通过 + commit** `feat(27a-b2): 下载半区 verified 链 + Range 语义`

### Task 9: 集成测试 tests/integration/swarm/web/

**Files:**
- Create: `tests/integration/swarm/web/http_dispatch_e2e_test.go`（suite + 真实网关进程，模式对齐 `media_gateway_e2e_test.go`）

- [ ] **Step 1: 写集成测试**（每函数 ≥2 断言，HTTP 真请求走 127.0.0.1 网关端口）：

```go
func (s *HTTPDispatchSuite) TestHTTP全链会话多轮() {
	// POST /api/v1/sessions(201) → POST /chat/completions SSE（MockLLM 两轮）
	// 断言 SSE 帧：web.response(accepted) → 事件流 → chat.final
	// → GET /sessions/{id}/history（JSON：messages 聚合 + total_pages）
	// → GET 带 cursor 续拉 → POST interrupt → session.delete
}
func (s *HTTPDispatchSuite) TestHTTP带附件() {
	// multipart POST /sessions/{id}/media → 落盘回读（对齐 27.8 store_image_item 断言口径）
	// → chat.send 引用素材 → 历史回读 attachment 语义
}
func (s *HTTPDispatchSuite) TestHTTPSessionCRUD() {
	// list/get_metadata/rename/pin/delete + PATCH 双分流
}
func (s *HTTPDispatchSuite) TestHTTPWorkspace操作() {
	// tree → preview(文本/二进制/目录 400) → entries_delete(白名单内外) → usage
}
func (s *HTTPDispatchSuite) TestHTTP下载Verified() {
	// 造 sealed 资产 → /download?token 全量流 = 源字节 → Range 断点 → 416 → 篡改 token 400
}
func (s *HTTPDispatchSuite) TestHTTPMETHODNOTFOUND与非回环匿名() {
	// skills.marketplace.list（RPC 未实现）→ 404 envelope METHOD_NOT_FOUND
	// 伪装非回环来源（RemoteAddr 改写不可行→单测已覆盖，此处验 envelope 头 X-Request-Id 恒在）
}
```

- [ ] **Step 2: 审计自检** `go run ./cmd/integration-audit -verify`（新目录调用跨度 ≥3，不满足则把 workspace server 侧与 gateway 断言并进同文件）
- [ ] **Step 3: race 复跑** `go test -race -count=2 -tags "sqlite_fts5 test" ./tests/integration/swarm/web/`
- [ ] **Step 4: commit** `test(27a-b2): HTTP 分发面集成 + e2e 全链`

### Task 10: catalog 对拍与 e2e 契约

**Files:**
- Test: `tests/integration/swarm/web/http_catalog_test.go`

- [ ] **Step 1: catalog 逐条对拍**：集成测试内请求 `GET /api/v1/catalog`，与 Task 3 机器对拍产物（Python `catalog_entries` JSON 落盘 fixture `testdata/b2_catalog.json`，注明生成命令与 Python 版本）逐行 diff（method+path+rpc_method+bind_session_param）
- [ ] **Step 2: e2e 前端契约抽查**：`webHttpClient.ts` http 档路由表（上游 `frontend/src/services/webHttpClient.ts:142/189` 等样本行）抽 10 条高频（sessions/chat/history/workspace/skills.get/config.get）与 Go catalog 匹配
- [ ] **Step 3: commit** `test(27a-b2): catalog 契约对拍`

### Task 11: plan 同步、方法清单对照与收尾

- [ ] **Step 1: 方法清单对照**（规则 12.4）：逐 Python 文件（web_http_routes/dispatch/timeouts/outbound HTTP 段/mapped/container 下载半区/workspace service+zones+adapter）列函数对照表，补入 spec §9；Go 侧多出/缺失逐条说明
- [ ] **Step 2: IMPLEMENTATION_PLAN.md 同步**：B2 行 ✅（五件回填逐项销账）+ B3/B4 行回收标注 + 27.11 行改判 + 27.8 行 D-27.8-7 更新（**保留两行 ⤴️ 原文记录不删**，按规则 2 追加式修订）
- [ ] **Step 3: doc.go 三处**：`channel_manager/web/doc.go`、`runtime/gateway_adapter/doc.go` 文件树增删
- [ ] **Step 4: 全量门禁**：`go build ./...` && `go vet ./...` && `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（对照 Task 0 基线，只允许既有红）+ `go run ./cmd/integration-audit -verify`
- [ ] **Step 5: push worktree 分支** `git push -u origin worktree-feat/27a-b2-http-dispatch`（规则 6/11：推送即交付，不追 CI）

---

## 风险与前置确认

1. **invoke/出站通道对应物**：Task 4/5 依赖 B1 的 `dispatch_web_request`→Go 侧对应函数与 outbound 注册链，Task 2 动手前先 grep `invoke_web_request`/`registerRequestOutbound` 确认签名，缺件在 Task 2 内补齐并登记。
2. **cron_live_bus**：`cron_runs_live`/`session_live` SSE 依赖 cron 运行通知总线，若 Go 侧无对应物，本批只落路由与 envelope、总线挂 ⤵️ 登记到领域二十五 cron 行（spec 偏差追加 D-B2-9）。
3. **token 签发侧形状**：Task 8 必须先 grep 27.8 的 `attachments/` 签发函数，验签以既有实现为准，不重造 HMAC。
4. **`_METHODS_WITHOUT_PARAM_SESSION_ID`**：Task 2 前从 outbound.py 顶部抄成员表，逐条对齐。

## Self-Review 记录

- Spec 覆盖：§3 文件映射→T1-T8；§4 行为要点→T5/T6 步骤；§5 回填→T3/T7/T8 + T11 销账；§7 测试→T1-T10；§8 plan 同步→T11。偏差 D-B2-1/7（auth 组）无任务=有意不移植，spec 已登记。✅
- 占位符扫描：无 TBD；"先 grep 确认"为防漂移动作而非缺口，已列入风险节。✅
- 类型一致性：`bindHTTPSession`/`waitResponse`/`iterSSEFrames` 等签名在 T2 定义、T4-T8 使用一致。✅
