package index

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeKVStore 模拟 KV 存储，基于 map + RWMutex 实现
type fakeKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// fakeVectorStore 模拟向量存储，基于 collections map 实现
type fakeVectorStore struct {
	mu          sync.RWMutex
	collections map[string][]map[string]any
	schemas     map[string]*vector.CollectionSchema
}

// fakeEmbedding 模拟嵌入模型，生成简单的确定性向量
type fakeEmbedding struct {
	dim int
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时接口检查
var (
	_ kv.BaseKVStore       = &fakeKVStore{}
	_ vector.BaseVectorStore = &fakeVectorStore{}
	_ embedding.BaseEmbedding = &fakeEmbedding{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFakeKVStore 创建模拟 KV 存储
func newFakeKVStore() *fakeKVStore {
	return &fakeKVStore{data: make(map[string][]byte)}
}

func (f *fakeKVStore) Set(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *fakeKVStore) ExclusiveSet(_ context.Context, key string, value []byte, _ int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; ok {
		return false, nil
	}
	f.data[key] = value
	return true, nil
}

func (f *fakeKVStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.data[key], nil
}

func (f *fakeKVStore) Exists(_ context.Context, key string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.data[key]
	return ok, nil
}

func (f *fakeKVStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

func (f *fakeKVStore) GetByPrefix(_ context.Context, prefix string) (map[string][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make(map[string][]byte)
	for k, v := range f.data {
		if strings.HasPrefix(k, prefix) {
			result[k] = v
		}
	}
	return result, nil
}

func (f *fakeKVStore) DeleteByPrefix(_ context.Context, prefix string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			delete(f.data, k)
		}
	}
	return nil
}

func (f *fakeKVStore) MGet(_ context.Context, keys []string) ([][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([][]byte, len(keys))
	for i, k := range keys {
		result[i] = f.data[k]
	}
	return result, nil
}

func (f *fakeKVStore) BatchDelete(_ context.Context, keys []string, _ int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, k := range keys {
		if _, ok := f.data[k]; ok {
			delete(f.data, k)
			count++
		}
	}
	return count, nil
}

func (f *fakeKVStore) Pipeline(_ context.Context) kv.KVPipeline {
	return nil
}

// newFakeVectorStore 创建模拟向量存储
func newFakeVectorStore() *fakeVectorStore {
	return &fakeVectorStore{
		collections: make(map[string][]map[string]any),
		schemas:     make(map[string]*vector.CollectionSchema),
	}
}

func (f *fakeVectorStore) CreateCollection(_ context.Context, name string, schema *vector.CollectionSchema, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.collections[name]; ok {
		return fmt.Errorf("集合 %s 已存在", name)
	}
	f.collections[name] = nil
	f.schemas[name] = schema
	return nil
}

func (f *fakeVectorStore) DeleteCollection(_ context.Context, name string, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.collections, name)
	delete(f.schemas, name)
	return nil
}

func (f *fakeVectorStore) CollectionExists(_ context.Context, name string, _ ...vector.Option) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.collections[name]
	return ok, nil
}

func (f *fakeVectorStore) GetSchema(_ context.Context, name string, _ ...vector.Option) (*vector.CollectionSchema, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.schemas[name], nil
}

func (f *fakeVectorStore) AddDocs(_ context.Context, collectionName string, docs []map[string]any, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.collections[collectionName] = append(f.collections[collectionName], docs...)
	return nil
}

// Search 返回集合中所有文档，按分数降序排列（0.9, 0.8, ...）
func (f *fakeVectorStore) Search(_ context.Context, collectionName string, _ []float64, _ string, topK int, _ map[string]any, _ ...vector.Option) ([]vector.VectorSearchResult, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	docs, ok := f.collections[collectionName]
	if !ok {
		return nil, nil
	}

	var results []vector.VectorSearchResult
	for i, doc := range docs {
		score := 0.9 - float64(i)*0.1
		if score < 0.1 {
			score = 0.1
		}
		results = append(results, vector.VectorSearchResult{
			Score:  score,
			Fields: doc,
		})
	}

	// 按分数降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

func (f *fakeVectorStore) DeleteDocsByIDs(_ context.Context, collectionName string, ids []string, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	docs, ok := f.collections[collectionName]
	if !ok {
		return nil
	}
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	var filtered []map[string]any
	for _, doc := range docs {
		if id, _ := doc["id"].(string); !idSet[id] {
			filtered = append(filtered, doc)
		}
	}
	f.collections[collectionName] = filtered
	return nil
}

func (f *fakeVectorStore) DeleteDocsByFilters(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) ListCollectionNames(_ context.Context) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	names := make([]string, 0, len(f.collections))
	for name := range f.collections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (f *fakeVectorStore) UpdateSchema(_ context.Context, _ string, _ []any, _ ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) UpdateCollectionMetadata(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) GetCollectionMetadata(_ context.Context, _ string, _ ...vector.Option) (map[string]any, error) {
	return nil, nil
}

// newFakeEmbedding 创建模拟嵌入模型
func newFakeEmbedding() *fakeEmbedding {
	return &fakeEmbedding{dim: 4}
}

func (f *fakeEmbedding) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	vec := make([]float64, f.dim)
	for i := range vec {
		vec[i] = float64(i+1) * 0.1
	}
	// 根据文本内容微调，使不同文本产生不同向量
	if len(text) > 0 {
		vec[0] += float64(text[0]) * 0.001
	}
	return vec, nil
}

func (f *fakeEmbedding) EmbedDocuments(_ context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec, _ := f.EmbedQuery(context.Background(), text)
		// 为每个文档添加偏移，使向量不同
		vec[0] += float64(i) * 0.01
		result[i] = vec
	}
	return result, nil
}

func (f *fakeEmbedding) Dimension() int {
	return f.dim
}

// newTestIndex 创建用于测试的 SimpleMemoryIndex
func newTestIndex() *SimpleMemoryIndex {
	return NewSimpleMemoryIndex(newFakeKVStore(), newFakeVectorStore(), newFakeEmbedding())
}

// newTestIndexWithNilEmbedding 创建嵌入模型为 nil 的 SimpleMemoryIndex
func newTestIndexWithNilEmbedding() *SimpleMemoryIndex {
	return NewSimpleMemoryIndex(newFakeKVStore(), newFakeVectorStore(), nil)
}

// padID 将 ID 填充或截断到 byteNumPerID(24) 字符长度
func padID(id string) string {
	if len(id) >= byteNumPerID {
		return id[:byteNumPerID]
	}
	return id + strings.Repeat("0", byteNumPerID-len(id))
}

// ──────────────────────────── NewSimpleMemoryIndex 测试 ────────────────────────────

func TestNewSimpleMemoryIndex(t *testing.T) {
	kvs := newFakeKVStore()
	vs := newFakeVectorStore()
	emb := newFakeEmbedding()

	idx := NewSimpleMemoryIndex(kvs, vs, emb)

	if idx == nil {
		t.Fatal("NewSimpleMemoryIndex 返回 nil")
	}
	if idx.kvStore != kvs {
		t.Error("kvStore 未正确赋值")
	}
	if idx.vectorStore != vs {
		t.Error("vectorStore 未正确赋值")
	}
	if idx.embeddingModel != emb {
		t.Error("embeddingModel 未正确赋值")
	}
	if idx.createdCollections == nil {
		t.Error("createdCollections 未初始化")
	}
}

// ──────────────────────────── SetEmbeddingModel 测试 ────────────────────────────

func TestSetEmbeddingModel(t *testing.T) {
	idx := newTestIndexWithNilEmbedding()

	if idx.embeddingModel != nil {
		t.Fatal("初始嵌入模型应为 nil")
	}

	emb := newFakeEmbedding()
	idx.SetEmbeddingModel(emb)

	if idx.embeddingModel != emb {
		t.Error("SetEmbeddingModel 未正确设置嵌入模型")
	}
}

// ──────────────────────────── SetStorageCodec 测试 ────────────────────────────

func TestSetStorageCodec(t *testing.T) {
	idx := newTestIndex()

	if idx.codec != nil {
		t.Fatal("初始编解码器应为 nil")
	}

	codec := newFakeCodec()
	idx.SetStorageCodec(codec)

	if idx.codec != codec {
		t.Error("SetStorageCodec 未正确设置编解码器")
	}
}

// ──────────────────────────── KV 辅助函数测试 ────────────────────────────

func TestKVHelper_KVMemKey(t *testing.T) {
	got := kvMemKey("user1", "scope1", "mem1")
	want := "UMD/user1/scope1/mem1"
	if got != want {
		t.Errorf("kvMemKey: 期望 %q, 实际 %q", want, got)
	}
}

func TestKVHelper_KVIDsKey(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		scopeID  string
		memType  string
		expected string
	}{
		{
			name:     "无 memType 时生成全局追踪键",
			userID:   "user1",
			scopeID:  "scope1",
			memType:  "",
			expected: "UMD/user1/scope1/ids",
		},
		{
			name:     "有 memType 时生成按类型追踪键",
			userID:   "user1",
			scopeID:  "scope1",
			memType:  "observation",
			expected: "UMD/user1/scope1/observation/ids",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kvIDsKey(tt.userID, tt.scopeID, tt.memType)
			if got != tt.expected {
				t.Errorf("kvIDsKey: 期望 %q, 实际 %q", tt.expected, got)
			}
		})
	}
}

func TestKVHelper_ParseAllIDs(t *testing.T) {
	id1 := padID("abc")
	id2 := padID("def")
	raw := id1 + id2

	ids := parseAllIDs(raw)
	if len(ids) != 2 {
		t.Fatalf("期望 2 个 ID, 实际 %d", len(ids))
	}
	if ids[0] != id1 {
		t.Errorf("第 1 个 ID: 期望 %q, 实际 %q", id1, ids[0])
	}
	if ids[1] != id2 {
		t.Errorf("第 2 个 ID: 期望 %q, 实际 %q", id2, ids[1])
	}
}

func TestKVHelper_ParseAllIDs_空字符串(t *testing.T) {
	ids := parseAllIDs("")
	if len(ids) != 0 {
		t.Errorf("空字符串应返回空切片, 实际长度 %d", len(ids))
	}
}

func TestKVHelper_AppendID(t *testing.T) {
	id1 := padID("abc")
	id2 := padID("def")

	result := appendID(id1, id2)
	if result != id1+id2 {
		t.Errorf("appendID: 期望 %q, 实际 %q", id1+id2, result)
	}
}

func TestKVHelper_RemoveID(t *testing.T) {
	id1 := padID("abc")
	id2 := padID("def")
	raw := id1 + id2

	result := removeID(raw, id1)
	if result != id2 {
		t.Errorf("removeID: 期望 %q, 实际 %q", id2, result)
	}
}

func TestKVHelper_RemoveID_不存在(t *testing.T) {
	id1 := padID("abc")
	id2 := padID("def")
	raw := id1 + id2
	unknown := padID("xyz")

	result := removeID(raw, unknown)
	if result != raw {
		t.Errorf("移除不存在的 ID 时应返回原字符串, 期望 %q, 实际 %q", raw, result)
	}
}

func TestKVHelper_KVDataToMemoryDoc_多种时间戳格式(t *testing.T) {
	tests := []struct {
		name       string
		data       map[string]any
		wantMem    string
		wantType   string
		checkTime  func(t *testing.T, ts time.Time)
	}{
		{
			name: "旧格式时间戳（连字符分隔时分秒）",
			data: map[string]any{
				"mem":       "旧格式记忆",
				"mem_type":  "test_old",
				"timestamp": "2025-01-15 10-30-00",
			},
			wantMem:  "旧格式记忆",
			wantType: "test_old",
			checkTime: func(t *testing.T, ts time.Time) {
				t.Helper()
				want := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
				if !ts.Equal(want) {
					t.Errorf("时间戳: 期望 %v, 实际 %v", want, ts)
				}
			},
		},
		{
			name: "标准格式时间戳（冒号分隔时分秒）",
			data: map[string]any{
				"mem":       "标准格式记忆",
				"mem_type":  "test_std",
				"timestamp": "2025-03-20 14:45:30",
			},
			wantMem:  "标准格式记忆",
			wantType: "test_std",
			checkTime: func(t *testing.T, ts time.Time) {
				t.Helper()
				want := time.Date(2025, 3, 20, 14, 45, 30, 0, time.UTC)
				if !ts.Equal(want) {
					t.Errorf("时间戳: 期望 %v, 实际 %v", want, ts)
				}
			},
		},
		{
			name: "无时间戳时使用当前 UTC 时间",
			data: map[string]any{
				"mem":      "无时间戳记忆",
				"mem_type": "test_no_ts",
			},
			wantMem:  "无时间戳记忆",
			wantType: "test_no_ts",
			checkTime: func(t *testing.T, ts time.Time) {
				t.Helper()
				if ts.IsZero() {
					t.Error("时间戳不应为零值")
				}
			},
		},
		{
			name: "额外字段保留到 Fields",
			data: map[string]any{
				"mem":       "带额外字段",
				"mem_type":  "test_extra",
				"timestamp": "2025-06-01 08-00-00",
				"priority":  "high",
				"source":    "api",
			},
			wantMem:  "带额外字段",
			wantType: "test_extra",
			checkTime: func(t *testing.T, ts time.Time) {
				t.Helper()
				want := time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC)
				if !ts.Equal(want) {
					t.Errorf("时间戳: 期望 %v, 实际 %v", want, ts)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := kvDataToMemoryDoc(tt.data, "mem-001")
			if doc.ID != "mem-001" {
				t.Errorf("ID: 期望 mem-001, 实际 %q", doc.ID)
			}
			if doc.Text != tt.wantMem {
				t.Errorf("Text: 期望 %q, 实际 %q", tt.wantMem, doc.Text)
			}
			if doc.Type != tt.wantType {
				t.Errorf("Type: 期望 %q, 实际 %q", tt.wantType, doc.Type)
			}
			tt.checkTime(t, doc.Timestamp)
		})
	}
}

func TestKVHelper_MemoryDocToKVData(t *testing.T) {
	ts := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC)
	doc := &MemoryDoc{
		ID:        "mem-001",
		Text:      "测试记忆",
		Type:      "observation",
		Timestamp: ts,
		Fields:    map[string]any{"priority": "high"},
	}

	data := memoryDocToKVData(doc, "user1", "scope1")

	if data["id"] != "mem-001" {
		t.Errorf("id: 期望 mem-001, 实际 %v", data["id"])
	}
	if data["user_id"] != "user1" {
		t.Errorf("user_id: 期望 user1, 实际 %v", data["user_id"])
	}
	if data["scope_id"] != "scope1" {
		t.Errorf("scope_id: 期望 scope1, 实际 %v", data["scope_id"])
	}
	if data["mem"] != "测试记忆" {
		t.Errorf("mem: 期望 测试记忆, 实际 %v", data["mem"])
	}
	if data["mem_type"] != "observation" {
		t.Errorf("mem_type: 期望 observation, 实际 %v", data["mem_type"])
	}
	if data["timestamp"] != "2025-07-25 10-30-00" {
		t.Errorf("timestamp: 期望 2025-07-25 10-30-00, 实际 %v", data["timestamp"])
	}
	if data["priority"] != "high" {
		t.Errorf("priority: 期望 high, 实际 %v", data["priority"])
	}
}

// ──────────────────────────── 集合名称辅助函数测试 ────────────────────────────

func TestGetCollectionName(t *testing.T) {
	got := getCollectionName("user1", "scope1", "observation")
	want := "uid_user1_gid_scope1_mtype_observation"
	if got != want {
		t.Errorf("getCollectionName: 期望 %q, 实际 %q", want, got)
	}
}

func TestParseMemTypeFromCollection(t *testing.T) {
	got := parseMemTypeFromCollection("uid_user1_gid_scope1_mtype_observation")
	want := "observation"
	if got != want {
		t.Errorf("parseMemTypeFromCollection: 期望 %q, 实际 %q", want, got)
	}
}

// ──────────────────────────── ensureCollection 测试 ────────────────────────────

func TestEnsureCollection_懒创建(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()
	colName := getCollectionName("user1", "scope1", "observation")

	// 第一次调用应创建集合
	if err := idx.ensureCollection(ctx, colName, 4); err != nil {
		t.Fatalf("第一次 ensureCollection 失败: %v", err)
	}

	exists, err := idx.vectorStore.CollectionExists(ctx, colName)
	if err != nil {
		t.Fatalf("检查集合存在失败: %v", err)
	}
	if !exists {
		t.Error("集合应已创建")
	}

	// 第二次调用应跳过（懒创建，已缓存）
	if err := idx.ensureCollection(ctx, colName, 4); err != nil {
		t.Fatalf("第二次 ensureCollection 失败: %v", err)
	}
}

// ──────────────────────────── readKVValue 测试 ────────────────────────────

func TestReadKVValue(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "正常字节数据",
			input: []byte("hello"),
			want:  "hello",
		},
		{
			name:  "nil 输入返回空字符串",
			input: nil,
			want:  "",
		},
		{
			name:  "空字节数据",
			input: []byte{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readKVValue(tt.input)
			if got != tt.want {
				t.Errorf("readKVValue: 期望 %q, 实际 %q", tt.want, got)
			}
		})
	}
}

// ──────────────────────────── AddMemories 测试 ────────────────────────────

func TestAddMemories_正常添加(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{
			ID:        memID,
			Text:      "用户偏好深色模式",
			Type:      "observation",
			Timestamp: time.Date(2025, 7, 25, 10, 0, 0, 0, time.UTC),
		},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err != nil {
		t.Fatalf("AddMemories 失败: %v", err)
	}

	// 验证 KV 存储中存在
	raw, err := idx.kvStore.Get(ctx, kvMemKey("user1", "scope1", memID))
	if err != nil {
		t.Fatalf("获取 KV 数据失败: %v", err)
	}
	if raw == nil {
		t.Fatal("KV 数据不应为 nil")
	}

	// 验证 ID 追踪
	idsRaw, _ := idx.kvStore.Get(ctx, kvIDsKey("user1", "scope1", ""))
	idsVal := readKVValue(idsRaw)
	ids := parseAllIDs(idsVal)
	found := false
	for _, id := range ids {
		if id == memID {
			found = true
			break
		}
	}
	if !found {
		t.Error("ID 追踪中应包含添加的 ID")
	}
}

func TestAddMemories_多类型分组(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	docs := []*MemoryDoc{
		{ID: memID1, Text: "观察记录", Type: "observation", Timestamp: time.Now().UTC()},
		{ID: memID2, Text: "用户画像", Type: "user_profile", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err != nil {
		t.Fatalf("AddMemories 失败: %v", err)
	}

	// 两个不同类型应创建两个集合
	col1 := getCollectionName("user1", "scope1", "observation")
	col2 := getCollectionName("user1", "scope1", "user_profile")

	exists1, _ := idx.vectorStore.CollectionExists(ctx, col1)
	exists2, _ := idx.vectorStore.CollectionExists(ctx, col2)

	if !exists1 {
		t.Error("observation 集合应已创建")
	}
	if !exists2 {
		t.Error("user_profile 集合应已创建")
	}
}

func TestAddMemories_EmbeddingModel为nil时返回错误(t *testing.T) {
	idx := newTestIndexWithNilEmbedding()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "测试", Type: "observation", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err == nil {
		t.Fatal("嵌入模型为 nil 时应返回错误")
	}
}

func TestAddMemories_空列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	err := idx.AddMemories(ctx, "user1", "scope1", nil)
	if err != nil {
		t.Errorf("空列表应返回 nil, 实际 %v", err)
	}

	err = idx.AddMemories(ctx, "user1", "scope1", []*MemoryDoc{})
	if err != nil {
		t.Errorf("空切片应返回 nil, 实际 %v", err)
	}
}

// ──────────────────────────── Search 测试 ────────────────────────────

func TestSearch_正常搜索(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "用户喜欢深色主题", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	results, err := idx.Search(ctx, "user1", "scope1", "深色", nil, 5)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("搜索应返回结果")
	}
	if results[0].Doc.ID != memID {
		t.Errorf("搜索结果 ID: 期望 %q, 实际 %q", memID, results[0].Doc.ID)
	}
}

func TestSearch_指定memTypes(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	docs := []*MemoryDoc{
		{ID: memID1, Text: "观察记录", Type: "observation", Timestamp: time.Now().UTC()},
		{ID: memID2, Text: "用户画像", Type: "user_profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 只搜索 observation 类型
	results, err := idx.Search(ctx, "user1", "scope1", "观察", []string{"observation"}, 5)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("搜索应返回结果")
	}
	for _, r := range results {
		if r.Doc.Type != "observation" {
			t.Errorf("搜索结果类型应为 observation, 实际 %q", r.Doc.Type)
		}
	}
}

func TestSearch_EmbeddingModel为nil时返回空(t *testing.T) {
	idx := newTestIndexWithNilEmbedding()
	ctx := context.Background()

	results, err := idx.Search(ctx, "user1", "scope1", "查询", nil, 5)
	if err != nil {
		t.Fatalf("嵌入模型为 nil 时应返回 nil, nil, 实际 err=%v", err)
	}
	if results != nil {
		t.Errorf("嵌入模型为 nil 时应返回 nil 结果, 实际 %v", results)
	}
}

func TestSearch_StorageCodec解码(t *testing.T) {
	idx := newTestIndex()
	codec := newFakeCodec()
	idx.SetStorageCodec(codec)
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "敏感数据", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	results, err := idx.Search(ctx, "user1", "scope1", "敏感", nil, 5)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("搜索应返回结果")
	}
	// 解码后应还原为原始文本
	if results[0].Doc.Text != "敏感数据" {
		t.Errorf("解码后文本: 期望 %q, 实际 %q", "敏感数据", results[0].Doc.Text)
	}
}

// ──────────────────────────── UpdateMemories 测试 ────────────────────────────

func TestUpdateMemories(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "原始记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 更新记忆
	updated := []*MemoryDoc{
		{ID: memID, Text: "更新后的记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	err := idx.UpdateMemories(ctx, "user1", "scope1", updated)
	if err != nil {
		t.Fatalf("UpdateMemories 失败: %v", err)
	}

	doc, err := idx.GetByID(ctx, "user1", "scope1", memID)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if doc.Text != "更新后的记忆" {
		t.Errorf("更新后文本: 期望 %q, 实际 %q", "更新后的记忆", doc.Text)
	}
}

func TestUpdateMemories_空列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	err := idx.UpdateMemories(ctx, "user1", "scope1", nil)
	if err != nil {
		t.Errorf("空列表应返回 nil, 实际 %v", err)
	}

	err = idx.UpdateMemories(ctx, "user1", "scope1", []*MemoryDoc{})
	if err != nil {
		t.Errorf("空切片应返回 nil, 实际 %v", err)
	}
}

// ──────────────────────────── DeleteMemories 测试 ────────────────────────────

func TestDeleteMemories_正常删除(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "待删除记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteMemories(ctx, "user1", "scope1", []string{memID})
	if err != nil {
		t.Fatalf("DeleteMemories 失败: %v", err)
	}

	// 验证 KV 中已删除
	raw, _ := idx.kvStore.Get(ctx, kvMemKey("user1", "scope1", memID))
	if raw != nil {
		t.Error("删除后 KV 数据应为 nil")
	}
}

func TestDeleteMemories_ID追踪清空时删除键(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "唯一记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 删除唯一的记忆
	err := idx.DeleteMemories(ctx, "user1", "scope1", []string{memID})
	if err != nil {
		t.Fatalf("DeleteMemories 失败: %v", err)
	}

	// ID 追踪键应被删除（清空时删除键）
	idsRaw, _ := idx.kvStore.Get(ctx, kvIDsKey("user1", "scope1", ""))
	if idsRaw != nil {
		t.Error("全局 ID 追踪键清空后应被删除")
	}

	typeIDsRaw, _ := idx.kvStore.Get(ctx, kvIDsKey("user1", "scope1", "observation"))
	if typeIDsRaw != nil {
		t.Error("类型 ID 追踪键清空后应被删除")
	}
}

func TestDeleteMemories_空列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	err := idx.DeleteMemories(ctx, "user1", "scope1", nil)
	if err != nil {
		t.Errorf("空列表应返回 nil, 实际 %v", err)
	}

	err = idx.DeleteMemories(ctx, "user1", "scope1", []string{})
	if err != nil {
		t.Errorf("空切片应返回 nil, 实际 %v", err)
	}
}

// ──────────────────────────── DeleteByUser 测试 ────────────────────────────

func TestDeleteByUser(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "用户记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteByUser(ctx, "user1")
	if err != nil {
		t.Fatalf("DeleteByUser 失败: %v", err)
	}

	// 验证 KV 已删除
	prefix := "UMD/user1/"
	allKV, _ := idx.kvStore.GetByPrefix(ctx, prefix)
	if len(allKV) != 0 {
		t.Errorf("用户 KV 数据应已全部删除, 实际剩余 %d 条", len(allKV))
	}

	// 验证向量集合已删除
	colName := getCollectionName("user1", "scope1", "observation")
	exists, _ := idx.vectorStore.CollectionExists(ctx, colName)
	if exists {
		t.Error("用户的向量集合应已删除")
	}
}

// ──────────────────────────── DeleteByScope 测试 ────────────────────────────

func TestDeleteByScope(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	docs1 := []*MemoryDoc{
		{ID: memID1, Text: "scope1 的记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	docs2 := []*MemoryDoc{
		{ID: memID2, Text: "scope2 的记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs1)
	_ = idx.AddMemories(ctx, "user1", "scope2", docs2)

	err := idx.DeleteByScope(ctx, "scope1")
	if err != nil {
		t.Fatalf("DeleteByScope 失败: %v", err)
	}

	// scope1 的数据应已删除
	raw, _ := idx.kvStore.Get(ctx, kvMemKey("user1", "scope1", memID1))
	if raw != nil {
		t.Error("scope1 的 KV 数据应已删除")
	}

	// scope2 的数据应保留
	raw2, _ := idx.kvStore.Get(ctx, kvMemKey("user1", "scope2", memID2))
	if raw2 == nil {
		t.Error("scope2 的 KV 数据应保留")
	}
}

// ──────────────────────────── DeleteByUserAndScope 测试 ────────────────────────────

func TestDeleteByUserAndScope(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	docs1 := []*MemoryDoc{
		{ID: memID1, Text: "user1+scope1 的记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	docs2 := []*MemoryDoc{
		{ID: memID2, Text: "user1+scope2 的记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs1)
	_ = idx.AddMemories(ctx, "user1", "scope2", docs2)

	err := idx.DeleteByUserAndScope(ctx, "user1", "scope1")
	if err != nil {
		t.Fatalf("DeleteByUserAndScope 失败: %v", err)
	}

	// user1+scope1 的数据应已删除
	raw, _ := idx.kvStore.Get(ctx, kvMemKey("user1", "scope1", memID1))
	if raw != nil {
		t.Error("user1+scope1 的 KV 数据应已删除")
	}

	// user1+scope2 的数据应保留
	raw2, _ := idx.kvStore.Get(ctx, kvMemKey("user1", "scope2", memID2))
	if raw2 == nil {
		t.Error("user1+scope2 的 KV 数据应保留")
	}

	// scope1 的向量集合应已删除
	col1 := getCollectionName("user1", "scope1", "observation")
	exists1, _ := idx.vectorStore.CollectionExists(ctx, col1)
	if exists1 {
		t.Error("user1+scope1 的向量集合应已删除")
	}

	// scope2 的向量集合应保留
	col2 := getCollectionName("user1", "scope2", "observation")
	exists2, _ := idx.vectorStore.CollectionExists(ctx, col2)
	if !exists2 {
		t.Error("user1+scope2 的向量集合应保留")
	}
}

// ──────────────────────────── GetByID 测试 ────────────────────────────

func TestGetByID_存在(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	ts := time.Date(2025, 7, 25, 10, 0, 0, 0, time.UTC)
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试记忆", Type: "observation", Timestamp: ts},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	doc, err := idx.GetByID(ctx, "user1", "scope1", memID)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if doc == nil {
		t.Fatal("GetByID 应返回文档")
	}
	if doc.ID != memID {
		t.Errorf("ID: 期望 %q, 实际 %q", memID, doc.ID)
	}
	if doc.Text != "测试记忆" {
		t.Errorf("Text: 期望 %q, 实际 %q", "测试记忆", doc.Text)
	}
}

func TestGetByID_不存在(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	doc, err := idx.GetByID(ctx, "user1", "scope1", "nonexistent")
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if doc != nil {
		t.Error("不存在的 ID 应返回 nil")
	}
}

func TestGetByID_StorageCodec解码(t *testing.T) {
	idx := newTestIndex()
	codec := newFakeCodec()
	idx.SetStorageCodec(codec)
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "敏感数据", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	doc, err := idx.GetByID(ctx, "user1", "scope1", memID)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if doc.Text != "敏感数据" {
		t.Errorf("解码后文本: 期望 %q, 实际 %q", "敏感数据", doc.Text)
	}
}

// ──────────────────────────── ListMemories 测试 ────────────────────────────

func TestListMemories_正常列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	ts1 := time.Date(2025, 7, 25, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2025, 7, 26, 10, 0, 0, 0, time.UTC)
	docs := []*MemoryDoc{
		{ID: memID1, Text: "记忆1", Type: "observation", Timestamp: ts1},
		{ID: memID2, Text: "记忆2", Type: "observation", Timestamp: ts2},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望 2 条记忆, 实际 %d", len(result))
	}
}

func TestListMemories_无数据(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if result != nil {
		t.Errorf("无数据时应返回 nil, 实际 %v", result)
	}
}

func TestListMemories_类型过滤(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	ts := time.Now().UTC()
	docs := []*MemoryDoc{
		{ID: memID1, Text: "观察记录", Type: "observation", Timestamp: ts},
		{ID: memID2, Text: "用户画像", Type: "user_profile", Timestamp: ts},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 只查询 observation 类型
	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, []string{"observation"})
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望 1 条记忆, 实际 %d", len(result))
	}
	if result[0].Type != "observation" {
		t.Errorf("类型: 期望 observation, 实际 %q", result[0].Type)
	}
}

// ──────────────────────────── ListUserScopes 测试 ────────────────────────────

// TestListUserScopes 验证能正确列出索引中的 (userID, scopeID) 对
func TestListUserScopes(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	ts := time.Now().UTC()
	docs1 := []*MemoryDoc{
		{ID: memID1, Text: "记忆1", Type: "observation", Timestamp: ts},
	}
	docs2 := []*MemoryDoc{
		{ID: memID2, Text: "记忆2", Type: "observation", Timestamp: ts},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs1)
	_ = idx.AddMemories(ctx, "user2", "scope2", docs2)

	scopes, err := idx.ListUserScopes(ctx)
	if err != nil {
		t.Fatalf("ListUserScopes 失败: %v", err)
	}
	if len(scopes) != 2 {
		t.Fatalf("期望 2 个 user-scope 对, 实际 %d", len(scopes))
	}

	// 验证包含预期的 user-scope 对
	scopeMap := make(map[string]bool)
	for _, s := range scopes {
		scopeMap[s.UserID+":"+s.ScopeID] = true
	}
	if !scopeMap["user1:scope1"] {
		t.Error("应包含 user1:scope1")
	}
	if !scopeMap["user2:scope2"] {
		t.Error("应包含 user2:scope2")
	}
}

// ──────────────────────────── 排序函数测试 ────────────────────────────

func TestSortSearchResultsByScore_多结果排序(t *testing.T) {
	results := []*MemorySearchResult{
		{Doc: &MemoryDoc{ID: "1"}, Score: 0.5},
		{Doc: &MemoryDoc{ID: "2"}, Score: 0.9},
		{Doc: &MemoryDoc{ID: "3"}, Score: 0.7},
	}
	sortSearchResultsByScore(results)
	if results[0].Score != 0.9 {
		t.Errorf("最高分应为 0.9, 实际 %f", results[0].Score)
	}
	if results[1].Score != 0.7 {
		t.Errorf("次高分应为 0.7, 实际 %f", results[1].Score)
	}
	if results[2].Score != 0.5 {
		t.Errorf("最低分应为 0.5, 实际 %f", results[2].Score)
	}
}

func TestSortDocsByTypeAndTime_按类型和时间排序(t *testing.T) {
	ts1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ts3 := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)

	docs := []*MemoryDoc{
		{ID: "1", Type: "profile", Timestamp: ts1},
		{ID: "2", Type: "fact", Timestamp: ts2},
		{ID: "3", Type: "profile", Timestamp: ts3},
	}
	typeOrder := map[string]int{"profile": 0, "fact": 1}
	sortDocsByTypeAndTime(docs, typeOrder)

	if docs[0].Type != "profile" {
		t.Errorf("docs[0].Type: 期望 profile, 实际 %q", docs[0].Type)
	}
	if docs[1].Type != "profile" {
		t.Errorf("docs[1].Type: 期望 profile, 实际 %q", docs[1].Type)
	}
	if docs[2].Type != "fact" {
		t.Errorf("docs[2].Type: 期望 fact, 实际 %q", docs[2].Type)
	}
	// 同类型按时间戳降序：ts3 > ts1
	if !docs[0].Timestamp.After(docs[1].Timestamp) {
		t.Errorf("profile 类型内应按时间戳降序排列")
	}
}

// ──────────────────────────── 时间戳解析补充测试 ────────────────────────────

func TestKVDataToMemoryDoc_RFC3339时间戳(t *testing.T) {
	data := map[string]any{
		"mem":       "RFC3339格式",
		"mem_type":  "test",
		"timestamp": "2025-07-25T10:30:00Z",
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	want := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC)
	if !doc.Timestamp.Equal(want) {
		t.Errorf("RFC3339 时间戳: 期望 %v, 实际 %v", want, doc.Timestamp)
	}
}

func TestKVDataToMemoryDoc_Unix时间戳(t *testing.T) {
	unixTs := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC).Unix()
	data := map[string]any{
		"mem":       "Unix时间戳",
		"mem_type":  "test",
		"timestamp": float64(unixTs),
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	want := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC)
	if !doc.Timestamp.Equal(want) {
		t.Errorf("Unix 时间戳: 期望 %v, 实际 %v", want, doc.Timestamp)
	}
}

func TestKVDataToMemoryDoc_无效时间戳字符串(t *testing.T) {
	data := map[string]any{
		"mem":       "无效时间戳",
		"mem_type":  "test",
		"timestamp": "not-a-date",
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	if doc.Timestamp.IsZero() {
		t.Error("无效时间戳应 fallback 到当前时间，不应为零值")
	}
}

func TestKVDataToMemoryDoc_空时间戳字符串(t *testing.T) {
	data := map[string]any{
		"mem":       "空时间戳",
		"mem_type":  "test",
		"timestamp": "",
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	if doc.Timestamp.IsZero() {
		t.Error("空时间戳应 fallback 到当前时间，不应为零值")
	}
}

func TestKVDataToMemoryDoc_timeTime类型(t *testing.T) {
	ts := time.Date(2025, 8, 1, 12, 0, 0, 0, time.UTC)
	data := map[string]any{
		"mem":       "time.Time类型",
		"mem_type":  "test",
		"timestamp": ts,
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	if !doc.Timestamp.Equal(ts) {
		t.Errorf("time.Time 类型: 期望 %v, 实际 %v", ts, doc.Timestamp)
	}
}

func TestKVDataToMemoryDoc_nil时间戳(t *testing.T) {
	data := map[string]any{
		"mem":      "nil时间戳",
		"mem_type": "test",
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	if doc.Timestamp.IsZero() {
		t.Error("nil 时间戳应 fallback 到当前时间")
	}
}

func TestKVDataToMemoryDoc_未知时间戳类型(t *testing.T) {
	data := map[string]any{
		"mem":       "未知类型时间戳",
		"mem_type":  "test",
		"timestamp": []string{"not", "valid"},
	}
	doc := kvDataToMemoryDoc(data, "mem-001")
	if doc.Timestamp.IsZero() {
		t.Error("未知类型时间戳应 fallback 到当前时间")
	}
}

// ──────────────────────────── memoryDocToKVData 补充测试 ────────────────────────────

func TestMemoryDocToKVData_零值时间戳(t *testing.T) {
	doc := &MemoryDoc{
		ID:        "mem-001",
		Text:      "零值时间戳",
		Type:      "test",
		Timestamp: time.Time{},
	}
	data := memoryDocToKVData(doc, "user1", "scope1")
	tsStr, _ := data["timestamp"].(string)
	if tsStr == "" {
		t.Error("零值时间戳应使用当前时间生成格式字符串")
	}
}

// ──────────────────────────── AddMemories StorageCodec 编码测试 ────────────────────────────

func TestAddMemories_StorageCodec编码(t *testing.T) {
	idx := newTestIndex()
	codec := newFakeCodec()
	idx.SetStorageCodec(codec)
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "需要加密的数据", Type: "observation", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err != nil {
		t.Fatalf("AddMemories 失败: %v", err)
	}

	// 验证 KV 中存储的是编码后的值
	raw, _ := idx.kvStore.Get(ctx, kvMemKey("user1", "scope1", memID))
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	memVal, _ := data["mem"].(string)
	if memVal == "需要加密的数据" {
		t.Error("KV 中存储的 mem 应为编码后的值，不应为明文")
	}
	if !strings.HasPrefix(memVal, "enc:") {
		t.Errorf("编码后的值应以 'enc:' 开头, 实际 %q", memVal)
	}
}

// ──────────────────────────── Search topK 默认值测试 ────────────────────────────

func TestSearch_topK为零时使用默认值(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试默认 topK", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	results, err := idx.Search(ctx, "user1", "scope1", "测试", nil, 0)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	// topK=0 应使用默认值 10，不应 panic
	_ = results
}

// ──────────────────────────── ListMemories 分页测试 ────────────────────────────

func TestListMemories_分页(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	ts := time.Now().UTC()
	var docs []*MemoryDoc
	for i := 0; i < 5; i++ {
		docs = append(docs, &MemoryDoc{
			ID:        padID(fmt.Sprintf("mem%03d", i)),
			Text:      fmt.Sprintf("记忆%d", i),
			Type:      "observation",
			Timestamp: ts,
		})
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// offset=2, limit=2 应返回第 3、4 条
	result, err := idx.ListMemories(ctx, "user1", "scope1", 2, 2, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("分页应返回 2 条, 实际 %d", len(result))
	}

	// offset 超出范围应返回 nil
	result2, err := idx.ListMemories(ctx, "user1", "scope1", 100, 10, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if result2 != nil {
		t.Errorf("超出范围的分页应返回 nil, 实际 %v", result2)
	}
}

// ──────────────────────────── DeleteMemories KV中无数据时跳过memType ────────────────────────────

func TestDeleteMemories_KV中无数据时memType为空(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "待删除", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 直接从 KV 删除数据，模拟 KV 中无该文档的情况
	_ = idx.kvStore.Delete(ctx, kvMemKey("user1", "scope1", memID))

	// 再调用 DeleteMemories，memType 应为空字符串
	err := idx.DeleteMemories(ctx, "user1", "scope1", []string{memID})
	if err != nil {
		t.Fatalf("DeleteMemories 失败: %v", err)
	}
}

// ──────────────────────────── parseMemTypeFromCollection 补充测试 ────────────────────────────

func TestParseMemTypeFromCollection_无mtype后缀(t *testing.T) {
	result := parseMemTypeFromCollection("uid_user1_gid_scope1")
	if result != "" {
		t.Errorf("无 _mtype_ 后缀时应返回空字符串, 实际 %q", result)
	}
}

// ──────────────────────────── Search 多类型合并排序测试 ────────────────────────────

// ──────────────────────────── 错误注入类型 ────────────────────────────

// failingKVStore 包装 fakeKVStore，可在指定方法上注入错误
type failingKVStore struct {
	*fakeKVStore
	// failGet Get 方法返回错误
	failGet bool
	// failSet Set 方法返回错误
	failSet bool
	// failDelete Delete 方法返回错误
	failDelete bool
	// failMGet MGet 方法返回错误
	failMGet bool
	// failGetByPrefix GetByPrefix 方法返回错误
	failGetByPrefix bool
	// failDeleteByPrefix DeleteByPrefix 方法返回错误
	failDeleteByPrefix bool
	// failBatchDelete BatchDelete 方法返回错误
	failBatchDelete bool
}

var _ kv.BaseKVStore = &failingKVStore{}

func (f *failingKVStore) Set(ctx context.Context, key string, value []byte) error {
	if f.failSet {
		return fmt.Errorf("KV Set 注入错误")
	}
	return f.fakeKVStore.Set(ctx, key, value)
}

func (f *failingKVStore) ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error) {
	return f.fakeKVStore.ExclusiveSet(ctx, key, value, expiry)
}

func (f *failingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if f.failGet {
		return nil, fmt.Errorf("KV Get 注入错误")
	}
	return f.fakeKVStore.Get(ctx, key)
}

func (f *failingKVStore) Exists(ctx context.Context, key string) (bool, error) {
	return f.fakeKVStore.Exists(ctx, key)
}

func (f *failingKVStore) Delete(ctx context.Context, key string) error {
	if f.failDelete {
		return fmt.Errorf("KV Delete 注入错误")
	}
	return f.fakeKVStore.Delete(ctx, key)
}

func (f *failingKVStore) GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error) {
	if f.failGetByPrefix {
		return nil, fmt.Errorf("KV GetByPrefix 注入错误")
	}
	return f.fakeKVStore.GetByPrefix(ctx, prefix)
}

func (f *failingKVStore) DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error {
	if f.failDeleteByPrefix {
		return fmt.Errorf("KV DeleteByPrefix 注入错误")
	}
	return f.fakeKVStore.DeleteByPrefix(ctx, prefix, batchSize)
}

func (f *failingKVStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	if f.failMGet {
		return nil, fmt.Errorf("KV MGet 注入错误")
	}
	return f.fakeKVStore.MGet(ctx, keys)
}

func (f *failingKVStore) BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error) {
	if f.failBatchDelete {
		return 0, fmt.Errorf("KV BatchDelete 注入错误")
	}
	return f.fakeKVStore.BatchDelete(ctx, keys, batchSize)
}

func (f *failingKVStore) Pipeline(_ context.Context) kv.KVPipeline {
	return nil
}

// failingVectorStore 包装 fakeVectorStore，可在指定方法上注入错误
type failingVectorStore struct {
	*fakeVectorStore
	// failCollectionExists CollectionExists 方法返回错误
	failCollectionExists bool
	// failCreateCollection CreateCollection 方法返回错误
	failCreateCollection bool
	// failSearch Search 方法返回错误
	failSearch bool
	// failListCollectionNames ListCollectionNames 方法返回错误
	failListCollectionNames bool
	// failDeleteCollection DeleteCollection 方法返回错误
	failDeleteCollection bool
	// failAddDocs AddDocs 方法返回错误
	failAddDocs bool
}

var _ vector.BaseVectorStore = &failingVectorStore{}

func (f *failingVectorStore) CreateCollection(ctx context.Context, name string, schema *vector.CollectionSchema, opts ...vector.Option) error {
	if f.failCreateCollection {
		return fmt.Errorf("Vector CreateCollection 注入错误")
	}
	return f.fakeVectorStore.CreateCollection(ctx, name, schema, opts...)
}

func (f *failingVectorStore) DeleteCollection(ctx context.Context, name string, opts ...vector.Option) error {
	if f.failDeleteCollection {
		return fmt.Errorf("Vector DeleteCollection 注入错误")
	}
	return f.fakeVectorStore.DeleteCollection(ctx, name, opts...)
}

func (f *failingVectorStore) CollectionExists(ctx context.Context, name string, opts ...vector.Option) (bool, error) {
	if f.failCollectionExists {
		return false, fmt.Errorf("Vector CollectionExists 注入错误")
	}
	return f.fakeVectorStore.CollectionExists(ctx, name, opts...)
}

func (f *failingVectorStore) GetSchema(ctx context.Context, name string, opts ...vector.Option) (*vector.CollectionSchema, error) {
	return f.fakeVectorStore.GetSchema(ctx, name, opts...)
}

func (f *failingVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...vector.Option) error {
	if f.failAddDocs {
		return fmt.Errorf("Vector AddDocs 注入错误")
	}
	return f.fakeVectorStore.AddDocs(ctx, collectionName, docs, opts...)
}

func (f *failingVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...vector.Option) ([]vector.VectorSearchResult, error) {
	if f.failSearch {
		return nil, fmt.Errorf("Vector Search 注入错误")
	}
	return f.fakeVectorStore.Search(ctx, collectionName, queryVector, vectorField, topK, filters, opts...)
}

func (f *failingVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...vector.Option) error {
	return f.fakeVectorStore.DeleteDocsByIDs(ctx, collectionName, ids, opts...)
}

func (f *failingVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...vector.Option) error {
	return f.fakeVectorStore.DeleteDocsByFilters(ctx, collectionName, filters, opts...)
}

func (f *failingVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	if f.failListCollectionNames {
		return nil, fmt.Errorf("Vector ListCollectionNames 注入错误")
	}
	return f.fakeVectorStore.ListCollectionNames(ctx)
}

func (f *failingVectorStore) UpdateSchema(ctx context.Context, name string, fields []any, opts ...vector.Option) error {
	return f.fakeVectorStore.UpdateSchema(ctx, name, fields, opts...)
}

func (f *failingVectorStore) UpdateCollectionMetadata(ctx context.Context, name string, metadata map[string]any, opts ...vector.Option) error {
	return f.fakeVectorStore.UpdateCollectionMetadata(ctx, name, metadata, opts...)
}

func (f *failingVectorStore) GetCollectionMetadata(ctx context.Context, name string, opts ...vector.Option) (map[string]any, error) {
	return f.fakeVectorStore.GetCollectionMetadata(ctx, name, opts...)
}

// failingEmbedding 包装 fakeEmbedding，可在指定方法上注入错误
type failingEmbedding struct {
	*fakeEmbedding
	// failEmbedDocuments EmbedDocuments 方法返回错误
	failEmbedDocuments bool
}

var _ embedding.BaseEmbedding = &failingEmbedding{}

func (f *failingEmbedding) EmbedDocuments(ctx context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	if f.failEmbedDocuments {
		return nil, fmt.Errorf("EmbedDocuments 注入错误")
	}
	return f.fakeEmbedding.EmbedDocuments(ctx, texts)
}

func (f *failingEmbedding) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return f.fakeEmbedding.EmbedQuery(ctx, text)
}

func (f *failingEmbedding) Dimension() int {
	return f.fakeEmbedding.Dimension()
}

// ──────────────────────────── addIDToTracking 错误分支测试 ────────────────────────────

// TestAddIDToTracking_KVGet失败 验证 KV Get 失败时 addIDToTracking 返回错误
func TestAddIDToTracking_KVGet失败(t *testing.T) {
	kvs := &failingKVStore{fakeKVStore: newFakeKVStore(), failGet: true}
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	err := idx.addIDToTracking(ctx, "user1", "scope1", padID("mem001"), "observation")
	if err == nil {
		t.Fatal("KV Get 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "获取全局 ID 追踪键失败") {
		t.Errorf("错误应包含 '获取全局 ID 追踪键失败', 实际 %v", err)
	}
}

// TestAddIDToTracking_KVSet失败 验证 KV Set 失败时 addIDToTracking 返回错误
func TestAddIDToTracking_KVSet失败(t *testing.T) {
	kvs := &failingKVStore{fakeKVStore: newFakeKVStore(), failSet: true}
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	err := idx.addIDToTracking(ctx, "user1", "scope1", padID("mem001"), "observation")
	if err == nil {
		t.Fatal("KV Set 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "更新全局 ID 追踪键失败") {
		t.Errorf("错误应包含 '更新全局 ID 追踪键失败', 实际 %v", err)
	}
}

// ──────────────────────────── removeIDFromTracking 错误分支测试 ────────────────────────────

// TestRemoveIDFromTracking_KVGet失败 验证 KV Get 失败时 removeIDFromTracking 返回错误
func TestRemoveIDFromTracking_KVGet失败(t *testing.T) {
	kvs := &failingKVStore{fakeKVStore: newFakeKVStore(), failGet: true}
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	err := idx.removeIDFromTracking(ctx, "user1", "scope1", padID("mem001"), "observation")
	if err == nil {
		t.Fatal("KV Get 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "获取全局 ID 追踪键失败") {
		t.Errorf("错误应包含 '获取全局 ID 追踪键失败', 实际 %v", err)
	}
}

// TestRemoveIDFromTracking_KVSet失败 验证 KV Set 失败时 removeIDFromTracking 返回错误
func TestRemoveIDFromTracking_KVSet失败(t *testing.T) {
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	// 先添加两个 ID 使追踪列表非空
	_ = idx.addIDToTracking(ctx, "user1", "scope1", padID("mem001"), "observation")
	_ = idx.addIDToTracking(ctx, "user1", "scope1", padID("mem002"), "observation")

	// 替换为会 Set 失败的 KV 存储
	failKVS := &failingKVStore{fakeKVStore: kvs, failSet: true}
	idx.kvStore = failKVS

	// 移除一个 ID，列表仍有剩余，走 Set 路径
	err := idx.removeIDFromTracking(ctx, "user1", "scope1", memID, "observation")
	if err == nil {
		t.Fatal("KV Set 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "更新全局 ID 追踪键失败") {
		t.Errorf("错误应包含 '更新全局 ID 追踪键失败', 实际 %v", err)
	}
}

// TestRemoveIDFromTracking_KVDelete失败 验证 KV Delete 失败时 removeIDFromTracking 返回错误
func TestRemoveIDFromTracking_KVDelete失败(t *testing.T) {
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	// 只添加一个 ID，移除后列表为空，走 Delete 路径
	_ = idx.addIDToTracking(ctx, "user1", "scope1", memID, "observation")

	// 替换为会 Delete 失败的 KV 存储
	failKVS := &failingKVStore{fakeKVStore: kvs, failDelete: true}
	idx.kvStore = failKVS

	err := idx.removeIDFromTracking(ctx, "user1", "scope1", memID, "observation")
	if err == nil {
		t.Fatal("KV Delete 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "删除全局 ID 追踪键失败") {
		t.Errorf("错误应包含 '删除全局 ID 追踪键失败', 实际 %v", err)
	}
}

// ──────────────────────────── ensureCollection 错误分支测试 ────────────────────────────

// TestEnsureCollection_集合已存在但未缓存 验证集合在 Vector Store 中已存在但未在缓存中时，ensureCollection 能正确缓存
func TestEnsureCollection_集合已存在但未缓存(t *testing.T) {
	vs := newFakeVectorStore()
	idx := NewSimpleMemoryIndex(newFakeKVStore(), vs, newFakeEmbedding())
	ctx := context.Background()

	colName := getCollectionName("user1", "scope1", "observation")
	// 直接在 vectorStore 中创建集合（绕过 ensureCollection，不进入缓存）
	_ = vs.CreateCollection(ctx, colName, &vector.CollectionSchema{})

	// 缓存中不存在该集合
	if idx.createdCollections[colName] {
		t.Fatal("集合不应在缓存中")
	}

	// 调用 ensureCollection 应检测到集合已存在并缓存
	if err := idx.ensureCollection(ctx, colName, 4); err != nil {
		t.Fatalf("ensureCollection 失败: %v", err)
	}

	if !idx.createdCollections[colName] {
		t.Error("集合应已缓存")
	}
}

// TestEnsureCollection_CollectionExists失败 验证 CollectionExists 失败时 ensureCollection 返回错误
func TestEnsureCollection_CollectionExists失败(t *testing.T) {
	vs := &failingVectorStore{fakeVectorStore: newFakeVectorStore(), failCollectionExists: true}
	idx := NewSimpleMemoryIndex(newFakeKVStore(), vs, newFakeEmbedding())
	ctx := context.Background()

	colName := getCollectionName("user1", "scope1", "observation")
	err := idx.ensureCollection(ctx, colName, 4)
	if err == nil {
		t.Fatal("CollectionExists 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "检查集合是否存在失败") {
		t.Errorf("错误应包含 '检查集合是否存在失败', 实际 %v", err)
	}
}

// TestEnsureCollection_CreateCollection失败 验证 CreateCollection 失败时 ensureCollection 返回错误
func TestEnsureCollection_CreateCollection失败(t *testing.T) {
	vs := &failingVectorStore{fakeVectorStore: newFakeVectorStore(), failCreateCollection: true}
	idx := NewSimpleMemoryIndex(newFakeKVStore(), vs, newFakeEmbedding())
	ctx := context.Background()

	colName := getCollectionName("user1", "scope1", "observation")
	err := idx.ensureCollection(ctx, colName, 4)
	if err == nil {
		t.Fatal("CreateCollection 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "创建向量集合失败") {
		t.Errorf("错误应包含 '创建向量集合失败', 实际 %v", err)
	}
}

// ──────────────────────────── AddMemories 错误分支测试 ────────────────────────────

// TestAddMemories_EmbedDocuments失败 验证 EmbedDocuments 失败时 AddMemories 返回错误
func TestAddMemories_EmbedDocuments失败(t *testing.T) {
	emb := &failingEmbedding{fakeEmbedding: newFakeEmbedding(), failEmbedDocuments: true}
	idx := NewSimpleMemoryIndex(newFakeKVStore(), newFakeVectorStore(), emb)
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试嵌入失败", Type: "observation", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err == nil {
		t.Fatal("EmbedDocuments 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "嵌入文档失败") {
		t.Errorf("错误应包含 '嵌入文档失败', 实际 %v", err)
	}
}

// TestAddMemories_AddDocs失败 验证 AddDocs 失败时 AddMemories 返回错误
func TestAddMemories_AddDocs失败(t *testing.T) {
	vs := &failingVectorStore{fakeVectorStore: newFakeVectorStore(), failAddDocs: true}
	idx := NewSimpleMemoryIndex(newFakeKVStore(), vs, newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试 AddDocs 失败", Type: "observation", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err == nil {
		t.Fatal("AddDocs 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "写入向量存储失败") {
		t.Errorf("错误应包含 '写入向量存储失败', 实际 %v", err)
	}
}

// ──────────────────────────── DeleteMemories 错误分支测试 ────────────────────────────

// TestDeleteMemories_KVGet失败 验证 KV Get 失败时 DeleteMemories 返回错误
func TestDeleteMemories_KVGet失败(t *testing.T) {
	kvs := &failingKVStore{fakeKVStore: newFakeKVStore(), failGet: true}
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	err := idx.DeleteMemories(ctx, "user1", "scope1", []string{padID("mem001")})
	if err == nil {
		t.Fatal("KV Get 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "获取 KV 数据失败") {
		t.Errorf("错误应包含 '获取 KV 数据失败', 实际 %v", err)
	}
}

// ──────────────────────────── DeleteByUser 错误分支测试 ────────────────────────────

// TestDeleteByUser_ListCollectionNames失败 验证 ListCollectionNames 失败时 DeleteByUser 返回错误
func TestDeleteByUser_ListCollectionNames失败(t *testing.T) {
	vs := &failingVectorStore{fakeVectorStore: newFakeVectorStore(), failListCollectionNames: true}
	idx := NewSimpleMemoryIndex(newFakeKVStore(), vs, newFakeEmbedding())
	ctx := context.Background()

	err := idx.DeleteByUser(ctx, "user1")
	if err == nil {
		t.Fatal("ListCollectionNames 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "列出向量集合失败") {
		t.Errorf("错误应包含 '列出向量集合失败', 实际 %v", err)
	}
}

// TestDeleteByUser_DeleteCollection失败 验证 DeleteCollection 失败时 DeleteByUser 不返回错误（仅记录日志继续）
func TestDeleteByUser_DeleteCollection失败(t *testing.T) {
	vs := newFakeVectorStore()
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, vs, newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "用户记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 替换为 DeleteCollection 会失败的 vectorStore
	failVS := &failingVectorStore{fakeVectorStore: vs, failDeleteCollection: true}
	idx.vectorStore = failVS

	// DeleteByUser 不应返回错误（DeleteCollection 失败仅记录日志继续）
	err := idx.DeleteByUser(ctx, "user1")
	if err != nil {
		t.Errorf("DeleteCollection 失败时 DeleteByUser 不应返回错误, 实际 %v", err)
	}
}

// ──────────────────────────── DeleteByUserAndScope 错误分支测试 ────────────────────────────

// TestDeleteByUserAndScope_ListCollectionNames失败 验证 collectionsFor 内部 ListCollectionNames 失败时返回错误
func TestDeleteByUserAndScope_ListCollectionNames失败(t *testing.T) {
	vs := &failingVectorStore{fakeVectorStore: newFakeVectorStore(), failListCollectionNames: true}
	idx := NewSimpleMemoryIndex(newFakeKVStore(), vs, newFakeEmbedding())
	ctx := context.Background()

	err := idx.DeleteByUserAndScope(ctx, "user1", "scope1")
	if err == nil {
		t.Fatal("ListCollectionNames 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "列出向量集合失败") {
		t.Errorf("错误应包含 '列出向量集合失败', 实际 %v", err)
	}
}

// TestDeleteByUserAndScope_DeleteCollection失败 验证 DeleteCollection 失败时不返回错误（仅记录日志继续）
func TestDeleteByUserAndScope_DeleteCollection失败(t *testing.T) {
	vs := newFakeVectorStore()
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, vs, newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "用户记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 替换为 DeleteCollection 会失败的 vectorStore
	failVS := &failingVectorStore{fakeVectorStore: vs, failDeleteCollection: true}
	idx.vectorStore = failVS

	// DeleteCollection 失败仅记录日志，不返回错误
	err := idx.DeleteByUserAndScope(ctx, "user1", "scope1")
	if err != nil {
		t.Errorf("DeleteCollection 失败时 DeleteByUserAndScope 不应返回错误, 实际 %v", err)
	}
}

// ──────────────────────────── Search 错误分支测试 ────────────────────────────

// TestSearch_CollectionExists失败 验证 CollectionExists 失败时 Search 跳过该集合并继续
func TestSearch_CollectionExists失败(t *testing.T) {
	vs := newFakeVectorStore()
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, vs, newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试数据", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 替换为 CollectionExists 会失败的 vectorStore
	failVS := &failingVectorStore{fakeVectorStore: vs, failCollectionExists: true}
	idx.vectorStore = failVS

	// Search 不应返回错误（CollectionExists 失败时 continue）
	results, err := idx.Search(ctx, "user1", "scope1", "测试", nil, 5)
	if err != nil {
		t.Fatalf("CollectionExists 失败时 Search 不应返回错误, 实际 %v", err)
	}
	if len(results) != 0 {
		t.Errorf("CollectionExists 失败时应无搜索结果, 实际 %d 条", len(results))
	}
}

// TestSearch_Search失败 验证 Search 方法失败时跳过该集合并继续
func TestSearch_Search失败(t *testing.T) {
	vs := newFakeVectorStore()
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, vs, newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试数据", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 替换为 Search 会失败的 vectorStore
	failVS := &failingVectorStore{fakeVectorStore: vs, failSearch: true}
	idx.vectorStore = failVS

	// Search 不应返回错误（Search 失败时 continue）
	results, err := idx.Search(ctx, "user1", "scope1", "测试", nil, 5)
	if err != nil {
		t.Fatalf("Search 方法失败时 Search 不应返回错误, 实际 %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search 方法失败时应无搜索结果, 实际 %d 条", len(results))
	}
}

// TestSearch_MGet失败 验证 MGet 失败时 Search 跳过该集合并继续
func TestSearch_MGet失败(t *testing.T) {
	vs := newFakeVectorStore()
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, vs, newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试数据", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 替换为 MGet 会失败的 KV 存储
	failKVS := &failingKVStore{fakeKVStore: kvs, failMGet: true}
	idx.kvStore = failKVS

	// Search 不应返回错误（MGet 失败时 continue）
	results, err := idx.Search(ctx, "user1", "scope1", "测试", nil, 5)
	if err != nil {
		t.Fatalf("MGet 失败时 Search 不应返回错误, 实际 %v", err)
	}
	if len(results) != 0 {
		t.Errorf("MGet 失败时应无搜索结果, 实际 %d 条", len(results))
	}
}

// TestSearch_集合不存在时跳过 验证搜索不存在的集合时返回空结果
func TestSearch_集合不存在时跳过(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	// 直接搜索从未添加过数据的集合
	results, err := idx.Search(ctx, "user1", "scope1", "查询", []string{"nonexistent_type"}, 5)
	if err != nil {
		t.Fatalf("搜索不存在的集合不应返回错误, 实际 %v", err)
	}
	if len(results) != 0 {
		t.Errorf("搜索不存在的集合应返回空结果, 实际 %d 条", len(results))
	}
}

// ──────────────────────────── GetByID 错误分支测试 ────────────────────────────

// TestGetByID_无效JSON 验证 KV 中存储无效 JSON 时 GetByID 返回错误
func TestGetByID_无效JSON(t *testing.T) {
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	kvKey := kvMemKey("user1", "scope1", memID)
	// 写入无效 JSON
	_ = kvs.Set(ctx, kvKey, []byte("这不是有效的JSON"))

	_, err := idx.GetByID(ctx, "user1", "scope1", memID)
	if err == nil {
		t.Fatal("无效 JSON 时 GetByID 应返回错误")
	}
	if !strings.Contains(err.Error(), "解析 KV 数据失败") {
		t.Errorf("错误应包含 '解析 KV 数据失败', 实际 %v", err)
	}
}

// TestGetByID_KVGet失败 验证 KV Get 失败时 GetByID 返回错误
func TestGetByID_KVGet失败(t *testing.T) {
	kvs := &failingKVStore{fakeKVStore: newFakeKVStore(), failGet: true}
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	_, err := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if err == nil {
		t.Fatal("KV Get 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "获取 KV 数据失败") {
		t.Errorf("错误应包含 '获取 KV 数据失败', 实际 %v", err)
	}
}

// ──────────────────────────── ListMemories 错误分支测试 ────────────────────────────

// TestListMemories_KVGet失败 验证 KV Get 失败时 ListMemories 返回错误
func TestListMemories_KVGet失败(t *testing.T) {
	kvs := &failingKVStore{fakeKVStore: newFakeKVStore(), failGet: true}
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	_, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err == nil {
		t.Fatal("KV Get 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "获取 ID 追踪键失败") {
		t.Errorf("错误应包含 '获取 ID 追踪键失败', 实际 %v", err)
	}
}

// TestListMemories_MGet失败 验证 MGet 失败时 ListMemories 返回错误
func TestListMemories_MGet失败(t *testing.T) {
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	memID := padID("mem001")
	docs := []*MemoryDoc{
		{ID: memID, Text: "测试记忆", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 替换为 MGet 会失败的 KV 存储
	failKVS := &failingKVStore{fakeKVStore: kvs, failMGet: true}
	idx.kvStore = failKVS

	_, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err == nil {
		t.Fatal("MGet 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "批量获取 KV 数据失败") {
		t.Errorf("错误应包含 '批量获取 KV 数据失败', 实际 %v", err)
	}
}

// TestListMemories_无效JSON 验证 KV 中存储无效 JSON 时 ListMemories 跳过该条目
func TestListMemories_无效JSON(t *testing.T) {
	kvs := newFakeKVStore()
	idx := NewSimpleMemoryIndex(kvs, newFakeVectorStore(), newFakeEmbedding())
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	docs := []*MemoryDoc{
		{ID: memID1, Text: "正常记忆", Type: "observation", Timestamp: time.Now().UTC()},
		{ID: memID2, Text: "将被损坏", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 将 memID2 的 KV 数据覆盖为无效 JSON
	kvKey2 := kvMemKey("user1", "scope1", memID2)
	_ = kvs.Set(ctx, kvKey2, []byte("无效JSON"))

	// ListMemories 应只返回 memID1（无效 JSON 条目被跳过）
	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 不应返回错误, 实际 %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望 1 条有效记忆, 实际 %d", len(result))
	}
	if result[0].ID != memID1 {
		t.Errorf("返回的记忆 ID: 期望 %q, 实际 %q", memID1, result[0].ID)
	}
}

// ──────────────────────────── Search 无效 JSON 测试 ────────────────────────────

// TestSearch_无效JSON 验证搜索时遇到无效 JSON 的 KV 数据时跳过该条目
func TestSearch_无效JSON(t *testing.T) {
	kvs := newFakeKVStore()
	vs := newFakeVectorStore()
	idx := NewSimpleMemoryIndex(kvs, vs, newFakeEmbedding())
	ctx := context.Background()

	memID1 := padID("mem001")
	memID2 := padID("mem002")
	docs := []*MemoryDoc{
		{ID: memID1, Text: "正常记忆", Type: "observation", Timestamp: time.Now().UTC()},
		{ID: memID2, Text: "将被损坏", Type: "observation", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	// 将 memID2 的 KV 数据覆盖为无效 JSON
	kvKey2 := kvMemKey("user1", "scope1", memID2)
	_ = kvs.Set(ctx, kvKey2, []byte("无效JSON"))

	// Search 应只返回 memID1（无效 JSON 条目被跳过）
	results, err := idx.Search(ctx, "user1", "scope1", "记忆", nil, 10)
	if err != nil {
		t.Fatalf("Search 不应返回错误, 实际 %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 条有效搜索结果, 实际 %d", len(results))
	}
	if results[0].Doc.ID != memID1 {
		t.Errorf("搜索结果 ID: 期望 %q, 实际 %q", memID1, results[0].Doc.ID)
	}
}
