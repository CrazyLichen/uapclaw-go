# E2A 协议数据模型实现设计（10.2.1~10.2.4 + 10.2.6）

> 本文档描述 `internal/swarm/e2a/` 包下 E2A 协议数据模型的 Go 实现，
> 对应 Python `jiuwenswarm/common/e2a/models.py` + `constants.py`，
> 覆盖 IMPLEMENTATION_PLAN.md 步骤 10.2.1~10.2.4 + 10.2.6。

## 1. 实现范围

方案 A 合并批次：**10.2.1 + 10.2.2 + 10.2.3 + 10.2.4 + 10.2.6**

| 步骤 | 内容 | Go 文件 |
|---|---|---|
| 10.2.1 | E2AEnvelope 请求信封 | `envelope.go` |
| 10.2.2 | E2AResponse 响应模型 | `response.go` |
| 10.2.3 | E2AProvenance / E2AAuth | `provenance.go` |
| 10.2.4 | E2AFileRef / IdentityOrigin | `provenance.go` |
| 10.2.6 | E2A Constants 协议常量 | `constants.go` |

## 2. 产出文件

```
internal/swarm/e2a/
├── doc.go
├── constants.go              # 10.2.6 协议常量
├── constants_test.go
├── provenance.go             # 10.2.3 + 10.2.4 子结构体
├── provenance_test.go
├── envelope.go               # 10.2.1 E2AEnvelope + 序列化/反序列化
├── envelope_test.go
├── response.go               # 10.2.2 E2AResponse + 反序列化
└── response_test.go
```

## 3. Python ↔ Go 对应关系

| Python 文件 | Go 文件 | Python 行号 |
|---|---|---|
| `jiuwenswarm/common/e2a/constants.py` | `constants.go` | 1-120 |
| `jiuwenswarm/common/e2a/models.py` (IdentityOrigin/E2AProvenance/E2AFileRef/E2AAuth) | `provenance.go` | 33-82 |
| `jiuwenswarm/common/e2a/models.py` (E2AEnvelope + 常量/函数 + 序列化辅助) | `envelope.go` | 24-158, 217-479 |
| `jiuwenswarm/common/e2a/models.py` (E2AResponse + 反序列化) | `response.go` | 161-214, 401-446 |

## 4. 核心设计决策

| 决策 | 选择 | 原因 |
|---|---|---|
| 字段类型 | 全值类型，不区分 None 和零值 | 用户确认：`str\|None` → `string`，`int\|None` → `int` |
| `jsonrpc_id` | `any` 类型 | Python `str\|int\|None` 三态，无其他方式 |
| 默认值保证 | 工厂函数 | Go 零值 ≠ Python 默认值（如 SourceProtocol="" vs "e2a"） |
| 序列化（ToMap） | `json.Marshal` → `json.Unmarshal` to map | 比反射递归更简洁可靠 |
| 反序列化（FromMap） | `map[string]any` 手动解析 | 对照 Python from_dict 逐字段解析，保留 legacy 兼容 |
| Legacy 兼容 | 全部保留 5 种 | 一比一翻译，不遗漏 |
| 枚举模式 | 对齐 Schema 层 MessageType 模式 | 项目一致性 |

## 5. constants.go 设计

### 5.1 常量分组

**来源协议常量**：
- `E2ASourceProtocolE2A = "e2a"`
- `E2ASourceProtocolACP = "acp"`
- `E2ASourceProtocolA2A = "a2a"`

**响应状态常量**：
- `E2AResponseStatusSucceeded = "succeeded"`
- `E2AResponseStatusFailed = "failed"`
- `E2AResponseStatusInProgress = "in_progress"`

**响应类型常量**（11 种）：
- `E2AResponseKindE2AComplete = "e2a.complete"`
- `E2AResponseKindE2AChunk = "e2a.chunk"`
- `E2AResponseKindE2AError = "e2a.error"`
- `E2AResponseKindACPSessionUpdate = "acp.session_update"`
- `E2AResponseKindACPPromptResult = "acp.prompt_result"`
- `E2AResponseKindACPJSONRPCError = "acp.jsonrpc_error"`
- `E2AResponseKindACPOutputRequest = "acp.output_request"`
- `E2AResponseKindA2ATask = "a2a.task"`
- `E2AResponseKindA2AMessage = "a2a.message"`
- `E2AResponseKindA2AStreamEvent = "a2a.stream_event"`
- `E2AResponseKindCron = "cron"`
- `E2AResponseKindExt = "ext"`

**Wire 内部键常量**：
- `E2AWireLegacyAgentResponseKey = "_e2a_wire_legacy_agent_response"`
- `E2AWireLegacyAgentChunkKey = "_e2a_wire_legacy_agent_chunk"`
- `E2AWireServerPushKey = "_jiuwenswarm_server_push"`

### 5.2 全局变量

- `E2AResponseKinds []string`：运行时完整响应类型列表（12 元素）
- `E2AA2AStreamBranches []string`：A2A 流分支（4 元素）
- `ACPClientToAgentMethods []string`：客户端→Agent 方法（13 元素）
- `ACPAgentToClientMethods []string`：Agent→客户端 方法（10 元素）
- `ACPNotificationNames []string`：ACP 通知名（3 元素）
- `ACPSessionUpdateKinds []string`：会话更新类型（12 元素）
- `E2AWireInternalMetadataKeys map[string]struct{}`：Wire 内部元数据键集合（3 键）

### 5.3 命名映射规则

Python `UPPER_SNAKE_CASE` → Go `CamelCase`（保留 `E2A`/`ACP` 前缀）：
- `E2A_SOURCE_PROTOCOL_E2A` → `E2ASourceProtocolE2A`
- `ACP_CLIENT_TO_AGENT_METHODS` → `ACPClientToAgentMethods`

Python `tuple[str, ...]` → Go `[]string`
Python `frozenset[str]` → Go `map[string]struct{}`

### 5.4 测试（4 个）

- `TestE2AResponseKinds_长度与内容`
- `TestACPSessionUpdateKinds_包含关键字段`
- `TestE2AWireInternalMetadataKeys_包含三个键`
- `TestConstants_值不可变`

## 6. provenance.go 设计

### 6.1 IdentityOrigin 枚举

```go
type IdentityOrigin string

const (
    IdentityOriginSystem  IdentityOrigin = "system"
    IdentityOriginUser    IdentityOrigin = "user"
    IdentityOriginAgent   IdentityOrigin = "agent"
    IdentityOriginService IdentityOrigin = "service"
)
```

配套函数（对齐 Schema 层 MessageType 模式）：
- `AllIdentityOrigins() []IdentityOrigin`
- `ParseIdentityOrigin(s string) (IdentityOrigin, error)`
- `IsValidIdentityOrigin(s string) bool`
- `String() string`
- `GoString() string`
- 包级 `identityOriginLookup map[string]IdentityOrigin` + `init()` 构建

### 6.2 E2AProvenance

```go
type E2AProvenance struct {
    SourceProtocol string         `json:"source_protocol"` // 默认 "e2a"
    Converter      string         `json:"converter"`
    ConvertedAt    string         `json:"converted_at"`
    Details        map[string]any `json:"details"`
}
```

工厂函数 `NewE2AProvenance()` 设置默认 `SourceProtocol = "e2a"`。

### 6.3 E2AFileRef

```go
type E2AFileRef struct {
    URI      string         `json:"uri"`
    Name     string         `json:"name"`
    MimeType string         `json:"mime_type"`
    Size     int            `json:"size"`
    Meta     map[string]any `json:"_meta"`
}
```

工厂函数 `NewE2AFileRef(uri string)`。

### 6.4 E2AAuth

```go
type E2AAuth struct {
    MethodID      string            `json:"method_id"`
    BearerToken   string            `json:"bearer_token"`
    APIKeyRef     string            `json:"api_key_ref"`
    CredentialRef string            `json:"credential_ref"`
    ExtraHeaders  map[string]string `json:"extra_headers"`
    Meta          map[string]any    `json:"_meta"`
}
```

工厂函数 `NewE2AAuth()`。

**注意**：Python `_meta` 字段 → Go `Meta` 导出字段，JSON tag 保留 `"_meta"`。

### 6.5 测试（11 个）

- `TestIdentityOrigin_枚举值`
- `TestIdentityOrigin_Parse_合法` / `TestIdentityOrigin_Parse_非法`
- `TestIdentityOrigin_String` / `TestIdentityOrigin_GoString`
- `TestNewE2AProvenance_默认值`
- `TestE2AProvenance_JSON序列化往返`
- `TestNewE2AFileRef_默认值`
- `TestE2AFileRef_JSON序列化往返`
- `TestNewE2AAuth_默认值`
- `TestE2AAuth_JSON序列化往返`
- `TestE2AFileRef_E2AAuth_Meta字段JSONTag`

## 7. envelope.go 设计

### 7.1 包级常量

- `E2AProtocolVersion = "1.0"`

### 7.2 E2AEnvelope 结构体（24 字段）

```go
type E2AEnvelope struct {
    // ─── 基础 / 关联 ───
    ProtocolVersion string         `json:"protocol_version"` // 默认 "1.0"
    Provenance      E2AProvenance  `json:"provenance"`
    RequestID       string         `json:"request_id"`
    JSONRPCID       any            `json:"jsonrpc_id"`      // str | int | None
    CorrelationID   string         `json:"correlation_id"`
    TaskID          string         `json:"task_id"`
    ContextID       string         `json:"context_id"`
    SessionID       string         `json:"session_id"`
    MessageID       string         `json:"message_id"`

    // ─── 时间戳 ───
    Timestamp       string         `json:"timestamp"`       // RFC 3339 UTC

    // ─── 身份与入口 ───
    IdentityOrigin  IdentityOrigin `json:"identity_origin"` // 默认 USER
    Channel         string         `json:"channel"`
    UserID          string         `json:"user_id"`
    ChatID          string         `json:"chat_id"`
    SourceAgentID   string         `json:"source_agent_id"`

    // ─── 网关 RPC ───
    Method              string         `json:"method"`
    Params              map[string]any `json:"params"`
    ExtMethod           string         `json:"ext_method"`
    SessionUpdateKind   string         `json:"session_update_kind"`
    IsStream            bool           `json:"is_stream"`

    // ─── 期望输出 ───
    ExpectedOutputModes []string       `json:"expected_output_modes"`

    // ─── 鉴权 ───
    Auth                E2AAuth        `json:"auth"`

    // ─── 扩展槽 ───
    ChannelContext       map[string]any `json:"channel_context"`
    A2AMetadata          map[string]any `json:"a2a_metadata"`
    ACPMeta              map[string]any `json:"acp_meta"`
}
```

### 7.3 导出函数

| 函数 | Python 对应 | 说明 |
|---|---|---|
| `UTCNowISO() string` | `utc_now_iso()` | RFC 3339 UTC 时间字符串 |
| `NewE2AEnvelope() *E2AEnvelope` | — | 工厂函数，设默认值 |
| `(e *E2AEnvelope) EnsureTimestamp()` | `ensure_timestamp()` | 时间戳填充 |
| `(e *E2AEnvelope) ToMap() map[string]any` | `to_dict()` | 序列化 |
| `EnvelopeFromMap(data map[string]any) *E2AEnvelope` | `from_dict(data)` | 反序列化 |
| `MergeParamsToACPPrompt(env *E2AEnvelope) map[string]any` | `merge_params_to_acp_prompt(envelope)` | ACP prompt 补全 |

### 7.4 非导出函数

| 函数 | Python 对应 | 说明 |
|---|---|---|
| `enumValue(v any) any` | `_enum_value(obj)` | 枚举值提取 |
| `structToMap(v any) map[string]any` | `_dataclass_to_json_dict(obj)` | 递归结构体→map |
| `provenanceFromMap(raw any) E2AProvenance` | `_provenance_from_dict(raw)` | Provenance 反序列化 |
| `normalizeTimestampValue(raw any) string` | `_normalize_timestamp_value(raw)` | 时间戳规范化 |
| `migrateLegacyBinding(data map[string]any, prov E2AProvenance) E2AProvenance` | `_migrate_legacy_binding(data, prov)` | binding 字段迁移 |
| `paramsWithOptionalLegacyPayload(data map[string]any) map[string]any` | `_params_with_optional_legacy_payload(data)` | payload 合并到 params |
| `getString / getStringWithDefault / getBool / getInt / getMapAny / getMapString / getStringSlice / getStringOrEmpty / isEmptySliceOrMap` | `data.get("xxx")` 系列调用 | map 提取辅助 |

### 7.5 Legacy 兼容逻辑（5 种）

| # | 兼容逻辑 | Go 函数 | Python 行号 |
|---|---|---|---|
| 1 | `binding` → `provenance.details.migrated_from_binding` | `migrateLegacyBinding` | 281-304 |
| 2 | 顶层 `payload` 合并到 `params`（不覆盖） | `paramsWithOptionalLegacyPayload` | 307-323 |
| 3 | `req_method` → `method` | `EnvelopeFromMap` 内部 | 364-370 |
| 4 | `channel_id` → `channel` | `EnvelopeFromMap` 内部 | 360-362 |
| 5 | 顶层 `metadata` → `channel_context` | `EnvelopeFromMap` 内部 | 352-358 |

### 7.6 测试（28 个）

- `TestNewE2AEnvelope_默认值`
- `TestE2AEnvelope_EnsureTimestamp_未设置` / `TestE2AEnvelope_EnsureTimestamp_已设置`
- `TestE2AEnvelope_ToMap_枚举值` / `TestE2AEnvelope_ToMap_嵌套Provenance`
- `TestEnvelopeFromMap_完整字段`
- `TestEnvelopeFromMap_legacy_binding迁移`
- `TestEnvelopeFromMap_legacy_payload合并`
- `TestEnvelopeFromMap_legacy_req_method`
- `TestEnvelopeFromMap_legacy_channel_id`
- `TestEnvelopeFromMap_legacy_metadata合并`
- `TestEnvelopeFromMap_timestamp_float转RFC3339`
- `TestEnvelopeFromMap_auth解析_map`
- `TestNormalizeTimestampValue_string` / `TestNormalizeTimestampValue_float` / `TestNormalizeTimestampValue_int` / `TestNormalizeTimestampValue_nil`
- `TestMigrateLegacyBinding_无binding字段` / `TestMigrateLegacyBinding_binding为ACP` / `TestMigrateLegacyBinding_binding为dict含value`
- `TestParamsWithOptionalLegacyPayload_有payload` / `TestParamsWithOptionalLegacyPayload_无payload`
- `TestMergeParamsToACPPrompt_非prompt方法` / `TestMergeParamsToACPPrompt_已有prompt` / `TestMergeParamsToACPPrompt_content_blocks转prompt` / `TestMergeParamsToACPPrompt_text转prompt` / `TestMergeParamsToACPPrompt_补session_id` / `TestMergeParamsToACPPrompt_补acp_meta`
- `TestE2AEnvelope_JSON序列化往返`
- `TestEnvelopeFromMap_ToMap_往返`

## 8. response.go 设计

### 8.1 E2AResponse 结构体（22 字段）

```go
type E2AResponse struct {
    // ─── 核心响应字段 ───
    ProtocolVersion string         `json:"protocol_version"` // 默认 "1.0"
    ResponseID      string         `json:"response_id"`
    RequestID       string         `json:"request_id"`
    Sequence        int            `json:"sequence"`        // 默认 0
    IsFinal         bool           `json:"is_final"`        // 默认 false
    Status          string         `json:"status"`          // 默认 "in_progress"
    ResponseKind    string         `json:"response_kind"`   // 默认 ""
    Timestamp       string         `json:"timestamp"`
    Provenance      E2AProvenance  `json:"provenance"`
    Body            map[string]any `json:"body"`

    // ─── 关联字段 ───
    JSONRPCID       any            `json:"jsonrpc_id"`      // str | int | None
    CorrelationID   string         `json:"correlation_id"`
    TaskID          string         `json:"task_id"`
    ContextID       string         `json:"context_id"`
    SessionID       string         `json:"session_id"`
    MessageID       string         `json:"message_id"`
    IsStream        bool           `json:"is_stream"`
    IdentityOrigin  IdentityOrigin `json:"identity_origin"` // 默认 AGENT
    Channel         string         `json:"channel"`
    UserID          string         `json:"user_id"`
    SourceAgentID   string         `json:"source_agent_id"`
    Method          string         `json:"method"`

    // ─── 扩展槽 ───
    Projections     map[string]any `json:"projections"`
    ChannelContext  map[string]any `json:"channel_context"`
    Metadata        map[string]any `json:"metadata"`
    A2AMetadata     map[string]any `json:"a2a_metadata"`
    ACPMeta         map[string]any `json:"acp_meta"`
}
```

### 8.2 导出函数

| 函数 | Python 对应 | 说明 |
|---|---|---|
| `NewE2AResponse() *E2AResponse` | — | 工厂函数，设默认值 |
| `(r *E2AResponse) EnsureTimestamp()` | `ensure_timestamp()` | 时间戳填充 |
| `(r *E2AResponse) ToMap() map[string]any` | `to_dict()` | 序列化 |
| `ResponseFromMap(data map[string]any) *E2AResponse` | `from_dict(data)` | 反序列化 |

### 8.3 EnvelopeFromMap vs ResponseFromMap 差异

| 差异点 | EnvelopeFromMap | ResponseFromMap |
|---|---|---|
| `identity_origin` 默认值 | `USER` | `AGENT` |
| `provenance` 迁移 | 有 `migrateLegacyBinding` | **无** |
| `params` 合并 | 有 `paramsWithOptionalLegacyPayload` | **无**（用 `body` 代替） |
| `auth` 解析 | 有 | **无** |
| `method` 兼容 | 有 `req_method` → `method` | **无** |
| `channel_context` 合并 | 有 `metadata` → `channel_context` | **无** |
| `sequence` 容错 | 无此字段 | 有 `try/except` 容错 |

### 8.4 测试（16 个）

- `TestNewE2AResponse_默认值`
- `TestE2AResponse_EnsureTimestamp_未设置` / `TestE2AResponse_EnsureTimestamp_已设置`
- `TestE2AResponse_ToMap_枚举值` / `TestE2AResponse_ToMap_嵌套Provenance` / `TestE2AResponse_ToMap_全部字段`
- `TestResponseFromMap_完整字段`
- `TestResponseFromMap_identity_origin默认AGENT`
- `TestResponseFromMap_sequence容错_float64` / `TestResponseFromMap_sequence容错_非数字`
- `TestResponseFromMap_channel兼容channel_id`
- `TestResponseFromMap_timestamp规范化`
- `TestResponseFromMap_status默认in_progress`
- `TestResponseFromMap_response_kind空串`
- `TestE2AResponse_JSON序列化往返`
- `TestResponseFromMap_ToMap_往返`

## 9. 函数清单汇总

| 文件 | 导出函数 | 非导出函数 |
|---|---|---|
| `constants.go` | — | — |
| `provenance.go` | `NewE2AProvenance`, `NewE2AFileRef`, `NewE2AAuth`, `AllIdentityOrigins`, `ParseIdentityOrigin`, `IsValidIdentityOrigin`, `String`, `GoString` | `init` |
| `envelope.go` | `UTCNowISO`, `NewE2AEnvelope`, `EnsureTimestamp`, `ToMap`, `EnvelopeFromMap`, `MergeParamsToACPPrompt` | `enumValue`, `structToMap`, `provenanceFromMap`, `normalizeTimestampValue`, `migrateLegacyBinding`, `paramsWithOptionalLegacyPayload`, `getString`, `getStringWithDefault`, `getBool`, `getInt`, `getMapAny`, `getMapString`, `getStringSlice`, `getStringOrEmpty`, `isEmptySliceOrMap` |
| `response.go` | `NewE2AResponse`, `EnsureTimestamp`, `ToMap`, `ResponseFromMap` | — |

## 10. 测试汇总

| 文件 | 测试数量 |
|---|---|
| `constants_test.go` | 4 |
| `provenance_test.go` | 11 |
| `envelope_test.go` | 28 |
| `response_test.go` | 16 |
| **合计** | **59** |

## 11. 验证点

- [ ] Schema 全部类型 JSON 序列化往返通过
- [ ] EnvelopeFromMap ∘ ToMap ≈ 恒等（往返一致性）
- [ ] ResponseFromMap ∘ ToMap ≈ 恒等（往返一致性）
- [ ] 5 种 Legacy 兼容逻辑均有测试覆盖
- [ ] 测试覆盖率 ≥ 85%
