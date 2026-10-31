package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	teammonitor "github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	ateamruntime "github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamEventQueueTimeout team 事件队列读取超时。
	// 对齐 Python: asyncio.wait_for(request_queue.get(), timeout=0.1)
	teamEventQueueTimeout = 100 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// findTeamSkillRail 查找拥有指定 pending request_id 的 TeamSkillEvolutionRail。
// S-41: 对齐 Python find_team_skill_rail(request_id, channel_id)——
// 两级查找：先本 channel 的 TeamManager 精确匹配，失败再跨所有 TeamManager 兜底。
// 删除原「缓存字段 + reflect.Type 全局查找」的错误语义（不按 request_id 匹配，
// 且缓存可能命中其他 session 的 rail）。
func (d *DeepAdapter) findTeamSkillRail(requestID, channelID string) *coreevolution.TeamSkillEvolutionRail {
	rail := team.GetTeamManager(channelID).FindTeamSkillRailForRequest(requestID)
	if rail != nil {
		return rail
	}
	return team.FindTeamSkillRailAcrossManagers(requestID)
}

// findSkillCreateRail 查找 TeamSkillCreateRail。
// 对齐 Python: _find_skill_create_rail()
func (d *DeepAdapter) findSkillCreateRail() *coreevolution.TeamSkillCreateRail {
	if d.skillCreateRail != nil {
		return d.skillCreateRail
	}
	// 在 instance 的已注册 rails 中查找
	if d.instance != nil {
		found := d.instance.FindRailsByType(reflect.TypeOf(&coreevolution.TeamSkillCreateRail{}))
		if len(found) > 0 {
			if r, ok := found[0].(*coreevolution.TeamSkillCreateRail); ok {
				return r
			}
		}
	}
	return nil
}

// handleTeamSkillEvolveApproval 处理 team skill 演进审批。
//
// 对齐 Python: handle_team_skill_evolve_approval(request_id, answers, session_id, channel_id)
//
//	(1) 查找 TeamSkillEvolutionRail
//	(2) 解析 answers → approve/reject
//	(3) 调用 rail.ApproveRecord 或 rail.RejectRecord
//	(4) 推送解决状态
func (d *DeepAdapter) handleTeamSkillEvolveApproval(ctx context.Context, requestID string, answers any, sessionID string, channelID string) bool {
	// S-41: 两级查找（本 channel → 跨 manager），对齐 Python find_team_skill_rail(request_id, channel_id)
	rail := d.findTeamSkillRail(requestID, channelID)
	if rail == nil {
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: TeamSkillEvolutionRail not initialized")
		return false
	}

	// S-42: 对齐 Python _option_matches(answers, ("accept", "接收", "接受"))——
	// 读 selected_options 字段，strip().lower() 大小写不敏感匹配
	parsedAnswers := parseApprovalAnswersFromAny(answers)
	accepted := optionMatchesLabels(parsedAnswers, "accept", "接收", "接受")

	logger.Info(logComponent).
		Str("request_id", requestID).
		Any("answers", answers).
		Bool("accepted", accepted).
		Msg("team skill evolve approval")

	if accepted {
		if err := rail.ApproveRecord(ctx, requestID); err != nil {
			logger.Error(logComponent).Err(err).
				Str("request_id", requestID).
				Msg("handleTeamSkillEvolveApproval: ApproveRecord failed")
			return false
		}
		// S-42: 对齐 Python——审批成功后同步工作区技能到全局 team_skills 目录
		// （同步失败仅 Warn，不影响审批结果，对齐 Python except 只打日志）
		if sessionID != "" {
			if !team.SyncTeamSkillsAcrossManagers(sessionID) {
				logger.Warn(logComponent).
					Str("session_id", sessionID).
					Msg("team skill sync after patch failed")
			}
		}
		logger.Info(logComponent).
			Str("request_id", requestID).
			Msg("team skill evolve accepted")
	} else {
		if err := rail.RejectRecord(ctx, requestID); err != nil {
			logger.Error(logComponent).Err(err).
				Str("request_id", requestID).
				Msg("handleTeamSkillEvolveApproval: RejectRecord failed")
			return false
		}
		logger.Info(logComponent).
			Str("request_id", requestID).
			Msg("team skill evolve rejected")
	}

	// 推送解决状态（对齐 Python：approve/reject 后统一推送关闭前端演进状态）
	_ = d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, sessionID, channelID, accepted)
	return true
}

// pushTeamSkillEvolveResolutionStatus 推送 team skill 演进解决状态，关闭前端演进状态。
// S-42: 实装（原为空壳只记日志）。
// 对齐 Python _push_team_skill_evolve_resolution_status——
// session_id 为空直接返回；stage=completed/hidden；status=end；
// 推送失败仅 Warn（对齐 Python except 只打日志）。
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(ctx context.Context, requestID, sessionID, channelID string, accepted bool) error {
	// 对齐 Python: if not session_id: return
	if sessionID == "" {
		return nil
	}
	stage := "hidden"
	message := "Team skill evolution rejected"
	if accepted {
		stage = "completed"
		message = "Team skill evolution accepted"
	}
	sendPush := d.getSendPushFunc()
	if sendPush == nil {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("pushTeamSkillEvolveResolutionStatus: sendPushFunc not injected")
		return nil
	}
	update := evolutionlogic.BuildEvolutionStatusUpdate(requestID, "end", stage, message)
	payload := map[string]any{
		"event_type": "chat.evolution_status",
		"status":     update.Status,
		"stage":      update.Stage,
		"message":    update.Message,
		"request_id": update.RequestID,
	}
	msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
	if err := sendPush(ctx, msg); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("request_id", requestID).
			Msg("team skill evolve status push failed")
	}
	return nil
}

// optionMatchesLabels 检查任一 answer 的 selected_options 是否匹配接受标签。
// S-42: 参数化版本，对齐 Python _option_matches(answers, accept_labels)——
// strip().lower() 大小写不敏感，容忍中英文标签（agent-core 中性英文 + UI 本地化中文）。
func optionMatchesLabels(answers []ApprovalAnswer, labels ...string) bool {
	normalizedAccept := make(map[string]bool, len(labels))
	for _, s := range labels {
		normalizedAccept[strings.ToLower(strings.TrimSpace(s))] = true
	}
	for _, ans := range answers {
		for _, opt := range ans.SelectedOptions {
			if normalizedAccept[strings.ToLower(strings.TrimSpace(opt))] {
				return true
			}
		}
	}
	return false
}

// processTeamMessageStream team 模式流式消息处理。
// 对齐 Python: team_helpers.process_team_message_stream()
// 9.55: 实现核心分流逻辑
//
// 改造后：通过 channel_id → TeamManager 路由，对齐 Python 的 get_team_manager(channel_id).interact()
// 而非通过 inputs["params"]["team_name"] 提取 teamName
//
// 参数分工对齐 Python：
//   - req（AgentRequest）：元信息 — session_id, request_id, channel_id
//   - inputs：业务参数 — query, params.team_name 等
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req *agentschema.AgentRequest, inputs map[string]any) (<-chan *agentschema.AgentResponseChunk, error) {
	logger.Info(logComponent).Str("mode", "team").Msg("processTeamMessageStream: team mode routing")

	ch := make(chan *agentschema.AgentResponseChunk, 64)

	// 防御：req 为 nil 时直接返回空流
	if req == nil {
		close(ch)
		return ch, nil
	}

	// 步骤 1: 从 req 获取元信息（对齐 Python: request.session_id / request.request_id / request.channel_id）
	// Python: session_id = request.session_id or "default" (team_helpers.py L616)
	sessionID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		sessionID = *req.SessionID
	}
	channelID := "default"
	if req.ChannelID != "" {
		channelID = req.ChannelID
	}

	// 步骤 2: 通过 channel_id 获取 TeamManager
	// 对齐 Python: get_team_manager(channel_id)
	// 事件广播 / code member 配置回调由 adapter 包 init 注册为 team 包默认值，
	// NewTeamManager 创建实例时继承（对齐 Python 模块级函数恒可用），
	// 此请求路径不再做任何注入写操作。
	teamManager := team.GetTeamManager(channelID)

	// 步骤 3: S-04——等待该 session 的 pending shutdown 清理完成（最多 15s）。
	// 对齐 Python team_helpers.py L624-635：前一个 team session 全员 shutdown 后
	// 清理（远程成员 bootstrap 清理）未完成时，等待其结束再继续，避免新请求
	// 读到残留的远程成员状态。等待失败（超时/panic）仅 warning，不中断请求。
	team.WaitForPendingShutdownCleanupForSession(ctx, sessionID)

	// 对齐 Python: if not team_manager.has_stream_task(session_id)
	isFirstRequest := !teamManager.HasStreamTask(sessionID)

	go func() {
		defer close(ch)

		// S-45: panic recovery——Go panic 展开时 defer 照常执行，recover 后 close(ch)
		// 正常关闭，消费方不阻塞。panic 时推送 chat.error + IsComplete 终止 chunk
		// （对齐 Python except L853-865 + fall-through L867-872）。
		defer func() {
			if r := recover(); r != nil {
				logger.Error(logComponent).
					Str("session_id", sessionID).Str("channel_id", channelID).
					Any("panic", r).
					Msg("processTeamMessageStream: stream goroutine panic recovered")
				ch <- &agentschema.AgentResponseChunk{
					RequestID:  req.RequestID,
					ChannelID:  channelID,
					Payload:    map[string]any{"event_type": "chat.error", "error": fmt.Sprintf("%v", r)},
					IsComplete: false,
				}
				ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
			}
		}()

		// S-45: waiter 清理 defer 化——正常结束与 panic 两条路径都执行
		// （对齐 Python finally 的 `if request_queue is not None` 判断）。
		waiterRegistered := false
		defer func() {
			if waiterRegistered {
				unregisterWaiter(channelID, sessionID, req.RequestID)
			}
		}()

		// 步骤 4: 提取 query directives（对齐 Python: _extract_query_directives）
		// Python: if is_first_request: query, hide_dm, debug = _extract_query_directives(str(query or ""))
		queryText := paramsString(inputs, "query", "")
		hideDM := false
		debug := false
		if isFirstRequest {
			cleanedQuery, h, dbg := extractQueryDirectives(queryText)
			if h || dbg {
				logger.Info(logComponent).
					Str("session_id", sessionID).
					Str("channel_id", channelID).
					Bool("hide_dm", h).
					Bool("debug", dbg).
					Msg("processTeamMessageStream: query directives captured for first team request")
			}
			queryText = cleanedQuery
			hideDM = h
			debug = dbg
		}

		// 步骤 5: 处理 team slash 命令
		// 对齐 Python: slash_result = await _handle_team_slash_command(channel_id, session_id, query_text)
		slashResult := HandleTeamSlashCommand(ctx, channelID, sessionID, queryText)
		if slashResult != nil {
			// 审批 chunks（对齐 Python: if approval_chunks → yield chunks + done + complete）
			if approvalChunks, ok := slashResult["approval_chunks"].([]map[string]any); ok && len(approvalChunks) > 0 {
				for _, chunk := range approvalChunks {
					ch <- &agentschema.AgentResponseChunk{
						RequestID:  req.RequestID,
						ChannelID:  channelID,
						Payload:    chunk,
						IsComplete: false,
					}
				}
				ch <- teamProcessingDoneChunk(req.RequestID, channelID, sessionID)
				ch <- &agentschema.AgentResponseChunk{
					RequestID:  req.RequestID,
					ChannelID:  channelID,
					Payload:    map[string]any{"event_type": "chat.done"},
					IsComplete: true,
				}
				return
			}
			// 非审批结果（对齐 Python: result_type == "error" → chat.error, else → chat.final）
			resultType, _ := slashResult["result_type"].(string)
			content, _ := slashResult["output"].(string)
			var payload map[string]any
			if resultType == "error" {
				payload = map[string]any{"event_type": "chat.error", "error": content}
			} else {
				payload = map[string]any{"event_type": "chat.final", "content": content}
			}
			ch <- &agentschema.AgentResponseChunk{
				RequestID:  req.RequestID,
				ChannelID:  channelID,
				Payload:    payload,
				IsComplete: false,
			}
			ch <- teamProcessingDoneChunk(req.RequestID, channelID, sessionID)
			ch <- &agentschema.AgentResponseChunk{
				RequestID:  req.RequestID,
				ChannelID:  channelID,
				IsComplete: true,
			}
			return
		}

		// 步骤 6: 获取 enriched team spec（对齐 Python L694-706——在 slash 命令后、
		// is_first_request 分支前执行，两分支共用）
		// Python: team_spec = await team_manager.get_enriched_team_spec(session_id, deep_agent, ...)
		var requestMetadata map[string]any
		if req.Metadata != nil {
			requestMetadata = make(map[string]any, len(req.Metadata))
			for k, v := range req.Metadata {
				requestMetadata[k] = v
			}
		}
		// M-34: 对齐 Python L703-704——params 是 dict 时 setdefault "mode"
		// （mode=="team.plan" 是 ApplyTeamPlanMode 的唯一入口，Metadata 优先级高于 Params）。
		// req.Params 为 json.RawMessage 延迟解析：数组/字符串 → Unmarshal 失败跳过
		// （= Python isinstance 非 dict）；null → paramsMap==nil 跳过（= Python params None）。
		if len(req.Params) > 0 {
			var paramsMap map[string]any
			if err := json.Unmarshal(req.Params, &paramsMap); err == nil && paramsMap != nil {
				if _, exists := requestMetadata["mode"]; !exists {
					if requestMetadata == nil {
						requestMetadata = make(map[string]any, 1)
					}
					requestMetadata["mode"] = paramsMap["mode"]
				}
			}
		}
		teamSpec := teamManager.GetEnrichedTeamSpec(ctx, sessionID, d.instance, req.RequestID, channelID, requestMetadata)
		if teamSpec == nil {
			logger.Error(logComponent).Str("session_id", sessionID).
				Msg("processTeamMessageStream: GetEnrichedTeamSpec returned nil")
			ch <- &agentschema.AgentResponseChunk{
				RequestID: req.RequestID, ChannelID: channelID,
				Payload:    map[string]any{"event_type": "chat.error", "error": "TeamAgent 创建失败：无法获取 team spec"},
				IsComplete: false,
			}
			ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
			return
		}
		teamName := teamSpec.TeamName

		// 步骤 7: 解析 /evolve_rebuild followup（S-22: 对齐 Python L729-750——
		// 在 is_first_request 分支之前执行，首次/后续两条路径共用；原先放在首请求块内，
		// 导致后续请求的 /evolve_rebuild 完全失效）
		// Python: followup_prompt, rebuild_error = await _resolve_team_rebuild_followup(channel_id, session_id, query_text)
		followupPrompt, rebuildError := ResolveTeamRebuildFollowup(ctx, channelID, sessionID, queryText)
		if rebuildError != "" {
			ch <- &agentschema.AgentResponseChunk{
				RequestID: req.RequestID, ChannelID: channelID,
				Payload:    map[string]any{"event_type": "chat.error", "error": rebuildError},
				IsComplete: false,
			}
			ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
			return
		}
		if followupPrompt != "" {
			queryText = followupPrompt
		}

		if isFirstRequest {
			// ─── 首次请求：完整 TeamAgent 创建 + streaming 流程 ───
			// 对齐 Python: process_team_message_stream 首次请求分支 (line 699-887)
			logger.Info(logComponent).Str("session_id", sessionID).Str("channel_id", channelID).
				Msg("processTeamMessageStream: first request")

			// 步骤 1: PrepareRuntimeActivation
			// Python: team_manager.ensure_team_shared_skills_initialized(team_spec)
			// Python: await team_manager.prepare_runtime_activation(session_id, team_name)
			teamManager.EnsureTeamSharedSkillsInitialized(teamSpec)
			if err := teamManager.PrepareRuntimeActivation(ctx, sessionID, teamName); err != nil {
				logger.Error(logComponent).Err(err).Str("session_id", sessionID).
					Msg("processTeamMessageStream: PrepareRuntimeActivation failed")
				ch <- &agentschema.AgentResponseChunk{
					RequestID: req.RequestID, ChannelID: channelID,
					Payload:    map[string]any{"event_type": "chat.error", "error": err.Error()},
					IsComplete: false,
				}
				ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
				return
			}

			// 步骤 4: 注册 waiter + 启动后台流任务
			// Python: request_queue = asyncio.Queue()
			// Python: waiter_key = (_resolve_channel_id(channel_id), session_id)
			// Python: _pending_waiters[waiter_key].append((rid, request_queue))
			requestQueue := make(chan map[string]any, 256)
			registerWaiter(channelID, sessionID, req.RequestID, requestQueue)
			waiterRegistered = true // S-45: defer 清理标记
			logger.Info(logComponent).
				Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
				Msg("processTeamMessageStream: first team request, waiter registered")

			// 步骤 5: 构建流环境变量 + 递增 round 计数
			// Python: stream_envs: dict[str, Any] = {}
			// Python: if hide_dm: stream_envs["hide_dm"] = True
			// Python: if debug: stream_envs[_STREAM_TRACE_ENV_KEY] = "1"
			// Python: round_id = increment_session_round_count(session_id)
			streamEnvs := map[string]any{}
			if hideDM {
				streamEnvs["hide_dm"] = true
			}
			if debug {
				streamEnvs[streamTraceEnvKey] = "1"
			}
			roundID, _ := sessionmd.IncrementSessionRoundCount(sessionID)

			// 步骤 6: 启动后台 goroutine 消费 team 流
			// Python: stream_task = asyncio.create_task(_consume_stream_with_query(...))
			streamCtx, streamCancel := context.WithCancel(ctx)
			// M-20: 先注册后启动——Python create_task 是惰性调度（任务体在控制权让出后才跑），
			// 文本顺序安全；Go 的 go 语句立即执行，若 goroutine 先跑完（如 RunAgentTeamStreaming
			// 立即出错），defer PopStreamTask 与 RegisterStreamTask 竞争会产生 streamTasks
			// 残留死条目。先注册再启动消除该竞窗。
			teamManager.RegisterStreamTask(sessionID, streamCancel)
			go d.consumeStreamWithQuery(streamCtx, channelID, sessionID, teamSpec, queryText, roundID, streamEnvs)

			// 步骤 8: 从 waiter queue 读取事件并输出到 ch
			// 对齐 Python: while team_manager.has_stream_task(session_id):
			//   event = await asyncio.wait_for(request_queue.get(), timeout=0.1)
			//   yield AgentResponseChunk(...)
			for teamManager.HasStreamTask(sessionID) {
				var event map[string]any
				select {
				case event = <-requestQueue:
					ch <- &agentschema.AgentResponseChunk{
						RequestID:  req.RequestID,
						ChannelID:  channelID,
						Payload:    event,
						IsComplete: false,
					}
					// Python: if event.get("event_type") == "team.error": break
					// S-08: 裸 break 只退出 select 不退出 for——对齐 Python break 退出整个
					// while 循环语义，改用 goto streamDone（与超时/取消分支同一出口）。
					if event != nil {
						if evtType, _ := event["event_type"].(string); evtType == "team.error" {
							goto streamDone
						}
					}
				case <-time.After(teamEventQueueTimeout):
					// 对齐 Python: except asyncio.TimeoutError: if not team_manager.has_stream_task: break; continue
					if !teamManager.HasStreamTask(sessionID) {
						goto streamDone
					}
					continue
				case <-ctx.Done():
					logger.Info(logComponent).
						Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).Str("request_id", req.RequestID).
						Msg("processTeamMessageStream: event stream cancelled")
					goto streamDone
				}
			}

		streamDone:
			ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}

			// S-45: waiter 清理由 defer 统一处理（panic/正常两路径）
			return
		}

		// ─── 后续请求：通过 TeamManager.Interact 路由 ───
		// 对齐 Python: team_manager.interact(session_id, user_input) (line 784-822)
		logger.Info(logComponent).
			Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
			Msg("processTeamMessageStream: follow-up team request")

		// M-02: 对齐 Python 后续请求使用 directive 清洗后的 query 变量（上文 queryText），
		// 不再重新读取原始 inputs["query"]
		if queryText != "" {
			// S-07: Interact 只返回 bool（对齐 Python 永不抛异常语义）
			// T-01: 使用类型化构造函数 NewInteractInputFromString（普通用户消息形态）
			if !teamManager.Interact(ctx, sessionID, interaction.NewInteractInputFromString(queryText)) {
				logger.Warn(logComponent).Str("session_id", sessionID).
					Msg("processTeamMessageStream: Interact returned false")
				ch <- &agentschema.AgentResponseChunk{
					RequestID: req.RequestID, ChannelID: channelID,
					Payload:    map[string]any{"event_type": "chat.error", "error": "interact failed"},
					IsComplete: false,
				}
				ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
				return
			}
		}

		logger.Info(logComponent).
			Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).Str("request_id", req.RequestID).
			Msg("processTeamMessageStream: follow-up request submitted without waiter")
		ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
	}()

	return ch, nil
}

// ensureMonitorForActiveRuntime 在 runtime 就绪后挂载 TeamMonitorHandler。
// 对齐 Python: ensure_monitor_for_active_runtime(channel_id, session_id, team_name, hide_dm) (line 133-178)
//
// Python 步骤：
//
//	Python: 1. tm = get_team_manager(channel_id)
//	Python: 2. existing = tm.get_monitor(session_id); if existing and existing.is_running: return
//	Python: 3. monitor = await Runner.get_agent_team_monitor(team_name=, session_id=, hide_dm=)
//	Python: 4. monitor_handler = TeamMonitorHandler(monitor, session_id)
//	Python: 5. await monitor_handler.start()
//	Python: 6. tm.register_monitor(session_id, monitor_handler)
//	Python: 7. if monitor_handler.is_running: asyncio.create_task(_consume_monitor_events(...))
//
// Go 差异：Go 端直接通过 TeamManager.EnsureMonitor 实现（内部封装了从 TeamAgent 创建
// TeamMonitor、启动 handler、注册 monitor、启动事件消费、自动启动 evolution watcher
// 等全部步骤），无需经过 Runner.get_agent_team_monitor 桥接层。
func (d *DeepAdapter) ensureMonitorForActiveRuntime(ctx context.Context, channelID, sessionID, teamName string, hideDM bool) {
	tm := team.GetTeamManager(channelID)

	// 通过 TeamAgent 获取并启动 monitor
	teamAgent := tm.GetTeamAgent(sessionID)
	if teamAgent == nil {
		logger.Warn(logComponent).
			Str("session_id", sessionID).Str("team_name", teamName).
			Msg("ensureMonitorForActiveRuntime: no TeamAgent, cannot start monitor")
		return
	}

	if err := tm.EnsureMonitor(ctx, sessionID, teamAgent, hideDM); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("session_id", sessionID).Str("team_name", teamName).
			Msg("ensureMonitorForActiveRuntime: EnsureMonitor failed")
		return
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).Str("team_name", teamName).
		Msg("ensureMonitorForActiveRuntime: monitor started")
}

// ensureTeamEvolutionWatcher 启动 team evolution 监控。
// 对齐 Python: ensure_team_evolution_watcher(channel_id, session_id, *, source) (line 331-379)
//
// Python 步骤：
//
//	Python: 1. tm = get_team_manager(channel_id)
//	Python: 2. watcher = tm.get_team_evolution_watcher(session_id)
//	Python: 3. if watcher is not None and not watcher.done(): return
//	Python: 4. rail = tm.get_team_skill_rail(session_id)
//	Python: 5. if rail is None: return
//	Python: 6. if not getattr(rail, "auto_scan", True): return
//	Python: 7. task = asyncio.create_task(_watch_team_evolution_and_push(channel_id, session_id, rail))
//	Python: 8. setattr(task, "_team_channel_id", channel_id)
//	Python: 9. setattr(task, "_team_session_id", session_id)
//
// 10. task.add_done_callback 注册完成回调（_on_team_watcher_done）
// 11. tm.register_team_evolution_watcher 注册 watcher（session_id, task）
func (d *DeepAdapter) ensureTeamEvolutionWatcher(ctx context.Context, channelID, sessionID, source string) {
	tm := team.GetTeamManager(channelID)

	// Python 步骤 2: watcher = tm.get_team_evolution_watcher(session_id)
	// Python 步骤 3: if watcher is not None and not watcher.done()
	cancelFn := tm.GetTeamEvolutionWatcher(sessionID)
	if cancelFn != nil {
		logger.Info(logComponent).
			Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: evolution monitor already running")
		return
	}

	// Python 步骤 4: rail = tm.get_team_skill_rail(session_id)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		logger.Warn(logComponent).
			Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: no TeamSkillEvolutionRail found, deferred")
		return
	}

	// Python 步骤 5: if not getattr(rail, "auto_scan", True)
	if !rail.AutoScan() {
		logger.Info(logComponent).
			Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: auto_scan disabled, skipped")
		return
	}

	// Python 步骤 6-7: 启动 watcher goroutine
	logger.Info(logComponent).
		Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
		Msg("ensureTeamEvolutionWatcher: launching evolution monitor")

	watcherCtx, watcherCancel := context.WithCancel(ctx)
	tm.RegisterTeamEvolutionWatcher(sessionID, watcherCancel)

	go func() {
		defer logger.Info(logComponent).Str("session_id", sessionID).Msg("ensureTeamEvolutionWatcher: evolution watcher goroutine exited")
		// S-43: goroutine 层兜底 recover——函数级 recover 注册前的 panic 也不会击穿进程，
		// 且保证 onTeamWatcherDone 仍执行（对齐 Python task done callback 语义）
		defer func() {
			if r := recover(); r != nil {
				logger.Error(logComponent).
					Str("session_id", sessionID).
					Any("panic", r).
					Msg("ensureTeamEvolutionWatcher: watcher goroutine panic recovered")
			}
		}()
		d.watchTeamEvolutionAndPushTeam(watcherCtx, channelID, sessionID, rail)
		// M-02: Pop 移入 onTeamWatcherDone（对齐 Python done callback 内 pop）
		onTeamWatcherDone(channelID, sessionID)
	}()
}

// consumeStreamWithQuery 后台消费 team 流并广播事件。
// 对齐 Python: _consume_stream_with_query(channel_id, session_id, team_spec, initial_query, *, round_id, envs) (line 889-1048)
//
// Python 步骤：
//
//  1. 广播 round-start 信号
//  2. 调用 Runner.run_agent_team_streaming 获取流
//  3. 遍历 chunk：过滤 leader/teammate → 解析 → 去重 → 广播
//  4. 处理 runtime_ready 事件（sync metadata + commit + attach hooks + ensure monitor + ensure evolution watcher）
//  5. 处理 team.completed 事件（广播 round-complete 信号）
//  6. 流结束：无 chunk → broadcast team.error；有 chunk → 记录日志
//  7. finally: 清理 pending 并弹出 stream task（对齐 Python finally: clear pending + pop stream task）
func (d *DeepAdapter) consumeStreamWithQuery(ctx context.Context, channelID, sessionID string, teamSpec any, initialQuery string, roundID int, envs map[string]any) {
	_envs := envs
	if _envs == nil {
		_envs = map[string]any{}
	}
	hideDM := false
	if v, ok := _envs["hide_dm"].(bool); ok {
		hideDM = v
	}

	receivedChunks := 0
	emittedAskUserRequestIDs := make(map[string]bool)

	defer func() {
		// Python 步骤 7: finally
		tm := team.GetTeamManager(channelID)
		tm.ClearPendingRuntime(sessionID)
		tm.PopStreamTask(sessionID)
	}()

	// S-44: 对齐 Python consume_stream_with_query 的 except Exception 块——
	// panic 时广播 team.error（含 error/session_id），前端能感知流失败。
	// defer LIFO：本 defer 后注册先执行，recover 后再走上方清理 defer，
	// 天然对齐 Python except → finally 顺序。
	// CancelledError 分支无需移植（Go ctx 取消走正常 return 路径不 panic）。
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("session_id", sessionID).Int("round_id", roundID).
				Any("panic", r).
				Msg("consumeStreamWithQuery: stream failed")
			broadcastEvent(channelID, sessionID, map[string]any{
				"event_type": "team.error",
				"error":      fmt.Sprintf("%v", r),
				"session_id": sessionID,
			})
		}
	}()

	logger.Info(logComponent).
		Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).Int("round_id", roundID).
		Msg("consumeStreamWithQuery: stream started")

	// Python 步骤 1: 广播 round-start 信号
	broadcastEvent(channelID, sessionID, map[string]any{
		"event_type":    "chat.processing_status",
		"session_id":    sessionID,
		"rid":           roundID,
		"is_processing": true,
		"is_complete":   false,
	})

	streamTraceEnabled := false
	if v, ok := _envs[streamTraceEnvKey].(string); ok && v != "" {
		streamTraceEnabled = true
	}
	if !streamTraceEnabled {
		if v := os.Getenv(streamTraceEnvKey); v != "" {
			streamTraceEnabled = true
		}
	}
	// T-01 遗留点 B（已回填）：trace 开启时创建 TeamStreamLogger，
	// 对齐 Python: lg = TeamStreamLogger(file_path=str(traces_dir / f"dump-team-{session_id}.txt"))
	var lg *teammonitor.TeamStreamLogger
	if streamTraceEnabled {
		tracesDir := filepath.Join(agentteams.GetAgentTeamsHome(), "traces")
		if l, err := teammonitor.NewTeamStreamLogger(filepath.Join(tracesDir, fmt.Sprintf("dump-team-%s.txt", sessionID))); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Msg("consumeStreamWithQuery: failed to create TeamStreamLogger")
		} else {
			lg = l
		}
	}

	// Python 步骤 2: 调用 Runner.run_agent_team_streaming
	// Go 差异：Python 通过 Runner.run_agent_team_streaming(agent_team=team_spec, inputs=..., session=..., envs=..., stream_logger=lg)
	// S-71(A1): Go 端走具体类型入口 RunTeamAgentStreaming——路由函数已删除，
	// teamSpec 即 *atschema.TeamAgentSpec（GetEnrichedTeamSpec 返回类型）。
	streamCh, err := ateamruntime.RunTeamAgentStreaming(ctx, teamSpec.(*atschema.TeamAgentSpec), map[string]any{"query": initialQuery}, sessionID, lg)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", sessionID).
			Msg("consumeStreamWithQuery: RunAgentTeamStreaming failed")
		broadcastEvent(channelID, sessionID, map[string]any{
			"event_type": "team.error",
			"error":      err.Error(),
			"session_id": sessionID,
		})
		return
	}

	// Python 步骤 3: 遍历流 chunk
	// S-05: 对齐 Python except CancelledError——select+ctx.Done 显式响应取消：
	// 记日志后正常退出（取消≠失败，不走 receivedChunks==0 的 team.error 分支）。
	// 生产侧 S-58 已在 team_runner.go 发送端 select 化并会在取消时关闭 mergedCh，
	// 此处消费侧补齐取消感知与日志。
	streamCancelled := false
loop:
	for {
		select {
		case <-ctx.Done():
			streamCancelled = true
			// Python: except asyncio.CancelledError: logger.info("[TeamHelpers] stream cancelled: ...")
			logger.Info(logComponent).
				Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).Int("round_id", roundID).
				Err(ctx.Err()).
				Msg("consumeStreamWithQuery: stream cancelled")
			break loop
		case chunk, ok := <-streamCh:
			if !ok {
				break loop
			}
			receivedChunks++

			// 将 stream.Schema 转为 map[string]any
			chunkMap := chunkToMap(chunk)
			if chunkMap == nil {
				continue
			}

			isLeader := isLeaderOutput(chunkMap)
			isTeammate := isTeammateOutput(chunkMap)
			if !isLeader && !isTeammate {
				continue
			}

			parsed := utilsParseStreamChunk(chunkMap)
			if parsed == nil {
				continue
			}

			if isDuplicateAskUserQuestion(parsed, emittedAskUserRequestIDs) {
				continue
			}

			parsed["rid"] = roundID

			if isTeammate {
				parsed = enrichTeammateEvent(parsed, chunkMap)
			}

			// Python 步骤 4: 处理 runtime_ready 事件
			if evtType, _ := parsed["event_type"].(string); evtType == "team.runtime_ready" {
				readyTeamName, _ := parsed["team_name"].(string)
				if readyTeamName == "" {
					if specTeamName := teamSpecTeamName(teamSpec); specTeamName != "" {
						readyTeamName = specTeamName
					}
				}
				activationKind := ""
				if v, _ := parsed["activation_kind"].(string); v != "" {
					activationKind = v
				}

				// 对齐 Python 步骤（Go 等价实现依次如下）：
				// 同步团队身份元数据 → 提交 runtime_ready → 附加分布式 hooks →
				// 确保活跃 runtime 的监控 → 注册团队进化 watcher
				SyncTeamIdentityMetadata(ctx, channelID, sessionID, "team", readyTeamName, activationKind)

				tm := team.GetTeamManager(channelID)
				tm.CommitRuntimeReady(sessionID, readyTeamName)

				// 分布式 hooks（S-04 回填；M-09: channelID 透传——原传 nil 导致
				// shutdown cleanup wrapper 丢失 channel 上下文，notice 推送回退 default）
				tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, &channelID)

				d.ensureMonitorForActiveRuntime(ctx, channelID, sessionID, readyTeamName, hideDM)
				d.ensureTeamEvolutionWatcher(ctx, channelID, sessionID, "runtime_ready")
			} else if evtType == "team.completed" {
				// Python 步骤 5: 处理 team.completed 事件
				// M-10: 此处有意重建事件（非直传 parsed）——对齐 Python
				// team_helpers.py L979-995：team.completed 触发的是统一的
				// round-complete 信号（chat.processing_status），只透传
				// member_count/task_count 两个统计字段，原始事件的其余字段
				//（如内部状态）按 Python 语义不透出。
				broadcastEvent(channelID, sessionID, map[string]any{
					"event_type":    "chat.processing_status",
					"session_id":    sessionID,
					"rid":           roundID,
					"is_processing": false,
					"is_complete":   true,
					"member_count":  parsed["member_count"],
					"task_count":    parsed["task_count"],
				})
				continue
			}

			broadcastEvent(channelID, sessionID, parsed)
		}
	}

	// Python 步骤 6: 流结束处理
	// S-05: 取消路径不走 team.error——取消是正常终止（对齐 Python CancelledError 语义），
	// receivedChunks==0 的 team.error 只在非取消的空流场景发出。
	if streamCancelled {
		return
	}
	if receivedChunks == 0 {
		logger.Warn(logComponent).
			Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
			Msg("consumeStreamWithQuery: stream ended with no output")
		broadcastEvent(channelID, sessionID, map[string]any{
			"event_type": "team.error",
			"error":      "Team stream ended with no output (possible pool/DB inconsistency or internal error)",
			"session_id": sessionID,
		})
	} else {
		logger.Info(logComponent).
			Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).Int("chunks", receivedChunks).
			Msg("consumeStreamWithQuery: stream ended")
	}
}

// watchTeamEvolutionAndPushTeam 后台监控 TeamSkillEvolutionRail 并推送状态。
// 对齐 Python: _watch_team_evolution_and_push(channel_id, session_id, rail) (line 1101-1393)
//
// Python 核心逻辑：
//  1. 创建 EvolutionPushContext（WebSocketGatewayPushTransport）
//  2. 主循环：drain_pending_approval_events → broadcast_evolution_progress
//  3. 分组审批 → push_evolution_event + push_evolution_status
//  4. 处理 outcome 事件 → team_evolution_end_update
//  5. 超时检测 + auto_scan 关闭检测
//  6. 异常处理 → push status end
func (d *DeepAdapter) watchTeamEvolutionAndPushTeam(ctx context.Context, channelID, sessionID string, rail *coreevolution.TeamSkillEvolutionRail) {
	// Python: push_context = EvolutionPushContext(transport=WebSocketGatewayPushTransport(), channel_id=, session_id=)
	// Go 差异：因 adapter→evolution→gateway_push→server→adapter 循环依赖，
	// 无法直接导入 adapter/evolution 包。此处将 push 逻辑内联到当前包。
	// T-01 遗留点 A（已回填）：通过 d.getSendPushFunc() 真实推送到前端，
	// 与个人 watcher（deep_adapter_evolution.go）共用同一套 sendPushFunc 注入机制。

	seenRequestIDs := make(map[string]bool)
	closedRequestIDs := make(map[string]bool)
	fallbackCycleIndex := 0
	var activeCycleRequestID string

	// 内部清理函数
	cleanupEvolutionRail := func() {
		_ = rail.CleanupBackgroundTasks()
	}

	// 内部推送 evolution status 函数（内联对齐 Python: push_evolution_status）
	// Python 团队路径 include_payload_request_id=True（默认），payload 含 request_id
	pushEvolutionStatus := func(requestID, status, stage string, message string) {
		sendPush := d.getSendPushFunc()
		if sendPush == nil {
			logger.Warn(logComponent).Str("session_id", sessionID).
				Msg("watchTeamEvolutionAndPushTeam: sendPushFunc not injected")
			return
		}
		update := evolutionlogic.BuildEvolutionStatusUpdate(requestID, status, stage, message)
		payload := map[string]any{
			"event_type": "chat.evolution_status",
			"status":     update.Status,
			"stage":      update.Stage,
			"message":    update.Message,
			"request_id": update.RequestID,
		}
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
		if err := sendPush(ctx, msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Str("request_id", requestID).
				Str("status", status).Str("stage", stage).
				Msg("watchTeamEvolutionAndPushTeam: failed to push evolution status")
		}
	}

	// 内部推送 cycle start 函数
	pushCycleStart := func(requestID string, progressStatuses []evolutionlogic.EvolutionProgressStatus) bool {
		if _, ok := closedRequestIDs[requestID]; ok {
			return false
		}
		requestProgress := evolutionlogic.ProgressForRequest(progressStatuses, requestID)
		stage := evolutionlogic.TeamEvolutionStartStage
		message := evolutionlogic.TeamEvolutionStartMessage
		if len(requestProgress) > 0 {
			stage = requestProgress[0].Stage
			message = requestProgress[0].Message
		}
		pushEvolutionStatus(requestID, "start", stage, message)
		return true
	}

	// 内部推送 evolution event 函数（内联对齐 Python: push_evolution_event）
	pushEvolutionEvent := func(requestID string, evt map[string]any) error {
		sendPush := d.getSendPushFunc()
		if sendPush == nil {
			logger.Warn(logComponent).Str("session_id", sessionID).
				Msg("watchTeamEvolutionAndPushTeam: sendPushFunc not injected")
			return fmt.Errorf("sendPushFunc not injected")
		}
		payload := evolutionlogic.EventPayloadDict(evt)
		evtType := evolutionlogic.EventType(evt)
		if evtType != "" {
			if _, ok := payload["event_type"]; !ok {
				payload["event_type"] = evtType
			}
		}
		if _, ok := payload["request_id"]; !ok {
			payload["request_id"] = requestID
		}
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
		if err := sendPush(ctx, msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Str("request_id", requestID).Str("event_type", evtType).
				Msg("watchTeamEvolutionAndPushTeam: failed to push evolution event")
			return err
		}
		return nil
	}

	lastEventAt := time.Now()
	// M-35: fallback 用 logic 常量 900.0（原本地错误常量 60.0 已删）
	eventTimeoutSec := resolveEvolutionEventTimeoutSec(rail, evolutionlogic.TeamEvolutionEventTimeoutSec)

	// S-43: 对齐 Python watch_team_evolution_and_push_team 的 except Exception 块——
	// panic 时推送真实失败消息（含 active request_id），不调 cleanupEvolutionRail
	// （Python except 不含 cleanup，仅 auto_scan 关闭分支调用）。
	// goroutine 层 onTeamWatcherDone 仍正常执行（函数 return 后外层代码继续跑）。
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("session_id", sessionID).
				Any("panic", r).
				Msg("watchTeamEvolutionAndPushTeam: evolution monitor failed")
			if activeCycleRequestID != "" {
				pushEvolutionStatus(activeCycleRequestID, "end", evolutionlogic.TeamEvolutionHiddenStage,
					fmt.Sprintf("Team skill evolution analysis failed: %v", r))
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Python: if not getattr(rail, "auto_scan", True)
		if !rail.AutoScan() {
			if activeCycleRequestID != "" {
				pushEvolutionStatus(activeCycleRequestID, "end", evolutionlogic.TeamEvolutionHiddenStage, "")
			}
			cleanupEvolutionRail()
			return
		}

		// Python: events = await rail.drain_pending_approval_events(wait=False) or []
		// S-23: wait 必须为 false——true 时 timeout=nil 回退 GetEvolutionTotalTimeoutSecs()，
		// watcher 循环可阻塞最长 900s 且期间 ctx.Done 完全无法响应（M-35 只修了常量，本笔误未修）。
		events := rail.DrainPendingApprovalEvents(false, nil)
		eventsMap := evolutionOutputSchemasToMaps(events)

		if len(eventsMap) == 0 {
			if activeCycleRequestID != "" {
				idleFor := time.Since(lastEventAt).Seconds()
				if idleFor >= eventTimeoutSec {
					logger.Warn(logComponent).
						Str("session_id", sessionID).Str("request_id", activeCycleRequestID).
						Float64("idle_for", idleFor).
						Msg("watchTeamEvolutionAndPushTeam: evolution monitor timed out")
					pushEvolutionStatus(activeCycleRequestID, "end", evolutionlogic.TeamEvolutionHiddenStage,
						fmt.Sprintf("Team skill evolution analysis timed out after %.0fs without host events", eventTimeoutSec))
					cleanupEvolutionRail()
					return
				}
			}
			// Python: await asyncio.sleep(TEAM_EVOLUTION_IDLE_SLEEP_SEC)
			// M-35: 改用 logic 常量 1.0s（原本地 500ms 且注释错误声称 Python=0.5，实际 Python=1.0）
			time.Sleep(time.Duration(evolutionlogic.TeamEvolutionIdleSleepSec * float64(time.Second)))
			continue
		}
		lastEventAt = time.Now()

		// Python: await broadcast_evolution_progress(channel_id, session_id, events, ...)
		// Go 差异：因循环依赖无法导入 adapter/evolution 包，内联 broadcast_evolution_progress 逻辑
		for _, evt := range eventsMap {
			if evolutionlogic.IsEvolutionApprovalEvent(evt) || evolutionlogic.IsEvolutionOutcomeEvent(evt) || evolutionlogic.TeamEvolutionTerminalProgress(evt) != nil {
				continue
			}
			parsed := utilsParseStreamChunk(evt)
			if parsed != nil {
				broadcastEvent(channelID, sessionID, parsed)
			}
		}

		// Python: grouped_approvals, _ = _group_team_evolution_approvals(session_id, events)
		groupedApprovals, _ := groupTeamEvolutionApprovals(sessionID, eventsMap)

		// Python: outcomes = [evolution_outcome_from_event(evt) for evt in events if is_evolution_outcome_event(evt)]
		var outcomes []map[string]string
		for _, evt := range eventsMap {
			if evolutionlogic.IsEvolutionOutcomeEvent(evt) {
				outcomes = append(outcomes, evolutionlogic.EvolutionOutcomeFromEvent(evt))
			}
		}

		// Python: terminal_progress = terminal_progress_from_events(events)
		terminalProgress := evolutionlogic.TerminalProgressFromEvents(eventsMap)

		// Python: visible_progress_statuses = visible_evolution_progress_from_events(events)
		visibleProgressStatuses := evolutionlogic.VisibleEvolutionProgressFromEvents(eventsMap)

		justStarted := false

		// 解析 active_cycle_request_id
		if activeCycleRequestID == "" {
			firstRequestID := ""
			// Python: 从 grouped_approvals 取第一个 key
			for rid := range groupedApprovals {
				firstRequestID = rid
				break
			}
			// Python: 从 visible_progress_statuses 取
			if firstRequestID == "" {
				for _, ps := range visibleProgressStatuses {
					if ps.RequestID != nil && *ps.RequestID != "" {
						firstRequestID = *ps.RequestID
						break
					}
				}
			}
			// Python: 从 events 中提取（非 progress）
			if firstRequestID == "" {
				for _, evt := range eventsMap {
					if evolutionlogic.EvolutionProgressStatusFromEvent(evt) != nil {
						continue
					}
					if extracted := evolutionlogic.ExtractEvolutionRequestID(evt); extracted != nil && *extracted != "" {
						firstRequestID = *extracted
						break
					}
				}
			}
			// Python: 从 terminal_progress 中提取
			if firstRequestID == "" {
				for _, item := range terminalProgress {
					if item.RequestID != nil && *item.RequestID != "" {
						stage := evolutionlogic.TerminalStage(item.Terminal)
						if stage != "" {
							if _, hidden := evolutionlogic.TeamEvolutionHiddenTerminalStages[stage]; !hidden {
								firstRequestID = *item.RequestID
								break
							}
						}
					}
				}
			}
			// Python: fallback cycle request id
			if firstRequestID == "" {
				hasNoRequestID := false
				for _, ps := range visibleProgressStatuses {
					if ps.RequestID == nil {
						hasNoRequestID = true
						break
					}
				}
				if hasNoRequestID {
					fallbackCycleIndex++
					firstRequestID = evolutionlogic.MakeTeamEvolutionCycleRequestID(sessionID, fallbackCycleIndex)
				} else {
					hasNoRequestIDTerminal := false
					for _, item := range terminalProgress {
						stage := evolutionlogic.TerminalStage(item.Terminal)
						if item.RequestID == nil {
							if _, hidden := evolutionlogic.TeamEvolutionHiddenTerminalStages[stage]; !hidden {
								hasNoRequestIDTerminal = true
								break
							}
						}
					}
					if hasNoRequestIDTerminal {
						fallbackCycleIndex++
						firstRequestID = evolutionlogic.MakeTeamEvolutionCycleRequestID(sessionID, fallbackCycleIndex)
					} else {
						continue
					}
				}
			}

			if pushCycleStart(firstRequestID, visibleProgressStatuses) {
				activeCycleRequestID = firstRequestID
				justStarted = true
			}
		}

		if activeCycleRequestID == "" {
			continue
		}

		// 推送 active cycle 的 progress status
		activeProgressStatuses := evolutionlogic.ProgressForRequest(visibleProgressStatuses, activeCycleRequestID)
		progressStatusesToPush := activeProgressStatuses
		if justStarted && len(activeProgressStatuses) > 1 {
			progressStatusesToPush = activeProgressStatuses[1:]
		}
		for _, ps := range progressStatusesToPush {
			if ps.Terminal {
				continue
			}
			pushEvolutionStatus(activeCycleRequestID, "progress", ps.Stage, ps.Message)
		}

		// 处理审批事件
		for requestID, approvalEvents := range groupedApprovals {
			if _, ok := closedRequestIDs[requestID]; ok {
				continue
			}
			if activeCycleRequestID != requestID {
				if !pushCycleStart(requestID, visibleProgressStatuses) {
					continue
				}
				activeCycleRequestID = requestID
			}
			if _, ok := seenRequestIDs[requestID]; ok {
				logger.Debug(logComponent).
					Str("session_id", sessionID).Str("request_id", requestID).
					Msg("watchTeamEvolutionAndPushTeam: skip duplicated team evolution approval batch")
				continue
			}
			seenRequestIDs[requestID] = true

			for _, evt := range approvalEvents {
				if err := pushEvolutionEvent(requestID, evt); err != nil {
					logger.Warn(logComponent).
						Str("request_id", requestID).
						Str("event_type", evolutionlogic.EventType(evt)).
						Err(err).
						Msg("watchTeamEvolutionAndPushTeam: push approval failed")
				}
			}

			pushEvolutionStatus(requestID, "end", "approval_required",
				"Team skill evolution proposal is awaiting approval")
			closedRequestIDs[requestID] = true
			activeCycleRequestID = ""
		}

		// 处理 outcome / terminal progress
		var terminal map[string]string
		if len(outcomes) > 0 {
			outcome := outcomes[len(outcomes)-1]
			status := "completed"
			if s, ok := outcome["status"]; ok && s != "" {
				status = s
			}
			message := ""
			if m, ok := outcome["message"]; ok {
				message = m
			}
			terminal = map[string]string{
				"status":  status,
				"stage":   status,
				"message": message,
			}
		} else if len(terminalProgress) > 0 {
			for _, item := range terminalProgress {
				if activeCycleRequestID == "" {
					continue
				}
				if item.RequestID != nil && *item.RequestID != activeCycleRequestID {
					continue
				}
				terminal = item.Terminal
			}
		}

		if terminal != nil && activeCycleRequestID != "" {
			endUpdate := evolutionlogic.TeamEvolutionEndUpdate(activeCycleRequestID, terminal)
			pushEvolutionStatus(endUpdate.RequestID, endUpdate.Status, endUpdate.Stage, endUpdate.Message)
			closedRequestIDs[activeCycleRequestID] = true
			activeCycleRequestID = ""
		}
	}
}

// chunkToMap 将 stream.Schema 转为 map[string]any。
// 对齐 Python: parse_stream_chunk(chunk) — 从 SDK 输出结构中提取前端可消费的 dict
func chunkToMap(chunk any) map[string]any {
	if chunk == nil {
		return nil
	}
	// 优先处理 map[string]any
	if m, ok := chunk.(map[string]any); ok {
		return m
	}
	// 处理 *stream.OutputSchema / stream.OutputSchema（公开字段，无 getter 方法）
	// T-01 遗留点 C：原实现只断言带 GetType/GetPayload 方法的接口，OutputSchema 实际是公开字段，
	// 三段断言全部失败返回 nil，导致 watcher 和 consumeStream 循环丢失全部事件。
	// S-20（方案A）：产出 typed 嵌套形状——payload 键保留嵌套（utilsParseStreamChunk
	// 的 typed 分支从 "payload" 键读，对齐 Python getattr(chunk, "payload")）+
	// payload 内容键提升顶层（evolutionlogic 判定函数直接读顶层键，经 EventPayloadDict
	// 解包后语义不变）+ type/index 顶层（对齐 getattr(chunk, "type")）。
	switch os := chunk.(type) {
	case *stream.OutputSchema:
		if os == nil || os.Payload == nil {
			return nil
		}
		if m := payloadToMap(os.Payload); m != nil {
			result := make(map[string]any, len(m)+3)
			for k, v := range m {
				result[k] = v
			}
			result["type"] = os.Type
			result["index"] = os.Index
			result["payload"] = m
			return result
		}
		return nil
	case stream.OutputSchema:
		if os.Payload == nil {
			return nil
		}
		if m := payloadToMap(os.Payload); m != nil {
			result := make(map[string]any, len(m)+3)
			for k, v := range m {
				result[k] = v
			}
			result["type"] = os.Type
			result["index"] = os.Index
			result["payload"] = m
			return result
		}
		return nil
	}
	// S-20: *TeamOutputSchema 显式分支——Go 类型断言不匹配嵌入类型
	// （case *stream.OutputSchema 不会命中 *TeamOutputSchema——团队流实际 chunk：
	// readyChunk 与 tagChunk 升级后的 leader/teammate chunk 都是它）。
	// Python parse_stream_chunk 用 hasattr 鸭子类型天然命中继承的 type/payload
	// 属性 + getattr 读 role/source_member；Go 以显式分支等价。
	// 形状（方案A 半展平，对齐 Python typed chunk 的语义）：payload 键保留嵌套 +
	// type/index/source_member/role 顶层——utilsParseStreamChunk 的 typed 分支
	// 从 "payload" 键读取（对齐 Python getattr(chunk, "payload")），
	// isLeader/isTeammate/enrich 从顶层 type/role/source_member 读取
	// （对齐 Python getattr(chunk, "type"/"role"/"source_member")）。
	// 注意不能用 ToMap() 接口兜底——原生 OutputSchema.ToMap 是 model_dump 嵌套语义
	// {type,index,payload}（TeamOutputSchema.ToMap 额外含 source_member/role 但同为嵌套），
	// 与此处需要的"属性访问等价物"语义相同但接口断言会劫持 native 分支；
	// 显式类型分支互斥更清晰。
	if tos, ok := chunk.(*atschema.TeamOutputSchema); ok {
		if tos == nil || tos.Payload == nil {
			return nil
		}
		if m := payloadToMap(tos.Payload); m != nil {
			result := make(map[string]any, len(m)+5)
			for k, v := range m {
				result[k] = v
			}
			result["type"] = tos.Type
			result["index"] = tos.Index
			result["payload"] = m
			if tos.SourceMember != nil {
				result["source_member"] = *tos.SourceMember
			}
			if tos.Role != nil {
				result["role"] = string(*tos.Role)
			}
			return result
		}
		return nil
	}
	// 处理带 getter 方法的输出结构
	type outputSchemaLike interface {
		GetType() string
		GetPayload() any
	}
	if os, ok := chunk.(outputSchemaLike); ok {
		payload := os.GetPayload()
		if payload == nil {
			return nil
		}
		if m := payloadToMap(payload); m != nil {
			// 附加 type 和其他元信息到 map
			result := make(map[string]any, len(m)+1)
			for k, v := range m {
				result[k] = v
			}
			result["type"] = os.GetType()
			return result
		}
	}
	return nil
}

// payloadToMap 将 OutputSchema.Payload 转为 map[string]any。
//
// S-03: 生产端（task_loop/executor.go、session_spawn_executor.go、
// task_scheduler.go）产出 *cschema.ControllerOutputPayload 结构体指针而非 map——
// 原三个 typed 分支只断言 map[string]any，结构体 payload 全部返回 nil，
// 导致 task_failed/task_completion 事件在消费循环被整块丢弃（前端静默失败）。
//
// 桥接实现已上提到 utils.PayloadToMap（deep 主循环与 ParseStreamChunk 共用），
// 此处保留函数名作为包内薄封装，避免改动既有调用点。
func payloadToMap(payload any) map[string]any {
	return utils.PayloadToMap(payload)
}

// teamSpecTeamName 从 teamSpec 中提取 team_name。
func teamSpecTeamName(teamSpec any) string {
	if teamSpec == nil {
		return ""
	}
	// T-19: 直接断言 *atschema.TeamAgentSpec——GetEnrichedTeamSpec 的实际返回类型。
	// 该结构体只有 TeamName 公开字段（无 GetTeamName()/TeamName() 方法），
	// 原先两个接口分支均无实现者（死代码），导致 runtime_ready 事件缺 team_name 时
	// 此兜底恒返回空串。Python 直接访问 team_spec.team_name 属性，无此间接层。
	if spec, ok := teamSpec.(*atschema.TeamAgentSpec); ok {
		return spec.TeamName
	}
	// 兼容断言：保留接口分支以防其他实现者（防御性，与 Python getattr 宽容度对应）
	type teamNameHolder interface {
		GetTeamName() string
	}
	if tn, ok := teamSpec.(teamNameHolder); ok {
		return tn.GetTeamName()
	}
	// 直接通过类型断言
	type specWithTeamName interface {
		TeamName() string
	}
	if s, ok := teamSpec.(specWithTeamName); ok {
		return s.TeamName()
	}
	// map[string]any 回退
	if m, ok := teamSpec.(map[string]any); ok {
		if name, _ := m["team_name"].(string); name != "" {
			return name
		}
	}
	return ""
}

// evolutionOutputSchemasToMaps 将 []*stream.OutputSchema 转为 []map[string]any。
// S-20 关联：产出 typed 嵌套形状（{type, index, payload, ...payload 键}——payload 键保留嵌套，
// 其余 payload 内容键同时提升顶层），对齐 Python watch 循环把原始 OutputSchema 对象
// 同时传给 is_evolution_approval_event（读 .type 属性+.payload）与 parse_stream_chunk
// （hasattr type/payload 走 typed 分支）的双语义：
//   - evolutionlogic 判定函数经 EventPayloadDict 解包 "payload" 键读内容键
//   - EventType 读顶层 "type" 键（对齐 .type 属性）
//   - utilsParseStreamChunk 走 typed 分支（llm_reasoning → chat.reasoning 等）
func evolutionOutputSchemasToMaps(events any) []map[string]any {
	if events == nil {
		return nil
	}
	// rail.DrainPendingApprovalEvents 返回 []*stream.OutputSchema
	if schemas, ok := events.([]*stream.OutputSchema); ok {
		result := make([]map[string]any, 0, len(schemas))
		for _, s := range schemas {
			if s == nil {
				continue
			}
			if m := chunkToMap(s); m != nil {
				result = append(result, m)
			}
		}
		return result
	}
	if slice, ok := events.([]any); ok {
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if m := chunkToMap(item); m != nil {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

// utilsParseStreamChunk 解析流式 chunk。
// 对齐 Python: parse_stream_chunk(chunk, _has_streamed_content=False)（server/utils/stream_utils.py L10-44）
// —— utils 层版本，与 interface_deep._parse_stream_chunk（Go: utils.ParseStreamChunk）是
// 两个不同的 Python 函数：utils 层无 harness.* 分支（message 走兜底 team. 直传），
// interface_deep 层有 harness.message/stage_result 等专属分支。团队消费链 import 的是 utils 层版本。
//
// Go 差异：Python 接收 SDK chunk 对象（hasattr 鸭子类型）；Go 端 chunk 已被 chunkToMap
// 转为 map[string]any，语义映射为 Python 的两个入口分支：
//   - map 含 "payload" 键（chunkToMap 对 OutputSchema/TeamOutputSchema 的产出）→ _parse_typed_chunk
//   - map 无 "payload" 键（event_type/type/content/output 直接顶层）→ _parse_dict_chunk
//
// S-20 完整落地：原实现是 identity 桩（T-01 标记），导致 parsed 无顶层 event_type，
// 消费循环的 runtime_ready 分支与 chat.delta 派生全部断裂，S-04 接入点成死代码。
func utilsParseStreamChunk(chunk map[string]any) map[string]any {
	if chunk == nil {
		return nil
	}
	// Python: if hasattr(chunk, "type") and hasattr(chunk, "payload"): return _parse_typed_chunk(...)
	// Go 等价：chunkToMap 的 typed 产出带 "payload" 键
	if _, hasPayload := chunk["payload"]; hasPayload {
		return parseTypedChunkMap(chunk)
	}
	// Python: elif isinstance(chunk, dict): return _parse_dict_chunk(chunk, ...)
	return parseDictChunkMap(chunk)
	// Python 其余分支（hasattr type 无 payload / event_type 属性 / 默认 chat.delta）
	// 在 chunkToMap 产出域内不会出现，不单独复刻。
}

// parseDictChunkMap 解析 dict 形状的 chunk。
// 对齐 Python: _parse_dict_chunk(chunk, _has_streamed_content)（stream_utils.py L47-94）
func parseDictChunkMap(chunk map[string]any) map[string]any {
	// Python: if "event_type" in chunk: return _serialize_chunk_recursive(chunk)
	if _, hasEvt := chunk["event_type"]; hasEvt {
		if m, ok := serializeDictChunkRecursive(chunk).(map[string]any); ok {
			return m
		}
		return chunk
	}
	// Python: if "type" in chunk: → event_type = type，剔除 type 键后透传
	if _, hasType := chunk["type"]; hasType {
		eventType, _ := chunk["type"].(string)
		result := map[string]any{"event_type": eventType}
		for k, v := range chunk {
			if k == "type" {
				continue
			}
			result[k] = serializeValueAny(v)
		}
		return result
	}
	// Python: if "content" in chunk: 空白内容返回 None
	if content, hasContent := chunk["content"]; hasContent {
		if s, ok := content.(string); ok && s != "" && strings.TrimSpace(s) != "" {
			return map[string]any{"event_type": "chat.delta", "content": s}
		}
		return nil
	}
	// Python: if "output" in chunk: result_type=="error" → chat.error；空白 → None；否则 chat.delta
	if output, hasOutput := chunk["output"]; hasOutput {
		if rt, _ := chunk["result_type"].(string); rt == "error" {
			errMsg, _ := output.(string)
			return map[string]any{"event_type": "chat.error", "error": errMsg}
		}
		if s, ok := output.(string); ok && s != "" && strings.TrimSpace(s) != "" {
			return map[string]any{"event_type": "chat.delta", "content": s}
		}
		return nil
	}
	// Python: return chunk（兜底原样返回）
	return chunk
}

// parseTypedChunkMap 解析 typed 形状的 chunk（chunkToMap 产出：{type, payload, ...}）。
// 对齐 Python: _parse_typed_chunk(chunk, _has_streamed_content)（stream_utils.py L107-341）
func parseTypedChunkMap(chunk map[string]any) map[string]any {
	chunkType, _ := chunk["type"].(string)
	payload, _ := chunk["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}

	// Python: if isinstance(chunk_type, str) and "." in chunk_type:
	if strings.Contains(chunkType, ".") {
		// Python: chat.{chunk_type} 前缀 + payload 键透传（serialize）
		if len(payload) > 0 {
			result := map[string]any{"event_type": chunkType}
			for k, v := range payload {
				result[k] = serializeValueAny(v)
			}
			return result
		}
		// Python: return {"event_type": chunk_type, "content": str(payload)}
		return map[string]any{"event_type": chunkType, "content": fmt.Sprintf("%v", payload)}
	}

	switch chunkType {
	case "controller_output":
		if len(payload) > 0 {
			// Python: interactions = _find_interaction_payloads(payload)
			interactions := utils.FindInteractionPayloads(payload)
			if len(interactions) > 0 {
				return parseInteractionPayloadUtils(interactions)
			}
			// Python: inner type 判断——task_completion → None；task_failed → chat.error；
			// 其余 inner type 落到后续分支（Python 无 return 即继续匹配）
			innerType, _ := payload["type"].(string)
			switch innerType {
			case "task_completion":
				return nil
			case "task_failed":
				errorMsg := "任务执行失败"
				if data, ok := payload["data"].([]any); ok {
					for _, item := range data {
						if m, ok := item.(map[string]any); ok {
							if text, ok := m["text"].(string); ok && text != "" {
								errorMsg = text
								break
							}
						}
					}
				}
				return map[string]any{"event_type": "chat.error", "error": errorMsg}
			}
		}
		// Python: payload is None 时不进入 controller_output 分支，落到兜底

	case "llm_output":
		content := strFromPayloadKey(payload, "content")
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		return map[string]any{"event_type": "chat.delta", "content": content}

	case "llm_reasoning":
		// Python: content or output，空白 → None
		content := strFromPayloadKey(payload, "content")
		if content == "" {
			content = strFromPayloadKey(payload, "output")
		}
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		return map[string]any{"event_type": "chat.reasoning", "content": content}

	case "content_chunk":
		content := strFromPayloadKey(payload, "content")
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		return map[string]any{"event_type": "chat.delta", "content": content}

	case "answer":
		// Python: result_type=="error" → chat.error（output 兜底 "未知错误"）
		if rt, _ := payload["result_type"].(string); rt == "error" {
			errMsg, _ := payload["output"].(string)
			if errMsg == "" {
				errMsg = "未知错误"
			}
			return map[string]any{"event_type": "chat.error", "error": errMsg}
		}
		var content string
		var isChunked bool
		if output, ok := payload["output"].(map[string]any); ok {
			content, _ = output["output"].(string)
			isChunked, _ = output["chunked"].(bool)
		} else if s, ok := payload["output"].(string); ok {
			content = s
		}
		if content == "" || strings.TrimSpace(content) == "" {
			return nil
		}
		// Python: _has_streamed_content and not is_chunked → chat.final；
		// is_chunked → chat.delta；否则 chat.final。
		// _has_streamed_content 团队消费循环恒 False（Python 团队路径不传该参数），按 False 处理。
		if isChunked {
			return map[string]any{"event_type": "chat.delta", "content": content}
		}
		return map[string]any{"event_type": "chat.final", "content": content}

	case "tool_call":
		toolInfo := any(payload)
		if sub, ok := payload["tool_call"].(map[string]any); ok {
			toolInfo = sub
		}
		return map[string]any{"event_type": "chat.tool_call", "tool_call": toolInfo}

	case "tool_update":
		// Python: update_info = payload.get("tool_update", payload)
		var updatePayload map[string]any
		if sub, ok := payload["tool_update"].(map[string]any); ok {
			updatePayload = sub
		} else if s, ok := payload["tool_update"].(string); ok {
			// Python: dict(update_info) if isinstance(update_info, dict) else {"content": str(update_info)}
			updatePayload = map[string]any{"content": s}
		} else {
			// 缺 tool_update 键 → 用整个 payload
			updatePayload = payload
		}
		result := map[string]any{"event_type": "chat.tool_update"}
		for k, v := range updatePayload {
			result[k] = v
		}
		return result

	case "tool_result":
		resultInfo := any(payload)
		if sub, ok := payload["tool_result"].(map[string]any); ok {
			resultInfo = sub
		}
		resultPayload := map[string]any{}
		if info, ok := resultInfo.(map[string]any); ok {
			if res, ok := info["result"]; ok {
				resultPayload["result"] = res
			} else {
				resultPayload["result"] = fmt.Sprintf("%v", info)
			}
			if tn, ok := info["tool_name"].(string); ok && tn != "" {
				resultPayload["tool_name"] = tn
			} else if tn, ok := info["name"].(string); ok && tn != "" {
				resultPayload["tool_name"] = tn
			}
			if tcid, ok := info["tool_call_id"].(string); ok && tcid != "" {
				resultPayload["tool_call_id"] = tcid
			} else if tcid, ok := info["toolCallId"].(string); ok && tcid != "" {
				resultPayload["tool_call_id"] = tcid
			}
			rawOutput := info["raw_output"]
			if rawOutput == nil {
				rawOutput = info["rawOutput"]
			}
			if rawOutput != nil {
				resultPayload["raw_output"] = rawOutput
			}
			for _, key := range []string{"success", "status", "is_error", "summary"} {
				if v, ok := info[key]; ok {
					resultPayload[key] = v
				}
			}
		} else {
			resultPayload["result"] = fmt.Sprintf("%v", resultInfo)
		}
		result := map[string]any{"event_type": "chat.tool_result"}
		for k, v := range resultPayload {
			result[k] = v
		}
		return result

	case "error":
		errorMsg := fmt.Sprintf("%v", payload)
		if m, ok := payload["error"]; ok {
			errorMsg = fmt.Sprintf("%v", m)
		}
		return map[string]any{"event_type": "chat.error", "error": errorMsg}

	case "thinking":
		return map[string]any{
			"event_type":    "chat.processing_status",
			"is_processing": true,
			"current_task":  "thinking",
		}

	case "todo.updated":
		var todos any = []any{}
		if td, ok := payload["todos"].([]any); ok {
			todos = td
		}
		return map[string]any{"event_type": "todo.updated", "todos": todos}

	case "context.usage":
		rate := 0.0
		if r, ok := payload["rate"].(float64); ok {
			rate = r
		}
		contextMax := 0.0
		if cm, ok := payload["context_max"].(float64); ok && cm != 0 {
			contextMax = cm
		}
		tokensUsed := 0.0
		if tu, ok := payload["tokens_used"].(float64); ok && tu != 0 {
			tokensUsed = tu
		}
		return map[string]any{
			"event_type":  "context.usage",
			"rate":        rate,
			"context_max": contextMax,
			"tokens_used": tokensUsed,
		}

	case "chat.ask_user_question":
		result := map[string]any{"event_type": "chat.ask_user_question"}
		for k, v := range payload {
			result[k] = v
		}
		return result

	case "__interaction__":
		return parseInteractionPayloadUtils([]any{any(payload)})
	}

	// Python 兜底: isinstance(payload, dict) → team.* 直传 / chat.{type} 前缀 + 键透传
	if len(payload) > 0 {
		if innerEventType, ok := payload["event_type"].(string); ok {
			// Python: team.* 命名空间事件直传（不包 chat. 前缀）
			if strings.HasPrefix(innerEventType, "team.") {
				result := map[string]any{}
				for k, v := range payload {
					result[k] = serializeValueAny(v)
				}
				return result
			}
		}
		result := map[string]any{"event_type": "chat." + chunkType}
		for k, v := range payload {
			result[k] = serializeValueAny(v)
		}
		return result
	}
	// Python: return {"event_type": f"chat.{chunk_type}", "content": str(payload)}
	return map[string]any{"event_type": "chat." + chunkType, "content": fmt.Sprintf("%v", payload)}
}

// parseInteractionPayloadUtils 对齐 Python utils 层 _parse_interaction_payload(interactions)
// （stream_utils.py L356-373）——与 interface_deep 版本（Go: utils.ParseInteractionPayload，
// 接收单 payload map）不同，utils 层版本接收 interactions 列表。
// activate_confirm → harness.activate_interaction 事件；否则委托
// interrupt.ConvertInteractionsToAskUserQuestion（Python: convert_interactions_to_ask_user_question）。
func parseInteractionPayloadUtils(interactions []any) map[string]any {
	for _, interaction := range interactions {
		m, ok := interaction.(map[string]any)
		if !ok {
			continue
		}
		if m["interaction_type"] == "activate_confirm" {
			extRuntimePath := teamHelperStrFromAny(m["extension_runtime_path"])
			if extRuntimePath == "" {
				extRuntimePath = teamHelperStrFromAny(m["runtime_path"])
			}
			options := []string{"accept", "reject"}
			if opts, ok := m["options"].([]any); ok && len(opts) > 0 {
				options = make([]string, 0, len(opts))
				for _, o := range opts {
					if s, ok := o.(string); ok {
						options = append(options, s)
					}
				}
			}
			return map[string]any{
				"event_type":             "harness.activate_interaction",
				"interaction_type":       "activate_confirm",
				"interaction_id":         teamHelperStrFromAny(m["interaction_id"]),
				"extension_name":         teamHelperStrFromAny(m["extension_name"]),
				"runtime_path":           teamHelperStrFromAny(m["runtime_path"]),
				"session_runtime_path":   teamHelperStrFromAny(m["session_runtime_path"]),
				"extension_runtime_path": extRuntimePath,
				"options":                options,
			}
		}
	}
	// Python: return convert_interactions_to_ask_user_question([payload])
	return interrupt.ConvertInteractionsToAskUserQuestion(interactions)
}

// strFromPayloadKey 从 payload 中安全提取字符串值。
func strFromPayloadKey(payload map[string]any, key string) string {
	if s, ok := payload[key].(string); ok {
		return s
	}
	return ""
}

// serializeValueAny 对齐 Python _serialize_value：datetime → RFC3339 字符串，其余原样。
func serializeValueAny(v any) any {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format(time.RFC3339Nano)
	}
	return v
}

// serializeDictChunkRecursive 对齐 Python _serialize_chunk_recursive：dict/list 递归 + 标量 _serialize_value。
func serializeDictChunkRecursive(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v2 := range val {
			result[k] = serializeDictChunkRecursive(v2)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v2 := range val {
			result[i] = serializeDictChunkRecursive(v2)
		}
		return result
	default:
		return serializeValueAny(val)
	}
}

// resolveEvolutionEventTimeoutSec 从 TeamSkillEvolutionRail 解析事件超时秒数。
// 对齐 Python: resolve_evolution_event_timeout_sec(rail, fallback_sec, grace_sec)
// （evolution_helpers.py L114-138）——rail 的 SDK 后台演进超时 + 宽限，
// 非法值（Inf/NaN/<=0）回退 fallback。比兄弟实现 deep_adapter_evolution.go
// 的 isfinite 检查更完整（同时挡 NaN 和 <=0）。
func resolveEvolutionEventTimeoutSec(rail *coreevolution.TeamSkillEvolutionRail, fallback float64) float64 {
	if rail == nil {
		return fallback
	}
	sdkTimeout := rail.GetEvolutionTotalTimeoutSecs()
	// 对齐 Python: if not math.isfinite(parsed_timeout) or parsed_timeout <= 0: return fallback
	if math.IsInf(sdkTimeout, 0) || math.IsNaN(sdkTimeout) || sdkTimeout <= 0 {
		return fallback
	}
	// 对齐 Python: return parsed_timeout + max(grace, 0.0)
	return sdkTimeout + math.Max(evolutionlogic.TeamEvolutionEventTimeoutGraceSec, 0.0)
}
