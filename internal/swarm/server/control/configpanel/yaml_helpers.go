// Package configpanel config.yaml 读写助手与 provider 表（27.4）。
// 对齐 Python common/config.py 的 load_yaml_round_trip/_set_config_path 与
// ProviderType 枚举派生的 available_model_providers。
package configpanel

import (
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	llmSchema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

// AscendAffinityProvider 昇腾亲和 provider 名（Python ASCEND_AFFINITY_PROVIDER）。
var AscendAffinityProvider = llmSchema.ProviderTypeAscendAffinity.String()

// ──────────────────────────── 全局变量 ────────────────────────────

// availableModelProviders 可用 provider 列表（Python:
// [provider.value for provider in ProviderType]，23.3 起由枚举生成）。
var availableModelProviders = func() []string {
	out := make([]string, 0, int(llmSchema.ProviderTypeAscendAffinity)+1)
	for p := llmSchema.ProviderTypeOpenAI; p <= llmSchema.ProviderTypeAscendAffinity; p++ {
		out = append(out, p.String())
	}
	return out
}()

// ──────────────────────────── 导出函数 ────────────────────────────

// AvailableModelProviders 返回可用 provider 列表。
func AvailableModelProviders() []string { return availableModelProviders }

// IsAvailableProvider provider 是否可用（Python: val not in available_model_providers）。
func IsAvailableProvider(provider string) bool {
	for _, p := range availableModelProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// ReadRawConfig 读 config.yaml 原始表（Python get_config_raw 等价；失败回空表）。
func ReadRawConfig() map[string]any {
	data, err := os.ReadFile(pathutil.ConfigFile())
	if err != nil {
		return map[string]any{}
	}
	var loaded map[string]any
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		logger.Warn(logComponent).Err(err).Msg("read config.yaml failed")
		return map[string]any{}
	}
	if loaded == nil {
		return map[string]any{}
	}
	return loaded
}

// ReplaceTeamsInConfig 替换 config.yaml 中的 modes.team 配置。
// Python: replace_teams_in_config (common/config.py L989-1038)。
// 空数组删除 modes.team 节点；非空经 SetRoundTrip 保注释写回；
// agents 写 web_config_panel.agent_team_agents。
func ReplaceTeamsInConfig(params map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	if _, err := cfg.Load(); err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	teamData, hasTeam := params["team"]
	if hasTeam {
		if teamList, ok := teamData.([]any); ok && len(teamList) == 0 {
			data, err := cfg.Load()
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}
			if modes, ok := data["modes"].(map[string]any); ok {
				if _, hasTeamKey := modes["team"]; hasTeamKey {
					delete(modes, "team")
					if err := cfg.Save(data); err != nil {
						return fmt.Errorf("保存配置失败（删除 modes.team）: %w", err)
					}
					logger.Info(logComponent).Msg("modes.team empty array, config node deleted")
				}
			}
			return nil
		}
		if err := cfg.SetRoundTrip("modes.team", teamData); err != nil {
			return fmt.Errorf("写入 modes.team 失败: %w", err)
		}
	}

	agentsData, hasAgents := params["agents"]
	if hasAgents {
		if err := cfg.SetRoundTrip("web_config_panel.agent_team_agents", agentsData); err != nil {
			return fmt.Errorf("写入 web_config_panel.agent_team_agents 失败: %w", err)
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mutateYAMLFile 读改写 config.yaml（Python load_yaml_round_trip + dump 的等价路径）。
func mutateYAMLFile(mutator func(root map[string]any)) bool {
	data, err := os.ReadFile(pathutil.ConfigFile())
	if err != nil {
		return false
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		logger.Warn(logComponent).Err(err).Msg("unmarshal config.yaml failed")
		return false
	}
	if root == nil {
		root = map[string]any{}
	}
	mutator(root)
	out, err := yaml.Marshal(root)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("marshal config.yaml failed")
		return false
	}
	if err := os.WriteFile(pathutil.ConfigFile(), out, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("write config.yaml failed")
		return false
	}
	return true
}
