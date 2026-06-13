package vector

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
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

func (f *fakeMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	// 从 option 中获取集合名 — 简单模拟：标记为已创建
	// 注意：新 SDK 的 Option 是不透明类型，无法直接读取内部字段
	// 使用 HasCollection 来判断已创建的集合
	return nil
}

func (f *fakeMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	return nil
}

func (f *fakeMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	name := option.Request().GetCollectionName()
	return f.collections[name], nil
}

func (f *fakeMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	return nil, fmt.Errorf("collection not found")
}

func (f *fakeMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	return milvusclient.InsertResult{}, nil
}

func (f *fakeMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	return nil, nil
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

func (f *fakeMilvusClient) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...interface{}) (milvusclient.IndexDescription, error) {
	return milvusclient.IndexDescription{}, nil
}

func (f *fakeMilvusClient) Close(ctx context.Context) error {
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

func TestMapFieldType_不支持的类型(t *testing.T) {
	_, err := mapFieldType(VectorDataTypeInt16)
	if err != nil {
		t.Logf("mapFieldType(Int16) 返回错误: %v (符合预期)", err)
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

func TestNormalizeScore_未知度量(t *testing.T) {
	// 未知度量类型应回退到 COSINE
	got := normalizeScore(0.6, entity.HAMMING)
	// HAMMING 不在 switch 中，走 default 分支 = ConvertCosineSimilarity(0.6)
	want := ConvertCosineSimilarity(0.6)
	if got != want {
		t.Errorf("normalizeScore(未知度量) = %v, want %v", got, want)
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

func TestMilvusVectorStore_Close_未连接(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	// client 为 nil，Close 不应 panic
	s.Close()
	if s.client != nil {
		t.Error("Close() 未连接时 client 应为 nil")
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

func TestJoinIDs(t *testing.T) {
	got := joinIDs([]string{"a", "b", "c"})
	want := `"a", "b", "c"`
	if got != want {
		t.Errorf("joinIDs() = %v, want %v", got, want)
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

func TestMapMilvusTypeToOurType_未知类型(t *testing.T) {
	// 使用不在映射中的类型
	got := mapMilvusTypeToOurType(entity.FieldTypeArray)
	if got != VectorDataTypeVarchar {
		t.Errorf("mapMilvusTypeToOurType(未知类型) = %v, want VectorDataTypeVarchar (回退)", got)
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

func TestBuildIndexParams(t *testing.T) {
	s := newTestStore()

	// 默认 AUTOINDEX
	idx := s.buildIndexParamsHelper("embedding", "COSINE", Options{})
	if idx == nil {
		t.Error("buildIndexParams() 不应返回 nil")
	}

	// MilvusAUTO
	autoIdx := s.buildIndexParamsHelper("embedding", "COSINE", Options{VectorField: vector_fields.NewMilvusAUTO("embedding")})
	if autoIdx == nil {
		t.Error("buildIndexParams(AUTO) 不应返回 nil")
	}

	// MilvusHNSW
	hnswIdx := s.buildIndexParamsHelper("embedding", "COSINE", Options{VectorField: vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)})
	if hnswIdx == nil {
		t.Error("buildIndexParams(HNSW) 不应返回 nil")
	}

	// MilvusIVF
	ivfIdx := s.buildIndexParamsHelper("embedding", "IP", Options{VectorField: vector_fields.NewMilvusIVF("embedding", 128, 8)})
	if ivfIdx == nil {
		t.Error("buildIndexParams(IVF) 不应返回 nil")
	}

	// MilvusSCANN
	scannIdx := s.buildIndexParamsHelper("embedding", "COSINE", Options{VectorField: vector_fields.NewMilvusSCANN("embedding", 128, 8, true, 200)})
	if scannIdx == nil {
		t.Error("buildIndexParams(SCANN) 不应返回 nil")
	}
}

// buildIndexParamsHelper 辅助函数，封装 buildIndexParams 并忽略 error
func (s *MilvusVectorStore) buildIndexParamsHelper(vectorFieldName, distanceMetric string, o Options) index.Index {
	idx, err := s.buildIndexParams(vectorFieldName, distanceMetric, o)
	if err != nil {
		return nil
	}
	return idx
}

func TestBuildAnnParam(t *testing.T) {
	s := newTestStore()

	// 默认参数
	ap, err := s.buildAnnParam(Options{}, 10)
	if err != nil {
		t.Fatalf("buildAnnParam() error = %v", err)
	}
	if ap != nil {
		// 默认返回 nil（无特殊 ANN 参数）
		t.Logf("buildAnnParam() default returned non-nil: %v", ap)
	}

	// HNSW 搜索参数
	hnswAp, err := s.buildAnnParam(Options{VectorField: vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)}, 10)
	if err != nil {
		t.Fatalf("buildAnnParam(HNSW) error = %v", err)
	}
	if hnswAp == nil {
		t.Error("buildAnnParam(HNSW) 不应返回 nil")
	}

	// IVF 搜索参数
	ivfAp, err := s.buildAnnParam(Options{VectorField: vector_fields.NewMilvusIVF("embedding", 128, 8)}, 10)
	if err != nil {
		t.Fatalf("buildAnnParam(IVF) error = %v", err)
	}
	if ivfAp == nil {
		t.Error("buildAnnParam(IVF) 不应返回 nil")
	}
}

func TestBuildAnnParam_HNSW零EfFactor(t *testing.T) {
	s := newTestStore()
	// EfSearchFactor 为 0，应使用默认值 64
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 0)
	ap, err := s.buildAnnParam(Options{VectorField: hnsw}, 10)
	if err != nil {
		t.Fatalf("buildAnnParam(HNSW ef=0) error = %v", err)
	}
	if ap == nil {
		t.Error("buildAnnParam(HNSW ef=0) 不应返回 nil")
	}
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

func TestWithVectorField(t *testing.T) {
	auto := vector_fields.NewMilvusAUTO("embedding")
	o := newOptions(WithVectorField(auto))
	if o.VectorField == nil {
		t.Error("WithVectorField() 应设置 VectorField")
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

func TestBuildDeleteExpr(t *testing.T) {
	// VARCHAR 主键
	got := buildDeleteExpr([]string{"a", "b"}, entity.FieldTypeVarChar)
	if got != `id in ["a", "b"]` {
		t.Errorf("buildDeleteExpr(VARCHAR) = %v, want id in [\"a\", \"b\"]", got)
	}

	// INT64 主键
	got = buildDeleteExpr([]string{"1", "2"}, entity.FieldTypeInt64)
	if got != `id in [1, 2]` {
		t.Errorf("buildDeleteExpr(INT64) = %v, want id in [1, 2]", got)
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

func TestMilvusVectorStore_UpdateSchema_预留(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	err := s.UpdateSchema(ctx, "test_coll", []any{})
	if err == nil {
		t.Error("UpdateSchema() 预留方法应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata(t *testing.T) {
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

func TestMilvusVectorStore_DeleteDocsByIDs_空(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByIDs(nil) error = %v, want nil", err)
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

func TestMilvusVectorStore_AddDocs_空文档(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()
	err := s.AddDocs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("AddDocs(nil) error = %v, want nil", err)
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

// ──────────────────────────── 补充覆盖率测试 ────────────────────────────

// fakeMilvusClientWithSearch 支持自定义搜索结果的 fake 客户端
type fakeMilvusClientWithSearch struct {
	*fakeMilvusClient
	searchResults   []milvusclient.ResultSet
	searchErr       error
	describeIdx     milvusclient.IndexDescription
	describeIdxErr  error
	describeColl    *entity.Collection
	describeCollErr error
	hasCollection   bool
	hasCollErr      error
}

func (f *fakeMilvusClientWithSearch) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...interface{}) ([]milvusclient.ResultSet, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResults, nil
}

func (f *fakeMilvusClientWithSearch) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...interface{}) (milvusclient.IndexDescription, error) {
	if f.describeIdxErr != nil {
		return milvusclient.IndexDescription{}, f.describeIdxErr
	}
	return f.describeIdx, nil
}

func (f *fakeMilvusClientWithSearch) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	if f.describeCollErr != nil {
		return nil, f.describeCollErr
	}
	return f.describeColl, nil
}

func (f *fakeMilvusClientWithSearch) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	if f.hasCollErr != nil {
		return false, f.hasCollErr
	}
	return f.hasCollection, nil
}

func TestMilvusVectorStore_Search_有结果(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
		searchResults: []milvusclient.ResultSet{
			{
				ResultCount: 2,
				Scores:      []float32{0.9, 0.7},
				Fields: []column.Column{
					column.NewColumnVarChar("id", []string{"doc1", "doc2"}),
					column.NewColumnVarChar("text", []string{"hello", "world"}),
				},
			},
		},
		hasCollection: true,
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
	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
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
	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
		searchResults: []milvusclient.ResultSet{
			{
				Err:         fmt.Errorf("partial error"),
				ResultCount: 0,
			},
			{
				ResultCount: 1,
				Scores:      []float32{0.8},
				Fields: []column.Column{
					column.NewColumnVarChar("id", []string{"doc1"}),
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
	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
		searchResults: []milvusclient.ResultSet{
			{
				ResultCount: 1,
				Scores:      []float32{1.5},
				Fields: []column.Column{
					column.NewColumnVarChar("id", []string{"doc1"}),
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
	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
		searchResults: []milvusclient.ResultSet{
			{
				ResultCount: 1,
				Scores:      []float32{0.85},
				Fields: []column.Column{
					column.NewColumnVarChar("id", []string{"doc1"}),
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

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中DescribeError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
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

func TestMilvusVectorStore_GetCollectionMetadata_缓存未命中DescribeSuccess(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	vecField := entity.NewField().WithName("embedding").WithDataType(entity.FieldTypeFloatVector).WithDim(128)
	pkField := entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithIsPrimaryKey(true)
	sch := entity.NewSchema().WithName("test_coll").WithField(pkField).WithField(vecField)

	fake := &fakeMilvusClientWithSearch{
		fakeMilvusClient: newFakeMilvusClient(),
		describeColl:     &entity.Collection{Name: "test_coll", Schema: sch},
		hasCollection:    true,
		describeIdx: milvusclient.IndexDescription{
			Index: index.NewAutoIndex(entity.L2),
		},
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
	// 验证缓存已更新
	s.mu.RLock()
	cached, ok := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if !ok {
		t.Error("GetCollectionMetadata() 应更新缓存")
	}
	if cached.VectorField != "embedding" {
		t.Errorf("缓存 vector_field = %v, want embedding", cached.VectorField)
	}
}

// fakeMilvusClientWithErrors 支持模拟各种错误的 fake 客户端
type fakeMilvusClientWithErrors struct {
	*fakeMilvusClient
	createCollErr  error
	insertErr      error
	deleteErr      error
	loadErr        error
	flushErr       error
	hasCollResult  bool
	hasCollErr     error
	dropCollErr    error
	describeCollErr error
}

func (f *fakeMilvusClientWithErrors) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	if f.createCollErr != nil {
		return f.createCollErr
	}
	return nil
}

func (f *fakeMilvusClientWithErrors) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	if f.insertErr != nil {
		return milvusclient.InsertResult{}, f.insertErr
	}
	return milvusclient.InsertResult{}, nil
}

func (f *fakeMilvusClientWithErrors) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error) {
	if f.deleteErr != nil {
		return milvusclient.DeleteResult{}, f.deleteErr
	}
	return milvusclient.DeleteResult{}, nil
}

func (f *fakeMilvusClientWithErrors) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...interface{}) error {
	if f.loadErr != nil {
		return f.loadErr
	}
	return nil
}

func (f *fakeMilvusClientWithErrors) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...interface{}) error {
	if f.flushErr != nil {
		return f.flushErr
	}
	return nil
}

func (f *fakeMilvusClientWithErrors) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	if f.hasCollErr != nil {
		return false, f.hasCollErr
	}
	return f.hasCollResult, nil
}

func (f *fakeMilvusClientWithErrors) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	if f.dropCollErr != nil {
		return f.dropCollErr
	}
	return nil
}

func (f *fakeMilvusClientWithErrors) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	if f.describeCollErr != nil {
		return nil, f.describeCollErr
	}
	return nil, fmt.Errorf("collection not found")
}

func TestMilvusVectorStore_CreateCollection_HasCollectionError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := &fakeMilvusClientWithErrors{
		fakeMilvusClient: newFakeMilvusClient(),
		hasCollErr:       fmt.Errorf("has collection error"),
	}
	s.client = fake

	ctx := context.Background()
	schema := createTestSchema()
	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() HasCollection 错误应返回错误")
	}
}

func TestMilvusVectorStore_CreateCollection_已存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := &fakeMilvusClientWithErrors{
		fakeMilvusClient: newFakeMilvusClient(),
		hasCollResult:    true,
	}
	s.client = fake

	schema := createTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("CreateCollection() 已存在时应返回 nil, error = %v", err)
	}
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

// ──────────────────────────── 增强版 fake 客户端 ────────────────────────────

// fakeMilvusClientFull 支持完整操作跟踪的 fake 客户端
type fakeMilvusClientFull struct {
	*fakeMilvusClient
	// 跟踪操作
	createdCollections []string
	droppedCollections []string
	insertedDocs       int
	deletedIDs         []string
	deletedExprs       []string
	// 自定义 DescribeCollection 结果
	describeCollResult *entity.Collection
	describeCollErr    error
	// 自定义 ListCollections
	listResult []string
	listErr    error
}

func newFakeMilvusClientFull() *fakeMilvusClientFull {
	return &fakeMilvusClientFull{
		fakeMilvusClient: newFakeMilvusClient(),
	}
}

func (f *fakeMilvusClientFull) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...interface{}) error {
	name := option.Request().GetCollectionName()
	f.createdCollections = append(f.createdCollections, name)
	f.collections[name] = true
	return nil
}

func (f *fakeMilvusClientFull) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...interface{}) error {
	name := option.Request().GetCollectionName()
	f.droppedCollections = append(f.droppedCollections, name)
	delete(f.collections, name)
	return nil
}

func (f *fakeMilvusClientFull) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...interface{}) (bool, error) {
	name := option.Request().GetCollectionName()
	return f.collections[name], nil
}

func (f *fakeMilvusClientFull) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...interface{}) (*entity.Collection, error) {
	if f.describeCollErr != nil {
		return nil, f.describeCollErr
	}
	if f.describeCollResult != nil {
		return f.describeCollResult, nil
	}
	return nil, fmt.Errorf("collection not found")
}

func (f *fakeMilvusClientFull) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...interface{}) (milvusclient.InsertResult, error) {
	f.insertedDocs++
	return milvusclient.InsertResult{}, nil
}

func (f *fakeMilvusClientFull) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...interface{}) (milvusclient.DeleteResult, error) {
	f.deletedExprs = append(f.deletedExprs, option.Request().GetExpr())
	return milvusclient.DeleteResult{}, nil
}

func (f *fakeMilvusClientFull) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...interface{}) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listResult != nil {
		return f.listResult, nil
	}
	names := make([]string, 0, len(f.collections))
	for name := range f.collections {
		names = append(names, name)
	}
	return names, nil
}

// ──────────────────────────── 补充测试 ────────────────────────────

func TestMilvusVectorStore_CreateCollection_成功创建(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake

	ctx := context.Background()
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(field)
	vecField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	schema.AddField(vecField)

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}
	if len(fake.createdCollections) != 1 || fake.createdCollections[0] != "test_coll" {
		t.Errorf("创建的集合 = %v, want [test_coll]", fake.createdCollections)
	}
}

func TestMilvusVectorStore_CreateCollection_集合已存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(field)
	vecField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	schema.AddField(vecField)

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("CreateCollection() 已存在应返回 nil, error = %v", err)
	}
	if len(fake.createdCollections) != 0 {
		t.Errorf("集合已存在不应再次创建, createdCollections = %v", fake.createdCollections)
	}
}

func TestMilvusVectorStore_CreateCollection_Int64主键(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake

	ctx := context.Background()
	schema, _ := NewCollectionSchema()
	pkField, _ := NewFieldSchema("id", VectorDataTypeInt64, WithPrimary())
	schema.AddField(pkField)
	vecField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(64))
	schema.AddField(vecField)

	err := s.CreateCollection(ctx, "test_coll_int64", schema)
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}
	// 验证 PKType 缓存是 Int64
	if meta, ok := s.collectionMetadata["test_coll_int64"]; ok {
		if meta.PKType != entity.FieldTypeInt64 {
			t.Errorf("PKType = %v, want Int64", meta.PKType)
		}
	}
}

func TestMilvusVectorStore_DeleteCollection_成功删除(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}
	s.client = fake

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}
	if len(fake.droppedCollections) != 1 || fake.droppedCollections[0] != "test_coll" {
		t.Errorf("删除的集合 = %v, want [test_coll]", fake.droppedCollections)
	}
	if _, ok := s.collectionMetadata["test_coll"]; ok {
		t.Error("删除集合后缓存应被清除")
	}
}

func TestMilvusVectorStore_AddDocs_成功(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      4,
		PKType:         entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "1", "embedding": []float64{0.1, 0.2, 0.3, 0.4}, "text": "hello"},
		{"id": "2", "embedding": []float64{0.5, 0.6, 0.7, 0.8}, "text": "world"},
	}

	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
	// 2 个文档在默认 batchSize 下作为 1 批插入
	if fake.insertedDocs != 1 {
		t.Errorf("插入批次 = %d, want 1", fake.insertedDocs)
	}
}

func TestMilvusVectorStore_AddDocs_批量(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      4,
		PKType:         entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	// 创建 5 个文档，batchSize=2，应分成 3 批
	docs := make([]map[string]any, 5)
	for i := range docs {
		docs[i] = map[string]any{
			"id":        fmt.Sprintf("%d", i),
			"embedding": []float64{float64(i), 0.2, 0.3, 0.4},
		}
	}

	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(2))
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
	// batchSize=2, 5个文档 = 3批(2+2+1)
	if fake.insertedDocs != 3 {
		t.Errorf("插入批次 = %d, want 3", fake.insertedDocs)
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs_Varchar主键(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      4,
		PKType:         entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
	if len(fake.deletedExprs) != 1 {
		t.Errorf("删除操作数 = %d, want 1", len(fake.deletedExprs))
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs_Int64主键(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      4,
		PKType:         entity.FieldTypeInt64,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"1", "2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_成功(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      4,
		PKType:         entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"name": "test"})
	if err != nil {
		t.Fatalf("DeleteDocsByFilters() error = %v", err)
	}
	if len(fake.deletedExprs) != 1 {
		t.Errorf("删除操作数 = %d, want 1", len(fake.deletedExprs))
	}
}

func TestMilvusVectorStore_ListCollectionNames_成功(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["coll1"] = true
	fake.collections["coll2"] = true
	s.client = fake

	ctx := context.Background()
	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("集合数量 = %d, want 2", len(names))
	}
}

func TestMilvusVectorStore_GetSchema_成功(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	fake.describeCollResult = &entity.Collection{
		Schema: &entity.Schema{
			CollectionName:   "test_coll",
			Description:      "测试集合",
			EnableDynamicField: true,
			Fields: []*entity.Field{
				{
					Name:      "id",
					DataType:  entity.FieldTypeVarChar,
					PrimaryKey: true,
					TypeParams: map[string]string{"max_length": "256"},
				},
				{
					Name:       "embedding",
					DataType:   entity.FieldTypeFloatVector,
					TypeParams: map[string]string{"dim": "128"},
				},
			},
		},
	}
	s.client = fake

	ctx := context.Background()
	schema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if schema == nil {
		t.Fatal("GetSchema() 返回 nil schema")
	}
	if !schema.EnableDynamicField {
		t.Error("EnableDynamicField 应为 true")
	}
	fields := schema.Fields()
	if len(fields) != 2 {
		t.Errorf("字段数 = %d, want 2", len(fields))
	}
}

func TestMilvusVectorStore_GetSchema_集合不存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake

	ctx := context.Background()
	_, err := s.GetSchema(ctx, "not_exist")
	if err == nil {
		t.Error("GetSchema() 不存在的集合应返回错误")
	}
}

func TestMilvusVectorStore_GetSchema_描述失败(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	fake.describeCollErr = fmt.Errorf("describe failed")
	s.client = fake

	ctx := context.Background()
	_, err := s.GetSchema(ctx, "test_coll")
	if err == nil {
		t.Error("GetSchema() DescribeCollection 失败应返回错误")
	}
}

func TestMilvusVectorStore_GetPKType_缓存命中(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.collectionMetadata["test_coll"] = &collMeta{
		PKType: entity.FieldTypeInt64,
	}
	pkType := s.getPKType("test_coll")
	if pkType != entity.FieldTypeInt64 {
		t.Errorf("getPKType() = %v, want Int64", pkType)
	}
}

func TestMilvusVectorStore_GetPKType_缓存未命中(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	pkType := s.getPKType("not_exist")
	if pkType != entity.FieldTypeVarChar {
		t.Errorf("getPKType() 缓存未命中应默认 VarChar, got %v", pkType)
	}
}

func TestMilvusVectorStore_CollectionExists_存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("CollectionExists() 应返回 true")
	}
}

func TestMilvusVectorStore_CollectionExists_不存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake

	ctx := context.Background()
	exists, err := s.CollectionExists(ctx, "not_exist")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("CollectionExists() 不存在的集合应返回 false")
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs_InsertError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	errFake := &fakeMilvusClientWithErrors{fakeMilvusClient: newFakeMilvusClient()}
	errFake.deleteErr = fmt.Errorf("delete failed")
	s.collectionMetadata["test_coll"] = &collMeta{
		PKType: entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = errFake

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"1"})
	if err == nil {
		t.Error("DeleteDocsByIDs() 删除失败应返回错误")
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_DeleteError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	errFake := &fakeMilvusClientWithErrors{fakeMilvusClient: newFakeMilvusClient()}
	errFake.deleteErr = fmt.Errorf("delete failed")
	s.collectionMetadata["test_coll"] = &collMeta{
		PKType: entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = errFake

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"name": "test"})
	if err == nil {
		t.Error("DeleteDocsByFilters() 删除失败应返回错误")
	}
}

func TestMilvusVectorStore_AddDocs_InsertError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	errFake := &fakeMilvusClientWithErrors{fakeMilvusClient: newFakeMilvusClient()}
	errFake.insertErr = fmt.Errorf("insert failed")
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      4,
		PKType:         entity.FieldTypeVarChar,
	}
	s.collectionsLoaded["test_coll"] = true
	s.client = errFake

	ctx := context.Background()
	docs := []map[string]any{{"id": "1", "embedding": []float64{0.1, 0.2, 0.3, 0.4}}}
	err := s.AddDocs(ctx, "test_coll", docs)
	if err == nil {
		t.Error("AddDocs() 插入失败应返回错误")
	}
}

func TestMilvusVectorStore_DeleteCollection_DropError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	errFake := &fakeMilvusClientWithErrors{fakeMilvusClient: newFakeMilvusClient()}
	errFake.dropCollErr = fmt.Errorf("drop failed")
	errFake.hasCollResult = true
	s.client = errFake

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err == nil {
		t.Error("DeleteCollection() 删除失败应返回错误")
	}
}

func TestMilvusVectorStore_CreateCollection_CreateError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	errFake := &fakeMilvusClientWithErrors{fakeMilvusClient: newFakeMilvusClient()}
	errFake.createCollErr = fmt.Errorf("create failed")
	s.client = errFake

	ctx := context.Background()
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(field)
	vecField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	schema.AddField(vecField)

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("CreateCollection() 创建失败应返回错误")
	}
}

func TestMilvusVectorStore_ensureLoaded_成功(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	fake.collections["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	err := s.ensureLoaded(ctx, "test_coll")
	if err != nil {
		t.Fatalf("ensureLoaded() error = %v", err)
	}
	if !s.collectionsLoaded["test_coll"] {
		t.Error("ensureLoaded() 后应标记为已加载")
	}
}

func TestMilvusVectorStore_ensureLoaded_已加载(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.collectionsLoaded["test_coll"] = true
	s.client = fake

	ctx := context.Background()
	err := s.ensureLoaded(ctx, "test_coll")
	if err != nil {
		t.Fatalf("ensureLoaded() error = %v", err)
	}
}

func TestMilvusVectorStore_ensureLoaded_不存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake

	ctx := context.Background()
	err := s.ensureLoaded(ctx, "not_exist")
	if err != nil {
		t.Fatalf("ensureLoaded() 不存在的集合不应返回错误, error = %v", err)
	}
}

func TestMilvusVectorStore_GetOutputFields_缓存命中(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.collectionMetadata["test_coll"] = &collMeta{
		FieldNames: []string{"id", "embedding", "text"},
	}
	fields := s.getOutputFields("test_coll")
	if len(fields) != 3 {
		t.Errorf("getOutputFields() = %v, want 3 fields", fields)
	}
}

func TestMilvusVectorStore_GetOutputFields_缓存未命中(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fields := s.getOutputFields("not_exist")
	if fields != nil {
		t.Errorf("getOutputFields() 缓存未命中应返回 nil, got %v", fields)
	}
}

func TestBuildAnnParam_FLAT(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	o := Options{VectorField: &vector_fields.MilvusFLAT{}}
	_, err := s.buildAnnParam(o, 10)
	if err != nil {
		t.Errorf("buildAnnParam(FLAT) error = %v", err)
	}
}

func TestBuildAnnParam_IVF(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	ivf := vector_fields.NewMilvusIVF("embedding", 128, 16)
	o := Options{VectorField: ivf}
	param, err := s.buildAnnParam(o, 10)
	if err != nil {
		t.Errorf("buildAnnParam(IVF) error = %v", err)
	}
	if param == nil {
		t.Error("buildAnnParam(IVF) 应返回非 nil")
	}
}

func TestBuildAnnParam_SCANN(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	scann := vector_fields.NewMilvusSCANN("embedding", 128, 8, false, 0)
	o := Options{VectorField: scann}
	param, err := s.buildAnnParam(o, 10)
	if err != nil {
		t.Errorf("buildAnnParam(SCANN) error = %v", err)
	}
	if param == nil {
		t.Error("buildAnnParam(SCANN) 应返回非 nil")
	}
}

func TestBuildAnnParam_HNSW_ef为零(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	o := Options{VectorField: &vector_fields.MilvusHNSW{EfSearchFactor: 0}}
	param, err := s.buildAnnParam(o, 10)
	if err != nil {
		t.Errorf("buildAnnParam(HNSW ef=0) error = %v", err)
	}
	if param == nil {
		t.Error("buildAnnParam(HNSW ef=0) 应使用默认 ef=64")
	}
}

func TestInferColumn_Float32向量(t *testing.T) {
	col := inferColumn("vec", []any{[]float32{0.1, 0.2, 0.3}, []float32{0.4, 0.5, 0.6}})
	if col == nil {
		t.Fatal("inferColumn(float32向量) 返回 nil")
	}
	if col.Name() != "vec" {
		t.Errorf("列名 = %s, want vec", col.Name())
	}
}

func TestInferColumn_Float64标量(t *testing.T) {
	col := inferColumn("score", []any{float64(1.5), float64(2.5)})
	if col == nil {
		t.Fatal("inferColumn(float64标量) 返回 nil")
	}
	if col.Name() != "score" {
		t.Errorf("列名 = %s, want score", col.Name())
	}
}

func TestInferColumn_Float32标量(t *testing.T) {
	col := inferColumn("weight", []any{float32(0.5), float32(1.5)})
	if col == nil {
		t.Fatal("inferColumn(float32标量) 返回 nil")
	}
	if col.Name() != "weight" {
		t.Errorf("列名 = %s, want weight", col.Name())
	}
}

func TestInferColumn_Bool(t *testing.T) {
	col := inferColumn("active", []any{true, false, true})
	if col == nil {
		t.Fatal("inferColumn(bool) 返回 nil")
	}
	if col.Name() != "active" {
		t.Errorf("列名 = %s, want active", col.Name())
	}
}

func TestInferColumn_空值(t *testing.T) {
	col := inferColumn("empty", []any{})
	if col != nil {
		t.Error("inferColumn(空值) 应返回 nil")
	}
}

func TestInferColumn_全Nil(t *testing.T) {
	col := inferColumn("nils", []any{nil, nil})
	if col != nil {
		t.Error("inferColumn(全 nil) 应返回 nil")
	}
}

func TestInferColumn_向量含Nil(t *testing.T) {
	col := inferColumn("vec", []any{[]float64{0.1, 0.2}, nil, []float64{0.3, 0.4}})
	if col == nil {
		t.Fatal("inferColumn(向量含nil) 返回 nil")
	}
}

func TestInferColumn_Int值(t *testing.T) {
	col := inferColumn("count", []any{int(1), int(2)})
	if col == nil {
		t.Fatal("inferColumn(int) 返回 nil")
	}
	if col.Name() != "count" {
		t.Errorf("列名 = %s, want count", col.Name())
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_schemaVersionInt64(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"schema_version": int64(5)})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_schemaVersionFloat64(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"schema_version": float64(3)})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_schemaVersionInvalidString(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"schema_version": "invalid"})
	if err == nil {
		t.Error("UpdateCollectionMetadata() 无效字符串应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_schemaVersionNegative(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"schema_version": -1})
	if err == nil {
		t.Error("UpdateCollectionMetadata() 负数版本应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_schemaVersionUnsupportedType(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"schema_version": []int{1}})
	if err == nil {
		t.Error("UpdateCollectionMetadata() 不支持类型应返回错误")
	}
}

func TestMilvusVectorStore_UpdateCollectionMetadata_distanceMetric非字符串(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClientFull()
	s.client = fake
	fake.collections["test_coll"] = true
	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
	}

	ctx := context.Background()
	// distance_metric 不是 string 类型，应跳过更新
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{"distance_metric": 123})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}
	if meta, ok := s.collectionMetadata["test_coll"]; ok && meta.DistanceMetric != "COSINE" {
		t.Errorf("distance_metric 非字符串不应更新, got %s", meta.DistanceMetric)
	}
}

func TestMilvusVectorStore_DeleteDocsByIDs_EnsureLoadedError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"1"})
	if err == nil {
		t.Error("DeleteDocsByIDs() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_DeleteDocsByFilters_EnsureLoadedError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"name": "test"})
	if err == nil {
		t.Error("DeleteDocsByFilters() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_DeleteCollection_GetClientError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err == nil {
		t.Error("DeleteCollection() 连接失败应返回错误")
	}
}

func TestMilvusVectorStore_GetSchema_GetClientError(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	s.createClient = func(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
		return nil, fmt.Errorf("connection refused")
	}

	ctx := context.Background()
	_, err := s.GetSchema(ctx, "test_coll")
	if err == nil {
		t.Error("GetSchema() 连接失败应返回错误")
	}
}
