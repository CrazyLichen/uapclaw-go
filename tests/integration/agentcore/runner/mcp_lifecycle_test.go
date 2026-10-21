//go:build integration

package runner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpLifecycleSuite MCP 工具生命周期集成测试。
//
// 对齐 Python: tests/system_tests/runner/test_runner.py 的
// test_mcp_tools_sse / test_mcp_tools_stdio / test_mcp_tools_playwright。
// 端到端验证：连接 → 拉取工具 → 调用工具 → 移除服务器。
//
// Python 原测试通过 unittest.mock.patch 类级替换 SseClient/StdioClient/PlaywrightClient
// 四个公共方法（connect/disconnect/list_tools/call_tool）；Go 无类级 patch 机制，
// 改用 ResourceMgr.SetMcpClientFactory 工厂注入 mock 客户端实现等价语义。
//
// 注：Python 原测试使用的 add_tool_servers / get_mcp_tool_infos(tool_server_name=...)
// 为过时 API（当前 Python 源码签名中不存在，原测试已被 @unittest.skip 标记），
// 此处对齐测试意图而非逐字复刻调用，统一走当前正确 API：
// AddMcpServer → GetMcpToolIDs/GetMcpToolInfos → GetTool → Invoke → RemoveMcpServer。
type McpLifecycleSuite struct {
	isuite.RunnerSuite
	// serverIDs 本测试添加的 MCP 服务器 ID，TearDownTest 统一清理
	serverIDs []string
}

// mcpLifecycleMockClient MCP 客户端 mock，记录四个公共方法的调用情况。
// 对齐 Python: AsyncMock 替换 connect/disconnect/list_tools/call_tool + assert_awaited_once_with。
//
// 注意与 Python 的返回值差异：Python 的 call_tool mock 返回纯字符串（提取在 client 层完成），
// Go 的 ExtractMCPToolResultContent 在 MCPTool.Invoke 内执行，
// 因此 CallTool 必须返回 MCP CallToolResult 的 map 形态（content 列表）才能提取出文本。
type mcpLifecycleMockClient struct {
	// tools ListTools 返回的工具卡片
	tools []*mcptypes.McpToolCard
	// toolResult CallTool 应返回的工具结果文本
	toolResult string
	// connected 是否已连接
	connected bool
	// connectCalls Connect 被调用次数
	connectCalls int
	// disconnectCalls Disconnect 被调用次数
	disconnectCalls int
	// listToolsCalls ListTools 被调用次数
	listToolsCalls int
	// callToolRecords CallTool 每次调用的参数记录
	callToolRecords []mcpCallToolRecord
}

// mcpCallToolRecord CallTool 调用参数记录。
type mcpCallToolRecord struct {
	// toolName 工具名
	toolName string
	// arguments 调用参数
	arguments map[string]any
}

// mcpLifecycleCase 单个生命周期测试用例配置。
type mcpLifecycleCase struct {
	// serverName MCP 服务器名称
	serverName string
	// serverID 固定的服务器 ID（保证清理确定性）
	serverID string
	// serverPath 服务器路径占位（不会发起真实网络连接）
	serverPath string
	// clientType 客户端类型：sse / stdio / playwright
	clientType string
	// params 传输层参数（stdio 用，值不会被真正使用）
	params map[string]any
	// tools mock 的工具卡片列表
	tools []*mcptypes.McpToolCard
	// toolResult mock 的工具执行结果
	toolResult string
	// invokeToolName 测试调用的工具名
	invokeToolName string
	// invokeInputs 测试调用的参数
	invokeInputs map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMcpLifecycleSuite 入口。
func TestMcpLifecycleSuite(t *testing.T) {
	suite.Run(t, new(McpLifecycleSuite))
}

// TestMcpLifecycle_SSE 端到端测试 MCP-SSE 工具生命周期。
// 对齐 Python: test_mcp_tools_sse —— mock SseClient 四个公共方法。
func (s *McpLifecycleSuite) TestMcpLifecycle_SSE() {
	s.runMcpLifecycle(mcpLifecycleCase{
		serverName: "browser-use-server",
		serverID:   "itest_mcp_sse_srv",
		serverPath: "http://127.0.0.1:8930/sse",
		clientType: "sse",
		tools: []*mcptypes.McpToolCard{
			mcp.NewMcpToolCard("browser_navigate", "Navigate to a URL", "my_server", []*schema.Param{
				{Name: "url", Description: "The URL to navigate to", Type: schema.ParamTypeString, Required: true},
			}),
			mcp.NewMcpToolCard("browser_extract_text", "Extract text from the current page", "my_server", []*schema.Param{
				{Name: "selector", Description: "CSS selector for the element", Type: schema.ParamTypeString, Required: true},
			}),
		},
		toolResult:     "Successfully navigated to example.com and extracted title: Example Domain",
		invokeToolName: "browser_navigate",
		invokeInputs:   map[string]any{"url": "https://example.com"},
	})
}

// TestMcpLifecycle_Stdio 端到端测试 MCP-stdio 工具生命周期。
// 对齐 Python: test_mcp_tools_stdio —— mock StdioClient 四个公共方法。
// 参数内容可以是任意占位符，真实值不会被用到。
func (s *McpLifecycleSuite) TestMcpLifecycle_Stdio() {
	s.runMcpLifecycle(mcpLifecycleCase{
		serverName: "doubter-mcp-server",
		serverID:   "itest_mcp_stdio_srv",
		serverPath: "",
		clientType: "stdio",
		params: map[string]any{
			"command": "python",
			"args":    []any{"dummy.py"},
		},
		tools: []*mcptypes.McpToolCard{
			mcp.NewMcpToolCard("doubter", "Doubter tool via stdio", "my_server", []*schema.Param{
				{Name: "history", Description: "Agent action history", Type: schema.ParamTypeString, Required: true},
			}),
			mcp.NewMcpToolCard("checker", "Checker tool via stdio", "my_server", []*schema.Param{
				{Name: "url", Description: "URL to check", Type: schema.ParamTypeString, Required: true},
			}),
		},
		toolResult:     "score: 0.85, decision: ACCEPT, review: actions verified",
		invokeToolName: "doubter",
		invokeInputs:   map[string]any{"history": "single_agent navigated to example.com and extracted title"},
	})
}

// TestMcpLifecycle_Playwright 端到端测试 MCP-Playwright 工具生命周期。
// 对齐 Python: test_mcp_tools_playwright —— mock PlaywrightClient 四个公共方法。
// serverPath 可以是 URL 占位，实际不会发起网络。
func (s *McpLifecycleSuite) TestMcpLifecycle_Playwright() {
	s.runMcpLifecycle(mcpLifecycleCase{
		serverName: "playwright-mcp-server",
		serverID:   "itest_mcp_pw_srv",
		serverPath: "http://127.0.0.1:8931/sse",
		clientType: "playwright",
		tools: []*mcptypes.McpToolCard{
			mcp.NewMcpToolCard("browser_navigate", "Navigate to a URL via Playwright", "my_server", []*schema.Param{
				{Name: "url", Description: "The URL to navigate to", Type: schema.ParamTypeString, Required: true},
			}),
			mcp.NewMcpToolCard("browser_click", "Click an element via Playwright", "my_server", []*schema.Param{
				{Name: "selector", Description: "CSS selector", Type: schema.ParamTypeString, Required: true},
			}),
		},
		toolResult:     "Navigated to https://example.com and clicked button",
		invokeToolName: "browser_navigate",
		invokeInputs:   map[string]any{"url": "https://example.com"},
	})
}

// TearDownTest 每个测试后清理全局 ResourceMgr 状态：
// 移除本测试添加的 MCP 服务器（含工具/标签缓存）并恢复默认客户端工厂。
// 对齐 resourcemgr-suite-isolation 先例，避免 suite 内测试间状态污染。
func (s *McpLifecycleSuite) TearDownTest() {
	if rm := s.GetResourceMgr(); rm != nil {
		for _, id := range s.serverIDs {
			_, _ = rm.RemoveMcpServer(s.Ctx, id, resources_manager.WithMcpSkipIfNotExists())
		}
		// 恢复默认客户端工厂（nil → createClient 走 mcp.NewMcpClient）
		rm.SetMcpClientFactory(nil)
	}
	s.serverIDs = nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runMcpLifecycle 执行单个 MCP 生命周期用例：
// 注入工厂 → AddMcpServer（连接+拉取）→ 列工具 → 取 ToolInfo → Invoke（断言参数）→ RemoveMcpServer。
func (s *McpLifecycleSuite) runMcpLifecycle(c mcpLifecycleCase) {
	rm := s.GetResourceMgr()
	s.Require().NotNil(rm)

	mockClient := &mcpLifecycleMockClient{tools: c.tools, toolResult: c.toolResult}

	// 注入 mock 客户端工厂（对齐 Python patch(SseClient/StdioClient/PlaywrightClient) 类级替换）
	rm.SetMcpClientFactory(func(_ *mcptypes.McpServerConfig) (mcptypes.McpClient, error) {
		return mockClient, nil
	})
	s.serverIDs = append(s.serverIDs, c.serverID)

	serverConfig := mcp.NewMcpServerConfig(c.serverName, c.serverPath, c.clientType,
		mcptypes.WithServerID(c.serverID), mcptypes.WithParams(c.params))

	// -------------------- 添加到管理器 --------------------
	// 对齐 Python: ok_list = await Runner.resource_mgr.add_tool_servers([mcp_server_config])
	cards, err := rm.AddMcpServer(s.Ctx, serverConfig)
	s.Require().NoError(err)
	s.Len(cards, len(c.tools))
	s.Equal(1, mockClient.connectCalls, "AddMcpServer 应恰好调用一次 Connect")
	s.Equal(1, mockClient.listToolsCalls, "AddMcpServer 应恰好调用一次 ListTools")
	s.True(mockClient.connected)

	// 工具 ID 为三段式 {serverID}.{serverName}.{toolName}
	// 对齐 Python: generate_mcp_tool_id(server_id, server_name, tool_name)
	expectedID := c.serverID + "." + c.serverName + "." + c.tools[0].Name
	s.Equal(expectedID, cards[0].ID)

	// -------------------- 工具列表校验 --------------------
	// 对齐 Python: server_tools = get_tool_infos(tool_server_name=...)（过时 API，改用当前等价）
	toolIDs := rm.GetMcpToolIDs(c.serverID)
	s.Require().Len(toolIDs, len(c.tools))
	s.Contains(toolIDs, expectedID)

	// -------------------- 工具信息校验 --------------------
	// 对齐 Python: tools = get_mcp_tool_infos(server_name=...)（Go 当前签名：按工具名+serverID）
	infos, err := rm.GetMcpToolInfos(s.Ctx, c.invokeToolName, c.serverID)
	s.Require().NoError(err)
	s.Require().Len(infos, 1)
	s.Equal(c.invokeToolName, infos[0].GetName())

	// -------------------- 调用工具 --------------------
	tools, err := rm.GetTool([]string{expectedID})
	s.Require().NoError(err)
	s.Require().Len(tools, 1)
	result, err := tools[0].Invoke(s.Ctx, c.invokeInputs)
	s.Require().NoError(err)

	// -------------------- 实例级调用断言 --------------------
	// 对齐 Python: mock_call_tool.assert_awaited_once_with(tool_name=..., arguments=...)
	s.Require().Len(mockClient.callToolRecords, 1, "CallTool 应恰好被调用一次")
	s.Equal(c.invokeToolName, mockClient.callToolRecords[0].toolName)
	s.Equal(c.invokeInputs, mockClient.callToolRecords[0].arguments)

	// -------------------- 结果校验 --------------------
	s.Require().NotNil(result)
	s.Contains(result, "result")
	s.Equal(c.toolResult, result["result"])

	// -------------------- 移除服务器 --------------------
	// 对齐 Python: await Runner.resource_mgr.remove_mcp_server(...)
	removed, err := rm.RemoveMcpServer(s.Ctx, c.serverID)
	s.Require().NoError(err)
	s.Equal([]string{c.serverID}, removed)
	s.Equal(1, mockClient.disconnectCalls, "RemoveMcpServer 应恰好调用一次 Disconnect")
	s.False(mockClient.connected)

	// 移除后工具清空（对齐 Python: empty_tools == None）
	s.Empty(rm.GetMcpToolIDs(c.serverID))
	_, err = rm.GetMcpToolInfos(s.Ctx, c.invokeToolName, c.serverID)
	s.Error(err, "移除服务器后获取工具信息应报错")
}

// --- mcpLifecycleMockClient 实现 mcptypes.McpClient ---

// Connect 记录调用并标记已连接。
func (c *mcpLifecycleMockClient) Connect(_ context.Context, _ ...mcptypes.ConnectOption) error {
	c.connectCalls++
	c.connected = true
	return nil
}

// Disconnect 记录调用并标记已断开。
func (c *mcpLifecycleMockClient) Disconnect(_ context.Context) error {
	c.disconnectCalls++
	c.connected = false
	return nil
}

// ListTools 记录调用并返回预置工具列表。
func (c *mcpLifecycleMockClient) ListTools(_ context.Context) ([]*mcptypes.McpToolCard, error) {
	c.listToolsCalls++
	return c.tools, nil
}

// CallTool 记录调用参数，返回 MCP CallToolResult 的 map 形态
// （content 列表 + text 项），供 MCPTool.Invoke 内的 ExtractMCPToolResultContent 提取文本。
// Python 原测试 mock 直接返回字符串是因为其提取逻辑在 client 层完成，Go 移植到了 Invoke 层。
func (c *mcpLifecycleMockClient) CallTool(_ context.Context, name string, arguments map[string]any) (any, error) {
	argsCopy := make(map[string]any, len(arguments))
	for k, v := range arguments {
		argsCopy[k] = v
	}
	c.callToolRecords = append(c.callToolRecords, mcpCallToolRecord{toolName: name, arguments: argsCopy})
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": c.toolResult},
		},
	}, nil
}

// GetToolInfo 未用到的接口方法，返回零值。
func (c *mcpLifecycleMockClient) GetToolInfo(_ context.Context, _ string) (*mcptypes.McpToolCard, error) {
	return nil, nil
}

// ListResources 未用到的接口方法，返回零值。
func (c *mcpLifecycleMockClient) ListResources(_ context.Context) ([]map[string]any, error) {
	return nil, nil
}

// ReadResource 未用到的接口方法，返回零值。
func (c *mcpLifecycleMockClient) ReadResource(_ context.Context, _ string) ([]map[string]any, error) {
	return nil, nil
}

// Close 关闭客户端。
func (c *mcpLifecycleMockClient) Close() error {
	return nil
}
