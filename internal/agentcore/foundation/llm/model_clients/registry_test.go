package model_clients

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// mockModelClient 用于测试的 BaseModelClient mock。
type mockModelClient struct{}

func (m *mockModelClient) Invoke(_ context.Context, _ MessagesParam, _ ...InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}
func (m *mockModelClient) Stream(_ context.Context, _ MessagesParam, _ ...StreamOption) (*StreamResult, error) {
	return nil, nil
}
func (m *mockModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}
func (m *mockModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}
func (m *mockModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}
func (m *mockModelClient) Release(_ context.Context, _ ...ReleaseOption) (bool, error) {
	return false, nil
}

// mockFactory 创建 mockModelClient 的工厂函数。
func mockFactory(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) BaseModelClient {
	return &mockModelClient{}
}

// TestClientRegistry_RegisterAndGet 测试注册和获取客户端。
func TestClientRegistry_RegisterAndGet(t *testing.T) {
	r := NewClientRegistry()
	r.Register("TestProvider", "llm", mockFactory)

	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("TestProvider", "key", "https://api.test.com",
		llmschema.WithVerifySSL(false),
	)

	client, err := r.GetClient("TestProvider", "llm", mc, cc)
	if err != nil {
		t.Fatalf("GetClient 报错: %v", err)
	}
	if client == nil {
		t.Error("GetClient 返回 nil")
	}
}

// TestClientRegistry_DuplicateRegister 测试重复注册（应警告但不报错）。
func TestClientRegistry_DuplicateRegister(t *testing.T) {
	r := NewClientRegistry()
	r.Register("DupProvider", "llm", mockFactory)
	r.Register("DupProvider", "llm", mockFactory) // 不应 panic

	clients := r.ListClients()
	if len(clients) != 1 {
		t.Errorf("重复注册后 ListClients 长度 = %d, 期望 1", len(clients))
	}
}

// TestClientRegistry_GetClient_NotFound 测试获取未注册的客户端。
func TestClientRegistry_GetClient_NotFound(t *testing.T) {
	r := NewClientRegistry()
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("Unknown", "key", "https://api.test.com",
		llmschema.WithVerifySSL(false),
	)

	_, err := r.GetClient("Unknown", "llm", mc, cc)
	if err == nil {
		t.Error("获取未注册客户端应报错")
	}
}

// TestClientRegistry_Unregister 测试注销客户端。
func TestClientRegistry_Unregister(t *testing.T) {
	r := NewClientRegistry()
	r.Register("ToRemove", "llm", mockFactory)

	err := r.Unregister("ToRemove", "llm")
	if err != nil {
		t.Fatalf("Unregister 报错: %v", err)
	}

	clients := r.ListClients()
	if len(clients) != 0 {
		t.Errorf("Unregister 后 ListClients 长度 = %d, 期望 0", len(clients))
	}
}

// TestClientRegistry_Unregister_NotFound 测试注销未注册的客户端。
func TestClientRegistry_Unregister_NotFound(t *testing.T) {
	r := NewClientRegistry()
	err := r.Unregister("NotExist", "llm")
	if err == nil {
		t.Error("注销未注册客户端应报错")
	}
}

// TestClientRegistry_ListClients 测试列出所有客户端。
func TestClientRegistry_ListClients(t *testing.T) {
	r := NewClientRegistry()
	r.Register("Provider1", "llm", mockFactory)
	r.Register("Provider2", "llm", mockFactory)

	clients := r.ListClients()
	if len(clients) != 2 {
		t.Errorf("ListClients 长度 = %d, 期望 2", len(clients))
	}
}

// TestClientRegistry_GetClient_EmptyName 测试空名称获取。
func TestClientRegistry_GetClient_EmptyName(t *testing.T) {
	r := NewClientRegistry()
	mc := llmschema.NewModelRequestConfig()
	cc := llmschema.NewModelClientConfig("", "key", "https://api.test.com",
		llmschema.WithVerifySSL(false),
	)

	_, err := r.GetClient("", "llm", mc, cc)
	if err == nil {
		t.Error("空名称应报错")
	}
}

// TestCreateModelClient_MissingProvider 测试缺少 provider。
func TestCreateModelClient_MissingProvider(t *testing.T) {
	cc := llmschema.NewModelClientConfig("", "key", "https://api.test.com")
	mc := llmschema.NewModelRequestConfig()

	_, err := CreateModelClient(cc, mc)
	if err == nil {
		t.Error("缺少 provider 应报错")
	}
}

// TestCreateModelClient_MissingClientID 测试缺少 client_id。
func TestCreateModelClient_MissingClientID(t *testing.T) {
	cc := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		ClientID:       "",
		APIKey:         "key",
		APIBase:        "https://api.openai.com/v1",
	}
	mc := llmschema.NewModelRequestConfig()

	_, err := CreateModelClient(cc, mc)
	if err == nil {
		t.Error("缺少 client_id 应报错")
	}
}

// TestRegistryProviderValidator 测试 ProviderValidator 桥接。
func TestRegistryProviderValidator(t *testing.T) {
	validator := &registryProviderValidator{}

	// 未注册时
	result := validator.ValidateProvider("OpenAI")
	if result != "" {
		t.Errorf("未注册时 ValidateProvider 应返回空, got %q", result)
	}

	// 注册后
	r := GetClientRegistry()
	r.Register("TestValidateProvider", "llm", mockFactory)

	result = validator.ValidateProvider("TestValidateProvider")
	if result == "" {
		t.Error("注册后 ValidateProvider 应返回非空")
	}
}
