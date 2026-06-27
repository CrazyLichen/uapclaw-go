package resources_manager

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockMcpClient 用于测试的 MCP 客户端 mock 实现
type mockMcpClient struct {
	// connected 是否已连接
	connected bool
	// tools 返回的工具列表
	tools []*types.McpToolCard
	// connectErr 连接错误
	connectErr error
	// disconnectErr 断开连接错误
	disconnectErr error
	// listToolsErr 列出工具错误
	listToolsErr error
}

// stubTool 用于测试的 Tool 桩实现
type stubTool struct {
	// card 工具卡片
	card *tool.ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// --- mockMcpClient 实现 types.McpClient ---

func (m *mockMcpClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockMcpClient) Disconnect(_ context.Context) error {
	if m.disconnectErr != nil {
		return m.disconnectErr
	}
	m.connected = false
	return nil
}

func (m *mockMcpClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	if m.listToolsErr != nil {
		return nil, m.listToolsErr
	}
	return m.tools, nil
}

func (m *mockMcpClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return nil, nil
}

func (m *mockMcpClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	return nil, nil
}

func (m *mockMcpClient) ListResources(_ context.Context) ([]any, error) {
	return nil, nil
}

func (m *mockMcpClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}

func (m *mockMcpClient) Close() error {
	return nil
}

// --- stubTool 实现 tool.Tool ---

func (s *stubTool) Card() *tool.ToolCard {
	return s.card
}

func (s *stubTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"result": "ok"}, nil
}

func (s *stubTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// TestToolMgr_AddTool 测试添加工具
func TestToolMgr_AddTool(t *testing.T) {
	mgr := NewToolMgr()
	t1 := &stubTool{card: tool.NewToolCard("test_tool", "测试工具", nil, nil)}
	err := mgr.AddTool("tool1", t1)
	if err != nil {
		t.Fatalf("期望添加工具成功，实际错误: %v", err)
	}
	if !mgr.tools.Contains("tool1") {
		t.Fatal("期望工具已注册")
	}
}

// TestToolMgr_AddTool_重复报错 测试重复添加工具报错
func TestToolMgr_AddTool_重复报错(t *testing.T) {
	mgr := NewToolMgr()
	t1 := &stubTool{card: tool.NewToolCard("test_tool", "测试工具", nil, nil)}
	err := mgr.AddTool("tool1", t1)
	if err != nil {
		t.Fatalf("第一次添加期望成功，实际错误: %v", err)
	}
	err = mgr.AddTool("tool1", t1)
	if err == nil {
		t.Fatal("重复添加期望报错，实际 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("期望 BaseError 类型，实际: %T", err)
	}
	if baseErr.Status() != exception.StatusResourceAddError {
		t.Fatalf("期望状态码 StatusResourceAddError，实际: %v", baseErr.Status())
	}
}

// TestToolMgr_GetTool 测试获取工具
func TestToolMgr_GetTool(t *testing.T) {
	mgr := NewToolMgr()
	t1 := &stubTool{card: tool.NewToolCard("test_tool", "测试工具", nil, nil)}
	err := mgr.AddTool("tool1", t1)
	if err != nil {
		t.Fatalf("添加工具失败: %v", err)
	}
	got, err := mgr.GetTool("tool1", nil)
	if err != nil {
		t.Fatalf("获取工具失败: %v", err)
	}
	if got != t1 {
		t.Fatal("期望返回同一工具实例")
	}
}

// TestToolMgr_GetTool_不存在 测试获取不存在的工具
func TestToolMgr_GetTool_不存在(t *testing.T) {
	mgr := NewToolMgr()
	_, err := mgr.GetTool("nonexistent", nil)
	if err == nil {
		t.Fatal("期望报错，实际 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("期望 BaseError 类型，实际: %T", err)
	}
	if baseErr.Status() != exception.StatusResourceGetError {
		t.Fatalf("期望状态码 StatusResourceGetError，实际: %v", baseErr.Status())
	}
}

// TestToolMgr_RemoveTool 测试移除工具
func TestToolMgr_RemoveTool(t *testing.T) {
	mgr := NewToolMgr()
	t1 := &stubTool{card: tool.NewToolCard("test_tool", "测试工具", nil, nil)}
	err := mgr.AddTool("tool1", t1)
	if err != nil {
		t.Fatalf("添加工具失败: %v", err)
	}
	removed, err := mgr.RemoveTool("tool1")
	if err != nil {
		t.Fatalf("移除工具失败: %v", err)
	}
	if removed != t1 {
		t.Fatal("期望返回被移除的工具实例")
	}
	if mgr.tools.Contains("tool1") {
		t.Fatal("期望工具已不存在")
	}

	// 移除不存在的工具应报错
	_, err = mgr.RemoveTool("tool1")
	if err == nil {
		t.Fatal("移除不存在的工具期望报错")
	}
}

// TestToolMgr_GenerateMcpToolID 测试生成 MCP 工具 ID
func TestToolMgr_GenerateMcpToolID(t *testing.T) {
	mgr := NewToolMgr()
	id := mgr.GenerateMcpToolID("srv123", "my_server", "search")
	expected := "srv123.my_server.search"
	if id != expected {
		t.Fatalf("期望 %s，实际 %s", expected, id)
	}
}

// TestToolMgr_AddSysOperationTools 测试注册系统操作关联工具
func TestToolMgr_AddSysOperationTools(t *testing.T) {
	mgr := NewToolMgr()
	mgr.AddSysOperationTools("sysop1", []string{"tool1", "tool2"})
	ids := mgr.GetSysOperationToolIDs("sysop1")
	if len(ids) != 2 {
		t.Fatalf("期望 2 个工具 ID，实际 %d", len(ids))
	}
	if ids[0] != "tool1" || ids[1] != "tool2" {
		t.Fatalf("期望 [tool1, tool2]，实际 %v", ids)
	}

	// 空 toolIDs 不注册
	mgr.AddSysOperationTools("sysop2", []string{})
	ids = mgr.GetSysOperationToolIDs("sysop2")
	if len(ids) != 0 {
		t.Fatalf("空 toolIDs 期望 0 个工具，实际 %d", len(ids))
	}
}

// TestToolMgr_RemoveSysOperationTools 测试注销系统操作关联工具
func TestToolMgr_RemoveSysOperationTools(t *testing.T) {
	mgr := NewToolMgr()
	mgr.AddSysOperationTools("sysop1", []string{"tool1", "tool2"})
	removed := mgr.RemoveSysOperationTools("sysop1")
	if len(removed) != 2 {
		t.Fatalf("期望 2 个工具 ID，实际 %d", len(removed))
	}

	// 注销后应为空
	ids := mgr.GetSysOperationToolIDs("sysop1")
	if len(ids) != 0 {
		t.Fatalf("注销后期望 0 个工具，实际 %d", len(ids))
	}

	// 注销不存在的系统操作应返回空列表
	removed = mgr.RemoveSysOperationTools("nonexistent")
	if len(removed) != 0 {
		t.Fatalf("注销不存在的系统操作期望返回空列表，实际 %d", len(removed))
	}
}

// TestToolMgr_GetSysOperationToolIDs 测试获取系统操作关联工具 ID
func TestToolMgr_GetSysOperationToolIDs(t *testing.T) {
	mgr := NewToolMgr()
	// 不存在的系统操作
	ids := mgr.GetSysOperationToolIDs("nonexistent")
	if len(ids) != 0 {
		t.Fatalf("不存在的系统操作期望返回空列表，实际 %d", len(ids))
	}

	mgr.AddSysOperationTools("sysop1", []string{"tool1"})
	ids = mgr.GetSysOperationToolIDs("sysop1")
	if len(ids) != 1 || ids[0] != "tool1" {
		t.Fatalf("期望 [tool1]，实际 %v", ids)
	}
}

// TestToolMgr_Release 测试释放空 ToolMgr 不 panic
func TestToolMgr_Release(t *testing.T) {
	mgr := NewToolMgr()
	err := mgr.Release(context.Background())
	if err != nil {
		t.Fatalf("释放空 ToolMgr 期望 nil，实际: %v", err)
	}
}

// TestToolMgr_GetMcpToolID 测试获取 MCP 工具 ID
func TestToolMgr_GetMcpToolID(t *testing.T) {
	mgr := NewToolMgr()
	// 不存在的服务器
	result := mgr.GetMcpToolID("nonexistent", "tool1")
	if result != nil {
		t.Fatalf("不存在的服务器期望 nil，实际 %v", result)
	}

	// 添加 MCP 服务器后测试
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 手动设置 mcpServerResources 来测试 GetMcpToolID
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config:  serverConfig,
		Client:  mockClient,
		ToolIDs: []string{"srv1.test_server.search"},
	}
	mgr.mu.Unlock()

	// toolName 为空时返回全部 toolIDs
	result = mgr.GetMcpToolID("srv1", "")
	if len(result) != 1 || result[0] != "srv1.test_server.search" {
		t.Fatalf("期望 [srv1.test_server.search]，实际 %v", result)
	}

	// 指定 toolName
	result = mgr.GetMcpToolID("srv1", "search")
	if len(result) != 1 || result[0] != "srv1.test_server.search" {
		t.Fatalf("期望 [srv1.test_server.search]，实际 %v", result)
	}
}

// TestToolMgr_GetMcpServerIDs 测试按名称获取服务器 ID 列表
func TestToolMgr_GetMcpServerIDs(t *testing.T) {
	mgr := NewToolMgr()
	// 不存在的服务器名称
	ids := mgr.GetMcpServerIDs("nonexistent")
	if len(ids) != 0 {
		t.Fatalf("不存在的服务器名称期望空列表，实际 %d", len(ids))
	}

	mgr.mu.Lock()
	mgr.mcpServerNameToIDs["test_server"] = []string{"srv1", "srv2"}
	mgr.mu.Unlock()

	ids = mgr.GetMcpServerIDs("test_server")
	if len(ids) != 2 {
		t.Fatalf("期望 2 个服务器 ID，实际 %d", len(ids))
	}
}

// TestToolMgr_GetMcpClient 测试获取 MCP 客户端
func TestToolMgr_GetMcpClient(t *testing.T) {
	mgr := NewToolMgr()
	// 不存在的服务器
	_, err := mgr.GetMcpClient("nonexistent")
	if err == nil {
		t.Fatal("不存在的服务器期望报错")
	}

	mockClient := &mockMcpClient{}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config: serverConfig,
		Client: mockClient,
	}
	mgr.mu.Unlock()

	client, err := mgr.GetMcpClient("srv1")
	if err != nil {
		t.Fatalf("获取 MCP 客户端失败: %v", err)
	}
	if client != mockClient {
		t.Fatal("期望返回同一客户端实例")
	}
}

// TestToolMgr_GetMcpServerConfig 测试获取 MCP 服务器配置（深拷贝）
func TestToolMgr_GetMcpServerConfig(t *testing.T) {
	mgr := NewToolMgr()
	// 不存在的服务器
	_, err := mgr.GetMcpServerConfig("nonexistent")
	if err == nil {
		t.Fatal("不存在的服务器期望报错")
	}

	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config: serverConfig,
	}
	mgr.mu.Unlock()

	config, err := mgr.GetMcpServerConfig("srv1")
	if err != nil {
		t.Fatalf("获取 MCP 服务器配置失败: %v", err)
	}
	if config.ServerID != "srv1" {
		t.Fatalf("期望 ServerID=srv1，实际 %s", config.ServerID)
	}
	if config.ServerName != "test_server" {
		t.Fatalf("期望 ServerName=test_server，实际 %s", config.ServerName)
	}
	// 深拷贝验证：修改返回值不影响原始
	config.ServerName = "modified"
	mgr.mu.RLock()
	original := mgr.mcpServerResources["srv1"].Config.ServerName
	mgr.mu.RUnlock()
	if original != "test_server" {
		t.Fatalf("深拷贝失败：原始 ServerName 被修改为 %s", original)
	}
}

// TestToolMgr_GetMcpToolIDs 测试获取服务器下所有工具 ID
func TestToolMgr_GetMcpToolIDs(t *testing.T) {
	mgr := NewToolMgr()
	// 不存在的服务器
	ids := mgr.GetMcpToolIDs("nonexistent")
	if len(ids) != 0 {
		t.Fatalf("不存在的服务器期望空列表，实际 %d", len(ids))
	}

	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config:  serverConfig,
		ToolIDs: []string{"tool1", "tool2"},
	}
	mgr.mu.Unlock()

	ids = mgr.GetMcpToolIDs("srv1")
	if len(ids) != 2 {
		t.Fatalf("期望 2 个工具 ID，实际 %d", len(ids))
	}
	if ids[0] != "tool1" || ids[1] != "tool2" {
		t.Fatalf("期望 [tool1, tool2]，实际 %v", ids)
	}
}

// TestToolMgr_AddToolServer_使用Mock 测试使用 mock 客户端添加 MCP 工具服务器
func TestToolMgr_AddToolServer_使用Mock(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
			mcp.NewMcpToolCard("calc", "计算工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 覆盖 createClient 方法：通过手动构建模拟添加过程
	// 由于 createClient 是非导出方法，我们通过直接调用 innerRefreshMcpTools 来测试
	err := mockClient.Connect(context.Background())
	if err != nil {
		t.Fatalf("mock 客户端连接失败: %v", err)
	}

	cards, err := mgr.innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)
	if err != nil {
		t.Fatalf("刷新 MCP 工具失败: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("期望 2 个工具卡片，实际 %d", len(cards))
	}

	// 验证工具已注册
	expectedID1 := mgr.GenerateMcpToolID("srv1", "test_server", "search")
	expectedID2 := mgr.GenerateMcpToolID("srv1", "test_server", "calc")
	if !mgr.tools.Contains(expectedID1) {
		t.Fatalf("工具 %s 未注册", expectedID1)
	}
	if !mgr.tools.Contains(expectedID2) {
		t.Fatalf("工具 %s 未注册", expectedID2)
	}

	// 验证 mcpServerResources 已更新
	ids := mgr.GetMcpToolIDs("srv1")
	if len(ids) != 2 {
		t.Fatalf("期望 2 个工具 ID，实际 %d", len(ids))
	}
}

// TestToolMgr_RemoveToolServer_使用Mock 测试使用 mock 客户端移除 MCP 工具服务器
func TestToolMgr_RemoveToolServer_使用Mock(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 先添加
	_ = mockClient.Connect(context.Background())
	_, _ = mgr.innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)
	mgr.mu.Lock()
	mgr.mcpServerNameToIDs["test_server"] = []string{"srv1"}
	mgr.mu.Unlock()

	// 移除
	removedIDs, err := mgr.RemoveToolServer(context.Background(), "srv1", true)
	if err != nil {
		t.Fatalf("移除工具服务器失败: %v", err)
	}
	if len(removedIDs) != 1 {
		t.Fatalf("期望 1 个被移除的工具 ID，实际 %d", len(removedIDs))
	}

	// 验证工具已移除
	expectedID := mgr.GenerateMcpToolID("srv1", "test_server", "search")
	if mgr.tools.Contains(expectedID) {
		t.Fatal("工具应已移除")
	}

	// 移除不存在的服务器（ignoreNotExist=true）
	removedIDs, err = mgr.RemoveToolServer(context.Background(), "nonexistent", true)
	if err != nil {
		t.Fatalf("ignoreNotExist=true 期望不报错，实际: %v", err)
	}
	if len(removedIDs) != 0 {
		t.Fatalf("期望 0 个被移除的工具 ID，实际 %d", len(removedIDs))
	}

	// 移除不存在的服务器（ignoreNotExist=false）
	_, err = mgr.RemoveToolServer(context.Background(), "nonexistent", false)
	if err == nil {
		t.Fatal("ignoreNotExist=false 期望报错")
	}
}

// TestToolMgr_RefreshToolServer_使用Mock 测试使用 mock 客户端刷新 MCP 工具服务器
func TestToolMgr_RefreshToolServer_使用Mock(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 手动设置资源
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config:         serverConfig,
		Client:         mockClient,
		ToolIDs:        []string{"srv1.test_server.search"},
		LastUpdateTime: time.Now(),
		ExpiryTime:     nil,
	}
	mgr.mu.Unlock()

	// 未过期且非强制刷新 → 返回空
	cards, err := mgr.RefreshToolServer(context.Background(), "srv1", false, false)
	if err != nil {
		t.Fatalf("刷新工具服务器失败: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("未过期且非强制刷新期望 0 个卡片，实际 %d", len(cards))
	}

	// 强制刷新 → 返回工具卡片
	cards, err = mgr.RefreshToolServer(context.Background(), "srv1", false, true)
	if err != nil {
		t.Fatalf("强制刷新失败: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("强制刷新期望 1 个卡片，实际 %d", len(cards))
	}

	// 不存在的服务器，skipNotExist=true
	cards, err = mgr.RefreshToolServer(context.Background(), "nonexistent", true, false)
	if err != nil {
		t.Fatalf("skipNotExist=true 期望不报错，实际: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("期望 0 个卡片，实际 %d", len(cards))
	}

	// 不存在的服务器，skipNotExist=false
	_, err = mgr.RefreshToolServer(context.Background(), "nonexistent", false, false)
	if err == nil {
		t.Fatal("skipNotExist=false 期望报错")
	}
}

// TestToolMgr_RefreshToolServer_过期刷新 测试过期后自动刷新
func TestToolMgr_RefreshToolServer_过期刷新(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 设置已过期的资源
	expiry := 0.001 // 1ms 过期
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config:         serverConfig,
		Client:         mockClient,
		ToolIDs:        []string{"srv1.test_server.search"},
		LastUpdateTime: time.Now().Add(-time.Second), // 1 秒前更新
		ExpiryTime:     &expiry,
	}
	mgr.mu.Unlock()

	// 过期后非强制刷新 → 自动刷新
	cards, err := mgr.RefreshToolServer(context.Background(), "srv1", false, false)
	if err != nil {
		t.Fatalf("过期刷新失败: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("过期刷新期望 1 个卡片，实际 %d", len(cards))
	}
}

// TestToolMgr_Release_有连接 测试释放有连接的 ToolMgr
func TestToolMgr_Release_有连接(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{connected: true}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))
	mgr.mu.Lock()
	mgr.mcpServerResources["srv1"] = &McpServerResource{
		Config: serverConfig,
		Client: mockClient,
	}
	mgr.mu.Unlock()

	err := mgr.Release(context.Background())
	if err != nil {
		t.Fatalf("释放期望 nil，实际: %v", err)
	}
	if mockClient.connected {
		t.Fatal("期望客户端已断开连接")
	}
}

// TestToolMgr_AddToolServer_重复添加 测试重复添加同一服务器返回缓存卡片
func TestToolMgr_AddToolServer_重复添加(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		connected: true,
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 先通过 innerRefreshMcpTools 注册工具和资源
	cards, _ := mgr.innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)
	_ = cards

	// 再次调用 AddToolServer 应返回缓存卡片（而非报错）
	cards2, err := mgr.AddToolServer(context.Background(), serverConfig, nil)
	if err != nil {
		t.Fatalf("重复添加期望返回缓存卡片，实际报错: %v", err)
	}
	if len(cards2) != 1 {
		t.Fatalf("期望 1 个缓存卡片，实际 %d", len(cards2))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestToolMgr_GetMcpTool_使用Mock 测试获取 MCP 工具
func TestToolMgr_GetMcpTool_使用Mock(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 先注册工具和服务器资源
	_ = mockClient.Connect(context.Background())
	_, _ = mgr.innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)

	// 获取 MCP 工具
	tl, err := mgr.GetMcpTool(context.Background(), "search", "srv1", nil)
	if err != nil {
		t.Fatalf("GetMcpTool 失败: %v", err)
	}
	if tl == nil {
		t.Fatal("期望非 nil 工具")
	}
}

// TestToolMgr_GetMcpTool_不存在 测试获取不存在的 MCP 工具
func TestToolMgr_GetMcpTool_不存在(t *testing.T) {
	mgr := NewToolMgr()

	// 服务器不存在
	_, err := mgr.GetMcpTool(context.Background(), "search", "nonexistent", nil)
	if err == nil {
		t.Fatal("服务器不存在期望报错")
	}
}

// TestToolMgr_GetMcpTools_使用Mock 测试获取指定服务器下所有工具
func TestToolMgr_GetMcpTools_使用Mock(t *testing.T) {
	mgr := NewToolMgr()
	mockClient := &mockMcpClient{
		tools: []*types.McpToolCard{
			mcp.NewMcpToolCard("search", "搜索工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
			mcp.NewMcpToolCard("calc", "计算工具", "test_server", nil, mcp.WithMcpToolCardServerID("srv1")),
		},
	}
	serverConfig := mcp.NewMcpServerConfig("test_server", "http://localhost:8080/sse", "sse", mcp.WithServerID("srv1"))

	// 先注册工具和服务器资源
	mockClient.Connect(context.Background())
	mgr.innerRefreshMcpTools(context.Background(), mockClient, serverConfig, nil)

	// 获取所有工具
	tools, err := mgr.GetMcpTools(context.Background(), "srv1", nil)
	if err != nil {
		t.Fatalf("GetMcpTools 失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("期望 2 个工具，实际 %d", len(tools))
	}
}

// TestToolMgr_GetMcpTools_不存在 测试获取不存在服务器的所有工具
func TestToolMgr_GetMcpTools_不存在(t *testing.T) {
	mgr := NewToolMgr()

	// 服务器不存在应返回空切片而非报错
	tools, err := mgr.GetMcpTools(context.Background(), "nonexistent", nil)
	if err != nil {
		t.Fatalf("服务器不存在期望 nil 错误，实际: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("期望 0 个工具，实际 %d", len(tools))
	}
}
