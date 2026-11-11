//go:build integration

package adapter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RootInstanceLazySuite 验证根 adapter 的 DeepAgent 惰建（10.3.28f）。
//
// 对应 Python _skip_own_instance_build（:12727）与 ensure_instance（:12809）：
// 根是 router + template holder，chat 每轮跑在 child 上，给根也建一份会
// 双倍工具注册、双倍 rail 装配、双倍 ensure_initialized，全挂关键路径。
type RootInstanceLazySuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRootInstanceLazySuite 运行根惰建集成测试。
func TestRootInstanceLazySuite(t *testing.T) {
	suite.Run(t, new(RootInstanceLazySuite))
}

// SetupTest 每个测试方法前构建隔离环境。
func (s *RootInstanceLazySuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_root_lazy")
	s.env.WriteConfigYAML(s.T(), "")
}

// TestCreateInstance后根无instance 根的 CreateInstance 不建 DeepAgent。
func (s *RootInstanceLazySuite) TestCreateInstance后根无instance() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.Nil(d.Instance(), "根 CreateInstance 后不应有 instance（router only）")
}

// TestEnsureInstance触发惰建 显式调用 EnsureInstance 才建。
func (s *RootInstanceLazySuite) TestEnsureInstance触发惰建() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.Nil(d.Instance(), "初始不应有 instance")

	inst, err := d.EnsureInstance(context.Background())
	s.Require().NoError(err)
	s.NotNil(inst, "EnsureInstance 应建出 DeepAgent")
	s.NotNil(d.Instance(), "惰建后 Instance() 应非 nil")
}

// TestSwitchMode触发惰建 非 chat RPC 走 EnsureInstance 而非假设根已有实例。
func (s *RootInstanceLazySuite) TestSwitchMode触发惰建() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.Nil(d.Instance(), "初始不应有 instance")
	// SwitchMode 是根上的非 chat RPC，必须先惰建再操作
	s.Require().NoError(d.SwitchMode(s.Ctx, "lazy-switch-session", "plan"))
	s.NotNil(d.Instance(), "SwitchMode 后根应有 instance（已惰建）")
}

// TestChild不建根instance chat 首轮只建 child，不建根。
func (s *RootInstanceLazySuite) TestChild不建根instance() {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	s.runOneTurn(d, "lazy-child-only", "req-lazy-child", "一问")

	// chat 路径建的是 child，根仍无 instance
	s.Nil(d.Instance(), "chat 首轮不应建根 instance")
	s.NotNil(d.GetLiveSessionInstance("lazy-child-only"), "应建出 child 的 DeepAgent")
}

// runOneTurn 跑一轮流式对话并消费到底。
func (s *RootInstanceLazySuite) runOneTurn(d *adapter.DeepAdapter, sessionID, requestID, query string) {
	s.T().Helper()

	params := json.RawMessage(`{"mode":"agent.work.plan"}`)
	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(s.Ctx, req, map[string]any{"query": query})
	s.Require().NoError(streamErr)
	for range chunkCh {
	}
}
