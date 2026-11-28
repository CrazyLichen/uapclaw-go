package kbadmin

import (
	"encoding/json"
)

// ──────────────────────────── 常量 ────────────────────────────

// 默认值常量（spec §1：上限无配置回退 50MiB；chunker 默认对齐 13.x 文本切分基线）
const (
	// defaultMaxFileMB 默认单文件上限（MB）
	defaultMaxFileMB = 50
	// defaultChunkerType 默认切分器类型
	defaultChunkerType = "text"
	// defaultChunkerChunkSize 默认块大小
	defaultChunkerChunkSize = 500
	// defaultChunkerChunkOverlap 默认块重叠
	defaultChunkerChunkOverlap = 50
)

// ──────────────────────────── 结构体 ────────────────────────────

// KBConfig kb_info.config_json 的解析结果（kbadmin 编排消费面）
type KBConfig struct {
	// MaxFileMB 单文件上限（MB），D-30-5-1 回填消费点
	MaxFileMB int `json:"max_file_mb"`
	// Chunker 切分配置
	Chunker ChunkerConfig `json:"chunker"`
	// Embedding 向量化配置，D-30-1-4 回填消费点
	Embedding EmbeddingEndpointConfig `json:"embedding"`
}

// ChunkerConfig 切分器配置
type ChunkerConfig struct {
	// Type 切分器类型
	Type string `json:"type"`
	// ChunkSize 块大小
	ChunkSize int `json:"chunk_size"`
	// ChunkOverlap 块重叠
	ChunkOverlap int `json:"chunk_overlap"`
}

// EmbeddingEndpointConfig embedding 服务端点配置
type EmbeddingEndpointConfig struct {
	// Provider 提供方（openai/dashscope/vllm；缺省 openai 兼容协议）
	Provider string `json:"provider"`
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// BaseURL API 地址
	BaseURL string `json:"base_url"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseKBConfig 解析 config_json；空/坏 JSON 回退默认（防御性：坏配置不让管理面挂死）
func ParseKBConfig(raw string) KBConfig {
	cfg := KBConfig{
		MaxFileMB: defaultMaxFileMB,
		Chunker: ChunkerConfig{Type: defaultChunkerType,
			ChunkSize: defaultChunkerChunkSize, ChunkOverlap: defaultChunkerChunkOverlap},
	}
	if raw == "" {
		return cfg
	}
	var parsed KBConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return cfg
	}
	if parsed.MaxFileMB > 0 {
		cfg.MaxFileMB = parsed.MaxFileMB
	}
	if parsed.Chunker.Type != "" {
		cfg.Chunker.Type = parsed.Chunker.Type
	}
	if parsed.Chunker.ChunkSize > 0 {
		cfg.Chunker.ChunkSize = parsed.Chunker.ChunkSize
	}
	if parsed.Chunker.ChunkOverlap > 0 {
		cfg.Chunker.ChunkOverlap = parsed.Chunker.ChunkOverlap
	}
	if parsed.Embedding.Provider != "" {
		cfg.Embedding = parsed.Embedding
	}
	return cfg
}
