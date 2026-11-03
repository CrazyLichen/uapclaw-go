# 9.83 Pulsar Message Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现分布式 Runner 的 Pulsar 消息队列传输层，并提前落位其两个硬依赖（Dmq 消息类型、消息序列化器）。

**Architecture:** 新建 `internal/agentcore/message_queue` 包承载 `MessageQueuePulsar`（实现既有 `MessageQueueBase` 接口），通过包内窄接口 `pulsarClientAPI`/`pulsarConsumerAPI`/`pulsarProducerAPI` 隔离真实 SDK 以支持 fake 单测；同时新建 `internal/agentcore/runner/drunner/dmessage_queue` 包承载 Dmq 消息类型与 wire 序列化器（`__class__` 标签协议，与 Python 互通）。两个包单向依赖已交付的 `internal/agentcore/runner/message_queue`，无循环。

**Tech Stack:** Go 1.26 · `github.com/apache/pulsar-client-go` v0.21.0 · zerolog（`internal/common/logger`）· `internal/common/exception` 错误码

**Spec:** `docs/superpowers/specs/2026-11-03-pulsar-message-queue-9.83-design.md`

---

## SDK 校核结论（实施前必读）

已下载 `pulsar-client-go@v0.21.0` 并核对真实签名，以下三处与 spec 的初始假设不同，本计划已按真实 API 编写：

| 项 | 真实签名 |
|---|---|
| 建客户端 | `pulsar.NewClient(pulsar.ClientOptions{URL: url}) (pulsar.Client, error)` — **返回 error** |
| 发消息 | `Producer.Send(ctx, *pulsar.ProducerMessage{Payload: []byte, Key: string}) (pulsar.MessageID, error)` — partition key 是 `Key` 字段 |
| 收消息超时 | `Consumer.Receive(ctx) (Message, error)`，ctx 到期时返回 `ctx.Err()`，判定用 `errors.Is(err, context.DeadlineExceeded)` |
| 订阅 | `Client.Subscribe(pulsar.ConsumerOptions{Topic, SubscriptionName, Type: pulsar.KeyShared})` |
| 建 producer | `Client.CreateProducer(pulsar.ProducerOptions{Topic: topic})` |
| 关闭 | `Client.Close()` / `Consumer.Close()` / `Producer.Close()` — 均无返回值 |
| 消息体 | `Message.Payload() []byte` |

**受影响的 spec 细节**：`Start()` 无 error 返回值（接口约束），建客户端失败时只能记 Error 日志并保持 `running=false`，后续 `Subscribe`/`Produce` 会正常报 not-running。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/agentcore/runner/message_queue/base.go`（改） | 给 `QueueMessage` 补 4 个 json tag |
| `internal/agentcore/runner/drunner/dmessage_queue/doc.go`（新） | 包文档 |
| `internal/agentcore/runner/drunner/dmessage_queue/message.go`（新） | `DMessageType`/`ResultType` 枚举 + `DmqRequestMessage`/`DmqResponseMessage` |
| `internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go`（新） | `SerializeMessage`/`DeserializeMessage`/`MessageToMap`/`serializePayload` |
| `internal/agentcore/runner/drunner/dmessage_queue/message_test.go`（新） | 结构体与构造函数测试 |
| `internal/agentcore/runner/drunner/dmessage_queue/message_serializer_test.go`（新） | 序列化往返与边界测试 |
| `internal/agentcore/message_queue/doc.go`（新） | 包文档 |
| `internal/agentcore/message_queue/pulsar_api.go`（新） | 三个窄接口 + SDK 适配器 |
| `internal/agentcore/message_queue/pulsar_api_test.go`（新） | fake 实现 + 适配器冒烟测试 |
| `internal/agentcore/message_queue/pulsar.go`（新） | `MessageQueuePulsar` + `PulsarSubscription` |
| `internal/agentcore/message_queue/pulsar_test.go`（新） | 主体行为测试 |
| `internal/agentcore/message_queue/pulsar_integration_test.go`（新） | `//go:build integration` 真实 broker 测试 |
| `IMPLEMENTATION_PLAN.md`（改） | 9.83 状态与产出列 |

---

## Task 1: 给 `QueueMessage` 补 json tag

**Files:**
- Modify: `internal/agentcore/runner/message_queue/base.go:72-81`
- Test: `internal/agentcore/runner/message_queue/base_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/agentcore/runner/message_queue/base_test.go` 末尾追加（文件已是 `package message_queue`）：

```go
// TestQueueMessage_JSONTag 验证 QueueMessage 序列化使用下划线键名
//
// 对齐 Python QueueMessage 的 message_id/payload/error_code/error_msg 字段名。
func TestQueueMessage_JSONTag(t *testing.T) {
	msg := NewQueueMessage(map[string]any{"k": "v"})
	msg.MessageID = "mid-1"

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	for _, key := range []string{"message_id", "payload", "error_code", "error_msg"} {
		if _, ok := got[key]; !ok {
			t.Errorf("缺少键 %q，实际输出: %s", key, string(data))
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/message_queue/ -run TestQueueMessage_JSONTag -v`
Expected: FAIL — 输出含 `MessageID` 而非 `message_id`，报 `缺少键 "message_id"`

- [ ] **Step 3: 补 json tag**

把 `base.go` 的 `QueueMessage` 定义改为：

```go
// QueueMessage 火忘消息，发布后不等待处理完成。
// 实现 QueueMessageBase 接口。
//
// Python: openjiuwen/core/runner/message_queue_base.py (QueueMessage)
type QueueMessage struct {
	// MessageID 消息唯一标识
	MessageID string `json:"message_id"`
	// Payload 消息载荷
	Payload map[string]any `json:"payload"`
	// ErrorCode 错误码
	ErrorCode int `json:"error_code"`
	// ErrorMsg 错误信息
	ErrorMsg string `json:"error_msg"`
}
```

- [ ] **Step 4: 补 import 并运行测试**

确认 `base_test.go` 顶部 import 块含 `"encoding/json"`（如已存在则跳过）。

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/message_queue/ -v`
Expected: PASS，全部用例绿

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/runner/message_queue/base.go internal/agentcore/runner/message_queue/base_test.go
git commit -m "feat(message_queue): QueueMessage 补 json tag 对齐 Python 字段名

9.83 D-wire：Pulsar 序列化依赖下划线键名，否则 wire 输出 MessageID
导致 Python 端 model_validate 取不到 message_id。"
```

---

## Task 2: 建 `dmessage_queue` 包与 Dmq 消息类型

**Files:**
- Create: `internal/agentcore/runner/drunner/dmessage_queue/doc.go`
- Create: `internal/agentcore/runner/drunner/dmessage_queue/message.go`
- Test: `internal/agentcore/runner/drunner/dmessage_queue/message_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/runner/drunner/dmessage_queue/message_test.go`：

```go
package dmessage_queue

import "testing"

// TestNewDmqRequestMessage_默认值 验证请求消息默认 type 为 INPUT
func TestNewDmqRequestMessage_默认值(t *testing.T) {
	msg := NewDmqRequestMessage(map[string]any{"q": "hi"})

	if msg.Type != DMessageTypeInput {
		t.Errorf("Type 期望 %q，实际 %q", DMessageTypeInput, msg.Type)
	}
	if msg.Payload["q"] != "hi" {
		t.Errorf("Payload 未正确写入，实际 %v", msg.Payload)
	}
	if msg.ErrorCode != 0 {
		t.Errorf("ErrorCode 期望 0，实际 %d", msg.ErrorCode)
	}
}

// TestNewDmqResponseMessage_默认值 验证响应消息默认 type/result_type
func TestNewDmqResponseMessage_默认值(t *testing.T) {
	msg := NewDmqResponseMessage(map[string]any{"a": 1})

	if msg.Type != DMessageTypeOutput {
		t.Errorf("Type 期望 %q，实际 %q", DMessageTypeOutput, msg.Type)
	}
	if msg.ResultType != ResultTypeMessage {
		t.Errorf("ResultType 期望 %q，实际 %q", ResultTypeMessage, msg.ResultType)
	}
}

// TestDmqMessage_实现QueueMessageBase 验证两个消息类型都满足基础接口
func TestDmqMessage_实现QueueMessageBase(t *testing.T) {
	var _ interface {
		GetMessageID() string
		SetMessageID(string)
		GetPayload() map[string]any
		GetErrorCode() int
		SetErrorCode(int)
		GetErrorMsg() string
		SetErrorMsg(string)
	} = NewDmqRequestMessage(nil)

	var _ interface {
		GetMessageID() string
		GetPayload() map[string]any
	} = NewDmqResponseMessage(nil)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -v`
Expected: FAIL — `undefined: NewDmqRequestMessage`（包内无源文件）

- [ ] **Step 3: 写 `doc.go`**

创建 `internal/agentcore/runner/drunner/dmessage_queue/doc.go`：

```go
// Package dmessage_queue 提供分布式消息队列的消息类型与 wire 序列化器。
//
// 本包承载分布式 Runner 跨进程通信的消息载体：请求消息（INPUT/STOP）与
// 响应消息（OUTPUT），以及把消息编解码为 Pulsar 可传输字节的序列化器。
// 序列化格式与 Python 侧一一对应，依赖 __class__ 标签做类型还原，
// 是 Go Runner 与 Python 客户端互通的前提。
//
// 文件目录：
//
//	dmessage_queue/
//	├── doc.go                 # 包文档
//	├── message.go             # DMessageType（分布式消息类型）/ResultType（结果类型） 枚举 + DmqRequestMessage（请求消息）/DmqResponseMessage（响应消息）
//	└── message_serializer.go  # SerializeMessage（序列化）/DeserializeMessage（反序列化）/MessageToMap（消息转 map）/serializePayload（payload 递归）
//
// 对应 Python 代码：
//
//	openjiuwen/core/runner/drunner/dmessage_queue/message.py              （DMessageType/ResultType/DmqRequestMessage/DmqResponseMessage）
//	openjiuwen/core/runner/drunner/dmessage_queue/message_serializer.py   （serialize_message/deserialize_message）
package dmessage_queue
```

- [ ] **Step 4: 写 `message.go`**

创建 `internal/agentcore/runner/drunner/dmessage_queue/message.go`：

```go
package dmessage_queue

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DmqRequestMessage 分布式请求消息。
//
// Python: DmqRequestMessage(DmqMessage)
// 内嵌 QueueMessage 复用 message_id/payload/error_code/error_msg 四个基础字段，
// 与 Python 的 QueueMessage 基类字段一一对应。
type DmqRequestMessage struct {
	// QueueMessage 基础消息字段
	message_queue.QueueMessage
	// Type 消息类型（INPUT / STOP），默认 INPUT
	Type DMessageType
	// ReplyTopic 响应回写 topic
	ReplyTopic string
	// RequestID 请求唯一标识
	RequestID string
	// SenderID 发送方标识
	SenderID string
	// ReceiverID 接收方标识
	ReceiverID string
	// EnableStream 是否启用流式响应
	EnableStream bool
	// ExpireAt 过期时间戳（秒级浮点），可选
	ExpireAt *float64
}

// DmqResponseMessage 分布式响应消息。
//
// Python: DmqResponseMessage(DmqMessage)
type DmqResponseMessage struct {
	// QueueMessage 基础消息字段
	message_queue.QueueMessage
	// Type 消息类型，默认 OUTPUT
	Type DMessageType
	// ResultType 结果类型（MESSAGE / ERROR），默认 MESSAGE
	ResultType ResultType
	// RequestID 请求唯一标识，与请求对应
	RequestID string
	// SenderID 发送方标识
	SenderID string
	// ReceiverID 接收方标识
	ReceiverID string
	// Seq 流式分片序号
	Seq int
	// LastChunk 是否为流式最后一片
	LastChunk bool
	// ExpireAt 过期时间戳（秒级浮点），可选
	ExpireAt *float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// DMessageType 分布式消息类型。
//
// Python: DMessageType(str, Enum)
type DMessageType string

const (
	// DMessageTypeInput 输入消息，发起 agent 调用
	DMessageTypeInput DMessageType = "INPUT"
	// DMessageTypeStop 中断消息
	DMessageTypeStop DMessageType = "STOP"
	// DMessageTypeOutput 输出消息，返回执行结果
	DMessageTypeOutput DMessageType = "OUTPUT"
)

// ResultType 响应结果类型。
//
// Python: ResultType(str, Enum)
type ResultType string

const (
	// ResultTypeMessage 正常消息结果
	ResultTypeMessage ResultType = "MESSAGE"
	// ResultTypeError 错误结果
	ResultTypeError ResultType = "ERROR"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDmqRequestMessage 创建分布式请求消息，Type 默认 INPUT。
//
// Python: DmqRequestMessage(payload=...)
func NewDmqRequestMessage(payload map[string]any) *DmqRequestMessage {
	return &DmqRequestMessage{
		QueueMessage: message_queue.QueueMessage{Payload: payload},
		Type:         DMessageTypeInput,
	}
}

// NewDmqResponseMessage 创建分布式响应消息，Type 默认 OUTPUT、ResultType 默认 MESSAGE。
//
// Python: DmqResponseMessage(payload=...)
func NewDmqResponseMessage(payload map[string]any) *DmqResponseMessage {
	return &DmqResponseMessage{
		QueueMessage: message_queue.QueueMessage{Payload: payload},
		Type:         DMessageTypeOutput,
		ResultType:   ResultTypeMessage,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -v`
Expected: PASS，3 个用例全绿

- [ ] **Step 6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/runner/drunner/dmessage_queue/
git commit -m "feat(dmessage_queue): Dmq 消息类型与枚举

9.83 前置件：DmqRequestMessage/DmqResponseMessage 内嵌 QueueMessage，
DMessageType(INPUT/STOP/OUTPUT) + ResultType(MESSAGE/ERROR)。"
```

---

## Task 3: `serializePayload` 递归与 `MessageToMap` 写出

**Files:**
- Create: `internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go`
- Test: `internal/agentcore/runner/drunner/dmessage_queue/message_serializer_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/runner/drunner/dmessage_queue/message_serializer_test.go`：

```go
package dmessage_queue

import (
	"strings"
	"testing"
	"time"
)

// TestMessageToMap_请求注入class 验证写侧注入 __class__ 与全字段
func TestMessageToMap_请求注入class(t *testing.T) {
	msg := NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-1"
	msg.ReplyTopic = "reply.topic"
	msg.RequestID = "rid-1"
	msg.EnableStream = true

	got, err := MessageToMap(msg)
	if err != nil {
		t.Fatalf("MessageToMap 失败: %v", err)
	}

	if got["__class__"] != "DmqRequestMessage" {
		t.Errorf("__class__ 期望 DmqRequestMessage，实际 %v", got["__class__"])
	}
	if got["message_id"] != "mid-1" {
		t.Errorf("message_id 期望 mid-1，实际 %v", got["message_id"])
	}
	if got["type"] != "INPUT" {
		t.Errorf("type 期望 INPUT，实际 %v", got["type"])
	}
	if got["reply_topic"] != "reply.topic" {
		t.Errorf("reply_topic 期望 reply.topic，实际 %v", got["reply_topic"])
	}
	if got["enable_stream"] != true {
		t.Errorf("enable_stream 期望 true，实际 %v", got["enable_stream"])
	}
}

// TestMessageToMap_响应全字段 验证响应消息字段完整
func TestMessageToMap_响应全字段(t *testing.T) {
	msg := NewDmqResponseMessage(map[string]any{"a": 1.0})
	msg.Seq = 3
	msg.LastChunk = true
	msg.ResultType = ResultTypeError

	got, err := MessageToMap(msg)
	if err != nil {
		t.Fatalf("MessageToMap 失败: %v", err)
	}

	if got["__class__"] != "DmqResponseMessage" {
		t.Errorf("__class__ 错误: %v", got["__class__"])
	}
	if got["seq"] != 3 {
		t.Errorf("seq 期望 3，实际 %v", got["seq"])
	}
	if got["last_chunk"] != true {
		t.Errorf("last_chunk 期望 true，实际 %v", got["last_chunk"])
	}
	if got["result_type"] != "ERROR" {
		t.Errorf("result_type 期望 ERROR，实际 %v", got["result_type"])
	}
}

// TestSerializePayload_datetime 验证 time.Time 编成 __type__ 包装
func TestSerializePayload_datetime(t *testing.T) {
	ts := time.Date(2026, 11, 3, 1, 2, 3, 0, time.UTC)

	got, err := serializePayload(ts, 0)
	if err != nil {
		t.Fatalf("serializePayload 失败: %v", err)
	}

	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("期望 map，实际 %T", got)
	}
	if m["__type__"] != "datetime" {
		t.Errorf("__type__ 期望 datetime，实际 %v", m["__type__"])
	}
	if m["value"] != "2026-11-03T01:02:03Z" {
		t.Errorf("value 期望 RFC3339，实际 %v", m["value"])
	}
}

// TestSerializePayload_嵌套递归 验证 map/list 递归
func TestSerializePayload_嵌套递归(t *testing.T) {
	in := map[string]any{
		"outer": []any{
			map[string]any{"inner": "v"},
		},
	}

	got, err := serializePayload(in, 0)
	if err != nil {
		t.Fatalf("serializePayload 失败: %v", err)
	}

	m := got.(map[string]any)
	list := m["outer"].([]any)
	inner := list[0].(map[string]any)
	if inner["inner"] != "v" {
		t.Errorf("嵌套值丢失，实际 %v", inner)
	}
}

// TestSerializePayload_深度超限报错 验证超过 10 层报错
func TestSerializePayload_深度超限报错(t *testing.T) {
	_, err := serializePayload("v", MaxRecurseDepth+1)
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	if !strings.Contains(err.Error(), "nested too deep") {
		t.Errorf("错误信息不符: %v", err)
	}
}

// TestMessageToMap_非Dmq类型报错 验证 D6：fail fast
func TestMessageToMap_非Dmq类型报错(t *testing.T) {
	_, err := MessageToMap(newPlainQueueMessage())
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
}
```

上面最后一条用例需要 `newPlainQueueMessage`——先在测试文件顶部加一个辅助构造函数（同包内可用 `message_queue` 包的类型）：

把测试文件的 import 块写成：

```go
import (
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
)
```

并在测试文件末尾追加辅助函数：

```go
// newPlainQueueMessage 构造一个非 Dmq 的普通 QueueMessage，用于 D6 用例
func newPlainQueueMessage() *message_queue.QueueMessage {
	return message_queue.NewQueueMessage(nil)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -run 'TestMessageToMap|TestSerializePayload' -v`
Expected: FAIL — `undefined: MessageToMap`、`undefined: serializePayload`、`undefined: MaxRecurseDepth`

- [ ] **Step 3: 写 `message_serializer.go` 第一部分**

创建 `internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go`：

```go
package dmessage_queue

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxRecurseDepth payload 递归深度上限。
//
// Python: message_serializer.MAX_RECURSE_DEPTH = 10
const MaxRecurseDepth = 10

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MessageToMap 将消息对象转为带 __class__ 标记的 map。
//
// Go 独有辅助：写侧 SerializeMessage 复用它，读侧 consumeLoop 用同一形态
// 把强类型消息交给 handler（handler 契约为 map[string]any）。
//
// 仅接受 DmqRequestMessage / DmqResponseMessage；其它类型立即报错（偏差 D6，
// Python 是发出去、对面 Unknown payload class 才炸）。
func MessageToMap(msg message_queue.QueueMessageBase) (map[string]any, error) {
	switch m := msg.(type) {
	case *DmqRequestMessage:
		payload, err := serializePayload(m.Payload, 1)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"__class__":     "DmqRequestMessage",
			"message_id":    m.MessageID,
			"payload":       payload,
			"error_code":    m.ErrorCode,
			"error_msg":     m.ErrorMsg,
			"type":          string(m.Type),
			"reply_topic":   m.ReplyTopic,
			"request_id":    m.RequestID,
			"sender_id":     m.SenderID,
			"receiver_id":   m.ReceiverID,
			"enable_stream": m.EnableStream,
			"expire_at":     m.ExpireAt,
		}, nil
	case *DmqResponseMessage:
		payload, err := serializePayload(m.Payload, 1)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"__class__":   "DmqResponseMessage",
			"message_id":  m.MessageID,
			"payload":     payload,
			"error_code":  m.ErrorCode,
			"error_msg":   m.ErrorMsg,
			"type":        string(m.Type),
			"result_type": string(m.ResultType),
			"request_id":  m.RequestID,
			"sender_id":   m.SenderID,
			"receiver_id": m.ReceiverID,
			"seq":         m.Seq,
			"last_chunk":  m.LastChunk,
			"expire_at":   m.ExpireAt,
		}, nil
	default:
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicMessageProductionError,
			exception.WithParam("topic", ""),
			exception.WithParam("message", msg),
			exception.WithParam("reason", "unsupported message type for pulsar wire"),
		)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// serializePayload 递归处理 payload 值，注入 datetime 特例并做深度检查。
//
// Python: message_serializer._serialize_payload(payload, depth)
// datetime 用 RFC3339 输出，等价 Python datetime.isoformat()。
func serializePayload(v any, depth int) (any, error) {
	if depth > MaxRecurseDepth {
		return nil, fmt.Errorf("payload nested too deep (> %d)", MaxRecurseDepth)
	}

	switch val := v.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return map[string]any{
			"__type__": "datetime",
			"value":    val.Format(time.RFC3339),
		}, nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			converted, err := serializePayload(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			converted, err := serializePayload(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	default:
		return v, nil
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -run 'TestMessageToMap|TestSerializePayload' -v`
Expected: PASS，6 个用例全绿

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go internal/agentcore/runner/drunner/dmessage_queue/message_serializer_test.go
git commit -m "feat(dmessage_queue): MessageToMap 写出与 payload 递归

9.83：注入 __class__ 标签、datetime 的 __type__ 包装、深度 10 限制；
非 Dmq 类型 fail fast（偏差 D6）。"
```

---

## Task 4: `SerializeMessage` 序列化为字节

**Files:**
- Modify: `internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go`
- Test: `internal/agentcore/runner/drunner/dmessage_queue/message_serializer_test.go`

- [ ] **Step 1: 写失败测试**

在 `message_serializer_test.go` 末尾追加：

```go
// TestSerializeMessage_产出合法JSON 验证序列化结果是带 __class__ 的 JSON 字节
func TestSerializeMessage_产出合法JSON(t *testing.T) {
	msg := NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-1"

	data, err := SerializeMessage(msg)
	if err != nil {
		t.Fatalf("SerializeMessage 失败: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("产出不是合法 JSON: %v，原始: %s", err, string(data))
	}
	if decoded["__class__"] != "DmqRequestMessage" {
		t.Errorf("__class__ 丢失: %s", string(data))
	}
}

// TestSerializeMessage_非Dmq类型报错 验证序列化阶段的 fail fast
func TestSerializeMessage_非Dmq类型报错(t *testing.T) {
	if _, err := SerializeMessage(newPlainQueueMessage()); err == nil {
		t.Fatal("期望报错，实际 nil")
	}
}
```

同时在测试文件 import 块补 `"encoding/json"`：

```go
import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
)
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -run TestSerializeMessage -v`
Expected: FAIL — `undefined: SerializeMessage`

- [ ] **Step 3: 实现 `SerializeMessage`**

在 `message_serializer.go` 的导出函数区块（`MessageToMap` 之后）插入：

```go
// SerializeMessage 将 Dmq 消息序列化为 wire 字节。
//
// Python: message_serializer.serialize_message(msg)
func SerializeMessage(msg message_queue.QueueMessageBase) ([]byte, error) {
	asMap, err := MessageToMap(msg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(asMap)
}
```

并把 `encoding/json` 加进 `message_serializer.go` 的 import 块：

```go
import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -v`
Expected: PASS，全部用例绿

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/runner/drunner/dmessage_queue/
git commit -m "feat(dmessage_queue): SerializeMessage 输出 wire 字节"
```

---

## Task 5: `DeserializeMessage` 读入与默认值补齐

**Files:**
- Modify: `internal/agentcore/runner/drunner/dmessage_queue/message_serializer.go`
- Test: `internal/agentcore/runner/drunner/dmessage_queue/message_serializer_test.go`

- [ ] **Step 1: 写失败测试**

在 `message_serializer_test.go` 末尾追加：

```go
// TestDeserializeMessage_请求往返 验证序列化→反序列化字段全等
func TestDeserializeMessage_请求往返(t *testing.T) {
	orig := NewDmqRequestMessage(map[string]any{"q": "hi"})
	orig.MessageID = "mid-1"
	orig.ReplyTopic = "reply.t"
	orig.RequestID = "rid-1"
	orig.SenderID = "s1"
	orig.ReceiverID = "r1"
	orig.EnableStream = true

	data, err := SerializeMessage(orig)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	got, err := DeserializeMessage(data)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	req, ok := got.(*DmqRequestMessage)
	if !ok {
		t.Fatalf("期望 *DmqRequestMessage，实际 %T", got)
	}
	if req.MessageID != "mid-1" || req.ReplyTopic != "reply.t" || req.RequestID != "rid-1" {
		t.Errorf("字段往返不一致: %+v", req)
	}
	if !req.EnableStream {
		t.Error("enable_stream 丢失")
	}
	if req.Payload["q"] != "hi" {
		t.Errorf("payload 丢失: %v", req.Payload)
	}
}

// TestDeserializeMessage_响应往返 验证响应类型与流式字段
func TestDeserializeMessage_响应往返(t *testing.T) {
	orig := NewDmqResponseMessage(map[string]any{"a": "b"})
	orig.Seq = 7
	orig.LastChunk = true

	data, err := SerializeMessage(orig)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	got, err := DeserializeMessage(data)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	resp, ok := got.(*DmqResponseMessage)
	if !ok {
		t.Fatalf("期望 *DmqResponseMessage，实际 %T", got)
	}
	if resp.Seq != 7 || !resp.LastChunk {
		t.Errorf("流式字段往返不一致: seq=%d last_chunk=%v", resp.Seq, resp.LastChunk)
	}
}

// TestDeserializeMessage_补默认值 验证缺 type/result_type 时补默认
func TestDeserializeMessage_补默认值(t *testing.T) {
	raw := []byte(`{"__class__":"DmqResponseMessage","message_id":"m1","payload":{}}`)

	got, err := DeserializeMessage(raw)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	resp := got.(*DmqResponseMessage)
	if resp.Type != DMessageTypeOutput {
		t.Errorf("Type 期望 OUTPUT，实际 %q", resp.Type)
	}
	if resp.ResultType != ResultTypeMessage {
		t.Errorf("ResultType 期望 MESSAGE，实际 %q", resp.ResultType)
	}
}

// TestDeserializeMessage_未知类名报错 验证对齐 Python ValueError
func TestDeserializeMessage_未知类名报错(t *testing.T) {
	raw := []byte(`{"__class__":"UnknownThing"}`)

	_, err := DeserializeMessage(raw)
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	if !strings.Contains(err.Error(), "unknown payload class: UnknownThing") {
		t.Errorf("错误信息不符: %v", err)
	}
}

// TestDeserializeMessage_嵌套payload保留map 验证 D4：内层不重建类型
func TestDeserializeMessage_嵌套payload保留map(t *testing.T) {
	raw := []byte(`{"__class__":"DmqResponseMessage","payload":{"__class__":"OutputSchema","content":"x"}}`)

	got, err := DeserializeMessage(raw)
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	resp := got.(*DmqResponseMessage)
	if resp.Payload["__class__"] != "OutputSchema" {
		t.Errorf("内层 __class__ 应原样保留，实际 %v", resp.Payload)
	}
}

// TestDeserializeMessage_坏JSON报错 验证非法字节返回错误
func TestDeserializeMessage_坏JSON报错(t *testing.T) {
	if _, err := DeserializeMessage([]byte("not-json")); err == nil {
		t.Fatal("期望报错，实际 nil")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -run TestDeserializeMessage -v`
Expected: FAIL — `undefined: DeserializeMessage`

- [ ] **Step 3: 实现 `DeserializeMessage`**

在 `message_serializer.go` 导出函数区块的 `SerializeMessage` 之后插入：

```go
// DeserializeMessage 将 wire 字节反序列化为强类型 Dmq 消息。
//
// Python: message_serializer.deserialize_message(data)
// payload 内部不做类型重建（偏差 D4），__class__ 键原样保留在 map 中，
// 因此 Go 收进来再发出去，wire 仍与 Python 兼容。
func DeserializeMessage(data []byte) (message_queue.QueueMessageBase, error) {
	var probe struct {
		Class string `json:"__class__"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	switch probe.Class {
	case "DmqRequestMessage":
		var msg DmqRequestMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		if msg.Type == "" {
			msg.Type = DMessageTypeInput
		}
		return &msg, nil
	case "DmqResponseMessage":
		var msg DmqResponseMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		if msg.Type == "" {
			msg.Type = DMessageTypeOutput
		}
		if msg.ResultType == "" {
			msg.ResultType = ResultTypeMessage
		}
		return &msg, nil
	default:
		return nil, fmt.Errorf("unknown payload class: %s", probe.Class)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/drunner/dmessage_queue/ -v -cover`
Expected: PASS，覆盖率 ≥85%

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/runner/drunner/dmessage_queue/
git commit -m "feat(dmessage_queue): DeserializeMessage 读入与默认值补齐

9.83：探 __class__ 后直接 unmarshal 到结构体，补 pydantic 默认值；
未知类名报错对齐 Python ValueError；payload 内部保持 map（偏差 D4）。"
```

---

## Task 6: 引入 Pulsar SDK 并定义窄接口

**Files:**
- Modify: `go.mod`
- Create: `internal/agentcore/message_queue/doc.go`
- Create: `internal/agentcore/message_queue/pulsar_api.go`
- Test: `internal/agentcore/message_queue/pulsar_api_test.go`

- [ ] **Step 1: 引入依赖**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
go get github.com/apache/pulsar-client-go/pulsar@v0.21.0
```

Expected: `go.mod` 出现 `github.com/apache/pulsar-client-go v0.21.0`，`go.sum` 新增大量条目（该 SDK 传递依赖含 grpc/k8s，与已有 milvus 依赖大量重叠）

- [ ] **Step 2: 写失败测试**

创建 `internal/agentcore/message_queue/pulsar_api_test.go`：

```go
package message_queue

import (
	"context"
	"testing"
)

// fakeMessage 测试用消息
type fakeMessage struct {
	payload []byte
}

// Payload 实现 pulsarMessageAPI
func (m *fakeMessage) Payload() []byte { return m.payload }

// fakeProducer 测试用生产者，记录发送参数
type fakeProducer struct {
	sentPayloads []string
	sentKeys     []string
	sendErr      error
	closed       int
}

// Send 实现 pulsarProducerAPI
func (p *fakeProducer) Send(ctx context.Context, payload []byte, partitionKey string) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.sentPayloads = append(p.sentPayloads, string(payload))
	p.sentKeys = append(p.sentKeys, partitionKey)
	return nil
}

// Close 实现 pulsarProducerAPI
func (p *fakeProducer) Close() { p.closed++ }

// fakeConsumer 测试用消费者
type fakeConsumer struct {
	messages []pulsarMessageAPI
	idx      int
	receiveErr error
	ackCount int
	closed   int
}

// Receive 实现 pulsarConsumerAPI
// 无待收消息时阻塞到 ctx 到期，模拟真实 SDK 的阻塞语义
func (c *fakeConsumer) Receive(ctx context.Context) (pulsarMessageAPI, error) {
	if c.receiveErr != nil {
		return nil, c.receiveErr
	}
	if c.idx >= len(c.messages) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	msg := c.messages[c.idx]
	c.idx++
	return msg, nil
}

// Ack 实现 pulsarConsumerAPI
func (c *fakeConsumer) Ack(msg pulsarMessageAPI) error {
	c.ackCount++
	return nil
}

// Close 实现 pulsarConsumerAPI
func (c *fakeConsumer) Close() { c.closed++ }

// fakeClient 测试用客户端
type fakeClient struct {
	producer     *fakeProducer
	consumer     *fakeConsumer
	producerErr  error
	consumerErr  error
	topics       []string
	subTopics    []string
	closed       int
}

// CreateProducer 实现 pulsarClientAPI
func (c *fakeClient) CreateProducer(topic string) (pulsarProducerAPI, error) {
	if c.producerErr != nil {
		return nil, c.producerErr
	}
	c.topics = append(c.topics, topic)
	return c.producer, nil
}

// Subscribe 实现 pulsarClientAPI
func (c *fakeClient) Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error) {
	if c.consumerErr != nil {
		return nil, c.consumerErr
	}
	c.subTopics = append(c.subTopics, topic)
	return c.consumer, nil
}

// Close 实现 pulsarClientAPI
func (c *fakeClient) Close() { c.closed++ }

// TestNarrowInterfaces_可被fake实现 验证三个窄接口可被测试替身满足
func TestNarrowInterfaces_可被fake实现(t *testing.T) {
	var _ pulsarClientAPI = &fakeClient{}
	var _ pulsarConsumerAPI = &fakeConsumer{}
	var _ pulsarProducerAPI = &fakeProducer{}
	var _ pulsarMessageAPI = &fakeMessage{}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -v`
Expected: FAIL — `undefined: pulsarClientAPI` 等四个类型

- [ ] **Step 4: 写 `doc.go`**

创建 `internal/agentcore/message_queue/doc.go`：

```go
// Package message_queue 提供 Pulsar 消息队列后端实现。
//
// 本包是分布式 Runner 的传输管道：按 topic 订阅、生产消息、producer LRU 缓存、
// 消费循环与生命周期管理。自身不含业务语义，消息字段由上层适配器解读。
//
// 通过包内窄接口（pulsarClientAPI/pulsarConsumerAPI/pulsarProducerAPI）
// 隔离 apache/pulsar-client-go，使 LRU、生命周期、消费循环、错误包装可用
// fake 覆盖，真实连接测试走 //go:build integration。
//
// 文件目录：
//
//	message_queue/
//	├── doc.go       # 包文档
//	├── pulsar_api.go # pulsarClientAPI（客户端窄接口）/pulsarConsumerAPI（消费者窄接口）/pulsarProducerAPI（生产者窄接口）/pulsarMessageAPI（消息窄接口） 接口 + sdkClient/sdkConsumer/sdkProducer/sdkMessage 适配器
//	└── pulsar.go    # MessageQueuePulsar（Pulsar消息队列）/PulsarSubscription（Pulsar订阅）
//
// 对应 Python 代码：openjiuwen/extensions/message_queue/message_queue_pulsar.py
package message_queue
```

- [ ] **Step 5: 写 `pulsar_api.go`**

创建 `internal/agentcore/message_queue/pulsar_api.go`：

```go
package message_queue

import (
	"context"

	"github.com/apache/pulsar-client-go/pulsar"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sdkClient apache/pulsar-client-go 客户端适配器
type sdkClient struct {
	// client 真实 SDK 客户端
	client pulsar.Client
}

// sdkConsumer 真实消费者适配器
type sdkConsumer struct {
	// consumer 真实 SDK 消费者
	consumer pulsar.Consumer
}

// sdkProducer 真实生产者适配器
type sdkProducer struct {
	// producer 真实 SDK 生产者
	producer pulsar.Producer
}

// sdkMessage 真实消息适配器
type sdkMessage struct {
	// msg 真实 SDK 消息
	msg pulsar.Message
}

// pulsarClientAPI Pulsar 客户端窄接口
type pulsarClientAPI interface {
	// CreateProducer 创建指定 topic 的 producer
	CreateProducer(topic string) (pulsarProducerAPI, error)
	// Subscribe 订阅 topic，返回 KeyShared 消费者
	Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error)
	// Close 关闭客户端
	Close()
}

// pulsarConsumerAPI Pulsar 消费者窄接口
type pulsarConsumerAPI interface {
	// Receive 阻塞接收一条消息，ctx 到期返回错误
	Receive(ctx context.Context) (pulsarMessageAPI, error)
	// Ack 确认消息
	Ack(msg pulsarMessageAPI) error
	// Close 关闭消费者
	Close()
}

// pulsarProducerAPI Pulsar 生产者窄接口
type pulsarProducerAPI interface {
	// Send 发送消息，partitionKey 决定分区与顺序
	Send(ctx context.Context, payload []byte, partitionKey string) error
	// Close 关闭生产者
	Close()
}

// pulsarMessageAPI Pulsar 消息窄接口
type pulsarMessageAPI interface {
	// Payload 返回消息体
	Payload() []byte
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSDKClient 创建真实 Pulsar 客户端。
//
// Python: pulsar.Client(self._url)
func newSDKClient(url string) (pulsarClientAPI, error) {
	client, err := pulsar.NewClient(pulsar.ClientOptions{URL: url})
	if err != nil {
		return nil, err
	}
	return &sdkClient{client: client}, nil
}

// CreateProducer 创建 producer 并包成窄接口。
func (c *sdkClient) CreateProducer(topic string) (pulsarProducerAPI, error) {
	producer, err := c.client.CreateProducer(pulsar.ProducerOptions{Topic: topic})
	if err != nil {
		return nil, err
	}
	return &sdkProducer{producer: producer}, nil
}

// Subscribe 创建 KeyShared 消费者并包成窄接口。
func (c *sdkClient) Subscribe(topic string, subscriptionName string) (pulsarConsumerAPI, error) {
	consumer, err := c.client.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: subscriptionName,
		Type:             pulsar.KeyShared,
	})
	if err != nil {
		return nil, err
	}
	return &sdkConsumer{consumer: consumer}, nil
}

// Close 关闭客户端。
func (c *sdkClient) Close() { c.client.Close() }

// Receive 接收一条消息并包成窄接口。
func (c *sdkConsumer) Receive(ctx context.Context) (pulsarMessageAPI, error) {
	msg, err := c.consumer.Receive(ctx)
	if err != nil {
		return nil, err
	}
	return &sdkMessage{msg: msg}, nil
}

// Ack 确认消息。
func (c *sdkConsumer) Ack(msg pulsarMessageAPI) error {
	sdkMsg, ok := msg.(*sdkMessage)
	if !ok {
		return nil
	}
	return c.consumer.Ack(sdkMsg.msg)
}

// Close 关闭消费者。
func (c *sdkConsumer) Close() { c.consumer.Close() }

// Send 发送消息，partitionKey 写入 ProducerMessage.Key。
func (p *sdkProducer) Send(ctx context.Context, payload []byte, partitionKey string) error {
	_, err := p.producer.Send(ctx, &pulsar.ProducerMessage{
		Payload: payload,
		Key:     partitionKey,
	})
	return err
}

// Close 关闭生产者。
func (p *sdkProducer) Close() { p.producer.Close() }

// Payload 返回消息体。
func (m *sdkMessage) Payload() []byte { return m.msg.Payload() }
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/message_queue/ && go test ./internal/agentcore/message_queue/ -v`
Expected: PASS（`TestNarrowInterfaces_可被fake实现`）

- [ ] **Step 7: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add go.mod go.sum internal/agentcore/message_queue/
git commit -m "feat(message_queue): 引入 Pulsar SDK 与三个窄接口

9.83：pulsar-client-go v0.21.0；窄接口隔离 SDK，使 LRU/生命周期/
消费循环可用 fake 覆盖（可 mock 禁 build tag）。"
```

---

## Task 7: `MessageQueuePulsar` 结构体与 Start/Stop

**Files:**
- Create: `internal/agentcore/message_queue/pulsar.go`
- Test: `internal/agentcore/message_queue/pulsar_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/message_queue/pulsar_test.go`：

```go
package message_queue

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
)

// newTestQueue 构造一个注入了 fake client 的队列（跳过 Start 的真实建连）
func newTestQueue(client pulsarClientAPI) *MessageQueuePulsar {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))
	q.client = client
	q.running.Store(true)
	return q
}

// TestNewMessageQueuePulsar_不建client 验证构造函数不建立连接
func TestNewMessageQueuePulsar_不建client(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	if q.client != nil {
		t.Error("构造后 client 应为 nil，对齐 Python __init__ 不建连接")
	}
	if q.maxWorkers != 8 {
		t.Errorf("maxWorkers 期望 8，实际 %d", q.maxWorkers)
	}
	if q.maxProducers != MaxProducers {
		t.Errorf("maxProducers 期望 %d，实际 %d", MaxProducers, q.maxProducers)
	}
}

// TestStart_幂等 验证重复 Start 只建一次 client
func TestStart_幂等(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))
	q.startClient = func(url string) (pulsarClientAPI, error) {
		return &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}, nil
	}

	q.Start()
	first := q.client
	q.Start()

	if first == nil || q.client != first {
		t.Error("重复 Start 应复用同一个 client")
	}
	if !q.running.Load() {
		t.Error("Start 后 running 应为 true")
	}
}

// TestStart_建client失败不置running 验证 SDK 建连失败的处理
func TestStart_建client失败不置running(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))
	q.startClient = func(url string) (pulsarClientAPI, error) {
		return nil, context.DeadlineExceeded
	}

	q.Start()

	if q.running.Load() {
		t.Error("建客户端失败时 running 应保持 false")
	}
}

// TestStop_未启动 验证未运行时 Stop 无副作用
func TestStop_未启动(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	if err := q.Stop(context.Background()); err != nil {
		t.Fatalf("Stop 期望 nil，实际 %v", err)
	}
}

// TestStop_关闭client与producer 验证 Stop 释放全部资源
func TestStop_关闭client与producer(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	if _, err := q.Subscribe("topic.a"); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}
	if err := q.Produce(context.Background(), "topic.a", newTestRequest()); err != nil {
		t.Fatalf("Produce 失败: %v", err)
	}

	if err := q.Stop(context.Background()); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}

	if client.closed != 1 {
		t.Errorf("client.Close 调用次数期望 1，实际 %d", client.closed)
	}
	if client.producer.closed != 1 {
		t.Errorf("producer.Close 调用次数期望 1，实际 %d", client.producer.closed)
	}
}
```

上面用到了两个后续任务才实现的符号（`q.startClient` 字段、`newTestRequest`）。为了让本任务可独立跑绿，先在测试文件底部加辅助函数：

```go
// newTestRequest 构造一条最小请求消息
func newTestRequest() *dmessage_queue.DmqRequestMessage {
	return dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
}
```

并在测试文件 import 块补：

```go
import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
)
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -run 'TestNewMessageQueuePulsar|TestStart|TestStop' -v`
Expected: FAIL — `undefined: NewMessageQueuePulsar`

- [ ] **Step 3: 写 `pulsar.go` 的结构体与 Start/Stop**

创建 `internal/agentcore/message_queue/pulsar.go`：

```go
package message_queue

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// producerEntry producer LRU 链表节点载荷
type producerEntry struct {
	// topic 该 producer 对应的 topic
	topic string
	// producer 生产者窄接口
	producer pulsarProducerAPI
}

// MessageQueuePulsar Pulsar 消息队列封装。
//
// Python: MessageQueuePulsar(MessageQueueBase)
type MessageQueuePulsar struct {
	// url Pulsar 服务地址
	url string
	// maxWorkers 工作线程数（仅保存，Go 侧不消费，偏差 D1）
	maxWorkers int
	// maxProducers producer LRU 容量上限
	maxProducers int
	// client Pulsar 客户端窄接口
	client pulsarClientAPI
	// startClient 建客户端钩子，便于测试注入
	startClient func(url string) (pulsarClientAPI, error)
	// producers topic → LRU 链表节点
	producers map[string]*list.Element
	// producerOrder producer LRU 顺序（Front 最旧）
	producerOrder *list.List
	// subs topic → 订阅
	subs map[string]*PulsarSubscription
	// mu 保护 producers/producerOrder/subs（单锁，偏差 D2）
	mu sync.Mutex
	// running 是否运行中
	running atomic.Bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxProducers producer LRU 容量上限。
//
// Python: MessageQueuePulsar.MAX_PRODUCERS = 10000
const MaxProducers = 10000

// DefaultSubscriptionName 默认订阅名。
//
// Python: MessageQueuePulsar.DEFAULT_SUBSCRIPTION_NAME = "default"
const DefaultSubscriptionName = "default"

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// errQueueNotRunning 消息队列未运行
var errQueueNotRunning = errors.New("pulsar message queue is not running")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageQueuePulsar 创建 Pulsar 消息队列，不建立连接。
//
// Python: MessageQueuePulsar.__init__(pulsar_config)
func NewMessageQueuePulsar(cfg *config.PulsarConfig) *MessageQueuePulsar {
	url := ""
	maxWorkers := 8
	if cfg != nil {
		url = cfg.URL
		if cfg.MaxWorkers > 0 {
			maxWorkers = cfg.MaxWorkers
		}
	}
	return &MessageQueuePulsar{
		url:           url,
		maxWorkers:    maxWorkers,
		maxProducers:  MaxProducers,
		startClient:   newSDKClient,
		producers:     make(map[string]*list.Element),
		producerOrder: list.New(),
		subs:          make(map[string]*PulsarSubscription),
	}
}

// Start 启动消息队列。
//
// Python: MessageQueuePulsar.start() L101-107
// Go 侧 start 无返回值（接口约束），建客户端失败时记 Error 日志并保持未运行。
func (q *MessageQueuePulsar) Start() {
	if q.running.Load() {
		return
	}

	client, err := q.startClient(q.url)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "mq_pulsar_start_failed").
			Str("url", q.url).
			Err(err).
			Msg("create pulsar client failed")
		return
	}

	q.client = client
	q.running.Store(true)
	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_started").
		Str("url", q.url).
		Msg("pulsar message queue started")
}

// Stop 停止消息队列，关闭全部订阅与 producer。
//
// Python: MessageQueuePulsar.stop() L109-124
func (q *MessageQueuePulsar) Stop(ctx context.Context) error {
	if !q.running.Load() {
		return nil
	}
	q.running.Store(false)

	q.mu.Lock()
	topics := make([]string, 0, len(q.subs))
	for topic := range q.subs {
		topics = append(topics, topic)
	}
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_stopping_subscriptions").
		Int("subscription_count", len(topics)).
		Msg("closing subscriptions")
	for _, topic := range topics {
		if err := q.Unsubscribe(ctx, topic); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "mq_pulsar_unsubscribe_failed").
				Str("topic", topic).
				Err(err).
				Msg("unsubscribe failed during stop")
		}
	}

	q.mu.Lock()
	producerCount := q.producerOrder.Len()
	for elem := q.producerOrder.Front(); elem != nil; elem = elem.Next() {
		elem.Value.(*producerEntry).producer.Close()
	}
	q.producers = make(map[string]*list.Element)
	q.producerOrder.Init()
	client := q.client
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_stopping_producers").
		Int("producer_count", producerCount).
		Msg("closing producers")

	if client != nil {
		client.Close()
	}
	q.client = nil

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_stopped").
		Msg("pulsar message queue stopped")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 补 `PulsarSubscription` 空壳以通过编译**

按项目「源码声明排列顺序」规范分三处插入（完整实现在 Task 10/11）：

① 结构体插入**结构体区块**（`producerEntry` 之后）：

```go
// PulsarSubscription Pulsar topic 订阅。
//
// Python: PulsarSubscription(SubscriptionBase)
type PulsarSubscription struct {
	// topic 订阅的 topic 名
	topic string
	// consumer Pulsar 消费者窄接口
	consumer pulsarConsumerAPI
	// handler 消息处理回调
	handler func(ctx context.Context, payload map[string]any) (any, error)
	// handlerMu 保护 handler
	handlerMu sync.RWMutex
	// stateMu 保护 cancel/done
	stateMu sync.Mutex
	// cancel 消费 goroutine 取消函数
	cancel context.CancelFunc
	// done 消费 goroutine 退出信号
	done chan struct{}
	// active 是否活跃
	active atomic.Bool
}
```

② 导出方法插入**导出函数区块**：

```go
// SetMessageHandler 设置消息处理回调。实现 SubscriptionBase 接口。
func (s *PulsarSubscription) SetMessageHandler(handler func(ctx context.Context, payload map[string]any) (any, error)) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.handler = handler
}

// IsActive 返回订阅是否活跃。实现 SubscriptionBase 接口。
func (s *PulsarSubscription) IsActive() bool { return s.active.Load() }
```

③ 构造函数插入**非导出函数区块**：

```go
// newPulsarSubscription 创建订阅。
func newPulsarSubscription(topic string, consumer pulsarConsumerAPI) *PulsarSubscription {
	return &PulsarSubscription{
		topic:    topic,
		consumer: consumer,
		done:     make(chan struct{}),
	}
}
```

> 注意：`PulsarSubscription` 的 `Activate`/`Deactivate` 在 Task 10 实现；本任务 `Stop` 会调用 `Unsubscribe`，而 `Unsubscribe` 在 Step 5 补最小实现，因此本任务需同时补 `Subscribe`/`Unsubscribe`/`Produce`。

- [ ] **Step 5: 补最小 `Subscribe`/`Unsubscribe`/`Produce` 使测试可编译**

在 `pulsar.go` 导出函数区块追加（完整 LRU 与错误包装在 Task 8/9 精化）：

```go
// Subscribe 创建 topic 订阅。
//
// Python: MessageQueuePulsar.subscribe(topic) L126-139
func (q *MessageQueuePulsar) Subscribe(topic string) (message_queue.SubscriptionBase, error) {
	if !q.running.Load() {
		return nil, errQueueNotRunning
	}

	q.mu.Lock()
	if sub, ok := q.subs[topic]; ok {
		q.mu.Unlock()
		return sub, nil
	}
	q.mu.Unlock()

	consumer, err := q.client.Subscribe(topic, DefaultSubscriptionName)
	if err != nil {
		return nil, err
	}

	sub := newPulsarSubscription(topic, consumer)
	q.mu.Lock()
	q.subs[topic] = sub
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_created").
		Str("topic", topic).
		Msg("create new subscription")
	return sub, nil
}

// Unsubscribe 取消 topic 订阅。
//
// Python: MessageQueuePulsar.unsubscribe(topic) L141-145
func (q *MessageQueuePulsar) Unsubscribe(ctx context.Context, topic string) error {
	q.mu.Lock()
	sub, ok := q.subs[topic]
	if ok {
		delete(q.subs, topic)
	}
	q.mu.Unlock()

	if !ok {
		return nil
	}

	sub.Deactivate()
	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_unsubscribed").
		Str("topic", topic).
		Msg("unsubscribed")
	return nil
}

// Produce 发布消息到指定 topic。
//
// Python: MessageQueuePulsar.produce_message(topic, message) L151-177
func (q *MessageQueuePulsar) Produce(ctx context.Context, topic string, msg message_queue.QueueMessageBase) error {
	return q.produceMessage(ctx, topic, msg)
}
```

并在非导出函数区块追加：

```go
// produceMessage 发布消息的内部实现
func (q *MessageQueuePulsar) produceMessage(ctx context.Context, topic string, msg message_queue.QueueMessageBase) error {
	if !q.running.Load() {
		return errQueueNotRunning
	}

	producer, err := q.getOrCreateProducer(topic)
	if err != nil {
		return err
	}

	content, err := dmessage_queue.SerializeMessage(msg)
	if err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_sending").
		Str("topic", topic).
		Str("message_id", msg.GetMessageID()).
		Msg("sending message")

	if err := producer.Send(ctx, content, msg.GetMessageID()); err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_sent").
		Str("topic", topic).
		Str("message_id", msg.GetMessageID()).
		Msg("message sent successfully")
	return nil
}

// getOrCreateProducer 获取或创建 producer（Task 9 补 LRU 淘汰）
func (q *MessageQueuePulsar) getOrCreateProducer(topic string) (pulsarProducerAPI, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if elem, ok := q.producers[topic]; ok {
		q.producerOrder.MoveToBack(elem)
		return elem.Value.(*producerEntry).producer, nil
	}

	producer, err := q.client.CreateProducer(topic)
	if err != nil {
		return nil, err
	}
	q.producers[topic] = q.producerOrder.PushBack(&producerEntry{topic: topic, producer: producer})
	return producer, nil
}
```

- [ ] **Step 6: 补 `Deactivate` 最小实现**

`Unsubscribe` 会调用 `sub.Deactivate()`，先给出 Task 10 会精化的最小版本。在 `pulsar.go` 的 `SetMessageHandler` 附近追加：

```go
// Activate 激活订阅，启动消费 goroutine（Task 10/11 实现）
func (s *PulsarSubscription) Activate() {}

// Deactivate 停用订阅，关闭消费者。
func (s *PulsarSubscription) Deactivate() {
	s.consumer.Close()
}
```

- [ ] **Step 7: 补 import**

`pulsar.go` 的 import 块需包含：

```go
import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 8: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -v`
Expected: PASS，本任务 5 个用例绿

- [ ] **Step 9: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/message_queue/
git commit -m "feat(message_queue): MessageQueuePulsar 结构体与 Start/Stop

9.83：Start 建客户端失败时保持未运行并记 Error（接口无 error 返回）；
Stop 顺序对齐 Python：停订阅→关 producer→关 client。"
```

---

## Task 8: `Subscribe`/`Unsubscribe` 错误码与复用语义

**Files:**
- Modify: `internal/agentcore/message_queue/pulsar.go`
- Test: `internal/agentcore/message_queue/pulsar_test.go`

- [ ] **Step 1: 写失败测试**

在 `pulsar_test.go` 末尾追加：

```go
// TestSubscribe_未启动报错 验证错误码为 MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR
func TestSubscribe_未启动报错(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	_, err := q.Subscribe("topic.a")
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("期望 *exception.BaseError，实际 %T", err)
	}
	if baseErr.Status().Name() != "MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR" {
		t.Errorf("错误码不符: %s", baseErr.Status().Name())
	}
}

// TestSubscribe_同topic复用 验证重复订阅返回同一实例且只建一次 consumer
func TestSubscribe_同topic复用(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	first, err := q.Subscribe("topic.a")
	if err != nil {
		t.Fatalf("首次订阅失败: %v", err)
	}
	second, err := q.Subscribe("topic.a")
	if err != nil {
		t.Fatalf("二次订阅失败: %v", err)
	}

	if first != second {
		t.Error("同 topic 重复订阅应返回同一订阅实例")
	}
	if len(client.subTopics) != 1 {
		t.Errorf("consumer 只应创建 1 次，实际 %d", len(client.subTopics))
	}
}

// TestSubscribe_建consumer失败报错 验证错误被包装
func TestSubscribe_建consumer失败报错(t *testing.T) {
	client := &fakeClient{consumerErr: context.DeadlineExceeded}
	q := newTestQueue(client)

	_, err := q.Subscribe("topic.a")
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	if _, ok := err.(*exception.BaseError); !ok {
		t.Fatalf("期望 *exception.BaseError，实际 %T", err)
	}
}

// TestUnsubscribe_不存在topic不报错 验证对齐 Python pop 后空判
func TestUnsubscribe_不存在topic不报错(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	if err := q.Unsubscribe(context.Background(), "no.such.topic"); err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
}

// TestUnsubscribe_关闭consumer 验证取消订阅会关闭已激活的消费者
func TestUnsubscribe_关闭consumer(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	sub, err := q.Subscribe("topic.a")
	if err != nil {
		t.Fatalf("订阅失败: %v", err)
	}
	// Python deactivate() 对未激活订阅直接返回（不关 consumer），故须先激活
	sub.Activate()

	if err := q.Unsubscribe(context.Background(), "topic.a"); err != nil {
		t.Fatalf("取消订阅失败: %v", err)
	}

	if client.consumer.closed != 1 {
		t.Errorf("consumer.Close 期望 1 次，实际 %d", client.consumer.closed)
	}
}
```

测试文件 import 块补 `"github.com/uapclaw/uapclaw-go/internal/common/exception"`。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -run 'TestSubscribe|TestUnsubscribe' -v`
Expected: FAIL — `TestSubscribe_未启动报错` 断言失败（当前返回裸 error 而非 BaseError）

- [ ] **Step 3: 加错误包装**

把 `pulsar.go` 的 `Subscribe` 替换为：

```go
// Subscribe 创建 topic 订阅。
//
// Python: MessageQueuePulsar.subscribe(topic) L126-139
// Python 用 try/except 包住全部步骤（含 _validate_running），统一转成订阅错误。
func (q *MessageQueuePulsar) Subscribe(topic string) (message_queue.SubscriptionBase, error) {
	if !q.running.Load() {
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicSubscriptionError,
			exception.WithCause(errQueueNotRunning),
			exception.WithParam("topic", topic),
			exception.WithParam("reason", errQueueNotRunning),
		)
	}

	q.mu.Lock()
	if sub, ok := q.subs[topic]; ok {
		q.mu.Unlock()
		return sub, nil
	}
	q.mu.Unlock()

	consumer, err := q.client.Subscribe(topic, DefaultSubscriptionName)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusMessageQueueTopicSubscriptionError,
			exception.WithCause(err),
			exception.WithParam("topic", topic),
			exception.WithParam("reason", err),
		)
	}

	sub := newPulsarSubscription(topic, consumer)
	q.mu.Lock()
	q.subs[topic] = sub
	q.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_created").
		Str("topic", topic).
		Msg("create new subscription")
	return sub, nil
}
```

`pulsar.go` import 块补：

```go
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -v`
Expected: PASS，全部用例绿

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/message_queue/
git commit -m "feat(message_queue): Subscribe/Unsubscribe 错误码与复用语义

9.83：订阅失败统一包装为 MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR；
Unsubscribe 对不存在 topic 返回 nil（对齐 Python pop 后空判）。"
```

---

## Task 9: producer LRU 缓存与淘汰

**Files:**
- Modify: `internal/agentcore/message_queue/pulsar.go`
- Test: `internal/agentcore/message_queue/pulsar_test.go`

- [ ] **Step 1: 写失败测试**

在 `pulsar_test.go` 末尾追加：

```go
// TestProduce_未启动报错 验证错误码为 MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR
func TestProduce_未启动报错(t *testing.T) {
	q := NewMessageQueuePulsar(config.NewPulsarConfig("pulsar://localhost:6650"))

	err := q.Produce(context.Background(), "topic.a", newTestRequest())
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("期望 *exception.BaseError，实际 %T", err)
	}
	if baseErr.Status().Name() != "MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR" {
		t.Errorf("错误码不符: %s", baseErr.Status().Name())
	}
}

// TestProduce_partitionKey为messageID 验证 partitionKey 传 message_id
func TestProduce_partitionKey为messageID(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-42"

	if err := q.Produce(context.Background(), "topic.a", msg); err != nil {
		t.Fatalf("Produce 失败: %v", err)
	}

	if len(client.producer.sentKeys) != 1 || client.producer.sentKeys[0] != "mid-42" {
		t.Errorf("partitionKey 期望 mid-42，实际 %v", client.producer.sentKeys)
	}
}

// TestGetOrCreateProducer_复用同一producer 验证同 topic 不重复创建
func TestGetOrCreateProducer_复用同一producer(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)

	if _, err := q.getOrCreateProducer("topic.a"); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if _, err := q.getOrCreateProducer("topic.a"); err != nil {
		t.Fatalf("二次获取失败: %v", err)
	}

	if len(client.topics) != 1 {
		t.Errorf("producer 只应创建 1 次，实际 %d", len(client.topics))
	}
}

// TestGetOrCreateProducer_LRU淘汰最旧 验证超过容量淘汰最久未用
func TestGetOrCreateProducer_LRU淘汰最旧(t *testing.T) {
	client := &fakeClient{producer: &fakeProducer{}, consumer: &fakeConsumer{}}
	q := newTestQueue(client)
	q.maxProducers = 2

	for _, topic := range []string{"t1", "t2"} {
		if _, err := q.getOrCreateProducer(topic); err != nil {
			t.Fatalf("创建 %s 失败: %v", topic, err)
		}
	}
	// 访问 t1，使其成为最近使用
	if _, err := q.getOrCreateProducer("t1"); err != nil {
		t.Fatalf("访问 t1 失败: %v", err)
	}
	// 创建 t3，应淘汰最久未用的 t2
	if _, err := q.getOrCreateProducer("t3"); err != nil {
		t.Fatalf("创建 t3 失败: %v", err)
	}

	if _, ok := q.producers["t2"]; ok {
		t.Error("t2 应已被淘汰")
	}
	if _, ok := q.producers["t1"]; !ok {
		t.Error("t1 应被保留（最近使用）")
	}
	if client.producer.closed != 1 {
		t.Errorf("被淘汰 producer 应 Close 1 次，实际 %d", client.producer.closed)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -run 'TestProduce|TestGetOrCreateProducer' -v`
Expected: FAIL — `TestProduce_未启动报错` 返回裸 error；`TestGetOrCreateProducer_LRU淘汰最旧` 未淘汰

- [ ] **Step 3: 加 LRU 淘汰与错误包装**

把 `getOrCreateProducer` 替换为完整版：

```go
// getOrCreateProducer 获取或创建 topic 对应的 producer，带 LRU 淘汰。
//
// Python: MessageQueuePulsar._get_or_create_producer(topic) L179-208
// 单锁内完成全部判断与创建（偏差 D2：取消 Python 的双检快路径）。
func (q *MessageQueuePulsar) getOrCreateProducer(topic string) (pulsarProducerAPI, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if elem, ok := q.producers[topic]; ok {
		q.producerOrder.MoveToBack(elem)
		return elem.Value.(*producerEntry).producer, nil
	}

	if len(q.producers) >= q.maxProducers {
		if front := q.producerOrder.Front(); front != nil {
			evicted := front.Value.(*producerEntry)
			q.producerOrder.Remove(front)
			delete(q.producers, evicted.topic)
			evicted.producer.Close()
			logger.Debug(logComponent).
				Str("event_type", "mq_pulsar_producer_evicted").
				Str("topic", evicted.topic).
				Msg("LRU producer evicted")
		}
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_producer_creating").
		Str("topic", topic).
		Msg("creating new producer")

	producer, err := q.client.CreateProducer(topic)
	if err != nil {
		return nil, err
	}
	q.producers[topic] = q.producerOrder.PushBack(&producerEntry{topic: topic, producer: producer})
	return producer, nil
}
```

把 `Produce` 替换为带错误包装的版本：

```go
// Produce 发布消息到指定 topic。
//
// Python: MessageQueuePulsar.produce_message(topic, message) L151-177
// Python 用 try/except 包住全部步骤（含 _validate_running），统一转成生产错误。
func (q *MessageQueuePulsar) Produce(ctx context.Context, topic string, msg message_queue.QueueMessageBase) error {
	if err := q.produceMessage(ctx, topic, msg); err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return err
		}
		return exception.BuildError(
			exception.StatusMessageQueueTopicMessageProductionError,
			exception.WithCause(err),
			exception.WithParam("topic", topic),
			exception.WithParam("message", msg),
			exception.WithParam("reason", err),
		)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -v`
Expected: PASS，全部用例绿

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/message_queue/
git commit -m "feat(message_queue): producer LRU 缓存与淘汰

9.83：container/list 承载 OrderedDict 语义，Front 最旧；
满额淘汰最久未用并 Close；Produce 统一包装生产错误。"
```

---

## Task 10: `Activate`/`Deactivate` 生命周期

**Files:**
- Modify: `internal/agentcore/message_queue/pulsar.go`
- Test: `internal/agentcore/message_queue/pulsar_test.go`

- [ ] **Step 1: 写失败测试**

在 `pulsar_test.go` 末尾追加：

```go
// TestActivate_幂等 验证重复 Activate 只起一条 goroutine
func TestActivate_幂等(t *testing.T) {
	consumer := &fakeConsumer{}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.Activate()
	sub.Activate()

	if !sub.IsActive() {
		t.Error("Activate 后应处于活跃状态")
	}
	sub.Deactivate()
}

// TestDeactivate_关闭consumer并退出goroutine 验证同步等待消费循环结束
func TestDeactivate_关闭consumer并退出goroutine(t *testing.T) {
	consumer := &fakeConsumer{}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.Activate()
	sub.Deactivate()

	if sub.IsActive() {
		t.Error("Deactivate 后应为非活跃")
	}
	if consumer.closed != 1 {
		t.Errorf("consumer.Close 期望 1 次，实际 %d", consumer.closed)
	}
}

// TestDeactivate_未激活无副作用 验证幂等
func TestDeactivate_未激活无副作用(t *testing.T) {
	consumer := &fakeConsumer{}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.Deactivate()

	if consumer.closed != 0 {
		t.Errorf("未激活时不应关闭 consumer，实际 %d", consumer.closed)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -run 'TestActivate|TestDeactivate' -v`
Expected: FAIL — `TestActivate_幂等` 断言失败（当前 `Activate` 是空实现，`IsActive` 恒 false）

- [ ] **Step 3: 实现 `Activate`/`Deactivate`**

把 `pulsar.go` 里的 `Activate`/`Deactivate` 空壳替换为：

```go
// Activate 激活订阅，启动消费 goroutine（幂等）。
//
// Python: PulsarSubscription.activate() L35-39
func (s *PulsarSubscription) Activate() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.active.Load() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	s.active.Store(true)

	go s.consumeLoop(ctx)

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_activated").
		Str("topic", s.topic).
		Msg("pulsar subscription activated")
}

// Deactivate 停用订阅，同步等待消费 goroutine 退出（幂等）。
//
// Python: PulsarSubscription.deactivate() L41-59
// Go 接口无 ctx/error 返回（偏差 D3），改为同步阻塞，最长等待一个 receive 超时周期。
func (s *PulsarSubscription) Deactivate() {
	s.stateMu.Lock()
	if !s.active.Load() {
		s.stateMu.Unlock()
		return
	}
	s.active.Store(false)
	cancel := s.cancel
	done := s.done
	s.stateMu.Unlock()

	if cancel != nil {
		cancel()
	}
	// 关闭消费者以解除阻塞中的 Receive
	s.consumer.Close()
	if done != nil {
		<-done
	}

	logger.Info(logComponent).
		Str("event_type", "mq_pulsar_subscription_deactivated").
		Str("topic", s.topic).
		Msg("pulsar subscription deactivated")
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -v`
Expected: PASS，全部用例绿

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/message_queue/
git commit -m "feat(message_queue): PulsarSubscription 生命周期

9.83：Activate 幂等起消费 goroutine；Deactivate cancel→Close→等 done，
同步阻塞（偏差 D3）。stateMu 保护 cancel/done 避免数据竞争。"
```

---

## Task 11: 消费循环 `consumeLoop`

**Files:**
- Modify: `internal/agentcore/message_queue/pulsar.go`
- Test: `internal/agentcore/message_queue/pulsar_test.go`

- [ ] **Step 1: 写失败测试**

在 `pulsar_test.go` 末尾追加：

```go
// TestConsumeLoop_handler收到完整消息map 验证传整条消息而非 payload 字段（偏差 D7）
func TestConsumeLoop_handler收到完整消息map(t *testing.T) {
	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-1"
	msg.ReplyTopic = "reply.t"
	data, err := dmessage_queue.SerializeMessage(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: data}}}
	sub := newPulsarSubscription("topic.a", consumer)

	received := make(chan map[string]any, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		received <- payload
		return nil, nil
	})

	sub.Activate()
	defer sub.Deactivate()

	select {
	case payload := <-received:
		if payload["__class__"] != "DmqRequestMessage" {
			t.Errorf("handler 应收到整条消息 map，实际: %v", payload)
		}
		if payload["reply_topic"] != "reply.t" {
			t.Errorf("reply_topic 丢失: %v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被调用")
	}
}

// TestConsumeLoop_成功后ack 验证处理后确认消息
func TestConsumeLoop_成功后ack(t *testing.T) {
	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	data, _ := dmessage_queue.SerializeMessage(msg)

	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: data}}}
	sub := newPulsarSubscription("topic.a", consumer)

	done := make(chan struct{}, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		done <- struct{}{}
		return nil, nil
	})

	sub.Activate()
	defer sub.Deactivate()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被调用")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if consumer.ackCount > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("消息未被 ack")
}

// TestConsumeLoop_handler报错不ack 验证对齐 Python 异常路径不 acknowledge
func TestConsumeLoop_handler报错不ack(t *testing.T) {
	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "hi"})
	data, _ := dmessage_queue.SerializeMessage(msg)

	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: data}}}
	sub := newPulsarSubscription("topic.a", consumer)

	called := make(chan struct{}, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		called <- struct{}{}
		return nil, context.Canceled
	})

	sub.Activate()
	defer sub.Deactivate()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未被调用")
	}

	time.Sleep(100 * time.Millisecond)
	if consumer.ackCount != 0 {
		t.Errorf("handler 报错时不应 ack，实际 ack %d 次", consumer.ackCount)
	}
}

// TestConsumeLoop_反序列化失败不ack 验证坏消息不确认
func TestConsumeLoop_反序列化失败不ack(t *testing.T) {
	consumer := &fakeConsumer{messages: []pulsarMessageAPI{&fakeMessage{payload: []byte("not-json")}}}
	sub := newPulsarSubscription("topic.a", consumer)

	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		return nil, nil
	})

	sub.Activate()
	time.Sleep(100 * time.Millisecond)
	sub.Deactivate()

	if consumer.ackCount != 0 {
		t.Errorf("反序列化失败时不应 ack，实际 %d", consumer.ackCount)
	}
}
```

测试文件 import 块补 `"time"`。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -run TestConsumeLoop -v`
Expected: FAIL — `undefined: (*PulsarSubscription).consumeLoop`（goroutine 调用不存在的方法）

- [ ] **Step 3: 实现 `consumeLoop`**

先把 `receiveTimeout` 常量插入 `pulsar.go` 的**常量区块**（`DefaultSubscriptionName` 之后）：

```go
// receiveTimeout 单次 Receive 的超时时间。
//
// Python: consumer.receive(timeout_millis=1000)
const receiveTimeout = time.Second
```

再在**非导出函数区块**追加：

```go
// consumeLoop 消费循环主逻辑。
//
// Python: PulsarSubscription._consume_loop() L64-83
func (s *PulsarSubscription) consumeLoop(ctx context.Context) {
	defer close(s.done)

	for s.active.Load() {
		recvCtx, cancel := context.WithTimeout(ctx, receiveTimeout)
		msg, err := s.consumer.Receive(recvCtx)
		cancel()

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				// 对齐 pulsar.Timeout: continue
				continue
			}
			if ctx.Err() != nil {
				return
			}
			logger.Error(logComponent).
				Str("event_type", "mq_pulsar_receive_error").
				Str("topic", s.topic).
				Err(err).
				Msg("pulsar receive failed")
			continue
		}

		decoded, err := dmessage_queue.DeserializeMessage(msg.Payload())
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "mq_pulsar_deserialize_error").
				Str("topic", s.topic).
				Err(err).
				Msg("pulsar message deserialize failed")
			continue
		}

		payload, err := dmessage_queue.MessageToMap(decoded)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "mq_pulsar_message_to_map_error").
				Str("topic", s.topic).
				Err(err).
				Msg("pulsar message to map failed")
			continue
		}

		logger.Info(logComponent).
			Str("event_type", "mq_pulsar_message_received").
			Str("topic", s.topic).
			Str("message_id", decoded.GetMessageID()).
			Str("message_class", fmt.Sprint(payload["__class__"])).
			Str("message_type", fmt.Sprint(payload["type"])).
			Msg("received message")

		if err := s.callHandler(ctx, payload); err != nil {
			logger.Error(logComponent).
				Str("event_type", "mq_pulsar_handler_error").
				Str("topic", s.topic).
				Err(err).
				Msg("pulsar message handler failed")
			// 对齐 Python 异常路径：不 acknowledge，触发重投
			continue
		}

		if err := s.consumer.Ack(msg); err != nil {
			logger.Error(logComponent).
				Str("event_type", "mq_pulsar_ack_error").
				Str("topic", s.topic).
				Err(err).
				Msg("pulsar message ack failed")
		}
	}
}

// callHandler 调用已注册的处理器；未注册时返回 nil（对齐 Python `if self._handler`）。
func (s *PulsarSubscription) callHandler(ctx context.Context, payload map[string]any) error {
	s.handlerMu.RLock()
	handler := s.handler
	s.handlerMu.RUnlock()

	if handler == nil {
		return nil
	}
	_, err := handler(ctx, payload)
	return err
}
```

`pulsar.go` import 块补 `"fmt"` 与 `"time"`。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/message_queue/ -race -v -cover`
Expected: PASS，全部用例绿，无 data race 报告，覆盖率 ≥85%

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/message_queue/
git commit -m "feat(message_queue): 消费循环

9.83：Receive(1s) 超时 continue、其它错误记日志继续；
反序列化失败与 handler 报错均不 ack（对齐 Python 触发重投）；
handler 收整条消息 map（偏差 D7）。"
```

---

## Task 12: 集成测试骨架

**Files:**
- Create: `internal/agentcore/message_queue/pulsar_integration_test.go`

- [ ] **Step 1: 写集成测试**

创建 `internal/agentcore/message_queue/pulsar_integration_test.go`：

```go
//go:build integration

package message_queue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/drunner/dmessage_queue"
)

// TestPulsarMessageQueue_真实收发 测试真实 Pulsar 端的完整链路
// 运行方式: PULSAR_URL=pulsar://localhost:6650 go test -tags=integration ./internal/agentcore/message_queue/...
func TestPulsarMessageQueue_真实收发(t *testing.T) {
	url := os.Getenv("PULSAR_URL")
	if url == "" {
		t.Skip("未设置 PULSAR_URL，跳过真实 Pulsar 测试")
	}

	topic := "persistent://public/default/uapclaw-9.83-test"

	q := NewMessageQueuePulsar(config.NewPulsarConfig(url))
	q.Start()
	if !q.running.Load() {
		t.Fatal("Pulsar 客户端启动失败，检查 PULSAR_URL 是否可达")
	}
	defer func() {
		if err := q.Stop(context.Background()); err != nil {
			t.Logf("Stop 报错: %v", err)
		}
	}()

	sub, err := q.Subscribe(topic)
	if err != nil {
		t.Fatalf("订阅失败: %v", err)
	}

	received := make(chan map[string]any, 1)
	sub.SetMessageHandler(func(ctx context.Context, payload map[string]any) (any, error) {
		received <- payload
		return nil, nil
	})
	sub.Activate()

	msg := dmessage_queue.NewDmqRequestMessage(map[string]any{"q": "integration"})
	msg.MessageID = "it-1"
	msg.ReplyTopic = "reply.t"
	if err := q.Produce(context.Background(), topic, msg); err != nil {
		t.Fatalf("生产失败: %v", err)
	}

	select {
	case payload := <-received:
		if payload["reply_topic"] != "reply.t" {
			t.Errorf("reply_topic 丢失: %v", payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("10s 内未收到消息")
	}
}
```

- [ ] **Step 2: 确认默认构建不受影响**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/message_queue/ && go vet -tags integration ./internal/agentcore/message_queue/`
Expected: 均无输出（编译通过）

- [ ] **Step 3: 确认无 broker 时跳过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags=integration ./internal/agentcore/message_queue/ -run TestPulsarMessageQueue_真实收发 -v`
Expected: `--- SKIP: TestPulsarMessageQueue_真实收发`（未设 PULSAR_URL）

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/message_queue/pulsar_integration_test.go
git commit -m "test(message_queue): Pulsar 真实收发集成测试骨架

9.83：//go:build integration + PULSAR_URL 环境变量，无 broker 时 skip。"
```

---

## Task 13: 全量回归、覆盖率与计划表回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:640`

- [ ] **Step 1: 确认无残留 go 进程**

Run: `pgrep -f 'go (build|test)' || echo "无残留进程"`
Expected: 输出 `无残留进程`；若有输出先 `pkill -f 'go build'; pkill -f 'go test'`

- [ ] **Step 2: 格式化与静态检查**

```bash
cd /home/opensource/uapclaw-gateway
gofmt -l internal/agentcore/message_queue/ internal/agentcore/runner/drunner/dmessage_queue/ internal/agentcore/runner/message_queue/base.go
go vet ./internal/agentcore/message_queue/... ./internal/agentcore/runner/drunner/...
```

Expected: `gofmt -l` 无输出；`go vet` 无输出

- [ ] **Step 3: 目标包覆盖率**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/message_queue/... ./internal/agentcore/runner/drunner/dmessage_queue/...
```

Expected: 两个包 `coverage: ≥85%`

- [ ] **Step 4: 全量回归**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|ok.*message_queue|ok.*dmessage_queue)" | head -20
```

Expected: 无 `FAIL` 行；两个新包为 `ok`

- [ ] **Step 5: 回填 `IMPLEMENTATION_PLAN.md`**

把第 640 行：

```
| 9.83 | ☐ | Pulsar Message Queue | 分布式消息队列 | `openjiuwen/extensions/message_queue/` |
```

改为：

```
| 9.83 | ✅ | Pulsar Message Queue | Pulsar 后端（MessageQueuePulsar + PulsarSubscription + 3 窄接口 + producer LRU + 消费循环）；⤴️ 提前落位 9.84 的 `drunner/dmessage_queue/{message.go, message_serializer.go}`；⤵️ `runner.go` 步骤3/步骤1 两个预留点待 9.84 回填 | `openjiuwen/extensions/message_queue/` |
```

- [ ] **Step 6: 检查 doc.go 与磁盘一致**

```bash
cd /home/opensource/uapclaw-gateway
ls internal/agentcore/message_queue/ internal/agentcore/runner/drunner/dmessage_queue/
```

Expected: 确认两个 `doc.go` 的文件目录段列出的所有 `.go` 文件（不含 `_test.go`）与磁盘一致：`message_queue` 下 `doc.go`/`pulsar.go`/`pulsar_api.go`；`dmessage_queue` 下 `doc.go`/`message.go`/`message_serializer.go`

- [ ] **Step 7: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add IMPLEMENTATION_PLAN.md
git commit -m "chore(plan): 9.83 Pulsar Message Queue 标记完成

覆盖率达标，全量回归零 FAIL；⤵️ runner.go 步骤3/步骤1 待 9.84 回填。"
```

---

## 自审记录

**spec 覆盖核对**

| spec 章节 | 对应任务 |
|---|---|
| §3 Dmq 消息类型 + `QueueMessage` json tag | Task 1、Task 2 |
| §4.3 写出规则（`MessageToMap` + `serializePayload`） | Task 3 |
| §4.2 `SerializeMessage` | Task 4 |
| §4.4 `DeserializeMessage` | Task 5 |
| §5.2 窄接口 + SDK 适配器 | Task 6 |
| §5.3 Start/Stop | Task 7 |
| §5.4 Subscribe/Unsubscribe | Task 8 |
| §5.5 Produce + `getOrCreateProducer` LRU | Task 9 |
| §6.2 Activate/Deactivate | Task 10 |
| §6.3 consumeLoop | Task 11 |
| §7 日志对齐表 | Task 7/8/9/10/11 分散落位 |
| §8 文件组织 | Task 2/6/7 创建顺序 |
| §9 测试策略 | 每个 Task 的测试步骤 + Task 12 集成 |
| §10 偏差 D1–D9 | 代码注释中标注编号 |
| §11 回填标记 | Task 1（json tag）、Task 13（plan） |
| §12 验收标准 | Task 13 全部覆盖 |

**与 spec 的两处实现级修正**（已在本文档「SDK 校核结论」说明）

1. `newSDKClient` 返回 `(pulsarClientAPI, error)`，`Start()` 失败时记 Error 并保持 `running=false`（spec §5.3 未写错误分支，因接口无 error 返回）。
2. `PulsarSubscription` 增加 `stateMu` 保护 `cancel`/`done`（spec §6.1 未列该字段），避免 `Activate` 与 `Deactivate` 并发时的数据竞争。

**类型一致性**：`MessageToMap`/`SerializeMessage`/`DeserializeMessage` 三处签名在 Task 3/4/5 与 Task 9/11 的调用点一致；`pulsarProducerAPI.Send`、`pulsarConsumerAPI.Receive/Ack/Close`、`pulsarClientAPI.CreateProducer/Subscribe/Close` 在 Task 6 定义、Task 7–11 使用，名称与参数一致。
