//go:build integration && llm

package real_llm

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAgentRealSuite 真实 LLM 下 CodeAgent plan 模式 E2E 测试套件。
//
// 对齐 Python: tests/system_tests/code_agent/test_code_agent_execute_mode_e2e.py
// 的 test_plan_mode_with_real_model_end_to_end（@unittest.skip("skip system test")，
// Python 从未运行）。
//
// 覆盖：
//   - CreateCodeAgent 工厂（自动注入 explore_agent/plan_agent + 必需 Rails）
//   - plan 模式下 enter_plan_mode 写入 plan 文件（.plans/<slug>.md）
//   - switch_mode("normal") 后按 plan 执行（todo_create + 工具链）
//   - 二轮调用不应再出现 enter_plan_mode
//
// Go 与 Python 差异：
//   - Python create_code_agent(workspace=self._work_dir)；
//     Go 用 workspace.NewWorkspace(root)（CreateCodeAgent 透传 Workspace）。
//   - Python session = create_agent_session(card=AgentCard(id="code_agent"...))；
//     Go 用 session.NewSession(WithSessionID + WithEnvs + WithCheckpointer)。
//   - Python 断言 state.plan_mode.plan_slug 非空 + plan 文件存在且非空；
//     Go 的 GetPlanFilePath 返回路径字符串（空串 = 无 slug）。
type CodeAgentRealSuite struct {
	isuite.RealLLMSuite
	// workDir 当前测试的 workspace 根目录
	workDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodeAgentRealSuite 运行真实 LLM CodeAgent plan 模式测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestCodeAgentRealSuite(t *testing.T) {
	suite.Run(t, new(CodeAgentRealSuite))
}

// SetupTest 为每个测试方法创建独立工作目录。
// 守卫放在测试方法内（skipUnlessRealLLMTests），避免连带跳过包引用验证测试。
func (s *CodeAgentRealSuite) SetupTest() {
	if !s.realLLMTestsEnabled() {
		return
	}
	s.SkipIfNoRealLLM()

	s.workDir = s.T().TempDir()
}

// TestCodeAgentReal_真实LLM_plan模式写入与执行 测试 CodeAgent plan 模式两轮流：
// 第一轮 plan 模式生成 plan 文件，第二轮 normal 模式按 plan 执行。
// 对齐 Python: test_plan_mode_with_real_model_end_to_end
//
// 断言对齐 Python 原文：
//   - 两轮 result_type=="answer"
//   - 第一轮后 plan_mode.mode=="plan" 且 plan_slug 非空且 plan 文件存在非空
//   - 第二轮 slice 无 enter_plan_mode、有 todo_create
//   - 第二轮后 plan_mode.mode=="normal"
func (s *CodeAgentRealSuite) TestCodeAgentReal_真实LLM_plan模式写入与执行() {
	s.skipUnlessRealLLMTests()
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Minute)
	defer cancel()

	// 对齐 Python: trace = ToolTraceRail() + create_code_agent(..., rails=[trace], ...)
	toolTrace := testhelpers.NewToolTraceRail()

	// NewWorkspace(rootPath, language)：构造不返回 error（校验失败 panic，对齐 Python raise）
	ws := workspace.NewWorkspace(s.workDir, "cn")

	// 对齐 Python: agent = create_code_agent(model=..., system_prompt=..., enable_task_loop=True, max_iterations=24, workspace=...)
	agent, err := harness.CreateCodeAgent(ctx, &hschema.SubagentCreateParams{
		Model: model,
		Card: agentschema.NewAgentCard(
			agentschema.WithAgentID("code_agent"),
			agentschema.WithAgentName("code_agent"),
		),
		SystemPrompt: "你是一个 AI 编程助手，当用户要求你写代码、创建文件、修改文件时，" +
			"你**必须**调用相应的工具，**绝对不能**直接在回复中输出代码。" +
			"你必须严格按照plan模式的工作流执行",
		Rails:          []agentinterfaces.AgentRail{toolTrace},
		EnableTaskLoop: true,
		MaxIterations:  24,
		Workspace:      ws,
	})
	s.Require().NoError(err, "CreateCodeAgent 失败")

	sess := session.NewSession(
		session.WithSessionID(fmt.Sprintf("codeagent_plan_%d", time.Now().UnixNano())),
		session.WithEnvs(map[string]any{}),
		session.WithCheckpointer(checkpointer.NewInMemoryCheckpointer()),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	defer sess.PostRun(ctx)

	// ---- 第一轮：plan 模式（对齐 Python first）----
	agent.SwitchMode(sess, "plan")

	first, err := agent.Invoke(ctx, map[string]any{
		"query": "帮我规划一个简单的模块开发任务，创建一个动态展示城市信息的网页。" +
			"所有问题你自己决定即可！",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "第一轮 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), first)

	// 对齐 Python: state_after_first.plan_mode 断言
	stateAfterFirst := agent.LoadState(sess)
	s.Require().NotNil(stateAfterFirst)
	s.Equal("plan", stateAfterFirst.PlanMode.Mode, "第一轮后 mode 应为 plan")
	s.NotEmpty(stateAfterFirst.PlanMode.PlanSlug, "第一轮后 plan_slug 应非空")

	planPath := agent.GetPlanFilePath(sess)
	s.Require().NotEmpty(planPath, "plan 文件路径不应为空（slug 已生成）")
	planContent, readErr := os.ReadFile(planPath)
	s.Require().NoError(readErr, "plan 文件应存在")
	s.NotEmpty(string(planContent), "plan 文件内容不应为空")

	firstCallCount := toolTrace.TotalCalls()

	// ---- 第二轮：normal 模式按 plan 执行（对齐 Python second）----
	agent.SwitchMode(sess, "normal")

	second, err := agent.Invoke(ctx, map[string]any{
		"query": "按照计划执行把",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "第二轮 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), second)

	// 对齐 Python: second_call_slice 断言（第二轮不应再出现 enter_plan_mode）
	secondCallSlice := toolTrace.GetCallSequence()[firstCallCount:]
	s.NotContains(secondCallSlice, "enter_plan_mode", "第二轮不应出现 enter_plan_mode")

	stateAfterSecond := agent.LoadState(sess)
	s.Require().NotNil(stateAfterSecond)
	s.Equal("normal", stateAfterSecond.PlanMode.Mode, "第二轮后 mode 应为 normal")
}

// TestCodeAgentRealDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *CodeAgentRealSuite) TestCodeAgentRealDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(harness.CreateCodeAgent)
	s.NotNil(workspace.NewWorkspace)
	s.NotNil(hschema.SubagentCreateParams{})
	s.NotNil(sys_operation.NewSysOperation)
	s.NotNil(testhelpers.NewToolTraceRail)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// realLLMTestsEnabled 检查 RUN_REAL_LLM_TESTS 开关是否开启。
// 对齐 Python: os.getenv("RUN_REAL_LLM_TESTS", "0") != "1" → pytest.skip
func (s *CodeAgentRealSuite) realLLMTestsEnabled() bool {
	return os.Getenv("RUN_REAL_LLM_TESTS") == "1"
}

// skipUnlessRealLLMTests 在 RUN_REAL_LLM_TESTS 未开启时跳过测试。
// 对齐 Python: pytest.skip("Real LLM test skipped. Set RUN_REAL_LLM_TESTS=1 to enable.")
func (s *CodeAgentRealSuite) skipUnlessRealLLMTests() {
	if !s.realLLMTestsEnabled() {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
}
