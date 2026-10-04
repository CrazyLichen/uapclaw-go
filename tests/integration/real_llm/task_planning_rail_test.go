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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskPlanningRailSuite TaskPlanningRail + 真实 LLM 集成测试套件。
// 对齐 Python: tests/system_tests/harness/rail/test_task_planning_rail_system.py
type TaskPlanningRailSuite struct {
	isuite.RealLLMSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTaskPlanningRailSuite 运行 TaskPlanningRail 真实 LLM 测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestTaskPlanningRailSuite(t *testing.T) {
	suite.Run(t, new(TaskPlanningRailSuite))
}

// TestTaskPlanningRail_真实LLM_规划 测试真实 LLM 下 TaskPlanningRail 的任务规划能力
// 对齐 Python: test_task_planning_rail_system.py
func (s *TaskPlanningRailSuite) TestTaskPlanningRail_真实LLM_规划() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 3*time.Minute)
	defer cancel()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("planning_real_llm"),
		agentschema.WithAgentName("PlanningRealLLMAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:              card,
		Model:             model,
		MaxIterations:     5,
		EnableTaskPlanning: true,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	rail := rails.NewTaskPlanningRail()
	agent.AddRail(rail)

	sess := session.NewSession(
		session.WithSessionID("planning_real_llm_session"),
		session.WithEnvs(map[string]any{}),
	)
	err = sess.PreRun(ctx)
	s.Require().NoError(err, "Session PreRun 失败")

	result, err := agent.Invoke(ctx, map[string]any{"query": "请帮我规划一个写 hello world 程序的任务"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Agent 运行失败")
	s.NotNil(result, "运行结果不应为 nil")
}

// TestTaskPlanningRailDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *TaskPlanningRailSuite) TestTaskPlanningRailDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(rails.NewTaskPlanningRail)
}
