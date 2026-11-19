// Python 源：jiuwenswarm/agents/harness/common/{cua_defaults,browser_defaults}.py
//
// cua（桌面）与 browser 子代理的共享缺省值与配置解析。本批 scoped 落
// config_specs 消费的常量与 resolve_cua_factory_options；build_cua_agent_card /
// cua_model_rejects_images（cua_defaults.py:44-58/:60-77，消费方为 cua 子代理
// 构造与 image-modality probe）⤴️cua 子代理构造章节（偏差 D-15.16-e10 ②）。

package tools

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// cuaDefaultsLogComponent CUA 缺省值组件日志。
	cuaDefaultsLogComponent = logger.ComponentCommon

	// DefaultCuaAgentMaxIterations 高于 agent-core create_cua_agent 默认（25）：
	// 感知-行动-验证循环每次动作后重新截窗，预算太小会在任务中途耗尽。
	// Python: DEFAULT_CUA_AGENT_MAX_ITERATIONS (cua_defaults.py:17)
	DefaultCuaAgentMaxIterations = 100
	// DefaultCuaSnapshotKeepLastK agent-core 默认：窗口截图在子代理上下文里全量保留；
	// 3 适配双窗口任务；1 在单窗口任务下上下文减半。
	// Python: DEFAULT_CUA_SNAPSHOT_KEEP_LAST_K (cua_defaults.py:21)
	DefaultCuaSnapshotKeepLastK = 3
	// DefaultBrowserAgentMaxIterations Python: browser_defaults.py:3
	DefaultBrowserAgentMaxIterations = 100
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveCuaFactoryOptions 把 react.subagents.cua_agent 旋钮映射到
// build_cua_agent_config kwargs。agent-core 在配置期对坏值抛 ValueError 会
// 中断整个 agent 构建；因此这里带 warning 丢弃非法项，agent-core 缺省生效。
// Python: resolve_cua_factory_options (cua_defaults.py:64-107)
func ResolveCuaFactoryOptions(subCfg any) map[string]any {
	cfg, _ := subCfg.(map[string]any)
	options := map[string]any{}

	if rawK, present := cfg["snapshot_keep_last_k"]; present && rawK != nil {
		keep := 0
		switch v := rawK.(type) {
		case int:
			keep = v
		case float64:
			if v == float64(int(v)) {
				keep = int(v)
			}
		case string:
			if isAllDigits(v) {
				keep = parseIntSafe(v)
			}
		}
		if keep >= 1 {
			options["cua_snapshot_keep_last_k"] = keep
		} else {
			logger.Warn(cuaDefaultsLogComponent).
				Msg("[cua_agent] ignoring snapshot_keep_last_k (need an int >= 1); using default")
		}
	}

	if rawPause, present := cfg["pause_on_user_input"]; present && rawPause != nil {
		switch v := rawPause.(type) {
		case bool:
			options["cua_pause_on_user_input"] = v
		case string:
			switch v {
			case "true", "false":
				options["cua_pause_on_user_input"] = v == "true"
			default:
				logger.Warn(cuaDefaultsLogComponent).
					Msg("[cua_agent] ignoring pause_on_user_input (need true/false); keeping the default (off)")
			}
		default:
			logger.Warn(cuaDefaultsLogComponent).
				Msg("[cua_agent] ignoring pause_on_user_input (need true/false); keeping the default (off)")
		}
	}

	return options
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isAllDigits 纯数字判断（Python str.isdigit()）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseIntSafe 安全转 int（调用点已保证 isAllDigits）。
func parseIntSafe(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
