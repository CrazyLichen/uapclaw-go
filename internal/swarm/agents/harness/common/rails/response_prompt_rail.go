package rails

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
	tools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ResponsePromptRail 注入稳定的输入与输出指令节。
//
// Python: ResponsePromptRail (response_prompt_rail.py, 260 行 1:1)。
// a2ui 段（_sync_a2ui_prompt_section）依赖 server/runtime/a2ui（2030 行，
// Go 全缺且无归属章节）——以清除语义降级，⤴️a2ui 立项后回填。
type ResponsePromptRail struct {
	rails.DeepAgentRail
	// builder 系统提示词构建器（Init 时捕获）
	builder saprompt.SystemPromptBuilderInterface
	// runtimeChannel 工厂注入的请求频道（set_channel）
	runtimeChannel string
	// sendFileConfigProvider 渠道配置读取缝（工厂注入；nil = 通道缺省语义）
	sendFileConfigProvider func() map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// responsePromptRailPriority rail 挂载优先级
	// Python: ResponsePromptRail.priority = 5
	responsePromptRailPriority = 5

	// a2uiBrowserWorkflowContextKey 浏览器工作流标记 extra 键
	// Python: A2UI_BROWSER_WORKFLOW_CONTEXT_KEY = "a2ui_browser_workflow"
	a2uiBrowserWorkflowContextKey = "a2ui_browser_workflow"

	// jiuwenswarmChannelContextKey 频道上下文 extra 键
	// Python: JIUWENSWARM_CHANNEL_CONTEXT_KEY（context_keys）
	jiuwenswarmChannelContextKey = "jiuwenswarm_channel"

	// jiuwenswarmSkipA2UIContextKey 跳过 a2ui 标记 extra 键
	// Python: JIUWENSWARM_SKIP_A2UI_CONTEXT_KEY（context_keys）
	jiuwenswarmSkipA2UIContextKey = "jiuwenswarm_skip_a2ui"

	// responseRailLogComponent 日志组件
	responseRailLogComponent = logger.ComponentAgentServer
)

// _ 编译时验证 ResponsePromptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ResponsePromptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewResponsePromptRail 创建 ResponsePromptRail 实例。
// Python: ResponsePromptRail()
func NewResponsePromptRail() *ResponsePromptRail {
	r := &ResponsePromptRail{}
	r.WithPriority(responsePromptRailPriority)
	return r
}

// SetChannel 每次请求更新频道。
// Python: ResponsePromptRail.set_channel (response_prompt_rail.py:58-60)
func (r *ResponsePromptRail) SetChannel(channel string) {
	r.runtimeChannel = strings.TrimSpace(channel)
}

// SetSendFileConfigProvider 注入渠道配置读取缝。
func (r *ResponsePromptRail) SetSendFileConfigProvider(fn func() map[string]any) {
	r.sendFileConfigProvider = fn
}

// Init 从 Agent 获取 system_prompt_builder 引用并存储。
// Python: ResponsePromptRail.init(agent)
func (r *ResponsePromptRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent != nil {
		r.builder = agent.SystemPromptBuilder()
	}
	return nil
}

// Uninit 清除注入的节并释放引用。
// Python: ResponsePromptRail.uninit (response_prompt_rail.py:49-56)
func (r *ResponsePromptRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.builder != nil {
		r.builder.RemoveSection("response")
		r.builder.RemoveSection("input")
		r.builder.RemoveSection("output")
		r.builder.RemoveSection(prompt.LocalSectionNameA2UI)
	}
	r.builder = nil
	r.runtimeChannel = ""
	return nil
}

// BeforeInvoke invoke 前：写入频道上下文 / skip_a2ui / 浏览器工作流标记。
// Python: ResponsePromptRail.before_invoke (response_prompt_rail.py:62-78)
func (r *ResponsePromptRail) BeforeInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	channel := r.resolveChannel(cbc)
	if channel != "" {
		cbc.SetExtra(jiuwenswarmChannelContextKey, channel)
	}
	if r.shouldSkipA2UI(cbc) {
		cbc.SetExtra(jiuwenswarmSkipA2UIContextKey, true)
	}
	cbc.SetExtra(a2uiBrowserWorkflowContextKey, r.isBrowserWorkflowQuery(cbc))
	return nil
}

// BeforeModelCall 注入输入/输出节并同步 a2ui 段。
// Python: ResponsePromptRail.before_model_call (response_prompt_rail.py:80-113)
func (r *ResponsePromptRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		return nil
	}

	language := r.builder.Language()
	if language == "" {
		language = "cn"
	}
	channel := r.resolveChannel(cbc)
	sendFileEnabled := true
	if channel != "" {
		sendFileEnabled = r.sendFileEnabledFallback(channel)
	}
	r.builder.RemoveSection("response")
	r.builder.AddSection(prompt.InputPrompt(language))
	r.builder.AddSection(prompt.OutputPrompt(language, sendFileEnabled))
	r.syncA2UIPromptSection(cbc, channel, r.shouldSkipA2UI(cbc), r.isBrowserWorkflow(cbc),
		r.resolveContextValue(cbc, "request_id"),
		orString(r.resolveContextValue(cbc, "conversation_id"), r.resolveContextValue(cbc, "session_id")))
	return nil
}

// GetCallbacks 覆写基类回调映射，注册 BeforeInvoke 与 BeforeModelCall。
func (r *ResponsePromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// shouldSkipA2UI 判断本请求是否跳过 a2ui。
// Python: _should_skip_a2ui (response_prompt_rail.py:115-133)
func (r *ResponsePromptRail) shouldSkipA2UI(cbc *agentinterfaces.AgentCallbackContext) bool {
	if inputs, ok := cbc.Inputs().(*agentinterfaces.MapInputs); ok && inputs != nil {
		if v, ok := inputs.Data[jiuwenswarmSkipA2UIContextKey].(bool); ok && v {
			return true
		}
	}
	if inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok {
		if v, ok := inputs.Extra[jiuwenswarmSkipA2UIContextKey].(bool); ok && v {
			return true
		}
	}
	if v, ok := r.resolveRunContextExtra(cbc)[jiuwenswarmSkipA2UIContextKey].(bool); ok && v {
		return true
	}
	if v, ok := cbc.GetExtra(jiuwenswarmSkipA2UIContextKey); ok {
		if b, ok := v.(bool); ok && b {
			return true
		}
	}
	return false
}

// resolveChannel 从回调输入读请求频道，缺省回退工厂注入频道。
// Python: _resolve_channel (response_prompt_rail.py:135-161)
func (r *ResponsePromptRail) resolveChannel(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil {
		return r.runtimeChannel
	}
	switch inputs := cbc.Inputs().(type) {
	case *agentinterfaces.MapInputs:
		if v, ok := inputs.Data["channel"]; ok && v != nil {
			return toString(v)
		}
	case *agentinterfaces.InvokeInputs:
		if v, ok := inputs.Extra["channel"]; ok && v != nil {
			return toString(v)
		}
	}
	if v, ok := r.resolveRunContextExtra(cbc)[jiuwenswarmChannelContextKey]; ok && v != nil {
		return toString(v)
	}
	if v, ok := cbc.GetExtra(jiuwenswarmChannelContextKey); ok && v != nil {
		return toString(v)
	}
	return r.runtimeChannel
}

// resolveRunContextExtra 读取当前调用的 SDK 保留元数据。
// Python: _resolve_run_context_extra (response_prompt_rail.py:163-180)
func (r *ResponsePromptRail) resolveRunContextExtra(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	var runContext any
	if inputs, ok := cbc.Inputs().(*agentinterfaces.MapInputs); ok && inputs != nil {
		runContext = inputs.Data["run_context"]
	} else if inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok {
		runContext = inputs.Extra["run_context"]
	}
	if runContext == nil {
		if v, ok := cbc.GetExtra("run_context"); ok {
			runContext = v
		}
	}
	switch rc := runContext.(type) {
	case map[string]any:
		if extra, ok := rc["extra"].(map[string]any); ok {
			return extra
		}
	}
	return map[string]any{}
}

// resolveContextValue 从 inputs / extra 依次取非空字符串值。
// Python: _resolve_context_value (response_prompt_rail.py:182-188)
func (r *ResponsePromptRail) resolveContextValue(cbc *agentinterfaces.AgentCallbackContext, key string) string {
	// Python: inputs.get(key) / extra.get(key)——Go 对应 MapInputs.Data、
	// InvokeInputs.Extra、ctx.extra 三态。
	for _, source := range []func() (map[string]any, bool){
		func() (map[string]any, bool) {
			if inputs, ok := cbc.Inputs().(*agentinterfaces.MapInputs); ok && inputs != nil {
				return inputs.Data, true
			}
			if inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok && inputs != nil {
				return inputs.Extra, true
			}
			return nil, false
		},
		func() (map[string]any, bool) {
			if extra := cbc.Extra(); extra != nil {
				return extra, true
			}
			return nil, false
		},
	} {
		if m, ok := source(); ok {
			if v, ok := m[key]; ok && v != nil && strings.TrimSpace(toString(v)) != "" {
				return toString(v)
			}
		}
	}
	return ""
}

// isBrowserWorkflowQuery 判断查询是否为 a2ui 浏览器工作流请求。
// Python: _is_browser_workflow (response_prompt_rail.py:190-197)
func (r *ResponsePromptRail) isBrowserWorkflow(cbc *agentinterfaces.AgentCallbackContext) bool {
	if v, ok := cbc.GetExtra(a2uiBrowserWorkflowContextKey); ok {
		b, _ := v.(bool)
		return b
	}
	return r.isBrowserWorkflowQuery(cbc)
}

// isBrowserWorkflowQuery 按查询文本判定（request-scoped 判定函数）。
// Python: is_a2ui_browser_workflow_request(query)（prompt_instructions.py）
// ⤴️a2ui 立项：Go 侧判定函数未实现，本批保守返回 false（不注入浏览器工作流）。
func (r *ResponsePromptRail) isBrowserWorkflowQuery(cbc *agentinterfaces.AgentCallbackContext) bool {
	return false
}

// syncA2UIPromptSection 注入或移除 a2ui 提示节。
// Python: _sync_a2ui_prompt_section (response_prompt_rail.py:199-260)。
// ⤴️a2ui 立项：is_a2ui_channel/get_current_a2ui_config/build_a2ui_prompt_section
// 基座（server/runtime/a2ui，2030 行）Go 全缺——本批保留清除与日志语义，
// 注入分支随 a2ui 域回填。
func (r *ResponsePromptRail) syncA2UIPromptSection(cbc *agentinterfaces.AgentCallbackContext, channel string, skipA2UI, includeBrowserWorkflows bool, requestID, conversationID string) {
	if r.builder == nil {
		return
	}
	// 基座缺失 → enabled=false → injected=false → 清除（与 Python enabled=false 分支一致）
	r.builder.RemoveSection(prompt.LocalSectionNameA2UI)
	logger.Info(responseRailLogComponent).
		Str("request_id", orString(requestID, "-")).
		Str("conversation_id", orString(conversationID, "-")).
		Str("channel", orString(channel, "-")).
		Bool("enabled", false).
		Bool("injected", false).
		Bool("browser_workflows", false).
		Bool("skip_a2ui", skipA2UI).
		Msg("A2UI prompt decision")
}

// sendFileEnabledFallback 经配置缝读取渠道投递开关；异常时按 Python 语义保持通道缺省 true。
func (r *ResponsePromptRail) sendFileEnabledFallback(channel string) bool {
	if r.sendFileConfigProvider == nil {
		return true
	}
	defer func() {
		// Python: except Exception → logger.exception + 保持 fallback
		_ = recover()
	}()
	return tools.IsSendFileEnabled(r.sendFileConfigProvider(), channel)
}

// toString 对齐 Python str(value)。
func toString(v any) string {
	return strings.TrimSpace(fmt.Sprint(v))
}

// orString 返回第一个非空字符串。
func orString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
