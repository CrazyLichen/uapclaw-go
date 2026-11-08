package lite

import (
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkingConfig 分块配置。对齐 Python MemorySettings.chunking
type ChunkingConfig struct {
	// Tokens 分块 token 数，默认 256
	Tokens float64
	// Overlap 重叠 token 数，默认 32
	Overlap float64
}

// HybridConfig 混合搜索配置。对齐 Python MemorySettings.query.hybrid
type HybridConfig struct {
	// Enabled 是否启用混合搜索，默认 true
	Enabled bool
	// VectorWeight 向量搜索权重，默认 0.7
	VectorWeight float64
	// TextWeight 文本搜索权重，默认 0.3
	TextWeight float64
	// CandidateMultiplier 候选倍数，默认 2.0
	CandidateMultiplier float64
}

// QueryConfig 查询配置。对齐 Python MemorySettings.query
type QueryConfig struct {
	// MaxResults 最大结果数，默认 10
	MaxResults float64
	// MinScore 最小相关度分数，默认 0.3
	MinScore float64
	// KeywordMinScore 纯关键词检索（无向量）时的最小相关度分数，默认 0.1。
	//
	// 为何比 MinScore 低：纯 BM25 分数经 rank→score 变换后常落在 0.1~0.3，
	// 沿用 0.3 会把真实命中全滤掉、搜索返回空。混合检索仍用 MinScore。
	// 对齐 Python settings.query["keywordMinScore"]（manager.py:1373）。
	KeywordMinScore float64
	// Hybrid 混合搜索配置
	Hybrid HybridConfig
}

// VectorStoreConfig 向量存储配置。对齐 Python MemorySettings.store.vector
type VectorStoreConfig struct {
	// Enabled 是否启用向量存储，默认 true
	Enabled bool
}

// FtsConfig 全文搜索配置。对齐 Python MemorySettings.store.fts
type FtsConfig struct {
	// Enabled 是否启用全文搜索，默认 true
	Enabled bool
}

// StoreConfig 存储配置。对齐 Python MemorySettings.store
type StoreConfig struct {
	// Path 数据库文件路径，默认 "memory.db"
	Path string
	// Vector 向量存储配置
	Vector VectorStoreConfig
	// Fts 全文搜索配置
	Fts FtsConfig
}

// SyncConfig 同步配置。对齐 Python MemorySettings.sync
type SyncConfig struct {
	// Watch 是否监听文件变更，默认 true
	Watch bool
	// WatchDebounceMs 防抖毫秒数，默认 2000
	WatchDebounceMs int
	// OnSearch 搜索前同步，默认 true
	OnSearch bool
	// OnSessionStart 会话开始时同步，默认 true
	OnSessionStart bool
	// IntervalMinutes 定时同步间隔分钟数，默认 0（禁用）
	IntervalMinutes int
}

// CacheConfig 缓存配置。对齐 Python MemorySettings.cache
type CacheConfig struct {
	// Enabled 是否启用缓存，默认 true
	Enabled bool
	// MaxEntries 最大缓存条目数，默认 10000
	MaxEntries float64
}

// MemorySettings 记忆配置。对齐 Python MemorySettings
type MemorySettings struct {
	// Provider 嵌入提供者类型，默认 "openai_compatible"
	Provider string
	// Model 嵌入模型名称，默认 "text-embedding-v3"
	Model string
	// Fallback 嵌入回退策略。
	//
	// 默认空串（不回落）：未配置 embedding 时记忆系统以 FTS5 关键词检索模式运行，
	// 而不是悄悄换成 mock provider。只有显式填 "mock" 才会用 mock（测试/本地联调）。
	// 上游 MemorySettings 无此字段——它压根没有 mock 回落机制。
	Fallback string
	// Sources 记忆来源目录列表，默认 ["memory", "sessions"]
	Sources []string
	// ExtraPaths 额外记忆路径
	ExtraPaths []string
	// Chunking 分块配置
	Chunking ChunkingConfig
	// Query 查询配置
	Query QueryConfig
	// Store 存储配置
	Store StoreConfig
	// Sync 同步配置
	Sync SyncConfig
	// Cache 缓存配置
	Cache CacheConfig
}

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
	// 先把一套开箱即用的默认值摆好：用 OpenAI 兼容的向量模型做召回，
	// 检索时向量与关键词混合打分，结果落在本地 memory.db，并开启后台同步和缓存。
	// 下面这些数字都是调过的常用值，调用方不特别指定时就按这套跑。
	s := &MemorySettings{
		Provider:   "openai_compatible",
		Model:      "text-embedding-v3",
		Fallback:   "",
		Sources:    []string{"memory", "sessions"},
		ExtraPaths: nil,
		Chunking: ChunkingConfig{
			Tokens:  256,
			Overlap: 32,
		},
		Query: QueryConfig{
			MaxResults: 10,
			MinScore:   0.3,
			// 纯关键词路径用更低阈值，否则 BM25 中等分命中会被 0.3 全部滤掉
			KeywordMinScore: 0.1,
			Hybrid: HybridConfig{
				Enabled:             true,
				VectorWeight:        0.7,
				TextWeight:          0.3,
				CandidateMultiplier: 2.0,
			},
		},
		Store: StoreConfig{
			Path:   "memory.db",
			Vector: VectorStoreConfig{Enabled: true},
			Fts:    FtsConfig{Enabled: true},
		},
		Sync: SyncConfig{
			Watch:           true,
			WatchDebounceMs: 2000,
			OnSearch:        true,
			OnSessionStart:  true,
			IntervalMinutes: 0,
		},
		Cache: CacheConfig{
			Enabled:    true,
			MaxEntries: 10000,
		},
	}

	// 再用调用方传进来的 overrides 逐项覆盖默认值。
	// 只认下面列举的这些键，认不出的键直接忽略；某个键的值类型对不上也跳过，
	// 这样配置写错时只是不生效，不会把整套默认值打乱。
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
		// sources / extra_paths 是字符串列表：调用方可能给强类型切片，
		// 也可能给从 JSON 或 YAML 解出来的通用切片，两种形态都要接住。
		case "sources":
			if v, ok := value.([]string); ok {
				s.Sources = v
			} else if v, ok := value.([]any); ok {
				strs := make([]string, 0, len(v))
				for _, item := range v {
					if str, ok := item.(string); ok {
						strs = append(strs, str)
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
					if str, ok := item.(string); ok {
						strs = append(strs, str)
					}
				}
				s.ExtraPaths = strs
			}
		// 剩下几个键对应成组的配置（分块、检索、存储、同步、缓存），
		// 传进来是一层小 map，这里把认识的字段逐个挑出来覆盖，
		// 没给的字段就继续沿用上面的默认值。
		case "chunking":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["tokens"].(float64); ok {
					s.Chunking.Tokens = v2
				} else if v2, ok := v["tokens"].(int); ok {
					s.Chunking.Tokens = float64(v2)
				}
				if v2, ok := v["overlap"].(float64); ok {
					s.Chunking.Overlap = v2
				} else if v2, ok := v["overlap"].(int); ok {
					s.Chunking.Overlap = float64(v2)
				}
			}
		case "query":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["max_results"].(float64); ok {
					s.Query.MaxResults = v2
				} else if v2, ok := v["max_results"].(int); ok {
					s.Query.MaxResults = float64(v2)
				}
				if v2, ok := v["min_score"].(float64); ok {
					s.Query.MinScore = v2
				}
				if hybrid, ok := v["hybrid"].(map[string]any); ok {
					if v2, ok := hybrid["enabled"].(bool); ok {
						s.Query.Hybrid.Enabled = v2
					}
					if v2, ok := hybrid["vectorWeight"].(float64); ok {
						s.Query.Hybrid.VectorWeight = v2
					}
					if v2, ok := hybrid["textWeight"].(float64); ok {
						s.Query.Hybrid.TextWeight = v2
					}
					if v2, ok := hybrid["candidateMultiplier"].(float64); ok {
						s.Query.Hybrid.CandidateMultiplier = v2
					}
				}
			}
		case "store":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["path"].(string); ok {
					s.Store.Path = v2
				}
				if vec, ok := v["vector"].(map[string]any); ok {
					if v2, ok := vec["enabled"].(bool); ok {
						s.Store.Vector.Enabled = v2
					}
				}
				if fts, ok := v["fts"].(map[string]any); ok {
					if v2, ok := fts["enabled"].(bool); ok {
						s.Store.Fts.Enabled = v2
					}
				}
			}
		case "sync":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["watch"].(bool); ok {
					s.Sync.Watch = v2
				}
				if v2, ok := v["watchDebounceMs"].(int); ok {
					s.Sync.WatchDebounceMs = v2
				}
				if v2, ok := v["onSearch"].(bool); ok {
					s.Sync.OnSearch = v2
				}
				if v2, ok := v["onSessionStart"].(bool); ok {
					s.Sync.OnSessionStart = v2
				}
				if v2, ok := v["intervalMinutes"].(int); ok {
					s.Sync.IntervalMinutes = v2
				}
			}
		case "cache":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["enabled"].(bool); ok {
					s.Cache.Enabled = v2
				}
				if v2, ok := v["maxEntries"].(float64); ok {
					s.Cache.MaxEntries = v2
				} else if v2, ok := v["maxEntries"].(int); ok {
					s.Cache.MaxEntries = float64(v2)
				}
			}
		}
	}

	// 覆盖处理完毕，返回最终配置。
	return s
}
