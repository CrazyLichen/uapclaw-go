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
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UapClaw Agent 统一门面。
//
// 提供：SDK 适配器路由、统一对外 API、公共编排
// （session 队列、Skills 路由、heartbeat、流式包装）。
//
// 对齐 Python: jiuwenswarm/server/runtime/agent_adapter/interface.py (JiuWenClaw)
type UapClaw struct {
	// adapter SDK 适配器（延迟初始化，ensureAdapter 时创建）。
	adapter adapter.AgentAdapter

	// skillManager 技能管理器（server 层）。
	skillManager *skill.SkillManager

	// agentConfigService Agent 配置 CRUD 服务。
	agentConfigService *AgentConfigService

	// sessionManager 会话任务队列管理器。
	sessionManager *session.SessionManager

	// skilldevService SkillDev 服务（懒初始化，ensureSkillDevService 时创建）。
	skilldevService *skilldev.SkillDevService

	// adapterMu 保护 adapter 字段的并发访问。
	adapterMu sync.Mutex

	// skilldevMu 保护 skilldevService 字段的并发访问。
	skilldevMu sync.Mutex
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

var ()

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

// NewUapClaw 创建 UapClaw 实例。
//
// 对齐 Python: JiuWenClaw.__init__()
func NewUapClaw() *UapClaw {
	return &UapClaw{
		sessionManager:     session.NewSessionManager(),
		skillManager:       skill.NewSkillManager(workspace.AgentWorkspaceDir()),
		agentConfigService: NewAgentConfigService(workspace.WorkspaceDir()),
	}
}

// ProcessMessage 处理非流式 Agent 请求。
//
// 对齐 Python: JiuWenClaw.process_message(request)
func (uc *UapClaw) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 对齐 Python logger.info：请求日志
	sessionIDForLog := ""
	if request.SessionID != nil {
		sessionIDForLog = *request.SessionID
	}
	logger.Info(logComponent).
		Str("event_type", "process_message").
		Str("request_id", request.RequestID).
		Str("channel_id", request.ChannelID).
		Str("session_id", sessionIDForLog).
		Msg("处理非流式请求")

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
		extReg, extErr := extensions.GetInstanceErr()
		if extErr == nil && extReg != nil {
			channelID := request.ChannelID
			sid := ""
			if request.SessionID != nil {
				sid = *request.SessionID
			}
			memCtx := &extensions.MemoryHookContext{
				SessionID:    sid,
				RequestID:    request.RequestID,
				ChannelID:    &channelID,
				AgentName:    "main_agent",
				WorkspaceDir: filepath.Join(workspace.AgentRootDir(), "home"),
				Extra:        parseRequestParams(request),
			}
			extReg.Trigger(ctx, extensions.AgentServerMemoryBeforeChat, memCtx.ToMap())
			// 从 memCtx.MemoryBlocks 拼接记忆注入 inputs
			memoryBlock := strings.Join(memCtx.MemoryBlocks, "\n\n")
			if memoryBlock != "" {
				inputs["memory_block"] = memoryBlock
			}
		}
	}

	// 提交到 session 队列并等待结果
	result, err := uc.sessionManager.SubmitAndWait(ctx, sessionID, func(taskCtx context.Context) (any, error) {
		return a.ProcessMessageImpl(taskCtx, request, inputs)
	})
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
	if resp.OK {
		content := uc.extractResponseContent(resp)
		// 对齐 Python：补传 extra 和 mode
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
			"assistant", content, float64(time.Now().UnixMilli())/1000,
			"chat.final", nil, nil, assistantMode)
	}

	// 云端记忆对话后钩子（对齐 Python: interface.py:866-877 MEMORY_AFTER_CHAT trigger）
	if memoryMode == "cloud" && resp.OK {
		extReg, extErr := extensions.GetInstanceErr()
		if extErr == nil && extReg != nil {
			content := uc.extractResponseContent(resp)
			channelID := request.ChannelID
			sid := ""
			if request.SessionID != nil {
				sid = *request.SessionID
			}
			afterCtx := &extensions.MemoryHookContext{
				SessionID:        sid,
				RequestID:        request.RequestID,
				ChannelID:        &channelID,
				AgentName:        "main_agent",
				WorkspaceDir:     filepath.Join(workspace.AgentRootDir(), "home"),
				AssistantMessage: &content,
				Extra:            parseRequestParams(request),
			}
			extReg.Trigger(ctx, extensions.AgentServerMemoryAfterChat, afterCtx.ToMap())
		}
	}

	return resp, nil
}

// ProcessMessageStream 处理流式 Agent 请求。
//
// 对齐 Python: JiuWenClaw.process_message_stream(request)
func (uc *UapClaw) ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	// 对齐 Python logger.info：流式请求日志
	sessionIDForLog := ""
	if request.SessionID != nil {
		sessionIDForLog = *request.SessionID
	}
	logger.Info(logComponent).
		Str("event_type", "process_message_stream").
		Str("request_id", request.RequestID).
		Str("channel_id", request.ChannelID).
		Str("session_id", sessionIDForLog).
		Msg("处理流式请求")

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
		"", nil, nil, userMode)

	// 5. 构建 inputs
	inputs, memoryMode, rawQuery := uc.BuildInputs(request)

	// Team 模式：使用原始 query，不经过 BuildUserPrompt 包装（对齐 Python: interface.py:940-949）
	if isTeam {
		if _, ok := inputs["query"].(*interaction.InteractiveInput); !ok {
			inputs["query"] = rawQuery
			logger.Info(logComponent).
				Str("event_type", "team_raw_query").
				Str("raw_query", truncateStr(rawQuery, 100)).
				Msg("Team 模式使用原始 query")
		}
	}

	// 云端记忆对话前钩子（对齐 Python: interface.py:951-963 MEMORY_BEFORE_CHAT trigger）
	if memoryMode == "cloud" {
		extReg, extErr := extensions.GetInstanceErr()
		if extErr == nil && extReg != nil {
			channelID := request.ChannelID
			sid := ""
			if request.SessionID != nil {
				sid = *request.SessionID
			}
			memCtx := &extensions.MemoryHookContext{
				SessionID:    sid,
				RequestID:    request.RequestID,
				ChannelID:    &channelID,
				AgentName:    "main_agent",
				WorkspaceDir: filepath.Join(workspace.AgentRootDir(), "home"),
				Extra:        parseRequestParams(request),
			}
			extReg.Trigger(ctx, extensions.AgentServerMemoryBeforeChat, memCtx.ToMap())
			// 从 memCtx.MemoryBlocks 拼接记忆注入 inputs
			memoryBlock := strings.Join(memCtx.MemoryBlocks, "\n\n")
			if memoryBlock != "" {
				inputs["memory_block"] = memoryBlock
			}
		}
	}

	// 6. 创建中转 channel
	outCh := make(chan *schema.AgentResponseChunk, 64)
	streamDone := make(chan struct{})

	// 7. 生产者 goroutine
	go func() {
		defer close(streamDone)
		chunkCh, streamErr := a.ProcessMessageStreamImpl(ctx, request, inputs)
		if streamErr != nil {
			// 对齐 Python except asyncio.CancelledError：取消不作为错误
			if streamErr == context.Canceled || streamErr == context.DeadlineExceeded {
				return
			}
			// 对齐 Python: append_history_record(event_type="chat.error", ...)
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

		for {
			select {
			case chunk, ok := <-outCh:
				if !ok {
					goto streamComplete
				}
				if payload := chunk.Payload; payload != nil {
					if eventType, _ := payload["event_type"].(string); eventType != "" {
						if shouldRecordHistory(eventType) {
							// 对齐 Python：补传 mode
							streamMode := ""
							if p := parseRequestParams(request); p != nil {
								if m, ok := p["mode"].(string); ok {
									streamMode = m
								}
							}
							if streamMode == "" {
								streamMode = "unknown"
							}
							// 对齐 Python: team.message 展开 event 字段到 extra
							var extraFields map[string]any
							if eventType == "team.message" {
								if event, ok := payload["event"]; ok {
									if eventData, ok := event.(map[string]any); ok {
										extraFields = make(map[string]any)
										for k, v := range eventData {
											if k != "type" && k != "timestamp" && k != "content" {
												extraFields[k] = v
											}
										}
									}
								}
							}
							session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
								"assistant", extractChunkContent(payload), float64(time.Now().UnixMilli())/1000,
								eventType, extraFields, nil, streamMode)
						}
						// 对齐 Python: context_compression_state 事件写入 compact history
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
			case <-streamDone:
				for len(outCh) > 0 {
					resultCh <- <-outCh
				}
				goto streamComplete
			}
		}

	streamComplete:
		// 云端记忆对话后钩子（对齐 Python: interface.py:1134-1146 MEMORY_AFTER_CHAT trigger）
		if memoryMode == "cloud" {
			extReg, extErr := extensions.GetInstanceErr()
			if extErr == nil && extReg != nil {
				assistantMessage := finalAnswerContent
				if assistantMessage == "" {
					assistantMessage = strings.Join(finalAnswerChunks, "")
				}
				channelID := request.ChannelID
				sid := ""
				if request.SessionID != nil {
					sid = *request.SessionID
				}
				afterCtx := &extensions.MemoryHookContext{
					SessionID:        sid,
					RequestID:        request.RequestID,
					ChannelID:        &channelID,
					AgentName:        "main_agent",
					WorkspaceDir:     filepath.Join(workspace.AgentRootDir(), "home"),
					AssistantMessage: &assistantMessage,
					Extra:            parseRequestParams(request),
				}
				extReg.Trigger(ctx, extensions.AgentServerMemoryAfterChat, afterCtx.ToMap())
			}
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
			Msg("Auto-Harness resume 请求")
	} else {
		// ⤵️ 10.6.19-23: Team 后续请求绕过 Session 队列（等待 TeamManager）
		// S09: 缺少 is_team_first_request 判断逻辑，Team 模式下后续请求会错误排队
		// Python 通过 team_manager.active_session_id / pending_session_id / has_stream_task 判断首次/后续
		_ = uc.sessionManager.EnsureSessionProcessor(ctx, sessionID)
	}

	return resultCh, nil
}

// ProcessInterrupt 处理中断请求。
//
// 对齐 Python: JiuWenClaw._process_interrupt(request)
func (uc *UapClaw) ProcessInterrupt(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
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
		_ = uc.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(supplement)", nil)
		return resp, err
	}

	// cancel（默认）
	resp, err := a.ProcessInterrupt(ctx, request)
	uc.cancelTeamWorkForSession(sessionID, request.ChannelID, "interrupt(intent="+intent+"): ")
	waitTimeout := 5 * time.Second
	_ = uc.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(cancel)", &waitTimeout)
	return resp, err
}

// GetContextUsage 获取上下文使用量。
// 对齐 Python: JiuWenClaw.get_context_usage(session_id) → adapter.GetContextUsage
func (uc *UapClaw) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
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
// 对齐 Python: JiuWenClaw.compress_context(session_id, return_state=True) → adapter.CompressContext
func (uc *UapClaw) CompressContext(ctx context.Context, sessionID string) (map[string]any, error) {
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
// 对齐 Python: JiuWenClaw.generate_recap(session_id) → adapter.GenerateRecap
func (uc *UapClaw) GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error) {
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
// 对应 Python: jiuwenswarm/server/agent_ws_server.py:1145-1154
func (uc *UapClaw) SwitchMode(ctx context.Context, sessionID, subMode string) error {
	if uc.adapter == nil {
		return nil
	}
	return uc.adapter.SwitchMode(ctx, sessionID, subMode)
}

// CreateInstance 创建 Agent 实例。
//
// 对齐 Python: JiuWenClaw.create_instance(config=None, *, mode="agent", sub_mode=None)
func (uc *UapClaw) CreateInstance(opts ...CreateInstanceOption) error {
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
		Msg("UapClaw Agent 实例已创建")
	// 启动 dreaming 后台任务（对齐 Python: interface.py:319-322）
	if dreamer, ok := a.(adapter.DreamingController); ok {
		go func() {
			_ = dreamer.TryStartDreaming(context.Background(), func() bool {
				return uc.sessionManager.HasActiveTasks()
			})
		}()
	}
	return nil
}

// ReloadAgentConfig 重载 Agent 配置。
//
// 对齐 Python: JiuWenClaw.reload_agent_config(config_base, env_overrides)
func (uc *UapClaw) ReloadAgentConfig(configBase map[string]any, envOverrides map[string]any) error {
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// 停止 dreaming（对齐 Python: interface.py:336-337）
	if dreamer, ok := a.(adapter.DreamingController); ok {
		_ = dreamer.TryStopDreaming(context.Background())
	}
	if err := a.ReloadAgentConfig(context.Background(), configBase, envOverrides); err != nil {
		return err
	}
	// 重启 dreaming（对齐 Python: interface.py:340-343）
	if dreamer, ok := a.(adapter.DreamingController); ok {
		go func() {
			_ = dreamer.TryStartDreaming(context.Background(), func() bool {
				return uc.sessionManager.HasActiveTasks()
			})
		}()
	}
	return nil
}

// CancelInflightWork 取消在途任务。
//
// 对齐 Python: JiuWenClaw.cancel_inflight_work(log_prefix)
func (uc *UapClaw) CancelInflightWork(reason string) error {
	_ = uc.sessionManager.CancelAllSessionTasks(context.Background(), reason)
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// 对齐 Python: abort_fn = getattr(adapter, "abort_on_gateway_disconnect", None)
	if aborter, ok := a.(interface {
		AbortOnGatewayDisconnect(ctx context.Context) error
	}); ok {
		if err := aborter.AbortOnGatewayDisconnect(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "GATEWAY_DISCONNECT_ABORT_FAILED").
				Msg("adapter.AbortOnGatewayDisconnect 失败")
		}
	}
	return nil
}

// Cleanup 清理资源。
//
// 对齐 Python: JiuWenClaw.cleanup()
func (uc *UapClaw) Cleanup() error {
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
//
// 对齐 Python: JiuWenClaw.get_instance() → self._adapter._instance（返回 DeepAgent）
func (uc *UapClaw) GetInstance() *harness.DeepAgent { return nil }

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
// 对齐 Python: is_team_mode = team_flag or (mode in {"team", "team.plan", "code.team"})
func isTeamMode(request *schema.AgentRequest) bool {
	params := parseRequestParams(request)
	if teamFlag, ok := params["team"].(bool); ok && teamFlag {
		return true
	}
	if mode, ok := params["mode"].(string); ok {
		modeLower := strings.TrimSpace(strings.ToLower(mode))
		return modeLower == "team" || modeLower == "team.plan" || modeLower == "code.team"
	}
	return false
}

// isAutoHarnessResume 判断请求是否为 Auto-Harness resume。
// 对齐 Python: is_auto_harness_resume = mode == "auto_harness" and isinstance(activate_response, dict)
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
// 对齐 Python: JiuWenClaw._process_team_interrupt()
func (uc *UapClaw) processTeamInterrupt(
	ctx context.Context,
	request *schema.AgentRequest,
	intent string,
	sessionID string,
) (*schema.AgentResponse, error) {
	switch intent {
	case "resume":
		// 对齐 Python: resume 直接返回提示信息
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
		_ = uc.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(pause)", nil)
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
		_ = uc.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(cancel)", nil)
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
// 对齐 Python: JiuWenClaw._cancel_team_work_for_session()
func (uc *UapClaw) cancelTeamWorkForSession(sessionID string, channelID string, logPrefix string) bool {
	// ⤵️ 10.6.19-23: get_team_manager + terminate_session_runtime
	// T09: CodeAdapter 缺少 configure_team_member_agent，Team 模式下 code 成员 Agent 无法正确配置
	// teamManager := getTeamManager(channelID)
	// return teamManager.TerminateSessionRuntime(ctx, sessionID, logPrefix)
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Msg("cancelTeamWorkForSession 等待 TeamManager 回填（10.6.19-23）")
	return false
}

// ensureAdapter 确保 SDK adapter 已初始化，幂等。
func (uc *UapClaw) ensureAdapter(mode string) (adapter.AgentAdapter, error) {
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
		Msg("UapClaw adapter 已初始化")
	return a, nil
}

// ensureSkillDevService 确保 SkillDevService 已初始化，幂等。
//
// 对齐 Python：JiuWenClaw 中 _skilldev_service 在首次使用时懒初始化。
func (uc *UapClaw) ensureSkillDevService() (*skilldev.SkillDevService, error) {
	uc.skilldevMu.Lock()
	defer uc.skilldevMu.Unlock()
	if uc.skilldevService != nil {
		return uc.skilldevService, nil
	}
	// 构造默认 SkillDevDeps（零值依赖，各字段在 SkillDevDeps 内部懒加载）
	deps := &skilldev.SkillDevDeps{}
	svc := skilldev.NewSkillDevService(deps)
	uc.skilldevService = svc
	logger.Info(logComponent).
		Msg("UapClaw SkillDevService 已懒初始化")
	return svc, nil
}

// adapterModeForRequest 从请求参数中提取 adapter mode。
// 对齐 Python _adapter_mode_for_request：strip+lower + team.plan→code + code.*→code。
func (uc *UapClaw) adapterModeForRequest(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if modeVal, ok := params["mode"]; ok {
		if modeStr, ok := modeVal.(string); ok && modeStr != "" {
			modeText := strings.TrimSpace(strings.ToLower(modeStr))
			// team.plan 映射为 code 模式
			if modeText == "team.plan" {
				return "code"
			}
			// code.* 映射为 code 模式
			if strings.HasPrefix(modeText, "code.") {
				return "code"
			}
			parts := strings.SplitN(modeText, ".", 2)
			return parts[0]
		}
	}
	return "agent"
}

// extractSessionID 从请求中提取 sessionID 字符串。
func (uc *UapClaw) extractSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil {
		return *request.SessionID
	}
	return ""
}

// extractQuery 从请求参数中提取 query 字段。
func (uc *UapClaw) extractQuery(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if q, ok := params["query"]; ok {
		if qStr, ok := q.(string); ok {
			return qStr
		}
	}
	return ""
}

// extractResponseContent 从响应中提取 content。
func (uc *UapClaw) extractResponseContent(resp *schema.AgentResponse) string {
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
func (uc *UapClaw) extractIntent(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if intent, ok := params["intent"]; ok {
		if intentStr, ok := intent.(string); ok && intentStr != "" {
			return intentStr
		}
	}
	return "cancel"
}

// extractChunkContent 从 chunk payload 中提取 content。
func extractChunkContent(payload map[string]any) string {
	if content, ok := payload["content"]; ok {
		if cStr, ok := content.(string); ok {
			return cStr
		}
	}
	return ""
}

// shouldRecordHistory 判断 event_type 是否需要记录到 history。
// 对齐 Python: should_record = et.startswith("chat.") or et == "team.message"
func shouldRecordHistory(eventType string) bool {
	if strings.HasPrefix(eventType, "chat.") {
		return true
	}
	// 对齐 Python: team.message 也记录 history
	return eventType == "team.message"
}

// handleSkillsRequest 处理 skills.* 请求。
func (uc *UapClaw) handleSkillsRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	if uc.skillManager == nil {
		return nil, nil
	}
	// 对齐 Python：有 pending 的 skillnet_install 时，阻止其他 skills 操作
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
	// 对齐 Python: skillnet_install 且 pending 时不重建，等安装完成回调触发
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
//
// 消费 chunk channel，收集所有 payload 后打包为 AgentResponse。
func (uc *UapClaw) handleSkillDevRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
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
func (uc *UapClaw) handlePluginsRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
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
//
// Handle 现在直接返回 chunk channel，此处只需追加终止哨兵。
func (uc *UapClaw) handleSkillDevStreamRequest(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
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
