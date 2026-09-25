package graph_memory

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/graph/extraction/registry"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EntityOrDeclaration 实体声明或已存在实体的联合类型
// 对齐 Python: Union[EntityDeclaration, Entity]
type EntityOrDeclaration struct {
	// Decl 新抽取的实体声明（非 nil 时 Entity 为 nil）
	Decl *extraction.EntityDeclaration
	// Entity 已存在的实体对象（非 nil 时 Decl 为 nil）
	Entity *graph.Entity
}

// IsEntity 是否为已存在实体
func (e EntityOrDeclaration) IsEntity() bool {
	return e.Entity != nil
}

// Name 获取实体名称
func (e EntityOrDeclaration) Name() string {
	if e.Entity != nil {
		return e.Entity.Name
	}
	if e.Decl != nil {
		return e.Decl.Name
	}
	return ""
}

// EntityTypeID 获取实体类型 ID
func (e EntityOrDeclaration) EntityTypeID() int {
	if e.Decl != nil {
		return e.Decl.EntityTypeID
	}
	return 0
}

// MergePair 实体合并对（目标实体 + 待合并的源实体列表）
//
// Python: tuple[Entity, list[Entity]] (resolve_entities 返回值)
type MergePair struct {
	// Target 合并目标实体
	Target *graph.Entity
	// Sources 待合并的源实体列表
	Sources []*graph.Entity
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// MatchISODatetime ISO 8601 日期时间正则
	//
	// Python: MATCH_ISO_DATETIME
	MatchISODatetime = regexp.MustCompile(
		`([0-9]{1,4})-([0-9]{1,2})-([0-9]{1,2})T([0-9]{1,2}):([0-9]{1,2}):([0-9]{1,2})(?:Z|\+([0-9]{1,2}):([0-9]{1,2}))?`,
	)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseISO 将 ISO 8601 日期时间字符串解析为 UNIX 时间戳和时区偏移（15分钟为单位）
//
// 解析失败返回 (-1, 0)。
//
// Python: parse_iso(time_str) -> tuple[int, int]
func ParseISO(timeStr string) (int64, int8) {
	if timeStr != "" {
		match := MatchISODatetime.FindStringSubmatch(timeStr)
		if match != nil {
			yyyy, _ := strconv.Atoi(match[1])
			mm, _ := strconv.Atoi(match[2])
			dd, _ := strconv.Atoi(match[3])
			h, _ := strconv.Atoi(match[4])
			m, _ := strconv.Atoi(match[5])
			s, _ := strconv.Atoi(match[6])

			isoStr := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", yyyy, mm, dd, h, m, s)

			// 手动对齐时区偏移
			offsetH := match[7]
			offsetM := match[8]
			hasOffset := offsetH != ""
			hasZ := strings.Contains(timeStr, "Z")
			if hasOffset {
				oh, _ := strconv.Atoi(offsetH)
				offsetStr := fmt.Sprintf("+%02d", oh)
				if offsetM != "" {
					om, _ := strconv.Atoi(offsetM)
					offsetStr += fmt.Sprintf(":%02d", om)
				}
				isoStr += offsetStr
			}

			var ts int64
			var offset int8
			var err error
			if hasOffset {
				ts, offset, err = graph.ISO2Timestamp(isoStr)
			} else if hasZ {
				// 有 Z 后缀表示 UTC（对齐 Python: Z 后缀按 UTC 处理）
				isoStr += "Z"
				ts, offset, err = graph.ISO2Timestamp(isoStr)
			} else {
				// 无时区信息时按本地时区解析（对齐 Python: 无 Z 后缀时按本地时区处理）
				local := time.Local
				t, parseErr := time.ParseInLocation("2006-01-02T15:04:05", isoStr, local)
				if parseErr != nil {
					return -1, 0
				}
				ts = t.Unix()
				_, localOffset := t.Zone()
				offset = int8(localOffset / (15 * 60))
			}
			if err == nil {
				return ts, offset
			}
		}
	}
	return -1, 0
}

// Dict2Relation 将 LLM 输出 dict 转为 Relation 对象
//
// 如果 response 只有 1 个 key 且其值为 dict，则展开为内层 dict。
// source_id/target_id 为 1-based 实体索引，解析失败返回 nil。
//
// Python: dict2relation(response, entities, **kwargs)
func Dict2Relation(response map[string]any, entities []*graph.Entity, createdAt int64, userID string) *graph.Relation {
	// 如果 response 只有 1 个 key，尝试展开其值
	if len(response) == 1 {
		for _, v := range response {
			if inner, ok := v.(map[string]any); ok {
				response = inner
			}
		}
	}

	sourceID := response["source_id"]
	targetID := response["target_id"]

	// 尝试解析 source_id 和 target_id（1-based 索引）
	srcInt, srcOk := toInt(sourceID)
	tgtInt, tgtOk := toInt(targetID)
	if !srcOk || !tgtOk {
		return nil
	}

	srcIdx := srcInt - 1
	tgtIdx := tgtInt - 1
	if srcIdx < 0 || tgtIdx < 0 || srcIdx >= len(entities) || tgtIdx >= len(entities) {
		return nil
	}

	lhs := entities[srcIdx]
	rhs := entities[tgtIdx]

	var relType string
	if lhs == rhs {
		relType = "EntityFact"
	} else {
		relType = "Relation"
	}

	name := anyToStr(response["name"])
	if name == "" {
		name = "RELATION"
	}

	content := anyToStr(response["fact"])
	if content == "" {
		content = name
	}

	validSinceStr, _ := response["valid_since"].(string)
	validUntilStr, _ := response["valid_until"].(string)
	validSince, offsetSince := ParseISO(validSinceStr)
	validUntil, offsetUntil := ParseISO(validUntilStr)

	rel := graph.NewRelation()
	rel.CreatedAt = createdAt
	rel.UserID = userID
	rel.ObjType = relType
	rel.Name = name
	rel.Content = content
	rel.ValidSince = validSince
	rel.ValidUntil = validUntil
	rel.OffsetSince = offsetSince
	rel.OffsetUntil = offsetUntil
	rel.LHS = lhs
	rel.RHS = rhs

	return rel
}

// ParseAllRelations 解析全部关系 + 去重 + 实体声明转换
//
// 先将 EntityDeclaration 转为 Entity，再对关系内容去重（短内容被长内容覆盖），
// 最后解析为 Relation 对象并去重实体。
//
// Python: parse_all_relations(relations, entities, entity_types, **kwargs)
func ParseAllRelations(relations []map[string]any, entityDecls []extraction.EntityDeclaration, entityTypes []registry.EntityDef, createdAt int64, userID string) ([]*graph.Relation, []*graph.Entity) {
	// 将 EntityDeclaration 转为 Entity
	entities := DeclareEntities(entityDecls, entityTypes, createdAt, userID)

	// 去重关系内容（LLM 可能重复输出）
	existingContents := make(map[string]struct{})
	for _, relation := range relations {
		newContent := strings.TrimSpace(anyToStr(relation["content"]))
		duplicate := false
		for oldContent := range existingContents {
			if strings.Contains(oldContent, newContent) {
				duplicate = true
				break
			}
		}
		if duplicate {
			relation["content"] = ""
		} else {
			relation["content"] = newContent
		}
		existingContents[newContent] = struct{}{}
	}

	// 解析关系抽取结果
	var result []*graph.Relation
	for _, rel := range relations {
		if r := Dict2Relation(rel, entities, createdAt, userID); r != nil {
			result = append(result, r)
		}
	}

	// 实体去重（按 UUID，去重应在关系解析之后）
	deduped := dedupEntitiesByUUID(entities)

	return result, deduped
}

// DeclareEntities 将 EntityDeclaration 转为 Entity
//
// Python: declare_entities(entities, entity_types, **kwargs)
func DeclareEntities(entityDecls []extraction.EntityDeclaration, entityTypes []registry.EntityDef, createdAt int64, userID string) []*graph.Entity {
	typeIDMax := len(entityTypes) - 1
	result := make([]*graph.Entity, 0, len(entityDecls))
	for _, ent := range entityDecls {
		e := graph.NewEntity()
		e.Name = ent.Name
		e.Content = ""
		e.CreatedAt = createdAt
		e.UserID = userID
		if typeIDMax >= 0 {
			typeIdx := ent.EntityTypeID
			if typeIdx > typeIDMax {
				typeIdx = typeIDMax
			}
			if typeIdx < 0 {
				typeIdx = 0
			}
			e.ObjType = entityTypes[typeIdx].Name
		}
		result = append(result, e)
	}
	return result
}

// ResolveEntities 实体去重/合并解析
//
// 返回 (已解析的实体列表, 合并对列表, 待删除的UUID集合)。
// 已解析的实体列表包含 EntityDeclaration 和 Entity 两种类型（对齐 Python: list[Union[EntityDeclaration, Entity]]）。
// 合并对列表中每项包含目标实体和待合并的源实体列表。
//
// Python: resolve_entities(candidates, existing, duplication)
func ResolveEntities(candidates []extraction.EntityDeclaration, existing []*graph.Entity, duplication []map[string]any) ([]EntityOrDeclaration, []MergePair, map[string]struct{}) {
	// 复制候选列表（结果列表可能混合 EntityDeclaration 和 Entity）
	result := make([]EntityOrDeclaration, len(candidates))
	for i := range candidates {
		c := candidates[i]
		result[i] = EntityOrDeclaration{Decl: &c}
	}

	nameLookup := make(map[string]*graph.Entity)
	uuidLookup := make(map[string]*graph.Entity)
	for _, ent := range existing {
		nameLookup[ent.Name] = ent
		uuidLookup[ent.UUID] = ent
	}

	numExisting := len(existing)
	numEntities := len(candidates) + numExisting

	mergeMap := make(map[string]map[string]struct{}) // target_uuid -> set of source_uuids
	isTarget := make(map[string]string)              // entity_uuid -> its target's uuid

	for _, dup := range duplication {
		var tgtEntity *graph.Entity
		if idInt, ok := toInt(dup["id"]); ok {
			idIdx := idInt - 1
			if idIdx >= 0 && idIdx < numExisting {
				tgtEntity = existing[idIdx]
			}
		} else {
			tgtEntity = nameLookup[anyToStr(dup["name"])]
		}

		if tgtEntity != nil {
			parseEntityMerging(
				dup, mergeMap, isTarget, result, existing,
				tgtEntity, numEntities, numExisting,
			)
		}
	}

	// 将 mergeMap 转为 mergeDict（UUID 集合 → Entity 列表）
	mergeDict := make(map[string][]*graph.Entity)
	for tgtUUID, srcUUIDs := range mergeMap {
		sources := make([]*graph.Entity, 0, len(srcUUIDs))
		for srcUUID := range srcUUIDs {
			sources = append(sources, uuidLookup[srcUUID])
		}
		mergeDict[tgtUUID] = sources
	}

	// 解析 mergeDict，确保 mergeDict 和 result 同步
	mergeDict = resolveMergeDict(mergeDict, result, uuidLookup)

	// 构建合并对列表
	mergePairs := make([]MergePair, 0, len(mergeDict))
	for tgtUUID, srcEntities := range mergeDict {
		mergePairs = append(mergePairs, MergePair{
			Target:  uuidLookup[tgtUUID],
			Sources: srcEntities,
		})
	}

	// 查找待删除的 UUID
	toRemove := findToRemove(mergeDict)

	return result, mergePairs, toRemove
}

// ParseRelationMerging 解析关系合并 LLM 响应
//
// 如果 need_merging 为真且有合并内容，更新 relation 并返回待删除的 UUID 集合。
//
// Python: parse_relation_merging(response, relation, existing_relations)
func ParseRelationMerging(response map[string]any, relation *graph.Relation, existingRelations []map[string]any) map[string]struct{} {
	toRemove := make(map[string]struct{})
	numExisting := len(existingRelations)

	needMerge := toBool(response["need_merging"])
	content := strings.TrimSpace(anyToStr(response["combined_content"]))

	if needMerge && content != "" {
		relation.Content = content

		validSinceStr, _ := response["valid_since"].(string)
		validSince, offsetSince := ParseISO(validSinceStr)
		if validSince >= 0 {
			relation.ValidSince = validSince
			relation.OffsetSince = offsetSince
		}

		validUntilStr, _ := response["valid_until"].(string)
		validUntil, offsetUntil := ParseISO(validUntilStr)
		if validUntil >= 0 {
			relation.ValidUntil = validUntil
			relation.OffsetUntil = offsetUntil
		}

		dupIDs := response["duplicate_ids"]
		if ids, ok := dupIDs.([]any); ok {
			for _, idVal := range ids {
				if id, ok := toInt(idVal); ok && id > 0 && id <= numExisting {
					if uuid, ok := existingRelations[id-1]["uuid"].(string); ok {
						toRemove[uuid] = struct{}{}
					} else {
						toRemove["ERROR"] = struct{}{}
					}
				}
			}
		}
	}

	return toRemove
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseEntityMerging 解析单个实体合并请求
//
// Python: _parse_entity_merging(dup, merge_map, is_target, result, existing, *, tgt_entity, num_entities, num_existing)
func parseEntityMerging(
	dup map[string]any,
	mergeMap map[string]map[string]struct{},
	isTarget map[string]string,
	result []EntityOrDeclaration,
	existing []*graph.Entity,
	tgtEntity *graph.Entity,
	numEntities int,
	numExisting int,
) {
	dupIDs := dup["duplicate_ids"]
	ids, ok := dupIDs.([]any)
	if !ok {
		return
	}

	for _, idVal := range ids {
		dupID, ok := toInt(idVal)
		if !ok {
			continue
		}

		dupID = dupID - 1
		if numExisting <= dupID && dupID < numEntities {
			// 现有实体替换新的候选实体
			result[dupID-numExisting] = EntityOrDeclaration{Entity: tgtEntity}
		} else if dupID >= 0 && dupID < numExisting {
			// 现有实体替换另一个现有实体
			srcEntity := existing[dupID]
			if tgtEntity.UUID == srcEntity.UUID {
				continue
			}

			_, tgtInMerge := mergeMap[tgtEntity.UUID]
			_, srcInMerge := mergeMap[srcEntity.UUID]

			if tgtInMerge {
				isTarget[srcEntity.UUID] = tgtEntity.UUID
				mergeMap[tgtEntity.UUID][srcEntity.UUID] = struct{}{}
			} else if srcInMerge {
				isTarget[tgtEntity.UUID] = srcEntity.UUID
				mergeMap[srcEntity.UUID][tgtEntity.UUID] = struct{}{}
			} else {
				tgtOfTgtUUID := tgtEntity.UUID
				if t, ok := isTarget[tgtEntity.UUID]; ok {
					tgtOfTgtUUID = t
				}
				isTarget[srcEntity.UUID] = tgtOfTgtUUID
				if _, ok := mergeMap[tgtOfTgtUUID]; !ok {
					mergeMap[tgtOfTgtUUID] = make(map[string]struct{})
				}
				mergeMap[tgtOfTgtUUID][srcEntity.UUID] = struct{}{}
			}
		}
	}
}

// resolveMergeDict 解析合并字典，确保 mergeDict 和 result 同步
//
// Python: _resolve_merge_dict(merge_dict, result, uuid_lookup)
func resolveMergeDict(
	mergeDict map[string][]*graph.Entity,
	result []EntityOrDeclaration,
	uuidLookup map[string]*graph.Entity,
) map[string][]*graph.Entity {
	mergeDictSorted := make(map[string][]*graph.Entity)

	for tgtUUID, srcEntities := range mergeDict {
		tgt := uuidLookup[tgtUUID]

		// 记录需要在 result 列表中替换的索引
		var replaceIdxList []int
		replaceCount := make(map[string]int)
		for _, src := range srcEntities {
			replaceCount[src.UUID] = 0
		}

		for _, src := range srcEntities {
			for idx, e := range result {
				if e.Entity == src {
					replaceIdxList = append(replaceIdxList, idx)
					replaceCount[src.UUID]++
				}
			}
		}

		// 检查目标实体是否在 result 中
		tgtInResult := false
		for _, e := range result {
			if e.Entity == tgt {
				tgtInResult = true
				break
			}
		}

		if tgtInResult || len(replaceIdxList) == 0 {
			// 目标实体在 result 中，或没有源实体在 result 中
			mergeDictSorted[tgtUUID] = srcEntities
			for _, idx := range replaceIdxList {
				result[idx] = EntityOrDeclaration{Entity: tgt}
			}
		} else {
			// 至少一个源实体在 result 中，但目标实体不在 result 中
			// 选择出现次数最多的源实体作为新目标
			newTgtUUID := findMaxCountUUID(replaceCount)
			newTgt := uuidLookup[newTgtUUID]

			// 将原目标添加到源列表，移除新目标
			srcEntities = append(srcEntities, tgt)
			srcEntities = removeEntityFromSlice(srcEntities, newTgt)

			mergeDictSorted[newTgtUUID] = srcEntities
			for _, idx := range replaceIdxList {
				result[idx] = EntityOrDeclaration{Entity: newTgt}
			}
		}
	}

	return mergeDictSorted
}

// findToRemove 查找因合并需要从数据库删除的实体 UUID 集合
//
// Python: _find_to_remove(merge_dict)
func findToRemove(mergeDict map[string][]*graph.Entity) map[string]struct{} {
	toRemove := make(map[string]struct{})
	for _, entityList := range mergeDict {
		for _, e := range entityList {
			toRemove[e.UUID] = struct{}{}
		}
	}
	// 目标实体的 UUID 不需要删除
	for tgtUUID := range mergeDict {
		delete(toRemove, tgtUUID)
	}
	return toRemove
}

// dedupEntitiesByUUID 按 UUID 去重实体（保留最后一个）
//
// Python: list({entity.uuid: entity for entity in entities}.values())
func dedupEntitiesByUUID(entities []*graph.Entity) []*graph.Entity {
	seen := make(map[string]*graph.Entity)
	for _, e := range entities {
		seen[e.UUID] = e
	}
	result := make([]*graph.Entity, 0, len(seen))
	for _, e := range seen {
		result = append(result, e)
	}
	return result
}

// toInt 将 any 值转换为 int（支持 int/int64/float64/string）
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// toBool 将 any 值转换为 bool（支持 bool/float64/int/string）
func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	case int:
		return b != 0
	case string:
		return strings.ToLower(b) == "true"
	default:
		return false
	}
}

// anyToStr 将 any 值转换为字符串
func anyToStr(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

// findMaxCountUUID 找到计数值最大的 UUID
func findMaxCountUUID(counts map[string]int) string {
	maxCount := -1
	var result string
	for uuid, count := range counts {
		if count > maxCount {
			maxCount = count
			result = uuid
		}
	}
	return result
}

// removeEntityFromSlice 从实体切片中移除第一个匹配的实体
func removeEntityFromSlice(s []*graph.Entity, target *graph.Entity) []*graph.Entity {
	for i, e := range s {
		if e == target {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
