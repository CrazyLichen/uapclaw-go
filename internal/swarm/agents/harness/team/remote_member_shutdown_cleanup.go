package team

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// 本文件对齐 Python remote_member_bootstrap.py 的 shutdown cleanup 切片（S-04）：
// 分布式 team 全员 shutdown 后延迟删除 session runtime，并在新请求进入
// process_team_message_stream 时等待 pending 清理完成。
//
// 完整对应 Python: jiuwenswarm/agents/harness/team/remote_member_bootstrap.py L102-107
// （_is_distributed_leader_runtime）、L1236-1313（全员关闭判定/调度/延迟清理/等待）、
// L1316-1331（wait_for）、L1334-1361（notice 推送）、L1364-1386（run_pending）、
// L1394-1474（attach wrapper）。
//
// Go 差异：Python 用 asyncio.Task + monkey-patch tool.invoke；Go 用
// goroutine + done channel + ResourceMgr 注册表替换（AbilityManager.executeTool
// 每次调用从注册表动态解析工具实例，注册表替换即生效，与 monkey-patch 等价）。

// ──────────────────────────── 结构体 ────────────────────────────

// shutdownCleanupEntry 会话级 shutdown 清理任务条目。
// 对齐 Python: _SHUTDOWN_CLEANUP_TASKS[sid] 中的 asyncio.Task（done()/结果读取）。
type shutdownCleanupEntry struct {
	// done 任务完成信号（goroutine close 后 result 可安全读取）
	done chan struct{}
	// result 清理结果（deleted 与否），done 关闭后只读
	result bool
}

// shutdownCleanupTool 包装 leader 的 shutdown_member 工具：
// 原始 invoke 成功且全员已 shutdown_requested/shutdown 时调度团队清理。
// 对齐 Python: attach_shutdown_member_remote_cleanup_wrapper 内的 wrapped_invoke。
type shutdownCleanupTool struct {
	// Tool 被包装的原始工具（通常已是 loggedTeamTool 包装层级，与 Python 包装链一致）
	tool.Tool
	// mu 保护下方三个可 rebind 字段的读写（Python 单线程 setattr 无锁，Go 需要锁）
	mu sync.RWMutex
	// sessionID 绑定的 session 标识（rebind 更新）
	sessionID string
	// channelID 绑定的 channel 标识（rebind 更新，空串等价 Python None）
	channelID string
	// teamAgent 绑定的 TeamAgent（rebind 更新，全员关闭判定用）
	teamAgent *agent.TeamAgent
	// constructSessionID 构造时捕获的 session 标识快照（不可变）。
	// 对齐 Python: wrapped_invoke 闭包变量 session_id（L1455-1457 fallback 源）。
	// Python rebind 路径（setattr 三属性 + return）不重建闭包，fallback 值
	// 恒为最初构造时的 session_id；Go 用显式快照字段等价实现该闭包语义。
	constructSessionID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// shutdownCleanupDelay 调度清理前的延迟（秒）。
	// 对齐 Python: await asyncio.sleep(2.0)（_delayed_shutdown_cleanup L1303）
	shutdownCleanupDelay = 2.0
	// shutdownCleanupWaitTimeout 等待 pending 清理完成的超时（秒）。
	// 对齐 Python: wait_for_pending_shutdown_cleanup_for_session(timeout=15.0)
	shutdownCleanupWaitTimeout = 15.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// shutdownCleanupTasks sessionID → pending 清理任务注册表。
	// 对齐 Python: _SHUTDOWN_CLEANUP_TASKS: dict[str, asyncio.Task]
	shutdownCleanupTasks = make(map[string]*shutdownCleanupEntry)
	// shutdownCleanupMu 保护 shutdownCleanupTasks 的互斥锁
	// （Python asyncio 单线程无锁，Go map 并发读写是 fatal error）
	shutdownCleanupMu sync.Mutex
	// shutdownCleanupNoticePusher 清理结果通知推送回调，由 adapter 层在包 init 时注入
	// （对齐 Python 直接构造 WebSocketGatewayPushTransport——Go 因循环依赖
	// team → adapter 改为注入回调，同 RegisterTeamCallbackDefaults 模式）。
	shutdownCleanupNoticePusher func(ctx context.Context, msg map[string]any) error
	// shutdownCleanupNoticeMu 保护 shutdownCleanupNoticePusher 的读写锁。
	// 注入方（adapter 层启动/测试 cleanup）与消费方（延迟清理后台 goroutine）
	// 可能并发读写该回调，无锁访问构成数据竞争。
	shutdownCleanupNoticeMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetShutdownCleanupNoticePusher 设置清理结果通知推送回调（由 adapter 层注入）。
// 对齐 Python: _push_shutdown_cleanup_notice 中的 WebSocketGatewayPushTransport().send_push
func SetShutdownCleanupNoticePusher(fn func(ctx context.Context, msg map[string]any) error) {
	shutdownCleanupNoticeMu.Lock()
	shutdownCleanupNoticePusher = fn
	shutdownCleanupNoticeMu.Unlock()
}

// WaitForPendingShutdownCleanupForSession 等待指定 session 的 pending shutdown 清理完成。
// 对齐 Python: wait_for_pending_shutdown_cleanup_for_session(session_id, timeout=15.0)
//
// 返回 false 的三种情况：无 pending 任务（Python task None）、超时（warn）、
// 内部 panic（等价消费点 except → warn）。shield 语义天然满足：等待方超时返回
// 不影响后台 goroutine 继续执行。
func WaitForPendingShutdownCleanupForSession(ctx context.Context, sessionID string) bool {
	// 对齐 Python 消费点（team_helpers.py L624-635）的 except → warning：
	// 内部任何 panic 只记日志返回 false，不中断请求处理
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).
				Str("session_id", sessionID).
				Msg("[TeamHelpers] waiting for pending shutdown cleanup failed")
		}
	}()

	sid := strings.TrimSpace(sessionID)
	shutdownCleanupMu.Lock()
	entry := shutdownCleanupTasks[sid]
	shutdownCleanupMu.Unlock()
	if entry == nil {
		return false
	}

	timer := time.NewTimer(time.Duration(shutdownCleanupWaitTimeout * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-entry.done:
		return entry.result
	case <-timer.C:
		logger.Warn(logComponent).Str("session_id", sid).
			Msg("[RemoteMemberBootstrap] timed out waiting for shutdown cleanup")
		return false
	case <-ctx.Done():
		// Python await 被 cancel 时 CancelledError 传播终止请求；
		// Go 上层 ctx 取消时提前返回（防御性，正常流不会触发）
		return false
	}
}

// AttachShutdownMemberRemoteCleanupWrapper 为 leader 的 shutdown_member 工具附加
// 远程清理 wrapper：工具成功执行且全员已请求/完成 shutdown 时调度团队清理。
// 对齐 Python: attach_shutdown_member_remote_cleanup_wrapper (L1394-1474)
func AttachShutdownMemberRemoteCleanupWrapper(teamAgent *agent.TeamAgent, sessionID string, channelID *string) {
	// 对齐 Python 调用点（team_manager.py L1543-1551）外层 try/except → warning：
	// attach 过程任何 panic 只记日志，不影响其余 hooks
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).
				Str("session_id", sessionID).
				Msg("[TeamManager] distributed hooks attach failed for Runner-owned TeamAgent")
		}
	}()

	// Python: config_base = _get_config()
	configBase, err := teamLoadConfig()
	if err != nil {
		configBase = map[string]any{}
	}
	if !isDistributedLeaderRuntime(configBase) {
		logger.Debug(logComponent).
			Msg("[RemoteMemberBootstrap] non-distributed leader runtime; skip shutdown cleanup wrapper")
		return
	}

	// Python: if getattr(team_agent, "role", None) != TeamRole.LEADER: return
	if teamAgent == nil || teamAgent.Role() != atschema.TeamRoleLeader {
		return
	}

	// Python: leader = _team_agent_deep_agent(team_agent)
	// Go 路径：TeamAgent.deepAgent 字段无公开访问器，等价走 harness.inner_agent
	// （TeamHarness.InnerAgent 返回内层 DeepAgent，Python 第二条 getattr 路径）
	var leader hinterfaces.DeepAgentInterface
	if h := teamAgent.Harness(); h != nil {
		leader = h.InnerAgent()
	}
	if leader == nil {
		logger.Debug(logComponent).
			Msg("[RemoteMemberBootstrap] skip shutdown cleanup wrapper: missing leader DeepAgent")
		return
	}

	toolID := resolveTeamToolID(leader, "shutdown_member")

	// Python: tag = getattr(getattr(leader, "card", None), "id", None)
	tag := ""
	if card := leader.Card(); card != nil {
		tag = card.ID
	}

	rm := runner.GetResourceMgr()
	if rm == nil {
		logger.Debug(logComponent).Str("tool_id", toolID).
			Str("session_id", sessionID).
			Msg("[RemoteMemberBootstrap] Runner.resource_mgr unavailable; skip shutdown cleanup wrapper")
		return
	}

	// Python: tool = Runner.resource_mgr.get_tool(tool_id, tag=tag) if tag else None
	// Python: if tool is None: tool = Runner.resource_mgr.get_tool(tool_id)
	var found tool.Tool
	foundWithTag := false
	if tag != "" {
		tools, getErr := rm.GetTool([]string{toolID}, resourcesmanager.WithTag(resourcesmanager.Tag(tag)))
		if getErr == nil && len(tools) > 0 {
			found = tools[0]
			foundWithTag = true
		}
	}
	if found == nil {
		tools, getErr := rm.GetTool([]string{toolID})
		if getErr == nil && len(tools) > 0 {
			found = tools[0]
		}
	}
	if found == nil {
		logger.Debug(logComponent).Str("tool_id", toolID).
			Str("session_id", sessionID).
			Msg("[RemoteMemberBootstrap] tool not in Runner.resource_mgr; skip shutdown cleanup wrapper")
		return
	}

	chID := ""
	if channelID != nil {
		chID = *channelID
	}

	// Python: if getattr(tool, _SHUTDOWN_CLEANUP_WRAPPED_ATTR, False): 更新 attrs 后 return
	// Go 等价：类型断言识别已包装实例，rebind 上下文字段
	if existing, ok := found.(*shutdownCleanupTool); ok {
		existing.rebind(sessionID, chID, teamAgent)
		logger.Info(logComponent).Str("tool_id", toolID).
			Str("session_id", sessionID).Str("channel", chID).
			Msg("[RemoteMemberBootstrap] rebound shutdown cleanup wrapper")
		return
	}

	// Go 等价 monkey-patch：注册表替换（AbilityManager.executeTool 每次调用
	// 从注册表动态解析工具实例，替换注册条目即生效）。
	// foundWithTag 时保留原 tag 语义（Python 不动注册表，tag 天然保留）。
	wrapped := &shutdownCleanupTool{
		Tool:      found,
		sessionID: sessionID,
		channelID: chID,
		teamAgent: teamAgent,
		// Python: 闭包捕获构造时 session_id（rebind 不重建闭包），
		// Go 快照等价——fallbackSessionID 的取值源
		constructSessionID: sessionID,
	}
	addOpts := []resourcesmanager.ResourceOption{resourcesmanager.WithRefresh()}
	if foundWithTag && tag != "" {
		addOpts = append(addOpts, resourcesmanager.WithTag(resourcesmanager.Tag(tag)))
	}
	if addErr := rm.AddTool(wrapped, addOpts...); addErr != nil {
		logger.Warn(logComponent).Str("tool_id", toolID).
			Str("session_id", sessionID).Err(addErr).
			Msg("[RemoteMemberBootstrap] failed to re-register wrapped shutdown_member tool")
		return
	}
	logger.Info(logComponent).Str("tool_id", toolID).
		Str("session_id", sessionID).Str("channel", chID).
		Msg("[RemoteMemberBootstrap] attached shutdown cleanup wrapper")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Invoke 包装原始 Invoke：成功且全员已请求/完成 shutdown 时调度团队清理。
// 对齐 Python: wrapped_invoke (L1432-1466)
func (w *shutdownCleanupTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// Python: result = await orig_invoke(inputs, **kwargs)（异常直接传播，不进 try）
	result, err := w.Tool.Invoke(ctx, inputs, opts...)
	if err != nil {
		return result, err
	}

	// 对齐 Python try/except Exception → warning：hook 失败不影响工具结果
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("[RemoteMemberBootstrap] shutdown cleanup hook failed")
			}
		}()
		// Python: if not bool(getattr(result, "success", False)): return result
		if result == nil {
			return
		}
		if success, _ := result["success"].(bool); !success {
			return
		}

		// Python: active_team_agent = getattr(self, _WRAPPED_TEAM_AGENT_ATTR, team_agent)
		w.mu.RLock()
		activeTeamAgent := w.teamAgent
		activeSessionID := w.sessionID
		activeChannelID := w.channelID
		w.mu.RUnlock()

		// Python: if not await _all_teammates_shutdown_requested_or_done(...): return result
		allDone, listErr := allTeammatesShutdownRequestedOrDone(ctx, activeTeamAgent)
		if listErr != nil {
			// 对齐 Python：list_members 异常传播到外层 except → warning
			logger.Warn(logComponent).Err(listErr).
				Msg("[RemoteMemberBootstrap] shutdown cleanup hook failed")
			return
		}
		if !allDone {
			return
		}

		// Python: active_session_id = str(getattr(self, ...ATTR, session_id) or session_id).strip() or session_id
		activeSessionID = strings.TrimSpace(activeSessionID)
		if activeSessionID == "" {
			activeSessionID = w.fallbackSessionID()
		}

		logger.Info(logComponent).
			Str("session_id", activeSessionID).
			Str("channel", activeChannelID).
			Msg("[RemoteMemberBootstrap] all teammates are shutdown-requested; scheduling team cleanup")
		scheduleShutdownCleanup(activeSessionID, activeChannelID)
	}()

	return result, nil
}

// rebind 更新绑定的 session/channel/TeamAgent（对齐 Python setattr 三个 ATTR）。
func (w *shutdownCleanupTool) rebind(sessionID, channelID string, teamAgent *agent.TeamAgent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sessionID = sessionID
	w.channelID = channelID
	w.teamAgent = teamAgent
}

// fallbackSessionID 返回构造时捕获的 sessionID 快照。
// 对齐 Python: wrapped_invoke 闭包变量 session_id——rebind 只 setattr 实例属性，
// 闭包 fallback 源不变，故本函数读 constructSessionID 而非可 rebind 的 sessionID。
func (w *shutdownCleanupTool) fallbackSessionID() string {
	// constructSessionID 构造后不可变，无需加锁
	return w.constructSessionID
}

// isDistributedLeaderRuntime 判定是否为分布式 leader 运行时。
// 对齐 Python: _is_distributed_leader_runtime (L102-107)
//
// 注意与 IsDistributedMode 的区别：本函数只认 runtime.mode=="distributed"
// （不含 transport.type=="pyzmq" 的 OR 分支），且要求 role=="leader"。
func isDistributedLeaderRuntime(configBase map[string]any) bool {
	teamCfg, _ := configBase["team"].(map[string]any)
	if teamCfg == nil {
		teamCfg = map[string]any{}
	}
	runtimeCfg, _ := teamCfg["runtime"].(map[string]any)
	if runtimeCfg == nil {
		runtimeCfg = map[string]any{}
	}
	mode := strings.ToLower(strings.TrimSpace(anyToStr(runtimeCfg["mode"])))
	role := strings.ToLower(strings.TrimSpace(anyToStr(runtimeCfg["role"])))
	return mode == "distributed" && role == "leader"
}

// resolveTeamToolID 按公开工具名解析注册的团队工具 ID。
// 对齐 Python: _team_tool_id (L139-152)
//
// 匹配规则：cid == "team.{tool_name}" 或前缀 "team.{tool_name}." 或 name == tool_name。
func resolveTeamToolID(leader hinterfaces.DeepAgentInterface, toolName string) string {
	expected := "team." + toolName
	if leader != nil {
		if am := leader.AbilityManager(); am != nil {
			for _, ability := range am.List() {
				if ability == nil {
					continue
				}
				cid := ability.AbilityID()
				name := ability.AbilityName()
				if cid == expected || strings.HasPrefix(cid, expected+".") || name == toolName {
					// Python: return cid or expected
					if cid != "" {
						return cid
					}
					return expected
				}
			}
		}
	}
	return expected
}

// allTeammatesShutdownRequestedOrDone 判定全员是否已请求或完成 shutdown。
// 对齐 Python: _all_teammates_shutdown_requested_or_done (L1236-1263)
//
// 缺 team_backend/列表为空/任一成员状态不在 {shutdown_requested, shutdown} → false。
func allTeammatesShutdownRequestedOrDone(ctx context.Context, teamAgent *agent.TeamAgent) (bool, error) {
	if teamAgent == nil {
		logger.Debug(logComponent).
			Msg("[RemoteMemberBootstrap] shutdown cleanup skipped: missing public list_members")
		return false, nil
	}
	backend := teamAgent.TeamBackend()
	if backend == nil {
		logger.Debug(logComponent).
			Msg("[RemoteMemberBootstrap] shutdown cleanup skipped: missing public list_members")
		return false, nil
	}

	// Python: closed_statuses = {SHUTDOWN_REQUESTED.value, SHUTDOWN.value}
	members, err := backend.ListMembers(ctx)
	if err != nil {
		return false, err
	}
	if len(members) == 0 {
		return false, nil
	}
	for _, m := range members {
		// Python: status = str(getattr(member, "status", "") or "").strip().lower()
		status := strings.ToLower(strings.TrimSpace(m.Status))
		if status != string(atschema.MemberStatusShutdownRequested) && status != string(atschema.MemberStatusShutdown) {
			logger.Debug(logComponent).
				Str("member", m.MemberName).
				Str("status", status).
				Msg("[RemoteMemberBootstrap] shutdown cleanup waiting for member")
			return false, nil
		}
	}
	return true, nil
}

// scheduleShutdownCleanup 调度会话级延迟清理（无 pending 或已完成时新建任务）。
// 对齐 Python: _schedule_shutdown_cleanup (L1266-1283)
func scheduleShutdownCleanup(sessionID string, channelID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}

	shutdownCleanupMu.Lock()
	existing, ok := shutdownCleanupTasks[sid]
	needCreate := !ok || isEntryDone(existing)
	if needCreate {
		entry := &shutdownCleanupEntry{done: make(chan struct{})}
		shutdownCleanupTasks[sid] = entry
		shutdownCleanupMu.Unlock()
		// Python: asyncio.create_task(_delayed_shutdown_cleanup(sid, channel_id))
		go func() {
			entry.result = delayedShutdownCleanup(sid, channelID, entry)
			close(entry.done)
		}()
	} else {
		shutdownCleanupMu.Unlock()
	}

	logger.Info(logComponent).
		Str("session_id", sid).
		Str("channel", channelID).
		Msg("[RemoteMemberBootstrap] marked session for post-stream shutdown cleanup")
}

// isEntryDone 非阻塞探测任务是否已完成（entry 为 nil 视为未注册=需新建）。
func isEntryDone(entry *shutdownCleanupEntry) bool {
	if entry == nil {
		return true
	}
	select {
	case <-entry.done:
		return true
	default:
		return false
	}
}

// delayedShutdownCleanup 延迟执行清理：先等待 leader 响应 flush，再删除 team runtime。
// 对齐 Python: _delayed_shutdown_cleanup (L1286-1313)
func delayedShutdownCleanup(sessionID string, channelID string, entry *shutdownCleanupEntry) (deleted bool) {
	// Python finally: 若注册表中当前条目仍是本任务则移除
	defer func() {
		shutdownCleanupMu.Lock()
		if cur, ok := shutdownCleanupTasks[sessionID]; ok && cur == entry {
			delete(shutdownCleanupTasks, sessionID)
		}
		shutdownCleanupMu.Unlock()
	}()

	// Python: await asyncio.sleep(2.0)——给 leader 响应一个 flush 窗口
	time.Sleep(time.Duration(shutdownCleanupDelay * float64(time.Second)))
	return runPendingShutdownCleanupForSession(context.Background(), sessionID, channelID)
}

// runPendingShutdownCleanupForSession 删除分布式 team session（全员已 shutdown 后）。
// 对齐 Python: run_pending_shutdown_cleanup_for_session (L1364-1386)
func runPendingShutdownCleanupForSession(ctx context.Context, sessionID string, channelID string) bool {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false
	}
	activeChannelID := channelID

	// Python: manager = get_team_manager(active_channel_id)
	manager := GetTeamManager(activeChannelID)
	deleted, err := manager.DeleteSessionRuntime(ctx, sid, "team.shutdown_all_members: ")
	if err != nil {
		// 对齐 Python except → warning + notice(deleted=False) + return False
		logger.Warn(logComponent).Str("session_id", sid).Err(err).
			Msg("[RemoteMemberBootstrap] post-stream shutdown cleanup failed")
		pushShutdownCleanupNotice(ctx, sid, activeChannelID, false)
		return false
	}

	logger.Info(logComponent).
		Str("session_id", sid).
		Bool("deleted", deleted).
		Msg("[RemoteMemberBootstrap] post-stream shutdown cleanup finished")
	pushShutdownCleanupNotice(ctx, sid, activeChannelID, deleted)
	return deleted
}

// pushShutdownCleanupNotice 推送清理结果通知到前端（失败仅 warning）。
// 对齐 Python: _push_shutdown_cleanup_notice (L1334-1361)
func pushShutdownCleanupNotice(ctx context.Context, sessionID string, channelID string, deleted bool) {
	shutdownCleanupNoticeMu.RLock()
	pusher := shutdownCleanupNoticePusher
	shutdownCleanupNoticeMu.RUnlock()
	if pusher == nil {
		// Go 差异：注入缺失等价 Python transport 构造异常被 except 捕获
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("[RemoteMemberBootstrap] shutdown cleanup notice push failed: pusher not injected")
		return
	}

	requestID := "team_shutdown_cleanup_" + sessionID
	content := "团队解散清理未完成，请查看后端日志确认原因。"
	if deleted {
		content = "团队已清空并解散，远端成员已恢复为空闲状态。"
	}
	payload := map[string]any{
		"event_type": "chat.final",
		"request_id": requestID,
		"session_id": sessionID,
		"content":    content,
	}
	// Python: build_server_push_message(session_id=, request_id=, fallback_channel_id=channel_id, payload=)
	msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
	if err := pusher(ctx, msg); err != nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Err(err).
			Msg("[RemoteMemberBootstrap] shutdown cleanup notice push failed")
	}
}
