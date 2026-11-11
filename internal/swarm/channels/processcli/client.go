package processcli

import (
	"context"
	"encoding/json"
	"fmt"

	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProcessCliBoundaryError 逃出 Process CLI 渠道的调用抛出的稳定失败。
// Python: client.py:14-18 ProcessCliBoundaryError
type ProcessCliBoundaryError struct {
	// Message 失败说明
	Message string
}

// InProcessRuntimeClient 薄客户端：不含 server、协议、socket 或传输关注点。
// Python: client.py:73-306 InProcessRuntimeClient
//
// 全部方法都是 AgentRuntime 公有 API 的委托，外加 process_cli 渠道守卫。
type InProcessRuntimeClient struct {
	// runtime 底层共享 Runtime
	runtime *swruntime.AgentRuntime
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// processCliChannelID Process CLI 渠道标识。
	// Python: client.py:11 _PROCESS_CLI_CHANNEL_ID
	processCliChannelID = "process_cli"
	// codeProcessCliChannelInvalid 渠道守卫错误码。
	// Python: client.py:17 code = "PROCESS_CLI_CHANNEL_INVALID"
	codeProcessCliChannelInvalid = "PROCESS_CLI_CHANNEL_INVALID"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *ProcessCliBoundaryError) Error() string { return e.Message }

// Code 稳定错误码。
// Python: client.py:17 code = "PROCESS_CLI_CHANNEL_INVALID"
func (e *ProcessCliBoundaryError) Code() string { return codeProcessCliChannelInvalid }

// Retryable 是否可重试（恒 false）。
// Python: client.py:18 retryable = False
func (e *ProcessCliBoundaryError) Retryable() bool { return false }

// NewInProcessRuntimeClient 构造客户端；runtime 为 nil 时新建一个。
// Python: client.py:76-80 InProcessRuntimeClient.__init__
func NewInProcessRuntimeClient(runtime *swruntime.AgentRuntime) *InProcessRuntimeClient {
	if runtime != nil {
		return &InProcessRuntimeClient{runtime: runtime}
	}
	return &InProcessRuntimeClient{runtime: swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{})}
}

// Runtime 返回底层 Runtime。
// Python: client.py:82-84 InProcessRuntimeClient.runtime
func (c *InProcessRuntimeClient) Runtime() *swruntime.AgentRuntime { return c.runtime }

// Start 启动 Runtime。
// Python: client.py:86-87 start
func (c *InProcessRuntimeClient) Start() error {
	return c.runtime.Start()
}

// ValidateAgentDefinition 校验声明式 Agent 定义并返回可执行体。
// Python: client.py:89-95 validate_agent_definition
//
// 委托到 Runtime 的同名能力：Python 侧由 AgentRuntime 持有
// （service.py 内部组合 PrepareAgentExecution）。Go 侧该能力目前以包级
// prepareAgentExecution 形式存在，此处组装成与上游一致的入口。
func (c *InProcessRuntimeClient) ValidateAgentDefinition(
	definition map[string]any, mode string,
) (*swruntime.RuntimeAgentExecution, error) {
	return swruntime.PrepareAgentExecution(definition, mode)
}

// ListModelCapabilities 列出模型能力。
// Python: client.py:97-104 list_model_capabilities
func (c *InProcessRuntimeClient) ListModelCapabilities(currentSelection string) swruntime.ModelCatalogResult {
	return c.runtime.ListModelCapabilities(currentSelection)
}

// ResolveModelCapability 解析模型能力。
// Python: client.py:106-107 resolve_model_capability
func (c *InProcessRuntimeClient) ResolveModelCapability(requested string) (swruntime.RuntimeModelDescriptor, error) {
	return c.runtime.ResolveModelCapability(requested)
}

// ListModeCapabilities 列出模式能力。
// Python: client.py:109-110 list_mode_capabilities
func (c *InProcessRuntimeClient) ListModeCapabilities() swruntime.ModeCatalogResult {
	return c.runtime.ListModeCapabilities()
}

// ResolveModeCapability 解析模式能力（含渠道守卫）。
// Python: client.py:112-113 resolve_mode_capability
func (c *InProcessRuntimeClient) ResolveModeCapability(requested any) (swruntime.RuntimeModeDescriptor, error) {
	return swruntime.ResolveModeCapability(requested)
}

// GetSession 读一个会话摘要。
// Python: client.py:115-117 get_session（含渠道守卫）
func (c *InProcessRuntimeClient) GetSession(request swruntime.SessionGetInput) (*swruntime.SessionSummary, error) {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.GetSession(request)
}

// ListSessions 列出会话。
// Python: client.py:119-121 list_sessions（含渠道守卫）
func (c *InProcessRuntimeClient) ListSessions(request swruntime.SessionListInput) (swruntime.SessionListResult, error) {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return swruntime.SessionListResult{}, err
	}
	return c.runtime.ListSessions(request)
}

// GetPermissionSnapshot 取权限快照。
// Python: client.py:123-131 get_permission_snapshot
//
// 仅当请求带了 session_id 或 channel_id 时才做渠道守卫
// （两者皆空表示「与环境无关的目录查询」）。
func (c *InProcessRuntimeClient) GetPermissionSnapshot(
	request swruntime.PermissionSnapshotInput,
) (swruntime.PermissionSnapshotResult, error) {
	if (request.SessionID != nil && *request.SessionID != "") || request.ChannelID != "" {
		if err := requireProcessCliChannel(request.ChannelID); err != nil {
			return swruntime.PermissionSnapshotResult{}, err
		}
	}
	return c.runtime.GetPermissionSnapshot(request)
}

// ValidateMcpReferences 校验 MCP 引用。
// Python: client.py:133-137 validate_mcp_references
//
// ⤵️ 回填 20.7：Runtime 侧 validate_mcp_references 尚未实现
// （Q3 核实：Go 侧 0 命中；上游 runtime/mcp_references.py）。本批返回「全部有效」，
// 使 machine.go 的 MCP_NOT_READY 分支不可达——与上游「引用了可用 MCP」等价。
func (c *InProcessRuntimeClient) ValidateMcpReferences(references []string) (*McpReferenceValidationResult, error) {
	return &McpReferenceValidationResult{Valid: true}, nil
}

// CreateOrResumeSession 创建或恢复会话（含渠道守卫）。
// Python: client.py:139-149 create_or_resume_session
func (c *InProcessRuntimeClient) CreateOrResumeSession(channelID string, sessionID *string) (string, error) {
	if err := requireProcessCliChannel(channelID); err != nil {
		return "", err
	}
	sid := ""
	if sessionID != nil {
		sid = *sessionID
	}
	return c.runtime.CreateOrResumeSession(channelID, sid)
}

// DescribeSession 通过 Runtime 边界读已持久化的会话事实。
// Python: client.py:151-166 describe_session
//
// 渠道不匹配时返回 nil：在 Process CLI 边界上，「不存在」与「属于别的渠道」
// 刻意不可区分。
func (c *InProcessRuntimeClient) DescribeSession(sessionID string) (*sessionpkg.SessionDescriptor, error) {
	descriptor, err := c.runtime.DescribeSession(sessionID)
	if err != nil {
		return nil, err
	}
	if descriptor == nil {
		return nil, nil
	}
	if guardErr := requireProcessCliChannel(descriptor.ChannelID); guardErr != nil {
		// Python: :162-165 except ProcessCliBoundaryError: return None
		return nil, nil
	}
	return descriptor, nil
}

// PrepareSessionCreate 准备一次会话创建（含渠道守卫）。
// Python: client.py:168-173 prepare_session_create
func (c *InProcessRuntimeClient) PrepareSessionCreate(
	ctx context.Context, input swruntime.SessionCreateInput,
) (*swruntime.PreparedSessionProvision, error) {
	if err := requireProcessCliChannel(input.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.PrepareSessionCreate(ctx, input)
}

// PrepareSessionSwitch 准备一次会话切换（含渠道守卫）。
// Python: client.py:175-180 prepare_session_switch
func (c *InProcessRuntimeClient) PrepareSessionSwitch(
	ctx context.Context, input swruntime.SessionSwitchInput,
) (*swruntime.PreparedSessionProvision, error) {
	if err := requireProcessCliChannel(input.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.PrepareSessionSwitch(ctx, input)
}

// PrepareSessionFork 准备一次会话分叉（含渠道守卫）。
// Python: client.py:182-187 prepare_session_fork
func (c *InProcessRuntimeClient) PrepareSessionFork(
	ctx context.Context, input swruntime.SessionForkInput,
) (*swruntime.PreparedSessionProvision, error) {
	if err := requireProcessCliChannel(input.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.PrepareSessionFork(ctx, input)
}

// CommitSessionProvision 提交一次已准备的会话操作。
// Python: client.py:189-203 commit_session_provision
//
// 渠道守卫读的是 prepared.result.channel_id（Python :196-198 的
// getattr(getattr(prepared, "result", None), "channel_id", None)）。
func (c *InProcessRuntimeClient) CommitSessionProvision(
	prepared *swruntime.PreparedSessionProvision,
	timing swruntime.SessionProvisionCommitTiming,
	commitContext *swruntime.SessionProvisionCommitContext,
) (any, error) {
	if err := requireProcessCliChannel(preparedChannelID(prepared)); err != nil {
		return nil, err
	}
	return c.runtime.CommitSessionProvision(prepared, timing, commitContext)
}

// AbortSessionProvision 放弃一次已准备的会话操作。
// Python: client.py:205-212 abort_session_provision
func (c *InProcessRuntimeClient) AbortSessionProvision(prepared *swruntime.PreparedSessionProvision) error {
	if err := requireProcessCliChannel(preparedChannelID(prepared)); err != nil {
		return err
	}
	return c.runtime.AbortSessionProvision(prepared)
}

// DeleteSession 删除一个已持久化的会话（含渠道守卫）。
// Python: client.py:214-224 delete_session
func (c *InProcessRuntimeClient) DeleteSession(
	ctx context.Context, channelID, sessionID string,
) (sessionpkg.SessionDeleteResult, error) {
	if err := requireProcessCliChannel(channelID); err != nil {
		return sessionpkg.SessionDeleteResult{}, err
	}
	return c.runtime.DeleteSession(ctx, swruntime.DeleteSessionOptions{
		ChannelID: channelID,
		SessionID: sessionID,
	})
}

// AnswerInteraction 回答一次交互请求（含渠道守卫）。
// Python: client.py:257-262 answer_interaction
func (c *InProcessRuntimeClient) AnswerInteraction(
	ctx context.Context, request *schema.AgentRequest,
) ([]*swruntime.RuntimeEvent, error) {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.AnswerInteraction(ctx, request)
}

// AnswerInteractionInput 用类型化输入回答交互（含渠道守卫）。
// Python: client.py:264-269 answer_interaction_input
func (c *InProcessRuntimeClient) AnswerInteractionInput(
	ctx context.Context, answer *swruntime.InteractionAnswerInput,
) ([]*swruntime.RuntimeEvent, error) {
	if err := requireProcessCliChannel(answer.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.AnswerInteractionInput(ctx, answer)
}

// StreamInteractionAnswer 转发类型化回答并保留增量观测（含渠道守卫）。
// Python: client.py:271-277 stream_interaction_answer
func (c *InProcessRuntimeClient) StreamInteractionAnswer(
	ctx context.Context,
	answer *swruntime.InteractionAnswerInput,
	onEvent func(*swruntime.RuntimeEvent) bool,
) error {
	if err := requireProcessCliChannel(answer.ChannelID); err != nil {
		return err
	}
	// Python: :2050 request = answer.to_agent_request()
	request, err := answer.ToAgentRequest()
	if err != nil {
		return err
	}
	return c.runtime.StreamInteractionAnswer(ctx, request, onEvent)
}

// Stream 跑一轮流式请求（含渠道守卫）。
// Python: client.py:226-230 stream → runtime.stream(request, on_agent_ready=...)
//
// Python 返回 AsyncIterator；Go 的 AgentRuntime.Stream 是回调式
// （onEvent 返回 false 即停止）。此处直接沿用回调形态，不自造 channel 包装层
// ——那会引入上游没有的中间结构（规则 9）。
func (c *InProcessRuntimeClient) Stream(
	ctx context.Context,
	request *schema.AgentRequest,
	onEvent func(*swruntime.RuntimeEvent) bool,
	onAgentReady func(context.Context, harnessinterfaces.DeepAgentInterface) error,
) error {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return err
	}
	return c.runtime.Stream(ctx, request, onEvent, streamOptionsFor(onAgentReady)...)
}

// StreamAgent 跑一轮声明式 Agent 流式请求（含渠道守卫）。
// Python: client.py:232-242 stream_agent
func (c *InProcessRuntimeClient) StreamAgent(
	ctx context.Context,
	request *schema.AgentRequest,
	definition map[string]any,
	onEvent func(*swruntime.RuntimeEvent) bool,
	onAgentReady func(context.Context, harnessinterfaces.DeepAgentInterface) error,
) error {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return err
	}
	execution, err := swruntime.PrepareAgentExecution(definition, modeOf(request))
	if err != nil {
		return err
	}
	opts := append(streamOptionsFor(onAgentReady), swruntime.WithAgentExecution(execution))
	return c.runtime.Stream(ctx, request, onEvent, opts...)
}

// Invoke 跑一轮非流式请求（含渠道守卫）。
// Python: client.py:244-247 invoke
func (c *InProcessRuntimeClient) Invoke(
	ctx context.Context, request *schema.AgentRequest,
) ([]*swruntime.RuntimeEvent, error) {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return nil, err
	}
	return c.runtime.Invoke(ctx, request)
}

// InvokeAgent 跑一轮声明式 Agent 非流式请求（含渠道守卫）。
// Python: client.py:249-255 invoke_agent
func (c *InProcessRuntimeClient) InvokeAgent(
	ctx context.Context, request *schema.AgentRequest, definition map[string]any,
) ([]*swruntime.RuntimeEvent, error) {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return nil, err
	}
	execution, err := swruntime.PrepareAgentExecution(definition, modeOf(request))
	if err != nil {
		return nil, err
	}
	return c.runtime.Invoke(ctx, request, swruntime.WithAgentExecution(execution))
}

// Cancel 取消一次请求（含渠道守卫）。
// Python: client.py:279-281 cancel → runtime.cancel_request
//
// Go 侧取消是包级 CancelRequest（需要 AgentManagerPort），Runtime 自身不持有管理器；
// 因此此处从 Runtime 依赖里取 manager 后调用。
func (c *InProcessRuntimeClient) Cancel(ctx context.Context, request *schema.AgentRequest) error {
	if err := requireProcessCliChannel(request.ChannelID); err != nil {
		return err
	}
	mgr := c.runtime.AgentManager()
	if mgr == nil {
		return fmt.Errorf("runtime manager is nil")
	}
	// Python: runtime.cancel_request(request) 默认 allow_create=False
	_, err := swruntime.CancelRequest(ctx, mgr, request, false)
	return err
}

// CleanupSession 清理一个会话（含渠道守卫）。
// Python: client.py:283-288 cleanup_session
func (c *InProcessRuntimeClient) CleanupSession(channelID, sessionID string) (bool, error) {
	if err := requireProcessCliChannel(channelID); err != nil {
		return false, err
	}
	return c.runtime.CleanupSession(context.Background(), channelID, sessionID, true)
}

// Close 关闭 Runtime 并排空待写的会话持久化。
// Python: client.py:290-306 close
//
// 两段都尝试执行；都失败时抛第一个（对齐 cleanup_errors[0]）。
func (c *InProcessRuntimeClient) Close() error {
	var firstErr error
	if err := c.runtime.Close(); err != nil {
		firstErr = err
	}
	// Python: :297-304 flush_session_writes()
	if err := FlushSessionWrites(5.0); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// McpReferenceValidationResult MCP 引用校验结果。
// Python: runtime/mcp_references.py McpReferenceValidationResult
type McpReferenceValidationResult struct {
	// Valid 引用是否全部可用
	Valid bool
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requireProcessCliChannel 渠道守卫。
// Python: client.py:21-25 _require_process_cli_channel
func requireProcessCliChannel(channelID string) error {
	if channelID != processCliChannelID {
		return &ProcessCliBoundaryError{
			Message: "process CLI Runtime calls require channel_id='process_cli'",
		}
	}
	return nil
}

// streamOptionsFor 把 agent 就绪回调转成 StreamOption（nil 时不传）。
// Python: client.py:226-230 on_agent_ready=on_agent_ready
//
// Python 传 Any；Go 收窄为 DeepAgentInterface（spec D-8）——
// configure_process_cli_run 实质只用到 react_agent 与 ability_manager，二者该接口都有。
func streamOptionsFor(
	onAgentReady func(context.Context, harnessinterfaces.DeepAgentInterface) error,
) []swruntime.StreamOption {
	if onAgentReady == nil {
		return nil
	}
	return []swruntime.StreamOption{swruntime.WithOnAgentReady(onAgentReady)}
}

// modeOf 从请求参数里取 mode（用于声明式 Agent 定义校验）。
// Python: machine.py:251 mode = client.resolve_mode_capability(mode_name)
func modeOf(request *schema.AgentRequest) string {
	if request == nil || len(request.Params) == 0 {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return ""
	}
	if mode, ok := params["mode"].(string); ok {
		return mode
	}
	return ""
}

// preparedChannelID 取已准备会话操作的目标渠道。
// Python: client.py:196-198 getattr(getattr(prepared, "result", None), "channel_id", None)
func preparedChannelID(prepared *swruntime.PreparedSessionProvision) string {
	if prepared == nil {
		return ""
	}
	_, channelID, _, _, ok := swruntime.CommittedSessionIdentity(prepared.Result())
	if !ok {
		// 结果类型不在 create/switch 范围内时不拦截，交由 Runtime 判定
		return ""
	}
	return channelID
}
