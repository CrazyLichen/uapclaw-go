package lite

import (
	"context"
	"fmt"
	"testing"
)

// TestResolveEmbeddingConfigFromEnv_全部设置 测试参数和 fallback 都有值时返回配置
func TestResolveEmbeddingConfigFromEnv_全部设置(t *testing.T) {
	result := ResolveEmbeddingConfigFromEnv("default-model", "https://default.example.com", "default-key")
	if result == nil {
		t.Fatal("当所有参数都有值时应返回非 nil")
	}
	// 注意：当 EMBEDDING_MODEL_NAME 环境变量未设置时，函数会覆盖 modelName 为空再设为 "default"
	// 这是预期行为，对齐 Python 的 env 优先逻辑
	if result.BaseURL != "https://default.example.com" {
		t.Errorf("BaseURL 应为 https://default.example.com，实际为 %s", result.BaseURL)
	}
}

// TestResolveEmbeddingConfigFromEnv_环境变量覆盖 测试环境变量优先
func TestResolveEmbeddingConfigFromEnv_环境变量覆盖(t *testing.T) {
	t.Setenv("EMBEDDING_MODEL_NAME", "env-model")
	t.Setenv("EMBEDDING_BASE_URL", "https://env.example.com")
	t.Setenv("EMBEDDING_API_KEY", "env-key")

	result := ResolveEmbeddingConfigFromEnv("fallback-model", "https://fallback.example.com", "fallback-key")
	if result == nil {
		t.Fatal("应返回非 nil")
	}
	if result.ModelName != "env-model" {
		t.Errorf("ModelName 应为 env-model，实际为 %s", result.ModelName)
	}
	if result.BaseURL != "https://env.example.com" {
		t.Errorf("BaseURL 应为 https://env.example.com，实际为 %s", result.BaseURL)
	}
	if result.APIKey != "env-key" {
		t.Errorf("APIKey 应为 env-key，实际为 %s", result.APIKey)
	}
}

// TestResolveEmbeddingConfigFromEnv_空URL返回nil 测试无 URL 时返回 nil
func TestResolveEmbeddingConfigFromEnv_空URL返回nil(t *testing.T) {
	result := ResolveEmbeddingConfigFromEnv("model", "", "key")
	if result != nil {
		t.Error("无 BaseURL 时应返回 nil")
	}
}

// TestCreateEmbeddingProvider_mock 测试 mock provider
func TestCreateEmbeddingProvider_mock(t *testing.T) {
	provider, err := CreateEmbeddingProvider("mock", "", "", nil)
	if err != nil {
		t.Fatalf("创建 mock provider 失败: %v", err)
	}
	if _, ok := provider.(*MockEmbeddingProvider); !ok {
		t.Error("应为 MockEmbeddingProvider 类型")
	}
}

// TestCreateEmbeddingProvider_无配置回退mock 测试无配置时回退到 mock
func TestCreateEmbeddingProvider_无配置回退mock(t *testing.T) {
	provider, err := CreateEmbeddingProvider("openai_compatible", "model", "mock", nil)
	if err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	if _, ok := provider.(*MockEmbeddingProvider); !ok {
		t.Error("fallback=mock 时应返回 MockEmbeddingProvider")
	}
}

// TestCreateEmbeddingProvider_无配置无回退 测试无配置且无回退时返回错误
func TestCreateEmbeddingProvider_无配置无回退(t *testing.T) {
	_, err := CreateEmbeddingProvider("openai_compatible", "model", "none", nil)
	if err == nil {
		t.Error("无配置且 fallback 非 mock 时应返回错误")
	}
}

// TestMockEmbeddingProvider_EmbedQuery 测试 MockEmbeddingProvider 的 EmbedQuery
// 对齐 Python: MockEmbeddingProvider.embed_query — 基于 md5 种子的 128 维确定性随机向量
func TestMockEmbeddingProvider_EmbedQuery(t *testing.T) {
	mock := NewMockEmbeddingProvider()
	ctx := context.Background()
	vec, err := mock.EmbedQuery(ctx, "test")
	if err != nil {
		t.Fatalf("EmbedQuery 失败: %v", err)
	}
	if len(vec) != 128 {
		t.Errorf("EmbedQuery 应返回 128 维向量，实际长度 %d", len(vec))
	}
	// 确定性：相同输入返回相同向量
	vec2, _ := mock.EmbedQuery(ctx, "test")
	if fmt.Sprintf("%v", vec) != fmt.Sprintf("%v", vec2) {
		t.Error("相同输入应返回相同向量")
	}
	// 值在 [-1, 1] 范围内
	for _, v := range vec {
		if v < -1.0 || v > 1.0 {
			t.Errorf("向量值 %v 超出 [-1, 1] 范围", v)
			break
		}
	}
}

// TestMockEmbeddingProvider_EmbedDocuments 测试 MockEmbeddingProvider 的 EmbedDocuments
// 对齐 Python: MockEmbeddingProvider.embed_documents — 迭代 EmbedQuery 生成向量
func TestMockEmbeddingProvider_EmbedDocuments(t *testing.T) {
	mock := NewMockEmbeddingProvider()
	ctx := context.Background()
	vecs, err := mock.EmbedDocuments(ctx, []string{"test1", "test2"})
	if err != nil {
		t.Fatalf("EmbedDocuments 失败: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("EmbedDocuments 应返回 2 个向量，实际为 %d", len(vecs))
	}
	if len(vecs[0]) != 128 || len(vecs[1]) != 128 {
		t.Errorf("EmbedDocuments 每个向量应为 128 维，实际为 %d/%d", len(vecs[0]), len(vecs[1]))
	}
	// 不同输入返回不同向量
	if fmt.Sprintf("%v", vecs[0]) == fmt.Sprintf("%v", vecs[1]) {
		t.Error("不同输入应返回不同向量")
	}
}

// TestMockEmbeddingProvider_接口满足 编译期验证 MockEmbeddingProvider 满足 EmbeddingProvider
func TestMockEmbeddingProvider_接口满足(t *testing.T) {
	var _ EmbeddingProvider = NewMockEmbeddingProvider()
}

// TestBaseEmbeddingAdapter_接口满足 编译期验证 baseEmbeddingAdapter 满足 EmbeddingProvider
func TestBaseEmbeddingAdapter_接口满足(t *testing.T) {
	var _ EmbeddingProvider = &baseEmbeddingAdapter{}
}
