package provider

import (
	"io"
	"strings"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenAIAdapter OpenAI / 兼容 endpoint 适配器。
// ← provider/openai_provider.py:13 OpenAIProviderAdapter
//
// TransformRequest / TransformResponse / TransformStreamChunk 均使用
// BaseProviderAdapter 的默认实现（透传），因为 intelli_router 内部本就是
// 用 OpenAI 格式作为标准格式。（py:16-19 原文）
type OpenAIAdapter struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAIAdapter 构造。
func NewOpenAIAdapter() *OpenAIAdapter { return &OpenAIAdapter{} }

// GetAPIURL chat/completions 端点。← py:28-30 get_api_url
func (a *OpenAIAdapter) GetAPIURL(deployment *intelliroutercore.Deployment, stream bool) string {
	return chatCompletionsURL(deployment.APIBase)
}

// GetHeaders 请求头。← py:32-46 get_headers
func (a *OpenAIAdapter) GetHeaders(deployment *intelliroutercore.Deployment) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + deployment.APIKey, // ← py:34
		"Content-Type":  "application/json",
	}
	if len(deployment.CustomHeaders) > 0 {
		// ← py:36-45 custom_headers 覆盖 Authorization（告警 + 移除默认头）：
		// custom_header_names = {name.lower()}，大小写不敏感检测
		customNames := make(map[string]bool, len(deployment.CustomHeaders))
		for name := range deployment.CustomHeaders {
			customNames[strings.ToLower(name)] = true // ← py:37
		}
		if customNames["authorization"] {
			for name := range headers {
				if strings.EqualFold(name, "authorization") { // ← py:41-43
					delete(headers, name)
				}
			}
		}
		for k, v := range deployment.CustomHeaders { // ← py:45 headers.update(...)
			headers[k] = v
		}
	}
	return headers
}

// TransformRequest 默认透传（继承 BaseProviderAdapter 默认实现，py:16-19）。
func (a *OpenAIAdapter) TransformRequest(model string, messages []map[string]any, _ *intelliroutercore.Deployment, kwargs map[string]any) map[string]any {
	return DefaultTransformRequest(model, messages, kwargs)
}

// ValidateRequestConfig 默认无校验（py:57-66 默认 return None）。
func (a *OpenAIAdapter) ValidateRequestConfig(_ *intelliroutercore.Deployment, _ map[string]any) {}

// TransformResponse 默认透传（py:73-78）。
func (a *OpenAIAdapter) TransformResponse(rawResponse map[string]any, _ string, _ *intelliroutercore.Deployment) map[string]any {
	return rawResponse
}

// TransformStreamChunk 默认透传（py:89-95）。
func (a *OpenAIAdapter) TransformStreamChunk(chunk map[string]any, _ string, _ *intelliroutercore.Deployment) map[string]any {
	return chunk
}

// SignRequest 默认不签名（py:102-106）。
func (a *OpenAIAdapter) SignRequest(_, _ string, headers map[string]string, _ []byte, _ *intelliroutercore.Deployment) map[string]string {
	return headers
}

// IterStreamEvents 标准 SSE 解析（默认实现，py:143）。
func (a *OpenAIAdapter) IterStreamEvents(body io.Reader, deploymentID string) (*StreamEventIter, error) {
	return NewStreamEventIter(body, deploymentID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// chatCompletionsURL 归一化 chat/completions URL。← py:22-27 _chat_completions_url
func chatCompletionsURL(apiBase string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	// ← py:24-26 已带 /chat/completions 则剥掉再拼（幂等）
	if strings.HasSuffix(base, "/chat/completions") {
		base = strings.TrimRight(base[:len(base)-len("/chat/completions")], "/")
	}
	return base + "/chat/completions"
}
