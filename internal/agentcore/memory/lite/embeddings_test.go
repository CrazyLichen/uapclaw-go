package lite

import (
	"context"
	"testing"
)

// TestMockEmbeddingProvider_EmbedQuery 测试 MockEmbeddingProvider.EmbedQuery
func TestMockEmbeddingProvider_EmbedQuery(t *testing.T) {
	p := NewMockEmbeddingProvider()
	vec, err := p.EmbedQuery(context.Background(), "test")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(vec) != 0 {
		t.Errorf("Expected empty vector, got len=%d", len(vec))
	}
}

// TestMockEmbeddingProvider_EmbedDocuments 测试 MockEmbeddingProvider.EmbedDocuments
func TestMockEmbeddingProvider_EmbedDocuments(t *testing.T) {
	p := NewMockEmbeddingProvider()
	vecs, err := p.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if vecs != nil {
		t.Errorf("Expected nil, got %v", vecs)
	}
}

// TestCreateEmbeddingProvider 测试 CreateEmbeddingProvider
func TestCreateEmbeddingProvider(t *testing.T) {
	p, err := CreateEmbeddingProvider("mock", "test-model", "mock", nil)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p == nil {
		t.Errorf("Expected non-nil provider")
	}
}

// TestResolveEmbeddingConfigFromEnv 测试 ResolveEmbeddingConfigFromEnv
func TestResolveEmbeddingConfigFromEnv(t *testing.T) {
	cfg := ResolveEmbeddingConfigFromEnv("", "", "")
	if cfg != nil {
		t.Errorf("Expected nil when no env vars set, got %v", cfg)
	}
}
