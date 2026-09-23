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

// TestValidateEntitiesEpisodes_同步连接信息 测试 Entity-Episode 双向引用同步
func TestValidateEntitiesEpisodes_同步连接信息(t *testing.T) {
	state := NewGraphMemState()

	// 准备实体：entity1 已引用 ep-current，entity2 未引用
	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity1.Episodes = []string{"ep-old", "ep-current", "ep-merge"}

	entity2 := graph.NewEntity()
	entity2.UUID = "entity-2"
	entity2.Episodes = []string{"ep-current"}

	entities := []*graph.Entity{entity1, entity2}

	// 准备当前 Episode
	currentEpisode := graph.NewEpisode()
	currentEpisode.UUID = "ep-current"
	currentEpisode.Entities = []string{"entity-old"}

	// 准备 mem_update 和 mem_update_skip_embed
	state.MemUpdate.UpdatedEntity = []*graph.Entity{entity1}
	state.MemUpdateSkipEmbed.UpdatedEntity = []*graph.Entity{entity1, entity2}
	state.MemUpdateSkipEmbed.UpdatedEpisode = []*graph.Episode{}

	// 准备合并信息
	srcEntity := graph.NewEntity()
	srcEntity.UUID = "src-1"
	srcEntity.Episodes = []string{"ep-merge"}

	mergeEp := graph.NewEpisode()
	mergeEp.UUID = "ep-merge"
	mergeEp.Entities = []string{"src-1"}
	state.LookupTable.Episodes["ep-merge"] = mergeEp

	state.MergeInfos["entity-1"] = &EntityMerge{
		Target: entity1,
		Source: map[string]*graph.Entity{"src-1": srcEntity},
	}

	// 执行
	ValidateEntitiesEpisodes(entities, currentEpisode, state)

	// 验证：当前 Episode 的 entities 应包含原有 + 新实体的 UUID（双向一致）
	if !containsString(currentEpisode.Entities, "entity-1") {
		t.Error("当前 Episode 应包含 entity-1（双向一致）")
	}
	if !containsString(currentEpisode.Entities, "entity-2") {
		t.Error("当前 Episode 应包含 entity-2（双向一致）")
	}
	if !containsString(currentEpisode.Entities, "entity-old") {
		t.Error("当前 Episode 应保留 entity-old")
	}

	// 验证：mem_update_skip_embed.updated_entity 应移除已在 mem_update.updated_entity 中的实体
	for _, e := range state.MemUpdateSkipEmbed.UpdatedEntity {
		if e == entity1 {
			t.Error("mem_update_skip_embed.updated_entity 不应包含 entity1（已在 mem_update.updated_entity 中）")
		}
	}

	// 验证：合并后 Episode 的 entities 中 src-1 应被替换为 entity-1
	if containsString(mergeEp.Entities, "src-1") {
		t.Error("合并后 Episode 的 entities 不应包含 src-1")
	}
	if !containsString(mergeEp.Entities, "entity-1") {
		t.Error("合并后 Episode 的 entities 应包含 entity-1（目标 UUID）")
	}
}

// TestValidateEntitiesEpisodes_双向校验修复 测试 Entity-Episode 双向引用修复
// 场景 1：Episode→Entity 但 Entity↔Episode 不存在 → 从 Episode 移除 Entity
// 场景 2：Entity→Episode 但 Episode↔Entity 不存在 → 向 Episode 添加 Entity
func TestValidateEntitiesEpisodes_双向校验修复(t *testing.T) {
	state := NewGraphMemState()

	// 场景 1：Episode 引用 entity-orphan，但 entity-orphan 不引用 Episode → entity-orphan 应被移除
	// 场景 2：entity-missing 引用 Episode，但 Episode 不引用 entity-missing → entity-missing 应被添加
	entityOrphan := graph.NewEntity()
	entityOrphan.UUID = "entity-orphan"
	entityOrphan.Episodes = []string{} // 不引用任何 Episode

	entityMissing := graph.NewEntity()
	entityMissing.UUID = "entity-missing"
	entityMissing.Episodes = []string{"ep-1"} // 引用 ep-1，但 ep-1 不引用它

	entities := []*graph.Entity{entityOrphan, entityMissing}

	// 准备 Episode
	currentEpisode := graph.NewEpisode()
	currentEpisode.UUID = "ep-current"
	currentEpisode.Entities = []string{}

	ep1 := graph.NewEpisode()
	ep1.UUID = "ep-1"
	ep1.Entities = []string{"entity-orphan"} // 引用 entity-orphan，但 entity-orphan 不引用它

	state.MemUpdate.UpdatedEntity = []*graph.Entity{entityOrphan, entityMissing}
	state.MemUpdateSkipEmbed.UpdatedEntity = []*graph.Entity{}
	state.MemUpdateSkipEmbed.UpdatedEpisode = []*graph.Episode{ep1}
	state.MergeInfos = map[string]*EntityMerge{}

	ValidateEntitiesEpisodes(entities, currentEpisode, state)

	// 场景 1：entity-orphan 不引用 ep-1，应从 ep-1 的 entities 中移除
	if containsString(ep1.Entities, "entity-orphan") {
		t.Error("ep-1 的 entities 不应包含 entity-orphan（因为 entity-orphan 不引用 ep-1）")
	}

	// 场景 2：entity-missing 引用 ep-1，应被添加到 ep-1 的 entities 中
	if !containsString(ep1.Entities, "entity-missing") {
		t.Error("ep-1 的 entities 应包含 entity-missing（因为 entity-missing 引用 ep-1）")
	}

	// 验证去重
	for _, ep := range append(state.MemUpdateSkipEmbed.UpdatedEpisode, currentEpisode) {
		seen := make(map[string]struct{})
		for _, uuid := range ep.Entities {
			if _, ok := seen[uuid]; ok {
				t.Errorf("Episode %s 的 entities 包含重复 UUID: %s", ep.UUID, uuid)
			}
			seen[uuid] = struct{}{}
		}
	}
}

// TestCreateEpisode_基本 测试创建 Episode
func TestCreateEpisode_基本(t *testing.T) {
	state := NewGraphMemState()
	state.CurrentTimestamp = 1000000
	state.ReferenceTimestamp = 999000
	state.EpisodeType = config.EpisodeTypeConversation
	state.Prompting.Language = "cn"
	state.Strategy.SkipUUIDDedupe = true // 跳过 UUID 去重避免数据库依赖

	// 使用 mock 数据库
	db := &mockGraphStore{}

	episode, err := CreateEpisode(context.Background(), db, "test-user", "hello world", state)
	if err != nil {
		t.Fatalf("CreateEpisode 返回错误: %v", err)
	}

	if episode.UserID != "test-user" {
		t.Errorf("期望 UserID=test-user，实际 %s", episode.UserID)
	}
	if episode.Content != "hello world" {
		t.Errorf("期望 Content=hello world，实际 %s", episode.Content)
	}
	if episode.ObjType != "CONVERSATION" {
		t.Errorf("期望 ObjType=CONVERSATION，实际 %s", episode.ObjType)
	}
	if episode.Language != "cn" {
		t.Errorf("期望 Language=cn，实际 %s", episode.Language)
	}
	if episode.CreatedAt != 1000000 {
		t.Errorf("期望 CreatedAt=1000000，实际 %d", episode.CreatedAt)
	}
	if episode.ValidSince != 999000 {
		t.Errorf("期望 ValidSince=999000，实际 %d", episode.ValidSince)
	}
	if len(state.MemUpdate.AddedEpisode) != 1 || state.MemUpdate.AddedEpisode[0] != episode {
		t.Error("episode 应被添加到 state.MemUpdate.AddedEpisode")
	}
}

// TestParseRelationUUIDsToRemove_基本 测试解析需删除的关系 UUID
func TestParseRelationUUIDsToRemove_基本(t *testing.T) {
	state := NewGraphMemState()

	// 准备关系
	relation := graph.NewRelation()
	relation.UUID = "rel-1"

	existingRelations := []map[string]any{
		{"uuid": "existing-1"},
		{"uuid": "existing-2"},
	}

	// 准备 LLM 响应（JSON 字符串），表示需要合并且 existing-1 为重复
	llmResponse := `{"need_merging": true, "combined_content": "merged content", "duplicate_ids": [1]}`

	tasks := []DedupeRelationTask{
		{
			Relation:          relation,
			ExistingRelations: existingRelations,
			Response:          llmResponse,
		},
	}

	ParseRelationUUIDsToRemove(tasks, state)

	// 验证：existing-1 的 UUID 应被加入 state.ToRemove
	found := false
	for _, item := range state.ToRemove {
		if item.UUID == "existing-1" && item.ObjType == "Relation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("state.ToRemove 应包含 existing-1 Relation")
	}
}

// TestParseRelationUUIDsToRemove_无需合并 测试 LLM 响应表示不需要合并
func TestParseRelationUUIDsToRemove_无需合并(t *testing.T) {
	state := NewGraphMemState()

	relation := graph.NewRelation()
	relation.UUID = "rel-1"

	tasks := []DedupeRelationTask{
		{
			Relation:          relation,
			ExistingRelations: []map[string]any{{"uuid": "existing-1"}},
			Response:          `{"need_merging": false}`,
		},
	}

	ParseRelationUUIDsToRemove(tasks, state)

	if len(state.ToRemove) != 0 {
		t.Errorf("无需合并时 state.ToRemove 应为空，实际 %d 项", len(state.ToRemove))
	}
}

// TestProcessRelations_移除废弃关系 测试从实体中移除废弃关系的引用
func TestProcessRelations_移除废弃关系(t *testing.T) {
	state := NewGraphMemState()
	state.Strategy.SkipUUIDDedupe = true

	// 准备实体，其中包含废弃关系的 UUID
	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity1.Relations = []string{"rel-deprecated", "rel-keep"}

	entities := []*graph.Entity{entity1}
	state.MemUpdate.RemovedRelation["rel-deprecated"] = struct{}{}

	// 准备 LookupTable 中的实体（UpdateConnectedEntities 需要）
	state.LookupTable.Entities[entity1.UUID] = entity1

	// 准备新关系
	newRel := graph.NewRelation()
	newRel.LHS = entity1.UUID
	newRel.RHS = entity1.UUID
	relations := []*graph.Relation{newRel}

	err := ProcessRelations(context.Background(), &mockGraphStore{}, entities, relations, state)
	if err != nil {
		t.Fatalf("ProcessRelations 返回错误: %v", err)
	}

	// 验证：废弃关系应从实体的 relations 中移除
	if containsString(entity1.Relations, "rel-deprecated") {
		t.Error("entity1.Relations 不应包含 rel-deprecated")
	}
	if !containsString(entity1.Relations, "rel-keep") {
		t.Error("entity1.Relations 应包含 rel-keep")
	}
}

// TestProcessEntities_基本 测试实体处理的基本逻辑
func TestProcessEntities_基本(t *testing.T) {
	state := NewGraphMemState()
	state.Strategy.SkipUUIDDedupe = true
	state.Prompting.Language = "en"

	// 准备实体
	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity1.Content = "\nhello" // 前缀换行应被移除
	entity1.Episodes = []string{}
	entity1.Relations = []string{"rel-deprecated"}

	entity2 := graph.NewEntity()
	entity2.UUID = "entity-2"
	entity2.Content = "world"
	entity2.Episodes = []string{}
	entity2.Relations = []string{}

	// entity1 是已检索实体（更新），entity2 是新实体
	state.RetrievedEntities["entity-1"] = entity1
	state.MemUpdate.RemovedRelation["rel-deprecated"] = struct{}{}

	entities := []*graph.Entity{entity1, entity2}

	// 准备当前 Episode
	currentEpisode := graph.NewEpisode()
	currentEpisode.UUID = "ep-current"

	err := ProcessEntities(context.Background(), &mockGraphStore{}, entities, currentEpisode, state)
	if err != nil {
		t.Fatalf("ProcessEntities 返回错误: %v", err)
	}

	// 验证：前缀换行被移除
	if entity1.Content != "hello" {
		t.Errorf("期望 Content=hello，实际 %s", entity1.Content)
	}

	// 验证：废弃关系被移除
	if containsString(entity1.Relations, "rel-deprecated") {
		t.Error("entity1.Relations 不应包含 rel-deprecated")
	}

	// 验证：当前 Episode 被关联
	if !containsString(entity1.Episodes, "ep-current") {
		t.Error("entity1.Episodes 应包含 ep-current")
	}

	// 验证：语言设置
	if entity1.Language != "en" {
		t.Errorf("期望 Language=en，实际 %s", entity1.Language)
	}

	// 验证：分类正确
	if !containsEntityPtr(state.MemUpdate.UpdatedEntity, entity1) {
		t.Error("entity1 应在 UpdatedEntity 中")
	}
	if !containsEntityPtr(state.MemUpdate.AddedEntity, entity2) {
		t.Error("entity2 应在 AddedEntity 中")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mockGraphStore 用于测试的模拟图存储
type mockGraphStore struct{}

func (m *mockGraphStore) Config() *graph.GraphConfig                         { return graph.NewGraphConfig("") }
func (m *mockGraphStore) Rebuild(_ context.Context) error                    { return nil }
func (m *mockGraphStore) Refresh(_ context.Context, _ ...graph.Option) error { return nil }
func (m *mockGraphStore) Close() error                                       { return nil }
func (m *mockGraphStore) AddEntity(_ context.Context, _ []*graph.Entity, _ ...graph.Option) error {
	return nil
}
func (m *mockGraphStore) AddRelation(_ context.Context, _ []*graph.Relation, _ ...graph.Option) error {
	return nil
}
func (m *mockGraphStore) AddEpisode(_ context.Context, _ []*graph.Episode, _ ...graph.Option) error {
	return nil
}
func (m *mockGraphStore) Query(_ context.Context, _ string, _ ...graph.Option) ([]map[string]any, error) {
	return nil, nil
}
func (m *mockGraphStore) Delete(_ context.Context, _ string, _ ...graph.Option) error { return nil }
func (m *mockGraphStore) IsEmpty(_ context.Context, _ string) (bool, error)           { return true, nil }
func (m *mockGraphStore) Search(_ context.Context, _ string, _ ...graph.Option) (map[string][]map[string]any, error) {
	return nil, nil
}
func (m *mockGraphStore) AttachEmbedder(_ embedding.BaseEmbedding) error { return nil }
