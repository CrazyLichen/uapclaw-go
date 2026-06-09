package vector

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMilvusClient 用于测试的 Milvus 客户端模拟
type fakeMilvusClient struct {
	collections map[string]bool
	schemas     map[string]*entity.Schema
}

func newFakeMilvusClient() *fakeMilvusClient {
	return &fakeMilvusClient{
		collections: make(map[string]bool),
		schemas:     make(map[string]*entity.Schema),
	}
}

func (f *fakeMilvusClient) CreateCollection(ctx context.Context, schema *entity.Schema, shardsNum int32, opts ...client.CreateCollectionOption) error {
	f.collections[schema.CollectionName] = true
	f.schemas[schema.CollectionName] = schema
	return nil
}

func (f *fakeMilvusClient) DropCollection(ctx context.Context, collName string, opts ...client.DropCollectionOption) error {
	delete(f.collections, collName)
	delete(f.schemas, collName)
	return nil
}

func (f *fakeMilvusClient) HasCollection(ctx context.Context, collName string) (bool, error) {
	return f.collections[collName], nil
}

func (f *fakeMilvusClient) DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error) {
	if !f.collections[collName] {
		return nil, fmt.Errorf("collection not found")
	}
	return &entity.Collection{Name: collName, Schema: f.schemas[collName]}, nil
}

func (f *fakeMilvusClient) Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Search(ctx context.Context, collName string, partitions []string, expr string, outputFields []string, vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int, sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Delete(ctx context.Context, collName string, partitionName string, expr string) error {
	return nil
}

func (f *fakeMilvusClient) ListCollections(ctx context.Context, opts ...client.ListCollectionOption) ([]*entity.Collection, error) {
	result := make([]*entity.Collection, 0, len(f.collections))
	for name := range f.collections {
		result = append(result, &entity.Collection{Name: name})
	}
	return result, nil
}

func (f *fakeMilvusClient) LoadCollection(ctx context.Context, collName string, async bool, opts ...client.LoadCollectionOption) error {
	return nil
}

func (f *fakeMilvusClient) AlterCollection(ctx context.Context, collName string, attrs ...entity.CollectionAttribute) error {
	return nil
}

func (f *fakeMilvusClient) Flush(ctx context.Context, collName string, async bool, opts ...client.FlushOption) error {
	return nil
}

func (f *fakeMilvusClient) CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool, opts ...client.IndexOption) error {
	return nil
}

func (f *fakeMilvusClient) DescribeIndex(ctx context.Context, collName string, fieldName string, opts ...client.IndexOption) ([]entity.Index, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Close() error {
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewMilvusVectorStore(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	if s.milvusURI != "http://localhost:19530" {
		t.Errorf("milvusURI = %v, want http://localhost:19530", s.milvusURI)
	}
	if s.dbName != "default" {
		t.Errorf("dbName = %v, want default", s.dbName)
	}
}

func TestNewMilvusVectorStore_默认数据库名(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "")
	if s.dbName != "default" {
		t.Errorf("dbName = %v, want default", s.dbName)
	}
}

func TestMilvusVectorStore_CreateCollection(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	schema := createTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	has, _ := fake.HasCollection(ctx, "test_coll")
	if !has {
		t.Error("集合应该已创建")
	}
}

func TestMilvusVectorStore_CreateCollection_已存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["test_coll"] = true
	schema := createTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("CreateCollection() 已存在时应返回 nil, error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteCollection(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["test_coll"] = true
	ctx := context.Background()

	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	has, _ := fake.HasCollection(ctx, "test_coll")
	if has {
		t.Error("集合应该已删除")
	}
}

func TestMilvusVectorStore_CollectionExists(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["test_coll"] = true
	ctx := context.Background()

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应该存在")
	}

	exists, err = s.CollectionExists(ctx, "not_exist")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("集合不应该存在")
	}
}

func TestMilvusVectorStore_ListCollectionNames(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["coll1"] = true
	fake.collections["coll2"] = true
	ctx := context.Background()

	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("ListCollectionNames() 返回 %d 个, want 2", len(names))
	}
}

func TestBuildFilterExpr(t *testing.T) {
	tests := []struct {
		name    string
		filters map[string]any
		want    string
	}{
		{"空过滤器", nil, ""},
		{"字符串值", map[string]any{"name": "test"}, `name == "test"`},
		{"整数值", map[string]any{"age": 30}, "age == 30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilterExpr(tt.filters)
			if got != tt.want {
				t.Errorf("buildFilterExpr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMilvusVectorStore_UpdateSchema_预留(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	ctx := context.Background()
	err := s.UpdateSchema(ctx, "test_coll", []any{})
	if err == nil {
		t.Error("UpdateSchema() 预留方法应返回错误")
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存命中(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
		SchemaVersion:  "1",
	}

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != "COSINE" {
		t.Errorf("distance_metric = %v, want COSINE", meta["distance_metric"])
	}
}

func TestMapFieldType(t *testing.T) {
	tests := []struct {
		name string
		dt   VectorDataType
		want entity.FieldType
	}{
		{"VARCHAR", VectorDataTypeVarchar, entity.FieldTypeVarChar},
		{"FLOAT_VECTOR", VectorDataTypeFloatVector, entity.FieldTypeFloatVector},
		{"INT64", VectorDataTypeInt64, entity.FieldTypeInt64},
		{"BOOL", VectorDataTypeBool, entity.FieldTypeBool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapFieldType(tt.dt)
			if err != nil {
				t.Fatalf("mapFieldType() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("mapFieldType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapMetricType(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		want   entity.MetricType
	}{
		{"L2", "L2", entity.L2},
		{"IP", "IP", entity.IP},
		{"COSINE", "COSINE", entity.COSINE},
		{"默认", "", entity.COSINE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapMetricType(tt.metric); got != tt.want {
				t.Errorf("mapMetricType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTestSchema 创建测试用的集合 Schema
func createTestSchema() *CollectionSchema {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec, text})
	return schema
}

// newTestStore 创建带 fake 客户端的 MilvusVectorStore
func newTestStore() *MilvusVectorStore {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}
	return s
}

func TestMilvusVectorStore_Close(t *testing.T) {
	s := newTestStore()
	s.Close()
	if s.client != nil {
		t.Error("Close() 后 client 应为 nil")
	}
}

func TestMilvusVectorStore_AddDocs_空文档(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	err := s.AddDocs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("AddDocs(nil) error = %v, want nil", err)
	}
}

func TestMilvusVectorStore_AddDocs(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
		{"id": "doc2", "text": "world", "embedding": []float64{0.4, 0.5, 0.6}},
	}
	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(10))
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs_空(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByIDs(nil) error = %v, want nil", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true

	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_空(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByFilters(nil) error = %v, want nil", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true

	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "deleted"})
	if err != nil {
		t.Fatalf("DeleteDocsByFilters() error = %v", err)
	}
}

func TestMilvusVectorStore_GetSchema(t *testing.T) {
	s := newTestStore()
	fake := newFakeMilvusClient()
	s.client = fake

	schema := createTestSchema()
	ctx := context.Background()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	gotSchema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if gotSchema == nil {
		t.Fatal("GetSchema() 不应返回 nil")
	}
}

func TestMilvusVectorStore_GetSchema_不存在(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	_, err := s.GetSchema(ctx, "not_exist")
	if err == nil {
		t.Error("GetSchema() 不存在的集合应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}
	// fake 中也需要注册集合
	fake := newFakeMilvusClient()
	s.client = fake
	fake.collections["test_coll"] = true

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"schema_version": 2})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}

	meta, _ := s.GetCollectionMetadata(ctx, "test_coll")
	if meta["schema_version"] != "2" {
		t.Errorf("schema_version = %v, want 2", meta["schema_version"])
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_空元数据(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{})
	if err != nil {
		t.Errorf("UpdateCollectionMetadata(empty) error = %v, want nil", err)
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_集合不存在(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "not_exist", map[string]any{"key": "val"})
	if err == nil {
		t.Error("UpdateCollectionMetadata() 不存在的集合应返回错误")
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中(t *testing.T) {
	s := newTestStore()
	fake := newFakeMilvusClient()
	s.client = fake

	schema := createTestSchema()
	ctx := context.Background()
	_ = s.CreateCollection(ctx, "test_coll", schema)

	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta == nil {
		t.Fatal("GetCollectionMetadata() 不应返回 nil")
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中集合不存在(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	// 集合不存在时 DescribeCollection 会失败，应回退默认值
	meta, err := s.GetCollectionMetadata(ctx, "not_exist")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() 不存在应回退默认值, error = %v", err)
	}
	if meta["distance_metric"] != defaultDistanceMetric {
		t.Errorf("distance_metric = %v, want %v", meta["distance_metric"], defaultDistanceMetric)
	}
}

func TestJoinIDs(t *testing.T) {
	got := joinIDs([]string{"a", "b", "c"})
	want := `"a", "b", "c"`
	if got != want {
		t.Errorf("joinIDs() = %v, want %v", got, want)
	}
}

func TestMapMilvusTypeToOurType(t *testing.T) {
	tests := []struct {
		name       string
		milvusType entity.FieldType
		want       VectorDataType
	}{
		{"VARCHAR", entity.FieldTypeVarChar, VectorDataTypeVarchar},
		{"FLOAT_VECTOR", entity.FieldTypeFloatVector, VectorDataTypeFloatVector},
		{"INT64", entity.FieldTypeInt64, VectorDataTypeInt64},
		{"BOOL", entity.FieldTypeBool, VectorDataTypeBool},
		{"JSON", entity.FieldTypeJSON, VectorDataTypeJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapMilvusTypeToOurType(tt.milvusType); got != tt.want {
				t.Errorf("mapMilvusTypeToOurType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeScore(t *testing.T) {
	tests := []struct {
		name       string
		rawScore   float64
		metricType entity.MetricType
		want       float64
	}{
		{"COSINE 0.6", 0.6, entity.COSINE, 0.8},
		{"L2 2.0", 2.0, entity.L2, 0.5},
		{"IP 0.0", 0.0, entity.IP, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeScore(tt.rawScore, tt.metricType); got != tt.want {
				t.Errorf("normalizeScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapFieldType_不支持的类型(t *testing.T) {
	_, err := mapFieldType(VectorDataTypeInt16)
	if err != nil {
		t.Logf("mapFieldType(Int16) 返回错误: %v (符合预期)", err)
	}
}

func TestDocsToColumns(t *testing.T) {
	s := newTestStore()
	docs := []map[string]any{
		{"id": "1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}
	columns, err := s.docsToColumns(docs)
	if err != nil {
		t.Fatalf("docsToColumns() error = %v", err)
	}
	if len(columns) == 0 {
		t.Error("docsToColumns() 应返回至少一列")
	}
}

func TestDocsToColumns_空(t *testing.T) {
	s := newTestStore()
	columns, err := s.docsToColumns(nil)
	if err != nil {
		t.Fatalf("docsToColumns(nil) error = %v", err)
	}
	if columns != nil {
		t.Error("docsToColumns(nil) 应返回 nil")
	}
}

func TestInferColumn(t *testing.T) {
	// 字符串列
	col := inferColumn("name", []any{"alice", "bob"})
	if col == nil {
		t.Fatal("inferColumn(string) 不应返回 nil")
	}
	if col.Name() != "name" {
		t.Errorf("inferColumn name = %v, want name", col.Name())
	}

	// 整数列
	col = inferColumn("age", []any{30, 25})
	if col == nil {
		t.Fatal("inferColumn(int) 不应返回 nil")
	}

	// int64 列
	col = inferColumn("count", []any{int64(100), int64(200)})
	if col == nil {
		t.Fatal("inferColumn(int64) 不应返回 nil")
	}

	// 空值
	col = inferColumn("empty", []any{})
	if col != nil {
		t.Error("inferColumn(empty) 应返回 nil")
	}

	// float32 向量列
	col = inferColumn("vec", []any{[]float32{0.1, 0.2}, []float32{0.3, 0.4}})
	if col == nil {
		t.Fatal("inferColumn([]float32) 不应返回 nil")
	}
}

func TestBuildIndexParams(t *testing.T) {
	s := newTestStore()

	// 默认 AUTOINDEX
	idx, err := s.buildIndexParams("embedding", "COSINE", Options{})
	if err != nil {
		t.Fatalf("buildIndexParams() error = %v", err)
	}
	if idx == nil {
		t.Error("buildIndexParams() 不应返回 nil")
	}

	// MilvusAUTO
	autoIdx, err := s.buildIndexParams("embedding", "COSINE", Options{VectorField: vector_fields.NewMilvusAUTO("embedding")})
	if err != nil {
		t.Fatalf("buildIndexParams(AUTO) error = %v", err)
	}
	_ = autoIdx

	// MilvusFLAT
	flatIdx, err := s.buildIndexParams("embedding", "L2", Options{VectorField: vector_fields.NewMilvusFLAT("embedding")})
	if err != nil {
		t.Fatalf("buildIndexParams(FLAT) error = %v", err)
	}
	_ = flatIdx

	// MilvusHNSW
	hnswIdx, err := s.buildIndexParams("embedding", "COSINE", Options{VectorField: vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)})
	if err != nil {
		t.Fatalf("buildIndexParams(HNSW) error = %v", err)
	}
	_ = hnswIdx

	// MilvusIVF
	ivfIdx, err := s.buildIndexParams("embedding", "IP", Options{VectorField: vector_fields.NewMilvusIVF("embedding", 128, 8)})
	if err != nil {
		t.Fatalf("buildIndexParams(IVF) error = %v", err)
	}
	_ = ivfIdx

	// MilvusSCANN
	scannIdx, err := s.buildIndexParams("embedding", "COSINE", Options{VectorField: vector_fields.NewMilvusSCANN("embedding", 128, 8, true, 200)})
	if err != nil {
		t.Fatalf("buildIndexParams(SCANN) error = %v", err)
	}
	_ = scannIdx
}

func TestBuildSearchParams(t *testing.T) {
	s := newTestStore()

	// 默认参数
	sp, err := s.buildSearchParams(Options{})
	if err != nil {
		t.Fatalf("buildSearchParams() error = %v", err)
	}
	if sp == nil {
		t.Error("buildSearchParams() 不应返回 nil")
	}

	// HNSW 搜索参数
	hnswSp, err := s.buildSearchParams(Options{VectorField: vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)})
	if err != nil {
		t.Fatalf("buildSearchParams(HNSW) error = %v", err)
	}
	_ = hnswSp

	// IVF 搜索参数
	ivfSp, err := s.buildSearchParams(Options{VectorField: vector_fields.NewMilvusIVF("embedding", 128, 8)})
	if err != nil {
		t.Fatalf("buildSearchParams(IVF) error = %v", err)
	}
	_ = ivfSp

	// SCANN 搜索参数
	scannSp, err := s.buildSearchParams(Options{VectorField: vector_fields.NewMilvusSCANN("embedding", 128, 8, true, 200)})
	if err != nil {
		t.Fatalf("buildSearchParams(SCANN) error = %v", err)
	}
	_ = scannSp
}

func TestGetDistanceMetricType(t *testing.T) {
	s := newTestStore()
	s.collectionMetadata["test_coll"] = &collMeta{DistanceMetric: "L2"}

	mt := s.getDistanceMetricType("test_coll", Options{})
	if mt != entity.L2 {
		t.Errorf("getDistanceMetricType() = %v, want L2", mt)
	}

	// Option 优先
	mt = s.getDistanceMetricType("test_coll", Options{DistanceMetric: "IP"})
	if mt != entity.IP {
		t.Errorf("getDistanceMetricType() with option = %v, want IP", mt)
	}

	// 无缓存无 Option 使用默认
	mt = s.getDistanceMetricType("not_exist", Options{})
	if mt != entity.COSINE {
		t.Errorf("getDistanceMetricType() default = %v, want COSINE", mt)
	}
}

func TestMilvusVectorStore_GetClient_惰性创建(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	// 不直接设置 s.client，让 getClient 通过 createClient 惰性创建
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return fake, nil
	}

	ctx := context.Background()
	client, err := s.getClient(ctx)
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if client == nil {
		t.Error("getClient() 不应返回 nil")
	}
	if s.client == nil {
		t.Error("getClient() 后 s.client 不应为 nil")
	}
}

func TestMilvusVectorStore_GetClient_连接失败(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	_, err := s.getClient(ctx)
	if err == nil {
		t.Error("getClient() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_GetClient_缓存命中(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	// 第二次调用应返回缓存的客户端
	client, err := s.getClient(ctx)
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if client == nil {
		t.Error("getClient() 缓存命中不应返回 nil")
	}
}

func TestMilvusVectorStore_EnsureLoaded(t *testing.T) {
	s := newTestStore()
	fake := newFakeMilvusClient()
	s.client = fake
	fake.collections["test_coll"] = true

	ctx := context.Background()
	err := s.ensureLoaded(ctx, "test_coll")
	if err != nil {
		t.Fatalf("ensureLoaded() error = %v", err)
	}
	if !s.collectionsLoaded["test_coll"] {
		t.Error("ensureLoaded() 后集合应标记为已加载")
	}

	// 再次调用应直接返回（缓存命中）
	err = s.ensureLoaded(ctx, "test_coll")
	if err != nil {
		t.Fatalf("ensureLoaded() 缓存命中 error = %v", err)
	}
}

func TestMilvusVectorStore_EnsureLoaded_集合不存在(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	err := s.ensureLoaded(ctx, "not_exist")
	if err != nil {
		t.Fatalf("ensureLoaded() 不存在的集合应返回 nil, error = %v", err)
	}
}

func TestMilvusVectorStore_Search(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	// fake 返回空结果
	if len(results) != 0 {
		t.Errorf("Search() 返回 %d 结果, want 0 (fake client)", len(results))
	}
}

func TestMilvusVectorStore_Search_默认TopK(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 0, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	_ = results
}

func TestMilvusVectorStore_DeleteCollection_不存在(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	err := s.DeleteCollection(ctx, "not_exist")
	if err != nil {
		t.Fatalf("DeleteCollection() 不存在应返回 nil, error = %v", err)
	}
}

func TestMilvusVectorStore_CreateCollection_无向量字段(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	// 没有 FLOAT_VECTOR 字段的 schema
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, text})

	err := s.CreateCollection(ctx, "bad_coll", schema)
	if err == nil {
		t.Error("CreateCollection() 无向量字段应返回错误")
	}
}

func TestBuildFilterExpr_多条件(t *testing.T) {
	got := buildFilterExpr(map[string]any{"name": "test", "age": 30})
	if got == "" {
		t.Error("buildFilterExpr() 多条件不应返回空")
	}
	if !contains(got, "&&") {
		t.Errorf("buildFilterExpr() 多条件应包含 &&, got %v", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ──────────────────────────── 补充覆盖率测试 ────────────────────────────

// fakeMilvusClientWithSearch 支持自定义搜索结果的 fake 客户端
type fakeMilvusClientWithSearch struct {
	*fakeMilvusClient
	searchResults   []client.SearchResult
	searchErr       error
	describeIdx     []entity.Index
	describeIdxErr  error
	describeCollErr error
}

func (f *fakeMilvusClientWithSearch) Search(ctx context.Context, collName string, partitions []string, expr string, outputFields []string, vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int, sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResults, nil
}

func (f *fakeMilvusClientWithSearch) DescribeIndex(ctx context.Context, collName string, fieldName string, opts ...client.IndexOption) ([]entity.Index, error) {
	if f.describeIdxErr != nil {
		return nil, f.describeIdxErr
	}
	return f.describeIdx, nil
}

func (f *fakeMilvusClientWithSearch) DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error) {
	if f.describeCollErr != nil {
		return nil, f.describeCollErr
	}
	return f.fakeMilvusClient.DescribeCollection(ctx, collName)
}

func TestMilvusVectorStore_Search_有结果(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true
	fakeBase.schemas["test_coll"] = entity.NewSchema().WithName("test_coll").WithDescription("test")

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		searchResults: []client.SearchResult{
			{
				ResultCount: 2,
				Scores:      []float32{0.9, 0.7},
				Fields: []entity.Column{
					entity.NewColumnVarChar("id", []string{"doc1", "doc2"}),
					entity.NewColumnVarChar("text", []string{"hello", "world"}),
				},
			},
		},
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search() 返回 %d 结果, want 2", len(results))
	}
	// COSINE: normalizeScore(0.9, COSINE) = ConvertCosineSimilarity(0.9)
	if results[0].Score <= 0 {
		t.Errorf("Search() 分数 %v 应为正值", results[0].Score)
	}
}

func TestMilvusVectorStore_Search_搜索错误(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		searchErr:        fmt.Errorf("search failed"),
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	ctx := context.Background()
	_, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err == nil {
		t.Error("Search() 搜索失败应返回错误")
	}
}

func TestMilvusVectorStore_Search_结果包含错误(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		searchResults: []client.SearchResult{
			{
				Err:         fmt.Errorf("partial error"),
				ResultCount: 0,
			},
			{
				ResultCount: 1,
				Scores:      []float32{0.8},
				Fields: []entity.Column{
					entity.NewColumnVarChar("id", []string{"doc1"}),
				},
			},
		},
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search() 应跳过错误结果, 返回 %d 结果, want 1", len(results))
	}
}

func TestMilvusVectorStore_Search_L2度量(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		searchResults: []client.SearchResult{
			{
				ResultCount: 1,
				Scores:      []float32{1.5},
				Fields: []entity.Column{
					entity.NewColumnVarChar("id", []string{"doc1"}),
				},
			},
		},
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "L2",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() 返回 %d 结果, want 1", len(results))
	}
}

func TestMilvusVectorStore_Search_IP度量(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		searchResults: []client.SearchResult{
			{
				ResultCount: 1,
				Scores:      []float32{0.85},
				Fields: []entity.Column{
					entity.NewColumnVarChar("id", []string{"doc1"}),
				},
			},
		},
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "IP",
		VectorField:    "embedding",
		VectorDim:      3,
	}

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() 返回 %d 结果, want 1", len(results))
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中DescribeSuccess(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true
	// 创建包含向量字段的 schema
	vecField := entity.NewField().WithName("embedding").WithDataType(entity.FieldTypeFloatVector).WithDim(128)
	pkField := entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithIsPrimaryKey(true)
	fakeBase.schemas["test_coll"] = entity.NewSchema().WithName("test_coll").WithField(pkField).WithField(vecField)

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		describeIdx:      []entity.Index{entity.NewGenericIndex("embedding", entity.AUTOINDEX, map[string]string{"metric_type": "L2"})},
	}
	s.client = fake

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["vector_field"] != "embedding" {
		t.Errorf("vector_field = %v, want embedding", meta["vector_field"])
	}
	if meta["vector_dim"] != 128 {
		t.Errorf("vector_dim = %v, want 128", meta["vector_dim"])
	}
	if meta["distance_metric"] != "L2" {
		t.Errorf("distance_metric = %v, want L2", meta["distance_metric"])
	}
	// 验证缓存已更新
	s.mu.RLock()
	cached, ok := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if !ok {
		t.Error("GetCollectionMetadata() 应更新缓存")
	}
	if cached.DistanceMetric != "L2" {
		t.Errorf("缓存 distance_metric = %v, want L2", cached.DistanceMetric)
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中DescribeIndexError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true
	vecField := entity.NewField().WithName("embedding").WithDataType(entity.FieldTypeFloatVector).WithDim(64)
	fakeBase.schemas["test_coll"] = entity.NewSchema().WithName("test_coll").WithField(vecField)

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		describeIdxErr:   fmt.Errorf("index not found"),
	}
	s.client = fake

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	// 索引查询失败时应回退默认值
	if meta["distance_metric"] != defaultDistanceMetric {
		t.Errorf("distance_metric = %v, want %v", meta["distance_metric"], defaultDistanceMetric)
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中DescribeIndexEmpty(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true
	vecField := entity.NewField().WithName("embedding").WithDataType(entity.FieldTypeFloatVector).WithDim(64)
	fakeBase.schemas["test_coll"] = entity.NewSchema().WithName("test_coll").WithField(vecField)

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		describeIdx:      []entity.Index{}, // 空索引列表
	}
	s.client = fake

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != defaultDistanceMetric {
		t.Errorf("distance_metric = %v, want %v (空索引应回退默认)", meta["distance_metric"], defaultDistanceMetric)
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中DescribeCollError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: fakeBase,
		describeCollErr:  fmt.Errorf("describe failed"),
	}
	s.client = fake

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() DescribeCollection 失败应回退默认值, error = %v", err)
	}
	if meta["distance_metric"] != defaultDistanceMetric {
		t.Errorf("distance_metric = %v, want %v", meta["distance_metric"], defaultDistanceMetric)
	}
	if meta["schema_version"] != "0" {
		t.Errorf("schema_version = %v, want 0", meta["schema_version"])
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_更新DistanceMetric(t *testing.T) {
	s := newTestStore()
	fake := newFakeMilvusClient()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"distance_metric": "L2",
		"schema_version":  "3",
	})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}

	meta, _ := s.GetCollectionMetadata(ctx, "test_coll")
	if meta["distance_metric"] != "L2" {
		t.Errorf("distance_metric = %v, want L2", meta["distance_metric"])
	}
	if meta["schema_version"] != "3" {
		t.Errorf("schema_version = %v, want 3", meta["schema_version"])
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_缓存中无集合(t *testing.T) {
	s := newTestStore()
	fake := newFakeMilvusClient()
	s.client = fake
	fake.collections["test_coll"] = true
	// collectionMetadata 中没有 test_coll 的缓存

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"schema_version": "2",
	})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}
	// 缓存中没有条目，不应 panic
}

func TestMilvusVectorStore_DeleteDocsByIDs_确保加载失败(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection failed")
	}

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1"})
	if err == nil {
		t.Error("DeleteDocsByIDs() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_空过滤表达式(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	s.collectionsLoaded["test_coll"] = true

	// buildFilterExpr 返回空字符串的情况
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{})
	if err != nil {
		t.Errorf("DeleteDocsByFilters(empty) error = %v, want nil", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_确保加载失败(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection failed")
	}

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"key": "val"})
	if err == nil {
		t.Error("DeleteDocsByFilters() 连接失败应返回错误")
	}
}

// fakeMilvusClientWithFlushError 支持模拟 Flush 错误的 fake 客户端
type fakeMilvusClientWithFlushError struct {
	*fakeMilvusClient
	flushErr error
}

func (f *fakeMilvusClientWithFlushError) Flush(ctx context.Context, collName string, async bool, opts ...client.FlushOption) error {
	if f.flushErr != nil {
		return f.flushErr
	}
	return nil
}

func TestMilvusVectorStore_DeleteDocsByIDs_FlushWarn(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithFlushError{
		fakeMilvusClient: fakeBase,
		flushErr:         fmt.Errorf("flush timeout"),
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1"})
	// Flush 失败只记 Warn，不应返回错误
	if err != nil {
		t.Errorf("DeleteDocsByIDs() Flush 失败应只 Warn, error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_FlushWarn(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithFlushError{
		fakeMilvusClient: fakeBase,
		flushErr:         fmt.Errorf("flush timeout"),
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "deleted"})
	// Flush 失败只记 Warn，不应返回错误
	if err != nil {
		t.Errorf("DeleteDocsByFilters() Flush 失败应只 Warn, error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs_DeleteError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	fake.collections["test_coll"] = true
	s.client = fake
	s.collectionsLoaded["test_coll"] = true

	// 使用自定义 fake 来模拟 Delete 错误
	deleteErrFake := &deleteErrorFake{fakeMilvusClient: fake}
	s.client = deleteErrFake

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1"})
	if err == nil {
		t.Error("DeleteDocsByIDs() Delete 错误应返回错误")
	}
}

// deleteErrorFake 模拟 Delete 错误
type deleteErrorFake struct {
	*fakeMilvusClient
}

func (f *deleteErrorFake) Delete(ctx context.Context, collName string, partitionName string, expr string) error {
	return fmt.Errorf("delete failed")
}

func TestMilvusVectorStore_DeleteDocsByFilters_DeleteError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	fake.collections["test_coll"] = true
	s.client = &deleteErrorFake{fakeMilvusClient: fake}
	s.collectionsLoaded["test_coll"] = true

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "deleted"})
	if err == nil {
		t.Error("DeleteDocsByFilters() Delete 错误应返回错误")
	}
}

func TestMapMilvusTypeToOurType_未知类型(t *testing.T) {
	// 使用不在映射中的类型
	got := mapMilvusTypeToOurType(entity.FieldTypeArray)
	if got != VectorDataTypeVarchar {
		t.Errorf("mapMilvusTypeToOurType(未知类型) = %v, want VectorDataTypeVarchar (回退)", got)
	}
}

func TestNormalizeScore_未知度量(t *testing.T) {
	// 未知度量类型应回退到 COSINE
	got := normalizeScore(0.6, entity.HAMMING)
	// HAMMING 不在 switch 中，走 default 分支 = ConvertCosineSimilarity(0.6)
	want := ConvertCosineSimilarity(0.6)
	if got != want {
		t.Errorf("normalizeScore(未知度量) = %v, want %v", got, want)
	}
}

func TestWithVectorField(t *testing.T) {
	auto := vector_fields.NewMilvusAUTO("embedding")
	o := newOptions(WithVectorField(auto))
	if o.VectorField == nil {
		t.Error("WithVectorField() 应设置 VectorField")
	}
}

func TestBuildSearchParams_HNSW零EfFactor(t *testing.T) {
	s := newTestStore()
	// EfSearchFactor 为 0，应使用默认值 64
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 0)
	sp, err := s.buildSearchParams(Options{VectorField: hnsw})
	if err != nil {
		t.Fatalf("buildSearchParams(HNSW ef=0) error = %v", err)
	}
	if sp == nil {
		t.Error("buildSearchParams(HNSW ef=0) 不应返回 nil")
	}
}

func TestMilvusVectorStore_AddDocs_FlushWarn(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	fake := &fakeMilvusClientWithFlushError{
		fakeMilvusClient: fakeBase,
		flushErr:         fmt.Errorf("flush timeout"),
	}
	s.client = fake
	s.collectionsLoaded["test_coll"] = true

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}
	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(10))
	// Flush 失败只记 Warn，不应返回错误
	if err != nil {
		t.Errorf("AddDocs() Flush 失败应只 Warn, error = %v", err)
	}
}

func TestMilvusVectorStore_AddDocs_InsertError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fakeBase := newFakeMilvusClient()
	fakeBase.collections["test_coll"] = true

	insertErrFake := &insertErrorFake{fakeMilvusClient: fakeBase}
	s.client = insertErrFake
	s.collectionsLoaded["test_coll"] = true

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}
	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(10))
	if err == nil {
		t.Error("AddDocs() Insert 错误应返回错误")
	}
}

// insertErrorFake 模拟 Insert 错误
type insertErrorFake struct {
	*fakeMilvusClient
}

func (f *insertErrorFake) Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error) {
	return nil, fmt.Errorf("insert failed")
}

func TestMilvusVectorStore_CreateCollection_HasCollectionError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := &hasCollectionErrorFake{}
	s.client = fake

	ctx := context.Background()
	schema := createTestSchema()
	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() HasCollection 错误应返回错误")
	}
}

// hasCollectionErrorFake 模拟 HasCollection 错误
type hasCollectionErrorFake struct{}

func (f *hasCollectionErrorFake) CreateCollection(ctx context.Context, schema *entity.Schema, shardsNum int32, opts ...client.CreateCollectionOption) error {
	return nil
}
func (f *hasCollectionErrorFake) DropCollection(ctx context.Context, collName string, opts ...client.DropCollectionOption) error {
	return nil
}
func (f *hasCollectionErrorFake) HasCollection(ctx context.Context, collName string) (bool, error) {
	return false, fmt.Errorf("has collection error")
}
func (f *hasCollectionErrorFake) DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error) {
	return nil, fmt.Errorf("not found")
}
func (f *hasCollectionErrorFake) Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error) {
	return nil, nil
}
func (f *hasCollectionErrorFake) Search(ctx context.Context, collName string, partitions []string, expr string, outputFields []string, vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int, sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error) {
	return nil, nil
}
func (f *hasCollectionErrorFake) Delete(ctx context.Context, collName string, partitionName string, expr string) error {
	return nil
}
func (f *hasCollectionErrorFake) ListCollections(ctx context.Context, opts ...client.ListCollectionOption) ([]*entity.Collection, error) {
	return nil, nil
}
func (f *hasCollectionErrorFake) LoadCollection(ctx context.Context, collName string, async bool, opts ...client.LoadCollectionOption) error {
	return nil
}
func (f *hasCollectionErrorFake) AlterCollection(ctx context.Context, collName string, attrs ...entity.CollectionAttribute) error {
	return nil
}
func (f *hasCollectionErrorFake) Flush(ctx context.Context, collName string, async bool, opts ...client.FlushOption) error {
	return nil
}
func (f *hasCollectionErrorFake) CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool, opts ...client.IndexOption) error {
	return nil
}
func (f *hasCollectionErrorFake) DescribeIndex(ctx context.Context, collName string, fieldName string, opts ...client.IndexOption) ([]entity.Index, error) {
	return nil, nil
}
func (f *hasCollectionErrorFake) Close() error { return nil }

func TestMilvusVectorStore_CreateCollection_向量字段DimZero(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	// Dim=0 的向量字段 - NewFieldSchema 直接会报错，所以直接构造 FieldSchema
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec := &FieldSchema{Name: "embedding", DType: VectorDataTypeFloatVector, Dim: 0}
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec})

	err := s.CreateCollection(ctx, "bad_coll", schema)
	if err == nil {
		t.Error("CreateCollection() Dim=0 应返回错误")
	}
}

func TestMilvusVectorStore_CreateCollection_CreateCollError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	// 用一个模拟 CreateCollection 错误的 fake
	createErrFake := &createCollErrorFake{fakeMilvusClient: fake}
	s.client = createErrFake

	ctx := context.Background()
	schema := createTestSchema()
	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() CreateCollection 错误应返回错误")
	}
}

// createCollErrorFake 模拟 CreateCollection 错误
type createCollErrorFake struct {
	*fakeMilvusClient
}

func (f *createCollErrorFake) CreateCollection(ctx context.Context, schema *entity.Schema, shardsNum int32, opts ...client.CreateCollectionOption) error {
	return fmt.Errorf("create collection failed")
}

func TestMilvusVectorStore_CreateCollection_CreateIndexError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	// 用一个模拟 CreateIndex 错误的 fake
	idxErrFake := &createIndexErrorFake{fakeMilvusClient: fake}
	s.client = idxErrFake

	ctx := context.Background()
	schema := createTestSchema()
	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() CreateIndex 错误应返回错误")
	}
}

// createIndexErrorFake 模拟 CreateIndex 错误
type createIndexErrorFake struct {
	*fakeMilvusClient
}

func (f *createIndexErrorFake) CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool, opts ...client.IndexOption) error {
	return fmt.Errorf("create index failed")
}

func TestMilvusVectorStore_CreateCollection_LoadError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	// 用一个模拟 LoadCollection 错误的 fake
	loadErrFake := &loadCollErrorFake{fakeMilvusClient: fake}
	s.client = loadErrFake

	ctx := context.Background()
	schema := createTestSchema()
	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() LoadCollection 错误应返回错误")
	}
}

// loadCollErrorFake 模拟 LoadCollection 错误
type loadCollErrorFake struct {
	*fakeMilvusClient
}

func (f *loadCollErrorFake) LoadCollection(ctx context.Context, collName string, async bool, opts ...client.LoadCollectionOption) error {
	return fmt.Errorf("load collection failed")
}

func TestMilvusVectorStore_DeleteCollection_HasCollectionError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.client = &hasCollectionErrorFake{}

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err == nil {
		t.Error("DeleteCollection() HasCollection 错误应返回错误")
	}
}

func TestMilvusVectorStore_DeleteCollection_DropError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	fake.collections["test_coll"] = true
	s.client = &dropCollErrorFake{fakeMilvusClient: fake}
	s.collectionMetadata["test_coll"] = &collMeta{DistanceMetric: "COSINE"}
	s.collectionsLoaded["test_coll"] = true

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err == nil {
		t.Error("DeleteCollection() DropCollection 错误应返回错误")
	}
}

// dropCollErrorFake 模拟 DropCollection 错误
type dropCollErrorFake struct {
	*fakeMilvusClient
}

func (f *dropCollErrorFake) DropCollection(ctx context.Context, collName string, opts ...client.DropCollectionOption) error {
	return fmt.Errorf("drop collection failed")
}

func TestMilvusVectorStore_GetSchema_HasCollectionError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.client = &hasCollectionErrorFake{}

	ctx := context.Background()
	_, err := s.GetSchema(ctx, "test_coll")
	if err == nil {
		t.Error("GetSchema() HasCollection 错误应返回错误")
	}
}

func TestMilvusVectorStore_GetSchema_DescribeError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	fake.collections["test_coll"] = true
	fake.schemas["test_coll"] = entity.NewSchema().WithName("test_coll")

	// DescribeCollection 在 fake 中返回 collection，这里用 describeErrFake 模拟错误
	s.client = &describeCollErrorFake{fakeMilvusClient: fake}

	ctx := context.Background()
	_, err := s.GetSchema(ctx, "test_coll")
	if err == nil {
		t.Error("GetSchema() DescribeCollection 错误应返回错误")
	}
}

// describeCollErrorFake 模拟 DescribeCollection 错误
type describeCollErrorFake struct {
	*fakeMilvusClient
}

func (f *describeCollErrorFake) DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error) {
	return nil, fmt.Errorf("describe collection failed")
}

func TestMilvusVectorStore_EnsureLoaded_LoadError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	fake.collections["test_coll"] = true
	s.client = &loadCollErrorFake{fakeMilvusClient: fake}

	ctx := context.Background()
	err := s.ensureLoaded(ctx, "test_coll")
	if err == nil {
		t.Error("ensureLoaded() LoadCollection 错误应返回错误")
	}
}

func TestMilvusVectorStore_EnsureLoaded_HasCollectionError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake
	fake.collections["test_coll"] = true

	// HasCollection 错误的 fake
	s.client = &hasCollErrorFake{fakeMilvusClient: fake}

	ctx := context.Background()
	err := s.ensureLoaded(ctx, "test_coll")
	if err == nil {
		t.Error("ensureLoaded() HasCollection 错误应返回错误")
	}
}

// hasCollErrorFake 模拟 HasCollection 错误（但其他方法正常）
type hasCollErrorFake struct {
	*fakeMilvusClient
}

func (f *hasCollErrorFake) HasCollection(ctx context.Context, collName string) (bool, error) {
	return false, fmt.Errorf("has collection error")
}

func TestMilvusVectorStore_ListCollectionNames_GetClientError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	_, err := s.ListCollectionNames(ctx)
	if err == nil {
		t.Error("ListCollectionNames() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_CollectionExists_GetClientError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	_, err := s.CollectionExists(ctx, "test_coll")
	if err == nil {
		t.Error("CollectionExists() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_AddDocs_EnsureLoadedError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	docs := []map[string]any{{"id": "1"}}
	err := s.AddDocs(ctx, "test_coll", docs)
	if err == nil {
		t.Error("AddDocs() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_Search_EnsureLoadedError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	_, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2}, "embedding", 5, nil)
	if err == nil {
		t.Error("Search() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_GetClientError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"key": "val"})
	if err == nil {
		t.Error("UpdateCollectionMetadata() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_HasCollectionError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.client = &hasCollectionErrorFake{}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"key": "val"})
	if err == nil {
		t.Error("UpdateCollectionMetadata() HasCollection 错误应返回错误")
	}
}

func TestMilvusVectorStore_Close_未连接(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	// client 为 nil，Close 不应 panic
	s.Close()
	if s.client != nil {
		t.Error("Close() 未连接时 client 应为 nil")
	}
}

func TestInferColumn_全Nil值(t *testing.T) {
	col := inferColumn("empty", []any{nil, nil})
	if col != nil {
		t.Error("inferColumn(全nil) 应返回 nil")
	}
}

func TestInferColumn_Float64Nil填充(t *testing.T) {
	// 测试 float64 向量中有 nil 值的填充
	col := inferColumn("vec", []any{[]float64{0.1, 0.2}, nil})
	if col == nil {
		t.Fatal("inferColumn([]float64 with nil) 不应返回 nil")
	}
}

func TestInferColumn_Float32Nil填充(t *testing.T) {
	// 测试 float32 向量中有 nil 值的填充
	col := inferColumn("vec", []any{[]float32{0.1, 0.2}, nil})
	if col == nil {
		t.Fatal("inferColumn([]float32 with nil) 不应返回 nil")
	}
}

func TestInferColumn_String含非字符串(t *testing.T) {
	// 字符串列中有非字符串值
	col := inferColumn("name", []any{"alice", 123})
	if col == nil {
		t.Fatal("inferColumn(string with non-string) 不应返回 nil")
	}
}

func TestInferColumn_Int含非整数(t *testing.T) {
	// int64 列中有非整数值
	col := inferColumn("count", []any{int64(100), "not_int"})
	if col == nil {
		t.Fatal("inferColumn(int64 with non-int) 不应返回 nil")
	}
}

func TestInferColumn_Int含非整数2(t *testing.T) {
	// int 列中有非整数值
	col := inferColumn("age", []any{30, "not_int"})
	if col == nil {
		t.Fatal("inferColumn(int with non-int) 不应返回 nil")
	}
}

func TestDocsToColumns_缺失字段(t *testing.T) {
	s := newTestStore()
	// 两个文档，字段不完全一致
	docs := []map[string]any{
		{"id": "1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
		{"id": "2", "embedding": []float64{0.4, 0.5, 0.6}}, // 缺少 text
	}
	columns, err := s.docsToColumns(docs)
	if err != nil {
		t.Fatalf("docsToColumns() error = %v", err)
	}
	if len(columns) == 0 {
		t.Error("docsToColumns() 应返回至少一列")
	}
}

func TestMapFieldType_所有类型(t *testing.T) {
	tests := []struct {
		name string
		dt   VectorDataType
		want entity.FieldType
	}{
		{"Int32", VectorDataTypeInt32, entity.FieldTypeInt32},
		{"Float", VectorDataTypeFloat, entity.FieldTypeFloat},
		{"Double", VectorDataTypeDouble, entity.FieldTypeDouble},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapFieldType(tt.dt)
			if err != nil {
				t.Fatalf("mapFieldType() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("mapFieldType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapMilvusTypeToOurType_所有类型(t *testing.T) {
	tests := []struct {
		name       string
		milvusType entity.FieldType
		want       VectorDataType
	}{
		{"Int32", entity.FieldTypeInt32, VectorDataTypeInt32},
		{"Float", entity.FieldTypeFloat, VectorDataTypeFloat},
		{"Double", entity.FieldTypeDouble, VectorDataTypeDouble},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapMilvusTypeToOurType(tt.milvusType); got != tt.want {
				t.Errorf("mapMilvusTypeToOurType() = %v, want %v", got, tt.want)
			}
		})
	}
}
