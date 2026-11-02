//go:build integration

package tools_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// afterToolCallRecorder 记录 AFTER_TOOL_CALL 是否触发及当时的异常。
//
// 用它断言「工具 panic 后 after 钩子仍然触发」——after 钩子是
// stream_event_rail 发 tool_result 的唯一入口，after 不触发就等于前端卡片
// 永久停在「执行中」。
type afterToolCallRecorder struct {
	agentinterfaces.BaseRail
	// mu 保护以下字段（工具执行在并行 goroutine 中）
	mu sync.Mutex
	// calls AFTER_TOOL_CALL 触发次数
	calls int
	// lastTool 最近一次的工具名
	lastTool string
	// lastErrMsg 最近一次回调看到的异常文本
	lastErrMsg string
}

// panicInvokeTool 在 Invoke 中直接 panic，模拟工具实现缺陷（如 read_file
// 在 sys_operation 为 nil 时的 nil 解引用）。
type panicInvokeTool struct {
	// card 工具卡片
	card *tool.ToolCard
}

// ToolPanicResultSuite 测试工具 panic 后仍会触发 AFTER_TOOL_CALL。
//
// 回归背景：panic 若逃出 executeSingleToolCall，能力层并行 goroutine 的
// recover 会把它变成 results[idx]，但 RailExecutor 的 finally 语义被绕过 ——
// AFTER_TOOL_CALL 不触发，stream_event_rail 不发 tool_result，前端工具卡片
// 永久停在「执行中」，而会话照常结束。
type ToolPanicResultSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolPanicResultSuite(t *testing.T) {
	suite.Run(t, new(ToolPanicResultSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetCallbacks 只关心 AFTER_TOOL_CALL。
func (r *afterToolCallRecorder) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackAfterToolCall, func(_ context.Context, railCtx any) error {
			cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
			if !ok {
				return nil
			}
			inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)

			r.mu.Lock()
			defer r.mu.Unlock()
			r.calls++
			if inputs != nil {
				r.lastTool = inputs.ToolName
			}
			if exc := cbc.Exception(); exc != nil {
				r.lastErrMsg = exc.Error()
			}
			return nil
		}),
	)
}

// snapshot 返回当前记录的快照。
func (r *afterToolCallRecorder) snapshot() (int, string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.lastTool, r.lastErrMsg
}

// Card 返回工具卡片。
func (p *panicInvokeTool) Card() *tool.ToolCard { return p.card }

// Invoke 直接 panic，模拟工具实现缺陷。
func (p *panicInvokeTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	panic("itest tool boom")
}

// Stream 不支持流式。
func (p *panicInvokeTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// TestToolPanic_仍触发AfterToolCall 回归：工具 panic 被转成 error 后，
// AFTER_TOOL_CALL 必须照常触发（否则前端卡片永远「执行中」）。
func (s *ToolPanicResultSuite) TestToolPanic_仍触发AfterToolCall() {
	const toolName = "itest_panic_tool"
	card := tool.NewToolCardWithID(toolName, toolName, "总是 panic 的工具", nil, nil)
	recorder := &afterToolCallRecorder{}
	recorder.WithPriority(90)

	s.MockLLM.SetResponses(
		// 第 1 轮：调用 panic 工具
		mockllm.CreateToolCallResponse(toolName, `{}`),
		// 第 2 轮：文本收尾
		mockllm.CreateTextResponse("工具失败后收尾"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{&panicInvokeTool{card: card}},
		Rails:         []agentinterfaces.AgentRail{recorder},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	res, err := agent.Invoke(s.Ctx, map[string]any{"query": "调一个会 panic 的工具"})
	s.Require().NoError(err, "工具 panic 不应让 Invoke 报错（应转成工具错误结果）")
	s.Require().NotNil(res)

	calls, lastTool, lastErrMsg := recorder.snapshot()
	s.Equal(1, calls, "AFTER_TOOL_CALL 应恰好触发一次（否则不会发 tool_result）")
	s.Equal(toolName, lastTool, "回调应看到被调用的工具名")
	s.True(strings.Contains(lastErrMsg, "panic"),
		"after 钩子应能看到 panic 异常，实际: %q", lastErrMsg)
}
