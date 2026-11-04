//go:build integration

package adapter_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
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
// 演进 rail 必须被注册（改动前懒加载只建不注册，必红）。
//
// 冷启动用 agent 模式：此时计划模式尚未进入，字段为 nil；首条消息走 slash 分支
// 懒加载，若只建不注册，随后的 updatePlanModeRails 会因字段非 nil 而跳过注册。
func (s *RailRegistrationSuite) TestSlashFirstMessage_EvolutionRailRegistered() {
	d := s.newAdapter("agent", evolutionExtra(true, false, false))
	s.Require().Equal(0, s.countEvolutionRails(d), "冷启动不应注册演进 rail")

	req := schema.NewAgentRequest("req-slash", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "/evolve_list", "mode": "agent.work.plan"}),
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

// TestPlanMessage_EvolutionRailRegistered 计划模式消息后注册且幂等。
func (s *RailRegistrationSuite) TestPlanMessage_EvolutionRailRegistered() {
	d := s.newAdapter("agent", evolutionExtra(true, false, false))
	s.Require().Equal(0, s.countEvolutionRails(d), "冷启动（agent 模式）不应注册演进 rail")

	for i := 0; i < 2; i++ {
		req := schema.NewAgentRequest("req-plan", "itest-channel", schema.ReqMethodChatSend,
			s.jsonParams(map[string]any{"query": "你好", "mode": "agent.work.plan"}),
			schema.WithAgentSessionID("itest-rail"))
		_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
		s.Require().NoError(err)
		s.Equal(1, s.countEvolutionRails(d), "第 %d 条计划消息后应恰好注册 1 个", i+1)
	}
}

// TestHotReload_EvolutionRailInPlace 热重载不重建演进 rail，只刷新开关。
func (s *RailRegistrationSuite) TestHotReload_EvolutionRailInPlace() {
	d := s.newAdapter("agent.work.plan", evolutionExtra(true, false, false))
	found := d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{}))
	s.Require().Len(found, 1, "计划模式冷启动后应已注册演进 rail")
	before, ok := found[0].(*coreevolution.SkillEvolutionRail)
	s.Require().True(ok)
	s.False(before.AutoScan(), "初始 auto_scan 应为 false")

	s.env.WriteConfigYAML(s.T(), evolutionExtra(true, true, false))
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	found = d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillEvolutionRail{}))
	s.Require().Len(found, 1)
	after, ok := found[0].(*coreevolution.SkillEvolutionRail)
	s.Require().True(ok)
	s.Same(before, after, "热重载不得替换演进 rail 实例")
	s.True(after.AutoScan(), "auto_scan 应就地刷新为 true")
}

// TestHotReload_PreservesStatefulRails 有状态 rail 不得被热重载重建。
func (s *RailRegistrationSuite) TestHotReload_PreservesStatefulRails() {
	d := s.newAdapter("agent.work.plan", evolutionExtra(true, false, false))

	req := schema.NewAgentRequest("req-plan", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "你好", "mode": "agent.work.plan"}),
		schema.WithAgentSessionID("itest-rail"))
	_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
	s.Require().NoError(err)

	planBefore := d.Instance().FindRailsByType(reflect.TypeOf(&rails.TaskPlanningRail{}))
	s.Require().Len(planBefore, 1, "TaskPlanningRail 应已注册")

	s.env.WriteConfigYAML(s.T(), evolutionExtra(true, false, false))
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	planAfter := d.Instance().FindRailsByType(reflect.TypeOf(&rails.TaskPlanningRail{}))
	s.Require().Len(planAfter, 1)
	s.Same(planBefore[0], planAfter[0], "TaskPlanningRail 不得被重建（否则 usageRecords/todosCache 归零）")
}

// TestSkillCreate_EnabledRegistersSingleAgentRail skill_create=true 挂单 Agent 版。
func (s *RailRegistrationSuite) TestSkillCreate_EnabledRegistersSingleAgentRail() {
	d := s.newAdapter("agent.work.plan", evolutionExtra(true, false, true))
	s.Equal(1, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillCreateRail{}))),
		"应注册单 Agent SkillCreateRail")
	s.Equal(0, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.TeamSkillCreateRail{}))),
		"不应注册 TeamSkillCreateRail")
}

// TestSkillCreate_DisabledThenHotEnabled 关闭时不注册，热重载打开后生效。
func (s *RailRegistrationSuite) TestSkillCreate_DisabledThenHotEnabled() {
	d := s.newAdapter("agent.work.plan", evolutionExtra(true, false, false))
	s.Equal(0, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillCreateRail{}))),
		"skill_create=false 不应注册")

	s.env.WriteConfigYAML(s.T(), evolutionExtra(true, false, true))
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	req := schema.NewAgentRequest("req-plan", "itest-channel", schema.ReqMethodChatSend,
		s.jsonParams(map[string]any{"query": "你好", "mode": "agent.work.plan"}),
		schema.WithAgentSessionID("itest-rail"))
	_, err := d.ProcessMessageImpl(s.Ctx, req, nil)
	s.Require().NoError(err)
	s.Equal(1, len(d.Instance().FindRailsByType(reflect.TypeOf(&coreevolution.SkillCreateRail{}))),
		"热重载打开 skill_create 后应注册")
}
