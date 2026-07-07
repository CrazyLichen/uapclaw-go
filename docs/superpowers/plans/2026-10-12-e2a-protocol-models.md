# E2A 协议数据模型实现计划（10.2.1~10.2.4 + 10.2.6）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `internal/swarm/e2a/` 包，一比一翻译 Python `jiuwenswarm/common/e2a/models.py` + `constants.py`，覆盖 IMPLEMENTATION_PLAN.md 步骤 10.2.1~10.2.4 + 10.2.6。

**Architecture:** 自底向上实现：先常量 → 子结构体 → 请求信封 → 响应模型。每个文件完成后立即编写测试验证。所有字段使用值类型，默认值通过工厂函数保证。序列化使用 json.Marshal/Unmarshal 中转，反序列化使用 map[string]any 手动解析，保留 5 种 Legacy 兼容逻辑。

**Tech Stack:** Go 1.24, encoding/json, time, fmt, strconv

---

## 文件结构

| 文件 | 职责 | 对应 Python |
|---|---|---|
| `internal/swarm/e2a/doc.go` | 包文档 | — |
| `internal/swarm/e2a/constants.go` | 协议常量（20+ 常量、6 切片、1 map） | `constants.py` |
| `internal/swarm/e2a/constants_test.go` | 常量测试 | — |
| `internal/swarm/e2a/provenance.go` | IdentityOrigin 枚举 + E2AProvenance/E2AFileRef/E2AAuth 结构体 | `models.py:33-82` |
| `internal/swarm/e2a/provenance_test.go` | 子结构体测试 | — |
| `internal/swarm/e2a/envelope.go` | E2AEnvelope + 序列化/反序列化 + Legacy 兼容 + MergeParamsToACPPrompt | `models.py:24-158,217-479` |
| `internal/swarm/e2a/envelope_test.go` | 信封测试 | — |
| `internal/swarm/e2a/response.go` | E2AResponse + 序列化/反序列化 | `models.py:161-214,401-446` |
| `internal/swarm/e2a/response_test.go` | 响应测试 | — |

---

### Task 1: constants.go — 协议常量

**Files:**
- Create: `internal/swarm/e2a/constants.go`
- Test: `internal/swarm/e2a/constants_test.go`

- [ ] **Step 1: 创建 e2a 目录和 constants.go**

```go
package e2a

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 来源协议（对应 Python E2A_SOURCE_PROTOCOL_*）
const (
	// E2ASourceProtocolE2A 原生 E2A 协议
	E2ASourceProtocolE2A = "e2a"
	// E2ASourceProtocolACP ACP 协议
	E2ASourceProtocolACP = "acp"
	// E2ASourceProtocolA2A A2A 协议
	E2ASourceProtocolA2A = "a2a"
)

// 响应状态（对应 Python E2A_RESPONSE_STATUS_*）
const (
	// E2AResponseStatusSucceeded 成功
	E2AResponseStatusSucceeded = "succeeded"
	// E2AResponseStatusFailed 失败
	E2AResponseStatusFailed = "failed"
	// E2AResponseStatusInProgress 进行中
	E2AResponseStatusInProgress = "in_progress"
)

// 响应类型（对应 Python E2A_RESPONSE_KIND_*，共 12 种）
const (
	// E2AResponseKindE2AComplete E2A 完整响应
	E2AResponseKindE2AComplete = "e2a.complete"
	// E2AResponseKindE2AChunk E2A 流式块
	E2AResponseKindE2AChunk = "e2a.chunk"
	// E2AResponseKindE2AError E2A 错误
	E2AResponseKindE2AError = "e2a.error"
	// E2AResponseKindACPSessionUpdate ACP 会话更新
	E2AResponseKindACPSessionUpdate = "acp.session_update"
	// E2AResponseKindACPPromptResult ACP 提示结果
	E2AResponseKindACPPromptResult = "acp.prompt_result"
	// E2AResponseKindACPJSONRPCError ACP JSON-RPC 错误
	E2AResponseKindACPJSONRPCError = "acp.jsonrpc_error"
	// E2AResponseKindACPOutputRequest ACP 输出请求
	E2AResponseKindACPOutputRequest = "acp.output_request"
	// E2AResponseKindA2ATask A2A 任务
	E2AResponseKindA2ATask = "a2a.task"
	// E2AResponseKindA2AMessage A2A 消息
	E2AResponseKindA2AMessage = "a2a.message"
	// E2AResponseKindA2AStreamEvent A2A 流事件
	E2AResponseKindA2AStreamEvent = "a2a.stream_event"
	// E2AResponseKindCron 定时任务
	E2AResponseKindCron = "cron"
	// E2AResponseKindExt 扩展
	E2AResponseKindExt = "ext"
)

// Wire 内部键（对应 Python E2A_WIRE_*）
const (
	// E2AWireLegacyAgentResponseKey 编码失败时旧 AgentResponse 写入 metadata 的键
	E2AWireLegacyAgentResponseKey = "_e2a_wire_legacy_agent_response"
	// E2AWireLegacyAgentChunkKey 编码失败时旧 AgentChunk 写入 metadata 的键
	E2AWireLegacyAgentChunkKey = "_e2a_wire_legacy_agent_chunk"
	// E2AWireServerPushKey 服务端推送标识键
	E2AWireServerPushKey = "_jiuwenswarm_server_push"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// E2AResponseKinds 运行时完整响应类型列表（12 种）
	E2AResponseKinds = []string{
		E2AResponseKindE2AComplete,
		E2AResponseKindE2AChunk,
		E2AResponseKindE2AError,
		E2AResponseKindACPSessionUpdate,
		E2AResponseKindACPPromptResult,
		E2AResponseKindACPJSONRPCError,
		E2AResponseKindACPOutputRequest,
		E2AResponseKindA2ATask,
		E2AResponseKindA2AMessage,
		E2AResponseKindA2AStreamEvent,
		E2AResponseKindCron,
		E2AResponseKindExt,
	}

	// E2AA2AStreamBranches A2A 流分支（4 种）
	E2AA2AStreamBranches = []string{
		"task",
		"message",
		"status_update",
		"artifact_update",
	}

	// ACPClientToAgentMethods 客户端→Agent JSON-RPC 方法（13 个）
	ACPClientToAgentMethods = []string{
		"initialize",
		"authenticate",
		"session/new",
		"session/load",
		"session/list",
		"session/set_mode",
		"session/set_config_option",
		"session/prompt",
		"session/set_model",
		"session/fork",
		"session/resume",
		"session/close",
		"logout",
	}

	// ACPAgentToClientMethods Agent→客户端方法（10 个）
	ACPAgentToClientMethods = []string{
		"session/update",
		"session/request_permission",
		"fs/read_text_file",
		"fs/write_text_file",
		"terminal/create",
		"terminal/output",
		"terminal/release",
		"terminal/wait_for_exit",
		"terminal/kill",
		"session/elicitation",
	}

	// ACPNotificationNames ACP 通知名（3 个）
	ACPNotificationNames = []string{
		"session/cancel",
		"session/update",
		"session/elicitation/complete",
	}

	// ACPSessionUpdateKinds 会话更新类型（12 个）
	ACPSessionUpdateKinds = []string{
		"user_message_chunk",
		"agent_message_chunk",
		"agent_thought_chunk",
		"tool_call",
		"tool_call_update",
		"todo_update",
		"plan",
		"available_commands_update",
		"current_mode_update",
		"config_option_update",
		"session_info_update",
		"usage_update",
	}

	// E2AWireInternalMetadataKeys Wire 内部元数据键集合
	E2AWireInternalMetadataKeys = map[string]struct{}{
		E2AWireServerPushKey:          {},
		E2AWireLegacyAgentChunkKey:    {},
		E2AWireLegacyAgentResponseKey: {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 constants_test.go**

```go
package e2a

import "testing"

// TestE2AResponseKinds_长度与内容 验证响应类型切片完整性
func TestE2AResponseKinds_长度与内容(t *testing.T) {
	if len(E2AResponseKinds) != 12 {
		t.Fatalf("期望 12 种 ResponseKind，实际 %d", len(E2AResponseKinds))
	}
	if E2AResponseKinds[0] != E2AResponseKindE2AComplete {
		t.Errorf("首个期望 %q，实际 %q", E2AResponseKindE2AComplete, E2AResponseKinds[0])
	}
	if E2AResponseKinds[11] != E2AResponseKindExt {
		t.Errorf("末个期望 %q，实际 %q", E2AResponseKindExt, E2AResponseKinds[11])
	}
}

// TestACPSessionUpdateKinds_包含关键字段 验证会话更新类型包含关键字段
func TestACPSessionUpdateKinds_包含关键字段(t *testing.T) {
	lookup := make(map[string]bool, len(ACPSessionUpdateKinds))
	for _, k := range ACPSessionUpdateKinds {
		lookup[k] = true
	}
	for _, key := range []string{"tool_call", "tool_call_update", "usage_update"} {
		if !lookup[key] {
			t.Errorf("ACPSessionUpdateKinds 缺少 %q", key)
		}
	}
}

// TestE2AWireInternalMetadataKeys_包含三个键 验证 Wire 内部键集合
func TestE2AWireInternalMetadataKeys_包含三个键(t *testing.T) {
	if len(E2AWireInternalMetadataKeys) != 3 {
		t.Fatalf("期望 3 个键，实际 %d", len(E2AWireInternalMetadataKeys))
	}
	for _, key := range []string{E2AWireServerPushKey, E2AWireLegacyAgentChunkKey, E2AWireLegacyAgentResponseKey} {
		if _, ok := E2AWireInternalMetadataKeys[key]; !ok {
			t.Errorf("E2AWireInternalMetadataKeys 缺少 %q", key)
		}
	}
}

// TestACPClientToAgentMethods_长度 验证客户端→Agent 方法数量
func TestACPClientToAgentMethods_长度(t *testing.T) {
	if len(ACPClientToAgentMethods) != 13 {
		t.Fatalf("期望 13 个方法，实际 %d", len(ACPClientToAgentMethods))
	}
	if len(ACPAgentToClientMethods) != 10 {
		t.Fatalf("期望 10 个方法，实际 %d", len(ACPAgentToClientMethods))
	}
	if len(ACPNotificationNames) != 3 {
		t.Fatalf("期望 3 个通知，实际 %d", len(ACPNotificationNames))
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/e2a/ -v -run "TestE2A|TestACP" -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/e2a/constants.go internal/swarm/e2a/constants_test.go
git commit -m "feat(e2a): 添加 E2A 协议常量（10.2.6）"
```

---

### Task 2: provenance.go — 子结构体

**Files:**
- Create: `internal/swarm/e2a/provenance.go`
- Test: `internal/swarm/e2a/provenance_test.go`

- [ ] **Step 1: 创建 provenance.go**

包含 IdentityOrigin 枚举（4 值 + Lookup 表 + Parse/IsValid/String/GoString）、E2AProvenance（4 字段 + 工厂）、E2AFileRef（5 字段 + 工厂）、E2AAuth（6 字段 + 工厂）。严格遵循规范2声明顺序。

完整代码参照设计文档第6节。关键点：
- `IdentityOrigin` 类型为 `string`，4 个常量值
- `identityOriginLookup` 包级变量 + `init()` 构建
- `NewE2AProvenance()` 设置 `SourceProtocol: E2ASourceProtocolE2A`
- `NewE2AFileRef(uri string)` 只设置 URI
- `NewE2AAuth()` 返回零值结构体
- E2AFileRef.Meta 和 E2AAuth.Meta 的 JSON tag 为 `"_meta"`（对齐 Python `_meta`）
- E2AAuth.ExtraHeaders 类型为 `map[string]string`

- [ ] **Step 2: 创建 provenance_test.go**

包含 11 个测试：
- IdentityOrigin 枚举：`TestIdentityOrigin_枚举值`、`TestParseIdentityOrigin_合法`、`TestParseIdentityOrigin_非法`、`TestIsValidIdentityOrigin`、`TestIdentityOrigin_String`、`TestIdentityOrigin_GoString`
- E2AProvenance：`TestNewE2AProvenance_默认值`（验证 SourceProtocol=="e2a"）、`TestE2AProvenance_JSON序列化往返`
- E2AFileRef：`TestNewE2AFileRef_默认值`、`TestE2AFileRef_JSON序列化往返`（验证 `_meta` tag）
- E2AAuth：`TestNewE2AAuth_默认值`、`TestE2AAuth_JSON序列化往返`（验证 `_meta` tag）

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/e2a/ -v -run "TestIdentityOrigin|TestNewE2A|TestE2AProvenance|TestE2AFileRef|TestE2AAuth" -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/e2a/provenance.go internal/swarm/e2a/provenance_test.go
git commit -m "feat(e2a): 添加 E2A 子结构体 IdentityOrigin/E2AProvenance/E2AFileRef/E2AAuth（10.2.3+10.2.4）"
```

---

### Task 3: envelope.go — E2AEnvelope + 序列化/反序列化

**Files:**
- Create: `internal/swarm/e2a/envelope.go`
- Test: `internal/swarm/e2a/envelope_test.go`

- [ ] **Step 1: 创建 envelope.go — 常量 + 结构体 + 工厂函数 + 方法**

包含：
- `E2AProtocolVersion = "1.0"` 常量
- `UTCNowISO() string` 函数（使用 `time.Now().UTC().Format(time.RFC3339Nano)`）
- `E2AEnvelope` 结构体（24 字段，全值类型，`jsonrpc_id` 为 `any`）
- `NewE2AEnvelope()` 工厂函数（设 ProtocolVersion="1.0", Provenance=NewE2AProvenance(), IdentityOrigin=IdentityOriginUser）
- `EnsureTimestamp()` 方法
- `ToMap()` 方法（调用 `structToMap`）

- [ ] **Step 2: 创建 envelope.go — map 提取辅助函数**

9 个非导出辅助函数：
- `getString(m map[string]any, key string) string`：取字符串，不存在返回 ""
- `getStringWithDefault(m map[string]any, key string, def string) string`：带默认值取字符串
- `getStringOrEmpty(m map[string]any, key string) string`：取字符串，nil→""
- `getBool(m map[string]any, key string, def bool) bool`：取布尔值
- `getInt(m map[string]any, key string, def int) int`：取整数
- `getMapAny(m map[string]any, key string) map[string]any`：取通用 map，nil→空 map
- `getMapString(m map[string]any, key string) map[string]string`：取字符串 map
- `getStringSlice(m map[string]any, key string) []string`：取字符串切片
- `isEmptySliceOrMap(v any) bool`：判断空切片/空 map

- [ ] **Step 3: 创建 envelope.go — 序列化辅助函数**

- `enumValue(v any) any`：枚举值提取（如果 v 有 String() 方法且不是 string/fmt.Stringer 标准类型，返回 String() 值）
- `structToMap(v any) map[string]any`：递归结构体→map，使用 json.Marshal→json.Unmarshal 中转

- [ ] **Step 4: 创建 envelope.go — 反序列化辅助函数**

- `provenanceFromMap(raw any) E2AProvenance`：一比一对照 Python `_provenance_from_dict`（3 分支：nil→默认值、E2AProvenance类型→原样、map→解析）
- `normalizeTimestampValue(raw any) string`：一比一对照 Python `_normalize_timestamp_value`（nil→""、string→原样、float64/int→RFC3339、其他→fmt.Sprintf）
- `migrateLegacyBinding(data map[string]any, prov E2AProvenance) E2AProvenance`：一比一对照 Python `_migrate_legacy_binding`（binding→details.migrated_from_binding，source_protocol 重写逻辑）
- `paramsWithOptionalLegacyPayload(data map[string]any) map[string]any`：一比一对照 Python `_params_with_optional_legacy_payload`（payload 合并到 params，不覆盖，跳过 nil/空切片/空 map）

- [ ] **Step 5: 创建 envelope.go — EnvelopeFromMap + MergeParamsToACPPrompt**

- `EnvelopeFromMap(data map[string]any) *E2AEnvelope`：一比一对照 Python `_envelope_from_dict`，完整包含 5 种 Legacy 兼容逻辑
- `MergeParamsToACPPrompt(env *E2AEnvelope) map[string]any`：一比一对照 Python `merge_params_to_acp_prompt`（method=="session/prompt" 时补全 prompt，4 级优先级：已有 prompt > content_blocks > text/content/query > 无）

- [ ] **Step 6: 创建 envelope_test.go**

28 个测试，覆盖：
- 工厂默认值：`TestNewE2AEnvelope_默认值`
- EnsureTimestamp：`TestE2AEnvelope_EnsureTimestamp_未设置`、`TestE2AEnvelope_EnsureTimestamp_已设置`
- ToMap：`TestE2AEnvelope_ToMap_枚举值`、`TestE2AEnvelope_ToMap_嵌套Provenance`
- EnvelopeFromMap 完整：`TestEnvelopeFromMap_完整字段`
- Legacy 兼容（5 种）：`TestEnvelopeFromMap_legacy_binding迁移`、`TestEnvelopeFromMap_legacy_payload合并`、`TestEnvelopeFromMap_legacy_req_method`、`TestEnvelopeFromMap_legacy_channel_id`、`TestEnvelopeFromMap_legacy_metadata合并`
- 时间戳规范化：`TestEnvelopeFromMap_timestamp_float转RFC3339`
- Auth 解析：`TestEnvelopeFromMap_auth解析_map`
- normalizeTimestampValue：`TestNormalizeTimestampValue_string`、`TestNormalizeTimestampValue_float`、`TestNormalizeTimestampValue_int`、`TestNormalizeTimestampValue_nil`
- migrateLegacyBinding：`TestMigrateLegacyBinding_无binding字段`、`TestMigrateLegacyBinding_binding为ACP`、`TestMigrateLegacyBinding_binding为dict含value`
- paramsWithOptionalLegacyPayload：`TestParamsWithOptionalLegacyPayload_有payload`、`TestParamsWithOptionalLegacyPayload_无payload`
- MergeParamsToACPPrompt：`TestMergeParamsToACPPrompt_非prompt方法`、`TestMergeParamsToACPPrompt_已有prompt`、`TestMergeParamsToACPPrompt_content_blocks转prompt`、`TestMergeParamsToACPPrompt_text转prompt`、`TestMergeParamsToACPPrompt_补session_id`、`TestMergeParamsToACPPrompt_补acp_meta`
- 序列化往返：`TestE2AEnvelope_JSON序列化往返`
- FromMap→ToMap 往返：`TestEnvelopeFromMap_ToMap_往返`

- [ ] **Step 7: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/e2a/ -v -run "TestNewE2AEnvelope|TestE2AEnvelope_|TestEnvelopeFromMap|TestNormalize|TestMigrate|TestParamsWith|TestMergeParams|TestGetString|TestGetBool|TestIsEmpty" -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/e2a/envelope.go internal/swarm/e2a/envelope_test.go
git commit -m "feat(e2a): 添加 E2AEnvelope 请求信封 + 序列化/反序列化 + Legacy 兼容（10.2.1）"
```

---

### Task 4: response.go — E2AResponse + 反序列化

**Files:**
- Create: `internal/swarm/e2a/response.go`
- Test: `internal/swarm/e2a/response_test.go`

- [ ] **Step 1: 创建 response.go**

包含：
- `E2AResponse` 结构体（22 字段，全值类型，`jsonrpc_id` 为 `any`，`IdentityOrigin` 默认 `AGENT`，`Status` 默认 `"in_progress"`）
- `NewE2AResponse()` 工厂函数（设 ProtocolVersion="1.0", Status=E2AResponseStatusInProgress, IdentityOrigin=IdentityOriginAgent, Provenance=NewE2AProvenance()）
- `EnsureTimestamp()` 方法
- `ToMap()` 方法（调用 `structToMap`）
- `ResponseFromMap(data map[string]any) *E2AResponse`：一比一对照 Python `_e2a_response_from_dict`（identity_origin 默认 AGENT、sequence 容错、channel 兼容 channel_id、无 legacy binding/payload/auth/metadata 合并）

- [ ] **Step 2: 创建 response_test.go**

16 个测试：
- 工厂默认值：`TestNewE2AResponse_默认值`（验证 ProtocolVersion="1.0", Status="in_progress", IdentityOrigin=AGENT）
- EnsureTimestamp：`TestE2AResponse_EnsureTimestamp_未设置`、`TestE2AResponse_EnsureTimestamp_已设置`
- ToMap：`TestE2AResponse_ToMap_枚举值`、`TestE2AResponse_ToMap_嵌套Provenance`、`TestE2AResponse_ToMap_全部字段`
- ResponseFromMap：`TestResponseFromMap_完整字段`、`TestResponseFromMap_identity_origin默认AGENT`、`TestResponseFromMap_sequence容错_float64`、`TestResponseFromMap_sequence容错_非数字`、`TestResponseFromMap_channel兼容channel_id`、`TestResponseFromMap_timestamp规范化`、`TestResponseFromMap_status默认in_progress`、`TestResponseFromMap_response_kind空串`
- 序列化往返：`TestE2AResponse_JSON序列化往返`
- FromMap→ToMap 往返：`TestResponseFromMap_ToMap_往返`

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/e2a/ -v -run "TestNewE2AResponse|TestE2AResponse_|TestResponseFromMap" -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/e2a/response.go internal/swarm/e2a/response_test.go
git commit -m "feat(e2a): 添加 E2AResponse 响应模型 + 反序列化（10.2.2）"
```

---

### Task 5: doc.go + 全量测试 + 覆盖率

**Files:**
- Create: `internal/swarm/e2a/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（更新 10.2.1~10.2.4+10.2.6 状态为 ✅）

- [ ] **Step 1: 创建 doc.go**

```go
// Package e2a 提供 E2A（Everything-to-Agent）统一信封协议的数据模型和编解码。
//
// E2A 协议是 Gateway 与 AgentServer 之间的核心通信协议，将 ACP、A2A 等多种外部协议消息
// 统一转换为 E2AEnvelope 请求信封和 E2AResponse 响应模型，实现协议无关的内部通信。
//
// 本包定义了 E2A 协议的完整数据模型，包括：
//   - 协议常量（来源协议、响应状态、响应类型、ACP 方法名、Wire 键名等）
//   - 子结构体（IdentityOrigin 枚举、E2AProvenance 出处追踪、E2AFileRef 文件引用、E2AAuth 身份鉴权）
//   - 请求信封（E2AEnvelope）及其序列化/反序列化，含 5 种 Legacy 兼容逻辑
//   - 响应模型（E2AResponse）及其序列化/反序列化
//   - ACP 参数补全（MergeParamsToACPPrompt）
//
// 文件目录：
//
//	e2a/
//	├── doc.go           # 包文档
//	├── constants.go     # 协议常量（来源协议、响应状态、响应类型、ACP 方法名、Wire 键名）
//	├── provenance.go    # IdentityOrigin 枚举 + E2AProvenance/E2AFileRef/E2AAuth 子结构体
//	├── envelope.go      # E2AEnvelope 请求信封 + 序列化/反序列化 + Legacy 兼容 + MergeParamsToACPPrompt
//	└── response.go      # E2AResponse 响应模型 + 序列化/反序列化
//
// 对应 Python 代码：jiuwenswarm/common/e2a/
package e2a
```

- [ ] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/e2a/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/e2a/ -count=1`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md 状态**

将 10.2.1、10.2.2、10.2.3、10.2.4、10.2.6 的状态从 `☐` 更新为 `✅`。

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/e2a/doc.go IMPLEMENTATION_PLAN.md
git commit -m "feat(e2a): 添加包文档 + 完成步骤 10.2.1~10.2.4+10.2.6"
```

---

## 自检清单

- [x] **Spec 覆盖**：设计文档 11 个章节均有对应 Task（constants→Task1, provenance→Task2, envelope→Task3, response→Task4, doc+覆盖率→Task5）
- [x] **占位符扫描**：无 TBD/TODO/类似实现/待填充内容
- [x] **类型一致性**：E2AProvenance 在 Task2 定义，Task3/Task4 引用一致；IdentityOrigin 在 Task2 定义，Task3/Task4 引用一致；E2AAuth 在 Task2 定义，Task3 引用一致；所有 JSON tag 与设计文档一致
