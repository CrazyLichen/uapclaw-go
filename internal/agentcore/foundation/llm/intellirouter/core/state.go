package core

import (
	"math"
	"sync"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// 最大历史记录数 ← core/state.py:27-29
const (
	// MaxLatencyHistory 延迟历史上限
	MaxLatencyHistory = 10
	// MaxTokensHistory token 历史上限
	MaxTokensHistory = 10
	// MaxRPMHistory RPM 历史上限（1 分钟）
	MaxRPMHistory = 60
)

// ──────────────────────────── 结构体 ────────────────────────────

// LatencyRecord 延迟记录。← core/state.py:32 LatencyRecord
type LatencyRecord struct {
	// Latency 请求延迟（秒）
	Latency float64
	// Tokens 本次请求 token 数
	Tokens int
	// Normalized 归一化延迟 latency/max(tokens,1)
	Normalized float64
	// Timestamp 记录时间戳
	Timestamp float64
}

// TokenUsage Token 使用记录。← core/state.py:39 TokenUsage
type TokenUsage struct {
	// Used 已用 token
	Used int
	// Limit 配额上限（0 = 未设限）
	Limit int
	// Timestamp 记录时间戳
	Timestamp float64
}

// Remaining 剩余 Token。← state.py:47 remaining property
func (u *TokenUsage) Remaining() int {
	remaining := u.Limit - u.Used
	if remaining < 0 {
		return 0
	}
	return remaining
}

// UtilizationRatio 使用率。← state.py:51 utilization_ratio property
func (u *TokenUsage) UtilizationRatio() float64 {
	if u.Limit == 0 {
		return 0.0
	}
	return float64(u.Used) / float64(u.Limit)
}

// RPMRecord RPM 记录。← core/state.py:56 RPMRecord
type RPMRecord struct {
	// Timestamp 请求时间戳
	Timestamp float64
}

// RPMTracker RPM 追踪器。← core/state.py:61 RPMTracker
type RPMTracker struct {
	// RPMLimit RPM 配额上限（0 = 未设限）
	RPMLimit int
	// Requests 窗口内请求记录
	Requests []RPMRecord
}

// nowFn 时间源（包级可注入，测试推时钟替代真实 sleep；生产默认 realNow）。
var nowFn = realNow

// ──────────────────────────── 导出函数 ────────────────────────────

// SetNowSource 注入时间源（仅测试使用；生产代码不得调用）。
func SetNowSource(fn func() float64) func() float64 {
	prev := nowFn
	nowFn = fn
	return prev
}

// AddRequest 添加请求记录。← state.py:65 add_request
func (t *RPMTracker) AddRequest() {
	t.Requests = append(t.Requests, RPMRecord{Timestamp: nowFn()})
	// 清理过期记录（保留最近 1 分钟）← py:68-69
	now := nowFn()
	kept := t.Requests[:0]
	for _, r := range t.Requests {
		if now-r.Timestamp < 60 {
			kept = append(kept, r)
		}
	}
	t.Requests = kept
}

// CurrentRPM 当前 RPM（最近 1 分钟的请求数）。← state.py:71 current_rpm property
func (t *RPMTracker) CurrentRPM() int {
	now := nowFn()
	count := 0
	for _, r := range t.Requests {
		if now-r.Timestamp < 60 {
			count++
		}
	}
	return count
}

// Remaining 剩余 RPM 配额。← state.py:75 remaining property
func (t *RPMTracker) Remaining() int {
	remaining := t.RPMLimit - t.CurrentRPM()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// UtilizationRatio RPM 使用率。← state.py:79 utilization_ratio property
func (t *RPMTracker) UtilizationRatio() float64 {
	if t.RPMLimit == 0 {
		return 0.0
	}
	return float64(t.CurrentRPM()) / float64(t.RPMLimit)
}

// LocalRouterState 本地路由状态 - 纯内存。← core/state.py:89 LocalRouterState
//
// 并发对齐：Python 用 threading.RLock（可重入，GetAvailableDeployments 持锁内
// 调 reset_deployment）。Go sync.Mutex 不可重入，因此拆分 resetDeploymentLocked
// 内部变体，公共入口持锁、内部变体在已持锁路径复用——语义等价。
// 23.4b 错误翻译层契约：DeploymentStatuses/ConsecutiveFailures/CooldownUntil
// 三个 map 必须导出（对应 py deployment_status/consecutive_failures/cooldown_until）。
type LocalRouterState struct {
	// DeploymentStatuses 部署状态 ← py:105
	DeploymentStatuses map[string]DeploymentStatus
	// ConsecutiveFailures 连续失败次数 ← py:106
	ConsecutiveFailures map[string]int
	// CooldownUntil 冷却时间戳 ← py:107（nil = 未在冷却）
	CooldownUntil map[string]*float64

	// Latencies 延迟历史 ← py:110
	Latencies map[string][]LatencyRecord

	// TotalTokens 总 token 数 ← py:113
	TotalTokens map[string]int
	// TotalRequests 总请求数 ← py:114
	TotalRequests map[string]int
	// HealthStates 健康状态 ← py:115
	HealthStates map[string]bool

	// TokenUsage Token 使用记录 ← py:118
	TokenUsage map[string]*TokenUsage
	// RPMTrackers RPM 追踪器 ← py:119
	RPMTrackers map[string]*RPMTracker

	// SessionDeploymentMap session 亲和映射（session_id → deployment_id）← py:122
	SessionDeploymentMap map[string]string
	// SessionTimestamps session 最近使用时间 ← py:123
	SessionTimestamps map[string]float64

	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLocalRouterState 构造本地路由状态。← py:89-124 + __post_init__(:126 RLock)
func NewLocalRouterState() *LocalRouterState {
	return &LocalRouterState{
		DeploymentStatuses:   map[string]DeploymentStatus{},
		ConsecutiveFailures:  map[string]int{},
		CooldownUntil:        map[string]*float64{},
		Latencies:            map[string][]LatencyRecord{},
		TotalTokens:          map[string]int{},
		TotalRequests:        map[string]int{},
		HealthStates:         map[string]bool{},
		TokenUsage:           map[string]*TokenUsage{},
		RPMTrackers:          map[string]*RPMTracker{},
		SessionDeploymentMap: map[string]string{},
		SessionTimestamps:    map[string]float64{},
	}
}

// ResetDeployment 原子性重置部署状态。← state.py:131 reset_deployment
//
// COOLDOWN 到期后的软恢复入口：状态与健康标记一并恢复为可用，让策略重新
// 获得调度机会；真实请求若仍失败会再次触发 on_failure 进入 COOLDOWN
// （consecutive_failures 保留，退避时长递增）。
func (s *LocalRouterState) ResetDeployment(deploymentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetDeploymentLocked(deploymentID)
}

// UpdateHealth 原子性更新健康状态。← state.py:146 update_health
//
// 健康检查通过时加速恢复为 HEALTHY。不健康时不降级——让真实的请求失败
// on_failure 去触发 COOLDOWN。
func (s *LocalRouterState) UpdateHealth(deploymentID string, isHealthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HealthStates[deploymentID] = isHealthy
	if isHealthy {
		s.DeploymentStatuses[deploymentID] = DeploymentStatusHealthy
		s.CooldownUntil[deploymentID] = nil
	}
}

// OnSuccess 成功回调 - 更新状态。← state.py:158 on_success
func (s *LocalRouterState) OnSuccess(deploymentID string, latency float64, tokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 重置失败计数 ← py:167
	s.ConsecutiveFailures[deploymentID] = 0

	// 恢复健康状态 ← py:170-171
	s.DeploymentStatuses[deploymentID] = DeploymentStatusHealthy
	s.CooldownUntil[deploymentID] = nil

	// 记录延迟 ← py:174-181
	record := LatencyRecord{
		Latency:    latency,
		Tokens:     tokens,
		Normalized: latency / float64(max(tokens, 1)),
		Timestamp:  nowFn(),
	}
	s.Latencies[deploymentID] = append(s.Latencies[deploymentID], record)
	// 保留最近 10 条 ← py:183-184
	if len(s.Latencies[deploymentID]) > MaxLatencyHistory {
		s.Latencies[deploymentID] = s.Latencies[deploymentID][1:]
	}

	// 更新使用量 ← py:187-188
	s.TotalTokens[deploymentID] = s.TotalTokens[deploymentID] + tokens
	s.TotalRequests[deploymentID] = s.TotalRequests[deploymentID] + 1
	// 更新健康状态 ← py:190
	s.HealthStates[deploymentID] = true

	// 更新 Token 使用 / RPM。不为未注册配额的部署自动创建默认条目
	// （limit 恒 0 会让 remaining 从 inf 塌缩为 0，破坏「未配置即不设限」
	// 语义）。配额条目只应由 router 的 quota wiring（_sync_quota_state）
	// 在部署注册时创建。← py:192-200
	if usage, ok := s.TokenUsage[deploymentID]; ok {
		usage.Used += tokens
	}
	if tracker, ok := s.RPMTrackers[deploymentID]; ok {
		tracker.AddRequest()
	}
}

// OnFailure 失败回调 - 统一走 COOLDOWN + backoff。← state.py:204 on_failure
//
// cooldown 时长按连续失败次数递增: 第 1 次 60s，第 2 次 120s，第 3 次 180s…
func (s *LocalRouterState) OnFailure(deploymentID string, err error, cooldownTime float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onFailureLocked(deploymentID, cooldownTime)
}

// GetAverageLatency 获取平均归一化延迟（latency/tokens，供策略打分使用）。
// ← state.py:223 get_average_latency
//
// 无延迟记录时返回 +Inf（未预热部署）。tokens<=0 的记录不参与归一化统计；
// 若不存在任何 tokens>0 的样本，同样返回 +Inf。
func (s *LocalRouterState) GetAverageLatency(deploymentID string) float64 {
	records := s.Latencies[deploymentID]
	var filtered []LatencyRecord
	for _, r := range records {
		if r.Tokens > 0 {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		return math.Inf(1) // ← py:235 float('inf')
	}
	sum := 0.0
	for _, r := range filtered {
		sum += r.Normalized
	}
	return sum / float64(len(filtered))
}

// GetAverageLatencyRaw 获取平均真实延迟（秒）；无记录返回 nil。
// ← state.py:243 get_average_latency_raw（面向用户统计口径，与归一化口径区分）
func (s *LocalRouterState) GetAverageLatencyRaw(deploymentID string) *float64 {
	records := s.Latencies[deploymentID]
	if len(records) == 0 {
		return nil
	}
	sum := 0.0
	for _, r := range records {
		sum += r.Latency
	}
	avg := sum / float64(len(records))
	return &avg
}

// GetAvailableDeployments 获取当前可用的部署 ID 列表。← state.py:254
func (s *LocalRouterState) GetAvailableDeployments(now float64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var available []string
	for depID, status := range s.DeploymentStatuses {
		if status == DeploymentStatusHealthy {
			available = append(available, depID)
		} else if status == DeploymentStatusCooldown {
			// ← py:262-264 冷却到期则软恢复并计入可用
			until := s.cooldownUntilOf(depID)
			if until <= now {
				s.resetDeploymentLocked(depID)
				available = append(available, depID)
			}
		}
	}
	return available
}

// GetTokenRemaining 获取剩余 Token 配额；未配置配额返回 +Inf（不设限）。
// ← state.py:267 get_token_remaining
func (s *LocalRouterState) GetTokenRemaining(deploymentID string) float64 {
	usage, ok := s.TokenUsage[deploymentID]
	if ok {
		return float64(usage.Remaining())
	}
	return math.Inf(1)
}

// RemoveDeployment 删除单个部署的全部运行时状态（热替换中该部署被移除时
// 调用）。与 CleanupDeployments 不同，精确作用于单个部署，不影响共享同一
// state 的其他 router 管理的部署。返回是否实际删除了状态。← state.py:277
func (s *LocalRouterState) RemoveDeployment(deploymentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, inStatus := s.DeploymentStatuses[deploymentID]
	_, inLatencies := s.Latencies[deploymentID]
	existed := inStatus || inLatencies
	s.removeFromRegistries(deploymentID)
	for sid, depID := range s.SessionDeploymentMap {
		if depID == deploymentID {
			delete(s.SessionDeploymentMap, sid)
			delete(s.SessionTimestamps, sid)
		}
	}
	return existed
}

// CleanupDeployments 清理不在 keepIDs 中的部署残留状态。← state.py:309
//
// 按 keepIDs 全集反向清理。若多个 router 共享同一 state 且各管不同部署，
// 会误删其他 router 管理的部署状态——该场景请使用 RemoveDeployment。
// 返回被清理的 deployment_id 列表（去重保序，py:342 dict.fromkeys）。
func (s *LocalRouterState) CleanupDeployments(keepIDs []string) []string {
	keep := make(map[string]bool, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = true
	}
	var removed []string
	s.mu.Lock()
	// ← py:318-330 for registry in (...)：逐注册表收集并反向清理
	for id := range s.DeploymentStatuses {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.ConsecutiveFailures {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.CooldownUntil {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.Latencies {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.TotalTokens {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.TotalRequests {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.HealthStates {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.TokenUsage {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	for id := range s.RPMTrackers {
		if !keep[id] {
			removed = append(removed, id)
		}
	}
	// 删除全部注册表中不在 keep 的条目 ← py:329-330
	s.removeFromRegistriesExcept(keep)
	// session 映射指向已移除部署的也一并清理 ← py:338-341
	for sid, depID := range s.SessionDeploymentMap {
		if !keep[depID] {
			delete(s.SessionDeploymentMap, sid)
			delete(s.SessionTimestamps, sid)
		}
	}
	s.mu.Unlock()
	// 去重保序 ← py:342
	seen := map[string]bool{}
	var result []string
	for _, id := range removed {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

// GetRPMRemaining 获取剩余 RPM 配额；未配置配额返回 +Inf。← state.py:346
func (s *LocalRouterState) GetRPMRemaining(deploymentID string) float64 {
	tracker, ok := s.RPMTrackers[deploymentID]
	if ok {
		return float64(tracker.Remaining())
	}
	return math.Inf(1)
}

// GetTokenUtilization 获取 Token 使用率。← state.py:356
func (s *LocalRouterState) GetTokenUtilization(deploymentID string) float64 {
	usage, ok := s.TokenUsage[deploymentID]
	if ok {
		return usage.UtilizationRatio()
	}
	return 0.0
}

// GetRPMUtilization 获取 RPM 使用率。← state.py:363
func (s *LocalRouterState) GetRPMUtilization(deploymentID string) float64 {
	tracker, ok := s.RPMTrackers[deploymentID]
	if ok {
		return tracker.UtilizationRatio()
	}
	return 0.0
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resetDeploymentLocked ResetDeployment 的已持锁变体。
// ← py:142-145（GetAvailableDeployments 持锁内复用，替代 Python RLock 重入）
func (s *LocalRouterState) resetDeploymentLocked(deploymentID string) {
	s.DeploymentStatuses[deploymentID] = DeploymentStatusHealthy
	s.CooldownUntil[deploymentID] = nil
	// health_state 必须同步恢复，否则 COOLDOWN 到期后 health_state 仍是
	// false，策略打分永远为 0，部署永远不会被再次调度，也就永远不会再
	// 进入 COOLDOWN。← py:138-141
	s.HealthStates[deploymentID] = true
}

// onFailureLocked OnFailure 的已持锁变体。← py:216-220
func (s *LocalRouterState) onFailureLocked(deploymentID string, cooldownTime float64) {
	s.ConsecutiveFailures[deploymentID] = s.ConsecutiveFailures[deploymentID] + 1
	failures := s.ConsecutiveFailures[deploymentID]
	s.DeploymentStatuses[deploymentID] = DeploymentStatusCooldown
	until := nowFn() + cooldownTime*float64(failures)
	s.CooldownUntil[deploymentID] = &until
	s.HealthStates[deploymentID] = false
}

// cooldownUntilOf 读冷却时间戳（已持锁路径使用；未设置返回 0，对齐 py:262 get(dep_id, 0)）。
func (s *LocalRouterState) cooldownUntilOf(deploymentID string) float64 {
	if until, ok := s.CooldownUntil[deploymentID]; ok && until != nil {
		return *until
	}
	return 0
}

// HealthStateOf 读健康状态；未登记默认 true。
// ← health/checker.py:163 state.health_state.get(id, True)
func (s *LocalRouterState) HealthStateOf(deploymentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.HealthStates[deploymentID]; ok {
		return v
	}
	return true
}

// DeploymentAvailable 按 RouterState 判定部署运行期是否可用。
// ← health/checker.py:231 _is_state_available（跨包访问 state 内部 map
// 必须经导出方法持锁读取——Python 同包直读 + with self.state.lock）
//
// 与 ReliableRouter._get_available_deployments 同口径：
// state.deployment_status 非 COOLDOWN 即可用；COOLDOWN 但 cooldown_until
// 已过期也视为可用（DeploymentStatus 枚举只有 HEALTHY/COOLDOWN，
// 不存在 UNHEALTHY 成员）。
//
// 这里只做判定、不做 reset_deployment 软恢复——软恢复是 router
// 调度路径的职责，查询方法不应有写副作用。（py:240-242 原文）
func (s *LocalRouterState) DeploymentAvailable(deploymentID string, now float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.DeploymentStatuses[deploymentID]
	if !ok {
		status = DeploymentStatusHealthy // ← py:236 get(id, HEALTHY)
	}
	if status == DeploymentStatusCooldown {
		until := s.cooldownUntilOf(deploymentID) // ← py:238 get(id, 0)
		if now < until {
			return false
		}
	}
	return true
}

// removeFromRegistriesExcept 删除除 keep 外的全部注册表条目。
func (s *LocalRouterState) removeFromRegistriesExcept(keep map[string]bool) {
	for id := range s.DeploymentStatuses {
		if !keep[id] {
			delete(s.DeploymentStatuses, id)
		}
	}
	for id := range s.ConsecutiveFailures {
		if !keep[id] {
			delete(s.ConsecutiveFailures, id)
		}
	}
	for id := range s.CooldownUntil {
		if !keep[id] {
			delete(s.CooldownUntil, id)
		}
	}
	for id := range s.Latencies {
		if !keep[id] {
			delete(s.Latencies, id)
		}
	}
	for id := range s.TotalTokens {
		if !keep[id] {
			delete(s.TotalTokens, id)
		}
	}
	for id := range s.TotalRequests {
		if !keep[id] {
			delete(s.TotalRequests, id)
		}
	}
	for id := range s.HealthStates {
		if !keep[id] {
			delete(s.HealthStates, id)
		}
	}
	for id := range s.TokenUsage {
		if !keep[id] {
			delete(s.TokenUsage, id)
		}
	}
	for id := range s.RPMTrackers {
		if !keep[id] {
			delete(s.RPMTrackers, id)
		}
	}
}

// removeFromRegistries 从九个注册表删除单个部署。← py:298-302
func (s *LocalRouterState) removeFromRegistries(deploymentID string) {
	delete(s.DeploymentStatuses, deploymentID)
	delete(s.ConsecutiveFailures, deploymentID)
	delete(s.CooldownUntil, deploymentID)
	delete(s.Latencies, deploymentID)
	delete(s.TotalTokens, deploymentID)
	delete(s.TotalRequests, deploymentID)
	delete(s.HealthStates, deploymentID)
	delete(s.TokenUsage, deploymentID)
	delete(s.RPMTrackers, deploymentID)
}

// realNow 生产时间源：秒精度，对齐 Python time.time()。
func realNow() float64 {
	return float64(time.Now().UnixMilli()) / 1000
}
