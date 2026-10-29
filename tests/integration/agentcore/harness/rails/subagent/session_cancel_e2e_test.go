//go:build integration

package subagent

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hsubagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionCancelE2ESuite 测试异步子代理（enableAsyncSubagent）端到端取消场景。
//
// 前置：SubagentRail async 分支注册 sessions_list/spawn/cancel 三工具并注入
// SessionToolkit；spawn 提交 SESSION_SPAWN 任务后由 SessionSpawnExecutor 后台
// 执行；cancel 经 TaskScheduler.CancelTask 真实取消 goroutine 并回写 toolkit。
//
// 对齐 Python: tests/system_tests/harness/test_deep_agent_subagent.py
//
//	TestDeepAgentSessionRailCancelMock 场景 1-6
//
// Go 与 Python 的差异处理：
//   - Python 通过 patch uuid4 固定 task_id；Go 无法 patch 包级函数，
//     改为 spawn 后经 toolkit.ListAll() 发现真实 task_id，再构造 cancel 参数
//   - Python 覆写 agent.create_subagent / schedule_auto_invoke_on_spawn_done；
//     Go 通过 SetCreateSubagentOverride / SetScheduleAutoInvokeOverride 测试钩子等价实现
type SessionCancelE2ESuite struct {
	isuite.AgentSuite
	// railToolIDs 每个 Agent 的 SubagentRail 注册到 ResourceMgr 的工具 ID
	// （sessions_list/spawn/cancel），TearDownTest 时清理。
	// AgentSuite 基类只清理 params.ToolInstances，rail 自注册的工具需此处兜底。
	railToolIDs []string
}

// TearDownTest 清理 SubagentRail 注册的 session 工具。
// 每个 Agent 使用唯一卡片 ID，工具 ID 互不冲突，但注册仍会在全局
// ResourceMgr 中累积；不清理会导致后续套件（如 TaskPlanningRail 查询
// task_tool）看到跨套件残留。
func (s *SessionCancelE2ESuite) TearDownTest() {
	if len(s.railToolIDs) > 0 {
		if rm := s.GetResourceMgr(); rm != nil {
			_, _ = rm.RemoveTool(s.railToolIDs)
		}
		s.railToolIDs = nil
	}
	s.AgentSuite.TearDownTest()
}

// SetupTest 为每个测试方法重置 MockLLM（对齐 Python per-test MockLLMModel 语义）。
//
// 背景：suite 级共享 MockLLM 时，前一个测试的残留后台 goroutine
// （场景 1 cancel 后仍 in-flight 的调度循环/事件处理）会消费下一个
// 测试 SetResponses 排队的 tool-call 响应——模型调用顺序错位导致
// sessions_spawn 从未被当前测试执行（toolkit 为空、断言失败）。
// Python 端每个测试方法 mock_llm = MockLLMModel() 独立创建，
// 残留 goroutine 拿到的是旧实例的空队列（返回默认文本），
// 不会污染新实例的响应队列。
//
// 实现方式：注销旧 provider，注册新 providerName + 新 MockModelClient，
// 同步更新 s.MockLLM / s.ProviderName / s.ClientConfig。
func (s *SessionCancelE2ESuite) SetupTest() {
	s.resetMockLLM()
}

// resetMockLLM 替换 MockLLM 实例并重新注册到 ClientRegistry。
func (s *SessionCancelE2ESuite) resetMockLLM() {
	// 注销旧 provider
	if s.ProviderName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.ProviderName, "llm")
	}

	// 新建 MockLLM 并注册新 provider（providerName 纳秒唯一，对齐 suite.uniqueProviderName）
	s.MockLLM = mockllm.NewMockModelClient()
	providerName := fmt.Sprintf("mock_cancel_e2e_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", s.MockLLM.Factory())
	s.ProviderName = providerName
	s.ClientConfig = &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
		APIKey:         "mock-api-key",
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSessionCancelE2ESuite(t *testing.T) {
	suite.Run(t, new(SessionCancelE2ESuite))
}

// TestSessionCancelE2E_场景1_立即取消 spawn 后立即取消。
// 对齐 Python: test_sessions_cancel_scenario1_immediate_cancel
func (s *SessionCancelE2ESuite) TestSessionCancelE2E_场景1_立即取消() {
	ctx := s.Ctx
	agent, sess := s.buildCancelAgent(ctx, nil, "cancel_s1")

	// 慢子代理（永不完成，被取消打断）
	s.setSleepSubagent(agent, 60*time.Second, "never")

	// 第一轮：spawn 后台任务 + 文本回复
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "long task"}`),
		mockllm.CreateTextResponse("spawn submitted"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "创建后台任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "第一轮 Invoke 失败")

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit, "SessionToolkit 应已注入")
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1, "应有 1 个后台任务")
	taskID := rows[0].TaskID

	// 第二轮：取消该任务 + 文本回复
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskID)),
		mockllm.CreateTextResponse("cancel requested"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "取消后台任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "第二轮 Invoke 失败")

	row, ok := toolkit.Get(taskID)
	s.Require().True(ok, "任务应存在")
	s.Equal("canceled", row.Status, "任务状态应为 canceled")
}

// TestSessionCancelE2E_场景2_运行中取消 任务进入 running 后取消。
// 对齐 Python: test_sessions_cancel_scenario2_cancel_when_running
func (s *SessionCancelE2ESuite) TestSessionCancelE2E_场景2_运行中取消() {
	ctx := s.Ctx
	agent, sess := s.buildCancelAgent(ctx, nil, "cancel_s2")

	// 慢子代理（8 秒，保证取消时仍在运行）
	s.setSleepSubagent(agent, 8*time.Second, "slow done")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "slow analysis"}`),
		mockllm.CreateTextResponse("spawn ok"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "请后台执行一个慢任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1)
	taskID := rows[0].TaskID

	// 等待一小段时间，让任务从 submitted 进入 working
	// （对齐 Python: await asyncio.sleep(0.2)）
	time.Sleep(200 * time.Millisecond)

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskID)),
		mockllm.CreateTextResponse("cancel ok"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "请取消这个后台任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	row, ok := toolkit.Get(taskID)
	s.Require().True(ok, "任务应存在")
	s.Equal("canceled", row.Status, "运行中任务取消后状态应为 canceled")
}

// TestSessionCancelE2E_场景3_取消不触发Steering 取消后不应触发 steering 注入。
// 对齐 Python: test_sessions_cancel_scenario3_cancel_should_not_trigger_steering
func (s *SessionCancelE2ESuite) TestSessionCancelE2E_场景3_取消不触发Steering() {
	ctx := s.Ctx
	observe := newSteeringObserver()
	agent, sess := s.buildCancelAgent(ctx, []agentinterfaces.AgentRail{observe}, "cancel_s3")

	s.setSleepSubagent(agent, 60*time.Second, "done")

	// 第一轮：spawn + 文本
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "slow task"}`),
		mockllm.CreateTextResponse("spawn ok"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "创建后台任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1)
	taskID := rows[0].TaskID

	// 第二轮：取消 + 文本
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskID)),
		mockllm.CreateTextResponse("cancel ok"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "取消它"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	// 重置观测状态（对齐 Python: observe.steer_seen_in_model_messages = False）
	observe.Reset()

	// 第三轮：取消后正常问答
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("普通回答"))
	_, err = agent.Invoke(ctx, map[string]any{"query": "取消后正常问答"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	s.False(observe.SteerSeen(), "取消后的正常问答不应触发 [STEERING] 注入")
}

// TestSessionCancelE2E_场景4_多任务取消其一 多任务中只取消一个，另一个完成。
// 对齐 Python: test_sessions_cancel_scenario4_cancel_one_of_multiple_tasks
func (s *SessionCancelE2ESuite) TestSessionCancelE2E_场景4_多任务取消其一() {
	ctx := s.Ctx
	agent, sess := s.buildCancelAgent(ctx, nil, "cancel_s4")

	// 按 task 描述绑定子代理行为：task a 慢（被取消）/ task b 快（完成）。
	// 对齐 Python: 场景 4 _factory（spawn 前设置，按 sub_session_id 查 toolkit 行
	// 绑定快慢；Go 无法 patch uuid 固定 task_id，改用 Description 区分）。
	// 必须在第一轮 Invoke 前设置——spawn 提交后 executor 会立即调用 CreateSubagent。
	s.setDescriptionBoundSubagents(agent, "task a", 60*time.Second, "a done")

	// 两个任务一次性 spawn（并发工具调用）
	s.MockLLM.SetResponses(
		mockllm.CreateMultiToolCallResponse([]mockllm.ToolCallSpec{
			{Name: "sessions_spawn", ArgsJSON: `{"subagent_type": "general-purpose", "task_description": "task a"}`},
			{Name: "sessions_spawn", ArgsJSON: `{"subagent_type": "general-purpose", "task_description": "task b"}`},
		}),
		mockllm.CreateTextResponse("two tasks spawned"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "创建两个后台任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().GreaterOrEqual(len(rows), 2, "应至少有 2 个后台任务")

	// 按描述区分 task a（慢，将被取消）与 task b（快，将完成）
	var taskAID, taskBID string
	for _, r := range rows {
		switch r.Description {
		case "task a":
			taskAID = r.TaskID
		case "task b":
			taskBID = r.TaskID
		}
	}
	s.Require().NotEmpty(taskAID, "应找到 task a")
	s.Require().NotEmpty(taskBID, "应找到 task b")

	// 取消 task a
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskAID)),
		mockllm.CreateTextResponse("one cancelled"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "取消其中一个任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	// 等待 task b 完成（对齐 Python: await asyncio.sleep(0.5)）
	s.Require().NoError(waitForTaskStatus(toolkit, taskBID, "completed", 5*time.Second),
		"等待 task b 完成超时")

	statusMap := map[string]string{}
	for _, r := range toolkit.ListAll() {
		statusMap[r.TaskID] = r.Status
	}
	s.Equal("canceled", statusMap[taskAID], "task a 应为 canceled")
	s.Equal("completed", statusMap[taskBID], "task b 应为 completed")
}

// TestSessionCancelE2E_场景5_重复取消幂等 同一任务重复取消应幂等。
// 对齐 Python: test_sessions_cancel_scenario5_repeat_cancel_idempotent
func (s *SessionCancelE2ESuite) TestSessionCancelE2E_场景5_重复取消幂等() {
	ctx := s.Ctx
	agent, sess := s.buildCancelAgent(ctx, nil, "cancel_s5")

	s.setSleepSubagent(agent, 60*time.Second, "done")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "task"}`),
		mockllm.CreateTextResponse("spawn ok"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "创建后台任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1)
	taskID := rows[0].TaskID

	// 第一次取消
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskID)),
		mockllm.CreateTextResponse("cancel 1 ok"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "第一次取消"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	row, ok := toolkit.Get(taskID)
	s.Require().True(ok, "任务应存在")
	s.Equal("canceled", row.Status, "第一次取消后应为 canceled")

	// 第二次取消（幂等）
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskID)),
		mockllm.CreateTextResponse("cancel 2 ok"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "第二次取消"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	row, ok = toolkit.Get(taskID)
	s.Require().True(ok, "任务应存在")
	s.Equal("canceled", row.Status, "重复取消后状态应保持 canceled")
}

// TestSessionCancelE2E_场景6_取消已完成保持completed 任务已完成后取消，应保持 completed。
// 对齐 Python: test_sessions_cancel_scenario6_cancel_completed_task
func (s *SessionCancelE2ESuite) TestSessionCancelE2E_场景6_取消已完成保持completed() {
	ctx := s.Ctx
	agent, sess := s.buildCancelAgent(ctx, nil, "cancel_s6")

	// 快子代理：立即完成（对齐 Python: _create_fast_subagent 返回 _FastSubAgent）
	s.setSleepSubagent(agent, 0, "completed quickly")

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_spawn",
			`{"subagent_type": "general-purpose", "task_description": "fast complete"}`),
		mockllm.CreateTextResponse("spawn ok"),
	)
	_, err := agent.Invoke(ctx, map[string]any{"query": "创建快速任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	toolkit := agent.GetSessionToolkit()
	s.Require().NotNil(toolkit)
	rows := toolkit.ListAll()
	s.Require().Len(rows, 1)
	taskID := rows[0].TaskID

	// 快子代理：等任务完成
	s.Require().NoError(waitForTaskStatus(toolkit, taskID, "completed", 5*time.Second),
		"等待快速任务完成超时")
	row, _ := toolkit.Get(taskID)
	s.Require().Equal("completed", row.Status, "完成后状态应为 completed")

	// 对已完成任务执行取消
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("sessions_cancel",
			fmt.Sprintf(`{"task_id": %q}`, taskID)),
		mockllm.CreateTextResponse("cancel requested"),
	)
	_, err = agent.Invoke(ctx, map[string]any{"query": "取消这个已完成任务"},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	row, _ = toolkit.Get(taskID)
	s.Require().Equal("completed", row.Status, "取消已完成任务后状态应保持 completed")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildCancelAgent 构建启用异步子代理的 DeepAgent。
// 对齐 Python: TestDeepAgentSessionRailCancelMock._build_cancel_agent。
// extraRails 为附加 Rail（场景 3 传入 steering 观测器），autoInvokeNoop
// 控制是否用 noop 替换 auto-invoke 调度（默认 true，对齐 Python noop 覆写）。
func (s *SessionCancelE2ESuite) buildCancelAgent(
	ctx context.Context,
	extraRails []agentinterfaces.AgentRail,
	sessionName string,
) (*harness.DeepAgent, *session.Session) {
	// noop auto-invoke：防止 spawn 完成/取消后 0.5s 的自动触发消费 mock 队列
	// 对齐 Python: agent.schedule_auto_invoke_on_spawn_done = self._noop_auto_invoke
	rails := extraRails

	// 唯一卡片 ID：session 工具的 resource ID 格式为 sessions_spawn_<agentID>，
	// 默认卡片 ID 相同会导致跨测试注册冲突——ResourceMgr 拒绝重复 ID 后
	// 旧工具实例（闭包绑定旧 toolkit）被复用，spawn 写入旧 toolkit 使断言失效。
	// 每个测试使用独立 agentID 保证工具注册到本测试的 toolkit。
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(sessionName),
		agentschema.WithAgentName(sessionName+"_agent"),
	)

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

	agent.SetScheduleAutoInvokeOverride(func(context.Context, string, float64) error {
		return nil
	})

	// 记录 rail 注册的 session 工具 ID，TearDownTest 时清理
	// 工具 ID 格式：sessions_list_<agentID> / sessions_spawn_<agentID> / sessions_cancel_<agentID>
	s.railToolIDs = append(s.railToolIDs,
		"sessions_list_"+sessionName,
		"sessions_spawn_"+sessionName,
		"sessions_cancel_"+sessionName,
	)

	sess := s.NewTestSession(sessionName)
	return agent, sess
}

// setSleepSubagent 注入固定延迟的子代理替身。
// 对齐 Python: _SleepSubAgent(delay, output)。
func (s *SessionCancelE2ESuite) setSleepSubagent(
	agent *harness.DeepAgent, delay time.Duration, output string,
) {
	agent.SetCreateSubagentOverride(func(ctx context.Context, _, _ string) (hinterfaces.DeepAgentInterface, error) {
		return newFakeSubAgent(ctx, delay, output), nil
	})
}

// setDescriptionBoundSubagents 注入按任务描述绑定慢/快行为的子代理替身。
// 对齐 Python: 场景 4 _factory（按 sub_session_id 查 toolkit 行绑定行为）。
// Go 无法 patch uuid 固定 task_id，改按 Description 区分：
// slowDescription 对应慢子代理（被取消），其余为快子代理（完成）。
// 必须在 spawn 前调用——spawn 提交后 executor 会立即调用 CreateSubagent。
// 竞态修复（2026-10-29 CI 两连挂）：AddTask 锁内注册后 scheduler 异步唤醒
// executeTask，而 spawn 工具的 toolkit.UpsertRunning 在 AddTask 返回后才执行
// ——CreateSubagent 可能先于行 upsert 到达。查不到行时轮询等待行出现
// （UpsertRunning 必然紧随其后），超时才回落快子代理。
func (s *SessionCancelE2ESuite) setDescriptionBoundSubagents(
	agent *harness.DeepAgent,
	slowDescription string,
	slowDelay time.Duration,
	slowOutput string,
) {
	agent.SetCreateSubagentOverride(func(ctx context.Context, _, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
		// 按 sub_session_id 查 toolkit 行，按 Description 绑定快慢；
		// 行尚未 upsert 时短暂轮询（AddTask→UpsertRunning 窗口）
		deadline := time.Now().Add(2 * time.Second)
		for {
			var matched *hsubagent.SessionTaskRow
			for _, row := range agent.GetSessionToolkit().ListAll() {
				if row.SubSessionID == subSessionID {
					m := row
					matched = &m
					break
				}
			}
			if matched != nil {
				if matched.Description == slowDescription {
					return newFakeSubAgent(ctx, slowDelay, slowOutput), nil
				}
				return newFakeSubAgent(ctx, 0, "b done"), nil
			}
			if time.Now().After(deadline) {
				// 未匹配时返回快子代理
				return newFakeSubAgent(ctx, 0, "b done"), nil
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

// waitForTaskStatus 轮询等待指定任务达到目标状态。
func waitForTaskStatus(toolkit *hsubagent.SessionToolkit, taskID, wantStatus string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if row, ok := toolkit.Get(taskID); ok && row.Status == wantStatus {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("等待任务 %s 达到 %s 状态超时（%v）", taskID, wantStatus, timeout)
}

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSubAgent 可控子代理替身，模拟慢/快子代理的 Invoke 行为。
// 对齐 Python: TestDeepAgentSessionRailCancelMock._SleepSubAgent / _FastSubAgent。
//
// 延迟期间监听 ctx 取消：TaskScheduler.CancelTask 取消运行中任务时，
// SessionSpawnExecutor 的后台 goroutine ctx 被取消，子代理 Invoke 应返回
// ctx.Err()（对齐 Python asyncio.sleep 被 cancel 打断的语义）。
type fakeSubAgent struct {
	// delay 模拟执行延迟（0 表示立即完成）
	delay time.Duration
	// output 完成时的输出
	output string
	// invokeStarted invoke 被调用的次数
	invokeStarted atomic.Int32
}

// steeringObserver steering 注入观测器，记录模型调用消息中的 [STEERING] 标记。
// 对齐 Python: LoopObserveRail(steer_text=steer_marker)。
type steeringObserver struct {
	agentinterfaces.BaseRail
	// steerSeen 是否在模型消息中观测到 [STEERING] 标记
	steerSeen atomic.Bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证
var _ interfaces.DeepAgentInterface = (*fakeSubAgent)(nil)
var _ agentinterfaces.AgentRail = (*steeringObserver)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// newFakeSubAgent 创建子代理替身。
func newFakeSubAgent(_ context.Context, delay time.Duration, output string) *fakeSubAgent {
	return &fakeSubAgent{delay: delay, output: output}
}

// Invoke 模拟子代理执行：延迟后返回 output；ctx 取消时立即返回错误。
func (f *fakeSubAgent) Invoke(ctx context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	f.invokeStarted.Add(1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]any{"output": f.output}, nil
}

// InvokeCount 返回 invoke 被调用次数（断言用）。
func (f *fakeSubAgent) InvokeCount() int32 {
	return f.invokeStarted.Load()
}

// SteerSeen 返回是否观测到 steering 标记。
func (o *steeringObserver) SteerSeen() bool {
	return o.steerSeen.Load()
}

// Reset 重置观测状态。
func (o *steeringObserver) Reset() {
	o.steerSeen.Store(false)
}

// GetCallbacks 注册 BeforeModelCall 回调。
func (o *steeringObserver) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return o.BuildCallbacks(
		o.CallbackFrom(agentinterfaces.CallbackBeforeModelCall, func(ctx context.Context, railCtx any) error {
			return o.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// BeforeModelCall 检查模型消息中是否包含 [STEERING] 标记。
func (o *steeringObserver) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil || cbc.Inputs() == nil {
		return nil
	}
	modelInputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || modelInputs == nil || modelInputs.Messages == nil {
		return nil
	}
	for _, msg := range modelInputs.Messages {
		content := msg.GetContent()
		if content.IsText() {
			text := content.Text()
			if len(text) >= len("[STEERING]") && containsSteering(text) {
				o.steerSeen.Store(true)
			}
		}
	}
	return nil
}

// newSteeringObserver 创建 steering 观测器。
func newSteeringObserver() *steeringObserver {
	return &steeringObserver{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsSteering 检查文本是否包含 [STEERING] 标记。
func containsSteering(text string) bool {
	for i := 0; i+len("[STEERING]") <= len(text); i++ {
		if text[i:i+len("[STEERING]")] == "[STEERING]" {
			return true
		}
	}
	return false
}

// ─────────── fakeSubAgent 其余接口面（桩实现） ───────────

// 以下方法为 DeepAgentInterface 的桩实现，仅满足接口编译约束。

// ReactAgent 返回 nil 桩。
func (f *fakeSubAgent) ReactAgent() *agents.ReActAgent { return nil }

// LoopCoordinator 返回 nil 桩。
func (f *fakeSubAgent) LoopCoordinator() interfaces.LoopCoordinatorInterface { return nil }

// LoopController 返回 nil 桩。
func (f *fakeSubAgent) LoopController() controller.ControllerInterface { return nil }

// EventHandler 返回 nil 桩。
func (f *fakeSubAgent) EventHandler() modules.EventHandler { return nil }

// LoadState 返回默认状态桩。
func (f *fakeSubAgent) LoadState(sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	st := hschema.NewDeepAgentState()
	return &st
}

// DeepConfig 返回空配置桩。
func (f *fakeSubAgent) DeepConfig() *hschema.DeepAgentConfig { return nil }

// IsInvokeActive 返回 false 桩。
func (f *fakeSubAgent) IsInvokeActive() bool { return false }

// IsAutoInvokeScheduled 返回 false 桩。
func (f *fakeSubAgent) IsAutoInvokeScheduled() bool { return false }

// SetAutoInvokeScheduled 空操作桩。
func (f *fakeSubAgent) SetAutoInvokeScheduled(bool) {}

// ScheduleAutoInvokeOnSpawnDone 返回 nil 桩。
func (f *fakeSubAgent) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}

// CreateSubagent 返回 nil 桩。
func (f *fakeSubAgent) CreateSubagent(context.Context, string, string) (interfaces.DeepAgentInterface, error) {
	return nil, nil
}

// SwitchMode 空操作桩。
func (f *fakeSubAgent) SwitchMode(sessioninterfaces.SessionFacade, string) {}

// RestoreModeAfterPlanExit 空操作桩。
func (f *fakeSubAgent) RestoreModeAfterPlanExit(sessioninterfaces.SessionFacade) {}

// GetPlanFilePath 返回空字符串桩。
func (f *fakeSubAgent) GetPlanFilePath(sessioninterfaces.SessionFacade) string { return "" }

// SaveState 空操作桩。
func (f *fakeSubAgent) SaveState(sessioninterfaces.SessionFacade, *hschema.DeepAgentState) {}

// FindRailsByType 返回 nil 桩。
func (f *fakeSubAgent) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail { return nil }

// AddRail 空操作桩。
func (f *fakeSubAgent) AddRail(agentinterfaces.AgentRail) {}

// UnregisterRail 返回 nil 桩。
func (f *fakeSubAgent) UnregisterRail(context.Context, agentinterfaces.AgentRail) error { return nil }

// InnerInvokeOverride 返回 nil 桩。
func (f *fakeSubAgent) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// Configure 返回 nil 桩。
func (f *fakeSubAgent) Configure(context.Context, agentinterfaces.AgentConfig) error { return nil }

// Stream 返回 nil 桩。
func (f *fakeSubAgent) Stream(context.Context, map[string]any, ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

// Card 返回测试卡片桩。
func (f *fakeSubAgent) Card() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentName("fake_sub_agent"))
}

// Config 返回 nil 桩。
func (f *fakeSubAgent) Config() agentinterfaces.AgentConfig { return nil }

// AbilityManager 返回 nil 桩。
func (f *fakeSubAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }

// CallbackManager 返回 nil 桩。
func (f *fakeSubAgent) CallbackManager() *agentinterfaces.AgentCallbackManager { return nil }

// SystemPromptBuilder 返回 nil 桩。
func (f *fakeSubAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return nil }

// RegisterCallback 返回 nil 桩。
func (f *fakeSubAgent) RegisterCallback(context.Context, agentinterfaces.AgentCallbackEvent, cb.PerAgentCallbackFunc, ...cb.CallbackOption) error {
	return nil
}

// RegisterRail 返回 nil 桩。
func (f *fakeSubAgent) RegisterRail(context.Context, agentinterfaces.AgentRail, ...cb.CallbackOption) error {
	return nil
}

// UapswarmProjectDir 返回空字符串桩。
func (f *fakeSubAgent) UapswarmProjectDir() string { return "" }

// SetUapswarmProjectDir 空操作桩。
func (f *fakeSubAgent) SetUapswarmProjectDir(string) {}

// UapswarmCodeProjectDir 返回空字符串桩。
func (f *fakeSubAgent) UapswarmCodeProjectDir() string { return "" }

// SetUapswarmCodeProjectDir 空操作桩。
func (f *fakeSubAgent) SetUapswarmCodeProjectDir(string) {}

// UapswarmAdapterMode 返回空字符串桩。
func (f *fakeSubAgent) UapswarmAdapterMode() string { return "" }

// SetUapswarmAdapterMode 空操作桩。
func (f *fakeSubAgent) SetUapswarmAdapterMode(string) {}
