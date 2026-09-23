package graph_memory

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLookupTables_GetEntity 测试 LookupTables.GetEntity 去重逻辑
func TestLookupTables_GetEntity(t *testing.T) {
	lt := NewLookupTables()

	input1 := map[string]any{
		"uuid":       "entity-1",
		"name":       "张三",
		"content":    "测试实体",
		"obj_type":   "Entity",
		"created_at": int64(1000),
	}
	input2 := map[string]any{
		"uuid":       "entity-1",
		"name":       "张三-修改",
		"content":    "测试实体-修改",
		"obj_type":   "Entity",
		"created_at": int64(2000),
	}

	// 首次获取应创建新实体
	e1 := lt.GetEntity(input1)
	if e1 == nil {
		t.Fatal("GetEntity 返回 nil")
	}
	if e1.UUID != "entity-1" {
		t.Errorf("UUID 期望 entity-1，实际 %s", e1.UUID)
	}
	if e1.Name != "张三" {
		t.Errorf("Name 期望 张三，实际 %s", e1.Name)
	}

	// 再次获取相同 UUID 应返回同一对象（去重）
	e2 := lt.GetEntity(input2)
	if e2 != e1 {
		t.Error("相同 UUID 的 GetEntity 应返回同一对象")
	}
	// 原始对象不应被覆盖
	if e2.Name != "张三" {
		t.Errorf("去重后 Name 应保持原始值 张三，实际 %s", e2.Name)
	}

	// 不同 UUID 应创建新实体
	input3 := map[string]any{"uuid": "entity-2", "name": "李四"}
	e3 := lt.GetEntity(input3)
	if e3 == e1 {
		t.Error("不同 UUID 的 GetEntity 应返回不同对象")
	}
	if e3.UUID != "entity-2" {
		t.Errorf("UUID 期望 entity-2，实际 %s", e3.UUID)
	}
	if len(lt.Entities) != 2 {
		t.Errorf("Entities 映射大小期望 2，实际 %d", len(lt.Entities))
	}
}

// TestLookupTables_GetRelation 测试 LookupTables.GetRelation 去重逻辑
func TestLookupTables_GetRelation(t *testing.T) {
	lt := NewLookupTables()

	input1 := map[string]any{
		"uuid":    "rel-1",
		"content": "关系1",
		"lhs":     "entity-a",
		"rhs":     "entity-b",
	}
	input2 := map[string]any{
		"uuid":    "rel-1",
		"content": "关系1-修改",
	}

	r1 := lt.GetRelation(input1)
	if r1 == nil {
		t.Fatal("GetRelation 返回 nil")
	}
	if r1.UUID != "rel-1" {
		t.Errorf("UUID 期望 rel-1，实际 %s", r1.UUID)
	}

	// 去重
	r2 := lt.GetRelation(input2)
	if r2 != r1 {
		t.Error("相同 UUID 的 GetRelation 应返回同一对象")
	}
}

// TestLookupTables_GetEpisode 测试 LookupTables.GetEpisode 去重逻辑
func TestLookupTables_GetEpisode(t *testing.T) {
	lt := NewLookupTables()

	input1 := map[string]any{
		"uuid":    "ep-1",
		"content": "片段1",
	}
	input2 := map[string]any{
		"uuid":    "ep-1",
		"content": "片段1-修改",
	}

	p1 := lt.GetEpisode(input1)
	if p1 == nil {
		t.Fatal("GetEpisode 返回 nil")
	}
	p2 := lt.GetEpisode(input2)
	if p2 != p1 {
		t.Error("相同 UUID 的 GetEpisode 应返回同一对象")
	}
}

// TestLookupTables_Clear 测试 LookupTables.Clear
func TestLookupTables_Clear(t *testing.T) {
	lt := NewLookupTables()
	lt.Entities["e1"] = graph.NewEntity()
	lt.Relations["r1"] = graph.NewRelation()
	lt.Episodes["p1"] = graph.NewEpisode()

	lt.Clear()

	if len(lt.Entities) != 0 {
		t.Errorf("Clear 后 Entities 应为空，实际 %d", len(lt.Entities))
	}
	if len(lt.Relations) != 0 {
		t.Errorf("Clear 后 Relations 应为空，实际 %d", len(lt.Relations))
	}
	if len(lt.Episodes) != 0 {
		t.Errorf("Clear 后 Episodes 应为空，实际 %d", len(lt.Episodes))
	}
}

// TestGraphMemUpdate_Merge 测试 GraphMemUpdate.Merge 合并逻辑
func TestGraphMemUpdate_Merge(t *testing.T) {
	e1 := graph.NewEntity()
	e1.UUID = "e1"
	e2 := graph.NewEntity()
	e2.UUID = "e2"

	r1 := graph.NewRelation()
	r1.UUID = "r1"

	u1 := &GraphMemUpdate{
		AddedEntity:     []*graph.Entity{e1},
		RemovedEntity:   map[string]struct{}{"old-1": {}},
		AddedRelation:   []*graph.Relation{r1},
		RemovedRelation: map[string]struct{}{"old-rel-1": {}},
	}

	u2 := &GraphMemUpdate{
		AddedEntity:     []*graph.Entity{e2},
		RemovedEntity:   map[string]struct{}{"old-2": {}},
		RemovedRelation: map[string]struct{}{"old-rel-2": {}},
	}

	merged := u1.Merge(u2)

	// 切片合并
	if len(merged.AddedEntity) != 2 {
		t.Errorf("AddedEntity 合并后长度期望 2，实际 %d", len(merged.AddedEntity))
	}
	if len(merged.AddedRelation) != 1 {
		t.Errorf("AddedRelation 合并后长度期望 1，实际 %d", len(merged.AddedRelation))
	}

	// Set 合并
	if len(merged.RemovedEntity) != 2 {
		t.Errorf("RemovedEntity 合并后长度期望 2，实际 %d", len(merged.RemovedEntity))
	}
	if _, ok := merged.RemovedEntity["old-1"]; !ok {
		t.Error("RemovedEntity 应包含 old-1")
	}
	if _, ok := merged.RemovedEntity["old-2"]; !ok {
		t.Error("RemovedEntity 应包含 old-2")
	}
	if len(merged.RemovedRelation) != 2 {
		t.Errorf("RemovedRelation 合并后长度期望 2，实际 %d", len(merged.RemovedRelation))
	}

	// 合并结果不应修改原始对象
	if len(u1.AddedEntity) != 1 {
		t.Error("Merge 不应修改原始 u1")
	}
}

// TestGraphMemState_ClearReferences 测试 GraphMemState.ClearReferences
func TestGraphMemState_ClearReferences(t *testing.T) {
	state := NewGraphMemState()

	// 填充一些数据
	entity := graph.NewEntity()
	entity.UUID = "test-entity"
	state.RetrievedEntities["e1"] = entity
	state.MergeInfos["m1"] = &EntityMerge{
		Target:          graph.NewEntity(),
		Source:          map[string]*graph.Entity{"s1": graph.NewEntity()},
		NewRelations:    []*graph.Relation{graph.NewRelation()},
		RelationsToKeep: map[string]struct{}{"r1": {}},
	}
	state.Content = "test content"
	state.History = "test history"
	state.TmpBuffer = append(state.TmpBuffer, "tmp")
	state.ToRemove = append(state.ToRemove, toRemoveItem{UUID: "x", ObjType: "Relation"})

	state.ClearReferences()

	// 验证所有引用已清除
	if len(state.MergeInfos) != 0 {
		t.Errorf("ClearReferences 后 MergeInfos 应为空，实际 %d", len(state.MergeInfos))
	}
	if len(state.RetrievedEntities) != 0 {
		t.Errorf("ClearReferences 后 RetrievedEntities 应为空，实际 %d", len(state.RetrievedEntities))
	}
	if state.Content != "" {
		t.Errorf("ClearReferences 后 Content 应为空")
	}
	if state.History != "" {
		t.Errorf("ClearReferences 后 History 应为空")
	}
	if len(state.TmpBuffer) != 0 {
		t.Errorf("ClearReferences 后 TmpBuffer 应为空")
	}
	if len(state.ToRemove) != 0 {
		t.Errorf("ClearReferences 后 ToRemove 应为空")
	}
	if state.MemUpdate != nil {
		t.Error("ClearReferences 后 MemUpdate 应为 nil")
	}
	if state.Strategy != nil {
		t.Error("ClearReferences 后 Strategy 应为 nil")
	}
}

// TestBatchEmbed_空输入 测试 BatchEmbed 空输入直接返回
func TestBatchEmbed_空输入(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	embedder := &fakeEmbedder{}

	// 空列表
	result := BatchEmbed(context.Background(), nil, embedder, cfg)
	if result != nil {
		t.Errorf("空输入 BatchEmbed 应返回 nil，实际 %v", result)
	}

	// 空切片
	result = BatchEmbed(context.Background(), []embeddable{}, embedder, cfg)
	if result != nil {
		t.Errorf("空切片 BatchEmbed 应返回 nil，实际 %v", result)
	}
}

// TestNewGraphMemState_默认值 测试 NewGraphMemState 默认值
func TestNewGraphMemState_默认值(t *testing.T) {
	state := NewGraphMemState()

	// 验证默认值
	if state.CurrentTimestamp <= 0 {
		t.Error("CurrentTimestamp 应大于 0")
	}
	if state.ReferenceTimestamp != 0 {
		t.Errorf("ReferenceTimestamp 期望 0，实际 %d", state.ReferenceTimestamp)
	}
	if state.LookupTable == nil {
		t.Error("LookupTable 不应为 nil")
	}
	if len(state.LookupTable.Entities) != 0 {
		t.Error("LookupTable.Entities 应为空 map")
	}
	if state.Strategy == nil {
		t.Error("Strategy 不应为 nil")
	}
	if state.Prompting == nil {
		t.Error("Prompting 不应为 nil")
	}
	if state.Prompting.Language != "cn" {
		t.Errorf("Prompting.Language 期望 cn，实际 %s", state.Prompting.Language)
	}
	if state.EpisodeType != config.EpisodeTypeConversation {
		t.Errorf("EpisodeType 期望 CONVERSATION，实际 %v", state.EpisodeType)
	}
	if state.Content != "" {
		t.Errorf("Content 期望空字符串，实际 %s", state.Content)
	}
	if state.History != "" {
		t.Errorf("History 期望空字符串，实际 %s", state.History)
	}
	if state.MemUpdate == nil {
		t.Error("MemUpdate 不应为 nil")
	}
	if state.MemUpdateSkipEmbed == nil {
		t.Error("MemUpdateSkipEmbed 不应为 nil")
	}
	if len(state.MemUpdate.AddedEntity) != 0 {
		t.Error("MemUpdate.AddedEntity 应为空")
	}
	if len(state.MemUpdate.RemovedEntity) != 0 {
		t.Error("MemUpdate.RemovedEntity 应为空")
	}
	if len(state.Extras) != 0 {
		t.Error("Extras 应为空 map")
	}
	if len(state.MergeInfos) != 0 {
		t.Error("MergeInfos 应为空 map")
	}
	if len(state.EntityTypes) != 0 {
		t.Error("EntityTypes 应为空 slice")
	}
}

// TestNewLookupTables 测试 NewLookupTables 默认值
func TestNewLookupTables(t *testing.T) {
	lt := NewLookupTables()
	if lt.Entities == nil {
		t.Error("Entities 不应为 nil")
	}
	if lt.Relations == nil {
		t.Error("Relations 不应为 nil")
	}
	if lt.Episodes == nil {
		t.Error("Episodes 不应为 nil")
	}
}

// TestEntityMerge_Clear 测试 EntityMerge.Clear
func TestEntityMerge_Clear(t *testing.T) {
	em := &EntityMerge{
		Target:          graph.NewEntity(),
		Source:          map[string]*graph.Entity{"s1": graph.NewEntity()},
		NewRelations:    []*graph.Relation{graph.NewRelation()},
		RelationsToKeep: map[string]struct{}{"r1": {}},
	}

	em.Clear()

	if em.Target != nil {
		t.Error("Clear 后 Target 应为 nil")
	}
	if len(em.Source) != 0 {
		t.Errorf("Clear 后 Source 应为空，实际 %d", len(em.Source))
	}
	if em.NewRelations != nil {
		t.Error("Clear 后 NewRelations 应为 nil")
	}
	if em.RelationsToKeep != nil {
		t.Error("Clear 后 RelationsToKeep 应为 nil")
	}
}

// TestGraphMemPrompting_Clear 测试 GraphMemPrompting.Clear
func TestGraphMemPrompting_Clear(t *testing.T) {
	p := newGraphMemPrompting()
	if p.SchemaEntityExtraction == nil {
		t.Error("初始 SchemaEntityExtraction 不应为 nil")
	}

	p.Clear()

	if p.SchemaEntityExtraction != nil {
		t.Error("Clear 后 SchemaEntityExtraction 应为 nil")
	}
	if p.SchemaEntityDedupe != nil {
		t.Error("Clear 后 SchemaEntityDedupe 应为 nil")
	}
	if p.SchemaRelationMerge != nil {
		t.Error("Clear 后 SchemaRelationMerge 应为 nil")
	}
	if p.SchemaRelationFilter != nil {
		t.Error("Clear 后 SchemaRelationFilter 应为 nil")
	}
}

// TestClassifyRelationsExtracted 测试关系分类逻辑
func TestClassifyRelationsExtracted(t *testing.T) {
	state := NewGraphMemState()

	// 设置 merge_infos：lhs==rhs 的自指向关系应被移除
	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity1.Content = "原始内容"
	entity2 := graph.NewEntity()
	entity2.UUID = "entity-2"

	state.MergeInfos["m1"] = &EntityMerge{
		Target:          entity1,
		Source:          map[string]*graph.Entity{"s1": entity2},
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: map[string]struct{}{},
	}

	// 设置 RetrievedEntities 以支持自指向关系处理
	state.RetrievedEntities["entity-1"] = entity1

	// 构造测试关系
	rel1 := graph.NewRelation()
	rel1.LHS = "entity-1"
	rel1.RHS = "entity-2"
	rel1.Content = "正常关系"

	rel2 := graph.NewRelation()
	rel2.LHS = "entity-1"
	rel2.RHS = "entity-1"
	rel2.Content = "自指向事实"

	rel3 := graph.NewRelation()
	rel3.LHS = "entity-2"
	rel3.RHS = "entity-1"
	rel3.Content = "" // 空内容

	relations := []*graph.Relation{rel1, rel2, rel3}
	ClassifyRelationsExtracted(relations, state)

	// rel1 正常 → 应在 TmpBuffer 中
	foundNormal := false
	for _, item := range state.TmpBuffer {
		if s, ok := item.(string); ok && s == "正常关系" {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Error("正常关系的 Content 应被添加到 TmpBuffer")
	}

	// rel2 自指向 → 应在 ToRemove 中，且实体 Content 应被更新
	foundSelfRef := false
	for _, item := range state.ToRemove {
		if item.UUID == rel2.UUID {
			foundSelfRef = true
		}
	}
	if !foundSelfRef {
		t.Error("自指向关系应被添加到 ToRemove")
	}
	if entity1.Content != "原始内容\n- 自指向事实" {
		t.Errorf("自指向关系应追加到实体 Content，实际: %s", entity1.Content)
	}

	// rel3 空内容 → 应在 ToRemove 中
	foundEmpty := false
	for _, item := range state.ToRemove {
		if item.UUID == rel3.UUID {
			foundEmpty = true
		}
	}
	if !foundEmpty {
		t.Error("空内容关系应被添加到 ToRemove")
	}
}

// TestClassifyRelationsExtracted_MergeInfo 测试 merge_info 中的关系分类
func TestClassifyRelationsExtracted_MergeInfo(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity1.Relations = []string{"existing-rel"}

	entity2 := graph.NewEntity()
	entity2.UUID = "entity-2"

	// lhs != rhs 的关系 → relations_to_keep
	relKeep := graph.NewRelation()
	relKeep.LHS = "entity-1"
	relKeep.RHS = "entity-2"

	// lhs == rhs 的关系 → removed_relation
	relRemove := graph.NewRelation()
	relRemove.LHS = "entity-1"
	relRemove.RHS = "entity-1"

	state.MergeInfos["m1"] = &EntityMerge{
		Target:          entity1,
		Source:          map[string]*graph.Entity{"s1": entity2},
		NewRelations:    []*graph.Relation{relKeep, relRemove},
		RelationsToKeep: map[string]struct{}{},
	}

	ClassifyRelationsExtracted(nil, state)

	// relKeep 应在 RelationsToKeep 中
	if _, ok := state.MergeInfos["m1"].RelationsToKeep[relKeep.UUID]; !ok {
		t.Error("lhs != rhs 的关系应在 RelationsToKeep 中")
	}

	// relRemove 应在 RemovedRelation 中
	if _, ok := state.MemUpdate.RemovedRelation[relRemove.UUID]; !ok {
		t.Error("lhs == rhs 的关系应在 RemovedRelation 中")
	}

	// Target.Relations 应合并了 RelationsToKeep 和原有的 "existing-rel"
	if len(entity1.Relations) != 2 {
		t.Errorf("Target.Relations 应有 2 个元素（RelationsToKeep + 原有），实际 %d", len(entity1.Relations))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fakeEmbedder 测试用模拟嵌入器
type fakeEmbedder struct {
	dimension int
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
	return make([]float64, f.dimension), nil
}

func (f *fakeEmbedder) EmbedDocuments(_ context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i := range texts {
		result[i] = make([]float64, f.dimension)
	}
	return result, nil
}

func (f *fakeEmbedder) Dimension() int {
	return f.dimension
}

func (f *fakeEmbedder) DimensionWithContext(_ context.Context) (int, error) {
	return f.dimension, nil
}
