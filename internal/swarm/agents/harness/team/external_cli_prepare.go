package team

import (
	external "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// 本文件移植团队装配前的外部 CLI agent 预处理。
// Python 源：jiuwenswarm/agents/harness/team/config_loader.py:33-71
// _prepare_external_cli_agents；调用位 :603（_load_team_spec 构建 spec 之前）。

// PrepareExternalCliAgents 在团队 spec 构建前完成外部 CLI agent 条目的收尾：
// ① 各条目的内建模型目录与其 CLI 最近上报的清单就地修正（配置是策略，CLI
// 拥有事实）；② 已启用的自定义 agent（编译期注册缝承载）补 team-mcp stdio
// server 启动命令——其团队工具经该 server 暴露。
// Python: _prepare_external_cli_agents (config_loader.py:33-71)
//
// specDict 是构建 TeamAgentSpec 前的字典形态；无 external_cli_agents 列表直接返回。
func PrepareExternalCliAgents(specDict map[string]any) {
	entries, ok := specDict["external_cli_agents"].([]any)
	if !ok {
		return
	}
	// ① 目录与最近上报对拍（external_cli_catalog.apply_reported_models，:48-53）
	external.ApplyReportedModels(entries)
	// ② 自定义 agent 补 team-mcp server 命令（:57-69）
	customNames := swarmcommon.CustomAgentNames()
	if len(customNames) == 0 {
		return
	}
	customSet := map[string]bool{}
	for _, name := range customNames {
		customSet[name] = true
	}
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["cli_agent"].(string)
		if !customSet[name] {
			continue
		}
		// setdefault 语义：已有值不覆盖（Python entry.setdefault("mcp_server_command", ...)）
		if _, has := entry["mcp_server_command"]; !has {
			entry["mcp_server_command"] = swarmcommon.TeamMCPServerCommand()
		}
	}
}
