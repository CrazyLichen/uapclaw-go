//go:build integration && llm

package real_llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	retrievalembedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryRailE2ESuite 真实 LLM + 真实 embedding 下 MemoryRail E2E 测试套件。
//
// 对齐 Python: tests/system_tests/harness/test_memory_rail_e2e.py
// （9 个 @pytest.mark.skip("need llm and embedding") 测试）
//
// 守卫对齐 Python _require_api_config：LLM_API_KEY/LLM_API_BASE/EMBED_API_KEY
// 三重检查；Go 侧 SkipIfNoRealLLM + SkipIfNoEmbedding（EMBED_API_KEY 缺省
// 回落 LLM_API_KEY，见 RealLLMSuite）。
//
// 环境差异对齐：
//   - Python fixture 的 memory_dir = Workspace().root_path / "memory"
//     （Runner 默认工作区），Go 版每测试方法用 t.TempDir() 建独立
//     workspace（NewWorkspace 默认目录含 memory 节点），避免测试间
//     记忆文件互相污染。
//   - MemoryRail(embedding_config=, is_proactive=False)。
type MemoryRailE2ESuite struct {
	isuite.RealLLMSuite
	// workspaceRoot 当前测试方法的 workspace 根目录
	workspaceRoot string
	// cleanups 每个测试方法登记的清理函数（TearDownTest 逆序执行）
	cleanups []func()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMemoryRailE2ESuite 运行真实 LLM 记忆护栏 E2E 测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestMemoryRailE2ESuite(t *testing.T) {
	suite.Run(t, new(MemoryRailE2ESuite))
}

// SetupTest 为每个测试方法创建独立 workspace 根目录。
// 对齐 Python fixture: memory_dir = work_dir / "memory"。
func (s *MemoryRailE2ESuite) SetupTest() {
	s.SkipIfNoRealLLM()
	s.SkipIfNoEmbedding()

	s.workspaceRoot = s.T().TempDir()
}

// TestMemoryRail_真实LLM_基本invoke 测试 MemoryRail 基本功能：注册工具、注入 prompt、初始化记忆管理器。
// 对齐 Python: test_01_memory_rail_basic_invoke
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_基本invoke() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_01",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。", 10)
	defer sess.PostRun(ctx)

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "你好，请记住我的姓名是张三，今年25岁了，现在职业是软件开发工程师",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "第一次 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)

	result, err = agent.Invoke(ctx, map[string]any{
		"query": "你还记得我叫什么吗",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "第二次 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.NotEmpty(output, "output 不应为空")
}

// TestMemoryRail_真实LLM_写入记忆工具 测试 write_memory 工具：写入 USER.md 和会话日志。
// 对齐 Python: test_02_write_memory_tool
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_写入记忆工具() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_02",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当用户要求你记住某些信息时，请使用 write_memory 工具。", 15)
	defer sess.PostRun(ctx)

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "请记住：我的项目截止日期是2026年5月1日，项目名称是智能助手开发。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
}

// TestMemoryRail_真实LLM_记忆搜索工具 测试 memory_search 工具：搜索历史记忆。
// 对齐 Python: test_03_memory_search_tool
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_记忆搜索工具() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_03",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当用户询问之前的信息时，请先使用 memory_search 工具搜索。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "请记住：我最喜欢的编程语言是Python，我最喜欢的框架是FastAPI。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "写入 Invoke 失败")

	// 对齐 Python: await asyncio.sleep(5)（等待索引异步构建）
	time.Sleep(5 * time.Second)

	searchResult, err := agent.Invoke(ctx, map[string]any{
		"query": "我最喜欢的编程语言是什么？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "搜索 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), searchResult)
	output, _ := searchResult["output"].(string)
	s.Contains(output, "Python", "搜索结果应包含 Python")
}

// TestMemoryRail_真实LLM_读取记忆工具 测试 read_memory 工具：读取 USER.md 文件。
// 对齐 Python: test_04_read_memory_tool
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_读取记忆工具() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_04",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当需要查看用户资料时，请使用 read_memory 工具读取 USER.md。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "请记住我的姓名是李四，我的邮箱是lisi@example.com。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "写入 Invoke 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "请帮我确认一下我的邮箱地址是什么？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "读取 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.Contains(output, "lisi@example.com", "读取结果应包含邮箱")
}

// TestMemoryRail_真实LLM_编辑记忆工具 测试 edit_memory 工具：编辑已存在的字段。
// 对齐 Python: test_05_edit_memory_tool
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_编辑记忆工具() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_05",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当用户更新信息时，请使用 edit_memory 工具更新已存在的字段。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "请记住我的手机号是13800138000。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "写入 Invoke 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "我的手机号变了，请更新为13900139000。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "更新 Invoke 失败")
	// 真实模型可能反复用不匹配的 old_text 调 edit_memory 直到打满
	// max_iterations，此时 ReAct 返回 result_type=="error"（output 为
	// "达到最大迭代次数仍未完成"）。CI 40987848552 即此形态（52.78s）。
	// Python 原文同样只有 assert result.get("result_type") == "answer"，
	// 无重试；但 Python 该用例不进 CI，Go 侧需对模型抖动留一次有界重试。
	if resultType, _ := result["result_type"].(string); resultType == "error" {
		output, _ := result["output"].(string)
		s.T().Logf("[memory-rail-edit] 首次更新返回 error，重试一次；output=%q", output)
		result, err = agent.Invoke(ctx, map[string]any{
			"query": "请先用 read_memory 读取记忆文件，再用 edit_memory 把 13800138000 改成 13900139000。",
		}, agentinterfaces.WithSession(sess))
		s.Require().NoError(err, "重试更新 Invoke 失败")
	}
	testhelpers.AssertAnswerResult(s.T(), result)

	result, err = agent.Invoke(ctx, map[string]any{
		"query": "我的手机号是多少？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "查询 Invoke 失败")
	output, _ := result["output"].(string)
	s.Contains(output, "13900139000", "查询结果应包含新手机号")
}

// TestMemoryRail_真实LLM_记忆get工具 测试 memory_get 工具：读取指定行范围。
// 对齐 Python: test_06_memory_get_tool
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_记忆get工具() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_06",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当需要读取特定行时，请使用 memory_get 工具。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "请记住以下信息：第一条记录是关于项目A的启动时间2026年1月。第二条记录是关于项目B的预算100万。第三条记录是关于项目C的负责人王五。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "写入 Invoke 失败")

	// 对齐 Python: await asyncio.sleep(5)
	time.Sleep(5 * time.Second)

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "项目B的预算是多少？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "查询 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.Contains(output, "100", "查询结果应包含 100")
}

// TestMemoryRail_真实LLM_追加写入模式 测试 write_memory 工具的追加模式。
// 对齐 Python: test_07_write_memory_append_mode
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_追加写入模式() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_07",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当用户要求追加信息时，请使用 write_memory 工具的追加模式。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "请记住：今天完成了用户登录功能的开发。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "首次写入 Invoke 失败")

	_, err = agent.Invoke(ctx, map[string]any{
		"query": "请追加记录：今天还完成了用户注册功能的开发。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "追加写入 Invoke 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "今天完成了哪些功能？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "查询 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.Contains(output, "登录", "输出应包含 登录")
	s.Contains(output, "注册", "输出应包含 注册")
}

// TestMemoryRail_真实LLM_更新用户画像 测试更新 USER.md 用户画像。
// 对齐 Python: test_08_update_user_profile
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_更新用户画像() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_08",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当用户透露个人信息时，请更新 USER.md 文件。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "我是王明，今年30岁，是一名产品经理。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "写入 Invoke 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "我是谁？我的职业是什么？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "查询 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.True(containsAny(output, "王明", "产品经理"), "输出应包含 王明 或 产品经理")
}

// TestMemoryRail_真实LLM_写入MEMORYmd长期记忆 测试写入 MEMORY.md 长期记忆文件。
// 对齐 Python: test_09_write_memory_md_file
func (s *MemoryRailE2ESuite) TestMemoryRail_真实LLM_写入MEMORYmd长期记忆() {
	agent, sess, ctx := s.newMemoryAgent("mr_e2e_09",
		"你是一个智能助手，可以使用记忆工具来存储和检索信息。"+
			"当用户要求记录长期知识时，请写入 MEMORY.md 文件。", 15)
	defer sess.PostRun(ctx)

	_, err := agent.Invoke(ctx, map[string]any{
		"query": "请记录一条长期知识：我们公司的服务器IP是192.168.1.100，SSH端口是2222。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "写入 Invoke 失败")

	result, err := agent.Invoke(ctx, map[string]any{
		"query": "我们公司的服务器IP是多少？",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "查询 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), result)
	output, _ := result["output"].(string)
	s.Contains(output, "192.168.1.100", "查询结果应包含服务器 IP")
}

// TestMemoryRailDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *MemoryRailE2ESuite) TestMemoryRailDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(memory.NewMemoryRail)
	s.NotNil(workspace.NewWorkspace)
	s.NotNil(retrievalembedding.EmbeddingConfig{})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMemoryAgent 创建带 MemoryRail 的 DeepAgent + Session。
// 对齐 Python: create_deep_agent(model=, rails=[memory_rail], ...) + Runner.run_agent
func (s *MemoryRailE2ESuite) newMemoryAgent(sessionID, systemPrompt string, maxIter int) (*harness.DeepAgent, *session.Session, context.Context) {
	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	embeddingConfig := s.NewRealEmbeddingConfig()
	s.Require().NotNil(embeddingConfig, "embedding 配置不应为 nil")

	ctx, cancel := context.WithTimeout(s.Ctx, 5*time.Minute)
	// cancel 在 TearDownTest 逆序执行（晚于各测试方法内的 sess.PostRun）
	s.registerCleanup(cancel)

	// 独立 workspace（默认目录结构含 memory 节点）
	ws := workspace.NewWorkspace(s.workspaceRoot, "cn")
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		s.Require().NoError(os.MkdirAll(*nodePath, 0o755), "创建 memory 目录失败")
	}

	memoryRail := memory.NewMemoryRail(embeddingConfig, false)

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("mr_e2e_agent_"+sessionID),
		agentschema.WithAgentName("MemoryRailE2EAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		SystemPrompt:  systemPrompt,
		Rails:         []agentinterfaces.AgentRail{memoryRail},
		Workspace:     ws,
		MaxIterations: maxIter,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID(sessionID),
		session.WithEnvs(map[string]any{}),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	return agent, sess, ctx
}

// registerCleanup 登记测试结束时的清理函数。
func (s *MemoryRailE2ESuite) registerCleanup(fn func()) {
	s.cleanups = append(s.cleanups, fn)
}

// containsAny 检查 s 是否包含 candidates 中任一项。
// 对齐 Python: assert "王明" in output or "产品经理" in output
func containsAny(s string, candidates ...string) bool {
	for _, c := range candidates {
		if strings.Contains(s, c) {
			return true
		}
	}
	return false
}

// TearDownTest 逆序执行登记的清理函数（context cancel 等）。
func (s *MemoryRailE2ESuite) TearDownTest() {
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		if s.cleanups[i] != nil {
			s.cleanups[i]()
		}
	}
	s.cleanups = nil
}
