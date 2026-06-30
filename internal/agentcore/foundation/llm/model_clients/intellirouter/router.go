package intellirouter

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
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

// ──────────────────────────── 接口 ────────────────────────────

// RouteStrategy 路由策略接口，定义如何从多个部署端点中选择一个。
type RouteStrategy interface {
	// Select 从健康的部署端点中选择一个。
	// modelName 用于按模型名筛选匹配的端点。
	// ctx 用于传递路由上下文（如 session_id 用于亲和性路由）。
	Select(deployments []*Deployment, modelName string, ctx *RoutingContext) (*Deployment, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// RoutingContext 路由上下文，携带请求级别的路由信息。
//
// 对应 Python: intelli_router.core.context.RoutingContext
type RoutingContext struct {
	// SessionID 会话标识，用于 Session 亲和性路由。
	// 同一个 SessionID 优先路由到之前用过的 deployment（软亲和性）。
	SessionID string
	// Kwargs 额外参数（预留扩展）
	Kwargs map[string]any
}

// Deployment 单个部署端点的运行时模型。
//
// 对应 Python: intelli_router.Deployment + intelli_router.LocalRouterState
// 包含配置信息和运行时状态（健康度、延迟、统计等）。
//
// 延迟冷启动：avgLatency 默认为 +inf（等价 Python float('inf')），
// 表示该端点从未被调用过、无延迟数据。首次 RecordSuccess 后更新为实际延迟。
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

// sessionEntry Session 亲和性映射条目。
type sessionEntry struct {
	deploymentID string
	lastUsed     time.Time
}

// ReliableRouter 智能路由器。
//
// 对应 Python: intelli_router.ReliableRouter
//
// 核心能力：
//   - 多部署端点管理（含 model 索引）
//   - 路由策略（simple-shuffle / round-robin / lowest-latency / adaptive）
//   - 自动重试（失败时切换到下一个 deployment）
//   - 状态管理（健康/cooldown/延迟/失败计数）
//   - Session 亲和性（同一个 session 优先路由到同一个 deployment）
//   - 健康检查（后台定期检查端点可用性，加速恢复不健康端点）
//   - 动态更新部署列表
//   - 缓存共享（相同配置共享同一个 router 实例）
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

// HealthCheckResult 健康检查结果。
//
// 对应 Python: intelli_router.health.checker.HealthCheckResult
type HealthCheckResult struct {
	DeploymentID string
	IsHealthy    bool
	Latency      float64 // 秒
	Error        string
	Timestamp    time.Time
}

// HealthChecker 健康检查器。
//
// 对应 Python: intelli_router.health.checker.SDKHealthChecker
// 后台定期检查端点可用性，加速恢复不健康的端点。
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

// SimpleShuffleStrategy 随机打乱策略。
//
// 对应 Python: strategy="simple-shuffle"
// 将健康的端点列表随机打乱，返回第一个。
type SimpleShuffleStrategy struct{}

// RoundRobinStrategy 轮询策略。
//
// 对应 Python: strategy="round-robin"
// 原子递增计数器，取模轮询健康端点。
type RoundRobinStrategy struct {
	counter uint64
}

// LowestLatencyStrategy 最低延迟优先策略。
//
// 对应 Python: strategy="lowest-latency" (intelli_router.strategy.lowest_latency)
//
// 选择历史平均延迟最低的健康端点。未被调用过的端点 avgLatency=+inf，
// 不会被选为"最低延迟"，但通过 exploration_ratio（默认 0.1）随机探索来积累延迟数据。
//
// 决策流程：
//  1. 以 exploration_ratio 概率随机选择一个端点（探索）
//  2. 否则选择 avgLatency 最低的端点（利用）
type LowestLatencyStrategy struct {
	// ExplorationRatio 探索比率（默认 0.1，即 10% 请求用于探索新端点）
	ExplorationRatio float64
}

// AdaptiveStrategy 自适应路由策略。
//
// 对应 Python: strategy="adaptive"
// 多因子加权评分 + Session 亲和性：
//
//	score = w_health * health_score + w_token * token_score + w_rpm * rpm_score + w_latency * latency_score
//
// (1-exploration_ratio) 选最高分 + exploration_ratio 随机探索
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

// ──────────────────────────── 常量 ────────────────────────────

// 默认策略参数
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

// ──────────────────────────── 全局变量 ────────────────────────────

// routerCache 路由器缓存，相同配置共享同一个 ReliableRouter 实例。
// 对应 Python: _router_cache
var routerCache map[string]*ReliableRouter

// routerCacheLock 路由器缓存锁。
var routerCacheLock sync.RWMutex

// logComponent intellirouter 包日志组件标识。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeployment 从 DeploymentConfig 创建 Deployment 运行时实例。
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

// IsHealthy 返回部署端点是否健康。
func (d *Deployment) IsHealthy() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.healthy
}

// GetAvgLatency 返回平均延迟（毫秒）。
//
// 未被调用过的端点返回 math.Inf(1)（+inf），等价 Python float('inf')。
func (d *Deployment) GetAvgLatency() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.avgLatency
}

// GetStats 返回端点统计信息。
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

// RecordSuccess 记录成功调用，更新延迟等指标。
//
// 对应 Python: LocalRouterState.on_success()
// 成功后：重置连续失败计数、恢复健康状态、更新 EMA 延迟。
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

// RecordFailure 记录失败调用，进入 cooldown + backoff 机制。
//
// 对应 Python: LocalRouterState.on_failure()
// 冷却时长按连续失败次数递增：第1次 60s，第2次 120s，第3次 180s ...
// 冷却期间该端点被视为不健康，冷却结束后自动恢复。
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

// IsCoolingDown 返回端点是否在冷却期。
//
// 冷却期结束后自动恢复为健康状态。
func (d *Deployment) IsCoolingDown() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return time.Now().Before(d.cooldownUntil)
}

// ──── HealthChecker ────

// NewHealthChecker 创建健康检查器。
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

// Start 启动后台健康检查。
func (h *HealthChecker) Start() {
	if h.running.Load() {
		return
	}
	h.running.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go h.backgroundLoop(ctx)
}

// Stop 停止后台健康检查。
func (h *HealthChecker) Stop() {
	if !h.running.Load() {
		return
	}
	h.running.Store(false)
	if h.cancel != nil {
		h.cancel()
	}
}

// CheckAll 并发检查所有部署端点。
//
// 对应 Python: SDKHealthChecker.check_all_deployments()
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

// GetLastResults 获取最近一次健康检查结果。
func (h *HealthChecker) GetLastResults() map[string]HealthCheckResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	// 返回副本
	result := make(map[string]HealthCheckResult, len(h.lastCheckResults))
	for k, v := range h.lastCheckResults {
		result[k] = v
	}
	return result
}

// checkDeployment 检查单个部署端点健康状态。
//
// 对应 Python: SDKHealthChecker.check_deployment()
// 发送一个最小化 completion 请求（ping 消息 + max_tokens=1）。
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

// backgroundLoop 后台健康检查循环。
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

// bytesReader 辅助函数，将 []byte 转为 io.Reader。
func bytesReader(b []byte) io.Reader { return (*bytesReaderImpl)(nil).withBytes(b) }

type bytesReaderImpl struct{ data []byte }

func (b *bytesReaderImpl) withBytes(data []byte) *bytesReaderImpl {
	return &bytesReaderImpl{data: data}
}

func (b *bytesReaderImpl) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

// ──── ReliableRouter ────

// NewReliableRouter 创建智能路由器。
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

// buildModelIndices 构建 model 索引（对齐 Python BaseRouter._build_model_indices）。
func (r *ReliableRouter) buildModelIndices() {
	r.modelIndices = make(map[string][]*Deployment)
	for _, dep := range r.deployments {
		r.modelIndices[dep.ModelName] = append(r.modelIndices[dep.ModelName], dep)
	}
}

// GetDeploymentsForModel 获取指定模型的所有部署。
//
// 对应 Python: BaseRouter.get_deployments_for_model()
func (r *ReliableRouter) GetDeploymentsForModel(model string) []*Deployment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelIndices[model]
}

// GetModelList 获取所有模型名列表。
//
// 对应 Python: BaseRouter.get_model_list()
func (r *ReliableRouter) GetModelList() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]string, 0, len(r.modelIndices))
	for model := range r.modelIndices {
		list = append(list, model)
	}
	return list
}

// SelectDeployment 根据策略选择一个部署端点。
//
// 仅从匹配 modelName 且可用（健康 + 未冷却）的端点中选择。
// 冷却期超时后自动恢复为健康状态（对齐 Python _get_available_deployments）。
//
// 对应 Python: ReliableRouter._get_available_deployments() + strategy.select_deployment()
func (r *ReliableRouter) SelectDeployment(modelName string) (*Deployment, error) {
	return r.SelectDeploymentWithContext(modelName, nil)
}

// SelectDeploymentWithContext 根据策略选择一个部署端点（带路由上下文）。
//
// 上下文中的 SessionID 用于 Session 亲和性路由。
// 决策流程（对齐 Python AdaptiveStrategy.select_deployment）：
//  1. Session 亲和性检查（软亲和性）—— 如命中则直接返回
//  2. 策略选择（探索 + 利用）
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
	// 对应 Python: Deployment.is_available(now) + COOLDOWN 超时恢复
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
	// 对应 Python: AdaptiveStrategy._get_session_affinity_deployment()
	if ctx != nil && ctx.SessionID != "" {
		affinityDep := r.getSessionAffinityDeployment(ctx.SessionID, available)
		if affinityDep != nil {
			return affinityDep, nil
		}
	}

	return r.strategy.Select(available, modelName, ctx)
}

// RecordSuccess 记录成功调用，并更新 Session 亲和性映射。
func (r *ReliableRouter) RecordSuccess(dep *Deployment, latency time.Duration) {
	dep.RecordSuccess(latency)
}

// RecordSuccessWithSession 记录成功调用，并更新 Session 亲和性映射。
//
// 对应 Python: AdaptiveStrategy._update_session_mapping()
func (r *ReliableRouter) RecordSuccessWithSession(dep *Deployment, latency time.Duration, sessionID string) {
	dep.RecordSuccess(latency)
	if sessionID != "" {
		r.updateSessionMapping(sessionID, dep.ID)
	}
}

// RecordFailure 记录失败调用。
func (r *ReliableRouter) RecordFailure(dep *Deployment) {
	dep.RecordFailure()
}

// UpdateDeployments 动态更新部署列表。
//
// 对应 Python: ReliableRouter.update_deployments()
// 重建 model 索引，更新健康检查器的部署列表。
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

// GetStats 获取路由器统计信息。
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

// GetHealthCheckResults 获取最近一次健康检查结果（如启用）。
func (r *ReliableRouter) GetHealthCheckResults() map[string]HealthCheckResult {
	if r.healthChecker == nil {
		return nil
	}
	return r.healthChecker.GetLastResults()
}

// StopHealthChecker 停止健康检查器。
func (r *ReliableRouter) StopHealthChecker() {
	if r.healthChecker != nil {
		r.healthChecker.Stop()
	}
}

// ──── Session 亲和性 ────

// getSessionAffinityDeployment 获取 session 对应的亲和性 deployment。
//
// 对应 Python: AdaptiveStrategy._get_session_affinity_deployment()
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

// updateSessionMapping 更新 session 到 deployment 的映射。
//
// 对应 Python: AdaptiveStrategy._update_session_mapping()
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

// cleanupExpiredSessions 清理过期的 session 映射。
//
// 对应 Python: AdaptiveStrategy._cleanup_expired_sessions()
func (r *ReliableRouter) cleanupExpiredSessions() {
	now := time.Now()
	for sid, entry := range r.sessionMap {
		if now.Sub(entry.lastUsed).Seconds() > defaultSessionTTL {
			delete(r.sessionMap, sid)
		}
	}
}

// GetOrCreateRouter 获取或创建路由器（缓存共享）。
//
// 对应 Python: IntelliRouterModelClient._get_or_create_router()
// 使用 double-check locking 确保线程安全。
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

	// double-check
	if r, ok := routerCache[key]; ok {
		return r
	}

	router := NewReliableRouter(config)
	routerCache[key] = router
	return router
}

// ──── SimpleShuffleStrategy ────

// Select 随机打乱后选第一个。
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

// ──── RoundRobinStrategy ────

// Select 轮询选择下一个。
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

// ──── LowestLatencyStrategy ────

// Select 选择延迟最低的端点。
//
// 对应 Python: LowestLatencyStrategy.select_deployment()
// 决策流程：
//  1. 以 exploration_ratio 概率随机选择一个端点（探索）
//  2. 否则选择 avgLatency 最低的端点（利用）
//
// 未被调用过的端点 avgLatency=+inf，不会被选为"最低延迟"，
// 但通过 exploration_ratio 随机探索来积累延迟数据。
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

// ──── AdaptiveStrategy ────

// NewAdaptiveStrategy 从 strategy_kwargs 创建自适应策略。
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

// Select 自适应选择：Session 亲和性 → 探索 → 评分选择。
//
// 对应 Python: AdaptiveStrategy.select_deployment()
// 决策流程：
//  1. Session 亲和性检查（软亲和性）
//  2. 以 exploration_ratio 概率随机选择（探索）
//  3. 计算加权评分，选最高分（利用）
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

// calculateScore 计算单个端点的自适应加权评分。
//
// 对应 Python: AdaptiveStrategy._calculate_score()
// 延迟冷启动：avgLatency=+inf（无数据）时 latencyScore=0.0，
// 等价 Python 中 avg_latency=float('inf') → latency_score=max(0, 1-inf)=0.0。
// 配合 exploration_ratio 随机探索来积累延迟数据。
func (s *AdaptiveStrategy) calculateScore(dep *Deployment) float64 {
	dep.mu.RLock()
	defer dep.mu.RUnlock()

	// 健康度评分：健康=1.0，不健康=0.0
	healthScore := 0.0
	if dep.healthy {
		healthScore = 1.0
	}

	// Token 评分：剩余越多越好（归一化到 0-1）
	// 对应 Python: token_remaining / token_threshold
	tokenScore := 1.0
	if dep.TPM > 0 && s.TokenThreshold > 0 {
		remaining := float64(dep.TPM) - float64(dep.totalCalls)
		if remaining < 0 {
			remaining = 0
		}
		tokenScore = math.Min(1.0, remaining/s.TokenThreshold)
	}

	// RPM 评分：剩余越多越好
	// 对应 Python: rpm_remaining / rpm_threshold
	rpmScore := 1.0
	if dep.RPM > 0 && s.RPMThreshold > 0 {
		remaining := float64(dep.RPM) - float64(dep.totalCalls)
		if remaining < 0 {
			remaining = 0
		}
		rpmScore = math.Min(1.0, remaining/s.RPMThreshold)
	}

	// 延迟评分：延迟越低分数越高
	// 对应 Python: latency_score = max(0, 1 - avg_latency)
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

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	routerCache = make(map[string]*ReliableRouter)
}

// createStrategy 根据策略名称创建路由策略实例。
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

// makeRouterKey 生成确定性缓存 key（MD5 哈希）。
//
// 对应 Python: IntelliRouterModelClient._make_router_key()
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

// getFloatFromAny 从 any 类型中提取 float64 值。
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
