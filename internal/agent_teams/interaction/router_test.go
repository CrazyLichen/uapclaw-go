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

func TestParseInteractStr_广播all(t *testing.T) {
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

func TestParseInteractStr_广播星号(t *testing.T) {
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
	if msg.Body() != "broadcast msg" {
		t.Errorf("Body = %v, want broadcast msg", msg.Body())
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
	// 两个载荷的 Body 应相同
	if msg1.Body() != "hello team" || msg2.Body() != "hello team" {
		t.Errorf("Body 应为 hello team, got %q and %q", msg1.Body(), msg2.Body())
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

func TestParseInteractStr_井号加多接收者含广播(t *testing.T) {
	// 广播覆盖其他接收者
	result := ParseInteractStr("# @alice @all hello")
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1 (广播覆盖)", len(result))
	}
	msg, ok := result[0].(*OperatorMessage)
	if !ok {
		t.Fatal("应为 OperatorMessage")
	}
	if msg.Target() != nil {
		t.Errorf("Target = %v, want nil (broadcast)", msg.Target())
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

func TestParseMention_无空格(t *testing.T) {
	// @alice 后无空格，不应匹配
	_, _, ok := ParseMention("@alice")
	if ok {
		t.Error("@alice 后无空格不应匹配")
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
	// 对齐 Python: mentions = "@ghost", general_body = "@ghost hi"
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

func TestResolveTargets_广播透传(t *testing.T) {
	payloads := []InteractPayload{NewOperatorMessage("hi all", nil)} // nil target = broadcast
	check := func(name string) (bool, error) { return false, nil }
	result, err := ResolveTargets(payloads, check)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if _, ok := result[0].(*OperatorMessage); !ok {
		t.Error("广播 OperatorMessage 应透传")
	}
}

// ──────────── DeliverDirect 测试 ────────────

func TestDeliverDirect_未知成员(t *testing.T) {
	check := func(name string) (bool, error) { return false, nil }
	result, err := DeliverDirect("hi", "user", "ghost", nil, check)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("未知成员应返回失败")
	}
	if result.Reason == nil || *result.Reason != "unknown_member:ghost" {
		t.Errorf("Reason = %v, want unknown_member:ghost", result.Reason)
	}
}

func TestDeliverDirect_成功(t *testing.T) {
	check := func(name string) (bool, error) { return true, nil }
	result, err := DeliverDirect("hi", "user", "alice", nil, check)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Error("已知成员应返回成功")
	}
}

// ──────────── 辅助函数 ────────────

func strPtr(s string) *string { return &s }
