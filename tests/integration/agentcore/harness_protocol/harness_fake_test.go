//go:build integration

package harness_protocol

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	agentteamsharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	basesuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// fakeCLIProvider 扮演外部 CLI provider（进程边界角色）。
//
// 它实现 internal/agent_teams/harness.HarnessProtocol（L2 契约），
// 用真实的 *session.Session 作为会话载体，并在「工具执行前」通过 15.3 的
// HarnessInteractionHandler 向宿主发起 ToolApprovalRequest —— 这正是
// Python harness_providers/base.py:724-739 _request_interaction 的行为。
//
// 它不做的事（避免冒充）：不 mock session、不 mock agent_teams/harness、
// 不替换 15.3 的任何实现。
type fakeCLIProvider struct {
	mu      sync.Mutex
	state   harness_protocol.HarnessState
	sess    *session.Session
	handler harness_protocol.HarnessInteractionHandler
	hooks   harness_protocol.HarnessHookDispatcher
	// toolCalls 记录已获批准的调用
	toolCalls []string
	// pendingIDs 记录发起过的 request_id 序列
	pendingIDs []string
}

// State 返回当前相位（L2 契约方法）。
func (p *fakeCLIProvider) State() harness_protocol.HarnessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// SessionID 返回会话标识（L2 契约方法）。
func (p *fakeCLIProvider) SessionID() string {
	if p.sess == nil {
		return ""
	}
	return p.sess.GetSessionID()
}

// Start 绑定真实 session 并进入 running（L2 契约方法）。
func (p *fakeCLIProvider) Start(_ context.Context, sess *session.Session) error {
	if sess == nil {
		return errors.New("session 不能为 nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sess = sess
	p.state = harness_protocol.HarnessStateRunning
	return nil
}

// Stop 进入 terminated（L2 契约方法）。
func (p *fakeCLIProvider) Stop(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = harness_protocol.HarnessStateTerminated
	return nil
}

// Outputs 返回输出迭代器（L2 契约方法；本 fake 不产输出）。
func (p *fakeCLIProvider) Outputs() *agentteamsharness.OutputIterator { return nil }

// Send 模拟一轮执行：先用 hooks.BeforePrompt，再对每个工具
// 走 hooks.BeforeTool → interactions.Handle(审批) → tools 执行 → hooks.AfterTool。
func (p *fakeCLIProvider) Send(ctx context.Context, content any, _ bool) error {
	prompt, _ := content.(string)

	// 边界 1：before_prompt
	if p.hooks != nil {
		result, err := p.hooks.BeforePrompt(ctx, harness_protocol.NewBeforePromptContext(
			"member-a", nil, ptr("turn-1"), prompt))
		if err != nil {
			return err
		}
		if !result.ContinueExecution {
			return nil // 宿主策略掐掉了本轮
		}
		prompt = result.Prompt
	}

	tools := splitTools(prompt)
	for _, tool := range tools {
		callID := "call-" + tool
		args := harness_protocol.JsonObject{"command": tool}
		var result any = "ok"

		// 边界 2：before_tool
		if p.hooks != nil {
			decision, err := p.hooks.BeforeTool(ctx, harness_protocol.BeforeToolContext{
				AgentName: "member-a", TurnID: ptr("turn-1"),
				CallID: callID, ToolName: tool, Arguments: args,
			})
			if err != nil {
				return err
			}
			switch decision.Decision {
			case harness_protocol.ToolDecisionKindDeny:
				continue // 宿主拒绝该工具
			case harness_protocol.ToolDecisionKindRewrite:
				args = decision.UpdatedArguments
			case harness_protocol.ToolDecisionKindAsk:
				// 转交交互审批，落到下面的 Handle 分支
			}
		}

		// 交互面：工具审批
		if p.handler != nil {
			requestID := "approval-" + callID
			req, err := harness_protocol.NewToolApprovalRequest(requestID, callID, tool,
				harness_protocol.ToolApprovalRequestOptions{
					Arguments: args,
					TurnID:    ptr("turn-1"),
				})
			if err != nil {
				return err
			}
			p.mu.Lock()
			p.pendingIDs = append(p.pendingIDs, requestID)
			p.mu.Unlock()

			resp, err := p.handler.Handle(ctx, req)
			if err != nil {
				return err
			}
			validated, err := harness_protocol.ValidateInteractionResponse(req, resp)
			if err != nil {
				return err
			}
			approval, ok := validated.(harness_protocol.ToolApprovalResponse)
			if !ok {
				if ap, okPtr := validated.(*harness_protocol.ToolApprovalResponse); okPtr {
					approval = *ap
				} else {
					return errors.New("回灌应答类型不符")
				}
			}
			if approval.Decision == harness_protocol.ToolApprovalDecisionDeny ||
				approval.Decision == harness_protocol.ToolApprovalDecisionAbort {
				continue // 宿主拒绝，工具不执行
			}
			if approval.UpdatedArguments != nil {
				args = approval.UpdatedArguments
			}
		}

		// 边界 3：after_tool（真实执行后的结果回灌）
		if p.hooks != nil {
			hookResult, err := p.hooks.AfterTool(ctx, harness_protocol.AfterToolContext{
				AgentName: "member-a", TurnID: ptr("turn-1"),
				CallID: callID, ToolName: tool, Arguments: args,
				Result: result,
			})
			if err != nil {
				return err
			}
			if hookResult.ReplaceResult {
				result = hookResult.UpdatedResult
			}
		}

		p.mu.Lock()
		p.toolCalls = append(p.toolCalls, tool)
		p.mu.Unlock()
		_ = result
	}

	// 边界 4：on_stop
	if p.hooks != nil {
		if err := p.hooks.OnStop(ctx, harness_protocol.StopHookContext{
			AgentName: "member-a", TurnID: ptr("turn-1"),
			Metadata: harness_protocol.JsonObject{"reason": "turn_done"},
		}); err != nil {
			return err
		}
	}
	return nil
}

// Abort 回到 idle（L2 契约方法）。
func (p *fakeCLIProvider) Abort(_ context.Context, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = harness_protocol.HarnessStateIdle
	return nil
}

// Pause 进入 paused（L2 契约方法）。
func (p *fakeCLIProvider) Pause(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = harness_protocol.HarnessStatePaused
	return nil
}

// Resume 回到 running（L2 契约方法）。
func (p *fakeCLIProvider) Resume(_ context.Context, _ any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = harness_protocol.HarnessStateRunning
	return nil
}

// Subscribe 空实现（本 fake 不产生命周期事件）。
func (p *fakeCLIProvider) Subscribe(
	_ context.Context,
	_ agentteamsharness.StateHandler,
	_ agentteamsharness.RoundHandler,
) error {
	return nil
}

// 编译期断言：fake 满足 L2 契约（若接口变化此处编译失败）
var _ agentteamsharness.HarnessProtocol = (*fakeCLIProvider)(nil)

// 编译期断言：两个 15.3 接口可被真实类型实现
var _ harness_protocol.HarnessInteractionHandler = (*recordingHandler)(nil)
var _ harness_protocol.HarnessHookDispatcher = (*recordingHooks)(nil)

// recordingHandler 记录 15.3 交互应答的宿主实现（扮演宿主策略层）。
type recordingHandler struct {
	mu sync.Mutex
	// decision 固定返回的审批决策
	decision harness_protocol.ToolApprovalDecision
	// cancelLog 记录 (requestID, reason)
	cancelLog []cancelRecord
	// handleCount 记录 Handle 调用次数
	handleCount int
}

// cancelRecord 一次 cancel 调用。
type cancelRecord struct {
	RequestID string
	Reason    harness_protocol.InteractionCancelReason
}

// Handle 按固定决策应答，并走 ValidateInteractionResponse 自校验。
func (h *recordingHandler) Handle(_ context.Context, request harness_protocol.HarnessInteractionRequest) (harness_protocol.HarnessInteractionResponse, error) {
	h.mu.Lock()
	h.handleCount++
	decision := h.decision
	h.mu.Unlock()

	switch r := request.(type) {
	case harness_protocol.ToolApprovalRequest:
		resp, err := harness_protocol.NewToolApprovalResponse(r.RequestID, decision,
			harness_protocol.ToolApprovalResponseOptions{})
		if err != nil {
			return nil, err
		}
		return harness_protocol.ValidateInteractionResponse(request, resp)
	case *harness_protocol.ToolApprovalRequest:
		resp, err := harness_protocol.NewToolApprovalResponse(r.RequestID, decision,
			harness_protocol.ToolApprovalResponseOptions{})
		if err != nil {
			return nil, err
		}
		return harness_protocol.ValidateInteractionResponse(request, resp)
	default:
		return nil, errors.New("unsupported request type in recordingHandler")
	}
}

// Cancel 记录取消原因（幂等：重复调用不改变结果）。
func (h *recordingHandler) Cancel(_ context.Context, requestID string, opts ...harness_protocol.CancelOption) error {
	reason := harness_protocol.InteractionCancelReasonProviderWithdrew
	for _, opt := range opts {
		if opt != nil {
			opt(&reason)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelLog = append(h.cancelLog, cancelRecord{RequestID: requestID, Reason: reason})
	return nil
}

// recordingHooks 记录 15.3 钩子调用的宿主实现。
type recordingHooks struct {
	mu sync.Mutex
	// beforePromptCalls / beforeToolCalls / afterToolCalls / onStopCalls 计数
	beforePromptCalls int
	beforeToolCalls   int
	afterToolCalls    int
	onStopCalls       int
	// denyTool 非空时该工具被 DENY
	denyTool string
	// rewriteTo 非空时 before_tool 返回 REWRITE 到该命令
	rewriteTo string
	// replaceResultWith 非 nil 时 after_tool 返回 replace_result=true
	replaceResultWith any
	// observedTurnIDs 记录每次钩子拿到的 turn_id（验证身份透传）
	observedTurnIDs []string
}

// BeforePrompt 记录调用。
func (h *recordingHooks) BeforePrompt(_ context.Context, hookCtx harness_protocol.BeforePromptContext) (harness_protocol.BeforePromptResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.beforePromptCalls++
	h.observedTurnIDs = append(h.observedTurnIDs, strOrEmpty(hookCtx.TurnID))
	return harness_protocol.NewBeforePromptResult(hookCtx.Prompt, harness_protocol.BeforePromptResultOptions{})
}

// BeforeTool 记录调用并给出允许/拒绝/改写决策。
func (h *recordingHooks) BeforeTool(_ context.Context, hookCtx harness_protocol.BeforeToolContext) (harness_protocol.ToolDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.beforeToolCalls++
	h.observedTurnIDs = append(h.observedTurnIDs, strOrEmpty(hookCtx.TurnID))
	if hookCtx.ToolName == h.denyTool {
		return harness_protocol.ToolDecision{Decision: harness_protocol.ToolDecisionKindDeny}, nil
	}
	if h.rewriteTo != "" {
		return harness_protocol.NewToolDecision(harness_protocol.ToolDecisionKindRewrite,
			harness_protocol.ToolDecisionOptions{
				UpdatedArguments: harness_protocol.JsonObject{"command": h.rewriteTo},
			})
	}
	return harness_protocol.ToolDecision{Decision: harness_protocol.ToolDecisionKindAllow}, nil
}

// AfterTool 记录调用并可选替换结果。
func (h *recordingHooks) AfterTool(_ context.Context, hookCtx harness_protocol.AfterToolContext) (harness_protocol.AfterToolResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.afterToolCalls++
	h.observedTurnIDs = append(h.observedTurnIDs, strOrEmpty(hookCtx.TurnID))
	if h.replaceResultWith != nil {
		return harness_protocol.NewAfterToolResult(harness_protocol.AfterToolResultOptions{
			ReplaceResult: true,
			UpdatedResult: h.replaceResultWith,
		})
	}
	return harness_protocol.NewAfterToolResult(harness_protocol.AfterToolResultOptions{})
}

// OnStop 记录调用。
func (h *recordingHooks) OnStop(_ context.Context, _ harness_protocol.StopHookContext) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onStopCalls++
	return nil
}

// ──────────────────────────── 测试助手 ────────────────────────────

// ptr 返回字符串指针。
func ptr(s string) *string { return &s }

// strOrEmpty 解引用或返回空串。
func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// splitTools 把逗号分隔的工具名拆开（空串返回 nil）。
func splitTools(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// newRealSession 用真实 session 子系统创建一个可用的会话。
// 这条调用是本目录跨子系统的真实证据之一（internal/agentcore/session）。
func newRealSession(t *testing.T, sessionID string) *session.Session {
	t.Helper()
	sess := session.CreateAgentSession(sessionID, nil, nil)
	if sess == nil {
		t.Fatal("CreateAgentSession 返回 nil")
	}
	if err := sess.PreRun(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("PreRun: %v", err)
	}
	return sess
}

// newInteractiveInput 用真实 interaction 子系统构造应答载体。
// 这是第二个真实子系统的证据（internal/agentcore/session/interaction，
// 按审计口径归并入 agentcore/session 子系统）。
func newInteractiveInput(t *testing.T, raw any) *sessioninteraction.InteractiveInput {
	t.Helper()
	input, err := sessioninteraction.NewInteractiveInput(raw)
	if err != nil {
		t.Fatalf("NewInteractiveInput: %v", err)
	}
	return input
}

// ProviderDriverSuite 驱动端 fake 的自检套件。
//
// 本文件主要是 fake 实现（无被测断言），但审计规则要求每个 integration
// 目录下的 _test.go 都自带 suite.Run；因此在此放一个自检套件，验证
// fakeCLIProvider 确实满足 L2 契约、并能驱动 15.3 的两个控制面接口。
type ProviderDriverSuite struct {
	basesuite.BaseIntegrationSuite
}

// TestProviderDriverSuite 套件入口。
func TestProviderDriverSuite(t *testing.T) {
	suite.Run(t, new(ProviderDriverSuite))
}

// TestFakeProvider_满足L2契约并可驱动控制面 fake 自身的可运转性自检。
func (s *ProviderDriverSuite) TestFakeProvider_满足L2契约并可驱动控制面() {
	sess := newRealSession(s.T(), "sess-driver-selfcheck")
	hooks := &recordingHooks{}
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{hooks: hooks, handler: handler}

	s.Require().NoError(provider.Start(context.Background(), sess))
	defer func() { _ = provider.Stop(context.Background()) }()

	s.Equal(harness_protocol.HarnessStateRunning, provider.State())
	s.Equal("sess-driver-selfcheck", provider.SessionID())
	// L2 契约的 10 个方法均可调用
	s.Nil(provider.Outputs())
	s.NoError(provider.Pause(context.Background()))
	s.Equal(harness_protocol.HarnessStatePaused, provider.State())
	s.NoError(provider.Resume(context.Background(), nil))
	s.Equal(harness_protocol.HarnessStateRunning, provider.State())
	s.NoError(provider.Abort(context.Background(), false))
	s.Equal(harness_protocol.HarnessStateIdle, provider.State())
	s.NoError(provider.Subscribe(context.Background(), nil, nil))

	// 控制面真的被驱动
	s.NoError(provider.Send(context.Background(), "shell", false))
	s.Equal(1, hooks.beforePromptCalls)
	s.Equal(1, hooks.beforeToolCalls)
	s.Equal(1, handler.handleCount)
}
