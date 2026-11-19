package common

import (
	"encoding/json"
	"strings"

	externalcli "github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli"
)

// 本文件移植外部 CLI 设置的「面板读展平」面。
// Python 源：jiuwenswarm/common/config_panel/external_cli_read.py（98 行）
// 与 gateway/common/config_panel/external_cli_read.py（32 行 gateway 子集副本，
// 头注声明「改动请同步两侧」）。偏差 D-15.16-d5：Go 无分仓副本需求，两份合一
// 实现；gateway 副本比母仓少 custom_agents_config_value/flatten 两个符号，
// 合一后全部可用，无行为差异。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// externalCliAgentKeyPrefix 展平键前缀。
	// Python: f"external_cli_agent_{cli_agent}_{suffix}" (external_cli_read.py:20-22)
	externalCliAgentKeyPrefix = "external_cli_agent_"
	// externalCliAgentsSegment config 段路径 modes.team.jiuwen_team.external_cli_agents
	externalCliAgentsSegment = "external_cli_agents"
	// externalCliPathHistoriesSegment config 段路径 modes.team.jiuwen_team.external_cli_path_histories
	externalCliPathHistoriesSegment = "external_cli_path_histories"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExternalCliAgentKey 展平键名。
// Python: external_cli_agent_key (external_cli_read.py:20-22)
func ExternalCliAgentKey(cliAgent, suffix string) string {
	return externalCliAgentKeyPrefix + cliAgent + "_" + suffix
}

// ExternalCliAgentKinds 返回 claude/codex 与注册表中全部自定义 agent。
// Python: external_cli_agent_kinds (external_cli_read.py:24-28)
func ExternalCliAgentKinds() []string {
	kinds := []string{"claude", "codex"}
	kinds = append(kinds, externalcli.CustomAgentNames()...)
	return kinds
}

// CustomAgentsConfigValue 把当前已注册自定义 agent 描述成配置面板字符串。
// Python: custom_agents_config_value (external_cli_read.py:31-46)
func CustomAgentsConfigValue() string {
	agents := externalcli.ListCustomAgents()
	entries := make([]map[string]any, 0, len(agents))
	for _, agent := range agents {
		entries = append(entries, map[string]any{
			"name":    agent.Name,
			"version": agent.Version,
			"summary": agent.Summary,
			"has_cli": agent.CLIPath != "",
		})
	}
	data, _ := json.Marshal(entries) // Python ensure_ascii=False 等价：UTF-8 直出
	return string(data)
}

// FlattenExternalCliAgentsForConfigPanel 把默认团队的 external_cli_agents 选择
// 投影成面板扁平键，不加载、不探测 provider。
// Python: flatten_external_cli_agents_for_config_panel (external_cli_read.py:59-98)
//
//   - kinds：claude/codex + 注册表自定义 agent（外部注入，便于测试与 gateway 子集）；
//   - uploadedCLIPaths：cli_agent → 其上传 CLI 路径（注册表 GetCustomAgent 的
//     cli_path 字段；内置 kind 恒空，语义对齐 Python _uploaded_cli_path :49-56）。
//
// 返回 map 键为 external_cli_agent_<kind>_<suffix>，值为字符串
//（enabled/use_builtin 布尔的字符串形态，path_histories 为 JSON 串）。
func FlattenExternalCliAgentsForConfigPanel(
	raw map[string]any,
	kinds []string,
	uploadedCLIPaths map[string]string,
) map[string]string {
	agents := nestedSegment(raw, externalCliAgentsSegment)
	histories := nestedSegment(raw, externalCliPathHistoriesSegment)

	// 组装 configured：cli_agent → {"cli_path": ...}（:67-79）
	configured := map[string]map[string]string{}
	if entries, ok := agents.([]any); ok {
		for _, item := range entries {
			cliAgent, cliPath := "", ""
			switch entry := item.(type) {
			case string: // 旧形态：纯字符串条目（:70-72）
				cliAgent = strings.TrimSpace(entry)
			case map[string]any: // dict 条目（:73-75）
				cliAgent = strings.TrimSpace(stringValue(entry["cli_agent"]))
				cliPath = strings.TrimSpace(stringValue(orValue(entry["cli_path"], entry["codex_bin"])))
			}
			if cliAgent != "" {
				configured[cliAgent] = map[string]string{"cli_path": cliPath}
			}
		}
	}

	flat := map[string]string{}
	for _, cliAgent := range kinds {
		entry, hasEntry := configured[cliAgent]
		cliPath := ""
		if hasEntry {
			cliPath = entry["cli_path"]
		}
		// 上传的可执行文件是该自定义 agent 的内建 CLI 选择（:84-85）
		builtin := hasEntry && (cliPath == "" || cliPath == uploadedCLIPaths[cliAgent])
		history := historiesMap(histories, cliAgent)
		flat[ExternalCliAgentKey(cliAgent, "enabled")] = boolStr(hasEntry)
		flat[ExternalCliAgentKey(cliAgent, "use_builtin")] = boolStr(builtin)
		// builtin 时 cli_path 展示为空（:96）
		flat[ExternalCliAgentKey(cliAgent, "cli_path")] = ""
		if !builtin {
			flat[ExternalCliAgentKey(cliAgent, "cli_path")] = cliPath
		}
		flat[ExternalCliAgentKey(cliAgent, "path_histories")] = pathHistoriesJSON(historyPaths(history))
	}
	return flat
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nestedSegment 沿 modes.team.jiuwen_team.<segment> 取段；
// 任一层非 dict 即 nil（Python: agents.get(segment) if isinstance(dict) 语义）。
func nestedSegment(raw map[string]any, segment string) any {
	cur := any(raw)
	for _, key := range []string{"modes", "team", "jiuwen_team"} {
		dict, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = dict[key]
	}
	dict, ok := cur.(map[string]any)
	if !ok {
		return nil
	}
	return dict[segment]
}

// historiesMap 从 histories 段取某 kind 的 history 列表。
func historiesMap(histories any, cliAgent string) []any {
	dict, ok := histories.(map[string]any)
	if !ok {
		return nil
	}
	list, _ := dict[cliAgent].([]any)
	return list
}

// historyPaths 首现去重 + 滤非串/空白（Python :87-93：保留首现，手改配置
// 不产生重复下拉项）。
func historyPaths(history []any) []string {
	paths := []string{}
	for _, item := range history {
		path, ok := item.(string)
		if !ok || strings.TrimSpace(path) == "" {
			continue
		}
		if !containsString(paths, path) {
			paths = append(paths, path)
		}
	}
	return paths
}

// pathHistoriesJSON 历史路径列表 → JSON 串。
// Python: json.dumps(..., ensure_ascii=False) (:97)
func pathHistoriesJSON(paths []string) string {
	if paths == nil {
		paths = []string{}
	}
	data, _ := json.Marshal(paths)
	return string(data)
}

// stringValue any → string（nil 安全）。
func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

// orValue Python `a or b` 语义：a 为空串取 b（:75 cli_path or codex_bin）。
func orValue(a, b any) any {
	if s, ok := a.(string); ok && s != "" {
		return a
	}
	return b
}

// boolStr Python str(bool) 面板约定的小写形态。
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

