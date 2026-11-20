package providers

import (
	swarmrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// builtin_rails.go —— swarm 内置 rail 的 provider 段。
//
// 这些元素由 openjiuwen 自身注册并被裸名引用（openjiuwen 持有真正的
// class）；本段只补 swarm 的声明与包装。声明在 import 时运行（模块级
// @harness_element 调用填充 catalog）；registry import 本模块保证
// register_from_catalog 之前声明已就位——Go 无 catalog，注册由
// RegisterBuiltinRailProviders 显式完成（偏差 F6）。
//
// Python: jiuwenswarm/agents/swarm/providers/builtin_rails.py（111 行）

// ──────────────────────────── 常量 ────────────────────────────

const (
	constResponsePrompt  = "swarm.response_prompt"
	constStreamEvent     = "swarm.stream_event"
	constAvatarPrompt    = "swarm.avatar_prompt"
	constMultimodalImage = "swarm.multimodal_image"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildResponsePromptRail 每次模型调用前追加响应格式提示段。
//
// Go 基座 ResponsePromptRail 缺 set_channel（Python :58-60 存 runtime
// channel）⤴️15.20；构造与挂载语义一致。
// Python: _build_response_prompt_rail (builtin_rails.py:61-67)
func buildResponsePromptRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	// ResponsePromptInput.resolve：channel = context_field(attr="channel", default="default")
	channel := stringFieldOrEmpty(view.ViewChannel(), "default")
	rail := swarmrails.NewResponsePromptRail()
	// Python: rail.set_channel(inp.channel)——基座缺 ⤴️15.20
	_ = channel
	return rail, nil
}

// buildStreamEventRail 跨成员生命周期发 JiuSwarm 流事件。
//
// Go 基座 JiuClawStreamEventRail 构造无 role 形参（Python :292-299 构造期
// 接收 role 并 lower-strip）⤴️15.20。
// Python: _build_stream_event_rail (builtin_rails.py:77-84)
func buildStreamEventRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	// StreamEventInput.resolve：role = context_field(attr="role", default="")
	role := stringFieldOrEmpty(view.ViewRole(), "")
	_ = role
	rail := swarmrails.NewJiuClawStreamEventRail()
	return rail, nil
}

// buildAvatarPromptRail 注入每请求数字人提示段（class builder 直注册）。
// Python: harness_element(name=AVATAR_PROMPT, builder=AvatarPromptRail)（:94-98）
func buildAvatarPromptRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = params
	_ = view
	// Python builder=AvatarPromptRail：manifest 以 (params, context) 实例化；
	// Go 基座 NewAvatarPromptRail() 无参构造（对齐）。
	return swarmrails.NewAvatarPromptRail(), nil
}

// buildMultimodalImageRail 请求图片附件注入成员模型上下文，或模型无原生
// 图像输入时剥离图像块。
// Python: harness_element(name=MULTIMODAL_IMAGE, builder=MultimodalImageRail)（:99-104）
func buildMultimodalImageRail(params map[string]any, view SwarmContextView) (any, error) {
	_ = view
	// Go 基座构造形参 enableImageMultimodal *bool；Python class builder 从
	// params 取值。params 缺省 nil（基座自决）。
	var enable *bool
	if v, ok := params["enable_image_multimodal"].(bool); ok {
		enable = &v
	}
	return swarmrails.NewMultimodalImageRail(enable), nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterBuiltinRailProviders 注册 builtin_rails 全部 provider。
// Python: builtin_rails.py 全部 @harness_element 段（:40-104）。
func RegisterBuiltinRailProviders() {
	hschema.RegisterRailProvider(constResponsePrompt, railProviderAdapter(buildResponsePromptRail))
	hschema.RegisterRailProvider(constStreamEvent, railProviderAdapter(buildStreamEventRail))
	hschema.RegisterRailProvider(constAvatarPrompt, railProviderAdapter(buildAvatarPromptRail))
	hschema.RegisterRailProvider(constMultimodalImage, railProviderAdapter(buildMultimodalImageRail))
}
