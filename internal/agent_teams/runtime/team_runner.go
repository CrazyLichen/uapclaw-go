package runtime

// TeamRunner — 团队执行入口函数。
//
// Python: _TeamRunnerMixin + _TeamRunnerClassMixin
// (openjiuwen/core/runner/team_runner.py)
//
// 本文件放在 runtime 包中（而非 runner 包）以避免循环依赖：
// runner 包被 agent_teams 下游包间接导入，无法反向导入 agent_teams。
//
// 三条路径路由：
//   - TeamAgent 路径（默认）：spec/str → Activate → invoke/stream
//   - BaseTeam 路径（base=True）：multi_agent.BaseTeam → invoke/stream
//   - Member 路径（member=True）：已构建的 TeamAgent → FinalizeMember + invoke/stream
//
// 对应 Python 代码：openjiuwen/core/runner/team_runner.py

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeGetter 可获取 TeamRuntime 的最小接口。
// 部分具体 BaseTeam 实现（如 HierarchicalToolsTeam）提供 GetRuntime() 方法，
// 但 BaseTeam 接口本身不包含此方法。通过此接口在运行时检查。
type RuntimeGetter interface {
	GetRuntime() *team_runtime.TeamRuntime
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamRunnerLogComponent 日志组件
	teamRunnerLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RunAgentTeam 统一团队执行入口（非流式）。
// Python: Runner.run_agent_team(agent_team, inputs, *, base, member, session, context, envs)
//
// 三条路径路由：
//   - base=True → RunBaseTeam（multi_agent BaseTeam 路径）
//   - member=True → RunTeamMember（spawn-only 路径）
//   - 默认 → runAgentTeam（TeamAgent 路径）
func RunAgentTeam(
	ctx context.Context,
	agentTeam any,
	inputs any,
	base bool,
	member bool,
	sess any,
) (map[string]any, error) {
	if base {
		return RunBaseTeam(ctx, agentTeam, inputs, sess)
	}
	if member {
		return RunTeamMember(ctx, agentTeam, inputs, sess)
	}
	return runAgentTeam(ctx, agentTeam, inputs, sess)
}

// RunAgentTeamStreaming 统一团队执行入口（流式）。
// Python: Runner.run_agent_team_streaming(agent_team, inputs, *, base, member, session, ...)
func RunAgentTeamStreaming(
	ctx context.Context,
	agentTeam any,
	inputs any,
	base bool,
	member bool,
	sess any,
	streamLogger *monitor.TeamStreamLogger,
) (<-chan stream.Schema, error) {
	if base {
		return RunBaseTeamStreaming(ctx, agentTeam, inputs, sess)
	}
	if member {
		return RunTeamMemberStreaming(ctx, agentTeam, inputs, sess)
	}
	return runAgentTeamStreaming(ctx, agentTeam, inputs, sess, streamLogger)
}

// RunBaseTeam BaseTeam 路径（非流式）。
// Python: _TeamRunnerMixin._run_base_team(base_team, inputs, *, session, context, envs)
func RunBaseTeam(
	ctx context.Context,
	baseTeam any,
	inputs any,
	sess any,
) (map[string]any, error) {
	teamInstance, err := prepareBaseTeam(baseTeam)
	if err != nil {
		return nil, err
	}

	teamSession := createAgentTeamSession(baseTeam, sess)
	inputsDict, _ := inputs.(map[string]any)
	if preErr := teamSession.PreRun(ctx, inputsDict); preErr != nil {
		logger.Warn(teamRunnerLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunBaseTeam: PreRun 失败")
	}

	var teamRuntime *team_runtime.TeamRuntime
	if getter, ok := teamInstance.(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	// Python: L304 — return await team_instance.invoke(inputs, session=team_session)
	var result any
	var invokeErr error
	result, invokeErr = teamInstance.Invoke(ctx, inputsDict, maschema.WithTeamSession(teamSession))

	if teamRuntime != nil {
		teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
	}
	_ = teamSession.PostRun(ctx)

	if result == nil {
		return nil, invokeErr
	}
	if resultMap, ok := result.(map[string]any); ok {
		return resultMap, invokeErr
	}
	// 结果不是 map[string]any 但调用无错误 → 返回包装错误
	if invokeErr == nil {
		return nil, fmt.Errorf("RunBaseTeam: invoke 返回了非 map[string]any 类型的结果（类型: %T）", result)
	}
	return nil, invokeErr
}

// RunBaseTeamStreaming BaseTeam 路径（流式）。
// Python: _TeamRunnerMixin._run_base_team_streaming(...)
func RunBaseTeamStreaming(
	ctx context.Context,
	baseTeam any,
	inputs any,
	sess any,
) (<-chan stream.Schema, error) {
	teamInstance, err := prepareBaseTeam(baseTeam)
	if err != nil {
		return nil, err
	}

	teamSession := createAgentTeamSession(baseTeam, sess)
	inputsDict, _ := inputs.(map[string]any)
	if preErr := teamSession.PreRun(ctx, inputsDict); preErr != nil {
		logger.Warn(teamRunnerLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunBaseTeamStreaming: PreRun 失败")
	}

	var teamRuntime *team_runtime.TeamRuntime
	if getter, ok := teamInstance.(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	agentCh, streamErr := teamInstance.Stream(ctx, inputsDict, maschema.WithTeamSession(teamSession))
	if streamErr != nil {
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(ctx)
		return nil, streamErr
	}

	mergedCh := make(chan stream.Schema, 64)
	go func() {
		defer close(mergedCh)
		for chunk := range agentCh {
			mergedCh <- chunk
		}
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(ctx)
	}()

	return mergedCh, nil
}

// RunTeamMember Member 路径（非流式）。
// Python: _TeamRunnerMixin._run_team_member(agent, inputs, *, session)
func RunTeamMember(
	ctx context.Context,
	agentTeam any,
	inputs any,
	sess any,
) (map[string]any, error) {
	ag, ok := agentTeam.(*agent.TeamAgent)
	if !ok {
		return nil, fmt.Errorf("RunTeamMember: agent_team 必须是 *agent.TeamAgent，实际 %T", agentTeam)
	}

	teamSession := createAgentTeamSession(agentTeam, sess)
	inputsDict, _ := inputs.(map[string]any)
	if preErr := teamSession.PreRun(ctx, inputsDict); preErr != nil {
		logger.Warn(teamRunnerLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunTeamMember: PreRun 失败")
	}

	var teamRuntime *team_runtime.TeamRuntime
	if getter, ok := agentTeam.(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	result, err := ag.Invoke(ctx, inputsDict, agentinterfaces.WithSession(teamSession))

	_ = FinalizeMember(ctx, ag)
	if teamRuntime != nil {
		teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
	}
	_ = teamSession.PostRun(ctx)

	return result, err
}

// RunTeamMemberStreaming Member 路径（流式）。
// Python: _TeamRunnerMixin._run_team_member_streaming(agent, inputs, *, session)
func RunTeamMemberStreaming(
	ctx context.Context,
	agentTeam any,
	inputs any,
	sess any,
) (<-chan stream.Schema, error) {
	ag, ok := agentTeam.(*agent.TeamAgent)
	if !ok {
		return nil, fmt.Errorf("RunTeamMemberStreaming: agent_team 必须是 *agent.TeamAgent，实际 %T", agentTeam)
	}

	teamSession := createAgentTeamSession(agentTeam, sess)
	inputsDict, _ := inputs.(map[string]any)
	if preErr := teamSession.PreRun(ctx, inputsDict); preErr != nil {
		logger.Warn(teamRunnerLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunTeamMemberStreaming: PreRun 失败")
	}

	var teamRuntime *team_runtime.TeamRuntime
	if getter, ok := agentTeam.(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	agentCh, streamErr := ag.Stream(ctx, inputsDict, agentinterfaces.WithSession(teamSession))
	if streamErr != nil {
		_ = FinalizeMember(ctx, ag)
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(ctx)
		return nil, streamErr
	}

	mergedCh := make(chan stream.Schema, 64)
	go func() {
		defer close(mergedCh)
		for chunk := range agentCh {
			mergedCh <- chunk
		}
		_ = FinalizeMember(ctx, ag)
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(ctx)
	}()

	return mergedCh, nil
}

// InteractAgentTeam 交互载荷投递到活跃团队的门控。
// Python: Runner.interact_agent_team(payload, *, team_name, session_id)
func InteractAgentTeam(
	ctx context.Context,
	payload any,
	teamName string,
	sessionID string,
) (*interaction.DeliverResult, error) {
	if teamName == "" || sessionID == "" {
		return interaction.NewDeliverResultFailure("missing_target"), nil
	}
	ctx = bindInteractTeamSession(ctx, sessionID)
	return GetTeamRuntimeManager().Interact(ctx, payload, teamName, sessionID)
}

// GetAgentTeamMonitor 获取活跃团队的 TeamMonitor。
// Python: Runner.get_agent_team_monitor(*, team_name, session_id, hide_dm)
func GetAgentTeamMonitor(ctx context.Context, teamName string, sessionID string, hideDM bool) (*monitor.TeamMonitor, error) {
	return GetTeamRuntimeManager().GetMonitor(ctx, teamName, sessionID, hideDM)
}

// ListActiveTeams 列出所有活跃团队。
// Python: Runner.list_active_teams()
func ListActiveTeams() []ActiveTeamInfo {
	return GetTeamRuntimeManager().ListActiveTeams()
}

// ResolveTeamAgentSpec 解析 run_agent_team 输入为 TeamAgentSpec。
// Python: _TeamRunnerMixin._resolve_team_agent_spec(agent_team, *, session)
func ResolveTeamAgentSpec(ctx context.Context, agentTeam any, sess any) (*atschema.TeamAgentSpec, error) {
	if spec, ok := agentTeam.(*atschema.TeamAgentSpec); ok {
		return spec, nil
	}
	if name, ok := agentTeam.(string); ok {
		mgr := GetTeamRuntimeManager()
		entry := mgr.Pool().Get(name)
		if entry != nil && entry.Agent.Spec() != nil {
			return entry.Agent.Spec(), nil
		}
		if sess != nil {
			spec := resolveSpecFromSessionBucket(ctx, name, sess)
			if spec != nil {
				return spec, nil
			}
		}
		return nil, fmt.Errorf(
			"team %q has no live pool entry and no persisted spec in the supplied session; first-time runs must pass a TeamAgentSpec on a new session",
			name,
		)
	}
	return nil, fmt.Errorf(
		"run_agent_team accepts str | *TeamAgentSpec; got %T. For BaseTeam pass base=True",
		agentTeam,
	)
}

// CloseTeamInteractGate 关闭并排空团队交互门控。
// Python: _TeamRunnerMixin._close_team_interact_gate(*, team_name, session_id)
func CloseTeamInteractGate(teamName string, sessionID string) {
	closeTeamInteractGate(teamName, sessionID)
}

// BuildTeamRuntimeReadyChunk 构建团队运行时就绪首帧。
// Python: _TeamRunnerMixin._build_team_runtime_ready_chunk(...)
func BuildTeamRuntimeReadyChunk(
	teamName string,
	sessionID string,
	actionKind RunActionKind,
	leaderMemberName string,
	leaderRole atschema.TeamRole,
) *atschema.TeamOutputSchema {
	return buildTeamRuntimeReadyChunk(teamName, sessionID, actionKind, leaderMemberName, leaderRole)
}

// BuildTeamControlChunk 构建团队控制帧。
// Python: _TeamRunnerMixin._build_team_control_chunk(payload, *, ...)
func BuildTeamControlChunk(
	payload map[string]any,
	leaderMemberName string,
	leaderRole atschema.TeamRole,
) *atschema.TeamOutputSchema {
	return buildTeamControlChunk(payload, leaderMemberName, leaderRole)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runAgentTeam TeamAgent 路径（非流式）。
// Python: _TeamRunnerMixin.run_agent_team(agent_team, inputs, *, session)
func runAgentTeam(
	ctx context.Context,
	agentTeam any,
	inputs any,
	sess any,
) (map[string]any, error) {
	spec, err := ResolveTeamAgentSpec(ctx, agentTeam, sess)
	if err != nil {
		return nil, err
	}

	activation, err := GetTeamRuntimeManager().Activate(ctx, spec, sess, inputs)
	if err != nil {
		return nil, err
	}

	action := activation.Action
	if IsTeamRejectKind(action.Kind) {
		logger.Warn(teamRunnerLogComponent).
			Str("team_name", spec.TeamName).
			Str("session_id", activation.Session.GetSessionID()).
			Str("kind", string(action.Kind)).
			Str("reason", action.Reason).
			Msg("run_agent_team rejected")
		return nil, nil
	}

	result, err := activation.Agent.Invoke(ctx, nil, agentinterfaces.WithSession(activation.Session))

	_ = GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
	closeTeamInteractGate(spec.TeamName, activation.Session.GetSessionID())
	_ = activation.Session.PostRun(ctx)

	return result, err
}

// runAgentTeamStreaming TeamAgent 路径（流式）。
// Python: _TeamRunnerMixin.run_agent_team_streaming(...)
func runAgentTeamStreaming(
	ctx context.Context,
	agentTeam any,
	inputs any,
	sess any,
	streamLogger *monitor.TeamStreamLogger,
) (<-chan stream.Schema, error) {
	spec, err := ResolveTeamAgentSpec(ctx, agentTeam, sess)
	if err != nil {
		return nil, err
	}

	activation, err := GetTeamRuntimeManager().Activate(ctx, spec, sess, inputs)
	if err != nil {
		return nil, err
	}

	action := activation.Action
	if IsTeamRejectKind(action.Kind) {
		logger.Warn(teamRunnerLogComponent).
			Str("team_name", spec.TeamName).
			Str("session_id", activation.Session.GetSessionID()).
			Str("kind", string(action.Kind)).
			Str("reason", action.Reason).
			Msg("run_agent_team_streaming rejected")
		return nil, nil
	}

	var leaderMemberName string
	if bp := activation.Agent.AgentCard(); bp != nil {
		leaderMemberName = bp.Name
	}
	leaderRole := atschema.TeamRoleLeader

	readyChunk := buildTeamRuntimeReadyChunk(
		spec.TeamName,
		activation.Session.GetSessionID(),
		action.Kind,
		leaderMemberName,
		leaderRole,
	)

	agentCh, streamErr := activation.Agent.Stream(ctx, nil, agentinterfaces.WithSession(activation.Session))
	if streamErr != nil {
		_ = GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
		closeTeamInteractGate(spec.TeamName, activation.Session.GetSessionID())
		_ = activation.Session.PostRun(ctx)
		return nil, streamErr
	}

	mergedCh := make(chan stream.Schema, 64)
	go func() {
		defer close(mergedCh)
		if streamLogger != nil {
			streamLogger.Feed(readyChunk)
		}
		mergedCh <- readyChunk
		for chunk := range agentCh {
			if streamLogger != nil {
				streamLogger.Feed(chunk)
			}
			mergedCh <- chunk
		}
		if streamLogger != nil {
			streamLogger.Flush()
		}
		_ = GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
		closeTeamInteractGate(spec.TeamName, activation.Session.GetSessionID())
		_ = activation.Session.PostRun(ctx)
	}()

	return mergedCh, nil
}

// resolveSpecFromSessionBucket 从 session bucket 读取持久化的 TeamAgentSpec。
// Python: _TeamRunnerMixin._resolve_spec_from_session_bucket(*, team_name, session)
func resolveSpecFromSessionBucket(ctx context.Context, teamName string, sess any) *atschema.TeamAgentSpec {
	teamSession := createAgentTeamSession(nil, sess)

	if preErr := teamSession.PreRun(ctx); preErr != nil {
		logger.Warn(teamRunnerLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("resolveSpecFromSessionBucket: PreRun 失败")
		return nil
	}

	bucket := metadata.ReadTeamNamespace(teamSession, teamName)
	if bucket == nil {
		return nil
	}

	specData, ok := bucket["spec"]
	if !ok || specData == nil {
		return nil
	}

	specMap, ok := specData.(map[string]any)
	if !ok {
		logger.Warn(teamRunnerLogComponent).
			Str("team_name", teamName).
			Str("session_id", teamSession.GetSessionID()).
			Msg("resolveSpecFromSessionBucket: spec 数据类型非 map[string]any")
		return nil
	}
	spec := atschema.NewTeamAgentSpecFromDict(specMap)
	if spec == nil {
		logger.Warn(teamRunnerLogComponent).
			Str("team_name", teamName).
			Str("session_id", teamSession.GetSessionID()).
			Msg("resolveSpecFromSessionBucket: spec 反序列化失败")
		return nil
	}
	return spec
}

// prepareBaseTeam 解析 BaseTeam 输入为具体实例。
// Python: _TeamRunnerMixin._prepare_base_team(base_team)
func prepareBaseTeam(baseTeam any) (maschema.BaseTeam, error) {
	if team, ok := baseTeam.(maschema.BaseTeam); ok {
		return team, nil
	}
	return nil, fmt.Errorf(
		"run_agent_team(base=True) accepts BaseTeam; got %T. For TeamAgentSpec drop base=True. For str team_id use runner.ResourceMgr",
		baseTeam,
	)
}

// createAgentTeamSession 从 agent_team 和 session 参数创建 AgentTeamSession。
// Python: _TeamRunnerMixin._create_agent_team_session(agent_team, session)
func createAgentTeamSession(agentTeam any, sess any) *session.AgentTeamSession {
	switch s := sess.(type) {
	case *session.AgentTeamSession:
		return s
	case *session.Session:
		return session.CreateAgentTeamSession(s.GetSessionID(), s.GetEnvs(), "")
	case string:
		return session.NewAgentTeamSession(session.WithAgentTeamSessionID(s))
	default:
		return session.NewAgentTeamSession()
	}
}

// closeTeamInteractGate 关闭并排空团队交互门控。
// Python: _TeamRunnerMixin._close_team_interact_gate(*, team_name, session_id)
func closeTeamInteractGate(teamName string, sessionID string) {
	entry := GetTeamRuntimeManager().Pool().Get(teamName)
	if entry == nil || entry.SessionID != sessionID {
		return
	}
	_ = entry.InteractGate.CloseAndDrain(context.Background())
}

// buildTeamRuntimeReadyChunk 构建团队运行时就绪首帧。
// Python: _TeamRunnerMixin._build_team_runtime_ready_chunk(...)
func buildTeamRuntimeReadyChunk(
	teamName string,
	sessionID string,
	actionKind RunActionKind,
	leaderMemberName string,
	leaderRole atschema.TeamRole,
) *atschema.TeamOutputSchema {
	payload := map[string]any{
		"event_type":      "team.runtime_ready",
		"team_name":       teamName,
		"session_id":      sessionID,
		"activation_kind": string(actionKind),
	}
	base := stream.OutputSchema{
		Type:    "message",
		Index:   0,
		Payload: payload,
	}
	role := leaderRole
	return atschema.NewTeamOutputSchema(base, strPtr(leaderMemberName), &role)
}

// buildTeamControlChunk 构建团队控制帧。
// Python: _TeamRunnerMixin._build_team_control_chunk(payload, *, ...)
func buildTeamControlChunk(
	payload map[string]any,
	leaderMemberName string,
	leaderRole atschema.TeamRole,
) *atschema.TeamOutputSchema {
	base := stream.OutputSchema{
		Type:    "message",
		Index:   0,
		Payload: payload,
	}
	role := leaderRole
	return atschema.NewTeamOutputSchema(base, strPtr(leaderMemberName), &role)
}

// bindInteractTeamSession 绑定交互团队 session 到 context。
// Python: _TeamRunnerMixin._bind_interact_team_session(session_id)
func bindInteractTeamSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	state := sessionctx.InitSessionState()
	state.SetSessionID(sessionID)
	return sessionctx.WithSessionState(ctx, state)
}
