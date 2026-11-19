package common

import (
	"encoding/json"
	"fmt"
	"os"
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
	// externalTransportConfigPath 外部回传通道段路径。
	// Python: EXTERNAL_TRANSPORT_CONFIG_PATH (config.py:60)
	externalTransportConfigPath = "modes.team.jiuwen_team.external_transport"
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
//   - 路径探测直调同包 DetectExternalCLIAgent（Python 同模块直调语义；
//     D-15.16-d9 已回收——detect 已按 Python 家移位到 swarm/common）。
func ExternalCliAgentsFromSwitches(
	raw map[string]any,
	params map[string]any,
	kinds []string,
	uploadedCLIPaths map[string]string,
) ([]map[string]any, error) {
	current := FlattenExternalCliAgentsForConfigPanel(raw, kinds, uploadedCLIPaths)
	persisted := nestedSegment(raw, externalCliAgentsSegment)
	untouchedEntries := map[string]map[string]any{}
	if list, ok := persisted.([]any); ok {
		for _, item := range list {
			switch entry := item.(type) {
			case map[string]any:
				cliAgent := strings.TrimSpace(config.String(entry["cli_agent"]))
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
			detection := DetectExternalCLIAgent(cliAgent, requestedPath, useBuiltin)
			status := config.String(detection["status"])
			if status != "ok" && status != "warning" {
				return nil, fmt.Errorf("%s cli_path is not available: %s",
					cliAgent, orElse(config.String(detection["message"]), status))
			}
			entry["cli_path"] = strings.TrimSpace(config.String(detection["path"]))
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
			source = config.String(v)
		} else if v, ok := current[historyKey]; ok {
			source = v
		}
		history := []string{}
		var parsed []any
		if err := json.Unmarshal([]byte(orElse(source, "[]")), &parsed); err == nil {
			for _, item := range parsed {
				path := config.String(item)
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
				name = config.String(entry["cli_agent"])
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
		name := strings.TrimSpace(config.String(entry["cli_agent"]))
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
		catalog, named := catalogs[strings.TrimSpace(config.String(entry["cli_agent"]))]
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
	return enabled, false, strings.TrimSpace(config.String(coalesceParam(params[cliPathKey], current[cliPathKey])))
}

// parseConfigSwitchBool 面板开关布尔解析。
// Python: parse_config_switch_bool (config_set_handlers.py:684-687)
func parseConfigSwitchBool(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	switch strings.ToLower(strings.TrimSpace(config.String(value))) {
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

// ──────────────────────────── 导出函数（保存段，15.16d 收口批） ────────────────────────────

// BuildExternalCliPublishURL 计算外部团队事件发布 URL。
// Python: build_external_cli_publish_url (config_set_handlers.py:1281-1292)。
// WEB_HOST 为通配地址时回落 127.0.0.1；IPv6 裸地址加方括号。
func BuildExternalCliPublishURL() string {
	const defaultPublishHost = "127.0.0.1"
	const defaultPublishPort = "19000"
	const publishPath = "/ws"
	host := strings.TrimSpace(firstNonEmpty(os.Getenv("WEB_HOST"), defaultPublishHost))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = defaultPublishHost
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	port := strings.TrimSpace(firstNonEmpty(os.Getenv("WEB_PORT"), defaultPublishPort))
	return fmt.Sprintf("ws://%s:%s%s", host, port, publishPath)
}

// UpdateExternalCliAgentsInConfig 重写默认团队的外部 CLI agent 开关列表。
// Python: update_external_cli_agents_in_config (config.py:2913-2952)。
// 开关载荷只点名 CLI 种类，已配置的 builtin_models 目录必须在重写中幸存
// （_carry_over_builtin_models）；存在跨进程成员时挂/保 hybrid transport
// 及其 publish URL，全部关闭时移除该键。
func UpdateExternalCliAgentsInConfig(cfg *config.Config, entries []map[string]any, publishURL string) error {
	cfg, err := resolveWriteConfig(cfg)
	if err != nil {
		return err
	}
	normalized := normalizeExternalCliAgents(anyListOf(entries))
	// 开关载荷只点名 CLI 种类：已配置的 builtin_models 必须幸存
	existingAny := cfg.Get(externalCliAgentsConfigPath)
	if existing, ok := existingAny.([]any); ok {
		for _, raw := range existing {
			old, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(config.String(old["cli_agent"]))
			catalog, hasCatalog := old["builtin_models"]
			if name == "" || !hasCatalog {
				continue
			}
			for _, item := range normalized {
				if item["cli_agent"] == name && item["builtin_models"] == nil {
					item["builtin_models"] = catalog
				}
			}
		}
	}
	if len(normalized) > 0 {
		if err := cfg.SetRoundTrip(externalCliAgentsConfigPath, anyListOf(normalized)); err != nil {
			return err
		}
	} else {
		if err := cfg.SetRoundTrip(externalCliAgentsConfigPath, nil); err != nil {
			return err
		}
	}
	// external_transport 段：有跨进程成员（非 claude）就需要 hybrid + publish URL
	if requiresExternalTransport(normalized) {
		transportAny := cfg.Get(externalTransportConfigPath)
		transportType := ""
		params := map[string]any{}
		if transport, ok := transportAny.(map[string]any); ok {
			transportType = strings.TrimSpace(config.String(transport["type"]))
			if p, ok := transport["params"].(map[string]any); ok {
				params = p
			}
		}
		if transportType != "" && transportType != "hybrid" {
			return fmt.Errorf("external_cli_agents needs a cross-process agent but external_transport.type must be hybrid")
		}
		if config.String(params["external_publish_url"]) == "" {
			url := strings.TrimSpace(publishURL)
			if url == "" {
				return fmt.Errorf("external_cli_agents needs a cross-process agent but external_cli_publish_url is empty")
			}
			params["external_publish_url"] = url
		}
		if err := cfg.SetRoundTrip(externalTransportConfigPath, map[string]any{"type": "hybrid", "params": params}); err != nil {
			return err
		}
	} else {
		if err := cfg.SetRoundTrip(externalTransportConfigPath, nil); err != nil {
			return err
		}
	}
	return nil
}

// ApplyExternalCliConfigKeys 处理 config.set/save_all 中 external_cli_agent_* 键。
// Python: config_set_handlers 保存段 is_external_cli_agent_config_key 分支
// （:1629-1666，含依赖门控与 refresh 探测）。
//   - 无触碰键直接返回 nil；
//   - cc/codex 的依赖安装门控随 D-15.16-d6 留缝（安装器不移植）；
//   - refresh 由调用方注入（catalog 探测在 externalcli 包，反向依赖被禁止）。
func ApplyExternalCliConfigKeys(raw map[string]any, params map[string]any,
	refresh func(entries []any, enabledNow []string) []string) error {
	touched := false
	for key := range params {
		if IsExternalCliAgentConfigKey(key) {
			touched = true
			break
		}
	}
	if !touched {
		return nil
	}
	kinds := ExternalCliAgentKinds()
	uploadedCLIPaths := map[string]string{}
	for _, name := range kinds {
		if agent := GetCustomAgent(name); agent != nil && agent.CLIPath != "" {
			uploadedCLIPaths[name] = agent.CLIPath
		}
	}
	// 依赖门控：codex/claude touched → ensure_*_dependency（D-15.16-d6 ⤴️15.7/15.8；
	// dsh 等自定义 agent 不触发门控）
	entries, err := ExternalCliAgentsFromSwitches(raw, params, kinds, uploadedCLIPaths)
	if err != nil {
		return err
	}
	if err := UpdateExternalCliAgentsInConfig(nil, entries, BuildExternalCliPublishURL()); err != nil {
		return err
	}
	histories := ExternalCliPathHistoriesFromSwitches(raw, params, kinds, uploadedCLIPaths)
	if err := UpdateExternalCliPathHistoriesInConfig(nil, histories); err != nil {
		return err
	}
	RefreshExternalCliBuiltinModelsWrapper(raw, entries,
		NewlyEnabledExternalCliAgents(raw, entries), refresh)
	return nil
}

// ──────────────────────────── 非导出函数（保存段） ────────────────────────────

// requiresExternalTransport 是否存在从另一进程到达团队的启用成员。
// Python: _requires_external_transport (config.py:2622-2631)。claude 进程内
// 挂团队工具；codex/自定义 agent 经 team MCP server 或 messager 跨进程，
// 需要 hybrid transport 及其 publish URL。
func requiresExternalTransport(entries []map[string]any) bool {
	for _, item := range entries {
		if item["cli_agent"] != "claude" {
			return true
		}
	}
	return false
}

// normalizeExternalCliAgents 归一化外部 CLI agent 列表。
// Python: _normalize_external_cli_agents (config.py:2632-2686)。
//   - 允许种类 = 内置闭集 + 注册表自定义 agent；
//   - cli_path/codex_bin（legacy）去空白；resolve_preserved_cli_path 的
//     Windows 保留路径解析 ⤴️windows_cli_paths 章节；
//   - builtin_models 逐条归一化（name 必填、efforts/default_effort 校验）。
func normalizeExternalCliAgents(list []any) []map[string]any {
	allowed := map[string]bool{}
	for _, kind := range ExternalCliAgentKinds() {
		allowed[kind] = true
	}
	normalized := []map[string]any{}
	seen := map[string]bool{}
	for _, raw := range list {
		var cliAgent string
		item, isDict := raw.(map[string]any)
		switch v := raw.(type) {
		case string:
			cliAgent = strings.TrimSpace(v)
		case map[string]any:
			cliAgent = strings.TrimSpace(config.String(item["cli_agent"]))
		default:
			continue
		}
		if cliAgent == "" || !allowed[cliAgent] || seen[cliAgent] {
			continue
		}
		seen[cliAgent] = true
		entry := map[string]any{"cli_agent": cliAgent}
		if isDict {
			cliPath := strings.TrimSpace(config.String(item["cli_path"]))
			legacyCodexBin := strings.TrimSpace(config.String(item["codex_bin"]))
			if cliPath != "" {
				entry["cli_path"] = cliPath
			} else if cliAgent == "codex" && legacyCodexBin != "" {
				entry["cli_path"] = legacyCodexBin
			}
			if catalog := normalizeBuiltinModels(config.DictList(item["builtin_models"])); len(catalog) > 0 {
				entry["builtin_models"] = anyListOf(catalog)
			}
		}
		normalized = append(normalized, entry)
	}
	return normalized
}

// normalizeBuiltinModels 归一化一个 kind 的内建模型目录。
// Python: _normalize_builtin_models (config.py:2563-2613)：name 必填去重；
// description 非空才保留；efforts 去空白去空；default_effort 必须在 efforts 内。
func normalizeBuiltinModels(list []map[string]any) []map[string]any {
	normalized := []map[string]any{}
	seen := map[string]bool{}
	for _, item := range list {
		name := strings.TrimSpace(config.String(item["name"]))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		entry := map[string]any{"name": name}
		if description := strings.TrimSpace(config.String(item["description"])); description != "" {
			entry["description"] = description
		}
		efforts := []string{}
		for _, effort := range config.StringList(item["efforts"]) {
			if trimmed := strings.TrimSpace(effort); trimmed != "" {
				efforts = append(efforts, trimmed)
			}
		}
		if len(efforts) > 0 {
			entry["efforts"] = efforts
		}
		if defaultEffort := strings.TrimSpace(config.String(item["default_effort"])); defaultEffort != "" {
			found := false
			for _, effort := range efforts {
				if effort == defaultEffort {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			entry["default_effort"] = defaultEffort
		}
		normalized = append(normalized, entry)
	}
	return normalized
}
