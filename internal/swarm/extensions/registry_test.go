package extensions

import (
	"context"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// TestExtensionRegistry_CreateInstance 测试创建单例
func TestExtensionRegistry_CreateInstance(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	if reg == nil {
		t.Error("CreateInstance() = nil, want non-nil")
	}
	// 再次获取应返回同一实例
	reg2 := GetInstance()
	if reg2 != reg {
		t.Error("GetInstance() != CreateInstance() result, want same instance")
	}
	ResetInstance()
}

// TestExtensionRegistry_CreateInstance_重复调用 测试重复创建应返回 nil
func TestExtensionRegistry_CreateInstance_重复调用(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	_ = CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	second := CreateInstance(fw, map[string]any{}, nil)
	if second != nil {
		t.Error("second CreateInstance() should return nil when instance already exists")
	}
}

// TestExtensionRegistry_GetInstance_未初始化 测试未初始化时 GetInstanceErr 应返回错误
func TestExtensionRegistry_GetInstance_未初始化(t *testing.T) {
	ResetInstance()
	_, err := GetInstanceErr()
	if err == nil {
		t.Error("GetInstanceErr() should return error when registry not initialized")
	}
}

// TestExtensionRegistry_ResetInstance 测试重置单例
func TestExtensionRegistry_ResetInstance(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	_ = CreateInstance(fw, map[string]any{}, nil)

	ResetInstance()
	_, err := GetInstanceErr()
	if err == nil {
		t.Error("after ResetInstance(), GetInstanceErr() should return error")
	}
}

// TestExtensionRegistry_RegisterAgentServerClient 测试注册 AgentServerClient 扩展
func TestExtensionRegistry_RegisterAgentServerClient(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	chTransport := transport.NewChannelTransport()
	ext := &testAgentServerClientExt{client: chTransport}
	reg.RegisterAgentServerClient(ext)

	gotExt := reg.GetAgentServerClientExtension()
	if gotExt != ext {
		t.Error("GetAgentServerClientExtension() != registered extension")
	}

	gotClient := reg.GetAgentServerClient()
	if gotClient != chTransport {
		t.Error("GetAgentServerClient() != chTransport")
	}
}

// TestExtensionRegistry_RegisterAgentServerClient_无注册 测试未注册时返回 nil
func TestExtensionRegistry_RegisterAgentServerClient_无注册(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	ext := reg.GetAgentServerClientExtension()
	if ext != nil {
		t.Error("GetAgentServerClientExtension() should be nil when no extension registered")
	}

	client := reg.GetAgentServerClient()
	if client != nil {
		t.Error("GetAgentServerClient() should be nil when no extension registered")
	}
}

// TestExtensionRegistry_RegisterAndTrigger 测试回调注册和触发
func TestExtensionRegistry_RegisterAndTrigger(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	var triggered bool
	var mu sync.Mutex
	reg.Register(GatewayBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		mu.Lock()
		triggered = true
		mu.Unlock()
		return nil
	}, 100)

	ctx := context.Background()
	reg.Trigger(ctx, GatewayBeforeChatRequest, map[string]any{"test": true})

	mu.Lock()
	if !triggered {
		t.Error("callback was not triggered")
	}
	mu.Unlock()
}

// TestExtensionRegistry_Trigger_无上下文 测试 trigger 传 nil context 不触发回调
func TestExtensionRegistry_Trigger_无上下文(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	var triggered bool
	reg.Register(GatewayStarted, func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	}, 100)

	// nil context 不触发
	reg.Trigger(nil, GatewayStarted, nil) //nolint:staticcheck // SA1012: 故意传入 nil context 测试不触发行为
	if triggered {
		t.Error("callback should not trigger with nil context")
	}
}

// TestExtensionRegistry_Config 测试 Config 属性
func TestExtensionRegistry_Config(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{"key": "value"}, nil)
	defer ResetInstance()

	cfg := reg.Config()
	if cfg.Config["key"] != "value" {
		t.Errorf("Config[key] = %v, want %q", cfg.Config["key"], "value")
	}
}

// TestExtensionRegistry_Unregister 测试回调注销
func TestExtensionRegistry_Unregister(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	var callCount int
	handler := func(ctx context.Context, data map[string]any) any {
		callCount++
		return nil
	}
	reg.Register(GatewayStarted, handler, 100)
	reg.Unregister(GatewayStarted, handler)

	reg.Trigger(context.Background(), GatewayStarted, nil)
	if callCount != 0 {
		t.Errorf("callCount = %d, want 0 after unregister", callCount)
	}
}

// TestExtensionRegistry_RegisterCryptoUtility 测试注册 CryptoUtility 扩展
func TestExtensionRegistry_RegisterCryptoUtility(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	ext := &testCryptoUtilityExt{}
	reg.RegisterCryptoUtility(ext)

	gotExt := reg.GetCryptoUtilityExtension()
	if gotExt != ext {
		t.Error("GetCryptoUtilityExtension() != registered extension")
	}

	// GetCryptoProvider 调用 GetCrypto
	provider := reg.GetCryptoProvider()
	if provider != nil {
		t.Error("GetCryptoProvider() should return nil for stub (GetCrypto returns nil)")
	}
}

// TestExtensionRegistry_RegisterCryptoUtility_无注册 测试未注册时返回 nil
func TestExtensionRegistry_RegisterCryptoUtility_无注册(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	ext := reg.GetCryptoUtilityExtension()
	if ext != nil {
		t.Error("GetCryptoUtilityExtension() should be nil when no extension registered")
	}

	provider := reg.GetCryptoProvider()
	if provider != nil {
		t.Error("GetCryptoProvider() should be nil when no extension registered")
	}
}

// testAgentServerClientExt 测试用的 AgentServerClientExtension 实现
// 实现 extensions.AgentServerClientExtension 本地接口（与 sdk.AgentServerClientExtension 方法签名一致）
type testAgentServerClientExt struct {
	client transport.AgentTransport
}

func (e *testAgentServerClientExt) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}
func (e *testAgentServerClientExt) Shutdown(ctx context.Context) error  { return nil }
func (e *testAgentServerClientExt) Metadata() *ExtensionMetadata        { return nil }
func (e *testAgentServerClientExt) SetExtensionDir(path string)         {}
func (e *testAgentServerClientExt) GetClient() transport.AgentTransport { return e.client }

// testCryptoUtilityExt 测试用的 CryptoUtilityExtension 实现
type testCryptoUtilityExt struct{}

func (e *testCryptoUtilityExt) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}
func (e *testCryptoUtilityExt) Shutdown(ctx context.Context) error { return nil }
func (e *testCryptoUtilityExt) Metadata() *ExtensionMetadata       { return nil }
func (e *testCryptoUtilityExt) SetExtensionDir(path string)        {}
func (e *testCryptoUtilityExt) GetCrypto() any                     { return nil }
