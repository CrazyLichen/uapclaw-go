package single_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNoopResourceManager_获取工具(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetTool("test_tool")
	if err == nil {
		t.Fatal("应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != exception.StatusAbilityNotFound.Code() {
		t.Errorf("Code = %d, want %d", baseErr.Code(), exception.StatusAbilityNotFound.Code())
	}
}

func TestNoopResourceManager_获取工作流(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetWorkflow("test_wf")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_获取Agent(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetAgent("test_agent")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_获取MCP工具信息(t *testing.T) {
	mgr := &NoopResourceManager{}
	infos, err := mgr.GetMcpToolInfos("server1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if infos != nil {
		t.Errorf("应返回 nil，实际 %v", infos)
	}
}

func TestNewResourceOptions_资源选项(t *testing.T) {
	opts := NewResourceOptions(
		WithResourceTag("my_tag"),
	)
	if opts.Tag != "my_tag" {
		t.Errorf("Tag = %q, want my_tag", opts.Tag)
	}
}

// TestWithResourceSession 验证 WithResourceSession 设置 Session。
func TestWithResourceSession(t *testing.T) {
	sess := &mockContextSession{}
	opts := NewResourceOptions(WithResourceSession(sess))
	if opts.Session != sess {
		t.Error("WithResourceSession 未正确设置 Session")
	}
}

// mockContextSession 用于测试的模拟 ContextSession。
type mockContextSession struct{}

func (m *mockContextSession) GetSessionID() string                    { return "test-session" }
func (m *mockContextSession) GetState(key string) (any, error)        { return nil, nil }
func (m *mockContextSession) UpdateState(state map[string]any)        {}

// 确认 mockContextSession 实现 context_engine.ContextSession 接口
var _ context_engine.ContextSession = (*mockContextSession)(nil)
