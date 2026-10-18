package memory

import "strings"

// ──────────────────────────── 结构体 ────────────────────────────

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

// ──────────────────────────── 非导出函数 ────────────────────────────
