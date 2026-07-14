package adapter

import (
	"context"
	"fmt"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerMcpServersFromConfig 从配置注册 MCP 服务。
// 对齐 Python: _register_mcp_servers_from_config(config_base, tag) (line 1169-1213)
//
// 从 configBase["mcp_servers"] 提取 MCP 配置列表，对每个启用的条目调 registerMcpServer。
func (d *DeepAdapter) registerMcpServersFromConfig(ctx context.Context, configBase map[string]any, tag string) error {
	entries := d.extractEnabledMcpServerEntries(configBase)
	for _, entry := range entries {
		config := d.buildMcpServerConfig(entry)
		if config == nil {
			continue
		}
		if err := d.registerMcpServer(ctx, config, tag); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("server_id", config.ServerID).
				Str("tag", tag).
				Msg("MCP 服务注册失败，跳过")
			continue
		}
		d.registeredMCPServerIDs[config.ServerID] = true
		d.registeredMCPServers[config.ServerID] = entry
	}

	logger.Info(logComponent).
		Int("count", len(entries)).
		Str("tag", tag).
		Msg("MCP 服务注册完成")
	return nil
}

// syncMcpServersForRuntime 热同步 MCP 服务配置。
// 对齐 Python: _sync_mcp_servers_for_runtime(config_base, tag) (line 1214-1273)
//
// 对比当前已注册 vs 配置中的，执行 to_remove/to_add/to_check。
func (d *DeepAdapter) syncMcpServersForRuntime(ctx context.Context, configBase map[string]any, tag string) error {
	entries := d.extractEnabledMcpServerEntries(configBase)
	newIDs := make(map[string]bool)
	for _, entry := range entries {
		config := d.buildMcpServerConfig(entry)
		if config != nil {
			newIDs[config.ServerID] = true
		}
	}

	// to_remove: 已注册但新配置中不存在
	for id := range d.registeredMCPServerIDs {
		if !newIDs[id] {
			if err := d.unregisterMcpServer(ctx, id); err != nil {
				logger.Warn(logComponent).Err(err).Str("server_id", id).Msg("MCP 服务移除失败")
			}
			delete(d.registeredMCPServerIDs, id)
			delete(d.registeredMCPServers, id)
		}
	}

	// to_add: 新配置中存在但未注册
	for _, entry := range entries {
		config := d.buildMcpServerConfig(entry)
		if config == nil {
			continue
		}
		if !d.registeredMCPServerIDs[config.ServerID] {
			if err := d.registerMcpServer(ctx, config, tag); err != nil {
				logger.Warn(logComponent).Err(err).Str("server_id", config.ServerID).Msg("MCP 服务注册失败")
				continue
			}
			d.registeredMCPServerIDs[config.ServerID] = true
			d.registeredMCPServers[config.ServerID] = entry
		}
	}

	// to_check: 已注册且新配置中也存在 → 刷新
	for id := range d.registeredMCPServerIDs {
		if newIDs[id] {
			if _, err := runner.GetResourceMgr().RefreshMcpServer(ctx, id); err != nil {
				logger.Warn(logComponent).Err(err).Str("server_id", id).Msg("MCP 服务刷新失败")
			}
		}
	}

	logger.Info(logComponent).Str("tag", tag).Msg("MCP 服务热同步完成")
	return nil
}

// buildMcpServerConfig 从配置条目构建 McpServerConfig。
// 对齐 Python: _build_mcp_server_config() (line 972-1020)
func (d *DeepAdapter) buildMcpServerConfig(entry map[string]any) *mcptypes.McpServerConfig {
	name, _ := entry["name"].(string)
	serverPath, _ := entry["server_path"].(string)
	clientType, _ := entry["client_type"].(string)
	if name == "" || serverPath == "" {
		return nil
	}

	opts := make([]mcptypes.McpServerConfigOption, 0)
	if id, ok := entry["server_id"].(string); ok && id != "" {
		opts = append(opts, mcptypes.WithServerID(id))
	}
	if params, ok := entry["params"].(map[string]any); ok {
		opts = append(opts, mcptypes.WithParams(params))
	}
	if headers, ok := entry["auth_headers"].(map[string]any); ok {
		h := make(map[string]string)
		for k, v := range headers {
			if s, ok := v.(string); ok {
				h[k] = s
			}
		}
		opts = append(opts, mcptypes.WithAuthHeaders(h))
	}

	return mcptypes.NewMcpServerConfig(name, serverPath, clientType, opts...)
}

// extractEnabledMcpServerEntries 从配置提取启用的 MCP 条目。
// 对齐 Python: _extract_enabled_mcp_server_entries() (line 1021-1060)
// 配置结构: configBase["mcp"]["servers"] — 嵌套两级，值为列表
func (d *DeepAdapter) extractEnabledMcpServerEntries(configBase map[string]any) []map[string]any {
	// 对齐 Python: mcp_cfg = config_base.get("mcp", {})
	mcpCfg, _ := configBase["mcp"].(map[string]any)
	if mcpCfg == nil {
		return nil
	}

	// 对齐 Python: servers = mcp_cfg.get("servers", [])
	serversRaw, ok := mcpCfg["servers"]
	if !ok || serversRaw == nil {
		return nil
	}

	// servers 是列表格式 []map[string]any（直接构建）或 []any（JSON 反序列化后）
	var result []map[string]any

	switch s := serversRaw.(type) {
	case []map[string]any:
		for _, entry := range s {
			if d.isMcpServerEntryEnabled(entry) {
				result = append(result, entry)
			}
		}
	case []any:
		for _, item := range s {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if d.isMcpServerEntryEnabled(entry) {
				result = append(result, entry)
			}
		}
	}

	return result
}

// isMcpServerEntryEnabled 检查 MCP 服务器条目是否启用。
// 对齐 Python: enabled 字段默认 true
func (d *DeepAdapter) isMcpServerEntryEnabled(entry map[string]any) bool {
	enabled := true
	if v, ok := entry["enabled"]; ok {
		if b, ok := v.(bool); ok {
			enabled = b
		}
	}
	return enabled
}

// registerMcpServer 注册 MCP 服务到 ResourceMgr。
// 对齐 Python: _register_mcp_server() (line 1061-1100)
func (d *DeepAdapter) registerMcpServer(ctx context.Context, config *mcptypes.McpServerConfig, tag string) error {
	_, err := runner.GetResourceMgr().AddMcpServer(ctx, config, resources_manager.WithMcpTag(resources_manager.Tag(tag)))
	if err != nil {
		return fmt.Errorf("AddMcpServer 失败: %w", err)
	}
	logger.Info(logComponent).
		Str("server_id", config.ServerID).
		Str("server_name", config.ServerName).
		Str("tag", tag).
		Msg("MCP 服务已注册")
	return nil
}

// unregisterMcpServer 从 ResourceMgr 移除 MCP 服务。
// 对齐 Python: _unregister_mcp_server() (line 1101-1130)
func (d *DeepAdapter) unregisterMcpServer(ctx context.Context, serverID string) error {
	_, err := runner.GetResourceMgr().RemoveMcpServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("RemoveMcpServer 失败: %w", err)
	}
	logger.Info(logComponent).
		Str("server_id", serverID).
		Msg("MCP 服务已移除")
	return nil
}
