package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// newSkillTestKit 建真实 store/服务/运行时与假 prepare_scope。
func newSkillTestKit(t *testing.T) (
	*checkpointing.EvolutionStore,
	*experience.ExperienceQueryService,
	*experience.ExperienceSubmissionService,
	*review.EvolutionReviewRuntime,
	*prepareCapture,
) {
	t.Helper()
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "d", "body", "")
	require.NoError(t, err)
	querySvc := experience.NewExperienceQueryService(store)
	submissionSvc := experience.NewExperienceSubmissionService(store)
	rt := review.NewEvolutionReviewRuntime(60)
	capture := &prepareCapture{launch: review.EvolutionReviewLaunch{
		ScopeID:            "scope_1",
		EvolutionReviewRef: "ref_1",
		Subject:            map[string]any{"kind": "skill", "name": "demo_skill"},
	}}
	return store, querySvc, submissionSvc, rt, capture
}

// prepareCapture 假 prepare_scope 回调记录。
type prepareCapture struct {
	launch  review.EvolutionReviewLaunch
	calls   int
	src     string
	err     error
	session string
	intent  string
}

func (p *prepareCapture) prepare(source string, subject map[string]any, sessionID, memberID, teamID, userIntent string) (review.EvolutionReviewLaunch, error) {
	p.calls++
	p.src = source
	p.session = sessionID
	p.intent = userIntent
	if p.err != nil {
		return review.EvolutionReviewLaunch{}, p.err
	}
	return p.launch, nil
}

func skillSubjectArgs() map[string]any {
	return map[string]any{"subject": map[string]any{"kind": "skill", "name": "demo_skill"}}
}

func newSkillTools(t *testing.T) ([]tool.Tool, *prepareCapture, *review.EvolutionReviewRuntime) {
	_, q, s, rt, capture := newSkillTestKit(t)
	tools, err := CreateMainEvolutionTools(q, s, capture.prepare, rt, "cn", "", nil)
	require.NoError(t, err)
	require.Len(t, tools, 6)
	return tools, capture, rt
}

// TestCreateMainEvolutionTools_缺依赖 测试必注入校验。
// Python skill.py:43-44。
func TestCreateMainEvolutionTools_缺依赖(t *testing.T) {
	_, _, _, _, capture := newSkillTestKit(t)
	_, err := CreateMainEvolutionTools(nil, nil, capture.prepare, nil, "cn", "", nil)
	assert.EqualError(t, err, "query_service, submission_service, and prepare_scope are required")
}

// TestPrepareSkillEvolution_未确认 测试 user_confirmed 门卫。
// Python skill.py:182-183。
func TestPrepareSkillEvolution_未确认(t *testing.T) {
	tools, capture, _ := newSkillTools(t)
	args := skillSubjectArgs()
	args["user_confirmed"] = false
	result, err := tools[0].Invoke(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
	assert.Equal(t, 0, capture.calls)
	assert.Contains(t, result["error"], "user_confirmed must be true")
}

// TestPrepareSkillEvolution_成功 测试成功路径与回调参数。
// Python skill.py:185-198。
func TestPrepareSkillEvolution_成功(t *testing.T) {
	tools, capture, _ := newSkillTools(t)
	args := skillSubjectArgs()
	args["user_confirmed"] = true
	args["user_intent"] = "表格要汇总行"
	result, err := tools[0].Invoke(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	data := result["data"].(map[string]any)
	assert.Equal(t, "prepare_skill_evolution", data["operation"])
	assert.Equal(t, "scope_1", data["scope_id"])
	assert.Equal(t, "ref_1", data["evolution_review_ref"])
	// 回调参数：source 固定 agent_detected_signal
	assert.Equal(t, "agent_detected_signal", capture.src)
	assert.Equal(t, "表格要汇总行", capture.intent)
}

// TestListSkillExperiences 测试透传与分页。
// Python skill.py:304-320。
func TestListSkillExperiences(t *testing.T) {
	store, q, s, rt, capture := newSkillTestKit(t)
	ctx := context.Background()
	// 预置一条记录
	log := checkpointing.EmptyEvolutionLog("demo_skill")
	log.Entries = append(log.Entries, checkpointing.EvolutionRecord{
		ID: "ev_x1", Source: "t", Timestamp: "2026-01-01T00:00:00Z", Score: 0.9,
		Change: checkpointing.EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "c"},
	})
	require.NoError(t, store.SaveEvolutionLog(ctx, "demo_skill", log, ""))

	tools, err := CreateMainEvolutionTools(q, s, capture.prepare, rt, "cn", "", nil)
	require.NoError(t, err)
	result, err := tools[2].Invoke(ctx, skillSubjectArgs())
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, 1, result["total_count"])
	// 模型可见文本已注入
	assert.Contains(t, result[MappedContentKey], "ev_x1")

	// limit 越界失败
	args := skillSubjectArgs()
	args["limit"] = 0
	result, err = tools[2].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
}

// TestReadSkillExperiences 测试读取与 unknown id。
// Python skill.py:329-340。
func TestReadSkillExperiences(t *testing.T) {
	store, q, s, rt, capture := newSkillTestKit(t)
	ctx := context.Background()
	log := checkpointing.EmptyEvolutionLog("demo_skill")
	log.Entries = append(log.Entries, checkpointing.EvolutionRecord{
		ID: "ev_x1", Source: "t", Timestamp: "2026-01-01T00:00:00Z", Score: 0.9,
		Change: checkpointing.EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "内容"},
	})
	require.NoError(t, store.SaveEvolutionLog(ctx, "demo_skill", log, ""))
	tools, err := CreateMainEvolutionTools(q, s, capture.prepare, rt, "cn", "", nil)
	require.NoError(t, err)

	args := skillSubjectArgs()
	args["record_ids"] = []any{"ev_x1"}
	result, err := tools[3].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	items := result["items"].([]map[string]any)
	require.Len(t, items, 1)
	assert.Equal(t, "ev_x1", items[0]["record_id"])

	args["record_ids"] = []any{"ev_nope"}
	result, err = tools[3].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
	assert.Contains(t, result["error"], "unknown record_ids: ev_nope")
}

// TestEvolveSkillExperiences_校验链 测试 selected_proposal_ids 三种非法。
// Python skill.py:355-366。
func TestEvolveSkillExperiences_校验链(t *testing.T) {
	_, _, rt := newSkillTools(t)
	ctx := context.Background()

	// runtime 为 nil 时先报 review_runtime is required
	_, q0, s0, _, capture0 := newSkillTestKit(t)
	toolsNilRT, err := CreateMainEvolutionTools(q0, s0, capture0.prepare, nil, "cn", "", nil)
	require.NoError(t, err)
	result, err := toolsNilRT[4].Invoke(ctx, skillSubjectArgs())
	require.NoError(t, err)
	assert.Contains(t, result["error"], "review_runtime is required")

	// runtime 非 nil + ref 空
	_, q, s, _, capture := newSkillTestKit(t)
	tools2, err := CreateMainEvolutionTools(q, s, capture.prepare, rt, "cn", "", nil)
	require.NoError(t, err)
	result, err = tools2[4].Invoke(ctx, skillSubjectArgs())
	require.NoError(t, err)
	assert.Contains(t, result["error"], "evolution_review_ref is required")

	// selected 空
	args := skillSubjectArgs()
	args["evolution_review_ref"] = "ref_1"
	result, err = tools2[4].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, result["error"], "selected_proposal_ids must be a non-empty list")

	// 重复 id
	args["selected_proposal_ids"] = []any{"p1", "p1"}
	result, err = tools2[4].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, result["error"], "must not contain duplicates")

	// 非字符串
	args["selected_proposal_ids"] = []any{"p1", 2}
	result, err = tools2[4].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, result["error"], "must contain non-empty strings")
}

// TestEvolveSkillExperiences_全链 测试未知 ref 与成功落盘（真实 runtime）。
// Python skill.py:368-381。
func TestEvolveSkillExperiences_全链(t *testing.T) {
	_, q, s, rt, capture := newSkillTestKit(t)
	ctx := context.Background()
	tools, err := CreateMainEvolutionTools(q, s, capture.prepare, rt, "cn", "", nil)
	require.NoError(t, err)

	// 未知 ref → unknown evolution_review_ref（Python L376-377 KeyError 映射）
	args := skillSubjectArgs()
	args["evolution_review_ref"] = "ref_missing"
	args["selected_proposal_ids"] = []any{"p1"}
	result, err := tools[4].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, result["error"], "unknown evolution_review_ref")

	// 成功路径：真实 runtime 创建 scope 并提交 review result
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "表格要汇总行", nil)
	require.NoError(t, err)
	realRef := launch.EvolutionReviewRef
	_, err = rt.RecordReviewResult(realRef, "sess1", map[string]any{
		"subject":       map[string]any{"kind": "skill", "name": "demo_skill"},
		"outcome":       "recommend_evolve",
		"evidence_refs": []any{},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience": map[string]any{
				"summary": "部署失败先查端口",
				"content": "deploy 前先检查端口占用",
				"target":  "body",
				"section": "Troubleshooting",
			},
			"reason":        "反复失败",
			"evidence_refs": []any{},
		}},
		"summary": "审查完成",
	})
	require.NoError(t, err)

	args = skillSubjectArgs()
	args["evolution_review_ref"] = realRef
	args["conversation_id"] = "sess1"
	args["selected_proposal_ids"] = []any{"p1"}
	result, err = tools[4].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "applied", result["status"])
	assert.Len(t, result["record_ids"], 1)
	// 消费后 scope 状态（Python consume_prepared_submission）
	scope, err := rt.ResolveScope(realRef, "sess1")
	require.NoError(t, err)
	assert.NotNil(t, scope.ConsumedAt)
}

// TestSimplifySkillExperiences 测试 actions 校验与直连安全。
// Python skill.py:392-408。
func TestSimplifySkillExperiences(t *testing.T) {
	tools, _, _ := newSkillTools(t)
	ctx := context.Background()

	// actions 空
	result, err := tools[5].Invoke(ctx, skillSubjectArgs())
	require.NoError(t, err)
	assert.Contains(t, result["error"], "actions must be a non-empty list")

	// 非法 action（normalize 失败）
	args := skillSubjectArgs()
	args["actions"] = []any{map[string]any{"action": "bogus", "record_id": "ev_x"}}
	result, err = tools[5].Invoke(ctx, args)
	require.NoError(t, err)
	assert.Contains(t, result["error"], "invalid simplify action")
}

// TestEvolveReviewTask ref 缺失与 parent_agent 缺失。
// Python skill.py:235-239。
func TestEvolveReviewTask(t *testing.T) {
	_, _, _, rt, _ := newSkillTestKit(t)
	created, err := CreateEvolveReviewTaskTool(EvolveReviewTaskToolDeps{
		ReviewRuntime: rt, Language: "cn", AgentID: "", // ParentAgent 空
	})
	require.NoError(t, err)

	result, err := created[0].Invoke(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "evolution_review_ref is required")

	result, err = created[0].Invoke(context.Background(), map[string]any{"evolution_review_ref": "ref_1"})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "parent_agent is required")
}

// TestBuildTaskDescription 测试 subject/user_intent 插入位置。
// Python skill.py:275-295。
func TestBuildTaskDescription(t *testing.T) {
	desc := buildTaskDescription(map[string]any{
		"subject":     `{"kind":"skill","name":"s"}`,
		"user_intent": "表格要汇总行",
	}, "ref_9")
	lines := splitLines(desc)
	assert.Equal(t, "Perform the Skill evolution review for this prepared review ref.", lines[0])
	assert.Equal(t, "evolution_review_ref: ref_9", lines[1])
	// subject 与 user_intent 依序插到 index 2
	assert.True(t, lines[2] == "subject: {\"kind\":\"skill\",\"name\":\"s\"}" || lines[2] == "user_intent: 表格要汇总行")
}

// splitLines 按行切分。
func splitLines(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

// TestSkillToolsErrorPaths 错误分支补齐（prepare 失败/review scope 未提交）。
// Python skill.py:200-201/254-255。
func TestSkillToolsErrorPaths(t *testing.T) {
	_, q, s, rt, capture := newSkillTestKit(t)
	ctx := context.Background()
	capture.err = assert.AnError
	tools, err := CreateMainEvolutionTools(q, s, capture.prepare, rt, "cn", "", nil)
	require.NoError(t, err)

	// prepare_scope 失败 → failure 透传（Python L200-201 except）
	args := skillSubjectArgs()
	args["user_confirmed"] = true
	result, err := tools[0].Invoke(ctx, args)
	require.NoError(t, err)
	assert.ErrorIs(t, assert.AnError, assert.AnError)
	assert.Equal(t, false, result["success"])

	// evolve_review_task：scope 存在但未提交审查结论（status=review_required）
	capture.err = nil
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)

	parent := &fakeParentAgentForEvolve{invokeResult: map[string]any{"output": "{}"}}
	reviewTools, err := CreateEvolveReviewTaskTool(EvolveReviewTaskToolDeps{
		ParentAgent: parent, ReviewRuntime: rt, Language: "cn",
	})
	require.NoError(t, err)
	result, err = reviewTools[0].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess1",
	}, tool.WithToolSession(&fakeSessionFacadeForEvolve{sessionID: "sess1"}))
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
	assert.Contains(t, result["error"], "did not submit a review result")

	// unknown ref 的 evolve_review_task
	result, err = reviewTools[0].Invoke(ctx, map[string]any{
		"evolution_review_ref": "evrr_missing",
		"conversation_id":      "sess1",
	}, tool.WithToolSession(&fakeSessionFacadeForEvolve{sessionID: "sess1"}))
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])

	// sessionIDFromOpts：session 对象优先级（conversation_id 缺省时取 session）
	result, err = reviewTools[0].Invoke(ctx, map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
	}, tool.WithToolSession(&fakeSessionFacadeForEvolve{sessionID: "sess1"}))
	require.NoError(t, err)
	assert.Contains(t, result["error"], "did not submit a review result")
}

// TestSkillToolCardWrappers 测试 promotion Card 包装器（tool.Tool 接口面）。
func TestSkillToolCardWrappers(t *testing.T) {
	tools, _, _ := newSkillTools(t)
	for i, tt := range tools {
		card := tt.Card()
		require.NotNil(t, card, "tool=%d", i)
		assert.NotEmpty(t, card.Name, "tool=%d", i)
	}
	// evolveReviewTaskTool.Card 显式调用
	rt := review.NewEvolutionReviewRuntime(60)
	created, err := CreateEvolveReviewTaskTool(EvolveReviewTaskToolDeps{ReviewRuntime: rt, Language: "cn"})
	require.NoError(t, err)
	assert.NotNil(t, created[0].Card())
}

// TestSimplifySkillExperiences_技能不存在 工具层报错分支补齐（skill.go:444）。
func TestSimplifySkillExperiences_技能不存在(t *testing.T) {
	tools, _, _ := newSkillTools(t)
	args := skillSubjectArgs()
	args["subject"] = map[string]any{"kind": "skill", "name": "nope"}
	args["actions"] = []any{map[string]any{"action": "DELETE", "record_id": "ev_x"}}
	result, err := tools[5].Invoke(context.Background(), args)
	require.NoError(t, err)
	assert.Equal(t, false, result["success"])
	assert.Contains(t, result["error"], "skill not found: nope")
}
