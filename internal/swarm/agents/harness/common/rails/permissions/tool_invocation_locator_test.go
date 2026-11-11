package permissions

import (
	"testing"
)

// newLocatorQueue 构造带一张 pending 卡的队列，返回队列与该卡的 wire key。
func newLocatorQueue(t *testing.T) (*RootPermissionQueue, *ToolInvocationKeyV1, map[string]any) {
	t.Helper()
	queue := NewRootPermissionQueue(nil)
	key, err := NewToolInvocationKeyV1("inv-1", "root-sess", "req-1", "agent", "exec-sess", "tc-1", 1)
	if err != nil {
		t.Fatalf("construct key: %v", err)
	}
	queue.cards[key.InvocationID] = RootPermissionCard{
		Key:   key,
		State: RootPermissionCardStatePending,
	}
	wire := map[string]any{
		"version":              1,
		"invocation_id":        "inv-1",
		"root_session_id":      "root-sess",
		"request_id":           "req-1",
		"executor_kind":        "agent",
		"execution_session_id": "exec-sess",
		"tool_call_id":         "tc-1",
	}
	return queue, key, wire
}

// TestToolInvocationLocator_无key返回absent 测试判据 1。
func TestToolInvocationLocator_无key返回absent(t *testing.T) {
	queue, _, _ := newLocatorQueue(t)
	state, card := ToolInvocationLocatorFromInteraction(map[string]any{}, map[string]any{}, queue)
	if state != ToolLocatorStateAbsent {
		t.Errorf("state = %q, want absent", state)
	}
	if card != nil {
		t.Error("card 应为 nil")
	}
}

// TestToolInvocationLocator_队列为nil返回invalid 测试判据 2。
func TestToolInvocationLocator_队列为nil返回invalid(t *testing.T) {
	interaction := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": map[string]any{"x": 1}},
	}
	state, _ := ToolInvocationLocatorFromInteraction(interaction, map[string]any{}, nil)
	if state != ToolLocatorStateInvalid {
		t.Errorf("state = %q, want invalid（队列不存在）", state)
	}
}

// TestToolInvocationLocator_key格式非法返回invalid 测试判据 3。
func TestToolInvocationLocator_key格式非法返回invalid(t *testing.T) {
	queue, _, _ := newLocatorQueue(t)
	interaction := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": "not_a_dict"},
	}
	state, _ := ToolInvocationLocatorFromInteraction(interaction, map[string]any{}, queue)
	if state != ToolLocatorStateInvalid {
		t.Errorf("state = %q, want invalid（key 类型不对）", state)
	}
}

// TestToolInvocationLocator_卡片不存在返回invalid 测试队列里没这张卡。
func TestToolInvocationLocator_卡片不存在返回invalid(t *testing.T) {
	queue, _, _ := newLocatorQueue(t)
	interaction := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": map[string]any{
			"version":              1,
			"invocation_id":        "inv-missing",
			"root_session_id":      "root-sess",
			"request_id":           "req-1",
			"executor_kind":        "agent",
			"execution_session_id": "exec-sess",
			"tool_call_id":         "tc-1",
		}},
	}
	state, _ := ToolInvocationLocatorFromInteraction(interaction, map[string]any{}, queue)
	if state != ToolLocatorStateInvalid {
		t.Errorf("state = %q, want invalid（卡片不存在）", state)
	}
}

// TestToolInvocationLocator_卡片非pending返回invalid 测试状态校验。
func TestToolInvocationLocator_卡片非pending返回invalid(t *testing.T) {
	queue, key, wire := newLocatorQueue(t)
	card := queue.cards[key.InvocationID]
	card.State = "answered"
	queue.cards[key.InvocationID] = card

	interaction := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": wire},
	}
	state, _ := ToolInvocationLocatorFromInteraction(interaction, map[string]any{}, queue)
	if state != ToolLocatorStateInvalid {
		t.Errorf("state = %q, want invalid（卡片已非 pending）", state)
	}
}

// TestToolInvocationLocator_live命中 测试正常路径。
func TestToolInvocationLocator_live命中(t *testing.T) {
	queue, key, wire := newLocatorQueue(t)
	interaction := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": wire},
	}
	state, card := ToolInvocationLocatorFromInteraction(interaction, map[string]any{}, queue)
	if state != ToolLocatorStateLive {
		t.Fatalf("state = %q, want live", state)
	}
	if card == nil || card.Key.InvocationID != key.InvocationID {
		t.Errorf("card = %+v, want 命中 inv-1", card)
	}
}

// TestToolInvocationLocator_value上的key也生效 测试第二层来源。
func TestToolInvocationLocator_value上的key也生效(t *testing.T) {
	queue, _, wire := newLocatorQueue(t)
	value := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": wire},
	}
	state, _ := ToolInvocationLocatorFromInteraction(map[string]any{}, value, queue)
	if state != ToolLocatorStateLive {
		t.Errorf("state = %q, want live（value.metadata 里的 key 也生效）", state)
	}
}

// TestToolInvocationLocator_value覆盖interaction 测试合并优先级。
func TestToolInvocationLocator_value覆盖interaction(t *testing.T) {
	queue, _, wire := newLocatorQueue(t)
	interaction := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": map[string]any{
			"version":              1,
			"invocation_id":        "inv-missing",
			"root_session_id":      "root-sess",
			"request_id":           "req-1",
			"executor_kind":        "agent",
			"execution_session_id": "exec-sess",
			"tool_call_id":         "tc-1",
		}},
	}
	value := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": wire},
	}
	state, _ := ToolInvocationLocatorFromInteraction(interaction, value, queue)
	if state != ToolLocatorStateLive {
		t.Errorf("state = %q, want live（value.metadata 应覆盖 interaction.metadata）", state)
	}
}
