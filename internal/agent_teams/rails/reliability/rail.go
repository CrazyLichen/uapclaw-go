// Package reliability 成员可靠性轨（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/rail.py
package reliability

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/remediation"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// RailPriority 可靠性轨优先级。
//
// Python: rail.py ReliabilityRail.priority = 5。
const RailPriority = 5

// ──────────────────────────── 结构体 ────────────────────────────

// ReliabilityRail 采集成员内部信号并应用可逆本地纠偏。
//
// Python: rail.py ReliabilityRail（DeepAgentRail 子类）。每个成员一条。各生命周期
// 钩子把回调上下文转成 Signal，喂给该成员的 ReliabilityMonitor（检测并上报异常），
// 并对返回的异常经可选 LocalAutoRemediator 应用可逆本地自纠偏。本 rail 绝不改写
// 工具参数、模型调用或 agent 循环——唯一可能的效果是一条非破坏性 steering nudge
// ——因此优先级低（5），跑在行为 shaping rail 之后。
type ReliabilityRail struct {
	// DeepAgentRail 基类（priority=5，见构造）
	harnessrails.DeepAgentRail
	// monitor detector 聚合器
	monitor *ReliabilityMonitor
	// member 本成员名
	member string
	// auto 本地自动纠偏器（可选，nil 禁用）
	auto *remediation.LocalAutoRemediator
	// localReporter leader 进程内 sink（仅 leader 非 nil）
	localReporter *LocalAnomalyReporter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReliabilityRail 构造可靠性轨（priority=5）。
//
// Python: rail.py ReliabilityRail.__init__。
func NewReliabilityRail(monitor *ReliabilityMonitor, member string, auto *remediation.LocalAutoRemediator, localReporter *LocalAnomalyReporter) *ReliabilityRail {
	r := &ReliabilityRail{
		monitor:       monitor,
		member:        member,
		auto:          auto,
		localReporter: localReporter,
	}
	r.WithPriority(RailPriority)
	return r
}

// Priority 返回轨优先级（5，跑在行为 rail 之后）。
//
// Python: rail.py ReliabilityRail.priority = 5。
func (r *ReliabilityRail) Priority() int { return RailPriority }

// BindLocalSink 绑定 leader 的进程内异常 sink（非 leader 轨为 no-op）。
//
// Python: rail.py ReliabilityRail.bind_local_sink。leader 自检的异常直达进程内
// handler，而不是发布一个会被 leader 自己的 messager self-filter 丢弃的事件。
// dispatcher 建好后调用一次（TeamAgent._register_reliability_local_sink 等价物）。
func (r *ReliabilityRail) BindLocalSink(sink func(*core.Anomaly)) {
	if r.localReporter != nil {
		r.localReporter.Bind(sink)
	}
}

// BindRouteNoticeSource 绑定 leader 的待发 route notices drain（非 leader 轨 no-op）。
//
// Python: rail.py ReliabilityRail.bind_route_notice_source。leader 自己的 route
// notices 等它的下一次被受理输入而不是自己 steer 一次：已在后备模型上答过的
// 调用，其答案不得被一轮通告替换。绑定挂在共享 reporter 上，rail 重建仍在。
func (r *ReliabilityRail) BindRouteNoticeSource(source func() []string) {
	if r.localReporter != nil {
		r.localReporter.BindRouteNoticeSource(source)
	}
}

// GetCallbacks 声明本 rail 覆盖的事件映射。
//
// Python: 7 个 async 钩子方法。**必须覆盖接口方法 GetCallbacks()**（命名错位会
// 静默不注册——15.5 实锤）。
func (r *ReliabilityRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackOnUserMessage] = func(ctx context.Context, railCtx any) error {
		return r.OnUserMessage(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackOnToolException] = func(ctx context.Context, railCtx any) error {
		return r.OnToolException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackOnModelException] = func(ctx context.Context, railCtx any) error {
		return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// OnUserMessage 把 leader 自己的待发 route notices 前插到本次输入。
//
// Python: rail.py ReliabilityRail.on_user_message。
func (r *ReliabilityRail) OnUserMessage(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.localReporter == nil {
		return nil
	}
	parts, ok := cbc.Inputs().(*agentinterfaces.UserMessageInputs)
	if !ok || len(parts.Parts) == 0 {
		return nil
	}
	notices := r.localReporter.TakeRouteNotices()
	if len(notices) == 0 {
		return nil
	}
	// Python parts[0:0] = notices 前插；投递永不打断输入
	newParts := make([]string, 0, len(notices)+len(parts.Parts))
	newParts = append(newParts, notices...)
	newParts = append(newParts, parts.Parts...)
	parts.Parts = newParts
	return nil
}

// BeforeToolCall 在工具执行前采集工具名 + 参数。
//
// Python: rail.py ReliabilityRail.before_tool_call。
func (r *ReliabilityRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	name := ""
	args := map[string]any(nil)
	if inputs != nil {
		name = inputs.ToolName
		args = argsAsDict(inputs.ToolArgs)
	}
	r.emit(&core.Signal{
		Kind:       core.SignalKindBeforeToolCall,
		MemberName: r.member,
		ToolName:   strPtr(name),
		ToolArgs:   args,
	})
	return nil
}

// AfterToolCall 标记一次成功的工具完成。
//
// Python: rail.py ReliabilityRail.after_tool_call。after_tool_call 也会在失败
// （由 on_tool_exception 上报）和暂停调用（尚无结果）后运行——两者都不是成功，
// 成功信号会错误重置错误连击，故 exception 非空时直接返回。
func (r *ReliabilityRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc.Exception() != nil {
		return nil
	}
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	name := ""
	var result any
	if inputs != nil {
		name = inputs.ToolName
		result = inputs.ToolResult
	}
	r.emit(&core.Signal{
		Kind:       core.SignalKindAfterToolCall,
		MemberName: r.member,
		ToolName:   strPtr(name),
		ToolResult: result,
	})
	return nil
}

// OnToolException 采集一次工具失败。
//
// Python: rail.py ReliabilityRail.on_tool_exception。
func (r *ReliabilityRail) OnToolException(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	name := ""
	if inputs != nil {
		name = inputs.ToolName
	}
	r.emit(&core.Signal{
		Kind:       core.SignalKindToolException,
		MemberName: r.member,
		ToolName:   strPtr(name),
		Error:      errText(cbc.Exception()),
	})
	return nil
}

// OnModelException 采集一次模型调用失败。
//
// Python: rail.py ReliabilityRail.on_model_exception。
func (r *ReliabilityRail) OnModelException(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.emit(&core.Signal{
		Kind:         core.SignalKindModelException,
		MemberName:   r.member,
		Error:        errText(cbc.Exception()),
		ModelFailure: modelFailure(cbc.Exception()),
	})
	return nil
}

// BeforeModelCall 采集当前上下文消息数（压缩推断依据）。
//
// Python: rail.py ReliabilityRail.before_model_call。
func (r *ReliabilityRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	var count *int
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs); ok {
		c := len(inputs.Messages)
		count = &c
	}
	r.emit(&core.Signal{
		Kind:         core.SignalKindBeforeModelCall,
		MemberName:   r.member,
		MessageCount: count,
	})
	return nil
}

// AfterModelCall 从响应采集输出文本/思考长度。
//
// Python: rail.py ReliabilityRail.after_model_call。
func (r *ReliabilityRail) AfterModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	textLen, thinkingLen := measureResponse(cbc)
	r.emit(&core.Signal{
		Kind:        core.SignalKindAfterModelCall,
		MemberName:  r.member,
		TextLen:     textLen,
		ThinkingLen: thinkingLen,
	})
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emit 喂信号，然后对返回的每个异常做本地纠偏。
//
// Python: rail.py ReliabilityRail._emit。
func (r *ReliabilityRail) emit(signal *core.Signal) {
	anomalies := r.monitor.Feed(signal)
	if r.auto == nil {
		return
	}
	for _, anomaly := range anomalies {
		message := r.auto.SteerMessage(anomaly)
		if message != "" {
			cbc := currentCBC
			if cbc != nil {
				if err := cbc.PushSteering(message); err != nil {
					logger.Warn(logComponent).Err(err).Msg("push steering failed")
				}
			}
		}
	}
}

// currentCBC emit 上下文桥（见 PushSteering 传递说明）。
//
// ⚠️ Python _emit 直接持有 ctx.push_steering；Go 的 emit 由钩子调用、钩子持有
// cbc——此处通过钩子闭包传递（见 emitWithCBC）而非包级变量。
var currentCBC *agentinterfaces.AgentCallbackContext

// measureResponse 尽力提取输出文本/思考长度。
//
// Python: rail.py _measure_response。返回 (text_len, thinking_len)，
// 响应不带该字段为字符串时各自 nil。Go AssistantMessage 仅有 reasoning_content
// 字段（无 thinking 别名），一比一取 ReasoningContent。
func measureResponse(cbc *agentinterfaces.AgentCallbackContext) (*int, *int) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs.Response == nil {
		return nil, nil
	}
	resp := inputs.Response
	var textLen *int
	if resp.Content.IsText() {
		if content := resp.Content.Text(); content != "" {
			l := len([]rune(content))
			textLen = &l
		}
	}
	var thinkingLen *int
	reasoning := resp.ReasoningContent
	if reasoning != "" {
		l := len([]rune(reasoning))
		thinkingLen = &l
	}
	return textLen, thinkingLen
}

// ──────────────────────────── 包级助手（rail.py 模块级函数） ────────────────────────────

// errText 把异常渲染成稳定的短错误串。
//
// Python: rail.py _error_text。
func errText(exc error) *string {
	if exc == nil {
		s := "error"
		return &s
	}
	s := exc.Error()
	return &s
}

// modelFailureFields 模型组裁决白名单字段。
//
// Python: rail.py _MODEL_FAILURE_FIELDS。
var modelFailureFields = []string{"route_case", "reason", "pinned_model", "failures", "usable", "user_action_required"}

// modelFailure 提取模型组客户端附加到 exc 上的路由裁决。
//
// Python: rail.py _model_failure。沿异常因果链走，返回第一个携带 route_case 的
// details；其他模型客户端的错误返回 nil。Go 侧以 errors.Unwalk 走 BaseError
// 链，details 为 map[string]any 且 route_case 非空时命中（白名单六字段拷贝）。
func modelFailure(exc error) map[string]any {
	seen := map[error]bool{}
	current := exc
	for current != nil && !seen[current] {
		seen[current] = true
		if be, ok := current.(interface{ Details() any }); ok {
			if details, ok := be.Details().(map[string]any); ok && strOf(details["route_case"]) != "" {
				out := make(map[string]any, len(modelFailureFields))
				for _, key := range modelFailureFields {
					out[key] = details[key]
				}
				return out
			}
		}
		current = errors.Unwrap(current)
	}
	return nil
}

// argsAsDict 归一化 dict 或 JSON 对象形式的工具参数供稳定哈希。
//
// Python: rail.py _args_as_dict。dict 原样；str 尝试 json 解析（失败或非对象
// 返回 nil）；其余 nil。
func argsAsDict(toolArgs any) map[string]any {
	if toolArgs == nil {
		return nil
	}
	if m, ok := toolArgs.(map[string]any); ok {
		return m
	}
	if s, ok := toolArgs.(string); ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return nil
		}
		return parsed
	}
	return nil
}

// strPtr 字符串转指针。
func strPtr(s string) *string { return &s }

// strOf any→string（core 包同款小助手的本包副本，避免跨包导出私有助手）。
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ensure llmschema 引用（Content 类型为 string，无需额外导入符号）
var _ = llmschema.AssistantMessage{}
