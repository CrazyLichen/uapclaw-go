package intellirouter

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 枚举 ────────────────────────────

// errorKind 错误种类标识，用于对齐 utils/exceptions.py 的类层级。
// getStatusCode 按此链（MRO 顺序）查 ERROR_STATUS_MAP。
type errorKind int

// 错误种类枚举，排列与 utils/exceptions.py 类层级一一对应
const (
	kindIntelliRouterError       errorKind = iota // ← exceptions.py:30
	kindRouterError                               // ← exceptions.py:52
	kindNoDeploymentAvailable                     // ← exceptions.py:56
	kindAllDeploymentsFailed                      // ← exceptions.py:65
	kindStrategyError                             // ← exceptions.py:74
	kindDeploymentError                           // ← exceptions.py:85
	kindDeploymentInCooldown                      // ← exceptions.py:89
	kindDeploymentFailed                          // ← exceptions.py:104
	kindDeploymentTimeoutError                    // ← exceptions.py:116
	kindDeploymentAuthError                       // ← exceptions.py:125
	kindDeploymentRateLimitError                  // ← exceptions.py:134
	kindDeploymentServerError                     // ← exceptions.py:146
	kindDeploymentNetworkError                    // ← exceptions.py:155
	kindCacheError                                // ← exceptions.py:170
	kindCacheKeyNotFound                          // ← exceptions.py:174
	kindCacheExpired                              // ← exceptions.py:183
	kindHealthCheckError                          // ← exceptions.py:194
	kindHealthCheckFailed                         // ← exceptions.py:198
	kindHealthCheckTimeout                        // ← exceptions.py:207
	kindConfigError                               // ← exceptions.py:218
	kindInvalidDeploymentConfig                   // ← exceptions.py:222
	kindMissingRequiredField                      // ← exceptions.py:231
)

// ──────────────────────────── 结构体 ────────────────────────────

// IRBase 所有 intellirouter 错误的公共载体。← exceptions.py:30 IntelliRouterError
//
// Go 无异常继承：具体错误类型内嵌本结构并携带 kind 链（自身 → 基类），
// 判型与状态码查表按链序进行，语义对齐 Python `type(error).__mro__`。
// Python 的 _rebuild_exception 深拷贝/pickle 机制（:15）无 Go 对应物，不移植。
type IRBase struct {
	// message 错误消息
	message string
	// details 结构化详情（日志/响应用）← exceptions.py:35 details
	details map[string]any
	// kinds 错误种类链，首元素为具体类型，末元素为 kindIntelliRouterError
	kinds []errorKind
}

// Error 实现 error 接口。
func (e *IRBase) Error() string { return e.message }

// irBase 提取内嵌基址：具体错误类型内嵌 IRBase（值或指针）后，该方法被
// 提升进方法集，asIRBase 借此完成判型（对齐 Python type(error).__mro__）。
func (e *IRBase) irBase() *IRBase { return e }

// Message 错误消息。← exceptions.py:33 self.message
func (e *IRBase) Message() string { return e.message }

// Details 结构化详情。← exceptions.py:34 self.details
func (e *IRBase) Details() map[string]any { return e.details }

// ToDict 转换为字典（用于日志/响应）。← exceptions.py:37 to_dict
func (e *IRBase) ToDict() map[string]any {
	return map[string]any{
		"error_type": kindName(e.kinds[0]),
		"message":    e.message,
		"details":    e.details,
	}
}

// NoDeploymentAvailable 无可用部署错误。← exceptions.py:56
type NoDeploymentAvailable struct{ IRBase }

// AllDeploymentsFailed 所有部署失败错误。← exceptions.py:65
type AllDeploymentsFailed struct{ IRBase }

// StrategyError 策略错误。← exceptions.py:74
type StrategyError struct{ IRBase }

// DeploymentInCooldown 部署在冷却期错误。← exceptions.py:89
type DeploymentInCooldown struct{ IRBase }

// DeploymentFailed 部署已失败错误。← exceptions.py:104
type DeploymentFailed struct{ IRBase }

// DeploymentTimeoutError 部署请求超时错误。← exceptions.py:116
type DeploymentTimeoutError struct{ IRBase }

// DeploymentAuthError 认证失败错误 (401, 403)。← exceptions.py:125
type DeploymentAuthError struct{ IRBase }

// DeploymentRateLimitError 限流错误 (429)。← exceptions.py:134
type DeploymentRateLimitError struct{ IRBase }

// DeploymentServerError 服务器错误 (5xx)。← exceptions.py:146
type DeploymentServerError struct{ IRBase }

// DeploymentNetworkError 网络连接错误。← exceptions.py:155
type DeploymentNetworkError struct{ IRBase }

// CacheKeyNotFound 缓存键不存在错误。← exceptions.py:174
type CacheKeyNotFound struct{ IRBase }

// CacheExpired 缓存已过期错误。← exceptions.py:183
type CacheExpired struct{ IRBase }

// HealthCheckFailed 健康检查失败错误。← exceptions.py:198
type HealthCheckFailed struct{ IRBase }

// HealthCheckTimeout 健康检查超时错误。← exceptions.py:207
type HealthCheckTimeout struct{ IRBase }

// InvalidDeploymentConfig 无效部署配置错误。← exceptions.py:222
type InvalidDeploymentConfig struct{ IRBase }

// MissingRequiredField 缺少必需字段错误。← exceptions.py:231
type MissingRequiredField struct{ IRBase }

// ──────────────────────────── 常量 ────────────────────────────

// errorStatusMap 异常类型到 HTTP 状态码映射。← exceptions.py:252 ERROR_STATUS_MAP
var errorStatusMap = map[errorKind]int{
	kindNoDeploymentAvailable:    503,
	kindAllDeploymentsFailed:     503,
	kindDeploymentInCooldown:     503,
	kindDeploymentFailed:         503,
	kindDeploymentTimeoutError:   504,
	kindDeploymentAuthError:      401,
	kindDeploymentRateLimitError: 429,
	kindDeploymentServerError:    502,
	kindDeploymentNetworkError:   502,
	kindCacheKeyNotFound:         404,
	kindCacheExpired:             410,
	kindHealthCheckFailed:        503,
	kindHealthCheckTimeout:       504,
	kindInvalidDeploymentConfig:  400,
	kindMissingRequiredField:     400,
	kindRouterError:              500,
	kindDeploymentError:          500,
	kindCacheError:               500,
	kindHealthCheckError:         500,
	kindConfigError:              400,
	kindIntelliRouterError:       500,
}

// kindNames 错误种类名表（ToDict 的 error_type 字段，对齐 Python 类名）。
var kindNames = map[errorKind]string{
	kindIntelliRouterError:       "IntelliRouterError",
	kindRouterError:              "RouterError",
	kindNoDeploymentAvailable:    "NoDeploymentAvailable",
	kindAllDeploymentsFailed:     "AllDeploymentsFailed",
	kindStrategyError:            "StrategyError",
	kindDeploymentError:          "DeploymentError",
	kindDeploymentInCooldown:     "DeploymentInCooldown",
	kindDeploymentFailed:         "DeploymentFailed",
	kindDeploymentTimeoutError:   "DeploymentTimeoutError",
	kindDeploymentAuthError:      "DeploymentAuthError",
	kindDeploymentRateLimitError: "DeploymentRateLimitError",
	kindDeploymentServerError:    "DeploymentServerError",
	kindDeploymentNetworkError:   "DeploymentNetworkError",
	kindCacheError:               "CacheError",
	kindCacheKeyNotFound:         "CacheKeyNotFound",
	kindCacheExpired:             "CacheExpired",
	kindHealthCheckError:         "HealthCheckError",
	kindHealthCheckFailed:        "HealthCheckFailed",
	kindHealthCheckTimeout:       "HealthCheckTimeout",
	kindConfigError:              "ConfigError",
	kindInvalidDeploymentConfig:  "InvalidDeploymentConfig",
	kindMissingRequiredField:     "MissingRequiredField",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNoDeploymentAvailable 构造无可用部署错误。← exceptions.py:56-64
func NewNoDeploymentAvailable(model, reason string) *NoDeploymentAvailable {
	return &NoDeploymentAvailable{IRBase{
		message: fmt.Sprintf("No available deployment for model '%s'. %s", model, reason),
		details: map[string]any{"model": model, "reason": reason},
		kinds:   []errorKind{kindNoDeploymentAvailable, kindRouterError, kindIntelliRouterError},
	}}
}

// NewAllDeploymentsFailed 构造所有部署失败错误。← exceptions.py:65-73
func NewAllDeploymentsFailed(model string, errs []any) *AllDeploymentsFailed {
	return &AllDeploymentsFailed{IRBase{
		message: fmt.Sprintf("All deployments failed for model '%s'", model),
		details: map[string]any{"model": model, "errors": errs},
		kinds:   []errorKind{kindAllDeploymentsFailed, kindRouterError, kindIntelliRouterError},
	}}
}

// NewStrategyError 构造策略错误。← exceptions.py:74-83
func NewStrategyError(strategyName, reason string) *StrategyError {
	return &StrategyError{IRBase{
		message: fmt.Sprintf("Strategy '%s' error: %s", strategyName, reason),
		details: map[string]any{"strategy": strategyName, "reason": reason},
		kinds:   []errorKind{kindStrategyError, kindRouterError, kindIntelliRouterError},
	}}
}

// NewDeploymentInCooldown 构造部署冷却错误。← exceptions.py:89-102
// now 注入当前时间（Go 可测性等价实现，替代 Python 内联 time.time()）。
func NewDeploymentInCooldown(deploymentID string, cooldownUntil, now float64) *DeploymentInCooldown {
	remaining := cooldownUntil - now
	if remaining < 0 {
		remaining = 0 // ← py:96 max(0, ...)
	}
	return &DeploymentInCooldown{IRBase{
		message: fmt.Sprintf("Deployment '%s' is in cooldown for %.1fs", deploymentID, remaining),
		details: map[string]any{
			"deployment_id":     deploymentID,
			"cooldown_until":    cooldownUntil,
			"remaining_seconds": remaining,
		},
		kinds: []errorKind{kindDeploymentInCooldown, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewDeploymentInCooldownAt 构造部署冷却错误（以真实时钟计剩余）。
func NewDeploymentInCooldownAt(deploymentID string, cooldownUntil float64) *DeploymentInCooldown {
	return NewDeploymentInCooldown(deploymentID, cooldownUntil, float64(time.Now().UnixMilli())/1000)
}

// NewDeploymentFailed 构造部署失败错误。← exceptions.py:104-114
func NewDeploymentFailed(deploymentID string, consecutiveFailures int) *DeploymentFailed {
	return &DeploymentFailed{IRBase{
		message: fmt.Sprintf("Deployment '%s' has failed %d times", deploymentID, consecutiveFailures),
		details: map[string]any{
			"deployment_id":        deploymentID,
			"consecutive_failures": consecutiveFailures,
		},
		kinds: []errorKind{kindDeploymentFailed, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewDeploymentTimeoutError 构造部署超时错误。← exceptions.py:116-123
func NewDeploymentTimeoutError(deploymentID string, timeout float64) *DeploymentTimeoutError {
	return &DeploymentTimeoutError{IRBase{
		message: fmt.Sprintf("Deployment '%s' timed out after %ss", deploymentID, utils.PyFloatStr(timeout)),
		details: map[string]any{"deployment_id": deploymentID, "timeout": timeout},
		kinds:   []errorKind{kindDeploymentTimeoutError, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewDeploymentAuthError 构造认证失败错误。← exceptions.py:125-132
func NewDeploymentAuthError(deploymentID string, statusCode int) *DeploymentAuthError {
	return &DeploymentAuthError{IRBase{
		message: fmt.Sprintf("Deployment '%s' returned HTTP %d (auth failed)", deploymentID, statusCode),
		details: map[string]any{"deployment_id": deploymentID, "status_code": statusCode},
		kinds:   []errorKind{kindDeploymentAuthError, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewDeploymentRateLimitError 构造限流错误。← exceptions.py:134-144
func NewDeploymentRateLimitError(deploymentID string, retryAfter *float64) *DeploymentRateLimitError {
	msg := fmt.Sprintf("Deployment '%s' rate limited", deploymentID)
	if retryAfter != nil {
		// ← py:137-139 msg += f", retry after {retry_after}s"（str(float) 语义）
		msg = fmt.Sprintf("%s, retry after %ss", msg, utils.PyFloatStr(*retryAfter))
	}
	return &DeploymentRateLimitError{IRBase{
		message: msg,
		details: map[string]any{"deployment_id": deploymentID, "retry_after": retryAfter},
		kinds:   []errorKind{kindDeploymentRateLimitError, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewDeploymentServerError 构造服务器错误。← exceptions.py:146-153
func NewDeploymentServerError(deploymentID string, statusCode int, responseBody string) *DeploymentServerError {
	return &DeploymentServerError{IRBase{
		message: fmt.Sprintf("Deployment '%s' returned HTTP %d (server error)", deploymentID, statusCode),
		details: map[string]any{
			"deployment_id": deploymentID,
			"status_code":   statusCode,
			"response_body": responseBody,
		},
		kinds: []errorKind{kindDeploymentServerError, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewDeploymentGenericError 构造基类部署错误（非专属类别的 HTTP 错误）。
// ← base_router.py:217-224 直接 raise DeploymentError(message=..., details=...)
func NewDeploymentGenericError(deploymentID string, statusCode int, responseBody string) error {
	return &IRBase{
		message: fmt.Sprintf("Deployment '%s' returned HTTP %d", deploymentID, statusCode),
		details: map[string]any{
			"deployment_id": deploymentID,
			"status_code":   statusCode,
			"response_body": responseBody,
		},
		kinds: []errorKind{kindDeploymentError, kindIntelliRouterError},
	}
}

// NewDeploymentNetworkError 构造网络连接错误。← exceptions.py:155-167
func NewDeploymentNetworkError(deploymentID, reason string) *DeploymentNetworkError {
	// deployment_id 传空串时不渲染 "Deployment ''" 前缀，由 reason 自足表达 ← py:162-164
	prefix := ""
	if deploymentID != "" {
		prefix = fmt.Sprintf("Deployment '%s' ", deploymentID)
	}
	return &DeploymentNetworkError{IRBase{
		message: fmt.Sprintf("%snetwork error: %s", prefix, reason),
		details: map[string]any{"deployment_id": deploymentID, "reason": reason},
		kinds:   []errorKind{kindDeploymentNetworkError, kindDeploymentError, kindIntelliRouterError},
	}}
}

// NewCacheKeyNotFound 构造缓存键不存在错误。← exceptions.py:174-181
func NewCacheKeyNotFound(key string) *CacheKeyNotFound {
	return &CacheKeyNotFound{IRBase{
		message: fmt.Sprintf("Cache key '%s' not found", key),
		details: map[string]any{"key": key},
		kinds:   []errorKind{kindCacheKeyNotFound, kindCacheError, kindIntelliRouterError},
	}}
}

// NewCacheExpired 构造缓存过期错误。← exceptions.py:183-191
func NewCacheExpired(key string, expiredAt float64) *CacheExpired {
	return &CacheExpired{IRBase{
		message: fmt.Sprintf("Cache key '%s' has expired", key),
		details: map[string]any{"key": key, "expired_at": expiredAt},
		kinds:   []errorKind{kindCacheExpired, kindCacheError, kindIntelliRouterError},
	}}
}

// NewHealthCheckFailed 构造健康检查失败错误。← exceptions.py:198-205
func NewHealthCheckFailed(deploymentID, errText string) *HealthCheckFailed {
	return &HealthCheckFailed{IRBase{
		message: fmt.Sprintf("Health check failed for deployment '%s'", deploymentID),
		details: map[string]any{"deployment_id": deploymentID, "error": errText},
		kinds:   []errorKind{kindHealthCheckFailed, kindHealthCheckError, kindIntelliRouterError},
	}}
}

// NewHealthCheckTimeout 构造健康检查超时错误。← exceptions.py:207-214
func NewHealthCheckTimeout(deploymentID string, timeout float64) *HealthCheckTimeout {
	return &HealthCheckTimeout{IRBase{
		message: fmt.Sprintf("Health check timed out for deployment '%s' after %ss", deploymentID, utils.PyFloatStr(timeout)),
		details: map[string]any{"deployment_id": deploymentID, "timeout": timeout},
		kinds:   []errorKind{kindHealthCheckTimeout, kindHealthCheckError, kindIntelliRouterError},
	}}
}

// NewInvalidDeploymentConfig 构造无效部署配置错误。← exceptions.py:222-229
func NewInvalidDeploymentConfig(field string, value any, reason string) *InvalidDeploymentConfig {
	return &InvalidDeploymentConfig{IRBase{
		message: fmt.Sprintf("Invalid deployment config: field '%s' with value '%v' - %s", field, value, reason),
		details: map[string]any{"field": field, "value": fmt.Sprintf("%v", value), "reason": reason},
		kinds:   []errorKind{kindInvalidDeploymentConfig, kindConfigError, kindIntelliRouterError},
	}}
}

// NewMissingRequiredField 构造缺少必需字段错误。← exceptions.py:231-238
func NewMissingRequiredField(field, context string) *MissingRequiredField {
	return &MissingRequiredField{IRBase{
		message: fmt.Sprintf("Missing required field '%s' in %s", field, context),
		details: map[string]any{"field": field, "context": context},
		kinds:   []errorKind{kindMissingRequiredField, kindConfigError, kindIntelliRouterError},
	}}
}

// StatusCodeOf 获取错误对应的 HTTP 状态码。← exceptions.py:267 get_status_code
// 按 kind 链（对齐 Python __mro__）查 errorStatusMap，未命中回落 500。
func StatusCodeOf(err error) int {
	var base *IRBase
	if !asIRBase(err, &base) || base == nil {
		return 500
	}
	for _, kind := range base.kinds {
		if code, ok := errorStatusMap[kind]; ok {
			return code
		}
	}
	return 500
}

// NewRouterError 构造路由器基础错误。← exceptions.py:52 RouterError（base_router.py:437 直接 raise）
func NewRouterError(strategy, message string) *StrategyError {
	return NewStrategyError(strategy, message)
}

// IsRouterError 判型：路由器错误。← exceptions.py:52 RouterError
func IsRouterError(err error) bool { return hasKind(err, kindRouterError) }

// IsDeploymentError 判型：部署错误。← exceptions.py:85 DeploymentError
func IsDeploymentError(err error) bool { return hasKind(err, kindDeploymentError) }

// IsCacheError 判型：缓存错误。← exceptions.py:170 CacheError
func IsCacheError(err error) bool { return hasKind(err, kindCacheError) }

// IsHealthCheckError 判型：健康检查错误。← exceptions.py:194 HealthCheckError
func IsHealthCheckError(err error) bool { return hasKind(err, kindHealthCheckError) }

// IsConfigError 判型：配置错误。← exceptions.py:218 ConfigError
func IsConfigError(err error) bool { return hasKind(err, kindConfigError) }

// IsIntelliRouterError 判型：基类。← exceptions.py:30 IntelliRouterError
func IsIntelliRouterError(err error) bool { return hasKind(err, kindIntelliRouterError) }

// IsDeploymentNetworkError 判型：网络错误。← exceptions.py:155
func IsDeploymentNetworkError(err error) bool { return hasKind(err, kindDeploymentNetworkError) }

// IsDeploymentRateLimitError 判型：限流。← exceptions.py:134
func IsDeploymentRateLimitError(err error) bool { return hasKind(err, kindDeploymentRateLimitError) }

// IsDeploymentServerError 判型：服务器错误。← exceptions.py:146
func IsDeploymentServerError(err error) bool { return hasKind(err, kindDeploymentServerError) }

// IsDeploymentTimeoutError 判型：超时。← exceptions.py:116
func IsDeploymentTimeoutError(err error) bool { return hasKind(err, kindDeploymentTimeoutError) }

// AsIRBase 导出判型提取（router 层 isRequestSideError 读 details 用）。
func AsIRBase(err error, target **IRBase) bool { return asIRBase(err, target) }

// ErrorTypeName 错误种类名（fallbackReasonFromError 的类名口径 ← py:291 type(error).__name__）。
func ErrorTypeName(err error) string {
	var base *IRBase
	if !asIRBase(err, &base) || base == nil {
		return "Exception"
	}
	return kindName(base.kinds[0])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// asIRBase 从 error 链中提取 IRBase：先试提升方法 irBase()（内嵌载体），
// 再直接断言 *IRBase，最后沿 Unwrap 链下探。
func asIRBase(err error, target **IRBase) bool {
	for err != nil {
		if b, ok := err.(interface{ irBase() *IRBase }); ok {
			*target = b.irBase()
			return true
		}
		if b, ok := err.(*IRBase); ok {
			*target = b
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// hasKind 判定错误是否携带指定种类。
func hasKind(err error, kind errorKind) bool {
	var base *IRBase
	if !asIRBase(err, &base) || base == nil {
		return false
	}
	for _, k := range base.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// kindName 种类名查表。
func kindName(kind errorKind) string {
	if name, ok := kindNames[kind]; ok {
		return name
	}
	return "IntelliRouterError"
}
