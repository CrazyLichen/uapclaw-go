//go:build integration

package tool_call

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeSyncMCPCaller 创建同步 MCP 调用函数。
// 返回的 APIWrapperFunc 内部会：创建 MCP 客户端 → 连接 → 调用工具 → 断开连接。
//
// 对齐 Python: openjiuwen/agent_evolving/optimizer/tool_call/callable_fortest.py (make_sync_mcp_caller)
//
//	Python 使用 SSETransport + Client + asyncio.run，Go 使用已有 MCP 客户端体系
//
// 运行方式: go test -tags=integration ./internal/evolving/optimizer/tool_call/...
func MakeSyncMCPCaller(url, name string) APIWrapperFunc {
	return func(tool map[string]any, toolInput map[string]any) (string, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 对齐 Python: transport = SSETransport(url=url); client = Client(transport)
		mcpConfig := mcptypes.NewMcpServerConfig(name, url, "sse")
		client, err := mcp.NewMcpClient(mcpConfig)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "MakeSyncMCPCaller").
				Str("url", url).
				Str("name", name).
				Err(err).
				Msg("创建 MCP 客户端失败")
			result, _ := json.Marshal(map[string]string{
				"error":    fmt.Sprintf("请求无效，错误: %v", err),
				"response": "",
			})
			return string(result), 12
		}

		// 对齐 Python: async with client:
		if err := client.Connect(ctx); err != nil {
			logger.Error(logComponent).
				Str("method", "MakeSyncMCPCaller").
				Str("url", url).
				Err(err).
				Msg("连接 MCP 服务器失败")
			result, _ := json.Marshal(map[string]string{
				"error":    fmt.Sprintf("请求无效，错误: %v", err),
				"response": "",
			})
			return string(result), 12
		}
		defer client.Disconnect(ctx)

		// 对齐 Python: tool_name = tool_arguments["name"]
		toolName, _ := toolInput["name"].(string)
		if toolName == "" {
			if n, ok := tool["name"]; ok {
				toolName = fmt.Sprintf("%v", n)
			}
		}

		// 对齐 Python: arguments = tool_arguments.get("arguments")
		arguments := make(map[string]any)
		if args, ok := toolInput["arguments"]; ok && args != nil {
			switch v := args.(type) {
			case map[string]any:
				arguments = v
			case string:
				// 对齐 Python: if isinstance(arguments, str): arguments = json.loads(arguments)
				if jsonErr := json.Unmarshal([]byte(v), &arguments); jsonErr != nil {
					logger.Error(logComponent).
						Str("method", "MakeSyncMCPCaller").
						Str("tool_name", toolName).
						Err(jsonErr).
						Msg("将参数字符串解析为 JSON 失败")
					result, _ := json.Marshal(map[string]string{
						"error":    fmt.Sprintf("请求无效，解析参数失败: %v", jsonErr),
						"response": "",
					})
					return string(result), 12
				}
			}
		}

		// 对齐 Python: result = await client.call_tool(tool_name, arguments)
		callResult, err := client.CallTool(ctx, toolName, arguments)
		if err != nil {
			logger.Error(logComponent).
				Str("method", "MakeSyncMCPCaller").
				Str("tool_name", toolName).
				Err(err).
				Msg("调用 MCP 工具失败")
			result, _ := json.Marshal(map[string]string{
				"error":    fmt.Sprintf("请求无效，错误: %v", err),
				"response": "",
			})
			return string(result), 12
		}

		// 对齐 Python: return result.content[0].text → json.dumps({'response': output})
		responseStr := fmt.Sprintf("%v", callResult)
		result, _ := json.Marshal(map[string]any{
			"response": responseStr,
		})
		return string(result), 0
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
