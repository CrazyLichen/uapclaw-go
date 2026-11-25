package checkpointing

import (
	"context"
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
