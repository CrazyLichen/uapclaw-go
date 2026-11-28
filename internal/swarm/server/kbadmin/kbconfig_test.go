package kbadmin

import "testing"

func TestParseKBConfig_默认值(t *testing.T) {
	cfg := ParseKBConfig("")
	if cfg.MaxFileMB != 50 {
		t.Fatalf("默认上限应 50MB, got %d", cfg.MaxFileMB)
	}
	if cfg.Chunker.Type != "text" || cfg.Chunker.ChunkSize != 500 || cfg.Chunker.ChunkOverlap != 50 {
		t.Fatalf("默认 chunker 配置不符: %+v", cfg.Chunker)
	}
}

func TestParseKBConfig_覆盖(t *testing.T) {
	cfg := ParseKBConfig(`{"max_file_mb":10,"chunker":{"type":"token","chunk_size":200,"chunk_overlap":20},
		"embedding":{"provider":"openai","model_name":"text-embedding-3-small","base_url":"http://127.0.0.1:9","api_key":"k"}}`)
	if cfg.MaxFileMB != 10 || cfg.Chunker.ChunkSize != 200 || cfg.Chunker.ChunkOverlap != 20 {
		t.Fatalf("覆盖失败: %+v", cfg)
	}
	if cfg.Embedding.Provider != "openai" || cfg.Embedding.ModelName != "text-embedding-3-small" {
		t.Fatalf("embedding 覆盖失败: %+v", cfg.Embedding)
	}
}

func TestParseKBConfig_坏JSON回退默认(t *testing.T) {
	cfg := ParseKBConfig("{bad json")
	if cfg.MaxFileMB != 50 {
		t.Fatalf("坏 JSON 应回退默认, got %+v", cfg)
	}
}
