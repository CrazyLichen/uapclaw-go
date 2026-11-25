package experience

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
)

// newQueryTestStore 建临时 store 并写入一个含两条记录的技能。
// 根目录用 t.TempDir()（禁用真实 /tmp，见 skills 测试技能根禁用记忆）。
func newQueryTestStore(t *testing.T) (*checkpointing.EvolutionStore, map[string]checkpointing.EvolutionRecord) {
	t.Helper()
	store := checkpointing.NewEvolutionStore([]string{t.TempDir()})
	ctx := context.Background()
	_, err := store.CreateSkill(ctx, "demo_skill", "desc", "body", "")
	require.NoError(t, err)

	records := map[string]checkpointing.EvolutionRecord{
		"ev_aaa": makeQueryRecord("ev_aaa", 0.9, "2026-01-02T00:00:00Z", "body", "Troubleshooting", "内容甲", "部署失败先查端口"),
		"ev_bbb": makeQueryRecord("ev_bbb", 0.7, "2026-01-03T00:00:00Z", "script", "Examples", "内容乙", "lint 规则更新示例"),
	}
	evoLog := checkpointing.EmptyEvolutionLog("demo_skill")
	for _, r := range records {
		evoLog.Entries = append(evoLog.Entries, r)
	}
	require.NoError(t, store.SaveEvolutionLog(ctx, "demo_skill", evoLog, ""))
	return store, records
}

// makeQueryRecord 构造测试记录（Target 取值受 protocols.EVOLUTION_TARGET_VALUES 约束）。
func makeQueryRecord(id string, score float64, ts, target, section, content, summary string) checkpointing.EvolutionRecord {
	r := checkpointing.EvolutionRecord{
		Source:    "test",
		Timestamp: ts,
		Context:   summary,
		Score:     score,
		Change: checkpointing.EvolutionPatch{
			Target:  signal.EvolutionTarget(target),
			Section: section,
			Action:  "append",
			Content: content,
		},
	}
	r.ID = id
	r.Summary = &summary
	return r
}

func querySubject() map[string]any {
	return map[string]any{"kind": "skill", "name": "demo_skill"}
}

// TestSplitExperienceIndexQueryTerms 测试 OR 词袋切分。
// Python query.py:13-23。
func TestSplitExperienceIndexQueryTerms(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, SplitExperienceIndexQueryTerms("A| b||a"))
	assert.Empty(t, SplitExperienceIndexQueryTerms(""))
	assert.Empty(t, SplitExperienceIndexQueryTerms("||"))
}

// TestFilterExperienceIndexRecords_排序 测试双排序语义。
// Python query.py:56-60。
func TestFilterExperienceIndexRecords_排序(t *testing.T) {
	store, _ := newQueryTestStore(t)
	records := store.GetRecordsByScore(context.Background(), "demo_skill", nil)

	// score_desc：主键 score 倒序（ev_aaa 0.9 在前）
	got, err := FilterExperienceIndexRecords(records, "", "", "", "score_desc")
	require.NoError(t, err)
	assert.Equal(t, "ev_aaa", got[0].ID)

	// updated_desc：仅 timestamp 倒序（ev_bbb 01-03 在前）
	got, err = FilterExperienceIndexRecords(records, "", "", "", "updated_desc")
	require.NoError(t, err)
	assert.Equal(t, "ev_bbb", got[0].ID)

	// 非法 sort 报错（Python L58-59 ValueError）
	_, err = FilterExperienceIndexRecords(records, "", "", "", "bogus")
	assert.EqualError(t, err, "sort must be score_desc or updated_desc")
}

// TestFilterExperienceIndexRecords_过滤 测试 target/section/query 三过滤。
// Python query.py:39-53。
func TestFilterExperienceIndexRecords_过滤(t *testing.T) {
	store, _ := newQueryTestStore(t)
	records := store.GetRecordsByScore(context.Background(), "demo_skill", nil)

	// target 精确匹配
	got, err := FilterExperienceIndexRecords(records, "body", "", "", "score_desc")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "ev_aaa", got[0].ID)

	// section 精确匹配
	got, err = FilterExperienceIndexRecords(records, "", "Examples", "", "score_desc")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "ev_bbb", got[0].ID)

	// query 词袋：命中 haystack（summary|id|target|section 任一包含即可）
	got, err = FilterExperienceIndexRecords(records, "", "", "lint|nomatch", "score_desc")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "ev_bbb", got[0].ID)

	// query 全不命中
	got, err = FilterExperienceIndexRecords(records, "", "", "nothing", "score_desc")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestExperienceQueryService_ListExperiences 测试列表边界与分页。
// Python query.py:83-125。
func TestExperienceQueryService_ListExperiences(t *testing.T) {
	store, _ := newQueryTestStore(t)
	svc := NewExperienceQueryService(store)
	ctx := context.Background()

	// 技能不存在（Python L99-100）
	_, err := svc.ListExperiences(ctx, map[string]any{"kind": "skill", "name": "nope"}, nil, 20, "", "", "", "", "score_desc")
	assert.EqualError(t, err, "skill not found: nope")

	// limit 越界（Python L101-102）
	_, err = svc.ListExperiences(ctx, querySubject(), nil, 0, "", "", "", "", "score_desc")
	assert.EqualError(t, err, "limit must be between 1 and 100")
	_, err = svc.ListExperiences(ctx, querySubject(), nil, 101, "", "", "", "", "score_desc")
	assert.EqualError(t, err, "limit must be between 1 and 100")

	// 正常列表：total/has_more/next_cursor/items
	result, err := svc.ListExperiences(ctx, querySubject(), nil, 1, "", "", "", "", "score_desc")
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "list", result["operation"])
	assert.Equal(t, 2, result["total_count"])
	assert.Equal(t, true, result["has_more"])
	assert.Equal(t, "1", result["next_cursor"])
	items := result["items"].([]map[string]any)
	require.Len(t, items, 1)
	assert.Equal(t, "ev_aaa", items[0]["record_id"])
	assert.Equal(t, 0.9, items[0]["score"])

	// 翻到末页：has_more=false、next_cursor=nil
	result, err = svc.ListExperiences(ctx, querySubject(), nil, 1, "1", "", "", "", "score_desc")
	require.NoError(t, err)
	assert.Equal(t, false, result["has_more"])
	assert.Nil(t, result["next_cursor"])

	// minScore 过滤
	minScore := 0.8
	result, err = svc.ListExperiences(ctx, querySubject(), &minScore, 20, "", "", "", "", "score_desc")
	require.NoError(t, err)
	assert.Equal(t, 1, result["total_count"])
}

// TestExperienceQueryService_ReadExperiences 测试读取边界与截断。
// Python query.py:127-159 + _to_read_item。
func TestExperienceQueryService_ReadExperiences(t *testing.T) {
	store, _ := newQueryTestStore(t)
	svc := NewExperienceQueryService(store)
	ctx := context.Background()

	// 空 record_ids（Python L140-141）
	_, err := svc.ReadExperiences(ctx, querySubject(), []string{}, 2000)
	assert.EqualError(t, err, "record_ids must be a non-empty list")

	// max_content_chars 越界（Python L142-143）
	_, err = svc.ReadExperiences(ctx, querySubject(), []string{"ev_aaa"}, 0)
	assert.EqualError(t, err, "max_content_chars must be between 1 and 20000")

	// 未知 id（Python L150-153）
	_, err = svc.ReadExperiences(ctx, querySubject(), []string{"ev_zzz"}, 2000)
	assert.EqualError(t, err, "unknown record_ids: ev_zzz")

	// 正常读取 + content_truncated 标记
	result, err := svc.ReadExperiences(ctx, querySubject(), []string{"ev_aaa", "ev_bbb"}, 2)
	require.NoError(t, err)
	assert.Equal(t, "read", result["operation"])
	items := result["items"].([]map[string]any)
	require.Len(t, items, 2)
	assert.Equal(t, "内容甲"[:2], items[0]["content"]) // byte 截断对齐 Python [:n]
	assert.Equal(t, true, items[0]["content_truncated"])
	assert.Equal(t, true, items[1]["content_truncated"]) // 中文 3 字节/字，2 字节同样截断

	// 不截断分支
	result, err = svc.ReadExperiences(ctx, querySubject(), []string{"ev_bbb"}, 20000)
	require.NoError(t, err)
	items = result["items"].([]map[string]any)
	assert.Equal(t, "内容乙", items[0]["content"])
	assert.Equal(t, false, items[0]["content_truncated"])

	// 保序：调用顺序 ev_bbb 在前
	result, err = svc.ReadExperiences(ctx, querySubject(), []string{"ev_bbb", "ev_aaa"}, 20000)
	require.NoError(t, err)
	items = result["items"].([]map[string]any)
	assert.Equal(t, "ev_bbb", items[0]["record_id"])
}

// TestPaginateExperienceIndex_游标越界 测试游标越界安全。
func TestPaginateExperienceIndex_游标越界(t *testing.T) {
	records := make([]checkpointing.EvolutionRecord, 3)
	page, next := paginateExperienceIndex(records, "99", 10)
	assert.Empty(t, page)
	assert.Empty(t, next)
	page, next = paginateExperienceIndex(records, "abc", 10) // 非法游标回落 0
	assert.Len(t, page, 3)
	assert.Empty(t, next)
}
