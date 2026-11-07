package permissions

import (
	"strings"
	"testing"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// seqIDFactory 返回递增的号码牌生成器，便于断言
func seqIDFactory() func() string {
	n := 0
	return func() string {
		n++
		return "tiv_" + strings.Repeat("x", n%3) + string(rune('0'+n%10))
	}
}

// newTestQueue 创建带确定性号码牌的队列
func newTestQueue() *RootPermissionQueue {
	counter := 0
	return NewRootPermissionQueue(func() string {
		counter++
		return "tiv_test_" + string(rune('0'+counter%10))
	})
}

// mustBegin 调用 Begin 并要求成功
func mustBegin(t *testing.T, q *RootPermissionQueue, rootSess, req, execSess, call, tool string) RootPermissionCard {
	t.Helper()
	card, err := q.Begin(rootSess, req, execSess, call, tool)
	if err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	return card
}

// testSnapshot 构造一个测试用中断请求快照
func testSnapshot() *InterruptRequestSnapshot {
	return &InterruptRequestSnapshot{
		Message:        "确认执行？",
		AutoConfirmKey: "ack",
		Metadata:       map[string]any{"k": "v"},
		InterruptID:    "intr_1",
		InterruptKind:  saschema.InterruptKindPermission,
		InterruptOwner: "owner-1",
	}
}

// interaction 构造一个 Core 中断项映射
func interaction(id string, request any) map[string]any {
	return map[string]any{"id": id, "value": request}
}

// wireKeyMap 构造一个合法的 wire 表示
func wireKeyMap(t *testing.T, invocationID, rootSess, req, execSess, call string) map[string]any {
	t.Helper()
	key, err := NewToolInvocationKeyV1(
		invocationID, rootSess, req, ExecutorKindAgent, execSess, call, ToolInvocationKeyVersion,
	)
	if err != nil {
		t.Fatalf("构造 key 失败: %v", err)
	}
	return key.ToWire()
}

// ──────────────────────────── 构造测试 ────────────────────────────

// TestNewRootPermissionQueue_默认工厂 验证 nil 工厂被替换为默认随机工厂
func TestNewRootPermissionQueue_默认工厂(t *testing.T) {
	q := NewRootPermissionQueue(nil)
	if q.idFactory == nil {
		t.Fatalf("应使用默认工厂")
	}
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if !strings.HasPrefix(card.Key.InvocationID, "tiv_") {
		t.Fatalf("默认号码牌前缀不符: %q", card.Key.InvocationID)
	}
}

// ──────────────────────────── Begin 测试 ────────────────────────────

// TestBegin_正常 验证新调用返回 active 卡
func TestBegin_正常(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")

	if card.State != RootPermissionCardStateActive {
		t.Fatalf("状态应为 active，实际 %q", card.State)
	}
	if card.ToolName != "tool" {
		t.Fatalf("工具名不符: %q", card.ToolName)
	}
	if card.Key.RootSessionID != "s" || card.Key.RequestID != "r" ||
		card.Key.ExecutionSessionID != "e" || card.Key.ToolCallID != "c" {
		t.Fatalf("key 字段不符: %+v", card.Key)
	}
	if card.Request != nil || card.RootContext != nil {
		t.Fatalf("新卡不应有 request/context")
	}
}

// TestBegin_必填校验 验证四个必填字段为空时报错
func TestBegin_必填校验(t *testing.T) {
	q := newTestQueue()
	cases := []struct{ root, req, exec, call, tool string }{
		{"", "r", "e", "c", "tool"},
		{"s", "", "e", "c", "tool"},
		{"s", "r", "", "c", "tool"},
		{"s", "r", "e", "", "tool"},
		{"s", "r", "e", "c", ""},
	}
	for _, c := range cases {
		if _, err := q.Begin(c.root, c.req, c.exec, c.call, c.tool); err == nil {
			t.Fatalf("空字段应报错: %+v", c)
		}
	}
}

// TestBegin_恢复命中待决卡 验证 pending 存在时直接返回该卡
func TestBegin_恢复命中待决卡(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	pending, err := q.MarkPending(card.Key, testSnapshot(), false, nil)
	if err != nil {
		t.Fatalf("MarkPending 失败: %v", err)
	}

	again, err := q.Begin("s", "r2", "e", "c", "tool")
	if err != nil {
		t.Fatalf("恢复应成功: %v", err)
	}
	if again.Key.InvocationID != pending.Key.InvocationID {
		t.Fatalf("应返回同一张待决卡")
	}
	if again.State != RootPermissionCardStatePending {
		t.Fatalf("状态应为 pending，实际 %q", again.State)
	}
}

// TestBegin_恢复时工具名不符 验证 pending 命中但工具名不同时报错
func TestBegin_恢复时工具名不符(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("MarkPending 失败: %v", err)
	}
	if _, err := q.Begin("s", "r2", "e", "c", "other-tool"); err == nil {
		t.Fatalf("工具名不符应报错")
	}
}

// TestBegin_重复调用 验证同四元组的活跃卡重复 Begin 报错
func TestBegin_重复调用(t *testing.T) {
	q := newTestQueue()
	mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.Begin("s", "r", "e", "c", "tool"); err == nil {
		t.Fatalf("重复调用应报错")
	}
}

// TestBegin_号码牌冲突 验证工厂返回已存在的号码牌时报错
func TestBegin_号码牌冲突(t *testing.T) {
	q := NewRootPermissionQueue(func() string { return "same-id" })
	mustBegin(t, q, "s", "r", "e", "c", "tool1")
	if _, err := q.Begin("s", "r", "e", "c2", "tool2"); err == nil {
		t.Fatalf("号码牌冲突应报错")
	}
}

// TestBegin_隔离会话 验证已隔离会话 Begin 报错
func TestBegin_隔离会话(t *testing.T) {
	q := newTestQueue()
	q.Quarantine("s")
	if _, err := q.Begin("s", "r", "e", "c", "tool"); err == nil {
		t.Fatalf("隔离会话应报错")
	}
}

// ──────────────────────────── MarkPending 测试 ────────────────────────────

// TestMarkPending_正常 验证 active 转 pending 并冻结请求
func TestMarkPending_正常(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	snap := testSnapshot()
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")

	pending, err := q.MarkPending(card.Key, snap, true, dc)
	if err != nil {
		t.Fatalf("MarkPending 失败: %v", err)
	}
	if pending.State != RootPermissionCardStatePending {
		t.Fatalf("状态应为 pending")
	}
	if !pending.AutoManual {
		t.Fatalf("auto_manual 应为 true")
	}
	if pending.RootContext != dc {
		t.Fatalf("root_context 应被冻结")
	}
	if pending.Request.Message != "确认执行？" {
		t.Fatalf("请求未冻结: %+v", pending.Request)
	}
}

// TestMarkPending_nil请求 验证 request 为 nil 时报错
func TestMarkPending_nil请求(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, nil, false, nil); err == nil {
		t.Fatalf("nil 请求应报错")
	}
}

// TestMarkPending_幂等 验证同请求重复 MarkPending 返回同一卡
func TestMarkPending_幂等(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	snap := testSnapshot()
	first, err := q.MarkPending(card.Key, snap, false, nil)
	if err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	second, err := q.MarkPending(card.Key, testSnapshot(), false, nil)
	if err != nil {
		t.Fatalf("重复应成功: %v", err)
	}
	if second.Key.InvocationID != first.Key.InvocationID {
		t.Fatalf("应返回同一卡")
	}
}

// TestMarkPending_请求变更 验证已 pending 但请求内容不同时报错
func TestMarkPending_请求变更(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	changed := testSnapshot()
	changed.Message = "换了消息"
	if _, err := q.MarkPending(card.Key, changed, false, nil); err == nil {
		t.Fatalf("请求变更应报错")
	}
}

// TestMarkPending_状态非法 验证 resuming 状态再 MarkPending 报错
func TestMarkPending_状态非法(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	q.order["s"] = []string{card.Key.InvocationID}
	answer, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err != nil {
		t.Fatalf("ReserveAnswer 失败: %v", err)
	}
	_ = answer

	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err == nil {
		t.Fatalf("resuming 状态应报错")
	}
}

// TestMarkPending_key不符 验证 key 不匹配时报错
func TestMarkPending_key不符(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	other, _ := NewToolInvocationKeyV1(
		"other", "s", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion,
	)
	if _, err := q.MarkPending(other, testSnapshot(), false, nil); err == nil {
		t.Fatalf("key 不符应报错")
	}
	_ = card
}

// TestMarkPending_pending冲突 验证同一三元组被另一张卡占用时报错
func TestMarkPending_pending冲突(t *testing.T) {
	q := newTestQueue()
	card1 := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card1.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("首次失败: %v", err)
	}

	// 直接注入一张同三元组但不同号码牌的卡，模拟冲突
	key2, _ := NewToolInvocationKeyV1(
		"another", "s", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion,
	)
	q.cards["another"] = RootPermissionCard{
		Key: key2, ToolName: "tool", State: RootPermissionCardStateActive,
	}
	if _, err := q.MarkPending(key2, testSnapshot(), false, nil); err == nil {
		t.Fatalf("pending 冲突应报错")
	}
}

// ──────────────────────────── PendingForCall 测试 ────────────────────────────

// TestPendingForCall_命中 验证能取到待决卡
func TestPendingForCall_命中(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}
	got, ok := q.PendingForCall("s", "e", "c")
	if !ok {
		t.Fatalf("应命中")
	}
	if got.Key.InvocationID != card.Key.InvocationID {
		t.Fatalf("卡片不符")
	}
}

// TestPendingForCall_未命中 验证无待决卡时返回 false
func TestPendingForCall_未命中(t *testing.T) {
	q := newTestQueue()
	if _, ok := q.PendingForCall("s", "e", "c"); ok {
		t.Fatalf("应未命中")
	}
	// 有 active 卡但未 pending
	mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, ok := q.PendingForCall("s", "e", "c"); ok {
		t.Fatalf("active 状态不应命中")
	}
}

// ──────────────────────────── ReserveAnswer 测试 ────────────────────────────

// preparedQueue 构造一个已 reconcile 出队首的队列
func preparedQueue(t *testing.T) (*RootPermissionQueue, RootPermissionCard) {
	t.Helper()
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("MarkPending 失败: %v", err)
	}
	if _, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{
			"interrupt_kind": saschema.InterruptKindPermission,
			"metadata": map[string]any{
				"tool_invocation_key": wireKeyMap(t, card.Key.InvocationID, "s", "r", "e", "c"),
			},
		})},
		[]any{"c"},
		"s",
	); err != nil {
		t.Fatalf("Reconcile 失败: %v", err)
	}
	return q, card
}

// TestReserveAnswer_正常 验证队首卡答案被预留为 resuming
func TestReserveAnswer_正常(t *testing.T) {
	q, card := preparedQueue(t)
	answer, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err != nil {
		t.Fatalf("Should succeed: %v", err)
	}
	if answer.Card.State != RootPermissionCardStateResuming {
		t.Fatalf("状态应为 resuming")
	}
	if answer.Payload["approved"] != true {
		t.Fatalf("payload 不符: %+v", answer.Payload)
	}
	sparse, ok := answer.InteractiveInput["c"]
	if !ok {
		t.Fatalf("稀疏输入应以 tool_call_id 为键")
	}
	if sparse.(map[string]any)["approved"] != true {
		t.Fatalf("稀疏输入内容不符")
	}
}

// TestReserveAnswer_rawInputs存在 验证 raw_inputs 非 nil 时报错
func TestReserveAnswer_rawInputs存在(t *testing.T) {
	q, card := preparedQueue(t)
	_, err := q.ReserveAnswer("s", map[string]any{"x": 1}, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err == nil {
		t.Fatalf("raw_inputs 应报错")
	}
}

// TestReserveAnswer_答案数量 验证必须恰好一个答案
func TestReserveAnswer_答案数量(t *testing.T) {
	q, card := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{}); err == nil {
		t.Fatalf("零答案应报错")
	}
	_, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
		"other":               map[string]any{"approved": true, "auto_confirm": false},
	})
	if err == nil {
		t.Fatalf("两个答案应报错")
	}
}

// TestReserveAnswer_非队首答案 验证答非队首卡时报错
func TestReserveAnswer_非队首答案(t *testing.T) {
	q, _ := preparedQueue(t)
	_, err := q.ReserveAnswer("s", nil, map[string]any{
		"not-the-head": map[string]any{"approved": true, "auto_confirm": false},
	})
	if err == nil {
		t.Fatalf("非队首答案应报错")
	}
}

// TestReserveAnswer_空队列 验证未 reconcile 时队列为空报错
func TestReserveAnswer_空队列(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}
	_, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err == nil {
		t.Fatalf("空 order 应报错")
	}
}

// TestReserveAnswer_队首非pending 验证队首卡状态非 pending 时报错
func TestReserveAnswer_队首非pending(t *testing.T) {
	q, card := preparedQueue(t)
	active := card
	active.State = RootPermissionCardStateActive
	q.cards[card.Key.InvocationID] = active

	_, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err == nil {
		t.Fatalf("队首非 pending 应报错")
	}
}

// TestReserveAnswer_payload非映射 验证 payload 非映射时报错
func TestReserveAnswer_payload非映射(t *testing.T) {
	q, card := preparedQueue(t)
	_, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: "not-a-map",
	})
	if err == nil {
		t.Fatalf("非映射 payload 应报错")
	}
}

// TestReserveAnswer_隔离会话 验证隔离会话报错
func TestReserveAnswer_隔离会话(t *testing.T) {
	q, card := preparedQueue(t)
	q.Quarantine("s")
	_, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err == nil {
		t.Fatalf("隔离会话应报错")
	}
}

// ──────────────────────────── normalizeRootPermissionAnswer 测试 ────────────────────────────

// TestNormalizeRootPermissionAnswer_正常 验证四个字段都通过
func TestNormalizeRootPermissionAnswer_正常(t *testing.T) {
	got, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": true, "persist_allow": true, "feedback": "ok",
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got["approved"] != true || got["auto_confirm"] != true ||
		got["persist_allow"] != true || got["feedback"] != "ok" {
		t.Fatalf("结果不符: %+v", got)
	}
}

// TestNormalizeRootPermissionAnswer_多余键 验证未知键报错
func TestNormalizeRootPermissionAnswer_多余键(t *testing.T) {
	_, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": false, "unknown": 1,
	})
	if err == nil {
		t.Fatalf("未知键应报错")
	}
}

// TestNormalizeRootPermissionAnswer_必填类型 验证 approved/auto_confirm 必须布尔
func TestNormalizeRootPermissionAnswer_必填类型(t *testing.T) {
	if _, err := normalizeRootPermissionAnswer(map[string]any{"auto_confirm": false}); err == nil {
		t.Fatalf("缺 approved 应报错")
	}
	if _, err := normalizeRootPermissionAnswer(map[string]any{"approved": 1, "auto_confirm": false}); err == nil {
		t.Fatalf("approved 非布尔应报错")
	}
	if _, err := normalizeRootPermissionAnswer(map[string]any{"approved": true}); err == nil {
		t.Fatalf("缺 auto_confirm 应报错")
	}
}

// TestNormalizeRootPermissionAnswer_optional类型 验证 persist_allow/feedback 类型校验
func TestNormalizeRootPermissionAnswer_optional类型(t *testing.T) {
	if _, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": false, "persist_allow": "yes",
	}); err == nil {
		t.Fatalf("persist_allow 非布尔应报错")
	}
	if _, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": false, "feedback": 123,
	}); err == nil {
		t.Fatalf("feedback 非字符串应报错")
	}
}

// TestNormalizeRootPermissionAnswer_反馈超长 验证超过 4000 字符报错
func TestNormalizeRootPermissionAnswer_反馈超长(t *testing.T) {
	if _, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": false,
		"feedback": strings.Repeat("a", rootPermissionAnswerFeedbackMaxLength+1),
	}); err == nil {
		t.Fatalf("超长反馈应报错")
	}
	exact := strings.Repeat("a", rootPermissionAnswerFeedbackMaxLength)
	if _, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": false, "feedback": exact,
	}); err != nil {
		t.Fatalf("等于上限应通过: %v", err)
	}
}

// TestNormalizeRootPermissionAnswer_persist需双true 验证 persist_allow 为真时 approved 与 auto_confirm 都要为真
func TestNormalizeRootPermissionAnswer_persist需双true(t *testing.T) {
	cases := []struct{ approved, auto bool }{
		{true, false}, {false, true}, {false, false},
	}
	for _, c := range cases {
		if _, err := normalizeRootPermissionAnswer(map[string]any{
			"approved": c.approved, "auto_confirm": c.auto, "persist_allow": true,
		}); err == nil {
			t.Fatalf("approved=%v auto=%v 应报错", c.approved, c.auto)
		}
	}
}

// TestNormalizeRootPermissionAnswer_省略persist 验证未传 persist_allow 时结果不含该键
func TestNormalizeRootPermissionAnswer_省略persist(t *testing.T) {
	got, err := normalizeRootPermissionAnswer(map[string]any{
		"approved": true, "auto_confirm": false,
	})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, exists := got["persist_allow"]; exists {
		t.Fatalf("不应含 persist_allow")
	}
}

// ──────────────────────────── ReleaseAnswer / Claim 测试 ────────────────────────────

// TestReleaseAnswer_退回pending 验证未派发的预留可退回 pending
func TestReleaseAnswer_退回pending(t *testing.T) {
	q, card := preparedQueue(t)
	answer, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err != nil {
		t.Fatalf("预留失败: %v", err)
	}
	q.ReleaseAnswer(answer)

	got, ok := q.Get(card.Key)
	if !ok {
		t.Fatalf("卡应仍存在")
	}
	if got.State != RootPermissionCardStatePending {
		t.Fatalf("应退回 pending，实际 %q", got.State)
	}
}

// TestReleaseAnswer_卡不匹配 验证卡已变更时不做任何事
func TestReleaseAnswer_卡不匹配(t *testing.T) {
	q, card := preparedQueue(t)
	answer, _ := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	// 篡改 answer 里的卡状态，使与队列中的不一致
	answer.Card.State = RootPermissionCardStateActive
	q.ReleaseAnswer(answer)

	got, _ := q.Get(card.Key)
	if got.State != RootPermissionCardStateResuming {
		t.Fatalf("不应改动，实际 %q", got.State)
	}
}

// TestClaimAnswerForCall_正常 验证 resuming 被认领为 active
func TestClaimAnswerForCall_正常(t *testing.T) {
	q, card := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	}); err != nil {
		t.Fatalf("预留失败: %v", err)
	}

	claim, err := q.ClaimAnswerForCall("s", "e", "c", "tool")
	if err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if claim.Card.State != RootPermissionCardStateActive {
		t.Fatalf("应为 active")
	}
	if claim.Quarantined {
		t.Fatalf("不应隔离")
	}
}

// TestClaimAnswerForCall_未预留 验证未预留时认领报错
func TestClaimAnswerForCall_未预留(t *testing.T) {
	q := newTestQueue()
	mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.ClaimAnswerForCall("s", "e", "c", "tool"); err == nil {
		t.Fatalf("未预留应报错")
	}
}

// TestClaimAnswerForCall_工具名不符 验证工具名不匹配时报错
func TestClaimAnswerForCall_工具名不符(t *testing.T) {
	q, card := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	}); err != nil {
		t.Fatalf("预留失败: %v", err)
	}
	if _, err := q.ClaimAnswerForCall("s", "e", "c", "other"); err == nil {
		t.Fatalf("工具名不符应报错")
	}
}

// TestClaimAnswerForCall_重复认领 验证同 active 键重复认领报错
func TestClaimAnswerForCall_重复认领(t *testing.T) {
	q, card := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	}); err != nil {
		t.Fatalf("预留失败: %v", err)
	}
	if _, err := q.ClaimAnswerForCall("s", "e", "c", "tool"); err != nil {
		t.Fatalf("首次认领失败: %v", err)
	}
	// 人为把 pending 索引补回，模拟重复认领
	q.pending[pendingCallKey{"s", "e", "c"}] = card.Key.InvocationID
	if _, err := q.ClaimAnswerForCall("s", "e", "c", "tool"); err == nil {
		t.Fatalf("重复认领应报错")
	}
}

// TestClaimAnswerForCall_已隔离 验证隔离会话认领时 Quarantined 为 true
func TestClaimAnswerForCall_已隔离(t *testing.T) {
	q, card := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	}); err != nil {
		t.Fatalf("预留失败: %v", err)
	}
	q.Quarantine("s")

	claim, err := q.ClaimAnswerForCall("s", "e", "c", "tool")
	if err != nil {
		t.Fatalf("认领失败: %v", err)
	}
	if !claim.Quarantined {
		t.Fatalf("应标记已隔离")
	}
}

// TestConsumeAcceptedAnswerIfQuarantined 验证隔离时可消费未认领答案
func TestConsumeAcceptedAnswerIfQuarantined(t *testing.T) {
	q, card := preparedQueue(t)
	answer, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	if err != nil {
		t.Fatalf("预留失败: %v", err)
	}

	// 未隔离 → false
	if q.ConsumeAcceptedAnswerIfQuarantined(answer) {
		t.Fatalf("未隔离应返回 false")
	}

	q.Quarantine("s")
	if !q.ConsumeAcceptedAnswerIfQuarantined(answer) {
		t.Fatalf("隔离后应返回 true")
	}
	if got, _ := q.Get(card.Key); got.Key != nil {
		t.Fatalf("卡应被删除")
	}
}

// TestConsumeAcceptedAnswerIfQuarantined_卡不匹配 验证卡不匹配返回 false
func TestConsumeAcceptedAnswerIfQuarantined_卡不匹配(t *testing.T) {
	q, card := preparedQueue(t)
	answer, _ := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	q.Quarantine("s")
	answer.Card.State = RootPermissionCardStateActive
	if q.ConsumeAcceptedAnswerIfQuarantined(answer) {
		t.Fatalf("不匹配应返回 false")
	}
}

// ──────────────────────────── Reconcile 测试 ────────────────────────────

// TestReconcile_非中断结果 验证 result_type 不是 interrupt 时返回 nil
func TestReconcile_非中断结果(t *testing.T) {
	q := newTestQueue()
	got, err := q.Reconcile("result", nil, nil, "s")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != nil {
		t.Fatalf("应返回 nil")
	}
}

// TestReconcile_快照非法 验证长度不一致/空/超上限时隔离并报错
func TestReconcile_快照非法(t *testing.T) {
	cases := []struct {
		name  string
		state []any
		ids   []any
	}{
		{"长度不一致", []any{map[string]any{}}, []any{}},
		{"空快照", []any{}, []any{}},
		{"超上限", make([]any, MaxRootPermissionCards+1), make([]any, MaxRootPermissionCards+1)},
	}
	for _, c := range cases {
		q := newTestQueue()
		if _, err := q.Reconcile("interrupt", c.state, c.ids, "s"); err == nil {
			t.Fatalf("%s 应报错", c.name)
		}
		if err := q.RaiseIfQuarantined("s"); err == nil {
			t.Fatalf("%s 应隔离会话", c.name)
		}
	}
}

// TestReconcile_跳过客户端工具 验证 CLIENT_TOOL 中断被跳过
func TestReconcile_跳过客户端工具(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}

	// 只含客户端工具中断 → 无权限卡 → 返回 nil
	got, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{
			"interrupt_kind": saschema.InterruptKindClientTool,
		})},
		[]any{"c"},
		"s",
	)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != nil {
		t.Fatalf("应返回 nil（无权限卡）")
	}
}

// TestReconcile_混入其他中断 验证同一批里混了非权限中断时报错
func TestReconcile_混入其他中断(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}

	_, err := q.Reconcile(
		"interrupt",
		[]any{
			interaction("c", map[string]any{
				"interrupt_kind": saschema.InterruptKindPermission,
				"metadata": map[string]any{
					"tool_invocation_key": wireKeyMap(t, card.Key.InvocationID, "s", "r", "e", "c"),
				},
			}),
			interaction("c2", map[string]any{"interrupt_kind": saschema.InterruptKindConfirm}),
		},
		[]any{"c", "c2"},
		"s",
	)
	if err == nil {
		t.Fatalf("混入其他中断应报错")
	}
}

// TestReconcile_身份不符 验证 wire key 的 tool_call_id 与内部 id 不符时报错
func TestReconcile_身份不符(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}

	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{
			"interrupt_kind": saschema.InterruptKindPermission,
			"metadata": map[string]any{
				// 故意用别的 tool_call_id
				"tool_invocation_key": wireKeyMap(t, card.Key.InvocationID, "s", "r", "e", "other"),
			},
		})},
		[]any{"c"},
		"s",
	)
	if err == nil {
		t.Fatalf("身份不符应报错")
	}
}

// TestReconcile_内部id不匹配 验证 interaction.id 与 interrupt_ids 不符时报错
func TestReconcile_内部id不匹配(t *testing.T) {
	q := newTestQueue()
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("wrong", map[string]any{})},
		[]any{"c"},
		"s",
	)
	if err == nil {
		t.Fatalf("内部 id 不匹配应报错")
	}
}

// TestReconcile_wireKey非法 验证 wire 缺字段时报错
func TestReconcile_wireKey非法(t *testing.T) {
	q := newTestQueue()
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{
			"metadata": map[string]any{"tool_invocation_key": map[string]any{"version": 1}},
		})},
		[]any{"c"},
		"s",
	)
	if err == nil {
		t.Fatalf("wire 缺字段应报错")
	}
}

// TestReconcile_重复invocation 验证两个相同 invocation_id 时报错
func TestReconcile_重复invocation(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}
	wire := wireKeyMap(t, card.Key.InvocationID, "s", "r", "e", "c")

	_, err := q.Reconcile(
		"interrupt",
		[]any{
			interaction("c", map[string]any{"metadata": map[string]any{"tool_invocation_key": wire}}),
			interaction("c", map[string]any{"metadata": map[string]any{"tool_invocation_key": wire}}),
		},
		[]any{"c", "c"},
		"s",
	)
	if err == nil {
		t.Fatalf("重复 invocation 应报错")
	}
}

// TestReconcile_卡状态非pending 验证队列里卡不是 pending 时报错
func TestReconcile_卡状态非pending(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	// 不 MarkPending，直接 reconcile
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{
			"metadata": map[string]any{
				"tool_invocation_key": wireKeyMap(t, card.Key.InvocationID, "s", "r", "e", "c"),
			},
		})},
		[]any{"c"},
		"s",
	)
	if err == nil {
		t.Fatalf("非 pending 应报错")
	}
}

// TestReconcile_兄弟卡缺失 验证 pending 兄弟卡未出现在快照里时报错
func TestReconcile_兄弟卡缺失(t *testing.T) {
	q := newTestQueue()
	card1 := mustBegin(t, q, "s", "r", "e", "c1", "tool")
	card2 := mustBegin(t, q, "s", "r", "e", "c2", "tool")
	for _, c := range []RootPermissionCard{card1, card2} {
		if _, err := q.MarkPending(c.Key, testSnapshot(), false, nil); err != nil {
			t.Fatalf("失败: %v", err)
		}
	}

	// 快照只包含 card1
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c1", map[string]any{
			"metadata": map[string]any{
				"tool_invocation_key": wireKeyMap(t, card1.Key.InvocationID, "s", "r", "e", "c1"),
			},
		})},
		[]any{"c1"},
		"s",
	)
	if err == nil {
		t.Fatalf("兄弟卡缺失应报错")
	}
}

// TestReconcile_多卡正常 验证两张卡都 pending 且都在快照里时成功
func TestReconcile_多卡正常(t *testing.T) {
	q := newTestQueue()
	card1 := mustBegin(t, q, "s", "r", "e", "c1", "tool")
	card2 := mustBegin(t, q, "s", "r", "e", "c2", "tool")
	for _, c := range []RootPermissionCard{card1, card2} {
		if _, err := q.MarkPending(c.Key, testSnapshot(), false, nil); err != nil {
			t.Fatalf("失败: %v", err)
		}
	}

	snap, err := q.Reconcile(
		"interrupt",
		[]any{
			interaction("c1", map[string]any{"metadata": map[string]any{
				"tool_invocation_key": wireKeyMap(t, card1.Key.InvocationID, "s", "r", "e", "c1"),
			}}),
			interaction("c2", map[string]any{"metadata": map[string]any{
				"tool_invocation_key": wireKeyMap(t, card2.Key.InvocationID, "s", "r", "e", "c2"),
			}}),
		},
		[]any{"c1", "c2"},
		"s",
	)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(snap.Cards) != 2 {
		t.Fatalf("应有 2 张卡，实际 %d", len(snap.Cards))
	}
	if q.order["s"][0] != card1.Key.InvocationID {
		t.Fatalf("队首应为 card1")
	}
}

// TestReconcile_孤儿resuming 验证会话内有 resuming 卡但不在快照里时报错
func TestReconcile_孤儿resuming(t *testing.T) {
	q, card := preparedQueue(t)
	// 让 card 处于 resuming
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	}); err != nil {
		t.Fatalf("预留失败: %v", err)
	}

	// 再来一张 pending 卡
	card2 := mustBegin(t, q, "s", "r", "e", "c2", "tool")
	if _, err := q.MarkPending(card2.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}

	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c2", map[string]any{"metadata": map[string]any{
			"tool_invocation_key": wireKeyMap(t, card2.Key.InvocationID, "s", "r", "e", "c2"),
		}})},
		[]any{"c2"},
		"s",
	)
	if err == nil {
		t.Fatalf("孤儿 resuming 应报错")
	}
}

// ──────────────────────────── Get / Finish 测试 ────────────────────────────

// TestGet_命中与未命中 验证精确取卡
func TestGet_命中与未命中(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")

	got, ok := q.Get(card.Key)
	if !ok || got.Key.InvocationID != card.Key.InvocationID {
		t.Fatalf("应命中")
	}

	other, _ := NewToolInvocationKeyV1("other", "s", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion)
	if _, ok := q.Get(other); ok {
		t.Fatalf("不存在应未命中")
	}

	// 号码牌存在但内容不符
	tampered, _ := NewToolInvocationKeyV1(card.Key.InvocationID, "s", "r", ExecutorKindAgent, "e", "zzz", ToolInvocationKeyVersion)
	if _, ok := q.Get(tampered); ok {
		t.Fatalf("内容不符应未命中")
	}
}

// TestFinish_正常与不匹配 验证删除语义
func TestFinish_正常与不匹配(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")

	other, _ := NewToolInvocationKeyV1(card.Key.InvocationID, "s", "r", ExecutorKindAgent, "e", "zzz", ToolInvocationKeyVersion)
	if q.Finish(other) {
		t.Fatalf("内容不符应返回 false")
	}
	if !q.Finish(card.Key) {
		t.Fatalf("应删除成功")
	}
	if _, ok := q.Get(card.Key); ok {
		t.Fatalf("应已删除")
	}
}

// TestFinishActive_只删active 验证 pending 状态不被删除
func TestFinishActive_只删active(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if q.FinishActive(card.Key) {
		t.Fatalf("pending 状态不应被 FinishActive 删除")
	}

	q2 := newTestQueue()
	card2 := mustBegin(t, q2, "s", "r", "e", "c", "tool")
	if !q2.FinishActive(card2.Key) {
		t.Fatalf("active 应删除成功")
	}
}

// ──────────────────────────── HasLive / SnapshotScope 测试 ────────────────────────────

// TestHasLive_按会话过滤 验证有卡为 true、无卡为 false、空会话不限
func TestHasLive_按会话过滤(t *testing.T) {
	q := newTestQueue()
	if q.HasLive("") {
		t.Fatalf("空队列应返回 false")
	}
	mustBegin(t, q, "s1", "r", "e", "c", "tool")
	if !q.HasLive("s1") {
		t.Fatalf("s1 应有卡")
	}
	if q.HasLive("s2") {
		t.Fatalf("s2 不应有卡")
	}
	if !q.HasLive("") {
		t.Fatalf("空会话应返回 true")
	}
}

// TestSnapshotScope_按请求过滤 验证可按 request_id 收窄
func TestSnapshotScope_按请求过滤(t *testing.T) {
	q := newTestQueue()
	mustBegin(t, q, "s", "r1", "e", "c1", "tool")
	mustBegin(t, q, "s", "r2", "e", "c2", "tool")
	mustBegin(t, q, "other", "r1", "e", "c3", "tool")

	if got := q.SnapshotScope("s", nil); len(got) != 2 {
		t.Fatalf("s 应有 2 张，实际 %d", len(got))
	}
	req := "r1"
	if got := q.SnapshotScope("s", &req); len(got) != 1 {
		t.Fatalf("r1 应有 1 张，实际 %d", len(got))
	}
	if got := q.SnapshotScope("nobody", nil); len(got) != 0 {
		t.Fatalf("无卡会话应为空")
	}
}

// ──────────────────────────── Cutover 测试 ────────────────────────────

// TestBeginCutover_空会话 验证无卡会话返回 false
func TestBeginCutover_空会话(t *testing.T) {
	q := newTestQueue()
	if q.BeginCutover("s") {
		t.Fatalf("无卡应返回 false")
	}
}

// TestBeginCutover_有卡会话 验证有卡时隔离并返回 true，重复调用仍 true
func TestBeginCutover_有卡会话(t *testing.T) {
	q := newTestQueue()
	mustBegin(t, q, "s", "r", "e", "c", "tool")
	if !q.BeginCutover("s") {
		t.Fatalf("有卡应返回 true")
	}
	if err := q.RaiseIfQuarantined("s"); err == nil {
		t.Fatalf("应已隔离")
	}
	if !q.BeginCutover("s") {
		t.Fatalf("重复调用应仍返回 true")
	}
}

// TestCutoverContinuationScope_未开始 验证未 cutover 时报错
func TestCutoverContinuationScope_未开始(t *testing.T) {
	q := newTestQueue()
	if _, err := q.CutoverContinuationScope("s"); err == nil {
		t.Fatalf("未 cutover 应报错")
	}
}

// TestCutoverContinuationScope_有active 验证仍有 active 卡时报错
func TestCutoverContinuationScope_有active(t *testing.T) {
	q := newTestQueue()
	mustBegin(t, q, "s", "r", "e", "c", "tool")
	q.Quarantine("s")
	if _, err := q.CutoverContinuationScope("s"); err == nil {
		t.Fatalf("有 active 应报错")
	}
}

// TestCutoverContinuationScope_有resuming 验证仍有 resuming 卡时报错
func TestCutoverContinuationScope_有resuming(t *testing.T) {
	q, card := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	}); err != nil {
		t.Fatalf("预留失败: %v", err)
	}
	if _, err := q.CutoverContinuationScope("s"); err == nil {
		t.Fatalf("有 resuming 应报错")
	}
}

// TestCutoverContinuationScope_正常 验证全 pending 时返回全部 key
func TestCutoverContinuationScope_正常(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if _, err := q.MarkPending(card.Key, testSnapshot(), false, nil); err != nil {
		t.Fatalf("失败: %v", err)
	}
	q.Quarantine("s")

	keys, err := q.CutoverContinuationScope("s")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("应有 1 个 key，实际 %d", len(keys))
	}
}

// ──────────────────────────── CancelSnapshot / DiscardContinuation 测试 ────────────────────────────

// TestCancelSnapshot_计数 验证只删匹配的卡并返回数量
func TestCancelSnapshot_计数(t *testing.T) {
	q := newTestQueue()
	c1 := mustBegin(t, q, "s", "r", "e", "c1", "tool")
	mustBegin(t, q, "s", "r", "e", "c2", "tool")

	other, _ := NewToolInvocationKeyV1("nope", "s", "r", ExecutorKindAgent, "e", "c1", ToolInvocationKeyVersion)
	removed := q.CancelSnapshot([]*ToolInvocationKeyV1{c1.Key, other})
	if removed != 1 {
		t.Fatalf("应删 1 张，实际 %d", removed)
	}
}

// TestDiscardContinuation_未开始 验证未 cutover 时报错
func TestDiscardContinuation_未开始(t *testing.T) {
	q := newTestQueue()
	if _, err := q.DiscardContinuation(nil, "s"); err == nil {
		t.Fatalf("未 cutover 应报错")
	}
}

// TestDiscardContinuation_范围变化 验证 live 与 expected 不一致时报错
func TestDiscardContinuation_范围变化(t *testing.T) {
	q := newTestQueue()
	mustBegin(t, q, "s", "r", "e", "c", "tool")
	q.Quarantine("s")
	if _, err := q.DiscardContinuation(nil, "s"); err == nil {
		t.Fatalf("范围变化应报错")
	}
}

// TestDiscardContinuation_正常 验证删除并解除隔离
func TestDiscardContinuation_正常(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	q.Quarantine("s")

	removed, err := q.DiscardContinuation([]*ToolInvocationKeyV1{card.Key}, "s")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if removed != 1 {
		t.Fatalf("应删 1 张，实际 %d", removed)
	}
	if err := q.RaiseIfQuarantined("s"); err != nil {
		t.Fatalf("应已解除隔离: %v", err)
	}
}

// ──────────────────────────── 隔离测试 ────────────────────────────

// TestQuarantine与Raise 验证隔离后报错
func TestQuarantine与Raise(t *testing.T) {
	q := newTestQueue()
	if err := q.RaiseIfQuarantined("s"); err != nil {
		t.Fatalf("未隔离不应报错: %v", err)
	}
	q.Quarantine("s")
	err := q.RaiseIfQuarantined("s")
	if err == nil {
		t.Fatalf("应报错")
	}
	if !errIsRootQueue(err, "permission_queue_quarantined") {
		t.Fatalf("错误码不符: %v", err)
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestRootQueueText_失败转错误码 验证归一化失败时错误码格式
func TestRootQueueText_失败转错误码(t *testing.T) {
	_, err := rootQueueText("root_session_id", "", 512)
	if err == nil {
		t.Fatalf("应报错")
	}
	if !errIsRootQueue(err, "permission_queue_root_session_id_invalid") {
		t.Fatalf("错误码不符: %v", err)
	}
}

// TestCloneSnapshot_深拷贝 验证修改克隆不影响原快照
func TestCloneSnapshot_深拷贝(t *testing.T) {
	orig := testSnapshot()
	orig.PayloadSchema = map[string]any{"type": "object"}
	orig.UIOptions = []map[string]any{{"label": "ok"}}

	clone := cloneSnapshot(orig)
	clone.Metadata["k"] = "changed"
	clone.PayloadSchema["type"] = "changed"
	clone.UIOptions[0]["label"] = "changed"

	if orig.Metadata["k"] != "v" {
		t.Fatalf("Metadata 应深拷贝")
	}
	if orig.PayloadSchema["type"] != "object" {
		t.Fatalf("PayloadSchema 应深拷贝")
	}
	if orig.UIOptions[0]["label"] != "ok" {
		t.Fatalf("UIOptions 应深拷贝")
	}
}

// TestCloneSnapshot_nil 验证 nil 快照返回 nil
func TestCloneSnapshot_nil(t *testing.T) {
	if cloneSnapshot(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
}

// TestSnapshotEqual_各维度 验证字段差异能被识别
func TestSnapshotEqual_各维度(t *testing.T) {
	base := testSnapshot()
	if !snapshotEqual(base, testSnapshot()) {
		t.Fatalf("相同快照应相等")
	}
	if snapshotEqual(nil, testSnapshot()) || snapshotEqual(testSnapshot(), nil) {
		t.Fatalf("nil 与非 nil 不应相等")
	}
	if !snapshotEqual(nil, nil) {
		t.Fatalf("双 nil 应相等")
	}

	mutations := []func(*InterruptRequestSnapshot){
		func(s *InterruptRequestSnapshot) { s.Message = "x" },
		func(s *InterruptRequestSnapshot) { s.AutoConfirmKey = "x" },
		func(s *InterruptRequestSnapshot) { s.InterruptID = "x" },
		func(s *InterruptRequestSnapshot) { s.InterruptKind = "x" },
		func(s *InterruptRequestSnapshot) { s.InterruptOwner = "x" },
		func(s *InterruptRequestSnapshot) { s.Metadata = map[string]any{"other": 1} },
		func(s *InterruptRequestSnapshot) { s.PayloadSchema = map[string]any{"a": 1} },
		func(s *InterruptRequestSnapshot) { s.UIOptions = []map[string]any{{"a": 1}} },
	}
	for i, mutate := range mutations {
		changed := testSnapshot()
		mutate(changed)
		if snapshotEqual(base, changed) {
			t.Fatalf("mutation %d 后应不相等", i)
		}
	}
}

// TestCardEqual_各维度 验证卡片差异能被识别
func TestCardEqual_各维度(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if !cardEqual(card, card) {
		t.Fatalf("同卡应相等")
	}

	changed := card
	changed.ToolName = "other"
	if cardEqual(card, changed) {
		t.Fatalf("工具名不同应不相等")
	}

	changed2 := card
	changed2.State = RootPermissionCardStatePending
	if cardEqual(card, changed2) {
		t.Fatalf("状态不同应不相等")
	}
}

// TestKeyEqual_nil处理 验证 nil 组合
func TestKeyEqual_nil处理(t *testing.T) {
	if !keyEqual(nil, nil) {
		t.Fatalf("双 nil 应相等")
	}
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	if keyEqual(nil, card.Key) || keyEqual(card.Key, nil) {
		t.Fatalf("nil 与非 nil 不应相等")
	}
}

// TestStringSetEqual 验证集合比较
func TestStringSetEqual(t *testing.T) {
	a := map[string]struct{}{"x": {}, "y": {}}
	b := map[string]struct{}{"y": {}, "x": {}}
	if !stringSetEqual(a, b) {
		t.Fatalf("相同集合应相等")
	}
	if stringSetEqual(a, map[string]struct{}{"x": {}}) {
		t.Fatalf("长度不同不应相等")
	}
	if stringSetEqual(a, map[string]struct{}{"x": {}, "z": {}}) {
		t.Fatalf("内容不同不应相等")
	}
}

// TestActiveKeyOf 验证四元组构造
func TestActiveKeyOf(t *testing.T) {
	key, _ := NewToolInvocationKeyV1("i", "s", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion)
	got := activeKeyOf(key)
	if got.rootSessionID != "s" || got.requestID != "r" ||
		got.executionSessionID != "e" || got.toolCallID != "c" {
		t.Fatalf("四元组不符: %+v", got)
	}
}

// TestInteractionOf_各种形态 验证三种输入形态
func TestInteractionOf_各种形态(t *testing.T) {
	if got := interactionOf(map[string]any{"id": "a", "value": 1}); got["id"] != "a" {
		t.Fatalf("直接映射应识别: %v", got)
	}
	if got := interactionOf(map[string]any{"payload": map[string]any{"id": "b", "value": 2}}); got["id"] != "b" {
		t.Fatalf("payload 包装应识别: %v", got)
	}
	if got := interactionOf("string"); got != nil {
		t.Fatalf("非映射应返回 nil")
	}
	if got := interactionOf(map[string]any{"id": "a"}); got != nil {
		t.Fatalf("缺 value 应返回 nil")
	}
}

// TestInteractionOf_接口实现 验证实现 InteractionItem 的对象
func TestInteractionOf_接口实现(t *testing.T) {
	got := interactionOf(fakeInteractionItem{id: "  x  ", value: 42})
	if got["id"] != "x" {
		t.Fatalf("应 trim id，实际 %v", got["id"])
	}
	if got["value"] != 42 {
		t.Fatalf("value 不符")
	}
}

// fakeInteractionItem 测试用 InteractionItem 实现
type fakeInteractionItem struct {
	id    string
	value any
}

// InteractionID 实现接口
func (f fakeInteractionItem) InteractionID() string { return f.id }

// InteractionValue 实现接口
func (f fakeInteractionItem) InteractionValue() any { return f.value }

// TestToInteractionRequest_接口实现 验证实现 InteractionRequestProvider 的对象
func TestToInteractionRequest_接口实现(t *testing.T) {
	req := &saschema.InterruptRequest{
		InterruptKind: saschema.InterruptKindPermission,
		Metadata:      map[string]any{"tool_invocation_key": map[string]any{}},
	}
	got, ok := toInteractionRequest(req)
	if !ok {
		t.Fatalf("应识别")
	}
	if got.Kind != saschema.InterruptKindPermission {
		t.Fatalf("kind 不符: %q", got.Kind)
	}
	if got.Metadata == nil {
		t.Fatalf("metadata 应透传")
	}
}

// TestToInteractionRequest_映射形态 验证映射形态读取 kind 与 metadata
func TestToInteractionRequest_映射形态(t *testing.T) {
	got, ok := toInteractionRequest(map[string]any{
		"interrupt_kind": saschema.InterruptKindAskUser,
		"metadata":       map[string]any{"a": 1},
	})
	if !ok {
		t.Fatalf("应识别")
	}
	if got.Kind != saschema.InterruptKindAskUser || got.Metadata["a"] != 1 {
		t.Fatalf("字段不符: %+v", got)
	}
	if got.Value == nil {
		t.Fatalf("Value 应指向原映射")
	}
}

// TestToInteractionRequest_不支持类型 验证其它类型返回 false
func TestToInteractionRequest_不支持类型(t *testing.T) {
	if _, ok := toInteractionRequest("string"); ok {
		t.Fatalf("不支持类型应返回 false")
	}
}

// TestRequestKeyOf_两处查找 验证 metadata 优先、回退 value
func TestRequestKeyOf_两处查找(t *testing.T) {
	key := map[string]any{"invocation_id": "i"}

	fromMeta := &interactionRequest{Metadata: map[string]any{"tool_invocation_key": key}}
	if got := requestKeyOf(fromMeta); got["invocation_id"] != "i" {
		t.Fatalf("应从 metadata 读取")
	}

	fromValue := &interactionRequest{Value: map[string]any{"tool_invocation_key": key}}
	if got := requestKeyOf(fromValue); got["invocation_id"] != "i" {
		t.Fatalf("应从 value 读取")
	}

	if got := requestKeyOf(&interactionRequest{}); got != nil {
		t.Fatalf("都无应返回 nil")
	}

	notMap := &interactionRequest{Metadata: map[string]any{"tool_invocation_key": "str"}}
	if got := requestKeyOf(notMap); got != nil {
		t.Fatalf("非映射应返回 nil")
	}
}

// TestDefaultInvocationIDFactory_唯一性 验证生成的前缀与唯一性
func TestDefaultInvocationIDFactory_唯一性(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := defaultInvocationIDFactory()
		if !strings.HasPrefix(id, "tiv_") {
			t.Fatalf("前缀不符: %q", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("出现重复: %q", id)
		}
		seen[id] = struct{}{}
	}
}

// TestRootPermissionQueueError_Error 验证错误文本为错误码
func TestRootPermissionQueueError_Error(t *testing.T) {
	err := &RootPermissionQueueError{Code: "permission_queue_x"}
	if err.Error() != "permission_queue_x" {
		t.Fatalf("Error() 应返回错误码，实际 %q", err.Error())
	}
}

// TestErrIsRootQueue_非队列错误 验证普通错误返回 false
func TestErrIsRootQueue_非队列错误(t *testing.T) {
	if errIsRootQueue(nil, "x") {
		t.Fatalf("nil 应返回 false")
	}
	if errIsRootQueue(&RootPermissionQueueError{Code: "a"}, "b") {
		t.Fatalf("错误码不同应返回 false")
	}
}

// ──────────────────────────── 补充分支覆盖 ────────────────────────────

// TestBegin_号码牌非法 验证工厂返回非法号码牌时报错
func TestBegin_号码牌非法(t *testing.T) {
	q := NewRootPermissionQueue(func() string { return "" })
	if _, err := q.Begin("s", "r", "e", "c", "tool"); err == nil {
		t.Fatalf("空号码牌应报错")
	}
}

// TestPendingForCall_卡不存在 验证 pending 索引存在但卡已删除时返回 false
func TestPendingForCall_卡不存在(t *testing.T) {
	q := newTestQueue()
	q.pending[pendingCallKey{"s", "e", "c"}] = "ghost"
	if _, ok := q.PendingForCall("s", "e", "c"); ok {
		t.Fatalf("卡不存在应返回 false")
	}
}

// TestReserveAnswer_cardID非法 验证 card_id 为空时报错
func TestReserveAnswer_cardID非法(t *testing.T) {
	q, _ := preparedQueue(t)
	if _, err := q.ReserveAnswer("s", nil, map[string]any{
		"  ": map[string]any{"approved": true, "auto_confirm": false},
	}); err == nil {
		t.Fatalf("空 card_id 应报错")
	}
}

// TestClaimAnswerForCall_字段非法 验证四个字段逐个非法时报错
func TestClaimAnswerForCall_字段非法(t *testing.T) {
	q := newTestQueue()
	cases := [][4]string{
		{"", "e", "c", "tool"},
		{"s", "", "c", "tool"},
		{"s", "e", "", "tool"},
		{"s", "e", "c", ""},
	}
	for _, c := range cases {
		if _, err := q.ClaimAnswerForCall(c[0], c[1], c[2], c[3]); err == nil {
			t.Fatalf("非法字段应报错: %+v", c)
		}
	}
}

// TestConsumeAcceptedAnswerIfQuarantined_卡不存在 验证卡已删除时返回 false
func TestConsumeAcceptedAnswerIfQuarantined_卡不存在(t *testing.T) {
	q, card := preparedQueue(t)
	answer, _ := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	q.Quarantine("s")
	// 删掉卡
	delete(q.cards, card.Key.InvocationID)
	if q.ConsumeAcceptedAnswerIfQuarantined(answer) {
		t.Fatalf("卡不存在应返回 false")
	}
}

// TestConsumeAcceptedAnswerIfQuarantined_非resuming 验证状态非 resuming 时返回 false
func TestConsumeAcceptedAnswerIfQuarantined_非resuming(t *testing.T) {
	q, card := preparedQueue(t)
	answer, _ := q.ReserveAnswer("s", nil, map[string]any{
		card.Key.InvocationID: map[string]any{"approved": true, "auto_confirm": false},
	})
	q.Quarantine("s")
	pending := answer.Card
	pending.State = RootPermissionCardStatePending
	q.cards[card.Key.InvocationID] = pending
	answer.Card = pending
	if q.ConsumeAcceptedAnswerIfQuarantined(answer) {
		t.Fatalf("非 resuming 应返回 false")
	}
}

// TestReconcile_rootSession非法 验证 root_session_id 为空时报错
func TestReconcile_rootSession非法(t *testing.T) {
	q := newTestQueue()
	if _, err := q.Reconcile("interrupt", []any{}, []any{}, ""); err == nil {
		t.Fatalf("空会话应报错")
	}
}

// TestReconcile_interruptID非法 验证 interrupt_ids 元素为空时报错
func TestReconcile_interruptID非法(t *testing.T) {
	q := newTestQueue()
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{})},
		[]any{"  "},
		"s",
	)
	if err == nil {
		t.Fatalf("空 interrupt_id 应报错")
	}
}

// TestReconcile_请求形态非法 验证 value 非法时报错
func TestReconcile_请求形态非法(t *testing.T) {
	q := newTestQueue()
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", "string-value")},
		[]any{"c"},
		"s",
	)
	if err == nil {
		t.Fatalf("非法请求形态应报错")
	}
}

// TestReconcile_卡不存在 验证 exactCardLocked 失败时隔离并报错
func TestReconcile_卡不存在(t *testing.T) {
	q := newTestQueue()
	wire := wireKeyMap(t, "ghost-id", "s", "r", "e", "c")
	_, err := q.Reconcile(
		"interrupt",
		[]any{interaction("c", map[string]any{
			"metadata": map[string]any{"tool_invocation_key": wire},
		})},
		[]any{"c"},
		"s",
	)
	if err == nil {
		t.Fatalf("卡不存在应报错")
	}
	if err := q.RaiseIfQuarantined("s"); err == nil {
		t.Fatalf("应隔离")
	}
}

// TestCutoverContinuationScope_空会话 验证无卡会话返回空切片
func TestCutoverContinuationScope_空会话(t *testing.T) {
	q := newTestQueue()
	q.Quarantine("s")
	keys, err := q.CutoverContinuationScope("s")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("应为空，实际 %d", len(keys))
	}
}

// TestDiscardContinuation_跨会话key 验证不属于该会话的 key 被跳过
func TestDiscardContinuation_跨会话key(t *testing.T) {
	q := newTestQueue()
	card := mustBegin(t, q, "s", "r", "e", "c", "tool")
	q.Quarantine("s")

	other, _ := NewToolInvocationKeyV1("x", "other-session", "r", ExecutorKindAgent, "e", "c", ToolInvocationKeyVersion)
	removed, err := q.DiscardContinuation([]*ToolInvocationKeyV1{card.Key, other}, "s")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if removed != 1 {
		t.Fatalf("应只删 1 张，实际 %d", removed)
	}
}

// TestRemoveLocked_清理order 验证删除后 order 被同步清理
func TestRemoveLocked_清理order(t *testing.T) {
	q := newTestQueue()
	c1 := mustBegin(t, q, "s", "r", "e", "c1", "tool")
	c2 := mustBegin(t, q, "s", "r", "e", "c2", "tool")
	q.order["s"] = []string{c1.Key.InvocationID, c2.Key.InvocationID}

	q.Finish(c2.Key)
	if len(q.order["s"]) != 1 || q.order["s"][0] != c1.Key.InvocationID {
		t.Fatalf("order 应只剩 c1: %v", q.order["s"])
	}
	q.Finish(c1.Key)
	if _, exists := q.order["s"]; exists {
		t.Fatalf("order 应被删除")
	}
}

// TestToInteractionRequest_接口实现非映射 验证 provider 实现但非映射时 Value 为 nil
func TestToInteractionRequest_接口实现非映射(t *testing.T) {
	got, ok := toInteractionRequest(fakeRequestProvider{kind: "permission"})
	if !ok {
		t.Fatalf("应识别")
	}
	if got.Kind != "permission" {
		t.Fatalf("kind 不符")
	}
	if got.Value != nil {
		t.Fatalf("非映射时 Value 应为 nil")
	}
}

// fakeRequestProvider 测试用 InteractionRequestProvider 实现
type fakeRequestProvider struct {
	kind     string
	metadata map[string]any
}

// RequestInterruptKind 实现接口
func (f fakeRequestProvider) RequestInterruptKind() string { return f.kind }

// RequestMetadata 实现接口
func (f fakeRequestProvider) RequestMetadata() map[string]any { return f.metadata }

// TestMapEqual_差异 验证 map 长度与内容差异
func TestMapEqual_差异(t *testing.T) {
	if !mapEqual(nil, nil) {
		t.Fatalf("双 nil 应相等")
	}
	if mapEqual(map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2}) {
		t.Fatalf("长度不同不应相等")
	}
	if mapEqual(map[string]any{"a": 1}, map[string]any{"b": 1}) {
		t.Fatalf("键不同不应相等")
	}
	if mapEqual(map[string]any{"a": 1}, map[string]any{"a": 2}) {
		t.Fatalf("值不同不应相等")
	}
	if !mapEqual(map[string]any{"a": 1}, map[string]any{"a": 1}) {
		t.Fatalf("相同应相等")
	}
}

// TestUIOptionsEqual_差异 验证选项列表差异
func TestUIOptionsEqual_差异(t *testing.T) {
	if !uiOptionsEqual(nil, nil) {
		t.Fatalf("双 nil 应相等")
	}
	if uiOptionsEqual([]map[string]any{{"a": 1}}, nil) {
		t.Fatalf("长度不同不应相等")
	}
	if uiOptionsEqual([]map[string]any{{"a": 1}}, []map[string]any{{"a": 2}}) {
		t.Fatalf("内容不同不应相等")
	}
	if !uiOptionsEqual([]map[string]any{{"a": 1}}, []map[string]any{{"a": 1}}) {
		t.Fatalf("相同应相等")
	}
}

// TestSnapshotEqual_数值型metadata 验证 metadata 值为非标量时不 panic
func TestSnapshotEqual_数值型metadata(t *testing.T) {
	a := &InterruptRequestSnapshot{Metadata: map[string]any{"n": 1}}
	b := &InterruptRequestSnapshot{Metadata: map[string]any{"n": 1}}
	if !snapshotEqual(a, b) {
		t.Fatalf("应相等")
	}
	c := &InterruptRequestSnapshot{Metadata: map[string]any{"n": 2}}
	if snapshotEqual(a, c) {
		t.Fatalf("应不相等")
	}
}
