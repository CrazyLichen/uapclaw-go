package memory

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalMemoryConfig memory.external 配置段（含默认值填充）。
// 对齐 Python get_external_memory_config 返回的 dict (L52-70)。
type ExternalMemoryConfig struct {
	// Provider 提供者名称（openjiuwen/mem0/openviking/插件名），空串=禁用
	Provider string
	// UserID 用户标识，默认 __default__
	UserID string
	// ScopeID 作用域标识，默认 __default__
	ScopeID string
	// AllowedPlugins 允许加载的插件列表
	AllowedPlugins []string
	// OpenJiuwen openjiuwen provider 子段配置
	OpenJiuwen map[string]any
	// Mem0 mem0 provider 子段配置
	Mem0 map[string]any
	// OpenViking openviking provider 子段配置
	OpenViking map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultExternalUser 外接记忆默认用户标识
	// Python: _DEFAULT_USER = "__default__" (external_memory_config.py L23)
	defaultExternalUser = "__default__"
	// defaultExternalScope 外接记忆默认作用域标识
	// Python: _DEFAULT_SCOPE = "__default__" (L24)
	defaultExternalScope = "__default__"
	// ltmSubdir LTM 数据子目录
	// Python: _LTM_SUBDIR = "memory/ltm" (L25)
	ltmSubdir = "memory/ltm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// validEngines 合法的 memory.engine 取值集合
// Python: _VALID_ENGINES = {"builtin", "external", "both", "none"} (L27)
var validEngines = map[string]struct{}{
	"builtin": {}, "external": {}, "both": {}, "none": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemoryEngine 返回记忆引擎策略：builtin | external | both | none。
// 对齐 Python get_memory_engine (L30-39)：控制允许挂载的记忆子系统，
// 默认 builtin（对齐早于此标志的配置向后兼容）。空白/非法值回落 builtin。
func GetMemoryEngine(cfg map[string]any) string {
	// Python: cfg = config if config is not None else _load_config()
	if cfg == nil {
		cfg = loadConfig()
	}
	memCfg, _ := cfg["memory"].(map[string]any)
	value := ""
	if memCfg != nil {
		value, _ = memCfg["engine"].(string)
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := validEngines[value]; !ok {
		return "builtin"
	}
	return value
}

// IsBuiltinMemoryAllowed 内置 MemoryRail 的引擎级门禁。
// 对齐 Python is_builtin_memory_allowed (L42-44)。
func IsBuiltinMemoryAllowed(cfg map[string]any) bool {
	engine := GetMemoryEngine(cfg)
	return engine == "builtin" || engine == "both"
}

// IsExternalMemoryAllowed ExternalMemoryRail 的引擎级门禁。
// 对齐 Python is_external_memory_allowed (L47-49)。
func IsExternalMemoryAllowed(cfg map[string]any) bool {
	engine := GetMemoryEngine(cfg)
	return engine == "external" || engine == "both"
}

// GetExternalMemoryConfig 返回带默认值填充的 memory.external 配置段。
// 对齐 Python get_external_memory_config (L52-70)。
func GetExternalMemoryConfig(cfg map[string]any) ExternalMemoryConfig {
	// Python: cfg = config if config is not None else _load_config()
	if cfg == nil {
		cfg = loadConfig()
	}
	var ext map[string]any
	if memCfg, ok := cfg["memory"].(map[string]any); ok {
		ext, _ = memCfg["external"].(map[string]any)
	}
	if ext == nil {
		ext = map[string]any{}
	}

	strVal := func(key string) string {
		s, _ := ext[key].(string)
		return s
	}
	sub := func(key string) map[string]any {
		m, _ := ext[key].(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		return m
	}

	// Python: "allowed_plugins": ext.get("allowed_plugins") or []
	var plugins []string
	if arr, ok := ext["allowed_plugins"].([]any); ok {
		plugins = make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				plugins = append(plugins, s)
			}
		}
	}

	return ExternalMemoryConfig{
		// Python: "provider": (ext.get("provider") or "").strip()
		Provider: strings.TrimSpace(strVal("provider")),
		// Python: "user_id": ext.get("user_id") or _DEFAULT_USER
		UserID: strValDefaultExternal(strVal("user_id")),
		// Python: "scope_id": ext.get("scope_id") or _DEFAULT_SCOPE
		ScopeID: strValDefaultExternal(strVal("scope_id")),
		AllowedPlugins: plugins,
		OpenJiuwen:     sub("openjiuwen"),
		Mem0:           sub("mem0"),
		OpenViking:     sub("openviking"),
	}
}

// IsExternalMemoryEnabled 外接记忆是否启用：引擎放行且配置了 provider。
// 对齐 Python is_external_memory_enabled (L73-77)。
func IsExternalMemoryEnabled(cfg map[string]any) bool {
	if !IsExternalMemoryAllowed(cfg) {
		return false
	}
	return GetExternalMemoryConfig(cfg).Provider != ""
}

// ResolveLtmDir 返回 LTM 数据目录 {workspace}/memory/ltm（自动创建）。
// 对齐 Python _resolve_ltm_dir (L80-84)：base.mkdir(parents=True, exist_ok=True)。
// mkdir 失败返回 error，沿 builder 链路传播为整体 nil（对齐 Python 异常传播）。
func ResolveLtmDir() (string, error) {
	base := filepath.Join(workspace.WorkspaceDir(), ltmSubdir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	return base, nil
}

// BuildOpenJiuwenProviderConfig 将 memory.external.openjiuwen 配置映射为
// OpenJiuwenProvider 预期的 config dict。
// 对齐 Python build_openjiuwen_provider_config (L87-126)：
//
//	{
//	  "kv":        {"backend": "shelve", "path": "..."},
//	  "vector":    {"backend": "chroma", "persist_directory": "..."},
//	  "db":        {"backend": "sqlite", "path": "..."},
//	  "embedding": {"model_name": "...", "base_url": "...", "api_key": "..."},
//	}
//
// kv 默认 shelve 是 jiuwenswarm builder 基线（L101: or "shelve"）；
// agentcore provider 内部默认 memory 是 openjiuwen 基线，两层各自对齐各自基线，
// builder 总是显式填 backend，provider 默认在本链路不生效（与 Python 一致）。
func BuildOpenJiuwenProviderConfig(extCfg ExternalMemoryConfig, cfg map[string]any) (map[string]any, error) {
	ltmDir, err := ResolveLtmDir()
	if err != nil {
		return nil, err
	}

	ojCfg := extCfg.OpenJiuwen
	strOr := func(v any, def string) string {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.ToLower(strings.TrimSpace(s))
		}
		return def
	}
	pathOr := func(v any, def string) string {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		return def
	}

	// Python L101: kv_backend = (oj_cfg.get("kv_type") or "shelve").lower()
	kvBackend := strOr(ojCfg["kv_type"], "shelve")
	kvPath := pathOr(ojCfg["kv_path"], filepath.Join(ltmDir, "kv"))

	vectorBackend := strOr(ojCfg["vector_type"], "chroma")
	vectorDir := pathOr(ojCfg["vector_persist_dir"], filepath.Join(ltmDir, "chroma"))

	dbBackend := strOr(ojCfg["db_type"], "sqlite")
	dbPath := pathOr(ojCfg["db_path"], filepath.Join(ltmDir, "ltm.db"))

	// Python L110-115: embed_cfg = get_embed_config() or {} + EMBED_* env 回退
	// 注意键名 embed_base_url（GetEmbedConfig），env 回退 EMBED_BASE_URL
	var modelName, baseURL, apiKey string
	if cfg == nil {
		// 显式 cfg 为空时走包内 GetEmbedConfig（读 config.yaml embed 段）
		ec := GetEmbedConfig()
		modelName, baseURL, apiKey = ec.Model, ec.BaseURL, ec.APIKey
	} else {
		// 调用方持有配置缓存时直接读 embed 段
		embedCfg, _ := cfg["embed"].(map[string]any)
		get := func(key string) string {
			if embedCfg != nil {
				if s, ok := embedCfg[key].(string); ok && s != "" {
					return s
				}
			}
			return ""
		}
		modelName = get("embed_model")
		baseURL = get("embed_base_url")
		apiKey = get("embed_api_key")
	}
	// Python L111-114: embed_cfg.get("model") or os.getenv("EMBED_MODEL", "")
	// 空串回落 env（config.go 的 strValDefault 是类型断言语义，不做空回落）
	modelName = strOrEnv(modelName, "EMBED_MODEL")
	baseURL = strOrEnv(baseURL, "EMBED_BASE_URL")
	apiKey = strOrEnv(apiKey, "EMBED_API_KEY")
	if modelName == "" {
		// Python L116-119: logger.warning("[external_memory] Embedding not configured — ...")
		logger.Warn(logComponent).
			Str("event_type", "external_memory").
			Msg("Embedding not configured — OpenJiuwen LTM will skip vector search")
	}

	return map[string]any{
		"kv":        map[string]any{"backend": kvBackend, "path": kvPath},
		"vector":    map[string]any{"backend": vectorBackend, "persist_directory": vectorDir},
		"db":        map[string]any{"backend": dbBackend, "path": dbPath},
		"embedding": map[string]any{"model_name": modelName, "base_url": baseURL, "api_key": apiKey},
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strValDefaultExternal 空串回落外接记忆默认值。
// 对齐 Python: ext.get(key) or _DEFAULT_USER/_DEFAULT_SCOPE
// （两个默认常量值同为 __default__，语义等价）
func strValDefaultExternal(s string) string {
	if s != "" {
		return s
	}
	return defaultExternalUser
}

// strOrEnv 空串回落环境变量。
// 对齐 Python: cfg.get(key) or os.getenv(ENV, "")
func strOrEnv(s, envKey string) string {
	if s != "" {
		return s
	}
	return os.Getenv(envKey)
}
