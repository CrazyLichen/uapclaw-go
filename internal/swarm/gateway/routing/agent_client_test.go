package routing

import "testing"

// ──────────────────────────── AgentClient 测试 ────────────────────────────

// TestNewAgentClient 创建AgentClient实例
func TestNewAgentClient(t *testing.T) {
	ac := NewAgentClient()
	if ac == nil {
		t.Fatal("NewAgentClient() 返回 nil，期望非 nil")
	}
}

// TestAgentClient_ServerReady 骨架实现返回false
func TestAgentClient_ServerReady(t *testing.T) {
	ac := NewAgentClient()
	if ac.ServerReady() {
		t.Error("ServerReady() 返回 true，期望 false（骨架实现）")
	}
}
