# 领域 3.11 — ToolAuth 工具认证配置与结果

> 对应 Python 源码：`openjiuwen/core/foundation/tool/auth/auth.py` + `auth_callback.py`
> 依赖：`openjiuwen/core/common/security/ssl_utils.py`

## 1. 概述

ToolAuth 为工具调用提供统一认证能力。工具（RestfulApi、MCP 客户端）在发起请求前，通过回调框架触发 `TOOL_AUTH` 事件，由注册的认证策略完成认证并返回结果，调用方从中提取 TLS 配置或认证头注入实际请求。

本设计同时修改 CallbackFramework 的回调签名，使其支持返回值（对齐 Python `AsyncCallbackFramework.trigger()` 的 `List[Any]` 返回模式）。

## 2. 核心决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 回调返回值 | 直接修改现有签名为带 `any` 返回值，TriggerLLM/TriggerTool 返回 `[]any` | 从框架根源统一解决，不新增并行签名；与 Python `List[Any]` 对齐，调用方自行类型断言 |
| SSL 工具归属 | 新建 `common/security/` 包 | 与 Python 目录结构对齐，SSL/TLS 安全配置属于安全范畴，RestfulApi 3.8 回填注释也指向此处 |
| 认证结果载体 | `*tls.Config`（SSL） / `*HeaderQueryProvider`（Header+Query） | Python 用 `aiohttp.TCPConnector`/`httpx.Auth` 是 Python HTTP 库特性，Go 用 `tls.Config` 和自定义结构体更自然 |
| AuthType 表示 | string 常量而非 iota 枚举 | 与 Python `AuthType` 枚举的字符串值对齐（`"ssl"`, `"header_and_query"`），跨语言一致 |
| 策略注册时机 | `init()` 中自动注册 + `RegisterAuthCallback()` 注册回调 | 策略注册与回调注册分离，策略在包加载时自注册，回调在使用时由应用层调用注册 |

## 3. 模块一：CallbackFramework 签名改造

### 3.1 签名变更

```go
// 修改前
type LLMCallbackFunc  func(ctx context.Context, data *LLMCallEventData)
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData)
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData)
func (fw *CallbackFramework) TriggerTool(ctx context.Context, data *ToolCallEventData)

// 修改后
type LLMCallbackFunc  func(ctx context.Context, data *LLMCallEventData) any
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData) any
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) []any
func (fw *CallbackFramework) TriggerTool(ctx context.Context, data *ToolCallEventData) []any
```

### 3.2 TriggerLLM 实现

```go
func (fw *CallbackFramework) TriggerLLM(ctx context.Context, data *LLMCallEventData) []any {
    if ctx == nil || data == nil {
        return nil
    }
    fw.mu.RLock()
    callbacks := fw.llmCallbacks[data.Event]
    fw.mu.RUnlock()

    results := make([]any, 0, len(callbacks))
    for _, fn := range callbacks {
        result := fn(ctx, data)
        results = append(results, result)
    }
    return results
}
```

`TriggerTool` 实现同理。

### 3.3 影响面与适配

现有回调全部返回 `nil`，无需逻辑改动：

| 文件 | 改动 |
|------|------|
| `callback/logging.go` — `LoggingLLMCallback` | 加 `return nil` |
| `callback/events_test.go` — ~10 个匿名回调 | 加 `return nil` |
| `callback/framework_test.go` — ~15 个匿名回调 | 加 `return nil` |
| `tool/lifecycle_tool.go` — ~12 处 `TriggerTool` 调用 | 忽略返回值（`_ = fw.TriggerTool(...)`） |
| `tool/lifecycle_tool_test.go` — ~10 个匿名回调 | 加 `return nil` |
| `llm/model.go` + 各 model_client — ~30 处 `TriggerLLM` 调用 | 忽略返回值 |
| `llm/model_test.go` — ~10 个匿名回调 | 加 `return nil` |

`OffLLM`/`OffTool` 的函数指针匹配逻辑不受影响。

## 4. 模块二：`common/security/` 包

### 4.1 文件结构

```
internal/common/security/
├── doc.go           # 包文档
└── ssl_utils.go     # SslUtils 工具函数
```

### 4.2 核心函数

| Python 方法 | Go 方法 | 签名 |
|------------|---------|------|
| `SslUtils.get_ssl_config()` | `GetSSLConfig()` | `(verify bool, certPath string, err error)` |
| `SslUtils.create_strict_ssl_context()` | `CreateStrictTLSConfig()` | `(*tls.Config, error)` |
| `SslUtils._bool_env()` | `boolEnv()` | `bool` |
| `SslUtils._secure_load_cert()` | `secureLoadCert()` | `error` |

### 4.3 GetSSLConfig

```go
// GetSSLConfig 解析 SSL 配置环境变量，返回是否验证和证书路径。
//
// 对应 Python: SslUtils.get_ssl_config()
//
// 逻辑：
//   - url 非 https → 不验证
//   - verifySwitchEnv 环境变量命中 triggerValue → 不验证
//   - 否则必须提供 sslCertEnv 环境变量指向的证书路径
func GetSSLConfig(verifySwitchEnv, sslCertEnv string, triggerValue []string, urlIsHTTPS bool) (verify bool, certPath string, err error)
```

### 4.4 CreateStrictTLSConfig

```go
// CreateStrictTLSConfig 创建严格 TLS 配置。
//
// 对应 Python: SslUtils.create_strict_ssl_context()
//
// 安全策略（对齐 Python）：
//   - 最低 TLS 1.2
//   - 禁止 TLS 1.0/1.1、SSLv2/v3
//   - 密码套件限制为 ECDHE-AES256-GCM/ECDHE-AES128-GCM
//   - 如果提供 certPath，安全加载证书（SAFE_CERT_DIR 校验 + O_NOFOLLOW + 大小限制）
func CreateStrictTLSConfig(certPath string) (*tls.Config, error)
```

### 4.5 secureLoadCert 安全加载

对齐 Python `_secure_load_cert`：
- 检查 `SAFE_CERT_DIR` 环境变量，证书路径必须在该目录下
- 使用 `os.OpenFile` 带 `O_RDONLY|O_NOFOLLOW` 标志
- 校验文件是常规文件、大小在 1~1MB 之间
- 读取后加载到 `x509.CertPool`

### 4.6 doc.go

```go
// Package security 提供安全相关的工具函数，包括 SSL/TLS 配置和证书安全加载。
//
// 本包对齐 Python openjiuwen/core/common/security/ 的能力，
// 为工具认证（ToolAuth）和其他需要 SSL 配置的组件提供基础设施。
//
// 文件目录：
//
//	security/
//	├── doc.go         # 包文档
//	└── ssl_utils.go   # SSL/TLS 配置工具函数
//
// 对应 Python 代码：openjiuwen/core/common/security/ssl_utils.py
package security
```

## 5. 模块三：`tool/auth/` 包

### 5.1 文件结构

```
internal/agentcore/foundation/tool/auth/
├── doc.go              # 包文档
├── auth.go             # ToolAuthConfig + ToolAuthResult 数据模型
└── auth_callback.go    # AuthType + AuthStrategy + Registry + 回调注册 + HeaderQueryProvider
```

### 5.2 auth.go — 数据模型

对齐 Python `auth.py` 的 `ToolAuthConfig` 和 `ToolAuthResult`：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// ToolAuthConfig 工具认证配置。
//
// 对应 Python: ToolAuthConfig
type ToolAuthConfig struct {
    // AuthType 认证类型：AuthTypeSSL 或 AuthTypeHeaderAndQuery
    AuthType string
    // Config 认证配置参数（各策略从中读取所需字段）
    Config map[string]any
    // ToolType 工具类型：restful_api、mcp 等
    ToolType string
    // ToolID 工具 ID（可选）
    ToolID string
}

// ToolAuthResult 工具认证结果。
//
// 对应 Python: ToolAuthResult
type ToolAuthResult struct {
    // Success 认证是否成功
    Success bool
    // AuthData 认证数据：
    //   SSL 策略 → {"tls_config": *tls.Config}
    //   HeaderQuery 策略 → {"auth_provider": *HeaderQueryProvider}
    AuthData map[string]any
    // Message 认证消息
    Message string
    // Error 认证错误
    Error error
}
```

### 5.3 auth_callback.go — 策略系统

#### AuthType 常量

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    // AuthTypeSSL SSL 认证类型
    AuthTypeSSL = "ssl"
    // AuthTypeHeaderAndQuery 请求头和查询参数认证类型
    AuthTypeHeaderAndQuery = "header_and_query"
)
```

#### AuthStrategy 接口

```go
// ──────────────────────────── 结构体 ────────────────────────────

// AuthStrategy 认证策略接口，不同认证类型实现此接口。
//
// 对应 Python: AuthStrategy (ABC)
type AuthStrategy interface {
    // Authenticate 执行认证，返回认证结果
    Authenticate(ctx context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error)
}
```

#### SSLAuthStrategy

```go
// SSLAuthStrategy SSL 认证策略。
//
// 从 authConfig.Config 中读取 verify_switch_env、ssl_cert_env、url，
// 调用 security.GetSSLConfig() + CreateStrictTLSConfig() 构建 TLS 配置。
//
// 对应 Python: SSLAuthStrategy
type SSLAuthStrategy struct{}

func (s *SSLAuthStrategy) Authenticate(_ context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error) {
    url, _ := authConfig.Config["url"].(string)
    urlIsHTTPS := url != "" && strings.HasPrefix(strings.ToLower(url), "https://")
    if url == "" {
        urlIsHTTPS = true // 与 Python 对齐：无 URL 时默认 HTTPS
    }

    verifySwitchEnv, _ := authConfig.Config["verify_switch_env"].(string)
    sslCertEnv, _ := authConfig.Config["ssl_cert_env"].(string)

    verify, certPath, err := security.GetSSLConfig(verifySwitchEnv, sslCertEnv, []string{"false"}, urlIsHTTPS)
    if err != nil {
        return nil, err
    }

    var tlsConfig *tls.Config
    if verify {
        tlsConfig, err = security.CreateStrictTLSConfig(certPath)
        if err != nil {
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
```

#### HeaderQueryAuthStrategy

```go
// HeaderQueryAuthStrategy 请求头和查询参数认证策略。
//
// 从 authConfig.Config 中读取 auth_headers 和 auth_query_params，
// 构建 HeaderQueryProvider。
//
// 对应 Python: HeaderQueryAuthStrategy
type HeaderQueryAuthStrategy struct{}

func (s *HeaderQueryAuthStrategy) Authenticate(_ context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error) {
    var provider *HeaderQueryProvider
    if authConfig.Config["auth_headers"] != nil || authConfig.Config["auth_query_params"] != nil {
        headers, _ := authConfig.Config["auth_headers"].(map[string]string)
        queryParams, _ := authConfig.Config["auth_query_params"].(map[string]string)
        provider = NewHeaderQueryProvider(headers, queryParams)
    }
    return &ToolAuthResult{
        Success:  true,
        AuthData: map[string]any{"auth_provider": provider},
        Message:  "Custom header and query authentication configured",
    }, nil
}
```

#### HeaderQueryProvider

```go
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
```

#### AuthStrategyRegistry

```go
// AuthStrategyRegistry 认证策略注册表。
//
// 对应 Python: AuthStrategyRegistry
type AuthStrategyRegistry struct {
    strategies map[string]AuthStrategy
}

// 全局注册表实例
var globalRegistry = &AuthStrategyRegistry{
    strategies: make(map[string]AuthStrategy),
}

func init() {
    globalRegistry.Register(AuthTypeSSL, &SSLAuthStrategy{})
    globalRegistry.Register(AuthTypeHeaderAndQuery, &HeaderQueryAuthStrategy{})
}

func (r *AuthStrategyRegistry) Register(authType string, strategy AuthStrategy) {
    r.strategies[authType] = strategy
}

func (r *AuthStrategyRegistry) ExecuteAuth(ctx context.Context, authConfig *ToolAuthConfig) (*ToolAuthResult, error) {
    strategy, ok := r.strategies[authConfig.AuthType]
    if !ok {
        return &ToolAuthResult{
            Success:  false,
            AuthData: map[string]any{},
            Message:  fmt.Sprintf("Unsupported auth type: %s", authConfig.AuthType),
        }, nil
    }
    return strategy.Authenticate(ctx, authConfig)
}
```

#### RegisterAuthCallback — 回调注册

```go
// RegisterAuthCallback 向回调框架注册 TOOL_AUTH 事件的统一认证处理器。
//
// 对应 Python: @framework.on(ToolCallEvents.TOOL_AUTH) + unified_auth_handler
//
// 应用初始化时调用此函数，将 TOOL_AUTH 事件处理器注册到全局 CallbackFramework。
// 处理器从 ToolCallEventData.Extra["auth_config"] 读取 *ToolAuthConfig，
// 调用 AuthStrategyRegistry.ExecuteAuth() 执行认证，返回 *ToolAuthResult。
func RegisterAuthCallback(fw *callback.CallbackFramework) {
    fw.OnTool(callback.ToolAuth, func(_ context.Context, data *callback.ToolCallEventData) any {
        authConfig, ok := data.Extra["auth_config"].(*ToolAuthConfig)
        if !ok {
            return &ToolAuthResult{
                Success:  false,
                AuthData: map[string]any{},
                Message:  "auth_config not found or invalid type in Extra",
            }
        }
        result, err := globalRegistry.ExecuteAuth(context.Background(), authConfig)
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
```

### 5.4 doc.go

```go
// Package auth 提供工具认证配置与结果，以及基于策略模式的认证执行框架。
//
// 工具（RestfulApi、MCP 客户端）在发起请求前，通过回调框架触发 TOOL_AUTH 事件，
// 由注册的认证策略（SSL、HeaderQuery）完成认证并返回结果，
// 调用方从中提取 TLS 配置或认证头注入实际请求。
//
// 认证策略：
//
//	AuthStrategy 接口 — 统一抽象
//	  ├── SSLAuthStrategy — SSL/TLS 证书认证（环境变量驱动）
//	  └── HeaderQueryAuthStrategy — 自定义请求头/查询参数认证
//
// 调用流程：
//
//	工具请求前 → CallbackFramework.TriggerTool(TOOL_AUTH, {auth_config: ...})
//	    → unifiedAuthHandler → AuthStrategyRegistry.ExecuteAuth()
//	    → 返回 ToolAuthResult{AuthData: {"tls_config" | "auth_provider"}}
//	    → 调用方提取结果注入请求
//
// 文件目录：
//
//	auth/
//	├── doc.go              # 包文档
//	├── auth.go             # ToolAuthConfig + ToolAuthResult 数据模型
//	└── auth_callback.go    # AuthType + AuthStrategy + Registry + 回调注册 + HeaderQueryProvider
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/auth/
package auth
```

## 6. 模块四：调用方回填

### 6.1 RestfulApi（`service_api/restful_api.go`）

替换 `doRequest()` 中注释掉的 SSL 代码：

```go
// 触发 TOOL_AUTH 回调获取 SSL 配置
results := callback.GetCallbackFramework().TriggerTool(ctx, &callback.ToolCallEventData{
    Event:    callback.ToolAuth,
    ToolName: r.card.Name,
    ToolID:   r.card.ID,
    Extra: map[string]any{
        "auth_config": &auth.ToolAuthConfig{
            AuthType: auth.AuthTypeSSL,
            Config: map[string]any{
                "verify_switch_env": restfulSSLVerifyEnv,
                "ssl_cert_env":      restfulSSLCertEnv,
                "url":               r.card.URL,
            },
            ToolType: "restful_api",
            ToolID:   r.card.ID,
        },
    },
})

// 从 results 中提取 *auth.ToolAuthResult
var tlsConfig *tls.Config
for _, r := range results {
    if authResult, ok := r.(*auth.ToolAuthResult); ok && authResult.Success {
        if tc, ok := authResult.AuthData["tls_config"].(*tls.Config); ok {
            tlsConfig = tc
            break
        }
    }
}

// 创建 HTTP 客户端（支持代理 + SSL）
client := &http.Client{
    Timeout: time.Duration(timeout * float64(time.Second)),
    Transport: &http.Transport{
        Proxy:           http.ProxyFromEnvironment,
        TLSClientConfig: tlsConfig,
    },
}
```

### 6.2 SseClient（`mcp/client/sse_client.go`）

在 `Connect()` 中触发 TOOL_AUTH 获取 auth_provider，提取 headers 注入 transport 选项：

```go
// 触发 TOOL_AUTH 回调获取认证信息
results := callback.GetCallbackFramework().TriggerTool(ctx, &callback.ToolCallEventData{
    Event:    callback.ToolAuth,
    ToolName: c.serverName,
    ToolID:   c.config.ServerID,
    Extra: map[string]any{
        "auth_config": &auth.ToolAuthConfig{
            AuthType: auth.AuthTypeHeaderAndQuery,
            Config: map[string]any{
                "auth_headers":      c.config.AuthHeaders,
                "auth_query_params": c.config.AuthQueryParams,
            },
            ToolType: c.serverName,
            ToolID:   c.config.ServerID,
        },
    },
})

// 从 results 中提取 *auth.ToolAuthResult → auth_provider
var provider *auth.HeaderQueryProvider
for _, r := range results {
    if authResult, ok := r.(*auth.ToolAuthResult); ok && authResult.Success {
        if p, ok := authResult.AuthData["auth_provider"].(*auth.HeaderQueryProvider); ok {
            provider = p
            break
        }
    }
}

// 将 provider 的 headers 合并到 config.AuthHeaders，一次性传入 transport 选项
if provider != nil && len(provider.Headers) > 0 {
    mergedHeaders := make(map[string]string)
    for k, v := range c.config.AuthHeaders {
        mergedHeaders[k] = v
    }
    for k, v := range provider.Headers {
        mergedHeaders[k] = v
    }
    transportOpts = append(transportOpts, mcptransport.WithHeaders(mergedHeaders))
}
```

### 6.3 StreamableHttpClient（`mcp/client/streamable_http_client.go`）

替换 `⤵️ 3.11 回填` 注释，模式与 SseClient 相同。

## 7. 完整调用流程图

```
RestfulApi.doRequest()
    │
    ├── results := CallbackFramework.TriggerTool(ctx, TOOL_AUTH, {auth_config: SSL})
    │       │
    │       ▼
    │   unifiedAuthHandler (回调) → AuthStrategyRegistry.ExecuteAuth()
    │       │
    │       └── SSLAuthStrategy.Authenticate()
    │             → security.GetSSLConfig()
    │             → security.CreateStrictTLSConfig()
    │             → ToolAuthResult{AuthData: {"tls_config": *tls.Config}}
    │
    ├── 从 results 提取 *ToolAuthResult → tls_config
    └── http.Transport{TLSClientConfig: tls_config}

SseClient.Connect() / StreamableHttpClient.Connect()
    │
    ├── results := CallbackFramework.TriggerTool(ctx, TOOL_AUTH, {auth_config: HEADER_AND_QUERY})
    │       │
    │       ▼
    │   unifiedAuthHandler (回调) → AuthStrategyRegistry.ExecuteAuth()
    │       │
    │       └── HeaderQueryAuthStrategy.Authenticate()
    │             → ToolAuthResult{AuthData: {"auth_provider": *HeaderQueryProvider}}
    │
    ├── 从 results 提取 *ToolAuthResult → auth_provider
    └── 将 headers 注入 mcptransport.WithHeaders()
```

## 8. 测试策略

### 8.1 CallbackFramework 签名改造测试

- 修改现有测试中的匿名回调签名（加 `return nil`）
- 新增 `TestTriggerToolWithResult` 测试回调返回值收集
- 新增 `TestTriggerLLMWithResult` 测试 LLM 回调返回值收集
- 确保现有 `TestCallbackFramework_OffTool`、`TestCallbackFramework_TriggerTool_NilContext` 等测试仍通过

### 8.2 security/ssl_utils 测试

- `TestGetSSLConfig` — 各种环境变量组合（HTTPS/HTTP、verify on/off、有/无证书）
- `TestGetSSLConfig_NonHTTPS` — 非 HTTPS URL 不验证
- `TestGetSSLConfig_VerifyOff` — 触发值命中时不验证
- `TestGetSSLConfig_NoCert` — verify=true 但无证书时报错
- `TestCreateStrictTLSConfig` — TLS 版本、密码套件校验
- `TestCreateStrictTLSConfig_WithCert` — 加载证书
- `TestSecureLoadCert` — 安全加载校验（符号链接拒绝、大小限制、路径校验）

### 8.3 tool/auth 测试

- `TestToolAuthConfig` — 数据模型字段
- `TestToolAuthResult` — 数据模型字段
- `TestSSLAuthStrategy_Authenticate` — 模拟环境变量
- `TestSSLAuthStrategy_Authenticate_NonHTTPS` — 非 HTTPS 跳过验证
- `TestHeaderQueryAuthStrategy_Authenticate` — headers + query params
- `TestHeaderQueryAuthStrategy_Authenticate_Empty` — 无 headers/query 时返回 nil provider
- `TestAuthStrategyRegistry_ExecuteAuth` — 已注册策略正常执行
- `TestAuthStrategyRegistry_ExecuteAuth_Unsupported` — 未注册策略返回失败
- `TestRegisterAuthCallback` — 回调注册后触发 TOOL_AUTH 可获取认证结果
- `TestHeaderQueryProvider` — headers/query 字段正确

### 8.4 调用方回填测试

- `TestRestfulApi_Invoke_WithSSLAuth` — RestfulApi 触发 TOOL_AUTH 获取 TLS 配置
- `TestSseClient_Connect_WithAuthHeaders` — SseClient 触发 TOOL_AUTH 获取 auth_provider
- `TestStreamableHttpClient_Connect_WithAuthHeaders` — StreamableHttpClient 同上

## 9. 新增文件清单

| 文件路径 | 说明 |
|---------|------|
| `internal/common/security/doc.go` | security 包文档 |
| `internal/common/security/ssl_utils.go` | SSL/TLS 配置工具函数 |
| `internal/common/security/ssl_utils_test.go` | SslUtils 单元测试 |
| `internal/agentcore/foundation/tool/auth/doc.go` | auth 包文档 |
| `internal/agentcore/foundation/tool/auth/auth.go` | ToolAuthConfig + ToolAuthResult |
| `internal/agentcore/foundation/tool/auth/auth_test.go` | 数据模型测试 |
| `internal/agentcore/foundation/tool/auth/auth_callback.go` | 认证策略 + 注册表 + 回调 |
| `internal/agentcore/foundation/tool/auth/auth_callback_test.go` | 策略 + 注册表 + 回调测试 |

## 10. 修改文件清单

| 文件路径 | 改动说明 |
|---------|---------|
| `internal/agentcore/runner/callback/framework.go` | 回调签名增加 `any` 返回值，TriggerLLM/TriggerTool 返回 `[]any` |
| `internal/agentcore/runner/callback/logging.go` | `LoggingLLMCallback` 加 `return nil` |
| `internal/agentcore/runner/callback/events_test.go` | 匿名回调加 `return nil` |
| `internal/agentcore/runner/callback/framework_test.go` | 匿名回调加 `return nil`，新增返回值测试 |
| `internal/agentcore/foundation/tool/lifecycle_tool.go` | TriggerTool 调用忽略返回值 |
| `internal/agentcore/foundation/tool/lifecycle_tool_test.go` | 匿名回调加 `return nil` |
| `internal/agentcore/foundation/llm/model.go` | TriggerLLM 调用忽略返回值 |
| `internal/agentcore/foundation/llm/model_test.go` | 匿名回调加 `return nil` |
| `internal/agentcore/foundation/llm/model_clients/openai/client.go` | TriggerLLM 调用忽略返回值 |
| `internal/agentcore/foundation/llm/model_clients/inference_affinity/client.go` | TriggerLLM 调用忽略返回值 |
| `internal/agentcore/foundation/llm/model_clients/siliconflow/client.go` | TriggerLLM 调用忽略返回值 |
| `internal/agentcore/foundation/tool/service_api/restful_api.go` | 回填 TOOL_AUTH SSL 认证 |
| `internal/agentcore/foundation/tool/service_api/restful_api_test.go` | SSL 认证测试 |
| `internal/agentcore/foundation/tool/mcp/client/sse_client.go` | 回填 TOOL_AUTH HeaderQuery 认证 |
| `internal/agentcore/foundation/tool/mcp/client/sse_client_test.go` | HeaderQuery 认证测试 |
| `internal/agentcore/foundation/tool/mcp/client/streamable_http_client.go` | 回填 TOOL_AUTH HeaderQuery 认证 |
| `internal/agentcore/foundation/tool/mcp/client/streamable_http_client_test.go` | HeaderQuery 认证测试 |
| `internal/agentcore/foundation/tool/doc.go` | 更新文件目录树增加 auth/ 子目录 |
| `IMPLEMENTATION_PLAN.md` | 3.11 状态更新 |
