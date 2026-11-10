//go:build integration

package adapter_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalInteractionSuite goal 在真实交互链路下的输出语义（对照基线）。
//
// 无活跃 goal 时共享流上只有 user 轮，不得出现分气泡 final —— 这是
// 「goal 轮在飞」那些分支的对照组：先证明基线干净，后续补 goal 轮用例时
// 才能把差异归因到 goal 本身。
//
// 跨子系统：swarm/server/adapter（租约 + 轮次闩 + 分气泡）+ agentcore/harness
// （交互监督器）+ agentcore/single_agent（ReAct 轮次）。
type GoalInteractionSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalInteractionSuite 套件入口。
func TestGoalInteractionSuite(t *testing.T) {
	suite.Run(t, new(GoalInteractionSuite))
}

// SetupTest 每测试隔离环境。
func (s *GoalInteractionSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "goal_interaction")
}

// TestNoGoal_不产生分气泡final 没有活跃目标时，共享流只有 user 轮的正常收尾，
// 不得凭空多出一个空 final（那会让前端提前关气泡）。
// Python: interface_deep.py:15574-15617（beginVisibleChatContent 的边界条件）
func (s *GoalInteractionSuite) TestNoGoal_不产生分气泡final() {
	s.env.WriteConfigYAML(s.T(), "")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse(goalItUserAnswer))

	d := s.newGoalAdapter()
	frames := s.streamFrames(d, goalItSession, "普通提问")

	userFinal := indexOfFinal(frames, goalItUserAnswer)
	s.Require().GreaterOrEqual(userFinal, 0, "user 轮必须有带内容的 chat.final")
	s.Equal(-1, indexOfEmptyFinalAfter(frames, userFinal),
		"没有活跃 goal 时不得出现分气泡 final")
}

// TestGoalRound_用户轮后接goal轮并分气泡 user 轮正常收尾，goal 轮在其后接棒，
// 两段内容不得糊进同一个气泡。
// Python: interface_deep.py:15574-15617（beginVisibleChatContent）
//
// 注意：本用例用**有界收集**（deadline）而非等流关闭。当前 goal 轮在
// max_attempts=1 下仍会无限重试——`ApplyAssessment` 收到的 goalID 为空，
// 与记录里的 goal_id 不匹配后静默跳过，状态永远停在 ACTIVE（详见
// docs/superpowers/plans 的已知问题）。因此这里只断言「goal 轮确实接棒
// 且分气泡正确」，不依赖流自然结束。
func (s *GoalInteractionSuite) TestGoalRound_用户轮后接goal轮并分气泡() {
	s.env.WriteConfigYAML(s.T(), "")
	s.MockLLM.SetResponses(mockllm.CreateTextResponse(goalItUserAnswer))

	d := s.newGoalAdapter()
	out, err := d.DispatchGoalControl(context.Background(), goalItSession, "set", goalItObjective, false, 0, 1)
	s.Require().NoError(err)
	s.Require().Equal("goal_stream", out["result_type"], "set 必须返回 goal_stream")

	frames := s.streamFramesBounded(d, goalItSession, "开始干活", goalRoundFrameBudget)

	userFinal := indexOfFinal(frames, goalItUserAnswer)
	s.Require().GreaterOrEqual(userFinal, 0, "user 轮必须有带内容的 chat.final")

	boundary := indexOfEmptyFinalAfter(frames, userFinal)
	s.Require().Greater(boundary, userFinal, "user 轮之后必须出现分气泡 final（空内容）")
	s.Require().True(hasDeltaAfter(frames, boundary), "分气泡之后必须有 goal 轮的 chat.delta")

	getOut, err := d.DispatchGoalControl(context.Background(), goalItSession, "get", "", false, 0, 0)
	s.Require().NoError(err)
	record, ok := getOut["goal"].(map[string]any)
	s.Require().True(ok, "get 必须返回 goal 记录")
	attempts, _ := record["attempt_count"].(int)
	s.GreaterOrEqual(attempts, 1, "goal 轮必须至少执行过一次")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newGoalAdapter 建根适配器并 CreateInstance（child 尾段自动 start_interaction）。
func (s *GoalInteractionSuite) newGoalAdapter() *adapter.DeepAdapter {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// streamFrames 跑一轮流式对话并返回帧的 payload 列表。
func (s *GoalInteractionSuite) streamFrames(
	d *adapter.DeepAdapter, sessionID, query string,
) []map[string]any {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.work.plan"})
	s.Require().NoError(err)

	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: "req-" + sessionID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{
		"query":           query,
		"conversation_id": sessionID,
	})
	s.Require().NoError(streamErr, "ProcessMessageStreamImpl 不应返回启动错误")

	var frames []map[string]any
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		frames = append(frames, chunk.Payload)
	}
	s.Require().NotEmpty(frames, "流式必须有帧")
	return frames
}

// streamFramesBounded 跑一轮流式并按帧数上限收集（流可能不自然结束）。
func (s *GoalInteractionSuite) streamFramesBounded(
	d *adapter.DeepAdapter, sessionID, query string, limit int,
) []map[string]any {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.work.plan"})
	s.Require().NoError(err)

	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: "req-" + sessionID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{
		"query":           query,
		"conversation_id": sessionID,
	})
	s.Require().NoError(streamErr, "ProcessMessageStreamImpl 不应返回启动错误")

	var frames []map[string]any
	deadline := time.After(goalRoundCollectTimeout)
	for len(frames) < limit {
		select {
		case <-deadline:
			return frames
		case chunk, ok := <-chunkCh:
			if !ok {
				return frames
			}
			if chunk != nil && chunk.Payload != nil {
				frames = append(frames, chunk.Payload)
			}
		}
	}
	return frames
}

// hasDeltaAfter 下标之后是否还有 chat.delta。
func hasDeltaAfter(frames []map[string]any, from int) bool {
	for i := from + 1; i < len(frames); i++ {
		if eventType, _ := frames[i]["event_type"].(string); eventType == "chat.delta" {
			return true
		}
	}
	return false
}

// indexOfFinal 找内容等于 content 的 chat.final 下标（找不到返回 -1）。
func indexOfFinal(frames []map[string]any, content string) int {
	for i, frame := range frames {
		if isFinalWith(frame, content) {
			return i
		}
	}
	return -1
}

// indexOfEmptyFinalAfter 从 from 之后找空内容的 chat.final 下标。
func indexOfEmptyFinalAfter(frames []map[string]any, from int) int {
	for i := from + 1; i < len(frames); i++ {
		if isFinalWith(frames[i], "") {
			return i
		}
	}
	return -1
}

// isFinalWith 判断是否为指定内容的 chat.final。
func isFinalWith(frame map[string]any, content string) bool {
	if eventType, _ := frame["event_type"].(string); eventType != "chat.final" {
		return false
	}
	got, _ := frame["content"].(string)
	return got == content
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// goalItSession 本套件的会话标识
	goalItSession = "it-goal-interaction"
	// goalItUserAnswer 用户轮的模型回答
	goalItUserAnswer = "已完成第一步。"
	// goalItObjective 目标文本
	goalItObjective = "修完测试"
	// goalRoundFrameBudget 有界收集的帧数上限（够覆盖 user 轮 + 分气泡 + 若干 goal 轮）
	goalRoundFrameBudget = 30
	// goalRoundCollectTimeout 有界收集的墙钟上限
	goalRoundCollectTimeout = 15 * time.Second
)
