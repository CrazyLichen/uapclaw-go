# 9.59b Interaction 层实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 TeamAgent 的 Interaction 层——团队 Agent 的「收件箱系统」，将用户自由文本或结构化输入正确路由到团队内部。

**Architecture:** 1:1 对齐 Python 目录结构，创建 `agent_teams/interaction/`（payload + router + user_inbox + human_agent_inbox）和 `agent_teams/runtime/`（gate + pool + manager）两个包。外部依赖用 `any` 占位 + 空 stub + 注释标注回填章节。InteractPayload 使用接口 + 类型断言 + Kind() 方法。

**Tech Stack:** Go 1.x, sync.Mutex/chan（InteractGate 并发原语）, regexp（路由解析）

**Design Spec:** `docs/superpowers/specs/2026-12-10-interaction-layer-9.59b-design.md`

---

## File Structure

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agent_teams/interaction/doc.go` | 包文档 |
| `internal/agent_teams/interaction/payload.go` | Payload 类型：GodViewMessage/OperatorMessage/HumanAgentMessage/InteractPayload 接口/DeliverResult/HumanAgentInboundEvent |
| `internal/agent_teams/interaction/payload_test.go` | Payload 类型测试 |
| `internal/agent_teams/interaction/router.go` | 输入解析：ParseInteractStr/ParseMention/IsReservedName/ResolveTargets/DeliverDirect |
| `internal/agent_teams/interaction/router_test.go` | 解析器测试 |
| `internal/agent_teams/interaction/user_inbox.go` | UserInbox 用户侧收件箱 |
| `internal/agent_teams/interaction/user_inbox_test.go` | UserInbox 测试 |
| `internal/agent_teams/interaction/human_agent_inbox.go` | HumanAgentInbox + 错误类型 |
| `internal/agent_teams/interaction/human_agent_inbox_test.go` | HumanAgentInbox 测试 |
| `internal/agent_teams/runtime/doc.go` | 包文档 |
| `internal/agent_teams/runtime/gate.go` | InteractGate 并发门控 |
| `internal/agent_teams/runtime/gate_test.go` | InteractGate 测试 |
| `internal/agent_teams/runtime/pool.go` | ActiveTeam/TeamRuntimePool |
| `internal/agent_teams/runtime/pool_test.go` | Pool 测试 |
| `internal/agent_teams/runtime/manager.go` | TeamRuntimeManager（interact 完整 + 其余空 stub） |
| `internal/agent_teams/runtime/manager_test.go` | Manager interact 测试 |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/agent_teams/doc.go` | 更新文件目录：interaction/ 和 runtime/ 条目去掉 ⤵️ 前缀 |
| `internal/agent_teams/agent/doc.go` | 更新文件目录：添加 interaction/ 和 runtime/ 相关说明 |
| `internal/agent_teams/agent/stream_controller.go` | 回填 pendingInterruptResumes 类型 + streamOneRound inputMap |
| `internal/agent_teams/agent/team_agent.go` | 回填 Interact()/Broadcast()/HumanAgentSay() |

---

## Task 1: interaction 包 — payload.go

**Files:**
- Create: `internal/agent_teams/interaction/doc.go`
- Create: `internal/agent_teams/interaction/payload.go`
- Create: `internal/agent_teams/interaction/payload_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package interaction 提供团队 Agent 的外部交互层。
//
// Interaction 层是 TeamAgent 对外暴露 interact() API 的核心实现——
// 团队 Agent 的「收件箱系统」。当外部用户有多种意图
// （直连 Leader、@某成员、广播、驱动 Human Agent Avatar）时，
// Interaction 层负责把自由文本或结构化输入正确路由到团队内部。
//
// 三种交互视角：
//   - God View — 直达 Leader DeepAgent（对齐历史 invoke 通道）
//   - Operator View — 以外部用户身份，@成员或广播
//   - Human-Agent View — 以注册的 human-agent 成员身份发言
//
// 文件目录：
//
//	interaction/
//	├── doc.go                # 包文档
//	├── payload.go            # 载荷类型（GodViewMessage/OperatorMessage/HumanAgentMessage/InteractPayload/DeliverResult/HumanAgentInboundEvent）
//	├── router.go             # 输入解析器（ParseInteractStr/ParseMention/IsReservedName/ResolveTargets/DeliverDirect）
//	├── user_inbox.go         # 用户侧收件箱（UserInbox）
//	└── human_agent_inbox.go  # Human-Agent 收件箱（HumanAgentInbox + 错误类型）
//
// 对应 Python 代码：openjiuwen/agent_teams/interaction/
package interaction
```

- [ ] **Step 2: 创建 payload.go**

```go
package interaction

import (
	"fmt"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PayloadKind 载荷类型枚举。
// 对齐 Python: isinstance(payload, GodViewMessage/OperatorMessage/HumanAgentMessage)
type PayloadKind int

// GodViewMessage 直达 Leader DeepAgent 的消息。
// 对齐 Python: GodViewMessage(body: str)
//
// 通道语义：`# body` 或裸文本 → 消息直送 Leader 的 DeepAgent，
// 等价于历史 invoke / deliver_to_leader 通道。
type GodViewMessage struct {
	// body 消息内容
	body string
}

// OperatorMessage 操作者视角消息（@成员/广播）。
// 对齐 Python: OperatorMessage(body: str, target: Optional[str])
//
// 通道语义：`@member body` → 点对点消息（target 为成员名）；
// `@all body` / `@* body` → 广播（target 为 nil）。
type OperatorMessage struct {
	// body 消息内容
	body string
	// target 目标成员名；nil 表示广播
	target *string
}

// HumanAgentMessage Human-Agent 成员消息。
// 对齐 Python: HumanAgentMessage(body: str, sender: str, target: Optional[str])
//
// 通道语义：`$name body` → 驱动 avatar（target 为 nil）；
// `$name @member body` → 点对点或广播。
type HumanAgentMessage struct {
	// body 消息内容
	body string
	// sender 发送者成员名
	sender string
	// target 目标成员名；nil 表示驱动 avatar；"*"/"all" 表示广播
	target *string
}

// DeliverResult 投递结果。
// 对齐 Python: DeliverResult(ok, message_id, reason)
//
// 成功时 OK 为 true 且 MessageID 携带消息 ID
// （deliver_to_leader 通道不产生 bus message，MessageID 为 nil）。
// 失败时 OK 为 false 且 Reason 携带短稳定 token。
type DeliverResult struct {
	// OK 是否成功
	OK bool
	// MessageID 消息 ID（可为 nil）
	MessageID *string
	// Reason 失败原因 token
	Reason *string
}

// HumanAgentInboundEvent 团队→用户通知事件。
// 对齐 Python: HumanAgentInboundEvent
//
// Phase-2 HITT 不会让 human-agent 的 LLM 自动消费入站消息——
// 消息直接透传给对应的外部用户。此结构体是运行时喂给
// HumanAgentInbox 注册的 on_inbound 回调的数据。
type HumanAgentInboundEvent struct {
	// MemberName 接收消息的 human-agent 成员名
	MemberName string
	// Sender 发送者成员名（或 "user" 伪成员）
	Sender string
	// Body 消息内容
	Body string
	// Broadcast 是否为广播消息
	Broadcast bool
	// MessageID 消息总线上的 ID
	MessageID string
	// Timestamp 毫秒级时间戳
	Timestamp int64
}

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// PayloadKindGodView God View 载荷——直连 Leader
	PayloadKindGodView PayloadKind = iota
	// PayloadKindOperator Operator 载荷——@成员/广播
	PayloadKindOperator
	// PayloadKindHumanAgent Human-Agent 载荷
	PayloadKindHumanAgent
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGodViewMessage 创建 GodView 消息。
func NewGodViewMessage(body string) *GodViewMessage {
	return &GodViewMessage{body: body}
}

// NewOperatorMessage 创建 Operator 消息。
// target 为 nil 表示广播。
func NewOperatorMessage(body string, target *string) *OperatorMessage {
	return &OperatorMessage{body: body, target: target}
}

// NewHumanAgentMessage 创建 Human-Agent 消息。
// target 为 nil 表示驱动 avatar。
func NewHumanAgentMessage(body string, sender string, target *string) *HumanAgentMessage {
	return &HumanAgentMessage{body: body, sender: sender, target: target}
}

// NewDeliverResultSuccess 创建成功的投递结果。
// 对齐 Python: DeliverResult.success(message_id)
func NewDeliverResultSuccess(messageID *string) *DeliverResult {
	return &DeliverResult{OK: true, MessageID: messageID}
}

// NewDeliverResultFailure 创建失败的投递结果。
// 对齐 Python: DeliverResult.failure(reason)
func NewDeliverResultFailure(reason string) *DeliverResult {
	return &DeliverResult{OK: false, Reason: &reason}
}

// ──────────────────────────── InteractPayload 接口实现 ────────────────────────────

// Kind 返回载荷类型。
func (g *GodViewMessage) Kind() PayloadKind { return PayloadKindGodView }

// Body 返回消息内容。
func (g *GodViewMessage) Body() string { return g.body }

// Kind 返回载荷类型。
func (o *OperatorMessage) Kind() PayloadKind { return PayloadKindOperator }

// Body 返回消息内容。
func (o *OperatorMessage) Body() string { return o.body }

// Target 返回目标成员名（nil 表示广播）。
func (o *OperatorMessage) Target() *string { return o.target }

// Kind 返回载荷类型。
func (h *HumanAgentMessage) Kind() PayloadKind { return PayloadKindHumanAgent }

// Body 返回消息内容。
func (h *HumanAgentMessage) Body() string { return h.body }

// Sender 返回发送者成员名。
func (h *HumanAgentMessage) Sender() string { return h.sender }

// Target 返回目标成员名。
func (h *HumanAgentMessage) Target() *string { return h.target }

// ──────────── DeliverResult 方法 ────────────

// IsOK 判断投递是否成功。
// 对齐 Python: DeliverResult.__bool__
func (d *DeliverResult) IsOK() bool { return d.OK }

// String 返回可读描述。
func (d *DeliverResult) String() string {
	if d.OK {
		if d.MessageID != nil {
			return fmt.Sprintf("DeliverResult(ok=true, message_id=%s)", *d.MessageID)
		}
		return "DeliverResult(ok=true, message_id=nil)"
	}
	if d.Reason != nil {
		return fmt.Sprintf("DeliverResult(ok=false, reason=%s)", *d.Reason)
	}
	return "DeliverResult(ok=false)"
}

// String 返回载荷类型的可读名称。
func (k PayloadKind) String() string {
	return payloadKindName(k)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// payloadKindName 载荷类型名称（用于日志）。
func payloadKindName(kind PayloadKind) string {
	switch kind {
	case PayloadKindGodView:
		return "GodView"
	case PayloadKindOperator:
		return "Operator"
	case PayloadKindHumanAgent:
		return "HumanAgent"
	default:
		return "Unknown"
	}
}

// isReservedMemberName 检查是否为运行时保留成员名。
// 对齐 Python: is_reserved_name(name)
func isReservedMemberName(name string) bool {
	return agentteams.ReservedMemberNames[name]
}
```

- [ ] **Step 3: 创建 payload_test.go**

```go
package interaction

import (
	"testing"
)

func TestNewGodViewMessage(t *testing.T) {
	msg := NewGodViewMessage("hello")
	if msg.Kind() != PayloadKindGodView {
		t.Errorf("Kind() = %v, want PayloadKindGodView", msg.Kind())
	}
	if msg.Body() != "hello" {
		t.Errorf("Body() = %v, want hello", msg.Body())
	}
}

func TestNewOperatorMessage_广播(t *testing.T) {
	msg := NewOperatorMessage("hi all", nil)
	if msg.Kind() != PayloadKindOperator {
		t.Errorf("Kind() = %v, want PayloadKindOperator", msg.Kind())
	}
	if msg.Body() != "hi all" {
		t.Errorf("Body() = %v, want hi all", msg.Body())
	}
	if msg.Target() != nil {
		t.Errorf("Target() = %v, want nil", msg.Target())
	}
}

func TestNewOperatorMessage_点对点(t *testing.T) {
	target := "alice"
	msg := NewOperatorMessage("hi", &target)
	if msg.Target() == nil || *msg.Target() != "alice" {
		t.Errorf("Target() = %v, want alice", msg.Target())
	}
}

func TestNewHumanAgentMessage(t *testing.T) {
	target := "bob"
	msg := NewHumanAgentMessage("hello", "human_agent", &target)
	if msg.Kind() != PayloadKindHumanAgent {
		t.Errorf("Kind() = %v, want PayloadKindHumanAgent", msg.Kind())
	}
	if msg.Sender() != "human_agent" {
		t.Errorf("Sender() = %v, want human_agent", msg.Sender())
	}
	if msg.Target() == nil || *msg.Target() != "bob" {
		t.Errorf("Target() = %v, want bob", msg.Target())
	}
}

func TestNewHumanAgentMessage_驱动avatar(t *testing.T) {
	msg := NewHumanAgentMessage("hello", "human_agent", nil)
	if msg.Target() != nil {
		t.Errorf("Target() = %v, want nil (drive avatar)", msg.Target())
	}
}

func TestDeliverResult_成功(t *testing.T) {
	id := "msg-123"
	r := NewDeliverResultSuccess(&id)
	if !r.IsOK() {
		t.Error("IsOK() = false, want true")
	}
	if r.MessageID == nil || *r.MessageID != "msg-123" {
		t.Errorf("MessageID = %v, want msg-123", r.MessageID)
	}
	if r.Reason != nil {
		t.Errorf("Reason = %v, want nil", r.Reason)
	}
}

func TestDeliverResult_成功无消息ID(t *testing.T) {
	r := NewDeliverResultSuccess(nil)
	if !r.IsOK() {
		t.Error("IsOK() = false, want true")
	}
	if r.MessageID != nil {
		t.Errorf("MessageID = %v, want nil", r.MessageID)
	}
}

func TestDeliverResult_失败(t *testing.T) {
	r := NewDeliverResultFailure("gate_closed")
	if r.IsOK() {
		t.Error("IsOK() = true, want false")
	}
	if r.Reason == nil || *r.Reason != "gate_closed" {
		t.Errorf("Reason = %v, want gate_closed", r.Reason)
	}
}

func TestDeliverResult_String(t *testing.T) {
	id := "msg-1"
	r1 := NewDeliverResultSuccess(&id)
	if r1.String() != "DeliverResult(ok=true, message_id=msg-1)" {
		t.Errorf("String() = %v", r1.String())
	}

	r2 := NewDeliverResultSuccess(nil)
	if r2.String() != "DeliverResult(ok=true, message_id=nil)" {
		t.Errorf("String() = %v", r2.String())
	}

	r3 := NewDeliverResultFailure("gate_closed")
	if r3.String() != "DeliverResult(ok=false, reason=gate_closed)" {
		t.Errorf("String() = %v", r3.String())
	}
}

func TestPayloadKindName(t *testing.T) {
	tests := []struct {
		kind PayloadKind
		want string
	}{
		{PayloadKindGodView, "GodView"},
		{PayloadKindOperator, "Operator"},
		{PayloadKindHumanAgent, "HumanAgent"},
		{PayloadKind(99), "Unknown"},
	}
	for _, tt := range tests {
		got := payloadKindName(tt.kind)
		if got != tt.want {
			t.Errorf("payloadKindName(%v) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestIsReservedMemberName(t *testing.T) {
	if !isReservedMemberName("user") {
		t.Error("user should be reserved")
	}
	if !isReservedMemberName("team_leader") {
		t.Error("team_leader should be reserved")
	}
	if !isReservedMemberName("human_agent") {
		t.Error("human_agent should be reserved")
	}
	if isReservedMemberName("alice") {
		t.Error("alice should not be reserved")
	}
}

func TestInteractPayload接口(t *testing.T) {
	// 确保三种类型都满足 InteractPayload 接口
	var _ InteractPayload = NewGodViewMessage("test")
	var _ InteractPayload = NewOperatorMessage("test", nil)
	var _ InteractPayload = NewHumanAgentMessage("test", "sender", nil)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/interaction/ -v -run "TestNew|TestDeliverResult|TestPayloadKindName|TestIsReservedMemberName|TestInteractPayload" -count=1`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/interaction/doc.go internal/agent_teams/interaction/payload.go internal/agent_teams/interaction/payload_test.go
git commit -m "feat(interaction): 添加 payload 类型 — GodViewMessage/OperatorMessage/HumanAgentMessage/DeliverResult (9.59b)"
```

---

## Task 2: interaction 包 — router.go

**Files:**
- Create: `internal/agent_teams/interaction/router.go`
- Create: `internal/agent_teams/interaction/router_test.go`

- [ ] **Step 1: 创建 router.go**

```go
package interaction

import (
	"regexp"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mentionRe @target body 正则
	// 对齐 Python: _MENTION_RE = re.compile(r"^@(\S+)\s+([\s\S]+)$")
	mentionRe = regexp.MustCompile(`^@(\S+)\s+([\s\S]+)$`)
	// humanAgentPrefixRe $name 前缀正则
	// 对齐 Python: _HUMAN_AGENT_PREFIX_RE = re.compile(r"^\$([^\s@]+)(?:\s+|(?=@))([\s\S]*)$")
	humanAgentPrefixRe = regexp.MustCompile(`^\$([^\s@]+)(?:\s+|(?=@))([\s\S]*)$`)
	// recipientRe @name 后续匹配正则
	// 对齐 Python: _RECIPIENT_RE = re.compile(r"^@(\S+)\s+")
	recipientRe = regexp.MustCompile(`^@(\S+)\s+`)
	// BroadcastTargets 广播目标集合
	// 对齐 Python: BROADCAST_TARGETS = frozenset({"all", "*"})
	BroadcastTargets = map[string]bool{"all": true, "*": true}
	// routerLogComponent 日志组件
	routerLogComponent = logger.ComponentChannel
)

// MemberExistsCheck 成员存在性检查函数类型。
// 对齐 Python: MemberExistsCheck = Callable[[str], Awaitable[bool]]
type MemberExistsCheck func(name string) (bool, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseInteractStr 将自由文本解析为 InteractPayload 列表。
// 对齐 Python: parse_interact_str(body)
//
// 语法规则：
//
//	input := channel? recipients? body
//	channel := "# " | "$" name (" " | "@")    // 默认 "# "
//	recipients := ("@" name " ")*
//	body := <remaining text>
//
// 产出的载荷列表：
//   - 空/纯空格输入 → 空列表
//   - GodViewMessage — `# body`（无 @recipient），或裸文本
//   - HumanAgentMessage — `$name body`（无 recipient），驱动 avatar
//   - OperatorMessage(target=nil) — `# @all/@* body`，广播
//   - HumanAgentMessage(target="*") — `$name @all/@* body`，广播
//   - []OperatorMessage — `# @m1 @m2 body`，每个 recipient 一个
//   - []HumanAgentMessage — `$name @m1 @m2 body`，每个 recipient 一个
func ParseInteractStr(body string) []InteractPayload {
	if body == "" || len(body) == 0 {
		return nil
	}
	// 检查是否纯空格
	allSpace := true
	for _, r := range body {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			allSpace = false
			break
		}
	}
	if allSpace {
		return nil
	}

	rest := body
	sender := agentteams.UserPseudoMemberName
	isHumanAgent := false

	// ---- 通道前缀 ----
	if len(rest) >= 2 && rest[:2] == "# " {
		rest = rest[2:]
		// 去除前导空格
		rest = trimLeadingSpaces(rest)
	} else {
		match := humanAgentPrefixRe.FindStringSubmatch(rest)
		if match != nil {
			sender = match[1]
			rest = match[2]
			rest = trimLeadingSpaces(rest)
			isHumanAgent = true
		}
		// else: 无识别前缀 → 视为 "# " 默认
	}

	// ---- 接收者 ----
	var recipients []string
	for {
		match := recipientRe.FindStringSubmatch(rest)
		if match == nil {
			break
		}
		recipients = append(recipients, match[1])
		rest = rest[len(match[0]):]
	}

	finalBody := rest

	// ---- 载荷合成 ----
	if len(recipients) == 0 {
		if isHumanAgent {
			return []InteractPayload{NewHumanAgentMessage(finalBody, sender, nil)}
		}
		return []InteractPayload{NewGodViewMessage(finalBody)}
	}

	hasBroadcast := false
	for _, r := range recipients {
		if BroadcastTargets[r] {
			hasBroadcast = true
			break
		}
	}

	if hasBroadcast {
		// 广播覆盖所有其他命名接收者
		if isHumanAgent {
			broadcastTarget := "*"
			return []InteractPayload{NewHumanAgentMessage(finalBody, sender, &broadcastTarget)}
		}
		return []InteractPayload{NewOperatorMessage(finalBody, nil)}
	}

	if isHumanAgent {
		result := make([]InteractPayload, len(recipients))
		for i, name := range recipients {
			target := name
			result[i] = NewHumanAgentMessage(finalBody, sender, &target)
		}
		return result
	}

	result := make([]InteractPayload, len(recipients))
	for i, name := range recipients {
		target := name
		result[i] = NewOperatorMessage(finalBody, &target)
	}
	return result
}

// ParseMention 解析单个 @target body。
// 对齐 Python: parse_mention(content)
// 返回 (target, body, true) 匹配成功；("","",false) 无匹配。
func ParseMention(content string) (target string, body string, ok bool) {
	if content == "" {
		return "", "", false
	}
	match := mentionRe.FindStringSubmatch(content)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

// IsReservedName 检查是否为运行时保留成员名。
// 对齐 Python: is_reserved_name(name)
func IsReservedName(name string) bool {
	return agentteams.ReservedMemberNames[name]
}

// ResolveTargets 校验 @<member> 接收者是否在花名册中。
// 对齐 Python: resolve_targets(payloads, *, member_exists)
//
// 已知接收者保留原载荷；未知接收者的 @提及折回到一条无提及消息；
// God View / Avatar 驱动 / 广播载荷不携带命名目标，直接透传。
func ResolveTargets(payloads []InteractPayload, memberExists MemberExistsCheck) ([]InteractPayload, error) {
	var unknown []InteractPayload
	var kept []InteractPayload

	for _, p := range payloads {
		name := namedTarget(p)
		if name == nil {
			kept = append(kept, p)
			continue
		}
		exists, err := memberExists(*name)
		if err != nil {
			return nil, err
		}
		if exists {
			kept = append(kept, p)
		} else {
			unknown = append(unknown, p)
		}
	}

	if len(unknown) == 0 {
		return payloads, nil
	}
	return append(kept, foldUnknownMentions(unknown)), nil
}

// DeliverDirect 验证 target 并发送点对点消息。
// 对齐 Python: deliver_direct(body, *, sender, target, message_manager, member_exists)
//
// messageManager 为 any 占位：
//
//	⤵️ 待 9.55 回填: TeamMessageManager — 调用 send_message(content, to_member_name, from_member_name)
func DeliverDirect(body string, sender string, target string, messageManager any, memberExists MemberExistsCheck) (*DeliverResult, error) {
	exists, err := memberExists(target)
	if err != nil {
		return nil, err
	}
	if !exists {
		reason := "unknown_member:" + target
		return NewDeliverResultFailure(reason), nil
	}
	// ⤵️ 待 9.55 回填: messageManager.SendMessage(content=body, to=target, from=sender)
	// 当前 stub: 模拟成功
	logger.Debug(routerLogComponent).Str("sender", sender).Str("target", target).
		Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("DeliverDirect (stub)")
	msgID := "stub-msg-id"
	return NewDeliverResultSuccess(&msgID), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// namedTarget 提取载荷中的点对点接收者。
// 对齐 Python: _named_target(payload)
// 返回 nil 表示无命名目标（God View / Avatar 驱动 / 广播）。
func namedTarget(payload InteractPayload) *string {
	switch p := payload.(type) {
	case *OperatorMessage:
		t := p.Target()
		if t == nil || BroadcastTargets[*t] {
			return nil
		}
		return t
	case *HumanAgentMessage:
		t := p.Target()
		if t == nil || BroadcastTargets[*t] {
			return nil
		}
		return t
	default:
		return nil
	}
}

// foldUnknownMentions 折叠未知 @提及到一条无提及消息。
// 对齐 Python: _fold_unknown_mentions(unknown)
func foldUnknownMentions(unknown []InteractPayload) InteractPayload {
	sample := unknown[0]
	// 重建 @提及文本
	mentions := ""
	for i, p := range unknown {
		if i > 0 {
			mentions += " "
		}
		mentions += "@" + *namedTarget(p)
	}
	generalBody := mentions + " " + sample.Body()
	if sample.Body() == "" {
		generalBody = mentions
	}

	switch s := sample.(type) {
	case *HumanAgentMessage:
		return NewHumanAgentMessage(generalBody, s.Sender(), nil)
	default:
		return NewGodViewMessage(generalBody)
	}
}

// trimLeadingSpaces 去除前导空白。
func trimLeadingSpaces(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return s[i:]
		}
	}
	return ""
}
```

**注意**: `router.go` 中 `DeliverDirect` 使用了 `fmt.Sprintf`，需要在 import 中加入 `"fmt"`。

- [ ] **Step 2: 创建 router_test.go**

```go
package interaction

import (
	"errors"
	"testing"
)

// ──────────── ParseInteractStr 测试 ────────────

func TestParseInteractStr_空输入(t *testing.T) {
	result := ParseInteractStr("")
	if result != nil {
		t.Errorf("空输入应返回 nil, got %v", result)
	}
}

func TestParseInteractStr_纯空格(t *testing.T) {
	result := ParseInteractStr("   \t\n  ")
	if result != nil {
		t.Errorf("纯空格应返回 nil, got %v", result)
	}
}

func TestParseInteractStr_裸文本默认GodView(t *testing.T) {
	result := ParseInteractStr("hello world")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*GodViewMessage)
	if !ok {
		t.Fatal("应为 GodViewMessage")
	}
	if msg.Body() != "hello world" {
		t.Errorf("Body = %v, want hello world", msg.Body())
	}
}

func TestParseInteractStr_井号前缀(t *testing.T) {
	result := ParseInteractStr("# hello leader")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*GodViewMessage)
	if !ok {
		t.Fatal("应为 GodViewMessage")
	}
	if msg.Body() != "hello leader" {
		t.Errorf("Body = %v, want hello leader", msg.Body())
	}
}

func TestParseInteractStr_美元前缀驱动avatar(t *testing.T) {
	result := ParseInteractStr("$human_agent do something")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*HumanAgentMessage)
	if !ok {
		t.Fatal("应为 HumanAgentMessage")
	}
	if msg.Sender() != "human_agent" {
		t.Errorf("Sender = %v, want human_agent", msg.Sender())
	}
	if msg.Target() != nil {
		t.Errorf("Target = %v, want nil (drive avatar)", msg.Target())
	}
	if msg.Body() != "do something" {
		t.Errorf("Body = %v, want do something", msg.Body())
	}
}

func TestParseInteractStr_at成员点对点(t *testing.T) {
	result := ParseInteractStr("@alice hello")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("应为 OperatorMessage")
	}
	if msg.Target() == nil || *msg.Target() != "alice" {
		t.Errorf("Target = %v, want alice", msg.Target())
	}
	if msg.Body() != "hello" {
		t.Errorf("Body = %v, want hello", msg.Body())
	}
}

func TestParseInteractStr_井号加广播(t *testing.T) {
	result := ParseInteractStr("# @all attention please")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("应为 OperatorMessage")
	}
	if msg.Target() != nil {
		t.Errorf("Target = %v, want nil (broadcast)", msg.Target())
	}
	if msg.Body() != "attention please" {
		t.Errorf("Body = %v, want attention please", msg.Body())
	}
}

func TestParseInteractStr_星号广播(t *testing.T) {
	result := ParseInteractStr("# @* broadcast msg")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("应为 OperatorMessage")
	}
	if msg.Target() != nil {
		t.Errorf("Target = %v, want nil (broadcast)", msg.Target())
	}
}

func TestParseInteractStr_多接收者(t *testing.T) {
	result := ParseInteractStr("@alice @bob hello team")
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	msg1, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("result[0] 应为 OperatorMessage")
	}
	if msg1.Target() == nil || *msg1.Target() != "alice" {
		t.Errorf("result[0] Target = %v, want alice", msg1.Target())
	}
	msg2, ok := result[1].(*OperatorMessage)
	if !ok {
		t.Fatal("result[1] 应为 OperatorMessage")
	}
	if msg2.Target() == nil || *msg2.Target() != "bob" {
		t.Errorf("result[1] Target = %v, want bob", msg2.Target())
	}
}

func TestParseInteractStr_美元前缀加接收者(t *testing.T) {
	result := ParseInteractStr("$human_agent @alice hello")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*HumanAgentMessage)
	if !ok {
		t.Fatal("应为 HumanAgentMessage")
	}
	if msg.Sender() != "human_agent" {
		t.Errorf("Sender = %v, want human_agent", msg.Sender())
	}
	if msg.Target() == nil || *msg.Target() != "alice" {
		t.Errorf("Target = %v, want alice", msg.Target())
	}
}

func TestParseInteractStr_美元前缀广播(t *testing.T) {
	result := ParseInteractStr("$human_agent @all hello")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*HumanAgentMessage)
	if !ok {
		t.Fatal("应为 HumanAgentMessage")
	}
	if msg.Target() == nil || *msg.Target() != "*" {
		t.Errorf("Target = %v, want * (broadcast)", msg.Target())
	}
}

func TestParseInteractStr_井号加at成员(t *testing.T) {
	result := ParseInteractStr("# @alice hello")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	msg, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("应为 OperatorMessage")
	}
	if msg.Target() == nil || *msg.Target() != "alice" {
		t.Errorf("Target = %v, want alice", msg.Target())
	}
	if msg.Body() != "hello" {
		t.Errorf("Body = %v, want hello", msg.Body())
	}
}

// ──────────── ParseMention 测试 ────────────

func TestParseMention_成功(t *testing.T) {
	target, body, ok := ParseMention("@alice hello")
	if !ok {
		t.Fatal("应匹配成功")
	}
	if target != "alice" {
		t.Errorf("target = %v, want alice", target)
	}
	if body != "hello" {
		t.Errorf("body = %v, want hello", body)
	}
}

func TestParseMention_无匹配(t *testing.T) {
	_, _, ok := ParseMention("no mention here")
	if ok {
		t.Error("不应匹配")
	}
}

func TestParseMention_空输入(t *testing.T) {
	_, _, ok := ParseMention("")
	if ok {
		t.Error("空输入不应匹配")
	}
}

// ──────────── IsReservedName 测试 ────────────

func TestIsReservedName(t *testing.T) {
	if !IsReservedName("user") {
		t.Error("user 应为保留名")
	}
	if !IsReservedName("team_leader") {
		t.Error("team_leader 应为保留名")
	}
	if !IsReservedName("human_agent") {
		t.Error("human_agent 应为保留名")
	}
	if IsReservedName("alice") {
		t.Error("alice 不应为保留名")
	}
}

// ──────────── ResolveTargets 测试 ────────────

func TestResolveTargets_全部已知(t *testing.T) {
	payloads := []InteractPayload{
		NewOperatorMessage("hi", strPtr("alice")),
		NewOperatorMessage("hi", strPtr("bob")),
	}
	check := func(name string) (bool, error) { return true, nil }
	result, err := ResolveTargets(payloads, check)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
}

func TestResolveTargets_未知接收者折叠(t *testing.T) {
	payloads := []InteractPayload{
		NewOperatorMessage("hi", strPtr("alice")),
		NewOperatorMessage("hi", strPtr("ghost")),
	}
	check := func(name string) (bool, error) { return name == "alice", nil }
	result, err := ResolveTargets(payloads, check)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2 (kept + folded)", len(result))
	}
	// 第一个是已知 alice
	op1, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("result[0] 应为 OperatorMessage")
	}
	if op1.Target() == nil || *op1.Target() != "alice" {
		t.Errorf("result[0] Target = %v, want alice", op1.Target())
	}
	// 第二个是折叠后的 GodViewMessage
	gv, ok := result[1].(*GodViewMessage)
	if !ok {
		t.Fatal("result[1] 应为折叠后的 GodViewMessage")
	}
	if gv.Body() != "@ghost hi" {
		t.Errorf("folded Body = %v, want @ghost hi", gv.Body())
	}
}

func TestResolveTargets_GodView透传(t *testing.T) {
	payloads := []InteractPayload{NewGodViewMessage("hello")}
	check := func(name string) (bool, error) { return false, nil }
	result, err := ResolveTargets(payloads, check)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if _, ok := result[0].(*GodViewMessage); !ok {
		t.Error("GodViewMessage 应透传")
	}
}

func TestResolveTargets_检查函数报错(t *testing.T) {
	payloads := []InteractPayload{NewOperatorMessage("hi", strPtr("alice"))}
	check := func(name string) (bool, error) { return false, errors.New("db error") }
	_, err := ResolveTargets(payloads, check)
	if err == nil {
		t.Error("应返回错误")
	}
}

// ──────────── 辅助函数 ────────────

func strPtr(s string) *string { return &s }
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/interaction/ -v -run "TestParseInteractStr|TestParseMention|TestIsReservedName|TestResolveTargets" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/interaction/router.go internal/agent_teams/interaction/router_test.go
git commit -m "feat(interaction): 添加 router 解析器 — ParseInteractStr/ParseMention/ResolveTargets (9.59b)"
```

---

## Task 3: interaction 包 — user_inbox.go

**Files:**
- Create: `internal/agent_teams/interaction/user_inbox.go`
- Create: `internal/agent_teams/interaction/user_inbox_test.go`

- [ ] **Step 1: 创建 user_inbox.go**

```go
package interaction

import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserInbox 用户侧收件箱，将外部输入路由到团队运行时。
// 对齐 Python: UserInbox (openjiuwen/agent_teams/interaction/user_inbox.py)
//
// 三种入口：
//   - DeliverToLeader — 纯文本直达 Leader DeepAgent（保留历史 invoke 语义）
//   - Direct — @member_name body 点对点消息
//   - Broadcast — 团队范围广播
//
// 所有路径通过 TeamMessageManager，消息最终存储在队友间流量的同一数据源。
type UserInbox struct {
	// messageManager 消息管理器
	// ⤵️ 待 9.55 回填: TeamMessageManager — 提供 SendMessage/BroadcastMessage 方法
	messageManager any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// inboxLogComponent 日志组件
	inboxLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserInbox 创建用户收件箱。
func NewUserInbox(messageManager any) *UserInbox {
	return &UserInbox{messageManager: messageManager}
}

// Direct 发送 @target body 点对点消息。
// 对齐 Python: UserInbox.direct(target, body)
//
// ⤵️ 待 9.55 回填: 调用 messageManager.SendMessage(content=body, to=target, from="user")
func (u *UserInbox) Direct(target string, body string) (*DeliverResult, error) {
	logger.Debug(inboxLogComponent).Str("target", target).
		Str("from", agentteams.UserPseudoMemberName).
		Str("body_len", formatLen(body)).
		Msg("UserInbox.Direct (stub)")
	// ⤵️ 待 9.55 回填: msgID := u.messageManager.SendMessage(body, target, agentteams.UserPseudoMemberName)
	// 当前 stub: 模拟成功
	msgID := "stub-direct-msg-id"
	return NewDeliverResultSuccess(&msgID), nil
}

// Broadcast 广播用户侧公告。
// 对齐 Python: UserInbox.broadcast(body)
//
// ⤵️ 待 9.55 回填: 调用 messageManager.BroadcastMessage(content=body, from="user")
func (u *UserInbox) Broadcast(body string) (*DeliverResult, error) {
	logger.Debug(inboxLogComponent).Str("from", agentteams.UserPseudoMemberName).
		Str("body_len", formatLen(body)).
		Msg("UserInbox.Broadcast (stub)")
	// ⤵️ 待 9.55 回填: msgID := u.messageManager.BroadcastMessage(body, agentteams.UserPseudoMemberName)
	// 当前 stub: 模拟成功
	msgID := "stub-broadcast-msg-id"
	return NewDeliverResultSuccess(&msgID), nil
}

// DeliverToLeader 将输入投递到 Leader DeepAgent。
// 对齐 Python: UserInbox.deliver_to_leader(deliver_input, body)
//
// 此通道不产生 bus message ID，成功时 MessageID 为 nil。
func DeliverToLeader(deliverInput func(ctx context.Context, content string) error, body string) (*DeliverResult, error) {
	logger.Debug(inboxLogComponent).Str("body_len", formatLen(body)).
		Msg("DeliverToLeader")
	if deliverInput == nil {
		return NewDeliverResultFailure("deliver_to_leader_failed:no_deliver_fn"), nil
	}
	ctx := context.Background()
	if err := deliverInput(ctx, body); err != nil {
		reason := "deliver_to_leader_failed:" + err.Error()
		return NewDeliverResultFailure(reason), nil
	}
	return NewDeliverResultSuccess(nil), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatLen 格式化字符串长度。
func formatLen(s string) string {
	return fmt.Sprintf("%d", len(s))
}
```

**注意**: 需要添加 `"fmt"` 到 import。

- [ ] **Step 2: 创建 user_inbox_test.go**

```go
package interaction

import (
	"context"
	"errors"
	"testing"
)

func TestNewUserInbox(t *testing.T) {
	u := NewUserInbox(nil)
	if u == nil {
		t.Error("NewUserInbox 应返回非 nil")
	}
}

func TestUserInbox_Direct_成功(t *testing.T) {
	u := NewUserInbox(nil)
	result, err := u.Direct("alice", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestUserInbox_Broadcast_成功(t *testing.T) {
	u := NewUserInbox(nil)
	result, err := u.Broadcast("hello all")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestDeliverToLeader_成功(t *testing.T) {
	var received string
	deliverInput := func(ctx context.Context, content string) error {
		received = content
		return nil
	}
	result, err := DeliverToLeader(deliverInput, "hello leader")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	if result.MessageID != nil {
		t.Errorf("MessageID = %v, want nil (deliver_to_leader 不产生 bus message)", result.MessageID)
	}
	if received != "hello leader" {
		t.Errorf("received = %v, want hello leader", received)
	}
}

func TestDeliverToLeader_投递失败(t *testing.T) {
	deliverInput := func(ctx context.Context, content string) error {
		return errors.New("agent busy")
	}
	result, err := DeliverToLeader(deliverInput, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("IsOK 应为 false")
	}
	if result.Reason == nil {
		t.Error("Reason 不应为 nil")
	}
}

func TestDeliverToLeader_nil回调(t *testing.T) {
	result, err := DeliverToLeader(nil, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("IsOK 应为 false（无回调）")
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/interaction/ -v -run "TestNewUserInbox|TestUserInbox|TestDeliverToLeader" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/interaction/user_inbox.go internal/agent_teams/interaction/user_inbox_test.go
git commit -m "feat(interaction): 添加 UserInbox 用户侧收件箱 (9.59b)"
```

---

## Task 4: interaction 包 — human_agent_inbox.go

**Files:**
- Create: `internal/agent_teams/interaction/human_agent_inbox.go`
- Create: `internal/agent_teams/interaction/human_agent_inbox_test.go`

- [ ] **Step 1: 创建 human_agent_inbox.go**

```go
package interaction

import (
	"fmt"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HumanAgentNotEnabledError 团队未注册 human-agent 成员时抛出。
// 对齐 Python: HumanAgentNotEnabledError
type HumanAgentNotEnabledError struct {
	// Message 错误描述
	Message string
}

// UnknownHumanAgentError 发送者不是已注册的 human-agent 成员时抛出。
// 对齐 Python: UnknownHumanAgentError
type UnknownHumanAgentError struct {
	// Sender 尝试使用的发送者名
	Sender string
	// Registered 已注册的 human-agent 成员名列表
	Registered []string
}

// HumanAgentInbox Human-Agent 收件箱，路由 human-agent 输入。
// 对齐 Python: HumanAgentInbox (openjiuwen/agent_teams/interaction/human_agent_inbox.py)
//
// 路由规则：
//   - to == nil → 驱动 avatar 的 DeepAgent
//   - to in {"all", "*"} → 广播
//   - to == "member" → 验证目标后发送点对点消息
type HumanAgentInbox struct {
	// team 团队后端
	// ⤵️ 待 9.55 回填: TeamBackend — 提供 HumanAgentNames/GetMember 方法
	team any
	// messageManager 消息管理器
	// ⤵️ 待 9.55 回填: TeamMessageManager — 提供 SendMessage/BroadcastMessage 方法
	messageManager any
	// agentLookup 解析 human-agent 成员名到活跃 TeamAgent
	agentLookup AgentLookup
	// onInbound 团队→用户通知回调
	onInbound OnInbound
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentLookup 解析 human-agent 成员名到活跃 TeamAgent 运行时。
// 对齐 Python: AgentLookup = Callable[[str], Optional[TeamAgent]]
// ⤵️ 待 9.55 回填: 返回 *TeamAgent
type AgentLookup func(sender string) any

// OnInbound 团队→用户通知回调。
// 对齐 Python: OnInbound = Callable[[HumanAgentInboundEvent], Awaitable[None]]
type OnInbound func(event HumanAgentInboundEvent) error

// NewHumanAgentInbox 创建 Human-Agent 收件箱。
func NewHumanAgentInbox(team any, messageManager any, agentLookup AgentLookup, onInbound OnInbound) *HumanAgentInbox {
	return &HumanAgentInbox{
		team:           team,
		messageManager: messageManager,
		agentLookup:    agentLookup,
		onInbound:      onInbound,
	}
}

// Send 分发已解析的 human-agent 载荷。
// 对齐 Python: HumanAgentInbox.send(body, to, sender)
//
// 路由：
//   - to == nil → 驱动 avatar DeepAgent
//   - to in {"all", "*"} → 广播
//   - to == "member" → 验证 + 点对点
func (h *HumanAgentInbox) Send(body string, to *string, sender *string) (*DeliverResult, error) {
	resolvedSender, err := h.resolveSender(sender)
	if err != nil {
		return nil, err
	}

	logger.Debug(inboxLogComponent).Str("sender", resolvedSender).
		Str("to", ptrStrOr(to, "<avatar>")).
		Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("HumanAgentInbox.Send")

	if to == nil {
		return h.driveAgent(body, resolvedSender)
	}
	if BroadcastTargets[*to] {
		// ⤵️ 待 9.55 回填: messageManager.BroadcastMessage(body, resolvedSender)
		msgID := "stub-ha-broadcast-msg-id"
		return NewDeliverResultSuccess(&msgID), nil
	}
	// 点对点
	return DeliverDirect(body, resolvedSender, *to, h.messageManager, h.memberExists)
}

// GetOnInbound 返回团队→用户通知回调。
func (h *HumanAgentInbox) GetOnInbound() OnInbound {
	return h.onInbound
}

// Error 接口实现
func (e *HumanAgentNotEnabledError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "no human-agent member is registered on this team"
}

// Error 接口实现
func (e *UnknownHumanAgentError) Error() string {
	return fmt.Sprintf("'%s' is not a registered human-agent member; registered members: %v",
		e.Sender, e.Registered)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSender 解析并验证发送者。
// 对齐 Python: HumanAgentInbox._resolve_sender(sender)
//
// sender 为 nil 时默认选择第一个注册的 human-agent 成员。
// ⤵️ 待 9.55 回填: 调用 team.HumanAgentNames() 获取已注册成员列表
func (h *HumanAgentInbox) resolveSender(sender *string) (string, error) {
	// ⤵️ 待 9.55 回填: names := h.team.HumanAgentNames()
	// 当前 stub: 使用默认 human_agent 名称
	names := []string{agentteams.HumanAgentMemberName}

	if len(names) == 0 {
		return "", &HumanAgentNotEnabledError{}
	}
	if sender == nil {
		// 确定性默认：优先使用保留名，否则按字典序第一个
		for _, n := range names {
			if n == agentteams.HumanAgentMemberName {
				return n, nil
			}
		}
		return names[0], nil
	}
	for _, n := range names {
		if n == *sender {
			return *sender, nil
		}
	}
	return "", &UnknownHumanAgentError{Sender: *sender, Registered: names}
}

// driveAgent 驱动 avatar DeepAgent。
// 对齐 Python: HumanAgentInbox._drive_agent(body, sender)
func (h *HumanAgentInbox) driveAgent(body string, sender string) (*DeliverResult, error) {
	if h.agentLookup == nil {
		logger.Warn(inboxLogComponent).Str("sender", sender).
			Msg("HumanAgentInbox: no agent_lookup wired; cannot deliver input")
		return NewDeliverResultFailure("agent_unavailable"), nil
	}
	agent := h.agentLookup(sender)
	if agent == nil {
		logger.Warn(inboxLogComponent).Str("sender", sender).
			Msg("HumanAgentInbox: human agent has no live runtime")
		return NewDeliverResultFailure("agent_unavailable"), nil
	}
	// ⤵️ 待 9.55 回填: agent.(*TeamAgent).DeliverInput(ctx, body)
	// 当前 stub: 模拟成功
	return NewDeliverResultSuccess(nil), nil
}

// memberExists 成员存在性检查。
// ⤵️ 待 9.55 回填: 调用 team.GetMember(name)
func (h *HumanAgentInbox) memberExists(name string) (bool, error) {
	// ⤵️ 待 9.55 回填: member, err := h.team.GetMember(name); return member != nil, err
	// 当前 stub: 假设存在
	return true, nil
}

// ptrStrOr 返回 *string 的值或默认值。
func ptrStrOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}
```

- [ ] **Step 2: 创建 human_agent_inbox_test.go**

```go
package interaction

import (
	"testing"
)

func TestHumanAgentNotEnabledError(t *testing.T) {
	err := &HumanAgentNotEnabledError{}
	if err.Error() == "" {
		t.Error("Error() 不应为空")
	}
	err2 := &HumanAgentNotEnabledError{Message: "custom"}
	if err2.Error() != "custom" {
		t.Errorf("Error() = %v, want custom", err2.Error())
	}
}

func TestUnknownHumanAgentError(t *testing.T) {
	err := &UnknownHumanAgentError{Sender: "ghost", Registered: []string{"human_agent"}}
	if err.Error() == "" {
		t.Error("Error() 不应为空")
	}
}

func TestNewHumanAgentInbox(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	if h == nil {
		t.Error("NewHumanAgentInbox 应返回非 nil")
	}
}

func TestHumanAgentInbox_Send_驱动avatar(t *testing.T) {
	var lookedUp string
	lookup := func(sender string) any {
		lookedUp = sender
		return "mock-agent" // 非 nil 表示有活跃运行时
	}
	h := NewHumanAgentInbox(nil, nil, lookup, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	if lookedUp != "human_agent" {
		t.Errorf("lookedUp = %v, want human_agent", lookedUp)
	}
}

func TestHumanAgentInbox_Send_广播(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	target := "all"
	result, err := h.Send("hello all", &target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_Send_无lookup时驱动失败(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("无 agentLookup 时驱动 avatar 应失败")
	}
	if result.Reason == nil || *result.Reason != "agent_unavailable" {
		t.Errorf("Reason = %v, want agent_unavailable", result.Reason)
	}
}

func TestHumanAgentInbox_Send_lookup返回nil(t *testing.T) {
	lookup := func(sender string) any { return nil }
	h := NewHumanAgentInbox(nil, nil, lookup, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("agentLookup 返回 nil 时应失败")
	}
}

func TestHumanAgentInbox_Send_未知发送者(t *testing.T) {
	h := NewHumanAgentInbox(nil, nil, nil, nil)
	sender := "ghost"
	_, err := h.Send("hello", nil, &sender)
	if err == nil {
		t.Error("未知发送者应返回错误")
	}
	if _, ok := err.(*UnknownHumanAgentError); !ok {
		t.Errorf("err 类型 = %T, want *UnknownHumanAgentError", err)
	}
}

func TestHumanAgentInbox_GetOnInbound(t *testing.T) {
	cb := func(event HumanAgentInboundEvent) error { return nil }
	h := NewHumanAgentInbox(nil, nil, nil, cb)
	if h.GetOnInbound() == nil {
		t.Error("GetOnInbound 不应为 nil")
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/interaction/ -v -run "TestHumanAgent|TestNewHumanAgentInbox" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/interaction/human_agent_inbox.go internal/agent_teams/interaction/human_agent_inbox_test.go
git commit -m "feat(interaction): 添加 HumanAgentInbox 收件箱 + 错误类型 (9.59b)"
```

---

## Task 5: runtime 包 — gate.go

**Files:**
- Create: `internal/agent_teams/runtime/doc.go`
- Create: `internal/agent_teams/runtime/gate.go`
- Create: `internal/agent_teams/runtime/gate_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package runtime 提供团队 Agent 运行时管理。
//
// Runtime 包管理 TeamAgent 的活跃运行时池、并发门控和生命周期调度。
// 核心组件：
//   - InteractGate：Run/Interact 并发门控，保证 streaming 结束前 interact 排空
//   - TeamRuntimePool：进程内活跃 TeamAgent 运行时池
//   - TeamRuntimeManager：运行时管理器（interact 路由 + 生命周期 stub）
//
// 文件目录：
//
//	runtime/
//	├── doc.go      # 包文档
//	├── gate.go     # InteractGate 并发门控
//	├── pool.go     # ActiveTeam/ActiveTeamInfo/TeamRuntimePool
//	└── manager.go  # TeamRuntimeManager（interact 完整实现，其余空 stub）
//
// 对应 Python 代码：openjiuwen/agent_teams/runtime/
package runtime
```

- [ ] **Step 2: 创建 gate.go**

```go
package runtime

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AdmissionTicket admit 成功后的不透明票据。
// 对齐 Python: AdmissionTicket (openjiuwen/agent_teams/runtime/gate.py)
//
// 调用方必须在 Agent 实际消费载荷后将票据传回 ConsumeDone，
// 以便门控正确排空。
type AdmissionTicket struct {
	// gate 所属门控
	gate *InteractGate
}

// InteractGate Run/Interact 并发门控。
// 对齐 Python: InteractGate (openjiuwen/agent_teams/runtime/gate.py)
//
// 当 run_agent_team_streaming 调用进行中时，interact_team 可通过
// 门控准入新载荷。Run 即将退出时关闭门控（拒绝后续 interact），
// 并等待飞行中载荷消费完毕后 stream 才真正结束。
//
// 状态转换：
//
//	OPEN    --Admit()-------->      OPEN, inflight++
//	OPEN    --CloseAndDrain()--> CLOSING --(inflight==0)--> DRAINED
//	CLOSING --Admit()-------->      nil (rejected)
//	*       --ConsumeDone()-->      inflight--; signal drained when zero
type InteractGate struct {
	// closed 门控是否已关闭
	closed bool
	// inflight 飞行中载荷计数
	inflight int
	// drained inflight==0 信号通道（关闭表示已排空）
	drained chan struct{}
	// mu 互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInteractGate 创建新门控。
// 对齐 Python: InteractGate.__init__()
func NewInteractGate() *InteractGate {
	g := &InteractGate{
		drained: make(chan struct{}),
	}
	// 初始状态：OPEN + inflight=0 → drained
	close(g.drained)
	return g
}

// Closed 门控是否已关闭。
// 对齐 Python: InteractGate.closed (property)
func (g *InteractGate) Closed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// Inflight 当前飞行中的载荷数。
// 对齐 Python: InteractGate.inflight (property)
func (g *InteractGate) Inflight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight
}

// Admit 尝试准入一个新载荷。
// 对齐 Python: InteractGate.admit()
//
// 门控已关闭时返回 nil；否则 inflight++ 并返回绑定到本门控的票据。
func (g *InteractGate) Admit() *AdmissionTicket {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.inflight++
	// inflight > 0 → 未排空，重新创建 drained 通道
	g.drained = make(chan struct{})
	return &AdmissionTicket{gate: g}
}

// ConsumeDone 标记载荷已消费。
// 对齐 Python: InteractGate.consume_done(ticket)
//
// 来自不同门控的票据被静默忽略。
// inflight 递减到 0 时关闭 drained 通道发出排空信号。
func (g *InteractGate) ConsumeDone(ticket *AdmissionTicket) {
	if ticket == nil || ticket.gate != g {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight <= 0 {
		return
	}
	g.inflight--
	if g.inflight == 0 {
		close(g.drained)
	}
}

// CloseAndDrain 关闭门控并等待飞行中载荷排空。
// 对齐 Python: InteractGate.close_and_drain()
//
// 支持通过 ctx 取消等待。
func (g *InteractGate) CloseAndDrain(ctx context.Context) error {
	g.mu.Lock()
	g.closed = true
	if g.inflight == 0 {
		// 已排空，直接关闭 drained 通道（如果未关闭）
		select {
		case <-g.drained:
			// 已关闭
		default:
			close(g.drained)
		}
		g.mu.Unlock()
		return nil
	}
	drainedCh := g.drained
	g.mu.Unlock()

	// 等待排空或 ctx 取消
	select {
	case <-drainedCh:
		return nil
	case <-ctx.Done():
		logger.Warn(gateLogComponent).Err(ctx.Err()).
			Msg("CloseAndDrain cancelled while waiting for inflight to drain")
		return ctx.Err()
	}
}

// Reset 重置门控供新一轮 Run 使用。
// 对齐 Python: InteractGate.reset()
//
// 清除 closed 标记并重置 inflight 为零。
func (g *InteractGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = false
	g.inflight = 0
	// 确保 drained 通道处于关闭状态
	select {
	case <-g.drained:
		// 已关闭，重新创建
		g.drained = make(chan struct{})
		close(g.drained)
	default:
		// 未关闭，直接关闭
		close(g.drained)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// gateLogComponent 日志组件
var gateLogComponent = logger.ComponentChannel
```

- [ ] **Step 3: 创建 gate_test.go**

```go
package runtime

import (
	"context"
	"testing"
	"time"
)

func TestNewInteractGate(t *testing.T) {
	g := NewInteractGate()
	if g.Closed() {
		t.Error("新门控不应已关闭")
	}
	if g.Inflight() != 0 {
		t.Errorf("Inflight = %d, want 0", g.Inflight())
	}
}

func TestInteractGate_Admit(t *testing.T) {
	g := NewInteractGate()
	ticket := g.Admit()
	if ticket == nil {
		t.Error("Admit 应返回非 nil 票据")
	}
	if g.Inflight() != 1 {
		t.Errorf("Inflight = %d, want 1", g.Inflight())
	}
}

func TestInteractGate_Admit_关闭后拒绝(t *testing.T) {
	g := NewInteractGate()
	ctx := context.Background()
	_ = g.CloseAndDrain(ctx)
	ticket := g.Admit()
	if ticket != nil {
		t.Error("关闭后 Admit 应返回 nil")
	}
}

func TestInteractGate_ConsumeDone(t *testing.T) {
	g := NewInteractGate()
	ticket := g.Admit()
	g.ConsumeDone(ticket)
	if g.Inflight() != 0 {
		t.Errorf("Inflight = %d, want 0", g.Inflight())
	}
}

func TestInteractGate_ConsumeDone_不同门控的票据(t *testing.T) {
	g1 := NewInteractGate()
	g2 := NewInteractGate()
	ticket := g1.Admit()
	g2.ConsumeDone(ticket) // 不应影响 g1
	if g1.Inflight() != 1 {
		t.Errorf("g1 Inflight = %d, want 1（不同门控票据应被忽略）", g1.Inflight())
	}
}

func TestInteractGate_ConsumeDone_nil票据(t *testing.T) {
	g := NewInteractGate()
	g.Admit()
	g.ConsumeDone(nil) // 不应 panic
	if g.Inflight() != 1 {
		t.Errorf("Inflight = %d, want 1", g.Inflight())
	}
}

func TestInteractGate_CloseAndDrain_无飞行中载荷(t *testing.T) {
	g := NewInteractGate()
	ctx := context.Background()
	err := g.CloseAndDrain(ctx)
	if err != nil {
		t.Errorf("CloseAndDrain error = %v", err)
	}
	if !g.Closed() {
		t.Error("CloseAndDrain 后应已关闭")
	}
}

func TestInteractGate_CloseAndDrain_等飞行中载荷(t *testing.T) {
	g := NewInteractGate()
	ticket := g.Admit()

	done := make(chan error, 1)
	go func() {
		ctx := context.Background()
		done <- g.CloseAndDrain(ctx)
	}()

	// 短暂等待确保 CloseAndDrain 已开始等待
	time.Sleep(50 * time.Millisecond)
	if !g.Closed() {
		t.Error("CloseAndDrain 应已设置 closed 标记")
	}

	// 消费完成 → 应解除 CloseAndDrain 等待
	g.ConsumeDone(ticket)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("CloseAndDrain error = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("CloseAndDrain 超时")
	}
}

func TestInteractGate_CloseAndDrain_ctx取消(t *testing.T) {
	g := NewInteractGate()
	g.Admit()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := g.CloseAndDrain(ctx)
	if err == nil {
		t.Error("ctx 取消时应返回错误")
	}
}

func TestInteractGate_Reset(t *testing.T) {
	g := NewInteractGate()
	ctx := context.Background()
	_ = g.CloseAndDrain(ctx)

	g.Reset()
	if g.Closed() {
		t.Error("Reset 后不应已关闭")
	}
	if g.Inflight() != 0 {
		t.Errorf("Reset 后 Inflight = %d, want 0", g.Inflight())
	}
	// Reset 后应可再次 Admit
	ticket := g.Admit()
	if ticket == nil {
		t.Error("Reset 后 Admit 应返回非 nil 票据")
	}
}

func TestInteractGate_多载荷场景(t *testing.T) {
	g := NewInteractGate()
	t1 := g.Admit()
	t2 := g.Admit()
	t3 := g.Admit()

	if g.Inflight() != 3 {
		t.Errorf("Inflight = %d, want 3", g.Inflight())
	}

	g.ConsumeDone(t1)
	if g.Inflight() != 2 {
		t.Errorf("Inflight = %d, want 2", g.Inflight())
	}

	g.ConsumeDone(t3)
	g.ConsumeDone(t2)
	if g.Inflight() != 0 {
		t.Errorf("Inflight = %d, want 0", g.Inflight())
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -v -count=1`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/runtime/doc.go internal/agent_teams/runtime/gate.go internal/agent_teams/runtime/gate_test.go
git commit -m "feat(runtime): 添加 InteractGate 并发门控 (9.59b)"
```

---

## Task 6: runtime 包 — pool.go

**Files:**
- Create: `internal/agent_teams/runtime/pool.go`
- Create: `internal/agent_teams/runtime/pool_test.go`

- [ ] **Step 1: 创建 pool.go**

```go
package runtime

import (
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeState 运行时状态。
// 对齐 Python: RuntimeState (openjiuwen/agent_teams/runtime/pool.py)
type RuntimeState int

// ActiveTeam 活跃团队条目。
// 对齐 Python: ActiveTeam (openjiuwen/agent_teams/runtime/pool.py)
//
// 每个 ActiveTeam 持有一个 TeamAgent Leader 实例、绑定的 session ID、
// 生命周期状态和 InteractGate 门控。
type ActiveTeam struct {
	// TeamName 团队名
	TeamName string
	// Agent TeamAgent Leader 实例
	// ⤵️ 待 9.55 回填: *TeamAgent
	Agent any
	// SessionID 当前绑定的 session ID
	SessionID string
	// State 生命周期状态
	State RuntimeState
	// InteractGate 并发门控
	InteractGate *InteractGate
}

// ActiveTeamInfo 活跃团队信息（只读视图）。
// 对齐 Python: ActiveTeamInfo (openjiuwen/agent_teams/runtime/pool.py)
//
// 排除 TeamAgent 引用和 InteractGate，供外部消费者安全读取。
type ActiveTeamInfo struct {
	// TeamName 团队名
	TeamName string
	// SessionID session ID
	SessionID string
	// State 生命周期状态
	State RuntimeState
	// GateClosed 门控是否已关闭
	GateClosed bool
}

// TeamRuntimePool 进程内活跃 TeamAgent 运行时池。
// 对齐 Python: TeamRuntimePool (openjiuwen/agent_teams/runtime/pool.py)
//
// 按 team_name 索引，同一团队同时绑定最多一个 session。
type TeamRuntimePool struct {
	// entries 团队条目映射
	entries map[string]*ActiveTeam
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// RuntimeStateRunning 运行中
	RuntimeStateRunning RuntimeState = iota
	// RuntimeStatePaused 已暂停
	RuntimeStatePaused
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamRuntimePool 创建运行时池。
func NewTeamRuntimePool() *TeamRuntimePool {
	return &TeamRuntimePool{
		entries: make(map[string]*ActiveTeam),
	}
}

// Get 获取活跃团队。
// 对齐 Python: TeamRuntimePool.get(team_name)
func (p *TeamRuntimePool) Get(teamName string) *ActiveTeam {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entries[teamName]
}

// HasActive 检查是否有活跃团队。
// 对齐 Python: TeamRuntimePool.has_active(team_name)
func (p *TeamRuntimePool) HasActive(teamName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.entries[teamName]
	return ok
}

// Add 注册活跃团队（覆盖同名的已有条目）。
// 对齐 Python: TeamRuntimePool.add(entry)
func (p *TeamRuntimePool) Add(entry *ActiveTeam) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[entry.TeamName] = entry
}

// Remove 移除活跃团队并返回条目。
// 对齐 Python: TeamRuntimePool.remove(team_name)
func (p *TeamRuntimePool) Remove(teamName string) *ActiveTeam {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entries[teamName]
	delete(p.entries, teamName)
	return entry
}

// ListTeamNames 返回所有团队名的快照。
// 对齐 Python: TeamRuntimePool.list_team_names()
func (p *TeamRuntimePool) ListTeamNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.entries))
	for name := range p.entries {
		names = append(names, name)
	}
	return names
}

// TeamsForSession 返回绑定到指定 session 的所有活跃团队。
// 对齐 Python: TeamRuntimePool.teams_for_session(session_id)
func (p *TeamRuntimePool) TeamsForSession(sessionID string) []*ActiveTeam {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []*ActiveTeam
	for _, entry := range p.entries {
		if entry.SessionID == sessionID {
			result = append(result, entry)
		}
	}
	return result
}

// ListAllInfo 返回所有活跃团队的只读快照。
// 对齐 Python: TeamRuntimePool.list_all_info()
func (p *TeamRuntimePool) ListAllInfo() []ActiveTeamInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]ActiveTeamInfo, 0, len(p.entries))
	for _, entry := range p.entries {
		result = append(result, ActiveTeamInfo{
			TeamName:    entry.TeamName,
			SessionID:   entry.SessionID,
			State:       entry.State,
			GateClosed:  entry.InteractGate.Closed(),
		})
	}
	return result
}
```

- [ ] **Step 2: 创建 pool_test.go**

```go
package runtime

import (
	"testing"
)

func TestNewTeamRuntimePool(t *testing.T) {
	p := NewTeamRuntimePool()
	if p == nil {
		t.Error("NewTeamRuntimePool 应返回非 nil")
	}
}

func TestTeamRuntimePool_AddAndGet(t *testing.T) {
	p := NewTeamRuntimePool()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		Agent:        nil,
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	p.Add(entry)

	got := p.Get("team-1")
	if got == nil || got.TeamName != "team-1" {
		t.Error("Get 应返回已添加的条目")
	}
}

func TestTeamRuntimePool_Get不存在(t *testing.T) {
	p := NewTeamRuntimePool()
	if p.Get("ghost") != nil {
		t.Error("不存在的团队应返回 nil")
	}
}

func TestTeamRuntimePool_HasActive(t *testing.T) {
	p := NewTeamRuntimePool()
	if p.HasActive("team-1") {
		t.Error("空池不应有活跃团队")
	}
	p.Add(&ActiveTeam{TeamName: "team-1", InteractGate: NewInteractGate()})
	if !p.HasActive("team-1") {
		t.Error("添加后应有活跃团队")
	}
}

func TestTeamRuntimePool_Remove(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", InteractGate: NewInteractGate()})
	removed := p.Remove("team-1")
	if removed == nil || removed.TeamName != "team-1" {
		t.Error("Remove 应返回被移除的条目")
	}
	if p.HasActive("team-1") {
		t.Error("Remove 后不应有活跃团队")
	}
}

func TestTeamRuntimePool_Remove不存在(t *testing.T) {
	p := NewTeamRuntimePool()
	removed := p.Remove("ghost")
	if removed != nil {
		t.Error("不存在的团队应返回 nil")
	}
}

func TestTeamRuntimePool_Add覆盖(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-1", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-2", InteractGate: NewInteractGate()})
	got := p.Get("team-1")
	if got.SessionID != "sess-2" {
		t.Errorf("SessionID = %v, want sess-2（覆盖）", got.SessionID)
	}
}

func TestTeamRuntimePool_ListTeamNames(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "alpha", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "beta", InteractGate: NewInteractGate()})
	names := p.ListTeamNames()
	if len(names) != 2 {
		t.Errorf("len = %d, want 2", len(names))
	}
}

func TestTeamRuntimePool_TeamsForSession(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-1", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "team-2", SessionID: "sess-1", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "team-3", SessionID: "sess-2", InteractGate: NewInteractGate()})
	teams := p.TeamsForSession("sess-1")
	if len(teams) != 2 {
		t.Errorf("len = %d, want 2", len(teams))
	}
}

func TestTeamRuntimePool_ListAllInfo(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-1", State: RuntimeStateRunning, InteractGate: NewInteractGate()})
	infos := p.ListAllInfo()
	if len(infos) != 1 {
		t.Fatalf("len = %d, want 1", len(infos))
	}
	if infos[0].TeamName != "team-1" {
		t.Errorf("TeamName = %v, want team-1", infos[0].TeamName)
	}
	if infos[0].State != RuntimeStateRunning {
		t.Errorf("State = %v, want RuntimeStateRunning", infos[0].State)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -v -run "TestNewTeamRuntimePool|TestTeamRuntimePool" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/runtime/pool.go internal/agent_teams/runtime/pool_test.go
git commit -m "feat(runtime): 添加 TeamRuntimePool 活跃团队池 (9.59b)"
```

---

## Task 7: runtime 包 — manager.go

**Files:**
- Create: `internal/agent_teams/runtime/manager.go`
- Create: `internal/agent_teams/runtime/manager_test.go`

- [ ] **Step 1: 创建 manager.go**

```go
package runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction as sessioninteraction"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamRuntimeManager 团队运行时管理器。
// 对齐 Python: TeamRuntimeManager (openjiuwen/agent_teams/runtime/manager.py)
//
// 持有进程内 TeamRuntimePool，分发每个 run_agent_team_streaming 调用。
// interact() 方法是 Interaction 层的中央路由。
type TeamRuntimeManager struct {
	// pool 活跃团队运行时池
	pool *TeamRuntimePool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mgrLogComponent 日志组件
	mgrLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamRuntimeManager 创建运行时管理器。
func NewTeamRuntimeManager() *TeamRuntimeManager {
	return &TeamRuntimeManager{
		pool: NewTeamRuntimePool(),
	}
}

// Pool 返回运行时池。
func (m *TeamRuntimeManager) Pool() *TeamRuntimePool {
	return m.pool
}

// Interact 路由交互载荷通过活跃团队的门控。
// 对齐 Python: TeamRuntimeManager.interact(payload, *, team_name, session_id)
//
// 接受三种输入类型：
//   - *sessioninteraction.InteractiveInput → 恢复中断
//   - string → ParseInteractStr → payloads
//   - interaction.InteractPayload → 直接分发
//
// 流程：查找活跃团队 → 类型判断 → admit → ResolveTargets → DispatchPayload → consume_done
func (m *TeamRuntimeManager) Interact(
	ctx context.Context,
	payload any,
	teamName string,
	sessionID string,
) (*interaction.DeliverResult, error) {
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		return interaction.NewDeliverResultFailure("not_active"), nil
	}

	// InteractiveInput → 恢复中断
	if interactiveInput, ok := payload.(*sessioninteraction.InteractiveInput); ok {
		// ⤵️ 待 9.55 回填: agent.HasPendingInterrupt() + agent.ResumeInterrupt(interactiveInput)
		// 当前 stub: 检查 Agent 字段是否非 nil
		return m.handleInteractiveInput(entry, interactiveInput)
	}

	// 解析 payloads
	var payloads []interaction.InteractPayload
	if strPayload, ok := payload.(string); ok {
		parsed := interaction.ParseInteractStr(strPayload)
		if len(parsed) == 0 {
			payloads = []interaction.InteractPayload{interaction.NewGodViewMessage(strPayload)}
		} else {
			payloads = parsed
		}
	} else if interactPayload, ok := payload.(interaction.InteractPayload); ok {
		payloads = []interaction.InteractPayload{interactPayload}
	} else {
		return interaction.NewDeliverResultFailure("unsupported_payload_type"), nil
	}

	// admit
	ticket := entry.InteractGate.Admit()
	if ticket == nil {
		return interaction.NewDeliverResultFailure("gate_closed"), nil
	}

	// consume_done 保证在 finally 中调用
	defer entry.InteractGate.ConsumeDone(ticket)

	// resolve_targets
	resolved, err := m.resolveRecipients(entry, payloads)
	if err != nil {
		return nil, err
	}

	// 逐个分发
	var lastResult *interaction.DeliverResult
	for _, p := range resolved {
		lastResult, err = m.dispatchPayload(ctx, entry, p)
		if err != nil {
			return nil, err
		}
		if !lastResult.IsOK() {
			return lastResult, nil
		}
	}
	if lastResult == nil {
		lastResult = interaction.NewDeliverResultSuccess(nil)
	}
	return lastResult, nil
}

// ──────────── 生命周期 stub ────────────

// Activate 激活团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Activate(ctx context.Context, teamName string, sessionID string, agent any) error {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("Activate (stub)")
	// ⤵️ 待 9.62: 创建 ActiveTeam → pool.Add(entry)
	return nil
}

// Finalize 终结团队运行。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Finalize(ctx context.Context, teamName string, sessionID string) error {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("Finalize (stub)")
	return nil
}

// Pause 暂停团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) Pause(ctx context.Context, teamName string, sessionID string) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("Pause (stub)")
	return false, nil
}

// StopTeam 停止团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) StopTeam(ctx context.Context, teamName string, sessionID string) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("StopTeam (stub)")
	return false, nil
}

// DeleteTeam 删除团队。
// ⤵️ 待 9.62 CoordinationKernel 章节回填
func (m *TeamRuntimeManager) DeleteTeam(ctx context.Context, teamName string, sessionID string) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("DeleteTeam (stub)")
	return false, nil
}

// RegisterHumanAgentInbound 注册团队→用户通知回调。
// ⤵️ 待 9.55 TeamBackend 回填
func (m *TeamRuntimeManager) RegisterHumanAgentInbound(ctx context.Context, teamName string, sessionID string, memberName string, callback any) (bool, error) {
	logger.Info(mgrLogComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Str("member_name", memberName).Msg("RegisterHumanAgentInbound (stub)")
	return false, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveEntry 查找活跃团队条目。
// 对齐 Python: TeamRuntimeManager._resolve_entry(team_name, session_id)
func (m *TeamRuntimeManager) resolveEntry(teamName string, sessionID string) *ActiveTeam {
	entry := m.pool.Get(teamName)
	if entry == nil {
		return nil
	}
	if entry.SessionID != sessionID {
		return nil
	}
	return entry
}

// handleInteractiveInput 处理 InteractiveInput（恢复中断）。
func (m *TeamRuntimeManager) handleInteractiveInput(entry *ActiveTeam, input *sessioninteraction.InteractiveInput) (*interaction.DeliverResult, error) {
	// ⤵️ 待 9.55 回填:
	//   if agent.has_pending_interrupt() {
	//       agent.ResumeInterrupt(input)
	//       return interaction.NewDeliverResultSuccess(nil), nil
	//   }
	//   return interaction.NewDeliverResultFailure("unsupported_interactive_input"), nil
	logger.Debug(mgrLogComponent).Msg("handleInteractiveInput (stub)")
	return interaction.NewDeliverResultSuccess(nil), nil
}

// resolveRecipients 校验 @<member> 接收者是否在花名册中。
// 对齐 Python: TeamRuntimeManager._resolve_recipients(agent, payloads)
func (m *TeamRuntimeManager) resolveRecipients(entry *ActiveTeam, payloads []interaction.InteractPayload) ([]interaction.InteractPayload, error) {
	// ⤵️ 待 9.55 回填: 从 agent.team_backend 获取 MemberExistsCheck
	// 当前 stub: 所有成员视为存在
	memberExists := func(name string) (bool, error) { return true, nil }
	return interaction.ResolveTargets(payloads, memberExists)
}

// dispatchPayload 按载荷类型分发。
// 对齐 Python: TeamRuntimeManager._dispatch_payload(agent, payload)
func (m *TeamRuntimeManager) dispatchPayload(
	ctx context.Context,
	entry *ActiveTeam,
	payload interaction.InteractPayload,
) (*interaction.DeliverResult, error) {
	switch p := payload.(type) {
	case *interaction.GodViewMessage:
		// ⤵️ 待 9.55 回填: agent.DeliverInput 作为 deliverInput
		deliverInput := func(ctx context.Context, content string) error {
			logger.Debug(mgrLogComponent).Str("body_len", fmt.Sprintf("%d", len(content))).
				Msg("deliverInput (stub)")
			return nil
		}
		return interaction.DeliverToLeader(deliverInput, p.Body()), nil

	case *interaction.OperatorMessage:
		// ⤵️ 待 9.55 回填: 从 agent.team_backend 获取 messageManager
		inbox := interaction.NewUserInbox(nil)
		if p.Target() == nil {
			// 广播前先自动启动所有未启动成员
			// ⤵️ 待 9.55 回填: agent.AutoStartAll()
			return inbox.Broadcast(p.Body())
		}
		// 点对点前先启动目标成员
		// ⤵️ 待 9.55 回填: agent.AutoStartMember(*p.Target())
		return inbox.Direct(*p.Target(), p.Body())

	case *interaction.HumanAgentMessage:
		// ⤵️ 待 9.55 回填: 从 agent.team_backend 和 agent.lookup_human_agent_runtime 构造 inbox
		hInbox := interaction.NewHumanAgentInbox(
			nil,   // team
			nil,   // messageManager
			nil,   // agentLookup
			nil,   // onInbound
		)
		result, err := hInbox.Send(p.Body(), p.Target(), strPtr(p.Sender()))
		if err != nil {
			if _, ok := err.(*interaction.HumanAgentNotEnabledError); ok {
				return interaction.NewDeliverResultFailure("human_agent_not_enabled"), nil
			}
			if _, ok := err.(*interaction.UnknownHumanAgentError); ok {
				return interaction.NewDeliverResultFailure("unknown_human_agent"), nil
			}
			return nil, err
		}
		return result, nil

	default:
		return interaction.NewDeliverResultFailure("unknown_payload:" + payload.Kind().String()), nil
	}
}

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }
```

**注意**：
1. Go import alias 语法：`sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"`（不是 `as`）
2. `dispatchPayload` 中使用了 `fmt.Sprintf`，需要在 import 中添加 `"fmt"`
3. `PayloadKind` 需要 `String()` 方法用于 unknown_payload 错误信息——已在 Task 1 的 `payload.go` 中添加 `String()` 方法
4. `interaction` 包的 import 路径：`"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"`

- [ ] **Step 2: 创建 manager_test.go**

```go
package runtime

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

func TestNewTeamRuntimeManager(t *testing.T) {
	m := NewTeamRuntimeManager()
	if m == nil {
		t.Error("NewTeamRuntimeManager 应返回非 nil")
	}
	if m.Pool() == nil {
		t.Error("Pool 不应为 nil")
	}
}

func TestTeamRuntimeManager_Interact_团队不存在(t *testing.T) {
	m := NewTeamRuntimeManager()
	result, err := m.Interact(context.Background(), "hello", "ghost", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("不活跃的团队应返回失败")
	}
	if result.Reason == nil || *result.Reason != "not_active" {
		t.Errorf("Reason = %v, want not_active", result.Reason)
	}
}

func TestTeamRuntimeManager_Interact_字符串输入(t *testing.T) {
	m := NewTeamRuntimeManager()
	// 添加活跃团队
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	result, err := m.Interact(context.Background(), "hello leader", "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_GodView载荷(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	payload := interaction.NewGodViewMessage("hello")
	result, err := m.Interact(context.Background(), payload, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_Operator载荷(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	target := "alice"
	payload := interaction.NewOperatorMessage("hello", &target)
	result, err := m.Interact(context.Background(), payload, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_门控关闭(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	// 关闭门控
	_ = entry.InteractGate.CloseAndDrain(context.Background())

	result, err := m.Interact(context.Background(), "hello", "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("门控关闭时应返回失败")
	}
	if result.Reason == nil || *result.Reason != "gate_closed" {
		t.Errorf("Reason = %v, want gate_closed", result.Reason)
	}
}

func TestTeamRuntimeManager_Interact_InteractiveInput(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	input, _ := sessioninteraction.NewInteractiveInput("resume data")
	result, err := m.Interact(context.Background(), input, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	// stub 总是返回成功
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true (stub)", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_session不匹配(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	result, err := m.Interact(context.Background(), "hello", "team-1", "wrong-session")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("session 不匹配应返回 not_active")
	}
}

func TestTeamRuntimeManager_Interact_不支持的载荷类型(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	result, err := m.Interact(context.Background(), 12345, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("不支持的载荷类型应返回失败")
	}
}

// ──────────── 生命周期 stub 测试 ────────────

func TestTeamRuntimeManager_Activate_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	err := m.Activate(context.Background(), "team-1", "sess-1", nil)
	if err != nil {
		t.Errorf("Activate stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_Pause_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	_, err := m.Pause(context.Background(), "team-1", "sess-1")
	if err != nil {
		t.Errorf("Pause stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_StopTeam_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	_, err := m.StopTeam(context.Background(), "team-1", "sess-1")
	if err != nil {
		t.Errorf("StopTeam stub 应返回 nil, got %v", err)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -v -run "TestNewTeamRuntimeManager|TestTeamRuntimeManager" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/runtime/manager.go internal/agent_teams/runtime/manager_test.go
git commit -m "feat(runtime): 添加 TeamRuntimeManager — interact 完整实现 + 生命周期 stub (9.59b)"
```

---

## Task 8: 回填已有 ⤵️ 标记

**Files:**
- Modify: `internal/agent_teams/agent/stream_controller.go`
- Modify: `internal/agent_teams/agent/team_agent.go`
- Modify: `internal/agent_teams/doc.go`

- [ ] **Step 1: 回填 stream_controller.go 的 pendingInterruptResumes 类型和 streamOneRound inputMap**

在 `stream_controller.go` 中：

1. 修改 `pendingInterruptResumes` 类型：`[]any` → 保留为 `[]any`（因为可能是 InteractPayload 或 InteractiveInput，两者混用），但添加更详细的注释标注回填来源
2. 回填 `streamOneRound` 中的 inputMap 补充 sessionID 和 teamSession

具体修改：
- `pendingInterruptResumes []any` 注释更新：`⤵️ 待 Interaction 层实现后回填类型` → `⤵️ 待 9.55 TeamAgent 完善后回填具体类型（InteractPayload 或 InteractiveInput）`
- `streamOneRound` 中 inputMap 添加 sessionID 读取

- [ ] **Step 2: 回填 team_agent.go 的 Interact/Broadcast/HumanAgentSay**

在 `team_agent.go` 中更新三个方法的注释，标注 Interaction 层已实现：

1. `Interact()` — 更新注释：`TODO(#9.62)` → 注释说明现在可委托 `runtime.TeamRuntimeManager.Interact()`
2. `Broadcast()` — 更新注释：`TODO(#9.62)` → 注释说明现在可委托 `interaction.UserInbox.Broadcast()`
3. `HumanAgentSay()` — 更新注释：`TODO(#9.62)` → 注释说明现在可委托 `interaction.HumanAgentInbox.Send()`

注意：这些方法当前无法直接调用 runtime/interaction 包（因为 TeamAgent 在 agent 包中，interaction/runtime 在子包中，需要通过依赖注入或接口），因此只更新注释标注可用路径，具体回填留到 9.55 TeamAgent 完善时做。

- [ ] **Step 3: 更新 doc.go 文件目录**

更新 `internal/agent_teams/doc.go`：
- `interaction/` 条目去掉 `⤵️ 回填:` 前缀，更新为已实现描述
- `runtime/` 条目去掉 `⤵️ 回填:` 前缀，更新为已实现描述

- [ ] **Step 4: 运行全量测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -count=1`

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/stream_controller.go internal/agent_teams/agent/team_agent.go internal/agent_teams/doc.go
git commit -m "refactor: 回填 9.59b Interaction 层 ⤵️ 标记 — 更新注释和 doc.go (9.59b)"
```

---

## Task 9: 运行覆盖率检查 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 运行覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agent_teams/interaction/ ./internal/agent_teams/runtime/`

确认覆盖率 ≥ 85%。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 9.59 行的描述**

将 9.59 行的状态保持 ✅，但描述更新为包含 Interaction 层：
```
| 9.59 | ✅ | SessionManager + Interaction 层 | 会话三态管理 + Interaction 层（payload/router/UserInbox/HumanAgentInbox + runtime gate/pool/manager.interact）；⤵️ 9.55 回填 TeamAgent 类型依赖 |
```

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md — 9.59b Interaction 层完成 (9.59b)"
```
