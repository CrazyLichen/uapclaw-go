//go:build llm

package real_llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentE2ESuite DeepAgent e2e 真实 LLM 集成测试套件。
// 对齐 Python: tests/system_tests/harness/test_deep_agent_e2e.py
type DeepAgentE2ESuite struct {
	isuite.RealLLMSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeepAgentE2ESuite 运行 DeepAgent e2e 真实 LLM 测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestDeepAgentE2ESuite(t *testing.T) {
	suite.Run(t, new(DeepAgentE2ESuite))
}

// TestDeepAgent_真实LLM_基础问答 测试真实 LLM 下 DeepAgent 的基础问答
// 对齐 Python: test_deep_agent_e2e.py — 简单问答场景
func (s *DeepAgentE2ESuite) TestDeepAgent_真实LLM_基础问答() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 2*time.Minute)
	defer cancel()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("e2e_real_llm_basic"),
		agentschema.WithAgentName("E2ERealLLMBasicAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("e2e_basic_session"),
		session.WithEnvs(map[string]any{}),
	)
	err = sess.PreRun(ctx)
	s.Require().NoError(err, "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "Go 语言的发明者是谁？"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Agent 运行失败")
	s.NotNil(result, "运行结果不应为 nil")
}

// TestDeepAgent_真实LLM_多轮交互 测试真实 LLM 下 DeepAgent 的多轮交互
// 对齐 Python: test_steer_inner_loop.py — ReAct 循环
func (s *DeepAgentE2ESuite) TestDeepAgent_真实LLM_多轮交互() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 3*time.Minute)
	defer cancel()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("e2e_real_llm_multi"),
		agentschema.WithAgentName("E2ERealLLMMultiAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("e2e_multi_session"),
		session.WithEnvs(map[string]any{}),
	)
	err = sess.PreRun(ctx)
	s.Require().NoError(err, "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "请先告诉我当前时间是什么，然后总结一下你的回答"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Agent 运行失败")
	s.NotNil(result, "运行结果不应为 nil")
}

// TestDeepAgentE2EDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *DeepAgentE2ESuite) TestDeepAgentE2EDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(harness.CreateDeepAgent)
}
