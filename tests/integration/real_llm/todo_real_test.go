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
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
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

// TodoRealSuite 真实 LLM 下 Todo 工具 E2E 测试套件。
//
// 对齐 Python: tests/system_tests/harness/tools/test_todo_tool.py
// （test_deep_agent_todo_create_list_modify，@unittest.skip("skip system test")）
//
// 覆盖：todo_create（创建）→ todo_list（列表 ≥2 次）→ todo_modify（修改）
// → todo_list（确认）→ 文本总结。
//
// Go 与 Python 差异：
//   - Python _get_todo_tools 注册 sys_operation_for_todo_tool（./workspace）
//     再手工构造 TodoCreateTool/TodoListTool/TodoModifyTool 注册 resource_mgr；
//     Go 用 todo.CreateTodosTool(workspace, fs, language, agentID) 一次创建
//     4 工具（create/list/get/modify），通过 ToolInstances 注入 DeepAgent。
//   - todo 工具通过 ToolCallOptions.Session 提取 sessionID，
//     持久化文件写在工作目录下（TodoTool.workspace 字段）。
type TodoRealSuite struct {
	isuite.RealLLMSuite
	// workDir 当前测试的工作目录（todo 持久化目录）
	workDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTodoRealSuite 运行真实 LLM Todo 工具测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestTodoRealSuite(t *testing.T) {
	suite.Run(t, new(TodoRealSuite))
}

// SetupTest 为每个测试方法创建独立工作目录。
// 守卫放在测试方法内（skipUnlessRealLLMTests），避免连带跳过包引用验证测试。
func (s *TodoRealSuite) SetupTest() {
	if !s.realLLMTestsEnabled() {
		return
	}
	s.SkipIfNoRealLLM()

	s.workDir = s.T().TempDir()
}

// TestTodoReal_真实LLM_创建列表修改 测试 Todo 工具三连：创建、列表、修改、再次确认。
// 对齐 Python: test_deep_agent_todo_create_list_modify
//
// 断言对齐 Python 原文：
//   - result_type=="answer" 且 output 非空
//   - todo_create 调用 ≥ 1
//   - todo_list 调用 ≥ 2
//   - todo_modify 调用 ≥ 1
//   - 总工具调用数 ≥ 4
func (s *TodoRealSuite) TestTodoReal_真实LLM_创建列表修改() {
	s.skipUnlessRealLLMTests()
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 8*time.Minute)
	defer cancel()

	// 对齐 Python: tool_trace = ToolTraceRail()
	toolTrace := testhelpers.NewToolTraceRail()

	// 对齐 Python: _get_todo_tools()（真实 fs 操作 + 独立工作目录）
	card := sys_operation.NewSysOperationCard()
	sysOp, err := sys_operation.NewSysOperation(card)
	s.Require().NoError(err, "创建 SysOperation 失败")

	todoTools, _ := todo.CreateTodosTool(s.workDir, sysOp.Fs(), "cn", "todo_real_agent")

	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("todo_real_agent"),
		agentschema.WithAgentName("TodoRealAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:  agentCard,
		Model: model,
		SystemPrompt: "你是一个严谨的任务执行助手。" +
			"当用户要求使用todo工具时，必须调用工具，不要凭空假设。",
		ToolInstances: todoTools,
		Rails:         []agentinterfaces.AgentRail{toolTrace},
		MaxIterations: 10,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID(fmt.Sprintf("todo_e2e_%d", time.Now().UnixNano())),
		session.WithEnvs(map[string]any{}),
		session.WithCheckpointer(checkpointer.NewInMemoryCheckpointer()),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	defer sess.PostRun(ctx)

	// 对齐 Python query 原文
	query := "请严格按顺序执行以下任务，并且每一步都必须调用工具：\n" +
		"1. 创建一个待办事项列表，包含3个任务：完成需求分析、编写代码、测试验证；\n" +
		"2. 列出当前的待办事项；\n" +
		"3. 修改第一个任务的状态为已完成；\n" +
		"4. 再次列出待办事项确认修改；\n" +
		"5. 最后输出一句中文总结。"

	result, err := agent.Invoke(ctx, map[string]any{"query": query},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.NotEmpty(output, "output 不应为空")

	// 对齐 Python: tool_counts 断言
	s.GreaterOrEqual(toolTrace.GetExecutionCount("todo_create"), 1,
		"todo_create 应至少调用 1 次")
	s.GreaterOrEqual(toolTrace.GetExecutionCount("todo_list"), 2,
		"todo_list 应至少调用 2 次")
	s.GreaterOrEqual(toolTrace.GetExecutionCount("todo_modify"), 1,
		"todo_modify 应至少调用 1 次")
	s.GreaterOrEqual(toolTrace.TotalCalls(), 4, "总工具调用数应 ≥ 4")
}

// TestTodoRealDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *TodoRealSuite) TestTodoRealDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(todo.CreateTodosTool)
	s.NotNil(testhelpers.NewToolTraceRail)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// realLLMTestsEnabled 检查 RUN_REAL_LLM_TESTS 开关是否开启。
// 对齐 Python: os.getenv("RUN_REAL_LLM_TESTS", "0") != "1" → pytest.skip
func (s *TodoRealSuite) realLLMTestsEnabled() bool {
	return os.Getenv("RUN_REAL_LLM_TESTS") == "1"
}

// skipUnlessRealLLMTests 在 RUN_REAL_LLM_TESTS 未开启时跳过测试。
// 对齐 Python: pytest.skip("Real LLM test skipped. Set RUN_REAL_LLM_TESTS=1 to enable.")
func (s *TodoRealSuite) skipUnlessRealLLMTests() {
	if !s.realLLMTestsEnabled() {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
}
