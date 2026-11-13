// Package health 部署健康检查。← intelli_router/health/checker.py
package health

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/provider"
)

// ──────────────────────────── 常量 ────────────────────────────

// 默认参数 ← health/checker.py:17-24
const (
	// DefaultCheckInterval 默认检查间隔（秒）
	DefaultCheckInterval = 300
	// DefaultCheckTimeout 默认检查超时（秒）
	DefaultCheckTimeout = 5
	// DefaultCheckMaxTokens 默认最大 tokens
	DefaultCheckMaxTokens = 10
)

// DefaultCheckMessage 默认检查消息。← py:21 DEFAULT_CHECK_MESSAGE
var DefaultCheckMessage = []map[string]any{{"role": "user", "content": "ping"}}

// ──────────────────────────── 结构体 ────────────────────────────

// HealthCheckResult 健康检查结果。← health/checker.py:27 HealthCheckResult
type HealthCheckResult struct {
	// DeploymentID 部署 ID
	DeploymentID string
	// IsHealthy 是否健康
	IsHealthy bool
	// Latency 检查延迟（秒；失败时 nil）← py:31
	Latency *float64
	// Error 错误信息 ← py:32
	Error *string
	// Timestamp 结果时间戳
	Timestamp float64
}

// SDKHealthChecker SDK 健康检查器 - 检查部署可用性（按需检查 + 后台可选）。
// ← health/checker.py:38 SDKHealthChecker
type SDKHealthChecker struct {
	// Deployments 被检查的部署列表 ← py:48
	Deployments []*intelliroutercore.Deployment
	// State 路由状态（回写健康标记）← py:49
	State *intelliroutercore.LocalRouterState
	// CheckInterval 检查间隔（秒）← py:50
	CheckInterval float64
	// CheckTimeout 检查超时（秒）← py:51
	CheckTimeout float64
	// CheckMessage 检查消息 ← py:54-57（拷贝存储，见构造注释）
	CheckMessage []map[string]any
	// CheckMaxTokens 默认最大 tokens ← py:57
	CheckMaxTokens int

	// httpClient 注入的 HTTP 客户端（测试注入 httptest；Python 按
	// verify_ssl 分组缓存 httpx.AsyncClient，Go net/http.Transport
	// 由调用方配置 TLS，此处单客户端等价）
	httpClient *http.Client

	running bool
	stopCh  chan struct{}
	mu      sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSDKHealthChecker 构造。← checker.py:41-61 __init__
//
// CheckMessage 拷贝存储：默认值不能是模块级可变对象（所有未传参实例会共享
// 同一列表），保存的也不能是调用方列表的引用（调用方后续 append 会悄悄改变
// 健康检查请求内容）。（py:53-55 原文）
func NewSDKHealthChecker(deployments []*intelliroutercore.Deployment, state *intelliroutercore.LocalRouterState, checkInterval, checkTimeout float64, checkMessage []map[string]any, checkMaxTokens int, httpClient *http.Client) *SDKHealthChecker {
	if checkInterval == 0 {
		checkInterval = DefaultCheckInterval // ← py:44 default
	}
	if checkTimeout == 0 {
		checkTimeout = DefaultCheckTimeout // ← py:45 default
	}
	if checkMessage == nil {
		checkMessage = DefaultCheckMessage
	}
	// ← py:54 self.check_message = list(check_message or DEFAULT_CHECK_MESSAGE)
	msgCopy := make([]map[string]any, len(checkMessage))
	copy(msgCopy, checkMessage)
	if checkMaxTokens == 0 {
		checkMaxTokens = DefaultCheckMaxTokens // ← py:46 default
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(checkTimeout * float64(time.Second))}
	}
	return &SDKHealthChecker{
		Deployments:    deployments,
		State:          state,
		CheckInterval:  checkInterval,
		CheckTimeout:   checkTimeout,
		CheckMessage:   msgCopy,
		CheckMaxTokens: checkMaxTokens,
		httpClient:     httpClient,
		stopCh:         make(chan struct{}),
	}
}

// CheckDeployment 检查单个部署健康状态。策略: 发送一个最小化 completion 请求。
// ← checker.py:80 check_deployment
func (c *SDKHealthChecker) CheckDeployment(deployment *intelliroutercore.Deployment) HealthCheckResult {
	start := time.Now()
	result := func(isHealthy bool, latency *float64, errMsg *string) HealthCheckResult {
		return HealthCheckResult{
			DeploymentID: deployment.ID,
			IsHealthy:    isHealthy,
			Latency:      latency,
			Error:        errMsg,
			Timestamp:    float64(time.Now().UnixMilli()) / 1000, // ← py:33 default_factory=time.time
		}
	}
	adapter, err := provider.GetProviderAdapter(deployment.Provider) // ← py:86-87
	if err != nil {
		msg := err.Error()
		return result(false, nil, &msg)
	}
	requestBody := adapter.TransformRequest( // ← py:88-93
		deployment.ModelName,
		c.CheckMessage,
		deployment,
		map[string]any{"max_tokens": c.CheckMaxTokens},
	)
	url := adapter.GetAPIURL(deployment, false) // ← py:94
	headers := adapter.GetHeaders(deployment)   // ← py:95
	bodyBytes, err := json.Marshal(requestBody) // ← py:100-101 先序列化成 bytes 再传，保证签名与实际请求体一致
	if err != nil {
		msg := err.Error()
		return result(false, nil, &msg)
	}
	headers = adapter.SignRequest("POST", url, headers, bodyBytes, deployment) // ← py:102-103

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		msg := err.Error()
		return result(false, nil, &msg)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req) // ← py:105-107（httpx client.post）
	if err != nil {
		msg := err.Error()
		return result(false, nil, &msg) // ← py:128-132 except Exception
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		latency := time.Since(start).Seconds() // ← py:109 latency = time.time() - start
		return result(true, &latency, nil)     // ← py:110-114
	}
	msg := "HTTP " + strconv.Itoa(resp.StatusCode) // ← py:116-119 f"HTTP {response.status_code}"
	return result(false, nil, &msg)
}

// CheckAllDeployments 检查所有部署（并发）。← checker.py:121 check_all_deployments
func (c *SDKHealthChecker) CheckAllDeployments() map[string]HealthCheckResult {
	results := make(map[string]HealthCheckResult, len(c.Deployments))
	var wg sync.WaitGroup
	var mu sync.Mutex
	// ← py:127-129 并发检查（asyncio.gather → WaitGroup + goroutine）
	for _, dep := range c.Deployments {
		wg.Add(1)
		go func(dep *intelliroutercore.Deployment) {
			defer wg.Done()
			r := c.CheckDeployment(dep)
			mu.Lock()
			defer mu.Unlock()
			results[dep.ID] = r
			// ← py:137-138 回写健康标记（异常路径无回写——goroutine 内
			// CheckDeployment 已把异常折叠为失败结果，gather(return_exceptions)
			// 分支在 Go 侧不可达，语义等价）
			c.State.UpdateHealth(dep.ID, r.IsHealthy)
		}(dep)
	}
	wg.Wait()
	return results
}

// GetHealthyDeployments 获取健康部署列表。← checker.py:145 get_healthy_deployments
//
// 两重条件（缺一不可）：
//  1. 健康检查通过：state.health_state 为 true（默认 true）；
//  2. 运行期状态可用：RouterState 里非「未过期的 COOLDOWN」。
//
// 运行期可用性以 RouterState 为准（base_strategy 契约明示「state 是运行期
// 状态的唯一事实源」）：真实请求失败触发的冷却只写进 state（on_failure），
// 不会写回 Deployment 对象。（py:150-159 原文）
func (c *SDKHealthChecker) GetHealthyDeployments(now float64) []*intelliroutercore.Deployment {
	var healthy []*intelliroutercore.Deployment
	for _, dep := range c.Deployments {
		if !c.State.HealthStateOf(dep.ID) { // ← py:163（get(id, True) 默认 true）
			continue
		}
		if !c.State.DeploymentAvailable(dep.ID, now) { // ← py:165-166（_is_state_available）
			continue
		}
		healthy = append(healthy, dep)
	}
	return healthy
}

// GetUnhealthyIDs 获取不健康部署 ID 集合。← checker.py:188 get_unhealthy_ids
func (c *SDKHealthChecker) GetUnhealthyIDs() map[string]bool {
	result := make(map[string]bool)
	for depID, isHealthy := range c.State.HealthStates {
		if !isHealthy {
			result[depID] = true
		}
	}
	return result
}

// StartBackgroundCheck 启动后台健康检查。← checker.py:197 start_background_check
func (c *SDKHealthChecker) StartBackgroundCheck() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return // ← py:199-200 幂等
	}
	c.running = true
	c.stopCh = make(chan struct{})
	// ← py:202 asyncio.create_task(_background_loop) → goroutine
	go c.backgroundLoop()
}

// StopBackgroundCheck 停止后台健康检查。← checker.py:205 stop_background_check
func (c *SDKHealthChecker) StopBackgroundCheck() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// backgroundLoop 后台检查循环。← checker.py:219 _background_loop
func (c *SDKHealthChecker) backgroundLoop() {
	ticker := time.NewTicker(time.Duration(c.CheckInterval * float64(time.Second)))
	defer ticker.Stop()
	// 启动即先查一轮（对齐 py:224-226 先 check 后 sleep 的顺序）
	c.checkSafely()
	for {
		select {
		case <-c.stopCh:
			return // ← py:222 while self._running 退出
		case <-ticker.C:
			c.checkSafely() // ← py:224-226 except Exception: pass 吞错
		}
	}
}

// checkSafely 单轮检查（异常吞掉，对齐 py:224-226）。
func (c *SDKHealthChecker) checkSafely() {
	func() {
		defer func() { _ = recover() }()
		c.CheckAllDeployments()
	}()
}
