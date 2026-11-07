package ability

import (
	mcptool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// unwrapMCPResult 拆出 MCP 结构体结果并给出模型可见文本。
//
// MCPTool.invoke 用 __mcp_result__ 承载 *MCPToolResult（普通 map 无法同时
// 表达"成功/失败状态"与"多模态数据"）。这里拆出结构体本体放进 Result，
// 文本交给 BuildToolMessageContent 的反射路径渲染——那是本仓既有的通用
// 机制（对齐 Python getattr(result, "data")），不再另写一份 MCP 专用渲染。
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
	return structured, BuildToolMessageContent(structured)
}

// coerceMCPToolResult 把 __mcp_result__ 的值还原为 *MCPToolResult。
//
// 为什么要还原：NewTool/NewInvokeFunction 的输出序列化（structToMap）会对
// 返回值做 JSON round-trip，*MCPToolResult 到这里已是 map[string]any。
// 这里按字段形态还原，而不是改动通用序列化链路——那条链路服务所有工具，
// 为单个类型开后门会波及全部输出。
//
// 已知局限（有意保留）：三键形态识别存在理论误判——若某个普通 MCP 工具
// 恰好返回 {"Success": bool, "Data": ..., "Err": string} 形状的业务数据，
// 会被当成 MCP 结构体。判定前提是：
//   - 三个键名必须同时是 PascalCase 的 Success / Data / Err
//   - Success 必须是 bool
//   - Data 需是 map、Err 需是 string 才能取到值（类型不符则退化为零值，仍算命中）
//
// 误判后果限于渲染路径变化（按 Data.content / Err 取文本），不会丢数据、
// 不改变 Result 的类型语义。真要根治需让 structToMap 支持"跳过 round-trip"
// 的标记接口，那会改动全仓所有工具的序列化路径，收益不抵风险，故不做。
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
