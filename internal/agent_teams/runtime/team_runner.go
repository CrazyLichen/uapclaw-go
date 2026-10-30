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
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeGetter 可获取 TeamRuntime 的最小接口。
// 部分具体 BaseTeam 实现（如 HierarchicalToolsTeam）提供 GetRuntime() 方法，
// 但 BaseTeam 接口本身不包含此方法。通过此接口在运行时检查。
type RuntimeGetter interface {
	GetRuntime() *team_runtime.TeamRuntime
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const ()

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RunTeamAgent TeamAgent 路径（非流式）——统一团队执行入口。
// Python: Runner.run_agent_team(agent_team, inputs, *, session)（默认分支）
//
// S-71(A1): 原路由函数 RunAgentTeam(agentTeam any, base, member) 删除——
// 生产调用方（deep_adapter）恒走默认分支传 *TeamAgentSpec，base/member
// 分支零生产调用且各自已有具体类型入口（RunBaseTeam/RunTeamMember）。
// str 场景由外层先调 ResolveTeamAgentSpec 解析后传 spec。
// inputs 同步收紧 map[string]any：三个下游消费者（TeamAgent.Invoke/
// BaseTeam.Invoke/PreRun）全部要求 map，Python 传非 dict 实际是
// AttributeError 崩溃，Go 用类型系统直接编码该约束。
func RunTeamAgent(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	inputs map[string]any,
	sess any,
) (map[string]any, error) {
	return runTeamAgent(ctx, spec, inputs, sess)
}

// RunTeamAgentStreaming TeamAgent 路径（流式）——统一团队执行入口。
// Python: Runner.run_agent_team_streaming(agent_team, inputs, *, session, ...)
// S-71(A1): 同 RunTeamAgent——路由函数删除，str 由外层解析。
func RunTeamAgentStreaming(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	inputs map[string]any,
	sess any,
	streamLogger *monitor.TeamStreamLogger,
) (<-chan stream.Schema, error) {
	return runTeamAgentStreaming(ctx, spec, inputs, sess, streamLogger)
}

// RunBaseTeam BaseTeam 路径（非流式）。
// Python: _TeamRunnerMixin._run_base_team(base_team, inputs, *, session, context, envs)
// M-56: 签名换 maschema.BaseTeam 具体类型（原 any）——
// str 场景由调用方经 runner.ResourceMgr.GetAgentTeam 解析后传实例。
// S-71(A1): inputs 收紧 map[string]any（下游 Invoke/PreRun 均要求 map）。
//
// M-02 有意偏差（归档）：Python `_prepare_base_team` 允许 base_team 为 str，
// 内部经 ResourceMgr.get_agent_team(team_id=...) 解析。Go 端不提供该分支：
//   - 类型系统直接编码约束，str 输入在编译期即被拒绝，不会运行期报错；
//   - RunBaseTeam/RunBaseTeamStreaming 生产零调用（仅测试），
//     实际生产入口 RunTeamAgentStreaming 的 str 场景已由 ResolveTeamAgentSpec 覆盖；
//   - SDK 使用方需要 str 语义时可自行调 runner.GetResourceMgr().GetAgentTeam。
//
// 如未来出现 str 生产调用，应新增独立 ResolveBaseTeam 而非放宽本签名。
func RunBaseTeam(
	ctx context.Context,
	baseTeam maschema.BaseTeam,
	inputs map[string]any,
	sess any,
) (map[string]any, error) {
	teamInstance := baseTeam

	teamSession := createAgentTeamSession(sess)
	// T-01: 对齐 Python 裸 await pre_run 异常传播（team_runner.py L299）
	// PreRun 是 checkpoint 恢复入口，失败时继续执行会用空状态跑完 session
	if preErr := teamSession.PreRun(ctx, inputs); preErr != nil {
		logger.Error(logComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunBaseTeam: PreRun failed")
		return nil, preErr
	}

	var teamRuntime *team_runtime.TeamRuntime
	if getter, ok := teamInstance.(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	// M-53: 清理 defer 化——对齐 Python try/finally（team_runner.py L303-308）：
	// Invoke panic 时 Unbind/PostRun 照常执行。
	defer func() {
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(ctx)
	}()

	// Python: L304 — return await team_instance.invoke(inputs, session=team_session)
	var result any
	var invokeErr error
	result, invokeErr = teamInstance.Invoke(ctx, inputs, maschema.WithTeamSession(teamSession))

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
// M-56: 签名换 maschema.BaseTeam 具体类型（原 any）。
// M-02: str 输入不在此解析——同 RunBaseTeam 的归档说明。
// S-71(A1): inputs 收紧 map[string]any。
// S-75/M-51: 清理闭包 + 两处调用——对齐 Python 生成器 finally 语义
// （team_runner.py L323-330：流耗尽/消费方停止后才 unbind+post_run）。
// 外层函数 return channel 时不能触发清理（流尚未消费，PostRun 提前
// commit 会因幂等标记导致最终状态永不持久化）。
func RunBaseTeamStreaming(
	ctx context.Context,
	baseTeam maschema.BaseTeam,
	inputs map[string]any,
	sess any,
) (<-chan stream.Schema, error) {
	teamInstance := baseTeam

	teamSession := createAgentTeamSession(sess)
	// T-01: 对齐 Python 裸 await pre_run 异常传播（team_runner.py L327）
	if preErr := teamSession.PreRun(ctx, inputs); preErr != nil {
		logger.Error(logComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunBaseTeamStreaming: PreRun failed")
		return nil, preErr
	}

	var teamRuntime *team_runtime.TeamRuntime
	if getter, ok := teamInstance.(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	// S-75/M-51: cleanup 闭包——PostRun 用 WithoutCancel 保证清理语义
	// 独立于调用方取消状态（checkpoint 持久化须运行到底）。
	cleanup := func() {
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(context.WithoutCancel(ctx))
	}

	agentCh, streamErr := teamInstance.Stream(ctx, inputs, maschema.WithTeamSession(teamSession))
	if streamErr != nil {
		// M-53: goroutine 未启动，streamErr 分支显式清理
		cleanup()
		return nil, streamErr
	}

	mergedCh := make(chan stream.Schema, 64)
	go func() {
		defer close(mergedCh)
		// S-75/M-51: 正常路径清理挂在流耗尽（或 ctx 取消退出）后执行
		defer cleanup()
		// S-58: 消费者停止读取（ctx 取消）与正常结束两路径都执行，
		// 对齐 Python generator finally 双路径语义（team_runner.py L264-275）
		for chunk := range agentCh {
			// S-58: select+ctx.Done 保护——消费者停止读取时 goroutine 不永久阻塞
			select {
			case mergedCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return mergedCh, nil
}

// RunTeamMember Member 路径（非流式）。
// Python: _TeamRunnerMixin._run_team_member(agent, inputs, *, session)
// T-01: 参数收窄到具体类型 *agent.TeamAgent——
// Python 名义签名 BaseAgent 实际窄（finalize_member 消费 TeamAgent 特有成员，
// 其他 BaseAgent 实例会 AttributeError）；Go 直接编码真实约束。
// 唯一工厂 spawn_manager 只产 *agent.TeamAgent，无第二实现。
func RunTeamMember(
	ctx context.Context,
	ag *agent.TeamAgent,
	inputs map[string]any,
	sess any,
) (map[string]any, error) {
	teamSession := createAgentTeamSession(sess)
	// T-01: 对齐 Python 裸 await pre_run 异常传播（team_runner.py L355）
	if preErr := teamSession.PreRun(ctx, inputs); preErr != nil {
		logger.Error(logComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunTeamMember: PreRun failed")
		return nil, preErr
	}

	var teamRuntime *team_runtime.TeamRuntime
	// T-01: 对齐 Python getattr(agent, "runtime", None)——TeamAgent 无 GetRuntime
	// 方法（两侧同源防御性分支，断言恒 false 保留对齐）
	if getter, ok := any(ag).(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	// M-53: 清理 defer 化——对齐 Python try/finally（team_runner.py L359-365）：
	// FinalizeMember/Unbind/PostRun 三步，Invoke panic 时同样执行。
	defer func() {
		_ = FinalizeMember(ctx, ag)
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(ctx)
	}()

	return ag.Invoke(ctx, inputs, agentinterfaces.WithSession(teamSession))
}

// RunTeamMemberStreaming Member 路径（流式）。
// Python: _TeamRunnerMixin._run_team_member_streaming(agent, inputs, *, session)
// T-01: 参数收窄到具体类型 *agent.TeamAgent（同 RunTeamMember）。
func RunTeamMemberStreaming(
	ctx context.Context,
	ag *agent.TeamAgent,
	inputs map[string]any,
	sess any,
) (<-chan stream.Schema, error) {
	teamSession := createAgentTeamSession(sess)
	// T-01: 对齐 Python 裸 await pre_run 异常传播（team_runner.py L377）
	if preErr := teamSession.PreRun(ctx, inputs); preErr != nil {
		logger.Error(logComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("RunTeamMemberStreaming: PreRun failed")
		return nil, preErr
	}

	var teamRuntime *team_runtime.TeamRuntime
	// T-01: 对齐 Python getattr(agent, "runtime", None)——TeamAgent 无 GetRuntime
	// 方法（两侧同源防御性分支，断言恒 false 保留对齐）
	if getter, ok := any(ag).(RuntimeGetter); ok {
		teamRuntime = getter.GetRuntime()
	}
	if teamRuntime != nil {
		teamRuntime.BindTeamSession(teamSession)
	}

	// S-75/M-51: cleanup 闭包——同 RunBaseTeamStreaming 模式，对齐
	// Python 生成器 finally（team_runner.py L384-388：流耗尽后才清理）。
	cleanup := func() {
		_ = FinalizeMember(context.WithoutCancel(ctx), ag)
		if teamRuntime != nil {
			teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
		}
		_ = teamSession.PostRun(context.WithoutCancel(ctx))
	}

	agentCh, streamErr := ag.Stream(ctx, inputs, agentinterfaces.WithSession(teamSession))
	if streamErr != nil {
		// M-53: goroutine 未启动，streamErr 分支显式清理
		cleanup()
		return nil, streamErr
	}

	mergedCh := make(chan stream.Schema, 64)
	go func() {
		defer close(mergedCh)
		// S-75/M-51: 正常路径清理挂在流耗尽（或 ctx 取消退出）后执行
		defer cleanup()
		for chunk := range agentCh {
			// S-58: select+ctx.Done 保护
			select {
			case mergedCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
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

// RegisterHumanAgentInbound 注册团队→用户通知回调（门面）。
// Python: Runner.register_human_agent_inbound(*, team_name, session_id, member_name, callback)
// (team_runner.py L877-892)
// S-04: server 层无入口调用 manager.RegisterHumanAgentInbound——
// manager.go:844 的注释指向本门面，补齐对齐 Python 的 classmethod 门面。
func RegisterHumanAgentInbound(
	ctx context.Context,
	teamName string,
	sessionID string,
	memberName string,
	callback tools.OnInbound,
) (bool, error) {
	return GetTeamRuntimeManager().RegisterHumanAgentInbound(ctx, teamName, sessionID, memberName, callback)
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
func CloseTeamInteractGate(ctx context.Context, teamName string, sessionID string) {
	closeTeamInteractGate(ctx, teamName, sessionID)
}

// BuildTeamRuntimeReadyChunk 构建团队运行时就绪首帧。
// Python: _TeamRunnerMixin._build_team_runtime_ready_chunk(...)
// M-22: leaderMemberName / leaderRole 用指针表达 Python 的 Optional[str]/Optional[Any]，
// nil 即 Python None。
func BuildTeamRuntimeReadyChunk(
	teamName string,
	sessionID string,
	actionKind RunActionKind,
	leaderMemberName *string,
	leaderRole *atschema.TeamRole,
) *atschema.TeamOutputSchema {
	return buildTeamRuntimeReadyChunk(teamName, sessionID, actionKind, leaderMemberName, leaderRole)
}

// BuildTeamControlChunk 构建团队控制帧。
// Python: _TeamRunnerMixin._build_team_control_chunk(payload, *, ...)
// M-22: 同 BuildTeamRuntimeReadyChunk——指针表达 Python 的 Optional。
func BuildTeamControlChunk(
	payload map[string]any,
	leaderMemberName *string,
	leaderRole *atschema.TeamRole,
) *atschema.TeamOutputSchema {
	return buildTeamControlChunk(payload, leaderMemberName, leaderRole)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runTeamAgent TeamAgent 路径（非流式）。
// Python: _TeamRunnerMixin.run_agent_team(agent_team, inputs, *, session)
// S-71(A1): agentTeam 收窄 *TeamAgentSpec（str 由外层经 ResolveTeamAgentSpec
// 解析），inputs 收窄 map[string]any。
func runTeamAgent(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	inputs map[string]any,
	sess any,
) (map[string]any, error) {
	activation, err := GetTeamRuntimeManager().Activate(ctx, spec, sess, inputs)
	if err != nil {
		return nil, err
	}

	// M-53: 清理代码 defer 化——对齐 Python try/finally（team_runner.py L183-204）：
	// invoke、reject 分支 return 都在 try 内，finally 三步清理（Finalize/closeGate/PostRun）
	// 两路径必执行；Invoke panic 时同样不漏清理。
	defer func() {
		_ = GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
		closeTeamInteractGate(ctx, spec.TeamName, activation.Session.GetSessionID())
		_ = activation.Session.PostRun(ctx)
	}()

	action := activation.Action
	if IsTeamRejectKind(action.Kind) {
		logger.Warn(logComponent).
			Str("team_name", spec.TeamName).
			Str("session_id", activation.Session.GetSessionID()).
			Str("kind", string(action.Kind)).
			Str("reason", action.Reason).
			Msg("run_agent_team rejected")
		return nil, nil
	}

	// S-24: 对齐 Python L194 activation.agent.invoke(inputs, session=...)——
	// 原硬编码 nil 导致团队首请求用户 query 整体丢失（TeamAgent.Invoke 读
	// inputs["query"] 得 nil map → PendingUserQuery 不设置 → 首轮空事件入队）。
	return activation.Agent.Invoke(ctx, inputs, agentinterfaces.WithSession(activation.Session))
}

// runTeamAgentStreaming TeamAgent 路径（流式）。
// Python: _TeamRunnerMixin.run_agent_team_streaming(...)
// S-71(A1): agentTeam 收窄 *TeamAgentSpec，inputs 收窄 map[string]any。
// S-75/M-51: 删除外层双重 defer——外层 return channel 时触发 PostRun 会因
// 幂等标记导致流耗尽后的最终状态永不持久化；改为 cleanup 闭包 + 两处
// 调用（reject/streamErr 显式、goroutine defer 正常路径）。
func runTeamAgentStreaming(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	inputs map[string]any,
	sess any,
	streamLogger *monitor.TeamStreamLogger,
) (<-chan stream.Schema, error) {
	activation, err := GetTeamRuntimeManager().Activate(ctx, spec, sess, inputs)
	if err != nil {
		return nil, err
	}

	// S-75/M-51: cleanup 闭包——WithoutCancel 保证 Finalize/closeGate/PostRun
	// 清理语义独立于调用方取消状态（PostRun 持久化 checkpoint 须运行到底）。
	cleanup := func() {
		if streamLogger != nil {
			streamLogger.Flush()
		}
		_ = GetTeamRuntimeManager().Finalize(context.WithoutCancel(ctx), spec.TeamName, activation.Session.GetSessionID())
		closeTeamInteractGate(ctx, spec.TeamName, activation.Session.GetSessionID())
		_ = activation.Session.PostRun(context.WithoutCancel(ctx))
	}

	action := activation.Action
	if IsTeamRejectKind(action.Kind) {
		logger.Warn(logComponent).
			Str("team_name", spec.TeamName).
			Str("session_id", activation.Session.GetSessionID()).
			Str("kind", string(action.Kind)).
			Str("reason", action.Reason).
			Msg("run_agent_team_streaming rejected")
		// S-25: 流式 reject 返回 error 而非 nil channel——
		// Python 流式路径 reject 直接 return（零 chunk 立即结束），
		// Go nil channel 会令调用方 range 永久阻塞；返回 error 走
		// 调用方现成错误分支（deep_adapter 广播 team.error / child.go
		// 包装上抛），错误信息携带具体 kind/reason。
		cleanup()
		return nil, exception.BuildError(
			exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"team rejected: kind=%s reason=%s", action.Kind, action.Reason)),
		)
	}

	// S-62: 对齐 Python L248-249 blueprint.member_name——成员标识而非显示名。
	// 原先 AgentCard().Name 是显示名，填入 TeamOutputSchema.source_member 会导致
	// readyChunk 成员归属错乱。
	// M-22: 严格对齐 Python getattr(blueprint, ..., None) 的 None 语义——
	// blueprint 缺失时 member_name / role 均为 None（Go 用 nil 表达），
	// 不再以空串 + TeamRoleLeader 兜底。
	var leaderMemberName *string
	var leaderRole *atschema.TeamRole
	if bp := activation.Agent.Blueprint(); bp != nil {
		leaderMemberName = bp.MemberName()
		role := bp.Role()
		leaderRole = &role
	}

	readyChunk := buildTeamRuntimeReadyChunk(
		spec.TeamName,
		activation.Session.GetSessionID(),
		action.Kind,
		leaderMemberName,
		leaderRole,
	)

	// S-24: 对齐 Python L260 activation.agent.stream(inputs, session=...)——
	// 与非流式路径同因：原硬编码 nil 导致流式团队首请求用户 query 整体丢失。
	agentCh, streamErr := activation.Agent.Stream(ctx, inputs, agentinterfaces.WithSession(activation.Session))
	if streamErr != nil {
		// M-53: goroutine 未启动，streamErr 分支显式清理
		cleanup()
		return nil, streamErr
	}

	mergedCh := make(chan stream.Schema, 64)
	go func() {
		defer close(mergedCh)
		// S-58/S-75: 正常路径清理挂在流耗尽（或 ctx 取消退出）后执行
		// （对齐 Python generator finally 双路径语义，team_runner.py L384-388）
		defer cleanup()
		if streamLogger != nil {
			streamLogger.Feed(readyChunk)
		}
		// S-58: readyChunk 发送同样防御性 select 化
		select {
		case mergedCh <- readyChunk:
		case <-ctx.Done():
			return
		}
		for chunk := range agentCh {
			if streamLogger != nil {
				streamLogger.Feed(chunk)
			}
			// S-58: select+ctx.Done 保护
			select {
			case mergedCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return mergedCh, nil
}

// resolveSpecFromSessionBucket 从 session bucket 读取持久化的 TeamAgentSpec。
// Python: _TeamRunnerMixin._resolve_spec_from_session_bucket(*, team_name, session)
func resolveSpecFromSessionBucket(ctx context.Context, teamName string, sess any) *atschema.TeamAgentSpec {
	teamSession := createAgentTeamSession(sess)

	if preErr := teamSession.PreRun(ctx); preErr != nil {
		logger.Warn(logComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("resolveSpecFromSessionBucket: PreRun failed")
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
		logger.Warn(logComponent).
			Str("team_name", teamName).
			Str("session_id", teamSession.GetSessionID()).
			Msg("resolveSpecFromSessionBucket: spec data type is not map[string]any")
		return nil
	}
	spec := atschema.NewTeamAgentSpecFromDict(specMap)
	if spec == nil {
		logger.Warn(logComponent).
			Str("team_name", teamName).
			Str("session_id", teamSession.GetSessionID()).
			Msg("resolveSpecFromSessionBucket: spec deserialization failed")
		return nil
	}
	return spec
}

// createAgentTeamSession 从 session 参数创建 AgentTeamSession。
// Python: _TeamRunnerMixin._create_agent_team_session(agent_team, session)（team_runner.py L682-695）
// T-01: 删除 Python 同源死参数 agent_team——Python 方法体内完全未使用该参数
// （只消费 session），Go 侧不保留死参数。
func createAgentTeamSession(sess any) *session.AgentTeamSession {
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
// M-54: ctx + WithoutCancel+30s——清理语义独立于调用方取消状态，
// inflight 载荷消费方卡死时 30s 兜底（S-30 FinalizeRound 同模式）。
func closeTeamInteractGate(ctx context.Context, teamName string, sessionID string) {
	entry := GetTeamRuntimeManager().Pool().Get(teamName)
	if entry == nil || entry.SessionID != sessionID {
		return
	}
	gateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = entry.InteractGate.CloseAndDrain(gateCtx)
}

// buildTeamRuntimeReadyChunk 构建团队运行时就绪首帧。
// Python: _TeamRunnerMixin._build_team_runtime_ready_chunk(...)
func buildTeamRuntimeReadyChunk(
	teamName string,
	sessionID string,
	actionKind RunActionKind,
	leaderMemberName *string,
	leaderRole *atschema.TeamRole,
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
	// M-22: 直接透传指针——nil 即 Python None，不再造空串/默认角色。
	return atschema.NewTeamOutputSchema(base, leaderMemberName, leaderRole)
}

// buildTeamControlChunk 构建团队控制帧。
// Python: _TeamRunnerMixin._build_team_control_chunk(payload, *, ...)
func buildTeamControlChunk(
	payload map[string]any,
	leaderMemberName *string,
	leaderRole *atschema.TeamRole,
) *atschema.TeamOutputSchema {
	base := stream.OutputSchema{
		Type:    "message",
		Index:   0,
		Payload: payload,
	}
	// M-22: 直接透传指针——nil 即 Python None。
	return atschema.NewTeamOutputSchema(base, leaderMemberName, leaderRole)
}

// bindInteractTeamSession 绑定交互团队 session 到 context。
// Python: _TeamRunnerMixin._bind_interact_team_session(session_id)（team_runner.py L140）
// M-02: 对齐 Python `if session_id and get_session_id() != session_id` 的跳过分支——
// 当前 context 已绑定同一 sessionID 时不重复覆盖（保留既有 SessionState 携带的其他状态）。
func bindInteractTeamSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	if current := sessionctx.GetSessionID(ctx); current == sessionID {
		return ctx
	}
	state := sessionctx.InitSessionState()
	state.SetSessionID(sessionID)
	return sessionctx.WithSessionState(ctx, state)
}
