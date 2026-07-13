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

// ──────────────────────────── 结构体 ────────────────────────────

type RouteStrategy interface {
	// Select 从健康的部署端点中选择一个。
	// modelName 用于按模型名筛选匹配的端点。
	// ctx 用于传递路由上下文（如 session_id 用于亲和性路由）。
	Select(deployments []*Deployment, modelName string, ctx *RoutingContext) (*Deployment, error)
}

type RoutingContext struct {
	// SessionID 会话标识，用于 Session 亲和性路由。
	// 同一个 SessionID 优先路由到之前用过的 deployment（软亲和性）。
	SessionID string
	// Kwargs 额外参数（预留扩展）
	Kwargs map[string]any
}

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

type HealthCheckResult struct {
	DeploymentID string
	IsHealthy    bool
	Latency      float64 // 秒
	Error        string
	Timestamp    time.Time
}

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

type SimpleShuffleStrategy struct{}

type RoundRobinStrategy struct {
	counter uint64
}

type LowestLatencyStrategy struct {
	// ExplorationRatio 探索比率（默认 0.1，即 10% 请求用于探索新端点）
	ExplorationRatio float64
}

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

func (d *Deployment) IsHealthy() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.healthy
}

func (d *Deployment) GetAvgLatency() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.avgLatency
}

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

func (d *Deployment) IsCoolingDown() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return time.Now().Before(d.cooldownUntil)
}

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

func (h *HealthChecker) Start() {
	if h.running.Load() {
		return
	}
	h.running.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go h.backgroundLoop(ctx)
}

func (h *HealthChecker) Stop() {
	if !h.running.Load() {
		return
	}
	h.running.Store(false)
	if h.cancel != nil {
		h.cancel()
	}
}

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

func (r *ReliableRouter) GetDeploymentsForModel(model string) []*Deployment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelIndices[model]
}

func (r *ReliableRouter) GetModelList() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]string, 0, len(r.modelIndices))
	for model := range r.modelIndices {
		list = append(list, model)
	}
	return list
}

func (r *ReliableRouter) SelectDeployment(modelName string) (*Deployment, error) {
	return r.SelectDeploymentWithContext(modelName, nil)
}

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

func (r *ReliableRouter) RecordSuccess(dep *Deployment, latency time.Duration) {
	dep.RecordSuccess(latency)
}

func (r *ReliableRouter) RecordSuccessWithSession(dep *Deployment, latency time.Duration, sessionID string) {
	dep.RecordSuccess(latency)
	if sessionID != "" {
		r.updateSessionMapping(sessionID, dep.ID)
	}
}

func (r *ReliableRouter) RecordFailure(dep *Deployment) {
	dep.RecordFailure()
}

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

func (r *ReliableRouter) GetHealthCheckResults() map[string]HealthCheckResult {
	if r.healthChecker == nil {
		return nil
	}
	return r.healthChecker.GetLastResults()
}

func (r *ReliableRouter) StopHealthChecker() {
	if r.healthChecker != nil {
		r.healthChecker.Stop()
	}
}

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

func (b *bytesReaderImpl) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func bytesReader(b []byte) io.Reader { return (*bytesReaderImpl)(nil).withBytes(b) }

func (b *bytesReaderImpl) withBytes(data []byte) *bytesReaderImpl {
	return &bytesReaderImpl{data: data}
}

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

func (r *ReliableRouter) buildModelIndices() {
	r.modelIndices = make(map[string][]*Deployment)
	for _, dep := range r.deployments {
		r.modelIndices[dep.ModelName] = append(r.modelIndices[dep.ModelName], dep)
	}
}

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

func (r *ReliableRouter) cleanupExpiredSessions() {
	now := time.Now()
	for sid, entry := range r.sessionMap {
		if now.Sub(entry.lastUsed).Seconds() > defaultSessionTTL {
			delete(r.sessionMap, sid)
		}
	}
}

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

func init() {
	routerCache = make(map[string]*ReliableRouter)
}

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
