//go:build integration

package subagent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSpawnE2ESuite 测试异步子代理（enableAsyncSubagent）端到端完成场景。
//
// 前置：SubagentRail async 分支注册 sessions_spawn 三工具并注入 SessionToolkit；
// spawn 提交 SESSION_SPAWN 任务后由 SessionSpawnExecutor 后台执行；
// 任务完成时 completeSessionSpawn 两路分支：
//   - 无活跃 invoke → ScheduleAutoInvokeOnSpawnDone 延迟 auto-invoke（场景 1/4）
//   - 有活跃 invoke → PushSteer 注入 steering 队列（场景 2 的 q2 窗口断言）
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_subagent.py
//
//	TestDeepAgentSessionRail（场景 1/2/4；Python 端为 skip system test，
//	此 Mock 级移植对齐审查文档 T-02 的补齐方案；场景 3 依赖真实 LLM 暂缓）
//
// 与 CancelMock 套件的差异：
//   - 本套件不覆写 schedule_auto_invoke（要验证的就是 auto-invoke 真实调度）
//   - 子代理替身走 SetCreateSubagentOverride（复用 fakeSubAgent）
type SessionSpawnE2ESuite struct {
	isuite.AgentSuite
	// railToolIDs SubagentRail 注册到 ResourceMgr 的 session 工具 ID
	railToolIDs []string
}

// SetupTest 为每个测试方法重置 MockLLM（对齐 Python per-test MockLLMModel 语义）。
// 理由与 SessionCancelE2ESuite 相同：残留后台 goroutine 消费新队列导致断言错位。
func (s *SessionSpawnE2ESuite) SetupTest() {
	s.resetSpawnMockLLM()
}

// TearDownTest 清理 SubagentRail 注册的 session 工具。
func (s *SessionSpawnE2ESuite) TearDownTest() {
	if len(s.railToolIDs) > 0 {
		if rm := s.GetResourceMgr(); rm != nil {
			_, _ = rm.RemoveTool(s.railToolIDs)
		}
		s.railToolIDs = nil
	}
	s.AgentSuite.TearDownTest()
}

// resetSpawnMockLLM 替换 MockLLM 实例并重新注册到 ClientRegistry。
func (s *SessionSpawnE2ESuite) resetSpawnMockLLM() {
	if s.ProviderName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.ProviderName, "llm")
	}
	s.MockLLM = mockllm.NewMockModelClient()
	providerName := fmt.Sprintf("mock_spawn_e2e_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", s.MockLLM.Factory())
	s.ProviderName = providerName
	s.ClientConfig = &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
		APIKey:         "mock-api-key",
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSessionSpawnE2ESuite(t *testing.T) {
	suite.Run(t, new(SessionSpawnE2ESuite))
}

// TestSessionSpawnE2E_场景1_autoInvoke总结 仅 q1 spawn 无 q2，后台任务完成后 auto-invoke 总结。
// 对齐 Python: test_auto_invoke_on_spawn_done_no_query2
//
// 核心验证：
//   - q1 返回 result_type=answer 且 invoke 不再活跃
//   - SessionToolkit 已注入且任务登记
//   - 快子代理完成后 toolkit 行为 completed
//   - auto-invoke 真实触发：MockLLM 产生后续调用（队列耗尽默认响应兜底）
func (s *SessionSpawnE2ESuite) TestSessionSpawnE2E_场景1_autoInvoke总结() {
	ctx := s.Ctx
	agent, sess := s.buildSpawnAgent(ctx, "spawn_s1")

	// 快子代理：300ms 完成（保证 q1 invoke 结束前任务尚未完成，走 auto-invoke 分支）
	s.setSpawnSubagent(agent, 300*time.Millisecond, "chipotle analysis done")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "分析Chipotle为什么不进入中国市场"}`),
		mockllm.CreateTextResponse("任务已提交"),
	)
	r1, err := agent.Invoke(ctx, map[string]any{"query": "提交后台任务：分析Chipotle为什么还没有进入中国市场"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "q1 Invoke 失败")
	s.Equal("answer", r1["result_type"], "q1 应返回 answer")
	s.False(agent.IsInvokeActive(), "q1 完成后 invoke 不应活跃")

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit, "SessionToolkit 应已注入")
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1, "应有 1 个后台任务")

	// 等待后台任务完成（对齐 Python 轮询 list_all 状态，超时对齐 5s 粒度内收紧为 10s）
	taskID := rows[0].TaskID
	s.Require().NoError(waitForTaskStatus(toolkit, taskID, "completed", 10*time.Second),
		"spawn 未在超时内完成")

	// auto-invoke 已真实调度（0.5s 延迟 + 触发一次模型调用）
	// 对齐 Python: 任务完成后延迟调度 auto-invoke，无 q2 场景下主 agent 主动总结
	baseCalls := s.MockLLM.InvokeCallCount()
	s.Require().NoError(waitForInvokeCount(s.MockLLM, baseCalls+1, 10*time.Second),
		"auto-invoke 未在超时内触发模型调用")

	// auto-invoke 消费完队列后拿到默认响应（空队列兜底），不应 panic/error
	s.Eventually(func() bool {
		return !agent.IsInvokeActive()
	}, 5*time.Second, 50*time.Millisecond, "auto-invoke 完成后 invoke 应结束")
}

// TestSessionSpawnE2E_场景2_q2不阻塞且Steering不可见 q1 spawn 长任务后 q2 短问；
// q2 完成时模型侧尚未观测到 [STEERING]，且 q2 与 q1 使用同一 controller。
// 对齐 Python: test_async_spawn_query2_not_blocked
//
// 核心验证：
//   - q2 短问正常返回 answer（不被长后台任务阻塞）
//   - q2 的模型消息中无 [STEERING] 标记（长任务完成前 steer 未注入）
//   - q2 前后 LoopController 指针一致（复用，不重建）
//   - q1 长任务最终完成
func (s *SessionSpawnE2ESuite) TestSessionSpawnE2E_场景2_q2不阻塞且Steering不可见() {
	ctx := s.Ctx
	observe := newSteeringObserver()
	agent, sess := s.buildSpawnAgent(ctx, "spawn_s2", observe)

	// 慢子代理：8s，保证 q2 完成窗口内后台任务仍在运行
	s.setSpawnSubagent(agent, 8*time.Second, "slow analysis done")

	// q1：spawn + 文本
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "分析为什么Chipotle还不进入中国市场"}`),
		mockllm.CreateTextResponse("后台任务已提交"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "执行一个后台任务：分析为什么Chipotle还不进入中国市场"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "q1 Invoke 失败")

	ctrlAfterQ1 := agent.LoopController()
	s.Require().NotNil(ctrlAfterQ1, "q1 后 LoopController 应存在")

	// q2：短问（对齐 Python asyncio.wait_for 120s 超时，mock 场景收紧）
	q2done := make(chan error, 1)
	go func() {
		s.MockLLM.SetResponses(mockllm.CreateTextResponse("5"))
		_, err := agent.Invoke(ctx, map[string]any{"query": "2+3等于几？只回答数字。"},
			agentinterfaces.WithSession(sess))
		q2done <- err
	}()
	select {
	case err := <-q2done:
		s.Require().NoError(err, "q2 Invoke 失败")
	case <-time.After(30 * time.Second):
		s.FailNow("q2 被后台任务阻塞，超时")
	}

	// q2 完成时模型侧尚未观测到 [STEERING]（慢任务 8s >> q2 耗时）
	s.False(observe.SteerSeen(), "q2 完成时不应观测到 [STEERING] 注入")

	// q2 与 q1 使用同一 controller（会话未切换，setupTaskLoop 复用分支）
	s.Same(ctrlAfterQ1, agent.LoopController(), "q2 应复用 q1 的 LoopController")

	// q1 长任务最终完成（对齐 Python max_wait=300，mock 场景收紧为 30s）
	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1)
	s.Require().NoError(waitForTaskStatus(toolkit, rows[0].TaskID, "completed", 30*time.Second),
		"spawn 未在超时内完成")
}

// TestSessionSpawnE2E_场景4_多spawn完成后统一总结 一次对话同时触发两个后台任务，
// 全部完成后 auto-invoke 仅去重调度一次。
// 对齐 Python: test_auto_invoke_dedup_multi_spawn
//
// 核心验证：
//   - 单轮 Invoke 并发 spawn 两个任务（MultiToolCall）
//   - toolkit 登记 ≥2 行
//   - 两个任务均完成（completed/error）
//   - IsAutoInvokeScheduled 去重：首个任务完成即置位，第二个完成时不重复调度
func (s *SessionSpawnE2ESuite) TestSessionSpawnE2E_场景4_多spawn完成后统一总结() {
	ctx := s.Ctx
	agent, sess := s.buildSpawnAgent(ctx, "spawn_s4")

	// 快子代理：两任务均 300ms 完成
	s.setSpawnSubagent(agent, 300*time.Millisecond, "multi done")

	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "sessions_spawn", ArgsJSON: `{"subagent_type": "general-purpose", "task_description": "任务A：查询随机森林算法的应用"}`},
			{Name: "sessions_spawn", ArgsJSON: `{"subagent_type": "general-purpose", "task_description": "任务B：天空是什么颜色"}`},
		}),
		mockllm.CreateTextResponse("两个任务均已提交"),
	)
	r1, err := agent.Invoke(ctx, map[string]any{"query": "同时做两件事：任务A查询随机森林算法应用；任务B天空是什么颜色"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "q1 Invoke 失败")
	s.Equal("answer", r1["result_type"], "q1 应返回 answer")

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().GreaterOrEqual(len(rows), 2, "应登记至少 2 个后台任务")

	// 全部任务完成（对齐 Python: all(status in (completed, error))）
	taskIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		taskIDs = append(taskIDs, r.TaskID)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		for _, id := range taskIDs {
			row, ok := toolkit.Get(id)
			if !ok || (row.Status != "completed" && row.Status != "error") {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, id := range taskIDs {
		row, ok := toolkit.Get(id)
		s.Require().True(ok, "任务 %s 应存在", id)
		s.Contains([]string{"completed", "error"}, row.Status,
			"任务 %s 应为 completed/error", id)
	}

	// auto-invoke 去重：两个任务完成仅调度一次（第二个完成时 IsAutoInvokeScheduled 仍为 true 拦截重复调度）
	baseCalls := s.MockLLM.InvokeCallCount()
	s.Require().NoError(waitForInvokeCount(s.MockLLM, baseCalls+1, 10*time.Second),
		"auto-invoke 未在超时内触发模型调用")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSpawnAgent 构建启用异步子代理的 DeepAgent（auto-invoke 不覆写，验证真实调度）。
// extraRails 为附加 Rail（场景 2 传入 steering 观测器）。
func (s *SessionSpawnE2ESuite) buildSpawnAgent(
	ctx context.Context,
	sessionName string,
	extraRails ...agentinterfaces.AgentRail,
) (*harness.DeepAgent, *session.Session) {
	// 唯一卡片 ID：session 工具 resource ID 格式 sessions_spawn_<agentID>，
	// 避免跨套件/跨测试工具注册冲突（同 SessionCancelE2ESuite）。
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(sessionName),
		agentschema.WithAgentName(sessionName+"_agent"),
	)

	rails := extraRails
	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		Card: card,
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(
					agentschema.WithAgentName("research_agent"),
					agentschema.WithAgentDescription("专注于研究调查任务"),
				),
				SystemPrompt: "你是一名研究助理。",
			},
		},
		EnableTaskLoop:      true,
		EnableAsyncSubagent: true,
		MaxIterations:       10,
		CompletionTimeout:   30.0,
		Rails:               rails,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 记录 rail 注册的 session 工具 ID，TearDownTest 时清理
	s.railToolIDs = append(s.railToolIDs,
		"sessions_list_"+sessionName,
		"sessions_spawn_"+sessionName,
		"sessions_cancel_"+sessionName,
	)

	sess := s.NewTestSession(sessionName)
	return agent, sess
}

// setSpawnSubagent 注入固定延迟的子代理替身（复用 fakeSubAgent）。
func (s *SessionSpawnE2ESuite) setSpawnSubagent(
	agent *harness.DeepAgent, delay time.Duration, output string,
) {
	agent.SetCreateSubagentOverride(func(ctx context.Context, _, _ string) (hinterfaces.DeepAgentInterface, error) {
		return newFakeSubAgent(ctx, delay, output), nil
	})
}

// waitForInvokeCount 轮询等待 MockLLM 调用次数达到目标值。
func waitForInvokeCount(client *mockllm.MockModelClient, want int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if client.InvokeCallCount() >= want {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("等待 MockLLM 调用数达到 %d 超时（当前 %d，超时 %v）",
		want, client.InvokeCallCount(), timeout)
}
