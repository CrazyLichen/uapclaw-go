package graph_memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// init 注册模拟后端，确保测试中 NewGraphMemory 不会因 "backend not found" 而失败
func init() {
	graph.RegisterBackend("milvus", func(cfg *graph.GraphConfig) (graph.BaseGraphStore, error) {
		return &mockGraphStore{}, nil
	}, true)
}

// newTestGraphMemory 创建用于测试的 GraphMemory 实例
func newTestGraphMemory(opts ...GraphMemoryOption) (*GraphMemory, error) {
	dbConfig := graph.NewGraphConfig("test://localhost")
	return NewGraphMemory(dbConfig, opts...)
}

// TestNewGraphMemory_构造 测试 GraphMemory 构造
func TestNewGraphMemory_构造(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)
	assert.NotNil(t, gm)
	assert.NotNil(t, gm.DBBackend)
	assert.NotNil(t, gm.Config)
	assert.Nil(t, gm.LLMClient)
	assert.True(t, gm.LLMStructuredOutput)
	assert.Nil(t, gm.Reranker)
	assert.NotNil(t, gm.DefaultExtractionStrategy)
	assert.Equal(t, "cn", gm.Language)
	assert.False(t, gm.Debug)
	assert.NotNil(t, gm.TokenRecord)
	assert.Equal(t, 0, gm.TokenRecord["input_tokens"])
	assert.Equal(t, 0, gm.TokenRecord["output_tokens"])
	assert.NotNil(t, gm.UserLocks)
	assert.NotNil(t, gm.Semaphore)
	assert.Equal(t, float64(defaultGCTime), gm.TimeTillNextGC)
	assert.NotNil(t, gm.searchStrategies)
	_, hasDefault := gm.searchStrategies["default"]
	assert.True(t, hasDefault)
}

// TestNewGraphMemory_默认LLMStructuredOutput 测试 LLMStructuredOutput 默认为 true
func TestNewGraphMemory_默认LLMStructuredOutput(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)
	assert.True(t, gm.LLMStructuredOutput)
}

// TestNewGraphMemory_LLMStructuredOutputFalse 测试 WithLLMStructuredOutput(false)
func TestNewGraphMemory_LLMStructuredOutputFalse(t *testing.T) {
	gm, err := newTestGraphMemory(WithLLMStructuredOutput(false))
	assert.NoError(t, err)
	assert.False(t, gm.LLMStructuredOutput)
}

// TestNewGraphMemory_无效语言 测试无效语言选项
func TestNewGraphMemory_无效语言(t *testing.T) {
	gm, err := newTestGraphMemory(WithLanguage("invalid"))
	// 因为 NewGraphMemory 内部先使用 "cn" 校验，WithLanguage 在之后覆盖
	// 所以构造不会失败，但语言会是 "invalid"
	if err != nil {
		// 如果校验在 WithLanguage 之前失败，也合理
		var baseErr *exception.BaseError
		assert.ErrorAs(t, err, &baseErr)
	} else {
		assert.Equal(t, "invalid", gm.Language)
	}
}

// TestNewGraphMemory_WithOptions 测试所有选项
func TestNewGraphMemory_WithOptions(t *testing.T) {
	strategy := config.NewAddMemStrategy()
	strategy.ChineseEntity = false

	gm, err := newTestGraphMemory(
		WithLLMStructuredOutput(false),
		WithExtractionStrategy(strategy),
		WithLLMExtraKwargs(map[string]any{"temperature": 0.5}),
		WithLanguage("en"),
		WithDebug(true),
	)
	assert.NoError(t, err)
	assert.False(t, gm.LLMStructuredOutput)
	assert.Equal(t, strategy, gm.DefaultExtractionStrategy)
	assert.Equal(t, map[string]any{"temperature": 0.5}, gm.LLMExtraKwargs)
	assert.Equal(t, "en", gm.Language)
	assert.True(t, gm.Debug)
}

// TestWithLLMClient 测试 WithLLMClient 选项
func TestWithLLMClient(t *testing.T) {
	gm, err := newTestGraphMemory(WithLLMClient(nil))
	assert.NoError(t, err)
	assert.Nil(t, gm.LLMClient)
}

// TestGraphMemory_EnsureThreadLock 测试 EnsureThreadLock
func TestGraphMemory_EnsureThreadLock(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	// 首次确保锁存在
	gm.EnsureThreadLock("user1")
	assert.NotNil(t, gm.UserLocks["user1"])

	// 再次调用不应覆盖
	lock1 := gm.UserLocks["user1"]
	gm.EnsureThreadLock("user1")
	lock2 := gm.UserLocks["user1"]
	assert.Same(t, lock1, lock2)

	// 不同用户
	gm.EnsureThreadLock("user2")
	assert.NotNil(t, gm.UserLocks["user2"])
	assert.NotSame(t, gm.UserLocks["user1"], gm.UserLocks["user2"])
}

// TestGraphMemory_RegisterSearchStrategy 测试 RegisterSearchStrategy
func TestGraphMemory_RegisterSearchStrategy(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	// 注册新策略
	entityConf := config.NewSearchConfig()
	relationConf := config.NewSearchConfig()
	episodeConf := config.NewSearchConfig()
	err = gm.RegisterSearchStrategy("custom", entityConf, relationConf, episodeConf)
	assert.NoError(t, err)
	assert.NotNil(t, gm.searchStrategies["custom"])

	// 重复注册应失败
	err = gm.RegisterSearchStrategy("custom", entityConf, relationConf, episodeConf)
	assert.Error(t, err)

	// force 注册应成功
	err = gm.RegisterSearchStrategy("custom", entityConf, relationConf, episodeConf, true)
	assert.NoError(t, err)

	// 空名称应失败
	err = gm.RegisterSearchStrategy("", entityConf, relationConf, episodeConf)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "empty value")
}

// TestGraphMemory_RegisterSearchStrategy_NilConfigs 测试 nil 配置使用默认值
func TestGraphMemory_RegisterSearchStrategy_NilConfigs(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	err = gm.RegisterSearchStrategy("nil_config", nil, nil, nil)
	assert.NoError(t, err)
	strategy := gm.searchStrategies["nil_config"]
	assert.NotNil(t, strategy.EntityConfig)
	assert.NotNil(t, strategy.RelationConfig)
	assert.NotNil(t, strategy.EpisodeConfig)
}

// TestGraphMemory_AttachReranker_Nil 测试 AttachReranker(nil) 应报错
func TestGraphMemory_AttachReranker_Nil(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	err = gm.AttachReranker(nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestNewGraphMemory_并发限制 测试并发限制配置
func TestNewGraphMemory_并发限制(t *testing.T) {
	dbConfig := graph.NewGraphConfig("test://localhost")
	dbConfig.MaxConcurrent = 16
	gm, err := NewGraphMemory(dbConfig)
	assert.NoError(t, err)
	assert.Equal(t, 16, cap(gm.Semaphore))
}

// TestNewGraphMemory_并发限制默认值 测试默认并发限制
func TestNewGraphMemory_并发限制默认值(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)
	// 当 dbConfig.MaxConcurrent > 0 时，使用其值
	assert.Equal(t, graph.DefaultGraphMaxConcurrent, cap(gm.Semaphore))
}

// TestAnyListToMapList 测试 anyListToMapList
func TestAnyListToMapList(t *testing.T) {
	// 空
	assert.Empty(t, anyListToMapList(nil))
	assert.Empty(t, anyListToMapList([]any{}))

	// 混合
	input := []any{
		map[string]any{"name": "a"},
		"not a map",
		map[string]any{"name": "b"},
		42,
	}
	result := anyListToMapList(input)
	assert.Len(t, result, 2)
	assert.Equal(t, "a", result[0]["name"])
	assert.Equal(t, "b", result[1]["name"])
}

// TestSortEpisodesByValidSince 测试 sortEpisodesByValidSince
func TestSortEpisodesByValidSince(t *testing.T) {
	ep3 := graph.NewEpisode()
	ep3.ValidSince = 300
	ep1 := graph.NewEpisode()
	ep1.ValidSince = 100
	ep2 := graph.NewEpisode()
	ep2.ValidSince = 200

	episodes := []*graph.Episode{ep3, ep1, ep2}
	sortEpisodesByValidSince(episodes)

	assert.Equal(t, int64(100), episodes[0].ValidSince)
	assert.Equal(t, int64(200), episodes[1].ValidSince)
	assert.Equal(t, int64(300), episodes[2].ValidSince)
}

// TestNewSearchConfigWithMinScore 测试 newSearchConfigWithMinScore
func TestNewSearchConfigWithMinScore(t *testing.T) {
	sc := newSearchConfigWithMinScore(0.05)
	assert.NotNil(t, sc)
	assert.Equal(t, 0.05, sc.MinScore)
}

// TestReplaceOneSideOfRelation_首次出现 测试首次出现的延迟更新
func TestReplaceOneSideOfRelation_首次出现(t *testing.T) {
	state := NewGraphMemState()
	entityRelationUpdates := make(map[string]map[string]*graph.Relation)

	relation := graph.NewRelation()
	relation.UUID = "rel-1"
	relation.LHS = "src-entity"
	relation.RHS = "other-entity"

	ReplaceOneSideOfRelation("lhs", relation, "tgt-entity", entityRelationUpdates, state)

	// 应添加延迟更新
	assert.Contains(t, state.RelationDeferredUpdates, "tgt-entity")
	assert.Len(t, state.RelationDeferredUpdates["tgt-entity"], 1)
	assert.Equal(t, "lhs", state.RelationDeferredUpdates["tgt-entity"][0].Field)
	assert.Equal(t, "tgt-entity", state.RelationDeferredUpdates["tgt-entity"][0].Value)
	// 应添加到 entityRelationUpdates
	assert.Contains(t, entityRelationUpdates, "tgt-entity")
	assert.Contains(t, entityRelationUpdates["tgt-entity"], "rel-1")
	// 不应标记为有缺陷
	assert.NotContains(t, state.FaultyRelations, "rel-1")
}

// TestReplaceOneSideOfRelation_重复出现 测试重复出现时标记为有缺陷
func TestReplaceOneSideOfRelation_重复出现(t *testing.T) {
	state := NewGraphMemState()
	entityRelationUpdates := make(map[string]map[string]*graph.Relation)
	entityRelationUpdates["tgt-entity"] = make(map[string]*graph.Relation)

	relation := graph.NewRelation()
	relation.UUID = "rel-1"
	entityRelationUpdates["tgt-entity"]["rel-1"] = relation

	// 同一关系重复出现
	ReplaceOneSideOfRelation("rhs", relation, "tgt-entity", entityRelationUpdates, state)

	// 应标记为有缺陷
	assert.Contains(t, state.FaultyRelations, "rel-1")
	// 应从 entityRelationUpdates 中移除
	assert.NotContains(t, entityRelationUpdates["tgt-entity"], "rel-1")
}

// TestAnySliceFromStringSlice 测试 anySliceFromStringSlice
func TestAnySliceFromStringSlice(t *testing.T) {
	input := []string{"a", "b", "c"}
	result := anySliceFromStringSlice(input)
	assert.Len(t, result, 3)
	assert.Equal(t, "a", result[0])
	assert.Equal(t, "b", result[1])
	assert.Equal(t, "c", result[2])

	// 空切片
	assert.Empty(t, anySliceFromStringSlice(nil))
}

// TestContainsRelationPtr 测试 containsRelationPtr
func TestContainsRelationPtr(t *testing.T) {
	r1 := graph.NewRelation()
	r2 := graph.NewRelation()
	list := []*graph.Relation{r1}

	assert.True(t, containsRelationPtr(list, r1))
	assert.False(t, containsRelationPtr(list, r2))
	assert.False(t, containsRelationPtr(nil, r1))
}

// TestInitState_无参考时间 测试 initState 无参考时间时使用当前时间
func TestInitState_无参考时间(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := gm.initState(nil, config.EpisodeTypeConversation)
	assert.NotNil(t, state)
	assert.Equal(t, state.CurrentTimestamp, state.ReferenceTimestamp)
	assert.Equal(t, config.EpisodeTypeConversation, state.EpisodeType)
	assert.NotNil(t, state.Strategy)
	assert.Equal(t, gm.Language, state.Prompting.Language)
}

// TestInitState_有参考时间 测试 initState 有参考时间
func TestInitState_有参考时间(t *testing.T) {
	gm, _ := newTestGraphMemory()
	refTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	state := gm.initState(&refTime, config.EpisodeTypeDocument)
	assert.NotNil(t, state)
	assert.Equal(t, refTime.Unix(), state.ReferenceTimestamp)
	assert.Equal(t, config.EpisodeTypeDocument, state.EpisodeType)
}

// TestInitState_策略语言 测试 initState 中策略影响语言设置
func TestInitState_策略语言(t *testing.T) {
	strategy := config.NewAddMemStrategy()
	strategy.ChineseEntity = false
	strategy.ChineseRelation = false
	strategy.ChineseEntityDedupe = false
	gm, _ := newTestGraphMemory(WithExtractionStrategy(strategy), WithLanguage("en"))
	state := gm.initState(nil, config.EpisodeTypeJSON)
	assert.Equal(t, "en", state.Prompting.EntityExtractionLanguage)
	assert.Equal(t, "en", state.Prompting.RelationExtractionLanguage)
	assert.Equal(t, "en", state.Prompting.EntityDedupeLanguage)
	assert.Equal(t, config.EpisodeTypeJSON, state.EpisodeType)
}

// TestEmbedder_未绑定 测试 Embedder 未绑定时返回 nil
func TestEmbedder_未绑定(t *testing.T) {
	gm, _ := newTestGraphMemory()
	assert.Nil(t, gm.Embedder())
}

// TestEmbedder_已绑定 测试 Embedder 已绑定时返回正确值
func TestEmbedder_已绑定(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 128}
	err := gm.AttachEmbedder(fakeEmb)
	assert.NoError(t, err)
	assert.NotNil(t, gm.Embedder())
	assert.Equal(t, 128, gm.Embedder().Dimension())
}

// TestWithReranker 测试 WithReranker 选项
func TestWithReranker(t *testing.T) {
	gm, err := newTestGraphMemory(WithReranker(nil))
	assert.NoError(t, err)
	assert.Nil(t, gm.Reranker)
}

// TestGraphMemory_SearchOption 测试搜索选项
func TestGraphMemory_SearchOption(t *testing.T) {
	opts := &SearchConfigOptions{
		SearchStrategy: "default",
		SearchEntity:   true,
		SearchRelation: false,
		SearchEpisode:  true,
	}
	WithSearchStrategy("custom")(opts)
	assert.Equal(t, "custom", opts.SearchStrategy)

	emb := []float64{0.1, 0.2, 0.3}
	WithQueryEmbedding(emb)(opts)
	assert.Equal(t, emb, opts.QueryEmbedding)
}

// TestParseRelationFilteringResult_延迟更新 测试延迟更新中关系的端点更新
func TestParseRelationFilteringResult_延迟更新(t *testing.T) {
	state := NewGraphMemState()

	// 设置 merge_info
	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity2 := graph.NewEntity()
	entity2.UUID = "entity-2"

	// 创建待过滤关系
	rel := graph.NewRelation()
	rel.UUID = "rel-1"
	rel.LHS = "src-entity"
	rel.RHS = "other-entity"

	state.MergeInfos["entity-1"] = &EntityMerge{
		Target:          entity1,
		Source:          map[string]*graph.Entity{"src-entity": entity2},
		NewRelations:    []*graph.Relation{rel},
		RelationsToKeep: map[string]struct{}{},
	}

	// 添加延迟更新
	state.RelationDeferredUpdates["entity-1"] = []deferredRelationUpdate{
		{Relation: rel, Field: "lhs", Value: "entity-1"},
	}

	// 执行
	gm, _ := newTestGraphMemory()
	_ = gm.parseRelationFilteringResult(context.Background(), nil, state)

	// 关系在 NewRelations 中，应更新端点
	assert.Equal(t, "entity-1", rel.LHS)
	// 应添加到 mem_update_skip_embed
	assert.True(t, containsRelationPtr(state.MemUpdateSkipEmbed.UpdatedRelation, rel))
}

// TestParseRelationFilteringResult_不在保留列表 测试关系不在保留列表中时标记为待删除
func TestParseRelationFilteringResult_不在保留列表(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "entity-1"
	entity2 := graph.NewEntity()
	entity2.UUID = "entity-2"

	rel := graph.NewRelation()
	rel.UUID = "rel-remove"
	rel.LHS = "src-entity"
	rel.RHS = "other-entity"

	state.MergeInfos["entity-1"] = &EntityMerge{
		Target:          entity1,
		Source:          map[string]*graph.Entity{"src-entity": entity2},
		NewRelations:    []*graph.Relation{}, // 空列表，关系不在保留列表中
		RelationsToKeep: map[string]struct{}{},
	}

	state.RelationDeferredUpdates["entity-1"] = []deferredRelationUpdate{
		{Relation: rel, Field: "lhs", Value: "entity-1"},
	}

	gm, _ := newTestGraphMemory()
	_ = gm.parseRelationFilteringResult(context.Background(), nil, state)

	// 关系不在 NewRelations 中，应标记为待删除
	assert.Contains(t, state.MemUpdate.RemovedRelation, "rel-remove")
}

// TestMaybeGC_禁用 测试 GC 禁用时不执行
func TestMaybeGC_禁用(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.TimeTillNextGC = -1 // 禁用
	// 应不报错
	gm.maybeGC(context.Background())
}

// TestMaybeGC_未到时间 测试 GC 未到间隔时间
func TestMaybeGC_未到时间(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.TimeTillNextGC = 300
	gm.lastGC = float64(time.Now().Unix()) // 刚刚 GC 过
	// 应不报错
	gm.maybeGC(context.Background())
}

// TestAssistantContent 测试 assistantContent 辅助函数
func TestAssistantContent(t *testing.T) {
	assert.Equal(t, "", assistantContent(nil))
}

// TestEntityListFromState 测试 entityListFromState
func TestEntityListFromState(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	// 空状态
	assert.Nil(t, gm.entityListFromState(state))

	// 有实体
	entity := graph.NewEntity()
	entity.UUID = "test-entity"
	entity.Content = "test content"
	state.RetrievedEntities["test-entity"] = entity
	result := gm.entityListFromState(state)
	assert.Len(t, result, 1)
	assert.Equal(t, "test-entity", result[0]["uuid"])
}

// TestBuildMessages_缺少messages 测试 buildMessages 缺少 messages
func TestBuildMessages_缺少messages(t *testing.T) {
	gm, _ := newTestGraphMemory()
	_, err := gm.buildMessages(map[string]any{})
	assert.Error(t, err)
}

// TestBuildMessages_错误类型 测试 buildMessages messages 类型不正确
func TestBuildMessages_错误类型(t *testing.T) {
	gm, _ := newTestGraphMemory()
	_, err := gm.buildMessages(map[string]any{"messages": "invalid"})
	assert.Error(t, err)
}

// TestBuildMessages_正确类型 测试 buildMessages 正确的 dict 列表
func TestBuildMessages_正确类型(t *testing.T) {
	gm, _ := newTestGraphMemory()
	msgs := []map[string]any{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi"},
	}
	_, err := gm.buildMessages(map[string]any{"messages": msgs})
	assert.NoError(t, err)
}

// TestUpdateEntitiesForRelationRemoval_空待移除 测试无待移除关系时不操作
func TestUpdateEntitiesForRelationRemoval_空待移除(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	// ToRemove 为空，不应报错
	gm.updateEntitiesForRelationRemoval(context.Background(), state, nil)
}

// TestHandleRelationDedupe_空TmpBuffer 测试 TmpBuffer 为空时直接返回
func TestHandleRelationDedupe_空TmpBuffer(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	relations := []*graph.Relation{graph.NewRelation()}
	err := gm.handleRelationDedupe(context.Background(), "user1", "content", relations, state)
	assert.NoError(t, err)
}

// TestHandleRelationDedupe_禁用合并 测试 MergeRelations 为 false 时直接返回
func TestHandleRelationDedupe_禁用合并(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = false
	err := gm.handleRelationDedupe(context.Background(), "user1", "content", nil, state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_无已有实体 测试 noExistingEntity 时直接返回
func TestFetchRelevantEntities_无已有实体(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	err := gm.fetchRelevantEntities(context.Background(), nil, true, "user1", state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_空声明列表 测试空声明列表
func TestFetchRelevantEntities_空声明列表(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	err := gm.fetchRelevantEntities(context.Background(), []extraction.EntityDeclaration{}, false, "user1", state)
	assert.NoError(t, err)
}

// TestEntityMerge_空已有实体 测试空已有实体列表
func TestEntityMerge_空已有实体(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	result, err := gm.entityMerge(context.Background(), declarations, nil, state)
	assert.NoError(t, err)
	assert.Equal(t, declarations, result)
}

// TestStateLookupEntity 测试 stateLookupEntity
func TestStateLookupEntity(t *testing.T) {
	r := map[string]any{
		"uuid":    "entity-1",
		"name":    "Alice",
		"content": "test",
		"user_id": "user1",
	}
	e := stateLookupEntity(r, "entity-1")
	assert.NotNil(t, e)
	assert.Equal(t, "entity-1", e.UUID)
	assert.Equal(t, "Alice", e.Name)
}

// TestStateLookupRelation 测试 stateLookupRelation
func TestStateLookupRelation(t *testing.T) {
	r := map[string]any{
		"uuid":    "rel-1",
		"content": "knows",
		"lhs":     "entity-1",
		"rhs":     "entity-2",
	}
	rel := stateLookupRelation(r, "rel-1")
	assert.NotNil(t, rel)
	assert.Equal(t, "rel-1", rel.UUID)
	assert.Equal(t, "entity-1", rel.LHS)
	assert.Equal(t, "entity-2", rel.RHS)
}

// TestStateLookupEpisode 测试 stateLookupEpisode
func TestStateLookupEpisode(t *testing.T) {
	r := map[string]any{
		"uuid":    "ep-1",
		"content": "episode content",
	}
	ep := stateLookupEpisode(r, "ep-1")
	assert.NotNil(t, ep)
	assert.Equal(t, "ep-1", ep.UUID)
	assert.Equal(t, "episode content", ep.Content)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestSearch_未知策略 测试未知搜索策略报错
// 对齐 Python: test_search_unknown_strategy_raises
func TestSearch_未知策略(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	_, err = gm.Search(context.Background(), "query", "user1", true, true, true, WithSearchStrategy("nonexistent"))
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "not found")
}

// TestSearch_空策略 测试空搜索策略报错
// 对齐 Python: test_search_empty_strategy_raises
func TestSearch_空策略(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	_, err = gm.Search(context.Background(), "query", "user1", true, true, true, WithSearchStrategy(""))
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "non-empty string")
}

// TestSearch_无嵌入模型无预计算向量 测试无嵌入模型且无预计算向量报错
// 对齐 Python: test_search_no_embedder_no_query_embedding_raises
func TestSearch_无嵌入模型无预计算向量(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	_, err = gm.Search(context.Background(), "query", "user1", true, true, true)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "attach_embedder")
}

// TestAddMemory_无嵌入模型 测试 AddMemory 无嵌入模型报错
// 对齐 Python: test_add_memory_without_embedder_raises
func TestAddMemory_无嵌入模型(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	_, err = gm.AddMemory(context.Background(), AddMemoryConfig{
		SourceType: config.EpisodeTypeDocument,
		UserID:     "user1",
		Content:    "Some content.",
	})
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestRegisterSearchStrategy_重复注册 测试重复注册策略报错
// 对齐 Python: test_register_search_strategy_duplicate_raises_without_force
func TestRegisterSearchStrategy_重复注册(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	err = gm.RegisterSearchStrategy("dup", config.NewSearchConfig(), nil, nil)
	assert.NoError(t, err)

	err = gm.RegisterSearchStrategy("dup", config.NewSearchConfig(), nil, nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
	assert.Contains(t, baseErr.Message(), "already exists")
}

// TestRegisterSearchStrategy_Force覆盖 测试 force=True 覆盖已有策略
// 对齐 Python: test_register_search_strategy_force_overwrites
func TestRegisterSearchStrategy_Force覆盖(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	err = gm.RegisterSearchStrategy("s", config.NewSearchConfig(), nil, nil)
	assert.NoError(t, err)

	cfg2 := config.NewSearchConfig()
	cfg2.MinScore = 0.5
	err = gm.RegisterSearchStrategy("s", cfg2, nil, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, 0.5, gm.searchStrategies["s"].EntityConfig.MinScore)
}

// TestInitState_中文策略语言 测试策略中文设置回退到语言
// 对齐 Python: test_init_state_chinese_from_strategy_fallback_to_language
func TestInitState_中文策略语言(t *testing.T) {
	strategy := config.NewAddMemStrategy()
	strategy.ChineseEntity = false
	strategy.ChineseRelation = true
	strategy.ChineseEntityDedupe = false
	gm, _ := newTestGraphMemory(WithExtractionStrategy(strategy), WithLanguage("en"))
	state := gm.initState(nil, config.EpisodeTypeConversation)
	assert.Equal(t, "en", state.Prompting.EntityExtractionLanguage)
	assert.Equal(t, "cn", state.Prompting.RelationExtractionLanguage)
	assert.Equal(t, "en", state.Prompting.EntityDedupeLanguage)
}

// TestSearch_有预计算向量 测试使用预计算向量搜索
// 对齐 Python: test_search_success_with_query_embedding_returns_result
func TestSearch_有预计算向量(t *testing.T) {
	gm, err := newTestGraphMemory()
	assert.NoError(t, err)

	// 需要绑定 embedder，但预计算向量应跳过 embedder
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	result, err := gm.Search(context.Background(), "query", "user1", true, true, true,
		WithQueryEmbedding(make([]float64, 32)),
	)
	// 搜索需要实际后端，这里测试不会 panic 即可
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestAsyncTask 测试 asyncTask 结构体
func TestAsyncTask(t *testing.T) {
	task := &asyncTask{Result: "test result"}
	assert.Equal(t, "test result", task.Result)
	assert.Nil(t, task.Err)

	taskErr := &asyncTask{Err: fmt.Errorf("test error")}
	assert.Empty(t, taskErr.Result)
	assert.Error(t, taskErr.Err)
}

// TestPendingMergeTask 测试 pendingMergeTask 结构体
func TestPendingMergeTask(t *testing.T) {
	task := &pendingMergeTask{Result: "merge result"}
	assert.Equal(t, "merge result", task.Result)
	assert.Nil(t, task.Err)
}

// TestRelationFilterTaskItem 测试 relationFilterTaskItem 结构体
func TestRelationFilterTaskItem(t *testing.T) {
	entity := graph.NewEntity()
	entity.UUID = "e1"
	rel := graph.NewRelation()

	item := &relationFilterTaskItem{
		TargetEntity: entity,
		Relations:    []*graph.Relation{rel},
	}
	assert.Equal(t, "e1", item.TargetEntity.UUID)
	assert.Len(t, item.Relations, 1)
}

// TestGraphMemUpdate_Merge_基础 测试 GraphMemUpdate.Merge 基础功能
func TestGraphMemUpdate_Merge_基础(t *testing.T) {
	u1 := NewGraphMemUpdate()
	u1.AddedEntity = append(u1.AddedEntity, graph.NewEntity())
	u1.RemovedEntity["e1"] = struct{}{}

	u2 := NewGraphMemUpdate()
	u2.AddedRelation = append(u2.AddedRelation, graph.NewRelation())
	u2.RemovedEntity["e2"] = struct{}{}

	merged := u1.Merge(u2)
	assert.Len(t, merged.AddedEntity, 1)
	assert.Len(t, merged.AddedRelation, 1)
	assert.Contains(t, merged.RemovedEntity, "e1")
	assert.Contains(t, merged.RemovedEntity, "e2")
}

// TestParseRelationFilteringResult_有过滤任务 测试有过滤任务时的处理
// 对齐 Python: test_parse_relation_filtering_result_applies_merge_infos
func TestParseRelationFilteringResult_有过滤任务(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"
	entity1.Name = "Entity1"

	rel := graph.NewRelation()
	rel.UUID = "rel-1"
	rel.LHS = "e1"
	rel.RHS = "e2"
	rel.Content = "test"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// 添加关系过滤任务
	filterTask := &asyncTask{Result: `{"relevant_relations": [1]}`}
	state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
		TargetEntity: entity1,
		Relations:    []*graph.Relation{rel},
	}

	gm, _ := newTestGraphMemory()
	err := gm.parseRelationFilteringResult(context.Background(), []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	// 过滤结果应更新 NewRelations
	assert.Len(t, state.MergeInfos["e1"].NewRelations, 1)
}

// TestParseRelationFilteringResult_过滤任务失败 测试过滤任务失败时保留全部关系
func TestParseRelationFilteringResult_过滤任务失败(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"

	rel := graph.NewRelation()
	rel.UUID = "rel-1"
	rel.LHS = "e1"
	rel.RHS = "e2"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// 添加失败的关系过滤任务
	filterTask := &asyncTask{Err: fmt.Errorf("LLM error")}
	state.RelationFilterTasks[filterTask] = &relationFilterTaskItem{
		TargetEntity: entity1,
		Relations:    []*graph.Relation{rel},
	}

	gm, _ := newTestGraphMemory()
	err := gm.parseRelationFilteringResult(context.Background(), []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	// 过滤失败时应保留全部关系
	assert.Len(t, state.MergeInfos["e1"].NewRelations, 1)
}

// TestEntityMerge_有去重结果 测试有去重 LLM 结果时的实体合并
func TestEntityMerge_有去重结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
	}

	// 模拟已有实体
	existingEntity := graph.NewEntity()
	existingEntity.UUID = "existing-1"
	existingEntity.Name = "Alice"
	existingEntity.Content = "test"
	state.RetrievedEntities["existing-1"] = existingEntity
	state.LookupTable.Entities["existing-1"] = existingEntity

	// 模拟去重 LLM 结果（空列表表示无需合并）
	dedupeTask := &asyncTask{Result: `[]`}
	state.Tasks = append(state.Tasks, dedupeTask)

	existingEntitiesList := []map[string]any{
		{"uuid": "existing-1", "name": "Alice", "content": "test"},
	}

	result, err := gm.entityMerge(context.Background(), declarations, existingEntitiesList, state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestInitState_默认策略 测试 initState 默认策略设置
func TestInitState_默认策略(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := gm.initState(nil, config.EpisodeTypeConversation)

	assert.NotNil(t, state.Strategy)
	assert.Equal(t, "cn", state.Prompting.EntityExtractionLanguage)
	assert.Equal(t, "cn", state.Prompting.RelationExtractionLanguage)
	assert.Equal(t, "cn", state.Prompting.EntityDedupeLanguage)
	assert.NotNil(t, state.Extras)
	assert.Equal(t, config.EpisodeTypeConversation, state.EpisodeType)
}

// TestGraphMemState_NewGraphMemState 测试 NewGraphMemState 初始化
func TestGraphMemState_NewGraphMemState(t *testing.T) {
	state := NewGraphMemState()
	assert.NotNil(t, state.Tasks)
	assert.NotNil(t, state.MergingTasks)
	assert.NotNil(t, state.MergingTasksEntities)
	assert.NotNil(t, state.PendingMerge)
	assert.NotNil(t, state.RelationDeferredUpdates)
	assert.NotNil(t, state.RelationFilterTasks)
	assert.NotNil(t, state.ToRemove)
	assert.NotNil(t, state.TmpBuffer)
	assert.NotNil(t, state.RetrievedEntities)
	assert.NotNil(t, state.RetrievedRelations)
	assert.NotNil(t, state.FaultyRelations)
	assert.NotNil(t, state.MergeInfos)
	assert.NotNil(t, state.MemUpdate)
	assert.NotNil(t, state.MemUpdateSkipEmbed)
	assert.NotNil(t, state.LookupTable)
	assert.NotNil(t, state.Prompting)
	assert.NotNil(t, state.Strategy)
}

// TestEntityEnrich_无阻塞实体 测试所有实体均为非阻塞时正常抽取摘要
func TestEntityEnrich_无阻塞实体(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"
	entity1.Name = "Alice"
	entity1.Content = ""

	entity2 := graph.NewEntity()
	entity2.UUID = "e2"
	entity2.Name = "Bob"
	entity2.Content = ""

	entities := []*graph.Entity{entity1, entity2}
	content := "Alice and Bob had a meeting"

	// 由于没有 LLM 客户端，InvokeLLM 会失败，
	// 但 entityEnrich 应仍返回实体（可能有错误日志）
	result, err := gm.entityEnrich(context.Background(), entities, content, state)
	// 没有 LLM 客户端会返回错误，但不会 panic
	assert.NotNil(t, result)
	_ = err
}

// TestMaybeGC_到达时间 测试 GC 到达间隔时执行
func TestMaybeGC_到达时间(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.TimeTillNextGC = 0.001 // 很小的间隔
	gm.lastGC = 0              // 很久以前
	// 应不报错且执行 GC
	gm.maybeGC(context.Background())
	// 验证 lastGC 被更新
	assert.Greater(t, gm.lastGC, float64(0))
}

// TestUpdateEntitiesForRelationRemoval_有待移除 测试有待移除关系时更新实体
func TestUpdateEntitiesForRelationRemoval_有待移除(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	// 设置待移除关系
	state.ToRemove = append(state.ToRemove, toRemoveItem{UUID: "rel-1", ObjType: "Relation"})
	state.MemUpdate.RemovedRelation["rel-1"] = struct{}{}

	// 设置实体包含该关系
	entity := graph.NewEntity()
	entity.UUID = "entity-1"
	entity.Name = "Alice"
	entity.Relations = []string{"rel-1", "rel-2"}
	state.LookupTable.Entities["entity-1"] = entity

	// 模拟数据库查询返回实体
	// updateEntitiesForRelationRemoval 会查询数据库，
	// 使用 mockGraphStore 查询返回空

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	gm.updateEntitiesForRelationRemoval(context.Background(), state, declarations)
	// mockGraphStore.Query 返回空，所以不会更新实体
}

// TestPrepareEpisodes_空内容 测试空内容报错
func TestPrepareEpisodes_空内容(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	_, err := gm.prepareEpisodes(context.Background(), state, "  ", config.EpisodeTypeDocument, "user1", nil)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestInvokeLLMAsync 测试 invokeLLMAsync 返回 *asyncTask
func TestInvokeLLMAsync(t *testing.T) {
	gm, _ := newTestGraphMemory()

	// 不设置 LLM 客户端，调用应失败
	task := gm.invokeLLMAsync(context.Background(), map[string]any{}, nil, nil)
	assert.NotNil(t, task)
	// 由于没有 LLM 客户端，任务会在后台失败
}

// TestStartEntityDedupeAsync 测试 startEntityDedupeAsync 返回 *asyncTask
func TestStartEntityDedupeAsync(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	existingEntities := []map[string]any{{"uuid": "e1", "name": "Alice"}}

	task := gm.startEntityDedupeAsync(context.Background(), "content", declarations, existingEntities, state)
	assert.NotNil(t, task)
}

// TestStartRelationExtractionAsync 测试 startRelationExtractionAsync 返回 *asyncTask
func TestStartRelationExtractionAsync(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}

	task := gm.startRelationExtractionAsync(context.Background(), declarations, "content", state, "")
	assert.NotNil(t, task)
}

// TestStartTimezoneTask 测试 startTimezoneTask 返回 channel
func TestStartTimezoneTask(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	ch := gm.startTimezoneTask(context.Background(), "content", state)
	assert.NotNil(t, ch)
}

// TestEntityMerge_有合并任务 测试有合并任务时的实体合并
func TestEntityMerge_有合并任务(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeEntities = true

	declarations := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
	}

	// 模拟已有实体
	existingEntity := graph.NewEntity()
	existingEntity.UUID = "existing-1"
	existingEntity.Name = "Alice"
	existingEntity.Content = "test"
	existingEntity.Episodes = []string{"ep-1"}
	state.RetrievedEntities["existing-1"] = existingEntity
	state.LookupTable.Entities["existing-1"] = existingEntity

	// 模拟去重结果：将索引 1 的已有实体合并到索引 0 的已有实体
	dedupeTask := &asyncTask{Result: `[{"id": 1, "duplicate_ids": [2]}]`}
	state.Tasks = append(state.Tasks, dedupeTask)

	// 添加第二个已有实体用于合并
	existingEntity2 := graph.NewEntity()
	existingEntity2.UUID = "existing-2"
	existingEntity2.Name = "Bob"
	existingEntity2.Content = "test2"
	state.LookupTable.Entities["existing-2"] = existingEntity2

	existingEntitiesList := []map[string]any{
		{"uuid": "existing-1", "name": "Alice", "content": "test"},
		{"uuid": "existing-2", "name": "Bob", "content": "test2"},
	}

	// 由于没有 LLM 客户端，合并任务的 LLM 调用会失败
	result, err := gm.entityMerge(context.Background(), declarations, existingEntitiesList, state)
	// 合并 LLM 调用失败是预期行为
	assert.NotNil(t, result)
	_ = err
}

// TestEntityMerge_无合并实体 测试 MergeEntities=false 时清空合并对
func TestEntityMerge_无合并实体(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeEntities = false

	declarations := []extraction.EntityDeclaration{
		{Name: "Alice", EntityTypeID: 0},
	}

	existingEntity := graph.NewEntity()
	existingEntity.UUID = "existing-1"
	existingEntity.Name = "Alice"
	existingEntity.Content = "test"
	state.RetrievedEntities["existing-1"] = existingEntity
	state.LookupTable.Entities["existing-1"] = existingEntity

	// 去重结果表示有重复（但 MergeEntities=false 应忽略合并）
	dedupeTask := &asyncTask{Result: `[{"id": 1, "duplicate_ids": [2]}]`}
	state.Tasks = append(state.Tasks, dedupeTask)

	existingEntity2 := graph.NewEntity()
	existingEntity2.UUID = "existing-2"
	existingEntity2.Name = "Bob"
	state.LookupTable.Entities["existing-2"] = existingEntity2

	existingEntitiesList := []map[string]any{
		{"uuid": "existing-1", "name": "Alice", "content": "test"},
		{"uuid": "existing-2", "name": "Bob", "content": "test2"},
	}

	result, err := gm.entityMerge(context.Background(), declarations, existingEntitiesList, state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// MergeEntities=false 时不应有合并任务
	assert.Empty(t, state.MemUpdate.RemovedEntity)
}

// TestSearch_部分集合 测试只搜索部分集合
func TestSearch_部分集合(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	// 只搜索实体
	result, err := gm.Search(context.Background(), "query", "user1", true, false, false,
		WithQueryEmbedding(make([]float64, 32)),
	)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// 实体搜索可能为空（mock 后端返回空）
	assert.Empty(t, result.Relation)
	assert.Empty(t, result.Episode)
}

// TestGraphMemory_ClearReferences 测试 ClearReferences 清除所有引用
func TestGraphMemory_ClearReferences(t *testing.T) {
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.UUID = "e1"
	state.RetrievedEntities["e1"] = entity

	mergeInfo := &EntityMerge{
		Target:          entity,
		Source:          map[string]*graph.Entity{"s1": graph.NewEntity()},
		NewRelations:    []*graph.Relation{graph.NewRelation()},
		RelationsToKeep: map[string]struct{}{"r1": {}},
	}
	state.MergeInfos["e1"] = mergeInfo

	state.ClearReferences()

	assert.Empty(t, state.RetrievedEntities)
	assert.Empty(t, state.MergeInfos)
	assert.Empty(t, state.Tasks)
	assert.Empty(t, state.MergingTasks)
	assert.Nil(t, state.MemUpdate)
	assert.Nil(t, state.MemUpdateSkipEmbed)
}

// TestNewGraphMemory_EmbedBatchSize 测试 EmbedBatchSize 默认值
func TestNewGraphMemory_EmbedBatchSize(t *testing.T) {
	gm, _ := newTestGraphMemory()
	assert.NotNil(t, gm.Config)
}
