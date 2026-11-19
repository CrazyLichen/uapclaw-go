package common

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 本文件移植外部 CLI 设置的「config 写链」。
// Python 源：jiuwenswarm/common/config_panel/config_set_handlers.py external_cli 段
// 与 jiuwenswarm/common/config.py 的三个 update_external_cli_*_in_config。
// YAML 写回走 cfg.SetRoundTrip（对齐 Python ruamel round-trip，保注释）。

// logComponent 写链组件日志。
const externalCliWriteLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 常量 ────────────────────────────

const (
	// externalCliAgentsConfigPath config 段路径。
	// Python: EXTERNAL_CLI_AGENTS_CONFIG_PATH (config.py:56)
	externalCliAgentsConfigPath = "modes.team.jiuwen_team.external_cli_agents"
	// externalCliPathHistoriesConfigPath 路径历史段路径。
	// Python: EXTERNAL_CLI_PATH_HISTORIES_CONFIG_PATH (config.py:57)
	externalCliPathHistoriesConfigPath = "modes.team.jiuwen_team.external_cli_path_histories"
	// externalCliPathHistoryLimit 历史窗口上限（保尾部：最近被替换的路径最需要保留）。
	// Python: EXTERNAL_CLI_PATH_HISTORY_LIMIT = 10 (config.py:59)
	externalCliPathHistoryLimit = 10
	// externalCliAgentKeySuffixes 自定义 agent 的四个设置键后缀。
	// Python: EXTERNAL_CLI_AGENT_KEY_SUFFIXES (external_cli_read.py:16)
)

// externalCliAgentKeySuffixes 见常量组注释（切片形态供遍历）。
var externalCliAgentKeySuffixes = []string{"enabled", "use_builtin", "cli_path", "path_histories"}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsExternalCliAgentConfigKey 判断 key 是否为内置或自定义 agent 的设置键。
// Python: is_external_cli_agent_config_key (config_set_handlers.py:680-682)
func IsExternalCliAgentConfigKey(key string) bool {
	if externalCliBuiltinConfigKeys[key] {
		return true
	}
	for _, cliAgent := range CustomAgentNames() {
		for _, suffix := range externalCliAgentKeySuffixes {
			if key == ExternalCliAgentKey(cliAgent, suffix) {
				return true
			}
		}
	}
	return false
}

// ExternalCliAgentsFromSwitches 从开关参数重建持久化的外部 CLI agent 列表。
// Python: external_cli_agents_from_switches (config_set_handlers.py:1151-1200)
//
// 只有本次保存参数携带其配置键的 agent 会被重校验（路径探测）与重建；未触碰的
// agent 原样保留持久化条目——保存一张卡片绝不重探测、重写或阻塞另一张。
//
//   - detect：路径探测回调（Python 同模块 detect_external_cli_agent；Go 落在
//     gateway 包，注入缝避免 swarm/common→gateway 反向依赖，偏差 D-15.16-d9）。
func ExternalCliAgentsFromSwitches(
	raw map[string]any,
	params map[string]any,
	kinds []string,
	uploadedCLIPaths map[string]string,
	detect func(cliAgent, requestedPath string, useBuiltin bool) map[string]any,
) ([]map[string]any, error) {
	current := FlattenExternalCliAgentsForConfigPanel(raw, kinds, uploadedCLIPaths)
	persisted := nestedSegment(raw, externalCliAgentsSegment)
	untouchedEntries := map[string]map[string]any{}
	if list, ok := persisted.([]any); ok {
		for _, item := range list {
			switch entry := item.(type) {
			case map[string]any:
				cliAgent := strings.TrimSpace(stringValue(entry["cli_agent"]))
				if cliAgent != "" {
					untouchedEntries[cliAgent] = entry
				}
			case string:
				if name := strings.TrimSpace(entry); name != "" {
					untouchedEntries[name] = map[string]any{"cli_agent": name}
				}
			}
		}
	}
	agents := []map[string]any{}
	for _, cliAgent := range kinds {
		// 只重写路径历史的保存不得重建条目：清理写绝不重探测过期 cli_path（:1168-1171）
		touched := false
		prefix := ExternalCliAgentKey(cliAgent, "")
		for key := range params {
			if strings.HasPrefix(key, prefix) && !strings.HasSuffix(key, "_path_histories") {
				touched = true
				break
			}
		}
		if !touched {
			if untouched, kept := untouchedEntries[cliAgent]; kept {
				agents = append(agents, cloneStringMap(untouched))
			}
			continue
		}
		enabled, useBuiltin, requestedPath := externalCliAgentEffectiveState(cliAgent, current, params)
		if !enabled {
			continue
		}
		entry := map[string]any{"cli_agent": cliAgent}
		uploaded := uploadedCLIPaths[cliAgent]
		if useBuiltin && uploaded != "" {
			entry["cli_path"] = uploaded
		} else if !useBuiltin {
			if detect == nil {
				return nil, fmt.Errorf("%s cli_path is not available: detector unavailable", cliAgent)
			}
			detection := detect(cliAgent, requestedPath, useBuiltin)
			status := stringValue(detection["status"])
			if status != "ok" && status != "warning" {
				return nil, fmt.Errorf("%s cli_path is not available: %s",
					cliAgent, orElse(stringValue(detection["message"]), status))
			}
			entry["cli_path"] = strings.TrimSpace(stringValue(detection["path"]))
		}
		agents = append(agents, entry)
	}
	return agents, nil
}

// ExternalCliPathHistoriesFromSwitches 合并持久化 CLI 路径历史与本次保存的历史参数。
// Python: external_cli_path_histories_from_switches (config_set_handlers.py:1202-1238)
// 切换到另一外部路径时把此前生效的路径追加进历史，使切换永不丢失；
// 当前生效路径不进历史（它永远是下拉框第一项）。
func ExternalCliPathHistoriesFromSwitches(
	raw map[string]any,
	params map[string]any,
	kinds []string,
	uploadedCLIPaths map[string]string,
) map[string][]string {
	current := FlattenExternalCliAgentsForConfigPanel(raw, kinds, uploadedCLIPaths)
	merged := map[string][]string{}
	for _, cliAgent := range kinds {
		historyKey := ExternalCliAgentKey(cliAgent, "path_histories")
		source := "[]"
		if v, ok := params[historyKey]; ok {
			source = stringValue(v)
		} else if v, ok := current[historyKey]; ok {
			source = v
		}
		history := []string{}
		var parsed []any
		if err := json.Unmarshal([]byte(orElse(source, "[]")), &parsed); err == nil {
			for _, item := range parsed {
				path := stringValue(item)
				if strings.TrimSpace(path) != "" {
					history = append(history, path)
				}
			}
		}
		cliPathKey := ExternalCliAgentKey(cliAgent, "cli_path")
		if _, touched := params[cliPathKey]; touched {
			oldPath := strings.TrimSpace(current[cliPathKey])
			_, _, newPath := externalCliAgentEffectiveState(cliAgent, current, params)
			// 切到内建 CLI 时新路径为空串：被替换的自定义路径仍须进历史（:1229-1233）
			if oldPath != "" && oldPath != newPath && !containsString(history, oldPath) {
				history = append(history, oldPath)
			}
		}
		if len(history) > 0 {
			merged[cliAgent] = history
		}
	}
	return merged
}

// NewlyEnabledExternalCliAgents 返回本次保存新启用的 kind：保存前缺席、保存后在场。
// Python: newly_enabled_external_cli_agents (config_set_handlers.py:1240-1256)
func NewlyEnabledExternalCliAgents(raw map[string]any, entries []map[string]any) []string {
	persisted := nestedSegment(raw, externalCliAgentsSegment)
	before := map[string]bool{}
	if list, ok := persisted.([]any); ok {
		for _, item := range list {
			name := ""
			switch entry := item.(type) {
			case map[string]any:
				name = stringValue(entry["cli_agent"])
			case string:
				name = entry
			}
			if name = strings.TrimSpace(name); name != "" {
				before[name] = true
			}
		}
	}
	now := []string{}
	for _, entry := range entries {
		name := strings.TrimSpace(stringValue(entry["cli_agent"]))
		if name != "" && !before[name] {
			now = append(now, name)
		}
	}
	return now
}

// RefreshExternalCliBuiltinModelsWrapper 保存链的 best-effort 包装：
// 内建模型目录对拍失败只警告、不阻塞开关。
// Python: refresh_external_cli_builtin_models 包装 (config_set_handlers.py:1258-1277)
func RefreshExternalCliBuiltinModelsWrapper(
	raw map[string]any,
	entries []map[string]any,
	enabledNow []string,
	refresh func(entries []any, enabledNow []string) []string,
) {
	if len(entries) == 0 {
		return
	}
	entryList := make([]any, 0, len(entries))
	for _, entry := range entries {
		entryList = append(entryList, entry)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(externalCliWriteLogComponent).
					Str("detail", fmt.Sprintf("%v", r)).
					Msg("[config.set] external CLI builtin model check failed")
			}
		}()
		refresh(entryList, enabledNow)
	}()
}

// UpdateExternalCliBuiltinModelsInConfig 替换指定 CLI kind 的内建模型目录。
// Python: update_external_cli_builtin_models_in_config (config.py:2994-3029)
// 空目录移除该键（该 kind 回落到 CLI 默认模型）。目录已在配置中的部分必须在
// 重写时幸存——开关载荷只点名 CLI 种类。
// cfg nil 时回落默认 config.New("")（对齐 Python CONFIG_YAML_PATH 全局）。
func UpdateExternalCliBuiltinModelsInConfig(
	cfg *config.Config,
	catalogs map[string][]map[string]any,
) error {
	if len(catalogs) == 0 {
		return nil
	}
	cfg, err := resolveWriteConfig(cfg)
	if err != nil {
		return err
	}
	entriesAny := cfg.Get(externalCliAgentsConfigPath)
	entries, ok := entriesAny.([]any)
	if !ok {
		return nil
	}
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		catalog, named := catalogs[strings.TrimSpace(stringValue(entry["cli_agent"]))]
		if !named {
			continue
		}
		if len(catalog) > 0 {
			entry["builtin_models"] = anyListOf(catalog)
		} else {
			delete(entry, "builtin_models")
		}
	}
	// SetRoundTrip 以整段替换承载就地修改
	return cfg.SetRoundTrip(externalCliAgentsConfigPath, entries)
}

// UpdateExternalCliPathHistoriesInConfig 持久化各 kind 的 CLI 路径历史。
// Python: update_external_cli_path_histories_in_config (config.py:2958-2992)
// 空列表的 kind 被丢弃；空映射移除整键。保尾部窗口：从头部丢弃会逐出最近被
// 替换的路径——正是下拉框最需要保留的那个。
func UpdateExternalCliPathHistoriesInConfig(cfg *config.Config, histories map[string][]string) error {
	normalized := map[string][]string{}
	for cliAgent, paths := range histories {
		name := strings.TrimSpace(cliAgent)
		if name == "" {
			continue
		}
		deduped := []string{}
		for _, path := range paths {
			value := strings.TrimSpace(path)
			if value != "" && !containsString(deduped, value) {
				deduped = append(deduped, value)
			}
		}
		if len(deduped) > 0 {
			if len(deduped) > externalCliPathHistoryLimit {
				deduped = deduped[len(deduped)-externalCliPathHistoryLimit:]
			}
			normalized[name] = deduped
		}
	}
	cfg, err := resolveWriteConfig(cfg)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil // Python 空映射 pop 整键；Go SetRoundTrip 不支持删除，空映射跳过（键保持原值）
	}
	return cfg.SetRoundTrip(externalCliPathHistoriesConfigPath, normalized)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveWriteConfig 写链配置解析：nil 回落默认路径。
func resolveWriteConfig(cfg *config.Config) (*config.Config, error) {
	if cfg != nil {
		return cfg, nil
	}
	return config.New("")
}

// externalCliBuiltinConfigKeys 内置 kind 的设置键闭集。
// Python: EXTERNAL_CLI_AGENT_CONFIG_KEYS (config_set_handlers.py:462-470)
var externalCliBuiltinConfigKeys = buildBuiltinConfigKeys()

func buildBuiltinConfigKeys() map[string]bool {
	keys := map[string]bool{}
	for _, kind := range []string{"claude", "codex"} {
		for _, suffix := range externalCliAgentKeySuffixes {
			keys[ExternalCliAgentKey(kind, suffix)] = true
		}
	}
	return keys
}

// externalCliAgentEffectiveState 取某 kind 本次保存的生效状态三元组。
// Python: external_cli_agent_effective_state (config_set_handlers.py:1136-1149)
// 返回 (enabled, use_builtin, requested_path)；params 缺键回落当前展平值。
func externalCliAgentEffectiveState(
	cliAgent string,
	current map[string]string,
	params map[string]any,
) (bool, bool, string) {
	enabledKey := ExternalCliAgentKey(cliAgent, "enabled")
	useBuiltinKey := ExternalCliAgentKey(cliAgent, "use_builtin")
	cliPathKey := ExternalCliAgentKey(cliAgent, "cli_path")
	enabled := parseConfigSwitchBool(coalesceParam(params[enabledKey], current[enabledKey]))
	useBuiltin := parseConfigSwitchBool(coalesceParam(params[useBuiltinKey], current[useBuiltinKey]))
	if useBuiltin {
		return enabled, true, ""
	}
	return enabled, false, strings.TrimSpace(stringValue(coalesceParam(params[cliPathKey], current[cliPathKey])))
}

// parseConfigSwitchBool 面板开关布尔解析。
// Python: parse_config_switch_bool (config_set_handlers.py:684-687)
func parseConfigSwitchBool(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	switch strings.ToLower(strings.TrimSpace(stringValue(value))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// coalesceParam Python `params.get(key, current[key])` 语义：params 显式携带
// （含布尔 false 的显式关闭）优先，否则回落展平值。
func coalesceParam(param, fallback any) any {
	if param != nil {
		if _, isBool := param.(bool); isBool {
			return param
		}
		if s, isStr := param.(string); isStr && s != "" {
			return param
		}
	}
	return fallback
}

// cloneStringMap 浅拷贝（untouched 条目原样保留时防共享突变）。
func cloneStringMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// anyListOf map 切片 → any 切片。
func anyListOf(dicts []map[string]any) []any {
	out := make([]any, 0, len(dicts))
	for _, dict := range dicts {
		out = append(out, dict)
	}
	return out
}

// orElse Python `a or b`（字符串版）。
func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
