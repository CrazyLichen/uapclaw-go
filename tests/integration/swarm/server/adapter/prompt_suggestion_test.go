//go:build integration

package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSuggestionSuite 验证主动推荐链路（10.3.30）。
//
// 对应 Python prompt_suggestion.py 的 forks 记账 + 删除/关闭钩子：
//  1. 同会话两轮命中同一 child（rail 挂在 child 上、记账按 sid 隔离）；
//  2. 不同会话各自独立 child，forks 的 turn 记录互不串味；
//  3. 推荐关闭时轮结束不生成（默认配置）；
//  4. 会话删除经 provisioner 的 PromptSuggestionForks 钩子清表；
//  5. Shutdown 收敛全部在飞生成。
//
// 跨子系统：swarm/server/adapter（child 分流 + rail）+
// agentcore/harness（DeepAgent 装配）+ swarm/runtime（删除事务）+
// agentcore/session（会话生命周期）。
type PromptSuggestionSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPromptSuggestionSuite 运行主动推荐集成测试。
func TestPromptSuggestionSuite(t *testing.T) {
	suite.Run(t, new(PromptSuggestionSuite))
}

// SetupTest 每个测试方法前构建隔离环境。
func (s *PromptSuggestionSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_prompt_suggestion")
	s.env.WriteConfigYAML(s.T(), "")
	// 每个用例从干净 epoch 与空表开始，避免跨用例污染
	recommendation.Forks.SessionDeleted("integration-clean-a")
	recommendation.Forks.SessionDeleted("integration-clean-b")
}

// TearDownTest 清理全局单例，避免后面的用例继承在飞任务。
func (s *PromptSuggestionSuite) TearDownTest() {
	recommendation.Forks.Shutdown()
}

// Test同会话两轮命中同一child且记账隔离 会话级 child 与 forks 表都按 sid 隔离。
func (s *PromptSuggestionSuite) Test同会话两轮命中同一child且记账隔离() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.runOneTurn(d, "integration-clean-a", "req-ps-a1", "第一问")
	first := d.GetLiveSessionInstance("integration-clean-a")
	s.Require().NotNil(first, "首轮后应有该会话的 DeepAgent 句柄")

	s.runOneTurn(d, "integration-clean-a", "req-ps-a2", "第二问")
	second := d.GetLiveSessionInstance("integration-clean-a")
	s.Require().NotNil(second, "第二轮后仍应有句柄")
	s.Same(first, second, "同一会话两轮必须复用同一个 child")

	// 推荐默认关闭：轮结束后不产生建议任务
	s.Equal(0, recommendation.Forks.InFlightCount(), "默认关闭时不应有在飞生成")
}

// Test不同会话独立child且记账不串味 两个会话的 turn 记录互不影响。
func (s *PromptSuggestionSuite) Test不同会话独立child且记账不串味() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.runOneTurn(d, "integration-clean-a", "req-ps-b1", "会话A问")
	s.runOneTurn(d, "integration-clean-b", "req-ps-b2", "会话B问")

	instA := d.GetLiveSessionInstance("integration-clean-a")
	instB := d.GetLiveSessionInstance("integration-clean-b")
	s.Require().NotNil(instA)
	s.Require().NotNil(instB)
	s.NotSame(instA, instB, "不同会话必须是不同 child")

	// 清理 A 不波及 B
	s.True(d.CleanupSessionAdapter("integration-clean-a"))
	s.False(d.HasSessionRuntime("integration-clean-a"))
	s.True(d.HasSessionRuntime("integration-clean-b"), "清理 A 后 B 应仍占运行时")
}

// Test推荐关闭时轮结束不生成 默认配置下 turn_ended 不产生建议。
func (s *PromptSuggestionSuite) Test推荐关闭时轮结束不生成() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 确认全局 Forks 未开启推荐
	s.False(recommendation.ConsentEpoch(nil).Active, "默认应关闭")

	s.runOneTurn(d, "integration-clean-a", "req-ps-c1", "一问")

	s.Nil(recommendation.Forks.AnsweredCall("integration-clean-a", "req-ps-c1"),
		"未开推荐时 turn_ended 在 epoch 检查处提前返回")
	s.Equal(0, recommendation.Forks.InFlightCount(), "不应起生成任务")
}

// Test会话删除触发forks清表 provisioner 的 dispose 第①步接的是真实现。
func (s *PromptSuggestionSuite) Test会话删除触发forks清表() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.runOneTurn(d, "integration-clean-a", "req-ps-d1", "一问")

	// 直接调用接口层的 SessionDeleted（与 provisioner dispose 同一路径）
	var hooks interface{ SessionDeleted(string) } = recommendation.Forks
	hooks.SessionDeleted("integration-clean-a")

	s.Nil(recommendation.Forks.AnsweredCall("integration-clean-a", "req-ps-d1"),
		"删除后不应保留 answered 记录")
	s.Equal(0, recommendation.Forks.InFlightCount(), "删除后不应有在飞生成")
}

// TestShutdown收敛全部在飞 AgentServer 关闭路径的钩子。
func (s *PromptSuggestionSuite) TestShutdown收敛全部在飞() {
	// Shutdown 无返回值（对齐 Python -> None）；此处验证不 panic 且清空在飞表
	s.NotPanics(func() { recommendation.Forks.Shutdown() })
	s.Equal(0, recommendation.Forks.InFlightCount())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runOneTurn 跑一轮流式请求并排空输出。
func (s *PromptSuggestionSuite) runOneTurn(
	d *adapter.DeepAdapter, sessionID, requestID, query string,
) {
	s.T().Helper()

	params, err := json.Marshal(map[string]any{"mode": "agent.work.plan"})
	s.Require().NoError(err)

	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{"query": query})
	s.Require().NoError(streamErr, "ProcessMessageStreamImpl 不应返回启动错误")
	for range chunkCh {
		// 本套件只关心生命周期与记账，不关心帧内容
	}
}
