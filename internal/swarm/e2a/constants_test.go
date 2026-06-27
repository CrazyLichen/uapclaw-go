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

// TestE2AA2AStreamBranches_长度 验证 A2A 流分支数量
func TestE2AA2AStreamBranches_长度(t *testing.T) {
	if len(E2AA2AStreamBranches) != 4 {
		t.Fatalf("期望 4 个分支，实际 %d", len(E2AA2AStreamBranches))
	}
}

// Test来源协议常量_值 验证来源协议常量值
func Test来源协议常量_值(t *testing.T) {
	if E2ASourceProtocolE2A != "e2a" {
		t.Errorf("E2ASourceProtocolE2A 期望 %q，实际 %q", "e2a", E2ASourceProtocolE2A)
	}
	if E2ASourceProtocolACP != "acp" {
		t.Errorf("E2ASourceProtocolACP 期望 %q，实际 %q", "acp", E2ASourceProtocolACP)
	}
	if E2ASourceProtocolA2A != "a2a" {
		t.Errorf("E2ASourceProtocolA2A 期望 %q，实际 %q", "a2a", E2ASourceProtocolA2A)
	}
}

// Test响应状态常量_值 验证响应状态常量值
func Test响应状态常量_值(t *testing.T) {
	if E2AResponseStatusSucceeded != "succeeded" {
		t.Errorf("E2AResponseStatusSucceeded 期望 %q，实际 %q", "succeeded", E2AResponseStatusSucceeded)
	}
	if E2AResponseStatusFailed != "failed" {
		t.Errorf("E2AResponseStatusFailed 期望 %q，实际 %q", "failed", E2AResponseStatusFailed)
	}
	if E2AResponseStatusInProgress != "in_progress" {
		t.Errorf("E2AResponseStatusInProgress 期望 %q，实际 %q", "in_progress", E2AResponseStatusInProgress)
	}
}
