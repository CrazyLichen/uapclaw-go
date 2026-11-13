package intellirouter

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/provider"
	llmrouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/router"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// routerCacheEntry 缓存的 router 实例。Python 另持 asyncio loop 绑定
// （py:282-302 _RouterCacheEntry.loop + :661-700 _ensure_router_for_current_loop）；
// Go net/http.Client 天然并发安全，loop 绑定防御不移植（偏差 D-4）。
type routerCacheEntry struct {
	router *llmrouter.ReliableRouter
}

// ──────────────────────────── 全局变量 ────────────────────────────

// routerCache 进程级 router 缓存：同配置 → 同 ReliableRouter 实例
// （Python: :304-305 _router_cache + _router_cache_lock）。
var (
	routerCacheMu sync.Mutex
	routerCache   = map[string]*routerCacheEntry{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MakeRouterKey 由 router 配置生成确定性缓存键。
// Python: intelli_router_model_client.py:460-472 _make_router_key
// （json.dumps(sort_keys=True) → Go map JSON 序列化天然按键排序）。
func MakeRouterKey(config *IntelliRouterClientConfig) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%d|%s|%t|%s|%t|%t|%d|%s",
		ptrStr(config.ModelGroupID), deploymentsJSON(config.Deployments),
		config.Strategy, kwargsJSON(config.StrategyKwargs),
		config.NumRetries, utils.PyFloatStr(config.Timeout),
		config.EnableHealthCheck, utils.PyFloatStr(config.HealthCheckInterval),
		config.VerifySSL, config.EnableObservability,
		config.WebDashboardPort, ptrStr(config.RouterScope))
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GetOrCreateRouter 从缓存取 ReliableRouter，无则创建并缓存。
// Python: intelli_router_model_client.py:474-495 _get_or_create_router
// （锁内 double-check；Go 无 loop 绑定，偏差 D-4）。
func GetOrCreateRouter(config *IntelliRouterClientConfig) (*llmrouter.ReliableRouter, error) {
	key := MakeRouterKey(config)
	routerCacheMu.Lock()
	defer routerCacheMu.Unlock()
	entry, ok := routerCache[key]
	if !ok {
		newRouter, err := createRouter(config)
		if err != nil {
			return nil, err
		}
		entry = &routerCacheEntry{router: newRouter}
		routerCache[key] = entry
	}
	return entry.router, nil
}

// CreateRouter 由 IntelliRouterClientConfig 创建 ReliableRouter（不进缓存）。
// Python: intelli_router_model_client.py:497-601 _create_router
func CreateRouter(config *IntelliRouterClientConfig) (*llmrouter.ReliableRouter, error) {
	return createRouter(config)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createRouter 创建 ReliableRouter。Python: :497-601 _create_router；
// web_dashboard（:551-557 告警与 :585-600 MetricsWebServer）不移植，偏差 D-3。
func createRouter(config *IntelliRouterClientConfig) (*llmrouter.ReliableRouter, error) {
	if config.WebDashboardPort > 0 && !config.EnableObservability {
		// Python: :551-557
		logWarn("intelli_router_web_dashboard_port is set but enable_observability is False, " +
			"web dashboard requires observability enabled")
	}

	// Python: :533-538 event_bus = cls._create_event_bus_with_core_bridge()
	eventBus := createEventBusWithCoreBridge()
	if config.EnableObservability {
		// Python: :539-549 LoggingHook/MetricsCollector 注册；两模块无 Go
		// 消费方，偏差 D-3 不移植（Go 零值 EventBus 也不会重复注册桥）。
		if len(eventBus.Handlers()) == 0 {
			eventBus = &observability.EventBus{}
			eventBus.Register(&CoreRoutingEventBridge{})
		}
	}

	params := config.routerParamsFor(eventBus)
	r := llmrouter.NewReliableRouter(params)
	applyRoutedModelHooks(r)
	return r, nil
}

// createEventBusWithCoreBridge 创建挂核心桥的事件总线。
// Python: intelli_router_model_client.py:602-611 _create_event_bus_with_core_bridge
func createEventBusWithCoreBridge() *observability.EventBus {
	bus := &observability.EventBus{}
	bus.Register(&CoreRoutingEventBridge{})
	return bus
}

// newHTTPClient 按 verify_ssl 构建共享 HTTP 客户端（Python httpx.AsyncClient
// verify= 语义；Go 侧 Transport TLS 配置）。
func newHTTPClient(verifySSL bool) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // nolint:gosec
	}
	return &http.Client{Transport: transport}
}

// ptrStr 指针字符串取值（nil → ""）。
func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// applyRoutedModelHooks 注入 D-5 钩子（Python: _routed_model_router_class
// 动态子类化 ReliableRouter；Go 构造期函数指针，偏差 D-5）。
func applyRoutedModelHooks(r *llmrouter.ReliableRouter) {
	r.HookGetAdapter = func(deployment *intelliroutercore.Deployment, inner provider.BaseProviderAdapter) provider.BaseProviderAdapter {
		// Python: :144-148 _get_adapter → _RoutedModelAdapter 包装
		return &routedModelAdapter{BaseProviderAdapter: inner}
	}
	r.HookRouteMetadata = func(deployment *intelliroutercore.Deployment, metadata map[string]any) map[string]any {
		// Python: :150-156 _route_metadata → 补 routed_model
		label := RoutedModelLabel(deployment)
		if label != "" {
			metadata["routed_model"] = label
		}
		return metadata
	}
	r.HookAnnounceRouteAttempt = func(deployment *intelliroutercore.Deployment) {
		announceRouteAttempt(r, deployment)
	}
}

// announceRouteAttempt LLM_ROUTE_ATTEMPT 通告 + 路由选择日志。
// Python: intelli_router_model_client.py:157-197 _announce_route_attempt +
// _log_route_selection。metadata 由本函数组装（Python 经 self._route_metadata
// 读取；Go 侧 routeMetadata 未导出，字段集合逐键对齐 _ROUTE_METADATA_FIELDS）。
func announceRouteAttempt(r *llmrouter.ReliableRouter, deployment *intelliroutercore.Deployment) {
	defer func() {
		// Python: :192-194 announcing must never fail the request
		_ = recover()
	}()
	metadata := map[string]any{
		"route_id":   deployment.ID,
		"model_id":   deployment.ModelID,
		"model_name": deployment.ModelName,
		"provider":   deployment.Provider,
	}
	if r.ModelGroupID != nil {
		metadata["model_group_id"] = *r.ModelGroupID
	}
	if label := RoutedModelLabel(deployment); label != "" {
		metadata["routed_model"] = label
	}
	groupID, _ := metadata["model_group_id"].(string)
	if groupID == "" {
		return
	}
	metadata["status"] = "sending"
	logRouteSelection(r, deployment, metadata)
	// Python: :189 trigger(LLMCallEvents.LLM_ROUTE_ATTEMPT, route_metadata=metadata)
	callback.Trigger(context.Background(), string(callback.LLMRouteAttempt), map[string]any{
		"route_metadata": metadata,
	})
}

// logRouteSelection 每次模型组尝试的日志（无论是否在 trace 中）。
// Python: intelli_router_model_client.py:173-191 _log_route_selection。
func logRouteSelection(r *llmrouter.ReliableRouter, deployment *intelliroutercore.Deployment, metadata map[string]any) {
	skipped := []string{}
	now := float64(time.Now().UnixMilli()) / 1000
	for _, candidate := range r.Deployments {
		if candidate.ID == deployment.ID {
			continue
		}
		if remaining := cooldownRemainingSeconds(r, candidate.ID, now); remaining != nil {
			skipped = append(skipped, fmt.Sprintf("%s(%.0fs)", RoutedModelLabel(candidate), *remaining))
		}
	}
	skipNote := ""
	if len(skipped) > 0 {
		skipNote = fmt.Sprintf(" cooldown_skipped=%s", strings.Join(skipped, ","))
	}
	modelLabel := orStr(metadata["routed_model"], metadata["model_name"])
	logInfo(fmt.Sprintf("model group route: group=%s model=%s route=%s%s",
		metadata["model_group_id"], modelLabel, metadata["route_id"], skipNote))
}

// cooldownRemainingSeconds 读 router 状态的冷却余量
// （Python: 经 routing 包 cooldown_remaining 读 state；Go 直接读导出字段）。
func cooldownRemainingSeconds(r *llmrouter.ReliableRouter, deploymentID string, now float64) *float64 {
	state := r.State
	if state == nil {
		return nil
	}
	if status, ok := state.DeploymentStatuses[deploymentID]; ok && status == intelliroutercore.DeploymentStatusCooldown {
		if until, ok := state.CooldownUntil[deploymentID]; ok && until != nil {
			remaining := *until - now
			if remaining > 0 {
				return &remaining
			}
		}
	}
	return nil
}

// orStr 第一个非空字符串值。
func orStr(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
