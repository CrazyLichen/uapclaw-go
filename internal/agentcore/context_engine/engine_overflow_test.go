package context_engine

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
)

// TestRecoverFromModelContextOverflow_流式已吐字拒绝 验证已向用户输出后不再重试。
func TestRecoverFromModelContextOverflow_流式已吐字拒绝(t *testing.T) {
	engine := &contextEngine{}
	recovered := engine.RecoverFromModelContextOverflow(
		context.Background(), nil, "ctx-missing", true, 3)
	if recovered {
		t.Fatal("流式已吐字时不允许恢复（重试会重发已发送前缀）")
	}
}

// TestRecoverFromModelContextOverflow_上下文不存在 验证压缩失败/无上下文时不重试。
func TestRecoverFromModelContextOverflow_上下文不存在(t *testing.T) {
	engine := NewContextEngine(schema.NewContextEngineConfig()).(*contextEngine)
	recovered := engine.RecoverFromModelContextOverflow(
		context.Background(), nil, "ctx-missing", false, 0)
	if recovered {
		t.Fatal("上下文不存在时压缩不会发生，应返回 false")
	}
}
