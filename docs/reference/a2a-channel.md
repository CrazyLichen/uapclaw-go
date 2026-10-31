# A2A 通道接入说明（11.26）

对应 Python：`jiuwenswarm/gateway/channel_manager/protocol/a2a/a2a_connect.py`
设计与偏差：`docs/superpowers/specs/2026-10-31-a2a-channel-11.26-design.md`

## 1. 概述与流程位置

A2A（Agent2Agent）入站通道让外部 A2A 客户端把本网关当作一个 A2A Agent 调用。
协议栈由官方 Go SDK `github.com/a2aproject/a2a-go/v2`（`a2asrv`）提供，本包只做网关桥。

```
外部 A2A 客户端 ──HTTP JSON-RPC / SSE──▶ A2AChannel（默认 127.0.0.1:19100）
   ├ POST {A2A_SERVER_PATH}          JSON-RPC（11 个方法）
   ├ GET  {A2A_SERVER_CARD_PATH}     Agent Card
   └ GET  {A2A_SERVER_EXTENDED_CARD_PATH} Extended Agent Card
        │ on_message 回调（ChannelManager.RegisterChannel 注入）
        ▼
   ChannelManager → MessageHandler（req_method=chat.send，params={query,files}）
        ▼ E2A 协议
   AgentServer → Agent 会话（session_id = A2A contextId）
        ▼ 回程事件
   MessageHandler → ChannelManager.dispatchRobotMessages → A2AChannel.Send
        ▼
   a2aExecutor 转 artifactUpdate/statusUpdate → SSE / 聚合 Task
```

## 2. 环境变量

`ChannelID` 固定为 `a2a`（无 env，对齐 Python）。

| 变量 | 默认 | 说明 |
|---|---|---|
| `A2A_SERVER_ENABLED` | 未设即关闭 | `1` / `true` / `yes` / `on` 为开启 |
| `A2A_SERVER_HOST` | `127.0.0.1` | HTTP 监听地址；对外服务常用 `0.0.0.0` |
| `A2A_SERVER_PORT` | `19100` | 与 Web(19001)/ACP 端口勿冲突；非法值回落默认并告警 |
| `A2A_SERVER_PATH` | `/a2a` | JSON-RPC 入口路径 |
| `A2A_SERVER_CARD_PATH` | `/.well-known/agent-card.json` | Agent Card 路径 |
| `A2A_SERVER_EXTENDED_CARD_PATH` | `/agent/authenticatedExtendedCard` | Extended Agent Card 路径 |
| `A2A_SERVER_PROTOCOL_VERSION` | `1.0.0` | 写入 `AgentInterface.protocolVersion`（与请求头 `A2A-Version` 不是一回事） |
| `A2A_SERVER_APP_NAME` | `UapClaw Gateway A2A Server` | Agent Card `name` |
| `A2A_SERVER_APP_DESCRIPTION` | `A2A ingress for UapClaw Gateway` | Agent Card `description` |
| `A2A_SERVER_APP_VERSION` | `0.1.0` | Agent Card `version` |

当 `A2A_SERVER_ENABLED` 为真但监听失败（端口占用等）时，Gateway 主流程继续启动，仅在日志记录 `A2A channel failed to start`。

## 3. 端点

| 端点 | 方法 | 说明 |
|---|---|---|
| `http://{HOST}:{PORT}{A2A_SERVER_PATH}` | POST | JSON-RPC 2.0（非 POST 返回 405，对齐 Python 路由声明） |
| `http://{HOST}:{PORT}{A2A_SERVER_CARD_PATH}` | GET | 公共 Agent Card |
| `http://{HOST}:{PORT}{A2A_SERVER_EXTENDED_CARD_PATH}` | GET | 扩展 Agent Card（与公共卡同源） |

方法集：`SendMessage` / `SendStreamingMessage` / `GetTask` / `ListTasks` / `CancelTask` / `SubscribeToTask` /
`GetTaskPushNotificationConfig` / `CreateTaskPushNotificationConfig` / `ListTaskPushNotificationConfigs` /
`DeleteTaskPushNotificationConfig` / `GetExtendedAgentCard`（与 Python `METHOD_TO_MODEL` 一致）。

## 4. 必需请求头

**`A2A-Version: 1.0`**。缺失或非 1.x 会被按 `0.3` 处理并返回：

```json
{"jsonrpc":"2.0","id":"…","error":{"code":-32009,"message":"this version is not supported: requested \"0.3\", served \"1.0\""}}
```

> Python 仓库 `docs/zh/A2A.md` §8 的 curl 示例缺少该头，按源码实际行为会失败；本文件示例均已带上。

## 5. 验证示例

非流式：

```bash
curl -sS -X POST "http://127.0.0.1:19100/a2a" \
  -H 'Content-Type: application/json' \
  -H 'A2A-Version: 1.0' \
  -d '{"jsonrpc":"2.0","id":"t1","method":"SendMessage","params":{"message":{"messageId":"m1","contextId":"c1","role":"ROLE_USER","parts":[{"text":"ping"}]}}}'
```

返回形如：

```json
{"jsonrpc":"2.0","id":"t1","result":{"task":{"id":"…","artifacts":[{"artifactId":"…_response","name":"response","parts":[{"text":"…"}]}],"contextId":"c1","status":{"state":"TASK_STATE_COMPLETED","timestamp":"…"}}}}
```

流式（SSE，逐帧 `data: {json}`，`artifactUpdate` 先于终态 `statusUpdate`）：

```bash
curl -sS -N -X POST "http://127.0.0.1:19100/a2a" \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -H 'A2A-Version: 1.0' \
  -d '{"jsonrpc":"2.0","id":"t2","method":"SendStreamingMessage","params":{"message":{"messageId":"m2","contextId":"c2","role":"ROLE_USER","parts":[{"text":"ping"}]}}}'
```

Agent Card：

```bash
curl -sS "http://127.0.0.1:19100/.well-known/agent-card.json"
```

## 6. 字段映射

### 6.1 请求（A2A → 内部 Message）

| A2A | 内部 |
|---|---|
| `taskId`（缺省由 SDK 生成） | `Message.id`（回包按此配对 pending） |
| `contextId` | `Message.session_id`（会话连续性由调用方承载） |
| `parts[].text`（多段以 `\n` 连接） | `params.query` |
| 非文本 `parts`（url / data / raw） | `params.files[]`，键含 `name`+`filename`、`media_type`+`type`、`url`+`uri`、`data`+`encoding=base64`、`raw` |
| `message.metadata` | `Message.metadata` |

### 6.2 响应（内部 Message → A2A）

| 内部 | A2A |
|---|---|
| `payload.content`（字符串，过滤 `is_complete` 哨兵） | `Part{text}` |
| `payload.result`（字符串） | `Part{text}` |
| `chat.error` | `Part{text}`（`error` → `content` → `agent request failed`） |
| `chat.tool_call` | `Part{text:"[tool_call] <name>"}` |
| `chat.tool_result` | `Part{text:"[tool_result:<name>] <result>"}` |
| `payload.files[]` | `Part{url}` / `Part{data}` / `Part{raw}`（附 filename / mediaType） |

终态映射：`chat.error → TASK_STATE_FAILED`、`chat.interrupt_result → TASK_STATE_CANCELED`、其余 `→ TASK_STATE_COMPLETED`。

## 7. 错误码

| code | reason | 触发 |
|---|---|---|
| `-32009` | `VERSION_NOT_SUPPORTED` | `A2A-Version` 缺失或非 1.x |
| `-32601` | `METHOD_NOT_FOUND` | 未知方法 |
| `-32602` | `INVALID_PARAMS` | params 缺失/类型错（如 `SendMessage` 缺 `message`） |
| `-32006` | `INVALID_AGENT_RESPONSE` | 执行器首事件非 Task/Message（Go 侧已用 `NewSubmittedTask` 规避） |
| `-32700` | `PARSE_ERROR` | 非法 JSON |

## 8. 停止语义

通道 `Stop` 会：关闭 HTTP 服务 → 向所有在途请求投递 `chat.error`（`payload={"error":"a2a channel stopped","is_complete":true}`）→ 清空 pending →
由 `app_gateway.Stop` 注销通道（`UnregisterChannel("a2a")`）。在途客户端收到 `TASK_STATE_FAILED` 而非挂死。

## 9. 已知偏差

见设计文档 §9 的 D1-D12，要点：

- **D1** 首事件必须是 `Task`（SDK 约束），Python 直接发 WORKING status；
- **D2** `protocolBinding` 用规范值 `"JSONRPC"`（Python 写小写）；
- **D3** task/context id 为 SDK 生成的 UUID，无 `a2a_`/`a2a_ctx_` 前缀；
- **D4** `Part.raw` 走 base64（Python 传 `str` 会 `TypeError`）；
- **D5** `MessageToText` 整体 payload 用 JSON（Python 为 `str(dict)`）；
- **D6** 扩展卡未独立配置，HTTP 与 RPC 出口同源同卡；
- **D10** `ChannelTypeA2A` 为 Go 侧新增枚举；
- **D11** pending 队列有界 64，满则丢弃并告警。
