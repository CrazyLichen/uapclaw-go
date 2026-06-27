package resources_manager

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TagMgr 标签管理器，维护资源与标签的双向映射关系。
//
// 对应 Python: TagMgr (openjiuwen/core/runner/resources_manager/tag_manager.py)
// 两个索引：
//   - resourceTags: 资源ID → 标签集合（正向索引）
//   - tagToResource: 标签 → 资源ID集合（反向索引）
//
// TagGlobal 是特殊内置标签，初始化时在 tagToResource 中预建空集合。
type TagMgr struct {
	// resourceTags 资源ID → 标签集合
	resourceTags map[string]map[Tag]struct{}
	// tagToResource 标签 → 资源ID集合
	tagToResource map[Tag]map[string]struct{}
	// mu 读写锁，保护并发访问
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTagMgr 创建标签管理器，初始化 TagGlobal 对应空集合。
//
// 对应 Python: TagMgr.__init__()
func NewTagMgr() *TagMgr {
	return &TagMgr{
		resourceTags:  make(map[string]map[Tag]struct{}),
		tagToResource: map[Tag]map[string]struct{}{TagGlobal: {}},
	}
}

// HasTag 检查标签是否存在。
//
// 对应 Python: TagMgr.has_tag(tag)
func (m *TagMgr) HasTag(tag Tag) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.tagToResource[tag]
	return ok
}

// ListTags 获取所有标签（排除空标签）。
//
// 对应 Python: TagMgr.list_tags()
func (m *TagMgr) ListTags() []Tag {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Tag, 0)
	for tag, resources := range m.tagToResource {
		if len(resources) > 0 {
			result = append(result, tag)
		}
	}
	return result
}

// HasResource 检查资源是否存在。
//
// 对应 Python: TagMgr.has_resource(resource_id)
func (m *TagMgr) HasResource(resourceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.resourceTags[resourceID]
	return ok
}

// TagResource 为资源添加标签（原子操作）。
// 如果包含 TagGlobal，则执行 GLOBAL 特殊逻辑：GLOBAL 资源不能有其他标签。
//
// 对应 Python: TagMgr.tag_resource(resource_id, tags)
func (m *TagMgr) TagResource(resourceID string, tags []Tag) []Tag {
	tagsToAdd := normalizeTags(tags)

	m.mu.Lock()
	defer m.mu.Unlock()

	// 确保资源存在
	if _, ok := m.resourceTags[resourceID]; !ok {
		m.resourceTags[resourceID] = make(map[Tag]struct{})
	}

	// 检查是否包含 GLOBAL 标签
	if _, hasGlobal := tagsToAdd[TagGlobal]; hasGlobal {
		oldTags := m.setGlobalResource(resourceID)
		logger.Info(logger.ComponentCommon).
			Str("resource_id", resourceID).
			Strs("old_tags", oldTags).
			Msg("已为资源添加 GLOBAL 标签，变更为 [GLOBAL]")
		return []Tag{TagGlobal}
	}

	// 添加标签
	currentTags := m.addResourceTags(resourceID, tagsToAdd)

	logger.Info(logger.ComponentCommon).
		Str("resource_id", resourceID).
		Strs("added_tags", tagSetToSortedSlice(tagsToAdd)).
		Strs("current_tags", currentTags).
		Msg("已为资源添加标签")
	return currentTags
}

// RemoveResource 完全移除资源及其所有标签。
//
// 对应 Python: TagMgr.remove_resource(resource_id)
func (m *TagMgr) RemoveResource(resourceID string) []Tag {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.resourceTags[resourceID]; !ok {
		return []Tag{}
	}

	removedTags := m.removeResource(resourceID)

	logger.Info(logger.ComponentCommon).
		Str("resource_id", resourceID).
		Strs("removed_tags", removedTags).
		Msg("已移除资源")
	return removedTags
}

// RemoveResourceTags 移除资源的指定标签。
// skipIfNotExists 为 true 时跳过不存在的标签，否则返回错误。
//
// 对应 Python: TagMgr.remove_resource_tags(resource_id, tags, skip_if_not_exists)
func (m *TagMgr) RemoveResourceTags(resourceID string, tags []Tag, skipIfNotExists bool) ([]Tag, error) {
	tagsToRemove := normalizeTags(tags)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.resourceTags[resourceID]; !ok {
		return nil, exception.BuildError(exception.StatusResourceTagRemoveResourceTagError,
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("tags", fmt.Sprintf("%v", tags)),
			exception.WithParam("reason", "Resource does not exist"),
		)
	}

	currentTags := m.resourceTags[resourceID]

	// 检查是否所有要删除的标签都存在
	if !skipIfNotExists {
		nonExistent := make([]Tag, 0)
		for tag := range tagsToRemove {
			if _, ok := currentTags[tag]; !ok {
				nonExistent = append(nonExistent, tag)
			}
		}
		if len(nonExistent) > 0 {
			return nil, exception.BuildError(exception.StatusResourceTagRemoveResourceTagError,
				exception.WithParam("resource_id", resourceID),
				exception.WithParam("tags", fmt.Sprintf("%v", nonExistent)),
				exception.WithParam("reason", "Tag does not exist"),
			)
		}
	}

	remainingTags := m.removeResourceTagsInternal(resourceID, tagsToRemove)

	logger.Info(logger.ComponentCommon).
		Str("resource_id", resourceID).
		Strs("removed_tags", tagSetToSortedSlice(tagsToRemove)).
		Strs("remaining_tags", remainingTags).
		Msg("已从资源移除标签")
	return remainingTags, nil
}

// UpdateResourceTags 更新资源标签。
// 如果包含 TagGlobal，则执行 GLOBAL 特殊逻辑。
//
// 对应 Python: TagMgr.update_resource_tags(resource_id, tags, tag_update_strategy)
func (m *TagMgr) UpdateResourceTags(resourceID string, tags []Tag, strategy TagUpdateStrategy) ([]Tag, error) {
	newTags := normalizeTags(tags)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.resourceTags[resourceID]; !ok {
		return nil, exception.BuildError(exception.StatusResourceTagReplaceResourceTagError,
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("tags", fmt.Sprintf("%v", tags)),
			exception.WithParam("reason", "Resource does not exist"),
		)
	}

	// 检查是否包含 GLOBAL 标签
	if _, hasGlobal := newTags[TagGlobal]; hasGlobal {
		oldTags := m.setGlobalResource(resourceID)
		logger.Info(logger.ComponentCommon).
			Str("resource_id", resourceID).
			Str("strategy", strategy.String()).
			Strs("old_tags", oldTags).
			Msg("已将资源更新为 GLOBAL")
		return []Tag{TagGlobal}, nil
	}

	// 根据策略执行操作
	switch strategy {
	case TagUpdateReplace:
		currentTags := m.replaceResourceTags(resourceID, newTags)
		logger.Info(logger.ComponentCommon).
			Str("resource_id", resourceID).
			Strs("new_tags", tagSetToSortedSlice(newTags)).
			Msg("已替换资源标签")
		return currentTags, nil
	case TagUpdateMerge:
		currentTags := m.addResourceTags(resourceID, newTags)
		logger.Info(logger.ComponentCommon).
			Str("resource_id", resourceID).
			Strs("added_tags", tagSetToSortedSlice(newTags)).
			Strs("current_tags", currentTags).
			Msg("已合并资源标签")
		return currentTags, nil
	default:
		return nil, exception.BuildError(exception.StatusResourceTagReplaceResourceTagError,
			exception.WithParam("resource_id", resourceID),
			exception.WithParam("tags", fmt.Sprintf("%v", tags)),
			exception.WithParam("reason", fmt.Sprintf("Unsupported strategy: %v", strategy)),
		)
	}
}

// RemoveTag 完全移除标签及其所有关联。
// skipIfNotExists 为 true 时跳过不存在的标签，否则返回错误。
//
// 对应 Python: TagMgr.remove_tag(tag, skip_if_not_exists)
func (m *TagMgr) RemoveTag(tag Tag, skipIfNotExists bool) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tagToResource[tag]; !ok {
		if skipIfNotExists {
			return []string{}, nil
		}
		return nil, exception.BuildError(exception.StatusResourceTagRemoveTagError,
			exception.WithParam("tag", tag),
			exception.WithParam("reason", "Tag does not exist"),
		)
	}

	affectedResources := m.removeTagInternal(tag)

	logger.Info(logger.ComponentCommon).
		Str("tag", tag).
		Strs("affected_resources", affectedResources).
		Msg("已移除标签")
	return affectedResources, nil
}

// GetTagResources 获取指定标签的所有资源。
//
// 对应 Python: TagMgr.get_tag_resources(tag)
func (m *TagMgr) GetTagResources(tag Tag) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	resources, ok := m.tagToResource[tag]
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(resources))
	for resourceID := range resources {
		result = append(result, resourceID)
	}
	return result
}

// FindResourcesByTags 根据标签查找资源。
// strategy 为 TagMatchAll 时，资源必须包含所有指定标签；
// strategy 为 TagMatchAny 时，资源包含任一指定标签即可。
// skipIfNotExists 为 true 时跳过不存在的标签，否则返回错误。
//
// 对应 Python: TagMgr.find_resources_by_tags(tags, tag_match_strategy, skip_if_not_exists)
func (m *TagMgr) FindResourcesByTags(tags []Tag, strategy TagMatchStrategy, skipIfNotExists bool) ([]string, error) {
	tagsToSearch := normalizeTags(tags)

	m.mu.RLock()
	defer m.mu.RUnlock()

	switch strategy {
	case TagMatchAny:
		// ANY 策略：资源包含任一指定标签即可
		foundResources := make(map[string]struct{})
		for tag := range tagsToSearch {
			resources, ok := m.tagToResource[tag]
			if !ok || len(resources) == 0 {
				if !isBuiltinTag(tag) && !skipIfNotExists {
					return nil, exception.BuildError(exception.StatusResourceTagFindResourceError,
						exception.WithParam("tag", fmt.Sprintf("%v", tags)),
						exception.WithParam("strategy", strategy.String()),
						exception.WithParam("reason", fmt.Sprintf("Tag '%s' does not exist", tag)),
					)
				}
			} else {
				for resourceID := range resources {
					foundResources[resourceID] = struct{}{}
				}
			}
		}
		result := make([]string, 0, len(foundResources))
		for resourceID := range foundResources {
			result = append(result, resourceID)
		}
		return result, nil

	case TagMatchAll:
		return m.findResourcesWithAllTags(tagsToSearch, skipIfNotExists)

	default:
		return nil, exception.BuildError(exception.StatusResourceTagFindResourceError,
			exception.WithParam("tag", fmt.Sprintf("%v", tags)),
			exception.WithParam("strategy", strategy.String()),
			exception.WithParam("reason", "Unsupported tag match strategy"),
		)
	}
}

// HasResourceTag 检查资源是否拥有指定标签。
//
// 对应 Python: TagMgr.has_resource_tag(resource_id, tag)
func (m *TagMgr) HasResourceTag(resourceID string, tag Tag) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tags, ok := m.resourceTags[resourceID]
	if !ok {
		return false
	}
	_, hasTag := tags[tag]
	return hasTag
}

// GetResourcesTags 获取资源的所有标签。
//
// 对应 Python: TagMgr.get_resources_tags(resource_id)
func (m *TagMgr) GetResourcesTags(resourceID string) []Tag {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tags, ok := m.resourceTags[resourceID]
	if !ok {
		return []Tag{}
	}
	result := make([]Tag, 0, len(tags))
	for tag := range tags {
		result = append(result, tag)
	}
	return result
}

// Display 显示当前状态，返回格式化字符串。
// enableLog 为 true 时同时输出日志。
//
// 对应 Python: TagMgr.display(enable_log)
func (m *TagMgr) Display(enableLog bool) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	sb.WriteString("\nTag -> Resource IDs:\n")

	// 按标签排序输出
	sortedTags := make([]Tag, 0, len(m.tagToResource))
	for tag := range m.tagToResource {
		sortedTags = append(sortedTags, tag)
	}
	sort.Strings(sortedTags)
	for _, tag := range sortedTags {
		resourceIDs := m.tagToResource[tag]
		if len(resourceIDs) > 0 {
			sortedResources := make([]string, 0, len(resourceIDs))
			for id := range resourceIDs {
				sortedResources = append(sortedResources, id)
			}
			sort.Strings(sortedResources)
			sb.WriteString(fmt.Sprintf("  tag['%s']: [%s]\n", tag, strings.Join(sortedResources, ", ")))
		}
	}

	sb.WriteString("\nResource -> Tags:\n")

	// 按资源ID排序输出
	sortedResources := make([]string, 0, len(m.resourceTags))
	for id := range m.resourceTags {
		sortedResources = append(sortedResources, id)
	}
	sort.Strings(sortedResources)
	for _, resourceID := range sortedResources {
		tags := m.resourceTags[resourceID]
		sortedTagNames := make([]string, 0, len(tags))
		for tag := range tags {
			sortedTagNames = append(sortedTagNames, tag)
		}
		sort.Strings(sortedTagNames)
		sb.WriteString(fmt.Sprintf("  resource['%s']: [%s]\n", resourceID, strings.Join(sortedTagNames, ", ")))
	}

	// 统计信息
	globalResources := m.tagToResource[TagGlobal]
	sb.WriteString(fmt.Sprintf("\nStatistics:\n"))
	sb.WriteString(fmt.Sprintf("  Total tags: %d\n", len(m.tagToResource)))
	sb.WriteString(fmt.Sprintf("  Total resources: %d\n", len(m.resourceTags)))
	sb.WriteString(fmt.Sprintf("  GLOBAL resources: %d\n", len(globalResources)))

	msg := sb.String()
	if enableLog {
		logger.Info(logger.ComponentCommon).
			Str("msg", msg).
			Msg("---- 标签管理器状态 ----")
	}

	return msg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setGlobalResource 将资源设置为 GLOBAL 标签，GLOBAL 资源不能有其他标签。
// 调用方必须持有写锁。
//
// 对应 Python: TagMgr._set_global_resource(resource_id)
func (m *TagMgr) setGlobalResource(resourceID string) []Tag {
	// 获取旧标签
	oldTags := m.resourceTags[resourceID]
	oldTagsSlice := make([]Tag, 0, len(oldTags))
	for tag := range oldTags {
		oldTagsSlice = append(oldTagsSlice, tag)
	}

	// 从 tagToResource 中移除旧标签关联
	for oldTag := range oldTags {
		if resources, ok := m.tagToResource[oldTag]; ok {
			delete(resources, resourceID)
			// 如果标签下没有资源且不是 GLOBAL，删除标签
			if len(resources) == 0 && oldTag != TagGlobal {
				delete(m.tagToResource, oldTag)
			}
		}
	}

	// 更新 resourceTags
	m.resourceTags[resourceID] = map[Tag]struct{}{TagGlobal: {}}

	// 更新 tagToResource
	if _, ok := m.tagToResource[TagGlobal]; !ok {
		m.tagToResource[TagGlobal] = make(map[string]struct{})
	}
	m.tagToResource[TagGlobal][resourceID] = struct{}{}

	return oldTagsSlice
}

// addResourceTags 为资源添加多个标签。
// 调用方必须持有写锁。
//
// 对应 Python: TagMgr._add_resource_tags(resource_id, tags_to_add)
func (m *TagMgr) addResourceTags(resourceID string, tagsToAdd map[Tag]struct{}) []Tag {
	// 确保资源存在
	if _, ok := m.resourceTags[resourceID]; !ok {
		return []Tag{}
	}

	// 检查是否已是 GLOBAL 资源
	currentTags := m.resourceTags[resourceID]
	if _, hasGlobal := currentTags[TagGlobal]; hasGlobal {
		return []Tag{TagGlobal} // GLOBAL 资源不能有其他标签
	}

	// 添加新标签
	for tag := range tagsToAdd {
		currentTags[tag] = struct{}{}
	}

	// 更新 tagToResource
	for tag := range tagsToAdd {
		if _, ok := m.tagToResource[tag]; !ok {
			m.tagToResource[tag] = make(map[string]struct{})
		}
		m.tagToResource[tag][resourceID] = struct{}{}
	}

	result := make([]Tag, 0, len(currentTags))
	for tag := range currentTags {
		result = append(result, tag)
	}
	return result
}

// removeResource 完全移除资源。
// 调用方必须持有写锁。
//
// 对应 Python: TagMgr._remove_resource(resource_id)
func (m *TagMgr) removeResource(resourceID string) []Tag {
	if _, ok := m.resourceTags[resourceID]; !ok {
		return []Tag{}
	}

	// 获取资源的所有标签
	tags := m.resourceTags[resourceID]
	tagsSlice := make([]Tag, 0, len(tags))
	for tag := range tags {
		tagsSlice = append(tagsSlice, tag)
	}

	// 从 tagToResource 中移除关联
	for tag := range tags {
		if resources, ok := m.tagToResource[tag]; ok {
			delete(resources, resourceID)
			// 如果标签下没有资源且不是 GLOBAL，删除标签
			if len(resources) == 0 && tag != TagGlobal {
				delete(m.tagToResource, tag)
			}
		}
	}

	// 从 resourceTags 中移除资源
	delete(m.resourceTags, resourceID)

	return tagsSlice
}

// removeResourceTagsInternal 移除资源的指定标签（内部实现）。
// 调用方必须持有写锁。
//
// 对应 Python: TagMgr._remove_resource_tags(resource_id, tags_to_remove)
func (m *TagMgr) removeResourceTagsInternal(resourceID string, tagsToRemove map[Tag]struct{}) []Tag {
	if _, ok := m.resourceTags[resourceID]; !ok {
		return []Tag{}
	}

	currentTags := m.resourceTags[resourceID]

	// 移除指定标签
	for tag := range tagsToRemove {
		if _, hasTag := currentTags[tag]; hasTag {
			delete(currentTags, tag)
			// 从 tagToResource 中移除关联
			if resources, ok := m.tagToResource[tag]; ok {
				delete(resources, resourceID)
				// 如果标签下没有资源且不是 GLOBAL，删除标签
				if len(resources) == 0 && tag != TagGlobal {
					delete(m.tagToResource, tag)
				}
			}
		}
	}

	// 如果资源没有标签了，移除资源
	if len(currentTags) == 0 {
		delete(m.resourceTags, resourceID)
	}

	result := make([]Tag, 0, len(currentTags))
	for tag := range currentTags {
		result = append(result, tag)
	}
	return result
}

// replaceResourceTags 替换资源的所有标签。
// 调用方必须持有写锁。
//
// 对应 Python: TagMgr._replace_resource_tags(resource_id, new_tags)
func (m *TagMgr) replaceResourceTags(resourceID string, newTags map[Tag]struct{}) []Tag {
	if _, ok := m.resourceTags[resourceID]; !ok {
		return []Tag{}
	}

	// 获取旧标签
	oldTags := m.resourceTags[resourceID]

	// 从 tagToResource 中移除旧标签关联
	for oldTag := range oldTags {
		if resources, ok := m.tagToResource[oldTag]; ok {
			delete(resources, resourceID)
			// 如果标签下没有资源且不是 GLOBAL，删除标签
			if len(resources) == 0 && oldTag != TagGlobal {
				delete(m.tagToResource, oldTag)
			}
		}
	}

	// 设置新标签
	newTagSet := make(map[Tag]struct{}, len(newTags))
	for tag := range newTags {
		newTagSet[tag] = struct{}{}
	}
	m.resourceTags[resourceID] = newTagSet

	// 更新 tagToResource
	for tag := range newTags {
		if _, ok := m.tagToResource[tag]; !ok {
			m.tagToResource[tag] = make(map[string]struct{})
		}
		m.tagToResource[tag][resourceID] = struct{}{}
	}

	result := make([]Tag, 0, len(newTags))
	for tag := range newTags {
		result = append(result, tag)
	}
	return result
}

// removeTagInternal 完全移除标签（内部实现）。
// 调用方必须持有写锁。
//
// 对应 Python: TagMgr._remove_tag(tag)
func (m *TagMgr) removeTagInternal(tag Tag) []string {
	if _, ok := m.tagToResource[tag]; !ok {
		return []string{}
	}

	// 获取所有拥有此标签的资源
	affectedResources := m.tagToResource[tag]
	affectedSlice := make([]string, 0, len(affectedResources))
	for resourceID := range affectedResources {
		affectedSlice = append(affectedSlice, resourceID)
	}

	// 从每个资源的标签集合中移除此标签
	for resourceID := range affectedResources {
		if tags, ok := m.resourceTags[resourceID]; ok {
			delete(tags, tag)
			// 如果资源没有标签了，移除资源
			if len(tags) == 0 {
				delete(m.resourceTags, resourceID)
			}
		}
	}

	// 从 tagToResource 中移除标签
	delete(m.tagToResource, tag)

	return affectedSlice
}

// findResourcesWithAllTags 查找拥有所有指定标签的资源。
// 调用方必须持有读锁。
//
// 对应 Python: TagMgr._find_resources_with_all_tags(required_tags, skip_if_not_exists)
func (m *TagMgr) findResourcesWithAllTags(requiredTags map[Tag]struct{}, skipIfNotExists bool) ([]string, error) {
	if len(requiredTags) == 0 {
		return []string{}, nil
	}

	// 检查所有标签是否存在
	for tag := range requiredTags {
		if _, ok := m.tagToResource[tag]; !ok {
			if !isBuiltinTag(tag) && !skipIfNotExists {
				return nil, exception.BuildError(exception.StatusResourceTagFindResourceError,
					exception.WithParam("tag", fmt.Sprintf("%v", requiredTags)),
					exception.WithParam("strategy", TagMatchAll.String()),
					exception.WithParam("reason", fmt.Sprintf("Tag '%s' does not exist", tag)),
				)
			}
		}
	}

	// 获取第一个标签的资源集合作为初始集合
	var firstTag Tag
	for tag := range requiredTags {
		firstTag = tag
		break
	}
	firstResources, ok := m.tagToResource[firstTag]
	if !ok {
		return []string{}, nil
	}
	foundResources := make(map[string]struct{}, len(firstResources))
	for id := range firstResources {
		foundResources[id] = struct{}{}
	}

	// 逐一取交集：必须拥有所有标签
	for tag := range requiredTags {
		resources, ok := m.tagToResource[tag]
		if !ok {
			// 标签不存在，交集为空
			return []string{}, nil
		}
		for id := range foundResources {
			if _, hasResource := resources[id]; !hasResource {
				delete(foundResources, id)
			}
		}
	}

	result := make([]string, 0, len(foundResources))
	for id := range foundResources {
		result = append(result, id)
	}
	return result, nil
}

// normalizeTags 将标签切片归一化为集合。
//
// 对应 Python: TagMgr._normalize_tags(tags)
func normalizeTags(tags []Tag) map[Tag]struct{} {
	result := make(map[Tag]struct{}, len(tags))
	for _, tag := range tags {
		result[tag] = struct{}{}
	}
	return result
}

// isBuiltinTag 检查是否为内置标签（仅 TagGlobal）。
//
// 对应 Python: TagMgr._is_builtin_tag(tag)
func isBuiltinTag(tag Tag) bool {
	return tag == TagGlobal
}

// tagSetToSortedSlice 将标签集合转为排序后的切片，用于日志输出。
func tagSetToSortedSlice(tags map[Tag]struct{}) []Tag {
	result := make([]Tag, 0, len(tags))
	for tag := range tags {
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

// String 返回标签更新策略的字符串表示。
func (s TagUpdateStrategy) String() string {
	switch s {
	case TagUpdateMerge:
		return "MERGE"
	case TagUpdateReplace:
		return "REPLACE"
	default:
		return fmt.Sprintf("TagUpdateStrategy(%d)", int(s))
	}
}

// String 返回标签匹配策略的字符串表示。
func (s TagMatchStrategy) String() string {
	switch s {
	case TagMatchAll:
		return "ALL"
	case TagMatchAny:
		return "ANY"
	default:
		return fmt.Sprintf("TagMatchStrategy(%d)", int(s))
	}
}
