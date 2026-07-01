package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AuthStrategy 认证策略接口，不同认证类型实现此接口。
//
// 对应 Python: AuthStrategy (ABC)
type AuthStrategy interface {
	// Authenticate 执行认证，返回认证结果
	Authenticate(ctx context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error)
}

// SSLAuthStrategy SSL 认证策略。
//
// 从 authConfig.Config 中读取 verify_switch_env、ssl_cert_env、url，
// 调用 security.GetSSLConfig() + CreateStrictTLSConfig() 构建 TLS 配置。
//
// 对应 Python: SSLAuthStrategy
type SSLAuthStrategy struct{}

// HeaderQueryAuthStrategy 请求头和查询参数认证策略。
//
// 从 authConfig.Config 中读取 auth_headers 和 auth_query_params，
// 构建 HeaderQueryProvider。
//
// 对应 Python: HeaderQueryAuthStrategy
type HeaderQueryAuthStrategy struct{}

// HeaderQueryProvider 请求头和查询参数认证提供者。
//
// 对应 Python: AuthHeaderAndQueryProvider (httpx.Auth 子类)
// Go 没有 httpx.Auth 等价物，实现为持有 headers/query maps 的结构体，
// 调用方自行将 headers/query 注入到 HTTP 请求或 MCP 传输选项中。
type HeaderQueryProvider struct {
	// Headers 认证请求头
	Headers map[string]string
	// QueryParams 认证查询参数
	QueryParams map[string]string
}

// AuthStrategyRegistry 认证策略注册表。
//
// 对应 Python: AuthStrategyRegistry
type AuthStrategyRegistry struct {
	strategies map[string]AuthStrategy
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// AuthTypeSSL SSL 认证类型
	AuthTypeSSL = "ssl"
	// AuthTypeHeaderAndQuery 请求头和查询参数认证类型
	AuthTypeHeaderAndQuery = "header_and_query"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// globalRegistry 全局注册表实例
var globalRegistry = &AuthStrategyRegistry{
	strategies: make(map[string]AuthStrategy),
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHeaderQueryProvider 创建 HeaderQueryProvider 实例。
func NewHeaderQueryProvider(headers, queryParams map[string]string) *HeaderQueryProvider {
	return &HeaderQueryProvider{
		Headers:     headers,
		QueryParams: queryParams,
	}
}

// RegisterAuthCallback 向回调框架注册 TOOL_AUTH 事件的统一认证处理器。
//
// 对应 Python: @framework.on(ToolCallEvents.TOOL_AUTH) + unified_auth_handler
//
// 应用初始化时调用此函数，将 TOOL_AUTH 事件处理器注册到全局 CallbackFramework。
// 处理器从 ToolCallEventData.Extra["auth_config"] 读取 *ToolAuthConfig，
// 调用 AuthStrategyRegistry.ExecuteAuth() 执行认证，返回 *ToolAuthResult。
func RegisterAuthCallback(fw *callback.CallbackFramework) {
	fw.OnTool(callback.ToolAuth, func(ctx context.Context, data *callback.ToolCallEventData) any {
		authConfig, ok := data.Extra["auth_config"].(*ToolAuthConfig)
		if !ok {
			logger.Warn(logger.ComponentAgentCore).
				Str("tool_name", data.ToolName).
				Str("tool_id", data.ToolID).
				Msg("auth_config 未找到或类型不匹配，跳过认证处理")
			return &ToolAuthResult{
				Success:  false,
				AuthData: map[string]any{},
				Message:  "auth_config not found or invalid type in Extra",
			}
		}
		result, err := globalRegistry.ExecuteAuth(ctx, authConfig)
		if err != nil {
			return &ToolAuthResult{
				Success:  false,
				AuthData: map[string]any{},
				Message:  err.Error(),
				Error:    err,
			}
		}
		return result
	})
}

// Authenticate 执行 SSL 认证。
//
// 对应 Python: SSLAuthStrategy.authenticate()
func (s *SSLAuthStrategy) Authenticate(_ context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error) {
	url, _ := authConfig.Config["url"].(string)
	urlIsHTTPS := url != "" && strings.HasPrefix(strings.ToLower(url), "https://")
	if url == "" {
		urlIsHTTPS = true // 与 Python 对齐：无 URL 时默认 HTTPS
	}

	verifySwitchEnv, _ := authConfig.Config["verify_switch_env"].(string)
	sslCertEnv, _ := authConfig.Config["ssl_cert_env"].(string)

	// 默认环境变量名，与 Python 对齐：config.get("verify_switch_env", "SSL_VERIFY")
	if verifySwitchEnv == "" {
		verifySwitchEnv = "SSL_VERIFY"
	}
	if sslCertEnv == "" {
		sslCertEnv = "SSL_CERT"
	}

	verify, certPath, err := security.GetSSLConfig(verifySwitchEnv, sslCertEnv, []string{"false"}, urlIsHTTPS)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("auth_type", AuthTypeSSL).
			Str("tool_type", authConfig.ToolType).
			Msg("SSL 认证配置获取失败")
		return nil, err
	}

	var tlsConfig *tls.Config
	if verify {
		tlsConfig, err = security.CreateStrictTLSConfig(certPath)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Err(err).
				Str("auth_type", AuthTypeSSL).
				Str("tool_type", authConfig.ToolType).
				Msg("创建严格 TLS 配置失败")
			return nil, err
		}
	} else {
		tlsConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &ToolAuthResult{
		Success:  true,
		AuthData: map[string]any{"tls_config": tlsConfig},
		Message:  "SSL authentication configured",
	}, nil
}

// Authenticate 执行 HeaderQuery 认证。
//
// 对应 Python: HeaderQueryAuthStrategy.authenticate()
func (s *HeaderQueryAuthStrategy) Authenticate(_ context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error) {
	var provider *HeaderQueryProvider
	if authConfig.Config["auth_headers"] != nil || authConfig.Config["auth_query_params"] != nil {
		headers := toStringMap(authConfig.Config["auth_headers"])
		queryParams := toStringMap(authConfig.Config["auth_query_params"])
		provider = NewHeaderQueryProvider(headers, queryParams)
		logger.Info(logger.ComponentAgentCore).
			Str("auth_type", AuthTypeHeaderAndQuery).
			Str("tool_type", authConfig.ToolType).
			Msg("使用自定义 header 和 query 认证")
	}
	return &ToolAuthResult{
		Success:  true,
		AuthData: map[string]any{"auth_provider": provider},
		Message:  "Custom header and query authentication configured",
	}, nil
}

// Register 注册认证策略。
func (r *AuthStrategyRegistry) Register(authType string, strategy AuthStrategy) {
	r.strategies[authType] = strategy
}

// ExecuteAuth 根据认证类型执行对应策略。
//
// 对应 Python: AuthStrategyRegistry.execute_auth()
func (r *AuthStrategyRegistry) ExecuteAuth(ctx context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error) {
	strategy, ok := r.strategies[authConfig.AuthType]
	if !ok {
		logger.Warn(logger.ComponentAgentCore).
			Str("auth_type", authConfig.AuthType).
			Msg("不支持的认证类型")
		return &ToolAuthResult{
			Success:  false,
			AuthData: map[string]any{},
			Message:  fmt.Sprintf("Unsupported auth type: %s", authConfig.AuthType),
		}, nil
	}
	return strategy.Authenticate(ctx, authConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册默认认证策略。
func init() {
	globalRegistry.Register(AuthTypeSSL, &SSLAuthStrategy{})
	globalRegistry.Register(AuthTypeHeaderAndQuery, &HeaderQueryAuthStrategy{})
}

// toStringMap 安全地将 any 转换为 map[string]string。
// 支持 map[string]string 和 map[string]any 两种类型。
func toStringMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	// 直接匹配 map[string]string
	if m, ok := v.(map[string]string); ok {
		return m
	}
	// 匹配 map[string]any 并转换
	if m, ok := v.(map[string]any); ok {
		result := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				result[k] = s
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		}
		return result
	}
	return nil
}
