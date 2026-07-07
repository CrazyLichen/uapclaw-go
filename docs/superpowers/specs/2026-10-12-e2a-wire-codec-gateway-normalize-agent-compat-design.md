# Wire Codec + gateway_normalize + agent_compat 实现设计

> 本文档描述 10.2.5 Wire Codec、10.2.7 gateway_normalize、10.2.8 agent_compat 三个步骤的 Go 实现设计，
> 以及前置的 Payload 类型改造（json.RawMessage → map[string]any）。

---

## 1. 前置改造：Payload 类型对齐 Python

### 1.1 改造范围

| 结构体 | 字段 | 变更前 | 变更后 |
|--------|------|--------|--------|
| `AgentResponse` | `Payload` | `json.RawMessage` | `map[string]any` |
| `AgentResponseChunk` | `Payload` | `json.RawMessage` | `map[string]any` |
| `Message` | `Payload` | `json.RawMessage` | `map[string]any` |

> `AgentRequest.Params` 和 `Message.Params` 保持 `json.RawMessage` 不变（延迟解析合理）。

### 1.2 schema/agent.go 变更明细

- `AgentResponse.Payload`：`json.RawMessage` → `map[string]any`
- `AgentResponseChunk.Payload`：`json.RawMessage` → `map[string]any`
- `WithPayload(p json.RawMessage)` → `WithPayload(p map[string]any)`
- `WithChunkPayload(p json.RawMessage)` → `WithChunkPayload(p map[string]any)`
- `NewAgentResponseChunk` 参数：`payload json.RawMessage` → `payload map[string]any`
- `NewTerminalChunk` 内部构造：`json.RawMessage(`{"is_complete":true}`)` → `map[string]any{"is_complete": true}`
- `IsTerminal()` 重写：从 `json.Unmarshal` 改为直接读 `map[string]any` 字段

### 1.3 IsTerminal() 新逻辑

```
1. !IsComplete → false
2. Payload 为 nil → true（形态 A：payload=None, is_complete=True）
3. Payload["event_type"] 存在 → false（带业务的结束 chunk）
4. Payload["content"] 非空非nil非空串 → false
5. Payload["error"] 非空非nil非空串 → false
6. Payload 仅有 "is_complete"=true 一个键 → true（形态 B）
7. 其他 → false
```

### 1.4 schema/message.go 变更明细

- `Message.Payload`：`json.RawMessage` → `map[string]any`

### 1.5 测试同步

- `agent_test.go`：所有 `json.RawMessage(...)` 构造改为 `map[string]any{...}`
- `message_test.go`：同上
- 断言从 `string(resp.Payload)` 改为直接读 map 字段

---

## 2. gateway_normalize.go (10.2.7)

### 2.1 请求方向：Message → E2AEnvelope

| 导出函数 | 签名 | Python 对应 |
|----------|------|-------------|
| `MessageToLegacyAgentDict` | `func MessageToLegacyAgentDict(msg *Message) map[string]any` | `message_to_legacy_agent_dict` |
| `MessageToE2A` | `func MessageToE2A(msg *Message) (*E2AEnvelope, error)` | `message_to_e2a` |
| `MessageToE2AOrFallback` | `func MessageToE2AOrFallback(msg *Message) *E2AEnvelope` | `message_to_e2a_or_fallback` |
| `BuildFallbackE2A` | `func BuildFallbackE2A(legacy map[string]any) *E2AEnvelope` | `build_fallback_e2a` |
| `E2AFromAgentFields` | `func E2AFromAgentFields(opts ...AgentFieldOption) *E2AEnvelope` | `e2a_from_agent_fields` |
| `ChannelContextForChannelReply` | `func ChannelContextForChannelReply(env *E2AEnvelope) map[string]any` | `channel_context_for_channel_reply` |

**AgentFieldOption** 为函数选项模式，支持以下字段：
- `WithFieldRequestID(string)`
- `WithFieldChannelID(string)`
- `WithFieldSessionID(string)`
- `WithFieldReqMethod(ReqMethod)`
- `WithFieldParams(map[string]any)`
- `WithFieldIsStream(bool)`
- `WithFieldTimestamp(float64)`
- `WithFieldMetadata(map[string]any)`

**关键逻辑**：

- `MessageToE2A`：从 Message 构造 E2AEnvelope，处理 `enable_memory` / `group_digital_avatar` 逻辑（完整对齐 Python）
  - `is_group_chat = metadata["avatar_mode"]` 存在且为 true
  - `should_disable_memory = (enable_memory==false && group_digital_avatar==true && is_group_chat==true)`
  - `final_enable_memory = !should_disable_memory`
  - 只有 `enable_memory` 不为零值时才写入 metadata
  - **注意**：Go 中 `Message.EnableMemory` 为 `bool`（零值 false），无法区分"未设置"和"显式设置 false"。Python 中 `msg.enable_memory is False` 可区分。当前实现中 Go 零值 false 等同于"显式设置 false"，与 Python 行为一致——因为 Python 的 `Message.enable_memory` 默认值也是 `False`。若未来需区分三态（未设置/true/false），需改为 `*bool`，但不在本次 scope 内
- `MessageToE2AOrFallback`：先调 `MessageToE2A`，失败或校验不通过则 `BuildFallbackE2A`
- `BuildFallbackE2A`：规范化失败时仍发 E2A 形状，在 `channel_context._jiuwenswarm` 内携带 legacy 快照
- `legacyPayloadWithinLimit`：legacy JSON 超过 512KB 时裁剪 params

**内部常量**：

```go
const (
    e2aInternalContextKey    = "_jiuwenswarm"
    e2aFallbackFailedKey     = "normalize_failed"
    e2aLegacyAgentRequestKey = "legacy_agent_request"
    maxLegacyAgentRequestJSONBytes = 512000
)
```

### 2.2 响应方向：AgentResponse/Chunk ↔ E2AResponse

| 导出函数 | 签名 | Python 对应 |
|----------|------|-------------|
| `E2AResponseFromAgentResponse` | `func E2AResponseFromAgentResponse(resp *AgentResponse, responseID string, sequence int, opts ...ResponseNormOption) *E2AResponse` | `e2a_response_from_agent_response` |
| `E2AResponseFromAgentChunk` | `func E2AResponseFromAgentChunk(chunk *AgentResponseChunk, responseID string, sequence int, isStream bool, opts ...ResponseNormOption) *E2AResponse` | `e2a_response_from_agent_chunk` |
| `E2AResponseToAgentResponse` | `func E2AResponseToAgentResponse(e2a *E2AResponse) (*AgentResponse, error)` | `e2a_response_to_agent_response` |
| `E2AResponseToAgentChunk` | `func E2AResponseToAgentChunk(e2a *E2AResponse) (*AgentResponseChunk, error)` | `e2a_response_to_agent_chunk` |

**ResponseNormOption** 支持：
- `WithNormTimestamp(string)` — 可选时间戳覆盖

**E2AResponseFromAgentResponse 逻辑**：
- `ok=true` → `response_kind=e2a.complete`, `status=succeeded`, `body={"result": payload}`
- `ok=false` → `response_kind=e2a.error`, `status=failed`, `body={"code":"E2A.AGENT_ERROR", "message": err, "details": payload}`
- `is_final=true`, `is_stream=false`, `identity_origin=AGENT`

**E2AResponseFromAgentChunk 逻辑**（三段分支）：
1. **终止帧**：`is_complete=true` 且 `payload=={"is_complete":true}` → `e2a.complete`/`succeeded`
2. **错误帧**：`is_complete=true` 且 `event_type=="chat.error"` → `e2a.error`/`failed`
3. **业务结束帧**：`is_complete=true` 且非上述两种 → `e2a.complete`/`succeeded`，`body={"result": payload}`
4. **中间帧**：`is_complete=false` → `e2a.chunk`/`in_progress`
   - `event_type=="chat.delta"`：映射 `delta_kind`（`source_chunk_type=="llm_reasoning"` → `"reasoning"`，否则 `"text"`）
   - 保留 `role`/`member_name` 字段（team-member attribution）
   - 其他 event_type：`delta_kind="custom"`, `delta=payload`

**E2AResponseToAgentResponse 逻辑**：
- `e2a.complete` + `succeeded` → `AgentResponse{ok=true, payload=body["result"]}`
- `e2a.error` 或 `failed` → `AgentResponse{ok=false, payload=body["details"]}`
- 其他 kind → 返回 error

**E2AResponseToAgentChunk 逻辑**：
- `e2a.complete` + `is_final` → 判断是否为空终止帧，返回 `AgentResponseChunk{is_complete=true}`
- `e2a.error` + `is_final` → `AgentResponseChunk{is_complete=true, payload=body["details"]}`
- `e2a.chunk` + `!is_final` → 反向映射 `chat.delta`（`delta_kind=="reasoning"` → `source_chunk_type="llm_reasoning"`）
- `cron` → `AgentResponseChunk{event_type="cron.response", is_complete=true}`
- `acp.output_request` → `AgentResponseChunk{event_type="acp.output_request", is_complete=false}`
- 其他 → 返回 error

---

## 3. wire_codec.go (10.2.5)

### 3.1 导出函数

| 导出函数 | 签名 | Python 对应 |
|----------|------|-------------|
| `IsE2AResponseWireDict` | `func IsE2AResponseWireDict(data map[string]any) bool` | `is_e2a_response_wire_dict` |
| `ParseAgentServerWireUnary` | `func ParseAgentServerWireUnary(data map[string]any) (*AgentResponse, error)` | `parse_agent_server_wire_unary` |
| `ParseAgentServerWireChunk` | `func ParseAgentServerWireChunk(data map[string]any) (*AgentResponseChunk, error)` | `parse_agent_server_wire_chunk` |
| `EncodeAgentResponseForWire` | `func EncodeAgentResponseForWire(resp *AgentResponse, responseID string, sequence int) map[string]any` | `encode_agent_response_for_wire` |
| `EncodeAgentChunkForWire` | `func EncodeAgentChunkForWire(chunk *AgentResponseChunk, responseID string, sequence int, isStream bool) map[string]any` | `encode_agent_chunk_for_wire` |
| `EncodeJSONParseErrorWire` | `func EncodeJSONParseErrorWire(requestID, channelID, message string, responseID ...string) map[string]any` | `encode_json_parse_error_wire` |

### 3.2 非导出函数

| 非导出函数 | 签名 | 功能 |
|------------|------|------|
| `rawDictToAgentResponse` | `func rawDictToAgentResponse(data map[string]any) *AgentResponse` | legacy fallback 用的原始 dict → AgentResponse |
| `rawDictToAgentChunk` | `func rawDictToAgentChunk(data map[string]any) *AgentResponseChunk` | legacy fallback 用的原始 dict → AgentResponseChunk |
| `fallbackWireUnaryFromLegacy` | `func fallbackWireUnaryFromLegacy(legacy map[string]any, responseID string, sequence int, exc error) map[string]any` | 编码失败时构造含 legacy 的 E2AResponse |
| `fallbackWireChunkFromLegacy` | `func fallbackWireChunkFromLegacy(legacy map[string]any, responseID string, sequence int, exc error, isStream bool) map[string]any` | 编码失败时构造含 legacy 的 E2AResponse |

### 3.3 IsE2AResponseWireDict 判别逻辑

```
1. data 不是 map → false
2. data["type"] == "event" → false（排除事件帧）
3. data["protocol_version"] != "1.0" → false
4. data["response_kind"] 为非空字符串 → true，否则 → false
```

### 3.4 出站编码流程

**EncodeAgentResponseForWire**：
```
1. rid = resp.RequestID
2. try:
   a. e2a = E2AResponseFromAgentResponse(resp, responseID, sequence)
   b. wire = e2a.ToMap()
   c. 成功 → 返回 wire，记录 Info 日志
3. catch ToMap 失败:
   a. 记录 Error 日志（stage=to_dict, legacy_stashed=true）
   b. → fallbackWireUnaryFromLegacy
4. catch 编码失败:
   a. 记录 Error 日志（stage=encode, legacy_stashed=true）
   b. → fallbackWireUnaryFromLegacy
```

**EncodeAgentChunkForWire**：同理，调用 `E2AResponseFromAgentChunk`。

### 3.5 入站解码流程（精简版，无 deprecated 形状判别）

**ParseAgentServerWireUnary**：
```
1. rid = data["request_id"]
2. if IsE2AResponseWireDict(data):
   a. e2a = ResponseFromMap(data)
   b. 检查 e2a.Metadata[E2AWireLegacyAgentResponseKey] 存在 → rawDictToAgentResponse 兜底
   c. out = E2AResponseToAgentResponse(e2a)
   d. 成功 → 返回 out，记录 Debug 日志
   e. E2AResponseToAgentResponse 失败 → 再次检查 legacy 键兜底，否则返回 error
3. 不是 E2A 格式 → 返回 error（精简版不兼容 deprecated 形状）
```

**ParseAgentServerWireChunk**：同理，使用 `E2AResponseToAgentChunk` 和 `E2AWireLegacyAgentChunkKey`。

### 3.6 Fallback 构造

`fallbackWireUnaryFromLegacy` 构造一个 `E2AResponse`：
- `status=failed`, `response_kind=e2a.error`
- `body={"code":"E2A.WIRE_ENCODE_ERROR", "message":"...", "details":{"error": exc.Error()}}`
- `metadata[E2AWireLegacyAgentResponseKey] = legacy`
- `identity_origin=AGENT`, `is_stream=false`, `is_final=true`

`fallbackWireChunkFromLegacy` 同理，使用 `E2AWireLegacyAgentChunkKey`。

### 3.7 EncodeJSONParseErrorWire

入站 JSON 无法解析时发送的单帧 E2A 错误：
- `status=failed`, `response_kind=e2a.error`
- `body={"code":"E2A.INVALID_JSON", "message": message}`
- 无 legacy blob

### 3.8 与 Python 的差异

| 项目 | Python | Go |
|------|--------|-----|
| deprecated 形状判别 | `_deprecated_unary_shape` / `_deprecated_chunk_shape` | **不实现**，入站非 E2A 直接返回 error |
| `asdict(resp)` 获取 legacy dict | `dataclasses.asdict` | 手动构造 `map[string]any`（`request_id`, `channel_id`, `ok`, `payload`, `metadata`） |
| `asdict(chunk)` 获取 legacy dict | `dataclasses.asdict` | 手动构造 `map[string]any`（`request_id`, `channel_id`, `payload`, `is_complete`） |
| `E2AResponse.from_dict` | classmethod | `ResponseFromMap` 函数 |
| `e2a.to_dict()` | method | `e2a.ToMap()` 方法 |

---

## 4. agent_compat.go (10.2.8)

### 4.1 导出函数

| 导出函数 | 签名 | Python 对应 |
|----------|------|-------------|
| `E2AToAgentRequest` | `func E2AToAgentRequest(env *E2AEnvelope) (*AgentRequest, error)` | `e2a_to_agent_request` |

### 4.2 非导出函数

| 非导出函数 | 签名 | 功能 |
|------------|------|------|
| `e2aTimestampToFloat` | `func e2aTimestampToFloat(ts string) float64` | ISO8601 → Unix 秒 |

### 4.3 逻辑

1. 检查 `env.ChannelContext` 中 `_jiuwenswarm.normalize_failed` 标记 → 有则返回 `error("e2a_to_agent_request called on fallback envelope; use legacy path")`
2. `env.Method` → `ParseReqMethod(methodStr)` 查找枚举（未知方法返回 error）
3. `env.Params`（`map[string]any`）→ `json.Marshal` → `json.RawMessage` 赋给 `AgentRequest.Params`
4. `env.Timestamp` → `e2aTimestampToFloat` → `AgentRequest.Timestamp`
5. 构造 `AgentRequest`：`request_id=env.RequestID`, `channel_id=env.Channel`, `session_id=env.SessionID`, `chat_id=env.ChatID`, `is_stream=env.IsStream`, `metadata=channelContext`（去掉 `_jiuwenswarm` 内部键后）

---

## 5. doc.go 更新

```go
// 文件目录：
//
//	e2a/
//	├── doc.go                 # 包文档
//	├── constants.go           # 协议常量（来源协议、响应状态、响应类型、ACP 方法名、Wire 键名）
//	├── provenance.go          # IdentityOrigin 枚举 + E2AProvenance/E2AFileRef/E2AAuth 子结构体
//	├── envelope.go            # E2AEnvelope 请求信封 + 序列化/反序列化 + Legacy 兼容 + MergeParamsToACPPrompt
//	├── response.go            # E2AResponse 响应模型 + 序列化/反序列化
//	├── gateway_normalize.go   # Message/E2A/AgentResponse 格式互转（请求方向 + 响应方向）
//	├── wire_codec.go          # E2A ↔ AgentResponse/AgentResponseChunk 线编解码 + fallback 兜底
//	└── agent_compat.go        # E2AEnvelope → AgentRequest 转换
```

---

## 6. 日志同步

对照 Python 日志调用，在 Go 代码等价位置补充结构化日志：

- **组件**：`logger.ComponentChannel`
- **级别映射**：`logger.debug` → `Debug`, `logger.info` → `Info`, `logger.warning` → `Warn`, `logger.error`/`logger.exception` → `Error`
- **关键字段**：`request_id`, `response_id`, `response_kind`, `stage`, `legacy_stashed`, `err`

关键日志点：
- Wire Codec 出站：成功（Info）、失败（Error + Err）
- Wire Codec 入站：E2A 解码成功（Debug）、fallback（Warn）、失败（Error + Err）
- gateway_normalize：规范化成功（Info）、fallback（Warn）
- agent_compat：未知方法（Error）

---

## 7. IMPLEMENTATION_PLAN.md 回填

完成后更新：
- 10.2.5：☐ → ✅
- 10.2.7：☐ → ✅
- 10.2.8：☐ → ✅
- doc.go 文件目录更新

---

## 8. 测试计划

### 8.1 gateway_normalize_test.go

- `TestMessageToLegacyAgentDict`：验证字段映射
- `TestMessageToE2A`：正常转换、enable_memory 逻辑
- `TestMessageToE2AOrFallback`：成功路径、失败 fallback 路径
- `TestBuildFallbackE2A`：fallback 信封结构
- `TestE2AFromAgentFields`：各字段选项
- `TestChannelContextForChannelReply`：去掉内部键
- `TestE2AResponseFromAgentResponse`：ok=true/false 两种路径
- `TestE2AResponseFromAgentChunk`：终止帧/错误帧/业务结束帧/中间帧(chat.delta)/中间帧(custom)
- `TestE2AResponseToAgentResponse`：e2a.complete/e2a.error/不支持的 kind
- `TestE2AResponseToAgentChunk`：e2a.complete/e2a.error/e2a.chunk(chat.delta)/e2a.chunk(custom)/cron/acp.output_request
- **往返测试**：`E2AResponseToAgentResponse(E2AResponseFromAgentResponse(resp))` ≈ resp
- **往返测试**：`E2AResponseToAgentChunk(E2AResponseFromAgentChunk(chunk))` ≈ chunk

### 8.2 wire_codec_test.go

- `TestIsE2AResponseWireDict`：E2A 格式/非 E2A 格式/事件帧
- `TestEncodeAgentResponseForWire`：正常编码/编码失败 fallback
- `TestEncodeAgentChunkForWire`：正常编码/编码失败 fallback
- `TestParseAgentServerWireUnary`：E2A 格式解析/legacy fallback/非 E2A 格式返回 error
- `TestParseAgentServerWireChunk`：同上
- `TestEncodeJSONParseErrorWire`：错误帧结构
- **往返测试**：`ParseAgentServerWireUnary(EncodeAgentResponseForWire(resp))` ≈ resp
- **往返测试**：`ParseAgentServerWireChunk(EncodeAgentChunkForWire(chunk))` ≈ chunk

### 8.3 agent_compat_test.go

- `TestE2AToAgentRequest`：正常转换/未知方法/fallback envelope
- `TestE2ATimestampToFloat`：ISO8601/空串/无效格式

### 8.4 Payload 类型改造回归测试

- 现有 `agent_test.go` 和 `message_test.go` 全部通过
- `IsTerminal()` 新逻辑：形态 A/形态 B/带业务字段/非终止

---

## 9. 实现顺序

1. **Payload 类型改造**：schema/agent.go + schema/message.go + 测试更新
2. **gateway_normalize.go**：请求方向 + 响应方向 + 测试
3. **wire_codec.go**：编解码 + fallback + 测试
4. **agent_compat.go**：转换 + 测试
5. **doc.go 更新** + **IMPLEMENTATION_PLAN.md 回填**
