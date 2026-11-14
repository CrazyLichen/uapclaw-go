package models

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelAllocator 模型分配器接口。
// Python: ModelAllocator Protocol
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
// Python: Allocation (openjiuwen/agent_teams/models/allocator.py)
//
// 携带被选中的池条目以及持久化 DB 引用所需的位置信息。
type Allocation struct {
	// Entry 选中的池条目
	Entry ModelPoolEntry
	// GroupIndex 条目在同名组内的位置索引
	GroupIndex int
	// RouteTag 把模型组成员钉到 fallback_tag 匹配的部署子集（可选）。
	// Python: allocator.py:79-81 route_tag
	RouteTag string
	// PersistenceModelName DB 持久化引用用的模型名（缺省用 Entry.ModelName）。
	// Python: allocator.py:78 persistence_model_name
	PersistenceModelName string
}

// RoundRobinModelAllocator 轮询分配器。
// Python: RoundRobinModelAllocator (allocator.py)
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
// Python: ByModelNameAllocator (allocator.py)
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
// Python: RouterAllocator (allocator.py)
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

// IntelliRouterAllocator IntelliRouter 承载的池上的分配器。
// 分配刻意与 RouterAllocator 一致：IntelliRouter 池是"单端点"恰好位于客户端
// 侧的路由池——model_name 仍映射唯一条目，无 hint 的 Allocate 仍返回首个
// 声明名。差别只在条目的含义：RouterAllocator 下是一条远端端点，这里是
// 完整 deployment 列表加一个向路由器请求的模型名。这是池怎么造的属性，
// 不是怎么分配的属性——所以本分配器只加校验不加行为。
// 校验重要：池也可能是手写的（model_pool=[...] + strategy="intelli_router"）
// 而非从 IntelliRouterConfig 展开。provider 写错的手写条目会静默物化成
// 凭证为空的普通客户端——deployment 列表被忽略，故障只在首个请求才暴露。
// 构造期检查把它变成 build() 时的配置错误。
// Python: IntelliRouterAllocator (allocator.py:639)
type IntelliRouterAllocator struct {
	// pool 池条目列表
	pool []ModelPoolEntry
	// poolDigest 池结构摘要
	poolDigest string
	// deployments 部署目录快照（构造期深拷贝，CLI 目录查询共享）
	deployments []map[string]any
}

// CliModelAllocation 为外部 CLI 进程投影的真实部署。
// Python: CliModelAllocation (allocator.py:99-106)
type CliModelAllocation struct {
	// Model 真实部署模型名
	Model string
	// Provider 上游 provider 名
	Provider string
	// APIBase 部署 API base
	APIBase string
	// APIKey 部署 API Key
	APIKey string
	// RouteID 部署路由 ID（可选）
	RouteID string
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentTeam

// ──────────────────────────── 导出函数 ────────────────────────────

// ToTeamModelConfig 物化为 TeamModelConfig。
// Python: Allocation.to_team_model_config()
// RouteTag 非空时经 _with_route_tag 把该成员的客户端配置切到 tag-filtered；
// 共享池条目绝不修改（pool.py:73-88）。
func (a Allocation) ToTeamModelConfig() TeamModelConfig {
	config := a.Entry.ToTeamModelConfig()
	if a.RouteTag != "" {
		config = withRouteTag(config, a.RouteTag)
	}
	return config
}

// ToDBRef 产生轻量级的 {model_name, model_index} 引用用于 DB 持久化。
// Python: Allocation.to_db_ref()
// route_tag 仅在模型组成员钉到某模型时出现；它是选择条件，不是配置快照。
func (a Allocation) ToDBRef() map[string]any {
	name := a.Entry.ModelName
	if a.PersistenceModelName != "" {
		name = a.PersistenceModelName
	}
	ref := map[string]any{
		"model_name":  name,
		"model_index": a.GroupIndex,
	}
	if a.RouteTag != "" {
		ref["route_tag"] = a.RouteTag
	}
	return ref
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
// Python: build_model_allocator(spec, team_spec)
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
				Msg("RouterAllocator construction failed")
			return nil
		}
		return alloc
	case "intelli_router":
		alloc, err := NewIntelliRouterAllocator(pool)
		if err != nil {
			logger.Info(logComponent).Str("team_name", teamName).
				Str("strategy", strategy).Err(err).
				Msg("IntelliRouterAllocator construction failed")
			return nil
		}
		return alloc
	default:
		logger.Info(logComponent).Str("team_name", teamName).
			Str("strategy", strategy).Int("pool_size", len(pool)).
			Msg("unknown model_pool_strategy, falling back to nil")
		return nil
	}
}

// NewIntelliRouterAllocator 从 IntelliRouter 形状的池初始化。
// 每个条目必须声明 api_provider=="intelli_router" 且在
// metadata.client.intelli_router.deployments 下携带非空 deployment 列表。
// Python: IntelliRouterAllocator.__init__ (allocator.py:662)
func NewIntelliRouterAllocator(pool []ModelPoolEntry) (*IntelliRouterAllocator, error) {
	if len(pool) == 0 {
		return nil, fmt.Errorf("IntelliRouterAllocator requires a non-empty pool")
	}
	a := &IntelliRouterAllocator{pool: append([]ModelPoolEntry(nil), pool...)}
	if len(a.pool) != 1 || a.pool[0].ModelName != INTELLI_ROUTER_UNIFIED_MODEL {
		return nil, fmt.Errorf("IntelliRouterAllocator requires exactly one logical '*' entry")
	}
	entry := a.pool[0]
	a.poolDigest = poolDigest(a.pool)
	if entry.APIProvider != INTELLI_ROUTER_PROVIDER {
		return nil, fmt.Errorf(
			"IntelliRouterAllocator requires its logical '*' entry to declare api_provider='%s'",
			INTELLI_ROUTER_PROVIDER)
	}
	var err error
	a.deployments, err = validateDeploymentCatalog(entry)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Allocate 返回逻辑条目，可钉到单个模型。
// 物理部署永不分配：钉选成员仍拿 "*" 条目，经 tag-filtered 路由到
// fallback_tag（大小写不敏感）等于 model_name 的部署。
// provider_filter 忽略——provider 过滤作用于物理 CLI 部署，不是逻辑条目。
// 不可选名返回 nil——显式点名的模型绝不回退整组。
// Python: IntelliRouterAllocator.allocate (allocator.py:712)
func (a *IntelliRouterAllocator) Allocate(modelName string) *Allocation {
	tag := strings.TrimSpace(modelName)
	if tag == "" || tag == INTELLI_ROUTER_UNIFIED_MODEL {
		return &Allocation{Entry: a.pool[0], GroupIndex: 0}
	}
	matched := matchRouteTag(tag, routerConfig(a.pool[0]))
	if matched == "" {
		return nil
	}
	return &Allocation{Entry: a.pool[0], GroupIndex: 0, RouteTag: matched}
}

// ListRouteTags 成员可点名的模型（fallback_tag 值），已排序。
// Python: IntelliRouterAllocator.list_route_tags (allocator.py:726)
func (a *IntelliRouterAllocator) ListRouteTags() []string {
	return selectableRouteTags(routerConfig(a.pool[0]))
}

// StateDict 返回分配器计数器快照。
// Python: allocator.py:729-730
func (a *IntelliRouterAllocator) StateDict() map[string]any {
	return map[string]any{"pool_digest": a.poolDigest}
}

// LoadStateDict 无操作恢复；IntelliRouter 没有轮转计数器。
// Python: allocator.py:732-735
func (a *IntelliRouterAllocator) LoadStateDict(state map[string]any) {
	if state == nil {
		return
	}
	if v, ok := state["pool_digest"]; ok && v == a.poolDigest {
		return
	}
	// 池组成变化不需要计数器重置
}

// ListCliModels 列出供外部 CLI 投影的真实部署候选。
// Python: IntelliRouterAllocator.list_cli_models (allocator.py:765)
func (a *IntelliRouterAllocator) ListCliModels(providerFilter func(string) bool) []CliModelAllocation {
	catalog := make([]CliModelAllocation, 0, len(a.deployments))
	for _, item := range a.deployments {
		routeID := ""
		if v, ok := item["route_id"].(string); ok {
			routeID = v
		}
		catalog = append(catalog, CliModelAllocation{
			Model:    dictStr(item, "model_name"),
			Provider: dictStr(item, "provider"),
			APIBase:  dictStr(item, "api_base"),
			APIKey:   dictStr(item, "api_key"),
			RouteID:  routeID,
		})
	}
	if providerFilter == nil {
		return catalog
	}
	filtered := make([]CliModelAllocation, 0, len(catalog))
	for _, item := range catalog {
		if providerFilter(item.Provider) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// ResolveCliModels 按真实部署名解析，永不返回 Team 的 "*" 条目。
// Python: IntelliRouterAllocator.resolve_cli_models (allocator.py:777)
func (a *IntelliRouterAllocator) ResolveCliModels(modelName string, providerFilter func(string) bool) []CliModelAllocation {
	name := strings.TrimSpace(modelName)
	if name == "" || name == INTELLI_ROUTER_UNIFIED_MODEL {
		return nil
	}
	var result []CliModelAllocation
	for _, item := range a.ListCliModels(providerFilter) {
		if item.Model == name {
			result = append(result, item)
		}
	}
	return result
}

// ResolveMemberModelFromPool 从池中按引用解析成员模型。⤴️ 9.64 回填完成
// Python: resolve_member_model(team_spec, model_name, model_index, route_tag)
//
//	(allocator.py:798)
//
// 纯位置查找，不触碰分配器计数器。routeTag 为钉选模型组成员持久化的
// fallback_tag：当前组仍服务该 tag 时重放（大小写归一为可选拼写）；不再
// 服务时 warning 丢弃并回退组级路由——组在两次运行之间被编辑时，成员
// 降级为整组路由而不是永远拿不到部署。
func ResolveMemberModelFromPool(pool []ModelPoolEntry, modelName string, modelIndex int, routeTag string) *TeamModelConfig {
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
	entry := group[idx]
	if routeTag != "" {
		matched := matchRouteTag(routeTag, routerConfig(entry))
		if matched == "" {
			logger.Warn(logComponent).Str("route_tag", routeTag).
				Msg("persisted route tag is no longer served by the current model group; " +
					"falling back to group-wide routing")
		}
		routeTag = matched
	}
	cfg := withRouteTag(entry.ToTeamModelConfig(), routeTag)
	return &cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// routerConfig 逻辑条目内嵌的结构化 IntelliRouter 配置（可为空 dict）。
// Python: _router_config (allocator.py:220)
func routerConfig(entry ModelPoolEntry) map[string]any {
	client, _ := entry.Metadata["client"].(map[string]any)
	router, _ := client["intelli_router"].(map[string]any)
	if router == nil {
		return map[string]any{}
	}
	return router
}

// tagKey 模型名比较键：仅大小写不同的拼写视为同一模型。
// Python: _tag_key (allocator.py:226)
func tagKey(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// groupFallbackTag tag-filtered 模型组已被限制到的 tag（如有）。
// Python: _group_fallback_tag (allocator.py:230)
func groupFallbackTag(router map[string]any) string {
	if s, _ := router["strategy"].(string); s != "tag-filtered" {
		return ""
	}
	kwargs, _ := router["strategy_kwargs"].(map[string]any)
	tag, _ := kwargs["fallback_tag"].(string)
	return strings.TrimSpace(tag)
}

// selectableRouteTags 成员可点名的 tag：部署 fallback_tag 值。
// 仅大小写不同的拼写折叠为一条，写成组序里的首个拼写；钉到它的成员
// 会被路由到全部（见 withRouteTag）。tag-filtered 组只路由到自己的 tag，
// 把成员钉到其他 tag 会绕过组的限制。
// Python: _selectable_route_tags (allocator.py:239)
func selectableRouteTags(router map[string]any) []string {
	spellings := map[string]string{}
	deps, _ := router["deployments"].([]any)
	for _, item := range deps {
		d, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := d["fallback_tag"].(string)
		tag = strings.TrimSpace(tag)
		if tag != "" {
			key := tagKey(tag)
			if _, exists := spellings[key]; !exists {
				spellings[key] = tag
			}
		}
	}
	groupTag := groupFallbackTag(router)
	if groupTag != "" {
		filtered := map[string]string{}
		for key, tag := range spellings {
			if key == tagKey(groupTag) {
				filtered[key] = tag
			}
		}
		spellings = filtered
	}
	values := make([]string, 0, len(spellings))
	for _, tag := range spellings {
		values = append(values, tag)
	}
	sort.Strings(values)
	return values
}

// matchRouteTag tag 的可选拼写（大小写不敏感）；未服务返回空串。
// Python: _match_route_tag (allocator.py:262)
func matchRouteTag(tag string, router map[string]any) string {
	key := tagKey(tag)
	for _, item := range selectableRouteTags(router) {
		if tagKey(item) == key {
			return item
		}
	}
	return ""
}

// groupTagSpelling tag-filtered 组自身 tag 的部署拼写（如有）。
// router 是结构化 IntelliRouterConfig。组非 tag-filtered、无 tag、或没有
// 任何大小写携带该 tag 的部署时返回空串。
// Python: _group_tag_spelling (allocator.py:271)
func groupTagSpelling(router any) string {
	cfg, ok := router.(*llmschema.IntelliRouterConfig)
	if !ok || cfg == nil || cfg.Strategy != "tag-filtered" {
		return ""
	}
	groupTag := ""
	if cfg.StrategyKwargs != nil {
		groupTag, _ = cfg.StrategyKwargs["fallback_tag"].(string)
	}
	groupTag = strings.TrimSpace(groupTag)
	if groupTag == "" {
		return ""
	}
	key := tagKey(groupTag)
	for _, deployment := range cfg.Deployments {
		if deployment.FallbackTag != nil && tagKey(*deployment.FallbackTag) == key {
			return strings.TrimSpace(*deployment.FallbackTag)
		}
	}
	return ""
}

// withRouteTag 返回仅路由到 fallback_tag==tag 的部署的 config。
// 使用路由器既有的 tag-filtered 策略。strategy_kwargs 整体替换而非合并：
// 组自身策略的 kwargs 对 tag-filtered 无效。tag 与 tag 仅大小写不同的部署
// 在该成员的副本里重标为 tag，让 tag-filtered（精确匹配）路由到全部。
// 共享池条目绝不修改。无 tag 时：tag-filtered 组按组自身 tag 同样处理——
// 大小写不敏感匹配部署 tag，组序首个匹配拼写（selectableRouteTags 展示的
// 那个）替换之并重标部署。钉选成员与整组成员因此以同样方式比较 tag。
// 匹配不到部署的组 tag 保持原配置，路由器会报告无部署匹配。
// Python: _with_route_tag (allocator.py:291)
func withRouteTag(config TeamModelConfig, tag string) TeamModelConfig {
	router := config.ModelClientConfig.IntelliRouter
	if router == nil {
		return config
	}
	if tag == "" {
		tag = groupTagSpelling(router)
		if tag == "" {
			return config
		}
	}
	key := tagKey(tag)
	deployments := make([]llmschema.IntelliRouterDeploymentConfig, len(router.Deployments))
	for i, deployment := range router.Deployments {
		if deployment.FallbackTag != nil && *deployment.FallbackTag != tag && tagKey(*deployment.FallbackTag) == key {
			retagged := deployment
			t := tag
			retagged.FallbackTag = &t
			deployments[i] = retagged
		} else {
			deployments[i] = deployment
		}
	}
	kwargs := map[string]any{"fallback_tag": tag}
	routerCopy := *router
	routerCopy.Strategy = "tag-filtered"
	routerCopy.StrategyKwargs = kwargs
	routerCopy.Deployments = deployments
	client := config.ModelClientConfig
	client.IntelliRouter = &routerCopy
	config.ModelClientConfig = client
	return config
}

// validateDeploymentCatalog 分配器构造期拒绝畸形 deployment。
// 非法 Team 配置必须在构造时报错，而不是等到外部 CLI 成员 spawn 时才炸。
// 快照按约定不可变：spawn 请求解析期间条目 metadata 的后续变更分配器不感知。
// Python: IntelliRouterAllocator._validate_deployment_catalog (allocator.py:740)
func validateDeploymentCatalog(entry ModelPoolEntry) ([]map[string]any, error) {
	client, _ := entry.Metadata["client"].(map[string]any)
	router, _ := client["intelli_router"].(map[string]any)
	var raw []any
	if router != nil {
		raw, _ = router["deployments"].([]any)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("IntelliRouterAllocator requires a non-empty deployments list")
	}
	out := make([]map[string]any, 0, len(raw))
	for index, item := range raw {
		d, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("IntelliRouterAllocator deployment at index %d must be an object", index)
		}
		model := dictStr(d, "model_name")
		provider := dictStr(d, "provider")
		if strings.TrimSpace(model) == "" || strings.TrimSpace(provider) == "" {
			return nil, fmt.Errorf(
				"IntelliRouterAllocator deployment at index %d requires non-empty model_name and provider: %v",
				index, d)
		}
		out = append(out, d)
	}
	return out, nil
}

// dictStr 读取 map 中字符串键（nil 安全）。
func dictStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// poolDigest 计算池结构形状的稳定摘要。
// Python: _pool_digest(pool)
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
// Python: _group_index_of(entry, group)
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
