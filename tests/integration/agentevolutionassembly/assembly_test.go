//go:build integration

// Package agentevolutionassembly 15.24c 审查子 agent 与 swarm 装配集成套件
// （spec docs/superpowers/specs/2026-11-26-15.24c-review-subagent-and-swarm-assembly-design.md §4）。
// 真实跨子系统链路（≥3）：harness/rails/evolution（member copy-on-write）
// → harness/rails/evolution/reviewsubagent + agent_evolving/tools（子 agent 装配与五只读工具挂接）
// → agent_evolving/checkpointing（store ForKind 落盘读回）
// → agent_evolving/experience（服务层 kind 透传）→ core/session（facade 扩面）。
package agentevolutionassembly

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	sessioniface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	reviewsubagent "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/reviewsubagent"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// EvolutionAssemblySuite 15.24c 装配集成套件。
type EvolutionAssemblySuite struct {
	suite.Suite
	ctx     context.Context
	store   *checkpointing.EvolutionStore
	manager *experience.ExperienceManager
}

func TestEvolutionAssemblySuite(t *testing.T) {
	suite.Run(t, new(EvolutionAssemblySuite))
}

func (s *EvolutionAssemblySuite) SetupTest() {
	s.ctx = context.Background()
	s.store = checkpointing.NewEvolutionStore([]string{s.T().TempDir()})
	mgr, err := experience.NewExperienceManager(s.store, nil, "skill", "cn", nil, nil, nil)
	s.Require().NoError(err)
	s.manager = mgr
}

// TestReviewSubagentAssemblyChain 审查子 agent 装配链：build config（五只读工具挂接）
// → ensure 查重 → binding 相等复用/不等 replace → remove。
// spec §4 itest-子 agent 链 + 装配链（subagent.py 1:1）。
func (s *EvolutionAssemblySuite) TestReviewSubagentAssemblyChain() {
	rt := review.NewEvolutionReviewRuntime(60)
	config, err := reviewsubagent.BuildEvolutionReviewAgentConfig(
		rt, nil, s.manager.QueryService(), s.store, "cn", 0, "agent-1")
	s.Require().NoError(err)
	s.Require().NotNil(config.AgentCard)
	s.Assert().Equal("evolution_reviewer", config.AgentCard.Name)
	s.Assert().Len(config.ToolInstances, 5, "五只读审查工具全部挂接")

	other := &harnessschema.SubAgentConfig{}
	subagents := reviewsubagent.EnsureEvolutionReviewAgentConfig(
		[]*harnessschema.SubAgentConfig{other}, config)
	s.Assert().Len(subagents, 2)
	same := reviewsubagent.EnsureEvolutionReviewAgentConfig(subagents, config)
	s.Assert().Same(&subagents[0], &same[0], "binding 相等 → 原样返回")

	rt2 := review.NewEvolutionReviewRuntime(60)
	config2, err := reviewsubagent.BuildEvolutionReviewAgentConfig(
		rt2, nil, s.manager.QueryService(), s.store, "cn", 0, "agent-1")
	s.Require().NoError(err)
	replaced := reviewsubagent.EnsureEvolutionReviewAgentConfig(subagents, config2)
	s.Assert().Same(config2, replaced[1], "binding 不等 → replace")

	removed := reviewsubagent.RemoveEvolutionReviewAgentConfig(replaced)
	s.Assert().Len(removed, 1)
	s.Assert().Same(other, removed[0])
}

// TestMemberCopyOnWriteChain member copy-on-write 链：全局库物化 → 私库隔离
// → 修改不回写全局库 → 已有真实目录保留。
// spec §4 itest-member 路径（member_skill_workspace.py 1:1）。
func (s *EvolutionAssemblySuite) TestMemberCopyOnWriteChain() {
	global := filepath.Join(s.T().TempDir(), "global")
	member := filepath.Join(s.T().TempDir(), "member")
	skillDir := filepath.Join(global, "demo-skill")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# demo"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(skillDir, "notes.txt"), []byte("body"), 0o644))

	dest, err := evolutionEnsureMemberSkillCopy(member, global, "demo-skill")
	s.Require().NoError(err)
	data, rerr := os.ReadFile(filepath.Join(dest, "notes.txt"))
	s.Require().NoError(rerr)
	s.Assert().Equal("body", string(data))

	// 私库修改不回写全局库（copy-on-write 隔离语义）
	s.Require().NoError(os.WriteFile(filepath.Join(dest, "notes.txt"), []byte("mine"), 0o644))
	after, rerr2 := os.ReadFile(filepath.Join(skillDir, "notes.txt"))
	s.Require().NoError(rerr2)
	s.Assert().Equal("body", string(after), "全局库不得被成员演进污染")

	dest2, err := evolutionEnsureMemberSkillCopy(member, global, "demo-skill")
	s.Require().NoError(err)
	s.Assert().Equal(dest, dest2, "已有真实成员目录原样保留")
}

// TestStoreForKindChain store subject_kind 落盘读回链（⑪ 销账）：
// ForKind 删除/标记幂等 + 服务层 kind 透传（submission simplify 走 ForKind）。
func (s *EvolutionAssemblySuite) TestStoreForKindChain() {
	_, err := s.store.CreateSkill(s.ctx, "kind_skill", "演示", "正文", "")
	s.Require().NoError(err)
	deleted, err := s.store.DeleteRecords(s.ctx, "kind_skill", []string{"missing"}, checkpointing.WithRecordsSubjectKind("skill"))
	s.Assert().NoError(err)
	s.Assert().Equal(0, deleted)
	marked, err := s.store.MarkRecordsApplied(s.ctx, "kind_skill", []string{"missing"}, checkpointing.WithRecordsSubjectKind("skill"))
	s.Assert().NoError(err)
	s.Assert().Equal(0, marked)
}

// TestSessionScopeFacadeChain session_scope 定位链（D-15.24b-1 销账验证）：
// Agent 会话 GetAgentID 取 card.id、GetTeamID 恒空；Team 会话反之。
func (s *EvolutionAssemblySuite) TestSessionScopeFacadeChain() {
	card := agentschema.NewAgentCard(agentschema.WithAgentID("agent-9"))
	agentSess := session.NewSession(session.WithCard(card))
	var facade sessioniface.SessionFacade = agentSess
	s.Assert().Equal("agent-9", facade.GetAgentID())
	s.Assert().Equal("", facade.GetTeamID())

	teamSess := session.NewAgentTeamSession()
	var teamFacade sessioniface.SessionFacade = teamSess
	s.Assert().Equal("", teamFacade.GetAgentID())
	s.Assert().Equal("agent_team", teamFacade.GetTeamID())
}
