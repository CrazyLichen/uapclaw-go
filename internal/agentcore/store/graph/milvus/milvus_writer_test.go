package milvus

import (
	"context"
	"fmt"
	"testing"

	graph "github.com/uapclaw/uapclaw-go/internal/agentcore/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeEmbedder 用于测试的嵌入模型模拟
type fakeEmbedder struct {
	embeddings [][]float64
	err        error
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.embeddings) > 0 {
		return f.embeddings[0], nil
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

func (f *fakeEmbedder) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	result := make([][]float64, len(texts))
	for i := range texts {
		if i < len(f.embeddings) {
			result[i] = f.embeddings[i]
		} else {
			result[i] = []float64{0.1, 0.2, 0.3}
		}
	}
	return result, nil
}

func (f *fakeEmbedder) Dimension() int {
	return 3
}

// fakeWriterClient 扩展 fakeMilvusClient，跟踪写入操作
type fakeWriterClient struct {
	*fakeMilvusClient
	insertedColl []string
	upsertedColl []string
	deletedExprs []string
	insertErr    error
	upsertErr    error
	deleteErr    error
}

func newFakeWriterClient() *fakeWriterClient {
	return &fakeWriterClient{
		fakeMilvusClient: newFakeMilvusClient(),
	}
}

func (f *fakeWriterClient) Insert(ctx context.Context, option interface{}, callOptions ...interface{}) (interface{}, error) {
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	f.insertedColl = append(f.insertedColl, "called")
	return nil, nil
}

func (f *fakeWriterClient) Upsert(ctx context.Context, option interface{}, callOptions ...interface{}) (interface{}, error) {
	if f.upsertErr != nil {
		return nil, f.upsertErr
	}
	f.upsertedColl = append(f.upsertedColl, "called")
	return nil, nil
}

func (f *fakeWriterClient) Delete(ctx context.Context, option interface{}, callOptions ...interface{}) (interface{}, error) {
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	f.deletedExprs = append(f.deletedExprs, "called")
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGraphWriter_AddEntity_基本写入(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 3, 10, 5)

	e := graph.NewEntity()
	e.Content = "测试实体"
	e.Name = "测试名称"

	ctx := context.Background()
	err := w.addEntity(ctx, []*graph.Entity{e}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("addEntity() error = %v", err)
	}
}

func TestGraphWriter_AddEntity_自动嵌入(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	emb := &fakeEmbedder{
		embeddings: [][]float64{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
	}
	w := newGraphWriter(fake, storageCfg, emb, 3, 10, 5)

	e := graph.NewEntity()
	e.Content = "测试实体"
	e.Name = "测试名称"

	ctx := context.Background()
	err := w.addEntity(ctx, []*graph.Entity{e})
	if err != nil {
		t.Fatalf("addEntity() error = %v", err)
	}
	// 嵌入应已回填
	if e.ContentEmbedding == nil {
		t.Error("ContentEmbedding 应已被填充")
	}
	if e.NameEmbedding == nil {
		t.Error("NameEmbedding 应已被填充")
	}
}

func TestGraphWriter_AddEntity_NoEmbed跳过嵌入(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	emb := &fakeEmbedder{}
	w := newGraphWriter(fake, storageCfg, emb, 3, 10, 5)

	e := graph.NewEntity()
	e.Content = "测试实体"

	ctx := context.Background()
	err := w.addEntity(ctx, []*graph.Entity{e}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("addEntity() error = %v", err)
	}
	if e.ContentEmbedding != nil {
		t.Error("NoEmbed=true 时 ContentEmbedding 不应被填充")
	}
}

func TestGraphWriter_AddRelation_基本写入(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 3, 10, 5)

	r := graph.NewRelation()
	r.Content = "测试关系"
	r.LHS = "entity1"
	r.RHS = "entity2"

	ctx := context.Background()
	err := w.addRelation(ctx, []*graph.Relation{r}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("addRelation() error = %v", err)
	}
}

func TestGraphWriter_AddEpisode_基本写入(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 3, 10, 5)

	p := graph.NewEpisode()
	p.Content = "测试片段"

	ctx := context.Background()
	err := w.addEpisode(ctx, []*graph.Episode{p}, graph.WithNoEmbed(true))
	if err != nil {
		t.Fatalf("addEpisode() error = %v", err)
	}
}

func TestGraphWriter_Delete_按ID删除(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 3, 10, 5)

	ctx := context.Background()
	err := w.delete(ctx, CollectionEntity, graph.WithIDs("id1", "id2"))
	if err != nil {
		t.Fatalf("delete() error = %v", err)
	}
}

func TestGraphWriter_Delete_空条件(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 3, 10, 5)

	ctx := context.Background()
	err := w.delete(ctx, CollectionEntity)
	if err != nil {
		t.Fatalf("delete() 空条件应返回 nil, error = %v", err)
	}
}

func TestGraphWriter_TruncateFields_截断超长内容(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	storageCfg.Content = 10
	storageCfg.Name = 5

	w := newGraphWriter(nil, storageCfg, nil, 3, 10, 5)

	m := map[string]any{
		"content":  "这是一段超长的内容文本应该被截断",
		"name":     "超长名称",
		"user_id":  "user1",
		"language": "cn",
	}
	result := w.truncateFields(m, CollectionEntity)

	if len(result["content"].(string)) > 10 {
		t.Errorf("content 应被截断到 %d 字符", storageCfg.Content)
	}
	if len(result["name"].(string)) > 5 {
		t.Errorf("name 应被截断到 %d 字符", storageCfg.Name)
	}
}

func TestGraphWriter_TruncateFields_短内容不截断(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(nil, storageCfg, nil, 3, 10, 5)

	m := map[string]any{
		"content": "短内容",
		"name":    "短名",
	}
	result := w.truncateFields(m, CollectionEntity)

	if result["content"] != "短内容" {
		t.Error("短内容不应被截断")
	}
	if result["name"] != "短名" {
		t.Error("短名称不应被截断")
	}
}

func TestGraphWriter_TruncateFields_数组截断(t *testing.T) {
	storageCfg := graph.NewDefaultStorageConfig()
	storageCfg.Relations = 2
	storageCfg.Episodes = 1
	storageCfg.Entities = 3

	w := newGraphWriter(nil, storageCfg, nil, 3, 10, 5)

	m := map[string]any{
		"relations": []string{"r1", "r2", "r3", "r4"},
		"episodes":  []string{"e1", "e2"},
		"entities":  []string{"en1", "en2", "en3", "en4", "en5"},
	}
	result := w.truncateFields(m, CollectionEntity)

	if len(result["relations"].([]string)) != 2 {
		t.Errorf("relations 应被截断到 2，实际 %d", len(result["relations"].([]string)))
	}
	if len(result["episodes"].([]string)) != 1 {
		t.Errorf("episodes 应被截断到 1，实际 %d", len(result["episodes"].([]string)))
	}
	if len(result["entities"].([]string)) != 3 {
		t.Errorf("entities 应被截断到 3，实际 %d", len(result["entities"].([]string)))
	}
}

func TestBuildIDFilterExpr(t *testing.T) {
	expr := buildIDFilterExpr([]string{"abc", "def"})
	expected := `uuid in ["abc", "def"]`
	if expr != expected {
		t.Errorf("buildIDFilterExpr() = %q, want %q", expr, expected)
	}
}

func TestBuildIDFilterExpr_空列表(t *testing.T) {
	expr := buildIDFilterExpr([]string{})
	if expr != "uuid in []" {
		t.Errorf("buildIDFilterExpr(空) = %q", expr)
	}
}

func TestGraphWriter_AddData_空列表(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 3, 10, 5)

	ctx := context.Background()
	err := w.addData(ctx, CollectionEntity, nil)
	if err != nil {
		t.Errorf("addData(空列表) 应返回 nil, error = %v", err)
	}
}

func TestSetEmbedding_Entity(t *testing.T) {
	e := graph.NewEntity()
	setEmbedding(e, "content_embedding", []float64{0.1, 0.2})
	if e.ContentEmbedding == nil {
		t.Error("ContentEmbedding 应被设置")
	}
	setEmbedding(e, "name_embedding", []float64{0.3, 0.4})
	if e.NameEmbedding == nil {
		t.Error("NameEmbedding 应被设置")
	}
}

func TestSetEmbedding_Episode(t *testing.T) {
	p := graph.NewEpisode()
	setEmbedding(p, "content_embedding", []float64{0.1, 0.2})
	if p.ContentEmbedding == nil {
		t.Error("ContentEmbedding 应被设置")
	}
}

func TestSetEmbedding_Relation(t *testing.T) {
	r := graph.NewRelation()
	setEmbedding(r, "content_embedding", []float64{0.1, 0.2})
	if r.ContentEmbedding == nil {
		t.Error("ContentEmbedding 应被设置")
	}
}

func TestExtractEmbedTasks(t *testing.T) {
	e := graph.NewEntity()
	e.Content = "test content"
	e.Name = "test name"

	tasks := extractEmbedTasks(e)
	if len(tasks) != 2 {
		t.Fatalf("Entity EmbedTasks 应返回 2，实际 %d", len(tasks))
	}
}

func TestExtractToMap(t *testing.T) {
	e := graph.NewEntity()
	e.Content = "hello"

	m := extractToMap(e)
	if m == nil {
		t.Fatal("extractToMap 应返回非 nil")
	}
	if m["content"] != "hello" {
		t.Errorf("content = %v, want hello", m["content"])
	}
}

func TestToAnySlice(t *testing.T) {
	entities := []*graph.Entity{graph.NewEntity(), graph.NewEntity()}
	result := toAnySlice(entities)
	if len(result) != 2 {
		t.Errorf("toAnySlice 长度 = %d, want 2", len(result))
	}
}

func TestApplyGraphOptions(t *testing.T) {
	o := applyGraphOptions(graph.WithNoEmbed(true), graph.WithFlush(true))
	if !o.NoEmbed {
		t.Error("NoEmbed 应为 true")
	}
	if !o.Flush {
		t.Error("Flush 应为 true")
	}
}

func TestNewGraphWriter_默认值(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	w := newGraphWriter(fake, storageCfg, nil, 0, 0, 0)

	if w.batchSize != defaultGraphBatchSize {
		t.Errorf("batchSize = %d, want %d", w.batchSize, defaultGraphBatchSize)
	}
	if w.embedDim != 0 {
		t.Errorf("embedDim 应保持传入值 0")
	}
}

func TestInferGraphColumn_Float64向量(t *testing.T) {
	col := inferGraphColumn("emb", []any{[]float64{0.1, 0.2, 0.3}, []float64{0.4, 0.5, 0.6}})
	if col == nil {
		t.Fatal("inferGraphColumn(float64向量) 返回 nil")
	}
	if col.Name() != "emb" {
		t.Errorf("列名 = %s, want emb", col.Name())
	}
}

func TestInferGraphColumn_String(t *testing.T) {
	col := inferGraphColumn("name", []any{"hello", "world"})
	if col == nil {
		t.Fatal("inferGraphColumn(string) 返回 nil")
	}
}

func TestInferGraphColumn_Int64(t *testing.T) {
	col := inferGraphColumn("ts", []any{int64(123), int64(456)})
	if col == nil {
		t.Fatal("inferGraphColumn(int64) 返回 nil")
	}
}

func TestInferGraphColumn_Int32(t *testing.T) {
	col := inferGraphColumn("offset", []any{int32(1), int32(2)})
	if col == nil {
		t.Fatal("inferGraphColumn(int32) 返回 nil")
	}
}

func TestInferGraphColumn_StringArray(t *testing.T) {
	col := inferGraphColumn("relations", []any{[]string{"r1", "r2"}, []string{"r3"}})
	if col == nil {
		t.Fatal("inferGraphColumn([]string) 返回 nil")
	}
}

func TestInferGraphColumn_空值(t *testing.T) {
	col := inferGraphColumn("empty", []any{})
	if col != nil {
		t.Error("inferGraphColumn(空) 应返回 nil")
	}
}

func TestMapsToColumns_空(t *testing.T) {
	cols, err := mapsToColumns(nil)
	if err != nil {
		t.Errorf("mapsToColumns(nil) error = %v", err)
	}
	if cols != nil {
		t.Error("mapsToColumns(nil) 应返回 nil")
	}
}

func TestMapsToColumns_基本(t *testing.T) {
	docs := []map[string]any{
		{"uuid": "abc", "content": "hello", "created_at": int64(123)},
		{"uuid": "def", "content": "world", "created_at": int64(456)},
	}
	cols, err := mapsToColumns(docs)
	if err != nil {
		t.Fatalf("mapsToColumns() error = %v", err)
	}
	if len(cols) < 3 {
		t.Errorf("应有至少 3 列，实际 %d", len(cols))
	}
}

func TestGraphWriter_FetchAndEmbed_失败(t *testing.T) {
	fake := newFakeMilvusClient()
	storageCfg := graph.NewDefaultStorageConfig()
	emb := &fakeEmbedder{err: fmt.Errorf("embed failed")}
	w := newGraphWriter(fake, storageCfg, emb, 3, 10, 5)

	e := graph.NewEntity()
	e.Content = "test"
	tasks := e.EmbedTasks()

	ctx := context.Background()
	err := w.fetchAndEmbed(ctx, tasks)
	if err == nil {
		t.Error("fetchAndEmbed 嵌入失败应返回错误")
	}
}
