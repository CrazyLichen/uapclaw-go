package mcp

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ListMcpResourcesInput list_mcp_resources 工具的输入参数。
// 对齐 Python: ListMcpResourcesTool.invoke inputs
type ListMcpResourcesInput struct {
	// ServerID MCP 服务器的 server_id
	ServerID string `json:"server_id"`
}

// ReadMcpResourceInput read_mcp_resource 工具的输入参数。
// 对齐 Python: ReadMcpResourceTool.invoke inputs
type ReadMcpResourceInput struct {
	// ServerID MCP 服务器的 server_id
	ServerID string `json:"server_id"`
	// URI 要读取的资源 URI
	URI string `json:"uri"`
}

// ──────────────────────────── 全局变量 ────────────────────────────

// mcpResourcesLogComponent 日志组件标识
var mcpResourcesLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewListMcpResourcesTool 创建列出 MCP 服务器资源工具。
//
// 对齐 Python: ListMcpResourcesTool (openjiuwen/harness/tools/mcp_tools.py L18-39)
func NewListMcpResourcesTool(language string, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("list_mcp_resources", "ListMcpResourcesTool", language, nil, agentID)

	fn := func(ctx context.Context, input ListMcpResourcesInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python L24-25: server_id = inputs.get("server_id"); if not server_id
		if input.ServerID == "" {
			return map[string]any{"success": false, "error": "server_id is required"}, nil
		}

		// 对齐 Python L27-38: resources = await Runner.resource_mgr.list_mcp_resources(server_id)
		resourceMgr := runner.GetResourceMgr()
		if resourceMgr == nil {
			return map[string]any{"success": false, "error": "resource manager not available"}, nil
		}

		resources, err := resourceMgr.ListMcpResources(ctx, input.ServerID)
		if err != nil {
			logger.Warn(mcpResourcesLogComponent).
				Str("tool_name", "list_mcp_resources").
				Str("server_id", input.ServerID).
				Err(err).
				Msg("ListMcpResourcesTool 列出资源失败")
			return map[string]any{"success": false, "error": err.Error()}, nil
		}

		// 对齐 Python L28-36: resources 类型为 []map[string]any，直接透传给 LLM。

		logger.Info(mcpResourcesLogComponent).
			Str("tool_name", "list_mcp_resources").
			Str("server_id", input.ServerID).
			Int("resource_count", len(resources)).
			Msg("ListMcpResourcesTool 列出资源完成")

		return map[string]any{"success": true, "data": resources}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}

// NewReadMcpResourceTool 创建读取 MCP 服务器资源工具。
//
// 对齐 Python: ReadMcpResourceTool (openjiuwen/harness/tools/mcp_tools.py L42-67)
func NewReadMcpResourceTool(language string, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("read_mcp_resource", "ReadMcpResourceTool", language, nil, agentID)

	fn := func(ctx context.Context, input ReadMcpResourceInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python L49-50: server_id = inputs.get("server_id"); if not server_id
		if input.ServerID == "" {
			return map[string]any{"success": false, "error": "server_id is required"}, nil
		}
		// 对齐 Python L51-52: uri = inputs.get("uri"); if not uri
		if input.URI == "" {
			return map[string]any{"success": false, "error": "uri is required"}, nil
		}

		// 对齐 Python L54-65: contents = await Runner.resource_mgr.read_mcp_resource(server_id, uri)
		resourceMgr := runner.GetResourceMgr()
		if resourceMgr == nil {
			return map[string]any{"success": false, "error": "resource manager not available"}, nil
		}

		contents, err := resourceMgr.ReadMcpResource(ctx, input.ServerID, input.URI)
		if err != nil {
			logger.Warn(mcpResourcesLogComponent).
				Str("tool_name", "read_mcp_resource").
				Str("server_id", input.ServerID).
				Str("uri", input.URI).
				Err(err).
				Msg("ReadMcpResourceTool 读取资源失败")
			return map[string]any{"success": false, "error": err.Error()}, nil
		}

		// 对齐 Python L55-63: contents 类型为 []map[string]any，直接透传给 LLM。

		logger.Info(mcpResourcesLogComponent).
			Str("tool_name", "read_mcp_resource").
			Str("server_id", input.ServerID).
			Str("uri", input.URI).
			Msg("ReadMcpResourceTool 读取资源完成")

		return map[string]any{"success": true, "data": contents}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}
