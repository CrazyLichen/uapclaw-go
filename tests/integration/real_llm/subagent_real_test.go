//go:build integration && llm

package real_llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
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

// SubagentRealSuite 真实 LLM 下子代理委派 E2E 测试套件。
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_subagent.py
// （TestDeepAgentSubagentRail 2 个 + TestDeepAgentSessionRail 场景 1/4，
// 全部 @unittest.skip("skip system test")，Python 从未运行）
//
// 覆盖：
//   - 同步 task_tool 委派：主 agent 调用 subagent 写文件、主 agent 读文件
//   - 异步 sessions_spawn：后台任务完成后 toolkit 状态轮询
//
// Go 与 Python 差异：
//   - Python fs_rail = SysOperationRail()（Runner 级 sys_operation）；
//     Go 侧每个测试注册独立 LocalSysOperation（t.TempDir 工作目录），
//     teardown 时 RemoveSysOperation 清理注册的 fs/shell/code 工具。
//   - Python ToolTraceRail 为测试文件内自定义类；
//     Go 用 testhelpers.ToolTraceRail（等价物）。
type SubagentRealSuite struct {
	isuite.RealLLMSuite
	// sysOperationID 当前测试注册的系统操作 ID
	sysOperationID string
	// workDir 当前测试的工作目录
	workDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSubagentRealSuite 运行真实 LLM 子代理委派测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestSubagentRealSuite(t *testing.T) {
	suite.Run(t, new(SubagentRealSuite))
}

// SetupTest 为每个测试方法注册独立 SysOperation。
// 对齐 Python asyncSetUp：deepagent_sysop_<uuid> + LocalWorkConfig(work_dir=tmp)。
// 守卫放在测试方法内（skipUnlessRealLLMTests），避免 SetupTest 的
// SkipIfNoRealLLM 连带跳过包引用验证测试。
func (s *SubagentRealSuite) SetupTest() {
	if !s.realLLMTestsEnabled() {
		return
	}
	s.SkipIfNoRealLLM()

	s.workDir = s.T().TempDir()
	s.sysOperationID = fmt.Sprintf("subagent_real_sysop_%d", time.Now().UnixNano())

	card := sys_operation.NewSysOperationCard()
	sysOp, err := sys_operation.NewSysOperation(card)
	s.Require().NoError(err, "创建 SysOperation 失败")

	err = runner.GetResourceMgr().AddSysOperation(s.sysOperationID, sysOp)
	s.Require().NoError(err, "注册 SysOperation 失败")
}

// TearDownTest 注销 SysOperation。
// 对齐 Python asyncTearDown：remove_sys_operation + tmp.cleanup。
// sysOperationID 为空（守卫未通过）时直接返回。
func (s *SubagentRealSuite) TearDownTest() {
	if s.sysOperationID == "" {
		return
	}
	// 忽略错误：清理失败不应掩盖原始断言
	_ = runner.GetResourceMgr().RemoveSysOperation([]string{s.sysOperationID})
	s.sysOperationID = ""
}

// TestSubagentReal_真实LLM_同步task_tool委派 测试主 agent 通过 task_tool 调用
// research_agent 完成调研并写文件、主 agent 读取结果。
// 对齐 Python: test_deep_agent_tasks_using_subagents
//
// 断言对齐 Python 原文：
//   - task_tool 调用 ≥ 1（主 agent 委派）
//   - read_file 调用 ≥ 1（主 agent 读取）
//   - summary_research.txt 文件存在（subagent 与主 agent 共享 workspace）
func (s *SubagentRealSuite) TestSubagentReal_真实LLM_同步task_tool委派() {
	s.skipUnlessRealLLMTests()
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 10*time.Minute)
	defer cancel()

	// 对齐 Python: fs_rail = SysOperationRail() + tool_trace = ToolTraceRail()
	fsRail := rails.NewSysOperationRail()
	toolTrace := testhelpers.NewToolTraceRail()

	// 对齐 Python: research_agent = SubAgentConfig(agent_card=..., system_prompt=..., rails=[fs_rail])
	researchAgent := &hschema.SubAgentConfig{
		AgentCard: agentschema.NewAgentCard(
			agentschema.WithAgentName("research_agent"),
			agentschema.WithAgentDescription("专注于研究调查任务，当用户想要调查某问题时，可使用该代理执行研究工作。每次只给这位研究员一个主题。"),
		),
		SystemPrompt: "你是一名研究助理，负责针对用户输入的主题开展研究工作。",
		Rails:        []agentinterfaces.AgentRail{fsRail},
	}

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("subagent_real_sync"),
		agentschema.WithAgentName("SubagentRealSyncAgent"),
	)
	// workspace 用默认（cwd 根）；fs 工具路径在 query 中以绝对路径给出，
	// 与 Python LocalWorkConfig(work_dir=tmp) 语义等价（绝对路径不经过 cwd 解析）
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		SystemPrompt:  "你是一个严谨的任务执行助手。当用户要求用工具处理文件时，必须调用工具，不要凭空假设。",
		Subagents:     []hschema.SubagentSpec{researchAgent},
		Rails:         []agentinterfaces.AgentRail{toolTrace, fsRail},
		SysOperation:  s.getSysOperation(),
		MaxIterations: 12,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("subagent_real_sync_session"),
		session.WithEnvs(map[string]any{}),
		session.WithCheckpointer(checkpointer.NewInMemoryCheckpointer()),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	defer sess.PostRun(ctx)

	// 对齐 Python query 原文（summary_research.txt 写到工作目录下）
	summaryPath := filepath.Join(s.workDir, "summary_research.txt")
	query := "请严格按顺序执行以下任务，并且每一步都必须调用工具：\n" +
		"1. 调查随机森林算法应用场景，创建summary_research.txt文件，写入内容为调查结果；\n" +
		"2. 使用工具读取 summary_research.txt 文件；\n" +
		"3. 返回文件的结果\n" +
		"summary_research.txt 的绝对路径是：" + summaryPath

	result, err := agent.Invoke(ctx, map[string]any{"query": query},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)

	// 对齐 Python: tool_counts 断言
	s.GreaterOrEqual(toolTrace.GetExecutionCount("task_tool"), 1,
		"主 agent 应至少调用一次 task_tool 委派子代理")
	s.GreaterOrEqual(toolTrace.GetExecutionCount("read_file"), 1,
		"主 agent 应至少调用一次 read_file")

	_, statErr := os.Stat(summaryPath)
	s.NoError(statErr, "summary_research.txt 应由 subagent 创建（共享 workspace）")
}

// TestSubagentReal_真实LLM_异步spawn后台任务 测试 enable_async_subagent 下
// sessions_spawn 提交后台任务，任务完成后 toolkit 状态轮询为 completed。
// 对齐 Python: TestDeepAgentSessionRail.test_auto_invoke_on_spawn_done_no_query2（场景 1 简化）
//
// 断言对齐 Python 原文（轮询语义）：
//   - toolkit.ListAll() 非空且全部任务达到 completed/error
func (s *SubagentRealSuite) TestSubagentReal_真实LLM_异步spawn后台任务() {
	s.skipUnlessRealLLMTests()
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 10*time.Minute)
	defer cancel()

	// 对齐 Python: SubAgentConfig(AgentCard(name="research_agent"...), rails=[SysOperationRail()], ...)
	researchAgent := &hschema.SubAgentConfig{
		AgentCard: agentschema.NewAgentCard(
			agentschema.WithAgentName("research_agent"),
			agentschema.WithAgentDescription("专注于研究调查任务，当用户想要调查某问题时，可使用该代理执行研究工作。每次只给这位研究员一个主题。"),
		),
		SystemPrompt: "你是研究助理，负责围绕用户输入的主题开展调研，仅需返回最终研究结果。",
		Rails:        []agentinterfaces.AgentRail{rails.NewSysOperationRail()},
	}

	card := agentschema.NewAgentCard(
		// 唯一卡片 ID：session 工具的 resource ID 格式为 sessions_spawn_<agentID>，
		// 与其他 suite 的注册冲突会导致旧工具实例（闭包绑定旧 toolkit）被复用
		agentschema.WithAgentID(fmt.Sprintf("subagent_real_async_%d", time.Now().UnixNano())),
		agentschema.WithAgentName("SubagentRealAsyncAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:                   card,
		Model:                  model,
		SystemPrompt:           "你是一个严谨的任务执行助手。当用户要求用工具处理文件时，必须调用工具，不要凭空假设。",
		Subagents:              []hschema.SubagentSpec{researchAgent},
		Rails:                  []agentinterfaces.AgentRail{},
		EnableTaskLoop:         true,
		EnableAsyncSubagent:    true,
		AddGeneralPurposeAgent: true,
		SysOperation:           s.getSysOperation(),
		MaxIterations:          20,
		CompletionTimeout:      60,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("subagent_real_async_session"),
		session.WithEnvs(map[string]any{}),
		session.WithCheckpointer(checkpointer.NewInMemoryCheckpointer()),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	defer sess.PostRun(ctx)

	// 对齐 Python q1 原文
	q1 := "提交后台任务：分析Chipotle为什么还没有进入中国市场，不要写入文件！"
	result, err := agent.Invoke(ctx, map[string]any{"query": q1},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	s.False(agent.IsInvokeActive(), "主 round 完成后 is_invoke_active 应为 false")

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit, "SessionToolkit 应已注入")

	// 对齐 Python: max_wait=300 / wait_interval=5 轮询全部任务完成
	done := false
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		rows := toolkit.ListAll()
		if len(rows) > 0 {
			allSettled := true
			for _, row := range rows {
				if row.Status != "completed" && row.Status != "error" {
					allSettled = false
					break
				}
			}
			if allSettled {
				time.Sleep(2 * time.Second)
				done = true
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
	s.True(done, "spawn 的后台任务未在超时内完成（对齐 Python: spawn 未在超时内完成）")
}

// TestSubagentRealDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *SubagentRealSuite) TestSubagentRealDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(rails.NewSysOperationRail)
	s.NotNil(hschema.SubAgentConfig{})
	s.NotNil(testhelpers.NewToolTraceRail)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// realLLMTestsEnabled 检查 RUN_REAL_LLM_TESTS 开关是否开启。
// 对齐 Python: os.getenv("RUN_REAL_LLM_TESTS", "0") != "1" → pytest.skip
func (s *SubagentRealSuite) realLLMTestsEnabled() bool {
	return os.Getenv("RUN_REAL_LLM_TESTS") == "1"
}

// skipUnlessRealLLMTests 在 RUN_REAL_LLM_TESTS 未开启时跳过测试。
// 对齐 Python: pytest.skip("Real LLM test skipped. Set RUN_REAL_LLM_TESTS=1 to enable.")
func (s *SubagentRealSuite) skipUnlessRealLLMTests() {
	if !s.realLLMTestsEnabled() {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
}

// getSysOperation 返回 SetupTest 注册的 SysOperation 实例。
// 对齐 Python: Runner.resource_mgr.get_sys_operation(self._sys_operation_id)
func (s *SubagentRealSuite) getSysOperation() sys_operation.SysOperation {
	instances, err := runner.GetResourceMgr().GetSysOperation([]string{s.sysOperationID})
	s.Require().NoError(err, "获取 SysOperation 失败")
	s.Require().NotEmpty(instances, "SysOperation 应已注册")
	return instances[0]
}
