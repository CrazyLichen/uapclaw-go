package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	runnnercallback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MCPTool MCP 协议工具，通过 McpClient 调用远程 MCP 服务器工具。
//
// Python: openjiuwen/core/foundation/tool/mcp/base.py (MCPTool)
type MCPTool struct {
	card      *types.McpToolCard
	mcpClient types.McpClient
	// includeImageContent 是否把图片块桥接为多模态数据（来自 McpServerConfig）
	includeImageContent bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// MCPToolResult 携带多模态数据或失败状态的 MCP 工具结果（types 包类型的别名）。
//
// 定义放在 types 包：调度层与 agent 层都要消费它，依赖纯类型包可避免
// 两个上层直接依赖 mcp 实现包。这里保留别名，使既有调用点无需改动。
//
// Python: McpToolResult (mcp/base.py:148-165)
type MCPToolResult = types.MCPToolResult

// 重导出变量和函数
var (
	// WithServerID 设置服务器标识。
	// TODO(#通用): 考虑移除 reexport，让调用者直接使用 types 包
	WithServerID = types.WithServerID
	// NewMcpServerConfig 创建 MCP 服务器配置。
	// TODO(#通用): 考虑移除 reexport，让调用者直接使用 types 包
	NewMcpServerConfig = types.NewMcpServerConfig
	// WithMcpToolCardServerID 设置 MCP 工具卡片的服务器标识。
	// TODO(#通用): 考虑移除 reexport，让调用者直接使用 types 包
	WithMcpToolCardServerID = types.WithMcpToolCardServerID
	// NewMcpToolCard 创建 MCP 工具卡片。
	// TODO(#通用): 考虑移除 reexport，让调用者直接使用 types 包
	NewMcpToolCard = types.NewMcpToolCard
)

// preserveMCPToolResultStatus 按 MCP 的 isError 给抽取结果打成功/失败标。
//
// isError 不为真时原样返回 content（成功值一个字节都不改）；
// isError 为真时收集全部文本块作为错误信息，包成 Success=false 的结构体。
//
// Python: preserve_mcp_tool_result_status (mcp/base.py:36-53)
func preserveMCPToolResultStatus(toolResult any, content any) any {
	resultMap, _ := toolResult.(map[string]any)
	isError, _ := resultMap["isError"].(bool)
	if !isError {
		return content
	}

	var errorParts []string
	if contentList, ok := resultMap["content"].([]any); ok {
		for _, itemRaw := range contentList {
			if item, ok := itemRaw.(map[string]any); ok {
				if text, ok := item["text"].(string); ok && text != "" {
					errorParts = append(errorParts, text)
				}
			}
		}
	}
	errorText := strings.Join(errorParts, "\n\n")
	if strings.TrimSpace(errorText) == "" {
		errorText = "MCP tool reported an error."
	}

	if existing, ok := content.(*MCPToolResult); ok {
		updated := *existing
		updated.Success = false
		updated.Err = errorText
		return &updated
	}
	return &MCPToolResult{Success: false, Data: map[string]any{"result": content}, Err: errorText}
}

// ExtractMCPToolResultContent 从 MCP CallToolResult 抽取紧凑结果。
//
// 保留全部内容块：文本块与图片占位符按顺序拼接，多块结果不再只剩最后一块
// （"动作确认文本 + 截图"这类结果的文本部分不再丢失）。
//
// 图片块默认转文本占位符；includeImageContent 为真时产出 *MCPToolResult，
// 其 Data.multimodal 交给 react agent 的多模态管线。
//
// 四个返回点都经 preserveMCPToolResultStatus，isError 不会被吞掉。
//
// Python: extract_mcp_tool_result_content (mcp/base.py:56-134)
func ExtractMCPToolResultContent(toolResult any, includeImageContent bool, toolName string) any {
	resultMap, ok := toolResult.(map[string]any)
	if !ok {
		return preserveMCPToolResultStatus(toolResult, nil)
	}
	contentRaw, hasContent := resultMap["content"]
	contentList, _ := contentRaw.([]any)
	if !hasContent || len(contentList) == 0 {
		return preserveMCPToolResultStatus(toolResult, nil)
	}

	var textParts []string
	type imageBlock struct{ mimeType, data string }
	var images []imageBlock

	for _, itemRaw := range contentList {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			if len(contentList) == 1 {
				return preserveMCPToolResultStatus(toolResult, fmt.Sprintf("%v", itemRaw))
			}
			textParts = append(textParts, fmt.Sprintf("%v", itemRaw))
			continue
		}

		if text, ok := item["text"].(string); ok {
			textParts = append(textParts, text)
			continue
		}

		if data, hasData := item["data"]; hasData {
			mimeType, _ := item["mimeType"].(string)
			if mimeType == "" {
				mimeType, _ = item["mime_type"].(string)
			}
			if strings.HasPrefix(mimeType, "image/") {
				dataText := fmt.Sprintf("%v", data)
				if includeImageContent {
					images = append(images, imageBlock{mimeType: mimeType, data: dataText})
				} else {
					textParts = append(textParts, fmt.Sprintf("[image content: %s, %d base64 chars]", mimeType, len(dataText)))
				}
				continue
			}
			if len(contentList) == 1 {
				return preserveMCPToolResultStatus(toolResult, data)
			}
			textParts = append(textParts, fmt.Sprintf("%v", data))
			continue
		}

		// 其他情况：等价 Python model_dump(exclude_none=True) 后 pop("data")
		cleaned := make(map[string]any, len(item))
		for key, value := range item {
			if key == "data" || value == nil {
				continue
			}
			cleaned[key] = value
		}
		if len(contentList) == 1 {
			return preserveMCPToolResultStatus(toolResult, cleaned)
		}
		if jsonBytes, marshalErr := json.Marshal(cleaned); marshalErr == nil {
			textParts = append(textParts, string(jsonBytes))
		} else {
			textParts = append(textParts, fmt.Sprintf("%v", item))
		}
	}

	text := strings.Join(textParts, "\n\n")
	if len(images) == 0 {
		return preserveMCPToolResultStatus(toolResult, text)
	}

	note := fmt.Sprintf("%d image(s) attached as multimodal input.", len(images))
	content := note
	if text != "" {
		content = text + "\n\n" + note
	}
	items := make([]any, 0, len(images))
	for _, img := range images {
		sourcePath := toolName
		if sourcePath == "" {
			sourcePath = "mcp_tool"
		}
		items = append(items, map[string]any{
			"type":        "image",
			"source":      "mcp",
			"source_path": sourcePath,
			"mime_type":   img.mimeType,
			"data_url":    "data:" + img.mimeType + ";base64," + img.data,
		})
	}
	return preserveMCPToolResultStatus(toolResult, &MCPToolResult{
		Success: true,
		Data:    map[string]any{"content": content, "multimodal": items},
	})
}

// NewMCPTool 创建 MCP 工具实例。
// mcpClient 为 nil 时返回 StatusToolMcpClientNotSupported 错误。
//
// Python: MCPTool.__init__(mcp_client, tool_info)
func NewMCPTool(mcpClient types.McpClient, card *types.McpToolCard, includeImageContent bool) (*MCPTool, error) {
	if mcpClient == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpClientNotSupported,
			exception.WithParam("card", card.String()),
		)
	}
	return &MCPTool{card: card, mcpClient: mcpClient, includeImageContent: includeImageContent}, nil
}

// Card 返回工具配置卡片。
func (t *MCPTool) Card() *tool.ToolCard {
	return &t.card.ToolCard
}

// GetCallTimeout 返回外层调用预算，沿用卡片级策略。
//
// MCP 远程调用的超时由 transport 与卡片声明共同决定，
// 这里不需要像 bash 那样额外留清理宽限。
func (t *MCPTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return tool.BaseCallTimeout(inputs, defaultTimeout)
}

// McpCard 返回 MCP 工具配置卡片，包含服务器标识信息。
func (t *MCPTool) McpCard() *types.McpToolCard {
	return t.card
}

// Invoke 调用 MCP 远程工具。
//
// 流程：
//  1. 如果 card.InputParams 不为 nil，用 SchemaUtils 格式化参数
//  2. mcpClient.CallTool 调用远程工具
//  3. ExtractMCPToolResultContent 提取紧凑结果
//  4. 返回 {"result": extracted}
//
// Python: MCPTool.invoke()
func (t *MCPTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	arguments := inputs
	if t.card.InputParams != nil {
		callOpts := tool.NewToolCallOptions(opts...)
		// MCPTool 默认移除 None 值参数，对齐 Python: skip_none_value = kwargs.get("skip_none_value", True)
		callOpts.SkipNoneValue = true
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

	extracted := ExtractMCPToolResultContent(result, t.includeImageContent, t.card.Name)
	// 多模态/失败结果原样透传结构体，普通结果包一层 {"result": ...}。
	// Python: invoke() 里 `if isinstance(result, McpToolResult): return result`
	if structured, ok := extracted.(*MCPToolResult); ok {
		return map[string]any{"__mcp_result__": structured}, nil
	}
	return map[string]any{"result": extracted}, nil
}

// Stream MCP 工具不支持流式调用，返回 ErrStreamNotSupported。
//
// Python: MCPTool.stream() → raise TOOL_STREAM_NOT_SUPPORTED
func (t *MCPTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}
