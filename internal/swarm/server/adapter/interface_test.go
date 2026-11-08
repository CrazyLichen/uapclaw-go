package adapter

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// stubAdapter 用于编译期验证 AgentAdapter 接口完整性。
// 如果 AgentAdapter 接口增减方法而 stubAdapter 未同步，编译将失败。
type stubAdapter struct{}

var _ AgentAdapter = (*stubAdapter)(nil)

func (s *stubAdapter) CreateInstance(_ context.Context, _ map[string]any, _ string, _ string) error {
	return nil
}

func (s *stubAdapter) ReloadAgentConfig(_ context.Context, _ map[string]any, _ map[string]any) error {
	return nil
}

func (s *stubAdapter) ProcessMessageImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) ProcessMessageStreamImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

func (s *stubAdapter) ProcessInterrupt(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) HandleUserAnswer(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) HandleHeartbeat(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) Cleanup() error {
	return nil
}

// HasSessionRuntime 桩：恒 false。
func (s *stubAdapter) HasSessionRuntime(string) bool { return false }

// CleanupSessionAdapter 桩：恒 false。
func (s *stubAdapter) CleanupSessionAdapter(string) bool { return false }

func (s *stubAdapter) Instance() *harness.DeepAgent {
	return nil
}

func (s *stubAdapter) SwitchMode(_ context.Context, _, _ string) error {
	return nil
}

// IsSessionScoped 桩：恒 false（本桩不模拟会话分身）。
func (s *stubAdapter) IsSessionScoped() bool { return false }

// MarkAsSessionScoped 桩：无操作。
func (s *stubAdapter) MarkAsSessionScoped(string) {}

// GetLiveSessionInstance 桩：恒 nil。
func (s *stubAdapter) GetLiveSessionInstance(string) *harness.DeepAgent { return nil }

// HandleSwarmflowReply 桩：返回未实现错误（方法体归团队域）。
func (s *stubAdapter) HandleSwarmflowReply(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, errors.New("swarmflow reply not implemented")
}

// PendingInteractions 桩：恒空。
func (s *stubAdapter) PendingInteractions(string) ([]map[string]any, error) { return nil, nil }

// TestAgentAdapter_编译期接口检查 验证 stubAdapter 满足 AgentAdapter 接口。
func TestAgentAdapter_编译期接口检查(t *testing.T) {
	// 此测试的目的是编译期检查：var _ AgentAdapter = (*stubAdapter)(nil)
	// 如果 stubAdapter 未完整实现 AgentAdapter，编译将失败。
	var a AgentAdapter = &stubAdapter{}
	_ = a
}

// TestAgentAdapter_接口含会话分身方法 编译期确认接口面已扩。
// 五方法来源：Python interface_deep.py 的三方法（:2110/:2711/:12744）
// 与 agent_adapters.py AgentAdapter Protocol 的两方法（:86/:94）。
func TestAgentAdapter_接口含会话分身方法(t *testing.T) {
	var a AgentAdapter = (*stubAdapter)(nil)
	var _ = a
	if _, ok := a.(interface{ IsSessionScoped() bool }); !ok {
		t.Error("AgentAdapter 应含 IsSessionScoped()")
	}
	if _, ok := a.(interface{ MarkAsSessionScoped(string) }); !ok {
		t.Error("AgentAdapter 应含 MarkAsSessionScoped(string)")
	}
	if _, ok := a.(interface {
		GetLiveSessionInstance(string) *harness.DeepAgent
	}); !ok {
		t.Error("AgentAdapter 应含 GetLiveSessionInstance(string)")
	}
	if _, ok := a.(interface {
		HandleSwarmflowReply(context.Context, *schema.AgentRequest) (*schema.AgentResponse, error)
	}); !ok {
		t.Error("AgentAdapter 应含 HandleSwarmflowReply")
	}
	if _, ok := a.(interface {
		PendingInteractions(string) ([]map[string]any, error)
	}); !ok {
		t.Error("AgentAdapter 应含 PendingInteractions")
	}
}
