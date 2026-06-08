package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	runnnercallback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MCPTool MCP 协议工具，通过 McpClient 调用远程 MCP 服务器工具。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/base.py (MCPTool)
type MCPTool struct {
	card      *McpToolCard
	mcpClient McpClient
}

// ──────────────────────────── 枚举 ────────────────────────────
//
// 以下类型定义在 mcp/types 子包中，此处通过类型别名重导出，
// 保持 mcp 包的公共 API 向后兼容。

// McpServerConfig MCP 服务器配置。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/base.py (McpServerConfig)
type McpServerConfig = types.McpServerConfig

// McpToolCard MCP 工具配置卡片，扩展 ToolCard 增加服务器标识。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/base.py (McpToolCard)
type McpToolCard = types.McpToolCard

// McpClient MCP 客户端接口，定义与 MCP 服务器交互的标准方法。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/mcp_client.py (McpClient)
type McpClient = types.McpClient

// ConnectOption 连接选项函数。
type ConnectOption = types.ConnectOption

// ConnectOptions 连接选项。
type ConnectOptions = types.ConnectOptions

// McpServerConfigOption 配置选项函数。
type McpServerConfigOption = types.McpServerConfigOption

// McpToolCardOption MCP 工具卡片选项函数。
type McpToolCardOption = types.McpToolCardOption

// ──────────────────────────── 常量 ────────────────────────────

// 重导出常量
const (
	// NoTimeout 不设超时，与 Python NO_TIMEOUT = -1 对齐。
	NoTimeout = types.NoTimeout
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 重导出函数
var (
	// WithRetryTimes 设置重试次数。
	WithRetryTimes = types.WithRetryTimes
	// WithConnectTimeout 设置连接超时时间（秒）。
	WithConnectTimeout = types.WithConnectTimeout
	// NewConnectOptions 从选项列表构造 ConnectOptions。
	NewConnectOptions = types.NewConnectOptions
	// WithServerID 设置服务器标识。
	WithServerID = types.WithServerID
	// WithParams 设置传输层参数。
	WithParams = types.WithParams
	// WithAuthHeaders 设置认证请求头。
	WithAuthHeaders = types.WithAuthHeaders
	// WithAuthQueryParams 设置认证查询参数。
	WithAuthQueryParams = types.WithAuthQueryParams
	// NewMcpServerConfig 创建 MCP 服务器配置。
	NewMcpServerConfig = types.NewMcpServerConfig
	// WithMcpToolCardServerID 设置 MCP 工具卡片的服务器标识。
	WithMcpToolCardServerID = types.WithMcpToolCardServerID
	// NewMcpToolCard 创建 MCP 工具卡片。
	NewMcpToolCard = types.NewMcpToolCard
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractMCPToolResultContent 从 MCP CallToolResult 提取紧凑结果值。
//
// 解析逻辑（符合 MCP 协议）：
//   - 提取 content 最后一个元素
//   - 如果有 text 字段 → 返回 text 字符串
//   - 如果有 data 字段 + mimeType 以 image/ 开头 → 返回 "[image content: {mime}, {len} base64 chars]"
//   - 如果有 data 字段 → 返回 data
//   - 其他 → 返回字符串化结果
//
// 对应 Python: extract_mcp_tool_result_content()
func ExtractMCPToolResultContent(toolResult any) any {
	resultMap, ok := toolResult.(map[string]any)
	if !ok {
		return nil
	}
	content, ok := resultMap["content"]
	if !ok {
		return nil
	}
	contentList, ok := content.([]any)
	if !ok || len(contentList) == 0 {
		return nil
	}

	item, ok := contentList[len(contentList)-1].(map[string]any)
	if !ok {
		return fmt.Sprintf("%v", contentList[len(contentList)-1])
	}

	// 优先检查 text 字段
	if text, ok := item["text"]; ok {
		if s, ok := text.(string); ok {
			return s
		}
	}

	// 检查 data 字段
	if data, ok := item["data"]; ok {
		// 检查是否为图片
		mimeType, _ := item["mimeType"].(string)
		if strings.HasPrefix(mimeType, "image/") {
			return fmt.Sprintf("[image content: %s, %d base64 chars]", mimeType, len(fmt.Sprintf("%v", data)))
		}
		return data
	}

	// 其他情况，尝试字符串化
	return fmt.Sprintf("%v", item)
}

// NewMCPTool 创建 MCP 工具实例。
// mcpClient 为 nil 时返回 StatusToolMcpClientNotSupported 错误。
//
// 对应 Python: MCPTool.__init__(mcp_client, tool_info)
func NewMCPTool(mcpClient McpClient, card *McpToolCard) (*MCPTool, error) {
	if mcpClient == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpClientNotSupported,
			exception.WithParam("card", card.String()),
		)
	}
	return &MCPTool{card: card, mcpClient: mcpClient}, nil
}

// Card 返回工具配置卡片。
func (t *MCPTool) Card() *tool.ToolCard {
	return &t.card.ToolCard
}

// Invoke 调用 MCP 远程工具。
//
// 流程：
//  1. 如果 card.InputParams 不为 nil，用 SchemaUtils 格式化参数
//  2. mcpClient.CallTool 调用远程工具
//  3. ExtractMCPToolResultContent 提取紧凑结果
//  4. 返回 {"result": extracted}
//
// 对应 Python: MCPTool.invoke()
func (t *MCPTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	arguments := inputs
	if t.card.InputParams != nil {
		callOpts := tool.NewToolCallOptions(opts...)
		// 触发 TOOL_PARSE_STARTED 事件
		runnnercallback.GetCallbackFramework().TriggerTool(ctx, &runnnercallback.ToolCallEventData{
			Event:    runnnercallback.ToolParseStarted,
			ToolName: t.card.Name,
			ToolID:   t.card.ID,
			Inputs:   inputs,
			Extra:    map[string]any{"schema": t.card.InputParams},
		})
		formatted, err := tool.SchemaUtils{}.FormatWithSchema(
			inputs, t.card.InputParams,
			tool.WithFormatSkipValidate(callOpts.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		arguments = formatted
		if callOpts.SkipNoneValue {
			arguments = tool.SchemaUtils{}.RemoveNoneValues(arguments)
			if arguments == nil {
				arguments = make(map[string]any)
			}
		}
		// 触发 TOOL_PARSE_FINISHED 事件
		runnnercallback.GetCallbackFramework().TriggerTool(ctx, &runnnercallback.ToolCallEventData{
			Event:    runnnercallback.ToolParseFinished,
			ToolName: t.card.Name,
			ToolID:   t.card.ID,
			Inputs:   arguments,
		})
	}

	result, err := t.mcpClient.CallTool(ctx, t.card.Name, arguments)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", t.card.String()),
			exception.WithCause(err),
		)
	}

	extracted := ExtractMCPToolResultContent(result)
	return map[string]any{"result": extracted}, nil
}

// Stream MCP 工具不支持流式调用，返回 ErrStreamNotSupported。
//
// 对应 Python: MCPTool.stream() → raise TOOL_STREAM_NOT_SUPPORTED
func (t *MCPTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}
