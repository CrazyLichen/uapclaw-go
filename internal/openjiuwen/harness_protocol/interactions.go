package harness_protocol

import (
	"context"
	"fmt"
	"math"
)

// 本文件对应 Python openjiuwen/harness_protocol/interactions.py（359 行）：
// 第三方 harness 在活跃轮内向宿主发起、必须等待回答的控制面契约。
//
// 与 events 的关键区别：interaction 是「harness 原地阻塞等宿主」的同步应答，
// 绝不能走事件流（SPEC.md:185-187：否则死锁、审批丢失、多消费者竞态）。
//
// 本文件只定义契约与构造期校验，不含调用点 —— 真实消费方为 15.5 provider 基座
// （Python: harness_providers/base.py:724-739 _request_interaction）。

// ──────────────────────────── 结构体 ────────────────────────────

// ToolApprovalRequest 请求授权 provider 执行工具调用。
// Python: interactions.py:66-88 ToolApprovalRequest
type ToolApprovalRequest struct {
	// RequestID 请求标识（cycle 内唯一）
	RequestID string
	// CallID 工具调用标识
	CallID string
	// ToolName 工具名
	ToolName string
	// Arguments 调用参数（已冻结）
	Arguments JsonObject
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// Title 展示标题（nil = 未提供）
	Title *string
	// Description 展示描述（nil = 未提供）
	Description *string
	// Reason 请求原因（nil = 未提供）
	Reason *string
	// DeadlineAt Unix 秒应答截止时刻（nil = 未提供）
	DeadlineAt *float64
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// ToolApprovalRequestOptions ToolApprovalRequest 的可选字段。
// Python: interactions.py:73-80 dataclass 默认值
type ToolApprovalRequestOptions struct {
	// Arguments 调用参数
	Arguments JsonObject
	// ProviderSessionID provider 会话
	ProviderSessionID *string
	// TurnID 所属轮次
	TurnID *string
	// Title 展示标题
	Title *string
	// Description 展示描述
	Description *string
	// Reason 请求原因
	Reason *string
	// DeadlineAt Unix 秒应答截止时刻
	DeadlineAt *float64
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// UserInputRequest 轮次活跃期间请求用户补充输入。
// Python: interactions.py:90-108 UserInputRequest
type UserInputRequest struct {
	// RequestID 请求标识
	RequestID string
	// Prompt 提问文案
	Prompt string
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// Choices 可选答案（空表示自由输入）
	Choices []string
	// DeadlineAt Unix 秒应答截止时刻（nil = 未提供）
	DeadlineAt *float64
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// UserInputRequestOptions UserInputRequest 的可选字段。
// Python: interactions.py:96-100 dataclass 默认值
type UserInputRequestOptions struct {
	// ProviderSessionID provider 会话
	ProviderSessionID *string
	// TurnID 所属轮次
	TurnID *string
	// Choices 可选答案
	Choices []string
	// DeadlineAt Unix 秒应答截止时刻
	DeadlineAt *float64
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// McpElicitationRequest 为 MCP elicitation 请求结构化用户输入。
// Python: interactions.py:110-130 McpElicitationRequest
type McpElicitationRequest struct {
	// RequestID 请求标识
	RequestID string
	// ServerName MCP 服务名
	ServerName string
	// Prompt 提问文案
	Prompt string
	// Schema 期望输入的结构（nil = 未提供）
	Schema JsonObject
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// DeadlineAt Unix 秒应答截止时刻（nil = 未提供）
	DeadlineAt *float64
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// McpElicitationRequestOptions McpElicitationRequest 的可选字段。
// Python: interactions.py:117-121 dataclass 默认值
type McpElicitationRequestOptions struct {
	// Schema 期望输入的结构
	Schema JsonObject
	// ProviderSessionID provider 会话
	ProviderSessionID *string
	// TurnID 所属轮次
	TurnID *string
	// DeadlineAt Unix 秒应答截止时刻
	DeadlineAt *float64
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// DynamicToolCallRequest 把 provider 发起的动态工具调用委托给宿主。
// Python: interactions.py:132-152 DynamicToolCallRequest
type DynamicToolCallRequest struct {
	// RequestID 请求标识
	RequestID string
	// CallID 工具调用标识
	CallID string
	// ToolName 工具名
	ToolName string
	// Arguments 调用参数（已冻结）
	Arguments JsonObject
	// Namespace 工具命名空间（nil = 未提供）
	Namespace *string
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// DeadlineAt Unix 秒应答截止时刻（nil = 未提供）
	DeadlineAt *float64
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// DynamicToolCallRequestOptions DynamicToolCallRequest 的可选字段。
// Python: interactions.py:139-144 dataclass 默认值
type DynamicToolCallRequestOptions struct {
	// Arguments 调用参数
	Arguments JsonObject
	// Namespace 工具命名空间
	Namespace *string
	// ProviderSessionID provider 会话
	ProviderSessionID *string
	// TurnID 所属轮次
	TurnID *string
	// DeadlineAt Unix 秒应答截止时刻
	DeadlineAt *float64
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// ProviderInteractionRequest 未被共享交互类型覆盖的 provider 扩展请求。
// Python: interactions.py:154-172 ProviderInteractionRequest
type ProviderInteractionRequest struct {
	// RequestID 请求标识
	RequestID string
	// Provider provider 名
	Provider string
	// RequestType provider 私有请求类型
	RequestType string
	// SchemaVersion provider 私有 schema 版本
	SchemaVersion string
	// Payload provider 私有载荷（已冻结）
	Payload JsonObject
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// DeadlineAt Unix 秒应答截止时刻（nil = 未提供）
	DeadlineAt *float64
}

// ProviderInteractionRequestOptions ProviderInteractionRequest 的可选字段。
// Python: interactions.py:162-165 dataclass 默认值
type ProviderInteractionRequestOptions struct {
	// Payload provider 私有载荷
	Payload JsonObject
	// ProviderSessionID provider 会话
	ProviderSessionID *string
	// TurnID 所属轮次
	TurnID *string
	// DeadlineAt Unix 秒应答截止时刻
	DeadlineAt *float64
}

// ToolApprovalResponse 对 ToolApprovalRequest 的决策应答。
// Python: interactions.py:174-190 ToolApprovalResponse
type ToolApprovalResponse struct {
	// RequestID 对应请求标识
	RequestID string
	// Decision 审批决策
	Decision ToolApprovalDecision
	// UpdatedArguments 改写后的参数（nil = 未提供）
	UpdatedArguments JsonObject
	// Reason 决策原因（nil = 未提供）
	Reason *string
	// Interrupt 是否中断（Python 默认 False）
	Interrupt bool
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// ToolApprovalResponseOptions ToolApprovalResponse 的可选字段。
// Python: interactions.py:180-183 dataclass 默认值
type ToolApprovalResponseOptions struct {
	// UpdatedArguments 改写后的参数
	UpdatedArguments JsonObject
	// Reason 决策原因
	Reason *string
	// Interrupt 是否中断
	Interrupt bool
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// UserInputResponse 对 UserInputRequest 的应答。
// Python: interactions.py:192-205 UserInputResponse
type UserInputResponse struct {
	// RequestID 对应请求标识
	RequestID string
	// Status 完成状态
	Status InteractionResponseStatus
	// Content 用户回答（已冻结；nil 为合法 JSON null）
	Content JsonValue
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// UserInputResponseOptions UserInputResponse 的可选字段。
// Python: interactions.py:198-199 dataclass 默认值
type UserInputResponseOptions struct {
	// Content 用户回答
	Content JsonValue
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// McpElicitationResponse 对 McpElicitationRequest 的应答。
// Python: interactions.py:207-220 McpElicitationResponse
type McpElicitationResponse struct {
	// RequestID 对应请求标识
	RequestID string
	// Status 完成状态
	Status InteractionResponseStatus
	// Values 结构化回答（已冻结）
	Values JsonObject
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// McpElicitationResponseOptions McpElicitationResponse 的可选字段。
// Python: interactions.py:213-214 dataclass 默认值
type McpElicitationResponseOptions struct {
	// Values 结构化回答
	Values JsonObject
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// DynamicToolCallResponse 对 DynamicToolCallRequest 的应答。
// Python: interactions.py:222-237 DynamicToolCallResponse
type DynamicToolCallResponse struct {
	// RequestID 对应请求标识
	RequestID string
	// Status 完成状态
	Status InteractionResponseStatus
	// Result 工具结果（已冻结；nil 为合法 JSON null）
	Result JsonValue
	// IsError 结果是否为错误
	IsError bool
	// ErrorMessage 错误文案（nil = 未提供）
	ErrorMessage *string
	// ProviderData provider 附加数据（已冻结）
	ProviderData JsonObject
}

// DynamicToolCallResponseOptions DynamicToolCallResponse 的可选字段。
// Python: interactions.py:228-231 dataclass 默认值
type DynamicToolCallResponseOptions struct {
	// Result 工具结果
	Result JsonValue
	// IsError 结果是否为错误
	IsError bool
	// ErrorMessage 错误文案
	ErrorMessage *string
	// ProviderData provider 附加数据
	ProviderData JsonObject
}

// ProviderInteractionResponse 对 ProviderInteractionRequest 的应答。
// Python: interactions.py:239-250 ProviderInteractionResponse
type ProviderInteractionResponse struct {
	// RequestID 对应请求标识
	RequestID string
	// Status 完成状态
	Status InteractionResponseStatus
	// Payload provider 私有载荷（已冻结）
	Payload JsonObject
}

// ProviderInteractionResponseOptions ProviderInteractionResponse 的可选字段。
// Python: interactions.py:245 dataclass 默认值
type ProviderInteractionResponseOptions struct {
	// Payload provider 私有载荷
	Payload JsonObject
}

// ──────────────────────────── 枚举 ────────────────────────────

// InteractionResponseStatus 非审批类交互的完成状态。
// Python: interactions.py:39-45 InteractionResponseStatus（4 项）
type InteractionResponseStatus string

const (
	// InteractionResponseStatusCompleted 已完成
	InteractionResponseStatusCompleted InteractionResponseStatus = "completed"
	// InteractionResponseStatusDeclined 已拒绝
	InteractionResponseStatusDeclined InteractionResponseStatus = "declined"
	// InteractionResponseStatusCancelled 已取消
	InteractionResponseStatusCancelled InteractionResponseStatus = "cancelled"
	// InteractionResponseStatusFailed 已失败
	InteractionResponseStatusFailed InteractionResponseStatus = "failed"
)

// ToolApprovalDecision 宿主对 provider 请求的工具执行的决策。
// Python: interactions.py:48-54 ToolApprovalDecision（4 项）
type ToolApprovalDecision string

const (
	// ToolApprovalDecisionAllow 允许本次执行
	ToolApprovalDecisionAllow ToolApprovalDecision = "allow"
	// ToolApprovalDecisionAllowForSession 本会话内始终允许
	ToolApprovalDecisionAllowForSession ToolApprovalDecision = "allow_for_session"
	// ToolApprovalDecisionDeny 拒绝执行
	ToolApprovalDecisionDeny ToolApprovalDecision = "deny"
	// ToolApprovalDecisionAbort 拒绝并中止整轮
	ToolApprovalDecisionAbort ToolApprovalDecision = "abort"
)

// InteractionCancelReason provider 到宿主交互被取消的原因。
// Python: interactions.py:57-63 InteractionCancelReason（4 项）
type InteractionCancelReason string

const (
	// InteractionCancelReasonProviderWithdrew provider 主动撤销
	InteractionCancelReasonProviderWithdrew InteractionCancelReason = "provider_withdrew"
	// InteractionCancelReasonTurnAborted 轮次中止
	InteractionCancelReasonTurnAborted InteractionCancelReason = "turn_aborted"
	// InteractionCancelReasonHarnessStopped harness 停止
	InteractionCancelReasonHarnessStopped InteractionCancelReason = "harness_stopped"
	// InteractionCancelReasonDeadlineExceeded 超过 deadline
	InteractionCancelReasonDeadlineExceeded InteractionCancelReason = "deadline_exceeded"
)

// ──────────────────────────── 结构体（接口定义）────────────────────────────

// HarnessInteractionRequest provider 在活跃轮内发起、必须等待回答的请求。
// Python: interactions.py:252-254 HarnessInteractionRequest（TypeAlias，5 分支 union）
type HarnessInteractionRequest interface {
	// requestKind 返回请求的判别字面量（供 ValidateInteractionResponse 判型；交互不过 wire codec）
	requestKind() string
	// expectedResponseKind 返回与之配对的应答 discriminator。
	// ⚠️ Go 专有成员：Python 用 _EXPECTED_RESPONSE_TYPE 字典表达
	// （interactions.py:264-270），Go 改为挂在请求类型上（Q5b 决策 3-a）。
	expectedResponseKind() string
}

// HarnessInteractionResponse 宿主对请求的应答。
// Python: interactions.py:255-261 HarnessInteractionResponse（TypeAlias，5 分支 union）
type HarnessInteractionResponse interface {
	// responseKind 返回应答的判别字面量（供 ValidateInteractionResponse 判型；交互不过 wire codec）
	responseKind() string
}

// HarnessInteractionHandler 活跃轮内请求/应答交互的宿主服务。
//
// 实现必须在提供 DeadlineAt 时于其前返回与请求配对的应答类型与相同 RequestID。
// Cancel 幂等，释放该请求挂起的宿主 UI 或策略操作。
// Harness 必须在 abort 其轮次或完成 stop 之前取消全部挂起请求。
// Python: interactions.py:291-338 HarnessInteractionHandler（handle + cancel）
type HarnessInteractionHandler interface {
	// Handle 等待并返回宿主对 request 的应答
	Handle(ctx context.Context, request HarnessInteractionRequest) (HarnessInteractionResponse, error)
	// Cancel 在 provider 撤销挂起交互时取消它（幂等）
	Cancel(ctx context.Context, requestID string, opts ...CancelOption) error
}

// CancelOption Cancel 的可选参数。
// Python: interactions.py:331-338 的 keyword-only 参数
// reason（默认 InteractionCancelReason.PROVIDER_WITHDREW）。
// Go 无默认参数与关键字参数，改用可省略的函数式选项。
type CancelOption func(reason *InteractionCancelReason)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolApprovalRequest 创建工具审批请求。
// Python: interactions.py:82-87 ToolApprovalRequest.__post_init__
func NewToolApprovalRequest(
	requestID string,
	callID string,
	toolName string,
	opts ToolApprovalRequestOptions,
) (ToolApprovalRequest, error) {
	// 步骤 1：共用校验（request_id / provider_session_id / turn_id / deadline_at）
	if err := validateRequestCommon(requestID, opts.ProviderSessionID, opts.TurnID, opts.DeadlineAt); err != nil {
		return ToolApprovalRequest{}, err
	}
	// 步骤 2：call_id 与 tool_name 非空
	if callID == "" || toolName == "" {
		return ToolApprovalRequest{}, &HarnessProtocolError{Msg: "tool approval call_id and tool_name must not be empty"}
	}
	// 步骤 3：冻结 arguments（Python default_factory=dict，故 nil 归一化为空对象）
	args, err := freezeJsonObjectOrDefault(opts.Arguments)
	if err != nil {
		return ToolApprovalRequest{}, err
	}
	// 步骤 4：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return ToolApprovalRequest{}, err
	}
	return ToolApprovalRequest{
		RequestID:         requestID,
		CallID:            callID,
		ToolName:          toolName,
		Arguments:         args,
		ProviderSessionID: opts.ProviderSessionID,
		TurnID:            opts.TurnID,
		Title:             opts.Title,
		Description:       opts.Description,
		Reason:            opts.Reason,
		DeadlineAt:        opts.DeadlineAt,
		ProviderData:      providerData,
	}, nil
}

// NewUserInputRequest 创建用户输入请求。
// Python: interactions.py:102-107 UserInputRequest.__post_init__
func NewUserInputRequest(requestID string, prompt string, opts UserInputRequestOptions) (UserInputRequest, error) {
	// 步骤 1：共用校验
	if err := validateRequestCommon(requestID, opts.ProviderSessionID, opts.TurnID, opts.DeadlineAt); err != nil {
		return UserInputRequest{}, err
	}
	// 步骤 2：prompt 非空
	if prompt == "" {
		return UserInputRequest{}, &HarnessProtocolError{Msg: "user input prompt must not be empty"}
	}
	// 步骤 3：choices 拷贝为独立切片（Go 无 tuple）
	choices := append([]string(nil), opts.Choices...)
	// 步骤 4：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return UserInputRequest{}, err
	}
	return UserInputRequest{
		RequestID:         requestID,
		Prompt:            prompt,
		ProviderSessionID: opts.ProviderSessionID,
		TurnID:            opts.TurnID,
		Choices:           choices,
		DeadlineAt:        opts.DeadlineAt,
		ProviderData:      providerData,
	}, nil
}

// NewMcpElicitationRequest 创建 MCP elicitation 请求。
// Python: interactions.py:123-129 McpElicitationRequest.__post_init__
func NewMcpElicitationRequest(
	requestID string,
	serverName string,
	prompt string,
	opts McpElicitationRequestOptions,
) (McpElicitationRequest, error) {
	// 步骤 1：共用校验
	if err := validateRequestCommon(requestID, opts.ProviderSessionID, opts.TurnID, opts.DeadlineAt); err != nil {
		return McpElicitationRequest{}, err
	}
	// 步骤 2：server_name 与 prompt 非空
	if serverName == "" || prompt == "" {
		return McpElicitationRequest{}, &HarnessProtocolError{
			Msg: "MCP elicitation server_name and prompt must not be empty",
		}
	}
	// 步骤 3：schema 非 nil 才冻结（Python: if self.schema is not None）
	var schema JsonObject
	if opts.Schema != nil {
		frozen, err := FreezeJsonObject(opts.Schema)
		if err != nil {
			return McpElicitationRequest{}, err
		}
		schema = frozen
	}
	// 步骤 4：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return McpElicitationRequest{}, err
	}
	return McpElicitationRequest{
		RequestID:         requestID,
		ServerName:        serverName,
		Prompt:            prompt,
		Schema:            schema,
		ProviderSessionID: opts.ProviderSessionID,
		TurnID:            opts.TurnID,
		DeadlineAt:        opts.DeadlineAt,
		ProviderData:      providerData,
	}, nil
}

// NewDynamicToolCallRequest 创建动态工具调用请求。
// Python: interactions.py:146-151 DynamicToolCallRequest.__post_init__
func NewDynamicToolCallRequest(
	requestID string,
	callID string,
	toolName string,
	opts DynamicToolCallRequestOptions,
) (DynamicToolCallRequest, error) {
	// 步骤 1：共用校验
	if err := validateRequestCommon(requestID, opts.ProviderSessionID, opts.TurnID, opts.DeadlineAt); err != nil {
		return DynamicToolCallRequest{}, err
	}
	// 步骤 2：call_id 与 tool_name 非空
	if callID == "" || toolName == "" {
		return DynamicToolCallRequest{}, &HarnessProtocolError{
			Msg: "dynamic tool call_id and tool_name must not be empty",
		}
	}
	// 步骤 3：冻结 arguments
	args, err := freezeJsonObjectOrDefault(opts.Arguments)
	if err != nil {
		return DynamicToolCallRequest{}, err
	}
	// 步骤 4：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return DynamicToolCallRequest{}, err
	}
	return DynamicToolCallRequest{
		RequestID:         requestID,
		CallID:            callID,
		ToolName:          toolName,
		Arguments:         args,
		Namespace:         opts.Namespace,
		ProviderSessionID: opts.ProviderSessionID,
		TurnID:            opts.TurnID,
		DeadlineAt:        opts.DeadlineAt,
		ProviderData:      providerData,
	}, nil
}

// NewProviderInteractionRequest 创建 provider 扩展交互请求。
// Python: interactions.py:167-171 ProviderInteractionRequest.__post_init__
func NewProviderInteractionRequest(
	requestID string,
	provider string,
	requestType string,
	schemaVersion string,
	opts ProviderInteractionRequestOptions,
) (ProviderInteractionRequest, error) {
	// 步骤 1：共用校验
	if err := validateRequestCommon(requestID, opts.ProviderSessionID, opts.TurnID, opts.DeadlineAt); err != nil {
		return ProviderInteractionRequest{}, err
	}
	// 步骤 2：provider / request_type / schema_version 非空
	if provider == "" || requestType == "" || schemaVersion == "" {
		return ProviderInteractionRequest{}, &HarnessProtocolError{
			Msg: "provider interaction namespace, type, and version must not be empty",
		}
	}
	// 步骤 3：冻结 payload
	payload, err := freezeJsonObjectOrDefault(opts.Payload)
	if err != nil {
		return ProviderInteractionRequest{}, err
	}
	return ProviderInteractionRequest{
		RequestID:         requestID,
		Provider:          provider,
		RequestType:       requestType,
		SchemaVersion:     schemaVersion,
		Payload:           payload,
		ProviderSessionID: opts.ProviderSessionID,
		TurnID:            opts.TurnID,
		DeadlineAt:        opts.DeadlineAt,
	}, nil
}

// NewToolApprovalResponse 创建工具审批应答。
// Python: interactions.py:185-189 ToolApprovalResponse.__post_init__
func NewToolApprovalResponse(
	requestID string,
	decision ToolApprovalDecision,
	opts ToolApprovalResponseOptions,
) (ToolApprovalResponse, error) {
	// 步骤 1：共用校验（request_id 非空）
	if err := validateResponseID(requestID); err != nil {
		return ToolApprovalResponse{}, err
	}
	// 步骤 2：updated_arguments 非 nil 才冻结
	var updated JsonObject
	if opts.UpdatedArguments != nil {
		frozen, err := FreezeJsonObject(opts.UpdatedArguments)
		if err != nil {
			return ToolApprovalResponse{}, err
		}
		updated = frozen
	}
	// 步骤 3：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return ToolApprovalResponse{}, err
	}
	return ToolApprovalResponse{
		RequestID:        requestID,
		Decision:         decision,
		UpdatedArguments: updated,
		Reason:           opts.Reason,
		Interrupt:        opts.Interrupt,
		ProviderData:     providerData,
	}, nil
}

// NewUserInputResponse 创建用户输入应答。
// Python: interactions.py:201-204 UserInputResponse.__post_init__
func NewUserInputResponse(
	requestID string,
	status InteractionResponseStatus,
	opts UserInputResponseOptions,
) (UserInputResponse, error) {
	// 步骤 1：共用校验
	if err := validateResponseID(requestID); err != nil {
		return UserInputResponse{}, err
	}
	// 步骤 2：冻结 content（JsonValue，nil 为合法 JSON null）
	content, err := FreezeJsonValue(opts.Content)
	if err != nil {
		return UserInputResponse{}, err
	}
	// 步骤 3：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return UserInputResponse{}, err
	}
	return UserInputResponse{
		RequestID:    requestID,
		Status:       status,
		Content:      content,
		ProviderData: providerData,
	}, nil
}

// NewMcpElicitationResponse 创建 MCP elicitation 应答。
// Python: interactions.py:216-219 McpElicitationResponse.__post_init__
func NewMcpElicitationResponse(
	requestID string,
	status InteractionResponseStatus,
	opts McpElicitationResponseOptions,
) (McpElicitationResponse, error) {
	// 步骤 1：共用校验
	if err := validateResponseID(requestID); err != nil {
		return McpElicitationResponse{}, err
	}
	// 步骤 2：冻结 values
	values, err := freezeJsonObjectOrDefault(opts.Values)
	if err != nil {
		return McpElicitationResponse{}, err
	}
	// 步骤 3：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return McpElicitationResponse{}, err
	}
	return McpElicitationResponse{
		RequestID:    requestID,
		Status:       status,
		Values:       values,
		ProviderData: providerData,
	}, nil
}

// NewDynamicToolCallResponse 创建动态工具调用应答。
// Python: interactions.py:233-236 DynamicToolCallResponse.__post_init__
func NewDynamicToolCallResponse(
	requestID string,
	status InteractionResponseStatus,
	opts DynamicToolCallResponseOptions,
) (DynamicToolCallResponse, error) {
	// 步骤 1：共用校验
	if err := validateResponseID(requestID); err != nil {
		return DynamicToolCallResponse{}, err
	}
	// 步骤 2：冻结 result
	result, err := FreezeJsonValue(opts.Result)
	if err != nil {
		return DynamicToolCallResponse{}, err
	}
	// 步骤 3：冻结 provider_data
	providerData, err := freezeJsonObjectOrDefault(opts.ProviderData)
	if err != nil {
		return DynamicToolCallResponse{}, err
	}
	return DynamicToolCallResponse{
		RequestID:    requestID,
		Status:       status,
		Result:       result,
		IsError:      opts.IsError,
		ErrorMessage: opts.ErrorMessage,
		ProviderData: providerData,
	}, nil
}

// NewProviderInteractionResponse 创建 provider 扩展交互应答。
// Python: interactions.py:247-249 ProviderInteractionResponse.__post_init__
func NewProviderInteractionResponse(
	requestID string,
	status InteractionResponseStatus,
	opts ProviderInteractionResponseOptions,
) (ProviderInteractionResponse, error) {
	// 步骤 1：共用校验
	if err := validateResponseID(requestID); err != nil {
		return ProviderInteractionResponse{}, err
	}
	// 步骤 2：冻结 payload
	payload, err := freezeJsonObjectOrDefault(opts.Payload)
	if err != nil {
		return ProviderInteractionResponse{}, err
	}
	return ProviderInteractionResponse{RequestID: requestID, Status: status, Payload: payload}, nil
}

// NewCancelOption 构造覆盖默认取消原因的选项。
// Python: interactions.py:335 reason 参数的显式取值。
// 零参 Cancel 等价于 NewCancelOption(InteractionCancelReasonProviderWithdrew)。
func NewCancelOption(reason InteractionCancelReason) CancelOption {
	return func(dst *InteractionCancelReason) {
		*dst = reason
	}
}

// ValidateInteractionResponse 校验请求身份与它要求的应答形状。
// Python: interactions.py:273-288 validate_interaction_response（2 步）
//
// Q5 决策改选 3 + Q5b 决策 3-a：应答配对关系由请求自身的
// expectedResponseKind() 声明，比较 kind 字符串。
//
// nil 入参：Python 侧传 None 会在 `response.request_id` 处抛
// `AttributeError: 'NoneType' object has no attribute 'request_id'`
// （2026-11-11 实测）。Go 侧不加守卫时是 nil 接口方法调用 panic，
// 排查困难且不可被 errors.Is 捕获；故此处前置 nil 判断并统一返回
// HarnessProtocolError，**语义等价**（同为异常拒绝，外部判断方式一致）。
func ValidateInteractionResponse(
	request HarnessInteractionRequest,
	response HarnessInteractionResponse,
) (HarnessInteractionResponse, error) {
	// 步骤 0（nil 守卫，Python AttributeError 的 error 化等价物）
	if request == nil {
		return nil, &HarnessProtocolError{Msg: "interaction request must not be nil"}
	}
	if response == nil {
		return nil, &HarnessProtocolError{Msg: "interaction response must not be nil"}
	}
	reqID := requestIDOf(request)
	respID := responseIDOf(response)
	// 步骤 1：request_id 必须一致（Python :279-282）
	if respID != reqID {
		return nil, &HarnessProtocolError{
			Msg: fmt.Sprintf("interaction response id %q does not match request %q", respID, reqID),
		}
	}
	// 步骤 2：应答 kind 必须是请求声明的期望 kind（Python :283-288）
	expected := request.expectedResponseKind()
	if got := response.responseKind(); got != expected {
		return nil, &HarnessProtocolError{
			Msg: fmt.Sprintf("%s requires %s, got %s", requestKindName(request), expected, got),
		}
	}
	return response, nil
}

// requestKind 返回线格式 discriminator。
func (r ToolApprovalRequest) requestKind() string { return "tool_approval" }

// expectedResponseKind 返回配对应答的 discriminator。
func (r ToolApprovalRequest) expectedResponseKind() string { return "tool_approval_response" }

// requestKind 返回线格式 discriminator。
func (r UserInputRequest) requestKind() string { return "user_input" }

// expectedResponseKind 返回配对应答的 discriminator。
func (r UserInputRequest) expectedResponseKind() string { return "user_input_response" }

// requestKind 返回线格式 discriminator。
func (r McpElicitationRequest) requestKind() string { return "mcp_elicitation" }

// expectedResponseKind 返回配对应答的 discriminator。
func (r McpElicitationRequest) expectedResponseKind() string { return "mcp_elicitation_response" }

// requestKind 返回线格式 discriminator。
func (r DynamicToolCallRequest) requestKind() string { return "dynamic_tool_call" }

// expectedResponseKind 返回配对应答的 discriminator。
func (r DynamicToolCallRequest) expectedResponseKind() string { return "dynamic_tool_call_response" }

// requestKind 返回线格式 discriminator。
func (r ProviderInteractionRequest) requestKind() string { return "provider_interaction" }

// expectedResponseKind 返回配对应答的 discriminator。
func (r ProviderInteractionRequest) expectedResponseKind() string {
	return "provider_interaction_response"
}

// responseKind 返回线格式 discriminator。
func (r ToolApprovalResponse) responseKind() string { return "tool_approval_response" }

// responseKind 返回线格式 discriminator。
func (r UserInputResponse) responseKind() string { return "user_input_response" }

// responseKind 返回线格式 discriminator。
func (r McpElicitationResponse) responseKind() string { return "mcp_elicitation_response" }

// responseKind 返回线格式 discriminator。
func (r DynamicToolCallResponse) responseKind() string { return "dynamic_tool_call_response" }

// responseKind 返回线格式 discriminator。
func (r ProviderInteractionResponse) responseKind() string { return "provider_interaction_response" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateRequestCommon 五个请求类型共用的构造期校验。
// Python: interactions.py:22-31 _validate_request_common（3 步，顺序不可调）
//
// Q7 决策 A 的核心：provider_session_id / turn_id 用 *string，
// nil = 未提供（Python None，放行），"" = 显式空串（拒绝）。
func validateRequestCommon(
	requestID string,
	providerSessionID *string,
	turnID *string,
	deadlineAt *float64,
) error {
	// 步骤 1：request_id 非空
	if requestID == "" {
		return &HarnessProtocolError{Msg: "interaction request_id must not be empty"}
	}
	// 步骤 2：provider_session_id / turn_id 若给了值则不得为空串
	if providerSessionID != nil && *providerSessionID == "" {
		return &HarnessProtocolError{Msg: "interaction provider_session_id must not be empty"}
	}
	if turnID != nil && *turnID == "" {
		return &HarnessProtocolError{Msg: "interaction turn_id must not be empty"}
	}
	// 步骤 3：deadline_at 给了值须为有限非负 Unix 秒
	if deadlineAt != nil {
		d := *deadlineAt
		if math.IsNaN(d) || math.IsInf(d, 0) || d < 0 {
			return &HarnessProtocolError{Msg: "interaction deadline_at must be a finite Unix timestamp"}
		}
	}
	return nil
}

// validateResponseID 五个应答类型共用的构造期校验。
// Python: interactions.py:34-36 _validate_response_id
func validateResponseID(requestID string) error {
	if requestID == "" {
		return &HarnessProtocolError{Msg: "interaction response request_id must not be empty"}
	}
	return nil
}

// freezeJsonObjectOrDefault 冻结 JSON 对象，nil 归一化为空对象。
// Python 等价物：dataclass field(default_factory=dict)。
// Python 侧 default_factory 使调用方永不传 None，而 Go Options 零值是 nil；
// 若不归一化，FreezeJsonObject(nil) 会返回 "must be a mapping" 错误。
func freezeJsonObjectOrDefault(value JsonObject) (JsonObject, error) {
	if value == nil {
		value = JsonObject{}
	}
	return FreezeJsonObject(value)
}

// requestIDOf 取请求标识，值形态与指针形态都识别。
// Python 侧用 getattr(request, "request_id") 对两种形态一视同仁；
// Go 对值/指针是不同动态类型，只列一种会静默落入 default（15.2 B11 教训）。
func requestIDOf(request HarnessInteractionRequest) string {
	switch r := request.(type) {
	case ToolApprovalRequest:
		return r.RequestID
	case *ToolApprovalRequest:
		return r.RequestID
	case UserInputRequest:
		return r.RequestID
	case *UserInputRequest:
		return r.RequestID
	case McpElicitationRequest:
		return r.RequestID
	case *McpElicitationRequest:
		return r.RequestID
	case DynamicToolCallRequest:
		return r.RequestID
	case *DynamicToolCallRequest:
		return r.RequestID
	case ProviderInteractionRequest:
		return r.RequestID
	case *ProviderInteractionRequest:
		return r.RequestID
	default:
		return ""
	}
}

// responseIDOf 取应答标识，值形态与指针形态都识别。
func responseIDOf(response HarnessInteractionResponse) string {
	switch r := response.(type) {
	case ToolApprovalResponse:
		return r.RequestID
	case *ToolApprovalResponse:
		return r.RequestID
	case UserInputResponse:
		return r.RequestID
	case *UserInputResponse:
		return r.RequestID
	case McpElicitationResponse:
		return r.RequestID
	case *McpElicitationResponse:
		return r.RequestID
	case DynamicToolCallResponse:
		return r.RequestID
	case *DynamicToolCallResponse:
		return r.RequestID
	case ProviderInteractionResponse:
		return r.RequestID
	case *ProviderInteractionResponse:
		return r.RequestID
	default:
		return ""
	}
}

// requestKindName 返回请求的 Go 类型名，用于错误文案。
// Python: f"{type(request).__name__}"（interactions.py:285-286）。
func requestKindName(request HarnessInteractionRequest) string {
	switch request.(type) {
	case ToolApprovalRequest, *ToolApprovalRequest:
		return "ToolApprovalRequest"
	case UserInputRequest, *UserInputRequest:
		return "UserInputRequest"
	case McpElicitationRequest, *McpElicitationRequest:
		return "McpElicitationRequest"
	case DynamicToolCallRequest, *DynamicToolCallRequest:
		return "DynamicToolCallRequest"
	case ProviderInteractionRequest, *ProviderInteractionRequest:
		return "ProviderInteractionRequest"
	default:
		return "unsupported interaction request"
	}
}
