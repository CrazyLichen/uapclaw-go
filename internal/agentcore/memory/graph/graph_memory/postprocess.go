package graph_memory

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DedupeRelationTask 关系去重任务
//
// Python: Tuple[Relation, List[Relation], asyncio.Future] (dedupe_relation_tasks)
type DedupeRelationTask struct {
	// Relation 待去重的关系
	Relation *graph.Relation
	// ExistingRelations 已有的关系列表（map 形式，对齐 Python 中从数据库查询的结果）
	ExistingRelations []map[string]any
	// Response LLM 返回的响应内容
	Response string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateEntitiesEpisodes 同步 Entity-Episode 连接信息
//
// 确保 Entity 和 Episode 之间的双向引用一致：
//  1. 将新实体的 UUID 加入当前 Episode 的 entities 列表
//  2. 从 mem_update_skip_embed.updated_entity 中移除已在 mem_update.updated_entity 中的实体
//  3. 遍历合并信息，将源实体的 Episode 引用替换为目标实体 UUID
//  4. 双向校验并修复 Entity↔Episode 连接不一致
//
// Python: validate_entities_episodes(entities, current_episode, state)
func ValidateEntitiesEpisodes(entities []*graph.Entity, currentEpisode *graph.Episode, state *GraphMemState) {
	// 将当前 Episode 的 entities 中已有 UUID 与新实体 UUID 合并去重
	currentEpisode.Entities = mergeStringSliceUnique(
		stringSliceFromAny(currentEpisode.Entities),
		entityUUIDs(entities),
	)

	// 从 mem_update_skip_embed.updated_entity 中移除已在 mem_update.updated_entity 中的实体
	state.MemUpdateSkipEmbed.UpdatedEntity = filterEntitiesNotIn(
		state.MemUpdateSkipEmbed.UpdatedEntity,
		state.MemUpdate.UpdatedEntity,
	)

	// 遍历合并信息：源实体被合并到目标实体后，更新 Episode 的 entities 引用
	for tgtUUID, mergeInfo := range state.MergeInfos {
		for srcUUID, src := range mergeInfo.Source {
			for _, epUUID := range src.Episodes {
				ep := state.LookupTable.Episodes[epUUID]
				if ep == nil {
					continue
				}
				updated := false
				if containsString(ep.Entities, srcUUID) {
					ep.Entities = removeString(ep.Entities, srcUUID)
					updated = true
				}
				if updated {
					ep.Entities = append(ep.Entities, tgtUUID)
					if !containsEpisode(state.MemUpdateSkipEmbed.UpdatedEpisode, ep) {
						state.MemUpdateSkipEmbed.UpdatedEpisode = append(state.MemUpdateSkipEmbed.UpdatedEpisode, ep)
					}
				}
			}
		}
	}

	// 双向校验 Entity↔Episode 连接一致性
	allEpisodes := append(state.MemUpdateSkipEmbed.UpdatedEpisode, currentEpisode)
	allEntities := append(state.MemUpdate.UpdatedEntity, state.MemUpdateSkipEmbed.UpdatedEntity...)
	for _, episode := range allEpisodes {
		for _, entity := range allEntities {
			ep2e := containsString(episode.Entities, entity.UUID)
			e2ep := containsString(entity.Episodes, episode.UUID)
			if ep2e && !e2ep {
				// Episode→Entity 但 Entity↔Episode 不存在：从 Episode 移除 Entity
				episode.Entities = removeString(episode.Entities, entity.UUID)
			} else if e2ep && !ep2e {
				// Entity→Episode 但 Episode↔Entity 不存在：向 Episode 添加 Entity
				episode.Entities = append(episode.Entities, entity.UUID)
			}
		}
		episode.Entities = uniqueStringSlice(episode.Entities)
	}
}

// CreateEpisode 创建 Episode 片段
//
// 根据状态中的时间戳、语言、片段类型等信息创建新的 Episode，
// 并通过 EnsureUniqueUUIDs 保证 UUID 唯一性。
//
// Python: create_episode(database, user_id, content, state)
func CreateEpisode(ctx context.Context, database graph.BaseGraphStore, userID string, content string, state *GraphMemState) (*graph.Episode, error) {
	currentEpisode := graph.NewEpisode()
	currentEpisode.CreatedAt = state.CurrentTimestamp
	currentEpisode.ValidSince = state.ReferenceTimestamp
	currentEpisode.UserID = userID
	currentEpisode.ObjType = state.EpisodeType.String()
	currentEpisode.Language = state.Prompting.Language
	currentEpisode.Content = content

	// 解析 Episode UUID（确保唯一性）
	uniqueUUIDs, err := graph.EnsureUniqueUUIDs(ctx, database, []string{currentEpisode.UUID}, graph.EpisodeCollection, state.Strategy.SkipUUIDDedupe)
	if err != nil {
		return nil, err
	}
	currentEpisode.UUID = uniqueUUIDs[0]

	state.MemUpdate.AddedEpisode = append(state.MemUpdate.AddedEpisode, currentEpisode)
	return currentEpisode, nil
}

// ProcessRelations 处理关系（删除废弃 + 更新关联 + UUID 去重）
//
// 1. 移除废弃关系对实体的影响：从实体的 relations 列表中删除已废弃关系的 UUID
// 2. 更新关系的关联实体：调用 UpdateConnectedEntities 将自身 UUID 加入 lhs/rhs 实体
// 3. 对新增关系的 UUID 进行去重
//
// Python: process_relations(database, entities, relations, state)
func ProcessRelations(ctx context.Context, database graph.BaseGraphStore, entities []*graph.Entity, relations []*graph.Relation, state *GraphMemState) error {
	toResolve := state.TmpBuffer
	state.TmpBuffer = state.TmpBuffer[:0]

	// 移除废弃关系：从实体的 relations 列表中删除已废弃关系的 UUID
	for relationUUID := range state.MemUpdate.RemovedRelation {
		allEntities := append(entities, state.MemUpdateSkipEmbed.UpdatedEntity...)
		for _, entity := range allEntities {
			if containsString(entity.Relations, relationUUID) {
				entity.Relations = removeString(entity.Relations, relationUUID)
			}
		}
	}

	// 处理关系：更新关联实体 + 累积到 mem_update
	for _, relation := range relations {
		relation.UpdateConnectedEntities(
			state.LookupTable.Entities[relation.LHS],
			state.LookupTable.Entities[relation.RHS],
		)
		state.MemUpdate.AddedRelation = append(state.MemUpdate.AddedRelation, relation)
		toResolve = append(toResolve, relation.UUID)
	}

	// 对新增关系的 UUID 去重
	if len(toResolve) > 0 {
		ids := anySliceToStrings(toResolve)
		uniqueUUIDs, err := graph.EnsureUniqueUUIDs(ctx, database, ids, graph.RelationCollection, state.Strategy.SkipUUIDDedupe)
		if err != nil {
			return err
		}
		for i, relation := range state.MemUpdate.AddedRelation {
			if i < len(uniqueUUIDs) {
				relation.UUID = uniqueUUIDs[i]
			}
		}
	}

	state.TmpBuffer = toResolve
	return nil
}

// ProcessEntities 处理实体（合并完成 + 关联 Episode + UUID 去重）
//
// 1. 等待所有合并任务完成，更新实体内容
// 2. 清理实体内容前缀换行、移除已废弃关系的引用
// 3. 关联当前 Episode、设置语言、分类为新增/更新
// 4. 对新增实体的 UUID 去重
//
// Python: process_entities(database, entities, current_episode, state)
func ProcessEntities(ctx context.Context, database graph.BaseGraphStore, entities []*graph.Entity, currentEpisode *graph.Episode, state *GraphMemState) error {
	toResolve := state.TmpBuffer
	state.TmpBuffer = state.TmpBuffer[:0]

	// 完成剩余的合并任务
	// Python: for future in state.merging_tasks: response = await future
	// Go 中合并任务以 *asyncTask 存储，MergingTasksEntities 保存关联实体
	for _, task := range state.MergingTasks {
		entity := state.MergingTasksEntities[task]
		// Python: update_entity(entity, response.content, state.prompting.schema_entity_extraction)
		if entity != nil && task != nil && task.Err == nil && task.Result != "" {
			UpdateEntity(entity, task.Result, state.Prompting.SchemaEntityExtraction)
		}
		if entity != nil && !containsEntityPtr(entities, entity) {
			entities = append(entities, entity)
		}
	}

	// 处理实体
	for _, entity := range entities {
		// 移除内容前缀换行
		entity.Content = strings.TrimPrefix(entity.Content, "\n")

		// 移除已废弃关系的引用
		state.ToRemove = state.ToRemove[:0]
		for _, r := range entity.Relations {
			if containsStringSet(state.MemUpdate.RemovedRelation, r) {
				state.ToRemove = append(state.ToRemove, toRemoveItem{UUID: r, ObjType: "Relation"})
			}
		}
		for _, item := range state.ToRemove {
			entity.Relations = removeString(entity.Relations, item.UUID)
		}

		// 关联当前 Episode
		if !containsString(entity.Episodes, currentEpisode.UUID) {
			entity.Episodes = append(entity.Episodes, currentEpisode.UUID)
		}

		// 设置语言
		entity.Language = state.Prompting.Language

		// 分类为新增/更新实体
		if _, ok := state.RetrievedEntities[entity.UUID]; ok {
			state.MemUpdate.UpdatedEntity = append(state.MemUpdate.UpdatedEntity, entity)
		} else {
			state.MemUpdate.AddedEntity = append(state.MemUpdate.AddedEntity, entity)
			toResolve = append(toResolve, entity.UUID)
		}
	}

	// 对新增实体的 UUID 去重
	if len(toResolve) > 0 {
		ids := anySliceToStrings(toResolve)
		uniqueUUIDs, err := graph.EnsureUniqueUUIDs(ctx, database, ids, graph.EntityCollection, state.Strategy.SkipUUIDDedupe)
		if err != nil {
			return err
		}
		for i, entity := range state.MemUpdate.AddedEntity {
			if i < len(uniqueUUIDs) {
				entity.UUID = uniqueUUIDs[i]
			}
		}
	}

	state.TmpBuffer = toResolve
	return nil
}

// ParseRelationUUIDsToRemove 解析需删除的关系 UUID
//
// 遍历关系去重任务列表，解析 LLM 响应中指定的重复关系 UUID，
// 将结果追加到 state.ToRemove。
//
// Python: parse_relation_uuids_to_remove(dedupe_relation_tasks, state)
func ParseRelationUUIDsToRemove(dedupeRelationTasks []DedupeRelationTask, state *GraphMemState) {
	for _, task := range dedupeRelationTasks {
		dedupeRelation := extraction.ParseJSON(task.Response, state.Prompting.SchemaRelationMerge)
		if dedupeRelation == nil {
			dedupeRelation = map[string]any{}
		}
		dedupeMap, ok := dedupeRelation.(map[string]any)
		if !ok {
			continue
		}
		toRemoveUUIDs := ParseRelationMerging(dedupeMap, task.Relation, task.ExistingRelations)
		for uuid := range toRemoveUUIDs {
			state.ToRemove = append(state.ToRemove, toRemoveItem{UUID: uuid, ObjType: "Relation"})
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringSliceFromAny 将 []any 转为 []string（对齐 Python 中 Episode.entities 可能包含 Entity 对象）
// Python: [e if isinstance(e, str) else e.uuid for e in current_episode.entities]
func stringSliceFromAny(items []string) []string {
	// Go 中 Entities 已是 []string，直接返回
	return items
}

// entityUUIDs 提取实体列表的 UUID
func entityUUIDs(entities []*graph.Entity) []string {
	result := make([]string, 0, len(entities))
	for _, e := range entities {
		result = append(result, e.UUID)
	}
	return result
}

// mergeStringSliceUnique 合并两个字符串切片并去重
func mergeStringSliceUnique(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

// filterEntitiesNotIn 从 source 中过滤掉在 target 中出现的实体（指针比较）
// 对齐 Python: [e for e in skip.updated_entity if e not in update.updated_entity]
func filterEntitiesNotIn(source []*graph.Entity, target []*graph.Entity) []*graph.Entity {
	result := make([]*graph.Entity, 0, len(source))
	for _, e := range source {
		if !containsEntityPtr(target, e) {
			result = append(result, e)
		}
	}
	return result
}

// containsEntityPtr 检查实体指针是否在列表中
func containsEntityPtr(list []*graph.Entity, target *graph.Entity) bool {
	for _, e := range list {
		if e == target {
			return true
		}
	}
	return false
}

// containsString 检查字符串是否在切片中
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

// containsStringSet 检查字符串是否在 set 中
func containsStringSet(set map[string]struct{}, target string) bool {
	_, ok := set[target]
	return ok
}

// removeString 从切片中移除所有匹配的字符串
func removeString(slice []string, target string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != target {
			result = append(result, s)
		}
	}
	return result
}

// uniqueStringSlice 对字符串切片去重
func uniqueStringSlice(slice []string) []string {
	seen := make(map[string]struct{}, len(slice))
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

// containsEpisode 检查 Episode 指针是否在列表中
func containsEpisode(list []*graph.Episode, target *graph.Episode) bool {
	for _, ep := range list {
		if ep == target {
			return true
		}
	}
	return false
}

// anySliceToStrings 将 []any 转为 []string
func anySliceToStrings(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
