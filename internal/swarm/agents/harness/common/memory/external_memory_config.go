package memory

import "strings"

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
