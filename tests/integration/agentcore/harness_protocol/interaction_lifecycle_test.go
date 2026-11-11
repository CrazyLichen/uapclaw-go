//go:build integration

package harness_protocol

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	basesuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// InteractionLifecycleSuite 交互请求-应答全生命周期集成套件。
// 跨 agentcore/harness_protocol（被测契约）+ agentcore/session（真实会话）
// + agent_teams/harness（L2 契约）三个子系统。
type InteractionLifecycleSuite struct {
	basesuite.BaseIntegrationSuite
}

// TestInteractionLifecycleSuite 套件入口。
func TestInteractionLifecycleSuite(t *testing.T) {
	suite.Run(t, new(InteractionLifecycleSuite))
}

// TestInteractionLifecycle_审批通过后工具执行 端到端：真实 session + 真实 L2 契约 +
// 15.3 交互应答 → 工具被放行
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_审批通过后工具执行() {
	sess := newRealSession(s.T(), "sess-approval-allow")
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{handler: handler}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	s.Require().Equal(harness_protocol.HarnessStateRunning, provider.State(), "Start 后应为 running")
	if provider.SessionID() != "sess-approval-allow" {
		s.Fail("SessionID = %q，期望 sess-approval-allow", provider.SessionID())
	}
	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}

	if handler.handleCount != 1 {
		s.Fail("handleCount = %d，期望 1", handler.handleCount)
	}
	if len(provider.toolCalls) != 1 || provider.toolCalls[0] != "shell" {
		s.Fail("toolCalls = %v，期望 [shell]", provider.toolCalls)
	}
	if len(provider.pendingIDs) != 1 {
		s.Fail("pendingIDs = %v，期望 1 个", provider.pendingIDs)
	}
}

// TestInteractionLifecycle_审批拒绝后工具不执行
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_审批拒绝后工具不执行() {
	sess := newRealSession(s.T(), "sess-approval-deny")
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionDeny}
	provider := &fakeCLIProvider{handler: handler}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if handler.handleCount != 1 {
		s.Fail("handleCount = %d，期望 1（宿主仍被问了）", handler.handleCount)
	}
	if len(provider.toolCalls) != 0 {
		s.Fail("toolCalls = %v，期望空（被拒绝）", provider.toolCalls)
	}
}

// TestInteractionLifecycle_审批ABORT后工具不执行 ABORT 表示拒绝并中止整轮
// Python: interactions.py:48-54 ToolApprovalDecision.ABORT
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_审批ABORT后工具不执行() {
	sess := newRealSession(s.T(), "sess-approval-abort")
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAbort}
	provider := &fakeCLIProvider{handler: handler}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if len(provider.toolCalls) != 0 {
		s.Fail("toolCalls = %v，期望空（ABORT 同样不执行）", provider.toolCalls)
	}
}

// TestInteractionLifecycle_多工具串行审批 5 个工具逐个审批，request_id 不串
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_多工具串行审批() {
	sess := newRealSession(s.T(), "sess-multi-tool")
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{handler: handler}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "read,write,edit,bash,grep", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if handler.handleCount != 5 {
		s.Fail("handleCount = %d，期望 5", handler.handleCount)
	}
	if len(provider.toolCalls) != 5 {
		s.Fail("toolCalls = %v，期望 5 个", provider.toolCalls)
	}
	// request_id 必须互不相同（否则应答会串到别的请求）
	seen := map[string]bool{}
	for _, id := range provider.pendingIDs {
		if seen[id] {
			s.Fail("request_id 重复: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != 5 {
		s.Fail("去重后 request_id 数 = %d，期望 5", len(seen))
	}
}

// TestInteractionLifecycle_并发多轮互不串扰 4 个 goroutine 各自跑一轮，
// 验证 15.3 契约层在多轮并发下不产生可观察的错配
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_并发多轮互不串扰() {
	const workers = 4
	var wg sync.WaitGroup
	errs := make([]error, workers)
	handlers := make([]*recordingHandler, workers)
	providers := make([]*fakeCLIProvider, workers)

	for i := 0; i < workers; i++ {
		sess := newRealSession(s.T(), "sess-concurrent-"+string(rune('a'+i)))
		handlers[i] = &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
		providers[i] = &fakeCLIProvider{handler: handlers[i]}
		if err := providers[i].Start(context.Background(), sess); err != nil {
			s.Require().NoError(err, "Start", i)
		}
	}
	defer func() {
		for i := range providers {
			_ = providers[i].Stop(context.Background())
		}
	}()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = providers[idx].Send(context.Background(), "shell", false)
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			s.Fail("worker %d Send: %v", i, errs[i])
		}
		if handlers[i].handleCount != 1 {
			s.Fail("worker %d handleCount = %d，期望 1", i, handlers[i].handleCount)
		}
		if len(providers[i].toolCalls) != 1 {
			s.Fail("worker %d toolCalls = %v，期望 1 个", i, providers[i].toolCalls)
		}
	}
}

// TestInteractionLifecycle_Cancel幂等且记录原因 验证 cancel 可重入 + 四种 reason
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_Cancel幂等且记录原因() {
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	ctx := context.Background()

	reasons := []harness_protocol.InteractionCancelReason{
		harness_protocol.InteractionCancelReasonProviderWithdrew,
		harness_protocol.InteractionCancelReasonTurnAborted,
		harness_protocol.InteractionCancelReasonHarnessStopped,
		harness_protocol.InteractionCancelReasonDeadlineExceeded,
	}
	if len(reasons) != 4 {
		s.Require().Len(reasons, 4, "取消原因数应为 4")
	}
	for _, r := range reasons {
		if err := handler.Cancel(ctx, "req-"+string(r), harness_protocol.NewCancelOption(r)); err != nil {
			s.Require().NoError(err, "Cancel")
		}
	}
	// 幂等：同一 id 重复取消不报错
	if err := handler.Cancel(ctx, "req-dup"); err != nil {
		s.Require().NoError(err, "第一次 Cancel")
	}
	if err := handler.Cancel(ctx, "req-dup"); err != nil {
		s.Require().NoError(err, "第二次 Cancel 应幂等")
	}
	if len(handler.cancelLog) != 6 {
		s.Require().Len(handler.cancelLog, 6, "cancelLog 应为 6 条")
	}
	// 前 4 条原因必须与传入一致
	for i, want := range reasons {
		if handler.cancelLog[i].Reason != want {
			s.Fail("cancelLog[%d].Reason = %q，期望 %q", i, handler.cancelLog[i].Reason, want)
		}
	}
	// 未传 option 时必须是默认 provider_withdrew（Q8 决策 2）
	if handler.cancelLog[4].Reason != harness_protocol.InteractionCancelReasonProviderWithdrew {
		s.Fail("零参 Cancel 的 reason = %q，期望 provider_withdrew", handler.cancelLog[4].Reason)
	}
}

// TestInteractionLifecycle_ctx取消传播到交互 验证 harness 在 ctx 取消时
// 能收到错误（模拟 turn abort 时取消 pending 交互的契约基础）
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_ctx取消传播到交互() {
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	req, err := harness_protocol.NewToolApprovalRequest("r1", "call-1", "shell",
		harness_protocol.ToolApprovalRequestOptions{})
	if err != nil {
		s.Require().NoError(err, "构造请求")
	}
	resp, err := handler.Handle(ctx, req)
	if err != nil {
		s.Require().NoError(err, "Handle")
	}
	if _, err := harness_protocol.ValidateInteractionResponse(req, resp); err != nil {
		s.Require().NoError(err, "应答校验失败")
	}
}

// TestInteractionLifecycle_真实InteractiveInput载体 用真实 interaction 子系统的
// InteractiveInput 作为应答载荷，验证 15.3 应答的 content 字段能承载它
func (s *InteractionLifecycleSuite) TestInteractionLifecycle_真实InteractiveInput载体() {
	input := newInteractiveInput(s.T(), map[string]any{"approved": true})
	if input == nil || !input.IsInteractiveInput() {
		s.FailNow("InteractiveInput 构造失败")
	}
	req, err := harness_protocol.NewUserInputRequest("r-input", "please confirm",
		harness_protocol.UserInputRequestOptions{
			Choices: []string{"yes", "no"},
		})
	if err != nil {
		s.Require().NoError(err, "UserInputRequest")
	}
	if len(req.Choices) != 2 {
		s.Fail("Choices = %v，期望 2 项", req.Choices)
	}
	resp, err := harness_protocol.NewUserInputResponse(req.RequestID,
		harness_protocol.InteractionResponseStatusCompleted,
		harness_protocol.UserInputResponseOptions{Content: input.PlainText()})
	if err != nil {
		s.Require().NoError(err, "UserInputResponse")
	}
	if _, err := harness_protocol.ValidateInteractionResponse(req, resp); err != nil {
		s.Require().NoError(err, "配对校验失败")
	}
}
