//go:build integration

package adapter_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RailRegistrationSuite 覆盖演进/创建 rail 的注册生命周期：
// 冷启动、计划模式消息、热重载、slash 首轮、skill_create 开关。
type RailRegistrationSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRailRegistrationSuite 运行 rail 注册生命周期集成测试。
func TestRailRegistrationSuite(t *testing.T) {
	suite.Run(t, new(RailRegistrationSuite))
}

// TestSlashFirstMessage_EvolutionRailRegistered 会话首条消息为 /evolve_list 时
// 演进 rail 必须被注册（当前实现只建不注册，改动前必红）。
func (s *RailRegistrationSuite) TestSlashFirstMessage_EvolutionRailRegistered() {
	d := s.newAdapter("agent.plan", evolutionExtra(true, false, false))
	s.Require().Equal(0, s.countEvolutionRails(d), "冷启动不应注册演进 rail")

	req := schema.NewAgentRequest("req-slash", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "/evolve_list", "mode": "agent.plan"}),
		schema.WithAgentSessionID("itest-rail"))
	_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
	s.Require().NoError(err)

	s.Equal(1, s.countEvolutionRails(d), "首条 /evolve_list 之后演进 rail 必须已注册")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每测试隔离数据目录与 mock LLM provider。
func (s *RailRegistrationSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_rail_registration")
}

// newAdapter 写入配置并装配 DeepAdapter。
func (s *RailRegistrationSuite) newAdapter(mode, reactExtra string) *adapter.DeepAdapter {
	s.env.WriteConfigYAML(s.T(), reactExtra)
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, mode, ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// evolutionExtra 生成 react.evolution 配置片段。
func evolutionExtra(enabled, autoScan, skillCreate bool) string {
	return "  evolution:\n" +
		"    enabled: " + boolText(enabled) + "\n" +
		"    auto_scan: " + boolText(autoScan) + "\n" +
		"    skill_create: " + boolText(skillCreate) + "\n"
}

// boolText 把 bool 转成 YAML 小写字面量。
func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// countEvolutionRails 返回实例上已注册的 SkillEvolutionRail 数量。
func (s *RailRegistrationSuite) countEvolutionRails(d *adapter.DeepAdapter) int {
	return len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{})))
}

// jsonParams 将 map 序列化为请求参数。
func (s *RailRegistrationSuite) jsonParams(params map[string]any) json.RawMessage {
	data, err := json.Marshal(params)
	s.Require().NoError(err)
	return data
}
