package provider

import (
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SiliconFlowAdapter SiliconFlow（硅基流动）适配器 - 在 OpenAI 兼容基础上清洗 tool_calls。
// ← provider/siliconflow_provider.py:10 SiliconFlowProviderAdapter(OpenAIProviderAdapter)
//
// SiliconFlow API 整体兼容 OpenAI 协议，但对 tool_calls 的字段校验非常
// 严格，不允许任何额外字段（如 index）。需要在请求发送前清洗。（py:14-16 原文）
type SiliconFlowAdapter struct {
	OpenAIAdapter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSiliconFlowAdapter 构造。
func NewSiliconFlowAdapter() *SiliconFlowAdapter { return &SiliconFlowAdapter{} }

// TransformRequest 调基座后清洗 tool_calls。← siliconflow_provider.py:16-25
func (a *SiliconFlowAdapter) TransformRequest(model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) map[string]any {
	body := a.OpenAIAdapter.TransformRequest(model, messages, deployment, kwargs)
	// ← py:23 sanitize_tool_calls
	if bodyMessages, ok := body["messages"].([]map[string]any); ok {
		body["messages"] = SanitizeToolCalls(bodyMessages)
	}
	return body
}
