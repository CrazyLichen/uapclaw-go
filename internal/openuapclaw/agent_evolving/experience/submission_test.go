package experience

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
)

// newSubmissionFixture 建临时 store + 服务 + 预置技能与一条已落盘记录。
func newSubmissionFixture(t *testing.T) (*checkpointing.EvolutionStore, *ExperienceSubmissionService) {
	t.Helper()
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "desc", "body", "")
	require.NoError(t, err)
	return store, NewExperienceSubmissionService(store)
}

func submissionSubject() map[string]any {
	return map[string]any{"kind": "skill", "name": "demo_skill"}
}

func submissionDrafts() []any {
	return []any{map[string]any{
		"summary": "部署失败先查端口",
		"content": "deploy 前先检查端口占用",
		"target":  "body",
		"section": "Troubleshooting",
	}}
}

// fakeResolver 预检解析协作面桩（对应 review.EvolutionProposalSelection 的适配产物）。
type fakeResolver struct {
	calls    int
	prepared *PreparedEvolveSubmission
	err      error
}

func (f *fakeResolver) ResolveSelectedProposals(
	_ string, _ map[string]any, _ []string, _ string,
) (*PreparedEvolveSubmission, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.prepared, nil
}

// TestApplyExperienceDrafts_成功 测试直接路径成功落盘。
// Python submission.py:43-82。
func TestApplyExperienceDrafts_成功(t *testing.T) {
	store, svc := newSubmissionFixture(t)
	ctx := context.Background()

	result, err := svc.ApplyExperienceDrafts(ctx, submissionSubject(), "", submissionDrafts(), "agent_evolve_tool")
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "evolve", result["operation"])
	assert.Equal(t, "applied", result["status"])
	assert.Equal(t, 1, result["applied_count"])
	assert.Equal(t, 0, result["pending_count"])
	assert.Nil(t, result["retry_request_id"])
	assert.Empty(t, result["errors"])

	// 真实落盘断言：store 读回记录
	log, err := store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	require.Len(t, log.Entries, 1)
	assert.Equal(t, "deploy 前先检查端口占用", log.Entries[0].Change.Content)
	assert.Equal(t, "agent_evolve_tool", log.Entries[0].Source)
}

// TestApplyExperienceDrafts_技能不存在 测试校验前置。
// Python submission.py:118-119。
func TestApplyExperienceDrafts_技能不存在(t *testing.T) {
	_, svc := newSubmissionFixture(t)
	_, err := svc.ApplyExperienceDrafts(
		context.Background(),
		map[string]any{"kind": "skill", "name": "nope"}, "", submissionDrafts(), "agent_evolve_tool")
	require.Error(t, err)
	assert.Equal(t, "skill not found: nope", err.Error())
}

// TestPrepareEvolveSubmission_不消费Ref 测试预检不消费引用。
// Python submission.py:143-160（"without consuming the ref"）。
func TestPrepareEvolveSubmission_不消费Ref(t *testing.T) {
	_, svc := newSubmissionFixture(t)
	resolver := &fakeResolver{prepared: &PreparedEvolveSubmission{
		Subject:          submissionSubject(),
		ExperienceDrafts: []map[string]any{submissionDrafts()[0].(map[string]any)},
		RecordSource:     "user_intent",
	}}

	// 同一 ref 可重复 prepare（不消费）
	for i := 0; i < 2; i++ {
		prepared, err := svc.PrepareEvolveSubmission(resolver, "ref_1", submissionSubject(), []string{"p1"}, "sess")
		require.NoError(t, err)
		assert.Equal(t, "user_intent", prepared.RecordSource)
	}
	assert.Equal(t, 2, resolver.calls)
}

// TestPrepareEvolveSubmission_解析失败透传 测试 resolver 错误透传。
func TestPrepareEvolveSubmission_解析失败透传(t *testing.T) {
	_, svc := newSubmissionFixture(t)
	resolver := &fakeResolver{err: assert.AnError}
	_, err := svc.PrepareEvolveSubmission(resolver, "ref_x", submissionSubject(), []string{"p1"}, "sess")
	assert.ErrorIs(t, err, assert.AnError)
}

// TestApplyPreparedEvolveSubmission_RecordSource覆写 测试来源覆写语义。
// Python submission.py:172（record_source = getattr(prepared, ..., None) or source）。
func TestApplyPreparedEvolveSubmission_RecordSource覆写(t *testing.T) {
	store, svc := newSubmissionFixture(t)
	ctx := context.Background()

	// RecordSource 非空 → 覆写 source
	prepared := &PreparedEvolveSubmission{
		Subject:          submissionSubject(),
		ExperienceDrafts: []map[string]any{submissionDrafts()[0].(map[string]any)},
		RecordSource:     "user_intent",
	}
	_, err := svc.ApplyPreparedEvolveSubmission(ctx, prepared, "agent_evolve_tool")
	require.NoError(t, err)
	log, err := store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	assert.Equal(t, "user_intent", log.Entries[0].Source)

	// RecordSource 空 → 回落 source
	prepared2 := &PreparedEvolveSubmission{
		Subject:          submissionSubject(),
		ExperienceDrafts: []map[string]any{submissionDrafts()[0].(map[string]any)},
	}
	_, err = svc.ApplyPreparedEvolveSubmission(ctx, prepared2, "agent_evolve_tool")
	require.NoError(t, err)
	log, err = store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	assert.Equal(t, "agent_evolve_tool", log.Entries[len(log.Entries)-1].Source)
}

// TestApplySimplifyActions 测试精简动作执行。
// Python submission.py:84-108。
func TestApplySimplifyActions(t *testing.T) {
	store, svc := newSubmissionFixture(t)
	ctx := context.Background()

	// 先落一条记录供 refine
	_, err := svc.ApplyExperienceDrafts(ctx, submissionSubject(), "", submissionDrafts(), "agent_evolve_tool")
	require.NoError(t, err)
	log, err := store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	require.Len(t, log.Entries, 1)
	recordID := log.Entries[0].ID

	// refine 已有记录
	actions := []any{map[string]any{
		"action":      "refine",
		"record_id":   recordID,
		"new_content": "deploy 前先检查端口占用与防火墙",
	}}
	result, err := svc.ApplySimplifyActions(ctx, submissionSubject(), "", actions)
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "simplify", result["operation"])
	assert.Equal(t, "applied", result["status"])
	counts := result["action_counts"].(map[string]int)
	assert.Equal(t, 1, counts["refined"])
	assert.Equal(t, 0, counts["errors"])

	// 引用不存在的 record_id → 校验失败（ValidateSimplifyRecordRefs）
	badActions := []any{map[string]any{"action": "refine", "record_id": "ev_nope", "new_content": "x"}}
	_, err = svc.ApplySimplifyActions(ctx, submissionSubject(), "", badActions)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "ev_nope") || strings.Contains(err.Error(), "record"))
}

// TestValidateSimplifyActions_技能不存在 测试校验前置。
func TestValidateSimplifyActions_技能不存在(t *testing.T) {
	_, svc := newSubmissionFixture(t)
	err := svc.PrepareSimplifySubmission(
		context.Background(),
		map[string]any{"kind": "skill", "name": "nope"}, "", []any{map[string]any{}})
	require.Error(t, err)
	assert.Equal(t, "skill not found: nope", err.Error())
}

// TestBindPendingApprovalSnapshots 测试快照表绑定。
// Python submission.py:39-41。
func TestBindPendingApprovalSnapshots(t *testing.T) {
	_, svc := newSubmissionFixture(t)
	external := map[string]*checkpointing.PendingChange{}
	svc.BindPendingApprovalSnapshots(external)
	svc.mu.Lock()
	assert.Equal(t, external, svc.pendingApprovalSnapshots)
	svc.mu.Unlock()
	// nil → 重建空表
	svc.BindPendingApprovalSnapshots(nil)
	svc.mu.Lock()
	assert.NotNil(t, svc.pendingApprovalSnapshots)
	svc.mu.Unlock()
}

// TestApplySimplifyActions_Merge与Delete 补齐 MERGE/DELETE 分支计数。
// Python submission.py:99（counts 汇总 deleted+merged+refined）。
func TestApplySimplifyActions_Merge与Delete(t *testing.T) {
	store, svc := newSubmissionFixture(t)
	ctx := context.Background()
	_, err := svc.ApplyExperienceDrafts(ctx, submissionSubject(), "", []any{
		map[string]any{"summary": "经验一", "content": "内容一", "target": "body", "section": "Troubleshooting"},
		map[string]any{"summary": "经验二", "content": "内容二", "target": "body", "section": "Troubleshooting"},
	}, "agent_evolve_tool")
	require.NoError(t, err)
	log, err := store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	require.Len(t, log.Entries, 2)
	keepID, removeID := log.Entries[0].ID, log.Entries[1].ID

	// MERGE：keep 主体 + remove 另一条
	result, err := svc.ApplySimplifyActions(ctx, submissionSubject(), "", []any{map[string]any{
		"action": "MERGE", "record_id": keepID,
		"merge_remove_ids": []any{removeID},
		"new_content":      "合并后的内容",
	}})
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	counts := result["action_counts"].(map[string]int)
	assert.Equal(t, 1, counts["merged"])
	assert.Equal(t, 0, counts["errors"])

	// DELETE 剩余记录
	log2, err := store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	remaining := log2.Entries[0].ID
	result, err = svc.ApplySimplifyActions(ctx, submissionSubject(), "", []any{map[string]any{
		"action": "DELETE", "record_id": remaining,
	}})
	require.NoError(t, err)
	counts = result["action_counts"].(map[string]int)
	assert.Equal(t, 1, counts["deleted"])

	// 全部删完后再校验引用 → not found
	err = svc.PrepareSimplifySubmission(ctx, submissionSubject(), "", []any{map[string]any{
		"action": "DELETE", "record_id": remaining,
	}})
	require.Error(t, err)
}

// TestSubmissionValidateErrorPaths 校验错误分支补齐。
// Python submission.py:110-137 各 return err。
func TestSubmissionValidateErrorPaths(t *testing.T) {
	store, svc := newSubmissionFixture(t)
	ctx := context.Background()

	// 非法 subject（缺 kind）→ NormalizeSubject 失败
	badSubject := map[string]any{"name": "demo_skill"}
	_, err := svc.ApplyExperienceDrafts(ctx, badSubject, "", submissionDrafts(), "agent_evolve_tool")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subject")

	// ApplySimplifyActions 技能不存在（L133 return）
	_, err = svc.ApplySimplifyActions(ctx, map[string]any{"kind": "skill", "name": "nope"}, "", []any{map[string]any{"action": "DELETE", "record_id": "ev_x"}})
	require.Error(t, err)

	// ValidateSimplifyActions 非法动作（NormalizeSimplifyDraft 失败）
	err = svc.PrepareSimplifySubmission(ctx, submissionSubject(), "", []any{map[string]any{"action": "bogus", "record_id": "ev_x"}})
	require.Error(t, err)

	// ValidateExperienceDrafts 草稿为空
	_, _, err = svc.ValidateExperienceDrafts(ctx, submissionSubject(), nil)
	require.Error(t, err)

	// store 引用存在性：PrepareSimplify 成功路径
	_, err = svc.ApplyExperienceDrafts(ctx, submissionSubject(), "", submissionDrafts(), "agent_evolve_tool")
	require.NoError(t, err)
	log, err := store.LoadFullEvolutionLog(ctx, "demo_skill")
	require.NoError(t, err)
	require.NotEmpty(t, log.Entries)
	err = svc.PrepareSimplifySubmission(ctx, submissionSubject(), "", []any{map[string]any{
		"action": "DELETE", "record_id": log.Entries[0].ID,
	}})
	assert.NoError(t, err)
}
