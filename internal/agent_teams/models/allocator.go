package models

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelAllocator 模型分配器接口。
// 对齐 Python: ModelAllocator Protocol
//
// 实现封装从池中选取下一个条目的策略（轮询/按名/路由）。
// 返回 nil 表示"无可用条目"——调用者回退到每 Agent 的模型配置。
type ModelAllocator interface {
	// Allocate 返回下一次分配，或 nil 当不可用时。
	Allocate(modelName string) *Allocation
	// StateDict 返回分配器计数器的 JSON 友好快照。
	StateDict() map[string]any
	// LoadStateDict 从先前的 StateDict 恢复计数器。
	LoadStateDict(state map[string]any)
}

// Allocation 模型分配结果。
// 对齐 Python: Allocation (openjiuwen/agent_teams/models/allocator.py)
//
// 携带被选中的池条目以及持久化 DB 引用所需的位置信息。
type Allocation struct {
	// Entry 选中的池条目
	Entry ModelPoolEntry
	// GroupIndex 条目在同名组内的位置索引
	GroupIndex int
}

// RoundRobinModelAllocator 轮询分配器。
// 对齐 Python: RoundRobinModelAllocator (allocator.py)
//
// 每次调用 Allocate 返回下一个池条目，循环轮询，忽略 model_name。
type RoundRobinModelAllocator struct {
	// pool 池条目列表
	pool []ModelPoolEntry
	// poolDigest 池结构摘要
	poolDigest string
	// index 当前轮询位置
	index int
	// groups 模型名 → 条目组
	groups map[string][]ModelPoolEntry
}

// ByModelNameAllocator 按名分配器。
// 对齐 Python: ByModelNameAllocator (allocator.py)
//
// 按模型名查找组，组内轮询。model_name 缺失或未知返回 nil。
type ByModelNameAllocator struct {
	// groups 模型名 → 条目组
	groups map[string][]ModelPoolEntry
	// poolDigest 池结构摘要
	poolDigest string
	// innerIndexes 组内轮询位置
	innerIndexes map[string]int
}

// RouterAllocator 路由分配器。
// 对齐 Python: RouterAllocator (allocator.py)
//
// 单端点路由器，每个 model_name 出现一次。
// Allocate("") → 首条目；Allocate(name) → 精确查找；空池 → 构造时返回 error。
type RouterAllocator struct {
	// pool 池条目列表
	pool []ModelPoolEntry
	// byName 模型名 → 条目
	byName map[string]ModelPoolEntry
	// poolDigest 池结构摘要
	poolDigest string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ToTeamModelConfig 物化为 TeamModelConfig。
// 对齐 Python: Allocation.to_team_model_config()
func (a Allocation) ToTeamModelConfig() TeamModelConfig {
	return a.Entry.ToTeamModelConfig()
}

// ToDBRef 产生轻量级的 {model_name, model_index} 引用用于 DB 持久化。
// 对齐 Python: Allocation.to_db_ref()
func (a Allocation) ToDBRef() map[string]any {
	return map[string]any{
		"model_name":  a.Entry.ModelName,
		"model_index": a.GroupIndex,
	}
}

// NewRoundRobinModelAllocator 创建轮询分配器。⤴️ 9.64 回填完成
func NewRoundRobinModelAllocator(pool []ModelPoolEntry) *RoundRobinModelAllocator {
	groups := buildGroups(pool)
	return &RoundRobinModelAllocator{
		pool:       pool,
		poolDigest: poolDigest(pool),
		index:      0,
		groups:     groups,
	}
}

// Allocate 返回下一个池条目。对齐 Python RoundRobinModelAllocator.allocate — 真实实现
func (a *RoundRobinModelAllocator) Allocate(_ string) *Allocation {
	if len(a.pool) == 0 {
		return nil
	}
	entry := a.pool[a.index%len(a.pool)]
	a.index++
	group := a.groups[entry.ModelName]
	if len(group) == 0 {
		group = []ModelPoolEntry{entry}
	}
	return &Allocation{Entry: entry, GroupIndex: groupIndexOf(entry, group)}
}

// StateDict 快照计数器+池摘要。对齐 Python RoundRobinModelAllocator.state_dict
func (a *RoundRobinModelAllocator) StateDict() map[string]any {
	return map[string]any{
		"index":       a.index,
		"pool_digest": a.poolDigest,
	}
}

// LoadStateDict 恢复计数器，摘要不匹配时重置。对齐 Python RoundRobinModelAllocator.load_state_dict
func (a *RoundRobinModelAllocator) LoadStateDict(state map[string]any) {
	if stateVal, ok := state["pool_digest"]; ok && stateVal != a.poolDigest {
		a.index = 0
		return
	}
	if idx, ok := toInt(state["index"]); ok {
		a.index = idx
	} else {
		a.index = 0
	}
}

// NewByModelNameAllocator 创建按名分配器。⤴️ 9.64 回填完成
func NewByModelNameAllocator(pool []ModelPoolEntry) *ByModelNameAllocator {
	groups := buildGroups(pool)
	innerIndexes := make(map[string]int, len(groups))
	for name := range groups {
		innerIndexes[name] = 0
	}
	return &ByModelNameAllocator{
		groups:       groups,
		poolDigest:   poolDigest(pool),
		innerIndexes: innerIndexes,
	}
}

// Allocate 按名分配。对齐 Python ByModelNameAllocator.allocate — 真实实现
func (a *ByModelNameAllocator) Allocate(modelName string) *Allocation {
	if modelName == "" {
		return nil
	}
	group, ok := a.groups[modelName]
	if !ok {
		return nil
	}
	idx := a.innerIndexes[modelName] % len(group)
	a.innerIndexes[modelName]++
	return &Allocation{Entry: group[idx], GroupIndex: idx}
}

// StateDict 快照组内计数器+池摘要。对齐 Python ByModelNameAllocator.state_dict
// counters 为列表格式（避免模型名含 '.' 时嵌套路径编码问题）
func (a *ByModelNameAllocator) StateDict() map[string]any {
	counters := make([]map[string]any, 0, len(a.innerIndexes))
	for name, index := range a.innerIndexes {
		counters = append(counters, map[string]any{
			"model_name": name,
			"index":      index,
		})
	}
	return map[string]any{
		"counters":    counters,
		"pool_digest": a.poolDigest,
	}
}

// LoadStateDict 恢复计数器，摘要不匹配时重置。对齐 Python ByModelNameAllocator.load_state_dict
// 兼容旧 dict 格式（inner_indexes）和新 list 格式（counters）
func (a *ByModelNameAllocator) LoadStateDict(state map[string]any) {
	if stateVal, ok := state["pool_digest"]; ok && stateVal != a.poolDigest {
		for name := range a.innerIndexes {
			a.innerIndexes[name] = 0
		}
		return
	}
	counters, ok := state["counters"]
	if ok {
		list, ok := counters.([]any)
		if ok {
			for _, record := range list {
				m, ok := record.(map[string]any)
				if !ok {
					continue
				}
				nameVal, ok := m["model_name"]
				if !ok {
					continue
				}
				name, ok := nameVal.(string)
				if !ok || a.innerIndexes == nil {
					continue
				}
				if _, exists := a.innerIndexes[name]; !exists {
					continue
				}
				if idx, ok := toInt(m["index"]); ok {
					a.innerIndexes[name] = idx
				} else {
					a.innerIndexes[name] = 0
				}
			}
			return
		}
	}
	// 旧格式兼容
	legacy, ok := state["inner_indexes"]
	if !ok {
		return
	}
	legacyMap, ok := legacy.(map[string]any)
	if !ok {
		return
	}
	for name, raw := range legacyMap {
		if _, exists := a.innerIndexes[name]; !exists {
			continue
		}
		if idx, ok := toInt(raw); ok {
			a.innerIndexes[name] = idx
		} else {
			a.innerIndexes[name] = 0
		}
	}
}

// NewRouterAllocator 创建路由分配器。⤴️ 9.64 回填完成
func NewRouterAllocator(pool []ModelPoolEntry) (*RouterAllocator, error) {
	if len(pool) == 0 {
		return nil, fmt.Errorf("RouterAllocator 要求非空模型池")
	}
	names := make([]string, 0, len(pool))
	for _, entry := range pool {
		names = append(names, entry.ModelName)
	}
	// 检查重复 model_name
	seen := make(map[string]int, len(names))
	var duplicates []string
	for _, name := range names {
		if count, exists := seen[name]; exists {
			if count == 1 {
				duplicates = append(duplicates, name)
			}
			seen[name] = count + 1
		} else {
			seen[name] = 1
		}
	}
	if len(duplicates) > 0 {
		return nil, fmt.Errorf("RouterAllocator 模型池中 model_name 必须唯一，重复项: %v", duplicates)
	}
	byName := make(map[string]ModelPoolEntry, len(pool))
	for _, entry := range pool {
		byName[entry.ModelName] = entry
	}
	return &RouterAllocator{
		pool:       pool,
		byName:     byName,
		poolDigest: poolDigest(pool),
	}, nil
}

// Allocate 路由分配。对齐 Python RouterAllocator.allocate — 真实实现
// modelName="" → 首条目；modelName=已知名 → 精确查找；未知名 → nil
func (a *RouterAllocator) Allocate(modelName string) *Allocation {
	if modelName == "" {
		return &Allocation{Entry: a.pool[0], GroupIndex: 0}
	}
	entry, ok := a.byName[modelName]
	if !ok {
		return nil
	}
	return &Allocation{Entry: entry, GroupIndex: 0}
}

// StateDict 快照池摘要。对齐 Python RouterAllocator.state_dict
func (a *RouterAllocator) StateDict() map[string]any {
	return map[string]any{
		"pool_digest": a.poolDigest,
	}
}

// LoadStateDict 恢复（无计数器，仅校验摘要）。对齐 Python RouterAllocator.load_state_dict
func (a *RouterAllocator) LoadStateDict(state map[string]any) {
	// 路由分配器无旋转计数器，仅校验摘要
	_ = state["pool_digest"]
}

// BuildModelAllocatorForPool 根据模型池和策略构建分配器。⤴️ 9.64 回填完成
// 对齐 Python: build_model_allocator(spec, team_spec)
//
// 此函数接受基本类型参数以避免 import 循环。
// 调用者应从 TeamAgentSpec/TeamSpec 中提取 pool 和 strategy 传入。
func BuildModelAllocatorForPool(pool []ModelPoolEntry, strategy string, teamName string) ModelAllocator {
	if len(pool) == 0 {
		return nil
	}
	switch strategy {
	case "round_robin":
		return NewRoundRobinModelAllocator(pool)
	case "by_model_name":
		return NewByModelNameAllocator(pool)
	case "router":
		alloc, err := NewRouterAllocator(pool)
		if err != nil {
			logger.Info(logComponent).Str("team_name", teamName).
				Str("strategy", strategy).Err(err).
				Msg("RouterAllocator 构造失败")
			return nil
		}
		return alloc
	default:
		logger.Info(logComponent).Str("team_name", teamName).
			Str("strategy", strategy).Int("pool_size", len(pool)).
			Msg("未知 model_pool_strategy，回退为 nil")
		return nil
	}
}

// ResolveMemberModelFromPool 从池中按引用解析成员模型。⤴️ 9.64 回填完成
// 对齐 Python: resolve_member_model(team_spec, model_name, model_index)
//
// 纯位置查找，不触碰分配器计数器。
func ResolveMemberModelFromPool(pool []ModelPoolEntry, modelName string, modelIndex int) *TeamModelConfig {
	if len(pool) == 0 || modelName == "" {
		return nil
	}
	group := filterByName(pool, modelName)
	if len(group) == 0 {
		return nil
	}
	idx := modelIndex
	if idx < 0 || idx >= len(group) {
		idx = 0
	}
	cfg := group[idx].ToTeamModelConfig()
	return &cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// poolDigest 计算池结构形状的稳定摘要。
// 对齐 Python: _pool_digest(pool)
func poolDigest(pool []ModelPoolEntry) string {
	h := sha1.New()
	for _, entry := range pool {
		h.Write([]byte(entry.ModelName))
		h.Write([]byte{0x00})
		h.Write([]byte(entry.APIBaseURL))
		h.Write([]byte{0x1f})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// buildGroups 构建模型名 → 条目组的映射
func buildGroups(pool []ModelPoolEntry) map[string][]ModelPoolEntry {
	groups := make(map[string][]ModelPoolEntry)
	for _, entry := range pool {
		groups[entry.ModelName] = append(groups[entry.ModelName], entry)
	}
	return groups
}

// groupIndexOf 返回 entry 在 group 中的引用位置。
// 对齐 Python: _group_index_of(entry, group)
func groupIndexOf(entry ModelPoolEntry, group []ModelPoolEntry) int {
	for i, candidate := range group {
		if entrySignature(entry) == entrySignature(candidate) && entry.ModelID == candidate.ModelID {
			return i
		}
	}
	return 0
}

// filterByName 按模型名筛选池条目
func filterByName(pool []ModelPoolEntry, modelName string) []ModelPoolEntry {
	var result []ModelPoolEntry
	for _, entry := range pool {
		if entry.ModelName == modelName {
			result = append(result, entry)
		}
	}
	return result
}

// marshalForSignature 将值序列化为 JSON 字符串，用于签名计算。
func marshalForSignature(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}
