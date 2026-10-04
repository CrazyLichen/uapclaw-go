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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserRailSuite AskUserRail + 真实 LLM 集成测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_deep_agent_ask_user.py
type AskUserRailSuite struct {
	isuite.RealLLMSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAskUserRailSuite 运行 AskUserRail 真实 LLM 测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestAskUserRailSuite(t *testing.T) {
	suite.Run(t, new(AskUserRailSuite))
}

// TestAskUserRail_真实LLM_中断 测试真实 LLM 下 AskUserRail 的中断行为
// 对齐 Python: test_hitl_ask_user_rail_multi_question
func (s *AskUserRailSuite) TestAskUserRail_真实LLM_中断() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 3*time.Minute)
	defer cancel()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("ask_user_real_llm"),
		agentschema.WithAgentName("AskUserRealLLMAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	rail := interrupt.NewAskUserRail()
	agent.AddRail(rail)

	sess := session.NewSession(
		session.WithSessionID("ask_user_real_llm_session"),
		session.WithEnvs(map[string]any{}),
	)
	err = sess.PreRun(ctx)
	s.Require().NoError(err, "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user 工具询问用户喜欢什么颜色"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Agent 运行失败")
	s.NotNil(result, "运行结果不应为 nil")
}

// TestAskUserRailDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *AskUserRailSuite) TestAskUserRailDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(interrupt.NewAskUserRail)
}
