package graph

import "testing"

// TestNewGraphConfig_默认值 测试 GraphConfig 默认值
func TestNewGraphConfig_默认值(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	if cfg.Backend != "milvus" {
		t.Errorf("Backend 默认应为 milvus，实际为 %s", cfg.Backend)
	}
	if cfg.Timeout != 15.0 {
		t.Errorf("Timeout 默认应为 15.0，实际为 %v", cfg.Timeout)
	}
	if cfg.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent 默认应为 10，实际为 %d", cfg.MaxConcurrent)
	}
	if cfg.EmbedDim != 512 {
		t.Errorf("EmbedDim 默认应为 512，实际为 %d", cfg.EmbedDim)
	}
	if cfg.EmbedBatchSize != 10 {
		t.Errorf("EmbedBatchSize 默认应为 10，实际为 %d", cfg.EmbedBatchSize)
	}
	if cfg.StorageConfig == nil {
		t.Error("StorageConfig 不应为 nil")
	}
	if cfg.IndexConfig == nil {
		t.Error("IndexConfig 不应为 nil")
	}
}

// TestNewDefaultStorageConfig_默认值 测试 StorageConfig 默认值
func TestNewDefaultStorageConfig_默认值(t *testing.T) {
	cfg := NewDefaultStorageConfig()
	if cfg.UUID != 32 {
		t.Errorf("UUID 默认应为 32，实际为 %d", cfg.UUID)
	}
	if cfg.Content != 65535 {
		t.Errorf("Content 默认应为 65535，实际为 %d", cfg.Content)
	}
}

// TestNewDefaultIndexConfig_默认值 测试 IndexConfig 默认值
func TestNewDefaultIndexConfig_默认值(t *testing.T) {
	cfg := NewDefaultIndexConfig()
	if cfg.DistanceMetric != "cosine" {
		t.Errorf("DistanceMetric 默认应为 cosine，实际为 %s", cfg.DistanceMetric)
	}
	if cfg.BM25Config == nil {
		t.Error("BM25Config 不应为 nil")
	}
	if cfg.BM25Config.B != 0.75 {
		t.Errorf("BM25 B 默认应为 0.75，实际为 %v", cfg.BM25Config.B)
	}
}

// TestGraphConfig_Validate_正常 测试正常配置校验
func TestGraphConfig_Validate_正常(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	if err := cfg.Validate(); err != nil {
		t.Errorf("正常配置校验不应报错: %v", err)
	}
}

// TestGraphConfig_Validate_URI为空 测试 URI 为空
func TestGraphConfig_Validate_URI为空(t *testing.T) {
	cfg := NewGraphConfig("")
	if err := cfg.Validate(); err == nil {
		t.Error("URI 为空应返回错误")
	}
}

// TestGraphConfig_Validate_Timeout无效 测试 Timeout 无效
func TestGraphConfig_Validate_Timeout无效(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.Timeout = -1
	if err := cfg.Validate(); err == nil {
		t.Error("Timeout ≤ 0 应返回错误")
	}
}

// TestGraphConfig_Validate_EmbedDim过小 测试 EmbedDim 过小
func TestGraphConfig_Validate_EmbedDim过小(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.EmbedDim = 16
	if err := cfg.Validate(); err == nil {
		t.Error("EmbedDim < 32 应返回错误")
	}
}

// TestGraphConfig_Validate_EmbedBatchSize无效 测试 EmbedBatchSize 无效
func TestGraphConfig_Validate_EmbedBatchSize无效(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.EmbedBatchSize = 0
	if err := cfg.Validate(); err == nil {
		t.Error("EmbedBatchSize < 1 应返回错误")
	}
}

// TestGraphConfig_Validate_MaxConcurrent无效 测试 MaxConcurrent 无效
func TestGraphConfig_Validate_MaxConcurrent无效(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.MaxConcurrent = -1
	if err := cfg.Validate(); err == nil {
		t.Error("MaxConcurrent < 0 应返回错误")
	}
}
