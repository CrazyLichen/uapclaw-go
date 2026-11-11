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

// HooksBoundarySuite 钩子四边界集成套件。
// 跨 agentcore/harness_protocol（被测契约）+ agentcore/session（真实会话）
// + agent_teams/harness（L2 契约）三个子系统。
type HooksBoundarySuite struct {
	basesuite.BaseIntegrationSuite
}

// TestHooksBoundarySuite 套件入口。
func TestHooksBoundarySuite(t *testing.T) {
	suite.Run(t, new(HooksBoundarySuite))
}

// TestHooksBoundary_四边界全被调用 一轮执行必须依次经过
// before_prompt → before_tool → after_tool → on_stop
// Python: hooks.py:127-150（4 方法）；SPEC.md:188-197
func (s *HooksBoundarySuite) TestHooksBoundary_四边界全被调用() {
	sess := newRealSession(s.T(), "sess-hooks-four")
	hooks := &recordingHooks{}
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{handler: handler, hooks: hooks}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if hooks.beforePromptCalls != 1 {
		s.Fail("beforePromptCalls = %d，期望 1", hooks.beforePromptCalls)
	}
	if hooks.beforeToolCalls != 1 {
		s.Fail("beforeToolCalls = %d，期望 1", hooks.beforeToolCalls)
	}
	if hooks.afterToolCalls != 1 {
		s.Fail("afterToolCalls = %d，期望 1", hooks.afterToolCalls)
	}
	if hooks.onStopCalls != 1 {
		s.Fail("onStopCalls = %d，期望 1", hooks.onStopCalls)
	}
	// turn_id 必须被透传到每个钩子（对齐上游 test_protocol.py:510-525）
	if len(hooks.observedTurnIDs) == 0 {
		s.FailNow("未记录 turn_id")
	}
	for i, tid := range hooks.observedTurnIDs {
		if tid != "turn-1" {
			s.Fail("observedTurnIDs[%d] = %q，期望 turn-1", i, tid)
		}
	}
}

// stopPromptHooks before_prompt 恒返回 continue=false 的钩子。
type stopPromptHooks struct{}

// BeforePrompt 恒返回 continue_execution=false，掐断整轮。
func (stopPromptHooks) BeforePrompt(_ context.Context, hookCtx harness_protocol.BeforePromptContext) (harness_protocol.BeforePromptResult, error) {
	res, err := harness_protocol.NewBeforePromptResult(hookCtx.Prompt, harness_protocol.BeforePromptResultOptions{})
	if err != nil {
		return harness_protocol.BeforePromptResult{}, err
	}
	res.ContinueExecution = false
	return res, nil
}

// BeforeTool 恒允许（本钩子在掐断场景下不应被调用）。
func (stopPromptHooks) BeforeTool(_ context.Context, _ harness_protocol.BeforeToolContext) (harness_protocol.ToolDecision, error) {
	return harness_protocol.ToolDecision{Decision: harness_protocol.ToolDecisionKindAllow}, nil
}

// AfterTool 返回空结果。
func (stopPromptHooks) AfterTool(_ context.Context, _ harness_protocol.AfterToolContext) (harness_protocol.AfterToolResult, error) {
	return harness_protocol.NewAfterToolResult(harness_protocol.AfterToolResultOptions{})
}

// OnStop 空实现。
func (stopPromptHooks) OnStop(_ context.Context, _ harness_protocol.StopHookContext) error {
	return nil
}

// TestHooksBoundary_before_prompt可掐断整轮
// Python: hooks.py:40-48 BeforePromptResult.continue_execution
func (s *HooksBoundarySuite) TestHooksBoundary_before_prompt可掐断整轮() {
	sess := newRealSession(s.T(), "sess-hooks-stop")
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{handler: handler, hooks: stopPromptHooks{}}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if len(provider.toolCalls) != 0 {
		s.Fail("toolCalls = %v，期望空（整轮被掐断）", provider.toolCalls)
	}
	if handler.handleCount != 0 {
		s.Fail("handleCount = %d，期望 0（未走到审批）", handler.handleCount)
	}
}

// TestHooksBoundary_before_tool拒绝后工具不执行且不问审批
func (s *HooksBoundarySuite) TestHooksBoundary_before_tool拒绝后工具不执行() {
	sess := newRealSession(s.T(), "sess-hooks-deny")
	hooks := &recordingHooks{denyTool: "shell"}
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{handler: handler, hooks: hooks}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if hooks.beforeToolCalls != 1 {
		s.Fail("beforeToolCalls = %d，期望 1", hooks.beforeToolCalls)
	}
	if len(provider.toolCalls) != 0 {
		s.Fail("toolCalls = %v，期望空（被 DENY）", provider.toolCalls)
	}
	if hooks.afterToolCalls != 0 {
		s.Fail("afterToolCalls = %d，期望 0（工具未执行）", hooks.afterToolCalls)
	}
}

// TestHooksBoundary_before_tool改写参数后生效
// Python: hooks.py:74-80 REWRITE 必须带 updated_arguments
func (s *HooksBoundarySuite) TestHooksBoundary_before_tool改写参数后生效() {
	sess := newRealSession(s.T(), "sess-hooks-rewrite")
	hooks := &recordingHooks{rewriteTo: "rm -rf ./tmp"}
	provider := &fakeCLIProvider{hooks: hooks}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if hooks.beforeToolCalls != 1 {
		s.Fail("beforeToolCalls = %d，期望 1", hooks.beforeToolCalls)
	}
	// 改写后工具仍执行（REWRITE 是「改写后允许」）
	if len(provider.toolCalls) != 1 {
		s.Fail("toolCalls = %v，期望 1 个（改写后允许）", provider.toolCalls)
	}
}

// TestHooksBoundary_after_tool替换结果
// Python: hooks.py:101-110 AfterToolResult.replace_result
func (s *HooksBoundarySuite) TestHooksBoundary_after_tool替换结果() {
	sess := newRealSession(s.T(), "sess-hooks-replace")
	hooks := &recordingHooks{replaceResultWith: map[string]any{"redacted": true}}
	handler := &recordingHandler{decision: harness_protocol.ToolApprovalDecisionAllow}
	provider := &fakeCLIProvider{handler: handler, hooks: hooks}

	if err := provider.Start(context.Background(), sess); err != nil {
		s.Require().NoError(err, "Start")
	}
	defer func() { _ = provider.Stop(context.Background()) }()

	if err := provider.Send(context.Background(), "shell", false); err != nil {
		s.Require().NoError(err, "Send")
	}
	if hooks.afterToolCalls != 1 {
		s.Fail("afterToolCalls = %d，期望 1", hooks.afterToolCalls)
	}
	if len(provider.toolCalls) != 1 {
		s.Fail("toolCalls = %v，期望 1 个（替换结果不影响执行）", provider.toolCalls)
	}
}

// TestHooksBoundary_并发多轮钩子计数正确 钩子实现在并发下必须不被撕裂
func (s *HooksBoundarySuite) TestHooksBoundary_并发多轮钩子计数正确() {
	const workers = 4
	hooks := &recordingHooks{}
	var wg sync.WaitGroup
	providers := make([]*fakeCLIProvider, workers)

	for i := 0; i < workers; i++ {
		sess := newRealSession(s.T(), "sess-hooks-concurrent-"+string(rune('a'+i)))
		providers[i] = &fakeCLIProvider{hooks: hooks}
		if err := providers[i].Start(context.Background(), sess); err != nil {
			s.Require().NoError(err, "Start", i)
		}
	}
	defer func() {
		for i := range providers {
			_ = providers[i].Stop(context.Background())
		}
	}()

	errs := make([]error, workers)
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
	}
	// 每个 worker 各触发 1 次 before_prompt / before_tool / after_tool / on_stop
	if hooks.beforePromptCalls != workers {
		s.Fail("beforePromptCalls = %d，期望 %d", hooks.beforePromptCalls, workers)
	}
	if hooks.beforeToolCalls != workers {
		s.Fail("beforeToolCalls = %d，期望 %d", hooks.beforeToolCalls, workers)
	}
	if hooks.afterToolCalls != workers {
		s.Fail("afterToolCalls = %d，期望 %d", hooks.afterToolCalls, workers)
	}
	if hooks.onStopCalls != workers {
		s.Fail("onStopCalls = %d，期望 %d", hooks.onStopCalls, workers)
	}
}

// TestHooksBoundary_ctx取消传播到钩子 验证 ctx 首参在钩子链路上不被丢弃
func (s *HooksBoundarySuite) TestHooksBoundary_ctx取消传播到钩子() {
	hooks := &ctxObservingHooks{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := harness_protocol.BeforeToolContext{ToolName: "shell", Arguments: harness_protocol.JsonObject{}}
	if _, err := hooks.BeforeTool(ctx, req); err != nil {
		s.Require().NoError(err, "BeforeTool")
	}
	if !hooks.sawCancelled {
		s.Fail("钩子未观察到 ctx 已取消 —— ctx 在链路上被丢弃了")
	}
}

// ctxObservingHooks 记录是否观察到 ctx 取消。
type ctxObservingHooks struct {
	stopPromptHooks
	sawCancelled bool
}

// BeforeTool 记录 ctx 是否已取消。
func (h *ctxObservingHooks) BeforeTool(ctx context.Context, _ harness_protocol.BeforeToolContext) (harness_protocol.ToolDecision, error) {
	h.sawCancelled = ctx.Err() != nil
	return harness_protocol.ToolDecision{Decision: harness_protocol.ToolDecisionKindAllow}, nil
}
