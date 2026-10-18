package memory

import (
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbedConfig 嵌入配置。对齐 Python get_embed_config() 返回的 dict
// （config.py L75-83：embed 段三键 embed_api_key/embed_base_url/embed_model，缺失为空串）
type EmbedConfig struct {
	// APIKey 嵌入服务密钥（embed.embed_api_key）
	APIKey string
	// BaseURL 嵌入服务地址（embed.embed_base_url）
	BaseURL string
	// Model 嵌入模型名（embed.embed_model）
	Model string
}

// ChunkingConfig 分块配置。对齐 Python MemorySettings.chunking（config.py dataclass 默认 {"tokens": 256, "overlap": 32}）
type ChunkingConfig struct {
	// Tokens 目标 token 数，默认 256
	Tokens int
	// Overlap 重叠 token 数，默认 32
	Overlap int
}

// HybridConfig 混合搜索配置。对齐 Python MemorySettings.query.hybrid
type HybridConfig struct {
	// Enabled 是否启用混合，默认 true
	Enabled bool
	// VectorWeight 向量权重，默认 0.7
	VectorWeight float64
	// TextWeight 文本权重，默认 0.3
	TextWeight float64
	// CandidateMultiplier 候选倍数，默认 2.0
	CandidateMultiplier float64
}

// QueryConfig 查询配置。对齐 Python MemorySettings.query
type QueryConfig struct {
	// MaxResults 最大结果数，默认 10
	MaxResults int
	// MinScore 最小分数，默认 0.3。
	// 注意：Python search() 代码内 get("minScore", 0.7) 是死默认值——
	// settings 默认 0.3 先生效，实际语义 0.3
	MinScore float64
	// Hybrid 混合搜索配置
	Hybrid HybridConfig
}

// StoreConfig 存储配置。对齐 Python MemorySettings.store
type StoreConfig struct {
	// Path 数据库路径（相对 workspace_dir/memory/），默认 "memory.db"
	Path string
	// VectorEnabled 向量搜索开关，默认 true
	VectorEnabled bool
	// FtsEnabled 全文搜索开关，默认 true
	FtsEnabled bool
}

// SyncConfig 同步配置。对齐 Python MemorySettings.sync
type SyncConfig struct {
	// Watch 文件监听开关，默认 true
	Watch bool
	// WatchDebounceMs 监听防抖毫秒，默认 2000
	WatchDebounceMs int
	// OnSearch 搜索前同步，默认 true
	OnSearch bool
	// OnSessionStart 会话开始同步，默认 true
	OnSessionStart bool
	// IntervalMinutes 定时同步分钟，默认 0（禁用）
	IntervalMinutes int
}

// CacheConfig 缓存配置。对齐 Python MemorySettings.cache
type CacheConfig struct {
	// Enabled 是否启用嵌入缓存，默认 true
	Enabled bool
	// MaxEntries 最大条目数，默认 10000
	MaxEntries int
}

// MemorySettings 记忆配置。对齐 Python MemorySettings dataclass（config.py L81-134）
type MemorySettings struct {
	// Provider 提供者类型，默认 "openai_compatible"
	Provider string
	// Model 嵌入模型，默认 "text-embedding-v3"（可被 embed 段覆盖）
	Model string
	// Fallback 回退策略，默认 "mock"
	Fallback string
	// Sources 记忆来源，默认 ["memory", "sessions"]
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// configCache 配置缓存。对齐 Python _config_cache（config.py L26）
var configCache map[string]any

// configCacheMu 配置缓存锁
var configCacheMu sync.RWMutex

// configLoader 配置加载器（默认读全局 config.yaml，测试可注入）
var configLoader = defaultConfigLoader

// ──────────────────────────── 导出函数 ────────────────────────────

// ClearConfigCache 清除配置缓存，使下次 loadConfig 重新读取。
// 对齐 Python clear_config_cache（config.py L47-50）
func ClearConfigCache() {
	configCacheMu.Lock()
	configCache = nil
	configCacheMu.Unlock()
}

// GetEmbedConfig 获取嵌入配置。对齐 Python get_embed_config（config.py L75-83）：
// 读 embed 段三键（embed_api_key/embed_base_url/embed_model），缺失返回空串
func GetEmbedConfig() EmbedConfig {
	cfg := loadConfig()
	embedCfg, _ := cfg["embed"].(map[string]any)
	if embedCfg == nil {
		embedCfg = map[string]any{}
	}
	return EmbedConfig{
		APIKey:  strVal(embedCfg["embed_api_key"]),
		BaseURL: strVal(embedCfg["embed_base_url"]),
		Model:   strVal(embedCfg["embed_model"]),
	}
}

// CreateMemorySettings 创建记忆配置。对齐 Python create_memory_settings（config.py L136-186）：
// dataclass 默认值 ← embed 段 model ← config memory 段逐字段覆盖 ← overrides 透传。
// store 段特殊：overrides 未提供 store 时从 memory.store 构造三键注入。
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	cfg := loadConfig()
	embedCfg := GetEmbedConfig()
	memoryCfg, _ := cfg["memory"].(map[string]any)

	// dataclass 默认值
	s := &MemorySettings{
		Provider:   "openai_compatible",
		Model:      "text-embedding-v3",
		Fallback:   "mock",
		Sources:    []string{"memory", "sessions"},
		ExtraPaths: nil,
		Chunking:   ChunkingConfig{Tokens: 256, Overlap: 32},
		Query: QueryConfig{
			MaxResults: 10,
			MinScore:   0.3,
			Hybrid:     HybridConfig{Enabled: true, VectorWeight: 0.7, TextWeight: 0.3, CandidateMultiplier: 2.0},
		},
		Store: StoreConfig{Path: "memory.db", VectorEnabled: true, FtsEnabled: true},
		Sync:  SyncConfig{Watch: true, WatchDebounceMs: 2000, OnSearch: true, OnSessionStart: true, IntervalMinutes: 0},
		Cache: CacheConfig{Enabled: true, MaxEntries: 10000},
	}

	// Python: settings.model = embed_config.get("model", settings.model)
	if embedCfg.Model != "" {
		s.Model = embedCfg.Model
	}

	// Python: if memory_config: 逐字段覆盖
	if memoryCfg != nil {
		if v, ok := memoryCfg["provider"].(string); ok {
			s.Provider = v
		}
		if v, ok := memoryCfg["fallback"].(string); ok {
			s.Fallback = v
		}
		if v := strSlice(memoryCfg["sources"]); v != nil {
			s.Sources = v
		}
		if v := strSlice(memoryCfg["extraPaths"]); v != nil {
			s.ExtraPaths = v
		}
	if v, ok := memoryCfg["chunking"].(map[string]any); ok {
		if t, ok := numVal(v["tokens"]); ok {
			s.Chunking.Tokens = int(t)
		}
		if o, ok := numVal(v["overlap"]); ok {
			s.Chunking.Overlap = int(o)
		}
	}
		if v, ok := memoryCfg["query"].(map[string]any); ok {
			applyQueryOverrides(s, v)
		}
		if v, ok := memoryCfg["sync"].(map[string]any); ok {
			applySyncOverrides(s, v)
		}
		if v, ok := memoryCfg["cache"].(map[string]any); ok {
			applyCacheOverrides(s, v)
		}
	}

	// Python: store 段特殊处理（overrides["store"] 未提供时构造三键：
	// path=store_config.get("path", "memory.db"), vector=..., fts=...）
	if _, exists := overrides["store"]; !exists {
		storeCfg, _ := memoryCfg["store"].(map[string]any)
		if storeCfg == nil {
			storeCfg = map[string]any{}
		}
		// 向量数据库索引文件存放在与 MEMORY.md 同目录 (workspace_dir/memory/memory.db)
		// 只使用文件名，让 manager 的 resolveDBPath 处理完整路径
		storeOverride := map[string]any{
			"path": strValDefault(storeCfg["path"], "memory.db"),
		}
		if ve, ok := storeCfg["vector"].(map[string]any); ok {
			storeOverride["vector"] = ve
		} else {
			storeOverride["vector"] = map[string]any{"enabled": true}
		}
		if fe, ok := storeCfg["fts"].(map[string]any); ok {
			storeOverride["fts"] = fe
		} else {
			storeOverride["fts"] = map[string]any{"enabled": true}
		}
		overrides = mapsClone(overrides)
		overrides["store"] = storeOverride
	}

	// Python: for key, value in overrides.items(): setattr
	applyOverrides(s, overrides)

	_ = workspaceDir // 对齐 Python 签名（workspace_dir 参数保留，Go 侧未用其默认目录逻辑）
	return s
}

// ResolveModeMemory 定位 modes 下指定 mode 的 memory 块。
// 对齐 Python _resolve_mode_memory（config.py L188-221）：
// "agent.plan"/"agent.fast" → modes.agent.plan；"plan"/"fast"（bare）→ 归 agent 命名空间；
// "code" → modes.code；找不到返回空 map（调用方视为禁用）
func ResolveModeMemory(mode string, cfg map[string]any) map[string]any {
	modesCfg, _ := cfg["modes"].(map[string]any)
	if modesCfg == nil {
		return map[string]any{}
	}

	token := strings.TrimSpace(mode)
	if token == "" {
		return map[string]any{}
	}

	var node map[string]any
	if strings.Contains(token, ".") {
		// 含点号（如 "agent.plan"）→ modes.agent.plan
		parts := strings.SplitN(token, ".", 2)
		top, _ := modesCfg[parts[0]].(map[string]any)
		if top == nil {
			return map[string]any{}
		}
		node, _ = top[parts[1]].(map[string]any)
	} else if token == "code" {
		node, _ = modesCfg["code"].(map[string]any)
	} else {
		// 不含点号（如 "plan"/"fast"）→ modes.agent.<plan|fast>（interface_deep 主入口命名空间）
		agentNode, _ := modesCfg["agent"].(map[string]any)
		if agentNode == nil {
			return map[string]any{}
		}
		node, _ = agentNode[token].(map[string]any)
	}

	if node == nil {
		return map[string]any{}
	}
	mem, _ := node["memory"].(map[string]any)
	if mem == nil {
		return map[string]any{}
	}
	return mem
}

// IsMemoryEnabled 检查指定 mode 下内置记忆是否启用。
// 对齐 Python is_memory_enabled（config.py L223-236）：读 modes.agent.<plan|fast>.memory.enabled
func IsMemoryEnabled(mode string, cfg map[string]any) bool {
	mem := ResolveModeMemory(mode, cfg)
	enabled, _ := mem["enabled"].(bool)
	return enabled
}

// IsProactiveMemory 检查指定 mode 下是否主动记忆。
// 对齐 Python is_proactive_memory（config.py L239-252）：
// true 时 agent 自动记录并在每次响应前搜索；false（默认）仅用户显式要求时记录/搜索
func IsProactiveMemory(mode string, cfg map[string]any) bool {
	mem := ResolveModeMemory(mode, cfg)
	proactive, _ := mem["is_proactive"].(bool)
	return proactive
}

// GetMemoryMode 读取 memory.mode：cloud 或 local（默认）。
// 对齐 Python get_memory_mode（config.py L254-259）
func GetMemoryMode(cfg map[string]any) string {
	memCfg, _ := cfg["memory"].(map[string]any)
	if memCfg == nil {
		return "local"
	}
	mode, _ := memCfg["mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "cloud" {
		return "cloud"
	}
	return "local"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultConfigLoader 默认配置加载：读全局 config.yaml。
// 对齐 Python _load_config（config.py L53-68）：文件不存在 warn + 空配置
func defaultConfigLoader() (map[string]any, error) {
	cfg, err := config.New(pathutil.ConfigFile())
	if err != nil {
		// 对齐 Python: logger.warning(f"Config file not found: {config_path}")
		// （config.New 失败等价于路径不可用）
		return map[string]any{}, nil
	}
	data, err := cfg.Load()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// loadConfig 带缓存加载配置。对齐 Python _load_config 的 _config_cache 语义：
// 命中直接返回；miss 调 configLoader 并回填缓存；加载失败返回空配置不缓存错误
func loadConfig() map[string]any {
	configCacheMu.RLock()
	if configCache != nil {
		defer configCacheMu.RUnlock()
		return configCache
	}
	configCacheMu.RUnlock()

	data, err := configLoader()
	if err != nil || data == nil {
		// 对齐 Python：yaml 解析异常时向上抛；此处降级为空配置并记录
		data = map[string]any{}
	}
	configCacheMu.Lock()
	configCache = data
	configCacheMu.Unlock()
	return data
}

// strVal 安全取字符串
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// numVal 安全取数值（yaml.v3 整数为 int、小数为 float64，需两者兼容；
// Python 动态类型无此区分，Go 侧统一转换）
func numVal(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// strValDefault 安全取字符串（带默认值）
func strValDefault(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

// strSlice 安全取字符串切片（[]string 或 []any）
func strSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		if len(arr) > 0 {
			return arr
		}
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// mapsClone 克隆 map（避免修改调用方 overrides）
func mapsClone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyQueryOverrides 应用 query 段覆盖（memory 段与 overrides 共用逻辑）
func applyQueryOverrides(s *MemorySettings, v map[string]any) {
	if mr, ok := numVal(v["maxResults"]); ok {
		s.Query.MaxResults = int(mr)
	}
	if ms, ok := numVal(v["minScore"]); ok {
		s.Query.MinScore = ms
	}
	if h, ok := v["hybrid"].(map[string]any); ok {
		if e, ok := h["enabled"].(bool); ok {
			s.Query.Hybrid.Enabled = e
		}
		if w, ok := numVal(h["vectorWeight"]); ok {
			s.Query.Hybrid.VectorWeight = w
		}
		if w, ok := numVal(h["textWeight"]); ok {
			s.Query.Hybrid.TextWeight = w
		}
		if cm, ok := numVal(h["candidateMultiplier"]); ok {
			s.Query.Hybrid.CandidateMultiplier = cm
		}
	}
}

// applySyncOverrides 应用 sync 段覆盖
func applySyncOverrides(s *MemorySettings, v map[string]any) {
	if w, ok := v["watch"].(bool); ok {
		s.Sync.Watch = w
	}
	if d, ok := numVal(v["watchDebounceMs"]); ok {
		s.Sync.WatchDebounceMs = int(d)
	}
	if o, ok := v["onSearch"].(bool); ok {
		s.Sync.OnSearch = o
	}
	if o, ok := v["onSessionStart"].(bool); ok {
		s.Sync.OnSessionStart = o
	}
	if m, ok := numVal(v["intervalMinutes"]); ok {
		s.Sync.IntervalMinutes = int(m)
	}
}

// applyCacheOverrides 应用 cache 段覆盖
func applyCacheOverrides(s *MemorySettings, v map[string]any) {
	if e, ok := v["enabled"].(bool); ok {
		s.Cache.Enabled = e
	}
	if m, ok := numVal(v["maxEntries"]); ok {
		s.Cache.MaxEntries = int(m)
	}
}

// applyOverrides 应用 overrides 到 settings。
// 对齐 Python for key, value in overrides.items(): if hasattr → setattr（config.py L180-182）
func applyOverrides(s *MemorySettings, overrides map[string]any) {
	if overrides == nil {
		return
	}
	if v, ok := overrides["provider"].(string); ok {
		s.Provider = v
	}
	if v, ok := overrides["model"].(string); ok {
		s.Model = v
	}
	if v, ok := overrides["fallback"].(string); ok {
		s.Fallback = v
	}
	if v := strSlice(overrides["sources"]); v != nil {
		s.Sources = v
	}
	if v := strSlice(overrides["extraPaths"]); v != nil {
		s.ExtraPaths = v
	}
	if v, ok := overrides["chunking"].(map[string]any); ok {
		if t, ok := numVal(v["tokens"]); ok {
			s.Chunking.Tokens = int(t)
		}
		if o, ok := numVal(v["overlap"]); ok {
			s.Chunking.Overlap = int(o)
		}
	}
	if v, ok := overrides["query"].(map[string]any); ok {
		applyQueryOverrides(s, v)
	}
	if v, ok := overrides["store"].(map[string]any); ok {
		if p, ok := v["path"].(string); ok {
			s.Store.Path = p
		}
		if ve, ok := v["vector"].(map[string]any); ok {
			if e, ok := ve["enabled"].(bool); ok {
				s.Store.VectorEnabled = e
			}
		}
		if fe, ok := v["fts"].(map[string]any); ok {
			if e, ok := fe["enabled"].(bool); ok {
				s.Store.FtsEnabled = e
			}
		}
	}
	if v, ok := overrides["sync"].(map[string]any); ok {
		applySyncOverrides(s, v)
	}
	if v, ok := overrides["cache"].(map[string]any); ok {
		applyCacheOverrides(s, v)
	}
}
