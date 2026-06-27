package resources_manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer/decorator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpServerResource MCP 服务器资源，记录单个 MCP 服务器的配置、客户端和工具列表。
//
// 对应 Python: McpServerResource (openjiuwen/core/runner/resources_manager/tool_manager.py)
type McpServerResource struct {
	// Config MCP 服务器配置
	Config *mcp.McpServerConfig
	// Client MCP 客户端
	Client mcp.McpClient
	// ToolIDs 该服务器下所有工具 ID
	ToolIDs []string
	// LastUpdateTime 最后更新时间
	LastUpdateTime time.Time
	// ExpiryTime 过期时间（秒），nil 表示永不过期
	ExpiryTime *float64
}

// SysOpToolResource 系统操作工具资源，记录系统操作与其关联的工具 ID。
//
// 对应 Python: SysOpToolResource (openjiuwen/core/runner/resources_manager/tool_manager.py)
type SysOpToolResource struct {
	// SysOpID 系统操作标识
	SysOpID string
	// ToolIDs 关联的工具 ID 列表
	ToolIDs []string
	// LastUpdateTime 最后更新时间
	LastUpdateTime time.Time
}

// ToolMgr 工具管理器，管理工具注册/获取/注销，以及 MCP 服务器工具的生命周期。
//
// 对应 Python: ToolMgr (openjiuwen/core/runner/resources_manager/tool_manager.py)
type ToolMgr struct {
	// tools 工具注册表
	tools *ThreadSafeDict[string, tool.Tool]
	// mcpServerNameToIDs 服务器名称到 server ID 列表的映射
	mcpServerNameToIDs map[string][]string
	// mcpServerResources server ID 到 McpServerResource 的映射
	mcpServerResources map[string]*McpServerResource
	// sysOpResources 系统操作 ID 到 SysOpToolResource 的映射
	sysOpResources map[string]*SysOpToolResource
	// mcpServerLocks server ID 粒度锁，序列化同一 server_id 的并发 add_tool_server 调用
	mcpServerLocks map[string]*sync.Mutex
	// mu 全局读写锁，保护 mcpServerNameToIDs / mcpServerResources / sysOpResources / mcpServerLocks
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolMgr 创建工具管理器。
//
// 对应 Python: ToolMgr.__init__()
func NewToolMgr() *ToolMgr {
	return &ToolMgr{
		tools:              NewThreadSafeDict[string, tool.Tool](),
		mcpServerNameToIDs: make(map[string][]string),
		mcpServerResources: make(map[string]*McpServerResource),
		sysOpResources:     make(map[string]*SysOpToolResource),
		mcpServerLocks:     make(map[string]*sync.Mutex),
	}
}

// AddTool 注册工具，重复添加返回错误。
//
// 对应 Python: ToolMgr.add_tool(tool_id, tool)
func (m *ToolMgr) AddTool(toolID string, t tool.Tool) error {
	if m.tools.Contains(toolID) {
		return exception.BuildError(
			exception.StatusResourceAddError,
			exception.WithParam("tool_id", toolID),
			exception.WithParam("reason", "工具已存在"),
		)
	}
	m.tools.Set(toolID, t)
	return nil
}

// GetTool 获取工具，如果 session 非 nil 则通过 DecorateToolWithTrace 添加追踪装饰。
//
// 对应 Python: ToolMgr.get_tool(tool_id, session)
func (m *ToolMgr) GetTool(toolID string, session decorator.TracerSession) (tool.Tool, error) {
	t := m.tools.Get(toolID)
	if t == nil {
		return nil, exception.BuildError(
			exception.StatusResourceGetError,
			exception.WithParam("resource_id", toolID),
			exception.WithParam("resource_type", "tool"),
			exception.WithParam("reason", "工具未找到"),
		)
	}
	if session != nil {
		return decorator.DecorateToolWithTrace(t, session), nil
	}
	return t, nil
}

// GetMcpTool 通过工具名和服务器 ID 获取 MCP 工具。
//
// 对应 Python: ToolMgr.get_mcp_tool(tool_name, server_id, session)
func (m *ToolMgr) GetMcpTool(ctx context.Context, toolName, serverID string, session decorator.TracerSession) (tool.Tool, error) {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return nil, exception.BuildError(
			exception.StatusResourceMCPToolGetError,
			exception.WithParam("server_id", serverID),
			exception.WithParam("tool_name", toolName),
			exception.WithParam("reason", "MCP 服务器资源未找到"),
		)
	}
	toolID := m.GenerateMcpToolID(serverID, resource.Config.ServerName, toolName)
	return m.GetTool(toolID, session)
}

// GetMcpTools 获取指定 MCP 服务器下的所有工具。
//
// 对应 Python: ToolMgr.get_mcp_tools(server_id, session)
func (m *ToolMgr) GetMcpTools(ctx context.Context, serverID string, session decorator.TracerSession) ([]tool.Tool, error) {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return nil, nil
	}
	results := make([]tool.Tool, 0, len(resource.ToolIDs))
	for _, toolID := range resource.ToolIDs {
		t, err := m.GetTool(toolID, session)
		if err != nil {
			continue
		}
		results = append(results, t)
	}
	return results, nil
}

// GetMcpToolID 获取 MCP 工具 ID。toolName 为空时返回该服务器下所有工具 ID。
//
// 对应 Python: ToolMgr.get_mcp_tool_id(server_id, tool_name)
func (m *ToolMgr) GetMcpToolID(serverID, toolName string) []string {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return nil
	}
	if toolName == "" {
		result := make([]string, len(resource.ToolIDs))
		copy(result, resource.ToolIDs)
		return result
	}
	toolID := m.GenerateMcpToolID(serverID, resource.Config.ServerName, toolName)
	return []string{toolID}
}

// RemoveTool 移除并返回工具。
//
// 对应 Python: ToolMgr.remove_tool(tool_id)
func (m *ToolMgr) RemoveTool(toolID string) (tool.Tool, error) {
	t := m.tools.Pop(toolID)
	if t == nil {
		return nil, exception.BuildError(
			exception.StatusResourceGetError,
			exception.WithParam("resource_id", toolID),
			exception.WithParam("resource_type", "tool"),
			exception.WithParam("reason", "工具未找到"),
		)
	}
	return t, nil
}

// GenerateMcpToolID 生成 MCP 工具 ID，格式为 {serverID}.{serverName}.{toolName}。
//
// 对应 Python: ToolMgr.generate_mcp_tool_id(server_id, server_name, tool_name)
func (m *ToolMgr) GenerateMcpToolID(serverID, serverName, toolName string) string {
	return fmt.Sprintf("%s.%s.%s", serverID, serverName, toolName)
}

// AddToolServer 添加 MCP 工具服务器，建立连接并注册工具。
// 获取 server_id 粒度锁 → 检查重复 → 创建客户端 → 连接 → 刷新工具 → 更新映射。
//
// 对应 Python: ToolMgr.add_tool_server(server_config, expiry_time)
func (m *ToolMgr) AddToolServer(ctx context.Context, serverConfig *mcp.McpServerConfig, expiryTime *float64) ([]*mcp.McpToolCard, error) {
	serverID := serverConfig.ServerID
	lock := m.mcpServerLock(serverID)
	lock.Lock()
	defer lock.Unlock()

	// 检查是否已注册
	m.mu.RLock()
	existing, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if ok && existing != nil {
		// 已注册 — 返回缓存的工具卡片
		cards := make([]*mcp.McpToolCard, 0)
		for _, toolID := range existing.ToolIDs {
			t := m.tools.Get(toolID)
			if t != nil {
				if mcpTool, isMcp := t.(*mcp.MCPTool); isMcp {
					cards = append(cards, deepCopyMcpToolCard(mcpTool.McpCard()))
				}
			}
		}
		return cards, nil
	}

	// 创建客户端
	client, err := m.createClient(serverConfig)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "TOOL_SERVER_ADD_ERROR").
			Str("server_id", serverID).
			Str("server_name", serverConfig.ServerName).
			Err(err).
			Msg("创建 MCP 客户端失败")
		return nil, exception.BuildError(
			exception.StatusResourceMCPServerAddError,
			exception.WithParam("server_id", serverID),
			exception.WithParam("reason", err.Error()),
			exception.WithCause(err),
		)
	}

	// 连接
	err = client.Connect(ctx)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "TOOL_SERVER_ADD_ERROR").
			Str("server_id", serverID).
			Str("server_name", serverConfig.ServerName).
			Err(err).
			Msg("MCP 服务器连接失败")
		return nil, exception.BuildError(
			exception.StatusResourceMCPServerConnectionError,
			exception.WithParam("server_id", serverID),
			exception.WithParam("reason", err.Error()),
			exception.WithCause(err),
		)
	}

	// 刷新工具
	results, err := m.innerRefreshMcpTools(ctx, client, serverConfig, expiryTime)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "TOOL_SERVER_ADD_ERROR").
			Str("server_id", serverID).
			Str("server_name", serverConfig.ServerName).
			Err(err).
			Msg("刷新 MCP 工具失败")
		return nil, exception.BuildError(
			exception.StatusResourceMCPServerAddError,
			exception.WithParam("server_id", serverID),
			exception.WithParam("reason", err.Error()),
			exception.WithCause(err),
		)
	}

	// 更新名称到 ID 的映射
	m.mu.Lock()
	ids, ok := m.mcpServerNameToIDs[serverConfig.ServerName]
	if !ok {
		ids = make([]string, 0)
	}
	m.mcpServerNameToIDs[serverConfig.ServerName] = append(ids, serverConfig.ServerID)
	m.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "TOOL_SERVER_ADD_SUCCESS").
		Str("server_id", serverID).
		Str("server_name", serverConfig.ServerName).
		Int("tool_count", len(results)).
		Msg("添加 MCP 工具服务器成功")

	return results, nil
}

// RemoveToolServer 移除 MCP 工具服务器，断开连接并清理映射。
//
// 对应 Python: ToolMgr.remove_tool_server(server_id, ignore_not_exist)
func (m *ToolMgr) RemoveToolServer(ctx context.Context, serverID string, ignoreNotExist bool) ([]string, error) {
	m.mu.Lock()
	resource, ok := m.mcpServerResources[serverID]
	if ok {
		delete(m.mcpServerResources, serverID)
	}
	m.mu.Unlock()

	if !ok || resource == nil {
		if !ignoreNotExist {
			return nil, exception.BuildError(
				exception.StatusResourceMCPServerRemoveError,
				exception.WithParam("server_id", serverID),
				exception.WithParam("reason", "服务器不存在"),
			)
		}
		return []string{}, nil
	}

	// 断开连接
	err := resource.Client.Disconnect(ctx)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "TOOL_SERVER_DISCONNECT_WARN").
			Str("server_id", serverID).
			Err(err).
			Msg("移除工具服务器断开连接异常")
	}

	// 移除工具
	m.innerRemoveMcpTools(resource.ToolIDs)

	// 清理名称到 ID 的映射
	m.mu.Lock()
	ids, ok := m.mcpServerNameToIDs[resource.Config.ServerName]
	if ok {
		for i, id := range ids {
			if id == serverID {
				m.mcpServerNameToIDs[resource.Config.ServerName] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(m.mcpServerNameToIDs[resource.Config.ServerName]) == 0 {
			delete(m.mcpServerNameToIDs, resource.Config.ServerName)
		}
	}
	m.mu.Unlock()

	logger.Info(logComponent).
		Str("event_type", "TOOL_SERVER_REMOVE_SUCCESS").
		Str("server_id", serverID).
		Int("tool_count", len(resource.ToolIDs)).
		Msg("移除 MCP 工具服务器成功")

	return resource.ToolIDs, nil
}

// AddSysOperationTools 注册系统操作关联工具。
//
// 对应 Python: ToolMgr.add_sys_operation_tools(sys_op_id, tool_ids)
func (m *ToolMgr) AddSysOperationTools(sysOpID string, toolIDs []string) {
	if len(toolIDs) == 0 {
		return
	}
	copied := make([]string, len(toolIDs))
	copy(copied, toolIDs)
	m.mu.Lock()
	m.sysOpResources[sysOpID] = &SysOpToolResource{
		SysOpID:        sysOpID,
		ToolIDs:        copied,
		LastUpdateTime: time.Now(),
	}
	m.mu.Unlock()
}

// RemoveSysOperationTools 注销系统操作关联工具，返回被注销的工具 ID 列表。
//
// 对应 Python: ToolMgr.remove_sys_operation_tools(sys_op_id)
func (m *ToolMgr) RemoveSysOperationTools(sysOpID string) []string {
	m.mu.Lock()
	resource, ok := m.sysOpResources[sysOpID]
	if ok {
		delete(m.sysOpResources, sysOpID)
	}
	m.mu.Unlock()
	if !ok || resource == nil {
		return []string{}
	}
	return resource.ToolIDs
}

// GetSysOperationToolIDs 获取系统操作关联的工具 ID 列表。
//
// 对应 Python: ToolMgr.get_sys_operation_tool_ids(sys_op_id)
func (m *ToolMgr) GetSysOperationToolIDs(sysOpID string) []string {
	m.mu.RLock()
	resource, ok := m.sysOpResources[sysOpID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return []string{}
	}
	return resource.ToolIDs
}

// RefreshToolServer 刷新 MCP 工具服务器，检查过期后刷新。
//
// 对应 Python: ToolMgr.refresh_tool_server(server_id, skip_not_exist, force)
func (m *ToolMgr) RefreshToolServer(ctx context.Context, serverID string, skipNotExist, force bool) ([]*mcp.McpToolCard, error) {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()

	if !ok || resource == nil {
		if !skipNotExist {
			return nil, exception.BuildError(
				exception.StatusResourceMCPServerRefreshError,
				exception.WithParam("server_id", serverID),
				exception.WithParam("reason", "服务器不存在"),
			)
		}
		return []*mcp.McpToolCard{}, nil
	}

	needRefresh := force
	if !force && resource.ExpiryTime != nil {
		if time.Since(resource.LastUpdateTime).Seconds() >= *resource.ExpiryTime {
			needRefresh = true
		}
	}

	if needRefresh {
		results, err := m.innerRefreshMcpTools(ctx, resource.Client, resource.Config, resource.ExpiryTime)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "TOOL_SERVER_REFRESH_ERROR").
				Str("server_id", serverID).
				Err(err).
				Msg("刷新 MCP 工具服务器失败")
			return nil, exception.BuildError(
				exception.StatusResourceMCPServerRefreshError,
				exception.WithParam("server_id", serverID),
				exception.WithParam("reason", err.Error()),
				exception.WithCause(err),
			)
		}
		return results, nil
	}

	return []*mcp.McpToolCard{}, nil
}

// GetMcpServerIDs 按名称获取 MCP 服务器 ID 列表。
//
// 对应 Python: ToolMgr.get_mcp_server_ids(server_name)
func (m *ToolMgr) GetMcpServerIDs(serverName string) []string {
	m.mu.RLock()
	ids, ok := m.mcpServerNameToIDs[serverName]
	m.mu.RUnlock()
	if !ok {
		return []string{}
	}
	result := make([]string, len(ids))
	copy(result, ids)
	return result
}

// GetMcpClient 获取 MCP 客户端。
//
// 对应 Python: ToolMgr.get_mcp_client(server_id)
func (m *ToolMgr) GetMcpClient(serverID string) (mcp.McpClient, error) {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return nil, exception.BuildError(
			exception.StatusResourceGetError,
			exception.WithParam("resource_id", serverID),
			exception.WithParam("resource_type", "mcp_client"),
			exception.WithParam("reason", "MCP 服务器资源未找到"),
		)
	}
	return resource.Client, nil
}

// GetMcpServerConfig 深拷贝配置返回。
//
// 对应 Python: ToolMgr.get_mcp_server_config(server_id)
func (m *ToolMgr) GetMcpServerConfig(serverID string) (*mcp.McpServerConfig, error) {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return nil, exception.BuildError(
			exception.StatusResourceGetError,
			exception.WithParam("resource_id", serverID),
			exception.WithParam("resource_type", "mcp_server_config"),
			exception.WithParam("reason", "MCP 服务器资源未找到"),
		)
	}
	// 深拷贝配置
	copied := &mcp.McpServerConfig{
		ServerID:   resource.Config.ServerID,
		ServerName: resource.Config.ServerName,
		ServerPath: resource.Config.ServerPath,
		ClientType: resource.Config.ClientType,
	}
	if resource.Config.Params != nil {
		copied.Params = make(map[string]any, len(resource.Config.Params))
		for k, v := range resource.Config.Params {
			copied.Params[k] = v
		}
	}
	if resource.Config.AuthHeaders != nil {
		copied.AuthHeaders = make(map[string]string, len(resource.Config.AuthHeaders))
		for k, v := range resource.Config.AuthHeaders {
			copied.AuthHeaders[k] = v
		}
	}
	if resource.Config.AuthQueryParams != nil {
		copied.AuthQueryParams = make(map[string]string, len(resource.Config.AuthQueryParams))
		for k, v := range resource.Config.AuthQueryParams {
			copied.AuthQueryParams[k] = v
		}
	}
	return copied, nil
}

// GetMcpToolIDs 获取指定服务器下所有工具 ID。
//
// 对应 Python: ToolMgr.get_mcp_tool_ids(server_id)
func (m *ToolMgr) GetMcpToolIDs(serverID string) []string {
	m.mu.RLock()
	resource, ok := m.mcpServerResources[serverID]
	m.mu.RUnlock()
	if !ok || resource == nil {
		return []string{}
	}
	copied := make([]string, len(resource.ToolIDs))
	copy(copied, resource.ToolIDs)
	return copied
}

// Release 释放所有 MCP 连接，遍历所有 MCP 服务器调用 Disconnect，忽略单个错误。
//
// 对应 Python: ToolMgr.release()
func (m *ToolMgr) Release(ctx context.Context) error {
	m.mu.RLock()
	resources := make([]*McpServerResource, 0, len(m.mcpServerResources))
	for _, res := range m.mcpServerResources {
		resources = append(resources, res)
	}
	m.mu.RUnlock()

	var firstErr error
	for _, res := range resources {
		err := res.Client.Disconnect(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logger.Warn(logComponent).
				Str("event_type", "TOOL_SERVER_RELEASE_WARN").
				Str("server_id", res.Config.ServerID).
				Err(err).
				Msg("释放 MCP 服务器连接异常")
			continue
		}
	}
	return firstErr
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createClient 调用 mcp.NewMcpClient 创建 MCP 客户端。
//
// 对应 Python: ToolMgr._create_client(config)
func (m *ToolMgr) createClient(config *mcp.McpServerConfig) (mcp.McpClient, error) {
	return mcp.NewMcpClient(config)
}

// innerRefreshMcpTools 刷新 MCP 工具：list_tools → 注册 MCPTool → 更新 mcpServerResources。
//
// 对应 Python: ToolMgr._inner_refresh_mcp_tools(client, server_config, expiry_time)
func (m *ToolMgr) innerRefreshMcpTools(ctx context.Context, client mcp.McpClient, serverConfig *mcp.McpServerConfig, expiryTime *float64) ([]*mcp.McpToolCard, error) {
	mcpCards, err := client.ListTools(ctx)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusResourceMCPServerRefreshError,
			exception.WithParam("server_id", serverConfig.ServerID),
			exception.WithParam("reason", err.Error()),
			exception.WithCause(err),
		)
	}
	if mcpCards == nil {
		mcpCards = make([]*mcp.McpToolCard, 0)
	}

	for _, card := range mcpCards {
		card.ID = m.GenerateMcpToolID(serverConfig.ServerID, serverConfig.ServerName, card.Name)
		mcpTool, createErr := mcp.NewMCPTool(client, card)
		if createErr != nil {
			logger.Error(logComponent).
				Str("event_type", "TOOL_SERVER_ADD_ERROR").
				Str("tool_id", card.ID).
				Str("tool_name", card.Name).
				Err(createErr).
				Msg("创建 MCPTool 失败")
			continue
		}
		addErr := m.AddTool(card.ID, mcpTool)
		if addErr != nil {
			logger.Warn(logComponent).
				Str("event_type", "TOOL_ADD_DUPLICATE_WARN").
				Str("tool_id", card.ID).
				Msg("工具已存在，跳过添加")
		}
	}

	mcpIDs := make([]string, len(mcpCards))
	for i, card := range mcpCards {
		mcpIDs[i] = card.ID
	}

	m.mu.Lock()
	m.mcpServerResources[serverConfig.ServerID] = &McpServerResource{
		Config:         serverConfig,
		Client:         client,
		ToolIDs:        mcpIDs,
		LastUpdateTime: time.Now(),
		ExpiryTime:     expiryTime,
	}
	m.mu.Unlock()

	return mcpCards, nil
}

// innerRemoveMcpTools 逐个移除工具，忽略错误。
//
// 对应 Python: ToolMgr._inner_remove_mcp_tools(tools)
func (m *ToolMgr) innerRemoveMcpTools(toolIDs []string) {
	if len(toolIDs) == 0 {
		return
	}
	for _, toolID := range toolIDs {
		_, err := m.RemoveTool(toolID)
		if err != nil {
			continue
		}
	}
}

// mcpServerLock 获取或创建 server_id 粒度锁。
//
// 对应 Python: ToolMgr._mcp_server_lock(server_id)
func (m *ToolMgr) mcpServerLock(serverID string) *sync.Mutex {
	m.mu.Lock()
	lock, ok := m.mcpServerLocks[serverID]
	if !ok {
		lock = &sync.Mutex{}
		m.mcpServerLocks[serverID] = lock
	}
	m.mu.Unlock()
	return lock
}

// deepCopyMcpToolCard 深拷贝 McpToolCard。
func deepCopyMcpToolCard(card *mcp.McpToolCard) *mcp.McpToolCard {
	if card == nil {
		return nil
	}
	copied := &mcp.McpToolCard{
		ToolCard:   card.ToolCard,
		ServerName: card.ServerName,
		ServerID:   card.ServerID,
	}
	return copied
}
