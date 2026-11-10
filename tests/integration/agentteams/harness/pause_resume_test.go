//go:build integration

package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// PauseResumeSuite 暂停/恢复/回滚集成测试。
//
// 跨度（规则 7）：agent_teams/harness → agentcore/harness/task_loop
// → single_agent/agents → agentcore/session（≥3 个 internal 二级子系统）。
type PauseResumeSuite struct {
	isuite.AgentSuite
}

// TestPauseResumeSuite 套件入口。
func TestPauseResumeSuite(t *testing.T) {
	suite.Run(t, new(PauseResumeSuite))
}

// newStartedHarness 构造并 Start 一个带 MockLLM 的 NativeHarness。
func (s *PauseResumeSuite) newStartedHarness() (*teamharness.NativeHarness, *session.Session) {
	spec, err := s.NewSpecWithMockLLM(4)
	s.Require().NoError(err)
	h, err := teamharness.NewNativeHarness(spec, nil)
	s.Require().NoError(err)
	sess := session.CreateAgentSession("pr-sess", h.DeepAgent().Card(), nil)
	s.Require().NoError(sess.PreRun(s.Ctx, map[string]any{}))
	s.Require().NoError(h.Start(s.Ctx, sess))
	return h, sess
}

// countContent 统计上下文中内容完全匹配的消息条数。
func countContent(msgs []llmschema.BaseMessage, want string) int {
	n := 0
	for _, m := range msgs {
		if m.GetContent().String() == want {
			n++
		}
	}
	return n
}

// contextMessages 读取本轮上下文段。
func (s *PauseResumeSuite) contextMessages(
	h *teamharness.NativeHarness, sess *session.Session, withHistory bool,
) []llmschema.BaseMessage {
	mc := h.DeepAgent().ReactAgent().ContextEngine().GetContext("default_context", sess.GetSessionID())
	s.Require().NotNil(mc)
	msgs, err := mc.GetMessages(0, withHistory)
	s.Require().NoError(err)
	return msgs
}

// TestI01_模型相位暂停后恢复不重复用户消息 验证续跑轮不追加新的 user turn。
func (s *PauseResumeSuite) TestI01_模型相位暂停后恢复不重复用户消息() {
	h, sess := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	// 首帧卡死 5s：确保暂停落在 MODEL 相位（脚本在 Stream 调用时即出队）
	s.MockLLM.WithStreamScripts(mockllm.StreamChunkScript{
		StallBefore: 5 * time.Second,
		Chunks:      []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("完成")},
	})

	s.Require().NoError(h.Send(s.Ctx, "原始任务", false))
	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateRunning },
		5*time.Second, 20*time.Millisecond)
	s.Require().NoError(h.Pause(s.Ctx))
	s.Require().Equal(harnessprotocol.HarnessStatePaused, h.State())

	before := countContent(s.contextMessages(h, sess, true), "原始任务")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("续跑完成"))
	s.Require().NoError(h.Resume(s.Ctx, nil))
	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateIdle },
		10*time.Second, 50*time.Millisecond)

	after := countContent(s.contextMessages(h, sess, true), "原始任务")
	s.Equal(before, after, "续跑轮不得重复追加 user turn")
}

// TestI03_立即中止回滚并投递marker 验证 abort(immediate) 回滚半轮并作废输出。
func (s *PauseResumeSuite) TestI03_立即中止回滚并投递marker() {
	h, sess := s.newStartedHarness()
	defer func() { _ = h.Stop(context.Background()) }()

	s.MockLLM.WithStreamScripts(mockllm.StreamChunkScript{
		StallBefore: 5 * time.Second,
		Chunks:      []*llmschema.AssistantMessageChunk{llmschema.NewAssistantMessageChunk("不该出现")},
	})
	s.Require().NoError(h.Send(s.Ctx, "长任务", false))
	s.Require().Eventually(func() bool { return h.State() == harnessprotocol.HarnessStateRunning },
		5*time.Second, 20*time.Millisecond)
	// 等到内层上下文已创建：否则 abort 可能落在 initContext 之前，
	// 回滚无对象可回（不是行为缺陷，是测试时序）。
	s.Require().Eventually(func() bool {
		return h.DeepAgent().ReactAgent().ContextEngine().
			GetContext("default_context", sess.GetSessionID()) != nil
	}, 5*time.Second, 20*time.Millisecond)

	s.Require().NoError(h.Abort(s.Ctx, true))
	s.Require().Equal(harnessprotocol.HarnessStateIdle, h.State())

	// marker 必须到达输出（本用例是唯一消费者）
	readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sawMarker := false
	for {
		chunk, ok := h.Outputs().Next(readCtx)
		if !ok {
			break
		}
		if out, isOut := chunk.(stream.OutputSchema); isOut && out.Type == "round_aborted" {
			sawMarker = true
		}
	}
	s.True(sawMarker, "immediate abort 必须投递 round_aborted 标记")

	s.Equal(0, countContent(s.contextMessages(h, sess, false), "不该出现"),
		"半轮 assistant 消息必须被回滚")
}

// TestI05_冷恢复续跑不追加用户消息 验证 pause→stop→start 等价于 resume。
func (s *PauseResumeSuite) TestI05_冷恢复续跑不追加用户消息() {
	h, sess := s.newStartedHarness()
	s.Require().NoError(h.Stop(context.Background()))

	// 同一会话重建 harness：模拟 pause → stop → start
	spec, err := s.NewSpecWithMockLLM(4)
	s.Require().NoError(err)
	h2, err := teamharness.NewNativeHarness(spec, nil)
	s.Require().NoError(err)
	s.Require().NoError(h2.Start(s.Ctx, sess))
	defer func() { _ = h2.Stop(context.Background()) }()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("续跑完成"))
	s.Require().NoError(h2.Resume(s.Ctx, "原始任务"))
	s.Require().Eventually(func() bool { return h2.State() == harnessprotocol.HarnessStateIdle },
		10*time.Second, 50*time.Millisecond)

	s.Equal(0, countContent(s.contextMessages(h2, sess, true), "原始任务"),
		"冷恢复轮不得追加 user turn")
}
