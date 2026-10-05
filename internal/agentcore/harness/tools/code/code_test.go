package code

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── resolveTimeout 测试 ────────────────────────────

// TestResolveTimeout_默认值 测试零值使用默认超时
func TestResolveTimeout_默认值(t *testing.T) {
	if resolveTimeout(0) != defaultTimeout {
		t.Errorf("零值应使用默认超时 %d，实际 %d", defaultTimeout, resolveTimeout(0))
	}
}

// TestResolveTimeout_正常值 测试正常超时值
func TestResolveTimeout_正常值(t *testing.T) {
	if resolveTimeout(120) != 120 {
		t.Errorf("正常值 120 应保持不变，实际 %d", resolveTimeout(120))
	}
}

// TestResolveTimeout_负值 测试负值使用默认超时
func TestResolveTimeout_负值(t *testing.T) {
	if resolveTimeout(-5) != defaultTimeout {
		t.Errorf("负值应使用默认超时 %d，实际 %d", defaultTimeout, resolveTimeout(-5))
	}
}

// TestResolveTimeout_超大值 测试超大值钳制
func TestResolveTimeout_超大值(t *testing.T) {
	if resolveTimeout(99999) > defaultMaxTimeout {
		t.Errorf("超大值应钳制到最大超时 %d", defaultMaxTimeout)
	}
}

// ──────────────────────────── NewCodeTool 测试 ────────────────────────────

// TestNewCodeTool 测试创建 CodeTool 实例
func TestNewCodeTool(t *testing.T) {
	op := &sys_operation.BaseSysOperation{}
	tool := NewCodeTool(op, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewCodeTool 不应返回 nil")
	}
}
