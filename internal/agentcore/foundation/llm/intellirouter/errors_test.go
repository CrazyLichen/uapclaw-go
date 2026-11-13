package intellirouter

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestErrorJudgement 判型与状态码——23.4b intelli_router_errors 四分类的依据。
func TestErrorJudgement(t *testing.T) {
	err := NewDeploymentRateLimitError("dep-1", nil)
	require.True(t, IsDeploymentError(err))
	require.False(t, IsRouterError(err))
	require.Equal(t, 429, StatusCodeOf(err)) // ← exceptions.py:267 get_status_code

	authErr := NewDeploymentAuthError("dep-1", 401)
	require.Equal(t, 401, StatusCodeOf(authErr))
	require.True(t, IsDeploymentError(authErr))

	// NoDeploymentAvailable 是 RouterError 子类 ← py:56
	nda := NewNoDeploymentAvailable("m", "cooldown")
	require.True(t, IsRouterError(nda))
	require.Equal(t, 503, StatusCodeOf(nda))
	require.NotEqual(t, 503, 0)
	_ = nda
}

// TestErrorMessages_逐字对齐 验证消息格式与 Python f-string 一致（payload 契约）。
func TestErrorMessages_逐字对齐(t *testing.T) {
	// ← py:58 f"No available deployment for model '{model}'. {reason}"
	require.Equal(t, "No available deployment for model 'm'. x", NewNoDeploymentAvailable("m", "x").Error())
	// ← py:67 f"All deployments failed for model '{model}'"
	require.Equal(t, "All deployments failed for model 'm'", NewAllDeploymentsFailed("m", nil).Error())
	// ← py:76 f"Strategy '{name}' error: {reason}"
	require.Equal(t, "Strategy 's' error: r", NewStrategyError("s", "r").Error())
	// ← py:97 f"...cooldown for {remaining:.1f}s"（Go %.1f 与 Python :.1f 等价）
	require.Equal(t, "Deployment 'd' is in cooldown for 5.0s", NewDeploymentInCooldown("d", 105.0, 100.0).Error())
	// ← py:96 max(0, cooldown_until - time.time())
	require.Equal(t, "Deployment 'd' is in cooldown for 0.0s", NewDeploymentInCooldown("d", 99.0, 100.0).Error())
	// ← py:106 f"...has failed {n} times"
	require.Equal(t, "Deployment 'd' has failed 3 times", NewDeploymentFailed("d", 3).Error())
	// ← py:118 f"...timed out after {timeout}s"（str(float)：30.0 非 30）
	require.Equal(t, "Deployment 'd' timed out after 30.0s", NewDeploymentTimeoutError("d", 30.0).Error())
	// ← py:127 f"...returned HTTP {code} (auth failed)"
	require.Equal(t, "Deployment 'd' returned HTTP 401 (auth failed)", NewDeploymentAuthError("d", 401).Error())
	// ← py:136/138 rate limited；retry_after 为 None 时无后缀
	require.Equal(t, "Deployment 'd' rate limited", NewDeploymentRateLimitError("d", nil).Error())
	ra := 2.5
	require.Equal(t, "Deployment 'd' rate limited, retry after 2.5s", NewDeploymentRateLimitError("d", &ra).Error())
	// ← py:148 f"...returned HTTP {code} (server error)"
	require.Equal(t, "Deployment 'd' returned HTTP 502 (server error)", NewDeploymentServerError("d", 502, "").Error())
	// ← py:158-164 deployment_id 空串不渲染前缀
	require.Equal(t, "Deployment 'd' network error: eof", NewDeploymentNetworkError("d", "eof").Error())
	require.Equal(t, "network error: eof", NewDeploymentNetworkError("", "eof").Error())
	// ← py:176/185/199/209/224/233 缓存/健康/配置消息
	require.Equal(t, "Cache key 'k' not found", NewCacheKeyNotFound("k").Error())
	require.Equal(t, "Cache key 'k' has expired", NewCacheExpired("k", 1.0).Error())
	require.Equal(t, "Health check failed for deployment 'd'", NewHealthCheckFailed("d", "x").Error())
	require.Equal(t, "Health check timed out for deployment 'd' after 3.0s", NewHealthCheckTimeout("d", 3.0).Error())
	require.Equal(t, "Invalid deployment config: field 'tpm' with value 'bad' - type", NewInvalidDeploymentConfig("tpm", "bad", "type").Error())
	require.Equal(t, "Missing required field 'api_key' in Deployment", NewMissingRequiredField("api_key", "Deployment").Error())
}

// TestToDict 验证 error_type 字段对齐 Python 类名。
func TestToDict(t *testing.T) {
	// ← exceptions.py:37 to_dict
	d := NewDeploymentRateLimitError("d", nil).ToDict()
	require.Equal(t, "DeploymentRateLimitError", d["error_type"])
	require.Equal(t, "Deployment 'd' rate limited", d["message"])
	require.Contains(t, d["details"], "deployment_id")
}

// TestStatusCodeOf_回退500 验证非 IRBase 错误与未映射种类回落 500。
func TestStatusCodeOf_回退500(t *testing.T) {
	// ← exceptions.py:275 return 500
	require.Equal(t, 500, StatusCodeOf(errors.New("plain")))
	require.Equal(t, 500, StatusCodeOf(fmt.Errorf("wrapped: %w", NewDeploymentFailed("d", 1))))
}
