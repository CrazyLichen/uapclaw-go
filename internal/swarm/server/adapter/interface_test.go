package adapter

import (
	"context"
	"testing"

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

func (s *stubAdapter) SwitchMode(_ context.Context, _, _ string) error {
	return nil
}

// TestAgentAdapter_编译期接口检查 验证 stubAdapter 满足 AgentAdapter 接口。
func TestAgentAdapter_编译期接口检查(t *testing.T) {
	// 此测试的目的是编译期检查：var _ AgentAdapter = (*stubAdapter)(nil)
	// 如果 stubAdapter 未完整实现 AgentAdapter，编译将失败。
	var a AgentAdapter = &stubAdapter{}
	_ = a
}
