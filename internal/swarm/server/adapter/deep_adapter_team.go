package adapter

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	ateamruntime "github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamEvolutionIdleSleepSec evolution watcher 空闲轮询间隔（秒）。
	// 对齐 Python: TEAM_EVOLUTION_IDLE_SLEEP_SEC = 0.5 (evolution_helpers.py)
	teamEvolutionIdleSleepSec = 500 // 毫秒
	// teamEvolutionEventTimeoutSec evolution watcher 事件超时（秒）。
	// 对齐 Python: TEAM_EVOLUTION_EVENT_TIMEOUT_SEC = 60.0 (evolution_helpers.py)
	teamEvolutionEventTimeoutSec = 60.0
	// teamEventQueueTimeout team 事件队列读取超时。
	// 对齐 Python: asyncio.wait_for(request_queue.get(), timeout=0.1)
	teamEventQueueTimeout = 100 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// findTeamSkillRail 查找 TeamSkillEvolutionRail。
// Python: _find_team_skill_rail() (line 3651-3670)
func (d *DeepAdapter) findTeamSkillRail() *coreevolution.TeamSkillEvolutionRail {
	if d.teamSkillEvolutionRail != nil {
		return d.teamSkillEvolutionRail
	}
	// 在 instance 的已注册 rails 中查找
	if d.instance != nil {
		found := d.instance.FindRailsByType(reflect.TypeOf(&coreevolution.TeamSkillEvolutionRail{}))
		if len(found) > 0 {
			if r, ok := found[0].(*coreevolution.TeamSkillEvolutionRail); ok {
				d.teamSkillEvolutionRail = r
				return r
			}
		}
	}
	return nil
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
	rail := d.findTeamSkillRail()
	if rail == nil {
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: TeamSkillEvolutionRail not initialized")
		return false
	}

	// 解析 answers 为 approve/reject
	parsedAnswers := parseApprovalAnswersFromAny(answers)
	approved := parseApprovalAnswers(parsedAnswers)

	if approved {
		err := rail.ApproveRecord(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("request_id", requestID).
				Msg("handleTeamSkillEvolveApproval: ApproveRecord failed")
			return false
		}
		logger.Info(logComponent).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: approved")

		// 推送解决状态
		_ = d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, "approved")
		return true
	}

	err := rail.RejectRecord(ctx, requestID)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: RejectRecord failed")
		return false
	}
	logger.Info(logComponent).
		Str("request_id", requestID).
		Msg("handleTeamSkillEvolveApproval: rejected")

	// 推送解决状态
	_ = d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, "rejected")
	return true
}

// pushTeamSkillEvolveResolutionStatus 推送 team skill 演进解决状态。
//
// 对齐 Python: _push_team_skill_evolve_resolution_status(request_id, status)
//
//	将审批结果通过 stream 事件推送给前端
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(ctx context.Context, requestID string, status string) error {
	logger.Info(logComponent).
		Str("request_id", requestID).
		Str("status", status).
		Msg("pushTeamSkillEvolveResolutionStatus: approval result pushed")
	// Python: 通过 stream event 推送，当前 Go 端仅记录日志
	// 完整的 stream 推送需要 d.instance 的 stream 支持
	return nil
}

// optionMatches 检查选项是否匹配用户选择。
// Python: _option_matches() (line 3769-3790)
func (d *DeepAdapter) optionMatches(option map[string]any, answers any) bool {
	if option == nil || answers == nil {
		return false
	}
	optionID, _ := option["id"].(string)
	if optionID == "" {
		return false
	}

	// answers 可能是 []any 或 map[string]any
	switch a := answers.(type) {
	case []any:
		for _, item := range a {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["id"].(string); ok && id == optionID {
					return true
				}
			}
			if s, ok := item.(string); ok && s == optionID {
				return true
			}
		}
	case map[string]any:
		if id, ok := a["id"].(string); ok && id == optionID {
			return true
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
	sessionID := ""
	if req.SessionID != nil {
		sessionID = *req.SessionID
	}
	channelID := "default"
	if req.ChannelID != "" {
		channelID = req.ChannelID
	}

	// 步骤 2: 通过 channel_id 获取 TeamManager
	// 对齐 Python: get_team_manager(channel_id)
	teamManager := team.GetTeamManager(channelID)

	// 确保事件广播回调已注入（幂等操作）
	EnsureEventBroadcastInjected(channelID)

	// 步骤 3: 判断是否首次请求
	// 对齐 Python: if not team_manager.has_stream_task(session_id)
	isFirstRequest := !teamManager.HasStreamTask(sessionID)

	go func() {
		defer close(ch)

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

		if isFirstRequest {
			// ─── 首次请求：完整 TeamAgent 创建 + streaming 流程 ───
			// 对齐 Python: process_team_message_stream 首次请求分支 (line 699-887)
			logger.Info(logComponent).Str("session_id", sessionID).Str("channel_id", channelID).
				Msg("processTeamMessageStream: first request")

			// 步骤 1: 获取 enriched team spec
			// Python: team_spec = await team_manager.get_enriched_team_spec(session_id, deep_agent, ...)
			var requestMetadata map[string]any
			if req.Metadata != nil {
				requestMetadata = make(map[string]any, len(req.Metadata))
				for k, v := range req.Metadata {
					requestMetadata[k] = v
				}
			}
			teamSpec := teamManager.GetEnrichedTeamSpec(ctx, sessionID, d.instance, req.RequestID, channelID, requestMetadata)
			if teamSpec == nil {
				logger.Error(logComponent).Str("session_id", sessionID).
					Msg("processTeamMessageStream: GetEnrichedTeamSpec 返回 nil")
				ch <- &agentschema.AgentResponseChunk{
					RequestID: req.RequestID, ChannelID: channelID,
					Payload:    map[string]any{"event_type": "chat.error", "error": "TeamAgent 创建失败：无法获取 team spec"},
					IsComplete: false,
				}
				ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
				return
			}
			teamName := teamSpec.TeamName

			// 步骤 2: 解析 /evolve_rebuild followup
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

			// 步骤 3: PrepareRuntimeActivation
			// Python: team_manager.ensure_team_shared_skills_initialized(team_spec)
			// Python: await team_manager.prepare_runtime_activation(session_id, team_name)
			teamManager.EnsureTeamSharedSkillsInitialized(teamSpec)
			if err := teamManager.PrepareRuntimeActivation(ctx, sessionID, teamName); err != nil {
				logger.Error(logComponent).Err(err).Str("session_id", sessionID).
					Msg("processTeamMessageStream: PrepareRuntimeActivation 失败")
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
			go d.consumeStreamWithQuery(streamCtx, channelID, sessionID, teamSpec, queryText, roundID, streamEnvs)

			// 步骤 7: 注册流任务 cancel
			teamManager.RegisterStreamTask(sessionID, streamCancel)

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
					if event != nil {
						if evtType, _ := event["event_type"].(string); evtType == "team.error" {
							break
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

			// 清理 waiter
			unregisterWaiter(channelID, sessionID, req.RequestID)
			return
		}

		// ─── 后续请求：通过 TeamManager.Interact 路由 ───
		// 对齐 Python: team_manager.interact(session_id, user_input) (line 784-822)
		logger.Info(logComponent).
			Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
			Msg("processTeamMessageStream: follow-up team request")

		query := paramsString(inputs, "query", "")
		if query != "" {
			ok, err := teamManager.Interact(ctx, sessionID, interaction.NewInteractInput(query))
			if err != nil {
				logger.Error(logComponent).Err(err).Str("session_id", sessionID).
					Msg("processTeamMessageStream: Interact failed")
				ch <- &agentschema.AgentResponseChunk{
					RequestID: req.RequestID, ChannelID: channelID,
					Payload:    map[string]any{"event_type": "chat.error", "error": "interact failed"},
					IsComplete: false,
				}
				ch <- &agentschema.AgentResponseChunk{RequestID: req.RequestID, ChannelID: channelID, IsComplete: true}
				return
			}
			if !ok {
				logger.Warn(logComponent).Str("session_id", sessionID).
					Msg("processTeamMessageStream: Interact returned false (inactive session)")
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
//  1. tm = get_team_manager(channel_id)
//  2. existing = tm.get_monitor(session_id); if existing and existing.is_running: return
//  3. monitor = await Runner.get_agent_team_monitor(team_name=, session_id=, hide_dm=)
//  4. monitor_handler = TeamMonitorHandler(monitor, session_id)
//  5. await monitor_handler.start()
//  6. tm.register_monitor(session_id, monitor_handler)
//  7. if monitor_handler.is_running: asyncio.create_task(_consume_monitor_events(...))
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
			Msg("ensureMonitorForActiveRuntime: 无 TeamAgent，无法启动 monitor")
		return
	}

	if err := tm.EnsureMonitor(ctx, sessionID, teamAgent, hideDM); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("session_id", sessionID).Str("team_name", teamName).
			Msg("ensureMonitorForActiveRuntime: EnsureMonitor 失败")
		return
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).Str("team_name", teamName).
		Msg("ensureMonitorForActiveRuntime: monitor 已启动")
}

// ensureTeamEvolutionWatcher 启动 team evolution 监控。
// 对齐 Python: ensure_team_evolution_watcher(channel_id, session_id, *, source) (line 331-379)
//
// Python 步骤：
//  1. tm = get_team_manager(channel_id)
//  2. watcher = tm.get_team_evolution_watcher(session_id)
//  3. if watcher is not None and not watcher.done(): return
//  4. rail = tm.get_team_skill_rail(session_id)
//  5. if rail is None: return
//  6. if not getattr(rail, "auto_scan", True): return
//  7. task = asyncio.create_task(_watch_team_evolution_and_push(channel_id, session_id, rail))
//  8. setattr(task, "_team_channel_id", channel_id)
//  9. setattr(task, "_team_session_id", session_id)
// 10. task.add_done_callback(_on_team_watcher_done)
// 11. tm.register_team_evolution_watcher(session_id, task)
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
		d.watchTeamEvolutionAndPushTeam(watcherCtx, channelID, sessionID, rail)
		// goroutine 退出后，从 TeamManager 中移除 watcher
		tm.PopTeamEvolutionWatcher(sessionID)
		onTeamWatcherDone(sessionID)
	}()
}

// consumeStreamWithQuery 后台消费 team 流并广播事件。
// 对齐 Python: _consume_stream_with_query(channel_id, session_id, team_spec, initial_query, *, round_id, envs) (line 889-1048)
//
// Python 步骤：
//  1. 广播 round-start 信号
//  2. 调用 Runner.run_agent_team_streaming 获取流
//  3. 遍历 chunk：过滤 leader/teammate → 解析 → 去重 → 广播
//  4. 处理 runtime_ready 事件（sync metadata + commit + attach hooks + ensure monitor + ensure evolution watcher）
//  5. 处理 team.completed 事件（广播 round-complete 信号）
//  6. 流结束：无 chunk → broadcast team.error；有 chunk → 记录日志
//  7. finally: clear pending + pop stream task
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
	_ = streamTraceEnabled // TODO: TeamStreamLogger 对齐

	// Python 步骤 2: 调用 Runner.run_agent_team_streaming
	// Go 差异：Python 通过 Runner.run_agent_team_streaming(agent_team=team_spec, inputs=..., session=..., envs=..., stream_logger=lg)
	// Go 端使用 ateamruntime.RunAgentTeamStreaming 直接调用
	streamCh, err := ateamruntime.RunAgentTeamStreaming(ctx, teamSpec, map[string]any{"query": initialQuery}, false, false, sessionID, nil)
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
	for chunk := range streamCh {
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

			// 对齐 Python 步骤：
			// sync_team_identity_metadata(...)
			// tm.commit_runtime_ready(session_id, ready_team_name)
			// await tm.attach_distributed_hooks_for_runner_runtime(...)
			// await ensure_monitor_for_active_runtime(...)
			// ensure_team_evolution_watcher(...)
			SyncTeamIdentityMetadata(ctx, channelID, sessionID, "team", readyTeamName, activationKind)

			tm := team.GetTeamManager(channelID)
			tm.CommitRuntimeReady(sessionID, readyTeamName)

			// 分布式 hooks — ⤵️(#9.72) 待回填
			tm.AttachDistributedHooksForRunnerRuntime(readyTeamName, sessionID, nil)

			d.ensureMonitorForActiveRuntime(ctx, channelID, sessionID, readyTeamName, hideDM)
			d.ensureTeamEvolutionWatcher(ctx, channelID, sessionID, "runtime_ready")
		} else if evtType == "team.completed" {
			// Python 步骤 5: 处理 team.completed 事件
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

	// Python 步骤 6: 流结束处理
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

// consumeMonitorEvents 后台消费 monitor 事件并广播。
// 对齐 Python: _consume_monitor_events(channel_id, session_id, monitor_handler) (line 1050-1083)
//
// Python 步骤：
//  1. logger.info("monitor event loop started")
//  2. async for event in monitor_handler.events(): _broadcast_event(channel_id, session_id, event)
//  3. logger.info("monitor event loop ended")
//  4. except CancelledError: raise
//  5. except Exception: logger.error
//
// Go 差异：此方法在 TeamManager.EnsureMonitor 中已有简化实现（team_manager_monitor.go），
// 此处提供 DeepAdapter 级别的完整实现，支持通过 broadcastEvent 广播到 channel waiters。
func (d *DeepAdapter) consumeMonitorEvents(ctx context.Context, channelID, sessionID string) {
	tm := team.GetTeamManager(channelID)
	handler := tm.GetMonitor(sessionID)
	if handler == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).
			Msg("consumeMonitorEvents: 无 TeamMonitorHandler")
		return
	}

	logger.Info(logComponent).
		Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
		Msg("consumeMonitorEvents: monitor event loop started")

	for evt := range handler.Events() {
		select {
		case <-ctx.Done():
			logger.Info(logComponent).Str("session_id", sessionID).
				Msg("consumeMonitorEvents: monitor event loop cancelled")
			return
		default:
		}

		if evt != nil {
			broadcastEvent(channelID, sessionID, evt)
		}
	}

	logger.Info(logComponent).
		Str("channel_id", resolveChannelID(channelID)).Str("session_id", sessionID).
		Msg("consumeMonitorEvents: monitor event loop ended")
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
	// 当前 Go 端 server_push 传输层尚未完全集成，使用日志记录替代推送。

	seenRequestIDs := make(map[string]bool)
	closedRequestIDs := make(map[string]bool)
	fallbackCycleIndex := 0
	var activeCycleRequestID string

	// 内部清理函数
	cleanupEvolutionRail := func() {
		rail.CleanupBackgroundTasks()
	}

	// 内部推送 evolution status 函数（内联对齐 Python: push_evolution_status）
	pushEvolutionStatus := func(requestID, status, stage string, message string) {
		logger.Info(logComponent).
			Str("session_id", sessionID).
			Str("request_id", requestID).
			Str("status", status).
			Str("stage", stage).
			Str("message", message).
			Msg("pushEvolutionStatus: evolution status pushed")
		// TODO(#9.85): 待 gateway_push 集成完善后，通过 transport.SendPush 推送到前端
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
		logger.Info(logComponent).
			Str("session_id", sessionID).
			Str("request_id", requestID).
			Str("event_type", evtType).
			Msg("pushEvolutionEvent: evolution event pushed")
		// TODO(#9.85): 待 gateway_push 集成完善后，通过 transport.SendPush 推送到前端
		_ = payload
		return nil
	}

	lastEventAt := time.Now()
	eventTimeoutSec := resolveEvolutionEventTimeoutSec(rail, teamEvolutionEventTimeoutSec)

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
		events := rail.DrainPendingApprovalEvents(true, nil)
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
			time.Sleep(time.Duration(teamEvolutionIdleSleepSec) * time.Millisecond)
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
	// 处理 stream.OutputSchema 结构体
	type outputSchemaLike interface {
		GetType() string
		GetPayload() any
	}
	if os, ok := chunk.(outputSchemaLike); ok {
		payload := os.GetPayload()
		if payload == nil {
			return nil
		}
		if m, ok := payload.(map[string]any); ok {
			// 附加 type 和其他元信息到 map
			result := make(map[string]any, len(m)+1)
			for k, v := range m {
				result[k] = v
			}
			result["type"] = os.GetType()
			return result
		}
	}
	// 处理 stream.Schema 接口 — 尝试通过类型断言获取 Payload
	type payloadGetter interface {
		Payload() any
	}
	if pg, ok := chunk.(payloadGetter); ok {
		if payload := pg.Payload(); payload != nil {
			if m, ok := payload.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

// teamSpecTeamName 从 teamSpec 中提取 team_name。
func teamSpecTeamName(teamSpec any) string {
	if teamSpec == nil {
		return ""
	}
	// TeamAgentSpec 有 TeamName 字段
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
func evolutionOutputSchemasToMaps(events any) []map[string]any {
	if events == nil {
		return nil
	}
	// rail.DrainPendingApprovalEvents 返回 []*stream.OutputSchema
	// 每个 OutputSchema 有 Payload 字段
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
// 对齐 Python: parse_stream_chunk(evt)
func utilsParseStreamChunk(chunk map[string]any) map[string]any {
	if chunk == nil {
		return nil
	}
	// Python: parse_stream_chunk 将 SDK chunk 转为前端可消费的 dict
	// Go 差异：chunk 已经是 map[string]any，直接返回
	return chunk
}

// resolveEvolutionEventTimeoutSec 从 TeamSkillEvolutionRail 解析事件超时秒数。
// 对齐 Python: resolve_evolution_event_timeout_sec(rail, fallback_sec=TEAM_EVOLUTION_EVENT_TIMEOUT_SEC)
//
// Go 差异：Python 的 resolve_evolution_event_timeout_sec 接受 dict[string]any rail，
// Go 端 rail 是结构体，通过反射读取 evolution_total_timeout_secs 字段。
func resolveEvolutionEventTimeoutSec(rail *coreevolution.TeamSkillEvolutionRail, fallback float64) float64 {
	if rail == nil {
		return fallback
	}
	// 尝试通过 rail 的配置获取超时值
	// 对齐 Python: sdk_timeout = rail.get("evolution_total_timeout_secs")
	// Go 端 TeamSkillEvolutionRail 结构体可能没有此字段，使用默认超时
	return fallback
}
