package evolution

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	gatewaypush "github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionPushContext evolution 推送上下文。
// 对齐 Python: EvolutionPushContext
type EvolutionPushContext struct {
	// Transport 推送传输
	Transport gatewaypush.GatewayPushTransport
	// ChannelID 通道标识（可能为空）
	ChannelID string
	// SessionID 会话标识
	SessionID string
}

// EvolutionStatusUpdate evolution 状态更新。
// 对齐 Python: EvolutionStatusUpdate
type EvolutionStatusUpdate struct {
	// RequestID 请求标识
	RequestID string
	// Status 状态
	Status string
	// Stage 阶段
	Stage string
	// Message 消息
	Message string
}

// EvolutionProgressStatus evolution 进度状态。
// 对齐 Python: EvolutionProgressStatus
type EvolutionProgressStatus struct {
	// Stage 阶段
	Stage string
	// Message 消息
	Message string
	// RequestID 请求标识（nil 表示无）
	RequestID *string
	// Terminal 是否终结
	Terminal bool
}

// TerminalProgressItem 终结进度条目。
// 对齐 Python: terminal_progress_from_events 返回的 tuple
type TerminalProgressItem struct {
	// RequestID 请求标识
	RequestID *string
	// Terminal 终结信息
	Terminal map[string]string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamEvolutionIdleSleepSec watcher 空闲轮询间隔
	TeamEvolutionIdleSleepSec = 1.0
	// TeamEvolutionEventTimeoutSec 事件超时
	TeamEvolutionEventTimeoutSec = 900.0
	// TeamEvolutionEventTimeoutGraceSec 超时宽限
	TeamEvolutionEventTimeoutGraceSec = 5.0

	// TeamEvolutionStartStage 起始阶段
	TeamEvolutionStartStage = "collecting"
	// TeamEvolutionStartMessage 起始消息
	TeamEvolutionStartMessage = "Running team skill evolution analysis..."
	// TeamEvolutionNoopStage 无演进（通用）
	TeamEvolutionNoopStage = "no_evolution_generated"
	// TeamEvolutionNoopNoSkillStage 无演进（无技能）
	TeamEvolutionNoopNoSkillStage = "no_evolution_no_skill"
	// TeamEvolutionNoopNoSignalStage 无演进（无信号）
	TeamEvolutionNoopNoSignalStage = "no_evolution_no_signal"
	// TeamEvolutionNoopNoRecordsStage 无演进（无记录）
	TeamEvolutionNoopNoRecordsStage = "no_evolution_no_records"
	// TeamEvolutionHiddenStage 隐藏阶段
	TeamEvolutionHiddenStage = "hidden"

	// logComponentEvolution 日志组件
	logComponentEvolution = logger.ComponentAgentServer
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// TeamEvolutionNoopMarkers 通用 noop 标记
	TeamEvolutionNoopMarkers = []string{
		"no existing skill found",
		"no evolution signals detected",
		"no evolution records generated",
	}
	// TeamEvolutionNoSkillMarkers 无技能标记
	TeamEvolutionNoSkillMarkers = []string{
		"no skill usage",
		"no existing skill",
		"no regular skill could be attributed",
		"no team/swarm skill",
	}
	// TeamEvolutionNoSignalMarkers 无信号标记
	TeamEvolutionNoSignalMarkers = []string{
		"no actionable evolution signals detected",
		"no evolution signals detected",
	}

	// TeamEvolutionNoopStages noop 阶段集合
	TeamEvolutionNoopStages = map[string]struct{}{
		TeamEvolutionNoopStage:          {},
		TeamEvolutionNoopNoSkillStage:   {},
		TeamEvolutionNoopNoSignalStage:  {},
		TeamEvolutionNoopNoRecordsStage: {},
	}
	// TeamEvolutionHiddenTerminalStages 隐藏终结阶段集合
	TeamEvolutionHiddenTerminalStages = map[string]struct{}{
		TeamEvolutionHiddenStage: {},
		"failed":                 {},
		"timed_out":              {},
	}
	// TeamEvolutionVisibleProgressStages 可见进度阶段集合
	TeamEvolutionVisibleProgressStages = map[string]struct{}{
		"generating":                    {},
		"approval_required":             {},
		"completed":                     {},
		TeamEvolutionNoopStage:          {},
		TeamEvolutionNoopNoSkillStage:   {},
		TeamEvolutionNoopNoSignalStage:  {},
		TeamEvolutionNoopNoRecordsStage: {},
	}

	// sdkProgressStageMap SDK→显示阶段映射
	// 对齐 Python: _SDK_PROGRESS_STAGE_MAP
	sdkProgressStageMap = map[string]string{
		"started":            "detecting",
		"detecting_signals":  "detecting",
		"staging":            "generating",
		"generating_updates": "generating",
		"approval_required":  "approval_required",
		"auto_approved":      "completed",
		"cancelled":          TeamEvolutionHiddenStage,
		"completed":          "completed",
		"failed":             "failed",
		"timed_out":          "timed_out",
	}

	// sdkProgressTerminalStages SDK 终结阶段集合
	// 对齐 Python: _SDK_PROGRESS_TERMINAL_STAGES
	sdkProgressTerminalStages = map[string]struct{}{
		"auto_approved": {},
		"cancelled":     {},
		"completed":     {},
		"failed":        {},
		"timed_out":     {},
	}
)

// BuildPushMessageFunc 构建 server_push 消息的函数类型。
// 对齐 Python: build_server_push_message 回调参数
type BuildPushMessageFunc func(sessionID, requestID, fallbackChannelID string, payload map[string]any) map[string]any

// ParseStreamChunkFunc 解析流式 chunk 的函数类型。
// 对齐 Python: parse_stream_chunk 回调参数
type ParseStreamChunkFunc func(evt any) map[string]any

// BroadcastEventFunc 广播事件的函数类型。
// 对齐 Python: broadcast_event 回调参数
type BroadcastEventFunc func(channelID *string, sessionID string, parsed map[string]any)

// WarnMissingRequestIDFunc 缺少 request_id 时的警告回调。
// 对齐 Python: group_evolution_approvals 的 warn_missing_request_id 参数
type WarnMissingRequestIDFunc func(sessionID string)

// ──────────────────────────── 导出函数 ────────────────────────────

// EventPayloadDict 提取事件 payload 为 map。
// 对齐 Python: event_payload_dict() — 仅处理 map[string]any 类型事件，
// 当前事件来源（drain_pending_approval_events）始终返回 dict。
// 保留 evt any 签名以兼容未来 SkillEvolutionRail 实现后的具体类型扩展。
func EventPayloadDict(evt any) map[string]any {
	if m, ok := evt.(map[string]any); ok {
		result := make(map[string]any, len(m))
		for k, v := range m {
			result[k] = v
		}
		return result
	}
	return map[string]any{}
}

// EventType 提取事件类型字符串。
// 对齐 Python: event_type() — 仅从 map[string]any 的 event_type 字段提取，
// 不再使用 reflect 访问 struct.Type 字段（过度对齐 Python hasattr 防御性代码）。
func EventType(evt any) string {
	payload := EventPayloadDict(evt)
	if t, ok := payload["event_type"].(string); ok {
		return t
	}
	return ""
}

// ResolveEvolutionEventTimeoutSec 解析演进事件超时时间。
// 对齐 Python: resolve_evolution_event_timeout_sec() — 仅从 map[string]any 读取，
// 不再使用 reflect 访问 struct 字段（过度对齐 Python hasattr 防御性代码）。
func ResolveEvolutionEventTimeoutSec(rail any, opts ...float64) float64 {
	fallback := TeamEvolutionEventTimeoutSec
	grace := TeamEvolutionEventTimeoutGraceSec

	if len(opts) > 0 && opts[0] > 0 {
		fallback = opts[0]
	}
	if len(opts) > 1 && opts[1] >= 0 {
		grace = opts[1]
	}

	if rail == nil {
		return fallback
	}

	// 从 map[string]any 中读取 evolution_total_timeout_secs
	var sdkTimeout any
	if m, ok := rail.(map[string]any); ok {
		sdkTimeout = m["evolution_total_timeout_secs"]
	}

	if sdkTimeout == nil {
		return fallback
	}

	parsedTimeout, ok := toFloat64(sdkTimeout)
	if !ok || math.IsInf(parsedTimeout, 0) || math.IsNaN(parsedTimeout) || parsedTimeout <= 0 {
		return fallback
	}
	return parsedTimeout + math.Max(grace, 0.0)
}

// IsEvolutionApprovalEvent 判断是否为演进审批事件（检查 event_type）。
// 对齐 Python: is_evolution_approval_event()
func IsEvolutionApprovalEvent(evt any) bool {
	if EventType(evt) == "chat.ask_user_question" {
		return true
	}
	payload := EventPayloadDict(evt)
	if t, ok := payload["event_type"].(string); ok && t == "chat.ask_user_question" {
		return true
	}
	return false
}

// EvolutionEventKind 判断事件类别（approval/outcome/progress/stream）。
// 对齐 Python: evolution_event_kind()
func EvolutionEventKind(evt any) string {
	payload := EventPayloadDict(evt)
	if meta, ok := payload["_evolution_meta"].(map[string]any); ok {
		if kind, ok := meta["event_kind"].(string); ok && strings.TrimSpace(kind) != "" {
			return kind
		}
	}
	if IsEvolutionApprovalEvent(evt) {
		return "approval"
	}
	return "stream"
}

// IsEvolutionOutcomeEvent 判断是否为演进结果事件。
// 对齐 Python: is_evolution_outcome_event()
func IsEvolutionOutcomeEvent(evt any) bool {
	return EvolutionEventKind(evt) == "outcome"
}

// EvolutionOutcomeFromEvent 提取演进结果。
// 对齐 Python: evolution_outcome_from_event()
func EvolutionOutcomeFromEvent(evt any) map[string]string {
	payload := EventPayloadDict(evt)
	if payload == nil {
		return map[string]string{"status": "completed", "message": ""}
	}

	meta, _ := payload["_evolution_meta"].(map[string]any)
	var metaStatus any
	if meta != nil {
		metaStatus = meta["status"]
	}

	status := "completed"
	if s, ok := payload["status"].(string); ok && strings.TrimSpace(s) != "" {
		status = strings.TrimSpace(strings.ToLower(s))
	} else if ms, ok := metaStatus.(string); ok && strings.TrimSpace(ms) != "" {
		status = strings.TrimSpace(strings.ToLower(ms))
	}
	if status == "" {
		status = "completed"
	}

	message := ""
	if m, ok := payload["message"].(string); ok {
		message = m
	} else if c, ok := payload["content"].(string); ok {
		message = c
	}

	return map[string]string{"status": status, "message": message}
}

// ExtractEvolutionRequestID 从事件中提取 request_id。
// 对齐 Python: extract_evolution_request_id()
func ExtractEvolutionRequestID(evt any) *string {
	payload := EventPayloadDict(evt)
	requestID := payload["request_id"]
	if requestID == nil {
		if meta, ok := payload["_evolution_meta"].(map[string]any); ok {
			requestID = meta["request_id"]
		}
	}
	if s, ok := requestID.(string); ok {
		s = strings.TrimSpace(s)
		if s != "" {
			return &s
		}
	}
	return nil
}

// EvolutionProgressStatusFromEvent 提取进度状态。
// 对齐 Python: evolution_progress_status_from_event()
func EvolutionProgressStatusFromEvent(evt any) *EvolutionProgressStatus {
	payload := EventPayloadDict(evt)
	meta, ok := payload["_evolution_meta"].(map[string]any)
	if !ok {
		return nil
	}

	eventKind := strings.TrimSpace(strings.ToLower(strFromAny(meta["event_kind"])))
	if eventKind != "progress" {
		return nil
	}

	rawStage := strings.TrimSpace(strings.ToLower(strFromAny(payload["stage"], meta["stage"])))
	if rawStage == "" {
		return nil
	}

	message := strings.TrimSpace(strFromAny(payload["message"], payload["content"]))
	noopStage := noopStageFromMessage(strings.ToLower(message))

	stage := rawStage
	if rawStage != "cancelled" && noopStage != nil {
		stage = *noopStage
	} else if mapped, ok := sdkProgressStageMap[rawStage]; ok {
		stage = mapped
	}

	_, terminal := sdkProgressTerminalStages[rawStage]
	requestID := ExtractEvolutionRequestID(evt)

	return &EvolutionProgressStatus{
		Stage:     stage,
		Message:   message,
		RequestID: requestID,
		Terminal:  terminal,
	}
}

// VisibleEvolutionProgressFromEvents 过滤可见进度。
// 对齐 Python: visible_evolution_progress_from_events()
func VisibleEvolutionProgressFromEvents(events []any) []EvolutionProgressStatus {
	var result []EvolutionProgressStatus
	for _, evt := range events {
		progress := EvolutionProgressStatusFromEvent(evt)
		if progress != nil {
			if _, ok := TeamEvolutionVisibleProgressStages[progress.Stage]; ok {
				result = append(result, *progress)
			}
		}
	}
	return result
}

// ProgressForRequest 按 requestID 过滤进度。
// 对齐 Python: progress_for_request()
func ProgressForRequest(statuses []EvolutionProgressStatus, requestID string) []EvolutionProgressStatus {
	var result []EvolutionProgressStatus
	for _, p := range statuses {
		if p.RequestID == nil || *p.RequestID == requestID {
			result = append(result, p)
		}
	}
	return result
}

// TerminalStage 提取终结阶段。
// 对齐 Python: terminal_stage()
func TerminalStage(terminal map[string]string) string {
	s := terminal["stage"]
	if s == "" {
		s = terminal["status"]
	}
	return strings.TrimSpace(strings.ToLower(s))
}

// TerminalProgressFromEvents 提取终结进度。
// 对齐 Python: terminal_progress_from_events()
func TerminalProgressFromEvents(events []any) []TerminalProgressItem {
	var result []TerminalProgressItem
	for _, evt := range events {
		terminal := TeamEvolutionTerminalProgress(evt)
		if terminal != nil {
			requestID := ExtractEvolutionRequestID(evt)
			result = append(result, TerminalProgressItem{
				RequestID: requestID,
				Terminal:  terminal,
			})
		}
	}
	return result
}

// TeamEvolutionTerminalProgress 判断终结进度。
// 对齐 Python: team_evolution_terminal_progress()
func TeamEvolutionTerminalProgress(evt any) map[string]string {
	payload := EventPayloadDict(evt)
	progress := EvolutionProgressStatusFromEvent(evt)

	// 隐藏终结阶段
	if progress != nil && progress.Terminal && progress.Stage == TeamEvolutionHiddenStage {
		return map[string]string{
			"status":  progress.Stage,
			"stage":   progress.Stage,
			"message": progress.Message,
		}
	}

	message := strFromAny(payload["message"], payload["content"])
	messageLower := strings.ToLower(message)
	noopStage := noopStageFromMessage(messageLower)
	if noopStage != nil {
		return map[string]string{
			"status":  "completed",
			"stage":   *noopStage,
			"message": orStr(message, "No evolution generated"),
		}
	}

	if progress != nil && progress.Terminal {
		if progress.Stage == TeamEvolutionNoopStage {
			return map[string]string{
				"status":  "completed",
				"stage":   TeamEvolutionNoopStage,
				"message": orStr(progress.Message, "No evolution generated"),
			}
		}
		return map[string]string{
			"status":  progress.Stage,
			"stage":   progress.Stage,
			"message": progress.Message,
		}
	}

	// 从 meta 中提取状态
	meta, _ := payload["_evolution_meta"].(map[string]any)
	var metaStatus, metaStage any
	if meta != nil {
		metaStatus = meta["status"]
		metaStage = meta["stage"]
	}

	status := strings.TrimSpace(strings.ToLower(strFromAny(payload["status"], metaStatus)))
	stage := strings.TrimSpace(strings.ToLower(strFromAny(payload["stage"], metaStage)))

	if status == "end" || stage == "completed" || stage == "failed" || stage == "timed_out" {
		return map[string]string{
			"status":  orStr(status, "end"),
			"stage":   orStr(stage, "completed"),
			"message": message,
		}
	}

	return nil
}

// BuildEvolutionStatusUpdate 构建状态更新。
// 对齐 Python: build_evolution_status_update()
func BuildEvolutionStatusUpdate(requestID, status, stage string, message ...string) EvolutionStatusUpdate {
	msg := ""
	if len(message) > 0 {
		msg = message[0]
	}
	return EvolutionStatusUpdate{
		RequestID: requestID,
		Status:    status,
		Stage:     stage,
		Message:   msg,
	}
}

// TeamEvolutionEndUpdate 构建终结更新。
// 对齐 Python: team_evolution_end_update()
func TeamEvolutionEndUpdate(requestID string, terminal map[string]string) EvolutionStatusUpdate {
	if terminal == nil {
		return BuildEvolutionStatusUpdate(
			requestID,
			"end",
			"completed",
			"Team skill evolution analysis completed",
		)
	}

	stage := strings.TrimSpace(strings.ToLower(orStr(terminal["stage"], terminal["status"], "completed")))
	message := terminal["message"]

	if stage == "failed" || stage == "timed_out" {
		return BuildEvolutionStatusUpdate(requestID, "end", TeamEvolutionHiddenStage, message)
	}
	if _, ok := TeamEvolutionNoopStages[stage]; ok {
		return BuildEvolutionStatusUpdate(requestID, "end", stage, message)
	}
	return BuildEvolutionStatusUpdate(
		requestID,
		"end",
		orStr(stage, "completed"),
		orStr(message, "Team skill evolution analysis completed"),
	)
}

// GroupEvolutionApprovals 审批分组。
// 对齐 Python: group_evolution_approvals() — 第二项始终返回 nil（Python 始终返回空列表 []）
func GroupEvolutionApprovals(sessionID string, events []any, warnMissing ...WarnMissingRequestIDFunc) (map[string][]any, []string) {
	grouped := make(map[string][]any)

	for _, evt := range events {
		if !IsEvolutionApprovalEvent(evt) {
			continue
		}
		requestID := ExtractEvolutionRequestID(evt)
		if requestID == nil {
			// 对齐 Python: 仅调用 warn 回调，不收集到返回值中
			if len(warnMissing) > 0 && warnMissing[0] != nil {
				warnMissing[0](sessionID)
			}
			continue
		}
		grouped[*requestID] = append(grouped[*requestID], evt)
	}

	return grouped, nil
}

// MakeTeamEvolutionCycleRequestID 生成 request_id。
// 对齐 Python: make_team_evolution_cycle_request_id()
func MakeTeamEvolutionCycleRequestID(sessionID string, cycleIndex int) string {
	return "team_evolve_" + sessionID + "_" + strconv.Itoa(cycleIndex)
}

// PushEvolutionStatus 推送状态。
// 对齐 Python: push_evolution_status()
func PushEvolutionStatus(
	ctx context.Context,
	pushCtx *EvolutionPushContext,
	update EvolutionStatusUpdate,
	buildMsgFn BuildPushMessageFunc,
	includePayloadRequestID ...bool,
) error {
	payload := map[string]any{
		"event_type": "chat.evolution_status",
		"status":     update.Status,
		"stage":      update.Stage,
		"message":    update.Message,
	}

	includeReqID := true
	if len(includePayloadRequestID) > 0 {
		includeReqID = includePayloadRequestID[0]
	}
	if includeReqID {
		payload["request_id"] = update.RequestID
	}

	msg := buildMsgFn(pushCtx.SessionID, update.RequestID, pushCtx.ChannelID, payload)
	return pushCtx.Transport.SendPush(ctx, msg)
}

// PushEvolutionEvent 推送事件。
// 对齐 Python: push_evolution_event()
func PushEvolutionEvent(
	ctx context.Context,
	pushCtx *EvolutionPushContext,
	requestID string,
	evt any,
	buildMsgFn BuildPushMessageFunc,
) error {
	payload := EventPayloadDict(evt)
	evtType := EventType(evt)
	if evtType != "" {
		if _, ok := payload["event_type"]; !ok {
			payload["event_type"] = evtType
		}
	}
	if _, ok := payload["request_id"]; !ok {
		payload["request_id"] = requestID
	}

	msg := buildMsgFn(pushCtx.SessionID, requestID, pushCtx.ChannelID, payload)
	return pushCtx.Transport.SendPush(ctx, msg)
}

// BroadcastEvolutionProgress 广播进度。
// 对齐 Python: broadcast_evolution_progress()
func BroadcastEvolutionProgress(
	ctx context.Context,
	channelID *string,
	sessionID string,
	events []any,
	parseChunk ParseStreamChunkFunc,
	broadcastEvent BroadcastEventFunc,
) error {
	for _, evt := range events {
		if IsEvolutionApprovalEvent(evt) || IsEvolutionOutcomeEvent(evt) || TeamEvolutionTerminalProgress(evt) != nil {
			continue
		}
		parsed := parseChunk(evt)
		if parsed != nil {
			broadcastEvent(channelID, sessionID, parsed)
		}
	}
	return nil
}

// PushEvolutionProgress 推送进度。
// 对齐 Python: push_evolution_progress()
func PushEvolutionProgress(
	ctx context.Context,
	pushCtx *EvolutionPushContext,
	requestID string,
	events []any,
	parseChunk ParseStreamChunkFunc,
	buildMsgFn BuildPushMessageFunc,
) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponentEvolution).
				Str("event_type", "EVOLUTION_PUSH_RECOVERED").
				Any("recover", r).
				Msg("PushEvolutionProgress panic 已恢复")
		}
	}()
	for _, evt := range events {
		if IsEvolutionApprovalEvent(evt) || IsEvolutionOutcomeEvent(evt) || TeamEvolutionTerminalProgress(evt) != nil {
			continue
		}
		parsed := parseChunk(evt)
		if parsed == nil {
			continue
		}
		msg := buildMsgFn(pushCtx.SessionID, requestID, pushCtx.ChannelID, parsed)
		if err := pushCtx.Transport.SendPush(ctx, msg); err != nil {
			logger.Warn(logComponentEvolution).
				Str("request_id", requestID).
				Str("session_id", pushCtx.SessionID).
				Err(err).
				Msg("推送 evolution 进度失败")
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// noopStageFromMessage 从消息内容推断 noop 阶段。
// 对齐 Python: _noop_stage_from_message()
func noopStageFromMessage(messageLower string) *string {
	if containsAnyMarker(messageLower, TeamEvolutionNoSkillMarkers) {
		result := TeamEvolutionNoopNoSkillStage
		return &result
	}
	if containsAnyMarker(messageLower, TeamEvolutionNoSignalMarkers) {
		result := TeamEvolutionNoopNoSignalStage
		return &result
	}
	if strings.Contains(messageLower, "no evolution records generated") {
		result := TeamEvolutionNoopNoRecordsStage
		return &result
	}
	if containsAnyMarker(messageLower, TeamEvolutionNoopMarkers) {
		result := TeamEvolutionNoopStage
		return &result
	}
	return nil
}

// containsAnyMarker 检查消息是否包含任一标记。
func containsAnyMarker(messageLower string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(messageLower, marker) {
			return true
		}
	}
	return false
}

// strFromAny 从多个 any 值中获取第一个非空字符串。
func strFromAny(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// orStr 返回第一个非空字符串。
func orStr(values ...string) string {
	for _, s := range values {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// toFloat64 尝试将 any 转换为 float64。
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}
