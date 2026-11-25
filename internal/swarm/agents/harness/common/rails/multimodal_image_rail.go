package rails

// 多模态图片输入适配护栏：为原生多模态模型调用准备图片附件。
//
// Python: jiuwenswarm/agents/harness/common/rails/multimodal_image_rail.py（60 行一比一移植，23.8）
// 仅注册 before_model_call；不注册系统提示词，也不在回调里读图片字节——
// 读文件发生在窗口变形器执行（上下文窗口组装）时。
//
// 语言差异：Python contextvars「隐形袋子」→ Go 由 adapter 在 invoke 入口写入
// request context，本 rail 从 BeforeModelCall 的 ctx 读取（偏差 D-23.8-1）；
// Python _window_mutators 列表 → Go 闭包由本 rail 持有、窗口组装点调用（D-23.8-3）。

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	ceiface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	hsrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件。
const multimodalImageLogComponent = logger.ComponentAgentCore

// MultimodalImageRailPriority rail 优先级。
// Python: MultimodalImageRail.priority = 75
const MultimodalImageRailPriority = 75

// ──────────────────────────── 结构体 ────────────────────────────

// MultimodalImageRail 原生多模态图片输入适配护栏。
type MultimodalImageRail struct {
	hsrails.DeepAgentRail
	// enableImageMultimodal 显式开关（nil=自动判定）。
	// Python: self._enable_image_multimodal
	enableImageMultimodal *bool
	// deepAgent init 时注入的 agent 引用。
	// Python: self._deep_agent
	deepAgent agentinterfaces.BaseAgent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMultimodalImageRail 构造图片适配护栏。
// Python: __init__(enable_image_multimodal=None)
func NewMultimodalImageRail(enableImageMultimodal *bool) *MultimodalImageRail {
	return &MultimodalImageRail{enableImageMultimodal: enableImageMultimodal}
}

// Init 注入 agent 引用。
// Python: init(agent)
func (r *MultimodalImageRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.deepAgent = agent
	return nil
}

// Uninit 反向清理。
func (r *MultimodalImageRail) Uninit(_ agentinterfaces.BaseAgent) error {
	r.deepAgent = nil
	return nil
}

// Priority rail 优先级。
// Python: priority = 75
func (r *MultimodalImageRail) Priority() int { return MultimodalImageRailPriority }

// GetCallbacks 覆写基类回调映射，注册 BeforeModelCall。
// 注意：基类方法体内的钩子派发是静态绑定（Go 嵌入虚方法陷阱），
// 必须像 ProjectMemoryRail 一样显式覆写注册，否则回调静默不触发。
func (r *MultimodalImageRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 回调 ────────────────────────────

// BeforeModelCall 模型调用前：开启路径安装窗口变形器，关闭路径剥离历史图片块。
// Python: before_model_call:40-59
func (r *MultimodalImageRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: if ctx.context is None: return（cbc 为 nil 时无上下文可操作）
	if cbc == nil {
		return nil
	}

	if r.readImageMultimodalEnabled() {
		// Python: image_files = current_multimodal_image_files()
		// Go: 图片列表由 adapter 写入 request ctx（D-23.8-1）
		imageFiles := prompt.MultimodalImageFilesFromContext(ctx)
		// Python: ensure_multimodal_image_window_mutator(ctx.context, image_files)
		// 替换式安装进 context 持久投影链（清旧标记项→按需挂新，user_prompt_builder.py:288-292）；
		// 消费点为取窗口时的持久+call 级合并（context.py:737 对应实现）。
		installed := prompt.EnsureMultimodalImageWindowMutator(cbc.ModelContext(), imageFiles)
		if installed {
			logger.Warn(multimodalImageLogComponent).
				Int("image_count", len(imageFiles)).
				Msg("PROBE: Installed multimodal image context-window adapter")
		} else {
			logger.Warn(multimodalImageLogComponent).Int("images", len(imageFiles)).
				Msg("PROBE: not installed")
			// 无图轮/载体不支持：清掉上一轮带标记残留（Python ensure 空图路径同款清理）
			if mc := cbc.ModelContext(); mc != nil {
				if registrar, ok := mc.(ceiface.WindowMutatorsRegistrar); ok {
					kept := make([]ceiface.WindowMutator, 0, len(registrar.WindowMutators()))
					for _, m := range registrar.WindowMutators() {
						if !prompt.IsMultimodalImageMutator(m) {
							kept = append(kept, m)
						}
					}
					registrar.SetWindowMutators(kept)
				}
			}
		}
		return nil
	}

	// Python: removed = strip_image_content_from_model_context(ctx.context)
	removed := r.stripImageContentFromCallbackContext(cbc)
	if removed > 0 {
		logger.Info(multimodalImageLogComponent).
			Int("removed", removed).
			Msg("Removed image content block(s) from chat-model context")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readImageMultimodalEnabled 开关判定：显式构造参数 > 四级自动解析。
// Python: _read_image_multimodal_enabled:33-38
func (r *MultimodalImageRail) readImageMultimodalEnabled() bool {
	if r.enableImageMultimodal != nil {
		return *r.enableImageMultimodal
	}
	return hsrails.ShouldEnableReadImageMultimodal(r.deepAgent, nil)
}

// stripImageContentFromCallbackContext 从回调上下文的消息面剥除图片块。
// Python: strip_image_content_from_model_context(ctx.context) 遍历 context.get_messages()；
// Go 侧经 cbc.modelContext 取消息列表（GetMessages(0, true) 对应 Python 默认全量）。
func (r *MultimodalImageRail) stripImageContentFromCallbackContext(cbc *agentinterfaces.AgentCallbackContext) int {
	if cbc.ModelContext() == nil {
		return 0
	}
	messages, err := cbc.ModelContext().GetMessages(0, true)
	if err != nil {
		logger.Error(multimodalImageLogComponent).Err(err).Msg("get messages for image strip failed")
		return 0
	}
	return prompt.StripImageContentFromModelContext(messages)
}
