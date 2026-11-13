package routing

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouteFailure 一个 deployment 最近一次失败。
// “message“ 是面向用户的短语，补全 "model 'X' (provider, host) ..."，
// 如 "rejected the API key (HTTP 401)"。
// Python: intelli_router_errors.py:99-111 RouteFailure（frozen dataclass）
type RouteFailure struct {
	// Category 分类（transient/deterministic/request_rejected/stream_interrupted）
	Category string
	// ErrorType intelli_router 异常类名
	ErrorType string
	// StatusCode HTTP 状态码（可选）
	StatusCode *int
	// Message 用户可读短语
	Message string
	// Timestamp 记录时间戳
	Timestamp float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// 路由 case 常量（Python: intelli_router_errors.py:50-53）。
const (
	// RouteCaseSwitchable 还有候选可用
	RouteCaseSwitchable = "switchable"
	// RouteCaseExhaustedRetryable 暂无候选但瞬态失败可恢复
	RouteCaseExhaustedRetryable = "exhausted_retryable"
	// RouteCaseExhaustedRequestRejected 请求本身被拒
	RouteCaseExhaustedRequestRejected = "exhausted_request_rejected"
	// RouteCaseExhaustedUserAction 需要人工介入
	RouteCaseExhaustedUserAction = "exhausted_user_action"
)

// 分类/原因常量（Python: intelli_router_errors.py:55-64）。
const (
	// CategoryDeterministic 确定性失败（凭据/端点）
	CategoryDeterministic = "deterministic"
	// CategoryTransient 瞬态失败
	CategoryTransient = "transient"
	// CategoryRequestRejected 请求被拒
	CategoryRequestRejected = "request_rejected"
	// CategoryStreamInterrupted 流中断
	CategoryStreamInterrupted = "stream_interrupted"

	// ReasonHasTransient 存在瞬态失败
	ReasonHasTransient = "has_transient"
	// ReasonAllDeterministic 全部确定性失败
	ReasonAllDeterministic = "all_deterministic"
	// ReasonStreamInterrupted 流中断原因
	ReasonStreamInterrupted = "stream_interrupted"
	// ReasonTagNotFound tag 不在组内
	ReasonTagNotFound = "tag_not_found"
	// ReasonTransientRepeated 瞬态失败连续超阈值
	ReasonTransientRepeated = "transient_repeated"
)

// ──────────────────────────── 常量 ────────────────────────────

// SwitchRetryDelaySeconds 还有候选时的重调路由延迟。失败的 deployment 已
// 进冷却，无需真等。（Python: intelli_router_errors.py:68）
const SwitchRetryDelaySeconds = 1.0

// maxRetryAfterSeconds 重试 rail 拒绝超过 60s 的延迟
// （model_anomaly_detection_rail）。Python: intelli_router_errors.py:70
const maxRetryAfterSeconds = 60.0

// cooldownMarginSeconds 让重试恰好落在冷却过期之后（Python: :72）
const cooldownMarginSeconds = 0.5

// repeatedFailureThreshold 连续失败达到该次数不值得再等（Python: :75）
const repeatedFailureThreshold = 2

// detailMaxChars 面向用户的 provider 错误摘录上限（Python: :77）
const detailMaxChars = 200

// transientErrorTypes 瞬态异常类型集合（Python: :79-84）
var transientErrorTypes = map[string]struct{}{
	"DeploymentServerError": {}, "DeploymentRateLimitError": {},
	"DeploymentTimeoutError": {}, "DeploymentNetworkError": {},
}

// deterministicStatusCodes 对 deployment 的拒绝（凭据/端点）。
// 换一个 deployment 可能有效；这个 deployment 在修好配置前不会恢复。
// （Python: :85-87）
var deterministicStatusCodes = map[int]struct{}{401: {}, 403: {}, 404: {}}

var httpStatusPattern = regexp.MustCompile(`returned HTTP (\d{3})`)
var timeoutPattern = regexp.MustCompile(`timed out after ([\d.]+)s`)

// successEvents / failureEvents 路由器事件（Python: :92-96）
var successEvents = map[string]struct{}{"request_succeeded": {}, "stream_succeeded": {}}
var failureEvents = map[string]struct{}{"request_retried": {}, "all_deployments_exhausted": {}}

// RoutingRecordEvents 事件桥订阅的记录事件集合（Python: :96 ROUTING_RECORD_EVENTS）。
var RoutingRecordEvents = map[string]struct{}{
	"request_retried": {}, "all_deployments_exhausted": {},
	"request_succeeded": {}, "stream_succeeded": {},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// lastFailures 进程级失败记录：(model_group_id, deployment_id) → 最近失败。
// 跨进程共享是因为不同策略的成员使用不同 router 实例，原因必须从任意实例
// 可读。（Python: :117-118 _last_failures + Lock）
var (
	lastFailuresMu sync.Mutex
	lastFailures   = map[[2]string]*RouteFailure{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResetFailureRecords 清空失败记录（测试用）。Python: :121-124
func ResetFailureRecords() {
	lastFailuresMu.Lock()
	defer lastFailuresMu.Unlock()
	lastFailures = map[[2]string]*RouteFailure{}
}

// RecordRoutingEvent 用路由器事件保持 deployment 失败记录同步。
// REQUEST_RETRIED 携带路由器切离的失败；ALL_DEPLOYMENTS_EXHAUSTED 携带一次
// 调用的最后失败；成功事件清记录，避免修好的 deployment 带着旧原因上报。
// Python: intelli_router_errors.py:244-267 record_routing_event
// event 参数为 duck-typed：读取 event_type/deployment_id/error_type/
// error_message/extra 字段（对应 Python getattr 链）。
func RecordRoutingEvent(event any) {
	eventType := eventString(event, "event_type")
	deploymentID := eventString(event, "deployment_id")
	extra, _ := eventField(event, "extra").(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	if deploymentID == "" {
		if routeID, ok := extra["route_id"].(string); ok {
			deploymentID = routeID
		}
	}
	if deploymentID == "" {
		return
	}
	groupID, _ := extra["model_group_id"].(string)
	if _, ok := successEvents[eventType]; ok {
		clearFailure(groupID, deploymentID)
		return
	}
	errorMessage, _ := eventField(event, "error_message").(string)
	if errorMessage == "" {
		return
	}
	parsedType, statusCode, timeout := parseErrorText(errorMessage)
	errorType := eventString(event, "error_type")
	if errorType == "" {
		if fr, ok := extra["fallback_reason"].(string); ok && fr != "" {
			errorType = fr
		} else {
			errorType = parsedType
		}
	}
	storeFailure(groupID, deploymentID, makeFailure(errorType, statusCode, timeout))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// storeFailure 记录失败（Python: :127-129 _store_failure）。
func storeFailure(modelGroupID, deploymentID string, failure *RouteFailure) {
	lastFailuresMu.Lock()
	defer lastFailuresMu.Unlock()
	lastFailures[[2]string{modelGroupID, deploymentID}] = failure
}

// loadFailure 读取失败（Python: :132-134 _load_failure）。
func loadFailure(modelGroupID, deploymentID string) *RouteFailure {
	lastFailuresMu.Lock()
	defer lastFailuresMu.Unlock()
	return lastFailures[[2]string{modelGroupID, deploymentID}]
}

// clearFailure 清除失败（Python: :137-139 _clear_failure）。
func clearFailure(modelGroupID, deploymentID string) {
	lastFailuresMu.Lock()
	defer lastFailuresMu.Unlock()
	delete(lastFailures, [2]string{modelGroupID, deploymentID})
}

// coerceStatus 归一状态码（Python: :142-149 _coerce_status）。
func coerceStatus(value any) *int {
	switch v := value.(type) {
	case bool:
		return nil
	case int:
		return &v
	case float64:
		n := int(v)
		return &n
	case string:
		s := strings.TrimSpace(v)
		for _, c := range s {
			if c < '0' || c > '9' {
				return nil
			}
		}
		if s == "" {
			return nil
		}
		n := 0
		fmt.Sscanf(s, "%d", &n)
		return &n
	}
	return nil
}

// classify 依据异常类型与状态码分类（Python: :152-159 _classify）。
func classify(errorType string, statusCode *int) string {
	if _, ok := transientErrorTypes[errorType]; ok {
		return CategoryTransient
	}
	if statusCode != nil {
		if _, ok := deterministicStatusCodes[*statusCode]; ok {
			return CategoryDeterministic
		}
		if *statusCode >= 400 && *statusCode < 500 && *statusCode != 429 {
			return CategoryRequestRejected
		}
	}
	return CategoryTransient
}

// providerDetail 从响应体提取 provider 自己的错误消息
// （Python: :162-181 _provider_detail）。
func providerDetail(body any) string {
	if body == nil {
		return ""
	}
	text := fmt.Sprintf("%v", body)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err == nil {
		var candidate any
		if errObj, ok := payload["error"].(map[string]any); ok {
			candidate = errObj["message"]
		} else {
			candidate = payload["error"]
		}
		if s, ok := candidate.(string); !ok || s == "" {
			candidate = payload["message"]
		}
		if s, ok := candidate.(string); ok && strings.TrimSpace(s) != "" {
			text = s
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > detailMaxChars {
		text = strings.TrimRight(text[:detailMaxChars], " ") + "..."
	}
	return text
}

// describe 单次失败的用户可读短语（Python: :184-201 _describe）。
func describe(errorType string, statusCode *int, timeout any, detail string) string {
	switch {
	case errorType == "DeploymentTimeoutError":
		if t, ok := timeout.(string); ok && t != "" {
			return fmt.Sprintf("did not respond within %ss", t)
		}
		if f, ok := timeout.(float64); ok {
			return fmt.Sprintf("did not respond within %ss", pyFloatRepr(f))
		}
		return "did not respond in time"
	case errorType == "DeploymentNetworkError":
		return "could not be reached"
	case errorType == "DeploymentRateLimitError" || (statusCode != nil && *statusCode == 429):
		return "is rate limited (HTTP 429)"
	case statusCode != nil && (*statusCode == 401 || *statusCode == 403):
		return fmt.Sprintf("rejected the API key (HTTP %d)", *statusCode)
	case statusCode != nil && *statusCode == 404:
		return "was not found at its endpoint (HTTP 404)"
	case statusCode != nil && *statusCode >= 500:
		return fmt.Sprintf("returned a server error (HTTP %d)", *statusCode)
	case statusCode != nil && *statusCode >= 400 && *statusCode < 500:
		phrase := fmt.Sprintf("rejected the request (HTTP %d)", *statusCode)
		if detail != "" {
			return phrase + ": " + detail
		}
		return phrase
	}
	return "failed"
}

// pyFloatRepr 对齐 Python str(float) 的 "30.0" 形态。
func pyFloatRepr(f float64) string {
	s := fmt.Sprintf("%f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + ".0"
}

// parseErrorText 从 intelli_router 消息推断 (error_type, status_code, timeout)
// （Python: :204-219 _parse_error_text）。
func parseErrorText(text string) (string, *int, any) {
	var statusCode *int
	if m := httpStatusPattern.FindStringSubmatch(text); m != nil {
		n := 0
		fmt.Sscanf(m[1], "%d", &n)
		statusCode = &n
	}
	if strings.Contains(text, "(auth failed)") {
		return "DeploymentAuthError", statusCode, nil
	}
	if strings.Contains(text, "(server error)") {
		return "DeploymentServerError", statusCode, nil
	}
	if strings.Contains(text, "rate limited") {
		n := 429
		return "DeploymentRateLimitError", &n, nil
	}
	if m := timeoutPattern.FindStringSubmatch(text); m != nil {
		return "DeploymentTimeoutError", nil, m[1]
	}
	if strings.Contains(text, "network error") {
		return "DeploymentNetworkError", nil, nil
	}
	return "DeploymentError", statusCode, nil
}

// makeFailure 构造失败记录（Python: :222-241 _make_failure）。
func makeFailure(errorType string, statusCode *int, timeout any, opts ...failureOption) *RouteFailure {
	var detail string
	var outputEmitted bool
	for _, opt := range opts {
		opt(&detail, &outputEmitted)
	}
	message := describe(errorType, statusCode, timeout, detail)
	category := classify(errorType, statusCode)
	if outputEmitted {
		category = CategoryStreamInterrupted
		message = fmt.Sprintf("stopped in the middle of its answer (%s)", message)
	}
	return &RouteFailure{
		Category:   category,
		ErrorType:  errorType,
		StatusCode: statusCode,
		Message:    message,
		Timestamp:  float64(time.Now().UnixMilli()) / 1000,
	}
}

// failureOption makeFailure 的可选参数（detail/outputEmitted）。
type failureOption func(*string, *bool)

// withDetail 附带 provider 详情。
func withDetail(detail string) failureOption {
	return func(d *string, _ *bool) { *d = detail }
}

// withOutputEmitted 标记流已输出。
func withOutputEmitted() failureOption {
	return func(_ *string, o *bool) { *o = true }
}

// recordException 记录 exc 背后的失败；返回触发它的 (deployment_id, failure)。
// Python: :270-307 _record_exception
func recordException(modelGroupID string, exc error, outputEmitted bool) (string, *RouteFailure) {
	var details map[string]any
	var ir interface{ Details() map[string]any }
	if asIR(exc, &ir) {
		details = ir.Details()
	}
	if details == nil {
		details = map[string]any{}
	}

	if deploymentID, ok := details["deployment_id"].(string); ok && deploymentID != "" {
		errorType := errorTypeName(exc)
		statusCode := coerceStatus(details["status_code"])
		if statusCode == nil && errorType == "DeploymentRateLimitError" {
			n := 429
			statusCode = &n
		}
		var timeout any
		if t, ok := details["timeout"]; ok {
			timeout = t
		}
		failure := makeFailure(errorType, statusCode, timeout,
			withDetail(providerDetail(details["response_body"])),
			boolFailureOption(outputEmitted))
		storeFailure(modelGroupID, deploymentID, failure)
		return deploymentID, failure
	}

	// AllDeploymentsFailed: errors = [(deployment_id, message), ...]
	var lastID string
	var last *RouteFailure
	if errs, ok := details["errors"].([]any); ok {
		for _, item := range errs {
			pair, ok := item.([]any)
			if !ok || len(pair) < 2 {
				continue
			}
			depID, _ := pair[0].(string)
			msg, _ := pair[1].(string)
			errorType, statusCode, timeout := parseErrorText(msg)
			failure := makeFailure(errorType, statusCode, timeout)
			storeFailure(modelGroupID, depID, failure)
			lastID, last = depID, failure
		}
	}
	return lastID, last
}

// boolFailureOption 转换 outputEmitted 布尔为选项。
func boolFailureOption(v bool) failureOption {
	if v {
		return withOutputEmitted()
	}
	return func(_ *string, _ *bool) {}
}

// asIR 尝试把 error 断言为带 Details() 的 intelli_router 错误。
func asIR(err error, out *interface{ Details() map[string]any }) bool {
	if v, ok := err.(interface{ Details() map[string]any }); ok {
		*out = v
		return true
	}
	return false
}

// errorTypeName 取异常类名（对齐 Python type(exc).__name__）。
func errorTypeName(err error) string {
	if n := intellirouter.ErrorTypeName(err); n != "Exception" {
		return n
	}
	return "DeploymentError"
}

// cooldownRemaining deployment 冷却剩余秒数；可选中返回 nil。
// Python: :310-320 cooldown_remaining
func cooldownRemaining(state *intelliroutercore.LocalRouterState, deploymentID string, now float64) *float64 {
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

// consecutiveFailures 连续失败次数（Python: :323-327 _consecutive_failures）。
func consecutiveFailures(state *intelliroutercore.LocalRouterState, deploymentID string) int {
	if state == nil {
		return 0
	}
	return state.ConsecutiveFailures[deploymentID]
}

// releaseCooldown 让 deployment 重新可选（LocalRouterState.reset_deployment）。
// Python: :330-334 _release_cooldown
func releaseCooldown(state *intelliroutercore.LocalRouterState, deploymentID string) {
	if state == nil {
		return
	}
	state.ResetDeployment(deploymentID)
}

// routingCandidates 路由器本次调用可去的 deployments，按组序
// （Python: :343-351 routing_candidates）。
func routingCandidates(deployments []*intelliroutercore.Deployment, tag, modelName string) []*intelliroutercore.Deployment {
	result := append([]*intelliroutercore.Deployment{}, deployments...)
	if modelName != "" && modelName != "*" {
		filtered := result[:0]
		for _, d := range result {
			if d.ModelName == modelName {
				filtered = append(filtered, d)
			}
		}
		result = filtered
	}
	if tag != "" {
		filtered := result[:0]
		for _, d := range result {
			if d.FallbackTag != nil && *d.FallbackTag == tag {
				filtered = append(filtered, d)
			}
		}
		result = filtered
	}
	return result
}

// endpointOf deployment 的主机名（Python: :354-357 _endpoint）。
func endpointOf(deployment *intelliroutercore.Deployment) string {
	apiBase := deployment.APIBase
	if strings.Contains(apiBase, "://") {
		if u, err := url.Parse(apiBase); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	return apiBase
}

// positionOf deployment 在组序中的 1-based 位置（路由器尝试顺序）。
// Python: :360-365 _position
func positionOf(deployments []*intelliroutercore.Deployment, deployment *intelliroutercore.Deployment) *int {
	for i, candidate := range deployments {
		if candidate == deployment {
			n := i + 1
			return &n
		}
	}
	return nil
}

// labelDeployment 渲染 "'name' (provider, host)"（Python: :368-372 _label）。
func labelDeployment(deployment *intelliroutercore.Deployment) string {
	return fmt.Sprintf("'%s' (%s, %s)", deployment.ModelName, deployment.Provider, endpointOf(deployment))
}

// eventField 从 duck-typed 事件对象读取字段（map 键或 struct 导出字段，
// 对应 Python getattr 链）。
func eventField(event any, name string) any {
	switch e := event.(type) {
	case map[string]any:
		return e[name]
	}
	rv := reflect.ValueOf(event)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	normalized := strings.ReplaceAll(name, "_", "")
	f := rv.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, normalized) })
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	return f.Interface()
}

// eventString 读字段并转字符串。
func eventString(event any, name string) string {
	s, _ := eventField(event, name).(string)
	return s
}
