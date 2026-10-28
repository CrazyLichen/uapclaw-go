# CLI 路由全链路往返集成测试 + 11.15 最小生产装配 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 生产代码装配 /tui CLI 路由（ForwardMethods 集合 + 5 个本地 ack handler + DisconnectHandler），并构建 GatewayServer→AgentServer→LLM mock 的单进程全链路往返集成测试（双 mode 覆盖）。

**Architecture:** 新增 `tui_route.go` 承载路由装配（对齐 Python tui_connect.py 的集合与 handler 定义，gateway 包内因循环依赖约束）；`AppGatewayServer.Start` 接线；集成测试自建 7 件套中间层（消费生产 `BuildTuiRouteConfig`，绕过 AppGatewayServer 的 fs 重依赖），mock 注入点在 `llm.ClientRegistry`。

**Tech Stack:** Go + gorilla/websocket + testify/suite + 现有 mockllm/suite 基建。

**Spec:** `docs/superpowers/specs/2026-10-28-cli-routing-fullchain-design.md`（含机器核对数据附录 A）

**编译纪律（每个任务）:** 编译前 `pgrep -f 'go (build|test)'` 检查残留进程，如有先 kill；同一时间只跑一个 go 编译进程；绝不 `go clean -cache`。

---

## 事件序列基准（测试断言依据，Go 实际链路推导）

`chat.send`（强制流式）单 chunk 无工具单轮，WS 客户端实际收到：

1. `res` 帧：`{type:"res", id:<reqID>, ok:true, payload:{accepted:true, session_id:<sid>}}` — 本地 handler ack（handleRawMessage 转发后落 LocalHandlers 分支）
2. `event` 帧 `chat.processing_status` `{is_processing:true}` — **gateway 侧**发出（forward_loop.go:239，`shouldEmitProcessingStatusForStream` 对 chat.send 恒 true）
3. `event` 帧 `chat.delta` `{content:<mock 文本>}` — DeepAdapter llm_output 分支
4. `event` 帧 `chat.usage_metadata` — DeepAdapter llm_usage 分支（无条件，token 全 0）
5. `event` 帧 `chat.final` — answer chunk → ParseStreamChunk(hasStreamedContent=true)
6. `event` 帧 `chat.processing_status` `{is_processing:false}` — **gateway 侧** streamFinalCleanup 补发（AgentServer 未发过 false）

不发：`chat.usage_summary`（TotalTokens=0）、`chat.reasoning`（无 reasoning chunk）。

---

### Task 1: writeResponseFrame 提取重构（gateway_server.go）

**Files:**
- Modify: `internal/swarm/gateway/gateway_server.go`（sendResponse L597-626）
- Test: `internal/swarm/gateway/tui_route_test.go`（Task 3 建，此处不建）

- [x] **Step 1.1: sendResponse 中提取包级函数**

将现有：

```go
// sendResponse 发送 res 帧。
//
// Python: send_response（L332-356）——发送失败仅 Debug。
func (s *GatewayServer) sendResponse(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	if payload == nil {
		payload = map[string]any{}
	}
	frame := map[string]any{
		"type":    "res",
		"id":      reqID,
		"ok":      ok,
		"payload": payload,
	}
	if !ok {
		if errMsg == "" {
			errMsg = "request failed"
		}
		frame["error"] = errMsg
		if code != "" {
			frame["code"] = code
		}
	}
	data, _ := json.Marshal(frame)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Debug(logComponent).
			Err(err).
			Str("method", "send_response").
			Msg("send failed (client disconnected?)")
	}
}
```

改为（sendResponse 薄包装 + 包级 writeResponseFrame）：

```go
// sendResponse 发送 res 帧。
//
// Python: send_response（L332-356）——发送失败仅 Debug。
func (s *GatewayServer) sendResponse(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	writeResponseFrame(ws, reqID, ok, payload, errMsg, code)
}

// writeResponseFrame 构造并写入 res 帧（包级，供 tui 本地 handler 复用）。
//
// 零行为变更：帧构造与写入逻辑自 sendResponse 提取（11.15 最小装配）。
func writeResponseFrame(ws *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	if payload == nil {
		payload = map[string]any{}
	}
	frame := map[string]any{
		"type":    "res",
		"id":      reqID,
		"ok":      ok,
		"payload": payload,
	}
	if !ok {
		if errMsg == "" {
			errMsg = "request failed"
		}
		frame["error"] = errMsg
		if code != "" {
			frame["code"] = code
		}
	}
	data, _ := json.Marshal(frame)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Debug(logComponent).
			Err(err).
			Str("method", "send_response").
			Msg("send failed (client disconnected?)")
	}
}
```

注意：按规范 2，`writeResponseFrame` 是非导出函数，须放文件"非导出函数"区块（现有 `probeWritable`/`resolveRoute` 等附近）；`sendResponse` 留在导出区块不动位置。

- [x] **Step 1.2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/gateway/`
Expected: 无输出（成功）

- [x] **Step 1.3: 既有单测全跑**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/gateway/ 2>&1 | tail -3`
Expected: `ok` （零行为变更重构，既有测试全绿）

- [x] **Step 1.4: Commit**

```bash
git add internal/swarm/gateway/gateway_server.go
git commit -m "refactor: 提取 writeResponseFrame 供 tui 本地 handler 复用（零行为变更）"
```

---

### Task 2: tui_route.go 集合与 handler（生产核心）

**Files:**
- Create: `internal/swarm/gateway/tui_route.go`
- Test: `internal/swarm/gateway/tui_route_test.go`（Task 3 建）

- [x] **Step 2.1: 创建 tui_route.go 完整文件**

对齐 Python `tui_connect.py` L163-330（集合）+ L1402-1433（5 handler）+ L2093-2119（binding）。注意按规范 2 的声明顺序（结构体→枚举→常量→全局变量→导出函数→非导出函数）与规范 1（全中文注释）：

```go
package gateway

import (
	"context"

	"github.com/gorilla/websocket"

	messagehandler "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TuiRouteParams TUI 路由装配参数。
//
// 对齐 Python CliRouteBindParams（tui_connect.py L343-350）裁剪：
// 仅保留 DisconnectHandler 所需的 message_handler；agent_client/
// on_config_saved/cron_controller 属纯本地 handler（config.*/cron.*），
// ⤵️ 11.15 完整范围回填时再扩展。
type TuiRouteParams struct {
	// MessageHandler 断连取消会话的处理器（可 nil，nil 时 DisconnectHandler 跳过）
	MessageHandler *messagehandler.MessageHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTuiRouteConfig 构建 /tui CLI 路由配置。
//
// 对齐 Python build_cli_route_binding（tui_connect.py L2093-2119）：
// forward 集合 80 项、no-local 集合 75 项（差集=5 个 chat 类方法，
// 计数已机器核对 2026-10-28，见 spec 附录 A）。
//
// 与 Python 目录结构的有意偏差：Python tui_connect.py 属 channel_manager
// 子包，但 Go 端 gateway → channel_manager 依赖方向已存在，tui 装配
// 放 gateway 包。
func BuildTuiRouteConfig(params TuiRouteParams) *RouteConfig {
	return &RouteConfig{
		Path:                         "/tui",
		ChannelID:                    "tui",
		ForwardMethods:               TuiForwardReqMethods,
		ForwardNoLocalHandlerMethods: TuiForwardNoLocalHandlerMethods,
		LocalHandlers: map[string]LocalHandlerFunc{
			"chat.send":        tuiChatSendHandler,
			"chat.resume":      tuiChatResumeHandler,
			"chat.interrupt":   tuiChatInterruptHandler,
			"chat.user_answer": tuiChatUserAnswerHandler,
			"history.get":      tuiHistoryGetHandler,
		},
		DisconnectHandler: tuiDisconnectHandler(params.MessageHandler),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tuiChatSendHandler chat.send 本地 ack。
//
// Python: _chat_send（tui_connect.py L1402-1405）——仅返回 ack，
// 消息转发由 ForwardMethods 集合路径完成。
func tuiChatSendHandler(ws *websocket.Conn, reqID string, _ map[string]any, sessionID string) error {
	writeResponseFrame(ws, reqID, true, map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}, "", "")
	return nil
}

// tuiChatResumeHandler chat.resume 本地 ack。
//
// Python: _chat_resume（L1407-1410）。
func tuiChatResumeHandler(ws *websocket.Conn, reqID string, _ map[string]any, sessionID string) error {
	writeResponseFrame(ws, reqID, true, map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}, "", "")
	return nil
}

// tuiChatInterruptHandler chat.interrupt 本地 ack（intent 可选透传）。
//
// Python: _chat_interrupt（L1412-1417）。
func tuiChatInterruptHandler(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
	payload := map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}
	if intent, ok := params["intent"].(string); ok && intent != "" {
		payload["intent"] = intent
	}
	writeResponseFrame(ws, reqID, true, payload, "", "")
	return nil
}

// tuiChatUserAnswerHandler chat.user_answer 本地 ack（request_id 可选透传）。
//
// Python: _chat_user_answer（L1419-1424）。
func tuiChatUserAnswerHandler(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
	payload := map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}
	if rid, ok := params["request_id"].(string); ok && rid != "" {
		payload["request_id"] = rid
	}
	writeResponseFrame(ws, reqID, true, payload, "", "")
	return nil
}

// tuiHistoryGetHandler history.get 本地 ack（session_id 覆写 + page_idx 透传）。
//
// Python: _history_get（L1426-1433）。
func tuiHistoryGetHandler(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error {
	payload := map[string]any{
		"accepted":   true,
		"session_id": sessionID,
	}
	if sid, ok := params["session_id"].(string); ok && sid != "" {
		payload["session_id"] = sid
	}
	if v, ok := params["page_idx"]; ok {
		payload["page_idx"] = v
	}
	writeResponseFrame(ws, reqID, true, payload, "", "")
	return nil
}

// tuiDisconnectHandler 构造断连回调：桥接 CancelAgentSessionsOnDisconnect。
//
// Python: _tui_disconnect（tui_connect.py L2106-2110）——
// mh 为 None 或无 stale keys 时直接返回。
func tuiDisconnectHandler(mh *messagehandler.MessageHandler) DisconnectFunc {
	return func(_ *websocket.Conn, staleSessionKeys [][2]string) {
		if mh == nil || len(staleSessionKeys) == 0 {
			return
		}
		mh.CancelAgentSessionsOnDisconnect(context.Background(), staleSessionKeys)
	}
}
```

注意 import：`context`（tuiDisconnectHandler 用）+ messagehandler 包（导入路径 `github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler`，别名 `messagehandler`）。**无循环依赖**：message_handler 不导入 gateway（app_gateway.go 已有同向导入先例）。本文件不用 logger（writeResponseFrame 在 gateway_server.go 内已带日志）。

**TuiForwardReqMethods / TuiForwardNoLocalHandlerMethods 两个集合**（80/75 项，放"全局变量"区块）。从 Python 源机器导出（Task 2.2 生成），格式：

```go
// TuiForwardReqMethods /tui 路由转发到 Agent 的 method 集合。
//
// Python: CLI_FORWARD_REQ_METHODS（tui_connect.py L163-248），80 项。
var TuiForwardReqMethods = map[string]bool{
	"command.add_dir": true,
	// …（80 项，Task 2.2 生成完整清单）
}

// TuiForwardNoLocalHandlerMethods /tui 路由仅转发、无本地 handler 的集合。
//
// Python: CLI_FORWARD_NO_LOCAL_HANDLER_METHODS（L250-330），75 项。
// 差集（forward − no_local）恰为 5 个 chat 类方法。
var TuiForwardNoLocalHandlerMethods = map[string]bool{
	"command.add_dir": true,
	// …（75 项，Task 2.2 生成完整清单）
}
```

- [x] **Step 2.2: 机器生成两个集合的 Go 代码**

Run（在仓库根目录）:

```bash
python3 -c "
import re
src = open('/home/opensource/jiuwenswarm-develop/jiuwenswarm/gateway/channel_manager/tui/tui_connect.py').read()

def extract(name):
    m = re.search(name + r'\s*=\s*frozenset\(\s*\{(.*?)\}\s*\)', src, re.DOTALL)
    body = re.sub(r'#.*', '', m.group(1))
    return sorted(x.strip().strip('\"').strip(\"'\") for x in body.split(',') if x.strip())

for goname, pyname in [('TuiForwardReqMethods', 'CLI_FORWARD_REQ_METHODS'),
                        ('TuiForwardNoLocalHandlerMethods', 'CLI_FORWARD_NO_LOCAL_HANDLER_METHODS')]:
    items = extract(pyname)
    print(f'// {goname} /tui 路由转发集合（{len(items)} 项）。')
    print(f'// Python: {pyname}（tui_connect.py）。机器核对 2026-10-28。')
    print(f'var {goname} = map[string]bool{{')
    for it in items:
        print(f'\t\"{it}\": true,')
    print('}')
    print()
"
```

Expected: 输出两段 Go 代码（80 项 + 75 项）。将输出粘贴进 tui_route.go 的"全局变量"区块。

- [x] **Step 2.3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/gateway/`
Expected: 无输出（成功）

- [x] **Step 2.4: Commit**

```bash
git add internal/swarm/gateway/tui_route.go
git commit -m "feat: /tui CLI 路由装配（80/75 集合 + 5 本地 ack handler + DisconnectHandler）
```

---

### Task 3: 生产单测 tui_route_test.go

**Files:**
- Create: `internal/swarm/gateway/tui_route_test.go`
- Test: 同文件

- [x] **Step 3.1: 写失败测试（差集 + 字段完整性 + DisconnectHandler nil 安全）**

```go
package gateway

import (
	"testing"
)

// TestTuiForwardSetDiff 测试两集合差集恰为 5 个 chat 类方法。
func TestTuiForwardSetDiff(t *testing.T) {
	diff := make([]string, 0)
	for m := range TuiForwardReqMethods {
		if !TuiForwardNoLocalHandlerMethods[m] {
			diff = append(diff, m)
		}
	}
	want := []string{
		"chat.interrupt", "chat.resume", "chat.send",
		"chat.user_answer", "history.get",
	}
	if len(diff) != len(want) {
		t.Fatalf("差集大小 = %d, 期望 5；差集: %v", len(diff), diff)
	}
	set := make(map[string]bool)
	for _, m := range diff {
		set[m] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("差集缺少 %s", w)
		}
	}
}

// TestTuiForwardSetCounts 测试集合计数（80/75，机器核对基准）。
func TestTuiForwardSetCounts(t *testing.T) {
	if len(TuiForwardReqMethods) != 80 {
		t.Errorf("TuiForwardReqMethods = %d 项, 期望 80", len(TuiForwardReqMethods))
	}
	if len(TuiForwardNoLocalHandlerMethods) != 75 {
		t.Errorf("TuiForwardNoLocalHandlerMethods = %d 项, 期望 75", len(TuiForwardNoLocalHandlerMethods))
	}
}

// TestBuildTuiRouteConfig 测试路由配置字段完整性。
func TestBuildTuiRouteConfig(t *testing.T) {
	rc := BuildTuiRouteConfig(TuiRouteParams{})
	if rc.Path != "/tui" {
		t.Errorf("Path = %s, 期望 /tui", rc.Path)
	}
	if rc.ChannelID != "tui" {
		t.Errorf("ChannelID = %s, 期望 tui", rc.ChannelID)
	}
	for _, m := range []string{"chat.send", "chat.interrupt", "chat.resume", "chat.user_answer", "history.get"} {
		if _, ok := rc.LocalHandlers[m]; !ok {
			t.Errorf("LocalHandlers 缺少 %s", m)
		}
	}
	if len(rc.LocalHandlers) != 5 {
		t.Errorf("LocalHandlers = %d 个, 期望 5", len(rc.LocalHandlers))
	}
	if rc.DisconnectHandler == nil {
		t.Error("DisconnectHandler 不应为 nil")
	}
	if !rc.ForwardMethods["chat.send"] || rc.ForwardNoLocalHandlerMethods["chat.send"] {
		t.Error("chat.send 应在 forward 集合且不在 no-local 集合")
	}
}

// TestTuiDisconnectHandlerNilSafe 测试 nil MessageHandler 时安全跳过。
func TestTuiDisconnectHandlerNilSafe(t *testing.T) {
	h := tuiDisconnectHandler(nil)
	h(nil, [][2]string{{"tui", "sess-1"}}) // 不应 panic
	h(nil, nil)                            // 空 keys 不应 panic
}
```

- [x] **Step 3.2: 跑测试验证失败→通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/gateway/ -run 'TestTui|TestBuildTuiRouteConfig' -v 2>&1 | tail -15`
Expected: 4 个测试 PASS

- [x] **Step 3.3: 补 5 handler ack 行为测试（httptest + gorilla）**

追加测试（同文件）：

```go
// TestTuiLocalHandlersAck 用真实 WS 连接验证 5 个 handler 的 ack 帧。
func TestTuiLocalHandlersAck(t *testing.T) {
	cases := []struct {
		name    string
		handler LocalHandlerFunc
		params  map[string]any
		want    map[string]any // payload 断言（键→期望值）
	}{
		{
			name:    "chat.send",
			handler: tuiChatSendHandler,
			params:  map[string]any{},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:    "chat.interrupt 带 intent",
			handler: tuiChatInterruptHandler,
			params:  map[string]any{"intent": "cancel"},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack", "intent": "cancel"},
		},
		{
			name:    "chat.interrupt 无 intent",
			handler: tuiChatInterruptHandler,
			params:  map[string]any{},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:    "chat.user_answer 带 request_id",
			handler: tuiChatUserAnswerHandler,
			params:  map[string]any{"request_id": "req-9"},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack", "request_id": "req-9"},
		},
		{
			name:    "history.get session_id 覆写 + page_idx",
			handler: tuiHistoryGetHandler,
			params:  map[string]any{"session_id": "sess-override", "page_idx": 2},
			want:    map[string]any{"accepted": true, "session_id": "sess-override", "page_idx": float64(2)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// gorilla 服务器 + 客户端直连（net.Pipe 风格用 httptest）
			upgrader := websocket.Upgrader{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer ws.Close()
				if err := tc.handler(ws, "req-ack-1", tc.params, "sess-ack"); err != nil {
					t.Errorf("handler 返回错误: %v", err)
				}
			}))
			defer srv.Close()

			wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("连接失败: %v", err)
			}
			defer conn.Close()

			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("读帧失败: %v", err)
			}
			var frame map[string]any
			if err := json.Unmarshal(data, &frame); err != nil {
				t.Fatalf("帧 JSON 解析失败: %v", err)
			}
			if frame["type"] != "res" || frame["id"] != "req-ack-1" || frame["ok"] != true {
				t.Errorf("帧头不符: %v", frame)
			}
			payload, _ := frame["payload"].(map[string]any)
			for k, v := range tc.want {
				got := payload[k]
				// page_idx JSON 数字解出 float64（已知坑：YAML/JSON 数字双类型）
				if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", v) {
					t.Errorf("payload[%s] = %v, 期望 %v", k, got, v)
				}
			}
			// 无 intent/request_id 时不应出现该键
			if _, has := tc.want["intent"]; !has {
				if _, leaked := payload["intent"]; leaked {
					t.Errorf("payload 不应包含 intent")
				}
			}
		})
	}
}
```

import 追加：`encoding/json`、`fmt`、`net/http`、`net/http/httptest`、`strings`、`time`、`github.com/gorilla/websocket`。

- [x] **Step 3.4: 跑全部新测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/gateway/ -run 'TestTui|TestBuildTuiRouteConfig' -v 2>&1 | tail -20`
Expected: 全部 PASS（含子测试）

- [x] **Step 3.5: Commit**

```bash
git add internal/swarm/gateway/tui_route_test.go
git commit -m "test: tui_route 集合差集/计数/ack 行为/nil 安全单测"
```

---

### Task 4: AppGatewayServer 接线

**Files:**
- Modify: `internal/swarm/gateway/app_gateway.go`（L409-431 装配段）

- [x] **Step 4.1: 修改装配段**

将现有（app_gateway.go L409-431）：

```go
	gwCfg := NewGatewayServerConfig()
	if v := os.Getenv("GATEWAY_HOST"); v != "" {
		gwCfg.Host = v
	}
	if v := os.Getenv("GATEWAY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			gwCfg.Port = p
		}
	}
	s.gwServer = NewGatewayServer(gwCfg)

	// 对齐 Python L1056-1064：for binding in route_bindings
	routeBindings := []*routing.RouteBinding{} // A1：空（⤵️ 11.25/11.15 注入）
	for _, binding := range routeBindings {
		routeConfig := gwCfg.Routes[binding.Path]
		s.channelMgr.RegisterExternalChannel(routeConfig.ChannelID, s.gwServer)
		if binding.Install != nil {
			binding.Install(s.gwServer)
		}
	}
```

改为：

```go
	gwCfg := NewGatewayServerConfig()
	if v := os.Getenv("GATEWAY_HOST"); v != "" {
		gwCfg.Host = v
	}
	if v := os.Getenv("GATEWAY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			gwCfg.Port = p
		}
	}
	// 11.15 最小装配：/tui CLI 路由（对齐 Python L1045-1054 build_cli_route_binding）
	// 纯本地 handler（config.*/session.list/memory.*/cron.* 等 27 个）⤵️ 11.15 完整范围
	if s.msgHandler != nil {
		tuiRoute := BuildTuiRouteConfig(TuiRouteParams{MessageHandler: s.msgHandler})
		gwCfg.Routes[tuiRoute.Path] = tuiRoute
	}
	s.gwServer = NewGatewayServer(gwCfg)

	// 对齐 Python L1056-1064：for binding in route_bindings
	routeBindings := []*routing.RouteBinding{} // ACP ⤵️ 11.25（TUI 已改为直填 Routes，不走 binding）
	for _, binding := range routeBindings {
		routeConfig := gwCfg.Routes[binding.Path]
		s.channelMgr.RegisterExternalChannel(routeConfig.ChannelID, s.gwServer)
		if binding.Install != nil {
			binding.Install(s.gwServer)
		}
	}
	// 11.15：/tui 出站注册（对齐 Python register_external_channel("tui", gateway_server)）
	s.channelMgr.RegisterExternalChannel("tui", s.gwServer)
```

- [x] **Step 4.2: 编译 + gateway 包全测**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/gateway/ && go test ./internal/swarm/gateway/ 2>&1 | tail -3`
Expected: 编译成功 + `ok`

- [x] **Step 4.3: Commit**

```bash
git add internal/swarm/gateway/app_gateway.go
git commit -m "feat: AppGatewayServer 接线 /tui CLI 路由 + 出站 external channel 注册"
```

---

### Task 5: 集成测试目录与 doc.go

**Files:**
- Create: `tests/integration/swarm/gateway/doc.go`

- [x] **Step 5.1: 创建 doc.go（规范 1）**

```go
//go:build integration

// Package gateway_test 提供 GatewayServer 多路由链路的集成测试。
//
// 覆盖 CLI 路由（/tui）全链路往返：gorilla WS 客户端 → GatewayServer
// （CLI ForwardMethods + 本地 ack handler + tui 默认 mode 注入）→
// InboundGatewayServer → ChannelManager → MessageHandler → AgentClient →
// ChannelTransport（进程内）→ AgentServer → DeepAdapter → llm.Model →
// ClientRegistry MockModelClient，出站沿 robotMessages → externalChannels
// 回到 WS 客户端。对齐 Python tests/system_tests/test_cli_channel_ws.py。
//
// 文件目录：
//
//	gateway/
//	├── doc.go              # 包文档
//	└── cli_route_test.go   # CliRouteFullChainSuite 双 mode 全链路测试
//
// 对应 Python 代码：tests/system_tests/test_cli_channel_ws.py + jiuwenswarm/gateway/channel_manager/tui/tui_connect.py
package gateway_test
```

- [x] **Step 5.2: Commit**

```bash
git add tests/integration/swarm/gateway/doc.go
git commit -m "docs: 集成测试 gateway 目录 doc.go"
```

---

### Task 6: 集成测试主体 cli_route_test.go

**Files:**
- Create: `tests/integration/swarm/gateway/cli_route_test.go`

本任务代码量大，拆 4 步：骨架装配（6.1-6.5）、主测试（6.6-6.7）、副测试（6.8）、收尾（6.9）。

- [x] **Step 6.1: 写 suite 骨架 + 装配（完整代码）**

```go
//go:build integration

package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	mh "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CliRouteFullChainSuite CLI 路由全链路往返集成测试套件。
//
// 对齐 Python test_cli_channel_ws.py（进程内变体：不 spawn 真实进程，
// mock 注入点在 llm.ClientRegistry 边界）。
// 全局单例规避：suite 生命周期内 7 件套只 Start/Stop 一次，
// 两个 mode 测试共享装配（GetAgent 缓存 key 含 mode，两实例共存无冲突）。
type CliRouteFullChainSuite struct {
	isuite.BaseIntegrationSuite
	// 7 件套（SetupSuite 构造，TearDownSuite 停止）
	inboundServer *gateway.InboundGatewayServer
	gwServer      *gateway.GatewayServer
	channelMgr    *channel_manager.ChannelManager
	msgHandler    *mh.MessageHandler
	agentClient   *routing.AgentClient
	transport     *transport.ChannelTransport
	agentServer   *server.AgentServer
	// gwPort GatewayServer 随机端口
	gwPort int
	// providerName mock provider 注册名
	providerName string
	// envBackup 环境变量备份（[key, 原值, 是否存在]，TearDown 恢复）
	envBackup [][3]string
	// modeRecorder 工厂 wrapper 记录的 (mode, subMode)
	modeRecorder struct {
		mu    sync.Mutex
		modes []string
	}
}

// ──────────────────────────── 全局变量 ────────────────────────────

// TestCliRouteFullChainSuite 运行 CLI 路由全链路集成测试套件
func TestCliRouteFullChainSuite(t *testing.T) {
	suite.Run(t, new(CliRouteFullChainSuite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 装配 7 件套并启动
func (s *CliRouteFullChainSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	// ── mock provider 注册（runner_suite.go 先例模式）──
	// 注意：必须在 setupDataDir 之前（config.yaml 要写入此 provider 名）
	s.providerName = fmt.Sprintf("mock_cli_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	// ── 环境三件套（forbidden_test.go 先例模式）──
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.setupDataDir())
	pathutil.ResetCache()

	// ── 7 件套构造（自底向上）──
	// 1. Transport + AgentServer + 工厂 wrapper（复刻 defaultAgentFactory + mode 记录）
	s.transport = transport.NewChannelTransport()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.agentServer = server.NewAgentServer(cfg, s.transport)
	s.agentServer.SetAgentFactoryForTest(s.recordingAgentFactory)

	// 2. AgentClient（消费同一 Transport）
	s.agentClient = routing.NewAgentClient(s.transport)

	// 3. MessageHandler
	s.msgHandler = mh.NewMessageHandler(s.agentClient)

	// 4. ChannelManager（注入 MessageHandler）
	s.channelMgr = channel_manager.NewChannelManager(s.msgHandler, nil, nil)

	// 5. InboundGatewayServer（NormalizeAndForwardMessage → ChannelManager）
	s.inboundServer = gateway.NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		gateway.NormalizeAndForwardMessage(msg, s.channelMgr)
	})

	// 6. GatewayServer（随机端口 + 生产 tui 路由装配）
	s.gwPort = s.pickFreePort()
	gwCfg := gateway.NewGatewayServerConfig()
	gwCfg.Host = "127.0.0.1"
	gwCfg.Port = s.gwPort
	gwCfg.Routes["/tui"] = gateway.BuildTuiRouteConfig(gateway.TuiRouteParams{
		MessageHandler: s.msgHandler,
	})
	s.gwServer = gateway.NewGatewayServer(gwCfg)
	s.gwServer.OnMessage(s.inboundServer.HandleMessage)

	// 7. 出站注册（tui → GatewayServer）
	s.channelMgr.RegisterExternalChannel("tui", s.gwServer)

	// ── Start 顺序（对齐 app_gateway.go）──
	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.agentClient.Connect(s.Ctx))
	s.Require().NoError(s.msgHandler.StartForwarding(s.Ctx))
	s.Require().NoError(s.channelMgr.StartDispatch(s.Ctx))
	s.Require().NoError(s.inboundServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停止 + 清理
func (s *CliRouteFullChainSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.inboundServer != nil {
		s.inboundServer.Stop()
	}
	if s.channelMgr != nil {
		_ = s.channelMgr.StopDispatch()
	}
	if s.msgHandler != nil {
		_ = s.msgHandler.StopForwarding()
	}
	if s.agentClient != nil {
		s.agentClient.Disconnect()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	pathutil.ResetCache()
	s.restoreEnv()
	s.BaseIntegrationSuite.TearDownSuite()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordingAgentFactory 复刻 runtime.defaultAgentFactory（agent_manager.go），
// 仅追加 (mode, subMode) 记录。若与 defaultAgentFactory 漂移须同步修订。
//
// mode 记录是 tui 默认 mode 注入（gateway_server.go L508-522 setdefault
// code.normal）的直接断言证据。
func (s *CliRouteFullChainSuite) recordingAgentFactory(config map[string]any, mode, subMode string) (*runtime.UapClaw, error) {
	s.modeRecorder.mu.Lock()
	s.modeRecorder.modes = append(s.modeRecorder.modes, mode+"/"+subMode)
	s.modeRecorder.mu.Unlock()

	agent := runtime.NewUapClaw()
	if err := agent.CreateInstance(
		runtime.WithCreateInstanceConfig(config),
		runtime.WithCreateInstanceMode(mode),
		runtime.WithCreateInstanceSubMode(subMode),
	); err != nil {
		return nil, fmt.Errorf("[AgentManager] 创建 Agent 实例失败: %w", err)
	}
	return agent, nil
}

// lastRecordedMode 返回最近一次记录的 mode/subMode
func (s *CliRouteFullChainSuite) lastRecordedMode() string {
	s.modeRecorder.mu.Lock()
	defer s.modeRecorder.mu.Unlock()
	if len(s.modeRecorder.modes) == 0 {
		return ""
	}
	return s.modeRecorder.modes[len(s.modeRecorder.modes)-1]
}

// setupDataDir 构造 UAPCLAW_DATA_DIR 三件套（config/config.yaml + workspace 目录）
func (s *CliRouteFullChainSuite) setupDataDir() string {
	dataDir := s.T().TempDir()
	configDir := filepath.Join(dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "agent", "workspace"), 0o755))

	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_name: mock-model
      is_default: true
      model_client_config:
        client_provider: %s
        api_key: mock-api-key
`, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644))
	return dataDir
}

// pickFreePort 选取随机可用端口（对齐 Python _pick_free_port）
func (s *CliRouteFullChainSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录原值并设置环境变量（TearDown 恢复）
func (s *CliRouteFullChainSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量
func (s *CliRouteFullChainSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
}

// dialWS 连接 /tui 路由
func (s *CliRouteFullChainSuite) dialWS() *websocket.Conn {
	url := fmt.Sprintf("ws://127.0.0.1:%d/tui", s.gwPort)
	var conn *websocket.Conn
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			return conn
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.T().Fatalf("连接 %s 失败: %v", url, err)
	return nil
}

// sendChatSend 发送 chat.send 请求
func (s *CliRouteFullChainSuite) sendChatSend(conn *websocket.Conn, reqID, sessionID, mode string) {
	params := map[string]any{
		"session_id": sessionID,
		"content":    "hello cli",
	}
	if mode != "" {
		params["mode"] = mode
	}
	frame := map[string]any{
		"type":   "req",
		"id":     reqID,
		"method": "chat.send",
		"params": params,
	}
	data, err := json.Marshal(frame)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, data))
}

// readFrameUntil 读帧直到谓词命中或超时，返回命中帧
func (s *CliRouteFullChainSuite) readFrameUntil(conn *websocket.Conn, timeout time.Duration, pred func(map[string]any) bool) map[string]any {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		_, data, err := conn.ReadMessage()
		if err != nil {
			s.T().Fatalf("读帧失败: %v", err)
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		if pred(frame) {
			return frame
		}
	}
	s.T().Fatal("超时：未等到目标帧")
	return nil
}

// eventSequence 收集事件帧序列（chat.final 或 processing_status(false) 后停止）
func (s *CliRouteFullChainSuite) eventSequence(conn *websocket.Conn, timeout time.Duration) []map[string]any {
	var events []map[string]any
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		if frame["type"] == "event" {
			events = append(events, frame)
			ev, _ := frame["event"].(string)
			if ev == "chat.processing_status" {
				if payload, ok := frame["payload"].(map[string]any); ok {
					if isProc, _ := payload["is_processing"].(bool); !isProc {
						return events // 序列终点
					}
				}
			}
		}
	}
	return events
}

// eventNames 提取事件名序列
func eventNames(events []map[string]any) []string {
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, fmt.Sprintf("%v", e["event"]))
	}
	return names
}

// findEvent 在序列中找第一个指定事件
func findEvent(events []map[string]any, name string) map[string]any {
	for _, e := range events {
		if e["event"] == name {
			return e
		}
	}
	return nil
}

// assertCommonRoundtrip 主/副测试共用的完整往返断言（对齐 Python + Go 链路事件序列基准）。
// mode 为空串表示不传 mode 字段（副测试验证 tui 默认注入）。
func (s *CliRouteFullChainSuite) assertCommonRoundtrip(reqID, sessionID, mode string) {
	conn := s.dialWS()
	defer conn.Close()

	s.sendChatSend(conn, reqID, sessionID, mode)

	// 断言①：ack 帧（对齐 Python 原断言）
	ack := s.readFrameUntil(conn, 10*time.Second, func(f map[string]any) bool {
		return f["type"] == "res" && f["id"] == reqID
	})
	s.Require().Equal(true, ack["ok"], "ack ok 应为 true")
	payload, ok := ack["payload"].(map[string]any)
	s.Require().True(ok, "ack payload 应为 map")
	s.Require().Equal(true, payload["accepted"], "accepted 应为 true")
	s.Require().Equal(sessionID, payload["session_id"], "session_id 应为 %s", sessionID)

	// 断言④前置：mock 命中验证（防 config.yaml 错误静默 fallback 真实 LLM）
	s.Require().GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1, "MockModelClient 未被调用——config.yaml 的 client_provider 未命中")

	// 断言②：完整事件序列
	events := s.eventSequence(conn, 30*time.Second)
	names := eventNames(events)
	// 首帧 processing_status(true)（gateway 侧发出）
	s.Require().NotEmpty(names, "应收到至少一个事件帧")
	s.Require().Equal("chat.processing_status", names[0], "首事件应为 processing_status(true)，实际: %v", names)
	if p, ok := events[0]["payload"].(map[string]any); ok {
		s.Require().Equal(true, p["is_processing"], "首事件 is_processing 应为 true")
	}
	// 尾帧 processing_status(false)
	s.Require().Equal("chat.processing_status", names[len(names)-1], "尾事件应为 processing_status(false)，实际: %v", names)
	if p, ok := events[len(events)-1]["payload"].(map[string]any); ok {
		s.Require().Equal(false, p["is_processing"], "尾事件 is_processing 应为 false")
	}
	// delta 在 final 前（顺序断言核心）
	deltaIdx, finalIdx := -1, -1
	for i, n := range names {
		if n == "chat.delta" && deltaIdx == -1 {
			deltaIdx = i
		}
		if n == "chat.final" && finalIdx == -1 {
			finalIdx = i
		}
	}
	s.Require().NotEqual(-1, deltaIdx, "缺少 chat.delta 事件: %v", names)
	s.Require().NotEqual(-1, finalIdx, "缺少 chat.final 事件: %v", names)
	s.Require().Less(deltaIdx, finalIdx, "chat.delta 应在 chat.final 之前: %v", names)
	// usage_metadata 存在（值不断言）
	s.Require().NotNil(findEvent(events, "chat.usage_metadata"), "缺少 chat.usage_metadata: %v", names)
	// usage_summary 不应出现（TotalTokens=0）
	s.Require().Nil(findEvent(events, "chat.usage_summary"), "不应出现 chat.usage_summary: %v", names)

	// 断言④：delta/final 内容 = mock 预设文本
	if d := findEvent(events, "chat.delta"); d != nil {
		if p, ok := d["payload"].(map[string]any); ok {
			s.Require().Contains(p["content"], "CLI 全链路响应", "delta 内容应为 mock 预设文本")
		}
	}
	if f := findEvent(events, "chat.final"); f != nil {
		if p, ok := f["payload"].(map[string]any); ok {
			s.Require().Contains(p["content"], "CLI 全链路响应", "final 内容应为 mock 预设文本")
		}
	}
}
```

**关键修正（已整合进上面的代码）**：`assertCommonRoundtrip` 的 mode 作为参数显式传入（第三个参数），`sendChatSend` 直接透传。

**import 说明**：Step 6.1 代码块顶部的 import 块已是完整正确版本（含 `net`、`schema`、`mockllm`、`pathutil` 别名；不含 `net/http`——测试代码用不到）。结构体的 `envBackup [][3]string` 字段已在代码中给出。

- [x] **Step 6.2: 编译验证（骨架）**

Run: `cd /home/opensource/uap-claw-go && go vet -tags=integration ./tests/integration/swarm/gateway/ 2>&1 | head -20`
Expected: 无 vet 错误（还没写测试方法，`go vet` 应通过；若报 unused import 则修正）

- [x] **Step 6.3: 写主测试（显式 agent.plan）**

```go
// TestChatSendFullChain_显式agentPlan 显式 mode=agent.plan 的全链路往返。
//
// 对齐 Python test_cli_route_system_roundtrip 的 chat_res 断言 +
// Go 侧完整事件序列断言。
func (s *CliRouteFullChainSuite) TestChatSendFullChain_显式agentPlan() {
	// mock 预设：单文本 chunk（队列耗尽坑预防——每测试重置）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("CLI 全链路响应：agent.plan"))
	s.modeRecorder.mu.Lock()
	s.modeRecorder.modes = nil
	s.modeRecorder.mu.Unlock()

	s.assertCommonRoundtrip("req-agent-plan", "sess_agent_plan", "agent.plan")

	// 断言③：mode 注入验证（显式 agent.plan）
	s.Require().Equal("agent/plan", s.lastRecordedMode(),
		"工厂应收到的 mode=agent/subMode=plan，实际: %s", s.lastRecordedMode())
}
```

（mockllm import 已含在 Step 6.1 主 import 块中，无需追加。）

- [x] **Step 6.4: 写副测试（不传 mode → tui 默认 code.normal）**

```go
// TestChatSendFullChain_默认codeNormal 不传 mode，验证 tui 路由默认注入 code.normal。
//
// 这是 gateway_server.go L508-522 setdefault 逻辑的直接断言：
// 若注入损坏，工厂将收到 agent/plan（AgentServer 侧 fallback），断言失败。
func (s *CliRouteFullChainSuite) TestChatSendFullChain_默认codeNormal() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("CLI 全链路响应：code.normal"))
	s.modeRecorder.mu.Lock()
	s.modeRecorder.modes = nil
	s.modeRecorder.mu.Unlock()

	s.assertCommonRoundtrip("req-code-normal", "sess_code_normal", "")

	// 断言③：tui 默认 mode 注入的直接证据
	s.Require().Equal("code/normal", s.lastRecordedMode(),
		"工厂应收到的 mode=code/subMode=normal（tui 默认注入），实际: %s", s.lastRecordedMode())
}
```

**注意**：上面代码块中不应保留任何 `s//` 开头的行——若粘贴时发现请删除（计划早期草稿笔误已在本文档修正）。

- [x] **Step 6.5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go vet -tags=integration ./tests/integration/swarm/gateway/ 2>&1 | head -20`
Expected: 无错误

- [x] **Step 6.6: 本地跑集成测试（主测试先跑通）**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration ./tests/integration/swarm/gateway/ -run TestCliRouteFullChainSuite -v -timeout 120s 2>&1 | tail -30`
Expected: 两个测试 PASS。若失败按报错修复（常见坑见 Step 6.7）。

- [x] **Step 6.7: 失败排查指引（按优先级）**

1. **mock 未命中**（InvokeCallCount=0）：config.yaml 的 `client_provider` 与注册名不一致，或 `pathutil.ResetCache()` 时机晚于首次 WorkspaceDir 解析——检查 SetupSuite 顺序（先 setenv + ResetCache 再构造 AgentServer）
2. **connection.ack 超时**：AgentServer.Start 后 AgentClient.Connect 顺序反了——确认 Start 顺序（agentServer.Start → agentClient.Connect）
3. **收不到事件帧**：channelMgr.StartDispatch 未启动，或 externalChannels 未注册（`RegisterExternalChannel("tui", gwServer)`）
4. **delta/final 内容空**：mock 队列耗尽（SetResponses 被消费后未重置）——每测试开头 SetResponses
5. **suite 第二个测试失败但第一个通过**：GetAgent 缓存了旧实例——mode 不同应产生新实例；检查 modeRecorder 清零逻辑（每测试开头清）
6. **端口占用**：pickFreePort 后立即被抢——重跑即可（测试偶发）

- [x] **Step 6.8: Rule 7 双层门禁验证**

Run: `cd /home/opensource/uap-claw-go && go run ./cmd/integration-audit -verify 2>&1 | tail -15`
Expected: 通过（新目录静跨度 ≥ 3：gateway/routing/transport/server/agentcore 多个二级子系统）

- [x] **Step 6.9: Commit**

```bash
git add tests/integration/swarm/gateway/
git commit -m "test: CLI 路由全链路往返集成测试（双 mode + 完整事件序列断言）

对齐 Python test_cli_channel_ws.py。7 件套进程内装配，
mock 在 llm.ClientRegistry 边界。覆盖 tui 默认 mode 注入。"
```

---

### Task 7: 文档更新

**Files:**
- Modify: `internal/swarm/gateway/doc.go`（文件目录条目）
- Modify: `IMPLEMENTATION_PLAN.md`（11.15 状态）

- [x] **Step 7.1: gateway/doc.go 加 tui_route.go 条目**

在 `internal/swarm/gateway/doc.go` 的文件目录树中按字母序插入：

```
├── tui_route.go         # /tui CLI 路由装配（集合 + 5 本地 ack handler + DisconnectHandler，11.15 最小装配）
```

- [x] **Step 7.2: IMPLEMENTATION_PLAN.md 11.15 状态更新**

`IMPLEMENTATION_PLAN.md` L751：

```
| 11.15 | 🔄 | TUI 通道 | CLI 路由最小装配已落地（/tui ForwardMethods 80 项 + 5 chat ack handler + DisconnectHandler + 全链路集成测试 tests/integration/swarm/gateway/）；纯本地 handler 27 个（config.*/session.list/memory.*/cron.* 等）与 TUI 面板/memory_rpc 注册 ⤵️ 继续 | `jiuwenswarm/gateway/channel_manager/tui/` |
```

同时检查 11.9 行（L744）的 "ACP/TUI 路由接线 ⤵️ 11.25/11.15" 备注是否需要更新为 "ACP ⤵️ 11.25（TUI 已于 11.15 最小装配落地）"。

- [ ] **Step 7.3: Commit**

```bash
git add internal/swarm/gateway/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 11.15 最小装配状态更新（CLI 路由已落地，纯本地 handler 续回填）"
```

---

### Task 8: 全量验证与远端 CI

**Files:**
- 无新文件

- [x] **Step 8.1: 全仓构建**

Run: `cd /home/opensource/uap-claw-go && go build ./... 2>&1 | head -10`
Expected: 无输出

- [x] **Step 8.2: 全仓单测（默认 tag）**

Run: `cd /home/opensource/uap-claw-go && go test ./... 2>&1 | grep -v "^ok\|no test files" | head -20`
Expected: 无 FAIL 行

- [x] **Step 8.3: 集成测试全量（本地）**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration ./tests/integration/... -timeout 600s 2>&1 | grep -v "^ok\|no test files" | head -20`
Expected: 无 FAIL 行

- [x] **Step 8.4: gofmt + staticcheck**

Run: `cd /home/opensource/uap-claw-go && gofmt -l internal/swarm/gateway/ tests/integration/swarm/gateway/ && staticcheck ./internal/swarm/gateway/... ./tests/integration/swarm/gateway/... 2>&1 | head -10`
Expected: gofmt 无输出；staticcheck 无告警（注意 CJK 注释 gofmt 坑——用完整路径执行）

- [ ] **Step 8.5: 推送并监控远端 CI（规则 6）**

```bash
git push
gh run list --limit 3
gh run watch <run-id>
```

Expected: Build/Lint/UnitTest/IntegrationTest 四 job 全绿。失败则修复再推，直到全绿。

- [ ] **Step 8.6: 最终 Commit（如有修复）**

```bash
git add -A
git commit -m "fix: CI 修复（如有）"
git push
```

---

## Self-Review 记录

1. **Spec 覆盖**：spec §3.1（tui_route.go）→ Task 1+2；§3.2（接线）→ Task 4；§3.3（重构）→ Task 1；§3.4（生产单测）→ Task 3；§4（集成测试）→ Task 5+6；§5 交付物 8 项→ Task 1-8（doc.go 两条 → Task 5+7，IMPLEMENTATION_PLAN → Task 7）；§7 验收 7 条 → Task 8。无缺口。
2. **占位符扫描**：Task 2 集合体量 80/75 项无法在计划中完整展开，用 Step 2.2 机器生成步骤替代（生成命令完整给出）——可执行生成步骤而非占位符。草稿期笔误（mode 占位传参、`s//` 注释残行、import 路径 `uap-claw-go` 错误、`path` 未加别名、providerName 赋值晚于 setupDataDir 使用、重复 import 参考段）已在落盘时全部修正进正文代码。
3. **类型一致性**：`writeResponseFrame`（Task 1 定义、Task 2 使用）；`BuildTuiRouteConfig(TuiRouteParams)`（Task 2 定义、Task 4/6 使用）；`TuiForwardReqMethods/TuiForwardNoLocalHandlerMethods`（Task 2 定义、Task 3 断言）；`recordingAgentFactory` 签名与 `runtime.AgentFactory` 一致（`func(config map[string]any, mode, subMode string) (*runtime.UapClaw, error)`）；`LocalHandlerFunc(ws, reqID, params, sessionID) error` 五个 handler 签名一致；import 别名（`pathutil`/`mh`/`model_clients`/`isuite`）全文统一。已核对。
4. **装配顺序验证**：providerName 赋值 → setupDataDir（写入 config.yaml 用到 providerName）→ setenv UAPCLAW_DATA_DIR → ResetCache → 7 件套构造（Transport→AgentServer→AgentClient→MessageHandler→ChannelManager→InboundGatewayServer→GatewayServer）→ Start 顺序（agentServer→agentClient→msgHandler→channelMgr→inboundServer→gwServer）。依赖闭包正确。
