// 补充输入 SDK 适配面（会话输入条目/同步入队/两守卫 rail/账本）。
// 对应上游 jiuwenswarm/server/runtime/agent_adapter/session_input.py
// （581 行；runtime/session_input.py 的 gateway 副本在 internal/swarm/runtime/session_input.go）。
package adapter

import (
	"encoding/json"
	"strings"

	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QueuedSessionInput 已绑定活跃轮的补充输入队列条目（别名）。
// 本体定义在 harness/schema/interaction.go（SDK controller 与 BoundaryGuard
// 直接消费该条目，Go 包边界要求类型居于两者共同依赖侧；归属偏差 D-25.6-s1）。
type QueuedSessionInput = hschema.QueuedSessionInput

// NewQueuedSessionInput 构造边界信号就绪的条目（别名转发）。
func NewQueuedSessionInput(query, requestID string) *QueuedSessionInput {
	return hschema.NewQueuedSessionInput(query, requestID)
}

// SteerTarget 补充输入投递目标实例的最小能力面。
// Python 为鸭子类型直取 instance.active_round / has_output_stream /
// loop_controller.enqueue_steer；Go 依赖倒置用窄接口承载（规则 10.2：
// 本接口是"DeepAgent 用别人"方向的反例——它是宿主调 agent 的能力，
// 但 EnqueueBoundSessionInput 属宿主侧函数，注入窄接口避免 runtime 包
// 反向依赖 harness）。
type SteerTarget interface {
	// ActiveRoundIs 报告实例当前活跃轮是否仍为 bound（同一轮记账对象）
	ActiveRoundIs(bound any) bool
	// HasOutputStream 报告输出租约是否有消费者
	HasOutputStream() bool
	// EnqueueSteerEntry 同步入队 steering 显示文本（与校验同临界区调用）
	EnqueueSteerEntry(display string) error
}

// EnqueueBoundOptions EnqueueBoundSessionInput 的调用参数。
type EnqueueBoundOptions struct {
	// Instance 投递目标实例
	Instance SteerTarget
	// BoundRound 准入时捕获的活跃轮（不得为 nil）
	BoundRound any
	// Request 原始请求（params 取 expected_execution_id/content/query/跨会话键）
	Request *schema.AgentRequest
	// Query 已解析的 sdk_request.inputs["query"] 文本
	Query string
	// MediaItems steer 图片引用列表（nil=纯文本）
	MediaItems []map[string]any
	// ExecutionLookup 按 execution_id 查账本（nil 时跳过执行复核）
	ExecutionLookup func(executionID string) *sessionpkg.SessionExecutionHandle
}

// EnqueueBoundSessionInput 同步校验并入队活跃轮的 steering queue。
// Python: session_input.py enqueue_bound_session_input（:102-160）
//
// 铁律：目标校验与入队之间不得插入任何可让渡点（含锁获取），否则目标轮
// 可能在间隙结束并错开新轮——调用方保证 Instance 三个方法在同一临界区
// 完成（DeepAgent 的 interactionControlMu 语义）。
// 拒收即错误上抛：入队尚未发生，前端拿到的失败就是"什么都没发"。
func EnqueueBoundSessionInput(opts EnqueueBoundOptions) (*QueuedSessionInput, error) {
	params := requestParamsOf(opts.Request)
	requestID := requestIDOf(opts.Request)

	// ① execution 复核：expected_execution_id 非空时查账本
	// Python: execution = runtime.get_session_execution(expected_id) if expected_id else None
	expectedID, _ := params["expected_execution_id"].(string)
	if opts.ExecutionLookup != nil && expectedID != "" {
		execution := opts.ExecutionLookup(expectedID)
		if execution == nil {
			return nil, &sessionpkg.SessionInputTargetError{
				Message: "the targeted execution has ended or changed; supplemental input was not sent",
			}
		}
		if execution.SessionID != sessionIDOf(opts.Request) ||
			execution.State != sessionpkg.SessionExecutionStateRunning ||
			execution.CancellationRequested {
			return nil, &sessionpkg.SessionInputTargetError{
				Message: "the targeted execution has ended or changed; supplemental input was not sent",
			}
		}
	}

	// ② bound_round 复核：目标轮必须仍是准入时捕获的那一轮
	// Python: instance.active_round is not target_round or target_round is None
	//         or not instance.has_output_stream()
	if opts.BoundRound == nil || !opts.Instance.ActiveRoundIs(opts.BoundRound) ||
		!opts.Instance.HasOutputStream() {
		return nil, &sessionpkg.SessionInputTargetError{
			Message: "the targeted execution has ended or changed; supplemental input was not sent",
		}
	}

	// ③ 构造条目：display_content 优先 content，回落 query
	// Python: content = params.get("content") or params.get("query") or entry
	display := ""
	if text, ok := params["content"].(string); ok {
		display = text
	} else if text, ok := params["query"].(string); ok {
		display = text
	}
	if display == "" {
		display = opts.Query
	}
	entry := NewQueuedSessionInput(opts.Query, requestID)
	entry.DisplayContent = display
	entry.MediaItems = opts.MediaItems

	// ④ 跨会话键：entry.cross_session + message_route 五键
	// Python: cross_session = request.params.get(SESSION_MESSAGE_INTERNAL_KEY)
	if cross, ok := params[sessionMessageKey].(map[string]any); ok {
		entry.CrossSession = make(map[string]any, len(cross)+1)
		for k, v := range cross {
			entry.CrossSession[k] = v
		}
		entry.CrossSession["content"] = entry.DisplayContent
		route := map[string]any{
			"session_id":        sessionIDOf(opts.Request),
			"request_id":        requestID,
			"user_id":           params["user_id"],
			"chain_id":          cross["chain_id"],
			"parent_message_id": cross["message_id"],
			"hop_count":         cross["hop_count"],
		}
		if route["chain_id"] == nil {
			route["chain_id"] = ""
		}
		if route["parent_message_id"] == nil {
			route["parent_message_id"] = ""
		}
		if route["hop_count"] == nil {
			route["hop_count"] = 0
		}
		entry.MessageRoute = route
	}

	// ⑤ 同步入队 steering（display_content 字符串；结构化条目由调用方
	// 挂进 inputs.parts 供 BoundaryGuard 过滤——Python duck-typing 等价）
	// Python: controller.enqueue_steer(entry)
	if err := opts.Instance.EnqueueSteerEntry(entry.DisplayContent); err != nil {
		return nil, err
	}
	return entry, nil
}

// ──────────────────────────── 以下为 agent_adapter/session_input.py 的两守卫 rail 与账本 ────────────────────────────

// SessionInputBoundaryGuard 在记忆等输入观察者之前剔除失败的发布。
// Python: session_input.py SessionInputBoundaryGuard（priority=1000）
//
// Go 偏差 D-25.6-s3：steering 队列元素是 string（Python 是对象 part），
// 失败条目在入队前已被 fail-fast 拦截（永不入队），本 rail 作为防线
// 依 Ledger 逐条核验 display 文本对应的条目状态：失败的 part 被剔除，
// 其余按原顺序放行。
type SessionInputBoundaryGuard struct {
	agentinterfaces.BaseRail
	// ledger 在途条目账本（deliver 链入队时登记，边界收尾后清理）
	ledger *SessionInputLedger
}

// sessionMessageKey 跨会话消息内部参数键（runtime 侧常量的 adapter 侧别名；
// adapter 不能 import swarm/runtime，常量值逐字对齐 _jiuwenswarm_cross_session）。
const sessionMessageKey = "_jiuwenswarm_cross_session"

// requestParamsOf 取请求 params（nil 安全；Params 是延迟解析的 RawMessage）。
// Python: request.params 直接字典访问。
// [Go 扩展] swarm/runtime 已有 parseRequestParams，但 server/adapter 直接
// import swarm/runtime 会成环（runtime→server/runtime→adapter），此处内联
// 同语义实现（nil/空/坏 JSON → 空表）。
func requestParamsOf(req *schema.AgentRequest) map[string]any {
	if req == nil || len(req.Params) == 0 {
		return map[string]any{}
	}
	params := map[string]any{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return map[string]any{}
	}
	return params
}

// requestIDOf 取请求信封 id（nil 安全）。
func requestIDOf(req *schema.AgentRequest) string {
	if req == nil {
		return ""
	}
	return req.RequestID
}

// sessionIDOf 取请求会话标识（nil 安全）。
func sessionIDOf(req *schema.AgentRequest) string {
	if req == nil || req.SessionID == nil {
		return ""
	}
	return strings.TrimSpace(*req.SessionID)
}

// NewSessionInputBoundaryGuard 创建边界守卫（默认优先级 1000）。
func NewSessionInputBoundaryGuard(ledger *SessionInputLedger) *SessionInputBoundaryGuard {
	return &SessionInputBoundaryGuard{BaseRail: *agentinterfaces.NewBaseRail(), ledger: ledger}
}

// Priority 返回 1000（先于一切默认 rail 执行）。
// Python: SessionInputBoundaryGuard.priority = 1000
func (g *SessionInputBoundaryGuard) Priority() int { return 1000 }

// OnUserMessage 等待每条在途条目的边界结果并剔除失败项。
// Python: SessionInputBoundaryGuard.on_user_message —— admitted 保持原顺序。
func (g *SessionInputBoundaryGuard) OnUserMessage(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.UserMessageInputs)
	if !ok || g.ledger == nil {
		return nil
	}
	admitted := make([]string, 0, len(inputs.Parts))
	for _, part := range inputs.Parts {
		if entry := g.ledger.Lookup(part); entry != nil {
			<-entry.BoundaryReady // 等待边界结果
			if entry.BoundaryError != nil {
				continue
			}
		}
		admitted = append(admitted, part)
	}
	inputs.Parts = admitted
	return nil
}

// GetCallbacks 声明 ON_USER_MESSAGE 钩子覆盖。
func (g *SessionInputBoundaryGuard) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return g.BaseRail.BuildCallbacks(g.BaseRail.CallbackFrom(
		agentinterfaces.CallbackOnUserMessage,
		func(ctx context.Context, railCtx any) error {
			return g.OnUserMessage(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		},
	))
}
