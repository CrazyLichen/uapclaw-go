package milvus

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMilvusClient 用于测试的 Milvus 客户端模拟
type fakeMilvusClient struct {
	collections map[string]bool
	createdColl []string
}

func newFakeMilvusClient() *fakeMilvusClient {
	return &fakeMilvusClient{
		collections: make(map[string]bool),
	}
}

func (f *fakeMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	name := option.Request().GetCollectionName()
	f.collections[name] = true
	f.createdColl = append(f.createdColl, name)
	return nil
}

func (f *fakeMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	name := option.Request().GetCollectionName()
	delete(f.collections, name)
	return nil
}

func (f *fakeMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	name := option.Request().GetCollectionName()
	return f.collections[name], nil
}

func (f *fakeMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	return milvusclient.InsertResult{}, nil
}

func (f *fakeMilvusClient) Upsert(ctx context.Context, option milvusclient.UpsertOption, callOptions ...interface{}) (milvusclient.UpsertResult, error) {
	return milvusclient.UpsertResult{}, nil
}

func (f *fakeMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return nil, nil
}

func (f *fakeMilvusClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...interface{}) (milvusclient.ResultSet, error) {
	return milvusclient.ResultSet{}, nil
}

func (f *fakeMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error) {
	return milvusclient.DeleteResult{}, nil
}

func (f *fakeMilvusClient) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...interface{}) ([]string, error) {
	names := make([]string, 0, len(f.collections))
	for name := range f.collections {
		names = append(names, name)
	}
	return names, nil
}

func (f *fakeMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...interface{}) error {
	return nil
}

func (f *fakeMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...interface{}) error {
	return nil
}

func (f *fakeMilvusClient) CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...interface{}) error {
	return nil
}

func (f *fakeMilvusClient) Close(ctx context.Context) error {
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBuildEntitySchema_字段完整性(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, err := buildEntitySchema(storageCfg, 512)
	if err != nil {
		t.Fatalf("buildEntitySchema() error = %v", err)
	}

	// 检查 Entity 特有字段
	fieldNames := make(map[string]bool)
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}

	// 通用字段
	for _, name := range []string{"uuid", "created_at", "user_id", "obj_type", "language", "metadata", "content", "content_embedding", "content_bm25"} {
		if !fieldNames[name] {
			t.Errorf("缺少通用字段: %s", name)
		}
	}
	// Entity 特有字段
	for _, name := range []string{"name", "name_embedding", "attributes", "relations", "episodes"} {
		if !fieldNames[name] {
			t.Errorf("缺少 Entity 特有字段: %s", name)
		}
	}
}

func TestBuildRelationSchema_字段完整性(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, err := buildRelationSchema(storageCfg, 512)
	if err != nil {
		t.Fatalf("buildRelationSchema() error = %v", err)
	}

	fieldNames := make(map[string]bool)
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}

	// 通用字段
	for _, name := range []string{"uuid", "created_at", "user_id", "obj_type", "language", "metadata", "content", "content_embedding", "content_bm25"} {
		if !fieldNames[name] {
			t.Errorf("缺少通用字段: %s", name)
		}
	}
	// Relation 特有字段
	for _, name := range []string{"name", "lhs", "rhs", "valid_since", "valid_until", "offset_since", "offset_until"} {
		if !fieldNames[name] {
			t.Errorf("缺少 Relation 特有字段: %s", name)
		}
	}
}

func TestBuildEpisodeSchema_字段完整性(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, err := buildEpisodeSchema(storageCfg, 512)
	if err != nil {
		t.Fatalf("buildEpisodeSchema() error = %v", err)
	}

	fieldNames := make(map[string]bool)
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}

	// 通用字段
	for _, name := range []string{"uuid", "created_at", "user_id", "obj_type", "language", "metadata", "content", "content_embedding", "content_bm25"} {
		if !fieldNames[name] {
			t.Errorf("缺少通用字段: %s", name)
		}
	}
	// Episode 特有字段
	for _, name := range []string{"valid_since", "entities"} {
		if !fieldNames[name] {
			t.Errorf("缺少 Episode 特有字段: %s", name)
		}
	}
}

func TestBuildEntitySchema_BM25Function(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, err := buildEntitySchema(storageCfg, 512)
	if err != nil {
		t.Fatalf("buildEntitySchema() error = %v", err)
	}

	// 检查 BM25 Function
	if len(schema.Functions) == 0 {
		t.Fatal("Entity Schema 应包含 BM25 Function")
	}
	found := false
	for _, fn := range schema.Functions {
		if fn.Name == "content_bm25_fn" && fn.Type == entity.FunctionTypeBM25 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Entity Schema 未找到 content_bm25_fn BM25 Function")
	}
}

func TestBuildRelationSchema_BM25Function(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, err := buildRelationSchema(storageCfg, 512)
	if err != nil {
		t.Fatalf("buildRelationSchema() error = %v", err)
	}

	if len(schema.Functions) == 0 {
		t.Fatal("Relation Schema 应包含 BM25 Function")
	}
	found := false
	for _, fn := range schema.Functions {
		if fn.Name == "content_bm25_fn" && fn.Type == entity.FunctionTypeBM25 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Relation Schema 未找到 content_bm25_fn BM25 Function")
	}
}

func TestBuildEpisodeSchema_BM25Function(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, err := buildEpisodeSchema(storageCfg, 512)
	if err != nil {
		t.Fatalf("buildEpisodeSchema() error = %v", err)
	}

	if len(schema.Functions) == 0 {
		t.Fatal("Episode Schema 应包含 BM25 Function")
	}
	found := false
	for _, fn := range schema.Functions {
		if fn.Name == "content_bm25_fn" && fn.Type == entity.FunctionTypeBM25 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Episode Schema 未找到 content_bm25_fn BM25 Function")
	}
}

func TestBuildEntitySchema_无NameEmbedding(t *testing.T) {
	// Relation 和 Episode 没有 name_embedding，只有 Entity 有
	storageCfg := graph.NewDefaultStorageConfig()

	entitySchema, _ := buildEntitySchema(storageCfg, 512)
	hasNameEmb := false
	for _, f := range entitySchema.Fields {
		if f.Name == "name_embedding" {
			hasNameEmb = true
			break
		}
	}
	if !hasNameEmb {
		t.Error("Entity Schema 应包含 name_embedding 字段")
	}

	relationSchema, _ := buildRelationSchema(storageCfg, 512)
	for _, f := range relationSchema.Fields {
		if f.Name == "name_embedding" {
			t.Error("Relation Schema 不应包含 name_embedding 字段")
			break
		}
	}

	episodeSchema, _ := buildEpisodeSchema(storageCfg, 512)
	for _, f := range episodeSchema.Fields {
		if f.Name == "name_embedding" {
			t.Error("Episode Schema 不应包含 name_embedding 字段")
			break
		}
	}
}

func TestEnsureCollections_全部新建(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	indexCfg := graph.NewDefaultIndexConfig()

	ctx := context.Background()
	err := EnsureCollections(ctx, fake, storageCfg, indexCfg, 512)
	if err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if len(fake.createdColl) != 3 {
		t.Errorf("应创建3个集合，实际创建了 %d 个", len(fake.createdColl))
	}
}

func TestEnsureCollections_已存在跳过(t *testing.T) {
	fake := newFakeMilvusClient()
	fake.collections[CollectionEntity] = true
	fake.collections[CollectionRelation] = true
	fake.collections[CollectionEpisode] = true
	storageCfg := graph.NewDefaultStorageConfig()
	indexCfg := graph.NewDefaultIndexConfig()

	ctx := context.Background()
	err := EnsureCollections(ctx, fake, storageCfg, indexCfg, 512)
	if err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if len(fake.createdColl) != 0 {
		t.Errorf("集合已存在不应重复创建，实际创建了 %d 个", len(fake.createdColl))
	}
}

func TestEnsureCollections_部分已存在(t *testing.T) {
	fake := newFakeMilvusClient()
	fake.collections[CollectionEntity] = true
	storageCfg := graph.NewDefaultStorageConfig()
	indexCfg := graph.NewDefaultIndexConfig()

	ctx := context.Background()
	err := EnsureCollections(ctx, fake, storageCfg, indexCfg, 512)
	if err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}
	if len(fake.createdColl) != 2 {
		t.Errorf("应创建2个新集合，实际创建了 %d 个", len(fake.createdColl))
	}
}

func TestBuildIndexOptions_Entity三索引(t *testing.T) {
	indexCfg := graph.NewDefaultIndexConfig()
	schema, _ := buildEntitySchema(graph.NewDefaultStorageConfig(), 512)

	opts, err := buildIndexOptions(indexCfg, CollectionEntity, schema)
	if err != nil {
		t.Fatalf("buildIndexOptions() error = %v", err)
	}
	// Entity 应有 name_embedding + content_embedding + content_bm25 + 标量字段索引
	if len(opts) < 3 {
		t.Errorf("Entity 索引数应 >= 3（含dense+sparse+scalar），实际 %d", len(opts))
	}
}

func TestBuildIndexOptions_Relation双索引(t *testing.T) {
	indexCfg := graph.NewDefaultIndexConfig()
	schema, _ := buildRelationSchema(graph.NewDefaultStorageConfig(), 512)

	opts, err := buildIndexOptions(indexCfg, CollectionRelation, schema)
	if err != nil {
		t.Fatalf("buildIndexOptions() error = %v", err)
	}
	// Relation 应有 content_embedding + content_bm25 + 标量字段索引
	if len(opts) < 2 {
		t.Errorf("Relation 索引数应 >= 2（含dense+sparse+scalar），实际 %d", len(opts))
	}
}

func TestAddCommonFields_字段列表(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema := entity.NewSchema().WithName("test_common")
	addCommonFields(schema, storageCfg, 256)

	fieldNames := make(map[string]bool)
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}

	expectedFields := []string{"uuid", "created_at", "user_id", "obj_type", "language", "metadata", "content", "content_embedding", "content_bm25"}
	for _, name := range expectedFields {
		if !fieldNames[name] {
			t.Errorf("通用字段缺失: %s", name)
		}
	}
}

func TestMapDistanceMetric(t *testing.T) {
	tests := []struct {
		input string
		want  entity.MetricType
	}{
		{"cosine", entity.COSINE},
		{"COSINE", entity.COSINE},
		{"euclidean", entity.L2},
		{"l2", entity.L2},
		{"L2", entity.L2},
		{"dot", entity.IP},
		{"ip", entity.IP},
		{"IP", entity.IP},
		{"unknown", entity.COSINE}, // 默认值
	}
	for _, tt := range tests {
		got := mapDistanceMetric(tt.input)
		if got != tt.want {
			t.Errorf("mapDistanceMetric(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBuildEntitySchema_UUID主键(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, _ := buildEntitySchema(storageCfg, 512)

	var pkField *entity.Field
	for _, f := range schema.Fields {
		if f.Name == "uuid" {
			pkField = f
			break
		}
	}
	if pkField == nil {
		t.Fatal("uuid 字段不存在")
	}
	if !pkField.PrimaryKey {
		t.Error("uuid 应为主键")
	}
	if pkField.DataType != entity.FieldTypeVarChar {
		t.Errorf("uuid 类型应为 VarChar，实际为 %v", pkField.DataType)
	}
}

func TestBuildEntitySchema_Array字段类型(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, _ := buildEntitySchema(storageCfg, 512)

	fieldMap := make(map[string]*entity.Field)
	for _, f := range schema.Fields {
		fieldMap[f.Name] = f
	}

	// 检查 relations 字段是 Array<VarChar>
	if f, ok := fieldMap["relations"]; ok {
		if f.DataType != entity.FieldTypeArray {
			t.Errorf("relations 类型应为 Array，实际为 %v", f.DataType)
		}
		if f.ElementType != entity.FieldTypeVarChar {
			t.Errorf("relations 元素类型应为 VarChar，实际为 %v", f.ElementType)
		}
	} else {
		t.Error("缺少 relations 字段")
	}

	// 检查 episodes 字段是 Array<VarChar>
	if f, ok := fieldMap["episodes"]; ok {
		if f.DataType != entity.FieldTypeArray {
			t.Errorf("episodes 类型应为 Array，实际为 %v", f.DataType)
		}
		if f.ElementType != entity.FieldTypeVarChar {
			t.Errorf("episodes 元素类型应为 VarChar，实际为 %v", f.ElementType)
		}
	} else {
		t.Error("缺少 episodes 字段")
	}
}

func TestBuildEpisodeSchema_Array字段类型(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	schema, _ := buildEpisodeSchema(storageCfg, 512)

	fieldMap := make(map[string]*entity.Field)
	for _, f := range schema.Fields {
		fieldMap[f.Name] = f
	}

	// 检查 entities 字段是 Array<VarChar>
	if f, ok := fieldMap["entities"]; ok {
		if f.DataType != entity.FieldTypeArray {
			t.Errorf("entities 类型应为 Array，实际为 %v", f.DataType)
		}
		if f.ElementType != entity.FieldTypeVarChar {
			t.Errorf("entities 元素类型应为 VarChar，实际为 %v", f.ElementType)
		}
	} else {
		t.Error("缺少 entities 字段")
	}
}
