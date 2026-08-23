package lite

import (
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySettings 记忆配置。对齐 Python MemorySettings
type MemorySettings struct {
	// Provider 嵌入提供者类型，默认 "openai_compatible"
	Provider string
	// Model 嵌入模型名称，默认 "text-embedding-v3"
	Model string
	// Fallback 嵌入回退策略，默认 "mock"
	Fallback string
	// Sources 记忆来源目录列表，默认 ["memory", "sessions"]
	Sources []string
	// ExtraPaths 额外记忆路径
	ExtraPaths []string
	// Chunking 分块配置，含 tokens 和 overlap
	Chunking map[string]any
	// Query 查询配置，含 max_results/min_score/hybrid
	Query map[string]any
	// Store 存储配置，含 path/vector/fts
	Store map[string]any
	// Sync 同步配置，含 watch/intervalMinutes
	Sync map[string]any
	// Cache 缓存配置
	Cache map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// IsMemoryEnabled 判断记忆系统是否启用。对齐 Python is_memory_enabled
func IsMemoryEnabled() bool {
	envEnabled := strings.ToLower(strings.TrimSpace(os.Getenv("MEMORY_ENABLED")))
	if envEnabled == "" {
		return true
	}
	return envEnabled == "true" || envEnabled == "1" || envEnabled == "yes"
}

// CreateMemorySettings 创建默认记忆配置。对齐 Python create_memory_settings
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	s := &MemorySettings{
		Provider:   "openai_compatible",
		Model:      "text-embedding-v3",
		Fallback:   "mock",
		Sources:    []string{"memory", "sessions"},
		ExtraPaths: nil,
		Chunking: map[string]any{
			"tokens":  256,
			"overlap": 32,
		},
		Query: map[string]any{
			"max_results": 10,
			"min_score":   0.3,
			"hybrid": map[string]any{
				"enabled":             true,
				"vectorWeight":        0.7,
				"textWeight":          0.3,
				"candidateMultiplier": 2.0,
			},
		},
		Store: map[string]any{
			"path": "memory.db",
			"vector": map[string]any{
				"enabled": true,
			},
			"fts": map[string]any{
				"enabled": true,
			},
		},
		Sync: map[string]any{
			"watch":           true,
			"watchDebounceMs": 2000,
			"onSearch":        true,
			"onSessionStart":  true,
			"intervalMinutes": 0,
		},
		Cache: map[string]any{
			"enabled":    true,
			"maxEntries": 10000,
		},
	}

	for key, value := range overrides {
		switch key {
		case "provider":
			if v, ok := value.(string); ok {
				s.Provider = v
			}
		case "model":
			if v, ok := value.(string); ok {
				s.Model = v
			}
		case "fallback":
			if v, ok := value.(string); ok {
				s.Fallback = v
			}
		case "sources":
			if v, ok := value.([]string); ok {
				s.Sources = v
			} else if v, ok := value.([]any); ok {
				strs := make([]string, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				s.Sources = strs
			}
		case "extra_paths":
			if v, ok := value.([]string); ok {
				s.ExtraPaths = v
			} else if v, ok := value.([]any); ok {
				strs := make([]string, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				s.ExtraPaths = strs
			}
		case "chunking":
			if v, ok := value.(map[string]any); ok {
				s.Chunking = v
			}
		case "query":
			if v, ok := value.(map[string]any); ok {
				s.Query = v
			}
		case "store":
			if v, ok := value.(map[string]any); ok {
				s.Store = v
			}
		case "sync":
			if v, ok := value.(map[string]any); ok {
				s.Sync = v
			}
		case "cache":
			if v, ok := value.(map[string]any); ok {
				s.Cache = v
			}
		}
	}

	return s
}

// ──────────────────────────── 非导出函数 ────────────────────────────
