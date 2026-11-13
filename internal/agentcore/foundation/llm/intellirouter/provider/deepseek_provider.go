package provider

import (
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepSeekAdapter DeepSeek 适配器 - 在 OpenAI 兼容基础上处理 reasoning_content。
// ← provider/deepseek_provider.py:9 DeepSeekProviderAdapter(OpenAIProviderAdapter)
//
// Go 无继承：嵌入 OpenAIAdapter，被覆盖的方法显式写全（defer 到嵌入值），
// 其余方法经嵌入自动继承（值语义，无虚方法派发问题——此处无基类方法体
// 内多态调用，与 memory 中 embedding 陷阱场景不同）。
type DeepSeekAdapter struct {
	OpenAIAdapter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepSeekAdapter 构造。
func NewDeepSeekAdapter() *DeepSeekAdapter { return &DeepSeekAdapter{} }

// TransformRequest 调基座后在 assistant 消息缺 reasoning_content 时补空串。
// ← deepseek_provider.py:16-31 transform_request
func (a *DeepSeekAdapter) TransformRequest(model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) map[string]any {
	body := a.OpenAIAdapter.TransformRequest(model, messages, deployment, kwargs)
	// 浅拷贝每条消息，避免修改调用方原始列表 ← py:28 注释
	newMessages := make([]map[string]any, 0)
	if bodyMessages, ok := body["messages"].([]map[string]any); ok {
		for _, msg := range bodyMessages {
			if msg["role"] == "assistant" {
				if _, has := msg["reasoning_content"]; !has {
					// ← py:29 {**msg, "reasoning_content": ""}
					shallow := make(map[string]any, len(msg)+1)
					for k, v := range msg {
						shallow[k] = v
					}
					shallow["reasoning_content"] = ""
					msg = shallow
				}
			}
			newMessages = append(newMessages, msg)
		}
		body["messages"] = newMessages
	}
	return body
}
