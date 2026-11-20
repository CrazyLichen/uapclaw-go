package harness_protocol

import "context"

// 本文件对应 Python openjiuwen/harness_protocol/hooks.py（163 行）：
// harness 执行边界上宿主策略扩展的控制面契约。
// 本文件只定义契约与构造期校验，不含任何调用点 —— 上游 harness_providers/
// 内亦无 hooks 调用（HostCapability.HOOKS 全仓 0 命中），首个消费方预计为
// 15.5 provider 基座或 15.7 claudecode 的 permission prompt 场景。
// 注意：events 的 HookObservedEvent 只是钩子调用的观测镜像，不得用于决策。

// ──────────────────────────── 结构体 ────────────────────────────

// BeforePromptContext 输入到达外部 agent 之前提供的上下文。
// Python: hooks.py:30-38 BeforePromptContext
type BeforePromptContext struct {
	// AgentName agent 名
	AgentName string
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// Prompt 待送入模型的输入
	Prompt string
}

// BeforePromptResult 钩子返回的提示词改写或停轮决策。
// Python: hooks.py:40-48 BeforePromptResult
type BeforePromptResult struct {
	// Prompt 改写后的提示词
	Prompt string
	// ContinueExecution 是否继续执行（Python 默认 True）
	ContinueExecution bool
	// Reason 停轮原因（nil = 未提供）
	Reason *string
	// AdditionalContext 追加上下文（nil = 未提供）
	AdditionalContext *string
}

// BeforePromptResultOptions BeforePromptResult 的可选字段。
// Python: hooks.py:45-47 dataclass 默认值
type BeforePromptResultOptions struct {
	// Reason 停轮原因
	Reason *string
	// AdditionalContext 追加上下文
	AdditionalContext *string
}

// BeforeToolContext 外部 agent 调用工具之前提供的上下文。
// Python: hooks.py:50-63 BeforeToolContext（6 必填，0 默认）
type BeforeToolContext struct {
	// AgentName agent 名
	AgentName string
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// CallID 工具调用标识
	CallID string
	// ToolName 工具名
	ToolName string
	// Arguments 调用参数（已冻结）
	Arguments JsonObject
}

// ToolDecision 工具执行前返回的权限与可选参数改写。
// Python: hooks.py:65-81 ToolDecision（1 必填 decision + 3 默认）
type ToolDecision struct {
	// Decision 决策种类
	Decision ToolDecisionKind
	// Reason 决策原因（nil = 未提供）
	Reason *string
	// UpdatedArguments 改写后的参数（仅 REWRITE 可用；nil = 未提供）
	UpdatedArguments JsonObject
	// AdditionalContext 追加上下文（nil = 未提供）
	AdditionalContext *string
}

// ToolDecisionOptions ToolDecision 的可选字段。
// Python: hooks.py:70-72 dataclass 默认值
type ToolDecisionOptions struct {
	// Reason 决策原因
	Reason *string
	// UpdatedArguments 改写后的参数（仅 REWRITE 可用）
	UpdatedArguments JsonObject
	// AdditionalContext 追加上下文
	AdditionalContext *string
}

// AfterToolContext 工具调用成功或失败之后提供的上下文。
// Python: hooks.py:83-98 AfterToolContext（6 必填 + 2 默认）
type AfterToolContext struct {
	// AgentName agent 名
	AgentName string
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// CallID 工具调用标识
	CallID string
	// ToolName 工具名
	ToolName string
	// Arguments 调用参数（已冻结）
	Arguments JsonObject
	// Result 工具结果（已冻结；Python 默认 None）
	Result JsonValue
	// Error 错误文案（nil = 无错误）
	Error *string
}

// AfterToolContextOptions AfterToolContext 的可选字段。
// Python: hooks.py:93-94 dataclass 默认值
type AfterToolContextOptions struct {
	// Result 工具结果
	Result JsonValue
	// Error 错误文案
	Error *string
}

// AfterToolResult 钩子返回的可选工具结果改写与追加上下文。
// Python: hooks.py:101-110 AfterToolResult（3 个字段全有默认值）
type AfterToolResult struct {
	// ReplaceResult 是否替换工具结果
	ReplaceResult bool
	// UpdatedResult 替换后的结果（已冻结）
	UpdatedResult JsonValue
	// AdditionalContext 追加上下文（nil = 未提供）
	AdditionalContext *string
}

// AfterToolResultOptions AfterToolResult 的全部字段。
// Python: hooks.py:105-107 dataclass 默认值（3 个字段全可选，
// 故 Go 构造器形参为 0，只能通过本 Options 传入）
type AfterToolResultOptions struct {
	// ReplaceResult 是否替换工具结果
	ReplaceResult bool
	// UpdatedResult 替换后的结果
	UpdatedResult JsonValue
	// AdditionalContext 追加上下文
	AdditionalContext *string
}

// StopHookContext 轮次即将停止时提供的上下文。
// Python: hooks.py:113-124 StopHookContext（3 必填 + 2 默认）
type StopHookContext struct {
	// AgentName agent 名
	AgentName string
	// ProviderSessionID provider 会话（nil = 未提供）
	ProviderSessionID *string
	// TurnID 所属轮次（nil = 未提供）
	TurnID *string
	// Reason 停止原因（nil = 未提供）
	Reason *string
	// Metadata 附加元数据（已冻结）
	Metadata JsonObject
}

// StopHookContextOptions StopHookContext 的可选字段。
// Python: hooks.py:120-121 dataclass 默认值（metadata 为 default_factory=dict）
type StopHookContextOptions struct {
	// Reason 停止原因
	Reason *string
	// Metadata 附加元数据
	Metadata JsonObject
}

// ──────────────────────────── 枚举 ────────────────────────────

// ToolDecisionKind 工具执行前返回的决策种类。
// Python: hooks.py:20-28 ToolDecisionKind（5 项）
type ToolDecisionKind string

const (
	// ToolDecisionKindAllow 允许执行
	ToolDecisionKindAllow ToolDecisionKind = "allow"
	// ToolDecisionKindDeny 拒绝执行
	ToolDecisionKindDeny ToolDecisionKind = "deny"
	// ToolDecisionKindRewrite 改写参数后允许执行
	ToolDecisionKindRewrite ToolDecisionKind = "rewrite"
	// ToolDecisionKindAsk 转交宿主交互审批
	ToolDecisionKindAsk ToolDecisionKind = "ask"
	// ToolDecisionKindProviderPolicy 交给 provider 原生权限策略
	ToolDecisionKindProviderPolicy ToolDecisionKind = "provider_policy"
)

// ──────────────────────────── 结构体（接口定义）────────────────────────────

// HarnessHookDispatcher 执行边界上被 harness await 的控制面回调。
//
// 钩子返回值可改变或停止执行；events 的 HookObservedEvent 只是钩子调用的
// 观测镜像，不得用于权限决策。
// 方法参数名用 hookCtx 而非 context 以避开包 context 遮蔽。
// Python: hooks.py:127-150 HarnessHookDispatcher（4 方法）
type HarnessHookDispatcher interface {
	// BeforePrompt 在模型执行前检视或改写输入
	BeforePrompt(ctx context.Context, hookCtx BeforePromptContext) (BeforePromptResult, error)
	// BeforeTool 授权、拒绝、改写参数、转交宿主审批或使用 provider 策略
	BeforeTool(ctx context.Context, hookCtx BeforeToolContext) (ToolDecision, error)
	// AfterTool 在执行后检视或改写工具结果
	AfterTool(ctx context.Context, hookCtx AfterToolContext) (AfterToolResult, error)
	// OnStop 在轮次停止被最终确认前观测
	OnStop(ctx context.Context, hookCtx StopHookContext) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBeforePromptContext 创建提示词前置上下文（无校验）。
// Python: hooks.py:30-38 无 __post_init__
func NewBeforePromptContext(
	agentName string,
	providerSessionID *string,
	turnID *string,
	prompt string,
) BeforePromptContext {
	return BeforePromptContext{
		AgentName:         agentName,
		ProviderSessionID: providerSessionID,
		TurnID:            turnID,
		Prompt:            prompt,
	}
}

// NewBeforePromptResult 创建提示词前置结果。
// Python: hooks.py:40-48 无 __post_init__；continue_execution 默认 True，
// Go Options 零值 bool 为 false，故在构造器内填默认值 true。
func NewBeforePromptResult(prompt string, opts BeforePromptResultOptions) (BeforePromptResult, error) {
	return BeforePromptResult{
		Prompt:            prompt,
		ContinueExecution: true, // Python 默认值
		Reason:            opts.Reason,
		AdditionalContext: opts.AdditionalContext,
	}, nil
}

// NewBeforeToolContext 创建工具前置上下文。
// Python: hooks.py:61-62 __post_init__（冻结 arguments）
func NewBeforeToolContext(
	agentName string,
	providerSessionID *string,
	turnID *string,
	callID string,
	toolName string,
	arguments JsonObject,
) (BeforeToolContext, error) {
	// 步骤 1：冻结 arguments（Python freeze_json_object(None) 抛 TypeError，
	// 故 nil 在此被拒绝 —— 与上游一致）
	frozenArguments, err := FreezeJsonObject(arguments)
	if err != nil {
		return BeforeToolContext{}, err
	}
	return BeforeToolContext{
		AgentName:         agentName,
		ProviderSessionID: providerSessionID,
		TurnID:            turnID,
		CallID:            callID,
		ToolName:          toolName,
		Arguments:         frozenArguments,
	}, nil
}

// NewToolDecision 创建工具决策。
// Python: hooks.py:74-80 ToolDecision.__post_init__（3 步，顺序不可调）
// Q9 决策 A：校验跨越「形参 decision」与「Options.UpdatedArguments」边界，
// 仍按 Python __post_init__ 顺序内联。
func NewToolDecision(decision ToolDecisionKind, opts ToolDecisionOptions) (ToolDecision, error) {
	updated := opts.UpdatedArguments
	// 步骤 1：updated_arguments 非 nil 则冻结
	if updated != nil {
		frozen, err := FreezeJsonObject(updated)
		if err != nil {
			return ToolDecision{}, err
		}
		updated = frozen
	}
	// 步骤 2：REWRITE 必须携带 updated_arguments
	if decision == ToolDecisionKindRewrite && updated == nil {
		return ToolDecision{}, &HarnessProtocolError{Msg: "rewrite tool decision requires updated_arguments"}
	}
	// 步骤 3：非 REWRITE 不得携带 updated_arguments
	if decision != ToolDecisionKindRewrite && updated != nil {
		return ToolDecision{}, &HarnessProtocolError{Msg: "updated_arguments is only valid for a rewrite tool decision"}
	}
	return ToolDecision{
		Decision:          decision,
		Reason:            opts.Reason,
		UpdatedArguments:  updated,
		AdditionalContext: opts.AdditionalContext,
	}, nil
}

// NewAfterToolContext 创建工具后置上下文。
// Python: hooks.py:96-98 __post_init__（冻结 arguments 与 result）
func NewAfterToolContext(
	agentName string,
	providerSessionID *string,
	turnID *string,
	callID string,
	toolName string,
	arguments JsonObject,
	opts AfterToolContextOptions,
) (AfterToolContext, error) {
	// 步骤 1：冻结 arguments
	frozenArguments, err := FreezeJsonObject(arguments)
	if err != nil {
		return AfterToolContext{}, err
	}
	// 步骤 2：冻结 result（JsonValue，nil 为合法 JSON null）
	frozenResult, err := FreezeJsonValue(opts.Result)
	if err != nil {
		return AfterToolContext{}, err
	}
	return AfterToolContext{
		AgentName:         agentName,
		ProviderSessionID: providerSessionID,
		TurnID:            turnID,
		CallID:            callID,
		ToolName:          toolName,
		Arguments:         frozenArguments,
		Result:            frozenResult,
		Error:             opts.Error,
	}, nil
}

// NewAfterToolResult 创建工具结果改写。
// Python: hooks.py:109-110 __post_init__（冻结 updated_result）
// Q6 特例：3 个字段全有默认值 → 形参 0。
func NewAfterToolResult(opts AfterToolResultOptions) (AfterToolResult, error) {
	// 步骤 1：冻结 updated_result
	frozenResult, err := FreezeJsonValue(opts.UpdatedResult)
	if err != nil {
		return AfterToolResult{}, err
	}
	return AfterToolResult{
		ReplaceResult:     opts.ReplaceResult,
		UpdatedResult:     frozenResult,
		AdditionalContext: opts.AdditionalContext,
	}, nil
}

// NewStopHookContext 创建停止钩子上下文。
// Python: hooks.py:123-124 __post_init__（冻结 metadata）
func NewStopHookContext(
	agentName string,
	providerSessionID *string,
	turnID *string,
	opts StopHookContextOptions,
) (StopHookContext, error) {
	// 步骤 0（Python default_factory 等价物）：metadata 零值归一化为空对象。
	// Python 侧 fields(default_factory=dict) 使调用方永不传 None；
	// Go Options 零值是 nil，而 FreezeJsonObject(nil) 会报 "must be a mapping"，
	// 故此处必须先补空 map，顺序在 freeze 之前。
	metadata := opts.Metadata
	if metadata == nil {
		metadata = JsonObject{}
	}
	// 步骤 1：冻结 metadata
	frozenMetadata, err := FreezeJsonObject(metadata)
	if err != nil {
		return StopHookContext{}, err
	}
	return StopHookContext{
		AgentName:         agentName,
		ProviderSessionID: providerSessionID,
		TurnID:            turnID,
		Reason:            opts.Reason,
		Metadata:          frozenMetadata,
	}, nil
}
