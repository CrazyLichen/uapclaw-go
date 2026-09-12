package lite

import (
	"os"
	"testing"
)

// TestIsMemoryEnabled_默认为true 测试默认启用
func TestIsMemoryEnabled_默认为true(t *testing.T) {
	os.Unsetenv("MEMORY_ENABLED")
	if !IsMemoryEnabled() {
		t.Error("默认应返回 true")
	}
}

// TestIsMemoryEnabled_环境变量为false 测试环境变量禁用
func TestIsMemoryEnabled_环境变量为false(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "false")
	if IsMemoryEnabled() {
		t.Error("MEMORY_ENABLED=false 时应返回 false")
	}
}

// TestIsMemoryEnabled_环境变量为0 测试环境变量 0
func TestIsMemoryEnabled_环境变量为0(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "0")
	if IsMemoryEnabled() {
		t.Error("MEMORY_ENABLED=0 时应返回 false")
	}
}

// TestIsMemoryEnabled_环境变量为yes 测试环境变量 yes
func TestIsMemoryEnabled_环境变量为yes(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "yes")
	if !IsMemoryEnabled() {
		t.Error("MEMORY_ENABLED=yes 时应返回 true")
	}
}

// TestIsMemoryEnabled_环境变量为1 测试环境变量 1
func TestIsMemoryEnabled_环境变量为1(t *testing.T) {
	t.Setenv("MEMORY_ENABLED", "1")
	if !IsMemoryEnabled() {
		t.Error("MEMORY_ENABLED=1 时应返回 true")
	}
}

// TestCreateMemorySettings_默认值 测试默认配置
func TestCreateMemorySettings_默认值(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if s.Provider != "openai_compatible" {
		t.Errorf("Provider 应为 openai_compatible，实际为 %s", s.Provider)
	}
	if s.Model != "text-embedding-v3" {
		t.Errorf("Model 应为 text-embedding-v3，实际为 %s", s.Model)
	}
	if s.Fallback != "mock" {
		t.Errorf("Fallback 应为 mock，实际为 %s", s.Fallback)
	}
	if len(s.Sources) != 2 || s.Sources[0] != "memory" || s.Sources[1] != "sessions" {
		t.Errorf("Sources 应为 [memory, sessions]，实际为 %v", s.Sources)
	}
}

// TestCreateMemorySettings_覆盖值 测试覆盖配置
func TestCreateMemorySettings_覆盖值(t *testing.T) {
	overrides := map[string]any{
		"provider": "mock",
		"model":    "custom-model",
		"fallback": "none",
	}
	s := CreateMemorySettings("/tmp", overrides)
	if s.Provider != "mock" {
		t.Errorf("Provider 应为 mock，实际为 %s", s.Provider)
	}
	if s.Model != "custom-model" {
		t.Errorf("Model 应为 custom-model，实际为 %s", s.Model)
	}
	if s.Fallback != "none" {
		t.Errorf("Fallback 应为 none，实际为 %s", s.Fallback)
	}
}

// TestCreateMemorySettings_分块配置 测试默认分块配置
func TestCreateMemorySettings_分块配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if s.Chunking.Tokens != 256 {
		t.Errorf("Chunking.Tokens 应为 256，实际为 %v", s.Chunking.Tokens)
	}
	if s.Chunking.Overlap != 32 {
		t.Errorf("Chunking.Overlap 应为 32，实际为 %v", s.Chunking.Overlap)
	}
}

// TestCreateMemorySettings_查询配置 测试默认查询配置
func TestCreateMemorySettings_查询配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if s.Query.MaxResults != 10 {
		t.Errorf("Query.MaxResults 应为 10，实际为 %v", s.Query.MaxResults)
	}
	if s.Query.MinScore != 0.3 {
		t.Errorf("Query.MinScore 应为 0.3，实际为 %v", s.Query.MinScore)
	}
}

// TestCreateMemorySettings_存储配置 测试默认存储配置
func TestCreateMemorySettings_存储配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if s.Store.Path != "memory.db" {
		t.Errorf("Store.Path 应为 memory.db，实际为 %v", s.Store.Path)
	}
	if !s.Store.Vector.Enabled {
		t.Error("Store.Vector.Enabled 应为 true")
	}
	if !s.Store.Fts.Enabled {
		t.Error("Store.Fts.Enabled 应为 true")
	}
}

// TestCreateMemorySettings_同步配置 测试默认同步配置
func TestCreateMemorySettings_同步配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if !s.Sync.Watch {
		t.Error("Sync.Watch 应为 true")
	}
	if s.Sync.WatchDebounceMs != 2000 {
		t.Errorf("Sync.WatchDebounceMs 应为 2000，实际为 %v", s.Sync.WatchDebounceMs)
	}
	if !s.Sync.OnSearch {
		t.Error("Sync.OnSearch 应为 true")
	}
	if !s.Sync.OnSessionStart {
		t.Error("Sync.OnSessionStart 应为 true")
	}
	if s.Sync.IntervalMinutes != 0 {
		t.Errorf("Sync.IntervalMinutes 应为 0，实际为 %v", s.Sync.IntervalMinutes)
	}
}

// TestCreateMemorySettings_缓存配置 测试默认缓存配置
func TestCreateMemorySettings_缓存配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if !s.Cache.Enabled {
		t.Error("Cache.Enabled 应为 true")
	}
	if s.Cache.MaxEntries != 10000 {
		t.Errorf("Cache.MaxEntries 应为 10000，实际为 %v", s.Cache.MaxEntries)
	}
}

// TestCreateMemorySettings_子配置覆盖 测试 overrides 覆盖子配置字段
func TestCreateMemorySettings_子配置覆盖(t *testing.T) {
	overrides := map[string]any{
		"chunking": map[string]any{
			"tokens":  512,
			"overlap": 64,
		},
		"query": map[string]any{
			"max_results": 20,
			"min_score":   0.5,
			"hybrid": map[string]any{
				"enabled":             false,
				"vectorWeight":        0.8,
				"textWeight":          0.2,
				"candidateMultiplier": 3.0,
			},
		},
		"store": map[string]any{
			"path": "custom.db",
			"vector": map[string]any{
				"enabled": false,
			},
			"fts": map[string]any{
				"enabled": false,
			},
		},
		"sync": map[string]any{
			"watch":           false,
			"watchDebounceMs": 500,
			"onSearch":        false,
			"onSessionStart":  false,
			"intervalMinutes": 10,
		},
		"cache": map[string]any{
			"enabled":    false,
			"maxEntries": 5000,
		},
	}
	s := CreateMemorySettings("/tmp", overrides)

	// Chunking 覆盖
	if s.Chunking.Tokens != 512 {
		t.Errorf("Chunking.Tokens 应为 512，实际为 %v", s.Chunking.Tokens)
	}
	if s.Chunking.Overlap != 64 {
		t.Errorf("Chunking.Overlap 应为 64，实际为 %v", s.Chunking.Overlap)
	}

	// Query 覆盖
	if s.Query.MaxResults != 20 {
		t.Errorf("Query.MaxResults 应为 20，实际为 %v", s.Query.MaxResults)
	}
	if s.Query.MinScore != 0.5 {
		t.Errorf("Query.MinScore 应为 0.5，实际为 %v", s.Query.MinScore)
	}
	if s.Query.Hybrid.Enabled {
		t.Error("Query.Hybrid.Enabled 应为 false")
	}
	if s.Query.Hybrid.VectorWeight != 0.8 {
		t.Errorf("Query.Hybrid.VectorWeight 应为 0.8，实际为 %v", s.Query.Hybrid.VectorWeight)
	}
	if s.Query.Hybrid.TextWeight != 0.2 {
		t.Errorf("Query.Hybrid.TextWeight 应为 0.2，实际为 %v", s.Query.Hybrid.TextWeight)
	}
	if s.Query.Hybrid.CandidateMultiplier != 3.0 {
		t.Errorf("Query.Hybrid.CandidateMultiplier 应为 3.0，实际为 %v", s.Query.Hybrid.CandidateMultiplier)
	}

	// Store 覆盖
	if s.Store.Path != "custom.db" {
		t.Errorf("Store.Path 应为 custom.db，实际为 %v", s.Store.Path)
	}
	if s.Store.Vector.Enabled {
		t.Error("Store.Vector.Enabled 应为 false")
	}
	if s.Store.Fts.Enabled {
		t.Error("Store.Fts.Enabled 应为 false")
	}

	// Sync 覆盖
	if s.Sync.Watch {
		t.Error("Sync.Watch 应为 false")
	}
	if s.Sync.WatchDebounceMs != 500 {
		t.Errorf("Sync.WatchDebounceMs 应为 500，实际为 %v", s.Sync.WatchDebounceMs)
	}
	if s.Sync.OnSearch {
		t.Error("Sync.OnSearch 应为 false")
	}
	if s.Sync.OnSessionStart {
		t.Error("Sync.OnSessionStart 应为 false")
	}
	if s.Sync.IntervalMinutes != 10 {
		t.Errorf("Sync.IntervalMinutes 应为 10，实际为 %v", s.Sync.IntervalMinutes)
	}

	// Cache 覆盖
	if s.Cache.Enabled {
		t.Error("Cache.Enabled 应为 false")
	}
	if s.Cache.MaxEntries != 5000 {
		t.Errorf("Cache.MaxEntries 应为 5000，实际为 %v", s.Cache.MaxEntries)
	}
}
func TestCreateMemorySettings_混合搜索配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if !s.Query.Hybrid.Enabled {
		t.Error("Query.Hybrid.Enabled 应为 true")
	}
	if s.Query.Hybrid.VectorWeight != 0.7 {
		t.Errorf("Query.Hybrid.VectorWeight 应为 0.7，实际为 %v", s.Query.Hybrid.VectorWeight)
	}
	if s.Query.Hybrid.TextWeight != 0.3 {
		t.Errorf("Query.Hybrid.TextWeight 应为 0.3，实际为 %v", s.Query.Hybrid.TextWeight)
	}
	if s.Query.Hybrid.CandidateMultiplier != 2.0 {
		t.Errorf("Query.Hybrid.CandidateMultiplier 应为 2.0，实际为 %v", s.Query.Hybrid.CandidateMultiplier)
	}
}
