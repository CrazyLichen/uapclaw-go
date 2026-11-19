// symphony.go：Symphony 与演进开关解析。
//
// 对应 Python 代码：jiuwenswarm/common/config.py:340-365（get_symphony_evolution_enabled）
// 与 jiuwenswarm/common/symphony_config.py:23-24/:276-286/:289-298
// （DEFAULT_* / _bool / resolve_symphony_*）。
// 层级登记（偏差 D-15.16-e10 ①）：Python symphony_config.py 与 config.py 同在
// jiuwenswarm/common 层；Go 侧 config.py 对应物收敛在 internal/common/config，
// 两个 resolver 随 getter 一并落此包（不新建 internal/swarm/common 依赖方向）。
package config

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultSymphonyEnabled Symphony 代码默认关。Python: symphony_config.py:23
	DefaultSymphonyEnabled = false
	// DefaultEvolutionEnabled Symphony 演进代码默认关。Python: symphony_config.py:24
	DefaultEvolutionEnabled = false
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveSymphonyEnabled 解析显式开关或继承代码缺省（null → default）。
// Python: resolve_symphony_enabled (symphony_config.py:289-292)
func ResolveSymphonyEnabled(value any) bool {
	return resolveSymphonyBool(value, DefaultSymphonyEnabled)
}

// ResolveSymphonyEvolutionEnabled 解析显式演进开关或继承代码缺省。
// Python: resolve_symphony_evolution_enabled (symphony_config.py:295-298)
func ResolveSymphonyEvolutionEnabled(value any) bool {
	return resolveSymphonyBool(value, DefaultEvolutionEnabled)
}

// SymphonyEvolutionEnabled Symphony 与其演进开关同时开启才为真。
// enabled 位于 evolution.flow 下；旧配置（evolution.enabled）向后兼容。
// Python: get_symphony_evolution_enabled (config.py:340-365)
func SymphonyEvolutionEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	symphony, ok := cfg["symphony"].(map[string]any)
	if !ok {
		symphony = map[string]any{}
	}
	evolution, ok := symphony["evolution"].(map[string]any)
	if !ok {
		evolution = map[string]any{}
	}
	// enabled 位于 evolution.flow 下；旧配置（evolution.enabled）向后兼容。
	flow, _ := evolution["flow"].(map[string]any)
	flowEnabled, flowPresent := flow["enabled"]
	if !flowPresent {
		flowEnabled = evolution["enabled"]
	}
	return ResolveSymphonyEnabled(symphony["enabled"]) && ResolveSymphonyEvolutionEnabled(flowEnabled)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSymphonyBool 宽松布尔：bool 原样；None→default；字符串按 1/true/yes/on
// 与 0/false/no/off 归一；其余得 default。
// Python: _bool (symphony_config.py:276-286)
func resolveSymphonyBool(value any, defaultVal bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case nil:
		return defaultVal
	case string:
		switch toLowerFold(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
		return defaultVal
	}
	return defaultVal
}
