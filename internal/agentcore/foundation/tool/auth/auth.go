package auth

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
