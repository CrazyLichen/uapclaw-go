package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentManagerPort 运行时需要的 manager 能力（由 server 层实现，避免反向依赖）。
type AgentManagerPort interface {
	// GetChatAgent 按 mode/subMode/projectDir 取聊天 agent（不存在时创建）。
	// agentDefinition / agentDefinitionFingerprint 非空时用于创建声明式 agent
	// （name/description/instructions/tools/skills）。
	// Python: agent_manager.py:658-659 get_agent_for_request(
	//           agent_definition=..., agent_definition_fingerprint=...)
	GetChatAgent(ctx context.Context, channelID, mode, subMode, projectDir string,
		agentDefinition map[string]any, agentDefinitionFingerprint string) (ChatAgentPort, error)
	// GetChatAgentNoWait 非阻塞取已有 agent（不存在时返回 nil，不创建）
	GetChatAgentNoWait(channelID, mode, projectDir, subMode string) ChatAgentPort
}

// ChatAgentPort agent 执行能力面（契约定义在 schema 包）。
//
// 放 schema 而非本包：它的实现在 server/runtime（上层），若契约留在 runtime，
// 上层实现它就得 import runtime，而 runtime 又要用上层实例——成环。
// 详见 schema/agent_port.go。
type ChatAgentPort = schema.ChatAgentPort

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PlanEntrySourceSlashCommand TUI /plan 命令来源
	PlanEntrySourceSlashCommand = "slash_command"
	// PlanEntrySourcePlanToggle Web 计划开关来源
	PlanEntrySourcePlanToggle = "plan_toggle"
	// PlanEntrySourceKey 请求参数中的进入来源键
	PlanEntrySourceKey = "plan_entry_source"
	// PlanReminderOriginalQueryKey 激活提醒改写前原 query 的存放键
	PlanReminderOriginalQueryKey = "plan_reminder_original_query"
	// PlanModeExitedEvent 计划模式结束事件类型
	PlanModeExitedEvent = "plan_mode_exited"
	// PreviousSessionModeKey 上一次会话模式参数键
	PreviousSessionModeKey = "_session_previous_mode"
	// TeamAttachOnlyParam 团队会话被动订阅参数键
	//
	// Python: openjiuwen_protocol.message.TEAM_ATTACH_ONLY_PARAM（协议包常量）
	TeamAttachOnlyParam = "team_attach_only"
	// sessionMessageInternalKey 跨会话消息内部参数键
	//
	// Python: gateway/common/session_message.SESSION_MESSAGE_INTERNAL_KEY
	sessionMessageInternalKey = "_jiuwenswarm_cross_session"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// chatTurnMethods 参与 plan 状态同步的请求方法（空串视为默认 chat）。
var chatTurnMethods = map[string]struct{}{
	"":            {},
	"chat.send":   {},
	"chat":        {},
	"chat.resume": {},
	"chat.answer": {},
}

// chatTurnRequestMethods 真实 chat 轮次的方法集合（决定 metadata 是否刷新时间戳）。
// Python: request.py CHAT_TURN_METHODS
var chatTurnRequestMethods = map[string]struct{}{
	"chat.send":   {},
	"chat.resume": {},
	"chat.answer": {},
}

// planEntrySources 显式进入计划模式的来源集合。
var planEntrySources = map[string]struct{}{
	PlanEntrySourceSlashCommand: {},
	PlanEntrySourcePlanToggle:   {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveRequestRuntimeMode 把请求 mode/subMode 解析为权威模式。
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode。
// 三段 canonical 直接按串语义解析（见 schema.ResolveNewCanonicalMode）；
// 其余走归一并按后缀判定 plan/team。
func ResolveRequestRuntimeMode(mode, subMode string) schema.ResolvedMode {
	raw := strings.TrimSpace(mode)
	if raw == "" {
		if sub := strings.TrimSpace(subMode); sub != "" {
			raw = "agent.work." + sub
		}
	}
	canonical := string(schema.NormalizeMode(raw))
	resolved := schema.ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        schema.IsPlanMode(canonical),
		IsTeam:        schema.IsTeamMode(canonical),
		NormalMode:    schema.BaseModeWithoutPlan(canonical),
	}
	if m, s, _, ok := schema.ResolveNewCanonicalMode(canonical); ok {
		resolved.ManagerMode, resolved.SubMode = m, s
	}
	return resolved
}

// ShouldSync 判断请求是否参与 plan 状态同步。
// Python: PlanModeController.should_sync（只认 chat 类方法）。
func ShouldSync(reqMethod string) bool {
	_, ok := chatTurnMethods[strings.TrimSpace(reqMethod)]
	return ok
}

// IsPlanEntrySource 判断进入来源是否属于显式进入。
// Python: PlanModeController.is_explicit_entry
func IsPlanEntrySource(source string) bool {
	_, ok := planEntrySources[strings.TrimSpace(source)]
	return ok
}

// ResolveAgentRequestMode 解析 mode 为 manager mode、sub-mode 与 canonical mode。
// Python: jiuwenswarm/runtime/request.py resolve_agent_request_mode（L266-328）。
// 只做 legacy 解析：Web 的 mode+work_mode 组合由 resolveRequestMode 先行处理。
// 与上游一致：裸 "agent"/"plan"/"fast" 不下沉 plan；只有显式 plan 串才带 plan 状态。
func ResolveAgentRequestMode(rawMode any, workMode string) (mode, subMode, canonical string) {
	text := schema.CanonicalizeModeText(rawModeText(rawMode))
	// 新三段 canonical 短路：environment/state 已内嵌，不能再按第一个点拆
	if m, s, c, ok := schema.ResolveNewCanonicalModeText(text); ok {
		return m, s, c
	}
	if text == "auto_harness" {
		return "auto_harness", "auto_harness", "auto_harness"
	}
	normalizedWork := schema.NormalizeWorkMode(workMode)
	if text == "plan" || text == "fast" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	// team plan 的两种 legacy canonical（上游 TEAM_PLAN_NORMAL_MODE / TEAM_PLAN_CODE_MODE）
	if text == "team.plan.normal" {
		return "team", "plan", "team.plan.normal"
	}
	if text == "team.plan.code" {
		return "code", "team", "team.plan.code"
	}
	// agent.plan 是真 legacy plan 模式，保留 plan 语义
	if text == "agent.plan" {
		if normalizedWork == "code" {
			return "code", "plan", "code.plan"
		}
		return "agent", "plan", "agent.plan"
	}
	parts := strings.Split(text, ".")
	mode = parts[0]
	if mode == "" {
		mode = "agent"
	}
	if mode == "agent" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	// team 族的 subMode 不参与实例查找（上游显式置 None）
	if mode == "team" {
		return "team", "", "team"
	}
	defaultSubMode := ""
	if mode == "code" {
		defaultSubMode = "normal"
	}
	if len(parts) > 1 && parts[1] != "" {
		subMode = parts[1]
	} else {
		subMode = defaultSubMode
	}
	if mode == "code" && subMode != "plan" && subMode != "normal" && subMode != "team" {
		subMode = "normal"
	}
	canonical = mode
	if subMode != "" {
		canonical = mode + "." + subMode
	}
	// 上游末尾按 work_mode 重折算 agent/code 的等价 canonical
	if canonical == "agent" || canonical == "code" || canonical == "code.normal" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		if normalizedWork == "work" {
			return "agent", "", "agent"
		}
	}
	return mode, subMode, canonical
}

// ApplyResolvedModeToRequest 解析并把 canonical mode 写回 request.Params。
// Python: jiuwenswarm/runtime/request.py apply_resolved_mode_to_request（L345-358）。
// 上游走 resolve_request_runtime_mode（含 Web 组合分支），Go 同步走 resolveRequestMode。
func ApplyResolvedModeToRequest(req *schema.AgentRequest, workMode string) (mode, subMode string) {
	params := parseRequestParams(req)
	resolved := resolveRequestMode(params["mode"], workMode)
	params["mode"] = resolved.CanonicalMode
	writeRequestParams(req, params)
	return resolved.ManagerMode, resolved.SubMode
}

// ResolveRequestRuntimeModeFromRequest 从请求解析权威运行时模式。
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode（L331-342）。
func ResolveRequestRuntimeModeFromRequest(req *schema.AgentRequest) schema.ResolvedMode {
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	return resolveRequestMode(params["mode"], workMode)
}

// parseRequestParams 解析 request.Params 为可写 map（nil/非法时返回空 map）。
func parseRequestParams(req *schema.AgentRequest) map[string]any {
	if req == nil || len(req.Params) == 0 {
		return map[string]any{}
	}
	params := map[string]any{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return map[string]any{}
	}
	return params
}

// writeRequestParams 把 params 序列化回 request.Params。
func writeRequestParams(req *schema.AgentRequest, params map[string]any) {
	if req == nil {
		return
	}
	updated, err := json.Marshal(params)
	if err != nil {
		return
	}
	req.Params = json.RawMessage(updated)
}

// ResolveAgentRequestModeFromRequest 纯读取请求的 mode（不修改 request）。
// Python: _handle_cancel / AgentManager 用 resolve_agent_request_mode 的只读形态。
func ResolveAgentRequestModeFromRequest(req *schema.AgentRequest) (mode, subMode string) {
	resolved := ResolveRequestRuntimeModeFromRequest(req)
	return resolved.ManagerMode, resolved.SubMode
}

// ResolveRequestProjectDir 解析用于构造 agent 的稳定工程身份。
// Python: jiuwenswarm/runtime/request.py resolve_request_project_dir（L44-82）。
// includeLegacyFallbacks=false 时（agent/code 的 projectless 轮次）不回落到
// cwd/trusted_dirs，保持 cwd 动态以便 adapter 分配会话任务工作区。
func ResolveRequestProjectDir(req *schema.AgentRequest, includeLegacyFallbacks bool) string {
	params := parseRequestParams(req)
	if v, ok := params["project_dir"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if req != nil && req.Metadata != nil {
		if v, ok := req.Metadata["project_dir"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if !includeLegacyFallbacks {
		return ""
	}
	if v, ok := params["cwd"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if req != nil && req.Metadata != nil {
		if v, ok := req.Metadata["cwd"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if dirs, ok := params["trusted_dirs"].([]any); ok && len(dirs) > 0 {
		if v, ok := dirs[0].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ResolveCancelProjectDir 解析传输无关的取消请求所需工程缓存键。
// Python: jiuwenswarm/runtime/request.py _resolve_cancel_project_dir（L84-149）。
// 偏差：Go 尚无 project_id → project_dir 注册表（上游 project_store.get_project_dir_by_id），
// 该分支记 ⤵️，其余分支（直接解析 + 会话 metadata）已对齐。
func ResolveCancelProjectDir(req *schema.AgentRequest) string {
	if direct := ResolveRequestProjectDir(req, true); direct != "" {
		return direct
	}
	if req == nil || req.SessionID == nil {
		return ""
	}
	sid := strings.TrimSpace(*req.SessionID)
	if sid == "" {
		return ""
	}
	meta := session.GetSessionMetadata(sid)
	if v, ok := meta["project_dir"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// PrepareChatTurn 解析会话语义、同步会话元数据并选出一个 agent。
// Python: jiuwenswarm/runtime/request.py prepare_chat_turn（L361-575）。
// 偏差（⤵️）：get_agent_for_request 原子准入、wait_for_session_prewarm、
// _repair_session_project_dir 与 bound project_id 绑定未移植（Go 无预热池 /
// 工程注册表）；其余步骤（work_mode 三级回落、sync_metadata、project_dir 回写、
// persist_session 锁定）与上游同序。
func PrepareChatTurn(
	ctx context.Context,
	mgr AgentManagerPort,
	req *schema.AgentRequest,
	channelID string,
	syncMetadata bool,
	agentDefinition map[string]any,
	agentDefinitionFingerprint string,
) (mode, subMode string, agent ChatAgentPort, err error) {
	params := parseRequestParams(req)
	rawMode, _ := params["mode"].(string)
	explicitModeProvided := strings.TrimSpace(rawMode) != ""

	sid := ""
	if req != nil && req.SessionID != nil {
		sid = strings.TrimSpace(*req.SessionID)
	}

	// 1. 会话已锁定的 mode/work_mode 继承（内部轮次含 heartbeat 不带显式 mode）
	runtimeWorkMode := ""
	sessionMetadata := map[string]any{}
	if sid != "" {
		sessionMetadata = session.GetSessionMetadata(sid)
		if storedMode, ok := sessionMetadata["mode"].(string); ok && strings.TrimSpace(storedMode) != "" {
			storedMode = strings.TrimSpace(storedMode)
			params[PreviousSessionModeKey] = storedMode
			if !explicitModeProvided {
				// 内部轮次（含 Heartbeat）继承会话锁定 mode，不算客户端显式切换
				params["mode"] = storedMode
			}
		}
		if wm, ok := sessionMetadata["work_mode"].(string); ok {
			if n := schema.NormalizeWorkMode(wm); n != "" {
				runtimeWorkMode = n
			}
		}
	}
	// 2. 请求自带 work_mode
	if runtimeWorkMode == "" {
		if wm, ok := params["work_mode"].(string); ok {
			runtimeWorkMode = schema.NormalizeWorkMode(wm)
		}
	}
	// 3. 渠道默认（Python: default_work_mode_for_channel —— tui→code，其余 work）
	if runtimeWorkMode == "" {
		runtimeWorkMode = defaultWorkModeForChannel(channelID)
		logger.Warn(logComponent).
			Str("channel_id", channelID).
			Str("session_id", sid).
			Str("work_mode", runtimeWorkMode).
			Msg("runtime request has no work_mode; defaulting by channel")
	}
	params["work_mode"] = runtimeWorkMode
	writeRequestParams(req, params)

	// 4. 解析 mode 并回写 canonical
	mode, subMode = ApplyResolvedModeToRequest(req, runtimeWorkMode)
	agentMode := mode
	if agentMode == "auto_harness" {
		agentMode = "agent"
	}
	canonicalMode := ""
	if parsed := parseRequestParams(req); parsed != nil {
		canonicalMode, _ = parsed["mode"].(string)
	}

	// 5. 工程身份（agent/code 的 projectless 轮次不回落 cwd）
	projectDir := ResolveRequestProjectDir(req, mode != "agent" && mode != "code")

	// 6. 同步会话元数据（Python: admit_request 内的 metadata_sync）
	if syncMetadata {
		// ApplyResolvedModeToRequest 已把 canonical mode 写回 params，此处只读
		projectDir = SyncChatRequestMetadata(req, projectDir, canonicalMode, explicitModeProvided, requestUserID(req))
		if sid != "" {
			// Python: 权限准入通过后重读 metadata（供 persist_session 判定）
			sessionMetadata = session.GetSessionMetadata(sid)
		}
	} else if projectDir == "" && sid != "" {
		if locked, ok := sessionMetadata["project_dir"].(string); ok && strings.TrimSpace(locked) != "" {
			projectDir = strings.TrimSpace(locked)
		}
	}

	// 7. 回写 project_dir 到 params + metadata（Python: admit_request 尾段）
	params = parseRequestParams(req)
	if projectDir != "" {
		params["project_dir"] = projectDir
		if req.Metadata == nil {
			req.Metadata = map[string]any{}
		}
		req.Metadata["project_dir"] = projectDir
	} else {
		delete(params, "project_dir")
		// delete 对 nil map 安全：无工程身份时同步清理 metadata 里的旧值
		delete(req.Metadata, "project_dir")
	}
	writeRequestParams(req, params)

	// 8. persist_session 是会话创建期身份：本轮值仅告警，不得改写
	effectivePersist, _ := sessionMetadata["persist_session"].(bool)
	if requested, ok := params["persist_session"].(bool); ok && requested != effectivePersist {
		logger.Warn(logComponent).
			Str("session_id", sid).
			Bool("locked", effectivePersist).
			Bool("requested", requested).
			Msg("session has locked persist_session; ignoring chat value")
	}
	if requested, ok := params["eternal_conversation_enabled"].(bool); ok && requested != effectivePersist {
		logger.Warn(logComponent).
			Str("session_id", sid).
			Bool("locked", effectivePersist).
			Bool("requested", requested).
			Msg("session has locked Persist Session; ignoring legacy chat value")
	}
	params["eternal_conversation_enabled"] = effectivePersist
	writeRequestParams(req, params)

	// 9. 取 agent（⤵️ 上游为 get_agent_for_request 原子准入 + 预热等待）
	// Python: request.py:534-537 agent_kwargs["agent_definition"] /
	//         ["agent_definition_fingerprint"] → get_agent_for_request
	agent, err = mgr.GetChatAgent(ctx, channelID, agentMode, subMode, projectDir,
		agentDefinition, agentDefinitionFingerprint)
	if err != nil {
		return "", "", nil, err
	}
	return mode, subMode, agent, nil
}

// SyncChatRequestMetadata 持久化 chat 请求元数据并返回锁定的工程目录。
// Python: jiuwenswarm/runtime/request.py sync_chat_request_metadata（L152-263）。
// attach-only 订阅是被动观察者：不提交输入，因此只读解析锁定目录、不写元数据。
func SyncChatRequestMetadata(
	req *schema.AgentRequest,
	projectDir string,
	mode string,
	explicitModeProvided bool,
	userID string,
) string {
	sid := ""
	if req != nil && req.SessionID != nil {
		sid = strings.TrimSpace(*req.SessionID)
	}
	if sid == "" {
		return projectDir
	}
	params := parseRequestParams(req)

	// attach-only：只读锁定目录，绝不写会话元数据
	if attachOnly, ok := params[TeamAttachOnlyParam].(bool); ok && attachOnly {
		if meta := session.GetSessionMetadata(sid); len(meta) > 0 {
			if locked, ok := meta["project_dir"].(string); ok && strings.TrimSpace(locked) != "" {
				return strings.TrimSpace(locked)
			}
		}
		return projectDir
	}

	rawModelName, _ := params["model_name"].(string)
	explicitModelProvided := strings.TrimSpace(rawModelName) != ""
	modelName := strings.TrimSpace(rawModelName)
	if !explicitModelProvided {
		modelName = strings.TrimSpace(os.Getenv("MODEL_NAME"))
	}

	requestProjectID, _ := params["project_id"].(string)
	requestCronID, _ := params["cron_id"].(string)
	_, crossSession := params[sessionMessageInternalKey].(map[string]any)
	_, chatTurnMethod := chatTurnRequestMethods[req.ReqMethod.String()]
	isChatTurn := chatTurnMethod && !crossSession

	var lastUserMessageAt *float64
	if isChatTurn {
		now := float64(time.Now().UnixNano()) / float64(time.Second)
		lastUserMessageAt = &now
	}

	return session.SyncSessionRequestMetadata(session.SessionRequestMetadataSync{
		SessionID:             sid,
		ChannelID:             req.ChannelID,
		Mode:                  mode,
		ExplicitModeProvided:  explicitModeProvided,
		Model:                 modelName,
		ExplicitModelProvided: explicitModelProvided,
		ProjectDir:            projectDir,
		ProjectID:             strings.TrimSpace(requestProjectID),
		CronID:                strings.TrimSpace(requestCronID),
		UserID:                strings.TrimSpace(userID),
		LastUserMessageAt:     lastUserMessageAt,
		IsChatTurn:            isChatTurn,
		WorkMode:              workModeParam(params),
		PersistSession:        legacyPersistSession(params["eternal_conversation_enabled"]),
	})
}

// CancelRequest 通过既有 Runtime agent 取消一次请求/会话。
// Python: jiuwenswarm/runtime/request.py cancel_request（L578-639）。
// 偏差（⤵️）：上游首步用 get_agent_for_session_nowait 按 session→agent 注册表查，
// Go 的 AgentManager 以 (channel, mode, subMode, projectDir) 为键、无会话注册表，
// 该分支缺席；其余三级 fallback 与"找不到时不新建"的 allowCreate 语义保持一致。
func CancelRequest(
	ctx context.Context,
	mgr AgentManagerPort,
	req *schema.AgentRequest,
	allowCreate bool,
) (*schema.AgentResponse, error) {
	channelID := ""
	if req != nil {
		channelID = req.ChannelID
	}
	if channelID == "" {
		channelID = "default"
	}
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	projectDir := ResolveCancelProjectDir(req)

	var agent ChatAgentPort
	if modeParam, _ := params["mode"].(string); strings.TrimSpace(modeParam) != "" {
		resolved := resolveRequestMode(modeParam, workMode)
		agentMode := resolved.ManagerMode
		if agentMode == "auto_harness" {
			agentMode = "agent"
		}
		agent = mgr.GetChatAgentNoWait(channelID, agentMode, projectDir, resolved.SubMode)
	}
	if agent == nil {
		agent = mgr.GetChatAgentNoWait(channelID, "", projectDir, "")
	}

	if agent == nil && !allowCreate {
		return schema.NewAgentResponse(req.RequestID, req.ChannelID,
			schema.WithResponseOK(true),
			schema.WithPayload(map[string]any{
				"event_type": "chat.interrupt_result",
				"success":    true,
				"message":    "当前会话任务已终止",
			}),
		), nil
	}

	if agent == nil {
		mode, subMode := ApplyResolvedModeToRequest(req, "")
		agentMode := mode
		if agentMode == "auto_harness" {
			agentMode = "agent"
		}
		// 取消路径不需要声明式定义：只取已有 agent 打断执行
		created, err := mgr.GetChatAgent(ctx, channelID, agentMode, subMode, projectDir, nil, "")
		if err != nil {
			return nil, err
		}
		agent = created
	}
	if agent == nil {
		return nil, errors.New("Failed to get agent for cancel request")
	}
	return agent.ProcessMessage(ctx, req)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
// resolveRequestMode 解析请求 mode：先 Web 组合，再 legacy 解析。
// Python: resolve_request_mode（gateway/common/mode_matrix.py:413-489）
//
// 薄转发：解析细节全部在 schema.ResolveRequestMode，legacy 解析器传
// ResolveAgentRequestMode（对应上游 legacy_resolver 参数）。
func resolveRequestMode(rawMode any, workMode string) schema.ResolvedMode {
	params := map[string]any{"mode": rawMode, "work_mode": workMode}
	// ResolveAgentRequestMode 的第一参是 any（兼容 Mode 枚举与裸字符串），
	// schema.ResolveRequestMode 的 legacyResolver 收 string，故包一层适配。
	legacy := func(mode, work string) (string, string, string) {
		return ResolveAgentRequestMode(mode, work)
	}
	return schema.ResolveRequestMode(params, legacy, workMode)
}

// rawModeText 取出 mode 的字符串形式（非字符串按空串处理）。
func rawModeText(rawMode any) string {
	s, _ := rawMode.(string)
	return s
}

// defaultWorkModeForChannel 按渠道推断默认 work_mode（tui → code，其余 work）。
// Python: jiuwenswarm/server/runtime/session/work_mode.default_work_mode_for_channel
func defaultWorkModeForChannel(channelID string) string {
	if strings.EqualFold(strings.TrimSpace(channelID), "tui") {
		return "code"
	}
	return "work"
}

// workModeParam 读取 params["work_mode"] 的字符串值。
func workModeParam(params map[string]any) string {
	s, _ := params["work_mode"].(string)
	return s
}

// requestUserID 读取请求携带的 user_id（schema.AgentRequest 尚无独立字段）。
func requestUserID(req *schema.AgentRequest) string {
	if req == nil {
		return ""
	}
	if req.Metadata != nil {
		if v, ok := req.Metadata["user_id"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if v, ok := parseRequestParams(req)["user_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// legacyPersistSession 解析旧式 eternal_conversation_enabled 取值。
// Python: sync_chat_request_metadata 的 bool / 字符串归一分支
func legacyPersistSession(raw any) *bool {
	if b, ok := raw.(bool); ok {
		value := b
		return &value
	}
	if s, ok := raw.(string); ok {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "1", "true", "yes", "on", "enabled":
			value := true
			return &value
		case "0", "false", "no", "off", "disabled":
			value := false
			return &value
		}
	}
	return nil
}
