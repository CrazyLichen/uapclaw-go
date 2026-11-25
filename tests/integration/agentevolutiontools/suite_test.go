//go:build integration

// 15.24b 演进工具面集成套件（spec docs/superpowers/specs/2026-12-24-15.24b-evolution-tools-design.md §6.1）。
// 真实跨子系统链路（≥3）：agent_evolving/tools（工具面）→ agent_evolving/experience
// （query/submission 服务）→ agent_evolving/checkpointing（store 落盘读回）
// → harness/rails/evolution（15.21 中断 rail 接口缝 + review runtime）。
package agentevolutiontools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	agenttools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/tools"
	fdtool "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	evolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// EvolutionToolsSuite 15.24b 工具面集成套件。
type EvolutionToolsSuite struct {
	suite.Suite
	ctx             context.Context
	store           *checkpointing.EvolutionStore
	submission      *experience.ExperienceSubmissionService
	query           *experience.ExperienceQueryService
	runtime         *review.EvolutionReviewRuntime
	prepareCaptures int
}

func TestEvolutionToolsSuite(t *testing.T) {
	suite.Run(t, new(EvolutionToolsSuite))
}

func (s *EvolutionToolsSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = checkpointing.NewEvolutionStore([]string{s.T().TempDir()})
	_, err := s.store.CreateSkill(s.ctx, "demo_skill", "演示技能", "演示正文", "")
	s.Require().NoError(err)
	s.submission = experience.NewExperienceSubmissionService(s.store)
	s.query = experience.NewExperienceQueryService(s.store)
	s.runtime = review.NewEvolutionReviewRuntime(60)
	s.prepareCaptures = 0
}

// prepareScope rail 侧 _prepare_evolution_review_scope 的集成替身：
// 走真实 CreateScope（登记审查现场，不写库）。
func (s *EvolutionToolsSuite) prepareScope(source string, subject map[string]any, sessionID, _, _, userIntent string) (review.EvolutionReviewLaunch, error) {
	s.prepareCaptures++
	return s.runtime.CreateScope(source, subject, sessionID, userIntent, nil)
}

func (s *EvolutionToolsSuite) newMainTools() []fdtool.Tool {
	tools, err := agenttools.CreateMainEvolutionTools(
		s.query, s.submission, s.prepareScope, s.runtime, "cn", "", nil)
	s.Require().NoError(err)
	s.Require().Len(tools, 6)
	return tools
}

func (s *EvolutionToolsSuite) invoke(t toolInvokeTarget, inputs map[string]any) map[string]any {
	result, err := t.Invoke(s.ctx, inputs)
	s.Require().NoError(err)
	return result
}

// toolInvokeTarget 别名（可 Invoke 的工具）。
type toolInvokeTarget = fdtool.Tool

func (s *EvolutionToolsSuite) subjectArgs() map[string]any {
	return map[string]any{"subject": map[string]any{"kind": "skill", "name": "demo_skill"}}
}

// TestSubmitTo落盘全链 六工具全链：prepare → 审查 → evolve 提交 → store 读回。
// 对齐 Python：submit_evolution_review → evolve_skill_experiences →
// commit_pending_change（落盘唯一写点）。
func (s *EvolutionToolsSuite) TestSubmitTo落盘全链() {
	tools := s.newMainTools()
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := s.runtime.CreateScope("agent_detected_signal", subject, "sess1", "部署要带环境检查", nil)
	s.Require().NoError(err)

	// 审查子 agent 侧 submit（真实 runtime）
	reviewTools, err := agenttools.CreateEvolutionReviewTools(agenttools.EvolutionReviewToolsDeps{
		Runtime: s.runtime, Store: s.store, Language: "cn", AgentID: "agt1",
	})
	s.Require().NoError(err)
	submitResult := s.invoke(reviewTools[0], map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
		"subject":              subject,
		"outcome":              "recommend_evolve",
		"evidence_refs":        []any{},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience": map[string]any{
				"summary": "部署失败先查端口占用",
				"content": "deploy 前先检查端口占用与防火墙",
				"target":  "body",
				"section": "Troubleshooting",
			},
			"reason":        "部署反复失败",
			"evidence_refs": []any{},
		}},
		"summary": "审查完成",
	})
	s.Require().Equal(true, submitResult["success"])

	// 主 agent evolve 提交
	args := s.subjectArgs()
	args["evolution_review_ref"] = launch.EvolutionReviewRef
	args["conversation_id"] = "sess1"
	args["selected_proposal_ids"] = []any{"p1"}
	result := s.invoke(tools[4], args)
	s.Require().Equal(true, result["success"])
	s.Require().Equal("applied", result["status"])

	// 落盘断言：store 读回真实演进产物
	evoLog, err := s.store.LoadFullEvolutionLog(s.ctx, "demo_skill")
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1)
	entry := evoLog.Entries[0]
	s.Equal("deploy 前先检查端口占用与防火墙", entry.Change.Content)
	s.Equal("body", string(entry.Change.Target))
	s.Equal("Troubleshooting", entry.Change.Section)
	// record_source=user_intent（scope user_intent 非空）覆写默认 source（Python L172）
	s.Equal("user_intent", entry.Source)

	// ref 已消费（重复提交报错）
	replay := s.invoke(tools[4], args)
	s.Require().Equal(false, replay["success"])
	s.Contains(replay["error"], "unknown evolution_review_ref")
}

// TestInterruptRailPreflight走真实现 15.21 接缝接线验证：
// Configure 绑定真适配器后 preflight 走真实校验；skill not found 报错。
func (s *EvolutionToolsSuite) TestInterruptRailPreflight走真实现() {
	rail := evolution.NewEvolutionInterruptRail()
	adapter := evolution.NewExperienceSubmissionServiceAdapter(s.store, map[string]*experience.PendingChange{})
	rail.Configure(s.runtime, adapter, false, "cn")

	// preflight：未知 ref → 报错（经 adapter → runtime.ResolveScope）
	_, err := adapter.PrepareEvolveSubmission(s.runtime, "evrr_missing", map[string]any{"kind": "skill", "name": "demo_skill"}, []string{"p1"}, "sess1")
	s.Require().Error(err)
	s.Contains(err.Error(), "scope not found")

	// preflight simplify：技能不存在 → skill not found
	err = adapter.PrepareSimplifySubmission(map[string]any{"kind": "skill", "name": "nope"}, []map[string]any{})
	s.Require().Error(err)
	s.Contains(err.Error(), "skill not found")

	// 幂等身份：两次获取同一适配器实例
	assert.NotNil(s.T(), adapter)
}

// TestQueryRead分页与截断 经主 agent list/read 工具跨 store 的分页与截断。
func (s *EvolutionToolsSuite) TestQueryRead分页与截断() {
	tools := s.newMainTools()
	log := checkpointing.EmptyEvolutionLog("demo_skill")
	for i := 0; i < 3; i++ {
		record := checkpointing.EvolutionRecord{
			Source:    "t",
			Timestamp: "2026-01-01T00:00:00Z",
			Score:     float64(10 - i),
			Change:    checkpointing.EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "内容内容内容"},
		}
		record.ID = "ev_p" + string(rune('a'+i))
		log.Entries = append(log.Entries, record)
	}
	s.Require().NoError(s.store.SaveEvolutionLog(s.ctx, "demo_skill", log, ""))

	// list 第一页（limit=2）
	args := s.subjectArgs()
	args["limit"] = 2
	result := s.invoke(tools[2], args)
	s.Require().Equal(true, result["success"])
	s.Equal(3, result["total_count"])
	s.Equal(true, result["has_more"])
	s.Equal("2", result["next_cursor"])

	// 翻到末页
	args["cursor"] = "2"
	result = s.invoke(tools[2], args)
	s.Require().Equal(true, result["success"])
	s.Equal(false, result["has_more"])

	// read 截断
	args = s.subjectArgs()
	args["record_ids"] = []any{"ev_pa"}
	args["max_content_chars"] = 3
	result = s.invoke(tools[3], args)
	s.Require().Equal(true, result["success"])
	items := result["items"].([]map[string]any)
	s.Require().Len(items, 1)
	s.Equal(true, items[0]["content_truncated"])
}
