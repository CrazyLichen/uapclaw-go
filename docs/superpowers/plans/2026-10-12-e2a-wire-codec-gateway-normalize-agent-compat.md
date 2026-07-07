# Wire Codec + gateway_normalize + agent_compat 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 10.2.5 Wire Codec + 10.2.7 gateway_normalize + 10.2.8 agent_compat，完成 E2A 协议层核心编解码和格式互转，同时将 Payload 字段从 json.RawMessage 改为 map[string]any 对齐 Python。

**Architecture:** 自底向上：先改造 Payload 类型（前置依赖），再实现 gateway_normalize（格式互转核心），再实现 wire_codec（编解码桥梁），最后实现 agent_compat（E2A→AgentRequest 转换）。每个模块先写测试再实现。

**Tech Stack:** Go 1.22+, encoding/json, internal/common/logger（ComponentChannel）

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|----------|------|
| 修改 | `internal/swarm/schema/agent.go` | Payload 类型 json.RawMessage → map[string]any，IsTerminal() 重写 |
| 修改 | `internal/swarm/schema/agent_test.go` | 测试适配新 Payload 类型 |
| 修改 | `internal/swarm/schema/message.go` | Message.Payload → map[string]any，工厂函数参数变更 |
| 修改 | `internal/swarm/schema/message_test.go` | 测试适配新 Payload 类型 |
| 创建 | `internal/swarm/e2a/gateway_normalize.go` | Message↔E2A、AgentResponse/Chunk↔E2AResponse 格式互转 |
| 创建 | `internal/swarm/e2a/gateway_normalize_test.go` | gateway_normalize 测试 |
| 创建 | `internal/swarm/e2a/wire_codec.go` | E2A 线编解码 + fallback |
| 创建 | `internal/swarm/e2a/wire_codec_test.go` | wire_codec 测试 |
| 创建 | `internal/swarm/e2a/agent_compat.go` | E2AEnvelope → AgentRequest |
| 创建 | `internal/swarm/e2a/agent_compat_test.go` | agent_compat 测试 |
| 修改 | `internal/swarm/e2a/doc.go` | 文件目录更新 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 10.2.5/10.2.7/10.2.8 状态回填 |

---

### Task 1: Payload 类型改造 — schema/agent.go

**Files:**
- Modify: `internal/swarm/schema/agent.go`
- Test: `internal/swarm/schema/agent_test.go`

- [ ] **Step 1: 修改 AgentResponse.Payload 和 AgentResponseChunk.Payload 类型**

在 `internal/swarm/schema/agent.go` 中：

将 `AgentResponse.Payload` 从 `json.RawMessage` 改为 `map[string]any`：
```go
// Payload 响应负载
Payload map[string]any `json:"payload,omitempty"`
```

将 `AgentResponseChunk.Payload` 从 `json.RawMessage` 改为 `map[string]any`：
```go
// Payload 响应负载片段
Payload map[string]any `json:"payload,omitempty"`
```

- [ ] **Step 2: 修改 Option 函数和工厂函数**

```go
// WithPayload 设置响应负载。
func WithPayload(p map[string]any) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Payload = p }
}

// WithChunkPayload 设置响应负载片段。
func WithChunkPayload(p map[string]any) AgentResponseChunkOption {
	return func(chunk *AgentResponseChunk) { chunk.Payload = p }
}

// NewAgentResponseChunk 创建 Agent 响应片段实例。
func NewAgentResponseChunk(requestID, channelID string, payload map[string]any, opts ...AgentResponseChunkOption) *AgentResponseChunk {
```

- [ ] **Step 3: 修改 NewTerminalChunk**

```go
func NewTerminalChunk(requestID, channelID string) *AgentResponseChunk {
	return &AgentResponseChunk{
		RequestID:  requestID,
		ChannelID:  channelID,
		Payload:    map[string]any{"is_complete": true},
		IsComplete: true,
	}
}
```

- [ ] **Step 4: 重写 IsTerminal() 方法**

```go
func (c *AgentResponseChunk) IsTerminal() bool {
	if !c.IsComplete {
		return false
	}
	// payload 为 nil → 终止哨兵（形态 A）
	if c.Payload == nil {
		return true
	}
	// 含 event_type → 非终止哨兵
	if _, ok := c.Payload["event_type"]; ok {
		return false
	}
	// 含 content 非空 → 非终止哨兵
	if v, ok := c.Payload["content"]; ok && v != nil {
		if s, isStr := v.(string); isStr && s != "" {
			return false
		}
		if !isStr {
			return false
		}
	}
	// 含 error 非空 → 非终止哨兵
	if v, ok := c.Payload["error"]; ok && v != nil {
		if s, isStr := v.(string); isStr && s != "" {
			return false
		}
		if !isStr {
			return false
		}
	}
	// 仅含 is_complete=true 且无其他业务键 → 终止哨兵（形态 B）
	if v, ok := c.Payload["is_complete"]; ok {
		if b, isBool := v.(bool); isBool && b {
			if len(c.Payload) == 1 {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 5: 移除 agent.go 中不再需要的 import**

`encoding/json` 在 `IsTerminal()` 中不再使用，但 `AgentRequest.Params` 仍为 `json.RawMessage`，所以保留。检查编译：
```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/...
```

---

### Task 2: Payload 类型改造 — schema/agent_test.go

**Files:**
- Modify: `internal/swarm/schema/agent_test.go`

- [ ] **Step 1: 修改 TestNewAgentResponse_使用Option**

将 `json.RawMessage(...)` 构造改为 `map[string]any{...}`：
```go
func TestNewAgentResponse_使用Option(t *testing.T) {
	payload := map[string]any{"content": "answer"}
	resp := NewAgentResponse("req-1", "web",
		WithResponseOK(false),
		WithPayload(payload),
		WithResponseMetadata(map[string]any{"key": "val"}),
	)

	if resp.OK {
		t.Error("OK 应为 false（被 Option 覆盖）")
	}
	if resp.Payload["content"] != "answer" {
		t.Errorf("Payload[\"content\"] = %v, 期望 \"answer\"", resp.Payload["content"])
	}
	if resp.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
}
```

- [ ] **Step 2: 修改 TestAgentResponse_JSON往返**

```go
func TestAgentResponse_JSON往返(t *testing.T) {
	original := &AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "final answer"},
		Metadata:  map[string]any{"method": "chat.send"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded AgentResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.OK != original.OK {
		t.Errorf("OK: got %v, want %v", decoded.OK, original.OK)
	}
	if decoded.Payload["content"] != "final answer" {
		t.Errorf("Payload[\"content\"]: got %v, want \"final answer\"", decoded.Payload["content"])
	}
	if decoded.Metadata["method"] != "chat.send" {
		t.Errorf("Metadata[\"method\"]: got %v, want \"chat.send\"", decoded.Metadata["method"])
	}
}
```

- [ ] **Step 3: 修改 AgentResponseChunk 相关测试**

`TestNewAgentResponseChunk`:
```go
func TestNewAgentResponseChunk(t *testing.T) {
	payload := map[string]any{"event_type": "chat.delta", "content": "hello"}
	chunk := NewAgentResponseChunk("req-1", "web", payload)
	// ... 断言改为读 map 字段
}
```

`TestNewAgentResponseChunk_使用Option`:
```go
func TestNewAgentResponseChunk_使用Option(t *testing.T) {
	payload := map[string]any{"event_type": "chat.delta", "content": "hello"}
	newPayload := map[string]any{"event_type": "chat.final", "content": "answer"}
	chunk := NewAgentResponseChunk("req-1", "web", payload,
		WithChunkIsComplete(true),
		WithChunkPayload(newPayload),
	)
	// ... 断言改为读 map 字段
}
```

`TestNewTerminalChunk`:
```go
func TestNewTerminalChunk(t *testing.T) {
	chunk := NewTerminalChunk("req-1", "web")
	// ... 断言 chunk.Payload["is_complete"] == true
}
```

- [ ] **Step 4: 修改 IsTerminal 测试**

`TestAgentResponseChunk_IsTerminal_含eventtype`:
```go
chunk := &AgentResponseChunk{
	RequestID:  "req-1",
	ChannelID:  "web",
	Payload:    map[string]any{"event_type": "chat.error", "error": "something"},
	IsComplete: true,
}
```

`TestAgentResponseChunk_IsTerminal_含content`:
```go
chunk := &AgentResponseChunk{
	RequestID:  "req-1",
	ChannelID:  "web",
	Payload:    map[string]any{"content": "final answer"},
	IsComplete: true,
}
```

`TestAgentResponseChunk_IsTerminal_含error`:
```go
chunk := &AgentResponseChunk{
	RequestID:  "req-1",
	ChannelID:  "web",
	Payload:    map[string]any{"error": "bad"},
	IsComplete: true,
}
```

`TestAgentResponseChunk_IsTerminal_中间chunk`:
```go
chunk := NewAgentResponseChunk("req-1", "web", map[string]any{"event_type": "chat.delta", "content": "hi"})
```

- [ ] **Step 5: 修改 AgentResponseChunk JSON 往返和 Validate 测试**

`TestAgentResponseChunk_JSON往返`、`TestAgentResponseChunk_IsComplete为true`、`TestAgentResponseChunk_Validate_正常` 等测试中的 `json.RawMessage(...)` 全部改为 `map[string]any{...}`。

- [ ] **Step 6: 运行 schema 包测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/...
```

预期：全部 PASS

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/schema/agent.go internal/swarm/schema/agent_test.go
git commit -m "refactor(schema): AgentResponse/Chunk.Payload 从 json.RawMessage 改为 map[string]any 对齐 Python"
```

---

### Task 3: Payload 类型改造 — schema/message.go + message_test.go

**Files:**
- Modify: `internal/swarm/schema/message.go`
- Modify: `internal/swarm/schema/message_test.go`

- [ ] **Step 1: 修改 Message.Payload 类型和工厂函数**

在 `message.go` 中：
```go
// Payload 响应/事件负载（res/event 用，req 为 nil）
Payload map[string]any `json:"payload,omitempty"`
```

修改 `NewResMessage` 和 `NewEventMessage` 签名：
```go
func NewResMessage(channelID, sessionID string, ok bool, payload map[string]any, opts ...MessageOption) *Message {
func NewEventMessage(channelID, sessionID string, eventType EventType, payload map[string]any, opts ...MessageOption) *Message {
```

- [ ] **Step 2: 修改 message_test.go 中所有 json.RawMessage Payload 构造**

所有 `Payload: json.RawMessage(...)` 改为 `Payload: map[string]any{...}`。
所有 `string(msg.Payload)` / `string(decoded.Payload)` 断言改为读 map 字段断言。
工厂函数调用中 `json.RawMessage(...)` payload 参数改为 `map[string]any{...}`。

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/schema/message.go internal/swarm/schema/message_test.go
git commit -m "refactor(schema): Message.Payload 从 json.RawMessage 改为 map[string]any 对齐 Python"
```

---

### Task 4: 实现 gateway_normalize.go — 请求方向

**Files:**
- Create: `internal/swarm/e2a/gateway_normalize.go`
- Create: `internal/swarm/e2a/gateway_normalize_test.go`

- [ ] **Step 1: 编写请求方向测试**

在 `gateway_normalize_test.go` 中编写：

```go
package e2a

import (
	"encoding/json"
	"testing"

	"uapclaw-gateway/internal/swarm/schema"
)

func TestMessageToLegacyAgentDict(t *testing.T) {
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{"query":"hello"}`))
	dict := MessageToLegacyAgentDict(msg)
	if dict["request_id"] != msg.ID {
		t.Errorf("request_id = %v, 期望 %s", dict["request_id"], msg.ID)
	}
	if dict["channel_id"] != "web" {
		t.Errorf("channel_id = %v, 期望 \"web\"", dict["channel_id"])
	}
	if dict["req_method"] != "chat.send" {
		t.Errorf("req_method = %v, 期望 \"chat.send\"", dict["req_method"])
	}
	if dict["is_stream"] != false {
		t.Error("is_stream 期望 false")
	}
}

func TestMessageToE2A(t *testing.T) {
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{"query":"hello"}`))
	env, err := MessageToE2A(msg)
	if err != nil {
		t.Fatalf("MessageToE2A 失败: %v", err)
	}
	if env.RequestID != msg.ID {
		t.Errorf("RequestID = %q, 期望 %s", env.RequestID, msg.ID)
	}
	if env.Channel != "web" {
		t.Errorf("Channel = %q, 期望 \"web\"", env.Channel)
	}
	if env.Method != "chat.send" {
		t.Errorf("Method = %q, 期望 \"chat.send\"", env.Method)
	}
}

func TestMessageToE2A_EnableMemory逻辑(t *testing.T) {
	// enable_memory=false, group_digital_avatar=true, avatar_mode=true → should_disable=true
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithEnableMemory(false),
		schema.WithGroupDigitalAvatar(true),
		schema.WithMetadata(map[string]any{"avatar_mode": true}),
	)
	env, err := MessageToE2A(msg)
	if err != nil {
		t.Fatalf("MessageToE2A 失败: %v", err)
	}
	meta, _ := env.Metadata["enable_memory"].(bool)
	if meta {
		t.Error("enable_memory 应为 false（should_disable=true）")
	}
}

func TestMessageToE2AOrFallback_成功(t *testing.T) {
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	env := MessageToE2AOrFallback(msg)
	if env == nil {
		t.Fatal("返回 nil")
	}
	if env.RequestID != msg.ID {
		t.Errorf("RequestID 不一致")
	}
}

func TestBuildFallbackE2A(t *testing.T) {
	legacy := map[string]any{"request_id": "req-1", "channel_id": "web"}
	env := BuildFallbackE2A(legacy)
	if env.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", env.RequestID)
	}
	ctx := env.ChannelContext
	internal, ok := ctx["_jiuwenswarm"].(map[string]any)
	if !ok {
		t.Fatal("channel_context._jiuwenswarm 不存在")
	}
	if normalizeFailed, _ := internal["normalize_failed"].(bool); !normalizeFailed {
		t.Error("normalize_failed 应为 true")
	}
}

func TestE2AFromAgentFields(t *testing.T) {
	env := E2AFromAgentFields(
		WithFieldRequestID("req-1"),
		WithFieldChannelID("web"),
		WithFieldReqMethod(schema.ReqMethodChatSend),
		WithFieldIsStream(true),
	)
	if env.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", env.RequestID)
	}
	if env.Method != "chat.send" {
		t.Errorf("Method = %q, 期望 \"chat.send\"", env.Method)
	}
	if !env.IsStream {
		t.Error("IsStream 应为 true")
	}
}

func TestChannelContextForChannelReply(t *testing.T) {
	env := &E2AEnvelope{
		ChannelContext: map[string]any{
			"_jiuwenswarm": map[string]any{"normalize_failed": true},
			"trace_id":     "trace-123",
		},
	}
	ctx := ChannelContextForChannelReply(env)
	if _, ok := ctx["_jiuwenswarm"]; ok {
		t.Error("应去掉 _jiuwenswarm 内部键")
	}
	if ctx["trace_id"] != "trace-123" {
		t.Error("应保留业务键 trace_id")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/e2a/... -run "TestMessageToLegacyAgentDict|TestMessageToE2A|TestMessageToE2AOrFallback|TestBuildFallbackE2A|TestE2AFromAgentFields|TestChannelContextForChannelReply"
```

预期：编译失败（函数未定义）

- [ ] **Step 3: 实现 gateway_normalize.go 请求方向**

创建 `internal/swarm/e2a/gateway_normalize.go`，包含：
- 内部常量：`e2aInternalContextKey`、`e2aFallbackFailedKey`、`e2aLegacyAgentRequestKey`、`maxLegacyAgentRequestJSONBytes`
- `AgentFieldOption` 函数选项类型及 8 个 WithField* 选项
- `MessageToLegacyAgentDict`
- `legacyPayloadWithinLimit`（非导出）
- `BuildFallbackE2A`
- `MessageToE2A`（含 enable_memory/group_digital_avatar 逻辑）
- `MessageToE2AOrFallback`
- `E2AFromAgentFields`
- `ChannelContextForChannelReply`

按照源码声明排列顺序：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/e2a/... -run "TestMessageToLegacyAgentDict|TestMessageToE2A|TestMessageToE2AOrFallback|TestBuildFallbackE2A|TestE2AFromAgentFields|TestChannelContextForChannelReply"
```

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/e2a/gateway_normalize.go internal/swarm/e2a/gateway_normalize_test.go
git commit -m "feat(e2a): 实现 gateway_normalize 请求方向 Message→E2AEnvelope"
```

---

### Task 5: 实现 gateway_normalize.go — 响应方向

**Files:**
- Modify: `internal/swarm/e2a/gateway_normalize.go`
- Modify: `internal/swarm/e2a/gateway_normalize_test.go`

- [ ] **Step 1: 编写响应方向测试**

```go
func TestE2AResponseFromAgentResponse_ok(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "answer"},
	}
	e2a := E2AResponseFromAgentResponse(resp, "resp-1", 0)
	if e2a.ResponseKind != E2AResponseKindE2AComplete {
		t.Errorf("ResponseKind = %q, 期望 e2a.complete", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusSucceeded {
		t.Errorf("Status = %q, 期望 succeeded", e2a.Status)
	}
	if !e2a.IsFinal {
		t.Error("IsFinal 应为 true")
	}
}

func TestE2AResponseFromAgentResponse_失败(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        false,
		Payload:   map[string]any{"error": "something failed"},
	}
	e2a := E2AResponseFromAgentResponse(resp, "resp-1", 0)
	if e2a.ResponseKind != E2AResponseKindE2AError {
		t.Errorf("ResponseKind = %q, 期望 e2a.error", e2a.ResponseKind)
	}
	if e2a.Status != E2AResponseStatusFailed {
		t.Errorf("Status = %q, 期望 failed", e2a.Status)
	}
}

func TestE2AResponseFromAgentChunk_终止帧(t *testing.T) {
	chunk := schema.NewTerminalChunk("req-1", "web")
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true)
	if e2a.ResponseKind != E2AResponseKindE2AComplete {
		t.Errorf("ResponseKind = %q, 期望 e2a.complete", e2a.ResponseKind)
	}
	if !e2a.IsFinal {
		t.Error("IsFinal 应为 true")
	}
}

func TestE2AResponseFromAgentChunk_错误帧(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID:  "req-1",
		ChannelID:  "web",
		Payload:    map[string]any{"event_type": "chat.error", "error": "timeout"},
		IsComplete: true,
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true)
	if e2a.ResponseKind != E2AResponseKindE2AError {
		t.Errorf("ResponseKind = %q, 期望 e2a.error", e2a.ResponseKind)
	}
}

func TestE2AResponseFromAgentChunk_中间帧(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "req-1",
		ChannelID: "web",
		Payload:   map[string]any{"event_type": "chat.delta", "content": "hello", "source_chunk_type": "llm_reasoning"},
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true)
	if e2a.ResponseKind != E2AResponseKindE2AChunk {
		t.Errorf("ResponseKind = %q, 期望 e2a.chunk", e2a.ResponseKind)
	}
	if e2a.Body["delta_kind"] != "reasoning" {
		t.Errorf("delta_kind = %v, 期望 \"reasoning\"", e2a.Body["delta_kind"])
	}
}

func TestE2AResponseToAgentResponse(t *testing.T) {
	e2a := &E2AResponse{
		ResponseKind: E2AResponseKindE2AComplete,
		Status:       E2AResponseStatusSucceeded,
		RequestID:    "req-1",
		Body:         map[string]any{"result": map[string]any{"content": "answer"}},
		IsFinal:      true,
	}
	resp, err := E2AResponseToAgentResponse(e2a)
	if err != nil {
		t.Fatalf("E2AResponseToAgentResponse 失败: %v", err)
	}
	if !resp.OK {
		t.Error("OK 应为 true")
	}
	if resp.Payload["content"] != "answer" {
		t.Errorf("Payload[\"content\"] = %v, 期望 \"answer\"", resp.Payload["content"])
	}
}

func TestE2AResponseToAgentChunk_终止帧(t *testing.T) {
	e2a := &E2AResponse{
		ResponseKind: E2AResponseKindE2AComplete,
		Status:       E2AResponseStatusSucceeded,
		RequestID:    "req-1",
		Body:         map[string]any{"result": map[string]any{}},
		IsFinal:      true,
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("E2AResponseToAgentChunk 失败: %v", err)
	}
	if !chunk.IsComplete {
		t.Error("IsComplete 应为 true")
	}
}

func TestE2AResponseToAgentChunk_chatDelta(t *testing.T) {
	e2a := &E2AResponse{
		ResponseKind: E2AResponseKindE2AChunk,
		Status:       E2AResponseStatusInProgress,
		RequestID:    "req-1",
		Body: map[string]any{
			"delta_kind":        "reasoning",
			"delta":             "thinking...",
			"event_type":        "chat.delta",
			"source_chunk_type": "text",
		},
		IsFinal: false,
	}
	chunk, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("E2AResponseToAgentChunk 失败: %v", err)
	}
	if chunk.Payload["source_chunk_type"] != "llm_reasoning" {
		t.Errorf("source_chunk_type = %v, 期望 \"llm_reasoning\"", chunk.Payload["source_chunk_type"])
	}
}

// 往返测试
func TestE2AResponseRoundtrip_非流式(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "answer"},
	}
	e2a := E2AResponseFromAgentResponse(resp, "resp-1", 0)
	back, err := E2AResponseToAgentResponse(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if back.RequestID != resp.RequestID {
		t.Errorf("RequestID 往返不一致")
	}
	if back.OK != resp.OK {
		t.Errorf("OK 往返不一致")
	}
}

func TestE2AResponseRoundtrip_流式(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "req-1",
		ChannelID: "web",
		Payload:   map[string]any{"event_type": "chat.delta", "content": "hello"},
	}
	e2a := E2AResponseFromAgentChunk(chunk, "resp-1", 1, true)
	back, err := E2AResponseToAgentChunk(e2a)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if back.RequestID != chunk.RequestID {
		t.Errorf("RequestID 往返不一致")
	}
	if back.Payload["content"] != "hello" {
		t.Errorf("Payload[\"content\"] 往返不一致")
	}
}
```

- [ ] **Step 2: 实现响应方向函数**

在 `gateway_normalize.go` 中添加：
- `ResponseNormOption` + `WithNormTimestamp`
- `E2AResponseFromAgentResponse`
- `E2AResponseFromAgentChunk`（含终止帧/错误帧/业务结束帧/中间帧四段分支）
- `E2AResponseToAgentResponse`
- `E2AResponseToAgentChunk`（含 e2a.complete/e2a.error/e2a.chunk/cron/acp.output_request 五段分支）

- [ ] **Step 3: 运行全部 gateway_normalize 测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/e2a/... -run "TestMessageTo|TestBuild|TestE2AFrom|TestChannel|TestE2AResponse"
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/e2a/gateway_normalize.go internal/swarm/e2a/gateway_normalize_test.go
git commit -m "feat(e2a): 实现 gateway_normalize 响应方向 AgentResponse/Chunk↔E2AResponse"
```

---

### Task 6: 实现 wire_codec.go

**Files:**
- Create: `internal/swarm/e2a/wire_codec.go`
- Create: `internal/swarm/e2a/wire_codec_test.go`

- [ ] **Step 1: 编写 wire_codec 测试**

```go
package e2a

import (
	"testing"

	"uapclaw-gateway/internal/swarm/schema"
)

func TestIsE2AResponseWireDict(t *testing.T) {
	// E2A 格式
	data := map[string]any{
		"protocol_version": "1.0",
		"response_kind":    "e2a.complete",
		"request_id":       "req-1",
	}
	if !IsE2AResponseWireDict(data) {
		t.Error("E2A 格式应返回 true")
	}

	// 事件帧
	eventData := map[string]any{"type": "event"}
	if IsE2AResponseWireDict(eventData) {
		t.Error("事件帧应返回 false")
	}

	// 非 E2A 格式
	nonE2A := map[string]any{"request_id": "req-1", "channel_id": "web", "ok": true}
	if IsE2AResponseWireDict(nonE2A) {
		t.Error("非 E2A 格式应返回 false")
	}
}

func TestEncodeAgentResponseForWire(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "answer"},
	}
	wire := EncodeAgentResponseForWire(resp, "resp-1", 0)
	if wire["protocol_version"] != "1.0" {
		t.Errorf("protocol_version = %v, 期望 \"1.0\"", wire["protocol_version"])
	}
	if wire["response_kind"] != "e2a.complete" {
		t.Errorf("response_kind = %v, 期望 \"e2a.complete\"", wire["response_kind"])
	}
}

func TestEncodeAgentChunkForWire(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "req-1",
		ChannelID: "web",
		Payload:   map[string]any{"event_type": "chat.delta", "content": "hello"},
	}
	wire := EncodeAgentChunkForWire(chunk, "resp-1", 1, true)
	if wire["response_kind"] != "e2a.chunk" {
		t.Errorf("response_kind = %v, 期望 \"e2a.chunk\"", wire["response_kind"])
	}
}

func TestParseAgentServerWireUnary(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "answer"},
	}
	wire := EncodeAgentResponseForWire(resp, "resp-1", 0)
	back, err := ParseAgentServerWireUnary(wire)
	if err != nil {
		t.Fatalf("ParseAgentServerWireUnary 失败: %v", err)
	}
	if back.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", back.RequestID)
	}
	if !back.OK {
		t.Error("OK 应为 true")
	}
}

func TestParseAgentServerWireChunk(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "req-1",
		ChannelID: "web",
		Payload:   map[string]any{"event_type": "chat.delta", "content": "hello"},
	}
	wire := EncodeAgentChunkForWire(chunk, "resp-1", 1, true)
	back, err := ParseAgentServerWireChunk(wire)
	if err != nil {
		t.Fatalf("ParseAgentServerWireChunk 失败: %v", err)
	}
	if back.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", back.RequestID)
	}
}

func TestParseAgentServerWireUnary_非E2A格式(t *testing.T) {
	data := map[string]any{"request_id": "req-1", "channel_id": "web", "ok": true}
	_, err := ParseAgentServerWireUnary(data)
	if err == nil {
		t.Error("非 E2A 格式应返回 error")
	}
}

func TestEncodeJSONParseErrorWire(t *testing.T) {
	wire := EncodeJSONParseErrorWire("req-1", "web", "invalid JSON")
	if wire["response_kind"] != "e2a.error" {
		t.Errorf("response_kind = %v, 期望 \"e2a.error\"", wire["response_kind"])
	}
	body, ok := wire["body"].(map[string]any)
	if !ok {
		t.Fatal("body 应为 map[string]any")
	}
	if body["code"] != "E2A.INVALID_JSON" {
		t.Errorf("body.code = %v, 期望 \"E2A.INVALID_JSON\"", body["code"])
	}
}

// 端到端往返测试
func TestWireCodecRoundtrip_非流式(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1",
		ChannelID: "web",
		OK:        true,
		Payload:   map[string]any{"content": "answer"},
	}
	wire := EncodeAgentResponseForWire(resp, "resp-1", 0)
	back, err := ParseAgentServerWireUnary(wire)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if back.RequestID != resp.RequestID || back.OK != resp.OK {
		t.Error("往返不一致")
	}
}

func TestWireCodecRoundtrip_流式(t *testing.T) {
	chunk := &schema.AgentResponseChunk{
		RequestID: "req-1",
		ChannelID: "web",
		Payload:   map[string]any{"event_type": "chat.delta", "content": "hello"},
	}
	wire := EncodeAgentChunkForWire(chunk, "resp-1", 1, true)
	back, err := ParseAgentServerWireChunk(wire)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if back.RequestID != chunk.RequestID {
		t.Error("往返不一致")
	}
	if back.Payload["content"] != "hello" {
		t.Error("Payload 往返不一致")
	}
}
```

- [ ] **Step 2: 实现 wire_codec.go**

创建 `internal/swarm/e2a/wire_codec.go`，包含：

按声明顺序：
- **结构体**：无
- **枚举**：无
- **常量**：`logComponent = logger.ComponentChannel`
- **全局变量**：无
- **导出函数**：
  - `IsE2AResponseWireDict`
  - `ParseAgentServerWireUnary`
  - `ParseAgentServerWireChunk`
  - `EncodeAgentResponseForWire`
  - `EncodeAgentChunkForWire`
  - `EncodeJSONParseErrorWire`
- **非导出函数**：
  - `rawDictToAgentResponse`
  - `rawDictToAgentChunk`
  - `fallbackWireUnaryFromLegacy`
  - `fallbackWireChunkFromLegacy`

- [ ] **Step 3: 运行 wire_codec 测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/e2a/... -run "TestIsE2A|TestEncode|TestParse|TestWireCodecRoundtrip"
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/e2a/wire_codec.go internal/swarm/e2a/wire_codec_test.go
git commit -m "feat(e2a): 实现 Wire Codec 编解码 + fallback 兜底"
```

---

### Task 7: 实现 agent_compat.go

**Files:**
- Create: `internal/swarm/e2a/agent_compat.go`
- Create: `internal/swarm/e2a/agent_compat_test.go`

- [ ] **Step 1: 编写 agent_compat 测试**

```go
package e2a

import (
	"testing"

	"uapclaw-gateway/internal/swarm/schema"
)

func TestE2AToAgentRequest(t *testing.T) {
	env := &E2AEnvelope{
		RequestID:  "req-1",
		Channel:    "web",
		SessionID:  "sess-1",
		Method:     "chat.send",
		Params:     map[string]any{"query": "hello"},
		IsStream:   false,
		Timestamp:  "2025-01-01T00:00:00Z",
	}
	req, err := E2AToAgentRequest(env)
	if err != nil {
		t.Fatalf("E2AToAgentRequest 失败: %v", err)
	}
	if req.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 \"req-1\"", req.RequestID)
	}
	if req.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", req.ChannelID)
	}
	if req.ReqMethod != schema.ReqMethodChatSend {
		t.Errorf("ReqMethod = %q, 期望 \"chat.send\"", req.ReqMethod)
	}
	if req.IsStream {
		t.Error("IsStream 应为 false")
	}
}

func TestE2AToAgentRequest_未知方法(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-1",
		Channel:   "web",
		Method:    "unknown.method",
	}
	_, err := E2AToAgentRequest(env)
	if err == nil {
		t.Error("未知方法应返回 error")
	}
}

func TestE2AToAgentRequest_FallbackEnvelope(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-1",
		Channel:   "web",
		ChannelContext: map[string]any{
			"_jiuwenswarm": map[string]any{"normalize_failed": true},
		},
	}
	_, err := E2AToAgentRequest(env)
	if err == nil {
		t.Error("fallback envelope 应返回 error")
	}
}

func TestE2ATimestampToFloat(t *testing.T) {
	ts := e2aTimestampToFloat("2025-01-01T00:00:00Z")
	if ts <= 0 {
		t.Errorf("有效时间戳应返回正数，实际 %v", ts)
	}

	tsEmpty := e2aTimestampToFloat("")
	if tsEmpty != 0 {
		t.Errorf("空串应返回 0，实际 %v", tsEmpty)
	}

	tsInvalid := e2aTimestampToFloat("not-a-date")
	if tsInvalid != 0 {
		t.Errorf("无效格式应返回 0，实际 %v", tsInvalid)
	}
}
```

- [ ] **Step 2: 实现 agent_compat.go**

创建 `internal/swarm/e2a/agent_compat.go`，包含：

按声明顺序：
- **常量**：`logComponent = logger.ComponentChannel`
- **导出函数**：`E2AToAgentRequest`
- **非导出函数**：`e2aTimestampToFloat`

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/e2a/... -run "TestE2ATo|TestE2ATimestamp"
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/e2a/agent_compat.go internal/swarm/e2a/agent_compat_test.go
git commit -m "feat(e2a): 实现 agent_compat E2AEnvelope→AgentRequest 转换"
```

---

### Task 8: 更新 doc.go + IMPLEMENTATION_PLAN.md 回填 + 全量测试

**Files:**
- Modify: `internal/swarm/e2a/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

在 `internal/swarm/e2a/doc.go` 中更新文件目录：
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

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 10.2.5、10.2.7、10.2.8 的状态从 `☐` 改为 `✅`：

查找并替换：
- `| 10.2.5 | ☐` → `| 10.2.5 | ✅`
- `| 10.2.7 | ☐` → `| 10.2.7 | ✅`
- `| 10.2.8 | ☐` → `| 10.2.8 | ✅`

- [ ] **Step 3: 运行 e2a 包全量测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/e2a/...
```

预期：全部 PASS

- [ ] **Step 4: 运行 schema 包全量测试（回归确认）**

```bash
cd /home/opensource/uapclaw-gateway && go test -v ./internal/swarm/schema/...
```

预期：全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/e2a/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(e2a): 更新 doc.go 文件目录 + IMPLEMENTATION_PLAN 回填 10.2.5/10.2.7/10.2.8 ✅"
```
