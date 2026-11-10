//go:build integration

package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionForkSuite 验证会话级 adapter 分身（10.3.28f）。
//
// 对应 Python interface_deep.py 的 _session_adapters（:2126）：
// 根只做路由，每会话由 CreateAdapter 造一个 child 独享 DeepAgent。
//
// 覆盖：
//  1. 同会话两轮命中同一 child（复用而非重复创建）；
//  2. 不同会话各自独立 child，清理其中一个不影响另一个；
//  3. child 的 DeepAgent 句柄可经 GetLiveSessionInstance 取到（25.8 铺路）；
//  4. attach-only 订阅创建 child 但不登记在飞任务。
type SessionForkSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionForkSuite 运行会话分身集成测试。
func TestSessionForkSuite(t *testing.T) {
	suite.Run(t, new(SessionForkSuite))
}

// SetupTest 每个测试方法前构建隔离环境。
func (s *SessionForkSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_session_fork")
	s.env.WriteConfigYAML(s.T(), "")
}

// Test同会话两轮命中同一child 同一 sessionID 的两次请求复用同一 child 实例。
func (s *SessionForkSuite) Test同会话两轮命中同一child() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 根不自建 DeepAgent（spec DEC-6 / D-6）
	s.Nil(d.Instance(), "根 CreateInstance 后不应有 instance（router only）")

	s.runOneTurn(d, "fork-same-1", "req-fork-same-1", "第一问")
	// 两轮之间 child 应保留在缓存里（未过期、未被淘汰）
	s.True(d.HasSessionRuntime("fork-same-1"), "首轮后该会话应占运行时")

	s.runOneTurn(d, "fork-same-1", "req-fork-same-2", "第二问")
	s.True(d.HasSessionRuntime("fork-same-1"), "第二轮后仍应占运行时")
}

// Test不同会话各自独立child 两个会话得到独立 child，清理一个不影响另一个。
func (s *SessionForkSuite) Test不同会话各自独立child() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.runOneTurn(d, "fork-a", "req-fork-a", "会话A的问")
	s.runOneTurn(d, "fork-b", "req-fork-b", "会话B的问")

	s.True(d.HasSessionRuntime("fork-a"), "会话 A 应占运行时")
	s.True(d.HasSessionRuntime("fork-b"), "会话 B 应占运行时")

	// 清理 A 不应波及 B
	_ = d.CleanupSessionAdapter("fork-a")
	s.False(d.HasSessionRuntime("fork-a"), "清理后 A 不应再占运行时")
	s.True(d.HasSessionRuntime("fork-b"), "清理 A 后 B 应仍占运行时")
}

// TestGetLiveSessionInstance_返回该会话的DeepAgent 25.8 的 per-session 句柄。
func (s *SessionForkSuite) TestGetLiveSessionInstance_返回该会话的DeepAgent() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 无 child 时应返回 nil
	s.Nil(d.GetLiveSessionInstance("fork-live"), "无 child 时应返回 nil")

	s.runOneTurn(d, "fork-live", "req-fork-live", "一问")

	inst := d.GetLiveSessionInstance("fork-live")
	s.Require().NotNil(inst, "有 child 后应能取到该会话的 DeepAgent 句柄")
	// 该句柄必须是 child 自己的实例，而不是根的（根此时没有 instance）
	s.Nil(d.Instance(), "根仍不应有 instance")

	// 未启动过的其他会话仍为 nil
	s.Nil(d.GetLiveSessionInstance("fork-not-started"), "未启动的会话应返回 nil")
}

// TestAttachOnly_创建child但不登记在飞任务 被动订阅语义。
func (s *SessionForkSuite) TestAttachOnly_创建child但不登记在飞任务() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	params, err := json.Marshal(map[string]any{
		"mode":             "agent.work.plan",
		"team_attach_only": true,
	})
	s.Require().NoError(err)

	sid := "fork-attach-only"
	req := &schema.AgentRequest{
		RequestID: "req-fork-attach",
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{"query": "订阅"})
	s.Require().NoError(streamErr)
	for range chunkCh {
	}

	// attach-only 仍创建 child（要接续既有会话），但不登记在飞任务
	s.NotNil(d.GetLiveSessionInstance(sid), "attach-only 仍应创建 child")
}

// runOneTurn 跑一轮流式对话并消费到底。
func (s *SessionForkSuite) runOneTurn(d *adapter.DeepAdapter, sessionID, requestID, query string) {
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
		// 本 suite 只关心生命周期，不关心帧内容
	}
}
