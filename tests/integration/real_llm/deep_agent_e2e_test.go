//go:build integration && llm

package real_llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
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
// 对齐 Python: test_deep_agent_invoke_e2e_require_api_key_base
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

// TestDeepAgent_真实LLM_流式输出 测试真实 LLM 下 DeepAgent.Stream 流式输出。
// 对齐 Python: test_deep_agent_stream_e2e_require_api_key_base
//
// 断言对齐 Python 原文：
//   - stream 至少产出一个 chunk
//   - chunk 中存在 type=="llm_output" 的数据
//   - llm_output chunk 的 content 拼接后非空
func (s *DeepAgentE2ESuite) TestDeepAgent_真实LLM_流式输出() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 2*time.Minute)
	defer cancel()

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("e2e_real_llm_stream"),
		agentschema.WithAgentName("E2ERealLLMStreamAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("e2e_stream_session"),
		session.WithEnvs(map[string]any{}),
	)
	err = sess.PreRun(ctx)
	s.Require().NoError(err, "Session PreRun 失败")

	ch, err := agent.Stream(ctx, map[string]any{"query": "请用一句话介绍你自己"},
		agentinterfaces.WithSession(sess),
	)
	s.Require().NoError(err, "Stream 调用失败")
	s.Require().NotNil(ch, "流式通道不应为 nil")

	// 消费所有 chunk（带超时保护，防止通道异常挂死）
	var chunks []stream.Schema
	type chunkOrTimeout struct {
		chunk stream.Schema
		ok    bool
	}
	recv := make(chan chunkOrTimeout, 1)
	go func() {
		for c := range ch {
			recv <- chunkOrTimeout{chunk: c, ok: true}
		}
		recv <- chunkOrTimeout{ok: false}
	}()

deadline:
	for {
		select {
		case r := <-recv:
			if !r.ok {
				break deadline
			}
			chunks = append(chunks, r.chunk)
		case <-ctx.Done():
			break deadline
		}
	}

	s.NotEmpty(chunks, "stream 应至少产出一个 chunk")

	// 对齐 Python: has_llm_output = any(c.type == "llm_output" for c in chunks)
	hasLLMOutput := false
	var combined strings.Builder
	for _, c := range chunks {
		if output, ok := c.(stream.OutputSchema); ok && output.Type == "llm_output" {
			hasLLMOutput = true
			if payloadMap, ok := output.Payload.(map[string]any); ok {
				if content, ok := payloadMap["content"].(string); ok {
					combined.WriteString(content)
				}
			}
		}
	}
	s.True(hasLLMOutput, "stream chunks 应包含 llm_output 类型数据")
	s.NotEmpty(combined.String(), "llm_output 拼接内容不应为空")
}

// TestDeepAgent_真实LLM_外循环多步steer与followup 测试真实 LLM 外层任务循环的多步规划 + steer + follow_up。
// 对齐 Python: test_deep_agent_task_loop_real_multistep_steer_follow_up
//
// Python 原文用 LoopObserveRail.iteration_event 等待轮次时机；
// Go 用 testhelpers.ModelCallObserver（LoopObserveRail 的等价物）轮询
// 模型调用次数达到目标后注入 steer/follow_up。
// 断言对齐 Python 原文：
//   - result_type=="answer" 且 output 非空
//   - 迭代轮次 ≥ 2（模型调用次数为代理指标）
//   - steering 文本出现在模型消息中（SteerSeenInMessages）
//
// 与 Python 的两点移植差异（CI 首跑 40856190636 失败定位）：
//
//  1. 补齐 TaskPlanningRail + sys_operation + 规划型 system_prompt。
//     Python 原文 create_deep_agent 传 rails=[planning_rail, observe_rail]
//     且 asyncSetUp 注册 sys_operation——todo 工具 + 规划提示词引导模型
//     产出多步任务结构，外层循环多轮运行，steer/follow_up 有注入窗口。
//     首跑版本漏掉 planning rail：模型单轮直接 answer，外层循环
//     hasRemainingTasks=false 直接退出，steer 与 ReAct drain 竞态必输。
//
//  2. Steer 后轮询 SteerLen 同步屏障。Steer 是 fire-and-forget：
//     TaskInteractionEvent 经 MQ 消费者 goroutine 异步转入 LoopQueues.steering，
//     而 ReAct 迭代开头的 DrainSteering 与消费者 goroutine 在 Go 真并行下
//     自由竞争（asyncio 单线程 FIFO 保证消费者先跑，Go 无此保证）。
//     CI 首跑实证：call 2 为 HasPendingSteering 空判定后的普通迭代，
//     [STEERING] 消息缺失 → 断言失败（14.44s）。对齐 outer_loop_test.go
//     的 waitForFollowUpQueued 先例（FollowUpLen 同族问题）。
func (s *DeepAgentE2ESuite) TestDeepAgent_真实LLM_外循环多步steer与followup() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 10*time.Minute)
	defer cancel()

	const steerText = "输出请使用简洁中文要点"
	const followUpText = "在结尾追加一条风险提示"

	observer := testhelpers.NewModelCallObserver()

	// 对齐 Python asyncSetUp：sys_operation（TaskPlanningRail 从
	// agent.DeepConfig().SysOperation 自动获取同一实例，todo 工具挂载其上）
	sysOpCard := sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
	)
	sysOp, err := sys_operation.NewSysOperation(sysOpCard)
	s.Require().NoError(err, "创建 SysOperation 失败")

	// 对齐 Python: planning_rail = TaskPlanningRail(sys_oper)
	planningRail := rails.NewTaskPlanningRail()
	planningRail.SetSysOperation(sysOp)

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("e2e_real_llm_taskloop"),
		agentschema.WithAgentName("E2ERealLLMTaskLoopAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:           card,
		Model:          model,
		SysOperation:   sysOp,
		Rails:          []agentinterfaces.AgentRail{planningRail, observer},
		EnableTaskLoop: true,
		MaxIterations:  12,
		// 对齐 Python 原文 system_prompt（规划导向）
		SystemPrompt: "你是一个严谨的任务执行助手。根据当前任务逐步输出结果。",
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("e2e_taskloop_session"),
		session.WithEnvs(map[string]any{}),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	query := "请制定一个简短的项目启动计划，包含以下方面：1. 需求分析；2. 技术选型；3. 实施方案。每个方面给出简要说明。"

	// goroutine 中执行 Invoke（对齐 Python asyncio.create_task + run_agent）
	type invokeResult struct {
		result map[string]any
		err    error
	}
	done := make(chan invokeResult, 1)
	go func() {
		result, invokeErr := agent.Invoke(ctx, map[string]any{"query": query},
			agentinterfaces.WithSession(sess))
		done <- invokeResult{result: result, err: invokeErr}
	}()

	// 等第 1 轮模型调用发生后注入 steer（对齐 Python iteration_event(1).wait()）
	s.Require().True(s.waitForModelCalls(observer, 1, 3*time.Minute),
		"等待第 1 次模型调用超时")
	agent.Steer(ctx, steerText, sess)
	// 同步屏障：确认 steer 已被 MQ 消费者处理并推入 steering 队列，
	// 消除 Steer→ReAct drain 竞态窗口（详见方法注释移植差异 2）
	s.Require().NoError(s.waitForSteerQueued(agent, 1, 30*time.Second),
		"等待 steer 入队超时")

	// 等第 2 轮模型调用发生后注入 follow_up（对齐 Python iteration_event(2).wait()）
	s.Require().True(s.waitForModelCalls(observer, 2, 5*time.Minute),
		"等待第 2 次模型调用超时")
	agent.FollowUp(ctx, followUpText, "", sess)
	// 同族同步屏障：follow_up 经 MQ 异步入队，外层循环退出检查
	// HasFollowUp 前必须确认已入队（对齐 outer_loop_test 先例）
	s.Require().NoError(s.waitForFollowUpQueued(agent, 1, 30*time.Second),
		"等待 follow_up 入队超时")

	// 等待 Invoke 完成（对齐 Python asyncio.wait_for(invoke_task, 600)）
	select {
	case r := <-done:
		s.Require().NoError(r.err, "Invoke 失败")
		s.Require().NotNil(r.result, "结果不应为 nil")
		testhelpers.AssertAnswerResult(s.T(), r.result)
		output, _ := r.result["output"].(string)
		s.NotEmpty(output, "answer output 不应为空")
	case <-time.After(10 * time.Minute):
		s.T().Fatal("Invoke 超时")
	}

	sess.PostRun(ctx)

	// 对齐 Python: assertGreaterEqual(observe_rail.iteration_count, 2)
	s.GreaterOrEqual(observer.CallCount(), 2, "任务循环应至少执行 2 轮模型调用")
	// 对齐 Python: assertTrue(observe_rail.steer_seen_in_model_messages)
	s.True(observer.SteerSeenInMessages(), "[STEERING] 应出现在模型消息中")
}

// TestDeepAgent_真实LLM_外循环流式输出 测试真实 LLM enable_task_loop=True 时 Stream 逐轮流式输出。
// 对齐 Python: test_deep_agent_task_loop_stream_e2e
//
// Python 原文收集 run_agent_streaming 的每轮 round result 并验证
// output 非空；Go 的 task_loop Stream 路径产出每轮 result 的
// OutputSchema（round result 写入流，见 writeRoundResultToStream）。
func (s *DeepAgentE2ESuite) TestDeepAgent_真实LLM_外循环流式输出() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 5*time.Minute)
	defer cancel()

	// 对齐 Python 原文（test_deep_agent_e2e.py L653-700）：
	// planning_rail + sys_operation + 规划型 system_prompt 缺一不可——
	// todo 工具 + 规划提示词引导模型产出多步任务结构（TaskPlan），
	// 外层循环多轮运行，stream 才有逐轮流式输出可验证。
	// 首跑 40965036663 失败定位（2.90s，roundOutputs=0）：漏 planning rail
	// 时模型单轮直接 answer，无 TaskPlan → hasRemainingTasks=false →
	// 外层循环单轮退出；对齐 steer 测试（25c74be7）同构补齐。
	sysOpCard := sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
	)
	sysOp, err := sys_operation.NewSysOperation(sysOpCard)
	s.Require().NoError(err, "创建 SysOperation 失败")

	// 对齐 Python: planning_rail = TaskPlanningRail(sys_oper)
	planningRail := rails.NewTaskPlanningRail()
	planningRail.SetSysOperation(sysOp)

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("e2e_real_llm_taskloop_stream"),
		agentschema.WithAgentName("E2ERealLLMTaskLoopStreamAgent"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:           card,
		Model:          model,
		SysOperation:   sysOp,
		Rails:          []agentinterfaces.AgentRail{planningRail},
		EnableTaskLoop: true,
		MaxIterations:  12,
		// 对齐 Python 原文 system_prompt（规划导向）
		SystemPrompt: "你是一个严谨的任务执行助手。根据当前任务逐步输出结果。",
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	sess := session.NewSession(
		session.WithSessionID("e2e_taskloop_stream_session"),
		session.WithEnvs(map[string]any{}),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")

	ch, err := agent.Stream(ctx, map[string]any{
		"query": "请制定一个简短的项目启动计划，包含以下方面：1. 需求分析；2. 技术选型。每个方面给出简要说明。",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "Stream 调用失败")

	// 消费所有 chunk（带超时保护）
	var chunks []stream.Schema
	type chunkOrTimeout struct {
		chunk stream.Schema
		ok    bool
	}
	recv := make(chan chunkOrTimeout, 1)
	go func() {
		for c := range ch {
			recv <- chunkOrTimeout{chunk: c, ok: true}
		}
		recv <- chunkOrTimeout{ok: false}
	}()

deadline:
	for {
		select {
		case r := <-recv:
			if !r.ok {
				break deadline
			}
			chunks = append(chunks, r.chunk)
		case <-ctx.Done():
			break deadline
		}
	}

	// 对齐 Python: assertGreater(len(round_results), 0)
	s.NotEmpty(chunks, "task loop stream 应至少产出一个 chunk")

	// 对齐 Python: 每轮 round result 的 output 非空。
	// task_loop 路径的每轮结果通过 OutputSchema 写入流。
	roundOutputs := 0
	for _, c := range chunks {
		if output, ok := c.(stream.OutputSchema); ok {
			if payloadMap, ok := output.Payload.(map[string]any); ok {
				if outputVal, ok := payloadMap["output"]; ok {
					if str, ok := outputVal.(string); ok && str != "" {
						roundOutputs++
					}
				}
			}
		}
	}
	s.Greater(roundOutputs, 0, "应至少有一轮非空 output 的结果")
}

// TestDeepAgentE2EDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *DeepAgentE2ESuite) TestDeepAgentE2EDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(harness.CreateDeepAgent)
	s.NotNil(stream.OutputSchema{})
	s.NotNil(testhelpers.NewModelCallObserver)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// waitForModelCalls 轮询等待 observer 记录的模型调用次数达到 n。
// 返回 false 表示超时未达到。替代 Python LoopObserveRail.iteration_event(k).wait()。
func (s *DeepAgentE2ESuite) waitForModelCalls(observer *testhelpers.ModelCallObserver, n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if observer.CallCount() >= n {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return observer.CallCount() >= n
}

// waitForSteerQueued 等待 steer 事件真实入队（LoopQueues.steering 非空）。
// 对齐 outer_loop_test.go 的 waitForFollowUpQueued 先例：Steer 是
// fire-and-forget，事件经 MQ 消费者 goroutine 异步转入 steering 队列，
// 与 ReAct 迭代 DrainSteering 存在调度竞争，需显式同步屏障。
func (s *DeepAgentE2ESuite) waitForSteerQueued(agent *harness.DeepAgent, expected int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tl, ok := agent.LoopController().(*task_loop.TaskLoopController); ok && tl != nil {
			if tl.SteerLen() >= expected {
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("等待 %d 条 steer 入队超时（%v）", expected, timeout)
}

// waitForFollowUpQueued 等待 follow_up 事件真实入队（LoopQueues.followUp 非空）。
// 对齐 outer_loop_test.go 的 waitForFollowUpQueued：外层循环退出检查
// HasFollowUp 前必须确认 follow_up 已被消费者处理入队。
func (s *DeepAgentE2ESuite) waitForFollowUpQueued(agent *harness.DeepAgent, expected int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tl, ok := agent.LoopController().(*task_loop.TaskLoopController); ok && tl != nil {
			if tl.FollowUpLen() >= expected {
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("等待 %d 条 follow-up 入队超时（%v）", expected, timeout)
}
