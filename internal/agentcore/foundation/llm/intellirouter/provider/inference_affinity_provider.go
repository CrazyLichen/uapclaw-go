package provider

import (
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InferenceAffinityAdapter InferenceAffinity（vLLM 部署）适配器。
// ← provider/inference_affinity_provider.py:10 InferenceAffinityProviderAdapter
// （继承 OpenAIProviderAdapter）
//
// vLLM 部署在 OpenAI 兼容基础上额外支持：
//   - KV cache 共享（cache_sharing + cache_salt）
//   - tool_calls 字段清洗（与 SiliconFlow 类似）
type InferenceAffinityAdapter struct {
	OpenAIAdapter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInferenceAffinityAdapter 构造。
func NewInferenceAffinityAdapter() *InferenceAffinityAdapter { return &InferenceAffinityAdapter{} }

// TransformRequest 调基座后清洗 tool_calls 并注入 vLLM KV cache 共享参数。
// ← inference_affinity_provider.py:20-38 transform_request
func (a *InferenceAffinityAdapter) TransformRequest(model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) map[string]any {
	// ← py:22-24 kwargs.pop 三个专属键
	sessionID := popAny(kwargs, "session_id")
	enableCacheSharing := popBool(kwargs, "enable_cache_sharing", false)
	returnTokenIDs := popAny(kwargs, "return_token_ids")

	body := a.OpenAIAdapter.TransformRequest(model, messages, deployment, kwargs)
	// ← py:26 sanitize_tool_calls
	if bodyMessages, ok := body["messages"].([]map[string]any); ok {
		body["messages"] = SanitizeToolCalls(bodyMessages)
	}

	// ← py:28-30 cache_sharing 仅在开启且有 session_id 时注入
	if enableCacheSharing && sessionID != nil {
		body["cache_sharing"] = true
		body["cache_salt"] = sessionID
	}
	if returnTokenIDs != nil {
		// ← py:32-34
		body["return_token_ids"] = returnTokenIDs
	}
	return body
}
