// Package router 路由器本体。← intelli_router/router/ 包
// （base_router.py / reliable_router.py）
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/provider"
)

// ──────────────────────────── 常量 ────────────────────────────

// 默认重试次数与超时。← base_router.py:34-37
const (
	// DefaultNumRetries 默认重试次数
	DefaultNumRetries = 0
	// DefaultTimeout 默认超时（秒）
	DefaultTimeout = 30.0
	// ModelWildcard 统一调度通配符。← reliable_router.py MODEL_WILDCARD
	ModelWildcard = "*"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseRouter 基础路由器 - 处理 API 池化和请求转发。
// ← base_router.py:41 BaseRouter
type BaseRouter struct {
	// Deployments 部署列表 ← py:50
	Deployments []*intelliroutercore.Deployment
	// NumRetries 重试次数 ← py:51
	NumRetries int
	// Timeout 超时（秒）← py:52
	Timeout float64
	// State 运行期状态（部署状态/冷却等）。BaseRouter 自身只读它做
	// 可用性过滤；ReliableRouter 会以自己的 state 覆盖此属性
	// （ReliableRouter 是该 state 的唯一写入方）。← py:55-57
	State *intelliroutercore.LocalRouterState

	// modelIndices 模型名 → 部署下标 ← py:65
	modelIndices map[string][]int
	// httpClient HTTP 客户端（Python 按 verify_ssl 分组缓存 httpx.AsyncClient；
	// Go net/http.Client TLS 配置由调用方注入，单客户端等价）
	httpClient *http.Client
	// adapterCache provider 适配器缓存 ← py:62
	// BatchCompletion 并发调用 Completion→GetAdapter 惰性建缓存，读写均需加锁
	adapterCache map[string]provider.BaseProviderAdapter
	// adapterCacheLock 保护 adapterCache 的并发读写
	adapterCacheLock sync.Mutex
	// hookAvailableDeploymentsForModel 可覆写钩子（py:370-378
	// _available_deployments_for_model：Python 子类覆写，Go 经构造期注入
	// 函数指针实现——嵌入方法静态绑定不会派发到「子类」版本）。
	// nil 时用 BaseRouter 默认实现（返回全部部署）。
	hookAvailableDeploymentsForModel func(model string) []*intelliroutercore.Deployment
	// deploymentsLock 保护 deployments/modelIndices 的热替换（读多写少）← py:64
	deploymentsLock sync.RWMutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseRouter 构造。← py:43-66 __init__
func NewBaseRouter(deployments []*intelliroutercore.Deployment, numRetries int, timeout float64, httpClient *http.Client) *BaseRouter {
	if numRetries == 0 {
		numRetries = DefaultNumRetries
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	r := &BaseRouter{
		Deployments:  deployments,
		NumRetries:   numRetries,
		Timeout:      timeout,
		State:        intelliroutercore.NewLocalRouterState(), // ← py:55
		httpClient:   httpClient,
		adapterCache: map[string]provider.BaseProviderAdapter{},
	}
	r.deploymentsLock.Lock()
	r.buildModelIndices() // ← py:65
	r.deploymentsLock.Unlock()
	return r
}

// buildModelIndices 构建模型名到部署列表的映射。
// ← py:68-79 _build_model_indices
//
// 先在局部变量构建完成后一次性原子赋值，避免并发请求在
// 「已清空、未填完」的窗口期读到空索引（热替换场景）。
// 调用方必须已持有 deploymentsLock 写锁（Python RLock 可重入，
// update_deployments 持锁内直调；Go RWMutex 不可重入，收敛为约定）。
func (r *BaseRouter) buildModelIndices() {
	indices := map[string][]int{}
	for i, dep := range r.Deployments {
		indices[dep.ModelName] = append(indices[dep.ModelName], i)
	}
	r.modelIndices = indices
}

// GetDeploymentsForModel 获取指定模型的所有部署。
// ← py:81-93 get_deployments_for_model
//
// 与 UpdateDeployments 的「先换列表再重建索引」热替换并发时，无锁读取可能
// 拿到新列表+旧索引（或反之）导致越界。加锁把「取索引 + 取列表」变成原子
// 操作。
func (r *BaseRouter) GetDeploymentsForModel(model string) []*intelliroutercore.Deployment {
	r.deploymentsLock.RLock()
	defer r.deploymentsLock.RUnlock()
	indices := r.modelIndices[model]
	result := make([]*intelliroutercore.Deployment, 0, len(indices))
	for _, i := range indices {
		if i < len(r.Deployments) {
			result = append(result, r.Deployments[i])
		}
	}
	return result
}

// GetModelList 获取所有模型名列表。← py:95-97
func (r *BaseRouter) GetModelList() []string {
	r.deploymentsLock.RLock()
	defer r.deploymentsLock.RUnlock()
	names := make([]string, 0, len(r.modelIndices))
	for name := range r.modelIndices {
		names = append(names, name)
	}
	return names
}

// DeploymentConfig 部署配置详情（不回传 api_key 明文）。← py:99-115
type DeploymentConfig struct {
	ID               string  `json:"id"`
	ModelID          string  `json:"model_id"`
	ModelName        string  `json:"model_name"`
	APIBase          string  `json:"api_base"`
	HasAPIKey        bool    `json:"has_api_key"`
	FallbackTag      *string `json:"fallback_tag"`
	ModelDescription *string `json:"model_description"`
}

// GetDeploymentConfigs 获取所有部署配置详情。出于安全考虑不回传 api_key
// 明文，只暴露是否已配置（has_api_key）。← py:99-115
func (r *BaseRouter) GetDeploymentConfigs() []DeploymentConfig {
	r.deploymentsLock.RLock()
	defer r.deploymentsLock.RUnlock()
	result := make([]DeploymentConfig, 0, len(r.Deployments))
	for _, dep := range r.Deployments {
		result = append(result, deploymentConfigOf(dep))
	}
	return result
}

// GetDeploymentConfigByModel 获取指定模型的部署配置详情。← py:117-133
func (r *BaseRouter) GetDeploymentConfigByModel(model string) []DeploymentConfig {
	r.deploymentsLock.RLock()
	defer r.deploymentsLock.RUnlock()
	var result []DeploymentConfig
	for _, dep := range r.Deployments {
		if dep.ModelName == model {
			result = append(result, deploymentConfigOf(dep))
		}
	}
	return result
}

// GetAdapter 获取 provider adapter（带缓存）。← py:135-142 _get_adapter
func (r *BaseRouter) GetAdapter(deployment *intelliroutercore.Deployment) (provider.BaseProviderAdapter, error) {
	p := deployment.Provider
	r.adapterCacheLock.Lock()
	defer r.adapterCacheLock.Unlock()
	if a, ok := r.adapterCache[p]; ok {
		return a, nil
	}
	a, err := provider.GetProviderAdapter(p)
	if err != nil {
		return nil, err
	}
	r.adapterCache[p] = a
	return a, nil
}

// RequestTimeout 计算请求实际生效的超时：deployment 显式设置优先，
// 否则回退 router 默认。← py:157-161 _request_timeout
func (r *BaseRouter) RequestTimeout(deployment *intelliroutercore.Deployment) float64 {
	if deployment.Timeout != nil {
		return *deployment.Timeout
	}
	return r.Timeout
}

// MakeRequest 发送 HTTP 请求并处理异常映射。← py:163-252 _make_request
//
// API 响应 JSON（dict 响应会附带 deployment_id 字段，标识实际服务该请求的
// 部署；provider 已提供该字段则不覆盖）。
func (r *BaseRouter) MakeRequest(ctx context.Context, deployment *intelliroutercore.Deployment, requestBody map[string]any) (map[string]any, error) {
	adapter, err := r.GetAdapter(deployment) // ← py:170
	if err != nil {
		return nil, err
	}
	url := adapter.GetAPIURL(deployment, false) // ← py:171
	headers := adapter.GetHeaders(deployment)   // ← py:172
	// 签名与发送共用同一份 body_bytes，天然同源。← py:175-176
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	headers = adapter.SignRequest("POST", url, headers, bodyBytes, deployment) // ← py:177
	requestTimeout := r.RequestTimeout(deployment)                             // ← py:178 per-request 超时覆盖 client 默认值

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(requestTimeout*float64(time.Second)))
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, intellirouter.NewDeploymentNetworkError(deployment.ID, err.Error())
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := r.httpClient.Do(req) // ← py:180-182
	if err != nil {
		// httpx.TimeoutException → DeploymentTimeoutError（py:184-188）
		if reqCtx.Err() == context.DeadlineExceeded {
			return nil, intellirouter.NewDeploymentTimeoutError(deployment.ID, requestTimeout)
		}
		// httpx.ConnectError / RemoteProtocolError → DeploymentNetworkError（py:230-237）
		return nil, intellirouter.NewDeploymentNetworkError(deployment.ID, err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, mapHTTPStatus(deployment, resp.StatusCode, string(body), resp.Header.Get("Retry-After"), requestTimeout)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		// 200 但 body 不是 JSON → 部署侧网络错误（JSONDecodeError 归部署侧，
		// 见 reliable_router._is_request_side_error 分类规则）
		return nil, intellirouter.NewDeploymentNetworkError(deployment.ID, err.Error())
	}
	result := adapter.TransformResponse(raw, deployment.ModelName, deployment) // ← py:184
	// 响应附带 deployment_id：setdefault，provider 已提供则不覆盖。← py:187-190
	if _, exists := result["deployment_id"]; !exists {
		result["deployment_id"] = deployment.ID
	}
	return result, nil
}

// Close 释放底层资源（Python close 关闭 httpx 客户端连接池；Go 的
// http.Client 由注入方管理生命周期，此处关闭空闲连接保持语义对齐）。
// ← base_router.py:254-259 close
func (r *BaseRouter) Close() {
	if tr, ok := r.httpClient.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// Completion 发送 completion 请求。← py:254-290 completion
func (r *BaseRouter) Completion(ctx context.Context, model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) (map[string]any, error) {
	if deployment == nil {
		deployments := r.stateAvailableDeployments(r.GetDeploymentsForModel(model))
		if len(deployments) == 0 {
			return nil, intellirouter.NewNoDeploymentAvailable(model, "No deployment")
		}
		deployment = deployments[0]
	}
	adapter, err := r.GetAdapter(deployment)
	if err != nil {
		return nil, err
	}
	adapter.ValidateRequestConfig(deployment, kwargs)                            // ← py:281
	requestBody := adapter.TransformRequest(model, messages, deployment, kwargs) // ← py:282-287
	return r.MakeRequest(ctx, deployment, requestBody)
}

// StreamChunk 流式 chunk 或错误。
type StreamChunk struct {
	Chunk map[string]any
	Err   error
}

// CompletionStream 流式 completion —— 以 channel 形式产出 OpenAI 统一 SSE
// 格式的 chunk。← py:292-364 acompletion_stream（async generator → channel）
//
// 消费方以 for range 消费；错误经 StreamChunk.Err 传出后通道关闭。
func (r *BaseRouter) CompletionStream(ctx context.Context, model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) <-chan StreamChunk {
	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		if deployment == nil {
			deployments := r.stateAvailableDeployments(r.GetDeploymentsForModel(model))
			if len(deployments) == 0 {
				out <- StreamChunk{Err: intellirouter.NewNoDeploymentAvailable(model, "No deployment")}
				return
			}
			deployment = deployments[0]
		}
		adapter, err := r.GetAdapter(deployment)
		if err != nil {
			out <- StreamChunk{Err: err}
			return
		}
		streamKwargs := map[string]any{"stream": true}
		for k, v := range kwargs {
			streamKwargs[k] = v // ← py:308 stream_kwargs = {"stream": True, **kwargs}
		}
		adapter.ValidateRequestConfig(deployment, streamKwargs) // ← py:309
		requestBody := adapter.TransformRequest(model, messages, deployment, streamKwargs)
		url := adapter.GetAPIURL(deployment, true) // ← py:313
		headers := adapter.GetHeaders(deployment)
		bodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			out <- StreamChunk{Err: err}
			return
		}
		headers = adapter.SignRequest("POST", url, headers, bodyBytes, deployment) // ← py:318
		requestTimeout := r.RequestTimeout(deployment)

		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(requestTimeout*float64(time.Second)))
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			out <- StreamChunk{Err: intellirouter.NewDeploymentNetworkError(deployment.ID, err.Error())}
			return
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := r.httpClient.Do(req)
		if err != nil {
			if reqCtx.Err() == context.DeadlineExceeded {
				out <- StreamChunk{Err: intellirouter.NewDeploymentTimeoutError(deployment.ID, requestTimeout)}
			} else {
				out <- StreamChunk{Err: intellirouter.NewDeploymentNetworkError(deployment.ID, err.Error())}
			}
			return
		}
		defer resp.Body.Close()
		// ← py:330-334 非 2xx 读出 body 再报错，避免隐藏真实状态码
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			out <- StreamChunk{Err: mapHTTPStatus(deployment, resp.StatusCode, string(body), resp.Header.Get("Retry-After"), requestTimeout)}
			return
		}
		it, err := adapter.IterStreamEvents(resp.Body, deployment.ID) // ← py:335
		if err != nil {
			out <- StreamChunk{Err: err}
			return
		}
		for {
			chunk, ok, err := it.Next()
			if err != nil {
				out <- StreamChunk{Err: err}
				return
			}
			if !ok {
				return // 流正常耗尽
			}
			// 消费者中断（ctx 取消）→ GeneratorExit 等价路径
			select {
			case <-ctx.Done():
				return
			default:
			}
			mapped := adapter.TransformStreamChunk(chunk, model, deployment) // ← py:336-337
			if mapped != nil {
				out <- StreamChunk{Chunk: mapped}
			}
		}
	}()
	return out
}

// CompletionWithFallback 带 fallback 的 completion。← py:394-439
func (r *BaseRouter) CompletionWithFallback(ctx context.Context, model string, messages []map[string]any, fallback map[string]string, kwargs map[string]any) (map[string]any, error) {
	modelsToTry := []string{model}
	if fb, ok := fallback[model]; ok {
		modelsToTry = append(modelsToTry, fb) // ← py:404-406
	}
	type depErr struct {
		id  string
		msg string
	}
	var errors []depErr
	for _, m := range modelsToTry {
		// 候选部署经钩子获取：ReliableRouter 覆写为 state 过滤版 ← py:410-413
		var deployments []*intelliroutercore.Deployment
		if r.hookAvailableDeploymentsForModel != nil {
			deployments = r.hookAvailableDeploymentsForModel(m)
		} else {
			deployments = r.availableDeploymentsForModel(m)
		}
		for _, dep := range deployments {
			resp, err := r.Completion(ctx, m, messages, dep, kwargs)
			if err == nil {
				return resp, nil
			}
			errors = append(errors, depErr{dep.ID, err.Error()})
		}
	}
	msgs := make([]string, len(errors))
	for i, e := range errors {
		msgs[i] = fmt.Sprintf("%q %q", e.id, e.msg)
	}
	return nil, intellirouter.NewRouterError("BaseRouter", fmt.Sprintf("All deployments failed: [%s]", strings.Join(msgs, ", ")))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deploymentConfigOf 组装配置详情（不回传 api_key 明文）。← py:101-114
func deploymentConfigOf(dep *intelliroutercore.Deployment) DeploymentConfig {
	return DeploymentConfig{
		ID:               dep.ID,
		ModelID:          dep.ModelID,
		ModelName:        dep.ModelName,
		APIBase:          dep.APIBase,
		HasAPIKey:        dep.APIKey != "",
		FallbackTag:      dep.FallbackTag,
		ModelDescription: dep.ModelDescription,
	}
}

// mapHTTPStatus HTTP 状态码 → 部署错误。← py:190-229 异常映射链
func mapHTTPStatus(deployment *intelliroutercore.Deployment, status int, body string, retryAfter string, requestTimeout float64) error {
	switch {
	case status == 401 || status == 403: // ← py:194-198
		return intellirouter.NewDeploymentAuthError(deployment.ID, status)
	case status == 429: // ← py:199-209
		var ra *float64
		if retryAfter != "" {
			if f, err := parseRetryAfter(retryAfter); err == nil {
				ra = &f
			}
		}
		return intellirouter.NewDeploymentRateLimitError(deployment.ID, ra)
	case status >= 500 && status < 600: // ← py:210-216
		return intellirouter.NewDeploymentServerError(deployment.ID, status, body)
	default: // ← py:217-224
		return intellirouter.NewDeploymentGenericError(deployment.ID, status, body)
	}
}

// parseRetryAfter 解析 retry-after 头（py:202-205 float() 失败忽略）。
func parseRetryAfter(v string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f)
	return f, err
}

// stateAvailableDeployments 按 state 过滤可用部署（BaseRouter 层的简化判断）。
// ← py:266-292 _state_available_deployments
//
// state 中标记为 COOLDOWN 且冷却截止时间未过的部署被跳过；冷却已过期的
// 部署直接视为可用（不写回 state）；未登记的部署默认 HEALTHY。
//
// 与 ReliableRouter._get_available_deployments 的差异：本方法不加 state 锁
// （BaseRouter 读多写少的轻量路径），也不做冷却到期的软恢复——那些写操作
// 属于 ReliableRouter 的职责。（py:273-277 原文）
func (r *BaseRouter) stateAvailableDeployments(deployments []*intelliroutercore.Deployment) []*intelliroutercore.Deployment {
	now := float64(time.Now().UnixMilli()) / 1000
	var available []*intelliroutercore.Deployment
	for _, dep := range deployments {
		status, ok := r.State.DeploymentStatuses[dep.ID]
		if !ok {
			status = intelliroutercore.DeploymentStatusHealthy // ← py:281 get(id, HEALTHY)
		}
		if status == intelliroutercore.DeploymentStatusCooldown {
			until := 0.0
			if u, ok := r.State.CooldownUntil[dep.ID]; ok && u != nil {
				until = *u // ← py:283 get(id, 0)
			}
			if now < until {
				continue
			}
		}
		available = append(available, dep)
	}
	return available
}

// availableDeploymentsForModel 获取模型的候选部署（可覆写钩子，review 16）。
// ← py:370-378 _available_deployments_for_model
//
// 默认返回该模型的全部部署（历史行为）；带状态管理的子类（ReliableRouter）
// 覆写为按 state 过滤后的可用部署。
func (r *BaseRouter) availableDeploymentsForModel(model string) []*intelliroutercore.Deployment {
	return r.GetDeploymentsForModel(model)
}
