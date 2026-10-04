package adapter

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pendingWaiter 单个等待条目。
// 对齐 Python: _pending_waiters 中的 (request_id, asyncio.Queue) 对
type pendingWaiter struct {
	// RequestID 请求标识
	RequestID string
	// Ch 事件接收通道
	Ch chan map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// hideDMPrefix 隐藏 DM 指令前缀。
	// Python: _HIDE_DM_PREFIX = "/hide_dm" (line 66)
	hideDMPrefix = "/hide_dm"
	// debugPrefix 调试指令前缀。
	// Python: _DEBUG_PREFIX = "/debug" (line 68)
	debugPrefix = "/debug"
	// streamTraceEnvKey 流追踪环境变量。
	// Python: _STREAM_TRACE_ENV_KEY = "JIUWENSWARM_TEAM_STREAM_TRACE" (line 67)
	streamTraceEnvKey = "JIUWENSWARM_TEAM_STREAM_TRACE"
)

// teamCreateKinds 团队创建类型集合。
// 对齐 Python: _TEAM_CREATE_KINDS = {RunActionKind.CREATE.value, RunActionKind.NEW_TEAM_IN_SESSION.value} (line 62-65)
// Go 端无 RunActionKind 枚举，直接用字符串常量集合。
var teamCreateKinds = map[string]bool{
	"CREATE":              true,
	"NEW_TEAM_IN_SESSION": true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pendingWaitersMu 保护 pendingWaiters 的读写锁。
	// Python 用 asyncio（单线程事件循环）无需锁，Go 需要 sync.RWMutex 保护并发安全。
	pendingWaitersMu sync.RWMutex
	// pendingWaiters 等待事件广播的请求队列。
	// key: [2]string{channelID, sessionID}，value: []pendingWaiter
	// 对齐 Python: _pending_waiters: dict[tuple[str, str], list[tuple[str, asyncio.Queue]]] (line 61)
	pendingWaiters = make(map[[2]string][]pendingWaiter)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// stripDirective 从 query 开头去除 slash 指令前缀。
// 仅当 query 以 prefix 开头且后跟空格或结尾时才去除。
// 对齐 Python: _strip_directive(query, prefix) (line 71-82)
func stripDirective(query, prefix string) (string, bool) {
	stripped := strings.TrimLeft(query, " \t")
	if !strings.HasPrefix(stripped, prefix) {
		return query, false
	}
	remainder := stripped[len(prefix):]
	if len(remainder) > 0 && remainder[0] != ' ' {
		return query, false
	}
	return strings.TrimLeft(remainder, " \t"), true
}

// extractQueryDirectives 从首次 team 请求中提取并去除所有 slash 指令。
// 返回 (cleanedQuery, hideDM, debug)。
// 对齐 Python: _extract_query_directives(query) (line 85-92)
func extractQueryDirectives(query string) (string, bool, bool) {
	query, hideDM := stripDirective(query, hideDMPrefix)
	query, debug := stripDirective(query, debugPrefix)
	return query, hideDM, debug
}

// resolveChannelID 规范化 channelID，空串回退为 "default"。
// 对齐 Python: _resolve_channel_id(channel_id) (line 129-130)
func resolveChannelID(channelID string) string {
	id := strings.TrimSpace(channelID)
	if id == "" {
		return "default"
	}
	return id
}

// isLeaderOutput 判断 team OutputSchema chunk 是否应展示给 claw 用户。
// 对齐 Python: _is_leader_output(chunk) (line 237-257)
//
// 规则：
//   - chunk["type"]=="message" 且 payload 中 event_type 为 "team.runtime_ready"/"team.completed" → true
//   - chunk["type"]=="team.runtime_ready" → true
//   - chunk["role"] 为 TeamRole.LEADER → true
//   - role 为 nil → true（默认视为 leader）
//   - 其他 → false
func isLeaderOutput(chunk map[string]any) bool {
	if chunk == nil {
		return true
	}
	chunkType, _ := chunk["type"].(string)
	payload, _ := chunk["payload"].(map[string]any)

	// team.runtime_ready 和 team.completed 是 leader 级控制事件
	if chunkType == "message" && payload != nil {
		if evtType, _ := payload["event_type"].(string); evtType == "team.runtime_ready" || evtType == "team.completed" {
			return true
		}
	}
	if chunkType == "team.runtime_ready" {
		return true
	}

	role := chunk["role"]
	if role == nil {
		return true
	}
	// Python: role == TeamRole.LEADER 或 str(role_value).strip().lower() == TeamRole.LEADER.value
	if roleStr, ok := role.(string); ok {
		return strings.TrimSpace(strings.ToLower(roleStr)) == "leader"
	}
	return false
}

// isTeammateOutput 判断 team OutputSchema chunk 是否来自 teammate。
// 对齐 Python: _is_teammate_output(chunk) (line 260-268)
func isTeammateOutput(chunk map[string]any) bool {
	if chunk == nil {
		return false
	}
	role := chunk["role"]
	if role == nil {
		return false
	}
	if roleStr, ok := role.(string); ok {
		return strings.TrimSpace(strings.ToLower(roleStr)) == "teammate"
	}
	return false
}

// enrichTeammateEvent 丰富 teammate 事件，添加 role 和 member_name 字段。
// 对齐 Python: _enrich_teammate_event(parsed, chunk) (line 271-278)
func enrichTeammateEvent(parsed map[string]any, chunk map[string]any) map[string]any {
	if parsed == nil {
		parsed = map[string]any{}
	}
	parsed["role"] = "teammate"
	// Python: source_member = getattr(chunk, "source_member", None)
	if sourceMember, _ := chunk["source_member"].(string); sourceMember != "" {
		parsed["member_name"] = sourceMember
	}
	return parsed
}

// isDuplicateAskUserQuestion 去重 chat.ask_user_question 事件。
// 对齐 Python: _is_duplicate_ask_user_question(parsed, emitted_request_ids) (line 281-293)
//
// 若 event_type != "chat.ask_user_question" 或 request_id 为空 → false（不去重）。
// 若 request_id 已在集合中 → true（重复）。
// 否则加入集合返回 false。
func isDuplicateAskUserQuestion(parsed map[string]any, emittedRequestIDs map[string]bool) bool {
	if parsed == nil {
		return false
	}
	evtType, _ := parsed["event_type"].(string)
	if evtType != "chat.ask_user_question" {
		return false
	}
	requestID := strings.TrimSpace(teamHelperStrFromAny(parsed["request_id"]))
	if requestID == "" {
		return false
	}
	if emittedRequestIDs[requestID] {
		return true
	}
	emittedRequestIDs[requestID] = true
	return false
}

// teamHelperStrFromAny 从 any 值中提取字符串。
// 命名为 teamHelperStrFromAny 避免与同包 deep_adapter_evolution.go 中已有的 strFromAny 混淆。
func teamHelperStrFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// approvalChunkFromEvent 从事件中提取审批 chunk。
// 对齐 Python: _approval_chunk_from_event(evt) (line 199-209)
//
// 检查 event_type == "chat.ask_user_question" 且 request_id 和 questions 有效。
// Go 端直接对 map[string]any 操作（Python 调 parse_stream_chunk，但 Go 端的事件已是解析后的 dict）。
func approvalChunkFromEvent(evt map[string]any) map[string]any {
	if evt == nil {
		return nil
	}
	evtType, _ := evt["event_type"].(string)
	if evtType != "chat.ask_user_question" {
		return nil
	}
	requestID, _ := evt["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		return nil
	}
	questions, _ := evt["questions"].([]any)
	if len(questions) == 0 {
		return nil
	}
	return evt
}

// approvalResultFromEventOrItems 构建审批结果 dict。
// 对齐 Python: _approval_result_from_event_or_items(...) (line 212-234)
//
// 优先从 event 提取 approval_chunk；次选 items 非空时返回 invalid_output；最后返回 no_changes_output。
func approvalResultFromEventOrItems(skillName string, event any, items []map[string]any, noChangesOutput, invalidOutput string) map[string]any {
	approvalChunk := approvalChunkFromEvent(teamHelperEventAsMap(event))
	if approvalChunk != nil {
		questions, _ := approvalChunk["questions"].([]any)
		return map[string]any{
			"output":          fmt.Sprintf("Skill '%s' 演进请求已生成，请在审批弹框中确认。", skillName),
			"result_type":     "answer",
			"approval_chunks": []map[string]any{approvalChunk},
			"question_count":  len(questions),
		}
	}
	if len(items) > 0 {
		return map[string]any{
			"output":      invalidOutput,
			"result_type": "error",
		}
	}
	return map[string]any{
		"output":      noChangesOutput,
		"result_type": "answer",
	}
}

// teamHelperEventAsMap 将 any 转为 map[string]any（对齐 Python 中 event 可以是 dict 或对象）。
func teamHelperEventAsMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// teamProcessingDoneChunk 构建 chat.processing_status(is_complete=True) chunk。
// 对齐 Python: _team_processing_done_chunk(request_id, channel_id, session_id) (line 296-311)
func teamProcessingDoneChunk(requestID, channelID, sessionID string) *agentschema.AgentResponseChunk {
	return &agentschema.AgentResponseChunk{
		RequestID: requestID,
		ChannelID: channelID,
		Payload: map[string]any{
			"event_type":    "chat.processing_status",
			"session_id":    sessionID,
			"is_processing": false,
			"is_complete":   true,
		},
		IsComplete: false,
	}
}

// broadcastEvent 向所有等待同一 (channelID, sessionID) 的请求队列广播事件。
// 对齐 Python: _broadcast_event(channel_id, session_id, event) (line 181-196)
func broadcastEvent(channelID, sessionID string, event map[string]any) {
	key := [2]string{resolveChannelID(channelID), sessionID}
	pendingWaitersMu.RLock()
	waiters := pendingWaiters[key]
	pendingWaitersMu.RUnlock()

	for _, w := range waiters {
		// Python: queue.put_nowait(dict(event)) — 浅拷贝，避免多个 waiter 共享同一 map 被修改
		copied := make(map[string]any, len(event))
		for k, v := range event {
			copied[k] = v
		}
		select {
		case w.Ch <- copied:
		default:
			// Python: except Exception — channel 满时丢弃并 debug 日志
			logger.Debug(logComponent).
				Str("channel_id", key[0]).
				Str("session_id", sessionID).
				Str("request_id", w.RequestID).
				Msg("broadcastEvent: channel full, dropping event")
		}
	}
}

// registerWaiter 注册一个请求等待者。
// 对齐 Python: _pending_waiters[key].append((request_id, queue))
func registerWaiter(channelID, sessionID, requestID string, ch chan map[string]any) {
	key := [2]string{resolveChannelID(channelID), sessionID}
	pendingWaitersMu.Lock()
	defer pendingWaitersMu.Unlock()
	pendingWaiters[key] = append(pendingWaiters[key], pendingWaiter{
		RequestID: requestID,
		Ch:        ch,
	})
}

// unregisterWaiter 注销一个请求等待者。
// 对齐 Python: 从 _pending_waiters[key] 中移除对应 (request_id, queue) 条目
func unregisterWaiter(channelID, sessionID, requestID string) {
	key := [2]string{resolveChannelID(channelID), sessionID}
	pendingWaitersMu.Lock()
	defer pendingWaitersMu.Unlock()
	waiters := pendingWaiters[key]
	for i, w := range waiters {
		if w.RequestID == requestID {
			pendingWaiters[key] = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(pendingWaiters[key]) == 0 {
		delete(pendingWaiters, key)
	}
}

// groupTeamEvolutionApprovals 审批分组（team 版包装）。
// 对齐 Python: _group_team_evolution_approvals(session_id, events) (line 314-328)
//
// 委托 evolutionlogic.GroupEvolutionApprovals + warnMissingRequestID 回调。
func groupTeamEvolutionApprovals(sessionID string, events []map[string]any) (map[string][]map[string]any, []string) {
	warnFn := func(sid string) {
		logger.Warn(logComponent).
			Str("session_id", sid).
			Msg("team evolution approval missing request_id")
	}
	return evolutionlogic.GroupEvolutionApprovals(sessionID, events, evolutionlogic.WarnMissingRequestIDFunc(warnFn))
}

// SyncTeamIdentityMetadata 持久化 team 身份元数据（仅新创建的 team 会话）。
// 对齐 Python: sync_team_identity_metadata(*) (line 95-127)
//
// 仅在 activationKind 属于 teamCreateKinds 时执行。
// 已有 team_name 不匹配时保留现有元数据并 warn。
func SyncTeamIdentityMetadata(ctx context.Context, channelID, sessionID, mode, readyTeamName, activationKind string) {
	normalizedKind := strings.TrimSpace(activationKind)
	if !teamCreateKinds[normalizedKind] {
		return
	}

	metadata := sessionmd.GetSessionMetadata(sessionID)
	existingTeamName := ""
	if metadata != nil {
		if tn, ok := metadata["team_name"].(string); ok {
			existingTeamName = strings.TrimSpace(tn)
		}
	}

	// Python: if existing_team_name and existing_team_name != ready_team_name → warn and return
	if existingTeamName != "" && existingTeamName != readyTeamName {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("existing_team_name", existingTeamName).
			Str("new_team_name", readyTeamName).
			Str("activation_kind", normalizedKind).
			Msg("SyncTeamIdentityMetadata: team session identity mismatch, keep existing metadata")
		return
	}

	// Python: update_session_metadata(session_id=session_id, channel_id=..., mode=mode, team_name=ready_team_name)
	resolvedChannel := resolveChannelID(channelID)
	sessionmd.UpdateSessionMetadata(sessionmd.SessionMetadataUpdate{
		SessionID: sessionID,
		ChannelID: &resolvedChannel,
		Mode:      &mode,
		TeamName:  &readyTeamName,
	})
}

// HandleTeamEvolveListCommand 处理 /evolve_list <skill_name> 命令（team 版）。
// 对齐 Python: _handle_team_evolve_list_command(channel_id, session_id, query) (line 421-491)
//
// 通过 TeamManager.GetTeamSkillRail 获取 rail，查询 store 展示经验摘要。
// 非 /evolve_list 前缀返回 nil。
func HandleTeamEvolveListCommand(ctx context.Context, channelID, sessionID, query string) map[string]any {
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/evolve_list") {
		return nil
	}

	tm := team.GetTeamManager(channelID)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		return map[string]any{
			"output":      "团队技能演进记录不可用：未找到 TeamSkillEvolutionRail。",
			"result_type": "error",
		}
	}

	store := rail.EvolutionStore()
	parts := strings.Fields(stripped)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	// Python: if not skill_name or skill_name.startswith("--")
	if skillName == "" || strings.HasPrefix(skillName, "--") {
		return map[string]any{
			"output":      "请指定 Skill 名称：`/evolve_list <skill_name>`",
			"result_type": "error",
		}
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return map[string]any{
			"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
			"result_type": "error",
		}
	}

	records := store.GetRecordsByScore(ctx, skillName, nil)
	if len(records) == 0 {
		return map[string]any{
			"output":      fmt.Sprintf("Skill '%s' 暂无演进经验。", skillName),
			"result_type": "answer",
		}
	}

	// Python: 构建摘要表格 (line 462-487)
	var avgScore float64
	for _, r := range records {
		avgScore += r.Score
	}
	avgScore /= float64(len(records))

	lines := []string{
		fmt.Sprintf("📊 Skill \"%s\" — 经验库摘要\n", skillName),
		fmt.Sprintf("共 %d 条经验 | 平均分：%.2f\n", len(records), avgScore),
		"| # | Score | Section | Content (preview) |",
		"|---|---:|---|---|",
	}
	for i, record := range records {
		section := strings.ReplaceAll(record.Change.Section, "|", "\\|")
		preview := record.Change.Content
		if idx := strings.Index(preview, "\n"); idx >= 0 {
			preview = preview[:idx]
		}
		if len(preview) > 40 {
			preview = preview[:40]
		}
		preview = strings.ReplaceAll(preview, "|", "\\|")
		lines = append(lines, fmt.Sprintf("| %d | %.2f | %s | %s |", i+1, record.Score, section, preview))
	}
	lines = append(lines, fmt.Sprintf("\n提示：使用 /evolve_simplify %s 执行智能整理", skillName))

	return map[string]any{
		"output":      strings.Join(lines, "\n"),
		"result_type": "answer",
	}
}

// HandleTeamSlashCommand 处理 team 模式专用 slash 命令。
// 对齐 Python: _handle_team_slash_command(channel_id, session_id, query) (line 494-608)
//
// 依次检查：/evolve_list → /evolve_simplify → /evolve。
// 非 team slash 命令返回 nil。
func HandleTeamSlashCommand(ctx context.Context, channelID, sessionID, query string) map[string]any {
	// Python 步骤 1: evolve_list_result = await _handle_team_evolve_list_command(...)
	listResult := HandleTeamEvolveListCommand(ctx, channelID, sessionID, query)
	if listResult != nil {
		return listResult
	}

	// Python 步骤 2: 检查是否为 /evolve_simplify 或 /evolve
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/evolve_simplify") && stripped != "/evolve" && !strings.HasPrefix(stripped, "/evolve ") {
		return nil
	}

	tm := team.GetTeamManager(channelID)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		return map[string]any{
			"output":      "团队技能演进不可用：未找到 TeamSkillEvolutionRail。",
			"result_type": "error",
		}
	}

	store := rail.EvolutionStore()

	// Python 步骤 3: /evolve_simplify (line 522-559)
	if strings.HasPrefix(stripped, "/evolve_simplify") {
		parts := strings.Fields(stripped)
		skillName := ""
		if len(parts) > 1 {
			skillName = parts[1]
		}
		var userIntent *string
		if len(parts) > 2 {
			intent := strings.Join(parts[2:], " ")
			userIntent = &intent
		}

		if skillName == "" {
			return map[string]any{
				"output":      "请指定 Skill 名称：`/evolve_simplify <skill_name> [user_intent]`",
				"result_type": "error",
			}
		}

		if !store.SkillExists(ctx, skillName) {
			available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
			if available == "" {
				available = "（无可用 Skill）"
			}
			return map[string]any{
				"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
				"result_type": "error",
			}
		}

		simplifyResult, err := rail.RequestSimplify(ctx, skillName, userIntent)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Msg("HandleTeamSlashCommand: evolve_simplify failed")
			return map[string]any{
				"output":      fmt.Sprintf("团队技能整理分析失败：%s", err),
				"result_type": "error",
			}
		}

		// Python: return _approval_result_from_event_or_items(skill_name, event, items, ...)
		return approvalResultFromEventOrItems(
			skillName,
			simplifyResult.ApprovalEvent,
			simplifyResult.Actions,
			fmt.Sprintf("Skill '%s' 经验库状态良好，无需整理。", skillName),
			fmt.Sprintf("Skill '%s' 精简方案已生成，但审批事件为空或格式无效。", skillName),
		)
	}

	// Python 步骤 4: /evolve (line 561-608)
	parts := strings.Fields(stripped)
	if len(parts) < 2 {
		return map[string]any{
			"output":      "请补充演进意图：`/evolve <skill_name> <user_query>`",
			"result_type": "error",
		}
	}
	skillName := strings.TrimSpace(parts[1])
	userQuery := ""
	if len(parts) > 2 {
		userQuery = strings.TrimSpace(strings.Join(parts[2:], " "))
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return map[string]any{
			"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
			"result_type": "error",
		}
	}

	if userQuery == "" {
		return map[string]any{
			"output":      "请补充演进意图：`/evolve <skill_name> <user_query>`",
			"result_type": "error",
		}
	}

	evolveResult, err := rail.RequestUserEvolution(ctx, skillName, userQuery, false)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("HandleTeamSlashCommand: evolve failed")
		return map[string]any{
			"output":      fmt.Sprintf("团队技能演进请求失败：%s", err),
			"result_type": "error",
		}
	}

	return approvalResultFromEventOrItems(
		skillName,
		evolveResult.ApprovalEvent,
		nil, // Python: items=list(getattr(evolve_result, "records", []) or [])
		fmt.Sprintf("Skill '%s' 未生成新的团队技能演进经验。", skillName),
		fmt.Sprintf("Skill '%s' 已生成团队技能演进经验，但审批事件为空或格式无效。", skillName),
	)
}

// ResolveTeamRebuildFollowup 解析 /evolve_rebuild 命令，返回 followup_prompt。
// 对齐 Python: _resolve_team_rebuild_followup(channel_id, session_id, query) (line 382-418)
func ResolveTeamRebuildFollowup(ctx context.Context, channelID, sessionID, query string) (string, string) {
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/evolve_rebuild") {
		return "", ""
	}

	tm := team.GetTeamManager(channelID)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		return "", "团队技能重建不可用：未找到 TeamSkillEvolutionRail。"
	}

	store := rail.EvolutionStore()
	parts := strings.Fields(stripped)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	var userIntent *string
	if len(parts) > 2 {
		intent := strings.Join(parts[2:], " ")
		userIntent = &intent
	}

	if skillName == "" {
		return "", "请指定 Skill 名称：`/evolve_rebuild <skill_name> [user_intent]`"
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return "", fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available)
	}

	// Python: followup_prompt = await rail.request_rebuild(skill_name, user_intent)
	followupPrompt, err := rail.RequestRebuild(ctx, skillName, userIntent, 0.5)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("ResolveTeamRebuildFollowup: evolve_rebuild failed")
		return "", fmt.Sprintf("团队技能重建分析失败：%s", err)
	}

	if followupPrompt == "" {
		return "", fmt.Sprintf("Skill '%s' 未生成可执行的重建指令。", skillName)
	}

	return followupPrompt, ""
}

// onTeamWatcherDone evolution 观察任务完成回调。
// 对齐 Python: _on_team_watcher_done(task) (line 1086-1098)
func onTeamWatcherDone(sessionID string) {
	logger.Info(logComponent).Str("session_id", sessionID).Msg("onTeamWatcherDone: team evolution watcher done")
}
