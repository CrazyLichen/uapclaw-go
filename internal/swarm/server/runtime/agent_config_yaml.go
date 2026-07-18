package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UpsertSubagentInConfig 在 react.subagents.<name> 中添加或更新 agent 启用状态。
// 对齐 Python: upsert_subagent_in_config(name, enabled)
//
// 自动创建不存在的 react / subagents 段。
// 保留已有的其他 subagent 配置键（如 max_iterations 等）。
func UpsertSubagentInConfig(name string, enabled bool) error {
	return upsertSubagentInConfigAt(name, enabled, pathutil.ConfigFile())
}

// RemoveSubagentFromConfig 从 react.subagents.<name> 中删除 agent 条目。
// 对齐 Python: remove_subagent_from_config(name)
//
// 返回 true 表示找到并删除，false 表示条目不存在。
func RemoveSubagentFromConfig(name string) (bool, error) {
	return removeSubagentFromConfigAt(name, pathutil.ConfigFile())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// upsertSubagentInConfigAt 在指定配置文件中添加或更新 agent 启用状态。
// 对齐 Python: upsert_subagent_in_config(name, enabled)
func upsertSubagentInConfigAt(name string, enabled bool, configPath string) error {
	// 步骤 1: 校验名称
	// 对齐 Python: target = str(name or "").strip(); if not target: raise ValueError(...)
	target := strings.TrimSpace(name)
	if target == "" {
		return fmt.Errorf("subagent name is required")
	}

	// 步骤 2: 读取配置文件
	// 对齐 Python: data = load_yaml_round_trip(CONFIG_YAML_PATH)
	data, err := loadYAMLForRoundTrip(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 步骤 3: 确保 react.subagents 结构存在
	// 对齐 Python: if "react" not in data or not isinstance(data["react"], dict): data["react"] = {}
	react, _ := data["react"].(map[string]any)
	if react == nil {
		react = make(map[string]any)
		data["react"] = react
	}
	subagents, _ := react["subagents"].(map[string]any)
	if subagents == nil {
		subagents = make(map[string]any)
		react["subagents"] = subagents
	}

	// 步骤 4: 在目标 agent 条目中设置 enabled
	// 对齐 Python: if target not in subagents or not isinstance(subagents[target], dict): subagents[target] = {}
	// subagents[target]["enabled"] = bool(enabled)
	agentCfg, _ := subagents[target].(map[string]any)
	if agentCfg == nil {
		agentCfg = make(map[string]any)
		subagents[target] = agentCfg
	}
	agentCfg["enabled"] = enabled

	// 步骤 5: 写回配置文件
	// 对齐 Python: dump_yaml_round_trip(CONFIG_YAML_PATH, data)
	return dumpYAMLForRoundTrip(configPath, data)
}

// removeSubagentFromConfigAt 从指定配置文件中删除 agent 条目。
// 对齐 Python: remove_subagent_from_config(name)
func removeSubagentFromConfigAt(name string, configPath string) (bool, error) {
	// 步骤 1: 校验名称
	// 对齐 Python: target = str(name or "").strip(); if not target: raise ValueError(...)
	target := strings.TrimSpace(name)
	if target == "" {
		return false, fmt.Errorf("subagent name is required")
	}

	// 步骤 2: 读取配置文件
	// 对齐 Python: data = load_yaml_round_trip(CONFIG_YAML_PATH)
	data, err := loadYAMLForRoundTrip(configPath)
	if err != nil {
		return false, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 步骤 3: 查找并删除条目
	// 对齐 Python: react = data.get("react"); if not isinstance(react, dict): return False
	react, _ := data["react"].(map[string]any)
	if react == nil {
		return false, nil
	}
	subagents, _ := react["subagents"].(map[string]any)
	if subagents == nil {
		return false, nil
	}
	if _, exists := subagents[target]; !exists {
		return false, nil
	}

	// 步骤 4: 删除条目
	// 对齐 Python: del subagents[target]
	delete(subagents, target)

	// 步骤 5: 写回配置文件
	// 对齐 Python: dump_yaml_round_trip(CONFIG_YAML_PATH, data)
	return true, dumpYAMLForRoundTrip(configPath, data)
}

// loadYAMLForRoundTrip 读取 YAML 文件用于往返修改（保留格式）。
// 对齐 Python: load_yaml_round_trip(path)
func loadYAMLForRoundTrip(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}
	if data == nil {
		data = make(map[string]any)
	}
	return data, nil
}

// dumpYAMLForRoundTrip 将修改后的 YAML 数据写回文件。
// 对齐 Python: dump_yaml_round_trip(path, data)
func dumpYAMLForRoundTrip(path string, data map[string]any) error {
	cfgInst, err := config.New(path)
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	return cfgInst.Save(data)
}
