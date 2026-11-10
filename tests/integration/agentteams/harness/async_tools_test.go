//go:build integration

package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// TestAsyncToolCompletion回灌新一轮 验证 launched → 后台完成 → 自动开新一轮。
func (s *NativeHarnessSuite) TestAsyncToolCompletion回灌新一轮() {
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

// TestNativeHarnessSuite 套件入口。
//
// 本目录所有用例都挂在 NativeHarnessSuite 上，由这个入口统一注册；
// 入口放在本文件而不放 native_harness_test.go，是静态审计「每个集成测试文件
// 必须自带 suite.Run 调用」的硬性要求。
func TestNativeHarnessSuite(t *testing.T) {
	suite.Run(t, new(NativeHarnessSuite))
}
