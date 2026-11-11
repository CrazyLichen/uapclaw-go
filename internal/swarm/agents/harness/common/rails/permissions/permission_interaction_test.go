package permissions

import (
	"testing"
	"time"
)

// TestContainsPermissionInteraction_顶层键命中 测试判据 3。
func TestContainsPermissionInteraction_顶层键命中(t *testing.T) {
	value := map[string]any{"tool_invocation_key": "abc"}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（顶层 tool_invocation_key）")
	}
}

// TestContainsPermissionInteraction_metadata键命中 测试判据 1。
func TestContainsPermissionInteraction_metadata键命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"tool_invocation_key": "abc"},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（metadata.tool_invocation_key）")
	}
}

// TestContainsPermissionInteraction_manualApproval为true命中 测试判据 2。
func TestContainsPermissionInteraction_manualApproval为true命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"manual_approval_supported": true},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（manual_approval_supported == true）")
	}
}

// TestContainsPermissionInteraction_manualApproval为数字1不命中 测试 is 语义。
//
// Python: metadata.get("manual_approval_supported") is True
// 1 == True 为真但 1 is True 为假，所以数字 1 不算。
func TestContainsPermissionInteraction_manualApproval为数字1不命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"manual_approval_supported": 1},
	}
	if ContainsPermissionInteraction(value) {
		t.Error("got true, want false（Python 用 is，1 不等于 True）")
	}
}

// TestContainsPermissionInteraction_manualApproval为字符串true不命中 测试类型严格。
func TestContainsPermissionInteraction_manualApproval为字符串true不命中(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"manual_approval_supported": "true"},
	}
	if ContainsPermissionInteraction(value) {
		t.Error("got true, want false（字符串 \"true\" 不是布尔 true）")
	}
}

// TestContainsPermissionInteraction_嵌套list命中 测试递归下钻。
func TestContainsPermissionInteraction_嵌套list命中(t *testing.T) {
	value := map[string]any{
		"state": []any{
			map[string]any{"foo": "bar"},
			map[string]any{"metadata": map[string]any{"tool_invocation_key": "k"}},
		},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（列表深层命中）")
	}
}

// TestContainsPermissionInteraction_无关结构不命中 测试普通中断不误判。
func TestContainsPermissionInteraction_无关结构不命中(t *testing.T) {
	value := map[string]any{
		"result_type": "interrupt",
		"state": []any{
			map[string]any{"question": "选哪个？", "options": []any{"A", "B"}},
		},
	}
	if ContainsPermissionInteraction(value) {
		t.Error("got true, want false（普通问答不含权限字段）")
	}
}

// TestContainsPermissionInteraction_循环引用不死循环 测试 id 去重。
func TestContainsPermissionInteraction_循环引用不死循环(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic

	done := make(chan bool, 1)
	go func() {
		ContainsPermissionInteraction(cyclic)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("循环引用导致死循环")
	}
}

// TestContainsPermissionInteraction_超深嵌套failSafe 测试扫满 128 层返回 true。
//
// Python: for _ in range(128) 跑完 → return bool(pending)
// 无法确认干净就按有权限交互处理（fail-safe 方向）。
func TestContainsPermissionInteraction_超深嵌套failSafe(t *testing.T) {
	var value any = map[string]any{"leaf": "x"}
	for i := 0; i < 200; i++ {
		value = map[string]any{"next": value}
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（扫满 128 层未完成应按有权限交互处理）")
	}
}

// TestContainsPermissionInteraction_结构体载体命中 测试对象分支。
func TestContainsPermissionInteraction_结构体载体命中(t *testing.T) {
	value := permissionInteractionCarrier{
		Metadata: map[string]any{"tool_invocation_key": "k"},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（带 Metadata 字段的对象）")
	}
}

// TestContainsPermissionInteraction_指针载体命中 测试指针对象分支。
func TestContainsPermissionInteraction_指针载体命中(t *testing.T) {
	value := &permissionInteractionCarrier{
		Metadata: map[string]any{"manual_approval_supported": true},
	}
	if !ContainsPermissionInteraction(value) {
		t.Error("got false, want true（指针对象的 Metadata 字段）")
	}
}

// TestContainsPermissionInteraction_基本类型不命中 测试标量。
func TestContainsPermissionInteraction_基本类型不命中(t *testing.T) {
	for _, value := range []any{"text", 42, 3.14, true, nil} {
		if ContainsPermissionInteraction(value) {
			t.Errorf("%v got true, want false", value)
		}
	}
}

// TestContainsPermissionInteraction_空容器不命中 测试空 map 与空列表。
func TestContainsPermissionInteraction_空容器不命中(t *testing.T) {
	if ContainsPermissionInteraction(map[string]any{}) {
		t.Error("空 map got true")
	}
	if ContainsPermissionInteraction([]any{}) {
		t.Error("空列表 got true")
	}
}

// TestPermissionRuntimeQuarantinedKey_字面量 测试键名与 Python 一字不差。
//
// Python: PERMISSION_RUNTIME_QUARANTINED_KEY =
//   "_jiuwenswarm_permission_runtime_quarantined_v1"
// 品牌前缀 jiuwenswarm 不映射为 uapclawswarm——它是已落盘的持久化键名。
func TestPermissionRuntimeQuarantinedKey_字面量(t *testing.T) {
	want := "_jiuwenswarm_permission_runtime_quarantined_v1"
	if PermissionRuntimeQuarantinedKey != want {
		t.Errorf("got %q, want %q", PermissionRuntimeQuarantinedKey, want)
	}
}

// permissionInteractionCarrier 测试用载体（带 Metadata 字段）。
type permissionInteractionCarrier struct {
	// Metadata 元数据
	Metadata map[string]any
}
