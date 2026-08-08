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
	tokens, _ := s.Chunking["tokens"].(int)
	if tokens != 256 {
		t.Errorf("Chunking.tokens 应为 256，实际为 %v", s.Chunking["tokens"])
	}
	overlap, _ := s.Chunking["overlap"].(int)
	if overlap != 32 {
		t.Errorf("Chunking.overlap 应为 32，实际为 %v", s.Chunking["overlap"])
	}
}

// TestCreateMemorySettings_混合搜索配置 测试默认混合搜索配置
func TestCreateMemorySettings_混合搜索配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	hybrid, _ := s.Query["hybrid"].(map[string]any)
	if hybrid == nil {
		t.Fatal("Query.hybrid 应为非 nil")
	}
	if hybrid["enabled"] != true {
		t.Error("hybrid.enabled 应为 true")
	}
	if hybrid["vectorWeight"] != 0.7 {
		t.Errorf("hybrid.vectorWeight 应为 0.7，实际为 %v", hybrid["vectorWeight"])
	}
	if hybrid["textWeight"] != 0.3 {
		t.Errorf("hybrid.textWeight 应为 0.3，实际为 %v", hybrid["textWeight"])
	}
}
