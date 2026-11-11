package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	singleagentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/sessionops"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	lifecyclepkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	srvruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sessionAgentManagerAdapter 把 AgentManager 适配到 provisioner 的窄接口。
type sessionAgentManagerAdapter struct {
	// mgr 被适配的 AgentManager
	mgr *srvruntime.AgentManager
}

// planControllerAdapter 把 PlanModeController 适配到 provisioner 的窄接口。
type planControllerAdapter struct {
	// ctrl 被适配的计划模式控制器
	ctrl *swruntime.PlanModeController
}

// sessionOpsAdapter 把 session 包的函数适配到 provisioner 的窄接口。
type sessionOpsAdapter struct{}

// noopTrajectoryHook 轨迹删除钩子桩。
// spec §10 R-8：等领域二十二（可观测性与 Trajectory v2）落地后回填。
type noopTrajectoryHook struct{}

// noopTeamBindingUnbinder 团队绑定解绑桩。
// spec §10 R-1 同批缺口：团队绑定解绑（team_binding_store 迁移后回填，
// 与 R-1 同属"删除后置副作用"，spec §10 未单列编号）。
type noopTeamBindingUnbinder struct{}

// lifecycleJournal 会话生命周期操作日志，数据源为 10.3.28e 的 lifecycle 包。
// Python: server/runtime/session/lifecycle.py
type lifecycleJournal struct{}

// sessionRuntimeCleanerAgent 把 Runtime 的会话清理适配到 provisioner 窄接口。
// Python: runtime/service.py cleanup_session（:3024）—— 上游删除事务经
// SessionDeleteLifecycle 回调 Runtime 自身方法，因此这里只做"转接到 Runtime"。
//
// 取代此前直接调 AgentManager.CleanupSessionRuntime 的旧适配器：它只实现了
// 四步中的第 2 步，丢掉了关协调器、解绑 owner、重置 plan（spec G-4）。
type sessionRuntimeCleanerAgent struct {
	// runtime 执行账本归属方
	runtime *swruntime.AgentRuntime
}

// teamExecutionControllerAdapter 团队执行控制面适配器。
//
// spec §8 D-13：TeamManager 的四方法（SetRoundStateReporter 等）仍待 25.2，
// 此处只实现删除事务所需的 quiesce / dispose / committed / aborted 四个动作。
// 未实现的动作记 Debug 并返回 nil——删除事务不能因为"团队体系还没迁移"
// 而整体失败，否则 team.delete 仍然不可用。
type teamExecutionControllerAdapter struct{}

// noopSubagentRuntimeStopper 子代理运行时停止器桩。
// spec §8 D-9：待 25.8 subagent_runtime（3650 行）落地后回填。
type noopSubagentRuntimeStopper struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errAgentManagerMissing agent 管理器缺失
	errAgentManagerMissing = errors.New("agent manager unavailable")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newSessionProvisioner 装配 provisioner（10.3.28b/c）。
// 5 个依赖中 TrajectoryHook / TeamBinding / LifecycleJournal 为桩，
// 回填点见设计文档 §10（R-1/R-3/R-4）。
func newSessionProvisioner(
	plan *swruntime.PlanModeController,
	mgr *srvruntime.AgentManager,
	agentRuntime *swruntime.AgentRuntime,
) *swruntime.RuntimeSessionProvisioner {
	var planController swruntime.SessionPlanController
	if plan != nil {
		planController = &planControllerAdapter{ctrl: plan}
	}
	var agentManager swruntime.SessionAgentManager
	if mgr != nil {
		agentManager = &sessionAgentManagerAdapter{mgr: mgr}
	}
	prov := swruntime.NewRuntimeSessionProvisioner(swruntime.ProvisionerDeps{
		AgentManager:            agentManager,
		PlanController:          planController,
		TrajectoryHook:          noopTrajectoryHook{},
		TeamBinding:             noopTeamBindingUnbinder{},
		LifecycleJournal:        lifecycleJournal{},
		ParticipantRegistry:     sessionpkg.NewRuntimeParticipantRegistry(),
		PromptForks:             recommendation.Forks,
		SubagentStopper:         noopSubagentRuntimeStopper{},
		TeamExecutionController: teamExecutionControllerAdapter{},
	})
	prov.SetListTeamSessions(listTeamSessionIDs)
	prov.SetSessionOps(&sessionOpsAdapter{})
	prov.CheckpointEnsure = adapter.EnsurePersistentCheckpointer
	prov.SessionCleaner = &sessionRuntimeCleanerAgent{runtime: agentRuntime}
	prov.RunnerRelease = func(ctx context.Context, sessionID string) error {
		return runner.Release(ctx, sessionID, false)
	}
	prov.DeleteAgentTeam = func(ctx context.Context, teamName string, sessionIDs []string) (bool, error) {
		return runner.DeleteAgentTeam(ctx, teamName, sessionIDs, true)
	}
	prov.RemoveMetadataCache = swarmsession.RemoveSessionMetadataCache
	// 会话目录解析与删除（删除事务的存在性判定与 rmtree）
	prov.SetResolveSessionDir(resolveSessionDirForProvisioner)
	prov.SetRemoveSessionDir(func(dir string) error { return os.RemoveAll(dir) })
	return prov
}

// ──────────────────────────── 适配器实现 ────────────────────────────

// ClaimPrewarmedSession 委托 AgentManager（Go 无预热池，直接新建）。
func (a *sessionAgentManagerAdapter) ClaimPrewarmedSession(
	ctx context.Context, req swruntime.PrewarmClaimRequest,
) (swruntime.PrewarmClaim, error) {
	if a.mgr == nil {
		return swruntime.PrewarmClaim{}, errAgentManagerMissing
	}
	sessionID, err := a.mgr.CreateSession(req.ChannelID, "")
	if err != nil {
		return swruntime.PrewarmClaim{}, err
	}
	return swruntime.PrewarmClaim{
		SessionID:     sessionID,
		PrewarmHit:    false,
		PrewarmStatus: "miss",
	}, nil
}

// ActivateSessionPrewarm 无预热池，空实现。
func (a *sessionAgentManagerAdapter) ActivateSessionPrewarm(string) {}

// ReleaseSessionPrewarmClaim 无预热池，空实现。
func (a *sessionAgentManagerAdapter) ReleaseSessionPrewarmClaim(context.Context, string) error {
	return nil
}

// CreateSession 委托 AgentManager。
func (a *sessionAgentManagerAdapter) CreateSession(_ context.Context, channelID, sessionID string) (string, error) {
	if a.mgr == nil {
		return "", errAgentManagerMissing
	}
	return a.mgr.CreateSession(channelID, sessionID)
}

// GetAgentNoWait 委托 AgentManager。
// UapClawSwarm 不实现 provisioner 的 SessionAgent 能力面（无 EnsureInstance/Card），
// 返回 nil 使 fork 走「跳过内存上下文复制」降级路径（spec §10 R-5）。
func (a *sessionAgentManagerAdapter) GetAgentNoWait(string) swruntime.SessionAgent {
	return nil
}

// ResetSession 委托 PlanModeController。
func (a *planControllerAdapter) ResetSession(sessionID string) {
	if a.ctrl != nil {
		a.ctrl.ResetSession(sessionID)
	}
}

// ForkSession 委托 sessionops.ForkSession。
func (*sessionOpsAdapter) ForkSession(params swruntime.ForkSessionParams) (swruntime.ForkSessionResult, error) {
	res, err := sessionops.ForkSession(sessionops.ForkSessionParams{
		SourceSessionID: params.SourceSessionID,
		TargetSessionID: params.TargetSessionID,
		Title:           params.Title,
		ChannelID:       params.ChannelID,
	})
	if err != nil {
		return swruntime.ForkSessionResult{}, err
	}
	return swruntime.ForkSessionResult{
		SourceSessionID: res.SourceSessionID,
		SessionID:       res.SessionID,
		Title:           res.Title,
	}, nil
}

// CopySessionContext 复制内存上下文（R-5 回填，10.3.30）。
// Python: session_ops_service.py:2046 copy_session_context -> bool（无 raise）。
// 通过 28f 的 GetLiveSessionInstance 取该会话正在跑的 DeepAgent 后转调真实实现。
func (a *sessionOpsAdapter) CopySessionContext(
	ctx context.Context, agent swruntime.SessionAgent, sourceID, targetID string, forceHistory bool,
) bool {
	deepAgent := liveDeepAgentOf(agent, sourceID)
	if deepAgent == nil {
		// Python: 无 agent 时记 warning 跳过（session_provisioner.py:1348-1352）
		logger.Warn(logComponent).Str("source", sourceID).
			Msg("copy_session_context: no live DeepAgent for session, skipping in-memory copy")
		return false
	}
	if forceHistory {
		return sessionops.CopySessionContextForceHistory(ctx, deepAgent,
			sourceID, targetID, swarmsession.GetSessionsDir())
	}
	return sessionops.CopySessionContext(ctx, deepAgent, sourceID, targetID)
}

// CopySessionState 复制 checkpoint state（R-5 回填，10.3.30）。
// Python: session_ops_service.py:1903 copy_session_state -> bool（无 raise）。
func (a *sessionOpsAdapter) CopySessionState(
	ctx context.Context, sourceID, targetID string, card any, agent swruntime.SessionAgent,
) bool {
	return sessionops.CopySessionState(ctx, sourceID, targetID, toAgentCard(card),
		liveDeepAgentOf(agent, sourceID))
}

// liveDeepAgentOf 取该会话正在跑的 DeepAgent（28f 提供的能力）。
// SessionAgent 接口本身不含 GetLiveSessionInstance，故用类型断言而非扩接口。
func liveDeepAgentOf(agent swruntime.SessionAgent, sessionID string) *harness.DeepAgent {
	provider, ok := agent.(interface {
		GetLiveSessionInstance(string) *harness.DeepAgent
	})
	if !ok {
		return nil
	}
	return provider.GetLiveSessionInstance(sessionID)
}

// toAgentCard 把 card 断言为 AgentCard；失败时给默认卡（对齐 Python 的
// AgentCard(id="jiuwenswarm", name="jiuwenswarm") 兜底）。
func toAgentCard(card any) *singleagentschema.AgentCard {
	if typed, ok := card.(*singleagentschema.AgentCard); ok && typed != nil {
		return typed
	}
	return singleagentschema.NewAgentCard(
		singleagentschema.WithAgentID("jiuwenswarm"),
		singleagentschema.WithAgentName("jiuwenswarm"),
	)
}

// GetSessionMetadata 读会话元数据。
func (*sessionOpsAdapter) GetSessionMetadata(sessionID string) map[string]any {
	return swarmsession.GetSessionMetadata(sessionID)
}

// SessionDirHasMetadata 判定 metadata.json 是否存在。
func (*sessionOpsAdapter) SessionDirHasMetadata(sessionID string) bool {
	path := filepath.Join(swarmsession.GetSessionsDir(), sessionID, "metadata.json")
	_, err := os.Stat(path)
	return err == nil
}

// InitSessionMetadata 初始化会话元数据。
// 走读-改-写：session 包的 InitSessionMetadata 只接受 6 个位置参数，
// 无法写入 project/work_mode/model 等字段，故直接组装完整 map 落盘。
func (*sessionOpsAdapter) InitSessionMetadata(params swruntime.InitMetadataParams) error {
	sessionsDir := swarmsession.GetSessionsDir()
	metadata := swarmsession.ReadSessionMetadata(sessionsDir, params.SessionID)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["session_id"] = params.SessionID
	metadata["channel_id"] = params.ChannelID
	metadata["user_id"] = params.UserID
	metadata["title"] = params.Title
	metadata["mode"] = params.Mode
	metadata["project_dir"] = params.ProjectDir
	metadata["project_id"] = params.ProjectID
	metadata["persist_session"] = params.PersistSession
	metadata["work_mode"] = params.WorkMode
	metadata["model"] = params.Model
	metadata["moa_enabled"] = params.MoaEnabled
	metadata["cron_id"] = params.CronID
	if _, ok := metadata["team_name"]; !ok {
		metadata["team_name"] = ""
	}
	if _, ok := metadata["created_at"]; !ok {
		metadata["created_at"] = swarmsession.CurrentTimestamp()
	}
	if params.ChannelMetadata != nil {
		metadata["channel_metadata"] = params.ChannelMetadata
	}
	if err := swarmsession.WriteSessionMetadata(sessionsDir, params.SessionID, metadata); err != nil {
		return err
	}
	// 让后续 GetSessionMetadata 立即读到新值
	swarmsession.RemoveSessionMetadataCache(params.SessionID)
	return nil
}

// SetSessionModelSelection 写会话模型选择。
// spec §10 R-5：当前无对应持久化接口，空实现（模型选择存储未迁移）。
func (*sessionOpsAdapter) SetSessionModelSelection(string, map[string]any) error { return nil }

// HistoryExists 判定 history 文件是否存在。
func (*sessionOpsAdapter) HistoryExists(sessionID string) bool {
	path := filepath.Join(swarmsession.GetSessionsDir(), sessionID, "history.json")
	_, err := os.Stat(path)
	return err == nil
}

// BeginSessionDelete 桩：告警并放行。
func (noopTrajectoryHook) BeginSessionDelete(_ context.Context, sessionID string) error {
	logger.Debug(logComponent).Str("session_id", sessionID).
		Msg("trajectory delete not implemented (see 10.3.28 spec R-8)")
	return nil
}

// AbortSessionDelete 桩。
func (noopTrajectoryHook) AbortSessionDelete(context.Context, string) error { return nil }

// CommitSessionDelete 桩。
func (noopTrajectoryHook) CommitSessionDelete(context.Context, string) error { return nil }

// UnbindSession 桩：告警并放行。
func (noopTeamBindingUnbinder) UnbindSession(teamName, sessionID string) error {
	logger.Debug(logComponent).Str("team_name", teamName).Str("session_id", sessionID).
		Msg("team binding unbind not implemented (see 10.3.28 spec R-1 batch)")
	return nil
}

// CleanupSession 委托 Runtime 的四步清理（resetPlanState=false）。
// Python: service.py:3440 cleanup_session(reset_plan_state=False)
//
// 删除事务期间必须保留 plan 状态：否则回滚时状态已丢。
func (a *sessionRuntimeCleanerAgent) CleanupSession(
	ctx context.Context, channelID, sessionID string,
) error {
	if a.runtime == nil {
		return nil
	}
	_, err := a.runtime.CleanupSession(ctx, channelID, sessionID, false)
	return err
}

// QuiesceForDelete 静默团队执行（删除前）。
// Python: TeamExecutionController.quiesce_for_delete
func (teamExecutionControllerAdapter) QuiesceForDelete(
	_ context.Context, target sessionpkg.SessionLifecycleTarget, _ string,
) error {
	logger.Debug(logComponent).Str("team_name", target.TeamName).
		Msg("team quiesce for delete not implemented (see 10.3.28 spec D-13)")
	return nil
}

// DisposeAfterResourceRelease 释放资源后销毁团队执行。
// Python: TeamExecutionController.dispose_after_resource_release
func (teamExecutionControllerAdapter) DisposeAfterResourceRelease(
	_ context.Context, target sessionpkg.SessionLifecycleTarget, _ string,
) error {
	logger.Debug(logComponent).Str("team_name", target.TeamName).
		Msg("team dispose after release not implemented (see 10.3.28 spec D-13)")
	return nil
}

// DeleteAborted 删除已中止（Runner 失败）。
// Python: TeamExecutionController.delete_aborted
func (teamExecutionControllerAdapter) DeleteAborted(target sessionpkg.SessionLifecycleTarget) {
	logger.Debug(logComponent).Str("team_name", target.TeamName).
		Msg("team delete aborted")
}

// DeleteCommitted 删除已提交。
// Python: TeamExecutionController.delete_committed
func (teamExecutionControllerAdapter) DeleteCommitted(target sessionpkg.SessionLifecycleTarget) {
	logger.Debug(logComponent).Str("team_name", target.TeamName).
		Msg("team delete committed")
}

// StopSubagentRuntimes 桩：记 Debug 并返回 nil。
func (noopSubagentRuntimeStopper) StopSubagentRuntimes(
	_ context.Context, sessionID, _ string,
) error {
	logger.Debug(logComponent).Str("session_id", sessionID).
		Msg("stop subagent runtimes not implemented (see 10.3.28 spec D-9)")
	return nil
}

// RecoveringDelete 该会话是否处于未完成的删除操作中。
// Python: lifecycle.py session_delete_pending（:142）
//
// 数据来自 10.3.28e 已落地的 lifecycle 包：operation.kind == "delete" 且
// status != "completed"，同时 blocked 为真（上游同一组三个条件）。
// 此前是恒 false 的桩，导致「删除中途崩溃 → 重启后无法识别恢复中的删除」。
func (lifecycleJournal) RecoveringDelete(sessionID string) bool {
	return lifecyclepkg.SessionDeletePending(sessionID)
}

// MarkCleanup 标记该会话进入清理阶段。
// Python: 由 lifecycle.MarkFinalizing 承载（operation.status → finalizing）。
// 无待完成操作时为空操作（上游同样容忍：不是删除流程就无需标记）。
func (lifecycleJournal) MarkCleanup(sessionID string) error {
	if !lifecyclepkg.SessionDeletePending(sessionID) {
		return nil
	}
	// deleted=false：清理阶段尚未落盘删除标记，deleted 由后续 committed 步骤置位。
	_, err := lifecyclepkg.MarkFinalizing("session", sessionID, false, nil)
	return err
}

// ──────────────────────────── 参数解析辅助（10.3.28b/c） ────────────────────────────

// resolveSessionDirForProvisioner 解析会话目录并过白名单。
// Python: server/runtime/session/session_history.py resolve_session_dir
//
// 两步防御（白名单 + 根内确认）与归档目录回退都在 common.ResolveSessionDir 内。
// 非法标识返回空路径 + false，由 provisioner 转成 BAD_REQUEST。
func resolveSessionDirForProvisioner(sessionID string) (string, bool) {
	dir, exists, err := common.ResolveSessionDir(sessionID, swarmsession.GetSessionsDir())
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("session directory rejected by whitelist")
		return "", false
	}
	return dir, exists
}

// parseSessionParams 解析请求参数为可写 map（nil/非法返回空 map）。
// 与 server/runtime 的同名函数语义一致，但本包需要独立实现以避开跨包依赖。
func parseSessionParams(request *schema.AgentRequest) map[string]any {
	if request == nil || len(request.Params) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(request.Params, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// paramString 取字符串参数（缺失或类型不符返回空串）。
func paramString(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return strings.TrimSpace(v)
}

// requestUserID 取请求的 user_id（缺省空串）。
//
// Python 侧 user_id 由鉴权层透传，落在请求 metadata 上。Go 原先读
// req.PermissionContext.PrincipalUserID，但该字段 9.90e 已删除——
// 权限上下文由 adapter 从 metadata 构建后注入 ctx，不随请求传递。
// 这里改为与 runtime.requestUserID 同源的读取顺序：metadata 优先，params 兜底。
func requestUserID(request *schema.AgentRequest) string {
	if request == nil {
		return ""
	}
	if request.Metadata != nil {
		if v, ok := request.Metadata["user_id"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if v, ok := parseSessionParams(request)["user_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// paramBool 取布尔参数（缺失或类型不符返回 false）。
func paramBool(params map[string]any, key string) bool {
	v, _ := params[key].(bool)
	return v
}

// firstNonEmptyString 取首个非空字符串。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sessionProvisionErrorResponse 把 provisioner 业务错误映射为响应。
// SessionProvisionError 带可选 Code（缺省 BAD_REQUEST），其余错误按 BAD_REQUEST 处理。
func sessionProvisionErrorResponse(
	request *schema.AgentRequest,
	err error,
) *schema.AgentResponse {
	code := "BAD_REQUEST"
	message := err.Error()
	var provErr *swruntime.SessionProvisionError
	if errors.As(err, &provErr) {
		message = provErr.Message
		if provErr.Code != "" {
			code = provErr.Code
		}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{
			"error": message,
			"code":  code,
		}),
	)
}

// listTeamSessionIDs 列出团队下全部会话标识（绑定的 ∪ 扫目录的）。
// Python: session_provisioner.py _inventory_team_session_ids（:1873）
//
// 两处都要扫：活动区与归档区。团队删除必须把已归档的成员会话也算进来，
// 否则归档区会留下属于已删除团队的孤儿目录。
func listTeamSessionIDs(teamName string) []string {
	active := swarmsession.GetSessionsDir()
	roots := []string{active, filepath.Join(filepath.Dir(active), "sessions_archived")}
	matched := map[string]struct{}{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			metadata := swarmsession.GetSessionMetadata(entry.Name())
			mode, _ := metadata["mode"].(string)
			storedTeam, _ := metadata["team_name"].(string)
			if schema.IsTeamMode(mode) && storedTeam == teamName {
				matched[entry.Name()] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(matched))
	for sessionID := range matched {
		out = append(out, sessionID)
	}
	sort.Strings(out)
	return out
}
