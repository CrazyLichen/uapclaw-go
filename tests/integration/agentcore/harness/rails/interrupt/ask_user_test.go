//go:build integration

package interrupt_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserSuite AskUserRail 集成测试套件。
// 对照 Python: tests/unit_tests/harness/rails/test_deep_agent_ask_user.py
//
// 测试 AskUserRail 的完整中断/恢复流程：
//  1. agent.Invoke → MockLLM 返回 ask_user tool_call → AskUserRail 拦截 → result_type=="interrupt"
//  2. agent.Invoke(query=InteractiveInput) → 用户通过 answers 回答 → result_type=="answer"
//
// 运行方式: go test -tags=integration ./tests/integration/agentcore/harness/rails/interrupt/...
type AskUserSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAskUserSuite 运行 AskUserRail 集成测试套件
func TestAskUserSuite(t *testing.T) {
	suite.Run(t, new(AskUserSuite))
}

// TestAskUserRail_Init注册工具 测试 Invoke 触发 Init 后 ask_user 工具注册到 AbilityManager。
// 对照 Python: TestStructuredAskUserRailLifecycle.test_init_registers_tool_in_ability_manager
//
// 注意：AskUserRail.Init 通过 pendingRails 在首次 Invoke 时延迟执行，
// 因此需要先 Invoke 一次才能在 AbilityManager 中看到注册的工具。
func (s *AskUserSuite) TestAskUserRail_Init注册工具() {
	rail := interrupt.NewAskUserRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 pendingRails → Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 ask_user"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ask_user 工具已注册到 AbilityManager
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("ask_user"), "ask_user 工具应已注册")
}

// TestAskUserRail_Uninit注销工具 测试 Uninit 从 AbilityManager 注销 ask_user 工具。
// 对照 Python: TestStructuredAskUserRailLifecycle.test_uninit_removes_tool_from_ability_manager
func (s *AskUserSuite) TestAskUserRail_Uninit注销工具() {
	rail := interrupt.NewAskUserRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 pendingRails → Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 ask_user"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证工具已注册
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("ask_user"), "Init 后 ask_user 工具应存在")

	// Uninit
	err = rail.Uninit(agent)
	s.Require().NoError(err, "Uninit 失败")

	// 验证工具已注销
	s.Nil(am.Get("ask_user"), "Uninit 后 ask_user 工具应为 nil")
}

// TestAskUserRail_单问题中断恢复 测试单问题 ask_user 工具调用的完整中断/恢复流程。
// 对照 Python: test_hitl_ask_user_rail（单问题中断→用户回答→恢复执行）
func (s *AskUserSuite) TestAskUserRail_单问题中断恢复() {
	ctx := s.Ctx

	rail := interrupt.NewAskUserRail()

	// MockLLM: 第 1 轮调用 ask_user 工具（触发中断），第 2 轮返回文本响应（恢复后完成）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("ask_user", `{"query":"请询问用户","questions":[{"header":"文件名","question":"你想用什么文件名？","options":[{"label":"file1.txt","description":"默认文件名"},{"label":"file2.txt","description":"备用文件名"}]}]}`),
		mockllm.CreateTextResponse("好的，用户选择的文件名是 user_choice.txt"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("askuser-single")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发 ask_user 中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user 工具询问用户文件名"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, stateList := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")

	// 验证 state 中包含 AskUserRail 的中断信息（questions 字段）
	s.Require().NotEmpty(stateList, "state 列表不应为空")

	// 第 2 步：用户通过 InteractiveInput 回答问题
	// 对齐 Python: InteractiveInput(user_inputs={tool_call_id: {"answers": {"你想用什么文件名？": "user_choice.txt"}}})
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: map[string]any{
				"answers": map[string]any{
					"你想用什么文件名？": "user_choice.txt",
				},
			},
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)

	sess.PostRun(ctx)
}

// TestAskUserRail_多问题中断恢复 测试多问题 ask_user 工具调用的完整中断/恢复流程。
// 对照 Python: test_hitl_ask_user_rail_multi_question
func (s *AskUserSuite) TestAskUserRail_多问题中断恢复() {
	ctx := s.Ctx

	rail := interrupt.NewAskUserRail()

	// MockLLM: 返回包含多个问题的 ask_user 工具调用
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("ask_user", `{"query":"请选择方案","questions":[{"header":"框架","question":"你使用哪个框架？","options":[{"label":"React","description":"前端框架"},{"label":"Vue","description":"渐进式框架"}]},{"header":"认证","question":"使用哪种认证方式？","options":[{"label":"JWT","description":"Token认证"},{"label":"Session","description":"会话认证"}]}]}`),
		mockllm.CreateTextResponse("好的，用户选择了 React + JWT"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("askuser-multi")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：Invoke 触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user 询问用户偏好"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")
	s.Require().NotNil(result, "结果不应为 nil")

	interruptIDs, stateList := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs, "应有至少一个 interrupt_id")
	s.Require().NotEmpty(stateList, "state 列表不应为空")

	// 第 2 步：用户回答多个问题
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: map[string]any{
				"answers": map[string]any{
					"你使用哪个框架？": "React",
					"使用哪种认证方式？": "JWT",
				},
			},
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)

	sess.PostRun(ctx)
}

// TestAskUserRail_字符串回答恢复 测试用户以纯字符串形式回答（单问题自动匹配）。
// 对照 Python: test_resume_with_string_directly_returns_formatted_result
func (s *AskUserSuite) TestAskUserRail_字符串回答恢复() {
	ctx := s.Ctx

	rail := interrupt.NewAskUserRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("ask_user", `{"query":"请询问","questions":[{"header":"语言","question":"你喜欢什么编程语言？","options":[]}]}`),
		mockllm.CreateTextResponse("好的，用户喜欢 Go"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("askuser-string")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user 询问编程语言偏好"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs)

	// 第 2 步：用户以纯字符串回答（对齐 Python 中 string 直接传入的场景）
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: "Go",
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)

	sess.PostRun(ctx)
}

// TestAskUserRail_无效输入重新中断 测试用户提供无效输入时重新触发中断。
// 对照 Python: test_invalid_user_input_returns_interrupt
func (s *AskUserSuite) TestAskUserRail_无效输入重新中断() {
	ctx := s.Ctx

	rail := interrupt.NewAskUserRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("ask_user", `{"query":"请询问","questions":[{"header":"确认","question":"是否继续？","options":[{"label":"是","description":"继续"},{"label":"否","description":"取消"}]}]}`),
		mockllm.CreateTextResponse("已收到用户确认"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("askuser-invalid")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user 询问"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs)

	// 第 2 步：提供无效输入（不支持的类型，如 int），期望重新中断
	// 对齐 Python: resolve_interrupt(ctx, tool_call, 42, auto_confirm_config) → InterruptResult
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: 42, // 无效类型：int
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "无效输入 Invoke 不应返回 error")
	s.Require().NotNil(result, "结果不应为 nil")

	// 无效输入应触发再次中断（对齐 Python: resolve_interrupt → InterruptResult）
	resultType, ok := result["result_type"].(string)
	s.Require().True(ok, "应包含 result_type 字段")
	s.Equal("interrupt", resultType, "无效输入应再次触发 interrupt")

	sess.PostRun(ctx)
}

// TestAskUserRail_AskUserPayload恢复 测试用户使用 AskUserPayload 对象恢复中断。
// 对照 Python: test_hitl_ask_user_rail_with_payload_object
func (s *AskUserSuite) TestAskUserRail_AskUserPayload恢复() {
	ctx := s.Ctx

	rail := interrupt.NewAskUserRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("ask_user", `{"query":"请选择","questions":[{"header":"文件","question":"你想用什么文件名？","options":[{"label":"file1.txt","description":"默认"}]}]}`),
		mockllm.CreateTextResponse("好的，文件名是 payload_answer.txt"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("askuser-payload")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：触发中断
	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user 询问"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs)

	// 第 2 步：使用 AskUserPayload 对象恢复（对齐 Python AskUserPayload 格式）
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: &interrupt.AskUserPayload{
				Answers: map[string]string{
					"你想用什么文件名？": "payload_answer.txt",
				},
			},
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)

	sess.PostRun(ctx)
}

// TestAskUserRail_无问题时空答案恢复 测试 ask_user 无 questions 但有用户答案时走 Reject。
// 对照 Python: test_no_questions_returns_simple_answer
func (s *AskUserSuite) TestAskUserRail_无问题时空答案恢复() {
	ctx := s.Ctx

	rail := interrupt.NewAskUserRail()

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("ask_user", `{"query":"简单确认"}`),
		mockllm.CreateTextResponse("确认完成"),
	)

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := s.NewTestSession("askuser-noquestions")
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	// 第 1 步：触发中断（无 questions 也触发）
	result, err := agent.Invoke(ctx, map[string]any{"query": "请使用 ask_user"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, 1)
	s.Require().NotEmpty(interruptIDs)

	// 第 2 步：用户输入简单答案
	interactiveInput := &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			interruptIDs[0]: map[string]any{
				"answers": map[string]any{
					"": "simple answer",
				},
			},
		},
	}

	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "恢复 Invoke 失败")
	s.Require().NotNil(result, "恢复结果不应为 nil")

	testhelpers.AssertAnswerResult(s.T(), result)

	sess.PostRun(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
