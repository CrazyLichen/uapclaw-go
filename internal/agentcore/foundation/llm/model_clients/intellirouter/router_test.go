package intellirouter

import (
	"math"
	"testing"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// newTestDeployments 创建测试用部署端点列表。
func newTestDeployments() []*Deployment {
	return []*Deployment{
		NewDeployment(DeploymentConfig{
			ID: "dep1", ModelName: "test-model", APIKey: "key1", APIBase: "https://api1.test.com",
			TPM: 100000, RPM: 60, Timeout: 30.0,
		}),
		NewDeployment(DeploymentConfig{
			ID: "dep2", ModelName: "test-model", APIKey: "key2", APIBase: "https://api2.test.com",
			TPM: 100000, RPM: 60, Timeout: 30.0,
		}),
		NewDeployment(DeploymentConfig{
			ID: "dep3", ModelName: "other-model", APIKey: "key3", APIBase: "https://api3.test.com",
			TPM: 100000, RPM: 60, Timeout: 30.0,
		}),
	}
}

// newTestRouter 创建测试用路由器。
func newTestRouter(strategy string) *ReliableRouter {
	return &ReliableRouter{
		deployments:    newTestDeployments(),
		strategy:       createStrategy(strategy, nil),
		numRetries:     3,
		timeout:        30.0,
		sessionMap:     make(map[string]sessionEntry),
		modelIndices:   make(map[string][]*Deployment),
	}
}

func init() {
	// 为测试用路由器构建 model 索引
}

// newTestRouterWithIndices 创建带 model 索引的测试路由器。
func newTestRouterWithIndices(strategy string) *ReliableRouter {
	r := newTestRouter(strategy)
	r.buildModelIndices()
	return r
}

// ──── Deployment 测试 ────

// TestDeployment_IsHealthy_Default 测试默认健康状态。
func TestDeployment_IsHealthy_Default(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})
	if !dep.IsHealthy() {
		t.Error("新创建的 Deployment 应为健康状态")
	}
}

// TestDeployment_AvgLatency_DefaultInf 测试新端点 avgLatency 默认为 +inf。
func TestDeployment_AvgLatency_DefaultInf(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})
	lat := dep.GetAvgLatency()
	if !math.IsInf(lat, 1) {
		t.Errorf("新端点 avgLatency 应为 +inf，实际为 %f", lat)
	}
}

// TestDeployment_RecordSuccess 测试成功记录。
func TestDeployment_RecordSuccess(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})

	dep.RecordFailure() // 先标记为不健康
	if dep.IsHealthy() {
		t.Error("RecordFailure 后应为不健康")
	}

	dep.RecordSuccess(100 * time.Millisecond)
	if !dep.IsHealthy() {
		t.Error("RecordSuccess 后应恢复健康")
	}
	if dep.GetAvgLatency() != 100.0 {
		t.Errorf("avgLatency = %f, 期望 100.0", dep.GetAvgLatency())
	}
	// 连续失败计数应重置
	dep.mu.RLock()
	cf := dep.consecutiveFailures
	dep.mu.RUnlock()
	if cf != 0 {
		t.Errorf("RecordSuccess 后 consecutiveFailures = %d, 期望 0", cf)
	}
}

// TestDeployment_RecordFailure 测试失败记录（cooldown + backoff）。
func TestDeployment_RecordFailure(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})
	dep.RecordFailure()

	if dep.IsHealthy() {
		t.Error("RecordFailure 后应为不健康")
	}
	// 应处于冷却期
	if !dep.IsCoolingDown() {
		t.Error("RecordFailure 后应在冷却期")
	}
	// 连续失败计数
	dep.mu.RLock()
	cf := dep.consecutiveFailures
	dep.mu.RUnlock()
	if cf != 1 {
		t.Errorf("consecutiveFailures = %d, 期望 1", cf)
	}
}

// TestDeployment_CooldownBackoff 测试冷却时间按连续失败递增。
func TestDeployment_CooldownBackoff(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})

	// 第1次失败：冷却 60s
	dep.RecordFailure()
	dep.mu.RLock()
	cd1 := dep.cooldownUntil
	cf1 := dep.consecutiveFailures
	dep.mu.RUnlock()
	if cf1 != 1 {
		t.Errorf("第1次失败 consecutiveFailures = %d, 期望 1", cf1)
	}

	// 第2次失败：冷却 120s
	dep.RecordFailure()
	dep.mu.RLock()
	cd2 := dep.cooldownUntil
	cf2 := dep.consecutiveFailures
	dep.mu.RUnlock()
	if cf2 != 2 {
		t.Errorf("第2次失败 consecutiveFailures = %d, 期望 2", cf2)
	}
	// cd2 应该比 cd1 更晚
	if !cd2.After(cd1) {
		t.Error("第2次冷却时间应比第1次长")
	}
}

// TestDeployment_CooldownAutoRecover 测试冷却期结束后自动恢复。
func TestDeployment_CooldownAutoRecover(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})
	dep.RecordFailure()
	dep.RecordFailure()

	// 手动设置冷却时间为过去，模拟冷却期已过
	dep.mu.Lock()
	dep.cooldownUntil = time.Now().Add(-1 * time.Second)
	dep.mu.Unlock()

	// IsCoolingDown 应返回 false
	if dep.IsCoolingDown() {
		t.Error("冷却期已过，IsCoolingDown 应返回 false")
	}
}

// TestDeployment_EMALatency 测试延迟指数移动平均。
func TestDeployment_EMALatency(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test"})

	dep.RecordSuccess(100 * time.Millisecond) // 第1次：avgLatency = 100（从 +inf 直接赋值）
	if dep.GetAvgLatency() != 100.0 {
		t.Errorf("第1次 avgLatency = %f, 期望 100.0", dep.GetAvgLatency())
	}

	dep.RecordSuccess(200 * time.Millisecond) // 第2次：avgLatency = 0.7*100 + 0.3*200 = 130
	expected := 0.7*100.0 + 0.3*200.0
	if dep.GetAvgLatency() != expected {
		t.Errorf("第2次 avgLatency = %f, 期望 %f", dep.GetAvgLatency(), expected)
	}
}

// TestDeployment_GetStats 测试统计信息。
func TestDeployment_GetStats(t *testing.T) {
	dep := NewDeployment(DeploymentConfig{ID: "test", ModelName: "gpt-4", APIBase: "https://api.test.com"})

	// 新端点 avgLatency 应显示为 -1（无数据）
	stats := dep.GetStats()
	if stats["avg_latency"] != float64(-1) {
		t.Errorf("新端点 avg_latency = %v, 期望 -1 (表示无数据)", stats["avg_latency"])
	}

	dep.RecordSuccess(50 * time.Millisecond)
	dep.RecordFailure()

	stats = dep.GetStats()
	if stats["id"] != "test" {
		t.Errorf("id = %v, 期望 test", stats["id"])
	}
	if stats["total_calls"] != int64(2) {
		t.Errorf("total_calls = %v, 期望 2", stats["total_calls"])
	}
	if stats["fail_count"] != int64(1) {
		t.Errorf("fail_count = %v, 期望 1", stats["fail_count"])
	}
}

// ──── SimpleShuffleStrategy 测试 ────

// TestSimpleShuffleStrategy_Select 测试随机打乱选择。
func TestSimpleShuffleStrategy_Select(t *testing.T) {
	strategy := &SimpleShuffleStrategy{}
	deps := newTestDeployments()

	selected, err := strategy.Select(deps, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected == nil {
		t.Fatal("selected 不应为 nil")
	}
}

// TestSimpleShuffleStrategy_Empty 测试空端点列表。
func TestSimpleShuffleStrategy_Empty(t *testing.T) {
	strategy := &SimpleShuffleStrategy{}
	_, err := strategy.Select([]*Deployment{}, "test-model", nil)
	if err == nil {
		t.Error("空端点列表应报错")
	}
}

// ──── RoundRobinStrategy 测试 ────

// TestRoundRobinStrategy_Select 测试轮询选择。
func TestRoundRobinStrategy_Select(t *testing.T) {
	strategy := &RoundRobinStrategy{}
	deps := newTestDeployments()

	// 轮询应按顺序选择
	selected1, _ := strategy.Select(deps, "test-model", nil)
	selected2, _ := strategy.Select(deps, "test-model", nil)
	selected3, _ := strategy.Select(deps, "test-model", nil)

	// 验证选择了不同的端点（轮询）
	ids := []string{selected1.ID, selected2.ID, selected3.ID}
	// 至少应有两个不同的 ID
	uniqueIDs := make(map[string]bool)
	for _, id := range ids {
		uniqueIDs[id] = true
	}
	if len(uniqueIDs) < 2 {
		t.Errorf("轮询应选择不同端点，实际选择的 ID: %v", ids)
	}
}

// ──── LowestLatencyStrategy 测试 ────

// TestLowestLatencyStrategy_Select 测试最低延迟选择。
func TestLowestLatencyStrategy_Select(t *testing.T) {
	// 禁用探索以确保确定性选择
	strategy := &LowestLatencyStrategy{ExplorationRatio: 0.0}
	deps := newTestDeployments()

	// dep1 延迟低
	deps[0].RecordSuccess(50 * time.Millisecond)
	// dep2 延迟高
	deps[1].RecordSuccess(500 * time.Millisecond)

	// 选择健康端点中的最低延迟
	healthy := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(healthy, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected.ID != "dep1" {
		t.Errorf("应选择最低延迟的 dep1，实际选择了 %s", selected.ID)
	}
}

// TestLowestLatencyStrategy_ColdStart 测试冷启动：所有端点均无延迟数据。
func TestLowestLatencyStrategy_ColdStart(t *testing.T) {
	strategy := &LowestLatencyStrategy{ExplorationRatio: 0.0}
	deps := newTestDeployments()

	// 所有端点都未被调用（avgLatency == +inf），应随机选择一个
	healthy := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(healthy, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected == nil {
		t.Fatal("冷启动时应选择一个端点")
	}
	if selected.ID != "dep1" && selected.ID != "dep2" {
		t.Errorf("冷启动时应从端点中选择，实际选择了 %s", selected.ID)
	}
}

// TestLowestLatencyStrategy_Exploration 测试探索比率。
func TestLowestLatencyStrategy_Exploration(t *testing.T) {
	strategy := &LowestLatencyStrategy{ExplorationRatio: 1.0}
	deps := newTestDeployments()

	// dep1 延迟最低，但 exploration_ratio=1.0 会随机选择
	deps[0].RecordSuccess(50 * time.Millisecond)
	deps[1].RecordSuccess(500 * time.Millisecond)

	healthy := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(healthy, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected == nil {
		t.Fatal("探索时应选择一个端点")
	}
}

// TestLowestLatencyStrategy_MixedColdStart 测试部分端点有延迟数据时选择已探索端点。
func TestLowestLatencyStrategy_MixedColdStart(t *testing.T) {
	strategy := &LowestLatencyStrategy{ExplorationRatio: 0.0}
	deps := newTestDeployments()

	// dep1 有延迟数据（已探索），dep2 没有（avgLatency=+inf）
	deps[0].RecordSuccess(50 * time.Millisecond)

	healthy := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(healthy, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	// dep1 延迟 50ms < dep2 延迟 +inf，所以应选 dep1
	if selected.ID != "dep1" {
		t.Errorf("有延迟数据的端点延迟最低时应被选择，期望 dep1，实际选择了 %s", selected.ID)
	}
}

// ──── AdaptiveStrategy 测试 ────

// TestAdaptiveStrategy_Select 测试自适应选择。
func TestAdaptiveStrategy_Select(t *testing.T) {
	strategy := NewAdaptiveStrategy(map[string]any{
		"exploration_ratio": 0.0,
		"w_health":          0.0,
		"w_token":           0.0,
		"w_rpm":             0.0,
		"w_latency":         1.0,
	})
	deps := newTestDeployments()

	// dep1 延迟低 → latencyScore = max(0, 1-50/1000) = 0.95 → 评分高
	deps[0].RecordSuccess(50 * time.Millisecond)
	// dep2 延迟高 → latencyScore = max(0, 1-500/1000) = 0.5 → 评分低
	deps[1].RecordSuccess(500 * time.Millisecond)

	healthy := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(healthy, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected.ID != "dep1" {
		t.Errorf("应选择低延迟的 dep1，实际选择了 %s", selected.ID)
	}
}

// TestAdaptiveStrategy_ColdStartLatency 测试冷启动：未探索端点 latencyScore=0.0。
func TestAdaptiveStrategy_ColdStartLatency(t *testing.T) {
	strategy := NewAdaptiveStrategy(map[string]any{
		"exploration_ratio": 0.0,
		"w_health":          0.0,
		"w_token":           0.0,
		"w_rpm":             0.0,
		"w_latency":         1.0,
	})
	deps := newTestDeployments()

	// dep1 有延迟数据（低延迟 50ms → latencyScore ≈ 0.95）
	deps[0].RecordSuccess(50 * time.Millisecond)
	// dep2 未被调用（avgLatency=+inf → latencyScore = 0.0）

	healthy := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(healthy, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected.ID != "dep1" {
		t.Errorf("已探索端点延迟低时应优先选择，期望 dep1，实际选择了 %s", selected.ID)
	}
}

// TestAdaptiveStrategy_UnhealthyPenalty 测试不健康端点的惩罚。
func TestAdaptiveStrategy_UnhealthyPenalty(t *testing.T) {
	strategy := NewAdaptiveStrategy(map[string]any{
		"exploration_ratio": 0.0,
		"w_health":          10.0,
		"w_token":           0.0,
		"w_rpm":             0.0,
		"w_latency":         0.0,
	})
	deps := newTestDeployments()

	deps[0].RecordFailure()
	deps[1].RecordSuccess(500 * time.Millisecond)

	allDeps := []*Deployment{deps[0], deps[1]}
	selected, err := strategy.Select(allDeps, "test-model", nil)
	if err != nil {
		t.Fatalf("Select 报错: %v", err)
	}
	if selected.ID != "dep2" {
		t.Errorf("应选择健康的 dep2，实际选择了 %s", selected.ID)
	}
}

// TestAdaptiveStrategy_DefaultKwargs 测试默认参数。
func TestAdaptiveStrategy_DefaultKwargs(t *testing.T) {
	strategy := NewAdaptiveStrategy(nil)
	if strategy.TokenThreshold != defaultTokenThreshold {
		t.Errorf("TokenThreshold = %f, 期望 %f", strategy.TokenThreshold, defaultTokenThreshold)
	}
	if strategy.ExplorationRatio != defaultExplorationRatio {
		t.Errorf("ExplorationRatio = %f, 期望 %f", strategy.ExplorationRatio, defaultExplorationRatio)
	}
}

// ──── ReliableRouter 测试 ────

// TestReliableRouter_SelectDeployment 测试路由器选择端点。
func TestReliableRouter_SelectDeployment(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	dep, err := router.SelectDeployment("test-model")
	if err != nil {
		t.Fatalf("SelectDeployment 报错: %v", err)
	}
	if dep.ModelName != "test-model" {
		t.Errorf("应选择 test-model 端点，实际选择了 %s", dep.ModelName)
	}
}

// TestReliableRouter_SelectDeployment_FallbackAll 测试无匹配模型时降级到所有端点。
func TestReliableRouter_SelectDeployment_FallbackAll(t *testing.T) {
	router := newTestRouterWithIndices("round-robin")

	dep, err := router.SelectDeployment("nonexistent-model")
	if err != nil {
		t.Fatalf("SelectDeployment 报错: %v", err)
	}
	if dep == nil {
		t.Fatal("降级选择不应返回 nil")
	}
}

// TestReliableRouter_SelectDeployment_AllUnhealthy 测试所有端点不健康或冷却中时报错。
func TestReliableRouter_SelectDeployment_AllUnhealthy(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	for _, dep := range router.deployments {
		if dep.ModelName == "test-model" {
			dep.RecordFailure()
		}
	}

	_, err := router.SelectDeployment("test-model")
	if err == nil {
		t.Error("所有端点不健康/冷却中时应报错")
	}
}

// TestReliableRouter_SelectDeployment_CooldownRecover 测试冷却期结束后自动恢复。
func TestReliableRouter_SelectDeployment_CooldownRecover(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	router.deployments[0].RecordFailure()

	// 手动设置冷却时间为过去，模拟冷却期已过
	router.deployments[0].mu.Lock()
	router.deployments[0].cooldownUntil = time.Now().Add(-1 * time.Second)
	router.deployments[0].mu.Unlock()

	dep, err := router.SelectDeployment("test-model")
	if err != nil {
		t.Fatalf("冷却期过后应能选择端点，报错: %v", err)
	}
	if dep == nil {
		t.Fatal("冷却期过后应返回有效端点")
	}
}

// TestReliableRouter_GetStats 测试路由器统计信息。
func TestReliableRouter_GetStats(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	stats := router.GetStats()
	if stats["total_deployments"] != 3 {
		t.Errorf("total_deployments = %v, 期望 3", stats["total_deployments"])
	}
	modelList, ok := stats["model_list"].([]string)
	if !ok {
		t.Fatal("model_list 应为 []string")
	}
	if len(modelList) != 2 {
		t.Errorf("model_list 长度 = %d, 期望 2", len(modelList))
	}
}

// TestReliableRouter_GetModelList 测试获取模型列表。
func TestReliableRouter_GetModelList(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	models := router.GetModelList()
	if len(models) != 2 {
		t.Errorf("model list 长度 = %d, 期望 2", len(models))
	}
}

// TestReliableRouter_GetDeploymentsForModel 测试按模型获取部署列表。
func TestReliableRouter_GetDeploymentsForModel(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	deps := router.GetDeploymentsForModel("test-model")
	if len(deps) != 2 {
		t.Errorf("test-model 端点数 = %d, 期望 2", len(deps))
	}
}

// TestReliableRouter_UpdateDeployments 测试动态更新部署列表。
func TestReliableRouter_UpdateDeployments(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	// 更新为只有 2 个端点
	newDeps := []*Deployment{
		NewDeployment(DeploymentConfig{ID: "new1", ModelName: "new-model", APIKey: "k1", APIBase: "https://new1.test.com"}),
		NewDeployment(DeploymentConfig{ID: "new2", ModelName: "new-model", APIKey: "k2", APIBase: "https://new2.test.com"}),
	}
	router.UpdateDeployments(newDeps)

	// 验证更新后的索引
	deps := router.GetDeploymentsForModel("new-model")
	if len(deps) != 2 {
		t.Errorf("new-model 端点数 = %d, 期望 2", len(deps))
	}

	// 旧模型应该不存在了
	deps = router.GetDeploymentsForModel("test-model")
	if len(deps) != 0 {
		t.Errorf("test-model 端点数 = %d, 期望 0", len(deps))
	}
}

// ──── Session 亲和性测试 ────

// TestSessionAffinity_Basic 测试基本 Session 亲和性。
func TestSessionAffinity_Basic(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	// 第一次调用，建立 session 映射
	dep, err := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("SelectDeploymentWithContext 报错: %v", err)
	}

	// 记录成功并更新 session 映射
	router.RecordSuccessWithSession(dep, 50*time.Millisecond, "session-1")

	// 第二次调用同一个 session，应优先选择同一个 deployment
	dep2, err := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("SelectDeploymentWithContext 报错: %v", err)
	}
	if dep2.ID != dep.ID {
		t.Errorf("Session 亲和性应优先选择同一个端点，第一次 %s，第二次 %s", dep.ID, dep2.ID)
	}
}

// TestSessionAffinity_DifferentSessions 测试不同 Session 的独立性。
func TestSessionAffinity_DifferentSessions(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	// session-1 绑定到 dep1
	dep1, _ := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "session-1"})
	router.RecordSuccessWithSession(dep1, 50*time.Millisecond, "session-1")

	// session-2 绑定到另一个端点
	dep2, _ := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "session-2"})
	router.RecordSuccessWithSession(dep2, 50*time.Millisecond, "session-2")

	// session-1 仍应路由到 dep1
	dep1Again, _ := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "session-1"})
	if dep1Again.ID != dep1.ID {
		t.Errorf("session-1 应亲和到 %s，实际选择了 %s", dep1.ID, dep1Again.ID)
	}
}

// TestSessionAffinity_Expired 测试过期 Session 的清理。
func TestSessionAffinity_Expired(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	// 建立映射
	dep, _ := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "expired-session"})
	router.RecordSuccessWithSession(dep, 50*time.Millisecond, "expired-session")

	// 手动设置映射的 lastUsed 为很久以前，模拟过期
	router.sessionMu.Lock()
	router.sessionMap["expired-session"] = sessionEntry{
		deploymentID: dep.ID,
		lastUsed:     time.Now().Add(-2 * time.Hour), // 2小时前
	}
	router.sessionMu.Unlock()

	// 过期后，不应再亲和到之前的端点
	// （可能选到同一个，也可能选到其他的，只验证不会因为过期映射报错）
	_, err := router.SelectDeploymentWithContext("test-model", &RoutingContext{SessionID: "expired-session"})
	if err != nil {
		t.Fatalf("过期 Session 不应报错: %v", err)
	}
}

// TestSessionAffinity_NoSession 测试无 Session 时的正常路由。
func TestSessionAffinity_NoSession(t *testing.T) {
	router := newTestRouterWithIndices("simple-shuffle")

	dep, err := router.SelectDeployment("test-model")
	if err != nil {
		t.Fatalf("SelectDeployment 报错: %v", err)
	}
	if dep == nil {
		t.Fatal("无 Session 时应正常路由")
	}
}

// ──── 路由缓存共享测试 ────

// TestRouterCache_SameConfig 测试相同配置共享路由器。
func TestRouterCache_SameConfig(t *testing.T) {
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	config := &IntelliRouterClientConfig{
		Strategy:   "simple-shuffle",
		NumRetries: 3,
		Timeout:    30.0,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIKey: "key1", APIBase: "https://api1.test.com"},
		},
	}

	router1 := GetOrCreateRouter(config)
	router2 := GetOrCreateRouter(config)

	if router1 != router2 {
		t.Error("相同配置应共享同一个路由器实例")
	}
	// 清理
	router1.StopHealthChecker()
}

// TestRouterCache_DifferentConfig 测试不同配置创建不同路由器。
func TestRouterCache_DifferentConfig(t *testing.T) {
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	config1 := &IntelliRouterClientConfig{
		Strategy:   "simple-shuffle",
		NumRetries: 3,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIKey: "key1", APIBase: "https://api1.test.com"},
		},
	}
	config2 := &IntelliRouterClientConfig{
		Strategy:   "round-robin",
		NumRetries: 3,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIKey: "key1", APIBase: "https://api1.test.com"},
		},
	}

	router1 := GetOrCreateRouter(config1)
	router2 := GetOrCreateRouter(config2)

	if router1 == router2 {
		t.Error("不同配置应创建不同的路由器实例")
	}
	router1.StopHealthChecker()
	router2.StopHealthChecker()
}

// ──── makeRouterKey 测试 ────

// TestMakeRouterKey_Deterministic 测试缓存 key 的确定性。
func TestMakeRouterKey_Deterministic(t *testing.T) {
	config := &IntelliRouterClientConfig{
		Strategy:   "simple-shuffle",
		NumRetries: 3,
		Timeout:    30.0,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIBase: "https://api1.test.com"},
		},
	}

	key1 := makeRouterKey(config)
	key2 := makeRouterKey(config)

	if key1 != key2 {
		t.Error("相同配置应生成相同的缓存 key")
	}
}

// TestMakeRouterKey_Different 测试不同配置生成不同 key。
func TestMakeRouterKey_Different(t *testing.T) {
	config1 := &IntelliRouterClientConfig{
		Strategy: "simple-shuffle",
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIBase: "https://api1.test.com"},
		},
	}
	config2 := &IntelliRouterClientConfig{
		Strategy: "round-robin",
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIBase: "https://api1.test.com"},
		},
	}

	key1 := makeRouterKey(config1)
	key2 := makeRouterKey(config2)

	if key1 == key2 {
		t.Error("不同配置应生成不同的缓存 key")
	}
}

// ──── createStrategy 测试 ────

// TestCreateStrategy 测试策略创建。
func TestCreateStrategy(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"simple-shuffle"},
		{"round-robin"},
		{"lowest-latency"},
		{"adaptive"},
		{"unknown"},
		{""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := createStrategy(tt.name, nil)
			if s == nil {
				t.Error("strategy 不应为 nil")
			}
		})
	}
}

// TestCreateStrategy_AdaptiveWithKwargs 测试 adaptive 策略带参数。
func TestCreateStrategy_AdaptiveWithKwargs(t *testing.T) {
	kwargs := map[string]any{
		"exploration_ratio": 0.2,
		"w_health":         2.0,
		"w_latency":        0.5,
	}
	s := createStrategy("adaptive", kwargs)
	adp, ok := s.(*AdaptiveStrategy)
	if !ok {
		t.Fatal("应为 AdaptiveStrategy 类型")
	}
	if adp.ExplorationRatio != 0.2 {
		t.Errorf("ExplorationRatio = %f, 期望 0.2", adp.ExplorationRatio)
	}
	if adp.WHealth != 2.0 {
		t.Errorf("WHealth = %f, 期望 2.0", adp.WHealth)
	}
	if adp.WLatency != 0.5 {
		t.Errorf("WLatency = %f, 期望 0.5", adp.WLatency)
	}
}

// TestCreateStrategy_LowestLatencyWithKwargs 测试 lowest-latency 策略带参数。
func TestCreateStrategy_LowestLatencyWithKwargs(t *testing.T) {
	kwargs := map[string]any{
		"exploration_ratio": 0.3,
	}
	s := createStrategy("lowest-latency", kwargs)
	ll, ok := s.(*LowestLatencyStrategy)
	if !ok {
		t.Fatal("应为 LowestLatencyStrategy 类型")
	}
	if ll.ExplorationRatio != 0.3 {
		t.Errorf("ExplorationRatio = %f, 期望 0.3", ll.ExplorationRatio)
	}
}

// TestCreateStrategy_LowestLatencyDefaultKwargs 测试 lowest-latency 默认参数。
func TestCreateStrategy_LowestLatencyDefaultKwargs(t *testing.T) {
	s := createStrategy("lowest-latency", nil)
	ll, ok := s.(*LowestLatencyStrategy)
	if !ok {
		t.Fatal("应为 LowestLatencyStrategy 类型")
	}
	if ll.ExplorationRatio != defaultExplorationRatio {
		t.Errorf("默认 ExplorationRatio = %f, 期望 %f", ll.ExplorationRatio, defaultExplorationRatio)
	}
}

// ──── 集成测试：从配置到路由器 ────

// TestFromConfigToRouter 测试从 ModelClientConfig 创建完整路由器。
func TestFromConfigToRouter(t *testing.T) {
	routerCacheLock.Lock()
	routerCache = make(map[string]*ReliableRouter)
	routerCacheLock.Unlock()

	cc := llmschema.NewModelClientConfig("intelli_router", "placeholder", "http://placeholder",
		llmschema.WithVerifySSL(false),
		llmschema.WithConfigExtra(map[string]any{
			"intelli_router_deployments": []map[string]any{
				{
					"id":         "dep1",
					"model_name": "deepseek-v4-flash",
					"api_key":    "sk-test",
					"api_base":   "https://api.deepseek.com",
					"tpm":        100000,
					"rpm":        60,
					"tags":       []any{"primary"},
				},
			},
			"intelli_router_strategy":    "adaptive",
			"intelli_router_num_retries": 3,
		}),
	)

	config := FromModelClientConfig(cc)
	router := GetOrCreateRouter(config)

	dep, err := router.SelectDeployment("deepseek-v4-flash")
	if err != nil {
		t.Fatalf("SelectDeployment 报错: %v", err)
	}
	if dep.ID != "dep1" {
		t.Errorf("选择的端点 ID = %q, 期望 %q", dep.ID, "dep1")
	}

	stats := router.GetStats()
	if stats["total_deployments"] != 1 {
		t.Errorf("total_deployments = %v, 期望 1", stats["total_deployments"])
	}

	router.StopHealthChecker()
}

// ──── 健康检查测试 ────

// TestHealthChecker_Creation 测试健康检查器创建。
func TestHealthChecker_Creation(t *testing.T) {
	deps := newTestDeployments()
	hc := NewHealthChecker(deps, 300.0, 5.0)

	if hc == nil {
		t.Fatal("HealthChecker 不应为 nil")
	}
	if hc.checkInterval != 300*time.Second {
		t.Errorf("checkInterval = %v, 期望 300s", hc.checkInterval)
	}
	if hc.checkTimeout != 5*time.Second {
		t.Errorf("checkTimeout = %v, 期望 5s", hc.checkTimeout)
	}
}

// TestHealthChecker_DefaultValues 测试健康检查器默认值。
func TestHealthChecker_DefaultValues(t *testing.T) {
	deps := newTestDeployments()
	hc := NewHealthChecker(deps, 0, 0) // 使用默认值

	if hc.checkInterval != 300*time.Second {
		t.Errorf("默认 checkInterval = %v, 期望 300s", hc.checkInterval)
	}
	if hc.checkTimeout != 5*time.Second {
		t.Errorf("默认 checkTimeout = %v, 期望 5s", hc.checkTimeout)
	}
}

// TestHealthChecker_StartStop 测试健康检查器启停。
func TestHealthChecker_StartStop(t *testing.T) {
	deps := newTestDeployments()
	hc := NewHealthChecker(deps, 300.0, 5.0)

	hc.Start()
	if !hc.running.Load() {
		t.Error("Start 后 running 应为 true")
	}

	hc.Stop()
	// 等待 goroutine 退出
	time.Sleep(100 * time.Millisecond)
	if hc.running.Load() {
		t.Error("Stop 后 running 应为 false")
	}
}

// TestReliableRouter_WithHealthCheck 测试带健康检查的路由器创建。
func TestReliableRouter_WithHealthCheck(t *testing.T) {
	config := &IntelliRouterClientConfig{
		Strategy:            "simple-shuffle",
		NumRetries:          3,
		Timeout:             30.0,
		EnableHealthCheck:   true,
		HealthCheckInterval: 300.0,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIKey: "key1", APIBase: "https://api1.test.com"},
		},
	}

	router := NewReliableRouter(config)
	defer router.StopHealthChecker()

	if router.healthChecker == nil {
		t.Error("EnableHealthCheck=true 时应创建 HealthChecker")
	}
}

// TestReliableRouter_WithoutHealthCheck 测试不带健康检查的路由器。
func TestReliableRouter_WithoutHealthCheck(t *testing.T) {
	config := &IntelliRouterClientConfig{
		Strategy:          "simple-shuffle",
		NumRetries:        3,
		Timeout:           30.0,
		EnableHealthCheck: false,
		Deployments: []DeploymentConfig{
			{ID: "dep1", ModelName: "test", APIKey: "key1", APIBase: "https://api1.test.com"},
		},
	}

	router := NewReliableRouter(config)

	if router.healthChecker != nil {
		t.Error("EnableHealthCheck=false 时不应创建 HealthChecker")
	}
}
