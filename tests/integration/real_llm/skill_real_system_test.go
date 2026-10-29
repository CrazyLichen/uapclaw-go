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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillRealSystemSuite 真实 LLM 下技能子系统 E2E 测试套件。
//
// 对齐 Python: tests/system_tests/agent/skill/test_skill_real_system.py
// 的 test_end_to_end_real_llm（RUN_REAL_LLM_TESTS=1 开关守卫）。
//
// 差异说明：Python 原文用 MockFS + MockSysOperation 补丁虚拟文件系统；
// Go 版直接使用真实 LocalSysOperation（t.TempDir 工作目录），覆盖同一
// 原生 sys_operation 工具链路：
//   - read_file: 读文本文件
//   - execute_code: 执行 python 代码并返回输出
//   - execute_cmd: 执行命令并返回 stdout
//
// Go 端守卫映射：RUN_REAL_LLM_TESTS=1（对齐 Python 语义）且要求
// LLM_API_KEY 已设置（SkipIfNoRealLLM）。
type SkillRealSystemSuite struct {
	isuite.RealLLMSuite
	// sysOperationID 注册到 ResourceMgr 的系统操作 ID
	sysOperationID string
	// tempRoot 临时工作根目录（sys_operation 的文件都在此下）
	tempRoot string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillRealSystemSuite 运行真实 LLM 技能子系统 E2E 测试套件
// 运行方式: RUN_REAL_LLM_TESTS=1 go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestSkillRealSystemSuite(t *testing.T) {
	suite.Run(t, new(SkillRealSystemSuite))
}

// SetupTest 每个测试方法创建独立临时目录与 SysOperation。
// 对齐 Python asyncSetUp 中的 tmp 目录 + sys_operation 注册。
// 注意：RUN_REAL_LLM_TESTS 守卫在测试方法内调用（skipUnlessRealLLMTests），
// 不放在 SetupTest，避免包引用验证测试被连带跳过。
func (s *SkillRealSystemSuite) SetupTest() {
	if !s.realLLMTestsEnabled() {
		return
	}
	s.SkipIfNoRealLLM()

	s.tempRoot = s.T().TempDir()
	s.sysOperationID = fmt.Sprintf("ut_skill_sysop_%d", time.Now().UnixNano())

	// card.ID 必须等于注册 ID：registerSysOperationTools 以 card.ID 前缀
	// 生成工具 ID（{card.ID}.fs.read_file），而 getSysOpTool 用
	// GenerateStaticToolID(sysOperationID, ...) 查找——两者不一致时
	// 工具查不到（CI 首跑 40856190636 实证：随机 UUID card.ID 导致
	// 0.00s 失败"未找到 sys_operation 工具"）。
	// 对齐 Python: SysOperationCard(id="sys_op", ...) 调用方自定 ID，
	// add_sys_operation 以 card.id 注册（resource_manager.py L724），
	// 工具 ID 全链路同前缀。
	card := sys_operation.NewSysOperationCard()
	card.BaseCard.ID = s.sysOperationID
	sysOp, err := sys_operation.NewSysOperation(card)
	s.Require().NoError(err, "创建 SysOperation 失败")

	err = runner.GetResourceMgr().AddSysOperation(s.sysOperationID, sysOp)
	s.Require().NoError(err, "注册 SysOperation 失败")
}

// TearDownTest 注销 SysOperation 并清理关联工具。
// 对齐 Python asyncTearDown 的 Runner.stop + tmp.cleanup。
// sysOperationID 为空（守卫未通过未注册）时直接返回。
func (s *SkillRealSystemSuite) TearDownTest() {
	if s.sysOperationID == "" {
		return
	}
	// 忽略错误：测试已失败时清理失败不应掩盖原始断言
	_ = runner.GetResourceMgr().RemoveSysOperation([]string{s.sysOperationID})
}

// TestSkillReal_真实LLM_注册并使用原生工具链 测试真实 LLM 下技能注册 + 原生工具三连调用。
// 对齐 Python: test_end_to_end_real_llm
//
// 覆盖（对齐 Python 注释）：
//   - register_skill: 从 skills_root 注册技能（Go 用 SkillUtil.RegisterSkills）
//   - read_file: 读文本文件，output 应包含文件内容
//   - execute_code: 运行 python 并返回输出（123+456=579）
//   - execute_cmd: 运行命令并返回 stdout（echo hello_cmd）
//
// 注意：Go 端 SkillUseRail 为 placeholder（9.8-9.24 待回填），技能提示词
// 通过 SkillUtil.GetSkillPrompt 拼入 SystemPrompt（对齐 Python SkillUtil 层语义）。
func (s *SkillRealSystemSuite) TestSkillReal_真实LLM_注册并使用原生工具链() {
	s.skipUnlessRealLLMTests()
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 5*time.Minute)
	defer cancel()

	// ---- 准备技能目录与样本文件（真实文件，替代 Python MockFS）----
	skillsRoot := filepath.Join(s.tempRoot, "skills")
	goodSkillDir := filepath.Join(skillsRoot, "good_skill")
	s.Require().NoError(os.MkdirAll(goodSkillDir, 0o755))
	skillMD := "---\nname: good_skill\ndescription: UT mock skill description\n---\n\n# Good Skill\n测试技能文档。\n"
	s.Require().NoError(os.WriteFile(filepath.Join(goodSkillDir, "SKILL.md"), []byte(skillMD), 0o600))

	filesDir := filepath.Join(s.tempRoot, "files")
	s.Require().NoError(os.MkdirAll(filesDir, 0o755))
	sampleTxt := filepath.Join(filesDir, "a.txt")
	s.Require().NoError(os.WriteFile(sampleTxt, []byte("hello_skill_tool"), 0o600))

	// ---- 技能注册（对齐 Python agent.register_skill(skills_root_ok)）----
	skillUtil := skills.NewSkillUtil(s.sysOperationID)
	err = skillUtil.RegisterSkills([]string{skillsRoot}, false)
	s.Require().NoError(err, "注册技能失败")
	s.True(skillUtil.HasSkill(), "应注册至少一个技能")

	// ---- 拉取原生 sys_operation 工具（对齐 Python get_sys_op_tool_cards）----
	readFileTool, err := s.getSysOpTool("fs", "read_file")
	s.Require().NoError(err)
	executeCodeTool, err := s.getSysOpTool("code", "execute_code")
	s.Require().NoError(err)
	executeCmdTool, err := s.getSysOpTool("shell", "execute_cmd")
	s.Require().NoError(err)

	// ---- 组装 SystemPrompt（Python 原文 + 技能提示词）----
	systemPrompt := "You are an intelligent assistant.\n" +
		"You MUST call tools when the user asks you to use them.\n" +
		"When asked to read a file, you MUST use read_file.\n" +
		"When asked to run Python, you MUST use execute_code.\n" +
		"When asked to run a command, you MUST use execute_cmd.\n" +
		"Return ONLY the final answer content.\n" +
		skillUtil.GetSkillPrompt()

	// ---- 创建 DeepAgent（对齐 Python ReActAgent + configure）----
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("ut_skill_agent"),
		agentschema.WithAgentName("ut_skill_agent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		SystemPrompt:  systemPrompt,
		ToolInstances: []tool.Tool{readFileTool, executeCodeTool, executeCmdTool},
		MaxIterations: 10,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("ut_skill_session_e2e"),
		session.WithEnvs(map[string]any{}),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	defer sess.PostRun(ctx)

	// ---- q1: read_file（对齐 Python q1）----
	q1 := "Use read_file to read this file and output its content exactly:\n" + sampleTxt
	r1, err := agent.Invoke(ctx, map[string]any{"query": q1}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "read_file 轮 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), r1)
	output1, _ := r1["output"].(string)
	s.Contains(output1, "hello_skill_tool", "read_file 轮 output 应包含文件内容")

	// ---- q2: execute_code（对齐 Python q2）----
	q2 := "Use execute_code to run python code and output the result only:\nprint(123 + 456)"
	r2, err := agent.Invoke(ctx, map[string]any{"query": q2}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "execute_code 轮 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), r2)
	output2, _ := r2["output"].(string)
	s.Contains(output2, "579", "execute_code 轮 output 应包含 579")

	// ---- q3: execute_cmd（对齐 Python q3）----
	q3 := "Use execute_cmd to execute the following command and output stdout only:\necho hello_cmd"
	r3, err := agent.Invoke(ctx, map[string]any{"query": q3}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "execute_cmd 轮 Invoke 失败")
	testhelpers.AssertAnswerResult(s.T(), r3)
	output3, _ := r3["output"].(string)
	s.Contains(output3, "hello_cmd", "execute_cmd 轮 output 应包含 hello_cmd")
}

// TestSkillRealDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *SkillRealSystemSuite) TestSkillRealDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(skills.NewSkillUtil)
	s.NotNil(sys_operation.NewSysOperation)
	s.NotNil(sys_operation.GenerateStaticToolID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// realLLMTestsEnabled 检查 RUN_REAL_LLM_TESTS 开关是否开启。
// 对齐 Python: os.getenv("RUN_REAL_LLM_TESTS", "0") != "1" → pytest.skip
func (s *SkillRealSystemSuite) realLLMTestsEnabled() bool {
	return os.Getenv("RUN_REAL_LLM_TESTS") == "1"
}

// skipUnlessRealLLMTests 在 RUN_REAL_LLM_TESTS 未开启时跳过测试。
// 对齐 Python: pytest.skip("Real LLM test skipped. Set RUN_REAL_LLM_TESTS=1 to enable.")
func (s *SkillRealSystemSuite) skipUnlessRealLLMTests() {
	if !s.realLLMTestsEnabled() {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
}

// getSysOpTool 按操作名和工具名拉取已注册的原生 sys_operation 工具实例。
// 对齐 Python: Runner.resource_mgr.get_sys_op_tool_cards(sys_operation_id=, operation_name=, tool_name=)
func (s *SkillRealSystemSuite) getSysOpTool(operationName, toolName string) (tool.Tool, error) {
	toolID := sys_operation.GenerateStaticToolID(s.sysOperationID, operationName, toolName)
	tools, err := runner.GetResourceMgr().GetTool([]string{toolID})
	if err != nil {
		return nil, err
	}
	if len(tools) == 0 {
		return nil, fmt.Errorf("未找到 sys_operation 工具: %s", toolID)
	}
	return tools[0], nil
}
