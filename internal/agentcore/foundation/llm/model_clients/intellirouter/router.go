package intellirouter

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouteStrategy 路由策略接口，从候选部署端点中挑出一个。
type RouteStrategy interface {
	// Select 从健康的部署端点中选择一个。
	// modelName 用于按模型名筛选匹配的端点。
	// ctx 用于传递路由上下文（如 session_id 用于亲和性路由）。
	Select(deployments []*Deployment, modelName string, ctx *RoutingContext) (*Deployment, error)
}

// RoutingContext 路由上下文，承载端到端请求中与路由决策相关的信息。
type RoutingContext struct {
	// SessionID 会话标识，用于 Session 亲和性路由。
	// 同一个 SessionID 优先路由到之前用过的 deployment（软亲和性）。
	SessionID string
	// Kwargs 额外参数（预留扩展）
	Kwargs map[string]any
}

// Deployment 单个模型部署端点：上半部分为配置项，下半部分为运行时健康状态。
type Deployment struct {
	// ID 部署端点唯一标识
	ID string
	// ModelName 模型名称
	ModelName string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基础 URL（OpenAI 兼容协议）
	APIBase string
	// TPM 每分钟 token 限制
	TPM int
	// RPM 每分钟请求限制
	RPM int
	// Tags 标签列表
	Tags []string
	// Timeout 请求超时时间（秒）
	Timeout float64
	// VerifySSL 是否验证 SSL 证书
	VerifySSL bool
	// 运行时状态（对应 Python LocalRouterState 中以 deployment_id 为 key 的条目）
	mu                  sync.RWMutex
	healthy             bool      // 是否健康，默认 true
	avgLatency          float64   // 平均延迟(ms)，默认 +inf 表示无数据
	totalCalls          int64     // 总调用次数
	failCount           int64     // 失败次数
	consecutiveFailures int64     // 连续失败次数
	cooldownUntil       time.Time // 冷却结束时间
}

type sessionEntry struct {
	deploymentID string
	lastUsed     time.Time
}

// ReliableRouter 可靠路由器：维护部署索引、按策略选点、失败重试与冷却。
type ReliableRouter struct {
	deployments    []*Deployment
	strategy       RouteStrategy
	numRetries     int
	timeout        float64
	cooldownTime   float64
	strategyKwargs map[string]any
	mu             sync.RWMutex
	// Model 索引：modelName → []*Deployment（对齐 Python BaseRouter._build_model_indices）
	modelIndices map[string][]*Deployment
	// Session 亲和性（对齐 Python LocalRouterState.session_deployment_map）
	sessionMu      sync.RWMutex
	sessionMap     map[string]sessionEntry // 会话ID → {部署ID, 最后使用时间}
	sessionCleanup time.Time               // 上次清理时间
	// 健康检查器（可选）
	healthChecker *HealthChecker
}

// HealthCheckResult 单个部署端点的一次健康探测结果。
type HealthCheckResult struct {
	DeploymentID string
	IsHealthy    bool
	Latency      float64 // 秒
	Error        string
	Timestamp    time.Time
}

// HealthChecker 后台健康检查器，按固定间隔探测部署端点并缓存最近结果。
type HealthChecker struct {
	deployments      []*Deployment
	checkInterval    time.Duration
	checkTimeout     time.Duration
	running          atomic.Bool
	cancel           context.CancelFunc
	lastCheckTime    time.Time
	lastCheckResults map[string]HealthCheckResult
	mu               sync.RWMutex
}

// SimpleShuffleStrategy 简单随机打乱策略，在候选端点中等概率抽取一个。
type SimpleShuffleStrategy struct{}

// RoundRobinStrategy 轮询策略，按调用次序依次选择端点。
type RoundRobinStrategy struct {
	counter uint64
}

// LowestLatencyStrategy 最低延迟策略，按平均延迟排序后取最优（保留部分探索）。
type LowestLatencyStrategy struct {
	// ExplorationRatio 探索比率（默认 0.1，即 10% 请求用于探索新端点）
	ExplorationRatio float64
}

// AdaptiveStrategy 自适应策略：按健康度、Token 用量、RPM 与延迟加权打分后选点。
type AdaptiveStrategy struct {
	// TokenThreshold token 用量阈值
	TokenThreshold float64
	// RPMThreshold RPM 阈值
	RPMThreshold float64
	// ExplorationRatio 探索比率（默认 0.1，即 10% 请求用于探索新端点）
	ExplorationRatio float64
	// WHealth 健康度权重
	WHealth float64
	// WToken token 用量权重
	WToken float64
	// WRPM RPM 权重
	WRPM float64
	// WLatency 延迟权重
	WLatency float64
	// Session 亲和性相关
	sessionTTL             float64 // Session 映射 TTL（秒），默认 1800（30分钟）
	sessionCleanupInterval float64 // Session 清理间隔（秒），默认 60
}

type bytesReaderImpl struct{ data []byte }

// ──────────────────────────── 常量 ────────────────────────────

const (
	defaultTokenThreshold     = 1000.0
	defaultRPMThreshold       = 10.0
	defaultExplorationRatio   = 0.1
	defaultWHealth            = 1.0
	defaultWToken             = 0.5
	defaultWRPM               = 0.3
	defaultWLatency           = 0.2
	defaultCooldownTime       = 60.0   // 冷却时间(秒)，对应 Python cooldown_time
	defaultSessionTTL         = 1800.0 // Session TTL(秒)，30分钟
	defaultSessionCleanup     = 60.0   // Session 清理间隔(秒)
	defaultHealthCheckTimeout = 5.0    // 健康检查超时(秒)
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var routerCache map[string]*ReliableRouter

var routerCacheLock sync.RWMutex

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeployment 从配置创建部署端点实例。
func NewDeployment(config DeploymentConfig) *Deployment {
	return &Deployment{
		ID:         config.ID,
		ModelName:  config.ModelName,
		APIKey:     config.APIKey,
		APIBase:    config.APIBase,
		TPM:        config.TPM,
		RPM:        config.RPM,
		Tags:       config.Tags,
		Timeout:    config.Timeout,
		VerifySSL:  config.VerifySSL,
		healthy:    true,        // 默认健康
		avgLatency: math.Inf(1), // 默认 +inf，表示无延迟数据
	}
}

// IsHealthy 判断该部署当前是否处于可用状态。
func (d *Deployment) IsHealthy() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.healthy
}

// GetAvgLatency 返回该部署的平均响应延迟（毫秒）。
func (d *Deployment) GetAvgLatency() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.avgLatency
}

// GetStats 返回该部署的调用量、错误数与平均延迟快照。
func (d *Deployment) GetStats() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var failRate float64
	if d.totalCalls > 0 {
		failRate = float64(d.failCount) / float64(d.totalCalls) * 100
	}

	// 延迟显示：+inf 表示无数据
	avgLatDisplay := d.avgLatency
	if math.IsInf(d.avgLatency, 1) {
		avgLatDisplay = -1 // -1 表示无延迟数据
	}

	return map[string]any{
		"id":                   d.ID,
		"model_name":           d.ModelName,
		"api_base":             d.APIBase,
		"healthy":              d.healthy,
		"avg_latency":          avgLatDisplay,
		"total_calls":          d.totalCalls,
		"fail_count":           d.failCount,
		"consecutive_failures": d.consecutiveFailures,
		"fail_rate":            failRate,
	}
}

// RecordSuccess 记录一次调用成功，顺带刷新平均延迟并清零连续失败计数。
func (d *Deployment) RecordSuccess(latency time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	latencyMs := float64(latency.Milliseconds())
	d.totalCalls++

	// 指数移动平均（EMA）更新延迟
	// 首次调用（avgLatency == +inf）直接赋值
	if math.IsInf(d.avgLatency, 1) {
		d.avgLatency = latencyMs
	} else {
		d.avgLatency = 0.7*d.avgLatency + 0.3*latencyMs
	}

	// 成功后重置连续失败计数
	d.consecutiveFailures = 0
	// 恢复健康状态
	d.healthy = true
}

// RecordFailure 记录一次调用失败，连续失败越多冷却时间越长。
func (d *Deployment) RecordFailure() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalCalls++
	d.failCount++
	d.consecutiveFailures++
	d.healthy = false
	d.cooldownUntil = time.Now().Add(
		time.Duration(defaultCooldownTime*float64(d.consecutiveFailures)) * time.Second,
	)
}

// IsCoolingDown 判断该部署是否还在失败后的冷却期内。
func (d *Deployment) IsCoolingDown() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return time.Now().Before(d.cooldownUntil)
}

// NewHealthChecker 创建健康检查器，定期探测各部署端点的可用性。
func NewHealthChecker(deployments []*Deployment, checkInterval, checkTimeout float64) *HealthChecker {
	if checkInterval <= 0 {
		checkInterval = 300.0 // 默认 5 分钟
	}
	if checkTimeout <= 0 {
		checkTimeout = defaultHealthCheckTimeout
	}
	return &HealthChecker{
		deployments:      deployments,
		checkInterval:    time.Duration(checkInterval * float64(time.Second)),
		checkTimeout:     time.Duration(checkTimeout * float64(time.Second)),
		lastCheckResults: make(map[string]HealthCheckResult),
	}
}

// Start 启动后台协程，按固定间隔探测各部署的健康状态。
func (h *HealthChecker) Start() {
	if h.running.Load() {
		return
	}
	h.running.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go h.backgroundLoop(ctx)
}

// Stop 停止后台健康探测。
func (h *HealthChecker) Stop() {
	if !h.running.Load() {
		return
	}
	h.running.Store(false)
	if h.cancel != nil {
		h.cancel()
	}
}

// CheckAll 逐个探测所有部署的健康状态，返回以部署名为键的结果表。
func (h *HealthChecker) CheckAll() map[string]HealthCheckResult {
	results := make(map[string]HealthCheckResult)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, dep := range h.deployments {
		wg.Add(1)
		go func(d *Deployment) {
			defer wg.Done()
			result := h.checkDeployment(d)
			mu.Lock()
			results[d.ID] = result
			// 健康检查通过时加速恢复为 HEALTHY
			if result.IsHealthy {
				d.mu.Lock()
				d.healthy = true
				d.consecutiveFailures = 0
				d.cooldownUntil = time.Time{}
				d.mu.Unlock()
			}
			mu.Unlock()
		}(dep)
	}
	wg.Wait()

	h.mu.Lock()
	h.lastCheckTime = time.Now()
	h.lastCheckResults = results
	h.mu.Unlock()

	return results
}

// GetLastResults 返回最近一轮健康探测的结果快照。
func (h *HealthChecker) GetLastResults() map[string]HealthCheckResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	// 返回副本
	return maps.Clone(h.lastCheckResults)
}

// NewReliableRouter 创建可靠路由器，包含部署列表、健康检查器和路由策略。
func NewReliableRouter(config *IntelliRouterClientConfig) *ReliableRouter {
	// 创建 Deployment 运行时实例
	deployments := make([]*Deployment, 0, len(config.Deployments))
	for _, depConfig := range config.Deployments {
		deployments = append(deployments, NewDeployment(depConfig))
	}

	// 创建路由策略
	strategy := createStrategy(config.Strategy, config.StrategyKwargs)

	router := &ReliableRouter{
		deployments:    deployments,
		strategy:       strategy,
		numRetries:     config.NumRetries,
		timeout:        config.Timeout,
		cooldownTime:   defaultCooldownTime,
		strategyKwargs: config.StrategyKwargs,
		sessionMap:     make(map[string]sessionEntry),
		modelIndices:   make(map[string][]*Deployment),
	}

	// 构建 model 索引
	router.buildModelIndices()

	// 启用健康检查
	if config.EnableHealthCheck {
		router.healthChecker = NewHealthChecker(
			deployments, config.HealthCheckInterval, defaultHealthCheckTimeout,
		)
		router.healthChecker.Start()
	}

	return router
}

// GetDeploymentsForModel 返回某个模型下配置的全部部署。
func (r *ReliableRouter) GetDeploymentsForModel(model string) []*Deployment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelIndices[model]
}

// GetModelList 返回当前可路由的模型名列表。
func (r *ReliableRouter) GetModelList() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]string, 0, len(r.modelIndices))
	for model := range r.modelIndices {
		list = append(list, model)
	}
	return list
}

// SelectDeployment 为一次调用挑选一个可用部署，按路由策略并跳过不健康或冷却中的部署。
func (r *ReliableRouter) SelectDeployment(modelName string) (*Deployment, error) {
	return r.SelectDeploymentWithContext(modelName, nil)
}

// SelectDeploymentWithContext 按当前策略为请求挑一个可用部署；候选全不可用时返回错误。
func (r *ReliableRouter) SelectDeploymentWithContext(modelName string, ctx *RoutingContext) (*Deployment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 从 model 索引中获取匹配端点
	candidates := r.modelIndices[modelName]

	// 如果没有匹配的，使用所有端点
	if len(candidates) == 0 {
		candidates = r.deployments
	}

	// 筛选可用端点（健康 + 冷却期已过）
	// Python: Deployment.is_available(now) + COOLDOWN 超时恢复
	var available []*Deployment
	for _, dep := range candidates {
		dep.mu.RLock()
		isHealthy := dep.healthy
		isCooling := time.Now().Before(dep.cooldownUntil)
		dep.mu.RUnlock()

		if isCooling {
			continue // 还在冷却期
		}
		if !isHealthy {
			// 冷却期已过但 healthy 仍为 false，自动恢复
			dep.mu.Lock()
			dep.healthy = true
			dep.consecutiveFailures = 0
			dep.mu.Unlock()
		}
		available = append(available, dep)
	}

	// 如果没有可用端点
	if len(available) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("IntelliRouter: 无可用部署端点 (model=%s, 所有端点均不健康或冷却中)", modelName)),
		)
	}

	// Session 亲和性检查（软亲和性）
	// Python: AdaptiveStrategy._get_session_affinity_deployment()
	if ctx != nil && ctx.SessionID != "" {
		affinityDep := r.getSessionAffinityDeployment(ctx.SessionID, available)
		if affinityDep != nil {
			return affinityDep, nil
		}
	}

	return r.strategy.Select(available, modelName, ctx)
}

// RecordSuccess 把调用成功的结果记到对应部署上。
func (r *ReliableRouter) RecordSuccess(dep *Deployment, latency time.Duration) {
	dep.RecordSuccess(latency)
}

// RecordSuccessWithSession 记录调用成功，并记住本次会话落在哪个部署上以便后续保持粘性。
func (r *ReliableRouter) RecordSuccessWithSession(dep *Deployment, latency time.Duration, sessionID string) {
	dep.RecordSuccess(latency)
	if sessionID != "" {
		r.updateSessionMapping(sessionID, dep.ID)
	}
}

// RecordFailure 把调用失败的结果记到对应部署上。
func (r *ReliableRouter) RecordFailure(dep *Deployment) {
	dep.RecordFailure()
}

// UpdateDeployments 用最新配置替换部署列表，并重建按模型查找的索引。
func (r *ReliableRouter) UpdateDeployments(newDeployments []*Deployment) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deployments = newDeployments
	r.buildModelIndices()

	if r.healthChecker != nil {
		r.healthChecker.mu.Lock()
		r.healthChecker.deployments = newDeployments
		r.healthChecker.mu.Unlock()
	}

	logger.Info(logComponent).
		Int("num_deployments", len(newDeployments)).
		Msg("ReliableRouter deployments updated.")
}

// GetStats 汇总路由器的运行统计（各部署指标、策略名、会话亲和表大小等）。
func (r *ReliableRouter) GetStats() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var depStats []map[string]any
	for _, dep := range r.deployments {
		depStats = append(depStats, dep.GetStats())
	}

	var modelList []string
	seen := make(map[string]bool)
	for _, dep := range r.deployments {
		if !seen[dep.ModelName] {
			modelList = append(modelList, dep.ModelName)
			seen[dep.ModelName] = true
		}
	}

	stats := map[string]any{
		"total_deployments": len(r.deployments),
		"model_list":        modelList,
		"deployments":       depStats,
	}

	// 包含 Session 亲和性映射信息
	r.sessionMu.RLock()
	stats["active_sessions"] = len(r.sessionMap)
	r.sessionMu.RUnlock()

	return stats
}

// GetHealthCheckResults 返回各部署最近一次健康探测的结果。
func (r *ReliableRouter) GetHealthCheckResults() map[string]HealthCheckResult {
	if r.healthChecker == nil {
		return nil
	}
	return r.healthChecker.GetLastResults()
}

// StopHealthChecker 停止健康探测协程，通常在客户端关闭时调用。
func (r *ReliableRouter) StopHealthChecker() {
	if r.healthChecker != nil {
		r.healthChecker.Stop()
	}
}

// Close 关闭 router 持有的后台资源（health checker goroutine）。
// 幂等；锁外 best-effort（对齐上游 `await entry.router.close()` 在锁外执行的语义）。
// 连接池是进程级共享 Transport，不属于单个 router——关闭它会波及在途的其他客户端，
// 故此处只回收本 router 独占的 goroutine。设计文档偏差 D-5 记录该差异。
// Python: intelli_router_model_client.py:691 await entry.router.close()
func (r *ReliableRouter) Close() {
	r.StopHealthChecker()
}

// GetOrCreateRouter 获取或创建全局缓存的可靠路由器（按 APIKey 索引）。
func GetOrCreateRouter(config *IntelliRouterClientConfig) *ReliableRouter {
	key := makeRouterKey(config)

	// 第一次检查（读锁，快速路径）
	routerCacheLock.RLock()
	if r, ok := routerCache[key]; ok {
		routerCacheLock.RUnlock()
		return r
	}
	routerCacheLock.RUnlock()

	// 加写锁
	routerCacheLock.Lock()
	defer routerCacheLock.Unlock()

	// 二次检查（加锁后再次确认）
	if r, ok := routerCache[key]; ok {
		return r
	}

	router := NewReliableRouter(config)
	routerCache[key] = router
	return router
}

// ResetRouterCache 显式驱逐并关闭缓存中的 router（配置换绑时调用）。
// 锁外 best-effort Close + 失败仅告警（对齐上游 L687-698 的驱逐段）。
// 上游按「事件循环变化」自动驱逐（_ensure_router_for_current_loop）；Go 无事件循环，
// net 池为进程级共享、goroutine 退出≠资源退役，该机制防护的失败模式不存在，
// 故改为显式触发——见设计文档偏差 D-5。
func ResetRouterCache(key string) {
	routerCacheLock.Lock()
	old, exists := routerCache[key]
	if exists {
		delete(routerCache, key)
	}
	routerCacheLock.Unlock()
	if !exists {
		return
	}
	old.Close()
}

// Select 随机洗牌后返回第一个部署，实现简单的负载打散。
func (s *SimpleShuffleStrategy) Select(deployments []*Deployment, _ string, _ *RoutingContext) (*Deployment, error) {
	if len(deployments) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("IntelliRouter: 无可用部署端点"),
		)
	}

	// Fisher-Yates 洗牌
	shuffled := make([]*Deployment, len(deployments))
	copy(shuffled, deployments)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled[0], nil
}

// Select 按轮询顺序挑下一个部署，保证各部署被均匀调用。
func (s *RoundRobinStrategy) Select(deployments []*Deployment, _ string, _ *RoutingContext) (*Deployment, error) {
	if len(deployments) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("IntelliRouter: 无可用部署端点"),
		)
	}

	idx := atomic.AddUint64(&s.counter, 1)
	return deployments[idx%uint64(len(deployments))], nil
}

// Select 选平均延迟最低的部署。
func (s *LowestLatencyStrategy) Select(deployments []*Deployment, _ string, _ *RoutingContext) (*Deployment, error) {
	if len(deployments) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("IntelliRouter: 无可用部署端点"),
		)
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// 探索：以 exploration_ratio 概率随机选择
	if r.Float64() < s.ExplorationRatio {
		return deployments[r.Intn(len(deployments))], nil
	}

	// 利用：选择延迟最低的端点
	// 未被调用过的端点 avgLatency=+inf，自然不会被选为最低
	var best *Deployment
	var bestLatency = math.Inf(1)

	for _, dep := range deployments {
		lat := dep.GetAvgLatency()
		if lat < bestLatency {
			best = dep
			bestLatency = lat
		}
	}

	// 如果所有端点都是 +inf（全未探索），随机选一个
	if best == nil || math.IsInf(bestLatency, 1) {
		return deployments[r.Intn(len(deployments))], nil
	}

	return best, nil
}

// NewAdaptiveStrategy 创建自适应路由策略，根据 kwargs 配置降级和权重参数。
func NewAdaptiveStrategy(kwargs map[string]any) *AdaptiveStrategy {
	s := &AdaptiveStrategy{
		TokenThreshold:         defaultTokenThreshold,
		RPMThreshold:           defaultRPMThreshold,
		ExplorationRatio:       defaultExplorationRatio,
		WHealth:                defaultWHealth,
		WToken:                 defaultWToken,
		WRPM:                   defaultWRPM,
		WLatency:               defaultWLatency,
		sessionTTL:             defaultSessionTTL,
		sessionCleanupInterval: defaultSessionCleanup,
	}

	// 从 kwargs 中提取参数
	if v, ok := getFloatFromAny(kwargs["token_threshold"]); ok {
		s.TokenThreshold = v
	}
	if v, ok := getFloatFromAny(kwargs["rpm_threshold"]); ok {
		s.RPMThreshold = v
	}
	if v, ok := getFloatFromAny(kwargs["exploration_ratio"]); ok {
		s.ExplorationRatio = v
	}
	if v, ok := getFloatFromAny(kwargs["w_health"]); ok {
		s.WHealth = v
	}
	if v, ok := getFloatFromAny(kwargs["w_token"]); ok {
		s.WToken = v
	}
	if v, ok := getFloatFromAny(kwargs["w_rpm"]); ok {
		s.WRPM = v
	}
	if v, ok := getFloatFromAny(kwargs["w_latency"]); ok {
		s.WLatency = v
	}

	return s
}

// Select 按综合评分选出最优部署（延迟、错误率、健康度加权）。
func (s *AdaptiveStrategy) Select(deployments []*Deployment, _ string, ctx *RoutingContext) (*Deployment, error) {
	if len(deployments) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("IntelliRouter: 无可用部署端点"),
		)
	}

	// 只有一个端点时直接返回
	if len(deployments) == 1 {
		return deployments[0], nil
	}

	// Session 亲和性检查（需要通过 ReliableRouter 传递）
	// 这里不直接访问 router，亲和性由 ReliableRouter.getSessionAffinityDeployment 处理
	// 策略层只负责评分和探索

	// 计算每个端点的评分
	type scored struct {
		dep   *Deployment
		score float64
	}
	scores := make([]scored, 0, len(deployments))

	for _, dep := range deployments {
		score := s.calculateScore(dep)
		scores = append(scores, scored{dep: dep, score: score})
	}

	// 按评分降序排序
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// 探索：以 exploration_ratio 概率随机选择
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	if r.Float64() < s.ExplorationRatio {
		idx := r.Intn(len(deployments))
		return deployments[idx], nil
	}

	// 利用：选择评分最高的
	return scores[0].dep, nil
}

// Read 实现 io.Reader 接口，按顺序读出自带的字节数据。
func (b *bytesReaderImpl) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// bytesReader 把一段字节包装成 io.Reader。
func bytesReader(b []byte) io.Reader { return (*bytesReaderImpl)(nil).withBytes(b) }

// withBytes 把字节数据挂到读取器上。
func (b *bytesReaderImpl) withBytes(data []byte) *bytesReaderImpl {
	return &bytesReaderImpl{data: data}
}

// checkDeployment 对单个部署做一次健康探测并记录结果。
func (h *HealthChecker) checkDeployment(dep *Deployment) HealthCheckResult {
	result := HealthCheckResult{
		DeploymentID: dep.ID,
		Timestamp:    time.Now(),
	}

	// 构造最小化请求
	reqBody := map[string]any{
		"model":      dep.ModelName,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// 构造 HTTP 请求
	url := dep.APIBase + "/chat/completions"
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost, url, bytesReader(bodyBytes),
	)
	if err != nil {
		result.IsHealthy = false
		result.Error = fmt.Sprintf("构造请求失败: %v", err)
		return result
	}
	req.Header.Set("Authorization", "Bearer "+dep.APIKey)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{Timeout: h.checkTimeout}
	if !dep.VerifySSL {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Seconds()

	if err != nil {
		result.IsHealthy = false
		result.Error = fmt.Sprintf("请求失败: %v", err)
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		result.IsHealthy = true
		result.Latency = latency
	} else {
		result.IsHealthy = false
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

// backgroundLoop 按固定间隔循环探测，直到收到停止信号。
func (h *HealthChecker) backgroundLoop(ctx context.Context) {
	// 首次立即检查一次
	h.CheckAll()

	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.CheckAll()
		}
	}
}

// buildModelIndices 按模型名重建部署索引。
func (r *ReliableRouter) buildModelIndices() {
	r.modelIndices = make(map[string][]*Deployment)
	for _, dep := range r.deployments {
		r.modelIndices[dep.ModelName] = append(r.modelIndices[dep.ModelName], dep)
	}
}

// getSessionAffinityDeployment 取该会话此前绑定的部署；绑定已不可用时返回 nil。
func (r *ReliableRouter) getSessionAffinityDeployment(sessionID string, available []*Deployment) *Deployment {
	if sessionID == "" {
		return nil
	}

	r.sessionMu.RLock()
	entry, ok := r.sessionMap[sessionID]
	r.sessionMu.RUnlock()

	if !ok {
		return nil
	}

	// 检查 TTL 是否过期
	if time.Since(entry.lastUsed).Seconds() > defaultSessionTTL {
		// 过期了，删除映射
		r.sessionMu.Lock()
		delete(r.sessionMap, sessionID)
		r.sessionMu.Unlock()
		return nil
	}

	// 查找对应的 deployment 是否在可用列表中
	for _, dep := range available {
		if dep.ID == entry.deploymentID {
			// 更新最近使用时间
			r.sessionMu.Lock()
			r.sessionMap[sessionID] = sessionEntry{
				deploymentID: entry.deploymentID,
				lastUsed:     time.Now(),
			}
			r.sessionMu.Unlock()
			return dep
		}
	}

	return nil
}

// updateSessionMapping 记录会话最近一次使用的部署，供下次保持粘性。
func (r *ReliableRouter) updateSessionMapping(sessionID, deploymentID string) {
	if sessionID == "" {
		return
	}

	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()

	r.sessionMap[sessionID] = sessionEntry{
		deploymentID: deploymentID,
		lastUsed:     time.Now(),
	}

	// 惰性清理过期 session
	if time.Since(r.sessionCleanup).Seconds() > defaultSessionCleanup {
		r.cleanupExpiredSessions()
		r.sessionCleanup = time.Now()
	}
}

// cleanupExpiredSessions 清掉过期的会话与部署的对应关系。
func (r *ReliableRouter) cleanupExpiredSessions() {
	now := time.Now()
	for sid, entry := range r.sessionMap {
		if now.Sub(entry.lastUsed).Seconds() > defaultSessionTTL {
			delete(r.sessionMap, sid)
		}
	}
}

// calculateScore 按延迟与错误率算出一个综合分数，分数越低越优先。
func (s *AdaptiveStrategy) calculateScore(dep *Deployment) float64 {
	dep.mu.RLock()
	defer dep.mu.RUnlock()

	// 健康度评分：健康=1.0，不健康=0.0
	healthScore := 0.0
	if dep.healthy {
		healthScore = 1.0
	}

	// Token 评分：剩余越多越好（归一化到 0-1）
	// Python: token_remaining / token_threshold
	tokenScore := 1.0
	if dep.TPM > 0 && s.TokenThreshold > 0 {
		remaining := float64(dep.TPM) - float64(dep.totalCalls)
		if remaining < 0 {
			remaining = 0
		}
		tokenScore = math.Min(1.0, remaining/s.TokenThreshold)
	}

	// RPM 评分：剩余越多越好
	// Python: rpm_remaining / rpm_threshold
	rpmScore := 1.0
	if dep.RPM > 0 && s.RPMThreshold > 0 {
		remaining := float64(dep.RPM) - float64(dep.totalCalls)
		if remaining < 0 {
			remaining = 0
		}
		rpmScore = math.Min(1.0, remaining/s.RPMThreshold)
	}

	// 延迟评分：延迟越低分数越高
	// Python: latency_score = max(0, 1 - avg_latency)
	// avgLatency=+inf（无数据）→ latencyScore=0.0
	latencyScore := 0.0
	if !math.IsInf(dep.avgLatency, 1) && dep.avgLatency >= 0 {
		// Python 用 1-avg_latency，但 avg_latency 单位是秒
		// Go 中 avgLatency 单位是毫秒，需要换算
		latencyScore = math.Max(0.0, 1.0-dep.avgLatency/1000.0)
	}

	// 加权求和
	return s.WHealth*healthScore + s.WToken*tokenScore + s.WRPM*rpmScore + s.WLatency*latencyScore
}

func init() {
	routerCache = make(map[string]*ReliableRouter)
}

// createStrategy 按配置里的策略名创建对应的路由策略。
func createStrategy(strategyName string, kwargs map[string]any) RouteStrategy {
	switch strategyName {
	case "round-robin":
		return &RoundRobinStrategy{}
	case "lowest-latency":
		explorationRatio := defaultExplorationRatio
		if v, ok := getFloatFromAny(kwargs["exploration_ratio"]); ok {
			explorationRatio = v
		}
		return &LowestLatencyStrategy{ExplorationRatio: explorationRatio}
	case "adaptive":
		return NewAdaptiveStrategy(kwargs)
	case "simple-shuffle":
		fallthrough
	default:
		return &SimpleShuffleStrategy{}
	}
}

// makeRouterKey 按配置生成路由实例的唯一键，便于相同配置复用同一实例。
func makeRouterKey(config *IntelliRouterClientConfig) string {
	// 序列化 deployments
	depIDs := make([]string, 0, len(config.Deployments))
	for _, dep := range config.Deployments {
		depIDs = append(depIDs, dep.ID+":"+dep.ModelName+":"+dep.APIBase)
	}
	deploymentsJSON, _ := json.Marshal(depIDs)

	// 序列化 strategy_kwargs
	kwargsJSON, _ := json.Marshal(config.StrategyKwargs)

	// 拼接原始字符串
	raw := fmt.Sprintf("%s|%s|%s|%d|%f|%v|%f|%v",
		string(deploymentsJSON),
		config.Strategy,
		string(kwargsJSON),
		config.NumRetries,
		config.Timeout,
		config.EnableHealthCheck,
		config.HealthCheckInterval,
		config.VerifySSL,
	)

	return fmt.Sprintf("%x", md5.Sum([]byte(raw)))
}

// getFloatFromAny 把任意数值转成 float64，无法转换时第二个返回值为 false。
func getFloatFromAny(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
