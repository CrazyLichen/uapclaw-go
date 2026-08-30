package search

import (
	"context"
	"fmt"
	"sort"

	storeindex "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchParams 搜索参数。
//
// 对应 Python: openjiuwen/core/memory/manage/search/search_manager.py (SearchParams)
type SearchParams struct {
	// UserID 用户 ID
	UserID string
	// ScopeID 范围 ID
	ScopeID string
	// Query 搜索查询文本
	Query string
	// TopK 返回的最大结果数
	TopK int
	// Threshold 匹配阈值
	Threshold float64
	// SearchType 指定搜索的记忆类型（可选）
	SearchType []string
}

// SearchManager 搜索操作统一路由器。
//
// 语义搜索按 search_type 分发到各 Manager；列表/分页直接走 memory_index。
// 三种 Fragment 类型共享同一个 FragmentMemoryManager 实例，需去重（对齐 Python: set(self.managers.values())）。
//
// 对应 Python: openjiuwen/core/memory/manage/search/search_manager.py (SearchManager)
type SearchManager struct {
	// managers 记忆类型 → Manager 实例映射
	managers map[string]index.BaseMemoryManager
	// cryptoKey 加密密钥
	cryptoKey []byte
	// memoryIndex 记忆索引
	memoryIndex storeindex.BaseMemoryIndex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// DefaultTopK 默认返回结果数。对齐 Python: SearchParams.top_k default=5
const DefaultTopK = 5

// DefaultThreshold 默认匹配阈值。对齐 Python: SearchParams.threshold default=0.3
const DefaultThreshold = 0.3

// ──────────────────────────── 全局变量 ────────────────────────────

// allMemManagerList 所有合法的记忆类型（对齐 Python: all_mem_manager_list = [item.value for item in MemoryType]）
var allMemManagerList = mem_model.AllMemoryTypeValues()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSearchManager 创建搜索管理器。
//
// 对齐 Python: SearchManager.__init__(managers, crypto_key, memory_index)
func NewSearchManager(managers map[string]index.BaseMemoryManager, cryptoKey []byte, memoryIndex storeindex.BaseMemoryIndex) *SearchManager {
	return &SearchManager{
		managers:    managers,
		cryptoKey:   cryptoKey,
		memoryIndex: memoryIndex,
	}
}

// NewSearchParams 创建默认搜索参数。
func NewSearchParams(userID, scopeID, query string) *SearchParams {
	return &SearchParams{
		UserID:     userID,
		ScopeID:    scopeID,
		Query:      query,
		TopK:       DefaultTopK,
		Threshold:  DefaultThreshold,
		SearchType: nil,
	}
}

// Search 语义搜索记忆。
//
// 按 search_type 分发到对应 Manager 的 Search()；无类型时遍历所有 Manager；
// 结果按 score 降序截断 top_k，过滤 threshold。
//
// 对齐 Python: SearchManager.search(params, **kwargs)
func (s *SearchManager) Search(ctx context.Context, params *SearchParams) ([]*storeindex.MemorySearchResult, error) {
	userID := params.UserID
	scopeID := params.ScopeID
	query := params.Query
	topK := params.TopK
	threshold := params.Threshold
	searchType := params.SearchType

	// 校验 search_type 合法性（对齐 Python: if st not in self.all_mem_manager_list）
	if searchType != nil {
		for _, st := range searchType {
			if !containsString(allMemManagerList, st) {
				return nil, exception.NewBaseError(
					exception.StatusMemoryGetMemoryExecutionError,
					exception.WithParam("memory_type", st),
					exception.WithMsg(fmt.Sprintf("%s is not a valid search type", st)),
				)
			}
		}
	}

	// 校验 search_type 对应 Manager 是否已初始化
	// 对齐 Python: if st and not self.managers.get(st)
	usedTypes := make(map[index.BaseMemoryManager][]string)
	if searchType != nil {
		for _, st := range searchType {
			manager, ok := s.managers[st]
			if !ok {
				return nil, exception.NewBaseError(
					exception.StatusMemoryGetMemoryExecutionError,
					exception.WithParam("memory_type", st),
					exception.WithMsg(fmt.Sprintf("%s memory manager not inited", st)),
				)
			}
			usedTypes[manager] = append(usedTypes[manager], st)
		}
	}

	var allResults []*storeindex.MemorySearchResult

	if searchType == nil {
		// 无 search_type → 遍历所有 Manager（去重）
		seen := make(map[index.BaseMemoryManager]bool)
		for _, manager := range s.managers {
			if seen[manager] {
				continue
			}
			seen[manager] = true
			res, err := manager.Search(ctx, userID, scopeID, query, topK, nil)
			if err != nil {
				continue
			}
			allResults = append(allResults, res...)
		}
	} else {
		// 按 search_type 路由（去重后调用）
		for manager, types := range usedTypes {
			res, err := manager.Search(ctx, userID, scopeID, query, topK, types)
			if err != nil {
				continue
			}
			allResults = append(allResults, res...)
		}
	}

	// 排序 + 截断 + threshold 过滤（对齐 Python: sorted + [:top_k] + threshold）
	if len(allResults) > topK {
		sort.Slice(allResults, func(i, j int) bool {
			return allResults[i].Score > allResults[j].Score
		})
	}
	var filtered []*storeindex.MemorySearchResult
	for _, r := range allResults {
		if r.Score >= threshold {
			filtered = append(filtered, r)
		}
	}
	if topK > 0 && len(filtered) > topK {
		filtered = filtered[:topK]
	}
	return filtered, nil
}

// ListUserMem 分页列出用户记忆。
//
// 对齐 Python: SearchManager.list_user_mem(user_id, scope_id, nums, pages, mem_type)
func (s *SearchManager) ListUserMem(ctx context.Context, userID string, scopeID string, nums int, pages int, memType string) ([]*storeindex.MemoryDoc, error) {
	if s.memoryIndex == nil {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "search_memory"),
			exception.WithMsg("memory index not inited"),
		)
	}
	start := nums * (pages - 1)
	var memTypes []string
	if memType != "" {
		memTypes = []string{memType}
	}
	return s.memoryIndex.ListMemories(ctx, userID, scopeID, start, nums, memTypes)
}

// ListUserProfile 列出用户画像记忆。
//
// 对齐 Python: SearchManager.list_user_profile(user_id, scope_id)
func (s *SearchManager) ListUserProfile(ctx context.Context, userID string, scopeID string) ([]*storeindex.MemoryDoc, error) {
	// 检查 Fragment 类型管理器是否已初始化（对齐 Python: if any item not in managers for item in FRAGMENT_MEMORY_TYPE）
	for _, fragType := range index.FragmentMemoryTypes {
		if _, ok := s.managers[fragType]; !ok {
			return nil, exception.NewBaseError(
				exception.StatusMemoryGetMemoryExecutionError,
				exception.WithParam("memory_type", "fragment_memory"),
				exception.WithMsg("fragment memory manager not inited"),
			)
		}
	}
	// 检查 user_profile 管理器是否是 FragmentMemoryManager（对齐 Python: isinstance check）
	manager, ok := s.managers[mem_model.MemoryTypeUserProfile.String()]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "fragment_memory"),
			exception.WithMsg("fragment memory manager not inited"),
		)
	}
	fm, ok := manager.(*index.FragmentMemoryManager)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", "fragment_memory"),
			exception.WithMsg("fragment memory manager class is not FragmentMemoryManager"),
		)
	}
	return fm.ListFragmentMemories(ctx, userID, scopeID, 0, 0, "")
}

// ListUserSummary 列出用户摘要记忆。
//
// 对齐 Python: SearchManager.list_user_summary(user_id, scope_id)
func (s *SearchManager) ListUserSummary(ctx context.Context, userID string, scopeID string) ([]*storeindex.MemoryDoc, error) {
	manager, ok := s.managers[mem_model.MemoryTypeSummary.String()]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeSummary.String()),
			exception.WithMsg(fmt.Sprintf("%s memory manager not inited", mem_model.MemoryTypeSummary.String())),
		)
	}
	sm, ok := manager.(*index.SummaryManager)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeSummary.String()),
			exception.WithMsg(fmt.Sprintf("%s manager class is not SummaryManager", mem_model.MemoryTypeSummary.String())),
		)
	}
	return sm.ListUserSummary(ctx, userID, scopeID, 0, 0)
}

// GetUserVariable 获取用户变量。
//
// 对齐 Python: SearchManager.get_user_variable(user_id, scope_id, var_name)
func (s *SearchManager) GetUserVariable(ctx context.Context, userID string, scopeID string, varName string) (string, error) {
	manager, ok := s.managers[mem_model.MemoryTypeVariable.String()]
	if !ok {
		return "", exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeVariable.String()),
			exception.WithMsg(fmt.Sprintf("%s memory manager not inited", mem_model.MemoryTypeVariable.String())),
		)
	}
	vm, ok := manager.(*index.VariableManager)
	if !ok {
		return "", exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeVariable.String()),
			exception.WithMsg(fmt.Sprintf("%s manager class is not VariableManager", mem_model.MemoryTypeVariable.String())),
		)
	}
	// 对齐 Python: query_variable(user_id, scope_id, name=var_name)
	// Go 中 QueryVariable(userID, scopeID, name, sessionID)，sessionID 为空表示用户级
	res, err := vm.QueryVariable(ctx, userID, scopeID, varName, "")
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	if v, exists := res[varName]; exists {
		return v, nil
	}
	return "", nil
}

// GetAllUserVariable 获取用户所有变量。
//
// 对齐 Python: SearchManager.get_all_user_variable(user_id, scope_id)
func (s *SearchManager) GetAllUserVariable(ctx context.Context, userID string, scopeID string) (map[string]string, error) {
	manager, ok := s.managers[mem_model.MemoryTypeVariable.String()]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeVariable.String()),
			exception.WithMsg(fmt.Sprintf("%s memory manager not inited", mem_model.MemoryTypeVariable.String())),
		)
	}
	vm, ok := manager.(*index.VariableManager)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithParam("memory_type", mem_model.MemoryTypeVariable.String()),
			exception.WithMsg(fmt.Sprintf("%s manager class is not VariableManager", mem_model.MemoryTypeVariable.String())),
		)
	}
	// 对齐 Python: query_variable(user_id, scope_id) — name 为空表示查询所有
	return vm.QueryVariable(ctx, userID, scopeID, "", "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsString 检查字符串是否在切片中。
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
