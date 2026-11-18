package schema

// 第三方外部成员运行时可靠性的共享领域词汇。
//
// 一个 Claude Agent SDK 或 OpenAI Codex SDK 成员会浮出两类可靠性信号：暂时性
// 的重试进度（SDK 仍在自动重试）与终局化的失败（一次启动或一轮已结束）。本文件
// 承载两者的共享领域词汇：
//
//   - ExternalRuntimeFailureCategory——失败类别的闭合集（鉴权/配额/限流/请求/
//     服务端/网络/进程启动/SDK 错误/未知）
//   - UserActionRequiredMap / UserActionRequired——类别是否需要用户或外部系统介入
//   - ExternalRuntimeFailureReason——结构化原因（原始文案、SDK 错误类型/码、HTTP 状态）
//   - ExternalRuntimeFailure——终局失败载荷；failure_id 是串起失败消息、轮次结果、
//     日志与 trace 的领域关联 id
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/external_runtime_reliability.py（108 行）

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalRuntimeFailureReason 外部运行时失败的结构化原因。
// Python: ExternalRuntimeFailureReason (external_runtime_reliability.py:66-74)
type ExternalRuntimeFailureReason struct {
	// Message 原始 SDK 错误文案
	Message string `json:"message"`
	// SDKErrorType SDK 异常类型名（如有）
	SDKErrorType string `json:"sdk_error_type"`
	// SDKErrorCode SDK 错误码（如有）
	SDKErrorCode string `json:"sdk_error_code"`
	// HTTPStatus 失败调用的 HTTP 状态码（如有）
	HTTPStatus *int `json:"http_status"`
}

// ExternalRuntimeFailure 外部运行时的终局失败载荷。
// Python: ExternalRuntimeFailure (external_runtime_reliability.py:75-101)
type ExternalRuntimeFailure struct {
	// Type 结构化 JSON 载荷判别字段，恒 "external_runtime_failed"
	Type string `json:"type"`
	// FailureID 稳定的失败关联 id（串起失败消息/轮次结果/日志/trace）
	FailureID string `json:"failure_id"`
	// TeamName 团队名
	TeamName string `json:"team_name"`
	// MemberName 失败成员名
	MemberName string `json:"member_name"`
	// AgentKind 产生失败的 SDK（claude/codex 或第三方 provider 名）
	AgentKind ExternalRuntimeAgentKind `json:"agent_kind"`
	// Model CLI/SDK 确认的实际服务模型（如已知）
	Model string `json:"model"`
	// CLIPath 显式配置的 CLI 可执行路径（如有）
	CLIPath *string `json:"cli_path"`
	// Phase 启动或轮次
	Phase ExternalRuntimePhase `json:"phase"`
	// Category 失败类别
	Category ExternalRuntimeFailureCategory `json:"category"`
	// UserActionRequired 是否需要用户介入
	UserActionRequired bool `json:"user_action_required"`
	// Summary 面向人/LLM 的一行描述
	Summary string `json:"summary"`
	// SuggestedAction 建议处理方向
	SuggestedAction string `json:"suggested_action"`
	// Reason 结构化原因
	Reason *ExternalRuntimeFailureReason `json:"reason"`
	// RoundID 成员轮次 id（startup 为 nil）
	RoundID *int `json:"round_id"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ExternalRuntimeFailureCategory 外部运行时失败类别的闭合集。
// Python: external_runtime_reliability.py:31-41 Literal
type ExternalRuntimeFailureCategory = string

// 类别常量（Python Literal 九项逐值）
const (
	// ExternalRuntimeFailureCategoryAuthRequired 需要鉴权
	ExternalRuntimeFailureCategoryAuthRequired ExternalRuntimeFailureCategory = "auth_required"
	// ExternalRuntimeFailureCategoryQuotaExceeded 配额耗尽
	ExternalRuntimeFailureCategoryQuotaExceeded ExternalRuntimeFailureCategory = "quota_exceeded"
	// ExternalRuntimeFailureCategoryRateLimited 被限流
	ExternalRuntimeFailureCategoryRateLimited ExternalRuntimeFailureCategory = "rate_limited"
	// ExternalRuntimeFailureCategoryRequestRejected 请求被拒
	ExternalRuntimeFailureCategoryRequestRejected ExternalRuntimeFailureCategory = "request_rejected"
	// ExternalRuntimeFailureCategoryServerUnavailable 服务端不可用
	ExternalRuntimeFailureCategoryServerUnavailable ExternalRuntimeFailureCategory = "server_unavailable"
	// ExternalRuntimeFailureCategoryNetworkTimeout 网络超时
	ExternalRuntimeFailureCategoryNetworkTimeout ExternalRuntimeFailureCategory = "network_timeout"
	// ExternalRuntimeFailureCategoryProcessStartFailed 进程启动失败
	ExternalRuntimeFailureCategoryProcessStartFailed ExternalRuntimeFailureCategory = "process_start_failed"
	// ExternalRuntimeFailureCategorySDKError SDK 错误
	ExternalRuntimeFailureCategorySDKError ExternalRuntimeFailureCategory = "sdk_error"
	// ExternalRuntimeFailureCategoryUnknown 未知
	ExternalRuntimeFailureCategoryUnknown ExternalRuntimeFailureCategory = "unknown"
)

// ExternalRuntimeAgentKind 产生失败的 SDK 种类（claude/codex 或第三方 provider 名）。
// Python: ExternalRuntimeAgentKind = str
type ExternalRuntimeAgentKind = string

// ExternalRuntimePhase 失败发生的阶段。
// Python: ExternalRuntimePhase = Literal["startup", "turn"]
type ExternalRuntimePhase = string

// 阶段常量（Python Literal 两项逐值）
const (
	// ExternalRuntimePhaseStartup 启动阶段
	ExternalRuntimePhaseStartup ExternalRuntimePhase = "startup"
	// ExternalRuntimePhaseTurn 轮次阶段
	ExternalRuntimePhaseTurn ExternalRuntimePhase = "turn"
)

// ──────────────────────────── 常量 ────────────────────────────

// externalRuntimeFailedType ExternalRuntimeFailure 的判别字段恒值。
// Python: type: Literal["external_runtime_failed"]
const externalRuntimeFailedType = "external_runtime_failed"

// ──────────────────────────── 全局变量 ────────────────────────────

// UserActionRequiredMap 每类别的默认 user_action_required；true 表示需要用户或外部系统介入。
// Python: USER_ACTION_REQUIRED (external_runtime_reliability.py:48-58)
var UserActionRequiredMap = map[ExternalRuntimeFailureCategory]bool{
	ExternalRuntimeFailureCategoryAuthRequired:       true,
	ExternalRuntimeFailureCategoryQuotaExceeded:      true,
	ExternalRuntimeFailureCategoryRateLimited:        false,
	ExternalRuntimeFailureCategoryRequestRejected:    true,
	ExternalRuntimeFailureCategoryServerUnavailable:  false,
	ExternalRuntimeFailureCategoryNetworkTimeout:     false,
	ExternalRuntimeFailureCategoryProcessStartFailed: true,
	ExternalRuntimeFailureCategorySDKError:           false,
	ExternalRuntimeFailureCategoryUnknown:            false,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// UserActionRequired 返回类别的默认 user_action_required（集外 false）。
// Python: user_action_required (external_runtime_reliability.py:61)
func UserActionRequired(category ExternalRuntimeFailureCategory) bool {
	return UserActionRequiredMap[category]
}

// NewExternalRuntimeFailureReason 构造失败原因（对应 Python BaseModel 字段默认值）。
func NewExternalRuntimeFailureReason(message, sdkErrorType, sdkErrorCode string, httpStatus *int) *ExternalRuntimeFailureReason {
	return &ExternalRuntimeFailureReason{
		Message:      message,
		SDKErrorType: sdkErrorType,
		SDKErrorCode: sdkErrorCode,
		HTTPStatus:   httpStatus,
	}
}

// NewExternalRuntimeFailure 构造终局失败载荷；Type 固定判别值（Python Literal 默认）、
// UserActionRequired 按类别闭合集推导、Reason 为 nil 时回落默认原因（Python default_factory）。
func NewExternalRuntimeFailure(
	failureID, teamName, memberName, agentKind, model string,
	phase ExternalRuntimePhase,
	category ExternalRuntimeFailureCategory,
	summary, suggestedAction string,
	cliPath *string, reason *ExternalRuntimeFailureReason,
) *ExternalRuntimeFailure {
	if reason == nil {
		reason = NewExternalRuntimeFailureReason("", "", "", nil)
	}
	return &ExternalRuntimeFailure{
		Type:               externalRuntimeFailedType,
		FailureID:          failureID,
		TeamName:           teamName,
		MemberName:         memberName,
		AgentKind:          agentKind,
		Model:              model,
		CLIPath:            cliPath,
		Phase:              phase,
		Category:           category,
		UserActionRequired: UserActionRequired(category),
		Summary:            summary,
		SuggestedAction:    suggestedAction,
		Reason:             reason,
	}
}
