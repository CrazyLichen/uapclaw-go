package adapter

import (
	"context"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// findTeamSkillRail 查找 TeamSkillEvolutionRail。
// Python: _find_team_skill_rail() (line 3651-3670)
func (d *DeepAdapter) findTeamSkillRail() *evolution.TeamSkillEvolutionRail {
	if d.teamSkillEvolutionRail != nil {
		return d.teamSkillEvolutionRail
	}
	// 在 instance 的已注册 rails 中查找
	if d.instance != nil {
		found := d.instance.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillEvolutionRail{}))
		if len(found) > 0 {
			if r, ok := found[0].(*evolution.TeamSkillEvolutionRail); ok {
				d.teamSkillEvolutionRail = r
				return r
			}
		}
	}
	return nil
}

// findSkillCreateRail 查找 TeamSkillCreateRail。
// 对齐 Python: _find_skill_create_rail()
func (d *DeepAdapter) findSkillCreateRail() *evolution.TeamSkillCreateRail {
	if d.skillCreateRail != nil {
		return d.skillCreateRail
	}
	// 在 instance 的已注册 rails 中查找
	if d.instance != nil {
		found := d.instance.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillCreateRail{}))
		if len(found) > 0 {
			if r, ok := found[0].(*evolution.TeamSkillCreateRail); ok {
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

	// 步骤 3: 判断是否首次请求
	// 对齐 Python: if not team_manager.has_stream_task(session_id)
	isFirstRequest := !teamManager.HasStreamTask(sessionID)

	go func() {
		defer close(ch)

		// 步骤 4: 提取 query directives（对齐 Python: _extract_query_directives）
		// Python: if is_first_request: query, hide_dm, debug = _extract_query_directives(str(query or ""))
		queryText := paramsString(inputs, "query", "")
		if isFirstRequest {
			cleanedQuery, hideDM, debug := extractQueryDirectives(queryText)
			if hideDM || debug {
				logger.Info(logComponent).
					Str("session_id", sessionID).
					Str("channel_id", channelID).
					Bool("hide_dm", hideDM).
					Bool("debug", debug).
					Msg("processTeamMessageStream: query directives captured for first team request")
			}
			// 对齐 Python: query 变量替换为 cleanedQuery
			queryText = cleanedQuery
			_ = hideDM
			_ = debug
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
			// 首次请求：创建 TeamAgent + 启动后台流任务
			logger.Info(logComponent).Str("session_id", sessionID).Str("channel_id", channelID).
				Msg("processTeamMessageStream: first request")

			// 对齐 Python 步骤：
			//  1. teamSpec = teamManager.GetEnrichedTeamSpec(session_id, deep_agent, ...)
			//  2. teamManager.PrepareRuntimeActivation(ctx, sessionID, teamSpec.TeamName)
			//  3. 创建 TeamAgent + 启动 streaming goroutine
			//  4. teamManager.CommitRuntimeReady(sessionID, teamSpec.TeamName)
			//  5. teamManager.OnRuntimeReady(ctx, sessionID, teamAgent, hideDM)
			//  5. 注册 stream task cancel
			// ⤵️(#9.85): TeamRunner 完整创建 TeamAgent + streaming 流程
			return
		}

		// 后续请求：通过 TeamManager.Interact 路由
		// 对齐 Python: team_manager.interact(session_id, user_input)
		query := paramsString(inputs, "query", "")
		ok, err := teamManager.Interact(ctx, sessionID, interaction.NewInteractInput(query))
		if err != nil {
			logger.Error(logComponent).Err(err).Str("session_id", sessionID).
				Msg("processTeamMessageStream: Interact failed")
			return
		}
		if !ok {
			logger.Warn(logComponent).Str("session_id", sessionID).
				Msg("processTeamMessageStream: Interact returned false (inactive session)")
		}
	}()

	return ch, nil
}

// ensureMonitorForActiveRuntime 在 runtime 就绪后挂载 TeamMonitorHandler。
// 对齐 Python: ensure_monitor_for_active_runtime(channel_id, session_id, team_name, hide_dm) (line 133-178)
// ⤵️(#9.85): 依赖 Runner.get_agent_team_monitor
func (d *DeepAdapter) ensureMonitorForActiveRuntime(ctx context.Context, channelID, sessionID, teamName string, hideDM bool) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", teamName).
		Msg("ensureMonitorForActiveRuntime: ⤵️(#9.85) pending Runner.get_agent_team_monitor")
}

// ensureTeamEvolutionWatcher 启动 team evolution 监控。
// 对齐 Python: ensure_team_evolution_watcher(channel_id, session_id, *, source) (line 331-379)
// 检查逻辑可落地，启动 goroutine 处 ⤵️(#9.85)
func (d *DeepAdapter) ensureTeamEvolutionWatcher(ctx context.Context, channelID, sessionID, source string) {
	tm := team.GetTeamManager(channelID)

	// Python: watcher = tm.get_team_evolution_watcher(session_id)
	// Python: if watcher is not None and not watcher.done()
	cancelFn := tm.GetTeamEvolutionWatcher(sessionID)
	if cancelFn != nil {
		logger.Info(logComponent).
			Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: evolution monitor already running")
		return
	}

	// Python: rail = tm.get_team_skill_rail(session_id)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		logger.Warn(logComponent).
			Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: no TeamSkillEvolutionRail found, deferred")
		return
	}

	// Python: if not getattr(rail, "auto_scan", True)
	if !rail.AutoScan() {
		logger.Info(logComponent).
			Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: auto_scan disabled, skipped")
		return
	}

	// ⤵️(#9.85): 启动 watcher goroutine
	// Python: task = asyncio.create_task(_watch_team_evolution_and_push(channel_id, session_id, rail))
	logger.Info(logComponent).
		Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
		Msg("ensureTeamEvolutionWatcher: ⤵️(#9.85) pending watchTeamEvolutionAndPush goroutine")
}

// consumeStreamWithQuery 后台消费 team 流并广播事件。
// 对齐 Python: _consume_stream_with_query(channel_id, session_id, team_spec, initial_query, *, round_id, envs) (line 889-1048)
// ⤵️(#9.85): 依赖 Runner.run_agent_team_streaming
func (d *DeepAdapter) consumeStreamWithQuery(ctx context.Context, channelID, sessionID string, teamSpec any, initialQuery string) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("consumeStreamWithQuery: ⤵️(#9.85) pending Runner.run_agent_team_streaming")
}

// consumeMonitorEvents 后台消费 monitor 事件并广播。
// 对齐 Python: _consume_monitor_events(channel_id, session_id, monitor_handler) (line 1050-1083)
// ⤵️(#10.6): 依赖 TeamMonitorHandler 事件流
func (d *DeepAdapter) consumeMonitorEvents(ctx context.Context, channelID, sessionID string) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("consumeMonitorEvents: ⤵️(#10.6) pending TeamMonitorHandler events")
}

// watchTeamEvolutionAndPushTeam 后台监控 TeamSkillEvolutionRail 并推送状态。
// 对齐 Python: _watch_team_evolution_and_push(channel_id, session_id, rail) (line 1101-1393)
// ⤵️(#9.85): 依赖 Runner 流式接口
func (d *DeepAdapter) watchTeamEvolutionAndPushTeam(ctx context.Context, channelID, sessionID string, rail *evolution.TeamSkillEvolutionRail) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("watchTeamEvolutionAndPushTeam: ⤵️(#9.85) pending evolution watcher implementation")
}
