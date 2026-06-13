package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8/esapi"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
)

// ──────────────────────────── 结构体 ────────────────────────────

// esMockHandler 记录 ES 请求并提供可配置的响应
type esMockHandler struct {
	// responses 按路径模式映射响应函数
	responses map[string]func(w http.ResponseWriter, r *http.Request)
	// requests 记录收到的请求
	requests []esMockRequest
}

// esMockRequest 记录单个 ES 请求
type esMockRequest struct {
	Method string
	Path   string
	Body   string
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ServeHTTP 实现 http.Handler 接口
func (h *esMockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 记录请求
	body := ""
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		r.Body = io.NopCloser(strings.NewReader(body))
	}
	h.requests = append(h.requests, esMockRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Body:   body,
	})

	// 按方法+路径匹配响应
	key := r.Method + " " + r.URL.Path
	for pattern, handler := range h.responses {
		if strings.Contains(key, pattern) || strings.Contains(r.URL.Path, pattern) {
			handler(w, r)
			return
		}
	}
	// 默认 404
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `{"error": "mock: path not found: %s %s"}`, r.Method, r.URL.Path)
}

// newTestESVectorStore 创建带 mock 服务器的 ESVectorStore
func newTestESVectorStore(handler *esMockHandler) *ESVectorStore {
	server := httptest.NewServer(handler)
	s := NewESVectorStore([]string{server.URL}, "", "")
	s.createClient = func(addrs []string, user, pass string) (esClient, error) {
		return newFakeESClient(server), nil
	}
	return s
}

// newTestESVectorStoreWithServer 创建带指定 httptest.Server 的 ESVectorStore
func newTestESVectorStoreWithServer(s *ESVectorStore, server *httptest.Server) {
	s.createClient = func(addrs []string, user, pass string) (esClient, error) {
		return newFakeESClient(server), nil
	}
}

// createESTestSchema 创建 ES 测试用 Schema
func createESTestSchema() *CollectionSchema {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec, text})
	return schema
}

// ─── 构造函数测试 ───

func TestNewESVectorStore(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "user", "pass")
	if len(s.addresses) != 1 || s.addresses[0] != "http://localhost:9200" {
		t.Errorf("addresses = %v, want [http://localhost:9200]", s.addresses)
	}
	if s.username != "user" {
		t.Errorf("username = %v, want user", s.username)
	}
	if s.password != "pass" {
		t.Errorf("password = %v, want pass", s.password)
	}
	if s.indexPrefix != esDefaultIndexPrefix {
		t.Errorf("indexPrefix = %v, want %v", s.indexPrefix, esDefaultIndexPrefix)
	}
	if s.createClient == nil {
		t.Error("createClient 不应为 nil")
	}
}

func TestNewESVectorStore_自定义前缀(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "", WithESIndexPrefix("my_prefix"))
	if s.indexPrefix != "my_prefix" {
		t.Errorf("indexPrefix = %v, want my_prefix", s.indexPrefix)
	}
}

// ─── esIndexName 测试 ───

func TestESVectorStore_esIndexName(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	name := s.esIndexName("test_coll")
	expected := "agent_vector__test_coll"
	if name != expected {
		t.Errorf("esIndexName(test_coll) = %v, want %v", name, expected)
	}
}

func TestESVectorStore_esIndexName_自定义前缀(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "", WithESIndexPrefix("custom"))
	name := s.esIndexName("my_coll")
	expected := "custom__my_coll"
	if name != expected {
		t.Errorf("esIndexName(my_coll) = %v, want %v", name, expected)
	}
}

// ─── CreateCollection 测试 ───

func TestESVectorStore_CreateCollection(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			// HEAD /agent_vector__test_coll → 索引不存在
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			// PUT /agent_vector__test_coll → 创建索引
			"PUT ": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"acknowledged":true}`)
			},
			// POST /agent_vector__test_coll/__collection_metadata__ → 存储 _meta 文档
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"result":"created"}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	schema := createESTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 验证请求记录
	if len(handler.requests) < 2 {
		t.Fatalf("应有至少 2 个请求，实际 %d", len(handler.requests))
	}
}

func TestESVectorStore_CreateCollection_已存在(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			// HEAD /agent_vector__test_coll → 索引已存在
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	schema := createESTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("集合已存在时应返回 nil, error = %v", err)
	}
}

func TestESVectorStore_CreateCollection_缺少向量字段(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk})
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err == nil {
		t.Error("缺少向量字段时应返回错误")
	}
}

// ─── DeleteCollection 测试 ───

func TestESVectorStore_DeleteCollection(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			// HEAD 检查索引存在
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			// DELETE 删除索引
			"DELETE": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"acknowledged":true}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}
}

func TestESVectorStore_DeleteCollection_不存在(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("集合不存在时应返回 nil, error = %v", err)
	}
}

// ─── CollectionExists 测试 ───

func TestESVectorStore_CollectionExists_存在(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应存在")
	}
}

func TestESVectorStore_CollectionExists_不存在(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("集合不应存在")
	}
}

// ─── AddDocs 测试 ───

func TestESVectorStore_AddDocs_空文档(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	ctx := context.Background()

	err := s.AddDocs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("AddDocs(nil) error = %v, want nil", err)
	}
}

func TestESVectorStore_AddDocs(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			// _meta 文档获取
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, `{"_source":{"_meta":{"schema":{"fields":[{"name":"id","type":"VARCHAR","is_primary":true},{"name":"embedding","type":"FLOAT_VECTOR","dim":3}]},"distance_metric":"COSINE","vector_field":"embedding"}}}`)
				}
			},
			// bulk 请求
			"_bulk": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"errors":false,"items":[{"index":{"status":201}}]}`)
			},
			// refresh 请求
			"_refresh": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_shards":{"total":1,"successful":1,"failed":0}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float64{0.1, 0.2, 0.3}},
	}

	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

// ─── Search 测试 ───

func TestESVectorStore_Search(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			// _meta 文档获取
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE","vector_field":"embedding"}}}`)
			},
			// _search 请求
			"_search": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"hits": {
						"hits": [
							{"_id":"doc1","_score":0.95,"_source":{"text":"hello","id":"doc1"}},
							{"_id":"doc2","_score":0.85,"_source":{"text":"world","id":"doc2"}}
						]
					}
				}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search() 结果数 = %v, want 2", len(results))
	}
	if results[0].Fields["id"] != "doc1" {
		t.Errorf("results[0] id = %v, want doc1", results[0].Fields["id"])
	}
}

func TestESVectorStore_Search_带过滤(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE"}}}`)
			},
			"_search": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"hits":{"hits":[]}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search() 结果数 = %v, want 0", len(results))
	}
}

// ─── DeleteDocsByIDs 测试 ───

func TestESVectorStore_DeleteDocsByIDs_空列表(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	ctx := context.Background()

	err := s.DeleteDocsByIDs(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByIDs(nil) error = %v, want nil", err)
	}
}

func TestESVectorStore_DeleteDocsByIDs(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"_bulk": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"errors":false,"items":[{"delete":{"status":200}}]}`)
			},
			"_refresh": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_shards":{"total":1,"successful":1,"failed":0}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs() error = %v", err)
	}
}

// ─── DeleteDocsByFilters 测试 ───

func TestESVectorStore_DeleteDocsByFilters_空过滤(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	ctx := context.Background()

	err := s.DeleteDocsByFilters(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("DeleteDocsByFilters(nil) error = %v, want nil", err)
	}
}

func TestESVectorStore_DeleteDocsByFilters(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"_delete_by_query": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"deleted":2}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "active"})
	if err != nil {
		t.Fatalf("DeleteDocsByFilters() error = %v", err)
	}
}

// ─── ListCollectionNames 测试 ───

func TestESVectorStore_ListCollectionNames(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			// indices.get 返回格式: { "index_name": { ... }, ... }
			"GET": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"agent_vector__coll1": {"aliases":{}},
					"agent_vector__coll2": {"aliases":{}},
					"other_index": {"aliases":{}}
				}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("len(names) = %v, want 2", len(names))
	}
	// map 遍历顺序不确定，检查包含关系
	nameSet := map[string]bool{names[0]: true, names[1]: true}
	if !nameSet["coll1"] || !nameSet["coll2"] {
		t.Errorf("names = %v, want [coll1, coll2]", names)
	}
}

// ─── UpdateSchema 测试 ───

func TestESVectorStore_UpdateSchema_未实现(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	ctx := context.Background()

	err := s.UpdateSchema(ctx, "test_coll", nil)
	if err == nil {
		t.Error("UpdateSchema 应返回未实现错误")
	}
}

// ─── UpdateCollectionMetadata 测试 ───

func TestESVectorStore_UpdateCollectionMetadata_空元数据(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "test_coll", nil)
	if err != nil {
		t.Errorf("UpdateCollectionMetadata(nil) error = %v, want nil", err)
	}
}

func TestESVectorStore_UpdateCollectionMetadata(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE","vector_field":"embedding"}}}`)
				} else {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, `{"result":"updated"}`)
				}
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"distance_metric": "L2",
		"schema_version":  1,
	})
	if err != nil {
		t.Fatalf("UpdateCollectionMetadata() error = %v", err)
	}
}

func TestESVectorStore_UpdateCollectionMetadata_无效SchemaVersion(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	ctx := context.Background()

	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"schema_version": -1,
	})
	if err == nil {
		t.Error("schema_version 为负数时应返回错误")
	}
}

// ─── GetCollectionMetadata 测试 ───

func TestESVectorStore_GetCollectionMetadata(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"L2","vector_field":"embedding","schema_version":2}}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != "L2" {
		t.Errorf("distance_metric = %v, want L2", meta["distance_metric"])
	}
	if meta["schema_version"] != float64(2) {
		t.Errorf("schema_version = %v, want 2", meta["schema_version"])
	}
}

func TestESVectorStore_GetCollectionMetadata_默认值(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{}}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != esDefaultDistanceMetric {
		t.Errorf("默认 distance_metric = %v, want %v", meta["distance_metric"], esDefaultDistanceMetric)
	}
	if meta["schema_version"] != 0 {
		t.Errorf("默认 schema_version = %v, want 0", meta["schema_version"])
	}
}

// ─── Close 测试 ───

func TestESVectorStore_Close(t *testing.T) {
	handler := &esMockHandler{}
	s := newTestESVectorStore(handler)

	// 先触发客户端创建
	ctx := context.Background()
	_, _ = s.CollectionExists(ctx, "test_coll")

	s.Close()
	if s.client != nil {
		t.Error("Close() 后 client 应为 nil")
	}
}

func TestESVectorStore_Close_未创建客户端(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	s.Close() // 不应 panic
}

// ─── getClient 惰性创建测试 ───

func TestESVectorStore_getClient_惰性创建(t *testing.T) {
	handler := &esMockHandler{}
	s := newTestESVectorStore(handler)
	defer s.Close()

	// client 应为 nil
	if s.client != nil {
		t.Error("初始 client 应为 nil")
	}

	c, err := s.getClient()
	if err != nil {
		t.Fatalf("getClient() error = %v", err)
	}
	if c == nil {
		t.Error("getClient() 返回 nil")
	}
	if s.client == nil {
		t.Error("getClient() 后 client 不应为 nil")
	}
}

func TestESVectorStore_getClient_创建失败(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	s.createClient = func(addrs []string, user, pass string) (esClient, error) {
		return nil, fmt.Errorf("连接失败")
	}

	_, err := s.getClient()
	if err == nil {
		t.Error("连接失败时应返回错误")
	}
}

// ─── 类型映射测试 ───

func TestEsMapFieldType(t *testing.T) {
	tests := []struct {
		name     string
		field    *FieldSchema
		wantType string
	}{
		{"FLOAT_VECTOR", &FieldSchema{DType: VectorDataTypeFloatVector, Dim: 128}, "dense_vector"},
		{"VARCHAR", &FieldSchema{DType: VectorDataTypeVarchar}, "keyword"},
		{"INT64", &FieldSchema{DType: VectorDataTypeInt64}, "long"},
		{"INT32", &FieldSchema{DType: VectorDataTypeInt32}, "integer"},
		{"INT16", &FieldSchema{DType: VectorDataTypeInt16}, "integer"},
		{"INT8", &FieldSchema{DType: VectorDataTypeInt8}, "integer"},
		{"FLOAT", &FieldSchema{DType: VectorDataTypeFloat}, "float"},
		{"DOUBLE", &FieldSchema{DType: VectorDataTypeDouble}, "double"},
		{"BOOL", &FieldSchema{DType: VectorDataTypeBool}, "boolean"},
		{"JSON", &FieldSchema{DType: VectorDataTypeJSON}, "object"},
		{"ARRAY", &FieldSchema{DType: VectorDataTypeArray}, "object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := esMapFieldType(tt.field)
			gotType, _ := result["type"].(string)
			if gotType != tt.wantType {
				t.Errorf("esMapFieldType(%v) type = %v, want %v", tt.name, gotType, tt.wantType)
			}
		})
	}
}

func TestEsMapFieldType_FloatVector默认维度(t *testing.T) {
	// Dim=0 应使用默认 768
	field := &FieldSchema{DType: VectorDataTypeFloatVector, Dim: 0}
	result := esMapFieldType(field)
	dims, _ := result["dims"].(int)
	if dims != 768 {
		t.Errorf("Dim=0 时 dims = %v, want 768", dims)
	}
}

func TestEsMapTypeToOurType(t *testing.T) {
	tests := []struct {
		esType string
		want   VectorDataType
	}{
		{"dense_vector", VectorDataTypeFloatVector},
		{"keyword", VectorDataTypeVarchar},
		{"text", VectorDataTypeVarchar},
		{"long", VectorDataTypeInt64},
		{"integer", VectorDataTypeInt32},
		{"short", VectorDataTypeInt16},
		{"byte", VectorDataTypeInt8},
		{"float", VectorDataTypeFloat},
		{"double", VectorDataTypeDouble},
		{"boolean", VectorDataTypeBool},
		{"object", VectorDataTypeJSON},
		{"unknown", VectorDataTypeVarchar},
	}
	for _, tt := range tests {
		got := esMapTypeToOurType(tt.esType)
		if got != tt.want {
			t.Errorf("esMapTypeToOurType(%q) = %v, want %v", tt.esType, got, tt.want)
		}
	}
}

func TestEsMapDistanceMetricToSimilarity(t *testing.T) {
	tests := []struct {
		metric string
		want   string
	}{
		{"COSINE", "cosine"},
		{"cosine", "cosine"},
		{"L2", "l2_norm"},
		{"IP", "dot_product"},
		{"UNKNOWN", "cosine"},
	}
	for _, tt := range tests {
		got := esMapDistanceMetricToSimilarity(tt.metric)
		if got != tt.want {
			t.Errorf("esMapDistanceMetricToSimilarity(%q) = %v, want %v", tt.metric, got, tt.want)
		}
	}
}

// ─── esBuildMappings 测试 ───

func TestEsBuildMappings(t *testing.T) {
	schema := createESTestSchema()
	mapping := esBuildMappings(schema, "COSINE", nil)

	// 检查 dynamic: strict
	if mapping["dynamic"] != "strict" {
		t.Errorf("dynamic = %v, want strict", mapping["dynamic"])
	}

	// 检查 properties
	props, ok := mapping["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 应为 map[string]any")
	}

	// 检查 embedding 向量字段
	embField, ok := props["embedding"].(map[string]any)
	if !ok {
		t.Fatal("embedding 字段应为 map[string]any")
	}
	if embField["type"] != "dense_vector" {
		t.Errorf("embedding type = %v, want dense_vector", embField["type"])
	}
	if embField["similarity"] != "cosine" {
		t.Errorf("embedding similarity = %v, want cosine", embField["similarity"])
	}
	if embField["dims"] != 128 {
		t.Errorf("embedding dims = %v, want 128", embField["dims"])
	}

	// 检查 _meta 字段
	metaField, ok := props["_meta"].(map[string]any)
	if !ok {
		t.Fatal("_meta 字段应为 map[string]any")
	}
	if metaField["enabled"] != false {
		t.Errorf("_meta enabled = %v, want false", metaField["enabled"])
	}
}

// ─── esNormalizeScore 测试 ───

func TestEsNormalizeScore_COSINE(t *testing.T) {
	// COSINE 距离 0 → 相似度 1.0
	score := esNormalizeScore(0, "COSINE")
	if score != 1.0 {
		t.Errorf("esNormalizeScore(0, COSINE) = %v, want 1.0", score)
	}
}

func TestEsNormalizeScore_L2(t *testing.T) {
	score := esNormalizeScore(0, "L2")
	if score != 1.0 {
		t.Errorf("esNormalizeScore(0, L2) = %v, want 1.0", score)
	}
}

func TestEsNormalizeScore_IP(t *testing.T) {
	score := esNormalizeScore(1.0, "IP")
	if score != 1.0 {
		t.Errorf("esNormalizeScore(1.0, IP) = %v, want 1.0", score)
	}
}

func TestEsNormalizeScore_默认(t *testing.T) {
	// 未知度量应 fallback 为 COSINE
	score := esNormalizeScore(0, "UNKNOWN")
	if score != 1.0 {
		t.Errorf("esNormalizeScore(0, UNKNOWN) = %v, want 1.0 (fallback COSINE)", score)
	}
}

// ─── esBuildFilterClause 测试 ───

func TestEsBuildFilterClause_单值(t *testing.T) {
	clauses := esBuildFilterClause(map[string]any{"status": "active"})
	if len(clauses) != 1 {
		t.Fatalf("len(clauses) = %v, want 1", len(clauses))
	}
	termClause, ok := clauses[0].(map[string]any)
	if !ok {
		t.Fatal("子句应为 map[string]any")
	}
	term, ok := termClause["term"].(map[string]any)
	if !ok {
		t.Fatal("应有 term 子句")
	}
	if term["status"] != "active" {
		t.Errorf("term[status] = %v, want active", term["status"])
	}
}

func TestEsBuildFilterClause_多值(t *testing.T) {
	clauses := esBuildFilterClause(map[string]any{"status": []any{"active", "pending"}})
	if len(clauses) != 1 {
		t.Fatalf("len(clauses) = %v, want 1", len(clauses))
	}
	termsClause, ok := clauses[0].(map[string]any)
	if !ok {
		t.Fatal("子句应为 map[string]any")
	}
	if _, ok := termsClause["terms"]; !ok {
		t.Fatal("应有 terms 子句")
	}
}

func TestEsBuildFilterClause_字符串切片(t *testing.T) {
	clauses := esBuildFilterClause(map[string]any{"tag": []string{"a", "b"}})
	if len(clauses) != 1 {
		t.Fatalf("len(clauses) = %v, want 1", len(clauses))
	}
	termsClause, ok := clauses[0].(map[string]any)
	if !ok {
		t.Fatal("子句应为 map[string]any")
	}
	if _, ok := termsClause["terms"]; !ok {
		t.Fatal("应有 terms 子句")
	}
}

// ─── esBuildBulkRequestBody 测试 ───

func TestEsBuildBulkRequestBody(t *testing.T) {
	docs := []map[string]any{
		{"id": "doc1", "text": "hello"},
		{"id": "doc2", "text": "world"},
	}
	body := esBuildBulkRequestBody(docs, "test_index", "id")
	if body == nil {
		t.Fatal("body 不应为 nil")
	}

	// 读取并验证 NDJSON 格式
	b, _ := io.ReadAll(body)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 4 { // 2 docs × 2 lines (action + doc)
		t.Errorf("行数 = %v, want 4", len(lines))
	}

	// 验证第一行是 action
	var action map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("解析 action 行失败: %v", err)
	}
	indexAction, _ := action["index"].(map[string]any)
	if indexAction["_index"] != "test_index" {
		t.Errorf("action _index = %v, want test_index", indexAction["_index"])
	}
	if indexAction["_id"] != "doc1" {
		t.Errorf("action _id = %v, want doc1", indexAction["_id"])
	}
}

func TestEsBuildBulkRequestBody_过滤Nil值(t *testing.T) {
	docs := []map[string]any{
		{"id": "doc1", "text": nil, "embedding": []float64{0.1}},
	}
	body := esBuildBulkRequestBody(docs, "test_index", "")
	if body == nil {
		t.Fatal("body 不应为 nil")
	}

	b, _ := io.ReadAll(body)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// 文档行（第二行）不应包含 text 字段
	var doc map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &doc); err != nil {
		t.Fatalf("解析文档行失败: %v", err)
	}
	if _, ok := doc["text"]; ok {
		t.Error("nil 值的 text 字段应被过滤")
	}
}

func TestEsBuildBulkRequestBody_空文档(t *testing.T) {
	body := esBuildBulkRequestBody(nil, "test_index", "")
	if body != nil {
		t.Error("空文档应返回 nil")
	}
}

// ─── esBuildBulkDeleteBody 测试 ───

func TestEsBuildBulkDeleteBody(t *testing.T) {
	ids := []string{"id1", "id2"}
	body := esBuildBulkDeleteBody(ids, "test_index")
	if body == nil {
		t.Fatal("body 不应为 nil")
	}

	b, _ := io.ReadAll(body)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 { // 2 delete actions
		t.Errorf("行数 = %v, want 2", len(lines))
	}

	// 验证第一行是 delete action
	var action map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("解析 action 行失败: %v", err)
	}
	deleteAction, _ := action["delete"].(map[string]any)
	if deleteAction["_index"] != "test_index" {
		t.Errorf("action _index = %v, want test_index", deleteAction["_index"])
	}
	if deleteAction["_id"] != "id1" {
		t.Errorf("action _id = %v, want id1", deleteAction["_id"])
	}
}

func TestEsBuildBulkDeleteBody_空列表(t *testing.T) {
	body := esBuildBulkDeleteBody(nil, "test_index")
	if body != nil {
		t.Error("空列表应返回 nil")
	}
}

// ─── esGetPrimaryKeyField 测试 ───

func TestEsGetPrimaryKeyField(t *testing.T) {
	schemaDict := map[string]any{
		"fields": []any{
			map[string]any{"name": "id", "type": "VARCHAR", "is_primary": true},
			map[string]any{"name": "embedding", "type": "FLOAT_VECTOR", "dim": 128},
		},
	}
	pk := esGetPrimaryKeyField(schemaDict)
	if pk != "id" {
		t.Errorf("esGetPrimaryKeyField() = %v, want id", pk)
	}
}

func TestEsGetPrimaryKeyField_无主键(t *testing.T) {
	schemaDict := map[string]any{
		"fields": []any{
			map[string]any{"name": "embedding", "type": "FLOAT_VECTOR"},
		},
	}
	pk := esGetPrimaryKeyField(schemaDict)
	if pk != "" {
		t.Errorf("esGetPrimaryKeyField() = %v, want empty", pk)
	}
}

func TestEsGetPrimaryKeyField_无效Schema(t *testing.T) {
	pk := esGetPrimaryKeyField(nil)
	if pk != "" {
		t.Errorf("esGetPrimaryKeyField(nil) = %v, want empty", pk)
	}
}

// ─── esStoreMetadata / esLoadMetadata 测试 ───

func TestEsStoreAndLoadMetadata(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost || r.Method == http.MethodPut {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, `{"result":"created"}`)
				} else {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE","vector_field":"embedding","schema_version":0}}}`)
				}
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	c, _ := s.getClient()

	// 存储
	metadata := map[string]any{
		"distance_metric": "COSINE",
		"vector_field":    "embedding",
		"schema_version":  0,
	}
	err := esStoreMetadata(ctx, c, "agent_vector__test_coll", metadata)
	if err != nil {
		t.Fatalf("esStoreMetadata() error = %v", err)
	}

	// 加载（通过方法调用，先从 ES 读取并缓存）
	loaded, err := s.esLoadMetadata(ctx, c, "agent_vector__test_coll")
	if err != nil {
		t.Fatalf("esLoadMetadata() error = %v", err)
	}
	if loaded["distance_metric"] != "COSINE" {
		t.Errorf("distance_metric = %v, want COSINE", loaded["distance_metric"])
	}

	// 第二次加载应从缓存命中（不再发请求到 ES）
	loaded2, err := s.esLoadMetadata(ctx, c, "agent_vector__test_coll")
	if err != nil {
		t.Fatalf("esLoadMetadata() 第二次 error = %v", err)
	}
	if loaded2["distance_metric"] != "COSINE" {
		t.Errorf("缓存 distance_metric = %v, want COSINE", loaded2["distance_metric"])
	}
}

// ─── resolveESVectorField 测试 ───

func TestESVectorStore_resolveESVectorField_无配置(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	o := Options{}
	result := s.resolveESVectorField(o)
	if result != nil {
		t.Errorf("无配置时 resolveESVectorField = %v, want nil", result)
	}
}

func TestESVectorStore_resolveESVectorField_有配置(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	vf := vector_fields.NewESVectorField("embedding")
	vf.NumCandidates = 200
	vf.ExtraConstruct = map[string]any{"m": 16}
	vf.ExtraSearch = map[string]any{"ef_search": 100}
	o := Options{VectorField: vf}
	result := s.resolveESVectorField(o)
	if result == nil {
		t.Fatal("有配置时 resolveESVectorField 不应为 nil")
	}
	if result["field_name"] != "embedding" {
		t.Errorf("field_name = %v, want embedding", result["field_name"])
	}
	if result["num_candidates"] != 200 {
		t.Errorf("num_candidates = %v, want 200", result["num_candidates"])
	}
	if result["m"] != 16 {
		t.Errorf("m = %v, want 16", result["m"])
	}
	if result["ef_search"] != 100 {
		t.Errorf("ef_search = %v, want 100", result["ef_search"])
	}
}

func TestESVectorStore_resolveESVectorField_非ES类型(t *testing.T) {
	s := NewESVectorStore([]string{"http://localhost:9200"}, "", "")
	o := Options{VectorField: "not_an_es_field"}
	result := s.resolveESVectorField(o)
	if result != nil {
		t.Errorf("非 ES 类型时 resolveESVectorField = %v, want nil", result)
	}
}

// ─── GetSchema 测试 ───

func TestESVectorStore_GetSchema_从Meta获取(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"schema":{"fields":[{"name":"id","type":"VARCHAR","is_primary":true},{"name":"embedding","type":"FLOAT_VECTOR","dim":128}]},"distance_metric":"COSINE"}}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	schema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if schema == nil {
		t.Fatal("GetSchema() 返回 nil")
	}
	vf := schema.GetVectorFields()
	if len(vf) != 1 {
		t.Errorf("向量字段数 = %v, want 1", len(vf))
	}
	if vf[0].Name != "embedding" {
		t.Errorf("向量字段名 = %v, want embedding", vf[0].Name)
	}
}

func TestESVectorStore_GetSchema_从Mapping反射(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				// _meta 文档不存在，回退到 mapping
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"found":false}`)
			},
			"mapping": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"agent_vector__test_coll": {
						"mappings": {
							"properties": {
								"id": {"type": "keyword"},
								"embedding": {"type": "dense_vector", "dims": 64},
								"_meta": {"type": "object", "enabled": false}
							}
						}
					}
				}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	schema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if schema == nil {
		t.Fatal("GetSchema() 返回 nil")
	}
	fields := schema.Fields()
	if len(fields) != 2 {
		t.Errorf("字段数 = %v, want 2（排除 _meta）", len(fields))
	}
}

// ─── Search _id 回填测试 ───

func TestESVectorStore_Search_ID回填(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE"}}}`)
			},
			"_search": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"hits": {
						"hits": [
							{"_id":"auto_id_1","_score":0.9,"_source":{"text":"hello"}}
						]
					}
				}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("结果数 = %v, want 1", len(results))
	}
	// _id 应回填到 id 字段
	if results[0].Fields["id"] != "auto_id_1" {
		t.Errorf("id = %v, want auto_id_1", results[0].Fields["id"])
	}
}

// ─── Search OutputFields 测试 ───

func TestESVectorStore_Search_OutputFields(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE"}}}`)
			},
			"_search": func(w http.ResponseWriter, r *http.Request) {
				// 验证请求体包含 _source includes
				body, _ := io.ReadAll(r.Body)
				var reqBody map[string]any
				_ = json.Unmarshal(body, &reqBody)

				source, _ := reqBody["_source"].(map[string]any)
				if source == nil {
					t.Error("_source 应存在")
				} else {
					includes, _ := source["includes"].([]any)
					if len(includes) == 0 {
						t.Error("_source 应包含 includes 字段")
					}
					// 验证 excludes 包含 _meta
					excludes, _ := source["excludes"].([]any)
					hasMeta := false
					for _, e := range excludes {
						if e == "_meta" {
							hasMeta = true
						}
					}
					if !hasMeta {
						t.Error("_source.excludes 应包含 _meta")
					}
				}

				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"hits":{"hits":[]}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	_, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil,
		WithOutputFields("text", "id"))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

// ─── fakeESClient ───

// fakeESClient 基于 httptest.Server 的 ES 客户端 mock。
// 实现了 esClient 接口，通过 Transport 方式转发请求到 httptest mock 服务器。
type fakeESClient struct {
	server *httptest.Server
}

func newFakeESClient(server *httptest.Server) *fakeESClient {
	return &fakeESClient{server: server}
}

// Do 实现 esClient 接口，将 esapi.Request 通过 Transport 转发到 httptest 服务器
func (c *fakeESClient) Do(ctx context.Context, req esapi.Request) (*esapi.Response, error) {
	resp, err := req.Do(ctx, c)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Close 实现 esClient 接口
func (c *fakeESClient) Close() {
	c.server.Close()
}

// Perform 实现 esapi.Transport 接口，将 HTTP 请求转发到 httptest 服务器
func (c *fakeESClient) Perform(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = c.server.Listener.Addr().String()
	return c.server.Client().Do(req)
}

// ─── 错误路径测试 ───

func TestESVectorStore_DeleteCollection_ES错误(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK) // 索引存在
			},
			"DELETE": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"internal server error"}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteCollection(ctx, "test_coll")
	if err == nil {
		t.Error("ES 返回错误时应返回 error")
	}
}

func TestESVectorStore_CollectionExists_异常状态(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	_, err := s.CollectionExists(ctx, "test_coll")
	if err == nil {
		t.Error("异常状态码应返回错误")
	}
}

func TestESVectorStore_AddDocs_批量部分失败(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"found":false}`)
				}
			},
			"_bulk": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"errors":true,"items":[{"index":{"status":400,"error":"bad request"}}]}`)
			},
			"_refresh": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_shards":{"total":1,"successful":1,"failed":0}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "doc1", "embedding": []float64{0.1, 0.2, 0.3}},
	}
	// 部分失败不会返回 error，只记录 Warn 日志
	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs 部分失败不返回 error, got: %v", err)
	}
}

func TestESVectorStore_DeleteDocsByIDs_批量部分失败(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"_bulk": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"errors":true,"items":[{"delete":{"status":404}}]}`)
			},
			"_refresh": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_shards":{"total":1,"successful":1,"failed":0}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteDocsByIDs(ctx, "test_coll", []string{"id1"})
	if err != nil {
		t.Fatalf("DeleteDocsByIDs 部分失败不返回 error, got: %v", err)
	}
}

func TestESVectorStore_DeleteDocsByFilters_ES错误(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"_delete_by_query": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"internal"}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.DeleteDocsByFilters(ctx, "test_coll", map[string]any{"status": "active"})
	if err == nil {
		t.Error("ES 错误应返回 error")
	}
}

func TestESVectorStore_ListCollectionNames_ES错误(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"GET": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"internal"}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	_, err := s.ListCollectionNames(ctx)
	if err == nil {
		t.Error("ES 错误应返回 error")
	}
}

func TestESVectorStore_ListCollectionNames_404(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"GET": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("404 不应返回错误, error = %v", err)
	}
	if len(names) != 0 {
		t.Errorf("404 应返回空列表, got %v", names)
	}
}

func TestESVectorStore_UpdateCollectionMetadata_ES存储失败(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"found":false}`)
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, `{"error":"internal"}`)
				}
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	err := s.UpdateCollectionMetadata(ctx, "test_coll", map[string]any{
		"distance_metric": "L2",
		"schema_version":  1,
	})
	if err == nil {
		t.Error("ES 存储失败应返回 error")
	}
}

func TestESVectorStore_GetCollectionMetadata_ES错误(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"internal"}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	_, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err == nil {
		t.Error("ES 错误应返回 error")
	}
}

func TestESVectorStore_CreateCollection_ES创建失败(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"HEAD": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			"PUT ": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":{"type":"mapper_parsing_exception"}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	schema := createESTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err == nil {
		t.Error("ES 创建失败应返回 error")
	}
}

func TestESVectorStore_AddDocs_刷新失败(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"found":false}`)
				}
			},
			"_bulk": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"errors":false,"items":[{"index":{"status":201}}]}`)
			},
			"_refresh": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"refresh failed"}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "doc1", "embedding": []float64{0.1, 0.2}},
	}
	// 刷新失败是非致命错误
	err := s.AddDocs(ctx, "test_coll", docs)
	if err != nil {
		t.Fatalf("刷新失败不应返回 error, got: %v", err)
	}
}

func TestESVectorStore_Search_默认TopK(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"COSINE"}}}`)
			},
			"_search": func(w http.ResponseWriter, r *http.Request) {
				// 验证请求体中 topK 默认值
				body, _ := io.ReadAll(r.Body)
				var reqBody map[string]any
				_ = json.Unmarshal(body, &reqBody)

				knn, _ := reqBody["knn"].(map[string]any)
				if knn["k"] != float64(5) {
					t.Errorf("默认 topK = %v, want 5", knn["k"])
				}
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"hits":{"hits":[]}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	_, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 0, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestESVectorStore_Search_L2距离(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{"distance_metric":"L2"}}}`)
			},
			"_search": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"hits":{"hits":[{"_id":"1","_score":1.5,"_source":{"text":"hello"}}]}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	results, err := s.Search(ctx, "test_coll", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("结果数 = %v, want 1", len(results))
	}
	// ES k-NN 的 _score 已经是归一化的相似度分数，直接返回原始值
	// _score = 1.5 不是 ES k-NN 的合法值（应为 (0,1]），
	// 但此处验证 Go 不再做额外归一化，直接返回原始 _score
	if results[0].Score != 1.5 {
		t.Errorf("L2 分数 = %v, want 1.5（ES 原始 _score，不做归一化）", results[0].Score)
	}
}

func TestESVectorStore_AddDocs_自定义BatchSize(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"found":false}`)
				}
			},
			"_bulk": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"errors":false,"items":[{"index":{"status":201}}]}`)
			},
			"_refresh": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_shards":{"total":1,"successful":1,"failed":0}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	docs := []map[string]any{
		{"id": "doc1", "embedding": []float64{0.1}},
		{"id": "doc2", "embedding": []float64{0.2}},
		{"id": "doc3", "embedding": []float64{0.3}},
	}

	err := s.AddDocs(ctx, "test_coll", docs, WithBatchSize(2))
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}
}

func TestESVectorStore_GetSchema_Meta解析失败回退(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				// _meta 文档存在但 schema 字段无效
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{"_meta":{}}}`)
			},
			"mapping": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"agent_vector__test_coll": {
						"mappings": {
							"properties": {
								"id": {"type": "keyword"},
								"embedding": {"type": "dense_vector", "dims": 128}
							}
						}
					}
				}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	schema, err := s.GetSchema(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if schema == nil {
		t.Fatal("GetSchema() 应从 mapping 回退获取")
	}
}

func TestEsStoreMetadata_序列化失败(t *testing.T) {
	// 用一个无法序列化的值触发 json.Marshal 错误
	metadata := map[string]any{
		"bad": make(chan int),
	}
	err := esStoreMetadata(context.Background(), nil, "test_index", metadata)
	if err == nil {
		t.Error("序列化失败应返回 error")
	}
}

func TestEsLoadMetadata_源数据缺失(t *testing.T) {
	handler := &esMockHandler{
		responses: map[string]func(w http.ResponseWriter, r *http.Request){
			"__collection_metadata__": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"_source":{}}`)
			},
		},
	}
	s := newTestESVectorStore(handler)
	defer s.Close()

	ctx := context.Background()
	c, _ := s.getClient()
	meta, err := s.esLoadMetadata(ctx, c, "test_index")
	if err != nil {
		t.Fatalf("esLoadMetadata() error = %v", err)
	}
	if len(meta) != 0 {
		t.Errorf("空 _source._meta 应返回空 map, got %v", meta)
	}
}

func TestEsBuildMappings_L2距离(t *testing.T) {
	schema := createESTestSchema()
	mapping := esBuildMappings(schema, "L2", nil)

	props, _ := mapping["properties"].(map[string]any)
	embField, _ := props["embedding"].(map[string]any)
	if embField["similarity"] != "l2_norm" {
		t.Errorf("L2 similarity = %v, want l2_norm", embField["similarity"])
	}
}

func TestEsBuildMappings_IP距离(t *testing.T) {
	schema := createESTestSchema()
	mapping := esBuildMappings(schema, "IP", nil)

	props, _ := mapping["properties"].(map[string]any)
	embField, _ := props["embedding"].(map[string]any)
	if embField["similarity"] != "dot_product" {
		t.Errorf("IP similarity = %v, want dot_product", embField["similarity"])
	}
}

func TestEsBuildMappings_带ESVectorField配置(t *testing.T) {
	schema := createESTestSchema()
	vf := vector_fields.NewESVectorField("embedding")
	vf.NumCandidates = 200
	vf.ExtraConstruct = map[string]any{"m": 16, "ef_construction": 200}
	config := map[string]any{
		"field_name":      "embedding",
		"num_candidates":  200,
		"m":               16,
		"ef_construction": 200,
	}
	mapping := esBuildMappings(schema, "COSINE", config)

	props, _ := mapping["properties"].(map[string]any)
	embField, _ := props["embedding"].(map[string]any)

	// 验证 construct 阶段参数已写入 mapping
	if embField["m"] != 16 {
		t.Errorf("embedding m = %v, want 16", embField["m"])
	}
	if embField["ef_construction"] != 200 {
		t.Errorf("embedding ef_construction = %v, want 200", embField["ef_construction"])
	}
	// num_candidates 和 field_name 不应出现在 mapping 中
	if _, ok := embField["num_candidates"]; ok {
		t.Error("num_candidates 不应出现在 mapping 中")
	}
	if _, ok := embField["field_name"]; ok {
		t.Error("field_name 不应出现在 mapping 中")
	}
}
