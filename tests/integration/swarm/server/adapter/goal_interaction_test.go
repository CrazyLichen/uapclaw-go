//go:build integration

package adapter_test

import (
	"encoding/json"
	"testing"

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
)
