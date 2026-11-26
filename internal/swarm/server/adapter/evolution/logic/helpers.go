package logic

import (
	"math"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionProgressStatus evolution 进度状态。
// Python: EvolutionProgressStatus
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
// Python: terminal_progress_from_events 返回的 tuple
type TerminalProgressItem struct {
	// RequestID 请求标识
	RequestID *string
	// Terminal 终结信息
	Terminal map[string]string
}

// EvolutionStatusUpdate evolution 状态更新。
// Python: EvolutionStatusUpdate
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

// WarnMissingRequestIDFunc 缺少 request_id 时的警告回调。
// Python: group_evolution_approvals 的 warn_missing_request_id 参数
type WarnMissingRequestIDFunc func(sessionID string)

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
	// Python: _SDK_PROGRESS_STAGE_MAP
	SdkProgressStageMap = map[string]string{
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
	// Python: _SDK_PROGRESS_TERMINAL_STAGES
	SdkProgressTerminalStages = map[string]struct{}{
		"auto_approved": {},
		"cancelled":     {},
		"completed":     {},
		"failed":        {},
		"timed_out":     {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EventPayloadDict 提取事件 payload 为 map。
// Python: event_payload_dict()（evolution_helpers.py L99-104）——
// 读 evt.payload 属性（OutputSchema 对象）或 dict 直返。
// Go 事件域是 map[string]any 两种形状：
//   - typed 嵌套形状（chunkToMap/evolutionOutputSchemasToMaps 产出）："payload" 键存在 → 解包返回 payload 内容
//     （对齐 Python hasattr(evt, "payload") 分支）
//   - 展平/dict 直出形状（outputSchemaToDict 产出）：无 "payload" 键 → 整体即 payload
//     （对齐 Python isinstance(evt, dict) 分支）
//
// 解包后叠加顶层身份键（type/event_type/role 等非 payload 键），确保
// EventType 等身份判定对 typed 形状也能从解包结果读到内容键。
func EventPayloadDict(evt map[string]any) map[string]any {
	if evt == nil {
		return map[string]any{}
	}
	if payload, ok := evt["payload"].(map[string]any); ok {
		result := cloneMap(payload)
		// 叠加顶层身份键（payload 内同名键优先——Python payload 是属性源）
		for k, v := range evt {
			if k == "payload" {
				continue
			}
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
		return result
	}
	return cloneMap(evt)
}

// EventType 提取事件类型字符串。
// Python: event_type()（evolution_helpers.py L107-113）——优先读 evt.type 属性
// （OutputSchema.Type；approval 事件 Type 即 "chat.ask_user_question"），
// 无 type 属性时读 payload 的 event_type 键。
// Go 事件域是 map[string]any：chunkToMap 展平时把 OutputSchema.Type 写入 "type" 键、
// payload 展平后 event_type 也在顶层——语义等价映射：先读 "type" 键（typed 形状），
// 再读 "event_type" 键（dict 形状 / payload 展平形状）。
// S-20 关联：原实现只读 event_type，对 Type="chat.ask_user_question" 的审批事件
// （payload 无 event_type 键）恒返回空——IsEvolutionApprovalEvent 恒 false，
// 演进审批在 watch 循环中被当 stream 事件跳过（grouped_approvals 恒空）。
func EventType(evt map[string]any) string {
	if evt == nil {
		return ""
	}
	// Python: evt_type = getattr(evt, "type", None) — chunkToMap 展平后的 "type" 键
	if t, ok := evt["type"].(string); ok && t != "" {
		return t
	}
	// Python: payload_type = payload.get("event_type")
	if t, ok := evt["event_type"].(string); ok {
		return t
	}
	return ""
}

// ResolveEvolutionEventTimeoutSec 解析演进事件超时时间。
// Python: resolve_evolution_event_timeout_sec() — 仅从 map[string]any 读取，
// 不再使用 reflect 访问 struct 字段（过度对齐 Python hasattr 防御性代码）。
func ResolveEvolutionEventTimeoutSec(rail map[string]any, opts ...float64) float64 {
	fallback := TeamEvolutionEventTimeoutSec
	grace := TeamEvolutionEventTimeoutGraceSec

	if len(opts) > 0 {
		fallback = opts[0] // Python: fallback_sec is not None → 使用传入值（含 0.0）
	}
	if len(opts) > 1 && opts[1] >= 0 {
		grace = opts[1]
	}

	if rail == nil {
		return fallback
	}

	sdkTimeout := rail["evolution_total_timeout_secs"]
	if sdkTimeout == nil {
		return fallback
	}

	parsedTimeout, ok := ToFloat64(sdkTimeout)
	if !ok || math.IsInf(parsedTimeout, 0) || math.IsNaN(parsedTimeout) || parsedTimeout <= 0 {
		return fallback
	}
	return parsedTimeout + math.Max(grace, 0.0)
}

// IsEvolutionApprovalEvent 判断是否为演进审批事件。
// Python: is_evolution_approval_event()（evolution_helpers.py L141-146）——
// 双判定：event_type()（.type 属性 / "type" 键优先）== chat.ask_user_question，
// 或 payload.event_type 键 == chat.ask_user_question（Type="team" 但 payload
// 携带 event_type 的旧形状事件，双源兜底）。
func IsEvolutionApprovalEvent(evt map[string]any) bool {
	if EventType(evt) == "chat.ask_user_question" {
		return true
	}
	payload := EventPayloadDict(evt)
	return payload["event_type"] == "chat.ask_user_question"
}

// EvolutionEventKind 判断事件类别（approval/outcome/progress/stream）。
// Python: evolution_event_kind()
func EvolutionEventKind(evt map[string]any) string {
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
// Python: is_evolution_outcome_event()
func IsEvolutionOutcomeEvent(evt map[string]any) bool {
	return EvolutionEventKind(evt) == "outcome"
}

// EvolutionOutcomeFromEvent 提取演进结果。
// Python: evolution_outcome_from_event()
func EvolutionOutcomeFromEvent(evt map[string]any) map[string]string {
	payload := EventPayloadDict(evt)
	if payload == nil {
		return map[string]string{"status": "completed", "message": ""}
	}

	meta, _ := evt["_evolution_meta"].(map[string]any)
	var metaStatus any
	if meta != nil {
		metaStatus = meta["status"]
	}

	status := "completed"
	if s, ok := evt["status"].(string); ok && strings.TrimSpace(s) != "" {
		status = strings.TrimSpace(strings.ToLower(s))
	} else if ms, ok := metaStatus.(string); ok && strings.TrimSpace(ms) != "" {
		status = strings.TrimSpace(strings.ToLower(ms))
	}
	if status == "" {
		status = "completed"
	}

	message := ""
	if m, ok := evt["message"].(string); ok {
		message = m
	} else if c, ok := evt["content"].(string); ok {
		message = c
	}

	return map[string]string{"status": status, "message": message}
}

// ExtractEvolutionRequestID 从事件中提取 request_id。
// Python: extract_evolution_request_id()
func ExtractEvolutionRequestID(evt map[string]any) *string {
	payload := EventPayloadDict(evt)
	requestID := payload["request_id"]
	if requestID == nil {
		if meta, ok := evt["_evolution_meta"].(map[string]any); ok {
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
// Python: evolution_progress_status_from_event()
func EvolutionProgressStatusFromEvent(evt map[string]any) *EvolutionProgressStatus {
	payload := EventPayloadDict(evt)
	meta, ok := payload["_evolution_meta"].(map[string]any)
	if !ok {
		return nil
	}

	eventKind := strings.TrimSpace(strings.ToLower(StrFromAny(meta["event_kind"])))
	if eventKind != "progress" {
		return nil
	}

	rawStage := strings.TrimSpace(strings.ToLower(StrFromAny(evt["stage"], meta["stage"])))
	if rawStage == "" {
		return nil
	}

	message := strings.TrimSpace(StrFromAny(evt["message"], evt["content"]))
	noopStage := noopStageFromMessage(strings.ToLower(message))

	stage := rawStage
	if rawStage != "cancelled" && noopStage != nil {
		stage = *noopStage
	} else if mapped, ok := SdkProgressStageMap[rawStage]; ok {
		stage = mapped
	}

	_, terminal := SdkProgressTerminalStages[rawStage]
	requestID := ExtractEvolutionRequestID(evt)

	return &EvolutionProgressStatus{
		Stage:     stage,
		Message:   message,
		RequestID: requestID,
		Terminal:  terminal,
	}
}

// VisibleEvolutionProgressFromEvents 过滤可见进度。
// Python: visible_evolution_progress_from_events()
func VisibleEvolutionProgressFromEvents(events []map[string]any) []EvolutionProgressStatus {
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
// Python: progress_for_request()
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
// Python: terminal_stage()
func TerminalStage(terminal map[string]string) string {
	s := terminal["stage"]
	if s == "" {
		s = terminal["status"]
	}
	return strings.TrimSpace(strings.ToLower(s))
}

// TerminalProgressFromEvents 提取终结进度。
// Python: terminal_progress_from_events()
func TerminalProgressFromEvents(events []map[string]any) []TerminalProgressItem {
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
// Python: team_evolution_terminal_progress()
func TeamEvolutionTerminalProgress(evt map[string]any) map[string]string {
	progress := EvolutionProgressStatusFromEvent(evt)

	// 隐藏终结阶段
	if progress != nil && progress.Terminal && progress.Stage == TeamEvolutionHiddenStage {
		return map[string]string{
			"status":  progress.Stage,
			"stage":   progress.Stage,
			"message": progress.Message,
		}
	}

	message := StrFromAny(evt["message"], evt["content"])
	messageLower := strings.ToLower(message)
	noopStage := noopStageFromMessage(messageLower)
	if noopStage != nil {
		return map[string]string{
			"status":  "completed",
			"stage":   *noopStage,
			"message": OrStr(message, "No evolution generated"),
		}
	}

	if progress != nil && progress.Terminal {
		if progress.Stage == TeamEvolutionNoopStage {
			return map[string]string{
				"status":  "completed",
				"stage":   TeamEvolutionNoopStage,
				"message": OrStr(progress.Message, "No evolution generated"),
			}
		}
		return map[string]string{
			"status":  progress.Stage,
			"stage":   progress.Stage,
			"message": progress.Message,
		}
	}

	// 从 meta 中提取状态
	meta, _ := evt["_evolution_meta"].(map[string]any)
	var metaStatus, metaStage any
	if meta != nil {
		metaStatus = meta["status"]
		metaStage = meta["stage"]
	}

	status := strings.TrimSpace(strings.ToLower(StrFromAny(evt["status"], metaStatus)))
	stage := strings.TrimSpace(strings.ToLower(StrFromAny(evt["stage"], metaStage)))

	if status == "end" || stage == "completed" || stage == "failed" || stage == "timed_out" {
		return map[string]string{
			"status":  OrStr(status, "end"),
			"stage":   OrStr(stage, "completed"),
			"message": message,
		}
	}

	return nil
}

// BuildEvolutionStatusUpdate 构建状态更新。
// Python: build_evolution_status_update()
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
// Python: team_evolution_end_update()
func TeamEvolutionEndUpdate(requestID string, terminal map[string]string) EvolutionStatusUpdate {
	if terminal == nil {
		return BuildEvolutionStatusUpdate(
			requestID,
			"end",
			"completed",
			"Team skill evolution analysis completed",
		)
	}

	stage := strings.TrimSpace(strings.ToLower(OrStr(terminal["stage"], terminal["status"], "completed")))
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
		OrStr(stage, "completed"),
		OrStr(message, "Team skill evolution analysis completed"),
	)
}

// GroupEvolutionApprovals 审批分组。
// Python: group_evolution_approvals() — 第二项始终返回 nil（Python 始终返回空列表 []）
func GroupEvolutionApprovals(sessionID string, events []map[string]any, warnMissing ...WarnMissingRequestIDFunc) (map[string][]map[string]any, []string) {
	grouped := make(map[string][]map[string]any)

	for _, evt := range events {
		if !IsEvolutionApprovalEvent(evt) {
			continue
		}
		requestID := ExtractEvolutionRequestID(evt)
		if requestID == nil {
			// Python: 仅调用 warn 回调，不收集到返回值中
			if len(warnMissing) > 0 && warnMissing[0] != nil {
				warnMissing[0](sessionID)
			}
			continue
		}
		grouped[*requestID] = append(grouped[*requestID], evt)
	}

	// T-01: 对齐 Python return grouped, []——返回空切片而非 nil，
	// 避免 JSON 序列化时输出 null（Python 输出 []）
	return grouped, []string{}
}

// MakeTeamEvolutionCycleRequestID 生成 request_id。
// Python: make_team_evolution_cycle_request_id()
func MakeTeamEvolutionCycleRequestID(sessionID string, cycleIndex int) string {
	return "team_evolve_" + sessionID + "_" + strconv.Itoa(cycleIndex)
}

// StrFromAny 从多个 any 值中获取第一个非空字符串。
func StrFromAny(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// OrStr 返回第一个非空字符串。
func OrStr(values ...string) string {
	for _, s := range values {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// ToFloat64 尝试将 any 转换为 float64。
func ToFloat64(v any) (float64, bool) {
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// noopStageFromMessage 从消息内容推断 noop 阶段。
// Python: _noop_stage_from_message()
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

// cloneMap 浅拷贝 map（替代被移除的 maps.Clone，避免引入 maps 包依赖）。
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// ──────────────────────────── 15.24d Task2：SDK 状态映射 ────────────────────────────

// userFacingGenerationText 内部工具链错误链归一后的用户面文案。
// Python: _user_facing_generation_error()（evolution_helpers.py:323）。
const userFacingGenerationText = "LLM 服务调用失败，请检查模型配置或稍后重试"

// internalGenerationErrorMarkers 归一判定的内部错误链标记。
// Python: internal_markers（evolution_helpers.py:315-322）。
var internalGenerationErrorMarkers = []string{
	"toolchain",
	"tool_call",
	"invoke_failed",
	"execution error",
	"optimizer",
}

// UserFacingGenerationError 隐藏底层工具链错误链，归一为用户面文案。
// Python: _user_facing_generation_error()（evolution_helpers.py:312-324）。
func UserFacingGenerationError(message string) string {
	lowered := strings.ToLower(message)
	for _, marker := range internalGenerationErrorMarkers {
		if strings.Contains(lowered, marker) {
			return userFacingGenerationText
		}
	}
	return message
}

// EvolutionStatusResponse 将 SDK EvolutionRequestResult.status 映射为用户面响应。
// Python: evolution_status_response()（evolution_helpers.py:274-309）。
//
// 步骤：
//
//	Python: 1. status = str(evolve_result.status).strip()；空 → None
//	Python: 2. message = str(evolve_result.message).strip()
//	Python: 3. generation_failed → 归一文案 or generation_failed_output（error）
//	Python: 4. skipped_skill_definition_not_found → 固定文案 + message 追加（error）
//	Python: 5. persistence_failed → "演进经验保存失败：{message}"（error）
//	Python: 6. no_evolution_no_records → no_records_output + message 追加（answer）
//	Python: 7. 其余 → None
// 返回 {"output","result_type"} 或 nil（evolveResult 形状：{"status","message"}）。
func EvolutionStatusResponse(evolveResult map[string]any, generationFailedOutput, noRecordsOutput string) map[string]string {
	if evolveResult == nil {
		return nil
	}
	status := StrFromAny(evolveResult["status"])
	status = strings.TrimSpace(status)
	if status == "" {
		return nil
	}
	message := strings.TrimSpace(StrFromAny(evolveResult["message"]))

	switch status {
	case "generation_failed":
		// Python :288：_user_facing_generation_error(message) or generation_failed_output
		output := UserFacingGenerationError(message)
		if output == "" {
			output = generationFailedOutput
		}
		return map[string]string{"output": output, "result_type": "error"}
	case "skipped_skill_definition_not_found":
		output := "Skill 缺少 SKILL.md，无法执行演进生成。"
		if message != "" {
			output = output + "\n" + message
		}
		return map[string]string{"output": output, "result_type": "error"}
	case "persistence_failed":
		output := "演进经验保存失败"
		if message != "" {
			output = output + "：" + message
		}
		return map[string]string{"output": output, "result_type": "error"}
	case "no_evolution_no_records":
		output := noRecordsOutput
		if message != "" {
			output = output + "\n" + message
		}
		return map[string]string{"output": output, "result_type": "answer"}
	}
	return nil
}


// ──────────────────────────── 15.24d Task3：slash 结果前端注记 ────────────────────────────

// RegularEvolutionSlashWarningPhrases 个人版 slash 结果 warning 短语集。
// Python: REGULAR_EVOLUTION_SLASH_WARNING_PHRASES（evolution_helpers.py:125-128）。
var RegularEvolutionSlashWarningPhrases = []string{
	"未生成可保存经验",
	"未发现明确的演进信号",
}

// TeamEvolutionSlashWarningPhrases 团队版 slash 结果 warning 短语集。
// Python: TEAM_EVOLUTION_SLASH_WARNING_PHRASES（evolution_helpers.py:129-132）。
var TeamEvolutionSlashWarningPhrases = []string{
	"未生成可保存经验",
	"未生成新的团队技能演进经验",
}

// evolutionSlashCommands slash 命令名表（去斜杠）。
// Python: _EVOLUTION_SLASH_COMMANDS（evolution_helpers.py:133-138）。
var evolutionSlashCommands = []string{
	"evolve_simplify",
	"evolve_rebuild",
	"evolve_rollback",
	"evolve_list",
}

// EvolutionSlashDisplayLevel 返回演进 slash 结果的前端显示级别。
// Python: evolution_slash_display_level()（evolution_helpers.py:327-338）。
func EvolutionSlashDisplayLevel(resultType, content string, warningPhrases []string) string {
	if warningPhrases == nil {
		warningPhrases = RegularEvolutionSlashWarningPhrases // Python 缺省参数（:331）
	}
	if resultType == "error" {
		return "error"
	}
	for _, phrase := range warningPhrases {
		if strings.Contains(content, phrase) {
			return "warning"
		}
	}
	return "info"
}

// EvolutionSlashCommandName 返回不带斜杠的演进 slash 命令名。
// Python: evolution_slash_command_name()（evolution_helpers.py:341-347）。
func EvolutionSlashCommandName(query string) string {
	stripped := strings.TrimSpace(query)
	for _, command := range evolutionSlashCommands {
		if strings.HasPrefix(stripped, "/"+command) {
			return command
		}
	}
	return "evolve"
}

// EvolutionSlashResult 为前端渲染注记 slash 结果（setdefault 语义：不覆盖已有键）。
// Python: evolution_slash_result()（evolution_helpers.py:350-368）。
func EvolutionSlashResult(command string, result map[string]any, warningPhrases []string) map[string]any {
	annotated := cloneMap(result)
	if _, ok := annotated["source"]; !ok {
		annotated["source"] = "slash_command"
	}
	if _, ok := annotated["slash_command"]; !ok {
		annotated["slash_command"] = command
	}
	if _, ok := annotated["display_level"]; !ok {
		resultType := "answer"
		if rt, ok := annotated["result_type"].(string); ok && rt != "" {
			resultType = strings.ToLower(strings.TrimSpace(rt))
		}
		output := StrFromAny(annotated["output"])
		annotated["display_level"] = EvolutionSlashDisplayLevel(resultType, output, warningPhrases)
	}
	return annotated
}
