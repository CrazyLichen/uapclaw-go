package rails

import (
	hllm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/modalityprobe"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// 本文件为 harness rails 共享的原生图片输入策略助手。
// Python: openjiuwen/harness/rails/_multimodal.py（一文件一文件移植，23.7）

// ──────────────────────────── 导出函数 ────────────────────────────

// ModelSupportsVision 读取模型的 supports_vision 声明；未声明返回 nil。
// Python: _model_supports_vision:14-27（ModelClientConfig extra="allow"，
// pydantic 自动接受；Go 为显式字段 SupportsVision *bool）
func ModelSupportsVision(model *hllm.Model) *bool {
	if model == nil || model.ClientConfig == nil {
		return nil
	}
	return model.ClientConfig.SupportsVision
}

// ShouldEnableReadImageMultimodal 解析 agent 当前模型能否接收图片字节。
//
// 解析优先级（从高到低）：
//  1. explicitValue（调用方显式覆盖）
//  2. DeepAgentConfig.enable_read_image_multimodal（全局 react 配置）
//  3. ModelClientConfig.supports_vision（模型条目声明）
//  4. 探测缓存（运行时探测结论）
//
// 专用 vision 工具与本判定无关：原生输入与工具式视觉是两个独立能力，
// 可以同时可用。
//
// Python: should_enable_read_image_multimodal:52-74
func ShouldEnableReadImageMultimodal(agent hinterfaces.DeepAgentInterface, explicitValue *bool) bool {
	if agent == nil {
		return false
	}
	var deepConfig *hschema.DeepAgentConfig
	if cfg := agent.DeepConfig(); cfg != nil {
		deepConfig = cfg
	}
	return resolveImageMultimodal(deepConfig, explicitValue)
}

// BuildReadImageMultimodalResolver 构建运行时动态解析闭包（不持有整个 agent）。
// Python: build_read_image_multimodal_resolver:77-91
func BuildReadImageMultimodalResolver(agent hinterfaces.DeepAgentInterface, explicitValue *bool) func() bool {
	var deepConfig *hschema.DeepAgentConfig
	if agent != nil {
		if cfg := agent.DeepConfig(); cfg != nil {
			deepConfig = cfg
		}
	}
	return func() bool {
		return resolveImageMultimodal(deepConfig, explicitValue)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveImageMultimodal 共享解析逻辑：显式 > 模型声明 > 全局 > 探测缓存。
// Python: _resolve_image_multimodal:30-49
func resolveImageMultimodal(deepConfig *hschema.DeepAgentConfig, explicitValue *bool) bool {
	if explicitValue != nil {
		return *explicitValue
	}

	if deepConfig == nil {
		return false
	}
	// Python: configured_value = deep_config.enable_read_image_multimodal
	// None=auto 继续向下走声明/探测；Go 用 nil 指针表达
	if deepConfig.EnableReadImageMultimodal != nil {
		return *deepConfig.EnableReadImageMultimodal
	}

	model := deepConfig.Model

	// 模型条目声明优先于探测缓存
	// Python: vision = _model_supports_vision(model)
	if vision := ModelSupportsVision(model); vision != nil {
		return *vision
	}

	// Python: return get_cached_image_support(model) is True
	cached := modalityprobe.GetCachedImageSupport(model)
	return cached != nil && *cached
}
