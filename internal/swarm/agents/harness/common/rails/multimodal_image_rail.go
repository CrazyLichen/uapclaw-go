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
	"sync"

	hsrails "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
	// imageMutator 当前窗口变形器闭包（每轮 before_model_call 替换式重建）。
	// Python: _window_mutators 的替换式安装；Go 侧由 rail 持有（D-23.8-3）。
	// imageMu 保护其并发读写（BeforeModelCall 写、窗口组装点读）。
	imageMutator prompt.MultimodalImageWindowMutator
	imageMu      sync.RWMutex
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
	r.imageMu.Lock()
	r.imageMutator = nil
	r.imageMu.Unlock()
	return nil
}

// Priority rail 优先级。
// Python: priority = 75
func (r *MultimodalImageRail) Priority() int { return MultimodalImageRailPriority }

// ImageMutator 供上下文窗口组装点取用当前变形器（D-23.8-3 的消费入口）。
// 未安装时返回 nil。
func (r *MultimodalImageRail) ImageMutator() prompt.MultimodalImageWindowMutator {
	r.imageMu.RLock()
	defer r.imageMu.RUnlock()
	return r.imageMutator
}

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
		mutator, installed := prompt.EnsureMultimodalImageWindowMutator(imageFiles)
		if installed {
			// Python 替换式安装：先清旧 mutator 再挂新（ensure_multimodal_image_window_mutator:289-292）；
			// Go 侧闭包由 rail 单槽持有，直接覆盖即等价「清旧再挂新」。
			r.imageMu.Lock()
			r.imageMutator = mutator
			r.imageMu.Unlock()
			logger.Info(multimodalImageLogComponent).
				Int("image_count", len(imageFiles)).
				Msg("Installed multimodal image context-window adapter")
		} else {
			// 无图轮：清掉上一轮残留（Python 每轮窗口全新，旧 mutator 不会带入本轮）
			r.imageMu.Lock()
			r.imageMutator = nil
			r.imageMu.Unlock()
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
