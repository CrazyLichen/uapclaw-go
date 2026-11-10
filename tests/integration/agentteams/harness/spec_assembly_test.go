//go:build integration

package harness_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpecAssemblySuite spec 驱动装配集成测试套件。
//
// 覆盖 spec §9.3 的 I-01~I-05：讨论点 C1/C2/C3/C4/C5/C8/C10 的端到端行为。
//
// 每条用例都真实调用 ≥3 个 internal 二级子系统的导出函数（规则 7）：
// agentcore/harness/schema + agent_teams/rails + agent_teams/harness
// （I-02 另加 agentcore/session）。
type SpecAssemblySuite struct {
	isuite.AgentSuite
}

// codeLikeContext 模拟上游 CodeBuildContext 的子类（多一个 Adapter 字段）。
type codeLikeContext struct {
	hschema.BuildContext
	// Adapter 子类专有字段
	Adapter string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSpecAssemblySuite 套件入口。
func TestSpecAssemblySuite(t *testing.T) {
	suite.Run(t, new(SpecAssemblySuite))
}

// SetupTest 每个用例前重置注册表，避免用例间污染。
//
// AgentSuite 没有 SetupTest（只在 SetupSuite/TearDownTest 里做套件级准备），
// 故这里只做 rail 注册表重置。
func (s *SpecAssemblySuite) SetupTest() {
	hschema.ResetRailRegistryForTest()
	rails.ResetRegistrationForTest()
	rails.RegisterTeamRailProviders()
}

// TearDownTest 每个用例后重置。
func (s *SpecAssemblySuite) TearDownTest() {
	s.AgentSuite.TearDownTest()
	hschema.ResetRailRegistryForTest()
	rails.ResetRegistrationForTest()
}

// Derive 覆写以保留子类类型（D2：Derive 必须返回子类而非基类）。
func (c *codeLikeContext) Derive(o hschema.BuildContextOverrides) hschema.BuildContextInterface {
	base, ok := c.BuildContext.Derive(o).(*hschema.BuildContext)
	if !ok {
		return &codeLikeContext{BuildContext: c.BuildContext, Adapter: c.Adapter}
	}
	return &codeLikeContext{BuildContext: *base, Adapter: c.Adapter}
}

// TestI01_BuildContext到成员装配 17 键句柄真实到达 rail。
func (s *SpecAssemblySuite) TestI01_BuildContext到成员装配() {
	// 1) agentcore/harness/schema：造 BuildContext
	buildCtx := &hschema.BuildContext{
		Language:   "cn",
		MemberName: "alice",
		Role:       "teammate",
	}
	s.Require().NotNil(buildCtx)

	// 2) agent_teams/rails：注入 17 键
	rails.InjectTeamHandles(buildCtx, rails.TeamHandles{})
	s.Require().Len(buildCtx.GetExtras(), 17)

	// 3) agent_teams/harness：用该 ctx 装配
	spec := atschema.NewDeepAgentSpec()
	spec.Language = "cn"
	spec.Rails = []hschema.RailSpec{{Type: rails.TeamPolicy, Params: map[string]any{
		"member_name": "alice",
		"language":    "cn",
	}}}

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, buildCtx, nil)
	s.Require().NotNil(h)

	// 4) 断言行为：policy rail 真的挂上了（不是只传了参数）
	found := h.FindRailsByType(&rails.TeamPolicyRail{})
	s.NotEmpty(found, "policy rail 应已挂载")
}

// TestI02_ResolveParts端到端 spec 解析后物化的 agent 可跑一轮。
func (s *SpecAssemblySuite) TestI02_ResolveParts端到端() {
	spec, err := s.NewSpecWithMockLLM(3)
	s.Require().NoError(err)
	spec.Language = "cn"

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h)
	s.Require().NotNil(h.Native())

	// 真实跑一轮（MockLLM 由 AgentSuite 预填）
	sess := session.CreateAgentSession("", h.Native().DeepAgent().Card(), nil)
	result, runErr := h.Native().RunOnce(s.Ctx, "说一句话", sess)

	s.NoError(runErr, "解析后物化的 agent 应可跑一轮")
	s.NotNil(result)
}

// TestI02b_模板非nil返回哨兵 AgentTemplateSpec 非 nil 时 RunOnce 返回哨兵。
func (s *SpecAssemblySuite) TestI02b_模板非nil返回哨兵() {
	spec := atschema.NewDeepAgentSpec()
	spec.AgentTemplateSpec = map[string]any{"root": "template"}

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h)
	s.Require().NotNil(h.Native())

	sess := session.CreateAgentSession("", h.Native().DeepAgent().Card(), nil)
	_, err := h.Native().RunOnce(s.Ctx, "hi", sess)

	s.ErrorIs(err, teamharness.ErrTemplateUnsupported, "⤵️ 15.7-15.9 前应返回哨兵")
}

// TestI03_注册表驱动挂载 移除注册后 rail 不再挂载（证明注册表是唯一入口）。
func (s *SpecAssemblySuite) TestI03_注册表驱动挂载() {
	spec := atschema.NewDeepAgentSpec()
	spec.Language = "cn"
	spec.Rails = []hschema.RailSpec{{Type: rails.TeamPolicy, Params: map[string]any{
		"member_name": "alice",
	}}}

	// 先确认正常情况能挂上
	h1 := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h1)
	s.NotEmpty(h1.FindRailsByType(&rails.TeamPolicyRail{}))

	// 从注册表移除后，同一 spec 不再挂载
	hschema.ResetRailRegistryForTest()
	spec2 := atschema.NewDeepAgentSpec()
	spec2.Language = "cn"
	spec2.Rails = []hschema.RailSpec{{Type: rails.TeamPolicy, Params: map[string]any{}}}

	h2 := teamharness.TeamHarnessBuild(spec2, "teammate", "bob", false, nil, nil)
	s.Require().NotNil(h2)
	s.Empty(h2.FindRailsByType(&rails.TeamPolicyRail{}), "注册表移除后不得再挂载")
}

// TestI04_子类BuildContext贯通 子类专有字段经解析后仍可取（D2 端到端）。
func (s *SpecAssemblySuite) TestI04_子类BuildContext贯通() {
	// 模拟上游 CodeBuildContext：子类多一个 Adapter 字段
	sub := &codeLikeContext{
		BuildContext: hschema.BuildContext{Language: "cn", MemberName: "alice"},
		Adapter:      "code-adapter-1",
	}

	// 注册一个 provider，断言它能拿到子类特有字段
	var seenAdapter string
	hschema.RegisterRailProvider("test.subclass.rail", func(
		_ map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		if c, ok := ctx.(*codeLikeContext); ok {
			seenAdapter = c.Adapter
		}
		return nil, nil
	})

	spec := atschema.NewDeepAgentSpec()
	spec.Language = "cn"
	spec.Rails = []hschema.RailSpec{{Type: "test.subclass.rail"}}

	teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, sub, nil)

	s.Equal("code-adapter-1", seenAdapter, "子类专有字段必须经 Derive 保留下来")
}

// TestI05_构造输入双源 只改 params 源字段，context 源字段仍正确。
func (s *SpecAssemblySuite) TestI05_构造输入双源() {
	buildCtx := &hschema.BuildContext{Language: "en", MemberName: "alice", Role: "teammate"}

	spec := atschema.NewDeepAgentSpec()
	spec.Language = "en"
	// 只声明 params 源字段，context 源字段靠 ctx 提供
	spec.Rails = []hschema.RailSpec{{Type: rails.TeamPolicy, Params: map[string]any{
		"lifecycle": "persistent",
	}}}

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, buildCtx, nil)
	s.Require().NotNil(h)

	found := h.FindRailsByType(&rails.TeamPolicyRail{})
	s.NotEmpty(found, "params 与 context 双源都应参与构造")
}

// TestI08_占位rail不阻断构建 5 真 + 3 占位同时声明，构建成功且成员可跑一轮。
func (s *SpecAssemblySuite) TestI08_占位rail不阻断构建() {
	spec, err := s.NewSpecWithMockLLM(2)
	s.Require().NoError(err)
	spec.Language = "cn"
	spec.Rails = []hschema.RailSpec{
		{Type: rails.TeamPolicy, Params: map[string]any{"member_name": "alice"}},
		// 三个未实现的占位
		{Type: rails.TeamReliability},
		{Type: rails.TeamSkillUse},
		{Type: rails.TeamObservability},
	}

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, nil, nil)
	s.Require().NotNil(h, "占位 rail 不得让构建失败")
	s.Require().NotNil(h.Native())

	// 成员仍可正常跑一轮
	sess := session.CreateAgentSession("", h.Native().DeepAgent().Card(), nil)
	_, runErr := h.Native().RunOnce(s.Ctx, "占位场景", sess)

	s.NoError(runErr, "占位 rail 不得毒化整条链路")
}

// TestI10_无Rails访问器 FindRails 仍可查到已挂载 rail（删除 Rails() 后功能未丢）。
func (s *SpecAssemblySuite) TestI10_无Rails访问器() {
	buildCtx := &hschema.BuildContext{Language: "cn", MemberName: "alice", Role: "teammate"}
	rails.InjectTeamHandles(buildCtx, rails.TeamHandles{})

	spec := atschema.NewDeepAgentSpec()
	spec.Language = "cn"
	spec.Rails = []hschema.RailSpec{{Type: rails.TeamPolicy, Params: map[string]any{
		"member_name": "alice",
	}}}

	h := teamharness.TeamHarnessBuild(spec, "teammate", "alice", false, buildCtx, nil)
	s.Require().NotNil(h)

	// 替代查询路径：FindRails（旧 Rails() 已删）
	found := h.FindRailsByType(&rails.TeamPolicyRail{})
	s.NotEmpty(found, "删除 Rails() 后 FindRails 仍应是受支持的查询路径")
}
