package sdk

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// TestAgentServerClientExtension_接口契约 测试接口实现
func TestAgentServerClientExtension_接口契约(t *testing.T) {
	var _ AgentServerClientExtension = (*mockAgentServerClientExt)(nil)
}

// TestAgentServerClientExtensionImpl_GetClient 测试 GetClient 返回 AgentTransport
func TestAgentServerClientExtensionImpl_GetClient(t *testing.T) {
	// 使用 ChannelTransport 作为测试中的 AgentTransport 实现
	chTransport := transport.NewChannelTransport()
	impl := &AgentServerClientExtensionImpl{
		client: chTransport,
	}
	client := impl.GetClient()
	if client == nil {
		t.Error("GetClient() = nil, want non-nil AgentTransport")
	}
}

// TestAgentServerClientExtensionImpl_Initialize 测试 Initialize 方法
func TestAgentServerClientExtensionImpl_Initialize(t *testing.T) {
	impl := &AgentServerClientExtensionImpl{}
	cfg := &extensions.ExtensionConfig{
		Config: map[string]any{"test": true},
	}
	err := impl.Initialize(context.Background(), cfg)
	if err != nil {
		t.Errorf("Initialize() error: %v", err)
	}
}

// TestAgentServerClientExtensionImpl_Shutdown 测试 Shutdown 方法
func TestAgentServerClientExtensionImpl_Shutdown(t *testing.T) {
	impl := &AgentServerClientExtensionImpl{}
	err := impl.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

// mockAgentServerClientExt 用于测试 AgentServerClientExtension 接口
type mockAgentServerClientExt struct {
	BaseExtensionImpl
	mockClient transport.AgentTransport
}

func (e *mockAgentServerClientExt) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

func (e *mockAgentServerClientExt) Shutdown(ctx context.Context) error {
	return nil
}

func (e *mockAgentServerClientExt) GetClient() transport.AgentTransport {
	return e.mockClient
}
