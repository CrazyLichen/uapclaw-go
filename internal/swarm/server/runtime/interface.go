package runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swsession "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UapClawSwarm Agent 统一门面。
// 提供：SDK 适配器路由、统一对外 API、公共编排
// （session 队列、Skills 路由、heartbeat、流式包装）。
// Python: jiuwenswarm/server/runtime/agent_adapter/interface.py (JiuWenClaw)
type UapClawSwarm struct {
	// adapter SDK 适配器（延迟初始化，ensureAdapter 时创建）。
	adapter adapter.AgentAdapter

	// skillManager 技能管理器（server 层）。
	skillManager *skill.SkillManager

	// agentConfigService Agent 配置 CRUD 服务。
	agentConfigService *AgentConfigService

	// coordinator 会话执行协调器（Runtime 层的跨会话执行账本）
	// Python: AgentRuntime._session_coordinator
	coordinator *swsession.RuntimeSessionCoordinator

	// sessionManager 门面自有的会话任务队列（同会话串行、跨会话并发）
	// Python: self._session_manager = SessionManager()（interface.py:1574）
	//
	// 与 coordinator 是**互斥**关系而非串联：一次请求按 uses_session_runtime 二选一，
	// 走协调器的不再进门面队列（Python service.py:1873 同）。
	sessionManager *swruntime.SessionManager

	// skilldevService SkillDev 服务（懒初始化，ensureSkillDevService 时创建）。
	skilldevService *skilldev.SkillDevService

	// adapterMu 保护 adapter 字段的并发访问。
	adapterMu sync.Mutex

	// skilldevMu 保护 skilldevService 字段的并发访问。
	skilldevMu sync.Mutex

	// appCtx 应用级 context，随服务生命周期创建和取消。
	// dreaming 等后台服务使用此 context 而非 context.Background()，
	// 确保服务关闭时后台服务可优雅停止。
	appCtx    context.Context
	appCancel context.CancelFunc
}

// agentConfigListerBridge 将 runtime.AgentConfigService 桥接到 adapter.AgentConfigLister 接口。
// 避免 adapter 直接导入 runtime 包造成循环依赖。
// 使用 types.AgentDefinition 共享类型，无需逐字段拷贝。
type agentConfigListerBridge struct {
	svc *AgentConfigService
}

// createInstanceConfig CreateInstance 的内部配置。
type createInstanceConfig struct {
	config  map[string]any
	mode    string
	subMode string
}

// CreateInstanceOption CreateInstance 的可选参数。
type CreateInstanceOption func(*createInstanceConfig)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// WithCreateInstanceConfig 设置 CreateInstance 配置。
func WithCreateInstanceConfig(config map[string]any) CreateInstanceOption {
	return func(c *createInstanceConfig) { c.config = config }
}

// WithCreateInstanceMode 设置 CreateInstance 模式。
func WithCreateInstanceMode(mode string) CreateInstanceOption {
	return func(c *createInstanceConfig) { c.mode = mode }
}

// WithCreateInstanceSubMode 设置 CreateInstance 子模式。
func WithCreateInstanceSubMode(subMode string) CreateInstanceOption {
	return func(c *createInstanceConfig) { c.subMode = subMode }
}

// NewUapClaw 创建 UapClawSwarm 实例。
// Python: JiuWenClaw.__init__()
func NewUapClaw() *UapClawSwarm {
	appCtx, appCancel := context.WithCancel(context.Background())
	return &UapClawSwarm{
		coordinator:        swsession.NewRuntimeSessionCoordinator(swsession.DefaultCoordinatorOptions()),
		skillManager:       skill.NewSkillManager(workspace.AgentWorkspaceDir()),
		agentConfigService: NewAgentConfigService(workspace.WorkspaceDir()),
		appCtx:             appCtx,
		appCancel:          appCancel,
		sessionManager:     swruntime.NewSessionManager(),
	}
}

// ProcessMessage 通过门面自有的会话调度器处理请求。
// Python: interface.py process_message（schedule_session=True）
//
// 与 ExecuteMessage 的唯一区别是调度归属：本方法把执行排进门面会话队列
// （同会话串行），适用于不归 Runtime 协调器管的零碎 RPC。
func (uc *UapClawSwarm) ProcessMessage(
	ctx context.Context, request *schema.AgentRequest,
) (*schema.AgentResponse, error) {
	return uc.processMessageInternal(ctx, request, true)
}

// ExecuteMessage 执行一次由 AgentRuntime 拥有调度的请求。
// Python: interface.py execute_message（schedule_session=False）
//
// 不排门面队列：Runtime 协调器已经为它分配了执行与代次，再排一次会把
// 同一请求调度两遍。
func (uc *UapClawSwarm) ExecuteMessage(
	ctx context.Context, request *schema.AgentRequest,
) (*schema.AgentResponse, error) {
	return uc.processMessageInternal(ctx, request, false)
}

// processMessageInternal 处理非流式请求的公共实现。
// Python: interface.py _process_message(request, schedule_session=...)
//
// scheduleSession 为真时把执行排进门面会话队列（同会话串行）；
// 为假时直接执行（Runtime 协调器已负责调度）。
func (uc *UapClawSwarm) processMessageInternal(
	ctx context.Context, request *schema.AgentRequest, scheduleSession bool,
) (*schema.AgentResponse, error) {
	if !scheduleSession || uc.sessionManager == nil {
		return uc.runMessageDirect(ctx, request)
	}
	sessionID := swruntime.GetSessionID(request.SessionID)
	value, err := uc.sessionManager.SubmitAndWait(ctx, sessionID,
		func(taskCtx context.Context) (any, error) {
			return uc.runMessageDirect(taskCtx, request)
		})
	if err != nil {
		return nil, err
	}
	response, _ := value.(*schema.AgentResponse)
	return response, nil
}

// runMessageDirect 直接执行一次非流式 Agent 请求（不经门面队列）。
// Python: JiuWenClaw.process_message(request) 的实现体
func (uc *UapClawSwarm) runMessageDirect(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// Python: logger.info：请求日志
	sessionIDForLog := ""
	if request.SessionID != nil {
		sessionIDForLog = *request.SessionID
	}
	logger.Info(logComponent).
		Str("event_type", "process_message").
		Str("request_id", request.RequestID).
		Str("channel_id", request.ChannelID).
		Str("session_id", sessionIDForLog).
		Msg("processing non-stream request")

	// 1. CANCEL 分支 → 委托 ProcessInterrupt
	if request.ReqMethod == schema.ReqMethodChatCancel {
		return uc.ProcessInterrupt(ctx, request)
	}

	// 2. 确保 adapter
	mode := uc.adapterModeForRequest(request)
	a, err := uc.ensureAdapter(mode)
	if err != nil {
		return nil, err
	}

	// 3. ANSWER 分支
	if request.ReqMethod == schema.ReqMethodChatAnswer {
		return a.HandleUserAnswer(ctx, request)
	}

	// 4. heartbeat 分支
	if resp, herr := a.HandleHeartbeat(ctx, request); resp != nil {
		return resp, herr
	}

	// 5. SkillDev 分支（非流式），对齐 Python：SkillDev 优先于 Skills 判断
	if resp, err := uc.handleSkillDevRequest(ctx, request); resp != nil {
		return resp, err
	}
	// 6. Skills 分支
	if resp, err := uc.handleSkillsRequest(ctx, request); resp != nil {
		return resp, err
	}
	// 7. Plugins 分支
	if resp, err := uc.handlePluginsRequest(ctx, request); resp != nil {
		return resp, err
	}

	// 8. 常规对话
	sessionID := session.NormalizeSessionID(uc.extractSessionID(request))

	// 记录 user 历史，对齐 Python：补传 mode 和 channel_metadata
	userMode := ""
	if p := parseRequestParams(request); p != nil {
		if m, ok := p["mode"].(string); ok {
			userMode = m
		}
	}
	if userMode == "" {
		userMode = "unknown"
	}
	session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", uc.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, request.Metadata, userMode)

	// 构建 inputs
	inputs, memoryMode, _ := uc.BuildInputs(request)

	// 云端记忆对话前钩子（对齐 Python: interface.py:834-845 MEMORY_BEFORE_CHAT trigger）
	if memoryMode == "cloud" {
		extensions.TriggerMemoryBeforeChat(ctx, request, inputs, filepath.Join(workspace.AgentRootDir(), "home"), parseRequestParams(request))
	}

	// 提交到会话协调器并等待结果
	// Python: AgentRuntime 的 _session_coordinator.run_unary（10.3.28d 取代 sessionManager.SubmitAndWait）
	_, _ = uc.coordinator.RegisterSession(sessionID, request.ChannelID,
		swsession.SessionPersistencePolicyPersistent)
	result, err := uc.coordinator.RunUnary(ctx, sessionID, request.RequestID,
		swsession.SessionWorkKindChatStream,
		func(taskCtx context.Context) (any, error) {
			return a.ProcessMessageImpl(taskCtx, request, inputs)
		},
		swsession.UnaryOptions{})
	if err != nil {
		return nil, err
	}

	resp, ok := result.(*schema.AgentResponse)
	if !ok || resp == nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(true),
		), nil
	}

	// 记录 assistant 历史
	assistantContent := ""
	if resp.OK {
		assistantContent = uc.extractResponseContent(resp)
		// Python: 补传 extra 和 mode
		assistantMode := ""
		if p := parseRequestParams(request); p != nil {
			if m, ok := p["mode"].(string); ok {
				assistantMode = m
			}
		}
		if assistantMode == "" {
			assistantMode = "unknown"
		}
		session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
			"assistant", assistantContent, float64(time.Now().UnixMilli())/1000,
			"chat.final", nil, nil, assistantMode)
	}

	// 云端记忆对话后钩子（对齐 Python: interface.py:852-878 MEMORY_AFTER_CHAT trigger。
	// Python 双条件 result.ok and payload.get("content")——响应无内容不触发）
	if memoryMode == "cloud" && resp.OK && assistantContent != "" {
		extensions.TriggerMemoryAfterChat(ctx, request, assistantContent, filepath.Join(workspace.AgentRootDir(), "home"), parseRequestParams(request))
	}

	return resp, nil
}

// ProcessMessageStream 通过门面自有的会话调度器处理流式请求。
// Python: interface.py process_message_stream（schedule_session=True）
//
// 与 ExecuteMessageStream 的唯一区别是调度归属；流式请求一旦排进队列，
// 返回的 channel 由队列内的执行协程生产，调用方照常消费。
func (uc *UapClawSwarm) ProcessMessageStream(
	ctx context.Context, request *schema.AgentRequest,
) (<-chan *schema.AgentResponseChunk, error) {
	return uc.processMessageStreamInternal(ctx, request, true)
}

// ExecuteMessageStream 执行一次由 AgentRuntime 拥有调度的流式请求。
// Python: interface.py execute_message_stream（schedule_session=False）
func (uc *UapClawSwarm) ExecuteMessageStream(
	ctx context.Context, request *schema.AgentRequest,
) (<-chan *schema.AgentResponseChunk, error) {
	return uc.processMessageStreamInternal(ctx, request, false)
}

// processMessageStreamInternal 流式请求的公共实现。
// Python: interface.py _process_message_stream(request, schedule_session=...)
//
// 流式不能像非流式那样"先排队再同步等结果"：队列里跑的是生产协程，
// 调用方要在外面消费它产出的 channel。这里用转接协程把队列内的输出搬到
// 外层 channel，保证调用方拿到的仍是连续流。
func (uc *UapClawSwarm) processMessageStreamInternal(
	ctx context.Context, request *schema.AgentRequest, scheduleSession bool,
) (<-chan *schema.AgentResponseChunk, error) {
	if !scheduleSession || uc.sessionManager == nil {
		return uc.runMessageStreamDirect(ctx, request)
	}
	sessionID := swruntime.GetSessionID(request.SessionID)
	inner := make(chan *schema.AgentResponseChunk)
	ready := make(chan error, 1)
	uc.sessionManager.SubmitTask(ctx, sessionID, func(taskCtx context.Context) (any, error) {
		stream, streamErr := uc.runMessageStreamDirect(taskCtx, request)
		if streamErr != nil {
			ready <- streamErr
			close(inner)
			return nil, streamErr
		}
		ready <- nil
		for chunk := range stream {
			select {
			case inner <- chunk:
			case <-taskCtx.Done():
				close(inner)
				return nil, taskCtx.Err()
			}
		}
		close(inner)
		return nil, nil
	})
	// 等首个结果（错误或"已开始生产"），避免调用方拿到空 channel 却不知原因
	if err := <-ready; err != nil {
		return nil, err
	}
	return inner, nil
}

// runMessageStreamDirect 直接执行一次流式 Agent 请求（不经门面队列）。
// Python: JiuWenClaw.process_message_stream(request) 的实现体
func (uc *UapClawSwarm) runMessageStreamDirect(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	// Python: logger.info：流式请求日志
	sessionIDForLog := ""
	if request.SessionID != nil {
		sessionIDForLog = *request.SessionID
	}
	logger.Info(logComponent).
		Str("event_type", "process_message_stream").
		Str("request_id", request.RequestID).
		Str("channel_id", request.ChannelID).
		Str("session_id", sessionIDForLog).
		Msg("processing stream request")

	// 1. SkillDev 流式分支
	if skill.IsSkillDevMethod(request.ReqMethod) {
		return uc.handleSkillDevStreamRequest(ctx, request)
	}

	// 2. 确保 adapter
	mode := uc.adapterModeForRequest(request)
	a, err := uc.ensureAdapter(mode)
	if err != nil {
		return nil, err
	}

	// 3. 提取 sessionID
	sessionID := session.NormalizeSessionID(uc.extractSessionID(request))

	// Team 模式判断（对齐 Python: interface.py:909-918）
	isTeam := isTeamMode(request)
	isAutoResume := isAutoHarnessResume(request)

	// 4. 记录 user 历史，对齐 Python：mode 取 request.params["mode"]，空时设为 "unknown"
	userMode := ""
	if p := parseRequestParams(request); p != nil {
		if m, ok := p["mode"].(string); ok {
			userMode = m
		}
	}
	if userMode == "" {
		userMode = "unknown"
	}
	session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", uc.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, request.Metadata, userMode)

	// 5. 构建 inputs
	inputs, memoryMode, rawQuery := uc.BuildInputs(request)

	// Team 模式：使用原始 query，不经过 BuildUserPrompt 包装（对齐 Python: interface.py:940-949）
	if isTeam {
		if _, ok := inputs["query"].(*interaction.InteractiveInput); !ok {
			inputs["query"] = rawQuery
			logger.Info(logComponent).
				Str("event_type", "team_raw_query").
				Str("raw_query", truncateStr(rawQuery, 100)).
				Msg("team mode using original query")
		}
	}

	// 云端记忆对话前钩子（对齐 Python: interface.py:951-963 MEMORY_BEFORE_CHAT trigger）
	if memoryMode == "cloud" {
		extensions.TriggerMemoryBeforeChat(ctx, request, inputs, filepath.Join(workspace.AgentRootDir(), "home"), parseRequestParams(request))
	}

	// 6. 创建中转 channel
	outCh := make(chan *schema.AgentResponseChunk, 64)

	// 7. 生产者 goroutine
	go func() {
		// 结束时关闭 outCh（对齐 Python 队列哨兵语义：消费者顺序处理完所有 chunk 后退出）。
		// 关闭的带缓冲 channel 接收端会先排空缓冲数据再返回 !ok，天然保证不丢 chunk。
		defer close(outCh)
		chunkCh, streamErr := a.ProcessMessageStreamImpl(ctx, request, inputs)
		if streamErr != nil {
			// Python: except asyncio.CancelledError：取消不作为错误（不落 history、不发帧）。
			// 注意只能以 ctx 状态判定：WrapError 曾用 %v 断链，errors.Is(err, …) 不可靠。
			if utils.Canceled(ctx) {
				return
			}
			// Python: append_history_record(event_type="chat.error", ...)
			errMode := ""
			if p := parseRequestParams(request); p != nil {
				if m, ok := p["mode"].(string); ok {
					errMode = m
				}
			}
			if errMode == "" {
				errMode = "unknown"
			}
			session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
				"assistant", streamErr.Error(), float64(time.Now().UnixMilli())/1000,
				"chat.error", nil, nil, errMode)
			outCh <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
				map[string]any{"event_type": "chat.error", "error": streamErr.Error()},
			)
			return
		}
		for chunk := range chunkCh {
			outCh <- chunk
		}
	}()

	// 8. 消费者 goroutine
	resultCh := make(chan *schema.AgentResponseChunk, 64)
	go func() {
		defer close(resultCh)
		var finalAnswerContent string
		var finalAnswerChunks []string

		// 顺序消费 outCh 直至关闭（对齐 Python 异步队列 + 哨兵的顺序语义：
		// 每个 chunk 都完整经过 history 记录与 finalAnswer 收集，不存在跳过路径）
		for chunk := range outCh {
			if payload := chunk.Payload; payload != nil {
				if eventType, _ := payload["event_type"].(string); eventType != "" {
					// 取消后的错误帧一律丢弃（既不下发也不落 history）。
					// 对齐 Python：取消（CancelledError）不产生 chat.error；Go 侧错误帧
					// 源头分散（ReAct 收尾、task-loop 轮次结果、adapter 兜底），在这里统一收口。
					if eventType == "chat.error" && utils.Canceled(ctx) {
						continue
					}
					if shouldRecordHistory(eventType) {
						// Python: 补传 mode
						streamMode := ""
						if p := parseRequestParams(request); p != nil {
							if m, ok := p["mode"].(string); ok {
								streamMode = m
							}
						}
						if streamMode == "" {
							streamMode = "unknown"
						}
						// Python: extra_fields = payload 去掉 event_type/content 后的全部字段
						// （对齐 interface.py:1063/1102；team.message 额外展平 event 子字段）
						extraFields := payloadExtras(payload)
						extraFields = mergeTeamEventExtras(extraFields, payload)
						session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
							"assistant", extractChunkContent(payload), float64(time.Now().UnixMilli())/1000,
							eventType, extraFields, nil, streamMode)
					}
					// Python: context_compression_state 事件写入 compact history
					if eventType == "context_compression_state" {
						compMode := ""
						if p := parseRequestParams(request); p != nil {
							if m, ok := p["mode"].(string); ok {
								compMode = m
							}
						}
						if compMode == "" {
							compMode = "unknown"
						}
						session.AppendCompactHistoryFromPayload(payload, sessionID, request.RequestID, request.ChannelID, compMode)
					}
					switch eventType {
					case "chat.final":
						if c, ok := payload["content"].(string); ok {
							finalAnswerContent = c
						}
					case "chat.delta":
						if c, ok := payload["content"].(string); ok {
							finalAnswerChunks = append(finalAnswerChunks, c)
						}
					}
				}
			}
			resultCh <- chunk
		}

		// 云端记忆对话后钩子（对齐 Python: interface.py:1134-1146 MEMORY_AFTER_CHAT trigger）
		if memoryMode == "cloud" {
			assistantMessage := finalAnswerContent
			if assistantMessage == "" {
				assistantMessage = strings.Join(finalAnswerChunks, "")
			}
			extensions.TriggerMemoryAfterChat(ctx, request, assistantMessage, filepath.Join(workspace.AgentRootDir(), "home"), parseRequestParams(request))
		}
		resultCh <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
	}()

	// 9. 提交流式任务
	// Auto-Harness resume 绕过 Session 队列（对齐 Python: interface.py:1007-1012）
	// 注意：Go 中流式请求已直接通过 goroutine 执行，不经过 SubmitAndWait 串行化，
	// EnsureSessionProcessor 仅确保 session 有追踪/取消能力。
	if isAutoResume {
		logger.Info(logComponent).
			Str("request_id", request.RequestID).
			Str("session_id", sessionID).
			Msg("auto-harness resume request")
	} else {
		// ⤵️ 10.6.19-23: Team 后续请求绕过 Session 队列（等待 TeamManager）
		// S09: 缺少 is_team_first_request 判断逻辑，Team 模式下后续请求会错误排队
		// Python 通过 team_manager.active_session_id / pending_session_id / has_stream_task 判断首次/后续
		_, _ = uc.coordinator.RegisterSession(sessionID, "", swsession.SessionPersistencePolicyPersistent)
	}

	return resultCh, nil
}

// ProcessInterrupt 处理中断请求。
// Python: JiuWenClaw._process_interrupt(request)
func (uc *UapClawSwarm) ProcessInterrupt(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	intent := uc.extractIntent(request)
	sessionID := session.NormalizeSessionID(uc.extractSessionID(request))

	// Team 模式分流（对齐 Python: interface.py:702-763）
	if isTeamMode(request) {
		return uc.processTeamInterrupt(ctx, request, intent, sessionID)
	}

	mode := uc.adapterModeForRequest(request)
	a, err := uc.ensureAdapter(mode)
	if err != nil {
		return nil, err
	}

	// 暂停/恢复
	if intent == "pause" || intent == "resume" {
		return a.ProcessInterrupt(ctx, request)
	}

	// 补充信息
	if intent == "supplement" {
		resp, err := a.ProcessInterrupt(ctx, request)
		_, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
			RequestID: &sessionID,
		})
		return resp, err
	}

	// cancel（默认）
	resp, err := a.ProcessInterrupt(ctx, request)
	uc.cancelTeamWorkForSession(sessionID, request.ChannelID, "interrupt(intent="+intent+"): ")
	waitTimeout := 5 * time.Second
	_, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
		RequestID:   &sessionID,
		WaitTimeout: &waitTimeout,
	})
	return resp, err
}

// GetContextUsage 获取上下文使用量。
// Python: JiuWenClaw.get_context_usage(session_id) → adapter.GetContextUsage
func (uc *UapClawSwarm) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
	a, err := uc.ensureAdapter("agent")
	if err != nil {
		return map[string]any{"usage": 0, "limit": 0}, err
	}
	cc, ok := a.(adapter.ContextCompressor)
	if !ok {
		return map[string]any{"usage": 0, "limit": 0}, nil
	}
	return cc.GetContextUsage(ctx, sessionID)
}

// CompressContext 压缩上下文。
// Python: JiuWenClaw.compress_context(session_id, return_state=True) → adapter.CompressContext
func (uc *UapClawSwarm) CompressContext(ctx context.Context, sessionID string) (map[string]any, error) {
	a, err := uc.ensureAdapter("agent")
	if err != nil {
		return map[string]any{"ok": false, "compressed": false}, err
	}
	cc, ok := a.(adapter.ContextCompressor)
	if !ok {
		return map[string]any{"ok": false, "compressed": false}, nil
	}
	// session=nil 安全：DeepAdapter.CompressContext 内部通过 WithCompressSessionID 传递 sessionID，
	// contextEngine 做 sess → opt.SessionID → defaultSessionID 三层 fallback。
	return cc.CompressContext(ctx, sessionID, nil, true)
}

// GenerateRecap 生成会话回顾。
// Python: JiuWenClaw.generate_recap(session_id) → adapter.GenerateRecap
func (uc *UapClawSwarm) GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error) {
	a, err := uc.ensureAdapter("agent")
	if err != nil {
		return map[string]any{"status": "failed", "error": err.Error()}, err
	}
	cc, ok := a.(adapter.ContextCompressor)
	if !ok {
		return map[string]any{"status": "failed", "error": "adapter 未实现 ContextCompressor"}, nil
	}
	return cc.GenerateRecap(ctx, sessionID)
}

// SwitchMode 切换运行模式，执行完整的 session 生命周期。
// 流程：preRun → switchMode → loadState → updateState → postRun
//
// Python: jiuwenswarm/server/agent_ws_server.py:1145-1154
func (uc *UapClawSwarm) SwitchMode(ctx context.Context, sessionID, subMode string) error {
	if uc.adapter == nil {
		return nil
	}
	return uc.adapter.SwitchMode(ctx, sessionID, subMode)
}

// CreateInstance 创建 Agent 实例。
// Python: JiuWenClaw.create_instance(config=None, *, mode="agent", sub_mode=None)
func (uc *UapClawSwarm) CreateInstance(opts ...CreateInstanceOption) error {
	cfg := defaultCreateInstanceConfig()
	for _, o := range opts {
		o(&cfg)
	}
	a, err := uc.ensureAdapter(cfg.mode)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := a.CreateInstance(ctx, cfg.config, cfg.mode, cfg.subMode); err != nil {
		return err
	}
	logger.Info(logComponent).
		Str("sdk", adapter.ResolveSDKChoice()).
		Str("mode", cfg.mode).
		Str("sub_mode", cfg.subMode).
		Msg("UapClawSwarm Agent instance created")
	// 启动 dreaming 后台任务（对齐 Python: interface.py:319-322）
	if dreamer, ok := a.(adapter.DreamingController); ok {
		go func() {
			_ = dreamer.TryStartDreaming(uc.appCtx, func() bool {
				return hasActiveExecutions(uc.coordinator)
			})
		}()
	}
	return nil
}

// ReloadAgentConfig 重载 Agent 配置。
// Python: JiuWenClaw.reload_agent_config(config_base, env_overrides)
func (uc *UapClawSwarm) ReloadAgentConfig(configBase map[string]any, envOverrides map[string]any) error {
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// 停止 dreaming（对齐 Python: interface.py:336-337）
	if dreamer, ok := a.(adapter.DreamingController); ok {
		_ = dreamer.TryStopDreaming(uc.appCtx)
	}
	if err := a.ReloadAgentConfig(context.Background(), configBase, envOverrides); err != nil {
		return err
	}
	// 重启 dreaming（对齐 Python: interface.py:340-343）
	if dreamer, ok := a.(adapter.DreamingController); ok {
		go func() {
			_ = dreamer.TryStartDreaming(uc.appCtx, func() bool {
				return hasActiveExecutions(uc.coordinator)
			})
		}()
	}
	return nil
}

// CancelInflightWork 取消在途任务。
// Python: JiuWenClaw.cancel_inflight_work(log_prefix)
func (uc *UapClawSwarm) CancelInflightWork(reason string) error {
	_ = uc.coordinator.Close(nil, nil)
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// Python: abort_fn = getattr(adapter, "abort_on_gateway_disconnect", None)
	if aborter, ok := a.(interface {
		AbortOnGatewayDisconnect(ctx context.Context) error
	}); ok {
		if err := aborter.AbortOnGatewayDisconnect(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "GATEWAY_DISCONNECT_ABORT_FAILED").
				Msg("adapter.AbortOnGatewayDisconnect failed")
		}
	}
	return nil
}

// Cleanup 清理资源。
// Python: JiuWenClaw.cleanup()
func (uc *UapClawSwarm) Cleanup() error {
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapter = nil
	uc.adapterMu.Unlock()
	if a != nil {
		_ = a.Cleanup()
	}
	return nil
}

// GetInstance 获取底层 DeepAgent 实例。
// Python: JiuWenClaw.get_instance() → self._adapter._instance（返回 DeepAgent）
func (uc *UapClawSwarm) GetInstance() *harness.DeepAgent {
	if uc == nil || uc.adapter == nil {
		return nil
	}
	return uc.adapter.Instance()
}

// ListCustomAgents 实现 adapter.AgentConfigLister 接口。
func (b *agentConfigListerBridge) ListCustomAgents() []*types.AgentDefinition {
	return b.svc.ListCustomAgents()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultCreateInstanceConfig 返回 CreateInstance 默认配置。
func defaultCreateInstanceConfig() createInstanceConfig {
	return createInstanceConfig{mode: "agent"}
}

// isTeamMode 判断请求是否为 Team 模式。
// Python: is_team_mode = team_flag or (mode in {"team", "team.plan", "code.team"})
func isTeamMode(request *schema.AgentRequest) bool {
	params := parseRequestParams(request)
	if teamFlag, ok := params["team"].(bool); ok && teamFlag {
		return true
	}
	if mode, ok := params["mode"].(string); ok {
		return schema.IsTeamMode(mode)
	}
	return false
}

// isAutoHarnessResume 判断请求是否为 Auto-Harness resume。
// Python: is_auto_harness_resume = mode == "auto_harness" and isinstance(activate_response, dict)
func isAutoHarnessResume(request *schema.AgentRequest) bool {
	params := parseRequestParams(request)
	if mode, ok := params["mode"].(string); ok {
		if strings.TrimSpace(strings.ToLower(mode)) == "auto_harness" {
			if _, ok2 := params["activate_response"].(map[string]any); ok2 {
				return true
			}
		}
	}
	return false
}

// processTeamInterrupt 处理 Team 模式中断请求。
// Python: JiuWenClaw._process_team_interrupt()
func (uc *UapClawSwarm) processTeamInterrupt(
	ctx context.Context,
	request *schema.AgentRequest,
	intent string,
	sessionID string,
) (*schema.AgentResponse, error) {
	switch intent {
	case "resume":
		// Python: resume 直接返回提示信息
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(true),
			schema.WithResponsePayload(map[string]any{
				"event_type": "chat.interrupt_result",
				"intent":     intent,
				"success":    true,
				"message":    "团队暂停后，直接发送下一条消息即可继续。",
			}),
		), nil

	case "pause":
		// ⤵️ 10.6.19-23: team_manager.pause_session_runtime(sessionID, reason)
		// S10: paused 硬编码为 false，Team pause 功能完全无效，依赖 TeamManager
		paused := false
		// Python: paused = teamManager.PauseSessionRuntime(ctx, sessionID, reason)
		_, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
			RequestID: &sessionID,
		})
		message := "团队已暂停"
		if !paused {
			message = "当前没有可暂停的团队任务"
		}
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(true),
			schema.WithResponsePayload(map[string]any{
				"event_type": "chat.interrupt_result",
				"intent":     intent,
				"success":    paused,
				"message":    message,
			}),
		), nil

	case "cancel":
		// ⤵️ 10.6.19-23: team_manager.cancel_session_runtime(sessionID, reason)
		// S10: cancelled 硬编码为 false，Team cancel 功能完全无效，依赖 TeamManager
		cancelled := false
		// Python: cancelled = teamManager.CancelSessionRuntime(ctx, sessionID, reason)
		_, _ = uc.coordinator.CancelExecution(sessionID, swsession.CancelExecutionOptions{
			RequestID: &sessionID,
		})
		message := "团队当前执行已结束"
		if !cancelled {
			message = "当前没有可取消的团队任务"
		}
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(true),
			schema.WithResponsePayload(map[string]any{
				"event_type": "chat.interrupt_result",
				"intent":     intent,
				"success":    cancelled,
				"message":    message,
			}),
		), nil

	default:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(true),
			schema.WithResponsePayload(map[string]any{
				"event_type": "chat.interrupt_result",
				"intent":     intent,
				"success":    false,
				"message":    "团队模式暂不支持中断意图: " + intent,
			}),
		), nil
	}
}

// cancelTeamWorkForSession 终止当前 session 的 Team runtime（若存在）。
// Python: JiuWenClaw._cancel_team_work_for_session()
func (uc *UapClawSwarm) cancelTeamWorkForSession(sessionID string, channelID string, logPrefix string) bool {
	// ⤵️ 10.6.19-23: get_team_manager + terminate_session_runtime
	// T09: CodeAdapter 缺少 configure_team_member_agent，Team 模式下 code 成员 Agent 无法正确配置
	// Python: teamManager := getTeamManager(channelID)
	// Python: return teamManager.TerminateSessionRuntime(ctx, sessionID, logPrefix)
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Msg("cancelTeamWorkForSession awaiting TeamManager backfill (10.6.19-23)")
	return false
}

// ensureAdapter 确保 SDK adapter 已初始化，幂等。
func (uc *UapClawSwarm) ensureAdapter(mode string) (adapter.AgentAdapter, error) {
	uc.adapterMu.Lock()
	defer uc.adapterMu.Unlock()
	if uc.adapter != nil {
		return uc.adapter, nil
	}
	a, err := adapter.CreateAdapter("", mode)
	if err != nil {
		return nil, err
	}
	// 若 adapter 有 SetSkillManager 方法，注入 skillManager
	if setter, ok := a.(interface{ SetSkillManager(*skill.SkillManager) }); ok {
		setter.SetSkillManager(uc.skillManager)
	}
	// 若 adapter 有 SetConfigLister 方法，注入 agentConfigService 桥接
	if setter, ok := a.(interface {
		SetConfigLister(adapter.AgentConfigLister)
	}); ok {
		setter.SetConfigLister(&agentConfigListerBridge{svc: uc.agentConfigService})
	}
	// 注册 SkillNet 安装完成后的回调（对齐 Python: interface.py:288-289）
	uc.skillManager.SetSkillnetInstallCompleteHook(func(ctx context.Context) error {
		return uc.CreateInstance(WithCreateInstanceConfig(nil))
	})
	uc.adapter = a
	logger.Info(logComponent).
		Str("sdk", adapter.ResolveSDKChoice()).
		Str("mode", mode).
		Msg("UapClawSwarm adapter initialized")
	return a, nil
}

// ensureSkillDevService 确保 SkillDevService 已初始化，幂等。
// SkillDevService 是无状态的，单实例即可服务所有请求。
// 首次调用时构造完整依赖：stateStore/workspaceProvider 基于 workspace 下
// skilldev 目录，模型配置从 config models.default 提取，
// MCP 工具工厂注入 swarmtools.GetMcpTools。
// Python: JiuWenClaw._get_skilldev_service (interface.py:247-279)
func (uc *UapClawSwarm) ensureSkillDevService() (*skilldev.SkillDevService, error) {
	uc.skilldevMu.Lock()
	defer uc.skilldevMu.Unlock()
	if uc.skilldevService != nil {
		return uc.skilldevService, nil
	}
	// 对齐 Python interface.py:261-263: get_workspace_dir() / "skilldev"
	skilldevBase := filepath.Join(workspace.WorkspaceDir(), "skilldev")
	stateStore := skilldev.NewStateStore(skilldevBase)
	workspaceProvider := skilldev.NewWorkspaceProvider(skilldevBase)

	// 对齐 Python interface.py:265-267: config.get("models", {}).get("default", {})
	// （get_config 含 _normalize_config——custom_headers 结构化——2026-10-28 审查接线）
	modelName := ""
	modelClientConfig := map[string]any{}
	if c, cfgErr := config.New("", config.WithNormalize(config.NormalizeConfig)); cfgErr == nil {
		if loaded, loadErr := c.Load(); loadErr == nil {
			if models, ok := loaded["models"].(map[string]any); ok {
				if defaultModel, ok := models["default"].(map[string]any); ok {
					if n, ok := defaultModel["model_name"].(string); ok {
						modelName = n
					}
					if mcc, ok := defaultModel["model_client_config"].(map[string]any); ok {
						modelClientConfig = mcc
					}
				}
			}
		}
	}

	deps := &skilldev.SkillDevDeps{
		ModelName:         modelName,
		ModelClientConfig: modelClientConfig,
		MCPToolsFactory:   swarmtools.GetMcpTools, // 对齐 Python: mcp_tools_factory=get_mcp_tools
		SysOpConfig:       nil,                    // 对齐 Python: sysop_config=None
		StateStore:        stateStore,
		WorkspaceProvider: workspaceProvider,
	}
	svc := skilldev.NewSkillDevService(deps)
	uc.skilldevService = svc
	logger.Info(logComponent).
		Msg("UapClawSwarm SkillDevService initialized with full deps")
	return svc, nil
}

// adapterModeForRequest 从请求参数中提取 adapter mode。
// Python: _adapter_mode_for_request：strip+lower + team.plan→code + code.*→code。
func (uc *UapClawSwarm) adapterModeForRequest(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if modeVal, ok := params["mode"]; ok {
		if modeStr, ok := modeVal.(string); ok && modeStr != "" {
			// 三段 canonical 按串语义解析（code environment → code 适配器）
			if managerMode, _, _, ok := schema.ResolveNewCanonicalMode(modeStr); ok {
				return managerMode
			}
			modeText := strings.TrimSpace(strings.ToLower(modeStr))
			// team.plan 映射为 code 模式
			if modeText == "team.plan" {
				return "code"
			}
			parts := strings.SplitN(modeText, ".", 2)
			return parts[0]
		}
	}
	return "agent"
}

// extractSessionID 从请求中提取 sessionID 字符串。
func (uc *UapClawSwarm) extractSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil {
		return *request.SessionID
	}
	return ""
}

// extractQuery 从请求参数中提取 query 字段。
func (uc *UapClawSwarm) extractQuery(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if q, ok := params["query"]; ok {
		if qStr, ok := q.(string); ok {
			return qStr
		}
	}
	return ""
}

// extractResponseContent 从响应中提取 content。
func (uc *UapClawSwarm) extractResponseContent(resp *schema.AgentResponse) string {
	if resp.Payload == nil {
		return ""
	}
	if content, ok := resp.Payload["content"]; ok {
		if cStr, ok := content.(string); ok {
			return cStr
		}
	}
	return ""
}

// extractIntent 从请求参数中提取 intent（默认 "cancel"）。
func (uc *UapClawSwarm) extractIntent(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if intent, ok := params["intent"]; ok {
		if intentStr, ok := intent.(string); ok && intentStr != "" {
			return intentStr
		}
	}
	return "cancel"
}

// payloadExtras 构造 history 记录的附加字段。
// 对齐 Python: interface.py:1063/1102 —— 取 payload 中除 event_type/content 外的全部字段。
// 注意保留 team.message 的嵌套 event（前端 extractTeamEventRecord 一级回退依赖它）。
func payloadExtras(payload map[string]any) map[string]any {
	extra := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == "event_type" || k == "content" {
			continue
		}
		extra[k] = v
	}
	return extra
}

// mergeTeamEventExtras 把 team.message 的 event 子字段展平到 extra 顶层。
// 对齐 Python: interface.py:1064-1070 —— event 里的 type/timestamp/content 不展平
// （它们属于事件元信息，且嵌套 event 已整体保留）。
func mergeTeamEventExtras(extra, payload map[string]any) map[string]any {
	if extra == nil || payload == nil {
		return extra
	}
	eventType, _ := payload["event_type"].(string)
	if eventType != "team.message" {
		return extra
	}
	eventData, ok := payload["event"].(map[string]any)
	if !ok {
		return extra
	}
	for k, v := range eventData {
		if k == "type" || k == "timestamp" || k == "content" {
			continue
		}
		extra[k] = v
	}
	return extra
}

// extractChunkContent 从 chunk payload 中提取 history 记录的 content。
// Python: interface.py:1085 —— payload.get("content") or payload.get("error") or ""
func extractChunkContent(payload map[string]any) string {
	if content, ok := payload["content"].(string); ok && content != "" {
		return content
	}
	if errText, ok := payload["error"].(string); ok {
		return errText
	}
	return ""
}

// shouldRecordHistory 判断 event_type 是否需要记录到 history。
// Python: should_record = et.startswith("chat.") or et == "team.message"
func shouldRecordHistory(eventType string) bool {
	if strings.HasPrefix(eventType, "chat.") {
		return true
	}
	// Python: team.message 也记录 history
	return eventType == "team.message"
}

// handleSkillsRequest 处理 skills.* 请求。
func (uc *UapClawSwarm) handleSkillsRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	if uc.skillManager == nil {
		return nil, nil
	}
	// Python: 有 pending 的 skillnet_install 时，阻止其他 skills 操作
	if uc.skillManager.HasPendingSkillnetInstall() {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithResponsePayload(map[string]any{
				"error": "有 SkillNet 安装正在进行中，请等待完成后再操作",
			}),
		), nil
	}
	handler, ok := skill.SkillRoutes[request.ReqMethod]
	if !ok {
		return nil, nil
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		params = make(map[string]any)
	}
	result, err := handler(uc.skillManager, ctx, params)
	if err != nil {
		return nil, err
	}
	// 若方法需要重建 Agent 实例
	// Python: skillnet_install 且 pending 时不重建，等安装完成回调触发
	if skill.NeedsRebuild(request.ReqMethod) {
		if request.ReqMethod == schema.ReqMethodSkillsSkillnetInstall {
			if pending, _ := result["pending"].(bool); pending {
				// SkillNet 安装尚在进行中，等完成回调触发重建
			} else {
				_ = uc.CreateInstance()
			}
		} else {
			_ = uc.CreateInstance()
		}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithResponsePayload(result),
	), nil
}

// handleSkillDevRequest 处理 skilldev.* 请求（非流式）。
// 消费 chunk channel，收集所有 payload 后打包为 AgentResponse。
func (uc *UapClawSwarm) handleSkillDevRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	if !skill.IsSkillDevMethod(request.ReqMethod) {
		return nil, nil
	}
	svc, err := uc.ensureSkillDevService()
	if err != nil {
		return nil, err
	}
	chunkCh, err := svc.Handle(ctx, request)
	if err != nil {
		return nil, err
	}
	// 收集所有 chunk 的 payload
	var events []map[string]any
	for chunk := range chunkCh {
		events = append(events, chunk.Payload)
	}
	payload := map[string]any{
		"ok":     true,
		"events": events,
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithResponsePayload(payload),
	), nil
}

// handlePluginsRequest 处理 plugins.* 请求。
func (uc *UapClawSwarm) handlePluginsRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	if uc.skillManager == nil {
		return nil, nil
	}
	handler, ok := skill.PluginRoutes[request.ReqMethod]
	if !ok {
		return nil, nil
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		params = make(map[string]any)
	}
	result, err := handler(uc.skillManager, ctx, params)
	if err != nil {
		return nil, err
	}
	if skill.NeedsRebuild(request.ReqMethod) {
		_ = uc.CreateInstance()
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithResponsePayload(result),
	), nil
}

// handleSkillDevStreamRequest 处理 skilldev.* 流式请求。
// Handle 现在直接返回 chunk channel，此处只需追加终止哨兵。
func (uc *UapClawSwarm) handleSkillDevStreamRequest(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	svc, err := uc.ensureSkillDevService()
	if err != nil {
		ch := make(chan *schema.AgentResponseChunk, 1)
		ch <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
			map[string]any{"event_type": "skilldev.error", "error": err.Error()},
		)
		ch <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
		close(ch)
		return ch, nil
	}
	// Handle 现在直接返回 chunk channel
	chunkCh, err := svc.Handle(ctx, request)
	if err != nil {
		ch := make(chan *schema.AgentResponseChunk, 1)
		ch <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
			map[string]any{"event_type": "skilldev.error", "error": err.Error()},
		)
		ch <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
		close(ch)
		return ch, nil
	}
	// 包装：追加终止哨兵
	resultCh := make(chan *schema.AgentResponseChunk, 64)
	go func() {
		defer close(resultCh)
		for chunk := range chunkCh {
			resultCh <- chunk
		}
		resultCh <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
	}()
	return resultCh, nil
}

// truncateStr 截断字符串到指定长度，超过时追加 "..."。
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// PlanStateAccessor 返回 plan 状态读写入口。
func (uc *UapClawSwarm) PlanStateAccessor() swruntime.PlanStateAccessor {
	provider, ok := uc.adapter.(interface {
		PlanStateAccessor() swruntime.PlanStateAccessor
	})
	if !ok {
		return nil
	}
	return provider.PlanStateAccessor()
}

// PlanModeSnapshot 只读当前会话 plan 模式（不做任何状态写入）。
// Python: DeepAgentState.plan_mode（session.plan_status 用）
func (uc *UapClawSwarm) PlanModeSnapshot(ctx context.Context, sessionID string) (string, bool) {
	acc := uc.PlanStateAccessor()
	if acc == nil || sessionID == "" {
		return "", false
	}
	st, err := acc.LoadPlanState(ctx, sessionID)
	if err != nil {
		return "", false
	}
	return st.Mode, true
}

// hasActiveExecutions 是否有任何非终态执行。
// 取代 SessionManager.HasActiveTasks()（10.3.28d）。
func hasActiveExecutions(c *swsession.RuntimeSessionCoordinator) bool {
	for _, snap := range c.SnapshotSessions() {
		for _, exec := range snap.Executions {
			if !exec.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// facadeUsesSessionRuntime 门面侧判定这次请求是否归 Runtime 调度。
// 委托 runtime 包的 UsesSessionRuntime（上游 uses_session_runtime）。
func facadeUsesSessionRuntime(request *schema.AgentRequest) bool {
	return swruntime.UsesSessionRuntime(request, false)
}

// HasSessionRuntime 门面是否还持有该会话运行时。
// Python: interface.py has_session_runtime（:6188）
//
// 两侧都查：门面会话队列 + adapter 侧会话运行时。任一为真即认为仍占用，
// 这样 cleanup_session_runtime 不会漏掉"队列还排着活"的会话。
func (uc *UapClawSwarm) HasSessionRuntime(sessionID string) bool {
	if uc.sessionManager != nil && uc.sessionManager.HasSessionRuntime(&sessionID) {
		return true
	}
	adapterValue := uc.adapterSnapshot()
	if adapterValue == nil {
		return false
	}
	return adapterValue.HasSessionRuntime(sessionID)
}

// CleanupSessionRuntime 释放该会话在门面与 adapter 侧的内存运行时，保留持久历史。
// Python: interface.py cleanup_session_runtime（:6176）
//
// 两半顺序与上游一致：先关门面队列，再清 adapter 侧缓存。
func (uc *UapClawSwarm) CleanupSessionRuntime(ctx context.Context, sessionID string) error {
	if uc.sessionManager != nil {
		if _, err := uc.sessionManager.CloseSession(ctx, sessionID, nil); err != nil {
			return err
		}
	}
	adapterValue := uc.adapterSnapshot()
	if adapterValue == nil {
		return nil
	}
	if !adapterValue.CleanupSessionAdapter(sessionID) {
		logger.Debug(logComponent).Str("session_id", sessionID).
			Msg("adapter side cleanup_session_adapter found nothing to clean")
	}
	return nil
}

// adapterSnapshot 并发安全地取当前 adapter（未初始化返回 nil）。
func (uc *UapClawSwarm) adapterSnapshot() adapter.AgentAdapter {
	uc.adapterMu.Lock()
	defer uc.adapterMu.Unlock()
	return uc.adapter
}
