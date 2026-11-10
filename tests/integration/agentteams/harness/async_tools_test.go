//go:build integration

package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AsyncToolsSuite 异步工具回灌集成测试。
//
// 跨度（规则 7）：agent_teams/harness → agent_teams/tools
// → agentcore/harness（DeepAgent 任务循环）→ agentcore/session
// （≥3 个 internal 二级子系统）。
type AsyncToolsSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAsyncToolsSuite 套件入口。
//
// 集成审计逐文件检查 suite.Run，入口必须与套件定义同文件。
func TestAsyncToolsSuite(t *testing.T) {
	suite.Run(t, new(AsyncToolsSuite))
}

// newStartedHarness 构造并 Start 一个带 MockLLM 的 NativeHarness。
//
// 与 NativeHarnessSuite 的同名助手逻辑一致，但套件类型不同，
// Go 嵌入无法跨套件绑定接收者，故独立实现（同 PauseResumeSuite 做法）。
func (s *AsyncToolsSuite) newStartedHarness() (*teamharness.NativeHarness, *session.Session) {
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 4,
	})
	s.Require().NoError(err)
	_ = agent
	spec, specErr := s.NewSpecWithMockLLM(4)
	s.Require().NoError(specErr)
	h, err := teamharness.NewNativeHarness(spec, nil)
	s.Require().NoError(err, "NewNativeHarness")

	sess := session.CreateAgentSession("async-tools-sess", h.DeepAgent().Card(), nil)
	s.Require().NoError(sess.PreRun(s.Ctx, map[string]any{}))
	s.Require().NoError(h.Start(s.Ctx, sess))
	return h, sess
}

// TestAsyncToolCompletion回灌新一轮 验证 launched → 后台完成 → 自动开新一轮。
func (s *AsyncToolsSuite) TestAsyncToolCompletion回灌新一轮() {
	h, _ := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("后台结果已收到"))
	before := s.MockLLM.InvokeCallCount()

	gate := make(chan struct{})
	h.AsyncToolRuntime().Launch("e2e", func(ctx context.Context) (any, error) {
		<-gate
		return "e2e-result", nil
	}, tools.AsyncToolLaunchOptions{ToolName: "demo"})
	close(gate)

	s.Require().Eventually(func() bool {
		rec := h.AsyncToolRuntime().Get("e2e")
		return rec != nil && rec.Status == teamharness.AsyncToolStatusCompleted
	}, 5*time.Second, 20*time.Millisecond, "异步任务应完成")

	s.Require().Eventually(func() bool {
		return s.MockLLM.InvokeCallCount() > before
	}, 10*time.Second, 50*time.Millisecond, "完成回灌应触发新一轮模型调用")
}
