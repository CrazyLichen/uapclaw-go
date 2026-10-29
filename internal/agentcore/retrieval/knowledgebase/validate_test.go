package knowledgebase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 校验流水线测试 ────────────────────────────

// TestValidateIndex_全属性匹配 对齐 test_validation_passes_when_all_attributes_match。
func TestValidateIndex_全属性匹配(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	assert.NoError(t, b.ValidateIndex(context.Background()))
}

// TestValidateIndex_vs缺失跳过 对齐 test_validation_skipped_when_vector_store_is_none。
// Python 语义：构造期单侧缺失不触发（跳过触发），非 validate_index 本体行为。
// Go 对齐：本例只断言构造不炸（vs nil 分支不进 postAssignValidation）。
func TestValidateIndex_vs缺失跳过(t *testing.T) {
	_, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im))
	require.NoError(t, err)
	assert.NotNil(t, b)
}

// TestValidateIndex_七属性逐项不匹配 对齐 test_validation_fails_on_mismatch_*（7 例合一）。
func TestValidateIndex_七属性逐项不匹配(t *testing.T) {
	cases := []struct {
		name string
		mut  func(vs *fakeAttrStore, im *fakeAttrIndexer)
		attr string
	}{
		{"database_name", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.databaseName = "db2" }, "database_name"},
		{"distance_metric", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.distanceMetric = "euclidean" }, "distance_metric"},
		{"text_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.textField = "text" }, "text_field"},
		{"vector_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.vectorField = any("vector") }, "vector_field"},
		{"sparse_vector_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.sparseVectorField = "bm25_vector" }, "sparse_vector_field"},
		{"metadata_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.metadataField = "meta" }, "metadata_field"},
		{"doc_id_field", func(vs *fakeAttrStore, im *fakeAttrIndexer) { im.docIDField = "doc_id" }, "doc_id_field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs, im := makeCompatiblePair()
			tc.mut(vs, im)
			_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
				WithVectorStore(vs), WithIndexManager(im))
			require.Error(t, err)
			var be *exception.BaseError
			require.True(t, errors.As(err, &be))
			assert.Equal(t, 155508, be.Code())
			assert.Contains(t, be.Message(), "incompatible "+tc.attr+" configs")
		})
	}
}

// TestValidateIndex_错误消息含类型名与值 对齐 test_validation_error_message_includes_type_names。
func TestValidateIndex_错误消息含类型名与值(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "db2"
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "fakeAttrStore")
	assert.Contains(t, msg, "fakeAttrIndexer")
	assert.Contains(t, msg, "test_db")
	assert.Contains(t, msg, "db2")
}

// TestValidateIndex_双nil属性通过 对齐 test_validation_passes_when_attributes_are_none。
func TestValidateIndex_双nil属性通过(t *testing.T) {
	vs := &fakeAttrStore{} // 全零值：vectorField any 为 nil，其余空串
	im := &fakeAttrIndexer{}
	// 空串 == 空串 匹配；vectorField nil == nil 匹配
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	assert.NoError(t, err)
}

// TestValidateIndex_首个不匹配即停 对齐 test_validation_fails_on_first_mismatch（顺序敏感）。
func TestValidateIndex_首个不匹配即停(t *testing.T) {
	vs, im := makeCompatiblePair()
	vs.databaseName = "db1"
	im.databaseName = "db2"
	vs.distanceMetric = "cosine"
	im.distanceMetric = "euclidean"
	_, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	// database_name 在 distance_metric 之前——消息只含 database_name
	assert.Contains(t, err.Error(), "incompatible database_name configs")
	assert.NotContains(t, err.Error(), "incompatible distance_metric configs")
}

// ──────────────────────────── setter 测试 ────────────────────────────

// TestSetVectorStore_后置触发校验 对齐 test_validation_runs_when_setting_vector_store_after_index_manager。
func TestSetVectorStore_后置触发校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im))
	require.NoError(t, err)
	assert.NoError(t, b.SetVectorStore(vs))
	assert.Equal(t, vs, b.vectorStore)
}

// TestSetIndexManager_后置触发校验 对齐 test_validation_runs_when_setting_index_manager_after_vector_store。
func TestSetIndexManager_后置触发校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithVectorStore(vs))
	require.NoError(t, err)
	assert.NoError(t, b.SetIndexManager(im))
	assert.Equal(t, im, b.indexManager)
}

// TestSetVectorStore_不匹配拒绝 setter 后配置漂移场景（Go 增强）。
func TestSetVectorStore_不匹配拒绝(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "db2"
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, WithIndexManager(im))
	require.NoError(t, err)
	err = b.SetVectorStore(vs)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
}

// TestSetVectorStore_nil无校验 SetVectorStore(nil) 不触发校验不 panic（Go 增强）。
func TestSetVectorStore_nil无校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	assert.NoError(t, b.SetVectorStore(nil))
	assert.Nil(t, b.vectorStore)
}

// TestSetIndexManager_卸载后再配齐重新校验（Go 增强：二次变更语义）。
func TestSetIndexManager_卸载后再配齐重新校验(t *testing.T) {
	vs, im := makeCompatiblePair()
	b, err := NewBaseKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	require.NoError(t, b.SetIndexManager(nil)) // 卸载
	_, im2 := makeCompatiblePair()
	assert.NoError(t, b.SetIndexManager(im2)) // 重新配齐 → 重新校验（兼容通过）
}
