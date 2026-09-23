// Package graph_memory 提供图记忆的核心输入校验和辅助函数。
//
// 包含添加记忆和搜索记忆的输入校验，以及消息转换、实体更新、
// LLM 调用参数组装和图记忆状态结构等工具函数。
//
// 文件目录：
//
//	graph_memory/
//	├── doc.go               # 包文档
//	├── states.go            # 图记忆状态结构（LookupTables/GraphMemUpdate/GraphMemState 等）
//	├── utils.go             # 消息转换、实体更新、参数组装
//	└── validate_input.go    # 输入校验函数
//
// 对应 Python 代码：openjiuwen/core/memory/graph/graph_memory/
package graph_memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// embeddable 可嵌入接口，图对象通过 EmbedTasks 方法提供嵌入任务
type embeddable interface {
	EmbedTasks() []graph.EmbedTask
}

// LookupTables UUID → Entity/Relation/Episode 的去重查找表
//
// Python: LookupTables (states.py)
type LookupTables struct {
	// Entities UUID → Entity 映射
	Entities map[string]*graph.Entity
	// Relations UUID → Relation 映射
	Relations map[string]*graph.Relation
	// Episodes UUID → Episode 映射
	Episodes map[string]*graph.Episode
}

// EntityMerge 实体合并信息
//
// Python: EntityMerge (states.py)
type EntityMerge struct {
	// Target 合并目标实体
	Target *graph.Entity
	// Source 合并来源实体（UUID → Entity）
	Source map[string]*graph.Entity
	// NewRelations 合并产生的新关系
	NewRelations []*graph.Relation
	// RelationsToKeep 保留的关系UUID集合
	RelationsToKeep map[string]struct{}
}

// GraphMemUpdate 图记忆增量变更累积器
//
// Python: GraphMemUpdate (states.py)
type GraphMemUpdate struct {
	// AddedEpisode 新增片段列表
	AddedEpisode []*graph.Episode
	// UpdatedEpisode 更新片段列表
	UpdatedEpisode []*graph.Episode
	// AddedEntity 新增实体列表
	AddedEntity []*graph.Entity
	// UpdatedEntity 更新实体列表
	UpdatedEntity []*graph.Entity
	// AddedRelation 新增关系列表
	AddedRelation []*graph.Relation
	// UpdatedRelation 更新关系列表
	UpdatedRelation []*graph.Relation
	// RemovedEntity 移除实体UUID集合
	RemovedEntity map[string]struct{}
	// RemovedRelation 移除关系UUID集合
	RemovedRelation map[string]struct{}
}

// GraphMemPrompting 图记忆提示词 Schema 配置
//
// Python: GraphMemPrompting (states.py)
type GraphMemPrompting struct {
	// SchemaEntityExtraction 实体摘要输出 Schema
	SchemaEntityExtraction map[string]any
	// SchemaEntityDedupe 实体去重输出 Schema
	SchemaEntityDedupe map[string]any
	// SchemaRelationMerge 关系合并输出 Schema
	SchemaRelationMerge map[string]any
	// SchemaRelationFilter 关系过滤输出 Schema
	SchemaRelationFilter map[string]any
	// Language 提示词语言
	Language string
	// EntityExtractionLanguage 实体抽取语言
	EntityExtractionLanguage string
	// RelationExtractionLanguage 关系抽取语言
	RelationExtractionLanguage string
	// EntityDedupeLanguage 实体去重语言
	EntityDedupeLanguage string
}

// EntityMerge 实体合并信息
//
// Python: relation_deferred_updates: dict[str, list[tuple[Relation, str, str]]]
type deferredRelationUpdate struct {
	// Relation 待更新的关系
	Relation *graph.Relation
	// Field 待更新的字段名
	Field string
	// Value 待设置的新值
	Value string
}

// toRemoveItem 待移除项（BaseGraphObject 的 UUID + ObjType）
//
// Python: to_remove: list[BaseGraphObject | str]
type toRemoveItem struct {
	// UUID 对象的 UUID
	UUID string
	// ObjType 对象类型
	ObjType string
}

// asyncTask 异步任务结果（对齐 Python asyncio.Task）
//
// Python 中 state.tasks 是 List[asyncio.Task]，Task 完成后通过 await 获取 AssistantMessage。
// Go 中使用 channel 模拟：任务在 goroutine 中执行，结果通过 channel 传回。
type asyncTask struct {
	// Result LLM 响应内容（对齐 Python: response.content）
	Result string
	// Err 任务执行错误
	Err error
}

// pendingMergeTask 待合并的阻塞任务
//
// Python: state.pending_merge[tgt.uuid] = task
type pendingMergeTask struct {
	// Result LLM 响应内容
	Result string
	// Err 任务执行错误
	Err error
}

// relationFilterTaskItem 关系过滤任务条目
//
// Python: relation_filter_tasks[task] = (tgt_entity, relation_list)
type relationFilterTaskItem struct {
	// TargetEntity 关联的目标实体
	TargetEntity *graph.Entity
	// Relations 待过滤的关系列表
	Relations []*graph.Relation
}

// GraphMemState 图记忆完整状态
//
// Python: GraphMemState (states.py)
type GraphMemState struct {
	// 任务缓冲区（对齐 Python: state.tasks: list[asyncio.Task]）
	Tasks                   []*asyncTask
	MergingTasks            []*asyncTask
	MergingTasksEntities    map[*asyncTask]*graph.Entity
	PendingMerge            map[string]*pendingMergeTask
	RelationDeferredUpdates map[string][]deferredRelationUpdate
	RelationFilterTasks     map[*asyncTask]*relationFilterTaskItem

	// 通用临时缓冲区
	ToRemove  []toRemoveItem
	TmpBuffer []any

	// 专用临时缓冲区
	UpdatedEntitiesInCurrentEp []*graph.Entity
	RetrievedEntities          map[string]*graph.Entity
	RetrievedRelations         map[string]*graph.Relation
	FaultyRelations            map[string]*graph.Relation
	MergeInfos                 map[string]*EntityMerge

	// 记忆变更（累积，最终统一刷入）
	MemUpdate          *GraphMemUpdate
	MemUpdateSkipEmbed *GraphMemUpdate

	// 共享变量/字典
	CurrentTimestamp   int64
	ReferenceTimestamp int64
	LookupTable        *LookupTables
	Extras             map[string]any
	Strategy           *config.AddMemStrategy
	Prompting          *GraphMemPrompting
	EntityTypes        []extraction.EntityDef
	EpisodeType        config.EpisodeType
	Content            string
	History            string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// （logComponent 定义在 utils.go 中）

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLookupTables 创建去重查找表
//
// Python: LookupTables()
func NewLookupTables() *LookupTables {
	return &LookupTables{
		Entities:  make(map[string]*graph.Entity),
		Relations: make(map[string]*graph.Relation),
		Episodes:  make(map[string]*graph.Episode),
	}
}

// GetEntity 去重安全地获取实体，若不存在则从输入构造
//
// Python: LookupTables.get_entity(input_obj)
func (l *LookupTables) GetEntity(input map[string]any) *graph.Entity {
	entityID, _ := input["uuid"].(string)
	if entity, ok := l.Entities[entityID]; ok {
		return entity
	}
	entity := entityFromMap(input)
	l.Entities[entityID] = entity
	return entity
}

// GetRelation 去重安全地获取关系，若不存在则从输入构造
//
// Python: LookupTables.get_relation(input_obj)
func (l *LookupTables) GetRelation(input map[string]any) *graph.Relation {
	relationID, _ := input["uuid"].(string)
	if relation, ok := l.Relations[relationID]; ok {
		return relation
	}
	relation := relationFromMap(input)
	l.Relations[relationID] = relation
	return relation
}

// GetEpisode 去重安全地获取片段，若不存在则从输入构造
//
// Python: LookupTables.get_episode(input_obj)
func (l *LookupTables) GetEpisode(input map[string]any) *graph.Episode {
	episodeID, _ := input["uuid"].(string)
	if episode, ok := l.Episodes[episodeID]; ok {
		return episode
	}
	episode := episodeFromMap(input)
	l.Episodes[episodeID] = episode
	return episode
}

// Clear 清除所有查找表引用，防止内存泄漏
//
// Python: LookupTables.clear()
func (l *LookupTables) Clear() {
	clearMap(l.Entities)
	clearMap(l.Relations)
	clearMap(l.Episodes)
}

// Clear 清除实体合并信息引用
//
// Python: EntityMerge.clear()
func (e *EntityMerge) Clear() {
	e.Target = nil
	clearMap(e.Source)
	e.NewRelations = nil
	e.RelationsToKeep = nil
}

// Merge 合并两个 GraphMemUpdate，返回新实例
//
// Python: GraphMemUpdate.__or__(other)
func (u *GraphMemUpdate) Merge(other *GraphMemUpdate) *GraphMemUpdate {
	result := &GraphMemUpdate{
		AddedEpisode:    append(sliceCopy(u.AddedEpisode), other.AddedEpisode...),
		UpdatedEpisode:  append(sliceCopy(u.UpdatedEpisode), other.UpdatedEpisode...),
		AddedEntity:     append(sliceCopy(u.AddedEntity), other.AddedEntity...),
		UpdatedEntity:   append(sliceCopy(u.UpdatedEntity), other.UpdatedEntity...),
		AddedRelation:   append(sliceCopy(u.AddedRelation), other.AddedRelation...),
		UpdatedRelation: append(sliceCopy(u.UpdatedRelation), other.UpdatedRelation...),
		RemovedEntity:   mergeSet(u.RemovedEntity, other.RemovedEntity),
		RemovedRelation: mergeSet(u.RemovedRelation, other.RemovedRelation),
	}
	return result
}

// Clear 清除提示词 Schema 引用
//
// Python: GraphMemPrompting.clear()
func (p *GraphMemPrompting) Clear() {
	p.SchemaEntityExtraction = nil
	p.SchemaEntityDedupe = nil
	p.SchemaRelationMerge = nil
	p.SchemaRelationFilter = nil
}

// NewGraphMemUpdate 创建空的增量变更累积器
func NewGraphMemUpdate() *GraphMemUpdate {
	return &GraphMemUpdate{
		AddedEpisode:    make([]*graph.Episode, 0),
		UpdatedEpisode:  make([]*graph.Episode, 0),
		AddedEntity:     make([]*graph.Entity, 0),
		UpdatedEntity:   make([]*graph.Entity, 0),
		AddedRelation:   make([]*graph.Relation, 0),
		UpdatedRelation: make([]*graph.Relation, 0),
		RemovedEntity:   make(map[string]struct{}),
		RemovedRelation: make(map[string]struct{}),
	}
}

// NewGraphMemState 创建图记忆状态（对齐 Python __init__ 所有字段默认值）
//
// Python: GraphMemState()
func NewGraphMemState() *GraphMemState {
	return &GraphMemState{
		Tasks:                   make([]*asyncTask, 0),
		MergingTasks:            make([]*asyncTask, 0),
		MergingTasksEntities:    make(map[*asyncTask]*graph.Entity),
		PendingMerge:            make(map[string]*pendingMergeTask),
		RelationDeferredUpdates: make(map[string][]deferredRelationUpdate),
		RelationFilterTasks:     make(map[*asyncTask]*relationFilterTaskItem),

		ToRemove:  make([]toRemoveItem, 0),
		TmpBuffer: make([]any, 0),

		UpdatedEntitiesInCurrentEp: make([]*graph.Entity, 0),
		RetrievedEntities:          make(map[string]*graph.Entity),
		RetrievedRelations:         make(map[string]*graph.Relation),
		FaultyRelations:            make(map[string]*graph.Relation),
		MergeInfos:                 make(map[string]*EntityMerge),

		MemUpdate:          NewGraphMemUpdate(),
		MemUpdateSkipEmbed: NewGraphMemUpdate(),

		CurrentTimestamp:   graph.GetCurrentUTCTimestamp(),
		ReferenceTimestamp: 0,
		LookupTable:        NewLookupTables(),
		Extras:             make(map[string]any),
		Strategy:           config.NewAddMemStrategy(),
		Prompting:          newGraphMemPrompting(),
		EntityTypes:        make([]extraction.EntityDef, 0),
		EpisodeType:        config.EpisodeTypeConversation,
		Content:            "",
		History:            "",
	}
}

// ClearReferences 清除 GraphMemState 所有引用，防止内存泄漏
//
// Python: GraphMemState.clear_references()
func (s *GraphMemState) ClearReferences() {
	// 先清除 merge_infos 中的 EntityMerge
	for _, mergeInfo := range s.MergeInfos {
		mergeInfo.Clear()
	}
	clearMap(s.MergeInfos)

	// 清除 LookupTables
	s.LookupTable.Clear()

	// 清除其他 map 引用
	clearMap(s.MergingTasksEntities)
	clearMap(s.PendingMerge)
	clearMap(s.RetrievedEntities)
	clearMap(s.RetrievedRelations)
	clearMap(s.FaultyRelations)
	clearMap(s.Extras)
	clearMap(s.RelationFilterTasks)
	for k := range s.RelationDeferredUpdates {
		delete(s.RelationDeferredUpdates, k)
	}

	// 清除 slice 引用
	s.Tasks = nil
	s.MergingTasks = nil
	s.ToRemove = nil
	s.TmpBuffer = nil
	s.UpdatedEntitiesInCurrentEp = nil
	s.EntityTypes = nil

	// 清除 MemUpdate
	s.MemUpdate = nil
	s.MemUpdateSkipEmbed = nil

	// 清除 Prompting
	s.Prompting.Clear()

	// 重置基本类型
	s.Content = ""
	s.History = ""
	s.Strategy = nil
}

// BatchEmbed 批量嵌入图对象列表。
// 成功时返回 nil；嵌入失败时返回失败的对象列表。
//
// Python: batch_embed(data, embedding_service, config)
func BatchEmbed(ctx context.Context, objects []embeddable, embedder embedding.BaseEmbedding, cfg *graph.GraphConfig) []embeddable {
	if len(objects) == 0 {
		return nil
	}

	// 收集所有嵌入任务：(对象, 字段名, 待嵌入文本)
	var embedTasks []graph.EmbedTask
	for _, obj := range objects {
		embedTasks = append(embedTasks, obj.EmbedTasks()...)
	}

	if len(embedTasks) == 0 {
		return nil
	}

	// 提取文本列表
	texts := make([]string, len(embedTasks))
	for i, t := range embedTasks {
		texts[i] = t.Text
	}

	// 执行嵌入
	embedResults, err := embedder.EmbedDocuments(ctx, texts, embedding.WithBatchSize(cfg.EmbedBatchSize))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Graph Memory: batch embed 失败")
		return objects
	}

	// 将嵌入结果设置到对应对象的字段
	for i, t := range embedTasks {
		if i < len(embedResults) {
			setEmbeddingField(t.Object, t.FieldName, embedResults[i])
		}
	}

	return nil
}

// PersistToDB 将图记忆状态持久化到数据库
//
// Python: persist_to_db(db_backend, state, config)
func PersistToDB(ctx context.Context, database graph.BaseGraphStore, state *GraphMemState, embedder embedding.BaseEmbedding, cfg *graph.GraphConfig) error {
	// 安全起见，先校验 mem_update_skip_embed.updated_entity
	state.TmpBuffer = state.TmpBuffer[:0]
	for _, entity := range state.MemUpdateSkipEmbed.UpdatedEntity {
		if entity.ContentEmbedding == nil || entity.NameEmbedding == nil {
			state.TmpBuffer = append(state.TmpBuffer, entity)
			if !containsEntity(state.MemUpdate.UpdatedEntity, entity) {
				state.MemUpdate.UpdatedEntity = append(state.MemUpdate.UpdatedEntity, entity)
			}
		}
	}
	for _, item := range state.TmpBuffer {
		if entity, ok := item.(*graph.Entity); ok {
			state.MemUpdateSkipEmbed.UpdatedEntity = removeEntity(state.MemUpdateSkipEmbed.UpdatedEntity, entity)
		}
	}

	// 尝试嵌入新增实体、新增关系和更新实体
	objectsToEmbed := collectEmbeddables(
		state.MemUpdate.AddedEntity, state.MemUpdate.AddedRelation, state.MemUpdate.UpdatedEntity,
	)
	retries := cfg.RequestMaxRetries
	for retries > 0 {
		failed := BatchEmbed(ctx, objectsToEmbed, embedder, cfg)
		if len(failed) == 0 {
			break
		}
		objectsToEmbed = failed
		retries--
	}
	if retries == 0 && len(objectsToEmbed) > 0 {
		return exception.BuildError(exception.StatusMemoryGraphEmbeddingCallFailed,
			exception.WithParam("error_msg", "Unable to access embedding service"))
	}

	// 尝试嵌入新增片段
	objectsToEmbed = collectEmbeddableEpisodes(state.MemUpdate.AddedEpisode)
	retries = cfg.RequestMaxRetries
	for retries > 0 {
		failed := BatchEmbed(ctx, objectsToEmbed, embedder, cfg)
		if len(failed) == 0 {
			break
		}
		// 可能片段过长，截断到一半重试
		if len(state.MemUpdate.AddedEpisode) > 0 {
			ep := state.MemUpdate.AddedEpisode[0]
			halfLen := len(ep.Content) / 2
			if halfLen > 0 {
				ep.Content = ep.Content[:halfLen]
			}
		}
		retries--
	}
	if retries == 0 && len(objectsToEmbed) > 0 {
		return exception.BuildError(exception.StatusMemoryGraphEmbeddingCallFailed,
			exception.WithParam("error_msg", "Unable to access embedding service for new episode, maybe exceeding context limit"))
	}

	// 阻断键盘中断，确保数据库操作完成
	cancel := BlockKeyboardInterrupt(ctx)
	defer cancel()

	// 写入数据库
	if err := database.AddEntity(ctx, state.MemUpdate.AddedEntity, graph.WithUpsert(false), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
		return err
	}
	if err := database.AddRelation(ctx, state.MemUpdate.AddedRelation, graph.WithUpsert(false), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
		return err
	}
	if err := database.AddEpisode(ctx, state.MemUpdate.AddedEpisode, graph.WithUpsert(false), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
		return err
	}
	if err := database.AddEntity(ctx, state.MemUpdate.UpdatedEntity, graph.WithUpsert(true), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
		return err
	}

	if len(state.MemUpdateSkipEmbed.UpdatedEpisode) > 0 {
		if err := database.AddEpisode(ctx, state.MemUpdateSkipEmbed.UpdatedEpisode, graph.WithUpsert(true), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
			return err
		}
	}
	if len(state.MemUpdateSkipEmbed.UpdatedEntity) > 0 {
		if err := database.AddEntity(ctx, state.MemUpdateSkipEmbed.UpdatedEntity, graph.WithUpsert(true), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
			return err
		}
	}
	if len(state.MemUpdateSkipEmbed.UpdatedRelation) > 0 {
		if err := database.AddRelation(ctx, state.MemUpdateSkipEmbed.UpdatedRelation, graph.WithUpsert(true), graph.WithFlush(false), graph.WithNoEmbed(true)); err != nil {
			return err
		}
	}
	if len(state.MemUpdate.RemovedEntity) > 0 {
		ids := setToAnySlice(state.MemUpdate.RemovedEntity)
		if err := database.Delete(ctx, graph.EntityCollection, graph.WithIDs(ids...)); err != nil {
			return err
		}
	}
	if len(state.MemUpdate.RemovedRelation) > 0 {
		ids := setToAnySlice(state.MemUpdate.RemovedRelation)
		if err := database.Delete(ctx, graph.RelationCollection, graph.WithIDs(ids...)); err != nil {
			return err
		}
	}

	return nil
}

// ClassifyRelationsExtracted 对提取的关系进行分类（实体合并导致的关系去重）
//
// Python: classify_relations_extracted(relations, state)
func ClassifyRelationsExtracted(relations []*graph.Relation, state *GraphMemState) {
	// 分类需要保留和移除的关系
	for _, mergeInfo := range state.MergeInfos {
		for _, relation := range mergeInfo.NewRelations {
			lhsUUID := relation.LHS
			rhsUUID := relation.RHS
			if lhsUUID != rhsUUID {
				mergeInfo.RelationsToKeep[relation.UUID] = struct{}{}
			} else {
				state.MemUpdate.RemovedRelation[relation.UUID] = struct{}{}
			}
		}
		// 合并保留关系到目标实体（对齐 Python: set.union）
		mergeInfo.Target.Relations = mergeStringSets(mergeInfo.RelationsToKeep, mergeInfo.Target.Relations)
	}

	// 分类提取的关系
	state.TmpBuffer = state.TmpBuffer[:0] // 关系内容，用于后续 _relation_dedupe
	for _, relation := range relations {
		// 记录自指向关系（关于对象的事实）
		relation.Language = state.Prompting.Language
		if strings.TrimSpace(relation.Content) == "" {
			state.ToRemove = append(state.ToRemove, toRemoveItem{UUID: relation.UUID, ObjType: "Relation"})
		} else if relation.LHS == relation.RHS {
			// 自指向关系：将关系内容追加到实体 content
			// Python 中 relation.lhs 是 Entity 对象，Go 中 LHS 是 UUID 字符串，
			// 需要通过 RetrievedEntities 查找对应实体
			if entity, ok := state.RetrievedEntities[relation.LHS]; ok {
				content := strings.TrimSuffix(entity.Content, "\n")
				entity.Content = fmt.Sprintf("%s\n- %s", content, relation.Content)
			}
			state.ToRemove = append(state.ToRemove, toRemoveItem{UUID: relation.UUID, ObjType: "Relation"})
		} else {
			state.TmpBuffer = append(state.TmpBuffer, relation.Content)
		}
	}
}

// BlockKeyboardInterrupt 阻断键盘中断信号，确保关键数据库操作完成。
// 返回的 CancelFunc 恢复原始信号处理；如期间收到 SIGINT，恢复后重新发送。
//
// Python: block_keyboard_interrupt()
func BlockKeyboardInterrupt(ctx context.Context) context.CancelFunc {
	received := false

	// 安装临时信号处理器
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT)

	// 取消通道
	done := make(chan struct{})

	// 启动信号处理协程
	go func() {
		select {
		case <-sigChan:
			logger.Warn(logComponent).Msg("Graph Memory: received SIGINT from user, but critical database operation is in progress")
			received = true
		case <-done:
			signal.Stop(sigChan)
			return
		}
	}()

	return func() {
		close(done)
		if received {
			// 恢复后重新发送中断信号
			syscall.Kill(syscall.Getpid(), syscall.SIGINT)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newGraphMemPrompting 创建提示词 Schema 配置（对齐 Python GraphMemPrompting 默认值）
func newGraphMemPrompting() *GraphMemPrompting {
	return &GraphMemPrompting{
		SchemaEntityExtraction:    extraction.ResponseFormat("EntitySummary", map[string]any{}),
		SchemaEntityDedupe:        extraction.ResponseFormat("EntityDuplication", map[string]any{}),
		SchemaRelationMerge:       extraction.ResponseFormat("MergeRelations", map[string]any{}),
		SchemaRelationFilter:      extraction.ResponseFormat("RelevantFacts", map[string]any{}),
		Language:                   "cn",
		EntityExtractionLanguage:   "cn",
		RelationExtractionLanguage: "cn",
		EntityDedupeLanguage:       "cn",
	}
}

// entityFromMap 从 map 构造 Entity（对齐 Python Entity(**input_obj)）
func entityFromMap(input map[string]any) *graph.Entity {
	e := graph.NewEntity()
	if v, ok := input["uuid"].(string); ok {
		e.UUID = v
	}
	if v, ok := toInt64(input["created_at"]); ok {
		e.CreatedAt = v
	}
	if v, ok := input["user_id"].(string); ok {
		e.UserID = v
	}
	if v, ok := input["obj_type"].(string); ok {
		e.ObjType = v
	}
	if v, ok := input["language"].(string); ok {
		e.Language = v
	}
	if v, ok := input["name"].(string); ok {
		e.Name = v
	}
	if v, ok := input["content"].(string); ok {
		e.Content = v
	}
	if v, ok := input["metadata"].(map[string]any); ok {
		e.Metadata = v
	}
	if v, ok := input["attributes"].(map[string]any); ok {
		e.Attributes = v
	}
	if v, ok := toStringSlice(input["relations"]); ok {
		e.Relations = v
	}
	if v, ok := toStringSlice(input["episodes"]); ok {
		e.Episodes = v
	}
	return e
}

// relationFromMap 从 map 构造 Relation
func relationFromMap(input map[string]any) *graph.Relation {
	r := graph.NewRelation()
	if v, ok := input["uuid"].(string); ok {
		r.UUID = v
	}
	if v, ok := toInt64(input["created_at"]); ok {
		r.CreatedAt = v
	}
	if v, ok := input["user_id"].(string); ok {
		r.UserID = v
	}
	if v, ok := input["obj_type"].(string); ok {
		r.ObjType = v
	}
	if v, ok := input["language"].(string); ok {
		r.Language = v
	}
	if v, ok := input["name"].(string); ok {
		r.Name = v
	}
	if v, ok := input["content"].(string); ok {
		r.Content = v
	}
	if v, ok := toInt64(input["valid_since"]); ok {
		r.ValidSince = v
	}
	if v, ok := toInt64(input["valid_until"]); ok {
		r.ValidUntil = v
	}
	if v, ok := toInt8(input["offset_since"]); ok {
		r.OffsetSince = v
	}
	if v, ok := toInt8(input["offset_until"]); ok {
		r.OffsetUntil = v
	}
	// lhs/rhs 在 Python 中可以是 Entity 对象或字符串，Go 统一为字符串 UUID
	if v, ok := input["lhs"].(string); ok {
		r.LHS = v
	}
	if v, ok := input["rhs"].(string); ok {
		r.RHS = v
	}
	return r
}

// episodeFromMap 从 map 构造 Episode
func episodeFromMap(input map[string]any) *graph.Episode {
	p := graph.NewEpisode()
	if v, ok := input["uuid"].(string); ok {
		p.UUID = v
	}
	if v, ok := toInt64(input["created_at"]); ok {
		p.CreatedAt = v
	}
	if v, ok := input["user_id"].(string); ok {
		p.UserID = v
	}
	if v, ok := input["obj_type"].(string); ok {
		p.ObjType = v
	}
	if v, ok := input["language"].(string); ok {
		p.Language = v
	}
	if v, ok := input["content"].(string); ok {
		p.Content = v
	}
	if v, ok := toInt64(input["valid_since"]); ok {
		p.ValidSince = v
	}
	if v, ok := toStringSlice(input["entities"]); ok {
		p.Entities = v
	}
	return p
}

// toInt64 从 any 转换为 int64
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// toInt8 从 any 转换为 int8
func toInt8(v any) (int8, bool) {
	i, ok := toInt64(v)
	if !ok || i < -128 || i > 127 {
		return 0, false
	}
	return int8(i), true
}

// toStringSlice 从 any 转换为 []string
func toStringSlice(v any) ([]string, bool) {
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result, true
	}
	return nil, false
}

// clearMap 清除 map 的所有键值对
func clearMap[K comparable, V any](m map[K]V) {
	for k := range m {
		delete(m, k)
	}
}

// mergeSet 合并两个 set
func mergeSet(a, b map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		result[k] = struct{}{}
	}
	for k := range b {
		result[k] = struct{}{}
	}
	return result
}

// mergeStringSets 合并 set 和 slice，去重后返回新 slice
// 对齐 Python: merge_info.target.relations = list(merge_info.relations_to_keep.union(merge_info.target.relations))
func mergeStringSets(set map[string]struct{}, slice []string) []string {
	seen := make(map[string]struct{}, len(set)+len(slice))
	result := make([]string, 0, len(set)+len(slice))
	for k := range set {
		seen[k] = struct{}{}
		result = append(result, k)
	}
	for _, s := range slice {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

// sliceCopy 浅拷贝切片
func sliceCopy[T any](s []T) []T {
	if s == nil {
		return nil
	}
	cp := make([]T, len(s))
	copy(cp, s)
	return cp
}

// setEmbeddingField 通过反射设置嵌入字段
// 对齐 Python: setattr(obj, attribute, embedding)
func setEmbeddingField(obj any, fieldName string, embedding []float64) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldNameToExported(fieldName))
	if f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(embedding))
	}
}

// fieldNameToExported 将 snake_case 字段名转为 Go 导出字段名
func fieldNameToExported(name string) string {
	switch name {
	case "content_embedding":
		return "ContentEmbedding"
	case "name_embedding":
		return "NameEmbedding"
	default:
		return name
	}
}

// collectEmbeddables 收集需要嵌入的图对象为 embeddable 列表
func collectEmbeddables(entities []*graph.Entity, relations []*graph.Relation, updatedEntities []*graph.Entity) []embeddable {
	var result []embeddable
	for _, e := range entities {
		result = append(result, e)
	}
	for _, r := range relations {
		result = append(result, r)
	}
	for _, e := range updatedEntities {
		result = append(result, e)
	}
	return result
}

// collectEmbeddableEpisodes 收集需要嵌入的片段为 embeddable 列表
func collectEmbeddableEpisodes(episodes []*graph.Episode) []embeddable {
	var result []embeddable
	for _, ep := range episodes {
		result = append(result, ep)
	}
	return result
}

// containsEntity 检查实体是否在列表中（指针比较）
func containsEntity(list []*graph.Entity, target *graph.Entity) bool {
	for _, e := range list {
		if e == target {
			return true
		}
	}
	return false
}

// removeEntity 从列表中移除指定实体（指针比较，移除所有匹配项）
// 对齐 Python: while entity in list: list.remove(entity)
func removeEntity(list []*graph.Entity, target *graph.Entity) []*graph.Entity {
	result := make([]*graph.Entity, 0, len(list))
	for _, e := range list {
		if e != target {
			result = append(result, e)
		}
	}
	return result
}

// setToAnySlice 将 map[string]struct{} 转为 []any
func setToAnySlice(s map[string]struct{}) []any {
	result := make([]any, 0, len(s))
	for k := range s {
		result = append(result, k)
	}
	return result
}
