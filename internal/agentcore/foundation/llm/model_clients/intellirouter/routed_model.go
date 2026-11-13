package intellirouter

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/provider"
)

// ──────────────────────────── 常量 ────────────────────────────

// RoutedModelPlaceholder 路由模型占位符
// （Python: intelli_router_model_client.py:56-61 ROUTED_MODEL_PLACEHOLDER）。
const RoutedModelPlaceholder = "<routed-model/>"

// ──────────────────────────── 导出函数 ────────────────────────────

// RoutedModelLabel 以模型组成员视角命名 deployment。
// fallback_tag 是组内配置的模型名（作为可选中模型提供）；model_name 是
// provider 面向的名字，未配 tag 时使用。
// Python: intelli_router_model_client.py:63-71 routed_model_label
func RoutedModelLabel(deployment *core.Deployment) string {
	label := ""
	if deployment.FallbackTag != nil {
		label = *deployment.FallbackTag
	}
	if label == "" {
		label = deployment.ModelName
	}
	return strings.TrimSpace(label)
}

// HasRoutedModelPlaceholder 文本是否携带路由模型占位符。
// Python: intelli_router_model_client.py:73-76 has_routed_model_placeholder
func HasRoutedModelPlaceholder(text string) bool {
	return strings.Contains(text, RoutedModelPlaceholder)
}

// FillRoutedModel 替换文本中的全部路由模型占位符。
// Python: intelli_router_model_client.py:78-80 fill_routed_model
func FillRoutedModel(text, label string) string {
	return strings.ReplaceAll(text, RoutedModelPlaceholder, label)
}

// ──────────────────────────── 结构体 ────────────────────────────

// routedModelAdapter 注入占位符回填的 provider 适配器包装。
// Python: intelli_router_model_client.py:111-128 _RoutedModelAdapter
// （__getattr__ 委托 → Go 内嵌接口隐式委托）。
type routedModelAdapter struct {
	// inner 被包装的原适配器（Python: self._adapter）；内嵌接口隐式委托
	// 其余方法（对齐 Python __getattr__ 转发）
	provider.BaseProviderAdapter
}

// TransformRequest 回填占位符后委托原适配器。
// Python: :117-124 transform_request。label 对畸形 deployment 也不为空
// （回退 model_name）；“or“ 兜底是占位符契约的边界：绝不把字面
// <routed-model/> 发给 provider。
func (a *routedModelAdapter) TransformRequest(model string, messages []map[string]any, deployment *core.Deployment, kwargs map[string]any) map[string]any {
	label := RoutedModelLabel(deployment)
	if label == "" {
		label = deployment.ModelName
		if label == "" {
			label = "unknown"
		}
	}
	for _, message := range messages {
		fillMessagePlaceholder(message, label)
	}
	return a.BaseProviderAdapter.TransformRequest(model, messages, deployment, kwargs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fillMessagePlaceholder 就地回填一条消息内的占位符。
// Python: :83-108 _fill_routed_model（Python 只拷贝含占位符的消息且不突变
// messages 本体；Go 侧 TransformRequest 入参 messages 为请求体内部组装产物，
// 就地回填不影响调用方复用——路由器每次尝试重建请求体，语义等价）。
// 覆盖两形态：content 为 string，或 content 为多模态 part 列表
// （{text: ...} / {image: ...}）。
func fillMessagePlaceholder(message map[string]any, label string) {
	content, ok := message["content"].(string)
	if ok {
		if HasRoutedModelPlaceholder(content) {
			message["content"] = FillRoutedModel(content, label)
		}
		return
	}
	parts, ok := message["content"].([]any)
	if !ok {
		return
	}
	for _, part := range parts {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := partMap["text"].(string); ok && HasRoutedModelPlaceholder(text) {
			partMap["text"] = FillRoutedModel(text, label)
		}
	}
}
