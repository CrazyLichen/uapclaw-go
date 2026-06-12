package vector

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDBClient 用于测试的数据库客户端模拟
type fakeDBClient struct {
	execFn      func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn     func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn  func(ctx context.Context, sql string, args ...any) pgx.Row
	closeCalled atomic.Bool
}

// fakeRows 用于测试的行结果模拟
type fakeRows struct {
	rows   [][]any
	idx    int
	closed bool
}

// fakeRow 用于测试的单行结果模拟
type fakeRow struct {
	vals []any
	err  error
}

// ──────────────────────────── 导出函数 ────────────────────────────

func newFakeDBClient() *fakeDBClient {
	return &fakeDBClient{
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("OK"), nil
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &fakeRows{}, nil
		},
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &fakeRow{}
		},
	}
}

func (f *fakeDBClient) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return f.execFn(ctx, sql, args...)
}

func (f *fakeDBClient) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return f.queryFn(ctx, sql, args...)
}

func (f *fakeDBClient) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRowFn(ctx, sql, args...)
}

func (f *fakeDBClient) Close() {
	f.closeCalled.Store(true)
}

// fakeRows 实现 pgx.Rows 接口

func (r *fakeRows) Close()                                       { r.closed = true }
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("") }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool {
	if r.idx < len(r.rows) {
		r.idx++
		return true
	}
	return false
}
func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return fmt.Errorf("no more rows")
	}
	row := r.rows[r.idx-1]
	for i, d := range dest {
		if i < len(row) {
			val := row[i]
			// 支持 *int 指针类型
			switch dp := d.(type) {
			case *string:
				if val != nil {
					*dp = fmt.Sprintf("%v", val)
				}
			case *bool:
				if val != nil {
					*dp = val.(bool)
				}
			case *int:
				if val != nil {
					*dp = val.(int)
				}
			case *float64:
				if val != nil {
					*dp = val.(float64)
				}
			case **int:
				// 支持 *int 的指针（用于 nullable 列如 charMaxLen, numPrec）
				if val != nil {
					v := val.(int)
					*dp = &v
				}
				// val == nil 时保持 *dp = nil
			default:
				return fmt.Errorf("unsupported scan type: %T", d)
			}
		}
	}
	return nil
}
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte    { return nil }
func (r *fakeRows) Conn() *pgx.Conn        { return nil }

// fakeRow 实现 pgx.Row 接口

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.vals) == 0 {
		return nil
	}
	for i, d := range dest {
		if i < len(r.vals) {
			switch dp := d.(type) {
			case *string:
				*dp = fmt.Sprintf("%v", r.vals[i])
			case *bool:
				*dp = r.vals[i].(bool)
			case *int:
				*dp = r.vals[i].(int)
			case *float64:
				*dp = r.vals[i].(float64)
			default:
				return fmt.Errorf("unsupported scan type: %T", d)
			}
		}
	}
	return nil
}

// newTestGaussStore 创建带 fake 客户端的 GaussVectorStore
func newTestGaussStore() *GaussVectorStore {
	s := NewGaussVectorStore("postgres://test:test@localhost:5432/testdb")
	fake := newFakeDBClient()
	s.pool = fake
	s.createPool = func(ctx context.Context, connString string) (dbClient, error) {
		return fake, nil
	}
	return s
}

func createGaussTestSchema() *CollectionSchema {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec, text})
	return schema
}

// ─── 构造函数测试 ───

func TestNewGaussVectorStore(t *testing.T) {
	s := NewGaussVectorStore("postgres://localhost:5432/test")
	if s.connConfig != "postgres://localhost:5432/test" {
		t.Errorf("connConfig = %v, want postgres://localhost:5432/test", s.connConfig)
	}
	if s.collectionMetadata == nil {
		t.Error("collectionMetadata 不应为 nil")
	}
	if s.createPool == nil {
		t.Error("createPool 不应为 nil")
	}
}

// ─── CreateCollection 测试 ───

func TestGaussVectorStore_CreateCollection(t *testing.T) {
	s := newTestGaussStore()
	schema := createGaussTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 验证元数据缓存
	s.mu.RLock()
	meta, ok := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("集合元数据应已缓存")
	}
	if meta.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %v, want COSINE", meta.DistanceMetric)
	}
	if meta.VectorField != "embedding" {
		t.Errorf("VectorField = %v, want embedding", meta.VectorField)
	}
}

func TestGaussVectorStore_CreateCollection_缺少主键(t *testing.T) {
	s := newTestGaussStore()
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{vec})
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("缺少主键时应返回错误")
	}
}

func TestGaussVectorStore_CreateCollection_缺少向量字段(t *testing.T) {
	s := newTestGaussStore()
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk})
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("缺少向量字段时应返回错误")
	}
}

func TestGaussVectorStore_CreateCollection_已存在(t *testing.T) {
	s := newTestGaussStore()
	schema := createGaussTestSchema()
	ctx := context.Background()

	// 模拟集合已存在
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{true}, err: nil}
	}

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("集合已存在时应返回 nil, error = %v", err)
	}
}

// ─── DeleteCollection 测试 ───

func TestGaussVectorStore_DeleteCollection(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	// 先缓存元数据
	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
	}
	s.mu.Unlock()

	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	// 验证缓存已清除
	s.mu.RLock()
	_, ok := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if ok {
		t.Error("删除后缓存应已清除")
	}
}

// ─── CollectionExists 测试 ───

func TestGaussVectorStore_CollectionExists_存在(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{true}, err: nil}
	}
	ctx := context.Background()

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应存在")
	}
}

func TestGaussVectorStore_CollectionExists_不存在(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{false}, err: nil}
	}
	ctx := context.Background()

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("集合不应存在")
	}
}

// ─── ListCollectionNames 测试 ───

func TestGaussVectorStore_ListCollectionNames(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &fakeRows{
			rows: [][]any{{"coll1"}, {"coll2"}},
		}, nil
	}
	ctx := context.Background()

	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("len(names) = %v, want 2", len(names))
	}
}

// ─── UpdateSchema 测试 ───

func TestGaussVectorStore_UpdateSchema_未实现(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.UpdateSchema(ctx, "test_coll", nil)
	if err == nil {
		t.Error("UpdateSchema 应返回未实现错误")
	}
}

// ─── UpdateCollectionMetadata 测试 ───

func TestGaussVectorStore_UpdateCollectionMetadata(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"distance_metric": "L2",
		"vector_field":    "embedding",
		"vector_dim":      256,
		"schema_version":  "2",
	})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}

	s.mu.RLock()
	meta := s.collectionMetadata["test_coll"]
	s.mu.RUnlock()
	if meta.DistanceMetric != "L2" {
		t.Errorf("DistanceMetric = %v, want L2", meta.DistanceMetric)
	}
	if meta.VectorDim != 256 {
		t.Errorf("VectorDim = %v, want 256", meta.VectorDim)
	}
}

// ─── GetCollectionMetadata 测试 ───

func TestGaussVectorStore_GetCollectionMetadata_缓存命中(t *testing.T) {
	s := newTestGaussStore()
	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
		SchemaVersion:  "1",
	}
	s.mu.Unlock()
	ctx := context.Background()

	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != "COSINE" {
		t.Errorf("distance_metric = %v, want COSINE", meta["distance_metric"])
	}
}

// ─── AddDocs 测试 ───

func TestGaussVectorStore_AddDocs_空文档(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.AddDocs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("AddDocs(nil) error = %v, want nil", err)
	}
}

func TestGaussVectorStore_AddDocs(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}

	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

// ─── DeleteDocsByIDs 测试 ───

func TestGaussVectorStore_DeleteDocsByIDs_空列表(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByIDs(nil) error = %v, want nil", err)
	}
}

func TestGaussVectorStore_DeleteDocsByIDs(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
}

// ─── DeleteDocsByFilters 测试 ───

func TestGaussVectorStore_DeleteDocsByFilters_空过滤(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByFilters(nil) error = %v, want nil", err)
	}
}

func TestGaussVectorStore_DeleteDocsByFilters(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("DeleteDocsByFilters() error = %v", err)
	}
}

// ─── Close 测试 ───

func TestGaussVectorStore_Close(t *testing.T) {
	s := newTestGaussStore()
	s.Close()
	if s.pool != nil {
		t.Error("Close() 后 pool 应为 nil")
	}
}

// ─── 类型映射测试 ───

func TestMapFieldTypeToPG(t *testing.T) {
	tests := []struct {
		dt     VectorDataType
		want   string
		hasErr bool
	}{
		{VectorDataTypeVarchar, "VARCHAR", false},
		{VectorDataTypeFloatVector, "FLOATVECTOR", false},
		{VectorDataTypeInt64, "BIGINT", false},
		{VectorDataTypeInt32, "INTEGER", false},
		{VectorDataTypeInt16, "SMALLINT", false},
		{VectorDataTypeInt8, "SMALLINT", false},
		{VectorDataTypeFloat, "REAL", false},
		{VectorDataTypeDouble, "DOUBLE PRECISION", false},
		{VectorDataTypeBool, "BOOLEAN", false},
		{VectorDataTypeJSON, "JSONB", false},
		{VectorDataTypeArray, "", true},
	}
	for _, tt := range tests {
		got, err := mapFieldTypeToPG(tt.dt)
		if tt.hasErr && err == nil {
			t.Errorf("mapFieldTypeToPG(%v) 应返回错误", tt.dt)
		}
		if !tt.hasErr && got != tt.want {
			t.Errorf("mapFieldTypeToPG(%v) = %v, want %v", tt.dt, got, tt.want)
		}
	}
}

func TestMapPGTypeToOurType(t *testing.T) {
	tests := []struct {
		pgType string
		want   VectorDataType
	}{
		{"varchar", VectorDataTypeVarchar},
		{"character varying", VectorDataTypeVarchar},
		{"floatvector", VectorDataTypeFloatVector},
		{"bigint", VectorDataTypeInt64},
		{"int8", VectorDataTypeInt64},
		{"integer", VectorDataTypeInt32},
		{"int4", VectorDataTypeInt32},
		{"smallint", VectorDataTypeInt16},
		{"int2", VectorDataTypeInt16},
		{"real", VectorDataTypeFloat},
		{"float4", VectorDataTypeFloat},
		{"double precision", VectorDataTypeDouble},
		{"float8", VectorDataTypeDouble},
		{"boolean", VectorDataTypeBool},
		{"bool", VectorDataTypeBool},
		{"jsonb", VectorDataTypeJSON},
		{"json", VectorDataTypeJSON},
		{"unknown_type", VectorDataTypeVarchar},
	}
	for _, tt := range tests {
		got := mapPGTypeToOurType(tt.pgType)
		if got != tt.want {
			t.Errorf("mapPGTypeToOurType(%q) = %v, want %v", tt.pgType, got, tt.want)
		}
	}
}

// ─── 距离转换测试 ───

func TestGaussNormalizeScore_COSINE(t *testing.T) {
	// GaussDB COSINE 距离 0 → 相似度 1.0
	score := gaussNormalizeScore(0, "COSINE")
	if score != 1.0 {
		t.Errorf("gaussNormalizeScore(0, COSINE) = %v, want 1.0", score)
	}
	// GaussDB COSINE 距离 2 → 相似度 0.0
	score = gaussNormalizeScore(2, "COSINE")
	if score != 0.0 {
		t.Errorf("gaussNormalizeScore(2, COSINE) = %v, want 0.0", score)
	}
}

func TestGaussNormalizeScore_L2(t *testing.T) {
	score := gaussNormalizeScore(0, "L2")
	if score != 1.0 {
		t.Errorf("gaussNormalizeScore(0, L2) = %v, want 1.0", score)
	}
}

// ─── SQL 构建测试 ───

func TestGaussBuildCreateTableSQL(t *testing.T) {
	schema := createGaussTestSchema()
	sql, err := gaussBuildCreateTableSQL("test_coll", schema)
	if err != nil {
		t.Fatalf("gaussBuildCreateTableSQL() error = %v", err)
	}
	if !strings.Contains(sql, "CREATE TABLE") {
		t.Errorf("SQL 应包含 CREATE TABLE, got: %s", sql)
	}
	if !strings.Contains(sql, `"id" VARCHAR`) {
		t.Errorf("SQL 应包含 id VARCHAR 列, got: %s", sql)
	}
	if !strings.Contains(sql, `"embedding" FLOATVECTOR(128)`) {
		t.Errorf("SQL 应包含 embedding FLOATVECTOR(128) 列, got: %s", sql)
	}
	if !strings.Contains(sql, "PRIMARY KEY") {
		t.Errorf("SQL 应包含 PRIMARY KEY, got: %s", sql)
	}
}

func TestGaussBuildCreateIndexSQL(t *testing.T) {
	diskann := vector_fields.NewGaussDiskANN("embedding")
	sql := gaussBuildCreateIndexSQL("idx_test_embedding", "test_coll", "embedding", gaussMetricCosine, diskann)
	if !strings.Contains(sql, "USING GSDISKANN") {
		t.Errorf("SQL 应包含 USING GSDISKANN, got: %s", sql)
	}
	if !strings.Contains(sql, "cosine") {
		t.Errorf("SQL 应包含 cosine, got: %s", sql)
	}
	if !strings.Contains(sql, "enable_pq = true") {
		t.Errorf("SQL 应包含 enable_pq = true, got: %s", sql)
	}
}

func TestGaussBuildFilterClause(t *testing.T) {
	clause, args := gaussBuildFilterClause(map[string]any{
		"status": "active",
		"count":  10,
	})
	if !strings.Contains(clause, `"status" = $`) {
		t.Errorf("子句应包含 status 过滤, got: %s", clause)
	}
	if !strings.Contains(clause, `"count" = $`) {
		t.Errorf("子句应包含 count 过滤, got: %s", clause)
	}
	if len(args) != 2 {
		t.Errorf("参数数量 = %v, want 2", len(args))
	}
}

func TestGaussFormatVector(t *testing.T) {
	result := gaussFormatVector([]float64{1.0, 2.5, 3.0})
	if !strings.Contains(result, "[") || !strings.Contains(result, "]") {
		t.Errorf("格式化结果应包含方括号, got: %s", result)
	}
}

// ─── 标识符转义测试 ───

func TestGaussBuildCreateTableSQL_标识符转义(t *testing.T) {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(64))
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec})

	sql, err := gaussBuildCreateTableSQL("my-collection", schema)
	if err != nil {
		t.Fatalf("gaussBuildCreateTableSQL() error = %v", err)
	}
	// pgx Identifier.Sanitize 会将表名加双引号
	if !strings.Contains(sql, `"my-collection"`) {
		t.Errorf("SQL 应包含转义表名, got: %s", sql)
	}
}

// ─── GetSchema 测试 ───

func TestGaussVectorStore_GetSchema(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)

	// 模拟 information_schema.columns 返回三行
	// 列格式：column_name, data_type, character_maximum_length, numeric_precision
	fake.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
		return &fakeRows{
			rows: [][]any{
				{"id", "varchar", 256, nil},
				{"embedding", "floatvector", nil, 128},
				{"text", "varchar", 65535, nil},
			},
		}, nil
	}
	// 让 CollectionExists 返回 true
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{true}, err: nil}
	}

	ctx := context.Background()

	// 先缓存元数据（包含 vector_dim）
	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
		SchemaVersion:  "1",
	}
	s.mu.Unlock()

	schema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if schema == nil {
		t.Fatal("GetSchema() 返回 nil")
	}
}

// ─── Search 测试 ───

func TestGaussVectorStore_Search(t *testing.T) {
	s := newTestGaussStore()

	// 先缓存元数据
	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}
	s.mu.Unlock()

	ctx := context.Background()

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	// fakeRows 默认无数据，结果应为空
	if len(results) != 0 {
		t.Errorf("Search() 结果数 = %v, want 0（fake 无数据）", len(results))
	}
}

func TestGaussVectorStore_Search_带过滤(t *testing.T) {
	s := newTestGaussStore()

	s.mu.Lock()
	s.collectionMetadata["test_coll"] = &gaussCollMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      3,
	}
	s.mu.Unlock()

	ctx := context.Background()

	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search() 结果数 = %v, want 0", len(results))
	}
}

// ─── getOrInitCollMeta 测试 ───

func TestGaussVectorStore_getOrInitCollMeta(t *testing.T) {
	s := NewGaussVectorStore("postgres://localhost/test")

	// 无缓存时应初始化默认元数据
	meta := s.getOrInitCollMeta("new_coll", "embedding")
	if meta.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %v, want COSINE", meta.DistanceMetric)
	}
	if meta.VectorField != "embedding" {
		t.Errorf("VectorField = %v, want embedding", meta.VectorField)
	}
}

// ─── getClient 惰性创建测试 ───

func TestGaussVectorStore_getClient_惰性创建(t *testing.T) {
	s := NewGaussVectorStore("postgres://localhost/test")
	fake := newFakeDBClient()
	s.createPool = func(ctx context.Context, connString string) (dbClient, error) {
		return fake, nil
	}

	// pool 应为 nil
	if s.pool != nil {
		t.Error("初始 pool 应为 nil")
	}

	ctx := context.Background()
	client, err := s.getClient(ctx)
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if client == nil {
		t.Error("getClient() 返回 nil")
	}
	if s.pool == nil {
		t.Error("getClient() 后 pool 不应为 nil")
	}
}

// ─── resolveDiskANNConfig 测试 ───

func TestGaussVectorStore_resolveDiskANNConfig_默认(t *testing.T) {
	s := NewGaussVectorStore("postgres://localhost/test")
	o := Options{}
	diskann := s.resolveDiskANNConfig("embedding", o)
	if diskann.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", diskann.VectorFieldName)
	}
}

func TestGaussVectorStore_resolveDiskANNConfig_自定义(t *testing.T) {
	s := NewGaussVectorStore("postgres://localhost/test")
	custom := vector_fields.NewGaussDiskANN("my_vec")
	custom.PGNseg = 256
	o := Options{VectorField: custom}
	diskann := s.resolveDiskANNConfig("embedding", o)
	if diskann.PGNseg != 256 {
		t.Errorf("PGNseg = %v, want 256", diskann.PGNseg)
	}
}

// ─── mapDistanceMetricToPG 测试 ───

func TestMapDistanceMetricToPG(t *testing.T) {
	if mapDistanceMetricToPG("L2") != gaussMetricL2 {
		t.Error("L2 应映射为 l2")
	}
	if mapDistanceMetricToPG("COSINE") != gaussMetricCosine {
		t.Error("COSINE 应映射为 cosine")
	}
	if mapDistanceMetricToPG("UNKNOWN") != gaussMetricCosine {
		t.Error("未知度量应 fallback 为 cosine")
	}
}

// ─── gaussConvertValue 测试 ───

func TestGaussConvertValue(t *testing.T) {
	if gaussConvertValue([]byte("hello")) != "hello" {
		t.Error("[]byte 应转为 string")
	}
	if gaussConvertValue(float32(1.5)) != float64(1.5) {
		t.Error("float32 应转为 float64")
	}
	if gaussConvertValue(int32(42)) != int64(42) {
		t.Error("int32 应转为 int64")
	}
	if gaussConvertValue(int16(10)) != int64(10) {
		t.Error("int16 应转为 int64")
	}
	if gaussConvertValue("str") != "str" {
		t.Error("string 应保持不变")
	}
}

// ─── gaussNormalizeScore 默认度量测试 ───

func TestGaussNormalizeScore_默认(t *testing.T) {
	// 未知度量应 fallback 为 COSINE
	score := gaussNormalizeScore(0, "UNKNOWN")
	if score != 1.0 {
		t.Errorf("gaussNormalizeScore(0, UNKNOWN) = %v, want 1.0 (fallback COSINE)", score)
	}
}

// ─── GetCollectionMetadata 回查测试 ───

func TestGaussVectorStore_GetCollectionMetadata_数据库回查(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.queryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		return &fakeRow{vals: []any{"embedding", "floatvector"}, err: nil}
	}
	ctx := context.Background()

	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["vector_field"] != "embedding" {
		t.Errorf("vector_field = %v, want embedding", meta["vector_field"])
	}
}

// ─── getClient 创建失败测试 ───

func TestGaussVectorStore_getClient_创建失败(t *testing.T) {
	s := NewGaussVectorStore("invalid-conn-string")
	s.createPool = func(ctx context.Context, connString string) (dbClient, error) {
		return nil, fmt.Errorf("连接失败")
	}
	ctx := context.Background()

	_, err := s.getClient(ctx)
	if err == nil {
		t.Error("连接失败时应返回错误")
	}
}

// ─── AddDocs 分批测试 ───

func TestGaussVectorStore_AddDocs_分批(t *testing.T) {
	s := newTestGaussStore()
	ctx := context.Background()

	// 3 个文档，batch_size=2，应分 2 批
	docs := []map[string]any{
		{"id": "doc1", "text": "a", "embedding": []float64{0.1}},
		{"id": "doc2", "text": "b", "embedding": []float64{0.2}},
		{"id": "doc3", "text": "c", "embedding": []float64{0.3}},
	}

	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(2))
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

// ─── DeleteDocsByIDs 错误路径测试 ───

func TestGaussVectorStore_DeleteDocsByIDs_执行失败(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag(""), fmt.Errorf("执行失败")
	}
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1"})
	if err == nil {
		t.Error("执行失败时应返回错误")
	}
}

// ─── DeleteDocsByFilters 错误路径测试 ───

func TestGaussVectorStore_DeleteDocsByFilters_执行失败(t *testing.T) {
	s := newTestGaussStore()
	fake := s.pool.(*fakeDBClient)
	fake.execFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag(""), fmt.Errorf("执行失败")
	}
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "active"})
	if err == nil {
		t.Error("执行失败时应返回错误")
	}
}
