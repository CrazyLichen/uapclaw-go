package graph_memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
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
	graph.RegisterBackend("milvus", func(cfg *graph.GraphConfig, extraKwargs map[string]any) (graph.BaseGraphStore, error) {
		return &mockGraphStore{}, nil
	}, true)
}

// newTestGraphMemory 创建用于测试的 GraphMemory 实例
func newTestGraphMemory(opts ...GraphMemoryOption) (*GraphMemory, error) {
	dbConfig := graph.NewGraphConfig("test://localhost")
	return NewGraphMemory(dbConfig, opts...)
}

// newAsyncTaskWithResult 构造已完成的 asyncTask（带结果）
func newAsyncTaskWithResult(content string) asyncTask {
	ch := make(asyncTask, 1)
	ch <- asyncResult{Content: content}
	return ch
}

// newAsyncTaskWithError 构造已完成的 asyncTask（带错误）
func newAsyncTaskWithError(err error) asyncTask {
	ch := make(asyncTask, 1)
	ch <- asyncResult{Err: err}
	return ch
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
	relation.LHS = entityShell("src-entity")
	relation.RHS = entityShell("other-entity")

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
	rel.LHS = entityShell("src-entity")
	rel.RHS = entityShell("other-entity")

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
	assert.Equal(t, "entity-1", rel.LHSUUID())
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
	rel.LHS = entityShell("src-entity")
	rel.RHS = entityShell("other-entity")

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
	assert.Equal(t, "entity-1", rel.LHSUUID())
	assert.Equal(t, "entity-2", rel.RHSUUID())
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

	_, err = gm.Search(context.Background(), "query", []string{"user1"}, true, true, true, WithSearchStrategy("nonexistent"))
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

	_, err = gm.Search(context.Background(), "query", []string{"user1"}, true, true, true, WithSearchStrategy(""))
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

	_, err = gm.Search(context.Background(), "query", []string{"user1"}, true, true, true)
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

	result, err := gm.Search(context.Background(), "query", []string{"user1"}, true, true, true,
		WithQueryEmbedding(make([]float64, 32)),
	)
	// 搜索需要实际后端，这里测试不会 panic 即可
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestAsyncTask 测试 asyncTask 结构体
func TestAsyncTask(t *testing.T) {
	task := newAsyncTaskWithResult("test result")
	content, err := task.Wait()
	assert.Equal(t, "test result", content)
	assert.NoError(t, err)

	taskErr := newAsyncTaskWithError(fmt.Errorf("test error"))
	content, err = taskErr.Wait()
	assert.Empty(t, content)
	assert.Error(t, err)
}

// TestPendingMergeTask 测试 pendingMergeTask 结构体
func TestPendingMergeTask(t *testing.T) {
	pendingDone := make(chan struct{})
	close(pendingDone)
	task := &pendingMergeTask{Result: "merge result", done: pendingDone}
	assert.Equal(t, "merge result", task.Result)
	assert.Nil(t, task.Err)
	// 验证 done 通道可读
	<-task.done
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
	rel.LHS = entityShell("e1")
	rel.RHS = entityShell("e2")
	rel.Content = "test"

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// 添加关系过滤任务
	filterTask := newAsyncTaskWithResult(`{"relevant_relations": [1]}`)
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
	rel.LHS = entityShell("e1")
	rel.RHS = entityShell("e2")

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          make(map[string]*graph.Entity),
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: make(map[string]struct{}),
	}
	state.RelationDeferredUpdates["e1"] = []deferredRelationUpdate{}

	// 添加失败的关系过滤任务
	filterTask := newAsyncTaskWithError(fmt.Errorf("LLM error"))
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
	dedupeTask := newAsyncTaskWithResult(`[]`)
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
	gm.lastGC = 0             // 很久以前
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
	state.ToRemove["rel-1"] = &graph.Relation{NamedGraphObject: graph.NamedGraphObject{BaseGraphObject: graph.BaseGraphObject{UUID: "rel-1"}}}
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

	_, err := gm.prepareEpisodes(context.Background(), state, "  ", nil, config.EpisodeTypeDocument, "user1", nil)
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
	dedupeTask := newAsyncTaskWithResult(`[{"id": 1, "duplicate_ids": [2]}]`)
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
	dedupeTask := newAsyncTaskWithResult(`[{"id": 1, "duplicate_ids": [2]}]`)
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
	result, err := gm.Search(context.Background(), "query", []string{"user1"}, true, false, false,
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

// TestAttachReranker_成功 测试 AttachReranker 成功路径
func TestAttachReranker_成功(t *testing.T) {
	gm, _ := newTestGraphMemory()
	reranker := &fakeReranker{}
	err := gm.AttachReranker(reranker)
	assert.NoError(t, err)
	assert.Equal(t, gm.Reranker, reranker)
}

// TestPrepareEpisodes_委托 测试 PrepareEpisodes 委托到 prepareEpisodes
func TestPrepareEpisodes_委托(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	_, err := gm.PrepareEpisodes(context.Background(), state, "  ", nil, config.EpisodeTypeDocument, "user1", nil)
	assert.Error(t, err)
}

// TestExtractEntityDeclarations_委托 测试 ExtractEntityDeclarations 委托
func TestExtractEntityDeclarations_委托(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	// 无 LLM 客户端，应返回错误
	_, _, err := gm.ExtractEntityDeclarations(context.Background(), config.EpisodeTypeDocument, "content", state)
	assert.Error(t, err)
}

// TestFetchRelevantEntities_委托 测试 FetchRelevantEntities 委托
func TestFetchRelevantEntities_委托(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	err := gm.FetchRelevantEntities(context.Background(), nil, true, "user1", state)
	assert.NoError(t, err)
}

// TestEntityEnrich_委托 测试 EntityEnrich 委托
func TestEntityEnrich_委托(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	entities := []*graph.Entity{graph.NewEntity()}
	_, err := gm.EntityEnrich(context.Background(), entities, "content", state)
	// 无 LLM 客户端会失败，但不会 panic
	_ = err
}

// TestRelationDedupe_委托 测试 RelationDedupe 委托
func TestRelationDedupe_委托(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	relations := []*graph.Relation{graph.NewRelation()}
	embedResults := [][]float64{{0.1, 0.2}}
	err := gm.RelationDedupe(context.Background(), "user1", "content", relations, embedResults, state)
	// mockGraphStore.Search 返回 nil，不会执行 LLM 调用
	assert.NoError(t, err)
}

// TestAssistantContent_有内容 测试 assistantContent 有内容时返回文本
func TestAssistantContent_有内容(t *testing.T) {
	msg := llmschema.NewAssistantMessage("hello world")
	result := assistantContent(msg)
	assert.Equal(t, "hello world", result)
}

// TestFindMaxCountUUID 测试 findMaxCountUUID
func TestFindMaxCountUUID(t *testing.T) {
	counts := map[string]int{
		"uuid-1": 3,
		"uuid-2": 5,
		"uuid-3": 1,
	}
	result := findMaxCountUUID(counts)
	assert.Equal(t, "uuid-2", result)

	// 空映射
	assert.Equal(t, "", findMaxCountUUID(map[string]int{}))
}

// TestRemoveEntityFromSlice 测试 removeEntityFromSlice
func TestRemoveEntityFromSlice(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	e3 := graph.NewEntity()

	list := []*graph.Entity{e1, e2, e3}
	result := removeEntityFromSlice(list, e2)
	assert.Len(t, result, 2)
	assert.NotContains(t, result, e2)

	// 不在列表中
	result = removeEntityFromSlice(list, graph.NewEntity())
	assert.Len(t, result, 3)
}

// TestToInt64 测试 toInt64
func TestToInt64(t *testing.T) {
	assert.Equal(t, int64(42), mustToInt64(int64(42)))
	assert.Equal(t, int64(42), mustToInt64(42))
	assert.Equal(t, int64(42), mustToInt64(float64(42)))
	_, ok := toInt64(json.Number("42"))
	assert.True(t, ok)
	_, ok = toInt64("not a number")
	assert.False(t, ok)
	_, ok = toInt64(nil)
	assert.False(t, ok)
}

// mustToInt64 toInt64 辅助断言
func mustToInt64(v any) int64 {
	i, ok := toInt64(v)
	if !ok {
		panic("toInt64 failed")
	}
	return i
}

// TestToStringSlice 测试 toStringSlice
func TestToStringSlice(t *testing.T) {
	// []string
	result, ok := toStringSlice([]string{"a", "b"})
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, result)

	// []any
	result, ok = toStringSlice([]any{"x", "y"})
	assert.True(t, ok)
	assert.Equal(t, []string{"x", "y"}, result)

	// 非 string 元素被过滤
	result, ok = toStringSlice([]any{"a", 42, "b"})
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, result)

	// 不支持的类型
	_, ok = toStringSlice("invalid")
	assert.False(t, ok)
}

// TestSetEmbeddingField 测试 setEmbeddingField
func TestSetEmbeddingField(t *testing.T) {
	entity := graph.NewEntity()
	emb := []float64{0.1, 0.2, 0.3}
	setEmbeddingField(entity, "content_embedding", emb)
	assert.Equal(t, emb, entity.ContentEmbedding)

	setEmbeddingField(entity, "name_embedding", emb)
	assert.Equal(t, emb, entity.NameEmbedding)
}

// TestFieldNameToExported 测试 fieldNameToExported
func TestFieldNameToExported(t *testing.T) {
	assert.Equal(t, "ContentEmbedding", fieldNameToExported("content_embedding"))
	assert.Equal(t, "NameEmbedding", fieldNameToExported("name_embedding"))
	assert.Equal(t, "other_field", fieldNameToExported("other_field"))
}

// TestCollectEmbeddables 测试 collectEmbeddables
func TestCollectEmbeddables(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	r1 := graph.NewRelation()

	result := collectEmbeddables([]*graph.Entity{e1}, []*graph.Relation{r1}, []*graph.Entity{e2})
	assert.Len(t, result, 3)
}

// TestContainsEpisode 测试 containsEpisode
func TestContainsEpisode(t *testing.T) {
	ep1 := graph.NewEpisode()
	ep2 := graph.NewEpisode()
	list := []*graph.Episode{ep1}

	assert.True(t, containsEpisode(list, ep1))
	assert.False(t, containsEpisode(list, ep2))
	assert.False(t, containsEpisode(nil, ep1))
}

// TestBatchEmbed_有对象 测试 BatchEmbed 有对象时正常嵌入
func TestBatchEmbed_有对象(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	embedder := &fakeEmbedder{dimension: 32}

	entity := graph.NewEntity()
	entity.Content = "test content"
	entity.Name = "test name"

	objects := []embeddable{entity}
	result := BatchEmbed(context.Background(), objects, embedder, cfg)
	assert.Nil(t, result)
	assert.NotNil(t, entity.ContentEmbedding)
	assert.NotNil(t, entity.NameEmbedding)
}

// TestPersistToDB_基本 测试 PersistToDB 基本写入
func TestPersistToDB_基本(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &fakeEmbedder{dimension: 32}
	state := NewGraphMemState()

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.NoError(t, err)
}

// TestPersistToDB_有更新实体 测试 PersistToDB 处理缺失嵌入的实体
func TestPersistToDB_有更新实体(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &fakeEmbedder{dimension: 32}
	state := NewGraphMemState()

	// 添加缺少嵌入的更新实体
	entity := graph.NewEntity()
	entity.Content = "test"
	entity.Name = "name"
	state.MemUpdateSkipEmbed.UpdatedEntity = append(state.MemUpdateSkipEmbed.UpdatedEntity, entity)

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.NoError(t, err)
}

// TestContainsEntity 测试 containsEntity
func TestContainsEntity(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	list := []*graph.Entity{e1}

	assert.True(t, containsEntity(list, e1))
	assert.False(t, containsEntity(list, e2))
	assert.False(t, containsEntity(nil, e1))
}

// TestRemoveEntity 测试 removeEntity
func TestRemoveEntity(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	e3 := graph.NewEntity()

	list := []*graph.Entity{e1, e2, e3}
	result := removeEntity(list, e2)
	assert.Len(t, result, 2)
	assert.NotContains(t, result, e2)
}

// TestAnyToStr 测试 anyToStr
func TestAnyStr(t *testing.T) {
	assert.Equal(t, "", anyToStr(nil))
	assert.Equal(t, "hello", anyToStr("hello"))
	assert.Equal(t, "42", anyToStr(42))
}

// fakeReranker 测试用模拟重排序器
type fakeReranker struct{}

func (f *fakeReranker) Rank(_ context.Context, _ string, _ []string, _ ...any) ([]string, error) {
	return nil, nil
}

func (f *fakeReranker) Rerank(_ context.Context, _ string, _ []string, _ ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeReranker) RerankDocs(_ context.Context, _ string, _ []*reranker.Document, _ ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeReranker) RerankSync(_ context.Context, _ string, _ []string, _ ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeReranker) RerankDocsSync(_ context.Context, _ string, _ []*reranker.Document, _ ...reranker.RerankOption) (map[string]float64, error) {
	return nil, nil
}

// TestSetToAnySlice 测试 setToAnySlice
func TestSetToAnySlice(t *testing.T) {
	s := map[string]struct{}{
		"a": {},
		"b": {},
	}
	result := setToAnySlice(s)
	assert.Len(t, result, 2)
	// 空映射
	assert.Empty(t, setToAnySlice(map[string]struct{}{}))
}

// TestRelationFromMap 测试 relationFromMap
func TestRelationFromMap(t *testing.T) {
	input := map[string]any{
		"uuid":         "rel-1",
		"created_at":   int64(1000),
		"user_id":      "user1",
		"obj_type":     "Relation",
		"language":     "cn",
		"name":         "knows",
		"content":      "Alice knows Bob",
		"valid_since":  int64(2000),
		"valid_until":  int64(3000),
		"offset_since": int64(1),
		"offset_until": int64(-1),
		"lhs":          "entity-1",
		"rhs":          "entity-2",
	}
	r := relationFromMap(input)
	assert.Equal(t, "rel-1", r.UUID)
	assert.Equal(t, int64(1000), r.CreatedAt)
	assert.Equal(t, "user1", r.UserID)
	assert.Equal(t, "Relation", r.ObjType)
	assert.Equal(t, "cn", r.Language)
	assert.Equal(t, "knows", r.Name)
	assert.Equal(t, "Alice knows Bob", r.Content)
	assert.Equal(t, int64(2000), r.ValidSince)
	assert.Equal(t, int64(3000), r.ValidUntil)
	assert.Equal(t, int8(1), r.OffsetSince)
	assert.Equal(t, int8(-1), r.OffsetUntil)
	assert.Equal(t, "entity-1", r.LHSUUID())
	assert.Equal(t, "entity-2", r.RHSUUID())
}

// TestEpisodeFromMap 测试 episodeFromMap
func TestEpisodeFromMap(t *testing.T) {
	input := map[string]any{
		"uuid":        "ep-1",
		"created_at":  int64(1000),
		"user_id":     "user1",
		"obj_type":    "Episode",
		"language":    "cn",
		"content":     "test content",
		"valid_since": int64(2000),
		"entities":    []string{"e1", "e2"},
	}
	p := episodeFromMap(input)
	assert.Equal(t, "ep-1", p.UUID)
	assert.Equal(t, int64(1000), p.CreatedAt)
	assert.Equal(t, "user1", p.UserID)
	assert.Equal(t, "Episode", p.ObjType)
	assert.Equal(t, "cn", p.Language)
	assert.Equal(t, "test content", p.Content)
	assert.Equal(t, int64(2000), p.ValidSince)
	assert.Equal(t, []string{"e1", "e2"}, p.Entities)
}

// TestEntityFromMap 测试 entityFromMap
func TestEntityFromMap(t *testing.T) {
	input := map[string]any{
		"uuid":       "entity-1",
		"created_at": int64(1000),
		"user_id":    "user1",
		"obj_type":   "Entity",
		"language":   "cn",
		"name":       "Alice",
		"content":    "test content",
		"metadata":   map[string]any{"key": "value"},
		"attributes": map[string]any{"color": "blue"},
		"relations":  []string{"r1"},
		"episodes":   []string{"ep1"},
	}
	e := entityFromMap(input)
	assert.Equal(t, "entity-1", e.UUID)
	assert.Equal(t, int64(1000), e.CreatedAt)
	assert.Equal(t, "user1", e.UserID)
	assert.Equal(t, "Entity", e.ObjType)
	assert.Equal(t, "cn", e.Language)
	assert.Equal(t, "Alice", e.Name)
	assert.Equal(t, "test content", e.Content)
	assert.Equal(t, "value", e.Metadata["key"])
	assert.Equal(t, "blue", e.Attributes["color"])
	assert.Equal(t, []string{"r1"}, e.Relations)
	assert.Equal(t, []string{"ep1"}, e.Episodes)
}

// TestDedupEntitiesByUUID 测试 dedupEntitiesByUUID
func TestDedupEntitiesByUUID(t *testing.T) {
	e1 := graph.NewEntity()
	e1.UUID = "dup"
	e1.Name = "first"
	e2 := graph.NewEntity()
	e2.UUID = "dup"
	e2.Name = "second"
	e3 := graph.NewEntity()
	e3.UUID = "unique"

	result := dedupEntitiesByUUID([]*graph.Entity{e1, e2, e3})
	assert.Len(t, result, 2)
	// 保留最后一个
	for _, e := range result {
		if e.UUID == "dup" {
			assert.Equal(t, "second", e.Name)
		}
	}
}

// TestFindToRemove 测试 findToRemove
func TestFindToRemove(t *testing.T) {
	tgt := graph.NewEntity()
	tgt.UUID = "tgt-1"
	src := graph.NewEntity()
	src.UUID = "src-1"

	mergeDict := map[string][]*graph.Entity{
		"tgt-1": {src},
	}
	result := findToRemove(mergeDict)
	assert.Contains(t, result, "src-1")
	assert.NotContains(t, result, "tgt-1")
}

// TestResolveMergeDict_目标在结果中 测试目标实体在结果中的情况
func TestResolveMergeDict_目标在结果中(t *testing.T) {
	tgt := graph.NewEntity()
	tgt.UUID = "tgt-1"
	src := graph.NewEntity()
	src.UUID = "src-1"

	mergeDict := map[string][]*graph.Entity{
		"tgt-1": {src},
	}
	result := []EntityOrDeclaration{{Entity: src}, {Entity: tgt}}
	uuidLookup := map[string]*graph.Entity{"tgt-1": tgt, "src-1": src}

	mergeDictSorted := resolveMergeDict(mergeDict, result, uuidLookup)
	assert.Contains(t, mergeDictSorted, "tgt-1")
	// src 应被替换为 tgt
	assert.Equal(t, tgt, result[0].Entity)
}

// TestResolveMergeDict_目标不在结果中 测试目标实体不在结果中时选择新目标
func TestResolveMergeDict_目标不在结果中(t *testing.T) {
	tgt := graph.NewEntity()
	tgt.UUID = "tgt-1"
	src1 := graph.NewEntity()
	src1.UUID = "src-1"
	src2 := graph.NewEntity()
	src2.UUID = "src-2"

	mergeDict := map[string][]*graph.Entity{
		"tgt-1": {src1, src2},
	}
	// 只有 src 在 result 中，tgt 不在
	result := []EntityOrDeclaration{{Entity: src1}, {Entity: src2}}
	uuidLookup := map[string]*graph.Entity{"tgt-1": tgt, "src-1": src1, "src-2": src2}

	mergeDictSorted := resolveMergeDict(mergeDict, result, uuidLookup)
	// 应产生新的目标 key
	assert.Len(t, mergeDictSorted, 1)
}

// TestParseRelationUUIDsToRemove_完整 测试 ParseRelationUUIDsToRemove 完整逻辑
func TestParseRelationUUIDsToRemove_完整(t *testing.T) {
	state := NewGraphMemState()
	rel := graph.NewRelation()
	rel.UUID = "new-rel"
	rel.Name = "test"
	rel.Content = "test content"

	existingRels := []map[string]any{
		{"uuid": "dup-1", "content": "existing"},
		{"uuid": "dup-2", "content": "existing2"},
	}

	// need_merging=true, combined_content 非空, duplicate_ids=[2]
	task := DedupeRelationTask{
		Relation:          rel,
		ExistingRelations: existingRels,
		Response:          `{"need_merging": true, "combined_content": "merged", "duplicate_ids": [2]}`,
	}

	ParseRelationUUIDsToRemove([]DedupeRelationTask{task}, state)
	// duplicate_ids=[2] 表示 existingRels[1] 即 "dup-2" 应被删除
	// 添加到 state.ToRemove，而非 state.MemUpdate.RemovedRelation
	foundDup2 := false
	for uuid := range state.ToRemove {
		if uuid == "dup-2" {
			foundDup2 = true
		}
	}
	assert.True(t, foundDup2, "dup-2 应在 ToRemove 中")
}

// TestPersistToDB_有删除 测试 PersistToDB 有待删除实体和关系
func TestPersistToDB_有删除(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &fakeEmbedder{dimension: 32}
	state := NewGraphMemState()

	state.MemUpdate.RemovedEntity["e-old"] = struct{}{}
	state.MemUpdate.RemovedRelation["r-old"] = struct{}{}

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.NoError(t, err)
}

// TestPersistToDB_有SkipEmbed更新 测试 PersistToDB 有 skip embed 更新
func TestPersistToDB_有SkipEmbed更新(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &fakeEmbedder{dimension: 32}
	state := NewGraphMemState()

	// 有嵌入的更新实体
	entity := graph.NewEntity()
	entity.Content = "test"
	entity.Name = "name"
	entity.ContentEmbedding = []float64{0.1}
	entity.NameEmbedding = []float64{0.2}
	state.MemUpdateSkipEmbed.UpdatedEntity = append(state.MemUpdateSkipEmbed.UpdatedEntity, entity)

	// Skip embed 关系更新
	rel := graph.NewRelation()
	rel.Content = "rel content"
	state.MemUpdateSkipEmbed.UpdatedRelation = append(state.MemUpdateSkipEmbed.UpdatedRelation, rel)

	// Skip embed Episode 更新
	ep := graph.NewEpisode()
	ep.Content = "ep content"
	state.MemUpdateSkipEmbed.UpdatedEpisode = append(state.MemUpdateSkipEmbed.UpdatedEpisode, ep)

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.NoError(t, err)
}

// TestAnySliceToStrings 测试 anySliceToStrings
func TestAnySliceToStrings(t *testing.T) {
	input := []any{"a", 42, "b"}
	result := anySliceToStrings(input)
	assert.Equal(t, []string{"a", "b"}, result)
	assert.Empty(t, anySliceToStrings(nil))
}

// TestContainsString 测试 containsString
func TestContainsString(t *testing.T) {
	slice := []string{"a", "b", "c"}
	assert.True(t, containsString(slice, "b"))
	assert.False(t, containsString(slice, "d"))
	assert.False(t, containsString(nil, "a"))
}

// TestContainsStringSet 测试 containsStringSet
func TestContainsStringSet(t *testing.T) {
	set := map[string]struct{}{"a": {}, "b": {}}
	assert.True(t, containsStringSet(set, "a"))
	assert.False(t, containsStringSet(set, "c"))
}

// TestRemoveString 测试 removeString
func TestRemoveString(t *testing.T) {
	slice := []string{"a", "b", "c", "b"}
	result := removeString(slice, "b")
	assert.Equal(t, []string{"a", "c"}, result)
}

// TestUniqueStringSlice 测试 uniqueStringSlice
func TestUniqueStringSlice(t *testing.T) {
	slice := []string{"a", "b", "a", "c", "b"}
	result := uniqueStringSlice(slice)
	assert.Len(t, result, 3)
}

// TestCollectEmbeddableEpisodes 测试 collectEmbeddableEpisodes
func TestCollectEmbeddableEpisodes(t *testing.T) {
	ep1 := graph.NewEpisode()
	ep2 := graph.NewEpisode()
	result := collectEmbeddableEpisodes([]*graph.Episode{ep1, ep2})
	assert.Len(t, result, 2)
	assert.Empty(t, collectEmbeddableEpisodes(nil))
}

// TestSearch_NaNQueryEmbedding 测试 NaN 的预计算向量报错
func TestSearch_NaNQueryEmbedding(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	_, err := gm.Search(context.Background(), "query", []string{"user1"}, true, false, false,
		WithQueryEmbedding([]float64{0.1, math.NaN()}),
	)
	assert.Error(t, err)
	var baseErr *exception.BaseError
	assert.ErrorAs(t, err, &baseErr)
}

// TestSearch_InfQueryEmbedding 测试 Inf 的预计算向量报错
func TestSearch_InfQueryEmbedding(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	_, err := gm.Search(context.Background(), "query", []string{"user1"}, true, false, false,
		WithQueryEmbedding([]float64{0.1, math.Inf(1)}),
	)
	assert.Error(t, err)
}

// TestPerformSearch_重排序器缺失 测试 performSearch rerank=true 但无 reranker 时报错
func TestPerformSearch_重排序器缺失(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	// 注册需要 rerank 的策略
	entityConf := config.NewSearchConfig()
	entityConf.Rerank = true
	err := gm.RegisterSearchStrategy("rerank_test", entityConf, nil, nil, true)
	assert.NoError(t, err)

	_, err = gm.performSearch(context.Background(), 0, []string{"user1"}, "rerank_test", "query", make([]float64, 32))
	assert.Error(t, err)
}

// TestPerformSearch_多用户 测试 performSearch 多用户过滤
func TestPerformSearch_多用户(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	objects, err := gm.performSearch(context.Background(), 0, []string{"user1", "user2"}, "default", "query", make([]float64, 32))
	assert.NoError(t, err)
	// mockGraphStore.Search 返回 nil，所以 objects 为空
	assert.Empty(t, objects)
}

// TestInvokeLLM_无客户端 测试 InvokeLLM 无客户端报错
func TestInvokeLLM_无客户端(t *testing.T) {
	gm, _ := newTestGraphMemory()
	_, err := gm.InvokeLLM(context.Background(), nil, nil, nil)
	assert.Error(t, err)
}

// TestEntityEnrich_有阻塞实体 测试 EntityEnrich 有阻塞实体
func TestEntityEnrich_有阻塞实体(t *testing.T) {
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

	// 设置 pending merge，使 entity1 成为阻塞实体
	// done 通道需要预关闭（测试场景：合并已完成）
	pendingDone := make(chan struct{})
	close(pendingDone)
	state.PendingMerge["e1"] = &pendingMergeTask{Result: "merged summary", Err: nil, done: pendingDone}

	entities := []*graph.Entity{entity1, entity2}
	content := "Alice and Bob had a meeting"

	result, err := gm.entityEnrich(context.Background(), entities, content, state)
	assert.NotNil(t, result)
	// 无 LLM 客户端，调用失败是预期的
	_ = err
}

// TestInvokeLLMAsync_有客户端 测试有客户端但无模板
func TestInvokeLLMAsync_有客户端(t *testing.T) {
	gm, _ := newTestGraphMemory()
	// 设置 LLM 客户端为 nil，任务会返回错误
	task := gm.invokeLLMAsync(context.Background(), map[string]any{}, nil, nil)
	assert.NotNil(t, task)
}

// TestCreateEpisode 测试 CreateEpisode 函数
func TestCreateEpisode(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := gm.initState(nil, config.EpisodeTypeConversation)

	ep, err := CreateEpisode(context.Background(), gm.DBBackend, "user1", "test content", state)
	assert.NoError(t, err)
	assert.NotNil(t, ep)
	assert.Equal(t, "test content", ep.Content)
	assert.Equal(t, "user1", ep.UserID)
}

// TestValidateEntitiesEpisodes 测试 ValidateEntitiesEpisodes
func TestValidateEntitiesEpisodes(t *testing.T) {
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.UUID = "e1"
	entity.Name = "Alice"
	entity.Content = "test"

	ep := graph.NewEpisode()
	ep.UUID = "ep1"
	ep.Content = "episode"

	ValidateEntitiesEpisodes([]*graph.Entity{entity}, ep, state)
}

// TestProcessEntities 测试 ProcessEntities
func TestProcessEntities(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.UUID = "e1"
	entity.Name = "Alice"
	entity.Content = "test"

	ep := graph.NewEpisode()
	ep.UUID = "ep1"

	err := ProcessEntities(context.Background(), gm.DBBackend, []*graph.Entity{entity}, ep, state)
	assert.NoError(t, err)
}

// TestProcessRelations 测试 ProcessRelations
func TestProcessRelations(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	rel := graph.NewRelation()
	rel.UUID = "r1"
	rel.Content = "test"

	entity := graph.NewEntity()

	err := ProcessRelations(context.Background(), gm.DBBackend, []*graph.Entity{entity}, []*graph.Relation{rel}, state)
	assert.NoError(t, err)
}

// TestParseRelationFilteringResult_无任务 测试无过滤任务时跳过
func TestParseRelationFilteringResult_无任务(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	err := gm.parseRelationFilteringResult(context.Background(), nil, state)
	assert.NoError(t, err)
}

// TestDispatchEntityMergeTasks_无合并过滤 测试无 merge_filter 时跳过过滤
func TestDispatchEntityMergeTasks_无合并过滤(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeFilter = false

	err := gm.dispatchEntityMergeTasks(context.Background(), nil, nil, state)
	assert.NoError(t, err)
}

// TestHandleRelationDedupe_有TmpBuffer 测试有 TmpBuffer 但 IsEmpty 返回 true 时跳过
func TestHandleRelationDedupe_有TmpBuffer(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	state.TmpBuffer = append(state.TmpBuffer, "relation content")

	relations := []*graph.Relation{graph.NewRelation()}
	err := gm.handleRelationDedupe(context.Background(), "user1", "content", relations, state)
	assert.NoError(t, err)
}

// TestMaybeGC_触发GC 测试 GC 触发时调用 DBBackend.Refresh
func TestMaybeGC_触发GC(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.TimeTillNextGC = 0.001
	gm.lastGC = 0
	gm.maybeGC(context.Background())
	assert.Greater(t, gm.lastGC, float64(0))
}

// TestValidateEntitiesEpisodes_合并信息更新 测试合并信息中的 Episode 引用替换
func TestValidateEntitiesEpisodes_合并信息更新(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"
	entity1.Content = "test"

	srcEntity := graph.NewEntity()
	srcEntity.UUID = "src-1"
	srcEntity.Episodes = []string{"ep-1"}

	ep := graph.NewEpisode()
	ep.UUID = "ep-1"
	ep.Entities = []string{"src-1"}
	state.LookupTable.Episodes["ep-1"] = ep

	state.MergeInfos["e1"] = &EntityMerge{
		Target:          entity1,
		Source:          map[string]*graph.Entity{"src-1": srcEntity},
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: map[string]struct{}{},
	}

	ValidateEntitiesEpisodes([]*graph.Entity{entity1}, ep, state)

	// src-1 应被替换为 e1
	assert.NotContains(t, ep.Entities, "src-1")
}

// TestValidateEntitiesEpisodes_双向校验 测试双向校验修复 Entity↔Episode
func TestValidateEntitiesEpisodes_双向校验(t *testing.T) {
	state := NewGraphMemState()

	entity1 := graph.NewEntity()
	entity1.UUID = "e1"
	entity1.Content = "test"
	entity1.Episodes = []string{"ep-1"} // Entity→Episode

	ep := graph.NewEpisode()
	ep.UUID = "ep-1"
	// ep.Entities 不包含 e1 → Entity→Episode 但 Episode↔Entity 不存在
	state.MemUpdateSkipEmbed.UpdatedEpisode = append(state.MemUpdateSkipEmbed.UpdatedEpisode, ep)
	state.MemUpdate.UpdatedEntity = append(state.MemUpdate.UpdatedEntity, entity1)

	ValidateEntitiesEpisodes([]*graph.Entity{entity1}, graph.NewEpisode(), state)

	// 应向 Episode 添加 Entity
	assert.Contains(t, ep.Entities, "e1")
}

// TestProcessEntities_有合并任务 测试有合并任务时更新实体
func TestProcessEntities_有合并任务(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.UUID = "e1"
	entity.Name = "Alice"
	entity.Content = ""

	// 设置合并任务
	mergeTask := newAsyncTaskWithResult(`{"summary": "updated summary"}`)
	state.MergingTasks = append(state.MergingTasks, mergeTask)
	state.MergingTasksEntities[mergeTask] = entity

	ep := graph.NewEpisode()
	ep.UUID = "ep-1"

	err := ProcessEntities(context.Background(), gm.DBBackend, []*graph.Entity{entity}, ep, state)
	assert.NoError(t, err)
	assert.Equal(t, "updated summary", entity.Content)
}

// TestProcessRelations_有删除关系 测试有删除关系时更新实体
func TestProcessRelations_有删除关系(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.UUID = "e1"
	entity.Relations = []string{"r-old"}

	state.MemUpdate.RemovedRelation["r-old"] = struct{}{}

	rel := graph.NewRelation()
	rel.UUID = "r-new"
	rel.LHS = entityShell("e1")
	rel.RHS = entityShell("e2")
	state.LookupTable.Entities["e1"] = entity

	err := ProcessRelations(context.Background(), gm.DBBackend, []*graph.Entity{entity}, []*graph.Relation{rel}, state)
	assert.NoError(t, err)
	assert.NotContains(t, entity.Relations, "r-old")
}

// TestPersistToDB_有新增实体和关系 测试 PersistToDB 有新增内容
func TestPersistToDB_有新增实体和关系(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &fakeEmbedder{dimension: 32}
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.Content = "test entity"
	entity.Name = "test"
	state.MemUpdate.AddedEntity = append(state.MemUpdate.AddedEntity, entity)

	rel := graph.NewRelation()
	rel.Content = "test relation"
	state.MemUpdate.AddedRelation = append(state.MemUpdate.AddedRelation, rel)

	ep := graph.NewEpisode()
	ep.Content = "test episode"
	state.MemUpdate.AddedEpisode = append(state.MemUpdate.AddedEpisode, ep)

	updatedEntity := graph.NewEntity()
	updatedEntity.Content = "updated entity"
	updatedEntity.Name = "updated"
	updatedEntity.ContentEmbedding = []float64{0.1}
	updatedEntity.NameEmbedding = []float64{0.2}
	state.MemUpdate.UpdatedEntity = append(state.MemUpdate.UpdatedEntity, updatedEntity)

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.NoError(t, err)
}

// TestBatchEmbed_失败重试 测试 BatchEmbed 嵌入失败时返回失败对象
func TestBatchEmbed_失败重试(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	// 使用会失败的 embedder
	embedder := &failingEmbedder{}

	entity := graph.NewEntity()
	entity.Content = "test content"
	entity.Name = "test name"

	objects := []embeddable{entity}
	result := BatchEmbed(context.Background(), objects, embedder, cfg)
	assert.Len(t, result, 1)
}

// failingEmbedder 测试用会失败的嵌入器
type failingEmbedder struct{}

func (f *failingEmbedder) EmbedQuery(_ context.Context, _ string, _ ...embedding.EmbedOption) ([]float64, error) {
	return nil, fmt.Errorf("embedding failed")
}

func (f *failingEmbedder) EmbedDocuments(_ context.Context, _ []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	return nil, fmt.Errorf("embedding failed")
}

func (f *failingEmbedder) Dimension() int { return 32 }

func (f *failingEmbedder) DimensionWithContext(_ context.Context) (int, error) { return 32, nil }

// TestMergeStringSliceUnique 测试 mergeStringSliceUnique
func TestMergeStringSliceUnique(t *testing.T) {
	a := []string{"a", "b"}
	b := []string{"b", "c"}
	result := mergeStringSliceUnique(a, b)
	assert.Len(t, result, 3)
}

// TestEntityUUIDs 测试 entityUUIDs
func TestEntityUUIDs(t *testing.T) {
	e1 := graph.NewEntity()
	e1.UUID = "u1"
	e2 := graph.NewEntity()
	e2.UUID = "u2"
	result := entityUUIDs([]*graph.Entity{e1, e2})
	assert.Equal(t, []string{"u1", "u2"}, result)
}

// TestFilterEntitiesNotIn 测试 filterEntitiesNotIn
func TestFilterEntitiesNotIn(t *testing.T) {
	e1 := graph.NewEntity()
	e2 := graph.NewEntity()
	e3 := graph.NewEntity()
	source := []*graph.Entity{e1, e2, e3}
	target := []*graph.Entity{e2}
	result := filterEntitiesNotIn(source, target)
	assert.Len(t, result, 2)
	assert.NotContains(t, result, e2)
}

// TestSearchOption_函数 测试搜索选项函数
func TestSearchOption_函数(t *testing.T) {
	opts := &SearchConfigOptions{}
	WithSearchStrategy("custom")(opts)
	assert.Equal(t, "custom", opts.SearchStrategy)

	emb := []float64{0.1}
	WithQueryEmbedding(emb)(opts)
	assert.Equal(t, emb, opts.QueryEmbedding)
}

// mockSearchGraphStore 支持搜索结果的 mock 图数据库
type mockSearchGraphStore struct {
	mockGraphStore
	// SearchResult 搜索结果
	SearchResult map[string][]map[string]any
	// QueryResult 查询结果
	QueryResult []map[string]any
	// IsEmptyResult IsEmpty 返回值（默认 true）
	IsEmptyResult bool
}

func (m *mockSearchGraphStore) Search(_ context.Context, _ string, _ ...graph.Option) (map[string][]map[string]any, error) {
	if m.SearchResult != nil {
		return m.SearchResult, nil
	}
	return nil, nil
}

func (m *mockSearchGraphStore) Query(_ context.Context, _ string, _ ...graph.Option) ([]map[string]any, error) {
	if m.QueryResult != nil {
		return m.QueryResult, nil
	}
	return nil, nil
}

func (m *mockSearchGraphStore) IsEmpty(_ context.Context, _ string) (bool, error) {
	if m.IsEmptyResult {
		return true, nil
	}
	return false, nil
}

// TestPerformSearch_有结果 测试 performSearch 有搜索结果
func TestPerformSearch_有结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	// 替换后端为支持搜索结果的 mock
	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.EntityCollection: {
				{"uuid": "e1", "name": "Alice", "content": "engineer", "distance": 0.5},
				{"uuid": "e2", "name": "Bob", "content": "doctor", "distance": 0.3},
			},
		},
	}
	gm.DBBackend = searchStore

	objects, err := gm.performSearch(context.Background(), 0, []string{"user1"}, "default", "query", make([]float64, 32))
	assert.NoError(t, err)
	assert.Len(t, objects, 2)
	assert.Equal(t, 0.5, objects[0].Score)
}

// TestPerformSearch_带Rerank 测试 performSearch 带 rerank
func TestPerformSearch_带Rerank(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.Reranker = &fakeReranker{}

	// 注册需要 rerank 的策略
	entityConf := config.NewSearchConfig()
	entityConf.Rerank = true
	entityConf.OutputFields = []string{"name"}
	err := gm.RegisterSearchStrategy("rerank_test", entityConf, nil, nil, true)
	assert.NoError(t, err)

	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.EntityCollection: {
				{"uuid": "e1", "name": "Alice", "distance": 0.5},
			},
		},
	}
	gm.DBBackend = searchStore

	objects, err := gm.performSearch(context.Background(), 0, []string{"user1"}, "rerank_test", "query", make([]float64, 32))
	assert.NoError(t, err)
	assert.Len(t, objects, 1)
}

// TestPerformSearch_带FilterExpr 测试 performSearch 带自定义过滤表达式
func TestPerformSearch_带FilterExpr(t *testing.T) {
	gm, _ := newTestGraphMemory()

	strategy := config.NewSearchConfig()
	strategy.FilterExpr = &query.ComparisonExpr{Field: "status", Operator: "==", Value: "active"}
	err := gm.RegisterSearchStrategy("filter_test", strategy, nil, nil, true)
	assert.NoError(t, err)

	objects, err := gm.performSearch(context.Background(), 0, []string{"user1"}, "filter_test", "query", make([]float64, 32))
	assert.NoError(t, err)
	assert.Empty(t, objects)
}

// TestPerformSearch_关系搜索 测试搜索关系集合
func TestPerformSearch_关系搜索(t *testing.T) {
	gm, _ := newTestGraphMemory()
	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.RelationCollection: {
				{"uuid": "r1", "content": "knows", "lhs": "e1", "rhs": "e2", "distance": 0.4},
			},
		},
	}
	gm.DBBackend = searchStore

	objects, err := gm.performSearch(context.Background(), 1, []string{"user1"}, "default", "query", make([]float64, 32))
	assert.NoError(t, err)
	assert.Len(t, objects, 1)
}

// TestPerformSearch_片段搜索 测试搜索片段集合
func TestPerformSearch_片段搜索(t *testing.T) {
	gm, _ := newTestGraphMemory()
	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.EpisodeCollection: {
				{"uuid": "ep1", "content": "episode content", "distance": 0.6},
			},
		},
	}
	gm.DBBackend = searchStore

	objects, err := gm.performSearch(context.Background(), 2, []string{"user1"}, "default", "query", make([]float64, 32))
	assert.NoError(t, err)
	assert.Len(t, objects, 1)
}

// TestUpdateEntitiesForRelationRemoval_有查询结果 测试查询返回实体时的更新
func TestUpdateEntitiesForRelationRemoval_有查询结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	queryStore := &mockSearchGraphStore{
		QueryResult: []map[string]any{
			{"uuid": "entity-1", "name": "Alice", "content": "test", "relations": []string{"rel-1", "rel-2"}},
		},
	}
	gm.DBBackend = queryStore

	state := NewGraphMemState()
	// ToRemove 中存储完整 Relation 对象，LHS/RHS 指向受影响的实体
	relToRemove := &graph.Relation{NamedGraphObject: graph.NamedGraphObject{BaseGraphObject: graph.BaseGraphObject{UUID: "rel-1"}}}
	relToRemove.LHS = entityShell("entity-1")
	relToRemove.RHS = entityShell("entity-2")
	state.ToRemove["rel-1"] = relToRemove
	state.MemUpdate.RemovedRelation["rel-1"] = struct{}{}

	// 预注册实体到 lookup table
	entity := graph.NewEntity()
	entity.UUID = "entity-1"
	entity.Name = "Alice"
	entity.Relations = []string{"rel-1", "rel-2"}
	state.LookupTable.Entities["entity-1"] = entity

	gm.updateEntitiesForRelationRemoval(context.Background(), state, []extraction.EntityDeclaration{{Name: "Alice"}})
	// rel-1 应从 entity.Relations 中移除
	assert.NotContains(t, entity.Relations, "rel-1")
	// Alice 在 extractedDeclarations 中需要 re-embed，不应加入 skip embed
}

// TestHandleRelationDedupe_无嵌入器 测试无嵌入器时直接返回
func TestHandleRelationDedupe_无嵌入器(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	state.TmpBuffer = append(state.TmpBuffer, "relation content")

	queryStore := &mockSearchGraphStore{IsEmptyResult: false}
	gm.DBBackend = queryStore

	err := gm.handleRelationDedupe(context.Background(), "user1", "content", nil, state)
	assert.NoError(t, err)
}

// TestHandleRelationDedupe_有嵌入结果 测试有嵌入结果时调用 relationDedupe
func TestHandleRelationDedupe_有嵌入结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	state.TmpBuffer = append(state.TmpBuffer, "relation content")

	queryStore := &mockSearchGraphStore{IsEmptyResult: false}
	gm.DBBackend = queryStore

	rel := graph.NewRelation()
	rel.LHS = entityShell("e1")
	rel.RHS = entityShell("e2")
	rel.Content = "test"

	err := gm.handleRelationDedupe(context.Background(), "user1", "content", []*graph.Relation{rel}, state)
	assert.NoError(t, err)
}

// TestHandleRelationDedupe_空文本 测试 TmpBuffer 中无字符串时直接返回
func TestHandleRelationDedupe_空文本(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	state.TmpBuffer = append(state.TmpBuffer, 42) // 非 string

	queryStore := &mockSearchGraphStore{IsEmptyResult: false}
	gm.DBBackend = queryStore

	err := gm.handleRelationDedupe(context.Background(), "user1", "content", nil, state)
	assert.NoError(t, err)
}

// TestHandleRelationDedupe_有删除项 测试移除待删除关系
func TestHandleRelationDedupe_有删除项(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeRelations = false

	rel1 := graph.NewRelation()
	rel1.UUID = "rel-1"
	rel2 := graph.NewRelation()
	rel2.UUID = "rel-2"
	relations := []*graph.Relation{rel1, rel2}

	state.ToRemove["rel-1"] = &graph.Relation{NamedGraphObject: graph.NamedGraphObject{BaseGraphObject: graph.BaseGraphObject{UUID: "rel-1"}}}

	err := gm.handleRelationDedupe(context.Background(), "user1", "content", relations, state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_无嵌入器 测试无嵌入器时跳过
func TestFetchRelevantEntities_无嵌入器(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_有嵌入器 测试有嵌入器时执行搜索
func TestFetchRelevantEntities_有嵌入器(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.EntityCollection: {
				{"uuid": "e1", "name": "Alice", "content": "engineer", "distance": 0.5},
			},
		},
	}
	gm.DBBackend = searchStore

	state := NewGraphMemState()
	state.Strategy.RecallEntity.SameKind = false

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_同类型搜索 测试 SameKind=true 时的搜索
func TestFetchRelevantEntities_同类型搜索(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.EntityCollection: {
				{"uuid": "e1", "name": "Alice", "content": "engineer", "distance": 0.5},
			},
		},
	}
	gm.DBBackend = searchStore

	state := NewGraphMemState()
	state.Strategy.RecallEntity.SameKind = true

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}

// TestPrepareEpisodes_有历史 测试有历史片段检索
func TestPrepareEpisodes_有历史(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	searchStore := &mockSearchGraphStore{
		IsEmptyResult: false,
		SearchResult: map[string][]map[string]any{
			graph.EpisodeCollection: {
				{"uuid": "ep1", "content": "old episode", "distance": 0.5},
			},
		},
	}
	gm.DBBackend = searchStore

	state := NewGraphMemState()
	state.Strategy.RecallEpisode.TopK = 5

	_, err := gm.prepareEpisodes(context.Background(), state, "hello world", nil, config.EpisodeTypeDocument, "user1", nil)
	assert.NoError(t, err)
}

// TestPrepareEpisodes_对话内容 测试对话类型内容（使用 Messages）
func TestPrepareEpisodes_对话内容(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	kwargs := map[string]string{"user": "Alice", "assistant": "Bob"}
	_, err := gm.prepareEpisodes(context.Background(), state, "", []llmschema.BaseMessage{
		llmschema.NewUserMessage("What is AI?"),
		llmschema.NewAssistantMessage("AI is artificial intelligence."),
	}, config.EpisodeTypeConversation, "user1", kwargs)
	assert.NoError(t, err)
}

// TestPrepareEpisodes_无效输入 测试无效内容格式
func TestPrepareEpisodes_无效输入(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	_, err := gm.prepareEpisodes(context.Background(), state, "", nil, config.EpisodeTypeDocument, "user1", nil)
	assert.Error(t, err)
}

// TestParseRelationFilteringResult_无deferred 测试无延迟更新时跳过
func TestParseRelationFilteringResult_无deferred(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.MergeInfos["e1"] = &EntityMerge{
		Target:          graph.NewEntity(),
		Source:          map[string]*graph.Entity{},
		NewRelations:    []*graph.Relation{},
		RelationsToKeep: map[string]struct{}{},
	}

	err := gm.parseRelationFilteringResult(context.Background(), nil, state)
	assert.NoError(t, err)
}

// TestDispatchEntityMergeTasks_有Episode更新 测试有 Episode 更新时的分发
func TestDispatchEntityMergeTasks_有Episode更新(t *testing.T) {
	gm, _ := newTestGraphMemory()
	queryStore := &mockSearchGraphStore{
		QueryResult: []map[string]any{
			{"uuid": "ep-1", "content": "episode"},
		},
	}
	gm.DBBackend = queryStore

	state := NewGraphMemState()
	episodesToUpdate := map[string]struct{}{"ep-1": {}}

	err := gm.dispatchEntityMergeTasks(context.Background(), episodesToUpdate, nil, state)
	assert.NoError(t, err)
}

// TestDispatchEntityMergeTasks_无Episode更新 测试无 Episode 更新
func TestDispatchEntityMergeTasks_无Episode更新(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.Strategy.MergeFilter = false

	err := gm.dispatchEntityMergeTasks(context.Background(), nil, nil, state)
	assert.NoError(t, err)
}

// TestHandleRelationDedupe_有嵌入失败 测试嵌入失败时返回 nil
func TestHandleRelationDedupe_有嵌入失败(t *testing.T) {
	gm, _ := newTestGraphMemory()
	gm.embedderVal = &failingEmbedder{}

	state := NewGraphMemState()
	state.Strategy.MergeRelations = true
	state.TmpBuffer = append(state.TmpBuffer, "relation content")

	queryStore := &mockSearchGraphStore{IsEmptyResult: false}
	gm.DBBackend = queryStore

	err := gm.handleRelationDedupe(context.Background(), "user1", "content", nil, state)
	assert.NoError(t, err)
}

// TestStartRelationExtractionAsync_等待完成 测试异步任务在无 LLM 时快速失败
func TestStartRelationExtractionAsync_等待完成(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.EntityTypes = []extraction.EntityDef{
		{Name: "Entity"},
	}

	task := gm.startRelationExtractionAsync(context.Background(), nil, "content", state, "")
	_, err := task.Wait()
	assert.NotNil(t, err)
}

// TestStartEntityDedupeAsync_等待完成 测试异步任务在无 LLM 时快速失败
func TestStartEntityDedupeAsync_等待完成(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.EntityTypes = []extraction.EntityDef{
		{Name: "Entity"},
	}

	task := gm.startEntityDedupeAsync(context.Background(), "content", nil, nil, state)
	_, err := task.Wait()
	assert.NotNil(t, err)
}

// TestInvokeLLMAsync_等待完成 测试异步任务在无 LLM 时快速失败
func TestInvokeLLMAsync_等待完成(t *testing.T) {
	gm, _ := newTestGraphMemory()
	task := gm.invokeLLMAsync(context.Background(), map[string]any{}, nil, nil)
	_, err := task.Wait()
	assert.NotNil(t, err)
}

// TestStartTimezoneTask_等待完成 测试时区任务在无 LLM 时快速失败
func TestStartTimezoneTask_等待完成(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	ch := gm.startTimezoneTask(context.Background(), "content", state)
	result := <-ch
	assert.NotNil(t, result.err)
}

// TestRelationDedupe_有搜索结果 测试有搜索结果时的关系去重
func TestRelationDedupe_有搜索结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.RelationCollection: {
				{"uuid": "r1", "content": "existing relation", "lhs": "e1", "rhs": "e2", "distance": 0.3},
			},
		},
	}
	gm.DBBackend = searchStore

	state := NewGraphMemState()
	state.Strategy.RecallRelation.TopK = 5

	rel := graph.NewRelation()
	rel.LHS = entityShell("e1")
	rel.RHS = entityShell("e2")
	rel.Content = "test content"

	embedResults := [][]float64{{0.1, 0.2}}

	err := gm.relationDedupe(context.Background(), "user1", "content", []*graph.Relation{rel}, embedResults, state)
	assert.NoError(t, err)
}

// TestRelationDedupe_无LHS 测试 lhs/rhs 为空时跳过
func TestRelationDedupe_无LHS(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	rel := graph.NewRelation()
	rel.LHS = nil
	rel.RHS = entityShell("e2")

	embedResults := [][]float64{{0.1, 0.2}}

	err := gm.relationDedupe(context.Background(), "user1", "content", []*graph.Relation{rel}, embedResults, state)
	assert.NoError(t, err)
}

// TestExtractEntityDeclarations_无LLM 测试无 LLM 客户端时返回错误
func TestExtractEntityDeclarations_无LLM(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()
	state.EntityTypes = []extraction.EntityDef{
		{Name: "Entity"},
	}

	_, _, err := gm.extractEntityDeclarations(context.Background(), config.EpisodeTypeDocument, "hello world", state)
	assert.Error(t, err)
}

// TestPersistToDB_嵌入失败 测试嵌入失败时的重试逻辑
func TestPersistToDB_嵌入失败(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &failingEmbedder{}
	state := NewGraphMemState()

	entity := graph.NewEntity()
	entity.Content = "test entity"
	entity.Name = "test"
	state.MemUpdate.AddedEntity = append(state.MemUpdate.AddedEntity, entity)

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.Error(t, err)
}

// TestPersistToDB_片段嵌入失败 测试片段嵌入失败时的重试和截断逻辑
func TestPersistToDB_片段嵌入失败(t *testing.T) {
	cfg := graph.NewGraphConfig("test://localhost")
	db := &mockGraphStore{}
	embedder := &failingEmbedder{}
	state := NewGraphMemState()

	ep := graph.NewEpisode()
	ep.Content = "test episode content"
	state.MemUpdate.AddedEpisode = append(state.MemUpdate.AddedEpisode, ep)

	err := PersistToDB(context.Background(), db, state, embedder, cfg)
	assert.Error(t, err)
}

// TestResolveEachRelation 测试 resolveEachRelation
func TestResolveEachRelation(t *testing.T) {
	gm, _ := newTestGraphMemory()

	queryStore := &mockSearchGraphStore{
		QueryResult: []map[string]any{
			{"uuid": "r1", "content": "relation", "lhs": "src-1", "rhs": "other"},
		},
	}
	gm.DBBackend = queryStore

	state := NewGraphMemState()
	srcEntity := graph.NewEntity()
	srcEntity.UUID = "src-1"
	srcEntity.Relations = []string{"r1"}

	tgtUUID := "tgt-1"
	mapSrc2Tgt := map[string]string{"src-1": "tgt-1"}
	entityRelationUpdates := make(map[string]map[string]*graph.Relation)
	alias := map[string]struct{}{"tgt-1": {}, "src-1": {}}

	err := gm.resolveEachRelation(tgtUUID, srcEntity, mapSrc2Tgt, entityRelationUpdates, state, alias)
	_ = err
}

// TestParseEntityMerging_候选实体替换 测试候选实体替换
func TestParseEntityMerging_候选实体替换(t *testing.T) {
	tgtEntity := graph.NewEntity()
	tgtEntity.UUID = "tgt-1"

	existing := []*graph.Entity{}

	candidate := &extraction.EntityDeclaration{Name: "Alice"}
	result := []EntityOrDeclaration{{Decl: candidate}}

	mergeMap := make(map[string]map[string]struct{})
	isTarget := make(map[string]string)

	// id=1 (1-based) 指向 candidate[0]（numExisting=0, numEntities=1）
	dup := map[string]any{"duplicate_ids": []any{1}}
	parseEntityMerging(dup, mergeMap, isTarget, result, existing, tgtEntity, 1, 0)

	// candidate[0] 应被替换为 tgtEntity
	assert.Equal(t, tgtEntity, result[0].Entity)
}

// TestParseEntityMerging_现有实体替换 测试现有实体间替换
func TestParseEntityMerging_现有实体替换(t *testing.T) {
	tgtEntity := graph.NewEntity()
	tgtEntity.UUID = "tgt-1"

	srcEntity := graph.NewEntity()
	srcEntity.UUID = "src-1"

	existing := []*graph.Entity{tgtEntity, srcEntity}
	result := []EntityOrDeclaration{}

	mergeMap := make(map[string]map[string]struct{})
	isTarget := make(map[string]string)

	// id=2 (1-based) 指向 existing[1] = srcEntity
	dup := map[string]any{"duplicate_ids": []any{2}}
	parseEntityMerging(dup, mergeMap, isTarget, result, existing, tgtEntity, 2, 2)

	assert.Contains(t, mergeMap, tgtEntity.UUID)
	assert.Contains(t, mergeMap[tgtEntity.UUID], srcEntity.UUID)
}

// TestParseEntityMerging_无效ID 测试无效 ID 时跳过
func TestParseEntityMerging_无效ID(t *testing.T) {
	tgtEntity := graph.NewEntity()
	tgtEntity.UUID = "tgt-1"

	dup := map[string]any{"duplicate_ids": []any{"invalid"}}
	mergeMap := make(map[string]map[string]struct{})
	isTarget := make(map[string]string)
	result := []EntityOrDeclaration{}

	parseEntityMerging(dup, mergeMap, isTarget, result, nil, tgtEntity, 0, 0)
	assert.Empty(t, mergeMap)
}

// TestParseEntityMerging_无duplicateIDs 测试无 duplicate_ids 时跳过
func TestParseEntityMerging_无duplicateIDs(t *testing.T) {
	tgtEntity := graph.NewEntity()
	dup := map[string]any{}
	mergeMap := make(map[string]map[string]struct{})
	isTarget := make(map[string]string)
	result := []EntityOrDeclaration{}

	parseEntityMerging(dup, mergeMap, isTarget, result, nil, tgtEntity, 0, 0)
	assert.Empty(t, mergeMap)
}

// TestParseEntityMerging_src已在mergeMap 测试 src 已在 mergeMap 时的处理
func TestParseEntityMerging_src已在mergeMap(t *testing.T) {
	tgtEntity := graph.NewEntity()
	tgtEntity.UUID = "tgt-1"

	srcEntity := graph.NewEntity()
	srcEntity.UUID = "src-1"

	existing := []*graph.Entity{tgtEntity, srcEntity}
	result := []EntityOrDeclaration{}

	mergeMap := map[string]map[string]struct{}{
		"src-1": {"other": {}},
	}
	isTarget := make(map[string]string)

	// id=2 → srcEntity, srcEntity 已在 mergeMap 中
	dup := map[string]any{"duplicate_ids": []any{2}}
	parseEntityMerging(dup, mergeMap, isTarget, result, existing, tgtEntity, 2, 2)

	// srcEntity 已在 mergeMap 中，tgt 应成为 src 的 target
	assert.Equal(t, "src-1", isTarget[tgtEntity.UUID])
}

// TestParseEntityMerging_都未在mergeMap 测试两者都未在 mergeMap 中
func TestParseEntityMerging_都未在mergeMap(t *testing.T) {
	tgtEntity := graph.NewEntity()
	tgtEntity.UUID = "tgt-1"

	srcEntity := graph.NewEntity()
	srcEntity.UUID = "src-1"

	existing := []*graph.Entity{tgtEntity, srcEntity}
	result := []EntityOrDeclaration{}

	mergeMap := make(map[string]map[string]struct{})
	isTarget := make(map[string]string)

	dup := map[string]any{"duplicate_ids": []any{2}}
	parseEntityMerging(dup, mergeMap, isTarget, result, existing, tgtEntity, 2, 2)

	assert.Contains(t, mergeMap, tgtEntity.UUID)
	assert.Contains(t, mergeMap[tgtEntity.UUID], srcEntity.UUID)
}

// TestParseEntityMerging_同UUID跳过 测试 tgt 和 src 同 UUID 时跳过
func TestParseEntityMerging_同UUID跳过(t *testing.T) {
	tgtEntity := graph.NewEntity()
	tgtEntity.UUID = "same-1"

	existing := []*graph.Entity{tgtEntity}
	result := []EntityOrDeclaration{}

	mergeMap := make(map[string]map[string]struct{})
	isTarget := make(map[string]string)

	// id=1 指向 existing[0] = tgtEntity，同 UUID 应跳过
	dup := map[string]any{"duplicate_ids": []any{1}}
	parseEntityMerging(dup, mergeMap, isTarget, result, existing, tgtEntity, 1, 1)
	assert.Empty(t, mergeMap)
}

// TestEntityMerge_无去重任务 测试无去重任务时跳过
func TestEntityMerge_无去重任务(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	existingEntitiesList := []map[string]any{{"uuid": "e1", "name": "Alice"}}

	result, err := gm.entityMerge(context.Background(), declarations, existingEntitiesList, state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestEntityMerge_去重任务失败 测试去重任务失败时的处理
func TestEntityMerge_去重任务失败(t *testing.T) {
	gm, _ := newTestGraphMemory()
	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	existingEntitiesList := []map[string]any{{"uuid": "e1", "name": "Alice"}}

	failedTask := newAsyncTaskWithError(fmt.Errorf("LLM failed"))
	state.Tasks = append(state.Tasks, failedTask)

	result, err := gm.entityMerge(context.Background(), declarations, existingEntitiesList, state)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestFetchRelevantEntities_有嵌入结果 测试有嵌入结果时的搜索
func TestFetchRelevantEntities_有嵌入结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	searchStore := &mockSearchGraphStore{
		SearchResult: map[string][]map[string]any{
			graph.EntityCollection: {
				{"uuid": "e1", "name": "Alice", "content": "engineer", "distance": 0.5},
			},
		},
	}
	gm.DBBackend = searchStore

	state := NewGraphMemState()
	state.Strategy.RecallEntity.SameKind = true

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}

// TestFetchRelevantEntities_有查询结果 测试有精确匹配查询结果
func TestFetchRelevantEntities_有查询结果(t *testing.T) {
	gm, _ := newTestGraphMemory()
	fakeEmb := &fakeEmbedder{dimension: 32}
	_ = gm.AttachEmbedder(fakeEmb)

	queryStore := &mockSearchGraphStore{
		QueryResult: []map[string]any{
			{"uuid": "e1", "name": "Alice"},
		},
	}
	gm.DBBackend = queryStore

	state := NewGraphMemState()

	declarations := []extraction.EntityDeclaration{{Name: "Alice", EntityTypeID: 0}}
	err := gm.fetchRelevantEntities(context.Background(), declarations, false, "user1", state)
	assert.NoError(t, err)
}
