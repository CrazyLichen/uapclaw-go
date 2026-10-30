package knowledgebase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeKB 可编程 KB 桩（Python AsyncMock 同构：只关心 Retrieve）。
type fakeKB struct {
	kbID    string
	results []common.RetrievalResult
	err     error
}

func (f *fakeKB) AddDocuments(context.Context, []reranker.Document, ...KBOption) ([]string, error) {
	return nil, nil
}
func (f *fakeKB) Retrieve(context.Context, string, *common.RetrievalConfig, ...KBOption) ([]common.RetrievalResult, error) {
	return f.results, f.err
}
func (f *fakeKB) DeleteDocuments(context.Context, []string, ...KBOption) (bool, error) {
	return false, nil
}
func (f *fakeKB) UpdateDocuments(context.Context, []reranker.Document, ...KBOption) ([]string, error) {
	return nil, nil
}
func (f *fakeKB) GetStatistics(context.Context) (map[string]any, error) { return nil, nil }
func (f *fakeKB) ParseFiles(context.Context, []string, ...KBOption) ([]reranker.Document, error) {
	return nil, nil
}
func (f *fakeKB) ParseURLs(context.Context, []string, ...KBOption) ([]reranker.Document, error) {
	return nil, nil
}
func (f *fakeKB) DeleteCollection(context.Context, string) error { return nil }
func (f *fakeKB) Close()                                         {}
func (f *fakeKB) Config() common.KnowledgeBaseConfig {
	return common.KnowledgeBaseConfig{KBID: f.kbID}
}

// 编译期断言：fakeKB 完整实现 KnowledgeBase
var _ KnowledgeBase = (*fakeKB)(nil)

// ──────────────────────────── RetrieveMultiKB ────────────────────────────

// TestRetrieveMultiKB_空列表 对齐 test_retrieve_multi_kb_empty_list。
func TestRetrieveMultiKB_空列表(t *testing.T) {
	assert.Empty(t, RetrieveMultiKB(context.Background(), nil, "test query", nil, nil))
	assert.Empty(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{}, "q", nil, nil))
}

// TestRetrieveMultiKB_成功 对齐 test_retrieve_multi_kb_success（去重 + 降序）。
func TestRetrieveMultiKB_成功(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "Result 1", Score: 0.9}, {Text: "Result 2", Score: 0.8},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "Result 2", Score: 0.85}, {Text: "Result 3", Score: 0.7},
	}}
	topK := 5
	results := RetrieveMultiKB(context.Background(), []KnowledgeBase{kb1, kb2}, "test query", nil, &topK)
	assert.Equal(t, []string{"Result 1", "Result 2", "Result 3"}, results)
}

// TestRetrieveMultiKB_部分失败 对齐 test_retrieve_multi_kb_with_failure（软失败）。
func TestRetrieveMultiKB_部分失败(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", err: errors.New("Error")}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}}
	results := RetrieveMultiKB(context.Background(), []KnowledgeBase{kb1, kb2}, "test query", nil, nil)
	assert.Equal(t, []string{"Result 1"}, results)
}

// TestRetrieveMultiKB_同分保首见序 同分时保持首次出现顺序
// （Python dict 插入序 + 稳定排序）。
func TestRetrieveMultiKB_同分保首见序(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "a", Score: 0.5}, {Text: "b", Score: 0.5},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "c", Score: 0.5},
	}}
	results := RetrieveMultiKB(context.Background(), []KnowledgeBase{kb1, kb2}, "q", nil, nil)
	assert.Equal(t, []string{"a", "b", "c"}, results)
}

// TestRetrieveMultiKB_Limit真值链 三档：topK 显式 > config.TopK > 默认 5。
func TestRetrieveMultiKB_Limit真值链(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "a", Score: 0.9}, {Text: "b", Score: 0.8}, {Text: "c", Score: 0.7},
		{Text: "d", Score: 0.6}, {Text: "e", Score: 0.5}, {Text: "f", Score: 0.4},
	}}
	// 档 1：显式 topK
	two := 2
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", nil, &two), 2)
	// 档 2：config.TopK（topK 为 nil）
	cfg := &common.RetrievalConfig{TopK: 3}
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", cfg, nil), 3)
	// 档 3：默认 5
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", nil, nil), 5)
	// truthiness：topK=0 视为未设（Python 0 为假值）→ 落到 config.TopK
	zero := 0
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", cfg, &zero), 3)
	// config.TopK=0 同样落到默认 5
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q",
		&common.RetrievalConfig{TopK: 0}, nil), 5)
}

// ──────────────────────────── RetrieveMultiKBWithSource ────────────────────────────

// TestRetrieveMultiKBWithSource_聚合 对齐 test_retrieve_multi_kb_with_source。
func TestRetrieveMultiKBWithSource_聚合(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "Result 1", Score: 0.9, Metadata: map[string]any{"raw_score": 0.9, "raw_score_scaled": 0.9}},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "Result 1", Score: 0.95, Metadata: map[string]any{"raw_score": 0.95, "raw_score_scaled": 0.95}},
	}}
	topK := 5
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "test query", nil, &topK)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Result 1", results[0].Text)
	assert.Equal(t, 0.95, results[0].Score)
	assert.Equal(t, 0.95, results[0].RawScore)
	assert.Equal(t, 0.95, results[0].RawScoreScaled)
	assert.Equal(t, []string{"kb1", "kb2"}, results[0].KbIDs)
}

// TestRetrieveMultiKBWithSource_空列表 空 kbs 返回空（Python `if not kbs`）。
func TestRetrieveMultiKBWithSource_空列表(t *testing.T) {
	got, err := RetrieveMultiKBWithSource(context.Background(), nil, "q", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestRetrieveMultiKBWithSource_RawScore语义 首见 meta 固定 + 后续非 nil 才 max。
//
// 对齐 Python 逐条推演：
//
//	kb1: raw_score=0.1（meta 显式）、scaled 回落 score=0.9
//	kb2: 无 meta → raw_score=scaled=0.95
//	kb3: raw_score=0.2、scaled 回落 score=0.5
//	结果：score=max(.9,.95,.5)=.95；raw=max(.1,.95,.2)=.95；scaled=max(.9,.95,.5)=.95
func TestRetrieveMultiKBWithSource_RawScore语义(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "t", Score: 0.9, Metadata: map[string]any{"raw_score": 0.1, "keep": "first"}},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "t", Score: 0.95}, // 无 metadata → raw_score 回落 score
	}}
	kb3 := &fakeKB{kbID: "kb3", results: []common.RetrievalResult{
		{Text: "t", Score: 0.5, Metadata: map[string]any{"raw_score": 0.2}},
	}}
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2, kb3}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	r := results[0]
	assert.Equal(t, 0.95, r.Score)
	assert.Equal(t, 0.95, r.RawScore)
	assert.Equal(t, 0.95, r.RawScoreScaled)
	assert.Equal(t, "first", r.Metadata["keep"]) // 首见 meta 固定不更新
	assert.Equal(t, []string{"kb1", "kb2", "kb3"}, r.KbIDs)
}

// TestRetrieveMultiKBWithSource_RawScore首见胜出 原始分只在更高时覆盖。
func TestRetrieveMultiKBWithSource_RawScore首见胜出(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "t", Score: 0.4, Metadata: map[string]any{"raw_score": 0.99, "raw_score_scaled": 0.99}},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "t", Score: 0.8, Metadata: map[string]any{"raw_score": 0.1, "raw_score_scaled": 0.1}},
	}}
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 0.8, results[0].Score)     // score 取大
	assert.Equal(t, 0.99, results[0].RawScore) // raw 取大（非末条覆盖）
	assert.Equal(t, 0.99, results[0].RawScoreScaled)
}

// TestRetrieveMultiKBWithSource_KbIDs排序去重 set 并集 + sorted。
func TestRetrieveMultiKBWithSource_KbIDs排序去重(t *testing.T) {
	kb1 := &fakeKB{kbID: "zz", results: []common.RetrievalResult{{Text: "t", Score: 0.9}}}
	kb2 := &fakeKB{kbID: "aa", results: []common.RetrievalResult{{Text: "t", Score: 0.8}}}
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, []string{"aa", "zz"}, results[0].KbIDs)
}

// TestRetrieveMultiKBWithSource_部分失败 软失败 + 配对不漂移（zip 语义）。
func TestRetrieveMultiKBWithSource_部分失败(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", err: errors.New("boom")}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{{Text: "ok", Score: 0.9}}}
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "ok", results[0].Text)
	assert.Equal(t, []string{"kb2"}, results[0].KbIDs)
}

// TestRetrieveMultiKBWithSource_稳定排序 同 score 保首见序。
func TestRetrieveMultiKBWithSource_稳定排序(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "first", Score: 0.5}, {Text: "second", Score: 0.5}, {Text: "third", Score: 0.5},
	}}
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, []string{"first", "second", "third"},
		[]string{results[0].Text, results[1].Text, results[2].Text})
}

// TestRetrieveMultiKBWithSource_Limit截断 截断在排序之后（Python ranked[:limit]）。
func TestRetrieveMultiKBWithSource_Limit截断(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "high", Score: 0.9}, {Text: "mid", Score: 0.5}, {Text: "low", Score: 0.1},
	}}
	one := 1
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, &one)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "high", results[0].Text)
}

// TestRetrieveMultiKBWithSource_负Limit Python 负 truthy → ranked[:-1] 等价截断。
func TestRetrieveMultiKBWithSource_负Limit(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "a", Score: 0.9}, {Text: "b", Score: 0.5},
	}}
	neg := -1
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, &neg)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "a", results[0].Text)
}

// TestRetrieveMultiKB_失败日志区分链路 两个聚合函数的 Warn 文案各自带
// 自己的函数名（对齐 Python `retrieve_multi_kb:` / `retrieve_multi_kb_with_source:`）。
//
// 日志经 logger.Setup(WithOutputDir(临时目录)) 落盘后读取断言；测试结束
// 调 logger.Close() 重置全局单例，避免污染同包其它用例。
func TestRetrieveMultiKB_失败日志区分链路(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, logger.Setup(logger.WithOutputDir(dir)))
	t.Cleanup(func() { _ = logger.Close() })

	bad := &fakeKB{kbID: "bad_kb", err: errors.New("boom")}

	RetrieveMultiKB(context.Background(), []KnowledgeBase{bad}, "q", nil, nil)
	_, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{bad}, "q", nil, nil)
	require.NoError(t, err)
	require.NoError(t, logger.Close())

	raw, err := os.ReadFile(filepath.Join(dir, "agent_core.log"))
	require.NoError(t, err)
	logged := string(raw)

	assert.Contains(t, logged, "retrieve_multi_kb: kb failed")
	assert.Contains(t, logged, "retrieve_multi_kb_with_source: kb failed")
	// kb_id 作为结构化字段随日志输出
	assert.Contains(t, logged, "bad_kb")
}

// TestRetrieveMultiKBWithSource_元数据非数值报错 对齐 Python：raw_score 为
// 非数值时 max() 抛 TypeError 中断整条聚合；Go 以 155509 返回错误而非静默跳过。
func TestRetrieveMultiKBWithSource_元数据非数值报错(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
	}{
		{"raw_score为字符串", map[string]any{"raw_score": "0.9"}},
		{"raw_score_scaled为字符串", map[string]any{"raw_score_scaled": "high"}},
		{"raw_score为bool", map[string]any{"raw_score": true}},
		{"raw_score为数组", map[string]any{"raw_score": []any{1.0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
				{Text: "t", Score: 0.9, Metadata: tc.meta},
			}}
			_, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, nil)
			require.Error(t, err)
			var be *exception.BaseError
			require.True(t, errors.As(err, &be))
			assert.Equal(t, 155509, be.Code())
		})
	}
}

// TestRetrieveMultiKBWithSource_元数据nil值不报错 键存在但值为 nil 时
// Python `is not None` 判假 → 不参与取最大且不报错。
func TestRetrieveMultiKBWithSource_元数据nil值不报错(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "t", Score: 0.7, Metadata: map[string]any{"raw_score": nil, "raw_score_scaled": nil}},
	}}
	results, err := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	// 不参与取最大 → 装填零值
	assert.Equal(t, 0.0, results[0].RawScore)
	assert.Equal(t, 0.0, results[0].RawScoreScaled)
	assert.Equal(t, 0.7, results[0].Score)
}

// TestRetrieveMultiKB_元数据非数值不影响 RetrieveMultiKB 文本版不读
// metadata，因此坏元数据不会让它失败（对齐 Python 只读 r.text / r.score）。
func TestRetrieveMultiKB_元数据非数值不影响(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "t", Score: 0.9, Metadata: map[string]any{"raw_score": "bad"}},
	}}
	assert.Equal(t, []string{"t"}, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", nil, nil))
}
