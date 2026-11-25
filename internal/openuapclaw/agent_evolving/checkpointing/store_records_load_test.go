package checkpointing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadRecordsByIds 按 id 批量加载并保持调用方顺序。
// Python: StoreRecordsHelper.load_records_by_ids (store_records.py:312-324)。
func TestLoadRecordsByIds(t *testing.T) {
	store := NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "d", "body", "")
	require.NoError(t, err)

	log := EmptyEvolutionLog("demo_skill")
	for _, id := range []string{"ev_a", "ev_b", "ev_c"} {
		r := EvolutionRecord{ID: id, Source: "t", Score: 0.6}
		r.Change = EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "c-" + id}
		log.Entries = append(log.Entries, r)
	}
	require.NoError(t, store.SaveEvolutionLog(ctx, "demo_skill", log, ""))

	// 保序读取
	got := store.LoadRecordsByIds(ctx, "demo_skill", []string{"ev_c", "ev_a"})
	require.Len(t, got, 2)
	assert.Equal(t, "ev_c", got[0].ID)
	assert.Equal(t, "ev_a", got[1].ID)

	// 空 id 列表 → 空结果（Python L320-321）
	assert.Empty(t, store.LoadRecordsByIds(ctx, "demo_skill", nil))

	// 缺失 id 静默跳过（unknown 检测在上层）
	got = store.LoadRecordsByIds(ctx, "demo_skill", []string{"ev_a", "ev_missing", "ev_b"})
	require.Len(t, got, 2)

	// 全部未知 → 空
	assert.Empty(t, store.LoadRecordsByIds(ctx, "demo_skill", []string{"ev_nope"}))
}

// TestLoadRecordsByIds_技能不存在 测试加载失败回落空结果。
// Python store_records.py:322 load_full_evolution_log 抛错由上层处理；
// Go 侧按既有 GetRecordsByScore 同款回落语义（Warn + 空结果）。
func TestLoadRecordsByIds_技能不存在(t *testing.T) {
	store := NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	assert.Empty(t, store.LoadRecordsByIds(ctx, "nope_skill", []string{"ev_a"}))
}

// TestSubjectKindFilter 测试 store 四方法 subject_kind 过滤链。
// Python: skill_exists/resolve_skill_dir kind 分支（evolution_store.py:253-279）。
func TestSubjectKindFilter(t *testing.T) {
	store := NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()

	// skill 类：frontmatter 无 kind（缺省 skill）
	_, err := store.CreateSkill(ctx, "plain_skill", "d", "body", "")
	require.NoError(t, err)

	// swarm-skill 类：手写目录 + frontmatter kind
	swarmDir := filepath.Join(store.BaseDirs()[0], "swarm_skill")
	require.NoError(t, os.MkdirAll(swarmDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(swarmDir, "SKILL.md"),
		[]byte("---\nkind: swarm-skill\nname: swarm_skill\n---\nbody"), 0644))

	// SkillExistsForKind：默认不过滤两个都存在
	assert.True(t, store.SkillExistsForKind(ctx, "plain_skill", ""))
	assert.True(t, store.SkillExistsForKind(ctx, "swarm_skill", ""))
	// kind 过滤
	assert.True(t, store.SkillExistsForKind(ctx, "plain_skill", "skill"))
	assert.False(t, store.SkillExistsForKind(ctx, "plain_skill", "swarm-skill"))
	assert.True(t, store.SkillExistsForKind(ctx, "swarm_skill", "swarm-skill"))
	assert.False(t, store.SkillExistsForKind(ctx, "swarm_skill", "skill"))
	// 别名归一：team-skill → swarm-skill
	assert.True(t, store.SkillExistsForKind(ctx, "swarm_skill", "team-skill"))

	// LoadRecordsByIdsForKind：kind 不匹配 → 空结果
	log := EmptyEvolutionLog("plain_skill")
	r := EvolutionRecord{ID: "ev_k1", Source: "t", Score: 0.6}
	r.Change = EvolutionPatch{Target: "body", Section: "Troubleshooting", Action: "append", Content: "c"}
	log.Entries = append(log.Entries, r)
	require.NoError(t, store.SaveEvolutionLog(ctx, "plain_skill", log, ""))
	assert.Len(t, store.LoadRecordsByIdsForKind(ctx, "plain_skill", []string{"ev_k1"}, "skill"), 1)
	assert.Empty(t, store.LoadRecordsByIdsForKind(ctx, "plain_skill", []string{"ev_k1"}, "swarm-skill"))

	// GetRecordsByScoreForKind / LoadFullEvolutionLogForKind 同链
	assert.Len(t, store.GetRecordsByScoreForKind(ctx, "plain_skill", nil, "skill"), 1)
	assert.Empty(t, store.GetRecordsByScoreForKind(ctx, "plain_skill", nil, "swarm-skill"))
	_, err = store.LoadFullEvolutionLogForKind(ctx, "plain_skill", "skill")
	assert.NoError(t, err)
}
