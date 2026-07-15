package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill/skilldev"
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

	// sessionManager 会话任务队列管理器。
	sessionManager *SessionManager

	// skilldevService SkillDev 服务（懒初始化，ensureSkillDevService 时创建）。
	skilldevService *skilldev.SkillDevService

	// adapterMu 保护 adapter 字段的并发访问。
	adapterMu sync.Mutex

	// skilldevMu 保护 skilldevService 字段的并发访问。
	skilldevMu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUapClaw 创建 UapClaw 实例。
//
// 对齐 Python: JiuWenClaw.__init__()
func NewUapClaw() *UapClaw {
	return &UapClaw{
		sessionManager: NewSessionManager(),
		skillManager:   skill.NewSkillManager(workspace.AgentWorkspaceDir()),
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
	sessionID := normalizeSessionID(uc.extractSessionID(request))

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
	AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", uc.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, request.Metadata, userMode)

	// 构建 inputs
	inputs, _, _ := uc.BuildInputs(request)

	// ⤵️ 10.3.2: cloud memory before-chat hook（ExtensionRegistry）

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
		AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
			"assistant", content, float64(time.Now().UnixMilli())/1000,
			"chat.final", nil, nil, assistantMode)
	}

	// ⤵️ 10.3.2: cloud memory after-chat hook

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
	sessionID := normalizeSessionID(uc.extractSessionID(request))

	// ⤵️ 10.3.2: Team 模式判断（isTeamMode / isAutoHarnessResume）
	// ⤵️ 10.3.2: Team 模式使用原始 query（不经过 BuildUserPrompt 包装）

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
	AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", uc.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, nil, userMode)

	// 5. 构建 inputs
	inputs, _, _ := uc.BuildInputs(request)

	// ⤵️ 10.3.2: cloud memory before-chat hook

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
			AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
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
							AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
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
							AppendCompactHistoryFromPayload(payload, sessionID, request.RequestID, request.ChannelID, compMode)
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
		// ⤵️ 10.3.2: cloud memory after-chat hook
		_ = finalAnswerContent
		_ = finalAnswerChunks
		resultCh <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
	}()

	// 9. 提交流式任务
	// ⤵️ 10.3.2: Team 后续请求 / Auto-Harness resume 绕过 Session 队列
	_ = uc.sessionManager.EnsureSessionProcessor(ctx, sessionID)

	return resultCh, nil
}

// ProcessInterrupt 处理中断请求。
//
// 对齐 Python: JiuWenClaw._process_interrupt(request)
func (uc *UapClaw) ProcessInterrupt(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	intent := uc.extractIntent(request)
	sessionID := normalizeSessionID(uc.extractSessionID(request))

	// ⤵️ 10.3.2: Team 模式分流（_processTeamInterrupt）

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
	// ⤵️ 10.3.2: cancelTeamWorkForSession(sessionID, channelID)
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

// SwitchMode 切换运行模式。
// ⤵️ 10.3.2: 完整实现需要 DeepAdapter 支撑（session 持久化 + switch_mode + load_state）
func (uc *UapClaw) SwitchMode(_, _ string) error { return nil }

// CreateInstance 创建 Agent 实例。
//
// 对齐 Python: JiuWenClaw.create_instance(config, mode, sub_mode)
func (uc *UapClaw) CreateInstance(config map[string]any, mode string, subMode string) error {
	a, err := uc.ensureAdapter(mode)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := a.CreateInstance(ctx, config, mode, subMode); err != nil {
		return err
	}
	logger.Info(logComponent).
		Str("sdk", adapter.ResolveSDKChoice()).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Msg("UapClaw Agent 实例已创建")
	// ⤵️ 10.3.2: 启动 dreaming 后台任务（adapter.TryStartDreaming）
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
	// ⤵️ 10.3.2: adapter.TryStopDreaming()
	if err := a.ReloadAgentConfig(context.Background(), configBase, envOverrides); err != nil {
		return err
	}
	// ⤵️ 10.3.2: adapter.TryStartDreaming()
	return nil
}

// CancelInflightWork 取消在途任务。
//
// 对齐 Python: JiuWenClaw.cancel_inflight_work()
func (uc *UapClaw) CancelInflightWork() error {
	_ = uc.sessionManager.CancelAllSessionTasks(context.Background(), "[gateway disconnect]")
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// ⤵️ 10.3.2: adapter.AbortOnGatewayDisconnect()
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

// ──────────────────────────── 非导出函数 ────────────────────────────

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
	// ⤵️ G33: 调用 uc.skillManager.SetSkillnetInstallCompleteHook(uc.CreateInstance) 注入 hook
	// ⤵️ G34: 启动 dreaming 后台任务（adapter.TryStartDreaming）
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
	if skill.NeedsRebuild(request.ReqMethod) {
		_ = uc.CreateInstance(nil, "", "")
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
		_ = uc.CreateInstance(nil, "", "")
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
