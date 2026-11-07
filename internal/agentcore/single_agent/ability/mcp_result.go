package ability

import (
	"encoding/json"

	mcptool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// emptySuccessText 工具成功但无可渲染文本时的兜底文案。
	//
	// 模型收到空字符串会当成"工具什么都没做"，给一句明确的占位更利于继续推理。
	// Python: EMPTY_SUCCESS_TEXT (foundation/tool/base.py:23)
	emptySuccessText = "Tool succeeded with empty output."
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// renderMCPResultForLLM 把 MCP 结果渲染成模型可见文本。
//
// invoke 把普通结果包成 {"result": value}，模型读的是 value 本身；
// 多模态结果读 data.content（multimodal 图片项由单独的消息投递）。
//
// Python: MCPTool.render_for_llm (mcp/base.py:167-176)
func renderMCPResultForLLM(r *mcptool.MCPToolResult) string {
	if r == nil {
		return emptySuccessText
	}
	if r.Data != nil {
		if text := renderPayloadText(r.Data); text != "" {
			return text
		}
	}
	if r.Err != "" {
		return r.Err
	}
	return emptySuccessText
}

// renderPayloadText 把任意 payload 渲染成模型可见文本。
//
// 字符串原样返回；含 content 键的 map 只取 content（同级键如 multimodal
// 属于程序侧数据，不进模型上下文）；其余类型 JSON 序列化。
//
// Python: render_payload_text (foundation/tool/base.py:33-57)
func renderPayloadText(payload any) string {
	if payload == nil {
		return ""
	}
	if m, ok := payload.(map[string]any); ok {
		if inner, has := m["content"]; has {
			if inner == nil {
				return ""
			}
			payload = inner
		}
	}
	if s, ok := payload.(string); ok {
		return s
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// unwrapMCPResult 拆出 MCP 结构体结果并给出模型可见文本。
//
// MCPTool.invoke 用 __mcp_result__ 承载 *MCPToolResult（普通 map 无法同时
// 表达"成功/失败状态"与"多模态数据"）。这里拆出结构体本体放进 Result，
// 文本按 MCPTool.render_for_llm 的规则渲染。
//
// 返回的 result 保持原值供上层（如轨迹记录）使用，content 是给模型的文本。
func unwrapMCPResult(result map[string]any) (any, string) {
	raw, has := result["__mcp_result__"]
	if !has {
		return result, BuildToolMessageContent(result)
	}
	structured, ok := coerceMCPToolResult(raw)
	if !ok {
		return result, BuildToolMessageContent(result)
	}
	if structured.Success {
		return structured, renderMCPResultForLLM(structured)
	}
	text := structured.Err
	if text == "" {
		text = renderMCPResultForLLM(structured)
	}
	return structured, text
}

// coerceMCPToolResult 把 __mcp_result__ 的值还原为 *MCPToolResult。
//
// 为什么要还原：NewTool/NewInvokeFunction 的输出序列化（structToMap）会对
// 返回值做 JSON round-trip，*MCPToolResult 到这里已是 map[string]any。
// 这里按字段形态还原，而不是改动通用序列化链路——那条链路服务所有工具，
// 为单个类型开后门会波及全部输出。
//
// 上游 McpToolResult 本就设计成与 ToolOutput 鸭子类型兼容
// （success / data / error 三键），这里的形态识别与该设计一致。
func coerceMCPToolResult(raw any) (*mcptool.MCPToolResult, bool) {
	if structured, ok := raw.(*mcptool.MCPToolResult); ok {
		return structured, true
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	// 三个键必须齐全，否则不是 MCP 结构体
	successRaw, hasSuccess := m["Success"]
	dataRaw, hasData := m["Data"]
	errRaw, hasErr := m["Err"]
	if !hasSuccess || !hasData || !hasErr {
		return nil, false
	}
	success, ok := successRaw.(bool)
	if !ok {
		return nil, false
	}
	data, _ := dataRaw.(map[string]any)
	errText, _ := errRaw.(string)
	return &mcptool.MCPToolResult{Success: success, Data: data, Err: errText}, true
}
