# Message 模型实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 10.1.4 Message 模型的完整 Go 代码，包括 MessageType 枚举、Message 结构体、Validate 校验、工厂函数（NewReqMessage/NewResMessage/NewEventMessage + MessageOption）、辅助函数（NewMessageID/NowTimestamp），与 Python `jiuwenswarm/common/schema/message.py` (Message) 一一对应。

**Architecture:** MessageType 枚举复用 ReqMethod/EventType/Mode 的 string 枚举模式（3 个常量 + lookup map + Parse/IsValid/String/GoString）。Message 结构体对齐 Python dataclass 字段布局，JSON tag 与 Python 字段名一致。工厂函数采用 MessageOption 函数选项模式。Validate 仅校验必须提供的 5 条规则。测试覆盖完整 JSON 往返 + Validate + 工厂函数 + 辅助函数。

**Tech Stack:** Go 1.24+, standard library (encoding/json, fmt, math/rand, time, testing), github.com/google/uuid

**设计文档:** `docs/superpowers/specs/2025-07-30-message-model-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/swarm/schema/message.go` | MessageType 枚举 + Message 结构体 + Validate + 工厂函数 + 辅助函数 |
| 创建 | `internal/swarm/schema/message_test.go` | 完整 JSON 往返 + Validate + 工厂函数 + 辅助函数测试 |
| 修改 | `internal/swarm/schema/doc.go` | 文件目录增加 message.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 步骤 10.1.4 状态 ☐→✅ |

---

### Task 1: 创建 message.go — MessageType 枚举 + Message 结构体

**Files:**
- Create: `internal/swarm/schema/message.go`

- [ ] **Step 1: 创建 message.go 完整文件**

```go
package schema

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Message 统一内部消息结构。
//
// 作为 Gateway/Channel 层的流通货币，承载请求、响应、事件三种方向的消息。
// 字段布局对齐 Python Message dataclass，JSON tag 与 Python 字段名一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (Message)
type Message struct {
	// ─── 必填字段 ───

	// ID 消息唯一标识（UUID v4，32 hex 无连字符）
	ID string `json:"id"`
	// Type 消息方向：req/res/event
	Type MessageType `json:"type"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// SessionID 会话标识（req 中总是非空，res/event 中可能为空）
	SessionID string `json:"session_id"`
	// Params 请求参数（req 用 json.RawMessage 延迟解析，res/event 为空）
	Params json.RawMessage `json:"params"`
	// Timestamp Unix 秒时间戳（含小数精度，对齐 Python time.time()）
	Timestamp float64 `json:"timestamp"`
	// OK 是否成功
	OK bool `json:"ok"`

	// ─── 可选字段（字符串/bool 不加 omitempty，保留零值输出） ───

	// Provider IM 平台名称（仅 IM 渠道 req 使用）
	Provider string `json:"provider"`
	// ChatID IM 聊天标识
	ChatID string `json:"chat_id"`
	// UserID 发送者标识
	UserID string `json:"user_id"`
	// BotID 机器人标识
	BotID string `json:"bot_id"`
	// ReqMethod 请求方法（仅 req 使用）
	ReqMethod ReqMethod `json:"req_method"`
	// EventType 事件类型（仅 event/res 使用）
	EventType EventType `json:"event_type"`
	// Mode 运行模式（仅 req 使用）
	Mode Mode `json:"mode"`
	// IsStream 是否流式请求（仅 req 使用）
	IsStream bool `json:"is_stream"`
	// StreamSeq 流式序号（0 表示未设置，实际序号从 1 开始）
	StreamSeq int `json:"stream_seq"`
	// StreamID 流式标识（空串表示未设置）
	StreamID string `json:"stream_id"`
	// GroupDigitalAvatar 数字分身群聊模式
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// EnableMemory 是否启用记忆
	EnableMemory bool `json:"enable_memory"`
	// EnableStreaming 是否启用流式输出
	// 注意：Python 默认 True，Go bool 零值 False；工厂函数显式设 True 对齐 Python
	EnableStreaming bool `json:"enable_streaming"`

	// ─── 可选字段（指针/切片/map 加 omitempty） ───

	// Payload 响应/事件负载（res/event 用，req 为 nil）
	Payload json.RawMessage `json:"payload,omitempty"`
	// Metadata 扩展元数据（Gateway 需要主动读写内部字段）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// MessageType 消息方向类型。
//
// 定义 Message 的方向语义：req（请求）、res（响应）、event（事件）。
// 与 Python Literal["req", "res", "event"] 一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (Message.type)
type MessageType string

const (
	// MessageTypeReq 请求消息
	MessageTypeReq MessageType = "req"
	// MessageTypeRes 响应消息
	MessageTypeRes MessageType = "res"
	// MessageTypeEvent 事件消息
	MessageTypeEvent MessageType = "event"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// messageTypeLookup 字符串值到 MessageType 枚举的查找表，用于 ParseMessageType/IsValidMessageType 的 O(1) 查找。
var messageTypeLookup map[string]MessageType

// ──────────────────────────── 导出函数 ────────────────────────────

// AllMessageTypes 返回所有 MessageType 枚举值。
func AllMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeReq,
		MessageTypeRes,
		MessageTypeEvent,
	}
}

// ParseMessageType 从字符串解析 MessageType，不合法返回错误。
func ParseMessageType(s string) (MessageType, error) {
	if mt, ok := messageTypeLookup[s]; ok {
		return mt, nil
	}
	return MessageType(""), fmt.Errorf("不合法的 MessageType 值: %q", s)
}

// IsValidMessageType 判断字符串是否为合法的 MessageType 值。
func IsValidMessageType(s string) bool {
	_, ok := messageTypeLookup[s]
	return ok
}

// Validate 校验 Message 的必须提供字段。
//
// 校验规则（对齐 Python 实际使用）：
//  1. id 非空
//  2. type 合法（req/res/event）
//  3. channel_id 非空
//  4. type=req 时 req_method 非零值
//  5. type=event 时 event_type 非零值
//
// 互斥约束（如 req 不应有 event_type）由工厂函数保证，不在 Validate 中强制校验。
func (m *Message) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("消息 id 不能为空")
	}
	if !IsValidMessageType(string(m.Type)) {
		return fmt.Errorf("不合法的 MessageType 值: %q", m.Type)
	}
	if m.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	if m.Type == MessageTypeReq && m.ReqMethod == "" {
		return fmt.Errorf("请求消息 req_method 不能为空")
	}
	if m.Type == MessageTypeEvent && m.EventType == "" {
		return fmt.Errorf("事件消息 event_type 不能为空")
	}
	return nil
}

// NewMessageID 生成 UUID v4，32 hex 无连字符，对齐 Python uuid4().hex。
func NewMessageID() string {
	return uuid.New().String()
}

// NowTimestamp 返回当前时间的 Unix 秒浮点数，对齐 Python time.time()。
func NowTimestamp() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// NewReqMessage 构造请求消息。
//
// 自动生成 UUID id 和 float64 timestamp，设置 type=MessageTypeReq, ok=true, enable_streaming=true。
// 工厂函数保证：event_type=零值, payload=nil, mode=零值, is_stream=false。
func NewReqMessage(channelID, sessionID string, reqMethod ReqMethod, params json.RawMessage, opts ...MessageOption) *Message {
	msg := &Message{
		ID:              NewMessageID(),
		Type:            MessageTypeReq,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Params:          params,
		Timestamp:       NowTimestamp(),
		OK:              true,
		ReqMethod:       reqMethod,
		EnableStreaming:  true,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// NewResMessage 构造响应消息。
//
// 自动生成 UUID id 和 float64 timestamp，设置 type=MessageTypeRes, enable_streaming=true。
// 工厂函数保证：req_method=零值, mode=零值, is_stream=false, params=nil。
func NewResMessage(channelID, sessionID string, ok bool, payload json.RawMessage, opts ...MessageOption) *Message {
	msg := &Message{
		ID:              NewMessageID(),
		Type:            MessageTypeRes,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Timestamp:       NowTimestamp(),
		OK:              ok,
		Payload:         payload,
		EnableStreaming:  true,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// NewEventMessage 构造事件消息。
//
// 自动生成 UUID id 和 float64 timestamp，设置 type=MessageTypeEvent, ok=true, enable_streaming=true。
// 工厂函数保证：req_method=零值, mode=零值, is_stream=false, params=nil。
func NewEventMessage(channelID, sessionID string, eventType EventType, payload json.RawMessage, opts ...MessageOption) *Message {
	msg := &Message{
		ID:              NewMessageID(),
		Type:            MessageTypeEvent,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Timestamp:       NowTimestamp(),
		OK:              true,
		EventType:       eventType,
		Payload:         payload,
		EnableStreaming:  true,
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建 MessageType 查找表
	mts := AllMessageTypes()
	messageTypeLookup = make(map[string]MessageType, len(mts))
	for _, mt := range mts {
		messageTypeLookup[string(mt)] = mt
	}
}
```

- [ ] **Step 2: 验证文件编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译成功，无错误

---

### Task 2: 创建 message.go — MessageType 方法 + MessageOption 函数

**Files:**
- Modify: `internal/swarm/schema/message.go`

- [ ] **Step 1: 追加 MessageType 的 String/GoString 方法和 MessageOption 函数**

在 message.go 的 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块中，在 `NowTimestamp` 函数之后追加以下代码：

```go
// MessageOption 消息可选配置函数。
type MessageOption func(*Message)

// WithMode 设置运行模式。
func WithMode(m Mode) MessageOption {
	return func(msg *Message) { msg.Mode = m }
}

// WithIsStream 设置是否流式。
func WithIsStream(v bool) MessageOption {
	return func(msg *Message) { msg.IsStream = v }
}

// WithProvider 设置 IM 平台名称。
func WithProvider(p string) MessageOption {
	return func(msg *Message) { msg.Provider = p }
}

// WithChatID 设置 IM 聊天标识。
func WithChatID(id string) MessageOption {
	return func(msg *Message) { msg.ChatID = id }
}

// WithUserID 设置发送者标识。
func WithUserID(id string) MessageOption {
	return func(msg *Message) { msg.UserID = id }
}

// WithBotID 设置机器人标识。
func WithBotID(id string) MessageOption {
	return func(msg *Message) { msg.BotID = id }
}

// WithMetadata 设置扩展元数据。
func WithMetadata(m map[string]any) MessageOption {
	return func(msg *Message) { msg.Metadata = m }
}

// WithGroupDigitalAvatar 设置数字分身群聊模式。
func WithGroupDigitalAvatar(v bool) MessageOption {
	return func(msg *Message) { msg.GroupDigitalAvatar = v }
}

// WithEnableMemory 设置是否启用记忆。
func WithEnableMemory(v bool) MessageOption {
	return func(msg *Message) { msg.EnableMemory = v }
}

// WithEnableStreaming 设置是否启用流式输出。
func WithEnableStreaming(v bool) MessageOption {
	return func(msg *Message) { msg.EnableStreaming = v }
}

// WithSessionID 设置会话标识。
func WithSessionID(id string) MessageOption {
	return func(msg *Message) { msg.SessionID = id }
}

// WithStreamSeq 设置流式序号。
func WithStreamSeq(seq int) MessageOption {
	return func(msg *Message) { msg.StreamSeq = seq }
}

// WithStreamID 设置流式标识。
func WithStreamID(id string) MessageOption {
	return func(msg *Message) { msg.StreamID = id }
}

// WithEventType 设置事件类型（仅 res 使用，如 CHAT_FINAL）。
func WithEventType(et EventType) MessageOption {
	return func(msg *Message) { msg.EventType = et }
}

// String 实现 fmt.Stringer 接口。
func (mt MessageType) String() string {
	return string(mt)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (mt MessageType) GoString() string {
	return fmt.Sprintf("schema.MessageType(%q)", string(mt))
}
```

- [ ] **Step 2: 验证文件编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译成功，无错误

---

### Task 3: 创建 message_test.go — MessageType 枚举测试

**Files:**
- Create: `internal/swarm/schema/message_test.go`

- [ ] **Step 1: 创建测试文件 — MessageType 枚举部分**

```go
package schema

import (
	"testing"
)

// ──────────────────────────── MessageType 枚举测试 ────────────────────────────

// TestAllMessageTypes 验证返回全部 3 个 MessageType 枚举值
func TestAllMessageTypes(t *testing.T) {
	all := AllMessageTypes()
	if len(all) != 3 {
		t.Fatalf("期望 3 个 MessageType，实际 %d", len(all))
	}
	expected := map[MessageType]bool{
		MessageTypeReq:   true,
		MessageTypeRes:   true,
		MessageTypeEvent: true,
	}
	for _, mt := range all {
		if !expected[mt] {
			t.Errorf("意外的 MessageType 值: %q", mt)
		}
	}
}

// TestParseMessageType_合法值 验证合法字符串解析
func TestParseMessageType_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  MessageType
	}{
		{"req", MessageTypeReq},
		{"res", MessageTypeRes},
		{"event", MessageTypeEvent},
	}
	for _, tt := range tests {
		got, err := ParseMessageType(tt.input)
		if err != nil {
			t.Errorf("ParseMessageType(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMessageType(%q) = %q, 期望 %q", tt.input, got, tt.want)
		}
	}
}

// TestParseMessageType_非法值 验证非法字符串返回错误
func TestParseMessageType_非法值(t *testing.T) {
	_, err := ParseMessageType("invalid")
	if err == nil {
		t.Error("ParseMessageType(\"invalid\") 期望返回错误，实际返回 nil")
	}
	_, err = ParseMessageType("")
	if err == nil {
		t.Error("ParseMessageType(\"\") 期望返回错误，实际返回 nil")
	}
}

// TestIsValidMessageType 验证 IsValidMessageType 判断
func TestIsValidMessageType(t *testing.T) {
	if !IsValidMessageType("req") {
		t.Error("IsValidMessageType(\"req\") 期望 true")
	}
	if !IsValidMessageType("res") {
		t.Error("IsValidMessageType(\"res\") 期望 true")
	}
	if !IsValidMessageType("event") {
		t.Error("IsValidMessageType(\"event\") 期望 true")
	}
	if IsValidMessageType("invalid") {
		t.Error("IsValidMessageType(\"invalid\") 期望 false")
	}
}

// TestMessageType_String 验证 String 方法
func TestMessageType_String(t *testing.T) {
	if MessageTypeReq.String() != "req" {
		t.Errorf("MessageTypeReq.String() = %q, 期望 \"req\"", MessageTypeReq.String())
	}
}

// TestMessageType_GoString 验证 GoString 方法
func TestMessageType_GoString(t *testing.T) {
	got := MessageTypeReq.GoString()
	want := `schema.MessageType("req")`
	if got != want {
		t.Errorf("MessageTypeReq.GoString() = %q, 期望 %q", got, want)
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -run TestAllMessageTypes|TestParseMessageType|TestIsValidMessageType|TestMessageType_String|TestMessageType_GoString ./internal/swarm/schema/ -v`
Expected: 全部 PASS

---

### Task 4: 追加 message_test.go — Validate 测试

**Files:**
- Modify: `internal/swarm/schema/message_test.go`

- [ ] **Step 1: 追加 Validate 合法/非法测试**

在 message_test.go 末尾追加：

```go
// ──────────────────────────── Validate 测试 ────────────────────────────

// TestValidate_请求消息合法 验证合法 req 消息通过校验
func TestValidate_请求消息合法(t *testing.T) {
	msg := &Message{
		ID:         "test-id",
		Type:       MessageTypeReq,
		ChannelID:  "web",
		SessionID:  "sess-1",
		Params:     json.RawMessage(`{"query":"hello"}`),
		Timestamp:  1712345678.123,
		OK:         true,
		ReqMethod:  ReqMethodChatSend,
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("合法 req 消息 Validate 返回错误: %v", err)
	}
}

// TestValidate_响应消息合法 验证合法 res 消息通过校验
func TestValidate_响应消息合法(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeRes,
		ChannelID: "web",
		OK:        true,
		Payload:   json.RawMessage(`{"content":"ok"}`),
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("合法 res 消息 Validate 返回错误: %v", err)
	}
}

// TestValidate_事件消息合法 验证合法 event 消息通过校验
func TestValidate_事件消息合法(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeEvent,
		ChannelID: "web",
		OK:        true,
		EventType: EventTypeChatDelta,
		Payload:   json.RawMessage(`{"content":"delta"}`),
	}
	if err := msg.Validate(); err != nil {
		t.Errorf("合法 event 消息 Validate 返回错误: %v", err)
	}
}

// TestValidate_ID为空 验证 ID 为空返回错误
func TestValidate_ID为空(t *testing.T) {
	msg := &Message{Type: MessageTypeReq, ChannelID: "web", ReqMethod: ReqMethodChatSend}
	if err := msg.Validate(); err == nil {
		t.Error("ID 为空时期望返回错误")
	}
}

// TestValidate_Type非法 验证非法 Type 返回错误
func TestValidate_Type非法(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageType("invalid"), ChannelID: "web"}
	if err := msg.Validate(); err == nil {
		t.Error("非法 Type 时期望返回错误")
	}
}

// TestValidate_ChannelID为空 验证 ChannelID 为空返回错误
func TestValidate_ChannelID为空(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageTypeReq, ReqMethod: ReqMethodChatSend}
	if err := msg.Validate(); err == nil {
		t.Error("ChannelID 为空时期望返回错误")
	}
}

// TestValidate_请求消息缺ReqMethod 验证 req 消息缺少 req_method 返回错误
func TestValidate_请求消息缺ReqMethod(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageTypeReq, ChannelID: "web"}
	if err := msg.Validate(); err == nil {
		t.Error("req 消息缺少 req_method 时期望返回错误")
	}
}

// TestValidate_事件消息缺EventType 验证 event 消息缺少 event_type 返回错误
func TestValidate_事件消息缺EventType(t *testing.T) {
	msg := &Message{ID: "test-id", Type: MessageTypeEvent, ChannelID: "web"}
	if err := msg.Validate(); err == nil {
		t.Error("event 消息缺少 event_type 时期望返回错误")
	}
}
```

需要在文件头部 import 中添加 `"encoding/json"`。

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -run TestValidate ./internal/swarm/schema/ -v`
Expected: 全部 PASS

---

### Task 5: 追加 message_test.go — 工厂函数测试

**Files:**
- Modify: `internal/swarm/schema/message_test.go`

- [ ] **Step 1: 追加工厂函数测试**

在 message_test.go 末尾追加：

```go
// ──────────────────────────── 工厂函数测试 ────────────────────────────

// TestNewReqMessage 验证 NewReqMessage 构造正确
func TestNewReqMessage(t *testing.T) {
	params := json.RawMessage(`{"query":"hello"}`)
	msg := NewReqMessage("web", "sess-1", ReqMethodChatSend, params)

	if msg.ID == "" {
		t.Error("ID 不应为空")
	}
	if len(msg.ID) != 36 {
		t.Errorf("ID 长度应为 36（UUID 带连字符），实际 %d", len(msg.ID))
	}
	if msg.Type != MessageTypeReq {
		t.Errorf("Type = %q, 期望 \"req\"", msg.Type)
	}
	if msg.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", msg.ChannelID)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, 期望 \"sess-1\"", msg.SessionID)
	}
	if string(msg.Params) != `{"query":"hello"}` {
		t.Errorf("Params = %s, 期望 {\"query\":\"hello\"}", string(msg.Params))
	}
	if msg.Timestamp <= 0 {
		t.Error("Timestamp 应为正数")
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if msg.ReqMethod != ReqMethodChatSend {
		t.Errorf("ReqMethod = %q, 期望 %q", msg.ReqMethod, ReqMethodChatSend)
	}
	if msg.EventType != "" {
		t.Errorf("EventType 应为零值，实际 %q", msg.EventType)
	}
	if msg.Payload != nil {
		t.Error("Payload 应为 nil")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（对齐 Python 默认值）")
	}
}

// TestNewReqMessage_WithOptions 验证 NewReqMessage 使用 Option
func TestNewReqMessage_WithOptions(t *testing.T) {
	params := json.RawMessage(`{}`)
	msg := NewReqMessage("web", "sess-1", ReqMethodChatSend, params,
		WithMode(ModeCodeNormal),
		WithIsStream(true),
		WithProvider("feishu"),
		WithChatID("chat-1"),
		WithUserID("user-1"),
		WithBotID("bot-1"),
		WithGroupDigitalAvatar(true),
		WithEnableMemory(true),
		WithEnableStreaming(false),
		WithMetadata(map[string]any{"key": "val"}),
	)

	if msg.Mode != ModeCodeNormal {
		t.Errorf("Mode = %q, 期望 %q", msg.Mode, ModeCodeNormal)
	}
	if !msg.IsStream {
		t.Error("IsStream 应为 true")
	}
	if msg.Provider != "feishu" {
		t.Errorf("Provider = %q, 期望 \"feishu\"", msg.Provider)
	}
	if msg.ChatID != "chat-1" {
		t.Errorf("ChatID = %q, 期望 \"chat-1\"", msg.ChatID)
	}
	if msg.UserID != "user-1" {
		t.Errorf("UserID = %q, 期望 \"user-1\"", msg.UserID)
	}
	if msg.BotID != "bot-1" {
		t.Errorf("BotID = %q, 期望 \"bot-1\"", msg.BotID)
	}
	if !msg.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if !msg.EnableMemory {
		t.Error("EnableMemory 应为 true")
	}
	if msg.EnableStreaming {
		t.Error("EnableStreaming 应为 false（被 Option 覆盖）")
	}
	if msg.Metadata["key"] != "val" {
		t.Error("Metadata[\"key\"] 期望 \"val\"")
	}
}

// TestNewResMessage 验证 NewResMessage 构造正确
func TestNewResMessage(t *testing.T) {
	payload := json.RawMessage(`{"content":"response"}`)
	msg := NewResMessage("web", "sess-1", true, payload)

	if msg.Type != MessageTypeRes {
		t.Errorf("Type = %q, 期望 \"res\"", msg.Type)
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if string(msg.Payload) != `{"content":"response"}` {
		t.Errorf("Payload = %s, 期望 {\"content\":\"response\"}", string(msg.Payload))
	}
	if msg.ReqMethod != "" {
		t.Errorf("ReqMethod 应为零值，实际 %q", msg.ReqMethod)
	}
	if msg.Params != nil {
		t.Error("Params 应为 nil")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（对齐 Python 默认值）")
	}
}

// TestNewResMessage_WithOptions 验证 NewResMessage 使用 Option 设置 EventType
func TestNewResMessage_WithOptions(t *testing.T) {
	payload := json.RawMessage(`{}`)
	msg := NewResMessage("web", "sess-1", true, payload,
		WithEventType(EventTypeChatFinal),
	)

	if msg.EventType != EventTypeChatFinal {
		t.Errorf("EventType = %q, 期望 %q", msg.EventType, EventTypeChatFinal)
	}
}

// TestNewEventMessage 验证 NewEventMessage 构造正确
func TestNewEventMessage(t *testing.T) {
	payload := json.RawMessage(`{"content":"delta"}`)
	msg := NewEventMessage("web", "sess-1", EventTypeChatDelta, payload)

	if msg.Type != MessageTypeEvent {
		t.Errorf("Type = %q, 期望 \"event\"", msg.Type)
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if msg.EventType != EventTypeChatDelta {
		t.Errorf("EventType = %q, 期望 %q", msg.EventType, EventTypeChatDelta)
	}
	if string(msg.Payload) != `{"content":"delta"}` {
		t.Errorf("Payload = %s, 期望 {\"content\":\"delta\"}", string(msg.Payload))
	}
	if msg.ReqMethod != "" {
		t.Errorf("ReqMethod 应为零值，实际 %q", msg.ReqMethod)
	}
	if msg.Params != nil {
		t.Error("Params 应为 nil")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（对齐 Python 默认值）")
	}
}

// TestNewEventMessage_WithOptions 验证 NewEventMessage 使用 Option 覆盖 EnableStreaming
func TestNewEventMessage_WithOptions(t *testing.T) {
	payload := json.RawMessage(`{}`)
	msg := NewEventMessage("web", "sess-1", EventTypeChatToolResult, payload,
		WithEnableStreaming(false),
	)

	if msg.EnableStreaming {
		t.Error("EnableStreaming 应为 false（被 Option 覆盖，对齐 cron 场景）")
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -run TestNew ./internal/swarm/schema/ -v`
Expected: 全部 PASS

---

### Task 6: 追加 message_test.go — JSON 往返测试

**Files:**
- Modify: `internal/swarm/schema/message_test.go`

- [ ] **Step 1: 追加 JSON 往返测试**

在 message_test.go 末尾追加：

```go
// ──────────────────────────── JSON 往返测试 ────────────────────────────

// TestMessageJSONRoundtrip_请求消息 验证 req 消息 JSON 序列化往返
func TestMessageJSONRoundtrip_请求消息(t *testing.T) {
	original := &Message{
		ID:               "test-req-id",
		Type:             MessageTypeReq,
		ChannelID:        "web",
		SessionID:        "sess-1",
		Params:           json.RawMessage(`{"query":"hello","mode":"agent.plan"}`),
		Timestamp:        1712345678.123,
		OK:               true,
		Provider:         "feishu",
		ChatID:           "chat-1",
		UserID:           "user-1",
		BotID:            "bot-1",
		ReqMethod:        ReqMethodChatSend,
		Mode:             ModeAgentPlan,
		IsStream:         true,
		StreamSeq:        3,
		StreamID:         "stream-1",
		GroupDigitalAvatar: true,
		EnableMemory:      true,
		EnableStreaming:    true,
		Metadata:          map[string]any{"method": "chat.send", "cwd": "/tmp"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	// 逐字段比对
	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID: got %q, want %q", decoded.SessionID, original.SessionID)
	}
	if string(decoded.Params) != string(original.Params) {
		t.Errorf("Params: got %s, want %s", string(decoded.Params), string(original.Params))
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.OK != original.OK {
		t.Errorf("OK: got %v, want %v", decoded.OK, original.OK)
	}
	if decoded.Provider != original.Provider {
		t.Errorf("Provider: got %q, want %q", decoded.Provider, original.Provider)
	}
	if decoded.ChatID != original.ChatID {
		t.Errorf("ChatID: got %q, want %q", decoded.ChatID, original.ChatID)
	}
	if decoded.UserID != original.UserID {
		t.Errorf("UserID: got %q, want %q", decoded.UserID, original.UserID)
	}
	if decoded.BotID != original.BotID {
		t.Errorf("BotID: got %q, want %q", decoded.BotID, original.BotID)
	}
	if decoded.ReqMethod != original.ReqMethod {
		t.Errorf("ReqMethod: got %q, want %q", decoded.ReqMethod, original.ReqMethod)
	}
	if decoded.Mode != original.Mode {
		t.Errorf("Mode: got %q, want %q", decoded.Mode, original.Mode)
	}
	if decoded.IsStream != original.IsStream {
		t.Errorf("IsStream: got %v, want %v", decoded.IsStream, original.IsStream)
	}
	if decoded.StreamSeq != original.StreamSeq {
		t.Errorf("StreamSeq: got %d, want %d", decoded.StreamSeq, original.StreamSeq)
	}
	if decoded.StreamID != original.StreamID {
		t.Errorf("StreamID: got %q, want %q", decoded.StreamID, original.StreamID)
	}
	if decoded.GroupDigitalAvatar != original.GroupDigitalAvatar {
		t.Errorf("GroupDigitalAvatar: got %v, want %v", decoded.GroupDigitalAvatar, original.GroupDigitalAvatar)
	}
	if decoded.EnableMemory != original.EnableMemory {
		t.Errorf("EnableMemory: got %v, want %v", decoded.EnableMemory, original.EnableMemory)
	}
	if decoded.EnableStreaming != original.EnableStreaming {
		t.Errorf("EnableStreaming: got %v, want %v", decoded.EnableStreaming, original.EnableStreaming)
	}
	if decoded.Metadata["method"] != "chat.send" {
		t.Errorf("Metadata[\"method\"]: got %v, want \"chat.send\"", decoded.Metadata["method"])
	}
}

// TestMessageJSONRoundtrip_响应消息 验证 res 消息 JSON 序列化往返
func TestMessageJSONRoundtrip_响应消息(t *testing.T) {
	original := &Message{
		ID:         "test-res-id",
		Type:       MessageTypeRes,
		ChannelID:  "web",
		SessionID:  "sess-1",
		Timestamp:  1712345678.456,
		OK:         true,
		Payload:    json.RawMessage(`{"content":"final answer","is_complete":true}`),
		EventType:  EventTypeChatFinal,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.Type != MessageTypeRes {
		t.Errorf("Type: got %q, want \"res\"", decoded.Type)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("Payload: got %s, want %s", string(decoded.Payload), string(original.Payload))
	}
	if decoded.EventType != EventTypeChatFinal {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, EventTypeChatFinal)
	}
}

// TestMessageJSONRoundtrip_事件消息 验证 event 消息 JSON 序列化往返
func TestMessageJSONRoundtrip_事件消息(t *testing.T) {
	original := &Message{
		ID:         "test-event-id",
		Type:       MessageTypeEvent,
		ChannelID:  "web",
		SessionID:  "sess-1",
		Timestamp:  1712345678.789,
		OK:         true,
		Payload:    json.RawMessage(`{"content":"delta text"}`),
		EventType:  EventTypeChatDelta,
		GroupDigitalAvatar: true,
		EnableMemory:       true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.Type != MessageTypeEvent {
		t.Errorf("Type: got %q, want \"event\"", decoded.Type)
	}
	if decoded.EventType != EventTypeChatDelta {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, EventTypeChatDelta)
	}
	if !decoded.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
}

// TestMessageJSONRoundtrip_最小消息 验证仅必填字段的消息往返
func TestMessageJSONRoundtrip_最小消息(t *testing.T) {
	original := &Message{
		ID:        "min-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		Params:    json.RawMessage(`{}`),
		Timestamp: 1712345678.0,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.ID != "min-id" {
		t.Errorf("ID: got %q, want \"min-id\"", decoded.ID)
	}
	if decoded.ReqMethod != ReqMethodChatSend {
		t.Errorf("ReqMethod: got %q, want %q", decoded.ReqMethod, ReqMethodChatSend)
	}
	// 可选字段应为零值
	if decoded.Provider != "" {
		t.Errorf("Provider 应为空，实际 %q", decoded.Provider)
	}
	if decoded.Payload != nil {
		t.Error("Payload 应为 nil")
	}
	if decoded.Metadata != nil {
		t.Error("Metadata 应为 nil")
	}
}
```

需要在文件头部 import 中确保有 `"encoding/json"`。

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -run TestMessageJSONRoundtrip ./internal/swarm/schema/ -v`
Expected: 全部 PASS

---

### Task 7: 追加 message_test.go — 辅助函数 + omitempty 测试

**Files:**
- Modify: `internal/swarm/schema/message_test.go`

- [ ] **Step 1: 追加辅助函数和 omitempty 测试**

在 message_test.go 末尾追加：

```go
// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestNewMessageID 验证 NewMessageID 生成 UUID 格式
func TestNewMessageID(t *testing.T) {
	id := NewMessageID()
	if id == "" {
		t.Error("NewMessageID 返回空串")
	}
	// UUID v4 标准格式：xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx，36 字符
	if len(id) != 36 {
		t.Errorf("ID 长度应为 36，实际 %d: %q", len(id), id)
	}
	// 两次生成应不同
	id2 := NewMessageID()
	if id == id2 {
		t.Error("两次生成的 ID 不应相同")
	}
}

// TestNowTimestamp 验证 NowTimestamp 返回合理时间戳
func TestNowTimestamp(t *testing.T) {
	ts := NowTimestamp()
	// 2024-01-01 00:00:00 UTC = 1704067200.0
	// 2030-01-01 00:00:00 UTC = 1893456000.0
	if ts < 1.7e9 || ts > 2.0e9 {
		t.Errorf("NowTimestamp = %v，不在合理范围内 [1.7e9, 2.0e9]", ts)
	}
}

// ──────────────────────────── omitempty 验证测试 ────────────────────────────

// TestMessageJSON_omitempty_payload 验证 payload 为 nil 时 JSON 省略
func TestMessageJSON_omitempty_payload(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		Params:    json.RawMessage(`{}`),
		Timestamp: 1712345678.0,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
		// Payload 为 nil
		// Metadata 为 nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	jsonStr := string(data)
	// payload 和 metadata 应被省略
	if contains(jsonStr, "payload") {
		t.Errorf("Payload 为 nil 时 JSON 应省略 payload 字段，实际: %s", jsonStr)
	}
	if contains(jsonStr, "metadata") {
		t.Errorf("Metadata 为 nil 时 JSON 应省略 metadata 字段，实际: %s", jsonStr)
	}
}

// TestMessageJSON_omitempty_payload非nil 验证 payload 非 nil 时 JSON 输出
func TestMessageJSON_omitempty_payload非nil(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeRes,
		ChannelID: "web",
		Timestamp: 1712345678.0,
		OK:        true,
		Payload:   json.RawMessage(`{"content":"ok"}`),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	jsonStr := string(data)
	if !contains(jsonStr, "payload") {
		t.Errorf("Payload 非 nil 时 JSON 应包含 payload 字段，实际: %s", jsonStr)
	}
}

// TestMessageJSON_omitempty_metadata非nil 验证 metadata 非 nil 时 JSON 输出
func TestMessageJSON_omitempty_metadata非nil(t *testing.T) {
	msg := &Message{
		ID:        "test-id",
		Type:      MessageTypeReq,
		ChannelID: "web",
		Params:    json.RawMessage(`{}`),
		Timestamp: 1712345678.0,
		OK:        true,
		ReqMethod: ReqMethodChatSend,
		Metadata:  map[string]any{"key": "val"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	jsonStr := string(data)
	if !contains(jsonStr, "metadata") {
		t.Errorf("Metadata 非 nil 时 JSON 应包含 metadata 字段，实际: %s", jsonStr)
	}
}
```

同时在 message_test.go 中添加辅助函数（在非导出函数区块）：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// contains 检查字符串 s 是否包含子串 sub
func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

// searchString 简单的字符串包含检查
func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 运行全部测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/ -v`
Expected: 全部 PASS

---

### Task 8: 更新 doc.go — 文件目录

**Files:**
- Modify: `internal/swarm/schema/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录，添加 message.go 条目**

将 doc.go 的文件目录部分从：

```
//	schema/
//	├── doc.go           # 包文档
//	├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
//	├── event_type.go    # EventType 枚举（26 个事件类型）
//	└── mode.go          # Mode 枚举（6 个运行模式）
```

更新为：

```
//	schema/
//	├── doc.go           # 包文档
//	├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
//	├── event_type.go    # EventType 枚举（26 个事件类型）
//	├── mode.go          # Mode 枚举（6 个运行模式）
//	└── message.go       # MessageType 枚举 + Message 模型 + 工厂函数 + Validate
```

同时更新包功能概述，在现有描述后追加 MessageType 和 Message 的说明。

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 编译成功

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md — 状态回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将步骤 10.1.4 状态从 ☐ 更新为 ✅**

找到 `| 10.1.4 | ☐ | Message 模型` 行，将 `☐` 替换为 `✅`。

- [ ] **Step 2: 验证修改正确**

Run: `grep "10.1.4" /home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md`
Expected: 输出包含 `✅`

---

### Task 10: 最终验证

- [ ] **Step 1: 运行 schema 包全部测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/schema/`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行项目整体编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功，无错误
