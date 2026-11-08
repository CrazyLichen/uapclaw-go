package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/sessionops"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
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

// noopLifecycleJournal 生命周期日志桩。
// spec §10 R-4：等 session/lifecycle.py 迁移后回填。
type noopLifecycleJournal struct{}

// noopPromptSuggestionForks 会话建议生成分支持有者桩。
// spec §8 D-10：待 10.3.30 recommendation（43821 行）落地后回填。
type noopPromptSuggestionForks struct{}

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
		AgentManager:        agentManager,
		PlanController:      planController,
		TrajectoryHook:      noopTrajectoryHook{},
		TeamBinding:         noopTeamBindingUnbinder{},
		LifecycleJournal:    noopLifecycleJournal{},
		ParticipantRegistry: session.NewRuntimeParticipantRegistry(),
		PromptForks:         noopPromptSuggestionForks{},
		SubagentStopper:     noopSubagentRuntimeStopper{},
	})
	prov.SetSessionOps(&sessionOpsAdapter{})
	prov.CheckpointEnsure = adapter.EnsurePersistentCheckpointer
	prov.RunnerRelease = func(ctx context.Context, sessionID string) error {
		return runner.Release(ctx, sessionID, false)
	}
	prov.DeleteAgentTeam = func(ctx context.Context, teamName string, sessionIDs []string) (bool, error) {
		return runner.DeleteAgentTeam(ctx, teamName, sessionIDs, true)
	}
	prov.RemoveMetadataCache = swarmsession.RemoveSessionMetadataCache
	// 会话目录解析与删除（删除事务的存在性判定与 rmtree）
	prov.SetResolveSessionDir(func(sessionID string) (string, bool) {
		dir := filepath.Join(swarmsession.GetSessionsDir(), sessionID)
		info, statErr := os.Stat(dir)
		return dir, statErr == nil && info.IsDir()
	})
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

// CopySessionContext 复制内存上下文。
// spec §10 R-5：需要 DeepAgent 实例，当前 provisioner 拿不到（AgentManager
// 返回 UapClawSwarm 不等于 SessionAgent），此桩为空实现，fork 只复制持久
// 历史与 metadata，不复制内存上下文。
func (*sessionOpsAdapter) CopySessionContext(context.Context, swruntime.SessionAgent, string, string, bool) error {
	return nil
}

// CopySessionState 复制 checkpoint state。
// spec §10 R-5：同 CopySessionContext，当前为空实现。
func (*sessionOpsAdapter) CopySessionState(context.Context, string, string, any, swruntime.SessionAgent) error {
	return nil
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

// SessionDeleted 桩：记 Debug 并返回。
func (noopPromptSuggestionForks) SessionDeleted(sessionID string) {
	logger.Debug(logComponent).Str("session_id", sessionID).
		Msg("prompt suggestion forks session_deleted not implemented (see 10.3.28 spec D-10)")
}

// StopSubagentRuntimes 桩：记 Debug 并返回 nil。
func (noopSubagentRuntimeStopper) StopSubagentRuntimes(
	_ context.Context, sessionID, _ string,
) error {
	logger.Debug(logComponent).Str("session_id", sessionID).
		Msg("stop subagent runtimes not implemented (see 10.3.28 spec D-9)")
	return nil
}

// RecoveringDelete 桩：恒 false。
// spec §10 R-4：会话生命周期日志未迁移，恢复中的删除无从判定。
func (noopLifecycleJournal) RecoveringDelete(string) bool { return false }

// MarkCleanup 桩。
func (noopLifecycleJournal) MarkCleanup(string) error { return nil }

// ──────────────────────────── 参数解析辅助（10.3.28b/c） ────────────────────────────

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

// requestUserID 取请求的 principal 用户标识（缺省空串）。
// Python 侧 user_id 由鉴权层透传；Go 放在 PermissionContext.PrincipalUserID。
func requestUserID(request *schema.AgentRequest) string {
	if request == nil || request.PermissionContext == nil {
		return ""
	}
	return strings.TrimSpace(request.PermissionContext.PrincipalUserID)
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
