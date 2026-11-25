package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// TestExperienceSubmissionServiceAdapter_Preflight 适配器 preflight 真链路。
// 15.21 接缝接线验证（Python approval_submission_service 语义）。
func TestExperienceSubmissionServiceAdapter_Preflight(t *testing.T) {
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "d", "body", "")
	require.NoError(t, err)
	log := checkpointing.EmptyEvolutionLog("demo_skill")
	record := checkpointing.EvolutionRecord{ID: "ev_a1", Source: "t", Score: 0.6}
	record.Change = checkpointing.EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "c"}
	log.Entries = append(log.Entries, record)
	require.NoError(t, store.SaveEvolutionLog(ctx, "demo_skill", log, ""))

	rt := review.NewEvolutionReviewRuntime(60)
	subject := map[string]any{"kind": "skill", "name": "demo_skill"}
	launch, err := rt.CreateScope("agent_detected_signal", subject, "sess1", "", nil)
	require.NoError(t, err)
	_, err = rt.RecordReviewResult(launch.EvolutionReviewRef, "sess1", map[string]any{
		"subject": subject, "outcome": "recommend_evolve", "evidence_refs": []any{},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience":  map[string]any{"summary": "s", "content": "c", "target": "body", "section": "Troubleshooting"},
			"reason":      "r", "evidence_refs": []any{},
		}},
		"summary": "done",
	})
	require.NoError(t, err)

	adapter := NewExperienceSubmissionServiceAdapter(store, map[string]*checkpointing.PendingChange{})

	// evolve preflight 成功（真实 runtime + 校验）
	selection, err := adapter.PrepareEvolveSubmission(rt, launch.EvolutionReviewRef, subject, []string{"p1"}, "sess1")
	require.NoError(t, err)
	require.NotNil(t, selection)
	assert.NotEmpty(t, selection.ExperienceDrafts)

	// unknown ref → 报错
	_, err = adapter.PrepareEvolveSubmission(rt, "evrr_missing", subject, []string{"p1"}, "sess1")
	require.Error(t, err)

	// simplify preflight 成功（引用已落盘 record）
	err = adapter.PrepareSimplifySubmission(subject, []map[string]any{{
		"action": "REFINE", "record_id": "ev_a1", "new_content": "新内容",
	}})
	assert.NoError(t, err)

	// simplify preflight 引用不存在 → 报错
	err = adapter.PrepareSimplifySubmission(subject, []map[string]any{{
		"action": "REFINE", "record_id": "ev_nope", "new_content": "x",
	}})
	require.Error(t, err)

	// 提交服务侧：ApplyExperienceDrafts 落盘链（bindings 共享 pending 表）
	adapter2 := experience.NewExperienceSubmissionService(store)
	_, err = adapter2.ApplyExperienceDrafts(ctx, subject, []any{map[string]any{
		"summary": "部署失败先查端口占用",
		"content": "deploy 前先检查端口占用",
		"target":  "body",
		"section": "Troubleshooting",
	}}, "agent_evolve_tool")
	require.NoError(t, err)
}
